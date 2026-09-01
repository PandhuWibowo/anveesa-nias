package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestPerUserConnection covers the "connect as yourself" feature.
//
//   - Unit portion (always runs on SQLite): the auth_mode gating — a per_user
//     connection with no stored login is refused, while a shared connection
//     falls through to the shared login.
//   - Live portion (runs only when NIAS_TEST_TARGET_PG points at a reachable
//     Postgres): storing a per-user login through the handler verifies it
//     against the DB, and GetDBForUser then connects to the real database as
//     that login.
func TestPerUserConnection(t *testing.T) {
	initAccessTestDB(t) // Postgres when NIAS_TEST_PG_URL is set, else SQLite
	SetEncryptionKey("0123456789abcdef0123456789abcdef") // 32-char test key

	userID := insertReturningID(t, `INSERT INTO users (username, password, role) VALUES ('peruser_`+uniq()+`','x','user')`)

	t.Run("per_user connection refuses a user with no stored login", func(t *testing.T) {
		connID := insertReturningID(t, `INSERT INTO connections (name, driver, database, host, port, username, password, auth_mode) VALUES ('pg','postgres','db','127.0.0.1',5432,'x','y','per_user')`)
		_, _, err := GetDBForUser(userID, connID)
		if err == nil || !strings.Contains(err.Error(), "database login") {
			t.Fatalf("per_user + no credential should require a personal login, got err=%v", err)
		}
	})

	t.Run("shared connection falls through to the shared login", func(t *testing.T) {
		connID := insertReturningID(t, `INSERT INTO connections (name, driver, database, host, port, username, password, auth_mode) VALUES ('pg','postgres','db','127.0.0.1',5432,'x','y','shared')`)
		// sql.Open is lazy, so this returns a handle without dialing — the point
		// is that it did NOT hit the per_user gate.
		if _, _, err := GetDBForUser(userID, connID); err != nil && strings.Contains(err.Error(), "database login") {
			t.Fatalf("shared connection must not require a personal login, got %v", err)
		}
	})

	// ── Live portion ────────────────────────────────────────────────
	target := os.Getenv("NIAS_TEST_TARGET_PG")
	if target == "" {
		t.Log("set NIAS_TEST_TARGET_PG=postgres://user:pass@host:port/db to run the live per-user connection check")
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("bad NIAS_TEST_TARGET_PG: %v", err)
	}
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	dbname := strings.TrimPrefix(u.Path, "/")
	dbUser := u.User.Username()
	dbPass, _ := u.User.Password()

	// A per_user connection whose *shared* login is deliberately bogus, so the
	// only way a query works is via the user's own stored login.
	liveConn := insertReturningID(t, fmt.Sprintf(
		`INSERT INTO connections (name, driver, database, host, port, username, password, ssl, auth_mode) VALUES ('live','postgres',%s,%s,%d,'bogus','bogus',0,'per_user')`,
		sqlStr(dbname), sqlStr(host), port))

	// Store the real login through the handler — this also exercises the
	// pre-save connectivity check.
	body := fmt.Sprintf(`{"db_username":%q,"db_password":%q}`, dbUser, dbPass)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/users/%d/connections/%d/credential", userID, liveConn),
		strings.NewReader(body))
	r.Header.Set("X-User-ID", "1")
	r.Header.Set("X-User-Role", "admin")
	SetUserConnCredential()(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("set credential: status %d body %s", rec.Code, rec.Body.String())
	}

	// Connect as the user and confirm we reach the real database.
	db, _, err := GetDBForUser(userID, liveConn)
	if err != nil {
		t.Fatalf("GetDBForUser (live): %v", err)
	}
	var who string
	if err := db.QueryRow("SELECT current_user").Scan(&who); err != nil {
		t.Fatalf("query as user: %v", err)
	}
	if who != dbUser {
		t.Fatalf("expected to be connected as %q, but the database says %q", dbUser, who)
	}
	t.Logf("live per-user connection authenticated to the target DB as %q", who)
}

// sqlStr renders a Go string as a single-quoted SQL literal.
func sqlStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
