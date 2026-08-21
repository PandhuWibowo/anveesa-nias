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

func TestVerifyRealScaleContinueOnError(t *testing.T) {
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

	body, _ := json.Marshal(map[string]interface{}{
		"dest_conn_id":      13,
		"object_key":        "backups/sandbox/backup_public_20260707_030927.sql.gz",
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

	start := time.Now()
	for i := 0; i < 600; i++ {
		time.Sleep(1 * time.Second)
		statusReq := httptest.NewRequest("GET", "/api/restore/jobs/"+jobResp.JobID, nil)
		statusRec := httptest.NewRecorder()
		GetRestoreJobStatus()(statusRec, statusReq)
		var status struct {
			Status        string `json:"status"`
			Executed      int64  `json:"executed"`
			FailedRows    int64  `json:"failed_rows"`
			FirstRowError string `json:"first_row_error"`
			Error         string `json:"error"`
			Current       string `json:"current"`
		}
		json.Unmarshal(statusRec.Body.Bytes(), &status)
		if i%20 == 0 {
			t.Logf("t=%s executed=%d failed_rows=%d current=%q", time.Since(start), status.Executed, status.FailedRows, status.Current)
		}
		if status.Status == "done" {
			t.Logf("DONE: elapsed=%s executed=%d failed_rows=%d first_row_error=%q", time.Since(start), status.Executed, status.FailedRows, status.FirstRowError)
			if status.FailedRows == 0 {
				t.Fatalf("expected at least the known bad-date row to be counted as failed, got 0")
			}
			return
		}
		if status.Status == "failed" {
			t.Fatalf("job aborted despite continue_on_error: %s", status.Error)
		}
	}
	t.Fatalf("timed out waiting for job")
}
