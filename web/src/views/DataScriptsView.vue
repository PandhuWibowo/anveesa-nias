<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useRoute } from 'vue-router'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import ScriptEditor from '@/components/ui/ScriptEditor.vue'

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
  params_schema: string
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

interface SchemaColumn {
  name: string
  data_type?: string
}

interface SchemaDatabase {
  name: string
  tables: Array<{ name: string }>
}

interface ScriptSchemaTable {
  name: string
  columns: Array<{ name: string; type?: string }>
}

interface ApplicableWorkflow {
  id: number
  name: string
  description: string
}

const LANGUAGE_LABELS: Record<string, string> = {
  javascript: 'JavaScript',
  python: 'Python',
  php: 'PHP',
}

const LANGUAGE_TEMPLATES: Record<string, string> = {
  javascript: `for (const userId of [4]) {
  plan.update("users", { id: userId }, { username: "pandhux" });
}`,
  python: `for user_id in [4]:
    plan.update("users", {"id": user_id}, {"username": "pandhux"})`,
  php: `$userIds = [4];
foreach ($userIds as $userId) {
    $plan->update("users", ["id" => $userId], ["username" => "pandhux"]);
}`,
}

const toast = useToast()
const route = useRoute()
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
const createPanelOpen = ref(false)
const libraryPanelOpen = ref(true)

const scripts = ref<DataScript[]>([])
const versions = ref<DataScriptVersion[]>([])
const scriptSchemaTables = ref<ScriptSchemaTable[]>([])
const selectedScriptId = ref<number | null>(null)
const selectedVersionId = ref<number | null>(null)
const selectedPlanId = ref<number | null>(null)
const selectedPlan = ref<DataChangePlan | null>(null)
const reviewNote = ref('')
const applicableWorkflows = ref<ApplicableWorkflow[]>([])
const selectedWorkflowId = ref<number | null>(null)
const workflowsLoading = ref(false)

const createForm = reactive({
  name: '',
  description: '',
  language: 'javascript',
  source_code: LANGUAGE_TEMPLATES.javascript,
})

const editor = reactive({
  source_code: '',
  conn_id: null as number | null,
  database: '',
})

const selectedScript = computed(() => scripts.value.find((item) => item.id === selectedScriptId.value) ?? null)
const selectedVersion = computed(() => versions.value.find((item) => item.id === selectedVersionId.value) ?? null)
const scriptPlans = computed(() => selectedScript.value?.plans ?? [])
const canApprove = computed(() => isAdmin.value || hasPermission('query.approve'))
const canExecute = computed(() => isAdmin.value || hasPermission('query.execute'))
const hasUnsavedChanges = computed(() => (selectedVersion.value?.source_code ?? '') !== editor.source_code)
const selectedLanguageLabel = computed(() => LANGUAGE_LABELS[selectedScript.value?.language || 'javascript'] || selectedScript.value?.language || 'Unknown')
const currentHelpExamples = computed(() => {
  switch (selectedScript.value?.language) {
    case 'python':
      return [
        'plan.update("users", {"id": 4}, {"username": "pandhux"})',
        'plan.insert("user_tags", {"user_id": 4, "tag": "vip"})',
        'plan.delete("staging_users", {"id": 77})',
      ]
    case 'php':
      return [
        '$plan->update("users", ["id" => 4], ["username" => "pandhux"]);',
        '$plan->insert("user_tags", ["user_id" => 4, "tag" => "vip"]);',
        '$plan->delete("staging_users", ["id" => 77]);',
      ]
    default:
      return [
        'plan.update("users", { id: 4 }, { username: "pandhux" });',
        'plan.insert("user_tags", { user_id: 4, tag: "vip" });',
        'plan.delete("staging_users", { id: 77 });',
      ]
  }
})

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
    if (!selectedVersionId.value || !versions.value.some((item) => item.id === selectedVersionId.value)) {
      selectedVersionId.value = versions.value[0]?.id ?? script.latest_version_id ?? null
    }
    editor.source_code = selectedVersion.value?.source_code || script.latest_source || scriptVersions?.[0]?.source_code || ''
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

async function loadApplicableWorkflows(connId?: number | null) {
  applicableWorkflows.value = []
  selectedWorkflowId.value = null
  if (!connId) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<ApplicableWorkflow[]>('/api/workflows/applicable', {
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

async function createScript() {
  if (!createForm.name.trim() || !createForm.source_code.trim()) return
  savingScript.value = true
  try {
    const { data } = await axios.post<DataScript>('/api/data-scripts', {
      name: createForm.name.trim(),
      description: createForm.description.trim(),
      language: createForm.language,
      source_code: createForm.source_code,
    })
    toast.success(`Script #${data.id} created`)
    createForm.name = ''
    createForm.description = ''
    createForm.language = 'javascript'
    createForm.source_code = LANGUAGE_TEMPLATES.javascript
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
    const { data } = await axios.post<DataScriptVersion>(`/api/data-scripts/${selectedScriptId.value}/versions`, {
      source_code: editor.source_code,
      params_schema: '{}',
    })
    toast.success('New script version saved')
    selectedVersionId.value = data.id
    await syncSelectedScript()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to save version')
  } finally {
    savingVersion.value = false
  }
}

async function previewScript() {
  if (!selectedScript.value || !selectedVersionId.value || !editor.conn_id) return
  if (hasUnsavedChanges.value) {
    toast.error('Save a new version before previewing these edits')
    return
  }
  previewing.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-scripts/${selectedScript.value.id}/preview`, {
      script_version_id: selectedVersionId.value,
      conn_id: editor.conn_id,
      database: editor.database,
    })
    if (!data || !data.id) {
      throw new Error('Preview returned an empty response')
    }
    toast.success(`Preview plan #${data.id} created`)
    await fetchScripts()
    await loadPlan(data.id)
    selectedPlanId.value = data.id
  } catch (error: any) {
    toast.error(error.response?.data?.error || error.message || 'Preview failed')
  } finally {
    previewing.value = false
  }
}

async function submitDraft() {
  if (!selectedScript.value || !selectedVersionId.value || !editor.conn_id) return
  if (hasUnsavedChanges.value) {
    toast.error('Save a new version before submitting these edits')
    return
  }
  if (!selectedWorkflowId.value) {
    toast.error('Select an approval workflow first')
    return
  }
  submittingPlan.value = true
  try {
    const { data } = await axios.post<DataChangePlan>(`/api/data-scripts/${selectedScript.value.id}/submit`, {
      script_version_id: selectedVersionId.value,
      conn_id: editor.conn_id,
      database: editor.database,
      workflow_id: selectedWorkflowId.value,
    })
    toast.success(`Draft #${data.id} submitted for review`)
    await fetchScripts()
    await loadPlan(data.id)
    selectedPlanId.value = data.id
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to submit draft')
  } finally {
    submittingPlan.value = false
  }
}

async function loadScriptSchema(connId: number | null, database: string) {
  scriptSchemaTables.value = []
  if (!connId || !database) return
  try {
    const { data } = await axios.get<SchemaDatabase[]>(`/api/connections/${connId}/schema`)
    const selectedDatabase = (data ?? []).find((item) => item.name === database)
    if (!selectedDatabase) return
    const tables = await Promise.all(
      selectedDatabase.tables.map(async (table) => {
        try {
          const { data: columns } = await axios.get<SchemaColumn[]>(
            `/api/connections/${connId}/schema/${encodeURIComponent(database)}/tables/${encodeURIComponent(table.name)}/columns`,
          )
          return {
            name: table.name,
            columns: (columns ?? []).map((column) => ({
              name: column.name,
              type: column.data_type,
            })),
          }
        } catch {
          return {
            name: table.name,
            columns: [],
          }
        }
      }),
    )
    scriptSchemaTables.value = tables.sort((a, b) => a.name.localeCompare(b.name))
  } catch {
    scriptSchemaTables.value = []
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

function formatJson(value?: Record<string, unknown>) {
  return JSON.stringify(value ?? {}, null, 2)
}

function hasFields(value?: Record<string, unknown>) {
  return !!value && Object.keys(value).length > 0
}

watch(selectedScriptId, async () => {
  selectedVersionId.value = null
  selectedPlanId.value = null
  selectedPlan.value = null
  if (selectedScriptId.value) {
    await syncSelectedScript()
  }
})

watch(selectedVersionId, (versionId) => {
  const version = versions.value.find((item) => item.id === versionId)
  if (version) {
    editor.source_code = version.source_code
  }
})

watch(selectedPlan, async (plan) => {
  await loadApplicableWorkflows(plan?.conn_id)
})

watch(() => createForm.language, (language) => {
  createForm.source_code = LANGUAGE_TEMPLATES[language] || LANGUAGE_TEMPLATES.javascript
})

watch(() => editor.conn_id, async (connId) => {
  await fetchDatabases(connId)
  editor.database = databases.value[0] ?? ''
  await loadScriptSchema(connId, editor.database)
  if (!selectedPlan.value || selectedPlan.value.conn_id !== connId) {
    await loadApplicableWorkflows(connId)
  }
})

watch(() => editor.database, async (database) => {
  await loadScriptSchema(editor.conn_id, database)
})

onMounted(async () => {
  const routeScriptId = Number(route.query.script)
  if (Number.isFinite(routeScriptId) && routeScriptId > 0) {
    selectedScriptId.value = routeScriptId
  }
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
            <div class="page-subtitle">Scripts are reusable definitions. Requests are created only after you run Preview Plan and generate a plan from a saved version.</div>
          </div>
        </section>

        <section class="page-card ds-create-panel">
          <div class="ds-section-head">
            <div>
              <div class="ds-section-title">Create Script</div>
              <div class="ds-section-subtitle">Start a new native data script without compressing the main editor area.</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" type="button" @click="createPanelOpen = !createPanelOpen">
              {{ createPanelOpen ? 'Hide' : 'Show' }}
            </button>
          </div>

          <div v-if="createPanelOpen" class="ds-create">
            <input v-model="createForm.name" class="base-input" placeholder="Script name" />
            <input v-model="createForm.description" class="base-input" placeholder="Description" />
            <select v-model="createForm.language" class="base-input">
              <option value="javascript">JavaScript</option>
              <option value="python">Python</option>
              <option value="php">PHP</option>
            </select>
            <ScriptEditor
              v-model="createForm.source_code"
              class="ds-source-editor ds-source-editor--create"
              placeholder="Start a new native data script"
              :schema-tables="scriptSchemaTables"
              :language="createForm.language"
              :file-label="`${createForm.name.trim() || 'new-data-script'}.${createForm.language === 'python' ? 'py' : createForm.language === 'php' ? 'php' : 'js'}`"
              :show-preview-button="false"
            />
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingScript || !createForm.name.trim() || !createForm.source_code.trim()" @click="createScript">
              {{ savingScript ? 'Creating…' : 'Create Script' }}
            </button>
          </div>
          <div v-else class="ds-collapsed-note">Creation form hidden to keep the editor workspace visible.</div>
        </section>

        <section class="page-card ds-library-panel">
          <div class="ds-section-head">
            <div>
              <div class="ds-section-title">Saved Scripts</div>
              <div class="ds-section-subtitle">{{ scripts.length }} available</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" type="button" @click="libraryPanelOpen = !libraryPanelOpen">
              {{ libraryPanelOpen ? 'Hide' : 'Show' }}
            </button>
          </div>

          <div v-if="libraryPanelOpen && loading" class="ds-empty">Loading scripts…</div>
          <div v-else-if="libraryPanelOpen && !scripts.length" class="ds-empty">No scripts yet.</div>
          <div v-else-if="libraryPanelOpen" class="ds-script-grid">
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
          </div>
          <div v-else class="ds-collapsed-note">Script library hidden. Open it when you want to switch scripts.</div>
        </section>

        <section class="ds-workspace" v-if="selectedScript">
          <div class="page-card ds-editor-panel">
              <div class="ds-section-head">
                <div>
                  <div class="ds-section-title">{{ selectedScript.name }}</div>
                  <div class="ds-section-subtitle">{{ selectedScript.description || 'No description provided' }}</div>
                </div>
                <div class="ds-actions">
                  <span class="ds-language-badge">{{ selectedLanguageLabel }}</span>
                  <button class="base-btn base-btn--ghost base-btn--sm" :disabled="savingVersion || !editor.source_code.trim()" @click="saveVersion">
                    {{ savingVersion ? 'Saving…' : 'Save New Version' }}
                  </button>
                </div>
              </div>

              <div class="ds-version-strip">
                <button
                  v-for="version in versions"
                  :key="version.id"
                  class="ds-version-chip"
                  :class="{ 'ds-version-chip--active': version.id === selectedVersionId }"
                  @click="selectedVersionId = version.id"
                >
                  <span>v{{ version.version_no }}</span>
                  <span>{{ formatDate(version.created_at) }}</span>
                </button>
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
                <select v-model="selectedWorkflowId" class="base-input" :disabled="workflowsLoading || !editor.conn_id || !applicableWorkflows.length">
                  <option :value="null">{{ workflowsLoading ? 'Loading workflows…' : 'Select workflow…' }}</option>
                  <option v-for="workflow in applicableWorkflows" :key="workflow.id" :value="workflow.id">{{ workflow.name }}</option>
                </select>
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="previewing || !editor.conn_id || !selectedVersionId || hasUnsavedChanges" @click="previewScript">
                  {{ previewing ? 'Previewing…' : 'Preview Plan' }}
                </button>
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="submittingPlan || !editor.conn_id || !selectedVersionId || hasUnsavedChanges || !selectedWorkflowId" @click="submitDraft">
                  {{ submittingPlan ? 'Submitting…' : 'Submit Draft' }}
                </button>
              </div>
              <div v-if="editor.conn_id && !workflowsLoading && !applicableWorkflows.length" class="ds-toolbar-note">
                No applicable workflow matches this connection. Configure one in `Workflows` before submitting.
              </div>

              <ScriptEditor
                v-model="editor.source_code"
                class="ds-source-editor"
                placeholder="Use plan.insert / plan.update / plan.delete"
                :schema-tables="scriptSchemaTables"
                :language="selectedScript.language"
                :file-label="`${selectedScript.name || 'data-script'}.${selectedScript.language === 'python' ? 'py' : selectedScript.language === 'php' ? 'php' : 'js'}`"
                @preview-request="previewScript"
              />

              <div v-if="selectedVersion" class="ds-editor-meta">
                <span>Editing version v{{ selectedVersion.version_no }}</span>
                <span>{{ hasUnsavedChanges ? 'Unsaved changes' : 'Saved version' }}</span>
              </div>

              <div class="ds-help">
                <div class="ds-help__title">Native Runtime</div>
                <code v-for="example in currentHelpExamples" :key="example">{{ example }}</code>
                <span>Use the language selected for this script and call the provided `plan` helper to emit operations.</span>
                <span>Preview is optional and only lets you inspect the exact plan before submitting.</span>
                <span>Submit Draft creates the exact plan under the hood and sends it straight into the approval queue.</span>
                <span>Saved script versions do not appear in the request queue by themselves. A request exists only after preview or submit creates a plan.</span>
              </div>
            </div>
        </section>

        <section class="page-card ds-review-panel" v-if="selectedScript">
          <div class="ds-section-head">
            <div>
              <div class="ds-section-title">Requests For This Script</div>
              <div class="ds-section-subtitle">Preview-created plans for this script, including draft, review, approval, and execution state</div>
            </div>
          </div>

          <div v-if="!scriptPlans.length" class="ds-empty">
            No requests yet for this script. Save your version, choose a connection and workflow, then click `Submit Draft`. Use `Preview Plan` only if you want to inspect the exact operations first.
          </div>
          <div v-else class="ds-plan-list">
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

          <div class="ds-plan-detail" v-if="selectedPlan">
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
              <div class="ds-workflow-box">
                <div class="ds-workflow-box__head">
                  <strong>Approval Workflow</strong>
                  <span v-if="workflowsLoading">Loading…</span>
                </div>
                <select
                  v-model="selectedWorkflowId"
                  class="base-input"
                  :disabled="workflowsLoading || !applicableWorkflows.length || selectedPlan.status !== 'draft' && selectedPlan.status !== 'rejected'"
                >
                  <option :value="null">Select workflow…</option>
                  <option v-for="workflow in applicableWorkflows" :key="workflow.id" :value="workflow.id">
                    {{ workflow.name }}
                  </option>
                </select>
                <div v-if="!workflowsLoading && !applicableWorkflows.length" class="ds-workflow-empty">
                  No applicable workflow matches this connection. Configure one in Workflows before submitting.
                </div>
                <div v-else-if="selectedWorkflowId" class="ds-workflow-hint">
                  {{ applicableWorkflows.find((workflow) => workflow.id === selectedWorkflowId)?.description || 'Selected workflow will be used for this plan.' }}
                </div>
              </div>
              <textarea v-model="reviewNote" class="base-input ds-note" rows="3" placeholder="Review note or rejection reason…" />
              <div class="ds-actions">
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
                <div class="ds-item-grid">
                  <code>PK: {{ formatJson(item.pk) }}</code>
                  <code v-if="hasFields(item.before)">Before: {{ formatJson(item.before) }}</code>
                  <code v-if="hasFields(item.after)">After: {{ formatJson(item.after) }}</code>
                </div>
                <div v-if="(item.op_type === 'update' || item.op_type === 'delete') && !hasFields(item.before)" class="ds-item-warning">
                  No matching row was found during preview for the supplied key.
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
.ds-view { min-width: 0; }
.ds-create-panel,
.ds-library-panel,
.ds-editor-panel,
.ds-review-panel {
  padding: 20px;
}
.ds-workspace { min-width: 0; }
.ds-section-head { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; margin-bottom:12px; }
.ds-section-head > :first-child { min-width:0; }
.ds-section-head--tight { margin-bottom:10px; }
.ds-section-title { font-size:14px; font-weight:700; color:var(--text-primary); }
.ds-section-subtitle { font-size:12px; color:var(--text-muted); }
.ds-create { display:flex; flex-direction:column; gap:10px; margin-bottom:18px; padding:14px; border:1px solid var(--border); border-radius:16px; background:rgba(255,255,255,0.02); }
.ds-create .base-input { width:100%; }
.ds-collapsed-note { padding: 12px 14px; border:1px dashed var(--border); border-radius:14px; color:var(--text-muted); font-size:12px; background:rgba(255,255,255,0.02); }
.ds-source { width:100%; min-height:220px; border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); color:var(--text-primary); font:12px/1.55 var(--mono, "JetBrains Mono", monospace); padding:14px; resize:vertical; }
.ds-source--compact { min-height:160px; }
.ds-source-editor { min-height: 0; }
.ds-version-strip { display:flex; gap:10px; overflow:auto; padding:2px 2px 6px; margin-bottom:14px; }
.ds-version-chip { display:flex; flex-direction:column; gap:2px; min-width:118px; text-align:left; border:1px solid var(--border); border-radius:12px; background:var(--bg-elevated); padding:10px 12px; color:var(--text-secondary); font-size:11px; cursor:pointer; }
.ds-version-chip--active { border-color: var(--brand); box-shadow: inset 0 0 0 1px var(--brand); color:var(--text-primary); }
.ds-language-badge { display:inline-flex; align-items:center; padding:6px 10px; border:1px solid var(--border); border-radius:999px; background:rgba(255,255,255,0.02); color:var(--text-secondary); font-size:11px; font-weight:700; }
.ds-empty { padding:18px; border:1px dashed var(--border); border-radius:14px; color:var(--text-muted); font-size:12px; text-align:center; }
.ds-script-item, .ds-plan-item { width:100%; text-align:left; border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); padding:12px 14px; margin-bottom:10px; cursor:pointer; }
.ds-script-item--active, .ds-plan-item--active { border-color: var(--brand); box-shadow: inset 0 0 0 1px var(--brand); }
.ds-script-item__name { font-size:13px; font-weight:600; color:var(--text-primary); }
.ds-script-item__meta, .ds-plan-item__meta, .ds-meta { font-size:11px; color:var(--text-muted); display:flex; gap:10px; flex-wrap:wrap; }
.ds-script-grid { display:grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap:12px; }
.ds-script-grid .ds-script-item { margin-bottom:0; min-height:78px; }
.ds-toolbar, .ds-actions, .ds-summary, .ds-flags { display:flex; gap:10px; flex-wrap:wrap; }
.ds-toolbar { margin-bottom:14px; padding:14px; border:1px solid var(--border); border-radius:16px; background:rgba(255,255,255,0.02); }
.ds-toolbar > * { flex:1 1 180px; }
.ds-toolbar-note { margin-top:-4px; margin-bottom:14px; padding:10px 12px; border-radius:12px; background:rgba(245,158,11,.12); color:#b45309; font-size:12px; }
.ds-editor-meta { display:flex; justify-content:space-between; gap:10px; margin-top:10px; font-size:11px; color:var(--text-muted); }
.ds-help { display:flex; flex-direction:column; gap:6px; margin-top:14px; padding:14px; border-radius:16px; background:var(--bg-elevated); border:1px solid var(--border); color:var(--text-secondary); font-size:12px; }
.ds-help__title { font-weight:700; color:var(--text-primary); }
.ds-plan-list { display:grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap:10px; margin-bottom:4px; }
.ds-plan-item { margin-bottom:0; }
.ds-plan-detail { margin-top:18px; padding-top:18px; border-top:1px solid var(--border); }
.ds-plan-item__top { display:flex; align-items:center; justify-content:space-between; gap:10px; font-size:12px; color:var(--text-secondary); margin-bottom:6px; }
.ds-status { display:inline-flex; align-items:center; padding:3px 8px; border-radius:999px; font-size:11px; font-weight:700; text-transform:capitalize; background:var(--bg-surface); color:var(--text-secondary); }
.ds-status[data-status="approved"], .ds-status[data-status="done"] { background: rgba(34,197,94,.15); color:#16a34a; }
.ds-status[data-status="pending_review"], .ds-status[data-status="executing"] { background: rgba(245,158,11,.16); color:#d97706; }
.ds-status[data-status="rejected"], .ds-status[data-status="failed"] { background: rgba(239,68,68,.14); color:#dc2626; }
.ds-stat { min-width:92px; padding:12px 14px; border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); display:flex; flex-direction:column; gap:4px; }
.ds-stat strong { font-size:18px; color:var(--text-primary); text-transform:capitalize; }
.ds-stat span { font-size:11px; color:var(--text-muted); text-transform:uppercase; letter-spacing:.04em; }
.ds-flag { display:inline-flex; padding:4px 8px; border-radius:999px; background:rgba(59,130,246,.12); color:#2563eb; font-size:11px; font-weight:700; }
.ds-note { width:100%; min-height:84px; }
.ds-review { display:flex; flex-direction:column; gap:10px; margin-top:16px; }
.ds-workflow-box { display:flex; flex-direction:column; gap:8px; padding:12px 14px; border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); }
.ds-workflow-box__head { display:flex; align-items:center; justify-content:space-between; gap:10px; font-size:12px; color:var(--text-primary); }
.ds-workflow-empty { font-size:12px; color:#b45309; background:rgba(245,158,11,.12); border-radius:10px; padding:8px 10px; }
.ds-workflow-hint { font-size:12px; color:var(--text-muted); }
.ds-message, .ds-error { margin-top:12px; padding:12px 14px; border-radius:14px; font-size:12px; }
.ds-message { background:rgba(59,130,246,.08); color:var(--text-secondary); }
.ds-error { background:rgba(239,68,68,.1); color:#b91c1c; }
.ds-item-warning { font-size:11px; color:#b45309; background:rgba(245,158,11,.12); border-radius:10px; padding:8px 10px; }
.ds-items { margin-top:16px; display:flex; flex-direction:column; gap:12px; }
.ds-items__head { font-size:12px; font-weight:700; color:var(--text-primary); }
.ds-item-row { border:1px solid var(--border); border-radius:14px; background:var(--bg-elevated); padding:12px 14px; display:flex; flex-direction:column; gap:8px; }
.ds-item-row__top { font-size:12px; font-weight:700; color:var(--text-primary); }
.ds-item-grid { display:grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap:8px; }
.ds-item-row code, .ds-help code {
  background:var(--bg-surface);
  border:1px solid var(--border);
  border-radius:10px;
  padding:8px 10px;
  font-size:11px;
  color:var(--text-secondary);
  white-space:pre-wrap;
  overflow-wrap:anywhere;
  word-break:break-word;
}
@media (max-width: 1100px) {
  .ds-toolbar > * { flex: 1 1 100%; }
  .ds-editor-meta { flex-direction:column; }
}
@media (max-width: 720px) {
  .ds-create-panel,
  .ds-library-panel,
  .ds-editor-panel,
  .ds-review-panel {
    padding: 16px;
  }
  .ds-section-head {
    flex-wrap: wrap;
  }
  .ds-script-grid { grid-template-columns: 1fr; }
}
</style>
