<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useSearchCache } from '@/composables/useSearchCache'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

type Tab = 'ilm' | 'templates' | 'app-policies' | 'shard-rules'

type ILMPhaseAction = { min_age?: string; actions?: Record<string, any> }
type ILMPolicy = {
  name: string
  phases: { hot?: ILMPhaseAction; warm?: ILMPhaseAction; cold?: ILMPhaseAction; delete?: ILMPhaseAction }
  modified_date?: string
  version?: number
  in_use_by?: any
  raw: any
}

type IndexTemplate = {
  name: string
  index_patterns: string[]
  priority?: number
  data_stream?: any
  raw: any
}

type AppPolicy = {
  id: number
  conn_id: number
  name: string
  type: string
  threshold_value: number
  threshold_unit: string
  action: string
  enabled: boolean
  last_run_at: string
  last_result: string
  created_at: string
}

type PolicyViolation = { index: string; value: string; note: string }

type ShardSetting = {
  index: string
  number_of_shards: string
  number_of_replicas: string
  routing_allocation_include?: string
  routing_allocation_exclude?: string
  routing_allocation_require?: string
  raw: any
}

const { connections, fetchConnections } = useConnections()
const toast = useToast()
const { confirm } = useConfirm()
const searchCache = useSearchCache()

const searchConnections = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

const activeTab = ref<Tab>('ilm')
const loading = ref(false)

// ── ILM ──────────────────────────────────────────────────────────────────────
const ilmPolicies = ref<ILMPolicy[]>([])
const ilmEditName = ref('')
const ilmEditJson = ref('')
const ilmEditing = ref<ILMPolicy | null>(null)
const ilmShowEditor = ref(false)

// ── Templates ─────────────────────────────────────────────────────────────────
const templates = ref<IndexTemplate[]>([])
const tplEditName = ref('')
const tplEditJson = ref('')
const tplEditing = ref<IndexTemplate | null>(null)
const tplShowEditor = ref(false)

// ── App Policies ──────────────────────────────────────────────────────────────
const appPolicies = ref<AppPolicy[]>([])
const appPolicyForm = ref<Partial<AppPolicy>>({})
const appPolicyEditing = ref<AppPolicy | null>(null)
const appPolicyShowForm = ref(false)
const runResults = ref<Record<number, { violations: PolicyViolation[]; summary: string }>>({})

// ── Shard Rules ───────────────────────────────────────────────────────────────
const shardSettings = ref<ShardSetting[]>([])
const shardFilterIndex = ref('')
const shardEditing = ref<ShardSetting | null>(null)
const shardEditJson = ref('')
const shardShowEditor = ref(false)

const filteredShards = computed(() => {
  const q = shardFilterIndex.value.trim().toLowerCase()
  if (!q) return shardSettings.value
  return shardSettings.value.filter(s => s.index.toLowerCase().includes(q))
})

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConnections.value.length === 1) {
    emit('set-conn', searchConnections.value[0].id)
    return
  }
  if (isSearch.value) await loadTab(activeTab.value)
})

watch(() => props.activeConnId, async () => {
  resetAll()
  if (isSearch.value) await loadTab(activeTab.value)
})

watch(activeTab, async (tab) => {
  if (isSearch.value) await loadTab(tab)
})

async function loadTab(tab: Tab, force = false) {
  switch (tab) {
    case 'ilm': return loadILM(force)
    case 'templates': return loadTemplates(force)
    case 'app-policies': return loadAppPolicies(force)
    case 'shard-rules': return loadShardSettings(force)
  }
}

// ── ILM ──────────────────────────────────────────────────────────────────────

function parseILM(data: Record<string, any>): ILMPolicy[] {
  return Object.entries(data).map(([name, v]) => ({
    name,
    phases: v.policy?.phases ?? {},
    modified_date: v.modified_date,
    version: v.version,
    in_use_by: v.in_use_by,
    raw: v,
  }))
}

async function loadILM(force = false) {
  if (!activeConn.value) return
  const id = activeConn.value.id
  if (!force) {
    const cached = searchCache.get<Record<string, any>>(id, 'ilm-policies')
    if (cached) { ilmPolicies.value = parseILM(cached); return }
  }
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${id}/search/ilm-policies`)
    searchCache.set(id, 'ilm-policies', data, 'policies')
    ilmPolicies.value = parseILM(data)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load ILM policies')
  } finally {
    loading.value = false
  }
}

function openNewILM() {
  ilmEditing.value = null
  ilmEditName.value = ''
  ilmEditJson.value = JSON.stringify({
    policy: {
      phases: {
        hot: { min_age: '0ms', actions: { rollover: { max_size: '50gb', max_age: '30d' } } },
        delete: { min_age: '90d', actions: { delete: {} } },
      },
    },
  }, null, 2)
  ilmShowEditor.value = true
}

function openEditILM(p: ILMPolicy) {
  ilmEditing.value = p
  ilmEditName.value = p.name
  ilmEditJson.value = JSON.stringify(p.raw, null, 2)
  ilmShowEditor.value = true
}

async function saveILM() {
  if (!activeConn.value || !ilmEditName.value.trim()) return
  loading.value = true
  try {
    const body = JSON.parse(ilmEditJson.value)
    await axios.put(`/api/connections/${activeConn.value.id}/search/ilm-policy?name=${encodeURIComponent(ilmEditName.value.trim())}`, body)
    toast.success('ILM policy saved')
    searchCache.invalidate(activeConn.value.id, 'ilm-policies')
    ilmShowEditor.value = false
    await loadILM(true)
  } catch (e: any) {
    toast.error(e instanceof SyntaxError ? 'JSON is invalid' : e?.response?.data?.error ?? 'Save failed')
  } finally {
    loading.value = false
  }
}

async function deleteILM(name: string) {
  const ok = await confirm(`Delete ILM policy "${name}"?`, 'Delete ILM Policy')
  if (!ok) return
  loading.value = true
  try {
    await axios.delete(`/api/connections/${activeConn.value!.id}/search/ilm-policy?name=${encodeURIComponent(name)}`)
    toast.success('ILM policy deleted')
    searchCache.invalidate(activeConn.value!.id, 'ilm-policies')
    await loadILM(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Delete failed')
  } finally {
    loading.value = false
  }
}

// ── Templates ─────────────────────────────────────────────────────────────────

function parseTemplates(data: any): IndexTemplate[] {
  const list: any[] = data?.index_templates ?? []
  return list.map((t: any) => ({
    name: t.name,
    index_patterns: t.index_template?.index_patterns ?? [],
    priority: t.index_template?.priority,
    data_stream: t.index_template?.data_stream,
    raw: t.index_template ?? t,
  }))
}

async function loadTemplates(force = false) {
  if (!activeConn.value) return
  const id = activeConn.value.id
  if (!force) {
    const cached = searchCache.get<any>(id, 'templates')
    if (cached) { templates.value = parseTemplates(cached); return }
  }
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${id}/search/templates`)
    searchCache.set(id, 'templates', data, 'policies')
    templates.value = parseTemplates(data)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load templates')
  } finally {
    loading.value = false
  }
}

function openNewTemplate() {
  tplEditing.value = null
  tplEditName.value = ''
  tplEditJson.value = JSON.stringify({
    index_patterns: ['logs-*'],
    priority: 100,
    template: {
      settings: { number_of_shards: 1, number_of_replicas: 1 },
      mappings: { properties: {} },
    },
  }, null, 2)
  tplShowEditor.value = true
}

function openEditTemplate(t: IndexTemplate) {
  tplEditing.value = t
  tplEditName.value = t.name
  tplEditJson.value = JSON.stringify(t.raw, null, 2)
  tplShowEditor.value = true
}

async function saveTemplate() {
  if (!activeConn.value || !tplEditName.value.trim()) return
  loading.value = true
  try {
    const body = JSON.parse(tplEditJson.value)
    await axios.put(`/api/connections/${activeConn.value.id}/search/template?name=${encodeURIComponent(tplEditName.value.trim())}`, body)
    toast.success('Template saved')
    searchCache.invalidate(activeConn.value.id, 'templates')
    tplShowEditor.value = false
    await loadTemplates(true)
  } catch (e: any) {
    toast.error(e instanceof SyntaxError ? 'JSON is invalid' : e?.response?.data?.error ?? 'Save failed')
  } finally {
    loading.value = false
  }
}

async function deleteTemplate(name: string) {
  const ok = await confirm(`Delete index template "${name}"?`, 'Delete Template')
  if (!ok) return
  loading.value = true
  try {
    await axios.delete(`/api/connections/${activeConn.value!.id}/search/template?name=${encodeURIComponent(name)}`)
    toast.success('Template deleted')
    searchCache.invalidate(activeConn.value!.id, 'templates')
    await loadTemplates(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Delete failed')
  } finally {
    loading.value = false
  }
}

// ── App Policies ──────────────────────────────────────────────────────────────

async function loadAppPolicies(force = false) {
  if (!activeConn.value) return
  const id = activeConn.value.id
  if (!force) {
    const cached = searchCache.get<AppPolicy[]>(id, 'app-policies')
    if (cached) { appPolicies.value = cached; return }
  }
  loading.value = true
  try {
    const { data } = await axios.get<AppPolicy[]>(`/api/search-app-policies?conn_id=${id}`)
    searchCache.set(id, 'app-policies', data, 'appPolicies')
    appPolicies.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load policies')
  } finally {
    loading.value = false
  }
}

function openNewAppPolicy() {
  appPolicyEditing.value = null
  appPolicyForm.value = {
    conn_id: activeConn.value?.id ?? 0,
    name: '',
    type: 'size_alert',
    threshold_value: 50,
    threshold_unit: 'GB',
    action: 'alert',
    enabled: true,
  }
  appPolicyShowForm.value = true
}

function openEditAppPolicy(p: AppPolicy) {
  appPolicyEditing.value = p
  appPolicyForm.value = { ...p }
  appPolicyShowForm.value = true
}

async function saveAppPolicy() {
  if (!activeConn.value || !appPolicyForm.value.name?.trim()) return
  loading.value = true
  try {
    if (appPolicyEditing.value) {
      await axios.put(`/api/search-app-policies/${appPolicyEditing.value.id}`, appPolicyForm.value)
    } else {
      appPolicyForm.value.conn_id = activeConn.value.id
      await axios.post('/api/search-app-policies', appPolicyForm.value)
    }
    toast.success('Policy saved')
    searchCache.invalidate(activeConn.value.id, 'app-policies')
    appPolicyShowForm.value = false
    await loadAppPolicies(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Save failed')
  } finally {
    loading.value = false
  }
}

async function deleteAppPolicy(p: AppPolicy) {
  const ok = await confirm(`Delete policy "${p.name}"?`, 'Delete Policy')
  if (!ok) return
  loading.value = true
  try {
    await axios.delete(`/api/search-app-policies/${p.id}`)
    toast.success('Policy deleted')
    searchCache.invalidate(p.conn_id, 'app-policies')
    await loadAppPolicies(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Delete failed')
  } finally {
    loading.value = false
  }
}

async function runAppPolicy(p: AppPolicy) {
  loading.value = true
  try {
    const { data } = await axios.post(`/api/search-app-policies/${p.id}/run`)
    runResults.value[p.id] = { violations: data.violations ?? [], summary: data.summary ?? '' }
    toast.success(data.summary ?? 'Policy evaluated')
    searchCache.invalidate(p.conn_id, 'app-policies')
    await loadAppPolicies(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Run failed')
  } finally {
    loading.value = false
  }
}

// ── Shard Rules ───────────────────────────────────────────────────────────────

function parseShardSettings(data: Record<string, any>): ShardSetting[] {
  return Object.entries(data).map(([index, v]) => {
    const settings = v?.settings?.index ?? {}
    return {
      index,
      number_of_shards: settings.number_of_shards ?? '-',
      number_of_replicas: settings.number_of_replicas ?? '-',
      routing_allocation_include: settings.routing?.allocation?.include?._tier_preference ?? '',
      routing_allocation_exclude: settings.routing?.allocation?.exclude?.toString() ?? '',
      routing_allocation_require: settings.routing?.allocation?.require?.toString() ?? '',
      raw: v,
    }
  })
}

async function loadShardSettings(force = false) {
  if (!activeConn.value) return
  const id = activeConn.value.id
  if (!force) {
    const cached = searchCache.get<Record<string, any>>(id, 'index-settings:_all')
    if (cached) { shardSettings.value = parseShardSettings(cached); return }
  }
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${id}/search/index-settings`)
    searchCache.set(id, 'index-settings:_all', data, 'settings')
    shardSettings.value = parseShardSettings(data)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load index settings')
  } finally {
    loading.value = false
  }
}

function openEditShardSettings(s: ShardSetting) {
  shardEditing.value = s
  shardEditJson.value = JSON.stringify({
    index: {
      number_of_replicas: s.number_of_replicas === '-' ? '1' : s.number_of_replicas,
    },
  }, null, 2)
  shardShowEditor.value = true
}

async function saveShardSettings() {
  if (!activeConn.value || !shardEditing.value) return
  loading.value = true
  try {
    const body = JSON.parse(shardEditJson.value)
    await axios.put(`/api/connections/${activeConn.value.id}/search/index-settings?index=${encodeURIComponent(shardEditing.value.index)}`, body)
    toast.success('Settings updated')
    searchCache.invalidate(activeConn.value!.id, 'index-settings:_all', `index-settings:${shardEditing.value!.index}`)
    shardShowEditor.value = false
    await loadShardSettings(true)
  } catch (e: any) {
    toast.error(e instanceof SyntaxError ? 'JSON is invalid' : e?.response?.data?.error ?? 'Update failed')
  } finally {
    loading.value = false
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function resetAll() {
  ilmPolicies.value = []
  templates.value = []
  appPolicies.value = []
  shardSettings.value = []
  runResults.value = {}
  ilmShowEditor.value = false
  tplShowEditor.value = false
  appPolicyShowForm.value = false
  shardShowEditor.value = false
}

function formatJSON(v: any) { return JSON.stringify(v, null, 2) }

function policyTypeLabel(type: string) {
  switch (type) {
    case 'size_alert': return 'Size Alert'
    case 'auto_delete_size': return 'Auto Delete (Size)'
    case 'auto_delete_age': return 'Auto Delete (Age)'
    default: return type
  }
}

function policyThresholdDisplay(p: AppPolicy) {
  if (p.type === 'auto_delete_age') return `${p.threshold_value} days`
  return `${p.threshold_value} ${p.threshold_unit}`
}

function ilmPhaseKeys(phases: ILMPolicy['phases']) {
  return Object.keys(phases).filter(k => phases[k as keyof typeof phases])
}

function ilmPhaseColor(phase: string) {
  if (phase === 'hot') return 'var(--warning)'
  if (phase === 'warm') return '#f97316'
  if (phase === 'cold') return '#60a5fa'
  if (phase === 'delete') return 'var(--danger)'
  return 'var(--text-muted)'
}

function formatDate(v: string) {
  if (!v) return '-'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}
</script>

<template>
  <div class="page-shell sp-root">
    <header class="sp-topbar">
      <div class="sp-title">
        <span class="sp-logo">{{ activeConn?.driver === 'opensearch' ? 'OS' : 'ES' }}</span>
        <div>
          <h1>Search Policies</h1>
          <p>{{ activeConn ? activeConn.name : 'No Elasticsearch or OpenSearch connection selected' }}</p>
        </div>
      </div>
      <div class="sp-actions">
        <select class="base-input sp-select" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Select search cluster</option>
          <option v-for="conn in searchConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || loading" @click="loadTab(activeTab, true)">Refresh</button>
      </div>
    </header>

    <section v-if="!isSearch" class="sp-empty">
      <h2>Select a search connection</h2>
      <p>Connect to an Elasticsearch or OpenSearch cluster to manage policies, templates, and shard settings.</p>
    </section>

    <template v-else>
      <div class="sp-tabs">
        <button :class="{ active: activeTab === 'ilm' }" @click="activeTab = 'ilm'">ILM Policies</button>
        <button :class="{ active: activeTab === 'templates' }" @click="activeTab = 'templates'">Index Templates</button>
        <button :class="{ active: activeTab === 'app-policies' }" @click="activeTab = 'app-policies'">App Policies</button>
        <button :class="{ active: activeTab === 'shard-rules' }" @click="activeTab = 'shard-rules'">Shard Rules</button>
      </div>

      <!-- ── ILM Policies ─────────────────────────────────────────────────── -->
      <section v-if="activeTab === 'ilm'" class="sp-section">
        <div class="sp-section-head">
          <div>
            <div class="sp-section-title">Index Lifecycle Management</div>
            <div class="sp-muted">{{ ilmPolicies.length }} policies</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openNewILM">+ New Policy</button>
        </div>

        <div v-if="ilmShowEditor" class="sp-editor-panel">
          <div class="sp-editor-head">
            <div class="sp-editor-title">{{ ilmEditing ? 'Edit' : 'New' }} ILM Policy</div>
            <button class="icon-btn" @click="ilmShowEditor = false">✕</button>
          </div>
          <label class="sp-field">
            <span>Policy Name</span>
            <input v-model="ilmEditName" class="base-input" :disabled="!!ilmEditing" placeholder="my-policy" />
          </label>
          <textarea v-model="ilmEditJson" class="base-input sp-editor-textarea" spellcheck="false" />
          <div class="sp-editor-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="ilmShowEditor = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading" @click="saveILM">Save</button>
          </div>
        </div>

        <div class="sp-table-wrap">
          <table class="sp-table">
            <thead><tr><th>Name</th><th>Phases</th><th>Version</th><th>Modified</th><th></th></tr></thead>
            <tbody>
              <tr v-for="p in ilmPolicies" :key="p.name">
                <td class="sp-mono">{{ p.name }}</td>
                <td>
                  <span v-for="phase in ilmPhaseKeys(p.phases)" :key="phase" class="sp-phase-badge" :style="{ color: ilmPhaseColor(phase), borderColor: ilmPhaseColor(phase) }">{{ phase }}</span>
                </td>
                <td class="sp-muted">{{ p.version ?? '-' }}</td>
                <td class="sp-muted">{{ formatDate(p.modified_date ?? '') }}</td>
                <td class="sp-actions-cell">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditILM(p)">Edit</button>
                  <button class="base-btn base-btn--danger base-btn--sm" :disabled="loading" @click="deleteILM(p.name)">Delete</button>
                </td>
              </tr>
              <tr v-if="!ilmPolicies.length"><td colspan="5" class="sp-empty-row">No ILM policies found.</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ── Index Templates ─────────────────────────────────────────────── -->
      <section v-if="activeTab === 'templates'" class="sp-section">
        <div class="sp-section-head">
          <div>
            <div class="sp-section-title">Index Templates</div>
            <div class="sp-muted">{{ templates.length }} templates</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openNewTemplate">+ New Template</button>
        </div>

        <div v-if="tplShowEditor" class="sp-editor-panel">
          <div class="sp-editor-head">
            <div class="sp-editor-title">{{ tplEditing ? 'Edit' : 'New' }} Index Template</div>
            <button class="icon-btn" @click="tplShowEditor = false">✕</button>
          </div>
          <label class="sp-field">
            <span>Template Name</span>
            <input v-model="tplEditName" class="base-input" :disabled="!!tplEditing" placeholder="my-template" />
          </label>
          <textarea v-model="tplEditJson" class="base-input sp-editor-textarea" spellcheck="false" />
          <div class="sp-editor-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="tplShowEditor = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading" @click="saveTemplate">Save</button>
          </div>
        </div>

        <div class="sp-table-wrap">
          <table class="sp-table">
            <thead><tr><th>Name</th><th>Patterns</th><th>Priority</th><th>Type</th><th></th></tr></thead>
            <tbody>
              <tr v-for="t in templates" :key="t.name">
                <td class="sp-mono">{{ t.name }}</td>
                <td><span v-for="p in t.index_patterns" :key="p" class="sp-tag">{{ p }}</span></td>
                <td class="sp-muted">{{ t.priority ?? '-' }}</td>
                <td class="sp-muted">{{ t.data_stream != null ? 'Data stream' : 'Index' }}</td>
                <td class="sp-actions-cell">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditTemplate(t)">Edit</button>
                  <button class="base-btn base-btn--danger base-btn--sm" :disabled="loading" @click="deleteTemplate(t.name)">Delete</button>
                </td>
              </tr>
              <tr v-if="!templates.length"><td colspan="5" class="sp-empty-row">No index templates found.</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ── App Policies ────────────────────────────────────────────────── -->
      <section v-if="activeTab === 'app-policies'" class="sp-section">
        <div class="sp-section-head">
          <div>
            <div class="sp-section-title">App-level Policies</div>
            <div class="sp-muted">Custom rules evaluated by NIAS</div>
          </div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openNewAppPolicy">+ New Policy</button>
        </div>

        <div v-if="appPolicyShowForm" class="sp-editor-panel">
          <div class="sp-editor-head">
            <div class="sp-editor-title">{{ appPolicyEditing ? 'Edit' : 'New' }} App Policy</div>
            <button class="icon-btn" @click="appPolicyShowForm = false">✕</button>
          </div>
          <div class="sp-form-grid">
            <label class="sp-field">
              <span>Name</span>
              <input v-model="appPolicyForm.name" class="base-input" placeholder="Bloated index alert" />
            </label>
            <label class="sp-field">
              <span>Type</span>
              <select v-model="appPolicyForm.type" class="base-input">
                <option value="size_alert">Size Alert</option>
                <option value="auto_delete_size">Auto Delete (by size)</option>
                <option value="auto_delete_age">Auto Delete (by age)</option>
              </select>
            </label>
            <label class="sp-field">
              <span>Threshold</span>
              <input v-model.number="appPolicyForm.threshold_value" class="base-input" type="number" min="0" step="any" />
            </label>
            <label class="sp-field">
              <span>Unit</span>
              <select v-model="appPolicyForm.threshold_unit" class="base-input" :disabled="appPolicyForm.type === 'auto_delete_age'">
                <option v-if="appPolicyForm.type === 'auto_delete_age'" value="days">days</option>
                <option value="MB">MB</option>
                <option value="GB">GB</option>
                <option value="TB">TB</option>
              </select>
            </label>
            <label class="sp-field sp-field--toggle">
              <input v-model="appPolicyForm.enabled" type="checkbox" />
              <span>Enabled</span>
            </label>
          </div>
          <div class="sp-editor-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="appPolicyShowForm = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading" @click="saveAppPolicy">Save</button>
          </div>
        </div>

        <div class="sp-policy-list">
          <div v-for="p in appPolicies" :key="p.id" class="sp-policy-card">
            <div class="sp-policy-card__head">
              <div class="sp-policy-card__meta">
                <strong>{{ p.name }}</strong>
                <span class="sp-tag sp-tag--type">{{ policyTypeLabel(p.type) }}</span>
                <span class="sp-tag" :class="p.enabled ? 'sp-tag--enabled' : 'sp-tag--disabled'">{{ p.enabled ? 'Enabled' : 'Disabled' }}</span>
              </div>
              <div class="sp-policy-card__actions">
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="runAppPolicy(p)">Run</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditAppPolicy(p)">Edit</button>
                <button class="base-btn base-btn--danger base-btn--sm" :disabled="loading" @click="deleteAppPolicy(p)">Delete</button>
              </div>
            </div>
            <div class="sp-policy-card__body">
              <span class="sp-muted">Threshold: <strong>{{ policyThresholdDisplay(p) }}</strong></span>
              <span class="sp-muted">Last run: <strong>{{ formatDate(p.last_run_at) }}</strong></span>
            </div>
            <div v-if="runResults[p.id]" class="sp-policy-result">
              <div class="sp-policy-result__summary">{{ runResults[p.id].summary }}</div>
              <div v-if="runResults[p.id].violations.length" class="sp-violation-list">
                <div v-for="v in runResults[p.id].violations" :key="v.index" class="sp-violation">
                  <span class="sp-mono">{{ v.index }}</span>
                  <span class="sp-muted">{{ v.note }}</span>
                </div>
              </div>
              <div v-else class="sp-muted">No violations.</div>
            </div>
          </div>
          <div v-if="!appPolicies.length" class="sp-empty-row">No app policies yet. Create one to start monitoring indices.</div>
        </div>
      </section>

      <!-- ── Shard Rules ─────────────────────────────────────────────────── -->
      <section v-if="activeTab === 'shard-rules'" class="sp-section">
        <div class="sp-section-head">
          <div>
            <div class="sp-section-title">Shard & Allocation Rules</div>
            <div class="sp-muted">{{ shardSettings.length }} indices</div>
          </div>
          <input v-model="shardFilterIndex" class="base-input sp-shard-filter" placeholder="Filter indices..." />
        </div>

        <div v-if="shardShowEditor" class="sp-editor-panel">
          <div class="sp-editor-head">
            <div class="sp-editor-title">Update Settings: <span class="sp-mono">{{ shardEditing?.index }}</span></div>
            <button class="icon-btn" @click="shardShowEditor = false">✕</button>
          </div>
          <div class="sp-muted" style="font-size:11.5px;padding-bottom:4px">Only dynamic settings (e.g. <code>number_of_replicas</code>, routing allocation) can be updated on live indices.</div>
          <textarea v-model="shardEditJson" class="base-input sp-editor-textarea" spellcheck="false" />
          <div class="sp-editor-footer">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="shardShowEditor = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading" @click="saveShardSettings">Save</button>
          </div>
        </div>

        <div class="sp-table-wrap">
          <table class="sp-table">
            <thead><tr><th>Index</th><th>Shards</th><th>Replicas</th><th>Tier preference</th><th></th></tr></thead>
            <tbody>
              <tr v-for="s in filteredShards" :key="s.index">
                <td class="sp-mono">{{ s.index }}</td>
                <td class="sp-muted">{{ s.number_of_shards }}</td>
                <td class="sp-muted">{{ s.number_of_replicas }}</td>
                <td class="sp-muted">{{ s.routing_allocation_include || '-' }}</td>
                <td class="sp-actions-cell">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditShardSettings(s)">Edit</button>
                </td>
              </tr>
              <tr v-if="!filteredShards.length"><td colspan="5" class="sp-empty-row">No indices found.</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.sp-root { background: var(--bg-body); padding: 18px; gap: 14px; }
.sp-topbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.sp-title { display: flex; align-items: center; gap: 12px; }
.sp-title h1 { margin: 0; font-size: 20px; color: var(--text-primary); }
.sp-title p { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.sp-logo { width: 38px; height: 38px; border-radius: 8px; background: #00bfb3; color: #fff; display: grid; place-items: center; font-weight: 800; font-size: 12px; flex-shrink: 0; }
.sp-actions { display: flex; align-items: center; gap: 8px; }
.sp-select { width: 240px; }
.sp-empty { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 36px; text-align: center; color: var(--text-muted); }
.sp-empty h2 { margin: 0 0 6px; color: var(--text-primary); font-size: 16px; }

.sp-tabs { display: flex; border-bottom: 1px solid var(--border); gap: 0; }
.sp-tabs button { border: 0; background: transparent; color: var(--text-muted); padding: 10px 16px; cursor: pointer; font-size: 13px; font-weight: 500; border-bottom: 2px solid transparent; margin-bottom: -1px; transition: color 0.12s, border-color 0.12s; }
.sp-tabs button:hover { color: var(--text-primary); }
.sp-tabs button.active { color: #00bfb3; border-bottom-color: #00bfb3; }

.sp-section { display: flex; flex-direction: column; gap: 14px; flex: 1; min-height: 0; }
.sp-section-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.sp-section-title { font-weight: 700; font-size: 14px; color: var(--text-primary); }
.sp-muted { color: var(--text-muted); font-size: 11.5px; }

.sp-editor-panel { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 14px; display: flex; flex-direction: column; gap: 10px; }
.sp-editor-head { display: flex; align-items: center; justify-content: space-between; }
.sp-editor-title { font-weight: 700; font-size: 13px; color: var(--text-primary); }
.sp-editor-footer { display: flex; justify-content: flex-end; gap: 8px; }
.sp-editor-textarea { min-height: 280px; resize: vertical; font-family: var(--mono); font-size: 12px; line-height: 1.6; }
.sp-field { display: flex; flex-direction: column; gap: 4px; }
.sp-field span { font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.sp-field--toggle { flex-direction: row; align-items: center; gap: 8px; }
.sp-field--toggle span { text-transform: none; font-weight: 500; font-size: 13px; color: var(--text-secondary); }
.sp-form-grid { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr auto; gap: 10px; align-items: end; }

.sp-table-wrap { overflow: auto; border: 1px solid var(--border); border-radius: 8px; }
.sp-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.sp-table th { background: var(--bg-elevated); color: var(--text-muted); font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; padding: 9px 12px; text-align: left; border-bottom: 1px solid var(--border); white-space: nowrap; }
.sp-table td { padding: 10px 12px; border-bottom: 1px solid var(--border); color: var(--text-secondary); vertical-align: middle; }
.sp-table tr:last-child td { border-bottom: 0; }
.sp-table tr:hover td { background: var(--bg-hover); }
.sp-actions-cell { text-align: right; white-space: nowrap; display: flex; gap: 6px; justify-content: flex-end; align-items: center; }
.sp-empty-row { text-align: center; color: var(--text-muted); padding: 24px !important; }

.sp-mono { font-family: var(--mono); font-size: 12px; color: var(--text-primary); }
.sp-tag { display: inline-flex; align-items: center; font-size: 10px; font-weight: 700; padding: 2px 7px; border-radius: 4px; border: 1px solid var(--border); background: var(--bg-elevated); color: var(--text-muted); margin-right: 4px; }
.sp-tag--enabled { color: var(--success); border-color: color-mix(in srgb, var(--success) 30%, transparent); background: color-mix(in srgb, var(--success) 10%, transparent); }
.sp-tag--disabled { color: var(--text-muted); }
.sp-tag--type { color: #00bfb3; border-color: rgba(0,191,179,0.3); background: rgba(0,191,179,0.08); }
.sp-phase-badge { display: inline-flex; align-items: center; font-size: 10px; font-weight: 700; padding: 2px 7px; border-radius: 4px; border: 1px solid; margin-right: 4px; background: transparent; text-transform: uppercase; }

.sp-policy-list { display: flex; flex-direction: column; gap: 10px; }
.sp-policy-card { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 14px; display: flex; flex-direction: column; gap: 8px; }
.sp-policy-card__head { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
.sp-policy-card__meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sp-policy-card__meta strong { font-size: 13.5px; color: var(--text-primary); }
.sp-policy-card__actions { display: flex; gap: 6px; }
.sp-policy-card__body { display: flex; gap: 20px; flex-wrap: wrap; }
.sp-policy-result { background: var(--bg-body); border: 1px solid var(--border); border-radius: 6px; padding: 10px; display: flex; flex-direction: column; gap: 6px; }
.sp-policy-result__summary { font-size: 12px; font-weight: 600; color: var(--text-primary); }
.sp-violation-list { display: flex; flex-direction: column; gap: 4px; max-height: 200px; overflow: auto; }
.sp-violation { display: flex; gap: 12px; align-items: baseline; font-size: 11.5px; }

.sp-shard-filter { width: 240px; height: 32px; }

@media (max-width: 900px) {
  .sp-topbar, .sp-actions { flex-direction: column; align-items: stretch; }
  .sp-form-grid { grid-template-columns: 1fr 1fr; }
  .sp-select, .sp-shard-filter { width: 100%; }
}
</style>
