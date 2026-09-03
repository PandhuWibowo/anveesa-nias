package handlers

// Regression test for a real production bug: signObjectStorageRequestFull
// used to sign a fixed host/x-amz-content-sha256/x-amz-date baseline only,
// ignoring any other header already set on the request. Alibaba OSS rejects
// that outright — "SignatureDoesNotMatch: HeadersNotSigned:
// X-AMZ-COPY-SOURCE" — since it (correctly, per the SigV4 spec) requires
// every header present on the wire to be included in SignedHeaders.
// Huawei OBS and MinIO didn't enforce this, which is why the same-bucket
// Move/Rename code path (the only caller that sets x-amz-copy-source) went
// this long without anyone hitting it.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func newSigningTestRequest(t *testing.T, extraHeaders map[string]string) *http.Request {
	t.Helper()
	req := &http.Request{
		Method: http.MethodPut,
		URL:    &url.URL{Scheme: "https", Host: "mybucket.oss-ap-southeast-5.aliyuncs.com", Path: "/dest/key.jpg"},
		Header: http.Header{},
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req
}

func signedHeadersOf(t *testing.T, req *http.Request) string {
	t.Helper()
	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatal("Authorization header was not set")
	}
	const marker = "SignedHeaders="
	i := strings.Index(auth, marker)
	if i < 0 {
		t.Fatalf("Authorization header missing SignedHeaders: %s", auth)
	}
	rest := auth[i+len(marker):]
	j := strings.Index(rest, ",")
	if j < 0 {
		t.Fatalf("Authorization header malformed: %s", auth)
	}
	return rest[:j]
}

func TestSignObjectStorageRequestFull_SignsExtraHeaders(t *testing.T) {
	// Same-bucket copy (Move/Rename) — the exact case that broke against
	// Alibaba OSS.
	req := newSigningTestRequest(t, map[string]string{
		"x-amz-copy-source": "/mybucket/source/key.jpg",
	})
	signObjectStorageRequestFull(req, "AKID", "SECRET", "ap-southeast-5", "s3", emptyPayloadHash(), nil)
	signed := signedHeadersOf(t, req)
	for _, want := range []string{"host", "x-amz-content-sha256", "x-amz-date", "x-amz-copy-source"} {
		if !strings.Contains(signed, want) {
			t.Errorf("SignedHeaders %q missing %q", signed, want)
		}
	}
}

func TestSignObjectStorageRequestFull_SignsRangeHeader(t *testing.T) {
	// openBucketObjectStream's resume-download support sets Range before
	// signing — same gap, different call site.
	req := newSigningTestRequest(t, map[string]string{
		"Range": "bytes=1024-",
	})
	signObjectStorageRequestFull(req, "AKID", "SECRET", "us-east-1", "s3", emptyPayloadHash(), nil)
	signed := signedHeadersOf(t, req)
	if !strings.Contains(signed, "range") {
		t.Errorf("SignedHeaders %q missing %q", signed, "range")
	}
}

func TestSignObjectStorageRequestFull_SignsBatchDeleteHeaders(t *testing.T) {
	// batchDeleteBucketObjects sets Content-MD5 and Content-Type before
	// signing — same gap, different call site.
	req := newSigningTestRequest(t, map[string]string{
		"Content-MD5":  "1B2M2Y8AsgTpgAmY7PhCfg==",
		"Content-Type": "application/xml",
	})
	signObjectStorageRequestFull(req, "AKID", "SECRET", "us-east-1", "s3", emptyPayloadHash(), nil)
	signed := signedHeadersOf(t, req)
	for _, want := range []string{"content-md5", "content-type"} {
		if !strings.Contains(signed, want) {
			t.Errorf("SignedHeaders %q missing %q", signed, want)
		}
	}
}

func TestSignObjectStorageRequestFull_NoExtraHeaders(t *testing.T) {
	// The plain list/head/delete case (no extra headers) must keep working
	// exactly as before — just host/x-amz-content-sha256/x-amz-date.
	req := newSigningTestRequest(t, nil)
	signObjectStorageRequestFull(req, "AKID", "SECRET", "us-east-1", "s3", emptyPayloadHash(), nil)
	signed := signedHeadersOf(t, req)
	if signed != "host;x-amz-content-sha256;x-amz-date" {
		t.Errorf("expected the plain baseline, got %q", signed)
	}
}

// emptyPayloadHash stands in for a real SHA-256 hex digest — these tests
// only assert on which header names end up in SignedHeaders, not on the
// signature bytes themselves, so any fixed placeholder value works.
func emptyPayloadHash() string {
	return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}
