package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// Scenario matrix for per-connection DB permission enforcement.
//
// Reported bug: a user granted only SELECT on a connection could still run
// UPDATE/INSERT/DELETE. Root cause: the query path used a binary
// CheckWritePermission (true if the user had ANY of insert/update/delete, and
// blind to create/alter/drop). These scenarios lock in the granular behaviour
// of CheckOperationPermission, which the execute paths now use.
//
// Runs on SQLite by default; set NIAS_TEST_PG_URL for Postgres (see
// folders_access_test.go for the shared harness helpers).

// permTestFixture seeds one connection (owned by ownerID) plus a member user
// with an explicit direct grant, and returns an *http.Request pre-populated
// with that user's auth headers.
func seedConnUser(t *testing.T, ownerID int64, grant string) (connID, userID int64) {
	t.Helper()
	// ownerID == 0 → NULL owner (legacy/ownerless connection).
	if ownerID == 0 {
		connID = insertReturningID(t, `INSERT INTO connections (name, driver, database) VALUES ('c-`+uniq()+`','postgres','app')`)
	} else {
		connID = insertReturningID(t, fmt.Sprintf(`INSERT INTO connections (name, driver, database, owner_id) VALUES ('c-%s','postgres','app',%d)`, uniq(), ownerID))
	}
	userID = insertReturningID(t, `INSERT INTO users (username, password, role) VALUES ('u-`+uniq()+`','x','user')`)
	if grant != "" {
		appdb.DB.Exec(appdb.ConvertQuery(`INSERT INTO user_connections (user_id, conn_id, permissions) VALUES (?,?,?)`),
			userID, connID, grant)
	}
	t.Cleanup(func() {
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM user_connections WHERE conn_id=?`), connID)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM connections WHERE id=?`), connID)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM users WHERE id=?`), userID)
	})
	return connID, userID
}

func uniq() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

func reqAs(userID int64, role string, connID int64) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/connections/"+strconv.FormatInt(connID, 10)+"/query", nil)
	if userID > 0 {
		r.Header.Set("X-User-ID", strconv.FormatInt(userID, 10))
	}
	r.Header.Set("X-User-Role", role)
	return r
}

func TestConnectionOperationPermissionScenarios(t *testing.T) {
	initAccessTestDB(t)

	t.Run("select-only user cannot UPDATE/INSERT/DELETE (the reported bug)", func(t *testing.T) {
		connID, userID := seedConnUser(t, 1, `["select"]`)
		r := reqAs(userID, "user", connID)
		if !CheckOperationPermission(r, connID, DbPermSelect) {
			t.Error("SELECT should be allowed")
		}
		for _, p := range []DbPerm{DbPermUpdate, DbPermInsert, DbPermDelete} {
			if CheckOperationPermission(r, connID, p) {
				t.Errorf("%s must be DENIED for a select-only grant", p)
			}
		}
	})

	t.Run("insert+update user cannot DELETE or DROP (granularity)", func(t *testing.T) {
		connID, userID := seedConnUser(t, 1, `["select","insert","update"]`)
		r := reqAs(userID, "user", connID)
		if !CheckOperationPermission(r, connID, DbPermInsert) {
			t.Error("INSERT should be allowed")
		}
		if !CheckOperationPermission(r, connID, DbPermUpdate) {
			t.Error("UPDATE should be allowed")
		}
		if CheckOperationPermission(r, connID, DbPermDelete) {
			t.Error("DELETE must be DENIED without delete grant")
		}
		if CheckOperationPermission(r, connID, DbPermDrop) {
			t.Error("DROP must be DENIED without drop grant")
		}
	})

	t.Run("schema grants are honoured (create allowed, delete not)", func(t *testing.T) {
		connID, userID := seedConnUser(t, 1, `["create","alter"]`)
		r := reqAs(userID, "user", connID)
		if !CheckOperationPermission(r, connID, DbPermCreate) {
			t.Error("CREATE should be allowed with create grant (old binary gate wrongly denied this)")
		}
		if !CheckOperationPermission(r, connID, DbPermAlter) {
			t.Error("ALTER should be allowed with alter grant")
		}
		if CheckOperationPermission(r, connID, DbPermDelete) {
			t.Error("DELETE must be DENIED without delete grant")
		}
	})

	t.Run("null-owner connection still enforces an explicit grant", func(t *testing.T) {
		connID, userID := seedConnUser(t, 0, `["select"]`)
		r := reqAs(userID, "user", connID)
		if CheckOperationPermission(r, connID, DbPermUpdate) {
			t.Error("UPDATE must be DENIED even on an ownerless connection when a grant exists")
		}
		if !CheckOperationPermission(r, connID, DbPermSelect) {
			t.Error("SELECT should be allowed")
		}
	})

	t.Run("null-owner connection with no grant stays permissive (legacy)", func(t *testing.T) {
		connID, userID := seedConnUser(t, 0, "")
		r := reqAs(userID, "user", connID)
		if !CheckOperationPermission(r, connID, DbPermUpdate) {
			t.Error("legacy ownerless + no-grant should remain permissive")
		}
	})

	t.Run("connection owner keeps full access despite a restrictive grant", func(t *testing.T) {
		connID, userID := seedConnUser(t, 1, `["select"]`)
		// Re-own the connection to the user themselves.
		appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET owner_id=? WHERE id=?`), userID, connID)
		r := reqAs(userID, "user", connID)
		if !CheckOperationPermission(r, connID, DbPermDelete) {
			t.Error("owner should retain write access")
		}
	})

	t.Run("admin bypasses per-connection permissions", func(t *testing.T) {
		connID, userID := seedConnUser(t, 1, `["select"]`)
		r := reqAs(userID, "admin", connID)
		if !CheckOperationPermission(r, connID, DbPermDrop) {
			t.Error("admin should bypass")
		}
	})

	t.Run("no user id → denied", func(t *testing.T) {
		connID, _ := seedConnUser(t, 1, `["select"]`)
		r := reqAs(0, "user", connID)
		if CheckOperationPermission(r, connID, DbPermUpdate) {
			t.Error("missing user id must be denied")
		}
	})
}

func TestRequiredPermForSQL(t *testing.T) {
	cases := map[string]DbPerm{
		"SELECT * FROM t":                DbPermSelect,
		"  with cte as (...) select 1":   DbPermSelect,
		"-- lead comment\nUPDATE t SET x=1": DbPermUpdate,
		"insert into t values (1)":       DbPermInsert,
		"DELETE FROM t":                  DbPermDelete,
		"TRUNCATE t":                     DbPermDelete,
		"create table t(x int)":          DbPermCreate,
		"ALTER TABLE t ADD c int":        DbPermAlter,
		"drop table t":                   DbPermDrop,
		"/* c */ EXPLAIN SELECT 1":       DbPermSelect,
	}
	for sql, want := range cases {
		if got := RequiredPermForSQL(sql); got != want {
			t.Errorf("RequiredPermForSQL(%q) = %q, want %q", sql, got, want)
		}
	}
}
