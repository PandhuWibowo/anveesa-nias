<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'

interface Role { id: number; name: string }
interface User { id: number; username: string }
interface Group { id: number; name: string; is_active: boolean }
interface Connection { id: number; name: string; driver: string; environment: string }
interface StepApprover { approver_type: 'role' | 'user'; approver_id: number; approver_name?: string }
interface WorkflowStep {
  id?: number
  step_order: number
  name: string
  required_approvals: number
  approvers: StepApprover[]
}
interface Workflow {
  id: number
  name: string
  description: string
  is_active: boolean
  assign_all_groups: boolean
  assign_all_connections: boolean
  access_groups: Array<{ group_id: number; group_name: string }>
  connections: Array<{ conn_id: number; name: string; environment: string }>
  steps: WorkflowStep[]
}

const toast = useToast()

const loading = ref(false)
const saving = ref(false)
const workflows = ref<Workflow[]>([])
const roles = ref<Role[]>([])
const users = ref<User[]>([])
const groups = ref<Group[]>([])
const connections = ref<Connection[]>([])
const showModal = ref(false)
const editingId = ref<number | null>(null)

const form = reactive({
  name: '',
  description: '',
  assign_all_groups: false,
  access_group_ids: [] as number[],
  assign_all_connections: false,
  connection_ids: [] as number[],
  steps: [] as WorkflowStep[],
})

const approverOptions = computed(() => [
  ...roles.value.map(role => ({ value: `role:${role.id}`, label: `Role: ${role.name}` })),
  ...users.value.map(user => ({ value: `user:${user.id}`, label: `User: ${user.username}` })),
])

function selectedApprovers(step: WorkflowStep) {
  return step.approvers.map(approver => `${approver.approver_type}:${approver.approver_id}`)
}

function setSelectedApprovers(step: WorkflowStep, values: string[]) {
  step.approvers = values.map(value => {
    const [type, id] = value.split(':')
    return { approver_type: type as 'role' | 'user', approver_id: Number(id) }
  })
}

function approverLabel(approver: StepApprover) {
  if (approver.approver_type === 'role') {
    return roles.value.find(role => role.id === approver.approver_id)?.name || `Role #${approver.approver_id}`
  }
  return users.value.find(user => user.id === approver.approver_id)?.username || `User #${approver.approver_id}`
}

function emptyStep(order: number): WorkflowStep {
  return { step_order: order, name: `Step ${order}`, required_approvals: 1, approvers: [] }
}

function resetForm() {
  editingId.value = null
  form.name = ''
  form.description = ''
  form.assign_all_groups = false
  form.access_group_ids = []
  form.assign_all_connections = false
  form.connection_ids = []
  form.steps = [emptyStep(1)]
}

function openCreate() {
  resetForm()
  showModal.value = true
}

function openEdit(workflow: Workflow) {
  editingId.value = workflow.id
  form.name = workflow.name
  form.description = workflow.description
  form.assign_all_groups = workflow.assign_all_groups
  form.access_group_ids = (workflow.access_groups ?? []).map(group => group.group_id)
  form.assign_all_connections = workflow.assign_all_connections
  form.connection_ids = (workflow.connections ?? []).map(connection => connection.conn_id)
  form.steps = (workflow.steps ?? []).map(step => ({
    step_order: step.step_order,
    name: step.name,
    required_approvals: step.required_approvals,
    approvers: step.approvers.map(approver => ({
      approver_type: approver.approver_type,
      approver_id: approver.approver_id,
    })),
  }))
  if (form.steps.length === 0) {
    form.steps = [emptyStep(1)]
  }
  showModal.value = true
}

function addStep() {
  form.steps.push(emptyStep(form.steps.length + 1))
}

function removeStep(index: number) {
  if (form.steps.length === 1) return
  form.steps.splice(index, 1)
  form.steps.forEach((step, idx) => { step.step_order = idx + 1 })
}

async function fetchData() {
  loading.value = true
  try {
    const results = await Promise.allSettled([
      axios.get<Workflow[]>('/api/workflows'),
      axios.get<Role[]>('/api/roles'),
      axios.get<User[]>('/api/admin/users'),
      axios.get<Group[]>('/api/folders'),
      axios.get<Connection[]>('/api/connections'),
    ])

    const errors: string[] = []
    const [workflowRes, roleRes, userRes, groupRes, connectionRes] = results

    if (workflowRes.status === 'fulfilled') {
      workflows.value = workflowRes.value.data || []
    } else {
      workflows.value = []
      errors.push(workflowRes.reason?.response?.data?.error || 'workflows')
    }

    if (roleRes.status === 'fulfilled') {
      roles.value = roleRes.value.data || []
    } else {
      roles.value = []
      errors.push(roleRes.reason?.response?.data?.error || 'roles')
    }

    if (userRes.status === 'fulfilled') {
      users.value = userRes.value.data || []
    } else {
      users.value = []
      errors.push(userRes.reason?.response?.data?.error || 'users')
    }

    if (groupRes.status === 'fulfilled') {
      groups.value = groupRes.value.data || []
    } else {
      groups.value = []
      errors.push(groupRes.reason?.response?.data?.error || 'groups')
    }

    if (connectionRes.status === 'fulfilled') {
      connections.value = connectionRes.value.data || []
    } else {
      connections.value = []
      errors.push(connectionRes.reason?.response?.data?.error || 'connections')
    }

    if (errors.length > 0) {
      toast.error(`Workflow page loaded partially. Failed: ${errors.join(', ')}`)
    }
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load workflows')
  } finally {
    loading.value = false
  }
}

async function saveWorkflow() {
  if (!form.name.trim()) {
    toast.error('Workflow name is required')
    return
  }
  if (!form.assign_all_groups && form.access_group_ids.length === 0) {
    toast.error('Select at least one access group')
    return
  }
  if (!form.assign_all_connections && form.connection_ids.length === 0) {
    toast.error('Select at least one connection')
    return
  }
  if (form.steps.some(step => !step.name.trim() || step.approvers.length === 0)) {
    toast.error('Each step needs a name and at least one approver')
    return
  }

  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      description: form.description.trim(),
      assign_all_groups: form.assign_all_groups,
      access_group_ids: form.assign_all_groups ? [] : form.access_group_ids,
      assign_all_connections: form.assign_all_connections,
      connection_ids: form.assign_all_connections ? [] : form.connection_ids,
      steps: form.steps.map((step, idx) => ({
        name: step.name.trim(),
        required_approvals: Math.max(1, step.required_approvals),
        approvers: step.approvers,
        step_order: idx + 1,
      })),
    }
    if (editingId.value) {
      await axios.put(`/api/workflows/${editingId.value}`, payload)
      toast.success('Workflow updated')
    } else {
      await axios.post('/api/workflows', payload)
      toast.success('Workflow created')
    }
    showModal.value = false
    await fetchData()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to save workflow')
  } finally {
    saving.value = false
  }
}

async function toggleWorkflow(workflow: Workflow) {
  try {
    await axios.put(`/api/workflows/${workflow.id}/active`, { is_active: !workflow.is_active })
    await fetchData()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to update workflow')
  }
}

async function removeWorkflow(workflow: Workflow) {
  if (!confirm(`Delete workflow "${workflow.name}"?`)) return
  try {
    await axios.delete(`/api/workflows/${workflow.id}`)
    toast.success('Workflow deleted')
    await fetchData()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to delete workflow')
  }
}

onMounted(() => {
  resetForm()
  fetchData()
})
</script>

<template>
  <div class="page-shell wf-root">
    <div class="page-scroll wf-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Approval Studio</div>
            <div class="page-title">Approval Workflows</div>
            <div class="page-subtitle">Configure the review lanes, scope, and approver chain that control write access across connections.</div>
          </div>
          <div class="page-hero__actions wf-header-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="fetchData">Refresh</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">New Workflow</button>
          </div>
        </section>

        <section class="page-panel wf-table-wrap">
          <div class="wf-panel-head">
            <div>
              <div class="wf-panel-title">Workflow Library</div>
              <div class="wf-panel-sub">{{ workflows.length }} configured workflows</div>
            </div>
          </div>

          <div v-if="loading" class="wf-loading">
            <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          </div>

          <div v-else class="wf-table-wrap__inner">
            <table class="wf-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Status</th>
                  <th>Scope</th>
                  <th>Steps</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="workflow in workflows" :key="workflow.id">
                  <td class="wf-td-main">
                    <div class="wf-name">{{ workflow.name }}</div>
                    <div class="wf-desc">{{ workflow.description || 'No description' }}</div>
                  </td>
                  <td>
                    <span class="wf-badge" :class="{ 'wf-badge--inactive': !workflow.is_active }">{{ workflow.is_active ? 'Active' : 'Inactive' }}</span>
                  </td>
                  <td class="wf-td-dim">
                    <div>{{ workflow.assign_all_groups ? 'All groups' : `${workflow.access_groups.length} groups` }}</div>
                    <div>{{ workflow.assign_all_connections ? 'All connections' : `${workflow.connections.length} connections` }}</div>
                  </td>
                  <td>
                    <div class="wf-steps">
                      <div v-for="step in workflow.steps" :key="`${workflow.id}-${step.step_order}`" class="wf-step">
                        <strong>{{ step.step_order }}. {{ step.name }}</strong>
                        <span>{{ step.required_approvals }} needed • {{ step.approvers.map(approver => approver.approver_name || approverLabel(approver)).join(', ') }}</span>
                      </div>
                    </div>
                  </td>
                  <td>
                    <div class="wf-actions">
                      <button class="base-btn base-btn--ghost base-btn--xs" @click="toggleWorkflow(workflow)">{{ workflow.is_active ? 'Disable' : 'Enable' }}</button>
                      <button class="base-btn base-btn--ghost base-btn--xs" @click="openEdit(workflow)">Edit</button>
                      <button class="base-btn base-btn--ghost base-btn--xs wf-btn-del" @click="removeWorkflow(workflow)">Delete</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="workflows.length === 0">
                  <td colspan="5" class="wf-empty-row">No workflows yet</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="showModal" class="wf-overlay" @click.self="showModal = false">
        <div class="wf-dialog">
          <div class="wf-dialog__head">
            <div class="wf-dialog__title">{{ editingId ? 'Edit Workflow' : 'New Workflow' }}</div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showModal = false">×</button>
          </div>
          <div class="wf-dialog__body">
            <label class="wf-label">Name</label>
            <input v-model="form.name" class="base-input" placeholder="Production approval chain" />

            <label class="wf-label">Description</label>
            <input v-model="form.description" class="base-input" placeholder="When this workflow should be used" />

            <div class="wf-toggle">
              <label><input v-model="form.assign_all_groups" type="checkbox" /> All groups</label>
              <label><input v-model="form.assign_all_connections" type="checkbox" /> All connections</label>
            </div>

            <template v-if="!form.assign_all_groups">
              <label class="wf-label">Access Groups</label>
              <select v-model="form.access_group_ids" class="wf-select" multiple>
                <option v-for="group in groups.filter(g => g.is_active)" :key="group.id" :value="group.id">{{ group.name }}</option>
              </select>
            </template>

            <template v-if="!form.assign_all_connections">
              <label class="wf-label">Connections</label>
              <select v-model="form.connection_ids" class="wf-select" multiple>
                <option v-for="connection in connections" :key="connection.id" :value="connection.id">{{ connection.name }} ({{ connection.environment }})</option>
              </select>
            </template>

            <div class="wf-section-head">
              <span>Steps</span>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="addStep">Add Step</button>
            </div>

            <div v-for="(step, index) in form.steps" :key="index" class="wf-step-card">
              <div class="wf-step-row">
                <input v-model="step.name" class="base-input" :placeholder="`Step ${index + 1}`" />
                <input v-model.number="step.required_approvals" class="base-input wf-step-count" type="number" min="1" />
                <button class="base-btn base-btn--ghost base-btn--xs" :disabled="form.steps.length === 1" @click="removeStep(index)">Remove</button>
              </div>
              <select
                :value="selectedApprovers(step)"
                class="wf-select"
                multiple
                @change="setSelectedApprovers(step, Array.from(($event.target as HTMLSelectElement).selectedOptions).map(option => option.value))"
              >
                <option v-for="option in approverOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </div>
          </div>
          <div class="wf-dialog__foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showModal = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="saveWorkflow">{{ saving ? 'Saving…' : 'Save Workflow' }}</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.wf-root { background: var(--bg-body); }
.wf-header-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.wf-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 20px 10px;
}
.wf-panel-title { font-size: 15px; font-weight: 700; color: var(--text-primary); }
.wf-panel-sub { margin-top: 4px; font-size: 12px; color: var(--text-muted); }
.wf-loading { display: flex; align-items: center; justify-content: center; padding: 40px; color: var(--text-muted); }
.wf-table-wrap { overflow: hidden; }
.wf-table-wrap__inner { overflow: auto; }
.wf-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.wf-table th { background: rgba(255, 255, 255, 0.02); padding: 11px 18px; border-bottom: 1px solid var(--border); font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.12em; color: var(--text-muted); text-align: left; }
.wf-table td { padding: 14px 18px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: top; }
.wf-table tr:last-child td { border-bottom: none; }
.wf-table tr:hover td { background: rgba(255, 255, 255, 0.03); }
.wf-td-main { min-width: 220px; }
.wf-name { font-weight: 600; }
.wf-desc, .wf-td-dim { color: var(--text-muted); font-size: 12px; margin-top: 4px; }
.wf-badge { display: inline-flex; font-size: 11px; font-weight: 700; text-transform: uppercase; padding: 4px 10px; border-radius: 999px; border: 1px solid rgba(92, 184, 165, 0.32); letter-spacing: 0.12em; color: #79d9c3; background: rgba(92, 184, 165, 0.12); }
.wf-badge--inactive { color: #f4a6a6; border-color: rgba(232, 128, 128, 0.25); background: rgba(232, 128, 128, 0.12); }
.wf-steps { display: flex; flex-direction: column; gap: 8px; }
.wf-step { display: flex; flex-direction: column; gap: 3px; font-size: 12px; color: var(--text-muted); padding-left: 12px; border-left: 2px solid rgba(92, 184, 165, 0.22); }
.wf-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.wf-btn-del { color: var(--danger) !important; }
.wf-empty-row { text-align: center; color: var(--text-muted); font-size: 13px; padding: 24px; }

.wf-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.wf-dialog { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 8px; width: min(760px, 92vw); max-height: 90vh; overflow: auto; box-shadow: 0 20px 60px rgba(0,0,0,0.5); }
.wf-dialog__head, .wf-dialog__foot { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.wf-dialog__foot { border-bottom: none; border-top: 1px solid var(--border); justify-content: flex-end; gap: 8px; }
.wf-dialog__title { font-size: 15px; color: var(--text-primary); font-weight: 600; }
.wf-dialog__body { padding: 20px; display: flex; flex-direction: column; gap: 10px; }
.wf-label { display: block; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); margin-bottom: 4px; }
.wf-toggle { display: flex; gap: 18px; flex-wrap: wrap; padding: 6px 0; font-size: 13px; color: var(--text-primary); }
.wf-select { width: 100%; min-height: 110px; padding: 8px 10px; background: var(--bg-body); border: 1px solid var(--border); border-radius: 5px; color: var(--text-primary); font-size: 13px; font-family: inherit; box-sizing: border-box; outline: none; }
.wf-select:focus { border-color: var(--brand); }
.wf-section-head { display: flex; align-items: center; justify-content: space-between; margin-top: 8px; font-size: 13px; font-weight: 600; color: var(--text-primary); }
.wf-step-card { border: 1px solid var(--border); border-radius: 8px; padding: 12px; background: var(--bg-body); display: flex; flex-direction: column; gap: 8px; }
.wf-step-row { display: grid; grid-template-columns: 1fr 90px auto; gap: 8px; }
.wf-step-count { text-align: center; }

@media (max-width: 720px) {
  .wf-step-row { grid-template-columns: 1fr; }
}
</style>
