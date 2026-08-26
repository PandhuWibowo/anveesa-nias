package handlers

// Scratch integration test against the local Postgres instance — exercises
// the real DDL/connection-string logic (pgConnInfoString escaping,
// quoteIdentPG/quoteSchemaTable, CREATE PUBLICATION/SUBSCRIPTION statement
// construction) without touching the app's own database or any real Nias
// connection. Creates two throwaway databases on localhost:5432, connects
// as the local trust-authenticated OS superuser role, and drops them again.
// Not meant to be committed / run in CI, same convention as
// cloud_storage_local_test.go.
//
// Run: PGREPL_TEST=1 go test ./handlers/ -run TestLocalPgReplication -v

import (
	"database/sql"
	"fmt"
	"os"
	"os/user"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

func TestLocalPgReplication(t *testing.T) {
	if os.Getenv("PGREPL_TEST") != "1" {
		t.Skip("set PGREPL_TEST=1 to run against local Postgres")
	}

	osUser, err := user.Current()
	if err != nil {
		t.Fatalf("could not determine current OS user: %v", err)
	}
	username := osUser.Username

	admin, err := sql.Open("postgres", "postgres://localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("local postgres not reachable: %v", err)
	}

	const srcDB = "nias_pgrepltest_src"
	const tgtDB = "nias_pgrepltest_tgt"
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

	src, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:5432/%s?sslmode=disable", srcDB))
	if err != nil {
		t.Fatalf("open src conn: %v", err)
	}
	defer src.Close()
	tgt, err := sql.Open("postgres", fmt.Sprintf("postgres://localhost:5432/%s?sslmode=disable", tgtDB))
	if err != nil {
		t.Fatalf("open tgt conn: %v", err)
	}
	defer tgt.Close()

	t.Run("quoteSchemaTable rejects a bare table name", func(t *testing.T) {
		if _, err := quoteSchemaTable("orders"); err == nil {
			t.Error("expected an error for a table reference without a schema")
		}
	})

	t.Run("CREATE PUBLICATION via the exact statement-building logic", func(t *testing.T) {
		if _, err := src.Exec(`CREATE TABLE public.widgets (id serial PRIMARY KEY, name text)`); err != nil {
			t.Fatalf("create table: %v", err)
		}
		ident, err := quoteSchemaTable("public.widgets")
		if err != nil {
			t.Fatalf("quoteSchemaTable: %v", err)
		}
		stmt := "CREATE PUBLICATION " + quoteIdentPG("nias_test_pub") + " FOR TABLE " + ident + " WITH (publish = 'insert,update,delete')"
		if _, err := src.Exec(stmt); err != nil {
			t.Fatalf("CREATE PUBLICATION failed: %v\nstatement: %s", err, stmt)
		}

		var pubinsert, pubupdate, pubdelete, pubtruncate bool
		err = src.QueryRow(`SELECT pubinsert, pubupdate, pubdelete, pubtruncate FROM pg_publication WHERE pubname = 'nias_test_pub'`).
			Scan(&pubinsert, &pubupdate, &pubdelete, &pubtruncate)
		if err != nil {
			t.Fatalf("read back publication: %v", err)
		}
		if !pubinsert || !pubupdate || !pubdelete || pubtruncate {
			t.Errorf("unexpected publish flags: insert=%v update=%v delete=%v truncate=%v (want true,true,true,false)", pubinsert, pubupdate, pubdelete, pubtruncate)
		}

		var tableCount int
		src.QueryRow(`SELECT count(*) FROM pg_publication_tables WHERE pubname = 'nias_test_pub' AND schemaname = 'public' AND tablename = 'widgets'`).Scan(&tableCount)
		if tableCount != 1 {
			t.Errorf("expected widgets to be in the publication, got count=%d", tableCount)
		}
	})

	t.Run("CREATE SUBSCRIPTION connection string round-trips through pgConnInfoString", func(t *testing.T) {
		// Logical replication requires the target table to already exist —
		// documented in the feature's own UI prerequisites banner — so the
		// subscriber side needs the same table shape as the publisher.
		if _, err := tgt.Exec(`CREATE TABLE public.widgets (id serial PRIMARY KEY, name text)`); err != nil {
			t.Fatalf("create target table: %v", err)
		}
		// Deliberately includes a single quote and a backslash in the
		// password — exactly the characters pgConnInfoString's escaping
		// exists to handle. Local auth is trust, so the actual value is
		// never checked, but the string still has to *parse* correctly for
		// the test to reach Postgres's own "wal_level" error instead of a
		// connection-string syntax error.
		info := &pgConnInfo{
			Host:     "localhost",
			Port:     5432,
			Database: srcDB,
			Username: username,
			Password: `w${eird'pass\word`,
			SSL:      false,
		}
		connStr := pgConnInfoString(info)
		stmt := fmt.Sprintf("CREATE SUBSCRIPTION %s CONNECTION '%s' PUBLICATION %s",
			quoteIdentPG("nias_test_sub"), escapePGLiteral(connStr), quoteIdentPG("nias_test_pub"))

		_, err := tgt.Exec(stmt)
		if err == nil {
			t.Log("CREATE SUBSCRIPTION succeeded outright (unexpected on wal_level=replica, but not a failure) — replication is live")
			tgt.Exec("DROP SUBSCRIPTION " + quoteIdentPG("nias_test_sub"))
			return
		}
		// The only acceptable failure mode is Postgres rejecting the
		// replication attempt for a server-side reason (wal_level) — that
		// means the connection string was parsed and authenticated fine,
		// which is what this test exists to prove. Any other error (parse
		// error, auth failure, role/database not found) means
		// pgConnInfoString produced a broken connection string.
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "wal_level") && !strings.Contains(msg, "logical decoding") {
			t.Fatalf("expected a wal_level-related failure (proving the connection string worked), got: %v\nstatement: %s", err, stmt)
		}
		t.Logf("got expected wal_level failure (connection string parsed/authenticated correctly): %v", err)
	})

	t.Run("publish operations subset — insert-only publication", func(t *testing.T) {
		if _, err := src.Exec(`CREATE TABLE public.gadgets (id serial PRIMARY KEY)`); err != nil {
			t.Fatalf("create table: %v", err)
		}
		ident, err := quoteSchemaTable("public.gadgets")
		if err != nil {
			t.Fatalf("quoteSchemaTable: %v", err)
		}
		stmt := "CREATE PUBLICATION " + quoteIdentPG("nias_test_pub_insertonly") + " FOR TABLE " + ident + " WITH (publish = 'insert')"
		if _, err := src.Exec(stmt); err != nil {
			t.Fatalf("CREATE PUBLICATION failed: %v", err)
		}
		var pubinsert, pubupdate, pubdelete bool
		err = src.QueryRow(`SELECT pubinsert, pubupdate, pubdelete FROM pg_publication WHERE pubname = 'nias_test_pub_insertonly'`).
			Scan(&pubinsert, &pubupdate, &pubdelete)
		if err != nil {
			t.Fatalf("read back publication: %v", err)
		}
		if !pubinsert || pubupdate || pubdelete {
			t.Errorf("insert-only publication has wrong flags: insert=%v update=%v delete=%v (want true,false,false)", pubinsert, pubupdate, pubdelete)
		}
	})

	t.Run("CREATE PUBLICATION with FOR ALL TABLES", func(t *testing.T) {
		stmt := "CREATE PUBLICATION " + quoteIdentPG("nias_test_pub_all") + " FOR ALL TABLES"
		if _, err := src.Exec(stmt); err != nil {
			t.Fatalf("CREATE PUBLICATION FOR ALL TABLES failed: %v", err)
		}
		var allTables bool
		src.QueryRow(`SELECT puballtables FROM pg_publication WHERE pubname = 'nias_test_pub_all'`).Scan(&allTables)
		if !allTables {
			t.Error("expected puballtables = true")
		}
		src.Exec("DROP PUBLICATION " + quoteIdentPG("nias_test_pub_all"))
	})

	t.Run("duplicate publication name is rejected by Postgres, not silently accepted", func(t *testing.T) {
		name := "nias_test_pub_dup"
		stmt := "CREATE PUBLICATION " + quoteIdentPG(name) + " FOR ALL TABLES"
		if _, err := src.Exec(stmt); err != nil {
			t.Fatalf("first CREATE PUBLICATION failed: %v", err)
		}
		defer src.Exec("DROP PUBLICATION " + quoteIdentPG(name))
		if _, err := src.Exec(stmt); err == nil {
			t.Error("expected the second CREATE PUBLICATION with the same name to fail")
		} else if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			t.Errorf(`expected an "already exists" error, got: %v`, err)
		}
	})

	t.Run("publication referencing a nonexistent table surfaces Postgres's own error", func(t *testing.T) {
		ident, err := quoteSchemaTable("public.does_not_exist_table")
		if err != nil {
			t.Fatalf("quoteSchemaTable: %v", err)
		}
		stmt := "CREATE PUBLICATION " + quoteIdentPG("nias_test_pub_missing") + " FOR TABLE " + ident
		_, err = src.Exec(stmt)
		if err == nil {
			src.Exec("DROP PUBLICATION " + quoteIdentPG("nias_test_pub_missing"))
			t.Fatal("expected CREATE PUBLICATION to fail for a nonexistent table")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			t.Errorf("expected a \"does not exist\" error, got: %v", err)
		}
	})

	t.Run("DROP PUBLICATION actually removes it", func(t *testing.T) {
		name := "nias_test_pub_dropme"
		if _, err := src.Exec("CREATE PUBLICATION " + quoteIdentPG(name) + " FOR ALL TABLES"); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := src.Exec("DROP PUBLICATION " + quoteIdentPG(name)); err != nil {
			t.Fatalf("drop: %v", err)
		}
		var count int
		src.QueryRow(`SELECT count(*) FROM pg_publication WHERE pubname = $1`, name).Scan(&count)
		if count != 0 {
			t.Errorf("expected publication to be gone after DROP, still found %d row(s)", count)
		}
	})

	t.Run("quoteIdentPG defeats identifier injection via an embedded double quote", func(t *testing.T) {
		// A name like `evil"; DROP TABLE widgets; --` must round-trip as a
		// single (harmless) identifier, not break out of the quoted
		// identifier and execute a second statement.
		evilName := `evil"; DROP TABLE public.widgets; --`
		quoted := quoteIdentPG(evilName)
		if _, err := src.Exec("CREATE PUBLICATION " + quoted + " FOR ALL TABLES"); err != nil {
			t.Fatalf("expected the malicious-looking name to be accepted as a literal identifier, got error: %v", err)
		}
		defer src.Exec("DROP PUBLICATION " + quoted)

		var count int
		src.QueryRow(`SELECT count(*) FROM pg_publication WHERE pubname = $1`, evilName).Scan(&count)
		if count != 1 {
			t.Errorf("expected exactly one publication literally named %q, found %d", evilName, count)
		}
		// The real assertion: widgets must still exist — if the injection
		// had worked, DROP TABLE would have run as a second statement.
		var tableStillExists int
		src.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='widgets'`).Scan(&tableStillExists)
		if tableStillExists != 1 {
			t.Error("widgets table was dropped — identifier quoting was broken by the injection attempt")
		}
	})

	t.Run("quoteSchemaTable rejects malformed references", func(t *testing.T) {
		for _, bad := range []string{"", "noschema", "schema.", ".table"} {
			if _, err := quoteSchemaTable(bad); err == nil {
				t.Errorf("expected quoteSchemaTable(%q) to fail, it did not", bad)
			}
		}
	})

	t.Run("quoteSchemaTable splits on the first dot only, for a table name that itself contains a dot", func(t *testing.T) {
		ident, err := quoteSchemaTable("a.b.c")
		if err != nil {
			t.Fatalf("expected a.b.c to be accepted as schema=a, table=\"b.c\", got error: %v", err)
		}
		if ident != `"a"."b.c"` {
			t.Errorf(`expected "a"."b.c", got %s`, ident)
		}
	})

	t.Run("has_replication_privilege detection reflects the connecting role's actual privilege", func(t *testing.T) {
		// The superuser role this whole test runs as should report true —
		// exercising the exact query ListPublications uses.
		var hasPriv bool
		if err := src.QueryRow(`SELECT rolreplication OR rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&hasPriv); err != nil {
			t.Fatalf("privilege query failed: %v", err)
		}
		if !hasPriv {
			t.Error("expected the local superuser test role to report has_replication_privilege = true")
		}

		// A freshly created, unprivileged role should report false.
		lowPrivRole := "nias_pgrepltest_lowpriv"
		admin.Exec("DROP ROLE IF EXISTS " + quoteIdentPG(lowPrivRole))
		if _, err := admin.Exec("CREATE ROLE " + quoteIdentPG(lowPrivRole) + " LOGIN NOSUPERUSER NOREPLICATION"); err != nil {
			t.Fatalf("create low-priv role: %v", err)
		}
		defer func() {
			// GRANT CONNECT leaves a pg_shdepend entry that blocks DROP
			// ROLE until revoked — dropping the database it was granted on
			// doesn't retroactively clear it, so this has to be explicit.
			admin.Exec(fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM %s", quoteIdentPG(srcDB), quoteIdentPG(lowPrivRole)))
			if _, err := admin.Exec("DROP ROLE " + quoteIdentPG(lowPrivRole)); err != nil {
				t.Logf("cleanup: failed to drop role %s: %v", lowPrivRole, err)
			}
		}()
		if _, err := admin.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quoteIdentPG(srcDB), quoteIdentPG(lowPrivRole))); err != nil {
			t.Fatalf("grant connect: %v", err)
		}

		lowPrivConn, err := sql.Open("postgres", fmt.Sprintf("postgres://%s@localhost:5432/%s?sslmode=disable", lowPrivRole, srcDB))
		if err != nil {
			t.Fatalf("open low-priv conn: %v", err)
		}

		var lowPrivHasPriv bool
		queryErr := lowPrivConn.QueryRow(`SELECT rolreplication OR rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&lowPrivHasPriv)
		// Close synchronously and confirm it before DROP ROLE runs (the
		// deferred one above only fires after this closure returns) — a
		// role can't be dropped while a backend is still connected as it,
		// and Postgres closing out that backend is not instantaneous with
		// the client-side Close() call returning.
		if closeErr := lowPrivConn.Close(); closeErr != nil {
			t.Logf("closing low-priv connection: %v", closeErr)
		}
		if queryErr != nil {
			t.Fatalf("privilege query as low-priv role failed: %v", queryErr)
		}
		if lowPrivHasPriv {
			t.Error("expected the unprivileged role to report has_replication_privilege = false")
		}
	})
}
