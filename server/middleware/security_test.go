package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_StaticRoutesBypassLimiter(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, nil, "test-static")
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/permissions", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d to static route: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimit_ApiRoutesAreLimited(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, nil, "test-api")
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ip := "203.0.113.6:1234"

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d within budget: expected 200, got %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request over budget: expected 429, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != `{"error":"rate limit exceeded"}`+"\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestRateLimit_StaticAndApiHaveIndependentImpact(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, nil, "test-mixed")
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ip := "203.0.113.7:1234"

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("static request %d: expected 200, got %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first api request should still have its full budget: expected 200, got %d", rec.Code)
	}
}
