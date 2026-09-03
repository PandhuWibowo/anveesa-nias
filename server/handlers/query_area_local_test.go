package handlers

// Scratch integration test for the SQL Studio multi-statement fixes
// (RunScript's transaction-awareness, comment-aware classification, and
// dollar-quote-safe splitting) — drives the real exported handlers
// end-to-end against a real local Postgres connections row, same
// convention as pg_replication_authz_local_test.go / pg_parameters_local_test.go.
//
// Run: QUERYAREA_TEST=1 go test ./handlers/ -run TestQueryArea -v

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"strconv"
	"testing"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	_ "github.com/lib/pq"
)

func TestQueryArea(t *testing.T) {
	if os.Getenv("QUERYAREA_TEST") != "1" {
		t.Skip("set QUERYAREA_TEST=1 to run against local nias_dev + local Postgres")
	}
	if err := appdb.Init(&config.Config{
		DBDriver: "postgres",
		DBURL:    "postgres://localhost:5432/nias_dev?sslmode=disable",
	}); err != nil {
		t.Skipf("nias_dev not reachable: %v", err)
	}
	conn := appdb.DB

	admin, err := sql.Open("postgres", "postgres://localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	// Registered via t.Cleanup (not `defer`) and added FIRST so, being
	// LIFO, it closes admin LAST — t.Cleanup callbacks all run after this
	// function's own defers/returns, so a `defer admin.Close()` here would
	// have closed the connection before the DB-drop cleanup below got to
	// use it, leaving the throwaway database behind silently (Exec on a
	// closed *sql.DB errors, which this test wasn't checking).
	t.Cleanup(func() { admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Skipf("local postgres not reachable: %v", err)
	}

	const targetDB = "nias_queryareatest"
	admin.Exec("DROP DATABASE IF EXISTS " + quoteIdentPG(targetDB) + " WITH (FORCE)")
	if _, err := admin.Exec("CREATE DATABASE " + quoteIdentPG(targetDB)); err != nil {
		t.Fatalf("create target db: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + quoteIdentPG(targetDB) + " WITH (FORCE)"); err != nil {
			t.Logf("cleanup: failed to drop %s: %v", targetDB, err)
		}
	})

	target, err := sql.Open("postgres", "postgres://localhost:5432/"+targetDB+"?sslmode=disable")
	if err != nil {
		t.Fatalf("open target conn: %v", err)
	}
	defer target.Close()
	if _, err := target.Exec(`CREATE TABLE widgets (id serial PRIMARY KEY, name text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := target.Exec(`INSERT INTO widgets (name) VALUES ('a'), ('b')`); err != nil {
		t.Fatalf("seed table: %v", err)
	}

	osUser, err := user.Current()
	if err != nil {
		t.Fatalf("could not determine current OS user: %v", err)
	}

	conn.Exec(`DELETE FROM connections WHERE name = 'queryareatest_conn'`)
	var connID int64
	if err := conn.QueryRow(
		`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, owner_id) VALUES ('queryareatest_conn','postgres','localhost',5432,$1,$2,'',0,999999999) RETURNING id`,
		targetDB, osUser.Username,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	t.Cleanup(func() {
		conn.Exec(`DELETE FROM connections WHERE id = $1`, connID)
	})

	connIDStr := strconv.FormatInt(connID, 10)

	postScript := func(sqlText string) (int, []ScriptResult) {
		body, _ := json.Marshal(map[string]string{"sql": sqlText})
		r := httptest.NewRequest(http.MethodPost, "/api/connections/"+connIDStr+"/script", bytes.NewReader(body))
		r.Header.Set("X-User-Role", "admin")
		rec := httptest.NewRecorder()
		RunScript()(rec, r)
		var results []ScriptResult
		json.Unmarshal(rec.Body.Bytes(), &results)
		return rec.Code, results
	}

	postQuery := func(sqlText string) (int, QueryResult) {
		body, _ := json.Marshal(map[string]string{"sql": sqlText})
		r := httptest.NewRequest(http.MethodPost, "/api/connections/"+connIDStr+"/query", bytes.NewReader(body))
		r.Header.Set("X-User-Role", "admin")
		rec := httptest.NewRecorder()
		ExecuteQuery()(rec, r)
		var result QueryResult
		json.Unmarshal(rec.Body.Bytes(), &result)
		return rec.Code, result
	}

	t.Run("single-statement /query endpoint still works unchanged", func(t *testing.T) {
		code, result := postQuery("SELECT * FROM widgets ORDER BY id;")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if result.RowCount != 2 {
			t.Errorf("expected 2 rows, got %d", result.RowCount)
		}
		if len(result.Columns) == 0 {
			t.Error("expected columns to be populated")
		}
	})

	t.Run("one failing statement in a script does not abort the others — each gets its own result", func(t *testing.T) {
		code, results := postScript("SELECT 1 AS n; SELECT * FROM not_a_real_table; SELECT 2 AS n;")
		if code != http.StatusOK {
			t.Fatalf("expected 200 (per-statement errors, not a request failure), got %d", code)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results (one per statement, including the failing one), got %d: %+v", len(results), results)
		}
		if results[0].Error != "" {
			t.Errorf("statement 1 should have succeeded, got error: %s", results[0].Error)
		}
		if results[1].Error == "" {
			t.Error("statement 2 (bad table) should have its own error, got none")
		}
		if results[2].Error != "" {
			t.Errorf("statement 3 should have run despite statement 2 failing, got error: %s", results[2].Error)
		}
	})

	t.Run("two SELECTs return two separate result sets with correct data", func(t *testing.T) {
		code, results := postScript("SELECT * FROM widgets ORDER BY id; SELECT count(*) FROM widgets;")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
		}
		if results[0].Error != "" {
			t.Errorf("statement 1 errored: %s", results[0].Error)
		}
		if results[0].RowCount != 2 {
			t.Errorf("statement 1: expected 2 rows, got %d", results[0].RowCount)
		}
		if results[1].Error != "" {
			t.Errorf("statement 2 errored: %s", results[1].Error)
		}
		if results[1].RowCount != 1 {
			t.Errorf("statement 2: expected 1 row (the count), got %d", results[1].RowCount)
		}
	})

	t.Run("a SELECT with a leading comment is still classified as a read and returns rows", func(t *testing.T) {
		code, results := postScript("-- fetch all widgets\nSELECT * FROM widgets ORDER BY id;")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Error != "" {
			t.Fatalf("statement errored (likely misclassified as a write): %s", results[0].Error)
		}
		if len(results[0].Columns) == 0 || results[0].RowCount != 2 {
			t.Errorf("expected the comment-led SELECT to return its rows, got columns=%v rowCount=%d", results[0].Columns, results[0].RowCount)
		}
	})

	t.Run("a dollar-quoted DO block with internal semicolons runs as one statement, not fragments", func(t *testing.T) {
		code, results := postScript(`DO $$ BEGIN INSERT INTO widgets (name) VALUES ('c'); INSERT INTO widgets (name) VALUES ('d'); END $$; SELECT count(*) FROM widgets;`)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 statements (DO block + final SELECT), got %d — dollar-quoted block was likely fragmented: %+v", len(results), results)
		}
		if results[0].Error != "" {
			t.Fatalf("DO block errored: %s", results[0].Error)
		}
		if results[1].Error != "" {
			t.Fatalf("count SELECT errored: %s", results[1].Error)
		}
		// 2 seeded + 2 inserted by the DO block = 4.
		var gotCount string
		if len(results[1].Rows) == 1 && len(results[1].Rows[0]) == 1 {
			gotCount = toString(results[1].Rows[0][0])
		}
		if gotCount != "4" {
			t.Errorf("expected widget count 4 after DO block, got %q", gotCount)
		}
	})

	t.Run("script respects an active transaction — rollback undoes what it ran", func(t *testing.T) {
		var before int
		target.QueryRow(`SELECT count(*) FROM widgets`).Scan(&before)

		beginReq := httptest.NewRequest(http.MethodPost, "/api/connections/"+connIDStr+"/transaction/begin", nil)
		beginRec := httptest.NewRecorder()
		BeginTransaction()(beginRec, beginReq)
		if beginRec.Code != http.StatusOK {
			t.Fatalf("BeginTransaction: expected 200, got %d: %s", beginRec.Code, beginRec.Body.String())
		}
		defer func() {
			rbReq := httptest.NewRequest(http.MethodPost, "/api/connections/"+connIDStr+"/transaction/rollback", nil)
			RollbackTransaction()(httptest.NewRecorder(), rbReq)
		}()

		code, results := postScript(`INSERT INTO widgets (name) VALUES ('tx-test-1'); INSERT INTO widgets (name) VALUES ('tx-test-2');`)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		for i, r := range results {
			if r.Error != "" {
				t.Fatalf("statement %d errored: %s", i, r.Error)
			}
		}

		// The pooled connection (outside the transaction) must NOT see these
		// rows yet — if it does, RunScript ran on the pool instead of the
		// active tx, silently bypassing the transaction the UI shows as
		// active.
		var duringTx int
		target.QueryRow(`SELECT count(*) FROM widgets`).Scan(&duringTx)
		if duringTx != before {
			t.Errorf("expected uncommitted inserts to be invisible outside the transaction: before=%d duringTx=%d", before, duringTx)
		}

		// Roll back now (the deferred call above also does this, but do it
		// explicitly here so we can assert the post-rollback count in this
		// same subtest).
		rbReq := httptest.NewRequest(http.MethodPost, "/api/connections/"+connIDStr+"/transaction/rollback", nil)
		rbRec := httptest.NewRecorder()
		RollbackTransaction()(rbRec, rbReq)
		if rbRec.Code != http.StatusOK {
			t.Fatalf("RollbackTransaction: expected 200, got %d: %s", rbRec.Code, rbRec.Body.String())
		}

		var after int
		target.QueryRow(`SELECT count(*) FROM widgets`).Scan(&after)
		if after != before {
			t.Errorf("expected rollback to undo the script's inserts: before=%d after=%d", before, after)
		}
	})
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
