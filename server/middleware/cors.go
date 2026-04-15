package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a middleware that handles Cross-Origin Resource Sharing.
// allowedOrigins can be a comma-separated list of origins or "*" for development only.
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	// Parse allowed origins into a map for O(1) lookup
	originSet := make(map[string]bool)
	allowAll := false

	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin == "*" {
			allowAll = true
		} else if origin != "" {
			originSet[origin] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestOrigin := r.Header.Get("Origin")

			// Determine if origin is allowed
			var allowedOrigin string
			if allowAll {
				// In development mode with "*", reflect the requesting origin
				// WARNING: Don't use "*" in production with credentials
				if requestOrigin != "" {
					allowedOrigin = requestOrigin
				} else {
					allowedOrigin = "*"
				}
			} else if originSet[requestOrigin] {
				allowedOrigin = requestOrigin
			}

			// Only set CORS headers if origin is allowed
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
				w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24 hours

				// Only allow credentials for specific origins, not wildcard
				if allowedOrigin != "*" {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				// Prevent MIME type sniffing
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				if allowedOrigin != "" {
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusForbidden)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
