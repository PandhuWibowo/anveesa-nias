package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type ChangeSetValidationResponse struct {
	OK               bool                      `json:"ok"`
	ValidationStatus ChangeSetValidationStatus `json:"validation_status"`
	Message          string                    `json:"message"`
	ImpactSummary    string                    `json:"impact_summary"`
}

func ListChangeSets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		userID, _, role := currentUserFromHeaders(r)
		query := `
			SELECT
				cs.id, cs.title, cs.description, cs.conn_id,
				COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, 'development'),
				cs.database_name, cs.statement, cs.rollback_sql, cs.impact_summary, cs.status,
				cs.creator_id, COALESCE(u1.username, ''),
				cs.reviewer_id, COALESCE(u2.username, ''), cs.review_note,
				COALESCE(cs.workflow_id, 0), cs.current_step, cs.revision,
				cs.validation_status, cs.validation_message, cs.validated_at,
				cs.execute_error, cs.executed_at, cs.created_at, cs.updated_at
			FROM change_sets cs
			LEFT JOIN connections c ON c.id = cs.conn_id
			LEFT JOIN users u1 ON u1.id = cs.creator_id
			LEFT JOIN users u2 ON u2.id = cs.reviewer_id
		`

		var rows *sql.Rows
		var err error
		if !isAuthEnabled() || role == "admin" {
			rows, err = appdb.DB.QueryContext(ctx, query+` ORDER BY cs.updated_at DESC`)
		} else {
			query += `
				WHERE cs.creator_id = ?
				   OR EXISTS (
						SELECT 1
						FROM workflow_step ws
						JOIN step_approver sa ON sa.step_id = ws.id
						LEFT JOIN roles r ON sa.approver_type = 'role' AND sa.approver_id = r.id
						WHERE ws.workflow_id = cs.workflow_id
						  AND ws.step_order = cs.current_step
						  AND (
								(sa.approver_type = 'user' AND sa.approver_id = ?)
							 OR (sa.approver_type = 'role' AND COALESCE(r.name, '') = ?)
						  )
				   )
				ORDER BY cs.updated_at DESC
			`
			rows, err = appdb.DB.QueryContext(ctx, appdb.ConvertQuery(query), userID, userID, role)
		}
		if err != nil {
			http.Error(w, jsonError("failed to list change sets"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		changeSets, err := scanChangeSets(rows)
		if err != nil {
			http.Error(w, jsonError("failed to read change sets"), http.StatusInternalServerError)
			return
		}
		if changeSets == nil {
			changeSets = []ChangeSet{}
		}
		json.NewEncoder(w).Encode(changeSets)
	}
}

func GetChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseChangeSetID(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		if !canViewChangeSet(r, cs) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(cs)
	}
}

func CreateChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, _ := currentUserFromHeaders(r)
		if isAuthEnabled() && userID == 0 {
			http.Error(w, jsonError("unauthorized"), http.StatusUnauthorized)
			return
		}

		var body CreateChangeSetRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		body.Statement = strings.TrimSpace(body.Statement)
		if body.Title == "" || body.Statement == "" || body.ConnID <= 0 {
			http.Error(w, jsonError("title, statement, and conn_id are required"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, body.ConnID) {
			http.Error(w, jsonError("connection access denied"), http.StatusForbidden)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		impactSummary := strings.TrimSpace(body.ImpactSummary)
		if impactSummary == "" {
			impactSummary = summarizeChangeImpact(body.Statement)
		}
		var id int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err := appdb.DB.QueryRow(`
				INSERT INTO change_sets
					(title, description, conn_id, database_name, statement, rollback_sql, impact_summary, status, creator_id, workflow_id, current_step, revision, validation_status, validation_message, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8, NULLIF($9, 0), 0, 1, 'pending', '', $10, $11)
				RETURNING id
			`, body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, strings.TrimSpace(body.RollbackSQL), impactSummary, userID, body.WorkflowID, now, now).Scan(&id)
			if err != nil {
				http.Error(w, jsonError("failed to create change set"), http.StatusInternalServerError)
				return
			}
		} else {
			res, err := appdb.DB.Exec(`
				INSERT INTO change_sets
					(title, description, conn_id, database_name, statement, rollback_sql, impact_summary, status, creator_id, workflow_id, current_step, revision, validation_status, validation_message, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'draft', ?, NULLIF(?, 0), 0, 1, 'pending', '', ?, ?)
			`, body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, strings.TrimSpace(body.RollbackSQL), impactSummary, userID, body.WorkflowID, now, now)
			if err != nil {
				http.Error(w, jsonError("failed to create change set"), http.StatusInternalServerError)
				return
			}
			id, _ = res.LastInsertId()
		}

		cs, _ := getChangeSetByID(id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(cs)
	}
}

func UpdateChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseChangeSetID(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}

		userID, _, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != cs.CreatorID {
			http.Error(w, jsonError("only the creator or an admin can edit this change set"), http.StatusForbidden)
			return
		}
		if cs.Status == QueryApprovalStatusExecuting || cs.Status == QueryApprovalStatusDone {
			http.Error(w, jsonError("executing or completed change sets cannot be edited"), http.StatusBadRequest)
			return
		}
		if cs.Status == QueryApprovalStatusApproved && role != "admin" {
			canManageApproved, err := canRequesterManageApprovedChangeSet(cs, userID, role)
			if err != nil {
				http.Error(w, jsonError("failed to validate approval permissions"), http.StatusInternalServerError)
				return
			}
			if !canManageApproved {
				http.Error(w, jsonError("only requesters included in the approver workflow can revise an approved change set"), http.StatusForbidden)
				return
			}
		}

		var body UpdateChangeSetRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		body.Statement = strings.TrimSpace(body.Statement)
		if body.Title == "" || body.Statement == "" || body.ConnID <= 0 {
			http.Error(w, jsonError("title, statement, and conn_id are required"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, body.ConnID) {
			http.Error(w, jsonError("connection access denied"), http.StatusForbidden)
			return
		}

		impactSummary := strings.TrimSpace(body.ImpactSummary)
		if impactSummary == "" {
			impactSummary = summarizeChangeImpact(body.Statement)
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE change_sets
			SET title = ?, description = ?, conn_id = ?, database_name = ?, statement = ?, rollback_sql = ?, impact_summary = ?,
				status = 'draft', workflow_id = NULLIF(?, 0), current_step = 0, reviewer_id = NULL, review_note = '',
				revision = revision + 1, validation_status = 'pending', validation_message = '', validated_at = NULL,
				execute_error = '', updated_at = ?
			WHERE id = ?
		`), body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, strings.TrimSpace(body.RollbackSQL), impactSummary, body.WorkflowID, now, id)
		if err != nil {
			http.Error(w, jsonError("failed to update change set"), http.StatusInternalServerError)
			return
		}

		updated, _ := getChangeSetByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func ValidateChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseActionChangeSetID(r.URL.Path, "/validate")
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		if !canEditChangeSet(r, cs) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		if cs.Status == QueryApprovalStatusExecuting || cs.Status == QueryApprovalStatusDone {
			http.Error(w, jsonError("executing or completed change sets cannot be revalidated"), http.StatusBadRequest)
			return
		}

		result := validateChangeSetTarget(r.Context(), cs.ConnID, cs.Database, cs.Statement)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE change_sets
			SET impact_summary = ?, validation_status = ?, validation_message = ?, validated_at = ?, updated_at = ?
			WHERE id = ?
		`), result.ImpactSummary, result.ValidationStatus, result.Message, now, now, id)
		if err != nil {
			http.Error(w, jsonError("failed to update validation result"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SubmitChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseActionChangeSetID(r.URL.Path, "/submit")
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		if !canEditChangeSet(r, cs) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		if cs.ValidationStatus != ChangeSetValidationPassed {
			http.Error(w, jsonError("validate the change set successfully before submitting"), http.StatusBadRequest)
			return
		}
		if cs.Status == QueryApprovalStatusExecuting || cs.Status == QueryApprovalStatusDone {
			http.Error(w, jsonError("executing or completed change sets cannot be submitted"), http.StatusBadRequest)
			return
		}

		userID, _, role := currentUserFromHeaders(r)
		resolveRole := role
		if resolveRole == "" {
			var creatorRole string
			_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT role FROM users WHERE id = ?`), cs.CreatorID).Scan(&creatorRole)
			resolveRole = creatorRole
		}
		workflowID, err := resolveWorkflowID(userID, resolveRole, cs.ConnID, cs.WorkflowID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if workflowID == 0 {
			http.Error(w, jsonError("no applicable workflow found"), http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE change_sets
			SET status = 'pending_review', workflow_id = ?, current_step = 1, updated_at = ?
			WHERE id = ?
		`), workflowID, now, id)
		if err != nil {
			http.Error(w, jsonError("failed to submit change set"), http.StatusInternalServerError)
			return
		}
		updated, _ := getChangeSetByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func GetChangeSetApprovalProgress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseActionChangeSetID(r.URL.Path, "/approval-progress")
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		if !canViewChangeSet(r, cs) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		if cs.WorkflowID == 0 {
			json.NewEncoder(w).Encode(map[string]any{
				"workflow_name": "",
				"current_step":  0,
				"total_steps":   0,
				"progress":      []ApprovalProgress{},
				"can_approve":   false,
				"can_execute":   false,
				"can_revise":    canEditChangeSet(r, cs),
			})
			return
		}

		wf, err := getWorkflowByID(cs.WorkflowID)
		if err != nil || wf == nil {
			http.Error(w, jsonError("workflow not found"), http.StatusInternalServerError)
			return
		}
		approvals, _ := listChangeSetApprovals(id, cs.Revision)
		progress := make([]ApprovalProgress, 0, len(wf.Steps))
		for _, step := range wf.Steps {
			p := ApprovalProgress{Step: step, Status: "pending", Approvals: []ChangeApproval{}}
			approvedCount := 0
			rejected := false
			for _, approval := range approvals {
				if approval.StepID == step.ID {
					p.Approvals = append(p.Approvals, approval)
					if approval.Action == "approved" {
						approvedCount++
					}
					if approval.Action == "rejected" {
						rejected = true
					}
				}
			}
			switch {
			case rejected:
				p.Status = "rejected"
			case approvedCount >= step.RequiredApprovals || step.StepOrder < cs.CurrentStep:
				p.Status = "approved"
			case step.StepOrder > cs.CurrentStep:
				p.Status = "waiting"
			}
			progress = append(progress, p)
		}

		userID, _, role := currentUserFromHeaders(r)
		canApprove := false
		if cs.CurrentStep > 0 {
			if step, _ := getStepByWorkflowAndOrder(cs.WorkflowID, cs.CurrentStep); step != nil {
				eligible, _ := isUserEligibleForStep(step.ID, userID, role)
				alreadyActed, _ := hasUserActedOnChangeSetStep(cs.ID, step.ID, userID, cs.Revision)
				canApprove = eligible && !alreadyActed
			}
		}
		canManageApproved := role == "admin"
		if !canManageApproved {
			canManageApproved, _ = canRequesterManageApprovedChangeSet(cs, userID, role)
		}
		canExecute := cs.Status == QueryApprovalStatusApproved && canManageApproved

		json.NewEncoder(w).Encode(map[string]any{
			"workflow_name": wf.Name,
			"current_step":  cs.CurrentStep,
			"total_steps":   len(wf.Steps),
			"progress":      progress,
			"can_approve":   canApprove,
			"can_execute":   canExecute,
			"can_revise":    canEditChangeSet(r, cs),
		})
	}
}

func ApproveChangeSetStep() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseActionChangeSetID(r.URL.Path, "/approve-step")
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		if cs.Status != QueryApprovalStatusPendingReview {
			http.Error(w, jsonError("only pending change sets can be approved"), http.StatusBadRequest)
			return
		}

		var body ApproveStepRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		if body.Action != "approved" && body.Action != "rejected" {
			http.Error(w, jsonError("action must be approved or rejected"), http.StatusBadRequest)
			return
		}
		if body.Action == "rejected" && strings.TrimSpace(body.Note) == "" {
			http.Error(w, jsonError("note is required when requesting changes"), http.StatusBadRequest)
			return
		}

		userID, username, role := currentUserFromHeaders(r)
		step, err := getStepByWorkflowAndOrder(cs.WorkflowID, cs.CurrentStep)
		if err != nil || step == nil {
			http.Error(w, jsonError("current step not found"), http.StatusBadRequest)
			return
		}
		eligible, _ := isUserEligibleForStep(step.ID, userID, role)
		if !eligible {
			http.Error(w, jsonError("you are not eligible to act on this step"), http.StatusForbidden)
			return
		}
		alreadyActed, _ := hasUserActedOnChangeSetStep(id, step.ID, userID, cs.Revision)
		if alreadyActed {
			http.Error(w, jsonError("you have already acted on this step"), http.StatusBadRequest)
			return
		}

		_, err = appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO change_set_approval (change_set_id, step_id, revision, user_id, username, action, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`), id, step.ID, cs.Revision, userID, username, body.Action, strings.TrimSpace(body.Note))
		if err != nil {
			http.Error(w, jsonError("failed to record approval action"), http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		if body.Action == "rejected" {
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
				UPDATE change_sets
				SET status = 'rejected', reviewer_id = ?, review_note = ?, updated_at = ?
				WHERE id = ?
			`), userID, strings.TrimSpace(body.Note), now, id)
			json.NewEncoder(w).Encode(map[string]string{"message": "changes requested"})
			return
		}

		approvedCount, _ := countChangeSetStepApprovals(id, step.ID, cs.Revision)
		if approvedCount >= step.RequiredApprovals {
			totalSteps, _ := countWorkflowSteps(cs.WorkflowID)
			if cs.CurrentStep >= totalSteps {
				_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
					UPDATE change_sets
					SET status = 'approved', reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
					WHERE id = ?
				`), userID, strings.TrimSpace(body.Note), cs.CurrentStep+1, now, id)
				json.NewEncoder(w).Encode(map[string]string{"message": "all steps approved, change set is approved"})
				return
			}
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE change_sets SET current_step = ?, updated_at = ? WHERE id = ?`), cs.CurrentStep+1, now, id)
			json.NewEncoder(w).Encode(map[string]string{"message": "step approved, moved to next step"})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "approval recorded, awaiting more approvals"})
	}
}

func ExecuteChangeSet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseActionChangeSetID(r.URL.Path, "/execute")
		if err != nil {
			http.Error(w, jsonError("invalid change set id"), http.StatusBadRequest)
			return
		}
		cs, err := getChangeSetByID(id)
		if err != nil || cs == nil {
			http.Error(w, jsonError("change set not found"), http.StatusNotFound)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != cs.CreatorID {
			http.Error(w, jsonError("only the creator or an admin can execute this change set"), http.StatusForbidden)
			return
		}
		if cs.Status != QueryApprovalStatusApproved {
			http.Error(w, jsonError("only approved change sets can be executed"), http.StatusBadRequest)
			return
		}
		if role != "admin" {
			canManageApproved, err := canRequesterManageApprovedChangeSet(cs, userID, role)
			if err != nil {
				http.Error(w, jsonError("failed to validate approval permissions"), http.StatusInternalServerError)
				return
			}
			if !canManageApproved {
				http.Error(w, jsonError("only requesters included in the approver workflow can execute an approved change set"), http.StatusForbidden)
				return
			}
		}
		if !CheckWritePermission(r, cs.ConnID) {
			http.Error(w, jsonError("write permission denied"), http.StatusForbidden)
			return
		}

		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE change_sets SET status = 'executing', updated_at = ? WHERE id = ?`), time.Now().UTC().Format("2006-01-02 15:04:05"), id)

		db, driver, err := GetDB(cs.ConnID)
		if err != nil {
			markChangeSetExecution(id, QueryApprovalStatusFailed, "database connection error")
			http.Error(w, jsonError("database connection error"), http.StatusBadGateway)
			return
		}
		if cs.Database != "" {
			switch driver {
			case "mysql":
				safeName := strings.ReplaceAll(cs.Database, "`", "``")
				_, _ = db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				safeName := strings.ReplaceAll(cs.Database, "]", "]]")
				_, _ = db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
		}

		start := time.Now()
		res, execErr := db.ExecContext(r.Context(), cs.Statement)
		durationMs := time.Since(start).Milliseconds()
		if execErr != nil {
			markChangeSetExecution(id, QueryApprovalStatusFailed, sanitizeDBError(execErr))
			http.Error(w, jsonError(sanitizeDBError(execErr)), http.StatusBadRequest)
			return
		}

		affected, _ := res.RowsAffected()
		markChangeSetExecution(id, QueryApprovalStatusDone, "")
		go WriteAuditLog(r.Header.Get("X-Username"), cs.ConnID, cs.Connection, cs.Statement, durationMs, affected, "")

		updated, _ := getChangeSetByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func parseChangeSetID(path string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(path, "/api/change-sets/"), 10, 64)
}

func parseActionChangeSetID(path, suffix string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/change-sets/")
	trimmed = strings.TrimSuffix(trimmed, suffix)
	return strconv.ParseInt(trimmed, 10, 64)
}

func scanChangeSets(rows *sql.Rows) ([]ChangeSet, error) {
	var changeSets []ChangeSet
	for rows.Next() {
		var cs ChangeSet
		var reviewerID sql.NullInt64
		var validatedAt sql.NullTime
		var executedAt sql.NullTime
		if err := rows.Scan(
			&cs.ID, &cs.Title, &cs.Description, &cs.ConnID,
			&cs.Connection, &cs.Driver, &cs.Environment,
			&cs.Database, &cs.Statement, &cs.RollbackSQL, &cs.ImpactSummary, &cs.Status,
			&cs.CreatorID, &cs.CreatorName,
			&reviewerID, &cs.ReviewerName, &cs.ReviewNote,
			&cs.WorkflowID, &cs.CurrentStep, &cs.Revision,
			&cs.ValidationStatus, &cs.ValidationMessage, &validatedAt,
			&cs.ExecuteError, &executedAt, &cs.CreatedAt, &cs.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if reviewerID.Valid {
			cs.ReviewerID = &reviewerID.Int64
		}
		if validatedAt.Valid {
			t := validatedAt.Time
			cs.ValidatedAt = &t
		}
		if executedAt.Valid {
			t := executedAt.Time
			cs.ExecutedAt = &t
		}
		cs.Approvers, _ = listChangeSetApproverNames(cs.ID, cs.Revision)
		changeSets = append(changeSets, cs)
	}
	return changeSets, rows.Err()
}

func getChangeSetByID(id int64) (*ChangeSet, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT
			cs.id, cs.title, cs.description, cs.conn_id,
			COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, 'development'),
			cs.database_name, cs.statement, cs.rollback_sql, cs.impact_summary, cs.status,
			cs.creator_id, COALESCE(u1.username, ''),
			cs.reviewer_id, COALESCE(u2.username, ''), cs.review_note,
			COALESCE(cs.workflow_id, 0), cs.current_step, cs.revision,
			cs.validation_status, cs.validation_message, cs.validated_at,
			cs.execute_error, cs.executed_at, cs.created_at, cs.updated_at
		FROM change_sets cs
		LEFT JOIN connections c ON c.id = cs.conn_id
		LEFT JOIN users u1 ON u1.id = cs.creator_id
		LEFT JOIN users u2 ON u2.id = cs.reviewer_id
		WHERE cs.id = ?
	`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	changeSets, err := scanChangeSets(rows)
	if err != nil || len(changeSets) == 0 {
		return nil, err
	}
	return &changeSets[0], nil
}

func validateChangeSetTarget(ctx context.Context, connID int64, database, statement string) ChangeSetValidationResponse {
	statement = strings.TrimSpace(statement)
	summary := summarizeChangeImpact(statement)
	op := strings.ToUpper(firstSQLToken(statement))
	if statement == "" {
		return ChangeSetValidationResponse{
			OK:               false,
			ValidationStatus: ChangeSetValidationFailed,
			Message:          "SQL statement is required",
			ImpactSummary:    summary,
		}
	}
	if isReadOnlySQL(statement) {
		return ChangeSetValidationResponse{
			OK:               false,
			ValidationStatus: ChangeSetValidationFailed,
			Message:          "change sets are for write or schema change SQL, not read-only statements",
			ImpactSummary:    summary,
		}
	}

	db, driver, err := GetDB(connID)
	if err != nil {
		return ChangeSetValidationResponse{
			OK:               false,
			ValidationStatus: ChangeSetValidationFailed,
			Message:          "database connection error",
			ImpactSummary:    summary,
		}
	}
	if database != "" {
		if !validIdentifier.MatchString(database) {
			return ChangeSetValidationResponse{
				OK:               false,
				ValidationStatus: ChangeSetValidationFailed,
				Message:          "invalid database name",
				ImpactSummary:    summary,
			}
		}
		switch driver {
		case "mysql":
			safeName := strings.ReplaceAll(database, "`", "``")
			if _, err := db.ExecContext(ctx, "USE `"+safeName+"`"); err != nil {
				return ChangeSetValidationResponse{
					OK:               false,
					ValidationStatus: ChangeSetValidationFailed,
					Message:          sanitizeDBError(err),
					ImpactSummary:    summary,
				}
			}
		case "sqlserver":
			safeName := strings.ReplaceAll(database, "]", "]]")
			if _, err := db.ExecContext(ctx, "USE ["+safeName+"]"); err != nil {
				return ChangeSetValidationResponse{
					OK:               false,
					ValidationStatus: ChangeSetValidationFailed,
					Message:          sanitizeDBError(err),
					ImpactSummary:    summary,
				}
			}
		}
	}

	if shouldExplainForValidation(op) {
		query := explainPrefixForDriver(driver) + statement
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return ChangeSetValidationResponse{
				OK:               false,
				ValidationStatus: ChangeSetValidationFailed,
				Message:          sanitizeDBError(err),
				ImpactSummary:    summary,
			}
		}
		_ = rows.Close()
		return ChangeSetValidationResponse{
			OK:               true,
			ValidationStatus: ChangeSetValidationPassed,
			Message:          "validation passed and execution plan was accepted",
			ImpactSummary:    summary,
		}
	}

	return ChangeSetValidationResponse{
		OK:               true,
		ValidationStatus: ChangeSetValidationPassed,
		Message:          "connection verified; statement classified as schema change and passed heuristic validation",
		ImpactSummary:    summary,
	}
}

func explainPrefixForDriver(driver string) string {
	switch driver {
	case "sqlite":
		return "EXPLAIN QUERY PLAN "
	default:
		return "EXPLAIN "
	}
}

func shouldExplainForValidation(operation string) bool {
	switch operation {
	case "INSERT", "UPDATE", "DELETE":
		return true
	default:
		return false
	}
}

func isReadOnlySQL(statement string) bool {
	upper := strings.ToUpper(strings.TrimSpace(statement))
	return strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") ||
		strings.HasPrefix(upper, "EXPLAIN") ||
		strings.HasPrefix(upper, "PRAGMA")
}

func firstSQLToken(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

var sqlObjectPattern = regexp.MustCompile(`(?i)\b(?:TABLE|INTO|UPDATE|FROM|JOIN|ALTER TABLE|DROP TABLE|CREATE TABLE)\s+([a-zA-Z_][a-zA-Z0-9_\.]*)`)

func summarizeChangeImpact(statement string) string {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return ""
	}
	op := strings.ToUpper(firstSQLToken(statement))
	matches := sqlObjectPattern.FindAllStringSubmatch(statement, -1)
	seen := map[string]struct{}{}
	var objects []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		objects = append(objects, name)
	}

	summary := op
	if len(objects) > 0 {
		summary += " affecting " + strings.Join(objects, ", ")
	}
	switch op {
	case "UPDATE", "DELETE":
		if !strings.Contains(strings.ToUpper(statement), " WHERE ") {
			summary += " without WHERE clause"
		}
	case "DROP", "TRUNCATE":
		summary += " with destructive impact"
	case "ALTER", "CREATE":
		summary += " schema change"
	}
	return strings.TrimSpace(summary)
}

func listChangeSetApproverNames(changeSetID int64, revision int) ([]string, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT DISTINCT username
		FROM change_set_approval
		WHERE change_set_id = ? AND revision = ? AND action = 'approved'
		ORDER BY username ASC
	`), changeSetID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func listChangeSetApprovals(changeSetID int64, revision int) ([]ChangeApproval, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT csa.id, csa.change_set_id, csa.step_id, COALESCE(ws.name, ''), COALESCE(ws.step_order, 0),
			csa.revision, csa.user_id, csa.username, csa.action, csa.note, csa.created_at
		FROM change_set_approval csa
		LEFT JOIN workflow_step ws ON ws.id = csa.step_id
		WHERE csa.change_set_id = ? AND csa.revision = ?
		ORDER BY csa.created_at ASC
	`), changeSetID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []ChangeApproval
	for rows.Next() {
		var approval ChangeApproval
		if err := rows.Scan(&approval.ID, &approval.RequestID, &approval.StepID, &approval.StepName, &approval.StepOrder, &approval.Revision, &approval.UserID, &approval.Username, &approval.Action, &approval.Note, &approval.CreatedAt); err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	return approvals, rows.Err()
}

func hasUserActedOnChangeSetStep(changeSetID, stepID, userID int64, revision int) (bool, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM change_set_approval WHERE change_set_id = ? AND step_id = ? AND user_id = ? AND revision = ?`), changeSetID, stepID, userID, revision).Scan(&count)
	return count > 0, err
}

func countChangeSetStepApprovals(changeSetID, stepID int64, revision int) (int, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM change_set_approval WHERE change_set_id = ? AND step_id = ? AND action = 'approved' AND revision = ?`), changeSetID, stepID, revision).Scan(&count)
	return count, err
}

func canRequesterManageApprovedChangeSet(cs *ChangeSet, userID int64, role string) (bool, error) {
	if cs == nil {
		return false, nil
	}
	if role == "admin" {
		return true, nil
	}
	if userID == 0 || userID != cs.CreatorID || cs.WorkflowID == 0 {
		return false, nil
	}
	steps, err := listWorkflowSteps(cs.WorkflowID)
	if err != nil {
		return false, err
	}
	for _, step := range steps {
		eligible, err := isUserEligibleForStep(step.ID, userID, role)
		if err != nil {
			return false, err
		}
		if eligible {
			return true, nil
		}
	}
	return false, nil
}

func markChangeSetExecution(id int64, status QueryApprovalStatus, execError string) {
	now := time.Now().UTC()
	if status == QueryApprovalStatusDone {
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE change_sets
			SET status = ?, execute_error = ?, executed_at = ?, updated_at = ?
			WHERE id = ?
		`), status, execError, now.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"), id)
		return
	}
	_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
		UPDATE change_sets
		SET status = ?, execute_error = ?, updated_at = ?
		WHERE id = ?
	`), status, execError, now.Format("2006-01-02 15:04:05"), id)
}

func canViewChangeSet(r *http.Request, cs *ChangeSet) bool {
	if !isAuthEnabled() {
		return true
	}
	userID, _, role := currentUserFromHeaders(r)
	if role == "admin" || userID == cs.CreatorID {
		return true
	}
	if cs.WorkflowID == 0 {
		return false
	}
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`SELECT id FROM workflow_step WHERE workflow_id = ?`), cs.WorkflowID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var stepID int64
		if err := rows.Scan(&stepID); err != nil {
			return false
		}
		if ok, _ := isUserEligibleForStep(stepID, userID, role); ok {
			return true
		}
	}
	return false
}

func canEditChangeSet(r *http.Request, cs *ChangeSet) bool {
	if !isAuthEnabled() {
		return true
	}
	userID, _, role := currentUserFromHeaders(r)
	return role == "admin" || userID == cs.CreatorID
}
