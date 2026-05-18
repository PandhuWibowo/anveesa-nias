package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ── Slow Query Log ──────────────────────────────────────────────────

type SlowQueryRow struct {
	QueryID       string  `json:"query_id"`
	Query         string  `json:"query"`
	StatementType string  `json:"statement_type"`
	Database      string  `json:"database"`
	Username      string  `json:"username"`
	Calls         int64   `json:"calls"`
	AvgMs         float64 `json:"avg_ms"`
	MinMs         float64 `json:"min_ms"`
	MaxMs         float64 `json:"max_ms"`
	TotalMs       float64 `json:"total_ms"`
	Rows          int64   `json:"rows"`
}

type SlowQueryResponse struct {
	Rows        []SlowQueryRow `json:"rows"`
	Total       int            `json:"total"`
	ThresholdMs float64        `json:"threshold_ms"`
	Source      string         `json:"source"`
	Notice      string         `json:"notice,omitempty"`
}

// DBSlowQueries returns slow-query data from pg_stat_statements for a single connection.
func DBSlowQueries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /api/connections/{id}/db-logs/slow-queries
		if len(parts) < 5 {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		thresholdMs := 1000.0
		if v := r.URL.Query().Get("threshold_ms"); v != "" {
			if n, e := strconv.ParseFloat(v, 64); e == nil && n >= 0 {
				thresholdMs = n
			}
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 {
				page = n
			}
		}
		offset := (page - 1) * limit
		dbFilter := r.URL.Query().Get("db")
		userFilter := r.URL.Query().Get("user")

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError("connection unavailable: "+err.Error()), http.StatusServiceUnavailable)
			return
		}
		if driver != "postgres" {
			http.Error(w, jsonError("DB Logs is only available for PostgreSQL connections"), http.StatusUnprocessableEntity)
			return
		}

		rows, notice, source := queryPGSlowQueries(r, db, thresholdMs, dbFilter, userFilter, limit, offset)
		total := len(rows) // approximate; fine for display
		if limit > 0 && len(rows) == limit {
			// could be more — report limit as floor
			total = offset + len(rows)
		}

		json.NewEncoder(w).Encode(SlowQueryResponse{
			Rows:        rows,
			Total:       total,
			ThresholdMs: thresholdMs,
			Source:      source,
			Notice:      notice,
		})
	}
}

func queryPGSlowQueries(r *http.Request, db *sql.DB, thresholdMs float64, dbFilter, userFilter string, limit, offset int) ([]SlowQueryRow, string, string) {
	ctx := r.Context()

	// Try PG 13+ column names first (total_exec_time / mean_exec_time)
	q := `
		SELECT
			s.queryid::text,
			s.query,
			d.datname,
			u.usename,
			s.calls,
			s.mean_exec_time,
			COALESCE(s.min_exec_time, 0),
			s.max_exec_time,
			s.total_exec_time,
			s.rows
		FROM pg_stat_statements s
		JOIN pg_database d ON d.oid = s.dbid
		JOIN pg_user u ON u.usesysid = s.userid
		WHERE s.mean_exec_time >= $1
	`
	args := []interface{}{thresholdMs}
	argIdx := 2
	if dbFilter != "" {
		q += ` AND d.datname = $` + strconv.Itoa(argIdx)
		args = append(args, dbFilter)
		argIdx++
	}
	if userFilter != "" {
		q += ` AND u.usename = $` + strconv.Itoa(argIdx)
		args = append(args, userFilter)
		argIdx++
	}
	q += ` ORDER BY s.mean_exec_time DESC LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		// Legacy PG < 13 used total_time / mean_time
		q2 := strings.ReplaceAll(q, "mean_exec_time", "mean_time")
		q2 = strings.ReplaceAll(q2, "min_exec_time", "0")
		q2 = strings.ReplaceAll(q2, "total_exec_time", "total_time")
		q2 = strings.ReplaceAll(q2, "max_exec_time", "max_time")
		rows, err = db.QueryContext(ctx, q2, args...)
		if err != nil {
			return nil, "pg_stat_statements extension is not available or you lack SELECT privilege on it. " +
				"Enable it with: CREATE EXTENSION IF NOT EXISTS pg_stat_statements;", ""
		}
		defer rows.Close()
		return scanSlowRows(rows), "", "pg_stat_statements (legacy)"
	}
	defer rows.Close()
	return scanSlowRows(rows), "", "pg_stat_statements"
}

func scanSlowRows(rows *sql.Rows) []SlowQueryRow {
	var result []SlowQueryRow
	for rows.Next() {
		var r SlowQueryRow
		if err := rows.Scan(
			&r.QueryID, &r.Query, &r.Database, &r.Username,
			&r.Calls, &r.AvgMs, &r.MinMs, &r.MaxMs, &r.TotalMs, &r.Rows,
		); err != nil {
			continue
		}
		r.StatementType = inferStatementType(r.Query)
		result = append(result, r)
	}
	if result == nil {
		result = []SlowQueryRow{}
	}
	return result
}

func inferStatementType(q string) string {
	t := strings.ToUpper(strings.TrimSpace(q))
	switch {
	case strings.HasPrefix(t, "SELECT"), strings.HasPrefix(t, "WITH"):
		return "SELECT"
	case strings.HasPrefix(t, "INSERT"):
		return "INSERT"
	case strings.HasPrefix(t, "UPDATE"):
		return "UPDATE"
	case strings.HasPrefix(t, "DELETE"):
		return "DELETE"
	case strings.HasPrefix(t, "CREATE"):
		return "CREATE"
	case strings.HasPrefix(t, "ALTER"):
		return "ALTER"
	case strings.HasPrefix(t, "DROP"):
		return "DROP"
	case strings.HasPrefix(t, "EXPLAIN"):
		return "EXPLAIN"
	default:
		return "OTHER"
	}
}

// ── Error Log ───────────────────────────────────────────────────────

type ErrorLogRow struct {
	LogTime      string `json:"log_time"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	Detail       string `json:"detail"`
	Hint         string `json:"hint"`
	Query        string `json:"query"`
	Username     string `json:"username"`
	DatabaseName string `json:"database_name"`
	AppName      string `json:"app_name"`
	RemoteHost   string `json:"remote_host"`
	SQLState     string `json:"sql_state"`
}

type ErrorLogResponse struct {
	Rows   []ErrorLogRow `json:"rows"`
	Total  int           `json:"total"`
	Source string        `json:"source"`
	Notice string        `json:"notice,omitempty"`
}

// DBErrorLogs returns error log rows from pg_catalog.pg_log (PG 15+) or pg_read_file fallback.
func DBErrorLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 5 {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 {
				page = n
			}
		}
		offset := (page - 1) * limit

		// Comma-separated severities: ERROR,FATAL,CONTEXT,STATEMENT,WARNING
		levelFilter := r.URL.Query().Get("level")

		// from / to in RFC3339 or YYYY-MM-DD
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError("connection unavailable: "+err.Error()), http.StatusServiceUnavailable)
			return
		}
		if driver != "postgres" {
			http.Error(w, jsonError("DB Logs is only available for PostgreSQL connections"), http.StatusUnprocessableEntity)
			return
		}

		rows, source, notice := queryPGErrorLogs(r.Context(), db, levelFilter, fromStr, toStr, limit, offset)
		json.NewEncoder(w).Encode(ErrorLogResponse{
			Rows:   rows,
			Total:  offset + len(rows),
			Source: source,
			Notice: notice,
		})
	}
}

func queryPGErrorLogs(ctx context.Context, db *sql.DB, levelFilter, fromStr, toStr string, limit, offset int) ([]ErrorLogRow, string, string) {

	levels := []string{}
	if levelFilter != "" {
		for _, l := range strings.Split(levelFilter, ",") {
			if t := strings.TrimSpace(strings.ToUpper(l)); t != "" {
				levels = append(levels, t)
			}
		}
	}

	var fromTime, toTime time.Time
	if fromStr != "" {
		fromTime, _ = time.Parse("2006-01-02", fromStr)
		if fromTime.IsZero() {
			fromTime, _ = time.Parse(time.RFC3339, fromStr)
		}
	}
	if toStr != "" {
		toTime, _ = time.Parse("2006-01-02", toStr)
		if toTime.IsZero() {
			toTime, _ = time.Parse(time.RFC3339, toStr)
		}
		if !toTime.IsZero() {
			toTime = toTime.Add(24*time.Hour - time.Second) // inclusive end of day
		}
	}

	// Attempt 1: pg_catalog.pg_log (PG 15+ / some managed DBs)
	pgRows, err := tryPGCatalogLog(ctx, db, levels, fromTime, toTime, limit, offset)
	if err == nil {
		return pgRows, "pg_catalog.pg_log", ""
	}

	// Attempt 2: pg_read_file on CSV log — only works if superuser
	pgRows, err = tryPGReadFileLog(ctx, db, levels, limit, offset)
	if err == nil {
		return pgRows, "pg_read_file (csv log)", ""
	}

	// Nothing worked
	notice := "Error log access requires pg_catalog.pg_log (PostgreSQL 15+) or superuser access for pg_read_file. " +
		"Check your PostgreSQL version and user permissions."
	return []ErrorLogRow{}, "", notice
}

func tryPGCatalogLog(ctx context.Context, db *sql.DB, levels []string, from, to time.Time, limit, offset int) ([]ErrorLogRow, error) {
	q := `
		SELECT
			log_time::text,
			error_severity,
			COALESCE(message, ''),
			COALESCE(detail, ''),
			COALESCE(hint, ''),
			COALESCE(query, ''),
			COALESCE(user_name, ''),
			COALESCE(database_name, ''),
			COALESCE(application_name, ''),
			COALESCE(connection_from, ''),
			COALESCE(sql_state_code, '')
		FROM pg_catalog.pg_log
		WHERE 1=1
	`
	args := []interface{}{}
	idx := 1

	if len(levels) > 0 {
		placeholders := make([]string, len(levels))
		for i, l := range levels {
			placeholders[i] = "$" + strconv.Itoa(idx)
			args = append(args, l)
			idx++
		}
		q += " AND error_severity IN (" + strings.Join(placeholders, ",") + ")"
	}
	if !from.IsZero() {
		q += " AND log_time >= $" + strconv.Itoa(idx)
		args = append(args, from)
		idx++
	}
	if !to.IsZero() {
		q += " AND log_time <= $" + strconv.Itoa(idx)
		args = append(args, to)
		idx++
	}
	q += " ORDER BY log_time DESC LIMIT $" + strconv.Itoa(idx) + " OFFSET $" + strconv.Itoa(idx+1)
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanErrorRows(rows), nil
}

func tryPGReadFileLog(ctx context.Context, db *sql.DB, levels []string, limit, offset int) ([]ErrorLogRow, error) {
	// Discover the most recent CSV log file (superuser only)
	var logFile string
	err := db.QueryRowContext(ctx, `
		SELECT name FROM pg_ls_logdir()
		WHERE name LIKE '%.csv'
		ORDER BY modification DESC
		LIMIT 1
	`).Scan(&logFile)
	if err != nil || logFile == "" {
		return nil, err
	}

	var content string
	err = db.QueryRowContext(ctx, `SELECT pg_read_file('log/'||$1, 0, 5000000)`, logFile).Scan(&content)
	if err != nil {
		return nil, err
	}

	return parseCSVLog(content, levels, limit, offset), nil
}

func parseCSVLog(content string, levels []string, limit, offset int) []ErrorLogRow {
	levelSet := map[string]bool{}
	for _, l := range levels {
		levelSet[strings.ToUpper(l)] = true
	}

	var result []ErrorLogRow
	skipped := 0
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		cols := splitCSVLine(line)
		if len(cols) < 14 {
			continue
		}
		severity := strings.ToUpper(strings.TrimSpace(cols[11]))
		if len(levelSet) > 0 && !levelSet[severity] {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(result) >= limit {
			break
		}
		result = append(result, ErrorLogRow{
			LogTime:      cols[0],
			Username:     cols[1],
			DatabaseName: cols[2],
			AppName:      cols[4],
			RemoteHost:   cols[5],
			Severity:     severity,
			SQLState:     cols[12],
			Message:      cols[13],
		})
	}
	if result == nil {
		result = []ErrorLogRow{}
	}
	return result
}

// splitCSVLine handles basic CSV line splitting (no multiline support needed for log lines).
func splitCSVLine(line string) []string {
	var fields []string
	inQuote := false
	field := strings.Builder{}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' && !inQuote:
			inQuote = true
		case c == '"' && inQuote:
			if i+1 < len(line) && line[i+1] == '"' {
				field.WriteByte('"')
				i++
			} else {
				inQuote = false
			}
		case c == ',' && !inQuote:
			fields = append(fields, field.String())
			field.Reset()
		default:
			field.WriteByte(c)
		}
	}
	fields = append(fields, field.String())
	return fields
}

func scanErrorRows(rows *sql.Rows) []ErrorLogRow {
	var result []ErrorLogRow
	for rows.Next() {
		var r ErrorLogRow
		if err := rows.Scan(
			&r.LogTime, &r.Severity, &r.Message, &r.Detail, &r.Hint,
			&r.Query, &r.Username, &r.DatabaseName, &r.AppName, &r.RemoteHost, &r.SQLState,
		); err != nil {
			continue
		}
		result = append(result, r)
	}
	if result == nil {
		result = []ErrorLogRow{}
	}
	return result
}
