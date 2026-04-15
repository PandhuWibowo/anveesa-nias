<template>
  <div class="rh-root">
    <div class="rh-header">
      <h2>Row Change History</h2>
      <div class="rh-filters">
        <select v-model="selectedConn" class="rh-select" @change="selectedTable = ''; load()">
          <option value="">Connection…</option>
          <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <input v-model="selectedDb" class="rh-input" placeholder="Database (optional)" @change="load()" />
        <input v-model="selectedTable" class="rh-input" placeholder="Table name…" @keydown.enter="load()" />
        <input v-model="pkFilter" class="rh-input" placeholder="PK value (optional)" @keydown.enter="load()" />
        <button class="rh-btn rh-btn--primary" @click="load()">Search</button>
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
              <button class="rh-expand-btn" @click="toggleExpand(c.id)">
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
              <button class="rh-undo-btn" @click="undo(c)"
                :disabled="undoing === c.id"
                :title="undoLabel(c.operation)">
                {{ undoing === c.id ? '…' : '↩' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="searched" class="rh-empty">No row changes found for the given filters.</div>
    <div v-else class="rh-empty">Select a connection and table to view row change history.</div>

    <div v-if="message" :class="['rh-toast', message.type === 'error' ? 'rh-toast--error' : 'rh-toast--ok']">
      {{ message.text }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'

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
.rh-root { padding: 24px; display: flex; flex-direction: column; gap: 16px; height: 100%; min-height: 0; }
.rh-header { display: flex; flex-direction: column; gap: 12px; }
.rh-header h2 { font-size: 20px; font-weight: 700; color: var(--text-primary); margin: 0; }
.rh-filters { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.rh-select, .rh-input { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 6px; color: var(--text-primary); font-size: 13px; padding: 6px 10px; }
.rh-btn { padding: 6px 16px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg-panel); color: var(--text-primary); font-size: 13px; cursor: pointer; }
.rh-btn--primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.rh-table-wrap { flex: 1; overflow: auto; min-height: 0; }
.rh-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.rh-table th { position: sticky; top: 0; background: var(--bg-sidebar); padding: 8px 12px; text-align: left; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); white-space: nowrap; }
.rh-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: top; }
.rh-mono { font-family: var(--font-mono); font-size: 12px; }
.rh-op { font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: 4px; text-transform: uppercase; letter-spacing: .3px; }
.rh-op--insert { background: rgba(86,196,144,.2); color: #56c490; }
.rh-op--update { background: rgba(249,212,79,.2); color: #c9a800; }
.rh-op--delete { background: rgba(249,127,79,.2); color: #f97f4f; }
.rh-diff-cell { max-width: 420px; }
.rh-expand-btn { background: none; border: none; color: var(--accent); font-size: 12px; cursor: pointer; padding: 0; }
.rh-diff { display: flex; gap: 8px; margin-top: 6px; }
.rh-diff-side { flex: 1; }
.rh-diff-label { font-size: 10px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; margin-bottom: 3px; }
.rh-diff-json { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; padding: 6px 8px; font-size: 11px; font-family: var(--font-mono); margin: 0; overflow-x: auto; max-height: 160px; overflow-y: auto; color: var(--text-primary); }
.rh-undo-btn { background: none; border: 1px solid var(--border); border-radius: 4px; color: var(--text-muted); cursor: pointer; font-size: 14px; padding: 2px 8px; }
.rh-undo-btn:hover:not(:disabled) { background: rgba(249,212,79,.15); color: #c9a800; }
.rh-undo-btn:disabled { opacity: .4; cursor: default; }
.rh-empty { color: var(--text-muted); text-align: center; padding: 40px; font-size: 14px; }
.rh-toast { position: fixed; bottom: 40px; left: 50%; transform: translateX(-50%); padding: 10px 20px; border-radius: 8px; font-size: 13px; z-index: 9999; }
.rh-toast--ok    { background: #56c490; color: #fff; }
.rh-toast--error { background: #f97f4f; color: #fff; }
</style>
