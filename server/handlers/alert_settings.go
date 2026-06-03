package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// ── Public API types (used by HTTP layer) ─────────────────────────────────────

type AlertChannelConfig struct {
	Telegram AlertTelegramConfig `json:"telegram"`
	Discord  AlertDiscordConfig  `json:"discord"`
	Slack    AlertSlackConfig    `json:"slack"`
	Webhook  AlertWebhookConfig  `json:"webhook"`
}

type AlertTelegramConfig struct {
	Enabled bool                  `json:"enabled"`
	Targets []AlertTelegramTarget `json:"targets"`
}

type AlertTelegramTarget struct {
	Name                string `json:"name"`
	BotToken            string `json:"bot_token"`
	ChatID              string `json:"chat_id"`
	TopicID             string `json:"topic_id"`
	DisableNotification bool   `json:"disable_notification"`
}

type AlertDiscordConfig struct {
	Enabled bool                 `json:"enabled"`
	Targets []AlertDiscordTarget `json:"targets"`
}

type AlertDiscordTarget struct {
	Name       string `json:"name"`
	WebhookURL string `json:"webhook_url"`
	Username   string `json:"username"`
	AvatarURL  string `json:"avatar_url"`
	FooterText string `json:"footer_text"`
}

type AlertSlackConfig struct {
	Enabled bool               `json:"enabled"`
	Targets []AlertSlackTarget `json:"targets"`
}

type AlertSlackTarget struct {
	Name       string `json:"name"`
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel"`
	Username   string `json:"username"`
	IconEmoji  string `json:"icon_emoji"`
}

type AlertWebhookConfig struct {
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url"`
	AuthHeader string `json:"auth_header"`
}

// ── Stored types (persisted in settings table with encrypted secrets) ─────────

type alertTelegramStored struct {
	Enabled bool                        `json:"enabled"`
	Targets []alertTelegramTargetStored `json:"targets"`
}

type alertTelegramTargetStored struct {
	Name                string `json:"name"`
	BotTokenEnc         string `json:"bot_token_enc"`
	ChatID              string `json:"chat_id"`
	TopicID             string `json:"topic_id"`
	DisableNotification bool   `json:"disable_notification"`
}

type alertDiscordStored struct {
	Enabled bool                        `json:"enabled"`
	Targets []alertDiscordTargetStored  `json:"targets"`
}

type alertDiscordTargetStored struct {
	Name          string `json:"name"`
	WebhookURLEnc string `json:"webhook_url_enc"`
	Username      string `json:"username"`
	AvatarURL     string `json:"avatar_url"`
	FooterText    string `json:"footer_text"`
}

type alertSlackStored struct {
	Enabled bool                     `json:"enabled"`
	Targets []alertSlackTargetStored `json:"targets"`
}

type alertSlackTargetStored struct {
	Name          string `json:"name"`
	WebhookURLEnc string `json:"webhook_url_enc"`
	Channel       string `json:"channel"`
	Username      string `json:"username"`
	IconEmoji     string `json:"icon_emoji"`
}

type alertWebhookStored struct {
	Enabled       bool   `json:"enabled"`
	URLEnc        string `json:"url_enc"`
	AuthHeaderEnc string `json:"auth_header_enc"`
}

const alertMaskPlaceholder = "••••••••"

// ── Handlers ──────────────────────────────────────────────────────────────────

func GetAlertSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var tg alertTelegramStored
		loadAlertSetting("alert_telegram", &tg)
		var dc alertDiscordStored
		loadAlertSetting("alert_discord", &dc)
		var sl alertSlackStored
		loadAlertSetting("alert_slack", &sl)
		var wh alertWebhookStored
		loadAlertSetting("alert_webhook", &wh)

		tgTargets := make([]AlertTelegramTarget, 0, len(tg.Targets))
		for _, t := range tg.Targets {
			tgTargets = append(tgTargets, AlertTelegramTarget{
				Name:                t.Name,
				BotToken:            alertMaskIfSet(t.BotTokenEnc),
				ChatID:              t.ChatID,
				TopicID:             t.TopicID,
				DisableNotification: t.DisableNotification,
			})
		}

		dcTargets := make([]AlertDiscordTarget, 0, len(dc.Targets))
		for _, t := range dc.Targets {
			dcTargets = append(dcTargets, AlertDiscordTarget{
				Name:       t.Name,
				WebhookURL: alertMaskIfSet(t.WebhookURLEnc),
				Username:   t.Username,
				AvatarURL:  t.AvatarURL,
				FooterText: t.FooterText,
			})
		}

		slTargets := make([]AlertSlackTarget, 0, len(sl.Targets))
		for _, t := range sl.Targets {
			slTargets = append(slTargets, AlertSlackTarget{
				Name:       t.Name,
				WebhookURL: alertMaskIfSet(t.WebhookURLEnc),
				Channel:    t.Channel,
				Username:   t.Username,
				IconEmoji:  t.IconEmoji,
			})
		}

		result := AlertChannelConfig{
			Telegram: AlertTelegramConfig{
				Enabled: tg.Enabled,
				Targets: tgTargets,
			},
			Discord: AlertDiscordConfig{
				Enabled: dc.Enabled,
				Targets: dcTargets,
			},
			Slack: AlertSlackConfig{
				Enabled: sl.Enabled,
				Targets: slTargets,
			},
			Webhook: AlertWebhookConfig{
				Enabled:    wh.Enabled,
				URL:        alertMaskIfSet(wh.URLEnc),
				AuthHeader: alertMaskIfSet(wh.AuthHeaderEnc),
			},
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SaveAlertSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AlertChannelConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := saveAlertTelegram(req.Telegram); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if err := saveAlertDiscord(req.Discord); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if err := saveAlertSlack(req.Slack); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if err := saveAlertWebhook(req.Webhook); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestAlertChannel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Channel     string `json:"channel"`
			TargetIndex int    `json:"target_index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}

		var err error
		targetLabel := ""
		switch req.Channel {
		case "telegram":
			err = testAlertTelegram(req.TargetIndex)
			var stored alertTelegramStored
			loadAlertSetting("alert_telegram", &stored)
			if req.TargetIndex >= 0 && req.TargetIndex < len(stored.Targets) {
				targetLabel = stored.Targets[req.TargetIndex].Name
			}
		case "discord":
			err = testAlertDiscord(req.TargetIndex)
			var stored alertDiscordStored
			loadAlertSetting("alert_discord", &stored)
			if req.TargetIndex >= 0 && req.TargetIndex < len(stored.Targets) {
				targetLabel = stored.Targets[req.TargetIndex].Name
			}
		case "slack":
			err = testAlertSlack(req.TargetIndex)
			var stored alertSlackStored
			loadAlertSetting("alert_slack", &stored)
			if req.TargetIndex >= 0 && req.TargetIndex < len(stored.Targets) {
				targetLabel = stored.Targets[req.TargetIndex].Name
			}
		case "webhook":
			err = testAlertWebhook()
		default:
			http.Error(w, `{"error":"unknown channel"}`, http.StatusBadRequest)
			return
		}
		if err != nil {
			LogAlertDelivery(req.Channel, targetLabel, "error", err.Error(), "test")
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		LogAlertDelivery(req.Channel, targetLabel, "ok", "", "test")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// ReadAlertSettings returns decrypted configs for internal use (scheduler, infra alerts, etc.).
func ReadAlertSettings() AlertChannelConfig {
	var tg alertTelegramStored
	loadAlertSetting("alert_telegram", &tg)
	var dc alertDiscordStored
	loadAlertSetting("alert_discord", &dc)
	var sl alertSlackStored
	loadAlertSetting("alert_slack", &sl)
	var wh alertWebhookStored
	loadAlertSetting("alert_webhook", &wh)

	tgTargets := make([]AlertTelegramTarget, 0, len(tg.Targets))
	for _, t := range tg.Targets {
		botToken, _ := decryptCredential(t.BotTokenEnc)
		tgTargets = append(tgTargets, AlertTelegramTarget{
			Name:                t.Name,
			BotToken:            botToken,
			ChatID:              t.ChatID,
			TopicID:             t.TopicID,
			DisableNotification: t.DisableNotification,
		})
	}

	dcTargets := make([]AlertDiscordTarget, 0, len(dc.Targets))
	for _, t := range dc.Targets {
		u, _ := decryptCredential(t.WebhookURLEnc)
		dcTargets = append(dcTargets, AlertDiscordTarget{
			Name: t.Name, WebhookURL: u,
			Username: t.Username, AvatarURL: t.AvatarURL, FooterText: t.FooterText,
		})
	}

	slTargets := make([]AlertSlackTarget, 0, len(sl.Targets))
	for _, t := range sl.Targets {
		u, _ := decryptCredential(t.WebhookURLEnc)
		slTargets = append(slTargets, AlertSlackTarget{
			Name: t.Name, WebhookURL: u,
			Channel: t.Channel, Username: t.Username, IconEmoji: t.IconEmoji,
		})
	}

	webhookURL, _ := decryptCredential(wh.URLEnc)
	authHeader, _ := decryptCredential(wh.AuthHeaderEnc)

	return AlertChannelConfig{
		Telegram: AlertTelegramConfig{Enabled: tg.Enabled, Targets: tgTargets},
		Discord:  AlertDiscordConfig{Enabled: dc.Enabled, Targets: dcTargets},
		Slack:    AlertSlackConfig{Enabled: sl.Enabled, Targets: slTargets},
		Webhook:  AlertWebhookConfig{Enabled: wh.Enabled, URL: webhookURL, AuthHeader: authHeader},
	}
}

// ── Save helpers ──────────────────────────────────────────────────────────────

func saveAlertTelegram(cfg AlertTelegramConfig) error {
	var current alertTelegramStored
	loadAlertSetting("alert_telegram", &current)

	targets := make([]alertTelegramTargetStored, 0, len(cfg.Targets))
	for i, t := range cfg.Targets {
		botTokenEnc := ""
		if i < len(current.Targets) {
			botTokenEnc = current.Targets[i].BotTokenEnc
		}
		if token := strings.TrimSpace(t.BotToken); token != "" && !strings.Contains(token, "•") {
			if enc, err := encryptCredential(token); err == nil {
				botTokenEnc = enc
			}
			// if encryption fails (e.g. key not configured), store plain text as fallback
			// decryptCredential handles plain (non-enc: prefixed) values transparently
			if botTokenEnc == "" {
				botTokenEnc = strings.TrimSpace(t.BotToken)
			}
		}
		targets = append(targets, alertTelegramTargetStored{
			Name:                strings.TrimSpace(t.Name),
			BotTokenEnc:         botTokenEnc,
			ChatID:              strings.TrimSpace(t.ChatID),
			TopicID:             strings.TrimSpace(t.TopicID),
			DisableNotification: t.DisableNotification,
		})
	}

	return upsertAlertSetting("alert_telegram", alertTelegramStored{
		Enabled: cfg.Enabled,
		Targets: targets,
	})
}

func saveAlertDiscord(cfg AlertDiscordConfig) error {
	var current alertDiscordStored
	loadAlertSetting("alert_discord", &current)

	targets := make([]alertDiscordTargetStored, 0, len(cfg.Targets))
	for i, t := range cfg.Targets {
		webhookURLEnc := ""
		// preserve existing encrypted value if the field is masked
		if i < len(current.Targets) {
			webhookURLEnc = current.Targets[i].WebhookURLEnc
		}
		if u := strings.TrimSpace(t.WebhookURL); u != "" && !strings.Contains(u, "•") {
			if _, err := url.ParseRequestURI(u); err != nil {
				return fmt.Errorf("target %d: invalid Discord webhook URL", i+1)
			}
			if enc, err := encryptCredential(u); err == nil {
				webhookURLEnc = enc
			} else {
				webhookURLEnc = u
			}
		}
		targets = append(targets, alertDiscordTargetStored{
			Name:          strings.TrimSpace(t.Name),
			WebhookURLEnc: webhookURLEnc,
			Username:      strings.TrimSpace(t.Username),
			AvatarURL:     strings.TrimSpace(t.AvatarURL),
			FooterText:    strings.TrimSpace(t.FooterText),
		})
	}

	return upsertAlertSetting("alert_discord", alertDiscordStored{
		Enabled: cfg.Enabled,
		Targets: targets,
	})
}

func saveAlertSlack(cfg AlertSlackConfig) error {
	var current alertSlackStored
	loadAlertSetting("alert_slack", &current)

	targets := make([]alertSlackTargetStored, 0, len(cfg.Targets))
	for i, t := range cfg.Targets {
		webhookURLEnc := ""
		if i < len(current.Targets) {
			webhookURLEnc = current.Targets[i].WebhookURLEnc
		}
		if u := strings.TrimSpace(t.WebhookURL); u != "" && !strings.Contains(u, "•") {
			if _, err := url.ParseRequestURI(u); err != nil {
				return fmt.Errorf("target %d: invalid Slack webhook URL", i+1)
			}
			if enc, err := encryptCredential(u); err == nil {
				webhookURLEnc = enc
			} else {
				webhookURLEnc = u
			}
		}
		targets = append(targets, alertSlackTargetStored{
			Name:          strings.TrimSpace(t.Name),
			WebhookURLEnc: webhookURLEnc,
			Channel:       strings.TrimSpace(t.Channel),
			Username:      strings.TrimSpace(t.Username),
			IconEmoji:     strings.TrimSpace(t.IconEmoji),
		})
	}

	return upsertAlertSetting("alert_slack", alertSlackStored{
		Enabled: cfg.Enabled,
		Targets: targets,
	})
}

func saveAlertWebhook(cfg AlertWebhookConfig) error {
	var current alertWebhookStored
	loadAlertSetting("alert_webhook", &current)

	urlEnc := current.URLEnc
	authHeaderEnc := current.AuthHeaderEnc

	if u := strings.TrimSpace(cfg.URL); u != "" && !strings.Contains(u, "•") {
		if _, err := url.ParseRequestURI(u); err != nil {
			return fmt.Errorf("invalid webhook URL")
		}
		if enc, err := encryptCredential(u); err == nil {
			urlEnc = enc
		} else {
			urlEnc = u
		}
	}
	if h := strings.TrimSpace(cfg.AuthHeader); h != "" && !strings.Contains(h, "•") {
		if enc, err := encryptCredential(h); err == nil {
			authHeaderEnc = enc
		} else {
			authHeaderEnc = h
		}
	}

	return upsertAlertSetting("alert_webhook", alertWebhookStored{
		Enabled:       cfg.Enabled,
		URLEnc:        urlEnc,
		AuthHeaderEnc: authHeaderEnc,
	})
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func testAlertTelegram(targetIndex int) error {
	var stored alertTelegramStored
	loadAlertSetting("alert_telegram", &stored)
	if !stored.Enabled {
		return fmt.Errorf("Telegram alerts are not enabled")
	}
	if len(stored.Targets) == 0 {
		return fmt.Errorf("no Telegram targets configured")
	}
	if targetIndex < 0 || targetIndex >= len(stored.Targets) {
		return fmt.Errorf("target index out of range")
	}
	t := stored.Targets[targetIndex]
	botToken, err := decryptCredential(t.BotTokenEnc)
	if err != nil || strings.TrimSpace(botToken) == "" {
		return fmt.Errorf("target %q: bot token is not configured", t.Name)
	}
	chatID := strings.TrimSpace(t.ChatID)
	if chatID == "" {
		return fmt.Errorf("target %q: chat_id is not configured", t.Name)
	}

	payload := map[string]any{
		"chat_id":              chatID,
		"text":                 fmt.Sprintf("✅ *Anveesa Nias test alert* — Telegram target _%s_ is configured correctly\\.", escapeTelegramMarkdown(t.Name)),
		"parse_mode":           "MarkdownV2",
		"disable_notification": t.DisableNotification,
	}
	if topicID := strings.TrimSpace(t.TopicID); topicID != "" {
		payload["message_thread_id"] = topicID
	}
	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	client := &http.Client{Timeout: 10 * time.Second}
	_, err = doNotificationPost(client, endpoint, body, nil)
	return err
}

func testAlertDiscord(targetIndex int) error {
	var stored alertDiscordStored
	loadAlertSetting("alert_discord", &stored)
	if !stored.Enabled {
		return fmt.Errorf("Discord alerts are not enabled")
	}
	if len(stored.Targets) == 0 {
		return fmt.Errorf("no Discord targets configured")
	}
	if targetIndex < 0 || targetIndex >= len(stored.Targets) {
		return fmt.Errorf("target index out of range")
	}
	t := stored.Targets[targetIndex]
	webhookURL, err := decryptCredential(t.WebhookURLEnc)
	if err != nil || strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("target %q: webhook URL is not configured", t.Name)
	}

	payload := map[string]any{
		"content": fmt.Sprintf("✅ **Anveesa Nias test alert** — Discord target **%s** is configured correctly.", t.Name),
	}
	if t.Username != "" {
		payload["username"] = t.Username
	}
	if t.AvatarURL != "" {
		payload["avatar_url"] = t.AvatarURL
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	_, err = doNotificationPost(client, webhookURL, body, nil)
	return err
}

func testAlertSlack(targetIndex int) error {
	var stored alertSlackStored
	loadAlertSetting("alert_slack", &stored)
	if !stored.Enabled {
		return fmt.Errorf("Slack alerts are not enabled")
	}
	if len(stored.Targets) == 0 {
		return fmt.Errorf("no Slack targets configured")
	}
	if targetIndex < 0 || targetIndex >= len(stored.Targets) {
		return fmt.Errorf("target index out of range")
	}
	t := stored.Targets[targetIndex]
	webhookURL, err := decryptCredential(t.WebhookURLEnc)
	if err != nil || strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("target %q: webhook URL is not configured", t.Name)
	}

	payload := map[string]any{
		"text": fmt.Sprintf("✅ *Anveesa Nias test alert* — Slack target *%s* is configured correctly.", t.Name),
	}
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
	client := &http.Client{Timeout: 10 * time.Second}
	_, err = doNotificationPost(client, webhookURL, body, nil)
	return err
}

func testAlertWebhook() error {
	var stored alertWebhookStored
	loadAlertSetting("alert_webhook", &stored)
	if !stored.Enabled {
		return fmt.Errorf("Webhook alerts are not enabled")
	}
	webhookURL, err := decryptCredential(stored.URLEnc)
	if err != nil || strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("Webhook URL is not configured")
	}

	payload := map[string]any{
		"event":   "test",
		"message": "Anveesa Nias test alert — webhook is configured correctly.",
	}
	body, _ := json.Marshal(payload)

	headers := map[string]string{}
	if stored.AuthHeaderEnc != "" {
		authHeader, err := decryptCredential(stored.AuthHeaderEnc)
		if err == nil && authHeader != "" {
			headers["Authorization"] = authHeader
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	_, err = doNotificationPost(client, webhookURL, body, headers)
	return err
}

// ── Telegram topic management ─────────────────────────────────────────────────

func CreateTelegramTopic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			BotToken  string `json:"bot_token"`
			ChatID    string `json:"chat_id"`
			TopicName string `json:"topic_name"`
			IconColor int    `json:"icon_color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		topicName := strings.TrimSpace(req.TopicName)
		if topicName == "" {
			http.Error(w, `{"error":"topic_name is required"}`, http.StatusBadRequest)
			return
		}

		// Resolve bot token: use the value from the request if it looks like a real
		// token (not masked). If it is masked (contains "•"), fall back to the stored
		// encrypted value matched by chat_id so the user doesn't have to re-enter it.
		botToken := strings.TrimSpace(req.BotToken)
		if strings.Contains(botToken, "•") || botToken == "" {
			var stored alertTelegramStored
			loadAlertSetting("alert_telegram", &stored)
			chatID := strings.TrimSpace(req.ChatID)
			for _, t := range stored.Targets {
				if strings.TrimSpace(t.ChatID) == chatID {
					dec, err := decryptCredential(t.BotTokenEnc)
					if err == nil && strings.TrimSpace(dec) != "" {
						botToken = dec
					}
					break
				}
			}
		}

		if botToken == "" {
			http.Error(w, `{"error":"bot token is required — fill it in or save the target first"}`, http.StatusBadRequest)
			return
		}
		chatID := strings.TrimSpace(req.ChatID)
		if chatID == "" {
			http.Error(w, `{"error":"chat_id is required"}`, http.StatusBadRequest)
			return
		}

		payload := map[string]any{
			"chat_id": chatID,
			"name":    topicName,
		}
		// valid icon_color values per Telegram docs
		validColors := map[int]bool{7322096: true, 16766590: true, 13338331: true, 9367192: true, 16749490: true, 16478047: true}
		if req.IconColor != 0 && validColors[req.IconColor] {
			payload["icon_color"] = req.IconColor
		}

		body, _ := json.Marshal(payload)
		endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/createForumTopic", botToken)
		client := &http.Client{Timeout: 10 * time.Second}

		httpReq, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, jsonError("request error"), http.StatusInternalServerError)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			http.Error(w, jsonError("failed to reach Telegram API"), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var tgResp struct {
			OK     bool `json:"ok"`
			Result struct {
				MessageThreadID int    `json:"message_thread_id"`
				Name            string `json:"name"`
				IconColor       int    `json:"icon_color"`
			} `json:"result"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil || !tgResp.OK {
			msg := tgResp.Description
			if msg == "" {
				msg = "Telegram API returned an error"
			}
			http.Error(w, jsonError(msg), http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"topic_id":   fmt.Sprintf("%d", tgResp.Result.MessageThreadID),
			"name":       tgResp.Result.Name,
			"icon_color": tgResp.Result.IconColor,
		})
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

func loadAlertSetting(key string, dest any) {
	var raw string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT value FROM settings WHERE key = ?`), key).Scan(&raw)
	if err != nil {
		// ErrNoRows is normal — key simply hasn't been saved yet
		if err.Error() != "sql: no rows in result set" {
			fmt.Printf("[alert_settings] load error key=%s: %v\n", key, err)
		}
		return
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), dest); err != nil {
			fmt.Printf("[alert_settings] unmarshal error key=%s: %v\n", key, err)
		}
	}
}

func upsertAlertSetting(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}
	fmt.Printf("[alert_settings] saving key=%s json=%s\n", key, string(raw))
	tx, err := appdb.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(appdb.ConvertQuery(`DELETE FROM settings WHERE key = ?`), key); err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	if _, err = tx.Exec(appdb.ConvertQuery(`INSERT INTO settings (key, value) VALUES (?, ?)`), key, string(raw)); err != nil {
		return fmt.Errorf("insert %s: %w", key, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", key, err)
	}
	fmt.Printf("[alert_settings] saved key=%s ok\n", key)
	return nil
}

// AlertSettingsStatus returns raw DB state for debugging — what keys exist and their sizes.
func AlertSettingsStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		keys := []string{"alert_telegram", "alert_discord", "alert_slack", "alert_webhook"}
		result := map[string]any{}
		for _, key := range keys {
			var raw string
			err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT value FROM settings WHERE key = ?`), key).Scan(&raw)
			if err != nil {
				if err.Error() == "sql: no rows in result set" {
					result[key] = map[string]any{"stored": false}
				} else {
					result[key] = map[string]any{"error": err.Error()}
				}
				continue
			}
			if raw == "" {
				result[key] = map[string]any{"stored": false}
				continue
			}
			var parsed map[string]any
			if jsonErr := json.Unmarshal([]byte(raw), &parsed); jsonErr != nil {
				result[key] = map[string]any{"stored": true, "parse_error": jsonErr.Error(), "raw_len": len(raw)}
				continue
			}
			targets, _ := parsed["targets"].([]any)
			result[key] = map[string]any{
				"stored":         true,
				"enabled":        parsed["enabled"],
				"target_count":   len(targets),
				"raw_bytes":      len(raw),
			}
		}
		json.NewEncoder(w).Encode(result)
	}
}

func alertMaskIfSet(enc string) string {
	if strings.TrimSpace(enc) == "" {
		return ""
	}
	return alertMaskPlaceholder
}
