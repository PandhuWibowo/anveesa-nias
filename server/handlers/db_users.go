package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

// ── Models ────────────────────────────────────────────────────────

type DBUser struct {
	Username    string `json:"username"`
	Host        string `json:"host,omitempty"` // MySQL/MariaDB only
	IsSuperuser bool   `json:"is_superuser"`
	CanCreateDB bool   `json:"can_create_db"`
	CanLogin    bool   `json:"can_login"`
}

// GrantEntry represents a set of privileges at a given scope.
// Level is one of: "global", "database", "schema", "table", "sequence", "function"
type GrantEntry struct {
	Level      string   `json:"level"`
	Database   string   `json:"database,omitempty"`
	Schema     string   `json:"schema,omitempty"`
	Table      string   `json:"table,omitempty"`   // reused for sequence/function name too
	Privileges []string `json:"privileges"`
}

// ── Privilege whitelists ──────────────────────────────────────────

var pgTablePrivileges = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"TRUNCATE": true, "REFERENCES": true, "TRIGGER": true,
}
var pgSchemaPrivileges = map[string]bool{"USAGE": true, "CREATE": true}
var pgDatabasePrivileges = map[string]bool{"CONNECT": true, "CREATE": true, "TEMP": true}
var pgSequencePrivileges = map[string]bool{"USAGE": true, "SELECT": true, "UPDATE": true}
var pgFunctionPrivileges = map[string]bool{"EXECUTE": true}

var mysqlPrivileges = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"CREATE": true, "DROP": true, "INDEX": true, "ALTER": true,
	"REFERENCES": true, "CREATE VIEW": true, "SHOW VIEW": true,
	"EXECUTE": true, "TRIGGER": true, "LOCK TABLES": true, "CREATE ROUTINE": true,
	"ALTER ROUTINE": true, "EVENT": true,
}

var dbIdentRe = regexp.MustCompile(`^[a-zA-Z0-9_@.\-%]+$`)

func validDBIdent(s string) bool {
	return s != "" && dbIdentRe.MatchString(s) && len(s) <= 128
}

func quoteIdentPG(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func escapePGLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func escapeMySQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

func filterAllowedPrivs(in []string, allowed map[string]bool) []string {
	var out []string
	for _, p := range in {
		up := strings.ToUpper(strings.TrimSpace(p))
		if allowed[up] {
			out = append(out, up)
		}
	}
	return out
}

func toPrivSet(privs []string) map[string]bool {
	m := make(map[string]bool)
	for _, p := range privs {
		m[strings.ToUpper(p)] = true
	}
	return m
}

func dbConnIDFromPath(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/connections/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("missing connection id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

func dbUsernameFromPath(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/connections/")
	parts := strings.Split(trimmed, "/")
	// parts: [connID, "db-users", username, ...]
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// ── List DB Users ─────────────────────────────────────────────────

func ListDBUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var users []DBUser

		switch driver {
		case "postgres":
			users, err = listPGUsers(r.Context(), db)
		case "mysql", "mariadb":
			users, err = listMySQLUsers(r.Context(), db)
		default:
			http.Error(w, jsonError("DB user management is only supported for PostgreSQL, MySQL, and MariaDB"), http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if users == nil {
			users = []DBUser{}
		}
		json.NewEncoder(w).Encode(users)
	}
}

func listPGUsers(ctx context.Context, db *sql.DB) ([]DBUser, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT usename, usesuper, usecreatedb
		FROM pg_user
		ORDER BY usename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []DBUser
	for rows.Next() {
		var u DBUser
		rows.Scan(&u.Username, &u.IsSuperuser, &u.CanCreateDB)
		u.CanLogin = true
		users = append(users, u)
	}
	return users, rows.Err()
}

func listMySQLUsers(ctx context.Context, db *sql.DB) ([]DBUser, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT User, Host,
		       IF(Super_priv='Y',1,0),
		       IF(Create_priv='Y',1,0)
		FROM mysql.user
		ORDER BY User, Host
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []DBUser
	for rows.Next() {
		var u DBUser
		var isSuper, createDB int
		rows.Scan(&u.Username, &u.Host, &isSuper, &createDB)
		u.IsSuperuser = isSuper == 1
		u.CanCreateDB = createDB == 1
		u.CanLogin = true
		users = append(users, u)
	}
	return users, rows.Err()
}

// ── Create DB User ────────────────────────────────────────────────

func CreateDBUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Host     string `json:"host"` // MySQL only, defaults to %
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Username == "" {
			http.Error(w, jsonError("username is required"), http.StatusBadRequest)
			return
		}
		if body.Password == "" {
			http.Error(w, jsonError("password is required"), http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch driver {
		case "postgres":
			stmt := fmt.Sprintf(
				"CREATE USER %s WITH LOGIN PASSWORD '%s'",
				quoteIdentPG(body.Username),
				escapePGLiteral(body.Password),
			)
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}

		case "mysql", "mariadb":
			host := body.Host
			if host == "" {
				host = "%"
			}
			stmt := fmt.Sprintf(
				"CREATE USER '%s'@'%s' IDENTIFIED BY '%s'",
				escapeMySQL(body.Username), escapeMySQL(host), escapeMySQL(body.Password),
			)
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			db.ExecContext(r.Context(), "FLUSH PRIVILEGES")

		default:
			http.Error(w, jsonError("unsupported driver"), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	}
}

// ── Drop DB User ──────────────────────────────────────────────────

func DropDBUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		username := dbUsernameFromPath(r.URL.Path)
		if username == "" {
			http.Error(w, jsonError("missing username"), http.StatusBadRequest)
			return
		}
		host := r.URL.Query().Get("host")

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch driver {
		case "postgres":
			stmt := fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdentPG(username))
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}

		case "mysql", "mariadb":
			if host == "" {
				host = "%"
			}
			stmt := fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", escapeMySQL(username), escapeMySQL(host))
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			db.ExecContext(r.Context(), "FLUSH PRIVILEGES")

		default:
			http.Error(w, jsonError("unsupported driver"), http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "dropped"})
	}
}

// ── Change Password ───────────────────────────────────────────────

func ChangeDBUserPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		username := dbUsernameFromPath(r.URL.Path)
		if username == "" {
			http.Error(w, jsonError("missing username"), http.StatusBadRequest)
			return
		}

		var body struct {
			Password string `json:"password"`
			Host     string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Password == "" {
			http.Error(w, jsonError("password is required"), http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch driver {
		case "postgres":
			stmt := fmt.Sprintf(
				"ALTER USER %s WITH PASSWORD '%s'",
				quoteIdentPG(username), escapePGLiteral(body.Password),
			)
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}

		case "mysql", "mariadb":
			host := body.Host
			if host == "" {
				host = "%"
			}
			stmt := fmt.Sprintf(
				"ALTER USER '%s'@'%s' IDENTIFIED BY '%s'",
				escapeMySQL(username), escapeMySQL(host), escapeMySQL(body.Password),
			)
			if _, err := db.ExecContext(r.Context(), stmt); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			db.ExecContext(r.Context(), "FLUSH PRIVILEGES")

		default:
			http.Error(w, jsonError("unsupported driver"), http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	}
}

// ── Get Grants ────────────────────────────────────────────────────

func GetDBUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		username := dbUsernameFromPath(r.URL.Path)
		if username == "" {
			http.Error(w, jsonError("missing username"), http.StatusBadRequest)
			return
		}
		host := r.URL.Query().Get("host")

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var grants []GrantEntry

		switch driver {
		case "postgres":
			grants, err = pgGetGrants(r.Context(), db, username)
		case "mysql", "mariadb":
			if host == "" {
				host = "%"
			}
			grants, err = mysqlGetGrants(r.Context(), db, username, host)
		default:
			http.Error(w, jsonError("unsupported driver"), http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if grants == nil {
			grants = []GrantEntry{}
		}
		json.NewEncoder(w).Encode(grants)
	}
}

func pgGetGrants(ctx context.Context, db *sql.DB, username string) ([]GrantEntry, error) {
	var grants []GrantEntry

	// 1. Database-level grants
	dbRows, err := db.QueryContext(ctx, `
		SELECT datname,
		       has_database_privilege($1, datname, 'CONNECT') as connect,
		       has_database_privilege($1, datname, 'CREATE')  as create_priv,
		       has_database_privilege($1, datname, 'TEMP')    as temp
		FROM pg_database
		WHERE NOT datistemplate
		ORDER BY datname
	`, username)
	if err == nil {
		defer dbRows.Close()
		for dbRows.Next() {
			var datname string
			var connect, createPriv, temp sql.NullBool
			dbRows.Scan(&datname, &connect, &createPriv, &temp)
			if datname == "" {
				continue
			}
			var privs []string
			if connect.Valid && connect.Bool {
				privs = append(privs, "CONNECT")
			}
			if createPriv.Valid && createPriv.Bool {
				privs = append(privs, "CREATE")
			}
			if temp.Valid && temp.Bool {
				privs = append(privs, "TEMP")
			}
			if len(privs) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "database",
					Database:   datname,
					Privileges: privs,
				})
			}
		}
	}

	// 2. Schema-level grants
	schemaRows, err := db.QueryContext(ctx, `
		SELECT nspname,
		       has_schema_privilege($1, nspname, 'USAGE')  as usage,
		       has_schema_privilege($1, nspname, 'CREATE') as create_priv
		FROM pg_namespace
		WHERE nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		  AND nspname NOT LIKE 'pg_temp_%'
		  AND nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY nspname
	`, username)
	if err == nil {
		defer schemaRows.Close()
		for schemaRows.Next() {
			var schemaName string
			var usage, createPriv sql.NullBool
			schemaRows.Scan(&schemaName, &usage, &createPriv)
			if schemaName == "" {
				continue
			}
			var privs []string
			if usage.Valid && usage.Bool {
				privs = append(privs, "USAGE")
			}
			if createPriv.Valid && createPriv.Bool {
				privs = append(privs, "CREATE")
			}
			if len(privs) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "schema",
					Schema:     schemaName,
					Privileges: privs,
				})
			}
		}
	}

	// 3. Table-level grants
	tableRows, err := db.QueryContext(ctx, `
		SELECT table_schema, table_name, array_to_string(array_agg(DISTINCT privilege_type ORDER BY privilege_type), ',') as privs
		FROM information_schema.role_table_grants
		WHERE grantee = $1
		GROUP BY table_schema, table_name
		ORDER BY table_schema, table_name
	`, username)
	if err == nil {
		defer tableRows.Close()
		for tableRows.Next() {
			var schema, table string
			var privsNull sql.NullString
			tableRows.Scan(&schema, &table, &privsNull)
			if !privsNull.Valid || privsNull.String == "" {
				continue
			}
			privsStr := privsNull.String
			var privs []string
			for _, p := range strings.Split(privsStr, ",") {
				if p = strings.TrimSpace(p); p != "" {
					privs = append(privs, p)
				}
			}
			if len(privs) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "table",
					Schema:     schema,
					Table:      table,
					Privileges: privs,
				})
			}
		}
	}

	// 4. Sequence-level grants
	seqRows, err := db.QueryContext(ctx, `
		SELECT table_schema, table_name, array_to_string(array_agg(DISTINCT privilege_type ORDER BY privilege_type), ',') as privs
		FROM information_schema.role_usage_grants
		WHERE grantee = $1
		  AND object_type = 'SEQUENCE'
		GROUP BY table_schema, table_name
		ORDER BY table_schema, table_name
	`, username)
	if err == nil {
		defer seqRows.Close()
		for seqRows.Next() {
			var schema, seqName string
			var privsNull sql.NullString
			seqRows.Scan(&schema, &seqName, &privsNull)
			if !privsNull.Valid || privsNull.String == "" {
				continue
			}
			var privs []string
			for _, p := range strings.Split(privsNull.String, ",") {
				if p = strings.TrimSpace(p); p != "" {
					privs = append(privs, p)
				}
			}
			if len(privs) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "sequence",
					Schema:     schema,
					Table:      seqName,
					Privileges: privs,
				})
			}
		}
	}

	// 5. Function/procedure grants
	fnRows, err := db.QueryContext(ctx, `
		SELECT routine_schema, routine_name, array_to_string(array_agg(DISTINCT privilege_type ORDER BY privilege_type), ',') as privs
		FROM information_schema.role_routine_grants
		WHERE grantee = $1
		GROUP BY routine_schema, routine_name
		ORDER BY routine_schema, routine_name
	`, username)
	if err == nil {
		defer fnRows.Close()
		for fnRows.Next() {
			var schema, fnName string
			var privsNull sql.NullString
			fnRows.Scan(&schema, &fnName, &privsNull)
			if !privsNull.Valid || privsNull.String == "" {
				continue
			}
			var privs []string
			for _, p := range strings.Split(privsNull.String, ",") {
				if p = strings.TrimSpace(p); p != "" {
					privs = append(privs, p)
				}
			}
			if len(privs) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "function",
					Schema:     schema,
					Table:      fnName,
					Privileges: privs,
				})
			}
		}
	}

	return grants, nil
}

func mysqlGetGrants(ctx context.Context, db *sql.DB, username, host string) ([]GrantEntry, error) {
	var grants []GrantEntry

	// 1. Global privileges from information_schema
	globalRow := db.QueryRowContext(ctx, `
		SELECT GROUP_CONCAT(PRIVILEGE_TYPE ORDER BY PRIVILEGE_TYPE SEPARATOR ',')
		FROM information_schema.USER_PRIVILEGES
		WHERE GRANTEE = CONCAT("'", ?, "'@'", ?, "'")
	`, username, host)
	var globalPrivsNullable sql.NullString
	globalRow.Scan(&globalPrivsNullable)
	if globalPrivsNullable.Valid && globalPrivsNullable.String != "" {
		privs := strings.Split(globalPrivsNullable.String, ",")
		// Filter out USAGE (it's the "no privilege" placeholder in MySQL)
		var filtered []string
		for _, p := range privs {
			p = strings.TrimSpace(p)
			if p != "" && p != "USAGE" {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) > 0 {
			grants = append(grants, GrantEntry{
				Level:      "global",
				Database:   "*",
				Table:      "*",
				Privileges: filtered,
			})
		}
	}

	// 2. Database-level privileges
	dbRows, err := db.QueryContext(ctx, `
		SELECT TABLE_SCHEMA, GROUP_CONCAT(PRIVILEGE_TYPE ORDER BY PRIVILEGE_TYPE SEPARATOR ',')
		FROM information_schema.SCHEMA_PRIVILEGES
		WHERE GRANTEE = CONCAT("'", ?, "'@'", ?, "'")
		GROUP BY TABLE_SCHEMA
		ORDER BY TABLE_SCHEMA
	`, username, host)
	if err == nil {
		defer dbRows.Close()
		for dbRows.Next() {
			var dbName string
			var privsNull sql.NullString
			dbRows.Scan(&dbName, &privsNull)
			if !privsNull.Valid || privsNull.String == "" {
				continue
			}
			var filtered []string
			for _, p := range strings.Split(privsNull.String, ",") {
				p = strings.TrimSpace(p)
				if p != "" && p != "USAGE" {
					filtered = append(filtered, p)
				}
			}
			if len(filtered) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "database",
					Database:   dbName,
					Privileges: filtered,
				})
			}
		}
	}

	// 3. Table-level privileges
	tblRows, err := db.QueryContext(ctx, `
		SELECT TABLE_SCHEMA, TABLE_NAME, GROUP_CONCAT(PRIVILEGE_TYPE ORDER BY PRIVILEGE_TYPE SEPARATOR ',')
		FROM information_schema.TABLE_PRIVILEGES
		WHERE GRANTEE = CONCAT("'", ?, "'@'", ?, "'")
		GROUP BY TABLE_SCHEMA, TABLE_NAME
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, username, host)
	if err == nil {
		defer tblRows.Close()
		for tblRows.Next() {
			var dbName, tblName string
			var privsNull sql.NullString
			tblRows.Scan(&dbName, &tblName, &privsNull)
			if !privsNull.Valid || privsNull.String == "" {
				continue
			}
			var filtered []string
			for _, p := range strings.Split(privsNull.String, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					filtered = append(filtered, p)
				}
			}
			if len(filtered) > 0 {
				grants = append(grants, GrantEntry{
					Level:      "table",
					Database:   dbName,
					Table:      tblName,
					Privileges: filtered,
				})
			}
		}
	}

	return grants, nil
}

// ── Apply Grants ──────────────────────────────────────────────────

func ApplyDBUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		username := dbUsernameFromPath(r.URL.Path)
		if username == "" {
			http.Error(w, jsonError("missing username"), http.StatusBadRequest)
			return
		}

		var body struct {
			Host   string       `json:"host"`
			Grants []GrantEntry `json:"grants"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch driver {
		case "postgres":
			if err := applyPGGrantsMultiDB(r.Context(), connID, db, username, body.Grants); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}

		case "mysql", "mariadb":
			host := body.Host
			if host == "" {
				host = "%"
			}
			if err := applyMySQLGrants(r.Context(), db, username, host, body.Grants); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}

		default:
			http.Error(w, jsonError("unsupported driver"), http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "applied"})
	}
}

// applyPGGrantsMultiDB groups grants by their target database and applies each
// group via a dedicated connection to that database.
func applyPGGrantsMultiDB(ctx context.Context, connID int64, defaultDB *sql.DB, username string, desired []GrantEntry) error {
	// Group by target database.
	// - "database" level grants have a Database field; they can be applied from any DB.
	// - "schema" and "table" level grants must be applied from within their target database.
	type dbGroup struct {
		targetDB string // empty = use defaultDB
		grants   []GrantEntry
	}
	groups := make(map[string][]GrantEntry)
	for _, g := range desired {
		var key string
		switch g.Level {
		case "schema", "table", "sequence", "function":
			key = g.Database // GRANT must run inside this database
		default:
			key = "" // database-level grants run from the current connection
		}
		groups[key] = append(groups[key], g)
	}

	for dbKey, grp := range groups {
		var execDB *sql.DB
		var tempConn bool
		if dbKey != "" {
			tc, _, err := openTempConn(connID, dbKey)
			if err != nil {
				return fmt.Errorf("cannot connect to database %q: %w", dbKey, err)
			}
			execDB = tc
			tempConn = true
		} else {
			execDB = defaultDB
		}

		if err := applyPGGrants(ctx, execDB, username, grp); err != nil {
			if tempConn {
				execDB.Close()
			}
			return err
		}
		if tempConn {
			execDB.Close()
		}
	}
	return nil
}

func applyPGGrants(ctx context.Context, db *sql.DB, username string, desired []GrantEntry) error {
	// Get current grants
	current, err := pgGetGrants(ctx, db, username)
	if err != nil {
		return err
	}

	// Build lookup maps
	type key struct{ level, database, schema, table string }
	currentMap := make(map[key]map[string]bool)
	for _, g := range current {
		k := key{g.Level, g.Database, g.Schema, g.Table}
		currentMap[k] = toPrivSet(g.Privileges)
	}
	desiredMap := make(map[key]map[string]bool)
	for _, g := range desired {
		k := key{g.Level, g.Database, g.Schema, g.Table}
		desiredMap[k] = toPrivSet(filterAllowedPrivs(g.Privileges,
			mergeMaps(pgTablePrivileges, pgSchemaPrivileges, pgDatabasePrivileges, pgSequencePrivileges, pgFunctionPrivileges)))
	}

	// Compute grants to add and revoke
	type change struct {
		grant  bool
		entry  GrantEntry
		privs  []string
	}
	var changes []change

	// All keys from both maps
	allKeys := make(map[key]bool)
	for k := range currentMap {
		allKeys[k] = true
	}
	for k := range desiredMap {
		allKeys[k] = true
	}

	for k := range allKeys {
		cur := currentMap[k]
		des := desiredMap[k]

		// Privs to grant (in desired but not current)
		var toGrant []string
		for p := range des {
			if !cur[p] {
				toGrant = append(toGrant, p)
			}
		}
		// Privs to revoke (in current but not desired)
		var toRevoke []string
		for p := range cur {
			if !des[p] {
				toRevoke = append(toRevoke, p)
			}
		}
		sort.Strings(toGrant)
		sort.Strings(toRevoke)

		entry := GrantEntry{Level: k.level, Database: k.database, Schema: k.schema, Table: k.table}

		if len(toGrant) > 0 {
			changes = append(changes, change{grant: true, entry: entry, privs: toGrant})
		}
		if len(toRevoke) > 0 {
			changes = append(changes, change{grant: false, entry: entry, privs: toRevoke})
		}
	}

	for _, c := range changes {
		stmt, err := buildPGGrantStmt(c.grant, username, c.entry, c.privs)
		if err != nil {
			continue // skip invalid entries
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute %q: %w", stmt, err)
		}
	}

	return nil
}

func buildPGGrantStmt(isGrant bool, username string, entry GrantEntry, privs []string) (string, error) {
	verb := "GRANT"
	prep := "TO"
	if !isGrant {
		verb = "REVOKE"
		prep = "FROM"
	}

	privsStr := strings.Join(privs, ", ")
	user := quoteIdentPG(username)

	switch entry.Level {
	case "database":
		allowed := filterAllowedPrivs(privs, pgDatabasePrivileges)
		if len(allowed) == 0 {
			return "", fmt.Errorf("no valid database privileges")
		}
		return fmt.Sprintf("%s %s ON DATABASE %s %s %s",
			verb, strings.Join(allowed, ", "),
			quoteIdentPG(entry.Database), prep, user), nil

	case "schema":
		allowed := filterAllowedPrivs(privs, pgSchemaPrivileges)
		if len(allowed) == 0 {
			return "", fmt.Errorf("no valid schema privileges")
		}
		return fmt.Sprintf("%s %s ON SCHEMA %s %s %s",
			verb, strings.Join(allowed, ", "),
			quoteIdentPG(entry.Schema), prep, user), nil

	case "table":
		allowed := filterAllowedPrivs(privs, pgTablePrivileges)
		if len(allowed) == 0 {
			return "", fmt.Errorf("no valid table privileges")
		}
		return fmt.Sprintf("%s %s ON TABLE %s.%s %s %s",
			verb, strings.Join(allowed, ", "),
			quoteIdentPG(entry.Schema), quoteIdentPG(entry.Table),
			prep, user), nil

	case "sequence":
		allowed := filterAllowedPrivs(privs, pgSequencePrivileges)
		if len(allowed) == 0 {
			return "", fmt.Errorf("no valid sequence privileges")
		}
		return fmt.Sprintf("%s %s ON SEQUENCE %s.%s %s %s",
			verb, strings.Join(allowed, ", "),
			quoteIdentPG(entry.Schema), quoteIdentPG(entry.Table),
			prep, user), nil

	case "function":
		allowed := filterAllowedPrivs(privs, pgFunctionPrivileges)
		if len(allowed) == 0 {
			return "", fmt.Errorf("no valid function privileges")
		}
		return fmt.Sprintf("%s %s ON FUNCTION %s.%s %s %s",
			verb, strings.Join(allowed, ", "),
			quoteIdentPG(entry.Schema), quoteIdentPG(entry.Table),
			prep, user), nil

	default:
		return "", fmt.Errorf("unknown level: %s", entry.Level)
	}

	_ = privsStr
	return "", fmt.Errorf("unhandled")
}

func applyMySQLGrants(ctx context.Context, db *sql.DB, username, host string, desired []GrantEntry) error {
	current, err := mysqlGetGrants(ctx, db, username, host)
	if err != nil {
		return err
	}

	type key struct{ level, database, table string }
	currentMap := make(map[key]map[string]bool)
	for _, g := range current {
		k := key{g.Level, g.Database, g.Table}
		currentMap[k] = toPrivSet(g.Privileges)
	}
	desiredMap := make(map[key]map[string]bool)
	for _, g := range desired {
		k := key{g.Level, g.Database, g.Table}
		desiredMap[k] = toPrivSet(filterAllowedPrivs(g.Privileges, mysqlPrivileges))
	}

	allKeys := make(map[key]bool)
	for k := range currentMap {
		allKeys[k] = true
	}
	for k := range desiredMap {
		allKeys[k] = true
	}

	userSpec := fmt.Sprintf("'%s'@'%s'", escapeMySQL(username), escapeMySQL(host))

	for k := range allKeys {
		cur := currentMap[k]
		des := desiredMap[k]

		var toGrant []string
		for p := range des {
			if !cur[p] {
				toGrant = append(toGrant, p)
			}
		}
		var toRevoke []string
		for p := range cur {
			if !des[p] {
				toRevoke = append(toRevoke, p)
			}
		}
		sort.Strings(toGrant)
		sort.Strings(toRevoke)

		target := mysqlGrantTarget(k.level, k.database, k.table)

		if len(toGrant) > 0 {
			stmt := fmt.Sprintf("GRANT %s ON %s TO %s", strings.Join(toGrant, ", "), target, userSpec)
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("GRANT failed: %w", err)
			}
		}
		if len(toRevoke) > 0 {
			stmt := fmt.Sprintf("REVOKE %s ON %s FROM %s", strings.Join(toRevoke, ", "), target, userSpec)
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("REVOKE failed: %w", err)
			}
		}
	}

	db.ExecContext(ctx, "FLUSH PRIVILEGES")
	return nil
}

func mysqlGrantTarget(level, database, table string) string {
	switch level {
	case "global":
		return "*.*"
	case "database":
		return fmt.Sprintf("`%s`.*", strings.ReplaceAll(database, "`", "``"))
	case "table":
		return fmt.Sprintf("`%s`.`%s`",
			strings.ReplaceAll(database, "`", "``"),
			strings.ReplaceAll(table, "`", "``"))
	}
	return "*.*"
}

func mergeMaps(maps ...map[string]bool) map[string]bool {
	out := make(map[string]bool)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// ── Temp cross-database connection ───────────────────────────────────
// openTempConn opens a short-lived *sql.DB to targetDB on the same server as
// connID. The caller MUST call db.Close() when done.
func openTempConn(connID int64, targetDB string) (*sql.DB, string, error) {
	var in ConnectionInput
	var sslInt int
	var encPwd string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`),
		connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Username, &encPwd, &sslInt)
	if err != nil {
		return nil, "", fmt.Errorf("connection not found")
	}
	in.SSL = sslInt == 1
	pwd, err := decryptCredential(encPwd)
	if err != nil {
		return nil, "", fmt.Errorf("decryption error")
	}
	in.Password = pwd
	in.Database = targetDB

	dsn, err := buildDSN(in)
	if err != nil {
		return nil, "", err
	}
	goDriver := driverName(in.Driver)
	db, err := sql.Open(goDriver, dsn)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	return db, goDriver, nil
}

// ── List Databases for grant target picker ────────────────────────

func ListDBsForGrantPicker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var dbs []string
		switch driver {
		case "postgres":
			rows, _ := db.QueryContext(r.Context(), `
				SELECT datname FROM pg_database
				WHERE NOT datistemplate ORDER BY datname
			`)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var n string
					rows.Scan(&n)
					dbs = append(dbs, n)
				}
			}
		case "mysql", "mariadb":
			rows, _ := db.QueryContext(r.Context(), `SHOW DATABASES`)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var n string
					rows.Scan(&n)
					if n != "information_schema" && n != "performance_schema" && n != "sys" {
						dbs = append(dbs, n)
					}
				}
			}
		}

		if dbs == nil {
			dbs = []string{}
		}
		json.NewEncoder(w).Encode(dbs)
	}
}

// ── List Tables for grant target picker ───────────────────────────

func ListTablesForGrantPicker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		dbName := r.URL.Query().Get("db")
		schemaName := r.URL.Query().Get("schema")

		type tableRow struct {
			Schema string `json:"schema"`
			Table  string `json:"table"`
		}
		var tables []tableRow

		// For PostgreSQL: if a specific target database is requested, open a
		// temporary connection to that database (just like DBeaver does).
		// For MySQL: always use the pooled connection since it can cross-query.
		_, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch driver {
		case "postgres":
			sch := schemaName
			if sch == "" {
				sch = "public"
			}
			// Use a temp connection to the target database so we see its tables
			queryDB, _, err := func() (*sql.DB, string, error) {
				if dbName != "" {
					return openTempConn(connID, dbName)
				}
				db, drv, e := GetDB(connID)
				return db, drv, e
			}()
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			// Only close if it's a temp connection (not the pooled one)
			if dbName != "" {
				defer queryDB.Close()
			}
			rows, _ := queryDB.QueryContext(r.Context(), `
				SELECT table_schema, table_name
				FROM information_schema.tables
				WHERE table_schema = $1
				  AND table_type = 'BASE TABLE'
				ORDER BY table_name
			`, sch)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var t tableRow
					rows.Scan(&t.Schema, &t.Table)
					tables = append(tables, t)
				}
			}

		case "mysql", "mariadb":
			if dbName == "" {
				json.NewEncoder(w).Encode([]tableRow{})
				return
			}
			db, _, err := GetDB(connID)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			rows, _ := db.QueryContext(r.Context(), `
				SELECT TABLE_SCHEMA, TABLE_NAME
				FROM information_schema.TABLES
				WHERE TABLE_SCHEMA = ?
				  AND TABLE_TYPE = 'BASE TABLE'
				ORDER BY TABLE_NAME
			`, dbName)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var t tableRow
					rows.Scan(&t.Schema, &t.Table)
					tables = append(tables, t)
				}
			}
		}

		if tables == nil {
			tables = []tableRow{}
		}
		json.NewEncoder(w).Encode(tables)
	}
}

// ── PostgreSQL schema list ────────────────────────────────────────

func ListSchemasForGrantPicker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		// Optional: query schemas from a specific database on the same server
		targetDB := r.URL.Query().Get("db")

		_, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var schemas []string
		if driver == "postgres" {
			queryDB, _, err := func() (*sql.DB, string, error) {
				if targetDB != "" {
					return openTempConn(connID, targetDB)
				}
				return GetDB(connID)
			}()
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			if targetDB != "" {
				defer queryDB.Close()
			}
			rows, _ := queryDB.QueryContext(r.Context(), `
				SELECT nspname FROM pg_namespace
				WHERE nspname NOT IN ('information_schema','pg_catalog','pg_toast')
				  AND nspname NOT LIKE 'pg_temp_%'
				  AND nspname NOT LIKE 'pg_toast_temp_%'
				ORDER BY nspname
			`)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var n string
					rows.Scan(&n)
					schemas = append(schemas, n)
				}
			}
		}

		if schemas == nil {
			schemas = []string{}
		}
		json.NewEncoder(w).Encode(schemas)
	}
}

// ── List Sequences for grant picker ──────────────────────────────

func ListSequencesForGrantPicker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		dbName := r.URL.Query().Get("db")
		schemaName := r.URL.Query().Get("schema")

		type row struct {
			Schema   string `json:"schema"`
			Sequence string `json:"sequence"`
		}
		var seqs []row

		queryDB, _, err := func() (*sql.DB, string, error) {
			if dbName != "" {
				return openTempConn(connID, dbName)
			}
			return GetDB(connID)
		}()
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if dbName != "" {
			defer queryDB.Close()
		}

		sch := schemaName
		if sch == "" {
			sch = "public"
		}
		rows, _ := queryDB.QueryContext(r.Context(), `
			SELECT sequence_schema, sequence_name
			FROM information_schema.sequences
			WHERE sequence_schema = $1
			ORDER BY sequence_name
		`, sch)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var s row
				rows.Scan(&s.Schema, &s.Sequence)
				seqs = append(seqs, s)
			}
		}

		if seqs == nil {
			seqs = []row{}
		}
		json.NewEncoder(w).Encode(seqs)
	}
}

// ── List Functions for grant picker ──────────────────────────────

func ListFunctionsForGrantPicker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := dbConnIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		dbName := r.URL.Query().Get("db")
		schemaName := r.URL.Query().Get("schema")

		type row struct {
			Schema   string `json:"schema"`
			Function string `json:"function"`
			Kind     string `json:"kind"` // FUNCTION or PROCEDURE
		}
		var fns []row

		queryDB, _, err := func() (*sql.DB, string, error) {
			if dbName != "" {
				return openTempConn(connID, dbName)
			}
			return GetDB(connID)
		}()
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if dbName != "" {
			defer queryDB.Close()
		}

		sch := schemaName
		if sch == "" {
			sch = "public"
		}
		rows, _ := queryDB.QueryContext(r.Context(), `
			SELECT routine_schema, routine_name,
			       CASE WHEN routine_type = 'PROCEDURE' THEN 'PROCEDURE' ELSE 'FUNCTION' END as kind
			FROM information_schema.routines
			WHERE routine_schema = $1
			  AND routine_type IN ('FUNCTION','PROCEDURE')
			ORDER BY routine_name
		`, sch)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var f row
				rows.Scan(&f.Schema, &f.Function, &f.Kind)
				fns = append(fns, f)
			}
		}

		if fns == nil {
			fns = []row{}
		}
		json.NewEncoder(w).Encode(fns)
	}
}
