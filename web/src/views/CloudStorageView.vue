<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import ConnectionPicker from '@/components/ui/ConnectionPicker.vue'
import SortIcon from '@/components/ui/SortIcon.vue'
import RowActionsMenu, { type RowAction } from '@/components/ui/RowActionsMenu.vue'
import ActionIcon from '@/components/ui/ActionIcon.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useListFilter } from '@/composables/useListFilter'
import { useSort } from '@/composables/useSort'
import { useConnections, type DbDriver } from '@/composables/useConnections'

// One entry in the browser table — folders come from the backend's
// CommonPrefixes (delimiter-based listing), files from Contents. Both carry
// their full bucket key already, so no client-side path-joining is needed
// for anything already listed (only for brand-new names: mkdir/rename/upload).
interface CsEntry {
  name: string
  isDir: boolean
  size: number
  lastModified: string
  key: string
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
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['cloudstorage.manage']))

const OSS_DRIVERS: DbDriver[] = ['s3_aws', 's3_gcp', 's3_oss', 's3_obs']
const { connections, fetchConnections } = useConnections()
const bucketConnections = computed(() => connections.value.filter((c) => OSS_DRIVERS.includes(c.driver)))

const connId = ref<number | null>(null)
const connStatus = ref<'unknown' | 'connecting' | 'connected' | 'error'>('unknown')
const cwd = ref('') // S3 prefix — '' is bucket root, otherwise always ends with '/'
const entries = ref<CsEntry[]>([])
const loading = ref(false)
const connError = ref('')
const truncated = ref(false)
const dragOver = ref(false)

const { search, filtered: searchedEntries } = useListFilter(entries, (e, q) => e.name.toLowerCase().includes(q))
function csSortValue(e: CsEntry, key: string): string | number {
  if (key === 'name') return e.name.toLowerCase()
  if (key === 'size') return e.size
  if (key === 'lastModified') return e.lastModified
  return ''
}
const { sortKey, sortDir, toggleSort, sort } = useSort<CsEntry>(csSortValue)
const filteredEntries = computed(() => sort(searchedEntries.value))

// ── Multi-select ──────────────────────────────────────────────
const selected = ref<Set<string>>(new Set())
function toggleSelect(name: string) {
  const s = new Set(selected.value)
  if (s.has(name)) s.delete(name); else s.add(name)
  selected.value = s
}
const allSelected = computed(() => filteredEntries.value.length > 0 && selected.value.size === filteredEntries.value.length)
function toggleSelectAll() {
  selected.value = allSelected.value ? new Set() : new Set(filteredEntries.value.map((e) => e.name))
}

const uploads = ref<UploadJob[]>([])
let uploadSeq = 1

// Modals
const showNewFolder = ref(false)
const folderName = ref('')
const showRename = ref(false)
const renameName = ref('')
const renameTarget = ref<CsEntry | null>(null)

// ── Path helpers ────────────────────────────────────────────────
function parentPrefix(p: string): string {
  const trimmed = p.replace(/\/$/, '')
  if (!trimmed.includes('/')) return ''
  return trimmed.slice(0, trimmed.lastIndexOf('/') + 1)
}
const crumbs = computed(() => {
  const segs = cwd.value.split('/').filter(Boolean)
  let acc = ''
  return segs.map((s) => {
    acc += s + '/'
    return { label: s, prefix: acc }
  })
})

// ── Loading ─────────────────────────────────────────────────────
const LAST_CONN_KEY = 'nias:cloudstorage:lastConn'

async function loadConnections() {
  if (!connections.value.length) await fetchConnections()
  if (connId.value === null && bucketConnections.value.length) {
    const saved = localStorage.getItem(LAST_CONN_KEY)
    if (saved === 'disconnected') return
    const savedId = saved ? Number(saved) : null
    const target = (savedId && bucketConnections.value.find((c) => c.id === savedId)) || bucketConnections.value[0]
    await selectConnection(target.id)
  }
}

async function selectConnection(id: number) {
  connId.value = id
  cwd.value = ''
  localStorage.setItem(LAST_CONN_KEY, String(id))
  await loadDir('')
}

function disconnectConn() {
  connId.value = null
  connStatus.value = 'unknown'
  localStorage.setItem(LAST_CONN_KEY, 'disconnected')
  cwd.value = ''
  entries.value = []
  connError.value = ''
  uploads.value = []
}

function basename(key: string): string {
  return key.replace(/\/$/, '').split('/').pop() || key
}

async function loadDir(prefix: string) {
  if (connId.value === null) return
  loading.value = true
  connError.value = ''
  if (connStatus.value !== 'connected') connStatus.value = 'connecting'
  try {
    const { data } = await axios.get<{
      prefix: string
      files: { key: string; size: number; last_modified: string }[]
      folders: string[]
      truncated: boolean
    }>(`/api/connections/${connId.value}/storage/list`, { params: { prefix } })
    cwd.value = data.prefix || ''
    const folderEntries: CsEntry[] = (data.folders || []).map((p) => ({
      name: basename(p), isDir: true, size: 0, lastModified: '', key: p,
    }))
    const fileEntries: CsEntry[] = (data.files || []).map((f) => ({
      name: basename(f.key), isDir: false, size: f.size, lastModified: f.last_modified, key: f.key,
    }))
    entries.value = [...folderEntries, ...fileEntries]
    truncated.value = !!data.truncated
    connStatus.value = 'connected'
    search.value = ''
    selected.value = new Set()
  } catch (e: any) {
    connError.value = e?.response?.data?.error || 'Could not list bucket'
    entries.value = []
    connStatus.value = 'error'
  } finally {
    loading.value = false
  }
}

function open(entry: CsEntry) {
  if (entry.isDir) loadDir(entry.key)
}
function up() {
  loadDir(parentPrefix(cwd.value))
}

// ── Download (native browser streaming) ─────────────────────────
function download(entry: CsEntry) {
  const token = localStorage.getItem('nias-token') || ''
  const url = `/api/connections/${connId.value}/storage/download?key=${encodeURIComponent(entry.key)}&token=${encodeURIComponent(token)}`
  const a = document.createElement('a')
  a.href = url
  a.download = entry.name
  a.click()
}

// Bulk download — zips one or more objects/folders server-side and streams
// the zip back natively, same anchor-tag-GET trick as single download.
// Folders (keys ending "/") are expanded recursively with structure kept.
function downloadZip(targets: CsEntry[]) {
  if (!targets.length) return
  const token = localStorage.getItem('nias-token') || ''
  const params = new URLSearchParams()
  for (const e of targets) params.append('key', e.key)
  params.set('token', token)
  const a = document.createElement('a')
  a.href = `/api/connections/${connId.value}/storage/zip?${params.toString()}`
  a.click()
}
function downloadSelected() {
  downloadZip(filteredEntries.value.filter((e) => selected.value.has(e.name)))
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
function wasInterrupted(job: UploadJob): boolean {
  return job.status === 'paused' || job.status === 'cancelled'
}
function performUpload(job: UploadJob) {
  const controller = new AbortController()
  job.controller = controller
  const fd = new FormData()
  fd.append('file', job.file)
  const start = Date.now()
  return axios.post(`/api/connections/${connId.value}/storage/upload`, fd, {
    params: { prefix: cwd.value.replace(/\/$/, '') },
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
    await performUpload(job)
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
async function submitNewFolder() {
  const name = folderName.value.trim()
  if (!name) return
  try {
    await axios.post(`/api/connections/${connId.value}/storage/mkdir`, { path: cwd.value + name })
    toast.success('Folder created')
    showNewFolder.value = false
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create folder')
  }
}
function rename(entry: CsEntry) {
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
  const to = entry.isDir ? cwd.value + name + '/' : cwd.value + name
  try {
    await axios.post(`/api/connections/${connId.value}/storage/rename`, { from: entry.key, to })
    toast.success('Renamed')
    showRename.value = false
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Rename failed')
  }
}
async function del(entry: CsEntry) {
  const ok = await confirm(
    `Delete "${entry.name}"${entry.isDir ? ' and everything in it' : ''}? This cannot be undone.`,
    'Delete',
  )
  if (!ok) return
  try {
    if (entry.isDir) {
      await axios.post(`/api/connections/${connId.value}/storage/delete-prefix`, { prefix: entry.key })
    } else {
      await axios.post(`/api/connections/${connId.value}/storage/delete`, { key: entry.key })
    }
    toast.success('Deleted')
    await loadDir(cwd.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Delete failed')
  }
}

function copyPath(key: string) {
  navigator.clipboard?.writeText(key)
  toast.success('Path copied')
}

// ── Row actions (Download inline for files; the rest in the "⋯" menu) ──
function rowActions(e: CsEntry): RowAction[] {
  const actions: RowAction[] = []
  if (!e.isDir) {
    actions.push({ key: 'download', label: 'Download', icon: 'download', primary: true, onClick: () => download(e) })
  } else {
    actions.push({ key: 'download-zip', label: 'Download as ZIP', icon: 'download', primary: true, onClick: () => downloadZip([e]) })
  }
  actions.push({ key: 'copy-path', label: 'Copy key', icon: 'copy', onClick: () => copyPath(e.key) })
  if (canManage.value) {
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
function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}
function fileIcon(e: CsEntry): string {
  if (e.isDir) return '📁'
  const ext = e.name.split('.').pop()?.toLowerCase() || ''
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) return '🖼️'
  if (['zip', 'tar', 'gz', 'tgz', 'xz', 'bz2', '7z'].includes(ext)) return '🗜️'
  if (['sh', 'bash', 'zsh'].includes(ext)) return '⚙️'
  if (['json', 'yml', 'yaml', 'toml', 'env', 'conf', 'ini'].includes(ext)) return '🧾'
  if (['md', 'txt', 'log'].includes(ext)) return '📝'
  if (['sql', 'gz'].includes(ext)) return '🗄️'
  return '📄'
}

onMounted(loadConnections)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Cloud Storage</div>
            <div class="page-subtitle">Browse, upload, and download files across your connected object storage buckets.</div>
          </div>
          <div class="page-hero__actions">
            <ConnectionPicker
              v-if="bucketConnections.length"
              :model-value="connId"
              :drivers="OSS_DRIVERS"
              placeholder="Select bucket…"
              @update:model-value="(id) => id !== null && selectConnection(id)"
            />
            <div v-if="connId !== null" class="cs-conn-status" :class="`cs-conn-status--${connStatus}`">
              <span class="cs-conn-dot"></span>
              <span>{{ connStatus === 'connected' ? 'Connected' : connStatus === 'connecting' ? 'Connecting…' : connStatus === 'error' ? 'Error' : 'Idle' }}</span>
            </div>
            <button v-if="connId !== null" class="base-btn base-btn--sm" :disabled="loading" @click="loadDir(cwd)">Refresh</button>
            <button v-if="connId !== null" class="base-btn base-btn--sm" @click="disconnectConn">Disconnect</button>
            <button class="base-btn base-btn--sm" @click="router.push({ name: 'connections' })">Manage connections</button>
          </div>
        </section>

        <div v-if="!bucketConnections.length" class="page-card cs-empty">
          <div class="cs-empty-icon">☁️</div>
          <h2>No object storage connections</h2>
          <p>Cloud Storage browses your existing AWS S3, GCP Storage, Alibaba OSS, or Huawei OBS connections.</p>
          <button class="base-btn base-btn--primary" @click="router.push({ name: 'connections' })">Add a connection</button>
        </div>

        <template v-else-if="connId !== null">
          <!-- Toolbar + breadcrumbs -->
          <div class="cs-bar">
            <button class="base-btn base-btn--sm" :disabled="cwd === ''" @click="up">↑ Up</button>
            <nav class="cs-crumbs">
              <button class="cs-crumb" @click="loadDir('')">/</button>
              <template v-for="(c, i) in crumbs" :key="c.prefix">
                <span class="cs-sep">/</span>
                <button class="cs-crumb" :class="{ 'cs-crumb--last': i === crumbs.length - 1 }" @click="loadDir(c.prefix)">{{ c.label }}</button>
              </template>
            </nav>
            <button class="icon-btn" title="Copy current prefix" @click="copyPath(cwd)">
              <ActionIcon name="copy" />
            </button>
            <div class="dk-spacer"></div>
            <input
              v-model="search"
              class="base-input cs-search"
              type="search"
              placeholder="Filter files…"
            />
            <button v-if="canManage" class="base-btn base-btn--sm" @click="newFolder">+ Folder</button>
            <label v-if="canManage" class="base-btn base-btn--primary base-btn--sm cs-upload-btn">
              ↑ Upload<input type="file" hidden multiple @change="onFilePick" />
            </label>
          </div>

          <div v-if="truncated" class="cs-truncated-hint">Showing the first 5,000 items in this folder — narrow the prefix to see more.</div>

          <!-- Bulk selection bar -->
          <div v-if="selected.size > 0" class="cs-bulk-bar">
            <span>{{ selected.size }} selected</span>
            <button class="base-btn base-btn--sm base-btn--primary" @click="downloadSelected">↓ Download ZIP</button>
            <button class="base-btn base-btn--sm" @click="selected = new Set()">Clear</button>
          </div>

          <!-- File list with drag-drop -->
          <div
            class="page-card cs-list"
            :class="{ 'cs-list--drag': dragOver }"
            @dragover.prevent="dragOver = canManage"
            @dragleave="dragOver = false"
            @drop.prevent="onDrop"
          >
            <div v-if="dragOver" class="cs-drop-hint">Drop files to upload to <b>/{{ cwd }}</b></div>
            <table class="cs-table">
              <thead>
                <tr>
                  <th class="cs-col-check"><input type="checkbox" :checked="allSelected" @change="toggleSelectAll" /></th>
                  <th class="cs-col-name" :class="{ sorted: sortKey === 'name' }" @click="toggleSort('name')">Name <SortIcon :active="sortKey === 'name'" :dir="sortDir" /></th>
                  <th class="cs-col-size" :class="{ sorted: sortKey === 'size' }" @click="toggleSort('size')">Size <SortIcon :active="sortKey === 'size'" :dir="sortDir" /></th>
                  <th class="cs-col-time" :class="{ sorted: sortKey === 'lastModified' }" @click="toggleSort('lastModified')">Modified <SortIcon :active="sortKey === 'lastModified'" :dir="sortDir" /></th>
                  <th class="cs-col-act"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="loading"><td colspan="5" class="cs-msg">Loading…</td></tr>
                <tr v-else-if="connError"><td colspan="5" class="cs-msg cs-err">{{ connError }}</td></tr>
                <tr v-else-if="!entries.length">
                  <td colspan="5" class="cs-dir-empty">
                    <span class="cs-dir-empty-icon">☁️</span>
                    <span class="cs-dir-empty-title">Empty folder</span>
                    <span class="cs-dir-empty-hint">Upload a file or create a folder to get started.</span>
                  </td>
                </tr>
                <tr v-else-if="!filteredEntries.length"><td colspan="5" class="cs-msg">No files match "{{ search }}".</td></tr>
                <tr v-for="e in filteredEntries" :key="e.name" class="cs-row" :class="{ 'cs-row--dir': e.isDir }">
                  <td class="cs-col-check" @click.stop>
                    <input type="checkbox" :checked="selected.has(e.name)" @change="toggleSelect(e.name)" />
                  </td>
                  <td class="cs-name" @click="e.isDir ? open(e) : undefined">
                    <span class="cs-icon">{{ fileIcon(e) }}</span>
                    <span class="cs-fname">{{ e.name }}</span>
                  </td>
                  <td class="cs-size">{{ e.isDir ? '—' : formatBytes(e.size) }}</td>
                  <td class="cs-time">{{ formatTime(e.lastModified) }}</td>
                  <td class="cs-act">
                    <RowActionsMenu :actions="rowActions(e)" />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>

        <!-- Idle: connections exist but none selected (after disconnect) -->
        <div v-else class="page-card cs-idle">
          <div class="cs-idle-icon">☁️</div>
          <p>Select a bucket from the dropdown above to connect.</p>
        </div>
      </div>
    </div>

    <!-- New folder modal -->
    <div v-if="showNewFolder" class="cs-modal-backdrop" @click.self="showNewFolder = false">
      <div class="cs-modal page-card">
        <div class="cs-modal-title">New folder</div>
        <input v-model="folderName" class="base-input cs-modal-input" placeholder="folder name" @keyup.enter="submitNewFolder" />
        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="showNewFolder = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!folderName.trim()" @click="submitNewFolder">Create</button>
        </div>
      </div>
    </div>

    <!-- Rename modal -->
    <div v-if="showRename" class="cs-modal-backdrop" @click.self="showRename = false">
      <div class="cs-modal page-card">
        <div class="cs-modal-title">Rename</div>
        <input v-model="renameName" class="base-input cs-modal-input" @keyup.enter="submitRename" />
        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="showRename = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!renameName.trim()" @click="submitRename">Rename</button>
        </div>
      </div>
    </div>

    <!-- Upload progress dock -->
    <div v-if="uploads.length" class="cs-uploads">
      <div class="cs-uploads-head">Uploads</div>
      <div v-for="u in uploads" :key="u.id" class="cs-up">
        <div class="cs-up-row">
          <span class="cs-up-name">{{ u.name }}</span>
          <span v-if="u.status === 'error'" class="cs-err">{{ u.error }}</span>
          <span v-else-if="u.status === 'done'" class="cs-up-done">✓ done</span>
          <span v-else-if="u.status === 'cancelled'" class="cs-up-meta">Cancelled</span>
          <span v-else-if="u.status === 'paused'" class="cs-up-meta">Paused · {{ u.pct }}%</span>
          <span v-else class="cs-up-meta">{{ u.pct }}% · {{ formatBytes(u.speed) }}/s</span>
        </div>
        <div class="cs-up-bar"><div class="cs-up-fill" :class="{ 'cs-up-fill--err': u.status === 'error', 'cs-up-fill--paused': u.status === 'paused' }" :style="{ width: u.pct + '%' }"></div></div>
        <div class="cs-up-actions">
          <button v-if="u.status === 'uploading'" class="cs-up-btn" @click="pauseUpload(u)">Pause</button>
          <button v-if="u.status === 'paused'" class="cs-up-btn" @click="resumeUpload(u)">Resume</button>
          <button v-if="u.status === 'uploading' || u.status === 'paused'" class="cs-up-btn cs-up-btn--danger" @click="cancelUpload(u)">Cancel</button>
          <button v-if="u.status === 'error' || u.status === 'cancelled'" class="cs-up-btn" @click="uploads = uploads.filter((j) => j.id !== u.id)">Dismiss</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Connection status badge */
.cs-conn-status { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 600; border: 1px solid transparent; }
.cs-conn-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.cs-conn-status--connected { color: var(--success); background: var(--success-bg, rgba(34,197,94,0.12)); }
.cs-conn-status--error { color: var(--danger); background: var(--danger-bg, rgba(239,68,68,0.12)); }
.cs-conn-status--connecting { color: var(--text-muted); background: var(--bg-hover); }
.cs-conn-status--connecting .cs-conn-dot { animation: cs-blink 1s ease-in-out infinite; }
.cs-conn-status--unknown { color: var(--text-muted); background: var(--bg-hover); }
@keyframes cs-blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.cs-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.cs-idle-icon { font-size: 22px; }
.cs-empty { text-align: center; padding: 64px 20px; color: var(--text-secondary); }
.cs-empty-icon { font-size: 44px; }
.cs-empty h2 { margin: 12px 0 4px; font-size: 16px; color: var(--text-primary); }
.cs-empty p { font-size: 13px; color: var(--text-muted); }

.cs-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.cs-crumbs { display: flex; align-items: center; gap: 2px; flex-wrap: wrap; font-size: 13px; min-width: 0; }
.cs-crumb { background: none; border: none; color: var(--brand); cursor: pointer; padding: 2px 4px; border-radius: var(--r-xs); font-family: var(--mono); font-size: 12px; }
.cs-crumb:hover { background: var(--bg-hover); }
.cs-crumb--last { color: var(--text-primary); font-weight: 600; }
.cs-sep { color: var(--text-muted); }
.cs-upload-btn { cursor: pointer; }
.cs-search { width: 200px; max-width: 40vw; }

.cs-truncated-hint { font-size: 12px; color: var(--text-muted); padding: 2px 2px 0; }

.cs-bulk-bar { display: flex; align-items: center; gap: 10px; padding: 8px 14px; background: var(--brand-dim); border: 1px solid var(--brand); border-radius: var(--r-sm); font-size: 13px; color: var(--text-primary); }
.cs-col-check { width: 1%; padding: 8px 4px 8px 12px !important; }

.cs-list { position: relative; padding: 4px 6px; overflow: hidden; }
.cs-list--drag { outline: 2px dashed var(--brand); outline-offset: -4px; }
.cs-drop-hint { position: absolute; inset: 0; z-index: 2; display: flex; align-items: center; justify-content: center; background: var(--brand-dim); color: var(--brand); font-size: 14px; pointer-events: none; }

.cs-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.cs-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.cs-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.cs-table tbody tr:last-child td { border-bottom: none; }
.cs-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.cs-err { color: var(--danger); }
.cs-dir-empty { text-align: center; padding: 36px 20px; }
.cs-dir-empty-icon { display: block; font-size: 26px; margin-bottom: 8px; opacity: 0.7; }
.cs-dir-empty-title { display: block; font-size: 13px; font-weight: 600; color: var(--text-secondary); }
.cs-dir-empty-hint { display: block; font-size: 12px; color: var(--text-muted); margin-top: 3px; }
.cs-row:hover { background: var(--bg-hover); }
.cs-name { display: flex; align-items: center; gap: 9px; cursor: pointer; }
.cs-row--dir .cs-fname { color: var(--brand); font-weight: 500; }
.cs-icon { font-size: 15px; width: 18px; text-align: center; }
.cs-fname { word-break: break-all; }
.cs-size { color: var(--text-secondary); white-space: nowrap; }
.cs-time { color: var(--text-muted); font-size: 12px; white-space: nowrap; }
.cs-act { text-align: right; white-space: nowrap; }
.cs-col-size, .cs-col-time { width: 1%; white-space: nowrap; }
.cs-col-size { min-width: 70px; }
.cs-col-time { min-width: 140px; }
.cs-col-name, .cs-col-size, .cs-col-time { cursor: pointer; user-select: none; }

.cs-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.cs-modal { width: 380px; max-width: 92vw; padding: 20px; }
.cs-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; word-break: break-all; }
.cs-modal-input { width: 100%; }
.cs-modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }

.cs-uploads { position: fixed; right: 18px; bottom: 18px; width: 320px; max-height: 50vh; overflow-y: auto; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); z-index: 90; padding: 10px 12px; }
.cs-uploads-head { font-size: 12px; font-weight: 600; color: var(--text-primary); margin-bottom: 8px; }
.cs-up { margin-bottom: 8px; }
.cs-up-row { display: flex; justify-content: space-between; gap: 8px; font-size: 11px; margin-bottom: 3px; }
.cs-up-name { color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.cs-up-meta { color: var(--text-muted); white-space: nowrap; font-family: var(--mono); }
.cs-up-done { color: var(--success); }
.cs-up-bar { height: 5px; background: var(--bg-hover); border-radius: 99px; overflow: hidden; }
.cs-up-fill { height: 100%; background: var(--brand); transition: width 0.2s ease; }
.cs-up-fill--err { background: var(--danger); }
.cs-up-fill--paused { background: var(--text-muted); }
.cs-up-actions { display: flex; justify-content: flex-end; gap: 6px; margin-top: 4px; }
.cs-up-btn { background: none; border: none; padding: 0; font-size: 11px; color: var(--brand); cursor: pointer; }
.cs-up-btn:hover { text-decoration: underline; }
.cs-up-btn--danger { color: var(--danger); }
</style>
