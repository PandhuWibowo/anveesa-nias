package handlers

import "testing"

// TestMysqlRestoreDBRewriterDetectsFromCreateDatabase covers dumps generated
// after the CREATE DATABASE IF NOT EXISTS statement was added — the common
// case going forward.
func TestMysqlRestoreDBRewriterDetectsFromCreateDatabase(t *testing.T) {
	rw := &mysqlRestoreDBRewriter{destDatabase: "target_db"}

	create := rw.rewrite("CREATE DATABASE IF NOT EXISTS `singa-pg-mono`;")
	if create != "CREATE DATABASE IF NOT EXISTS `target_db`;" {
		t.Fatalf("CREATE DATABASE not rewritten: %q", create)
	}

	table := rw.rewrite("CREATE TABLE `singa-pg-mono`.`orders` (`id` INT)")
	if table != "CREATE TABLE `target_db`.`orders` (`id` INT)" {
		t.Fatalf("CREATE TABLE not rewritten: %q", table)
	}

	insert := rw.rewrite("INSERT INTO `singa-pg-mono`.`orders` (`id`) VALUES (1), (2)")
	if insert != "INSERT INTO `target_db`.`orders` (`id`) VALUES (1), (2)" {
		t.Fatalf("INSERT not rewritten: %q", insert)
	}
}

// TestMysqlRestoreDBRewriterDetectsFromQualifiedReference covers dumps taken
// before the CREATE DATABASE statement existed — detection must fall back to
// the first ordinary db-qualified reference instead.
func TestMysqlRestoreDBRewriterDetectsFromQualifiedReference(t *testing.T) {
	rw := &mysqlRestoreDBRewriter{destDatabase: "target_db"}

	table := rw.rewrite("CREATE TABLE `singa-pg-mono`.`orders` (`id` INT)")
	if table != "CREATE TABLE `target_db`.`orders` (`id` INT)" {
		t.Fatalf("CREATE TABLE not rewritten: %q", table)
	}

	fk := rw.rewrite("ALTER TABLE `singa-pg-mono`.`orders` ADD CONSTRAINT fk1 FOREIGN KEY (`customer_id`) REFERENCES `singa-pg-mono`.`customers` (`id`)")
	want := "ALTER TABLE `target_db`.`orders` ADD CONSTRAINT fk1 FOREIGN KEY (`customer_id`) REFERENCES `target_db`.`customers` (`id`)"
	if fk != want {
		t.Fatalf("ALTER TABLE not fully rewritten:\n got  %q\n want %q", fk, want)
	}
}

// TestMysqlRestoreDBRewriterLeavesRowDataAlone makes sure the rewrite never
// touches string-literal row data, even data that happens to contain a
// backtick — only the identifier-shaped `db`.`table` qualifier is a target.
func TestMysqlRestoreDBRewriterLeavesRowDataAlone(t *testing.T) {
	rw := &mysqlRestoreDBRewriter{destDatabase: "target_db"}
	rw.detect("CREATE TABLE `singa-pg-mono`.`notes` (`id` INT)")

	stmt := "INSERT INTO `singa-pg-mono`.`notes` (`id`, `body`) VALUES (1, 'uses a `backtick` in prose')"
	got := rw.rewrite(stmt)
	want := "INSERT INTO `target_db`.`notes` (`id`, `body`) VALUES (1, 'uses a `backtick` in prose')"
	if got != want {
		t.Fatalf("row data was altered:\n got  %q\n want %q", got, want)
	}
}

// TestMysqlRestoreDBRewriterNoOpWithoutDestDatabase preserves today's
// behavior exactly when the feature isn't opted into.
func TestMysqlRestoreDBRewriterNoOpWithoutDestDatabase(t *testing.T) {
	rw := &mysqlRestoreDBRewriter{}
	stmt := "CREATE TABLE `singa-pg-mono`.`orders` (`id` INT)"
	if got := rw.rewrite(stmt); got != stmt {
		t.Fatalf("expected no-op, got %q", got)
	}
}

// TestMysqlRestoreDBRewriterSameName is a same-name restore (destDatabase
// happens to equal the source) — must not needlessly touch the statement.
func TestMysqlRestoreDBRewriterSameName(t *testing.T) {
	rw := &mysqlRestoreDBRewriter{destDatabase: "singa-pg-mono"}
	stmt := "CREATE TABLE `singa-pg-mono`.`orders` (`id` INT)"
	if got := rw.rewrite(stmt); got != stmt {
		t.Fatalf("expected no-op for identical db name, got %q", got)
	}
}
