package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter provides simple rate limiting per IP
type RateLimiter struct {
	mu       sync.RWMutex
	clients  map[string]*clientInfo
	rate     int           // requests per window
	window   time.Duration // time window
	cleanInt time.Duration // cleanup interval
}

type clientInfo struct {
	count    int
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter with the specified rate per window
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		clients:  make(map[string]*clientInfo),
		rate:     rate,
		window:   window,
		cleanInt: window * 2,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanInt)
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.window)
		for ip, info := range rl.clients {
			if info.lastSeen.Before(cutoff) {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow returns true if the client can make a request
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	info, exists := rl.clients[ip]
	now := time.Now()

	if !exists || now.Sub(info.lastSeen) > rl.window {
		rl.clients[ip] = &clientInfo{count: 1, lastSeen: now}
		return true
	}

	if info.count >= rl.rate {
		return false
	}

	info.count++
	info.lastSeen = now
	return true
}

// RateLimit middleware wraps handlers with rate limiting
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts client IP from request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (trusted proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Use the first IP in the chain
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}

// SecurityHeaders adds common security headers
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		// XSS protection (legacy browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Don't expose server information
		w.Header().Del("Server")
		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

// LoginRateLimiter is stricter rate limiting for login attempts (5 per minute)
var LoginRateLimiter = NewRateLimiter(5, time.Minute)

// RateLimitLogin wraps login handlers with strict rate limiting
func RateLimitLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		if !LoginRateLimiter.Allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"too many login attempts, try again later"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
