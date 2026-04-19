package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anveesa/nias/db"
)

// db.ConvertQuery converts SQLite ? placeholders to PostgreSQL $1, $2, ... if needed

// ── Roles ──

// ListRoles returns all roles
func ListRoles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.DB.Query(`
			SELECT r.id, r.name, r.description, r.permissions, r.is_system, r.is_active, r.created_at, r.updated_at,
			       (SELECT COUNT(*) FROM users WHERE role_id = r.id) AS user_count
			FROM roles r
			ORDER BY r.name
		`)
		if err != nil {
			http.Error(w, "failed to list roles", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var roles []Role
		for rows.Next() {
			var r Role
			var isSystem, isActive int
			if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Permissions, &isSystem, &isActive, &r.CreatedAt, &r.UpdatedAt, &r.UserCount); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r.IsSystem = isSystem == 1
			r.IsActive = isActive == 1
			roles = append(roles, r)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(roles)
	}
}

// GetRole returns a single role by ID
func GetRole() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/roles/"), "/")
		id, _ := strconv.ParseInt(parts[0], 10, 64)

		var role Role
		var isSystem, isActive int
		err := db.DB.QueryRow(`
			SELECT r.id, r.name, r.description, r.permissions, r.is_system, r.is_active, r.created_at, r.updated_at,
			       (SELECT COUNT(*) FROM users WHERE role_id = r.id) AS user_count
			FROM roles r
			WHERE r.id = ?
		`, id).Scan(&role.ID, &role.Name, &role.Description, &role.Permissions, &isSystem, &isActive, &role.CreatedAt, &role.UpdatedAt, &role.UserCount)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "role not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to get role", http.StatusInternalServerError)
			return
		}
		role.IsSystem = isSystem == 1
		role.IsActive = isActive == 1

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(role)
	}
}

// CreateRole creates a new role
func CreateRole() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateRoleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		permsJSON := AppPermsToJSON(req.Permissions)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")

		var id int64
		if db.IsPostgreSQL() || db.IsMySQL() {
			// Use RETURNING for PostgreSQL/MySQL
			err := db.DB.QueryRow(`
				INSERT INTO roles (name, description, permissions, is_system, is_active, created_at, updated_at)
				VALUES ($1, $2, $3, 0, 1, $4, $5) RETURNING id
			`, req.Name, req.Description, permsJSON, now, now).Scan(&id)
			if err != nil {
				http.Error(w, "failed to create role", http.StatusInternalServerError)
				return
			}
		} else {
			// Use LastInsertId for SQLite
			result, err := db.DB.Exec(`
				INSERT INTO roles (name, description, permissions, is_system, is_active, created_at, updated_at)
				VALUES (?, ?, ?, 0, 1, ?, ?)
			`, req.Name, req.Description, permsJSON, now, now)
			if err != nil {
				http.Error(w, "failed to create role", http.StatusInternalServerError)
				return
			}
			id, _ = result.LastInsertId()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

// UpdateRole updates an existing role  
func UpdateRole() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/roles/"), "/")
		id, _ := strconv.ParseInt(parts[0], 10, 64)

		// Check if system role - allow editing permissions but not name
		var isSystem int
		var currentName string
		db.DB.QueryRow(db.ConvertQuery(`SELECT is_system, name FROM roles WHERE id = ?`), id).Scan(&isSystem, &currentName)

		var req CreateRoleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// For system roles, keep the original name
		if isSystem == 1 {
			req.Name = currentName
		}

		permsJSON := AppPermsToJSON(req.Permissions)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")

		_, err := db.DB.Exec(db.ConvertQuery(`
			UPDATE roles SET name = ?, description = ?, permissions = ?, updated_at = ?
			WHERE id = ?
		`), req.Name, req.Description, permsJSON, now, id)
		if err != nil {
			http.Error(w, "failed to update role", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// DeleteRole deletes a role
func DeleteRole() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/roles/"), "/")
		id, _ := strconv.ParseInt(parts[0], 10, 64)

		// Check if system role
		var isSystem int
		db.DB.QueryRow(db.ConvertQuery(`SELECT is_system FROM roles WHERE id = ?`), id).Scan(&isSystem)
		if isSystem == 1 {
			http.Error(w, "cannot delete system role", http.StatusForbidden)
			return
		}

		// Check if role has users
		var count int
		db.DB.QueryRow(db.ConvertQuery(`SELECT COUNT(*) FROM users WHERE role_id = ?`), id).Scan(&count)
		if count > 0 {
			http.Error(w, "cannot delete role with assigned users", http.StatusConflict)
			return
		}

		_, err := db.DB.Exec(db.ConvertQuery(`DELETE FROM roles WHERE id = ?`), id)
		if err != nil {
			http.Error(w, "failed to delete role", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// ListPermissions returns all available application permissions
func ListAppPermissions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AllAppPermissions)
	}
}

// GetMyPermissions returns the effective application permissions for the current user.
func GetMyPermissions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID, err := strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
		if err != nil || userID == 0 {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		perms, err := db.GetUserAppPermissions(userID)
		if err != nil {
			http.Error(w, "failed to load permissions", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"permissions": perms,
			"role":        r.Header.Get("X-User-Role"),
		})
	}
}

// ── User Connection Assignments ──

// GetUserConnections returns all connection assignments for a user
func GetUserConnections() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/users/"), "/")
		userID, _ := strconv.ParseInt(parts[0], 10, 64)

		role, err := db.GetUserRole(userID)
		if err != nil {
			http.Error(w, "failed to get user role", http.StatusInternalServerError)
			return
		}

		assignments, err := db.GetUserConnectionAssignments(userID, role)
		if err != nil {
			http.Error(w, "failed to get assignments", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(assignments)
	}
}

// SetUserConnections sets direct connection assignments for a user
func SetUserConnections() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/users/"), "/")
		userID, _ := strconv.ParseInt(parts[0], 10, 64)

		var req struct {
			ConnectionIDs         []int64                `json:"connection_ids"`
			ConnectionPermissions []ConnectionPermission `json:"connection_permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		permsMap := make(map[int64][]db.DbPerm)
		for _, cp := range req.ConnectionPermissions {
			dbPerms := make([]db.DbPerm, len(cp.Permissions))
			for i, p := range cp.Permissions {
				dbPerms[i] = db.DbPerm(p)
			}
			permsMap[cp.ConnID] = dbPerms
		}

		if err := db.SetUserDirectConnections(userID, req.ConnectionIDs, permsMap); err != nil {
			http.Error(w, "failed to set connections", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
