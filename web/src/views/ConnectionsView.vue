<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import axios from 'axios'
import { useConnections, type DbDriver, type ConnectionForm } from '@/composables/useConnections'
import { useFolders } from '@/composables/useFolders'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const { connections, loading, testConnection, saveConnection, removeConnection, fetchConnections } = useConnections()
const { folders, fetchFolders, moveConnection, setConnectionVisibility } = useFolders()
const toast = useToast()
const { confirm } = useConfirm()

onMounted(() => fetchFolders())

const showForm = ref(false)
const editingId = ref<number | null>(null)
const testing = ref(false)
const saving = ref(false)
const testResult = ref<{ ok: boolean; message: string } | null>(null)

const defaultPorts: Record<DbDriver, number> = {
  postgres: 5432,
  mysql: 3306,
  mariadb: 3306,
  mssql: 1433,
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

const drivers: Array<{ key: DbDriver; label: string; badge: string; sub: string }> = [
  { key: 'postgres', label: 'PostgreSQL', badge: 'PG',  sub: 'v12+' },
  { key: 'mysql',    label: 'MySQL',      badge: 'MY',  sub: 'v8+' },
  { key: 'mariadb',  label: 'MariaDB',    badge: 'MB',  sub: 'v10+' },
  { key: 'mssql',    label: 'SQL Server', badge: 'MS',  sub: '2019+' },
]

function selectDriver(d: DbDriver) {
  form.driver = d
  form.port = defaultPorts[d]
  testResult.value = null
}

function resetForm() {
  editingId.value = null
  form.name = ''
  form.driver = 'postgres'
  form.host = 'localhost'
  form.port = 5432
  form.database = ''
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
  } catch (err) {
    toast.error('Failed to load connection')
  }
}

async function handleTest() {
  testing.value = true
  testResult.value = null
  testResult.value = await testConnection({ ...form })
  testing.value = false
}

async function handleSave() {
  if (!form.name.trim()) {
    toast.error('Connection name is required')
    return
  }
  
  saving.value = true
  let conn
  
  if (editingId.value) {
    // Update existing connection
    try {
      const { data } = await axios.put(`/api/connections/${editingId.value}`, form)
      conn = data
      await fetchConnections()
      toast.success(`Connection "${conn.name}" updated`)
    } catch (err) {
      toast.error('Failed to update connection')
    }
  } else {
    // Create new connection
    conn = await saveConnection({ ...form })
    if (conn) {
      toast.success(`Connection "${conn.name}" saved`)
    } else {
      toast.error('Failed to save connection')
    }
  }
  
  saving.value = false
  if (conn) {
    showForm.value = false
    resetForm()
  }
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
    }
    const driver = driverMap[scheme] ?? ('postgres' as DbDriver)
    form.driver = driver
    form.host = url.hostname || 'localhost'
    form.port = url.port ? parseInt(url.port) : defaultPorts[driver]
    form.database = url.pathname.replace(/^\//, '')
    form.username = decodeURIComponent(url.username || '')
    form.password = decodeURIComponent(url.password || '')
    form.ssl = url.searchParams.get('sslmode') === 'require' || url.searchParams.get('ssl') === 'true'
    if (!form.name) form.name = `${driver} / ${form.database}`
    showURLImport.value = false
    urlInput.value = ''
    testResult.value = null
  } catch {
    // ignore parse errors - let user fix the URL
  }
}

async function handleDelete(id: number, name: string) {
  const ok = await confirm(`Delete connection "${name}"? This cannot be undone.`, 'Delete Connection')
  if (!ok) return
  const success = await removeConnection(id)
  if (success) toast.success('Connection deleted')
  else toast.error('Failed to delete connection')
}
</script>

<template>
  <div class="page-shell conn-page">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Connections</div>
            <div class="page-subtitle">Add, organize, and validate the database endpoints your team works against every day.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--primary base-btn--sm" @click="showForm = !showForm">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              New Connection
            </button>
          </div>
        </section>

        <div class="conn-layout">
      <!-- Connection list -->
      <section class="page-panel conn-list">
        <div class="conn-list__head">
          <div>
            <div class="conn-list__title">Connection Library</div>
            <div class="conn-list__sub">{{ connections.length }} saved endpoints</div>
          </div>
        </div>
        <div v-if="loading" style="display:flex;align-items:center;gap:8px;color:var(--text-muted);font-size:13px;padding:20px">
          <svg class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          Loading connections…
        </div>

        <div v-else-if="connections.length === 0" class="empty-state" style="padding:40px 0">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
          No connections yet. Add your first one.
        </div>

        <div v-else style="display:flex;flex-direction:column;gap:8px">
          <div
            v-for="conn in connections"
            :key="conn.id"
            class="conn-row"
          >
            <div class="conn-badge" :class="`conn-badge--${conn.driver}`" style="width:36px;height:36px;border-radius:var(--r-sm);font-size:12px">
              {{ conn.driver === 'postgres' ? 'PG' : conn.driver === 'mysql' ? 'MY' : conn.driver === 'mariadb' ? 'MB' : 'MS' }}
            </div>
            <div style="flex:1;min-width:0">
              <div style="font-size:13px;font-weight:600;color:var(--text-primary);display:flex;align-items:center;gap:6px">
                {{ conn.name }}
                <span v-if="conn.visibility === 'private'" style="font-size:11px" title="Private">🔒</span>
                <span v-else style="font-size:11px" title="Shared">🌐</span>
              </div>
              <div style="font-size:11.5px;font-family:var(--mono);color:var(--text-muted);margin-top:2px">
                {{ conn.username ? `${conn.username}@` : '' }}{{ conn.host }}:{{ conn.port }}/{{ conn.database }}
              </div>
              <div v-if="conn.folder_id" style="font-size:10.5px;color:var(--text-muted);margin-top:2px">
                📁 {{ folders.find(f => f.id === conn.folder_id)?.name ?? 'Folder' }}
              </div>
            </div>
            <span class="badge badge--default">{{ conn.driver.toUpperCase() }}</span>
            <!-- Quick folder assign -->
            <select
              :value="conn.folder_id ?? ''"
              class="conn-folder-select"
              title="Move to folder"
              @change="moveConnection(conn.id, ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null)"
            >
              <option value="">📂 Unfiled</option>
              <option v-for="f in folders" :key="f.id" :value="f.id">{{ f.visibility === 'shared' ? '🌐' : '🔒' }} {{ f.name }}</option>
            </select>
            <!-- Visibility toggle -->
            <button class="icon-btn" :title="conn.visibility === 'shared' ? 'Make private' : 'Make shared'"
              @click="setConnectionVisibility(conn.id, conn.visibility === 'shared' ? 'private' : 'shared').then(() => fetchConnections())">
              {{ conn.visibility === 'shared' ? '🌐' : '🔒' }}
            </button>
            <button class="icon-btn" @click="editConnection(conn.id)" title="Edit">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
            </button>
            <button class="icon-btn danger" @click="handleDelete(conn.id, conn.name)" title="Delete">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/></svg>
            </button>
          </div>
        </div>
      </section>

      <!-- Add connection form -->
      <Transition name="modal">
        <div
          v-if="showForm"
          class="conn-form page-panel"
        >
          <div class="modal-hd">
            <span class="modal-title">{{ editingId ? 'Edit Connection' : 'New Connection' }}</span>
            <button class="icon-btn" @click="showForm = false; resetForm()">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>

          <div class="modal-bd">
            <!-- URL import -->
            <div class="form-group">
              <button class="base-btn base-btn--ghost base-btn--sm" style="width:100%;justify-content:center" @click="showURLImport = !showURLImport">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                Import from URL
              </button>
              <div v-if="showURLImport" style="margin-top:8px;display:flex;gap:6px">
                <input
                  v-model="urlInput"
                  class="base-input"
                  placeholder="postgres://user:pass@host:5432/dbname"
                  style="flex:1;font-family:var(--font-mono);font-size:11px"
                  @keydown.enter="parseConnectionURL(urlInput)"
                />
                <button class="base-btn base-btn--primary base-btn--sm" @click="parseConnectionURL(urlInput)">Parse</button>
              </div>
            </div>

            <!-- Driver selection -->
            <div class="form-group">
              <label class="form-label">Database Engine</label>
              <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:6px">
                <button
                  v-for="d in drivers"
                  :key="d.key"
                  class="provider-card"
                  :class="{ 'is-active': form.driver === d.key }"
                  style="padding:8px 10px"
                  @click="selectDriver(d.key)"
                >
                  <div class="provider-card__icon" :class="`provider-card__icon--${d.key}`" style="width:26px;height:26px;font-size:10px">{{ d.badge }}</div>
                  <div>
                    <span class="provider-card__name" style="font-size:11.5px">{{ d.label }}</span>
                    <span class="provider-card__sub">{{ d.sub }}</span>
                  </div>
                </button>
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">Connection Name</label>
              <input v-model="form.name" class="base-input" placeholder="My Database" />
            </div>

            <template>
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
                <input v-model="form.database" class="base-input" placeholder="mydb" />
              </div>

              <div class="form-row">
                <div class="form-group">
                  <label class="form-label">Username</label>
                  <input v-model="form.username" class="base-input" placeholder="postgres" />
                </div>
                <div class="form-group">
                  <label class="form-label">Password</label>
                  <input v-model="form.password" class="base-input" type="password" placeholder="••••••••" />
                </div>
              </div>

              <div style="display:flex;align-items:center;gap:8px">
                <input id="ssl" type="checkbox" v-model="form.ssl" style="accent-color:var(--brand)" />
                <label for="ssl" class="form-label" style="cursor:pointer;margin:0">Enable SSL/TLS</label>
              </div>
            </template>

            <!-- SSH Tunnel -->
            <details class="ssh-section">
              <summary class="ssh-summary">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="11" width="20" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
                SSH Tunnel (optional)
              </summary>
              <div class="ssh-fields">
                <div class="form-group">
                  <label class="form-label">SSH Host</label>
                  <input v-model="form.ssh_host" class="base-input" placeholder="bastion.example.com" />
                </div>
                <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
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
                  <label class="form-label">SSH Private Key <span class="form-hint" style="display:inline">(PEM, optional)</span></label>
                  <textarea v-model="form.ssh_key" class="base-input" rows="3" placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;..." style="font-family:monospace;font-size:11px;resize:vertical" />
                </div>
              </div>
            </details>

            <!-- Tags -->
            <div class="form-group">
              <label class="form-label">Tags <span class="form-hint" style="display:inline">(comma-separated, e.g. "Production, Analytics")</span></label>
              <input v-model="form.tags" class="base-input" placeholder="Production, Analytics" />
            </div>

            <!-- Folder & Visibility -->
            <div class="form-group">
              <label class="form-label">Folder</label>
              <select v-model="form.folder_id" class="base-input">
                <option :value="null">No folder (Unfiled)</option>
                <option v-for="f in folders" :key="f.id" :value="f.id">
                  {{ f.visibility === 'shared' ? '🌐' : '🔒' }} {{ f.name }}
                </option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">Visibility</label>
              <div style="display:flex;gap:8px">
                <label class="vis-radio" :class="{ active: form.visibility === 'shared' }">
                  <input type="radio" v-model="form.visibility" value="shared" style="display:none" />
                  🌐 Shared <span class="form-hint" style="display:inline">— visible to all users</span>
                </label>
                <label class="vis-radio" :class="{ active: form.visibility === 'private' }">
                  <input type="radio" v-model="form.visibility" value="private" style="display:none" />
                  🔒 Private <span class="form-hint" style="display:inline">— only you</span>
                </label>
              </div>
            </div>

            <!-- Test result -->
            <div v-if="testResult" class="notice" :class="testResult.ok ? 'notice--success' : 'notice--error'">
              <svg v-if="testResult.ok" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              {{ testResult.message }}
            </div>
          </div>

          <div class="modal-ft">
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="testing" @click="handleTest">
              <svg v-if="testing" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              {{ testing ? 'Testing…' : 'Test' }}
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="handleSave">
              <svg v-if="saving" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              {{ saving ? (editingId ? 'Updating…' : 'Saving…') : (editingId ? 'Update Connection' : 'Save Connection') }}
            </button>
          </div>
        </div>
      </Transition>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.conn-page {
  background: var(--bg-body);
}

.conn-layout {
  display: flex;
  gap: 20px;
  align-items: flex-start;
}

.conn-list {
  flex: 1;
  min-width: 0;
  padding: 18px;
}

.conn-list__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}

.conn-list__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.conn-list__sub {
  margin-top: 4px;
  font-size: 12px;
  color: var(--text-muted);
}

.conn-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.03), transparent 44%),
    var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 16px;
  transition: border-color var(--dur), transform var(--dur), box-shadow var(--dur);
}

.conn-row:hover {
  border-color: rgba(92, 184, 165, 0.22);
  transform: translateY(-1px);
  box-shadow: var(--shadow-sm);
}

.conn-form {
  width: 380px;
  flex-shrink: 0;
  overflow: hidden;
}

@media (max-width: 1040px) {
  .conn-layout {
    flex-direction: column;
  }

  .conn-form {
    width: 100%;
  }
}
</style>
