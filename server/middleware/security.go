package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anveesa/nias/cache"
)

// RateLimiter provides simple rate limiting per IP
type RateLimiter struct {
	store      cache.Store
	fallback   *memoryRateLimiter
	rate       int
	window     time.Duration
	keyPrefix  string
	lastWarnMu sync.Mutex
	lastWarnAt time.Time
}

type clientInfo struct {
	count    int
	lastSeen time.Time
}

type memoryRateLimiter struct {
	mu      sync.RWMutex
	clients map[string]*clientInfo
	window  time.Duration
}

// NewRateLimiter creates a rate limiter with the specified rate per window
func NewRateLimiter(rate int, window time.Duration, store cache.Store, keyPrefix string) *RateLimiter {
	if rate <= 0 {
		log.Printf("Invalid rate limit value %d, using default 100", rate)
		rate = 100
	}
	if window <= 0 {
		log.Printf("Invalid rate limit window %s, using default 1m", window)
		window = time.Minute
	}
	if store == nil {
		store = cache.Default()
	}
	return &RateLimiter{
		store:     store,
		fallback:  newMemoryRateLimiter(window),
		rate:      rate,
		window:    window,
		keyPrefix: keyPrefix,
	}
}

func newMemoryRateLimiter(window time.Duration) *memoryRateLimiter {
	return &memoryRateLimiter{
		clients: make(map[string]*clientInfo),
		window:  window,
	}
}

func (rl *memoryRateLimiter) allow(ip string, rate int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for candidate, info := range rl.clients {
		if info.lastSeen.Before(cutoff) {
			delete(rl.clients, candidate)
		}
	}
	now := time.Now()
	info, exists := rl.clients[ip]

	if !exists || now.Sub(info.lastSeen) > rl.window {
		rl.clients[ip] = &clientInfo{count: 1, lastSeen: now}
		return true
	}

	if info.count >= rate {
		return false
	}

	info.count++
	info.lastSeen = now
	return true
}

// Allow returns true if the client can make a request
func (rl *RateLimiter) Allow(ip string) bool {
	if rl.store != nil && rl.store.BackendName() == "redis" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		bucket := time.Now().UTC().Unix() / maxInt64(1, int64(rl.window/time.Second))
		key := fmt.Sprintf("ratelimit:%s:%d:%s", rl.keyPrefix, bucket, ip)
		count, err := rl.store.Increment(ctx, key, rl.window+5*time.Second)
		if err == nil {
			return count <= int64(rl.rate)
		}
		rl.warnRedisFallback(err)
	}
	return rl.fallback.allow(ip, rl.rate)
}

func (rl *RateLimiter) RetryAfter() string {
	seconds := maxInt64(1, int64(rl.window/time.Second))
	return strconv.FormatInt(seconds, 10)
}

func (rl *RateLimiter) warnRedisFallback(err error) {
	rl.lastWarnMu.Lock()
	defer rl.lastWarnMu.Unlock()
	now := time.Now()
	if now.Sub(rl.lastWarnAt) < time.Minute {
		return
	}
	rl.lastWarnAt = now
	log.Printf("WARNING: Redis rate limiter failed, falling back to in-memory limiter: %v", err)
}

// RateLimit middleware wraps handlers with rate limiting
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Retry-After", rl.RetryAfter())
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
		// Prevent clickjacking for the app shell, but allow explicit public embed routes.
		if !strings.HasPrefix(r.URL.Path, "/embed/") {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		}
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
var LoginRateLimiter = NewRateLimiter(5, time.Minute, nil, "login")

func ConfigureLoginRateLimiter(rl *RateLimiter) {
	if rl != nil {
		LoginRateLimiter = rl
	}
}

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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
