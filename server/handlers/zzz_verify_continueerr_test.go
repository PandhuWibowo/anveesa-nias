package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	"github.com/joho/godotenv"
)

func TestVerifyContinueOnError(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env found")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	// One good row, one row with the exact reported bad-date value, one more
	// good row after it — with continue_on_error, the bad row should be
	// skipped and the two good ones (plus the CREATE TABLE) should commit.
	dump := `CREATE TABLE "public"."zzz_continueerr_test" (
    "id" bigint NOT NULL,
    "logged_date" timestamp,
    PRIMARY KEY ("id")
);
INSERT INTO "public"."zzz_continueerr_test" ("id", "logged_date") VALUES (1, '2026-01-01 00:00:00');
INSERT INTO "public"."zzz_continueerr_test" ("id", "logged_date") VALUES (2, '0000-01-01 18:00:00');
INSERT INTO "public"."zzz_continueerr_test" ("id", "logged_date") VALUES (3, '2026-01-02 00:00:00');
`

	body, _ := json.Marshal(map[string]interface{}{
		"sql":               dump,
		"continue_on_error": true,
	})
	req := httptest.NewRequest("POST", "/api/connections/22/restore", bytes.NewReader(body))
	req.Header.Set("X-User-Role", "admin")
	rec := httptest.NewRecorder()
	RestoreBackup()(rec, req)
	if rec.Code != 202 {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var jobResp struct {
		JobID string `json:"job_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &jobResp)

	var final struct {
		Status        string `json:"status"`
		Executed      int64  `json:"executed"`
		FailedRows    int64  `json:"failed_rows"`
		FirstRowError string `json:"first_row_error"`
		Error         string `json:"error"`
	}
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		statusReq := httptest.NewRequest("GET", "/api/restore/jobs/"+jobResp.JobID, nil)
		statusRec := httptest.NewRecorder()
		GetRestoreJobStatus()(statusRec, statusReq)
		json.Unmarshal(statusRec.Body.Bytes(), &final)
		if final.Status == "done" || final.Status == "failed" {
			break
		}
	}
	t.Logf("status=%s executed=%d failed_rows=%d first_row_error=%q job_error=%q",
		final.Status, final.Executed, final.FailedRows, final.FirstRowError, final.Error)

	if final.Status != "done" {
		t.Fatalf("expected job to succeed overall despite the bad row, got status=%s error=%s", final.Status, final.Error)
	}
	if final.FailedRows != 1 {
		t.Fatalf("expected exactly 1 failed row, got %d", final.FailedRows)
	}
	if final.FirstRowError == "" {
		t.Fatalf("expected first_row_error to be captured")
	}

	db, _, _ := GetDB(22)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "public"."zzz_continueerr_test"`).Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 good rows to have committed, got %d", count)
	}
	var ids []int
	rows, _ := db.Query(`SELECT id FROM "public"."zzz_continueerr_test" ORDER BY id`)
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	t.Logf("committed row ids: %v (expect [1 3], skipping bad row 2)", ids)
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("expected rows [1 3] to be present, got %v", ids)
	}
}
