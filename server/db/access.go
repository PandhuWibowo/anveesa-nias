package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// DbPerm represents a database operation permission (duplicated from handlers for import cycle avoidance)
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

var AllDbPerms = []DbPerm{DbPermSelect, DbPermInsert, DbPermUpdate, DbPermDelete, DbPermCreate, DbPermAlter, DbPermDrop}

// ── Role & Permission Resolution ──

// GetUserRole returns the role name for a user ID (with default fallback)
func GetUserRole(userID int64) (string, error) {
	var roleName string
	err := DB.QueryRow(`
		SELECT COALESCE(r.name, 'user')
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE u.id = ?
	`, userID).Scan(&roleName)
	if err != nil {
		if err == sql.ErrNoRows {
			return "user", nil
		}
		return "", err
	}
	return roleName, nil
}

// GetUserAppPermissions returns the effective application permissions for a user
func GetUserAppPermissions(userID int64) ([]string, error) {
	var rolePerms, userPerms string
	err := DB.QueryRow(`
		SELECT 
			COALESCE(r.permissions, '[]'),
			COALESCE(u.permissions, '[]')
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE u.id = ?
	`, userID).Scan(&rolePerms, &userPerms)
	if err != nil {
		if err == sql.ErrNoRows {
			return []string{}, nil
		}
		return nil, err
	}

	permsMap := make(map[string]bool)

	// Parse role permissions
	var rp []string
	if json.Unmarshal([]byte(rolePerms), &rp) == nil {
		for _, p := range rp {
			permsMap[p] = true
		}
	}

	// Parse user-specific permission overrides (additive)
	var up []string
	if json.Unmarshal([]byte(userPerms), &up) == nil {
		for _, p := range up {
			permsMap[p] = true
		}
	}

	result := make([]string, 0, len(permsMap))
	for p := range permsMap {
		result = append(result, p)
	}
	return result, nil
}

// HasUserAppPermission checks if a user has a specific application permission
func HasUserAppPermission(userID int64, perm string) bool {
	perms, err := GetUserAppPermissions(userID)
	if err != nil {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// ── Connection Access Resolution ──

// GetAccessibleConnectionIDs returns all connection IDs a user can access.
// Returns nil for admin role (means all connections).
func GetAccessibleConnectionIDs(userID int64, role string) ([]int64, error) {
	// Admin gets everything
	if role == "admin" {
		return nil, nil // nil = unrestricted
	}

	rows, err := DB.Query(`
		SELECT DISTINCT conn_id FROM (
			-- via active folders/groups (respecting role_restrict)
			SELECT fc.conn_id
			FROM folder_members fm
			JOIN connection_folders f ON f.id = fm.folder_id
			JOIN folder_connections fc ON fc.folder_id = fm.folder_id
			WHERE fm.user_id = ?
			  AND f.is_active = 1
			  AND (f.role_restrict = '' OR f.role_restrict = ?)
			UNION
			-- direct user-connection assignments
			SELECT conn_id FROM user_connections WHERE user_id = ?
			UNION
			-- connections owned by the user
			SELECT id FROM connections WHERE owner_id = ?
		)
	`, userID, role, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetUserConnectionPermissions returns the effective DB permissions a user has on a specific connection.
//
// Resolution strategy:
//   - Admin → all permissions
//   - If user has BOTH direct and group assignments → INTERSECTION (direct acts as ceiling)
//   - If user has one source → use that source
//   - Group permissions are UNION across all groups
//   - Owner always gets all permissions
func GetUserConnectionPermissions(userID int64, role string, connID int64) ([]DbPerm, error) {
	// Admin gets everything
	if role == "admin" {
		return AllDbPerms, nil
	}

	// Check if user owns the connection
	var ownerID int64
	err := DB.QueryRow(`SELECT owner_id FROM connections WHERE id = ?`, connID).Scan(&ownerID)
	if err == nil && ownerID == userID {
		return AllDbPerms, nil
	}

	// 1. Direct user-connection permissions
	var directRaw string
	hasDirect := false
	err = DB.QueryRow(`
		SELECT COALESCE(permissions, '[]')
		FROM user_connections
		WHERE user_id = ? AND conn_id = ?
	`, userID, connID).Scan(&directRaw)
	if err == nil {
		hasDirect = true
	}

	// 2. Group-level permissions (union across all matching groups)
	rows, err := DB.Query(`
		SELECT COALESCE(fc.permissions, '[]')
		FROM folder_connections fc
		JOIN folder_members fm ON fm.folder_id = fc.folder_id
		JOIN connection_folders f ON f.id = fc.folder_id
		WHERE fm.user_id = ?
		  AND fc.conn_id = ?
		  AND f.is_active = 1
		  AND (f.role_restrict = '' OR f.role_restrict = ?)
	`, userID, connID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hasGroup := false
	groupPerms := make(map[DbPerm]bool)
	for rows.Next() {
		hasGroup = true
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, p := range parseDbPerms(raw) {
			groupPerms[p] = true
		}
	}

	// No source at all
	if !hasDirect && !hasGroup {
		return []DbPerm{}, nil
	}

	// Only direct
	if hasDirect && !hasGroup {
		return parseDbPerms(directRaw), nil
	}

	// Only group
	if !hasDirect && hasGroup {
		var result []DbPerm
		for _, p := range AllDbPerms {
			if groupPerms[p] {
				result = append(result, p)
			}
		}
		return result, nil
	}

	// Both exist → intersection (most restrictive wins)
	directPerms := make(map[DbPerm]bool)
	for _, p := range parseDbPerms(directRaw) {
		directPerms[p] = true
	}
	var result []DbPerm
	for _, p := range AllDbPerms {
		if directPerms[p] && groupPerms[p] {
			result = append(result, p)
		}
	}
	return result, nil
}

// parseDbPerms is a helper to decode permission JSON
func parseDbPerms(raw string) []DbPerm {
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

// dbPermsToJSON encodes permissions to JSON
func dbPermsToJSON(perms []DbPerm) string {
	if len(perms) == 0 {
		b, _ := json.Marshal(AllDbPerms)
		return string(b)
	}
	b, _ := json.Marshal(perms)
	return string(b)
}

// ── Access Group Management ──

// ListAccessGroups returns all folders/groups with member & connection counts
func ListAccessGroups() ([]map[string]interface{}, error) {
	rows, err := DB.Query(`
		SELECT 
			f.id, f.name, f.parent_id, f.owner_id, f.visibility, f.color, 
			f.role_restrict, f.is_active, f.sort_order, f.created_at,
			(SELECT COUNT(*) FROM folder_members WHERE folder_id = f.id) AS member_count,
			(SELECT COUNT(*) FROM folder_connections WHERE folder_id = f.id) AS conn_count
		FROM connection_folders f
		ORDER BY f.sort_order, f.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []map[string]interface{}
	for rows.Next() {
		var id, ownerID, sortOrder, memberCount, connCount int64
		var name, visibility, color, roleRestrict, createdAt string
		var parentID sql.NullInt64
		var isActive bool
		
		err := rows.Scan(&id, &name, &parentID, &ownerID, &visibility, &color, &roleRestrict, &isActive, &sortOrder, &createdAt, &memberCount, &connCount)
		if err != nil {
			return nil, err
		}

		group := map[string]interface{}{
			"id":               id,
			"name":             name,
			"parent_id":        nil,
			"owner_id":         ownerID,
			"visibility":       visibility,
			"color":            color,
			"role_restrict":    roleRestrict,
			"is_active":        isActive,
			"sort_order":       sortOrder,
			"member_count":     memberCount,
			"connection_count": connCount,
			"created_at":       createdAt,
		}
		if parentID.Valid {
			group["parent_id"] = parentID.Int64
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// GetGroupMembers returns users assigned to a group
func GetGroupMembers(groupID int64) ([]map[string]interface{}, error) {
	rows, err := DB.Query(`
		SELECT u.id, u.username, COALESCE(r.name, 'user') as role
		FROM users u
		JOIN folder_members fm ON fm.user_id = u.id
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE fm.folder_id = ?
		ORDER BY u.username
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []map[string]interface{}
	for rows.Next() {
		var id int64
		var username, role string
		if err := rows.Scan(&id, &username, &role); err != nil {
			return nil, err
		}
		members = append(members, map[string]interface{}{
			"user_id":  id,
			"username": username,
			"role":     role,
		})
	}
	return members, nil
}

// GetGroupConnections returns connections assigned to a group
func GetGroupConnections(groupID int64) ([]map[string]interface{}, error) {
	rows, err := DB.Query(`
		SELECT c.id, c.name, c.driver, c.host, c.port, 
		       COALESCE(c.environment, 'development'), 
		       COALESCE(fc.permissions, '[]')
		FROM connections c
		JOIN folder_connections fc ON fc.conn_id = c.id
		WHERE fc.folder_id = ?
		ORDER BY c.name
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []map[string]interface{}
	for rows.Next() {
		var id, port int64
		var name, driver, host, environment, permsRaw string
		if err := rows.Scan(&id, &name, &driver, &host, &port, &environment, &permsRaw); err != nil {
			return nil, err
		}
		conns = append(conns, map[string]interface{}{
			"conn_id":     id,
			"name":        name,
			"driver":      driver,
			"host":        host,
			"port":        port,
			"environment": environment,
			"permissions": parseDbPerms(permsRaw),
		})
	}
	return conns, nil
}

// SetGroupMembers replaces all members for a group
func SetGroupMembers(groupID int64, userIDs []int64) error {
	if _, err := DB.Exec(`DELETE FROM folder_members WHERE folder_id = ?`, groupID); err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return nil
	}
	vals := make([]string, len(userIDs))
	args := make([]interface{}, 0, len(userIDs)*2)
	for i, uid := range userIDs {
		vals[i] = "(?, ?)"
		args = append(args, groupID, uid)
	}
	_, err := DB.Exec(`INSERT OR IGNORE INTO folder_members (folder_id, user_id) VALUES `+strings.Join(vals, ","), args...)
	return err
}

// SetGroupConnections replaces all connections for a group
func SetGroupConnections(groupID int64, connIDs []int64, permsMap map[int64][]DbPerm) error {
	if _, err := DB.Exec(`DELETE FROM folder_connections WHERE folder_id = ?`, groupID); err != nil {
		return err
	}
	if len(connIDs) == 0 {
		return nil
	}
	vals := make([]string, len(connIDs))
	args := make([]interface{}, 0, len(connIDs)*3)
	for i, cid := range connIDs {
		vals[i] = "(?, ?, ?)"
		perms := AllDbPerms
		if p, ok := permsMap[cid]; ok && len(p) > 0 {
			perms = p
		}
		args = append(args, groupID, cid, dbPermsToJSON(perms))
	}
	_, err := DB.Exec(`INSERT OR IGNORE INTO folder_connections (folder_id, conn_id, permissions) VALUES `+strings.Join(vals, ","), args...)
	return err
}

// SetUserDirectConnections replaces direct connection assignments for a user
func SetUserDirectConnections(userID int64, connIDs []int64, permsMap map[int64][]DbPerm) error {
	if _, err := DB.Exec(`DELETE FROM user_connections WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if len(connIDs) == 0 {
		return nil
	}
	vals := make([]string, len(connIDs))
	args := make([]interface{}, 0, len(connIDs)*3)
	for i, cid := range connIDs {
		vals[i] = "(?, ?, ?)"
		perms := AllDbPerms
		if p, ok := permsMap[cid]; ok && len(p) > 0 {
			perms = p
		}
		args = append(args, userID, cid, dbPermsToJSON(perms))
	}
	_, err := DB.Exec(`INSERT OR IGNORE INTO user_connections (user_id, conn_id, permissions) VALUES `+strings.Join(vals, ","), args...)
	return err
}

// GetUserConnectionAssignments returns all connection assignments for a user
func GetUserConnectionAssignments(userID int64, role string) ([]map[string]interface{}, error) {
	query := `
		SELECT DISTINCT 
			c.id, c.name, c.driver, c.host, c.port, 
			COALESCE(c.environment, 'development') AS environment,
			'direct' AS source,
			COALESCE(uc.permissions, '[]') AS permissions
		FROM connections c
		JOIN user_connections uc ON uc.conn_id = c.id
		WHERE uc.user_id = ?
		
		UNION
		
		SELECT DISTINCT 
			c.id, c.name, c.driver, c.host, c.port,
			COALESCE(c.environment, 'development') AS environment,
			f.name AS source,
			COALESCE(fc.permissions, '[]') AS permissions
		FROM connections c
		JOIN folder_connections fc ON fc.conn_id = c.id
		JOIN connection_folders f ON f.id = fc.folder_id
		JOIN folder_members fm ON fm.folder_id = f.id
		WHERE fm.user_id = ?
		  AND f.is_active = 1
		  AND (f.role_restrict = '' OR f.role_restrict = ?)
		
		ORDER BY name
	`

	rows, err := DB.Query(query, userID, userID, role)
	if err != nil {
		return nil, fmt.Errorf("query user connections: %w", err)
	}
	defer rows.Close()

	var assignments []map[string]interface{}
	for rows.Next() {
		var id, port int64
		var name, driver, host, environment, source, permsRaw string
		if err := rows.Scan(&id, &name, &driver, &host, &port, &environment, &source, &permsRaw); err != nil {
			return nil, err
		}
		assignments = append(assignments, map[string]interface{}{
			"conn_id":     id,
			"name":        name,
			"driver":      driver,
			"host":        host,
			"port":        port,
			"environment": environment,
			"source":      source,
			"permissions": parseDbPerms(permsRaw),
		})
	}
	return assignments, nil
}
