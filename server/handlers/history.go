package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type HistoryEntry struct {
	ID         int64   `json:"id"`
	ConnID     int64   `json:"conn_id"`
	SQL        string  `json:"sql"`
	DurationMs int64   `json:"duration_ms"`
	RowCount   int     `json:"row_count"`
	Error      *string `json:"error,omitempty"`
	ExecutedAt string  `json:"executed_at"`
}

func GetHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		rows, err := appdb.DB.Query(`
			SELECT id, conn_id, sql, duration_ms, row_count, error, executed_at
			FROM query_history WHERE conn_id = ?
			ORDER BY executed_at DESC LIMIT 200
		`, connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var history []HistoryEntry
		for rows.Next() {
			var h HistoryEntry
			rows.Scan(&h.ID, &h.ConnID, &h.SQL, &h.DurationMs, &h.RowCount, &h.Error, &h.ExecutedAt)
			history = append(history, h)
		}
		if history == nil {
			history = []HistoryEntry{}
		}
		json.NewEncoder(w).Encode(history)
	}
}

func SaveHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		var body struct {
			SQL        string  `json:"sql"`
			DurationMs int64   `json:"duration_ms"`
			RowCount   int     `json:"row_count"`
			Error      *string `json:"error"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}

		res, err := appdb.DB.Exec(
			`INSERT INTO query_history (conn_id, sql, duration_ms, row_count, error) VALUES (?, ?, ?, ?, ?)`,
			connID, body.SQL, body.DurationMs, body.RowCount, body.Error,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		// Keep only last 500 per connection
		appdb.DB.Exec(`
			DELETE FROM query_history WHERE conn_id = ? AND id NOT IN (
				SELECT id FROM query_history WHERE conn_id = ? ORDER BY executed_at DESC LIMIT 500
			)
		`, connID, connID)

		id, _ := res.LastInsertId()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	}
}

func ClearHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM query_history WHERE conn_id = ?`), connID)
		w.WriteHeader(http.StatusNoContent)
	}
}
