package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type ConnectionFolder struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ParentID   *int64 `json:"parent_id"`
	OwnerID    int64  `json:"owner_id"`
	Visibility string `json:"visibility"` // "private" | "shared"
	IsActive   bool   `json:"is_active"`
	Color      string `json:"color"`
	SortOrder  int    `json:"sort_order"`
	CreatedAt  string `json:"created_at"`
}

func ListFolders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")

		var rows *sql.Rows
		var err error

		// Admin or no auth enabled: see all folders
		if userRole == "admin" || !isAuthEnabled() {
			rows, err = appdb.DB.Query(
				`SELECT id, name, parent_id, owner_id, visibility, COALESCE(is_active,1), color, COALESCE(sort_order,0), created_at
				 FROM connection_folders ORDER BY sort_order, name`,
			)
		} else {
			// Regular user: see shared folders, owned folders, or folders they're members of
			var userID int64
			if userIDStr != "" {
				userID, _ = strconv.ParseInt(userIDStr, 10, 64)
			}
			rows, err = appdb.DB.Query(
				`SELECT DISTINCT cf.id, cf.name, cf.parent_id, cf.owner_id, cf.visibility, COALESCE(cf.is_active,1), cf.color, COALESCE(cf.sort_order,0), cf.created_at
				 FROM connection_folders cf
				 LEFT JOIN folder_members fm ON cf.id = fm.folder_id AND fm.user_id = ?
				 WHERE cf.visibility='shared' OR cf.owner_id=? OR fm.folder_id IS NOT NULL
				 ORDER BY cf.sort_order, cf.name`,
				userID, userID,
			)
		}
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var folders []ConnectionFolder
		for rows.Next() {
			var f ConnectionFolder
			var isActive int
			rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.Visibility, &isActive, &f.Color, &f.SortOrder, &f.CreatedAt)
			f.IsActive = isActive == 1
			folders = append(folders, f)
		}
		if folders == nil {
			folders = []ConnectionFolder{}
		}
		json.NewEncoder(w).Encode(folders)
	}
}

func CreateFolder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var f ConnectionFolder
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil || strings.TrimSpace(f.Name) == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		if f.Visibility == "" {
			f.Visibility = "private"
		}
		if f.Color == "" {
			f.Color = "#4f9cf9"
		}
	if uid, err := strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64); err == nil {
		f.OwnerID = uid
	}

	// Compute next sort_order in a separate query to avoid self-referencing subquery issues
	var nextOrder int
	appdb.DB.QueryRow(`SELECT COALESCE(MAX(sort_order),0)+1 FROM connection_folders`).Scan(&nextOrder)

	res, err := appdb.DB.Exec(
		`INSERT INTO connection_folders (name, parent_id, owner_id, visibility, color, sort_order)
		 VALUES (?,?,?,?,?,?)`,
		f.Name, f.ParentID, f.OwnerID, f.Visibility, f.Color, nextOrder,
	)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
		return
	}
		f.ID, _ = res.LastInsertId()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(f)
	}
}

func UpdateFolder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]

		// Check ownership or admin
		if !canModifyFolder(r, id) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var f ConnectionFolder
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		_, err := appdb.DB.Exec(
			`UPDATE connection_folders SET name=?, parent_id=?, visibility=?, color=?, sort_order=? WHERE id=?`,
			f.Name, f.ParentID, f.Visibility, f.Color, f.SortOrder, id,
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func DeleteFolder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]

		// Check ownership or admin
		if !canModifyFolder(r, id) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET folder_id=NULL WHERE folder_id=?`), id)
		appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connection_folders SET parent_id=NULL WHERE parent_id=?`), id)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM connection_folders WHERE id=?`), id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// canModifyFolder checks if the current user can modify a folder
func canModifyFolder(r *http.Request, folderID string) bool {
	// No auth or admin: allowed
	if !isAuthEnabled() {
		return true
	}
	userRole := r.Header.Get("X-User-Role")
	if userRole == "admin" {
		return true
	}

	// Check if user owns this folder
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return false
	}
	userID, _ := strconv.ParseInt(userIDStr, 10, 64)

	var ownerID int64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT owner_id FROM connection_folders WHERE id=?`), folderID).Scan(&ownerID)
	if err != nil {
		return false
	}
	return ownerID == userID
}

func MoveConnectionToFolder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.SplitN(path, "/", 3)
		if len(parts) < 1 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID := parts[0]
		connIDInt, _ := strconv.ParseInt(connID, 10, 64)

		// Check if user can modify this connection
		if !canModifyConnection(r, connIDInt) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var req struct {
			FolderID   *int64 `json:"folder_id"`
			Visibility string `json:"visibility"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		// If moving to a folder, check if user has access to that folder
		if req.FolderID != nil {
			if !canAccessFolder(r, *req.FolderID) {
				http.Error(w, `{"error":"cannot move to this folder"}`, http.StatusForbidden)
				return
			}
		}

		if req.Visibility != "" {
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET folder_id=?, visibility=? WHERE id=?`), req.FolderID, req.Visibility, connID)
		} else {
			appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET folder_id=? WHERE id=?`), req.FolderID, connID)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func SetConnectionVisibility() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.SplitN(path, "/", 3)
		connID := parts[0]
		connIDInt, _ := strconv.ParseInt(connID, 10, 64)

		// Check if user can modify this connection
		if !canModifyConnection(r, connIDInt) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var req struct {
			Visibility string `json:"visibility"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Visibility != "private" && req.Visibility != "shared" {
			http.Error(w, `{"error":"visibility must be private or shared"}`, http.StatusBadRequest)
			return
		}
		appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET visibility=? WHERE id=?`), req.Visibility, connID)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// canAccessFolder checks if the current user can access a folder (for moving connections into it)
func canAccessFolder(r *http.Request, folderID int64) bool {
	if !isAuthEnabled() {
		return true
	}
	userRole := r.Header.Get("X-User-Role")
	if userRole == "admin" {
		return true
	}

	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return false
	}
	userID, _ := strconv.ParseInt(userIDStr, 10, 64)

	var visibility string
	var ownerID int64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT visibility, owner_id FROM connection_folders WHERE id=?`), folderID).Scan(&visibility, &ownerID)
	if err != nil {
		return false
	}
	return visibility == "shared" || ownerID == userID
}
