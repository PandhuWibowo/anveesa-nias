# Permission System Usage Guide

## Quick Start

The server has started successfully with **all permission tables migrated**. Here's how to use the new system:

---

## 1. Understanding the System

### **Default Setup**

After migration:
- ✅ Two system roles created: **admin** and **user**
- ✅ Existing users default to **user** role (role_id = 2)
- ✅ All existing connections remain accessible to their owners and admins
- ✅ Legacy permission system still works alongside new system

### **Three Permission Layers**

```
Application → Can user access this feature?
Connection  → Can user access this database?
Operation   → Can user run this SQL operation?
```

---

## 2. Managing Roles

### **View Roles**

Navigate to: **Administration → Permissions** (or `/rbac`)

### **Create Custom Role**

```http
POST /api/roles
{
  "name": "ReadOnlyDev",
  "description": "Developers with read-only access",
  "permissions": [
    "connections.view",
    "query.execute",
    "schema.browse"
  ]
}
```

### **Assign Role to User**

```sql
-- Via database (UI coming soon)
UPDATE users 
SET role_id = (SELECT id FROM roles WHERE name = 'ReadOnlyDev')
WHERE username = 'john.doe';
```

---

## 3. Database Operation Permissions

### **7 Permission Types**

| Permission | Allows                          | SQL Examples              |
|------------|---------------------------------|---------------------------|
| `select`   | Read data                       | SELECT, SHOW, DESCRIBE    |
| `insert`   | Add new rows                    | INSERT                    |
| `update`   | Modify existing rows            | UPDATE                    |
| `delete`   | Remove rows                     | DELETE, TRUNCATE          |
| `create`   | Create new tables/indexes       | CREATE TABLE, CREATE INDEX|
| `alter`    | Modify table structure          | ALTER TABLE               |
| `drop`     | Delete tables/indexes           | DROP TABLE, DROP INDEX    |

### **Permission Presets**

```javascript
// Read-only
["select"]

// Standard read-write
["select", "insert", "update", "delete"]

// Full access
["select", "insert", "update", "delete", "create", "alter", "drop"]
```

---

## 4. Direct User-Connection Assignments

### **Grant User Access to Specific Connection**

```sql
-- Give user #5 read-only access to connection #10
INSERT INTO user_connections (user_id, conn_id, permissions)
VALUES (5, 10, '["select"]');

-- Give user #7 read-write access to connection #20
INSERT INTO user_connections (user_id, conn_id, permissions)
VALUES (7, 20, '["select","insert","update","delete"]');
```

### **Query User's Connections**

```sql
SELECT 
  c.name,
  uc.permissions
FROM user_connections uc
JOIN connections c ON c.id = uc.conn_id
WHERE uc.user_id = 5;
```

---

## 5. Access Groups (Team-Based)

### **Convert Folder to Access Group**

Existing folders automatically work as access groups. Enhance them:

```sql
-- Make folder a "DBA Team" group
UPDATE connection_folders 
SET role_restrict = 'user',  -- Only 'user' role members can join
    is_active = 1             -- Active group
WHERE id = 1;
```

### **Add Members to Group**

```sql
-- Add users to the "DBA Team" folder/group
INSERT INTO folder_members (folder_id, user_id)
VALUES 
  (1, 5),  -- User ID 5
  (1, 6),  -- User ID 6
  (1, 7);  -- User ID 7
```

### **Add Connections to Group**

```sql
-- Add connections to group with permissions
INSERT INTO folder_connections (folder_id, conn_id, permissions)
VALUES 
  (1, 10, '["select","insert","update","delete"]'),  -- Read-write
  (1, 20, '["select"]'),                             -- Read-only
  (1, 30, '["select","insert","update","delete","create","alter","drop"]'); -- Full
```

### **Result**

All members of group #1 can now access connections #10, #20, #30 with their respective permissions.

---

## 6. Permission Resolution Examples

### **Example 1: Group-Only Access**

```
User #5 is member of "QA Team" group
Group has connection #10 with ["select","insert","update","delete"]
→ User #5 can read-write on connection #10
```

### **Example 2: Direct Override (Intersection)**

```
User #5 is member of "QA Team" with ["select","insert","update","delete"]
User #5 also has direct assignment with ["select"]
→ User #5 can ONLY SELECT (intersection = most restrictive)
```

### **Example 3: Multiple Groups (Union)**

```
User #5 is member of:
  - "Dev Team" with connection #10: ["select"]
  - "QA Team" with connection #10: ["select","insert","update","delete"]
→ User #5 can read-write on connection #10 (union of groups)
```

### **Example 4: Owner Bypass**

```
User #5 created connection #10
→ User #5 has ALL permissions regardless of groups/roles
```

---

## 7. Testing Permissions

### **Test Read-Only User**

```bash
# 1. Create test user
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"readonly","password":"test123"}'

# 2. Assign read-only permission
sqlite3 data.db "INSERT INTO user_connections (user_id, conn_id, permissions) 
VALUES (
  (SELECT id FROM users WHERE username='readonly'),
  1,
  '[\"select\"]'
)"

# 3. Login as readonly user and try INSERT
# → Should get 403 Forbidden
```

### **Test Group Access**

```bash
# 1. Create group
sqlite3 data.db "INSERT INTO connection_folders (name, owner_id, is_active) 
VALUES ('Test Group', 1, 1)"

# 2. Add member
sqlite3 data.db "INSERT INTO folder_members (folder_id, user_id) 
VALUES (
  (SELECT id FROM connection_folders WHERE name='Test Group'),
  (SELECT id FROM users WHERE username='readonly')
)"

# 3. Add connection to group
sqlite3 data.db "INSERT INTO folder_connections (folder_id, conn_id, permissions)
VALUES (
  (SELECT id FROM connection_folders WHERE name='Test Group'),
  2,
  '[\"select\",\"insert\",\"update\",\"delete\"]'
)"

# 4. User 'readonly' can now access connection #2 with read-write permissions
```

---

## 8. Common Scenarios

### **Scenario: Sensitive Data Access Control**

```sql
-- Mark sensitive connections
UPDATE connections SET environment = 'sensitive' WHERE id IN (1, 2, 3);

-- Create "Sensitive Data Team" group
INSERT INTO connection_folders (name, role_restrict, is_active)
VALUES ('Sensitive Data Team', 'admin', 1);

-- Add senior DBAs
INSERT INTO folder_members (folder_id, user_id)
SELECT 
  (SELECT id FROM connection_folders WHERE name='Production Team'),
  id
FROM users WHERE role = 'admin';

-- Grant read-only access to sensitive data
INSERT INTO folder_connections (folder_id, conn_id, permissions)
SELECT 
  (SELECT id FROM connection_folders WHERE name='Sensitive Data Team'),
  id,
  '["select"]'
FROM connections WHERE environment = 'sensitive';

-- Result: Only admins can access sensitive data, and only with SELECT
```

### **Scenario: Temporary Access**

```sql
-- Grant contractor temporary full access to staging
INSERT INTO user_connections (user_id, conn_id, permissions)
VALUES (
  (SELECT id FROM users WHERE username='contractor'),
  (SELECT id FROM connections WHERE name='Staging DB'),
  '["select","insert","update","delete","create","alter","drop"]'
);

-- Revoke later
DELETE FROM user_connections 
WHERE user_id = (SELECT id FROM users WHERE username='contractor');
```

### **Scenario: Read-Only Analysts**

```sql
-- Create analyst role
INSERT INTO roles (name, description, permissions, is_system)
VALUES (
  'analyst',
  'Data analysts with read-only access',
  '["connections.view","query.execute","schema.browse"]',
  0
);

-- Assign analysts to role
UPDATE users SET role_id = (SELECT id FROM roles WHERE name='analyst')
WHERE username IN ('alice', 'bob', 'charlie');

-- Grant SELECT-only access to analytics database
INSERT INTO user_connections (user_id, conn_id, permissions)
SELECT 
  u.id,
  (SELECT id FROM connections WHERE name='Analytics DB'),
  '["select"]'
FROM users u WHERE u.role_id = (SELECT id FROM roles WHERE name='analyst');
```

---

## 9. Monitoring & Audit

### **Check User's Effective Permissions**

```sql
-- See what connections a user can access
SELECT * FROM (
  -- Direct assignments
  SELECT c.name, 'direct' as source, uc.permissions
  FROM user_connections uc
  JOIN connections c ON c.id = uc.conn_id
  WHERE uc.user_id = 5
  
  UNION ALL
  
  -- Via groups
  SELECT c.name, f.name as source, fc.permissions
  FROM folder_members fm
  JOIN connection_folders f ON f.id = fm.folder_id
  JOIN folder_connections fc ON fc.folder_id = fm.folder_id
  JOIN connections c ON c.id = fc.conn_id
  WHERE fm.user_id = 5 AND f.is_active = 1
);
```

### **Audit Group Membership**

```sql
-- Who has access to sensitive connections?
SELECT DISTINCT u.username, u.role
FROM users u
JOIN folder_members fm ON fm.user_id = u.id
JOIN connection_folders f ON f.id = fm.folder_id
JOIN folder_connections fc ON fc.folder_id = f.id
JOIN connections c ON c.id = fc.conn_id
WHERE c.environment = 'sensitive' AND f.is_active = 1;
```

---

## 10. Migration Checklist

- [x] Database schema migrated
- [x] System roles created (admin, user)
- [ ] Review existing users and assign appropriate roles
- [ ] Create custom roles for your organization
- [ ] Organize connections into access groups
- [ ] Set per-connection permissions
- [ ] Test read-only user cannot execute writes
- [ ] Test group membership grants access
- [ ] Test disabled groups block access

---

## API Endpoints

### **Roles**
- `GET /api/roles` — List all roles
- `POST /api/roles` — Create role
- `GET /api/roles/:id` — Get role details
- `PUT /api/roles/:id` — Update role
- `DELETE /api/roles/:id` — Delete role

### **Permissions**
- `GET /api/app-permissions` — List all application permission keys
- `GET /api/my-permissions` — Get current user's effective permissions

### **Legacy (Backward Compatible)**
- `GET /api/permissions` — List legacy permission records
- `POST /api/permissions` — Create legacy permission
- `DELETE /api/permissions/:id` — Delete legacy permission

---

## Troubleshooting

### **User Can't See Connections**

1. Check role assignment: `SELECT role_id FROM users WHERE id = ?`
2. Check role permissions: `SELECT permissions FROM roles WHERE id = ?`
3. Check group membership: `SELECT * FROM folder_members WHERE user_id = ?`
4. Check direct assignments: `SELECT * FROM user_connections WHERE user_id = ?`

### **Query Blocked (403 Forbidden)**

1. Check SQL statement type (SELECT, INSERT, etc.)
2. Check user's connection permissions
3. Verify connection access first
4. Check if group is active

### **Can't Modify Role**

- System roles (admin, user) cannot be modified or deleted
- Roles with assigned users cannot be deleted

---

## Next Steps

1. **Explore RBAC UI**: Visit `/rbac` to manage roles visually
2. **Create Custom Roles**: Define roles matching your organization
3. **Set Up Groups**: Convert folders to access groups with permissions
4. **Test Scenarios**: Verify read-only users are properly restricted
5. **Document Your Policies**: Track which teams have access to what

The permission system is **live and enforcing**. All SQL queries are now checked against user permissions automatically! 🔒
