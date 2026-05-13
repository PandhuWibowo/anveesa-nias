package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// ── Config CRUD ─────────────────────────────────────────────────────────────

type CloudProviderConfig struct {
	ID         int64  `json:"id"`
	ConnID     int64  `json:"conn_id"`
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	ProjectID  string `json:"project_id"`
	InstanceID string `json:"instance_id"`
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key,omitempty"` // omitted on reads
}

func GetCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		var cfg CloudProviderConfig
		err := appdb.DB.QueryRow(appdb.ConvertQuery(`
			SELECT id, conn_id, provider, region, project_id, instance_id, access_key
			FROM cloud_provider_configs WHERE conn_id = ?
		`), connID).Scan(&cfg.ID, &cfg.ConnID, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"configured": false})
			return
		}
		cfg.SecretKey = "" // never return secret key
		json.NewEncoder(w).Encode(map[string]any{"configured": true, "config": cfg})
	}
}

func SaveCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		var body CloudProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}
		if body.Region == "" || body.ProjectID == "" || body.InstanceID == "" || body.AccessKey == "" || body.SecretKey == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "all fields are required"})
			return
		}

		provider := body.Provider
		if provider == "" {
			provider = "huawei"
		}

		// Upsert
		_, err := appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO cloud_provider_configs (conn_id, provider, region, project_id, instance_id, access_key, secret_key, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(conn_id) DO UPDATE SET
				provider    = excluded.provider,
				region      = excluded.region,
				project_id  = excluded.project_id,
				instance_id = excluded.instance_id,
				access_key  = excluded.access_key,
				secret_key  = excluded.secret_key,
				updated_at  = CURRENT_TIMESTAMP
		`), connID, provider, body.Region, body.ProjectID, body.InstanceID, body.AccessKey, body.SecretKey)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func DeleteCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cloud_provider_configs WHERE conn_id = ?`), connID)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// ── Huawei Cloud RDS log proxy ──────────────────────────────────────────────

// CloudErrorLogs proxies Huawei RDS error log API.
func CloudErrorLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured for this connection"})
			return
		}

		q := r.URL.Query()
		startDate := q.Get("from")
		endDate := q.Get("to")
		level := q.Get("level")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 1 {
			offset = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 100 {
			limit = 50
		}

		// Huawei requires "yyyy-mm-ddThh:mm:ss+0000" format (literal +0000, not Z)
		if startDate == "" {
			startDate = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endDate == "" {
			endDate = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_date", startDate)
		params.Set("end_date", endDate)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))
		if level != "" {
			params.Set("level", strings.ToUpper(level))
		}

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/errorlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudSlowLogs proxies Huawei RDS slow query log API.
func CloudSlowLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured for this connection"})
			return
		}

		q := r.URL.Query()
		startDate := q.Get("from")
		endDate := q.Get("to")
		dbName := q.Get("db")
		stmtType := q.Get("type")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 1 {
			offset = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 100 {
			limit = 50
		}

		if startDate == "" {
			startDate = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endDate == "" {
			endDate = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_date", startDate)
		params.Set("end_date", endDate)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))
		if dbName != "" {
			params.Set("database", dbName)
		}
		if stmtType != "" && stmtType != "ALL" {
			params.Set("type", strings.ToUpper(stmtType))
		}

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/slowlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudAuditLogs lists Huawei RDS audit log files for a time range.
func CloudAuditLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured"})
			return
		}

		q := r.URL.Query()
		startTime := q.Get("from")
		endTime := q.Get("to")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 0 {
			offset = 0
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 50 {
			limit = 50
		}

		if startTime == "" {
			startTime = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endTime == "" {
			endTime = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_time", startTime)
		params.Set("end_time", endTime)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/auditlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudAuditLogLinks generates 5-minute download links for audit log files.
func CloudAuditLogLinks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured"})
			return
		}

		var reqBody struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil || len(reqBody.IDs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
			return
		}
		if len(reqBody.IDs) > 50 {
			reqBody.IDs = reqBody.IDs[:50]
		}

		bodyBytes, _ := json.Marshal(reqBody)
		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/auditlog-links",
			cfg.Region, cfg.ProjectID, cfg.InstanceID,
		)

		respBody, status, apiErr := huaweiRequest(cfg, "POST", apiURL, string(bodyBytes))
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(respBody)
	}
}

// ── Huawei AK/SK signer ─────────────────────────────────────────────────────

type cloudCfg struct {
	Provider   string
	Region     string
	ProjectID  string
	InstanceID string
	AccessKey  string
	SecretKey  string
}

func loadCloudConfig(connID int64) (*cloudCfg, error) {
	var cfg cloudCfg
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_configs WHERE conn_id = ?
	`), connID).Scan(&cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// huaweiRequest performs an AK/SK-signed request to Huawei Cloud API.
// It follows Huawei Cloud's standard AK/SK signing scheme where the HMAC key
// is the raw SecretKey (not a derived key like AWS SigV4).
func huaweiRequest(cfg *cloudCfg, method, rawURL, body string) ([]byte, int, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	dateLong := now.Format("20060102T150405Z")

	// Canonical query string (sorted, RFC 3986 percent-encoded — no + for spaces)
	queryParams := parsedURL.Query()
	queryKeys := make([]string, 0, len(queryParams))
	for k := range queryParams {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)
	canonicalQueryParts := make([]string, 0, len(queryKeys))
	for _, k := range queryKeys {
		for _, v := range queryParams[k] {
			canonicalQueryParts = append(canonicalQueryParts, rfc3986EncodeStr(k)+"="+rfc3986EncodeStr(v))
		}
	}
	canonicalQuery := strings.Join(canonicalQueryParts, "&")

	// Canonical headers (must be lowercase, sorted, terminated with \n each)
	host := parsedURL.Host
	canonicalHeaders := fmt.Sprintf("content-type:application/json\nhost:%s\nx-sdk-date:%s\n", host, dateLong)
	signedHeaders := "content-type;host;x-sdk-date"

	// Payload hash
	payloadHash := sha256Hex(body)

	// Canonical URI must end with "/" per Huawei signing spec, even though the
	// actual HTTP request does not include the trailing slash.
	canonicalURI := parsedURL.EscapedPath()
	if !strings.HasSuffix(canonicalURI, "/") {
		canonicalURI += "/"
	}

	// Canonical request
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// String to sign — Huawei uses SDK-HMAC-SHA256 with only date + hash (no credential scope)
	stringToSign := strings.Join([]string{
		"SDK-HMAC-SHA256",
		dateLong,
		sha256Hex(canonicalRequest),
	}, "\n")

	// Signing key is the raw SecretKey bytes (no derivation)
	signature := hex.EncodeToString(cloudHMAC([]byte(cfg.SecretKey), stringToSign))

	authHeader := fmt.Sprintf(
		"SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		cfg.AccessKey, signedHeaders, signature,
	)

	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sdk-Date", dateLong)
	req.Header.Set("Authorization", authHeader)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

// rfc3986EncodeStr percent-encodes a string per RFC 3986 (spaces become %20, not +).
func rfc3986EncodeStr(s string) string {
	return strings.NewReplacer("+", "%20").Replace(url.QueryEscape(s))
}

// rfc3986Encode encodes url.Values using RFC 3986 percent-encoding.
func rfc3986Encode(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(v))
	for _, k := range keys {
		for _, val := range v[k] {
			parts = append(parts, rfc3986EncodeStr(k)+"="+rfc3986EncodeStr(val))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func cloudHMAC(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
