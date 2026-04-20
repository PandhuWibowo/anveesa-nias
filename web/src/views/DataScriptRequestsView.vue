<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'

interface DataPlanSummary {
  updates: number
  inserts: number
  deletes: number
}

interface DataPlanRisk {
  level: string
  flags: string[]
}

interface DataChangePlanItem {
  id: number
  seq_no: number
  op_type: 'insert' | 'update' | 'delete'
  table_name: string
  pk: Record<string, unknown>
  before: Record<string, unknown>
  after: Record<string, unknown>
}

interface DataChangePlan {
  id: number
  script_id: number
  script_name: string
  script_language: string
  script_version_id: number
  script_version_no: number
  conn_id: number
  connection: string
  database_name: string
  status: string
  summary: DataPlanSummary
  risk: DataPlanRisk
  creator_name: string
  reviewer_name?: string
  review_note: string
  execute_error: string
  created_at: string
  items?: DataChangePlanItem[]
}

interface WorkflowOption {
  id: number
  name: string
  description: string
}

const toast = useToast()
const router = useRouter()
const { hasPermission, isAdmin } = useAuth()

const loading = ref(false)
const detailLoading = ref(false)
const plans = ref<DataChangePlan[]>([])
const selectedPlanId = ref<number | null>(null)
const selectedPlan = ref<DataChangePlan | null>(null)
const filter = ref<'all' | 'draft' | 'pending_review' | 'approved' | 'rejected' | 'done' | 'failed'>('all')
const reviewNote = ref('')
const workflowsLoading = ref(false)
const applicableWorkflows = ref<WorkflowOption[]>([])
const selectedWorkflowId = ref<number | null>(null)
const submittingPlan = ref(false)
const reviewing = ref(false)
const executing = ref(false)

const filteredPlans = computed(() =>
  filter.value === 'all' ? plans.value : plans.value.filter((plan) => plan.status === filter.value),
)

const canApprove = computed(() => isAdmin.value || hasPermission('query.approve'))
const canExecute = computed(() => isAdmin.value || hasPermission('query.execute'))

async function fetchPlans() {
  loading.value = true
  try {
    const { data } = await axios.get<DataChangePlan[]>('/api/data-change-plans')
    plans.value = Array.isArray(data) ? data : []
    if (!selectedPlanId.value || !plans.value.some((plan) => plan.id === selectedPlanId.value)) {
      selectedPlanId.value = filteredPlans.value[0]?.id ?? plans.value[0]?.id ?? null
    }
    if (selectedPlanId.value) {
      await loadPlan(selectedPlanId.value)
    }
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load data script requests')
  } finally {
    loading.value = false
  }
}

async function loadPlan(planId: number) {
  selectedPlanId.value = planId
  detailLoading.value = true
  try {
    const { data } = await axios.get<DataChangePlan>(`/api/data-change-plans/${planId}`)
    selectedPlan.value = data
    await loadApplicableWorkflows(data.conn_id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load request detail')
  } finally {
    detailLoading.value = false
  }
}

async function loadApplicableWorkflows(connId?: number | null) {
  applicableWorkflows.value = []
  selectedWorkflowId.value = null
  if (!connId) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<WorkflowOption[]>('/api/workflows/applicable', {
      params: { conn_id: connId },
    })
    applicableWorkflows.value = Array.isArray(data) ? data : []
    selectedWorkflowId.value = applicableWorkflows.value[0]?.id ?? null
  } catch {
    applicableWorkflows.value = []
  } finally {
    workflowsLoading.value = false
  }
}

async function submitPlan() {
  if (!selectedPlan.value) return
  submittingPlan.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-change-plans/${selectedPlan.value.id}/submit`, {
      workflow_id: selectedWorkflowId.value || 0,
    })
    toast.success(`Plan #${data.id} submitted for review`)
    await fetchPlans()
    await loadPlan(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to submit plan')
  } finally {
    submittingPlan.value = false
  }
}

async function reviewPlan(action: 'approved' | 'rejected') {
  if (!selectedPlan.value) return
  reviewing.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-change-plans/${selectedPlan.value.id}/review`, {
      action,
      note: reviewNote.value,
    })
    toast.success(action === 'approved' ? 'Plan approved' : 'Plan rejected')
    reviewNote.value = ''
    await fetchPlans()
    await loadPlan(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to review plan')
  } finally {
    reviewing.value = false
  }
}

async function executePlan() {
  if (!selectedPlan.value) return
  executing.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-change-plans/${selectedPlan.value.id}/execute`)
    toast.success(`Plan #${data.id} executed`)
    await fetchPlans()
    await loadPlan(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to execute plan')
  } finally {
    executing.value = false
  }
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function formatJson(value?: Record<string, unknown>) {
  return JSON.stringify(value ?? {}, null, 2)
}

function hasFields(value?: Record<string, unknown>) {
  return !!value && Object.keys(value).length > 0
}

function openScript(scriptId: number) {
  router.push({ name: 'data-scripts', query: { script: String(scriptId) } })
}

watch(filteredPlans, async (items) => {
  if (!items.length) {
    selectedPlanId.value = null
    selectedPlan.value = null
    return
  }
  if (!selectedPlanId.value || !items.some((plan) => plan.id === selectedPlanId.value)) {
    await loadPlan(items[0].id)
  }
})

onMounted(async () => {
  await fetchPlans()
})
</script>

<template>
  <div class="page-shell dsr-view">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Change</div>
            <div class="page-title">Data Script Requests</div>
            <div class="page-subtitle">This page shows preview-created requests only. Saved scripts or saved versions do not appear here until Preview Plan creates a plan.</div>
          </div>
        </section>

        <section class="page-card dsr-card">
          <div class="dsr-toolbar">
            <select v-model="filter" class="base-input dsr-filter">
              <option value="all">All statuses</option>
              <option value="draft">Draft</option>
              <option value="pending_review">Pending review</option>
              <option value="approved">Approved</option>
              <option value="rejected">Rejected</option>
              <option value="done">Done</option>
              <option value="failed">Failed</option>
            </select>
            <div class="dsr-count">{{ filteredPlans.length }} plans</div>
          </div>

          <div class="dsr-layout">
            <div class="dsr-list">
              <div v-if="loading" class="dsr-empty">Loading requests…</div>
              <div v-else-if="!filteredPlans.length" class="dsr-empty">
                No data script requests match this filter. If you only saved a script, open `Data Scripts`, select a connection, and click `Preview Plan` to create a request.
              </div>
              <button
                v-for="plan in filteredPlans"
                :key="plan.id"
                class="dsr-item"
                :class="{ 'dsr-item--active': plan.id === selectedPlanId }"
                @click="loadPlan(plan.id)"
              >
                <div class="dsr-item__top">
                  <strong>#{{ plan.id }}</strong>
                  <span class="dsr-status" :data-status="plan.status">{{ plan.status }}</span>
                </div>
                <div class="dsr-item__script">{{ plan.script_name || `Script #${plan.script_id}` }}</div>
                <div class="dsr-item__meta">v{{ plan.script_version_no }} · {{ plan.connection || 'No connection' }}</div>
                <div class="dsr-item__meta">{{ plan.creator_name || 'unknown' }} · {{ formatDate(plan.created_at) }}</div>
              </button>
            </div>

            <div class="dsr-detail">
              <div v-if="detailLoading" class="dsr-empty">Loading request detail…</div>
              <div v-else-if="!selectedPlan" class="dsr-empty">Select a request to inspect it.</div>
              <div v-else class="dsr-detail-card">
                <div class="dsr-detail__head">
                  <div>
                    <div class="dsr-detail__title">{{ selectedPlan.script_name || `Script #${selectedPlan.script_id}` }}</div>
                    <div class="dsr-detail__sub">Plan #{{ selectedPlan.id }} · v{{ selectedPlan.script_version_no }} · {{ selectedPlan.connection }} · {{ selectedPlan.database_name || 'default database/schema' }}</div>
                  </div>
                  <div class="dsr-detail__actions">
                    <button class="base-btn base-btn--ghost base-btn--sm" @click="openScript(selectedPlan.script_id)">Open Script</button>
                    <span class="dsr-status" :data-status="selectedPlan.status">{{ selectedPlan.status }}</span>
                  </div>
                </div>

                <div class="dsr-summary">
                  <div class="dsr-stat"><strong>{{ selectedPlan.summary.updates }}</strong><span>Updates</span></div>
                  <div class="dsr-stat"><strong>{{ selectedPlan.summary.inserts }}</strong><span>Inserts</span></div>
                  <div class="dsr-stat"><strong>{{ selectedPlan.summary.deletes }}</strong><span>Deletes</span></div>
                  <div class="dsr-stat"><strong>{{ selectedPlan.risk.level }}</strong><span>Risk</span></div>
                </div>

                <div v-if="selectedPlan.risk.flags?.length" class="dsr-flags">
                  <span v-for="flag in selectedPlan.risk.flags" :key="flag" class="dsr-flag">{{ flag }}</span>
                </div>

                <div class="dsr-meta">
                  <span>Created by {{ selectedPlan.creator_name || 'unknown' }}</span>
                  <span>Reviewer {{ selectedPlan.reviewer_name || '—' }}</span>
                  <span>{{ formatDate(selectedPlan.created_at) }}</span>
                </div>

                <div class="dsr-review-box">
                  <div class="dsr-workflow">
                    <div class="dsr-workflow__head">
                      <strong>Approval Workflow</strong>
                      <span v-if="workflowsLoading">Loading…</span>
                    </div>
                    <select
                      v-model="selectedWorkflowId"
                      class="base-input"
                      :disabled="workflowsLoading || !applicableWorkflows.length || selectedPlan.status !== 'draft' && selectedPlan.status !== 'rejected'"
                    >
                      <option :value="null">Select workflow…</option>
                      <option v-for="workflow in applicableWorkflows" :key="workflow.id" :value="workflow.id">{{ workflow.name }}</option>
                    </select>
                    <div v-if="!workflowsLoading && !applicableWorkflows.length" class="dsr-workflow__empty">
                      No applicable workflow matches this connection.
                    </div>
                  </div>

                  <textarea v-model="reviewNote" class="base-input dsr-note" rows="3" placeholder="Review note or rejection reason…" />

                  <div class="dsr-actions">
                    <button class="base-btn base-btn--ghost base-btn--sm" :disabled="submittingPlan || !selectedWorkflowId || selectedPlan.status !== 'draft' && selectedPlan.status !== 'rejected'" @click="submitPlan">
                      {{ submittingPlan ? 'Submitting…' : 'Submit Draft' }}
                    </button>
                    <button v-if="canApprove" class="base-btn base-btn--ghost base-btn--sm" :disabled="reviewing || selectedPlan.status !== 'pending_review'" @click="reviewPlan('rejected')">
                      Reject
                    </button>
                    <button v-if="canApprove" class="base-btn base-btn--primary base-btn--sm" :disabled="reviewing || selectedPlan.status !== 'pending_review'" @click="reviewPlan('approved')">
                      Approve
                    </button>
                    <button v-if="canExecute" class="base-btn base-btn--primary base-btn--sm" :disabled="executing || selectedPlan.status !== 'approved'" @click="executePlan">
                      {{ executing ? 'Executing…' : 'Execute' }}
                    </button>
                  </div>
                </div>

                <div v-if="selectedPlan.review_note" class="dsr-message"><strong>Review note:</strong> {{ selectedPlan.review_note }}</div>
                <div v-if="selectedPlan.execute_error" class="dsr-error"><strong>Execution error:</strong> {{ selectedPlan.execute_error }}</div>

                <div class="dsr-items">
                  <div class="dsr-items__head">Planned Operations</div>
                  <div v-for="item in selectedPlan.items" :key="item.id" class="dsr-item-row">
                    <div class="dsr-item-row__top">{{ item.seq_no }}. {{ item.op_type.toUpperCase() }} {{ item.table_name }}</div>
                    <div class="dsr-item-grid">
                      <code>PK: {{ formatJson(item.pk) }}</code>
                      <code v-if="hasFields(item.before)">Before: {{ formatJson(item.before) }}</code>
                      <code v-if="hasFields(item.after)">After: {{ formatJson(item.after) }}</code>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dsr-card { padding: 20px; }
.dsr-toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:16px; }
.dsr-filter { max-width: 220px; }
.dsr-count { font-size:12px; color:var(--text-muted); }
.dsr-layout { display:grid; grid-template-columns: 320px minmax(0, 1fr); gap:16px; min-width:0; }
.dsr-list { display:flex; flex-direction:column; gap:10px; min-width:0; }
.dsr-item { width:100%; text-align:left; border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); padding:12px 14px; cursor:pointer; }
.dsr-item--active { border-color: var(--brand); box-shadow: inset 0 0 0 1px var(--brand); }
.dsr-item__top, .dsr-detail__head, .dsr-workflow__head { display:flex; align-items:flex-start; justify-content:space-between; gap:10px; }
.dsr-item__script, .dsr-detail__title, .dsr-items__head { font-size:13px; font-weight:700; color:var(--text-primary); }
.dsr-item__meta, .dsr-meta, .dsr-detail__sub { font-size:11px; color:var(--text-muted); display:flex; gap:8px; flex-wrap:wrap; }
.dsr-status { display:inline-flex; align-items:center; padding:3px 8px; border-radius:999px; font-size:11px; font-weight:700; text-transform:capitalize; background:var(--bg-surface); color:var(--text-secondary); }
.dsr-status[data-status="approved"], .dsr-status[data-status="done"] { background: rgba(34,197,94,.15); color:#16a34a; }
.dsr-status[data-status="pending_review"], .dsr-status[data-status="executing"] { background: rgba(245,158,11,.16); color:#d97706; }
.dsr-status[data-status="rejected"], .dsr-status[data-status="failed"] { background: rgba(239,68,68,.14); color:#dc2626; }
.dsr-detail-card { border:1px solid var(--border); border-radius:18px; background:var(--bg-elevated); padding:18px; display:flex; flex-direction:column; gap:14px; min-width:0; }
.dsr-detail__actions, .dsr-actions, .dsr-flags, .dsr-summary { display:flex; gap:10px; flex-wrap:wrap; }
.dsr-stat { min-width:92px; padding:12px 14px; border:1px solid var(--border); border-radius:14px; background:var(--bg-surface); display:flex; flex-direction:column; gap:4px; }
.dsr-stat strong { font-size:18px; color:var(--text-primary); text-transform:capitalize; }
.dsr-stat span { font-size:11px; color:var(--text-muted); text-transform:uppercase; letter-spacing:.04em; }
.dsr-flag { display:inline-flex; padding:4px 8px; border-radius:999px; background:rgba(59,130,246,.12); color:#2563eb; font-size:11px; font-weight:700; }
.dsr-meta { padding-bottom:4px; border-bottom:1px solid var(--border); }
.dsr-review-box, .dsr-workflow { display:flex; flex-direction:column; gap:10px; }
.dsr-review-box { padding:14px; border:1px solid var(--border); border-radius:16px; background:rgba(255,255,255,0.02); }
.dsr-workflow__empty, .dsr-message, .dsr-error { padding:12px 14px; border-radius:14px; font-size:12px; }
.dsr-workflow__empty { color:#b45309; background:rgba(245,158,11,.12); }
.dsr-message { color:var(--text-secondary); background:rgba(59,130,246,.08); }
.dsr-error { color:#b91c1c; background:rgba(239,68,68,.10); }
.dsr-note { width:100%; min-height:84px; }
.dsr-items { display:flex; flex-direction:column; gap:12px; }
.dsr-item-row { border:1px solid var(--border); border-radius:14px; background:var(--bg-surface); padding:12px 14px; display:flex; flex-direction:column; gap:8px; }
.dsr-item-row__top { font-size:12px; font-weight:700; color:var(--text-primary); }
.dsr-item-grid { display:grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap:8px; }
.dsr-item-row code { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; padding:8px 10px; font-size:11px; color:var(--text-secondary); white-space:pre-wrap; overflow-wrap:anywhere; word-break:break-word; }
.dsr-empty { padding:18px; border:1px dashed var(--border); border-radius:14px; color:var(--text-muted); font-size:12px; text-align:center; }
@media (max-width: 980px) {
  .dsr-layout { grid-template-columns: 1fr; }
}
@media (max-width: 720px) {
  .dsr-card { padding: 16px; }
  .dsr-toolbar { flex-wrap: wrap; }
  .dsr-filter { max-width: none; width: 100%; }
  .dsr-detail__head { flex-wrap: wrap; }
}
</style>
