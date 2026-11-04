package handlers

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pierrec/lz4/v4"
)

// Vectors come from aliyun-log-go-sdk v0.1.131: the GET one is its own
// SignerV1 test, the POST one is what its SignerV1 returns for the same body.
func TestSLSSignatureMatchesSDK(t *testing.T) {
	date := "Mon, 3 Jan 2010 08:33:47 GMT"
	postBody := `{"from":1762160000,"line":100,"offset":0,"powerSql":false,"query":"level: error | select count(*) as pv","reverse":true,"to":1762161000,"topic":""}`
	cases := []struct {
		name        string
		method      string
		path        string
		query       url.Values
		body        string
		contentType string
		want        string
	}{
		{name: "list logstores", method: http.MethodGet, path: "/logstores", want: "Rwm6cTKzoti4HWoe+GKcb6Kv07E="},
		{name: "list logstores paged", method: http.MethodGet, path: "/logstores", query: url.Values{"offset": {"0"}, "size": {"500"}}, want: "BJb4deyulr2TzuEKsxPL+9srlNc="},
		{name: "histogram with special characters", method: http.MethodGet, path: "/logstores/a_b", query: url.Values{"type": {"histogram"}, "from": {"1"}, "to": {"2"}, "query": {"a & b = c + d"}}, want: "NPtDfsjwCtBO1IWLPUCOL3iL94w="},
		{name: "get logs v3 body", method: http.MethodPost, path: "/logstores/test-b2capp/logs", body: postBody, contentType: "application/json", want: "w/09N6c1ZL+bJeNQuydQD3RYvd8="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("Date", date)
			h.Set("x-log-apiversion", "0.6.0")
			h.Set("x-log-signaturemethod", "hmac-sha1")
			h.Set("x-log-bodyrawsize", strconv.Itoa(len(tc.body)))
			contentMD5 := ""
			if tc.body != "" {
				contentMD5 = fmt.Sprintf("%X", md5.Sum([]byte(tc.body)))
			}
			got := slsSignature("mockAccessKeySecret", tc.method, contentMD5, tc.contentType, date, h, tc.path, tc.query)
			if got != tc.want {
				t.Fatalf("signature = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSLSEndpoint(t *testing.T) {
	cases := []struct {
		host, project, wantScheme, wantEndpoint string
	}{
		{"ap-southeast-5.log.aliyuncs.com", "proj", "https", "ap-southeast-5.log.aliyuncs.com"},
		{"https://ap-southeast-5.log.aliyuncs.com/", "proj", "https", "ap-southeast-5.log.aliyuncs.com"},
		{"http://cn-hangzhou-intranet.log.aliyuncs.com", "proj", "http", "cn-hangzhou-intranet.log.aliyuncs.com"},
		{"proj.ap-southeast-3.log.aliyuncs.com", "proj", "https", "ap-southeast-3.log.aliyuncs.com"},
		{"  ap-southeast-3.log.aliyuncs.com/logstores  ", "proj", "https", "ap-southeast-3.log.aliyuncs.com"},
	}
	for _, tc := range cases {
		scheme, endpoint := slsEndpoint(tc.host, tc.project)
		if scheme != tc.wantScheme || endpoint != tc.wantEndpoint {
			t.Errorf("slsEndpoint(%q) = %s, %s; want %s, %s", tc.host, scheme, endpoint, tc.wantScheme, tc.wantEndpoint)
		}
	}
}

func TestNormalizeSLSLogsRequest(t *testing.T) {
	now := time.Unix(1762161000, 0)
	cases := []struct {
		name    string
		in      slsLogsRequest
		wantErr string
		check   func(t *testing.T, got slsLogsRequest)
	}{
		{
			name: "defaults to the last 15 minutes and 100 lines",
			in:   slsLogsRequest{Logstore: "test-b2capp"},
			check: func(t *testing.T, got slsLogsRequest) {
				if got.To != now.Unix() || got.From != now.Unix()-900 || got.Line != slsMaxLines {
					t.Fatalf("got from=%d to=%d line=%d", got.From, got.To, got.Line)
				}
			},
		},
		{
			name: "clamps line and offset",
			in:   slsLogsRequest{Logstore: "app_log", From: 1, To: 2, Line: 5000, Offset: -3},
			check: func(t *testing.T, got slsLogsRequest) {
				if got.Line != slsMaxLines || got.Offset != 0 {
					t.Fatalf("got line=%d offset=%d", got.Line, got.Offset)
				}
			},
		},
		{name: "rejects path traversal in logstore", in: slsLogsRequest{Logstore: "../shards"}, wantErr: "invalid logstore name"},
		{name: "rejects uppercase logstore", in: slsLogsRequest{Logstore: "AppLog"}, wantErr: "invalid logstore name"},
		{name: "rejects reversed range", in: slsLogsRequest{Logstore: "app", From: 20, To: 10}, wantErr: "from must be before to"},
		{name: "rejects ranges over 30 days", in: slsLogsRequest{Logstore: "app", From: 1, To: 1 + slsMaxRangeSec + 1}, wantErr: "30 days"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			err := normalizeSLSLogsRequest(&got, now)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestSLSDecompress(t *testing.T) {
	raw := []byte(strings.Repeat(`{"content":"hello"}`, 50))
	compressed := make([]byte, lz4.CompressBlockBound(len(raw)))
	n, err := lz4.CompressBlock(raw, compressed, nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed = compressed[:n]

	h := http.Header{}
	h.Set("x-log-compresstype", "lz4")
	h.Set("x-log-bodyrawsize", strconv.Itoa(len(raw)))
	got, err := slsDecompress(h, compressed)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("lz4 decompress failed: %v", err)
	}

	h.Set("x-log-bodyrawsize", strconv.Itoa(len(raw)+1))
	if _, err := slsDecompress(h, compressed); err == nil {
		t.Fatal("expected size mismatch error")
	}

	h.Set("x-log-compresstype", "zstd")
	if _, err := slsDecompress(h, compressed); err == nil {
		t.Fatal("expected unsupported compression error")
	}

	if got, err := slsDecompress(http.Header{}, raw); err != nil || string(got) != string(raw) {
		t.Fatal("uncompressed body should pass through")
	}
}

// fakeSLSServer answers like the SLS API after re-checking the request
// signature, and returns an http.Client that routes every SLS host to it.
func fakeSLSServer(t *testing.T, secret string) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Host != "proj.ap-southeast-5.log.aliyuncs.com" {
			t.Errorf("host = %s", r.Host)
		}
		contentMD5 := r.Header.Get("Content-MD5")
		if len(body) > 0 && contentMD5 != fmt.Sprintf("%X", md5.Sum(body)) {
			t.Errorf("Content-MD5 = %s", contentMD5)
		}
		want := "LOG AKID:" + slsSignature(secret, r.Method, contentMD5, r.Header.Get("Content-Type"), r.Header.Get("Date"), r.Header, r.URL.Path, r.URL.Query())
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"errorCode":"SignatureNotMatch","errorMessage":"signature not match"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/logstores":
			io.WriteString(w, `{"count":2,"total":2,"logstores":["web","api"]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/logstores/api/logs":
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil || req["query"] != "level: error" || req["line"] != float64(100) {
				t.Errorf("unexpected body: %s", body)
			}
			raw := []byte(`{"meta":{"progress":"Complete","count":1,"processedRows":42,"hasSQL":false},"data":[{"__time__":"1762161000","content":"boom"}]}`)
			compressed := make([]byte, lz4.CompressBlockBound(len(raw)))
			n, _ := lz4.CompressBlock(raw, compressed, nil)
			w.Header().Set("x-log-compresstype", "lz4")
			w.Header().Set("x-log-bodyrawsize", strconv.Itoa(len(raw)))
			w.Write(compressed[:n])
		case r.Method == http.MethodGet && r.URL.Path == "/logstores/api" && r.URL.Query().Get("type") == "histogram":
			if r.URL.Query().Get("query") != "level: error" {
				t.Errorf("histogram query = %q, want the search part only", r.URL.Query().Get("query"))
			}
			io.WriteString(w, `[{"from":1762160000,"to":1762160500,"count":3,"progress":"Complete"},{"from":1762160500,"to":1762161000,"count":4,"progress":"Complete"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"errorCode":"LogStoreNotExist","errorMessage":"logstore does not exist"}`)
		}
	}))
	t.Cleanup(server.Close)

	addr := server.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}

func fakeSLS(t *testing.T, secret string) *slsClient {
	t.Helper()
	client, err := newSLSClient(ConnectionInput{Driver: "alibaba_sls", Host: "ap-southeast-5.log.aliyuncs.com", Database: "proj", Username: "AKID", Password: secret})
	if err != nil {
		t.Fatal(err)
	}
	client.scheme = "http"
	client.http = fakeSLSServer(t, secret)
	return client
}

func TestSLSClientDo(t *testing.T) {
	client := fakeSLS(t, "secret")
	ctx := context.Background()

	logstores, err := client.listLogstores(ctx)
	if err != nil || strings.Join(logstores, ",") != "api,web" {
		t.Fatalf("listLogstores = %v, %v", logstores, err)
	}

	var result struct {
		Meta struct {
			ProcessedRows int64 `json:"processedRows"`
		} `json:"meta"`
		Data []map[string]string `json:"data"`
	}
	body := []byte(`{"line":100,"query":"level: error"}`)
	if _, err := client.do(ctx, http.MethodPost, "/logstores/api/logs", nil, body, &result); err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(result.Data) != 1 || result.Data[0]["content"] != "boom" || result.Meta.ProcessedRows != 42 {
		t.Fatalf("unexpected logs result: %+v", result)
	}

	_, err = client.do(ctx, http.MethodGet, "/logstores/missing", url.Values{"type": {"histogram"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "LogStoreNotExist") {
		t.Fatalf("expected SLS error code, got %v", err)
	}

	client.accessKeySecret = "wrong"
	if err := testSLSInput(ctx, ConnectionInput{}); err == nil {
		t.Fatal("expected validation error for empty input")
	}
	if _, err := client.listLogstores(ctx); err == nil || !strings.Contains(err.Error(), "SignatureNotMatch") {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestValidateConnectionInputSLS(t *testing.T) {
	base := ConnectionInput{Name: "sls", Driver: "alibaba_sls", Host: "ap-southeast-5.log.aliyuncs.com", Database: "proj", Username: "AKID", Password: "secret"}
	if err := validateConnectionInput(&base); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	cases := map[string]func(in *ConnectionInput){
		"SLS endpoint is required":                                      func(in *ConnectionInput) { in.Host = "" },
		"SLS project must be 3-63 lowercase letters, digits or hyphens": func(in *ConnectionInput) { in.Database = " " },
		"AccessKey ID is required":                                      func(in *ConnectionInput) { in.Username = "" },
	}
	for _, project := range []string{"x.evil.com/#", "Proj", "ab", "-proj"} {
		in := base
		in.Database = project
		if err := validateConnectionInput(&in); err == nil {
			t.Errorf("project %q should be rejected", project)
		}
	}
	keepSecret := base
	keepSecret.Password = ""
	if err := validateConnectionInput(&keepSecret); err != nil {
		t.Errorf("blank secret must be allowed so an edit keeps the stored one: %v", err)
	}
	if _, err := newSLSClient(keepSecret); err == nil {
		t.Error("newSLSClient must refuse an empty secret")
	}
	for want, mutate := range cases {
		in := base
		mutate(&in)
		if err := validateConnectionInput(&in); err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if _, err := buildDSN(base); err == nil {
		t.Error("buildDSN should refuse alibaba_sls")
	}
}

func TestSLSHandlers(t *testing.T) {
	initAccessTestDB(t)
	SetEncryptionKey("0123456789abcdef0123456789abcdef")
	original := slsHTTPClient
	slsHTTPClient = fakeSLSServer(t, "secret")
	t.Cleanup(func() { slsHTTPClient = original })

	encSecret, err := encryptCredential("secret")
	if err != nil {
		t.Fatal(err)
	}
	slsID := insertReturningID(t, `INSERT INTO connections (name, driver, host, port, database, username, password, ssl) VALUES ('sls','alibaba_sls','http://ap-southeast-5.log.aliyuncs.com',443,'proj','AKID','`+encSecret+`',0)`)
	redisID := insertReturningID(t, `INSERT INTO connections (name, driver, host, port, database, username, password, ssl) VALUES ('rd','redis','127.0.0.1',6379,'0','','',0)`)

	call := func(handler http.HandlerFunc, method string, connID int64, sub, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, fmt.Sprintf("/api/connections/%d/sls/%s", connID, sub), strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler(rec, req)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	t.Run("lists logstores", func(t *testing.T) {
		code, out := call(SLSLogstores(), http.MethodGet, slsID, "logstores", "")
		if code != http.StatusOK || out["project"] != "proj" || fmt.Sprint(out["logstores"]) != "[api web]" {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})

	t.Run("returns logs from the v3 endpoint", func(t *testing.T) {
		code, out := call(SLSLogs(), http.MethodPost, slsID, "logs", `{"logstore":"api","query":"level: error","from":1762160000,"to":1762161000}`)
		logs, _ := out["logs"].([]any)
		if code != http.StatusOK || len(logs) != 1 || out["progress"] != "Complete" || out["processed_rows"] != float64(42) {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})

	t.Run("histogram drops the analytic part and sums buckets", func(t *testing.T) {
		code, out := call(SLSHistogram(), http.MethodPost, slsID, "histogram", `{"logstore":"api","query":"level: error | select count(*) as pv","from":1762160000,"to":1762161000}`)
		if code != http.StatusOK || out["total"] != float64(7) {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})

	t.Run("rejects an invalid logstore before calling SLS", func(t *testing.T) {
		code, out := call(SLSLogs(), http.MethodPost, slsID, "logs", `{"logstore":"../x"}`)
		if code != http.StatusBadRequest || out["error"] != "invalid logstore name" {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})

	t.Run("test connection signs a list call", func(t *testing.T) {
		for secret, want := range map[string]int{"secret": http.StatusOK, "wrong": http.StatusBadGateway} {
			body := `{"name":"sls","driver":"alibaba_sls","host":"http://ap-southeast-5.log.aliyuncs.com","database":"proj","username":"AKID","password":"` + secret + `","ssl":true}`
			rec := httptest.NewRecorder()
			TestConnection()(rec, httptest.NewRequest(http.MethodPost, "/api/connections/test", strings.NewReader(body)))
			if rec.Code != want {
				t.Fatalf("secret %q: code=%d body=%s", secret, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("create requires the secret, update keeps the stored one", func(t *testing.T) {
		rec := httptest.NewRecorder()
		CreateConnection()(rec, httptest.NewRequest(http.MethodPost, "/api/connections", strings.NewReader(`{"name":"no-secret","driver":"alibaba_sls","host":"ap-southeast-5.log.aliyuncs.com","database":"proj","username":"AKID"}`)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "AccessKey secret is required") {
			t.Fatalf("create without secret: code=%d body=%s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		UpdateConnection()(rec, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/connections/%d", slsID), strings.NewReader(`{"name":"sls renamed","driver":"alibaba_sls","host":"http://ap-southeast-5.log.aliyuncs.com","database":"proj","username":"AKID"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("update without secret: code=%d body=%s", rec.Code, rec.Body.String())
		}
		if code, out := call(SLSLogstores(), http.MethodGet, slsID, "logstores", ""); code != http.StatusOK {
			t.Fatalf("stored secret lost after update: code=%d out=%v", code, out)
		}
	})

	t.Run("refuses a connection of another driver", func(t *testing.T) {
		code, out := call(SLSLogstores(), http.MethodGet, redisID, "logstores", "")
		if code != http.StatusBadGateway || out["error"] != "connection is not Alibaba Cloud SLS" {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})
}
