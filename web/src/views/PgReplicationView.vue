<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useConnections } from '@/composables/useConnections'
import ConnectionPicker from '@/components/ui/ConnectionPicker.vue'

// Mirrors server/handlers/pg_replication.go's JSON shapes.
interface ReplicationLink {
  id: number
  source_connection_id: number
  source_connection_name: string
  target_connection_id: number
  target_connection_name: string
  publication_name: string
  subscription_name: string
  created_by: string
  created_at: string
}
interface SubscriptionStatus {
  name: string
  enabled: boolean
  pid?: number
  received_lsn?: string
  latest_end_lsn?: string
  last_msg_receipt_time?: string
  lag_seconds?: number
  lag_bytes?: number
}
interface SchemaTable {
  schema: string
  table: string
}
interface PublicationsResponse {
  publications: unknown[]
  wal_level: string
  wal_level_ok: boolean
  has_replication_privilege: boolean
}

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['pgreplication.manage']))

const { fetchConnections, connections } = useConnections()

// ── Links list + live status polling ─────────────────────────────
const links = ref<ReplicationLink[]>([])
const loading = ref(false)
const statusByLink = ref<Record<number, SubscriptionStatus | null>>({})
let refreshTimer: ReturnType<typeof setInterval> | null = null

async function loadLinks() {
  loading.value = true
  try {
    const { data } = await axios.get<ReplicationLink[]>('/api/pg-replication/links')
    links.value = data
    await refreshStatuses()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load replication links')
  } finally {
    loading.value = false
  }
}

// One status request per distinct target connection (not per link) — a
// target can host several subscriptions from different sources.
async function refreshStatuses() {
  const byTarget = new Map<number, ReplicationLink[]>()
  for (const l of links.value) {
    const group = byTarget.get(l.target_connection_id) || []
    group.push(l)
    byTarget.set(l.target_connection_id, group)
  }
  for (const [targetId, group] of byTarget) {
    try {
      const { data } = await axios.get<SubscriptionStatus[]>('/api/pg-replication/subscriptions', {
        params: { connection_id: targetId },
      })
      const byName = new Map(data.map((s) => [s.name, s]))
      for (const l of group) statusByLink.value[l.id] = byName.get(l.subscription_name) ?? null
    } catch {
      for (const l of group) statusByLink.value[l.id] = null
    }
  }
}

async function toggleEnabled(link: ReplicationLink) {
  const status = statusByLink.value[link.id]
  const action = status?.enabled ? 'disable' : 'enable'
  try {
    await axios.patch(
      `/api/pg-replication/subscriptions/${encodeURIComponent(link.subscription_name)}`,
      { action },
      { params: { connection_id: link.target_connection_id } },
    )
    toast.success(`Subscription ${action}d`)
    await refreshStatuses()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || `Failed to ${action} subscription`)
  }
}

async function dropLink(link: ReplicationLink) {
  const ok = await confirm(
    `Drop subscription "${link.subscription_name}" on ${link.target_connection_name}? This stops replication immediately. The publication "${link.publication_name}" on ${link.source_connection_name} is left in place.`,
    'Drop',
  )
  if (!ok) return
  try {
    await axios.delete(`/api/pg-replication/subscriptions/${encodeURIComponent(link.subscription_name)}`, {
      params: { connection_id: link.target_connection_id },
    })
    toast.success('Subscription dropped')
    await loadLinks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to drop subscription')
  }
}

// ── Check & Sync modal (non-destructive drift check + catch-up) ────
interface CompareResult {
  missing_on_target: number
  extra_on_target: number
  differs: number
  in_sync: boolean
}
const showCheck = ref(false)
const checkLink = ref<ReplicationLink | null>(null)
const checkTables = ref<string[]>([])
const checkLoading = ref(false)
const compareResults = ref<Record<string, CompareResult | 'checking' | 'error' | undefined>>({})
const reconcileBusy = ref<Record<string, boolean>>({})

async function openCheck(link: ReplicationLink) {
  checkLink.value = link
  checkTables.value = []
  compareResults.value = {}
  showCheck.value = true
  checkLoading.value = true
  try {
    const { data } = await axios.get<PublicationsResponse>('/api/pg-replication/publications', {
      params: { connection_id: link.source_connection_id },
    })
    const pub = (data.publications as any[]).find((p) => p.name === link.publication_name)
    checkTables.value = (pub?.tables || []).map((t: SchemaTable) => `${t.schema}.${t.table}`)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load tables for this publication')
  } finally {
    checkLoading.value = false
  }
}

async function runCompare(table: string) {
  if (!checkLink.value) return
  compareResults.value = { ...compareResults.value, [table]: 'checking' }
  try {
    const { data } = await axios.get<CompareResult>('/api/pg-replication/compare', {
      params: {
        source_connection_id: checkLink.value.source_connection_id,
        target_connection_id: checkLink.value.target_connection_id,
        table,
      },
    })
    compareResults.value = { ...compareResults.value, [table]: data }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || `Failed to compare ${table}`)
    compareResults.value = { ...compareResults.value, [table]: 'error' }
  }
}

async function runReconcile(table: string) {
  if (!checkLink.value) return
  reconcileBusy.value = { ...reconcileBusy.value, [table]: true }
  try {
    const { data } = await axios.post<{ inserted: number; updated: number }>('/api/pg-replication/reconcile', {
      sourceConnectionId: checkLink.value.source_connection_id,
      targetConnectionId: checkLink.value.target_connection_id,
      table,
    })
    toast.success(`${table}: inserted ${data.inserted}, updated ${data.updated} row(s) — nothing else touched`)
    await runCompare(table)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || `Failed to reconcile ${table}`)
  } finally {
    reconcileBusy.value = { ...reconcileBusy.value, [table]: false }
  }
}

function compareLabel(table: string): string {
  const r = compareResults.value[table]
  if (r === undefined) return ''
  if (r === 'checking') return 'Checking…'
  if (r === 'error') return 'Check failed'
  if (r.in_sync) return 'In sync'
  const parts: string[] = []
  if (r.missing_on_target) parts.push(`${r.missing_on_target} missing`)
  if (r.differs) parts.push(`${r.differs} differ`)
  if (r.extra_on_target) parts.push(`${r.extra_on_target} extra on target (untouched)`)
  return parts.join(', ')
}
function needsPull(table: string): boolean {
  const r = compareResults.value[table]
  return typeof r === 'object' && (r.missing_on_target > 0 || r.differs > 0)
}

// ── New Replication modal ─────────────────────────────────────────
const showCreate = ref(false)
const sourceConnId = ref<number | null>(null)
const targetConnId = ref<number | null>(null)
const pubName = ref('')
const subName = ref('')
const allTables = ref(true)
const availableTables = ref<SchemaTable[]>([])
const loadingTables = ref(false)
const selectedTables = ref<Set<string>>(new Set())
const copyData = ref(true)
const truncateTarget = ref(false)
const creating = ref(false)
const createError = ref('')
const walWarning = ref<{ walLevelOk: boolean; hasPriv: boolean } | null>(null)

function openCreate() {
  showCreate.value = true
  sourceConnId.value = null
  targetConnId.value = null
  const ts = Date.now()
  pubName.value = `nias_pub_${ts}`
  subName.value = `nias_sub_${ts}`
  allTables.value = true
  availableTables.value = []
  selectedTables.value = new Set()
  copyData.value = true
  truncateTarget.value = false
  createError.value = ''
  walWarning.value = null
}

async function onSourceSelected(id: number | null) {
  sourceConnId.value = id
  walWarning.value = null
  availableTables.value = []
  selectedTables.value = new Set()
  if (id == null) return
  try {
    const { data } = await axios.get<PublicationsResponse>('/api/pg-replication/publications', { params: { connection_id: id } })
    walWarning.value = { walLevelOk: data.wal_level_ok, hasPriv: data.has_replication_privilege }
  } catch {
    // Best-effort — CREATE PUBLICATION will surface a clear error anyway if
    // wal_level/privileges are actually a problem.
  }
  loadingTables.value = true
  try {
    const { data } = await axios.get<SchemaTable[]>(`/api/connections/${id}/db-users-tables`)
    availableTables.value = data
  } catch {
    availableTables.value = []
  } finally {
    loadingTables.value = false
  }
}

function toggleTable(key: string) {
  const s = new Set(selectedTables.value)
  if (s.has(key)) s.delete(key)
  else s.add(key)
  selectedTables.value = s
}

async function submitCreate() {
  if (!sourceConnId.value || !targetConnId.value) {
    createError.value = 'Pick a source and target connection'
    return
  }
  if (!pubName.value.trim() || !subName.value.trim()) {
    createError.value = 'Publication and subscription names are required'
    return
  }
  if (!allTables.value && selectedTables.value.size === 0) {
    createError.value = 'Select at least one table, or choose "all tables"'
    return
  }
  const tables = allTables.value
    ? availableTables.value.map((t) => `${t.schema}.${t.table}`)
    : Array.from(selectedTables.value)

  // Checking the box alone isn't enough friction for a statement that
  // deletes every row in every one of these tables — "All tables" on a real
  // database can silently mean dozens of tables, not just the one the user
  // was thinking about when they checked it.
  if (copyData.value && truncateTarget.value) {
    const targetName = connections.value.find((c) => c.id === targetConnId.value)?.name || `connection #${targetConnId.value}`
    const ok = await confirm(
      `This will run TRUNCATE on ${tables.length} table(s) on "${targetName}" before the copy starts: ${tables.join(', ')}. A timestamped backup copy of each table (e.g. "${tables[0]?.split('.').pop()}_bak_<timestamp>") is created first, but the live tables themselves will be emptied immediately — restoring from a backup afterward is a manual step, not automatic.`,
      'Truncate & start replication',
    )
    if (!ok) return
  }

  creating.value = true
  createError.value = ''
  // Publication creation and subscription creation are two separate calls
  // (they run against two different Postgres servers, so there's no single
  // transaction that could span both) — track whether the first one
  // succeeded so a failure on the second can roll it back instead of
  // leaving an orphaned publication sitting on the source forever.
  let publicationCreated = false
  try {
    await axios.post('/api/pg-replication/publications', {
      connectionId: sourceConnId.value,
      name: pubName.value.trim(),
      allTables: allTables.value,
      tables: allTables.value ? [] : Array.from(selectedTables.value),
    })
    publicationCreated = true
    await axios.post('/api/pg-replication/subscriptions', {
      targetConnectionId: targetConnId.value,
      sourceConnectionId: sourceConnId.value,
      name: subName.value.trim(),
      publicationName: pubName.value.trim(),
      copyData: copyData.value,
      truncateFirst: copyData.value && truncateTarget.value,
      tables,
    })
    toast.success('Replication started')
    showCreate.value = false
    await loadLinks()
  } catch (e: any) {
    const message = e?.response?.data?.error || 'Failed to create replication'
    if (publicationCreated) {
      try {
        await axios.delete(`/api/pg-replication/publications/${encodeURIComponent(pubName.value.trim())}`, {
          params: { connection_id: sourceConnId.value },
        })
        createError.value = message
      } catch {
        createError.value = `${message} — and the publication it created on the source could not be rolled back automatically; you may need to drop "${pubName.value.trim()}" manually.`
      }
    } else {
      createError.value = message
    }
  } finally {
    creating.value = false
  }
}

// ── Formatting ──────────────────────────────────────────────────
function formatBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.max(n, 1)) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(i ? 1 : 0)} ${u[i]}`
}
function formatLag(link: ReplicationLink): string {
  const s = statusByLink.value[link.id]
  if (s === undefined) return '…'
  if (!s) return 'Unknown'
  if (!s.enabled) return 'Disabled'
  if (s.lag_bytes !== undefined && s.lag_bytes !== null) return formatBytes(s.lag_bytes)
  if (s.lag_seconds !== undefined && s.lag_seconds !== null) return `${Math.round(s.lag_seconds)}s since last message`
  return 'Unknown'
}
function isEnabled(link: ReplicationLink): boolean {
  return statusByLink.value[link.id]?.enabled ?? false
}
function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(async () => {
  await fetchConnections()
  await loadLinks()
  refreshTimer = setInterval(refreshStatuses, 15000)
})
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Postgres Replication</div>
            <div class="page-subtitle">Create and monitor native logical replication (publications/subscriptions) between Postgres connections.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="loading" @click="loadLinks">Refresh</button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openCreate">+ New Replication</button>
          </div>
        </section>

        <div class="page-card pr-notice">
          <strong>Before you start:</strong> the target Postgres server must be able to reach the source Postgres server directly over the network — Nias only runs the setup DDL, it doesn't relay the data. The source needs <code>wal_level = logical</code> (a server-side setting, may require a restart) and the connecting role needs replication privilege. Tables must already exist on the target with matching structure — logical replication ships row changes, not schema.
        </div>

        <div class="page-card pr-list">
          <table class="pr-table">
            <thead>
              <tr>
                <th>Source</th>
                <th>Target</th>
                <th>Publication</th>
                <th>Subscription</th>
                <th>Status</th>
                <th>Lag</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading"><td colspan="8" class="pr-msg">Loading…</td></tr>
              <tr v-else-if="!links.length">
                <td colspan="8" class="pr-msg">No replication links yet. Click "New Replication" to sync data between two Postgres connections.</td>
              </tr>
              <tr v-for="link in links" :key="link.id">
                <td>{{ link.source_connection_name || `#${link.source_connection_id}` }}</td>
                <td>{{ link.target_connection_name || `#${link.target_connection_id}` }}</td>
                <td class="pr-mono">{{ link.publication_name }}</td>
                <td class="pr-mono">{{ link.subscription_name }}</td>
                <td>
                  <span class="pr-badge" :class="isEnabled(link) ? 'pr-badge--on' : 'pr-badge--off'">
                    {{ isEnabled(link) ? 'Enabled' : 'Disabled' }}
                  </span>
                </td>
                <td class="pr-mono">{{ formatLag(link) }}</td>
                <td class="pr-time">{{ formatTime(link.created_at) }}<span v-if="link.created_by"> · {{ link.created_by }}</span></td>
                <td class="pr-act">
                  <button class="pr-link-btn" @click="openCheck(link)">Check &amp; Sync</button>
                  <template v-if="canManage">
                    <button class="pr-link-btn" @click="toggleEnabled(link)">{{ isEnabled(link) ? 'Disable' : 'Enable' }}</button>
                    <button class="pr-link-btn pr-link-btn--danger" @click="dropLink(link)">Drop</button>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- New Replication modal -->
    <div v-if="showCreate" class="pr-modal-backdrop" @click.self="showCreate = false">
      <div class="pr-modal page-card">
        <div class="pr-modal-title">New Replication</div>

        <div class="form-group">
          <label class="form-label">Source connection (publisher)</label>
          <ConnectionPicker
            :model-value="sourceConnId"
            @update:model-value="onSourceSelected"
            :drivers="['postgres']"
            :exclude-ids="targetConnId != null ? [targetConnId] : []"
            placeholder="Select a Postgres connection…"
            full-width
          />
        </div>

        <div v-if="walWarning && (!walWarning.walLevelOk || !walWarning.hasPriv)" class="pr-warning">
          <span v-if="!walWarning.walLevelOk">wal_level is not "logical" on this connection — CREATE PUBLICATION will work, but replication won't start until it's changed and the server restarted.</span>
          <span v-if="!walWarning.hasPriv">The connecting user doesn't appear to have replication privilege — this may fail.</span>
        </div>

        <div class="form-group">
          <label class="form-label">Target connection (subscriber)</label>
          <ConnectionPicker
            v-model="targetConnId"
            :drivers="['postgres']"
            :exclude-ids="sourceConnId != null ? [sourceConnId] : []"
            placeholder="Select a Postgres connection…"
            full-width
          />
        </div>

        <div class="form-group">
          <label class="form-label">Tables</label>
          <label class="pr-checkline"><input type="checkbox" v-model="allTables" /> All tables</label>
          <div v-if="!allTables" class="pr-tables">
            <div v-if="!sourceConnId" class="pr-msg">Pick a source connection first.</div>
            <div v-else-if="loadingTables" class="pr-msg">Loading tables…</div>
            <div v-else-if="!availableTables.length" class="pr-msg">No tables found.</div>
            <label v-for="t in availableTables" :key="`${t.schema}.${t.table}`" class="pr-checkline">
              <input type="checkbox" :checked="selectedTables.has(`${t.schema}.${t.table}`)" @change="toggleTable(`${t.schema}.${t.table}`)" />
              {{ t.schema }}.{{ t.table }}
            </label>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Publication name</label>
          <input v-model="pubName" class="base-input" />
        </div>
        <div class="form-group">
          <label class="form-label">Subscription name</label>
          <input v-model="subName" class="base-input" />
        </div>
        <div class="form-group">
          <label class="pr-checkline"><input type="checkbox" v-model="copyData" /> Copy existing data on creation</label>
        </div>
        <div v-if="copyData" class="form-group">
          <label class="pr-checkline"><input type="checkbox" v-model="truncateTarget" /> Replace existing data in target tables first</label>
          <div v-if="truncateTarget" class="pr-warning">
            This runs <code>TRUNCATE</code> on the selected target table(s) right before the copy starts, deleting whatever rows are already there — a timestamped backup table is created first, but restoring from it is a manual step. Use this when the target already has rows that would collide with the source's — otherwise the initial copy fails on the first duplicate key and never completes.
          </div>
        </div>

        <div v-if="createError" class="pr-msg pr-err">{{ createError }}</div>

        <div class="pr-modal-actions">
          <button class="base-btn base-btn--sm" @click="showCreate = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="creating" @click="submitCreate">
            {{ creating ? 'Starting…' : 'Start Replication' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Check & Sync modal -->
    <div v-if="showCheck" class="pr-modal-backdrop" @click.self="showCheck = false">
      <div class="pr-modal pr-modal--wide page-card">
        <div class="pr-modal-title">Check &amp; Sync</div>
        <div class="pr-msg" style="text-align:left;padding:0 0 12px">
          Compares rows between <strong>{{ checkLink?.source_connection_name }}</strong> (publisher) and
          <strong>{{ checkLink?.target_connection_name }}</strong> (subscriber) by primary key. "Pull from publisher"
          only inserts missing rows and updates ones that differ — nothing is ever deleted, and rows that exist only
          on the target are left alone.
        </div>

        <div class="pr-modal-scroll">
          <div v-if="checkLoading" class="pr-msg">Loading tables…</div>
          <div v-else-if="!checkTables.length" class="pr-msg">No tables found in this publication.</div>
          <table v-else class="pr-table">
            <thead>
              <tr>
                <th>Table</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="t in checkTables" :key="t">
                <td class="pr-mono">{{ t }}</td>
                <td class="pr-mono">{{ compareLabel(t) }}</td>
                <td class="pr-act">
                  <button class="pr-link-btn" :disabled="compareResults[t] === 'checking'" @click="runCompare(t)">Check</button>
                  <button
                    v-if="canManage && needsPull(t)"
                    class="pr-link-btn"
                    :disabled="reconcileBusy[t]"
                    @click="runReconcile(t)"
                  >{{ reconcileBusy[t] ? 'Pulling…' : 'Pull from publisher' }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="pr-modal-actions">
          <button class="base-btn base-btn--sm" @click="showCheck = false">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pr-notice { font-size: 13px; color: var(--text-secondary); line-height: 1.6; padding: 12px 16px; }
.pr-notice code { font-family: var(--mono); background: var(--bg-hover); padding: 1px 5px; border-radius: var(--r-xs); font-size: 12px; }

.pr-list { padding: 4px 6px; overflow: hidden; }
.pr-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.pr-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.pr-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.pr-table tbody tr:last-child td { border-bottom: none; }
.pr-mono { font-family: var(--mono); font-size: 12px; }
.pr-time { color: var(--text-muted); font-size: 12px; white-space: nowrap; }
.pr-msg { text-align: center; color: var(--text-muted); padding: 20px; }
.pr-err { color: var(--danger); text-align: left; padding: 4px 0; }
.pr-act { text-align: right; white-space: nowrap; }

.pr-badge { display: inline-flex; align-items: center; padding: 2px 9px; border-radius: 99px; font-size: 11px; font-weight: 600; }
.pr-badge--on { color: var(--success); background: var(--success-bg, rgba(34,197,94,0.12)); }
.pr-badge--off { color: var(--text-muted); background: var(--bg-hover); }

.pr-link-btn { background: none; border: none; padding: 0; margin-left: 12px; font-size: 12px; color: var(--brand); cursor: pointer; }
.pr-link-btn:hover { text-decoration: underline; }
.pr-link-btn--danger { color: var(--danger); }

.pr-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; overflow-y: auto; padding: 24px 0; }
.pr-modal { width: 480px; max-width: 92vw; padding: 20px; }
.pr-modal--wide { width: 640px; }
.pr-modal-scroll { max-height: 50vh; overflow-y: auto; }
.pr-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 14px; }
.pr-modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }

.pr-checkline { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 3px 0; cursor: pointer; }
.pr-tables { max-height: 160px; overflow-y: auto; margin-top: 6px; padding: 6px 10px; background: var(--bg-hover); border: 1px solid var(--border); border-radius: var(--r-sm); }
.pr-warning { font-size: 12px; color: var(--danger); background: var(--danger-bg, rgba(239,68,68,0.08)); border-radius: var(--r-sm); padding: 8px 10px; margin: -6px 0 14px; display: flex; flex-direction: column; gap: 4px; }
</style>
