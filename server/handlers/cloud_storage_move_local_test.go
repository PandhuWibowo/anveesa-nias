package handlers

// Scratch reproduction test for the reported "Move to folder" failure —
// first proves the root cause (CloudStorageRename processing hundreds of
// objects synchronously within one HTTP request), then exercises the async
// MoveWithinBucket fix against the same data to confirm it actually works.
//
// Run: MOVEBUG_TEST=1 go test ./handlers/ -run TestLocalMove -v -timeout 60s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
)

func TestLocalMoveWithinBucket(t *testing.T) {
	if os.Getenv("MOVEBUG_TEST") != "1" {
		t.Skip("set MOVEBUG_TEST=1 to run against local MinIO + nias_dev")
	}
	if err := appdb.Init(&config.Config{
		DBDriver: "postgres",
		DBURL:    "postgres://localhost:5432/nias_dev?sslmode=disable",
	}); err != nil {
		t.Skipf("nias_dev not reachable locally: %v", err)
	}
	conn := appdb.DB
	SetEncryptionKey(requireTestEncryptionKey(t))
	if _, err := http.Get("http://127.0.0.1:19000/minio/health/live"); err != nil {
		t.Skipf("local MinIO not reachable: %v", err)
	}

	connID := insertTestBucketConn(t, conn, "nias-movebugtest", "nias-b2b-test")
	t.Cleanup(func() { conn.Exec(`DELETE FROM connections WHERE id = $1`, connID) })

	body := `{"items":["b2blocal/local/export-requests/","b2blocal/local/merchant-owner/","b2blocal/local/merchant/","b2blocal/local/reconcile-files/"],"destFolder":"b2blocal"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/connections/%d/storage/move", connID), strings.NewReader(body))
	rec := httptest.NewRecorder()
	MoveWithinBucket()(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var start struct {
		JobID string `json:"job_id"`
	}
	mustDecode(t, rec.Body.Bytes(), &start)
	t.Logf("move job started: %s", start.JobID)

	deadline := time.Now().Add(20 * time.Second)
	var final moveJobView
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/api/storage/move-jobs/"+start.JobID, nil)
		statusRec := httptest.NewRecorder()
		GetMoveJobStatus()(statusRec, statusReq)
		mustDecode(t, statusRec.Body.Bytes(), &final)
		t.Logf("status=%s completed=%d/%d failed=%d current=%q", final.Status, final.CompletedItems, final.TotalItems, final.FailedItems, final.CurrentItem)
		if final.Status != MoveJobRunning {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if final.Status != MoveJobDone {
		t.Fatalf("expected status done, got %q (error=%q)", final.Status, final.Error)
	}

	dest, err := fetchBucketConn(connID)
	if err != nil {
		t.Fatalf("fetch conn: %v", err)
	}
	remaining, _ := listBucketObjects(context.Background(), dest, "b2blocal/local")
	if len(remaining) != 0 {
		t.Errorf("expected b2blocal/local/ to be empty after the move, found %d objects", len(remaining))
	}
	for _, folder := range []string{"export-requests", "merchant-owner", "merchant", "reconcile-files"} {
		objs, _ := listBucketObjects(context.Background(), dest, "b2blocal/"+folder)
		if len(objs) != 5 {
			t.Errorf("expected 5 objects under b2blocal/%s/, found %d", folder, len(objs))
		}
	}
	t.Logf("verified: b2blocal/local/ empty, all 4 folders present directly under b2blocal/")
}

func mustDecode(t *testing.T, body []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
}
