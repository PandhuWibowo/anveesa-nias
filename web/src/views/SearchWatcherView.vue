<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

interface WatchDoc {
  id: string
  status?: { state?: { active: boolean }; last_checked?: string; last_met_condition?: string }
  trigger?: any
  condition?: any
  actions?: any
  raw: any
}

interface HistoryEntry {
  _id: string
  _source: {
    watch_id: string
    state: string
    result?: { condition?: { met: boolean }; execution_time?: string }
    trigger_event?: { triggered_time?: string }
  }
}

const { connections, fetchConnections } = useConnections()
const toast = useToast()
const { confirm } = useConfirm()

const searchConnections = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

type Tab = 'watches' | 'history'

const activeTab = ref<Tab>('watches')
const loading = ref(false)
const watches = ref<WatchDoc[]>([])
const history = ref<HistoryEntry[]>([])
const watcherAvailable = ref<boolean | null>(null)
const statsInfo = ref<any>(null)

// Editor modal
const showEditor = ref(false)
const editorId = ref('')
const editorBody = ref('')
const editorIsNew = ref(false)
const editorLoading = ref(false)

// Execute result modal
const showExecResult = ref(false)
const execResult = ref<any>(null)
const execLoading = ref(false)

// History filter
const historyWatchFilter = ref('')

const filteredHistory = computed(() => {
  const q = historyWatchFilter.value.trim().toLowerCase()
  if (!q) return history.value
  return history.value.filter(h => h._source?.watch_id?.toLowerCase().includes(q))
})

const DEFAULT_WATCH = JSON.stringify({
  trigger: { schedule: { interval: '1h' } },
  input: {
    search: {
      request: {
        indices: ['logs-*'],
        body: {
          query: { bool: { filter: [{ range: { '@timestamp': { gte: 'now-1h' } } }] } },
          aggs: { error_count: { filter: { term: { 'log.level': 'error' } } } },
        },
      },
    },
  },
  condition: { compare: { 'ctx.payload.aggregations.error_count.doc_count': { gte: 10 } } },
  actions: {
    log_error: {
      logging: { text: 'Found {{ctx.payload.aggregations.error_count.doc_count}} errors in the last hour' },
    },
  },
}, null, 2)

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConnections.value.length === 1) {
    emit('set-conn', searchConnections.value[0].id)
    return
  }
  if (isSearch.value) await loadWatches()
})

watch(() => props.activeConnId, async () => {
  watches.value = []
  history.value = []
  watcherAvailable.value = null
  if (isSearch.value) await loadWatches()
})

watch(activeTab, async (tab) => {
  if (!isSearch.value) return
  if (tab === 'history' && !history.value.length) await loadHistory()
})

async function loadWatches() {
  if (!activeConn.value) return
  loading.value = true
  try {
    // Check watcher availability
    try {
      const { data } = await axios.get(`/api/connections/${activeConn.value.id}/search/watcher-stats`)
      statsInfo.value = data
      watcherAvailable.value = true
    } catch {
      watcherAvailable.value = false
    }

    if (!watcherAvailable.value) return

    const { data } = await axios.get(`/api/connections/${activeConn.value.id}/search/watches`)
    const rawHits: any[] = data?.hits?.hits ?? []
    watches.value = rawHits.map((h: any) => ({
      id: h._id,
      status: h._source?.status,
      trigger: h._source?.trigger,
      condition: h._source?.condition,
      actions: h._source?.actions,
      raw: h._source,
    }))
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load watches')
  } finally {
    loading.value = false
  }
}

async function loadHistory() {
  if (!activeConn.value) return
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${activeConn.value.id}/search/watch-history`)
    history.value = data?.hits?.hits ?? []
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load watch history')
  } finally {
    loading.value = false
  }
}

function openNewWatch() {
  editorIsNew.value = true
  editorId.value = ''
  editorBody.value = DEFAULT_WATCH
  showEditor.value = true
}

async function openEditWatch(w: WatchDoc) {
  editorIsNew.value = false
  editorId.value = w.id
  editorLoading.value = true
  showEditor.value = true
  try {
    const { data } = await axios.get(`/api/connections/${activeConn.value!.id}/search/watch`, {
      params: { id: w.id },
    })
    const watchBody = data?.watch ?? data
    editorBody.value = JSON.stringify(watchBody, null, 2)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load watch')
  } finally {
    editorLoading.value = false
  }
}

async function saveWatch() {
  if (!editorId.value.trim()) { toast.error('Watch ID is required'); return }
  let body: any
  try { body = JSON.parse(editorBody.value) } catch { toast.error('Invalid JSON'); return }
  editorLoading.value = true
  try {
    await axios.put(`/api/connections/${activeConn.value!.id}/search/watch`, body, {
      params: { id: editorId.value.trim() },
    })
    toast.success(`Watch "${editorId.value}" saved`)
    showEditor.value = false
    await loadWatches()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to save watch')
  } finally {
    editorLoading.value = false
  }
}

async function deleteWatch(w: WatchDoc) {
  const ok = await confirm(`Delete watch "${w.id}"?`, 'Delete Watch')
  if (!ok) return
  try {
    await axios.delete(`/api/connections/${activeConn.value!.id}/search/watch`, { params: { id: w.id } })
    toast.success(`Watch "${w.id}" deleted`)
    await loadWatches()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to delete watch')
  }
}

async function executeWatch(w: WatchDoc, force = false) {
  execLoading.value = true
  showExecResult.value = true
  execResult.value = null
  try {
    const { data } = await axios.post(`/api/connections/${activeConn.value!.id}/search/watch-execute`, null, {
      params: { id: w.id, force: force ? 'true' : 'false' },
    })
    execResult.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Execute failed')
    showExecResult.value = false
  } finally {
    execLoading.value = false
  }
}

async function toggleActive(w: WatchDoc) {
  const isActive = w.status?.state?.active ?? true
  const endpoint = isActive ? 'watch-deactivate' : 'watch-activate'
  try {
    await axios.put(`/api/connections/${activeConn.value!.id}/search/${endpoint}`, null, { params: { id: w.id } })
    toast.success(`Watch "${w.id}" ${isActive ? 'deactivated' : 'activated'}`)
    await loadWatches()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Toggle failed')
  }
}

function watchScheduleLabel(w: WatchDoc): string {
  const sched = w.trigger?.schedule
  if (!sched) return '-'
  if (sched.interval) return `every ${sched.interval}`
  if (sched.cron) return `cron: ${sched.cron}`
  if (sched.hourly) return 'hourly'
  if (sched.daily) return 'daily'
  if (sched.weekly) return 'weekly'
  return JSON.stringify(sched)
}

function watchConditionLabel(w: WatchDoc): string {
  const cond = w.condition
  if (!cond) return '-'
  if (cond.always) return 'always'
  if (cond.never) return 'never'
  if (cond.compare) return `compare: ${Object.keys(cond.compare)[0] ?? ''}`
  if (cond.script) return 'script'
  return Object.keys(cond)[0] ?? '-'
}

function watchActionsLabel(w: WatchDoc): string {
  if (!w.actions) return '-'
  return Object.keys(w.actions).join(', ')
}

function histStateClass(state: string): string {
  const s = (state || '').toLowerCase()
  if (s === 'executed') return 'ws-executed'
  if (s === 'failed') return 'ws-failed'
  if (s === 'throttled') return 'ws-throttled'
  return 'ws-other'
}

function formatTs(v: string | undefined): string {
  if (!v) return '-'
  const d = new Date(v)
  return isNaN(d.getTime()) ? v : d.toLocaleString()
}

function formatJSON(v: any): string {
  return JSON.stringify(v, null, 2)
}
</script>

<template>
  <div class="sw-root page-shell">
    <header class="sw-topbar">
      <div class="sw-title">
        <span class="sw-logo">{{ activeConn?.driver === 'opensearch' ? 'OS' : 'ES' }}</span>
        <div>
          <h1>Watcher</h1>
          <p>{{ activeConn ? activeConn.name : 'No connection selected' }}</p>
        </div>
      </div>
      <div class="sw-actions">
        <select class="base-input sw-select" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Select cluster</option>
          <option v-for="c in searchConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <button v-if="isSearch && watcherAvailable" class="base-btn base-btn--primary base-btn--sm" @click="openNewWatch">+ New Watch</button>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || loading" @click="loadWatches">Refresh</button>
      </div>
    </header>

    <section v-if="!isSearch" class="sw-empty">
      <h2>Select a search connection</h2>
      <p>Connect to an Elasticsearch cluster with the Watcher feature enabled to manage alert watches.</p>
    </section>

    <template v-else>
      <!-- Watcher not available -->
      <div v-if="watcherAvailable === false" class="sw-unavailable">
        <strong>Elasticsearch Watcher is not available</strong>
        <p>Watcher is a paid X-Pack feature. It may not be enabled on your cluster, or this may be a basic/free license.</p>
      </div>

      <template v-else-if="watcherAvailable">
        <!-- Stats -->
        <div v-if="statsInfo" class="sw-stats">
          <div class="sw-stat">
            <span>Watcher State</span>
            <strong :class="statsInfo.stats?.[0]?.watcher_state === 'started' ? 'ok' : ''">
              {{ statsInfo.stats?.[0]?.watcher_state ?? '-' }}
            </strong>
          </div>
          <div class="sw-stat">
            <span>Watches</span>
            <strong>{{ watches.length }}</strong>
          </div>
          <div class="sw-stat">
            <span>Watch Count (cluster)</span>
            <strong>{{ statsInfo.stats?.[0]?.watch_count ?? '-' }}</strong>
          </div>
          <div class="sw-stat">
            <span>Queued Watches</span>
            <strong>{{ statsInfo.stats?.[0]?.execution_thread_pool?.queue_size ?? '-' }}</strong>
          </div>
        </div>

        <!-- Tabs -->
        <div class="sw-tabs">
          <button :class="{ active: activeTab === 'watches' }" @click="activeTab = 'watches'">Watches</button>
          <button :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">Execution History</button>
        </div>

        <!-- ── Watches tab ─────────────────────────────── -->
        <div v-if="activeTab === 'watches'" class="sw-panel">
          <div v-if="!watches.length && !loading" class="sw-empty-hint">
            No watches found. Create one to start monitoring your cluster.
          </div>
          <div v-else class="sw-table-wrap">
            <table class="sw-table">
              <thead>
                <tr>
                  <th>Watch ID</th>
                  <th>State</th>
                  <th>Schedule</th>
                  <th>Condition</th>
                  <th>Actions</th>
                  <th>Last Checked</th>
                  <th>Last Met</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="w in watches" :key="w.id">
                  <td class="sw-bold sw-mono">{{ w.id }}</td>
                  <td>
                    <span class="sw-badge" :class="w.status?.state?.active !== false ? 'sw-active' : 'sw-inactive'">
                      {{ w.status?.state?.active !== false ? 'active' : 'inactive' }}
                    </span>
                  </td>
                  <td class="sw-mono">{{ watchScheduleLabel(w) }}</td>
                  <td class="sw-mono">{{ watchConditionLabel(w) }}</td>
                  <td class="sw-mono">{{ watchActionsLabel(w) }}</td>
                  <td class="sw-muted">{{ formatTs(w.status?.last_checked) }}</td>
                  <td class="sw-muted">{{ formatTs(w.status?.last_met_condition) }}</td>
                  <td class="sw-actions-cell">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openEditWatch(w)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="executeWatch(w)">Simulate</button>
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="toggleActive(w)">
                      {{ w.status?.state?.active !== false ? 'Deactivate' : 'Activate' }}
                    </button>
                    <button class="base-btn base-btn--danger base-btn--xs" @click="deleteWatch(w)">Delete</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- ── History tab ─────────────────────────────── -->
        <div v-if="activeTab === 'history'" class="sw-panel">
          <div class="sw-history-filter">
            <input v-model="historyWatchFilter" class="base-input sw-hist-filter" placeholder="Filter by watch ID…" />
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="loadHistory">Refresh</button>
            <span class="sw-muted">{{ filteredHistory.length }} entries</span>
          </div>
          <div v-if="!filteredHistory.length && !loading" class="sw-empty-hint">No history entries found.</div>
          <div v-else class="sw-table-wrap">
            <table class="sw-table">
              <thead>
                <tr>
                  <th>Watch ID</th>
                  <th>State</th>
                  <th>Condition Met</th>
                  <th>Triggered At</th>
                  <th>Execution Time</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="entry in filteredHistory" :key="entry._id">
                  <td class="sw-bold sw-mono">{{ entry._source?.watch_id ?? '-' }}</td>
                  <td>
                    <span class="sw-badge" :class="histStateClass(entry._source?.state)">
                      {{ entry._source?.state ?? '-' }}
                    </span>
                  </td>
                  <td class="sw-center">
                    <span v-if="entry._source?.result?.condition?.met === true" class="sw-yes">✓</span>
                    <span v-else-if="entry._source?.result?.condition?.met === false" class="sw-no">✗</span>
                    <span v-else class="sw-muted">-</span>
                  </td>
                  <td class="sw-muted">{{ formatTs(entry._source?.trigger_event?.triggered_time) }}</td>
                  <td class="sw-muted">{{ formatTs(entry._source?.result?.execution_time) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </template>

    <!-- ── Editor Modal ───────────────────────────────── -->
    <div v-if="showEditor" class="sw-modal-overlay" @click.self="showEditor = false">
      <div class="sw-modal">
        <div class="sw-modal-head">
          <h2>{{ editorIsNew ? 'New Watch' : `Edit Watch: ${editorId}` }}</h2>
          <button class="sw-modal-close" @click="showEditor = false">✕</button>
        </div>
        <div class="sw-modal-body">
          <label class="sw-label">
            Watch ID
            <input v-model="editorId" class="base-input" :disabled="!editorIsNew" placeholder="e.g. high-error-rate" />
          </label>
          <label class="sw-label">
            Watch Definition (JSON)
            <textarea v-model="editorBody" class="base-input sw-editor-textarea" spellcheck="false" />
          </label>
        </div>
        <div class="sw-modal-foot">
          <button class="base-btn base-btn--ghost" @click="showEditor = false">Cancel</button>
          <button class="base-btn base-btn--primary" :disabled="editorLoading" @click="saveWatch">
            {{ editorLoading ? 'Saving…' : 'Save Watch' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ── Execute Result Modal ───────────────────────── -->
    <div v-if="showExecResult" class="sw-modal-overlay" @click.self="showExecResult = false">
      <div class="sw-modal sw-modal--wide">
        <div class="sw-modal-head">
          <h2>Simulation Result</h2>
          <button class="sw-modal-close" @click="showExecResult = false">✕</button>
        </div>
        <div class="sw-modal-body">
          <div v-if="execLoading" class="sw-exec-loading">Running simulation…</div>
          <div v-else-if="execResult">
            <div class="sw-exec-status">
              <span class="sw-badge" :class="execResult.watch_record?.state?.successful ? 'sw-active' : 'sw-failed-badge'">
                {{ execResult.watch_record?.state?.successful ? 'success' : 'failed' }}
              </span>
              <span class="sw-muted">Condition met: {{ execResult.watch_record?.result?.condition?.met ? 'yes' : 'no' }}</span>
            </div>
            <pre class="sw-exec-json">{{ formatJSON(execResult) }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sw-root { background: var(--bg-body); padding: 18px; gap: 14px; }
.sw-topbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.sw-title { display: flex; align-items: center; gap: 12px; }
.sw-title h1 { margin: 0; font-size: 20px; color: var(--text-primary); }
.sw-title p { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.sw-logo { width: 38px; height: 38px; border-radius: 8px; background: #f59e0b; color: #fff; display: grid; place-items: center; font-weight: 800; font-size: 10px; flex-shrink: 0; }
.sw-actions { display: flex; align-items: center; gap: 8px; }
.sw-select { width: 220px; }

.sw-empty { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 36px; text-align: center; color: var(--text-muted); }
.sw-empty h2 { margin: 0 0 6px; color: var(--text-primary); font-size: 16px; }
.sw-unavailable { border: 1px solid color-mix(in srgb, var(--warning) 40%, var(--border)); background: color-mix(in srgb, var(--warning) 8%, var(--bg-elevated)); border-radius: 8px; padding: 20px 24px; color: var(--text-primary); }
.sw-unavailable strong { display: block; margin-bottom: 6px; color: var(--warning); }
.sw-unavailable p { margin: 0; color: var(--text-muted); font-size: 13px; }

/* Stats */
.sw-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.sw-stat { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 5px; }
.sw-stat span { color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; font-weight: 600; }
.sw-stat strong { color: var(--text-primary); font-size: 16px; font-weight: 700; }
.sw-stat strong.ok { color: var(--success); }

/* Tabs */
.sw-tabs { display: flex; border-bottom: 1px solid var(--border); }
.sw-tabs button { border: none; border-bottom: 2px solid transparent; background: transparent; color: var(--text-muted); padding: 9px 18px; cursor: pointer; font-size: 13px; font-weight: 600; transition: color 0.15s, border-color 0.15s; }
.sw-tabs button.active { color: #00bfb3; border-bottom-color: #00bfb3; }

.sw-panel { display: flex; flex-direction: column; gap: 12px; }
.sw-empty-hint { text-align: center; padding: 48px; color: var(--text-muted); font-size: 13px; }

/* Table */
.sw-table-wrap { overflow: auto; border: 1px solid var(--border); border-radius: 8px; }
.sw-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.sw-table th { background: var(--bg-elevated); color: var(--text-muted); font-weight: 700; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; padding: 8px 12px; text-align: left; white-space: nowrap; border-bottom: 1px solid var(--border); }
.sw-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: middle; }
.sw-table tr:last-child td { border-bottom: none; }
.sw-table tbody tr:hover td { background: var(--bg-elevated); }
.sw-bold { font-weight: 600; }
.sw-mono { font-family: var(--mono); font-size: 11.5px; }
.sw-muted { color: var(--text-muted); font-size: 11.5px; }
.sw-center { text-align: center; }
.sw-yes { color: var(--success); font-weight: 700; }
.sw-no { color: var(--danger); font-weight: 700; }
.sw-actions-cell { white-space: nowrap; }
.sw-actions-cell .base-btn { margin-right: 4px; }

/* Badges */
.sw-badge { border-radius: 4px; padding: 2px 8px; font-size: 10.5px; font-weight: 700; text-transform: uppercase; display: inline-block; }
.sw-active { background: color-mix(in srgb, var(--success) 14%, transparent); color: var(--success); border: 1px solid color-mix(in srgb, var(--success) 28%, transparent); }
.sw-inactive { background: var(--bg-body); color: var(--text-muted); border: 1px solid var(--border); }
.sw-executed { background: color-mix(in srgb, var(--success) 14%, transparent); color: var(--success); border: 1px solid color-mix(in srgb, var(--success) 28%, transparent); }
.sw-failed { background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger); border: 1px solid color-mix(in srgb, var(--danger) 28%, transparent); }
.sw-failed-badge { background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger); border: 1px solid color-mix(in srgb, var(--danger) 28%, transparent); }
.sw-throttled { background: color-mix(in srgb, var(--warning) 14%, transparent); color: var(--warning); border: 1px solid color-mix(in srgb, var(--warning) 28%, transparent); }
.sw-other { background: var(--bg-elevated); color: var(--text-muted); border: 1px solid var(--border); }

/* History filter */
.sw-history-filter { display: flex; align-items: center; gap: 10px; }
.sw-hist-filter { flex: 1; max-width: 320px; height: 34px; }

/* Modal */
.sw-modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.55); z-index: 200; display: flex; align-items: center; justify-content: center; padding: 24px; }
.sw-modal { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 12px; width: 100%; max-width: 640px; display: flex; flex-direction: column; max-height: 90vh; overflow: hidden; }
.sw-modal--wide { max-width: 860px; }
.sw-modal-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.sw-modal-head h2 { margin: 0; font-size: 16px; color: var(--text-primary); }
.sw-modal-close { border: 0; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 16px; padding: 4px; }
.sw-modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; overflow-y: auto; flex: 1; }
.sw-modal-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 14px 20px; border-top: 1px solid var(--border); }
.sw-label { display: flex; flex-direction: column; gap: 6px; font-size: 12px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; }
.sw-label .base-input { color: var(--text-primary); font-size: 13px; }
.sw-editor-textarea { min-height: 360px; font-family: var(--mono); font-size: 12px; line-height: 1.6; resize: vertical; }
.sw-exec-loading { text-align: center; padding: 32px; color: var(--text-muted); }
.sw-exec-status { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.sw-exec-json { background: var(--bg-body); border: 1px solid var(--border); border-radius: 6px; padding: 12px; font-family: var(--mono); font-size: 11.5px; line-height: 1.55; color: var(--text-secondary); overflow: auto; max-height: 420px; white-space: pre-wrap; word-break: break-word; margin: 0; }

@media (max-width: 900px) {
  .sw-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .sw-topbar, .sw-actions { flex-direction: column; align-items: stretch; }
  .sw-select { width: 100%; }
}
</style>
