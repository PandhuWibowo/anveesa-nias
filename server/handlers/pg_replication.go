package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

// quoteSchemaTable quotes a "schema.table" reference as two separately
// quoted identifiers (schema."table" style quoting is wrong; each part
// needs its own quotes) for use in CREATE PUBLICATION ... FOR TABLE.
func quoteSchemaTable(schemaTable string) (string, error) {
	parts := strings.SplitN(schemaTable, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid table reference %q — expected schema.table", schemaTable)
	}
	return quoteIdentPG(parts[0]) + "." + quoteIdentPG(parts[1]), nil
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
	TargetConnectionID int64  `json:"targetConnectionId"`
	SourceConnectionID int64  `json:"sourceConnectionId"`
	Name               string `json:"name"`
	PublicationName    string `json:"publicationName"`
	CopyData           *bool  `json:"copyData"` // nil = Postgres default (true)
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

// DropSubscription drops a subscription and removes its bookkeeping link.
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
		db, err := requirePostgresDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if _, err := db.ExecContext(r.Context(), "DROP SUBSCRIPTION "+quoteIdentPG(name)); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM pg_replication_links WHERE target_connection_id=? AND subscription_name=?`), connID, name)
		json.NewEncoder(w).Encode(map[string]string{"status": "dropped"})
	}
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
