<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useSavedQueries, type SavedQuery } from '@/composables/useSavedQueries'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { pendingSQL } from '@/composables/usePendingSQL'
import { pendingAIAnalytics } from '@/composables/usePendingAIAnalytics'

const router = useRouter()
const { queries, loading, fetchAll, save, remove } = useSavedQueries()
const { connections } = useConnections()
const toast = useToast()

const search = ref('')
const filterConnId = ref<number | 'all'>('all')

// Edit state
const editingId = ref<number | null>(null)
const editName = ref('')
const editDesc = ref('')

// New query form
const showNew = ref(false)
const newName = ref('')
const newDesc = ref('')
const newSQL = ref('')
const newConnId = ref<number | null>(null)
const newSaving = ref(false)

// Expanded SQL preview
const expandedId = ref<number | null>(null)

onMounted(() => fetchAll())

const filtered = computed(() => {
  let list = queries.value
  if (filterConnId.value !== 'all') {
    list = list.filter(q => q.conn_id === filterConnId.value)
  }
  if (search.value.trim()) {
    const s = search.value.toLowerCase()
    list = list.filter(q =>
      q.name.toLowerCase().includes(s) ||
      q.sql.toLowerCase().includes(s) ||
      q.description?.toLowerCase().includes(s)
    )
  }
  return list
})

function connName(connId: number | null) {
  if (!connId) return null
  return connections.value.find(c => c.id === connId)?.name ?? `#${connId}`
}

function connDriver(connId: number | null) {
  if (!connId) return null
  return connections.value.find(c => c.id === connId)?.driver ?? null
}

const driverColor: Record<string, string> = {
  postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b', mssql: '#cc2927',
}

function startEdit(q: SavedQuery) {
  editingId.value = q.id
  editName.value = q.name
  editDesc.value = q.description ?? ''
}

async function saveEdit(q: SavedQuery) {
  if (!editName.value.trim()) return
  try {
    await axios.put(`/api/saved-queries/${q.id}`, {
      name: editName.value.trim(),
      description: editDesc.value,
    })
    await fetchAll()
    toast.success('Query updated')
  } catch {
    toast.error('Failed to update')
  }
  editingId.value = null
}

function cancelEdit() { editingId.value = null }

async function deleteQuery(q: SavedQuery) {
  if (!confirm(`Delete "${q.name}"?`)) return
  await remove(q.id)
  toast.success('Query deleted')
}

function copySQL(sql: string) {
  navigator.clipboard.writeText(sql)
  toast.success('Copied to clipboard')
}

function openInDataBrowser(sql: string) {
  pendingSQL.value = sql
  router.push({ name: 'data' })
}

function analyzeWithAI(q: SavedQuery) {
  if (!q.conn_id) {
    toast.error('Saved query must be tied to a connection before AI can analyze it')
    return
  }
  pendingAIAnalytics.value = {
    connId: q.conn_id,
    title: q.name,
    question: q.description?.trim() || 'Summarize the main insight from this saved query result.',
    sql: q.sql,
    source: 'saved_query',
  }
  router.push({ name: 'ai-analytics' })
}

async function createNew() {
  if (!newName.value.trim() || !newSQL.value.trim()) return
  newSaving.value = true
  try {
    await save(newName.value.trim(), newSQL.value.trim(), newDesc.value, newConnId.value)
    toast.success('Query saved')
    showNew.value = false
    newName.value = ''; newDesc.value = ''; newSQL.value = ''; newConnId.value = null
  } catch {
    toast.error('Failed to save')
  } finally {
    newSaving.value = false
  }
}

function formatDate(d: string) {
  return new Date(d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}
</script>

<template>
  <div class="page-shell sq-view">
    <div class="page-scroll">
      <div class="page-stack">
    <section class="page-hero">
      <div class="page-hero__content">
        <div class="page-kicker">Library</div>
        <div class="page-title">Saved Queries</div>
        <div class="page-subtitle">Store reusable SQL, filter it by connection, and reopen it directly in the data workspace when you need it again.</div>
      </div>
      <div class="page-hero__actions">
    <div class="sq-header">
      <div class="sq-header__left">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--brand)"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
        <div>
          <p class="sq-subtitle">{{ queries.length }} quer{{ queries.length !== 1 ? 'ies' : 'y' }} saved by your session</p>
        </div>
      </div>
      <button class="base-btn base-btn--primary base-btn--sm" @click="showNew = !showNew">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        New Query
      </button>
    </div>
      </div>
    </section>

    <!-- New query form -->
    <div v-if="showNew" class="page-card sq-new-form">
      <div class="sq-new-form__row">
        <div class="sq-field">
          <label class="sq-label">Name *</label>
          <input v-model="newName" class="base-input" placeholder="e.g. Active users last 30 days" />
        </div>
        <div class="sq-field">
          <label class="sq-label">Connection (optional)</label>
          <select v-model="newConnId" class="base-input">
            <option :value="null">All connections</option>
            <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
      </div>
      <div class="sq-field">
        <label class="sq-label">Description (optional)</label>
        <input v-model="newDesc" class="base-input" placeholder="Brief description…" />
      </div>
      <div class="sq-field">
        <label class="sq-label">SQL *</label>
        <textarea v-model="newSQL" class="sq-sql-input" rows="5" placeholder="SELECT * FROM …" />
      </div>
      <div class="sq-new-form__footer">
        <button class="base-btn base-btn--ghost base-btn--sm" @click="showNew = false">Cancel</button>
        <button class="base-btn base-btn--primary base-btn--sm" :disabled="!newName.trim() || !newSQL.trim() || newSaving" @click="createNew">
          {{ newSaving ? 'Saving…' : 'Save Query' }}
        </button>
      </div>
    </div>

    <!-- Filters -->
    <div class="page-toolbar-surface sq-filters">
      <div class="sq-search-wrap">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--text-muted);flex-shrink:0"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        <input v-model="search" class="sq-search" placeholder="Search by name, SQL or description…" />
      </div>
      <select v-model="filterConnId" class="sq-filter-select">
        <option value="all">All connections</option>
        <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
      </select>
      <span class="sq-count">{{ filtered.length }} result{{ filtered.length !== 1 ? 's' : '' }}</span>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="sq-empty">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="animation:spin 1s linear infinite;color:var(--brand)"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Loading…
    </div>

    <!-- Empty state -->
    <div v-else-if="!filtered.length" class="sq-empty">
      <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
      <p style="color:var(--text-muted);font-size:13px;margin-top:8px">
        {{ search || filterConnId !== 'all' ? 'No queries match your filters.' : 'No saved queries yet. Save a query from the SQL editor in Data Browser.' }}
      </p>
    </div>

    <!-- Query list -->
    <div v-else class="sq-list">
      <div v-for="q in filtered" :key="q.id" class="page-card sq-card">

        <!-- Card header -->
        <div class="sq-card__header">
          <!-- Edit mode -->
          <template v-if="editingId === q.id">
            <input v-model="editName" class="base-input sq-edit-name" placeholder="Query name…" @keydown.enter="saveEdit(q)" @keydown.escape="cancelEdit" />
            <input v-model="editDesc" class="base-input sq-edit-desc" placeholder="Description…" @keydown.enter="saveEdit(q)" @keydown.escape="cancelEdit" />
            <div class="sq-card__actions">
              <button class="base-btn base-btn--primary base-btn--xs" @click="saveEdit(q)">Save</button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="cancelEdit">Cancel</button>
            </div>
          </template>

          <!-- View mode -->
          <template v-else>
            <div class="sq-card__meta">
              <span class="sq-card__name">{{ q.name }}</span>
              <span v-if="connName(q.conn_id)" class="sq-card__conn" :style="{ borderColor: driverColor[connDriver(q.conn_id)!] ?? '#555' }">
                <span class="sq-card__conn-dot" :style="{ background: driverColor[connDriver(q.conn_id)!] ?? '#555' }" />
                {{ connName(q.conn_id) }}
              </span>
              <span v-else class="sq-card__global">
                <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
                All connections
              </span>
              <span class="sq-card__date">{{ formatDate(q.created_at) }}</span>
            </div>
            <div class="sq-card__actions">
              <button class="base-btn base-btn--ghost base-btn--xs" @click="copySQL(q.sql)" title="Copy SQL">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                Copy
              </button>
              <button class="base-btn base-btn--primary base-btn--xs" @click="openInDataBrowser(q.sql)" title="Open in Data Browser SQL tab">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
                Open in SQL
              </button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="analyzeWithAI(q)" title="Open this saved query in AI Analytics">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3l1.9 4.8L19 9.7l-3.8 3.1 1.2 4.9L12 15l-4.4 2.7 1.2-4.9L5 9.7l5.1-1.9L12 3z"/></svg>
                Analyze with AI
              </button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="startEdit(q)" title="Edit name & description">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                Edit
              </button>
              <button class="base-btn base-btn--ghost base-btn--xs" style="color:#f87171" @click="deleteQuery(q)" title="Delete">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/></svg>
              </button>
            </div>
          </template>
        </div>

        <!-- Description -->
        <p v-if="q.description && editingId !== q.id" class="sq-card__desc">{{ q.description }}</p>

        <!-- SQL preview -->
        <div class="sq-card__sql-wrap" @click="expandedId = expandedId === q.id ? null : q.id">
          <pre class="sq-card__sql" :class="{ 'sq-card__sql--expanded': expandedId === q.id }">{{ q.sql }}</pre>
          <button class="sq-card__expand" v-if="q.sql.length > 120 || q.sql.includes('\n')">
            {{ expandedId === q.id ? 'Collapse' : 'Expand' }}
          </button>
        </div>
      </div>
    </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sq-view {
  background: var(--bg-body);
}

/* ── Header ─────────────────────────────────────────────────────── */
.sq-header {
  display: flex; align-items: flex-start; justify-content: space-between;
  gap: 16px; flex-shrink: 0;
}
.sq-header__left { display: flex; align-items: flex-start; gap: 12px; }
.sq-title { font-size: 18px; font-weight: 700; color: var(--text-primary); margin: 0 0 2px; }
.sq-subtitle { font-size: 12px; color: var(--text-muted); margin: 0; }

/* ── New form ────────────────────────────────────────────────────── */
.sq-new-form {
  padding: 16px; display: flex; flex-direction: column; gap: 10px;
}
.sq-new-form__row { display: flex; gap: 12px; }
.sq-new-form__footer { display: flex; justify-content: flex-end; gap: 8px; }
.sq-field { display: flex; flex-direction: column; gap: 4px; flex: 1; }
.sq-label { font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.4px; }
.sq-sql-input {
  background: var(--bg-surface); border: 1px solid var(--border); border-radius: 6px;
  color: var(--text-primary); font-family: var(--mono, monospace); font-size: 12.5px;
  padding: 8px 10px; resize: vertical; outline: none; transition: border-color 0.15s;
}
.sq-sql-input:focus { border-color: var(--brand); }

/* ── Filters ─────────────────────────────────────────────────────── */
.sq-filters {
  display: flex; align-items: center; gap: 10px; flex-shrink: 0;
}
.sq-search-wrap {
  display: flex; align-items: center; gap: 8px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; padding: 6px 10px; flex: 1; max-width: 400px;
  transition: border-color 0.15s;
}
.sq-search-wrap:focus-within { border-color: var(--brand); }
.sq-search {
  flex: 1; background: transparent; border: none; outline: none;
  font-size: 13px; color: var(--text-primary);
}
.sq-filter-select {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; color: var(--text-primary); font-size: 12px;
  padding: 6px 10px; outline: none; cursor: pointer;
}
.sq-count { font-size: 11px; color: var(--text-muted); white-space: nowrap; }

/* ── Empty / loading ─────────────────────────────────────────────── */
.sq-empty {
  flex: 1; display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 8px; padding: 60px 0;
}

/* ── List ────────────────────────────────────────────────────────── */
.sq-list { display: flex; flex-direction: column; gap: 10px; }

/* ── Card ────────────────────────────────────────────────────────── */
.sq-card {
  overflow: hidden;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.sq-card:hover { border-color: var(--brand); box-shadow: 0 2px 12px rgba(0,0,0,0.1); }

.sq-card__header {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; padding: 12px 14px; flex-wrap: wrap;
}

.sq-card__meta { display: flex; align-items: center; gap: 10px; flex: 1; flex-wrap: wrap; }

.sq-card__name {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
}
.sq-card__desc {
  font-size: 12px; color: var(--text-muted);
  padding: 0 14px 8px; margin: 0;
}

.sq-card__conn {
  display: flex; align-items: center; gap: 5px;
  font-size: 11px; color: var(--text-muted);
  padding: 2px 7px; border-radius: 10px;
  border: 1px solid; background: transparent;
}
.sq-card__conn-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.sq-card__global {
  display: flex; align-items: center; gap: 4px;
  font-size: 11px; color: var(--text-muted);
  padding: 2px 7px; border-radius: 10px;
  border: 1px solid var(--border);
}
.sq-card__date { font-size: 11px; color: var(--text-muted); }

.sq-card__actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }

.sq-edit-name { font-size: 13px; font-weight: 600; max-width: 240px; }
.sq-edit-desc { font-size: 12px; max-width: 320px; }

/* ── SQL preview ─────────────────────────────────────────────────── */
.sq-card__sql-wrap {
  position: relative; cursor: pointer;
  border-top: 1px solid var(--border);
  background: var(--bg-surface);
}
.sq-card__sql {
  margin: 0; padding: 10px 14px;
  font-family: var(--mono, monospace); font-size: 12px;
  color: var(--text-primary); line-height: 1.5;
  white-space: pre; overflow: hidden;
  max-height: 40px;
  text-overflow: ellipsis;
  transition: max-height 0.2s ease;
}
.sq-card__sql--expanded { max-height: 400px; overflow: auto; white-space: pre; }

.sq-card__expand {
  position: absolute; bottom: 4px; right: 10px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 4px; font-size: 10px; color: var(--text-muted);
  padding: 1px 6px; cursor: pointer;
}
.sq-card__expand:hover { color: var(--brand); border-color: var(--brand); }

@keyframes spin { to { transform: rotate(360deg); } }
</style>
