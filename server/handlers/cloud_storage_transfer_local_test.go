package handlers

// Scratch integration test against a local MinIO instance (same convention
// as cloud_storage_local_test.go) — runs a REAL transfer job through the
// actual TransferToBuckets/GetTransferJobStatus handlers (called directly,
// bypassing HTTP auth/routing since this only needs the handler logic
// itself) and polls live progress the exact same way the frontend's
// progress dock does, printing each snapshot. This is how "check the UI, I
// need movement details from live progress" gets verified without a real
// browser or the user's actual cloud credentials: the dock renders exactly
// this JSON, so seeing it change here over time is seeing what the dock
// would show.
//
// Requires local MinIO on 127.0.0.1:19000 (root user/pass
// testaccesskey/testsecretkey12345) with nias-transfer-src seeded and
// nias-transfer-dst existing, and the local nias_dev app database.
//
// Run: TRANSFER_TEST=1 go test ./handlers/ -run TestLocalTransferLiveProgress -v -timeout 60s

import (
	"database/sql"
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

func TestLocalTransferLiveProgress(t *testing.T) {
	if os.Getenv("TRANSFER_TEST") != "1" {
		t.Skip("set TRANSFER_TEST=1 to run against local MinIO + nias_dev")
	}

	// db.Init (not a bare sql.Open + assigning db.DB) matters here — same
	// lesson as middleware/pg_replication_permission_local_test.go:
	// ConvertQuery's ?->$1 rewriting reads the package-level dbDriver var
	// that only Init sets, and fetchBucketConn's query goes through it.
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

	srcConnID := insertTestBucketConn(t, conn, "nias-progresstest-src", "nias-transfer-src")
	dstConnID := insertTestBucketConn(t, conn, "nias-progresstest-dst", "nias-transfer-dst")
	t.Cleanup(func() {
		conn.Exec(`DELETE FROM connections WHERE id IN ($1,$2)`, srcConnID, dstConnID)
	})

	// ── Kick off the transfer exactly as CloudStorageView.vue's
	// submitTransfer() does — same request body shape, same handler.
	body := fmt.Sprintf(`{"items":["bigset/"],"destinations":[{"connectionId":%d,"prefix":"nias-progress-test/"}],"mode":"copy","conflictPolicy":"skip"}`, dstConnID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/connections/%d/storage/transfer", srcConnID), strings.NewReader(body))
	rec := httptest.NewRecorder()
	TransferToBuckets()(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	var startResp struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	t.Logf("transfer job started: %s", startResp.JobID)

	// ── Poll exactly like the frontend's pollTransferJob() does — same
	// endpoint, same 1.5s-ish cadence — logging every snapshot so the
	// sequence of values below is literally what the progress dock renders
	// over the course of this transfer.
	deadline := time.Now().Add(30 * time.Second)
	var lastStatus string
	pollCount := 0
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/api/storage/transfer-jobs/"+startResp.JobID, nil)
		statusRec := httptest.NewRecorder()
		GetTransferJobStatus()(statusRec, statusReq)

		var snap transferJobView
		if err := json.Unmarshal(statusRec.Body.Bytes(), &snap); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		pollCount++
		t.Logf("poll #%d: status=%s completed=%d/%d failed=%d skipped=%d transferred=%d/%d bytes current_item=%q",
			pollCount, snap.Status, snap.CompletedItems, snap.TotalItems, snap.FailedItems, snap.SkippedItems,
			snap.TransferredBytes, snap.TotalBytes, snap.CurrentItem)

		lastStatus = string(snap.Status)
		if snap.Status != TransferJobRunning {
			for i, r := range snap.Results {
				if i >= 3 {
					break
				}
				t.Logf("result[%d]: %+v", i, r)
			}
			if snap.Status != TransferJobDone {
				t.Errorf("expected final status %q, got %q (error=%q)", TransferJobDone, snap.Status, snap.Error)
			}
			if snap.CompletedItems != snap.TotalItems {
				t.Errorf("expected all %d items completed, got %d", snap.TotalItems, snap.CompletedItems)
			}
			if snap.FailedItems != 0 || snap.SkippedItems != 0 {
				t.Errorf("expected zero failed/skipped on a clean run into an empty prefix, got failed=%d skipped=%d", snap.FailedItems, snap.SkippedItems)
			}
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if lastStatus == "" || lastStatus == string(TransferJobRunning) {
		t.Fatalf("transfer did not finish within the test deadline (last status: %q)", lastStatus)
	}

	// ── Verify the objects actually landed at the destination, under the
	// exact prefix requested, preserving the source's relative structure.
	dstConn, err := fetchBucketConn(dstConnID)
	if err != nil {
		t.Fatalf("fetch dst conn: %v", err)
	}
	objs, err := listBucketObjects(req.Context(), dstConn, "nias-progress-test/bigset")
	if err != nil {
		t.Fatalf("list destination: %v", err)
	}
	if len(objs) != 150 {
		t.Errorf("expected 150 objects at the destination, got %d", len(objs))
	}
	t.Logf("verified %d objects landed at nias-transfer-dst/nias-progress-test/bigset/*", len(objs))

	// Cleanup: remove what this test wrote to the destination bucket so
	// re-runs start from a clean slate.
	var keys []string
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	if len(keys) > 0 {
		if _, err := batchDeleteBucketObjects(req.Context(), dstConn, keys); err != nil {
			t.Logf("cleanup: failed to delete destination test objects: %v", err)
		}
	}
}

func insertTestBucketConn(t *testing.T, conn *sql.DB, name, bucket string) int64 {
	t.Helper()
	conn.Exec(`DELETE FROM connections WHERE name = $1`, name)
	encPassword, err := encryptCredential("testsecretkey12345")
	if err != nil {
		t.Fatalf("encrypt test credential: %v", err)
	}
	var id int64
	err = conn.QueryRow(
		`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, disconnected)
		 VALUES ($1, 's3_aws', 'localhost', 19000, $2, 'testaccesskey', $3, 0, 0) RETURNING id`,
		name, bucket, encPassword,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test connection %s: %v", name, err)
	}
	return id
}

// requireTestEncryptionKey mirrors how main.go loads NIAS_ENCRYPTION_KEY —
// read directly from the environment by this Go process, never printed, so
// the key itself never appears in any tool output.
func requireTestEncryptionKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("NIAS_ENCRYPTION_KEY")
	if key == "" {
		t.Skip("NIAS_ENCRYPTION_KEY not set in this process's environment — source the same .env the dev server uses")
	}
	return key
}
