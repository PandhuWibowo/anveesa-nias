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
	dbpkg "github.com/anveesa/nias/db"
)

type BackupDownloadRequest struct {
	ID           int64               `json:"id"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	ConnID       int64               `json:"conn_id"`
	Connection   string              `json:"connection"`
	Driver       string              `json:"driver"`
	Environment  string              `json:"environment"`
	DatabaseName string              `json:"database_name"`
	Status       QueryApprovalStatus `json:"status"`
	CreatorID    int64               `json:"creator_id"`
	CreatorName  string              `json:"creator_name"`
	ReviewerID   *int64              `json:"reviewer_id,omitempty"`
	ReviewerName string              `json:"reviewer_name,omitempty"`
	ReviewNote   string              `json:"review_note"`
	WorkflowID   int64               `json:"workflow_id"`
	CurrentStep  int                 `json:"current_step"`
	Revision     int                 `json:"revision"`
	ExecuteError string              `json:"execute_error"`
	ExecutedAt   *time.Time          `json:"executed_at,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type CreateBackupDownloadRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ConnID      int64  `json:"conn_id"`
	Database    string `json:"database"`
	WorkflowID  int64  `json:"workflow_id"`
}

type ReviewBackupDownloadRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

func ListBackupDownloadRequests() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, role := currentUserFromHeaders(r)
		includeAll := role == "admin" || dbpkg.HasUserAppPermission(userID, PermQueryApprove)
		requests, err := listBackupDownloadRequests(userID, includeAll)
		if err != nil {
			http.Error(w, jsonError("failed to list backup download requests"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(requests)
	}
}

func CreateBackupDownloadRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, _, role := currentUserFromHeaders(r)
		var body CreateBackupDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		body.Description = strings.TrimSpace(body.Description)
		body.Database = strings.TrimSpace(body.Database)
		if body.ConnID <= 0 {
			http.Error(w, jsonError("conn_id is required"), http.StatusBadRequest)
			return
		}
		if body.Title == "" {
			body.Title = fmt.Sprintf("Download backup for connection #%d", body.ConnID)
		}
		if body.Database != "" && !validIdentifier.MatchString(body.Database) {
			http.Error(w, jsonError("invalid database name"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, body.ConnID) {
			http.Error(w, jsonError("connection access denied"), http.StatusForbidden)
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
		var requestID int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err = appdb.DB.QueryRow(`
				INSERT INTO backup_download_requests
					(title, description, conn_id, database_name, status, creator_id, workflow_id, current_step, revision, created_at, updated_at)
				VALUES ($1, $2, $3, $4, 'pending_review', $5, $6, 1, 1, $7, $8)
				RETURNING id
			`, body.Title, body.Description, body.ConnID, body.Database, userID, workflowID, now, now).Scan(&requestID)
		} else {
			res, execErr := appdb.DB.Exec(appdb.ConvertQuery(`
				INSERT INTO backup_download_requests
					(title, description, conn_id, database_name, status, creator_id, workflow_id, current_step, revision, created_at, updated_at)
				VALUES (?, ?, ?, ?, 'pending_review', ?, ?, 1, 1, ?, ?)
			`), body.Title, body.Description, body.ConnID, body.Database, userID, workflowID, now, now)
			err = execErr
			if err == nil {
				requestID, _ = res.LastInsertId()
			}
		}
		if err != nil {
			http.Error(w, jsonError("failed to create backup download request"), http.StatusInternalServerError)
			return
		}
		req, _ := getBackupDownloadRequestByID(requestID)
		json.NewEncoder(w).Encode(req)
	}
}

func GetBackupDownloadRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseBackupDownloadRequestID(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getBackupDownloadRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("backup download request not found"), http.StatusNotFound)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		if role != "admin" && !dbpkg.HasUserAppPermission(userID, PermQueryApprove) && userID != req.CreatorID {
			http.Error(w, jsonError("access denied"), http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(req)
	}
}

func ReviewBackupDownloadRequestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := parseBackupDownloadRequestActionID(r.URL.Path, "/review")
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getBackupDownloadRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("backup download request not found"), http.StatusNotFound)
			return
		}
		if req.Status != QueryApprovalStatusPendingReview {
			http.Error(w, jsonError("only pending review requests can be reviewed"), http.StatusBadRequest)
			return
		}
		userID, username, role := currentUserFromHeaders(r)
		var body ReviewBackupDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request"), http.StatusBadRequest)
			return
		}
		body.Action = strings.TrimSpace(body.Action)
		body.Note = strings.TrimSpace(body.Note)
		if body.Action != "approved" && body.Action != "rejected" {
			http.Error(w, jsonError("action must be approved or rejected"), http.StatusBadRequest)
			return
		}
		if body.Action == "rejected" && body.Note == "" {
			http.Error(w, jsonError("review note is required when rejecting"), http.StatusBadRequest)
			return
		}
		step, err := getStepByWorkflowAndOrder(req.WorkflowID, req.CurrentStep)
		if err != nil || step == nil {
			http.Error(w, jsonError("current approval step not found"), http.StatusBadRequest)
			return
		}
		eligible, _ := isUserEligibleForStep(step.ID, userID, role)
		if !eligible {
			http.Error(w, jsonError("you are not eligible to review this step"), http.StatusForbidden)
			return
		}
		alreadyActed, _ := hasUserActedOnBackupDownloadStep(req.ID, step.ID, userID, req.Revision)
		if alreadyActed {
			http.Error(w, jsonError("you have already acted on this step"), http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`
			INSERT INTO backup_download_approval (request_id, step_id, revision, user_id, username, action, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`), req.ID, step.ID, req.Revision, userID, username, body.Action, body.Note); err != nil {
			http.Error(w, jsonError("failed to record review action"), http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		if body.Action == "rejected" {
			_, err = appdb.DB.Exec(appdb.ConvertQuery(`
				UPDATE backup_download_requests
				SET status = 'rejected', reviewer_id = ?, review_note = ?, updated_at = ?
				WHERE id = ?
			`), userID, body.Note, now, id)
		} else {
			approvedCount, _ := countBackupDownloadStepApprovals(req.ID, step.ID, req.Revision)
			if approvedCount >= step.RequiredApprovals {
				totalSteps, _ := countWorkflowSteps(req.WorkflowID)
				if req.CurrentStep >= totalSteps {
					_, err = appdb.DB.Exec(appdb.ConvertQuery(`
						UPDATE backup_download_requests
						SET status = 'approved', reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
						WHERE id = ?
					`), userID, body.Note, req.CurrentStep+1, now, id)
				} else {
					_, err = appdb.DB.Exec(appdb.ConvertQuery(`
						UPDATE backup_download_requests
						SET reviewer_id = ?, review_note = ?, current_step = ?, updated_at = ?
						WHERE id = ?
					`), userID, body.Note, req.CurrentStep+1, now, id)
				}
			}
		}
		if err != nil {
			http.Error(w, jsonError("failed to update approval"), http.StatusInternalServerError)
			return
		}
		updated, _ := getBackupDownloadRequestByID(id)
		json.NewEncoder(w).Encode(updated)
	}
}

func DownloadApprovedBackupRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseBackupDownloadRequestActionID(r.URL.Path, "/download")
		if err != nil {
			http.Error(w, jsonError("invalid request id"), http.StatusBadRequest)
			return
		}
		req, err := getBackupDownloadRequestByID(id)
		if err != nil || req == nil {
			http.Error(w, jsonError("backup download request not found"), http.StatusNotFound)
			return
		}
		userID, _, role := currentUserFromHeaders(r)
		if role != "admin" && userID != req.CreatorID {
			http.Error(w, jsonError("only the requester or admin can download this backup"), http.StatusForbidden)
			return
		}
		if req.Status != QueryApprovalStatusApproved && req.Status != QueryApprovalStatusDone {
			http.Error(w, jsonError("backup download request is not approved"), http.StatusBadRequest)
			return
		}
		if !CheckReadPermission(r, req.ConnID) {
			http.Error(w, jsonError("connection access denied"), http.StatusForbidden)
			return
		}
		db, driver, err := GetDB(req.ConnID)
		if err != nil {
			http.Error(w, jsonError("database connection error"), http.StatusBadGateway)
			return
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE backup_download_requests SET status = 'executing', updated_at = ? WHERE id = ?`), now, id)
		filename := fmt.Sprintf("backup_%s_%s.sql", req.DatabaseName, time.Now().Format("20060102_150405"))
		if strings.TrimSpace(req.DatabaseName) == "" {
			filename = fmt.Sprintf("backup_conn_%d_%s.sql", req.ConnID, time.Now().Format("20060102_150405"))
		}
		w.Header().Set("Content-Type", "application/sql")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		if err := writeBackupDump(r.Context(), w, db, driver, req.DatabaseName); err != nil {
			_, _ = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE backup_download_requests SET status = 'failed', execute_error = ?, updated_at = ? WHERE id = ?`), sanitizeDBError(err), time.Now().UTC().Format("2006-01-02 15:04:05"), id)
			return
		}
		_, _ = appdb.DB.Exec(appdb.ConvertQuery(`
			UPDATE backup_download_requests
			SET status = 'done', execute_error = '', executed_at = ?, updated_at = ?
			WHERE id = ?
		`), now, now, id)
	}
}

func listBackupDownloadRequests(userID int64, includeAll bool) ([]BackupDownloadRequest, error) {
	query := `
		SELECT
			r.id, r.title, r.description, r.conn_id, COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, ''),
			r.database_name, r.status, r.creator_id, COALESCE(u1.username, ''), r.reviewer_id, COALESCE(u2.username, ''),
			r.review_note, COALESCE(r.workflow_id, 0), r.current_step, r.revision, r.execute_error, r.executed_at, r.created_at, r.updated_at
		FROM backup_download_requests r
		LEFT JOIN connections c ON c.id = r.conn_id
		LEFT JOIN users u1 ON u1.id = r.creator_id
		LEFT JOIN users u2 ON u2.id = r.reviewer_id
	`
	var rows *sql.Rows
	var err error
	if includeAll {
		rows, err = appdb.DB.Query(appdb.ConvertQuery(query + ` ORDER BY r.created_at DESC, r.id DESC`))
	} else {
		rows, err = appdb.DB.Query(appdb.ConvertQuery(query+` WHERE r.creator_id = ? ORDER BY r.created_at DESC, r.id DESC`), userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupDownloadRequests(rows)
}

func getBackupDownloadRequestByID(id int64) (*BackupDownloadRequest, error) {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT
			r.id, r.title, r.description, r.conn_id, COALESCE(c.name, ''), COALESCE(c.driver, ''), COALESCE(c.environment, ''),
			r.database_name, r.status, r.creator_id, COALESCE(u1.username, ''), r.reviewer_id, COALESCE(u2.username, ''),
			r.review_note, COALESCE(r.workflow_id, 0), r.current_step, r.revision, r.execute_error, r.executed_at, r.created_at, r.updated_at
		FROM backup_download_requests r
		LEFT JOIN connections c ON c.id = r.conn_id
		LEFT JOIN users u1 ON u1.id = r.creator_id
		LEFT JOIN users u2 ON u2.id = r.reviewer_id
		WHERE r.id = ?
	`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests, err := scanBackupDownloadRequests(rows)
	if err != nil || len(requests) == 0 {
		return nil, err
	}
	return &requests[0], nil
}

func scanBackupDownloadRequests(rows *sql.Rows) ([]BackupDownloadRequest, error) {
	var requests []BackupDownloadRequest
	for rows.Next() {
		var item BackupDownloadRequest
		var reviewerID sql.NullInt64
		var reviewerName sql.NullString
		var executedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Description, &item.ConnID, &item.Connection, &item.Driver, &item.Environment,
			&item.DatabaseName, &item.Status, &item.CreatorID, &item.CreatorName, &reviewerID, &reviewerName,
			&item.ReviewNote, &item.WorkflowID, &item.CurrentStep, &item.Revision, &item.ExecuteError, &executedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
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
		requests = append(requests, item)
	}
	return requests, rows.Err()
}

func parseBackupDownloadRequestID(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/backup-download-requests/")
	return strconv.ParseInt(trimmed, 10, 64)
}

func parseBackupDownloadRequestActionID(path, suffix string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/backup-download-requests/")
	trimmed = strings.TrimSuffix(trimmed, suffix)
	return strconv.ParseInt(trimmed, 10, 64)
}

func hasUserActedOnBackupDownloadStep(requestID, stepID, userID int64, revision int) (bool, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM backup_download_approval WHERE request_id = ? AND step_id = ? AND user_id = ? AND revision = ?`), requestID, stepID, userID, revision).Scan(&count)
	return count > 0, err
}

func countBackupDownloadStepApprovals(requestID, stepID int64, revision int) (int, error) {
	var count int
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COUNT(*) FROM backup_download_approval WHERE request_id = ? AND step_id = ? AND action = 'approved' AND revision = ?`), requestID, stepID, revision).Scan(&count)
	return count, err
}
