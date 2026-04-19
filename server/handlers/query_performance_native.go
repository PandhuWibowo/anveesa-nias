package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	appdb "github.com/anveesa/nias/db"
)

type NativeQueryStat struct {
	ConnID         int64   `json:"conn_id"`
	ConnName       string  `json:"conn_name"`
	Driver         string  `json:"driver"`
	Source         string  `json:"source"`
	Fingerprint    string  `json:"fingerprint"`
	SQL            string  `json:"sql"`
	Calls          int64   `json:"calls"`
	TotalMs        float64 `json:"total_ms"`
	AvgMs          float64 `json:"avg_ms"`
	MaxMs          float64 `json:"max_ms"`
	Rows           int64   `json:"rows"`
	RowsExamined   int64   `json:"rows_examined"`
	LastSeen       string  `json:"last_seen"`
}

type NativeQueryNotice struct {
	ConnID   int64  `json:"conn_id"`
	ConnName string `json:"conn_name"`
	Driver   string `json:"driver"`
	Message  string `json:"message"`
}

type NativeQueryPerformanceResponse struct {
	Stats   []NativeQueryStat   `json:"stats"`
	Notices []NativeQueryNotice `json:"notices"`
}

type nativeConnectionSummary struct {
	ID     int64
	Name   string
	Driver string
}

func ListNativeQueryPerformance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}

		var filterConnID int64
		if raw := r.URL.Query().Get("conn_id"); raw != "" && raw != "all" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
				filterConnID = n
			}
		}

		conns, err := listAccessibleConnectionSummaries(r)
		if err != nil {
			http.Error(w, jsonError("failed to load connections"), http.StatusInternalServerError)
			return
		}

		resp := NativeQueryPerformanceResponse{
			Stats:   []NativeQueryStat{},
			Notices: []NativeQueryNotice{},
		}

		for _, conn := range conns {
			if filterConnID > 0 && conn.ID != filterConnID {
				continue
			}

			stats, notice := loadNativeStatsForConnection(r, conn, limit)
			if notice != nil {
				resp.Notices = append(resp.Notices, *notice)
			}
			resp.Stats = append(resp.Stats, stats...)
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func listAccessibleConnectionSummaries(r *http.Request) ([]nativeConnectionSummary, error) {
	userIDStr := r.Header.Get("X-User-ID")
	userRole := r.Header.Get("X-User-Role")

	var userID int64
	if userIDStr != "" {
		userID, _ = strconv.ParseInt(userIDStr, 10, 64)
	}

	var (
		query string
		args  []interface{}
	)

	if userRole == "admin" || !isAuthEnabled() {
		query = `SELECT c.id, c.name, c.driver FROM connections c ORDER BY c.id`
	} else {
		query = appdb.ConvertQuery(`SELECT DISTINCT c.id, c.name, c.driver
			FROM connections c
			LEFT JOIN connection_folders f ON c.folder_id = f.id
			LEFT JOIN user_connections uc ON c.id = uc.conn_id AND uc.user_id = ?
			LEFT JOIN folder_members fm ON f.id = fm.folder_id AND fm.user_id = ?
			WHERE
			  (c.visibility = 'shared' AND (f.id IS NULL OR f.visibility = 'shared'))
			  OR c.owner_id = ?
			  OR f.owner_id = ?
			  OR uc.conn_id IS NOT NULL
			  OR fm.folder_id IS NOT NULL
			ORDER BY c.id`)
		args = append(args, userID, userID, userID, userID)
	}

	rows, err := appdb.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []nativeConnectionSummary
	for rows.Next() {
		var c nativeConnectionSummary
		if err := rows.Scan(&c.ID, &c.Name, &c.Driver); err != nil {
			continue
		}
		conns = append(conns, c)
	}
	return conns, nil
}

func loadNativeStatsForConnection(r *http.Request, conn nativeConnectionSummary, limit int) ([]NativeQueryStat, *NativeQueryNotice) {
	db, driver, err := GetDB(conn.ID)
	if err != nil {
		return nil, &NativeQueryNotice{ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver, Message: "connection unavailable"}
	}

	switch driver {
	case "postgres":
		stats, err := loadPostgresNativeStats(r, db, conn, limit)
		if err != nil {
			return nil, &NativeQueryNotice{ConnID: conn.ID, ConnName: conn.Name, Driver: driver, Message: err.Error()}
		}
		return stats, nil
	case "mysql":
		stats, err := loadMySQLNativeStats(r, db, conn, limit)
		if err != nil {
			return nil, &NativeQueryNotice{ConnID: conn.ID, ConnName: conn.Name, Driver: driver, Message: err.Error()}
		}
		return stats, nil
	default:
		return nil, &NativeQueryNotice{ConnID: conn.ID, ConnName: conn.Name, Driver: driver, Message: "native query stats not supported for this driver"}
	}
}

func loadPostgresNativeStats(r *http.Request, db *sql.DB, conn nativeConnectionSummary, limit int) ([]NativeQueryStat, error) {
	ctx := r.Context()
	query := `
		SELECT queryid::text, query, calls, total_exec_time, mean_exec_time, max_exec_time, rows, 0::bigint, ''::text
		FROM pg_stat_statements
		ORDER BY mean_exec_time DESC
		LIMIT $1
	`
	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		legacyQuery := `
			SELECT queryid::text, query, calls, total_time, mean_time, max_time, rows, 0::bigint, ''::text
			FROM pg_stat_statements
			ORDER BY mean_time DESC
			LIMIT $1
		`
		rows, err = db.QueryContext(ctx, legacyQuery, limit)
		if err != nil {
			return nil, sqlFeatureError("pg_stat_statements extension unavailable or access denied")
		}
	}
	defer rows.Close()

	stats := make([]NativeQueryStat, 0, limit)
	for rows.Next() {
		var stat NativeQueryStat
		if err := rows.Scan(&stat.Fingerprint, &stat.SQL, &stat.Calls, &stat.TotalMs, &stat.AvgMs, &stat.MaxMs, &stat.Rows, &stat.RowsExamined, &stat.LastSeen); err != nil {
			continue
		}
		stat.ConnID = conn.ID
		stat.ConnName = conn.Name
		stat.Driver = conn.Driver
		stat.Source = "postgres:pg_stat_statements"
		stats = append(stats, stat)
	}
	return stats, nil
}

func loadMySQLNativeStats(r *http.Request, db *sql.DB, conn nativeConnectionSummary, limit int) ([]NativeQueryStat, error) {
	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(DIGEST, ''),
			COALESCE(DIGEST_TEXT, ''),
			COUNT_STAR,
			COALESCE(SUM_TIMER_WAIT / 1000000000, 0),
			COALESCE(AVG_TIMER_WAIT / 1000000000, 0),
			COALESCE(MAX_TIMER_WAIT / 1000000000, 0),
			COALESCE(SUM_ROWS_SENT, 0),
			COALESCE(SUM_ROWS_EXAMINED, 0),
			COALESCE(DATE_FORMAT(LAST_SEEN, '%Y-%m-%d %H:%i:%s'), '')
		FROM performance_schema.events_statements_summary_by_digest
		WHERE DIGEST_TEXT IS NOT NULL
		ORDER BY AVG_TIMER_WAIT DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, sqlFeatureError("performance_schema statement digest tables unavailable or access denied")
	}
	defer rows.Close()

	stats := make([]NativeQueryStat, 0, limit)
	for rows.Next() {
		var stat NativeQueryStat
		if err := rows.Scan(&stat.Fingerprint, &stat.SQL, &stat.Calls, &stat.TotalMs, &stat.AvgMs, &stat.MaxMs, &stat.Rows, &stat.RowsExamined, &stat.LastSeen); err != nil {
			continue
		}
		stat.ConnID = conn.ID
		stat.ConnName = conn.Name
		stat.Driver = conn.Driver
		stat.Source = "mysql:performance_schema"
		stats = append(stats, stat)
	}
	return stats, nil
}

func sqlFeatureError(message string) error {
	return &nativeFeatureError{message: message}
}

type nativeFeatureError struct {
	message string
}

func (e *nativeFeatureError) Error() string {
	return e.message
}
