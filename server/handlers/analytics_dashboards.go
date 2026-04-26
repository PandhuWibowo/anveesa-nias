package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type AnalyticsDashboard struct {
	ID            int64                           `json:"id"`
	Name          string                          `json:"name"`
	Description   string                          `json:"description"`
	Visibility    string                          `json:"visibility"`
	ShareToken    string                          `json:"share_token,omitempty"`
	DefaultPreset string                          `json:"default_preset"`
	Presets       []AnalyticsDashboardViewPreset  `json:"presets,omitempty"`
	Access        []AnalyticsDashboardAccessEntry `json:"access,omitempty"`
	CreatedAt     string                          `json:"created_at"`
	UpdatedAt     string                          `json:"updated_at"`
	Blocks        []AnalyticsDashboardBlock       `json:"blocks,omitempty"`
}

type AnalyticsDashboardViewPreset struct {
	Name         string            `json:"name"`
	GlobalFilter string            `json:"global_filter"`
	Params       map[string]string `json:"params"`
}

type AnalyticsDashboardAccessEntry struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	AccessLevel string `json:"access_level"`
}

type AnalyticsDashboardBlock struct {
	ID           int64                          `json:"id"`
	DashboardID  int64                          `json:"dashboard_id"`
	SavedQueryID int64                          `json:"saved_query_id"`
	Title        string                         `json:"title"`
	ChartType    string                         `json:"chart_type"`
	XKey         string                         `json:"x_key"`
	YKey         string                         `json:"y_key"`
	ColumnSpan   int                            `json:"column_span"`
	RowSpan      int                            `json:"row_span"`
	Params       []AnalyticsDashboardBlockParam `json:"params,omitempty"`
	SortOrder    int                            `json:"sort_order"`
}

type AnalyticsDashboardBlockParam struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Type         string `json:"type"`
	DefaultValue string `json:"default_value"`
}

type AnalyticsDashboardRenderBlock struct {
	AnalyticsDashboardBlock
	ConnectionID int64           `json:"connection_id"`
	QueryName    string          `json:"query_name"`
	Description  string          `json:"description"`
	SQL          string          `json:"sql"`
	Columns      []string        `json:"columns"`
	Rows         [][]interface{} `json:"rows"`
	RowCount     int             `json:"row_count"`
	DurationMs   int64           `json:"duration_ms"`
	Error        string          `json:"error"`
}

type AnalyticsDashboardRender struct {
	ID          int64                           `json:"id"`
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Params      []AnalyticsDashboardParameter   `json:"params"`
	Blocks      []AnalyticsDashboardRenderBlock `json:"blocks"`
}

type AnalyticsDashboardParameter struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

func ListAnalyticsDashboards() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := queryAnalyticsDashboardsForRequest(r)
		if err != nil {
			http.Error(w, jsonError("failed to list dashboards"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := []AnalyticsDashboard{}
		for rows.Next() {
			var item AnalyticsDashboard
			if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Visibility, &item.DefaultPreset, &item.CreatedAt, &item.UpdatedAt); err != nil {
				http.Error(w, jsonError("failed to read dashboards"), http.StatusInternalServerError)
				return
			}
			items = append(items, item)
		}
		json.NewEncoder(w).Encode(items)
	}
}

func CreateAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		id, err := insertRowReturningID(appdb.ConvertQuery(`
			INSERT INTO analytics_dashboards (name, description, user_id, visibility, share_token, presets_json, default_preset, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`), strings.TrimSpace(body.Name), strings.TrimSpace(body.Description), nullableUserID(userID), "private", "", "[]", "", now, now)
		if err != nil {
			http.Error(w, jsonError("failed to create dashboard"), http.StatusInternalServerError)
			return
		}
		item, _ := getAnalyticsDashboardByID(id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(item)
	}
}

func GetAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/")
		if err != nil {
			http.Error(w, jsonError("invalid dashboard id"), http.StatusBadRequest)
			return
		}
		item, err := getAnalyticsDashboardByID(id)
		if err != nil || item == nil || !canAccessAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("dashboard not found"), http.StatusNotFound)
			return
		}
		blocks, _ := listAnalyticsDashboardBlocks(id)
		item.Blocks = blocks
		json.NewEncoder(w).Encode(item)
	}
}

func UpdateAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/")
		if err != nil {
			http.Error(w, jsonError("invalid dashboard id"), http.StatusBadRequest)
			return
		}
		if !canManageAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		var body struct {
			Name          string                          `json:"name"`
			Description   string                          `json:"description"`
			Visibility    string                          `json:"visibility"`
			ShareToken    string                          `json:"share_token"`
			DefaultPreset string                          `json:"default_preset"`
			Presets       []AnalyticsDashboardViewPreset  `json:"presets"`
			Access        []AnalyticsDashboardAccessEntry `json:"access"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, jsonError("name required"), http.StatusBadRequest)
			return
		}
		if !canManageAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		visibility := normalizeDashboardVisibility(body.Visibility)
		presets := normalizeDashboardViewPresets(body.Presets)
		accessEntries := normalizeDashboardAccessEntries(body.Access)
		defaultPreset := strings.TrimSpace(body.DefaultPreset)
		if defaultPreset != "" && !dashboardPresetExists(presets, defaultPreset) {
			defaultPreset = ""
		}
		item, err := getAnalyticsDashboardByID(id)
		if err != nil || item == nil {
			http.Error(w, jsonError("dashboard not found"), http.StatusNotFound)
			return
		}
		shareToken := strings.TrimSpace(body.ShareToken)
		if shareToken == "" {
			shareToken = item.ShareToken
		}
		if visibility == "public" && shareToken == "" {
			shareToken, err = generateDashboardShareToken()
			if err != nil {
				http.Error(w, jsonError("failed to generate share token"), http.StatusInternalServerError)
				return
			}
		}
		if visibility != "public" {
			shareToken = ""
		}
		presetsJSON, err := json.Marshal(presets)
		if err != nil {
			http.Error(w, jsonError("failed to encode dashboard presets"), http.StatusInternalServerError)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE analytics_dashboards
			SET name = ?, description = ?, visibility = ?, share_token = ?, presets_json = ?, default_preset = ?, updated_at = ?
			WHERE id = ?
		`), strings.TrimSpace(body.Name), strings.TrimSpace(body.Description), visibility, shareToken, string(presetsJSON), defaultPreset, time.Now().UTC().Format("2006-01-02 15:04:05"), id)
		if err != nil {
			http.Error(w, jsonError("failed to update dashboard"), http.StatusInternalServerError)
			return
		}
		if err := replaceAnalyticsDashboardAccess(id, accessEntries); err != nil {
			http.Error(w, jsonError("failed to update dashboard access"), http.StatusInternalServerError)
			return
		}
		item, _ = getAnalyticsDashboardByID(id)
		json.NewEncoder(w).Encode(item)
	}
}

func ListAnalyticsDashboardUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT u.id, u.username, COALESCE(r.name, u.role) AS role
			FROM users u
			LEFT JOIN roles r ON r.id = u.role_id
			WHERE COALESCE(u.is_active, 1) = 1
			ORDER BY u.username ASC, u.id ASC
		`))
		if err != nil {
			http.Error(w, jsonError("failed to list users"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id int64
			var username, role string
			if err := rows.Scan(&id, &username, &role); err != nil {
				http.Error(w, jsonError("failed to read users"), http.StatusInternalServerError)
				return
			}
			items = append(items, map[string]any{
				"id":       id,
				"username": username,
				"role":     role,
			})
		}
		json.NewEncoder(w).Encode(items)
	}
}

func RenderSharedAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/analytics-dashboards/shared/"))
		if token == "" {
			http.Error(w, jsonError("invalid dashboard share token"), http.StatusBadRequest)
			return
		}
		item, err := getAnalyticsDashboardByShareToken(token)
		if err != nil || item == nil || item.Visibility != "public" {
			http.Error(w, jsonError("shared dashboard not found"), http.StatusNotFound)
			return
		}
		rendered, err := renderAnalyticsDashboardForHeaders("", "admin", item.ID, dashboardParamsFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(rendered)
	}
}

func DeleteAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/")
		if err != nil {
			http.Error(w, jsonError("invalid dashboard id"), http.StatusBadRequest)
			return
		}
		if !canManageAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM analytics_dashboard_blocks WHERE dashboard_id = ?`), id)
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM analytics_dashboards WHERE id = ?`), id)
		if err != nil {
			http.Error(w, jsonError("failed to delete dashboard"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func PreviewAnalyticsDashboardQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			ConnID int64  `json:"conn_id"`
			SQL    string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ConnID <= 0 || strings.TrimSpace(body.SQL) == "" {
			http.Error(w, jsonError("connection and sql required"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, body.ConnID) {
			http.Error(w, jsonError("read permission denied"), http.StatusForbidden)
			return
		}
		sqlText := normalizeAnalyticsSQL(body.SQL)
		if err := validateAnalyticsSQL(sqlText); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dbConn, _, err := GetDB(body.ConnID)
		if err != nil {
			http.Error(w, jsonError("database connection error"), http.StatusBadGateway)
			return
		}
		result, err := executeAnalyticsQuery(r.Context(), dbConn, sqlText)
		if err != nil {
			http.Error(w, jsonError(sanitizeDBError(err)), http.StatusBadRequest)
			return
		}
		if len(result.Rows) > 100 {
			result.Rows = result.Rows[:100]
		}
		result.RowCount = len(result.Rows)
		json.NewEncoder(w).Encode(result)
	}
}

func CreateAnalyticsDashboardBlock() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePathActionID(r.URL.Path, "/api/analytics-dashboards/", "/blocks")
		if err != nil {
			http.Error(w, jsonError("invalid dashboard id"), http.StatusBadRequest)
			return
		}
		if !canManageAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		var body AnalyticsDashboardBlock
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SavedQueryID <= 0 {
			http.Error(w, jsonError("saved_query_id required"), http.StatusBadRequest)
			return
		}
		savedQuery, err := getSavedQueryByID(body.SavedQueryID)
		if err != nil || savedQuery == nil || !canAccessSavedQuery(r, body.SavedQueryID) {
			http.Error(w, jsonError("saved query not found"), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Title) == "" {
			body.Title = savedQuery.Name
		}
		body.ChartType = normalizeDashboardChartType(body.ChartType)
		columnSpan := normalizeDashboardColumnSpan(body.ColumnSpan)
		if body.ChartType == "kpi" && body.ColumnSpan == 0 {
			columnSpan = 1
		}
		nextOrder := nextDashboardBlockOrder(id)
		blockParams := normalizeDashboardBlockParams(body.Params, extractDashboardSQLParameters(savedQuery.SQL), nil)
		paramsJSON, err := json.Marshal(blockParams)
		if err != nil {
			http.Error(w, jsonError("failed to encode dashboard block params"), http.StatusInternalServerError)
			return
		}
		blockID, err := insertRowReturningID(appdb.ConvertQuery(`
			INSERT INTO analytics_dashboard_blocks (dashboard_id, saved_query_id, title, chart_type, x_key, y_key, column_span, row_span, params_json, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`), id, body.SavedQueryID, strings.TrimSpace(body.Title), body.ChartType, strings.TrimSpace(body.XKey), strings.TrimSpace(body.YKey), columnSpan, normalizeDashboardRowSpan(body.RowSpan), string(paramsJSON), nextOrder, time.Now().UTC().Format("2006-01-02 15:04:05"), time.Now().UTC().Format("2006-01-02 15:04:05"))
		if err != nil {
			http.Error(w, jsonError("failed to create dashboard block"), http.StatusInternalServerError)
			return
		}
		block, _ := getAnalyticsDashboardBlockByID(blockID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(block)
	}
}

func UpdateAnalyticsDashboardBlock() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/blocks/")
		if err != nil {
			http.Error(w, jsonError("invalid block id"), http.StatusBadRequest)
			return
		}
		block, err := getAnalyticsDashboardBlockByID(id)
		if err != nil || block == nil || !canManageAnalyticsDashboard(r, block.DashboardID) {
			http.Error(w, jsonError("block not found"), http.StatusNotFound)
			return
		}
		var body AnalyticsDashboardBlock
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		savedQuery, err := getSavedQueryByID(block.SavedQueryID)
		if err != nil || savedQuery == nil || !canAccessSavedQuery(r, block.SavedQueryID) {
			http.Error(w, jsonError("saved query not found"), http.StatusBadRequest)
			return
		}
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = block.Title
		}
		chartType := normalizeDashboardChartType(firstNonEmptyString(body.ChartType, block.ChartType))
		xKey := strings.TrimSpace(firstNonEmptyString(body.XKey, block.XKey))
		yKey := strings.TrimSpace(firstNonEmptyString(body.YKey, block.YKey))
		columnSpan := block.ColumnSpan
		if body.ColumnSpan > 0 {
			columnSpan = normalizeDashboardColumnSpan(body.ColumnSpan)
		}
		rowSpan := block.RowSpan
		if body.RowSpan > 0 {
			rowSpan = normalizeDashboardRowSpan(body.RowSpan)
		}
		blockParams := normalizeDashboardBlockParams(body.Params, extractDashboardSQLParameters(savedQuery.SQL), block.Params)
		paramsJSON, err := json.Marshal(blockParams)
		if err != nil {
			http.Error(w, jsonError("failed to encode dashboard block params"), http.StatusInternalServerError)
			return
		}
		sortOrder := block.SortOrder
		if body.SortOrder > 0 {
			sortOrder = body.SortOrder
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE analytics_dashboard_blocks
			SET title = ?, chart_type = ?, x_key = ?, y_key = ?, column_span = ?, row_span = ?, params_json = ?, sort_order = ?, updated_at = ?
			WHERE id = ?
		`), title, chartType, xKey, yKey, columnSpan, rowSpan, string(paramsJSON), sortOrder, time.Now().UTC().Format("2006-01-02 15:04:05"), id)
		if err != nil {
			http.Error(w, jsonError("failed to update block"), http.StatusInternalServerError)
			return
		}
		updated, _ := getAnalyticsDashboardBlockByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func DeleteAnalyticsDashboardBlock() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/blocks/")
		if err != nil {
			http.Error(w, jsonError("invalid block id"), http.StatusBadRequest)
			return
		}
		block, err := getAnalyticsDashboardBlockByID(id)
		if err != nil || block == nil || !canManageAnalyticsDashboard(r, block.DashboardID) {
			http.Error(w, jsonError("block not found"), http.StatusNotFound)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM analytics_dashboard_blocks WHERE id = ?`), id)
		if err != nil {
			http.Error(w, jsonError("failed to delete block"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func RenderAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePathActionID(r.URL.Path, "/api/analytics-dashboards/", "/render")
		if err != nil {
			http.Error(w, jsonError("invalid dashboard id"), http.StatusBadRequest)
			return
		}
		item, err := getAnalyticsDashboardByID(id)
		if err != nil || item == nil || !canAccessAnalyticsDashboard(r, id) {
			http.Error(w, jsonError("dashboard not found"), http.StatusNotFound)
			return
		}
		rendered, err := renderAnalyticsDashboardForHeaders(r.Header.Get("X-User-ID"), r.Header.Get("X-User-Role"), id, dashboardParamsFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(rendered)
	}
}

var dashboardParamPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
var dashboardParamNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func dashboardParamsFromRequest(r *http.Request) map[string]string {
	params := map[string]string{}
	for key, values := range r.URL.Query() {
		if !strings.HasPrefix(key, "param_") || len(values) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "param_"))
		if name == "" {
			continue
		}
		params[name] = values[0]
	}
	return params
}

func extractDashboardSQLParameters(sqlText string) []string {
	matches := dashboardParamPattern.FindAllStringSubmatch(sqlText, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func bindDashboardSQLParameters(sqlText string, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	var bindErr error
	bound := dashboardParamPattern.ReplaceAllStringFunc(sqlText, func(match string) string {
		sub := dashboardParamPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := strings.TrimSpace(sub[1])
		value, ok := params[name]
		if !ok {
			return "NULL"
		}
		return sqlLiteralFromDashboardParam(value)
	})
	if bindErr != nil {
		return "", bindErr
	}
	return bound, nil
}

func sqlLiteralFromDashboardParam(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "NULL"
	}
	lower := strings.ToLower(trimmed)
	if lower == "true" || lower == "false" {
		return strings.ToUpper(lower)
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return trimmed
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return trimmed
	}
	return "'" + strings.ReplaceAll(trimmed, "'", "''") + "'"
}

func inferDashboardParamType(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "date"), strings.Contains(lower, "time"):
		return "date"
	case strings.HasSuffix(lower, "_id"), strings.Contains(lower, "count"), strings.Contains(lower, "limit"), strings.Contains(lower, "days"):
		return "number"
	default:
		return "text"
	}
}

func renderAnalyticsDashboardForUser(userID, dashboardID int64, params map[string]string) (AnalyticsDashboardRender, error) {
	role := ""
	_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(role,'') FROM users WHERE id = ?`), userID).Scan(&role)
	return renderAnalyticsDashboardForHeaders(fmt.Sprintf("%d", userID), role, dashboardID, params)
}

func renderAnalyticsDashboardForHeaders(userIDStr, role string, dashboardID int64, params map[string]string) (AnalyticsDashboardRender, error) {
	item, err := getAnalyticsDashboardByID(dashboardID)
	if err != nil || item == nil {
		return AnalyticsDashboardRender{}, fmt.Errorf("dashboard not found")
	}
	headers := map[string]string{
		"X-User-ID":   strings.TrimSpace(userIDStr),
		"X-User-Role": strings.TrimSpace(role),
	}
	blocks, err := listAnalyticsDashboardBlocks(dashboardID)
	if err != nil {
		return AnalyticsDashboardRender{}, err
	}
	rendered := AnalyticsDashboardRender{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		Params:      []AnalyticsDashboardParameter{},
		Blocks:      []AnalyticsDashboardRenderBlock{},
	}
	defaultParamValues := map[string]string{}
	if item.DefaultPreset != "" {
		for _, preset := range item.Presets {
			if preset.Name != item.DefaultPreset {
				continue
			}
			for key, value := range preset.Params {
				defaultParamValues[key] = strings.TrimSpace(value)
			}
			break
		}
	}
	paramSet := map[string]bool{}
	for _, block := range blocks {
		renderBlock := AnalyticsDashboardRenderBlock{AnalyticsDashboardBlock: block}
		savedQuery, queryErr := getSavedQueryByID(block.SavedQueryID)
		if queryErr != nil || savedQuery == nil || !canAccessSavedQueryByHeaders(headers["X-User-ID"], headers["X-User-Role"], block.SavedQueryID) {
			renderBlock.Error = "saved query not available"
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		renderBlock.QueryName = savedQuery.Name
		renderBlock.Description = savedQuery.Description
		renderBlock.SQL = savedQuery.SQL
		if savedQuery.ConnID == nil || *savedQuery.ConnID <= 0 {
			renderBlock.Error = "saved query is not tied to a connection"
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		renderBlock.ConnectionID = *savedQuery.ConnID
		configuredParams := normalizeDashboardBlockParams(block.Params, extractDashboardSQLParameters(savedQuery.SQL), block.Params)
		effectiveParams := map[string]string{}
		for _, param := range configuredParams {
			value := strings.TrimSpace(param.DefaultValue)
			if presetValue, ok := defaultParamValues[param.Name]; ok {
				value = strings.TrimSpace(presetValue)
			}
			if requestValue, ok := params[param.Name]; ok {
				value = strings.TrimSpace(requestValue)
			}
			effectiveParams[param.Name] = value
			if paramSet[param.Name] {
				continue
			}
			paramSet[param.Name] = true
			rendered.Params = append(rendered.Params, AnalyticsDashboardParameter{
				Name:  param.Name,
				Label: param.Label,
				Type:  param.Type,
				Value: value,
			})
		}
		if !checkReadPermissionByHeaders(headers["X-User-ID"], headers["X-User-Role"], renderBlock.ConnectionID) {
			renderBlock.Error = "read permission denied"
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		dbConn, _, err := GetDB(renderBlock.ConnectionID)
		if err != nil {
			renderBlock.Error = "database connection error"
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		sqlText, bindErr := bindDashboardSQLParameters(savedQuery.SQL, effectiveParams)
		if bindErr != nil {
			renderBlock.Error = bindErr.Error()
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		sqlText = normalizeAnalyticsSQL(sqlText)
		if err := validateAnalyticsSQL(sqlText); err != nil {
			renderBlock.Error = err.Error()
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		result, err := executeAnalyticsQuery(context.Background(), dbConn, sqlText)
		if err != nil {
			renderBlock.Error = sanitizeDBError(err)
			rendered.Blocks = append(rendered.Blocks, renderBlock)
			continue
		}
		renderBlock.Columns = result.Columns
		renderBlock.Rows = result.Rows
		renderBlock.RowCount = result.RowCount
		renderBlock.DurationMs = result.DurationMs
		rendered.Blocks = append(rendered.Blocks, renderBlock)
	}
	return rendered, nil
}

func queryAnalyticsDashboardsForRequest(r *http.Request) (*sql.Rows, error) {
	if !isAuthEnabled() || r.Header.Get("X-User-Role") == "admin" {
		return appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, name, COALESCE(description,''), COALESCE(visibility,'private'), COALESCE(default_preset,''), created_at, updated_at
			FROM analytics_dashboards
			ORDER BY updated_at DESC, id DESC
		`))
	}
	userID, _, _ := currentUserFromHeaders(r)
	return appdb.DB.Query(appdb.ConvertQuery(`
		SELECT DISTINCT d.id, d.name, COALESCE(d.description,''), COALESCE(d.visibility,'private'), COALESCE(d.default_preset,''), d.created_at, d.updated_at
		FROM analytics_dashboards d
		LEFT JOIN analytics_dashboard_access a ON a.dashboard_id = d.id AND a.user_id = ?
		WHERE d.user_id = ? OR COALESCE(d.visibility,'private') IN ('shared','public') OR a.user_id IS NOT NULL
		ORDER BY updated_at DESC, id DESC
	`), userID, userID)
}

func canAccessAnalyticsDashboard(r *http.Request, id int64) bool {
	if !isAuthEnabled() || r.Header.Get("X-User-Role") == "admin" {
		return true
	}
	userID, _, _ := currentUserFromHeaders(r)
	var ownerID sql.NullInt64
	var visibility string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT user_id, COALESCE(visibility,'private') FROM analytics_dashboards WHERE id = ?`), id).Scan(&ownerID, &visibility)
	if err != nil {
		return false
	}
	if !ownerID.Valid || ownerID.Int64 == userID || visibility == "shared" || visibility == "public" {
		return true
	}
	level, err := getAnalyticsDashboardAccessLevel(id, userID)
	return err == nil && (level == "viewer" || level == "editor")
}

func canManageAnalyticsDashboard(r *http.Request, id int64) bool {
	if !isAuthEnabled() || r.Header.Get("X-User-Role") == "admin" {
		return true
	}
	userID, _, _ := currentUserFromHeaders(r)
	var ownerID sql.NullInt64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT user_id FROM analytics_dashboards WHERE id = ?`), id).Scan(&ownerID)
	if err != nil {
		return false
	}
	if !ownerID.Valid || ownerID.Int64 == userID {
		return true
	}
	level, err := getAnalyticsDashboardAccessLevel(id, userID)
	return err == nil && level == "editor"
}

func canAccessSavedQuery(r *http.Request, id int64) bool {
	if !isAuthEnabled() || r.Header.Get("X-User-Role") == "admin" {
		return true
	}
	userID, _, _ := currentUserFromHeaders(r)
	return canAccessSavedQueryByHeaders(fmt.Sprintf("%d", userID), r.Header.Get("X-User-Role"), id)
}

func canAccessSavedQueryByHeaders(userIDStr, role string, id int64) bool {
	if !isAuthEnabled() || strings.TrimSpace(role) == "admin" {
		return true
	}
	userID, _ := strconv.ParseInt(strings.TrimSpace(userIDStr), 10, 64)
	var ownerID sql.NullInt64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT user_id FROM saved_queries WHERE id = ?`), id).Scan(&ownerID)
	if err != nil {
		return false
	}
	return !ownerID.Valid || ownerID.Int64 == userID
}

func checkReadPermissionByHeaders(userIDStr, role string, connID int64) bool {
	if !isAuthEnabled() {
		return true
	}
	if strings.TrimSpace(role) == "admin" {
		return true
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(userIDStr), 10, 64)
	if err != nil || userID == 0 {
		return false
	}
	var ownerID sql.NullInt64
	err = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT owner_id FROM connections WHERE id = ?`), connID).Scan(&ownerID)
	if err == nil && ownerID.Valid && ownerID.Int64 == userID {
		return true
	}
	if err == nil && !ownerID.Valid {
		return true
	}
	perms, err := appdb.GetUserConnectionPermissions(userID, role, connID)
	if err != nil || len(perms) == 0 {
		return err != nil || len(perms) == 0
	}
	for _, p := range perms {
		if string(p) == "select" {
			return true
		}
	}
	return false
}

func getAnalyticsDashboardByID(id int64) (*AnalyticsDashboard, error) {
	var item AnalyticsDashboard
	var presetsJSON string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, COALESCE(description,''), COALESCE(visibility,'private'), COALESCE(share_token,''), COALESCE(default_preset,''), COALESCE(presets_json,'[]'), created_at, updated_at
		FROM analytics_dashboards
		WHERE id = ?
	`), id).Scan(&item.ID, &item.Name, &item.Description, &item.Visibility, &item.ShareToken, &item.DefaultPreset, &presetsJSON, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Presets = decodeDashboardViewPresets(presetsJSON)
	item.Access = listAnalyticsDashboardAccess(id)
	if item.DefaultPreset != "" && !dashboardPresetExists(item.Presets, item.DefaultPreset) {
		item.DefaultPreset = ""
	}
	return &item, nil
}

func getAnalyticsDashboardByShareToken(token string) (*AnalyticsDashboard, error) {
	var item AnalyticsDashboard
	var presetsJSON string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, COALESCE(description,''), COALESCE(visibility,'private'), COALESCE(share_token,''), COALESCE(default_preset,''), COALESCE(presets_json,'[]'), created_at, updated_at
		FROM analytics_dashboards
		WHERE share_token = ?
	`), strings.TrimSpace(token)).Scan(&item.ID, &item.Name, &item.Description, &item.Visibility, &item.ShareToken, &item.DefaultPreset, &presetsJSON, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Presets = decodeDashboardViewPresets(presetsJSON)
	item.Access = listAnalyticsDashboardAccess(item.ID)
	if item.DefaultPreset != "" && !dashboardPresetExists(item.Presets, item.DefaultPreset) {
		item.DefaultPreset = ""
	}
	return &item, nil
}

func getAnalyticsDashboardAccessLevel(dashboardID, userID int64) (string, error) {
	var level string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT COALESCE(access_level,'')
		FROM analytics_dashboard_access
		WHERE dashboard_id = ? AND user_id = ?
	`), dashboardID, userID).Scan(&level)
	if err != nil {
		return "", err
	}
	return normalizeDashboardAccessLevel(level), nil
}

func listAnalyticsDashboardAccess(dashboardID int64) []AnalyticsDashboardAccessEntry {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT a.user_id, COALESCE(u.username,''), COALESCE(a.access_level,'viewer')
		FROM analytics_dashboard_access a
		LEFT JOIN users u ON u.id = a.user_id
		WHERE a.dashboard_id = ?
		ORDER BY COALESCE(u.username,''), a.user_id
	`), dashboardID)
	if err != nil {
		return []AnalyticsDashboardAccessEntry{}
	}
	defer rows.Close()
	items := []AnalyticsDashboardAccessEntry{}
	for rows.Next() {
		var item AnalyticsDashboardAccessEntry
		if err := rows.Scan(&item.UserID, &item.Username, &item.AccessLevel); err != nil {
			return []AnalyticsDashboardAccessEntry{}
		}
		item.AccessLevel = normalizeDashboardAccessLevel(item.AccessLevel)
		items = append(items, item)
	}
	return items
}

func replaceAnalyticsDashboardAccess(dashboardID int64, entries []AnalyticsDashboardAccessEntry) error {
	if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM analytics_dashboard_access WHERE dashboard_id = ?`), dashboardID); err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, entry := range entries {
		if entry.UserID <= 0 {
			continue
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO analytics_dashboard_access (dashboard_id, user_id, access_level, created_at)
			VALUES (?, ?, ?, ?)
		`), dashboardID, entry.UserID, normalizeDashboardAccessLevel(entry.AccessLevel), now); err != nil {
			return err
		}
	}
	return nil
}

func decodeDashboardViewPresets(raw string) []AnalyticsDashboardViewPreset {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []AnalyticsDashboardViewPreset{}
	}
	var presets []AnalyticsDashboardViewPreset
	if err := json.Unmarshal([]byte(raw), &presets); err != nil {
		return []AnalyticsDashboardViewPreset{}
	}
	return normalizeDashboardViewPresets(presets)
}

func normalizeDashboardViewPresets(input []AnalyticsDashboardViewPreset) []AnalyticsDashboardViewPreset {
	seen := map[string]bool{}
	out := make([]AnalyticsDashboardViewPreset, 0, len(input))
	for _, preset := range input {
		name := strings.TrimSpace(preset.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		params := map[string]string{}
		for key, value := range preset.Params {
			paramName := normalizeDashboardParamName(key)
			if paramName == "" {
				continue
			}
			params[paramName] = strings.TrimSpace(value)
		}
		out = append(out, AnalyticsDashboardViewPreset{
			Name:         name,
			GlobalFilter: strings.TrimSpace(preset.GlobalFilter),
			Params:       params,
		})
	}
	return out
}

func dashboardPresetExists(presets []AnalyticsDashboardViewPreset, name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	for _, preset := range presets {
		if strings.TrimSpace(preset.Name) == trimmed {
			return true
		}
	}
	return false
}

func normalizeDashboardAccessEntries(input []AnalyticsDashboardAccessEntry) []AnalyticsDashboardAccessEntry {
	seen := map[int64]bool{}
	out := make([]AnalyticsDashboardAccessEntry, 0, len(input))
	for _, entry := range input {
		if entry.UserID <= 0 || seen[entry.UserID] {
			continue
		}
		seen[entry.UserID] = true
		out = append(out, AnalyticsDashboardAccessEntry{
			UserID:      entry.UserID,
			Username:    strings.TrimSpace(entry.Username),
			AccessLevel: normalizeDashboardAccessLevel(entry.AccessLevel),
		})
	}
	return out
}

func normalizeDashboardAccessLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "editor":
		return "editor"
	default:
		return "viewer"
	}
}

func normalizeDashboardVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "shared", "public":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "private"
	}
}

func generateDashboardShareToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func listAnalyticsDashboardBlocks(dashboardID int64) ([]AnalyticsDashboardBlock, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, dashboard_id, saved_query_id, COALESCE(title,''), COALESCE(chart_type,'table'), COALESCE(x_key,''), COALESCE(y_key,''), COALESCE(column_span,1), COALESCE(row_span,2), COALESCE(params_json,'[]'), COALESCE(sort_order,0)
		FROM analytics_dashboard_blocks
		WHERE dashboard_id = ?
		ORDER BY sort_order ASC, id ASC
	`), dashboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AnalyticsDashboardBlock{}
	for rows.Next() {
		var item AnalyticsDashboardBlock
		var paramsJSON string
		if err := rows.Scan(&item.ID, &item.DashboardID, &item.SavedQueryID, &item.Title, &item.ChartType, &item.XKey, &item.YKey, &item.ColumnSpan, &item.RowSpan, &paramsJSON, &item.SortOrder); err != nil {
			return nil, err
		}
		item.ColumnSpan = normalizeDashboardColumnSpan(item.ColumnSpan)
		item.RowSpan = normalizeDashboardRowSpan(item.RowSpan)
		item.Params = decodeDashboardBlockParams(paramsJSON)
		items = append(items, item)
	}
	return items, nil
}

func getAnalyticsDashboardBlockByID(id int64) (*AnalyticsDashboardBlock, error) {
	var item AnalyticsDashboardBlock
	var paramsJSON string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, dashboard_id, saved_query_id, COALESCE(title,''), COALESCE(chart_type,'table'), COALESCE(x_key,''), COALESCE(y_key,''), COALESCE(column_span,1), COALESCE(row_span,2), COALESCE(params_json,'[]'), COALESCE(sort_order,0)
		FROM analytics_dashboard_blocks
		WHERE id = ?
	`), id).Scan(&item.ID, &item.DashboardID, &item.SavedQueryID, &item.Title, &item.ChartType, &item.XKey, &item.YKey, &item.ColumnSpan, &item.RowSpan, &paramsJSON, &item.SortOrder)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.ColumnSpan = normalizeDashboardColumnSpan(item.ColumnSpan)
	item.RowSpan = normalizeDashboardRowSpan(item.RowSpan)
	item.Params = decodeDashboardBlockParams(paramsJSON)
	return &item, nil
}

func decodeDashboardBlockParams(raw string) []AnalyticsDashboardBlockParam {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []AnalyticsDashboardBlockParam{}
	}
	var params []AnalyticsDashboardBlockParam
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return []AnalyticsDashboardBlockParam{}
	}
	return normalizeDashboardBlockParams(params, nil, nil)
}

func normalizeDashboardBlockParams(input []AnalyticsDashboardBlockParam, discovered []string, existing []AnalyticsDashboardBlockParam) []AnalyticsDashboardBlockParam {
	seen := map[string]bool{}
	byName := map[string]AnalyticsDashboardBlockParam{}
	for _, item := range existing {
		name := normalizeDashboardParamName(item.Name)
		if name == "" {
			continue
		}
		item.Name = name
		byName[name] = item
	}
	for _, item := range input {
		name := normalizeDashboardParamName(item.Name)
		if name == "" {
			continue
		}
		item.Name = name
		byName[name] = mergeDashboardBlockParam(byName[name], item)
	}
	out := make([]AnalyticsDashboardBlockParam, 0, len(discovered)+len(byName))
	for _, name := range discovered {
		normalized := normalizeDashboardParamName(name)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, finalizeDashboardBlockParam(normalized, byName[normalized]))
	}
	for name, item := range byName {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, finalizeDashboardBlockParam(name, item))
	}
	return out
}

func mergeDashboardBlockParam(base, override AnalyticsDashboardBlockParam) AnalyticsDashboardBlockParam {
	if strings.TrimSpace(override.Label) != "" {
		base.Label = override.Label
	}
	if strings.TrimSpace(override.Type) != "" {
		base.Type = override.Type
	}
	if override.DefaultValue != "" || base.DefaultValue == "" {
		base.DefaultValue = override.DefaultValue
	}
	if strings.TrimSpace(base.Name) == "" {
		base.Name = strings.TrimSpace(override.Name)
	}
	return base
}

func finalizeDashboardBlockParam(name string, item AnalyticsDashboardBlockParam) AnalyticsDashboardBlockParam {
	item.Name = name
	item.Label = strings.TrimSpace(item.Label)
	if item.Label == "" {
		item.Label = strings.ReplaceAll(strings.Title(strings.ReplaceAll(name, "_", " ")), "  ", " ")
	}
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "number", "date", "text":
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
	default:
		item.Type = inferDashboardParamType(name)
	}
	item.DefaultValue = strings.TrimSpace(item.DefaultValue)
	return item
}

func normalizeDashboardParamName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if !dashboardParamNamePattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

func getSavedQueryByID(id int64) (*SavedQuery, error) {
	var item SavedQuery
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
		FROM saved_queries
		WHERE id = ?
	`), id).Scan(&item.ID, &item.Name, &item.ConnID, &item.SQL, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func nextDashboardBlockOrder(dashboardID int64) int {
	var next int
	_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM analytics_dashboard_blocks WHERE dashboard_id = ?`), dashboardID).Scan(&next)
	if next <= 0 {
		return 1
	}
	return next
}

func normalizeDashboardChartType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bar", "horizontal-bar", "line", "area", "scatter", "pie", "donut", "kpi", "table":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "table"
	}
}

func normalizeDashboardColumnSpan(value int) int {
	switch value {
	case 2, 3:
		return value
	default:
		return 1
	}
}

func normalizeDashboardRowSpan(value int) int {
	switch value {
	case 1, 3:
		return value
	default:
		return 2
	}
}

func nullableUserID(userID int64) any {
	if userID <= 0 {
		return nil
	}
	return userID
}
