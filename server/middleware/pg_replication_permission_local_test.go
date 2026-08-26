package middleware

// Scratch integration test against the local Postgres app database (same
// convention as handlers/cloud_storage_local_test.go and
// handlers/pg_replication_local_test.go) — exercises the *actual* gate every
// /api/pg-replication/* route is wrapped in (main.go:
// requireAny := mw.RequireAnyAppPermissionHeader(...)), against real
// throwaway roles/users, not a mock of db.HasUserAppPermission. Connects
// directly to the local nias_dev database via trust-authenticated local
// Postgres (no app credentials read), creates its own role/user rows
// prefixed pgrepltest_, and deletes them afterward.
//
// Run: PGREPL_PERM_TEST=1 go test ./middleware/ -run TestPgReplicationPermissionGating -v

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
)

func TestPgReplicationPermissionGating(t *testing.T) {
	if os.Getenv("PGREPL_PERM_TEST") != "1" {
		t.Skip("set PGREPL_PERM_TEST=1 to run against the local nias_dev database")
	}

	// Going through db.Init (not a bare sql.Open + assigning db.DB directly)
	// matters here: ConvertQuery's ?->$1 rewriting reads the package-level
	// dbDriver var that only Init sets, and every permission lookup this
	// test exercises goes through ConvertQuery. A bare sql.Open leaves
	// dbDriver empty, silently breaking every query below with no crash —
	// just wrong placeholder syntax against lib/pq, which reads as "no
	// permission" instead of a query error.
	if err := appdb.Init(&config.Config{
		DBDriver: "postgres",
		DBURL:    "postgres://localhost:5432/nias_dev?sslmode=disable",
	}); err != nil {
		t.Skipf("nias_dev not reachable locally: %v", err)
	}
	conn := appdb.DB
	// No defer conn.Close() here deliberately: t.Cleanup callbacks run
	// *after* the test function (and its local defers) return, so a local
	// defer would close the connection before the DELETE-based cleanup
	// below gets to run on it. The process exits right after this test
	// package finishes anyway, so leaving the connection open is harmless.

	const viewPerm = "pgreplication.view"
	const managePerm = "pgreplication.manage"

	// Dummy handler standing in for the real ListPublications/
	// CreatePublication/etc. handlers — this test is about the permission
	// gate every route is wrapped in, not the pub/sub DDL logic itself
	// (covered separately in handlers/pg_replication_local_test.go).
	okHandler := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	viewGate := RequireAnyAppPermissionHeader(viewPerm, managePerm) // GET routes (list/status)
	manageGate := RequireAnyAppPermissionHeader(managePerm)         // mutating routes (create/drop/enable/disable)

	// Defensive upfront cleanup (users before roles, for the FK) in case a
	// previous run left rows behind — e.g. from a panic, or from Ctrl-C.
	conn.Exec(`DELETE FROM users WHERE username LIKE 'pgrepltest_%'`)
	conn.Exec(`DELETE FROM roles WHERE name LIKE 'pgrepltest_%'`)

	viewRoleID := createTestRole(t, conn, "pgrepltest_role_view", []string{viewPerm})
	viewUserID := createTestUser(t, conn, "pgrepltest_user_view", viewRoleID)
	manageRoleID := createTestRole(t, conn, "pgrepltest_role_manage", []string{viewPerm, managePerm})
	manageUserID := createTestUser(t, conn, "pgrepltest_user_manage", manageRoleID)
	unrelatedRoleID := createTestRole(t, conn, "pgrepltest_role_unrelated", []string{"connections.view"})
	unrelatedUserID := createTestUser(t, conn, "pgrepltest_user_unrelated", unrelatedRoleID)

	t.Cleanup(func() {
		if _, err := conn.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, viewUserID, manageUserID, unrelatedUserID); err != nil {
			t.Logf("cleanup: failed to delete test users: %v", err)
		}
		if _, err := conn.Exec(`DELETE FROM roles WHERE id IN ($1,$2,$3)`, viewRoleID, manageRoleID, unrelatedRoleID); err != nil {
			t.Logf("cleanup: failed to delete test roles: %v", err)
		}
	})

	call := func(gate func(http.HandlerFunc) http.HandlerFunc, userID int64, role string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/pg-replication/publications", nil)
		if userID > 0 {
			req.Header.Set("X-User-ID", strconv.FormatInt(userID, 10))
		}
		if role != "" {
			req.Header.Set("X-User-Role", role)
		}
		rec := httptest.NewRecorder()
		gate(okHandler)(rec, req)
		return rec.Code
	}

	scenarios := []struct {
		name   string
		gate   func(http.HandlerFunc) http.HandlerFunc
		userID int64
		role   string
		want   int
	}{
		{"no auth headers at all", viewGate, 0, "", http.StatusUnauthorized},
		{"admin role bypasses regardless of stored permissions", manageGate, unrelatedUserID, "admin", http.StatusOK},
		{"view-only user on a view-gated route", viewGate, viewUserID, "user", http.StatusOK},
		{"view-only user on a manage-gated route is rejected", manageGate, viewUserID, "user", http.StatusForbidden},
		{"manage user on a view-gated route", viewGate, manageUserID, "user", http.StatusOK},
		{"manage user on a manage-gated route", manageGate, manageUserID, "user", http.StatusOK},
		{"user with only an unrelated permission is rejected", viewGate, unrelatedUserID, "user", http.StatusForbidden},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			got := call(sc.gate, sc.userID, sc.role)
			if got != sc.want {
				t.Errorf("got HTTP %d, want %d", got, sc.want)
			}
		})
	}
}

func createTestRole(t *testing.T, conn *sql.DB, name string, perms []string) int64 {
	t.Helper()
	permsJSON, _ := json.Marshal(perms)
	var id int64
	err := conn.QueryRow(
		`INSERT INTO roles (name, description, permissions, is_system) VALUES ($1, 'pg-replication permission test - safe to delete', $2, 0) RETURNING id`,
		name, string(permsJSON),
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test role %s: %v", name, err)
	}
	return id
}

func createTestUser(t *testing.T, conn *sql.DB, username string, roleID int64) int64 {
	t.Helper()
	var id int64
	err := conn.QueryRow(
		`INSERT INTO users (username, password, role, role_id, is_active) VALUES ($1, 'x', 'user', $2, 1) RETURNING id`,
		username, roleID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test user %s: %v", username, err)
	}
	return id
}
