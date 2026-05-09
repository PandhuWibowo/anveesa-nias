package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type laravelQueueFeatureFlags struct {
	Retry          bool `json:"retry"`
	Delete         bool `json:"delete"`
	Clear          bool `json:"clear"`
	EditedReplay   bool `json:"editedReplay"`
	ReadOnly       bool `json:"readOnly"`
	RequireConfirm bool `json:"requireConfirm"`
}

type laravelQueueRules struct {
	ReadyMax         int64 `json:"readyMax"`
	FailedMax        int64 `json:"failedMax"`
	StuckMax         int64 `json:"stuckMax"`
	OldestMinutesMax int64 `json:"oldestMinutesMax"`
	NoConsumption    bool  `json:"noConsumption"`
}

type laravelQueueOpsSettings struct {
	FeatureFlags        laravelQueueFeatureFlags `json:"featureFlags"`
	QueueRules          laravelQueueRules        `json:"queueRules"`
	BusinessFieldsInput string                   `json:"businessFieldsInput"`
	SandboxQueue        string                   `json:"sandboxQueue"`
	Environment         string                   `json:"environment"`
	UpdatedAt           string                   `json:"updated_at,omitempty"`
	UpdatedBy           int64                    `json:"updated_by,omitempty"`
}

type laravelQueueAuditRow struct {
	ID            int64          `json:"id"`
	ConnID        int64          `json:"conn_id"`
	FailedConnID  int64          `json:"failed_conn_id"`
	UserID        int64          `json:"user_id"`
	Username      string         `json:"username"`
	Action        string         `json:"action"`
	Queue         string         `json:"queue"`
	TargetQueue   string         `json:"target_queue"`
	JobUUID       string         `json:"job_uuid"`
	FailedJobID   int64          `json:"failed_job_id"`
	PayloadEdited bool           `json:"payload_edited"`
	Status        string         `json:"status"`
	Error         string         `json:"error"`
	Details       map[string]any `json:"details"`
	CreatedAt     string         `json:"created_at"`
}

type laravelQueueQuarantineRow struct {
	ID           int64  `json:"id"`
	ConnID       int64  `json:"conn_id"`
	FailedConnID int64  `json:"failed_conn_id"`
	FailedJobID  int64  `json:"failed_job_id"`
	UUID         string `json:"uuid"`
	Queue        string `json:"queue"`
	JobName      string `json:"job_name"`
	Payload      string `json:"payload"`
	Exception    string `json:"exception"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
	CreatedBy    int64  `json:"created_by"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type laravelQueueQuarantineRequest struct {
	FailedConnID int64  `json:"failed_conn_id"`
	FailedJobID  int64  `json:"failed_job_id"`
	UUID         string `json:"uuid"`
	Queue        string `json:"queue"`
	JobName      string `json:"job_name"`
	Payload      string `json:"payload"`
	Exception    string `json:"exception"`
	Reason       string `json:"reason"`
}

type laravelQueueAlertRequest struct {
	Alerts []struct {
		Level  string `json:"level"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"alerts"`
	Queue  string `json:"queue"`
	Prefix string `json:"prefix"`
}

type laravelQueueAgentRequest struct {
	Command string         `json:"command"`
	Queue   string         `json:"queue"`
	Options map[string]any `json:"options"`
}

func LaravelQueueOpsSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			settings, err := getLaravelQueueOpsSettings(connID)
			if err != nil {
				http.Error(w, jsonError("failed to load queue settings"), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(settings)
		case http.MethodPut:
			var settings laravelQueueOpsSettings
			if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
				http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
				return
			}
			settings = normalizeLaravelQueueOpsSettings(settings)
			userID, _, _ := currentUserFromHeaders(r)
			if err := saveLaravelQueueOpsSettings(connID, userID, settings); err != nil {
				http.Error(w, jsonError("failed to save queue settings"), http.StatusInternalServerError)
				return
			}
			writeLaravelQueueAudit(r, connID, 0, "settings_update", "", "", "", 0, false, "success", "", map[string]any{"environment": settings.Environment})
			json.NewEncoder(w).Encode(settings)
		default:
			http.NotFound(w, r)
		}
	}
}

func LaravelQueueAudit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		limit := queryInt(r, "limit", 100, 1, 500)
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, conn_id, failed_conn_id, user_id, username, action, queue, target_queue, job_uuid, failed_job_id,
			       payload_edited, status, error, details_json, created_at
			FROM laravel_queue_audit
			WHERE conn_id = ?
			ORDER BY id DESC
			LIMIT ?
		`), connID, limit)
		if err != nil {
			http.Error(w, jsonError("failed to load queue audit"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := []laravelQueueAuditRow{}
		for rows.Next() {
			var item laravelQueueAuditRow
			var edited int
			var details string
			if err := rows.Scan(&item.ID, &item.ConnID, &item.FailedConnID, &item.UserID, &item.Username, &item.Action, &item.Queue, &item.TargetQueue, &item.JobUUID, &item.FailedJobID, &edited, &item.Status, &item.Error, &details, &item.CreatedAt); err != nil {
				http.Error(w, jsonError("failed to read queue audit"), http.StatusInternalServerError)
				return
			}
			item.PayloadEdited = edited == 1
			item.Details = parseJSONMapSafe(details)
			items = append(items, item)
		}
		json.NewEncoder(w).Encode(items)
	}
}

func LaravelQueueQuarantine() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			items, err := listLaravelQueueQuarantine(connID)
			if err != nil {
				http.Error(w, jsonError("failed to load quarantine"), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(items)
		case http.MethodPost:
			var body laravelQueueQuarantineRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
				return
			}
			if body.FailedJobID <= 0 {
				http.Error(w, `{"error":"failed job id is required"}`, http.StatusBadRequest)
				return
			}
			userID, _, _ := currentUserFromHeaders(r)
			now := time.Now().UTC().Format("2006-01-02 15:04:05")
			_, err := appdb.DB.Exec(appdb.ConvertQuery(`
				INSERT INTO laravel_queue_quarantine
					(conn_id, failed_conn_id, failed_job_id, uuid, queue, job_name, payload, exception, reason, status, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'quarantined', ?, ?, ?)
				ON CONFLICT(conn_id, failed_conn_id, failed_job_id)
				DO UPDATE SET uuid = excluded.uuid, queue = excluded.queue, job_name = excluded.job_name, payload = excluded.payload,
					exception = excluded.exception, reason = excluded.reason, status = 'quarantined', updated_at = excluded.updated_at
			`), connID, body.FailedConnID, body.FailedJobID, body.UUID, body.Queue, body.JobName, body.Payload, body.Exception, body.Reason, userID, now, now)
			if err != nil {
				// MySQL fallback for ON CONFLICT.
				_, err = appdb.DB.Exec(appdb.ConvertQuery(`
					INSERT INTO laravel_queue_quarantine
						(conn_id, failed_conn_id, failed_job_id, uuid, queue, job_name, payload, exception, reason, status, created_by, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'quarantined', ?, ?, ?)
				`), connID, body.FailedConnID, body.FailedJobID, body.UUID, body.Queue, body.JobName, body.Payload, body.Exception, body.Reason, userID, now, now)
			}
			if err != nil {
				http.Error(w, jsonError("failed to quarantine job"), http.StatusInternalServerError)
				return
			}
			writeLaravelQueueAudit(r, connID, body.FailedConnID, "quarantine", body.Queue, "", body.UUID, body.FailedJobID, false, "success", "", map[string]any{"reason": body.Reason})
			json.NewEncoder(w).Encode(map[string]string{"message": "Job quarantined"})
		default:
			http.NotFound(w, r)
		}
	}
}

func LaravelQueueQuarantineItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], ""), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, `{"error":"invalid quarantine id"}`, http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE laravel_queue_quarantine SET status = 'released', updated_at = ? WHERE id = ? AND conn_id = ?`), time.Now().UTC().Format("2006-01-02 15:04:05"), id, connID)
		if err != nil {
			http.Error(w, jsonError("failed to release quarantine item"), http.StatusInternalServerError)
			return
		}
		writeLaravelQueueAudit(r, connID, 0, "quarantine_release", "", "", "", id, false, "success", "", nil)
		json.NewEncoder(w).Encode(map[string]string{"message": "Quarantine item released"})
	}
}

func LaravelQueueAlerts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var body laravelQueueAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		userID, _, _ := currentUserFromHeaders(r)
		count := 0
		for _, alert := range body.Alerts {
			title := strings.TrimSpace(alert.Title)
			detail := strings.TrimSpace(alert.Detail)
			if title == "" || detail == "" {
				continue
			}
			EmitNotification(NotificationEventInput{
				EventType:    "laravel_queue.alert",
				Category:     "queue",
				Severity:     alert.Level,
				Title:        title,
				Message:      detail,
				EntityType:   "laravel_queue",
				ConnectionID: connID,
				ActorUserID:  userID,
				Payload: map[string]any{
					"queue":  body.Queue,
					"prefix": body.Prefix,
					"detail": detail,
				},
			})
			count++
		}
		writeLaravelQueueAudit(r, connID, 0, "alerts_emit", body.Queue, "", "", 0, false, "success", "", map[string]any{"count": count})
		json.NewEncoder(w).Encode(map[string]any{"message": "alerts emitted", "count": count})
	}
}

func LaravelQueueAgentAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var body laravelQueueAgentRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeLaravelQueueAudit(r, connID, 0, "agent_"+strings.TrimSpace(body.Command), body.Queue, "", "", 0, false, "blocked", "laravel agent is not configured", body.Options)
		http.Error(w, jsonError("Laravel agent is not configured yet. Install a Laravel-side agent/API before running queue worker commands."), http.StatusNotImplemented)
	}
}

func getLaravelQueueOpsSettings(connID int64) (laravelQueueOpsSettings, error) {
	settings := defaultLaravelQueueOpsSettings()
	var raw, updatedAt sql.NullString
	var updatedBy sql.NullInt64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT settings_json, updated_at, updated_by
		FROM laravel_queue_profiles
		WHERE conn_id = ? AND environment = 'default'
	`), connID).Scan(&raw, &updatedAt, &updatedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return settings, nil
		}
		return settings, err
	}
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &settings)
	}
	settings = normalizeLaravelQueueOpsSettings(settings)
	settings.UpdatedAt = updatedAt.String
	settings.UpdatedBy = updatedBy.Int64
	return settings, nil
}

func saveLaravelQueueOpsSettings(connID, userID int64, settings laravelQueueOpsSettings) error {
	settings.Environment = "default"
	raw, _ := json.Marshal(settings)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Try UPSERT (SQLite / PostgreSQL ON CONFLICT syntax)
	_, err := appdb.DB.Exec(appdb.ConvertQuery(`
		INSERT INTO laravel_queue_profiles
			(conn_id, environment, settings_json, created_by, updated_by, created_at, updated_at)
		VALUES (?, 'default', ?, ?, ?, ?, ?)
		ON CONFLICT(conn_id, environment)
		DO UPDATE SET settings_json = excluded.settings_json, updated_by = excluded.updated_by, updated_at = excluded.updated_at
	`), connID, string(raw), userID, userID, now, now)
	if err == nil {
		return nil
	}

	// MySQL fallback: INSERT ... ON DUPLICATE KEY UPDATE
	_, err = appdb.DB.Exec(`
		INSERT INTO laravel_queue_profiles
			(conn_id, environment, settings_json, created_by, updated_by, created_at, updated_at)
		VALUES (?, 'default', ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE settings_json = VALUES(settings_json), updated_by = VALUES(updated_by), updated_at = VALUES(updated_at)
	`, connID, string(raw), userID, userID, now, now)
	return err
}

func defaultLaravelQueueOpsSettings() laravelQueueOpsSettings {
	return laravelQueueOpsSettings{
		Environment: "default",
		FeatureFlags: laravelQueueFeatureFlags{
			Retry:          true,
			Delete:         true,
			Clear:          true,
			EditedReplay:   true,
			ReadOnly:       false,
			RequireConfirm: true,
		},
		QueueRules: laravelQueueRules{
			ReadyMax:         100,
			FailedMax:        10,
			StuckMax:         0,
			OldestMinutesMax: 30,
			NoConsumption:    true,
		},
		BusinessFieldsInput: "tenant_id,user_id,order_id,invoice_id,email,amount",
		SandboxQueue:        "debug",
	}
}

func normalizeLaravelQueueOpsSettings(settings laravelQueueOpsSettings) laravelQueueOpsSettings {
	def := defaultLaravelQueueOpsSettings()
	if settings.Environment == "" {
		settings.Environment = "default"
	}
	if settings.BusinessFieldsInput == "" {
		settings.BusinessFieldsInput = def.BusinessFieldsInput
	}
	if settings.SandboxQueue == "" {
		settings.SandboxQueue = def.SandboxQueue
	}
	return settings
}

func enforceLaravelQueueAction(connID int64, action string) error {
	settings, err := getLaravelQueueOpsSettings(connID)
	if err != nil {
		return nil
	}
	flags := settings.FeatureFlags
	if flags.ReadOnly {
		return fmt.Errorf("read-only mode is enabled")
	}
	switch action {
	case "retry":
		if !flags.Retry {
			return fmt.Errorf("retry actions are disabled")
		}
	case "delete":
		if !flags.Delete {
			return fmt.Errorf("delete actions are disabled")
		}
	case "clear":
		if !flags.Clear {
			return fmt.Errorf("clear queue is disabled")
		}
	case "editedReplay":
		if !flags.EditedReplay {
			return fmt.Errorf("edited payload replay is disabled")
		}
	}
	return nil
}

func listLaravelQueueQuarantine(connID int64) ([]laravelQueueQuarantineRow, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, conn_id, failed_conn_id, failed_job_id, uuid, queue, job_name, payload, exception, reason, status, created_by, created_at, updated_at
		FROM laravel_queue_quarantine
		WHERE conn_id = ? AND status = 'quarantined'
		ORDER BY id DESC
	`), connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []laravelQueueQuarantineRow{}
	for rows.Next() {
		var item laravelQueueQuarantineRow
		if err := rows.Scan(&item.ID, &item.ConnID, &item.FailedConnID, &item.FailedJobID, &item.UUID, &item.Queue, &item.JobName, &item.Payload, &item.Exception, &item.Reason, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func writeLaravelQueueAudit(r *http.Request, connID, failedConnID int64, action, queue, targetQueue, jobUUID string, failedJobID int64, payloadEdited bool, status, actionErr string, details map[string]any) {
	userID, username, _ := currentUserFromHeaders(r)
	if details == nil {
		details = map[string]any{}
	}
	raw, _ := json.Marshal(details)
	edited := 0
	if payloadEdited {
		edited = 1
	}
	_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
		INSERT INTO laravel_queue_audit
			(conn_id, failed_conn_id, user_id, username, action, queue, target_queue, job_uuid, failed_job_id, payload_edited, status, error, details_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`), connID, failedConnID, userID, username, action, queue, targetQueue, jobUUID, failedJobID, edited, status, actionErr, string(raw), time.Now().UTC().Format("2006-01-02 15:04:05"))
}
