package handlers

// End-to-end Check & Sync scenarios against a real local Postgres. Creates two
// throwaway databases on localhost:5432 (acting as the source/publisher and
// target/subscriber connections), drives the actual comparePgTables /
// reconcilePgTable code paths, and asserts on real row counts after each run.
// Same convention as pg_replication_local_test.go — not for CI.
//
// Run: PGREPL_TEST=1 go test ./handlers/ -run TestReconcileScenarios -v

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func TestReconcileScenarios(t *testing.T) {
	if os.Getenv("PGREPL_TEST") != "1" {
		t.Skip("set PGREPL_TEST=1 to run against local Postgres")
	}
	ctx := context.Background()

	admin, err := sql.Open("postgres", "postgres://localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("local postgres not reachable: %v", err)
	}

	const srcDB = "nias_reconcile_src"
	const tgtDB = "nias_reconcile_tgt"
	cleanup := func() {
		admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentPG(srcDB)))
		admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentPG(tgtDB)))
	}
	cleanup()
	defer cleanup()
	if _, err := admin.Exec("CREATE DATABASE " + quoteIdentPG(srcDB)); err != nil {
		t.Fatalf("create src db: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + quoteIdentPG(tgtDB)); err != nil {
		t.Fatalf("create tgt db: %v", err)
	}

	src := openTestDB(t, srcDB)
	defer src.Close()
	tgt := openTestDB(t, tgtDB)
	defer tgt.Close()

	mustExec := func(db *sql.DB, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	rowCount := func(db *sql.DB, table string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM public."+quoteIdentPG(table)).Scan(&n); err != nil {
			t.Fatalf("count public.%s: %v", table, err)
		}
		return n
	}

	// ── Scenario 1: primary key — drift is detected, reconciled, and
	// target-only rows are never touched. ──────────────────────────────────
	t.Run("PK drift: insert missing, update differing, leave extra", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.orders (id int primary key, status text, total int)`)
		mustExec(tgt, `CREATE TABLE public.orders (id int primary key, status text, total int)`)
		// Source: rows 1..3. Target: has 1 (identical), 2 (differing status),
		// is missing 3, and has an extra row 99 the source never had.
		mustExec(src, `INSERT INTO public.orders VALUES (1,'paid',100),(2,'paid',200),(3,'pending',300)`)
		mustExec(tgt, `INSERT INTO public.orders VALUES (1,'paid',100),(2,'refunded',200),(99,'paid',999)`)

		cmp, err := comparePgTables(ctx, src, tgt, "public", "orders")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if cmp.MissingOnTarget != 1 || cmp.Differs != 1 || cmp.ExtraOnTarget != 1 || cmp.InSync {
			t.Fatalf("compare = %+v, want missing=1 differs=1 extra=1 in_sync=false", cmp)
		}

		rec, noPK, err := reconcilePgTable(ctx, src, tgt, "public", "orders")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if noPK {
			t.Error("expected the primary-key path, got no-PK")
		}
		if rec.Inserted != 1 || rec.Updated != 1 {
			t.Errorf("reconcile = %+v, want inserted=1 updated=1", rec)
		}
		// Extra target row 99 must survive; target now has 1,2,3,99 = 4 rows.
		if n := rowCount(tgt, "orders"); n != 4 {
			t.Errorf("target row count = %d, want 4 (extra row preserved)", n)
		}
		var status2 string
		tgt.QueryRowContext(ctx, `SELECT status FROM public.orders WHERE id=2`).Scan(&status2)
		if status2 != "paid" {
			t.Errorf("row 2 status = %q, want %q (updated to source)", status2, "paid")
		}

		// Re-running is a no-op except the still-reported extra row.
		cmp2, _ := comparePgTables(ctx, src, tgt, "public", "orders")
		if !cmp2.InSync || cmp2.MissingOnTarget != 0 || cmp2.Differs != 0 || cmp2.ExtraOnTarget != 1 {
			t.Errorf("re-compare = %+v, want in_sync=true missing=0 differs=0 extra=1", cmp2)
		}
		rec2, _, _ := reconcilePgTable(ctx, src, tgt, "public", "orders")
		if rec2.Inserted != 0 || rec2.Updated != 0 {
			t.Errorf("second reconcile = %+v, want no writes", rec2)
		}
	})

	// ── Scenario 2: THE robustness fix — same data, different physical column
	// order across the two servers must read as in-sync, not total drift. ───
	t.Run("PK column-order independence", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.acct (id int primary key, first_name text, last_name text)`)
		// Target declares the same columns in a different physical order.
		mustExec(tgt, `CREATE TABLE public.acct (id int primary key, last_name text, first_name text)`)
		mustExec(src, `INSERT INTO public.acct (id, first_name, last_name) VALUES (1,'ada','lovelace')`)
		mustExec(tgt, `INSERT INTO public.acct (id, first_name, last_name) VALUES (1,'ada','lovelace')`)

		cmp, err := comparePgTables(ctx, src, tgt, "public", "acct")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !cmp.InSync || cmp.Differs != 0 {
			t.Fatalf("compare = %+v, want in_sync=true differs=0 despite differing column order", cmp)
		}

		// A genuine value change is still caught, and reconcile fixes it even
		// though the two tables' physical layouts differ.
		mustExec(tgt, `UPDATE public.acct SET first_name='grace' WHERE id=1`)
		cmp2, _ := comparePgTables(ctx, src, tgt, "public", "acct")
		if cmp2.Differs != 1 {
			t.Fatalf("compare after edit = %+v, want differs=1", cmp2)
		}
		if _, _, err := reconcilePgTable(ctx, src, tgt, "public", "acct"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		var name string
		tgt.QueryRowContext(ctx, `SELECT first_name FROM public.acct WHERE id=1`).Scan(&name)
		if name != "ada" {
			t.Errorf("first_name = %q, want %q after reconcile", name, "ada")
		}
	})

	// ── Scenario 3: no primary key — multiset (count-aware) catch-up. ───────
	t.Run("no-PK multiset insert", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.tags (label text)`)
		mustExec(tgt, `CREATE TABLE public.tags (label text)`)
		mustExec(src, `INSERT INTO public.tags VALUES ('x'),('x'),('x')`) // 3 copies
		mustExec(tgt, `INSERT INTO public.tags VALUES ('x')`)             // 1 copy

		cmp, err := comparePgTables(ctx, src, tgt, "public", "tags")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !cmp.NoPrimaryKey || cmp.MissingOnTarget != 2 || cmp.InSync {
			t.Fatalf("compare = %+v, want no_primary_key=true missing=2 in_sync=false", cmp)
		}
		rec, noPK, err := reconcilePgTable(ctx, src, tgt, "public", "tags")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if !noPK || rec.Inserted != 2 {
			t.Fatalf("reconcile = %+v noPK=%v, want inserted=2 via no-PK path", rec, noPK)
		}
		if n := rowCount(tgt, "tags"); n != 3 {
			t.Errorf("target count = %d, want 3", n)
		}
		cmp2, _ := comparePgTables(ctx, src, tgt, "public", "tags")
		if !cmp2.InSync {
			t.Errorf("re-compare = %+v, want in_sync=true", cmp2)
		}
	})

	// ── Scenario 4: no primary key — surplus copies on the target are
	// reported but never deleted. ──────────────────────────────────────────
	t.Run("no-PK never deletes surplus", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.notes (body text)`)
		mustExec(tgt, `CREATE TABLE public.notes (body text)`)
		mustExec(src, `INSERT INTO public.notes VALUES ('a')`)
		mustExec(tgt, `INSERT INTO public.notes VALUES ('a'),('a'),('a')`)

		cmp, _ := comparePgTables(ctx, src, tgt, "public", "notes")
		if cmp.MissingOnTarget != 0 || cmp.ExtraOnTarget != 2 || !cmp.InSync {
			t.Fatalf("compare = %+v, want missing=0 extra=2 in_sync=true", cmp)
		}
		rec, _, err := reconcilePgTable(ctx, src, tgt, "public", "notes")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if rec.Inserted != 0 {
			t.Errorf("reconcile inserted %d, want 0", rec.Inserted)
		}
		if n := rowCount(tgt, "notes"); n != 3 {
			t.Errorf("target count = %d, want 3 (nothing deleted)", n)
		}
	})

	// ── Scenario 5: schema mismatch is refused up front (400), never a
	// half-broken compare or a mass-duplicating sync. ──────────────────────
	t.Run("schema mismatch is rejected", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.prod (id int primary key, name text)`)
		mustExec(tgt, `CREATE TABLE public.prod (id int primary key, name text, extra text)`)
		mustExec(src, `INSERT INTO public.prod VALUES (1,'a')`)
		mustExec(tgt, `INSERT INTO public.prod VALUES (1,'a','z')`)

		if _, err := comparePgTables(ctx, src, tgt, "public", "prod"); !errors.Is(err, errColumnMismatch) {
			t.Errorf("compare error = %v, want errColumnMismatch", err)
		}
		if _, _, err := reconcilePgTable(ctx, src, tgt, "public", "prod"); !errors.Is(err, errColumnMismatch) {
			t.Errorf("reconcile error = %v, want errColumnMismatch", err)
		}
	})

	// ── Scenario 6: no-PK, both sides non-empty but zero overlapping rows —
	// the format-mismatch guard flags compare and refuses the sync. ────────
	t.Run("no-PK zero-overlap guard", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.events (v int)`)
		mustExec(tgt, `CREATE TABLE public.events (v int)`)
		mustExec(src, `INSERT INTO public.events VALUES (1),(2),(3)`)
		mustExec(tgt, `INSERT INTO public.events VALUES (7),(8),(9)`)

		cmp, err := comparePgTables(ctx, src, tgt, "public", "events")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !cmp.FormatMismatchSuspected {
			t.Errorf("compare = %+v, want format_mismatch_suspected=true", cmp)
		}
		_, _, err = reconcilePgTable(ctx, src, tgt, "public", "events")
		if !errors.Is(err, errFormatMismatch) {
			t.Errorf("reconcile error = %v, want errFormatMismatch", err)
		}
		if n := rowCount(tgt, "events"); n != 3 {
			t.Errorf("target count = %d, want 3 (sync refused, nothing inserted)", n)
		}
	})

	// ── Scenario 7: an empty target is a legitimate first-time copy, NOT a
	// suspected mismatch — the guard must not block it. ────────────────────
	t.Run("no-PK empty target is a clean initial copy", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.seed (v int)`)
		mustExec(tgt, `CREATE TABLE public.seed (v int)`)
		mustExec(src, `INSERT INTO public.seed VALUES (1),(2),(2),(3)`)

		cmp, _ := comparePgTables(ctx, src, tgt, "public", "seed")
		if cmp.FormatMismatchSuspected {
			t.Errorf("compare = %+v, want format_mismatch_suspected=false for empty target", cmp)
		}
		if cmp.MissingOnTarget != 4 {
			t.Errorf("compare missing = %d, want 4", cmp.MissingOnTarget)
		}
		rec, _, err := reconcilePgTable(ctx, src, tgt, "public", "seed")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if rec.Inserted != 4 || rowCount(tgt, "seed") != 4 {
			t.Errorf("reconcile inserted=%d count=%d, want 4/4", rec.Inserted, rowCount(tgt, "seed"))
		}
	})

	// ── Scenario 8: a mid-batch write failure rolls the whole reconcile back,
	// never leaving the target partially synced. ───────────────────────────
	t.Run("transaction rolls back on mid-batch failure", func(t *testing.T) {
		mustExec(src, `CREATE TABLE public.ledger (id int primary key, amount int)`)
		// Target rejects negative amounts; the source holds one that violates it.
		mustExec(tgt, `CREATE TABLE public.ledger (id int primary key, amount int CHECK (amount >= 0))`)
		mustExec(src, `INSERT INTO public.ledger VALUES (1,10),(2,20),(3,-5),(4,30)`)

		_, _, err := reconcilePgTable(ctx, src, tgt, "public", "ledger")
		if err == nil {
			t.Fatal("expected reconcile to fail on the CHECK-violating row")
		}
		if n := rowCount(tgt, "ledger"); n != 0 {
			t.Errorf("target count = %d, want 0 — the failed batch must roll back entirely", n)
		}
	})
}

func openTestDB(t *testing.T, dbName string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:5432/%s?sslmode=disable", dbName))
	if err != nil {
		t.Fatalf("open %s: %v", dbName, err)
	}
	return db
}
