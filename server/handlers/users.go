package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/bcrypt"
)

type UserInfo struct {
	ID                   int64    `json:"id"`
	Username             string   `json:"username"`
	Role                 string   `json:"role"`
	RoleID               int64    `json:"role_id"`
	IsActive             bool     `json:"is_active"`
	Permissions          []string `json:"permissions"`
	EffectivePermissions []string `json:"effective_permissions"`
	MFAEnabled           bool     `json:"mfa_enabled"`
	MFAExempt            bool     `json:"mfa_exempt"`
	CreatedAt            string   `json:"created_at"`
}

func ListUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`
				SELECT u.id, u.username, COALESCE(r.name, u.role) as role,
				       COALESCE(u.role_id, 2) as role_id,
				       COALESCE(u.is_active, 1) as is_active,
				       COALESCE(u.permissions, '[]') as permissions,
				       COALESCE(u.totp_enabled, 0) as mfa_enabled,
				       COALESCE(u.mfa_exempt, 0) as mfa_exempt,
				       u.created_at
				FROM users u
			LEFT JOIN roles r ON r.id = u.role_id
			ORDER BY u.id
		`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var users []UserInfo
		for rows.Next() {
			var u UserInfo
			var isActive int
			var permissions string
			var mfaEnabled int
			var mfaExempt int
			rows.Scan(&u.ID, &u.Username, &u.Role, &u.RoleID, &isActive, &permissions, &mfaEnabled, &mfaExempt, &u.CreatedAt)
			u.IsActive = isActive == 1
			u.MFAEnabled = mfaEnabled == 1
			u.MFAExempt = mfaExempt == 1
			u.Permissions = ParseAppPerms(permissions)
			if effectivePerms, err := appdb.GetUserAppPermissions(u.ID); err == nil {
				u.EffectivePermissions = effectivePerms
			} else {
				u.EffectivePermissions = u.Permissions
			}
			users = append(users, u)
		}
		if users == nil {
			users = []UserInfo{}
		}
		json.NewEncoder(w).Encode(users)
	}
}

// ResetUserMFA clears a user's TOTP enrollment (secret, enabled flag, and
// backup codes) so they can log in without MFA again — the admin-side
// counterpart to self-service Disable2FA, for account-recovery when a user
// has lost their authenticator device and can't disable it themselves. It
// also grants an MFA-enforcement exemption, so a reset user isn't
// immediately walled off again by an org-wide MFA policy before they've had
// a chance to re-enroll — the exemption clears automatically once they set
// MFA up again (see Enable2FA), or an admin can revoke it manually.
func ResetUserMFA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idStr := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"), "/reset-mfa")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE users SET totp_enabled = 0, totp_secret = NULL, backup_codes = NULL, mfa_exempt = 1 WHERE id = ?`), id); err != nil {
			http.Error(w, jsonError("failed to reset MFA"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "MFA reset — user can log in without it"})
	}
}

func UpdateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var body struct {
			Role        string    `json:"role"`
			RoleID      *int64    `json:"role_id"`
			IsActive    *bool     `json:"is_active"`
			Password    string    `json:"password"`
			Connections []int64   `json:"connections"`
			Permissions *[]string `json:"permissions"`
			MFAExempt   *bool     `json:"mfa_exempt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Update role by role_id (new RBAC system)
		if body.RoleID != nil {
			var roleName string
			err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT name FROM roles WHERE id = ?`), *body.RoleID).Scan(&roleName)
			if err == nil {
				appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET role = ?, role_id = ? WHERE id = ?`), roleName, *body.RoleID, id)
			}
		} else if body.Role != "" {
			// Update role by name (legacy)
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET role = ? WHERE id = ?`), body.Role, id)
		}

		// Update user-level app permission overrides (ABAC grants)
		if body.Permissions != nil {
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET permissions = ? WHERE id = ?`), AppPermsToJSON(*body.Permissions), id)
		}

		// Update password if provided
		if body.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if err == nil {
				appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET password = ? WHERE id = ?`), string(hash), id)
			}
		}

		if body.IsActive != nil {
			isActive := 0
			if *body.IsActive {
				isActive = 1
			}
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET is_active = ? WHERE id = ?`), isActive, id)
		}

		// Manually grant/revoke an MFA-enforcement exemption, independent of
		// resetting MFA itself (e.g. to permanently exempt a service account,
		// or to revoke an exemption once a reset user should be compliant again).
		if body.MFAExempt != nil {
			exempt := 0
			if *body.MFAExempt {
				exempt = 1
			}
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE users SET mfa_exempt = ? WHERE id = ?`), exempt, id)
		}

		// Update connection assignments
		if body.Connections != nil {
			// Clear existing connections
			appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM user_connections WHERE user_id = ?`), id)

			// Add new connections
			for _, connID := range body.Connections {
				appdb.DB.Exec(appdb.ConvertQuery(`INSERT INTO user_connections (user_id, conn_id) VALUES (?, ?)`), id, connID)
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "user updated"})
	}
}

func DeleteUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM users WHERE id=?`), id)
		w.WriteHeader(http.StatusNoContent)
	}
}
