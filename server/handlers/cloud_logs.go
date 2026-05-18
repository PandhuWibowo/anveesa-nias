package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

var cloudHTTPClient = &http.Client{Timeout: 30 * time.Second}

const (
	alibabaDASSlowLogDefaultLookback = 7 * 24 * time.Hour
	alibabaDASSlowLogChunkRange      = 6 * time.Hour
	alibabaDASSlowLogRetryRange      = time.Hour
	alibabaDASSlowLogMaxChunks       = 32
)

// ── Config CRUD ─────────────────────────────────────────────────────────────

type CloudProviderConfig struct {
	ID         int64  `json:"id"`
	ConnID     int64  `json:"conn_id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	ProjectID  string `json:"project_id"`
	InstanceID string `json:"instance_id"`
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key,omitempty"` // omitted on reads
	IsActive   bool   `json:"is_active"`
}

func GetCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		if err := ensureCloudInstancesForConn(connID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		writeCloudConfigState(w, connID, false)
	}
}

func SaveCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		var body CloudProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		provider := body.Provider
		if provider == "" {
			provider = "huawei"
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		body.Provider = provider
		body.Region = strings.TrimSpace(body.Region)
		body.ProjectID = strings.TrimSpace(body.ProjectID)
		body.InstanceID = strings.TrimSpace(body.InstanceID)
		body.AccessKey = strings.TrimSpace(body.AccessKey)
		body.SecretKey = strings.TrimSpace(body.SecretKey)
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			body.Name = cloudConfigDisplayName(provider, body.Region, body.InstanceID)
		}

		var existing *cloudCfg
		if body.ID > 0 {
			existing, _ = loadCloudConfigByID(connID, body.ID)
			if existing == nil {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "cloud instance config not found"})
				return
			}
		}
		if body.ID == 0 {
			if match, err := loadCloudConfigByIdentity(connID, provider, body.Region, body.InstanceID); err == nil {
				body.ID = match.ID
				existing = match
			}
		}
		if body.SecretKey == "" {
			if existing == nil {
				existing, _ = loadCloudConfig(connID)
			}
			if existing != nil && strings.EqualFold(existing.Provider, provider) && existing.AccessKey == body.AccessKey {
				body.SecretKey = existing.SecretKey
			}
		}
		if body.ID == 0 && existing != nil && strings.EqualFold(existing.Provider, provider) && existing.Region == body.Region && existing.InstanceID == body.InstanceID {
			body.ID = existing.ID
		}

		// Provider-specific required fields:
		// - huawei: region, project_id, instance_id, access_key, secret_key
		// - alibaba: region, instance_id, access_key, secret_key (project_id unused)
		switch provider {
		case "huawei":
			if body.Region == "" || body.ProjectID == "" || body.InstanceID == "" || body.AccessKey == "" || body.SecretKey == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "provider=huawei requires region, project_id, instance_id, access_key, secret_key"})
				return
			}
		case "alibaba":
			if body.Region == "" || body.InstanceID == "" || body.AccessKey == "" || body.SecretKey == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "provider=alibaba requires region, instance_id, access_key, secret_key"})
				return
			}
			body.ProjectID = "" // not used by Alibaba Cloud RDS OpenAPI
		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported provider: " + provider})
			return
		}

		tx, err := appdb.DB.Begin()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if _, err = tx.Exec(appdb.ConvertQuery(`UPDATE cloud_provider_instances SET is_active = 0 WHERE conn_id = ?`), connID); err != nil {
			tx.Rollback()
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if body.ID > 0 {
			_, err := tx.Exec(appdb.ConvertQuery(`
					UPDATE cloud_provider_instances
					SET name = ?, provider = ?, region = ?, project_id = ?, instance_id = ?, access_key = ?, secret_key = ?, is_active = 1, updated_at = CURRENT_TIMESTAMP
					WHERE id = ? AND conn_id = ?
				`), body.Name, provider, body.Region, body.ProjectID, body.InstanceID, body.AccessKey, body.SecretKey, body.ID, connID)
			if err != nil {
				tx.Rollback()
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		} else {
			if _, err := tx.Exec(appdb.ConvertQuery(`
					INSERT INTO cloud_provider_instances (conn_id, name, provider, region, project_id, instance_id, access_key, secret_key, is_active, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
				`), connID, body.Name, provider, body.Region, body.ProjectID, body.InstanceID, body.AccessKey, body.SecretKey); err != nil {
				tx.Rollback()
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}
		if err := tx.Commit(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		_ = syncLegacyCloudConfig(connID, &cloudCfg{
			Name:       body.Name,
			Provider:   provider,
			Region:     body.Region,
			ProjectID:  body.ProjectID,
			InstanceID: body.InstanceID,
			AccessKey:  body.AccessKey,
			SecretKey:  body.SecretKey,
		})
		writeCloudConfigState(w, connID, true)
	}
}

func ActivateCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		var body struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud instance id is required"})
			return
		}
		cfg, err := loadCloudConfigByID(connID, body.ID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud instance config not found"})
			return
		}

		tx, err := appdb.DB.Begin()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if _, err = tx.Exec(appdb.ConvertQuery(`UPDATE cloud_provider_instances SET is_active = 0 WHERE conn_id = ?`), connID); err != nil {
			tx.Rollback()
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if _, err = tx.Exec(appdb.ConvertQuery(`UPDATE cloud_provider_instances SET is_active = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND conn_id = ?`), body.ID, connID); err != nil {
			tx.Rollback()
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = syncLegacyCloudConfig(connID, cfg)
		writeCloudConfigState(w, connID, true)
	}
}

func DeleteCloudConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)

		if id > 0 {
			if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cloud_provider_instances WHERE conn_id = ? AND id = ?`), connID, id); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if err := ensureOneActiveCloudConfig(connID); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			configs, err := listCloudConfigs(connID)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if len(configs) == 0 {
				_, _ = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cloud_provider_configs WHERE conn_id = ?`), connID)
			} else {
				activeID := configs[0].ID
				for _, cfg := range configs {
					if cfg.IsActive {
						activeID = cfg.ID
						break
					}
				}
				if cfg, err := loadCloudConfigByID(connID, activeID); err == nil {
					_ = syncLegacyCloudConfig(connID, cfg)
				}
			}
			writeCloudConfigState(w, connID, true)
			return
		}

		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cloud_provider_instances WHERE conn_id = ?`), connID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM cloud_provider_configs WHERE conn_id = ?`), connID)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "configured": false, "configs": []CloudProviderConfig{}})
	}
}

// ── Huawei Cloud RDS log proxy ──────────────────────────────────────────────

// CloudErrorLogs proxies Huawei RDS error log API.
func CloudErrorLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured for this connection"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			provider = "huawei"
		}
		if provider == "alibaba" {
			cloudErrorLogsAlibaba(cfg)(w, r)
			return
		}
		if provider != "huawei" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported cloud provider: " + cfg.Provider})
			return
		}

		q := r.URL.Query()
		startDate := q.Get("from")
		endDate := q.Get("to")
		level := q.Get("level")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 1 {
			offset = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 100 {
			limit = 50
		}

		// Huawei requires "yyyy-mm-ddThh:mm:ss+0000" format (literal +0000, not Z)
		if startDate == "" {
			startDate = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endDate == "" {
			endDate = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_date", startDate)
		params.Set("end_date", endDate)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))
		if level != "" {
			params.Set("level", strings.ToUpper(level))
		}

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/errorlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(r.Context(), cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudSlowLogs proxies Huawei RDS slow query log API.
func CloudSlowLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured for this connection"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			provider = "huawei"
		}
		if provider == "alibaba" {
			cloudSlowLogsAlibaba(cfg)(w, r)
			return
		}
		if provider != "huawei" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported cloud provider: " + cfg.Provider})
			return
		}

		q := r.URL.Query()
		startDate := q.Get("from")
		endDate := q.Get("to")
		dbName := q.Get("db")
		stmtType := q.Get("type")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 1 {
			offset = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 100 {
			limit = 50
		}

		if startDate == "" {
			startDate = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endDate == "" {
			endDate = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_date", startDate)
		params.Set("end_date", endDate)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))
		if dbName != "" {
			params.Set("database", dbName)
		}
		if stmtType != "" && stmtType != "ALL" {
			params.Set("type", strings.ToUpper(stmtType))
		}

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/slowlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(r.Context(), cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudAuditLogs lists Huawei RDS audit log files for a time range.
func CloudAuditLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			provider = "huawei"
		}
		if provider != "huawei" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud audit logs are only supported for provider=huawei"})
			return
		}

		q := r.URL.Query()
		startTime := q.Get("from")
		endTime := q.Get("to")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		offset, _ := strconv.Atoi(pageStr)
		if offset < 0 {
			offset = 0
		}
		limit, _ := strconv.Atoi(limitStr)
		if limit < 1 || limit > 50 {
			limit = 50
		}

		if startTime == "" {
			startTime = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02T15:04:05+0000")
		}
		if endTime == "" {
			endTime = time.Now().UTC().Format("2006-01-02T15:04:05+0000")
		}

		params := url.Values{}
		params.Set("start_time", startTime)
		params.Set("end_time", endTime)
		params.Set("offset", strconv.Itoa(offset))
		params.Set("limit", strconv.Itoa(limit))

		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/auditlog?%s",
			cfg.Region, cfg.ProjectID, cfg.InstanceID, rfc3986Encode(params),
		)

		body, status, apiErr := huaweiRequest(r.Context(), cfg, "GET", apiURL, "")
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// CloudAuditLogLinks generates 5-minute download links for audit log files.
func CloudAuditLogLinks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		cfg, err := loadCloudConfig(connID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud provider not configured"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			provider = "huawei"
		}
		if provider != "huawei" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "cloud audit log download links are only supported for provider=huawei"})
			return
		}

		var reqBody struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil || len(reqBody.IDs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
			return
		}
		if len(reqBody.IDs) > 50 {
			reqBody.IDs = reqBody.IDs[:50]
		}

		bodyBytes, _ := json.Marshal(reqBody)
		apiURL := fmt.Sprintf(
			"https://rds.%s.myhuaweicloud.com/v3/%s/instances/%s/auditlog-links",
			cfg.Region, cfg.ProjectID, cfg.InstanceID,
		)

		respBody, status, apiErr := huaweiRequest(r.Context(), cfg, "POST", apiURL, string(bodyBytes))
		if apiErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Huawei API error: " + apiErr.Error()})
			return
		}
		w.WriteHeader(status)
		w.Write(respBody)
	}
}

// ── Alibaba Cloud RDS log proxy ─────────────────────────────────────────────

func cloudErrorLogsAlibaba(cfg *cloudCfg) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		fromStr := q.Get("from")
		toStr := q.Get("to")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")

		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		// Alibaba RDS RPC APIs expect PageSize 30..100.
		if limit < 30 {
			limit = 30
		}
		if limit > 100 {
			limit = 100
		}

		fromT := parseCloudTimeOrDefault(fromStr, time.Now().UTC().AddDate(0, 0, -7))
		toT := parseCloudTimeOrDefault(toStr, time.Now().UTC())
		startTime := fromT.UTC().Format("2006-01-02T15:04Z")
		endTime := toT.UTC().Format("2006-01-02T15:04Z")

		resp, err := aliyunRDSRPC(r.Context(), cfg, "DescribeErrorLogs", map[string]string{
			"RegionId":     cfg.Region,
			"DBInstanceId": cfg.InstanceID,
			"StartTime":    startTime,
			"EndTime":      endTime,
			"PageSize":     strconv.Itoa(limit),
			"PageNumber":   strconv.Itoa(page),
		})
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "Alibaba API error: " + err.Error()})
			return
		}

		total := int(anyInt64(resp["TotalRecordCount"]))
		list := make([]map[string]any, 0)
		if items, ok := resp["Items"].(map[string]any); ok {
			if rows, ok := items["ErrorLog"].([]any); ok {
				for _, row := range rows {
					m, _ := row.(map[string]any)
					if m == nil {
						continue
					}
					list = append(list, map[string]any{
						"time":    anyString(m["CreateTime"]),
						"level":   "ERROR",
						"content": anyString(m["ErrorInfo"]),
					})
				}
			}
		}
		if list == nil {
			list = []map[string]any{}
		}

		// Return Huawei-compatible shape so the existing UI mapping works.
		json.NewEncoder(w).Encode(map[string]any{
			"error_log_list": list,
			"total_record":   total,
			"provider":       "alibaba",
			"source":         "Alibaba Cloud RDS API",
		})
	}
}

func cloudSlowLogsAlibaba(cfg *cloudCfg) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		fromStr := q.Get("from")
		toStr := q.Get("to")
		pageStr := q.Get("page")
		limitStr := q.Get("limit")
		dbName := q.Get("db")

		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(limitStr)
		// DAS supports PageSize 5..100 and supports RDS PostgreSQL slow logs.
		if limit < 5 {
			limit = 25
		}
		if limit > 100 {
			limit = 100
		}

		now := time.Now().UTC()
		toT := parseCloudTimeOrDefault(toStr, now)
		if toT.After(now) {
			toT = now
		}
		fromT := parseCloudTimeOrDefault(fromStr, toT.Add(-alibabaDASSlowLogDefaultLookback))
		notice := ""
		if fromT.After(toT) {
			fromT = toT.Add(-alibabaDASSlowLogDefaultLookback)
			notice = "Alibaba DAS slow-log range was adjusted because the start time was after the end time."
		}

		list := make([]map[string]any, 0)
		total := 0
		if toT.Sub(fromT) <= alibabaDASSlowLogChunkRange {
			rows, chunkTotal, err := fetchAlibabaDASSlowLogChunk(r.Context(), cfg, fromT, toT, limit, page, dbName)
			if err != nil && strings.Contains(err.Error(), "RequestTimeout") && toT.Sub(fromT) > alibabaDASSlowLogRetryRange {
				fromT = toT.Add(-alibabaDASSlowLogRetryRange)
				notice = "Alibaba DAS timed out, so results were narrowed to the final 1 hour of the selected range."
				rows, chunkTotal, err = fetchAlibabaDASSlowLogChunk(r.Context(), cfg, fromT, toT, limit, page, dbName)
			}
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{"error": "Alibaba API error: " + err.Error() + ". Try a shorter time range or add a database filter."})
				return
			}
			list = rows
			total = chunkTotal
		} else {
			notice = "Alibaba DAS slow-log results were scanned in 6-hour chunks to avoid provider timeouts."
			offset := (page - 1) * limit
			seen := 0
			chunkEnd := toT
			var lastErr error
			for chunks := 0; chunkEnd.After(fromT) && chunks < alibabaDASSlowLogMaxChunks && len(list) < limit; chunks++ {
				chunkStart := chunkEnd.Add(-alibabaDASSlowLogChunkRange)
				if chunkStart.Before(fromT) {
					chunkStart = fromT
				}

				rows, chunkTotal, err := fetchAlibabaDASSlowLogChunk(r.Context(), cfg, chunkStart, chunkEnd, 100, 1, dbName)
				if err != nil && strings.Contains(err.Error(), "RequestTimeout") {
					subEnd := chunkEnd
					for subEnd.After(chunkStart) && len(list) < limit {
						subStart := subEnd.Add(-alibabaDASSlowLogRetryRange)
						if subStart.Before(chunkStart) {
							subStart = chunkStart
						}
						rows, chunkTotal, err = fetchAlibabaDASSlowLogChunk(r.Context(), cfg, subStart, subEnd, 100, 1, dbName)
						if err != nil {
							lastErr = err
						} else {
							total += chunkTotal
							seen = appendAlibabaSlowRowsForPage(&list, rows, seen, offset, limit)
						}
						subEnd = subStart
					}
				} else if err != nil {
					lastErr = err
				} else {
					total += chunkTotal
					seen = appendAlibabaSlowRowsForPage(&list, rows, seen, offset, limit)
				}
				chunkEnd = chunkStart
			}
			if lastErr != nil && len(list) == 0 && total == 0 {
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{"error": "Alibaba API error: " + lastErr.Error() + ". Try a shorter time range or add a database filter."})
				return
			}
		}
		if list == nil {
			list = []map[string]any{}
		}

		// Return Huawei-compatible shape so the existing UI mapping works.
		json.NewEncoder(w).Encode(map[string]any{
			"slow_log_list": list,
			"total_record":  total,
			"provider":      "alibaba",
			"source":        "Alibaba Cloud DAS API",
			"notice":        notice,
		})
	}
}

func fetchAlibabaDASSlowLogChunk(ctx context.Context, cfg *cloudCfg, fromT, toT time.Time, limit, page int, dbName string) ([]map[string]any, int, error) {
	params := map[string]string{
		"RegionId":   cfg.Region,
		"InstanceId": cfg.InstanceID,
		"StartTime":  strconv.FormatInt(fromT.UTC().UnixMilli(), 10),
		"EndTime":    strconv.FormatInt(toT.UTC().UnixMilli(), 10),
		"PageSize":   strconv.Itoa(limit),
		"PageNumber": strconv.Itoa(page),
	}
	if strings.TrimSpace(dbName) != "" {
		params["Filters.1.Key"] = "dbName"
		params["Filters.1.Value"] = strings.TrimSpace(dbName)
	}

	resp, err := aliyunDASRPC(ctx, cfg, "DescribeSlowLogRecords", params)
	if err != nil {
		return nil, 0, err
	}
	return parseAlibabaDASSlowLogResponse(resp)
}

func parseAlibabaDASSlowLogResponse(resp map[string]any) ([]map[string]any, int, error) {
	data := anyMap(resp["Data"])
	if data == nil {
		data = resp
	}
	total := int(firstInt64(data, "TotalRecords", "TotalRecordCount", "totalRecords", "totalRecordCount", "Total", "total", "Count", "count"))
	list := make([]map[string]any, 0)
	rows := firstSlice(data, "Logs", "logs", "List", "list", "Items", "items", "Records", "records", "Rows", "rows", "Data", "data", "Result", "result")
	if rows == nil {
		rows = firstSlice(resp, "Logs", "logs", "List", "list", "Items", "items", "Records", "records", "Rows", "rows", "Data", "data", "Result", "result")
	}
	if total == 0 && rows != nil {
		total = len(rows)
	}
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if m == nil {
			continue
		}

		sqlText := firstString(m, "SQLText", "SqlText", "sqlText", "sql_text", "SQL", "sql", "Psql", "psql", "Query", "query")
		if sqlText == "" {
			sqlText = anyString(m["Command"])
		}
		stmtType := firstString(m, "SqlType", "sqlType", "sql_type", "Type", "type")
		if stmtType == "" {
			stmtType = inferStatementType(sqlText)
		}

		queryMs := firstFloat64(m, "QueryTime", "queryTime", "query_time", "Duration", "duration", "ElapsedTime", "elapsedTime")
		if queryMs == 0 {
			queryMs = firstFloat64(m, "QueryTimeSeconds", "queryTimeSeconds", "query_time_seconds") * 1000.0
		}
		lockMs := firstFloat64(m, "LockTime", "lockTime", "lock_time")
		if lockMs == 0 {
			lockMs = firstFloat64(m, "LockTimeSeconds", "lockTimeSeconds", "lock_time_seconds") * 1000.0
		}

		rowsSent := firstInt64(m, "RowsSent", "rowsSent", "rows_sent")
		if rowsSent == 0 {
			rowsSent = firstInt64(m, "ReturnItemNumbers", "returnItemNumbers", "return_item_numbers")
		}
		if rowsSent == 0 {
			rowsSent = firstInt64(m, "ReturnNum", "returnNum", "return_num")
		}

		list = append(list, map[string]any{
			"query_sample":  sqlText,
			"type":          strings.ToUpper(stmtType),
			"database":      firstString(m, "DBName", "dbName", "db_name", "Database", "database"),
			"users":         firstString(m, "AccountName", "accountName", "account_name", "User", "user", "UserName", "userName"),
			"count":         "1",
			"time":          formatDurationMs(queryMs),
			"lock_time":     formatDurationMs(lockMs),
			"rows_sent":     strconv.FormatInt(rowsSent, 10),
			"rows_examined": strconv.FormatInt(firstInt64(m, "RowsExamined", "rowsExamined", "rows_examined"), 10),
			"client_ip":     firstString(m, "HostAddress", "hostAddress", "host_address", "ClientIP", "clientIp", "client_ip"),
			"start_time":    firstString(m, "QueryStartTime", "queryStartTime", "query_start_time", "StartTime", "startTime", "start_time", "Timestamp", "timestamp"),
		})
	}
	return list, total, nil
}

func appendAlibabaSlowRowsForPage(dst *[]map[string]any, rows []map[string]any, seen, offset, limit int) int {
	for _, row := range rows {
		if seen >= offset && len(*dst) < limit {
			*dst = append(*dst, row)
		}
		seen++
	}
	return seen
}

// ── Huawei AK/SK signer ─────────────────────────────────────────────────────

type cloudCfg struct {
	ID         int64
	Name       string
	Provider   string
	Region     string
	ProjectID  string
	InstanceID string
	AccessKey  string
	SecretKey  string
}

func loadCloudConfig(connID int64) (*cloudCfg, error) {
	_ = ensureCloudInstancesForConn(connID)
	var cfg cloudCfg
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_instances
		WHERE conn_id = ?
		ORDER BY is_active DESC, id ASC
		LIMIT 1
	`), connID).Scan(&cfg.ID, &cfg.Name, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	if err == nil {
		return &cfg, nil
	}
	if !errorsIsNoRows(err) {
		return nil, err
	}

	err = appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_configs WHERE conn_id = ?
	`), connID).Scan(&cfg.ID, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	cfg.Name = cloudConfigDisplayName(cfg.Provider, cfg.Region, cfg.InstanceID)
	return &cfg, err
}

func loadCloudConfigByID(connID, id int64) (*cloudCfg, error) {
	var cfg cloudCfg
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_instances
		WHERE conn_id = ? AND id = ?
	`), connID, id).Scan(&cfg.ID, &cfg.Name, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadCloudConfigByIdentity(connID int64, provider, region, instanceID string) (*cloudCfg, error) {
	var cfg cloudCfg
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_instances
		WHERE conn_id = ? AND provider = ? AND region = ? AND instance_id = ?
		ORDER BY is_active DESC, id ASC
		LIMIT 1
	`), connID, provider, region, instanceID).Scan(&cfg.ID, &cfg.Name, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ensureCloudInstancesForConn(connID int64) error {
	var count int
	if err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM cloud_provider_instances WHERE conn_id = ?`), connID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	var cfg cloudCfg
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT provider, region, project_id, instance_id, access_key, secret_key
		FROM cloud_provider_configs WHERE conn_id = ?
	`), connID).Scan(&cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &cfg.SecretKey)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil
		}
		return err
	}
	cfg.Name = cloudConfigDisplayName(cfg.Provider, cfg.Region, cfg.InstanceID)
	_, err = appdb.DB.Exec(appdb.ConvertQuery(`
		INSERT INTO cloud_provider_instances (conn_id, name, provider, region, project_id, instance_id, access_key, secret_key, is_active, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
	`), connID, cfg.Name, cfg.Provider, cfg.Region, cfg.ProjectID, cfg.InstanceID, cfg.AccessKey, cfg.SecretKey)
	return err
}

func ensureOneActiveCloudConfig(connID int64) error {
	var activeCount int
	if err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM cloud_provider_instances WHERE conn_id = ? AND is_active = 1`), connID).Scan(&activeCount); err != nil {
		return err
	}
	if activeCount > 0 {
		return nil
	}
	var firstID int64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT id FROM cloud_provider_instances WHERE conn_id = ? ORDER BY id ASC LIMIT 1`), connID).Scan(&firstID)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil
		}
		return err
	}
	_, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE cloud_provider_instances SET is_active = 1, updated_at = CURRENT_TIMESTAMP WHERE conn_id = ? AND id = ?`), connID, firstID)
	return err
}

func syncLegacyCloudConfig(connID int64, cfg *cloudCfg) error {
	if cfg == nil {
		return nil
	}
	_, err := appdb.DB.Exec(appdb.ConvertQuery(`
		INSERT INTO cloud_provider_configs (conn_id, provider, region, project_id, instance_id, access_key, secret_key, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(conn_id) DO UPDATE SET
			provider    = excluded.provider,
			region      = excluded.region,
			project_id  = excluded.project_id,
			instance_id = excluded.instance_id,
			access_key  = excluded.access_key,
			secret_key  = excluded.secret_key,
			updated_at  = CURRENT_TIMESTAMP
	`), connID, cfg.Provider, cfg.Region, cfg.ProjectID, cfg.InstanceID, cfg.AccessKey, cfg.SecretKey)
	return err
}

func listCloudConfigs(connID int64) ([]CloudProviderConfig, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, conn_id, name, provider, region, project_id, instance_id, access_key, is_active
		FROM cloud_provider_instances
		WHERE conn_id = ?
		ORDER BY is_active DESC, provider ASC, region ASC, name ASC, id ASC
	`), connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configs := make([]CloudProviderConfig, 0)
	for rows.Next() {
		var cfg CloudProviderConfig
		var active int
		if err := rows.Scan(&cfg.ID, &cfg.ConnID, &cfg.Name, &cfg.Provider, &cfg.Region, &cfg.ProjectID, &cfg.InstanceID, &cfg.AccessKey, &active); err != nil {
			return nil, err
		}
		cfg.SecretKey = ""
		cfg.IsActive = active != 0
		if cfg.Name == "" {
			cfg.Name = cloudConfigDisplayName(cfg.Provider, cfg.Region, cfg.InstanceID)
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

func writeCloudConfigState(w http.ResponseWriter, connID int64, ok bool) {
	configs, err := listCloudConfigs(connID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	var active *CloudProviderConfig
	for i := range configs {
		if configs[i].IsActive {
			active = &configs[i]
			break
		}
	}
	if active == nil && len(configs) > 0 {
		active = &configs[0]
	}
	resp := map[string]any{
		"configured": active != nil,
		"configs":    configs,
	}
	if ok {
		resp["ok"] = true
	}
	if active != nil {
		resp["config"] = active
		resp["provider"] = active.Provider
	}
	json.NewEncoder(w).Encode(resp)
}

func cloudConfigDisplayName(provider, region, instanceID string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	label := "Cloud"
	if provider == "alibaba" {
		label = "Alibaba"
	} else if provider == "huawei" {
		label = "Huawei"
	}
	instanceID = strings.TrimSpace(instanceID)
	region = strings.TrimSpace(region)
	if instanceID != "" && region != "" {
		return label + " · " + instanceID + " · " + region
	}
	if instanceID != "" {
		return label + " · " + instanceID
	}
	return label
}

func errorsIsNoRows(err error) bool {
	return err == nil || err == sql.ErrNoRows
}

// huaweiRequest performs an AK/SK-signed request to Huawei Cloud API.
// It follows Huawei Cloud's standard AK/SK signing scheme where the HMAC key
// is the raw SecretKey (not a derived key like AWS SigV4).
func huaweiRequest(ctx context.Context, cfg *cloudCfg, method, rawURL, body string) ([]byte, int, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	dateLong := now.Format("20060102T150405Z")

	// Canonical query string (sorted, RFC 3986 percent-encoded — no + for spaces)
	queryParams := parsedURL.Query()
	queryKeys := make([]string, 0, len(queryParams))
	for k := range queryParams {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)
	canonicalQueryParts := make([]string, 0, len(queryKeys))
	for _, k := range queryKeys {
		for _, v := range queryParams[k] {
			canonicalQueryParts = append(canonicalQueryParts, rfc3986EncodeStr(k)+"="+rfc3986EncodeStr(v))
		}
	}
	canonicalQuery := strings.Join(canonicalQueryParts, "&")

	// Canonical headers (must be lowercase, sorted, terminated with \n each)
	host := parsedURL.Host
	canonicalHeaders := fmt.Sprintf("content-type:application/json\nhost:%s\nx-sdk-date:%s\n", host, dateLong)
	signedHeaders := "content-type;host;x-sdk-date"

	// Payload hash
	payloadHash := sha256Hex(body)

	// Canonical URI must end with "/" per Huawei signing spec, even though the
	// actual HTTP request does not include the trailing slash.
	canonicalURI := parsedURL.EscapedPath()
	if !strings.HasSuffix(canonicalURI, "/") {
		canonicalURI += "/"
	}

	// Canonical request
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// String to sign — Huawei uses SDK-HMAC-SHA256 with only date + hash (no credential scope)
	stringToSign := strings.Join([]string{
		"SDK-HMAC-SHA256",
		dateLong,
		sha256Hex(canonicalRequest),
	}, "\n")

	// Signing key is the raw SecretKey bytes (no derivation)
	signature := hex.EncodeToString(cloudHMAC([]byte(cfg.SecretKey), stringToSign))

	authHeader := fmt.Sprintf(
		"SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		cfg.AccessKey, signedHeaders, signature,
	)

	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sdk-Date", dateLong)
	req.Header.Set("Authorization", authHeader)

	resp, err := cloudHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

// rfc3986EncodeStr percent-encodes a string per RFC 3986 (spaces become %20, not +).
func rfc3986EncodeStr(s string) string {
	return strings.NewReplacer("+", "%20").Replace(url.QueryEscape(s))
}

// rfc3986Encode encodes url.Values using RFC 3986 percent-encoding.
func rfc3986Encode(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(v))
	for _, k := range keys {
		for _, val := range v[k] {
			parts = append(parts, rfc3986EncodeStr(k)+"="+rfc3986EncodeStr(val))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func cloudHMAC(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// ── Alibaba Cloud RDS (RPC signature v1.0) helpers ──────────────────────────

func aliyunRDSRPC(ctx context.Context, cfg *cloudCfg, action string, params map[string]string) (map[string]any, error) {
	all := map[string]string{
		"Action":           action,
		"Version":          "2014-08-15",
		"Format":           "JSON",
		"AccessKeyId":      cfg.AccessKey,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   aliyunNonce(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		all[k] = v
	}
	if _, ok := all["RegionId"]; !ok && strings.TrimSpace(cfg.Region) != "" {
		all["RegionId"] = strings.TrimSpace(cfg.Region)
	}

	all["Signature"] = aliyunSignature("GET", all, cfg.SecretKey)
	rawURL := "https://" + aliyunRDSEndpoint(cfg.Region) + "/?" + aliyunCanonicalQuery(all)

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cloudHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("invalid Alibaba response (HTTP %d)", resp.StatusCode)
	}
	if code, ok := out["Code"].(string); ok && code != "" {
		msg := anyString(out["Message"])
		if msg == "" {
			msg = anyString(out["message"])
		}
		if msg != "" {
			return nil, fmt.Errorf("%s: %s", code, msg)
		}
		return nil, fmt.Errorf("%s", code)
	}
	if resp.StatusCode >= 400 {
		if msg := anyString(out["Message"]); msg != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}

func aliyunDASRPC(ctx context.Context, cfg *cloudCfg, action string, params map[string]string) (map[string]any, error) {
	all := map[string]string{
		"Action":           action,
		"Version":          "2020-01-16",
		"Format":           "JSON",
		"AccessKeyId":      cfg.AccessKey,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   aliyunNonce(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		all[k] = v
	}
	if _, ok := all["RegionId"]; !ok && strings.TrimSpace(cfg.Region) != "" {
		all["RegionId"] = strings.TrimSpace(cfg.Region)
	}

	all["Signature"] = aliyunSignature("GET", all, cfg.SecretKey)
	rawURL := "https://das.cn-shanghai.aliyuncs.com/?" + aliyunCanonicalQuery(all)

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cloudHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("invalid Alibaba DAS response (HTTP %d)", resp.StatusCode)
	}
	success := anyBool(out["Success"])
	code := anyString(out["Code"])
	if !success || resp.StatusCode >= 400 || (code != "" && code != "200") {
		msg := anyString(out["Message"])
		if msg == "" {
			msg = anyString(out["message"])
		}
		if code != "" && msg != "" {
			return nil, fmt.Errorf("%s: %s", code, msg)
		}
		if msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		if code != "" {
			return nil, fmt.Errorf("%s", code)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}

func aliyunRDSEndpoint(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "cn-zhangjiakou":
		return "rds.cn-zhangjiakou.aliyuncs.com"
	case "ap-northeast-1":
		return "rds.ap-northeast-1.aliyuncs.com"
	case "ap-southeast-3":
		return "rds.ap-southeast-3.aliyuncs.com"
	case "me-east-1":
		return "rds.me-east-1.aliyuncs.com"
	case "cn-huhehaote":
		return "rds.cn-huhehaote.aliyuncs.com"
	case "ap-southeast-5":
		return "rds.ap-southeast-5.aliyuncs.com"
	case "eu-central-1":
		return "rds.eu-central-1.aliyuncs.com"
	case "eu-west-1":
		return "rds.eu-west-1.aliyuncs.com"
	case "cn-chengdu":
		return "rds.cn-chengdu.aliyuncs.com"
	default:
		return "rds.aliyuncs.com"
	}
}

func aliyunCanonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, aliyunPercentEncode(k)+"="+aliyunPercentEncode(params[k]))
	}
	return strings.Join(parts, "&")
}

func aliyunSignature(method string, params map[string]string, secret string) string {
	filtered := map[string]string{}
	for k, v := range params {
		if strings.EqualFold(k, "Signature") {
			continue
		}
		filtered[k] = v
	}
	canonical := aliyunCanonicalQuery(filtered)
	stringToSign := method + "&" + aliyunPercentEncode("/") + "&" + aliyunPercentEncode(canonical)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func aliyunPercentEncode(s string) string {
	// Aliyun RPC signature encoding:
	// - space -> %20
	// - * -> %2A
	// - %7E -> ~
	return strings.NewReplacer("+", "%20", "*", "%2A", "%7E", "~").Replace(url.QueryEscape(s))
}

func aliyunNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func parseCloudTimeOrDefault(raw string, def time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	layouts := []string{
		"2006-01-02",
		"2006-01-02T15:04:05-0700", // e.g. 2026-05-16T00:00:00+0000
		"2006-01-02T15:04Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	if len(raw) >= 10 {
		if t, err := time.Parse("2006-01-02", raw[:10]); err == nil {
			return t
		}
	}
	return def
}

func formatDurationMs(ms float64) string {
	if ms <= 0 {
		return "0 ms"
	}
	if ms >= 1000 {
		return fmt.Sprintf("%.3f s", ms/1000.0)
	}
	if ms >= 10 {
		return fmt.Sprintf("%.1f ms", ms)
	}
	return fmt.Sprintf("%.3f ms", ms)
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

func anyInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		i, _ := x.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return i
	default:
		return 0
	}
}

func anyFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		return 0
	}
}

func anyBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true") || strings.TrimSpace(x) == "1"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

func anyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := anyString(m[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := anyInt64(m[key]); value != 0 {
			return value
		}
	}
	return 0
}

func firstFloat64(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value := anyFloat64(m[key]); value != 0 {
			return value
		}
	}
	return 0
}

func firstSlice(m map[string]any, keys ...string) []any {
	for _, key := range keys {
		switch value := m[key].(type) {
		case []any:
			return value
		case map[string]any:
			if rows := firstSlice(value, "Logs", "logs", "List", "list", "Items", "items", "Records", "records", "Rows", "rows"); rows != nil {
				return rows
			}
		}
	}
	return nil
}
