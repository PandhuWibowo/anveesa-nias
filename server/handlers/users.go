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
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	RoleID    int64  `json:"role_id"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
}

func ListUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`
			SELECT u.id, u.username, COALESCE(r.name, u.role) as role, 
			       COALESCE(u.role_id, 2) as role_id,
			       COALESCE(u.is_active, 1) as is_active,
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
			rows.Scan(&u.ID, &u.Username, &u.Role, &u.RoleID, &isActive, &u.CreatedAt)
			u.IsActive = isActive == 1
			users = append(users, u)
		}
		if users == nil {
			users = []UserInfo{}
		}
		json.NewEncoder(w).Encode(users)
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
			Role     string `json:"role"`
			RoleID   *int64 `json:"role_id"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Update role by role_id (new RBAC system)
		if body.RoleID != nil {
			var roleName string
			err := appdb.DB.QueryRow(`SELECT name FROM roles WHERE id = ?`, *body.RoleID).Scan(&roleName)
			if err == nil {
				appdb.DB.Exec(`UPDATE users SET role = ?, role_id = ? WHERE id = ?`, roleName, *body.RoleID, id)
			}
		} else if body.Role != "" {
			// Update role by name (legacy)
			appdb.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, body.Role, id)
		}

		// Update password if provided
		if body.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if err == nil {
				appdb.DB.Exec(`UPDATE users SET password = ? WHERE id = ?`, string(hash), id)
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
		appdb.DB.Exec(`DELETE FROM users WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
