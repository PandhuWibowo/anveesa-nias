package handlers

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type laravelQueueSummary struct {
	Name     string `json:"name"`
	Ready    int64  `json:"ready"`
	Delayed  int64  `json:"delayed"`
	Reserved int64  `json:"reserved"`
	Notify   bool   `json:"notify"`
}

type laravelQueueJob struct {
	ID          string         `json:"id"`
	State       string         `json:"state"`
	Queue       string         `json:"queue"`
	UUID        string         `json:"uuid,omitempty"`
	DisplayName string         `json:"display_name,omitempty"`
	Job         string         `json:"job,omitempty"`
	CommandName string         `json:"command_name,omitempty"`
	Attempts    int64          `json:"attempts"`
	MaxTries    int64          `json:"max_tries,omitempty"`
	Timeout     int64          `json:"timeout,omitempty"`
	Backoff     any            `json:"backoff,omitempty"`
	Score       int64          `json:"score,omitempty"`
	AvailableAt string         `json:"available_at,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	Raw         string         `json:"raw"`
}

type laravelQueueJobsResponse struct {
	Queue string            `json:"queue"`
	Jobs  []laravelQueueJob `json:"jobs"`
}

type laravelQueueActionRequest struct {
	Queue  string `json:"queue"`
	Prefix string `json:"prefix"`
	State  string `json:"state"`
	Raw    string `json:"raw"`
	DB     *int   `json:"db"`
}

type laravelFailedJob struct {
	ID         int64          `json:"id"`
	UUID       string         `json:"uuid,omitempty"`
	Connection string         `json:"connection"`
	Queue      string         `json:"queue"`
	Payload    map[string]any `json:"payload,omitempty"`
	RawPayload string         `json:"raw_payload"`
	Exception  string         `json:"exception"`
	FailedAt   string         `json:"failed_at"`
}

type laravelFailedJobActionRequest struct {
	ID            int64  `json:"id"`
	RedisConnID   int64  `json:"redis_conn_id"`
	RedisDB       *int   `json:"redis_db"`
	Prefix        string `json:"prefix"`
	Queue         string `json:"queue"`
	Payload       string `json:"payload"`
	DeleteAfter   bool   `json:"delete_after"`
	PayloadEdited bool   `json:"payload_edited"`
}

type laravelHorizonSummary struct {
	Detected    bool             `json:"detected"`
	KeyCount    int              `json:"key_count"`
	Supervisors int64            `json:"supervisors"`
	Masters     int64            `json:"masters"`
	RecentJobs  int64            `json:"recent_jobs"`
	FailedJobs  int64            `json:"failed_jobs"`
	Workload    map[string]int64 `json:"workload,omitempty"`
	SampleKeys  []string         `json:"sample_keys"`
}

func LaravelQueueQueues() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		prefix := laravelQueuePrefix(r)
		keys, err := scanRedisKeys(r.Context(), client, prefix+":*", 1000)
		if err != nil {
			http.Error(w, jsonError("redis scan failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		names := map[string]bool{}
		for _, key := range keys {
			if name := laravelQueueNameFromKey(prefix, key); name != "" {
				names[name] = true
			}
		}
		if len(names) == 0 {
			names["default"] = true
		}

		summaries := make([]laravelQueueSummary, 0, len(names))
		for name := range names {
			ready, _ := redisInt(client.command(r.Context(), "LLEN", laravelQueueKey(prefix, name, "")))
			delayed, _ := redisInt(client.command(r.Context(), "ZCARD", laravelQueueKey(prefix, name, "delayed")))
			reserved, _ := redisInt(client.command(r.Context(), "ZCARD", laravelQueueKey(prefix, name, "reserved")))
			notify, _ := redisInt(client.command(r.Context(), "EXISTS", laravelQueueKey(prefix, name, "notify")))
			summaries = append(summaries, laravelQueueSummary{
				Name:     name,
				Ready:    ready,
				Delayed:  delayed,
				Reserved: reserved,
				Notify:   notify > 0,
			})
		}
		sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
		writeRedisAudit(r, "laravel_queue_list", connID, connName, prefix, "")
		json.NewEncoder(w).Encode(summaries)
	}
}

func LaravelQueueJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		queue := strings.TrimSpace(r.URL.Query().Get("queue"))
		if queue == "" {
			queue = "default"
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		prefix := laravelQueuePrefix(r)
		limit := queryInt(r, "limit", 100, 1, 500)
		jobs := make([]laravelQueueJob, 0, limit)

		readyRaw, err := client.command(r.Context(), "LRANGE", laravelQueueKey(prefix, queue, ""), "0", strconv.Itoa(limit-1))
		if err != nil {
			http.Error(w, jsonError("redis lrange failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		for _, raw := range anySliceToStrings(readyRaw) {
			jobs = append(jobs, parseLaravelQueueJob(raw, queue, "ready", 0))
		}

		for _, state := range []string{"delayed", "reserved"} {
			raw, err := client.command(r.Context(), "ZRANGE", laravelQueueKey(prefix, queue, state), "0", strconv.Itoa(limit-1), "WITHSCORES")
			if err != nil {
				http.Error(w, jsonError("redis zrange failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			for _, pair := range zsetPairs(raw) {
				score, _ := strconv.ParseInt(pair["score"], 10, 64)
				jobs = append(jobs, parseLaravelQueueJob(pair["member"], queue, state, score))
			}
		}

		writeRedisAudit(r, "laravel_queue_jobs", connID, connName, queue, "")
		json.NewEncoder(w).Encode(laravelQueueJobsResponse{Queue: queue, Jobs: jobs})
	}
}

func LaravelQueueAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		action := ""
		if len(parts) >= 4 {
			action = parts[3]
		}

		var payload laravelQueueActionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		payload.Queue = strings.TrimSpace(payload.Queue)
		if payload.Queue == "" {
			payload.Queue = "default"
		}
		payload.Prefix = strings.Trim(strings.TrimSpace(payload.Prefix), ":")
		if payload.Prefix == "" {
			payload.Prefix = "queues"
		}

		client, connName, err := openRedisClient(connID, payload.DB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		switch action {
		case "delete":
			if err := enforceLaravelQueueAction(connID, "delete"); err != nil {
				writeLaravelQueueAudit(r, connID, 0, "delete", payload.Queue, "", "", 0, false, "blocked", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
			if err := laravelQueueRemoveJob(r, client, payload); err != nil {
				writeRedisAudit(r, "laravel_queue_delete", connID, connName, payload.Queue, err.Error())
				writeLaravelQueueAudit(r, connID, 0, "delete", payload.Queue, "", "", 0, false, "failed", err.Error(), map[string]any{"state": payload.State})
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			writeRedisAudit(r, "laravel_queue_delete", connID, connName, payload.Queue, "")
			writeLaravelQueueAudit(r, connID, 0, "delete", payload.Queue, "", "", 0, false, "success", "", map[string]any{"state": payload.State})
			json.NewEncoder(w).Encode(map[string]string{"message": "Job deleted"})
		case "requeue":
			if err := enforceLaravelQueueAction(connID, "retry"); err != nil {
				writeLaravelQueueAudit(r, connID, 0, "requeue", payload.Queue, payload.Queue, "", 0, false, "blocked", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
			if payload.Raw == "" {
				http.Error(w, `{"error":"job payload is required"}`, http.StatusBadRequest)
				return
			}
			if payload.State != "delayed" && payload.State != "reserved" {
				http.Error(w, `{"error":"only delayed or reserved jobs can be requeued"}`, http.StatusBadRequest)
				return
			}
			if err := laravelQueueRemoveJob(r, client, payload); err != nil {
				writeRedisAudit(r, "laravel_queue_requeue", connID, connName, payload.Queue, err.Error())
				writeLaravelQueueAudit(r, connID, 0, "requeue", payload.Queue, payload.Queue, "", 0, false, "failed", err.Error(), map[string]any{"state": payload.State})
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			if _, err := client.command(r.Context(), "LPUSH", laravelQueueKey(payload.Prefix, payload.Queue, ""), payload.Raw); err != nil {
				writeRedisAudit(r, "laravel_queue_requeue", connID, connName, payload.Queue, err.Error())
				writeLaravelQueueAudit(r, connID, 0, "requeue", payload.Queue, payload.Queue, "", 0, false, "failed", err.Error(), map[string]any{"state": payload.State})
				http.Error(w, jsonError("redis lpush failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			writeRedisAudit(r, "laravel_queue_requeue", connID, connName, payload.Queue, "")
			writeLaravelQueueAudit(r, connID, 0, "requeue", payload.Queue, payload.Queue, "", 0, false, "success", "", map[string]any{"state": payload.State})
			json.NewEncoder(w).Encode(map[string]string{"message": "Job requeued"})
		case "clear":
			if err := enforceLaravelQueueAction(connID, "clear"); err != nil {
				writeLaravelQueueAudit(r, connID, 0, "clear", payload.Queue, "", "", 0, false, "blocked", err.Error(), map[string]any{"state": payload.State})
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
			keys := []string{}
			switch payload.State {
			case "ready":
				keys = append(keys, laravelQueueKey(payload.Prefix, payload.Queue, ""))
			case "delayed", "reserved", "notify":
				keys = append(keys, laravelQueueKey(payload.Prefix, payload.Queue, payload.State))
			default:
				keys = append(keys,
					laravelQueueKey(payload.Prefix, payload.Queue, ""),
					laravelQueueKey(payload.Prefix, payload.Queue, "delayed"),
					laravelQueueKey(payload.Prefix, payload.Queue, "reserved"),
					laravelQueueKey(payload.Prefix, payload.Queue, "notify"),
				)
			}
			args := append([]string{"DEL"}, keys...)
			if _, err := client.command(r.Context(), args...); err != nil {
				writeRedisAudit(r, "laravel_queue_clear", connID, connName, payload.Queue, err.Error())
				writeLaravelQueueAudit(r, connID, 0, "clear", payload.Queue, "", "", 0, false, "failed", err.Error(), map[string]any{"state": payload.State})
				http.Error(w, jsonError("redis delete failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			writeRedisAudit(r, "laravel_queue_clear", connID, connName, payload.Queue, "")
			writeLaravelQueueAudit(r, connID, 0, "clear", payload.Queue, "", "", 0, false, "success", "", map[string]any{"state": payload.State})
			json.NewEncoder(w).Encode(map[string]string{"message": "Queue cleared"})
		default:
			http.Error(w, `{"error":"unsupported queue action"}`, http.StatusBadRequest)
		}
	}
}

func LaravelQueueFailedJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		db, driver, err := openRemoteDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		defer db.Close()

		limit := queryInt(r, "limit", 100, 1, 500)
		query := "SELECT id, COALESCE(uuid,''), connection, queue, payload, exception, failed_at FROM failed_jobs ORDER BY id DESC LIMIT " + strconv.Itoa(limit)
		if driver == "sqlserver" {
			query = "SELECT TOP " + strconv.Itoa(limit) + " id, COALESCE(uuid,''), connection, queue, payload, exception, failed_at FROM failed_jobs ORDER BY id DESC"
		}

		rows, err := db.QueryContext(r.Context(), query)
		if err != nil {
			http.Error(w, jsonError("failed_jobs query failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer rows.Close()

		jobs := []laravelFailedJob{}
		for rows.Next() {
			var job laravelFailedJob
			var uuid, connection, queue, payload, exception sql.NullString
			var failedAt any
			if err := rows.Scan(&job.ID, &uuid, &connection, &queue, &payload, &exception, &failedAt); err != nil {
				http.Error(w, jsonError("failed_jobs scan failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			job.UUID = uuid.String
			job.Connection = connection.String
			job.Queue = queue.String
			job.RawPayload = payload.String
			job.Exception = exception.String
			job.FailedAt = fmt.Sprint(failedAt)
			var decoded map[string]any
			if err := json.Unmarshal([]byte(payload.String), &decoded); err == nil {
				job.Payload = decoded
			}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, jsonError("failed_jobs iteration error: "+err.Error()), http.StatusBadGateway)
		return
	}
	json.NewEncoder(w).Encode(jobs)
}
}

func LaravelQueueFailedJobAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		action := ""
		if len(parts) >= 4 {
			action = parts[3]
		}

		var payload laravelFailedJobActionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if payload.ID <= 0 {
			http.Error(w, `{"error":"failed job id is required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := openRemoteDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		defer db.Close()

		switch action {
	case "retry-failed":
		if payload.RedisConnID <= 0 {
			http.Error(w, `{"error":"redis connection id is required"}`, http.StatusBadRequest)
			return
		}
		if err := enforceLaravelQueueAction(payload.RedisConnID, "retry"); err != nil {
			writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "blocked", err.Error(), nil)
			http.Error(w, jsonError(err.Error()), http.StatusForbidden)
			return
		}
		if payload.DeleteAfter {
			if err := enforceLaravelQueueAction(payload.RedisConnID, "delete"); err != nil {
				writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "blocked", err.Error(), map[string]any{"delete_after": true})
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
		}
		if payload.PayloadEdited {
			if err := enforceLaravelQueueAction(payload.RedisConnID, "editedReplay"); err != nil {
				writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, true, "blocked", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
		}
			payload.Prefix = strings.Trim(strings.TrimSpace(payload.Prefix), ":")
			if payload.Prefix == "" {
				payload.Prefix = "queues"
			}
			payload.Queue = strings.TrimSpace(payload.Queue)
			if payload.Queue == "" {
				payload.Queue = "default"
			}
			if payload.Payload == "" {
				http.Error(w, `{"error":"failed job payload is required"}`, http.StatusBadRequest)
				return
			}
			redisClient, _, err := openRedisClient(payload.RedisConnID, payload.RedisDB)
			if err != nil {
				writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "failed", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			if _, err := redisClient.command(r.Context(), "LPUSH", laravelQueueKey(payload.Prefix, payload.Queue, ""), payload.Payload); err != nil {
				writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "failed", err.Error(), nil)
				http.Error(w, jsonError("redis lpush failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			if payload.DeleteAfter {
				if err := deleteLaravelFailedJob(r, db, driver, payload.ID); err != nil {
					writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "failed", err.Error(), map[string]any{"delete_after": true})
					http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
					return
				}
			}
			writeLaravelQueueAudit(r, payload.RedisConnID, connID, "retry_failed", payload.Queue, payload.Queue, "", payload.ID, payload.PayloadEdited, "success", "", map[string]any{"delete_after": payload.DeleteAfter})
			json.NewEncoder(w).Encode(map[string]string{"message": "Failed job retried"})
		case "delete-failed":
			enforceConnID := payload.RedisConnID
			if enforceConnID <= 0 {
				enforceConnID = connID
			}
			if err := enforceLaravelQueueAction(enforceConnID, "delete"); err != nil {
				writeLaravelQueueAudit(r, enforceConnID, connID, "delete_failed", "", "", "", payload.ID, false, "blocked", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusForbidden)
				return
			}
			if err := deleteLaravelFailedJob(r, db, driver, payload.ID); err != nil {
				writeLaravelQueueAudit(r, enforceConnID, connID, "delete_failed", "", "", "", payload.ID, false, "failed", err.Error(), nil)
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			writeLaravelQueueAudit(r, enforceConnID, connID, "delete_failed", "", "", "", payload.ID, false, "success", "", nil)
			json.NewEncoder(w).Encode(map[string]string{"message": "Failed job deleted"})
		default:
			http.Error(w, `{"error":"unsupported failed job action"}`, http.StatusBadRequest)
		}
	}
}

func LaravelQueueHorizon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		client, _, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		keys, err := scanRedisKeys(r.Context(), client, "horizon:*", 500)
		if err != nil {
			http.Error(w, jsonError("horizon scan failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		summary := laravelHorizonSummary{
			Detected:   len(keys) > 0,
			KeyCount:   len(keys),
			Workload:   map[string]int64{},
			SampleKeys: keys,
		}
		if len(summary.SampleKeys) > 12 {
			summary.SampleKeys = summary.SampleKeys[:12]
		}
		summary.Supervisors, _ = redisInt(client.command(r.Context(), "SCARD", "horizon:supervisors"))
		summary.Masters, _ = redisInt(client.command(r.Context(), "SCARD", "horizon:masters"))
		summary.RecentJobs, _ = redisInt(client.command(r.Context(), "ZCARD", "horizon:recent_jobs"))
		summary.FailedJobs, _ = redisInt(client.command(r.Context(), "ZCARD", "horizon:failed_jobs"))

		workloadRaw, err := client.command(r.Context(), "HGETALL", "horizon:workload")
		if err == nil {
			for queue, value := range stringPairsToMap(workloadRaw) {
				n, _ := strconv.ParseInt(value, 10, 64)
				summary.Workload[queue] = n
			}
		}
		json.NewEncoder(w).Encode(summary)
	}
}

func deleteLaravelFailedJob(r *http.Request, db *sql.DB, _ string, id int64) error {
	if _, err := db.ExecContext(r.Context(), "DELETE FROM failed_jobs WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed job delete failed: %w", err)
	}
	return nil
}

func laravelQueueRemoveJob(r *http.Request, client *redisClient, payload laravelQueueActionRequest) error {
	if payload.Raw == "" {
		return fmt.Errorf("job payload is required")
	}
	switch payload.State {
	case "ready":
		_, err := client.command(r.Context(), "LREM", laravelQueueKey(payload.Prefix, payload.Queue, ""), "0", payload.Raw)
		if err != nil {
			return fmt.Errorf("redis lrem failed: %w", err)
		}
	case "delayed", "reserved":
		_, err := client.command(r.Context(), "ZREM", laravelQueueKey(payload.Prefix, payload.Queue, payload.State), payload.Raw)
		if err != nil {
			return fmt.Errorf("redis zrem failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported job state")
	}
	return nil
}

func laravelQueuePrefix(r *http.Request) string {
	prefix := strings.Trim(strings.TrimSpace(r.URL.Query().Get("prefix")), ":")
	if prefix == "" {
		return "queues"
	}
	return prefix
}

func laravelQueueNameFromKey(prefix, key string) string {
	base := prefix + ":"
	if !strings.HasPrefix(key, base) {
		return ""
	}
	name := strings.TrimPrefix(key, base)
	for _, suffix := range []string{":delayed", ":reserved", ":notify"} {
		name = strings.TrimSuffix(name, suffix)
	}
	if name == "" || strings.Contains(name, ":") {
		return ""
	}
	return name
}

func laravelQueueKey(prefix, queue, suffix string) string {
	key := prefix + ":" + queue
	if suffix != "" {
		key += ":" + suffix
	}
	return key
}

func parseLaravelQueueJob(raw, queue, state string, score int64) laravelQueueJob {
	sum := sha1.Sum([]byte(raw))
	job := laravelQueueJob{
		ID:    hex.EncodeToString(sum[:8]),
		State: state,
		Queue: queue,
		Score: score,
		Raw:   raw,
	}
	if score > 0 {
		job.AvailableAt = time.Unix(score, 0).Format(time.RFC3339)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return job
	}
	job.Payload = payload
	job.UUID, _ = payload["uuid"].(string)
	job.DisplayName, _ = payload["displayName"].(string)
	job.Job, _ = payload["job"].(string)
	if attempts, ok := payload["attempts"].(float64); ok {
		job.Attempts = int64(attempts)
	}
	job.MaxTries = laravelPayloadInt(payload["maxTries"])
	job.Timeout = laravelPayloadInt(payload["timeout"])
	job.Backoff = payload["backoff"]
	if data, ok := payload["data"].(map[string]any); ok {
		job.CommandName, _ = data["commandName"].(string)
	}
	if job.ID == "" && job.UUID != "" {
		job.ID = job.UUID
	}
	if job.DisplayName == "" && job.CommandName != "" {
		job.DisplayName = job.CommandName
	}
	if job.DisplayName == "" {
		job.DisplayName = fmt.Sprintf("%s job", state)
	}
	return job
}

func laravelPayloadInt(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}
