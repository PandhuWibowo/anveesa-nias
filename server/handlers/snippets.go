package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type Snippet struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	Tags        string `json:"tags"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func ListSnippets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query().Get("q")
		query := `SELECT id, name, COALESCE(description,''), sql, COALESCE(tags,''), created_at, updated_at FROM snippets`
		args := []interface{}{}
		if q != "" {
			query += ` WHERE name LIKE ? OR description LIKE ? OR tags LIKE ?`
			pct := "%" + q + "%"
			args = append(args, pct, pct, pct)
		}
		query += ` ORDER BY updated_at DESC`

		rows, err := appdb.DB.Query(query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []Snippet
		for rows.Next() {
			var s Snippet
			rows.Scan(&s.ID, &s.Name, &s.Description, &s.SQL, &s.Tags, &s.CreatedAt, &s.UpdatedAt)
			list = append(list, s)
		}
		if list == nil {
			list = []Snippet{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateSnippet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var s Snippet
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil || strings.TrimSpace(s.Name) == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		res, err := appdb.DB.Exec(
			`INSERT INTO snippets (name, description, sql, tags, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			s.Name, s.Description, s.SQL, s.Tags, now, now,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		s.ID, _ = res.LastInsertId()
		s.CreatedAt = now
		s.UpdatedAt = now
		json.NewEncoder(w).Encode(s)
	}
}

func UpdateSnippet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		var s Snippet
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		appdb.DB.Exec(
			`UPDATE snippets SET name=?, description=?, sql=?, tags=?, updated_at=? WHERE id=?`,
			s.Name, s.Description, s.SQL, s.Tags, now, id,
		)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func DeleteSnippet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		appdb.DB.Exec(`DELETE FROM snippets WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
