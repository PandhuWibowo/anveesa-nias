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

// Transfer — mirrors server/handlers/cloud_storage_transfer.go's transferJobView
interface TransferDestSummary { connection_id: number; conn_name: string; prefix: string }
interface TransferItemResult { source_key: string; dest_connection_id: number; dest_key: string; status: 'done' | 'failed' | 'skipped'; bytes?: number; error?: string }
interface TransferJobView {
  id: string
  status: 'running' | 'done' | 'partial' | 'failed' | 'canceled'
  started_at: string
  done_at?: string
  mode: 'copy' | 'move'
  conflict_policy: 'overwrite' | 'skip'
  source_conn_name: string
  destinations: TransferDestSummary[]
  object_count: number
  total_items: number
  completed_items: number
  failed_items: number
  skipped_items: number
  total_bytes?: number
  transferred_bytes: number
  moved_source_objects?: number
  error?: string
  current_item?: string
  results: TransferItemResult[]
}
interface TransferDestChoice { connectionId: number; prefix: string; checked: boolean }

// Move — mirrors server/handlers/cloud_storage_move.go's moveJobView. Same
// job/poll pattern as Transfer, but scoped to one connection (no fan-out).
interface MoveItemResult { source_key: string; dest_key: string; status: 'done' | 'failed'; error?: string }
interface MoveJobView {
  id: string
  status: 'running' | 'done' | 'partial' | 'failed' | 'canceled'
  started_at: string
  done_at?: string
  connection_id: number
  dest_folder: string
  object_count: number
  total_items: number
  completed_items: number
  failed_items: number
  error?: string
  current_item?: string
  results: MoveItemResult[]
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

// Preview — images/video/audio/PDF render via native elements pointed at the
// download URL (no fetch needed); text/JSON/CSV fetch content through the
// read endpoint and render client-side. Excel/Word are intentionally not
// supported (would need new parsing dependencies) — falls through to
// "unsupported".
type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'json' | 'csv' | 'text' | 'unsupported'
const showPreview = ref(false)
const previewEntry = ref<CsEntry | null>(null)
const previewKind = ref<PreviewKind>('text')
const previewLoading = ref(false)
const previewError = ref('')
const previewContent = ref('')
const previewCsvRows = ref<string[][]>([])
const previewTruncated = ref(false)
const previewBinary = ref(false)

// Transfer — copy/move selected files/folders to one or more other bucket
// connections (possibly a different cloud provider entirely), run as a
// background job polled the same way BackupView.vue polls restore jobs.
const showTransfer = ref(false)
const transferItems = ref<CsEntry[]>([])
const transferMode = ref<'copy' | 'move'>('copy')
const transferConflictPolicy = ref<'overwrite' | 'skip'>('overwrite')
const transferDestChoices = ref<TransferDestChoice[]>([])
const transferDestCount = computed(() => transferDestChoices.value.filter((d) => d.checked).length)
const transferSubmitting = ref(false)
const transferError = ref('')

const showTransferProgress = ref(false)
const transferJobId = ref<string | null>(null)
const transferJob = ref<TransferJobView | null>(null)
const TRANSFER_JOB_KEY = 'nias:cloudstorage:activeTransferJob'

// Metadata editor — S3 has no in-place metadata edit; the backend implements
// "save" as a copy-to-self with x-amz-metadata-directive: REPLACE.
const showMetadata = ref(false)
const metadataEntry = ref<CsEntry | null>(null)
const metadataLoading = ref(false)
const metadataSaving = ref(false)
const metadataError = ref('')
const metadataContentType = ref('')
const metadataCacheControl = ref('')
const metadataRows = ref<{ key: string; val: string }[]>([])
const metadataReadOnly = ref({ size: 0, etag: '', lastModified: '' })

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
function downloadUrl(entry: CsEntry): string {
  const token = localStorage.getItem('nias-token') || ''
  return `/api/connections/${connId.value}/storage/download?key=${encodeURIComponent(entry.key)}&token=${encodeURIComponent(token)}`
}
function download(entry: CsEntry) {
  const a = document.createElement('a')
  a.href = downloadUrl(entry)
  a.download = entry.name
  a.click()
}

// ── Preview ───────────────────────────────────────────────────────
const previewUrl = computed(() => previewEntry.value ? downloadUrl(previewEntry.value) : '')

function detectPreviewKind(name: string): PreviewKind {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'bmp', 'ico'].includes(ext)) return 'image'
  if (['mp4', 'webm', 'mov', 'm4v'].includes(ext)) return 'video'
  if (['mp3', 'wav', 'ogg', 'm4a', 'flac'].includes(ext)) return 'audio'
  if (ext === 'pdf') return 'pdf'
  if (ext === 'json') return 'json'
  if (['csv', 'tsv'].includes(ext)) return 'csv'
  if (['txt', 'md', 'log', 'yml', 'yaml', 'toml', 'env', 'conf', 'ini', 'sh', 'bash', 'zsh', 'sql', 'xml', 'html', 'css', 'js', 'ts', 'vue', 'go', 'py', 'java', 'rb', 'rs', 'c', 'h', 'cpp'].includes(ext)) return 'text'
  return 'unsupported'
}

// Splits on the first row's delimiter (comma vs tab) — no quoted-field
// escaping, matches this view's "simple client-side preview" scope rather
// than a full CSV parser.
function parseCsvPreview(text: string): string[][] {
  const lines = text.split(/\r\n|\n/).filter((l) => l.length > 0).slice(0, 500)
  const delim = lines[0]?.includes('\t') ? '\t' : ','
  return lines.map((line) => line.split(delim))
}

async function openPreview(entry: CsEntry) {
  previewEntry.value = entry
  showPreview.value = true
  previewError.value = ''
  previewContent.value = ''
  previewCsvRows.value = []
  previewTruncated.value = false
  previewBinary.value = false
  const kind = detectPreviewKind(entry.name)
  previewKind.value = kind
  // image/video/audio/pdf render straight from the download URL — no fetch
  if (kind === 'image' || kind === 'video' || kind === 'audio' || kind === 'pdf' || kind === 'unsupported') return

  previewLoading.value = true
  try {
    const { data } = await axios.get<{ content: string; size: number; truncated: boolean; binary: boolean }>(
      `/api/connections/${connId.value}/storage/read`,
      { params: { key: entry.key } },
    )
    previewTruncated.value = data.truncated
    previewBinary.value = data.binary
    if (data.binary) return
    if (kind === 'json') {
      try {
        previewContent.value = JSON.stringify(JSON.parse(data.content), null, 2)
      } catch {
        previewContent.value = data.content
      }
    } else if (kind === 'csv') {
      previewCsvRows.value = parseCsvPreview(data.content)
    } else {
      previewContent.value = data.content
    }
  } catch (e: any) {
    previewError.value = e?.response?.data?.error || 'Could not read file'
  } finally {
    previewLoading.value = false
  }
}
function closePreview() {
  showPreview.value = false
  previewEntry.value = null
}

// ── Metadata editor ─────────────────────────────────────────────
function addMetaRow() {
  metadataRows.value.push({ key: '', val: '' })
}
async function openMetadata(entry: CsEntry) {
  metadataEntry.value = entry
  showMetadata.value = true
  metadataError.value = ''
  metadataContentType.value = ''
  metadataCacheControl.value = ''
  metadataRows.value = []
  metadataLoading.value = true
  try {
    const { data } = await axios.get<{
      content_type: string
      cache_control: string
      size: number
      etag: string
      last_modified: string
      metadata: Record<string, string>
    }>(`/api/connections/${connId.value}/storage/metadata`, { params: { key: entry.key } })
    metadataContentType.value = data.content_type || ''
    metadataCacheControl.value = data.cache_control || ''
    metadataRows.value = Object.entries(data.metadata || {}).map(([key, val]) => ({ key, val }))
    metadataReadOnly.value = { size: data.size, etag: data.etag, lastModified: data.last_modified }
  } catch (e: any) {
    metadataError.value = e?.response?.data?.error || 'Could not load metadata'
  } finally {
    metadataLoading.value = false
  }
}
function closeMetadata() {
  showMetadata.value = false
  metadataEntry.value = null
}
async function saveMetadata() {
  if (!metadataEntry.value) return
  metadataSaving.value = true
  try {
    const metadata: Record<string, string> = {}
    for (const row of metadataRows.value) {
      const key = row.key.trim()
      if (key) metadata[key] = row.val
    }
    await axios.post(`/api/connections/${connId.value}/storage/metadata`, {
      key: metadataEntry.value.key,
      content_type: metadataContentType.value,
      cache_control: metadataCacheControl.value,
      metadata,
    })
    toast.success('Metadata updated')
    closeMetadata()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update metadata')
  } finally {
    metadataSaving.value = false
  }
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

// ── Transfer ────────────────────────────────────────────────────
function connectionLabel(id: number): string {
  const c = connections.value.find((x) => x.id === id)
  return c ? `${c.name} (${c.database || c.driver})` : `#${id}`
}
function openTransfer(items: CsEntry[]) {
  if (!items.length) return
  transferItems.value = items
  transferMode.value = 'copy'
  transferConflictPolicy.value = 'overwrite'
  transferError.value = ''
  transferDestChoices.value = bucketConnections.value
    .filter((c) => c.id !== connId.value)
    .map((c) => ({ connectionId: c.id, prefix: cwd.value.replace(/\/$/, ''), checked: false }))
  showTransfer.value = true
}
function transferSelected() {
  openTransfer(filteredEntries.value.filter((e) => selected.value.has(e.name)))
}
async function submitTransfer() {
  const destinations = transferDestChoices.value
    .filter((d) => d.checked)
    .map((d) => ({ connectionId: d.connectionId, prefix: d.prefix.trim() }))
  if (!destinations.length) {
    transferError.value = 'Pick at least one destination'
    return
  }
  transferSubmitting.value = true
  transferError.value = ''
  try {
    const { data } = await axios.post<{ job_id: string }>(`/api/connections/${connId.value}/storage/transfer`, {
      items: transferItems.value.map((e) => e.key),
      destinations,
      mode: transferMode.value,
      conflictPolicy: transferConflictPolicy.value,
    })
    showTransfer.value = false
    startTransferPolling(data.job_id)
  } catch (e: any) {
    transferError.value = e?.response?.data?.error || 'Failed to start transfer'
  } finally {
    transferSubmitting.value = false
  }
}

function saveActiveTransferJob(id: string) {
  localStorage.setItem(TRANSFER_JOB_KEY, id)
}
function clearActiveTransferJob() {
  localStorage.removeItem(TRANSFER_JOB_KEY)
}
function startTransferPolling(jobId: string) {
  transferJobId.value = jobId
  transferJob.value = null
  saveActiveTransferJob(jobId)
  showTransferProgress.value = true
  pollTransferJob(jobId)
}
async function pollTransferJob(jobId: string) {
  while (transferJobId.value === jobId) {
    try {
      const { data } = await axios.get<TransferJobView>(`/api/storage/transfer-jobs/${jobId}`)
      if (transferJobId.value !== jobId) return
      transferJob.value = data
      if (data.status !== 'running') {
        clearActiveTransferJob()
        if (data.status === 'done') toast.success('Transfer complete')
        else if (data.status === 'partial') toast.error(`Transfer finished — ${data.failed_items} failed, ${data.skipped_items} skipped`)
        else if (data.status === 'canceled') toast.error('Transfer canceled')
        else toast.error(data.error || 'Transfer failed')
        await loadDir(cwd.value)
        return
      }
    } catch {
      clearActiveTransferJob()
      return
    }
    await new Promise((r) => setTimeout(r, 1500))
  }
}
async function cancelTransfer() {
  if (!transferJobId.value) return
  try {
    await axios.delete(`/api/storage/transfer-jobs/${transferJobId.value}`)
  } catch {
    // best-effort — the poller will still pick up the final status
  }
}
function closeTransferProgress() {
  showTransferProgress.value = false
  transferJobId.value = null
  transferJob.value = null
}
const transferProgressPct = computed(() => {
  const j = transferJob.value
  if (!j || !j.total_items) return 0
  return Math.round(((j.completed_items + j.failed_items + j.skipped_items) / j.total_items) * 100)
})

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
// ── Move to folder (bulk, same connection) ──────────────────────
// Backed by a background job (server/handlers/cloud_storage_move.go), not a
// synchronous loop — moving a folder with hundreds of objects means
// hundreds of sequential S3 copy round-trips, which against a real cloud
// provider can easily exceed a browser/reverse-proxy timeout well before
// finishing. A job that returns immediately and gets polled (same pattern
// as Transfer below) survives that; a blocking request per item doesn't —
// it dies mid-copy, before the originals are ever deleted, so the move
// silently never happens.
const showMoveTo = ref(false)
const moveToItems = ref<CsEntry[]>([])
const moveToDestFolder = ref('')
const moveToSubmitting = ref(false)
const moveToError = ref('')

const showMoveProgress = ref(false)
const moveJobId = ref<string | null>(null)
const moveJob = ref<MoveJobView | null>(null)
const MOVE_JOB_KEY = 'nias:cloudstorage:activeMoveJob'

function openMoveTo(items: CsEntry[]) {
  if (!items.length) return
  moveToItems.value = items
  moveToDestFolder.value = parentPrefix(cwd.value).replace(/\/$/, '')
  moveToError.value = ''
  showMoveTo.value = true
}
function moveSelected() {
  openMoveTo(filteredEntries.value.filter((e) => selected.value.has(e.name)))
}
async function submitMoveTo() {
  const destFolder = moveToDestFolder.value.trim().replace(/^\/+|\/+$/g, '')
  if (!destFolder) {
    moveToError.value = 'Destination folder is required'
    return
  }
  moveToSubmitting.value = true
  moveToError.value = ''
  try {
    const { data } = await axios.post<{ job_id: string }>(`/api/connections/${connId.value}/storage/move`, {
      items: moveToItems.value.map((e) => e.key),
      destFolder,
    })
    showMoveTo.value = false
    startMovePolling(data.job_id)
  } catch (e: any) {
    moveToError.value = e?.response?.data?.error || 'Failed to start move'
  } finally {
    moveToSubmitting.value = false
  }
}

function saveActiveMoveJob(id: string) {
  localStorage.setItem(MOVE_JOB_KEY, id)
}
function clearActiveMoveJob() {
  localStorage.removeItem(MOVE_JOB_KEY)
}
function startMovePolling(jobId: string) {
  moveJobId.value = jobId
  moveJob.value = null
  saveActiveMoveJob(jobId)
  showMoveProgress.value = true
  pollMoveJob(jobId)
}
async function pollMoveJob(jobId: string) {
  while (moveJobId.value === jobId) {
    try {
      const { data } = await axios.get<MoveJobView>(`/api/storage/move-jobs/${jobId}`)
      if (moveJobId.value !== jobId) return
      moveJob.value = data
      if (data.status !== 'running') {
        clearActiveMoveJob()
        if (data.status === 'done') toast.success(`Moved ${data.completed_items} item${data.completed_items === 1 ? '' : 's'} to ${data.dest_folder}/`)
        else if (data.status === 'partial') toast.error(`Move finished — ${data.failed_items} failed`)
        else if (data.status === 'canceled') toast.error('Move canceled')
        else toast.error(data.error || 'Move failed')
        selected.value = new Set()
        await loadDir(cwd.value)
        return
      }
    } catch {
      clearActiveMoveJob()
      return
    }
    await new Promise((r) => setTimeout(r, 1500))
  }
}
async function cancelMove() {
  if (!moveJobId.value) return
  try {
    await axios.delete(`/api/storage/move-jobs/${moveJobId.value}`)
  } catch {
    // best-effort — the poller will still pick up the final status
  }
}
function closeMoveProgress() {
  showMoveProgress.value = false
  moveJobId.value = null
  moveJob.value = null
}
const moveProgressPct = computed(() => {
  const j = moveJob.value
  if (!j || !j.total_items) return 0
  return Math.round(((j.completed_items + j.failed_items) / j.total_items) * 100)
})

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
    actions.push({ key: 'view', label: 'View', icon: 'view', primary: true, onClick: () => openPreview(e) })
    actions.push({ key: 'download', label: 'Download', icon: 'download', primary: true, onClick: () => download(e) })
  } else {
    actions.push({ key: 'download-zip', label: 'Download as ZIP', icon: 'download', primary: true, onClick: () => downloadZip([e]) })
  }
  actions.push({ key: 'copy-path', label: 'Copy key', icon: 'copy', onClick: () => copyPath(e.key) })
  if (!e.isDir) {
    actions.push({ key: 'info', label: 'Metadata…', icon: 'info', onClick: () => openMetadata(e) })
  }
  if (canManage.value) {
    actions.push({ key: 'transfer', label: 'Transfer…', icon: 'move', onClick: () => openTransfer([e]) })
    actions.push({ key: 'move-to', label: 'Move to folder…', icon: 'move', onClick: () => openMoveTo([e]) })
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

onMounted(() => {
  loadConnections()
  const activeJobId = localStorage.getItem(TRANSFER_JOB_KEY)
  if (activeJobId) {
    transferJobId.value = activeJobId
    showTransferProgress.value = true
    pollTransferJob(activeJobId)
  }
  const activeMoveJobId = localStorage.getItem(MOVE_JOB_KEY)
  if (activeMoveJobId) {
    moveJobId.value = activeMoveJobId
    showMoveProgress.value = true
    pollMoveJob(activeMoveJobId)
  }
})
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
            <button v-if="canManage" class="base-btn base-btn--sm" @click="transferSelected">⇄ Transfer</button>
            <button v-if="canManage" class="base-btn base-btn--sm" @click="moveSelected">→ Move to folder</button>
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
                  <td class="cs-name" @click="e.isDir ? open(e) : openPreview(e)">
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

    <!-- Move to folder modal -->
    <div v-if="showMoveTo" class="cs-modal-backdrop" @click.self="showMoveTo = false">
      <div class="cs-modal page-card">
        <div class="cs-modal-title">Move {{ moveToItems.length }} item{{ moveToItems.length === 1 ? '' : 's' }} to…</div>

        <div class="cs-transfer-source">
          <div v-for="e in moveToItems.slice(0, 8)" :key="e.key" class="cs-transfer-source-item">
            <span class="cs-icon">{{ fileIcon(e) }}</span>{{ e.key }}
          </div>
          <div v-if="moveToItems.length > 8" class="cs-transfer-more">+{{ moveToItems.length - 8 }} more</div>
        </div>

        <label class="form-label">Destination folder (same bucket)</label>
        <input v-model="moveToDestFolder" class="base-input cs-modal-input" placeholder="e.g. b2blocal" @keyup.enter="submitMoveTo" />
        <p class="cs-meta-empty" style="margin-top:6px">Items keep their own name — only the parent folder changes. Overwrites anything already at the destination with the same name.</p>

        <div v-if="moveToError" class="cs-msg cs-err">{{ moveToError }}</div>

        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="showMoveTo = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="moveToSubmitting || !moveToDestFolder.trim()" @click="submitMoveTo">
            {{ moveToSubmitting ? 'Moving…' : 'Move' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Transfer modal -->
    <div v-if="showTransfer" class="cs-modal-backdrop" @click.self="showTransfer = false">
      <div class="cs-modal cs-transfer-modal page-card">
        <div class="cs-modal-title">Transfer {{ transferItems.length }} item{{ transferItems.length === 1 ? '' : 's' }}</div>

        <div class="cs-transfer-source">
          <div v-for="e in transferItems.slice(0, 8)" :key="e.key" class="cs-transfer-source-item">
            <span class="cs-icon">{{ fileIcon(e) }}</span>{{ e.key }}
          </div>
          <div v-if="transferItems.length > 8" class="cs-transfer-more">+{{ transferItems.length - 8 }} more</div>
        </div>

        <div class="form-group">
          <label class="form-label">Mode</label>
          <div class="cs-transfer-radios">
            <label><input type="radio" value="copy" v-model="transferMode" /> Copy (keep source)</label>
            <label><input type="radio" value="move" v-model="transferMode" /> Move (delete source once every destination succeeds)</label>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">If the destination already has the file</label>
          <div class="cs-transfer-radios">
            <label><input type="radio" value="overwrite" v-model="transferConflictPolicy" /> Overwrite</label>
            <label><input type="radio" value="skip" v-model="transferConflictPolicy" /> Skip</label>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Destinations</label>
          <div v-if="!transferDestChoices.length" class="cs-msg">No other bucket connections available — add one under Manage connections.</div>
          <div v-for="d in transferDestChoices" :key="d.connectionId" class="cs-transfer-dest">
            <label class="cs-transfer-dest-check">
              <input type="checkbox" v-model="d.checked" />
              <span>{{ connectionLabel(d.connectionId) }}</span>
            </label>
            <input v-if="d.checked" v-model="d.prefix" class="base-input cs-transfer-dest-prefix" placeholder="destination folder (optional)" />
          </div>
        </div>

        <div v-if="transferError" class="cs-msg cs-err">{{ transferError }}</div>

        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="showTransfer = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="transferSubmitting || !transferDestCount" @click="submitTransfer">
            {{ transferSubmitting ? 'Starting…' : 'Start Transfer' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Preview modal -->
    <div v-if="showPreview" class="cs-modal-backdrop" @click.self="closePreview">
      <div class="cs-modal cs-view-modal page-card">
        <div class="cs-modal-title">{{ previewEntry?.name }}</div>

        <div v-if="previewKind === 'image'" class="cs-preview-media">
          <img :src="previewUrl" :alt="previewEntry?.name" />
        </div>
        <div v-else-if="previewKind === 'video'" class="cs-preview-media">
          <video :src="previewUrl" controls />
        </div>
        <div v-else-if="previewKind === 'audio'" class="cs-preview-media cs-preview-media--audio">
          <audio :src="previewUrl" controls />
        </div>
        <div v-else-if="previewKind === 'pdf'" class="cs-preview-pdf">
          <iframe :src="previewUrl" title="PDF preview" />
        </div>
        <div v-else-if="previewKind === 'unsupported'" class="cs-msg">This file type can't be previewed here. Use Download instead.</div>
        <div v-else-if="previewLoading" class="cs-msg">Loading…</div>
        <div v-else-if="previewError" class="cs-msg cs-err">{{ previewError }}</div>
        <div v-else-if="previewBinary" class="cs-msg">This file looks binary and can't be previewed. Use Download instead.</div>
        <template v-else-if="previewKind === 'csv'">
          <div v-if="previewTruncated" class="cs-view-notice">Showing the first 2 MB of this file.</div>
          <div class="cs-csv-wrap">
            <table class="cs-csv-table">
              <tbody>
                <tr v-for="(row, i) in previewCsvRows" :key="i" :class="{ 'cs-csv-header': i === 0 }">
                  <td v-for="(cell, j) in row" :key="j">{{ cell }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
        <template v-else>
          <div v-if="previewTruncated" class="cs-view-notice">Showing the first 2 MB of this file.</div>
          <pre class="cs-view-content">{{ previewContent }}</pre>
        </template>

        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="previewEntry && download(previewEntry)">Download</button>
          <button class="base-btn base-btn--sm" @click="closePreview">Close</button>
        </div>
      </div>
    </div>

    <!-- Metadata modal -->
    <div v-if="showMetadata" class="cs-modal-backdrop" @click.self="closeMetadata">
      <div class="cs-modal cs-meta-modal page-card">
        <div class="cs-modal-title">{{ metadataEntry?.name }} — Metadata</div>

        <div v-if="metadataLoading" class="cs-msg">Loading…</div>
        <div v-else-if="metadataError" class="cs-msg cs-err">{{ metadataError }}</div>
        <template v-else>
          <div class="form-group">
            <label class="form-label">Content-Type</label>
            <input v-model="metadataContentType" class="base-input" placeholder="e.g. image/png" />
          </div>
          <div class="form-group">
            <label class="form-label">Cache-Control</label>
            <input v-model="metadataCacheControl" class="base-input" placeholder="e.g. public, max-age=31536000" />
          </div>
          <div class="form-group">
            <div class="cs-meta-rows-head">
              <label class="form-label" style="margin-bottom:0">Custom Metadata</label>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="addMetaRow">+ Add</button>
            </div>
            <div v-for="(row, i) in metadataRows" :key="i" class="cs-meta-row">
              <input v-model="row.key" class="base-input" placeholder="key" />
              <input v-model="row.val" class="base-input" placeholder="value" />
              <button class="cs-meta-row-del" @click="metadataRows.splice(i, 1)" title="Remove">
                <ActionIcon name="close" />
              </button>
            </div>
            <p v-if="!metadataRows.length" class="cs-meta-empty">No custom metadata.</p>
          </div>
          <div class="cs-meta-readonly">
            <div>Size: <strong>{{ formatBytes(metadataReadOnly.size) }}</strong></div>
            <div v-if="metadataReadOnly.etag">ETag: <strong>{{ metadataReadOnly.etag }}</strong></div>
            <div v-if="metadataReadOnly.lastModified">Modified: <strong>{{ formatTime(metadataReadOnly.lastModified) }}</strong></div>
          </div>
        </template>

        <div class="cs-modal-actions">
          <button class="base-btn base-btn--sm" @click="closeMetadata">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="metadataSaving || metadataLoading" @click="saveMetadata">
            {{ metadataSaving ? 'Saving…' : 'Save' }}
          </button>
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

    <!-- Transfer progress dock -->
    <div v-if="showTransferProgress && transferJob" class="cs-uploads cs-transfer-dock">
      <div class="cs-uploads-head">
        <span>Transfer {{ transferJob.status === 'running' ? 'in progress' : transferJob.status }}</span>
        <button class="cs-up-btn" @click="closeTransferProgress">✕</button>
      </div>
      <div class="cs-up-bar">
        <div
          class="cs-up-fill"
          :class="{ 'cs-up-fill--err': transferJob.status === 'failed' || transferJob.status === 'partial' }"
          :style="{ width: transferProgressPct + '%' }"
        ></div>
      </div>
      <div class="cs-transfer-progress-meta">
        {{ transferJob.completed_items }}/{{ transferJob.total_items }} done
        <span v-if="transferJob.failed_items">· {{ transferJob.failed_items }} failed</span>
        <span v-if="transferJob.skipped_items">· {{ transferJob.skipped_items }} skipped</span>
        · {{ formatBytes(transferJob.transferred_bytes) }}
      </div>
      <div v-if="transferJob.status === 'running' && transferJob.current_item" class="cs-transfer-current">{{ transferJob.current_item }}</div>
      <div class="cs-up-actions">
        <button v-if="transferJob.status === 'running'" class="cs-up-btn cs-up-btn--danger" @click="cancelTransfer">Cancel</button>
      </div>
      <div v-if="transferJob.status !== 'running' && transferJob.results.some((r) => r.status === 'failed')" class="cs-transfer-errors">
        <div v-for="(r, i) in transferJob.results.filter((r) => r.status === 'failed').slice(0, 5)" :key="i" class="cs-transfer-error-row">
          {{ r.source_key }}: {{ r.error }}
        </div>
      </div>
    </div>

    <!-- Move progress dock -->
    <div v-if="showMoveProgress && moveJob" class="cs-uploads cs-transfer-dock cs-move-dock">
      <div class="cs-uploads-head">
        <span>Move {{ moveJob.status === 'running' ? 'in progress' : moveJob.status }}</span>
        <button class="cs-up-btn" @click="closeMoveProgress">✕</button>
      </div>
      <div class="cs-up-bar">
        <div
          class="cs-up-fill"
          :class="{ 'cs-up-fill--err': moveJob.status === 'failed' || moveJob.status === 'partial' }"
          :style="{ width: moveProgressPct + '%' }"
        ></div>
      </div>
      <div class="cs-transfer-progress-meta">
        {{ moveJob.completed_items }}/{{ moveJob.total_items }} done
        <span v-if="moveJob.failed_items">· {{ moveJob.failed_items }} failed</span>
        · to {{ moveJob.dest_folder }}/
      </div>
      <div v-if="moveJob.status === 'running' && moveJob.current_item" class="cs-transfer-current">{{ moveJob.current_item }}</div>
      <div class="cs-up-actions">
        <button v-if="moveJob.status === 'running'" class="cs-up-btn cs-up-btn--danger" @click="cancelMove">Cancel</button>
      </div>
      <div v-if="moveJob.status !== 'running' && moveJob.results.some((r) => r.status === 'failed')" class="cs-transfer-errors">
        <div v-for="(r, i) in moveJob.results.filter((r) => r.status === 'failed').slice(0, 5)" :key="i" class="cs-transfer-error-row">
          {{ r.source_key }}: {{ r.error }}
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

.cs-view-modal { width: 820px; max-width: 92vw; max-height: 86vh; display: flex; flex-direction: column; }
.cs-view-notice { font-size: 12px; color: var(--text-muted); margin-bottom: 8px; }
.cs-view-content { flex: 1; overflow: auto; background: var(--bg-hover); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 12px; font-family: var(--mono); font-size: 12px; white-space: pre-wrap; word-break: break-all; margin: 0; }
.cs-preview-media { display: flex; align-items: center; justify-content: center; max-height: 70vh; overflow: auto; background: var(--bg-hover); border-radius: var(--r-sm); }
.cs-preview-media img { max-width: 100%; max-height: 70vh; object-fit: contain; }
.cs-preview-media video { max-width: 100%; max-height: 70vh; }
.cs-preview-media--audio { padding: 30px; }
.cs-preview-pdf { flex: 1; min-height: 60vh; }
.cs-preview-pdf iframe { width: 100%; height: 60vh; border: 1px solid var(--border); border-radius: var(--r-sm); }
.cs-csv-wrap { flex: 1; overflow: auto; border: 1px solid var(--border); border-radius: var(--r-sm); }
.cs-csv-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.cs-csv-table td { padding: 5px 8px; border-bottom: 1px solid var(--border); border-right: 1px solid var(--border); white-space: nowrap; }
.cs-csv-header td { font-weight: 600; background: var(--bg-hover); position: sticky; top: 0; }

.cs-meta-modal { width: 460px; max-width: 92vw; }
.cs-meta-rows-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.cs-meta-row { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; }
.cs-meta-row .base-input { font-size: 12px; padding: 6px 8px; }
.cs-meta-row-del { flex-shrink: 0; background: none; border: none; padding: 4px; color: var(--text-muted); cursor: pointer; border-radius: var(--r-xs); }
.cs-meta-row-del:hover { color: var(--danger); background: var(--bg-hover); }
.cs-meta-empty { font-size: 11px; color: var(--text-muted); margin: 0; }
.cs-meta-readonly { padding: 10px 12px; background: var(--bg-hover); border-radius: var(--r-sm); font-size: 11px; color: var(--text-muted); line-height: 1.8; }
.cs-meta-readonly strong { color: var(--text-secondary); font-weight: 500; word-break: break-all; }

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

/* Transfer modal */
.cs-transfer-modal { width: 480px; max-width: 92vw; }
.cs-transfer-source { max-height: 120px; overflow-y: auto; background: var(--bg-hover); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 8px 10px; margin-bottom: 14px; font-size: 12px; font-family: var(--mono); }
.cs-transfer-source-item { display: flex; align-items: center; gap: 6px; word-break: break-all; padding: 2px 0; }
.cs-transfer-more { color: var(--text-muted); padding: 2px 0; }
.cs-transfer-radios { display: flex; flex-direction: column; gap: 6px; font-size: 13px; }
.cs-transfer-radios label { display: flex; align-items: center; gap: 6px; cursor: pointer; }
.cs-transfer-dest { display: flex; flex-direction: column; gap: 6px; padding: 6px 0; border-bottom: 1px solid var(--border); }
.cs-transfer-dest:last-child { border-bottom: none; }
.cs-transfer-dest-check { display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; }
.cs-transfer-dest-prefix { margin-left: 24px; width: calc(100% - 24px); font-size: 12px; }

/* Transfer / Move progress docks */
.cs-transfer-dock { right: 352px; }
.cs-move-dock { right: 704px; }
.cs-transfer-dock .cs-uploads-head { display: flex; align-items: center; justify-content: space-between; }
.cs-transfer-progress-meta { font-size: 11px; color: var(--text-muted); margin-top: 6px; display: flex; gap: 6px; flex-wrap: wrap; }
.cs-transfer-current { font-size: 11px; color: var(--text-secondary); font-family: var(--mono); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 4px; }
.cs-transfer-errors { margin-top: 8px; border-top: 1px solid var(--border); padding-top: 6px; max-height: 100px; overflow-y: auto; }
.cs-transfer-error-row { font-size: 11px; color: var(--danger); word-break: break-all; padding: 2px 0; }
</style>
