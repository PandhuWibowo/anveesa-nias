<script setup lang="ts">
import { ref, watch } from 'vue'
import { useSchema, type SchemaTable } from '@/composables/useSchema'

const props = defineProps<{
  connId: number | null
}>()

const emit = defineEmits<{
  (e: 'select-table', payload: { db: string; table: string; type: string }): void
}>()

const { databases, loadingSchema, fetchSchema } = useSchema()

const expandedDbs = ref<Set<string>>(new Set())
const activeTable = ref<string>('')

watch(
  () => props.connId,
  (id) => {
    if (id) fetchSchema(id)
  },
  { immediate: true },
)

function toggleDb(name: string) {
  if (expandedDbs.value.has(name)) expandedDbs.value.delete(name)
  else expandedDbs.value.add(name)
}

function selectTable(db: string, table: SchemaTable) {
  activeTable.value = `${db}.${table.name}`
  emit('select-table', { db, table: table.name, type: table.type })
}
</script>

<template>
  <div class="schema-tree">
    <div v-if="loadingSchema" style="padding:12px 8px;display:flex;align-items:center;gap:8px;color:var(--text-muted);font-size:12px">
      <svg class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Loading schema…
    </div>

    <div v-else-if="!connId" class="empty-state" style="padding:20px 8px;font-size:12px">
      Select a connection to browse the schema.
    </div>

    <div v-else-if="databases.length === 0" class="empty-state" style="padding:20px 8px;font-size:12px">
      No databases found.
    </div>

    <template v-else>
      <div v-for="db in databases" :key="db.name">
        <!-- Database node -->
        <div
          class="schema-node"
          style="padding-left:4px;font-weight:600"
          :class="{ 'is-active': expandedDbs.has(db.name) }"
          @click="toggleDb(db.name)"
        >
          <span class="schema-node__chevron" :class="{ 'is-open': expandedDbs.has(db.name) }">
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>
          </span>
          <svg class="schema-node__icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/></svg>
          <span class="schema-node__label">{{ db.name }}</span>
          <span class="schema-node__count">{{ db.tables.length }}</span>
        </div>

        <!-- Tables -->
        <template v-if="expandedDbs.has(db.name)">
          <div
            v-for="table in db.tables"
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
