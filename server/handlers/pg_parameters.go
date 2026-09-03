package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// pg_parameters.go exposes pg_settings (Postgres's live view of
// postgresql.conf-backed GUCs) for inspection, and lets an operator change
// them via ALTER SYSTEM SET — the same underlying mechanism `psql` or an ops
// runbook would use, just from inside Nias. Nias never restarts the
// Postgres process itself: there's no SQL-level way to do that, and this
// app has no link between a Postgres connection and the container/host it
// actually runs in. For postmaster-context settings (like wal_level) the
// caller still has to restart Postgres externally — ListPgSettings and
// UpdatePgSetting both surface pending_restart so the UI can say so
// clearly instead of implying the change already took effect.

type pgSetting struct {
	Name           string `json:"name"`
	Setting        string `json:"setting"`
	Unit           string `json:"unit"`
	Category       string `json:"category"`
	ShortDesc      string `json:"short_desc"`
	Context        string `json:"context"`
	VarType        string `json:"vartype"`
	MinVal         string `json:"min_val"`
	MaxVal         string `json:"max_val"`
	EnumVals       string `json:"enumvals"`
	BootVal        string `json:"boot_val"`
	ResetVal       string `json:"reset_val"`
	PendingRestart bool   `json:"pending_restart"`
}

// ListPgSettings returns every row of pg_settings for a Postgres connection.
// GET /api/pg-parameters/settings?connection_id=X
func ListPgSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := pgReplConnID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, connID) {
			http.Error(w, jsonError("permission denied on connection"), http.StatusForbidden)
			return
		}
		db, err := requirePostgresDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		rows, err := db.QueryContext(r.Context(), `
			SELECT name, setting, COALESCE(unit,''), COALESCE(category,''), COALESCE(short_desc,''),
			       context, vartype, COALESCE(min_val,''), COALESCE(max_val,''),
			       COALESCE(array_to_string(enumvals, ','), ''), COALESCE(boot_val,''), COALESCE(reset_val,''), pending_restart
			FROM pg_settings
			ORDER BY category, name`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		settings := []pgSetting{}
		for rows.Next() {
			var s pgSetting
			if err := rows.Scan(&s.Name, &s.Setting, &s.Unit, &s.Category, &s.ShortDesc,
				&s.Context, &s.VarType, &s.MinVal, &s.MaxVal, &s.EnumVals, &s.BootVal, &s.ResetVal, &s.PendingRestart); err != nil {
				continue
			}
			settings = append(settings, s)
		}
		json.NewEncoder(w).Encode(settings)
	}
}

type updatePgSettingRequest struct {
	Value *string `json:"value"` // nil = reset to default (ALTER SYSTEM RESET)
}

// UpdatePgSetting runs ALTER SYSTEM SET (or RESET, if value is nil/omitted)
// for one parameter, then reloads the config so anything reloadable takes
// effect immediately. Settings with context = "postmaster" still need an
// external restart no matter what — this only ever runs SQL, it can't
// restart the process itself.
// PUT /api/pg-parameters/settings/{name}?connection_id=X
func UpdatePgSetting() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		start := time.Now()
		username := r.Header.Get("X-Username")
		if username == "" {
			username = "anonymous"
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/pg-parameters/settings/")
		if name == "" {
			http.Error(w, jsonError("parameter name is required"), http.StatusBadRequest)
			return
		}
		connID, err := pgReplConnID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if !CheckWritePermission(r, connID) {
			http.Error(w, jsonError("permission denied on connection"), http.StatusForbidden)
			return
		}
		var req updatePgSettingRequest
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&req) // best-effort: absent/empty body = reset
		}

		connName := connectionNameByID(connID)
		db, err := requirePostgresDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		// Round-trip the name through pg_settings itself rather than trusting
		// the URL path as an identifier directly — this both rejects unknown
		// parameters up front (a clear 400 instead of a raw Postgres DDL
		// error) and guarantees the identifier we go on to quote and
		// interpolate is exactly what Postgres already knows as a real GUC.
		var realName, context string
		if lookupErr := db.QueryRowContext(r.Context(), `SELECT name, context FROM pg_settings WHERE name = $1`, name).Scan(&realName, &context); lookupErr != nil {
			writePgParamAudit("alter_system", name, "", username, connID, connName, time.Since(start).Milliseconds(), "unknown parameter")
			http.Error(w, jsonError("unknown parameter: "+name), http.StatusBadRequest)
			return
		}

		var stmt, action, details string
		if req.Value == nil {
			stmt = "ALTER SYSTEM RESET " + quoteIdentPG(realName)
			action = "alter_system_reset"
			details = "reset to default"
		} else {
			stmt = "ALTER SYSTEM SET " + quoteIdentPG(realName) + " = '" + escapePGLiteral(*req.Value) + "'"
			action = "alter_system_set"
			details = "set to " + *req.Value
		}

		if _, err := db.ExecContext(r.Context(), stmt); err != nil {
			writePgParamAudit(action, realName, details, username, connID, connName, time.Since(start).Milliseconds(), err.Error())
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		// Reload picks up anything with context sighup/backend/superuser/user
		// immediately; postmaster-context settings are left pending until an
		// external restart regardless of whether this call succeeds.
		db.ExecContext(r.Context(), `SELECT pg_reload_conf()`)

		var pendingRestart bool
		db.QueryRowContext(r.Context(), `SELECT pending_restart FROM pg_settings WHERE name = $1`, realName).Scan(&pendingRestart)

		writePgParamAudit(action, realName, details, username, connID, connName, time.Since(start).Milliseconds(), "")

		json.NewEncoder(w).Encode(map[string]any{
			"name":            realName,
			"context":         context,
			"pending_restart": pendingRestart,
		})
	}
}

// ReloadPgConfig runs pg_reload_conf() — useful after editing
// postgresql.conf directly outside Nias, or just to re-apply after a batch
// of changes made through UpdatePgSetting.
// POST /api/pg-parameters/reload?connection_id=X
func ReloadPgConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := pgReplConnID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if !CheckWritePermission(r, connID) {
			http.Error(w, jsonError("permission denied on connection"), http.StatusForbidden)
			return
		}
		db, err := requirePostgresDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var ok bool
		if err := db.QueryRowContext(r.Context(), `SELECT pg_reload_conf()`).Scan(&ok); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"reloaded": ok})
	}
}

// writePgParamAudit records a parameter change under the shared audit_log
// table (see backup_bucket.go's writeBackupAudit for the same pattern) —
// ALTER SYSTEM SET is a high-impact, easy-to-fat-finger action worth a
// permanent trail of who changed what and when.
func writePgParamAudit(action, target, details, username string, connID int64, connName string, durationMs int64, errMsg string) {
	var connIDPtr *int64
	if connID > 0 {
		connIDPtr = &connID
	}
	writeAuditEvent("pg_parameters", action, target, details, username, connIDPtr, connName, "", durationMs, 0, errMsg)
}
