package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/anveesa/nias/db"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	SessionID string `json:"session_id"`
	jwt.RegisteredClaims
}

func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			
			// Extract claims and set as headers for downstream handlers
			if claims, ok := token.Claims.(*Claims); ok {
				active, err := db.IsUserActive(claims.UserID)
				if err != nil || !active {
					http.Error(w, `{"error":"account is locked"}`, http.StatusUnauthorized)
					return
				}
				if claims.SessionID == "" {
					http.Error(w, `{"error":"invalid session"}`, http.StatusUnauthorized)
					return
				}
				sessionActive, err := db.IsSessionActive(claims.SessionID)
				if err != nil || !sessionActive {
					http.Error(w, `{"error":"session revoked"}`, http.StatusUnauthorized)
					return
				}
				_ = db.TouchSession(claims.SessionID)
				r.Header.Set("X-User-ID", strconv.FormatInt(claims.UserID, 10))
				r.Header.Set("X-Username", claims.Username)
				r.Header.Set("X-User-Role", claims.Role)
			}
			
			next.ServeHTTP(w, r)
		})
	}
}
