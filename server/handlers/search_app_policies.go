package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type SearchAppPolicy struct {
	ID             int64   `json:"id"`
	ConnID         int64   `json:"conn_id"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	ThresholdValue float64 `json:"threshold_value"`
	ThresholdUnit  string  `json:"threshold_unit"`
	Action         string  `json:"action"`
	Enabled        bool    `json:"enabled"`
	LastRunAt      string  `json:"last_run_at"`
	LastResult     string  `json:"last_result"`
	CreatedAt      string  `json:"created_at"`
}

type SearchAppPolicyViolation struct {
	Index string `json:"index"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

type SearchAppPolicyRunResult struct {
	PolicyID   int64                      `json:"policy_id"`
	Evaluated  int                        `json:"evaluated"`
	Violations []SearchAppPolicyViolation `json:"violations"`
	Summary    string                     `json:"summary"`
}

func ListSearchAppPolicies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connIDStr := r.URL.Query().Get("conn_id")
		var (
			rows *sql.Rows
			err  error
		)
		if connIDStr != "" {
			connID, _ := strconv.ParseInt(connIDStr, 10, 64)
			rows, err = appdb.DB.QueryContext(r.Context(),
				appdb.ConvertQuery(`SELECT id, conn_id, name, type, threshold_value, threshold_unit, action, enabled, last_run_at, COALESCE(last_result,''), created_at FROM search_app_policies WHERE conn_id=? ORDER BY created_at DESC`),
				connID)
		} else {
			rows, err = appdb.DB.QueryContext(r.Context(),
				appdb.ConvertQuery(`SELECT id, conn_id, name, type, threshold_value, threshold_unit, action, enabled, last_run_at, COALESCE(last_result,''), created_at FROM search_app_policies ORDER BY created_at DESC`))
		}
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		policies := make([]SearchAppPolicy, 0)
		for rows.Next() {
			var p SearchAppPolicy
			var enabled int
			var lastRunAt sql.NullString
			if err := rows.Scan(&p.ID, &p.ConnID, &p.Name, &p.Type, &p.ThresholdValue, &p.ThresholdUnit, &p.Action, &enabled, &lastRunAt, &p.LastResult, &p.CreatedAt); err != nil {
				continue
			}
			p.Enabled = enabled == 1
			p.LastRunAt = lastRunAt.String
			policies = append(policies, p)
		}
		json.NewEncoder(w).Encode(policies)
	}
}

func CreateSearchAppPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var p SearchAppPolicy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, jsonError("invalid JSON body"), http.StatusBadRequest)
			return
		}
		if p.ConnID == 0 || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Type) == "" {
			http.Error(w, jsonError("conn_id, name and type are required"), http.StatusBadRequest)
			return
		}
		if p.ThresholdUnit == "" {
			p.ThresholdUnit = "GB"
		}
		if p.Action == "" {
			p.Action = "alert"
		}
		enabled := 1
		if !p.Enabled {
			enabled = 0
		}
		res, err := appdb.DB.Exec(
			appdb.ConvertQuery(`INSERT INTO search_app_policies (conn_id, name, type, threshold_value, threshold_unit, action, enabled) VALUES (?,?,?,?,?,?,?)`),
			p.ConnID, strings.TrimSpace(p.Name), p.Type, p.ThresholdValue, p.ThresholdUnit, p.Action, enabled,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		p.ID = id
		p.Enabled = enabled == 1
		json.NewEncoder(w).Encode(p)
	}
}

func UpdateSearchAppPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := policyIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid policy id"), http.StatusBadRequest)
			return
		}
		var p SearchAppPolicy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, jsonError("invalid JSON body"), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Type) == "" {
			http.Error(w, jsonError("name and type are required"), http.StatusBadRequest)
			return
		}
		enabled := 0
		if p.Enabled {
			enabled = 1
		}
		_, err = appdb.DB.Exec(
			appdb.ConvertQuery(`UPDATE search_app_policies SET name=?, type=?, threshold_value=?, threshold_unit=?, action=?, enabled=? WHERE id=?`),
			strings.TrimSpace(p.Name), p.Type, p.ThresholdValue, p.ThresholdUnit, p.Action, enabled, id,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		p.ID = id
		p.Enabled = enabled == 1
		json.NewEncoder(w).Encode(p)
	}
}

func DeleteSearchAppPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := policyIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid policy id"), http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM search_app_policies WHERE id=?`), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func RunSearchAppPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := policyIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid policy id"), http.StatusBadRequest)
			return
		}

		var p SearchAppPolicy
		var enabled int
		err = appdb.DB.QueryRow(
			appdb.ConvertQuery(`SELECT id, conn_id, name, type, threshold_value, threshold_unit, action, enabled FROM search_app_policies WHERE id=?`), id,
		).Scan(&p.ID, &p.ConnID, &p.Name, &p.Type, &p.ThresholdValue, &p.ThresholdUnit, &p.Action, &enabled)
		p.Enabled = enabled == 1
		if err != nil {
			http.Error(w, jsonError("policy not found"), http.StatusNotFound)
			return
		}
		p.Enabled = enabled == 1

		result, err := evaluateSearchPolicy(r.Context(), p)
		if err != nil {
			http.Error(w, jsonError("evaluation failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		summary := fmt.Sprintf("%d violation(s) found across %d indices", len(result.Violations), result.Evaluated)
		resultJSON, _ := json.Marshal(result.Violations)
		now := time.Now().UTC().Format(time.RFC3339)
		appdb.DB.Exec(
			appdb.ConvertQuery(`UPDATE search_app_policies SET last_run_at=?, last_result=? WHERE id=?`),
			now, string(resultJSON), id,
		)
		result.Summary = summary
		json.NewEncoder(w).Encode(result)
	}
}

func evaluateSearchPolicy(ctx context.Context, p SearchAppPolicy) (*SearchAppPolicyRunResult, error) {
	client, err := openSearchClient(p.ConnID)
	if err != nil {
		return nil, err
	}

	var indices []SearchIndexInfo
	path := "/_cat/indices/*?format=json&bytes=b&s=index&expand_wildcards=all&h=health,status,index,uuid,pri,rep,docs.count,store.size,creation.date,creation.date.string"
	if err := client.doJSON(ctx, "GET", path, nil, &indices); err != nil {
		return nil, err
	}

	result := &SearchAppPolicyRunResult{
		PolicyID:   p.ID,
		Evaluated:  len(indices),
		Violations: []SearchAppPolicyViolation{},
	}

	thresholdBytes := toBytes(p.ThresholdValue, p.ThresholdUnit)

	for _, idx := range indices {
		switch p.Type {
		case "size_alert", "auto_delete_size":
			if idx.StoreBytes >= thresholdBytes {
				result.Violations = append(result.Violations, SearchAppPolicyViolation{
					Index: idx.Name,
					Value: formatPolicyBytes(idx.StoreBytes),
					Note:  fmt.Sprintf("size %s exceeds threshold %s %s", formatPolicyBytes(idx.StoreBytes), formatPolicyFloat(p.ThresholdValue), p.ThresholdUnit),
				})
			}
		case "auto_delete_age":
			if idx.CreatedAt == "" {
				continue
			}
			created, err := time.Parse(time.RFC3339, idx.CreatedAt)
			if err != nil {
				continue
			}
			ageDays := time.Since(created).Hours() / 24
			if ageDays >= p.ThresholdValue {
				result.Violations = append(result.Violations, SearchAppPolicyViolation{
					Index: idx.Name,
					Value: fmt.Sprintf("%.0f days", ageDays),
					Note:  fmt.Sprintf("age %.0f days exceeds threshold %.0f days", ageDays, p.ThresholdValue),
				})
			}
		}
	}
	return result, nil
}

func toBytes(value float64, unit string) int64 {
	switch strings.ToUpper(unit) {
	case "KB":
		return int64(value * 1024)
	case "MB":
		return int64(value * 1024 * 1024)
	case "GB":
		return int64(value * 1024 * 1024 * 1024)
	case "TB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(value)
	}
}

func formatPolicyBytes(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(b)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func formatPolicyFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func policyIDFromPath(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/search-app-policies/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("missing policy id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}
