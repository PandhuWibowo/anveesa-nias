package handlers

// Alibaba Cloud Simple Log Service (SLS) — read-only log search for "sls"
// connections. A connection maps onto SLS like this:
//
//	host     → region endpoint, e.g. ap-southeast-3.log.aliyuncs.com
//	database → project name
//	username → AccessKey ID
//	password → AccessKey secret (AES-encrypted at rest)
//
// The client speaks the SLS REST API directly with the v1 (hmac-sha1)
// signature, mirroring aliyun-log-go-sdk, so no SDK dependency is pulled in.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
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

const (
	slsAPIVersion   = "0.6.0"
	slsMaxLines     = 100 // SLS caps GetLogs at 100 lines per call
	slsDefaultRange = 15 * time.Minute
)

var slsHTTPClient = &http.Client{Timeout: 30 * time.Second}

type slsClient struct {
	endpoint  string // region endpoint without scheme
	project   string
	keyID     string
	keySecret string
}

type slsAPIError struct {
	Status  int
	Code    string `json:"errorCode"`
	Message string `json:"errorMessage"`
}

// slsInputError marks a bad request parameter (400) as opposed to an
// upstream SLS failure (502).
type slsInputError string

func (e slsInputError) Error() string { return string(e) }

func (e *slsAPIError) Error() string {
	if e.Code == "" && e.Message == "" {
		return fmt.Sprintf("SLS returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// normalizeSLSEndpoint strips a scheme, path and the project prefix someone
// may have pasted from the console URL, leaving the bare region endpoint.
func normalizeSLSEndpoint(raw, project string) string {
	ep := strings.TrimSpace(raw)
	ep = strings.TrimPrefix(ep, "https://")
	ep = strings.TrimPrefix(ep, "http://")
	if i := strings.Index(ep, "/"); i >= 0 {
		ep = ep[:i]
	}
	if project != "" {
		ep = strings.TrimPrefix(ep, project+".")
	}
	return ep
}

func newSLSClient(in ConnectionInput) *slsClient {
	project := strings.TrimSpace(in.Database)
	return &slsClient{
		endpoint:  normalizeSLSEndpoint(in.Host, project),
		project:   project,
		keyID:     strings.TrimSpace(in.Username),
		keySecret: in.Password,
	}
}

// slsCanonicalResource is the path plus the query sorted by key, with raw
// (unescaped) values, as the v1 signature expects.
func slsCanonicalResource(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte('?')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		for _, v := range q[k] {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

// slsSign returns the Authorization header value for a body-less request.
// headers must already hold Date and every x-log-* / x-acs-* header.
func slsSign(keyID, keySecret, method, path string, q url.Values, headers map[string]string) string {
	var logKeys []string
	logHeaders := map[string]string{}
	for k, v := range headers {
		l := strings.ToLower(strings.TrimSpace(k))
		if strings.HasPrefix(l, "x-log-") || strings.HasPrefix(l, "x-acs-") {
			logHeaders[l] = strings.TrimSpace(v)
			logKeys = append(logKeys, l)
		}
	}
	sort.Strings(logKeys)
	canon := make([]string, len(logKeys))
	for i, k := range logKeys {
		canon[i] = k + ":" + logHeaders[k]
	}

	signStr := method + "\n" +
		"" + "\n" + // Content-MD5: no body
		headers["Content-Type"] + "\n" +
		headers["Date"] + "\n" +
		strings.Join(canon, "\n") + "\n" +
		slsCanonicalResource(path, q)

	mac := hmac.New(sha1.New, []byte(keySecret))
	mac.Write([]byte(signStr))
	return "LOG " + keyID + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// get performs a signed GET against the project host and decodes JSON into out.
func (c *slsClient) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	host := c.project + "." + c.endpoint
	headers := map[string]string{
		"Date":                  time.Now().UTC().Format(http.TimeFormat),
		"x-log-apiversion":      slsAPIVersion,
		"x-log-signaturemethod": "hmac-sha1",
		"x-log-bodyrawsize":     "0",
	}
	auth := slsSign(c.keyID, c.keySecret, http.MethodGet, path, q, headers)

	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: q.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")

	resp, err := slsHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := &slsAPIError{Status: resp.StatusCode}
		_ = json.Unmarshal(body, apiErr)
		return resp.Header, apiErr
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.Header, fmt.Errorf("unexpected SLS response: %w", err)
		}
	}
	return resp.Header, nil
}

func (c *slsClient) listLogstores(ctx context.Context, size int) ([]string, error) {
	var out struct {
		Logstores []string `json:"logstores"`
	}
	q := url.Values{"offset": {"0"}, "size": {strconv.Itoa(size)}}
	if _, err := c.get(ctx, "/logstores", q, &out); err != nil {
		return nil, err
	}
	if out.Logstores == nil {
		out.Logstores = []string{}
	}
	sort.Strings(out.Logstores)
	return out.Logstores, nil
}

func testSLSInput(ctx context.Context, in ConnectionInput) error {
	_, err := newSLSClient(in).listLogstores(ctx, 1)
	return err
}

func openSLSClient(connID int64) (*slsClient, error) {
	var in ConnectionInput
	var encPassword string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(
		`SELECT driver, COALESCE(host,''), COALESCE(database,''), COALESCE(username,''), COALESCE(password,'') FROM connections WHERE id=?`),
		connID,
	).Scan(&in.Driver, &in.Host, &in.Database, &in.Username, &encPassword)
	if err != nil {
		return nil, fmt.Errorf("connection not found")
	}
	if in.Driver != "sls" {
		return nil, fmt.Errorf("connection is not an Alibaba Cloud SLS project")
	}
	secret, err := decryptCredential(encPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt AccessKey secret")
	}
	in.Password = secret
	return newSLSClient(in), nil
}

// slsTimeRange reads from/to (unix seconds) from the query, defaulting to the
// last 15 minutes.
func slsTimeRange(q url.Values) (int64, int64, error) {
	now := time.Now().Unix()
	to := now
	from := now - int64(slsDefaultRange/time.Second)
	if v := q.Get("to"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, slsInputError("invalid to")
		}
		to = n
	}
	if v := q.Get("from"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, slsInputError("invalid from")
		}
		from = n
	}
	if from >= to {
		return 0, 0, slsInputError("from must be before to")
	}
	return from, to, nil
}

func slsLogstorePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/?#") {
		return "", slsInputError("logstore is required")
	}
	return "/logstores/" + name, nil
}

// slsHandler resolves the connection and writes upstream errors uniformly.
func slsHandler(fn func(w http.ResponseWriter, r *http.Request, c *slsClient) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, err := openSLSClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := fn(w, r.WithContext(ctx), client); err != nil {
			status := http.StatusBadGateway
			if _, ok := err.(slsInputError); ok {
				status = http.StatusBadRequest
			}
			http.Error(w, jsonError(err.Error()), status)
		}
	}
}

// SLSLogstores — GET /api/connections/{id}/sls/logstores
func SLSLogstores() http.HandlerFunc {
	return slsHandler(func(w http.ResponseWriter, r *http.Request, c *slsClient) error {
		stores, err := c.listLogstores(r.Context(), 500)
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(map[string]any{"project": c.project, "logstores": stores})
	})
}

type slsField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SLSFields — GET /api/connections/{id}/sls/fields?logstore=
// Lists the indexed fields. A logstore without an index returns an empty list
// and indexed=false, since searching it is then impossible.
func SLSFields() http.HandlerFunc {
	return slsHandler(func(w http.ResponseWriter, r *http.Request, c *slsClient) error {
		path, err := slsLogstorePath(r.URL.Query().Get("logstore"))
		if err != nil {
			return err
		}
		var idx struct {
			Keys map[string]struct {
				Type string `json:"type"`
			} `json:"keys"`
			Line json.RawMessage `json:"line"`
		}
		_, err = c.get(r.Context(), path+"/index", nil, &idx)
		if apiErr, ok := err.(*slsAPIError); ok && apiErr.Code == "IndexConfigNotExist" {
			return json.NewEncoder(w).Encode(map[string]any{"indexed": false, "full_text": false, "fields": []slsField{}})
		}
		if err != nil {
			return err
		}
		fields := make([]slsField, 0, len(idx.Keys))
		for name, k := range idx.Keys {
			fields = append(fields, slsField{Name: name, Type: k.Type})
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		return json.NewEncoder(w).Encode(map[string]any{
			"indexed":   true,
			"full_text": len(idx.Line) > 0 && string(idx.Line) != "null",
			"fields":    fields,
		})
	})
}

// SLSHistogram — GET /api/connections/{id}/sls/histogram?logstore=&from=&to=&query=
func SLSHistogram() http.HandlerFunc {
	return slsHandler(func(w http.ResponseWriter, r *http.Request, c *slsClient) error {
		q := r.URL.Query()
		path, err := slsLogstorePath(q.Get("logstore"))
		if err != nil {
			return err
		}
		from, to, err := slsTimeRange(q)
		if err != nil {
			return err
		}
		var buckets []struct {
			From  int64 `json:"from"`
			To    int64 `json:"to"`
			Count int64 `json:"count"`
		}
		params := url.Values{
			"type":  {"histogram"},
			"from":  {strconv.FormatInt(from, 10)},
			"to":    {strconv.FormatInt(to, 10)},
			"topic": {q.Get("topic")},
			"query": {q.Get("query")},
		}
		hdr, err := c.get(r.Context(), path, params, &buckets)
		if err != nil {
			return err
		}
		total, _ := strconv.ParseInt(hdr.Get("X-Log-Count"), 10, 64)
		out := make([]map[string]int64, 0, len(buckets))
		for _, b := range buckets {
			out = append(out, map[string]int64{"from": b.From, "to": b.To, "count": b.Count})
		}
		return json.NewEncoder(w).Encode(map[string]any{
			"count":    total,
			"complete": hdr.Get("X-Log-Progress") != "Incomplete",
			"buckets":  out,
		})
	})
}

// SLSLogs — GET /api/connections/{id}/sls/logs?logstore=&from=&to=&query=&line=&offset=&reverse=
// Rows come back as SLS returns them: flat string maps with __time__,
// __source__, __topic__ and __tag__:* alongside the log's own fields. For a
// query with a SQL part ("* | select ...") the rows are the result set.
func SLSLogs() http.HandlerFunc {
	return slsHandler(func(w http.ResponseWriter, r *http.Request, c *slsClient) error {
		q := r.URL.Query()
		path, err := slsLogstorePath(q.Get("logstore"))
		if err != nil {
			return err
		}
		from, to, err := slsTimeRange(q)
		if err != nil {
			return err
		}
		line, _ := strconv.Atoi(q.Get("line"))
		if line <= 0 || line > slsMaxLines {
			line = slsMaxLines
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		if offset < 0 {
			offset = 0
		}
		reverse := q.Get("reverse") != "false"
		params := url.Values{
			"type":    {"log"},
			"from":    {strconv.FormatInt(from, 10)},
			"to":      {strconv.FormatInt(to, 10)},
			"topic":   {q.Get("topic")},
			"query":   {q.Get("query")},
			"line":    {strconv.Itoa(line)},
			"offset":  {strconv.Itoa(offset)},
			"reverse": {strconv.FormatBool(reverse)},
		}
		var rows []map[string]any
		hdr, err := c.get(r.Context(), path, params, &rows)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		elapsed, _ := strconv.ParseInt(hdr.Get("X-Log-Elapsed-Millisecond"), 10, 64)
		return json.NewEncoder(w).Encode(map[string]any{
			"rows":       rows,
			"count":      len(rows),
			"complete":   hdr.Get("X-Log-Progress") != "Incomplete",
			"elapsed_ms": elapsed,
			"has_more":   len(rows) == line,
		})
	})
}
