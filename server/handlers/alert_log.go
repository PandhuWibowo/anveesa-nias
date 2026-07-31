package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type AlertLogEntry struct {
	ID          int64  `json:"id"`
	Channel     string `json:"channel"`
	TargetName  string `json:"target_name"`
	Status      string `json:"status"`
	ErrorMsg    string `json:"error_msg"`
	TriggeredBy string `json:"triggered_by"`
	CreatedAt   string `json:"created_at"`
}

// LogAlertDelivery records an alert delivery attempt. Called internally after
// each send attempt (test or real trigger).
func LogAlertDelivery(channel, targetName, status, errMsg, triggeredBy string) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	appdb.DB.Exec(appdb.ConvertQuery(`
		INSERT INTO alert_log (channel, target_name, status, error_msg, triggered_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`), channel, targetName, status, errMsg, triggeredBy, now)
}

// ListAlertLog returns recent alert log entries.
// GET /api/alerts/log?limit=100&channel=telegram&status=error
func ListAlertLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		limit := 200
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		channel := strings.TrimSpace(r.URL.Query().Get("channel"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))

		query := `SELECT id, channel, target_name, status, error_msg, triggered_by, created_at FROM alert_log`
		args := []any{}
		conditions := []string{}

		if channel != "" {
			conditions = append(conditions, "channel = ?")
			args = append(args, channel)
		}
		if status != "" {
			conditions = append(conditions, "status = ?")
			args = append(args, status)
		}
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		query += " ORDER BY created_at DESC LIMIT ?"
		args = append(args, limit)

		rows, err := appdb.DB.Query(appdb.ConvertQuery(query), args...)
		if err != nil {
			http.Error(w, jsonError("failed to query alert log"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		entries := make([]AlertLogEntry, 0)
		for rows.Next() {
			var e AlertLogEntry
			if err := rows.Scan(&e.ID, &e.Channel, &e.TargetName, &e.Status, &e.ErrorMsg, &e.TriggeredBy, &e.CreatedAt); err != nil {
				continue
			}
			entries = append(entries, e)
		}
		json.NewEncoder(w).Encode(entries)
	}
}

// ClearAlertLog deletes all alert log entries.
// DELETE /api/alerts/log
func ClearAlertLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := appdb.DB.Exec(`DELETE FROM alert_log`); err != nil {
			http.Error(w, jsonError("failed to clear alert log"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AlertLogStats returns counts grouped by channel and status.
// GET /api/alerts/log/stats
func AlertLogStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := appdb.DB.Query(`
			SELECT channel, status, COUNT(*) as count
			FROM alert_log
			GROUP BY channel, status
			ORDER BY channel, status
		`)
		if err != nil {
			http.Error(w, jsonError("failed to query stats"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type stat struct {
			Channel string `json:"channel"`
			Status  string `json:"status"`
			Count   int    `json:"count"`
		}
		stats := make([]stat, 0)
		for rows.Next() {
			var s stat
			if err := rows.Scan(&s.Channel, &s.Status, &s.Count); err != nil {
				continue
			}
			stats = append(stats, s)
		}
		json.NewEncoder(w).Encode(stats)
	}
}
