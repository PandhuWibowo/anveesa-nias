package handlers

// Scratch integration test for pg_parameters.go — exercises the real
// exported handlers (ListPgSettings, UpdatePgSetting, ReloadPgConfig)
// end-to-end against a real local Postgres connections row, same
// convention as pg_replication_authz_local_test.go.
//
// Run: PGPARAMS_TEST=1 go test ./handlers/ -run TestPgParameters -v

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

func TestPgParameters(t *testing.T) {
	if os.Getenv("PGPARAMS_TEST") != "1" {
		t.Skip("set PGPARAMS_TEST=1 to run against local nias_dev + local Postgres")
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
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("local postgres not reachable: %v", err)
	}

	conn.Exec(`DELETE FROM connections WHERE name = 'pgparamstest_conn'`)
	var connID int64
	if err := conn.QueryRow(
		`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, owner_id) VALUES ('pgparamstest_conn','postgres','localhost',5432,'postgres',$1,'',0,999999999) RETURNING id`,
		currentOSUser(t),
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	t.Cleanup(func() {
		conn.Exec(`DELETE FROM connections WHERE id = $1`, connID)
		conn.Exec(`DELETE FROM audit_log WHERE event_type = 'pg_parameters' AND conn_id = $1`, connID)
	})

	doReq := func(method, path string, body any) (int, map[string]any) {
		var r *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			r = httptest.NewRequest(method, path, bytes.NewReader(b))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("X-User-Role", "admin") // admin bypasses per-connection checks — this test is about the SQL/wiring, not authz (covered separately)
		rec := httptest.NewRecorder()
		switch {
		case method == http.MethodGet:
			ListPgSettings()(rec, r)
		case method == http.MethodPut:
			UpdatePgSetting()(rec, r)
		case method == http.MethodPost:
			ReloadPgConfig()(rec, r)
		}
		var decoded map[string]any
		json.Unmarshal(rec.Body.Bytes(), &decoded)
		return rec.Code, decoded
	}

	t.Run("ListPgSettings returns a populated, well-formed list", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/pg-parameters/settings?connection_id="+strconv.FormatInt(connID, 10), nil)
		r.Header.Set("X-User-Role", "admin")
		rec := httptest.NewRecorder()
		ListPgSettings()(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var settings []pgSetting
		if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(settings) < 100 {
			t.Fatalf("expected 100+ pg_settings rows, got %d", len(settings))
		}
		found := false
		for _, s := range settings {
			if s.Name == "max_connections" {
				found = true
				if s.Context != "postmaster" {
					t.Errorf("max_connections context = %q, want postmaster", s.Context)
				}
			}
		}
		if !found {
			t.Error("max_connections not found in listing")
		}
	})

	t.Run("UpdatePgSetting sets a user-context value, reload applies it, reset restores default", func(t *testing.T) {
		path := "/api/pg-parameters/settings/statement_timeout?connection_id=" + strconv.FormatInt(connID, 10)

		code, resp := doReq(http.MethodPut, path, map[string]any{"value": "45000"})
		if code != http.StatusOK {
			t.Fatalf("set: expected 200, got %d: %v", code, resp)
		}
		if resp["name"] != "statement_timeout" {
			t.Errorf("unexpected response name: %v", resp)
		}

		var applied string
		if err := admin.QueryRow(`SHOW statement_timeout`).Scan(&applied); err != nil {
			t.Fatalf("SHOW after set: %v", err)
		}
		// A fresh connection (admin's) should see the new server default —
		// only already-open sessions keep their pre-reload value.
		if applied != "45s" {
			t.Errorf("expected new session to see 45s, got %q", applied)
		}

		code, resp = doReq(http.MethodPut, path, map[string]any{"value": nil})
		if code != http.StatusOK {
			t.Fatalf("reset: expected 200, got %d: %v", code, resp)
		}

		var afterReset string
		if err := admin.QueryRow(`SHOW statement_timeout`).Scan(&afterReset); err != nil {
			t.Fatalf("SHOW after reset: %v", err)
		}
		if afterReset != "0" {
			t.Errorf("expected statement_timeout back to default 0 after reset, got %q", afterReset)
		}
	})

	t.Run("UpdatePgSetting rejects an unknown parameter", func(t *testing.T) {
		path := "/api/pg-parameters/settings/not_a_real_setting?connection_id=" + strconv.FormatInt(connID, 10)
		code, resp := doReq(http.MethodPut, path, map[string]any{"value": "x"})
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %v", code, resp)
		}
	})

	t.Run("ReloadPgConfig succeeds", func(t *testing.T) {
		path := "/api/pg-parameters/reload?connection_id=" + strconv.FormatInt(connID, 10)
		code, resp := doReq(http.MethodPost, path, nil)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", code, resp)
		}
		if resp["reloaded"] != true {
			t.Errorf("expected reloaded=true, got %v", resp)
		}
	})

	t.Run("audit trail recorded the set/reset actions", func(t *testing.T) {
		var count int
		conn.QueryRow(`SELECT count(*) FROM audit_log WHERE event_type = 'pg_parameters' AND conn_id = $1`, connID).Scan(&count)
		if count < 2 {
			t.Errorf("expected at least 2 audit_log rows (set + reset), got %d", count)
		}
	})
}

func currentOSUser(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatalf("could not determine current OS user: %v", err)
	}
	return u.Username
}
