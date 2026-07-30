<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import RowActionsMenu, { type RowAction } from '@/components/ui/RowActionsMenu.vue'
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

interface DaemonInfo {
  version?: string
  api_version?: string
  os?: string
  arch?: string
}

interface HostForm {
  name: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  ssh_password: string
  ssh_key: string
  socket_path: string
}

// Ping status per host
type HostPingState = 'unknown' | 'ok' | 'err'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['sftp.manage']))

// Only remote SSH hosts can be used for SFTP
const allHosts = ref<DockerHost[]>([])
const hosts = computed(() => allHosts.value.filter((h) => h.ssh_host))
const { search, filtered: filteredHosts } = useListFilter(hosts, (h, q) =>
  h.name.toLowerCase().includes(q) || h.ssh_host.toLowerCase().includes(q),
)
const pingState = ref<Record<number, HostPingState>>({})
const pingingId = ref<number | null>(null)
const loading = ref(false)

// Form state
const showHostForm = ref(false)
const editingHostId = ref<number | null>(null)
const testing = ref(false)
const savingHost = ref(false)
const form = ref<HostForm>(emptyForm())

function emptyForm(): HostForm {
  return {
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

async function loadHosts() {
  loading.value = true
  try {
    const { data } = await axios.get<DockerHost[]>('/api/docker/hosts')
    allHosts.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load hosts')
  } finally {
    loading.value = false
  }
}

async function pingHost(h: DockerHost) {
  pingingId.value = h.id
  try {
    await axios.get<DaemonInfo>(`/api/docker/hosts/${h.id}/ping`)
    pingState.value = { ...pingState.value, [h.id]: 'ok' }
    toast.success(`SSH reachable — ${h.ssh_user ? h.ssh_user + '@' : ''}${h.ssh_host}`)
  } catch (e: any) {
    pingState.value = { ...pingState.value, [h.id]: 'err' }
    toast.error(e?.response?.data?.error || 'Host unreachable')
  } finally {
    pingingId.value = null
  }
}

async function pingAll() {
  for (const h of hosts.value) {
    void pingHost(h)
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
    if (editingHostId.value !== null && !form.value.ssh_password && !form.value.ssh_key) {
      const { data } = await axios.get<DaemonInfo>(`/api/docker/hosts/${editingHostId.value}/ping`)
      toast.success(`SSH reachable — Docker ${data.version ?? ''} (${data.os ?? ''}/${data.arch ?? ''})`)
    } else {
      const { data } = await axios.post<DaemonInfo>('/api/docker/hosts/test', hostPayload())
      toast.success(`SSH reachable — Docker ${data.version ?? ''} (${data.os ?? ''}/${data.arch ?? ''})`)
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
  if (!form.value.ssh_host.trim() || !form.value.ssh_user.trim()) {
    toast.error('SSH host and SSH user are required')
    return
  }
  if (editingHostId.value === null && !form.value.ssh_password && !form.value.ssh_key) {
    toast.error('Provide an SSH password or private key')
    return
  }
  savingHost.value = true
  try {
    if (editingHostId.value === null) {
      await axios.post<{ id: number }>('/api/docker/hosts', hostPayload())
      toast.success('SSH host added')
    } else {
      await axios.put(`/api/docker/hosts/${editingHostId.value}`, hostPayload())
      toast.success('SSH host updated')
    }
    showHostForm.value = false
    await loadHosts()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save host')
  } finally {
    savingHost.value = false
  }
}

async function deleteHost(h: DockerHost) {
  const ok = await confirm(
    `Remove "${h.name}"? This only deletes the saved SSH connection, not anything on the server.`,
    'Delete SSH host',
  )
  if (!ok) return
  try {
    await axios.delete(`/api/docker/hosts/${h.id}`)
    toast.success('Host removed')
    const newState = { ...pingState.value }
    delete newState[h.id]
    pingState.value = newState
    await loadHosts()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete host')
  }
}

function browseHost(h: DockerHost) {
  router.push({ name: 'sftp', query: { host: h.id } })
}

function cardActions(h: DockerHost): RowAction[] {
  const actions: RowAction[] = [
    { key: 'browse', label: 'Browse files', icon: 'arrow-right', primary: true, onClick: () => browseHost(h) },
    { key: 'check', label: pingingId.value === h.id ? 'Checking…' : 'Check', icon: 'check', primary: true, disabled: pingingId.value === h.id, onClick: () => pingHost(h) },
  ]
  if (canManage.value) {
    actions.push({ key: 'edit', label: 'Edit', icon: 'edit', onClick: () => openEditHost(h) })
    actions.push({ key: 'delete', label: 'Delete', icon: 'delete', danger: true, onClick: () => deleteHost(h) })
  }
  return actions
}

onMounted(async () => {
  await loadHosts()
  pingAll()
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
            <div class="page-title">SFTP Hosts</div>
            <div class="page-subtitle">
              Manage SSH host connections for file browsing — add servers here, then open SFTP to browse their files.
            </div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="loading" @click="pingAll">Check all</button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openAddHost">
              + Add host
            </button>
          </div>
        </section>

        <div v-if="loading" class="sfh-loading">Loading hosts…</div>

        <template v-else-if="hosts.length">
          <div class="sfh-toolbar">
            <input v-model="search" class="base-input sfh-search" type="search" placeholder="Filter hosts…" />
          </div>

          <!-- Host cards -->
          <div v-if="filteredHosts.length" class="sfh-grid">
          <div v-for="h in filteredHosts" :key="h.id" class="sfh-card page-card">
            <div class="sfh-card-head">
              <div class="sfh-status-row">
                <span
                  class="sfh-dot"
                  :class="pingState[h.id] === 'ok' ? 'sfh-dot--ok' : pingState[h.id] === 'err' ? 'sfh-dot--err' : 'sfh-dot--unknown'"
                  :title="pingState[h.id] === 'ok' ? 'SSH reachable' : pingState[h.id] === 'err' ? 'Unreachable' : 'Status unknown'"
                ></span>
                <span class="sfh-name">{{ h.name }}</span>
              </div>
              <div class="sfh-meta">
                <span class="sfh-ssh">{{ h.ssh_user ? h.ssh_user + '@' : '' }}{{ h.ssh_host }}{{ h.ssh_port && h.ssh_port !== 22 ? ':' + h.ssh_port : '' }}</span>
              </div>
            </div>

            <div class="sfh-status-label">
              <span v-if="pingingId === h.id" class="sfh-muted">Checking…</span>
              <span v-else-if="pingState[h.id] === 'ok'" class="sfh-ok">SSH reachable</span>
              <span v-else-if="pingState[h.id] === 'err'" class="sfh-err">Unreachable</span>
              <span v-else class="sfh-muted">Status unknown — click Check</span>
            </div>

            <div class="sfh-actions">
              <RowActionsMenu :actions="cardActions(h)" />
            </div>
          </div>
          </div>
          <div v-else class="page-card sfh-empty">
            <p>No hosts match "{{ search }}".</p>
          </div>
        </template>

        <!-- Empty state -->
        <div v-else-if="!loading" class="page-card sfh-empty">
          <div class="sfh-empty-icon">🗂️</div>
          <h2>No SSH hosts yet</h2>
          <p>Add a remote server by SSH to browse and manage its files via SFTP.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="openAddHost">Add your first host</button>
        </div>
      </div>
    </div>

    <!-- Host form modal -->
    <div v-if="showHostForm" class="sfh-modal-backdrop" @click.self="showHostForm = false">
      <div class="sfh-modal page-card">
        <div class="sfh-modal-title">{{ editingHostId === null ? 'Add SSH host' : 'Edit SSH host' }}</div>
        <div class="sfh-form">
          <label>
            Name
            <input v-model="form.name" class="base-input" placeholder="prod-01" />
          </label>
          <div class="sfh-form-row">
            <label class="sfh-grow">
              SSH host
              <input v-model="form.ssh_host" class="base-input" placeholder="10.0.0.5 or host.example.com" />
            </label>
            <label class="sfh-port-field">
              Port
              <input v-model.number="form.ssh_port" class="base-input" type="number" />
            </label>
          </div>
          <label>
            SSH user
            <input v-model="form.ssh_user" class="base-input" placeholder="ubuntu" />
          </label>
          <label>
            SSH password
            <input
              v-model="form.ssh_password"
              class="base-input"
              type="password"
              :placeholder="editingHostId !== null ? '•••••• (unchanged)' : ''"
            />
          </label>
          <label>
            SSH private key (optional)
            <textarea
              v-model="form.ssh_key"
              class="base-input sfh-textarea"
              rows="3"
              :placeholder="editingHostId !== null ? '(unchanged)' : '-----BEGIN OPENSSH PRIVATE KEY-----'"
            ></textarea>
          </label>
        </div>
        <div class="sfh-modal-actions">
          <button class="base-btn base-btn--sm" :disabled="testing" @click="testHost">
            {{ testing ? 'Testing…' : 'Test connection' }}
          </button>
          <div class="sfh-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showHostForm = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingHost" @click="saveHost">
            {{ savingHost ? 'Saving…' : 'Save' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sfh-toolbar { display: flex; margin-bottom: 14px; }
.sfh-search { width: 240px; max-width: 40vw; }

/* Host cards grid */
.sfh-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
}

.sfh-card {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* Card header */
.sfh-card-head { display: flex; flex-direction: column; gap: 4px; }

.sfh-status-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sfh-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.sfh-dot--ok      { background: var(--success); }
.sfh-dot--err     { background: var(--danger); }
.sfh-dot--unknown { background: var(--text-muted); opacity: 0.5; }

.sfh-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.sfh-meta { padding-left: 16px; }
.sfh-ssh {
  font-size: 12px;
  font-family: var(--mono);
  color: var(--text-muted);
}

/* Status label */
.sfh-status-label { font-size: 12px; }
.sfh-ok   { color: var(--success); }
.sfh-err  { color: var(--danger); }
.sfh-muted { color: var(--text-muted); }

/* Action buttons */
.sfh-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  padding-top: 6px;
  border-top: 1px solid var(--border);
}

/* Loading / empty */
.sfh-loading {
  padding: 40px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}

.sfh-empty {
  padding: 60px 40px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}
.sfh-empty-icon { font-size: 48px; }
.sfh-empty h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.sfh-empty p  { font-size: 13px; color: var(--text-muted); margin: 0; max-width: 340px; }

/* Modal */
.sfh-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.sfh-modal { padding: 22px; width: 440px; max-width: 92vw; }
.sfh-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 14px; }
.sfh-form { display: flex; flex-direction: column; gap: 10px; }
.sfh-form label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-secondary); }
.sfh-form-row { display: flex; gap: 10px; }
.sfh-grow { flex: 1; }
.sfh-port-field { width: 90px; }
.sfh-textarea { font-family: var(--mono); font-size: 11px; resize: vertical; }
.sfh-modal-actions { display: flex; gap: 8px; margin-top: 16px; align-items: center; }
.sfh-spacer { flex: 1; }
</style>
