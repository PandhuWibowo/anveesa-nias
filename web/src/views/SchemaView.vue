<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import SchemaTree from '@/components/database/SchemaTree.vue'
import DataTable from '@/components/database/DataTable.vue'
import ConnectionPicker from '@/components/ui/ConnectionPicker.vue'
import { useSchema } from '@/composables/useSchema'
import { useConnections } from '@/composables/useConnections'

const props = defineProps<{ activeConnId?: number | null }>()

const { connections } = useConnections()
const { columns, fetchColumns } = useSchema()

// Each view owns its own connection — initialized from the global hint but fully independent
const localConnId = ref<number | null>(props.activeConnId ?? null)

// When the sidebar picks a connection, sync it in only if we haven't overridden locally
const syncedFromProps = ref(false)
watch(() => props.activeConnId, (id) => {
  if (id != null && !syncedFromProps.value) {
    localConnId.value = id
  }
  if (id != null) syncedFromProps.value = true
}, { immediate: true })

// Reset table selection when connection changes
watch(localConnId, () => {
  selected.value = null
  columns.value = []
})

const activeConn = computed(() =>
  localConnId.value ? connections.value.find(c => c.id === localConnId.value) ?? null : null
)

const selected = ref<{ db: string; table: string; type: string } | null>(null)
const loadingCols = ref(false)

async function handleSelectTable(payload: { db: string; table: string; type: string }) {
  selected.value = payload
  loadingCols.value = true
  await fetchColumns(localConnId.value ?? 0, payload.db, payload.table)
  loadingCols.value = false
}

const columnRows = computed(() =>
  columns.value.map(c => [
    c.name,
    c.data_type,
    c.is_nullable ? 'YES' : 'NO',
    c.is_primary_key ? 'YES' : '',
    c.default_value ?? '',
  ]),
)

const driverColors: Record<string, string> = {
  postgres: '#336791', mysql: '#f29111', sqlite: '#7bc8f6', mssql: '#cc2927',
}
const driverLabels: Record<string, string> = {
  postgres: 'PG', mysql: 'MY', sqlite: 'SQ', mssql: 'MS',
}
function driverColor(driver: string) { return driverColors[driver] ?? '#555' }
function driverLabel(driver: string) { return driverLabels[driver] ?? '??' }
</script>

<template>
  <div style="display:flex;flex-direction:column;width:100%;height:100%;min-height:0;overflow:hidden">

    <!-- View toolbar -->
    <div class="view-toolbar">
      <div class="view-toolbar__left">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color:var(--brand);flex-shrink:0">
          <polygon points="12 2 2 7 12 12 22 7 12 2"/>
          <polyline points="2 17 12 22 22 17"/>
          <polyline points="2 12 12 17 22 12"/>
        </svg>
        <span class="view-toolbar__title">Schema Browser</span>
      </div>
      <div class="view-toolbar__right">
        <span class="view-toolbar__label">Connection</span>
        <ConnectionPicker v-model="localConnId" placeholder="Pick a connection…" />
      </div>
    </div>

    <!-- Body -->
    <div style="display:flex;flex:1;min-height:0;overflow:hidden">

      <!-- No connection selected -->
      <div v-if="!activeConn" class="view-no-conn">
        <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)">
          <path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/>
        </svg>
        <p class="view-no-conn__text">Select a connection above to browse its schema</p>
        <ConnectionPicker v-model="localConnId" placeholder="Choose connection…" />
      </div>

      <template v-else>
        <!-- Schema tree panel -->
        <div class="schema-tree-panel">
          <div class="panel-header">
            <span>{{ activeConn.name }}</span>
            <span class="driver-badge" :style="{ background: driverColor(activeConn.driver) }">
              {{ driverLabel(activeConn.driver) }}
            </span>
          </div>
          <SchemaTree :connId="localConnId" @select-table="handleSelectTable" />
        </div>

        <!-- Detail panel -->
        <div style="flex:1;min-width:0;display:flex;flex-direction:column;overflow:hidden">
          <div class="detail-header">
            <template v-if="selected">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color:var(--brand)">
                <rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>
              </svg>
              <span style="font-size:14px;font-weight:600;color:var(--text-primary)">{{ selected.db }}.{{ selected.table }}</span>
              <span class="badge badge--default" style="font-size:10px">{{ selected.type }}</span>
            </template>
            <span v-else style="font-size:13px;color:var(--text-muted)">Select a table from the schema tree</span>
          </div>

          <div style="flex:1;overflow:hidden" v-if="selected">
            <div style="padding:8px 14px;border-bottom:1px solid var(--border);display:flex;gap:8px;background:var(--bg-surface);flex-shrink:0;align-items:center">
              <span style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.4px;color:var(--text-muted)">Columns</span>
              <span class="badge badge--default">{{ columns.length }}</span>
            </div>
            <DataTable
              :columns="['Name', 'Type', 'Nullable', 'Primary Key', 'Default']"
              :rows="columnRows"
              :loading="loadingCols"
              :show-row-numbers="false"
            />
          </div>

          <div v-else class="empty-state">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)">
              <polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/>
            </svg>
            Select a table to inspect its schema.
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.view-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
  gap: 12px;
  overflow: visible;
  position: relative;
  z-index: 10;
}
.view-toolbar__left {
  display: flex;
  align-items: center;
  gap: 8px;
}
.view-toolbar__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.view-toolbar__right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.view-toolbar__label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
  font-weight: 600;
}

.view-no-conn {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--text-muted);
}
.view-no-conn__text {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}

.schema-tree-panel {
  width: 260px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  background: var(--bg-surface);
  overflow: hidden;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.driver-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 16px;
  border-radius: 3px;
  font-size: 9px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 0.3px;
}

.detail-header {
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  min-height: 40px;
}
</style>
