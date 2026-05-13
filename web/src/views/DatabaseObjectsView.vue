<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'
import { useSchema, type SchemaObjectItem, type SchemaObjectDetail } from '@/composables/useSchema'
import { pendingSQL } from '@/composables/usePendingSQL'

const props = defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const router = useRouter()
const { connections } = useConnections()
const { databases, error: dbError, fetchDatabases } = useDatabases()
const { fetchMetadata, fetchObjectDetail, metadata } = useSchema()

const connId = ref<number | null>(null)
const activeDb = ref('')
const activeTab = ref('indexes')
const search = ref('')
const selectedItem = ref<SchemaObjectItem | null>(null)
const detail = ref<SchemaObjectDetail | null>(null)
const detailLoading = ref(false)
const metadataLoading = ref(false)
const copyDone = ref(false)

const tabDefs = [
  { key: 'indexes',           label: 'Indexes',       icon: 'IX' },
  { key: 'views',             label: 'Views',         icon: 'VW' },
  { key: 'materialized_views',label: 'Mat. Views',    icon: 'MV' },
  { key: 'functions',         label: 'Functions',     icon: 'FN' },
  { key: 'procedures',        label: 'Procedures',    icon: 'PR' },
  { key: 'triggers',          label: 'Triggers',      icon: 'TR' },
  { key: 'sequences',         label: 'Sequences',     icon: 'SQ' },
  { key: 'types',             label: 'Types',         icon: 'TY' },
]

const activeConn = computed(() =>
  connId.value != null ? connections.value.find(c => c.id === connId.value) ?? null : null
)

function isNonSqlDriver(driver: string) {
  return driver === 'redis' || driver === 'memcache' || driver === 'kafka' || driver === 'elasticsearch' || driver === 'opensearch' || driver === 's3_aws' || driver === 's3_gcp' || driver === 's3_oss' || driver === 's3_obs'
}

const availableTabs = computed(() => {
  if (!metadata.value) return []
  return tabDefs.filter(t => {
    const group = metadata.value!.groups.find(g => g.key === t.key)
    return group && group.items.length > 0
  })
})

const totalObjects = computed(() => {
  if (!metadata.value) return 0
  return tabDefs.reduce((sum, t) => {
    const g = metadata.value!.groups.find(g => g.key === t.key)
    return sum + (g?.items.length ?? 0)
  }, 0)
})

const currentGroup = computed(() => {
  if (!metadata.value) return null
  return metadata.value.groups.find(g => g.key === activeTab.value) ?? null
})

const filteredItems = computed(() => {
  const items = currentGroup.value?.items ?? []
  const q = search.value.toLowerCase().trim()
  if (!q) return items
  return items.filter(i =>
    i.name.toLowerCase().includes(q) ||
    (i.parent_name ?? '').toLowerCase().includes(q) ||
    (i.summary ?? '').toLowerCase().includes(q)
  )
})

// Sync with parent's activeConnId
watch(() => props.activeConnId, async (id) => {
  if (id == null) return
  const conn = connections.value.find(c => c.id === id)
  if (!conn || isNonSqlDriver(conn.driver)) return
  if (connId.value === id) return
  connId.value = id
}, { immediate: true })

watch(connId, async (id) => {
  activeDb.value = ''
  metadata.value = null
  selectedItem.value = null
  detail.value = null
  if (!id) return
  await fetchDatabases(id)
  activeDb.value = databases.value[0] ?? ''
})

watch(activeDb, async (db) => {
  selectedItem.value = null
  detail.value = null
  if (!connId.value || !db) return
  metadataLoading.value = true
  await fetchMetadata(connId.value, db)
  metadataLoading.value = false
  if (availableTabs.value.length > 0) {
    activeTab.value = availableTabs.value[0].key
  }
})

watch(activeTab, () => {
  selectedItem.value = null
  detail.value = null
  search.value = ''
})

async function selectItem(item: SchemaObjectItem) {
  selectedItem.value = item
  detail.value = null
  if (!connId.value || !activeDb.value) return
  detailLoading.value = true
  detail.value = await fetchObjectDetail(connId.value, activeDb.value, item.type, item.name)
  detailLoading.value = false
}

function copyDDL() {
  const ddl = detail.value?.ddl
  if (!ddl) return
  navigator.clipboard.writeText(ddl).then(() => {
    copyDone.value = true
    setTimeout(() => { copyDone.value = false }, 1500)
  })
}

function openInSQL() {
  const ddl = detail.value?.ddl
  if (!ddl) return
  pendingSQL.value = ddl
  router.push({ name: 'data' })
}

function itemBadge(item: SchemaObjectItem): string {
  if (activeTab.value === 'indexes') {
    if (item.summary?.includes('primary')) return 'PK'
    if (item.summary?.includes('unique')) return 'UQ'
  }
  return ''
}

function itemMeta(item: SchemaObjectItem): string {
  return item.parent_name ?? item.summary ?? ''
}

function countForTab(key: string): number {
  if (!metadata.value) return 0
  return metadata.value.groups.find(g => g.key === key)?.items.length ?? 0
}
</script>

<template>
  <div class="page-shell dbo-root">
    <!-- Toolbar -->
    <div class="dbo-toolbar">
      <div class="dbo-toolbar__left">
        <div class="dbo-brand">
          <span class="dbo-brand__icon">&#9632;</span>
          <span class="dbo-brand__label">Database Objects</span>
        </div>
        <div class="dbo-selectors">
          <div class="dbo-selector">
            <label class="dbo-selector__label">Connection</label>
            <select
              class="dbo-select"
              :value="connId"
              @change="connId = Number(($event.target as HTMLSelectElement).value) || null"
            >
              <option :value="null">— select —</option>
              <option
                v-for="c in connections.filter(c => !isNonSqlDriver(c.driver))"
                :key="c.id"
                :value="c.id"
              >{{ c.name }}</option>
            </select>
          </div>
          <div v-if="databases.length > 0" class="dbo-selector">
            <label class="dbo-selector__label">Database</label>
            <select class="dbo-select" v-model="activeDb">
              <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
            </select>
          </div>
        </div>
      </div>

      <div v-if="metadata && !metadataLoading" class="dbo-toolbar__right">
        <span class="dbo-count">{{ totalObjects.toLocaleString() }} objects in {{ activeDb }}</span>
        <input
          v-model="search"
          class="dbo-search"
          :placeholder="`Search ${currentGroup?.label ?? 'objects'}…`"
          type="text"
        />
      </div>
    </div>

    <!-- Empty / loading states -->
    <div v-if="!connId" class="dbo-empty">
      <div class="dbo-empty__icon">&#9632;</div>
      <div class="dbo-empty__title">No connection selected</div>
      <div class="dbo-empty__sub">Choose a database connection above to browse its objects.</div>
    </div>
    <div v-else-if="metadataLoading" class="dbo-empty">
      <svg class="spin" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      <div class="dbo-empty__sub">Loading schema objects…</div>
    </div>
    <div v-else-if="dbError" class="dbo-empty">
      <div class="dbo-empty__icon dbo-empty__icon--error">&#9888;</div>
      <div class="dbo-empty__title">Cannot connect to database</div>
      <div class="dbo-empty__sub">{{ dbError }}</div>
      <div class="dbo-empty__hint">Check that the database server for this connection is running and reachable.</div>
    </div>
    <div v-else-if="!metadata" class="dbo-empty">
      <div class="dbo-empty__icon">&#9632;</div>
      <div class="dbo-empty__title">No data</div>
      <div class="dbo-empty__sub">Select a database to load its object catalog.</div>
    </div>
    <div v-else-if="availableTabs.length === 0" class="dbo-empty">
      <div class="dbo-empty__title">No browsable objects</div>
      <div class="dbo-empty__sub">This database has no indexes, views, functions, triggers, sequences, or types.</div>
    </div>

    <!-- Main layout -->
    <div v-else class="dbo-body">
      <!-- Object type sidebar -->
      <div class="dbo-typelist">
        <button
          v-for="t in availableTabs"
          :key="t.key"
          class="dbo-type-btn"
          :class="{ 'dbo-type-btn--active': activeTab === t.key }"
          @click="activeTab = t.key"
        >
          <span class="dbo-type-btn__badge">{{ t.icon }}</span>
          <span class="dbo-type-btn__label">{{ t.label }}</span>
          <span class="dbo-type-btn__count">{{ countForTab(t.key) }}</span>
        </button>
      </div>

      <!-- Object list -->
      <div class="dbo-list">
        <div v-if="filteredItems.length === 0" class="dbo-list__empty">
          {{ search ? 'No matches for "' + search + '"' : 'No objects in this category.' }}
        </div>
        <button
          v-for="item in filteredItems"
          :key="item.name"
          class="dbo-item"
          :class="{ 'dbo-item--active': selectedItem?.name === item.name }"
          @click="selectItem(item)"
        >
          <div class="dbo-item__name">{{ item.name }}</div>
          <div v-if="itemMeta(item)" class="dbo-item__meta">{{ itemMeta(item) }}</div>
          <span v-if="itemBadge(item)" class="dbo-item__badge">{{ itemBadge(item) }}</span>
        </button>
      </div>

      <!-- Detail panel -->
      <div class="dbo-detail">
        <div v-if="!selectedItem" class="dbo-detail__empty">
          <div class="dbo-detail__empty-icon">&#9636;</div>
          <div>Select an object to view its details</div>
        </div>

        <div v-else-if="detailLoading" class="dbo-detail__empty">
          <svg class="spin" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <div>Loading…</div>
        </div>

        <template v-else-if="detail">
          <div class="dbo-detail__header">
            <div class="dbo-detail__name">{{ detail.name }}</div>
            <div class="dbo-detail__type">{{ detail.type }}</div>
            <div style="flex:1"/>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="copyDDL" :disabled="!detail.ddl">
              {{ copyDone ? 'Copied!' : 'Copy DDL' }}
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="openInSQL" :disabled="!detail.ddl">
              Open in SQL Studio
            </button>
          </div>

          <!-- Properties -->
          <div v-if="detail.properties?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Properties</div>
            <div class="dbo-props">
              <div v-for="p in detail.properties" :key="p.label" class="dbo-prop">
                <span class="dbo-prop__label">{{ p.label }}</span>
                <span class="dbo-prop__value">{{ p.value }}</span>
              </div>
            </div>
          </div>

          <!-- Columns (for views / tables) -->
          <div v-if="detail.columns?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Columns</div>
            <div class="dbo-table-wrap">
              <table class="dbo-table">
                <thead>
                  <tr><th>Name</th><th>Type</th><th>Nullable</th><th>Default</th></tr>
                </thead>
                <tbody>
                  <tr v-for="col in detail.columns" :key="col.name">
                    <td class="mono">{{ col.name }}<span v-if="col.is_primary_key" class="dbo-pk"> PK</span></td>
                    <td class="mono muted">{{ col.data_type }}</td>
                    <td class="muted">{{ col.is_nullable ? 'YES' : 'NO' }}</td>
                    <td class="mono muted small">{{ col.default_value ?? '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Indexes on this object -->
          <div v-if="detail.indexes?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Indexes</div>
            <div class="dbo-table-wrap">
              <table class="dbo-table">
                <thead>
                  <tr><th>Name</th><th>Method</th><th>Columns</th><th>Flags</th></tr>
                </thead>
                <tbody>
                  <tr v-for="ix in detail.indexes" :key="ix.name">
                    <td class="mono">{{ ix.name }}</td>
                    <td class="muted">{{ ix.method }}</td>
                    <td class="mono small">{{ ix.columns?.join(', ') }}</td>
                    <td>
                      <span v-if="ix.is_primary" class="dbo-flag dbo-flag--pk">PK</span>
                      <span v-if="ix.is_unique" class="dbo-flag dbo-flag--uq">UQ</span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Constraints -->
          <div v-if="detail.constraints?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Constraints</div>
            <div class="dbo-table-wrap">
              <table class="dbo-table">
                <thead>
                  <tr><th>Name</th><th>Type</th><th>Columns</th><th>References</th></tr>
                </thead>
                <tbody>
                  <tr v-for="c in detail.constraints" :key="c.name">
                    <td class="mono">{{ c.name }}</td>
                    <td class="muted small">{{ c.constraint_type }}</td>
                    <td class="mono small">{{ c.columns?.join(', ') }}</td>
                    <td class="mono muted small">{{ c.referenced_table ?? '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Triggers on this object -->
          <div v-if="detail.triggers?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Triggers</div>
            <div class="dbo-table-wrap">
              <table class="dbo-table">
                <thead>
                  <tr><th>Name</th><th>Timing</th><th>Events</th></tr>
                </thead>
                <tbody>
                  <tr v-for="t in detail.triggers" :key="t.name">
                    <td class="mono">{{ t.name }}</td>
                    <td class="muted small">{{ t.timing }}</td>
                    <td class="muted small">{{ t.events }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Enum values (for types) -->
          <div v-if="detail.enum_values?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Enum Values</div>
            <div class="dbo-enum-list">
              <span v-for="v in detail.enum_values" :key="v" class="dbo-enum-val">{{ v }}</span>
            </div>
          </div>

          <!-- Dependencies -->
          <div v-if="detail.dependencies?.length" class="dbo-detail__section">
            <div class="dbo-detail__section-title">Dependencies</div>
            <div class="dbo-props">
              <div v-for="d in detail.dependencies" :key="d.label" class="dbo-prop">
                <span class="dbo-prop__label">{{ d.label }}</span>
                <span class="dbo-prop__value mono">{{ d.value }}</span>
              </div>
            </div>
          </div>

          <!-- DDL -->
          <div v-if="detail.ddl" class="dbo-detail__section dbo-detail__section--ddl">
            <div class="dbo-detail__section-title">DDL</div>
            <pre class="dbo-ddl">{{ detail.ddl }}</pre>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ── Shell ─────────────────────────────────────────────── */
.dbo-root {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-body);
  overflow: hidden;
}

/* ── Toolbar ───────────────────────────────────────────── */
.dbo-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 16px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  flex-wrap: wrap;
}
.dbo-toolbar__left  { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.dbo-toolbar__right { display: flex; align-items: center; gap: 12px; }

.dbo-brand { display: flex; align-items: center; gap: 8px; }
.dbo-brand__icon  { color: var(--brand, #5b8dee); font-size: 14px; }
.dbo-brand__label { font-weight: 700; font-size: 14px; color: var(--text-primary); white-space: nowrap; }

.dbo-selectors { display: flex; align-items: flex-end; gap: 10px; flex-wrap: wrap; }
.dbo-selector  { display: flex; flex-direction: column; gap: 3px; }
.dbo-selector__label {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.4px; color: var(--text-muted);
}
.dbo-select {
  padding: 5px 10px; background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 13px; min-width: 160px;
  outline: none; cursor: pointer;
}
.dbo-select:focus { border-color: var(--brand, #5b8dee); }

.dbo-count { font-size: 12px; color: var(--text-muted); white-space: nowrap; }
.dbo-search {
  padding: 5px 10px; background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 13px; width: 220px;
  outline: none;
}
.dbo-search:focus { border-color: var(--brand, #5b8dee); }

/* ── Empty state ───────────────────────────────────────── */
.dbo-empty {
  flex: 1; display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 10px; color: var(--text-muted);
  padding: 40px 20px; text-align: center;
}
.dbo-empty__icon  { font-size: 32px; opacity: 0.3; }
.dbo-empty__icon--error { color: #f87171; opacity: 0.8; }
.dbo-empty__title { font-weight: 600; font-size: 15px; color: var(--text-secondary); }
.dbo-empty__sub   { font-size: 13px; }
.dbo-empty__hint  { font-size: 12px; color: var(--text-muted); font-style: italic; }

/* ── Main body ─────────────────────────────────────────── */
.dbo-body {
  flex: 1; display: flex; overflow: hidden;
}

/* ── Type sidebar ──────────────────────────────────────── */
.dbo-typelist {
  width: 160px; flex-shrink: 0; border-right: 1px solid var(--border);
  background: var(--bg-surface); display: flex; flex-direction: column;
  padding: 8px 6px; gap: 2px; overflow-y: auto;
}
.dbo-type-btn {
  display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 7px 10px; border-radius: 6px; border: none; cursor: pointer;
  background: none; color: var(--text-secondary); font-size: 12.5px;
  text-align: left; transition: background 0.1s, color 0.1s;
}
.dbo-type-btn:hover { background: var(--bg-elevated); color: var(--text-primary); }
.dbo-type-btn--active {
  background: var(--brand-subtle, rgba(91,141,238,0.12));
  color: var(--brand, #5b8dee); font-weight: 600;
}
.dbo-type-btn__badge {
  font-size: 9.5px; font-weight: 700; font-family: var(--mono, monospace);
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 3px; padding: 1px 4px; color: var(--text-muted);
  flex-shrink: 0;
}
.dbo-type-btn--active .dbo-type-btn__badge {
  background: var(--brand-subtle, rgba(91,141,238,0.15));
  border-color: var(--brand, #5b8dee); color: var(--brand, #5b8dee);
}
.dbo-type-btn__label { flex: 1; }
.dbo-type-btn__count {
  font-size: 10.5px; font-weight: 700; color: var(--text-muted);
  background: var(--bg-body); border-radius: 10px; padding: 1px 6px;
  min-width: 20px; text-align: center;
}

/* ── Object list ───────────────────────────────────────── */
.dbo-list {
  width: 260px; flex-shrink: 0; border-right: 1px solid var(--border);
  overflow-y: auto; display: flex; flex-direction: column; padding: 6px 4px;
}
.dbo-list__empty {
  padding: 24px 16px; text-align: center; font-size: 12.5px; color: var(--text-muted);
}
.dbo-item {
  display: flex; flex-direction: column; align-items: flex-start; gap: 2px;
  padding: 7px 10px; border-radius: 6px; border: none; cursor: pointer;
  background: none; width: 100%; text-align: left; position: relative;
  transition: background 0.1s;
}
.dbo-item:hover { background: var(--bg-elevated); }
.dbo-item--active { background: var(--brand-subtle, rgba(91,141,238,0.1)); }
.dbo-item__name {
  font-size: 12.5px; font-weight: 500; color: var(--text-primary);
  font-family: var(--mono, monospace); word-break: break-all;
}
.dbo-item--active .dbo-item__name { color: var(--brand, #5b8dee); }
.dbo-item__meta  { font-size: 11px; color: var(--text-muted); font-family: var(--mono, monospace); }
.dbo-item__badge {
  position: absolute; right: 8px; top: 8px;
  font-size: 9px; font-weight: 700; padding: 1px 5px; border-radius: 3px;
  background: var(--brand-subtle, rgba(91,141,238,0.15));
  color: var(--brand, #5b8dee);
}

/* ── Detail panel ──────────────────────────────────────── */
.dbo-detail {
  flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 0;
}
.dbo-detail__empty {
  flex: 1; display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 10px; color: var(--text-muted);
  font-size: 13px; padding: 40px;
}
.dbo-detail__empty-icon { font-size: 28px; opacity: 0.2; }

.dbo-detail__header {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
  background: var(--bg-surface); position: sticky; top: 0; z-index: 2;
  flex-shrink: 0;
}
.dbo-detail__name {
  font-size: 14px; font-weight: 700; color: var(--text-primary);
  font-family: var(--mono, monospace);
}
.dbo-detail__type {
  font-size: 10.5px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.5px; color: var(--text-muted);
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 4px; padding: 2px 7px;
}

.dbo-detail__section {
  padding: 14px 16px; border-bottom: 1px solid var(--border);
}
.dbo-detail__section-title {
  font-size: 10.5px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.5px; color: var(--text-muted); margin-bottom: 10px;
}

/* Properties */
.dbo-props { display: flex; flex-direction: column; gap: 6px; }
.dbo-prop  { display: flex; gap: 12px; font-size: 12.5px; align-items: flex-start; }
.dbo-prop__label {
  width: 140px; flex-shrink: 0; color: var(--text-muted); font-weight: 500;
}
.dbo-prop__value { color: var(--text-primary); font-size: 12.5px; word-break: break-all; }

/* Table */
.dbo-table-wrap { overflow-x: auto; }
.dbo-table {
  width: 100%; border-collapse: collapse; font-size: 12px;
}
.dbo-table th {
  text-align: left; padding: 5px 10px; font-size: 10.5px; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted);
  border-bottom: 1px solid var(--border); background: var(--bg-elevated);
}
.dbo-table td {
  padding: 6px 10px; border-bottom: 1px solid var(--border);
  color: var(--text-primary); vertical-align: top;
}
.dbo-table tr:last-child td { border-bottom: none; }
.dbo-table tr:hover td { background: var(--bg-elevated); }

.mono  { font-family: var(--mono, monospace); }
.muted { color: var(--text-muted); }
.small { font-size: 11.5px; }
.dbo-pk { font-size: 9px; font-weight: 700; color: var(--brand, #5b8dee); margin-left: 4px; }

/* Flags */
.dbo-flag {
  font-size: 9.5px; font-weight: 700; padding: 1px 5px; border-radius: 3px; margin-right: 3px;
}
.dbo-flag--pk { background: rgba(91,141,238,0.15); color: var(--brand, #5b8dee); }
.dbo-flag--uq { background: rgba(74,222,128,0.15); color: #4ade80; }

/* Enum values */
.dbo-enum-list { display: flex; flex-wrap: wrap; gap: 6px; }
.dbo-enum-val {
  font-size: 12px; font-family: var(--mono, monospace);
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 4px; padding: 2px 8px; color: var(--text-primary);
}

/* DDL */
.dbo-detail__section--ddl { flex: 1; }
.dbo-ddl {
  font-family: var(--mono, monospace); font-size: 12px; line-height: 1.6;
  color: var(--text-primary); background: var(--bg-elevated);
  border: 1px solid var(--border); border-radius: 6px;
  padding: 12px 14px; overflow-x: auto; white-space: pre;
  margin: 0;
}

/* Spin animation */
.spin { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
