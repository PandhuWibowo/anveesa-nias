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
	EventType  string `json:"event_type"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Details    string `json:"details"`
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
	writeAuditEvent("query_execution", "execute_query", connName, "", username, &connID, connName, sql, durationMs, rowCount, errMsg)
}

func WriteFeatureAccessAudit(username, action, target, details string) {
	writeAuditEvent("feature_access", action, target, details, username, nil, "", "", 0, 0, "")
}

func writeAuditEvent(eventType, action, target, details, username string, connID *int64, connName, sql string, durationMs, rowCount int64, errMsg string) {
	query := appdb.ConvertQuery(`INSERT INTO audit_log (event_type, action, target, details, username, conn_id, conn_name, sql, duration_ms, row_count, error, executed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	
	_, err := appdb.DB.Exec(
		query,
		eventType, action, target, details, username, connID, connName, sql, durationMs, rowCount, errMsg,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		// Log error but don't fail the request
		println("Failed to write audit log:", err.Error())
		return
	}
	
	// Prune to last 10000 entries (in a separate transaction to reduce lock time)
	go func() {
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM audit_log WHERE id NOT IN (SELECT id FROM audit_log ORDER BY id DESC LIMIT 10000)`))
	}()
}

func ListAuditLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userRole := r.Header.Get("X-User-Role")
		username := r.Header.Get("X-Username")

		limit := 200
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		filter := r.URL.Query().Get("q")
		eventType := r.URL.Query().Get("event_type")
		connID := r.URL.Query().Get("conn_id")
		sinceHours := r.URL.Query().Get("since_hours")
		hasError := r.URL.Query().Get("has_error")
		minDurationMs := r.URL.Query().Get("min_duration_ms")

		query := `SELECT id, COALESCE(event_type,'query_execution'), COALESCE(action,''), COALESCE(target,''), COALESCE(details,''), username, conn_id, conn_name, sql, duration_ms, row_count, COALESCE(error,''), executed_at
		           FROM audit_log`
		args := []interface{}{}
		whereClause := []string{}

		// Non-admin users can only see their own audit logs
		if userRole != "admin" && username != "" {
			whereClause = append(whereClause, "username = ?")
			args = append(args, username)
		}

		if eventType != "" && eventType != "all" {
			whereClause = append(whereClause, "event_type = ?")
			args = append(args, eventType)
		}

		if connID != "" && connID != "all" {
			if n, err := strconv.ParseInt(connID, 10, 64); err == nil && n > 0 {
				whereClause = append(whereClause, "conn_id = ?")
				args = append(args, n)
			}
		}

		if sinceHours != "" {
			if n, err := strconv.Atoi(sinceHours); err == nil && n > 0 {
				since := time.Now().Add(-time.Duration(n) * time.Hour).Format("2006-01-02 15:04:05")
				whereClause = append(whereClause, "executed_at >= ?")
				args = append(args, since)
			}
		}

		if hasError == "1" || strings.EqualFold(hasError, "true") {
			whereClause = append(whereClause, "COALESCE(error, '') != ''")
		}

		if minDurationMs != "" {
			if n, err := strconv.ParseInt(minDurationMs, 10, 64); err == nil && n >= 0 {
				whereClause = append(whereClause, "duration_ms >= ?")
				args = append(args, n)
			}
		}

		if filter != "" {
			whereClause = append(whereClause, "(sql LIKE ? OR username LIKE ? OR conn_name LIKE ? OR target LIKE ? OR details LIKE ?)")
			pct := "%" + filter + "%"
			args = append(args, pct, pct, pct, pct, pct)
		}

		if len(whereClause) > 0 {
			query += ` WHERE ` + strings.Join(whereClause, " AND ")
		}

		query += ` ORDER BY id DESC LIMIT ?`
		args = append(args, limit)

		// Convert query for PostgreSQL/MySQL compatibility
		query = appdb.ConvertQuery(query)

		rows, err := appdb.DB.Query(query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var entries []AuditEntry
		for rows.Next() {
			var e AuditEntry
			rows.Scan(&e.ID, &e.EventType, &e.Action, &e.Target, &e.Details, &e.Username, &e.ConnID, &e.ConnName, &e.SQL, &e.DurationMs, &e.RowCount, &e.Error, &e.ExecutedAt)
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []AuditEntry{}
		}
		json.NewEncoder(w).Encode(entries)
	}
}

func LogFeatureAccess() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		username := r.Header.Get("X-Username")
		if username == "" {
			username = "anonymous"
		}
		var body struct {
			Action  string `json:"action"`
			Target  string `json:"target"`
			Details string `json:"details"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(body.Action)
		target := strings.TrimSpace(body.Target)
		if action == "" {
			action = "open_feature"
		}
		if target == "" {
			http.Error(w, jsonError("target is required"), http.StatusBadRequest)
			return
		}
		WriteFeatureAccessAudit(username, action, target, strings.TrimSpace(body.Details))
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		
		userRole := r.Header.Get("X-User-Role")
		username := r.Header.Get("X-Username")
		
		var total, errors int64
		var avgMs float64
		
		// Non-admin users only see their own stats
		if userRole != "admin" && username != "" {
			appdb.DB.QueryRow(appdb.ConvertQuery(`
				SELECT COUNT(*), COUNT(CASE WHEN error != '' THEN 1 END), COALESCE(AVG(duration_ms),0) 
				FROM audit_log 
				WHERE username = ?
			`), username).Scan(&total, &errors, &avgMs)
		} else {
			appdb.DB.QueryRow(`
				SELECT COUNT(*), COUNT(CASE WHEN error != '' THEN 1 END), COALESCE(AVG(duration_ms),0) 
				FROM audit_log
			`).Scan(&total, &errors, &avgMs)
		}
		
		var queryCount, featureCount int64
		if userRole != "admin" && username != "" {
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM audit_log WHERE username = ? AND event_type = 'query_execution'`), username).Scan(&queryCount)
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM audit_log WHERE username = ? AND event_type = 'feature_access'`), username).Scan(&featureCount)
		} else {
			appdb.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event_type = 'query_execution'`).Scan(&queryCount)
			appdb.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event_type = 'feature_access'`).Scan(&featureCount)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"total": total, "errors": errors, "avg_ms": avgMs,
			"query_count": queryCount, "feature_count": featureCount,
		})
	}
}
