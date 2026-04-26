package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anveesa/nias/cache"
	appdb "github.com/anveesa/nias/db"
)

type Schedule struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	ConnID         int64   `json:"conn_id"`
	DashboardID    int64   `json:"dashboard_id"`
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
			SELECT id, name, conn_id, COALESCE(dashboard_id,0), sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0),
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
			rows.Scan(&s.ID, &s.Name, &s.ConnID, &s.DashboardID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold,
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
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil || strings.TrimSpace(s.Name) == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		if s.IntervalMin <= 0 {
			s.IntervalMin = 60
		}
		if strings.TrimSpace(s.Kind) == "" {
			s.Kind = "query"
		}
		if s.Kind == "dashboard_report" {
			if s.DashboardID <= 0 {
				http.Error(w, `{"error":"dashboard_id required"}`, http.StatusBadRequest)
				return
			}
			s.ConnID = 0
			s.SQL = ""
		} else if strings.TrimSpace(s.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}
		userID, _, _ := currentUserFromHeaders(r)
		nextRun := time.Now().Add(time.Duration(s.IntervalMin) * time.Minute).Format("2006-01-02 15:04:05")
		res, err := appdb.DB.Exec(
			`INSERT INTO schedules (name, conn_id, dashboard_id, sql, kind, ai_prompt, created_by, interval_min, alert_condition, alert_threshold, enabled, next_run_at, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,1,?,?)`,
			s.Name, s.ConnID, s.DashboardID, s.SQL, s.Kind, s.AIPrompt, userID, s.IntervalMin, s.AlertCondition, s.AlertThreshold, nextRun,
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
		if s.Kind == "dashboard_report" {
			s.ConnID = 0
			s.SQL = ""
		}
		appdb.DB.Exec(
			`UPDATE schedules SET name=?, conn_id=?, dashboard_id=?, sql=?, kind=?, ai_prompt=?, interval_min=?, alert_condition=?, alert_threshold=?, enabled=? WHERE id=?`,
			s.Name, s.ConnID, s.DashboardID, s.SQL, s.Kind, s.AIPrompt, s.IntervalMin, s.AlertCondition, s.AlertThreshold, enabled, s.ID,
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
			`SELECT id, name, conn_id, COALESCE(dashboard_id,0), sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0), interval_min, COALESCE(alert_condition,''), COALESCE(alert_threshold,0), enabled FROM schedules WHERE id=?`, id,
		).Scan(&s.ID, &s.Name, &s.ConnID, &s.DashboardID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold, &enabled)
		if err != nil {
			http.Error(w, `{"error":"schedule not found"}`, http.StatusNotFound)
			return
		}
		result, runErr := executeScheduleWithLock(s, true)
		if runErr != nil && strings.Contains(strings.ToLower(runErr.Error()), "already running") {
			http.Error(w, `{"error":"schedule is already running"}`, http.StatusConflict)
			return
		}
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
		invalidateNotificationCountCache(0, userID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func UnreadCount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, _ := currentUserFromHeaders(r)
		cachedJSONResponse(w, r, "notifications:unread:"+strconv.FormatInt(userID, 10), 20*time.Second, func() (any, error) {
			var cnt int64
			err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM notifications WHERE read=0 AND (target_user_id = 0 OR target_user_id = ?)`), userID).Scan(&cnt)
			if err != nil {
				return nil, err
			}
			return map[string]any{"count": cnt}, nil
		})
	}
}

// executeSchedule runs a schedule's SQL and records the result.
func executeSchedule(s Schedule) (map[string]any, error) {
	switch firstNonEmptyString(strings.TrimSpace(s.Kind), "query") {
	case "ai_summary":
		db, _, err := GetDB(s.ConnID)
		if err != nil {
			recordScheduleRun(s.ID, 0, "", err.Error(), false)
			return nil, err
		}
		return executeAISummarySchedule(s, db)
	case "dashboard_report":
		return executeDashboardReportSchedule(s)
	}
	db, _, err := GetDB(s.ConnID)
	if err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		return nil, err
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
		{"role": "system", "content": analyticsSummaryPrompt(prompt, "", plan, queryResult)},
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

func executeDashboardReportSchedule(s Schedule) (map[string]any, error) {
	rendered, err := renderAnalyticsDashboardForUser(s.CreatedBy, s.DashboardID, nil)
	if err != nil {
		recordScheduleRun(s.ID, 0, "", err.Error(), false)
		emitDashboardReportError(s, err.Error())
		return nil, err
	}
	successBlocks := 0
	failedBlocks := 0
	totalRows := 0
	titles := make([]string, 0, len(rendered.Blocks))
	for _, block := range rendered.Blocks {
		if strings.TrimSpace(block.Error) != "" {
			failedBlocks++
			continue
		}
		successBlocks++
		totalRows += block.RowCount
		titles = append(titles, block.Title)
	}
	summary := fmt.Sprintf("Dashboard \"%s\" rendered with %d successful blocks, %d failed blocks, and %d total rows.", rendered.Name, successBlocks, failedBlocks, totalRows)
	recordScheduleRun(s.ID, int64(totalRows), summary, "", failedBlocks > 0)
	now := time.Now().Format("2006-01-02 15:04:05")
	next := time.Now().Add(time.Duration(s.IntervalMin) * time.Minute).Format("2006-01-02 15:04:05")
	appdb.DB.Exec(appdb.ConvertQuery(`UPDATE schedules SET last_run_at=?, next_run_at=? WHERE id=?`), now, next, s.ID)

	EmitNotification(NotificationEventInput{
		EventType:     "dashboard.report",
		Category:      "alert",
		Severity:      ternarySeverity(failedBlocks > 0, "warning", "info"),
		Title:         fmt.Sprintf("Dashboard report ready for %s", rendered.Name),
		Message:       summary,
		EntityType:    "analytics_dashboard",
		EntityID:      s.DashboardID,
		TargetUserIDs: dedupeUserIDs([]int64{s.CreatedBy}),
		Payload: map[string]any{
			"schedule_id":       s.ID,
			"schedule_name":     s.Name,
			"dashboard_id":      s.DashboardID,
			"dashboard_name":    rendered.Name,
			"successful_blocks": successBlocks,
			"failed_blocks":     failedBlocks,
			"row_count":         totalRows,
			"block_titles":      titles,
		},
	})

	return map[string]any{
		"dashboard_id":      s.DashboardID,
		"dashboard_name":    rendered.Name,
		"successful_blocks": successBlocks,
		"failed_blocks":     failedBlocks,
		"row_count":         totalRows,
		"summary":           summary,
	}, nil
}

func emitDashboardReportError(s Schedule, errMsg string) {
	EmitNotification(NotificationEventInput{
		EventType:     "dashboard.report.failed",
		Category:      "alert",
		Severity:      "error",
		Title:         fmt.Sprintf("Dashboard report failed for schedule %s", s.Name),
		Message:       errMsg,
		EntityType:    "analytics_dashboard",
		EntityID:      s.DashboardID,
		TargetUserIDs: dedupeUserIDs([]int64{s.CreatedBy}),
		Payload: map[string]any{
			"schedule_id":   s.ID,
			"schedule_name": s.Name,
			"dashboard_id":  s.DashboardID,
			"error":         errMsg,
		},
	})
}

func ternarySeverity(condition bool, whenTrue, whenFalse string) string {
	if condition {
		return whenTrue
	}
	return whenFalse
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
var schedulerInstanceID = fmt.Sprintf("scheduler-%d", time.Now().UTC().UnixNano())

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
				processSchedulerTick()
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
		`SELECT id, name, conn_id, COALESCE(dashboard_id,0), sql, COALESCE(kind,'query'), COALESCE(ai_prompt,''), COALESCE(created_by,0), interval_min, COALESCE(alert_condition,''), COALESCE(alert_threshold,0)
		 FROM schedules WHERE enabled=1 AND (next_run_at IS NULL OR next_run_at <= ?)`, now)
	if err != nil {
		return
	}
	var due []Schedule
	for rows.Next() {
		var s Schedule
		rows.Scan(&s.ID, &s.Name, &s.ConnID, &s.DashboardID, &s.SQL, &s.Kind, &s.AIPrompt, &s.CreatedBy, &s.IntervalMin, &s.AlertCondition, &s.AlertThreshold)
		due = append(due, s)
	}
	rows.Close()
	for _, s := range due {
		go executeScheduleWithLock(s, false) //nolint:errcheck
	}
}

func processSchedulerTick() {
	lockKey := "scheduler:tick"
	owner := fmt.Sprintf("%s:%d", schedulerInstanceID, time.Now().UTC().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	locked, err := cache.Default().AcquireLock(ctx, lockKey, owner, 55*time.Second)
	cancel()
	if err != nil || !locked {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		_ = cache.Default().ReleaseLock(releaseCtx, lockKey, owner)
	}()
	runDueSchedules()
}

func executeScheduleWithLock(s Schedule, manual bool) (map[string]any, error) {
	lockKey := fmt.Sprintf("schedule:run:%d", s.ID)
	owner := fmt.Sprintf("%s:%d", schedulerInstanceID, time.Now().UTC().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	locked, err := cache.Default().AcquireLock(ctx, lockKey, owner, 10*time.Minute)
	if err != nil {
		if manual {
			return nil, fmt.Errorf("schedule lock failed: %w", err)
		}
		return nil, nil
	}
	if !locked {
		if manual {
			return nil, fmt.Errorf("schedule is already running")
		}
		return nil, nil
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		_ = cache.Default().ReleaseLock(releaseCtx, lockKey, owner)
	}()

	return executeSchedule(s)
}
