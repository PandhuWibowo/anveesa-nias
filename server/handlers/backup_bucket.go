package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// ── Async backup job store ────────────────────────────────────────────────────

type BackupJobStatus string

const (
	BackupJobPending  BackupJobStatus = "pending"
	BackupJobRunning  BackupJobStatus = "running"
	BackupJobDone     BackupJobStatus = "done"
	BackupJobFailed   BackupJobStatus = "failed"
	BackupJobCanceled BackupJobStatus = "canceled"
)

type BackupJob struct {
	ID        string          `json:"id"`
	Status    BackupJobStatus `json:"status"`
	StartedAt time.Time       `json:"started_at"`
	DoneAt    *time.Time      `json:"done_at,omitempty"`

	// Live upload progress (updated atomically, no lock needed)
	Stage         string `json:"stage"`           // "dumping" | "uploading"
	UploadedBytes int64  `json:"uploaded_bytes"`  // bytes sent to bucket so far

	// Result (populated on done)
	ObjectKey         string `json:"object_key,omitempty"`
	Bucket            string `json:"bucket,omitempty"`
	SizeBytes         int64  `json:"size_bytes,omitempty"`
	UncompressedBytes int64  `json:"uncompressed_bytes,omitempty"`

	// Error (populated on failed)
	Error string `json:"error,omitempty"`

	cancel        context.CancelFunc
	uploadCounter *int64 // points to countingReader.n for live reads
	mu            sync.Mutex
}

var (
	backupJobs   sync.Map          // id → *BackupJob
	jobIDCounter uint64
)

func newJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getBackupJob(id string) (*BackupJob, bool) {
	v, ok := backupJobs.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*BackupJob), true
}

// bucketConnRow holds the S3 connection credentials fetched from the DB.
type bucketConnRow struct {
	Driver   string
	Host     string
	Port     int
	Bucket   string // stored in the "database" column
	Username string // access key
	Password string // secret key (encrypted)
	SSL      bool
}

func fetchBucketConn(connID int64) (*bucketConnRow, error) {
	var ssl int
	var encPassword string
	row := &bucketConnRow{}
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), COALESCE(database,''), COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`),
		connID,
	).Scan(&row.Driver, &row.Host, &row.Port, &row.Bucket, &row.Username, &encPassword, &ssl)
	if err != nil {
		return nil, fmt.Errorf("bucket connection not found")
	}
	if !isObjectStorageDriver(row.Driver) {
		return nil, fmt.Errorf("destination connection is not an object storage provider")
	}
	row.SSL = ssl == 1
	pw, err := decryptCredential(encPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret key")
	}
	row.Password = pw
	return row, nil
}

// countingReader wraps an io.Reader and counts bytes read (upload side).
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	atomic.AddInt64(&c.n, int64(n))
	return n, err
}

// countingWriter wraps an io.Writer and counts bytes written (dump side).
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	atomic.AddInt64(&c.n, int64(n))
	return n, err
}

type dumpStats struct {
	uncompressedBytes int64
	err               error
}

// BackupToBucket starts an async backup job and returns a job ID immediately.
// The actual dump+upload runs in a background goroutine; callers poll
// GET /api/backup/jobs/:id for status.
//
// POST /api/backup/to-bucket
func BackupToBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			SourceConnID int64         `json:"source_conn_id"`
			Database     string        `json:"database"`
			DestConnID   int64         `json:"dest_conn_id"`
			Prefix       string        `json:"prefix"`
			Subfolder    string        `json:"subfolder"`
			Options      BackupOptions `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if req.SourceConnID == 0 {
			http.Error(w, `{"error":"source_conn_id is required"}`, http.StatusBadRequest)
			return
		}
		if req.DestConnID == 0 {
			http.Error(w, `{"error":"dest_conn_id is required"}`, http.StatusBadRequest)
			return
		}
		if req.Options.Sections == "" {
			req.Options = DefaultBackupOptions()
		}

		if !CheckReadPermission(r, req.SourceConnID) {
			http.Error(w, `{"error":"permission denied on source connection"}`, http.StatusForbidden)
			return
		}
		if req.Database != "" && !validIdentifier.MatchString(req.Database) {
			http.Error(w, `{"error":"invalid database name"}`, http.StatusBadRequest)
			return
		}

		srcDB, driver, err := GetDB(req.SourceConnID)
		if err != nil {
			http.Error(w, `{"error":"source connection error: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		dest, err := fetchBucketConn(req.DestConnID)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		// Build object key
		ext := ".sql"
		if req.Options.Compress {
			ext = ".sql.gz"
		}
		ts := time.Now().UTC().Format("20060102_150405")
		prefix := strings.TrimSpace(req.Prefix)
		if prefix == "" {
			prefix = "backup"
		}
		dbPart := req.Database
		if dbPart == "" {
			dbPart = "db"
		}
		objectName := fmt.Sprintf("%s_%s_%s%s", prefix, dbPart, ts, ext)
		if sub := strings.Trim(strings.TrimSpace(req.Subfolder), "/"); sub != "" {
			objectName = sub + "/" + objectName
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &BackupJob{
			ID:        newJobID(),
			Status:    BackupJobRunning,
			Stage:     "dumping",
			StartedAt: time.Now(),
			cancel:    jobCancel,
		}
		backupJobs.Store(job.ID, job)

		// Run dump + upload in background so the HTTP response returns immediately.
		go func() {
			defer jobCancel()

			// Streaming pipeline: dump → [gzip] → pipe → S3 PUT
			pr, pw := io.Pipe()
			statsCh := make(chan dumpStats, 1)

			go func() {
				var gz *gzip.Writer
				var pipeOut io.Writer = pw
				if req.Options.Compress {
					gz, _ = gzip.NewWriterLevel(pw, gzip.BestCompression)
					pipeOut = gz
				}
				cw := &countingWriter{w: pipeOut}
				dumpErr := writeBackupDump(jobCtx, cw, srcDB, driver, req.Database, req.Options)
				if gz != nil && dumpErr == nil {
					dumpErr = gz.Close()
				}
				pw.CloseWithError(dumpErr)
				statsCh <- dumpStats{uncompressedBytes: atomic.LoadInt64(&cw.n), err: dumpErr}
			}()

			cr := &countingReader{r: pr}
			job.mu.Lock()
			job.Stage = "uploading"
			job.uploadCounter = &cr.n
			job.mu.Unlock()
			uploadErr := uploadToBucketStream(jobCtx, dest, objectName, cr)
			if uploadErr != nil {
				pr.CloseWithError(uploadErr)
			}
			<-statsCh // drain goroutine

			now := time.Now()
			job.mu.Lock()
			defer job.mu.Unlock()
			job.DoneAt = &now
			if uploadErr != nil || jobCtx.Err() != nil {
				if jobCtx.Err() != nil && uploadErr == nil {
					job.Status = BackupJobCanceled
				} else {
					job.Status = BackupJobFailed
					if uploadErr != nil {
						job.Error = uploadErr.Error()
					} else {
						job.Error = jobCtx.Err().Error()
					}
				}
				return
			}
			job.Status = BackupJobDone
			job.ObjectKey = objectName
			job.Bucket = dest.Bucket
			job.SizeBytes = atomic.LoadInt64(&cr.n)
		}()

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
	}
}

// GetBackupJobStatus returns the current status of a backup job.
// GET /api/backup/jobs/:id
func GetBackupJobStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/backup/jobs/")
		job, ok := getBackupJob(id)
		if !ok {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		job.mu.Lock()
		if job.uploadCounter != nil {
			job.UploadedBytes = atomic.LoadInt64(job.uploadCounter)
		}
		defer job.mu.Unlock()
		json.NewEncoder(w).Encode(job)
	}
}

// CancelBackupJob cancels a running backup job.
// DELETE /api/backup/jobs/:id
func CancelBackupJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/backup/jobs/")
		job, ok := getBackupJob(id)
		if !ok {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		job.mu.Lock()
		if job.Status == BackupJobRunning {
			job.cancel()
			job.Status = BackupJobCanceled
		}
		job.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

// uploadToBucketStream uploads body to S3 using chunked transfer encoding and
// UNSIGNED-PAYLOAD signing so the entire content never needs to be held in
// memory.  The HTTP client timeout is set to 4 hours to accommodate GB+ dumps.
func uploadToBucketStream(ctx interface {
	Done() <-chan struct{}
	Value(interface{}) interface{}
	Err() error
	Deadline() (time.Time, bool)
}, dest *bucketConnRow, objectKey string, body io.Reader) error {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	key := strings.TrimPrefix(objectKey, "/")

	virtualHost := bucket + "." + endpointHost
	uploadURL := fmt.Sprintf("%s://%s/%s", scheme, virtualHost, url.PathEscape(key))

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, body)
	if err != nil {
		return err
	}

	// Chunked transfer — no Content-Length needed, no full-body hash required
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Content-Type", "application/octet-stream")

	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)
	signObjectStorageUnsigned(req, dest.Username, dest.Password, region, service)

	// 4-hour timeout — generous for multi-GB uploads
	client := &http.Client{Timeout: 4 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("bucket returned HTTP %d", resp.StatusCode)
}

// signObjectStorageUnsigned signs an S3 PUT request using UNSIGNED-PAYLOAD so
// that the body hash is never computed — required for streaming uploads where
// the content length / hash is not known upfront.
func signObjectStorageUnsigned(req *http.Request, accessKey, secretKey, region, service string) {
	const payloadHash = "UNSIGNED-PAYLOAD"

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	hashReq := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credScope + "\n" + hex.EncodeToString(hashReq[:])

	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+secretKey), dateStamp), region), service), "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credScope, signedHeaders, sig,
	))
}

// uploadToBucket is a convenience wrapper for callers that already have the
// full payload in memory (e.g. pipeline CSV exports).  For large SQL dumps use
// uploadToBucketStream instead.
func uploadToBucket(ctx interface {
	Done() <-chan struct{}
	Value(interface{}) interface{}
	Err() error
	Deadline() (time.Time, bool)
}, dest *bucketConnRow, objectKey string, data []byte) error {
	return uploadToBucketStream(ctx, dest, objectKey, bytes.NewReader(data))
}

// PresignDownload returns a short-lived pre-signed download URL for a bucket object.
// The frontend fetches this endpoint (with auth), then opens the returned URL directly
// so the browser downloads from OBS/S3 without routing through the app server.
// GET /api/backup/presign?dest_conn_id=N&object_key=path/to/file.sql.gz
func PresignDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		destIDStr := r.URL.Query().Get("dest_conn_id")
		destID, err := strconv.ParseInt(destIDStr, 10, 64)
		if err != nil || destID == 0 {
			http.Error(w, `{"error":"dest_conn_id required"}`, http.StatusBadRequest)
			return
		}
		objectKey := strings.TrimPrefix(r.URL.Query().Get("object_key"), "/")
		if objectKey == "" {
			http.Error(w, `{"error":"object_key required"}`, http.StatusBadRequest)
			return
		}

		dest, err := fetchBucketConn(destID)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		signed, err := presignedDownloadURL(dest, objectKey, 60*time.Minute)
		if err != nil {
			http.Error(w, `{"error":"failed to sign URL: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"url": signed})
	}
}

// DownloadFromBucket proxies a file from S3-compatible storage to the browser.
// Used as fallback when the bucket is not publicly reachable from the browser.
// GET /api/backup/bucket-download?dest_conn_id=N&object_key=path/to/file.sql.gz
func DownloadFromBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		destIDStr := r.URL.Query().Get("dest_conn_id")
		destID, err := strconv.ParseInt(destIDStr, 10, 64)
		if err != nil || destID == 0 {
			http.Error(w, `{"error":"dest_conn_id required"}`, http.StatusBadRequest)
			return
		}
		objectKey := strings.TrimPrefix(r.URL.Query().Get("object_key"), "/")
		if objectKey == "" {
			http.Error(w, `{"error":"object_key required"}`, http.StatusBadRequest)
			return
		}

		dest, err := fetchBucketConn(destID)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		endpointHost := buildS3Host(dest)
		scheme := "https"
		if !dest.SSL {
			scheme = "http"
		}
		bucket := strings.Trim(dest.Bucket, "/")
		virtualHost := bucket + "." + endpointHost
		downloadURL := fmt.Sprintf("%s://%s/%s", scheme, virtualHost, url.PathEscape(objectKey))

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, downloadURL, nil)
		if err != nil {
			http.Error(w, `{"error":"failed to build request: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		payloadHash := sha256.Sum256([]byte{})
		payloadHashHex := hex.EncodeToString(payloadHash[:])
		region := objectStorageRegion(dest.Driver, endpointHost)
		service := objectStorageService(dest.Driver)
		signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, nil)

		client := &http.Client{Timeout: 4 * time.Hour}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, `{"error":"download failed: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			http.Error(w, fmt.Sprintf(`{"error":"bucket returned HTTP %d"}`, resp.StatusCode), http.StatusBadGateway)
			return
		}

		parts := strings.Split(objectKey, "/")
		filename := parts[len(parts)-1]
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		io.Copy(w, resp.Body)
	}
}

// presignedDownloadURL returns a time-limited pre-signed GET URL for an object.
// The browser can use this URL to download directly from the bucket without
// routing through the application server.
func presignedDownloadURL(dest *bucketConnRow, objectKey string, expires time.Duration) (string, error) {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	key := strings.TrimPrefix(objectKey, "/")
	virtualHost := bucket + "." + endpointHost
	objectURL := fmt.Sprintf("%s://%s/%s", scheme, virtualHost, url.PathEscape(key))

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)

	expireSecs := int(expires.Seconds())
	credScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	credential := dest.Username + "/" + credScope

	// Build canonical query string (params must be sorted alphabetically)
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(expireSecs))
	q.Set("X-Amz-SignedHeaders", "host")
	canonicalQuery := q.Encode() // url.Values.Encode() sorts keys

	canonicalHeaders := "host:" + virtualHost + "\n"
	canonicalURI := "/" + url.PathEscape(key)

	canonicalRequest := strings.Join([]string{
		"GET",
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		"host",           // signed headers
		"UNSIGNED-PAYLOAD",
	}, "\n")

	hashReq := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credScope + "\n" + hex.EncodeToString(hashReq[:])

	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+dest.Password), dateStamp), region), service), "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	finalURL := objectURL + "?" + canonicalQuery + "&X-Amz-Signature=" + sig
	return finalURL, nil
}

// ListBucketBackups lists objects in a bucket with an optional prefix filter.
// GET /api/backup/bucket-list?dest_conn_id=N&subfolder=backups/
func ListBucketBackups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		destIDStr := r.URL.Query().Get("dest_conn_id")
		destID, err := strconv.ParseInt(destIDStr, 10, 64)
		if err != nil || destID == 0 {
			http.Error(w, `{"error":"dest_conn_id required"}`, http.StatusBadRequest)
			return
		}
		subfolder := strings.Trim(r.URL.Query().Get("subfolder"), "/")

		dest, err := fetchBucketConn(destID)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		objects, err := listBucketObjects(r.Context(), dest, subfolder)
		if err != nil {
			http.Error(w, `{"error":"list failed: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"objects": objects,
			"bucket":  dest.Bucket,
		})
	}
}

// ── S3 list ───────────────────────────────────────────────────────────────────

type s3Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

func listBucketObjects(ctx interface {
	Done() <-chan struct{}
	Value(interface{}) interface{}
	Err() error
	Deadline() (time.Time, bool)
}, dest *bucketConnRow, prefix string) ([]s3Object, error) {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")

	virtualHost := bucket + "." + endpointHost
	listURL := fmt.Sprintf("%s://%s/?list-type=2&max-keys=200", scheme, virtualHost)
	if prefix != "" {
		listURL += "&prefix=" + url.QueryEscape(prefix+"/")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}

	payloadHash := sha256.Sum256([]byte{})
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)
	signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, nil)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bucket list returned HTTP %d", resp.StatusCode)
	}

	var objects []s3Object
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	xml := body.String()

	for {
		start := strings.Index(xml, "<Contents>")
		if start < 0 {
			break
		}
		end := strings.Index(xml[start:], "</Contents>")
		if end < 0 {
			break
		}
		block := xml[start : start+end+len("</Contents>")]
		xml = xml[start+end+len("</Contents>"):]

		obj := s3Object{}
		if k := extractXMLTag(block, "Key"); k != "" {
			obj.Key = k
		}
		if s := extractXMLTag(block, "Size"); s != "" {
			obj.Size, _ = strconv.ParseInt(s, 10, 64)
		}
		if lm := extractXMLTag(block, "LastModified"); lm != "" {
			obj.LastModified = lm
		}
		objects = append(objects, obj)
	}
	return objects, nil
}

func extractXMLTag(src, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	s := strings.Index(src, open)
	if s < 0 {
		return ""
	}
	s += len(open)
	e := strings.Index(src[s:], close)
	if e < 0 {
		return ""
	}
	return src[s : s+e]
}

func buildS3Host(dest *bucketConnRow) string {
	h := strings.TrimPrefix(strings.TrimPrefix(dest.Host, "https://"), "http://")
	h = strings.TrimRight(h, "/")
	if dest.Port > 0 && dest.Port != 80 && dest.Port != 443 && !strings.Contains(h, ":") {
		h = fmt.Sprintf("%s:%d", h, dest.Port)
	}
	return h
}

// signObjectStorageRequestFull signs with the actual payload hash (used for GET/HEAD/LIST).
func signObjectStorageRequestFull(req *http.Request, accessKey, secretKey, region, service, payloadHash string, _ []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	hashReq := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credScope + "\n" + hex.EncodeToString(hashReq[:])

	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+secretKey), dateStamp), region), service), "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credScope, signedHeaders, sig,
	))
}
