package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

// InfraAlertRule represents an infrastructure alert rule stored in the app DB.
type InfraAlertRule struct {
	ID           int64   `json:"id"`
	ConnID       int64   `json:"conn_id"`
	Name         string  `json:"name"`
	MetricField  string  `json:"metric_field"`
	GroupField   string  `json:"group_field"`
	IndexPattern string  `json:"index_pattern"`
	Threshold    float64 `json:"threshold"`
	Comparison   string  `json:"comparison"`
	DurationMin  int     `json:"duration_min"`
	Enabled      bool    `json:"enabled"`
	CreatedBy    *int64  `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// ListInfraAlertRules returns all alert rules for a connection.
// GET /api/connections/{id}/infra-alert-rules
func ListInfraAlertRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, conn_id, name, metric_field, group_field, index_pattern,
			        threshold, comparison, duration_min, enabled, created_by, created_at, updated_at
			 FROM infra_alert_rules
			 WHERE conn_id = ?
			 ORDER BY created_at DESC`),
			connID,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []InfraAlertRule
		for rows.Next() {
			var rule InfraAlertRule
			var enabledInt int
			if err := rows.Scan(
				&rule.ID, &rule.ConnID, &rule.Name, &rule.MetricField, &rule.GroupField,
				&rule.IndexPattern, &rule.Threshold, &rule.Comparison, &rule.DurationMin,
				&enabledInt, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt,
			); err != nil {
				http.Error(w, jsonError("failed to read alert rules"), http.StatusInternalServerError)
				return
			}
			rule.Enabled = enabledInt == 1
			list = append(list, rule)
		}
		if list == nil {
			list = []InfraAlertRule{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

// CreateInfraAlertRule creates a new alert rule for a connection.
// POST /api/connections/{id}/infra-alert-rules
func CreateInfraAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Name         string  `json:"name"`
			MetricField  string  `json:"metric_field"`
			GroupField   string  `json:"group_field"`
			IndexPattern string  `json:"index_pattern"`
			Threshold    float64 `json:"threshold"`
			Comparison   string  `json:"comparison"`
			DurationMin  int     `json:"duration_min"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Name == "" || body.MetricField == "" {
			http.Error(w, jsonError("name and metric_field are required"), http.StatusBadRequest)
			return
		}
		if body.GroupField == "" {
			body.GroupField = "host.name"
		}
		if body.IndexPattern == "" {
			body.IndexPattern = "metricbeat-*"
		}
		if body.Comparison == "" {
			body.Comparison = "gt"
		}
		if body.DurationMin == 0 {
			body.DurationMin = 5
		}

		userID, _, _ := currentUserFromHeaders(r)
		var createdBy *int64
		if userID != 0 {
			createdBy = &userID
		}

		res, err := appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO infra_alert_rules
			 (conn_id, name, metric_field, group_field, index_pattern, threshold, comparison, duration_min, enabled, created_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`),
			connID, body.Name, body.MetricField, body.GroupField, body.IndexPattern,
			body.Threshold, body.Comparison, body.DurationMin, createdBy,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var rule InfraAlertRule
		var enabledInt int
		appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, conn_id, name, metric_field, group_field, index_pattern,
			        threshold, comparison, duration_min, enabled, created_by, created_at, updated_at
			 FROM infra_alert_rules WHERE id = ?`), id).Scan(
			&rule.ID, &rule.ConnID, &rule.Name, &rule.MetricField, &rule.GroupField,
			&rule.IndexPattern, &rule.Threshold, &rule.Comparison, &rule.DurationMin,
			&enabledInt, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt,
		)
		rule.Enabled = enabledInt == 1

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(rule)
	}
}

// UpdateInfraAlertRule updates an existing alert rule.
// PUT /api/connections/{id}/infra-alert-rules/{ruleID}
func UpdateInfraAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ruleID, err := infraRuleIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid rule id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Name         string  `json:"name"`
			MetricField  string  `json:"metric_field"`
			GroupField   string  `json:"group_field"`
			IndexPattern string  `json:"index_pattern"`
			Threshold    float64 `json:"threshold"`
			Comparison   string  `json:"comparison"`
			DurationMin  int     `json:"duration_min"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Name == "" || body.MetricField == "" {
			http.Error(w, jsonError("name and metric_field are required"), http.StatusBadRequest)
			return
		}

		_, err = appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE infra_alert_rules
			 SET name=?, metric_field=?, group_field=?, index_pattern=?, threshold=?, comparison=?, duration_min=?,
			     updated_at=CURRENT_TIMESTAMP
			 WHERE id=?`),
			body.Name, body.MetricField, body.GroupField, body.IndexPattern,
			body.Threshold, body.Comparison, body.DurationMin, ruleID,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		var rule InfraAlertRule
		var enabledInt int
		if err := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, conn_id, name, metric_field, group_field, index_pattern,
			        threshold, comparison, duration_min, enabled, created_by, created_at, updated_at
			 FROM infra_alert_rules WHERE id = ?`), ruleID).Scan(
			&rule.ID, &rule.ConnID, &rule.Name, &rule.MetricField, &rule.GroupField,
			&rule.IndexPattern, &rule.Threshold, &rule.Comparison, &rule.DurationMin,
			&enabledInt, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			http.Error(w, jsonError("rule not found"), http.StatusNotFound)
			return
		}
		rule.Enabled = enabledInt == 1

		json.NewEncoder(w).Encode(rule)
	}
}

// DeleteInfraAlertRule deletes an alert rule.
// DELETE /api/connections/{id}/infra-alert-rules/{ruleID}
func DeleteInfraAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ruleID, err := infraRuleIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid rule id"), http.StatusBadRequest)
			return
		}

		res, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM infra_alert_rules WHERE id = ?`), ruleID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.Error(w, jsonError("rule not found"), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ToggleInfraAlertRule enables or disables an alert rule.
// PATCH /api/connections/{id}/infra-alert-rules/{ruleID}/toggle
func ToggleInfraAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ruleID, err := infraRuleIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid rule id"), http.StatusBadRequest)
			return
		}

		_, err = appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE infra_alert_rules SET enabled = CASE WHEN enabled=1 THEN 0 ELSE 1 END,
			 updated_at=CURRENT_TIMESTAMP WHERE id=?`), ruleID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		var rule InfraAlertRule
		var enabledInt int
		if err := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, conn_id, name, metric_field, group_field, index_pattern,
			        threshold, comparison, duration_min, enabled, created_by, created_at, updated_at
			 FROM infra_alert_rules WHERE id = ?`), ruleID).Scan(
			&rule.ID, &rule.ConnID, &rule.Name, &rule.MetricField, &rule.GroupField,
			&rule.IndexPattern, &rule.Threshold, &rule.Comparison, &rule.DurationMin,
			&enabledInt, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			http.Error(w, jsonError("rule not found"), http.StatusNotFound)
			return
		}
		rule.Enabled = enabledInt == 1

		json.NewEncoder(w).Encode(rule)
	}
}

// infraRuleIDFromPath extracts the alert rule ID from a path like
// /api/connections/{connID}/infra-alert-rules/{ruleID}[/toggle]
func infraRuleIDFromPath(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/connections/")
	parts := strings.Split(trimmed, "/")
	// parts[0] = connID, parts[1] = "infra-alert-rules", parts[2] = ruleID
	if len(parts) < 3 {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseInt(parts[2], 10, 64)
}
