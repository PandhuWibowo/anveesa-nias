package handlers

// Cloud Storage Move relocates files/folders within a SINGLE bucket
// connection to a new parent folder ("flatten b2blocal/local/X up to
// b2blocal/X"). It reuses the same copy-then-delete approach
// CloudStorageRename (cloud_storage.go) already uses for a single folder,
// but as a background job instead of one big synchronous HTTP request —
// CloudStorageRename processes the whole object list inline before
// responding, which is fine for a handful of files but breaks down for a
// few hundred: each object is its own S3 copy round-trip, and against a
// real cloud provider (not localhost) that easily exceeds a browser or
// reverse-proxy timeout well before the request finishes, killing it
// mid-copy — before the old objects are ever deleted, so the move silently
// never happens. This file is the same async-job fix Cloud Storage Transfer
// already applied to the analogous cross-connection problem
// (cloud_storage_transfer.go) — same job/poll pattern, same atomic-counter
// discipline to keep GetMoveJobStatus's JSON reads race-free against the
// worker goroutines' atomic writes.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type MoveJobStatus string

const (
	MoveJobRunning  MoveJobStatus = "running"
	MoveJobDone     MoveJobStatus = "done"
	MoveJobPartial  MoveJobStatus = "partial"
	MoveJobFailed   MoveJobStatus = "failed"
	MoveJobCanceled MoveJobStatus = "canceled"
)

type MoveItemResult struct {
	SourceKey string `json:"source_key"`
	DestKey   string `json:"dest_key"`
	Status    string `json:"status"` // "done" | "failed"
	Error     string `json:"error,omitempty"`
}

const maxMoveResults = 500

type MoveJob struct {
	ID           string
	Status       MoveJobStatus // mutex-guarded; written once, at finalize
	StartedAt    time.Time
	DoneAt       *time.Time // mutex-guarded
	ConnectionID int64
	DestFolder   string
	ObjectCount  int    // unique source objects
	TotalItems   int64  // == ObjectCount here (one connection, no fan-out)
	Error        string // mutex-guarded

	completedItems int64 // atomic
	failedItems    int64 // atomic

	currentItem string           // mutex-guarded
	results     []MoveItemResult // mutex-guarded
	cancel      context.CancelFunc
	mu          sync.Mutex
}

var moveJobs sync.Map // id → *MoveJob

func getMoveJob(id string) (*MoveJob, bool) {
	v, ok := moveJobs.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*MoveJob), true
}

type moveJobView struct {
	ID             string           `json:"id"`
	Status         MoveJobStatus    `json:"status"`
	StartedAt      time.Time        `json:"started_at"`
	DoneAt         *time.Time       `json:"done_at,omitempty"`
	ConnectionID   int64            `json:"connection_id"`
	DestFolder     string           `json:"dest_folder"`
	ObjectCount    int              `json:"object_count"`
	TotalItems     int64            `json:"total_items"`
	CompletedItems int64            `json:"completed_items"`
	FailedItems    int64            `json:"failed_items"`
	Error          string           `json:"error,omitempty"`
	CurrentItem    string           `json:"current_item,omitempty"`
	Results        []MoveItemResult `json:"results"`
}

func (job *MoveJob) snapshot() moveJobView {
	job.mu.Lock()
	v := moveJobView{
		ID: job.ID, Status: job.Status, StartedAt: job.StartedAt, DoneAt: job.DoneAt,
		ConnectionID: job.ConnectionID, DestFolder: job.DestFolder,
		ObjectCount: job.ObjectCount, TotalItems: job.TotalItems,
		Error: job.Error, CurrentItem: job.currentItem,
		Results: append([]MoveItemResult{}, job.results...),
	}
	job.mu.Unlock()
	v.CompletedItems = atomic.LoadInt64(&job.completedItems)
	v.FailedItems = atomic.LoadInt64(&job.failedItems)
	return v
}

type moveRequest struct {
	// Items are source keys — a key ending in "/" is a folder and is
	// expanded recursively, same convention as Transfer/DeletePrefix. Each
	// item keeps its own basename under DestFolder — "b2blocal/local/foo/"
	// with DestFolder "b2blocal" becomes "b2blocal/foo/...".
	Items      []string `json:"items"`
	DestFolder string   `json:"destFolder"`
}

// moveTask is one source object to relocate, remembering which top-level
// selected item it came from so the worker can tell when that whole item's
// objects have all copied successfully (and only then is it safe to delete
// the item's originals).
type moveTask struct {
	sourceKey string
	destKey   string
	itemIndex int
}

// moveItemPlan is one top-level selected item (a file or a whole folder)
// and everything enumerated under it.
type moveItemPlan struct {
	trimmedKey string // item's own key, no trailing slash
	isDir      bool
	objects    []s3Object
}

const moveWorkerCount = 5

// MoveWithinBucket enumerates the requested objects, computes each one's
// new key (flattened under DestFolder, preserving relative structure below
// each selected item), and starts a background job that copies everything
// via S3's native copy (copyBucketObject — same connection, so unlike
// cross-connection Transfer this never needs to download+reupload) and only
// deletes a given item's originals once every one of its objects has copied
// successfully.
//
// POST /api/connections/{id}/storage/move
func MoveWithinBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var req moveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		destFolder := strings.Trim(strings.TrimSpace(req.DestFolder), "/")
		if destFolder == "" {
			http.Error(w, jsonError("destFolder is required"), http.StatusBadRequest)
			return
		}
		if len(req.Items) == 0 {
			http.Error(w, jsonError("at least one item is required"), http.StatusBadRequest)
			return
		}

		conn, err := fetchBucketConn(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		var plans []moveItemPlan
		seen := map[string]bool{}
		var tasks []moveTask
		for _, raw := range req.Items {
			key := strings.TrimSpace(raw)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			isDir := strings.HasSuffix(key, "/")
			trimmed := strings.TrimSuffix(key, "/")
			base := trimmed
			if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
				base = trimmed[idx+1:]
			}

			var objs []s3Object
			if isDir {
				objs, err = listBucketObjects(r.Context(), conn, trimmed)
				if err != nil {
					http.Error(w, jsonError("failed to list \""+key+"\": "+err.Error()), http.StatusBadGateway)
					return
				}
			} else {
				size, exists, herr := headBucketObject(r.Context(), conn, key)
				if herr != nil {
					http.Error(w, jsonError("failed to inspect \""+key+"\": "+herr.Error()), http.StatusBadGateway)
					return
				}
				if !exists {
					http.Error(w, jsonError("source object not found: "+key), http.StatusBadRequest)
					return
				}
				objs = []s3Object{{Key: key, Size: size}}
			}

			itemIndex := len(plans)
			plans = append(plans, moveItemPlan{trimmedKey: trimmed, isDir: isDir, objects: objs})
			for _, obj := range objs {
				var destKey string
				if isDir {
					destKey = destFolder + "/" + base + strings.TrimPrefix(obj.Key, trimmed)
				} else {
					destKey = destFolder + "/" + base
				}
				tasks = append(tasks, moveTask{sourceKey: obj.Key, destKey: destKey, itemIndex: itemIndex})
			}
		}
		if len(tasks) == 0 {
			http.Error(w, jsonError("no objects found under the selected item(s)"), http.StatusBadRequest)
			return
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &MoveJob{
			ID:           newJobID(),
			Status:       MoveJobRunning,
			StartedAt:    time.Now(),
			ConnectionID: connID,
			DestFolder:   destFolder,
			ObjectCount:  len(tasks),
			TotalItems:   int64(len(tasks)),
			cancel:       jobCancel,
		}
		moveJobs.Store(job.ID, job)

		go runMoveJob(jobCtx, job, conn, plans, tasks)

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
	}
}

func runMoveJob(jobCtx context.Context, job *MoveJob, conn *bucketConnRow, plans []moveItemPlan, tasks []moveTask) {
	defer job.cancel()

	taskCh := make(chan moveTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	itemObjectCount := make([]int, len(plans))
	for i, p := range plans {
		itemObjectCount[i] = len(p.objects)
	}
	var itemMu sync.Mutex
	itemSuccessCount := make([]int, len(plans))
	itemFailed := make([]bool, len(plans))

	var wg sync.WaitGroup
	for i := 0; i < moveWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				if jobCtx.Err() != nil {
					continue
				}
				job.mu.Lock()
				job.currentItem = task.sourceKey
				job.mu.Unlock()

				err := copyBucketObject(jobCtx, conn, task.sourceKey, task.destKey)
				if err != nil {
					atomic.AddInt64(&job.failedItems, 1)
					appendMoveResult(job, MoveItemResult{SourceKey: task.sourceKey, DestKey: task.destKey, Status: "failed", Error: err.Error()})
					itemMu.Lock()
					itemFailed[task.itemIndex] = true
					itemMu.Unlock()
					continue
				}
				atomic.AddInt64(&job.completedItems, 1)
				appendMoveResult(job, MoveItemResult{SourceKey: task.sourceKey, DestKey: task.destKey, Status: "done"})
				itemMu.Lock()
				itemSuccessCount[task.itemIndex]++
				itemMu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Only delete a selected item's originals once every one of its objects
	// copied successfully — a partial failure on one item never loses data,
	// and never blocks the other items from completing.
	if jobCtx.Err() == nil {
		for i, p := range plans {
			if itemFailed[i] || itemSuccessCount[i] != itemObjectCount[i] || itemObjectCount[i] == 0 {
				continue
			}
			if p.isDir {
				var keys []string
				for _, obj := range p.objects {
					keys = append(keys, obj.Key)
				}
				batchDeleteBucketObjects(jobCtx, conn, keys)
				deleteBucketObject(jobCtx, conn, p.trimmedKey+"/")
			} else {
				deleteBucketObject(jobCtx, conn, p.trimmedKey)
			}
		}
	}

	completed := atomic.LoadInt64(&job.completedItems)
	failed := atomic.LoadInt64(&job.failedItems)

	now := time.Now()
	job.mu.Lock()
	job.DoneAt = &now
	switch {
	case jobCtx.Err() != nil:
		job.Status = MoveJobCanceled
		job.Error = "canceled"
	case completed == 0:
		job.Status = MoveJobFailed
		job.Error = "all items failed"
	case failed == 0:
		job.Status = MoveJobDone
	default:
		job.Status = MoveJobPartial
		job.Error = fmt.Sprintf("%d failed", failed)
	}
	job.mu.Unlock()
}

func appendMoveResult(job *MoveJob, res MoveItemResult) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if len(job.results) >= maxMoveResults {
		if res.Status != "failed" {
			return
		}
		for i, r := range job.results {
			if r.Status != "failed" {
				job.results = append(job.results[:i], job.results[i+1:]...)
				break
			}
		}
		if len(job.results) >= maxMoveResults {
			job.results = job.results[1:]
		}
	}
	job.results = append(job.results, res)
}

// GetMoveJobStatus returns the current status of a move job.
// GET /api/storage/move-jobs/{id}
func GetMoveJobStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/storage/move-jobs/")
		job, ok := getMoveJob(id)
		if !ok {
			http.Error(w, jsonError("job not found"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(job.snapshot())
	}
}

// CancelMoveJob cancels a running move job. Objects already copied to the
// destination are left in place; an item's originals are only ever deleted
// once every one of its objects finished copying, so a cancel can never
// leave a source item deleted without its copy landing.
// DELETE /api/storage/move-jobs/{id}
func CancelMoveJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/storage/move-jobs/")
		job, ok := getMoveJob(id)
		if !ok {
			http.Error(w, jsonError("job not found"), http.StatusNotFound)
			return
		}
		job.mu.Lock()
		if job.Status == MoveJobRunning {
			job.cancel()
		}
		job.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"status": "canceling"})
	}
}
