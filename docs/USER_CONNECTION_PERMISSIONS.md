# User Connection Permissions - User Guide

## 🎯 Overview

The Users module now includes **Direct Connection Permission Management**, allowing you to:
- ✅ Assign specific connections to users
- ✅ Set granular database permissions per connection
- ✅ View connections organized by folder
- ✅ Manage unfiled connections separately

---

## 📋 Features

### **1. Connection Organization**

Connections are displayed grouped by folders:

```
┌─────────────────────────────────────────┐
│ 📁 Production                           │
│   ☑ [PG] Production Main               │
│   ☐ [MY] Production Replica            │
├─────────────────────────────────────────┤
│ 📁 Development                          │
│   ☑ [PG] Dev Database                  │
│   ☐ [SQ] Local SQLite                  │
├─────────────────────────────────────────┤
│ 📁 Unfiled Connections                 │
│   ☐ [MY] Test Server                   │
└─────────────────────────────────────────┘
```

### **2. Granular DB Permissions**

For each selected connection, you can assign:
- ☑ **SELECT** - Read data
- ☑ **INSERT** - Add new rows
- ☑ **UPDATE** - Modify existing rows
- ☑ **DELETE** - Remove rows
- ☐ **CREATE** - Create tables/indexes
- ☐ **ALTER** - Modify table structure
- ☐ **DROP** - Delete tables/indexes

---

## 🚀 How to Use

### **Creating a User with Connection Permissions**

1. **Go to Permissions → Users tab**
2. **Click "Create User"**
3. **Fill in basic info:**
   - Username: `john.doe`
   - Password: `secure123`
   - Role: `user`

4. **Scroll to "Direct Connection Permissions"**
5. **Select connections:**
   - Check the box next to desired connections
   - Example: ☑ Production Main

6. **Set DB permissions:**
   - For Production Main, select:
     - ☑ SELECT (read only)
   
7. **Click "Create User"**

**Result:** John can only read from Production Main, no write operations allowed.

---

### **Editing User Connection Permissions**

1. **Find user in the table**
2. **Click "Edit" button**
3. **Modify connection assignments:**
   - Check/uncheck connections
   - Adjust permissions for each connection
4. **Click "Update User"**

---

### **Example Scenarios**

#### **Scenario 1: Read-Only Analyst**

**Goal:** Give analyst read-only access to analytics database.

**Steps:**
1. Create/Edit user
2. Assign role: `analyst`
3. Select connection: ☑ Analytics DB
4. Set permissions: ☑ SELECT only
5. Save

**Result:** User can query but not modify data.

---

#### **Scenario 2: Developer with Multiple Databases**

**Goal:** Give developer access to dev and staging with full permissions.

**Steps:**
1. Create/Edit user
2. Assign role: `user`
3. Select connections:
   - ☑ Dev Database
   - ☑ Staging Database
4. Set permissions for both:
   - ☑ SELECT
   - ☑ INSERT
   - ☑ UPDATE
   - ☑ DELETE
5. Save

**Result:** Developer can read and write to dev and staging.

---

#### **Scenario 3: DBA with Structure Permissions**

**Goal:** Give DBA full permissions including DDL operations.

**Steps:**
1. Create/Edit user
2. Assign role: `admin`
3. Select connection: ☑ Production Main
4. Set all permissions:
   - ☑ SELECT
   - ☑ INSERT
   - ☑ UPDATE
   - ☑ DELETE
   - ☑ CREATE
   - ☑ ALTER
   - ☑ DROP
5. Save

**Result:** DBA can perform all operations including schema changes.

---

## 🔍 Understanding Connection Sources

Users can have access to connections from three sources:

### **1. Direct Assignment** (This Feature)
- Manually assigned in Users module
- User-specific permissions
- Highest priority (acts as ceiling when combined with groups)

### **2. Access Groups**
- Inherited from group membership
- Shared permissions across team
- Union of all group permissions

### **3. Ownership**
- User created the connection
- Automatically has full access
- Cannot be restricted

---

## 🎨 UI Layout

```
┌──────────────────────────────────────────────────────┐
│ Edit User: john.doe                             [×]  │
├──────────────────────────────────────────────────────┤
│                                                       │
│ Username *                                            │
│ [john.doe____________] (disabled when editing)       │
│                                                       │
│ Password (leave blank to keep current)                │
│ [___________________]                                 │
│                                                       │
│ Assign Role *                                         │
│ [user ▼]                                              │
│                                                       │
│ Direct Connection Permissions                         │
│ ┌──────────────────────────────────────────────────┐ │
│ │ 📁 Production                                    │ │
│ │ ┌──────────────────────────────────────────────┐│ │
│ ││ ☑ [PG] Production Main                       ││ │
│ ││ ────────────────────────────────────────────  ││ │
│ ││ ☑ SELECT  ☐ INSERT  ☐ UPDATE  ☐ DELETE      ││ │
│ ││ ☐ CREATE  ☐ ALTER   ☐ DROP                  ││ │
│ │└──────────────────────────────────────────────┘│ │
│ │                                                  │ │
│ │ 📁 Development                                   │ │
│ │ ┌──────────────────────────────────────────────┐│ │
│ ││ ☑ [PG] Dev Database                          ││ │
│ ││ ────────────────────────────────────────────  ││ │
│ ││ ☑ SELECT  ☑ INSERT  ☑ UPDATE  ☑ DELETE      ││ │
│ ││ ☐ CREATE  ☐ ALTER   ☐ DROP                  ││ │
│ │└──────────────────────────────────────────────┘│ │
│ └──────────────────────────────────────────────────┘ │
│                                                       │
│                          [Cancel] [Update User]       │
└──────────────────────────────────────────────────────┘
```

---

## 🔐 Permission Resolution Logic

When a user tries to access a connection, the system checks:

### **Priority Order:**
1. **Is user admin?** → Full access ✓
2. **Does user own connection?** → Full access ✓
3. **Has direct assignment?** → Use those permissions
4. **Has group access?** → Use group permissions
5. **No access** → Denied ✗

### **Combined Access:**
```
If user has BOTH direct AND group access:
→ INTERSECTION (most restrictive wins)

Direct:     [SELECT, INSERT, UPDATE, DELETE]
Group:      [SELECT]
Result:     [SELECT] only

If user has ONLY group access from multiple groups:
→ UNION (most permissive wins)

Group A:    [SELECT]
Group B:    [SELECT, INSERT, UPDATE, DELETE]
Result:     [SELECT, INSERT, UPDATE, DELETE]
```

---

## 📊 Database Structure

### **Table: user_connections**
```sql
CREATE TABLE user_connections (
    user_id     INTEGER NOT NULL,
    conn_id     INTEGER NOT NULL,
    permissions TEXT DEFAULT '["select","insert","update","delete"]',
    PRIMARY KEY (user_id, conn_id)
)
```

### **Example Data:**
```sql
-- User 5 has read-only access to connection 10
INSERT INTO user_connections (user_id, conn_id, permissions)
VALUES (5, 10, '["select"]');

-- User 7 has read-write access to connection 20
INSERT INTO user_connections (user_id, conn_id, permissions)
VALUES (7, 20, '["select","insert","update","delete"]');
```

---

## 🔍 API Endpoints

### **Get User's Connections**
```http
GET /api/users/{id}/connections

Response:
[
  {
    "conn_id": 10,
    "name": "Production Main",
    "driver": "postgres",
    "source": "direct",
    "permissions": ["select"]
  },
  {
    "conn_id": 20,
    "name": "Dev Database",
    "driver": "postgres",
    "source": "QA Team",
    "permissions": ["select", "insert", "update", "delete"]
  }
]
```

### **Set User's Direct Connections**
```http
POST /api/users/{id}/connections

Body:
{
  "connection_ids": [10, 20],
  "connection_permissions": [
    {
      "conn_id": 10,
      "permissions": ["select"]
    },
    {
      "conn_id": 20,
      "permissions": ["select", "insert", "update", "delete"]
    }
  ]
}
```

---

## ✅ Testing

### **Test 1: Create User with Connection**

1. Go to Permissions → Users
2. Click "Create User"
3. Fill:
   - Username: testuser
   - Password: test123
   - Role: user
4. Select: Dev Database
5. Set: SELECT only
6. Click "Create User"
7. **Verify:** User appears in table

### **Test 2: Verify Permissions**

1. Login as testuser
2. Connect to Dev Database
3. Try SELECT query → ✓ Should work
4. Try INSERT query → ✗ Should fail with 403

### **Test 3: Update Permissions**

1. Login as admin
2. Edit testuser
3. Change Dev Database permissions to include INSERT
4. Click "Update User"
5. Login as testuser again
6. Try INSERT query → ✓ Should now work

---

## 🎯 Best Practices

### **1. Principle of Least Privilege**
✓ Give users only the minimum permissions they need
✗ Don't give everyone full access

### **2. Use Groups for Teams**
✓ Assign connections via Access Groups for teams
✓ Use direct assignments for individual exceptions
✗ Don't create individual assignments for every user

### **3. Separate Environments**
✓ Production: Read-only by default
✓ Staging: Read-write for developers
✓ Development: Full access including DDL

### **4. Regular Audits**
✓ Review user permissions quarterly
✓ Remove access for inactive users
✓ Check for excessive permissions

---

## 🚨 Important Notes

1. **Direct assignments override group permissions** when both exist (intersection)
2. **Connection owner always has full access** regardless of assignments
3. **Admin role bypasses all permission checks**
4. **Permissions are checked on every SQL query** in real-time
5. **Folder organization is visual only** - doesn't affect permissions

---

## 📝 Summary

✅ **Full CRUD for user connection assignments**  
✅ **Folder-based organization of connections**  
✅ **Unfiled connections shown separately**  
✅ **7 granular database permissions**  
✅ **Real-time permission enforcement**  
✅ **Works alongside group-based access**  
✅ **Intuitive checkbox UI**  
✅ **Production-ready and tested**  

The User Connection Permissions feature is now **fully functional**! 🎉
