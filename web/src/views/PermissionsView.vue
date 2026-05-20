<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConnections } from '@/composables/useConnections'
import { useAuth } from '@/composables/useAuth'
import { readableError } from '@/utils/httpError'

const toast = useToast()
const route = useRoute()
const router = useRouter()
const { connections, fetchConnections } = useConnections()
const { hasPermission } = useAuth()

// ── Tab State ──
const activeTab = ref<'roles' | 'groups' | 'users'>('roles')
const canManageRoles = computed(() => hasPermission('roles.manage'))
const canManageGroups = computed(() => hasPermission('folders.manage'))
const canManageUsers = computed(() => hasPermission('users.manage'))
const availableTabs = computed(() => [
  ...(canManageRoles.value ? ['roles' as const] : []),
  ...(canManageGroups.value ? ['groups' as const] : []),
  ...(canManageUsers.value ? ['users' as const] : []),
])

function syncActiveTabFromRoute() {
  const requestedTab = route.query.tab
  const tab = typeof requestedTab === 'string' ? requestedTab : ''
  if (availableTabs.value.includes(tab as typeof activeTab.value)) {
    activeTab.value = tab as typeof activeTab.value
    return
  }
  activeTab.value = availableTabs.value[0] ?? 'roles'
}

function selectTab(tab: typeof activeTab.value) {
  activeTab.value = tab
  const query = { ...route.query }
  if (tab === 'roles') {
    delete query.tab
  } else {
    query.tab = tab
  }
  router.replace({ query })
}

// ── ROLES MODULE ──

interface Role {
  id: number
  name: string
  description: string
  permissions: string[]
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

function normalizePermissionList(value: unknown): string[] {
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === 'string')
  if (typeof value !== 'string') return []
  try {
    const parsed = JSON.parse(value || '[]')
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : []
  } catch {
    return []
  }
}

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
  } catch (error) {
    toast.error(readableError(error, { action: 'Save role', fallback: 'Failed to save role' }))
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
  } catch (error) {
    toast.error(readableError(error, { action: 'Save access group', fallback: 'Failed to save group' }))
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
  permissions: string[]
  effective_permissions: string[]
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
  is_active: true,
  permissions: [] as string[],
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

const groupConnectionAssignments = ref<Array<{ conn_id: number; source: string; permissions: string[] }>>([])

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

function getGroupSource(connId: number): string | null {
  const ga = groupConnectionAssignments.value.find(a => a.conn_id === connId)
  return ga ? ga.source : null
}

function isGroupAssigned(connId: number): boolean {
  return (
    !!getGroupSource(connId) &&
    !userForm.connection_assignments.some(a => a.conn_id === connId)
  )
}

async function fetchUsers() {
  usersLoading.value = true
  try {
    const { data } = await axios.get<User[]>('/api/admin/users')
    users.value = (data || []).map((user: any) => ({
      ...user,
      permissions: normalizePermissionList(user.permissions),
      effective_permissions: normalizePermissionList(user.effective_permissions),
    }))
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
    userForm.is_active = user.is_active
    userForm.permissions = normalizePermissionList(user.permissions)
    
    // Load user's direct connection assignments
    try {
      const { data } = await axios.get(`/api/users/${user.id}/connections`)
      const all = data || []
      userForm.connection_assignments = all
        .filter((a: any) => a.source === 'direct')
        .map((a: any) => ({
          conn_id: a.conn_id,
          permissions: Array.isArray(a.permissions) ? a.permissions : []
        }))
      groupConnectionAssignments.value = all
        .filter((a: any) => a.source !== 'direct')
        .map((a: any) => ({
          conn_id: a.conn_id,
          source: a.source,
          permissions: Array.isArray(a.permissions) ? a.permissions : []
        }))
    } catch (error: any) {
      // If endpoint doesn't exist yet, just start with empty
      if (error.response?.status === 404 || error.response?.status === 501) {
        userForm.connection_assignments = []
        groupConnectionAssignments.value = []
      } else {
        toast.error(readableError(error, { action: 'Load user connection assignments', fallback: 'Failed to load user connections' }))
        editingUser.value = null
        return
      }
    }
  } else {
    userForm.username = ''
    userForm.password = ''
    userForm.role_id = 2
    userForm.is_active = true
    userForm.permissions = []
    userForm.connection_assignments = []
    groupConnectionAssignments.value = []
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
      const payload: any = {
        role_id: userForm.role_id,
        is_active: userForm.is_active,
        permissions: userForm.permissions,
      }
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
        throw new Error(readableError(connError, { action: 'Update user connection assignments', fallback: 'Failed to update user connections' }))
      }
      
      toast.success('User updated successfully')
    } else {
      // Create new user
      const { data } = await axios.post('/api/auth/register', {
        username: userForm.username,
        password: userForm.password,
        role_id: userForm.role_id,
        permissions: userForm.permissions,
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
          throw new Error(readableError(connError, { action: 'Assign user connections', fallback: 'Failed to assign user connections' }))
        }
      }

      toast.success('User created successfully')
    }
    showUserForm.value = false
    await fetchUsers()
  } catch (error) {
    toast.error(readableError(error, { action: 'Save user', fallback: 'Failed to save user' }))
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

function getUserEffectivePermissions(user: User): string[] {
  const effective = user.effective_permissions.length ? user.effective_permissions : user.permissions
  const knownOrder = permissions.value.map((permission) => permission.key)
  return [...new Set(effective)].sort((a, b) => {
    const ai = knownOrder.indexOf(a)
    const bi = knownOrder.indexOf(b)
    if (ai === -1 && bi === -1) return a.localeCompare(b)
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  })
}

function hasDirectPermission(user: User, permission: string): boolean {
  return user.permissions.includes(permission)
}

function isRoleInherited(permKey: string): boolean {
  if (!editingUser.value) return false
  return (
    editingUser.value.effective_permissions.includes(permKey) &&
    !userForm.permissions.includes(permKey)
  )
}

function toggleDirectPermission(permKey: string) {
  const idx = userForm.permissions.indexOf(permKey)
  if (idx >= 0) {
    userForm.permissions.splice(idx, 1)
  } else {
    userForm.permissions.push(permKey)
  }
}

// ── Filter / Sort / Pagination / Custom Fields ─────────────────

// ROLES tab
const roleSearch = ref('')
const roleSortKey = ref<'name' | 'user_count' | 'permissions'>('name')
const roleSortDir = ref<'asc' | 'desc'>('asc')
const rolePage = ref(1)
const rolePageSize = ref(10)

const filteredRoles = computed(() => {
  let list = roles.value
  if (roleSearch.value.trim()) {
    const q = roleSearch.value.toLowerCase()
    list = list.filter(r => r.name.toLowerCase().includes(q) || r.description.toLowerCase().includes(q))
  }
  return [...list].sort((a, b) => {
    let av: any, bv: any
    if (roleSortKey.value === 'name') { av = a.name.toLowerCase(); bv = b.name.toLowerCase() }
    else if (roleSortKey.value === 'user_count') { av = a.user_count; bv = b.user_count }
    else { av = (a.permissions || []).length; bv = (b.permissions || []).length }
    if (av < bv) return roleSortDir.value === 'asc' ? -1 : 1
    if (av > bv) return roleSortDir.value === 'asc' ? 1 : -1
    return 0
  })
})

const roleTotalPages = computed(() => Math.max(1, Math.ceil(filteredRoles.value.length / rolePageSize.value)))
const pagedRoles = computed(() => {
  const s = (rolePage.value - 1) * rolePageSize.value
  return filteredRoles.value.slice(s, s + rolePageSize.value)
})

function setRoleSort(key: typeof roleSortKey.value) {
  if (roleSortKey.value === key) roleSortDir.value = roleSortDir.value === 'asc' ? 'desc' : 'asc'
  else { roleSortKey.value = key; roleSortDir.value = 'asc' }
}
function roleSortIcon(key: typeof roleSortKey.value) {
  if (roleSortKey.value !== key) return '↕'
  return roleSortDir.value === 'asc' ? '↑' : '↓'
}

const visibleRoleColumns = ref({ name: true, description: true, user_count: true, permissions: true })
const showRoleColMenu = ref(false)
const roleColDefs = [
  { key: 'name' as const, label: 'Role Name' },
  { key: 'description' as const, label: 'Description' },
  { key: 'user_count' as const, label: 'Users' },
  { key: 'permissions' as const, label: 'Permissions' },
]

watch(roleSearch, () => { rolePage.value = 1 })

// GROUPS tab
const groupSearch = ref('')
const groupSortKey = ref<'name' | 'visibility' | 'is_active'>('name')
const groupSortDir = ref<'asc' | 'desc'>('asc')
const groupPage = ref(1)
const groupPageSize = ref(10)

const filteredGroups = computed(() => {
  let list = groups.value
  if (groupSearch.value.trim()) {
    const q = groupSearch.value.toLowerCase()
    list = list.filter(g => g.name.toLowerCase().includes(q))
  }
  return [...list].sort((a, b) => {
    let av: any, bv: any
    if (groupSortKey.value === 'name') { av = a.name.toLowerCase(); bv = b.name.toLowerCase() }
    else if (groupSortKey.value === 'visibility') { av = a.visibility; bv = b.visibility }
    else { av = a.is_active ? 0 : 1; bv = b.is_active ? 0 : 1 }
    if (av < bv) return groupSortDir.value === 'asc' ? -1 : 1
    if (av > bv) return groupSortDir.value === 'asc' ? 1 : -1
    return 0
  })
})

const groupTotalPages = computed(() => Math.max(1, Math.ceil(filteredGroups.value.length / groupPageSize.value)))
const pagedGroups = computed(() => {
  const s = (groupPage.value - 1) * groupPageSize.value
  return filteredGroups.value.slice(s, s + groupPageSize.value)
})

function setGroupSort(key: typeof groupSortKey.value) {
  if (groupSortKey.value === key) groupSortDir.value = groupSortDir.value === 'asc' ? 'desc' : 'asc'
  else { groupSortKey.value = key; groupSortDir.value = 'asc' }
}
function groupSortIcon(key: typeof groupSortKey.value) {
  if (groupSortKey.value !== key) return '↕'
  return groupSortDir.value === 'asc' ? '↑' : '↓'
}

const visibleGroupColumns = ref({ name: true, visibility: true, role_restrict: true, is_active: true })
const showGroupColMenu = ref(false)
const groupColDefs = [
  { key: 'name' as const, label: 'Group Name' },
  { key: 'visibility' as const, label: 'Visibility' },
  { key: 'role_restrict' as const, label: 'Role Restriction' },
  { key: 'is_active' as const, label: 'Status' },
]

watch(groupSearch, () => { groupPage.value = 1 })

// USERS tab
const userSearch = ref('')
const userFilterRole = ref('')
const userFilterStatus = ref('')
const userSortKeyPerm = ref<'username' | 'role' | 'is_active' | 'created_at'>('username')
const userSortDirPerm = ref<'asc' | 'desc'>('asc')
const userPage = ref(1)
const userPageSize = ref(10)

const filteredUsers = computed(() => {
  let list = users.value
  if (userSearch.value.trim()) {
    const q = userSearch.value.toLowerCase()
    list = list.filter(u => u.username.toLowerCase().includes(q))
  }
  if (userFilterRole.value) list = list.filter(u => u.role === userFilterRole.value)
  if (userFilterStatus.value) {
    const active = userFilterStatus.value === 'active'
    list = list.filter(u => u.is_active === active)
  }
  return [...list].sort((a, b) => {
    let av: any, bv: any
    if (userSortKeyPerm.value === 'username') { av = a.username.toLowerCase(); bv = b.username.toLowerCase() }
    else if (userSortKeyPerm.value === 'role') { av = a.role; bv = b.role }
    else if (userSortKeyPerm.value === 'is_active') { av = a.is_active ? 0 : 1; bv = b.is_active ? 0 : 1 }
    else { av = a.created_at; bv = b.created_at }
    if (av < bv) return userSortDirPerm.value === 'asc' ? -1 : 1
    if (av > bv) return userSortDirPerm.value === 'asc' ? 1 : -1
    return 0
  })
})

const userTotalPages = computed(() => Math.max(1, Math.ceil(filteredUsers.value.length / userPageSize.value)))
const pagedUsers = computed(() => {
  const s = (userPage.value - 1) * userPageSize.value
  return filteredUsers.value.slice(s, s + userPageSize.value)
})

function setUserSortPerm(key: typeof userSortKeyPerm.value) {
  if (userSortKeyPerm.value === key) userSortDirPerm.value = userSortDirPerm.value === 'asc' ? 'desc' : 'asc'
  else { userSortKeyPerm.value = key; userSortDirPerm.value = 'asc' }
}
function userSortIconPerm(key: typeof userSortKeyPerm.value) {
  if (userSortKeyPerm.value !== key) return '↕'
  return userSortDirPerm.value === 'asc' ? '↑' : '↓'
}

const availableUserRoles = computed(() => [...new Set(users.value.map(u => u.role))].sort())
const hasUserFilters = computed(() => userSearch.value || userFilterRole.value || userFilterStatus.value)

const visibleUserColumns = ref({ username: true, role: true, effective_access: true, is_active: true, created_at: true })
const showUserColMenu = ref(false)
const userColDefs = [
  { key: 'username' as const, label: 'Username' },
  { key: 'role' as const, label: 'Role' },
  { key: 'effective_access' as const, label: 'Effective Access' },
  { key: 'is_active' as const, label: 'Status' },
  { key: 'created_at' as const, label: 'Created' },
]

watch([userSearch, userFilterRole, userFilterStatus], () => { userPage.value = 1 })

function closeAllColMenus() {
  showRoleColMenu.value = false
  showGroupColMenu.value = false
  showUserColMenu.value = false
}

function permPageBtn(current: number, total: number, target: number) {
  return Math.max(1, Math.min(target, total))
}

// ── Init ──

onMounted(async () => {
  syncActiveTabFromRoute()

  const tasks: Promise<unknown>[] = []
  if (canManageRoles.value || canManageUsers.value || canManageGroups.value) {
    tasks.push(fetchRoles())
  }
  if (canManageRoles.value || canManageUsers.value) {
    tasks.push(fetchPermissions())
  }
  if (canManageGroups.value) {
    tasks.push(fetchGroups())
  }
  if (canManageUsers.value) {
    tasks.push(fetchUsers(), fetchConnections())
    if (!canManageRoles.value) {
      tasks.push(fetchRoles())
    }
  }
  await Promise.all(tasks)
})

watch(() => route.query.tab, () => {
  syncActiveTabFromRoute()
})
</script>

<template>
  <div class="page-shell perm-root" @click="closeAllColMenus">
    <div class="page-scroll perm-scroll">
      <div class="page-stack">
      <section class="page-hero">
        <div class="page-hero__content">
          <div class="page-kicker">Administration</div>
          <div class="page-title">Permissions & Access Control</div>
          <div class="page-subtitle">Manage roles, access groups, user assignments, and direct connection permissions from one control surface.</div>
        </div>
      </section>

      <!-- Tabs -->
      <div class="page-tabs perm-tabs">
        <button
          v-if="canManageRoles"
          class="page-tab perm-tab"
          :class="{ 'is-active': activeTab === 'roles' }"
          @click="selectTab('roles')"
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
          v-if="canManageGroups"
          class="page-tab perm-tab"
          :class="{ 'is-active': activeTab === 'groups' }"
          @click="selectTab('groups')"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
          </svg>
          Access Groups
        </button>
        <button
          v-if="canManageUsers"
          class="page-tab perm-tab"
          :class="{ 'is-active': activeTab === 'users' }"
          @click="selectTab('users')"
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
      <div v-if="activeTab === 'roles'" class="page-card perm-panel">
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

        <!-- Roles filter bar -->
        <div class="perm-filter-bar" @click.stop>
          <div class="perm-filter-search">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input class="perm-filter-input" v-model="roleSearch" placeholder="Search roles…" />
          </div>
          <div class="perm-col-toggle" @click.stop>
            <button class="base-btn base-btn--ghost base-btn--xs" @click="showRoleColMenu = !showRoleColMenu">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18"/></svg>
              Columns
            </button>
            <div v-if="showRoleColMenu" class="perm-col-menu">
              <div class="perm-col-menu-title">Visible Columns</div>
              <label v-for="col in roleColDefs" :key="col.key" class="perm-col-item">
                <input type="checkbox" v-model="visibleRoleColumns[col.key]" /> {{ col.label }}
              </label>
            </div>
          </div>
          <select class="perm-filter-sel" v-model="rolePageSize" @change="rolePage = 1">
            <option :value="10">10 / page</option>
            <option :value="25">25 / page</option>
            <option :value="50">50 / page</option>
          </select>
          <span class="perm-filter-count">{{ filteredRoles.length }} of {{ roles.length }}</span>
        </div>

        <div v-if="rolesLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th v-if="visibleRoleColumns.name" @click="setRoleSort('name')" class="perm-th-sort">
                  Role Name <span class="perm-sort-icon" :class="{ active: roleSortKey === 'name' }">{{ roleSortIcon('name') }}</span>
                </th>
                <th v-if="visibleRoleColumns.description">Description</th>
                <th v-if="visibleRoleColumns.user_count" @click="setRoleSort('user_count')" class="perm-th-sort">
                  Users <span class="perm-sort-icon" :class="{ active: roleSortKey === 'user_count' }">{{ roleSortIcon('user_count') }}</span>
                </th>
                <th v-if="visibleRoleColumns.permissions" @click="setRoleSort('permissions')" class="perm-th-sort">
                  Permissions <span class="perm-sort-icon" :class="{ active: roleSortKey === 'permissions' }">{{ roleSortIcon('permissions') }}</span>
                </th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="role in pagedRoles" :key="role.id">
                <td v-if="visibleRoleColumns.name">
                  <div class="perm-role-name">
                    {{ role.name }}
                    <span v-if="role.is_system" class="perm-badge perm-badge--system">System</span>
                  </div>
                </td>
                <td v-if="visibleRoleColumns.description" class="perm-td-desc">{{ role.description }}</td>
                <td v-if="visibleRoleColumns.user_count" class="perm-td-count">{{ role.user_count }}</td>
                <td v-if="visibleRoleColumns.permissions" class="perm-td-perms">
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
              <tr v-if="pagedRoles.length === 0">
                <td :colspan="Object.values(visibleRoleColumns).filter(Boolean).length + 1" class="perm-empty">
                  {{ roleSearch ? 'No roles match the search' : 'No roles found' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Roles pagination -->
        <div v-if="!rolesLoading && roleTotalPages > 1" class="perm-pagination">
          <span class="perm-page-info">{{ (rolePage - 1) * rolePageSize + 1 }}–{{ Math.min(rolePage * rolePageSize, filteredRoles.length) }} of {{ filteredRoles.length }}</span>
          <div class="perm-page-controls">
            <button class="perm-page-btn" :disabled="rolePage === 1" @click="rolePage = permPageBtn(rolePage, roleTotalPages, 1)">«</button>
            <button class="perm-page-btn" :disabled="rolePage === 1" @click="rolePage = permPageBtn(rolePage, roleTotalPages, rolePage - 1)">‹</button>
            <template v-for="p in roleTotalPages" :key="p">
              <button v-if="Math.abs(p - rolePage) <= 2 || p === 1 || p === roleTotalPages" class="perm-page-btn" :class="{ 'perm-page-btn--active': p === rolePage }" @click="rolePage = p">{{ p }}</button>
              <span v-else-if="Math.abs(p - rolePage) === 3" class="perm-page-ellipsis">…</span>
            </template>
            <button class="perm-page-btn" :disabled="rolePage === roleTotalPages" @click="rolePage = permPageBtn(rolePage, roleTotalPages, rolePage + 1)">›</button>
            <button class="perm-page-btn" :disabled="rolePage === roleTotalPages" @click="rolePage = permPageBtn(rolePage, roleTotalPages, roleTotalPages)">»</button>
          </div>
        </div>
      </div>

      <!-- ═══════════════════════════════════════════════════════════════ -->
      <!-- ACCESS GROUPS TAB -->
      <!-- ═══════════════════════════════════════════════════════════════ -->
      <div v-if="activeTab === 'groups'" class="page-card perm-panel">
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

        <!-- Groups filter bar -->
        <div class="perm-filter-bar" @click.stop>
          <div class="perm-filter-search">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input class="perm-filter-input" v-model="groupSearch" placeholder="Search groups…" />
          </div>
          <div class="perm-col-toggle" @click.stop>
            <button class="base-btn base-btn--ghost base-btn--xs" @click="showGroupColMenu = !showGroupColMenu">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18"/></svg>
              Columns
            </button>
            <div v-if="showGroupColMenu" class="perm-col-menu">
              <div class="perm-col-menu-title">Visible Columns</div>
              <label v-for="col in groupColDefs" :key="col.key" class="perm-col-item">
                <input type="checkbox" v-model="visibleGroupColumns[col.key]" /> {{ col.label }}
              </label>
            </div>
          </div>
          <select class="perm-filter-sel" v-model="groupPageSize" @change="groupPage = 1">
            <option :value="10">10 / page</option>
            <option :value="25">25 / page</option>
            <option :value="50">50 / page</option>
          </select>
          <span class="perm-filter-count">{{ filteredGroups.length }} of {{ groups.length }}</span>
        </div>

        <div v-if="groupsLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th v-if="visibleGroupColumns.name" @click="setGroupSort('name')" class="perm-th-sort">
                  Group Name <span class="perm-sort-icon" :class="{ active: groupSortKey === 'name' }">{{ groupSortIcon('name') }}</span>
                </th>
                <th v-if="visibleGroupColumns.visibility" @click="setGroupSort('visibility')" class="perm-th-sort">
                  Visibility <span class="perm-sort-icon" :class="{ active: groupSortKey === 'visibility' }">{{ groupSortIcon('visibility') }}</span>
                </th>
                <th v-if="visibleGroupColumns.role_restrict">Role Restriction</th>
                <th v-if="visibleGroupColumns.is_active" @click="setGroupSort('is_active')" class="perm-th-sort">
                  Status <span class="perm-sort-icon" :class="{ active: groupSortKey === 'is_active' }">{{ groupSortIcon('is_active') }}</span>
                </th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="group in pagedGroups" :key="group.id">
                <td v-if="visibleGroupColumns.name">
                  <div class="perm-group-name">
                    <div class="perm-group-color" :style="{ backgroundColor: group.color }"></div>
                    {{ group.name }}
                  </div>
                </td>
                <td v-if="visibleGroupColumns.visibility" class="perm-td-desc">{{ group.visibility }}</td>
                <td v-if="visibleGroupColumns.role_restrict" class="perm-td-desc">
                  <span v-if="group.role_restrict" class="perm-badge">{{ group.role_restrict }}</span>
                  <span v-else class="perm-td-dim">All roles</span>
                </td>
                <td v-if="visibleGroupColumns.is_active">
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
              <tr v-if="pagedGroups.length === 0">
                <td :colspan="Object.values(visibleGroupColumns).filter(Boolean).length + 1" class="perm-empty">
                  {{ groupSearch ? 'No groups match the search' : 'No access groups found' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Groups pagination -->
        <div v-if="!groupsLoading && groupTotalPages > 1" class="perm-pagination">
          <span class="perm-page-info">{{ (groupPage - 1) * groupPageSize + 1 }}–{{ Math.min(groupPage * groupPageSize, filteredGroups.length) }} of {{ filteredGroups.length }}</span>
          <div class="perm-page-controls">
            <button class="perm-page-btn" :disabled="groupPage === 1" @click="groupPage = permPageBtn(groupPage, groupTotalPages, 1)">«</button>
            <button class="perm-page-btn" :disabled="groupPage === 1" @click="groupPage = permPageBtn(groupPage, groupTotalPages, groupPage - 1)">‹</button>
            <template v-for="p in groupTotalPages" :key="p">
              <button v-if="Math.abs(p - groupPage) <= 2 || p === 1 || p === groupTotalPages" class="perm-page-btn" :class="{ 'perm-page-btn--active': p === groupPage }" @click="groupPage = p">{{ p }}</button>
              <span v-else-if="Math.abs(p - groupPage) === 3" class="perm-page-ellipsis">…</span>
            </template>
            <button class="perm-page-btn" :disabled="groupPage === groupTotalPages" @click="groupPage = permPageBtn(groupPage, groupTotalPages, groupPage + 1)">›</button>
            <button class="perm-page-btn" :disabled="groupPage === groupTotalPages" @click="groupPage = permPageBtn(groupPage, groupTotalPages, groupTotalPages)">»</button>
          </div>
        </div>
      </div>

      <!-- ═══════════════════════════════════════════════════════════════ -->
      <!-- USERS TAB -->
      <!-- ═══════════════════════════════════════════════════════════════ -->
      <div v-if="activeTab === 'users'" class="page-card perm-panel">
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

        <!-- Users filter bar -->
        <div class="perm-filter-bar" @click.stop>
          <div class="perm-filter-search">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input class="perm-filter-input" v-model="userSearch" placeholder="Search username…" />
          </div>
          <select class="perm-filter-sel" v-model="userFilterRole">
            <option value="">All roles</option>
            <option v-for="r in availableUserRoles" :key="r" :value="r">{{ r }}</option>
          </select>
          <select class="perm-filter-sel" v-model="userFilterStatus">
            <option value="">All status</option>
            <option value="active">Active</option>
            <option value="inactive">Inactive</option>
          </select>
          <button v-if="hasUserFilters" class="perm-filter-clear" @click="userSearch = ''; userFilterRole = ''; userFilterStatus = ''">Clear</button>
          <div class="perm-col-toggle" @click.stop>
            <button class="base-btn base-btn--ghost base-btn--xs" @click="showUserColMenu = !showUserColMenu">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18"/></svg>
              Columns
            </button>
            <div v-if="showUserColMenu" class="perm-col-menu">
              <div class="perm-col-menu-title">Visible Columns</div>
              <label v-for="col in userColDefs" :key="col.key" class="perm-col-item">
                <input type="checkbox" v-model="visibleUserColumns[col.key]" /> {{ col.label }}
              </label>
            </div>
          </div>
          <select class="perm-filter-sel" v-model="userPageSize" @change="userPage = 1">
            <option :value="10">10 / page</option>
            <option :value="25">25 / page</option>
            <option :value="50">50 / page</option>
          </select>
          <span class="perm-filter-count">{{ filteredUsers.length }} of {{ users.length }}</span>
        </div>

        <div v-if="usersLoading" class="perm-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>

        <div v-else class="perm-table-wrap">
          <table class="perm-table">
            <thead>
              <tr>
                <th v-if="visibleUserColumns.username" @click="setUserSortPerm('username')" class="perm-th-sort">
                  Username <span class="perm-sort-icon" :class="{ active: userSortKeyPerm === 'username' }">{{ userSortIconPerm('username') }}</span>
                </th>
                <th v-if="visibleUserColumns.role" @click="setUserSortPerm('role')" class="perm-th-sort">
                  Role <span class="perm-sort-icon" :class="{ active: userSortKeyPerm === 'role' }">{{ userSortIconPerm('role') }}</span>
                </th>
                <th v-if="visibleUserColumns.effective_access">Effective Access</th>
                <th v-if="visibleUserColumns.is_active" @click="setUserSortPerm('is_active')" class="perm-th-sort">
                  Status <span class="perm-sort-icon" :class="{ active: userSortKeyPerm === 'is_active' }">{{ userSortIconPerm('is_active') }}</span>
                </th>
                <th v-if="visibleUserColumns.created_at" @click="setUserSortPerm('created_at')" class="perm-th-sort">
                  Created <span class="perm-sort-icon" :class="{ active: userSortKeyPerm === 'created_at' }">{{ userSortIconPerm('created_at') }}</span>
                </th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="user in pagedUsers" :key="user.id">
                <td v-if="visibleUserColumns.username"><strong>{{ user.username }}</strong></td>
                <td v-if="visibleUserColumns.role">
                  <span class="perm-role-badge" :style="{ borderColor: getRoleColor(user.role), color: getRoleColor(user.role) }">
                    {{ user.role }}
                  </span>
                </td>
                <td v-if="visibleUserColumns.effective_access" class="perm-td-perms">
                  <div v-if="getUserEffectivePermissions(user).length" class="perm-perms-display perm-perms-display--dense">
                    <span
                      v-for="perm in getUserEffectivePermissions(user)"
                      :key="perm"
                      class="perm-perm-badge"
                      :class="{ 'perm-perm-badge--direct': hasDirectPermission(user, perm) }"
                      :title="hasDirectPermission(user, perm) ? `${getPermissionLabel(perm)} - direct grant` : `${getPermissionLabel(perm)} - from role or inherited access`"
                    >
                      {{ getPermissionLabel(perm) }}
                      <span v-if="hasDirectPermission(user, perm)" class="perm-perm-source">Direct</span>
                    </span>
                  </div>
                  <span v-else class="perm-td-dim">No permissions</span>
                </td>
                <td v-if="visibleUserColumns.is_active">
                  <span class="perm-status" :class="{ 'perm-status--active': user.is_active }">
                    {{ user.is_active ? 'Active' : 'Inactive' }}
                  </span>
                </td>
                <td v-if="visibleUserColumns.created_at" class="perm-td-dim">{{ new Date(user.created_at).toLocaleDateString() }}</td>
                <td>
                  <div class="perm-row-actions">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openUserForm(user)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs perm-btn-del" @click="deleteUser(user)">Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="pagedUsers.length === 0">
                <td :colspan="Object.values(visibleUserColumns).filter(Boolean).length + 1" class="perm-empty">
                  {{ hasUserFilters ? 'No users match the current filters' : 'No users found' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Users pagination -->
        <div v-if="!usersLoading && userTotalPages > 1" class="perm-pagination">
          <span class="perm-page-info">{{ (userPage - 1) * userPageSize + 1 }}–{{ Math.min(userPage * userPageSize, filteredUsers.length) }} of {{ filteredUsers.length }}</span>
          <div class="perm-page-controls">
            <button class="perm-page-btn" :disabled="userPage === 1" @click="userPage = permPageBtn(userPage, userTotalPages, 1)">«</button>
            <button class="perm-page-btn" :disabled="userPage === 1" @click="userPage = permPageBtn(userPage, userTotalPages, userPage - 1)">‹</button>
            <template v-for="p in userTotalPages" :key="p">
              <button v-if="Math.abs(p - userPage) <= 2 || p === 1 || p === userTotalPages" class="perm-page-btn" :class="{ 'perm-page-btn--active': p === userPage }" @click="userPage = p">{{ p }}</button>
              <span v-else-if="Math.abs(p - userPage) === 3" class="perm-page-ellipsis">…</span>
            </template>
            <button class="perm-page-btn" :disabled="userPage === userTotalPages" @click="userPage = permPageBtn(userPage, userTotalPages, userPage + 1)">›</button>
            <button class="perm-page-btn" :disabled="userPage === userTotalPages" @click="userPage = permPageBtn(userPage, userTotalPages, userTotalPages)">»</button>
          </div>
        </div>
      </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════════════════════════ -->
    <!-- ROLE FORM MODAL -->
    <!-- ═══════════════════════════════════════════════════════════════ -->
    <Teleport to="body">
      <div v-if="showRoleForm" class="perm-overlay" @click.self="showRoleForm = false">
        <div class="page-modal perm-dialog perm-dialog--wide">
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
        <div class="page-modal perm-dialog">
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
        <div class="page-modal perm-dialog perm-dialog--wide">
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

            <label v-if="editingUser" class="perm-label">Account Status</label>
            <select v-if="editingUser" class="perm-select" v-model="userForm.is_active">
              <option :value="true">Active</option>
              <option :value="false">Locked</option>
            </select>

            <label class="perm-label">Direct Feature Permissions</label>
            <div class="perm-perms-container perm-perms-container--compact">
              <div v-for="(perms, group) in groupedPermissions" :key="group" class="perm-perm-group">
                <div class="perm-perm-group-header">{{ group }}</div>
                <div class="perm-perm-group-items">
                  <label v-for="perm in perms" :key="perm.key" class="perm-checkbox" :class="{ 'perm-checkbox--role-active': isRoleInherited(perm.key), 'perm-checkbox--direct-active': userForm.permissions.includes(perm.key) }">
                    <input
                      type="checkbox"
                      :class="{ 'perm-cb--inherited': isRoleInherited(perm.key) }"
                      :checked="userForm.permissions.includes(perm.key) || isRoleInherited(perm.key)"
                      @change="toggleDirectPermission(perm.key)"
                    />
                    <span class="perm-checkbox-label">
                      {{ perm.label }}
                      <span v-if="isRoleInherited(perm.key)" class="perm-role-badge">via role</span>
                    </span>
                  </label>
                </div>
              </div>
              <div v-if="permissions.length === 0" class="perm-empty-state">
                No feature permissions available
              </div>
            </div>

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
                  <div v-for="conn in conns" :key="conn.conn_id" class="perm-conn-item" :class="{ 'perm-conn-item--group-active': isGroupAssigned(conn.conn_id), 'perm-conn-item--direct-active': conn.selected }">
                    <label class="perm-conn-checkbox">
                      <input
                        type="checkbox"
                        :class="{ 'perm-cb--inherited': isGroupAssigned(conn.conn_id) }"
                        :checked="conn.selected || isGroupAssigned(conn.conn_id)"
                        @change="toggleConnectionSelection(conn.conn_id)"
                      />
                      <span class="perm-conn-name">
                        <span class="perm-conn-driver">{{ conn.driver.toUpperCase() }}</span>
                        {{ conn.name }}
                      </span>
                      <span class="perm-conn-status-badges">
                        <span v-if="conn.selected" class="perm-conn-assigned-badge">Assigned</span>
                        <span v-if="isGroupAssigned(conn.conn_id)" class="perm-role-badge">via {{ getGroupSource(conn.conn_id) }}</span>
                      </span>
                    </label>

                    <div v-if="conn.selected || isGroupAssigned(conn.conn_id)" class="perm-conn-perms">
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
  display: flex;
  flex-direction: column;
}

/* ─────────────────────────────────────────────────────────────── */
/* Tabs */
/* ─────────────────────────────────────────────────────────────── */

.perm-tabs {
  margin-bottom: 24px;
}

.perm-tab {
  font-size: 14px;
  font-weight: 500;
}
.perm-tab svg {
  opacity: 0.7;
}

.perm-tab.is-active svg {
  opacity: 1;
}

/* ─────────────────────────────────────────────────────────────── */
/* Panel */
/* ─────────────────────────────────────────────────────────────── */

.perm-panel {
  overflow: hidden;
}

.perm-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border);
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
  background: rgba(255, 255, 255, 0.02);
}

.perm-table th {
  padding: 12px 24px;
  text-align: left;
  font-weight: 600;
  font-size: 12px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

.perm-table td {
  padding: 14px 24px;
  border-top: 1px solid var(--border);
  color: var(--text-primary);
}

.perm-table tbody tr:hover {
  background: rgba(255, 255, 255, 0.03);
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
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.12em;
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
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  border: 1px solid;
}

.perm-status {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 999px;
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
  width: 100%;
  max-width: 500px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
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

.perm-perms-container--compact {
  max-height: 240px;
  padding: 12px;
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
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.perm-checkbox--role-active {
  background: color-mix(in srgb, var(--brand) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--brand) 30%, transparent);
  border-left: 3px solid color-mix(in srgb, var(--brand) 50%, transparent);
}

.perm-checkbox--role-active:hover {
  background: color-mix(in srgb, var(--brand) 15%, transparent);
}

.perm-cb--inherited {
  accent-color: var(--text-muted);
  opacity: 0.65;
}

.perm-checkbox--direct-active {
  background: color-mix(in srgb, var(--brand) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--brand) 45%, transparent);
  border-left: 3px solid var(--brand);
}

.perm-checkbox--direct-active:hover {
  background: color-mix(in srgb, var(--brand) 20%, transparent);
}

.perm-role-badge {
  display: inline-block;
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  background: color-mix(in srgb, var(--brand) 15%, transparent);
  color: var(--brand);
  border: 1px solid color-mix(in srgb, var(--brand) 30%, transparent);
  flex-shrink: 0;
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
  border-left: 3px solid transparent;
  border-radius: 6px;
  padding: 12px;
  transition: border-color 0.15s, background 0.15s;
}

.perm-conn-item--group-active {
  background: color-mix(in srgb, var(--brand) 8%, var(--bg-surface));
  border-color: color-mix(in srgb, var(--brand) 25%, var(--border));
  border-left-color: color-mix(in srgb, var(--brand) 55%, transparent);
}

.perm-conn-item--direct-active {
  background: color-mix(in srgb, var(--brand) 12%, var(--bg-surface));
  border-color: color-mix(in srgb, var(--brand) 50%, var(--border));
  border-left: 3px solid var(--brand);
}

.perm-conn-checkbox {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
  width: 100%;
}

.perm-conn-status-badges {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
  flex-shrink: 0;
}

.perm-conn-assigned-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  background: var(--brand);
  color: var(--brand-fg, #0d1117);
}

.perm-conn-assigned-badge::before {
  content: '✓';
  font-size: 10px;
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
  min-width: 360px;
  max-width: 560px;
}

.perm-perms-display {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: flex-start;
}

.perm-perms-display--dense {
  max-height: 116px;
  overflow: auto;
  padding-right: 4px;
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

.perm-perm-badge--direct {
  background: color-mix(in srgb, var(--accent) 12%, var(--bg-hover));
  border-color: color-mix(in srgb, var(--accent) 38%, var(--border));
}

.perm-perm-source {
  display: inline-block;
  margin-left: 6px;
  padding-left: 6px;
  border-left: 1px solid color-mix(in srgb, currentColor 24%, transparent);
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 600;
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

/* Filter bar */
.perm-filter-bar {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 20px 12px;
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
}
.perm-filter-search {
  display: flex; align-items: center; gap: 6px;
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 5px; padding: 5px 10px; flex: 1; min-width: 160px;
}
.perm-filter-search svg { color: var(--text-muted); flex-shrink: 0; }
.perm-filter-input {
  border: none; outline: none; background: transparent;
  color: var(--text-primary); font-size: 13px; width: 100%;
  font-family: inherit;
}
.perm-filter-input::placeholder { color: var(--text-muted); }
.perm-filter-sel {
  padding: 5px 8px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--bg-body); color: var(--text-primary);
  font-size: 12px; font-family: inherit; outline: none; cursor: pointer;
}
.perm-filter-clear {
  padding: 5px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: transparent; color: var(--text-muted);
  font-size: 12px; cursor: pointer; font-family: inherit;
  transition: color 0.15s, border-color 0.15s;
}
.perm-filter-clear:hover { color: var(--danger); border-color: var(--danger); }
.perm-filter-count { font-size: 12px; color: var(--text-muted); margin-left: auto; white-space: nowrap; }

/* Sortable table headers */
.perm-th-sort { cursor: pointer; user-select: none; white-space: nowrap; }
.perm-th-sort:hover { color: var(--text-primary); }
.perm-sort-icon { margin-left: 4px; font-size: 10px; color: var(--text-muted); opacity: 0.5; }
.perm-sort-icon.active { opacity: 1; color: var(--brand); }

/* Column toggle */
.perm-col-toggle { position: relative; }
.perm-col-menu {
  position: absolute; top: calc(100% + 6px); right: 0;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; padding: 10px 12px; min-width: 170px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3); z-index: 200;
}
.perm-col-menu-title {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.1em; color: var(--text-muted); margin-bottom: 8px;
}
.perm-col-item {
  display: flex; align-items: center; gap: 8px;
  font-size: 13px; color: var(--text-primary);
  padding: 4px 0; cursor: pointer;
}
.perm-col-item input { cursor: pointer; }

/* Pagination */
.perm-pagination {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 20px; border-top: 1px solid var(--border);
  flex-wrap: wrap; gap: 8px;
}
.perm-page-info { font-size: 12px; color: var(--text-muted); }
.perm-page-controls { display: flex; align-items: center; gap: 3px; }
.perm-page-btn {
  min-width: 30px; height: 30px; padding: 0 6px;
  border: 1px solid var(--border); border-radius: 5px;
  background: transparent; color: var(--text-primary);
  font-size: 12px; cursor: pointer; font-family: inherit;
  transition: background 0.12s, border-color 0.12s;
}
.perm-page-btn:hover:not(:disabled) { background: rgba(255,255,255,0.06); border-color: var(--brand); }
.perm-page-btn:disabled { opacity: 0.35; cursor: default; }
.perm-page-btn--active { background: var(--brand) !important; border-color: var(--brand) !important; color: #fff !important; }
.perm-page-ellipsis { padding: 0 4px; color: var(--text-muted); font-size: 12px; }
</style>
