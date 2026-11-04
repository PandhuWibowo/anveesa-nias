package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/pierrec/lz4/v4"
)

// Alibaba Cloud Simple Log Service (SLS). A connection stores the region
// endpoint in host (e.g. ap-southeast-3.log.aliyuncs.com), the SLS project in
// database, and the AccessKey pair in username/password. Requests go to
// {project}.{endpoint} and are signed with the SLS v1 scheme (HMAC-SHA1 over
// the canonical x-log-/x-acs- headers and sorted query). Endpoints, signing and
// lz4 handling mirror the official aliyun-log-go-sdk, so no SDK dependency is
// needed.

const (
	slsAPIVersion  = "0.6.0"
	slsMaxLines    = 100
	slsMaxRangeSec = 30 * 24 * 60 * 60
)

var (
	slsLogstoreNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,62}$`)
	slsProjectNamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
)

var slsHTTPClient = &http.Client{Timeout: 30 * time.Second}

type slsClient struct {
	scheme          string
	endpoint        string
	project         string
	accessKeyID     string
	accessKeySecret string
	http            *http.Client
}

type slsError struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

type slsLogsRequest struct {
	Logstore string `json:"logstore"`
	Query    string `json:"query"`
	Topic    string `json:"topic"`
	From     int64  `json:"from"`
	To       int64  `json:"to"`
	Line     int    `json:"line"`
	Offset   int    `json:"offset"`
	Reverse  *bool  `json:"reverse"`
}

type slsHistogramBucket struct {
	From     int64  `json:"from"`
	To       int64  `json:"to"`
	Count    int64  `json:"count"`
	Progress string `json:"progress"`
}

func isSLSDriver(driver string) bool {
	return driver == "alibaba_sls"
}

// slsEndpoint strips the scheme, path and a leading "{project}." from the
// stored host so users can paste either the region endpoint or the full
// project host from the console. HTTPS unless the host says http:// (as the
// official SDK does for intranet endpoints).
func slsEndpoint(host, project string) (scheme, endpoint string) {
	scheme = "https"
	raw := strings.TrimSpace(host)
	if strings.HasPrefix(raw, "http://") {
		scheme = "http"
	}
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if slash := strings.Index(raw, "/"); slash >= 0 {
		raw = raw[:slash]
	}
	if project != "" {
		raw = strings.TrimPrefix(raw, project+".")
	}
	return scheme, raw
}

func newSLSClient(in ConnectionInput) (*slsClient, error) {
	project := strings.TrimSpace(in.Database)
	scheme, endpoint := slsEndpoint(in.Host, project)
	if endpoint == "" {
		return nil, fmt.Errorf("SLS endpoint is required")
	}
	if !slsProjectNamePattern.MatchString(project) {
		return nil, fmt.Errorf("invalid SLS project name")
	}
	if strings.TrimSpace(in.Username) == "" || strings.TrimSpace(in.Password) == "" {
		return nil, fmt.Errorf("AccessKey ID and AccessKey secret are required")
	}
	return &slsClient{
		scheme:          scheme,
		endpoint:        endpoint,
		project:         project,
		accessKeyID:     strings.TrimSpace(in.Username),
		accessKeySecret: strings.TrimSpace(in.Password),
		http:            slsHTTPClient,
	}, nil
}

func openSLSClient(connID int64) (*slsClient, error) {
	var in ConnectionInput
	var ssl, disconnected int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl, COALESCE(disconnected,0) FROM connections WHERE id=?`), connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl, &disconnected)
	if err != nil {
		return nil, fmt.Errorf("connection not found")
	}
	if disconnected == 1 {
		return nil, fmt.Errorf("connection is disconnected")
	}
	if !isSLSDriver(in.Driver) {
		return nil, fmt.Errorf("connection is not Alibaba Cloud SLS")
	}
	password, err := decryptCredential(encPassword)
	if err != nil {
		return nil, fmt.Errorf("decryption error")
	}
	in.Password = password
	in.SSL = ssl == 1
	return newSLSClient(in)
}

// slsSignature builds the v1 Authorization value:
// VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedLOGHeaders\nCanonicalizedResource
func slsSignature(secret, method, contentMD5, contentType, date string, headers http.Header, path string, query url.Values) string {
	var logHeaders []string
	for key, values := range headers {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-log-") || strings.HasPrefix(lower, "x-acs-") {
			logHeaders = append(logHeaders, lower+":"+strings.Join(values, ","))
		}
	}
	sort.Strings(logHeaders)

	resource := path
	if len(query) > 0 {
		keys := make([]string, 0, len(query))
		for k := range query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+"="+query.Get(k))
		}
		resource += "?" + strings.Join(pairs, "&")
	}

	stringToSign := strings.Join([]string{method, contentMD5, contentType, date, strings.Join(logHeaders, "\n"), resource}, "\n")
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (c *slsClient) do(ctx context.Context, method, path string, query url.Values, body []byte, out any) (http.Header, error) {
	requestURL := fmt.Sprintf("%s://%s.%s%s", c.scheme, c.project, c.endpoint, path)
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	contentMD5, contentType := "", ""
	if body != nil {
		contentMD5 = fmt.Sprintf("%X", md5.Sum(body))
		contentType = "application/json"
		req.Header.Set("Content-MD5", contentMD5)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept-Encoding", "lz4")
	}
	req.Header.Set("Date", date)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-log-apiversion", slsAPIVersion)
	req.Header.Set("x-log-signaturemethod", "hmac-sha1")
	req.Header.Set("x-log-bodyrawsize", strconv.Itoa(len(body)))
	signature := slsSignature(c.accessKeySecret, method, contentMD5, contentType, date, req.Header, path, query)
	req.Header.Set("Authorization", "LOG "+c.accessKeyID+":"+signature)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var slsErr slsError
		if json.Unmarshal(respBody, &slsErr) == nil && slsErr.ErrorCode != "" {
			return nil, fmt.Errorf("%s: %s", slsErr.ErrorCode, slsErr.ErrorMessage)
		}
		return nil, fmt.Errorf("SLS returned %s", resp.Status)
	}
	if respBody, err = slsDecompress(resp.Header, respBody); err != nil {
		return nil, err
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return nil, fmt.Errorf("invalid SLS response: %w", err)
		}
	}
	return resp.Header, nil
}

func slsDecompress(header http.Header, body []byte) ([]byte, error) {
	compressType := header.Get("x-log-compresstype")
	if compressType == "" {
		return body, nil
	}
	if compressType != "lz4" {
		return nil, fmt.Errorf("unsupported SLS compression: %s", compressType)
	}
	rawSize, err := strconv.Atoi(header.Get("x-log-bodyrawsize"))
	if err != nil || rawSize < 0 || rawSize > 64<<20 {
		return nil, fmt.Errorf("invalid SLS x-log-bodyrawsize")
	}
	out := make([]byte, rawSize)
	n, err := lz4.UncompressBlock(body, out)
	if err != nil {
		return nil, fmt.Errorf("decompress SLS response: %w", err)
	}
	if n != rawSize {
		return nil, fmt.Errorf("SLS response size %d does not match x-log-bodyrawsize %d", n, rawSize)
	}
	return out, nil
}

func (c *slsClient) listLogstores(ctx context.Context) ([]string, error) {
	var logstores []string
	offset := 0
	for {
		var page struct {
			Count     int      `json:"count"`
			Total     int      `json:"total"`
			Logstores []string `json:"logstores"`
		}
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("size", "500")
		if _, err := c.do(ctx, http.MethodGet, "/logstores", query, nil, &page); err != nil {
			return nil, err
		}
		logstores = append(logstores, page.Logstores...)
		offset += len(page.Logstores)
		if len(page.Logstores) == 0 || offset >= page.Total {
			break
		}
	}
	sort.Strings(logstores)
	return logstores, nil
}

func testSLSInput(ctx context.Context, in ConnectionInput) error {
	client, err := newSLSClient(in)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("offset", "0")
	query.Set("size", "1")
	_, err = client.do(ctx, http.MethodGet, "/logstores", query, nil, nil)
	return err
}

// normalizeSLSLogsRequest validates the logstore and clamps the time range
// (unix seconds, default last 15 minutes) and page size.
func normalizeSLSLogsRequest(body *slsLogsRequest, now time.Time) error {
	body.Logstore = strings.TrimSpace(body.Logstore)
	if !slsLogstoreNamePattern.MatchString(body.Logstore) {
		return fmt.Errorf("invalid logstore name")
	}
	body.Query = strings.TrimSpace(body.Query)
	if body.To <= 0 {
		body.To = now.Unix()
	}
	if body.From <= 0 {
		body.From = body.To - 15*60
	}
	if body.From >= body.To {
		return fmt.Errorf("from must be before to")
	}
	if body.To-body.From > slsMaxRangeSec {
		return fmt.Errorf("time range must be 30 days or less")
	}
	if body.Line <= 0 || body.Line > slsMaxLines {
		body.Line = slsMaxLines
	}
	if body.Offset < 0 {
		body.Offset = 0
	}
	return nil
}

func decodeSLSLogsRequest(w http.ResponseWriter, r *http.Request) (*slsClient, *slsLogsRequest, bool) {
	connID, err := connectionIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
		return nil, nil, false
	}
	var body slsLogsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
		return nil, nil, false
	}
	if err := normalizeSLSLogsRequest(&body, time.Now()); err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
		return nil, nil, false
	}
	client, err := openSLSClient(connID)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return nil, nil, false
	}
	return client, &body, true
}

func SLSLogstores() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, err := openSLSClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		logstores, err := client.listLogstores(r.Context())
		if err != nil {
			http.Error(w, jsonError("list logstores failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"project":   client.project,
			"logstores": logstores,
		})
	}
}

func SLSLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, body, ok := decodeSLSLogsRequest(w, r)
		if !ok {
			return
		}
		reverse := true
		if body.Reverse != nil {
			reverse = *body.Reverse
		}
		reqBody, err := json.Marshal(map[string]any{
			"from":     body.From,
			"to":       body.To,
			"topic":    body.Topic,
			"line":     body.Line,
			"offset":   body.Offset,
			"reverse":  reverse,
			"query":    body.Query,
			"powerSql": false,
		})
		if err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}

		start := time.Now()
		var result struct {
			Meta struct {
				Progress      string   `json:"progress"`
				Count         int64    `json:"count"`
				ProcessedRows int64    `json:"processedRows"`
				HasSQL        bool     `json:"hasSQL"`
				Keys          []string `json:"keys"`
			} `json:"meta"`
			Data []map[string]string `json:"data"`
		}
		if _, err := client.do(r.Context(), http.MethodPost, "/logstores/"+body.Logstore+"/logs", nil, reqBody, &result); err != nil {
			http.Error(w, jsonError("get logs failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		logs := result.Data
		if logs == nil {
			logs = []map[string]string{}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"logs":           logs,
			"count":          len(logs),
			"progress":       result.Meta.Progress,
			"processed_rows": result.Meta.ProcessedRows,
			"has_sql":        result.Meta.HasSQL,
			"keys":           result.Meta.Keys,
			"took_ms":        time.Since(start).Milliseconds(),
		})
	}
}

func SLSHistogram() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, body, ok := decodeSLSLogsRequest(w, r)
		if !ok {
			return
		}
		query := url.Values{}
		query.Set("type", "histogram")
		query.Set("from", strconv.FormatInt(body.From, 10))
		query.Set("to", strconv.FormatInt(body.To, 10))
		// Histograms count matching logs only; an analytic "| select" part
		// is not accepted here, so keep just the search statement.
		if search := strings.TrimSpace(strings.SplitN(body.Query, "|", 2)[0]); search != "" {
			query.Set("query", search)
		}
		if body.Topic != "" {
			query.Set("topic", body.Topic)
		}

		buckets := []slsHistogramBucket{}
		headers, err := client.do(r.Context(), http.MethodGet, "/logstores/"+body.Logstore, query, nil, &buckets)
		if err != nil {
			http.Error(w, jsonError("get histogram failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		var total int64
		for _, b := range buckets {
			total += b.Count
		}
		json.NewEncoder(w).Encode(map[string]any{
			"buckets":  buckets,
			"total":    total,
			"progress": headers.Get("x-log-progress"),
		})
	}
}
