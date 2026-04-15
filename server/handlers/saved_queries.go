package handlers

import (
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
		rows, err := appdb.DB.Query(
			`SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
			 FROM saved_queries ORDER BY updated_at DESC`,
		)
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
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := appdb.DB.Exec(
			`INSERT INTO saved_queries (name, conn_id, sql, description, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			body.Name, body.ConnID, body.SQL, body.Description, now, now,
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
		appdb.DB.Exec(`DELETE FROM saved_queries WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
