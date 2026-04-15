<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConnections } from '@/composables/useConnections'

const toast = useToast()
const { connections, fetchConnections } = useConnections()

// ── Tab State ──
const activeTab = ref<'roles' | 'groups' | 'users'>('roles')

// ── ROLES MODULE ──

interface Role {
  id: number
  name: string
  description: string
  permissions: string
  is_system: boolean
  is_active: boolean
  user_count: number
  created_at: string
  updated_at: string
}

interface Permission {
  key: string
  label: string
  group: string
}

const roles = ref<Role[]>([])
const permissions = ref<Permission[]>([])
const rolesLoading = ref(false)
const showRoleForm = ref(false)
const editingRole = ref<Role | null>(null)
const roleSaving = ref(false)

const roleForm = reactive({
  name: '',
  description: '',
  permissions: [] as string[],
})

const groupedPermissions = computed(() => {
  const grouped: Record<string, Permission[]> = {}
  permissions.value.forEach(p => {
    if (!grouped[p.group]) grouped[p.group] = []
    grouped[p.group].push(p)
  })
  return grouped
})

async function fetchRoles() {
  rolesLoading.value = true
  try {
    const { data } = await axios.get<Role[]>('/api/roles')
    // Parse permissions if they're JSON strings
    roles.value = (data || []).map((role: any) => ({
      ...role,
      permissions: typeof role.permissions === 'string' 
        ? (JSON.parse(role.permissions || '[]')) 
        : (role.permissions || [])
    }))
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to load roles')
  } finally {
    rolesLoading.value = false
  }
}

async function fetchPermissions() {
  try {
    const { data } = await axios.get<Permission[]>('/api/app-permissions')
    permissions.value = data || []
  } catch (error: any) {
    console.error('Failed to fetch permissions:', error)
  }
}

function openRoleForm(role: Role | null = null) {
  editingRole.value = role
  if (role) {
    roleForm.name = role.name
    roleForm.description = role.description
    // Handle permissions whether they're already parsed or not
    if (Array.isArray(role.permissions)) {
      roleForm.permissions = role.permissions
    } else if (typeof role.permissions === 'string') {
      try {
        const perms = JSON.parse(role.permissions)
        roleForm.permissions = Array.isArray(perms) ? perms : []
      } catch {
        roleForm.permissions = []
      }
    } else {
      roleForm.permissions = []
    }
  } else {
    roleForm.name = ''
    roleForm.description = ''
    roleForm.permissions = []
  }
  showRoleForm.value = true
}

async function saveRole() {
  if (!roleForm.name.trim()) {
    toast.error('Role name is required')
    return
  }

  roleSaving.value = true
  try {
    if (editingRole.value) {
      await axios.put(`/api/roles/${editingRole.value.id}`, roleForm)
      toast.success('Role updated successfully')
    } else {
      await axios.post('/api/roles', roleForm)
      toast.success('Role created successfully')
    }
    showRoleForm.value = false
    await fetchRoles()
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to save role')
  } finally {
    roleSaving.value = false
  }
}

async function deleteRole(role: Role) {
  if (role.is_system) {
    toast.error('Cannot delete system role')
    return
  }
  if (role.user_count > 0) {
    toast.error('Cannot delete role with assigned users')
    return
  }
  if (!confirm(`Delete role "${role.name}"? This cannot be undone.`)) return

  try {
    await axios.delete(`/api/roles/${role.id}`)
    toast.success('Role deleted successfully')
    await fetchRoles()
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to delete role')
  }
}

// ── ACCESS GROUPS MODULE ──

interface AccessGroup {
  id: number
  name: string
  visibility: string
  color: string
  role_restrict: string
  is_active: boolean
  parent_id: number | null
  owner_id: number
  sort_order: number
  created_at: string
}

const groups = ref<AccessGroup[]>([])
const groupsLoading = ref(false)
const showGroupForm = ref(false)
const editingGroup = ref<AccessGroup | null>(null)
const groupSaving = ref(false)

const groupForm = reactive({
  name: '',
  visibility: 'shared',
  role_restrict: '',
  color: '#3B82F6',
})

async function fetchGroups() {
  groupsLoading.value = true
  try {
    const { data } = await axios.get<AccessGroup[]>('/api/folders')
    groups.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to load access groups')
  } finally {
    groupsLoading.value = false
  }
}

function openGroupForm(group: AccessGroup | null = null) {
  editingGroup.value = group
  if (group) {
    groupForm.name = group.name
    groupForm.visibility = group.visibility || 'shared'
    groupForm.role_restrict = group.role_restrict || ''
    groupForm.color = group.color || '#3B82F6'
  } else {
    groupForm.name = ''
    groupForm.visibility = 'shared'
    groupForm.role_restrict = ''
    groupForm.color = '#3B82F6'
  }
  showGroupForm.value = true
}

async function saveGroup() {
  if (!groupForm.name.trim()) {
    toast.error('Group name is required')
    return
  }

  groupSaving.value = true
  try {
    if (editingGroup.value) {
      await axios.put(`/api/folders/${editingGroup.value.id}`, {
        name: groupForm.name,
        visibility: groupForm.visibility,
        role_restrict: groupForm.role_restrict,
      })
      toast.success('Access group updated')
    } else {
      await axios.post('/api/folders', {
        name: groupForm.name,
        visibility: groupForm.visibility,
        color: groupForm.color,
        role_restrict: groupForm.role_restrict,
      })
      toast.success('Access group created')
    }
    showGroupForm.value = false
    await fetchGroups()
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to save group')
  } finally {
    groupSaving.value = false
  }
}

async function deleteGroup(group: AccessGroup) {
  if (!confirm(`Delete access group "${group.name}"? This will remove all member and connection assignments.`)) return

  try {
    await axios.delete(`/api/folders/${group.id}`)
    toast.success('Access group deleted')
    await fetchGroups()
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to delete group')
  }
}

// ── USERS MODULE ──

interface User {
  id: number
  username: string
  role: string
  role_id: number
  is_active: boolean
  created_at: string
}

const users = ref<User[]>([])
const usersLoading = ref(false)
const showUserForm = ref(false)
const editingUser = ref<User | null>(null)
const userSaving = ref(false)

const userForm = reactive({
  username: '',
  password: '',
  role_id: 2,
  connection_assignments: [] as Array<{
    conn_id: number
    permissions: string[]
  }>,
})

// Connection grouping
interface GroupedConnection {
  conn_id: number
  name: string
  driver: string
  folder_id: number | null
  selected: boolean
  permissions: string[]
}

const groupedConnections = computed(() => {
  const grouped: Record<string, GroupedConnection[]> = {}

  // Group connections by folder
  connections.value.forEach(conn => {
    const assignment = userForm.connection_assignments.find(a => a.conn_id === conn.id)
    
    const item: GroupedConnection = {
      conn_id: conn.id,
      name: conn.name,
      driver: conn.driver,
      folder_id: conn.folder_id || null,
      selected: !!assignment,
      permissions: assignment?.permissions || ['select', 'insert', 'update', 'delete']
    }

    // Find folder name
    let folderName = 'Unfiled Connections'
    if (conn.folder_id) {
      const folder = groups.value.find(g => g.id === conn.folder_id)
      if (folder) {
        folderName = folder.name
      }
    }

    if (!grouped[folderName]) grouped[folderName] = []
    grouped[folderName].push(item)
  })

  return grouped
})

const dbPermissions = [
  { key: 'select', label: 'SELECT (Read)' },
  { key: 'insert', label: 'INSERT (Add)' },
  { key: 'update', label: 'UPDATE (Modify)' },
  { key: 'delete', label: 'DELETE (Remove)' },
  { key: 'create', label: 'CREATE (Tables)' },
  { key: 'alter', label: 'ALTER (Structure)' },
  { key: 'drop', label: 'DROP (Delete Tables)' },
]

function toggleConnectionSelection(connId: number) {
  const idx = userForm.connection_assignments.findIndex(a => a.conn_id === connId)
  if (idx >= 0) {
    userForm.connection_assignments.splice(idx, 1)
  } else {
    userForm.connection_assignments.push({
      conn_id: connId,
      permissions: ['select', 'insert', 'update', 'delete']
    })
  }
}

function updateConnectionPermissions(connId: number, perms: string[]) {
  const assignment = userForm.connection_assignments.find(a => a.conn_id === connId)
  if (assignment) {
    assignment.permissions = perms
  }
}

async function fetchUsers() {
  usersLoading.value = true
  try {
    const { data } = await axios.get<User[]>('/api/admin/users')
    users.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to load users')
  } finally {
    usersLoading.value = false
  }
}

async function openUserForm(user: User | null = null) {
  editingUser.value = user
  if (user) {
    userForm.username = user.username
    userForm.password = ''
    userForm.role_id = user.role_id
    
    // Load user's direct connection assignments
    try {
      const { data } = await axios.get(`/api/users/${user.id}/connections`)
      console.log('Loaded user connections:', data)
      userForm.connection_assignments = (data || [])
        .filter((a: any) => a.source === 'direct')
        .map((a: any) => ({
          conn_id: a.conn_id,
          permissions: Array.isArray(a.permissions) ? a.permissions : []
        }))
      console.log('Processed assignments:', userForm.connection_assignments)
    } catch (error: any) {
      console.error('Failed to load user connections:', error)
      // If endpoint doesn't exist yet, just start with empty
      if (error.response?.status === 404 || error.response?.status === 501) {
        console.log('Endpoint not implemented, starting with empty assignments')
      } else {
        toast.error('Failed to load user connections')
      }
      userForm.connection_assignments = []
    }
  } else {
    userForm.username = ''
    userForm.password = ''
    userForm.role_id = 2
    userForm.connection_assignments = []
  }
  showUserForm.value = true
}

async function saveUser() {
  if (!userForm.username.trim()) {
    toast.error('Username is required')
    return
  }

  if (!editingUser.value && !userForm.password.trim()) {
    toast.error('Password is required for new users')
    return
  }

  userSaving.value = true
  try {
    let userId: number

    if (editingUser.value) {
      // Update existing user
      userId = editingUser.value.id
      const payload: any = { role_id: userForm.role_id }
      if (userForm.password.trim()) {
        payload.password = userForm.password
      }
      await axios.put(`/api/admin/users/${userId}`, payload)
      
      // Always update connection assignments (even if empty, to clear previous)
      try {
        await axios.post(`/api/users/${userId}/connections`, {
          connection_ids: userForm.connection_assignments.map(a => a.conn_id),
          connection_permissions: userForm.connection_assignments
        })
      } catch (connError) {
        console.error('Failed to update connections:', connError)
        // Continue even if connection assignment fails
      }
      
      toast.success('User updated successfully')
    } else {
      // Create new user
      const { data } = await axios.post('/api/auth/register', {
        username: userForm.username,
        password: userForm.password,
        role_id: userForm.role_id,
      })
      userId = data.id

      // Assign connections to new user
      if (userForm.connection_assignments.length > 0) {
        try {
          await axios.post(`/api/users/${userId}/connections`, {
            connection_ids: userForm.connection_assignments.map(a => a.conn_id),
            connection_permissions: userForm.connection_assignments
          })
        } catch (connError) {
          console.error('Failed to assign connections:', connError)
          // Continue even if connection assignment fails
        }
      }

      toast.success('User created successfully')
    }
    showUserForm.value = false
    await fetchUsers()
  } catch (error: any) {
    console.error('Save user error:', error)
    toast.error(error.response?.data || error.message || 'Failed to save user')
  } finally {
    userSaving.value = false
  }
}

async function deleteUser(user: User) {
  if (!confirm(`Delete user "${user.username}"? This cannot be undone.`)) return

  try {
    await axios.delete(`/api/admin/users/${user.id}`)
    toast.success('User deleted successfully')
    await fetchUsers()
  } catch (error: any) {
    toast.error(error.response?.data || 'Failed to delete user')
  }
}

// ── Helpers ──

function getRoleColor(name: string) {
  if (name === 'admin') return '#f59e0b'
  if (name === 'user') return '#60a5fa'
  return '#8b5cf6'
}

function getPermissionCount(permsJson: string) {
  try {
    const perms = JSON.parse(permsJson)
    return Array.isArray(perms) ? perms.length : 0
  } catch {
    return 0
  }
}

function getPermissionLabel(permKey: string): string {
  // Find in grouped permissions
  for (const group in groupedPermissions.value) {
    const perm = groupedPermissions.value[group].find((p: any) => p.key === permKey)
    if (perm) return perm.label
  }
  // Fallback: format the key
  return permKey
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

// ── Init ──

onMounted(async () => {
  await Promise.all([
    fetchRoles(),
    fetchPermissions(),
    fetchGroups(),
    fetchConnections(),
    fetchUsers(),
  ])
})
</script>

<template>
  <div class="perm-root">
    <div class="perm-scroll">
      <!-- Page Header -->
      <div class="perm-header">
        <div>
          <div class="perm-title">Permissions & Access Control</div>
          <div class="perm-sub">Manage roles, access groups, and user permissions</div>
        </div>
      </div>

      <!-- Tabs -->
      <div class="perm-tabs">
        <button
          class="perm-tab"
          :class="{ 'perm-tab--active': activeTab === 'roles' }"
          @click="activeTab = 'roles'"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
            <circle cx="9" cy="7" r="4"/>
            <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
            <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
          </svg>
          Roles
        </button>
        <button
          class="perm-tab"
          :class="{ 'perm-tab--active': activeTab === 'groups' }"
          @click="activeTab = 'groups'"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
          </svg>
          Access Groups
        </button>
        <button
          class="perm-tab"
          :class="{ 'perm-tab--active': activeTab === 'users' }"
          @click="activeTab = 'users'"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/>
            <circle cx="12" cy="7" r="4"/>
          </svg>
          Users
        </button>
      </div>

      <!-- ═══════════════════════════════════════════════════════════════ -->
      <!-- ROLES TAB -->
      <!-- ═══════════════════════════════════════════════════════════════ -->
      <div v-if="activeTab === 'roles'" class="perm-panel">
        <div class="perm-panel-header">
          <div>
            <div class="perm-panel-title">Roles</div>
            <div class="perm-panel-sub">Define roles with specific application permissions</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openRoleForm(null)">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Create Role
          </button>
        </div>

        <div v-if="rolesLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th>Role Name</th>
                <th>Description</th>
                <th>Users</th>
                <th>Permissions</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="role in roles" :key="role.id">
                <td>
                  <div class="perm-role-name">
                    {{ role.name }}
                    <span v-if="role.is_system" class="perm-badge perm-badge--system">System</span>
                  </div>
                </td>
                <td class="perm-td-desc">{{ role.description }}</td>
                <td class="perm-td-count">{{ role.user_count }}</td>
                <td class="perm-td-perms">
                  <div class="perm-perms-display">
                    <span 
                      v-for="perm in (role.permissions || [])" 
                      :key="perm" 
                      class="perm-perm-badge"
                      :title="getPermissionLabel(perm)"
                    >
                      {{ getPermissionLabel(perm) }}
                    </span>
                    <span v-if="!role.permissions || role.permissions.length === 0" class="perm-td-dim">No permissions</span>
                  </div>
                </td>
                <td>
                  <div class="perm-row-actions">
                    <button
                      class="base-btn base-btn--ghost base-btn--xs"
                      @click="openRoleForm(role)"
                    >Edit</button>
                    <button
                      v-if="!role.is_system && role.user_count === 0"
                      class="base-btn base-btn--ghost base-btn--xs perm-btn-del"
                      @click="deleteRole(role)"
                    >Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="roles.length === 0">
                <td colspan="5" class="perm-empty">No roles found</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ═══════════════════════════════════════════════════════════════ -->
      <!-- ACCESS GROUPS TAB -->
      <!-- ═══════════════════════════════════════════════════════════════ -->
      <div v-if="activeTab === 'groups'" class="perm-panel">
        <div class="perm-panel-header">
          <div>
            <div class="perm-panel-title">Access Groups</div>
            <div class="perm-panel-sub">Team-based connection access management</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openGroupForm(null)">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Create Group
          </button>
        </div>

        <div v-if="groupsLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th>Group Name</th>
                <th>Visibility</th>
                <th>Role Restriction</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="group in groups" :key="group.id">
                <td>
                  <div class="perm-group-name">
                    <div class="perm-group-color" :style="{ backgroundColor: group.color }"></div>
                    {{ group.name }}
                  </div>
                </td>
                <td class="perm-td-desc">{{ group.visibility }}</td>
                <td class="perm-td-desc">
                  <span v-if="group.role_restrict" class="perm-badge">{{ group.role_restrict }}</span>
                  <span v-else class="perm-td-dim">All roles</span>
                </td>
                <td>
                  <span class="perm-status" :class="{ 'perm-status--active': group.is_active }">
                    {{ group.is_active ? 'Active' : 'Inactive' }}
                  </span>
                </td>
                <td>
                  <div class="perm-row-actions">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openGroupForm(group)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs perm-btn-del" @click="deleteGroup(group)">Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="groups.length === 0">
                <td colspan="5" class="perm-empty">No access groups found</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ═══════════════════════════════════════════════════════════════ -->
      <!-- USERS TAB -->
      <!-- ═══════════════════════════════════════════════════════════════ -->
      <div v-if="activeTab === 'users'" class="perm-panel">
        <div class="perm-panel-header">
          <div>
            <div class="perm-panel-title">User Management</div>
            <div class="perm-panel-sub">Create, edit, and manage user accounts</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openUserForm(null)">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Create User
          </button>
        </div>

        <div v-if="usersLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th>Username</th>
                <th>Role</th>
                <th>Status</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="user in users" :key="user.id">
                <td><strong>{{ user.username }}</strong></td>
                <td>
                  <span class="perm-role-badge" :style="{ borderColor: getRoleColor(user.role), color: getRoleColor(user.role) }">
                    {{ user.role }}
                  </span>
                </td>
                <td>
                  <span class="perm-status" :class="{ 'perm-status--active': user.is_active }">
                    {{ user.is_active ? 'Active' : 'Inactive' }}
                  </span>
                </td>
                <td class="perm-td-dim">{{ new Date(user.created_at).toLocaleDateString() }}</td>
                <td>
                  <div class="perm-row-actions">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openUserForm(user)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs perm-btn-del" @click="deleteUser(user)">Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="users.length === 0">
                <td colspan="5" class="perm-empty">No users found</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════ -->
    <!-- ROLE FORM MODAL -->
    <!-- ═══════════════════════════════════════════════════════════════ -->
    <Teleport to="body">
      <div v-if="showRoleForm" class="perm-overlay" @click.self="showRoleForm = false">
        <div class="perm-dialog perm-dialog--wide">
          <div class="perm-dialog-header">
            <div class="perm-dialog-title">{{ editingRole ? 'Edit Role' : 'Create Role' }}</div>
            <button class="perm-dialog-close" @click="showRoleForm = false">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>

          <div class="perm-dialog-body">
            <label class="perm-label">Role Name <span class="perm-required">*</span></label>
            <input 
              class="perm-input" 
              v-model="roleForm.name" 
              placeholder="e.g. Developer, Analyst, DBA"
              :disabled="editingRole?.is_system"
              :title="editingRole?.is_system ? 'System role names cannot be changed' : ''"
            />
            <div v-if="editingRole?.is_system" class="perm-hint">System role name cannot be changed</div>

            <label class="perm-label">Description</label>
            <textarea class="perm-textarea" v-model="roleForm.description" rows="2" placeholder="Brief description of this role"></textarea>

            <label class="perm-label">Permissions</label>
            <div class="perm-perms-container">
              <div v-for="(perms, group) in groupedPermissions" :key="group" class="perm-perm-group">
                <div class="perm-perm-group-header">{{ group }}</div>
                <div class="perm-perm-group-items">
                  <label v-for="perm in perms" :key="perm.key" class="perm-checkbox">
                    <input type="checkbox" :value="perm.key" v-model="roleForm.permissions" />
                    <span class="perm-checkbox-label">{{ perm.label }}</span>
                  </label>
                </div>
              </div>
            </div>
          </div>

          <div class="perm-dialog-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showRoleForm = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveRole" :disabled="roleSaving">
              {{ roleSaving ? 'Saving…' : (editingRole ? 'Update Role' : 'Create Role') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- ═══════════════════════════════════════════════════════════════ -->
    <!-- GROUP FORM MODAL -->
    <!-- ═══════════════════════════════════════════════════════════════ -->
    <Teleport to="body">
      <div v-if="showGroupForm" class="perm-overlay" @click.self="showGroupForm = false">
        <div class="perm-dialog">
          <div class="perm-dialog-header">
            <div class="perm-dialog-title">{{ editingGroup ? 'Edit Access Group' : 'Create Access Group' }}</div>
            <button class="perm-dialog-close" @click="showGroupForm = false">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>

          <div class="perm-dialog-body">
            <label class="perm-label">Group Name <span class="perm-required">*</span></label>
            <input class="perm-input" v-model="groupForm.name" placeholder="e.g. QA Team, Production DBAs" />

            <label class="perm-label">Visibility <span class="perm-required">*</span></label>
            <select class="perm-select" v-model="groupForm.visibility">
              <option value="private">Private</option>
              <option value="shared">Shared</option>
              <option value="public">Public</option>
            </select>

            <label class="perm-label">Role Restriction</label>
            <select class="perm-select" v-model="groupForm.role_restrict">
              <option value="">No restriction (all roles)</option>
              <option v-for="role in roles" :key="role.id" :value="role.name">{{ role.name }} only</option>
            </select>

            <label class="perm-label" v-if="!editingGroup">Color</label>
            <input v-if="!editingGroup" class="perm-color-input" type="color" v-model="groupForm.color" />
          </div>

          <div class="perm-dialog-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showGroupForm = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveGroup" :disabled="groupSaving">
              {{ groupSaving ? 'Saving…' : (editingGroup ? 'Update Group' : 'Create Group') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- ═══════════════════════════════════════════════════════════════ -->
    <!-- USER FORM MODAL -->
    <!-- ═══════════════════════════════════════════════════════════════ -->
    <Teleport to="body">
      <div v-if="showUserForm" class="perm-overlay" @click.self="showUserForm = false">
        <div class="perm-dialog perm-dialog--wide">
          <div class="perm-dialog-header">
            <div class="perm-dialog-title">{{ editingUser ? 'Edit User' : 'Create User' }}</div>
            <button class="perm-dialog-close" @click="showUserForm = false">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>

          <div class="perm-dialog-body">
            <label class="perm-label">Username <span class="perm-required">*</span></label>
            <input 
              class="perm-input" 
              v-model="userForm.username" 
              placeholder="Username" 
              :disabled="!!editingUser"
            />

            <label class="perm-label">
              Password 
              <span v-if="!editingUser" class="perm-required">*</span>
              <span v-else class="perm-hint">(leave blank to keep current)</span>
            </label>
            <input 
              class="perm-input" 
              type="password" 
              v-model="userForm.password" 
              placeholder="Password"
              autocomplete="new-password"
            />

            <label class="perm-label">Assign Role <span class="perm-required">*</span></label>
            <select class="perm-select" v-model="userForm.role_id">
              <option v-for="role in roles" :key="role.id" :value="role.id">{{ role.name }}</option>
            </select>

            <label class="perm-label">Direct Connection Permissions</label>
            <div class="perm-connections-container">
              <div v-for="(conns, folderName) in groupedConnections" :key="folderName" class="perm-conn-folder">
                <div class="perm-conn-folder-header">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
                  </svg>
                  {{ folderName }}
                </div>
                <div class="perm-conn-list">
                  <div v-for="conn in conns" :key="conn.conn_id" class="perm-conn-item">
                    <label class="perm-conn-checkbox">
                      <input 
                        type="checkbox" 
                        :checked="conn.selected"
                        @change="toggleConnectionSelection(conn.conn_id)"
                      />
                      <span class="perm-conn-name">
                        <span class="perm-conn-driver">{{ conn.driver.toUpperCase() }}</span>
                        {{ conn.name }}
                      </span>
                    </label>
                    
                    <div v-if="conn.selected" class="perm-conn-perms">
                      <label v-for="perm in dbPermissions" :key="perm.key" class="perm-perm-checkbox">
                        <input 
                          type="checkbox"
                          :value="perm.key"
                          :checked="conn.permissions.includes(perm.key)"
                          @change="(e) => {
                            const target = e.target as HTMLInputElement
                            const perms = conn.permissions.filter(p => p !== perm.key)
                            if (target.checked) perms.push(perm.key)
                            updateConnectionPermissions(conn.conn_id, perms)
                          }"
                        />
                        <span>{{ perm.label }}</span>
                      </label>
                    </div>
                  </div>
                </div>
              </div>

              <div v-if="Object.keys(groupedConnections).length === 0" class="perm-empty-state">
                No connections available
              </div>
            </div>
          </div>

          <div class="perm-dialog-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showUserForm = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveUser" :disabled="userSaving">
              {{ userSaving ? 'Saving…' : (editingUser ? 'Update User' : 'Create User') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.perm-root {
  position: relative;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-body);
}

.perm-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 24px 32px;
}

.perm-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
}

.perm-title {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
  margin-bottom: 4px;
}

.perm-sub {
  font-size: 14px;
  color: var(--text-muted);
}

/* ─────────────────────────────────────────────────────────────── */
/* Tabs */
/* ─────────────────────────────────────────────────────────────── */

.perm-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--border);
  padding-bottom: 0;
}

.perm-tab {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  position: relative;
  top: 1px;
}

.perm-tab:hover {
  color: var(--text-primary);
  background: var(--bg-dim);
}

.perm-tab--active {
  color: var(--brand);
  border-bottom-color: var(--brand);
}

.perm-tab svg {
  opacity: 0.7;
}

.perm-tab--active svg {
  opacity: 1;
}

/* ─────────────────────────────────────────────────────────────── */
/* Panel */
/* ─────────────────────────────────────────────────────────────── */

.perm-panel {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}

.perm-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}

.perm-panel-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 2px;
}

.perm-panel-sub {
  font-size: 13px;
  color: var(--text-muted);
}

/* ─────────────────────────────────────────────────────────────── */
/* Table */
/* ─────────────────────────────────────────────────────────────── */

.perm-table-wrap {
  overflow-x: auto;
}

.perm-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.perm-table thead {
  background: var(--bg-surface);
}

.perm-table th {
  padding: 12px 24px;
  text-align: left;
  font-weight: 600;
  font-size: 13px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.perm-table td {
  padding: 14px 24px;
  border-top: 1px solid var(--border);
  color: var(--text-primary);
}

.perm-table tbody tr:hover {
  background: var(--bg-dim);
}

.perm-td-desc {
  color: var(--text-muted);
  font-size: 13px;
}

.perm-td-dim {
  color: var(--text-muted);
  font-size: 13px;
}

.perm-td-count {
  text-align: center;
  font-weight: 600;
  color: var(--text-muted);
}

.perm-empty {
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
  padding: 40px 20px !important;
}

.perm-loading {
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 60px 20px;
  color: var(--brand);
}

/* ─────────────────────────────────────────────────────────────── */
/* Table Components */
/* ─────────────────────────────────────────────────────────────── */

.perm-role-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}

.perm-group-name {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 500;
}

.perm-group-color {
  width: 4px;
  height: 24px;
  border-radius: 2px;
  flex-shrink: 0;
}

.perm-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  background: var(--bg-dim);
  color: var(--text-muted);
}

.perm-badge--system {
  background: #fef3c7;
  color: #92400e;
}

.perm-role-badge {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  border: 1px solid;
}

.perm-status {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  background: var(--bg-dim);
  color: var(--text-muted);
}

.perm-status--active {
  background: #d1fae5;
  color: #065f46;
}

.perm-row-actions {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}

.perm-btn-del {
  color: #dc2626 !important;
}

.perm-btn-del:hover {
  background: #fee2e2 !important;
}

/* ─────────────────────────────────────────────────────────────── */
/* Modal */
/* ─────────────────────────────────────────────────────────────── */

.perm-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
  padding: 20px;
}

.perm-dialog {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 100%;
  max-width: 500px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0,0,0,0.5);
}

.perm-dialog--wide {
  max-width: 700px;
}

.perm-dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.perm-dialog-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.perm-dialog-close {
  background: transparent;
  border: none;
  padding: 4px;
  cursor: pointer;
  color: var(--text-muted);
  transition: color 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.perm-dialog-close:hover {
  color: var(--text-primary);
}

.perm-dialog-body {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
}

.perm-dialog-footer {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  padding: 16px 24px;
  border-top: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
}

/* ─────────────────────────────────────────────────────────────── */
/* Form Elements */
/* ─────────────────────────────────────────────────────────────── */

.perm-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 6px;
  margin-top: 16px;
}

.perm-label:first-child {
  margin-top: 0;
}

.perm-required {
  color: #dc2626;
}

.perm-hint {
  font-weight: 400;
  color: var(--text-muted);
  font-size: 12px;
}

.perm-input,
.perm-textarea,
.perm-select {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 14px;
  color: var(--text-primary);
  background: var(--bg-body);
  transition: border-color 0.15s;
  font-family: inherit;
}

.perm-input:focus,
.perm-textarea:focus,
.perm-select:focus {
  outline: none;
  border-color: var(--brand);
}

.perm-input:disabled {
  background: var(--bg-dim);
  cursor: not-allowed;
  color: var(--text-muted);
}

.perm-textarea {
  resize: vertical;
  min-height: 60px;
}

.perm-color-input {
  width: 100%;
  height: 40px;
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
}

/* ─────────────────────────────────────────────────────────────── */
/* Permissions Container */
/* ─────────────────────────────────────────────────────────────── */

.perm-perms-container {
  margin-top: 8px;
  padding: 16px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  max-height: 320px;
  overflow-y: auto;
}

.perm-perm-group {
  margin-bottom: 20px;
}

.perm-perm-group:last-child {
  margin-bottom: 0;
}

.perm-perm-group-header {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--brand);
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.perm-perm-group-items {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 8px;
}

.perm-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  cursor: pointer;
  user-select: none;
  border-radius: 4px;
  transition: background 0.15s;
}

.perm-checkbox:hover {
  background: var(--bg-dim);
}

.perm-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
  flex-shrink: 0;
}

.perm-checkbox-label {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
}

/* ─────────────────────────────────────────────────────────────── */
/* Connection Permissions */
/* ─────────────────────────────────────────────────────────────── */

.perm-connections-container {
  margin-top: 8px;
  padding: 16px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  max-height: 400px;
  overflow-y: auto;
}

.perm-conn-folder {
  margin-bottom: 20px;
}

.perm-conn-folder:last-child {
  margin-bottom: 0;
}

.perm-conn-folder-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--brand);
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.perm-conn-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.perm-conn-item {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px;
}

.perm-conn-checkbox {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
}

.perm-conn-checkbox input[type="checkbox"] {
  width: 18px;
  height: 18px;
  cursor: pointer;
  flex-shrink: 0;
}

.perm-conn-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
}

.perm-conn-driver {
  display: inline-block;
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 700;
  background: var(--brand);
  color: var(--brand-fg);
}

.perm-conn-perms {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.perm-perm-checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-primary);
  cursor: pointer;
  user-select: none;
  padding: 4px;
  border-radius: 4px;
  transition: background 0.15s;
}

.perm-perm-checkbox:hover {
  background: var(--bg-hover);
}

.perm-perm-checkbox input[type="checkbox"] {
  width: 14px;
  height: 14px;
  cursor: pointer;
  flex-shrink: 0;
}

.perm-empty-state {
  text-align: center;
  padding: 40px 20px;
  color: var(--text-muted);
  font-size: 13px;
}

/* ─────────────────────────────────────────────────────────────── */
/* Permission Badges in Table */
/* ─────────────────────────────────────────────────────────────── */

.perm-td-perms {
  max-width: 400px;
}

.perm-perms-display {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: flex-start;
}

.perm-perm-badge {
  display: inline-block;
  padding: 3px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  background: var(--bg-hover);
  color: var(--text-primary);
  border: 1px solid var(--border);
  white-space: nowrap;
}

/* ─────────────────────────────────────────────────────────────── */
/* Hint Text */
/* ─────────────────────────────────────────────────────────────── */

.perm-hint {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: -8px;
  margin-bottom: 8px;
  font-style: italic;
}

/* ─────────────────────────────────────────────────────────────── */
/* Spinner */
/* ─────────────────────────────────────────────────────────────── */

@keyframes spin {
  to { transform: rotate(360deg); }
}

.spin {
  animation: spin 1s linear infinite;
}
</style>
