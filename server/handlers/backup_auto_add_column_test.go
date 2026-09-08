package handlers

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// TestMissingColumnFromErrPostgresReportedCase reproduces the exact error
// wording from the bug report this feature fixes: restoring a dump into a
// table that pre-exists on the target without a column ("feature_code")
// the dump's data references.
func TestMissingColumnFromErrPostgresReportedCase(t *testing.T) {
	pqErr := &pq.Error{
		Code:    "42703",
		Message: `column "feature_code" of relation "billing_ledger" does not exist`,
	}
	col, ok := missingColumnFromErr("postgres", pqErr)
	if !ok {
		t.Fatal("expected ok=true for the reported 42703 error")
	}
	if col != "feature_code" {
		t.Fatalf("column = %q, want feature_code", col)
	}
}

func TestMissingColumnFromErrMySQL(t *testing.T) {
	myErr := &mysql.MySQLError{Number: 1054, Message: "Unknown column 'feature_code' in 'field list'"}
	col, ok := missingColumnFromErr("mysql", myErr)
	if !ok || col != "feature_code" {
		t.Fatalf("got col=%q ok=%v, want feature_code/true", col, ok)
	}
}

func TestMissingColumnFromErrSQLite(t *testing.T) {
	err := sqliteNoColumnErr("table billing_ledger has no column named feature_code")
	col, ok := missingColumnFromErr("sqlite3", err)
	if !ok || col != "feature_code" {
		t.Fatalf("got col=%q ok=%v, want feature_code/true", col, ok)
	}
}

type sqliteNoColumnErr string

func (e sqliteNoColumnErr) Error() string { return string(e) }

func TestExecRestoreStreamAutoAddColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE billing_ledger (id INTEGER PRIMARY KEY, amount TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	dump := `INSERT INTO billing_ledger (id, amount, feature_code) VALUES (1, '10.00', 'abc');`

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	var columnsAdded int64
	var addedTable, addedCol, addedType string
	onColumnAdd := func(table, column, sqlType string) {
		addedTable, addedCol, addedType = table, column, sqlType
	}

	executed, skipped, execErr := execRestoreStream(
		context.Background(), tx, strings.NewReader(dump), "sqlite3",
		false, false, true, // skipConflicts, continueOnError, autoAddColumns
		nil, nil, nil, &columnsAdded,
		nil, nil, onColumnAdd,
	)
	if execErr != nil {
		t.Fatalf("execRestoreStream: %v", execErr)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if executed != 1 || skipped != 0 {
		t.Fatalf("executed=%d skipped=%d, want executed=1 skipped=0", executed, skipped)
	}
	if columnsAdded != 1 {
		t.Fatalf("columnsAdded=%d, want 1", columnsAdded)
	}
	if addedTable != "billing_ledger" || addedCol != "feature_code" || addedType != "TEXT" {
		t.Fatalf("onColumnAdd got (%q,%q,%q), want (billing_ledger,feature_code,TEXT)", addedTable, addedCol, addedType)
	}

	var featureCode string
	if err := db.QueryRow(`SELECT feature_code FROM billing_ledger WHERE id = 1`).Scan(&featureCode); err != nil {
		t.Fatalf("query repaired row: %v", err)
	}
	if featureCode != "abc" {
		t.Fatalf("feature_code = %q, want abc", featureCode)
	}
}

func TestExecRestoreStreamNoAutoAddColumnsAbortsAsBefore(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE billing_ledger (id INTEGER PRIMARY KEY, amount TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	dump := `INSERT INTO billing_ledger (id, amount, feature_code) VALUES (1, '10.00', 'abc');`

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	_, _, execErr := execRestoreStream(
		context.Background(), tx, strings.NewReader(dump), "sqlite3",
		false, false, false, // autoAddColumns off — must behave exactly as before this feature
		nil, nil, nil, nil,
		nil, nil, nil,
	)
	if execErr == nil {
		t.Fatal("expected execRestoreStream to fail without autoAddColumns, got nil error")
	}
}

func TestParseInsertColumnsAndFirstTuple(t *testing.T) {
	stmt := `INSERT INTO "public"."billing_ledger" ("id", "amount", "feature_code") VALUES (1, '10.00', 'abc')`
	tableRef, cols, vals, ok := parseInsertColumnsAndFirstTuple(stmt)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tableRef != `"public"."billing_ledger"` {
		t.Fatalf("tableRef = %q", tableRef)
	}
	wantCols := []string{"id", "amount", "feature_code"}
	for i, c := range wantCols {
		if cols[i] != c {
			t.Fatalf("cols[%d] = %q, want %q", i, cols[i], c)
		}
	}
	if vals[2] != "'abc'" {
		t.Fatalf("vals[2] = %q, want 'abc'", vals[2])
	}
}

func TestParseInsertColumnsAndFirstTupleNoColumnList(t *testing.T) {
	stmt := `INSERT INTO billing_ledger VALUES (1, '10.00', 'abc')`
	_, _, _, ok := parseInsertColumnsAndFirstTuple(stmt)
	if ok {
		t.Fatal("expected ok=false for a bare INSERT with no explicit column list")
	}
}

func TestInferLiteralBucket(t *testing.T) {
	cases := map[string]literalBucket{
		"NULL":      bucketText,
		"'hello'":   bucketText,
		"42":        bucketBigInt,
		"-3.14":     bucketDouble,
		"TRUE":      bucketBoolean,
		"nextval()": bucketText,
	}
	for lit, want := range cases {
		if got := inferLiteralBucket(lit); got != want {
			t.Errorf("inferLiteralBucket(%q) = %v, want %v", lit, got, want)
		}
	}
}
