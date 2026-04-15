<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'

interface AuditEntry {
  id: number
  username: string
  conn_id: number | null
  conn_name: string
  sql: string
  duration_ms: number
  row_count: number
  error: string
  executed_at: string
}

interface Stats { total: number; errors: number; avg_ms: number }

const entries = ref<AuditEntry[]>([])
const stats = ref<Stats | null>(null)
const loading = ref(false)
const filter = ref('')
const limit = ref(200)
const expanded = ref<number | null>(null)

async function load() {
  loading.value = true
  try {
    const [{ data: e }, { data: s }] = await Promise.all([
      axios.get<AuditEntry[]>('/api/admin/audit', { params: { q: filter.value || undefined, limit: limit.value } }),
      axios.get<Stats>('/api/admin/audit/stats'),
    ])
    entries.value = e ?? []
    stats.value = s
  } finally {
    loading.value = false
  }
}

async function clearAll() {
  if (!confirm('Clear entire audit log?')) return
  await axios.delete('/api/admin/audit')
  await load()
}

onMounted(load)
</script>

<template>
  <div class="al-root">
    <div class="al-scroll">
      <!-- Header -->
      <div class="al-header">
        <div>
          <div class="al-title">Audit Log</div>
          <div class="al-sub">All queries executed across connections.</div>
        </div>
        <div style="flex:1"/>
        <!-- Stats -->
        <div v-if="stats" class="al-stats">
          <div class="al-stat"><span class="al-stat-val">{{ stats.total.toLocaleString() }}</span><span class="al-stat-lbl">Total</span></div>
          <div class="al-stat"><span class="al-stat-val al-stat-err">{{ stats.errors.toLocaleString() }}</span><span class="al-stat-lbl">Errors</span></div>
          <div class="al-stat"><span class="al-stat-val">{{ Math.round(stats.avg_ms) }}ms</span><span class="al-stat-lbl">Avg</span></div>
        </div>
      </div>

      <!-- Toolbar -->
      <div class="al-toolbar">
        <input
          class="al-search"
          v-model="filter"
          placeholder="Filter by SQL, user, or connection…"
          @keydown.enter="load"
        />
        <select class="al-limit" v-model="limit" @change="load">
          <option :value="100">100</option>
          <option :value="200">200</option>
          <option :value="500">500</option>
          <option :value="1000">1000</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="load">Refresh</button>
        <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="clearAll">Clear All</button>
      </div>

      <!-- Table -->
      <div class="al-table-wrap">
        <div v-if="loading" class="al-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>
        <table v-else class="al-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>User</th>
              <th>Connection</th>
              <th>SQL</th>
              <th class="al-th-right">Duration</th>
              <th class="al-th-right">Rows</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="e in entries" :key="e.id">
              <tr class="al-row" :class="{ 'al-row--err': e.error, 'al-row--open': expanded === e.id }" @click="expanded = expanded === e.id ? null : e.id">
                <td class="al-td-dim al-td-nowrap">{{ new Date(e.executed_at).toLocaleTimeString() }}</td>
                <td class="al-td-user">{{ e.username || '—' }}</td>
                <td class="al-td-dim">{{ e.conn_name || '—' }}</td>
                <td class="al-td-sql">{{ e.sql }}</td>
                <td class="al-td-right al-td-num">{{ e.duration_ms }}ms</td>
                <td class="al-td-right al-td-num">{{ e.row_count }}</td>
                <td>
                  <span class="al-badge" :class="e.error ? 'al-badge--err' : 'al-badge--ok'">
                    {{ e.error ? 'Error' : 'OK' }}
                  </span>
                </td>
              </tr>
              <tr v-if="expanded === e.id" class="al-detail-row">
                <td colspan="7">
                  <div class="al-detail">
                    <div v-if="e.error" class="al-detail-error">{{ e.error }}</div>
                    <pre class="al-detail-sql">{{ e.sql }}</pre>
                    <div class="al-detail-meta">
                      {{ new Date(e.executed_at).toLocaleString() }}
                      · {{ e.duration_ms }}ms
                      · {{ e.row_count }} rows
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="entries.length === 0">
              <td colspan="7" style="text-align:center;color:var(--text-muted);padding:24px;font-size:13px">
                No audit log entries.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.al-root { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.al-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 24px 28px 40px; display: flex; flex-direction: column; gap: 16px; }
.al-header { display: flex; align-items: flex-start; gap: 16px; flex-wrap: wrap; }
.al-title { font-size: 20px; font-weight: 700; color: var(--text-primary); }
.al-sub { font-size: 13px; color: var(--text-muted); margin-top: 3px; }
.al-stats { display: flex; gap: 16px; }
.al-stat { display: flex; flex-direction: column; align-items: flex-end; }
.al-stat-val { font-size: 18px; font-weight: 700; color: var(--text-primary); }
.al-stat-err { color: var(--danger); }
.al-stat-lbl { font-size: 10.5px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.4px; }
.al-loading { display: flex; align-items: center; justify-content: center; padding: 40px; color: var(--text-muted); }
.al-toolbar { display: flex; align-items: center; gap: 8px; }
.al-search {
  flex: 1; padding: 6px 12px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 13px;
  font-family: inherit; outline: none;
}
.al-search:focus { border-color: var(--brand); }
.al-limit {
  padding: 5px 8px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 12px;
  cursor: pointer; outline: none;
}
.al-table-wrap { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.al-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.al-table th {
  padding: 8px 14px; background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  font-size: 10.5px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.4px; color: var(--text-muted); text-align: left;
}
.al-table td { padding: 7px 14px; border-bottom: 1px solid var(--border); color: var(--text-primary); }
.al-row { cursor: pointer; transition: background 0.1s; }
.al-row:hover td { background: var(--bg-hover); }
.al-row--err td { color: var(--danger); }
.al-row--open td { background: var(--bg-hover); }
.al-td-dim { color: var(--text-muted); }
.al-td-nowrap { white-space: nowrap; }
.al-td-user { font-weight: 600; }
.al-td-sql {
  max-width: 300px; overflow: hidden; text-overflow: ellipsis;
  white-space: nowrap; font-family: var(--mono, monospace); font-size: 11.5px;
}
.al-td-right { text-align: right; }
.al-td-num { font-variant-numeric: tabular-nums; font-family: var(--mono, monospace); }
.al-th-right { text-align: right; }
.al-badge { padding: 1px 7px; border-radius: 4px; font-size: 10px; font-weight: 700; }
.al-badge--ok { background: rgba(74,222,128,0.15); color: #4ade80; }
.al-badge--err { background: rgba(248,113,113,0.15); color: #f87171; }
.al-detail-row td { padding: 0 !important; }
.al-detail { padding: 12px 16px; background: var(--bg-body); border-bottom: 1px solid var(--border); }
.al-detail-error { color: var(--danger); font-size: 12px; margin-bottom: 8px; }
.al-detail-sql {
  margin: 0 0 8px; padding: 10px 12px;
  background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 6px;
  font-family: var(--mono, monospace); font-size: 12px; line-height: 1.6;
  color: var(--text-primary); white-space: pre-wrap; word-break: break-all;
}
.al-detail-meta { font-size: 11px; color: var(--text-muted); }
</style>
