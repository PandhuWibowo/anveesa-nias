<template>
  <div class="rh-root">
    <div class="rh-header">
      <div>
        <h1 class="page-title">Row history</h1>
        <p class="page-subtitle">Every insert, update, and delete made through Nias, with the before and after values. Undo a change in one click.</p>
      </div>
      <div class="rh-filters">
        <select v-model="selectedConn" class="base-select rh-select" aria-label="Connection" @change="selectedTable = ''; load()">
          <option value="" disabled>Connection</option>
          <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <input v-model="selectedDb" class="base-input rh-input" placeholder="Database (optional)" @change="load()" />
        <input v-model="selectedTable" class="base-input rh-input" placeholder="Table" @keydown.enter="load()" />
        <input v-model="pkFilter" class="base-input rh-input" placeholder="PK value (optional)" @keydown.enter="load()" />
        <button class="base-btn base-btn--primary" @click="load()">Show changes</button>
      </div>
    </div>

    <div class="rh-table-wrap" v-if="changes.length > 0">
      <table class="rh-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Operation</th>
            <th>Table</th>
            <th>PK</th>
            <th>User</th>
            <th>Before → After</th>
            <th>Undo</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in changes" :key="c.id">
            <td class="rh-mono">{{ c.changed_at }}</td>
            <td>
              <span :class="['rh-op', 'rh-op--' + c.operation.toLowerCase()]">{{ c.operation }}</span>
            </td>
            <td>{{ c.table_name }}</td>
            <td class="rh-mono">{{ c.pk_column }}: {{ c.pk_value }}</td>
            <td>{{ c.username || '—' }}</td>
            <td class="rh-diff-cell">
              <button class="base-btn base-btn--ghost base-btn--xs" @click="toggleExpand(c.id)">
                {{ expanded.has(c.id) ? '▾' : '▸' }} View diff
              </button>
              <div v-if="expanded.has(c.id)" class="rh-diff">
                <div class="rh-diff-side">
                  <div class="rh-diff-label">Before</div>
                  <pre class="rh-diff-json">{{ fmtJSON(c.before_data) }}</pre>
                </div>
                <div class="rh-diff-side">
                  <div class="rh-diff-label">After</div>
                  <pre class="rh-diff-json">{{ fmtJSON(c.after_data) }}</pre>
                </div>
              </div>
            </td>
            <td>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="undo(c)"
                :disabled="undoing === c.id"
                :title="undoLabel(c.operation)">
                {{ undoing === c.id ? '…' : '↩' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <EmptyState v-else-if="searched" icon="rowhistory" title="No changes found" text="Nothing was recorded for these filters. Only edits made through Nias are tracked." />
    <EmptyState v-else icon="rowhistory" title="Pick a table" text="Choose a connection and enter a table name to see its recorded changes." />

    <div v-if="message" :class="['rh-toast', message.type === 'error' ? 'rh-toast--error' : 'rh-toast--ok']">
      {{ message.text }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{ activeConnId?: number | null }>()

interface RowChange {
  id: number
  conn_id: number
  database: string
  table_name: string
  operation: string
  pk_column: string
  pk_value: string
  before_data: string
  after_data: string
  username: string
  changed_at: string
}
interface Conn { id: number; name: string }

const connections = ref<Conn[]>([])
const changes = ref<RowChange[]>([])
const selectedConn = ref<number | ''>('')
const selectedDb = ref('')
const selectedTable = ref('')
const pkFilter = ref('')
const expanded = ref(new Set<number>())
const searched = ref(false)
const undoing = ref<number | null>(null)
const message = ref<{ type: string; text: string } | null>(null)

onMounted(async () => {
  const { data } = await axios.get('/api/connections')
  connections.value = data
  if (props.activeConnId && connections.value.some(c => c.id === props.activeConnId)) {
    selectedConn.value = props.activeConnId
  }
})

async function load() {
  if (!selectedConn.value || !selectedTable.value.trim()) { searched.value = true; changes.value = []; return }
  const db = selectedDb.value || '_'
  let url = `/api/connections/${selectedConn.value}/row-history/${encodeURIComponent(db)}/${encodeURIComponent(selectedTable.value)}`
  if (pkFilter.value) url += `?pk=${encodeURIComponent(pkFilter.value)}`
  try {
    const { data } = await axios.get(url)
    changes.value = data
    searched.value = true
  } catch (e: unknown) {
    showMsg('error', (e as Error).message)
  }
}

async function undo(c: RowChange) {
  if (!confirm(`Undo this ${c.operation} on row ${c.pk_column}=${c.pk_value}?`)) return
  undoing.value = c.id
  try {
    await axios.post(`/api/connections/${c.conn_id}/row-history/_/_/undo`, { change_id: c.id })
    showMsg('ok', `${c.operation} undone successfully`)
    await load()
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error ?? String(e)
    showMsg('error', msg)
  } finally {
    undoing.value = null
  }
}

function undoLabel(op: string) {
  return { INSERT: 'Undo: delete this row', UPDATE: 'Undo: restore previous values', DELETE: 'Undo: re-insert row' }[op] ?? 'Undo'
}

function toggleExpand(id: number) {
  if (expanded.value.has(id)) expanded.value.delete(id)
  else expanded.value.add(id)
}

function fmtJSON(s: string) {
  try { return JSON.stringify(JSON.parse(s), null, 2) } catch { return s || '—' }
}

function showMsg(type: string, text: string) {
  message.value = { type, text }
  setTimeout(() => { message.value = null }, 4000)
}
</script>

<style scoped>
.rh-root { padding: 28px 32px 32px; display: flex; flex-direction: column; gap: 20px; height: 100%; min-height: 0; overflow: hidden; }
@media (max-width: 900px) { .rh-root { padding: 20px 16px; } }
.rh-header { display: flex; flex-direction: column; gap: 16px; }
.rh-filters { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.rh-select, .rh-input { width: auto; min-width: 160px; flex: 0 1 200px; }
.rh-table-wrap { flex: 1; overflow: auto; min-height: 0; }
.rh-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.rh-table th { position: sticky; top: 0; background: var(--bg-sidebar); padding: 8px 12px; text-align: left; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); white-space: nowrap; }
.rh-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: top; }
.rh-mono { font-family: var(--font-mono); font-size: 12px; }
.rh-op { font-size: 12px; font-weight: 700; padding: 2px 8px; border-radius: 4px; }
.rh-op--insert { background: var(--success-bg); color: var(--success); }
.rh-op--update { background: var(--warning-bg); color: var(--warning); }
.rh-op--delete { background: var(--danger-bg); color: var(--danger); }
.rh-diff-cell { max-width: 420px; }
.rh-diff { display: flex; gap: 8px; margin-top: 6px; }
.rh-diff-side { flex: 1; }
.rh-diff-label { font-size: 12px; font-weight: 700; color: var(--text-muted); margin-bottom: 3px; }
.rh-diff-json { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; padding: 6px 8px; font-size: 11px; font-family: var(--font-mono); margin: 0; overflow-x: auto; max-height: 160px; overflow-y: auto; color: var(--text-primary); }
.rh-empty { color: var(--text-muted); text-align: center; padding: 40px; font-size: 14px; }
.rh-toast { position: fixed; bottom: 40px; left: 50%; transform: translateX(-50%); padding: 10px 20px; border-radius: 8px; font-size: 13px; z-index: 9999; }
.rh-toast--ok    { background: var(--success); color: #fff; }
.rh-toast--error { background: #f97f4f; color: #fff; }
</style>
