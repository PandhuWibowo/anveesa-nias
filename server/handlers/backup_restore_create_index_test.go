package handlers

import (
	"testing"

	"github.com/go-sql-driver/mysql"
)

// TestAddStatementIfNotExistsLeavesMySQLIndexAlone reproduces the exact
// syntax error from the bug report: MySQL/MariaDB's CREATE INDEX has no IF
// NOT EXISTS clause at all, so rewriting into that shape is a 1064 syntax
// error, not a harmless no-op the way it is for CREATE TABLE.
func TestAddStatementIfNotExistsLeavesMySQLIndexAlone(t *testing.T) {
	stmt := "CREATE INDEX `acquirer_issuer_lists_bank_vendor_id_foreign` ON `acquirer_issuer_lists` (`bank_vendor_id`)"

	for _, driver := range []string{"mysql", "mariadb"} {
		if got := addStatementIfNotExists(stmt, driver); got != stmt {
			t.Fatalf("[%s] expected CREATE INDEX left untouched, got %q", driver, got)
		}
	}
}

// TestAddStatementIfNotExistsRewritesIndexForPostgresAndSQLite covers the
// drivers that DO support the clause — behavior there must be unchanged.
func TestAddStatementIfNotExistsRewritesIndexForPostgresAndSQLite(t *testing.T) {
	stmt := `CREATE INDEX "idx_orders_customer" ON "orders" ("customer_id")`
	want := `CREATE INDEX IF NOT EXISTS "idx_orders_customer" ON "orders" ("customer_id")`

	for _, driver := range []string{"postgres", "sqlite", "sqlite3"} {
		if got := addStatementIfNotExists(stmt, driver); got != want {
			t.Fatalf("[%s] got %q, want %q", driver, got, want)
		}
	}
}

// TestAddStatementIfNotExistsStillRewritesCreateTableForMySQL makes sure the
// mysql/mariadb carve-out is scoped to CREATE INDEX only — CREATE TABLE IF
// NOT EXISTS is valid MySQL syntax and must still be applied.
func TestAddStatementIfNotExistsStillRewritesCreateTableForMySQL(t *testing.T) {
	stmt := "CREATE TABLE `orders` (`id` INT)"
	want := "CREATE TABLE IF NOT EXISTS `orders` (`id` INT)"
	if got := addStatementIfNotExists(stmt, "mysql"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestIsMySQLDuplicateIndexErr covers the execution-time fallback that makes
// CREATE INDEX idempotent for mysql/mariadb instead of the syntax rewrite.
func TestIsMySQLDuplicateIndexErr(t *testing.T) {
	dupErr := &mysql.MySQLError{Number: 1061, Message: "Duplicate key name 'idx_orders_customer'"}
	otherErr := &mysql.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"}

	if !isMySQLDuplicateIndexErr("mysql", dupErr) {
		t.Fatal("expected 1061 to be recognized as a duplicate index error")
	}
	if !isMySQLDuplicateIndexErr("mariadb", dupErr) {
		t.Fatal("expected 1061 to be recognized for mariadb too")
	}
	if isMySQLDuplicateIndexErr("mysql", otherErr) {
		t.Fatal("a syntax error must not be treated as a benign duplicate-index skip")
	}
	if isMySQLDuplicateIndexErr("postgres", dupErr) {
		t.Fatal("postgres never produces MySQL error codes — must not match")
	}

	// The bug-report case: re-running a restore hits an index name that's
	// now held by an already-created FK constraint, not another index.
	fkDupErr := &mysql.MySQLError{Number: 1826, Message: "Duplicate foreign key constraint name 'acquirer_issuer_lists_bank_vendor_id_foreign'"}
	if !isMySQLDuplicateIndexErr("mysql", fkDupErr) {
		t.Fatal("expected 1826 (duplicate FK constraint name) to also be tolerated")
	}
}

// TestIsAlterTableAddConstraintStatement covers the other statement shape
// that can hit error 1826 on a re-run: the FK-adding ALTER TABLE itself
// (generateFKsDDL), not just its supporting CREATE INDEX.
func TestIsAlterTableAddConstraintStatement(t *testing.T) {
	fk := "ALTER TABLE `acquirer_issuer_lists` ADD CONSTRAINT `acquirer_issuer_lists_bank_vendor_id_foreign` FOREIGN KEY (`bank_vendor_id`) REFERENCES `bank_vendors` (`id`)"
	if !isAlterTableAddConstraintStatement(fk) {
		t.Fatalf("expected %q to be recognized as an ADD CONSTRAINT statement", fk)
	}

	plainAlter := "ALTER TABLE `orders` ADD COLUMN `note` TEXT"
	if isAlterTableAddConstraintStatement(plainAlter) {
		t.Fatalf("a plain ALTER TABLE ADD COLUMN must not match: %q", plainAlter)
	}
}
