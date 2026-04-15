package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type AuditEntry struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	ConnID     *int64 `json:"conn_id"`
	ConnName   string `json:"conn_name"`
	SQL        string `json:"sql"`
	DurationMs int64  `json:"duration_ms"`
	RowCount   int64  `json:"row_count"`
	Error      string `json:"error"`
	ExecutedAt string `json:"executed_at"`
}

// WriteAuditLog writes a query execution record to the audit log.
func WriteAuditLog(username string, connID int64, connName, sql string, durationMs, rowCount int64, errMsg string) {
	appdb.DB.Exec(
		`INSERT INTO audit_log (username, conn_id, conn_name, sql, duration_ms, row_count, error, executed_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		username, connID, connName, sql, durationMs, rowCount, errMsg,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	// Prune to last 10000 entries
	appdb.DB.Exec(`DELETE FROM audit_log WHERE id NOT IN (SELECT id FROM audit_log ORDER BY id DESC LIMIT 10000)`)
}

func ListAuditLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		limit := 200
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		filter := r.URL.Query().Get("q")

		query := `SELECT id, username, conn_id, conn_name, sql, duration_ms, row_count, COALESCE(error,''), executed_at
		           FROM audit_log`
		args := []interface{}{}
		if filter != "" {
			query += ` WHERE sql LIKE ? OR username LIKE ? OR conn_name LIKE ?`
			pct := "%" + filter + "%"
			args = append(args, pct, pct, pct)
		}
		query += ` ORDER BY id DESC LIMIT ?`
		args = append(args, limit)

		rows, err := appdb.DB.Query(query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var entries []AuditEntry
		for rows.Next() {
			var e AuditEntry
			rows.Scan(&e.ID, &e.Username, &e.ConnID, &e.ConnName, &e.SQL, &e.DurationMs, &e.RowCount, &e.Error, &e.ExecutedAt)
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []AuditEntry{}
		}
		json.NewEncoder(w).Encode(entries)
	}
}

func ClearAuditLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appdb.DB.Exec(`DELETE FROM audit_log`)
		w.WriteHeader(http.StatusNoContent)
	}
}

// AuditMiddleware wraps ExecuteQuery to log queries.
func AuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept POST .../query
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/query") {
			// We can't easily intercept the response body here without wrapping ResponseWriter.
			// Auditing is done directly in ExecuteQuery handler for accuracy.
		}
		next.ServeHTTP(w, r)
	})
}

// WriteAuditFromRequest is called from ExecuteQuery after execution.
func WriteAuditFromRequest(r *http.Request, connID int64, connName, sql string, durationMs, rowCount int64, errMsg string) {
	username := "anonymous"
	if u := r.Header.Get("X-Username"); u != "" {
		username = u
	}
	WriteAuditLog(username, connID, connName, sql, durationMs, rowCount, errMsg)
}

func GetAuditStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var total, errors int64
		var avgMs float64
		appdb.DB.QueryRow(`SELECT COUNT(*), COUNT(CASE WHEN error != '' THEN 1 END), COALESCE(AVG(duration_ms),0) FROM audit_log`).
			Scan(&total, &errors, &avgMs)
		json.NewEncoder(w).Encode(map[string]any{
			"total": total, "errors": errors, "avg_ms": avgMs,
		})
	}
}
