package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

type NotificationEvent struct {
	ID           int64          `json:"id"`
	EventType    string         `json:"event_type"`
	Category     string         `json:"category"`
	Severity     string         `json:"severity"`
	Title        string         `json:"title"`
	Message      string         `json:"message"`
	EntityType   string         `json:"entity_type"`
	EntityID     int64          `json:"entity_id"`
	ConnectionID int64          `json:"connection_id"`
	ActorUserID  int64          `json:"actor_user_id"`
	Payload      map[string]any `json:"payload"`
	CreatedAt    string         `json:"created_at"`
}

type NotificationEventInput struct {
	EventType     string
	Category      string
	Severity      string
	Title         string
	Message       string
	EntityType    string
	EntityID      int64
	ConnectionID  int64
	ActorUserID   int64
	TargetUserIDs []int64
	Payload       map[string]any
}

type NotificationTarget struct {
	ID                 int64          `json:"id"`
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	Description        string         `json:"description"`
	Config             map[string]any `json:"config"`
	IsActive           bool           `json:"is_active"`
	CreatedBy          int64          `json:"created_by"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	HasSecret          bool           `json:"has_secret"`
	HasSecondarySecret bool           `json:"has_secondary_secret"`
}

type NotificationRule struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	EventType       string `json:"event_type"`
	Severity        string `json:"severity"`
	EntityType      string `json:"entity_type"`
	ConnectionID    int64  `json:"connection_id"`
	ActorUserID     int64  `json:"actor_user_id"`
	TitleTemplate   string `json:"title_template"`
	MessageTemplate string `json:"message_template"`
	TargetID        int64  `json:"target_id"`
	IsActive        bool   `json:"is_active"`
	CreatedBy       int64  `json:"created_by"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type NotificationDelivery struct {
	ID               int64             `json:"id"`
	EventID          int64             `json:"event_id"`
	TargetID         int64             `json:"target_id"`
	TargetName       string            `json:"target_name"`
	Channel          string            `json:"channel"`
	Status           string            `json:"status"`
	Attempts         int               `json:"attempts"`
	LastError        string            `json:"last_error"`
	LastResponseCode int               `json:"last_response_code"`
	Payload          map[string]any    `json:"payload"`
	NextAttemptAt    string            `json:"next_attempt_at"`
	LastAttemptAt    string            `json:"last_attempt_at"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	Event            NotificationEvent `json:"event"`
}

type notificationTargetPayload struct {
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Description     string         `json:"description"`
	Config          map[string]any `json:"config"`
	Secret          string         `json:"secret"`
	SecondarySecret string         `json:"secondary_secret"`
	IsActive        *bool          `json:"is_active"`
}

type notificationRulePayload struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	EventType       string `json:"event_type"`
	Severity        string `json:"severity"`
	EntityType      string `json:"entity_type"`
	ConnectionID    int64  `json:"connection_id"`
	ActorUserID     int64  `json:"actor_user_id"`
	TitleTemplate   string `json:"title_template"`
	MessageTemplate string `json:"message_template"`
	TargetID        int64  `json:"target_id"`
	IsActive        *bool  `json:"is_active"`
}

var (
	notificationStop chan struct{}
	notificationMu   sync.Mutex
)
var notificationInstanceID = fmt.Sprintf("notification-%d", time.Now().UTC().UnixNano())

func StartNotificationWorker() {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	if notificationStop != nil {
		return
	}
	notificationStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				processNotificationWorkerTick()
			case <-notificationStop:
				return
			}
		}
	}()
}

func StopNotificationWorker() {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	if notificationStop != nil {
		close(notificationStop)
		notificationStop = nil
	}
}

func EmitNotification(input NotificationEventInput) {
	input.EventType = strings.TrimSpace(input.EventType)
	input.Category = strings.TrimSpace(input.Category)
	input.Severity = normalizeNotificationSeverity(input.Severity)
	input.Title = strings.TrimSpace(input.Title)
	input.Message = strings.TrimSpace(input.Message)
	if input.EventType == "" || input.Title == "" || input.Message == "" {
		return
	}
	if input.Category == "" {
		input.Category = "system"
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}

	payloadJSON, err := json.Marshal(input.Payload)
	if err != nil {
		payloadJSON = []byte(`{}`)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	eventID, err := insertRowReturningID(appdb.ConvertQuery(`
		INSERT INTO notification_events
			(event_type, category, severity, title, message, entity_type, entity_id, connection_id, actor_user_id, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`), input.EventType, input.Category, input.Severity, input.Title, input.Message, input.EntityType, input.EntityID, input.ConnectionID, input.ActorUserID, string(payloadJSON), now)
	if err != nil {
		return
	}

	targetUserIDs := dedupeUserIDs(input.TargetUserIDs)
	if len(targetUserIDs) == 0 {
		targetUserIDs = []int64{0}
	}
	for _, targetUserID := range targetUserIDs {
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO notifications
				(event_id, target_user_id, event_type, severity, type, title, message, entity_type, entity_id, read, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
		`), eventID, targetUserID, input.EventType, input.Severity, input.Category, input.Title, input.Message, input.EntityType, input.EntityID, now)
	}

	queueNotificationDeliveries(eventID, input.EventType, input.Severity, input.ConnectionID, string(payloadJSON), now)
}

func ListNotificationEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, event_type, category, severity, title, message, entity_type, entity_id, connection_id, actor_user_id, payload, created_at
			FROM notification_events
			ORDER BY id DESC
			LIMIT ?
		`), limit)
		if err != nil {
			http.Error(w, jsonError("failed to list notification events"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		events, err := scanNotificationEvents(rows)
		if err != nil {
			http.Error(w, jsonError("failed to read notification events"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(events)
	}
}

func ListNotificationTargets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, name, type, description, config_json, secret_enc, secondary_secret_enc, is_active, created_by, created_at, updated_at
			FROM notification_targets
			ORDER BY updated_at DESC, id DESC
		`))
		if err != nil {
			http.Error(w, jsonError("failed to list notification targets"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items, err := scanNotificationTargets(rows)
		if err != nil {
			http.Error(w, jsonError("failed to read notification targets"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(items)
	}
}

func CreateNotificationTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		var body notificationTargetPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		target, secretEnc, secondarySecretEnc, err := validateNotificationTargetPayload(body, true)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		configJSON, _ := json.Marshal(target.Config)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		id, err := insertRowReturningID(appdb.ConvertQuery(`
			INSERT INTO notification_targets
				(name, type, description, config_json, secret_enc, secondary_secret_enc, is_active, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`), target.Name, target.Type, target.Description, string(configJSON), secretEnc, secondarySecretEnc, boolToInt(target.IsActive), userID, now, now)
		if err != nil {
			http.Error(w, jsonError("failed to create notification target"), http.StatusInternalServerError)
			return
		}
		created, err := getNotificationTargetByID(id)
		if err != nil || created == nil {
			http.Error(w, jsonError("notification target created but could not be loaded"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}
}

func UpdateNotificationTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseIDFromPath(r.URL.Path, "/api/notification-targets/")
		if err != nil {
			http.Error(w, jsonError("invalid target id"), http.StatusBadRequest)
			return
		}
		existing, err := getNotificationTargetRowByID(id)
		if err != nil || existing == nil {
			http.Error(w, jsonError("notification target not found"), http.StatusNotFound)
			return
		}
		var body notificationTargetPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		target, secretEnc, secondarySecretEnc, err := validateNotificationTargetPayload(body, false)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if secretEnc == "" {
			secretEnc = existing.SecretEnc
		}
		if secondarySecretEnc == "" {
			secondarySecretEnc = existing.SecondarySecretEnc
		}
		configJSON, _ := json.Marshal(target.Config)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE notification_targets
			SET name = ?, type = ?, description = ?, config_json = ?, secret_enc = ?, secondary_secret_enc = ?, is_active = ?, updated_at = ?
			WHERE id = ?
		`), target.Name, target.Type, target.Description, string(configJSON), secretEnc, secondarySecretEnc, boolToInt(target.IsActive), now, id)
		if err != nil {
			http.Error(w, jsonError("failed to update notification target"), http.StatusInternalServerError)
			return
		}
		updated, _ := getNotificationTargetByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func DeleteNotificationTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDFromPath(r.URL.Path, "/api/notification-targets/")
		if err != nil {
			http.Error(w, jsonError("invalid target id"), http.StatusBadRequest)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM notification_targets WHERE id = ?`), id)
		if err != nil {
			http.Error(w, jsonError("failed to delete notification target"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestNotificationTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePathActionID(r.URL.Path, "/api/notification-targets/", "/test")
		if err != nil {
			http.Error(w, jsonError("invalid target id"), http.StatusBadRequest)
			return
		}
		row, err := getNotificationTargetRowByID(id)
		if err != nil || row == nil {
			http.Error(w, jsonError("notification target not found"), http.StatusNotFound)
			return
		}
		event := NotificationEvent{
			ID:        0,
			EventType: "system.test",
			Category:  "system",
			Severity:  "info",
			Title:     "Notification test",
			Message:   fmt.Sprintf("Test message for %s integration", row.Name),
			Payload: map[string]any{
				"source": "notification_target_test",
			},
			CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		}
		payloadJSON, _ := json.Marshal(map[string]any{
			"event_type": event.EventType,
			"title":      event.Title,
			"message":    event.Message,
			"severity":   event.Severity,
			"payload":    event.Payload,
		})
		statusCode, err := sendNotificationToTarget(*row, event, payloadJSON)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "status_code": statusCode})
	}
}

func ListNotificationRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, name, description, event_type, severity, entity_type, connection_id, actor_user_id, title_template, message_template, target_id, is_active, created_by, created_at, updated_at
			FROM notification_rules
			ORDER BY updated_at DESC, id DESC
		`))
		if err != nil {
			http.Error(w, jsonError("failed to list notification rules"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items, err := scanNotificationRules(rows)
		if err != nil {
			http.Error(w, jsonError("failed to read notification rules"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(items)
	}
}

func CreateNotificationRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		var body notificationRulePayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		rule, err := validateNotificationRulePayload(body)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		id, err := insertRowReturningID(appdb.ConvertQuery(`
			INSERT INTO notification_rules
				(name, description, event_type, severity, entity_type, connection_id, actor_user_id, title_template, message_template, target_id, is_active, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`), rule.Name, rule.Description, rule.EventType, rule.Severity, rule.EntityType, rule.ConnectionID, rule.ActorUserID, rule.TitleTemplate, rule.MessageTemplate, rule.TargetID, boolToInt(rule.IsActive), userID, now, now)
		if err != nil {
			http.Error(w, jsonError("failed to create notification rule"), http.StatusInternalServerError)
			return
		}
		created, _ := getNotificationRuleByID(id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}
}

func UpdateNotificationRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseIDFromPath(r.URL.Path, "/api/notification-rules/")
		if err != nil {
			http.Error(w, jsonError("invalid rule id"), http.StatusBadRequest)
			return
		}
		existing, err := getNotificationRuleByID(id)
		if err != nil || existing == nil {
			http.Error(w, jsonError("notification rule not found"), http.StatusNotFound)
			return
		}
		var body notificationRulePayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		rule, err := validateNotificationRulePayload(body)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE notification_rules
			SET name = ?, description = ?, event_type = ?, severity = ?, entity_type = ?, connection_id = ?, actor_user_id = ?, title_template = ?, message_template = ?, target_id = ?, is_active = ?, updated_at = ?
			WHERE id = ?
		`), rule.Name, rule.Description, rule.EventType, rule.Severity, rule.EntityType, rule.ConnectionID, rule.ActorUserID, rule.TitleTemplate, rule.MessageTemplate, rule.TargetID, boolToInt(rule.IsActive), now, id)
		if err != nil {
			http.Error(w, jsonError("failed to update notification rule"), http.StatusInternalServerError)
			return
		}
		updated, _ := getNotificationRuleByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func DeleteNotificationRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDFromPath(r.URL.Path, "/api/notification-rules/")
		if err != nil {
			http.Error(w, jsonError("invalid rule id"), http.StatusBadRequest)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM notification_rules WHERE id = ?`), id)
		if err != nil {
			http.Error(w, jsonError("failed to delete notification rule"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ListNotificationDeliveries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT
				d.id, d.event_id, d.target_id, COALESCE(t.name, ''), d.channel, d.status, d.attempts,
				COALESCE(d.last_error, ''), d.last_response_code, d.payload_json,
				d.next_attempt_at, d.last_attempt_at, d.created_at, d.updated_at
			FROM notification_deliveries d
			LEFT JOIN notification_targets t ON t.id = d.target_id
			ORDER BY d.id DESC
			LIMIT ?
		`), limit)
		if err != nil {
			http.Error(w, jsonError("failed to list notification deliveries"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := []NotificationDelivery{}
		for rows.Next() {
			var item NotificationDelivery
			var payloadJSON string
			var nextAttemptAt, lastAttemptAt, createdAt, updatedAt any
			if err := rows.Scan(
				&item.ID, &item.EventID, &item.TargetID, &item.TargetName, &item.Channel, &item.Status, &item.Attempts,
				&item.LastError, &item.LastResponseCode, &payloadJSON,
				&nextAttemptAt, &lastAttemptAt, &createdAt, &updatedAt,
			); err != nil {
				http.Error(w, jsonError("failed to read notification deliveries"), http.StatusInternalServerError)
				return
			}
			item.NextAttemptAt = dbValueString(nextAttemptAt)
			item.LastAttemptAt = dbValueString(lastAttemptAt)
			item.CreatedAt = dbValueString(createdAt)
			item.UpdatedAt = dbValueString(updatedAt)
			item.Payload = parseJSONMapSafe(payloadJSON)
			item.Event = notificationEventFromDeliveryPayload(item.Payload, item.CreatedAt)
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, jsonError("failed to read notification deliveries"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(items)
	}
}

func notificationEventFromDeliveryPayload(payload map[string]any, fallbackCreatedAt string) NotificationEvent {
	event := NotificationEvent{
		EventType:  strings.TrimSpace(fmt.Sprintf("%v", payload["event_type"])),
		Severity:   normalizeNotificationSeverity(fmt.Sprintf("%v", payload["severity"])),
		Title:      strings.TrimSpace(fmt.Sprintf("%v", payload["title"])),
		Message:    strings.TrimSpace(fmt.Sprintf("%v", payload["message"])),
		EntityType: strings.TrimSpace(fmt.Sprintf("%v", payload["entity_type"])),
		CreatedAt:  fallbackCreatedAt,
	}
	if value, ok := payload["entity_id"]; ok {
		event.EntityID = parseAnyInt64(value)
	}
	if value, ok := payload["connection_id"]; ok {
		event.ConnectionID = parseAnyInt64(value)
	}
	if value, ok := payload["actor_user_id"]; ok {
		event.ActorUserID = parseAnyInt64(value)
	}
	if renderedTitle := strings.TrimSpace(fmt.Sprintf("%v", payload["rendered_title"])); renderedTitle != "" {
		event.Title = renderedTitle
	}
	if renderedMessage := strings.TrimSpace(fmt.Sprintf("%v", payload["rendered_message"])); renderedMessage != "" {
		event.Message = renderedMessage
	}
	if nestedPayload, ok := payload["payload"].(map[string]any); ok {
		event.Payload = nestedPayload
	} else {
		event.Payload = map[string]any{}
	}
	return event
}

func parseAnyInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func dbValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case time.Time:
		return v.UTC().Format("2006-01-02 15:04:05")
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func normalizeNotificationSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "success":
		return "success"
	case "warning":
		return "warning"
	case "error":
		return "error"
	default:
		return "info"
	}
}

func queueNotificationDeliveries(eventID int64, eventType, severity string, connectionID int64, payloadJSON, now string) {
	event, err := getNotificationEventByID(eventID)
	if err != nil || event == nil {
		return
	}
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, target_id, title_template, message_template
		FROM notification_rules
		WHERE is_active = 1
		  AND (event_type = '*' OR event_type = ?)
		  AND (severity = '' OR severity = ?)
		  AND (connection_id = 0 OR connection_id = ?)
		  AND (entity_type = '' OR entity_type = (SELECT entity_type FROM notification_events WHERE id = ?))
		  AND (actor_user_id = 0 OR actor_user_id = (SELECT actor_user_id FROM notification_events WHERE id = ?))
	`), eventType, severity, connectionID, eventID, eventID)
	if err != nil {
		return
	}
	defer rows.Close()
	seenTargets := map[int64]bool{}
	for rows.Next() {
		var ruleID, targetID int64
		var titleTemplate, messageTemplate string
		if err := rows.Scan(&ruleID, &targetID, &titleTemplate, &messageTemplate); err != nil {
			continue
		}
		if targetID <= 0 || seenTargets[targetID] {
			continue
		}
		seenTargets[targetID] = true
		target, err := getNotificationTargetRowByID(targetID)
		if err != nil || target == nil || !target.IsActive {
			continue
		}
		renderedTitle := renderNotificationTemplate(titleTemplate, *event, parseJSONMapSafe(payloadJSON))
		renderedMessage := renderNotificationTemplate(messageTemplate, *event, parseJSONMapSafe(payloadJSON))
		if strings.TrimSpace(renderedTitle) == "" {
			renderedTitle = event.Title
		}
		if strings.TrimSpace(renderedMessage) == "" {
			renderedMessage = event.Message
		}
		deliveryPayloadJSON, _ := json.Marshal(map[string]any{
			"event_type":       event.EventType,
			"severity":         event.Severity,
			"entity_type":      event.EntityType,
			"entity_id":        event.EntityID,
			"connection_id":    event.ConnectionID,
			"actor_user_id":    event.ActorUserID,
			"title":            event.Title,
			"message":          event.Message,
			"rendered_title":   renderedTitle,
			"rendered_message": renderedMessage,
			"payload":          event.Payload,
			"rule_id":          ruleID,
			"target_type":      target.Type,
		})
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO notification_deliveries
				(event_id, target_id, channel, status, attempts, payload_json, next_attempt_at, created_at, updated_at)
			VALUES (?, ?, ?, 'pending', 0, ?, ?, ?, ?)
		`), eventID, targetID, target.Type, string(deliveryPayloadJSON), now, now, now)
	}
}

func processPendingNotificationDeliveries() {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT d.id, d.event_id, d.target_id, d.payload_json,
		       e.event_type, e.category, e.severity, e.title, e.message, e.entity_type, e.entity_id, e.connection_id, e.actor_user_id, e.payload, e.created_at,
		       t.name, t.type, t.description, t.config_json, t.secret_enc, t.is_active, t.created_by, t.created_at, t.updated_at
		FROM notification_deliveries d
		JOIN notification_events e ON e.id = d.event_id
		JOIN notification_targets t ON t.id = d.target_id
		WHERE d.status IN ('pending', 'retrying')
		  AND d.next_attempt_at <= ?
		  AND t.is_active = 1
		ORDER BY d.id ASC
		LIMIT 25
	`), time.Now().UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var deliveryID int64
		var payloadJSON, eventPayloadJSON string
		var event NotificationEvent
		var target notificationTargetRow
		var active int
		if err := rows.Scan(
			&deliveryID, &event.ID, &target.ID, &payloadJSON,
			&event.EventType, &event.Category, &event.Severity, &event.Title, &event.Message, &event.EntityType, &event.EntityID, &event.ConnectionID, &event.ActorUserID, &eventPayloadJSON, &event.CreatedAt,
			&target.Name, &target.Type, &target.Description, &target.ConfigJSON, &target.SecretEnc, &active, &target.CreatedBy, &target.CreatedAt, &target.UpdatedAt,
		); err != nil {
			continue
		}
		target.IsActive = active == 1
		event.Payload = parseJSONMapSafe(eventPayloadJSON)

		lockKey := fmt.Sprintf("notification:delivery:%d", deliveryID)
		lockOwner := fmt.Sprintf("%s:%d", notificationInstanceID, time.Now().UTC().UnixNano())
		lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
		locked, lockErr := cache.Default().AcquireLock(lockCtx, lockKey, lockOwner, 2*time.Minute)
		lockCancel()
		if lockErr != nil || !locked {
			continue
		}

		lastAttempt := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE notification_deliveries
			SET status = 'sending', attempts = attempts + 1, last_attempt_at = ?, updated_at = ?
			WHERE id = ?
		`), lastAttempt, lastAttempt, deliveryID)

		statusCode, sendErr := sendNotificationToTarget(target, event, []byte(payloadJSON))
		if sendErr != nil {
			attempts := currentDeliveryAttempts(deliveryID)
			nextAttemptAt := nextNotificationAttempt(attempts)
			status := "retrying"
			if attempts >= 5 {
				status = "failed"
				nextAttemptAt = ""
			}
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
				UPDATE notification_deliveries
				SET status = ?, last_error = ?, last_response_code = ?, next_attempt_at = ?, updated_at = ?
				WHERE id = ?
			`), status, truncateNotificationError(sendErr.Error()), statusCode, nextAttemptAt, lastAttempt, deliveryID)
			releaseNotificationDeliveryLock(lockKey, lockOwner)
			continue
		}
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE notification_deliveries
			SET status = 'delivered', last_error = '', last_response_code = ?, next_attempt_at = ?, updated_at = ?
			WHERE id = ?
		`), statusCode, lastAttempt, lastAttempt, deliveryID)
		releaseNotificationDeliveryLock(lockKey, lockOwner)
	}
}

func processNotificationWorkerTick() {
	lockKey := "notification:worker:tick"
	lockOwner := fmt.Sprintf("%s:%d", notificationInstanceID, time.Now().UTC().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	locked, err := cache.Default().AcquireLock(ctx, lockKey, lockOwner, 14*time.Second)
	cancel()
	if err != nil || !locked {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer releaseCancel()
		_ = cache.Default().ReleaseLock(releaseCtx, lockKey, lockOwner)
	}()

	emitOverdueNotificationEvents()
	processPendingNotificationDeliveries()
}

func releaseNotificationDeliveryLock(lockKey, owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cache.Default().ReleaseLock(ctx, lockKey, owner)
}

type notificationTargetRow struct {
	ID                 int64
	Name               string
	Type               string
	Description        string
	ConfigJSON         string
	SecretEnc          string
	SecondarySecretEnc string
	IsActive           bool
	CreatedBy          int64
	CreatedAt          string
	UpdatedAt          string
}

func getNotificationTargetRowByID(id int64) (*notificationTargetRow, error) {
	var row notificationTargetRow
	var active int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, type, description, config_json, secret_enc, secondary_secret_enc, is_active, created_by, created_at, updated_at
		FROM notification_targets
		WHERE id = ?
	`), id).Scan(&row.ID, &row.Name, &row.Type, &row.Description, &row.ConfigJSON, &row.SecretEnc, &row.SecondarySecretEnc, &active, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.IsActive = active == 1
	return &row, nil
}

func getNotificationTargetByID(id int64) (*NotificationTarget, error) {
	row, err := getNotificationTargetRowByID(id)
	if err != nil || row == nil {
		return nil, err
	}
	return &NotificationTarget{
		ID:                 row.ID,
		Name:               row.Name,
		Type:               row.Type,
		Description:        row.Description,
		Config:             parseJSONMapSafe(row.ConfigJSON),
		IsActive:           row.IsActive,
		CreatedBy:          row.CreatedBy,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		HasSecret:          strings.TrimSpace(row.SecretEnc) != "",
		HasSecondarySecret: strings.TrimSpace(row.SecondarySecretEnc) != "",
	}, nil
}

func getNotificationRuleByID(id int64) (*NotificationRule, error) {
	var item NotificationRule
	var active int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, description, event_type, severity, entity_type, connection_id, actor_user_id, title_template, message_template, target_id, is_active, created_by, created_at, updated_at
		FROM notification_rules
		WHERE id = ?
	`), id).Scan(&item.ID, &item.Name, &item.Description, &item.EventType, &item.Severity, &item.EntityType, &item.ConnectionID, &item.ActorUserID, &item.TitleTemplate, &item.MessageTemplate, &item.TargetID, &active, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.IsActive = active == 1
	return &item, nil
}

func sendNotificationToTarget(target notificationTargetRow, event NotificationEvent, payloadJSON []byte) (int, error) {
	secret, err := decryptCredential(target.SecretEnc)
	if err != nil {
		return 0, fmt.Errorf("decrypt target secret: %w", err)
	}
	secondarySecret, err := decryptCredential(target.SecondarySecretEnc)
	if err != nil {
		return 0, fmt.Errorf("decrypt target secondary secret: %w", err)
	}
	config := parseJSONMapSafe(target.ConfigJSON)
	renderedPayload := parseJSONMapSafe(string(payloadJSON))
	client := &http.Client{Timeout: 10 * time.Second}
	title := strings.TrimSpace(fmt.Sprintf("%v", renderedPayload["rendered_title"]))
	message := strings.TrimSpace(fmt.Sprintf("%v", renderedPayload["rendered_message"]))
	if title == "" {
		title = event.Title
	}
	if message == "" {
		message = event.Message
	}

	switch target.Type {
	case "slack":
		if strings.TrimSpace(secret) == "" {
			return 0, fmt.Errorf("%s webhook URL is required", target.Type)
		}
		body, _ := json.Marshal(buildSlackPayload(event, renderedPayload, title, message, config))
		return doNotificationPost(client, secret, body, nil)
	case "discord":
		if strings.TrimSpace(secret) == "" {
			return 0, fmt.Errorf("%s webhook URL is required", target.Type)
		}
		body, _ := json.Marshal(buildDiscordPayload(event, renderedPayload, title, message, config))
		return doNotificationPost(client, secret, body, nil)
	case "webhook":
		if strings.TrimSpace(secret) == "" {
			return 0, fmt.Errorf("webhook URL is required")
		}
		headers := configStringMap(config["headers"])
		addWebhookSignatureHeaders(headers, payloadJSON, secondarySecret, config)
		return doNotificationPost(client, secret, payloadJSON, headers)
	case "telegram":
		chatID := strings.TrimSpace(fmt.Sprintf("%v", config["chat_id"]))
		if strings.TrimSpace(secret) == "" || chatID == "" {
			return 0, fmt.Errorf("telegram bot token and chat_id are required")
		}
		body, _ := json.Marshal(map[string]any{
			"chat_id":              chatID,
			"text":                 fmt.Sprintf("*%s*\n%s", escapeTelegramMarkdown(title), escapeTelegramMarkdown(message)),
			"parse_mode":           "MarkdownV2",
			"disable_notification": configBool(config["disable_notification"]),
		})
		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", secret)
		return doNotificationPost(client, url, body, nil)
	default:
		return 0, fmt.Errorf("unsupported notification target type: %s", target.Type)
	}
}

func buildSlackPayload(event NotificationEvent, renderedPayload map[string]any, title, message string, config map[string]any) map[string]any {
	fields := []map[string]string{}
	for _, pair := range notificationDetailPairs(event, renderedPayload) {
		fields = append(fields, map[string]string{
			"type": "mrkdwn",
			"text": fmt.Sprintf("*%s*\n%s", pair[0], pair[1]),
		})
	}
	payload := map[string]any{
		"text": fmt.Sprintf("%s\n%s", title, message),
		"attachments": []map[string]any{{
			"color": notificationSeverityColor(event.Severity),
			"blocks": []map[string]any{
				{
					"type": "header",
					"text": map[string]string{"type": "plain_text", "text": truncateNotificationText(title, 150)},
				},
				{
					"type": "section",
					"text": map[string]string{"type": "mrkdwn", "text": truncateNotificationText(message, 2800)},
				},
				{
					"type":   "section",
					"fields": fields,
				},
			},
		}},
	}
	if username := strings.TrimSpace(fmt.Sprintf("%v", config["username"])); username != "" {
		payload["username"] = username
	}
	if channel := strings.TrimSpace(fmt.Sprintf("%v", config["channel"])); channel != "" {
		payload["channel"] = channel
	}
	if iconEmoji := strings.TrimSpace(fmt.Sprintf("%v", config["icon_emoji"])); iconEmoji != "" {
		payload["icon_emoji"] = iconEmoji
	}
	if iconURL := strings.TrimSpace(fmt.Sprintf("%v", config["icon_url"])); iconURL != "" {
		payload["icon_url"] = iconURL
	}
	return payload
}

func buildDiscordPayload(event NotificationEvent, renderedPayload map[string]any, title, message string, config map[string]any) map[string]any {
	fields := []map[string]any{}
	for _, pair := range notificationDetailPairs(event, renderedPayload) {
		fields = append(fields, map[string]any{
			"name":   pair[0],
			"value":  truncateNotificationText(pair[1], 1024),
			"inline": true,
		})
	}
	payload := map[string]any{
		"content": "",
		"embeds": []map[string]any{{
			"title":       truncateNotificationText(title, 256),
			"description": truncateNotificationText(message, 4096),
			"color":       notificationSeverityColorInt(event.Severity),
			"fields":      fields,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		}},
	}
	if username := strings.TrimSpace(fmt.Sprintf("%v", config["username"])); username != "" {
		payload["username"] = username
	}
	if avatarURL := strings.TrimSpace(fmt.Sprintf("%v", config["avatar_url"])); avatarURL != "" {
		payload["avatar_url"] = avatarURL
	}
	if embeds, ok := payload["embeds"].([]map[string]any); ok && len(embeds) > 0 {
		if footerText := strings.TrimSpace(fmt.Sprintf("%v", config["footer_text"])); footerText != "" {
			embeds[0]["footer"] = map[string]any{"text": footerText}
		}
		if authorName := strings.TrimSpace(fmt.Sprintf("%v", config["author_name"])); authorName != "" {
			embeds[0]["author"] = map[string]any{"name": authorName}
		}
	}
	return payload
}

func notificationDetailPairs(event NotificationEvent, renderedPayload map[string]any) [][2]string {
	payloadFields := map[string]any{}
	if raw, ok := renderedPayload["payload"].(map[string]any); ok {
		payloadFields = raw
	}
	pairs := [][2]string{
		{"Event", fallbackNotificationValue(event.EventType, renderedPayload["event_type"])},
		{"Severity", fallbackNotificationValue(event.Severity, renderedPayload["severity"])},
		{"Entity", fmt.Sprintf("%s #%d", fallbackNotificationValue(event.EntityType, renderedPayload["entity_type"]), event.EntityID)},
	}
	if event.ConnectionID > 0 {
		pairs = append(pairs, [2]string{"Connection", strconv.FormatInt(event.ConnectionID, 10)})
	}
	if event.ActorUserID > 0 {
		pairs = append(pairs, [2]string{"Actor User", strconv.FormatInt(event.ActorUserID, 10)})
	}
	if status := strings.TrimSpace(fmt.Sprintf("%v", payloadFields["status"])); status != "" {
		pairs = append(pairs, [2]string{"Status", status})
	}
	if note := strings.TrimSpace(fmt.Sprintf("%v", payloadFields["note"])); note != "" {
		pairs = append(pairs, [2]string{"Note", note})
	}
	return pairs
}

func fallbackNotificationValue(primary string, secondary any) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", secondary))
}

func notificationSeverityColor(severity string) string {
	switch normalizeNotificationSeverity(severity) {
	case "success":
		return "#27ae60"
	case "warning":
		return "#f39c12"
	case "error":
		return "#e74c3c"
	default:
		return "#4f9cf9"
	}
}

func notificationSeverityColorInt(severity string) int {
	switch normalizeNotificationSeverity(severity) {
	case "success":
		return 0x27ae60
	case "warning":
		return 0xf39c12
	case "error":
		return 0xe74c3c
	default:
		return 0x4f9cf9
	}
}

func truncateNotificationText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func escapeTelegramMarkdown(value string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(value)
}

func doNotificationPost(client *http.Client, endpoint string, body []byte, headers map[string]string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("remote endpoint returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func validateNotificationTargetPayload(body notificationTargetPayload, requireSecret bool) (NotificationTarget, string, string, error) {
	target := NotificationTarget{
		Name:        strings.TrimSpace(body.Name),
		Type:        strings.ToLower(strings.TrimSpace(body.Type)),
		Description: strings.TrimSpace(body.Description),
		Config:      body.Config,
		IsActive:    body.IsActive == nil || *body.IsActive,
	}
	if target.Name == "" {
		return target, "", "", fmt.Errorf("name is required")
	}
	switch target.Type {
	case "webhook", "slack", "discord", "telegram":
	default:
		return target, "", "", fmt.Errorf("type must be webhook, slack, discord, or telegram")
	}
	if target.Config == nil {
		target.Config = map[string]any{}
	}
	if target.Type == "telegram" {
		if strings.TrimSpace(fmt.Sprintf("%v", target.Config["chat_id"])) == "" {
			return target, "", "", fmt.Errorf("chat_id is required for telegram")
		}
	}
	secret := strings.TrimSpace(body.Secret)
	if requireSecret && secret == "" {
		return target, "", "", fmt.Errorf("secret is required")
	}
	secretEnc := ""
	if secret != "" {
		var err error
		secretEnc, err = encryptCredential(secret)
		if err != nil {
			return target, "", "", fmt.Errorf("failed to encrypt secret")
		}
	}
	secondarySecretEnc := ""
	if secondarySecret := strings.TrimSpace(body.SecondarySecret); secondarySecret != "" {
		var err error
		secondarySecretEnc, err = encryptCredential(secondarySecret)
		if err != nil {
			return target, "", "", fmt.Errorf("failed to encrypt secondary secret")
		}
	}
	return target, secretEnc, secondarySecretEnc, nil
}

func validateNotificationRulePayload(body notificationRulePayload) (NotificationRule, error) {
	rule := NotificationRule{
		Name:            strings.TrimSpace(body.Name),
		Description:     strings.TrimSpace(body.Description),
		EventType:       strings.TrimSpace(body.EventType),
		Severity:        strings.TrimSpace(strings.ToLower(body.Severity)),
		EntityType:      strings.TrimSpace(body.EntityType),
		ConnectionID:    body.ConnectionID,
		ActorUserID:     body.ActorUserID,
		TitleTemplate:   strings.TrimSpace(body.TitleTemplate),
		MessageTemplate: strings.TrimSpace(body.MessageTemplate),
		TargetID:        body.TargetID,
		IsActive:        body.IsActive == nil || *body.IsActive,
	}
	if rule.Name == "" {
		return rule, fmt.Errorf("name is required")
	}
	if rule.TargetID <= 0 {
		return rule, fmt.Errorf("target_id is required")
	}
	if rule.EventType == "" {
		rule.EventType = "*"
	}
	if rule.Severity != "" && normalizeNotificationSeverity(rule.Severity) != rule.Severity {
		return rule, fmt.Errorf("severity must be info, success, warning, error, or empty")
	}
	target, err := getNotificationTargetRowByID(rule.TargetID)
	if err != nil {
		return rule, fmt.Errorf("failed to validate target")
	}
	if target == nil {
		return rule, fmt.Errorf("notification target not found")
	}
	return rule, nil
}

func parseJSONMapSafe(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func configStringMap(v any) map[string]string {
	out := map[string]string{}
	switch headers := v.(type) {
	case map[string]any:
		for key, value := range headers {
			out[key] = fmt.Sprintf("%v", value)
		}
	case map[string]string:
		return headers
	}
	return out
}

func configBool(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true") || strings.TrimSpace(value) == "1"
	default:
		return false
	}
}

func addWebhookSignatureHeaders(headers map[string]string, body []byte, signingSecret string, config map[string]any) {
	if strings.TrimSpace(signingSecret) == "" {
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	headerName := strings.TrimSpace(fmt.Sprintf("%v", config["signing_header"]))
	if headerName == "" {
		headerName = "X-Nias-Signature-256"
	}
	headers[headerName] = signature
	headers["X-Nias-Timestamp"] = timestamp
}

func scanNotificationEvents(rows *sql.Rows) ([]NotificationEvent, error) {
	items := []NotificationEvent{}
	for rows.Next() {
		var item NotificationEvent
		var payloadJSON string
		if err := rows.Scan(&item.ID, &item.EventType, &item.Category, &item.Severity, &item.Title, &item.Message, &item.EntityType, &item.EntityID, &item.ConnectionID, &item.ActorUserID, &payloadJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Payload = parseJSONMapSafe(payloadJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanNotificationTargets(rows *sql.Rows) ([]NotificationTarget, error) {
	items := []NotificationTarget{}
	for rows.Next() {
		var item NotificationTarget
		var configJSON, secretEnc, secondarySecretEnc string
		var active int
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Description, &configJSON, &secretEnc, &secondarySecretEnc, &active, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Config = parseJSONMapSafe(configJSON)
		item.IsActive = active == 1
		item.HasSecret = strings.TrimSpace(secretEnc) != ""
		item.HasSecondarySecret = strings.TrimSpace(secondarySecretEnc) != ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanNotificationRules(rows *sql.Rows) ([]NotificationRule, error) {
	items := []NotificationRule{}
	for rows.Next() {
		var item NotificationRule
		var active int
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.EventType, &item.Severity, &item.EntityType, &item.ConnectionID, &item.ActorUserID, &item.TitleTemplate, &item.MessageTemplate, &item.TargetID, &active, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IsActive = active == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func parseIDFromPath(path, prefix string) (int64, error) {
	raw := strings.TrimPrefix(path, prefix)
	raw = strings.Trim(raw, "/")
	return strconv.ParseInt(raw, 10, 64)
}

func parsePathActionID(path, prefix, suffix string) (int64, error) {
	raw := strings.TrimPrefix(path, prefix)
	raw = strings.TrimSuffix(raw, suffix)
	raw = strings.Trim(raw, "/")
	return strconv.ParseInt(raw, 10, 64)
}

func insertRowReturningID(query string, args ...any) (int64, error) {
	if appdb.IsPostgreSQL() || appdb.IsMySQL() {
		query = strings.TrimSpace(query)
		if !strings.Contains(strings.ToUpper(query), "RETURNING ID") {
			query += " RETURNING id"
		}
		var id int64
		if err := appdb.DB.QueryRow(query, args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := appdb.DB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func dedupeUserIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func nextNotificationAttempt(attempts int) string {
	backoff := time.Minute
	switch {
	case attempts >= 4:
		backoff = 30 * time.Minute
	case attempts == 3:
		backoff = 15 * time.Minute
	case attempts == 2:
		backoff = 5 * time.Minute
	}
	return time.Now().UTC().Add(backoff).Format("2006-01-02 15:04:05")
}

func currentDeliveryAttempts(deliveryID int64) int {
	var attempts int
	_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(attempts, 0) FROM notification_deliveries WHERE id = ?`), deliveryID).Scan(&attempts)
	return attempts
}

func truncateNotificationError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 400 {
		return msg[:400]
	}
	return msg
}

func usernameOrFallback(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "Someone"
}

func getNotificationEventByID(id int64) (*NotificationEvent, error) {
	var item NotificationEvent
	var payloadJSON string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, event_type, category, severity, title, message, entity_type, entity_id, connection_id, actor_user_id, payload, created_at
		FROM notification_events
		WHERE id = ?
	`), id).Scan(&item.ID, &item.EventType, &item.Category, &item.Severity, &item.Title, &item.Message, &item.EntityType, &item.EntityID, &item.ConnectionID, &item.ActorUserID, &payloadJSON, &item.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Payload = parseJSONMapSafe(payloadJSON)
	return &item, nil
}

func renderNotificationTemplate(tmpl string, event NotificationEvent, rawPayload map[string]any) string {
	out := strings.TrimSpace(tmpl)
	if out == "" {
		return ""
	}
	values := map[string]string{
		"event_type":    event.EventType,
		"category":      event.Category,
		"severity":      event.Severity,
		"title":         event.Title,
		"message":       event.Message,
		"entity_type":   event.EntityType,
		"entity_id":     strconv.FormatInt(event.EntityID, 10),
		"connection_id": strconv.FormatInt(event.ConnectionID, 10),
		"actor_user_id": strconv.FormatInt(event.ActorUserID, 10),
		"created_at":    event.CreatedAt,
	}
	for key, value := range rawPayload {
		if nested, ok := value.(map[string]any); ok && key == "payload" {
			for nestedKey, nestedValue := range nested {
				values["payload."+nestedKey] = fmt.Sprintf("%v", nestedValue)
			}
			continue
		}
		values[key] = fmt.Sprintf("%v", value)
	}
	for key, value := range event.Payload {
		values["payload."+key] = fmt.Sprintf("%v", value)
	}
	for key, value := range values {
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}

func emitOverdueNotificationEvents() {
	cutoff := time.Now().UTC().Add(-6 * time.Hour)
	emitOverdueApprovalRequests(cutoff)
	emitOverdueDataChangePlans(cutoff)
	emitOverdueBackupRequests(cutoff)
}

func emitOverdueApprovalRequests(cutoff time.Time) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, title, conn_id, creator_id, workflow_id, current_step
		FROM query_approval_request
		WHERE status = 'pending_review' AND updated_at <= ?
	`), cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, connID, creatorID, workflowID int64
		var currentStep int
		var title string
		if rows.Scan(&id, &title, &connID, &creatorID, &workflowID, &currentStep) != nil {
			continue
		}
		if notificationEventExistsSince("approval_request.overdue", "approval_request", id, cutoff) {
			continue
		}
		targetUserIDs := []int64{creatorID}
		if step, _ := getStepByWorkflowAndOrder(workflowID, currentStep); step != nil {
			targetUserIDs = append(targetUserIDs, getWorkflowApproverUserIDs(step.ID)...)
		}
		EmitNotification(NotificationEventInput{
			EventType:     "approval_request.overdue",
			Category:      "approval",
			Severity:      "warning",
			Title:         "Approval request overdue",
			Message:       fmt.Sprintf("Approval request \"%s\" is still waiting for review", title),
			EntityType:    "approval_request",
			EntityID:      id,
			ConnectionID:  connID,
			TargetUserIDs: targetUserIDs,
			Payload:       map[string]any{"status": "pending_review", "overdue_hours": 6},
		})
	}
}

func emitOverdueDataChangePlans(cutoff time.Time) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, conn_id, creator_id, workflow_id, current_step
		FROM data_change_plans
		WHERE status = 'pending_review' AND updated_at <= ?
	`), cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, connID, creatorID, workflowID int64
		var currentStep int
		if rows.Scan(&id, &connID, &creatorID, &workflowID, &currentStep) != nil {
			continue
		}
		if notificationEventExistsSince("data_script.overdue", "data_change_plan", id, cutoff) {
			continue
		}
		targetUserIDs := []int64{creatorID}
		if step, _ := getStepByWorkflowAndOrder(workflowID, currentStep); step != nil {
			targetUserIDs = append(targetUserIDs, getWorkflowApproverUserIDs(step.ID)...)
		}
		EmitNotification(NotificationEventInput{
			EventType:     "data_script.overdue",
			Category:      "data_script",
			Severity:      "warning",
			Title:         "Data script request overdue",
			Message:       fmt.Sprintf("Data script plan #%d is still waiting for review", id),
			EntityType:    "data_change_plan",
			EntityID:      id,
			ConnectionID:  connID,
			TargetUserIDs: targetUserIDs,
			Payload:       map[string]any{"status": "pending_review", "overdue_hours": 6},
		})
	}
}

func emitOverdueBackupRequests(cutoff time.Time) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, title, conn_id, creator_id, workflow_id, current_step
		FROM backup_download_requests
		WHERE status = 'pending_review' AND updated_at <= ?
	`), cutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, connID, creatorID, workflowID int64
		var currentStep int
		var title string
		if rows.Scan(&id, &title, &connID, &creatorID, &workflowID, &currentStep) != nil {
			continue
		}
		if notificationEventExistsSince("backup_request.overdue", "backup_download_request", id, cutoff) {
			continue
		}
		targetUserIDs := []int64{creatorID}
		if step, _ := getStepByWorkflowAndOrder(workflowID, currentStep); step != nil {
			targetUserIDs = append(targetUserIDs, getWorkflowApproverUserIDs(step.ID)...)
		}
		EmitNotification(NotificationEventInput{
			EventType:     "backup_request.overdue",
			Category:      "backup",
			Severity:      "warning",
			Title:         "Backup request overdue",
			Message:       fmt.Sprintf("Backup request \"%s\" is still waiting for review", title),
			EntityType:    "backup_download_request",
			EntityID:      id,
			ConnectionID:  connID,
			TargetUserIDs: targetUserIDs,
			Payload:       map[string]any{"status": "pending_review", "overdue_hours": 6},
		})
	}
}

func notificationEventExistsSince(eventType, entityType string, entityID int64, since time.Time) bool {
	var count int
	_ = appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT COUNT(*)
		FROM notification_events
		WHERE event_type = ? AND entity_type = ? AND entity_id = ? AND created_at >= ?
	`), eventType, entityType, entityID, since.Format("2006-01-02 15:04:05")).Scan(&count)
	return count > 0
}

func getWorkflowApproverUserIDs(stepID int64) []int64 {
	approvers, err := listStepApprovers(stepID)
	if err != nil {
		return nil
	}
	ids := []int64{}
	roleNames := []string{}
	for _, approver := range approvers {
		switch approver.ApproverType {
		case "user":
			ids = append(ids, approver.ApproverID)
		case "role":
			roleNames = append(roleNames, approver.ApproverName)
		}
	}
	if len(roleNames) > 0 {
		placeholders := make([]string, len(roleNames))
		args := make([]any, 0, len(roleNames))
		for i, roleName := range roleNames {
			placeholders[i] = "?"
			args = append(args, roleName)
		}
		query := appdb.ConvertQuery(fmt.Sprintf(`
			SELECT u.id
			FROM users u
			JOIN roles r ON r.id = u.role_id
			WHERE u.is_active = 1 AND r.name IN (%s)
		`, strings.Join(placeholders, ",")))
		rows, err := appdb.DB.Query(query, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
		}
	}
	return dedupeUserIDs(ids)
}
