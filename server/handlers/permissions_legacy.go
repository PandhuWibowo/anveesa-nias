package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/anveesa/nias/db"
)

// Legacy permission handlers for backward compatibility with the old permissions table.
// These are kept for backward compatibility but should be migrated to the new RBAC system.

// ListPermissions returns legacy permissions from the old permissions table
func ListPermissions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if admin
		role := r.Header.Get("X-User-Role")
		if role != "admin" && isAuthEnabled() {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		rows, err := db.DB.Query(`
			SELECT p.id, p.user_id, p.conn_id, p.level,
			       COALESCE(u.username,''), COALESCE(c.name,'All connections')
			FROM permissions p
			LEFT JOIN users u ON u.id = p.user_id
			LEFT JOIN connections c ON c.id = p.conn_id
			ORDER BY p.user_id, p.conn_id`)
		if err != nil {
			http.Error(w, "failed to list permissions", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var perms []map[string]interface{}
		for rows.Next() {
			var id, userID, connID int64
			var level, username, connName string
			if err := rows.Scan(&id, &userID, &connID, &level, &username, &connName); err != nil {
				continue
			}
			perms = append(perms, map[string]interface{}{
				"id":        id,
				"user_id":   userID,
				"conn_id":   connID,
				"level":     level,
				"username":  username,
				"conn_name": connName,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(perms)
	}
}

// UpsertPermission creates or updates a legacy permission
func UpsertPermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if admin
		role := r.Header.Get("X-User-Role")
		if role != "admin" && isAuthEnabled() {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		var req struct {
			UserID int64  `json:"user_id"`
			ConnID int64  `json:"conn_id"`
			Level  string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Upsert into legacy permissions table
		_, err := db.DB.Exec(`
			INSERT INTO permissions (user_id, conn_id, level) 
			VALUES (?, ?, ?)
			ON CONFLICT(user_id, conn_id) 
			DO UPDATE SET level = excluded.level
		`, req.UserID, req.ConnID, req.Level)
		if err != nil {
			http.Error(w, "failed to save permission", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// DeletePermission removes a legacy permission
func DeletePermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if admin
		role := r.Header.Get("X-User-Role")
		if role != "admin" && isAuthEnabled() {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		// Validate ID is numeric
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		_, err := db.DB.Exec(`DELETE FROM permissions WHERE id = ?`, id)
		if err != nil {
			http.Error(w, "failed to delete permission", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
