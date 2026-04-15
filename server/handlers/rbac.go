package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type Permission struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	ConnID int64  `json:"conn_id"` // -1 = all connections
	Level  string `json:"level"`   // readonly | readwrite | admin
}

type PermissionView struct {
	Permission
	Username string `json:"username"`
	ConnName string `json:"conn_name"`
}

// isAuthEnabled checks if authentication is set up (any users exist)
func isAuthEnabled() bool {
	var count int
	appdb.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0
}

// requireAdmin returns true if the request is from an admin user
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	role := r.Header.Get("X-User-Role")
	if role != "admin" {
		// If auth is not enabled, allow access
		if !isAuthEnabled() {
			return true
		}
		http.Error(w, `{"error":"admin access required"}`, http.StatusForbidden)
		return false
	}
	return true
}

func ListPermissions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`
			SELECT p.id, p.user_id, p.conn_id, p.level,
			       COALESCE(u.username,''), COALESCE(c.name,'All connections')
			FROM permissions p
			LEFT JOIN users u ON u.id = p.user_id
			LEFT JOIN connections c ON c.id = p.conn_id
			ORDER BY p.user_id, p.conn_id`)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []PermissionView
		for rows.Next() {
			var p PermissionView
			rows.Scan(&p.ID, &p.UserID, &p.ConnID, &p.Level, &p.Username, &p.ConnName)
			list = append(list, p)
		}
		if list == nil {
			list = []PermissionView{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func UpsertPermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var p Permission
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.UserID == 0 {
			http.Error(w, `{"error":"user_id and level required"}`, http.StatusBadRequest)
			return
		}
		// Validate level
		if p.Level != "readonly" && p.Level != "readwrite" && p.Level != "admin" {
			p.Level = "readonly"
		}
		_, err := appdb.DB.Exec(
			`INSERT INTO permissions (user_id, conn_id, level) VALUES (?,?,?)
			 ON CONFLICT(user_id, conn_id) DO UPDATE SET level=excluded.level`,
			p.UserID, p.ConnID, p.Level,
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func DeletePermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		// Validate ID is numeric
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		appdb.DB.Exec(`DELETE FROM permissions WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetUserLevel returns the effective permission level for a user on a connection.
// Order: specific conn > all-connections (-1) > default (readwrite for admin role, readonly otherwise)
func GetUserLevel(userID, connID int64, userRole string) string {
	if userRole == "admin" {
		return "admin"
	}
	var level string
	// Check specific connection
	if err := appdb.DB.QueryRow(
		`SELECT level FROM permissions WHERE user_id=? AND conn_id=? LIMIT 1`, userID, connID,
	).Scan(&level); err == nil {
		return level
	}
	// Check wildcard
	if err := appdb.DB.QueryRow(
		`SELECT level FROM permissions WHERE user_id=? AND conn_id=-1 LIMIT 1`, userID,
	).Scan(&level); err == nil {
		return level
	}
	// Default: if no permissions set, grant readwrite for usability
	// In strict mode, this could be "readonly" or denied
	return "readwrite"
}

// CheckWritePermission returns true if the request can perform write ops on connID.
func CheckWritePermission(r *http.Request, connID int64) bool {
	// If auth is not enabled (no users), allow all operations
	if !isAuthEnabled() {
		return true
	}

	role := r.Header.Get("X-User-Role")
	if role == "admin" {
		return true
	}

	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		// No auth header = deny by default when auth is enabled
		return false
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return false
	}

	level := GetUserLevel(userID, connID, role)
	return level == "readwrite" || level == "admin"
}

// CheckReadPermission returns true if the request can read from connID.
func CheckReadPermission(r *http.Request, connID int64) bool {
	// If auth is not enabled (no users), allow all operations
	if !isAuthEnabled() {
		return true
	}

	role := r.Header.Get("X-User-Role")
	if role == "admin" {
		return true
	}

	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		// No auth header = deny by default when auth is enabled
		return false
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return false
	}

	level := GetUserLevel(userID, connID, role)
	return level == "readonly" || level == "readwrite" || level == "admin"
}
