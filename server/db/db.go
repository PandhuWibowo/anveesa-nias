// Package db manages the internal SQLite store for connections and users.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}

	DB = db
	return migrate()
}

func migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			username   TEXT UNIQUE NOT NULL,
			password   TEXT NOT NULL,
			role       TEXT NOT NULL DEFAULT 'user',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS connections (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			driver     TEXT NOT NULL,
			host       TEXT,
			port       INTEGER,
			database   TEXT NOT NULL,
			username   TEXT,
			password   TEXT,
			ssl        INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS query_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			conn_id     INTEGER NOT NULL,
			sql         TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			row_count   INTEGER NOT NULL DEFAULT 0,
			error       TEXT,
			executed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_query_history_conn ON query_history(conn_id, executed_at DESC)`,
		`ALTER TABLE connections ADD COLUMN tags TEXT DEFAULT ''`,
		`ALTER TABLE connections ADD COLUMN ssh_host TEXT DEFAULT ''`,
		`ALTER TABLE connections ADD COLUMN ssh_port INTEGER DEFAULT 22`,
		`ALTER TABLE connections ADD COLUMN ssh_user TEXT DEFAULT ''`,
		`ALTER TABLE connections ADD COLUMN ssh_password TEXT DEFAULT ''`,
		`ALTER TABLE connections ADD COLUMN ssh_key TEXT DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			username    TEXT NOT NULL DEFAULT '',
			conn_id     INTEGER,
			conn_name   TEXT NOT NULL DEFAULT '',
			sql         TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			row_count   INTEGER NOT NULL DEFAULT 0,
			error       TEXT DEFAULT '',
			executed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(executed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS schedules (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL,
			conn_id         INTEGER NOT NULL,
			sql             TEXT NOT NULL,
			interval_min    INTEGER NOT NULL DEFAULT 60,
			alert_condition TEXT DEFAULT '',
			alert_threshold REAL DEFAULT 0,
			enabled         INTEGER NOT NULL DEFAULT 1,
			last_run_at     DATETIME,
			next_run_at     DATETIME,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_runs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_id INTEGER NOT NULL,
			row_count   INTEGER DEFAULT 0,
			error       TEXT DEFAULT '',
			alerted     INTEGER DEFAULT 0,
			ran_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			type       TEXT NOT NULL DEFAULT 'info',
			title      TEXT NOT NULL,
			message    TEXT NOT NULL,
			read       INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS row_changes (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			conn_id     INTEGER NOT NULL,
			database    TEXT DEFAULT '',
			table_name  TEXT NOT NULL,
			operation   TEXT NOT NULL,
			pk_column   TEXT DEFAULT '',
			pk_value    TEXT DEFAULT '',
			before_data TEXT DEFAULT '',
			after_data  TEXT DEFAULT '',
			username    TEXT DEFAULT '',
			changed_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_row_changes_table ON row_changes(conn_id, table_name)`,
		`CREATE TABLE IF NOT EXISTS permissions (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id  INTEGER NOT NULL,
			conn_id  INTEGER NOT NULL DEFAULT -1,
			level    TEXT NOT NULL DEFAULT 'readonly',
			UNIQUE(user_id, conn_id)
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS snippets (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			description TEXT DEFAULT '',
			sql         TEXT NOT NULL DEFAULT '',
			tags        TEXT DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS saved_queries (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			conn_id     INTEGER,
			sql         TEXT NOT NULL,
			description TEXT DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS connection_folders (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			parent_id   INTEGER DEFAULT NULL,
			owner_id    INTEGER DEFAULT 0,
			visibility  TEXT NOT NULL DEFAULT 'private',
			color       TEXT DEFAULT '#4f9cf9',
			sort_order  INTEGER DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE connections ADD COLUMN folder_id INTEGER DEFAULT NULL`,
		`ALTER TABLE connections ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared'`,
		`ALTER TABLE connections ADD COLUMN owner_id INTEGER DEFAULT 0`,
		`ALTER TABLE connection_folders ADD COLUMN sort_order INTEGER DEFAULT 0`,
		`ALTER TABLE connection_folders ADD COLUMN owner_id INTEGER DEFAULT 0`,
		`ALTER TABLE connection_folders ADD COLUMN parent_id INTEGER DEFAULT NULL`,
		`ALTER TABLE connection_folders ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'`,
		`ALTER TABLE connection_folders ADD COLUMN color TEXT DEFAULT '#4f9cf9'`,
	}
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			// ALTER TABLE ADD COLUMN is idempotent-ish; ignore "duplicate column" errors
			if !isAlterAdd(s) {
				return fmt.Errorf("migrate: %w", err)
			}
		}
	}
	return nil
}

func isAlterAdd(s string) bool {
	u := len(s)
	if u > 30 {
		u = 30
	}
	upper := s[:u]
	return len(upper) > 5 && upper[:5] == "ALTER"
}

// Ping checks if the database is accessible
func Ping() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	return DB.Ping()
}

// Close closes the database connection
func Close() error {
	if DB == nil {
		return nil
	}
	return DB.Close()
}
