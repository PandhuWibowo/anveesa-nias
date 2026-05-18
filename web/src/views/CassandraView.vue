<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useCassandra, type CassandraColumnSummary, type CassandraDashboardData, type CassandraKeyspaceSummary, type CassandraResult, type CassandraTableSummary } from '@/composables/useCassandra'
import { useToast } from '@/composables/useToast'
import { readableError } from '@/utils/httpError'

const props = defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const cassandra = useCassandra()
const toast = useToast()

const loading = ref(false)
const loadingRows = ref(false)
const error = ref('')
const dashboard = ref<CassandraDashboardData | null>(null)
const keyspaces = ref<CassandraKeyspaceSummary[]>([])
const tables = ref<CassandraTableSummary[]>([])
const columns = ref<CassandraColumnSummary[]>([])
const selectedKeyspace = ref('')
const selectedTable = ref('')
const tableSearch = ref('')
const rowLimit = ref(100)
const activeTab = ref<'data' | 'structure' | 'query'>('data')
const result = ref<CassandraResult>({ columns: [], rows: [], row_count: 0, applied: false, duration_ms: 0 })
const queryResult = ref<CassandraResult>({ columns: [], rows: [], row_count: 0, applied: false, duration_ms: 0 })
const cql = ref('SELECT *\nFROM system.local\nLIMIT 10;')

const cassandraConnections = computed(() => connections.value.filter(c => c.driver === 'cassandra'))
const activeConn = computed(() => connections.value.find(c => c.id === props.activeConnId) ?? null)
const isCassandra = computed(() => activeConn.value?.driver === 'cassandra')
const selectedTableInfo = computed(() => tables.value.find(t => t.name === selectedTable.value) ?? null)
const filteredTables = computed(() => {
  const q = tableSearch.value.trim().toLowerCase()
  if (!q) return tables.value
  return tables.value.filter(t => t.name.toLowerCase().includes(q))
})
const visibleColumns = computed(() => result.value.columns.length ? result.value.columns : columns.value.map(c => c.name))
const systemKeyspaces = new Set(['system', 'system_auth', 'system_distributed', 'system_schema', 'system_traces', 'system_views', 'system_virtual_schema'])

function compact(value: any) {
  if (value == null) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function rowValue(row: Record<string, any>, column: string) {
  return compact(row[column])
}

function keyspaceClass(name: string) {
  return systemKeyspaces.has(name) ? 'System' : 'User'
}

async function selectConnection(rawId: string | number) {
  const id = Number(rawId)
  if (!id) return
  emit('set-conn', id)
}

async function loadAll() {
  if (!activeConn.value || !isCassandra.value) return
  loading.value = true
  error.value = ''
  result.value = { columns: [], rows: [], row_count: 0, applied: false, duration_ms: 0 }
  try {
    const [dash, ks] = await Promise.all([
      cassandra.dashboard(activeConn.value.id),
      cassandra.keyspaces(activeConn.value.id),
    ])
    dashboard.value = dash
    keyspaces.value = ks
    const nextKeyspace = selectedKeyspace.value || dash.keyspace || ks.find(k => !systemKeyspaces.has(k.name))?.name || ks[0]?.name || ''
    if (selectedKeyspace.value === nextKeyspace) {
      await loadTables()
    } else {
      selectedKeyspace.value = nextKeyspace
    }
  } catch (e) {
    error.value = readableError(e, { action: 'Load Cassandra workspace', fallback: 'Failed to load Cassandra workspace' })
    toast.error(error.value)
  } finally {
    loading.value = false
  }
}

async function loadTables() {
  if (!activeConn.value || !selectedKeyspace.value) return
  const preferred = selectedTable.value
  tables.value = await cassandra.tables(activeConn.value.id, selectedKeyspace.value)
  const nextTable = tables.value.some(t => t.name === preferred) ? preferred : (tables.value[0]?.name ?? '')
  columns.value = []
  if (selectedTable.value === nextTable) {
    await loadColumns()
    await loadRows()
  } else {
    selectedTable.value = nextTable
  }
}

async function loadColumns() {
  if (!activeConn.value || !selectedKeyspace.value || !selectedTable.value) return
  columns.value = await cassandra.columns(activeConn.value.id, selectedKeyspace.value, selectedTable.value)
}

async function loadRows() {
  if (!activeConn.value || !selectedKeyspace.value || !selectedTable.value) return
  loadingRows.value = true
  try {
    result.value = await cassandra.rows(activeConn.value.id, selectedKeyspace.value, selectedTable.value, rowLimit.value)
  } catch (e) {
    toast.error(readableError(e, { action: 'Load Cassandra rows', fallback: 'Failed to load Cassandra rows' }))
  } finally {
    loadingRows.value = false
  }
}

async function runQuery() {
  if (!activeConn.value || !cql.value.trim()) return
  loadingRows.value = true
  try {
    queryResult.value = await cassandra.query(activeConn.value.id, selectedKeyspace.value, cql.value, rowLimit.value)
    activeTab.value = 'query'
  } catch (e) {
    toast.error(readableError(e, { action: 'Run CQL', fallback: 'Failed to run CQL' }))
  } finally {
    loadingRows.value = false
  }
}

function useTableQuery() {
  if (!selectedKeyspace.value || !selectedTable.value) return
  cql.value = `SELECT *\nFROM "${selectedKeyspace.value}"."${selectedTable.value}"\nLIMIT ${rowLimit.value};`
  activeTab.value = 'query'
}

watch(() => props.activeConnId, () => {
  if (activeConn.value?.driver === 'cassandra') loadAll()
})

watch(selectedKeyspace, () => {
  selectedTable.value = ''
  if (isCassandra.value && selectedKeyspace.value) loadTables()
})

watch(selectedTable, () => {
  if (isCassandra.value && selectedTable.value) {
    loadColumns()
    loadRows()
  }
})

onMounted(async () => {
  await fetchConnections()
  if (!props.activeConnId && cassandraConnections.value.length === 1) {
    emit('set-conn', cassandraConnections.value[0].id)
  } else if (activeConn.value?.driver === 'cassandra') {
    await loadAll()
  }
})
</script>

<template>
  <div class="cass-page">
    <div class="cass-toolbar">
      <div>
        <div class="page-kicker">Wide-column database</div>
        <div class="cass-title">Cassandra Workbench</div>
      </div>
      <div class="cass-actions">
        <select class="base-input cass-select" :value="activeConnId ?? ''" @change="selectConnection(($event.target as HTMLSelectElement).value)">
          <option value="">Select Cassandra connection</option>
          <option v-for="conn in cassandraConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--sm" :disabled="!isCassandra || loading" @click="loadAll">Refresh</button>
      </div>
    </div>

    <div v-if="!isCassandra" class="cass-empty">
      <div class="cass-empty__mark">CA</div>
      <div>
        <h2>Choose a Cassandra connection</h2>
        <p>Create or select a Cassandra connection to browse keyspaces, inspect tables, and run CQL.</p>
      </div>
    </div>

    <div v-else class="cass-grid">
      <aside class="cass-sidebar">
        <div class="cass-panel-head">
          <span>Keyspaces</span>
          <span class="cass-muted">{{ keyspaces.length }}</span>
        </div>
        <div class="cass-keyspaces">
          <button
            v-for="ks in keyspaces"
            :key="ks.name"
            class="cass-keyspace"
            :class="{ 'is-active': selectedKeyspace === ks.name }"
            @click="selectedKeyspace = ks.name"
          >
            <span>{{ ks.name }}</span>
            <small>{{ keyspaceClass(ks.name) }} · {{ ks.table_count }}</small>
          </button>
        </div>

        <div class="cass-panel-head cass-panel-head--spaced">
          <span>Tables</span>
          <span class="cass-muted">{{ filteredTables.length }}</span>
        </div>
        <input v-model="tableSearch" class="base-input cass-search" placeholder="Filter tables" />
        <div class="cass-tables">
          <button
            v-for="table in filteredTables"
            :key="table.name"
            class="cass-table"
            :class="{ 'is-active': selectedTable === table.name }"
            @click="selectedTable = table.name"
          >
            <span>{{ table.name }}</span>
            <small>{{ table.columns }} cols</small>
          </button>
        </div>
      </aside>

      <main class="cass-main">
        <section class="cass-summary">
          <div class="cass-stat">
            <span>Cluster</span>
            <strong>{{ dashboard?.cluster_name || activeConn?.name }}</strong>
          </div>
          <div class="cass-stat">
            <span>Version</span>
            <strong>{{ dashboard?.version || '-' }}</strong>
          </div>
          <div class="cass-stat">
            <span>Keyspaces</span>
            <strong>{{ dashboard?.keyspaces ?? '-' }}</strong>
          </div>
          <div class="cass-stat">
            <span>Tables</span>
            <strong>{{ dashboard?.tables ?? '-' }}</strong>
          </div>
        </section>

        <section class="cass-workbench">
          <div class="cass-workbench__bar">
            <div>
              <strong>{{ selectedKeyspace || 'No keyspace' }}<span v-if="selectedTable"> / {{ selectedTable }}</span></strong>
              <small v-if="selectedTableInfo">{{ selectedTableInfo.partition_key || 'No partition key metadata' }}</small>
            </div>
            <div class="cass-actions">
              <input v-model.number="rowLimit" class="base-input cass-limit" type="number" min="1" max="500" />
              <button class="base-btn base-btn--sm" :disabled="!selectedTable || loadingRows" @click="loadRows">Load</button>
              <button class="base-btn base-btn--sm" :disabled="!selectedTable" @click="useTableQuery">CQL</button>
            </div>
          </div>

          <div class="cass-tabs">
            <button :class="{ 'is-active': activeTab === 'data' }" @click="activeTab = 'data'">Data</button>
            <button :class="{ 'is-active': activeTab === 'structure' }" @click="activeTab = 'structure'">Structure</button>
            <button :class="{ 'is-active': activeTab === 'query' }" @click="activeTab = 'query'">Query</button>
          </div>

          <div v-if="activeTab === 'data'" class="cass-result">
            <div v-if="loadingRows" class="cass-loading">Loading rows...</div>
            <table v-else class="cass-data-table">
              <thead>
                <tr>
                  <th v-for="col in visibleColumns" :key="col">{{ col }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, idx) in result.rows" :key="idx">
                  <td v-for="col in visibleColumns" :key="col">{{ rowValue(row, col) }}</td>
                </tr>
                <tr v-if="!result.rows.length">
                  <td :colspan="Math.max(visibleColumns.length, 1)" class="cass-empty-cell">No rows loaded.</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div v-else-if="activeTab === 'structure'" class="cass-structure">
            <div v-for="col in columns" :key="col.name" class="cass-column">
              <div>
                <strong>{{ col.name }}</strong>
                <span>{{ col.type }}</span>
              </div>
              <small>{{ col.kind }} · {{ col.position }}</small>
            </div>
          </div>

          <div v-else class="cass-query">
            <textarea v-model="cql" class="cass-editor" spellcheck="false" />
            <div class="cass-query__actions">
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="loadingRows" @click="runQuery">Run CQL</button>
              <span class="cass-muted">{{ queryResult.row_count }} rows · {{ queryResult.duration_ms }} ms</span>
            </div>
            <div class="cass-result cass-result--query">
              <table class="cass-data-table">
                <thead>
                  <tr>
                    <th v-for="col in queryResult.columns" :key="col">{{ col }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, idx) in queryResult.rows" :key="idx">
                    <td v-for="col in queryResult.columns" :key="col">{{ rowValue(row, col) }}</td>
                  </tr>
                  <tr v-if="!queryResult.rows.length">
                    <td :colspan="Math.max(queryResult.columns.length, 1)" class="cass-empty-cell">Run a query to see results.</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>
      </main>
    </div>
  </div>
</template>

<style scoped>
.cass-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px;
  overflow: hidden;
}
.cass-toolbar,
.cass-workbench__bar,
.cass-actions,
.cass-summary {
  display: flex;
  align-items: center;
}
.cass-toolbar {
  justify-content: space-between;
  gap: 12px;
}
.cass-title {
  font-size: 22px;
  font-weight: 800;
  color: var(--text);
}
.cass-actions {
  gap: 8px;
}
.cass-select {
  width: 260px;
}
.cass-grid {
  min-height: 0;
  flex: 1;
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 12px;
}
.cass-sidebar,
.cass-workbench,
.cass-empty,
.cass-stat {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 8px;
}
.cass-sidebar {
  min-height: 0;
  padding: 10px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.cass-panel-head {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  font-weight: 800;
  color: var(--text);
  text-transform: uppercase;
}
.cass-panel-head--spaced {
  margin-top: 14px;
}
.cass-muted,
.cass-keyspace small,
.cass-table small,
.cass-workbench__bar small,
.cass-column small {
  color: var(--text-muted);
}
.cass-keyspaces,
.cass-tables {
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 8px;
}
.cass-keyspaces {
  max-height: 210px;
}
.cass-tables {
  min-height: 0;
}
.cass-keyspace,
.cass-table {
  border: 0;
  background: transparent;
  color: var(--text);
  text-align: left;
  padding: 8px;
  border-radius: 6px;
  display: flex;
  justify-content: space-between;
  gap: 8px;
  cursor: pointer;
}
.cass-keyspace:hover,
.cass-table:hover,
.cass-keyspace.is-active,
.cass-table.is-active,
.cass-tabs button.is-active {
  background: var(--surface-hover);
}
.cass-search {
  margin-top: 8px;
}
.cass-main {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.cass-summary {
  gap: 8px;
  border: 0;
  background: transparent;
}
.cass-stat {
  flex: 1;
  padding: 12px;
}
.cass-stat span {
  display: block;
  font-size: 12px;
  color: var(--text-muted);
}
.cass-stat strong {
  display: block;
  margin-top: 4px;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cass-workbench {
  min-height: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.cass-workbench__bar {
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
}
.cass-workbench__bar strong,
.cass-workbench__bar small {
  display: block;
}
.cass-limit {
  width: 82px;
}
.cass-tabs {
  display: flex;
  gap: 4px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}
.cass-tabs button {
  border: 0;
  background: transparent;
  color: var(--text);
  padding: 7px 10px;
  border-radius: 6px;
  font-weight: 700;
  cursor: pointer;
}
.cass-result {
  min-height: 0;
  flex: 1;
  overflow: auto;
}
.cass-result--query {
  max-height: 320px;
  border-top: 1px solid var(--border);
}
.cass-data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.cass-data-table th,
.cass-data-table td {
  border-bottom: 1px solid var(--border);
  border-right: 1px solid var(--border);
  padding: 7px 9px;
  text-align: left;
  white-space: nowrap;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.cass-data-table th {
  position: sticky;
  top: 0;
  background: var(--surface);
  z-index: 1;
  color: var(--text);
}
.cass-empty-cell,
.cass-loading {
  color: var(--text-muted);
  padding: 18px;
}
.cass-structure {
  padding: 12px;
  overflow: auto;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
  gap: 8px;
}
.cass-column {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px;
}
.cass-column div {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.cass-column strong,
.cass-column span {
  color: var(--text);
}
.cass-query {
  min-height: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
}
.cass-editor {
  min-height: 180px;
  resize: vertical;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  outline: none;
}
.cass-query__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}
.cass-empty {
  padding: 24px;
  display: flex;
  align-items: center;
  gap: 16px;
}
.cass-empty__mark {
  width: 46px;
  height: 46px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: #1f6feb;
  color: white;
  font-weight: 800;
}
.cass-empty h2 {
  margin: 0 0 4px;
  color: var(--text);
}
.cass-empty p {
  margin: 0;
  color: var(--text-muted);
}
@media (max-width: 900px) {
  .cass-page {
    overflow: auto;
  }
  .cass-toolbar,
  .cass-workbench__bar {
    align-items: stretch;
    flex-direction: column;
  }
  .cass-grid {
    grid-template-columns: 1fr;
  }
  .cass-sidebar {
    max-height: 420px;
  }
  .cass-summary {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .cass-select {
    width: 100%;
  }
}
</style>
