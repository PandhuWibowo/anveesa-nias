<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useConnections, type DbDriver, type ConnectionForm } from '@/composables/useConnections'
import { useFolders } from '@/composables/useFolders'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import DriverIcon from '@/components/ui/DriverIcon.vue'
import { readableError } from '@/utils/httpError'

const { connections, loading, error: connectionError, testConnection, saveConnection, removeConnection, fetchConnections, disconnectConnection, reconnectConnection } = useConnections()
const { folders, fetchFolders, moveConnection, setConnectionVisibility } = useFolders()
const toast = useToast()
const { confirm } = useConfirm()
const router = useRouter()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

onMounted(() => fetchFolders())

const showForm = ref(false)
const editingId = ref<number | null>(null)
const testing = ref(false)
const saving = ref(false)
const testResult = ref<{ ok: boolean; message: string } | null>(null)

const defaultPorts: Record<DbDriver, number> = {
  sqlite: 0,
  postgres: 5432,
  mysql: 3306,
  mariadb: 3306,
  mssql: 1433,
  redis: 6379,
  memcache: 11211,
  kafka: 9092,
  mongodb: 27017,
  elasticsearch: 9200,
  opensearch: 9200,
  s3_aws: 443,
  s3_gcp: 443,
  s3_oss: 443,
  s3_obs: 443,
}

const defaultHosts: Record<DbDriver, string> = {
  sqlite: 'localhost',
  postgres: 'localhost',
  mysql: 'localhost',
  mariadb: 'localhost',
  mssql: 'localhost',
  redis: 'localhost',
  memcache: 'localhost',
  kafka: 'localhost',
  mongodb: 'localhost',
  elasticsearch: 'localhost',
  opensearch: 'localhost',
  s3_aws: 's3.us-east-1.amazonaws.com',
  s3_gcp: 'storage.googleapis.com',
  s3_oss: 'oss-cn-hangzhou.aliyuncs.com',
  s3_obs: 'obs.ap-southeast-1.myhuaweicloud.com',
}

const form = reactive<ConnectionForm>({
  name: '',
  driver: 'postgres',
  host: 'localhost',
  port: 5432,
  database: '',
  username: '',
  password: '',
  ssl: false,
  tags: '',
  ssh_host: '',
  ssh_port: 22,
  ssh_user: '',
  ssh_password: '',
  ssh_key: '',
  folder_id: null,
  visibility: 'shared',
})

type DriverOption = { key: DbDriver; label: string; badge: string; sub: string }

const driverGroups: Array<{ key: string; label: string; options: DriverOption[] }> = [
  {
    key: 'rdbms',
    label: 'Relational',
    options: [
      { key: 'sqlite',   label: 'SQLite',     badge: 'SL',  sub: 'file' },
      { key: 'postgres', label: 'PostgreSQL', badge: 'PG',  sub: 'v12+' },
      { key: 'mysql',    label: 'MySQL',      badge: 'MY',  sub: 'v8+' },
      { key: 'mariadb',  label: 'MariaDB',    badge: 'MB',  sub: 'v10+' },
      { key: 'mssql',    label: 'SQL Server', badge: 'MS',  sub: '2019+' },
    ],
  },
  {
    key: 'cache',
    label: 'Cache',
    options: [
      { key: 'redis', label: 'Redis', badge: 'RD', sub: 'v6+' },
      { key: 'memcache', label: 'Memcache', badge: 'MC', sub: 'v1.6+' },
    ],
  },
  {
    key: 'streaming',
    label: 'Streaming',
    options: [
      { key: 'kafka', label: 'Kafka', badge: 'KF', sub: 'v2+' },
    ],
  },
  {
    key: 'document',
    label: 'Document Database',
    options: [
      { key: 'mongodb', label: 'MongoDB', badge: 'MG', sub: 'v6+' },
    ],
  },
  {
    key: 'search',
    label: 'Search & Observability',
    options: [
      { key: 'elasticsearch', label: 'Elasticsearch', badge: 'ES', sub: 'v8+' },
      { key: 'opensearch', label: 'OpenSearch', badge: 'OS', sub: 'v2+' },
    ],
  },
  {
    key: 'object-storage',
    label: 'Object Storage',
    options: [
      { key: 's3_aws', label: 'AWS S3',         badge: 'S3',  sub: 'bucket' },
      { key: 's3_gcp', label: 'GCP Storage',    badge: 'GCS', sub: 'S3 API' },
      { key: 's3_oss', label: 'Alibaba OSS',    badge: 'OSS', sub: 'bucket' },
      { key: 's3_obs', label: 'Huawei OBS',     badge: 'OBS', sub: 'bucket' },
    ],
  },
]

const defaultDatabases: Record<DbDriver, string> = {
  sqlite: 'nias.db',
  postgres: 'postgres',
  mysql: '',
  mariadb: '',
  mssql: '',
  redis: '0',
  memcache: '',
  kafka: '',
  mongodb: 'admin',
  elasticsearch: '',
  opensearch: '',
  s3_aws: '',
  s3_gcp: '',
  s3_oss: '',
  s3_obs: '',
}

const driverDescriptions: Record<DbDriver, string> = {
  sqlite:   'Serverless, file-based database — no server needed. Just point to a file path.',
  postgres: 'Feature-rich open-source RDBMS. Fill in host, port, database name, username, and password.',
  mysql:    "The world's most popular open-source relational database. Fill in host, port, database, username, and password.",
  mariadb:  'Community-maintained MySQL fork. Configuration is identical to MySQL.',
  mssql:    'Microsoft SQL Server. Uses host, port (1433), database, and Windows or SQL auth credentials.',
  redis:    'In-memory key-value store. Only host, port, and an optional password are required. The "database" is a numeric index (0–15).',
  memcache: 'Distributed memory caching — no authentication. Just a host and port.',
  kafka:    'Distributed event streaming. Provide a broker host and port. SASL username/password are optional.',
  mongodb:  'Document database. Use host/port or paste a mongodb:// / mongodb+srv:// URI in the host field.',
  elasticsearch: 'Search and observability datastore. Provide the HTTP endpoint, optional credentials, and an optional default index.',
  opensearch:    'OpenSearch-compatible search cluster. Provide the HTTP endpoint, optional credentials, and an optional default index.',
  s3_aws:   'Amazon S3 object storage. Provide your bucket name, endpoint, Access Key ID, and Secret Access Key.',
  s3_gcp:   'Google Cloud Storage via the S3-compatible API. Endpoint, bucket, and service-account credentials are required.',
  s3_oss:   'Alibaba Cloud OSS via the S3-compatible API. Provide your OSS endpoint, bucket, and access credentials.',
  s3_obs:   'Huawei Cloud OBS via the S3-compatible API. Provide your OBS endpoint, bucket, and access credentials.',
}

// ── Computed driver flags ─────────────────────────────────────────
const isSQLite    = computed(() => form.driver === 'sqlite')
const isRedis     = computed(() => form.driver === 'redis')
const isMemcache  = computed(() => form.driver === 'memcache')
const isKafka     = computed(() => form.driver === 'kafka')
const isMongoDB   = computed(() => form.driver === 'mongodb')
const isSearch    = computed(() => form.driver === 'elasticsearch' || form.driver === 'opensearch')
const isS3        = computed(() => isObjectStorageDriver(form.driver))
const isRDBMS     = computed(() => ['postgres', 'mysql', 'mariadb', 'mssql'].includes(form.driver))

function isObjectStorageDriver(driver: DbDriver) {
  return driver === 's3_aws' || driver === 's3_gcp' || driver === 's3_oss' || driver === 's3_obs'
}

function driverCategory(driver: DbDriver): string {
  if (['postgres', 'mysql', 'mariadb', 'mssql', 'sqlite'].includes(driver)) return 'rdbms'
  if (driver === 'redis' || driver === 'memcache') return 'cache'
  if (driver === 'kafka') return 'streaming'
  if (driver === 'mongodb') return 'document'
  if (driver === 'elasticsearch' || driver === 'opensearch') return 'search'
  return 's3'
}

function categoryLabel(driver: DbDriver): string {
  const cat = driverCategory(driver)
  return { rdbms: 'Relational DB', cache: 'Cache', streaming: 'Streaming', document: 'Document DB', search: 'Search', s3: 'Object Storage' }[cat] ?? cat
}

function driverBadge(driver: DbDriver) {
  return ({ sqlite: 'SL', postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS', redis: 'RD', memcache: 'MC', kafka: 'KF', mongodb: 'MG', elasticsearch: 'ES', opensearch: 'OS', s3_aws: 'S3', s3_gcp: 'GCS', s3_oss: 'OSS', s3_obs: 'OBS' } as Record<DbDriver, string>)[driver] ?? driver.slice(0, 2).toUpperCase()
}

function driverFullName(driver: DbDriver) {
  return ({ sqlite: 'SQLite', postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', mssql: 'SQL Server', redis: 'Redis', memcache: 'Memcache', kafka: 'Kafka', mongodb: 'MongoDB', elasticsearch: 'Elasticsearch', opensearch: 'OpenSearch', s3_aws: 'AWS S3', s3_gcp: 'GCP Storage', s3_oss: 'Alibaba OSS', s3_obs: 'Huawei OBS' } as Record<DbDriver, string>)[driver] ?? driver
}

function connDetailLine(conn: { driver: DbDriver; host: string; port: number; database: string; username: string }): string {
  if (conn.driver === 'sqlite') return conn.database || '(no file path set)'
  if (isObjectStorageDriver(conn.driver)) {
    const bucket = conn.database || '(no bucket)'
    return `${bucket}  ·  ${conn.host}`
  }
  if (conn.driver === 'memcache') {
    return `${conn.host}${conn.port ? ':' + conn.port : ''}`
  }
  if (conn.driver === 'kafka') {
    const sasl = conn.username ? `  ·  SASL: ${conn.username}` : ''
    return `${conn.host}${conn.port ? ':' + conn.port : ''}${sasl}`
  }
  if (conn.driver === 'elasticsearch' || conn.driver === 'opensearch') {
    const index = conn.database ? `  ·  ${conn.database}` : ''
    const auth = conn.username ? `  ·  ${conn.username}` : ''
    return `${conn.host}${conn.port ? ':' + conn.port : ''}${index}${auth}`
  }
  if (conn.driver === 'mongodb') {
    const db = conn.database ? `  ·  ${conn.database}` : ''
    const auth = conn.username ? `  ·  ${conn.username}` : ''
    return `${conn.host}${conn.port ? ':' + conn.port : ''}${db}${auth}`
  }
  if (conn.driver === 'redis') {
    const db = conn.database ? `  ·  db ${conn.database}` : '  ·  db 0'
    return `${conn.host}${conn.port ? ':' + conn.port : ''}${db}`
  }
  // RDBMS
  const user = conn.username ? `${conn.username}@` : ''
  const db = conn.database ? `  ·  ${conn.database}` : ''
  return `${user}${conn.host}${conn.port ? ':' + conn.port : ''}${db}`
}

function openConnectionLabel(driver: DbDriver): string {
  if (driver === 'redis') return 'Browse'
  if (driver === 'memcache') return 'Browse'
  if (driver === 'kafka') return 'Browse'
  if (driver === 'mongodb') return 'Manage'
  if (driver === 'elasticsearch' || driver === 'opensearch') return 'Browse'
  if (isObjectStorageDriver(driver)) return 'Browse'
  return 'Open'
}

function selectDriver(d: DbDriver) {
  const previousDefaultHost = defaultHosts[form.driver]
  form.driver = d
  form.port = defaultPorts[d]
  form.ssl = isObjectStorageDriver(d) ? true : form.ssl
  if (!form.host || form.host === previousDefaultHost || form.host === 'localhost') {
    form.host = defaultHosts[d]
  }
  if (!form.database || Object.values(defaultDatabases).includes(form.database)) {
    form.database = defaultDatabases[d]
  }
  testResult.value = null
}

function resetForm() {
  editingId.value = null
  form.name = ''
  form.driver = 'postgres'
  form.host = 'localhost'
  form.port = 5432
  form.database = 'postgres'
  form.username = ''
  form.password = ''
  form.ssl = false
  form.tags = ''
  form.ssh_host = ''
  form.ssh_port = 22
  form.ssh_user = ''
  form.ssh_password = ''
  form.ssh_key = ''
  form.folder_id = null
  form.visibility = 'shared'
  testResult.value = null
}

async function editConnection(id: number) {
  try {
    const { data: conn } = await axios.get(`/api/connections/${id}`)
    editingId.value = id
    form.name = conn.name
    form.driver = conn.driver
    form.host = conn.host || 'localhost'
    form.port = conn.port || defaultPorts[conn.driver as DbDriver]
    form.database = conn.database
    form.username = conn.username || ''
    form.password = conn.password || ''
    form.ssl = conn.ssl || false
    form.tags = conn.tags || ''
    form.ssh_host = conn.ssh_host || ''
    form.ssh_port = conn.ssh_port || 22
    form.ssh_user = conn.ssh_user || ''
    form.ssh_password = conn.ssh_password || ''
    form.ssh_key = conn.ssh_key || ''
    form.folder_id = conn.folder_id
    form.visibility = conn.visibility || 'shared'
    testResult.value = null
    showForm.value = true
  } catch {
    toast.error('Failed to load connection')
  }
}

function validateForm(): string | null {
  if (!form.name.trim()) return 'Connection name is required'
  if (isObjectStorageDriver(form.driver)) {
    if (!form.host.trim()) return 'Endpoint host is required'
    if (!form.database.trim()) return 'Bucket name is required'
    if (!form.username.trim()) return 'Access key is required'
    if (!form.password.trim()) return 'Secret key is required'
  }
  if ((form.driver === 'elasticsearch' || form.driver === 'opensearch') && !form.host.trim()) {
    return 'Search endpoint host is required'
  }
  if (form.driver === 'mongodb' && !form.host.trim()) {
    return 'MongoDB host or URI is required'
  }
  const needsDb = ['sqlite', 'postgres', 'mysql', 'mariadb', 'mssql']
  if (needsDb.includes(form.driver) && !form.database.trim()) {
    return form.driver === 'sqlite' ? 'SQLite file path is required' : 'Database name is required'
  }
  return null
}

async function handleTest() {
  const err = validateForm()
  if (err) { toast.error(err); return }
  testing.value = true
  testResult.value = null
  testResult.value = await testConnection({ ...form })
  testing.value = false
}

async function handleSave() {
  const err = validateForm()
  if (err) { toast.error(err); return }
  saving.value = true
  let conn
  if (editingId.value) {
    try {
      const { data } = await axios.put(`/api/connections/${editingId.value}`, form)
      conn = data
      await fetchConnections()
      toast.success(`Connection "${conn.name}" updated`)
    } catch (e) {
      toast.error(readableError(e, { action: 'Update connection', fallback: 'Failed to update connection' }))
    }
  } else {
    conn = await saveConnection({ ...form })
    if (conn) toast.success(`Connection "${conn.name}" saved`)
    else toast.error(connectionError.value || 'Failed to save connection')
  }
  saving.value = false
  if (conn) { showForm.value = false; resetForm() }
}

// ── URL import ────────────────────────────────────────────────────
const urlInput = ref('')
const showURLImport = ref(false)

function parseConnectionURL(raw: string) {
  try {
    const url = new URL(raw.trim())
    const scheme = url.protocol.replace(':', '')
    const driverMap: Record<string, DbDriver> = {
      postgres: 'postgres', postgresql: 'postgres',
      mysql: 'mysql', mariadb: 'mariadb',
      mssql: 'mssql', sqlserver: 'mssql',
      redis: 'redis', rediss: 'redis',
      memcache: 'memcache', memcached: 'memcache',
      kafka: 'kafka',
      mongodb: 'mongodb', 'mongodb+srv': 'mongodb',
      elasticsearch: 'elasticsearch', elastic: 'elasticsearch', es: 'elasticsearch',
      opensearch: 'opensearch', os: 'opensearch',
      s3: 's3_aws', s3a: 's3_aws',
      gcs: 's3_gcp', gs: 's3_gcp',
      oss: 's3_oss',
      obs: 's3_obs',
    }
    const driver = driverMap[scheme] ?? ('postgres' as DbDriver)
    form.driver = driver
    form.host = driver === 'mongodb' && (scheme === 'mongodb' || scheme === 'mongodb+srv') ? raw.trim() : (url.hostname || 'localhost')
    form.port = url.port ? parseInt(url.port) : defaultPorts[driver]
    form.database = url.pathname.replace(/^\//, '')
    form.username = decodeURIComponent(url.username || '')
    form.password = decodeURIComponent(url.password || '')
    form.ssl = scheme === 'rediss' || scheme === 'https' || scheme === 'mongodb+srv' || url.searchParams.get('sslmode') === 'require' || url.searchParams.get('ssl') === 'true' || url.searchParams.get('tls') === 'true'
    if (!form.name) {
      form.name = driver === 'kafka' || driver === 'memcache' || driver === 'mongodb' || driver === 'elasticsearch' || driver === 'opensearch'
        ? `${driver} / ${form.host}`
        : isObjectStorageDriver(driver)
          ? `${driverFullName(driver)} / ${form.database || form.host}`
          : `${driver} / ${form.database}`
    }
    showURLImport.value = false
    urlInput.value = ''
    testResult.value = null
  } catch {
    // ignore parse errors
  }
}

function openConnection(id: number, driver: DbDriver) {
  emit('set-conn', id)
  router.push({ name: driver === 'redis' ? 'redis' : driver === 'memcache' ? 'memcache' : driver === 'kafka' ? 'kafka' : driver === 'mongodb' ? 'mongodb' : driver === 'elasticsearch' || driver === 'opensearch' ? 'search' : isObjectStorageDriver(driver) ? 'connections' : 'data' })
}

async function handleDelete(id: number, name: string) {
  const ok = await confirm(`Delete connection "${name}"? This cannot be undone.`, 'Delete Connection')
  if (!ok) return
  const success = await removeConnection(id)
  if (success) toast.success('Connection deleted')
  else toast.error('Failed to delete connection')
}

async function handleDisconnect(id: number, name: string) {
  const success = await disconnectConnection(id)
  if (success) toast.success(`"${name}" disconnected`)
  else toast.error('Failed to disconnect')
}

async function handleReconnect(id: number, name: string) {
  const success = await reconnectConnection(id)
  if (success) toast.success(`"${name}" reconnected`)
  else toast.error('Failed to reconnect')
}
</script>

<template>
  <div class="page-shell conn-page">
    <div class="page-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Connections</div>
            <div class="page-subtitle">Add, organize, and test your database and service endpoints.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--primary base-btn--sm" @click="resetForm(); showForm = true">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              New Connection
            </button>
          </div>
        </section>

        <!-- Connection List -->
        <section class="page-panel conn-panel">
          <div class="conn-panel__head">
            <div>
              <div class="conn-panel__title">Saved Connections</div>
              <div class="conn-panel__sub">{{ connections.length }} endpoint{{ connections.length !== 1 ? 's' : '' }}</div>
            </div>
          </div>

          <!-- Loading -->
          <div v-if="loading" class="conn-loading">
            <svg class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            Loading connections…
          </div>

          <!-- Empty state -->
          <div v-else-if="connections.length === 0" class="conn-empty">
            <div class="conn-empty__icon">
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
            </div>
            <div class="conn-empty__title">No connections yet</div>
            <div class="conn-empty__sub">Add your first connection to get started. Supports PostgreSQL, MySQL, Redis, Kafka, S3 and more.</div>
            <button class="base-btn base-btn--primary base-btn--sm" style="margin-top:4px" @click="resetForm(); showForm = true">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              Add First Connection
            </button>
          </div>

          <!-- Connection cards -->
          <div v-else class="conn-items">
            <div
              v-for="conn in connections"
              :key="conn.id"
              class="conn-card"
              :class="[`conn-card--${driverCategory(conn.driver)}`, { 'conn-card--disconnected': conn.disconnected }]"
            >
              <!-- Driver badge -->
              <div class="conn-card__badge conn-badge" :class="`conn-badge--${conn.driver}`">
                <DriverIcon :driver="conn.driver" :size="22" />
              </div>

              <!-- Main info -->
              <div class="conn-card__body">
                <div class="conn-card__title-row">
                  <span class="conn-card__name">{{ conn.name }}</span>
                  <span class="conn-card__driver-tag">{{ driverFullName(conn.driver) }}</span>
                  <span class="conn-card__category-tag">{{ categoryLabel(conn.driver) }}</span>
                  <span class="conn-card__vis" :title="conn.visibility === 'shared' ? 'Shared' : 'Private'">
                    {{ conn.visibility === 'shared' ? '🌐' : '🔒' }}
                  </span>
                </div>
                <div class="conn-card__detail">{{ connDetailLine(conn) }}</div>
                <div class="conn-card__meta-row">
                  <span v-if="conn.folder_id" class="conn-card__folder">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                    {{ folders.find(f => f.id === conn.folder_id)?.name ?? 'Folder' }}
                  </span>
                  <span v-if="conn.tags" class="conn-card__tags">
                    <span v-for="tag in conn.tags.split(',').filter((t: string) => t.trim())" :key="tag" class="conn-tag">{{ tag.trim() }}</span>
                  </span>
                  <span v-if="conn.ssl" class="conn-card__ssl-badge">SSL</span>
                  <span v-if="conn.ssh_host" class="conn-card__ssh-badge">SSH</span>
                </div>
              </div>

              <!-- Status + actions -->
              <div class="conn-card__right">
                <div class="conn-card__status">
                  <span v-if="conn.disconnected" class="conn-status conn-status--off">Disconnected</span>
                  <span v-else class="conn-status conn-status--on">Connected</span>
                </div>
                <div class="conn-card__actions">
                  <button
                    class="base-btn base-btn--sm"
                    :class="conn.disconnected ? 'base-btn--ghost' : 'base-btn--primary'"
                    :disabled="conn.disconnected"
                    :title="conn.disconnected ? 'Reconnect first to open' : `Open in ${driverFullName(conn.driver)} view`"
                    @click="openConnection(conn.id, conn.driver)"
                  >
                    {{ conn.disconnected ? 'Offline' : openConnectionLabel(conn.driver) }}
                  </button>

                  <select
                    :value="conn.folder_id ?? ''"
                    class="conn-card__folder-sel"
                    title="Move to folder"
                    @change="moveConnection(conn.id, ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null)"
                  >
                    <option value="">📂 Unfiled</option>
                    <option v-for="f in folders" :key="f.id" :value="f.id">{{ f.name }}</option>
                  </select>

                  <button class="icon-btn" title="Edit connection" @click="editConnection(conn.id)">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                  </button>

                  <button
                    v-if="!conn.disconnected"
                    class="icon-btn"
                    title="Disconnect"
                    @click="handleDisconnect(conn.id, conn.name)"
                  >
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
                  </button>
                  <button
                    v-else
                    class="icon-btn"
                    style="color: var(--success)"
                    title="Reconnect"
                    @click="handleReconnect(conn.id, conn.name)"
                  >
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                  </button>

                  <button class="icon-btn danger" title="Delete connection" @click="handleDelete(conn.id, conn.name)">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/></svg>
                  </button>
                </div>
              </div>
            </div>
          </div>
        </section>

      </div>
    </div>

    <!-- Form Modal -->
    <Teleport to="body">
      <Transition name="conn-modal">
        <div v-if="showForm" class="conn-modal-backdrop" @click.self="showForm = false; resetForm()">
          <div class="conn-modal-shell">

            <!-- Header -->
            <div class="conn-modal-head">
              <div class="conn-modal-head__left">
                <span class="conn-modal-title">{{ editingId ? 'Edit Connection' : 'New Connection' }}</span>
                <span v-if="editingId" class="conn-modal-subtitle">Modifying an existing endpoint</span>
                <span v-else class="conn-modal-subtitle">Configure your new endpoint</span>
              </div>
              <button class="icon-btn" @click="showForm = false; resetForm()">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>

            <!-- Body -->
            <div class="conn-modal-body">

              <!-- URL import -->
              <div class="form-group">
                <button class="base-btn base-btn--ghost base-btn--sm url-import-btn" @click="showURLImport = !showURLImport">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                  Import from connection URL
                </button>
                <div v-if="showURLImport" class="url-import-row">
                  <input
                    v-model="urlInput"
                    class="base-input"
                    placeholder="postgres://user:pass@host:5432/dbname  ·  mongodb+srv://user:pass@cluster/db  ·  redis://host:6379"
                    style="flex:1;font-family:var(--mono);font-size:11px"
                    @keydown.enter="parseConnectionURL(urlInput)"
                  />
                  <button class="base-btn base-btn--primary base-btn--sm" @click="parseConnectionURL(urlInput)">Parse</button>
                </div>
              </div>

              <!-- Step 1: Choose provider -->
              <div class="form-section">
                <div class="form-section__label">
                  <span class="form-section__num">1</span>
                  Choose provider
                </div>
                <div class="provider-groups">
                  <div v-for="group in driverGroups" :key="group.key" class="provider-group">
                    <div class="provider-group__head">{{ group.label }}</div>
                    <div class="provider-grid">
                      <button
                        v-for="d in group.options"
                        :key="d.key"
                        class="provider-card"
                        :class="[`provider-card--${d.key}`, { 'is-active': form.driver === d.key }]"
                        @click="selectDriver(d.key)"
                      >
                        <div class="provider-card__icon" :class="`provider-card__icon--${d.key}`">
                          <DriverIcon :driver="d.key" :size="16" />
                        </div>
                        <div class="provider-card__body">
                          <span class="provider-card__name">{{ d.label }}</span>
                          <span class="provider-card__sub">{{ d.sub }}</span>
                        </div>
                        <svg v-if="form.driver === d.key" class="provider-card__check" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      </button>
                    </div>
                  </div>
                </div>

                <!-- Driver description hint -->
                <div class="driver-hint">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
                  <span>{{ driverDescriptions[form.driver] }}</span>
                </div>
              </div>

              <!-- Step 2: Connection details -->
              <div class="form-section">
                <div class="form-section__label">
                  <span class="form-section__num">2</span>
                  Connection details
                </div>

                <!-- Connection name + visibility (always shown) -->
                <div class="form-row">
                  <div class="form-group" style="flex:2">
                    <label class="form-label">Connection Name</label>
                    <input v-model="form.name" class="base-input" placeholder="e.g. Production DB, Local Redis, Dev S3" />
                  </div>
                  <div class="form-group" style="flex:1">
                    <label class="form-label">Visibility</label>
                    <div style="display:flex;gap:6px;height:32px">
                      <label class="vis-radio" :class="{ active: form.visibility === 'shared' }" style="flex:1;justify-content:center">
                        <input type="radio" v-model="form.visibility" value="shared" style="display:none" />
                        🌐 Shared
                      </label>
                      <label class="vis-radio" :class="{ active: form.visibility === 'private' }" style="flex:1;justify-content:center">
                        <input type="radio" v-model="form.visibility" value="private" style="display:none" />
                        🔒 Private
                      </label>
                    </div>
                  </div>
                </div>

                <!-- ── SQLite ─────────────────────────────────────────── -->
                <template v-if="isSQLite">
                  <div class="form-group">
                    <label class="form-label">Database File Path</label>
                    <input v-model="form.database" class="base-input" placeholder="/path/to/nias.db" />
                    <div class="form-hint">Absolute or relative path to the .db file. The file will be created if it doesn't exist.</div>
                  </div>
                </template>

                <!-- ── Object Storage (S3 / GCS / OSS / OBS) ──────────── -->
                <template v-else-if="isS3">
                  <div class="form-group">
                    <label class="form-label">Endpoint Host</label>
                    <input v-model="form.host" class="base-input" :placeholder="defaultHosts[form.driver]" />
                    <div class="form-hint">The S3-compatible endpoint URL (without https://).</div>
                  </div>
                  <div class="form-group">
                    <label class="form-label">Bucket</label>
                    <input v-model="form.database" class="base-input" placeholder="my-bucket-name" />
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">Access Key ID</label>
                      <input v-model="form.username" class="base-input" placeholder="AKIAIOSFODNN7EXAMPLE" autocomplete="off" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">Secret Access Key</label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="••••••••" />
                    </div>
                  </div>
                  <div class="field-note field-note--info">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
                    SSL/TLS is always enabled for object storage.
                  </div>
                </template>

                <!-- ── Memcache ─────────────────────────────────────────── -->
                <template v-else-if="isMemcache">
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Host</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="field-note">
                    Memcache does not support authentication. Only host and port are needed.
                  </div>
                </template>

                <!-- ── Redis ──────────────────────────────────────────────── -->
                <template v-else-if="isRedis">
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Host</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">
                        Password
                        <span class="field-optional">optional</span>
                      </label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="(leave blank if none)" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">
                        Database Index
                        <span class="field-optional">0 – 15</span>
                      </label>
                      <input v-model="form.database" class="base-input" placeholder="0" />
                    </div>
                  </div>
                  <div class="field-check-row">
                    <input id="redis-ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                    <label for="redis-ssl" class="field-check-label">Enable SSL/TLS <span class="field-optional">(uses rediss:// scheme)</span></label>
                  </div>
                </template>

                <!-- ── Kafka ──────────────────────────────────────────────── -->
                <template v-else-if="isKafka">
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Broker Host</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">
                        SASL Username
                        <span class="field-optional">optional</span>
                      </label>
                      <input v-model="form.username" class="base-input" placeholder="(leave blank if no auth)" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">
                        SASL Password
                        <span class="field-optional">optional</span>
                      </label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="(leave blank if no auth)" />
                    </div>
                  </div>
                  <div class="field-check-row">
                    <input id="kafka-ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                    <label for="kafka-ssl" class="field-check-label">Enable SSL/TLS</label>
                  </div>
                </template>

                <!-- ── MongoDB ───────────────────────────────────────────── -->
                <template v-else-if="isMongoDB">
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Host or URI</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost or mongodb+srv://cluster.example.net/app" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="form-label">Default Database</label>
                    <input v-model="form.database" class="base-input" placeholder="admin" />
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">
                        Username
                        <span class="field-optional">optional if URI includes auth</span>
                      </label>
                      <input v-model="form.username" class="base-input" placeholder="mongodb user" autocomplete="off" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">
                        Password
                        <span class="field-optional">optional if URI includes auth</span>
                      </label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="(leave blank if none)" />
                    </div>
                  </div>
                  <div class="field-check-row">
                    <input id="mongodb-ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                    <label for="mongodb-ssl" class="field-check-label">Enable TLS</label>
                  </div>
                </template>

                <!-- ── Elasticsearch / OpenSearch ──────────────────────── -->
                <template v-else-if="isSearch">
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Endpoint Host</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="form-label">
                      Default Index
                      <span class="field-optional">optional</span>
                    </label>
                    <input v-model="form.database" class="base-input" placeholder="logs-*, traces-*, metrics-*" />
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">
                        Username
                        <span class="field-optional">optional</span>
                      </label>
                      <input v-model="form.username" class="base-input" placeholder="elastic" autocomplete="off" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">
                        Password / API Key
                        <span class="field-optional">optional</span>
                      </label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="(leave blank if none)" />
                    </div>
                  </div>
                  <div class="field-check-row">
                    <input id="search-ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                    <label for="search-ssl" class="field-check-label">Enable SSL/TLS</label>
                  </div>
                </template>

                <!-- ── Relational DB (postgres / mysql / mariadb / mssql) ── -->
                <template v-else>
                  <div class="form-row">
                    <div class="form-group" style="flex:2">
                      <label class="form-label">Host</label>
                      <input v-model="form.host" class="base-input" placeholder="localhost" />
                    </div>
                    <div class="form-group" style="flex:1">
                      <label class="form-label">Port</label>
                      <input v-model.number="form.port" class="base-input" type="number" />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="form-label">Database</label>
                    <input v-model="form.database" class="base-input" :placeholder="defaultDatabases[form.driver] || 'database_name'" />
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">Username</label>
                      <input v-model="form.username" class="base-input" placeholder="postgres" autocomplete="off" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">Password</label>
                      <input v-model="form.password" class="base-input" type="password" placeholder="••••••••" />
                    </div>
                  </div>
                  <div class="field-check-row">
                    <input id="rdbms-ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                    <label for="rdbms-ssl" class="field-check-label">Enable SSL/TLS</label>
                  </div>
                </template>
              </div>

              <!-- Step 3: Advanced -->
              <details class="conn-advanced">
                <summary class="conn-advanced__trigger">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93A10 10 0 0 0 5 5.07M4.93 19.07A10 10 0 0 0 19 18.93M2 12h1m18 0h1M12 2v1m0 18v1M6.34 17.66l-.7.7m11.32-11.32.7.7M6.34 6.34l-.7-.7m11.32 11.32.7.7"/></svg>
                  Advanced options
                  <span class="conn-advanced__hint">SSH tunnel, tags, folder</span>
                </summary>
                <div class="conn-advanced__body">

                  <!-- SSH Tunnel -->
                  <div class="adv-section">
                    <div class="adv-section__label">SSH Tunnel <span class="field-optional">optional</span></div>
                    <div class="form-group">
                      <label class="form-label">SSH Host</label>
                      <input v-model="form.ssh_host" class="base-input" placeholder="bastion.example.com" />
                    </div>
                    <div class="form-row">
                      <div class="form-group">
                        <label class="form-label">SSH Port</label>
                        <input v-model.number="form.ssh_port" class="base-input" type="number" placeholder="22" />
                      </div>
                      <div class="form-group">
                        <label class="form-label">SSH User</label>
                        <input v-model="form.ssh_user" class="base-input" placeholder="ubuntu" />
                      </div>
                    </div>
                    <div class="form-group">
                      <label class="form-label">SSH Password</label>
                      <input v-model="form.ssh_password" class="base-input" type="password" placeholder="••••••••" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">SSH Private Key <span class="field-optional">PEM, optional</span></label>
                      <textarea v-model="form.ssh_key" class="base-input" rows="3" placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;..." style="font-family:monospace;font-size:11px;resize:vertical" />
                    </div>
                  </div>

                  <!-- Tags -->
                  <div class="form-group">
                    <label class="form-label">Tags <span class="field-optional">comma-separated</span></label>
                    <input v-model="form.tags" class="base-input" placeholder="Production, Analytics, Reporting" />
                  </div>

                  <!-- Folder -->
                  <div class="form-group">
                    <label class="form-label">Folder</label>
                    <select v-model="form.folder_id" class="base-input">
                      <option :value="null">No folder (Unfiled)</option>
                      <option v-for="f in folders" :key="f.id" :value="f.id">
                        {{ f.visibility === 'shared' ? '🌐' : '🔒' }} {{ f.name }}
                      </option>
                    </select>
                  </div>
                </div>
              </details>

              <!-- Test result -->
              <div v-if="testResult" class="notice" :class="testResult.ok ? 'notice--success' : 'notice--error'">
                <svg v-if="testResult.ok" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
                {{ testResult.message }}
              </div>

            </div>

            <!-- Footer -->
            <div class="conn-modal-foot">
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="testing" @click="handleTest">
                <svg v-if="testing" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                {{ testing ? 'Testing…' : 'Test Connection' }}
              </button>
              <div style="display:flex;gap:8px">
                <button class="base-btn base-btn--ghost base-btn--sm" @click="showForm = false; resetForm()">Cancel</button>
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="handleSave">
                  <svg v-if="saving" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  {{ saving ? (editingId ? 'Updating…' : 'Saving…') : (editingId ? 'Update Connection' : 'Save Connection') }}
                </button>
              </div>
            </div>

          </div>
        </div>
      </Transition>
    </Teleport>

  </div>
</template>

<style scoped>
.conn-page {
  background: var(--bg-body);
}

/* ── Connection panel ── */
.conn-panel {
  padding: 20px;
}

.conn-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.conn-panel__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.conn-panel__sub {
  margin-top: 2px;
  font-size: 12px;
  color: var(--text-muted);
}

.conn-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
  padding: 24px;
}

/* ── Empty state ── */
.conn-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 56px 24px;
  text-align: center;
}

.conn-empty__icon {
  width: 56px;
  height: 56px;
  border-radius: 16px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  margin-bottom: 4px;
}

.conn-empty__title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.conn-empty__sub {
  font-size: 13px;
  color: var(--text-muted);
  max-width: 380px;
  line-height: 1.6;
}

/* ── Connection cards ── */
.conn-items {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.conn-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-radius: 12px;
  border-left: 3px solid var(--border);
  transition: border-color 0.15s, box-shadow 0.15s;
}

.conn-card:hover {
  border-color: var(--brand);
  border-left-color: var(--brand);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--brand) 15%, transparent);
}

.conn-card--disconnected {
  opacity: 0.65;
}

/* Category-specific left border colors */
.conn-card--rdbms        { border-left-color: #336791; }
.conn-card--rdbms:hover  { border-left-color: #336791; }
.conn-card--cache        { border-left-color: #c6302b; }
.conn-card--cache:hover  { border-left-color: #c6302b; }
.conn-card--streaming    { border-left-color: #231f20; }
.conn-card--streaming:hover { border-left-color: #231f20; }
.conn-card--search       { border-left-color: #00bfb3; }
.conn-card--search:hover { border-left-color: #00bfb3; }
.conn-card--s3           { border-left-color: #f59e0b; }
.conn-card--s3:hover     { border-left-color: #f59e0b; }

.conn-card__badge {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 700;
  flex-shrink: 0;
}

.conn-card__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.conn-card__title-row {
  display: flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
}

.conn-card__name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

.conn-card__driver-tag {
  font-size: 10.5px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 1px 6px;
}

.conn-card__category-tag {
  font-size: 10px;
  font-weight: 600;
  color: var(--brand);
  background: var(--brand-dim);
  border-radius: 4px;
  padding: 1px 6px;
  letter-spacing: 0.02em;
}

.conn-card__vis {
  font-size: 11px;
  opacity: 0.6;
}

.conn-card__detail {
  font-size: 11.5px;
  font-family: var(--mono);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conn-card__meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 1px;
}

.conn-card__folder {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10.5px;
  color: var(--text-muted);
}

.conn-card__tags {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.conn-card__ssl-badge,
.conn-card__ssh-badge {
  font-size: 9.5px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 3px;
  letter-spacing: 0.04em;
}

.conn-card__ssl-badge {
  background: color-mix(in srgb, var(--success) 14%, transparent);
  color: var(--success);
}

.conn-card__ssh-badge {
  background: color-mix(in srgb, var(--warning) 14%, transparent);
  color: var(--warning);
}

/* Status + actions column */
.conn-card__right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
  flex-shrink: 0;
}

.conn-card__status {
  display: flex;
  align-items: center;
}

.conn-status {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 99px;
}

.conn-status::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.conn-status--on {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 12%, transparent);
}

.conn-status--on::before {
  background: var(--success);
}

.conn-status--off {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}

.conn-status--off::before {
  background: var(--danger);
}

.conn-card__actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.conn-card__folder-sel {
  height: 28px;
  padding: 0 6px;
  font-size: 11.5px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  max-width: 120px;
}

/* ── Connection badge colors ── */
.conn-badge {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
}

.conn-badge--postgres  { background: #336791; }
.conn-badge--mysql     { background: #e48e00; }
.conn-badge--mariadb   { background: #c0765a; }
.conn-badge--mssql     { background: #cc2927; }
.conn-badge--redis     { background: #c6302b; }
.conn-badge--memcache  { background: #16a34a; }
.conn-badge--kafka     { background: #231f20; }
.conn-badge--mongodb   { background: #00a35c; }
.conn-badge--elasticsearch { background: #00bfb3; }
.conn-badge--opensearch    { background: #005eb8; }
.conn-badge--sqlite    { background: #4b5563; }
.conn-badge--s3_aws    { background: #f59e0b; }
.conn-badge--s3_gcp    { background: #4285f4; }
.conn-badge--s3_oss    { background: #ff6a00; }
.conn-badge--s3_obs    { background: #c00000; }

/* ── Modal backdrop + shell ── */
.conn-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 500;
  padding: 24px;
}

.conn-modal-shell {
  background: var(--bg-elevated);
  border: 1px solid var(--border-2);
  border-radius: 18px;
  box-shadow: 0 24px 80px rgba(0, 0, 0, 0.5);
  width: 100%;
  max-width: 560px;
  max-height: calc(100dvh - 48px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.conn-modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.conn-modal-head__left {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.conn-modal-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.conn-modal-subtitle {
  font-size: 11.5px;
  color: var(--text-muted);
}

.conn-modal-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.conn-modal-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

/* ── Modal transitions ── */
.conn-modal-enter-active,
.conn-modal-leave-active {
  transition: opacity 0.18s var(--ease);
}

.conn-modal-enter-active .conn-modal-shell,
.conn-modal-leave-active .conn-modal-shell {
  transition: transform 0.18s var(--ease), opacity 0.18s var(--ease);
}

.conn-modal-enter-from,
.conn-modal-leave-to {
  opacity: 0;
}

.conn-modal-enter-from .conn-modal-shell,
.conn-modal-leave-to .conn-modal-shell {
  transform: scale(0.96) translateY(8px);
  opacity: 0;
}

/* ── URL import ── */
.url-import-btn {
  width: 100%;
  justify-content: center;
}

.url-import-row {
  margin-top: 8px;
  display: flex;
  gap: 6px;
}

/* ── Form sections ── */
.form-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-section__label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.form-section__num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--brand);
  color: var(--brand-fg);
  font-size: 10px;
  font-weight: 700;
  flex-shrink: 0;
}

/* ── Driver description hint ── */
.driver-hint {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  background: var(--brand-dim);
  border: 1px solid color-mix(in srgb, var(--brand) 20%, transparent);
  border-radius: 8px;
  font-size: 12px;
  color: var(--brand);
  line-height: 1.5;
}

.driver-hint svg {
  flex-shrink: 0;
  margin-top: 1px;
}

/* ── Field helpers ── */
.field-optional {
  font-size: 10.5px;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 4px;
}

.field-check-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.field-check-label {
  font-size: 12.5px;
  color: var(--text-secondary);
  cursor: pointer;
  user-select: none;
}

.field-note {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11.5px;
  color: var(--text-muted);
  padding: 8px 10px;
  background: var(--bg-body);
  border-radius: 7px;
  line-height: 1.4;
}

.field-note--info {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 8%, transparent);
}

/* ── Provider picker ── */
.provider-groups {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.provider-group {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.provider-group__head {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 5px;
  min-width: 0;
}

.provider-card {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 8px 10px;
  border: 1.5px solid var(--border-2);
  border-radius: 9px;
  background: var(--bg-surface);
  cursor: pointer;
  transition: border-color 0.13s, background 0.13s, box-shadow 0.13s;
  position: relative;
  text-align: left;
  width: 100%;
}

.provider-card:hover {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 8%, var(--bg-elevated));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--brand) 20%, transparent);
}

.provider-card.is-active {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, var(--bg-elevated));
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 18%, transparent);
}

.provider-card__icon {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 9px;
  font-weight: 700;
  flex-shrink: 0;
  color: #fff;
}

.provider-card__icon--postgres { background: #336791; }
.provider-card__icon--mysql    { background: #e48e00; }
.provider-card__icon--mariadb  { background: #c0765a; }
.provider-card__icon--mssql    { background: #cc2927; }
.provider-card__icon--redis    { background: #c6302b; }
.provider-card__icon--memcache { background: #16a34a; }
.provider-card__icon--kafka    { background: #231f20; }
.provider-card__icon--mongodb  { background: #00a35c; }
.provider-card__icon--elasticsearch { background: #00bfb3; }
.provider-card__icon--opensearch    { background: #005eb8; }
.provider-card__icon--sqlite   { background: #4b5563; }
.provider-card__icon--s3_aws   { background: #f59e0b; }
.provider-card__icon--s3_gcp   { background: #4285f4; }
.provider-card__icon--s3_oss   { background: #ff6a00; }
.provider-card__icon--s3_obs   { background: #c00000; }

.provider-card__body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 1;
  min-width: 0;
}

.provider-card__name {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.provider-card.is-active .provider-card__name {
  color: var(--brand);
}

.provider-card__sub {
  font-size: 10px;
  color: var(--text-muted);
}

.provider-card__check {
  color: var(--brand);
  flex-shrink: 0;
}

/* ── Advanced section ── */
.conn-advanced {
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
}

.conn-advanced__trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 14px;
  cursor: pointer;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-secondary);
  list-style: none;
  user-select: none;
  background: var(--bg-body);
  transition: background 0.12s;
}

.conn-advanced__trigger:hover {
  background: var(--bg-elevated);
}

.conn-advanced__hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 2px;
}

.conn-advanced__body {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  border-top: 1px solid var(--border);
}

.adv-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.adv-section__label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
}

/* ── Conn tag ── */
.conn-tag {
  font-size: 9px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--brand-dim);
  color: var(--brand);
  font-weight: 600;
  letter-spacing: 0.03em;
  text-transform: uppercase;
}

/* ── Responsive ── */
@media (max-width: 680px) {
  .conn-card {
    flex-wrap: wrap;
    gap: 10px;
  }

  .conn-card__right {
    width: 100%;
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }

  .conn-modal-backdrop {
    padding: 0;
    align-items: flex-end;
  }

  .conn-modal-shell {
    max-width: 100%;
    width: 100%;
    max-height: calc(100dvh - 16px);
    border-radius: 20px 20px 0 0;
  }

  .conn-modal-body {
    padding: 16px;
  }

  .conn-modal-foot {
    padding: 12px 16px calc(12px + env(safe-area-inset-bottom, 0px));
  }

  .provider-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 480px) {
  .conn-card__title-row {
    gap: 5px;
  }

  .conn-card__category-tag {
    display: none;
  }
}
</style>
