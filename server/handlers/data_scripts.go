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

type DataScript struct {
	ID              int64              `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Language        string             `json:"language"`
	CreatedBy       int64              `json:"created_by"`
	LatestVersionID int64              `json:"latest_version_id"`
	LatestVersionNo int                `json:"latest_version_no"`
	LatestSource    string             `json:"latest_source"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Plans           []DataChangePlan   `json:"plans,omitempty"`
}

type DataScriptVersion struct {
	ID          int64     `json:"id"`
	ScriptID    int64     `json:"script_id"`
	VersionNo   int       `json:"version_no"`
	SourceCode  string    `json:"source_code"`
	ParamsSchema string   `json:"params_schema"`
	SDKVersion  string    `json:"sdk_version"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type DataChangePlan struct {
	ID              int64                `json:"id"`
	ScriptID        int64                `json:"script_id"`
	ScriptVersionID int64                `json:"script_version_id"`
	ConnID          int64                `json:"conn_id"`
	Connection      string               `json:"connection"`
	DatabaseName    string               `json:"database_name"`
	Status          QueryApprovalStatus  `json:"status"`
	Summary         DataPlanSummary      `json:"summary"`
	Risk            DataPlanRisk         `json:"risk"`
	CreatorID       int64                `json:"creator_id"`
	CreatorName     string               `json:"creator_name"`
	ReviewerID      *int64               `json:"reviewer_id,omitempty"`
	ReviewerName    string               `json:"reviewer_name,omitempty"`
	ReviewNote      string               `json:"review_note"`
	WorkflowID      int64                `json:"workflow_id"`
	CurrentStep     int                  `json:"current_step"`
	Revision        int                  `json:"revision"`
	ExecuteError    string               `json:"execute_error"`
	ExecutedAt      *time.Time           `json:"executed_at,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Items           []DataChangePlanItem `json:"items,omitempty"`
}

type DataChangePlanItem struct {
	ID        int64                  `json:"id"`
	PlanID     int64                 `json:"plan_id"`
	SeqNo      int                   `json:"seq_no"`
	OpType     string                `json:"op_type"`
	TableName  string                `json:"table_name"`
	PK         map[string]any        `json:"pk"`
	Before     map[string]any        `json:"before"`
	After      map[string]any        `json:"after"`
	CreatedAt  time.Time             `json:"created_at"`
}

type DataPlanSummary struct {
	Updates int                       `json:"updates"`
	Inserts int                       `json:"inserts"`
	Deletes int                       `json:"deletes"`
	Tables  []DataPlanTableBreakdown  `json:"tables"`
}

type DataPlanTableBreakdown struct {
	Table   string `json:"table"`
	Updates int    `json:"updates"`
	Inserts int    `json:"inserts"`
	Deletes int    `json:"deletes"`
}

type DataPlanRisk struct {
	Level string   `json:"level"`
	Flags []string `json:"flags"`
}

type CreateDataScriptRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	SourceCode  string `json:"source_code"`
}

type CreateDataScriptVersionRequest struct {
	SourceCode   string `json:"source_code"`
	ParamsSchema string `json:"params_schema"`
}

type PreviewDataScriptRequest struct {
	ScriptVersionID int64  `json:"script_version_id"`
	ConnID          int64  `json:"conn_id"`
	Database        string `json:"database"`
}

type ReviewDataPlanRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

type rawScriptOperation struct {
	OpType    string
	TableName string
	PK        map[string]any
	After     map[string]any
}

func ListDataScripts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT
				ds.id, ds.name, ds.description, ds.language, ds.created_by, ds.created_at, ds.updated_at,
				COALESCE(dsv.id, 0), COALESCE(dsv.version_no, 0), COALESCE(dsv.source_code, '')
			FROM data_scripts ds
			LEFT JOIN data_script_versions dsv
				ON dsv.id = (
					SELECT id FROM data_script_versions
					WHERE script_id = ds.id
					ORDER BY version_no DESC, id DESC
					LIMIT 1
				)
			WHERE ds.is_active = 1
			ORDER BY ds.updated_at DESC, ds.id DESC
		`))
		if err != nil {
			http.Error(w, jsonError("failed to list data scripts"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		scripts := []DataScript{}
		for rows.Next() {
			var item DataScript
			if err := rows.Scan(
				&item.ID, &item.Name, &item.Description, &item.Language, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
				&item.LatestVersionID, &item.LatestVersionNo, &item.LatestSource,
			); err != nil {
				http.Error(w, jsonError("failed to read data scripts"), http.StatusInternalServerError)
				return
			}
			scripts = append(scripts, item)
		}
		json.NewEncoder(w).Encode(scripts)
	}
}

func CreateDataScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		var body CreateDataScriptRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		body.Description = strings.TrimSpace(body.Description)
		body.Language = strings.TrimSpace(body.Language)
		body.SourceCode = strings.TrimSpace(body.SourceCode)
		if body.Name == "" || body.SourceCode == "" {
			http.Error(w, jsonError("name and source_code are required"), http.StatusBadRequest)
			return
		}
		if body.Language == "" {
			body.Language = "javascript"
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		tx, err := appdb.DB.Begin()
		if err != nil {
			http.Error(w, jsonError("failed to start transaction"), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		var scriptID int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err = tx.QueryRow(`
				INSERT INTO data_scripts (name, description, language, created_by, is_active, created_at, updated_at)
				VALUES ($1, $2, $3, $4, 1, $5, $6)
				RETURNING id
			`, body.Name, body.Description, body.Language, userID, now, now).Scan(&scriptID)
		} else {
			res, execErr := tx.Exec(`
				INSERT INTO data_scripts (name, description, language, created_by, is_active, created_at, updated_at)
				VALUES (?, ?, ?, ?, 1, ?, ?)
			`, body.Name, body.Description, body.Language, userID, now, now)
			err = execErr
			if err == nil {
				scriptID, _ = res.LastInsertId()
			}
		}
		if err != nil {
			http.Error(w, jsonError("failed to create script"), http.StatusInternalServerError)
			return
		}
		if _, err := createDataScriptVersionTx(tx, scriptID, body.SourceCode, "{}", userID); err != nil {
			http.Error(w, jsonError("failed to create initial version"), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, jsonError("failed to commit script"), http.StatusInternalServerError)
			return
		}

		script, _ := getDataScriptByID(scriptID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(script)
	}
}

func GetDataScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseDataScriptID(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid script id"), http.StatusBadRequest)
			return
		}
		script, err := getDataScriptByID(id)
		if err != nil || script == nil {
			http.Error(w, jsonError("script not found"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(script)
	}
}

func ListDataScriptVersions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseDataScriptActionID(r.URL.Path, "/versions")
		if err != nil {
			http.Error(w, jsonError("invalid script id"), http.StatusBadRequest)
			return
		}
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, script_id, version_no, source_code, params_schema, sdk_version, created_by, created_at
			FROM data_script_versions
			WHERE script_id = ?
			ORDER BY version_no DESC, id DESC
		`), id)
		if err != nil {
			http.Error(w, jsonError("failed to list versions"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		versions := []DataScriptVersion{}
		for rows.Next() {
			var v DataScriptVersion
			if err := rows.Scan(&v.ID, &v.ScriptID, &v.VersionNo, &v.SourceCode, &v.ParamsSchema, &v.SDKVersion, &v.CreatedBy, &v.CreatedAt); err != nil {
				http.Error(w, jsonError("failed to read versions"), http.StatusInternalServerError)
				return
			}
			versions = append(versions, v)
		}
		json.NewEncoder(w).Encode(versions)
	}
}

func CreateDataScriptVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		id, err := parseDataScriptActionID(r.URL.Path, "/versions")
		if err != nil {
			http.Error(w, jsonError("invalid script id"), http.StatusBadRequest)
			return
		}
		var body CreateDataScriptVersionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.SourceCode = strings.TrimSpace(body.SourceCode)
		if body.SourceCode == "" {
			http.Error(w, jsonError("source_code is required"), http.StatusBadRequest)
			return
		}
		if body.ParamsSchema == "" {
			body.ParamsSchema = "{}"
		}
		tx, err := appdb.DB.Begin()
		if err != nil {
			http.Error(w, jsonError("failed to start transaction"), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		versionID, err := createDataScriptVersionTx(tx, id, body.SourceCode, body.ParamsSchema, userID)
		if err != nil {
			http.Error(w, jsonError("failed to create version"), http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(appdb.ConvertQuery(`UPDATE data_scripts SET updated_at = ? WHERE id = ?`), time.Now().UTC().Format("2006-01-02 15:04:05"), id); err != nil {
			http.Error(w, jsonError("failed to update script timestamp"), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, jsonError("failed to commit version"), http.StatusInternalServerError)
			return
		}
		version, _ := getDataScriptVersionByID(versionID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(version)
	}
}

func PreviewDataScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		id, err := parseDataScriptActionID(r.URL.Path, "/preview")
		if err != nil {
			http.Error(w, jsonError("invalid script id"), http.StatusBadRequest)
			return
		}
		var body PreviewDataScriptRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		if body.ConnID <= 0 || body.ScriptVersionID <= 0 {
			http.Error(w, jsonError("conn_id and script_version_id are required"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, body.ConnID) {
			http.Error(w, jsonError("connection access denied"), http.StatusForbidden)
			return
		}

		version, err := getDataScriptVersionByID(body.ScriptVersionID)
		if err != nil || version == nil || version.ScriptID != id {
			http.Error(w, jsonError("script version not found"), http.StatusNotFound)
			return
		}
		ops, err := parseDataScriptOperations(version.SourceCode)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if len(ops) == 0 {
			http.Error(w, jsonError("no plan operations found; use plan.insert/update/delete(...) lines"), http.StatusBadRequest)
			return
		}

		planID, err := createDataChangePlan(id, version.ID, body.ConnID, strings.TrimSpace(body.Database), userID, ops)
		if err != nil {
			http.Error(w, jsonError("failed to create preview plan"), http.StatusInternalServerError)
			return
		}
		plan, _ := getDataChangePlanByID(planID)
		json.NewEncoder(w).Encode(plan)
	}
}

func ListDataScriptPlans() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseDataScriptActionID(r.URL.Path, "/plans")
		if err != nil {
			http.Error(w, jsonError("invalid script id"), http.StatusBadRequest)
			return
		}
		plans, err := listDataChangePlans(id)
		if err != nil {
			http.Error(w, jsonError("failed to list plans"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(plans)
	}
}

func GetDataChangePlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePlanID(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid plan id"), http.StatusBadRequest)
			return
		}
		plan, err := getDataChangePlanByID(id)
		if err != nil || plan == nil {
			http.Error(w, jsonError("plan not found"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(plan)
	}
}

func SubmitDataChangePlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePlanActionID(r.URL.Path, "/submit")
		if err != nil {
			http.Error(w, jsonError("invalid plan id"), http.StatusBadRequest)
			return
		}
		plan, err := getDataChangePlanByID(id)
		if err != nil || plan == nil {
			http.Error(w, jsonError("plan not found"), http.StatusNotFound)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != plan.CreatorID {
			http.Error(w, jsonError("only the creator or admin can submit this plan"), http.StatusForbidden)
			return
		}
		if plan.Status != QueryApprovalStatusDraft && plan.Status != QueryApprovalStatusRejected {
			http.Error(w, jsonError("only draft or rejected plans can be submitted"), http.StatusBadRequest)
			return
		}
		resolveRole := role
		if resolveRole == "" {
			var creatorRole string
			_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT role FROM users WHERE id = ?`), plan.CreatorID).Scan(&creatorRole)
			resolveRole = creatorRole
		}
		workflowID, err := resolveWorkflowID(userID, resolveRole, plan.ConnID, 0)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if workflowID == 0 {
			http.Error(w, jsonError("no applicable workflow found"), http.StatusBadRequest)
			return
		}
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE data_change_plans
			SET status = 'pending_review', workflow_id = ?, current_step = 1, review_note = '', reviewer_id = NULL, updated_at = ?
			WHERE id = ?
		`), workflowID, time.Now().UTC().Format("2006-01-02 15:04:05"), id)
		if err != nil {
			http.Error(w, jsonError("failed to submit plan"), http.StatusInternalServerError)
			return
		}
		updated, _ := getDataChangePlanByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func ReviewDataChangePlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePlanActionID(r.URL.Path, "/review")
		if err != nil {
			http.Error(w, jsonError("invalid plan id"), http.StatusBadRequest)
			return
		}
		plan, err := getDataChangePlanByID(id)
		if err != nil || plan == nil {
			http.Error(w, jsonError("plan not found"), http.StatusNotFound)
			return
		}
		if plan.Status != QueryApprovalStatusPendingReview {
			http.Error(w, jsonError("only pending review plans can be reviewed"), http.StatusBadRequest)
			return
		}
		userID, username, role := currentUserFromHeaders(r)
		var body ReviewDataPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Action = strings.TrimSpace(body.Action)
		if body.Action != "approved" && body.Action != "rejected" {
			http.Error(w, jsonError("action must be approved or rejected"), http.StatusBadRequest)
			return
		}
		if body.Action == "rejected" && strings.TrimSpace(body.Note) == "" {
			http.Error(w, jsonError("review note is required when rejecting"), http.StatusBadRequest)
			return
		}
		step, err := getStepByWorkflowAndOrder(plan.WorkflowID, plan.CurrentStep)
		if err != nil || step == nil {
			http.Error(w, jsonError("current approval step not found"), http.StatusBadRequest)
			return
		}
		eligible, _ := isUserEligibleForStep(step.ID, userID, role)
		if !eligible {
			http.Error(w, jsonError("you are not eligible to review this step"), http.StatusForbidden)
			return
		}
		alreadyActed, _ := hasUserActedOnDataPlanStep(plan.ID, step.ID, userID, plan.Revision)
		if alreadyActed {
			http.Error(w, jsonError("you have already acted on this step"), http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO data_change_plan_approval (plan_id, step_id, revision, user_id, username, action, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`), plan.ID, step.ID, plan.Revision, userID, username, body.Action, strings.TrimSpace(body.Note)); err != nil {
			http.Error(w, jsonError("failed to record review action"), http.StatusInternalServerError)
			return
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		if body.Action == "rejected" {
			_, err = appdb.DB.Exec(appdb.ConvertQuery(`
				UPDATE data_change_plans
				SET status = 'rejected', reviewer_id = ?, review_note = ?, updated_at = ?
				WHERE id = ?
			`), userID, strings.TrimSpace(body.Note), now, id)
			if err != nil {
				http.Error(w, jsonError("failed to update review"), http.StatusInternalServerError)
				return
			}
		} else {
			approvedCount, _ := countDataPlanStepApprovals(plan.ID, step.ID, plan.Revision)
			if approvedCount >= step.RequiredApprovals {
				totalSteps, _ := countWorkflowSteps(plan.WorkflowID)
				if plan.CurrentStep >= totalSteps {
					_, err = appdb.DB.Exec(appdb.ConvertQuery(`
						UPDATE data_change_plans
						SET status = 'approved', reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
						WHERE id = ?
					`), userID, strings.TrimSpace(body.Note), plan.CurrentStep+1, now, id)
				} else {
					_, err = appdb.DB.Exec(appdb.ConvertQuery(`
						UPDATE data_change_plans
						SET reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
						WHERE id = ?
					`), userID, strings.TrimSpace(body.Note), plan.CurrentStep+1, now, id)
				}
				if err != nil {
					http.Error(w, jsonError("failed to advance approval"), http.StatusInternalServerError)
					return
				}
			}
		}
		go WriteFeatureAccessAudit(username, "review_data_script_plan", fmt.Sprintf("plan:%d", id), body.Action)
		updated, _ := getDataChangePlanByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func ExecuteDataChangePlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parsePlanActionID(r.URL.Path, "/execute")
		if err != nil {
			http.Error(w, jsonError("invalid plan id"), http.StatusBadRequest)
			return
		}
		plan, err := getDataChangePlanByID(id)
		if err != nil || plan == nil {
			http.Error(w, jsonError("plan not found"), http.StatusNotFound)
			return
		}
		userID, username, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != plan.CreatorID {
			http.Error(w, jsonError("only the creator or admin can execute this plan"), http.StatusForbidden)
			return
		}
		if plan.Status != QueryApprovalStatusApproved {
			http.Error(w, jsonError("only approved plans can be executed"), http.StatusBadRequest)
			return
		}
		if !CheckWritePermission(r, plan.ConnID) {
			http.Error(w, jsonError("write permission denied"), http.StatusForbidden)
			return
		}

		db, driver, err := GetDB(plan.ConnID)
		if err != nil {
			http.Error(w, jsonError("database connection error"), http.StatusBadGateway)
			return
		}
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE data_change_plans SET status = 'executing', updated_at = ? WHERE id = ?`), time.Now().UTC().Format("2006-01-02 15:04:05"), id)

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, jsonError("failed to start target transaction"), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		start := time.Now()
		var affected int64
		for _, item := range plan.Items {
			n, execErr := executeDataPlanItem(tx, driver, plan.DatabaseName, item)
			if execErr != nil {
				markDataPlanExecution(id, QueryApprovalStatusFailed, sanitizeDBError(execErr))
				http.Error(w, jsonError(sanitizeDBError(execErr)), http.StatusBadRequest)
				return
			}
			affected += n
		}
		if err := tx.Commit(); err != nil {
			markDataPlanExecution(id, QueryApprovalStatusFailed, sanitizeDBError(err))
			http.Error(w, jsonError(sanitizeDBError(err)), http.StatusBadRequest)
			return
		}

		markDataPlanExecution(id, QueryApprovalStatusDone, "")
		go WriteAuditLog(username, plan.ConnID, plan.Connection, fmt.Sprintf("DATA SCRIPT PLAN #%d", id), time.Since(start).Milliseconds(), affected, "")
		updated, _ := getDataChangePlanByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func createDataScriptVersionTx(tx *sql.Tx, scriptID int64, sourceCode, paramsSchema string, userID int64) (int64, error) {
	var nextVersion int
	if err := tx.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(MAX(version_no), 0) + 1 FROM data_script_versions WHERE script_id = ?`), scriptID).Scan(&nextVersion); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if appdb.IsPostgreSQL() || appdb.IsMySQL() {
		var versionID int64
		err := tx.QueryRow(`
			INSERT INTO data_script_versions (script_id, version_no, source_code, params_schema, sdk_version, created_by, created_at)
			VALUES ($1, $2, $3, $4, 'v1', $5, $6)
			RETURNING id
		`, scriptID, nextVersion, sourceCode, paramsSchema, userID, now).Scan(&versionID)
		return versionID, err
	}
	res, err := tx.Exec(`
		INSERT INTO data_script_versions (script_id, version_no, source_code, params_schema, sdk_version, created_by, created_at)
		VALUES (?, ?, ?, ?, 'v1', ?, ?)
	`, scriptID, nextVersion, sourceCode, paramsSchema, userID, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func getDataScriptByID(id int64) (*DataScript, error) {
	row := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT
			ds.id, ds.name, ds.description, ds.language, ds.created_by, ds.created_at, ds.updated_at,
			COALESCE(dsv.id, 0), COALESCE(dsv.version_no, 0), COALESCE(dsv.source_code, '')
		FROM data_scripts ds
		LEFT JOIN data_script_versions dsv
			ON dsv.id = (
				SELECT id FROM data_script_versions WHERE script_id = ds.id ORDER BY version_no DESC, id DESC LIMIT 1
			)
		WHERE ds.id = ? AND ds.is_active = 1
	`), id)
	var item DataScript
	if err := row.Scan(
		&item.ID, &item.Name, &item.Description, &item.Language, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&item.LatestVersionID, &item.LatestVersionNo, &item.LatestSource,
	); err != nil {
		return nil, err
	}
	plans, _ := listDataChangePlans(id)
	item.Plans = plans
	return &item, nil
}

func getDataScriptVersionByID(id int64) (*DataScriptVersion, error) {
	row := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, script_id, version_no, source_code, params_schema, sdk_version, created_by, created_at
		FROM data_script_versions WHERE id = ?
	`), id)
	var v DataScriptVersion
	if err := row.Scan(&v.ID, &v.ScriptID, &v.VersionNo, &v.SourceCode, &v.ParamsSchema, &v.SDKVersion, &v.CreatedBy, &v.CreatedAt); err != nil {
		return nil, err
	}
	return &v, nil
}

func createDataChangePlan(scriptID, versionID, connID int64, database string, userID int64, ops []rawScriptOperation) (int64, error) {
	summary, risk := summarizeDataPlan(ops)
	summaryJSON, _ := json.Marshal(summary)
	riskJSON, _ := json.Marshal(risk)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	tx, err := appdb.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var planID int64
	if appdb.IsPostgreSQL() || appdb.IsMySQL() {
		err = tx.QueryRow(`
			INSERT INTO data_change_plans (script_id, script_version_id, conn_id, database_name, status, summary_json, risk_json, creator_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8, $9)
			RETURNING id
		`, scriptID, versionID, connID, database, string(summaryJSON), string(riskJSON), userID, now, now).Scan(&planID)
	} else {
		res, execErr := tx.Exec(`
			INSERT INTO data_change_plans (script_id, script_version_id, conn_id, database_name, status, summary_json, risk_json, creator_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?)
		`, scriptID, versionID, connID, database, string(summaryJSON), string(riskJSON), userID, now, now)
		err = execErr
		if err == nil {
			planID, _ = res.LastInsertId()
		}
	}
	if err != nil {
		return 0, err
	}

	dbConn, driver, err := GetDB(connID)
	if err != nil {
		return 0, err
	}

	for idx, op := range ops {
		before := map[string]any{}
		after := op.After
		if op.OpType == "update" || op.OpType == "delete" {
			before, err = fetchPlanTargetRow(dbConn, driver, database, op.TableName, op.PK)
			if err != nil {
				return 0, err
			}
		}
		if op.OpType == "update" {
			merged := map[string]any{}
			for key, val := range before {
				merged[key] = val
			}
			for key, val := range after {
				merged[key] = val
			}
			after = merged
		}
		pkJSON, _ := json.Marshal(op.PK)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if _, err := tx.Exec(appdb.ConvertQuery(`
			INSERT INTO data_change_plan_items (plan_id, seq_no, op_type, table_name, pk_json, before_json, after_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`), planID, idx+1, op.OpType, op.TableName, string(pkJSON), string(beforeJSON), string(afterJSON), now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return planID, nil
}

func listDataChangePlans(scriptID int64) ([]DataChangePlan, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT
			p.id, p.script_id, p.script_version_id, p.conn_id, COALESCE(c.name, ''), p.database_name, p.status,
			p.summary_json, p.risk_json, p.creator_id, COALESCE(u1.username, ''), p.reviewer_id, COALESCE(u2.username, ''),
			p.review_note, COALESCE(p.workflow_id, 0), p.current_step, p.revision, p.execute_error, p.executed_at, p.created_at, p.updated_at
		FROM data_change_plans p
		LEFT JOIN connections c ON c.id = p.conn_id
		LEFT JOIN users u1 ON u1.id = p.creator_id
		LEFT JOIN users u2 ON u2.id = p.reviewer_id
		WHERE p.script_id = ?
		ORDER BY p.created_at DESC, p.id DESC
	`), scriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDataChangePlans(rows, false)
}

func getDataChangePlanByID(id int64) (*DataChangePlan, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT
			p.id, p.script_id, p.script_version_id, p.conn_id, COALESCE(c.name, ''), p.database_name, p.status,
			p.summary_json, p.risk_json, p.creator_id, COALESCE(u1.username, ''), p.reviewer_id, COALESCE(u2.username, ''),
			p.review_note, COALESCE(p.workflow_id, 0), p.current_step, p.revision, p.execute_error, p.executed_at, p.created_at, p.updated_at
		FROM data_change_plans p
		LEFT JOIN connections c ON c.id = p.conn_id
		LEFT JOIN users u1 ON u1.id = p.creator_id
		LEFT JOIN users u2 ON u2.id = p.reviewer_id
		WHERE p.id = ?
	`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans, err := scanDataChangePlans(rows, true)
	if err != nil || len(plans) == 0 {
		return nil, err
	}
	return &plans[0], nil
}

func scanDataChangePlans(rows *sql.Rows, withItems bool) ([]DataChangePlan, error) {
	plans := []DataChangePlan{}
	for rows.Next() {
		var item DataChangePlan
		var summaryJSON, riskJSON string
		var reviewerID sql.NullInt64
		var reviewerName sql.NullString
		var executedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.ScriptID, &item.ScriptVersionID, &item.ConnID, &item.Connection, &item.DatabaseName, &item.Status,
			&summaryJSON, &riskJSON, &item.CreatorID, &item.CreatorName, &reviewerID, &reviewerName,
			&item.ReviewNote, &item.WorkflowID, &item.CurrentStep, &item.Revision, &item.ExecuteError, &executedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(summaryJSON), &item.Summary)
		_ = json.Unmarshal([]byte(riskJSON), &item.Risk)
		if reviewerID.Valid {
			item.ReviewerID = &reviewerID.Int64
		}
		if reviewerName.Valid {
			item.ReviewerName = reviewerName.String
		}
		if executedAt.Valid {
			t := executedAt.Time
			item.ExecutedAt = &t
		}
		if withItems {
			items, _ := listPlanItems(item.ID)
			item.Items = items
		}
		plans = append(plans, item)
	}
	return plans, rows.Err()
}

func listPlanItems(planID int64) ([]DataChangePlanItem, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, plan_id, seq_no, op_type, table_name, pk_json, before_json, after_json, created_at
		FROM data_change_plan_items
		WHERE plan_id = ?
		ORDER BY seq_no ASC, id ASC
	`), planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DataChangePlanItem{}
	for rows.Next() {
		var item DataChangePlanItem
		var pkJSON, beforeJSON, afterJSON string
		if err := rows.Scan(&item.ID, &item.PlanID, &item.SeqNo, &item.OpType, &item.TableName, &pkJSON, &beforeJSON, &afterJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pkJSON), &item.PK)
		_ = json.Unmarshal([]byte(beforeJSON), &item.Before)
		_ = json.Unmarshal([]byte(afterJSON), &item.After)
		items = append(items, item)
	}
	return items, rows.Err()
}

func parseDataScriptOperations(source string) ([]rawScriptOperation, error) {
	lines := strings.Split(source, "\n")
	ops := []rawScriptOperation{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "const ") || strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "function ") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "plan.insert("):
			args, err := parseCallArgs(line, "plan.insert(")
			if err != nil || len(args) != 2 {
				return nil, fmt.Errorf("invalid insert syntax: %s", line)
			}
			values, err := parseJSONMap(args[1])
			if err != nil {
				return nil, fmt.Errorf("invalid insert values JSON: %s", line)
			}
			ops = append(ops, rawScriptOperation{OpType: "insert", TableName: strings.Trim(args[0], `"'`), After: values, PK: map[string]any{}})
		case strings.HasPrefix(line, "plan.update("):
			args, err := parseCallArgs(line, "plan.update(")
			if err != nil || len(args) != 3 {
				return nil, fmt.Errorf("invalid update syntax: %s", line)
			}
			pk, err := parseJSONMap(args[1])
			if err != nil {
				return nil, fmt.Errorf("invalid update pk JSON: %s", line)
			}
			after, err := parseJSONMap(args[2])
			if err != nil {
				return nil, fmt.Errorf("invalid update after JSON: %s", line)
			}
			ops = append(ops, rawScriptOperation{OpType: "update", TableName: strings.Trim(args[0], `"'`), PK: pk, After: after})
		case strings.HasPrefix(line, "plan.delete("):
			args, err := parseCallArgs(line, "plan.delete(")
			if err != nil || len(args) != 2 {
				return nil, fmt.Errorf("invalid delete syntax: %s", line)
			}
			pk, err := parseJSONMap(args[1])
			if err != nil {
				return nil, fmt.Errorf("invalid delete pk JSON: %s", line)
			}
			ops = append(ops, rawScriptOperation{OpType: "delete", TableName: strings.Trim(args[0], `"'`), PK: pk, After: map[string]any{}})
		}
	}
	return ops, nil
}

func parseCallArgs(line, prefix string) ([]string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(line), ";")
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, ")") {
		return nil, fmt.Errorf("invalid call")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), ")")
	args := []string{}
	current := strings.Builder{}
	depth := 0
	inString := false
	stringChar := byte(0)
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			current.WriteByte(ch)
			if ch == stringChar && (i == 0 || body[i-1] != '\\') {
				inString = false
			}
			continue
		}
		switch ch {
		case '\'', '"':
			inString = true
			stringChar = ch
			current.WriteByte(ch)
		case '{', '[':
			depth++
			current.WriteByte(ch)
		case '}', ']':
			depth--
			current.WriteByte(ch)
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteByte(ch)
			}
		default:
			current.WriteByte(ch)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		args = append(args, strings.TrimSpace(current.String()))
	}
	return args, nil
}

func parseJSONMap(raw string) (map[string]any, error) {
	var out map[string]any
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

func summarizeDataPlan(ops []rawScriptOperation) (DataPlanSummary, DataPlanRisk) {
	summary := DataPlanSummary{Tables: []DataPlanTableBreakdown{}}
	byTable := map[string]*DataPlanTableBreakdown{}
	flags := []string{}
	addFlag := func(flag string) {
		for _, existing := range flags {
			if existing == flag {
				return
			}
		}
		flags = append(flags, flag)
	}
	for _, op := range ops {
		tb := byTable[op.TableName]
		if tb == nil {
			tb = &DataPlanTableBreakdown{Table: op.TableName}
			byTable[op.TableName] = tb
			summary.Tables = append(summary.Tables, *tb)
		}
		switch op.OpType {
		case "insert":
			summary.Inserts++
			tb.Inserts++
		case "update":
			summary.Updates++
			tb.Updates++
		case "delete":
			summary.Deletes++
			tb.Deletes++
			addFlag("delete_operation")
		}
	}
	for i := range summary.Tables {
		if tb := byTable[summary.Tables[i].Table]; tb != nil {
			summary.Tables[i] = *tb
		}
	}
	total := summary.Updates + summary.Inserts + summary.Deletes
	if total > 25 {
		addFlag("bulk_change")
	}
	if len(summary.Tables) > 1 {
		addFlag("multi_table_change")
	}
	level := "low"
	switch {
	case total > 100 || summary.Deletes > 0:
		level = "high"
	case total > 20 || len(flags) > 0:
		level = "medium"
	}
	return summary, DataPlanRisk{Level: level, Flags: flags}
}

func executeDataPlanItem(tx *sql.Tx, driver, database string, item DataChangePlanItem) (int64, error) {
	tableRef := qualifiedTableName(driver, database, item.TableName)
	switch item.OpType {
	case "insert":
		cols := make([]string, 0, len(item.After))
		vals := make([]string, 0, len(item.After))
		for key, val := range item.After {
			cols = append(cols, quoteIdent(driver, key))
			vals = append(vals, sqlLiteral(val))
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableRef, strings.Join(cols, ", "), strings.Join(vals, ", "))
		res, err := tx.Exec(query)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	case "update":
		if len(item.PK) == 0 {
			return 0, fmt.Errorf("update on %s requires pk", item.TableName)
		}
		sets := []string{}
		for key, val := range item.After {
			sets = append(sets, fmt.Sprintf("%s = %s", quoteIdent(driver, key), sqlLiteral(val)))
		}
		query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableRef, strings.Join(sets, ", "), buildEqualityWhere(driver, item.PK))
		res, err := tx.Exec(query)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	case "delete":
		if len(item.PK) == 0 {
			return 0, fmt.Errorf("delete on %s requires pk", item.TableName)
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE %s", tableRef, buildEqualityWhere(driver, item.PK))
		res, err := tx.Exec(query)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	default:
		return 0, fmt.Errorf("unsupported op type")
	}
}

func buildEqualityWhere(driver string, pk map[string]any) string {
	parts := []string{}
	for key, val := range pk {
		parts = append(parts, fmt.Sprintf("%s = %s", quoteIdent(driver, key), sqlLiteral(val)))
	}
	return strings.Join(parts, " AND ")
}

func fetchPlanTargetRow(dbConn *sql.DB, driver, database, table string, pk map[string]any) (map[string]any, error) {
	if len(pk) == 0 {
		return map[string]any{}, nil
	}
	tableRef := qualifiedTableName(driver, database, table)
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s", tableRef, buildEqualityWhere(driver, pk))
	rows, err := dbConn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return map[string]any{}, nil
	}
	values := make([]any, len(cols))
	targets := make([]any, len(cols))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := rows.Scan(targets...); err != nil {
		return nil, err
	}
	result := map[string]any{}
	for i, col := range cols {
		switch v := values[i].(type) {
		case []byte:
			result[col] = string(v)
		default:
			result[col] = v
		}
	}
	return result, nil
}

func markDataPlanExecution(id int64, status QueryApprovalStatus, execError string) {
	now := time.Now().UTC()
	if status == QueryApprovalStatusDone {
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE data_change_plans
			SET status = ?, execute_error = ?, executed_at = ?, updated_at = ?
			WHERE id = ?
		`), status, execError, now.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"), id)
		return
	}
	_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
		UPDATE data_change_plans
		SET status = ?, execute_error = ?, updated_at = ?
		WHERE id = ?
	`), status, execError, now.Format("2006-01-02 15:04:05"), id)
}

func hasUserActedOnDataPlanStep(planID, stepID, userID int64, revision int) (bool, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM data_change_plan_approval WHERE plan_id = ? AND step_id = ? AND user_id = ? AND revision = ?`), planID, stepID, userID, revision).Scan(&count)
	return count > 0, err
}

func countDataPlanStepApprovals(planID, stepID int64, revision int) (int, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM data_change_plan_approval WHERE plan_id = ? AND step_id = ? AND action = 'approved' AND revision = ?`), planID, stepID, revision).Scan(&count)
	return count, err
}

func parseDataScriptID(path string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(path, "/api/data-scripts/"), 10, 64)
}

func parseDataScriptActionID(path, suffix string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/data-scripts/")
	trimmed = strings.TrimSuffix(trimmed, suffix)
	return strconv.ParseInt(trimmed, 10, 64)
}

func parsePlanID(path string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(path, "/api/data-change-plans/"), 10, 64)
}

func parsePlanActionID(path, suffix string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/data-change-plans/")
	trimmed = strings.TrimSuffix(trimmed, suffix)
	return strconv.ParseInt(trimmed, 10, 64)
}
