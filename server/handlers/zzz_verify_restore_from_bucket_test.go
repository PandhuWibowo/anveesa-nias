package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	"github.com/joho/godotenv"
)

func TestVerifyListPostgresObjects(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env found")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db init: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	dest, err := fetchBucketConn(13)
	if err != nil {
		t.Fatalf("fetchBucketConn: %v", err)
	}
	objects, err := listBucketObjects(context.Background(), dest, "")
	if err != nil {
		t.Fatalf("listBucketObjects: %v", err)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Size < objects[j].Size })
	for _, o := range objects {
		if strings.Contains(o.Key, "_public_") && (o.Size > 100*1024*1024) && (o.Size < 500*1024*1024) {
			t.Logf("candidate: %s size=%d", o.Key, o.Size)
		}
	}
}

func TestVerifyDumpToFile(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env found")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db init: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	dest, err := fetchBucketConn(13)
	if err != nil {
		t.Fatalf("fetchBucketConn: %v", err)
	}
	resp, err := openBucketObjectStream(context.Background(), dest, "backups/sandbox/backup_public_20260707_030927.sql.gz")
	if err != nil {
		t.Fatalf("openBucketObjectStream: %v", err)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	out, err := os.Create("/private/tmp/claude-501/-Users-pandhuwibowo-Portfolio-anveesa-anveesa-nias/b3b505ec-4c56-4000-a99e-ad9aede9deee/scratchpad/dump.sql")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer out.Close()
	n, err := io.Copy(out, gz)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	t.Logf("wrote %d bytes", n)
}

func TestVerifyReplayHeadFile(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env found")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db init: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	f, err := os.Open("/private/tmp/claude-501/-Users-pandhuwibowo-Portfolio-anveesa-anveesa-nias/b3b505ec-4c56-4000-a99e-ad9aede9deee/scratchpad/dump_head.sql")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	db, _, err := GetDB(16)
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	executed, skipped, err := execRestoreStream(context.Background(), tx, f)
	t.Logf("executed=%d skipped=%d err=%v", executed, skipped, err)
	if err != nil {
		tx.Rollback()
		t.Fatalf("execRestoreStream failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// Temporary manual verification for the streaming restore-from-bucket path
// against a large, real object. Not part of the permanent suite; deleted
// after use.
func TestVerifyRestoreFromBucketStreaming(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env found, skipping integration verification")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db init: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	target := "backups/sandbox/backup_public_20260707_030927.sql.gz"
	t.Logf("using object %s", target)

	start := time.Now()
	body, _ := json.Marshal(map[string]interface{}{
		"dest_conn_id": 13,
		"object_key":   target,
	})
	req := httptest.NewRequest("POST", "/api/connections/16/restore", bytes.NewReader(body))
	req.Header.Set("X-User-Role", "admin")
	rec := httptest.NewRecorder()
	RestoreBackup()(rec, req)
	t.Logf("elapsed=%s status=%d body=%s", time.Since(start), rec.Code, rec.Body.String())
	if rec.Code != 200 {
		t.Fatalf("restore-from-bucket failed: %s", rec.Body.String())
	}
}
