<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConnections } from '@/composables/useConnections'
import { readableError } from '@/utils/httpError'
import { useListFilter } from '@/composables/useListFilter'
import { useSort } from '@/composables/useSort'
import SortIcon from '@/components/ui/SortIcon.vue'
import ConnectionGate from '@/components/ui/ConnectionGate.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()
const toast = useToast()
const { connections } = useConnections()
const route = useRoute()

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
// true when the API answers 403: the account lacks "Manage Database Users & Grants"
const forbidden = ref(false)
const selectedUser = ref<DBUser | null>(null)

const { search: userSearch, filtered: searchedUsers } = useListFilter(users, (u, q) =>
  u.username.toLowerCase().includes(q) || (u.host || '').toLowerCase().includes(q)
)
function dbUserSortValue(u: DBUser, key: string): unknown {
  return u[key as keyof DBUser]
}
const { sortKey: userSortKey, sortDir: userSortDir, toggleSort: toggleUserSort, sort: sortUsers } = useSort<DBUser>(dbUserSortValue)
const filteredUsers = computed(() => sortUsers(searchedUsers.value))

async function loadUsers() {
  if (!props.activeConnId) return
  loadingUsers.value = true
  try {
    const { data } = await axios.get<DBUser[]>(`/api/connections/${props.activeConnId}/db-users`)
    forbidden.value = false
    users.value = data ?? []
    if (selectedUser.value) {
      selectedUser.value = users.value.find(u => u.username === selectedUser.value!.username && u.host === selectedUser.value!.host) ?? null
    }
  } catch (e) {
    if (axios.isAxiosError(e) && e.response?.status === 403) {
      forbidden.value = true
      users.value = []
      selectedUser.value = null
      return
    }
    toast.error(readableError(e, { action: 'Load DB users', fallback: 'Failed to load database users' }))
  } finally {
    loadingUsers.value = false
  }
}

function selectUser(u: DBUser) {
  selectedUser.value = u
  loadGrants(u)
  // The endpoint answers [] for MySQL, so no driver check here (the connection
  // list may still be loading on a deep link).
  loadDefaultPrivs(u)
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
  // Review the exact GRANT/REVOKE statements first; apply or send for approval from there.
  const u = selectedUser.value
  await openPlan({
    title: `Review grant changes — ${u.username}`,
    method: 'put',
    url: `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}/grants`,
    payload: { host: u.host || '%', grants: editingGrants.value.filter(g => (g.privileges ?? []).length > 0) },
    onDone: async () => {
      grantsDirty.value = false
      await loadGrants(u)
    },
  })
}

// ── Plan review: preview SQL → apply now or submit for approval ───

interface GrantBatch { database?: string; statements: string[] }
interface WorkflowOption { id: number; name: string }
interface PlanOptions {
  title: string
  method: 'put' | 'post'
  url: string
  payload: Record<string, unknown>
  fromWizard?: boolean
  onDone?: () => void | Promise<void>
}

const plan = reactive({
  open: false,
  loading: false,
  busy: false,
  error: '',
  title: '',
  batches: [] as GrantBatch[],
  approvalRequired: false,
  workflows: [] as WorkflowOption[],
  workflowId: null as number | null,
  requestTitle: '',
  requestDesc: '',
  showRequestForm: false,
})
let planOpts: PlanOptions | null = null

const planStatementCount = computed(() => plan.batches.reduce((n, b) => n + b.statements.length, 0))
const planSql = computed(() => plan.batches
  .map(b => (b.database ? `-- database: ${b.database}\n` : '') + b.statements.map(st => `${st};`).join('\n'))
  .join('\n\n'))

async function callPlan(mode: 'preview' | 'apply' | 'request', extra: Record<string, unknown> = {}) {
  if (!planOpts) throw new Error('no plan')
  const body = { ...planOpts.payload, mode, ...extra }
  return planOpts.method === 'put'
    ? axios.put(planOpts.url, body)
    : axios.post(planOpts.url, body)
}

async function openPlan(opts: PlanOptions) {
  planOpts = opts
  Object.assign(plan, {
    open: true, loading: true, busy: false, error: '', title: opts.title, batches: [],
    approvalRequired: false, workflows: [], workflowId: null, requestTitle: '', requestDesc: '', showRequestForm: false,
  })
  try {
    const { data } = await callPlan('preview')
    plan.batches = data?.batches ?? []
    plan.approvalRequired = !!data?.approval_required
    plan.workflows = data?.workflows ?? []
    plan.workflowId = plan.workflows[0]?.id ?? null
    plan.showRequestForm = plan.approvalRequired
  } catch (e) {
    plan.error = readableError(e, { action: 'Preview', fallback: 'Failed to build the SQL for this change' })
  } finally {
    plan.loading = false
  }
}

async function finishPlan() {
  plan.open = false
  if (planOpts?.fromWizard) wiz.open = false
  await planOpts?.onDone?.()
}

async function applyPlan() {
  plan.busy = true
  plan.error = ''
  try {
    const { data } = await callPlan('apply')
    toast.success(data?.status === 'unchanged' ? 'Nothing to change' : 'Grants applied')
    await finishPlan()
  } catch (e) {
    if (axios.isAxiosError(e) && e.response?.status === 409 && e.response.data?.approval_required) {
      plan.approvalRequired = true
      plan.workflows = e.response.data.workflows ?? []
      plan.workflowId = plan.workflows[0]?.id ?? null
      plan.showRequestForm = true
      plan.error = 'This connection requires approval for changes. Submit it as an approval request instead.'
    } else {
      plan.error = readableError(e, { action: 'Apply grants', fallback: 'Failed to apply grants' })
    }
  } finally {
    plan.busy = false
  }
}

async function requestPlan() {
  plan.busy = true
  plan.error = ''
  try {
    const { data } = await callPlan('request', {
      workflow_id: plan.workflowId ?? 0,
      title: plan.requestTitle.trim(),
      description: plan.requestDesc.trim(),
    })
    const ids = (data?.requests ?? []).map((r: { id: number }) => `#${r.id}`).join(', ')
    toast.success(`Approval request ${ids} submitted. It runs once approved (Changes › Approvals).`)
    await finishPlan()
  } catch (e) {
    plan.error = readableError(e, { action: 'Submit for approval', fallback: 'Failed to submit approval request' })
  } finally {
    plan.busy = false
  }
}

function planOptsFromWizard() {
  return !!planOpts?.fromWizard
}

function backToWizard() {
  plan.open = false
  wiz.open = true
}

async function copyPlanSql() {
  try {
    await navigator.clipboard.writeText(planSql.value)
    toast.success('SQL copied')
  } catch {
    toast.error('Could not copy to clipboard')
  }
}

// ── Grant access wizard (schema-wide presets) ─────────────────────

type AccessPreset = 'readonly' | 'readwrite' | 'custom'

const wiz = reactive({
  open: false,
  action: 'grant' as 'grant' | 'revoke',
  userKey: '',
  database: '',
  schema: '',
  preset: 'readonly' as AccessPreset,
  tablePrivs: ['SELECT'] as string[],
  seqPrivs: ['USAGE', 'SELECT'] as string[],
  includeSeq: true,
  includeFuture: true,
  futureRoles: [] as string[],
})
const wizSchemas = ref<string[]>([])
const wizOwners = ref<{ current_user: string; schema_owner: string; owners: string[] }>({ current_user: '', schema_owner: '', owners: [] })
const wizOwnersLoading = ref(false)

const userKey = (u: DBUser) => `${u.username}@${u.host || ''}`
const wizUser = computed(() => users.value.find(u => userKey(u) === wiz.userKey) ?? null)

// Roles whose new objects can be covered by ALTER DEFAULT PRIVILEGES
const wizRoleChoices = computed(() => {
  const set = new Set<string>()
  if (wizOwners.value.current_user) set.add(wizOwners.value.current_user)
  if (wizOwners.value.schema_owner) set.add(wizOwners.value.schema_owner)
  for (const o of wizOwners.value.owners) set.add(o)
  // pg_* roles (e.g. pg_database_owner, owner of "public" since PG15) are built-in
  // pseudo-roles that never create tables themselves
  return [...set].filter(r => !r.startsWith('pg_'))
})

const presetInfo: Record<AccessPreset, { label: string; pg: string; mysql: string }> = {
  readonly: { label: 'Read only', pg: 'SELECT on tables · USAGE, SELECT on sequences', mysql: 'SELECT, SHOW VIEW' },
  readwrite: { label: 'Read / write', pg: 'SELECT, INSERT, UPDATE, DELETE · sequences USAGE, SELECT, UPDATE', mysql: 'SELECT, INSERT, UPDATE, DELETE, SHOW VIEW' },
  custom: { label: 'Custom', pg: 'Pick the privileges yourself', mysql: 'Pick the privileges yourself' },
}

async function openWizard(prefill: { user?: string; host?: string; database?: string; schema?: string; action?: 'grant' | 'revoke' } = {}) {
  const preUser = prefill.user
    ? users.value.find(u => u.username === prefill.user && (prefill.host == null || (u.host || '') === prefill.host))
    : selectedUser.value
  Object.assign(wiz, {
    open: true,
    action: prefill.action ?? 'grant',
    userKey: preUser ? userKey(preUser) : '',
    database: prefill.database ?? (isPG.value ? (activeConn.value?.database || '') : ''),
    schema: prefill.schema ?? (isPG.value ? 'public' : ''),
    preset: 'readonly',
    tablePrivs: isPG.value ? ['SELECT'] : ['SELECT'],
    seqPrivs: ['USAGE', 'SELECT'],
    includeSeq: true,
    includeFuture: true,
    futureRoles: [],
  })
  wizSchemas.value = []
  await loadAvailableDbs()
  if (isPG.value) {
    if (!wiz.database && availableDbs.value.length) wiz.database = availableDbs.value[0]
    await onWizDatabaseChange(false)
  }
}

async function onWizDatabaseChange(resetSchema = true) {
  if (!isPG.value || !props.activeConnId) return
  if (resetSchema) wiz.schema = ''
  wizSchemas.value = []
  if (!wiz.database) return
  try {
    const { data } = await axios.get<string[]>(`/api/connections/${props.activeConnId}/db-users-schemas`, { params: { db: wiz.database } })
    wizSchemas.value = data ?? []
  } catch { wizSchemas.value = [] }
  if (wiz.schema && !wizSchemas.value.includes(wiz.schema)) wiz.schema = ''
  if (!wiz.schema && wizSchemas.value.includes('public')) wiz.schema = 'public'
  await onWizSchemaChange()
}

async function onWizSchemaChange() {
  wizOwners.value = { current_user: '', schema_owner: '', owners: [] }
  wiz.futureRoles = []
  if (!isPG.value || !props.activeConnId || !wiz.schema) return
  wizOwnersLoading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${props.activeConnId}/db-users-schema-owners`, { params: { db: wiz.database, schema: wiz.schema } })
    wizOwners.value = { current_user: data?.current_user ?? '', schema_owner: data?.schema_owner ?? '', owners: data?.owners ?? [] }
    // Default: cover objects created by whoever already owns tables here (usually the
    // migration/app role); with no tables yet, the connection's own user.
    wiz.futureRoles = wizOwners.value.owners.length ? [...wizOwners.value.owners] : (wizOwners.value.current_user ? [wizOwners.value.current_user] : [])
  } catch { /* owners are a convenience; the plan still works without FOR ROLE */ } finally {
    wizOwnersLoading.value = false
  }
}

function toggleIn(list: string[], v: string) {
  const i = list.indexOf(v)
  if (i >= 0) list.splice(i, 1)
  else list.push(v)
}

const wizCanPreview = computed(() => {
  if (!wizUser.value) return false
  if (isPG.value) {
    if (!wiz.database || !wiz.schema) return false
  } else if (!wiz.database) return false
  if (wiz.action === 'grant' && wiz.preset === 'custom' && wiz.tablePrivs.length === 0) return false
  return true
})

async function previewWizard() {
  const u = wizUser.value
  if (!u || !props.activeConnId) return
  const payload: Record<string, unknown> = {
    action: wiz.action,
    host: u.host || '%',
    database: wiz.database,
    schema: isPG.value ? wiz.schema : '',
    preset: wiz.preset,
    table_privileges: wiz.tablePrivs,
    sequence_privileges: wiz.seqPrivs,
    include_sequences: isPG.value && wiz.includeSeq,
    include_future: isPG.value && wiz.includeFuture,
    future_for_roles: isPG.value && wiz.includeFuture ? wiz.futureRoles : [],
  }
  wiz.open = false
  const scope = isPG.value ? `${wiz.database}.${wiz.schema}` : wiz.database
  await openPlan({
    title: `${wiz.action === 'revoke' ? 'Revoke' : 'Grant'} ${u.username} access to ${scope}`,
    method: 'post',
    url: `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}/access`,
    payload,
    fromWizard: true,
    onDone: async () => {
      if (selectedUser.value && userKey(selectedUser.value) === userKey(u)) {
        await loadGrants(u)
        if (isPG.value) await loadDefaultPrivs(u)
      } else {
        selectUser(u)
      }
    },
  })
}

// ── Default privileges (PostgreSQL: future objects) ───────────────

interface DefaultPriv {
  database: string
  schema: string
  owner: string
  object_type: string
  objects: string
  privileges: string[]
}

const defaultPrivs = ref<DefaultPriv[]>([])
const loadingDefaultPrivs = ref(false)

async function loadDefaultPrivs(u: DBUser) {
  if (!props.activeConnId) return
  loadingDefaultPrivs.value = true
  defaultPrivs.value = []
  try {
    const { data } = await axios.get<DefaultPriv[]>(
      `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}/default-privileges`
    )
    defaultPrivs.value = data ?? []
  } catch (e) {
    toast.error(readableError(e, { action: 'Load default privileges', fallback: 'Failed to load default privileges' }))
  } finally {
    loadingDefaultPrivs.value = false
  }
}

async function revokeDefaultPriv(d: DefaultPriv) {
  const u = selectedUser.value
  if (!u || !props.activeConnId) return
  await openPlan({
    title: `Stop granting ${d.objects.toLowerCase()} created by ${d.owner} to ${u.username}`,
    method: 'post',
    url: `/api/connections/${props.activeConnId}/db-users/${encodeURIComponent(u.username)}/access`,
    payload: { action: 'revoke_default', database: d.database, schema: d.schema, owner: d.owner, object_type: d.object_type },
    onDone: () => loadDefaultPrivs(u),
  })
}

// ── Access summary (per database › schema) ────────────────────────

interface AccessSummary {
  key: string
  database: string
  schema: string
  schemaPrivs: string[]
  tableCount: number
  tablePrivs: string[]
  sequenceCount: number
  futureObjects: string[]
}

const accessSummary = computed<AccessSummary[]>(() => {
  if (!isPG.value) return []
  const map = new Map<string, AccessSummary>()
  const get = (database: string, schema: string) => {
    const key = `${database}\u0000${schema}`
    let e = map.get(key)
    if (!e) {
      e = { key, database, schema, schemaPrivs: [], tableCount: 0, tablePrivs: [], sequenceCount: 0, futureObjects: [] }
      map.set(key, e)
    }
    return e
  }
  for (const g of grants.value) {
    if (!g.schema) continue
    const e = get(g.database ?? '', g.schema)
    if (g.level === 'schema') e.schemaPrivs = [...g.privileges]
    if (g.level === 'table') {
      e.tableCount++
      for (const p of g.privileges) if (!e.tablePrivs.includes(p)) e.tablePrivs.push(p)
    }
    if (g.level === 'sequence') e.sequenceCount++
  }
  for (const d of defaultPrivs.value) {
    if (!d.schema) continue
    const e = get(d.database, d.schema)
    const label = `${d.objects.toLowerCase()}: ${d.privileges.join(', ')}`
    if (!e.futureObjects.includes(label)) e.futureObjects.push(label)
  }
  // Hide schemas that only have the default PUBLIC USAGE and nothing else
  return [...map.values()]
    .filter(e => e.tableCount > 0 || e.sequenceCount > 0 || e.futureObjects.length > 0)
    .sort((a, b) => (a.database + a.schema).localeCompare(b.database + b.schema))
})

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
const activeTab = ref<'grants' | 'defaults' | 'password'>('grants')

// ── Init ─────────────────────────────────────────────────────────

// Deep links: /db-users?conn=ID&user=NAME&host=H&db=DB&schema=S&grant=1
// (used by the connection card / context-menu shortcuts and the command palette)
// On a deep link the connection list can still be loading; the wizard needs the driver.
function waitForActiveConn(timeoutMs = 5000): Promise<void> {
  if (activeConn.value) return Promise.resolve()
  return new Promise(resolve => {
    const stop = watch(activeConn, c => { if (c) { stop(); resolve() } })
    setTimeout(() => { stop(); resolve() }, timeoutMs)
  })
}

async function applyRouteQuery() {
  await waitForActiveConn()
  const q = route.query
  const user = typeof q.user === 'string' ? q.user : ''
  if (user) {
    const host = typeof q.host === 'string' ? q.host : undefined
    const u = users.value.find(x => x.username === user && (host == null || (x.host || '') === host))
    if (u) selectUser(u)
  }
  if (q.grant === '1' && !forbidden.value) {
    await openWizard({
      user: user || undefined,
      database: typeof q.db === 'string' ? q.db : undefined,
      schema: typeof q.schema === 'string' ? q.schema : undefined,
    })
  }
}

onMounted(async () => {
  const wanted = Number(route.query.conn)
  if (wanted && wanted !== props.activeConnId) {
    emit('set-conn', wanted) // the watcher below loads users for it
    return
  }
  if (props.activeConnId) {
    await loadUsers()
    await applyRouteQuery()
  }
})

watch(() => props.activeConnId, async (id) => {
  if (id) {
    selectedUser.value = null
    grants.value = []
    defaultPrivs.value = []
    await loadUsers()
    if (Number(route.query.conn) === id) await applyRouteQuery()
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
            <div class="page-title">DB Users &amp; Grants</div>
            <div class="page-subtitle">
              Create and manage database-level users, passwords, and privilege grants — directly on the connected server.
              Use <strong>Grant access</strong> to give a user read-only or read/write access to a whole schema, including tables created later.
            </div>
          </div>
          <div class="page-hero__actions" v-if="isSupported && activeConnId && !forbidden">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadUsers">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="openCreateModal">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              Create User
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="users.length === 0" @click="openWizard()">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
              Grant access
            </button>
          </div>
        </section>

        <!-- No connection selected -->
        <ConnectionGate v-if="!isSupported" :drivers="['postgres', 'mysql', 'mariadb']" label="PostgreSQL or MySQL" short="database" purpose="to manage its database users and grants" icon="users" @select="(id: number) => emit('set-conn', id)" />

        <!-- No permission -->
        <EmptyState
          v-else-if="forbidden"
          icon="lock"
          title="You don't have access to DB Users & Grants"
          text="This page needs the &quot;Manage Database Users & Grants&quot; permission. Ask an administrator to add it to your role in Settings › Roles."
        />

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

            <!-- Sort -->
            <div class="dbu-sort-bar">
              <span class="dbu-sort-label">Sort</span>
              <button class="dbu-sort-btn" :class="{ sorted: userSortKey === 'username' }" @click="toggleUserSort('username')">
                Username <SortIcon :active="userSortKey === 'username'" :dir="userSortDir" />
              </button>
              <button class="dbu-sort-btn" :class="{ sorted: userSortKey === 'host' }" @click="toggleUserSort('host')">
                Host <SortIcon :active="userSortKey === 'host'" :dir="userSortDir" />
              </button>
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
                  <button class="dbu-icon-btn" title="Grant access to a schema / database" @click.stop="openWizard({ user: u.username, host: u.host || '' })">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>
                  </button>
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
                  <button class="base-btn base-btn--primary base-btn--sm" @click="openWizard({ user: selectedUser.username, host: selectedUser.host || '' })">Grant access</button>
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
                <button
                  v-if="isPG"
                  class="dbu-tab"
                  :class="{ 'dbu-tab--active': activeTab === 'defaults' }"
                  @click="activeTab = 'defaults'"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>
                  Future objects
                  <span v-if="defaultPrivs.length" class="dbu-tab-count">{{ defaultPrivs.length }}</span>
                </button>
              </div>

              <!-- Default privileges tab (PostgreSQL) -->
              <div v-if="activeTab === 'defaults'" class="dbu-grants-panel">
                <div class="dbu-help">
                  <strong>ALTER DEFAULT PRIVILEGES</strong> — privileges this user automatically gets on objects created later.
                  They only apply to objects created by the listed <em>owner</em> role.
                </div>
                <div v-if="loadingDefaultPrivs" class="dbu-loading">
                  <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                </div>
                <div v-else-if="defaultPrivs.length === 0" class="dbu-grants-empty">
                  No default privileges for this user. Use "Grant access" with "Also tables created later" to add them.
                </div>
                <div v-else class="dbu-grant-table-wrap">
                  <table class="dbu-grant-table">
                    <thead>
                      <tr>
                        <th class="dbu-th-target">Database › Schema</th>
                        <th class="dbu-th-target">Objects</th>
                        <th class="dbu-th-target">Created by</th>
                        <th class="dbu-th-target">Privileges</th>
                        <th class="dbu-th-del"></th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="d in defaultPrivs" :key="d.database + d.schema + d.owner + d.object_type">
                        <td class="dbu-td-target">{{ d.database }} › {{ d.schema || '(all schemas)' }}</td>
                        <td class="dbu-td-target">{{ d.objects || d.object_type }}</td>
                        <td class="dbu-td-target">{{ d.owner }}</td>
                        <td class="dbu-td-target">{{ d.privileges.join(', ') }}</td>
                        <td class="dbu-td-del">
                          <button class="dbu-icon-btn dbu-icon-btn--danger" title="Revoke (review first)" @click="revokeDefaultPriv(d)">
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                          </button>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>

              <!-- Grants tab -->
              <div v-if="activeTab === 'grants'" class="dbu-grants-panel">
                <!-- Access summary per database › schema -->
                <div v-if="accessSummary.length && !loadingGrants" class="dbu-summary">
                  <div v-for="a in accessSummary" :key="a.key" class="dbu-summary-card">
                    <div class="dbu-summary-scope">{{ a.database }} › {{ a.schema }}</div>
                    <div class="dbu-summary-line">
                      <span v-if="a.tableCount">{{ a.tablePrivs.join(', ') }} on {{ a.tableCount }} table{{ a.tableCount !== 1 ? 's' : '' }}</span>
                      <span v-if="a.sequenceCount"> · {{ a.sequenceCount }} sequence{{ a.sequenceCount !== 1 ? 's' : '' }}</span>
                    </div>
                    <div v-if="a.futureObjects.length" class="dbu-summary-line dbu-dim">Future {{ a.futureObjects.join(' · ') }}</div>
                    <div class="dbu-summary-actions">
                      <button class="dbu-link-btn" @click="openWizard({ user: selectedUser.username, host: selectedUser.host || '', database: a.database, schema: a.schema })">Change</button>
                      <button class="dbu-link-btn dbu-link-btn--danger" @click="openWizard({ user: selectedUser.username, host: selectedUser.host || '', database: a.database, schema: a.schema, action: 'revoke' })">Revoke</button>
                    </div>
                  </div>
                </div>

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
                    {{ savingGrants ? 'Saving…' : 'Review & save' }}
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

    <!-- ── Grant access wizard ─────────────────────────────────── -->
    <Teleport to="body">
      <div v-if="wiz.open" class="dbu-overlay" @click.self="wiz.open = false">
        <div class="dbu-dialog dbu-dialog--wide">
          <div class="dbu-dialog-head">
            <div class="dbu-dialog-title">{{ wiz.action === 'revoke' ? 'Revoke access' : 'Grant access' }}</div>
            <button class="dbu-icon-btn" @click="wiz.open = false">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="dbu-dialog-body">
            <div class="dbu-level-pills">
              <button class="dbu-level-pill" :class="{ 'dbu-level-pill--active': wiz.action === 'grant' }" @click="wiz.action = 'grant'">Grant</button>
              <button class="dbu-level-pill" :class="{ 'dbu-level-pill--active': wiz.action === 'revoke' }" @click="wiz.action = 'revoke'">Revoke</button>
            </div>

            <div class="dbu-field">
              <label class="dbu-label">1 · User</label>
              <select class="dbu-select" v-model="wiz.userKey">
                <option value="">— select —</option>
                <option v-for="u in users" :key="userKey(u)" :value="userKey(u)">{{ u.username }}{{ u.host ? '@' + u.host : '' }}</option>
              </select>
              <span class="dbu-hint">Not there yet? <button class="dbu-link-btn" @click="wiz.open = false; openCreateModal()">Create the user</button> first.</span>
            </div>

            <div class="dbu-field-row">
              <div class="dbu-field">
                <label class="dbu-label">2 · Database</label>
                <select class="dbu-select" v-model="wiz.database" @change="onWizDatabaseChange()">
                  <option value="">— select —</option>
                  <option v-for="d in availableDbs" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
              <div v-if="isPG" class="dbu-field">
                <label class="dbu-label">Schema</label>
                <select class="dbu-select" v-model="wiz.schema" :disabled="!wiz.database" @change="onWizSchemaChange()">
                  <option value="">— select —</option>
                  <option v-for="sc in wizSchemas" :key="sc" :value="sc">{{ sc }}</option>
                </select>
              </div>
            </div>

            <template v-if="wiz.action === 'grant'">
              <div class="dbu-field">
                <label class="dbu-label">3 · Access</label>
                <div class="dbu-preset-list">
                  <label v-for="(info, key) in presetInfo" :key="key" class="dbu-preset" :class="{ 'dbu-preset--on': wiz.preset === key }">
                    <input type="radio" :value="key" v-model="wiz.preset" />
                    <span class="dbu-preset-name">{{ info.label }}</span>
                    <span class="dbu-preset-desc">{{ isPG ? info.pg : info.mysql }}</span>
                  </label>
                </div>
              </div>

              <div v-if="wiz.preset === 'custom'" class="dbu-field dbu-priv-picker">
                <label class="dbu-label">{{ isPG ? 'Table privileges' : 'Privileges' }}</label>
                <div class="dbu-priv-grid">
                  <label v-for="priv in (isPG ? pgTablePrivs : mysqlPrivs)" :key="priv" class="dbu-priv-pill" :class="{ 'dbu-priv-pill--on': wiz.tablePrivs.includes(priv) }">
                    <input type="checkbox" style="display:none" :checked="wiz.tablePrivs.includes(priv)" @change="toggleIn(wiz.tablePrivs, priv)" />
                    <span class="dbu-priv-pill-name">{{ priv }}</span>
                  </label>
                </div>
                <template v-if="isPG && wiz.includeSeq">
                  <label class="dbu-label" style="margin-top:8px">Sequence privileges</label>
                  <div class="dbu-priv-grid">
                    <label v-for="priv in pgSequencePrivs" :key="priv" class="dbu-priv-pill" :class="{ 'dbu-priv-pill--on': wiz.seqPrivs.includes(priv) }">
                      <input type="checkbox" style="display:none" :checked="wiz.seqPrivs.includes(priv)" @change="toggleIn(wiz.seqPrivs, priv)" />
                      <span class="dbu-priv-pill-name">{{ priv }}</span>
                    </label>
                  </div>
                </template>
              </div>
            </template>

            <template v-if="isPG">
              <label class="dbu-check">
                <input type="checkbox" v-model="wiz.includeSeq" />
                <span>Include sequences <span class="dbu-hint">(needed for serial / identity columns)</span></span>
              </label>
              <label class="dbu-check">
                <input type="checkbox" v-model="wiz.includeFuture" />
                <span>Also tables created later <span class="dbu-hint">(ALTER DEFAULT PRIVILEGES)</span></span>
              </label>
              <div v-if="wiz.includeFuture" class="dbu-field dbu-indent">
                <label class="dbu-label">For objects created by</label>
                <div v-if="wizOwnersLoading" class="dbu-hint">Loading roles…</div>
                <div v-else-if="wizRoleChoices.length === 0" class="dbu-hint">Pick a schema first.</div>
                <div v-else class="dbu-priv-grid">
                  <label v-for="r in wizRoleChoices" :key="r" class="dbu-priv-pill" :class="{ 'dbu-priv-pill--on': wiz.futureRoles.includes(r) }">
                    <input type="checkbox" style="display:none" :checked="wiz.futureRoles.includes(r)" @change="toggleIn(wiz.futureRoles, r)" />
                    <span class="dbu-priv-pill-name">{{ r }}</span>
                    <span class="dbu-priv-pill-desc">
                      {{ r === wizOwners.current_user ? 'this connection' : '' }}{{ r === wizOwners.current_user && wizOwners.owners.includes(r) ? ' · ' : '' }}{{ wizOwners.owners.includes(r) ? 'owns tables here' : (r === wizOwners.schema_owner ? 'schema owner' : '') }}
                    </span>
                  </label>
                </div>
                <span class="dbu-hint">Default privileges only cover objects created by these roles — usually your migration / app owner.</span>
              </div>
            </template>
            <div v-else class="dbu-dialog-note">
              MySQL grants on <code>database.*</code> already cover tables created later.
            </div>
          </div>
          <div class="dbu-dialog-foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="wiz.open = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="!wizCanPreview" @click="previewWizard">Review SQL</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- ── Review SQL → apply / submit for approval ────────────── -->
    <Teleport to="body">
      <div v-if="plan.open" class="dbu-overlay" @click.self="!plan.busy && (plan.open = false)">
        <div class="dbu-dialog dbu-dialog--wide">
          <div class="dbu-dialog-head">
            <div class="dbu-dialog-title">{{ plan.title }}</div>
            <button class="dbu-icon-btn" :disabled="plan.busy" @click="plan.open = false">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="dbu-dialog-body">
            <div v-if="plan.loading" class="dbu-loading">
              <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            </div>
            <template v-else>
              <div v-if="planStatementCount === 0 && !plan.error" class="dbu-grants-empty">Nothing to change — the user already has exactly these privileges.</div>
              <template v-else-if="planStatementCount > 0">
                <div class="dbu-plan-head">
                  <span>{{ planStatementCount }} statement{{ planStatementCount !== 1 ? 's' : '' }} will run on <strong>{{ activeConn?.name }}</strong></span>
                  <button class="dbu-link-btn" @click="copyPlanSql">Copy SQL</button>
                </div>
                <pre class="dbu-sql">{{ planSql }}</pre>
              </template>

              <div v-if="plan.approvalRequired" class="dbu-dialog-note dbu-dialog-note--warn">
                Changes on this connection go through an approval workflow. Submit the SQL above for approval; it runs once approved.
              </div>

              <div v-if="plan.showRequestForm && planStatementCount > 0" class="dbu-request-form">
                <div v-if="plan.workflows.length === 0" class="dbu-hint">No approval workflow applies to this connection.</div>
                <template v-else>
                  <div v-if="plan.workflows.length > 1" class="dbu-field">
                    <label class="dbu-label">Workflow</label>
                    <select class="dbu-select" v-model="plan.workflowId">
                      <option v-for="wf in plan.workflows" :key="wf.id" :value="wf.id">{{ wf.name }}</option>
                    </select>
                  </div>
                  <div class="dbu-field">
                    <label class="dbu-label">Title <span class="dbu-hint">(optional)</span></label>
                    <input class="dbu-input" v-model="plan.requestTitle" :placeholder="plan.title" />
                  </div>
                  <div class="dbu-field">
                    <label class="dbu-label">Reason <span class="dbu-hint">(optional)</span></label>
                    <input class="dbu-input" v-model="plan.requestDesc" placeholder="e.g. read-only access for the reporting team" />
                  </div>
                </template>
              </div>

              <div v-if="plan.error" class="dbu-error">{{ plan.error }}</div>
            </template>
          </div>
          <div class="dbu-dialog-foot">
            <button v-if="planOptsFromWizard()" class="base-btn base-btn--ghost base-btn--sm" :disabled="plan.busy" @click="backToWizard">Back</button>
            <div style="flex:1"></div>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="plan.busy" @click="plan.open = false">Cancel</button>
            <template v-if="!plan.loading && planStatementCount > 0">
              <button
                v-if="plan.workflows.length > 0 && !plan.showRequestForm"
                class="base-btn base-btn--ghost base-btn--sm"
                :disabled="plan.busy"
                @click="plan.showRequestForm = true"
              >Send for approval…</button>
              <button
                v-if="plan.showRequestForm && plan.workflows.length > 0"
                class="base-btn base-btn--primary base-btn--sm"
                :disabled="plan.busy"
                @click="requestPlan"
              >{{ plan.busy ? 'Submitting…' : 'Submit for approval' }}</button>
              <button
                v-if="!plan.approvalRequired"
                class="base-btn base-btn--sm"
                :class="plan.showRequestForm ? 'base-btn--ghost' : 'base-btn--primary'"
                :disabled="plan.busy"
                @click="applyPlan"
              >{{ plan.busy && !plan.showRequestForm ? 'Applying…' : 'Apply now' }}</button>
            </template>
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
  font-size: 12px; font-weight: 700; padding: 1px 6px; border-radius: 3px;
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

/* Sort bar */
.dbu-sort-bar {
  display: flex; align-items: center; gap: 6px;
  padding: 6px 12px 8px; border-bottom: 1px solid var(--border);
}
.dbu-sort-label { font-size: 11px; color: var(--text-muted); }
.dbu-sort-btn {
  display: inline-flex; align-items: center; gap: 4px;
  background: transparent; border: 1px solid var(--border); border-radius: 999px;
  padding: 2px 8px; font-size: 11px; color: var(--text-muted);
  cursor: pointer; font-family: inherit; transition: all 0.12s;
}
.dbu-sort-btn:hover { color: var(--text-primary); border-color: var(--text-secondary); }
.dbu-sort-btn.sorted { color: var(--brand); border-color: var(--brand); }

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
  font-size: 12px; font-weight: 600; padding: 2px 6px; border-radius: 999px;
  border: 1px solid;
}
.dbu-badge--super { color: var(--warning); border-color: var(--warning); }
.dbu-badge--createdb { color: var(--info); border-color: var(--info); }

/* Icons */
.dbu-icon-btn {
  width: 26px; height: 26px; display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); border-radius: 5px; background: transparent;
  color: var(--text-muted); cursor: pointer; transition: all 0.12s;
}
.dbu-icon-btn:hover { background: rgba(255,255,255,0.08); color: var(--text-primary); }
.dbu-icon-btn--danger:hover { background: var(--danger-bg); border-color: var(--danger); color: var(--danger); }

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
  font-size: 11px; color: var(--warning, var(--warning));
  background: var(--warning-bg); border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
  border-radius: 999px; padding: 2px 8px;
}
.dbu-grants-empty {
  padding: 32px; text-align: center; font-size: 13px; color: var(--text-muted);
}

/* Grant sections */
.dbu-grant-sections { padding: 16px; display: flex; flex-direction: column; gap: 20px; }
.dbu-grant-section-title {
  font-size: 12px; font-weight: 700; color: var(--text-muted); margin-bottom: 8px;
}
.dbu-grant-table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: 6px; }
.dbu-grants-panel > .dbu-grant-table-wrap { margin: 0 16px 16px; }
.dbu-grant-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.dbu-grant-table th {
  padding: 8px 10px; background: rgba(255,255,255,0.02);
  border-bottom: 1px solid var(--border);
  font-size: 12px; font-weight: 600; color: var(--text-muted); text-align: left;
}
.dbu-th-priv { text-align: center; min-width: 58px; }
.dbu-th-priv-label { display: block; font-size: 12px; font-weight: 700; }
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
  font-size: 12px; font-weight: 600; color: var(--text-muted);
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
.dbu-pw-fill--weak { background: var(--danger, var(--danger)); }
.dbu-pw-fill--ok { background: var(--warning); }
.dbu-pw-fill--strong { background: var(--success); }
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
  .dbu-panel--left { border-right: none; border-bottom: 1px solid var(--border); max-height: 320px; overflow: hidden; }
  .dbu-user-list { min-height: 0; }
}

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin 0.8s linear infinite; }
/* ── Grant access wizard / SQL review ───────────────────────────── */
.dbu-dialog--wide { width: min(640px, calc(100vw - 32px)); }
.dbu-field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.dbu-field-row > .dbu-field:only-child { grid-column: 1 / -1; }
.dbu-preset-list { display: grid; gap: 6px; }
.dbu-preset {
  display: grid; grid-template-columns: auto 1fr; column-gap: 8px; align-items: center;
  padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; cursor: pointer;
}
.dbu-preset input { grid-row: span 2; margin: 0; accent-color: var(--accent); }
.dbu-preset--on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 8%, transparent); }
.dbu-preset-name { font-weight: 600; font-size: 13px; color: var(--text); }
.dbu-preset-desc { font-size: 12px; color: var(--text-muted); }
.dbu-check { display: flex; align-items: flex-start; gap: 8px; font-size: 13px; color: var(--text); cursor: pointer; }
.dbu-check input { margin-top: 2px; accent-color: var(--accent); }
.dbu-indent { margin-left: 24px; }
.dbu-link-btn {
  background: none; border: none; padding: 0; font: inherit; font-size: 12px;
  color: var(--accent); cursor: pointer; text-decoration: underline; text-underline-offset: 2px;
}
.dbu-link-btn--danger { color: var(--danger); }
.dbu-plan-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 12.5px; color: var(--text-muted); }
.dbu-sql {
  margin: 0; padding: 12px; max-height: 320px; overflow: auto;
  background: var(--canvas); border: 1px solid var(--border); border-radius: 8px;
  font-family: 'IBM Plex Mono', ui-monospace, monospace; font-size: 12px; line-height: 1.6;
  color: var(--text); white-space: pre-wrap; word-break: break-word;
}
.dbu-request-form { display: grid; gap: 10px; padding-top: 4px; }
.dbu-error {
  padding: 8px 10px; border-radius: 8px; font-size: 12.5px;
  color: var(--danger); background: color-mix(in srgb, var(--danger) 10%, transparent);
}
.dbu-dialog-note--warn { color: var(--warning, #b7791f); }
.dbu-help { font-size: 12.5px; color: var(--text-muted); line-height: 1.5; padding: 12px 16px; }
.dbu-tab-count {
  font-size: 11px; padding: 0 6px; border-radius: 999px;
  background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent);
}
.dbu-summary { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 8px; padding: 16px 16px 4px; }
.dbu-summary-card { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; display: grid; gap: 4px; }
.dbu-summary-scope { font-family: 'IBM Plex Mono', ui-monospace, monospace; font-size: 12.5px; font-weight: 600; color: var(--text); }
.dbu-summary-line { font-size: 12px; color: var(--text); }
.dbu-summary-actions { display: flex; gap: 12px; margin-top: 2px; }
@media (max-width: 640px) {
  .dbu-field-row { grid-template-columns: 1fr; }
}
</style>
