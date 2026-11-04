package handlers

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanPGAccessReadOnlySchema(t *testing.T) {
	batches, err := planPGAccess("production_kiki_user", dbAccessRequest{
		Database:         "prod",
		Schema:           "public",
		Preset:           "readonly",
		IncludeSequences: true,
		IncludeFuture:    true,
	}, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`GRANT CONNECT ON DATABASE "prod" TO "production_kiki_user"`,
		`GRANT USAGE ON SCHEMA "public" TO "production_kiki_user"`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "production_kiki_user"`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA "public" TO "production_kiki_user"`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA "public" GRANT SELECT ON TABLES TO "production_kiki_user"`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA "public" GRANT USAGE, SELECT ON SEQUENCES TO "production_kiki_user"`,
	}
	if len(batches) != 1 || batches[0].Database != "prod" {
		t.Fatalf("unexpected batches: %+v", batches)
	}
	if !reflect.DeepEqual(batches[0].Statements, want) {
		t.Fatalf("statements mismatch\n got: %q\nwant: %q", batches[0].Statements, want)
	}
}

func TestPlanPGAccessFutureForRoles(t *testing.T) {
	batches, err := planPGAccess("ro", dbAccessRequest{
		Schema:         "app",
		Preset:         "readwrite",
		IncludeFuture:  true,
		FutureForRoles: []string{"migrator"},
	}, "appdb")
	if err != nil {
		t.Fatal(err)
	}
	if batches[0].Database != "appdb" {
		t.Fatalf("empty database should default to the connection's database, got %q", batches[0].Database)
	}
	last := batches[0].Statements[len(batches[0].Statements)-1]
	want := `ALTER DEFAULT PRIVILEGES FOR ROLE "migrator" IN SCHEMA "app" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "ro"`
	if last != want {
		t.Fatalf("got %q want %q", last, want)
	}
	for _, s := range batches[0].Statements {
		if strings.Contains(s, "SEQUENCES") {
			t.Fatalf("sequences not requested but got %q", s)
		}
	}
}

func TestPlanPGAccessRevokeAndRejects(t *testing.T) {
	batches, err := planPGAccess("ro", dbAccessRequest{Action: "revoke", Schema: "public", IncludeSequences: true}, "db")
	if err != nil {
		t.Fatal(err)
	}
	if got := batches[0].Statements[len(batches[0].Statements)-1]; got != `REVOKE ALL ON SCHEMA "public" FROM "ro"` {
		t.Fatalf("unexpected last revoke: %q", got)
	}
	if _, err := planPGAccess("ro", dbAccessRequest{Schema: "bad\nname"}, "db"); err == nil {
		t.Fatal("expected multi-line schema name to be rejected")
	}
	if _, err := planPGAccess("ro", dbAccessRequest{Schema: "s", Preset: "custom"}, "db"); err == nil {
		t.Fatal("expected custom preset without privileges to be rejected")
	}
	b, err := planPGAccess("ro", dbAccessRequest{Action: "revoke_default", Owner: "app", Schema: "public", ObjectType: "r"}, "db")
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Statements[0] != `ALTER DEFAULT PRIVILEGES FOR ROLE "app" IN SCHEMA "public" REVOKE ALL ON TABLES FROM "ro"` {
		t.Fatalf("unexpected revoke_default: %q", b[0].Statements[0])
	}
}

func TestPlanMySQLAccess(t *testing.T) {
	b, err := planMySQLAccess("app", "%", dbAccessRequest{Database: "shop", Preset: "readonly"})
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Statements[0] != "GRANT SELECT, SHOW VIEW ON `shop`.* TO 'app'@'%'" {
		t.Fatalf("unexpected: %q", b[0].Statements[0])
	}
}

func TestDBAccessScriptRoundTrip(t *testing.T) {
	in := grantBatch{Database: "prod", Statements: []string{
		`GRANT USAGE ON SCHEMA "public" TO "u"`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "u"`,
	}}
	out, ok := parseDBAccessScript(buildDBAccessScript(in))
	if !ok {
		t.Fatal("script not recognised")
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch: %+v vs %+v", in, out)
	}
	if _, ok := parseDBAccessScript("GRANT SELECT ON t TO u"); ok {
		t.Fatal("plain SQL must not be treated as a DB access script")
	}
}
