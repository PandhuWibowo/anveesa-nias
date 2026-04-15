<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { NButton, NCard, NTable, NTag, NSwitch, NModal, NForm, NFormItem, NInput, NCheckboxGroup, NCheckbox, NSpace, NSpin } from 'naive-ui'
import { useToast } from '@/composables/useToast'

const toast = useToast()

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
const loading = ref(false)
const showModal = ref(false)
const editingRole = ref<Role | null>(null)

const formData = ref({
  name: '',
  description: '',
  permissions: [] as string[],
})

onMounted(async () => {
  await Promise.all([fetchRoles(), fetchPermissions()])
})

async function fetchRoles() {
  loading.value = true
  try {
    const { data } = await axios.get<Role[]>('/api/roles')
    roles.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data?.message || 'Failed to load roles')
  } finally {
    loading.value = false
  }
}

async function fetchPermissions() {
  try {
    const { data } = await axios.get<Permission[]>('/api/app-permissions')
    permissions.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data?.message || 'Failed to load permissions')
  }
}

function openCreateModal() {
  editingRole.value = null
  formData.value = {
    name: '',
    description: '',
    permissions: [],
  }
  showModal.value = true
}

function openEditModal(role: Role) {
  editingRole.value = role
  try {
    const perms = JSON.parse(role.permissions)
    formData.value = {
      name: role.name,
      description: role.description,
      permissions: Array.isArray(perms) ? perms : [],
    }
  } catch {
    formData.value = {
      name: role.name,
      description: role.description,
      permissions: [],
    }
  }
  showModal.value = true
}

async function saveRole() {
  if (!formData.value.name) {
    toast.error('Role name is required')
    return
  }

  try {
    if (editingRole.value) {
      await axios.put(`/api/roles/${editingRole.value.id}`, formData.value)
      toast.success('Role updated successfully')
    } else {
      await axios.post('/api/roles', formData.value)
      toast.success('Role created successfully')
    }
    showModal.value = false
    fetchRoles()
  } catch (error: any) {
    toast.error(error.response?.data?.message || 'Failed to save role')
  }
}

async function deleteRole(role: Role) {
  if (!confirm(`Delete role "${role.name}"?`)) return
  
  try {
    await axios.delete(`/api/roles/${role.id}`)
    toast.success('Role deleted successfully')
    fetchRoles()
  } catch (error: any) {
    toast.error(error.response?.data?.message || 'Failed to delete role')
  }
}

// Group permissions by category
const groupedPermissions = ref<Record<string, Permission[]>>({})
$: {
  const grouped: Record<string, Permission[]> = {}
  permissions.value.forEach(p => {
    if (!grouped[p.group]) grouped[p.group] = []
    grouped[p.group].push(p)
  })
  groupedPermissions.value = grouped
}
</script>

<template>
  <div class="rbac-view">
    <div class="rbac-header">
      <div>
        <h1 class="rbac-title">Roles & Permissions</h1>
        <p class="rbac-subtitle">Manage user roles and access control</p>
      </div>
      <NButton type="primary" @click="openCreateModal">
        <template #icon>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
        </template>
        Create Role
      </NButton>
    </div>

    <NSpin :show="loading">
      <NCard v-for="role in roles" :key="role.id" class="role-card">
        <div class="role-card-header">
          <div class="role-info">
            <div class="role-name">
              {{ role.name }}
              <NTag v-if="role.is_system" size="small" type="info">System</NTag>
            </div>
            <div class="role-description">{{ role.description }}</div>
            <div class="role-meta">
              <span>{{ role.user_count }} users</span>
              <span>·</span>
              <span>{{ JSON.parse(role.permissions || '[]').length }} permissions</span>
            </div>
          </div>
          <div class="role-actions">
            <NButton v-if="!role.is_system" size="small" @click="openEditModal(role)">Edit</NButton>
            <NButton v-if="!role.is_system" size="small" type="error" @click="deleteRole(role)">Delete</NButton>
          </div>
        </div>
      </NCard>
    </NSpin>

    <NModal v-model:show="showModal" preset="card" :title="editingRole ? 'Edit Role' : 'Create Role'" style="width: 600px">
      <NForm>
        <NFormItem label="Role Name">
          <NInput v-model:value="formData.name" placeholder="e.g. Developer, DBA" />
        </NFormItem>
        <NFormItem label="Description">
          <NInput v-model:value="formData.description" placeholder="Brief description of this role" type="textarea" />
        </NFormItem>
        <NFormItem label="Permissions">
          <div class="permissions-grid">
            <div v-for="(perms, group) in groupedPermissions" :key="group" class="permission-group">
              <div class="permission-group-title">{{ group }}</div>
              <NCheckboxGroup v-model:value="formData.permissions">
                <NSpace vertical>
                  <NCheckbox v-for="perm in perms" :key="perm.key" :value="perm.key">
                    {{ perm.label }}
                  </NCheckbox>
                </NSpace>
              </NCheckboxGroup>
            </div>
          </div>
        </NFormItem>
      </NForm>
      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 8px">
          <NButton @click="showModal = false">Cancel</NButton>
          <NButton type="primary" @click="saveRole">{{ editingRole ? 'Update' : 'Create' }}</NButton>
        </div>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.rbac-view {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}

.rbac-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
}

.rbac-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 4px 0;
}

.rbac-subtitle {
  font-size: 14px;
  color: var(--text-muted);
  margin: 0;
}

.role-card {
  margin-bottom: 16px;
}

.role-card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.role-info {
  flex: 1;
}

.role-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 4px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.role-description {
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}

.role-meta {
  font-size: 12px;
  color: var(--text-muted);
  display: flex;
  gap: 8px;
}

.role-actions {
  display: flex;
  gap: 8px;
}

.permissions-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
  max-height: 400px;
  overflow-y: auto;
  padding: 12px;
  background: var(--bg-surface);
  border-radius: 8px;
}

.permission-group {
  padding: 12px;
  background: var(--bg-elevated);
  border-radius: 6px;
}

.permission-group-title {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 12px;
}
</style>
