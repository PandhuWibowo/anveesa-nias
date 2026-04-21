// Package db manages the internal database store for connections and users.
package db

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/anveesa/nias/config"
	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var DB *sql.DB
var dbDriver string

func Init(cfg *config.Config) error {
	var db *sql.DB
	var err error

	dbDriver = cfg.DBDriver

	switch cfg.DBDriver {
	case "postgres":
		// PostgreSQL (including AWS RDS PostgreSQL)
		db, err = sql.Open("postgres", cfg.DBURL)
		if err != nil {
			return fmt.Errorf("open postgres: %w", err)
		}

		// Configure connection pool for PostgreSQL
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(0)

		// Test connection
		if err := db.Ping(); err != nil {
			return fmt.Errorf("ping postgres: %w", err)
		}

	case "mysql":
		// MySQL/MariaDB (including AWS RDS MySQL/MariaDB)
		// Register TLS config if SSL is enabled
		if cfg.DBSSLMode != "disable" && cfg.DBSSLRootCert != "" {
			if err := registerMySQLTLS(cfg.DBSSLRootCert); err != nil {
				return fmt.Errorf("register MySQL TLS: %w", err)
			}
		}

		db, err = sql.Open("mysql", cfg.DBURL)
		if err != nil {
			return fmt.Errorf("open mysql: %w", err)
		}

		// Configure connection pool for MySQL
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(0)

		// Test connection
		if err := db.Ping(); err != nil {
			return fmt.Errorf("ping mysql: %w", err)
		}

	default:
		return fmt.Errorf("unsupported database driver: %s", cfg.DBDriver)
	}

	DB = db
	if err := migrate(); err != nil {
		return err
	}
	return seedDefaultAdmin()
}

// registerMySQLTLS registers TLS configuration for MySQL (for RDS SSL)
func registerMySQLTLS(certPath string) error {
	rootCertPool := x509.NewCertPool()
	pem, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read cert file: %w", err)
	}
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return fmt.Errorf("failed to append PEM")
	}

	return mysql.RegisterTLSConfig("custom", &tls.Config{
		RootCAs: rootCertPool,
	})
}

// IsPostgreSQL returns true if using PostgreSQL
func IsPostgreSQL() bool {
	return dbDriver == "postgres"
}

// IsMySQL returns true if using MySQL/MariaDB
func IsMySQL() bool {
	return dbDriver == "mysql"
}

// ConvertQuery converts SQLite ? placeholders to PostgreSQL/MySQL $1, $2, ... if needed
func ConvertQuery(query string) string {
	if !IsPostgreSQL() && !IsMySQL() {
		return query // SQLite uses ?, no conversion needed
	}

	// Handle INSERT OR IGNORE (SQLite-specific)
	hasInsertOrIgnore := strings.Contains(query, "INSERT OR IGNORE")
	if hasInsertOrIgnore {
		query = strings.Replace(query, "INSERT OR IGNORE", "INSERT", 1)
	}

	// Convert ? to $1, $2, $3, etc.
	result := ""
	paramCount := 1
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(query); i++ {
		ch := query[i]

		// Track if we're inside a string literal
		if (ch == '\'' || ch == '"') && (i == 0 || query[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
			}
		}

		// Only replace ? outside of string literals
		if ch == '?' && !inQuote {
			result += "$" + strconv.Itoa(paramCount)
			paramCount++
		} else {
			result += string(ch)
		}
	}

	// Add ON CONFLICT DO NOTHING for PostgreSQL/MySQL if original had INSERT OR IGNORE
	if hasInsertOrIgnore && (IsPostgreSQL() || IsMySQL()) {
		result += " ON CONFLICT DO NOTHING"
	}

	return result
}

// convertSQL converts SQLite SQL to PostgreSQL/MySQL SQL if needed
func convertSQL(stmt string) string {
	if IsPostgreSQL() {
		// PostgreSQL conversions
		stmt = strings.ReplaceAll(stmt, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")
		stmt = strings.ReplaceAll(stmt, "AUTOINCREMENT", "")
		stmt = strings.ReplaceAll(stmt, "DATETIME", "TIMESTAMP")

		// Handle INSERT OR IGNORE for migrations
		if strings.Contains(stmt, "INSERT OR IGNORE") {
			// Convert to INSERT ... ON CONFLICT DO NOTHING
			// For roles table, add unique constraint handling
			stmt = strings.Replace(stmt, "INSERT OR IGNORE", "INSERT", 1)
			if strings.Contains(stmt, "INTO roles") {
				// Add ON CONFLICT for roles table (unique on name)
				stmt = strings.TrimSuffix(stmt, ";")
				stmt += " ON CONFLICT (name) DO NOTHING"
			} else {
				// Generic ON CONFLICT for other tables
				stmt += " ON CONFLICT DO NOTHING"
			}
		}

		// Handle ON DELETE CASCADE for foreign keys
		// PostgreSQL is stricter about REFERENCES syntax
		return stmt

	} else if IsMySQL() {
		// MySQL/MariaDB conversions (for RDS MySQL/MariaDB)
		stmt = strings.ReplaceAll(stmt, "INTEGER PRIMARY KEY AUTOINCREMENT", "INT PRIMARY KEY AUTO_INCREMENT")
		stmt = strings.ReplaceAll(stmt, "AUTOINCREMENT", "AUTO_INCREMENT")
		stmt = strings.ReplaceAll(stmt, "DATETIME", "DATETIME")

		// Handle INSERT OR IGNORE for MySQL
		if strings.Contains(stmt, "INSERT OR IGNORE") {
			stmt = strings.Replace(stmt, "INSERT OR IGNORE", "INSERT IGNORE", 1)
		}

		// MySQL uses different check constraint syntax (5.7 doesn't support CHECK)
		// For compatibility, we'll keep CHECK but it may be ignored in older MySQL
		return stmt
	}

	// SQLite - no conversion needed
	return stmt
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
		`CREATE TABLE IF NOT EXISTS notification_events (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type     TEXT NOT NULL DEFAULT '',
			category       TEXT NOT NULL DEFAULT 'system',
			severity       TEXT NOT NULL DEFAULT 'info',
			title          TEXT NOT NULL DEFAULT '',
			message        TEXT NOT NULL DEFAULT '',
			entity_type    TEXT NOT NULL DEFAULT '',
			entity_id      INTEGER NOT NULL DEFAULT 0,
			connection_id  INTEGER NOT NULL DEFAULT 0,
			actor_user_id  INTEGER NOT NULL DEFAULT 0,
			payload        TEXT NOT NULL DEFAULT '{}',
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE notifications ADD COLUMN event_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE notifications ADD COLUMN target_user_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE notifications ADD COLUMN event_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notifications ADD COLUMN severity TEXT NOT NULL DEFAULT 'info'`,
		`ALTER TABLE notifications ADD COLUMN entity_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notifications ADD COLUMN entity_id INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_target_read ON notifications(target_user_id, read, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_event ON notifications(event_id)`,
		`CREATE TABLE IF NOT EXISTS notification_targets (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			type        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			config_json TEXT NOT NULL DEFAULT '{}',
			secret_enc  TEXT NOT NULL DEFAULT '',
			secondary_secret_enc TEXT NOT NULL DEFAULT '',
			is_active   INTEGER NOT NULL DEFAULT 1,
			created_by  INTEGER NOT NULL DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE notification_targets ADD COLUMN secondary_secret_enc TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS notification_rules (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			name           TEXT NOT NULL,
			description    TEXT NOT NULL DEFAULT '',
			event_type     TEXT NOT NULL DEFAULT '*',
			severity       TEXT NOT NULL DEFAULT '',
			entity_type    TEXT NOT NULL DEFAULT '',
			connection_id  INTEGER NOT NULL DEFAULT 0,
			actor_user_id  INTEGER NOT NULL DEFAULT 0,
			title_template TEXT NOT NULL DEFAULT '',
			message_template TEXT NOT NULL DEFAULT '',
			target_id      INTEGER NOT NULL DEFAULT 0,
			is_active      INTEGER NOT NULL DEFAULT 1,
			created_by     INTEGER NOT NULL DEFAULT 0,
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE notification_rules ADD COLUMN entity_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notification_rules ADD COLUMN actor_user_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE notification_rules ADD COLUMN title_template TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notification_rules ADD COLUMN message_template TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_notification_rules_match ON notification_rules(is_active, event_type, connection_id)`,
		`CREATE TABLE IF NOT EXISTS notification_deliveries (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id           INTEGER NOT NULL,
			target_id          INTEGER NOT NULL,
			channel            TEXT NOT NULL,
			status             TEXT NOT NULL DEFAULT 'pending',
			attempts           INTEGER NOT NULL DEFAULT 0,
			last_error         TEXT NOT NULL DEFAULT '',
			last_response_code INTEGER NOT NULL DEFAULT 0,
			payload_json       TEXT NOT NULL DEFAULT '{}',
			next_attempt_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_attempt_at    DATETIME DEFAULT NULL,
			created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_pending ON notification_deliveries(status, next_attempt_at, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_event ON notification_deliveries(event_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL,
			token_id     TEXT NOT NULL UNIQUE,
			ip_address   TEXT DEFAULT '',
			user_agent   TEXT DEFAULT '',
			last_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at   DATETIME NOT NULL,
			revoked_at   DATETIME DEFAULT NULL,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_token ON auth_sessions(token_id)`,
		`CREATE TABLE IF NOT EXISTS login_events (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id        INTEGER DEFAULT NULL,
			username       TEXT NOT NULL DEFAULT '',
			ip_address     TEXT DEFAULT '',
			user_agent     TEXT DEFAULT '',
			success        INTEGER NOT NULL DEFAULT 0,
			failure_reason TEXT DEFAULT '',
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_login_events_user_time ON login_events(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_login_events_username_time ON login_events(username, created_at DESC)`,
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
		`CREATE TABLE IF NOT EXISTS change_sets (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			title             TEXT NOT NULL,
			description       TEXT NOT NULL DEFAULT '',
			conn_id           INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name     TEXT NOT NULL DEFAULT '',
			statement         TEXT NOT NULL,
			rollback_sql      TEXT NOT NULL DEFAULT '',
			impact_summary    TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','pending_review','approved','rejected','executing','done','failed')),
			creator_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			reviewer_id       INTEGER REFERENCES users(id) ON DELETE SET NULL,
			review_note       TEXT NOT NULL DEFAULT '',
			workflow_id       INTEGER REFERENCES approval_workflow(id) ON DELETE SET NULL,
			current_step      INTEGER NOT NULL DEFAULT 0,
			revision          INTEGER NOT NULL DEFAULT 1,
			validation_status TEXT NOT NULL DEFAULT 'pending' CHECK(validation_status IN ('pending','passed','failed')),
			validation_message TEXT NOT NULL DEFAULT '',
			validated_at      DATETIME NULL,
			execute_error     TEXT NOT NULL DEFAULT '',
			executed_at       DATETIME NULL,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS change_set_approval (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			change_set_id INTEGER NOT NULL REFERENCES change_sets(id) ON DELETE CASCADE,
			step_id    INTEGER NOT NULL REFERENCES workflow_step(id),
			revision   INTEGER NOT NULL DEFAULT 1,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username   TEXT NOT NULL DEFAULT '',
			action     TEXT NOT NULL CHECK(action IN ('approved', 'rejected')),
			note       TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_change_sets_status ON change_sets(status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_change_sets_conn ON change_sets(conn_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_change_sets_creator ON change_sets(creator_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_change_set_approval_change_set ON change_set_approval(change_set_id, revision)`,
		`CREATE TABLE IF NOT EXISTS data_scripts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			language    TEXT NOT NULL DEFAULT 'javascript',
			created_by  INTEGER NOT NULL DEFAULT 0,
			is_active   INTEGER NOT NULL DEFAULT 1,
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS data_script_versions (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			script_id     INTEGER NOT NULL REFERENCES data_scripts(id) ON DELETE CASCADE,
			version_no    INTEGER NOT NULL DEFAULT 1,
			source_code   TEXT NOT NULL DEFAULT '',
			params_schema TEXT NOT NULL DEFAULT '{}',
			sdk_version   TEXT NOT NULL DEFAULT 'v1',
			created_by    INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS data_change_plans (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			script_id         INTEGER NOT NULL REFERENCES data_scripts(id) ON DELETE CASCADE,
			script_version_id INTEGER NOT NULL REFERENCES data_script_versions(id) ON DELETE CASCADE,
			conn_id           INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name     TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','pending_review','approved','rejected','executing','done','failed')),
			summary_json      TEXT NOT NULL DEFAULT '{}',
			risk_json         TEXT NOT NULL DEFAULT '{}',
			creator_id        INTEGER NOT NULL DEFAULT 0,
			reviewer_id       INTEGER DEFAULT NULL,
			review_note       TEXT NOT NULL DEFAULT '',
			workflow_id       INTEGER REFERENCES approval_workflow(id) ON DELETE SET NULL,
			current_step      INTEGER NOT NULL DEFAULT 0,
			revision          INTEGER NOT NULL DEFAULT 1,
			execute_error     TEXT NOT NULL DEFAULT '',
			executed_at       DATETIME DEFAULT NULL,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS data_change_plan_approval (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			plan_id     INTEGER NOT NULL REFERENCES data_change_plans(id) ON DELETE CASCADE,
			step_id     INTEGER NOT NULL REFERENCES workflow_step(id),
			revision    INTEGER NOT NULL DEFAULT 1,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username    TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL CHECK(action IN ('approved','rejected')),
			note        TEXT NOT NULL DEFAULT '',
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS data_change_plan_items (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			plan_id     INTEGER NOT NULL REFERENCES data_change_plans(id) ON DELETE CASCADE,
			seq_no      INTEGER NOT NULL DEFAULT 1,
			op_type     TEXT NOT NULL CHECK(op_type IN ('insert','update','delete')),
			table_name  TEXT NOT NULL,
			pk_json     TEXT NOT NULL DEFAULT '{}',
			before_json TEXT NOT NULL DEFAULT '{}',
			after_json  TEXT NOT NULL DEFAULT '{}',
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS backup_download_requests (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			title          TEXT NOT NULL,
			description    TEXT NOT NULL DEFAULT '',
			conn_id        INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name  TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL DEFAULT 'pending_review' CHECK(status IN ('draft','pending_review','approved','rejected','executing','done','failed')),
			creator_id     INTEGER NOT NULL DEFAULT 0,
			reviewer_id    INTEGER DEFAULT NULL,
			review_note    TEXT NOT NULL DEFAULT '',
			workflow_id    INTEGER REFERENCES approval_workflow(id) ON DELETE SET NULL,
			current_step   INTEGER NOT NULL DEFAULT 1,
			revision       INTEGER NOT NULL DEFAULT 1,
			execute_error  TEXT NOT NULL DEFAULT '',
			executed_at    DATETIME DEFAULT NULL,
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS backup_download_approval (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL REFERENCES backup_download_requests(id) ON DELETE CASCADE,
			step_id    INTEGER NOT NULL REFERENCES workflow_step(id),
			revision   INTEGER NOT NULL DEFAULT 1,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username   TEXT NOT NULL DEFAULT '',
			action     TEXT NOT NULL CHECK(action IN ('approved','rejected')),
			note       TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_data_scripts_created_by ON data_scripts(created_by, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_data_script_versions_script_id ON data_script_versions(script_id, version_no DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_data_change_plans_script_id ON data_change_plans(script_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_data_change_plans_status ON data_change_plans(status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_data_change_plan_approval_plan ON data_change_plan_approval(plan_id, revision)`,
		`CREATE INDEX IF NOT EXISTS idx_data_change_plan_items_plan_id ON data_change_plan_items(plan_id, seq_no ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_download_requests_status ON backup_download_requests(status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_download_requests_creator ON backup_download_requests(creator_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_download_requests_conn ON backup_download_requests(conn_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_download_approval_request ON backup_download_approval(request_id, revision)`,
		`ALTER TABLE data_scripts ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_scripts ADD COLUMN language TEXT NOT NULL DEFAULT 'javascript'`,
		`ALTER TABLE data_scripts ADD COLUMN created_by INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE data_scripts ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE data_scripts ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`,
		`ALTER TABLE data_script_versions ADD COLUMN params_schema TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE data_script_versions ADD COLUMN sdk_version TEXT NOT NULL DEFAULT 'v1'`,
		`ALTER TABLE data_script_versions ADD COLUMN created_by INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE data_change_plans ADD COLUMN database_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_change_plans ADD COLUMN status TEXT NOT NULL DEFAULT 'draft'`,
		`ALTER TABLE data_change_plans ADD COLUMN summary_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE data_change_plans ADD COLUMN risk_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE data_change_plans ADD COLUMN creator_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE data_change_plans ADD COLUMN reviewer_id INTEGER DEFAULT NULL`,
		`ALTER TABLE data_change_plans ADD COLUMN review_note TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_change_plans ADD COLUMN workflow_id INTEGER DEFAULT NULL`,
		`ALTER TABLE data_change_plans ADD COLUMN current_step INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE data_change_plans ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE data_change_plans ADD COLUMN execute_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_change_plans ADD COLUMN executed_at DATETIME DEFAULT NULL`,
		`ALTER TABLE data_change_plans ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`,
		`ALTER TABLE data_change_plan_approval ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE data_change_plan_approval ADD COLUMN username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_change_plan_approval ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE data_change_plan_items ADD COLUMN pk_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE data_change_plan_items ADD COLUMN before_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE data_change_plan_items ADD COLUMN after_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE backup_download_requests ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_requests ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_requests ADD COLUMN database_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_requests ADD COLUMN status TEXT NOT NULL DEFAULT 'pending_review'`,
		`ALTER TABLE backup_download_requests ADD COLUMN creator_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE backup_download_requests ADD COLUMN reviewer_id INTEGER DEFAULT NULL`,
		`ALTER TABLE backup_download_requests ADD COLUMN review_note TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_requests ADD COLUMN workflow_id INTEGER DEFAULT NULL`,
		`ALTER TABLE backup_download_requests ADD COLUMN current_step INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE backup_download_requests ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE backup_download_requests ADD COLUMN execute_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_requests ADD COLUMN executed_at DATETIME DEFAULT NULL`,
		`ALTER TABLE backup_download_requests ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`,
		`ALTER TABLE backup_download_approval ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE backup_download_approval ADD COLUMN username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backup_download_approval ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_step_workflow ON workflow_step(workflow_id, step_order)`,
		`CREATE INDEX IF NOT EXISTS idx_step_approver_step ON step_approver(step_id)`,
		`CREATE INDEX IF NOT EXISTS idx_step_approver_type_id ON step_approver(approver_type, approver_id)`,
		`CREATE INDEX IF NOT EXISTS idx_connections_folder ON connections(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_connections_visibility_owner ON connections(visibility, owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_connections_user ON user_connections(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_connections_conn ON user_connections(conn_id)`,
		`CREATE INDEX IF NOT EXISTS idx_folder_members_user ON folder_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_folder_members_folder ON folder_members(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_connection_folders_owner ON connection_folders(owner_id)`,

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
			 '["connections.view","connections.create","connections.edit","connections.delete","query.execute","query.approve","savedqueries.manage","snippets.manage","schema.browse","schema.diff.view","audit.view","ai.use","ai.manage","security.self","notifications.view","notifications.manage","backups.manage","schedules.manage","health.view","rowhistory.view","users.manage","folders.manage","roles.manage","workflows.manage"]',
			 1),
			('poweruser', 'Non-admin full access for testing',
			 '["connections.view","connections.create","connections.edit","connections.delete","query.execute","query.approve","savedqueries.manage","snippets.manage","schema.browse","schema.diff.view","audit.view","ai.use","ai.manage","security.self","notifications.view","notifications.manage","backups.manage","schedules.manage","health.view","rowhistory.view","users.manage","folders.manage","roles.manage","workflows.manage"]',
			 1),
			('user', 'Standard user access',
			 '["connections.view","query.execute","savedqueries.manage","snippets.manage","schema.browse","ai.use","security.self","notifications.view"]',
			 1)`,

		// Add role_id and per-user permission overrides
		`ALTER TABLE users ADD COLUMN role_id INTEGER REFERENCES roles(id) DEFAULT 2`,
		`ALTER TABLE users ADD COLUMN permissions TEXT DEFAULT '[]'`,
		`ALTER TABLE users ADD COLUMN is_active INTEGER DEFAULT 1`,

		// Set existing users to active
		`UPDATE users SET is_active = 1 WHERE is_active IS NULL`,
		`UPDATE roles SET permissions = '["connections.view","connections.create","connections.edit","connections.delete","query.execute","query.approve","savedqueries.manage","snippets.manage","schema.browse","schema.diff.view","audit.view","ai.use","ai.manage","security.self","notifications.view","notifications.manage","backups.manage","schedules.manage","health.view","rowhistory.view","users.manage","folders.manage","roles.manage","workflows.manage"]' WHERE name = 'admin'`,
		`UPDATE roles SET permissions = '["connections.view","connections.create","connections.edit","connections.delete","query.execute","query.approve","savedqueries.manage","snippets.manage","schema.browse","schema.diff.view","audit.view","ai.use","ai.manage","security.self","notifications.view","notifications.manage","backups.manage","schedules.manage","health.view","rowhistory.view","users.manage","folders.manage","roles.manage","workflows.manage"]' WHERE name = 'poweruser'`,
		`UPDATE roles SET permissions = '["connections.view","query.execute","savedqueries.manage","snippets.manage","schema.browse","ai.use","security.self","notifications.view"]' WHERE name = 'user'`,
		`ALTER TABLE query_approval_request ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE query_approval ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
	}
	for _, s := range stmts {
		convertedSQL := convertSQL(s)
		if _, err := DB.Exec(convertedSQL); err != nil {
			// Ignore errors for ALTER TABLE ADD COLUMN (duplicate column)
			// PostgreSQL: "column already exists"
			// SQLite: "duplicate column name"
			// MySQL: "Duplicate column name"
			errMsg := strings.ToLower(err.Error())
			if isAlterAdd(s) && (strings.Contains(errMsg, "duplicate column") ||
				strings.Contains(errMsg, "already exists")) {
				continue
			}
			// Ignore table/index already exists
			// PostgreSQL: "already exists"
			// MySQL: "already exists" or "table 'name' already exists"
			if strings.Contains(errMsg, "already exists") {
				continue
			}
			// For non-critical errors, log and continue
			if !strings.Contains(s, "CREATE TABLE") {
				fmt.Printf("Warning: migration error (non-fatal): %v\n", err)
				continue
			}
			return fmt.Errorf("migrate: %w", err)
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

// seedDefaultAdmin creates a default admin account if no users exist
func seedDefaultAdmin() error {
	// Check if any users exist
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("check users count: %w", err)
	}

	// If users already exist, skip seeding
	if count > 0 {
		return nil
	}

	// Ensure admin role exists (id=1)
	var roleCount int
	DB.QueryRow(`SELECT COUNT(*) FROM roles WHERE id = 1`).Scan(&roleCount)
	if roleCount == 0 {
		// Create admin role
		if IsPostgreSQL() || IsMySQL() {
			DB.Exec(`INSERT INTO roles (id, name, description, is_system) VALUES (1, 'Admin', 'Full system access', 1)`)
		} else {
			DB.Exec(`INSERT INTO roles (id, name, description, is_system) VALUES (1, 'Admin', 'Full system access', 1)`)
		}
	}

	// Get default credentials from environment
	username := getEnvOrDefault("DEFAULT_ADMIN_USERNAME", "admin")
	password := getEnvOrDefault("DEFAULT_ADMIN_PASSWORD", "<CHANGE_ME_ADMIN_PASSWORD>")

	// Skip if using default insecure password in production
	env := getEnvOrDefault("NIAS_ENV", "development")
	if env == "production" && password == "<CHANGE_ME_ADMIN_PASSWORD>" {
		return fmt.Errorf("DEFAULT_ADMIN_PASSWORD must be set in production")
	}

	// Hash the password (using bcrypt cost 12 same as auth.go)
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Create the admin user (use $1, $2 for PostgreSQL/MySQL, ? for SQLite)
	var query string
	if IsPostgreSQL() || IsMySQL() {
		query = `INSERT INTO users (username, password, role, role_id, is_active) VALUES ($1, $2, 'admin', 1, 1)`
	} else {
		query = `INSERT INTO users (username, password, role, role_id, is_active) VALUES (?, ?, 'admin', 1, 1)`
	}

	_, err = DB.Exec(query, username, hash)
	if err != nil {
		return fmt.Errorf("create default admin: %w", err)
	}

	fmt.Printf("✓ Default admin account created: %s\n", username)
	fmt.Printf("  Username: %s\n", username)
	if password == "<CHANGE_ME_ADMIN_PASSWORD>" {
		fmt.Printf("  Password: %s (CHANGE THIS IMMEDIATELY!)\n", password)
	} else {
		fmt.Printf("  Password: %s\n", password)
	}
	fmt.Println("  Please change the password after first login!")

	return nil
}

// hashPassword hashes a password using bcrypt (same as auth.go)
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// getEnvOrDefault returns environment variable or default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
