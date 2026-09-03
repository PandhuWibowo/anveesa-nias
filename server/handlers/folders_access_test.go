package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
)

// Regression test for the Access Groups feature. It proves two previously
// broken behaviours are now fixed end-to-end through the real HTTP handlers:
//
//   Bug 1 — "Role Restriction" was silently dropped by /api/folders. Here we
//           confirm role_restrict is persisted on create and enforced by the
//           access resolver.
//   Bug 2 — group members and connection grants were unreachable (the wiring
//           functions were orphaned), so folder_members/folder_connections were
//           never written and a non-owner member never gained access. Here we
//           add a member + a connection grant via the new endpoints and confirm
//           the member actually gains access.
//
// Driver: runs on SQLite by default (zero setup). Set NIAS_TEST_PG_URL to a
// disposable Postgres database to run the identical assertions there, e.g.
//
//	NIAS_TEST_PG_URL="postgres://localhost:5432/nias_test?sslmode=disable" \
//	    go test ./handlers/ -run TestAccessGroupMembershipAndRoleRestrict -v
func TestAccessGroupMembershipAndRoleRestrict(t *testing.T) {
	initAccessTestDB(t)

	// A connection owned by someone else, so the member can only reach it
	// through the group (not via ownership).
	connID := insertReturningID(t, `INSERT INTO connections (name, driver, database, owner_id) VALUES ('grp-conn','postgres','app',999)`)
	// A non-admin, non-owner member user with role "viewer" (unique name so a
	// shared Postgres DB doesn't collide across runs).
	uname := fmt.Sprintf("viewer_grp_%d", time.Now().UnixNano())
	memberID := insertReturningID(t, `INSERT INTO users (username, password, role) VALUES ('`+uname+`','x','viewer')`)
	t.Cleanup(func() {
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM connections WHERE id=?`), connID)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM users WHERE id=?`), memberID)
	})

	asAdmin := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-User-ID", "1")
		r.Header.Set("X-User-Role", "admin")
		return r
	}

	// 1. Create an access group restricted to role "viewer".
	rec := httptest.NewRecorder()
	CreateFolder()(rec, asAdmin(http.MethodPost, "/api/folders",
		`{"name":"QA","visibility":"private","role_restrict":"viewer","color":"#123456"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: status %d body %s", rec.Code, rec.Body.String())
	}
	var created ConnectionFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created folder: %v", err)
	}
	if created.RoleRestrict != "viewer" {
		t.Fatalf("Bug 1: role_restrict not returned on create, got %q", created.RoleRestrict)
	}
	groupID := created.ID
	if groupID == 0 {
		t.Fatalf("CreateFolder returned id=0 (LastInsertId unsupported on this driver)")
	}
	t.Cleanup(func() { appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM connection_folders WHERE id=?`), groupID) })

	var persisted string
	appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT role_restrict FROM connection_folders WHERE id=?`), groupID).Scan(&persisted)
	if persisted != "viewer" {
		t.Fatalf("Bug 1: role_restrict not persisted in DB, got %q", persisted)
	}

	gid := strconv.FormatInt(groupID, 10)
	cid := strconv.FormatInt(connID, 10)
	mid := strconv.FormatInt(memberID, 10)

	// 2. Grant the connection to the group.
	rec = httptest.NewRecorder()
	SetGroupConnections()(rec, asAdmin(http.MethodPut, "/api/folders/"+gid+"/connections",
		`{"connection_ids":[`+cid+`],"connection_permissions":[{"conn_id":`+cid+`,"permissions":["select"]}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set group connections: status %d body %s", rec.Code, rec.Body.String())
	}

	// 3. Add the member to the group.
	rec = httptest.NewRecorder()
	SetGroupMembers()(rec, asAdmin(http.MethodPut, "/api/folders/"+gid+"/members",
		`{"user_ids":[`+mid+`]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set group members: status %d body %s", rec.Code, rec.Body.String())
	}

	// Sanity: the members endpoint reflects the new member.
	rec = httptest.NewRecorder()
	GroupMembers()(rec, asAdmin(http.MethodGet, "/api/folders/"+gid+"/members", ""))
	if !strings.Contains(rec.Body.String(), uname) {
		t.Fatalf("Bug 2: member not returned by GET members, body %s", rec.Body.String())
	}

	// 4. Member with the matching role gains access through the group.
	ids, err := appdb.GetAccessibleConnectionIDs(memberID, "viewer")
	if err != nil {
		t.Fatalf("accessible (viewer): %v", err)
	}
	if !containsID(ids, connID) {
		t.Fatalf("Bug 2: member should have access via group, got %v", ids)
	}

	// 5. The same user under a non-matching role is blocked by role_restrict.
	ids, err = appdb.GetAccessibleConnectionIDs(memberID, "editor")
	if err != nil {
		t.Fatalf("accessible (editor): %v", err)
	}
	if containsID(ids, connID) {
		t.Fatalf("Bug 1: role_restrict should block a non-viewer role, got %v", ids)
	}
}

// initAccessTestDB initialises the internal app DB for the test. Postgres when
// NIAS_TEST_PG_URL is set (skips if unreachable), otherwise a throwaway SQLite file.
func initAccessTestDB(t *testing.T) {
	t.Helper()
	if pgURL := os.Getenv("NIAS_TEST_PG_URL"); pgURL != "" {
		if err := appdb.Init(&config.Config{DBDriver: "postgres", DBURL: pgURL}); err != nil {
			t.Skipf("NIAS_TEST_PG_URL set but Postgres not reachable: %v", err)
		}
		return
	}
	dbPath := filepath.Join(t.TempDir(), "nias_test.db")
	if err := appdb.Init(&config.Config{DBDriver: "sqlite", DBURL: dbPath}); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { appdb.DB.Close() })
}

// insertReturningID runs a parameterless INSERT and returns the new row id on
// either driver (lib/pq has no LastInsertId, so use RETURNING there).
func insertReturningID(t *testing.T, insert string) int64 {
	t.Helper()
	var id int64
	if appdb.IsPostgreSQL() {
		if err := appdb.DB.QueryRow(appdb.ConvertQuery(insert + " RETURNING id")).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}
	res, err := appdb.DB.Exec(appdb.ConvertQuery(insert))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ = res.LastInsertId()
	return id
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
