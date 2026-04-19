<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'

interface DataScript {
  id: number
  name: string
  description: string
  language: string
  latest_version_id: number
  latest_version_no: number
  latest_source: string
  plans?: DataChangePlan[]
}

interface DataScriptVersion {
  id: number
  script_id: number
  version_no: number
  source_code: string
  created_at: string
}

interface DataPlanSummary {
  updates: number
  inserts: number
  deletes: number
  tables: Array<{ table: string; updates: number; inserts: number; deletes: number }>
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
  script_version_id: number
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

const toast = useToast()
const { connections, fetchConnections } = useConnections()
const { databases, fetchDatabases, loading: databasesLoading } = useDatabases()
const { hasPermission, isAdmin } = useAuth()

const loading = ref(false)
const savingScript = ref(false)
const savingVersion = ref(false)
const previewing = ref(false)
const submittingPlan = ref(false)
const reviewing = ref(false)
const executing = ref(false)

const scripts = ref<DataScript[]>([])
const versions = ref<DataScriptVersion[]>([])
const selectedScriptId = ref<number | null>(null)
const selectedPlanId = ref<number | null>(null)
const selectedPlan = ref<DataChangePlan | null>(null)
const reviewNote = ref('')

const createForm = reactive({
  name: '',
  description: '',
  source_code: `// Use one operation per line with JSON objects
plan.update("users", {"id": 101}, {"status": "active"});
plan.insert("user_tags", {"user_id": 101, "tag": "activated"});
// plan.delete("staging_users", {"id": 77});`,
})

const editor = reactive({
  source_code: '',
  conn_id: null as number | null,
  database: '',
})

const selectedScript = computed(() => scripts.value.find((item) => item.id === selectedScriptId.value) ?? null)
const scriptPlans = computed(() => selectedScript.value?.plans ?? [])
const canApprove = computed(() => isAdmin.value || hasPermission('query.approve'))
const canExecute = computed(() => isAdmin.value || hasPermission('query.execute'))

async function fetchScripts() {
  loading.value = true
  try {
    const { data } = await axios.get<DataScript[]>('/api/data-scripts')
    scripts.value = data || []
    if (!selectedScriptId.value && scripts.value.length) {
      selectedScriptId.value = scripts.value[0].id
    }
    await syncSelectedScript()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load data scripts')
  } finally {
    loading.value = false
  }
}

async function syncSelectedScript() {
  if (!selectedScriptId.value) return
  try {
    const [{ data: script }, { data: scriptVersions }] = await Promise.all([
      axios.get<DataScript>(`/api/data-scripts/${selectedScriptId.value}`),
      axios.get<DataScriptVersion[]>(`/api/data-scripts/${selectedScriptId.value}/versions`),
    ])
    const idx = scripts.value.findIndex((item) => item.id === script.id)
    if (idx >= 0) scripts.value[idx] = script
    versions.value = scriptVersions || []
    editor.source_code = script.latest_source || scriptVersions?.[0]?.source_code || ''
    if (!selectedPlanId.value && script.plans?.length) {
      selectedPlanId.value = script.plans[0].id
    }
    if (selectedPlanId.value) {
      await loadPlan(selectedPlanId.value)
    } else {
      selectedPlan.value = null
    }
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load script detail')
  }
}

async function loadPlan(planId: number) {
  selectedPlanId.value = planId
  try {
    const { data } = await axios.get<DataChangePlan>(`/api/data-change-plans/${planId}`)
    selectedPlan.value = data
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load preview plan')
  }
}

async function createScript() {
  if (!createForm.name.trim() || !createForm.source_code.trim()) return
  savingScript.value = true
  try {
    const { data } = await axios.post<DataScript>('/api/data-scripts', {
      name: createForm.name.trim(),
      description: createForm.description.trim(),
      language: 'javascript',
      source_code: createForm.source_code,
    })
    toast.success(`Script #${data.id} created`)
    createForm.name = ''
    createForm.description = ''
    selectedScriptId.value = data.id
    await fetchScripts()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to create script')
  } finally {
    savingScript.value = false
  }
}

async function saveVersion() {
  if (!selectedScriptId.value || !editor.source_code.trim()) return
  savingVersion.value = true
  try {
    await axios.post(`/api/data-scripts/${selectedScriptId.value}/versions`, {
      source_code: editor.source_code,
      params_schema: '{}',
    })
    toast.success('New script version saved')
    await syncSelectedScript()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to save version')
  } finally {
    savingVersion.value = false
  }
}

async function previewScript() {
  if (!selectedScript.value || !selectedScript.value.latest_version_id || !editor.conn_id) return
  previewing.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-scripts/${selectedScript.value.id}/preview`, {
      script_version_id: selectedScript.value.latest_version_id,
      conn_id: editor.conn_id,
      database: editor.database,
    })
    toast.success(`Preview plan #${data.id} created`)
    await fetchScripts()
    await loadPlan(data.id)
    selectedPlanId.value = data.id
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Preview failed')
  } finally {
    previewing.value = false
  }
}

async function submitPlan() {
  if (!selectedPlan.value) return
  submittingPlan.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-change-plans/${selectedPlan.value.id}/submit`)
    toast.success(`Plan #${data.id} submitted for review`)
    await fetchScripts()
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
    await fetchScripts()
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
    await fetchScripts()
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

watch(selectedScriptId, async () => {
  selectedPlanId.value = null
  selectedPlan.value = null
  if (selectedScriptId.value) {
    await syncSelectedScript()
  }
})

watch(() => editor.conn_id, async (connId) => {
  await fetchDatabases(connId)
  editor.database = databases.value[0] ?? ''
})

onMounted(async () => {
  await fetchConnections()
  await fetchScripts()
})
</script>

<template>
  <div class="page-shell ds-view">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Automation</div>
            <div class="page-title">Data Scripts</div>
            <div class="page-subtitle">Write constrained JS-like data change scripts, preview the row operations, then require approval before execution.</div>
          </div>
        </section>

        <div class="ds-grid">
          <aside class="page-card ds-sidebar">
            <div class="ds-section-head">
              <div>
                <div class="ds-section-title">Scripts</div>
                <div class="ds-section-subtitle">{{ scripts.length }} available</div>
              </div>
            </div>

            <div class="ds-create">
              <input v-model="createForm.name" class="base-input" placeholder="Script name" />
              <input v-model="createForm.description" class="base-input" placeholder="Description" />
              <textarea v-model="createForm.source_code" class="ds-source ds-source--compact" rows="8" />
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingScript || !createForm.name.trim() || !createForm.source_code.trim()" @click="createScript">
                {{ savingScript ? 'Creating…' : 'Create Script' }}
              </button>
            </div>

            <div v-if="loading" class="ds-empty">Loading scripts…</div>
            <div v-else-if="!scripts.length" class="ds-empty">No scripts yet.</div>
            <button
              v-for="item in scripts"
              :key="item.id"
              class="ds-script-item"
              :class="{ 'ds-script-item--active': item.id === selectedScriptId }"
              @click="selectedScriptId = item.id"
            >
              <div class="ds-script-item__name">{{ item.name }}</div>
              <div class="ds-script-item__meta">v{{ item.latest_version_no }} · {{ item.language }}</div>
            </button>
          </aside>

          <section class="page-card ds-main" v-if="selectedScript">
            <div class="ds-section-head">
              <div>
                <div class="ds-section-title">{{ selectedScript.name }}</div>
                <div class="ds-section-subtitle">{{ selectedScript.description || 'No description provided' }}</div>
              </div>
              <div class="ds-actions">
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="savingVersion || !editor.source_code.trim()" @click="saveVersion">
                  {{ savingVersion ? 'Saving…' : 'Save New Version' }}
                </button>
              </div>
            </div>

            <div class="ds-toolbar">
              <select v-model="editor.conn_id" class="base-input">
                <option :value="null">Select connection…</option>
                <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
              </select>
              <select v-model="editor.database" class="base-input" :disabled="!editor.conn_id || databasesLoading || !databases.length">
                <option value="">Default database/schema</option>
                <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
              </select>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="previewing || !editor.conn_id || !selectedScript.latest_version_id" @click="previewScript">
                {{ previewing ? 'Previewing…' : 'Preview Plan' }}
              </button>
            </div>

            <textarea v-model="editor.source_code" class="ds-source" rows="14" spellcheck="false" />

            <div class="ds-help">
              <div class="ds-help__title">V1 script syntax</div>
              <code>plan.insert("table", {"id": 1, "name": "A"})</code>
              <code>plan.update("table", {"id": 1}, {"status": "active"})</code>
              <code>plan.delete("table", {"id": 1})</code>
              <span>Use one operation per line with valid JSON objects.</span>
            </div>

            <div class="ds-panel-grid">
              <div class="page-toolbar-surface ds-plan-list">
                <div class="ds-section-head ds-section-head--tight">
                  <div>
                    <div class="ds-section-title">Preview Plans</div>
                    <div class="ds-section-subtitle">Latest previews and approvals</div>
                  </div>
                </div>
                <div v-if="!scriptPlans.length" class="ds-empty">No preview plan yet.</div>
                <button
                  v-for="plan in scriptPlans"
                  :key="plan.id"
                  class="ds-plan-item"
                  :class="{ 'ds-plan-item--active': plan.id === selectedPlanId }"
                  @click="loadPlan(plan.id)"
                >
                  <div class="ds-plan-item__top">
                    <span>#{{ plan.id }}</span>
                    <span class="ds-status" :data-status="plan.status">{{ plan.status }}</span>
                  </div>
                  <div class="ds-plan-item__meta">{{ plan.connection || 'No connection' }} · {{ formatDate(plan.created_at) }}</div>
                </button>
              </div>

              <div class="page-toolbar-surface ds-plan-detail" v-if="selectedPlan">
                <div class="ds-section-head ds-section-head--tight">
                  <div>
                    <div class="ds-section-title">Plan #{{ selectedPlan.id }}</div>
                    <div class="ds-section-subtitle">{{ selectedPlan.connection }} · {{ selectedPlan.database_name || 'default database/schema' }}</div>
                  </div>
                  <span class="ds-status" :data-status="selectedPlan.status">{{ selectedPlan.status }}</span>
                </div>

                <div class="ds-summary">
                  <div class="ds-stat"><strong>{{ selectedPlan.summary.updates }}</strong><span>Updates</span></div>
                  <div class="ds-stat"><strong>{{ selectedPlan.summary.inserts }}</strong><span>Inserts</span></div>
                  <div class="ds-stat"><strong>{{ selectedPlan.summary.deletes }}</strong><span>Deletes</span></div>
                  <div class="ds-stat"><strong>{{ selectedPlan.risk.level }}</strong><span>Risk</span></div>
                </div>

                <div v-if="selectedPlan.risk.flags?.length" class="ds-flags">
                  <span v-for="flag in selectedPlan.risk.flags" :key="flag" class="ds-flag">{{ flag }}</span>
                </div>

                <div class="ds-review">
                  <textarea v-model="reviewNote" class="base-input ds-note" rows="3" placeholder="Review note or rejection reason…" />
                  <div class="ds-actions">
                    <button class="base-btn base-btn--ghost base-btn--sm" :disabled="submittingPlan || selectedPlan.status !== 'draft' && selectedPlan.status !== 'rejected'" @click="submitPlan">
                      {{ submittingPlan ? 'Submitting…' : 'Submit for Approval' }}
                    </button>
                    <button v-if="canApprove" class="base-btn base-btn--ghost base-btn--sm" :disabled="reviewing || selectedPlan.status !== 'pending_review'" @click="reviewPlan('rejected')">
                      Reject
                    </button>
                    <button v-if="canApprove" class="base-btn base-btn--primary base-btn--sm" :disabled="reviewing || selectedPlan.status !== 'pending_review'" @click="reviewPlan('approved')">
                      Approve
                    </button>
                    <button v-if="canExecute" class="base-btn base-btn--primary base-btn--sm" :disabled="executing || selectedPlan.status !== 'approved'" @click="executePlan">
                      {{ executing ? 'Executing…' : 'Execute Approved Plan' }}
                    </button>
                  </div>
                </div>

                <div class="ds-meta">
                  <span>Created by {{ selectedPlan.creator_name || 'unknown' }}</span>
                  <span>Reviewer {{ selectedPlan.reviewer_name || '—' }}</span>
                  <span>{{ formatDate(selectedPlan.created_at) }}</span>
                </div>

                <div v-if="selectedPlan.review_note" class="ds-message">
                  <strong>Review note:</strong> {{ selectedPlan.review_note }}
                </div>
                <div v-if="selectedPlan.execute_error" class="ds-error">
                  <strong>Execution error:</strong> {{ selectedPlan.execute_error }}
                </div>

                <div class="ds-items">
                  <div class="ds-items__head">Planned Operations</div>
                  <div v-for="item in selectedPlan.items" :key="item.id" class="ds-item-row">
                    <div class="ds-item-row__top">
                      <span>{{ item.seq_no }}. {{ item.op_type.toUpperCase() }} {{ item.table_name }}</span>
                    </div>
                    <code>PK: {{ JSON.stringify(item.pk) }}</code>
                    <code v-if="Object.keys(item.after || {}).length">After: {{ JSON.stringify(item.after) }}</code>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ds-grid { display:grid; grid-template-columns: 320px minmax(0,1fr); gap:16px; }
.ds-sidebar, .ds-main { min-height: 0; }
.ds-section-head { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; margin-bottom:12px; }
.ds-section-head--tight { margin-bottom:10px; }
.ds-section-title { font-size:14px; font-weight:700; color:var(--text-primary); }
.ds-section-subtitle { font-size:12px; color:var(--text-muted); }
.ds-create { display:flex; flex-direction:column; gap:8px; margin-bottom:14px; }
.ds-source { width:100%; min-height:220px; border:1px solid var(--border); border-radius:10px; background:var(--bg-elevated); color:var(--text-primary); font:12px/1.55 var(--mono, "JetBrains Mono", monospace); padding:12px; resize:vertical; }
.ds-source--compact { min-height:160px; }
.ds-empty { padding:16px; border:1px dashed var(--border); border-radius:10px; color:var(--text-muted); font-size:12px; text-align:center; }
.ds-script-item, .ds-plan-item { width:100%; text-align:left; border:1px solid var(--border); border-radius:10px; background:var(--bg-elevated); padding:10px 12px; margin-bottom:8px; cursor:pointer; }
.ds-script-item--active, .ds-plan-item--active { border-color: var(--brand); box-shadow: inset 0 0 0 1px var(--brand); }
.ds-script-item__name { font-size:13px; font-weight:600; color:var(--text-primary); }
.ds-script-item__meta, .ds-plan-item__meta, .ds-meta { font-size:11px; color:var(--text-muted); display:flex; gap:10px; flex-wrap:wrap; }
.ds-toolbar, .ds-actions, .ds-summary, .ds-flags { display:flex; gap:10px; flex-wrap:wrap; }
.ds-toolbar { margin-bottom:12px; }
.ds-help { display:flex; flex-direction:column; gap:6px; margin-top:10px; padding:12px; border-radius:10px; background:var(--bg-elevated); border:1px solid var(--border); color:var(--text-secondary); font-size:12px; }
.ds-help__title { font-weight:700; color:var(--text-primary); }
.ds-panel-grid { display:grid; grid-template-columns: 280px minmax(0,1fr); gap:12px; margin-top:14px; }
.ds-plan-item__top { display:flex; align-items:center; justify-content:space-between; gap:10px; font-size:12px; color:var(--text-secondary); margin-bottom:6px; }
.ds-status { display:inline-flex; align-items:center; padding:3px 8px; border-radius:999px; font-size:11px; font-weight:700; text-transform:capitalize; background:var(--bg-surface); color:var(--text-secondary); }
.ds-status[data-status="approved"], .ds-status[data-status="done"] { background: rgba(34,197,94,.15); color:#16a34a; }
.ds-status[data-status="pending_review"], .ds-status[data-status="executing"] { background: rgba(245,158,11,.16); color:#d97706; }
.ds-status[data-status="rejected"], .ds-status[data-status="failed"] { background: rgba(239,68,68,.14); color:#dc2626; }
.ds-stat { min-width:92px; padding:10px 12px; border:1px solid var(--border); border-radius:10px; background:var(--bg-elevated); display:flex; flex-direction:column; gap:4px; }
.ds-stat strong { font-size:18px; color:var(--text-primary); text-transform:capitalize; }
.ds-stat span { font-size:11px; color:var(--text-muted); text-transform:uppercase; letter-spacing:.04em; }
.ds-flag { display:inline-flex; padding:4px 8px; border-radius:999px; background:rgba(59,130,246,.12); color:#2563eb; font-size:11px; font-weight:700; }
.ds-note { width:100%; min-height:84px; }
.ds-review { display:flex; flex-direction:column; gap:10px; margin-top:14px; }
.ds-message, .ds-error { margin-top:10px; padding:10px 12px; border-radius:10px; font-size:12px; }
.ds-message { background:rgba(59,130,246,.08); color:var(--text-secondary); }
.ds-error { background:rgba(239,68,68,.1); color:#b91c1c; }
.ds-items { margin-top:14px; display:flex; flex-direction:column; gap:10px; }
.ds-items__head { font-size:12px; font-weight:700; color:var(--text-primary); }
.ds-item-row { border:1px solid var(--border); border-radius:10px; background:var(--bg-elevated); padding:10px 12px; display:flex; flex-direction:column; gap:6px; }
.ds-item-row__top { font-size:12px; font-weight:700; color:var(--text-primary); }
.ds-item-row code, .ds-help code { background:var(--bg-surface); border:1px solid var(--border); border-radius:8px; padding:6px 8px; font-size:11px; color:var(--text-secondary); }
@media (max-width: 1100px) {
  .ds-grid, .ds-panel-grid { grid-template-columns: 1fr; }
}
</style>
