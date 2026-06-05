package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type FailedJobAlertConfig struct {
	ConnID          int64  `json:"conn_id"`
	Enabled         bool   `json:"enabled"`
	LastSeenID      int64  `json:"last_seen_id"`
	PollIntervalMin int    `json:"poll_interval_min"`
	QueuesFilter    string `json:"queues_filter"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

func GetFailedJobAlertConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cfg := loadFailedJobAlertConfig(connID)
		json.NewEncoder(w).Encode(cfg)
	}
}

func SaveFailedJobAlertConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var req FailedJobAlertConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid body"), http.StatusBadRequest)
			return
		}
		if req.PollIntervalMin < 1 {
			req.PollIntervalMin = 5
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		enabled := 0
		if req.Enabled {
			enabled = 1
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO laravel_failed_job_alerts (conn_id, enabled, poll_interval_min, queues_filter, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(conn_id) DO UPDATE SET
				enabled = excluded.enabled,
				poll_interval_min = excluded.poll_interval_min,
				queues_filter = excluded.queues_filter,
				updated_at = excluded.updated_at
		`), connID, enabled, req.PollIntervalMin, strings.TrimSpace(req.QueuesFilter), now)
		if err != nil {
			// fallback: delete + insert for databases that don't support ON CONFLICT
			appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM laravel_failed_job_alerts WHERE conn_id = ?`), connID)
			_, err = appdb.DB.Exec(appdb.ConvertQuery(`
				INSERT INTO laravel_failed_job_alerts (conn_id, enabled, poll_interval_min, queues_filter, updated_at)
				VALUES (?, ?, ?, ?, ?)
			`), connID, enabled, req.PollIntervalMin, strings.TrimSpace(req.QueuesFilter), now)
			if err != nil {
				http.Error(w, jsonError("failed to save config"), http.StatusInternalServerError)
				return
			}
		}
		json.NewEncoder(w).Encode(loadFailedJobAlertConfig(connID))
	}
}

func loadFailedJobAlertConfig(connID int64) FailedJobAlertConfig {
	cfg := FailedJobAlertConfig{ConnID: connID, PollIntervalMin: 5}
	var enabled int
	var queuesFilter, updatedAt sql.NullString
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT enabled, last_seen_id, poll_interval_min, queues_filter, updated_at
		FROM laravel_failed_job_alerts WHERE conn_id = ?
	`), connID).Scan(&enabled, &cfg.LastSeenID, &cfg.PollIntervalMin, &queuesFilter, &updatedAt)
	if err != nil && err != sql.ErrNoRows {
		fmt.Printf("[failed_job_alert] load config error conn=%d: %v\n", connID, err)
	}
	cfg.Enabled = enabled == 1
	cfg.QueuesFilter = queuesFilter.String
	cfg.UpdatedAt = updatedAt.String
	return cfg
}

func TestFailedJobAlertConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var connName string
		appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(name,'') FROM connections WHERE id = ?`), connID).Scan(&connName)
		if connName == "" {
			connName = fmt.Sprintf("Connection #%d", connID)
		}

		settings := ReadAlertSettings()
		if !settings.Telegram.Enabled && !settings.Discord.Enabled && !settings.Slack.Enabled && !(settings.Webhook.Enabled && strings.TrimSpace(settings.Webhook.URL) != "") {
			http.Error(w, jsonError("no alert channels are enabled — configure at least one in Admin → Alert Settings"), http.StatusBadRequest)
			return
		}

		title := fmt.Sprintf("🔔 Test Alert — %s", connName)
		message := fmt.Sprintf("Connection: %s\nFailed Job Alerts are configured and active on this connection.", connName)
		sendAlertToAllChannels(settings, title, message, "test_connection")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// StartFailedJobAlertWorker runs a background goroutine that polls user database
// connections for new failed Laravel jobs and sends alerts via Alert Settings.
func StartFailedJobAlertWorker() {
	go func() {
		// stagger first run slightly so the app finishes starting
		time.Sleep(30 * time.Second)
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			runFailedJobAlertCheck()
		}
	}()
}

func runFailedJobAlertCheck() {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT conn_id, last_seen_id, poll_interval_min, COALESCE(queues_filter,'')
		FROM laravel_failed_job_alerts
		WHERE enabled = 1
	`))
	if err != nil {
		return
	}
	defer rows.Close()

	type watchEntry struct {
		ConnID          int64
		LastSeenID      int64
		PollIntervalMin int
		QueuesFilter    string
	}
	var entries []watchEntry
	for rows.Next() {
		var e watchEntry
		if err := rows.Scan(&e.ConnID, &e.LastSeenID, &e.PollIntervalMin, &e.QueuesFilter); err == nil {
			entries = append(entries, e)
		}
	}
	rows.Close()

	settings := ReadAlertSettings()
	for _, e := range entries {
		checkFailedJobsForConn(e.ConnID, e.LastSeenID, e.QueuesFilter, settings)
	}
}

func checkFailedJobsForConn(connID, lastSeenID int64, queuesFilter string, settings AlertChannelConfig) {
	db, driver, err := GetDB(connID)
	if err != nil {
		return
	}

	query := `SELECT id, COALESCE(queue,''), COALESCE(payload,''), COALESCE(exception,'') FROM failed_jobs WHERE id > ? ORDER BY id ASC LIMIT 20`
	if driver == "sqlserver" {
		query = `SELECT TOP 20 id, COALESCE(queue,''), COALESCE(payload,''), COALESCE(exception,'') FROM failed_jobs WHERE id > ? ORDER BY id ASC`
	}

	rows, err := db.Query(query, lastSeenID)
	if err != nil {
		return
	}
	defer rows.Close()

	type jobRow struct {
		ID        int64
		Queue     string
		Payload   string
		Exception string
	}
	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		if err := rows.Scan(&j.ID, &j.Queue, &j.Payload, &j.Exception); err == nil {
			jobs = append(jobs, j)
		}
	}
	if len(jobs) == 0 {
		return
	}

	// filter by queue if configured
	allowedQueues := map[string]bool{}
	for _, q := range strings.Split(queuesFilter, ",") {
		q = strings.TrimSpace(q)
		if q != "" {
			allowedQueues[q] = true
		}
	}

	var maxID int64
	for _, j := range jobs {
		if j.ID > maxID {
			maxID = j.ID
		}
		if len(allowedQueues) > 0 && !allowedQueues[j.Queue] {
			continue
		}

		// extract job class from payload
		jobClass := extractJobClass(j.Payload)

		// truncate exception to first line
		exception := strings.SplitN(strings.TrimSpace(j.Exception), "\n", 2)[0]
		if len(exception) > 200 {
			exception = exception[:200] + "…"
		}

		title := fmt.Sprintf("🔴 Failed Job — %s", jobClass)
		message := fmt.Sprintf("Queue: %s\nJob: %s\nError: %s", j.Queue, jobClass, exception)

		sendAlertToAllChannels(settings, title, message, "failed_job")
	}

	// update last_seen_id
	appdb.DB.Exec(appdb.ConvertQuery(`
		UPDATE laravel_failed_job_alerts SET last_seen_id = ?, updated_at = ? WHERE conn_id = ?
	`), maxID, time.Now().UTC().Format("2006-01-02 15:04:05"), connID)
}

func extractJobClass(payload string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return "Unknown"
	}
	if class, ok := m["displayName"].(string); ok && class != "" {
		parts := strings.Split(class, `\`)
		return parts[len(parts)-1]
	}
	if class, ok := m["job"].(string); ok && class != "" {
		parts := strings.Split(class, `\`)
		return parts[len(parts)-1]
	}
	return "Unknown"
}

// sendAlertToAllChannels broadcasts a message to all enabled Alert Settings channels.
func sendAlertToAllChannels(settings AlertChannelConfig, title, message, triggeredBy string) {

	if settings.Telegram.Enabled {
		for _, t := range settings.Telegram.Targets {
			if strings.TrimSpace(t.BotToken) == "" || strings.TrimSpace(t.ChatID) == "" {
				continue
			}
			text := fmt.Sprintf("*%s*\n%s", escapeTelegramMarkdown(title), escapeTelegramMarkdown(message))
			body, _ := json.Marshal(map[string]any{
				"chat_id":              t.ChatID,
				"text":                 text,
				"parse_mode":           "MarkdownV2",
				"disable_notification": t.DisableNotification,
			})
			if t.TopicID != "" {
				var m map[string]any
				json.Unmarshal(body, &m)
				m["message_thread_id"] = t.TopicID
				body, _ = json.Marshal(m)
			}
			endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
			_, err := doNotificationPost(defaultAlertHTTPClient, endpoint, body, nil)
			status, errMsg := "ok", ""
			if err != nil {
				status, errMsg = "error", err.Error()
			}
			LogAlertDelivery("telegram", t.Name, status, errMsg, triggeredBy)
		}
	}

	if settings.Discord.Enabled {
		for _, t := range settings.Discord.Targets {
			if strings.TrimSpace(t.WebhookURL) == "" {
				continue
			}
			payload := map[string]any{"content": fmt.Sprintf("**%s**\n%s", title, message)}
			if t.Username != "" {
				payload["username"] = t.Username
			}
			if t.AvatarURL != "" {
				payload["avatar_url"] = t.AvatarURL
			}
			body, _ := json.Marshal(payload)
			_, err := doNotificationPost(defaultAlertHTTPClient, t.WebhookURL, body, nil)
			status, errMsg := "ok", ""
			if err != nil {
				status, errMsg = "error", err.Error()
			}
			LogAlertDelivery("discord", t.Name, status, errMsg, triggeredBy)
		}
	}

	if settings.Slack.Enabled {
		for _, t := range settings.Slack.Targets {
			if strings.TrimSpace(t.WebhookURL) == "" {
				continue
			}
			payload := map[string]any{"text": fmt.Sprintf("*%s*\n%s", title, message)}
			if t.Channel != "" {
				payload["channel"] = t.Channel
			}
			if t.Username != "" {
				payload["username"] = t.Username
			}
			if t.IconEmoji != "" {
				payload["icon_emoji"] = t.IconEmoji
			}
			body, _ := json.Marshal(payload)
			_, err := doNotificationPost(defaultAlertHTTPClient, t.WebhookURL, body, nil)
			status, errMsg := "ok", ""
			if err != nil {
				status, errMsg = "error", err.Error()
			}
			LogAlertDelivery("slack", t.Name, status, errMsg, triggeredBy)
		}
	}

	if settings.Webhook.Enabled && strings.TrimSpace(settings.Webhook.URL) != "" {
		body, _ := json.Marshal(map[string]any{
			"event":   triggeredBy,
			"title":   title,
			"message": message,
		})
		headers := map[string]string{}
		if settings.Webhook.AuthHeader != "" {
			headers["Authorization"] = settings.Webhook.AuthHeader
		}
		_, err := doNotificationPost(defaultAlertHTTPClient, settings.Webhook.URL, body, headers)
		status, errMsg := "ok", ""
		if err != nil {
			status, errMsg = "error", err.Error()
		}
		LogAlertDelivery("webhook", "", status, errMsg, triggeredBy)
	}
}

var defaultAlertHTTPClient = &http.Client{Timeout: 10 * time.Second}
