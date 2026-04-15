package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type SavedQuery struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	ConnID      *int64  `json:"conn_id"`
	SQL         string  `json:"sql"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func ListSavedQueries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		userRole := r.Header.Get("X-User-Role")
		userIDStr := r.Header.Get("X-User-ID")
		
		var rows *sql.Rows
		var err error
		
		// If auth is not enabled, show all queries
		if !isAuthEnabled() {
			rows, err = appdb.DB.Query(
				`SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
				 FROM saved_queries ORDER BY updated_at DESC`,
			)
		} else if userRole == "admin" {
			// Admin sees all queries
			rows, err = appdb.DB.Query(
				`SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
				 FROM saved_queries ORDER BY updated_at DESC`,
			)
		} else {
			// Regular user only sees their own queries
			userID, _ := strconv.ParseInt(userIDStr, 10, 64)
			rows, err = appdb.DB.Query(
				`SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
				 FROM saved_queries WHERE user_id = ?
				 ORDER BY updated_at DESC`,
				userID,
			)
		}
		
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []SavedQuery
		for rows.Next() {
			var q SavedQuery
			rows.Scan(&q.ID, &q.Name, &q.ConnID, &q.SQL, &q.Description, &q.CreatedAt, &q.UpdatedAt)
			list = append(list, q)
		}
		if list == nil {
			list = []SavedQuery{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateSavedQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			Name        string `json:"name"`
			ConnID      *int64 `json:"conn_id"`
			SQL         string `json:"sql"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.SQL == "" {
			http.Error(w, `{"error":"name and sql required"}`, http.StatusBadRequest)
			return
		}
		
		// Get user ID from context
		var userID *int64
		if userIDStr := r.Header.Get("X-User-ID"); userIDStr != "" {
			if uid, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
				userID = &uid
			}
		}
		
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := appdb.DB.Exec(
			`INSERT INTO saved_queries (name, conn_id, sql, description, user_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			body.Name, body.ConnID, body.SQL, body.Description, userID, now, now,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	}
}

func UpdateSavedQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idStr := strings.TrimPrefix(r.URL.Path, "/api/saved-queries/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		
		userRole := r.Header.Get("X-User-Role")
		userIDStr := r.Header.Get("X-User-ID")
		
		// Check ownership if not admin and auth is enabled
		if isAuthEnabled() && userRole != "admin" && userIDStr != "" {
			userID, _ := strconv.ParseInt(userIDStr, 10, 64)
			var ownerID sql.NullInt64
			err := appdb.DB.QueryRow(`SELECT user_id FROM saved_queries WHERE id = ?`, id).Scan(&ownerID)
			if err != nil || (ownerID.Valid && ownerID.Int64 != userID) {
				http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
				return
			}
		}
		
		var body struct {
			Name        string `json:"name"`
			SQL         string `json:"sql"`
			Description string `json:"description"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		now := time.Now().Format("2006-01-02 15:04:05")
		appdb.DB.Exec(
			`UPDATE saved_queries SET name=?, sql=?, description=?, updated_at=? WHERE id=?`,
			body.Name, body.SQL, body.Description, now, id,
		)
		w.WriteHeader(http.StatusNoContent)
	}
}

func DeleteSavedQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/saved-queries/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		
		userRole := r.Header.Get("X-User-Role")
		userIDStr := r.Header.Get("X-User-ID")
		
		// Check ownership if not admin and auth is enabled
		if isAuthEnabled() && userRole != "admin" && userIDStr != "" {
			userID, _ := strconv.ParseInt(userIDStr, 10, 64)
			var ownerID sql.NullInt64
			err := appdb.DB.QueryRow(`SELECT user_id FROM saved_queries WHERE id = ?`, id).Scan(&ownerID)
			if err != nil || (ownerID.Valid && ownerID.Int64 != userID) {
				http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
				return
			}
		}
		
		appdb.DB.Exec(`DELETE FROM saved_queries WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
