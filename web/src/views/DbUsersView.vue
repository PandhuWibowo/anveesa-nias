<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConnections } from '@/composables/useConnections'
import { readableError } from '@/utils/httpError'

const props = defineProps<{ activeConnId: number | null }>()
const toast = useToast()
const { connections } = useConnections()

const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null
)

const supportedDrivers = ['postgres', 'mysql', 'mariadb']
const isSupported = computed(() => activeConn.value && supportedDrivers.includes(activeConn.value.driver))

// ── DB Users list ─────────────────────────────────────────────────

interface DBUser {
  username: string
  host: string
  is_superuser: boolean
  can_create_db: boolean
  can_login: boolean
}

const users = ref<DBUser[]>([])
const loadingUsers = ref(false)
const selectedUser = ref<DBUser | null>(null)
const userSearch = ref('')

const filteredUsers = computed(() => {
  const q = userSearch.value.toLowerCase()
  if (!q) return users.value
  return users.value.filter(u => u.username.toLowerCase().includes(q) || u.host?.toLowerCase().includes(q))
})

async function loadUsers() {
  if (!props.activeConnId) return
  loadingUsers.value = true
  try {
    const { data } = await axios.get<DBUser[]>(`/api/connections/${props.activeConnId}/db-users`)
    users.value = data ?? []
    if (selectedUser.value) {
      selectedUser.value = users.value.find(u => u.username === selectedUser.value!.username && u.host === selectedUser.value!.host) ?? null
    }
  } catch (e) {
    toast.error(readableError(e, { action: 'Load DB users', fallback: 'Failed to load database users' }))
  } finally {
    loadingUsers.value = false
  }
}

function selectUser(u: DBUser) {
  selectedUser.value = u
  loadGrants(u)
  activeTab.value = 'grants'
}

// ── Grants ────────────────────────────────────────────────────────

interface GrantEntry {
  level: 'global' | 'database' | 'schema' | 'table' | 'sequence' | 'function'
  database?: string
  schema?: string
  table?: string  // also used for sequence name and function name
  privileges: string[]
}

const grants = ref<GrantEntry[]>([])
const loadingGrants = ref(false)
const savingGrants = ref(false)

const pgTablePrivs = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']
const pgSchemaPrivs = ['USAGE', 'CREATE']
const pgDatabasePrivs = ['CONNECT', 'CREATE', 'TEMP']
const pgSequencePrivs = ['USAGE', 'SELECT', 'UPDATE']
const pgFunctionPrivs = ['EXECUTE']
const mysqlPrivs = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE VIEW', 'SHOW VIEW', 'EXECUTE', 'TRIGGER', 'LOCK TABLES',
  'CREATE ROUTINE', 'ALTER ROUTINE', 'EVENT',
]

// Privilege descriptions for tooltip
const privDescriptions: Record<string, string> = {
  SELECT: 'DML · Read rows', INSERT: 'DML · Add rows', UPDATE: 'DML · Modify rows',
  DELETE: 'DML · Remove rows', TRUNCATE: 'DML · Empty table', REFERENCES: 'DML · Create FK references',
  TRIGGER: 'DDL · Create triggers on table', USAGE: 'Access schema / sequence / type',
  CREATE: 'DDL · Create objects inside this scope', CONNECT: 'Connect to this database',
  TEMP: 'Create temporary tables', EXECUTE: 'Run this function or procedure',
}

async function loadGrants(u: DBUser) {
  if (!props.activeConnId) return
  loadingGrants.value = true
  grants.value = []
  try {
    const params = u.host ? { host: u.host } : {}
    const { data } = await axios.get<GrantEntry[]>(
      `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}/grants`,
      { params }
    )
    grants.value = data ?? []
  } catch (e) {
    toast.error(readableError(e, { action: 'Load grants', fallback: 'Failed to load grants' }))
  } finally {
    loadingGrants.value = false
  }
}

// ── Grant editing ─────────────────────────────────────────────────

const editingGrants = ref<GrantEntry[]>([])
const grantsDirty = ref(false)
const showAddGrantModal = ref(false)

const validLevels = new Set<string>(['global', 'database', 'schema', 'table', 'sequence', 'function'])

watch(grants, (val) => {
  editingGrants.value = (val ?? [])
    .map(g => ({
      level: g.level as GrantEntry['level'],
      database: g.database || undefined,
      schema: g.schema || undefined,
      table: g.table || undefined,
      privileges: Array.isArray(g.privileges) ? g.privileges : [],
    }))
    .filter(g => validLevels.has(g.level))
  grantsDirty.value = false
})

function getEditGrant(level: string, database?: string, schema?: string, table?: string): GrantEntry | undefined {
  return editingGrants.value.find(g =>
    g.level === level &&
    (g.database ?? '') === (database ?? '') &&
    (g.schema ?? '') === (schema ?? '') &&
    (g.table ?? '') === (table ?? '')
  )
}

function ensureGrant(level: GrantEntry['level'], database?: string, schema?: string, table?: string): GrantEntry {
  let g = getEditGrant(level, database, schema, table)
  if (!g) {
    g = { level, database, schema, table, privileges: [] }
    editingGrants.value.push(g)
  }
  return g
}

function togglePriv(level: GrantEntry['level'], priv: string, database?: string, schema?: string, table?: string) {
  const g = ensureGrant(level, database, schema, table)
  if (!Array.isArray(g.privileges)) g.privileges = []
  const idx = g.privileges.indexOf(priv)
  if (idx >= 0) {
    g.privileges.splice(idx, 1)
  } else {
    g.privileges.push(priv)
  }
  grantsDirty.value = true
}

function hasPriv(level: GrantEntry['level'], priv: string, database?: string, schema?: string, table?: string): boolean {
  return getEditGrant(level, database, schema, table)?.privileges.includes(priv) ?? false
}

async function saveGrants() {
  if (!selectedUser.value || !props.activeConnId) return
  savingGrants.value = true
  try {
    await axios.put(
      `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(selectedUser.value.username)}/grants`,
      { host: selectedUser.value.host || '%', grants: editingGrants.value.filter(g => (g.privileges ?? []).length > 0) }
    )
    toast.success('Grants saved successfully')
    grantsDirty.value = false
    await loadGrants(selectedUser.value)
  } catch (e) {
    toast.error(readableError(e, { action: 'Save grants', fallback: 'Failed to save grants' }))
  } finally {
    savingGrants.value = false
  }
}

// ── Add grant row ─────────────────────────────────────────────────

const newGrantLevel = ref<'global' | 'database' | 'schema' | 'table' | 'sequence' | 'function'>('database')
const newGrantDatabase = ref('')
const newGrantSchema = ref('')
const newGrantTable = ref('')
const newGrantPrivileges = ref<string[]>([])

const availableDbs = ref<string[]>([])
const availableSchemas = ref<string[]>([])
const availableTables = ref<Array<{ schema: string; table: string }>>([])
const availableSequences = ref<Array<{ schema: string; sequence: string }>>([])
const availableFunctions = ref<Array<{ schema: string; function: string; kind: string }>>([])

async function loadAvailableDbs() {
  if (!props.activeConnId) return
  try {
    const { data } = await axios.get<string[]>(`/api/connections/${props.activeConnId}/db-users-dbs`)
    availableDbs.value = data ?? []
  } catch { availableDbs.value = [] }
}

async function loadAvailableSchemas(forDb?: string) {
  if (!props.activeConnId) return
  availableSchemas.value = []
  try {
    const params = forDb ? { db: forDb } : {}
    const { data } = await axios.get<string[]>(`/api/connections/${props.activeConnId}/db-users-schemas`, { params })
    availableSchemas.value = data ?? []
  } catch { availableSchemas.value = [] }
}

async function loadAvailableTables(db: string, schema: string) {
  if (!props.activeConnId) return
  availableTables.value = []
  try {
    const { data } = await axios.get<Array<{ schema: string; table: string }>>(
      `/api/connections/${props.activeConnId}/db-users-tables`,
      { params: { db, schema } }
    )
    availableTables.value = data ?? []
  } catch { availableTables.value = [] }
}

async function loadAvailableSequences(db: string, schema: string) {
  if (!props.activeConnId) return
  availableSequences.value = []
  try {
    const { data } = await axios.get<Array<{ schema: string; sequence: string }>>(
      `/api/connections/${props.activeConnId}/db-users-sequences`,
      { params: { db, schema } }
    )
    availableSequences.value = data ?? []
  } catch { availableSequences.value = [] }
}

async function loadAvailableFunctions(db: string, schema: string) {
  if (!props.activeConnId) return
  availableFunctions.value = []
  try {
    const { data } = await axios.get<Array<{ schema: string; function: string; kind: string }>>(
      `/api/connections/${props.activeConnId}/db-users-functions`,
      { params: { db, schema } }
    )
    availableFunctions.value = data ?? []
  } catch { availableFunctions.value = [] }
}

function defaultPrivsForLevel(level: string): string[] {
  if (isPG.value) {
    if (level === 'database') return ['CONNECT']
    if (level === 'schema') return ['USAGE']
    if (level === 'sequence') return ['USAGE']
    if (level === 'function') return ['EXECUTE']
    return ['SELECT']
  }
  return level === 'global' ? [] : ['SELECT']
}

function onLevelPillClick(lvl: typeof newGrantLevel.value) {
  newGrantLevel.value = lvl
  newGrantDatabase.value = ''
  newGrantSchema.value = ''
  newGrantTable.value = ''
  newGrantPrivileges.value = defaultPrivsForLevel(lvl)
  availableTables.value = []
  availableSequences.value = []
  availableFunctions.value = []
}

function openAddGrant() {
  const lvl = activeConn.value?.driver === 'postgres' ? 'database' as const : 'global' as const
  newGrantLevel.value = lvl
  newGrantDatabase.value = ''
  newGrantSchema.value = ''
  newGrantTable.value = ''
  newGrantPrivileges.value = defaultPrivsForLevel(lvl)
  availableTables.value = []
  availableSequences.value = []
  availableFunctions.value = []
  availableSchemas.value = []
  showAddGrantModal.value = true
  loadAvailableDbs()
}

function confirmAddGrant() {
  const level = newGrantLevel.value

  const db = newGrantDatabase.value || undefined
  const schema = newGrantSchema.value || undefined
  const table = newGrantTable.value || undefined

  const g = ensureGrant(level, db, schema, table)
  if (!Array.isArray(g.privileges)) g.privileges = []
  // Use privileges chosen in the modal; fall back to safe defaults only if none selected
  if (newGrantPrivileges.value.length > 0) {
    // Merge: keep existing + add newly selected
    for (const p of newGrantPrivileges.value) {
      if (!g.privileges.includes(p)) g.privileges.push(p)
    }
  } else {
    if (g.privileges.length === 0) {
      g.privileges = defaultPrivsForLevel(level)
    }
  }
  // patch: remove privs that were deselected
  g.privileges = newGrantPrivileges.value.length > 0 ? [...newGrantPrivileges.value] : g.privileges
  if (g.privileges.length === 0) {
    if (level === 'database') g.privileges = ['CONNECT']
    else if (level === 'schema') g.privileges = ['USAGE']
    else g.privileges = ['SELECT']
  }
  grantsDirty.value = true
  showAddGrantModal.value = false
}

function removeGrantRow(idx: number) {
  editingGrants.value.splice(idx, 1)
  grantsDirty.value = true
}

// ── Create user modal ─────────────────────────────────────────────

const showCreateModal = ref(false)
const createUsername = ref('')
const createPassword = ref('')
const createPasswordConfirm = ref('')
const createHost = ref('%')
const createSaving = ref(false)
const showPw = ref(false)

function openCreateModal() {
  createUsername.value = ''
  createPassword.value = ''
  createPasswordConfirm.value = ''
  createHost.value = '%'
  createSaving.value = false
  showPw.value = false
  showCreateModal.value = true
}

async function saveCreateUser() {
  if (!createUsername.value.trim()) { toast.error('Username is required'); return }
  if (!createPassword.value) { toast.error('Password is required'); return }
  if (createPassword.value !== createPasswordConfirm.value) { toast.error('Passwords do not match'); return }
  createSaving.value = true
  try {
    await axios.post(`/api/connections/${props.activeConnId}/db-users`, {
      username: createUsername.value.trim(),
      password: createPassword.value,
      host: activeConn.value?.driver !== 'postgres' ? createHost.value : undefined,
    })
    toast.success(`User "${createUsername.value}" created`)
    showCreateModal.value = false
    await loadUsers()
  } catch (e) {
    toast.error(readableError(e, { action: 'Create DB user', fallback: 'Failed to create database user' }))
  } finally {
    createSaving.value = false
  }
}

// ── Change password modal ─────────────────────────────────────────

const showPwModal = ref(false)
const changePwUser = ref<DBUser | null>(null)
const changePwNew = ref('')
const changePwConfirm = ref('')
const changePwSaving = ref(false)
const showNewPw = ref(false)

function openChangePw(u: DBUser) {
  changePwUser.value = u
  changePwNew.value = ''
  changePwConfirm.value = ''
  changePwSaving.value = false
  showNewPw.value = false
  showPwModal.value = true
}

async function saveChangePw() {
  if (!changePwNew.value) { toast.error('Password is required'); return }
  if (changePwNew.value !== changePwConfirm.value) { toast.error('Passwords do not match'); return }
  changePwSaving.value = true
  try {
    await axios.patch(
      `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(changePwUser.value!.username)}/password`,
      { password: changePwNew.value, host: changePwUser.value!.host }
    )
    toast.success('Password updated')
    showPwModal.value = false
  } catch (e) {
    toast.error(readableError(e, { action: 'Change password', fallback: 'Failed to change password' }))
  } finally {
    changePwSaving.value = false
  }
}

// ── Drop user ─────────────────────────────────────────────────────

async function dropUser(u: DBUser) {
  const label = u.host ? `${u.username}@${u.host}` : u.username
  if (!confirm(`Drop database user "${label}"? This cannot be undone.`)) return
  try {
    const params = u.host ? { host: u.host } : {}
    await axios.delete(`/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}`, { params })
    toast.success(`User "${label}" dropped`)
    if (selectedUser.value?.username === u.username) selectedUser.value = null
    await loadUsers()
  } catch (e) {
    toast.error(readableError(e, { action: 'Drop user', fallback: 'Failed to drop user' }))
  }
}

// ── Tabs ──────────────────────────────────────────────────────────
const activeTab = ref<'grants' | 'password'>('grants')

// ── Init ─────────────────────────────────────────────────────────

onMounted(async () => {
  if (props.activeConnId) await loadUsers()
})

watch(() => props.activeConnId, async (id) => {
  if (id) {
    selectedUser.value = null
    grants.value = []
    await loadUsers()
  }
})

// Grant display helpers
const isPG = computed(() => activeConn.value?.driver === 'postgres')
const isMySQL = computed(() => activeConn.value?.driver === 'mysql' || activeConn.value?.driver === 'mariadb')

function privColsForLevel(level: string): string[] {
  if (isPG.value) {
    if (level === 'database') return pgDatabasePrivs
    if (level === 'schema') return pgSchemaPrivs
    if (level === 'sequence') return pgSequencePrivs
    if (level === 'function') return pgFunctionPrivs
    if (level === 'table') return pgTablePrivs
    return []
  }
  return mysqlPrivs
}

const groupedGrants = computed(() => {
  const map: Record<string, GrantEntry[]> = {}
  for (const g of editingGrants.value) {
    const key = g.level
    if (!key || !validLevels.has(key)) continue
    if (!map[key]) map[key] = []
    map[key].push(g)
  }
  return map
})

function levelLabel(level: string) {
  return ({
    global: 'Global (*.*)', database: 'Database', schema: 'Schema',
    table: 'Table', sequence: 'Sequence', function: 'Function / Procedure',
  } as Record<string, string>)[level] ?? level
}

function grantLabel(g: GrantEntry) {
  if (g.level === 'global') return '*.*'
  if (g.level === 'database') return g.database ?? ''
  if (g.level === 'schema') return `${g.database ? g.database + ' › ' : ''}${g.schema ?? ''}`
  if (g.level === 'table' || g.level === 'sequence' || g.level === 'function')
    return `${g.database ? g.database + ' › ' : ''}${g.schema ? g.schema + '.' : ''}${g.table ?? ''}`
  return ''
}
</script>

<template>
  <div class="page-shell dbu-root">
    <div class="page-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Administration</div>
            <div class="page-title">Database User Manager</div>
            <div class="page-subtitle">
              Create and manage database-level users, passwords, and privilege grants — directly on the connected server.
            </div>
          </div>
          <div class="page-hero__actions" v-if="isSupported && activeConnId">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadUsers">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="openCreateModal">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              Create User
            </button>
          </div>
        </section>

        <!-- No connection selected -->
        <div v-if="!activeConnId" class="dbu-empty-state">
          <div class="dbu-empty-icon">
            <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
          </div>
          <div class="dbu-empty-title">No connection selected</div>
          <div class="dbu-empty-sub">Select an active database connection from the top navigation to manage its users.</div>
        </div>

        <!-- Driver not supported -->
        <div v-else-if="!isSupported" class="dbu-empty-state">
          <div class="dbu-empty-icon">
            <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          </div>
          <div class="dbu-empty-title">Not supported for {{ activeConn?.driver }}</div>
          <div class="dbu-empty-sub">Database user management is available for PostgreSQL, MySQL, and MariaDB connections.</div>
        </div>

        <!-- Main two-panel layout -->
        <div v-else class="dbu-layout">

          <!-- Left: user list -->
          <div class="dbu-panel dbu-panel--left">
            <div class="dbu-conn-info">
              <div class="dbu-conn-info__row">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
                <span class="dbu-conn-info__name">{{ activeConn?.name }}</span>
                <span class="dbu-conn-info__driver">{{ activeConn?.driver }}</span>
              </div>
              <div class="dbu-conn-info__row dbu-conn-info__row--host">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>
                <span class="dbu-conn-info__host">{{ activeConn?.host }}</span>
                <span v-if="activeConn?.port" class="dbu-conn-info__port">:{{ activeConn?.port }}</span>
              </div>
            </div>
            <div class="dbu-panel-head">
              <div>
                <div class="dbu-panel-title">DB Users</div>
                <div class="dbu-panel-sub">{{ users.length }} user{{ users.length !== 1 ? 's' : '' }} on this server</div>
              </div>
            </div>

            <!-- Search -->
            <div class="dbu-search">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
              <input class="dbu-search-input" v-model="userSearch" placeholder="Filter users…" />
            </div>

            <div v-if="loadingUsers" class="dbu-loading">
              <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            </div>
            <div v-else class="dbu-user-list">
              <div
                v-for="u in filteredUsers"
                :key="u.username + (u.host || '')"
                class="dbu-user-row"
                :class="{ 'dbu-user-row--active': selectedUser?.username === u.username && selectedUser?.host === u.host }"
                @click="selectUser(u)"
              >
                <div class="dbu-user-avatar">
                  {{ u.username.slice(0, 2).toUpperCase() }}
                </div>
                <div class="dbu-user-info">
                  <div class="dbu-user-name">{{ u.username }}</div>
                  <div class="dbu-user-meta">
                    <span v-if="u.host" class="dbu-meta-host">@{{ u.host }}</span>
                    <span v-if="u.is_superuser" class="dbu-badge dbu-badge--super">Superuser</span>
                    <span v-else-if="u.can_create_db" class="dbu-badge dbu-badge--createdb">CreateDB</span>
                  </div>
                </div>
                <div class="dbu-user-actions">
                  <button class="dbu-icon-btn" title="Change password" @click.stop="openChangePw(u)">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
                  </button>
                  <button class="dbu-icon-btn dbu-icon-btn--danger" title="Drop user" @click.stop="dropUser(u)">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>
                  </button>
                </div>
              </div>
              <div v-if="filteredUsers.length === 0" class="dbu-list-empty">No users found</div>
            </div>
          </div>

          <!-- Right: grants / password panel -->
          <div class="dbu-panel dbu-panel--right">
            <div v-if="!selectedUser" class="dbu-empty-state dbu-empty-state--inline">
              <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
              <div>Select a user to manage their privileges</div>
            </div>

            <template v-else>
              <div class="dbu-panel-head">
                <div>
                  <div class="dbu-panel-title">
                    {{ selectedUser.username }}<span v-if="selectedUser.host" class="dbu-dim">@{{ selectedUser.host }}</span>
                  </div>
                  <div class="dbu-panel-sub">
                    <span v-if="selectedUser.is_superuser" class="dbu-badge dbu-badge--super">Superuser</span>
                    <span v-else-if="selectedUser.can_create_db" class="dbu-badge dbu-badge--createdb">Can Create DB</span>
                    <span v-else class="dbu-dim">Standard user</span>
                  </div>
                </div>
                <div style="display:flex;gap:8px">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="openChangePw(selectedUser)">Change Password</button>
                </div>
              </div>

              <!-- Tabs -->
              <div class="dbu-tabs">
                <button
                  class="dbu-tab"
                  :class="{ 'dbu-tab--active': activeTab === 'grants' }"
                  @click="activeTab = 'grants'"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 11 12 14 22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>
                  Privileges
                </button>
              </div>

              <!-- Grants tab -->
              <div v-if="activeTab === 'grants'" class="dbu-grants-panel">
                <div class="dbu-grants-toolbar">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="openAddGrant">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                    Add Scope
                  </button>
                  <div style="flex:1"></div>
                  <button
                    v-if="grantsDirty"
                    class="base-btn base-btn--primary base-btn--sm"
                    :disabled="savingGrants"
                    @click="saveGrants"
                  >
                    {{ savingGrants ? 'Saving…' : 'Save Changes' }}
                  </button>
                  <span v-if="grantsDirty" class="dbu-dirty-badge">Unsaved changes</span>
                </div>

                <div v-if="loadingGrants" class="dbu-loading">
                  <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                </div>

                <div v-else-if="editingGrants.length === 0" class="dbu-grants-empty">
                  No privileges granted. Click "Add Scope" to add a grant.
                </div>

                <div v-else class="dbu-grant-sections">
                  <template v-for="(entries, level) in groupedGrants" :key="level">
                    <div class="dbu-grant-section">
                      <div class="dbu-grant-section-title">{{ levelLabel(String(level)) }}</div>
                      <div class="dbu-grant-table-wrap">
                        <table class="dbu-grant-table">
                          <thead>
                            <tr>
                              <th class="dbu-th-target">
                                {{ level === 'table' ? 'Database › Schema.Table'
                                   : level === 'sequence' ? 'Database › Schema.Sequence'
                                   : level === 'function' ? 'Database › Schema.Function'
                                   : level === 'schema' ? 'Database › Schema'
                                   : level === 'database' ? 'Database'
                                   : 'Scope' }}
                              </th>
                              <th v-for="priv in privColsForLevel(String(level))" :key="priv" class="dbu-th-priv" :title="privDescriptions[priv] ?? priv">
                                <span class="dbu-th-priv-label">{{ priv.length > 8 ? priv.slice(0, 7) + '…' : priv }}</span>
                                <span v-if="privDescriptions[priv]" class="dbu-th-priv-cat">{{ privDescriptions[priv].split(' · ')[0] }}</span>
                              </th>
                              <th class="dbu-th-del"></th>
                            </tr>
                          </thead>
                          <tbody>
                            <tr v-for="(g, gi) in entries" :key="gi">
                              <td class="dbu-td-target">{{ grantLabel(g) || '—' }}</td>
                              <td v-for="priv in privColsForLevel(String(level))" :key="priv" class="dbu-td-priv">
                                <label class="dbu-priv-check">
                                  <input
                                    type="checkbox"
                                    :checked="(g.privileges ?? []).includes(priv)"
                                    @change="togglePriv(g.level, priv, g.database, g.schema, g.table)"
                                  />
                                </label>
                              </td>
                              <td class="dbu-td-del">
                                <button class="dbu-icon-btn dbu-icon-btn--danger" title="Remove scope" @click="removeGrantRow(editingGrants.indexOf(g))">
                                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                                </button>
                              </td>
                            </tr>
                          </tbody>
                        </table>
                      </div>
                    </div>
                  </template>
                </div>
              </div>
            </template>
          </div>
        </div>

      </div>
    </div>

    <!-- ── Create user modal ───────────────────────────────────── -->
    <Teleport to="body">
      <div v-if="showCreateModal" class="dbu-overlay" @click.self="showCreateModal = false">
        <div class="dbu-dialog">
          <div class="dbu-dialog-head">
            <div class="dbu-dialog-title">Create Database User</div>
            <button class="dbu-icon-btn" @click="showCreateModal = false">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="dbu-dialog-body">
            <div class="dbu-field">
              <label class="dbu-label">Username</label>
              <input class="dbu-input" v-model="createUsername" placeholder="e.g. app_readonly" autocomplete="off" />
            </div>
            <div v-if="isMySQL" class="dbu-field">
              <label class="dbu-label">Host <span class="dbu-hint">(use % to allow any host)</span></label>
              <input class="dbu-input" v-model="createHost" placeholder="%" autocomplete="off" />
            </div>
            <div class="dbu-field">
              <label class="dbu-label">Password</label>
              <div class="dbu-pw-wrap">
                <input class="dbu-input" :type="showPw ? 'text' : 'password'" v-model="createPassword" placeholder="••••••••" autocomplete="new-password" />
                <button type="button" class="dbu-pw-eye" @click="showPw = !showPw">
                  <svg v-if="showPw" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                  <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                </button>
              </div>
            </div>
            <div class="dbu-field">
              <label class="dbu-label">Confirm Password</label>
              <input class="dbu-input" type="password" v-model="createPasswordConfirm" placeholder="••••••••" autocomplete="new-password" />
            </div>

            <!-- Strength indicator -->
            <div v-if="createPassword" class="dbu-pw-strength">
              <div class="dbu-pw-bar">
                <div
                  class="dbu-pw-fill"
                  :style="{ width: `${Math.min(createPassword.length * 8, 100)}%` }"
                  :class="{
                    'dbu-pw-fill--weak': createPassword.length < 8,
                    'dbu-pw-fill--ok': createPassword.length >= 8 && createPassword.length < 12,
                    'dbu-pw-fill--strong': createPassword.length >= 12,
                  }"
                ></div>
              </div>
              <span class="dbu-pw-label">
                {{ createPassword.length < 8 ? 'Weak' : createPassword.length < 12 ? 'Good' : 'Strong' }}
              </span>
            </div>

            <div class="dbu-dialog-note">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
              This creates a user on the <strong>{{ activeConn?.name }}</strong> server. Assign privileges after creation.
            </div>
          </div>
          <div class="dbu-dialog-foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showCreateModal = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="createSaving" @click="saveCreateUser">
              {{ createSaving ? 'Creating…' : 'Create User' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- ── Change password modal ───────────────────────────────── -->
    <Teleport to="body">
      <div v-if="showPwModal" class="dbu-overlay" @click.self="showPwModal = false">
        <div class="dbu-dialog">
          <div class="dbu-dialog-head">
            <div class="dbu-dialog-title">Change Password — {{ changePwUser?.username }}</div>
            <button class="dbu-icon-btn" @click="showPwModal = false">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="dbu-dialog-body">
            <div class="dbu-field">
              <label class="dbu-label">New Password</label>
              <div class="dbu-pw-wrap">
                <input class="dbu-input" :type="showNewPw ? 'text' : 'password'" v-model="changePwNew" placeholder="••••••••" autocomplete="new-password" />
                <button type="button" class="dbu-pw-eye" @click="showNewPw = !showNewPw">
                  <svg v-if="showNewPw" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                  <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                </button>
              </div>
            </div>
            <div class="dbu-field">
              <label class="dbu-label">Confirm Password</label>
              <input class="dbu-input" type="password" v-model="changePwConfirm" placeholder="••••••••" autocomplete="new-password" />
            </div>
          </div>
          <div class="dbu-dialog-foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showPwModal = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="changePwSaving" @click="saveChangePw">
              {{ changePwSaving ? 'Saving…' : 'Update Password' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- ── Add grant scope modal ───────────────────────────────── -->
    <Teleport to="body">
      <div v-if="showAddGrantModal" class="dbu-overlay" @click.self="showAddGrantModal = false">
        <div class="dbu-dialog">
          <div class="dbu-dialog-head">
            <div class="dbu-dialog-title">Add Grant Scope</div>
            <button class="dbu-icon-btn" @click="showAddGrantModal = false">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="dbu-dialog-body">
            <div class="dbu-field">
              <label class="dbu-label">Grant level</label>
              <div class="dbu-level-pills">
                <template v-if="isPG">
                  <button v-for="lvl in ['database','schema','table','sequence','function']" :key="lvl"
                    class="dbu-level-pill" :class="{ 'dbu-level-pill--active': newGrantLevel === lvl }"
                    @click="onLevelPillClick(lvl as any)">{{ levelLabel(lvl) }}</button>
                </template>
                <template v-else>
                  <button v-for="lvl in ['global','database','table']" :key="lvl"
                    class="dbu-level-pill" :class="{ 'dbu-level-pill--active': newGrantLevel === lvl }"
                    @click="onLevelPillClick(lvl as any)">{{ levelLabel(lvl) }}</button>
                </template>
              </div>
            </div>

            <!-- PostgreSQL: database level → pick which server database to grant CONNECT/CREATE on -->
            <template v-if="isPG && newGrantLevel === 'database'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
            </template>

            <!-- PostgreSQL: schema level → pick database then schema -->
            <template v-if="isPG && newGrantLevel === 'schema'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase" @change="newGrantSchema = ''; loadAvailableSchemas(newGrantDatabase)">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Schema</label>
                <select class="dbu-select" v-model="newGrantSchema" :disabled="!newGrantDatabase">
                  <option value="">— select —</option>
                  <option v-for="s in availableSchemas" :key="s" :value="s">{{ s }}</option>
                </select>
              </div>
            </template>

            <!-- PostgreSQL: table level → pick database → schema → table -->
            <template v-if="isPG && newGrantLevel === 'table'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase" @change="newGrantSchema = ''; newGrantTable = ''; availableTables = []; loadAvailableSchemas(newGrantDatabase)">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Schema</label>
                <select class="dbu-select" v-model="newGrantSchema" :disabled="!newGrantDatabase" @change="newGrantTable = ''; loadAvailableTables(newGrantDatabase, newGrantSchema)">
                  <option value="">— select —</option>
                  <option v-for="s in availableSchemas" :key="s" :value="s">{{ s }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Table</label>
                <select class="dbu-select" v-model="newGrantTable" :disabled="!newGrantSchema">
                  <option value="">— select —</option>
                  <option v-for="t in availableTables" :key="t.schema + '.' + t.table" :value="t.table">{{ t.table }}</option>
                </select>
              </div>
            </template>

            <!-- PostgreSQL: sequence level → database → schema → sequence -->
            <template v-if="isPG && newGrantLevel === 'sequence'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase" @change="newGrantSchema = ''; newGrantTable = ''; availableSequences = []; loadAvailableSchemas(newGrantDatabase)">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Schema</label>
                <select class="dbu-select" v-model="newGrantSchema" :disabled="!newGrantDatabase" @change="newGrantTable = ''; loadAvailableSequences(newGrantDatabase, newGrantSchema)">
                  <option value="">— select —</option>
                  <option v-for="s in availableSchemas" :key="s" :value="s">{{ s }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Sequence</label>
                <select class="dbu-select" v-model="newGrantTable" :disabled="!newGrantSchema">
                  <option value="">— select —</option>
                  <option v-for="s in availableSequences" :key="s.schema + '.' + s.sequence" :value="s.sequence">{{ s.sequence }}</option>
                </select>
              </div>
            </template>

            <!-- PostgreSQL: function level → database → schema → function -->
            <template v-if="isPG && newGrantLevel === 'function'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase" @change="newGrantSchema = ''; newGrantTable = ''; availableFunctions = []; loadAvailableSchemas(newGrantDatabase)">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Schema</label>
                <select class="dbu-select" v-model="newGrantSchema" :disabled="!newGrantDatabase" @change="newGrantTable = ''; loadAvailableFunctions(newGrantDatabase, newGrantSchema)">
                  <option value="">— select —</option>
                  <option v-for="s in availableSchemas" :key="s" :value="s">{{ s }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Function / Procedure</label>
                <select class="dbu-select" v-model="newGrantTable" :disabled="!newGrantSchema">
                  <option value="">— select —</option>
                  <option v-for="f in availableFunctions" :key="f.schema + '.' + f.function" :value="f.function">
                    {{ f.function }} <span v-if="f.kind === 'PROCEDURE'">(proc)</span>
                  </option>
                </select>
              </div>
            </template>

            <!-- MySQL/MariaDB: global → no target needed -->
            <template v-if="isMySQL && newGrantLevel === 'global'">
              <div class="dbu-grant-db-note">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
                Global grants apply to all databases (<code>*.*</code>)
              </div>
            </template>

            <!-- MySQL/MariaDB: database level -->
            <template v-if="isMySQL && newGrantLevel === 'database'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
            </template>

            <!-- MySQL/MariaDB: table level -->
            <template v-if="isMySQL && newGrantLevel === 'table'">
              <div class="dbu-field">
                <label class="dbu-label">Database</label>
                <select class="dbu-select" v-model="newGrantDatabase" @change="loadAvailableTables(newGrantDatabase, '')">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div class="dbu-field">
                <label class="dbu-label">Table</label>
                <select class="dbu-select" v-model="newGrantTable" :disabled="!newGrantDatabase">
                  <option value="">— select —</option>
                  <option v-for="t in availableTables" :key="t.schema + '.' + t.table" :value="t.table">{{ t.table }}</option>
                </select>
              </div>
            </template>

            <!-- Privilege checkboxes — always shown -->
            <div class="dbu-field dbu-priv-picker">
              <label class="dbu-label">
                Privileges
                <span class="dbu-hint">select which to grant</span>
              </label>
              <div class="dbu-priv-grid">
                <label
                  v-for="priv in privColsForLevel(newGrantLevel)"
                  :key="priv"
                  class="dbu-priv-pill"
                  :class="{ 'dbu-priv-pill--on': newGrantPrivileges.includes(priv) }"
                >
                  <input
                    type="checkbox"
                    :value="priv"
                    v-model="newGrantPrivileges"
                    style="display:none"
                  />
                  <span class="dbu-priv-pill-name">{{ priv }}</span>
                  <span class="dbu-priv-pill-desc">{{ (privDescriptions[priv] ?? '').split(' · ')[1] ?? privDescriptions[priv] ?? '' }}</span>
                </label>
                <div v-if="privColsForLevel(newGrantLevel).length === 0" class="dbu-priv-empty">
                  No configurable privileges for this level.
                </div>
              </div>
            </div>
          </div>
          <div class="dbu-dialog-foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showAddGrantModal = false">Cancel</button>
            <button
              class="base-btn base-btn--primary base-btn--sm"
              :disabled="newGrantPrivileges.length === 0 && privColsForLevel(newGrantLevel).length > 0"
              @click="confirmAddGrant"
            >Add Scope</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.dbu-root { background: var(--bg-body); }

/* Two-panel layout */
.dbu-layout {
  display: grid;
  grid-template-columns: 260px 1fr;
  gap: 0;
  min-height: 500px;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}

/* Panels */
.dbu-panel { display: flex; flex-direction: column; background: var(--bg-elevated); }
.dbu-panel--left { border-right: 1px solid var(--border); }
.dbu-panel-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 16px 10px;
  border-bottom: 1px solid var(--border);
}
.dbu-panel-title { font-size: 14px; font-weight: 700; color: var(--text-primary); }
.dbu-panel-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }

/* Connection info strip */
.dbu-conn-info {
  padding: 10px 14px 8px;
  border-bottom: 1px solid var(--border);
  background: rgba(255,255,255,0.02);
  display: flex; flex-direction: column; gap: 4px;
}
.dbu-conn-info__row {
  display: flex; align-items: center; gap: 6px;
  color: var(--text-muted);
}
.dbu-conn-info__row--host { font-family: var(--mono, monospace); font-size: 11px; }
.dbu-conn-info__name {
  font-size: 12px; font-weight: 600; color: var(--text-primary);
  overflow: hidden; white-space: nowrap; text-overflow: ellipsis; flex: 1;
}
.dbu-conn-info__driver {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.08em; padding: 1px 6px; border-radius: 3px;
  background: rgba(var(--brand-rgb,99,102,241),0.12); color: var(--brand);
  flex-shrink: 0;
}
.dbu-conn-info__host { font-size: 11px; color: var(--text-primary); flex: 1; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.dbu-conn-info__port { font-size: 11px; color: var(--text-muted); flex-shrink: 0; }

/* User list */
.dbu-search {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 12px; border-bottom: 1px solid var(--border);
  color: var(--text-muted);
}
.dbu-search-input {
  border: none; outline: none; background: transparent;
  color: var(--text-primary); font-size: 12px; width: 100%; font-family: inherit;
}
.dbu-search-input::placeholder { color: var(--text-muted); }
.dbu-user-list { overflow-y: auto; flex: 1; }
.dbu-user-row {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 12px; cursor: pointer;
  border-bottom: 1px solid var(--border);
  transition: background 0.1s;
}
.dbu-user-row:last-child { border-bottom: none; }
.dbu-user-row:hover { background: rgba(255,255,255,0.04); }
.dbu-user-row--active { background: rgba(var(--brand-rgb,99,102,241),0.1) !important; }
.dbu-user-avatar {
  width: 30px; height: 30px; border-radius: 50%;
  background: rgba(var(--brand-rgb,99,102,241),0.15);
  color: var(--brand); font-size: 11px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.dbu-user-info { flex: 1; min-width: 0; }
.dbu-user-name { font-size: 13px; font-weight: 600; color: var(--text-primary); truncate: ellipsis; overflow: hidden; white-space: nowrap; }
.dbu-user-meta { display: flex; align-items: center; gap: 4px; margin-top: 2px; flex-wrap: wrap; }
.dbu-meta-host { font-size: 11px; color: var(--text-muted); }
.dbu-user-actions { display: flex; gap: 4px; flex-shrink: 0; opacity: 0; transition: opacity 0.12s; }
.dbu-user-row:hover .dbu-user-actions { opacity: 1; }
.dbu-list-empty { padding: 24px; text-align: center; font-size: 12px; color: var(--text-muted); }

/* Badges */
.dbu-badge {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.08em; padding: 2px 6px; border-radius: 999px;
  border: 1px solid;
}
.dbu-badge--super { color: #f59e0b; border-color: #f59e0b; }
.dbu-badge--createdb { color: #60a5fa; border-color: #60a5fa; }

/* Icons */
.dbu-icon-btn {
  width: 26px; height: 26px; display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); border-radius: 5px; background: transparent;
  color: var(--text-muted); cursor: pointer; transition: all 0.12s;
}
.dbu-icon-btn:hover { background: rgba(255,255,255,0.08); color: var(--text-primary); }
.dbu-icon-btn--danger:hover { background: rgba(239,68,68,0.1); border-color: var(--danger); color: var(--danger); }

/* Right panel content */
.dbu-tabs {
  display: flex; border-bottom: 1px solid var(--border);
  padding: 0 16px;
}
.dbu-tab {
  display: flex; align-items: center; gap: 6px;
  padding: 10px 12px; font-size: 13px; color: var(--text-muted);
  background: transparent; border: none; cursor: pointer;
  border-bottom: 2px solid transparent; margin-bottom: -1px;
  transition: all 0.12s;
}
.dbu-tab:hover { color: var(--text-primary); }
.dbu-tab--active { color: var(--brand); border-bottom-color: var(--brand); }

/* Grants panel */
.dbu-grants-panel { flex: 1; overflow: auto; }
.dbu-grants-toolbar {
  display: flex; align-items: center; gap: 8px;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
}
.dbu-dirty-badge {
  font-size: 11px; color: var(--warning, #f59e0b);
  background: rgba(245,158,11,0.1); border: 1px solid rgba(245,158,11,0.3);
  border-radius: 999px; padding: 2px 8px;
}
.dbu-grants-empty {
  padding: 32px; text-align: center; font-size: 13px; color: var(--text-muted);
}

/* Grant sections */
.dbu-grant-sections { padding: 16px; display: flex; flex-direction: column; gap: 20px; }
.dbu-grant-section-title {
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.1em; color: var(--text-muted); margin-bottom: 8px;
}
.dbu-grant-table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: 6px; }
.dbu-grant-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.dbu-grant-table th {
  padding: 8px 10px; background: rgba(255,255,255,0.02);
  border-bottom: 1px solid var(--border);
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.08em; color: var(--text-muted); text-align: left;
}
.dbu-th-priv { text-align: center; min-width: 58px; }
.dbu-th-priv-label { display: block; font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; }
.dbu-th-priv-cat { display: block; font-size: 9px; font-weight: 400; text-transform: none; letter-spacing: 0; color: var(--text-muted); opacity: 0.7; }
.dbu-th-target { min-width: 120px; }
.dbu-th-del { width: 32px; }
.dbu-grant-table td { padding: 8px 10px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.dbu-grant-table tr:last-child td { border-bottom: none; }
.dbu-td-target { font-family: var(--mono, monospace); color: var(--text-primary); font-weight: 500; }
.dbu-td-priv { text-align: center; }
.dbu-td-del { text-align: center; }
.dbu-priv-check { display: flex; justify-content: center; cursor: pointer; }
.dbu-priv-check input { cursor: pointer; width: 14px; height: 14px; }

/* Empty state */
.dbu-empty-state {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  padding: 60px 24px; gap: 12px; color: var(--text-muted);
  text-align: center;
}
.dbu-empty-state--inline { flex: 1; padding: 40px 24px; }
.dbu-empty-icon { color: var(--text-muted); opacity: 0.5; }
.dbu-empty-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.dbu-empty-sub { font-size: 13px; color: var(--text-muted); max-width: 360px; }

/* Loading */
.dbu-loading {
  display: flex; align-items: center; justify-content: center;
  padding: 32px; color: var(--text-muted);
}

/* Misc */
.dbu-dim { color: var(--text-muted); }

/* Dialog / modals */
.dbu-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.dbu-dialog {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 10px; width: min(440px, 92vw);
  box-shadow: 0 24px 64px rgba(0,0,0,0.5);
  display: flex; flex-direction: column;
}
.dbu-dialog-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 20px; border-bottom: 1px solid var(--border);
}
.dbu-dialog-title { font-size: 15px; font-weight: 700; color: var(--text-primary); }
.dbu-dialog-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.dbu-dialog-foot {
  display: flex; justify-content: flex-end; gap: 8px;
  padding: 14px 20px; border-top: 1px solid var(--border);
}
.dbu-dialog-note {
  display: flex; align-items: flex-start; gap: 8px;
  font-size: 12px; color: var(--text-muted);
  background: rgba(255,255,255,0.03); border: 1px solid var(--border);
  border-radius: 6px; padding: 10px 12px;
}
.dbu-field { display: flex; flex-direction: column; gap: 5px; }
.dbu-label {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.4px; color: var(--text-muted);
}
.dbu-hint { font-size: 10px; text-transform: none; letter-spacing: 0; font-weight: 400; }
.dbu-input {
  padding: 7px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--bg-body); color: var(--text-primary);
  font-size: 13px; font-family: inherit; outline: none;
  transition: border-color 0.15s;
}
.dbu-input:focus { border-color: var(--brand); }
.dbu-select {
  padding: 7px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--bg-body); color: var(--text-primary);
  font-size: 13px; font-family: inherit; outline: none; cursor: pointer;
}
.dbu-pw-wrap { position: relative; }
.dbu-pw-wrap .dbu-input { padding-right: 38px; width: 100%; box-sizing: border-box; }
.dbu-pw-eye {
  position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
  background: transparent; border: none; color: var(--text-muted); cursor: pointer;
  padding: 4px; display: flex;
}
.dbu-pw-strength { display: flex; align-items: center; gap: 8px; }
.dbu-pw-bar { flex: 1; height: 4px; background: rgba(255,255,255,0.1); border-radius: 2px; overflow: hidden; }
.dbu-pw-fill { height: 100%; border-radius: 2px; transition: width 0.3s, background 0.3s; }
.dbu-pw-fill--weak { background: var(--danger, #ef4444); }
.dbu-pw-fill--ok { background: #f59e0b; }
.dbu-pw-fill--strong { background: #22c55e; }
.dbu-pw-label { font-size: 11px; color: var(--text-muted); white-space: nowrap; }

/* Privilege picker inside Add Grant Scope modal */
.dbu-priv-picker { gap: 6px; }
.dbu-priv-grid {
  display: flex; flex-wrap: wrap; gap: 6px;
}
.dbu-priv-pill {
  display: flex; flex-direction: column;
  padding: 7px 12px; border: 1px solid var(--border);
  border-radius: 6px; cursor: pointer; min-width: 90px;
  background: var(--bg-body); transition: all 0.12s;
  user-select: none;
}
.dbu-priv-pill:hover { border-color: var(--brand); }
.dbu-priv-pill--on { border-color: var(--brand); background: rgba(var(--brand-rgb,99,102,241),0.1); }
.dbu-priv-pill-name { font-size: 12px; font-weight: 700; color: var(--text-primary); }
.dbu-priv-pill--on .dbu-priv-pill-name { color: var(--brand); }
.dbu-priv-pill-desc { font-size: 10px; color: var(--text-muted); margin-top: 2px; }
.dbu-priv-empty { font-size: 12px; color: var(--text-muted); }

/* Grant scope info note */
.dbu-grant-db-note {
  display: flex; align-items: center; gap: 7px;
  font-size: 12px; color: var(--text-muted);
  background: rgba(255,255,255,0.03); border: 1px solid var(--border);
  border-radius: 5px; padding: 8px 10px;
}
.dbu-grant-db-note strong { color: var(--text-primary); }
.dbu-grant-db-note code { font-family: var(--mono, monospace); font-size: 11px; }

/* Grant level pills */
.dbu-level-pills { display: flex; gap: 6px; flex-wrap: wrap; }
.dbu-level-pill {
  padding: 5px 12px; border: 1px solid var(--border);
  border-radius: 999px; background: transparent; color: var(--text-muted);
  font-size: 12px; cursor: pointer; font-family: inherit;
  transition: all 0.12s;
}
.dbu-level-pill:hover { border-color: var(--brand); color: var(--text-primary); }
.dbu-level-pill--active { border-color: var(--brand); color: var(--brand); background: rgba(var(--brand-rgb,99,102,241),0.08); }

/* Responsive */
@media (max-width: 700px) {
  .dbu-layout { grid-template-columns: 1fr; }
  .dbu-panel--left { border-right: none; border-bottom: 1px solid var(--border); max-height: 260px; }
}

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin 0.8s linear infinite; }
</style>
