<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useListFilter } from '@/composables/useListFilter'

interface DockerHost {
  id: number
  name: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  socket_path: string
  owner_id: number
  created_at: string
}

interface HostSummary {
  host_id: number
  name: string
  ssh_host: string
  reachable: boolean
  running: number
  total: number
  images: number
  version: string
  error?: string
}

interface DaemonInfo {
  version?: string
  api_version?: string
  os?: string
  arch?: string
}

interface HostForm {
  mode: 'local' | 'remote'
  name: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  ssh_password: string
  ssh_key: string
  socket_path: string
}

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['docker.manage']))

const hosts = ref<DockerHost[]>([])
const { search, filtered: filteredHosts } = useListFilter(hosts, (h, q) => h.name.toLowerCase().includes(q) || h.ssh_host.toLowerCase().includes(q))
const summaryMap = ref<Map<number, HostSummary>>(new Map())
const loading = ref(false)
const overviewLoading = ref(false)

// Ping status per host during a test-from-card action
const pingingId = ref<number | null>(null)

// Form state
const showHostForm = ref(false)
const editingHostId = ref<number | null>(null)
const testing = ref(false)
const savingHost = ref(false)
const form = ref<HostForm>(emptyForm())

function emptyForm(): HostForm {
  return {
    mode: 'remote',
    name: '',
    ssh_host: '',
    ssh_port: 22,
    ssh_user: '',
    ssh_password: '',
    ssh_key: '',
    socket_path: '/var/run/docker.sock',
  }
}

function hostPayload() {
  const f = form.value
  if (f.mode === 'local') {
    return { name: f.name, ssh_host: '', ssh_user: '', ssh_password: '', ssh_key: '', socket_path: f.socket_path }
  }
  return {
    name: f.name,
    ssh_host: f.ssh_host,
    ssh_port: f.ssh_port,
    ssh_user: f.ssh_user,
    ssh_password: f.ssh_password,
    ssh_key: f.ssh_key,
    socket_path: f.socket_path,
  }
}

function summaryFor(h: DockerHost): HostSummary | undefined {
  return summaryMap.value.get(h.id)
}

async function loadHosts() {
  loading.value = true
  try {
    const { data } = await axios.get<DockerHost[]>('/api/docker/hosts')
    hosts.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load Docker hosts')
  } finally {
    loading.value = false
  }
}

async function loadOverview() {
  overviewLoading.value = true
  try {
    const { data } = await axios.get<HostSummary[]>('/api/docker/overview')
    const m = new Map<number, HostSummary>()
    for (const s of data) m.set(s.host_id, s)
    summaryMap.value = m
  } catch {
    // overview is best-effort; don't show an error
  } finally {
    overviewLoading.value = false
  }
}

async function pingHost(h: DockerHost) {
  pingingId.value = h.id
  try {
    const { data } = await axios.get<DaemonInfo>(`/api/docker/hosts/${h.id}/ping`)
    toast.success(`Connected — Docker ${data.version ?? ''} (${data.os ?? ''}/${data.arch ?? ''})`)
    // Refresh overview to update the status badge
    await loadOverview()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Host unreachable')
    // Mark as unreachable in the map without a full reload
    const existing = summaryMap.value.get(h.id)
    summaryMap.value.set(h.id, {
      host_id: h.id,
      name: h.name,
      ssh_host: h.ssh_host,
      reachable: false,
      running: existing?.running ?? 0,
      total: existing?.total ?? 0,
      images: existing?.images ?? 0,
      version: existing?.version ?? '',
      error: e?.response?.data?.error || 'unreachable',
    })
  } finally {
    pingingId.value = null
  }
}

function openAddHost() {
  editingHostId.value = null
  form.value = emptyForm()
  showHostForm.value = true
}

function openEditHost(h: DockerHost) {
  editingHostId.value = h.id
  form.value = {
    mode: h.ssh_host ? 'remote' : 'local',
    name: h.name,
    ssh_host: h.ssh_host,
    ssh_port: h.ssh_port || 22,
    ssh_user: h.ssh_user,
    ssh_password: '',
    ssh_key: '',
    socket_path: h.socket_path || '/var/run/docker.sock',
  }
  showHostForm.value = true
}

async function testHost() {
  testing.value = true
  try {
    if (editingHostId.value !== null && form.value.mode === 'remote' && !form.value.ssh_password && !form.value.ssh_key) {
      const { data } = await axios.get<DaemonInfo>(`/api/docker/hosts/${editingHostId.value}/ping`)
      toast.success(`Connected — Docker ${data.version ?? ''} (${data.os ?? ''}/${data.arch ?? ''})`)
    } else {
      const { data } = await axios.post<DaemonInfo>('/api/docker/hosts/test', hostPayload())
      toast.success(`Connected — Docker ${data.version ?? ''} (${data.os ?? ''}/${data.arch ?? ''})`)
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Connection test failed')
  } finally {
    testing.value = false
  }
}

async function saveHost() {
  if (!form.value.name.trim()) {
    toast.error('Name is required')
    return
  }
  if (form.value.mode === 'remote') {
    if (!form.value.ssh_host.trim() || !form.value.ssh_user.trim()) {
      toast.error('SSH host and SSH user are required for a remote host')
      return
    }
    if (editingHostId.value === null && !form.value.ssh_password && !form.value.ssh_key) {
      toast.error('Provide an SSH password or private key')
      return
    }
  }
  savingHost.value = true
  try {
    if (editingHostId.value === null) {
      await axios.post<{ id: number }>('/api/docker/hosts', hostPayload())
      toast.success('Docker host added')
    } else {
      await axios.put(`/api/docker/hosts/${editingHostId.value}`, hostPayload())
      toast.success('Docker host updated')
    }
    showHostForm.value = false
    await loadHosts()
    await loadOverview()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save host')
  } finally {
    savingHost.value = false
  }
}

async function deleteHost(h: DockerHost) {
  const ok = await confirm(
    `Remove "${h.name}"? This only deletes the saved connection, not anything on the server.`,
    'Delete Docker host',
  )
  if (!ok) return
  try {
    await axios.delete(`/api/docker/hosts/${h.id}`)
    toast.success('Host removed')
    await loadHosts()
    summaryMap.value.delete(h.id)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete host')
  }
}

function manageHost(h: DockerHost) {
  router.push({ name: 'docker', query: { host: h.id } })
}

onMounted(async () => {
  await loadHosts()
  loadOverview()
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Docker Hosts</div>
            <div class="page-subtitle">
              Manage SSH connections to your servers — add, edit, and test hosts here, then open Docker to manage their containers.
            </div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="overviewLoading" @click="loadOverview">
              {{ overviewLoading ? 'Refreshing…' : 'Refresh status' }}
            </button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openAddHost">
              + Add host
            </button>
          </div>
        </section>

        <!-- Toolbar -->
        <div v-if="hosts.length" class="dkh-toolbar">
          <input v-model="search" class="base-input dkh-search" type="search" placeholder="Filter hosts…" />
        </div>

        <!-- Host cards -->
        <div v-if="loading" class="dkh-loading">Loading hosts…</div>

        <template v-else-if="hosts.length">
          <div v-if="filteredHosts.length" class="dkh-grid">
            <div v-for="h in filteredHosts" :key="h.id" class="dkh-card page-card">
              <div class="dkh-card-head">
                <div class="dkh-status-row">
                  <span
                    class="dkh-dot"
                    :class="summaryFor(h) === undefined ? 'dkh-dot--unknown' : summaryFor(h)!.reachable ? 'dkh-dot--ok' : 'dkh-dot--err'"
                    :title="summaryFor(h) === undefined ? 'Status unknown' : summaryFor(h)!.reachable ? 'Reachable' : (summaryFor(h)!.error || 'Unreachable')"
                  ></span>
                  <span class="dkh-name">{{ h.name }}</span>
                </div>
                <div class="dkh-meta">
                  <span v-if="h.ssh_host" class="dkh-ssh">{{ h.ssh_user ? h.ssh_user + '@' : '' }}{{ h.ssh_host }}{{ h.ssh_port && h.ssh_port !== 22 ? ':' + h.ssh_port : '' }}</span>
                  <span v-else class="dkh-local">local daemon</span>
                </div>
              </div>

              <div v-if="summaryFor(h)" class="dkh-stats">
                <template v-if="summaryFor(h)!.reachable">
                  <div class="dkh-stat">
                    <span class="dkh-stat-val">{{ summaryFor(h)!.running }}<span class="dkh-stat-total">/{{ summaryFor(h)!.total }}</span></span>
                    <span class="dkh-stat-label">containers</span>
                  </div>
                  <div class="dkh-stat">
                    <span class="dkh-stat-val">{{ summaryFor(h)!.images }}</span>
                    <span class="dkh-stat-label">images</span>
                  </div>
                  <div v-if="summaryFor(h)!.version" class="dkh-stat">
                    <span class="dkh-stat-val dkh-version">v{{ summaryFor(h)!.version }}</span>
                    <span class="dkh-stat-label">Docker</span>
                  </div>
                </template>
                <div v-else class="dkh-err">{{ summaryFor(h)!.error || 'Unreachable' }}</div>
              </div>
              <div v-else class="dkh-stats dkh-stats--pending">
                <span class="dkh-muted">{{ overviewLoading ? 'Checking status…' : 'Status unknown' }}</span>
              </div>

              <div class="dkh-actions">
                <button class="base-btn base-btn--primary base-btn--xs" @click="manageHost(h)">Manage →</button>
                <button
                  class="base-btn base-btn--xs"
                  :disabled="pingingId === h.id"
                  @click="pingHost(h)"
                >{{ pingingId === h.id ? 'Testing…' : 'Test' }}</button>
                <button v-if="canManage" class="base-btn base-btn--xs" @click="openEditHost(h)">Edit</button>
                <button v-if="canManage" class="base-btn base-btn--danger base-btn--xs" @click="deleteHost(h)">Delete</button>
              </div>
            </div>
          </div>
          <div v-else class="page-card dkh-empty">
            <p>No hosts match "{{ search }}".</p>
          </div>
        </template>

        <!-- Empty state -->
        <div v-else class="page-card dkh-empty">
          <div class="dkh-empty-icon">🐳</div>
          <h2>No Docker hosts yet</h2>
          <p>Connect a remote server by SSH to browse and control its containers.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="openAddHost">Add your first host</button>
        </div>
      </div>
    </div>

    <!-- Host form modal -->
    <div v-if="showHostForm" class="dk-modal-backdrop" @click.self="showHostForm = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">{{ editingHostId === null ? 'Add Docker host' : 'Edit Docker host' }}</div>
        <div class="dk-form">
          <div class="dk-mode">
            <button
              type="button"
              :class="['dk-mode-opt', { 'dk-mode-opt--active': form.mode === 'local' }]"
              @click="form.mode = 'local'"
            >This machine</button>
            <button
              type="button"
              :class="['dk-mode-opt', { 'dk-mode-opt--active': form.mode === 'remote' }]"
              @click="form.mode = 'remote'"
            >Remote host (SSH)</button>
          </div>

          <label>Name<input v-model="form.name" class="base-input" :placeholder="form.mode === 'local' ? 'local' : 'prod-01'" /></label>

          <p v-if="form.mode === 'local'" class="dk-hint">
            Connects directly to the Docker daemon running on this server — no SSH needed.
            Requires the Docker socket to be reachable by the Anveesa Nias process.
          </p>

          <template v-if="form.mode === 'remote'">
            <div class="dk-form-row">
              <label class="dk-grow">SSH host<input v-model="form.ssh_host" class="base-input" placeholder="10.0.0.5 or host.example.com" /></label>
              <label class="dk-port-field">Port<input v-model.number="form.ssh_port" class="base-input" type="number" /></label>
            </div>
            <label>SSH user<input v-model="form.ssh_user" class="base-input" placeholder="ubuntu" /></label>
            <label>
              SSH password
              <input v-model="form.ssh_password" class="base-input" type="password" :placeholder="editingHostId !== null ? '•••••• (unchanged)' : ''" />
            </label>
            <label>
              SSH private key (optional)
              <textarea v-model="form.ssh_key" class="base-input dk-textarea" rows="3" :placeholder="editingHostId !== null ? '(unchanged)' : '-----BEGIN OPENSSH PRIVATE KEY-----'"></textarea>
            </label>
          </template>

          <label>Docker socket path<input v-model="form.socket_path" class="base-input" /></label>
        </div>
        <div class="dk-modal-actions">
          <button class="base-btn base-btn--sm" :disabled="testing" @click="testHost">{{ testing ? 'Testing…' : 'Test connection' }}</button>
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showHostForm = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingHost" @click="saveHost">{{ savingHost ? 'Saving…' : 'Save' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Toolbar */
.dkh-toolbar { display: flex; justify-content: flex-end; }
.dkh-search { width: 240px; max-width: 100%; }

/* Host cards grid */
.dkh-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
}

.dkh-card {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* Card header */
.dkh-card-head { display: flex; flex-direction: column; gap: 4px; }

.dkh-status-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dkh-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dkh-dot--ok    { background: var(--success); }
.dkh-dot--err   { background: var(--danger); }
.dkh-dot--unknown { background: var(--text-muted); opacity: 0.5; }

.dkh-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.dkh-meta { display: flex; align-items: center; gap: 6px; padding-left: 16px; }
.dkh-ssh  { font-size: 12px; font-family: var(--mono); color: var(--text-muted); }
.dkh-local { font-size: 12px; color: var(--text-muted); font-style: italic; }

/* Stats row */
.dkh-stats {
  display: flex;
  gap: 16px;
  align-items: flex-end;
  min-height: 40px;
}
.dkh-stats--pending { align-items: center; }

.dkh-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.dkh-stat-val {
  font-size: 20px;
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1;
}
.dkh-stat-total {
  font-size: 13px;
  font-weight: 400;
  color: var(--text-muted);
}
.dkh-stat-label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.dkh-version {
  font-size: 14px;
  font-family: var(--mono);
  font-weight: 400;
  color: var(--text-secondary);
}

.dkh-err {
  font-size: 12px;
  color: var(--danger);
}
.dkh-muted { font-size: 12px; color: var(--text-muted); }

/* Action buttons */
.dkh-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  padding-top: 4px;
  border-top: 1px solid var(--border);
}

/* Loading / empty */
.dkh-loading {
  padding: 40px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}

.dkh-empty {
  padding: 60px 40px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}
.dkh-empty-icon { font-size: 48px; }
.dkh-empty h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.dkh-empty p  { font-size: 13px; color: var(--text-muted); margin: 0; max-width: 340px; }

/* Re-use DockerView modal styles */
.dk-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.dk-modal { padding: 22px; width: 440px; max-width: 92vw; }
.dk-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 14px; }
.dk-form { display: flex; flex-direction: column; gap: 10px; }
.dk-form label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-secondary); }
.dk-mode { display: flex; gap: 6px; background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r); padding: 3px; }
.dk-mode-opt { flex: 1; padding: 7px 10px; font-size: 12px; border: none; background: none; color: var(--text-muted); border-radius: var(--r-sm); cursor: pointer; transition: all var(--dur) var(--ease); }
.dk-mode-opt--active { background: var(--brand); color: var(--brand-fg); }
.dk-hint { font-size: 11px; line-height: 1.5; color: var(--text-muted); background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 8px 10px; margin: 0; }
.dk-form-row { display: flex; gap: 10px; }
.dk-grow { flex: 1; }
.dk-port-field { width: 90px; }
.dk-textarea { font-family: var(--mono); font-size: 11px; resize: vertical; }
.dk-modal-actions { display: flex; gap: 8px; margin-top: 16px; align-items: center; }
.dk-spacer { flex: 1; }
</style>
