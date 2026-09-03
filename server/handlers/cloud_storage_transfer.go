package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cloud Storage Transfer moves files/folders from one bucket connection to
// one or more other bucket connections — including across cloud providers
// (Huawei OBS → Alibaba OSS → AWS S3 etc). Because S3's native
// x-amz-copy-source copy only works within a single provider's own
// credentials, a cross-provider transfer has no server-side "copy" to lean
// on: the app relays it itself, reading each object from
// openBucketObjectStream (backup_bucket.go) and writing it out via
// uploadObjectSpooled (cloud_storage.go) — which spools to a temp file to
// learn the real size before the PUT. That spooling (not a direct
// stream-to-stream pipe) is required, not just simpler: MinIO, and per
// uploadObjectSpooled's own doc comment very likely real AWS S3/GCS/Alibaba
// OSS too, reject a chunked/unsigned-payload PUT with 411 Length Required —
// only Huawei OBS tolerates that in this app's experience. This reuses the
// exact async-job pattern BackupJob already established (backup_bucket.go)
// — a goroutine populates a mutex-guarded struct kept in an in-memory
// sync.Map, polled via GET/DELETE .../transfer-jobs/{id}.

// ── Async transfer job store ────────────────────────────────────────────────

type TransferJobStatus string

const (
	TransferJobRunning  TransferJobStatus = "running"
	TransferJobDone     TransferJobStatus = "done"
	TransferJobPartial  TransferJobStatus = "partial" // finished, but some items failed or were skipped
	TransferJobFailed   TransferJobStatus = "failed"
	TransferJobCanceled TransferJobStatus = "canceled"
)

// transferDestSummary describes one destination for display purposes.
type transferDestSummary struct {
	ConnectionID int64  `json:"connection_id"`
	ConnName     string `json:"conn_name"`
	Prefix       string `json:"prefix"`
}

// TransferItemResult records the outcome of copying one source object to one
// destination — a transfer of N objects to M destinations produces up to
// N*M of these.
type TransferItemResult struct {
	SourceKey  string `json:"source_key"`
	DestConnID int64  `json:"dest_connection_id"`
	DestKey    string `json:"dest_key"`
	Status     string `json:"status"` // "done" | "failed" | "skipped"
	Bytes      int64  `json:"bytes,omitempty"`
	Error      string `json:"error,omitempty"`
}

// maxTransferResults caps how many per-item results a job keeps in memory —
// failures are always worth surfacing so the user can retry just those, but
// keeping every success too would make a 5000-object transfer to 3
// destinations hold 15000 result rows in memory and in the poll response for
// no practical benefit.
const maxTransferResults = 500

// TransferJob's item/byte counters are updated with atomic.AddInt64 from
// many concurrent worker goroutines while a poll can be reading them at the
// same time — so, like BackupJob's uploadCounter, they're kept as
// unexported plain int64s accessed only via the atomic package, never
// through encoding/json's field reflection (which doesn't know to use
// atomic loads and would otherwise race with the writers).
type TransferJob struct {
	ID        string
	Status    TransferJobStatus // mutex-guarded; only ever written once, at finalize
	StartedAt time.Time
	DoneAt    *time.Time // mutex-guarded

	Mode           string // "copy" | "move"
	ConflictPolicy string // "overwrite" | "skip"

	SourceConnID   int64
	SourceConnName string
	Destinations   []transferDestSummary
	ObjectCount    int   // unique source objects
	TotalItems     int64 // objects × destinations
	TotalBytes     int64 // best-effort; immutable after job creation

	Error string // mutex-guarded

	completedItems     int64 // atomic
	failedItems        int64 // atomic
	skippedItems       int64 // atomic
	transferredBytes   int64 // atomic
	movedSourceObjects int64 // mutex-guarded

	currentItem string               // mutex-guarded
	results     []TransferItemResult // mutex-guarded
	cancel      context.CancelFunc
	mu          sync.Mutex
}

var transferJobs sync.Map // id → *TransferJob

func getTransferJob(id string) (*TransferJob, bool) {
	v, ok := transferJobs.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*TransferJob), true
}

// transferJobView is the JSON shape returned by GetTransferJobStatus, built
// via TransferJob.snapshot() rather than a direct json.Marshal of the job so
// that the atomic counters get atomic reads.
type transferJobView struct {
	ID                 string                `json:"id"`
	Status             TransferJobStatus     `json:"status"`
	StartedAt          time.Time             `json:"started_at"`
	DoneAt             *time.Time            `json:"done_at,omitempty"`
	Mode               string                `json:"mode"`
	ConflictPolicy     string                `json:"conflict_policy"`
	SourceConnID       int64                 `json:"source_conn_id"`
	SourceConnName     string                `json:"source_conn_name"`
	Destinations       []transferDestSummary `json:"destinations"`
	ObjectCount        int                   `json:"object_count"`
	TotalItems         int64                 `json:"total_items"`
	CompletedItems     int64                 `json:"completed_items"`
	FailedItems        int64                 `json:"failed_items"`
	SkippedItems       int64                 `json:"skipped_items"`
	TotalBytes         int64                 `json:"total_bytes,omitempty"`
	TransferredBytes   int64                 `json:"transferred_bytes"`
	MovedSourceObjects int64                 `json:"moved_source_objects,omitempty"`
	Error              string                `json:"error,omitempty"`
	CurrentItem        string                `json:"current_item,omitempty"`
	Results            []TransferItemResult  `json:"results"`
}

func (job *TransferJob) snapshot() transferJobView {
	job.mu.Lock()
	v := transferJobView{
		ID: job.ID, Status: job.Status, StartedAt: job.StartedAt, DoneAt: job.DoneAt,
		Mode: job.Mode, ConflictPolicy: job.ConflictPolicy,
		SourceConnID: job.SourceConnID, SourceConnName: job.SourceConnName,
		Destinations: job.Destinations, ObjectCount: job.ObjectCount,
		TotalItems: job.TotalItems, TotalBytes: job.TotalBytes,
		Error: job.Error, CurrentItem: job.currentItem,
		Results:            append([]TransferItemResult{}, job.results...),
		MovedSourceObjects: job.movedSourceObjects,
	}
	job.mu.Unlock()
	v.CompletedItems = atomic.LoadInt64(&job.completedItems)
	v.FailedItems = atomic.LoadInt64(&job.failedItems)
	v.SkippedItems = atomic.LoadInt64(&job.skippedItems)
	v.TransferredBytes = atomic.LoadInt64(&job.transferredBytes)
	return v
}

// ── Request/response shapes ─────────────────────────────────────────────────

type transferDestInput struct {
	ConnectionID int64  `json:"connectionId"`
	Prefix       string `json:"prefix"`
}

type transferRequest struct {
	// Items are source keys — a key ending in "/" is a folder and is expanded
	// recursively (same convention CloudStorageDeletePrefix/DownloadZip use).
	Items          []string            `json:"items"`
	Destinations   []transferDestInput `json:"destinations"`
	Mode           string              `json:"mode"`
	ConflictPolicy string              `json:"conflictPolicy"`
}

type resolvedTransferDest struct {
	conn     *bucketConnRow
	connID   int64
	connName string
	prefix   string // "" or "foo/bar/" (trimmed, trailing slash)
}

// transferTask is one (source object → one destination) unit of work.
type transferTask struct {
	sourceKey string
	size      int64
	dest      resolvedTransferDest
}

const (
	maxTransferDestinations = 10
	transferWorkerCount     = 5
)

// TransferToBuckets enumerates the requested source objects, validates every
// destination connection up front, then starts a background job and returns
// its ID immediately — the actual streaming happens in a goroutine, exactly
// like BackupToBucket (backup_bucket.go).
//
// POST /api/connections/{id}/storage/transfer
func TransferToBuckets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		start := time.Now()
		username := r.Header.Get("X-Username")
		if username == "" {
			username = "anonymous"
		}

		srcConnID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var req transferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}

		if req.Mode == "" {
			req.Mode = "copy"
		}
		if req.Mode != "copy" && req.Mode != "move" {
			http.Error(w, jsonError("mode must be \"copy\" or \"move\""), http.StatusBadRequest)
			return
		}
		if req.ConflictPolicy == "" {
			req.ConflictPolicy = "overwrite"
		}
		if req.ConflictPolicy != "overwrite" && req.ConflictPolicy != "skip" {
			http.Error(w, jsonError("conflictPolicy must be \"overwrite\" or \"skip\""), http.StatusBadRequest)
			return
		}
		if len(req.Items) == 0 {
			http.Error(w, jsonError("at least one item is required"), http.StatusBadRequest)
			return
		}
		if len(req.Destinations) == 0 {
			http.Error(w, jsonError("at least one destination is required"), http.StatusBadRequest)
			return
		}
		if len(req.Destinations) > maxTransferDestinations {
			http.Error(w, jsonError(fmt.Sprintf("at most %d destinations are supported per transfer", maxTransferDestinations)), http.StatusBadRequest)
			return
		}

		srcConn, err := fetchBucketConn(srcConnID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		srcConnName := connectionNameByID(srcConnID)

		destinations := make([]resolvedTransferDest, 0, len(req.Destinations))
		destSummaries := make([]transferDestSummary, 0, len(req.Destinations))
		for _, d := range req.Destinations {
			destConn, err := fetchBucketConn(d.ConnectionID)
			if err != nil {
				http.Error(w, jsonError(fmt.Sprintf("destination connection %d: %s", d.ConnectionID, err.Error())), http.StatusBadRequest)
				return
			}
			prefix := strings.Trim(d.Prefix, "/")
			if prefix != "" {
				prefix += "/"
			}
			destConnName := connectionNameByID(d.ConnectionID)
			destinations = append(destinations, resolvedTransferDest{
				conn: destConn, connID: d.ConnectionID, connName: destConnName, prefix: prefix,
			})
			destSummaries = append(destSummaries, transferDestSummary{
				ConnectionID: d.ConnectionID, ConnName: destConnName, Prefix: prefix,
			})
		}

		// Enumerate every source object up front (same synchronous-listing
		// approach CloudStorageDeletePrefix already uses for folder prefixes)
		// so the job has a fixed, known total before it starts streaming.
		seen := map[string]bool{}
		var objects []s3Object
		for _, raw := range req.Items {
			key := strings.TrimSpace(raw)
			if key == "" {
				continue
			}
			if strings.HasSuffix(key, "/") {
				trimmed := strings.TrimSuffix(key, "/")
				objs, err := listBucketObjects(r.Context(), srcConn, trimmed)
				if err != nil {
					http.Error(w, jsonError("failed to list \""+key+"\": "+err.Error()), http.StatusBadGateway)
					return
				}
				for _, o := range objs {
					if !seen[o.Key] {
						seen[o.Key] = true
						objects = append(objects, o)
					}
				}
				continue
			}
			if seen[key] {
				continue
			}
			size, exists, err := headBucketObject(r.Context(), srcConn, key)
			if err != nil {
				http.Error(w, jsonError("failed to inspect \""+key+"\": "+err.Error()), http.StatusBadGateway)
				return
			}
			if !exists {
				http.Error(w, jsonError("source object not found: "+key), http.StatusBadRequest)
				return
			}
			seen[key] = true
			objects = append(objects, s3Object{Key: key, Size: size})
		}
		if len(objects) == 0 {
			http.Error(w, jsonError("no objects found under the selected item(s)"), http.StatusBadRequest)
			return
		}

		tasks := make([]transferTask, 0, len(objects)*len(destinations))
		var totalBytes int64
		for _, obj := range objects {
			totalBytes += obj.Size * int64(len(destinations))
			for _, d := range destinations {
				tasks = append(tasks, transferTask{sourceKey: obj.Key, size: obj.Size, dest: d})
			}
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &TransferJob{
			ID:             newJobID(),
			Status:         TransferJobRunning,
			StartedAt:      time.Now(),
			Mode:           req.Mode,
			ConflictPolicy: req.ConflictPolicy,
			SourceConnID:   srcConnID,
			SourceConnName: srcConnName,
			Destinations:   destSummaries,
			ObjectCount:    len(objects),
			TotalItems:     int64(len(tasks)),
			TotalBytes:     totalBytes,
			cancel:         jobCancel,
		}
		transferJobs.Store(job.ID, job)

		go runTransferJob(jobCtx, job, srcConn, destinations, tasks, req.Mode, req.ConflictPolicy, username, start)

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
	}
}

func runTransferJob(
	jobCtx context.Context,
	job *TransferJob,
	srcConn *bucketConnRow,
	destinations []resolvedTransferDest,
	tasks []transferTask,
	mode, conflictPolicy, username string,
	start time.Time,
) {
	defer job.cancel()

	taskCh := make(chan transferTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	// Tracks, per source key, how many destinations have successfully
	// received it — a source object is only ever deleted (move mode) once
	// every destination has it, so a mid-transfer failure never loses data.
	var moveMu sync.Mutex
	moveSuccess := map[string]int{}

	var wg sync.WaitGroup
	for i := 0; i < transferWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				if jobCtx.Err() != nil {
					continue
				}
				transferOneTask(jobCtx, job, srcConn, task, conflictPolicy, &moveMu, moveSuccess)
			}
		}()
	}
	wg.Wait()

	var movedCount int64
	if mode == "move" && jobCtx.Err() == nil {
		var toDelete []string
		moveMu.Lock()
		for key, count := range moveSuccess {
			if count == len(destinations) {
				toDelete = append(toDelete, key)
			}
		}
		moveMu.Unlock()
		if len(toDelete) > 0 {
			deleted, _ := batchDeleteBucketObjects(jobCtx, srcConn, toDelete)
			movedCount = int64(deleted)
		}
	}

	completed := atomic.LoadInt64(&job.completedItems)
	failed := atomic.LoadInt64(&job.failedItems)
	skipped := atomic.LoadInt64(&job.skippedItems)

	now := time.Now()
	job.mu.Lock()
	job.DoneAt = &now
	job.movedSourceObjects = movedCount
	switch {
	case jobCtx.Err() != nil:
		job.Status = TransferJobCanceled
		job.Error = "canceled"
	case completed == 0:
		job.Status = TransferJobFailed
		job.Error = "all items failed"
	case failed == 0 && skipped == 0:
		job.Status = TransferJobDone
	default:
		job.Status = TransferJobPartial
		job.Error = fmt.Sprintf("%d failed, %d skipped", failed, skipped)
	}
	finalErr := job.Error
	job.mu.Unlock()

	details := fmt.Sprintf("mode=%s conflict=%s objects=%d destinations=%d completed=%d failed=%d skipped=%d moved=%d",
		mode, conflictPolicy, job.ObjectCount, len(destinations), completed, failed, skipped, movedCount)
	writeAuditEvent("storage_transfer", "transfer", job.SourceConnName, details, username,
		&job.SourceConnID, job.SourceConnName, "", time.Since(start).Milliseconds(), 0, finalErr)
}

func transferOneTask(jobCtx context.Context, job *TransferJob, srcConn *bucketConnRow, task transferTask, conflictPolicy string, moveMu *sync.Mutex, moveSuccess map[string]int) {
	destKey := task.dest.prefix + task.sourceKey

	job.mu.Lock()
	job.currentItem = task.sourceKey + " → " + task.dest.connName
	job.mu.Unlock()

	recordSkip := func() {
		atomic.AddInt64(&job.skippedItems, 1)
		appendTransferResult(job, TransferItemResult{
			SourceKey: task.sourceKey, DestConnID: task.dest.connID, DestKey: destKey, Status: "skipped",
		})
		markMoveSuccess(moveMu, moveSuccess, task.sourceKey)
	}

	if conflictPolicy == "skip" {
		_, exists, err := headBucketObject(jobCtx, task.dest.conn, destKey)
		if err != nil {
			atomic.AddInt64(&job.failedItems, 1)
			appendTransferResult(job, TransferItemResult{
				SourceKey: task.sourceKey, DestConnID: task.dest.connID, DestKey: destKey, Status: "failed",
				Error: "conflict check failed: " + err.Error(),
			})
			return
		}
		if exists {
			recordSkip()
			return
		}
	}

	resp, err := openBucketObjectStream(jobCtx, srcConn, task.sourceKey, 0)
	if err != nil {
		atomic.AddInt64(&job.failedItems, 1)
		appendTransferResult(job, TransferItemResult{
			SourceKey: task.sourceKey, DestConnID: task.dest.connID, DestKey: destKey, Status: "failed", Error: err.Error(),
		})
		return
	}
	cr := &countingReader{r: resp.Body}
	// uploadObjectSpooled (not uploadToBucketStream) deliberately — chunked,
	// unsigned-payload PUTs are only tolerated by Huawei OBS in practice;
	// MinIO and, per cloud_storage.go's own findings, very likely real AWS
	// S3/GCS/Alibaba OSS too reject them with 411 Length Required. Spooling
	// to a temp file first (learning the real size) is what actually works
	// across providers — the same reason CloudStorageUpload already uses it
	// for browser uploads.
	uploadErr := uploadObjectSpooled(jobCtx, task.dest.conn, destKey, cr, "")
	resp.Body.Close()

	bytes := atomic.LoadInt64(&cr.n)
	atomic.AddInt64(&job.transferredBytes, bytes)

	if uploadErr != nil {
		atomic.AddInt64(&job.failedItems, 1)
		appendTransferResult(job, TransferItemResult{
			SourceKey: task.sourceKey, DestConnID: task.dest.connID, DestKey: destKey, Status: "failed", Error: uploadErr.Error(),
		})
		return
	}

	atomic.AddInt64(&job.completedItems, 1)
	appendTransferResult(job, TransferItemResult{
		SourceKey: task.sourceKey, DestConnID: task.dest.connID, DestKey: destKey, Status: "done", Bytes: bytes,
	})
	markMoveSuccess(moveMu, moveSuccess, task.sourceKey)
}

func markMoveSuccess(moveMu *sync.Mutex, moveSuccess map[string]int, key string) {
	moveMu.Lock()
	moveSuccess[key]++
	moveMu.Unlock()
}

// appendTransferResult keeps the newest maxTransferResults entries, always
// preferring to keep failures — see maxTransferResults' doc comment.
func appendTransferResult(job *TransferJob, res TransferItemResult) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if len(job.results) >= maxTransferResults {
		if res.Status != "failed" {
			return
		}
		// Drop the oldest non-failed result to make room, if any; otherwise
		// just drop the oldest entry outright rather than growing unbounded.
		for i, r := range job.results {
			if r.Status != "failed" {
				job.results = append(job.results[:i], job.results[i+1:]...)
				break
			}
		}
		if len(job.results) >= maxTransferResults {
			job.results = job.results[1:]
		}
	}
	job.results = append(job.results, res)
}

// headBucketObject issues a signed HEAD request and reports whether the
// object exists (404 is not treated as an error — that's the whole point of
// a "does this exist" check). Used both to size single-file source items
// and to implement the "skip if destination already has it" conflict policy.
func headBucketObject(ctx context.Context, dest *bucketConnRow, key string) (size int64, exists bool, err error) {
	resp, err := bucketSignedRequest(ctx, dest, http.MethodHead, key, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer resp.Body.Close()
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		size, _ = strconv.ParseInt(cl, 10, 64)
	}
	return size, true, nil
}

// GetTransferJobStatus returns the current status of a transfer job.
// GET /api/storage/transfer-jobs/{id}
func GetTransferJobStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/storage/transfer-jobs/")
		job, ok := getTransferJob(id)
		if !ok {
			http.Error(w, jsonError("job not found"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(job.snapshot())
	}
}

// CancelTransferJob cancels a running transfer job. Objects already fully
// uploaded to a destination are left in place; move-mode source deletion
// only ever happens for objects that finished on every destination, so a
// cancel can never leave a source object deleted without its copies landing.
// DELETE /api/storage/transfer-jobs/{id}
func CancelTransferJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/storage/transfer-jobs/")
		job, ok := getTransferJob(id)
		if !ok {
			http.Error(w, jsonError("job not found"), http.StatusNotFound)
			return
		}
		job.mu.Lock()
		if job.Status == TransferJobRunning {
			job.cancel()
		}
		job.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"status": "canceling"})
	}
}
