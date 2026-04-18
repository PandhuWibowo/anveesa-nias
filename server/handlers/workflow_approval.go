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

func currentUserFromHeaders(r *http.Request) (int64, string, string) {
	userID, _ := strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	return userID, r.Header.Get("X-Username"), r.Header.Get("X-User-Role")
}

// appdb.ConvertQuery converts SQLite ? placeholders to PostgreSQL $1, $2, ... if needed

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !isAuthEnabled() {
		return true
	}
	if r.Header.Get("X-User-Role") == "admin" {
		return true
	}
	http.Error(w, `{"error":"admin access required"}`, http.StatusForbidden)
	return false
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parseSQLBool(v int) bool { return v == 1 }

func ListWorkflows() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`
			SELECT id, name, description, is_active, assign_all_groups, assign_all_connections, created_at, updated_at
			FROM approval_workflow
			ORDER BY id ASC
		`)
		if err != nil {
			http.Error(w, jsonError("failed to list workflows"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var workflows []ApprovalWorkflow
		var workflowIDs []int64
		
		for rows.Next() {
			var wf ApprovalWorkflow
			var isActive, allGroups, allConnections int
			if err := rows.Scan(&wf.ID, &wf.Name, &wf.Description, &isActive, &allGroups, &allConnections, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
				http.Error(w, jsonError("failed to scan workflow"), http.StatusInternalServerError)
				return
			}
			wf.IsActive = parseSQLBool(isActive)
			wf.AssignAllGroups = parseSQLBool(allGroups)
			wf.AssignAllConnections = parseSQLBool(allConnections)
			wf.Steps = []WorkflowStep{}
			wf.AccessGroups = []WorkflowAccessGroup{}
			wf.Connections = []WorkflowConnection{}
			workflows = append(workflows, wf)
			workflowIDs = append(workflowIDs, wf.ID)
		}
		
		// Close rows before making additional queries to release lock
		rows.Close()
		
		// Load related data for each workflow (after closing main query)
		for i := range workflows {
			workflows[i].Steps, _ = listWorkflowSteps(workflows[i].ID)
			workflows[i].AccessGroups, _ = listWorkflowAccessGroups(workflows[i].ID)
			workflows[i].Connections, _ = listWorkflowConnections(workflows[i].ID)
			if workflows[i].Steps == nil {
				workflows[i].Steps = []WorkflowStep{}
			}
			if workflows[i].AccessGroups == nil {
				workflows[i].AccessGroups = []WorkflowAccessGroup{}
			}
			if workflows[i].Connections == nil {
				workflows[i].Connections = []WorkflowConnection{}
			}
		}
		
		if workflows == nil {
			workflows = []ApprovalWorkflow{}
		}
		json.NewEncoder(w).Encode(workflows)
	}
}

func GetWorkflow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/workflows/"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid workflow id"), http.StatusBadRequest)
			return
		}
		wf, err := getWorkflowByID(id)
		if err != nil || wf == nil {
			http.Error(w, jsonError("workflow not found"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(wf)
	}
}

func CreateWorkflow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")

		var req CreateWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Name) == "" || len(req.Steps) == 0 {
			http.Error(w, jsonError("name and at least one step are required"), http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		tx, err := appdb.DB.Begin()
		if err != nil {
			http.Error(w, jsonError("failed to start transaction"), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		var workflowID int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			// Use RETURNING for PostgreSQL/MySQL
			err := tx.QueryRow(`
				INSERT INTO approval_workflow (name, description, is_active, assign_all_groups, assign_all_connections, created_at, updated_at)
				VALUES ($1, $2, 1, $3, $4, $5, $6) RETURNING id
			`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), boolInt(req.AssignAllGroups), boolInt(req.AssignAllConnections), now, now).Scan(&workflowID)
			if err != nil {
				http.Error(w, jsonError("failed to create workflow"), http.StatusInternalServerError)
				return
			}
		} else {
			// Use LastInsertId for SQLite
			res, err := tx.Exec(`
				INSERT INTO approval_workflow (name, description, is_active, assign_all_groups, assign_all_connections, created_at, updated_at)
				VALUES (?, ?, 1, ?, ?, ?, ?)
			`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), boolInt(req.AssignAllGroups), boolInt(req.AssignAllConnections), now, now)
			if err != nil {
				http.Error(w, jsonError("failed to create workflow"), http.StatusInternalServerError)
				return
			}
			workflowID, _ = res.LastInsertId()
		}

		if err := replaceWorkflowStepsTx(tx, workflowID, req.Steps); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := replaceWorkflowGroupsTx(tx, workflowID, req.AccessGroupIDs); err != nil {
			http.Error(w, jsonError("failed to save workflow groups"), http.StatusInternalServerError)
			return
		}
		if err := replaceWorkflowConnectionsTx(tx, workflowID, req.ConnectionIDs); err != nil {
			http.Error(w, jsonError("failed to save workflow connections"), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, jsonError("failed to commit workflow"), http.StatusInternalServerError)
			return
		}

		wf, _ := getWorkflowByID(workflowID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(wf)
	}
}

func UpdateWorkflow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/workflows/"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid workflow id"), http.StatusBadRequest)
			return
		}

		var req CreateWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Name) == "" || len(req.Steps) == 0 {
			http.Error(w, jsonError("name and at least one step are required"), http.StatusBadRequest)
			return
		}

		tx, err := appdb.DB.Begin()
		if err != nil {
			http.Error(w, jsonError("failed to start transaction"), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			UPDATE approval_workflow
			SET name = ?, description = ?, assign_all_groups = ?, assign_all_connections = ?, updated_at = ?
			WHERE id = ?
		`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), boolInt(req.AssignAllGroups), boolInt(req.AssignAllConnections), time.Now().UTC().Format("2006-01-02 15:04:05"), id); err != nil {
			http.Error(w, jsonError("failed to update workflow"), http.StatusInternalServerError)
			return
		}
		if err := replaceWorkflowStepsTx(tx, id, req.Steps); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := replaceWorkflowGroupsTx(tx, id, req.AccessGroupIDs); err != nil {
			http.Error(w, jsonError("failed to save workflow groups"), http.StatusInternalServerError)
			return
		}
		if err := replaceWorkflowConnectionsTx(tx, id, req.ConnectionIDs); err != nil {
			http.Error(w, jsonError("failed to save workflow connections"), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, jsonError("failed to commit workflow"), http.StatusInternalServerError)
			return
		}

		wf, _ := getWorkflowByID(id)
		json.NewEncoder(w).Encode(wf)
	}
}

func ToggleWorkflowActive() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/workflows/")
		path = strings.TrimSuffix(path, "/active")
		id, err := strconv.ParseInt(path, 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid workflow id"), http.StatusBadRequest)
			return
		}
		var body struct {
			IsActive bool `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`UPDATE approval_workflow SET is_active = ?, updated_at = ? WHERE id = ?`), boolInt(body.IsActive), time.Now().UTC().Format("2006-01-02 15:04:05"), id); err != nil {
			http.Error(w, jsonError("failed to update workflow"), http.StatusInternalServerError)
			return
		}
		wf, _ := getWorkflowByID(id)
		json.NewEncoder(w).Encode(wf)
	}
}

func DeleteWorkflow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/workflows/"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid workflow id"), http.StatusBadRequest)
			return
		}
		var count int
		_ = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM query_approval_request WHERE workflow_id = ?`), id).Scan(&count)
		if count > 0 {
			http.Error(w, jsonError("cannot delete workflow in use by approval requests"), http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM approval_workflow WHERE id = ?`), id); err != nil {
			http.Error(w, jsonError("failed to delete workflow"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ListApplicableWorkflows() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := strconv.ParseInt(r.URL.Query().Get("conn_id"), 10, 64)
		if err != nil || connID <= 0 {
			http.Error(w, jsonError("conn_id is required"), http.StatusBadRequest)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		workflows, err := findApplicableWorkflows(userID, role, connID)
		if err != nil {
			http.Error(w, jsonError("failed to list applicable workflows"), http.StatusInternalServerError)
			return
		}
		if workflows == nil {
			workflows = []ApprovalWorkflow{}
		}
		json.NewEncoder(w).Encode(workflows)
	}
}

func ListApprovalRequests() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, role := currentUserFromHeaders(r)

		query := `
			SELECT
				q.id, q.title, q.description, q.conn_id,
				COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, 'development'),
				q.database_name, q.statement, q.status,
				q.creator_id, COALESCE(u1.username, ''),
				q.reviewer_id, COALESCE(u2.username, ''), q.review_note,
				q.workflow_id, q.current_step, q.revision, q.execute_error, q.executed_at, q.created_at, q.updated_at
			FROM query_approval_request q
			LEFT JOIN connections c ON c.id = q.conn_id
			LEFT JOIN users u1 ON u1.id = q.creator_id
			LEFT JOIN users u2 ON u2.id = q.reviewer_id
		`
		var rows *sql.Rows
		var err error
		if !isAuthEnabled() || role == "admin" {
			query += ` ORDER BY q.created_at DESC`
			rows, err = appdb.DB.Query(query)
		} else {
			query += `
				WHERE q.creator_id = ?
				   OR EXISTS (
						SELECT 1
						FROM workflow_step ws
						JOIN step_approver sa ON sa.step_id = ws.id
						LEFT JOIN roles r ON sa.approver_type = 'role' AND sa.approver_id = r.id
						WHERE ws.workflow_id = q.workflow_id
						  AND ws.step_order = q.current_step
						  AND (
								(sa.approver_type = 'user' AND sa.approver_id = ?)
							 OR (sa.approver_type = 'role' AND COALESCE(r.name, '') = ?)
						  )
				   )
				ORDER BY q.created_at DESC
			`
			query = appdb.ConvertQuery(query)
			rows, err = appdb.DB.Query(query, userID, userID, role)
		}
		if err != nil {
			http.Error(w, jsonError("failed to list approval requests"), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		requests, err := scanApprovalRequests(rows)
		if err != nil {
			http.Error(w, jsonError("failed to read approval requests"), http.StatusInternalServerError)
			return
		}
		if requests == nil {
			requests = []QueryApprovalRequest{}
		}
		json.NewEncoder(w).Encode(requests)
	}
}

func GetApprovalRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/approval-requests/"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getApprovalRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("approval request not found"), http.StatusNotFound)
			return
		}
		if !canViewApprovalRequest(r, req) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(req)
	}
}

func CreateApprovalRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, username, role := currentUserFromHeaders(r)
		if isAuthEnabled() && userID == 0 {
			http.Error(w, jsonError("unauthorized"), http.StatusUnauthorized)
			return
		}

		var body CreateQueryApprovalRequest
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

		workflowID, err := resolveWorkflowID(userID, role, body.ConnID, body.WorkflowID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if workflowID == 0 {
			http.Error(w, jsonError("no applicable workflow found"), http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		
		var reqID int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			// Use RETURNING for PostgreSQL/MySQL
			err := appdb.DB.QueryRow(`
				INSERT INTO query_approval_request
					(title, description, conn_id, database_name, statement, status, creator_id, workflow_id, current_step, revision, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, 'pending_review', $6, $7, 1, 1, $8, $9) RETURNING id
			`, body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, userID, workflowID, now, now).Scan(&reqID)
			if err != nil {
				http.Error(w, jsonError("failed to create approval request"), http.StatusInternalServerError)
				return
			}
		} else {
			// Use LastInsertId for SQLite
			res, err := appdb.DB.Exec(`
				INSERT INTO query_approval_request
					(title, description, conn_id, database_name, statement, status, creator_id, workflow_id, current_step, revision, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, 'pending_review', ?, ?, 1, 1, ?, ?)
			`, body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, userID, workflowID, now, now)
			if err != nil {
				http.Error(w, jsonError("failed to create approval request"), http.StatusInternalServerError)
				return
			}
			reqID, _ = res.LastInsertId()
		}
		req, _ := getApprovalRequestByID(reqID)
		if req != nil && username != "" {
			req.CreatorName = username
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(req)
	}
}

func UpdateApprovalRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/approval-requests/"), 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getApprovalRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("approval request not found"), http.StatusNotFound)
			return
		}

		userID, _, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != req.CreatorID {
			http.Error(w, jsonError("only the creator or an admin can edit this request"), http.StatusForbidden)
			return
		}
		if role != "admin" && req.Status == QueryApprovalStatusApproved {
			canManageApproved, err := canRequesterManageApprovedRequest(req, userID, role)
			if err != nil {
				http.Error(w, jsonError("failed to validate approval permissions"), http.StatusInternalServerError)
				return
			}
			if !canManageApproved {
				http.Error(w, jsonError("only requesters included in the approver workflow can revise an approved request"), http.StatusForbidden)
				return
			}
		}
		if req.Status == QueryApprovalStatusExecuting || req.Status == QueryApprovalStatusDone {
			http.Error(w, jsonError("executing or completed requests cannot be edited"), http.StatusBadRequest)
			return
		}

		var body UpdateQueryApprovalRequest
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

		resolveRole := role
		var creatorRole string
		if err := appdb.DB.QueryRow(`SELECT role FROM users WHERE id = ?`, req.CreatorID).Scan(&creatorRole); err == nil && creatorRole != "" {
			resolveRole = creatorRole
		}
		workflowID, err := resolveWorkflowID(req.CreatorID, resolveRole, body.ConnID, body.WorkflowID)
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
			UPDATE query_approval_request
			SET title = ?, description = ?, conn_id = ?, database_name = ?, statement = ?,
				status = 'pending_review', workflow_id = ?, current_step = 1, reviewer_id = NULL,
				revision = revision + 1, execute_error = '', updated_at = ?
			WHERE id = ?
		`), body.Title, strings.TrimSpace(body.Description), body.ConnID, strings.TrimSpace(body.Database), body.Statement, workflowID, now, id)
		if err != nil {
			http.Error(w, jsonError("failed to update approval request"), http.StatusInternalServerError)
			return
		}

		updated, _ := getApprovalRequestByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func GetApprovalProgress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/approval-requests/")
		path = strings.TrimSuffix(path, "/approval-progress")
		id, err := strconv.ParseInt(path, 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getApprovalRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("approval request not found"), http.StatusNotFound)
			return
		}
		if !canViewApprovalRequest(r, req) {
			http.Error(w, jsonError("permission denied"), http.StatusForbidden)
			return
		}

		wf, err := getWorkflowByID(req.WorkflowID)
		if err != nil || wf == nil {
			http.Error(w, jsonError("workflow not found"), http.StatusInternalServerError)
			return
		}
		approvals, _ := listRequestApprovals(id, req.Revision)
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
			case approvedCount >= step.RequiredApprovals || step.StepOrder < req.CurrentStep:
				p.Status = "approved"
			case step.StepOrder > req.CurrentStep:
				p.Status = "waiting"
			}
			progress = append(progress, p)
		}

		userID, _, role := currentUserFromHeaders(r)
		canApprove := false
		if req.CurrentStep > 0 {
			if step, _ := getStepByWorkflowAndOrder(req.WorkflowID, req.CurrentStep); step != nil {
				eligible, _ := isUserEligibleForStep(step.ID, userID, role)
				alreadyActed, _ := hasUserActedOnStep(req.ID, step.ID, userID, req.Revision)
				canApprove = eligible && !alreadyActed
			}
		}
		canManageApproved := role == "admin"
		if !canManageApproved {
			canManageApproved, _ = canRequesterManageApprovedRequest(req, userID, role)
		}
		canExecute := req.Status == QueryApprovalStatusApproved && canManageApproved
		canRevise := req.Status != QueryApprovalStatusExecuting && req.Status != QueryApprovalStatusDone &&
			(role == "admin" || userID == req.CreatorID) &&
			(req.Status != QueryApprovalStatusApproved || canManageApproved)

		json.NewEncoder(w).Encode(map[string]any{
			"workflow_id":   req.WorkflowID,
			"workflow_name": wf.Name,
			"current_step":  req.CurrentStep,
			"total_steps":   len(wf.Steps),
			"progress":      progress,
			"approvals":     approvals,
			"can_approve":   canApprove,
			"can_execute":   canExecute,
			"can_revise":    canRevise,
		})
	}
}

func ApproveApprovalStep() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/approval-requests/")
		path = strings.TrimSuffix(path, "/approve-step")
		id, err := strconv.ParseInt(path, 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getApprovalRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("approval request not found"), http.StatusNotFound)
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
		step, err := getStepByWorkflowAndOrder(req.WorkflowID, req.CurrentStep)
		if err != nil || step == nil {
			http.Error(w, jsonError("current step not found"), http.StatusBadRequest)
			return
		}
		eligible, _ := isUserEligibleForStep(step.ID, userID, role)
		if !eligible {
			http.Error(w, jsonError("you are not eligible to act on this step"), http.StatusForbidden)
			return
		}
		alreadyActed, _ := hasUserActedOnStep(id, step.ID, userID, req.Revision)
		if alreadyActed {
			http.Error(w, jsonError("you have already acted on this step"), http.StatusBadRequest)
			return
		}

		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO query_approval (request_id, step_id, revision, user_id, username, action, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`), id, step.ID, req.Revision, userID, username, body.Action, strings.TrimSpace(body.Note)); err != nil {
			http.Error(w, jsonError("failed to record approval action"), http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		if body.Action == "rejected" {
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
				UPDATE query_approval_request
				SET status = 'rejected', reviewer_id = ?, review_note = ?, updated_at = ?
				WHERE id = ?
			`), userID, strings.TrimSpace(body.Note), now, id)
			json.NewEncoder(w).Encode(map[string]string{"message": "changes requested"})
			return
		}

		approvedCount, _ := countStepApprovals(id, step.ID, req.Revision)
		if approvedCount >= step.RequiredApprovals {
			totalSteps, _ := countWorkflowSteps(req.WorkflowID)
			if req.CurrentStep >= totalSteps {
				_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
					UPDATE query_approval_request
					SET status = 'approved', reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
					WHERE id = ?
				`), userID, strings.TrimSpace(body.Note), req.CurrentStep+1, now, id)
				json.NewEncoder(w).Encode(map[string]string{"message": "all steps approved, request is approved"})
				return
			}
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE query_approval_request SET current_step = ?, updated_at = ? WHERE id = ?`), req.CurrentStep+1, now, id)
			json.NewEncoder(w).Encode(map[string]string{"message": "step approved, moved to next step"})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "approval recorded, awaiting more approvals"})
	}
}

func ExecuteApprovalRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/approval-requests/")
		path = strings.TrimSuffix(path, "/execute")
		id, err := strconv.ParseInt(path, 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getApprovalRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("approval request not found"), http.StatusNotFound)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		if isAuthEnabled() && role != "admin" && userID != req.CreatorID {
			http.Error(w, jsonError("only the creator or an admin can execute this request"), http.StatusForbidden)
			return
		}
		if req.Status != QueryApprovalStatusApproved {
			http.Error(w, jsonError("only approved requests can be executed"), http.StatusBadRequest)
			return
		}
		if role != "admin" {
			canManageApproved, err := canRequesterManageApprovedRequest(req, userID, role)
			if err != nil {
				http.Error(w, jsonError("failed to validate approval permissions"), http.StatusInternalServerError)
				return
			}
			if !canManageApproved {
				http.Error(w, jsonError("only requesters included in the approver workflow can execute an approved request"), http.StatusForbidden)
				return
			}
		}
		if !CheckWritePermission(r, req.ConnID) {
			http.Error(w, jsonError("write permission denied"), http.StatusForbidden)
			return
		}

		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE query_approval_request SET status = 'executing', updated_at = ? WHERE id = ?`), time.Now().UTC().Format("2006-01-02 15:04:05"), id)

		db, driver, err := GetDB(req.ConnID)
		if err != nil {
			markApprovalRequestExecution(id, QueryApprovalStatusFailed, "database connection error")
			http.Error(w, jsonError("database connection error"), http.StatusBadGateway)
			return
		}
		if req.Database != "" {
			switch driver {
			case "mysql":
				safeName := strings.ReplaceAll(req.Database, "`", "``")
				_, _ = db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				safeName := strings.ReplaceAll(req.Database, "]", "]]")
				_, _ = db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
		}

		start := time.Now()
		res, execErr := db.ExecContext(r.Context(), req.Statement)
		durationMs := time.Since(start).Milliseconds()
		if execErr != nil {
			markApprovalRequestExecution(id, QueryApprovalStatusFailed, sanitizeDBError(execErr))
			http.Error(w, jsonError(sanitizeDBError(execErr)), http.StatusBadRequest)
			return
		}

		affected, _ := res.RowsAffected()
		markApprovalRequestExecution(id, QueryApprovalStatusDone, "")
		go WriteAuditLog(r.Header.Get("X-Username"), req.ConnID, req.Connection, req.Statement, durationMs, affected, "")

		updated, _ := getApprovalRequestByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func getWorkflowByID(id int64) (*ApprovalWorkflow, error) {
	var wf ApprovalWorkflow
	var isActive, allGroups, allConnections int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, name, description, is_active, assign_all_groups, assign_all_connections, created_at, updated_at
		FROM approval_workflow
		WHERE id = ?
	`), id).Scan(&wf.ID, &wf.Name, &wf.Description, &isActive, &allGroups, &allConnections, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	wf.IsActive = parseSQLBool(isActive)
	wf.AssignAllGroups = parseSQLBool(allGroups)
	wf.AssignAllConnections = parseSQLBool(allConnections)
	wf.Steps, _ = listWorkflowSteps(wf.ID)
	wf.AccessGroups, _ = listWorkflowAccessGroups(wf.ID)
	wf.Connections, _ = listWorkflowConnections(wf.ID)
	if wf.Steps == nil {
		wf.Steps = []WorkflowStep{}
	}
	if wf.AccessGroups == nil {
		wf.AccessGroups = []WorkflowAccessGroup{}
	}
	if wf.Connections == nil {
		wf.Connections = []WorkflowConnection{}
	}
	return &wf, nil
}

func listWorkflowSteps(workflowID int64) ([]WorkflowStep, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT id, workflow_id, step_order, name, required_approvals
		FROM workflow_step
		WHERE workflow_id = ?
		ORDER BY step_order ASC
	`), workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []WorkflowStep
	for rows.Next() {
		var step WorkflowStep
		if err := rows.Scan(&step.ID, &step.WorkflowID, &step.StepOrder, &step.Name, &step.RequiredApprovals); err != nil {
			return nil, err
		}
		step.Approvers, _ = listStepApprovers(step.ID)
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func listStepApprovers(stepID int64) ([]StepApprover, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT sa.id, sa.step_id, sa.approver_type, sa.approver_id,
			CASE
				WHEN sa.approver_type = 'role' THEN COALESCE(r.name, 'unknown')
				WHEN sa.approver_type = 'user' THEN COALESCE(u.username, 'unknown')
				ELSE 'unknown'
			END
		FROM step_approver sa
		LEFT JOIN roles r ON sa.approver_type = 'role' AND sa.approver_id = r.id
		LEFT JOIN users u ON sa.approver_type = 'user' AND sa.approver_id = u.id
		WHERE sa.step_id = ?
	`), stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvers []StepApprover
	for rows.Next() {
		var approver StepApprover
		if err := rows.Scan(&approver.ID, &approver.StepID, &approver.ApproverType, &approver.ApproverID, &approver.ApproverName); err != nil {
			return nil, err
		}
		approvers = append(approvers, approver)
	}
	return approvers, rows.Err()
}

func listWorkflowAccessGroups(workflowID int64) ([]WorkflowAccessGroup, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT wf.folder_id, COALESCE(f.name, 'unknown')
		FROM workflow_folder wf
		LEFT JOIN connection_folders f ON f.id = wf.folder_id
		WHERE wf.workflow_id = ?
		ORDER BY f.name ASC
	`), workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []WorkflowAccessGroup
	for rows.Next() {
		var group WorkflowAccessGroup
		if err := rows.Scan(&group.GroupID, &group.GroupName); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func listWorkflowConnections(workflowID int64) ([]WorkflowConnection, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT wc.conn_id, COALESCE(c.name, 'unknown'), COALESCE(c.driver, ''), COALESCE(c.environment, 'development')
		FROM workflow_connection wc
		LEFT JOIN connections c ON c.id = wc.conn_id
		WHERE wc.workflow_id = ?
		ORDER BY c.name ASC
	`), workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []WorkflowConnection
	for rows.Next() {
		var conn WorkflowConnection
		if err := rows.Scan(&conn.ConnID, &conn.Name, &conn.Driver, &conn.Environment); err != nil {
			return nil, err
		}
		connections = append(connections, conn)
	}
	return connections, rows.Err()
}

func replaceWorkflowStepsTx(tx *sql.Tx, workflowID int64, steps []CreateWorkflowStepReq) error {
	if _, err := tx.Exec(appdb.ConvertQuery(`DELETE FROM workflow_step WHERE workflow_id = ?`), workflowID); err != nil {
		return err
	}
	for i, step := range steps {
		if strings.TrimSpace(step.Name) == "" || len(step.Approvers) == 0 {
			return fmt.Errorf("each step requires a name and at least one approver")
		}
		required := step.RequiredApprovals
		if required < 1 {
			required = 1
		}
		
		var stepID int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			// Use RETURNING for PostgreSQL/MySQL
			err := tx.QueryRow(`INSERT INTO workflow_step (workflow_id, step_order, name, required_approvals) VALUES ($1, $2, $3, $4) RETURNING id`, workflowID, i+1, strings.TrimSpace(step.Name), required).Scan(&stepID)
			if err != nil {
				return err
			}
		} else {
			// Use LastInsertId for SQLite
			res, err := tx.Exec(`INSERT INTO workflow_step (workflow_id, step_order, name, required_approvals) VALUES (?, ?, ?, ?)`, workflowID, i+1, strings.TrimSpace(step.Name), required)
			if err != nil {
				return err
			}
			stepID, _ = res.LastInsertId()
		}
		for _, approver := range step.Approvers {
			if approver.ApproverType != "role" && approver.ApproverType != "user" {
				return fmt.Errorf("invalid approver type")
			}
			if _, err := tx.Exec(appdb.ConvertQuery(`INSERT INTO step_approver (step_id, approver_type, approver_id) VALUES (?, ?, ?)`), stepID, approver.ApproverType, approver.ApproverID); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceWorkflowGroupsTx(tx *sql.Tx, workflowID int64, groupIDs []int64) error {
	if _, err := tx.Exec(appdb.ConvertQuery(`DELETE FROM workflow_folder WHERE workflow_id = ?`), workflowID); err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		query := `INSERT OR IGNORE INTO workflow_folder (workflow_id, folder_id) VALUES (?, ?)`
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			query = `INSERT INTO workflow_folder (workflow_id, folder_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		}
		if _, err := tx.Exec(query, workflowID, groupID); err != nil {
			return err
		}
	}
	return nil
}

func replaceWorkflowConnectionsTx(tx *sql.Tx, workflowID int64, connIDs []int64) error {
	if _, err := tx.Exec(appdb.ConvertQuery(`DELETE FROM workflow_connection WHERE workflow_id = ?`), workflowID); err != nil {
		return err
	}
	for _, connID := range connIDs {
		query := `INSERT OR IGNORE INTO workflow_connection (workflow_id, conn_id) VALUES (?, ?)`
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			query = `INSERT INTO workflow_connection (workflow_id, conn_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		}
		if _, err := tx.Exec(query, workflowID, connID); err != nil {
			return err
		}
	}
	return nil
}

func findApplicableWorkflows(userID int64, role string, connID int64) ([]ApprovalWorkflow, error) {
	if role == "admin" {
		rows, err := appdb.DB.Query(appdb.ConvertQuery(`
			SELECT id, name
			FROM approval_workflow
			WHERE is_active = 1
			  AND (assign_all_connections = 1 OR id IN (SELECT workflow_id FROM workflow_connection WHERE conn_id = ?))
			ORDER BY name ASC
		`), connID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var workflows []ApprovalWorkflow
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			wf, _ := getWorkflowByID(id)
			if wf != nil {
				workflows = append(workflows, *wf)
			}
		}
		return workflows, rows.Err()
	}

	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT DISTINCT aw.id, aw.name
		FROM approval_workflow aw
		WHERE aw.is_active = 1
		  AND (aw.assign_all_connections = 1 OR aw.id IN (
				SELECT workflow_id FROM workflow_connection WHERE conn_id = ?
		  ))
		  AND (
				aw.assign_all_groups = 1 OR aw.id IN (
					SELECT wf.workflow_id
					FROM workflow_folder wf
					JOIN connection_folders f ON f.id = wf.folder_id
					LEFT JOIN folder_members fm ON fm.folder_id = f.id
					WHERE (fm.user_id = ? OR f.owner_id = ?)
					  AND COALESCE(f.is_active, 1) = 1
					  AND (COALESCE(f.role_restrict, '') = '' OR f.role_restrict = ?)
				)
		  )
		ORDER BY aw.name ASC
	`), connID, userID, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []ApprovalWorkflow
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		wf, _ := getWorkflowByID(id)
		if wf != nil {
			workflows = append(workflows, *wf)
		}
	}
	return workflows, rows.Err()
}

func resolveWorkflowID(userID int64, role string, connID, explicitID int64) (int64, error) {
	workflows, err := findApplicableWorkflows(userID, role, connID)
	if err != nil {
		return 0, err
	}
	if explicitID > 0 {
		for _, wf := range workflows {
			if wf.ID == explicitID {
				return explicitID, nil
			}
		}
		return 0, fmt.Errorf("selected workflow is not applicable for this connection")
	}
	if len(workflows) == 1 {
		return workflows[0].ID, nil
	}
	if len(workflows) > 1 {
		return 0, fmt.Errorf("multiple workflows apply; select one explicitly")
	}
	return 0, nil
}

func scanApprovalRequests(rows *sql.Rows) ([]QueryApprovalRequest, error) {
	var requests []QueryApprovalRequest
	for rows.Next() {
		var req QueryApprovalRequest
		var reviewerID sql.NullInt64
		var executedAt sql.NullTime
		if err := rows.Scan(
			&req.ID, &req.Title, &req.Description, &req.ConnID,
			&req.Connection, &req.Driver, &req.Environment,
			&req.Database, &req.Statement, &req.Status,
			&req.CreatorID, &req.CreatorName,
			&reviewerID, &req.ReviewerName, &req.ReviewNote,
			&req.WorkflowID, &req.CurrentStep, &req.Revision, &req.ExecuteError, &executedAt, &req.CreatedAt, &req.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if reviewerID.Valid {
			req.ReviewerID = &reviewerID.Int64
		}
		if executedAt.Valid {
			t := executedAt.Time
			req.ExecutedAt = &t
		}
		req.Approvers, _ = listRequestApproverNames(req.ID, req.Revision)
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func getApprovalRequestByID(id int64) (*QueryApprovalRequest, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT
			q.id, q.title, q.description, q.conn_id,
			COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, 'development'),
			q.database_name, q.statement, q.status,
			q.creator_id, COALESCE(u1.username, ''),
			q.reviewer_id, COALESCE(u2.username, ''), q.review_note,
			q.workflow_id, q.current_step, q.revision, q.execute_error, q.executed_at, q.created_at, q.updated_at
		FROM query_approval_request q
		LEFT JOIN connections c ON c.id = q.conn_id
		LEFT JOIN users u1 ON u1.id = q.creator_id
		LEFT JOIN users u2 ON u2.id = q.reviewer_id
		WHERE q.id = ?
	`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests, err := scanApprovalRequests(rows)
	if err != nil || len(requests) == 0 {
		return nil, err
	}
	return &requests[0], nil
}

func listRequestApproverNames(requestID int64, revision int) ([]string, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT DISTINCT username
		FROM query_approval
		WHERE request_id = ? AND revision = ? AND action = 'approved'
		ORDER BY username ASC
	`), requestID, revision)
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

func listRequestApprovals(requestID int64, revision int) ([]ChangeApproval, error) {
	rows, err := appdb.DB.Query(`
		SELECT qa.id, qa.request_id, qa.step_id, COALESCE(ws.name, ''), COALESCE(ws.step_order, 0),
			qa.revision, qa.user_id, qa.username, qa.action, qa.note, qa.created_at
		FROM query_approval qa
		LEFT JOIN workflow_step ws ON ws.id = qa.step_id
		WHERE qa.request_id = ? AND qa.revision = ?
		ORDER BY qa.created_at ASC
	`, requestID, revision)
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

func getStepByWorkflowAndOrder(workflowID int64, order int) (*WorkflowStep, error) {
	var step WorkflowStep
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT id, workflow_id, step_order, name, required_approvals
		FROM workflow_step
		WHERE workflow_id = ? AND step_order = ?
	`), workflowID, order).Scan(&step.ID, &step.WorkflowID, &step.StepOrder, &step.Name, &step.RequiredApprovals)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	step.Approvers, _ = listStepApprovers(step.ID)
	return &step, nil
}

func isUserEligibleForStep(stepID, userID int64, role string) (bool, error) {
	approvers, err := listStepApprovers(stepID)
	if err != nil {
		return false, err
	}
	for _, approver := range approvers {
		if approver.ApproverType == "user" && approver.ApproverID == userID {
			return true, nil
		}
		if approver.ApproverType == "role" && approver.ApproverName == role {
			return true, nil
		}
	}
	return false, nil
}

func hasUserActedOnStep(requestID, stepID, userID int64, revision int) (bool, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM query_approval WHERE request_id = ? AND step_id = ? AND user_id = ? AND revision = ?`), requestID, stepID, userID, revision).Scan(&count)
	return count > 0, err
}

func countStepApprovals(requestID, stepID int64, revision int) (int, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM query_approval WHERE request_id = ? AND step_id = ? AND action = 'approved' AND revision = ?`), requestID, stepID, revision).Scan(&count)
	return count, err
}

func canRequesterManageApprovedRequest(req *QueryApprovalRequest, userID int64, role string) (bool, error) {
	if req == nil {
		return false, nil
	}
	if role == "admin" {
		return true, nil
	}
	if userID == 0 || userID != req.CreatorID {
		return false, nil
	}
	steps, err := listWorkflowSteps(req.WorkflowID)
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

func countWorkflowSteps(workflowID int64) (int, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM workflow_step WHERE workflow_id = ?`), workflowID).Scan(&count)
	return count, err
}

func markApprovalRequestExecution(id int64, status QueryApprovalStatus, execError string) {
	now := time.Now().UTC()
	if status == QueryApprovalStatusDone {
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE query_approval_request
			SET status = ?, execute_error = ?, executed_at = ?, updated_at = ?
			WHERE id = ?
		`), status, execError, now.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"), id)
		return
	}
	_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
		UPDATE query_approval_request
		SET status = ?, execute_error = ?, updated_at = ?
		WHERE id = ?
	`), status, execError, now.Format("2006-01-02 15:04:05"), id)
}

func canViewApprovalRequest(r *http.Request, req *QueryApprovalRequest) bool {
	if !isAuthEnabled() {
		return true
	}
	userID, _, role := currentUserFromHeaders(r)
	if role == "admin" || userID == req.CreatorID {
		return true
	}
	rows, err := appdb.DB.Query(`
		SELECT ws.id
		FROM workflow_step ws
		WHERE ws.workflow_id = ?
	`, req.WorkflowID)
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
