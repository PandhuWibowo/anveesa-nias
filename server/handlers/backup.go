package handlers

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Options ───────────────────────────────────────────────────────────────────

// BackupOptions mirrors pgAdmin's backup dialog settings.
type BackupOptions struct {
	// Sections — which parts to emit
	Sections string `json:"sections"` // "all" | "pre-data" | "data" | "post-data"

	// DDL / pre-data options
	DropExisting bool `json:"drop_existing"` // emit DROP TABLE IF EXISTS before CREATE
	IfNotExists  bool `json:"if_not_exists"` // use CREATE TABLE IF NOT EXISTS

	// Data options
	ColumnInsert    bool `json:"column_insert"`    // INSERT INTO t (c1,c2) VALUES (...)
	UseTransaction  bool `json:"use_transaction"`  // BEGIN/COMMIT per table
	DisableFKChecks bool `json:"disable_fk_checks"` // SET FOREIGN_KEY_CHECKS=0 wrapper

	// Post-data / extra DDL
	IncludeIndexes  bool `json:"include_indexes"`  // emit CREATE INDEX after data
	IncludeFKs      bool `json:"include_fks"`      // emit ADD CONSTRAINT … FOREIGN KEY
	IncludeViews    bool `json:"include_views"`    // emit CREATE VIEW definitions
	IncludeSequences bool `json:"include_sequences"` // emit CREATE SEQUENCE (PG only)
	IncludeTriggers bool `json:"include_triggers"` // emit CREATE TRIGGER (best-effort)

	// Output
	Compress bool `json:"compress"` // gzip the output (.sql.gz)

	// Filters
	Schema        string   `json:"schema"`         // target schema (default varies per driver)
	IncludeTables []string `json:"include_tables"` // if non-empty, only these tables
	ExcludeTables []string `json:"exclude_tables"` // always skip these tables
}

// DefaultBackupOptions returns sensible defaults matching pgAdmin's defaults.
func DefaultBackupOptions() BackupOptions {
	return BackupOptions{
		Sections:        "all",
		DropExisting:    false,
		IfNotExists:     false,
		ColumnInsert:    true,
		UseTransaction:  false,
		DisableFKChecks: true,
		IncludeIndexes:  true,
		IncludeFKs:      true,
		IncludeViews:    false,
		IncludeSequences: false,
		IncludeTriggers: false,
		Compress:        false,
	}
}

// backupOptionsFromQuery reads options from URL query params (for GET endpoint).
func backupOptionsFromQuery(r *http.Request) BackupOptions {
	q := r.URL.Query()
	boolQ := func(key string, def bool) bool {
		v := q.Get(key)
		if v == "" {
			return def
		}
		return v == "1" || v == "true"
	}
	strQ := func(key, def string) string {
		if v := q.Get(key); v != "" {
			return v
		}
		return def
	}
	opts := DefaultBackupOptions()
	opts.Sections = strQ("sections", opts.Sections)
	opts.DropExisting = boolQ("drop_existing", opts.DropExisting)
	opts.IfNotExists = boolQ("if_not_exists", opts.IfNotExists)
	opts.ColumnInsert = boolQ("column_insert", opts.ColumnInsert)
	opts.UseTransaction = boolQ("use_transaction", opts.UseTransaction)
	opts.DisableFKChecks = boolQ("disable_fk_checks", opts.DisableFKChecks)
	opts.IncludeIndexes = boolQ("include_indexes", opts.IncludeIndexes)
	opts.IncludeFKs = boolQ("include_fks", opts.IncludeFKs)
	opts.IncludeViews = boolQ("include_views", opts.IncludeViews)
	opts.IncludeSequences = boolQ("include_sequences", opts.IncludeSequences)
	opts.IncludeTriggers = boolQ("include_triggers", opts.IncludeTriggers)
	opts.Compress = boolQ("compress", opts.Compress)
	opts.Schema = strQ("schema", "")
	if inc := q.Get("include_tables"); inc != "" {
		for _, t := range strings.Split(inc, ",") {
			if s := strings.TrimSpace(t); s != "" {
				opts.IncludeTables = append(opts.IncludeTables, s)
			}
		}
	}
	if exc := q.Get("exclude_tables"); exc != "" {
		for _, t := range strings.Split(exc, ",") {
			if s := strings.TrimSpace(t); s != "" {
				opts.ExcludeTables = append(opts.ExcludeTables, s)
			}
		}
	}
	return opts
}

// ── Restore allow-list ────────────────────────────────────────────────────────

// allowedRestoreStatements must cover every statement shape writeBackupDump
// can emit (see generateTableDDL, writePGCreateSequences, writePGEnums,
// writePGSequenceReset, writeViews, fkDisable/EnableStatement) — otherwise a
// restore of the app's own backups silently drops statements a later one
// depends on (e.g. a CREATE TABLE ... DEFAULT nextval(seq) failing because the
// CREATE SEQUENCE before it was skipped).
var allowedRestoreStatements = []string{
	"INSERT ", "CREATE TABLE", "CREATE INDEX", "CREATE UNIQUE INDEX",
	"CREATE SEQUENCE", "CREATE TYPE", "CREATE OR REPLACE VIEW", "CREATE VIEW",
	"SELECT SETVAL(",
	"DROP TABLE", "DROP INDEX", "ALTER TABLE", "SET ", "BEGIN", "COMMIT", "ROLLBACK", "DO ",
	"EXEC SP_MSFOREACHTABLE",
}

func isAllowedRestoreStatement(stmt string) bool {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	for _, prefix := range allowedRestoreStatements {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// ── Restore progress labels ─────────────────────────────────────────────────
//
// describeStatement turns a raw SQL statement into a short human-readable
// label ("Inserting into orders") for live progress display — a running
// count alone doesn't tell an operator what's actually happening during a
// multi-minute restore. Best-effort only: an unrecognized shape falls back to
// a truncated statement preview rather than failing.

var (
	reInsertInto  = regexp.MustCompile(`(?is)^INSERT\s+INTO\s+([^\s(]+)`)
	reCreateTable = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(]+)`)
	reCreateIndex = regexp.MustCompile(`(?is)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?\S+\s+ON\s+([^\s(]+)`)
	reCreateSeq   = regexp.MustCompile(`(?is)^CREATE\s+SEQUENCE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)`)
	reCreateType  = regexp.MustCompile(`(?is)^CREATE\s+TYPE\s+(\S+)`)
	reCreateView  = regexp.MustCompile(`(?is)^CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+(\S+)`)
	reAlterTable  = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+([^\s(]+)`)
	reDropTable   = regexp.MustCompile(`(?is)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^\s(]+)`)
	reDropIndex   = regexp.MustCompile(`(?is)^DROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?([^\s(]+)`)
	reSetval      = regexp.MustCompile(`(?is)^SELECT\s+SETVAL\(\s*'([^']+)'`)
	reDoTable     = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+([^\s(]+)`)
)

// cleanIdent strips quoting/brackets and collapses `"schema"."table"` down to
// `schema.table` for a readable label.
func cleanIdent(s string) string {
	s = strings.ReplaceAll(s, `"."`, ".")
	s = strings.Trim(s, `"`+"`"+`[]`)
	return s
}

func describeStatement(stmt string) string {
	s := strings.TrimSpace(stmt)
	upper := strings.ToUpper(s)
	switch {
	case reInsertInto.MatchString(s):
		return "Inserting into " + cleanIdent(reInsertInto.FindStringSubmatch(s)[1])
	case reCreateTable.MatchString(s):
		return "Creating table " + cleanIdent(reCreateTable.FindStringSubmatch(s)[1])
	case reCreateIndex.MatchString(s):
		return "Indexing " + cleanIdent(reCreateIndex.FindStringSubmatch(s)[1])
	case reCreateSeq.MatchString(s):
		return "Creating sequence " + cleanIdent(reCreateSeq.FindStringSubmatch(s)[1])
	case reCreateType.MatchString(s):
		return "Creating type " + cleanIdent(reCreateType.FindStringSubmatch(s)[1])
	case reCreateView.MatchString(s):
		return "Creating view " + cleanIdent(reCreateView.FindStringSubmatch(s)[1])
	case reSetval.MatchString(s):
		return "Resetting sequence " + cleanIdent(reSetval.FindStringSubmatch(s)[1])
	case strings.HasPrefix(upper, "DO "):
		if m := reDoTable.FindStringSubmatch(s); m != nil {
			return "Repairing constraints on " + cleanIdent(m[1])
		}
		return "Applying constraint repair"
	case reDropTable.MatchString(s):
		return "Dropping table " + cleanIdent(reDropTable.FindStringSubmatch(s)[1])
	case reDropIndex.MatchString(s):
		return "Dropping index " + cleanIdent(reDropIndex.FindStringSubmatch(s)[1])
	case reAlterTable.MatchString(s):
		return "Altering table " + cleanIdent(reAlterTable.FindStringSubmatch(s)[1])
	case strings.HasPrefix(upper, "SET "):
		return "Configuring session"
	case strings.HasPrefix(upper, "BEGIN"):
		return "Starting transaction block"
	case strings.HasPrefix(upper, "COMMIT"):
		return "Committing"
	case strings.HasPrefix(upper, "EXEC"):
		return "Toggling foreign key checks"
	default:
		if len(s) > 48 {
			return s[:48] + "…"
		}
		return s
	}
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// GetBackup streams a SQL dump as a downloadable file.
// GET /api/connections/{id}/backup?database=name&sections=all&drop_existing=1…
func GetBackup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, connID) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}

		dbName := r.URL.Query().Get("database")
		if dbName != "" && !validIdentifier.MatchString(dbName) {
			http.Error(w, "invalid database name", http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, "connection error", http.StatusBadGateway)
			return
		}

		opts := backupOptionsFromQuery(r)
		ts := time.Now().Format("20060102_150405")
		if opts.Compress {
			filename := fmt.Sprintf("backup_%s_%s.sql.gz", dbName, ts)
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
			gz := gzip.NewWriter(w)
			defer gz.Close()
			if err := writeBackupDump(r.Context(), gz, db, driver, dbName, opts); err != nil {
				return
			}
		} else {
			filename := fmt.Sprintf("backup_%s_%s.sql", dbName, ts)
			w.Header().Set("Content-Type", "application/sql")
			w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
			if err := writeBackupDump(r.Context(), w, db, driver, dbName, opts); err != nil {
				return
			}
		}
	}
}

// ── Async restore job store ───────────────────────────────────────────────────
//
// Restores of real-world dumps can run for many minutes (multi-GB files,
// hundreds of thousands of statements). Running that synchronously inside one
// HTTP request is fragile — it's at the mercy of the server's WriteTimeout,
// reverse proxies, and the browser tab staying open — so instead the request
// just kicks off a background job (mirroring BackupToBucket's job pattern)
// and the frontend polls for live progress.

type RestoreJobStatus string

const (
	RestoreJobRunning  RestoreJobStatus = "running"
	RestoreJobDone     RestoreJobStatus = "done"
	RestoreJobFailed   RestoreJobStatus = "failed"
	RestoreJobCanceled RestoreJobStatus = "canceled"
)

type RestoreJob struct {
	ID        string           `json:"id"`
	Status    RestoreJobStatus `json:"status"`
	StartedAt time.Time        `json:"started_at"`
	DoneAt    *time.Time       `json:"done_at,omitempty"`

	// Executed/Skipped/FailedRows are updated live via atomic ops from the
	// streaming executor — read with atomic.LoadInt64, never under mu.
	Executed int64 `json:"executed"`
	Skipped  int64 `json:"skipped"`
	// FailedRows counts INSERTs that errored and were rolled back to a
	// per-statement savepoint instead of aborting the whole restore — only
	// nonzero when the caller opted into continueOnError.
	FailedRows int64 `json:"failed_rows"`

	// FirstRowError captures the first row-level error's text (e.g. "date/time
	// field value out of range") for diagnostics — mu-protected since it's a
	// plain string, not an atomic. Only the first is kept; a bad dump can
	// produce millions of identical failures and there's no value in storing
	// each one.
	FirstRowError string `json:"first_row_error,omitempty"`

	// Current/CurrentCount/Recent give a human-readable window into what the
	// executor is actually doing right now (e.g. "Inserting into orders"), not
	// just a running total — all mu-protected since they're plain values, not
	// atomics. Recent only gets a new entry when the label actually changes
	// (a bulk insert into one table can be the identical label for hundreds of
	// thousands of consecutive statements — repeating that line in a log is
	// noise, not information); CurrentCount tracks how many times the current
	// label has repeated in a row instead.
	Current      string   `json:"current,omitempty"`
	CurrentCount int64    `json:"current_count,omitempty"`
	Recent       []string `json:"recent,omitempty"`

	Error string `json:"error,omitempty"`

	cancel context.CancelFunc
	mu     sync.Mutex
}

var restoreJobs sync.Map // id → *RestoreJob

func getRestoreJob(id string) (*RestoreJob, bool) {
	v, ok := restoreJobs.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*RestoreJob), true
}

// RestoreBackup starts an async restore job and returns a job ID immediately.
// The actual download/decompress/execute runs in a background goroutine;
// callers poll GET /api/restore/jobs/:id for status.
// POST /api/connections/{id}/restore
func RestoreBackup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		if !CheckWritePermission(r, connID) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}
		role := r.Header.Get("X-User-Role")
		if role != "admin" && isAuthEnabled() {
			http.Error(w, `{"error":"admin access required for restore"}`, http.StatusForbidden)
			return
		}

		var req struct {
			SQL             string `json:"sql"`
			DestConnID      int64  `json:"dest_conn_id"`
			ObjectKey       string `json:"object_key"`
			SkipConflicts   bool   `json:"skip_conflicts"`
			ContinueOnError bool   `json:"continue_on_error"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		fromBucket := strings.TrimSpace(req.SQL) == "" && req.DestConnID != 0 && strings.TrimSpace(req.ObjectKey) != ""
		if !fromBucket && strings.TrimSpace(req.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}

		// The upload path posts SQL text inline as JSON from the browser, so it
		// stays tight. The bucket path streams the object straight through
		// (download → gunzip → statement executor) without ever buffering the
		// whole dump in memory, so multi-GB restores are fine there.
		const maxUploadBytes = 50 * 1024 * 1024
		if !fromBucket && len(req.SQL) > maxUploadBytes {
			http.Error(w, `{"error":"SQL too large (max 50MB, use a bucket source for larger dumps)"}`, http.StatusBadRequest)
			return
		}

		// Fail fast on bad inputs before committing to a background job: these
		// are quick metadata/pool lookups, not the slow part.
		var dest *bucketConnRow
		if fromBucket {
			dest, err = fetchBucketConn(req.DestConnID)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
		}
		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, `{"error":"connection error"}`, http.StatusBadGateway)
			return
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &RestoreJob{
			ID:        newJobID(),
			Status:    RestoreJobRunning,
			StartedAt: time.Now(),
			cancel:    jobCancel,
		}
		restoreJobs.Store(job.ID, job)

		go func() {
			defer jobCancel()

			var reader io.Reader
			var bodyCloser io.Closer
			var stepErr error

			if fromBucket {
				resp, err := openBucketObjectStream(jobCtx, dest, req.ObjectKey)
				if err != nil {
					stepErr = fmt.Errorf("failed to fetch object from bucket: %w", err)
				} else {
					bodyCloser = resp.Body
					reader = resp.Body
					if strings.HasSuffix(strings.ToLower(req.ObjectKey), ".gz") {
						gz, gzErr := gzip.NewReader(resp.Body)
						if gzErr != nil {
							resp.Body.Close()
							bodyCloser = nil
							stepErr = fmt.Errorf("failed to decompress object: %w", gzErr)
						} else {
							reader = gz
						}
					}
				}
			} else {
				reader = strings.NewReader(req.SQL)
			}

			onExec := func(stmt string) {
				label := describeStatement(stmt)
				job.mu.Lock()
				if label == job.Current {
					job.CurrentCount++
				} else {
					job.Current = label
					job.CurrentCount = 1
					job.Recent = append([]string{label}, job.Recent...)
					if len(job.Recent) > 8 {
						job.Recent = job.Recent[:8]
					}
				}
				job.mu.Unlock()
			}
			onFail := func(rowErr error) {
				job.mu.Lock()
				if job.FirstRowError == "" {
					job.FirstRowError = rowErr.Error()
				}
				job.mu.Unlock()
			}

			if stepErr == nil {
				tx, err := db.BeginTx(jobCtx, nil)
				if err != nil {
					stepErr = fmt.Errorf("transaction error: %w", err)
				} else {
					executed, _, execErr := execRestoreStream(jobCtx, tx, reader, driver, req.SkipConflicts, req.ContinueOnError, &job.Executed, &job.Skipped, &job.FailedRows, onExec, onFail)
					if execErr != nil {
						tx.Rollback()
						stepErr = fmt.Errorf("execution error at statement %d: %w", executed+1, execErr)
					} else if err := tx.Commit(); err != nil {
						stepErr = fmt.Errorf("commit error: %w", err)
					}
				}
			}
			if bodyCloser != nil {
				bodyCloser.Close()
			}

			now := time.Now()
			job.mu.Lock()
			defer job.mu.Unlock()
			job.DoneAt = &now
			if stepErr != nil {
				if jobCtx.Err() != nil {
					job.Status = RestoreJobCanceled
				} else {
					job.Status = RestoreJobFailed
					job.Error = stepErr.Error()
				}
				return
			}
			job.Status = RestoreJobDone
		}()

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
	}
}

// GetRestoreJobStatus returns the current status of a restore job.
// GET /api/restore/jobs/:id
func GetRestoreJobStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/restore/jobs/")
		job, ok := getRestoreJob(id)
		if !ok {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		job.mu.Lock()
		status, doneAt, errMsg := job.Status, job.DoneAt, job.Error
		current, currentCount, recent := job.Current, job.CurrentCount, job.Recent
		firstRowError := job.FirstRowError
		job.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":              job.ID,
			"status":          status,
			"started_at":      job.StartedAt,
			"done_at":         doneAt,
			"executed":        atomic.LoadInt64(&job.Executed),
			"skipped":         atomic.LoadInt64(&job.Skipped),
			"failed_rows":     atomic.LoadInt64(&job.FailedRows),
			"first_row_error": firstRowError,
			"current":         current,
			"current_count":   currentCount,
			"recent":          recent,
			"error":         errMsg,
		})
	}
}

// CancelRestoreJob cancels a running restore job. The in-flight transaction
// is rolled back (its statements were never committed), so cancelling is
// always safe.
// DELETE /api/restore/jobs/:id
func CancelRestoreJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/restore/jobs/")
		job, ok := getRestoreJob(id)
		if !ok {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		job.mu.Lock()
		if job.Status == RestoreJobRunning {
			job.cancel()
			job.Status = RestoreJobCanceled
		}
		job.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Core dump engine ──────────────────────────────────────────────────────────

func writeBackupDump(ctx context.Context, w io.Writer, db *sql.DB, driver, dbName string, opts BackupOptions) error {
	fmt.Fprintf(w, "-- Anveesa Nias Database Backup\n")
	fmt.Fprintf(w, "-- Driver: %s | Database: %s\n", driver, dbName)
	fmt.Fprintf(w, "-- Sections: %s\n", opts.Sections)
	fmt.Fprintf(w, "-- Generated: %s\n\n", time.Now().Format(time.RFC3339))

	schema := resolveSchema(driver, opts.Schema)
	// For MySQL/MariaDB, "schema" == the database name. Use the caller-supplied
	// dbName so that table listing, index, and FK queries target the right database
	// rather than whatever DATABASE() returns for the connection.
	if (driver == "mysql" || driver == "mariadb") && schema == "" && dbName != "" {
		schema = dbName
	}

	// Emit SET search_path for PostgreSQL so that unqualified names in FK
	// REFERENCES clauses and other DDL produced by pg_get_constraintdef resolve
	// correctly when the dump is restored in a fresh database.
	if driver == "postgres" && schema != "" {
		fmt.Fprintf(w, "SET search_path TO %q;\n\n", schema)
	}

	tables, err := listBackupTables(ctx, db, driver, schema, dbName, opts)
	if err != nil {
		return err
	}

	emitPreData := opts.Sections == "all" || opts.Sections == "pre-data"
	emitData := opts.Sections == "all" || opts.Sections == "data"
	emitPostData := opts.Sections == "all" || opts.Sections == "post-data"

	// Global FK disable wrapper
	if emitData && opts.DisableFKChecks {
		fmt.Fprintf(w, "%s\n\n", fkDisableStatement(driver))
	}

	// activeTables is the list used for data/post-data phases. When pre-data is
	// included we restrict it to tables whose DDL succeeded, keeping the dump
	// self-consistent (no INSERT without a preceding CREATE TABLE).
	activeTables := tables

	// Pre-data: CREATE TABLE DDL
	if emitPreData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- PRE-DATA: schema definitions\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")

		if driver == "postgres" {
			// Sequences must come before CREATE TABLE: DEFAULT nextval('seq') is
			// resolved at CREATE TABLE parse time, so the sequence must already exist.
			if err := writePGCreateSequences(ctx, w, db, schema); err != nil {
				fmt.Fprintf(w, "-- Error writing sequences: %v\n\n", err)
			}
			// ENUM types must also exist before tables that reference them.
			if err := writePGEnums(ctx, w, db, schema); err != nil {
				fmt.Fprintf(w, "-- Error writing enum types: %v\n\n", err)
			}
		}

		var err error
		activeTables, err = writePreData(ctx, w, db, driver, schema, tables, opts)
		if err != nil {
			return err
		}
	}

	// Data: INSERT statements
	if emitData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- DATA: row inserts\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")
		if err := writeData(ctx, w, db, driver, schema, activeTables, opts); err != nil {
			return err
		}

		// Reset sequences after data so nextval() continues from the correct value.
		if driver == "postgres" {
			if err := writePGSequenceReset(ctx, w, db, schema); err != nil {
				fmt.Fprintf(w, "-- Error resetting sequences: %v\n\n", err)
			}
		}
	}

	// Post-data: indexes, FK constraints
	if emitPostData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- POST-DATA: indexes and constraints\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")
		if err := writePostData(ctx, w, db, driver, schema, activeTables, opts); err != nil {
			return err
		}
	}

	// Views
	if opts.IncludeViews && (emitPreData || emitPostData) {
		if err := writeViews(ctx, w, db, driver, schema); err != nil {
			fmt.Fprintf(w, "-- Error writing views: %v\n\n", err)
		}
	}

	// Re-enable FK
	if emitData && opts.DisableFKChecks {
		fmt.Fprintf(w, "\n%s\n", fkEnableStatement(driver))
	}

	fmt.Fprintf(w, "\n-- End of dump\n")
	return nil
}

// ── Pre-data ──────────────────────────────────────────────────────────────────

// writePreData writes CREATE TABLE DDL for each table and returns the subset of
// tables whose DDL was successfully generated. Callers should use this returned
// slice for data/post-data phases so the dump stays self-consistent.
func writePreData(ctx context.Context, w io.Writer, db *sql.DB, driver, schema string, tables []string, opts BackupOptions) ([]string, error) {
	var ok []string
	for _, tbl := range tables {
		if ctx.Err() != nil {
			return ok, ctx.Err()
		}
		ddl, err := generateTableDDL(ctx, db, driver, schema, tbl, opts)
		if err != nil {
			fmt.Fprintf(w, "-- ERROR generating DDL for %s: %v\n\n", tbl, err)
			continue
		}
		fmt.Fprintln(w, ddl)
		fmt.Fprintln(w)
		ok = append(ok, tbl)
	}
	return ok, nil
}

func generateTableDDL(ctx context.Context, db *sql.DB, driver, schema, table string, opts BackupOptions) (string, error) {
	var sb strings.Builder

	if opts.DropExisting {
		dropKW := "DROP TABLE IF EXISTS"
		tblRef := quoteIdentForDriver(driver, schema, table)
		fmt.Fprintf(&sb, "%s %s;\n", dropKW, tblRef)
	}

	switch driver {
	case "mysql", "mariadb":
		return mysqlTableDDL(ctx, &sb, db, table, opts)
	case "sqlite":
		return sqliteTableDDL(ctx, &sb, db, table, opts)
	case "postgres":
		return pgTableDDL(ctx, &sb, db, schema, table, opts)
	default: // mssql + fallback
		return mssqlTableDDL(ctx, &sb, db, schema, table, opts)
	}
}

// MySQL / MariaDB: SHOW CREATE TABLE gives the full DDL.
func mysqlTableDDL(ctx context.Context, sb *strings.Builder, db *sql.DB, table string, opts BackupOptions) (string, error) {
	var tblName, createSQL string
	row := db.QueryRowContext(ctx, "SHOW CREATE TABLE `"+strings.ReplaceAll(table, "`", "``")+"`")
	if err := row.Scan(&tblName, &createSQL); err != nil {
		return "", err
	}
	if opts.IfNotExists && !strings.Contains(strings.ToUpper(createSQL), "IF NOT EXISTS") {
		createSQL = strings.Replace(createSQL, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)
	}
	sb.WriteString(createSQL)
	sb.WriteString(";")
	return sb.String(), nil
}

// SQLite: sql column in sqlite_master holds the original CREATE TABLE statement.
func sqliteTableDDL(ctx context.Context, sb *strings.Builder, db *sql.DB, table string, opts BackupOptions) (string, error) {
	var createSQL string
	row := db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table)
	if err := row.Scan(&createSQL); err != nil {
		return "", err
	}
	if opts.IfNotExists && !strings.Contains(strings.ToUpper(createSQL), "IF NOT EXISTS") {
		createSQL = strings.Replace(createSQL, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)
		createSQL = strings.Replace(createSQL, "CREATE TABLE IF NOT EXISTS IF NOT EXISTS", "CREATE TABLE IF NOT EXISTS", 1)
	}
	sb.WriteString(createSQL)
	sb.WriteString(";")
	return sb.String(), nil
}

// PostgreSQL: reconstruct CREATE TABLE from information_schema.
func pgTableDDL(ctx context.Context, sb *strings.Builder, db *sql.DB, schema, table string, opts BackupOptions) (string, error) {
	if schema == "" {
		schema = "public"
	}

	type colDef struct {
		name     string
		colType  string
		nullable string
		defVal   sql.NullString
	}
	rows, err := db.QueryContext(ctx, `
		SELECT column_name,
			CASE
				WHEN data_type IN ('character varying','varchar') AND character_maximum_length IS NOT NULL
					THEN 'varchar(' || character_maximum_length || ')'
				WHEN data_type IN ('character','char') AND character_maximum_length IS NOT NULL
					THEN 'char(' || character_maximum_length || ')'
				WHEN data_type = 'numeric' AND numeric_precision IS NOT NULL AND numeric_scale IS NOT NULL
					THEN 'numeric(' || numeric_precision || ',' || numeric_scale || ')'
				WHEN data_type = 'ARRAY'
					-- udt_name for arrays has a leading '_' (e.g. _text → text[])
					THEN CASE WHEN LEFT(udt_name, 1) = '_'
					          THEN SUBSTRING(udt_name FROM 2) || '[]'
					          ELSE udt_name || '[]'
					     END
				WHEN data_type = 'USER-DEFINED'
					-- enums, domains, composite types — use the actual type name
					THEN udt_name
				ELSE data_type
			END,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var cols []colDef
	for rows.Next() {
		var c colDef
		if err := rows.Scan(&c.name, &c.colType, &c.nullable, &c.defVal); err != nil {
			return "", fmt.Errorf("scanning columns for %s.%s: %w", schema, table, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterating columns for %s.%s: %w", schema, table, err)
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table not found: %s.%s", schema, table)
	}

	// Primary key columns
	pkRows, _ := db.QueryContext(ctx, `
		SELECT kc.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kc
			ON tc.constraint_name = kc.constraint_name AND tc.table_schema = kc.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY kc.ordinal_position`, schema, table)
	pkCols := map[string]bool{}
	if pkRows != nil {
		for pkRows.Next() {
			var col string
			pkRows.Scan(&col)
			pkCols[col] = true
		}
		pkRows.Close()
	}

	createKW := "CREATE TABLE"
	if opts.IfNotExists {
		createKW = "CREATE TABLE IF NOT EXISTS"
	}
	fmt.Fprintf(sb, "%s %q.%q (\n", createKW, schema, table)

	colLines := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		line := fmt.Sprintf("    %q %s", c.name, c.colType)
		if c.nullable == "NO" {
			line += " NOT NULL"
		}
		if c.defVal.Valid && c.defVal.String != "" {
			// Strip ::regclass so nextval('seq'::regclass) → nextval('seq').
			// PostgreSQL resolves an explicit ::regclass cast at CREATE TABLE parse
			// time, failing immediately if the sequence doesn't exist yet. With an
			// implicit text→regclass cast the lookup is deferred to call time, which
			// our explicit-value INSERTs never trigger.
			defStr := strings.ReplaceAll(c.defVal.String, "::regclass", "")
			line += " DEFAULT " + defStr
		}
		colLines = append(colLines, line)
	}

	// Inline PRIMARY KEY
	if len(pkCols) > 0 {
		pks := []string{}
		for _, c := range cols {
			if pkCols[c.name] {
				pks = append(pks, fmt.Sprintf("%q", c.name))
			}
		}
		colLines = append(colLines, "    PRIMARY KEY ("+strings.Join(pks, ", ")+")")
	}

	sb.WriteString(strings.Join(colLines, ",\n"))
	sb.WriteString("\n);")
	return sb.String(), nil
}

// MSSQL: reconstruct CREATE TABLE from INFORMATION_SCHEMA.
func mssqlTableDDL(ctx context.Context, sb *strings.Builder, db *sql.DB, schema, table string, opts BackupOptions) (string, error) {
	if schema == "" {
		schema = "dbo"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME,
			DATA_TYPE +
			CASE
				WHEN CHARACTER_MAXIMUM_LENGTH IS NOT NULL AND DATA_TYPE IN ('varchar','nvarchar','char','nchar')
					THEN '(' + CAST(CHARACTER_MAXIMUM_LENGTH AS VARCHAR) + ')'
				WHEN DATA_TYPE IN ('decimal','numeric') AND NUMERIC_PRECISION IS NOT NULL
					THEN '(' + CAST(NUMERIC_PRECISION AS VARCHAR) + ',' + CAST(NUMERIC_SCALE AS VARCHAR) + ')'
				ELSE ''
			END,
			IS_NULLABLE,
			COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type colDef struct {
		name, colType, nullable string
		defVal                  sql.NullString
	}
	var cols []colDef
	for rows.Next() {
		var c colDef
		rows.Scan(&c.name, &c.colType, &c.nullable, &c.defVal)
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table not found: %s.%s", schema, table)
	}

	createKW := "CREATE TABLE"
	if opts.IfNotExists {
		createKW = "IF NOT EXISTS (SELECT 1 FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME='" + table + "') CREATE TABLE"
	}
	fmt.Fprintf(sb, "%s [%s].[%s] (\n", createKW, schema, table)

	lines := make([]string, 0, len(cols))
	for _, c := range cols {
		line := fmt.Sprintf("    [%s] %s", c.name, c.colType)
		if c.nullable == "NO" {
			line += " NOT NULL"
		}
		if c.defVal.Valid && c.defVal.String != "" {
			line += " DEFAULT " + c.defVal.String
		}
		lines = append(lines, line)
	}
	sb.WriteString(strings.Join(lines, ",\n"))
	sb.WriteString("\n);")
	return sb.String(), nil
}

// ── Data ──────────────────────────────────────────────────────────────────────

func writeData(ctx context.Context, w io.Writer, db *sql.DB, driver, schema string, tables []string, opts BackupOptions) error {
	for _, tbl := range tables {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		tblQ := quoteIdentForDriver(driver, schema, tbl)
		fmt.Fprintf(w, "-- Table: %s\n", tbl)

		if opts.UseTransaction {
			fmt.Fprintf(w, "BEGIN;\n")
		}

		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s`, tblQ))
		if err != nil {
			fmt.Fprintf(w, "-- ERROR reading %s: %v\n\n", tbl, err)
			if opts.UseTransaction {
				fmt.Fprintf(w, "ROLLBACK;\n\n")
			}
			continue
		}

		cols, _ := rows.Columns()
		colQuoted := make([]string, len(cols))
		for i, c := range cols {
			colQuoted[i] = quoteIdentForDriver(driver, "", c)
		}

		rowCount := 0
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			sqlVals := make([]string, len(vals))
			for i, v := range vals {
				sqlVals[i] = sqlLiteral(v)
			}

			var stmt string
			if opts.ColumnInsert {
				stmt = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
					tblQ,
					strings.Join(colQuoted, ", "),
					strings.Join(sqlVals, ", "))
			} else {
				stmt = fmt.Sprintf("INSERT INTO %s VALUES (%s);",
					tblQ,
					strings.Join(sqlVals, ", "))
			}
			fmt.Fprintln(w, stmt)
			rowCount++
		}
		rows.Close()

		if opts.UseTransaction {
			fmt.Fprintf(w, "COMMIT;\n")
		}
		fmt.Fprintf(w, "-- %d rows dumped from %s\n\n", rowCount, tbl)
	}
	return nil
}

// ── Post-data ─────────────────────────────────────────────────────────────────

func writePostData(ctx context.Context, w io.Writer, db *sql.DB, driver, schema string, tables []string, opts BackupOptions) error {
	for _, tbl := range tables {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if opts.IncludeIndexes {
			idxStmts, err := generateIndexesDDL(ctx, db, driver, schema, tbl)
			if err == nil && len(idxStmts) > 0 {
				fmt.Fprintf(w, "-- Indexes for %s\n", tbl)
				for _, s := range idxStmts {
					fmt.Fprintln(w, s+";")
				}
				fmt.Fprintln(w)
			}
		}

		if driver == "postgres" {
			pkStmts, err := generatePKsDDL(ctx, db, schema, tbl)
			if err == nil && len(pkStmts) > 0 {
				fmt.Fprintf(w, "-- Primary keys for %s\n", tbl)
				for _, s := range pkStmts {
					fmt.Fprintln(w, s+";")
				}
				fmt.Fprintln(w)
			}
		}

		if opts.IncludeFKs && (driver == "postgres" || driver == "mysql" || driver == "mariadb") {
			fkStmts, err := generateFKsDDL(ctx, db, driver, schema, tbl)
			if err == nil && len(fkStmts) > 0 {
				fmt.Fprintf(w, "-- Foreign keys for %s\n", tbl)
				for _, s := range fkStmts {
					fmt.Fprintln(w, s+";")
				}
				fmt.Fprintln(w)
			}
		}
	}
	return nil
}

func generateIndexesDDL(ctx context.Context, db *sql.DB, driver, schema, table string) ([]string, error) {
	var stmts []string
	switch driver {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		rows, err := db.QueryContext(ctx,
			`SELECT indexname, indexdef FROM pg_indexes WHERE schemaname=$1 AND tablename=$2 AND indexname NOT IN (
				SELECT constraint_name FROM information_schema.table_constraints
				WHERE table_schema=$1 AND table_name=$2 AND constraint_type='PRIMARY KEY'
			)`, schema, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, def string
			rows.Scan(&name, &def)
			def = addIfNotExistsToIndex(def)
			stmts = append(stmts, def)
		}
	case "sqlite":
		rows, err := db.QueryContext(ctx,
			`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL`, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var def string
			rows.Scan(&def)
			stmts = append(stmts, def)
		}
	case "mysql", "mariadb":
		// SHOW CREATE TABLE already includes indexes; emit CREATE INDEX separately
		dbRef := "DATABASE()"
		if schema != "" {
			dbRef = "'" + strings.ReplaceAll(schema, "'", "''") + "'"
		}
		rows, err := db.QueryContext(ctx,
			"SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE FROM INFORMATION_SCHEMA.STATISTICS "+
				"WHERE TABLE_SCHEMA="+dbRef+" AND TABLE_NAME=? "+
				"AND INDEX_NAME != 'PRIMARY' ORDER BY INDEX_NAME, SEQ_IN_INDEX",
			table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type idxRow struct {
			name string
			col  string
			nonU int
		}
		byName := map[string][]idxRow{}
		var order []string
		for rows.Next() {
			var r idxRow
			rows.Scan(&r.name, &r.col, &r.nonU)
			if _, seen := byName[r.name]; !seen {
				order = append(order, r.name)
			}
			byName[r.name] = append(byName[r.name], r)
		}
		for _, name := range order {
			idxRows := byName[name]
			unique := ""
			if idxRows[0].nonU == 0 {
				unique = "UNIQUE "
			}
			cols := make([]string, len(idxRows))
			for i, r := range idxRows {
				cols[i] = "`" + r.col + "`"
			}
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX `%s` ON `%s` (%s)", unique, name, table, strings.Join(cols, ", ")))
		}
	}
	return stmts, nil
}

// generatePKsDDL emits ALTER TABLE … ADD PRIMARY KEY wrapped in a DO block so
// it is a no-op when the constraint already exists (duplicate_object) but still
// gets applied when the table was previously created without one.
func generatePKsDDL(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT kc.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kc
			ON tc.constraint_name = kc.constraint_name AND tc.table_schema = kc.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY kc.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			continue
		}
		cols = append(cols, fmt.Sprintf("%q", col))
	}
	if len(cols) == 0 {
		return nil, nil
	}

	stmt := fmt.Sprintf(
		"DO $$ BEGIN"+
			" IF to_regclass('%s.%s') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = '%s.%s'::regclass AND contype = 'p') THEN"+
			" ALTER TABLE %q.%q ADD PRIMARY KEY (%s);"+
			" END IF;"+
			" END $$",
		schema, table, schema, table, schema, table, strings.Join(cols, ", "))
	return []string{stmt}, nil
}

func generateFKsDDL(ctx context.Context, db *sql.DB, driver, schema, table string) ([]string, error) {
	var stmts []string
	switch driver {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		rows, err := db.QueryContext(ctx,
			`SELECT conname, pg_get_constraintdef(oid)
			 FROM pg_constraint
			 WHERE conrelid = ($1||'.'||$2)::regclass AND contype='f'`, schema, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, def string
			rows.Scan(&name, &def)
			stmts = append(stmts, fmt.Sprintf(
				"DO $$ BEGIN ALTER TABLE %q.%q ADD CONSTRAINT %q %s; EXCEPTION WHEN duplicate_object THEN NULL; WHEN SQLSTATE '42830' THEN NULL; END $$",
				schema, table, name, def))
		}
	case "mysql", "mariadb":
		fkDbRef := "DATABASE()"
		if schema != "" {
			fkDbRef = "'" + strings.ReplaceAll(schema, "'", "''") + "'"
		}
		rows, err := db.QueryContext(ctx,
			`SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
			 FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
			 WHERE TABLE_SCHEMA=`+fkDbRef+` AND TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL
			 ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type fkRow struct{ cname, col, refTbl, refCol string }
		byName := map[string][]fkRow{}
		var order []string
		for rows.Next() {
			var r fkRow
			rows.Scan(&r.cname, &r.col, &r.refTbl, &r.refCol)
			if _, seen := byName[r.cname]; !seen {
				order = append(order, r.cname)
			}
			byName[r.cname] = append(byName[r.cname], r)
		}
		for _, name := range order {
			fkRows := byName[name]
			cols := make([]string, len(fkRows))
			refCols := make([]string, len(fkRows))
			for i, r := range fkRows {
				cols[i] = "`" + r.col + "`"
				refCols[i] = "`" + r.refCol + "`"
			}
			refTbl := fkRows[0].refTbl
			stmts = append(stmts, fmt.Sprintf(
				"ALTER TABLE `%s` ADD CONSTRAINT `%s` FOREIGN KEY (%s) REFERENCES `%s` (%s)",
				table, name, strings.Join(cols, ", "), refTbl, strings.Join(refCols, ", ")))
		}
	}
	return stmts, nil
}

// ── Views ─────────────────────────────────────────────────────────────────────

func writeViews(ctx context.Context, w io.Writer, db *sql.DB, driver, schema string) error {
	var rows *sql.Rows
	var err error

	switch driver {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		rows, err = db.QueryContext(ctx,
			`SELECT table_name, view_definition FROM information_schema.views WHERE table_schema=$1`, schema)
	case "mysql", "mariadb":
		viewDbRef := "DATABASE()"
		if schema != "" {
			viewDbRef = "'" + strings.ReplaceAll(schema, "'", "''") + "'"
		}
		rows, err = db.QueryContext(ctx,
			`SELECT TABLE_NAME, VIEW_DEFINITION FROM INFORMATION_SCHEMA.VIEWS WHERE TABLE_SCHEMA=`+viewDbRef)
	case "sqlite":
		rows, err = db.QueryContext(ctx,
			`SELECT name, sql FROM sqlite_master WHERE type='view'`)
	default:
		return nil // mssql: skip for now
	}
	if err != nil || rows == nil {
		return err
	}
	defer rows.Close()

	fmt.Fprintf(w, "-- ================================================================\n")
	fmt.Fprintf(w, "-- VIEWS\n")
	fmt.Fprintf(w, "-- ================================================================\n\n")

	for rows.Next() {
		var name, def string
		rows.Scan(&name, &def)
		if driver == "sqlite" {
			fmt.Fprintf(w, "%s;\n\n", def)
		} else {
			fmt.Fprintf(w, "CREATE OR REPLACE VIEW %s AS\n%s;\n\n", quoteIdentForDriver(driver, schema, name), def)
		}
	}
	return nil
}

// ── Enums (PostgreSQL only) ───────────────────────────────────────────────────

// writePGEnums emits CREATE TYPE … AS ENUM statements for all enum types in the
// target schema. These must appear before CREATE TABLE because table columns can
// reference them, and pg_get_constraintdef output also uses unqualified type names.
func writePGEnums(ctx context.Context, w io.Writer, db *sql.DB, schema string) error {
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT t.typname, string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) AS labels
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		JOIN pg_enum e ON e.enumtypid = t.oid
		WHERE n.nspname = $1
		GROUP BY t.typname
		ORDER BY t.typname`, schema)
	if err != nil {
		return err
	}
	defer rows.Close()

	any := false
	for rows.Next() {
		var typeName, labels string
		if err := rows.Scan(&typeName, &labels); err != nil {
			continue
		}
		quoted := []string{}
		for _, lbl := range strings.Split(labels, ",") {
			quoted = append(quoted, "'"+strings.ReplaceAll(lbl, "'", "''")+"'")
		}
		fmt.Fprintf(w, "CREATE TYPE %q.%q AS ENUM (%s);\n", schema, typeName, strings.Join(quoted, ", "))
		any = true
	}
	if any {
		fmt.Fprintln(w)
	}
	return rows.Err()
}

// ── Sequences (PostgreSQL only) ───────────────────────────────────────────────

// writePGCreateSequences emits CREATE SEQUENCE statements. Must run BEFORE
// CREATE TABLE because DEFAULT nextval('seq') is resolved at parse time — if
// the sequence doesn't exist, the CREATE TABLE fails immediately.
func writePGCreateSequences(ctx context.Context, w io.Writer, db *sql.DB, schema string) error {
	if schema == "" {
		schema = "public"
	}
	// pg_sequences is available in PostgreSQL 10+.
	rows, err := db.QueryContext(ctx, `
		SELECT sequencename, increment, minimum_value, maximum_value, start_value, cache_size, cycle_option
		FROM pg_sequences WHERE schemaname = $1
		ORDER BY sequencename`, schema)
	if err != nil {
		// Fallback: basic CREATE SEQUENCE without parameters.
		return writePGSequencesFallback(ctx, w, db, schema)
	}
	defer rows.Close()

	any := false
	for rows.Next() {
		var name string
		var inc, minV, maxV, startV, cache int64
		var cycle bool
		if err := rows.Scan(&name, &inc, &minV, &maxV, &startV, &cache, &cycle); err != nil {
			continue
		}
		cycleKW := "NO CYCLE"
		if cycle {
			cycleKW = "CYCLE"
		}
		fmt.Fprintf(w,
			"CREATE SEQUENCE IF NOT EXISTS %q.%q INCREMENT BY %d MINVALUE %d MAXVALUE %d START WITH %d CACHE %d %s;\n",
			schema, name, inc, minV, maxV, startV, cache, cycleKW)
		any = true
	}
	if any {
		fmt.Fprintln(w)
	}
	return rows.Err()
}

// writePGSequencesFallback is used when pg_sequences is not available.
func writePGSequencesFallback(ctx context.Context, w io.Writer, db *sql.DB, schema string) error {
	rows, err := db.QueryContext(ctx,
		`SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema=$1 ORDER BY sequence_name`, schema)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		rows.Scan(&name)
		fmt.Fprintf(w, "CREATE SEQUENCE IF NOT EXISTS %q.%q;\n", schema, name)
	}
	fmt.Fprintln(w)
	return nil
}

// writePGSequenceReset emits SELECT setval(...) for all sequences so that
// after data is restored the sequences continue from the correct value.
func writePGSequenceReset(ctx context.Context, w io.Writer, db *sql.DB, schema string) error {
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT sequencename, last_value
		FROM pg_sequences WHERE schemaname = $1 AND last_value IS NOT NULL
		ORDER BY sequencename`, schema)
	if err != nil {
		return err
	}
	defer rows.Close()

	any := false
	for rows.Next() {
		var name string
		var lastVal int64
		if err := rows.Scan(&name, &lastVal); err != nil {
			continue
		}
		fmt.Fprintf(w, "SELECT setval('%s.%s', %d);\n", schema, name, lastVal)
		any = true
	}
	if any {
		fmt.Fprintln(w)
	}
	return rows.Err()
}

// ── Table list ────────────────────────────────────────────────────────────────

func listBackupTables(ctx context.Context, db *sql.DB, driver, schema, dbName string, opts BackupOptions) ([]string, error) {
	var tableQ string
	switch driver {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		tableQ = fmt.Sprintf(
			`SELECT table_name FROM information_schema.tables WHERE table_schema='%s' AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	case "mysql", "mariadb":
		dbRef := "DATABASE()"
		if schema != "" {
			dbRef = "'" + strings.ReplaceAll(schema, "'", "''") + "'"
		}
		tableQ = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=` + dbRef + ` AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
	case "sqlite":
		tableQ = `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	default:
		tableQ = `SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
	}

	rows, err := db.QueryContext(ctx, tableQ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		if tablePassesFilter(t, opts.IncludeTables, opts.ExcludeTables) {
			tables = append(tables, t)
		}
	}
	return tables, nil
}

func tablePassesFilter(tbl string, include, exclude []string) bool {
	tblLower := strings.ToLower(tbl)
	if len(exclude) > 0 {
		for _, ex := range exclude {
			if strings.ToLower(strings.TrimSpace(ex)) == tblLower {
				return false
			}
		}
	}
	if len(include) > 0 {
		for _, inc := range include {
			if strings.ToLower(strings.TrimSpace(inc)) == tblLower {
				return true
			}
		}
		return false
	}
	return true
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func resolveSchema(driver, schema string) string {
	if schema != "" {
		return schema
	}
	switch driver {
	case "postgres":
		return "public"
	case "mssql":
		return "dbo"
	default:
		return ""
	}
}

// quoteIdentForDriver quotes an identifier using the correct dialect.
// If schema is empty, just quote the table name.
func quoteIdentForDriver(driver, schema, name string) string {
	switch driver {
	case "mysql", "mariadb":
		esc := "`" + strings.ReplaceAll(name, "`", "``") + "`"
		if schema != "" {
			return "`" + strings.ReplaceAll(schema, "`", "``") + "`." + esc
		}
		return esc
	case "mssql", "sqlserver":
		esc := "[" + strings.ReplaceAll(name, "]", "]]") + "]"
		if schema != "" {
			return "[" + strings.ReplaceAll(schema, "]", "]]") + "]." + esc
		}
		return esc
	default: // postgres, sqlite
		esc := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		if schema != "" && driver == "postgres" {
			return `"` + strings.ReplaceAll(schema, `"`, `""`) + `".` + esc
		}
		return esc
	}
}

func fkDisableStatement(driver string) string {
	switch driver {
	case "mysql", "mariadb":
		return "SET FOREIGN_KEY_CHECKS=0;"
	case "postgres":
		return "SET session_replication_role = replica;"
	case "mssql", "sqlserver":
		return "EXEC sp_msforeachtable 'ALTER TABLE ? NOCHECK CONSTRAINT all';"
	default:
		return "-- FK checks not applicable for " + driver
	}
}

func fkEnableStatement(driver string) string {
	switch driver {
	case "mysql", "mariadb":
		return "SET FOREIGN_KEY_CHECKS=1;"
	case "postgres":
		return "SET session_replication_role = DEFAULT;"
	case "mssql", "sqlserver":
		return "EXEC sp_msforeachtable 'ALTER TABLE ? WITH CHECK CHECK CONSTRAINT all';"
	default:
		return ""
	}
}

func sqlLiteral(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case []byte:
		return "'" + strings.ReplaceAll(string(t), "'", "''") + "'"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case time.Time:
		return "'" + t.Format("2006-01-02 15:04:05.999999999Z07:00") + "'"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func addIfNotExistsToIndex(def string) string {
	upper := strings.ToUpper(def)
	if strings.HasPrefix(upper, "CREATE UNIQUE INDEX ") && !strings.Contains(upper, " IF NOT EXISTS ") {
		return "CREATE UNIQUE INDEX IF NOT EXISTS " + def[len("CREATE UNIQUE INDEX "):]
	}
	if strings.HasPrefix(upper, "CREATE INDEX ") && !strings.Contains(upper, " IF NOT EXISTS ") {
		return "CREATE INDEX IF NOT EXISTS " + def[len("CREATE INDEX "):]
	}
	return def
}

// maxRestoreStatementBytes bounds a single statement's buffered size — a
// defensive cap in case a dump is malformed (e.g. a missing terminating
// semicolon swallows the rest of the file into one "statement"), not a limit
// on overall dump size.
const maxRestoreStatementBytes = 512 * 1024 * 1024

// ── Skip-conflicts rewriting ─────────────────────────────────────────────────
//
// Re-running a restore against a target that already has the data fails on
// the first primary-key collision (by design — see execRestoreStream's single
// transaction). When the caller explicitly opts in to skipConflicts, these
// rewrite each statement to be a no-op on conflict instead of erroring, so a
// restore can be safely re-run.

// addStatementIfNotExists makes schema-creating statements idempotent even if
// the dump wasn't generated with the backup-time "IF NOT EXISTS" option —
// without this, CREATE TABLE would still fail immediately, before even
// reaching the INSERTs that addConflictSkip protects.
func addStatementIfNotExists(stmt string) string {
	upper := strings.ToUpper(stmt)
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE ") && !strings.Contains(upper, "IF NOT EXISTS"):
		return "CREATE TABLE IF NOT EXISTS " + stmt[len("CREATE TABLE "):]
	case strings.HasPrefix(upper, "CREATE SEQUENCE ") && !strings.Contains(upper, "IF NOT EXISTS"):
		return "CREATE SEQUENCE IF NOT EXISTS " + stmt[len("CREATE SEQUENCE "):]
	case strings.HasPrefix(upper, "CREATE UNIQUE INDEX ") || strings.HasPrefix(upper, "CREATE INDEX "):
		return addIfNotExistsToIndex(stmt)
	default:
		return stmt
	}
}

// addConflictSkip rewrites a plain INSERT into this driver's "ignore on
// conflict" form. Assumes the statement text is exactly what writeData
// generates ("INSERT INTO " uppercase) since restore only ever runs against
// this app's own dumps. MSSQL has no simple equivalent syntax, so it's left
// unmodified there — skip-conflicts only actually skips on the other drivers.
func addConflictSkip(stmt, driver string) string {
	const prefix = "INSERT INTO "
	if !strings.HasPrefix(stmt, prefix) {
		return stmt
	}
	switch driver {
	case "postgres":
		return stmt + " ON CONFLICT DO NOTHING"
	case "mysql", "mariadb":
		return "INSERT IGNORE INTO " + stmt[len(prefix):]
	case "sqlite":
		return "INSERT OR IGNORE INTO " + stmt[len(prefix):]
	default:
		return stmt
	}
}

// savepointStatements returns this driver's syntax for a named savepoint
// used to isolate a single risky statement within the larger restore
// transaction. MSSQL has no separate "release" step — a SAVE TRANSACTION
// marker just stays valid until the outer transaction ends — so release is
// returned empty there and callers should skip that step.
func savepointStatements(driver string) (save, rollbackTo, release string) {
	switch driver {
	case "mssql", "sqlserver":
		return "SAVE TRANSACTION restore_row", "ROLLBACK TRANSACTION restore_row", ""
	default: // postgres, mysql, mariadb, sqlite
		return "SAVEPOINT restore_row", "ROLLBACK TO SAVEPOINT restore_row", "RELEASE SAVEPOINT restore_row"
	}
}

// execRestoreStream reads SQL statements from r as they are parsed — buffering
// only the current statement, never the whole dump — and executes each
// allowed one against tx. This lets restores from bucket-hosted dumps scale to
// multi-GB files without holding the file (or its decompressed form) in
// memory.
//
// Comment lines (outside a string literal) are stripped per-line rather than
// checked only on the fully-accumulated statement: our own dumps always open
// with several "-- ..." header lines directly followed by the first real
// statement with no semicolon in between, so treating the whole
// semicolon-delimited chunk as "a comment if it starts with --" silently
// drops that first statement (notably `SET search_path`) along with the
// header.
// executedCounter/skippedCounter/failedCounter, if non-nil, are incremented
// atomically as statements complete — a live progress feed for a caller
// polling a job status endpoint on another goroutine, independent of the
// (executed, skipped int) totals returned at the end. onExec, if non-nil, is
// called with the raw statement text right after each successful execution,
// for callers that want a human-readable "what's happening right now" feed
// rather than just a running count. When skipConflicts is true, statements
// are rewritten (see addStatementIfNotExists/addConflictSkip) so re-running a
// restore against a target that already has the data no-ops instead of
// erroring. When continueOnError is true, INSERTs run inside a per-statement
// SAVEPOINT — a row that fails (bad data, e.g. an out-of-range date) is
// rolled back to that savepoint and counted as failed instead of aborting the
// whole (potentially hours-long, multi-million-statement) transaction; onFail,
// if non-nil, is called with each such error.
func execRestoreStream(ctx context.Context, tx *sql.Tx, r io.Reader, driver string, skipConflicts, continueOnError bool, executedCounter, skippedCounter, failedCounter *int64, onExec func(stmt string), onFail func(err error)) (executed, skipped int, err error) {
	br := bufio.NewReaderSize(r, 256*1024)
	var cur strings.Builder
	inStr := false
	// inDollar tracks PostgreSQL's "$$ ... $$" dollar-quoting (used by the PK/FK
	// repair DO-blocks this app's own dumps emit). Only the untagged "$$" form
	// is generated, so a bare toggle-on-"$$" is enough — no need to match a
	// custom $tag$. Without this, a semicolon INSIDE the DO block (terminating
	// one of its internal statements) is misread as ending the whole
	// statement, splitting it and leaving an "unterminated dollar-quoted
	// string" for Postgres to choke on.
	inDollar := false
	pendingDollar := false
	// pendingQuote defers the "is this `'` a close, or the first half of a
	// doubled `''` escape?" decision across a line-read boundary — needed
	// because a string literal's data can legitimately contain '\n'. A quote
	// inside a string is only ever resolved by looking at the NEXT character
	// (standard SQL: '' inside a string is one literal quote, not a close);
	// there is no backslash-escaping in this app's own dumps (sqlLiteral
	// doubles quotes), so backslashes in the data — e.g. JSON payloads like
	// "application\/json" — are just ordinary bytes and must never be treated
	// as an escape character for the surrounding quote.
	pendingQuote := false

	flush := func() error {
		stmt := strings.TrimSpace(cur.String())
		cur.Reset()
		if stmt == "" {
			return nil
		}
		if !isAllowedRestoreStatement(stmt) {
			skipped++
			if skippedCounter != nil {
				atomic.AddInt64(skippedCounter, 1)
			}
			return nil
		}
		if skipConflicts {
			stmt = addStatementIfNotExists(stmt)
			stmt = addConflictSkip(stmt, driver)
		}

		// Only INSERTs get the savepoint treatment: they're the high-volume,
		// data-dependent statements where a single malformed row shouldn't sink
		// an otherwise-good multi-million-row restore. A DDL statement failing
		// (bad CREATE TABLE, etc.) is a real problem worth aborting for.
		if continueOnError && strings.HasPrefix(stmt, "INSERT") {
			save, rollbackTo, release := savepointStatements(driver)
			if _, spErr := tx.ExecContext(ctx, save); spErr != nil {
				return spErr
			}
			if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
				if _, rbErr := tx.ExecContext(ctx, rollbackTo); rbErr != nil {
					return rbErr
				}
				if failedCounter != nil {
					atomic.AddInt64(failedCounter, 1)
				}
				if onFail != nil {
					onFail(execErr)
				}
				return nil
			}
			if release != "" {
				if _, relErr := tx.ExecContext(ctx, release); relErr != nil {
					return relErr
				}
			}
		} else if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			return execErr
		}

		executed++
		if executedCounter != nil {
			atomic.AddInt64(executedCounter, 1)
		}
		if onExec != nil {
			onExec(stmt)
		}
		return nil
	}

	for {
		line, readErr := br.ReadString('\n')
		if len(line) > 0 {
			// A "--" line comment only counts as one outside a string literal
			// or dollar-quoted block (state carried over from prior lines);
			// otherwise it's just literal content that happens to contain dashes.
			if !inStr && !inDollar && strings.HasPrefix(strings.TrimSpace(line), "--") {
				// nothing to do — the whole line is discarded
			} else {
				for i := 0; i < len(line); i++ {
					b := line[i]

					if pendingQuote {
						pendingQuote = false
						if b == '\'' {
							// doubled quote spanning the line boundary: one
							// literal `'` in the data, string stays open.
							cur.WriteByte(b)
							continue
						}
						// the deferred quote from the previous byte was the
						// real close; fall through and process b normally.
						inStr = false
					}

					if !inDollar && b == '\'' {
						if inStr {
							if i+1 < len(line) {
								if line[i+1] == '\'' {
									cur.WriteByte(b)
									cur.WriteByte(line[i+1])
									i++
									continue
								}
								inStr = false
								cur.WriteByte(b)
								continue
							}
							// last byte of this line — defer to the next line's first byte
							pendingQuote = true
							cur.WriteByte(b)
							continue
						}
						inStr = true
						cur.WriteByte(b)
						continue
					}

					if !inStr && !inDollar {
						if b == '$' {
							if pendingDollar {
								inDollar = !inDollar
								pendingDollar = false
							} else {
								pendingDollar = true
							}
						} else {
							pendingDollar = false
						}
					}

					if b == ';' && !inStr && !inDollar {
						if ferr := flush(); ferr != nil {
							return executed, skipped, ferr
						}
					} else {
						cur.WriteByte(b)
						if cur.Len() > maxRestoreStatementBytes {
							return executed, skipped, fmt.Errorf("single statement exceeds %dMB — dump may be missing a terminating semicolon", maxRestoreStatementBytes/(1024*1024))
						}
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return executed, skipped, readErr
		}
	}
	if err := flush(); err != nil {
		return executed, skipped, err
	}
	return executed, skipped, nil
}
