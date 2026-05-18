package handlers

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

var allowedRestoreStatements = []string{
	"INSERT ", "CREATE TABLE", "CREATE INDEX", "CREATE UNIQUE INDEX",
	"DROP TABLE", "DROP INDEX", "ALTER TABLE", "SET ", "BEGIN", "COMMIT", "ROLLBACK", "DO ",
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

// RestoreBackup executes uploaded SQL statements.
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
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}
		if len(req.SQL) > 50*1024*1024 {
			http.Error(w, `{"error":"SQL too large (max 50MB)"}`, http.StatusBadRequest)
			return
		}

		db, _, err := GetDB(connID)
		if err != nil {
			http.Error(w, `{"error":"connection error"}`, http.StatusBadGateway)
			return
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, `{"error":"transaction error"}`, http.StatusInternalServerError)
			return
		}

		stmts := splitSQL(req.SQL)
		executed, skipped := 0, 0
		for _, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || strings.HasPrefix(stmt, "--") {
				continue
			}
			if !isAllowedRestoreStatement(stmt) {
				skipped++
				continue
			}
			if _, err := tx.ExecContext(r.Context(), stmt); err != nil {
				tx.Rollback()
				http.Error(w, `{"error":"execution error at statement `+strconv.Itoa(executed+1)+`"}`, http.StatusBadRequest)
				return
			}
			executed++
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, `{"error":"commit error"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "executed": executed, "skipped": skipped})
	}
}

// ── Core dump engine ──────────────────────────────────────────────────────────

func writeBackupDump(ctx context.Context, w io.Writer, db *sql.DB, driver, dbName string, opts BackupOptions) error {
	fmt.Fprintf(w, "-- Anveesa Nias Database Backup\n")
	fmt.Fprintf(w, "-- Driver: %s | Database: %s\n", driver, dbName)
	fmt.Fprintf(w, "-- Sections: %s\n", opts.Sections)
	fmt.Fprintf(w, "-- Generated: %s\n\n", time.Now().Format(time.RFC3339))

	schema := resolveSchema(driver, opts.Schema)

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

	// Pre-data: CREATE TABLE DDL
	if emitPreData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- PRE-DATA: schema definitions\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")
		if err := writePreData(ctx, w, db, driver, schema, tables, opts); err != nil {
			return err
		}
	}

	// Data: INSERT statements
	if emitData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- DATA: row inserts\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")
		if err := writeData(ctx, w, db, driver, tables, opts); err != nil {
			return err
		}
	}

	// Post-data: indexes, FK constraints
	if emitPostData {
		fmt.Fprintf(w, "-- ================================================================\n")
		fmt.Fprintf(w, "-- POST-DATA: indexes and constraints\n")
		fmt.Fprintf(w, "-- ================================================================\n\n")
		if err := writePostData(ctx, w, db, driver, schema, tables, opts); err != nil {
			return err
		}
	}

	// Views
	if opts.IncludeViews && (emitPreData || emitPostData) {
		if err := writeViews(ctx, w, db, driver, schema); err != nil {
			fmt.Fprintf(w, "-- Error writing views: %v\n\n", err)
		}
	}

	// Sequences (PG only)
	if opts.IncludeSequences && driver == "postgres" && emitPreData {
		if err := writePGSequences(ctx, w, db, schema); err != nil {
			fmt.Fprintf(w, "-- Error writing sequences: %v\n\n", err)
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

func writePreData(ctx context.Context, w io.Writer, db *sql.DB, driver, schema string, tables []string, opts BackupOptions) error {
	for _, tbl := range tables {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ddl, err := generateTableDDL(ctx, db, driver, schema, tbl, opts)
		if err != nil {
			fmt.Fprintf(w, "-- ERROR generating DDL for %s: %v\n\n", tbl, err)
			continue
		}
		fmt.Fprintln(w, ddl)
		fmt.Fprintln(w)
	}
	return nil
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
			return "", err
		}
		cols = append(cols, c)
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
			line += " DEFAULT " + c.defVal.String
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

func writeData(ctx context.Context, w io.Writer, db *sql.DB, driver string, tables []string, opts BackupOptions) error {
	for _, tbl := range tables {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		tblQ := quoteIdentForDriver(driver, "", tbl)
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
		rows, err := db.QueryContext(ctx,
			"SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE FROM INFORMATION_SCHEMA.STATISTICS "+
				"WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? "+
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
				"DO $$ BEGIN ALTER TABLE %q.%q ADD CONSTRAINT %q %s; EXCEPTION WHEN duplicate_object THEN NULL; END $$",
				schema, table, name, def))
		}
	case "mysql", "mariadb":
		rows, err := db.QueryContext(ctx,
			`SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
			 FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
			 WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL
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
		rows, err = db.QueryContext(ctx,
			`SELECT TABLE_NAME, VIEW_DEFINITION FROM INFORMATION_SCHEMA.VIEWS WHERE TABLE_SCHEMA=DATABASE()`)
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

// ── Sequences (PostgreSQL only) ───────────────────────────────────────────────

func writePGSequences(ctx context.Context, w io.Writer, db *sql.DB, schema string) error {
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx,
		`SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema=$1 ORDER BY sequence_name`, schema)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Fprintf(w, "-- ================================================================\n")
	fmt.Fprintf(w, "-- SEQUENCES\n")
	fmt.Fprintf(w, "-- ================================================================\n\n")

	for rows.Next() {
		var name string
		rows.Scan(&name)
		// Emit a basic CREATE SEQUENCE; current value would need nextval() call
		fmt.Fprintf(w, "CREATE SEQUENCE IF NOT EXISTS %q.%q;\n", schema, name)
	}
	fmt.Fprintln(w)
	return nil
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
		tableQ = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
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

func splitSQL(sql string) []string {
	var stmts []string
	var cur strings.Builder
	inStr := false
	for i, ch := range sql {
		if ch == '\'' && (i == 0 || sql[i-1] != '\\') {
			inStr = !inStr
		}
		if ch == ';' && !inStr {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			cur.Reset()
		} else {
			cur.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
