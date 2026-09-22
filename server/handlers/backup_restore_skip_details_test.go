package handlers

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestExecRestoreStreamOnSkipReportsUnsupportedStatement covers the "do we
// have any feature to see the skip statement?" ask: a statement shape
// isAllowedRestoreStatement doesn't recognize must be reported via onSkip
// with a reason, not just silently counted.
func TestExecRestoreStreamOnSkipReportsUnsupportedStatement(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// GRANT isn't in allowedRestoreStatements — a realistic "dump contains
	// something this restorer doesn't handle" case.
	dump := "GRANT ALL ON t TO someuser; INSERT INTO t (id) VALUES (1);"

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	var skippedStmt, skippedReason string
	onSkip := func(stmt, reason string) {
		skippedStmt, skippedReason = stmt, reason
	}

	executed, skipped, execErr := execRestoreStream(
		context.Background(), tx, strings.NewReader(dump), "sqlite3",
		false, false, false,
		"",
		nil, nil, nil, nil,
		nil, nil, nil, onSkip,
	)
	if execErr != nil {
		t.Fatalf("execRestoreStream: %v", execErr)
	}
	if executed != 1 || skipped != 1 {
		t.Fatalf("executed=%d skipped=%d, want 1/1", executed, skipped)
	}
	if !strings.Contains(skippedStmt, "GRANT") {
		t.Fatalf("onSkip got stmt=%q, want it to contain the GRANT statement", skippedStmt)
	}
	if skippedReason != "unsupported statement type for restore" {
		t.Fatalf("onSkip got reason=%q", skippedReason)
	}
}
