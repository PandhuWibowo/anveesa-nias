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
			event_type  TEXT NOT NULL DEFAULT 'query_execution',
			action      TEXT NOT NULL DEFAULT '',
			target      TEXT NOT NULL DEFAULT '',
			details     TEXT NOT NULL DEFAULT '',
			username    TEXT NOT NULL DEFAULT '',
			conn_id     INTEGER,
			conn_name   TEXT NOT NULL DEFAULT '',
			sql         TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			row_count   INTEGER NOT NULL DEFAULT 0,
			error       TEXT DEFAULT '',
			executed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE audit_log ADD COLUMN event_type TEXT NOT NULL DEFAULT 'query_execution'`,
		`ALTER TABLE audit_log ADD COLUMN action TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE audit_log ADD COLUMN target TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE audit_log ADD COLUMN details TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(executed_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_type_time ON audit_log(event_type, executed_at DESC)`,
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
		`ALTER TABLE saved_queries ADD COLUMN user_id INTEGER DEFAULT NULL`,
		`ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT NULL`,
		`ALTER TABLE users ADD COLUMN totp_enabled INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN backup_codes TEXT DEFAULT NULL`,
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

		// ── Phase 1: Enhanced Database Operation Permissions ──
		`ALTER TABLE permissions ADD COLUMN db_permissions TEXT DEFAULT '["select","insert","update","delete","create","alter","drop"]'`,
		`ALTER TABLE permissions ADD COLUMN database_filter TEXT DEFAULT ''`,
		`ALTER TABLE permissions ADD COLUMN is_active INTEGER DEFAULT 1`,
		`ALTER TABLE connections ADD COLUMN environment TEXT DEFAULT 'development'`,

		// ── Phase 2: Access Groups (Folder Extensions) ──
		`ALTER TABLE connection_folders ADD COLUMN role_restrict TEXT DEFAULT ''`,
		`ALTER TABLE connection_folders ADD COLUMN is_active INTEGER DEFAULT 1`,

		// Folder members (group membership)
		`CREATE TABLE IF NOT EXISTS folder_members (
			folder_id INTEGER NOT NULL REFERENCES connection_folders(id) ON DELETE CASCADE,
			user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			PRIMARY KEY (folder_id, user_id)
		)`,

		// Folder connections (connections in folders with permissions)
		`CREATE TABLE IF NOT EXISTS folder_connections (
			folder_id   INTEGER NOT NULL REFERENCES connection_folders(id) ON DELETE CASCADE,
			conn_id     INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			permissions TEXT DEFAULT '["select","insert","update","delete","create","alter","drop"]',
			PRIMARY KEY (folder_id, conn_id)
		)`,

		// Direct user-connection assignments
		`CREATE TABLE IF NOT EXISTS user_connections (
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			conn_id     INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			permissions TEXT DEFAULT '["select","insert","update","delete","create","alter","drop"]',
			PRIMARY KEY (user_id, conn_id)
		)`,

		// ── Approval Workflows ──
		`CREATE TABLE IF NOT EXISTS approval_workflow (
			id                     INTEGER PRIMARY KEY AUTOINCREMENT,
			name                   TEXT NOT NULL UNIQUE,
			description            TEXT NOT NULL DEFAULT '',
			is_active              INTEGER NOT NULL DEFAULT 1,
			assign_all_groups      INTEGER NOT NULL DEFAULT 0,
			assign_all_connections INTEGER NOT NULL DEFAULT 0,
			created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_step (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			workflow_id        INTEGER NOT NULL REFERENCES approval_workflow(id) ON DELETE CASCADE,
			step_order         INTEGER NOT NULL DEFAULT 1,
			name               TEXT NOT NULL DEFAULT '',
			required_approvals INTEGER NOT NULL DEFAULT 1,
			created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS step_approver (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			step_id       INTEGER NOT NULL REFERENCES workflow_step(id) ON DELETE CASCADE,
			approver_type TEXT NOT NULL CHECK(approver_type IN ('role', 'user')),
			approver_id   INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_folder (
			workflow_id INTEGER NOT NULL REFERENCES approval_workflow(id) ON DELETE CASCADE,
			folder_id   INTEGER NOT NULL REFERENCES connection_folders(id) ON DELETE CASCADE,
			PRIMARY KEY (workflow_id, folder_id)
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_connection (
			workflow_id INTEGER NOT NULL REFERENCES approval_workflow(id) ON DELETE CASCADE,
			conn_id     INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			PRIMARY KEY (workflow_id, conn_id)
		)`,
		`CREATE TABLE IF NOT EXISTS query_approval_request (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			title        TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			conn_id      INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name TEXT NOT NULL DEFAULT '',
			statement    TEXT NOT NULL,
			status       TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','pending_review','approved','rejected','executing','done','failed')),
			creator_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			reviewer_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			review_note  TEXT NOT NULL DEFAULT '',
			workflow_id  INTEGER NOT NULL REFERENCES approval_workflow(id) ON DELETE CASCADE,
			current_step INTEGER NOT NULL DEFAULT 0,
			revision     INTEGER NOT NULL DEFAULT 1,
			execute_error TEXT NOT NULL DEFAULT '',
			executed_at  DATETIME NULL,
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS query_approval (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL REFERENCES query_approval_request(id) ON DELETE CASCADE,
			step_id    INTEGER NOT NULL REFERENCES workflow_step(id),
			revision   INTEGER NOT NULL DEFAULT 1,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username   TEXT NOT NULL DEFAULT '',
			action     TEXT NOT NULL CHECK(action IN ('approved', 'rejected')),
			note       TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_query_approval_request_status ON query_approval_request(status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_query_approval_request_conn ON query_approval_request(conn_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_query_approval_request_creator ON query_approval_request(creator_id, created_at DESC)`,

		// ── Phase 3: Application-Level Role Permissions ──
		`CREATE TABLE IF NOT EXISTS roles (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT UNIQUE NOT NULL,
			description TEXT DEFAULT '',
			permissions TEXT DEFAULT '[]',
			is_system   INTEGER DEFAULT 0,
			is_active   INTEGER DEFAULT 1,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Seed system roles
		`INSERT OR IGNORE INTO roles (name, description, permissions, is_system) VALUES
			('admin', 'Full system access',
			 '["connections.view","connections.create","connections.edit","connections.delete","query.execute","schema.browse","audit.view","users.manage","folders.manage","roles.manage"]',
			 1),
			('user', 'Standard user access',
			 '["connections.view","query.execute","schema.browse"]',
			 1)`,

		// Add role_id and per-user permission overrides
		`ALTER TABLE users ADD COLUMN role_id INTEGER REFERENCES roles(id) DEFAULT 2`,
		`ALTER TABLE users ADD COLUMN permissions TEXT DEFAULT '[]'`,
		`ALTER TABLE users ADD COLUMN is_active INTEGER DEFAULT 1`,

		// Set existing users to active
		`UPDATE users SET is_active = 1 WHERE is_active IS NULL`,
		`UPDATE roles SET permissions = '["connections.view","connections.create","connections.edit","connections.delete","query.execute","query.approve","schema.browse","audit.view","users.manage","folders.manage","roles.manage","workflows.manage"]' WHERE name = 'admin'`,
		`ALTER TABLE query_approval_request ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE query_approval ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
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
