# UI Permission Fixes

## Issue
Non-admin users (like "andi" with "user" role) were seeing admin-only menu items and could attempt to access them, resulting in permission denied errors.

## Solution

### 1. Frontend Menu Filtering (`TopNav.vue`)

Added permission-based menu filtering:

```typescript
// Check if user is admin
const isAdmin = computed(() => !authEnabled.value || user.value?.role === 'admin')

// Mark menu groups that require admin access
const allMenuGroups = [
  // ... data menu (accessible to all)
  {
    id: 'admin',
    label: 'Administration',
    requiresAdmin: true, // ← Admin-only
    items: [...]
  },
  {
    id: 'tools',
    label: 'Tools',
    requiresAdmin: true, // ← Admin-only
    items: [...]
  },
  {
    id: 'monitor',
    label: 'Monitoring',
    requiresAdmin: true, // ← Admin-only
    items: [...]
  }
]

// Filter menu groups based on permissions
const menuGroups = computed(() => {
  return allMenuGroups.filter(group => {
    if (group.requiresAdmin && !isAdmin.value) {
      return false
    }
    return true
  })
})
```

### 2. Route Guards (`router/index.ts`)

Added `meta: { requiresAdmin: true }` to admin-only routes:

**Admin-Only Routes:**
- `/users` - User Management
- `/connections` - Connection Management  
- `/permissions` - Permissions & RBAC
- `/audit` - Audit Log
- `/diff` - Schema Diff
- `/scheduler` - Scheduler
- `/backup` - Backup
- `/health` - Health Monitoring
- `/watcher` - Watcher
- `/row-history` - Row History

**Route Guard Logic:**
```typescript
router.beforeEach((to) => {
  const { isAuthenticated, authEnabled, user } = useAuth()

  // Admin-only routes - require admin role
  if (to.meta.requiresAdmin && authEnabled.value) {
    if (!isAuthenticated.value) {
      return { name: 'login' }
    }
    if (user.value?.role !== 'admin') {
      return { name: 'welcome' } // Redirect non-admins to home
    }
  }
})
```

## Result

**For Admin Users (pandhu):**
- ✅ See all menu items (Data, Administration, Tools, Monitoring)
- ✅ Can access all routes

**For Regular Users (andi):**
- ✅ Only see "Data" menu with:
  - Dashboard
  - Data Browser
  - ER Diagram
  - Saved Queries
- ❌ Cannot see Administration, Tools, or Monitoring menus
- ❌ Cannot access admin routes directly (URL protection)
- ✅ Read-only access to connections (SELECT only)

## Testing

1. **Login as andi (user role):**
   - Should only see "Home" and "Data" in top navigation
   - No "Administration", "Tools", or "Monitoring" menus
   - Can browse data but cannot modify

2. **Login as pandhu (admin role):**
   - Should see all menu items
   - Full access to all features
   - Can manage users, connections, and permissions

3. **Direct URL Access:**
   - Try accessing `/permissions` as andi
   - Should be redirected to home page
   - Admin routes are protected

## Backend Permission Enforcement

The backend already has permission checks in place that will:
- ✅ Check user ownership of connections
- ✅ Default to allowing access for legacy connections (no owner_id)
- ✅ Fall back to permission system for explicit restrictions
- ✅ Admin role bypasses all checks

This provides a layered security approach:
1. **UI Layer**: Hide inaccessible features (UX improvement)
2. **Router Layer**: Prevent direct URL navigation (client security)
3. **Backend Layer**: Enforce permissions on API calls (server security)
