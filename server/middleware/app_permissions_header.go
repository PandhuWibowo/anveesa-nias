package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/anveesa/nias/db"
)

func authEnabledForPermissions() bool {
	var count int
	db.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0
}

func RequireAnyAppPermissionHeader(perms ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authEnabledForPermissions() {
				next(w, r)
				return
			}

			role := r.Header.Get("X-User-Role")
			if role == "admin" {
				next(w, r)
				return
			}

			userIDStr := r.Header.Get("X-User-ID")
			if userIDStr == "" {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			userID, err := strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			for _, perm := range perms {
				if db.HasUserAppPermission(userID, perm) {
					next(w, r)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
		}
	}
}

