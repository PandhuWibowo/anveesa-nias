package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

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

// BackupToBucket streams a SQL dump from a source database connection and
// uploads it directly to an S3-compatible bucket.
// POST /api/backup/to-bucket
func BackupToBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			SourceConnID int64         `json:"source_conn_id"`
			Database     string        `json:"database"`
			DestConnID   int64         `json:"dest_conn_id"`
			Prefix       string        `json:"prefix"`    // filename prefix, e.g. "myapp"
			Subfolder    string        `json:"subfolder"` // optional path inside bucket
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

		// Check read permission on source
		if !CheckReadPermission(r, req.SourceConnID) {
			http.Error(w, `{"error":"permission denied on source connection"}`, http.StatusForbidden)
			return
		}

		// Validate database name
		if req.Database != "" && !validIdentifier.MatchString(req.Database) {
			http.Error(w, `{"error":"invalid database name"}`, http.StatusBadRequest)
			return
		}

		// Open source DB
		srcDB, driver, err := GetDB(req.SourceConnID)
		if err != nil {
			http.Error(w, `{"error":"source connection error: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}

		// Get destination bucket credentials
		dest, err := fetchBucketConn(req.DestConnID)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		// Generate the SQL dump into memory
		var buf bytes.Buffer
		if err := writeBackupDump(r.Context(), &buf, srcDB, driver, req.Database, req.Options); err != nil {
			http.Error(w, `{"error":"backup generation failed: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		// Build object key: subfolder/prefix_database_timestamp.sql
		ts := time.Now().UTC().Format("20060102_150405")
		prefix := strings.TrimSpace(req.Prefix)
		if prefix == "" {
			prefix = "backup"
		}
		dbPart := req.Database
		if dbPart == "" {
			dbPart = "db"
		}
		objectName := fmt.Sprintf("%s_%s_%s.sql", prefix, dbPart, ts)
		subfolder := strings.Trim(strings.TrimSpace(req.Subfolder), "/")
		if subfolder != "" {
			objectName = subfolder + "/" + objectName
		}

		// Upload to bucket
		objectSize := int64(buf.Len())
		if err := uploadToBucket(r.Context(), dest, objectName, buf.Bytes()); err != nil {
			http.Error(w, `{"error":"upload failed: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          true,
			"object_key":  objectName,
			"bucket":      dest.Bucket,
			"size_bytes":  objectSize,
			"uploaded_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
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

// ── S3-compatible upload ──────────────────────────────────────────────────────

func uploadToBucket(ctx interface{ Done() <-chan struct{} }, dest *bucketConnRow, objectKey string, data []byte) error {
	endpointHost := buildS3Host(dest)
	scheme := "https"
	if !dest.SSL {
		scheme = "http"
	}
	bucket := strings.Trim(dest.Bucket, "/")
	key := strings.TrimPrefix(objectKey, "/")

	uploadURL := fmt.Sprintf("%s://%s/%s/%s", scheme, endpointHost, url.PathEscape(bucket), url.PathEscape(key))

	payloadHash := sha256.Sum256(data)
	payloadHashHex := hex.EncodeToString(payloadHash[:])

	httpCtx, ok := ctx.(interface {
		Done() <-chan struct{}
		Value(interface{}) interface{}
		Err() error
		Deadline() (time.Time, bool)
	})
	_ = ok

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")

	region := objectStorageRegion(dest.Driver, endpointHost)
	service := objectStorageService(dest.Driver)
	signObjectStorageRequestFull(req, dest.Username, dest.Password, region, service, payloadHashHex, data)

	client := &http.Client{Timeout: 5 * time.Minute}
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

	listURL := fmt.Sprintf("%s://%s/%s?list-type=2&max-keys=200", scheme, endpointHost, url.PathEscape(bucket))
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

	// Parse minimal XML
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

// signObjectStorageRequestFull signs with the actual payload hash (for PUT).
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
