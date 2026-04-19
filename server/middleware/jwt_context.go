package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/anveesa/nias/db"
	"github.com/golang-jwt/jwt/v5"
)

// InjectUserContext extracts JWT claims and injects user info into request headers
// This allows handlers to access user information via headers
func InjectUserContext(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for Authorization header
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				tokenStr := strings.TrimPrefix(auth, "Bearer ")
				token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
					return []byte(jwtSecret), nil
				})
				
				// If token is valid, extract claims and set headers
				if err == nil && token.Valid {
					if claims, ok := token.Claims.(*Claims); ok {
						active, activeErr := db.IsUserActive(claims.UserID)
						sessionActive, sessionErr := db.IsSessionActive(claims.SessionID)
						if activeErr == nil && active && claims.SessionID != "" && sessionErr == nil && sessionActive {
							_ = db.TouchSession(claims.SessionID)
							r.Header.Set("X-User-ID", strconv.FormatInt(claims.UserID, 10))
							r.Header.Set("X-Username", claims.Username)
							r.Header.Set("X-User-Role", claims.Role)
						}
					}
				}
			}
			
			next.ServeHTTP(w, r)
		})
	}
}
