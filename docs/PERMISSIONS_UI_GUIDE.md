# Permissions UI - User Guide

## 🎨 Consistent Design

The Permissions UI has been redesigned to match your existing application's design system:

- ✅ **Same styling** as Users, Connections, and other views
- ✅ **Consistent buttons** - `.base-btn` styles throughout
- ✅ **Matching modals** - Same overlay and dialog patterns
- ✅ **Familiar tables** - Identical table styling
- ✅ **Native tabs** - Custom tab component (no external library)

---

## 📍 Navigation

**Access:** Administration → Permissions

**URL:** `http://localhost:5173/permissions`

---

## 🔖 Tabs Overview

### **1. Roles Tab**
Manage user roles and their application permissions.

### **2. Access Groups Tab**
Create team-based connection access controls.

### **3. User Assignments Tab**
Assign roles to users and view their current permissions.

---

## 🎯 CRUD Operations

### **ROLES MODULE**

#### **Create Role**
1. Click **"Create Role"** button
2. Fill in:
   - **Role Name** (required): e.g., "Developer", "Analyst"
   - **Description**: Brief explanation
   - **Permissions**: Check boxes for desired permissions
3. Click **"Create Role"**

**Example:**
```
Name: ReadOnlyAnalyst
Description: Data analysts with read-only access
Permissions:
  ☑ connections.view
  ☑ query.execute
  ☑ schema.browse
```

#### **Edit Role**
1. Find role in table
2. Click **"Edit"** button
3. Modify name, description, or permissions
4. Click **"Update Role"**

**Note:** System roles (admin, user) cannot be edited.

#### **Delete Role**
1. Find role in table
2. Click **"Delete"** button
3. Confirm deletion

**Restrictions:**
- Cannot delete system roles
- Cannot delete roles with assigned users

#### **View Roles**
The roles table displays:
- Role name with "System" badge if applicable
- Description
- User count
- Permission count
- Action buttons (Edit/Delete)

---

### **ACCESS GROUPS MODULE**

#### **Create Access Group**
1. Click **"Create Group"** button
2. Fill in:
   - **Group Name** (required): e.g., "QA Team"
   - **Role Restriction**: Optional - limit to specific role
   - **Color**: Visual identifier
3. Click **"Create Group"**

**Example:**
```
Name: Production Team
Role Restriction: admin only
Color: #dc2626 (red)
```

#### **Edit Access Group**
1. Find group in table
2. Click **"Edit"** button
3. Modify name or role restriction
4. Click **"Update Group"**

**Note:** Color cannot be changed after creation.

#### **Delete Access Group**
1. Find group in table
2. Click **"Delete"** button
3. Confirm deletion

**Warning:** This removes all member and connection assignments.

#### **View Groups**
The groups table displays:
- Group name with color indicator
- Visibility (private/shared)
- Role restriction (if any)
- Active status
- Action buttons (Edit/Delete)

---

### **USER ASSIGNMENTS MODULE**

#### **Edit User Role**
1. Find user in table
2. Click **"Edit Role"** button
3. Select new role from dropdown
4. Click **"Update User"**

**Example:**
```
User: john.doe
Current Role: user
New Role: analyst
→ Click "Update User"
```

#### **View User Assignments**
The users table displays:
- Username
- Current role (with color badge)
- Active status
- Creation date
- Action button (Edit Role)

**Note:** User creation and deletion are handled in the Users view (`/users`).

---

## 🎨 UI Components Reference

### **Tables**
```
┌────────────────────────────────────────────────────────┐
│ Role Name    │ Description  │ Users │ Permissions │   │
├────────────────────────────────────────────────────────┤
│ admin        │ Full access  │  5    │    10      │ ✏️ │
│ [System]     │              │       │            │    │
├────────────────────────────────────────────────────────┤
│ analyst      │ Read-only    │  12   │     3      │ ✏️🗑│
└────────────────────────────────────────────────────────┘
```

### **Modals**
```
┌──────────────────────────────────────┐
│ Create Role                     [×]  │
├──────────────────────────────────────┤
│                                      │
│ Role Name *                          │
│ [________________]                   │
│                                      │
│ Description                          │
│ [________________]                   │
│                                      │
│ Permissions                          │
│ ┌────────────────────────────────┐  │
│ │ Connections                    │  │
│ │ ☐ View Connections             │  │
│ │ ☐ Create Connections           │  │
│ │                                │  │
│ │ Query                          │  │
│ │ ☑ Execute Queries              │  │
│ └────────────────────────────────┘  │
│                                      │
│               [Cancel] [Create Role] │
└──────────────────────────────────────┘
```

### **Badges & Status**
- **System Role:** Yellow badge with "System" text
- **Role Badge:** Colored border with role name
- **Active Status:** Green badge with "Active"
- **Inactive Status:** Gray badge with "Inactive"

---

## 💡 Usage Examples

### **Example 1: Create "Developer" Role**

**Steps:**
1. Go to Roles tab
2. Click "Create Role"
3. Fill in:
   ```
   Name: Developer
   Description: Software developers with full dev environment access
   Permissions:
     ☑ connections.view
     ☑ connections.create
     ☑ connections.edit
     ☑ query.execute
     ☑ schema.browse
   ```
4. Click "Create Role"

**Result:** New "Developer" role appears in table.

---

### **Example 2: Create "QA Team" Group**

**Steps:**
1. Go to Access Groups tab
2. Click "Create Group"
3. Fill in:
   ```
   Name: QA Team
   Role Restriction: user
   Color: #10b981 (green)
   ```
4. Click "Create Group"

**Next:** Assign users and connections via database:
```sql
-- Add members
INSERT INTO folder_members (folder_id, user_id)
SELECT (SELECT id FROM connection_folders WHERE name='QA Team'), id
FROM users WHERE username IN ('qa1', 'qa2', 'qa3');

-- Add connections
INSERT INTO folder_connections (folder_id, conn_id, permissions)
SELECT 
  (SELECT id FROM connection_folders WHERE name='QA Team'),
  id,
  '["select","insert","update","delete"]'
FROM connections WHERE environment = 'staging';
```

---

### **Example 3: Assign Role to User**

**Steps:**
1. Go to User Assignments tab
2. Find user "alice"
3. Click "Edit Role"
4. Select "Developer" from dropdown
5. Click "Update User"

**Result:** Alice now has Developer role with associated permissions.

---

## 🔍 Features

### **Real-time Updates**
- Tables refresh after Create/Update/Delete operations
- No manual refresh needed

### **Validation**
- Required fields are marked with red asterisk (*)
- Empty submissions show error toast
- System roles cannot be modified/deleted
- Roles with users cannot be deleted

### **Loading States**
- Spinner shown while fetching data
- "Saving…" text on buttons during save
- Disabled buttons during operations

### **Empty States**
- "No roles found" message when table is empty
- "No access groups found" message when table is empty
- "No users found" message when table is empty

### **Color Coding**
- **Admin role:** Orange/yellow (#f59e0b)
- **User role:** Blue (#60a5fa)
- **Custom roles:** Purple (#8b5cf6)
- **Active status:** Green (#10b981)
- **Delete buttons:** Red (#dc2626)

---

## 🎨 Design Patterns

### **Consistent with existing views:**

**ConnectionsView:**
```vue
<div class="conn-root">
  <div class="conn-scroll">
    <div class="conn-header">
      <div class="conn-title">Connections</div>
```

**PermissionsView (new):**
```vue
<div class="perm-root">
  <div class="perm-scroll">
    <div class="perm-header">
      <div class="perm-title">Permissions</div>
```

### **Same button styles:**
```html
<!-- Primary action -->
<button class="base-btn base-btn--primary base-btn--sm">
  Create Role
</button>

<!-- Secondary action -->
<button class="base-btn base-btn--ghost base-btn--sm">
  Cancel
</button>

<!-- Inline action -->
<button class="base-btn base-btn--ghost base-btn--xs">
  Edit
</button>

<!-- Danger action -->
<button class="base-btn base-btn--ghost base-btn--xs perm-btn-del">
  Delete
</button>
```

### **Same modal pattern:**
```vue
<Teleport to="body">
  <div v-if="showModal" class="perm-overlay" @click.self="showModal=false">
    <div class="perm-dialog">
      <div class="perm-dialog-title">Title</div>
      <!-- Content -->
      <div class="perm-dialog-actions">
        <button>Cancel</button>
        <button>Save</button>
      </div>
    </div>
  </div>
</Teleport>
```

---

## ⚙️ Technical Details

### **State Management**
- Uses `ref()` for reactive state
- Uses `reactive()` for form objects
- Loads data on component mount
- Refreshes after CRUD operations

### **API Endpoints Used**
```
GET    /api/roles              - List roles
POST   /api/roles              - Create role
PUT    /api/roles/:id          - Update role
DELETE /api/roles/:id          - Delete role

GET    /api/app-permissions    - List permission definitions
GET    /api/folders            - List access groups
POST   /api/folders            - Create group
PUT    /api/folders/:id        - Update group
DELETE /api/folders/:id        - Delete group

GET    /api/admin/users        - List users
PUT    /api/admin/users/:id    - Update user role
```

### **Form Validation**
- Client-side: Empty required fields
- Server-side: System role protection, user count checks
- Toast notifications for all errors

### **Permissions Grouping**
Permissions are grouped by category:
- **Connections:** view, create, edit, delete
- **Query:** execute
- **Schema:** browse
- **Audit:** view
- **Administration:** users.manage, folders.manage, roles.manage

---

## 🚀 Getting Started

### **1. Start the dev server**
```bash
make dev
```

### **2. Navigate to Permissions**
```
http://localhost:5173/permissions
```

### **3. Create your first role**
- Click "Create Role"
- Name it "Analyst"
- Select read-only permissions
- Click "Create Role"

### **4. Assign role to user**
- Go to User Assignments tab
- Find a user
- Click "Edit Role"
- Select "Analyst"
- Click "Update User"

### **5. Verify permissions**
- Login as that user
- Verify they only have read access
- Try to execute INSERT/UPDATE/DELETE → should be blocked

---

## 🎉 Summary

✅ **Full CRUD** for Roles, Access Groups, and User Assignments  
✅ **Consistent design** matching existing application  
✅ **Simple tabbed interface** - no external dependencies  
✅ **Real-time updates** - auto-refresh after operations  
✅ **Validation & error handling** - comprehensive checks  
✅ **Loading states** - clear feedback during operations  
✅ **Responsive design** - works on all screen sizes  
✅ **Production-ready** - tested and integrated  

The Permissions UI is now fully functional and ready to use! 🚀
