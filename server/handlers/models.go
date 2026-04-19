package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anveesa/nias/db"
)

// ── Database Operation Permissions ──

// DbPerm represents a database operation permission.
type DbPerm string

const (
	DbPermSelect DbPerm = "select"
	DbPermInsert DbPerm = "insert"
	DbPermUpdate DbPerm = "update"
	DbPermDelete DbPerm = "delete"
	DbPermCreate DbPerm = "create"
	DbPermAlter  DbPerm = "alter"
	DbPermDrop   DbPerm = "drop"
)

// AllDbPerms is the full set of database permissions.
var AllDbPerms = []DbPerm{DbPermSelect, DbPermInsert, DbPermUpdate, DbPermDelete, DbPermCreate, DbPermAlter, DbPermDrop}

// ReadOnlyPerms is the read-only permission set.
var ReadOnlyPerms = []DbPerm{DbPermSelect}

// ReadWritePerms is the standard read-write permission set.
var ReadWritePerms = []DbPerm{DbPermSelect, DbPermInsert, DbPermUpdate, DbPermDelete}

// ParseDbPerms decodes a JSON string into a slice of DbPerm.
func ParseDbPerms(raw string) []DbPerm {
	if raw == "" {
		return AllDbPerms
	}
	var perms []DbPerm
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return AllDbPerms
	}
	if len(perms) == 0 {
		return AllDbPerms
	}
	return perms
}

// DbPermsToJSON encodes a slice of DbPerm to a JSON string.
func DbPermsToJSON(perms []DbPerm) string {
	if len(perms) == 0 {
		b, _ := json.Marshal(AllDbPerms)
		return string(b)
	}
	b, _ := json.Marshal(perms)
	return string(b)
}

// HasDbPerm checks if a permission exists in a slice.
func HasDbPerm(perms []DbPerm, perm DbPerm) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// DetectRequiredPerm determines which DB permission is needed based on the SQL statement.
func DetectRequiredPerm(statement string) DbPerm {
	s := strings.TrimSpace(strings.ToUpper(statement))
	switch {
	case strings.HasPrefix(s, "SELECT"),
		strings.HasPrefix(s, "SHOW"),
		strings.HasPrefix(s, "DESCRIBE"),
		strings.HasPrefix(s, "DESC "),
		strings.HasPrefix(s, "EXPLAIN"):
		return DbPermSelect
	case strings.HasPrefix(s, "INSERT"):
		return DbPermInsert
	case strings.HasPrefix(s, "UPDATE"):
		return DbPermUpdate
	case strings.HasPrefix(s, "DELETE"),
		strings.HasPrefix(s, "TRUNCATE"):
		return DbPermDelete
	case strings.HasPrefix(s, "CREATE"):
		return DbPermCreate
	case strings.HasPrefix(s, "ALTER"):
		return DbPermAlter
	case strings.HasPrefix(s, "DROP"):
		return DbPermDrop
	default:
		return ""
	}
}

// ── Application-Level Permissions ──

// Application permission keys
const (
	PermConnectionsView   = "connections.view"
	PermConnectionsCreate = "connections.create"
	PermConnectionsEdit   = "connections.edit"
	PermConnectionsDelete = "connections.delete"
	PermQueryExecute      = "query.execute"
	PermQueryApprove      = "query.approve"
	PermSavedQueriesManage = "savedqueries.manage"
	PermSnippetsManage    = "snippets.manage"
	PermNotificationsView = "notifications.view"
	PermSchemaBrowse      = "schema.browse"
	PermSchemaDiffView    = "schema.diff.view"
	PermAuditView         = "audit.view"
	PermAIUse             = "ai.use"
	PermAIManage          = "ai.manage"
	PermSecuritySelf      = "security.self"
	PermBackupsManage     = "backups.manage"
	PermSchedulesManage   = "schedules.manage"
	PermHealthView        = "health.view"
	PermRowHistoryView    = "rowhistory.view"
	PermUsersManage       = "users.manage"
	PermFoldersManage     = "folders.manage"
	PermRolesManage       = "roles.manage"
	PermWorkflowsManage   = "workflows.manage"
)

// AllAppPermissions is the master list of every permission key.
var AllAppPermissions = []PermissionDef{
	{Key: PermConnectionsView, Label: "View Connections", Group: "Connections"},
	{Key: PermConnectionsCreate, Label: "Create Connections", Group: "Connections"},
	{Key: PermConnectionsEdit, Label: "Edit Connections", Group: "Connections"},
	{Key: PermConnectionsDelete, Label: "Delete Connections", Group: "Connections"},
	{Key: PermQueryExecute, Label: "Execute Queries", Group: "Query"},
	{Key: PermQueryApprove, Label: "Approve Query Requests", Group: "Query"},
	{Key: PermSavedQueriesManage, Label: "Manage Saved Queries", Group: "Query"},
	{Key: PermSnippetsManage, Label: "Manage Snippets", Group: "Query"},
	{Key: PermSchemaBrowse, Label: "Browse Schema", Group: "Schema"},
	{Key: PermSchemaDiffView, Label: "View Schema Diff", Group: "Schema"},
	{Key: PermAuditView, Label: "View Audit Logs", Group: "Audit"},
	{Key: PermAIUse, Label: "Use AI Assistant", Group: "AI"},
	{Key: PermAIManage, Label: "Manage AI Settings", Group: "AI"},
	{Key: PermSecuritySelf, Label: "Manage Own Security", Group: "Security"},
	{Key: PermNotificationsView, Label: "View Notifications", Group: "Operations"},
	{Key: PermBackupsManage, Label: "Manage Backups", Group: "Operations"},
	{Key: PermSchedulesManage, Label: "Manage Schedules", Group: "Operations"},
	{Key: PermHealthView, Label: "View Health", Group: "Operations"},
	{Key: PermRowHistoryView, Label: "View Row History", Group: "Operations"},
	{Key: PermUsersManage, Label: "Manage Users", Group: "Administration"},
	{Key: PermFoldersManage, Label: "Manage Folders", Group: "Administration"},
	{Key: PermRolesManage, Label: "Manage Roles", Group: "Administration"},
	{Key: PermWorkflowsManage, Label: "Manage Workflows", Group: "Administration"},
}

// PermissionDef describes a single permission for the UI.
type PermissionDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// ── Role Model ──

// Role represents a role with application-level permissions.
type Role struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions string    `json:"permissions"` // JSON array of permission keys
	IsSystem    bool      `json:"is_system"`
	IsActive    bool      `json:"is_active"`
	UserCount   int       `json:"user_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateRoleRequest is the body for creating/updating a role.
type CreateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// ParseAppPerms decodes a JSON string into a slice of permission keys.
func ParseAppPerms(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var perms []string
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return []string{}
	}
	return perms
}

// AppPermsToJSON encodes a slice of permission keys to a JSON string.
func AppPermsToJSON(perms []string) string {
	if len(perms) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(perms)
	return string(b)
}

// HasAppPerm checks if a permission exists in a slice.
func HasAppPerm(perms []string, perm string) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// ── Access Group Model ──

// AccessGroup represents a folder extended with group membership.
type AccessGroup struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	ParentID        *int64    `json:"parent_id"`
	OwnerID         int64     `json:"owner_id"`
	Visibility      string    `json:"visibility"` // private|shared
	Color           string    `json:"color"`
	RoleRestrict    string    `json:"role_restrict"` // '' = all roles, or specific role name
	IsActive        bool      `json:"is_active"`
	SortOrder       int       `json:"sort_order"`
	MemberCount     int       `json:"member_count"`
	ConnectionCount int       `json:"connection_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// GroupMember is a user assigned to a group.
type GroupMember struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// GroupConnection is a connection assigned to a group.
type GroupConnection struct {
	ConnID      int64    `json:"conn_id"`
	Name        string   `json:"name"`
	Driver      string   `json:"driver"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Environment string   `json:"environment"`
	Permissions []DbPerm `json:"permissions"`
}

// ConnectionPermission pairs a connection ID with its allowed operations.
type ConnectionPermission struct {
	ConnID      int64    `json:"conn_id"`
	Permissions []DbPerm `json:"permissions"`
}

// CreateAccessGroupRequest is the request body for creating a group.
type CreateAccessGroupRequest struct {
	Name                  string                 `json:"name"`
	Description           string                 `json:"description"`
	ParentID              *int64                 `json:"parent_id"`
	RoleRestrict          string                 `json:"role_restrict"`
	Color                 string                 `json:"color"`
	UserIDs               []int64                `json:"user_ids"`
	ConnectionIDs         []int64                `json:"connection_ids"`
	ConnectionPermissions []ConnectionPermission `json:"connection_permissions"`
}

// UpdateAccessGroupRequest is the request body for updating a group.
type UpdateAccessGroupRequest struct {
	Name                  string                 `json:"name"`
	Description           string                 `json:"description"`
	RoleRestrict          string                 `json:"role_restrict"`
	UserIDs               []int64                `json:"user_ids"`
	ConnectionIDs         []int64                `json:"connection_ids"`
	ConnectionPermissions []ConnectionPermission `json:"connection_permissions"`
}

// UserConnectionAssignment represents connection access for a user.
type UserConnectionAssignment struct {
	ConnID      int64    `json:"conn_id"`
	Name        string   `json:"name"`
	Driver      string   `json:"driver"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Environment string   `json:"environment"`
	Source      string   `json:"source"` // "direct" or group name
	Permissions []DbPerm `json:"permissions"`
}

// ── Approval Workflow Models ──

type WorkflowAccessGroup struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

type WorkflowConnection struct {
	ConnID      int64  `json:"conn_id"`
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Environment string `json:"environment"`
}

type StepApprover struct {
	ID           int64  `json:"id"`
	StepID       int64  `json:"step_id"`
	ApproverType string `json:"approver_type"`
	ApproverID   int64  `json:"approver_id"`
	ApproverName string `json:"approver_name"`
}

type WorkflowStep struct {
	ID                int64          `json:"id"`
	WorkflowID        int64          `json:"workflow_id"`
	StepOrder         int            `json:"step_order"`
	Name              string         `json:"name"`
	RequiredApprovals int            `json:"required_approvals"`
	Approvers         []StepApprover `json:"approvers"`
}

type ApprovalWorkflow struct {
	ID                   int64                 `json:"id"`
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	IsActive             bool                  `json:"is_active"`
	AssignAllGroups      bool                  `json:"assign_all_groups"`
	AssignAllConnections bool                  `json:"assign_all_connections"`
	AccessGroups         []WorkflowAccessGroup `json:"access_groups,omitempty"`
	Connections          []WorkflowConnection  `json:"connections,omitempty"`
	Steps                []WorkflowStep        `json:"steps,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type CreateApproverReq struct {
	ApproverType string `json:"approver_type"`
	ApproverID   int64  `json:"approver_id"`
}

type CreateWorkflowStepReq struct {
	Name              string              `json:"name"`
	RequiredApprovals int                 `json:"required_approvals"`
	Approvers         []CreateApproverReq `json:"approvers"`
}

type CreateWorkflowRequest struct {
	Name                 string                  `json:"name"`
	Description          string                  `json:"description"`
	AssignAllGroups      bool                    `json:"assign_all_groups"`
	AccessGroupIDs       []int64                 `json:"access_group_ids"`
	AssignAllConnections bool                    `json:"assign_all_connections"`
	ConnectionIDs        []int64                 `json:"connection_ids"`
	Steps                []CreateWorkflowStepReq `json:"steps"`
}

type QueryApprovalStatus string

const (
	QueryApprovalStatusDraft         QueryApprovalStatus = "draft"
	QueryApprovalStatusPendingReview QueryApprovalStatus = "pending_review"
	QueryApprovalStatusApproved      QueryApprovalStatus = "approved"
	QueryApprovalStatusRejected      QueryApprovalStatus = "rejected"
	QueryApprovalStatusExecuting     QueryApprovalStatus = "executing"
	QueryApprovalStatusDone          QueryApprovalStatus = "done"
	QueryApprovalStatusFailed        QueryApprovalStatus = "failed"
)

type QueryApprovalRequest struct {
	ID           int64               `json:"id"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	ConnID       int64               `json:"conn_id"`
	Connection   string              `json:"connection"`
	Driver       string              `json:"driver"`
	Environment  string              `json:"environment"`
	Database     string              `json:"database"`
	Statement    string              `json:"statement"`
	Status       QueryApprovalStatus `json:"status"`
	CreatorID    int64               `json:"creator_id"`
	CreatorName  string              `json:"creator_name"`
	ReviewerID   *int64              `json:"reviewer_id,omitempty"`
	ReviewerName string              `json:"reviewer_name,omitempty"`
	ReviewNote   string              `json:"review_note,omitempty"`
	WorkflowID   int64               `json:"workflow_id"`
	CurrentStep  int                 `json:"current_step"`
	Revision     int                 `json:"revision"`
	Approvers    []string            `json:"approvers,omitempty"`
	ExecuteError string              `json:"execute_error,omitempty"`
	ExecutedAt   *time.Time          `json:"executed_at,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type CreateQueryApprovalRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ConnID      int64  `json:"conn_id"`
	Database    string `json:"database"`
	Statement   string `json:"statement"`
	WorkflowID  int64  `json:"workflow_id"`
}

type UpdateQueryApprovalRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ConnID      int64  `json:"conn_id"`
	Database    string `json:"database"`
	Statement   string `json:"statement"`
	WorkflowID  int64  `json:"workflow_id"`
}

type ChangeApproval struct {
	ID        int64     `json:"id"`
	RequestID int64     `json:"request_id"`
	StepID    int64     `json:"step_id"`
	StepName  string    `json:"step_name"`
	StepOrder int       `json:"step_order"`
	Revision  int       `json:"revision"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type ApprovalProgress struct {
	Step      WorkflowStep     `json:"step"`
	Status    string           `json:"status"`
	Approvals []ChangeApproval `json:"approvals"`
}

type ApproveStepRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

// ── Helper Functions for Backward Compatibility ──

// isAuthEnabled checks if authentication is enabled (i.e., users exist)
func isAuthEnabled() bool {
	var count int
	db.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0
}

// CheckReadPermission checks if a user has read access to a connection
func CheckReadPermission(r *http.Request, connID int64) bool {
	// If auth is not enabled, allow all operations
	if !isAuthEnabled() {
		return true
	}

	role := r.Header.Get("X-User-Role")
	if role == "admin" {
		return true
	}

	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return false
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return false
	}

	// Check if user owns this connection (backward compatibility)
	var ownerID sql.NullInt64
	err = db.DB.QueryRow(db.ConvertQuery(`SELECT owner_id FROM connections WHERE id = ?`), connID).Scan(&ownerID)
	if err == nil && ownerID.Valid && ownerID.Int64 == userID {
		return true
	}

	// If no owner is set (legacy connections), allow access (default permissive)
	if err == nil && !ownerID.Valid {
		return true
	}

	perms, err := db.GetUserConnectionPermissions(userID, role, connID)
	if err != nil {
		// Default to allowing if no permission system is configured
		return true
	}

	// If no explicit permissions, default to allowing (backward compatibility)
	if len(perms) == 0 {
		return true
	}

	// Check for select permission
	for _, p := range perms {
		if string(p) == string(DbPermSelect) {
			return true
		}
	}
	return false
}

// CheckWritePermission checks if a user has write access to a connection
func CheckWritePermission(r *http.Request, connID int64) bool {
	// If auth is not enabled, allow all operations
	if !isAuthEnabled() {
		return true
	}

	role := r.Header.Get("X-User-Role")
	if role == "admin" {
		return true
	}

	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return false
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return false
	}

	// Check if user owns this connection (backward compatibility)
	var ownerID sql.NullInt64
	err = db.DB.QueryRow(db.ConvertQuery(`SELECT owner_id FROM connections WHERE id = ?`), connID).Scan(&ownerID)
	if err == nil && ownerID.Valid && ownerID.Int64 == userID {
		return true
	}

	// If no owner is set (legacy connections), allow access (default permissive)
	if err == nil && !ownerID.Valid {
		return true
	}

	perms, err := db.GetUserConnectionPermissions(userID, role, connID)
	if err != nil {
		// Default to allowing if no permission system is configured
		return true
	}

	// If no explicit permissions, default to allowing (backward compatibility)
	if len(perms) == 0 {
		return true
	}

	// Check for write permissions (insert, update, delete)
	for _, p := range perms {
		ps := string(p)
		if ps == string(DbPermInsert) || ps == string(DbPermUpdate) || ps == string(DbPermDelete) {
			return true
		}
	}
	return false
}
