package handlers

// Scratch integration test for the per-connection authorization + orphaned-
// publication cleanup added to pg_replication.go. Unlike
// pg_replication_local_test.go (which exercises the DDL/escaping logic
// directly against Postgres) and middleware/pg_replication_permission_local_test.go
// (which only exercises the app-level permission gate every route is
// wrapped in), this test drives the actual exported handler functions
// end-to-end — real connections rows, real user_connections grants, real
// local Postgres source/target databases — to prove:
//   - CreateSubscription is denied without read on the source / write on the target
//   - DropSubscription is denied without write on the target
//   - DropSubscription actually drops the paired publication on the source
//     when authorized, and correctly leaves it alone when the caller lacks
//     write on the source, or when another link still references it
//
// Run: PGREPL_AUTHZ_TEST=1 go test ./handlers/ -run TestPgReplicationAuthz -v
import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"strconv"
	"strings"
	"testing"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	_ "github.com/lib/pq"
)

// pgReplTestResp and pgReplTestDoFn let the top-level mustCreate/cleanupLink
// helpers share the exact same request-firing closure the test builds
// in-line — Go function types must match exactly (not just structurally) to
// be passed as a parameter, hence the named type instead of an inline struct
// literal in each helper's signature.
type pgReplTestResp struct {
	code int
	body map[string]any
}
type pgReplTestDoFn func(method, path string, userID int64, payload any) pgReplTestResp

func TestPgReplicationAuthz(t *testing.T) {
	if os.Getenv("PGREPL_AUTHZ_TEST") != "1" {
		t.Skip("set PGREPL_AUTHZ_TEST=1 to run against the local nias_dev + local Postgres")
	}

	if err := appdb.Init(&config.Config{
		DBDriver: "postgres",
		DBURL:    "postgres://localhost:5432/nias_dev?sslmode=disable",
	}); err != nil {
		t.Skipf("nias_dev not reachable locally: %v", err)
	}
	conn := appdb.DB

	osUser, err := user.Current()
	if err != nil {
		t.Fatalf("could not determine current OS user: %v", err)
	}
	osUsername := osUser.Username

	// Source and target deliberately live on two SEPARATE local Postgres
	// clusters (5432 and 5433), not two databases on the same instance.
	// Same-instance loopback replication is a known Postgres footgun: the
	// still-open CREATE SUBSCRIPTION transaction on the target and the
	// replication-slot creation it triggers on the source share the same
	// cluster-wide transaction-ID space, so the slot creation ends up
	// waiting on a lock held by the very transaction that's waiting on it —
	// a self-deadlock that has nothing to do with the code under test and
	// would never happen in the two-separate-servers topology this feature
	// actually documents (see the page's own "Before you start" banner).
	const srcPort = 5432
	const tgtPort = 5433
	srcAdmin, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:%d/postgres?sslmode=disable", srcPort))
	if err != nil {
		t.Fatalf("open source admin conn: %v", err)
	}
	defer srcAdmin.Close()
	if err := srcAdmin.Ping(); err != nil {
		t.Skipf("local postgres (source, :%d) not reachable: %v", srcPort, err)
	}
	tgtAdmin, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:%d/postgres?sslmode=disable", tgtPort))
	if err != nil {
		t.Fatalf("open target admin conn: %v", err)
	}
	defer tgtAdmin.Close()
	if err := tgtAdmin.Ping(); err != nil {
		t.Skipf("local postgres (target, :%d) not reachable: %v", tgtPort, err)
	}

	const srcDB = "nias_pgrepltest2_src"
	const tgtDB = "nias_pgrepltest2_tgt"
	dropDBs := func() {
		srcAdmin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentPG(srcDB)))
		tgtAdmin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentPG(tgtDB)))
	}
	dropDBs()
	defer dropDBs()
	if _, err := srcAdmin.Exec("CREATE DATABASE " + quoteIdentPG(srcDB)); err != nil {
		t.Fatalf("create src db: %v", err)
	}
	if _, err := tgtAdmin.Exec("CREATE DATABASE " + quoteIdentPG(tgtDB)); err != nil {
		t.Fatalf("create tgt db: %v", err)
	}

	src, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:%d/%s?sslmode=disable", srcPort, srcDB))
	if err != nil {
		t.Fatalf("open src conn: %v", err)
	}
	defer src.Close()
	tgt, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:%d/%s?sslmode=disable", tgtPort, tgtDB))
	if err != nil {
		t.Fatalf("open tgt conn: %v", err)
	}
	defer tgt.Close()

	if _, err := src.Exec(`ALTER SYSTEM SET wal_level = 'logical'`); err != nil {
		t.Fatalf("set wal_level: %v", err)
	}
	// wal_level is a postmaster-restart setting, not per-database — reload
	// alone won't apply it. If the local server wasn't already running with
	// wal_level=logical, skip: this test can't restart the local Postgres
	// service itself, and the pg_replication_local_test.go convention is to
	// treat unmet local prerequisites as a skip, not a failure.
	var walLevel string
	if err := src.QueryRow(`SHOW wal_level`).Scan(&walLevel); err != nil || walLevel != "logical" {
		t.Skipf("local Postgres is not running with wal_level=logical (got %q) — restart it with that setting to run this test", walLevel)
	}

	if _, err := src.Exec(`CREATE TABLE public.widgets (id serial PRIMARY KEY, name text)`); err != nil {
		t.Fatalf("create source table: %v", err)
	}
	if _, err := tgt.Exec(`CREATE TABLE public.widgets (id serial PRIMARY KEY, name text)`); err != nil {
		t.Fatalf("create target table: %v", err)
	}

	// ── Nias-side fixtures: connections rows + throwaway users/grants ──────
	conn.Exec(`DELETE FROM users WHERE username LIKE 'pgrepltest2_%'`)
	conn.Exec(`DELETE FROM connections WHERE name LIKE 'pgrepltest2_%'`)

	insertConn := func(name, dbname string, port int) int64 {
		var id int64
		err := conn.QueryRow(
			`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, owner_id) VALUES ($1,'postgres','localhost',$2,$3,$4,'',0,999999999) RETURNING id`,
			name, port, dbname, osUsername,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert connection %s: %v", name, err)
		}
		return id
	}
	srcConnID := insertConn("pgrepltest2_src", srcDB, srcPort)
	tgtConnID := insertConn("pgrepltest2_tgt", tgtDB, tgtPort)

	insertUser := func(username string) int64 {
		var id int64
		err := conn.QueryRow(`INSERT INTO users (username, password, role, is_active) VALUES ($1, 'x', 'user', 1) RETURNING id`, username).Scan(&id)
		if err != nil {
			t.Fatalf("insert user %s: %v", username, err)
		}
		return id
	}
	grant := func(userID, connID int64, perms string) {
		if _, err := conn.Exec(`INSERT INTO user_connections (user_id, conn_id, permissions) VALUES ($1,$2,$3)`, userID, connID, perms); err != nil {
			t.Fatalf("grant perms: %v", err)
		}
	}

	authorizedUser := insertUser("pgrepltest2_user_authorized")
	grant(authorizedUser, srcConnID, `["select","insert","update","delete","create","alter","drop"]`)
	grant(authorizedUser, tgtConnID, `["select","insert","update","delete","create","alter","drop"]`)

	noSourceReadUser := insertUser("pgrepltest2_user_nosourceread")
	grant(noSourceReadUser, srcConnID, `["insert"]`) // has a grant row, but not select
	grant(noSourceReadUser, tgtConnID, `["select","insert","update","delete","create","alter","drop"]`)

	noTargetWriteUser := insertUser("pgrepltest2_user_notargetwrite")
	grant(noTargetWriteUser, srcConnID, `["select","insert","update","delete","create","alter","drop"]`)
	grant(noTargetWriteUser, tgtConnID, `["select"]`) // read-only on target

	// target-write but NOT source-write — used to prove the drop-time
	// publication cleanup is itself permission-checked against the source.
	noSourceWriteUser := insertUser("pgrepltest2_user_nosourcewrite")
	grant(noSourceWriteUser, srcConnID, `["select"]`) // read-only on source
	grant(noSourceWriteUser, tgtConnID, `["select","insert","update","delete","create","alter","drop"]`)

	t.Cleanup(func() {
		conn.Exec(`DELETE FROM users WHERE username LIKE 'pgrepltest2_%'`)
		conn.Exec(`DELETE FROM connections WHERE name LIKE 'pgrepltest2_%'`)
	})

	// ── HTTP helpers ─────────────────────────────────────────────────────
	var doJSON pgReplTestDoFn
	doJSON = func(method, path string, userID int64, payload any) pgReplTestResp {
		var body *bytes.Buffer
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewBuffer(b)
		} else {
			body = bytes.NewBuffer(nil)
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("X-User-ID", strconv.FormatInt(userID, 10))
		req.Header.Set("X-User-Role", "user")
		rec := httptest.NewRecorder()

		switch {
		case method == http.MethodPost && path == "/api/pg-replication/publications":
			CreatePublication()(rec, req)
		case method == http.MethodDelete && strings.HasPrefix(path, "/api/pg-replication/publications/"):
			DropPublication()(rec, req)
		case method == http.MethodPost && path == "/api/pg-replication/subscriptions":
			CreateSubscription()(rec, req)
		case method == http.MethodDelete && strings.HasPrefix(path, "/api/pg-replication/subscriptions/"):
			DropSubscription()(rec, req)
		default:
			t.Fatalf("doJSON: unrecognized route %s %s", method, path)
		}

		var decoded map[string]any
		json.Unmarshal(rec.Body.Bytes(), &decoded)
		return pgReplTestResp{code: rec.Code, body: decoded}
	}

	publicationExists := func(name string) bool {
		var count int
		src.QueryRow(`SELECT count(*) FROM pg_publication WHERE pubname=$1`, name).Scan(&count)
		return count > 0
	}
	subscriptionExists := func(name string) bool {
		var count int
		tgt.QueryRow(`SELECT count(*) FROM pg_subscription WHERE subname=$1`, name).Scan(&count)
		return count > 0
	}

	t.Run("CreateSubscription denied without read on source", func(t *testing.T) {
		r := doJSON(http.MethodPost, "/api/pg-replication/subscriptions", noSourceReadUser, map[string]any{
			"sourceConnectionId": srcConnID, "targetConnectionId": tgtConnID,
			"name": "pgrt2_sub_denied1", "publicationName": "pgrt2_pub_denied1",
		})
		if r.code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%v", r.code, r.body)
		}
	})

	t.Run("CreateSubscription denied without write on target", func(t *testing.T) {
		r := doJSON(http.MethodPost, "/api/pg-replication/subscriptions", noTargetWriteUser, map[string]any{
			"sourceConnectionId": srcConnID, "targetConnectionId": tgtConnID,
			"name": "pgrt2_sub_denied2", "publicationName": "pgrt2_pub_denied2",
		})
		if r.code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%v", r.code, r.body)
		}
	})

	t.Run("full authorized flow: create, verify, drop cleans up the publication", func(t *testing.T) {
		pubName := "pgrt2_pub_happy"
		subName := "pgrt2_sub_happy"

		pr := doJSON(http.MethodPost, "/api/pg-replication/publications", authorizedUser, map[string]any{
			"connectionId": srcConnID, "name": pubName, "allTables": false, "tables": []string{"public.widgets"},
		})
		if pr.code != http.StatusOK {
			t.Fatalf("CreatePublication: expected 200, got %d body=%v", pr.code, pr.body)
		}
		if !publicationExists(pubName) {
			t.Fatal("publication was not actually created on the source")
		}

		sr := doJSON(http.MethodPost, "/api/pg-replication/subscriptions", authorizedUser, map[string]any{
			"sourceConnectionId": srcConnID, "targetConnectionId": tgtConnID,
			"name": subName, "publicationName": pubName,
		})
		if sr.code != http.StatusOK {
			t.Fatalf("CreateSubscription: expected 200, got %d body=%v", sr.code, sr.body)
		}
		if !subscriptionExists(subName) {
			t.Fatal("subscription was not actually created on the target")
		}
		var linkCount int
		conn.QueryRow(`SELECT count(*) FROM pg_replication_links WHERE subscription_name=$1`, subName).Scan(&linkCount)
		if linkCount != 1 {
			t.Fatalf("expected exactly one bookkeeping link row, got %d", linkCount)
		}

		dr := doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+subName+"?connection_id="+strconv.FormatInt(tgtConnID, 10), authorizedUser, nil)
		if dr.code != http.StatusOK {
			t.Fatalf("DropSubscription: expected 200, got %d body=%v", dr.code, dr.body)
		}
		if subscriptionExists(subName) {
			t.Error("subscription still exists on target after drop")
		}
		if publicationExists(pubName) {
			t.Error("BUG: publication still exists on source after drop — orphaned-publication cleanup did not run")
		}
		conn.QueryRow(`SELECT count(*) FROM pg_replication_links WHERE subscription_name=$1`, subName).Scan(&linkCount)
		if linkCount != 0 {
			t.Error("bookkeeping link row was not removed")
		}
	})

	t.Run("drop denied without write on target — nothing is touched", func(t *testing.T) {
		pubName := "pgrt2_pub_dropdenied"
		subName := "pgrt2_sub_dropdenied"
		mustCreate(t, doJSON, pubName, subName, srcConnID, tgtConnID, authorizedUser)
		defer cleanupLink(t, doJSON, src, tgt, pubName, subName, tgtConnID, authorizedUser)

		dr := doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+subName+"?connection_id="+strconv.FormatInt(tgtConnID, 10), noTargetWriteUser, nil)
		if dr.code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%v", dr.code, dr.body)
		}
		if !subscriptionExists(subName) {
			t.Error("subscription should still exist — the denied drop must not have run")
		}
		if !publicationExists(pubName) {
			t.Error("publication should still exist — the denied drop must not have run")
		}
	})

	t.Run("drop without write on source leaves the publication orphaned instead of bypassing the check", func(t *testing.T) {
		pubName := "pgrt2_pub_nosrcwrite"
		subName := "pgrt2_sub_nosrcwrite"
		mustCreate(t, doJSON, pubName, subName, srcConnID, tgtConnID, authorizedUser)
		defer src.Exec("DROP PUBLICATION IF EXISTS " + quoteIdentPG(pubName))

		dr := doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+subName+"?connection_id="+strconv.FormatInt(tgtConnID, 10), noSourceWriteUser, nil)
		if dr.code != http.StatusOK {
			t.Fatalf("DropSubscription itself should succeed (caller has target write), got %d body=%v", dr.code, dr.body)
		}
		if subscriptionExists(subName) {
			t.Error("subscription should be gone from target")
		}
		if !publicationExists(pubName) {
			t.Error("publication should have been LEFT ALONE (caller lacked write on source) — it must not silently disappear")
		}
	})

	t.Run("shared publication survives dropping one of two links, and is removed once the last one drops", func(t *testing.T) {
		pubName := "pgrt2_pub_shared"
		sub1 := "pgrt2_sub_shared_1"
		sub2 := "pgrt2_sub_shared_2"

		pr := doJSON(http.MethodPost, "/api/pg-replication/publications", authorizedUser, map[string]any{
			"connectionId": srcConnID, "name": pubName, "allTables": false, "tables": []string{"public.widgets"},
		})
		if pr.code != http.StatusOK {
			t.Fatalf("CreatePublication: %d %v", pr.code, pr.body)
		}
		for _, sub := range []string{sub1, sub2} {
			sr := doJSON(http.MethodPost, "/api/pg-replication/subscriptions", authorizedUser, map[string]any{
				"sourceConnectionId": srcConnID, "targetConnectionId": tgtConnID,
				"name": sub, "publicationName": pubName,
			})
			if sr.code != http.StatusOK {
				t.Fatalf("CreateSubscription %s: %d %v", sub, sr.code, sr.body)
			}
		}

		dr1 := doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+sub1+"?connection_id="+strconv.FormatInt(tgtConnID, 10), authorizedUser, nil)
		if dr1.code != http.StatusOK {
			t.Fatalf("drop sub1: %d %v", dr1.code, dr1.body)
		}
		if !publicationExists(pubName) {
			t.Error("publication was dropped while sub2's link still references it")
		}

		dr2 := doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+sub2+"?connection_id="+strconv.FormatInt(tgtConnID, 10), authorizedUser, nil)
		if dr2.code != http.StatusOK {
			t.Fatalf("drop sub2: %d %v", dr2.code, dr2.body)
		}
		if publicationExists(pubName) {
			t.Error("publication should be gone now that the last referencing link was dropped")
		}
	})
}

// mustCreate is a small shared helper for the drop-focused subtests that
// don't need to re-verify the create path (already covered by "full
// authorized flow" above).
func mustCreate(t *testing.T, doJSON pgReplTestDoFn, pubName, subName string, srcConnID, tgtConnID, userID int64) {
	t.Helper()
	pr := doJSON(http.MethodPost, "/api/pg-replication/publications", userID, map[string]any{
		"connectionId": srcConnID, "name": pubName, "allTables": false, "tables": []string{"public.widgets"},
	})
	if pr.code != http.StatusOK {
		t.Fatalf("setup CreatePublication: %d %v", pr.code, pr.body)
	}
	sr := doJSON(http.MethodPost, "/api/pg-replication/subscriptions", userID, map[string]any{
		"sourceConnectionId": srcConnID, "targetConnectionId": tgtConnID,
		"name": subName, "publicationName": pubName,
	})
	if sr.code != http.StatusOK {
		t.Fatalf("setup CreateSubscription: %d %v", sr.code, sr.body)
	}
}

func cleanupLink(t *testing.T, doJSON pgReplTestDoFn, src, tgt *sql.DB, pubName, subName string, tgtConnID, userID int64) {
	t.Helper()
	doJSON(http.MethodDelete, "/api/pg-replication/subscriptions/"+subName+"?connection_id="+strconv.FormatInt(tgtConnID, 10), userID, nil)
	tgt.Exec("DROP SUBSCRIPTION IF EXISTS " + quoteIdentPG(subName))
	src.Exec("DROP PUBLICATION IF EXISTS " + quoteIdentPG(pubName))
}
