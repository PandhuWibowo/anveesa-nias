<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import axios from 'axios'
import SearchSelect from '@/components/ui/SearchSelect.vue'
import SortIcon from '@/components/ui/SortIcon.vue'
import RowActionsMenu, { type RowAction } from '@/components/ui/RowActionsMenu.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useListFilter } from '@/composables/useListFilter'
import { useSort } from '@/composables/useSort'

interface SshHost {
  id: number
  name: string
  ssh_host: string
}
interface SftpEntry {
  name: string
  size: number
  isDir: boolean
  isLink: boolean
  mode: string
  modTime: number
}
interface UploadJob {
  id: number
  file: File
  name: string
  loaded: number
  total: number
  pct: number
  speed: number
  status: 'uploading' | 'paused' | 'done' | 'error' | 'cancelled'
  error: string
  controller: AbortController | null
}

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['sftp.manage']))

const hosts = ref<SshHost[]>([])
const hostId = ref<number | null>(null)
const hostStatus = ref<'unknown' | 'connecting' | 'connected' | 'error'>('unknown')
const hostOptions = computed(() => hosts.value.map((h) => ({
  value: h.id,
  label: `${h.name} (${h.ssh_host})`,
})))
const cwd = ref('/')
const entries = ref<SftpEntry[]>([])
const loading = ref(false)
const connError = ref('')
const dragOver = ref(false)

const { search, filtered: searchedEntries } = useListFilter(entries, (e, q) => e.name.toLowerCase().includes(q))
function sftpSortValue(e: SftpEntry, key: string): string | number {
  if (key === 'name') return e.name.toLowerCase()
  if (key === 'size') return e.size
  if (key === 'modTime') return e.modTime
  return ''
}
const { sortKey, sortDir, toggleSort, sort } = useSort<SftpEntry>(sftpSortValue)
const filteredEntries = computed(() => sort(searchedEntries.value))

const uploads = ref<UploadJob[]>([])
let uploadSeq = 1

// Modals
const showNewFolder = ref(false)
const folderName = ref('')
const showRename = ref(false)
const renameName = ref('')
const renameTarget = ref<SftpEntry | null>(null)
const showCompress = ref(false)
const compressTarget = ref<SftpEntry | null>(null)
const compressFormat = ref<'zip' | 'targz'>('zip')
const compressing = ref(false)

// View file
const showView = ref(false)
const viewName = ref('')
const viewContent = ref('')
const viewLoading = ref(false)
const viewError = ref('')
const viewTruncated = ref(false)
const viewBinary = ref(false)

// ── Path helpers ────────────────────────────────────────────────
function joinPath(base: string, name: string) {
  return base === '/' ? '/' + name : base.replace(/\/$/, '') + '/' + name
}
function parentPath(p: string) {
  if (p === '/' || !p.includes('/')) return '/'
  const i = p.replace(/\/$/, '').lastIndexOf('/')
  return i <= 0 ? '/' : p.slice(0, i)
}
const crumbs = computed(() => {
  const segs = cwd.value.split('/').filter(Boolean)
  let acc = ''
  return segs.map((s) => {
    acc += '/' + s
    return { label: s, path: acc }
  })
})

// ── Loading ─────────────────────────────────────────────────────
// Remember the user's connection intent across refreshes.
const LAST_HOST_KEY = 'nias:sftp:lastHost'

async function loadHosts() {
  try {
    const { data } = await axios.get<SshHost[]>('/api/docker/hosts')
    hosts.value = data.filter((h) => h.ssh_host)
    if (hostId.value === null && hosts.value.length) {
      // An explicit ?host= (e.g. from the SSH Hosts page) means "connect now".
      const queryId = route.query.host ? Number(route.query.host) : null
      const queryHost = queryId ? hosts.value.find((h) => h.id === queryId) : null
      if (queryHost) { await selectHost(queryHost.id); return }
      const saved = localStorage.getItem(LAST_HOST_KEY)
      if (saved === 'disconnected') return // user explicitly disconnected
      const savedId = saved ? Number(saved) : null
      const target = (savedId && hosts.value.find((h) => h.id === savedId)) || hosts.value[0]
      await selectHost(target.id)
    }
  } catch (e: any) {
    toast.error('Failed to load hosts')
  }
}

async function selectHost(id: number) {
  hostId.value = id
  cwd.value = '/'
  localStorage.setItem(LAST_HOST_KEY, String(id))
  await loadDir('')
}

function disconnectHost() {
  hostId.value = null
  hostStatus.value = 'unknown'
  localStorage.setItem(LAST_HOST_KEY, 'disconnected')
  // Clear browsing state so nothing stale is shown after disconnect
  cwd.value = '/'
  entries.value = []
  connError.value = ''
  uploads.value = []
}

async function loadDir(path: string) {
  if (hostId.value === null) return
  loading.value = true
  connError.value = ''
  if (hostStatus.value !== 'connected') hostStatus.value = 'connecting'
  try {
    const { data } = await axios.get<{ path: string; entries: SftpEntry[] }>(
      `/api/sftp/hosts/${hostId.value}/list`,
      { params: { path } },
    )
    cwd.value = data.path || '/'
    entries.value = data.entries || []
    hostStatus.value = 'connected'
    search.value = ''
  } catch (e: any) {
    connError.value = e?.response?.data?.error || 'Could not list directory'
    entries.value = []
    hostStatus.value = 'error'
  } finally {
    loading.value = false
  }
}

function open(entry: SftpEntry) {
  if (entry.isDir) loadDir(joinPath(cwd.value, entry.name))
}
function up() {
  loadDir(parentPath(cwd.value))
}

// ── Download (native browser streaming) ─────────────────────────
function download(entry: SftpEntry) {
  const token = localStorage.getItem('nias-token') || ''
  const url = `/api/sftp/hosts/${hostId.value}/download?path=${encodeURIComponent(joinPath(cwd.value, entry.name))}&token=${encodeURIComponent(token)}`
  const a = document.createElement('a')
  a.href = url
  a.download = entry.name
  a.click()
}

// ── Upload (streamed, with progress + speed; pause/resume/cancel) ──
async function uploadFiles(files: FileList | File[]) {
  for (const file of Array.from(files)) {
    const job: UploadJob = {
      id: uploadSeq++,
      file,
      name: file.name,
      loaded: 0,
      total: file.size,
      pct: 0,
      speed: 0,
      status: 'uploading',
      error: '',
      controller: null,
    }
    uploads.value = [job, ...uploads.value]
    await uploadOne(job)
  }
}
// Pause aborts the in-flight request; Resume re-uploads the same file from
// the start (0%) — there's no server-side append/resume support, so this is
// a "stop and restart" pause rather than a true byte-offset resume.
// (Split into its own function so TS re-checks job.status fresh instead of
// narrowing it to the literal set from earlier in uploadOne's control flow.)
function wasInterrupted(job: UploadJob): boolean {
  return job.status === 'paused' || job.status === 'cancelled'
}
function performUpload(job: UploadJob, sudo: boolean) {
  const controller = new AbortController()
  job.controller = controller
  const fd = new FormData()
  fd.append('file', job.file)
  const start = Date.now()
  return axios.post(`/api/sftp/hosts/${hostId.value}/upload`, fd, {
    params: { path: cwd.value, ...sudoParams(sudo) },
    signal: controller.signal,
    onUploadProgress: (e) => {
      job.loaded = e.loaded
      job.total = e.total || job.file.size
      job.pct = job.total ? Math.round((job.loaded / job.total) * 100) : 0
      const elapsed = (Date.now() - start) / 1000
      job.speed = elapsed > 0 ? job.loaded / elapsed : 0
    },
  })
}
async function uploadOne(job: UploadJob) {
  job.status = 'uploading'
  job.error = ''
  try {
    await withSudoRetry((sudo) => performUpload(job, sudo))
    job.status = 'done'
    job.pct = 100
    setTimeout(() => {
      uploads.value = uploads.value.filter((u) => u.id !== job.id)
    }, 2500)
    await loadDir(cwd.value)
  } catch (e: any) {
    if (wasInterrupted(job)) {
      // expected — pauseUpload()/cancelUpload() already set the status and aborted
    } else if (axios.isCancel(e) || e.code === 'ERR_CANCELED') {
      job.status = 'cancelled'
    } else {
      job.status = 'error'
      job.error = e?.response?.data?.error || 'upload failed'
    }
  } finally {
    job.controller = null
  }
}
function pauseUpload(job: UploadJob) {
  if (job.status !== 'uploading') return
  job.status = 'paused'
  job.controller?.abort()
}
function resumeUpload(job: UploadJob) {
  if (job.status !== 'paused') return
  job.loaded = 0
  job.pct = 0
  job.speed = 0
  uploadOne(job)
}
function cancelUpload(job: UploadJob) {
  job.status = 'cancelled'
  job.controller?.abort()
  uploads.value = uploads.value.filter((u) => u.id !== job.id)
}
function onFilePick(ev: Event) {
  const input = ev.target as HTMLInputElement
  if (input.files?.length) uploadFiles(input.files)
  input.value = ''
}
function onDrop(ev: DragEvent) {
  dragOver.value = false
  if (ev.dataTransfer?.files?.length) uploadFiles(ev.dataTransfer.files)
}

// ── File ops ────────────────────────────────────────────────────
function newFolder() {
  folderName.value = ''
  showNewFolder.value = true
}
// A permission/sudo error means the SSH user lacks direct access to read the
// source or write into the destination directory (e.g. a root-owned system
// path) — the fix is to elevate. We retry once with sudo, transparently.
function isPermDenied(msg: string): boolean {
  const m = msg.toLowerCase()
  return m.includes('permission denied') || m.includes('not permitted') || m.includes('sudo')
}
async function withSudoRetry<T>(run: (sudo: boolean) => Promise<T>): Promise<T> {
  try {
    return await run(false)
  } catch (e: any) {
    if (!isPermDenied(e?.response?.data?.error || '')) throw e
    toast.info('Permission denied — retrying with sudo')
    return run(true)
  }
}
function sudoParams(sudo: boolean) {
  return sudo ? { sudo: '1' } : {}
}

async function submitNewFolder() {
  const name = folderName.value.trim()
  if (!name) return
  try {
    await withSudoRetry((sudo) => axios.post(`/api/sftp/hosts/${hostId.value}/mkdir`,
      { path: joinPath(cwd.value, name) }, { params: sudoParams(sudo) }))
    toast.success('Folder created')
    showNewFolder.value = false
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create folder')
  }
}
function rename(entry: SftpEntry) {
  renameTarget.value = entry
  renameName.value = entry.name
  showRename.value = true
}
async function submitRename() {
  const name = renameName.value.trim()
  const entry = renameTarget.value
  if (!entry || !name || name === entry.name) {
    showRename.value = false
    return
  }
  try {
    await withSudoRetry((sudo) => axios.post(`/api/sftp/hosts/${hostId.value}/rename`, {
      from: joinPath(cwd.value, entry.name),
      to: joinPath(cwd.value, name),
    }, { params: sudoParams(sudo) }))
    toast.success('Renamed')
    showRename.value = false
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Rename failed')
  }
}
function compress(entry: SftpEntry) {
  compressTarget.value = entry
  compressFormat.value = 'zip'
  showCompress.value = true
}
async function submitCompress() {
  const entry = compressTarget.value
  if (!entry) return
  compressing.value = true
  try {
    const { data } = await withSudoRetry((sudo) => axios.post<{ name: string }>(
      `/api/sftp/hosts/${hostId.value}/compress`,
      { path: joinPath(cwd.value, entry.name), format: compressFormat.value },
      { params: sudoParams(sudo) },
    ))
    toast.success(`Created ${data.name}`)
    showCompress.value = false
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Compress failed')
  } finally {
    compressing.value = false
  }
}
async function del(entry: SftpEntry) {
  const ok = await confirm(
    `Delete "${entry.name}"${entry.isDir ? ' and everything in it' : ''}? This cannot be undone.`,
    'Delete',
  )
  if (!ok) return
  try {
    await withSudoRetry((sudo) => axios.post(`/api/sftp/hosts/${hostId.value}/delete`,
      { path: joinPath(cwd.value, entry.name) }, { params: sudoParams(sudo) }))
    toast.success('Deleted')
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Delete failed')
  }
}

// ── View file ───────────────────────────────────────────────────
function isArchive(name: string): boolean {
  const n = name.toLowerCase()
  return n.endsWith('.zip') || n.endsWith('.tar.gz') || n.endsWith('.tgz') || n.endsWith('.tar')
}
async function view(entry: SftpEntry) {
  viewName.value = entry.name
  viewContent.value = ''
  viewError.value = ''
  viewTruncated.value = false
  viewBinary.value = false
  showView.value = true
  viewLoading.value = true
  try {
    if (isArchive(entry.name)) {
      const { data } = await axios.get<{ listing: string }>(
        `/api/sftp/hosts/${hostId.value}/archive`,
        { params: { path: joinPath(cwd.value, entry.name) } },
      )
      viewContent.value = data.listing
    } else {
      const { data } = await axios.get<{ content: string; truncated: boolean; binary: boolean }>(
        `/api/sftp/hosts/${hostId.value}/read`,
        { params: { path: joinPath(cwd.value, entry.name) } },
      )
      viewContent.value = data.content
      viewTruncated.value = data.truncated
      viewBinary.value = data.binary
    }
  } catch (e: any) {
    viewError.value = e?.response?.data?.error || 'Could not read file'
  } finally {
    viewLoading.value = false
  }
}
async function extract(entry: SftpEntry) {
  const ok = await confirm(`Extract "${entry.name}" into this folder? Existing files with the same names will be overwritten.`, 'Extract')
  if (!ok) return
  try {
    await withSudoRetry((sudo) => axios.post(`/api/sftp/hosts/${hostId.value}/extract`,
      { path: joinPath(cwd.value, entry.name) }, { params: sudoParams(sudo) }))
    toast.success(`Extracted ${entry.name}`)
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Extract failed')
  }
}

// ── Row actions (View/Download inline; the rest in the "⋯" menu) ──
function rowActions(e: SftpEntry): RowAction[] {
  const actions: RowAction[] = []
  if (!e.isDir) {
    actions.push({ key: 'view', label: 'View', icon: 'view', primary: true, onClick: () => view(e) })
    actions.push({ key: 'download', label: 'Download', icon: 'download', primary: true, onClick: () => download(e) })
  }
  if (canManage.value) {
    if (e.isDir) {
      actions.push({ key: 'compress', label: 'Compress', icon: 'archive', onClick: () => compress(e) })
    } else if (isArchive(e.name)) {
      actions.push({ key: 'extract', label: 'Extract', icon: 'unarchive', onClick: () => extract(e) })
    }
    actions.push({ key: 'rename', label: 'Rename', icon: 'edit', onClick: () => rename(e) })
    actions.push({ key: 'delete', label: 'Delete', icon: 'delete', danger: true, onClick: () => del(e) })
  }
  return actions
}

// ── Formatting ──────────────────────────────────────────────────
function formatBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(i ? 1 : 0)} ${u[i]}`
}
function formatTime(unix: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleString()
}
function fileIcon(e: SftpEntry): string {
  if (e.isDir) return '📁'
  if (e.isLink) return '🔗'
  const ext = e.name.split('.').pop()?.toLowerCase() || ''
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) return '🖼️'
  if (['zip', 'tar', 'gz', 'tgz', 'xz', 'bz2', '7z'].includes(ext)) return '🗜️'
  if (['sh', 'bash', 'zsh'].includes(ext)) return '⚙️'
  if (['json', 'yml', 'yaml', 'toml', 'env', 'conf', 'ini'].includes(ext)) return '🧾'
  if (['md', 'txt', 'log'].includes(ext)) return '📝'
  return '📄'
}

onMounted(loadHosts)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">SFTP</div>
            <div class="page-subtitle">Browse, upload, and download files on your servers over SSH.</div>
          </div>
          <div class="page-hero__actions">
            <SearchSelect
              v-if="hosts.length"
              class="sf-host"
              :model-value="hostId"
              :options="hostOptions"
              placeholder="Select host…"
              @update:model-value="selectHost(Number($event))"
            />
            <div v-if="hostId !== null" class="sf-conn-status" :class="`sf-conn-status--${hostStatus}`">
              <span class="sf-conn-dot"></span>
              <span>{{ hostStatus === 'connected' ? 'Connected' : hostStatus === 'connecting' ? 'Connecting…' : hostStatus === 'error' ? 'Error' : 'Idle' }}</span>
            </div>
            <button v-if="hostId !== null" class="base-btn base-btn--sm" :disabled="loading" @click="loadDir(cwd)">Refresh</button>
            <button v-if="hostId !== null" class="base-btn base-btn--sm" @click="disconnectHost">Disconnect</button>
            <button v-if="canManage" class="base-btn base-btn--sm" @click="router.push({ name: 'ssh-hosts' })">Manage hosts</button>
          </div>
        </section>

        <div v-if="!hosts.length" class="page-card sf-empty">
          <div class="sf-empty-icon">🗂️</div>
          <h2>No SSH servers</h2>
          <p>SFTP uses your remote SSH hosts. Add one under <b>SFTP Hosts</b> first.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="router.push({ name: 'ssh-hosts' })">Add your first host</button>
        </div>

        <template v-else-if="hostId !== null">
          <!-- Toolbar + breadcrumbs -->
          <div class="sf-bar">
            <button class="base-btn base-btn--sm" :disabled="cwd === '/'" @click="up">↑ Up</button>
            <nav class="sf-crumbs">
              <button class="sf-crumb" @click="loadDir('/')">/</button>
              <template v-for="(c, i) in crumbs" :key="c.path">
                <span class="sf-sep">/</span>
                <button class="sf-crumb" :class="{ 'sf-crumb--last': i === crumbs.length - 1 }" @click="loadDir(c.path)">{{ c.label }}</button>
              </template>
            </nav>
            <div class="dk-spacer"></div>
            <input
              v-model="search"
              class="base-input sf-search"
              type="search"
              placeholder="Filter files…"
            />
            <button v-if="canManage" class="base-btn base-btn--sm" @click="newFolder">+ Folder</button>
            <label v-if="canManage" class="base-btn base-btn--primary base-btn--sm sf-upload-btn">
              ↑ Upload<input type="file" hidden multiple @change="onFilePick" />
            </label>
          </div>

          <!-- File list with drag-drop -->
          <div
            class="page-card sf-list"
            :class="{ 'sf-list--drag': dragOver }"
            @dragover.prevent="dragOver = canManage"
            @dragleave="dragOver = false"
            @drop.prevent="onDrop"
          >
            <div v-if="dragOver" class="sf-drop-hint">Drop files to upload to <b>{{ cwd }}</b></div>
            <table class="sf-table">
              <thead>
                <tr>
                  <th class="sf-col-name" :class="{ sorted: sortKey === 'name' }" @click="toggleSort('name')">Name <SortIcon :active="sortKey === 'name'" :dir="sortDir" /></th>
                  <th class="sf-col-size" :class="{ sorted: sortKey === 'size' }" @click="toggleSort('size')">Size <SortIcon :active="sortKey === 'size'" :dir="sortDir" /></th>
                  <th class="sf-col-mode">Permissions</th>
                  <th class="sf-col-time" :class="{ sorted: sortKey === 'modTime' }" @click="toggleSort('modTime')">Modified <SortIcon :active="sortKey === 'modTime'" :dir="sortDir" /></th>
                  <th class="sf-col-act"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="loading"><td colspan="5" class="sf-msg">Loading…</td></tr>
                <tr v-else-if="connError"><td colspan="5" class="sf-msg sf-err">{{ connError }}</td></tr>
                <tr v-else-if="!entries.length"><td colspan="5" class="sf-msg">Empty directory.</td></tr>
                <tr v-else-if="!filteredEntries.length"><td colspan="5" class="sf-msg">No files match "{{ search }}".</td></tr>
                <tr v-for="e in filteredEntries" :key="e.name" class="sf-row" :class="{ 'sf-row--dir': e.isDir }">
                  <td class="sf-name" @click="e.isDir ? open(e) : view(e)">
                    <span class="sf-icon">{{ fileIcon(e) }}</span>
                    <span class="sf-fname">{{ e.name }}</span>
                  </td>
                  <td class="sf-size">{{ e.isDir ? '—' : formatBytes(e.size) }}</td>
                  <td class="sf-mode">{{ e.mode }}</td>
                  <td class="sf-time">{{ formatTime(e.modTime) }}</td>
                  <td class="sf-act">
                    <RowActionsMenu :actions="rowActions(e)" />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>

        <!-- Idle: hosts exist but none selected (after disconnect) -->
        <div v-else class="page-card sf-idle">
          <div class="sf-idle-icon">🗂️</div>
          <p>Select a host from the dropdown above to connect.</p>
        </div>
      </div>
    </div>

    <!-- New folder modal -->
    <div v-if="showNewFolder" class="sf-modal-backdrop" @click.self="showNewFolder = false">
      <div class="sf-modal page-card">
        <div class="sf-modal-title">New folder</div>
        <input v-model="folderName" class="base-input sf-modal-input" placeholder="folder name" @keyup.enter="submitNewFolder" />
        <div class="sf-modal-actions">
          <button class="base-btn base-btn--sm" @click="showNewFolder = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!folderName.trim()" @click="submitNewFolder">Create</button>
        </div>
      </div>
    </div>

    <!-- Rename modal -->
    <div v-if="showRename" class="sf-modal-backdrop" @click.self="showRename = false">
      <div class="sf-modal page-card">
        <div class="sf-modal-title">Rename</div>
        <input v-model="renameName" class="base-input sf-modal-input" @keyup.enter="submitRename" />
        <div class="sf-modal-actions">
          <button class="base-btn base-btn--sm" @click="showRename = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!renameName.trim()" @click="submitRename">Rename</button>
        </div>
      </div>
    </div>

    <!-- Compress modal -->
    <div v-if="showCompress" class="sf-modal-backdrop" @click.self="showCompress = false">
      <div class="sf-modal page-card">
        <div class="sf-modal-title">Compress "{{ compressTarget?.name }}"</div>
        <div class="sf-compress-formats">
          <label class="sf-radio">
            <input type="radio" value="zip" v-model="compressFormat" />
            <span>.zip</span>
          </label>
          <label class="sf-radio">
            <input type="radio" value="targz" v-model="compressFormat" />
            <span>.tar.gz</span>
          </label>
        </div>
        <div class="sf-modal-actions">
          <button class="base-btn base-btn--sm" @click="showCompress = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="compressing" @click="submitCompress">
            {{ compressing ? 'Compressing…' : 'Compress' }}
          </button>
        </div>
      </div>
    </div>

    <!-- View file modal -->
    <div v-if="showView" class="sf-modal-backdrop" @click.self="showView = false">
      <div class="sf-modal sf-view-modal page-card">
        <div class="sf-modal-title">{{ viewName }}</div>
        <div v-if="viewLoading" class="sf-msg">Loading…</div>
        <div v-else-if="viewError" class="sf-msg sf-err">{{ viewError }}</div>
        <div v-else-if="viewBinary" class="sf-msg">This file looks binary and can't be previewed. Use Download instead.</div>
        <template v-else>
          <div v-if="viewTruncated" class="sf-view-notice">Showing the first 2 MB of this file.</div>
          <pre class="sf-view-content">{{ viewContent }}</pre>
        </template>
        <div class="sf-modal-actions">
          <button class="base-btn base-btn--sm" @click="showView = false">Close</button>
        </div>
      </div>
    </div>

    <!-- Upload progress dock -->
    <div v-if="uploads.length" class="sf-uploads">
      <div class="sf-uploads-head">Uploads</div>
      <div v-for="u in uploads" :key="u.id" class="sf-up">
        <div class="sf-up-row">
          <span class="sf-up-name">{{ u.name }}</span>
          <span v-if="u.status === 'error'" class="sf-err">{{ u.error }}</span>
          <span v-else-if="u.status === 'done'" class="sf-up-done">✓ done</span>
          <span v-else-if="u.status === 'cancelled'" class="sf-up-meta">Cancelled</span>
          <span v-else-if="u.status === 'paused'" class="sf-up-meta">Paused · {{ u.pct }}%</span>
          <span v-else class="sf-up-meta">{{ u.pct }}% · {{ formatBytes(u.speed) }}/s</span>
        </div>
        <div class="sf-up-bar"><div class="sf-up-fill" :class="{ 'sf-up-fill--err': u.status === 'error', 'sf-up-fill--paused': u.status === 'paused' }" :style="{ width: u.pct + '%' }"></div></div>
        <div class="sf-up-actions">
          <button v-if="u.status === 'uploading'" class="sf-up-btn" @click="pauseUpload(u)">Pause</button>
          <button v-if="u.status === 'paused'" class="sf-up-btn" @click="resumeUpload(u)">Resume</button>
          <button v-if="u.status === 'uploading' || u.status === 'paused'" class="sf-up-btn sf-up-btn--danger" @click="cancelUpload(u)">Cancel</button>
          <button v-if="u.status === 'error' || u.status === 'cancelled'" class="sf-up-btn" @click="uploads = uploads.filter((j) => j.id !== u.id)">Dismiss</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sf-host { min-width: 240px; }

/* Connection status badge */
.sf-conn-status { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 600; border: 1px solid transparent; }
.sf-conn-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.sf-conn-status--connected { color: var(--success); background: var(--success-bg, rgba(34,197,94,0.12)); }
.sf-conn-status--error { color: var(--danger); background: var(--danger-bg, rgba(239,68,68,0.12)); }
.sf-conn-status--connecting { color: var(--text-muted); background: var(--bg-hover); }
.sf-conn-status--connecting .sf-conn-dot { animation: sf-blink 1s ease-in-out infinite; }
.sf-conn-status--unknown { color: var(--text-muted); background: var(--bg-hover); }
@keyframes sf-blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.sf-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.sf-idle-icon { font-size: 22px; }
.sf-empty { text-align: center; padding: 64px 20px; color: var(--text-secondary); }
.sf-empty-icon { font-size: 44px; }
.sf-empty h2 { margin: 12px 0 4px; font-size: 16px; color: var(--text-primary); }
.sf-empty p { font-size: 13px; color: var(--text-muted); }

.sf-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.sf-crumbs { display: flex; align-items: center; gap: 2px; flex-wrap: wrap; font-size: 13px; min-width: 0; }
.sf-crumb { background: none; border: none; color: var(--brand); cursor: pointer; padding: 2px 4px; border-radius: var(--r-xs); font-family: var(--mono); font-size: 12px; }
.sf-crumb:hover { background: var(--bg-hover); }
.sf-crumb--last { color: var(--text-primary); font-weight: 600; }
.sf-sep { color: var(--text-muted); }
.sf-upload-btn { cursor: pointer; }
.sf-search { width: 200px; max-width: 40vw; }

.sf-list { position: relative; padding: 4px 6px; overflow: hidden; }
.sf-list--drag { outline: 2px dashed var(--brand); outline-offset: -4px; }
.sf-drop-hint { position: absolute; inset: 0; z-index: 2; display: flex; align-items: center; justify-content: center; background: var(--brand-dim); color: var(--brand); font-size: 14px; pointer-events: none; }

.sf-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.sf-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.sf-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.sf-table tbody tr:last-child td { border-bottom: none; }
.sf-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.sf-err { color: var(--danger); }
.sf-row:hover { background: var(--bg-hover); }
.sf-name { display: flex; align-items: center; gap: 9px; cursor: pointer; }
.sf-row--dir .sf-fname { color: var(--brand); font-weight: 500; }
.sf-icon { font-size: 15px; width: 18px; text-align: center; }
.sf-fname { word-break: break-all; }
.sf-size { color: var(--text-secondary); white-space: nowrap; }
.sf-mode { font-family: var(--mono); font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.sf-time { color: var(--text-muted); font-size: 12px; white-space: nowrap; }
.sf-act { text-align: right; white-space: nowrap; }
.sf-col-size, .sf-col-mode, .sf-col-time { width: 1%; }
.sf-col-name, .sf-col-size, .sf-col-time { cursor: pointer; user-select: none; }

.sf-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.sf-modal { width: 380px; max-width: 92vw; padding: 20px; }
.sf-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; word-break: break-all; }
.sf-modal-input { width: 100%; }
.sf-modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }

.sf-compress-formats { display: flex; gap: 16px; }
.sf-radio { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-primary); cursor: pointer; }

.sf-view-modal { width: 760px; max-width: 92vw; max-height: 82vh; display: flex; flex-direction: column; }
.sf-view-notice { font-size: 12px; color: var(--text-muted); margin-bottom: 8px; }
.sf-view-content { flex: 1; overflow: auto; background: var(--bg-hover); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 12px; font-family: var(--mono); font-size: 12px; white-space: pre-wrap; word-break: break-all; margin: 0; }

.sf-uploads { position: fixed; right: 18px; bottom: 18px; width: 320px; max-height: 50vh; overflow-y: auto; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); z-index: 90; padding: 10px 12px; }
.sf-uploads-head { font-size: 12px; font-weight: 600; color: var(--text-primary); margin-bottom: 8px; }
.sf-up { margin-bottom: 8px; }
.sf-up-row { display: flex; justify-content: space-between; gap: 8px; font-size: 11px; margin-bottom: 3px; }
.sf-up-name { color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.sf-up-meta { color: var(--text-muted); white-space: nowrap; font-family: var(--mono); }
.sf-up-done { color: var(--success); }
.sf-up-bar { height: 5px; background: var(--bg-hover); border-radius: 99px; overflow: hidden; }
.sf-up-fill { height: 100%; background: var(--brand); transition: width 0.2s ease; }
.sf-up-fill--err { background: var(--danger); }
.sf-up-fill--paused { background: var(--text-muted); }
.sf-up-actions { display: flex; justify-content: flex-end; gap: 6px; margin-top: 4px; }
.sf-up-btn { background: none; border: none; padding: 0; font-size: 11px; color: var(--brand); cursor: pointer; }
.sf-up-btn:hover { text-decoration: underline; }
.sf-up-btn--danger { color: var(--danger); }
</style>
