<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import ConnectionPicker from '@/components/ui/ConnectionPicker.vue'

// Mirrors server/handlers/pg_parameters.go's pgSetting JSON shape.
interface PgSetting {
  name: string
  setting: string
  unit: string
  category: string
  short_desc: string
  context: string
  vartype: string
  min_val: string
  max_val: string
  enumvals: string
  boot_val: string
  reset_val: string
  pending_restart: boolean
}

const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const { fetchConnections } = useConnections()
const { hasAnyPermission, isAdmin } = useAuth()

const canManage = computed(() => isAdmin.value || hasAnyPermission(['pgparameters.manage']))

const connId = ref<number | null>(route.query.connection_id ? Number(route.query.connection_id) : null)
const settings = ref<PgSetting[]>([])
const loading = ref(false)
const loaded = ref(false)
const reloading = ref(false)

const search = ref('')
const categoryFilter = ref('all')
const restartFilter = ref(false)

const categories = computed(() => {
  const set = new Set(settings.value.map(s => s.category).filter(Boolean))
  return Array.from(set).sort()
})

const filteredSettings = computed(() => {
  const q = search.value.trim().toLowerCase()
  return settings.value.filter(s => {
    if (categoryFilter.value !== 'all' && s.category !== categoryFilter.value) return false
    if (restartFilter.value && !s.pending_restart) return false
    if (!q) return true
    return s.name.toLowerCase().includes(q) || s.short_desc.toLowerCase().includes(q)
  })
})

const pendingRestartCount = computed(() => settings.value.filter(s => s.pending_restart).length)

async function loadSettings() {
  if (!connId.value) return
  loading.value = true
  try {
    const { data } = await axios.get<PgSetting[]>('/api/pg-parameters/settings', {
      params: { connection_id: connId.value },
    })
    settings.value = data ?? []
    loaded.value = true
  } catch (err: any) {
    toast.error(err?.response?.data?.error || 'Failed to load pg_settings')
    settings.value = []
  } finally {
    loading.value = false
  }
}

async function reloadConfig() {
  if (!connId.value) return
  reloading.value = true
  try {
    await axios.post('/api/pg-parameters/reload', null, { params: { connection_id: connId.value } })
    toast.success('Configuration reloaded')
    await loadSettings()
  } catch (err: any) {
    toast.error(err?.response?.data?.error || 'Reload failed')
  } finally {
    reloading.value = false
  }
}

const editingName = ref<string | null>(null)
const editValue = ref('')
const saving = ref<Set<string>>(new Set())

function canEdit(s: PgSetting): boolean {
  return canManage.value && s.context !== 'internal'
}

function startEdit(s: PgSetting) {
  if (!canEdit(s)) return
  editingName.value = s.name
  editValue.value = s.setting
}

function cancelEdit() {
  editingName.value = null
  editValue.value = ''
}

async function saveEdit(s: PgSetting) {
  if (!connId.value) return
  saving.value = new Set([...saving.value, s.name])
  try {
    const { data } = await axios.put(`/api/pg-parameters/settings/${encodeURIComponent(s.name)}`, {
      value: editValue.value,
    }, { params: { connection_id: connId.value } })
    s.setting = editValue.value
    s.pending_restart = !!data.pending_restart
    toast.success(
      data.pending_restart
        ? `${s.name} set — requires a Postgres restart to take effect`
        : `${s.name} updated`,
    )
    cancelEdit()
  } catch (err: any) {
    toast.error(err?.response?.data?.error || `Failed to update ${s.name}`)
  } finally {
    saving.value = new Set([...saving.value].filter(n => n !== s.name))
  }
}

async function resetSetting(s: PgSetting) {
  if (!connId.value) return
  const ok = await confirm(`Reset "${s.name}" to its default (${s.boot_val || 'boot default'})? Current value is "${s.setting}".`, 'Reset')
  if (!ok) return
  saving.value = new Set([...saving.value, s.name])
  try {
    const { data } = await axios.put(`/api/pg-parameters/settings/${encodeURIComponent(s.name)}`, {
      value: null,
    }, { params: { connection_id: connId.value } })
    s.setting = s.reset_val || s.boot_val
    s.pending_restart = !!data.pending_restart
    toast.success(`${s.name} reset to default`)
  } catch (err: any) {
    toast.error(err?.response?.data?.error || `Failed to reset ${s.name}`)
  } finally {
    saving.value = new Set([...saving.value].filter(n => n !== s.name))
  }
}

function enumOptions(s: PgSetting): string[] {
  return s.enumvals ? s.enumvals.split(',').map(v => v.trim()).filter(Boolean) : []
}

function formatValue(s: PgSetting): string {
  return s.unit ? `${s.setting} ${s.unit}` : s.setting
}

watch(connId, () => {
  editingName.value = null
  settings.value = []
  loaded.value = false
  if (connId.value) loadSettings()
})

onMounted(async () => {
  await fetchConnections()
  if (connId.value) await loadSettings()
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Postgres Parameters</div>
            <div class="page-subtitle">Inspect pg_settings and change server parameters with ALTER SYSTEM SET on a Postgres connection.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="!connId || reloading" @click="reloadConfig">
              {{ reloading ? 'Reloading…' : 'Reload Config' }}
            </button>
            <button class="base-btn base-btn--sm" :disabled="!connId || loading" @click="loadSettings">Refresh</button>
          </div>
        </section>

        <div class="page-card pp-notice">
          <strong>Before you change anything:</strong> Nias runs these changes as plain SQL (<code>ALTER SYSTEM SET</code> + <code>pg_reload_conf()</code>) against the connection you pick — it can't restart the Postgres process itself. Settings marked <strong>postmaster</strong> context only take effect after you restart Postgres externally (a Docker container restart, a service restart, or your cloud provider's console for managed instances).
        </div>

        <div class="page-card pp-card">
          <div class="page-card__body pp-card-body">
            <ConnectionPicker v-model="connId" :drivers="['postgres']" placeholder="Select a Postgres connection…" full-width />

            <template v-if="connId">
              <div v-if="pendingRestartCount" class="notice notice--warning">
                {{ pendingRestartCount }} parameter{{ pendingRestartCount === 1 ? '' : 's' }} changed and waiting on a Postgres restart to take effect.
              </div>

              <div class="pp-filters">
                <input v-model="search" class="base-input" placeholder="Search name or description…" style="flex:1" />
                <select v-model="categoryFilter" class="base-input pp-category">
                  <option value="all">All categories</option>
                  <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
                </select>
                <label class="pp-restart-filter">
                  <input type="checkbox" v-model="restartFilter" />
                  Pending restart only
                </label>
              </div>

              <div v-if="loading" class="empty-state">Loading pg_settings…</div>
              <div v-else-if="!filteredSettings.length" class="empty-state">
                {{ loaded ? 'No parameters match this filter.' : 'No parameters loaded.' }}
              </div>
              <div v-else class="pp-table">
                <div class="pp-table__header">
                  <span>Name</span>
                  <span>Value</span>
                  <span>Context</span>
                  <span></span>
                </div>
                <div v-for="s in filteredSettings" :key="s.name" class="pp-row" :class="{ 'pp-row--pending': s.pending_restart }">
                  <div class="pp-row__name">
                    <span class="pp-row__name-text">{{ s.name }}</span>
                    <span v-if="s.pending_restart" class="pp-badge pp-badge--pending" title="Needs a Postgres restart to take effect">restart pending</span>
                    <span class="pp-row__desc">{{ s.short_desc }}</span>
                  </div>

                  <div class="pp-row__value">
                    <template v-if="editingName === s.name">
                      <select v-if="s.vartype === 'bool'" v-model="editValue" class="base-input base-input--sm">
                        <option value="on">on</option>
                        <option value="off">off</option>
                      </select>
                      <select v-else-if="s.vartype === 'enum' && enumOptions(s).length" v-model="editValue" class="base-input base-input--sm">
                        <option v-for="opt in enumOptions(s)" :key="opt" :value="opt">{{ opt }}</option>
                      </select>
                      <input v-else v-model="editValue" class="base-input base-input--sm" @keydown.enter="saveEdit(s)" @keydown.esc="cancelEdit" />
                      <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving.has(s.name)" @click="saveEdit(s)">Save</button>
                      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="saving.has(s.name)" @click="cancelEdit">Cancel</button>
                    </template>
                    <template v-else>
                      <span class="pp-row__value-text" :title="formatValue(s)">{{ formatValue(s) }}</span>
                    </template>
                  </div>

                  <div class="pp-row__context">
                    <span class="pp-context-badge" :data-context="s.context">{{ s.context }}</span>
                  </div>

                  <div class="pp-row__actions">
                    <template v-if="editingName !== s.name">
                      <button
                        v-if="canManage"
                        class="base-btn base-btn--ghost base-btn--sm"
                        :disabled="!canEdit(s) || saving.has(s.name)"
                        :title="s.context === 'internal' ? 'Compile-time setting, cannot be changed' : 'Edit'"
                        @click="startEdit(s)"
                      >
                        Edit
                      </button>
                      <button
                        v-if="canManage && s.setting !== s.reset_val"
                        class="base-btn base-btn--ghost base-btn--sm"
                        :disabled="!canEdit(s) || saving.has(s.name)"
                        @click="resetSetting(s)"
                      >
                        Reset
                      </button>
                    </template>
                  </div>
                </div>
              </div>
            </template>
            <div v-else class="empty-state">Select a Postgres connection to view its parameters.</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pp-notice {
  padding: 16px 20px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary);
}
.pp-notice code {
  font-family: var(--mono);
  background: var(--bg-body);
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 12px;
}

.pp-card { padding: 20px; }
.pp-card-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.pp-filters {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.pp-category { max-width: 260px; }
.pp-restart-filter {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.pp-table {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
}

.pp-table__header,
.pp-row {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1.4fr) 110px 140px;
  gap: 10px;
  align-items: center;
  padding: 10px 14px;
}

.pp-table__header {
  background: var(--bg-body);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}

.pp-row {
  border-bottom: 1px solid var(--border);
  transition: background 0.1s;
}
.pp-row:last-child { border-bottom: none; }
.pp-row:hover { background: var(--bg-body); }
.pp-row--pending { background: rgba(245, 158, 11, 0.06); }

.pp-row__name { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.pp-row__name-text {
  font-family: var(--mono);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-primary);
  word-break: break-word;
}
.pp-row__desc {
  font-size: 11px;
  color: var(--text-muted);
}

.pp-badge {
  display: inline-flex;
  align-self: flex-start;
  padding: 1px 6px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}
.pp-badge--pending {
  background: rgba(245, 158, 11, 0.16);
  color: #d97706;
}

.pp-row__value {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.pp-row__value-text {
  font-family: var(--mono);
  font-size: 12.5px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.base-input--sm { padding: 4px 8px; font-size: 12px; min-width: 0; }

.pp-context-badge {
  display: inline-flex;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 10.5px;
  font-weight: 600;
  background: var(--bg-surface);
  color: var(--text-secondary);
  text-transform: capitalize;
}
.pp-context-badge[data-context="postmaster"] { background: rgba(239, 68, 68, 0.12); color: #dc2626; }
.pp-context-badge[data-context="internal"] { background: var(--bg-body); color: var(--text-muted); }
.pp-context-badge[data-context="sighup"] { background: rgba(59, 130, 246, 0.12); color: #2563eb; }

.pp-row__actions {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}
</style>
