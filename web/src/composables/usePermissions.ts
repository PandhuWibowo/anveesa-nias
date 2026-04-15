import { ref } from 'vue'
import axios from 'axios'

export interface Role {
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

export interface Permission {
  key: string
  label: string
  group: string
}

export interface AccessGroup {
  id: number
  name: string
  parent_id: number | null
  owner_id: number
  visibility: string
  color: string
  role_restrict: string
  is_active: boolean
  sort_order: number
  member_count: number
  connection_count: number
  created_at: string
}

export interface GroupMember {
  user_id: number
  username: string
  role: string
}

export interface GroupConnection {
  conn_id: number
  name: string
  driver: string
  host: string
  port: number
  environment: string
  permissions: string[]
}

export interface UserConnectionAssignment {
  conn_id: number
  name: string
  driver: string
  host: string
  port: number
  environment: string
  source: string
  permissions: string[]
}

export function usePermissions() {
  const roles = ref<Role[]>([])
  const permissions = ref<Permission[]>([])
  const accessGroups = ref<AccessGroup[]>([])
  const loading = ref(false)

  // ── Roles ──

  async function fetchRoles() {
    loading.value = true
    try {
      const { data } = await axios.get<Role[]>('/api/roles')
      roles.value = data || []
      return data
    } catch (error) {
      console.error('Failed to fetch roles:', error)
      throw error
    } finally {
      loading.value = false
    }
  }

  async function fetchPermissions() {
    try {
      const { data } = await axios.get<Permission[]>('/api/app-permissions')
      permissions.value = data || []
      return data
    } catch (error) {
      console.error('Failed to fetch permissions:', error)
      throw error
    }
  }

  async function createRole(payload: { name: string; description: string; permissions: string[] }) {
    const { data } = await axios.post('/api/roles', payload)
    await fetchRoles()
    return data
  }

  async function updateRole(id: number, payload: { name: string; description: string; permissions: string[] }) {
    await axios.put(`/api/roles/${id}`, payload)
    await fetchRoles()
  }

  async function deleteRole(id: number) {
    await axios.delete(`/api/roles/${id}`)
    await fetchRoles()
  }

  // ── Access Groups ──

  async function fetchAccessGroups() {
    loading.value = true
    try {
      const { data } = await axios.get<AccessGroup[]>('/api/folders')
      accessGroups.value = data || []
      return data
    } catch (error) {
      console.error('Failed to fetch access groups:', error)
      throw error
    } finally {
      loading.value = false
    }
  }

  async function createAccessGroup(payload: {
    name: string
    role_restrict?: string
    color?: string
    user_ids?: number[]
    connection_ids?: number[]
    connection_permissions?: Array<{ conn_id: number; permissions: string[] }>
  }) {
    const { data } = await axios.post('/api/folders', {
      name: payload.name,
      visibility: 'shared',
      color: payload.color || '#3B82F6',
      role_restrict: payload.role_restrict || '',
      is_active: true,
    })
    await fetchAccessGroups()
    return data
  }

  async function updateAccessGroup(id: number, payload: {
    name: string
    role_restrict?: string
  }) {
    await axios.put(`/api/folders/${id}`, payload)
    await fetchAccessGroups()
  }

  async function deleteAccessGroup(id: number) {
    await axios.delete(`/api/folders/${id}`)
    await fetchAccessGroups()
  }

  // ── User Assignments ──

  async function fetchUserConnections(userId: number) {
    const { data } = await axios.get<UserConnectionAssignment[]>(`/api/users/${userId}/connections`)
    return data
  }

  async function assignUserConnections(userId: number, payload: {
    connection_ids: number[]
    connection_permissions: Array<{ conn_id: number; permissions: string[] }>
  }) {
    await axios.post(`/api/users/${userId}/connections`, payload)
  }

  return {
    // State
    roles,
    permissions,
    accessGroups,
    loading,

    // Roles
    fetchRoles,
    fetchPermissions,
    createRole,
    updateRole,
    deleteRole,

    // Access Groups
    fetchAccessGroups,
    createAccessGroup,
    updateAccessGroup,
    deleteAccessGroup,

    // User Assignments
    fetchUserConnections,
    assignUserConnections,
  }
}
