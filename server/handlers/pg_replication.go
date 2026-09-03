package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// pg_replication.go manages native Postgres logical replication (CREATE
// PUBLICATION / CREATE SUBSCRIPTION) between two stored Postgres
// connections. Unlike Cloud Storage transfer, the actual data never flows
// through Nias — once CREATE SUBSCRIPTION runs, replication traffic goes
// directly between the two Postgres servers, so this file's job is limited
// to running that DDL on each side (via the connections' own pooled
// *sql.DB, GetDB in pool.go) and reading back status/lag for the UI.
// Nias's own pg_replication_links table (db/db.go) is bookkeeping only — it
// exists so the UI can show which publication/subscription pair belongs to
// which two Nias connections, since Postgres itself only knows a raw libpq
// connection string, not a Nias connection id.

type pgConnInfo struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSL      bool
}

// fetchPgConnInfo fetches and decrypts a Postgres connection's fields —
// mirrors openRemoteDB's inline fetch (connections.go:1031), but returns
// the raw fields instead of an opened *sql.DB, since callers here need to
// build a libpq connection string for a *different* connection to use.
func fetchPgConnInfo(connID int64) (*pgConnInfo, error) {
	var info pgConnInfo
	var driver string
	var ssl int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`),
		connID,
	).Scan(&driver, &info.Host, &info.Port, &info.Database, &info.Username, &encPassword, &ssl)
	if err != nil {
		return nil, fmt.Errorf("connection not found")
	}
	if driver != "postgres" {
		return nil, fmt.Errorf("connection is not PostgreSQL")
	}
	info.SSL = ssl == 1
	password, err := decryptCredential(encPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials")
	}
	info.Password = password
	return &info, nil
}

// pgConnInfoString builds a libpq keyword/value connection string for use in
// CREATE SUBSCRIPTION ... CONNECTION '...'. Every value is single-quoted
// with backslash/quote escaping applied unconditionally (not just when a
// value "looks like" it needs it, the way escapePostgresValue in
// connections.go does) — libpq's keyword/value format has no issue with an
// always-quoted value, and always quoting is the only way to guarantee an
// unusual password (empty, containing a quote, etc.) never produces a
// silently-broken connection string.
func pgConnInfoString(info *pgConnInfo) string {
	sslmode := "disable"
	if info.SSL {
		sslmode = "require"
	}
	kv := func(key, val string) string {
		val = strings.ReplaceAll(val, `\`, `\\`)
		val = strings.ReplaceAll(val, `'`, `\'`)
		return key + "='" + val + "'"
	}
	return strings.Join([]string{
		kv("host", info.Host),
		kv("port", strconv.Itoa(info.Port)),
		kv("dbname", info.Database),
		kv("user", info.Username),
		kv("password", info.Password),
		kv("sslmode", sslmode),
	}, " ")
}

func pgReplConnID(r *http.Request) (int64, error) {
	idStr := r.URL.Query().Get("connection_id")
	if idStr == "" {
		return 0, fmt.Errorf("connection_id is required")
	}
	return strconv.ParseInt(idStr, 10, 64)
}

// requirePostgresDB returns the live pooled *sql.DB for connID, rejecting
// anything that isn't a Postgres connection — every operation in this file
// (publications, subscriptions, replication slots) is Postgres-only syntax.
func requirePostgresDB(connID int64) (*sql.DB, error) {
	db, driver, err := GetDB(connID)
	if err != nil {
		return nil, err
	}
	if driver != "postgres" {
		return nil, fmt.Errorf("connection is not PostgreSQL")
	}
	return db, nil
}

// splitSchemaTable splits a "schema.table" reference into its two raw
// (unquoted) parts.
func splitSchemaTable(schemaTable string) (schema, table string, err error) {
	parts := strings.SplitN(schemaTable, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid table reference %q — expected schema.table", schemaTable)
	}
	return parts[0], parts[1], nil
}

// quoteSchemaTable quotes a "schema.table" reference as two separately
// quoted identifiers (schema."table" style quoting is wrong; each part
// needs its own quotes) for use in CREATE PUBLICATION ... FOR TABLE.
func quoteSchemaTable(schemaTable string) (string, error) {
	schema, table, err := splitSchemaTable(schemaTable)
	if err != nil {
		return "", err
	}
	return quoteIdentPG(schema) + "." + quoteIdentPG(table), nil
}

// ── Publications ─────────────────────────────────────────────────────────

type pgPublicationTable struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
}

type pgPublication struct {
	Name      string               `json:"name"`
	AllTables bool                 `json:"all_tables"`
	Insert    bool                 `json:"insert"`
	Update    bool                 `json:"update"`
	Delete    bool                 `json:"delete"`
	Truncate  bool                 `json:"truncate"`
	Tables    []pgPublicationTable `json:"tables"`
}

// ListPublications returns every publication on a Postgres connection, plus
// prerequisite warnings (wal_level, replication privilege) the create-flow
// UI surfaces before the user tries and fails.
// GET /api/pg-replication/publications?connection_id=X
func ListPublications() http.HandlerFunc {
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

		rows, err := db.QueryContext(r.Context(), `SELECT pubname, puballtables, pubinsert, pubupdate, pubdelete, pubtruncate FROM pg_publication ORDER BY pubname`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		pubs := map[string]*pgPublication{}
		var order []string
		for rows.Next() {
			p := &pgPublication{}
			if err := rows.Scan(&p.Name, &p.AllTables, &p.Insert, &p.Update, &p.Delete, &p.Truncate); err != nil {
				continue
			}
			pubs[p.Name] = p
			order = append(order, p.Name)
		}
		rows.Close()

		tblRows, err := db.QueryContext(r.Context(), `SELECT pubname, schemaname, tablename FROM pg_publication_tables ORDER BY pubname, schemaname, tablename`)
		if err == nil {
			for tblRows.Next() {
				var pubname, schema, table string
				if scanErr := tblRows.Scan(&pubname, &schema, &table); scanErr != nil {
					continue
				}
				if p, ok := pubs[pubname]; ok {
					p.Tables = append(p.Tables, pgPublicationTable{Schema: schema, Table: table})
				}
			}
			tblRows.Close()
		}

		result := make([]*pgPublication, 0, len(order))
		for _, name := range order {
			result = append(result, pubs[name])
		}

		var walLevel string
		_ = db.QueryRowContext(r.Context(), `SHOW wal_level`).Scan(&walLevel)
		var hasReplPriv bool
		_ = db.QueryRowContext(r.Context(), `SELECT rolreplication OR rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&hasReplPriv)

		json.NewEncoder(w).Encode(map[string]any{
			"publications":              result,
			"wal_level":                 walLevel,
			"wal_level_ok":              walLevel == "logical",
			"has_replication_privilege": hasReplPriv,
		})
	}
}

type createPublicationRequest struct {
	ConnectionID int64    `json:"connectionId"`
	Name         string   `json:"name"`
	AllTables    bool     `json:"allTables"`
	Tables       []string `json:"tables"`     // "schema.table"
	Operations   []string `json:"operations"` // subset of insert/update/delete/truncate; empty = all
}

// CreatePublication runs CREATE PUBLICATION on the given connection.
// POST /api/pg-replication/publications
func CreatePublication() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req createPublicationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			http.Error(w, jsonError("name is required"), http.StatusBadRequest)
			return
		}
		if !req.AllTables && len(req.Tables) == 0 {
			http.Error(w, jsonError(`select at least one table, or choose "all tables"`), http.StatusBadRequest)
			return
		}
		if !CheckWritePermission(r, req.ConnectionID) {
			http.Error(w, jsonError("permission denied on connection"), http.StatusForbidden)
			return
		}
		db, err := requirePostgresDB(req.ConnectionID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var stmt strings.Builder
		stmt.WriteString("CREATE PUBLICATION " + quoteIdentPG(name))
		if req.AllTables {
			stmt.WriteString(" FOR ALL TABLES")
		} else {
			idents := make([]string, 0, len(req.Tables))
			for _, t := range req.Tables {
				ident, identErr := quoteSchemaTable(t)
				if identErr != nil {
					http.Error(w, jsonError(identErr.Error()), http.StatusBadRequest)
					return
				}
				idents = append(idents, ident)
			}
			stmt.WriteString(" FOR TABLE " + strings.Join(idents, ", "))
		}
		if len(req.Operations) > 0 {
			allowed := map[string]bool{"insert": true, "update": true, "delete": true, "truncate": true}
			var ops []string
			for _, op := range req.Operations {
				op = strings.ToLower(strings.TrimSpace(op))
				if allowed[op] {
					ops = append(ops, op)
				}
			}
			if len(ops) > 0 {
				stmt.WriteString(fmt.Sprintf(" WITH (publish = '%s')", strings.Join(ops, ",")))
			}
		}

		if _, err := db.ExecContext(r.Context(), stmt.String()); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": name})
	}
}

// DropPublication drops a publication.
// DELETE /api/pg-replication/publications/{name}?connection_id=X
func DropPublication() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := strings.TrimPrefix(r.URL.Path, "/api/pg-replication/publications/")
		if name == "" {
			http.Error(w, jsonError("publication name is required"), http.StatusBadRequest)
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
		db, err := requirePostgresDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if _, err := db.ExecContext(r.Context(), "DROP PUBLICATION "+quoteIdentPG(name)); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "dropped"})
	}
}

// ── Subscriptions ────────────────────────────────────────────────────────

type pgSubscription struct {
	Name               string     `json:"name"`
	Enabled            bool       `json:"enabled"`
	PID                *int64     `json:"pid,omitempty"`
	ReceivedLSN        string     `json:"received_lsn,omitempty"`
	LatestEndLSN       string     `json:"latest_end_lsn,omitempty"`
	LastMsgReceiptTime *time.Time `json:"last_msg_receipt_time,omitempty"`
	LagSeconds         *float64   `json:"lag_seconds,omitempty"`
	LagBytes           *int64     `json:"lag_bytes,omitempty"` // best-effort, from the source connection's replication slot
}

// listReplicationLinksForTarget maps subscription name -> source connection
// id for every link bookkept against targetConnID, so ListSubscriptions can
// go fetch byte lag from the right source connection.
func listReplicationLinksForTarget(targetConnID int64) map[string]int64 {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`SELECT subscription_name, source_connection_id FROM pg_replication_links WHERE target_connection_id=?`), targetConnID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var srcID int64
		if err := rows.Scan(&name, &srcID); err != nil {
			continue
		}
		out[name] = srcID
	}
	return out
}

// fetchSourceSlotLagBytes queries the source connection's replication slot
// (created automatically by CREATE SUBSCRIPTION, named after the
// subscription by default) for how far the publisher's WAL has advanced
// past what's been confirmed flushed to the subscriber.
func fetchSourceSlotLagBytes(ctx context.Context, srcConnID int64, slotName string) (int64, error) {
	db, err := requirePostgresDB(srcConnID)
	if err != nil {
		return 0, err
	}
	var lag int64
	err = db.QueryRowContext(ctx, `
		SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)::bigint
		FROM pg_replication_slots WHERE slot_name = $1`, slotName).Scan(&lag)
	if err != nil {
		return 0, err
	}
	return lag, nil
}

// ListSubscriptions returns every subscription on a Postgres connection with
// stat-based status and lag.
// GET /api/pg-replication/subscriptions?connection_id=X
func ListSubscriptions() http.HandlerFunc {
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
			SELECT s.subname, s.subenabled, st.pid, st.received_lsn::text, st.latest_end_lsn::text, st.last_msg_receipt_time
			FROM pg_subscription s
			LEFT JOIN pg_stat_subscription st ON st.subname = s.subname
			WHERE s.subdbid = (SELECT oid FROM pg_database WHERE datname = current_database())
			ORDER BY s.subname`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		var subs []*pgSubscription
		for rows.Next() {
			s := &pgSubscription{}
			var pid sql.NullInt64
			var receivedLSN, latestEndLSN sql.NullString
			var lastMsg sql.NullTime
			if scanErr := rows.Scan(&s.Name, &s.Enabled, &pid, &receivedLSN, &latestEndLSN, &lastMsg); scanErr != nil {
				continue
			}
			if pid.Valid {
				v := pid.Int64
				s.PID = &v
			}
			s.ReceivedLSN = receivedLSN.String
			s.LatestEndLSN = latestEndLSN.String
			if lastMsg.Valid {
				t := lastMsg.Time
				s.LastMsgReceiptTime = &t
				lag := time.Since(t).Seconds()
				s.LagSeconds = &lag
			}
			subs = append(subs, s)
		}
		rows.Close()

		links := listReplicationLinksForTarget(connID)
		for _, sub := range subs {
			srcConnID, ok := links[sub.Name]
			if !ok {
				continue
			}
			if lag, lagErr := fetchSourceSlotLagBytes(r.Context(), srcConnID, sub.Name); lagErr == nil {
				sub.LagBytes = &lag
			}
		}

		json.NewEncoder(w).Encode(subs)
	}
}

type createSubscriptionRequest struct {
	TargetConnectionID int64    `json:"targetConnectionId"`
	SourceConnectionID int64    `json:"sourceConnectionId"`
	Name               string   `json:"name"`
	PublicationName    string   `json:"publicationName"`
	CopyData           *bool    `json:"copyData"`      // nil = Postgres default (true)
	TruncateFirst      bool     `json:"truncateFirst"` // wipe Tables on the target before the initial copy
	Tables             []string `json:"tables"`        // "schema.table" list to truncate when TruncateFirst is set
}

// CreateSubscription builds a CONNECTION string from the source connection's
// decrypted credentials and runs CREATE SUBSCRIPTION on the target
// connection — this is the one call in this file that actually starts
// replication traffic flowing (directly between the two Postgres servers,
// not through Nias); everything else here is either single-connection DDL
// or a read-only status query.
// POST /api/pg-replication/subscriptions
func CreateSubscription() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		username := r.Header.Get("X-Username")
		if username == "" {
			username = "anonymous"
		}
		var req createSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.Name)
		pubName := strings.TrimSpace(req.PublicationName)
		if name == "" || pubName == "" {
			http.Error(w, jsonError("name and publicationName are required"), http.StatusBadRequest)
			return
		}
		if req.TargetConnectionID == req.SourceConnectionID {
			http.Error(w, jsonError("source and target must be different connections"), http.StatusBadRequest)
			return
		}
		// Read on source (its credentials are being pulled to build the
		// CONNECTION string) and write on target (CREATE SUBSCRIPTION runs
		// there) — without both, a user holding only the app-level
		// pgreplication.manage permission could point a subscription at any
		// stored Postgres connection regardless of that connection's own
		// per-user grants.
		if !CheckReadPermission(r, req.SourceConnectionID) {
			http.Error(w, jsonError("permission denied on source connection"), http.StatusForbidden)
			return
		}
		if !CheckWritePermission(r, req.TargetConnectionID) {
			http.Error(w, jsonError("permission denied on target connection"), http.StatusForbidden)
			return
		}

		srcInfo, err := fetchPgConnInfo(req.SourceConnectionID)
		if err != nil {
			http.Error(w, jsonError("source connection: "+err.Error()), http.StatusBadRequest)
			return
		}
		targetDB, err := requirePostgresDB(req.TargetConnectionID)
		if err != nil {
			http.Error(w, jsonError("target connection: "+err.Error()), http.StatusBadGateway)
			return
		}

		// Postgres's initial data copy (copy_data=true) is a raw insert of every
		// source row — it has no upsert/merge behavior, so any table on the
		// target that already holds rows (e.g. from before this subscription
		// existed) makes the copy fail outright on the first duplicate key and
		// keeps failing on every retry. TruncateFirst clears those tables right
		// before CREATE SUBSCRIPTION runs so the copy lands on a clean table
		// instead of erroring forever in the background.
		if req.TruncateFirst && len(req.Tables) > 0 {
			quoted := make([]string, len(req.Tables))
			backupRefs := make([]string, len(req.Tables))
			backupTS := time.Now().Format("20060102150405")
			for i, t := range req.Tables {
				schema, table, err := splitSchemaTable(t)
				if err != nil {
					http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
					return
				}
				qualified := quoteIdentPG(schema) + "." + quoteIdentPG(table)
				quoted[i] = qualified
				// A row-for-row snapshot taken right before TRUNCATE — the only
				// way back if this table shouldn't have been wiped turns out to
				// be "restore from here," since TRUNCATE itself has no undo.
				backupTable := fmt.Sprintf("%s_bak_%s", table, backupTS)
				backupQualified := quoteIdentPG(schema) + "." + quoteIdentPG(backupTable)
				backupRefs[i] = schema + "." + backupTable
				if _, err := targetDB.ExecContext(r.Context(), "CREATE TABLE "+backupQualified+" AS SELECT * FROM "+qualified); err != nil {
					http.Error(w, jsonError(fmt.Sprintf("backing up %s before truncate: %v", t, err)), http.StatusInternalServerError)
					return
				}
			}
			if _, err := targetDB.ExecContext(r.Context(), "TRUNCATE TABLE "+strings.Join(quoted, ", ")); err != nil {
				http.Error(w, jsonError("truncating target tables before copy: "+err.Error()), http.StatusInternalServerError)
				return
			}
			var targetName string
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT name FROM connections WHERE id=?`), req.TargetConnectionID).Scan(&targetName)
			WriteFeatureAccessAudit(username, "pg_replication_truncate", targetName,
				fmt.Sprintf("Truncated %s before creating subscription %q (backup tables: %s)",
					strings.Join(req.Tables, ", "), name, strings.Join(backupRefs, ", ")))
		}

		connStr := pgConnInfoString(srcInfo)
		stmt := fmt.Sprintf("CREATE SUBSCRIPTION %s CONNECTION '%s' PUBLICATION %s",
			quoteIdentPG(name), escapePGLiteral(connStr), quoteIdentPG(pubName))
		if req.CopyData != nil && !*req.CopyData {
			stmt += " WITH (copy_data = false)"
		}

		if _, err := targetDB.ExecContext(r.Context(), stmt); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		if _, err := appdb.DB.Exec(
			appdb.ConvertQuery(`INSERT INTO pg_replication_links (source_connection_id, target_connection_id, publication_name, subscription_name, created_by) VALUES (?, ?, ?, ?, ?)`),
			req.SourceConnectionID, req.TargetConnectionID, pubName, name, username,
		); err != nil {
			// The subscription itself succeeded — a bookkeeping failure here
			// shouldn't be reported to the caller as the operation having
			// failed, just logged; worst case GET /links just won't show
			// this pairing until it's manually reconciled.
			log.Printf("pg_replication: failed to record link for subscription %s: %v", name, err)
		}

		json.NewEncoder(w).Encode(map[string]string{"name": name})
	}
}

type alterSubscriptionRequest struct {
	Action string `json:"action"` // "enable" | "disable"
}

// AlterSubscriptionState enables or disables a subscription.
// PATCH /api/pg-replication/subscriptions/{name}?connection_id=X
func AlterSubscriptionState() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := strings.TrimPrefix(r.URL.Path, "/api/pg-replication/subscriptions/")
		connID, err := pgReplConnID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var req alterSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		var clause string
		switch req.Action {
		case "enable":
			clause = "ENABLE"
		case "disable":
			clause = "DISABLE"
		default:
			http.Error(w, jsonError(`action must be "enable" or "disable"`), http.StatusBadRequest)
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
		if _, err := db.ExecContext(r.Context(), "ALTER SUBSCRIPTION "+quoteIdentPG(name)+" "+clause); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": clause})
	}
}

// DropSubscription drops a subscription, removes its bookkeeping link, and —
// best-effort — drops the paired publication on the source too, as long as
// no other tracked link still references it (a publication can in principle
// be shared by more than one subscription, e.g. two different targets
// replicating from the same source) and the caller has write access on the
// source connection. Without this, every link ever created and dropped
// through this UI leaked one CREATE PUBLICATION object on the source
// forever, since nothing else in the app ever cleans those up.
// DELETE /api/pg-replication/subscriptions/{name}?connection_id=X
func DropSubscription() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := strings.TrimPrefix(r.URL.Path, "/api/pg-replication/subscriptions/")
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

		// Look up the bookkeeping link *before* dropping anything — DROP
		// SUBSCRIPTION alone only ever touches the target, so this is the
		// only place that still knows which source connection/publication
		// this subscription was paired with.
		var srcConnID int64
		var pubName string
		linkErr := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT source_connection_id, publication_name FROM pg_replication_links WHERE target_connection_id=? AND subscription_name=?`),
			connID, name,
		).Scan(&srcConnID, &pubName)

		if _, err := db.ExecContext(r.Context(), "DROP SUBSCRIPTION "+quoteIdentPG(name)); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM pg_replication_links WHERE target_connection_id=? AND subscription_name=?`), connID, name)

		if linkErr == nil {
			var stillReferenced int
			appdb.DB.QueryRow(appdb.ConvertQuery(
				`SELECT COUNT(*) FROM pg_replication_links WHERE source_connection_id=? AND publication_name=?`),
				srcConnID, pubName,
			).Scan(&stillReferenced)
			if stillReferenced == 0 {
				if !CheckWritePermission(r, srcConnID) {
					log.Printf("pg_replication: not dropping orphaned publication %s on connection %d — caller lacks write permission on the source", pubName, srcConnID)
				} else if srcDB, srcErr := requirePostgresDB(srcConnID); srcErr == nil {
					if _, dropErr := srcDB.ExecContext(r.Context(), "DROP PUBLICATION IF EXISTS "+quoteIdentPG(pubName)); dropErr != nil {
						log.Printf("pg_replication: failed to drop orphaned publication %s on connection %d: %v", pubName, srcConnID, dropErr)
					}
				}
			}
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "dropped"})
	}
}

// ── Reconcile (non-destructive drift check + catch-up) ──────────────────
//
// Native logical replication's initial "copy_data" is a one-shot, all-or-
// nothing table copy — it has no notion of "the target already has some of
// these rows, just add what's missing." Reconcile fills that gap without
// ever deleting anything: it compares rows by primary key + a content hash,
// then only INSERTs rows the target is missing and UPDATEs rows whose
// content differs from the source. Rows that exist only on the target are
// left completely untouched — this is what makes it safe to run repeatedly
// (e.g. to catch up after a subscription was down, or before bidirectional
// replication exists at all) without risking data that only ever lived on
// one side.

// tableColumns returns schema.table's column names in declaration order.
func tableColumns(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s.%s not found", schema, table)
	}
	return cols, nil
}

// tablePrimaryKey returns schema.table's primary key column names, in key
// order — quote_ident($1)||'.'||quote_ident($2) lets Postgres do the
// identifier quoting safely instead of string-concatenating it ourselves.
func tablePrimaryKey(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = (quote_ident($1) || '.' || quote_ident($2))::regclass
		  AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func quoteIdentList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = quoteIdentPG(n)
	}
	return strings.Join(q, ", ")
}

// rowContentHashExpr builds the SQL expression used to fingerprint a row for
// drift comparison. It hashes an EXPLICIT column list — md5(ROW("a","b",…)::
// text) — instead of the bare whole-row cast md5(t::text). The whole-row cast
// serializes columns in each table's *physical* order, so the same logical
// table stored with a different column order on the two servers (a column
// dropped and re-added, or the table recreated) would fingerprint every row
// differently: Compare would report the entire table as drifted and Sync would
// duplicate all of it. Callers pass the same name-sorted column list (see
// matchedColumns) to both sides, so the fingerprint depends only on values,
// not on physical layout.
func rowContentHashExpr(cols []string) string {
	return "md5(ROW(" + quoteIdentList(cols) + ")::text)"
}

// errColumnMismatch (the two tables don't share the same columns) and
// errFormatMismatch (a no-PK compare found zero overlapping rows despite both
// sides having data) are the two "the caller's data, not our bug" outcomes of
// compare/reconcile. They're wrapped into the returned error so the HTTP layer
// can map them to a 4xx via httpStatusForCompareErr instead of a blanket 500.
var (
	errColumnMismatch = errors.New("column mismatch")
	errFormatMismatch = errors.New("row-format mismatch")
)

// httpStatusForCompareErr classifies a compare/reconcile error: a schema
// mismatch is a bad request, a suspected format mismatch is a conflict the
// caller must resolve, and anything else is a genuine server-side failure.
func httpStatusForCompareErr(err error) int {
	switch {
	case errors.Is(err, errColumnMismatch):
		return http.StatusBadRequest
	case errors.Is(err, errFormatMismatch):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// matchedColumns returns the columns of schema.table sorted by name, but only
// after verifying both connections expose the exact same column *set*. A
// mismatch (a column present on one side and not the other) is returned as an
// error rather than silently producing rows that never fingerprint-match —
// that would look like total drift and, on Sync, mass-duplicate the table. The
// sort makes the order identical on both sides regardless of physical layout,
// which is what makes rowContentHashExpr comparable across the two servers.
func matchedColumns(ctx context.Context, srcDB, targetDB *sql.DB, schema, table string) ([]string, error) {
	srcCols, err := tableColumns(ctx, srcDB, schema, table)
	if err != nil {
		return nil, fmt.Errorf("reading source columns: %w", err)
	}
	targetCols, err := tableColumns(ctx, targetDB, schema, table)
	if err != nil {
		return nil, fmt.Errorf("reading target columns: %w", err)
	}
	srcSet := make(map[string]bool, len(srcCols))
	for _, c := range srcCols {
		srcSet[c] = true
	}
	targetSet := make(map[string]bool, len(targetCols))
	for _, c := range targetCols {
		targetSet[c] = true
	}
	var onlySource, onlyTarget []string
	for _, c := range srcCols {
		if !targetSet[c] {
			onlySource = append(onlySource, c)
		}
	}
	for _, c := range targetCols {
		if !srcSet[c] {
			onlyTarget = append(onlyTarget, c)
		}
	}
	if len(onlySource) > 0 || len(onlyTarget) > 0 {
		return nil, fmt.Errorf("%w, refusing to compare: source-only %v, target-only %v", errColumnMismatch, onlySource, onlyTarget)
	}
	sorted := append([]string(nil), srcCols...)
	sort.Strings(sorted)
	return sorted, nil
}

// keyString joins scanned primary-key values into a single map key —
// []byte and other driver-returned types are normalized via fmt.Sprint so
// the same logical value hashes the same way regardless of which of the two
// connections it was read from.
func keyString(vals []interface{}) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		if b, ok := v.([]byte); ok {
			parts[i] = string(b)
		} else {
			parts[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(parts, "\x1f")
}

// fetchKeyHashes reads (primary key -> content hash) for every row of
// schema.table — hashExpr fingerprints the row server-side (see
// rowContentHashExpr), so only a short hash per row crosses the wire instead
// of every column, keeping a compare cheap even on wide tables.
func fetchKeyHashes(ctx context.Context, db *sql.DB, schema, table string, pk []string, hashExpr string) (map[string]string, error) {
	qualified := quoteIdentPG(schema) + "." + quoteIdentPG(table)
	query := fmt.Sprintf("SELECT %s, %s FROM %s t", quoteIdentList(pk), hashExpr, qualified)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	pkVals := make([]interface{}, len(pk))
	pkPtrs := make([]interface{}, len(pk))
	for i := range pkVals {
		pkPtrs[i] = &pkVals[i]
	}
	var hash string
	scanDest := append(append([]interface{}{}, pkPtrs...), &hash)
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			return nil, err
		}
		result[keyString(pkVals)] = hash
	}
	return result, rows.Err()
}

// fetchRowHashCounts counts how many rows of each exact content a table has,
// for a table with no primary key — REPLICA IDENTITY FULL (Postgres's own
// fallback for PK-less tables) treats the entire row as its identity, so
// this does the same: the hash IS the key. Counting (a multiset) rather than
// just recording presence (a set) matters for tables that legitimately have
// duplicate rows — otherwise a target that already has one copy of some
// content would be wrongly treated as "has all copies" the source has.
// GROUP BY does the counting server-side, so only one row per distinct
// content crosses the wire rather than one per source row. hashExpr is the
// shared fingerprint expression (see rowContentHashExpr) so both sides count
// by an identical, physical-order-independent key.
func fetchRowHashCounts(ctx context.Context, db *sql.DB, schema, table, hashExpr string) (map[string]int, error) {
	qualified := quoteIdentPG(schema) + "." + quoteIdentPG(table)
	rows, err := db.QueryContext(ctx, "SELECT "+hashExpr+", count(*) FROM "+qualified+" t GROUP BY 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	var h string
	var n int
	for rows.Next() {
		if err := rows.Scan(&h, &n); err != nil {
			return nil, err
		}
		result[h] = n
	}
	return result, rows.Err()
}

type pgCompareResult struct {
	MissingOnTarget int  `json:"missing_on_target"`
	ExtraOnTarget   int  `json:"extra_on_target"`
	Differs         int  `json:"differs"`
	InSync          bool `json:"in_sync"`
	NoPrimaryKey    bool `json:"no_primary_key"`
	// FormatMismatchSuspected is set on a no-primary-key compare when both
	// tables hold rows but share not a single identical fingerprint. Real
	// drift almost never means *zero* rows match; total non-overlap instead
	// points at values that serialize to text differently on the two servers
	// (float precision, timestamp/timezone rendering, json whitespace) — in
	// which case the "missing" count is an illusion and Sync would duplicate
	// the whole table. Reconcile refuses in this state; the UI warns instead
	// of offering a one-click Sync.
	FormatMismatchSuspected bool `json:"format_mismatch_suspected"`
}

// countHashOverlap reports how many distinct fingerprints appear in both
// multisets. Zero overlap while both sides are non-empty is the signal that a
// no-primary-key compare is looking at a formatting mismatch rather than real
// drift (see pgCompareResult.FormatMismatchSuspected). An empty target is not
// suspicious — that is a legitimate first-time full copy.
func countHashOverlap(a, b map[string]int) int {
	overlap := 0
	for h := range a {
		if b[h] > 0 {
			overlap++
		}
	}
	return overlap
}

// ComparePgTables reports, without changing anything, how a target table
// has drifted from its source: rows the target is missing, rows the target
// has that the source doesn't (never touched by Reconcile), and rows whose
// content differs under the same primary key.
// GET /api/pg-replication/compare?source_connection_id=X&target_connection_id=Y&table=schema.table
func ComparePgTables() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		srcID, err := strconv.ParseInt(r.URL.Query().Get("source_connection_id"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("source_connection_id is required"), http.StatusBadRequest)
			return
		}
		targetID, err := strconv.ParseInt(r.URL.Query().Get("target_connection_id"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("target_connection_id is required"), http.StatusBadRequest)
			return
		}
		schema, table, err := splitSchemaTable(r.URL.Query().Get("table"))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, srcID) || !CheckReadPermission(r, targetID) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		srcDB, err := requirePostgresDB(srcID)
		if err != nil {
			http.Error(w, jsonError("source connection: "+err.Error()), http.StatusBadGateway)
			return
		}
		targetDB, err := requirePostgresDB(targetID)
		if err != nil {
			http.Error(w, jsonError("target connection: "+err.Error()), http.StatusBadGateway)
			return
		}
		result, err := comparePgTables(r.Context(), srcDB, targetDB, schema, table)
		if err != nil {
			http.Error(w, jsonError(err.Error()), httpStatusForCompareErr(err))
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// comparePgTables computes how the target table has drifted from the source
// without modifying anything. Errors wrap the sentinels above where relevant
// (see httpStatusForCompareErr).
func comparePgTables(ctx context.Context, srcDB, targetDB *sql.DB, schema, table string) (pgCompareResult, error) {
	hashCols, err := matchedColumns(ctx, srcDB, targetDB, schema, table)
	if err != nil {
		return pgCompareResult{}, err
	}
	hashExpr := rowContentHashExpr(hashCols)
	pk, err := tablePrimaryKey(ctx, targetDB, schema, table)
	if err != nil {
		return pgCompareResult{}, err
	}
	if len(pk) == 0 {
		srcCounts, err := fetchRowHashCounts(ctx, srcDB, schema, table, hashExpr)
		if err != nil {
			return pgCompareResult{}, fmt.Errorf("reading source: %w", err)
		}
		targetCounts, err := fetchRowHashCounts(ctx, targetDB, schema, table, hashExpr)
		if err != nil {
			return pgCompareResult{}, fmt.Errorf("reading target: %w", err)
		}
		result := pgCompareResult{NoPrimaryKey: true}
		for h, n := range srcCounts {
			if deficit := n - targetCounts[h]; deficit > 0 {
				result.MissingOnTarget += deficit
			}
		}
		for h, n := range targetCounts {
			if surplus := n - srcCounts[h]; surplus > 0 {
				result.ExtraOnTarget += surplus
			}
		}
		result.InSync = result.MissingOnTarget == 0
		if len(srcCounts) > 0 && len(targetCounts) > 0 && countHashOverlap(srcCounts, targetCounts) == 0 {
			result.FormatMismatchSuspected = true
		}
		return result, nil
	}
	srcHashes, err := fetchKeyHashes(ctx, srcDB, schema, table, pk, hashExpr)
	if err != nil {
		return pgCompareResult{}, fmt.Errorf("reading source: %w", err)
	}
	targetHashes, err := fetchKeyHashes(ctx, targetDB, schema, table, pk, hashExpr)
	if err != nil {
		return pgCompareResult{}, fmt.Errorf("reading target: %w", err)
	}
	var result pgCompareResult
	for k, srcHash := range srcHashes {
		targetHash, ok := targetHashes[k]
		if !ok {
			result.MissingOnTarget++
		} else if targetHash != srcHash {
			result.Differs++
		}
	}
	for k := range targetHashes {
		if _, ok := srcHashes[k]; !ok {
			result.ExtraOnTarget++
		}
	}
	result.InSync = result.MissingOnTarget == 0 && result.Differs == 0
	return result, nil
}

type pgReconcileResult struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
}

// reconcileNoPrimaryKey inserts source rows into a target table that has no
// primary key, matching by row content hash instead of a key column — see
// fetchRowHashCounts. This is a multiset comparison: if the source holds three
// identical copies of some content and the target holds one, two more are
// inserted so the counts match. It never deletes, so content the target has in
// surplus is left untouched. All inserts run inside a single transaction, so a
// mid-run failure rolls back rather than leaving the table half-reconciled.
// cols is the target's column list (INSERT order); hashExpr is the shared,
// physical-order-independent fingerprint (see rowContentHashExpr) used to
// match rows across the two servers.
func reconcileNoPrimaryKey(ctx context.Context, srcDB, targetDB *sql.DB, schema, table string, cols []string, hashExpr string) (pgReconcileResult, error) {
	var result pgReconcileResult
	targetCounts, err := fetchRowHashCounts(ctx, targetDB, schema, table, hashExpr)
	if err != nil {
		return result, fmt.Errorf("reading target: %w", err)
	}
	srcCounts, err := fetchRowHashCounts(ctx, srcDB, schema, table, hashExpr)
	if err != nil {
		return result, fmt.Errorf("reading source: %w", err)
	}
	// Both sides hold rows yet not one fingerprint matches — almost certainly a
	// value-formatting mismatch, not real drift (see FormatMismatchSuspected).
	// Inserting here would duplicate the entire source table, so refuse.
	if len(srcCounts) > 0 && len(targetCounts) > 0 && countHashOverlap(srcCounts, targetCounts) == 0 {
		return result, fmt.Errorf("%w: source and target share no matching rows despite both having data — likely a value-format difference (float precision, timestamp/timezone, json whitespace), not real drift", errFormatMismatch)
	}
	// How many more copies of each distinct row content the target needs —
	// computed once up front so streaming the source below can insert
	// exactly that many and no more, instead of either skipping every
	// duplicate outright (undercounts) or inserting every one unconditionally
	// (overcounts into actual duplication).
	needed := make(map[string]int, len(srcCounts))
	for h, n := range srcCounts {
		if deficit := n - targetCounts[h]; deficit > 0 {
			needed[h] = deficit
		}
	}

	qualified := quoteIdentPG(schema) + "." + quoteIdentPG(table)
	srcRows, err := srcDB.QueryContext(ctx, "SELECT "+quoteIdentList(cols)+", "+hashExpr+" FROM "+qualified+" t")
	if err != nil {
		return result, fmt.Errorf("reading source rows: %w", err)
	}
	defer srcRows.Close()

	tx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("starting transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualified, quoteIdentList(cols), placeholders(len(cols)))

	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var hash string
	ptrs[len(cols)] = &hash

	for srcRows.Next() {
		if err := srcRows.Scan(ptrs...); err != nil {
			return pgReconcileResult{}, fmt.Errorf("reading source rows: %w", err)
		}
		if needed[hash] <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, insertSQL, vals...); err != nil {
			return pgReconcileResult{}, fmt.Errorf("writing row: %w", err)
		}
		needed[hash]--
		result.Inserted++
	}
	if err := srcRows.Err(); err != nil {
		return pgReconcileResult{}, fmt.Errorf("reading source rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return pgReconcileResult{}, fmt.Errorf("committing: %w", err)
	}
	committed = true
	return result, nil
}

// ReconcilePgTable pulls a target table back into sync with its source
// without ever deleting anything: rows missing on the target are inserted,
// rows present under the same primary key but with different content are
// updated to match the source, and rows that only exist on the target are
// left exactly as they are. Safe to call repeatedly — already-matching rows
// are skipped, not rewritten.
// POST /api/pg-replication/reconcile { sourceConnectionId, targetConnectionId, table }
func ReconcilePgTable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			SourceConnectionID int64  `json:"sourceConnectionId"`
			TargetConnectionID int64  `json:"targetConnectionId"`
			Table              string `json:"table"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		schema, table, err := splitSchemaTable(req.Table)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, req.SourceConnectionID) {
			http.Error(w, jsonError("permission denied on source connection"), http.StatusForbidden)
			return
		}
		if !CheckWritePermission(r, req.TargetConnectionID) {
			http.Error(w, jsonError("permission denied on target connection"), http.StatusForbidden)
			return
		}
		srcDB, err := requirePostgresDB(req.SourceConnectionID)
		if err != nil {
			http.Error(w, jsonError("source connection: "+err.Error()), http.StatusBadGateway)
			return
		}
		targetDB, err := requirePostgresDB(req.TargetConnectionID)
		if err != nil {
			http.Error(w, jsonError("target connection: "+err.Error()), http.StatusBadGateway)
			return
		}

		result, noPK, err := reconcilePgTable(r.Context(), srcDB, targetDB, schema, table)
		if err != nil {
			http.Error(w, jsonError(err.Error()), httpStatusForCompareErr(err))
			return
		}
		detail := fmt.Sprintf("inserted %d, updated %d row(s) from connection %d into connection %d", result.Inserted, result.Updated, req.SourceConnectionID, req.TargetConnectionID)
		if noPK {
			detail = fmt.Sprintf("(no primary key, matched by row content) inserted %d row(s) from connection %d into connection %d", result.Inserted, req.SourceConnectionID, req.TargetConnectionID)
		}
		WriteFeatureAccessAudit(r.Header.Get("X-Username"), "pg_replication_reconcile", fmt.Sprintf("%s.%s", schema, table), detail)

		json.NewEncoder(w).Encode(result)
	}
}

// reconcilePgTable dispatches to the primary-key or no-primary-key reconcile
// path depending on whether the target table has a primary key, and reports
// which path ran (for audit wording). See reconcileByPrimaryKey and
// reconcileNoPrimaryKey.
func reconcilePgTable(ctx context.Context, srcDB, targetDB *sql.DB, schema, table string) (pgReconcileResult, bool, error) {
	hashCols, err := matchedColumns(ctx, srcDB, targetDB, schema, table)
	if err != nil {
		return pgReconcileResult{}, false, err
	}
	hashExpr := rowContentHashExpr(hashCols)
	cols, err := tableColumns(ctx, targetDB, schema, table)
	if err != nil {
		return pgReconcileResult{}, false, err
	}
	pk, err := tablePrimaryKey(ctx, targetDB, schema, table)
	if err != nil {
		return pgReconcileResult{}, false, err
	}
	if len(pk) == 0 {
		result, err := reconcileNoPrimaryKey(ctx, srcDB, targetDB, schema, table, cols, hashExpr)
		return result, true, err
	}
	result, err := reconcileByPrimaryKey(ctx, srcDB, targetDB, schema, table, pk, cols, hashExpr)
	return result, false, err
}

// reconcileByPrimaryKey upserts source rows into the target keyed by primary
// key: rows the target is missing are inserted, rows whose content fingerprint
// differs are updated to match the source, already-identical rows are skipped,
// and rows that exist only on the target are never touched. Every write runs
// inside one transaction, so a mid-run failure rolls the whole catch-up back
// rather than leaving the table partially updated. hashExpr is the shared,
// physical-order-independent fingerprint (see rowContentHashExpr).
func reconcileByPrimaryKey(ctx context.Context, srcDB, targetDB *sql.DB, schema, table string, pk, cols []string, hashExpr string) (pgReconcileResult, error) {
	var result pgReconcileResult
	targetHashes, err := fetchKeyHashes(ctx, targetDB, schema, table, pk, hashExpr)
	if err != nil {
		return result, fmt.Errorf("reading target: %w", err)
	}

	qualified := quoteIdentPG(schema) + "." + quoteIdentPG(table)
	srcRows, err := srcDB.QueryContext(ctx, "SELECT "+quoteIdentList(cols)+", "+hashExpr+" FROM "+qualified+" t")
	if err != nil {
		return result, fmt.Errorf("reading source rows: %w", err)
	}
	defer srcRows.Close()

	pkPos := make([]int, len(pk))
	for i, k := range pk {
		for j, c := range cols {
			if c == k {
				pkPos[i] = j
				break
			}
		}
	}

	conflictSet := make([]string, len(cols)-len(pk))
	{
		pkSet := make(map[string]bool, len(pk))
		for _, k := range pk {
			pkSet[k] = true
		}
		i := 0
		for _, c := range cols {
			if pkSet[c] {
				continue
			}
			conflictSet[i] = quoteIdentPG(c) + " = EXCLUDED." + quoteIdentPG(c)
			i++
		}
	}
	upsertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		qualified, quoteIdentList(cols), placeholders(len(cols)), quoteIdentList(pk), strings.Join(conflictSet, ", "))
	if len(conflictSet) == 0 {
		// A table whose only columns are the primary key — nothing to
		// update, so a plain "do nothing" avoids an empty SET clause.
		upsertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING",
			qualified, quoteIdentList(cols), placeholders(len(cols)), quoteIdentList(pk))
	}

	tx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("starting transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols)+1)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var hash string
	ptrs[len(cols)] = &hash

	for srcRows.Next() {
		if err := srcRows.Scan(ptrs...); err != nil {
			return pgReconcileResult{}, fmt.Errorf("reading source rows: %w", err)
		}
		keyVals := make([]interface{}, len(pk))
		for i, pos := range pkPos {
			keyVals[i] = vals[pos]
		}
		key := keyString(keyVals)
		targetHash, exists := targetHashes[key]
		if exists && targetHash == hash {
			continue // already identical — nothing to do
		}
		if _, err := tx.ExecContext(ctx, upsertSQL, vals...); err != nil {
			return pgReconcileResult{}, fmt.Errorf("writing row %v: %w", keyVals, err)
		}
		if exists {
			result.Updated++
		} else {
			result.Inserted++
		}
	}
	if err := srcRows.Err(); err != nil {
		return pgReconcileResult{}, fmt.Errorf("reading source rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return pgReconcileResult{}, fmt.Errorf("committing: %w", err)
	}
	committed = true
	return result, nil
}

func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(p, ", ")
}

// ── Links (bookkeeping listing) ─────────────────────────────────────────

type pgReplicationLink struct {
	ID               int64  `json:"id"`
	SourceConnID     int64  `json:"source_connection_id"`
	SourceConnName   string `json:"source_connection_name"`
	TargetConnID     int64  `json:"target_connection_id"`
	TargetConnName   string `json:"target_connection_name"`
	PublicationName  string `json:"publication_name"`
	SubscriptionName string `json:"subscription_name"`
	CreatedBy        string `json:"created_by"`
	CreatedAt        string `json:"created_at"`
}

// ListReplicationLinks returns every tracked publication/subscription pair
// with human-readable connection names joined in — the primary listing the
// frontend polls. Live status/lag comes separately from
// GET .../subscriptions?connection_id=<target> per link, not from this
// table (which only ever stores what was true at creation time).
// GET /api/pg-replication/links
func ListReplicationLinks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT l.id, l.source_connection_id, COALESCE(sc.name,''), l.target_connection_id, COALESCE(tc.name,''),
			       l.publication_name, l.subscription_name, l.created_by, l.created_at
			FROM pg_replication_links l
			LEFT JOIN connections sc ON sc.id = l.source_connection_id
			LEFT JOIN connections tc ON tc.id = l.target_connection_id
			ORDER BY l.id DESC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		links := []pgReplicationLink{}
		for rows.Next() {
			var l pgReplicationLink
			if scanErr := rows.Scan(&l.ID, &l.SourceConnID, &l.SourceConnName, &l.TargetConnID, &l.TargetConnName,
				&l.PublicationName, &l.SubscriptionName, &l.CreatedBy, &l.CreatedAt); scanErr != nil {
				continue
			}
			links = append(links, l)
		}
		json.NewEncoder(w).Encode(links)
	}
}
