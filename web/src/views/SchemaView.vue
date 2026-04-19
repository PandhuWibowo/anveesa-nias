<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ConnectionPicker from '@/components/ui/ConnectionPicker.vue'
import DataTable from '@/components/database/DataTable.vue'
import SchemaExplorerTree from '@/components/database/SchemaExplorerTree.vue'
import {
  useSchema,
  type SchemaObjectDetail,
  type SchemaProperty,
} from '@/composables/useSchema'
import { useConnections } from '@/composables/useConnections'

const props = defineProps<{ activeConnId?: number | null }>()

const { connections } = useConnections()
const { databases, loadingSchema, metadata, objectDetail, fetchSchema, fetchMetadata, fetchObjectDetail } = useSchema()

const localConnId = ref<number | null>(props.activeConnId ?? null)
const activeDatabase = ref('')
const selectedKey = ref('')
const detailLoading = ref(false)

watch(() => props.activeConnId, (id) => {
  if (id != null) localConnId.value = id
}, { immediate: true })

watch(localConnId, async (id) => {
  metadata.value = null
  objectDetail.value = null
  selectedKey.value = ''
  activeDatabase.value = ''
  if (!id) return
  await fetchSchema(id)
  activeDatabase.value = databases.value[0]?.name ?? ''
}, { immediate: true })

watch(activeDatabase, async (dbName) => {
  metadata.value = null
  objectDetail.value = null
  selectedKey.value = ''
  if (!localConnId.value || !dbName) return
  const catalog = await fetchMetadata(localConnId.value, dbName)
  const firstItem = catalog?.groups.find(group => group.items.length > 0)?.items[0]
  if (firstItem) {
    await selectObject({ type: firstItem.type, name: firstItem.name })
  }
})

const activeConn = computed(() =>
  localConnId.value ? connections.value.find(c => c.id === localConnId.value) ?? null : null
)

async function selectObject(payload: { type: string; name: string }) {
  if (!localConnId.value || !activeDatabase.value) return
  selectedKey.value = `${payload.type}:${payload.name}`
  detailLoading.value = true
  await fetchObjectDetail(localConnId.value, activeDatabase.value, payload.type, payload.name)
  detailLoading.value = false
}

function rowsForProperties(properties: SchemaProperty[]) {
  return properties.map((property) => [property.label, property.value])
}

const indexRows = computed(() => (objectDetail.value?.indexes ?? []).map((index) => [
  index.name,
  index.table_name,
  index.method,
  index.is_unique ? 'YES' : 'NO',
  index.is_primary ? 'YES' : '',
  index.columns.join(', '),
]))

const constraintRows = computed(() => (objectDetail.value?.constraints ?? []).map((constraint) => [
  constraint.name,
  constraint.constraint_type,
  constraint.columns.join(', '),
  constraint.referenced_table ?? '',
  constraint.definition,
]))

const triggerRows = computed(() => (objectDetail.value?.triggers ?? []).map((trigger) => [
  trigger.name,
  trigger.table_name,
  trigger.timing,
  trigger.events,
]))

const sequenceRows = computed(() => (objectDetail.value?.sequences ?? []).map((sequence) => [
  sequence.name,
  sequence.start_value,
  sequence.increment_by,
  sequence.cache_size,
  sequence.cycle ? 'YES' : 'NO',
  sequence.owned_by ?? '',
]))

const columnRows = computed(() => (objectDetail.value?.columns ?? []).map((column) => [
  column.name,
  column.data_type,
  column.is_nullable ? 'YES' : 'NO',
  column.is_primary_key ? 'YES' : '',
  column.default_value ?? '',
]))
</script>

<template>
  <div class="schema-explorer">
    <div class="schema-explorer__toolbar">
      <div class="schema-explorer__title-wrap">
        <div class="schema-explorer__title">Schema Explorer</div>
        <div class="schema-explorer__subtitle">Browse tables, views, indexes, sequences, triggers, routines, types, DDL, and structural metadata.</div>
      </div>
      <div class="schema-explorer__controls">
        <ConnectionPicker v-model="localConnId" placeholder="Pick a connection…" />
        <select v-model="activeDatabase" class="base-input" :disabled="!databases.length">
          <option value="" disabled>Select database/schema…</option>
          <option v-for="database in databases" :key="database.name" :value="database.name">{{ database.name }}</option>
        </select>
      </div>
    </div>

    <div class="schema-explorer__layout">
      <aside class="schema-explorer__sidebar">
        <div class="schema-explorer__sidebar-head">
          <span>{{ activeConn?.name ?? 'No connection' }}</span>
          <span>{{ activeDatabase || 'Schema' }}</span>
        </div>
        <div v-if="loadingSchema" class="schema-explorer__empty">Loading database structure…</div>
        <div v-else-if="!activeConn" class="schema-explorer__empty">Select a connection to inspect database structure.</div>
        <div v-else-if="!activeDatabase" class="schema-explorer__empty">Choose a database or schema to load structural objects.</div>
        <SchemaExplorerTree
          v-else
          :catalog="metadata"
          :selected-key="selectedKey"
          @select-object="selectObject"
        />
      </aside>

      <section class="schema-explorer__detail">
        <div v-if="detailLoading" class="schema-explorer__empty">Loading object detail…</div>
        <div v-else-if="!objectDetail" class="schema-explorer__empty">Select an object to inspect its DDL and structure.</div>
        <template v-else>
          <div class="schema-explorer__hero">
            <div>
              <div class="schema-explorer__object-kicker">{{ objectDetail.type }}</div>
              <div class="schema-explorer__object-title">{{ objectDetail.name }}</div>
              <div class="schema-explorer__object-sub">{{ objectDetail.database }}</div>
            </div>
          </div>

          <div class="schema-explorer__grid">
            <div class="schema-explorer__panel">
              <div class="schema-explorer__panel-title">Properties</div>
              <DataTable
                :columns="['Property', 'Value']"
                :rows="rowsForProperties(objectDetail.properties)"
                :show-row-numbers="false"
              />
            </div>
            <div v-if="objectDetail.enum_values?.length" class="schema-explorer__panel">
              <div class="schema-explorer__panel-title">Enum Values</div>
              <DataTable
                :columns="['Value']"
                :rows="objectDetail.enum_values.map((value) => [value])"
                :show-row-numbers="false"
              />
            </div>
          </div>

          <div class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">DDL</div>
            <pre class="schema-explorer__code">{{ objectDetail.ddl || '-- definition unavailable for this driver/object' }}</pre>
          </div>

          <div v-if="objectDetail.routine" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Routine</div>
            <DataTable
              :columns="['Field', 'Value']"
              :rows="[
                ['Type', objectDetail.routine.routine_type],
                ['Identity', objectDetail.routine.identity],
                ['Return Type', objectDetail.routine.return_type || ''],
              ]"
              :show-row-numbers="false"
            />
          </div>

          <div v-if="objectDetail.columns.length" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Columns</div>
            <DataTable
              :columns="['Name', 'Type', 'Nullable', 'Primary Key', 'Default']"
              :rows="columnRows"
              :show-row-numbers="false"
            />
          </div>

          <div v-if="objectDetail.indexes.length" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Indexes</div>
            <DataTable
              :columns="['Name', 'Table', 'Method', 'Unique', 'Primary', 'Columns']"
              :rows="indexRows"
              :show-row-numbers="false"
            />
          </div>

          <div v-if="objectDetail.constraints.length" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Constraints</div>
            <DataTable
              :columns="['Name', 'Type', 'Columns', 'References', 'Definition']"
              :rows="constraintRows"
              :show-row-numbers="false"
            />
          </div>

          <div v-if="objectDetail.triggers.length" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Triggers</div>
            <DataTable
              :columns="['Name', 'Table', 'Timing', 'Events']"
              :rows="triggerRows"
              :show-row-numbers="false"
            />
          </div>

          <div v-if="objectDetail.sequences.length" class="schema-explorer__panel">
            <div class="schema-explorer__panel-title">Sequences</div>
            <DataTable
              :columns="['Name', 'Start', 'Increment', 'Cache', 'Cycle', 'Owned By']"
              :rows="sequenceRows"
              :show-row-numbers="false"
            />
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<style scoped>
.schema-explorer { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.schema-explorer__toolbar {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 18px 22px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}
.schema-explorer__title { font-size: 20px; font-weight: 800; color: var(--text-primary); }
.schema-explorer__subtitle { margin-top: 6px; color: var(--text-secondary); max-width: 760px; line-height: 1.55; }
.schema-explorer__controls { display: flex; gap: 10px; align-items: center; }
.schema-explorer__layout { flex: 1; min-height: 0; display: grid; grid-template-columns: 320px minmax(0, 1fr); }
.schema-explorer__sidebar { border-right: 1px solid var(--border); background: var(--bg-surface); display: flex; flex-direction: column; min-height: 0; }
.schema-explorer__sidebar-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
}
.schema-explorer__detail { min-height: 0; overflow: auto; padding: 18px; display: flex; flex-direction: column; gap: 14px; }
.schema-explorer__empty { display: grid; place-items: center; min-height: 240px; text-align: center; color: var(--text-muted); padding: 20px; }
.schema-explorer__hero {
  padding: 18px 20px;
  border: 1px solid color-mix(in srgb, var(--border) 78%, #0f766e 22%);
  border-radius: 16px;
  background:
    radial-gradient(circle at top left, rgba(15, 118, 110, 0.12), transparent 34%),
    linear-gradient(145deg, color-mix(in srgb, var(--bg-elevated) 88%, #f0fdfa 12%), var(--bg-elevated));
}
.schema-explorer__object-kicker { font-size: 11px; text-transform: uppercase; letter-spacing: .08em; font-weight: 800; color: #0f766e; }
.schema-explorer__object-title { font-size: 24px; font-weight: 800; color: var(--text-primary); margin-top: 4px; }
.schema-explorer__object-sub { color: var(--text-secondary); margin-top: 4px; }
.schema-explorer__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.schema-explorer__panel { border: 1px solid var(--border); border-radius: 16px; background: color-mix(in srgb, var(--bg-elevated) 86%, transparent); overflow: hidden; }
.schema-explorer__panel-title {
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.schema-explorer__code {
  margin: 0;
  padding: 16px;
  white-space: pre-wrap;
  overflow-x: auto;
  color: var(--text-primary);
  background: color-mix(in srgb, var(--bg-app) 92%, #031918 8%);
}

@media (max-width: 960px) {
  .schema-explorer__toolbar { flex-direction: column; }
  .schema-explorer__controls { flex-direction: column; align-items: stretch; }
  .schema-explorer__layout { grid-template-columns: 1fr; }
  .schema-explorer__grid { grid-template-columns: 1fr; }
}
</style>
