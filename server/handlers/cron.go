package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/ssh"
)

// ── Types ─────────────────────────────────────────────────────────────────

// CronHostRef is the minimal host info the scheduler needs so users can pick
// dispatch targets. Cron reuses the shared SSH Hosts (docker_hosts, managed
// under the "SSH Hosts" page) rather than owning its own host list.
type CronHostRef struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	SSHHost string `json:"ssh_host"`
}

type CronJob struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	WorkingDir string  `json:"working_dir"`
	Category   string  `json:"category"`
	CronExpr   string  `json:"cron_expr"`
	TimeoutSec int     `json:"timeout_sec"`
	HostIDs    []int64 `json:"host_ids"`
	Enabled    bool    `json:"enabled"`
	CreatedBy  int64   `json:"created_by"`
	LastRunAt  string  `json:"last_run_at"`
	CreatedAt  string  `json:"created_at"`
}

type CronJobInput struct {
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	WorkingDir string  `json:"working_dir"`
	Category   string  `json:"category"`
	CronExpr   string  `json:"cron_expr"`
	TimeoutSec int     `json:"timeout_sec"`
	HostIDs    []int64 `json:"host_ids"`
	Enabled    bool    `json:"enabled"`
}

type CronJobRun struct {
	ID         int64  `json:"id"`
	JobID      int64  `json:"job_id"`
	HostID     int64  `json:"host_id"`
	HostName   string `json:"host_name"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	DurationMs int64  `json:"duration_ms"`
}

const cronOutputCap = 64 * 1024

// cronLastFired dedupes scheduled dispatch so a job fires at most once per
// wall-clock minute even if the tick loop overlaps. Keyed by job id -> minute.
var cronLastFired sync.Map

// ── Small helpers ─────────────────────────────────────────────────────────

func truncateCronOutput(s string) string {
	if len(s) > cronOutputCap {
		return s[:cronOutputCap] + "\n…(truncated)"
	}
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func marshalHostIDs(ids []int64) string {
	if ids == nil {
		ids = []int64{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func parseHostIDs(s string) []int64 {
	var ids []int64
	if strings.TrimSpace(s) == "" {
		return []int64{}
	}
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return []int64{}
	}
	return ids
}

func cronJobIDFromPath(r *http.Request) (int64, error) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cron/jobs/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing job id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

// ── SSH ───────────────────────────────────────────────────────────────────

// runRemoteCommand executes command on the host over SSH and returns the
// captured stdout/stderr and the process exit code. A non-zero exit is
// reported via exitCode (err stays nil); err is only set for transport,
// auth, or timeout failures. Reuses the shared DockerHost SSH dialer
// (sshClientForHost in docker.go).
func runRemoteCommand(h *DockerHost, command, workingDir string, timeoutSec int) (stdout, stderr string, exitCode int, err error) {
	client, err := sshClientForHost(h)
	if err != nil {
		return "", "", -1, err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", "", -1, err
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	cmd := command
	if strings.TrimSpace(workingDir) != "" {
		cmd = fmt.Sprintf("cd %s && %s", shellQuote(workingDir), command)
	}
	if timeoutSec <= 0 {
		timeoutSec = 3600
	}

	if err := sess.Start(cmd); err != nil {
		return "", "", -1, err
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case werr := <-done:
		err = werr
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return truncateCronOutput(outBuf.String()), truncateCronOutput(errBuf.String()), -1,
			fmt.Errorf("timed out after %ds", timeoutSec)
	}

	exitCode = 0
	if err != nil {
		if ee, ok := err.(*ssh.ExitError); ok {
			exitCode = ee.ExitStatus()
			err = nil // non-zero exit is a job outcome, not a transport error
		} else {
			exitCode = -1
		}
	}
	return truncateCronOutput(outBuf.String()), truncateCronOutput(errBuf.String()), exitCode, err
}

// ── Hosts (read-only view of the shared SSH Hosts / docker_hosts) ──────────

// ListCronHosts returns the shared SSH Hosts so users can pick dispatch
// targets. It reads docker_hosts (the "SSH Hosts" managed elsewhere) but is
// gated by cron permissions and never exposes credentials, so cron users do
// not need Docker permissions to see the pick-list.
func ListCronHosts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, ssh_host FROM docker_hosts
			 WHERE ssh_host IS NOT NULL AND ssh_host != '' ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []CronHostRef{}
		for rows.Next() {
			var h CronHostRef
			if err := rows.Scan(&h.ID, &h.Name, &h.SSHHost); err != nil {
				continue
			}
			list = append(list, h)
		}
		json.NewEncoder(w).Encode(list)
	}
}

// ── Job CRUD ──────────────────────────────────────────────────────────────

func scanCronJob(sc interface{ Scan(...any) error }) (CronJob, error) {
	var j CronJob
	var hostIDs string
	var enabled int
	var lastRun sql.NullString // nullable timestamp — cannot COALESCE to '' in postgres
	err := sc.Scan(&j.ID, &j.Name, &j.Command, &j.WorkingDir, &j.Category, &j.CronExpr,
		&j.TimeoutSec, &hostIDs, &enabled, &j.CreatedBy, &lastRun, &j.CreatedAt)
	if err != nil {
		return j, err
	}
	j.HostIDs = parseHostIDs(hostIDs)
	j.Enabled = enabled == 1
	j.LastRunAt = lastRun.String
	return j, nil
}

const cronJobCols = `id, name, command, COALESCE(working_dir,''), COALESCE(category,''), cron_expr,
	timeout_sec, COALESCE(host_ids,'[]'), enabled, COALESCE(created_by,0),
	last_run_at, created_at`

func ListCronJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT ` + cronJobCols + ` FROM cron_jobs ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []CronJob{}
		for rows.Next() {
			j, err := scanCronJob(rows)
			if err != nil {
				continue
			}
			list = append(list, j)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func GetCronJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		j, err := scanCronJob(appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT `+cronJobCols+` FROM cron_jobs WHERE id=?`), id))
		if err != nil {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(j)
	}
}

func validateCronJobInput(in *CronJobInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if !cronExprValid(in.CronExpr) {
		return fmt.Errorf("schedule must be a valid 5-field cron expression")
	}
	return nil
}

// cronExprValid checks the expression has 5 fields and that each field can
// match at least one value in its own domain, reusing the existing per-field
// evaluator (cronField in scheduler.go). Fields are independent dimensions, so
// every field matching some value means the whole expression can fire.
func cronExprValid(expr string) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	ranges := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}} // min,hour,dom,month,dow
	for i, f := range fields {
		matched := false
		for v := ranges[i][0]; v <= ranges[i][1] && !matched; v++ {
			if cronField(f, v) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func CreateCronJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in CronJobInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateCronJobInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if in.TimeoutSec <= 0 {
			in.TimeoutSec = 3600
		}
		createdBy, _ := currentUserID(r)
		id, err := insertRowReturningID(appdb.ConvertQuery(
			`INSERT INTO cron_jobs (name, command, working_dir, category, cron_expr, timeout_sec, host_ids, enabled, created_by)
			 VALUES (?,?,?,?,?,?,?,?,?)`),
			in.Name, in.Command, in.WorkingDir, in.Category, strings.TrimSpace(in.CronExpr),
			in.TimeoutSec, marshalHostIDs(in.HostIDs), boolToInt(in.Enabled), createdBy)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

func UpdateCronJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		var in CronJobInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateCronJobInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if in.TimeoutSec <= 0 {
			in.TimeoutSec = 3600
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE cron_jobs SET name=?, command=?, working_dir=?, category=?, cron_expr=?,
			        timeout_sec=?, host_ids=?, enabled=? WHERE id=?`),
			in.Name, in.Command, in.WorkingDir, in.Category, strings.TrimSpace(in.CronExpr),
			in.TimeoutSec, marshalHostIDs(in.HostIDs), boolToInt(in.Enabled), id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "ok": true})
	}
}

func DeleteCronJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cron_jobs WHERE id=?`), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cron_job_runs WHERE job_id=?`), id) //nolint:errcheck
		cronLastFired.Delete(id)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

func ToggleCronJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE cron_jobs SET enabled=? WHERE id=?`), boolToInt(body.Enabled), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "enabled": body.Enabled, "ok": true})
	}
}

// ── Run history ───────────────────────────────────────────────────────────

// scanCronRun scans a cron_job_runs row. finished_at is a nullable timestamp,
// so it is selected raw and read through sql.NullString (postgres rejects
// COALESCE(<timestamp>, '')).
func scanCronRun(sc interface{ Scan(...any) error }) (CronJobRun, error) {
	var run CronJobRun
	var finished sql.NullString
	err := sc.Scan(&run.ID, &run.JobID, &run.HostID, &run.HostName, &run.Trigger,
		&run.Status, &run.ExitCode, &run.Stdout, &run.Stderr, &run.StartedAt,
		&finished, &run.DurationMs)
	if err != nil {
		return run, err
	}
	run.FinishedAt = finished.String
	return run, nil
}

func ListCronJobRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, job_id, COALESCE(host_id,0), COALESCE(host_name,''), COALESCE(trigger,''),
			        COALESCE(status,''), COALESCE(exit_code,0), COALESCE(stdout,''), COALESCE(stderr,''),
			        started_at, finished_at, COALESCE(duration_ms,0)
			 FROM cron_job_runs WHERE job_id=? ORDER BY id DESC LIMIT `+strconv.Itoa(limit)), id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []CronJobRun{}
		for rows.Next() {
			run, err := scanCronRun(rows)
			if err != nil {
				continue
			}
			list = append(list, run)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func GetCronRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cron/runs/"), "/")
		rid, err := strconv.ParseInt(strings.Split(rest, "/")[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid run id"}`, http.StatusBadRequest)
			return
		}
		run, err := scanCronRun(appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, job_id, COALESCE(host_id,0), COALESCE(host_name,''), COALESCE(trigger,''),
			        COALESCE(status,''), COALESCE(exit_code,0), COALESCE(stdout,''), COALESCE(stderr,''),
			        started_at, finished_at, COALESCE(duration_ms,0)
			 FROM cron_job_runs WHERE id=?`), rid))
		if err != nil {
			http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(run)
	}
}

// RunCronJobNow triggers an immediate (manual) execution of a job.
func RunCronJobNow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronJobIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
			return
		}
		j, err := scanCronJob(appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT `+cronJobCols+` FROM cron_jobs WHERE id=?`), id))
		if err != nil {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		if len(j.HostIDs) == 0 {
			http.Error(w, jsonError("job has no target hosts"), http.StatusBadRequest)
			return
		}
		executeCronJobOnHosts(j, "manual")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "hosts": len(j.HostIDs)})
	}
}

// ── Dispatch ──────────────────────────────────────────────────────────────

// executeCronJobOnHosts fans a job out to each target host, recording one
// cron_job_runs row per host. Each host runs in its own goroutine.
func executeCronJobOnHosts(job CronJob, trigger string) {
	for _, hid := range job.HostIDs {
		go func(hostID int64) {
			started := time.Now()
			h, loadErr := loadDockerHost(hostID)
			hostName := ""
			if h != nil {
				hostName = h.Name
			}
			runID, err := insertRowReturningID(appdb.ConvertQuery(
				`INSERT INTO cron_job_runs (job_id, host_id, host_name, trigger, status)
				 VALUES (?,?,?,?,'running')`),
				job.ID, hostID, hostName, trigger)
			if err != nil {
				return
			}

			var stdout, stderr, status string
			var exitCode int
			if loadErr != nil {
				stderr = "load host: " + loadErr.Error()
				exitCode = -1
				status = "failed"
			} else {
				var runErr error
				stdout, stderr, exitCode, runErr = runRemoteCommand(h, job.Command, job.WorkingDir, job.TimeoutSec)
				switch {
				case runErr != nil:
					if stderr != "" {
						stderr += "\n"
					}
					stderr += runErr.Error()
					status = "failed"
				case exitCode == 0:
					status = "success"
				default:
					status = "failed"
				}
			}
			dur := time.Since(started).Milliseconds()
			appdb.DB.Exec(appdb.ConvertQuery( //nolint:errcheck
				`UPDATE cron_job_runs SET status=?, exit_code=?, stdout=?, stderr=?,
				        finished_at=CURRENT_TIMESTAMP, duration_ms=? WHERE id=?`),
				status, exitCode, truncateCronOutput(stdout), truncateCronOutput(stderr), dur, runID)
		}(hid)
	}
}

// runDueCronJobs fires any enabled cron job whose schedule matches the current
// minute. Called from processSchedulerTick (scheduler.go) under the shared
// distributed lock, so it runs at most once per minute across instances.
func runDueCronJobs() {
	now := time.Now()
	minute := now.Format("2006-01-02 15:04")
	rows, err := appdb.DB.Query(appdb.ConvertQuery(
		`SELECT ` + cronJobCols + ` FROM cron_jobs WHERE enabled=1`))
	if err != nil {
		return
	}
	var due []CronJob
	for rows.Next() {
		j, err := scanCronJob(rows)
		if err != nil {
			continue
		}
		if len(j.HostIDs) == 0 || !cronMatches(j.CronExpr, now) {
			continue
		}
		if v, ok := cronLastFired.Load(j.ID); ok && v == minute {
			continue // already fired this minute
		}
		cronLastFired.Store(j.ID, minute)
		due = append(due, j)
	}
	rows.Close()
	for _, j := range due {
		appdb.DB.Exec(appdb.ConvertQuery( //nolint:errcheck
			`UPDATE cron_jobs SET last_run_at=CURRENT_TIMESTAMP WHERE id=?`), j.ID)
		executeCronJobOnHosts(j, "scheduled")
	}
}
