package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type Schedule struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	ConnID         int64   `json:"conn_id"`
	SQL            string  `json:"sql"`
	Kind           string  `json:"kind"`
	AIPrompt       string  `json:"ai_prompt"`
	CreatedBy      int64   `json:"created_by"`
	IntervalMin    int     `json:"interval_min"`
	AlertCondition string  `json:"alert_condition"` // "row_count_gt" | "row_count_lt" | "value_gt" | ""
	AlertThreshold float64 `json:"alert_threshold"`
	Enabled        bool    `json:"enabled"`
	LastRunAt      string  `json:"last_run_at"`
	NextRunAt      string  `json:"next_run_at"`
	CreatedAt      string  `json:"created_at"`
}

type ScheduleRun struct {
	ID         int64  `json:"id"`
	ScheduleID int64  `json:"schedule_id"`
	RowCount   int64  `json:"row_count"`
	Summary    string `json:"summary"`
	Error      string `json:"error"`
	Alerted    bool   `json:"alerted"`
	RanAt      string `json:"ran_at"`
}

type Notification struct {
	ID         int64  `json:"id"`
	EventID    int64  `json:"event_id"`
	EventType  string `json:"event_type"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	Read       bool   `json:"read"`
	CreatedAt  string `json:"created_at"`
}

func ListSchedules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`
			SELECT id, name, conn_id, sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0),
			       interval_min, COALESCE(alert_condition,''), COALESCE(alert_threshold,0),
			       enabled, COALESCE(last_run_at,''), COALESCE(next_run_at,''), created_at
			FROM schedules ORDER BY id`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []Schedule
		for rows.Next() {
			var s Schedule
			var enabled int
			rows.Scan(&s.ID, &s.Name, &s.ConnID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold,
				&enabled, &s.LastRunAt, &s.NextRunAt, &s.CreatedAt)
			s.Enabled = enabled == 1
			list = append(list, s)
		}
		if list == nil {
			list = []Schedule{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateSchedule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var s Schedule
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil || s.Name == "" || s.SQL == "" {
			http.Error(w, `{"error":"name and sql required"}`, http.StatusBadRequest)
			return
		}
		if s.IntervalMin <= 0 {
			s.IntervalMin = 60
		}
		if strings.TrimSpace(s.Kind) == "" {
			s.Kind = "query"
		}
		userID, _, _ := currentUserFromHeaders(r)
		nextRun := time.Now().Add(time.Duration(s.IntervalMin) * time.Minute).Format("2006-01-02 15:04:05")
		res, err := appdb.DB.Exec(
			`INSERT INTO schedules (name, conn_id, sql, kind, ai_prompt, created_by, interval_min, alert_condition, alert_threshold, enabled, next_run_at, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?,1,?,?)`,
			s.Name, s.ConnID, s.SQL, s.Kind, s.AIPrompt, userID, s.IntervalMin, s.AlertCondition, s.AlertThreshold, nextRun,
			time.Now().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		s.ID, _ = res.LastInsertId()
		s.NextRunAt = nextRun
		s.Enabled = true
		json.NewEncoder(w).Encode(s)
	}
}

func UpdateSchedule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var s Schedule
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		enabled := 0
		if s.Enabled {
			enabled = 1
		}
		if strings.TrimSpace(s.Kind) == "" {
			s.Kind = "query"
		}
		appdb.DB.Exec(
			`UPDATE schedules SET name=?, sql=?, kind=?, ai_prompt=?, interval_min=?, alert_condition=?, alert_threshold=?, enabled=? WHERE id=?`,
			s.Name, s.SQL, s.Kind, s.AIPrompt, s.IntervalMin, s.AlertCondition, s.AlertThreshold, enabled, s.ID,
		)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func DeleteSchedule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM schedules WHERE id=?`), id)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM schedule_runs WHERE schedule_id=?`), id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func RunScheduleNow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-2] // /api/schedules/{id}/run
		var s Schedule
		var enabled int
		err := appdb.DB.QueryRow(
			`SELECT id, name, conn_id, sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0), interval_min, COALESCE(alert_condition,''), COALESCE(alert_threshold,0), enabled FROM schedules WHERE id=?`, id,
		).Scan(&s.ID, &s.Name, &s.ConnID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold, &enabled)
		if err != nil {
			http.Error(w, `{"error":"schedule not found"}`, http.StatusNotFound)
			return
		}
		result, runErr := executeSchedule(s)
		json.NewEncoder(w).Encode(map[string]any{"ok": runErr == nil, "result": result})
	}
}

func GetScheduleRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-2]
		rows, err := appdb.DB.Query(
			`SELECT id, schedule_id, row_count, COALESCE(summary,''), COALESCE(error,''), alerted, ran_at FROM schedule_runs WHERE schedule_id=? ORDER BY id DESC LIMIT 50`, id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []ScheduleRun
		for rows.Next() {
			var sr ScheduleRun
			var alerted int
			rows.Scan(&sr.ID, &sr.ScheduleID, &sr.RowCount, &sr.Summary, &sr.Error, &alerted, &sr.RanAt)
			sr.Alerted = alerted == 1
			list = append(list, sr)
		}
		if list == nil {
			list = []ScheduleRun{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

// Notifications
func ListNotifications() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, event_id, event_type, type, severity, title, message, entity_type, entity_id, read, created_at
			FROM notifications
			WHERE target_user_id = 0 OR target_user_id = ?
			ORDER BY id DESC
			LIMIT 100
		`), userID)
		if err != nil {
			json.NewEncoder(w).Encode([]Notification{})
			return
		}
		defer rows.Close()
		var list []Notification
		for rows.Next() {
			var n Notification
			var read int
			rows.Scan(&n.ID, &n.EventID, &n.EventType, &n.Type, &n.Severity, &n.Title, &n.Message, &n.EntityType, &n.EntityID, &read, &n.CreatedAt)
			n.Read = read == 1
			list = append(list, n)
		}
		if list == nil {
			list = []Notification{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func MarkNotificationsRead() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, _ := currentUserFromHeaders(r)
		appdb.DB.Exec(appdb.ConvertQuery(`UPDATE notifications SET read=1 WHERE target_user_id = 0 OR target_user_id = ?`), userID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func UnreadCount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		var cnt int64
		appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM notifications WHERE read=0 AND (target_user_id = 0 OR target_user_id = ?)`), userID).Scan(&cnt)
		json.NewEncoder(w).Encode(map[string]any{"count": cnt})
	}
}

// executeSchedule runs a schedule's SQL and records the result.
func executeSchedule(s Schedule) (map[string]any, error) {
	db, _, err := GetDB(s.ConnID)
	if err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		return nil, err
	}

	if firstNonEmptyString(strings.TrimSpace(s.Kind), "query") == "ai_summary" {
		return executeAISummarySchedule(s, db)
	}

	rows, err := db.Query(s.SQL)
	var rowCount int64
	if err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		return nil, err
	}
	for rows.Next() {
		rowCount++
	}
	rows.Close()

	// Check alert condition
	alerted := false
	if s.AlertCondition != "" {
		alerted = checkAlert(s.AlertCondition, s.AlertThreshold, rowCount)
		if alerted {
			msg := fmt.Sprintf("Schedule '%s' triggered: %d rows returned (threshold %.0f)", s.Name, rowCount, s.AlertThreshold)
			EmitNotification(NotificationEventInput{
				EventType:    "schedule.alert",
				Category:     "alert",
				Severity:     "warning",
				Title:        "Schedule Alert",
				Message:      msg,
				EntityType:   "schedule",
				EntityID:     s.ID,
				ConnectionID: s.ConnID,
				Payload: map[string]any{
					"schedule_id":   s.ID,
					"schedule_name": s.Name,
					"row_count":     rowCount,
					"threshold":     s.AlertThreshold,
				},
			})
		}
	}

	recordScheduleRun(s.ID, rowCount, "", "", alerted)
	now := time.Now().Format("2006-01-02 15:04:05")
	next := time.Now().Add(time.Duration(s.IntervalMin) * time.Minute).Format("2006-01-02 15:04:05")
	appdb.DB.Exec(appdb.ConvertQuery(`UPDATE schedules SET last_run_at=?, next_run_at=? WHERE id=?`), now, next, s.ID)

	return map[string]any{"row_count": rowCount, "alerted": alerted}, nil
}

func executeAISummarySchedule(s Schedule, dbConn *sql.DB) (map[string]any, error) {
	resolved, err := resolveAISettingsForUserID(s.CreatedBy)
	if err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		emitScheduleAIError(s, err.Error())
		return nil, err
	}

	sqlText := normalizeAnalyticsSQL(s.SQL)
	if err := validateAnalyticsSQL(sqlText); err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		emitScheduleAIError(s, err.Error())
		return nil, err
	}

	queryResult, err := executeAnalyticsQuery(context.Background(), dbConn, sqlText)
	if err != nil {
		msg := sanitizeDBError(err)
		recordScheduleRun(s.ID, 0, "", msg, false)
		emitScheduleAIError(s, msg)
		return nil, err
	}

	plan := aiAnalyticsPlan{
		Title:     firstNonEmptyString(s.Name, "Scheduled AI Summary"),
		SQL:       sqlText,
		ChartType: "table",
	}
	prompt := firstNonEmptyString(strings.TrimSpace(s.AIPrompt), "Summarize the biggest takeaway from this scheduled query result and note any risks or unusual changes.")
	summaryContent, err := callAIText(context.Background(), resolved.APIKey, resolved.BaseURL, resolved.Model, []map[string]string{
		{"role": "system", "content": analyticsSummaryPrompt(prompt, plan, queryResult)},
	}, 900)
	if err != nil {
		recordScheduleRun(s.ID, int64(queryResult.RowCount), "", err.Error(), false)
		emitScheduleAIError(s, err.Error())
		return nil, err
	}

	summary, parseErr := parseAnalyticsSummary(summaryContent)
	if parseErr != nil {
		summary = aiAnalyticsSummary{Summary: strings.TrimSpace(summaryContent), ChartType: "table"}
	}
	finalSummary := firstNonEmptyString(summary.Summary, "Scheduled AI summary completed.")
	recordScheduleRun(s.ID, int64(queryResult.RowCount), finalSummary, "", false)

	now := time.Now().Format("2006-01-02 15:04:05")
	next := time.Now().Add(time.Duration(s.IntervalMin) * time.Minute).Format("2006-01-02 15:04:05")
	appdb.DB.Exec(appdb.ConvertQuery(`UPDATE schedules SET last_run_at=?, next_run_at=? WHERE id=?`), now, next, s.ID)

	EmitNotification(NotificationEventInput{
		EventType:     "schedule.ai_summary",
		Category:      "alert",
		Severity:      "info",
		Title:         fmt.Sprintf("AI summary ready for %s", s.Name),
		Message:       finalSummary,
		EntityType:    "schedule",
		EntityID:      s.ID,
		ConnectionID:  s.ConnID,
		TargetUserIDs: dedupeUserIDs([]int64{s.CreatedBy}),
		Payload: map[string]any{
			"schedule_id":   s.ID,
			"schedule_name": s.Name,
			"row_count":     queryResult.RowCount,
			"summary":       finalSummary,
			"chart_type":    summary.ChartType,
			"follow_ups":    summary.FollowUpQuestions,
			"schedule_kind": "ai_summary",
		},
	})

	return map[string]any{
		"row_count": queryResult.RowCount,
		"summary":   finalSummary,
		"chart":     firstNonEmptyString(summary.ChartType, "table"),
	}, nil
}

func emitScheduleAIError(s Schedule, errMsg string) {
	EmitNotification(NotificationEventInput{
		EventType:     "schedule.ai_summary.failed",
		Category:      "alert",
		Severity:      "error",
		Title:         fmt.Sprintf("AI summary failed for %s", s.Name),
		Message:       errMsg,
		EntityType:    "schedule",
		EntityID:      s.ID,
		ConnectionID:  s.ConnID,
		TargetUserIDs: dedupeUserIDs([]int64{s.CreatedBy}),
		Payload: map[string]any{
			"schedule_id":   s.ID,
			"schedule_name": s.Name,
			"error":         errMsg,
			"schedule_kind": "ai_summary",
		},
	})
}

func checkAlert(condition string, threshold float64, rowCount int64) bool {
	switch condition {
	case "row_count_gt":
		return float64(rowCount) > threshold
	case "row_count_lt":
		return float64(rowCount) < threshold
	case "row_count_eq":
		return float64(rowCount) == threshold
	}
	return false
}

func recordScheduleRun(scheduleID, rowCount int64, summary, errMsg string, alerted bool) {
	al := 0
	if alerted {
		al = 1
	}
	appdb.DB.Exec(
		`INSERT INTO schedule_runs (schedule_id, row_count, summary, error, alerted, ran_at) VALUES (?,?,?,?,?,?)`,
		scheduleID, rowCount, summary, errMsg, al, time.Now().Format("2006-01-02 15:04:05"),
	)
}

var schedulerStop chan struct{}
var schedulerMu sync.Mutex

// StartScheduler runs due schedules in the background.
func StartScheduler() {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	if schedulerStop != nil {
		return
	}
	schedulerStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runDueSchedules()
			case <-schedulerStop:
				return
			}
		}
	}()
}

// StopScheduler stops the background scheduler
func StopScheduler() {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	if schedulerStop != nil {
		close(schedulerStop)
		schedulerStop = nil
	}
}

func runDueSchedules() {
	now := time.Now().Format("2006-01-02 15:04:05")
	rows, err := appdb.DB.Query(
		`SELECT id, name, conn_id, sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0), interval_min, COALESCE(alert_condition,''), COALESCE(alert_threshold,0)
		 FROM schedules WHERE enabled=1 AND (next_run_at IS NULL OR next_run_at <= ?)`, now)
	if err != nil {
		return
	}
	var due []Schedule
	for rows.Next() {
		var s Schedule
		rows.Scan(&s.ID, &s.Name, &s.ConnID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold)
		due = append(due, s)
	}
	rows.Close()
	for _, s := range due {
		go executeSchedule(s) //nolint:errcheck
	}
}
