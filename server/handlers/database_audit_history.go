package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type NativeAuditHistoryEntry struct {
	ConnID       int64  `json:"conn_id"`
	ConnName     string `json:"conn_name"`
	Driver       string `json:"driver"`
	OccurredAt   string `json:"occurred_at"`
	Username     string `json:"username"`
	ClientAddr   string `json:"client_addr"`
	CommandType  string `json:"command_type"`
	Statement    string `json:"statement"`
	ThreadID     int64  `json:"thread_id"`
	DatabaseName string `json:"database_name"`
}

type NativeAuditHistoryNotice struct {
	ConnID   int64  `json:"conn_id"`
	ConnName string `json:"conn_name"`
	Driver   string `json:"driver"`
	Level    string `json:"level"`
	Message  string `json:"message"`
}

type NativeAuditHistoryResponse struct {
	Entries []NativeAuditHistoryEntry  `json:"entries"`
	Notices []NativeAuditHistoryNotice `json:"notices"`
}

func ListNativeDatabaseAuditHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var filterConnID int64
		if raw := r.URL.Query().Get("conn_id"); raw != "" && raw != "all" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
				filterConnID = n
			}
		}

		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}

		conns, err := listAccessibleConnectionSummaries(r)
		if err != nil {
			http.Error(w, jsonError("failed to load connections"), http.StatusInternalServerError)
			return
		}

		resp := NativeAuditHistoryResponse{
			Entries: []NativeAuditHistoryEntry{},
			Notices: []NativeAuditHistoryNotice{},
		}

		for _, conn := range conns {
			if filterConnID > 0 && conn.ID != filterConnID {
				continue
			}

			entries, notices := loadNativeAuditHistoryForConnection(r, conn, limit)
			resp.Entries = append(resp.Entries, entries...)
			resp.Notices = append(resp.Notices, notices...)
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func loadNativeAuditHistoryForConnection(r *http.Request, conn nativeConnectionSummary, limit int) ([]NativeAuditHistoryEntry, []NativeAuditHistoryNotice) {
	db, driver, err := GetDB(conn.ID)
	if err != nil {
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "error", Message: "connection unavailable",
		}}
	}

	switch driver {
	case "mysql":
		return loadMySQLAuditHistory(r, db, conn, limit)
	case "postgres":
		return nil, loadPostgresAuditHistoryReadiness(r, db, conn)
	default:
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: driver,
			Level: "unsupported", Message: "native SQL-readable audit history not supported for this driver",
		}}
	}
}

func loadMySQLAuditHistory(r *http.Request, db *sql.DB, conn nativeConnectionSummary, limit int) ([]NativeAuditHistoryEntry, []NativeAuditHistoryNotice) {
	var generalLog, logOutput string
	if err := db.QueryRowContext(r.Context(), `SELECT @@global.general_log, @@global.log_output`).Scan(&generalLog, &logOutput); err != nil {
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "failed to inspect MySQL audit settings",
		}}
	}

	if generalLog != "1" && generalLog != "ON" {
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "MySQL general log is disabled; enable general_log for historical outside-app audit",
		}}
	}
	if logOutput != "TABLE" && logOutput != "TABLE,FILE" && logOutput != "FILE,TABLE" {
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "MySQL general log is not written to TABLE; set log_output=TABLE to query audit history here",
		}}
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(DATE_FORMAT(event_time, '%Y-%m-%d %H:%i:%s'), ''),
			COALESCE(user_host, ''),
			COALESCE(command_type, ''),
			COALESCE(argument, ''),
			COALESCE(thread_id, 0)
		FROM mysql.general_log
		WHERE command_type IN ('Connect', 'Query', 'Quit', 'Execute', 'Prepare')
		ORDER BY event_time DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "mysql.general_log is not queryable with the current database user",
		}}
	}
	defer rows.Close()

	entries := make([]NativeAuditHistoryEntry, 0, limit)
	for rows.Next() {
		var (
			eventTime, userHost, commandType, argument string
			threadID                                  int64
		)
		if err := rows.Scan(&eventTime, &userHost, &commandType, &argument, &threadID); err != nil {
			continue
		}
		username, clientAddr, databaseName := parseMySQLUserHost(userHost)
		entries = append(entries, NativeAuditHistoryEntry{
			ConnID:       conn.ID,
			ConnName:     conn.Name,
			Driver:       conn.Driver,
			OccurredAt:   eventTime,
			Username:     username,
			ClientAddr:   clientAddr,
			CommandType:  commandType,
			Statement:    argument,
			ThreadID:     threadID,
			DatabaseName: databaseName,
		})
	}

	return entries, []NativeAuditHistoryNotice{{
		ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
		Level: "info", Message: "history loaded from mysql.general_log",
	}}
}

func loadPostgresAuditHistoryReadiness(r *http.Request, db *sql.DB, conn nativeConnectionSummary) []NativeAuditHistoryNotice {
	var loggingCollector, logDestination, preloadLibraries, pgauditLog string
	_ = db.QueryRowContext(r.Context(), `
		SELECT
			current_setting('logging_collector', true),
			current_setting('log_destination', true),
			current_setting('shared_preload_libraries', true),
			COALESCE(current_setting('pgaudit.log', true), '')
	`).Scan(&loggingCollector, &logDestination, &preloadLibraries, &pgauditLog)

	if loggingCollector != "on" && loggingCollector != "true" {
		return []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "PostgreSQL logging_collector is not enabled; external historical audit cannot be queried here",
		}}
	}

	if pgauditLog == "" {
		return []NativeAuditHistoryNotice{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "info", Message: "PostgreSQL logging is enabled, but pgaudit is not configured; use pgaudit or log ingestion for detailed outside-app history",
		}}
	}

	return []NativeAuditHistoryNotice{{
		ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
		Level: "info", Message: "PostgreSQL logging and pgaudit appear enabled; this app still needs log ingestion to display historical entries here",
	}}
}

func parseMySQLUserHost(userHost string) (username, clientAddr, databaseName string) {
	username = userHost
	clientAddr = ""
	if at := strings.Index(userHost, "@"); at >= 0 {
		username = userHost[:at]
		clientAddr = userHost[at+1:]
	}
	username = trimMySQLAuditToken(username)
	clientAddr = trimMySQLAuditToken(clientAddr)
	return username, clientAddr, ""
}

func trimMySQLAuditToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	return value
}
