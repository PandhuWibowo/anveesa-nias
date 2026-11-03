package handlers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The expected value was produced by aliyun-log-go-sdk v0.1.131 SignerV1 for
// the same request, so this pins our signature to the official one.
func TestSLSSignMatchesSDK(t *testing.T) {
	q := url.Values{
		"type": {"log"}, "from": {"1762160000"}, "to": {"1762160900"}, "topic": {""},
		"query": {`level: error and content: "POST /api"`},
		"line":  {"100"}, "offset": {"0"}, "reverse": {"true"},
	}
	headers := map[string]string{
		"Date":                  "Mon, 03 Nov 2025 10:00:00 GMT",
		"x-log-apiversion":      "0.6.0",
		"x-log-bodyrawsize":     "0",
		"x-log-signaturemethod": "hmac-sha1",
	}
	got := slsSign("testKeyId", "testKeySecret", http.MethodGet, "/logstores/test-b2capp", q, headers)
	want := "LOG testKeyId:o3W+GkEKjPYuH2BbLjbaX1Gz2qw="
	if got != want {
		t.Fatalf("signature mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestNormalizeSLSEndpoint(t *testing.T) {
	cases := map[string]string{
		"ap-southeast-3.log.aliyuncs.com":                     "ap-southeast-3.log.aliyuncs.com",
		"https://ap-southeast-3.log.aliyuncs.com/":            "ap-southeast-3.log.aliyuncs.com",
		"https://my-proj.ap-southeast-3.log.aliyuncs.com/lst": "ap-southeast-3.log.aliyuncs.com",
		"  cn-hangzhou-intranet.log.aliyuncs.com ":            "cn-hangzhou-intranet.log.aliyuncs.com",
	}
	for in, want := range cases {
		if got := normalizeSLSEndpoint(in, "my-proj"); got != want {
			t.Errorf("normalizeSLSEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeSLS routes every dial to a local TLS server and checks that requests
// arrive on the project host, signed, with the expected path and query.
func fakeSLS(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	ts := httptest.NewTLSServer(handler)
	prev := slsHTTPClient
	slsHTTPClient = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, ts.Listener.Addr().String())
		},
	}}
	t.Cleanup(func() { slsHTTPClient = prev; ts.Close() })
}

func TestSLSClientGetLogs(t *testing.T) {
	fakeSLS(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "my-proj.ap-southeast-3.log.aliyuncs.com" {
			t.Errorf("host = %q", r.Host)
		}
		if r.URL.Path != "/logstores/app-logs" || r.URL.Query().Get("type") != "log" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		headers := map[string]string{"Date": r.Header.Get("Date")}
		for _, k := range []string{"x-log-apiversion", "x-log-bodyrawsize", "x-log-signaturemethod"} {
			headers[k] = r.Header.Get(k)
		}
		want := slsSign("AKID", "secret", r.Method, r.URL.Path, r.URL.Query(), headers)
		if r.Header.Get("Authorization") != want {
			t.Errorf("authorization = %q, want %q", r.Header.Get("Authorization"), want)
		}
		w.Header().Set("X-Log-Progress", "Complete")
		json.NewEncoder(w).Encode([]map[string]string{{"__time__": "1762160001", "level": "info"}})
	})

	c := &slsClient{endpoint: "ap-southeast-3.log.aliyuncs.com", project: "my-proj", keyID: "AKID", keySecret: "secret"}
	var rows []map[string]any
	hdr, err := c.get(context.Background(), "/logstores/app-logs", url.Values{"type": {"log"}, "query": {"*"}}, &rows)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(rows) != 1 || rows[0]["level"] != "info" || hdr.Get("X-Log-Progress") != "Complete" {
		t.Fatalf("unexpected result: %v %v", rows, hdr)
	}
}

func TestSLSClientSurfacesAPIError(t *testing.T) {
	fakeSLS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errorCode":"SignatureNotMatch","errorMessage":"signature is not matched"}`))
	})
	c := &slsClient{endpoint: "cn-hangzhou.log.aliyuncs.com", project: "p", keyID: "k", keySecret: "s"}
	_, err := c.listLogstores(context.Background(), 1)
	apiErr, ok := err.(*slsAPIError)
	if !ok || apiErr.Code != "SignatureNotMatch" || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected SignatureNotMatch API error, got %v", err)
	}
}
