package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/anveesa/nias/db"
)

func EnforceMFASetup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || isMFAAllowedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" || !mfaPolicyEnforced() {
			next.ServeHTTP(w, r)
			return
		}

		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil || userHasMFA(userID) || userExemptFromMFA(userID) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":              "MFA setup required before using the application",
			"mfa_required_setup": true,
		})
	})
}

func isMFAAllowedPath(path string) bool {
	switch path {
	case "/api/auth/setup", "/api/auth/login", "/api/auth/logout", "/api/auth/me", "/api/auth/2fa/status", "/api/auth/2fa/setup", "/api/auth/2fa/enable",
		// Lets an admin who hasn't set up MFA yet still turn off org-wide
		// enforcement themselves, rather than being permanently locked out
		// by their own policy. Still gated by requireAny(PermUsersManage)
		// at the route level (main.go) — this only removes the MFA-wall
		// check, not the permission check, so non-admins gain nothing.
		"/api/auth/mfa-policy":
		return true
	default:
		return false
	}
}

func mfaPolicyEnforced() bool {
	var value string
	err := db.DB.QueryRow(db.ConvertQuery(`SELECT value FROM settings WHERE key = ?`), "security.mfa_enforced").Scan(&value)
	return err == nil && value == "true"
}

func userHasMFA(userID int64) bool {
	var enabled int
	err := db.DB.QueryRow(db.ConvertQuery(`SELECT COALESCE(totp_enabled, 0) FROM users WHERE id = ?`), userID).Scan(&enabled)
	return err == nil && enabled == 1
}

func userExemptFromMFA(userID int64) bool {
	var exempt int
	err := db.DB.QueryRow(db.ConvertQuery(`SELECT COALESCE(mfa_exempt, 0) FROM users WHERE id = ?`), userID).Scan(&exempt)
	return err == nil && exempt == 1
}
