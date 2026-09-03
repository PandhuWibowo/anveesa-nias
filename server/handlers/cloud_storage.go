package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/golang-jwt/jwt/v5"
)

// Cloud Storage browses/manages objects in an already-connected object
// storage bucket (s3_aws / s3_gcp / s3_oss / s3_obs) — the same connections
// used as backup destinations in backup_bucket.go. It reuses that file's
// SigV4 plumbing (fetchBucketConn, buildS3Host, objectStorageRegion/Service,
// signObjectStorageRequestFull, uploadToBucketStream, openBucketObjectStream,
// listBucketObjects) rather than adding a new S3 client.

// listBucketPage lists one "folder level" under prefix using S3's
// delimiter=/ semantics: files land in Contents, subfolders land in
// CommonPrefixes. This is what makes the browser show folders instead of a
// flat recursive dump (listBucketObjects in backup_bucket.go intentionally
// has no delimiter — it's used for the flat backup-history list).
func listBucketPage(ctx context.Context, dest *bucketConnRow, prefix string) (files []s3Object, folders []string, truncated bool, err error) {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	virtualHost := bucket + "." + endpointHost

	continuationToken := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		q.Set("max-keys", "1000")
		q.Set("delimiter", "/")
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if continuationToken != "" {
			q.Set("continuation-token", continuationToken)
		}
		listURL := fmt.Sprintf("%s://%s/?%s", scheme, virtualHost, q.Encode())

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
		if reqErr != nil {
			return nil, nil, false, reqErr
		}
		payloadHash := sha256.Sum256([]byte{})
		payloadHashHex := hex.EncodeToString(payloadHash[:])
		region := objectStorageRegion(dest.Driver, endpointHost)
		service := objectStorageService(dest.Driver)
		signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, nil)

		client := &http.Client{Timeout: 15 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			return nil, nil, false, doErr
		}
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, nil, false, fmt.Errorf("bucket list returned HTTP %d", resp.StatusCode)
		}
		xmlBody := body.String()

		// Files (<Contents>) — skip the zero-byte folder-marker object itself.
		rest := xmlBody
		for {
			start := strings.Index(rest, "<Contents>")
			if start < 0 {
				break
			}
			end := strings.Index(rest[start:], "</Contents>")
			if end < 0 {
				break
			}
			block := rest[start : start+end+len("</Contents>")]
			rest = rest[start+end+len("</Contents>"):]

			key := extractXMLTag(block, "Key")
			if key == "" || key == prefix {
				continue
			}
			obj := s3Object{Key: key}
			if s := extractXMLTag(block, "Size"); s != "" {
				obj.Size, _ = strconv.ParseInt(s, 10, 64)
			}
			obj.LastModified = extractXMLTag(block, "LastModified")
			files = append(files, obj)
		}

		// Folders (<CommonPrefixes><Prefix>)
		rest = xmlBody
		for {
			start := strings.Index(rest, "<CommonPrefixes>")
			if start < 0 {
				break
			}
			end := strings.Index(rest[start:], "</CommonPrefixes>")
			if end < 0 {
				break
			}
			block := rest[start : start+end+len("</CommonPrefixes>")]
			rest = rest[start+end+len("</CommonPrefixes>"):]
			if p := extractXMLTag(block, "Prefix"); p != "" {
				folders = append(folders, p)
			}
		}

		if len(files)+len(folders) >= maxListedObjects {
			truncated = extractXMLTag(xmlBody, "IsTruncated") == "true"
			break
		}
		if extractXMLTag(xmlBody, "IsTruncated") != "true" {
			break
		}
		nextToken := extractXMLTag(xmlBody, "NextContinuationToken")
		if nextToken == "" {
			break
		}
		continuationToken = nextToken
	}
	return files, folders, truncated, nil
}

// bucketSignedRequest issues a signed request with a small (or empty)
// in-memory body — the shared plumbing behind delete/copy/mkdir. Large
// streamed bodies (upload/download) go through uploadToBucketStream /
// openBucketObjectStream instead, which sign UNSIGNED-PAYLOAD.
func bucketSignedRequest(ctx context.Context, dest *bucketConnRow, method, key string, headers map[string]string, body []byte) (*http.Response, error) {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	virtualHost := bucket + "." + endpointHost
	reqURL := fmt.Sprintf("%s://%s/%s", scheme, virtualHost, s3KeyPathEscape(key))

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)
	signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, nil)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody := new(bytes.Buffer)
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("bucket returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(errBody.String()))
	}
	return resp, nil
}

// uploadObjectSpooled uploads body to key with a real Content-Length header
// instead of Transfer-Encoding: chunked. uploadToBucketStream's chunked
// streaming (no upfront length) is what Huawei OBS accepts in this app's
// existing backup features, but strict S3 implementations — confirmed here
// against a local MinIO, and very likely real AWS S3, GCS, and Alibaba OSS
// too — reject a PUT with neither Content-Length nor AWS's own "aws-chunked"
// framing, returning 411 Length Required. Browser-uploaded files are small
// enough to spool to a temp file first (learns the size, gives a seekable
// body); the multi-GB DB-dump pipeline in backup_bucket.go deliberately
// overlaps generation with upload and can't be pre-sized this way, so it's
// intentionally left on the chunked path rather than changed here.
func uploadObjectSpooled(ctx context.Context, dest *bucketConnRow, key string, body io.Reader, contentType string) error {
	tmp, err := os.CreateTemp("", "nias-cloudstorage-upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	size, err := io.Copy(tmp, body)
	if err != nil {
		return fmt.Errorf("failed to buffer upload: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	uploadURL := fmt.Sprintf("%s://%s.%s/%s", scheme, bucket, endpointHost, s3KeyPathEscape(key))

	// Go's net/http treats ContentLength == 0 with a non-nil Body as
	// "unknown length" (falls back to chunked) — the exact 411 this
	// function exists to avoid. http.NoBody is the documented way to
	// signal a genuinely empty body (used by mkdir's zero-byte marker
	// objects and any zero-byte file upload).
	var reqBody io.Reader = tmp
	if size == 0 {
		reqBody = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, reqBody)
	if err != nil {
		return err
	}
	req.ContentLength = size
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)

	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)
	signObjectStorageUnsigned(req, dest.Username, dest.Password, region, service)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	errBody := new(bytes.Buffer)
	errBody.ReadFrom(resp.Body)
	return fmt.Errorf("bucket returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(errBody.String()))
}

func deleteBucketObject(ctx context.Context, dest *bucketConnRow, key string) error {
	resp, err := bucketSignedRequest(ctx, dest, http.MethodDelete, key, nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// batchDeleteBucketObjects uses S3's Multi-Object Delete API (POST /?delete=,
// up to 1000 keys per request) instead of one DELETE per key — the same
// batching the AWS SDK does internally, which matters here since a folder
// delete can easily involve thousands of objects and a one-request-per-key
// loop would be both slow and put unnecessary load on the bucket service.
func batchDeleteBucketObjects(ctx context.Context, dest *bucketConnRow, keys []string) (deleted int, err error) {
	if len(keys) == 0 {
		return 0, nil
	}
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	virtualHost := bucket + "." + endpointHost

	const batchSize = 1000
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]

		var sb strings.Builder
		sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Delete>`)
		for _, k := range batch {
			sb.WriteString("<Object><Key>")
			_ = xml.EscapeText(&sb, []byte(k))
			sb.WriteString("</Key></Object>")
		}
		sb.WriteString("<Quiet>true</Quiet></Delete>")
		body := []byte(sb.String())
		md5sum := md5.Sum(body)

		deleteURL := fmt.Sprintf("%s://%s/?delete=", scheme, virtualHost)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, deleteURL, bytes.NewReader(body))
		if reqErr != nil {
			return deleted, reqErr
		}
		req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(md5sum[:]))
		req.Header.Set("Content-Type", "application/xml")

		payloadHash := sha256.Sum256(body)
		payloadHashHex := hex.EncodeToString(payloadHash[:])
		region := objectStorageRegion(dest.Driver, endpointHost)
		service := objectStorageService(dest.Driver)
		signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, nil)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			return deleted, doErr
		}
		respBody := new(bytes.Buffer)
		respBody.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return deleted, fmt.Errorf("batch delete returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(respBody.String()))
		}
		deleted += len(batch)
	}
	return deleted, nil
}

func copyBucketObject(ctx context.Context, dest *bucketConnRow, srcKey, dstKey string) error {
	return copyBucketObjectWithHeaders(ctx, dest, srcKey, dstKey, nil)
}

// copyBucketObjectWithHeaders is copyBucketObject plus caller-supplied extra
// headers merged into the PUT-copy request — used by the metadata editor to
// set x-amz-metadata-directive: REPLACE plus new Content-Type/Cache-Control/
// x-amz-meta-* headers on a copy-to-self (srcKey == dstKey), since S3 has no
// in-place metadata edit.
func copyBucketObjectWithHeaders(ctx context.Context, dest *bucketConnRow, srcKey, dstKey string, extraHeaders map[string]string) error {
	bucket := strings.Trim(dest.Bucket, "/")
	copySource := "/" + bucket + "/" + s3KeyPathEscape(srcKey)
	headers := map[string]string{"x-amz-copy-source": copySource}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	resp, err := bucketSignedRequest(ctx, dest, http.MethodPut, dstKey, headers, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func cloudStorageSelfAuthOK(w http.ResponseWriter, r *http.Request) bool {
	if len(jwtSecret) == 0 {
		return true
	}
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(_ *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil || claims.UserID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	// Match RequireAnyAppPermissionHeader's admin bypass: appdb.HasUserAppPermission
	// checks the role's literal permissions JSON, which is only populated with
	// whatever permission keys existed at the time the role was seeded/edited —
	// newly added permissions (like these two) won't be in an existing admin
	// role's array even though admin should implicitly have everything.
	if claims.Role == "admin" {
		return true
	}
	if !appdb.HasUserAppPermission(claims.UserID, PermCloudStorageAccess) && !appdb.HasUserAppPermission(claims.UserID, PermCloudStorageManage) {
		http.Error(w, "insufficient permissions", http.StatusForbidden)
		return false
	}
	return true
}

// CloudStorageList lists one folder level (files + subfolders) under prefix.
// GET /api/connections/{id}/storage/list?prefix=
func CloudStorageList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		prefix := strings.Trim(r.URL.Query().Get("prefix"), "/")
		if prefix != "" {
			prefix += "/"
		}
		files, folders, truncated, err := listBucketPage(r.Context(), dest, prefix)
		if err != nil {
			http.Error(w, jsonError("list failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if files == nil {
			files = []s3Object{}
		}
		if folders == nil {
			folders = []string{}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"prefix":    prefix,
			"files":     files,
			"folders":   folders,
			"truncated": truncated,
			"bucket":    dest.Bucket,
		})
	}
}

// CloudStorageDownload streams a single object to the browser. Self-
// authenticates via ?token= (browser-native anchor-tag download).
// GET /api/connections/{id}/storage/download?key=&token=
func CloudStorageDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cloudStorageSelfAuthOK(w, r) {
			return
		}
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		if key == "" {
			http.Error(w, jsonError("key is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		resp, err := openBucketObjectStream(r.Context(), dest, key, 0)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(key)+`"`)
		io.Copy(w, resp.Body)
	}
}

// cloudStorageMaxRead caps how much of a file CloudStorageRead will return —
// matches sftpMaxRead's cap so the two preview code paths behave the same.
const cloudStorageMaxRead = 2 << 20 // 2 MiB

// CloudStorageRead returns a text preview of a remote object's content.
// Mirrors SftpRead in sftp.go exactly (same response shape) so the frontend
// preview modal can share logic between the SFTP and Cloud Storage browsers.
// GET /api/connections/{id}/storage/read?key=
func CloudStorageRead() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		if key == "" {
			http.Error(w, jsonError("key is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		resp, err := openBucketObjectStream(r.Context(), dest, key, 0)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var size int64
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			size, _ = strconv.ParseInt(cl, 10, 64)
		}
		buf, err := io.ReadAll(io.LimitReader(resp.Body, cloudStorageMaxRead))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		binary := bytes.IndexByte(buf, 0) != -1
		truncated := size > int64(len(buf)) || int64(len(buf)) >= cloudStorageMaxRead
		json.NewEncoder(w).Encode(map[string]any{
			"key":       key,
			"content":   string(buf),
			"size":      size,
			"truncated": truncated,
			"binary":    binary,
		})
	}
}

func cloudStorageZipFilename(keys []string) string {
	if len(keys) == 1 {
		base := path.Base(strings.TrimSuffix(keys[0], "/"))
		if base == "" || base == "." || base == "/" {
			base = "download"
		}
		return base + ".zip"
	}
	return fmt.Sprintf("download-%d.zip", time.Now().Unix())
}

func writeCloudObjectToZip(ctx context.Context, zw *zip.Writer, dest *bucketConnRow, key, entryName string) {
	resp, err := openBucketObjectStream(ctx, dest, key, 0)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	fw, err := zw.Create(entryName)
	if err != nil {
		return
	}
	io.Copy(fw, resp.Body)
}

// CloudStorageDownloadZip streams one or more objects/folders as a single ZIP
// — folders (keys ending "/") are expanded recursively via listBucketObjects
// with their structure preserved, matching SftpDownloadZip's behavior.
// Self-authenticates via ?token=.
// GET /api/connections/{id}/storage/zip?key=&key=...&token=
func CloudStorageDownloadZip() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cloudStorageSelfAuthOK(w, r) {
			return
		}
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		seen := map[string]bool{}
		var keys []string
		for _, k := range r.URL.Query()["key"] {
			k = strings.TrimSpace(k)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			http.Error(w, jsonError("at least one key is required"), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+cloudStorageZipFilename(keys)+`"`)

		zw := zip.NewWriter(w)
		defer zw.Close()

		for _, k := range keys {
			if strings.HasSuffix(k, "/") {
				trimmed := strings.TrimSuffix(k, "/")
				objs, err := listBucketObjects(r.Context(), dest, trimmed)
				if err != nil {
					continue
				}
				base := path.Base(trimmed)
				for _, obj := range objs {
					entryName := base + "/" + strings.TrimPrefix(obj.Key, k)
					writeCloudObjectToZip(r.Context(), zw, dest, obj.Key, entryName)
				}
			} else {
				writeCloudObjectToZip(r.Context(), zw, dest, k, path.Base(k))
			}
		}
	}
}

// CloudStorageUpload proxies one or more multipart files straight into the
// bucket under prefix (streamed part-by-part, never buffered on disk).
// POST /api/connections/{id}/storage/upload?prefix=
func CloudStorageUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		prefix := strings.Trim(r.URL.Query().Get("prefix"), "/")
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, jsonError("expected multipart upload"), http.StatusBadRequest)
			return
		}
		count := 0
		for {
			part, partErr := mr.NextPart()
			if partErr == io.EOF {
				break
			}
			if partErr != nil {
				http.Error(w, jsonError(partErr.Error()), http.StatusBadRequest)
				return
			}
			if part.FormName() != "file" || part.FileName() == "" {
				part.Close()
				continue
			}
			name := path.Base(part.FileName())
			key := name
			if prefix != "" {
				key = prefix + "/" + name
			}
			contentType := part.Header.Get("Content-Type")
			if err := uploadObjectSpooled(r.Context(), dest, key, part, contentType); err != nil {
				part.Close()
				http.Error(w, jsonError("upload failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			part.Close()
			count++
		}
		json.NewEncoder(w).Encode(map[string]int{"uploaded": count})
	}
}

// CloudStorageDelete deletes a single object.
// POST /api/connections/{id}/storage/delete  { "key": "..." }
func CloudStorageDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var body struct {
			Key string `json:"key"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Key) == "" {
			http.Error(w, jsonError("key is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := deleteBucketObject(r.Context(), dest, body.Key); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// CloudStorageDeletePrefix recursively deletes every object under a folder
// prefix (bounded by maxListedObjects, same safety cap as the bucket-history
// list in backup_bucket.go), then the folder marker itself.
// POST /api/connections/{id}/storage/delete-prefix  { "prefix": "..." }
func CloudStorageDeletePrefix() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var body struct {
			Prefix string `json:"prefix"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		trimmed := strings.Trim(body.Prefix, "/")
		if trimmed == "" {
			http.Error(w, jsonError("prefix is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		objs, err := listBucketObjects(r.Context(), dest, trimmed)
		if err != nil {
			http.Error(w, jsonError("list failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		keys := make([]string, len(objs))
		for i, obj := range objs {
			keys[i] = obj.Key
		}
		deleted, batchErr := batchDeleteBucketObjects(r.Context(), dest, keys)
		// Best-effort: also remove the zero-byte folder-marker object itself
		// (listBucketObjects already includes it if it exists as a real
		// object, so this is usually a harmless no-op safety net).
		_ = deleteBucketObject(r.Context(), dest, trimmed+"/")
		if batchErr != nil {
			http.Error(w, jsonError("delete failed after removing "+strconv.Itoa(deleted)+" of "+strconv.Itoa(len(keys))+" objects: "+batchErr.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"deleted": deleted, "failed": len(keys) - deleted})
	}
}

// CloudStorageRename renames/moves a single object or an entire folder
// (S3 has no native rename — implemented as copy-to-new-key + delete-old).
// POST /api/connections/{id}/storage/rename  { "from": "...", "to": "..." }
func CloudStorageRename() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var body struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.To) == "" {
			http.Error(w, jsonError("from and to are required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		if strings.HasSuffix(body.From, "/") {
			// Folder rename: copy every object under the prefix to the new
			// prefix, then delete the old ones (bounded, same as delete-prefix).
			fromPrefix := strings.TrimSuffix(body.From, "/")
			toPrefix := strings.TrimSuffix(body.To, "/")
			objs, err := listBucketObjects(r.Context(), dest, fromPrefix)
			if err != nil {
				http.Error(w, jsonError("list failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			// S3 has no batch-copy API, so copies still happen one at a
			// time — but the old keys can be removed in one batch-delete
			// call afterwards instead of one DELETE per object.
			var copiedKeys []string
			failed := 0
			for _, obj := range objs {
				newKey := toPrefix + strings.TrimPrefix(obj.Key, fromPrefix)
				if err := copyBucketObject(r.Context(), dest, obj.Key, newKey); err != nil {
					failed++
					continue
				}
				copiedKeys = append(copiedKeys, obj.Key)
			}
			moved, delErr := batchDeleteBucketObjects(r.Context(), dest, copiedKeys)
			_ = deleteBucketObject(r.Context(), dest, fromPrefix+"/")
			if delErr != nil {
				http.Error(w, jsonError("copied "+strconv.Itoa(len(copiedKeys))+" objects but failed to remove the originals: "+delErr.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"moved": moved, "failed": failed})
			return
		}

		if err := copyBucketObject(r.Context(), dest, body.From, body.To); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if err := deleteBucketObject(r.Context(), dest, body.From); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// CloudStorageMkdir creates a "folder" — a zero-byte object with a
// trailing-slash key, the standard S3 folder-marker convention.
// POST /api/connections/{id}/storage/mkdir  { "path": "..." }
func CloudStorageMkdir() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		key := strings.Trim(body.Path, "/")
		if key == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := uploadObjectSpooled(r.Context(), dest, key+"/", bytes.NewReader(nil), ""); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// CloudStorageGetMetadata HEADs an object and returns its Content-Type,
// size, ETag, Last-Modified, and any x-amz-meta-* custom metadata.
// GET /api/connections/{id}/storage/metadata?key=
func CloudStorageGetMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		if key == "" {
			http.Error(w, jsonError("key is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		resp, err := bucketSignedRequest(r.Context(), dest, http.MethodHead, key, nil, nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var size int64
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			size, _ = strconv.ParseInt(cl, 10, 64)
		}
		metadata := map[string]string{}
		for h, vals := range resp.Header {
			if len(vals) == 0 {
				continue
			}
			if lower := strings.ToLower(h); strings.HasPrefix(lower, "x-amz-meta-") {
				metadata[strings.TrimPrefix(lower, "x-amz-meta-")] = vals[0]
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"key":           key,
			"content_type":  resp.Header.Get("Content-Type"),
			"cache_control": resp.Header.Get("Cache-Control"),
			"size":          size,
			"etag":          strings.Trim(resp.Header.Get("ETag"), `"`),
			"last_modified": resp.Header.Get("Last-Modified"),
			"metadata":      metadata,
		})
	}
}

// CloudStorageUpdateMetadata rewrites an object's Content-Type,
// Cache-Control, and custom metadata. S3 has no in-place metadata edit —
// this is implemented as a copy-to-self with x-amz-metadata-directive:
// REPLACE, matching how Vestra's own metadata editor works for S3/OBS/OSS.
// POST /api/connections/{id}/storage/metadata
// { "key": "...", "content_type": "...", "cache_control": "...", "metadata": {...} }
func CloudStorageUpdateMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var body struct {
			Key          string            `json:"key"`
			ContentType  string            `json:"content_type"`
			CacheControl string            `json:"cache_control"`
			Metadata     map[string]string `json:"metadata"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Key) == "" {
			http.Error(w, jsonError("key is required"), http.StatusBadRequest)
			return
		}
		dest, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		headers := map[string]string{"x-amz-metadata-directive": "REPLACE"}
		if body.ContentType != "" {
			headers["Content-Type"] = body.ContentType
		}
		if body.CacheControl != "" {
			headers["Cache-Control"] = body.CacheControl
		}
		for k, v := range body.Metadata {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			headers["x-amz-meta-"+k] = v
		}
		if err := copyBucketObjectWithHeaders(r.Context(), dest, body.Key, body.Key, headers); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}
