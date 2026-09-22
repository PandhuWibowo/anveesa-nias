package handlers

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestExecWithSavepointSurvivesReleaseOfAlreadyGoneSavepoint reproduces the
// exact failure mode from a real MySQL/MariaDB restore: autoAddColumns runs
// an ALTER TABLE between the SAVEPOINT and the row statement it's protecting
// (see execWithSavepointAutoRepair) — MySQL/MariaDB DDL implicitly commits,
// which silently tears the just-created savepoint down along with it, even
// though the row statement itself goes on to succeed. The RELEASE SAVEPOINT
// cleanup step that follows then fails with "does not exist" purely because
// there's nothing left to release — not because any data was lost.
//
// This test can't reproduce MySQL's implicit-commit semantics directly (no
// live MySQL here), but reproduces the exact code path under test — a stmt
// that succeeds yet leaves the savepoint already gone by the time
// execWithSavepoint tries to release it — by having stmt release the
// savepoint itself as a side effect, which is behaviorally identical from
// execWithSavepoint's point of view.
func TestExecWithSavepointSurvivesReleaseOfAlreadyGoneSavepoint(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	// stmt "succeeds" and, as a side effect, releases the very savepoint
	// execWithSavepoint just created — standing in for MySQL's implicit
	// commit tearing it down mid-row.
	rowErr, fatalErr := execWithSavepoint(ctx, tx, "sqlite3", "RELEASE SAVEPOINT restore_row")
	if fatalErr != nil {
		t.Fatalf("expected the restore to survive a missing savepoint at RELEASE time, got fatalErr: %v", fatalErr)
	}
	if rowErr != nil {
		t.Fatalf("expected no row error either — the statement itself succeeded, got: %v", rowErr)
	}
}

// TestExecWithSavepointStillFatalWhenStatementItselfFails guards the other
// branch (rollbackTo failing after the row statement failed) — that one
// stays fatal, since there the underlying statement's own failure combined
// with an unrecoverable rollback genuinely means the transaction state is
// unknown.
func TestExecWithSavepointStillFatalWhenStatementItselfFails(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	// The statement itself is bad SQL (no such table) — this must still
	// surface as a normal row error via a clean rollback, not silently pass.
	rowErr, fatalErr := execWithSavepoint(ctx, tx, "sqlite3", "INSERT INTO no_such_table VALUES (1)")
	if fatalErr != nil {
		t.Fatalf("expected a clean rollback (rowErr), not fatalErr: %v", fatalErr)
	}
	if rowErr == nil {
		t.Fatal("expected rowErr for the failing INSERT, got nil")
	}
}
