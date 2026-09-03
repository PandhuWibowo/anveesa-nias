<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useSchema, type SchemaTable } from '@/composables/useSchema'

const props = defineProps<{
  connId: number | null
  active?: boolean
  selectedTable?: string
  refreshKey?: number
}>()

const emit = defineEmits<{
  (e: 'select-table', payload: { db: string; table: string; type: string }): void
}>()

const { databases, loadingSchema, error: schemaError, fetchSchema } = useSchema()

const expandedDbs = ref<Set<string>>(new Set())
const activeTable = ref<string>('')
const searchQuery = ref('')

// While searching, matching databases are force-expanded regardless of manual
// toggle state so results are visible without an extra click; clearing the
// search reverts to whatever the user had manually expanded.
function isDbExpanded(dbName: string) {
  return searchQuery.value.trim() ? filteredTablesFor(dbName).length > 0 : expandedDbs.value.has(dbName)
}

function filteredTablesFor(dbName: string) {
  const db = databases.value.find(d => d.name === dbName)
  const tables = db ? tablesFor(db) : []
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return tables
  return tables.filter(t => t.name.toLowerCase().includes(q))
}

const visibleDatabases = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return databases.value
  return databases.value.filter(db => filteredTablesFor(db.name).length > 0 || db.name.toLowerCase().includes(q))
})

watch(
  [() => props.connId, () => props.active, () => props.refreshKey],
  ([id, isActive], oldValues) => {
    const active = isActive !== false
    const didRefresh = oldValues !== undefined && oldValues[2] !== props.refreshKey
    if (id && active) fetchSchema(id, { refresh: didRefresh })
  },
  { immediate: true },
)

watch(() => props.selectedTable, (sel) => {
  if (!sel) return
  const dotIdx = sel.indexOf('.')
  const db = dotIdx !== -1 ? sel.slice(0, dotIdx) : sel
  if (db) expandedDbs.value.add(db)
  activeTable.value = sel
}, { immediate: true })

function toggleDb(name: string) {
  if (expandedDbs.value.has(name)) expandedDbs.value.delete(name)
  else expandedDbs.value.add(name)
}

function selectTable(db: string, table: SchemaTable) {
  activeTable.value = `${db}.${table.name}`
  emit('select-table', { db, table: table.name, type: table.type })
}

function tablesFor(db: { tables?: SchemaTable[] | null }) {
  return Array.isArray(db.tables) ? db.tables : []
}
</script>

<template>
  <div class="schema-tree">
    <div v-if="!loadingSchema && connId && !schemaError && databases.length > 0" class="schema-tree__search">
      <div class="schema-tree__search-box">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        <input v-model="searchQuery" type="text" placeholder="Search tables…" class="schema-tree__search-input" />
        <button v-if="searchQuery" class="schema-tree__search-clear" title="Clear search" @click="searchQuery = ''">✕</button>
      </div>
    </div>

    <div v-if="loadingSchema" style="padding:12px 8px;display:flex;align-items:center;gap:8px;color:var(--text-muted);font-size:12px">
      <svg class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Loading schema…
    </div>

    <div v-else-if="!connId" class="empty-state" style="padding:20px 8px;font-size:12px">
      Select a connection to browse the schema.
    </div>

    <div v-else-if="schemaError" class="notice notice--error" style="margin:8px;font-size:12px">
      {{ schemaError }}
    </div>

    <div v-else-if="databases.length === 0" class="empty-state" style="padding:20px 8px;font-size:12px">
      No databases found.
    </div>

    <div v-else-if="visibleDatabases.length === 0" class="empty-state" style="padding:20px 8px;font-size:12px">
      No tables match "{{ searchQuery }}".
    </div>

    <template v-else>
      <div v-for="db in visibleDatabases" :key="db.name">
        <!-- Database node -->
        <div
          class="schema-node"
          style="padding-left:4px;font-weight:600"
          :class="{ 'is-active': isDbExpanded(db.name) }"
          @click="toggleDb(db.name)"
        >
          <span class="schema-node__chevron" :class="{ 'is-open': isDbExpanded(db.name) }">
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>
          </span>
          <svg class="schema-node__icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/></svg>
          <span class="schema-node__label">{{ db.name }}</span>
          <span class="schema-node__count">{{ searchQuery.trim() ? filteredTablesFor(db.name).length : tablesFor(db).length }}</span>
        </div>

        <!-- Tables -->
        <template v-if="isDbExpanded(db.name)">
          <div
            v-for="table in filteredTablesFor(db.name)"
            :key="table.name"
            class="schema-node"
            style="padding-left:20px"
            :class="{ 'is-active': activeTable === `${db.name}.${table.name}` }"
            @click="selectTable(db.name, table)"
          >
            <svg class="schema-node__icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <template v-if="table.type === 'view'">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>
              </template>
              <template v-else>
                <rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>
              </template>
            </svg>
            <span class="schema-node__label">{{ table.name }}</span>
            <span class="col-type-badge" v-if="table.type === 'view'">view</span>
            <span class="schema-node__count" v-if="table.row_count !== undefined">{{ table.row_count.toLocaleString() }}</span>
          </div>
        </template>
      </div>
    </template>
  </div>
</template>
