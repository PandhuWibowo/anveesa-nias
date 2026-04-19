package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

type NativeAccessSession struct {
	ConnID          int64  `json:"conn_id"`
	ConnName        string `json:"conn_name"`
	Driver          string `json:"driver"`
	Username        string `json:"username"`
	ClientAddr      string `json:"client_addr"`
	ApplicationName string `json:"application_name"`
	DatabaseName    string `json:"database_name"`
	SessionState    string `json:"session_state"`
	Command         string `json:"command"`
	DurationSec     int64  `json:"duration_sec"`
	WaitEvent       string `json:"wait_event"`
	StartedAt       string `json:"started_at"`
	QueryStartedAt  string `json:"query_started_at"`
	QueryText       string `json:"query_text"`
}

type NativeAuditCapability struct {
	ConnID   int64  `json:"conn_id"`
	ConnName string `json:"conn_name"`
	Driver   string `json:"driver"`
	Level    string `json:"level"`
	Message  string `json:"message"`
}

type NativeAuditResponse struct {
	Sessions     []NativeAccessSession   `json:"sessions"`
	Capabilities []NativeAuditCapability `json:"capabilities"`
}

func ListNativeDatabaseAudit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

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

		resp := NativeAuditResponse{
			Sessions:     []NativeAccessSession{},
			Capabilities: []NativeAuditCapability{},
		}

		for _, conn := range conns {
			if filterConnID > 0 && conn.ID != filterConnID {
				continue
			}

			sessions, capabilities := loadNativeAuditForConnection(r, conn)
			resp.Sessions = append(resp.Sessions, sessions...)
			resp.Capabilities = append(resp.Capabilities, capabilities...)
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func loadNativeAuditForConnection(r *http.Request, conn nativeConnectionSummary) ([]NativeAccessSession, []NativeAuditCapability) {
	db, driver, err := GetDB(conn.ID)
	if err != nil {
		return nil, []NativeAuditCapability{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "error", Message: "connection unavailable",
		}}
	}

	switch driver {
	case "postgres":
		return loadPostgresAuditSessions(r, db, conn)
	case "mysql":
		return loadMySQLAuditSessions(r, db, conn)
	default:
		return nil, []NativeAuditCapability{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: driver,
			Level: "unsupported", Message: "native session audit not supported for this driver",
		}}
	}
}

func loadPostgresAuditSessions(r *http.Request, db *sql.DB, conn nativeConnectionSummary) ([]NativeAccessSession, []NativeAuditCapability) {
	rows, err := db.QueryContext(r.Context(), `
		SELECT
			usename,
			COALESCE(client_addr::text, ''),
			COALESCE(application_name, ''),
			COALESCE(datname, ''),
			COALESCE(state, ''),
			'backend',
			COALESCE(EXTRACT(EPOCH FROM (now() - COALESCE(query_start, backend_start))), 0)::bigint,
			TRIM(BOTH ' ' FROM CONCAT(COALESCE(wait_event_type, ''), ' ', COALESCE(wait_event, ''))),
			COALESCE(to_char(backend_start, 'YYYY-MM-DD HH24:MI:SS'), ''),
			COALESCE(to_char(query_start, 'YYYY-MM-DD HH24:MI:SS'), ''),
			COALESCE(query, '')
		FROM pg_stat_activity
		WHERE pid <> pg_backend_pid()
		ORDER BY query_start DESC NULLS LAST, backend_start DESC NULLS LAST
	`)
	if err != nil {
		return nil, []NativeAuditCapability{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "pg_stat_activity unavailable or access denied",
		}}
	}
	defer rows.Close()

	sessions := make([]NativeAccessSession, 0)
	for rows.Next() {
		var s NativeAccessSession
		if err := rows.Scan(&s.Username, &s.ClientAddr, &s.ApplicationName, &s.DatabaseName, &s.SessionState, &s.Command, &s.DurationSec, &s.WaitEvent, &s.StartedAt, &s.QueryStartedAt, &s.QueryText); err != nil {
			continue
		}
		s.ConnID = conn.ID
		s.ConnName = conn.Name
		s.Driver = conn.Driver
		sessions = append(sessions, s)
	}

	return sessions, []NativeAuditCapability{{
		ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
		Level: "info", Message: "live session visibility from pg_stat_activity; full external history still requires PostgreSQL logging or pgaudit",
	}}
}

func loadMySQLAuditSessions(r *http.Request, db *sql.DB, conn nativeConnectionSummary) ([]NativeAccessSession, []NativeAuditCapability) {
	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(USER, ''),
			COALESCE(HOST, ''),
			'',
			COALESCE(DB, ''),
			COALESCE(STATE, ''),
			COALESCE(COMMAND, ''),
			COALESCE(TIME, 0),
			'',
			'',
			'',
			COALESCE(INFO, '')
		FROM information_schema.PROCESSLIST
		WHERE ID <> CONNECTION_ID()
		ORDER BY TIME DESC
	`)
	if err != nil {
		return nil, []NativeAuditCapability{{
			ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
			Level: "warning", Message: "PROCESSLIST unavailable or access denied",
		}}
	}
	defer rows.Close()

	sessions := make([]NativeAccessSession, 0)
	for rows.Next() {
		var s NativeAccessSession
		if err := rows.Scan(&s.Username, &s.ClientAddr, &s.ApplicationName, &s.DatabaseName, &s.SessionState, &s.Command, &s.DurationSec, &s.WaitEvent, &s.StartedAt, &s.QueryStartedAt, &s.QueryText); err != nil {
			continue
		}
		s.ConnID = conn.ID
		s.ConnName = conn.Name
		s.Driver = conn.Driver
		sessions = append(sessions, s)
	}

	return sessions, []NativeAuditCapability{{
		ConnID: conn.ID, ConnName: conn.Name, Driver: conn.Driver,
		Level: "info", Message: "live session visibility from PROCESSLIST; full external history requires MySQL audit/general logs or an audit plugin",
	}}
}
