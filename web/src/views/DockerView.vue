<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import axios from 'axios'
import SearchSelect from '@/components/ui/SearchSelect.vue'
import SortIcon from '@/components/ui/SortIcon.vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useListFilter } from '@/composables/useListFilter'
import { useSort } from '@/composables/useSort'

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
interface DockerPort {
  ip: string
  privatePort: number
  publicPort: number
  type: string
}
interface DockerContainer {
  id: string
  names: string[]
  image: string
  state: string
  status: string
  created: number
  ports: DockerPort[]
  labels?: Record<string, string>
}
interface DockerImage {
  id: string
  repoTags: string[]
  size: number
  created: number
}
interface DockerVolume {
  name: string
  driver: string
  mountpoint: string
  createdAt: string
  scope: string
  size: number
  refCount: number
}
interface NetConn {
  name: string
  ipv4: string
}
interface DockerNetwork {
  name: string
  id: string
  driver: string
  scope: string
  subnet: string
  containers: number
  connected: NetConn[]
}
interface DfCategory {
  count: number
  size: number
  reclaimable: number
}
interface DiskUsage {
  images: DfCategory
  containers: DfCategory
  volumes: DfCategory
  build_cache: DfCategory
}
interface TopoNode {
  id: string
  type: 'container' | 'network' | 'volume'
  label: string
  state?: string
  image?: string
  project?: string
  driver?: string
  ports?: string[]
}
interface TopoEdge {
  from: string
  to: string
  type: 'network' | 'volume'
  label?: string
}
interface ContainerStats {
  cpu_percent: number
  mem_usage: number
  mem_limit: number
  mem_percent: number
  net_rx: number
  net_tx: number
  blk_read: number
  blk_write: number
  pids: number
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
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['docker.manage']))
const canExec = computed(() => hasAnyPermission(['docker.exec']))

const hosts = ref<DockerHost[]>([])
const activeHostId = ref<number | null>(null)
const activeHost = computed(() => hosts.value.find((h) => h.id === activeHostId.value) ?? null)
const hostOptions = computed(() => hosts.value.map((h) => ({
  value: h.id,
  label: h.ssh_host ? `${h.name} (${h.ssh_host})` : `${h.name} (local)`,
})))

const tab = ref<'containers' | 'images' | 'volumes' | 'networks'>('containers')
const containers = ref<DockerContainer[]>([])
const images = ref<DockerImage[]>([])
const volumes = ref<DockerVolume[]>([])
const networks = ref<DockerNetwork[]>([])
const groupByCompose = ref(true)

// ── Multi-host overview ─────────────────────────────────────────
const overview = ref<HostSummary[]>([])
const showOverview = ref(false)

// ── Disk usage ──────────────────────────────────────────────────
const showDf = ref(false)
const dfLoading = ref(false)
const diskUsage = ref<DiskUsage | null>(null)

// ── Topology ────────────────────────────────────────────────────
const showTopology = ref(false)
const topoLoading = ref(false)
const topoNodes = ref<TopoNode[]>([])
const topoEdges = ref<TopoEdge[]>([])
const topoPan = ref({ x: 40, y: 20 })
const topoZoom = ref(1)
let topoDragging = false
let topoDragStart = { x: 0, y: 0, px: 0, py: 0 }
const topoCanvasEl = ref<HTMLElement | null>(null)
const selectedNodeId = ref<string | null>(null)
const topoCustomPos = ref<Record<string, { x: number; y: number }>>({})
let draggingNodeId: string | null = null
let nodeDragOffset = { x: 0, y: 0 }
let nodeMoved = false

// Expanded network rows (connected containers)
const netExpanded = ref<Record<string, boolean>>({})

// Create volume / network
const showCreateVol = ref(false)
const createVolForm = ref({ name: '', driver: '' })
const showCreateNet = ref(false)
const createNetForm = ref({ name: '', driver: 'bridge' })

// File browser
const showFiles = ref(false)
const filesCid = ref('')
const filesName = ref('')
const filesPath = ref('/')
const fileEntries = ref<{ name: string; size: number; isDir: boolean; mode: string }[]>([])
const filesLoading = ref(false)

// Compose
const showCompose = ref(false)
const composing = ref(false)
const composeOutput = ref('')
const composeForm = ref({ name: '', yaml: '' })

// Events
const showEvents = ref(false)
const events = ref<{ type: string; action: string; name: string; time: number }[]>([])

// Container internals (top / changes), shown in inspect
const topData = ref<{ titles: string[]; processes: string[][] } | null>(null)
const changesData = ref<{ Path: string; Kind: number }[]>([])

// Commit modal
const showCommit = ref(false)
const commitCid = ref('')
const commitForm = ref({ repo: '', tag: 'snapshot', comment: '' })

// Image history modal
const showHistory = ref(false)
const historyImage = ref('')
const historyData = ref<{ Id: string; Created: number; CreatedBy: string; Size: number; Comment: string }[]>([])

// Network connect
const netConnectSel = ref<Record<string, string>>({})

// Bulk selection
const selectedCids = ref<Set<string>>(new Set())

// Network / volume detail slide-overs
const showNetInspect = ref(false)
const netInspectName = ref('')
const netInspectData = ref<any>(null)
const showVolInspect = ref(false)
const volInspectName = ref('')
const volInspectData = ref<any>(null)

// ── Rename modal ────────────────────────────────────────────────
const showRename = ref(false)
const renameCid = ref('')
const renameValue = ref('')
const loading = ref(false)
const daemonInfo = ref<DaemonInfo | null>(null)
const connError = ref('')

const statsMap = ref<Record<string, ContainerStats>>({})
const expanded = ref<Record<string, boolean>>({})
const busyAction = ref('') // "<cid>:<action>"

// ── Host form modal ─────────────────────────────────────────────
const showHostForm = ref(false)
const editingHostId = ref<number | null>(null)
const savingHost = ref(false)
const testing = ref(false)
const emptyForm = (): HostForm => ({
  mode: 'local',
  name: '',
  ssh_host: '',
  ssh_port: 22,
  ssh_user: '',
  ssh_password: '',
  ssh_key: '',
  socket_path: '/var/run/docker.sock',
})

// Strip SSH fields when saving a local host so the backend connects to the
// daemon socket on this machine directly (empty ssh_host = local mode).
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
const form = ref<HostForm>(emptyForm())

// ── Logs modal ──────────────────────────────────────────────────
const showLogs = ref(false)
const logsText = ref('')
const logsTitle = ref('')
const logsCid = ref('')
const logsLoading = ref(false)
const logsTail = ref('200')
const logsTimestamps = ref(false)
const logsPretty = ref(true)

// ── Inspect / exec slide-over ───────────────────────────────────
const inspectOpen = ref(false)
const inspectLoading = ref(false)
const inspectData = ref<any>(null)
const inspectCid = ref('')
const inspectName = ref('')
const inspectRunning = ref(false)
const execCmd = ref('')
const execOutput = ref('')
const execExit = ref<number | null>(null)
const execRunning = ref(false)

// ── Run-container modal ─────────────────────────────────────────
const showRunForm = ref(false)
const runSaving = ref(false)
const runForm = ref({
  image: '',
  name: '',
  ports: '',
  env: '',
  volumes: '',
  cmd: '',
  network: '',
  restartPolicy: 'no',
  memoryMb: 0,
  cpus: 0,
  autoPull: true,
})

// ── Images management ───────────────────────────────────────────
const pullImage = ref('')
const pulling = ref(false)
const pruning = ref(false)

// Pull-with-auth (private registry) modal
const showPullAuth = ref(false)
const pullAuth = ref({ username: '', password: '', registry: '' })

// Build modal
const showBuild = ref(false)
const building = ref(false)
const buildOutput = ref('')
const buildForm = ref({ tag: '', gitUrl: '', dockerfile: '' })

// ── Auto-refresh ────────────────────────────────────────────────
const autoRefresh = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | undefined

// ── Terminal ────────────────────────────────────────────────────
const showTerminal = ref(false)
const termFullscreen = ref(false)
const termFontSize = ref(13)
const termTitle = ref('')
const termEl = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let termSocket: WebSocket | null = null

// ── Data loading ────────────────────────────────────────────────
// Remember the user's connection intent across refreshes.
const LAST_HOST_KEY = 'nias:docker:lastHost'

async function loadHosts() {
  try {
    const { data } = await axios.get<DockerHost[]>('/api/docker/hosts')
    hosts.value = data
    if (activeHostId.value === null && data.length) {
      // An explicit ?host= (e.g. from the SSH Hosts page) means "connect now".
      const queryId = route.query.host ? Number(route.query.host) : null
      const queryHost = queryId ? data.find((h) => h.id === queryId) : null
      if (queryHost) { await selectHost(queryHost.id); return }
      const saved = localStorage.getItem(LAST_HOST_KEY)
      if (saved === 'disconnected') return // user explicitly disconnected
      const savedId = saved ? Number(saved) : null
      const target = (savedId && data.find((h) => h.id === savedId)) || data[0]
      activeHostId.value = target.id
      await refresh()
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load Docker hosts')
  }
}

async function selectHost(id: number) {
  if (activeHostId.value === id) return
  closeAllStatStreams()
  activeHostId.value = id
  statsMap.value = {}
  expanded.value = {}
  localStorage.setItem(LAST_HOST_KEY, String(id))
  await refresh()
}

function disconnectHost() {
  closeAllStatStreams()
  autoRefresh.value = false
  activeHostId.value = null
  localStorage.setItem(LAST_HOST_KEY, 'disconnected')
  // Clear content so nothing stale is shown after disconnect
  containers.value = []
  images.value = []
  volumes.value = []
  networks.value = []
  daemonInfo.value = null
  connError.value = ''
  statsMap.value = {}
  expanded.value = {}
}

async function refresh(silent = false) {
  if (activeHostId.value === null) return
  if (!silent) loading.value = true
  connError.value = ''
  try {
    const { data: ping } = await axios.get<DaemonInfo>(`/api/docker/hosts/${activeHostId.value}/ping`)
    daemonInfo.value = ping
    // Load everything so tab counts are accurate and switching is instant.
    await Promise.all([loadContainers(), loadImages(), loadVolumes(), loadNetworks()])
  } catch (e: any) {
    connError.value = e?.response?.data?.error || e?.message || 'Could not reach the Docker daemon'
    containers.value = []
    images.value = []
    volumes.value = []
    networks.value = []
    daemonInfo.value = null
  } finally {
    if (!silent) loading.value = false
  }
}

async function loadContainers() {
  const { data } = await axios.get<DockerContainer[]>(`/api/docker/hosts/${activeHostId.value}/containers`)
  containers.value = data
}

async function loadImages() {
  const { data } = await axios.get<DockerImage[]>(`/api/docker/hosts/${activeHostId.value}/images`)
  images.value = data
}

async function loadVolumes() {
  const { data } = await axios.get<DockerVolume[]>(`/api/docker/hosts/${activeHostId.value}/volumes`)
  volumes.value = data
}

async function loadNetworks() {
  const { data } = await axios.get<DockerNetwork[]>(`/api/docker/hosts/${activeHostId.value}/networks`)
  networks.value = data
}

function switchTab(t: 'containers' | 'images' | 'volumes' | 'networks') {
  tab.value = t
}

// ── Container actions ───────────────────────────────────────────
async function containerAction(
  c: DockerContainer,
  action: 'start' | 'stop' | 'restart' | 'pause' | 'unpause',
) {
  busyAction.value = `${c.id}:${action}`
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/containers/${c.id}/${action}`)
    toast.success(`${action} → ${containerName(c)}`)
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || `Failed to ${action} container`)
  } finally {
    busyAction.value = ''
  }
}

// Build an authenticated WebSocket URL (token via query — browsers can't set headers).
function wsUrl(path: string, params: Record<string, string | number> = {}) {
  const token = localStorage.getItem('nias-token') || ''
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const qs = new URLSearchParams({ ...(params as Record<string, string>), token }).toString()
  return `${proto}//${location.host}${path}?${qs}`
}

// ── Live stats streaming (per expanded container) ───────────────
const statsSockets = new Map<string, WebSocket>()
const statsHistory = ref<Record<string, { cpu: number[]; mem: number[]; net: number[] }>>({})
const statsPrevNet = new Map<string, number>()
const STATS_POINTS = 60 // ~1 minute at 1 sample/sec

function closeStatStream(cid: string) {
  const ws = statsSockets.get(cid)
  if (ws) {
    ws.onclose = null
    ws.close()
    statsSockets.delete(cid)
  }
  statsPrevNet.delete(cid)
  if (statsHistory.value[cid]) {
    const h = { ...statsHistory.value }
    delete h[cid]
    statsHistory.value = h
  }
}
function closeAllStatStreams() {
  for (const cid of [...statsSockets.keys()]) closeStatStream(cid)
}
function connectStatStream(cid: string) {
  closeStatStream(cid)
  const ws = new WebSocket(wsUrl(`/api/docker/hosts/${activeHostId.value}/containers/${cid}/statstream`))
  statsSockets.set(cid, ws)
  ws.onmessage = (ev) => {
    try {
      const d = JSON.parse(ev.data as string)
      statsMap.value = { ...statsMap.value, [cid]: d }
      // Net rate (bytes/s) from cumulative counters.
      const totalNet = (d.net_rx || 0) + (d.net_tx || 0)
      const prev = statsPrevNet.get(cid)
      const rate = prev != null ? Math.max(0, totalNet - prev) : 0
      statsPrevNet.set(cid, totalNet)
      const h = statsHistory.value[cid] || { cpu: [], mem: [], net: [] }
      const push = (arr: number[], v: number) => {
        arr.push(v)
        if (arr.length > STATS_POINTS) arr.shift()
      }
      push(h.cpu, d.cpu_percent || 0)
      push(h.mem, d.mem_percent || 0)
      push(h.net, rate)
      statsHistory.value = { ...statsHistory.value, [cid]: { cpu: [...h.cpu], mem: [...h.mem], net: [...h.net] } }
    } catch {}
  }
}

// SVG sparkline path builders (viewBox 0 0 W H).
function sparkLine(values: number[], w = 120, h = 30, max?: number): string {
  if (!values.length) return ''
  const mx = max ?? Math.max(...values, 0.0001)
  const step = values.length > 1 ? w / (values.length - 1) : 0
  return values
    .map((v, i) => `${i === 0 ? 'M' : 'L'} ${(i * step).toFixed(1)} ${(h - (Math.min(v, mx) / mx) * h).toFixed(1)}`)
    .join(' ')
}
function sparkArea(values: number[], w = 120, h = 30, max?: number): string {
  const line = sparkLine(values, w, h, max)
  return line ? `${line} L ${w} ${h} L 0 ${h} Z` : ''
}
function toggleStats(c: DockerContainer) {
  const open = !expanded.value[c.id]
  expanded.value = { ...expanded.value, [c.id]: open }
  if (open) connectStatStream(c.id)
  else closeStatStream(c.id)
}

// ── Live log streaming ──────────────────────────────────────────
let logsSocket: WebSocket | null = null
let logBuffer = ''
let logFlushTimer: ReturnType<typeof setTimeout> | undefined
function flushLogBuffer() {
  logFlushTimer = undefined
  if (!logBuffer) return
  let t = logsText.value + logBuffer
  logBuffer = ''
  if (t.length > 1_000_000) t = t.slice(t.length - 800_000) // cap memory on long follows
  logsText.value = t
}
function closeLogStream() {
  if (logFlushTimer) {
    clearTimeout(logFlushTimer)
    logFlushTimer = undefined
  }
  logBuffer = ''
  if (logsSocket) {
    logsSocket.onclose = null
    logsSocket.close()
    logsSocket = null
  }
}
function connectLogStream() {
  closeLogStream()
  logsText.value = ''
  logsLoading.value = true
  const ws = new WebSocket(
    wsUrl(`/api/docker/hosts/${activeHostId.value}/containers/${logsCid.value}/logstream`, {
      tail: logsTail.value,
      timestamps: logsTimestamps.value ? 1 : 0,
    }),
  )
  ws.binaryType = 'arraybuffer'
  logsSocket = ws
  const dec = new TextDecoder()
  ws.onmessage = (ev) => {
    logsLoading.value = false
    logBuffer += typeof ev.data === 'string' ? ev.data : dec.decode(new Uint8Array(ev.data as ArrayBuffer))
    // Throttle reactive updates so parsing stays smooth on chatty streams.
    if (!logFlushTimer) logFlushTimer = setTimeout(flushLogBuffer, 200)
  }
  ws.onclose = () => {
    logsLoading.value = false
  }
  ws.onerror = () => {
    logsLoading.value = false
  }
}

// ── Smart log parsing (levels, JSON, stack-trace grouping) ──────
interface LogEntry { ts: string; level: string; text: string; json: string; cont: string[] }
const logTsRe = /^(\d{4}-\d{2}-\d{2}T[\d:.]+Z)\s+(.*)$/
const logLevelRe = /\b(FATAL|PANIC|ERROR|ERR|WARN(?:ING)?|INFO|DEBUG|TRACE)\b/i
const logContRe = /^(\s+|at\s|\.\.\.\s|Caused by|Traceback|\s*File ")/

function normLevel(l: string): string {
  const u = l.toUpperCase()
  if (u === 'ERR') return 'ERROR'
  if (u === 'WARNING') return 'WARN'
  if (u === 'PANIC') return 'FATAL'
  return u
}
const parsedLogEntries = computed<LogEntry[]>(() => {
  const out: LogEntry[] = []
  for (const raw of logsText.value.split('\n')) {
    if (raw === '') continue
    if (out.length && logContRe.test(raw) && !logTsRe.test(raw)) {
      out[out.length - 1].cont.push(raw)
      continue
    }
    let ts = ''
    let body = raw
    const m = raw.match(logTsRe)
    if (m) {
      ts = m[1]
      body = m[2]
    }
    let level = ''
    const lm = body.match(logLevelRe)
    if (lm) level = normLevel(lm[1])
    let json = ''
    const trimmed = body.trim()
    if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
      try {
        json = JSON.stringify(JSON.parse(trimmed), null, 2)
      } catch {
        /* not JSON */
      }
    }
    out.push({ ts, level, text: body, json, cont: [] })
  }
  return out
})
const { search: logsSearch, filtered: logEntries } = useListFilter(parsedLogEntries, (e, q) =>
  e.text.toLowerCase().includes(q) || e.cont.some((c) => c.toLowerCase().includes(q)),
)

// Raw text respecting the search filter (for the plain view + download).
const logsDisplay = computed(() => {
  if (!logsSearch.value.trim()) return logsText.value
  const q = logsSearch.value.toLowerCase()
  return logsText.value
    .split('\n')
    .filter((l) => l.toLowerCase().includes(q))
    .join('\n')
})

function downloadLogs() {
  const blob = new Blob([logsText.value], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${logsTitle.value || 'container'}.log`
  a.click()
  URL.revokeObjectURL(a.href)
}

function openLogs(c: DockerContainer) {
  logsTitle.value = containerName(c)
  logsCid.value = c.id
  showLogs.value = true
  connectLogStream()
}

function closeLogs() {
  showLogs.value = false
  logsCid.value = ''
  closeLogStream()
}

// ── Inspect / exec ──────────────────────────────────────────────
async function openInspect(c: DockerContainer) {
  inspectCid.value = c.id
  inspectName.value = containerName(c)
  inspectRunning.value = c.state === 'running'
  inspectData.value = null
  inspectOpen.value = true
  inspectLoading.value = true
  execCmd.value = ''
  execOutput.value = ''
  execExit.value = null
  loadInternals(c)
  try {
    const { data } = await axios.get(`/api/docker/hosts/${activeHostId.value}/containers/${c.id}/inspect`)
    inspectData.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to inspect container')
    inspectOpen.value = false
  } finally {
    inspectLoading.value = false
  }
}

async function runExec() {
  const cmd = execCmd.value.trim()
  if (!cmd) return
  execRunning.value = true
  execExit.value = null
  try {
    const { data } = await axios.post<{ output: string; exit_code: number }>(
      `/api/docker/hosts/${activeHostId.value}/containers/${inspectCid.value}/exec`,
      { cmd },
    )
    execOutput.value = data.output || '(no output)'
    execExit.value = data.exit_code
  } catch (e: any) {
    execOutput.value = e?.response?.data?.error || 'Exec failed'
    execExit.value = -1
  } finally {
    execRunning.value = false
  }
}

// ── Container remove / run ──────────────────────────────────────
async function removeContainer(c: DockerContainer) {
  const ok = await confirm(`Remove container "${containerName(c)}"? This cannot be undone.`, 'Remove container')
  if (!ok) return
  try {
    await axios.delete(`/api/docker/hosts/${activeHostId.value}/containers/${c.id}`)
    toast.success('Container removed')
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove container')
  }
}

function openRun() {
  runForm.value = {
    image: '',
    name: '',
    ports: '',
    env: '',
    volumes: '',
    cmd: '',
    network: '',
    restartPolicy: 'no',
    memoryMb: 0,
    cpus: 0,
    autoPull: true,
  }
  showRunForm.value = true
}

async function submitRun() {
  if (!runForm.value.image.trim()) {
    toast.error('Image is required')
    return
  }
  // ports: "8080:80, 5432:5432/tcp"  → [{host,container,proto}]
  const ports = runForm.value.ports
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((spec) => {
      const [hostPart, contPart] = spec.split(':')
      const [container, proto] = (contPart ?? hostPart).split('/')
      return { host: contPart ? hostPart : '', container: container, proto: proto || 'tcp' }
    })
  const env = runForm.value.env
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
  // volumes: "/data:/var/lib/data, /cfg:/etc/app:ro"
  const vols = runForm.value.volumes
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((spec) => {
      const parts = spec.split(':')
      return { host: parts[0], container: parts[1] || '', ro: parts[2] === 'ro' }
    })
    .filter((v) => v.host && v.container)
  runSaving.value = true
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/containers`, {
      image: runForm.value.image.trim(),
      name: runForm.value.name.trim(),
      ports,
      env,
      volumes: vols,
      cmd: runForm.value.cmd.trim(),
      network: runForm.value.network.trim(),
      restart_policy: runForm.value.restartPolicy,
      memory: runForm.value.memoryMb > 0 ? Math.round(runForm.value.memoryMb * 1024 * 1024) : 0,
      cpus: Number(runForm.value.cpus) || 0,
      auto_pull: runForm.value.autoPull,
    })
    toast.success('Container started')
    showRunForm.value = false
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to run container')
  } finally {
    runSaving.value = false
  }
}

// ── Image management ────────────────────────────────────────────
async function pullImageNow(auth?: { username: string; password: string; registry: string }) {
  const img = pullImage.value.trim()
  if (!img) return
  pulling.value = true
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/images/pull`, { image: img, ...(auth || {}) })
    toast.success(`Pulled ${img}`)
    pullImage.value = ''
    showPullAuth.value = false
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to pull image')
  } finally {
    pulling.value = false
  }
}

function openPullAuth() {
  if (!pullImage.value.trim()) {
    toast.error('Enter an image name first')
    return
  }
  pullAuth.value = { username: '', password: '', registry: '' }
  showPullAuth.value = true
}

// ── Image build ─────────────────────────────────────────────────
function openBuild() {
  buildForm.value = { tag: '', gitUrl: '', dockerfile: '' }
  buildOutput.value = ''
  showBuild.value = true
}

async function submitBuild() {
  if (!buildForm.value.tag.trim()) {
    toast.error('Image tag is required')
    return
  }
  if (!buildForm.value.gitUrl.trim() && !buildForm.value.dockerfile.trim()) {
    toast.error('Provide a Git URL or a Dockerfile')
    return
  }
  building.value = true
  buildOutput.value = 'Building… (this can take a while)\n'
  try {
    const { data } = await axios.post<{ ok: boolean; output: string; error: string }>(
      `/api/docker/hosts/${activeHostId.value}/images/build`,
      { tag: buildForm.value.tag.trim(), git_url: buildForm.value.gitUrl.trim(), dockerfile: buildForm.value.dockerfile },
    )
    buildOutput.value = data.output || '(no output)'
    if (data.error) {
      buildOutput.value += `\n\nERROR: ${data.error}`
      toast.error('Build failed')
    } else {
      toast.success(`Built ${buildForm.value.tag.trim()}`)
      await loadImages()
    }
  } catch (e: any) {
    buildOutput.value += `\n\nERROR: ${e?.response?.data?.error || 'build request failed'}`
    toast.error(e?.response?.data?.error || 'Build failed')
  } finally {
    building.value = false
  }
}

async function removeImage(img: DockerImage) {
  const label = img.repoTags && img.repoTags.length ? img.repoTags[0] : shortId(img.id)
  const ok = await confirm(`Remove image "${label}"?`, 'Remove image')
  if (!ok) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/images/remove`, { ref: img.id, force: true })
    toast.success('Image removed')
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove image')
  }
}

async function pruneContainers() {
  const ok = await confirm('Remove all stopped containers on this host?', 'Prune containers')
  if (!ok) return
  pruning.value = true
  try {
    const { data } = await axios.post<{ SpaceReclaimed?: number }>(`/api/docker/hosts/${activeHostId.value}/prune/containers`)
    toast.success(`Pruned — reclaimed ${formatBytes(data.SpaceReclaimed || 0)}`)
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Prune failed')
  } finally {
    pruning.value = false
  }
}

async function pruneImages() {
  const ok = await confirm('Remove all dangling (untagged) images on this host?', 'Prune images')
  if (!ok) return
  pruning.value = true
  try {
    const { data } = await axios.post<{ SpaceReclaimed?: number }>(`/api/docker/hosts/${activeHostId.value}/prune/images`)
    toast.success(`Pruned — reclaimed ${formatBytes(data.SpaceReclaimed || 0)}`)
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Prune failed')
  } finally {
    pruning.value = false
  }
}

// ── Volumes & networks ──────────────────────────────────────────
async function removeVolume(v: DockerVolume) {
  const ok = await confirm(`Remove volume "${v.name}"? Any data it holds is lost.`, 'Remove volume')
  if (!ok) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/volumes/remove`, { name: v.name, force: true })
    toast.success('Volume removed')
    await loadVolumes()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove volume')
  }
}

async function removeNetwork(n: DockerNetwork) {
  const ok = await confirm(`Remove network "${n.name}"?`, 'Remove network')
  if (!ok) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/networks/remove`, { id: n.id })
    toast.success('Network removed')
    await loadNetworks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove network')
  }
}

async function pruneVolumes() {
  const ok = await confirm('Remove all unused volumes on this host?', 'Prune volumes')
  if (!ok) return
  pruning.value = true
  try {
    const { data } = await axios.post<{ SpaceReclaimed?: number }>(`/api/docker/hosts/${activeHostId.value}/prune/volumes`)
    toast.success(`Pruned — reclaimed ${formatBytes(data.SpaceReclaimed || 0)}`)
    await loadVolumes()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Prune failed')
  } finally {
    pruning.value = false
  }
}

async function pruneNetworks() {
  const ok = await confirm('Remove all unused networks on this host?', 'Prune networks')
  if (!ok) return
  pruning.value = true
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/prune/networks`)
    toast.success('Unused networks pruned')
    await loadNetworks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Prune failed')
  } finally {
    pruning.value = false
  }
}

async function createVolume() {
  if (!createVolForm.value.name.trim()) {
    toast.error('Volume name is required')
    return
  }
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/volumes`, createVolForm.value)
    toast.success('Volume created')
    showCreateVol.value = false
    await loadVolumes()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create volume')
  }
}

async function createNetwork() {
  if (!createNetForm.value.name.trim()) {
    toast.error('Network name is required')
    return
  }
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/networks`, createNetForm.value)
    toast.success('Network created')
    showCreateNet.value = false
    await loadNetworks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create network')
  }
}

// ── File browser ────────────────────────────────────────────────
function joinPath(base: string, name: string) {
  return base === '/' ? '/' + name : base + '/' + name
}
async function loadFiles(path: string) {
  filesLoading.value = true
  try {
    const { data } = await axios.get<{ path: string; entries: typeof fileEntries.value }>(
      `/api/docker/hosts/${activeHostId.value}/containers/${filesCid.value}/ls`,
      { params: { path } },
    )
    filesPath.value = data.path
    fileEntries.value = (data.entries || []).sort((a, b) =>
      a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
    )
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to list files (needs a running container with /bin/sh)')
  } finally {
    filesLoading.value = false
  }
}
function openFiles(c: DockerContainer) {
  filesCid.value = c.id
  filesName.value = containerName(c)
  filesPath.value = '/'
  fileEntries.value = []
  showFiles.value = true
  loadFiles('/')
}
function fileNavigate(entry: { name: string; isDir: boolean }) {
  if (entry.isDir) loadFiles(joinPath(filesPath.value, entry.name))
}
function filesUp() {
  if (filesPath.value === '/') return
  loadFiles(filesPath.value.replace(/\/[^/]+$/, '') || '/')
}
async function downloadFile(entry: { name: string }) {
  try {
    const { data } = await axios.get(
      `/api/docker/hosts/${activeHostId.value}/containers/${filesCid.value}/download`,
      { params: { path: joinPath(filesPath.value, entry.name) }, responseType: 'blob' },
    )
    const a = document.createElement('a')
    a.href = URL.createObjectURL(data)
    a.download = entry.name
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) {
    toast.error(await blobError(e, 'Download failed'))
  }
}
async function uploadFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const fd = new FormData()
  fd.append('file', file)
  fd.append('path', filesPath.value)
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/containers/${filesCid.value}/upload`, fd)
    toast.success(`Uploaded ${file.name}`)
    loadFiles(filesPath.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Upload failed')
  } finally {
    input.value = ''
  }
}

// ── Image save / load ───────────────────────────────────────────
// Extract a readable error from a blob/JSON axios error (blob responses hide it).
async function blobError(e: any, fallback: string): Promise<string> {
  const data = e?.response?.data
  if (data instanceof Blob) {
    try {
      const text = await data.text()
      try {
        return JSON.parse(text).error || text || fallback
      } catch {
        return text || fallback
      }
    } catch {
      /* ignore */
    }
  }
  if (e?.response?.status) return `${fallback} (HTTP ${e.response.status})`
  return e?.message || fallback
}

async function saveImage(img: DockerImage) {
  try {
    const { data } = await axios.get(`/api/docker/hosts/${activeHostId.value}/images/save`, {
      params: { ref: img.id },
      responseType: 'blob',
    })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(data)
    a.download = `${(img.repoTags?.[0] || shortId(img.id)).replace(/[/:]/g, '_')}.tar`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) {
    toast.error(await blobError(e, 'Export failed'))
  }
}
async function loadImageFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const fd = new FormData()
  fd.append('file', file)
  pulling.value = true
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/images/load`, fd)
    toast.success('Image imported')
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Import failed')
  } finally {
    pulling.value = false
    input.value = ''
  }
}

// ── Compose stacks ──────────────────────────────────────────────
function openCompose() {
  composeForm.value = { name: '', yaml: '' }
  composeOutput.value = ''
  showCompose.value = true
}
async function submitCompose() {
  if (!composeForm.value.name.trim() || !composeForm.value.yaml.trim()) {
    toast.error('Stack name and compose YAML are required')
    return
  }
  composing.value = true
  composeOutput.value = 'Deploying…\n'
  try {
    const { data } = await axios.post<{ ok: boolean; output: string; error: string }>(
      `/api/docker/hosts/${activeHostId.value}/compose`,
      { name: composeForm.value.name.trim(), yaml: composeForm.value.yaml },
    )
    composeOutput.value = data.output || '(no output)'
    if (data.error) {
      composeOutput.value += `\n\nERROR: ${data.error}`
      toast.error('Deploy failed')
    } else {
      toast.success(`Stack "${composeForm.value.name.trim()}" deployed`)
      await loadContainers()
    }
  } catch (e: any) {
    composeOutput.value += `\n\nERROR: ${e?.response?.data?.error || 'request failed'}`
    toast.error('Deploy failed')
  } finally {
    composing.value = false
  }
}
async function removeStack(project: string) {
  const ok = await confirm(`Bring down the compose stack "${project}" (stop + remove its containers and networks)?`, 'Remove stack')
  if (!ok) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/compose/down`, { name: project })
    toast.success(`Stack "${project}" removed`)
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove stack')
  }
}

// ── Events ──────────────────────────────────────────────────────
async function loadEvents() {
  try {
    const { data } = await axios.get<{ events: typeof events.value; until: number }>(
      `/api/docker/hosts/${activeHostId.value}/events`,
    )
    events.value = (data.events || []).reverse()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load events')
  }
}
function toggleEvents() {
  showEvents.value = !showEvents.value
  if (showEvents.value && activeHostId.value !== null) loadEvents()
}

// ── Container internals (top / changes / commit) ────────────────
function loadInternals(c: DockerContainer) {
  topData.value = null
  changesData.value = []
  if (c.state !== 'running') return
  axios
    .get<{ Titles: string[]; Processes: string[][] }>(`/api/docker/hosts/${activeHostId.value}/containers/${c.id}/top`)
    .then((r) => {
      topData.value = { titles: r.data.Titles || [], processes: r.data.Processes || [] }
    })
    .catch(() => {})
  axios
    .get<{ Path: string; Kind: number }[]>(`/api/docker/hosts/${activeHostId.value}/containers/${c.id}/changes`)
    .then((r) => {
      changesData.value = r.data || []
    })
    .catch(() => {})
}
function changeKind(kind: number) {
  return kind === 1 ? 'added' : kind === 2 ? 'deleted' : 'modified'
}
function openCommitInspect() {
  commitCid.value = inspectCid.value
  commitForm.value = { repo: inspectName.value.replace(/[^a-z0-9_.-]/gi, '-').toLowerCase(), tag: 'snapshot', comment: '' }
  showCommit.value = true
}
async function submitCommit() {
  if (!commitForm.value.repo.trim()) {
    toast.error('Repository is required')
    return
  }
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/containers/${commitCid.value}/commit`, commitForm.value)
    toast.success('Snapshotted to image')
    showCommit.value = false
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Commit failed')
  }
}

// ── Image history ───────────────────────────────────────────────
async function openHistory(img: DockerImage) {
  historyImage.value = img.repoTags?.[0] || shortId(img.id)
  historyData.value = []
  showHistory.value = true
  try {
    const { data } = await axios.get<typeof historyData.value>(`/api/docker/hosts/${activeHostId.value}/images/history`, {
      params: { ref: img.id },
    })
    historyData.value = data || []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load history')
  }
}
function cleanLayerCmd(cmd: string) {
  return (cmd || '').replace(/^\/bin\/sh -c #\(nop\)\s*/, '').replace(/^\/bin\/sh -c /, 'RUN ').trim() || '(empty layer)'
}

// ── Network connect / disconnect ────────────────────────────────
async function connectNet(n: DockerNetwork) {
  const cname = netConnectSel.value[n.id]
  if (!cname) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/networks/connect`, { network: n.id, container: cname })
    toast.success('Container connected')
    await loadNetworks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Connect failed')
  }
}
async function disconnectNet(n: DockerNetwork, cname: string) {
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/networks/disconnect`, { network: n.id, container: cname })
    toast.success('Container disconnected')
    await loadNetworks()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Disconnect failed')
  }
}

// ── Network / volume inspect ────────────────────────────────────
function objEntries(o: Record<string, string> | undefined): [string, string][] {
  return o ? Object.entries(o) : []
}
async function openNetInspect(n: DockerNetwork) {
  netInspectName.value = n.name
  netInspectData.value = null
  showNetInspect.value = true
  try {
    const { data } = await axios.get(`/api/docker/hosts/${activeHostId.value}/networks/inspect`, { params: { id: n.id } })
    netInspectData.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to inspect network')
    showNetInspect.value = false
  }
}
const netInspView = computed(() => {
  const d = netInspectData.value
  if (!d) return null
  const containers = d.Containers || {}
  return {
    id: d.Id || '',
    driver: d.Driver || '',
    scope: d.Scope || '',
    internal: !!d.Internal,
    attachable: !!d.Attachable,
    ingress: !!d.Ingress,
    ipv6: !!d.EnableIPv6,
    created: d.Created || '',
    ipam: (d.IPAM?.Config || []).map((c: any) => ({ subnet: c.Subnet || '', gateway: c.Gateway || '', range: c.IPRange || '' })),
    options: objEntries(d.Options),
    labels: objEntries(d.Labels),
    containers: Object.keys(containers).map((cid) => ({
      name: containers[cid]?.Name || cid.slice(0, 12),
      ipv4: containers[cid]?.IPv4Address || '',
      ipv6: containers[cid]?.IPv6Address || '',
      mac: containers[cid]?.MacAddress || '',
    })),
  }
})
async function openVolInspect(v: DockerVolume) {
  volInspectName.value = v.name
  volInspectData.value = null
  showVolInspect.value = true
  try {
    const { data } = await axios.get(`/api/docker/hosts/${activeHostId.value}/volumes/inspect`, { params: { name: v.name } })
    volInspectData.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to inspect volume')
    showVolInspect.value = false
  }
}
const volInspView = computed(() => {
  const wrap = volInspectData.value
  if (!wrap) return null
  const d = wrap.volume || {}
  return {
    driver: d.Driver || '',
    scope: d.Scope || '',
    mountpoint: d.Mountpoint || '',
    createdAt: d.CreatedAt || '',
    options: objEntries(d.Options),
    labels: objEntries(d.Labels),
    status: objEntries(d.Status),
    size: d.UsageData?.Size,
    refCount: d.UsageData?.RefCount,
    usedBy: (wrap.used_by || []) as { container: string; state: string; destination: string; rw: boolean }[],
  }
})

// ── Bulk selection ──────────────────────────────────────────────
function toggleSelect(id: string) {
  const s = new Set(selectedCids.value)
  s.has(id) ? s.delete(id) : s.add(id)
  selectedCids.value = s
}
async function batchAction(action: 'start' | 'stop' | 'restart' | 'remove') {
  const cids = [...selectedCids.value]
  if (!cids.length) return
  if (action === 'remove') {
    const ok = await confirm(`Remove ${cids.length} selected container(s)? This cannot be undone.`, 'Bulk remove')
    if (!ok) return
  }
  let failed = 0
  for (const id of cids) {
    try {
      if (action === 'remove') await axios.delete(`/api/docker/hosts/${activeHostId.value}/containers/${id}`)
      else await axios.post(`/api/docker/hosts/${activeHostId.value}/containers/${id}/${action}`)
    } catch {
      failed++
    }
  }
  toast.success(`${action} — ${cids.length - failed} ok${failed ? `, ${failed} failed` : ''}`)
  selectedCids.value = new Set()
  await loadContainers()
}

// ── Interactive terminal ────────────────────────────────────────
function refitTerminal() {
  nextTick(() => {
    if (fitAddon) fitAddon.fit()
    if (term && termSocket?.readyState === WebSocket.OPEN) {
      termSocket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
  })
}

function toggleTermFullscreen() {
  termFullscreen.value = !termFullscreen.value
  refitTerminal()
}

function changeFont(delta: number) {
  termFontSize.value = Math.min(22, Math.max(9, termFontSize.value + delta))
  if (term) term.options.fontSize = termFontSize.value
  refitTerminal()
}

function disposeTerminal() {
  window.removeEventListener('resize', refitTerminal)
  if (termSocket) {
    termSocket.onclose = null
    termSocket.close()
    termSocket = null
  }
  if (term) {
    term.dispose()
    term = null
  }
  fitAddon = null
}

function closeTerminal() {
  showTerminal.value = false
  termFullscreen.value = false
  disposeTerminal()
}

async function openTerminal(c: DockerContainer) {
  termTitle.value = containerName(c)
  showTerminal.value = true
  await nextTick()
  if (!termEl.value) return

  term = new Terminal({
    cursorBlink: true,
    fontSize: termFontSize.value,
    fontFamily: "'JetBrains Mono', Menlo, monospace",
    theme: { background: '#0d1117' },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  fitAddon.fit()
  window.addEventListener('resize', refitTerminal)

  // Copy (on selection), paste, and Esc-to-exit-fullscreen.
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true
    const mod = e.ctrlKey || e.metaKey
    if (mod && e.key === 'c' && term?.hasSelection()) {
      navigator.clipboard.writeText(term.getSelection())
      return false
    }
    if (mod && e.key === 'v') {
      navigator.clipboard.readText().then((t) => {
        if (termSocket?.readyState === WebSocket.OPEN) termSocket.send(new TextEncoder().encode(t))
      })
      return false
    }
    if (e.key === 'Escape' && termFullscreen.value) {
      termFullscreen.value = false
      refitTerminal()
      return false
    }
    return true
  })

  const token = localStorage.getItem('nias-token') || ''
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${location.host}/api/docker/hosts/${activeHostId.value}/containers/${c.id}/terminal?shell=/bin/sh&token=${encodeURIComponent(token)}`
  const ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'
  termSocket = ws

  const sendResize = () => {
    if (!term || ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
  }

  ws.onopen = () => {
    term?.writeln('\x1b[90mConnected. If you see no prompt, the container may not have /bin/sh.\x1b[0m')
    sendResize()
    term?.focus()
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') term?.write(ev.data)
    else term?.write(new Uint8Array(ev.data as ArrayBuffer))
  }
  ws.onclose = () => term?.writeln('\r\n\x1b[90m[session closed]\x1b[0m')
  ws.onerror = () => term?.writeln('\r\n\x1b[31m[connection error]\x1b[0m')

  term.onData((d) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(d))
  })
  term.onResize(sendResize)
}

// ── Search / filter ─────────────────────────────────────────────
// One search box drives four independent resource lists (only one tab is
// visible at a time), so each list gets its own useListFilter instance and
// a writable `search` computed fans a single input out to all of them.
const { search: searchContainers, filtered: filteredContainers } = useListFilter(containers, (c, q) =>
  containerName(c).toLowerCase().includes(q) || c.image.toLowerCase().includes(q),
)
const { search: searchImages, filtered: filteredImages } = useListFilter(images, (i, q) =>
  (i.repoTags || []).join(' ').toLowerCase().includes(q),
)
const { search: searchVolumes, filtered: filteredVolumes } = useListFilter(volumes, (v, q) =>
  v.name.toLowerCase().includes(q),
)
const { search: searchNetworks, filtered: filteredNetworks } = useListFilter(networks, (n, q) =>
  n.name.toLowerCase().includes(q),
)
const search = computed<string>({
  get: () => searchContainers.value,
  set: (v: string) => {
    searchContainers.value = v
    searchImages.value = v
    searchVolumes.value = v
    searchNetworks.value = v
  },
})

// ── Sort (containers table) ─────────────────────────────────────
function containerSortValue(c: DockerContainer, key: string): unknown {
  switch (key) {
    case 'name': return containerName(c).toLowerCase()
    case 'image': return c.image.toLowerCase()
    case 'state': return c.state.toLowerCase()
    case 'status': return c.status.toLowerCase()
    default: return ''
  }
}
const { sortKey: containerSortKey, sortDir: containerSortDir, toggleSort: toggleContainerSort, sort: sortContainers } = useSort<DockerContainer>(containerSortValue)

// ── Compose grouping ────────────────────────────────────────────
const containerGroups = computed(() => {
  const list = sortContainers(filteredContainers.value)
  if (!groupByCompose.value) return [{ project: '', containers: list }]
  const groups: Record<string, DockerContainer[]> = {}
  for (const c of list) {
    const project = c.labels?.['com.docker.compose.project'] || ''
    ;(groups[project] ||= []).push(c)
  }
  // Standalone (no project) last; projects alphabetical.
  return Object.keys(groups)
    .sort((a, b) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b)))
    .map((project) => ({ project, containers: groups[project] }))
})

// ── Multi-host overview ─────────────────────────────────────────
async function loadOverview() {
  try {
    const { data } = await axios.get<HostSummary[]>('/api/docker/overview')
    overview.value = data
  } catch {
    overview.value = []
  }
}
function toggleOverview() {
  showOverview.value = !showOverview.value
  if (showOverview.value) loadOverview()
}

// ── Disk usage ──────────────────────────────────────────────────
async function loadDf() {
  dfLoading.value = true
  try {
    const { data } = await axios.get<DiskUsage>(`/api/docker/hosts/${activeHostId.value}/df`)
    diskUsage.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load disk usage')
  } finally {
    dfLoading.value = false
  }
}
function toggleDf() {
  showDf.value = !showDf.value
  if (showDf.value && activeHostId.value !== null) loadDf()
}

// ── Topology graph ──────────────────────────────────────────────
function truncate(s: string | undefined, n: number) {
  if (!s) return ''
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}
function topoNodeClass(n: TopoNode) {
  if (n.type === 'network') return 'dk-tnode--net'
  if (n.type === 'volume') return 'dk-tnode--vol'
  if (n.state === 'running') return 'dk-tnode--running'
  if (n.state === 'paused' || n.state === 'created') return 'dk-tnode--paused'
  return 'dk-tnode--exited'
}

async function refreshTopology() {
  if (activeHostId.value === null) return
  try {
    const { data } = await axios.get<{ nodes: TopoNode[]; edges: TopoEdge[] }>(
      `/api/docker/hosts/${activeHostId.value}/topology`,
    )
    topoNodes.value = data.nodes || []
    topoEdges.value = data.edges || []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load topology')
    if (!topoNodes.value.length) showTopology.value = false
  }
}

async function openTopology() {
  if (activeHostId.value === null) return
  showTopology.value = true
  topoLoading.value = true
  topoPan.value = { x: 40, y: 20 }
  topoZoom.value = 1
  topoCustomPos.value = {}
  selectedNodeId.value = null
  await refreshTopology()
  topoLoading.value = false
}

// Convert a client coordinate into graph-space (accounting for pan/zoom).
function topoToGraph(clientX: number, clientY: number) {
  const rect = topoCanvasEl.value?.getBoundingClientRect()
  const left = rect?.left ?? 0
  const top = rect?.top ?? 0
  return { x: (clientX - left - topoPan.value.x) / topoZoom.value, y: (clientY - top - topoPan.value.y) / topoZoom.value }
}

function startNodeDrag(nd: { x: number; y: number; node: TopoNode }, e: MouseEvent) {
  draggingNodeId = nd.node.id
  nodeMoved = false
  const g = topoToGraph(e.clientX, e.clientY)
  nodeDragOffset = { x: g.x - nd.x, y: g.y - nd.y }
}

const selectedNode = computed(() => topoNodes.value.find((n) => n.id === selectedNodeId.value) || null)
const selectedTopoContainer = computed(() => {
  const n = selectedNode.value
  if (!n || n.type !== 'container') return null
  const cid = n.id.replace(/^c:/, '')
  return containers.value.find((c) => c.id === cid) || null
})

function topoOpen(fn: (c: DockerContainer) => void) {
  const c = selectedTopoContainer.value
  if (!c) return
  showTopology.value = false
  fn(c)
}
async function topoContainerAction(action: 'start' | 'stop' | 'restart') {
  const c = selectedTopoContainer.value
  if (!c) return
  await containerAction(c, action)
  await refreshTopology()
}

const TOPO = { cw: 210, ch: 62, nw: 150, nh: 42, vw: 190, vh: 38, gap: 14, colNet: 40, colCon: 380, colVol: 760 }

const topoLayout = computed(() => {
  const pos: Record<string, { x: number; y: number; w: number; h: number; node: TopoNode }> = {}
  const nets = topoNodes.value.filter((n) => n.type === 'network')
  const vols = topoNodes.value.filter((n) => n.type === 'volume')
  const cons = topoNodes.value
    .filter((n) => n.type === 'container')
    .slice()
    .sort((a, b) => {
      const pa = a.project || '~~'
      const pb = b.project || '~~'
      return pa === pb ? a.label.localeCompare(b.label) : pa.localeCompare(pb)
    })

  let y = 20
  for (const n of nets) {
    pos[n.id] = { x: TOPO.colNet, y, w: TOPO.nw, h: TOPO.nh, node: n }
    y += TOPO.nh + TOPO.gap
  }
  y = 20
  let lastProj: string | null = null
  for (const c of cons) {
    const proj = c.project || ''
    if (lastProj !== null && proj !== lastProj) y += 12
    lastProj = proj
    pos[c.id] = { x: TOPO.colCon, y, w: TOPO.cw, h: TOPO.ch, node: c }
    y += TOPO.ch + TOPO.gap
  }
  y = 20
  for (const v of vols) {
    pos[v.id] = { x: TOPO.colVol, y, w: TOPO.vw, h: TOPO.vh, node: v }
    y += TOPO.vh + TOPO.gap
  }

  // Apply any manual (dragged) overrides.
  for (const [id, p] of Object.entries(topoCustomPos.value)) {
    if (pos[id]) {
      pos[id].x = p.x
      pos[id].y = p.y
    }
  }

  const edges = topoEdges.value
    .map((e) => {
      const a = pos[e.from]
      const b = pos[e.to]
      if (!a || !b) return null
      let sx: number, sy: number, ex: number, ey: number
      if (e.type === 'network') {
        sx = a.x
        sy = a.y + a.h / 2
        ex = b.x + b.w
        ey = b.y + b.h / 2
      } else {
        sx = a.x + a.w
        sy = a.y + a.h / 2
        ex = b.x
        ey = b.y + b.h / 2
      }
      const mx = (sx + ex) / 2
      return {
        key: `${e.from}|${e.to}|${e.type}`,
        type: e.type,
        path: `M ${sx} ${sy} C ${mx} ${sy}, ${mx} ${ey}, ${ex} ${ey}`,
      }
    })
    .filter((e): e is NonNullable<typeof e> => e !== null)

  return { nodes: Object.values(pos), edges }
})

function topoWheel(e: WheelEvent) {
  e.preventDefault()
  const delta = e.deltaY < 0 ? 1.1 : 0.9
  topoZoom.value = Math.min(2.5, Math.max(0.3, topoZoom.value * delta))
}
function topoMouseDown(e: MouseEvent) {
  // Background drag = pan; deselect on a plain background click.
  topoDragging = true
  topoDragStart = { x: e.clientX, y: e.clientY, px: topoPan.value.x, py: topoPan.value.y }
}
function topoMouseMove(e: MouseEvent) {
  if (draggingNodeId) {
    nodeMoved = true
    const g = topoToGraph(e.clientX, e.clientY)
    topoCustomPos.value = { ...topoCustomPos.value, [draggingNodeId]: { x: g.x - nodeDragOffset.x, y: g.y - nodeDragOffset.y } }
    return
  }
  if (!topoDragging) return
  topoPan.value = { x: topoDragStart.px + (e.clientX - topoDragStart.x), y: topoDragStart.py + (e.clientY - topoDragStart.y) }
}
function topoMouseUp(e: MouseEvent) {
  if (draggingNodeId) {
    if (!nodeMoved) selectedNodeId.value = draggingNodeId // click (no drag) = select
    draggingNodeId = null
    topoDragging = false
    return
  }
  // Plain background click (no pan movement) deselects.
  if (topoDragging && Math.abs(e.clientX - topoDragStart.x) < 3 && Math.abs(e.clientY - topoDragStart.y) < 3) {
    selectedNodeId.value = null
  }
  topoDragging = false
}
function topoZoomBy(f: number) {
  topoZoom.value = Math.min(2.5, Math.max(0.3, topoZoom.value * f))
}
function topoFit() {
  topoPan.value = { x: 40, y: 20 }
  topoZoom.value = 1
}

// ── Rename ──────────────────────────────────────────────────────
function openRename(c: DockerContainer) {
  renameCid.value = c.id
  renameValue.value = containerName(c)
  showRename.value = true
}
async function submitRename() {
  const name = renameValue.value.trim()
  if (!name) return
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/containers/${renameCid.value}/rename`, { name })
    toast.success('Container renamed')
    showRename.value = false
    await loadContainers()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to rename')
  }
}

// ── Host CRUD ───────────────────────────────────────────────────
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
    // For an existing host with no new credentials, ping the saved record.
    // For an existing remote host with no new credentials, ping the saved record.
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
      const { data } = await axios.post<{ id: number }>('/api/docker/hosts', hostPayload())
      toast.success('Docker host added')
      showHostForm.value = false
      await loadHosts()
      await selectHost(data.id)
    } else {
      await axios.put(`/api/docker/hosts/${editingHostId.value}`, hostPayload())
      toast.success('Docker host updated')
      showHostForm.value = false
      await loadHosts()
      if (activeHostId.value === editingHostId.value) await refresh()
    }
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
    if (activeHostId.value === h.id) {
      activeHostId.value = null
      containers.value = []
      images.value = []
      daemonInfo.value = null
    }
    await loadHosts()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete host')
  }
}

// ── Formatting helpers ──────────────────────────────────────────
function containerName(c: DockerContainer): string {
  const n = c.names?.[0] || c.id
  return n.replace(/^\//, '')
}
function shortId(id: string): string {
  return id.replace(/^sha256:/, '').slice(0, 12)
}
function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(i ? 1 : 0)} ${units[i]}`
}
function formatRelative(unixSec: number): string {
  if (!unixSec) return '-'
  const diff = Date.now() / 1000 - unixSec
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}
function stateBadge(state: string): string {
  if (state === 'running') return 'badge--success'
  if (state === 'exited' || state === 'dead') return 'badge--danger'
  if (state === 'paused' || state === 'created') return 'badge--warning'
  return 'badge--default'
}
function portLabel(p: DockerPort): string {
  if (p.publicPort) return `${p.publicPort}→${p.privatePort}/${p.type}`
  return `${p.privatePort}/${p.type}`
}
function hostLabel(h: DockerHost): string {
  return h.ssh_host ? `${h.name} (${h.ssh_host})` : `${h.name} (local)`
}

// Curated view-model from the raw Docker inspect payload.
const inspView = computed(() => {
  const d = inspectData.value
  if (!d) return null
  const nets = d.NetworkSettings?.Networks || {}
  return {
    image: d.Config?.Image || d.Image || '',
    command: [...(d.Config?.Entrypoint || []), ...(d.Config?.Cmd || [])].join(' '),
    workdir: d.Config?.WorkingDir || '',
    hostname: d.Config?.Hostname || '',
    created: d.Created || '',
    restart: d.HostConfig?.RestartPolicy?.Name || 'no',
    sizeRw: d.SizeRw,
    sizeRootFs: d.SizeRootFs,
    status: d.State?.Status || '',
    startedAt: d.State?.StartedAt || '',
    exitCode: d.State?.ExitCode,
    health: d.State?.Health?.Status || '',
    env: (d.Config?.Env || []) as string[],
    mounts: (d.Mounts || []).map((m: any) => ({
      type: m.Type,
      source: m.Source,
      destination: m.Destination,
      rw: m.RW,
    })),
    networks: Object.keys(nets).map((name) => ({
      name,
      ip: nets[name]?.IPAddress || '',
      gateway: nets[name]?.Gateway || '',
      mac: nets[name]?.MacAddress || '',
    })),
  }
})

// Auto-refresh polls the lists silently while enabled.
watch(autoRefresh, (on) => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = undefined
  }
  if (on) {
    refreshTimer = setInterval(() => {
      if (activeHostId.value !== null && !connError.value) refresh(true)
    }, 5000)
  }
})

onBeforeUnmount(() => {
  if (refreshTimer) clearInterval(refreshTimer)
  closeLogStream()
  closeAllStatStreams()
  disposeTerminal()
})

onMounted(loadHosts)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <!-- Hero header -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Docker</div>
            <div class="page-subtitle">
              Browse and control containers and images across your servers over SSH — no daemon port exposed.
            </div>
          </div>
          <div class="page-hero__actions">
            <SearchSelect
              v-if="hosts.length"
              class="dk-host-select"
              :model-value="activeHostId"
              :options="hostOptions"
              placeholder="Select host…"
              @update:model-value="selectHost(Number($event))"
            />
            <input
              v-if="activeHost"
              v-model="search"
              class="base-input dk-search"
              type="search"
              placeholder="Filter…"
            />
            <button v-if="hosts.length" class="base-btn base-btn--sm" @click="toggleOverview">{{ showOverview ? 'Hide overview' : 'Overview' }}</button>
            <button v-if="activeHost" class="base-btn base-btn--sm" @click="toggleDf">{{ showDf ? 'Hide usage' : 'Disk usage' }}</button>
            <button v-if="activeHost" class="base-btn base-btn--primary base-btn--sm" @click="openTopology">🗺 Topology</button>
            <label v-if="activeHost" class="dk-autorefresh" title="Auto-refresh every 5s">
              <input type="checkbox" v-model="autoRefresh" /> Auto
            </label>
            <button v-if="activeHost" class="base-btn base-btn--sm" :disabled="loading" @click="refresh()">Refresh</button>
            <button v-if="activeHost" class="base-btn base-btn--sm" @click="disconnectHost">Disconnect</button>
            <button v-if="activeHost && canManage" class="base-btn base-btn--sm" @click="openEditHost(activeHost)">Edit host</button>
            <button v-if="canManage" class="base-btn base-btn--sm" @click="router.push({ name: 'ssh-hosts' })">Manage hosts</button>
          </div>
        </section>

        <!-- Multi-host overview -->
        <div v-if="showOverview && hosts.length" class="page-card dk-overview">
          <div class="dk-overview-grid">
            <div v-for="h in overview" :key="h.host_id" class="dk-ov-card" @click="selectHost(h.host_id)">
              <div class="dk-ov-head">
                <span class="dk-dot" :class="h.reachable ? 'dk-dot--ok' : 'dk-dot--err'"></span>
                <span class="dk-ov-name">{{ h.name }}</span>
                <span class="dk-muted">{{ h.ssh_host || 'local' }}</span>
              </div>
              <div v-if="h.reachable" class="dk-ov-stats">
                <span><b>{{ h.running }}</b>/{{ h.total }} running</span>
                <span><b>{{ h.images }}</b> images</span>
                <span class="dk-muted">v{{ h.version }}</span>
              </div>
              <div v-else class="dk-ov-err">{{ h.error || 'unreachable' }}</div>
            </div>
            <div v-if="!overview.length" class="dk-muted">Loading host summaries…</div>
          </div>
        </div>

        <!-- Disk usage -->
        <div v-if="showDf && activeHost" class="page-card dk-df">
          <div v-if="dfLoading" class="dk-muted">Calculating disk usage…</div>
          <div v-else-if="diskUsage" class="dk-df-grid">
            <div class="dk-df-card">
              <div class="dk-df-type">Images <span class="dk-muted">×{{ diskUsage.images.count }}</span></div>
              <div class="dk-df-size">{{ formatBytes(diskUsage.images.size) }}</div>
              <div class="dk-df-recl">{{ formatBytes(diskUsage.images.reclaimable) }} reclaimable</div>
            </div>
            <div class="dk-df-card">
              <div class="dk-df-type">Containers <span class="dk-muted">×{{ diskUsage.containers.count }}</span></div>
              <div class="dk-df-size">{{ formatBytes(diskUsage.containers.size) }}</div>
              <div class="dk-df-recl">{{ formatBytes(diskUsage.containers.reclaimable) }} reclaimable</div>
            </div>
            <div class="dk-df-card">
              <div class="dk-df-type">Volumes <span class="dk-muted">×{{ diskUsage.volumes.count }}</span></div>
              <div class="dk-df-size">{{ formatBytes(diskUsage.volumes.size) }}</div>
              <div class="dk-df-recl">{{ formatBytes(diskUsage.volumes.reclaimable) }} reclaimable</div>
            </div>
            <div class="dk-df-card">
              <div class="dk-df-type">Build cache <span class="dk-muted">×{{ diskUsage.build_cache.count }}</span></div>
              <div class="dk-df-size">{{ formatBytes(diskUsage.build_cache.size) }}</div>
              <div class="dk-df-recl">{{ formatBytes(diskUsage.build_cache.reclaimable) }} reclaimable</div>
            </div>
          </div>
        </div>

        <!-- Events -->
        <div v-if="showEvents && activeHost" class="page-card dk-events">
          <div class="dk-events-head">
            <span class="dk-df-type">Recent events (last hour)</span>
            <button class="base-btn base-btn--xs" @click="loadEvents">Refresh</button>
          </div>
          <div v-if="!events.length" class="dk-muted">No events in the last hour.</div>
          <div v-for="(ev, i) in events" :key="i" class="dk-event">
            <span class="dk-event-time">{{ new Date(ev.time * 1000).toLocaleTimeString() }}</span>
            <span class="badge badge--default dk-event-type">{{ ev.type }}</span>
            <span class="dk-event-action">{{ ev.action }}</span>
            <span class="dk-name">{{ ev.name }}</span>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="!hosts.length" class="page-card dk-empty">
          <div class="dk-empty-icon">🐳</div>
          <h2>No Docker hosts yet</h2>
          <p>Connect a server by SSH to browse and control its containers.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="router.push({ name: 'ssh-hosts' })">Add your first host</button>
          <p v-else class="dk-muted">You don't have permission to add Docker hosts.</p>
        </div>

        <template v-else-if="activeHostId !== null">
          <!-- Daemon status + tabs -->
          <div class="dk-bar">
            <div class="dk-connstat">
              <template v-if="daemonInfo">
                <span class="dk-dot dk-dot--ok"></span>
                <span>Connected</span>
                <span class="dk-meta">Docker {{ daemonInfo.version }} · API {{ daemonInfo.api_version }} · {{ daemonInfo.os }}/{{ daemonInfo.arch }}</span>
              </template>
              <template v-else-if="connError">
                <span class="dk-dot dk-dot--err"></span>
                <span class="dk-err">{{ connError }}</span>
              </template>
              <template v-else>
                <span class="dk-dot"></span>
                <span>{{ loading ? 'Connecting…' : 'Select a host' }}</span>
              </template>
            </div>
            <div class="page-tabs">
              <button :class="['page-tab', { 'is-active': tab === 'containers' }]" @click="switchTab('containers')">
                Containers <span class="dk-count">{{ containers.length }}</span>
              </button>
              <button :class="['page-tab', { 'is-active': tab === 'images' }]" @click="switchTab('images')">
                Images <span class="dk-count">{{ images.length }}</span>
              </button>
              <button :class="['page-tab', { 'is-active': tab === 'volumes' }]" @click="switchTab('volumes')">
                Volumes <span class="dk-count">{{ volumes.length }}</span>
              </button>
              <button :class="['page-tab', { 'is-active': tab === 'networks' }]" @click="switchTab('networks')">
                Networks <span class="dk-count">{{ networks.length }}</span>
              </button>
            </div>
          </div>

          <!-- Per-tab actions -->
          <div v-if="!loading && !connError" class="dk-toolbar">
            <template v-if="tab === 'containers'">
              <template v-if="selectedCids.size">
                <span class="dk-name">{{ selectedCids.size }} selected</span>
                <button v-if="canManage" class="base-btn base-btn--xs base-btn--primary" @click="batchAction('start')">Start</button>
                <button v-if="canManage" class="base-btn base-btn--xs" @click="batchAction('restart')">Restart</button>
                <button v-if="canManage" class="base-btn base-btn--xs" @click="batchAction('stop')">Stop</button>
                <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="batchAction('remove')">Remove</button>
                <button class="base-btn base-btn--xs" @click="selectedCids = new Set()">Clear</button>
                <div class="dk-spacer"></div>
              </template>
              <template v-else>
                <label class="dk-autorefresh"><input type="checkbox" v-model="groupByCompose" /> Group by Compose</label>
                <button class="base-btn base-btn--sm" @click="toggleEvents">{{ showEvents ? 'Hide events' : 'Events' }}</button>
                <div class="dk-spacer"></div>
              </template>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneContainers">Prune stopped</button>
              <button v-if="canManage" class="base-btn base-btn--sm" @click="openCompose">Deploy stack</button>
              <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openRun">+ Run container</button>
            </template>
            <template v-else-if="tab === 'images'">
              <input
                v-model="pullImage"
                class="base-input dk-pull-input"
                placeholder="image to pull, e.g. nginx:alpine"
                @keyup.enter="pullImageNow()"
              />
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pulling || !pullImage.trim()" @click="pullImageNow()">{{ pulling ? 'Pulling…' : 'Pull' }}</button>
              <button v-if="canManage" class="base-btn base-btn--sm" title="Pull from a private registry" @click="openPullAuth">🔒</button>
              <div class="dk-spacer"></div>
              <label v-if="canManage" class="base-btn base-btn--sm dk-import-btn">{{ pulling ? 'Importing…' : 'Import' }}<input type="file" accept=".tar" hidden @change="loadImageFile" /></label>
              <button v-if="canManage" class="base-btn base-btn--sm" @click="openBuild">Build image</button>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneImages">Prune dangling</button>
            </template>
            <template v-else-if="tab === 'volumes'">
              <span class="dk-muted">{{ volumes.length }} volume{{ volumes.length === 1 ? '' : 's' }}</span>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneVolumes">Prune unused</button>
              <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="createVolForm = { name: '', driver: '' }; showCreateVol = true">+ Create volume</button>
            </template>
            <template v-else>
              <span class="dk-muted">{{ networks.length }} network{{ networks.length === 1 ? '' : 's' }}</span>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneNetworks">Prune unused</button>
              <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="createNetForm = { name: '', driver: 'bridge' }; showCreateNet = true">+ Create network</button>
            </template>
          </div>

          <div v-if="loading" class="page-card dk-loading">Loading…</div>

          <!-- Containers -->
          <div v-else-if="tab === 'containers'" class="page-card dk-table-wrap">
            <table class="dk-table">
              <thead>
                <tr>
                  <th></th>
                  <th class="dk-th-sort" :class="{ sorted: containerSortKey === 'name' }" @click="toggleContainerSort('name')">Name <SortIcon :active="containerSortKey === 'name'" :dir="containerSortDir" /></th>
                  <th class="dk-th-sort" :class="{ sorted: containerSortKey === 'image' }" @click="toggleContainerSort('image')">Image <SortIcon :active="containerSortKey === 'image'" :dir="containerSortDir" /></th>
                  <th class="dk-th-sort" :class="{ sorted: containerSortKey === 'state' }" @click="toggleContainerSort('state')">State <SortIcon :active="containerSortKey === 'state'" :dir="containerSortDir" /></th>
                  <th class="dk-th-sort" :class="{ sorted: containerSortKey === 'status' }" @click="toggleContainerSort('status')">Status <SortIcon :active="containerSortKey === 'status'" :dir="containerSortDir" /></th>
                  <th>Ports</th>
                  <th class="dk-actions-col">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!filteredContainers.length && !connError">
                  <td colspan="7" class="dk-empty-row">No containers{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <template v-for="group in containerGroups" :key="group.project || '_standalone'">
                  <tr v-if="groupByCompose && group.project" class="dk-group-row">
                    <td colspan="7">
                      <span class="dk-group-badge">compose</span> {{ group.project }}
                      <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger dk-stack-down" @click="removeStack(group.project)">Down</button>
                    </td>
                  </tr>
                  <template v-for="c in group.containers" :key="c.id">
                  <tr>
                    <td class="dk-first-cell">
                      <input type="checkbox" :checked="selectedCids.has(c.id)" @change="toggleSelect(c.id)" />
                      <button class="dk-icon-btn" title="Stats" @click="toggleStats(c)">
                        {{ expanded[c.id] ? '▾' : '▸' }}
                      </button>
                    </td>
                    <td>
                      <div class="dk-name">{{ containerName(c) }}</div>
                      <div class="dk-id">{{ shortId(c.id) }}</div>
                    </td>
                    <td class="dk-image">{{ c.image }}</td>
                    <td><span class="badge" :class="stateBadge(c.state)">{{ c.state }}</span></td>
                    <td class="dk-status">{{ c.status }}</td>
                    <td>
                      <span v-for="(p, i) in c.ports" :key="i" class="dk-port">{{ portLabel(p) }}</span>
                    </td>
                    <td class="dk-actions-col">
                      <button class="base-btn base-btn--xs" @click="openInspect(c)">Details</button>
                      <button class="base-btn base-btn--xs" @click="openLogs(c)">Logs</button>
                      <button v-if="canExec && c.state === 'running'" class="base-btn base-btn--xs" @click="openTerminal(c)">Terminal</button>
                      <button v-if="c.state === 'running'" class="base-btn base-btn--xs" @click="openFiles(c)">Files</button>
                      <template v-if="canManage">
                        <button
                          v-if="c.state !== 'running'"
                          class="base-btn base-btn--xs base-btn--primary"
                          :disabled="busyAction === `${c.id}:start`"
                          @click="containerAction(c, 'start')"
                        >Start</button>
                        <button
                          v-if="c.state === 'running'"
                          class="base-btn base-btn--xs"
                          :disabled="busyAction === `${c.id}:restart`"
                          @click="containerAction(c, 'restart')"
                        >Restart</button>
                        <button
                          v-if="c.state === 'running'"
                          class="base-btn base-btn--xs"
                          :disabled="busyAction === `${c.id}:pause`"
                          @click="containerAction(c, 'pause')"
                        >Pause</button>
                        <button
                          v-if="c.state === 'paused'"
                          class="base-btn base-btn--xs base-btn--primary"
                          :disabled="busyAction === `${c.id}:unpause`"
                          @click="containerAction(c, 'unpause')"
                        >Unpause</button>
                        <button
                          v-if="c.state === 'running' || c.state === 'paused'"
                          class="base-btn base-btn--xs base-btn--danger"
                          :disabled="busyAction === `${c.id}:stop`"
                          @click="containerAction(c, 'stop')"
                        >Stop</button>
                        <button class="base-btn base-btn--xs" @click="openRename(c)">Rename</button>
                        <button
                          class="base-btn base-btn--xs base-btn--danger"
                          @click="removeContainer(c)"
                        >Remove</button>
                      </template>
                    </td>
                  </tr>
                  <tr v-if="expanded[c.id]" class="dk-stats-row">
                    <td></td>
                    <td colspan="6">
                      <div v-if="statsMap[c.id]" class="dk-stats">
                        <div class="dk-stat">
                          <span class="dk-stat-label">CPU</span>
                          <span class="dk-stat-val">{{ statsMap[c.id].cpu_percent.toFixed(1) }}%</span>
                        </div>
                        <div class="dk-stat">
                          <span class="dk-stat-label">Memory</span>
                          <span class="dk-stat-val">
                            {{ formatBytes(statsMap[c.id].mem_usage) }} / {{ formatBytes(statsMap[c.id].mem_limit) }}
                            ({{ statsMap[c.id].mem_percent.toFixed(1) }}%)
                          </span>
                        </div>
                        <div class="dk-stat">
                          <span class="dk-stat-label">Net I/O</span>
                          <span class="dk-stat-val">↓ {{ formatBytes(statsMap[c.id].net_rx) }} · ↑ {{ formatBytes(statsMap[c.id].net_tx) }}</span>
                        </div>
                        <div class="dk-stat">
                          <span class="dk-stat-label">Block I/O</span>
                          <span class="dk-stat-val">R {{ formatBytes(statsMap[c.id].blk_read) }} · W {{ formatBytes(statsMap[c.id].blk_write) }}</span>
                        </div>
                        <div class="dk-stat">
                          <span class="dk-stat-label">PIDs</span>
                          <span class="dk-stat-val">{{ statsMap[c.id].pids }}</span>
                        </div>
                      </div>
                      <div v-if="statsMap[c.id] && statsHistory[c.id]" class="dk-sparks">
                        <div class="dk-spark-card">
                          <div class="dk-spark-head"><span>CPU</span><b>{{ statsMap[c.id].cpu_percent.toFixed(1) }}%</b></div>
                          <svg class="dk-spark" viewBox="0 0 120 30" preserveAspectRatio="none">
                            <path :d="sparkArea(statsHistory[c.id].cpu)" class="dk-spark-area dk-spark--cpu" />
                            <path :d="sparkLine(statsHistory[c.id].cpu)" class="dk-spark-line dk-spark--cpu" />
                          </svg>
                        </div>
                        <div class="dk-spark-card">
                          <div class="dk-spark-head"><span>Memory</span><b>{{ statsMap[c.id].mem_percent.toFixed(1) }}%</b></div>
                          <svg class="dk-spark" viewBox="0 0 120 30" preserveAspectRatio="none">
                            <path :d="sparkArea(statsHistory[c.id].mem, 120, 30, 100)" class="dk-spark-area dk-spark--mem" />
                            <path :d="sparkLine(statsHistory[c.id].mem, 120, 30, 100)" class="dk-spark-line dk-spark--mem" />
                          </svg>
                        </div>
                        <div class="dk-spark-card">
                          <div class="dk-spark-head"><span>Net rate</span><b>{{ formatBytes(statsHistory[c.id].net[statsHistory[c.id].net.length - 1] || 0) }}/s</b></div>
                          <svg class="dk-spark" viewBox="0 0 120 30" preserveAspectRatio="none">
                            <path :d="sparkArea(statsHistory[c.id].net)" class="dk-spark-area dk-spark--net" />
                            <path :d="sparkLine(statsHistory[c.id].net)" class="dk-spark-line dk-spark--net" />
                          </svg>
                        </div>
                      </div>
                      <div v-if="!statsMap[c.id]" class="dk-muted">Loading stats…</div>
                    </td>
                  </tr>
                  </template>
                </template>
              </tbody>
            </table>
          </div>

          <!-- Images -->
          <div v-else-if="tab === 'images'" class="page-card dk-table-wrap">
            <table class="dk-table">
              <thead>
                <tr>
                  <th>Repository : Tag</th>
                  <th>Image ID</th>
                  <th>Size</th>
                  <th>Created</th>
                  <th class="dk-actions-col">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!filteredImages.length && !connError">
                  <td colspan="5" class="dk-empty-row">No images{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <tr v-for="img in filteredImages" :key="img.id">
                  <td>
                    <span v-if="img.repoTags && img.repoTags.length" class="dk-name">{{ img.repoTags.join(', ') }}</span>
                    <span v-else class="dk-muted">&lt;none&gt;</span>
                  </td>
                  <td class="dk-id">{{ shortId(img.id) }}</td>
                  <td>{{ formatBytes(img.size) }}</td>
                  <td class="dk-status">{{ formatRelative(img.created) }}</td>
                  <td class="dk-actions-col">
                    <button class="base-btn base-btn--xs" @click="openHistory(img)">Layers</button>
                    <button class="base-btn base-btn--xs" @click="saveImage(img)">Export</button>
                    <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="removeImage(img)">Remove</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Volumes -->
          <div v-else-if="tab === 'volumes'" class="page-card dk-table-wrap">
            <table class="dk-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Driver</th>
                  <th>Size</th>
                  <th>Used by</th>
                  <th>Mountpoint</th>
                  <th class="dk-actions-col">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!filteredVolumes.length && !connError">
                  <td colspan="6" class="dk-empty-row">No volumes{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <tr v-for="v in filteredVolumes" :key="v.name">
                  <td class="dk-name">{{ v.name }}</td>
                  <td class="dk-status">{{ v.driver }}</td>
                  <td class="dk-status">{{ v.size >= 0 ? formatBytes(v.size) : '—' }}</td>
                  <td class="dk-status">
                    <span v-if="v.refCount < 0" class="dk-muted">—</span>
                    <span v-else :class="{ 'dk-unused': v.refCount === 0 }">{{ v.refCount }} container{{ v.refCount === 1 ? '' : 's' }}</span>
                  </td>
                  <td class="dk-mono dk-id">{{ v.mountpoint }}</td>
                  <td class="dk-actions-col">
                    <button class="base-btn base-btn--xs" @click="openVolInspect(v)">Details</button>
                    <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="removeVolume(v)">Remove</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Networks -->
          <div v-else class="page-card dk-table-wrap">
            <table class="dk-table">
              <thead>
                <tr>
                  <th></th>
                  <th>Name</th>
                  <th>Driver</th>
                  <th>Scope</th>
                  <th>Subnet</th>
                  <th>Containers</th>
                  <th class="dk-actions-col">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!filteredNetworks.length && !connError">
                  <td colspan="7" class="dk-empty-row">No networks{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <template v-for="n in filteredNetworks" :key="n.id">
                  <tr>
                    <td>
                      <button
                        class="dk-icon-btn"
                        title="Connected containers / connect"
                        @click="netExpanded = { ...netExpanded, [n.id]: !netExpanded[n.id] }"
                      >{{ netExpanded[n.id] ? '▾' : '▸' }}</button>
                    </td>
                    <td class="dk-name">{{ n.name }}</td>
                    <td class="dk-status">{{ n.driver }}</td>
                    <td class="dk-status">{{ n.scope }}</td>
                    <td class="dk-mono">{{ n.subnet || '—' }}</td>
                    <td class="dk-status">{{ n.containers }}</td>
                    <td class="dk-actions-col">
                      <button class="base-btn base-btn--xs" @click="openNetInspect(n)">Details</button>
                      <button
                        v-if="canManage && !['bridge', 'host', 'none'].includes(n.name)"
                        class="base-btn base-btn--xs base-btn--danger"
                        @click="removeNetwork(n)"
                      >Remove</button>
                    </td>
                  </tr>
                  <tr v-if="netExpanded[n.id]" class="dk-stats-row">
                    <td></td>
                    <td colspan="6">
                      <div class="dk-netconns">
                        <span v-if="!n.connected || !n.connected.length" class="dk-muted">No containers connected.</span>
                        <span v-for="c in n.connected" :key="c.name" class="dk-netconn">
                          <span class="dk-name">{{ c.name }}</span>
                          <span class="dk-mono dk-muted">{{ c.ipv4 || '—' }}</span>
                          <button v-if="canManage" class="dk-chip-x" title="Disconnect" @click="disconnectNet(n, c.name)">×</button>
                        </span>
                      </div>
                      <div v-if="canManage" class="dk-net-connect">
                        <select v-model="netConnectSel[n.id]" class="base-input dk-net-sel">
                          <option value="">attach a container…</option>
                          <option v-for="c in containers" :key="c.id" :value="containerName(c)">{{ containerName(c) }}</option>
                        </select>
                        <button class="base-btn base-btn--xs" :disabled="!netConnectSel[n.id]" @click="connectNet(n)">Connect</button>
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>

        </template>

        <!-- Idle: hosts exist but none selected (after disconnect) -->
        <div v-else class="page-card dk-idle">
          <div class="dk-idle-icon">🐳</div>
          <p>Select a host from the dropdown above to connect.</p>
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

    <!-- Logs modal -->
    <div v-if="showLogs" class="dk-modal-backdrop" @click.self="closeLogs">
      <div class="dk-modal dk-modal--wide page-card">
        <div class="dk-modal-title">
          Logs — {{ logsTitle }}
          <span class="dk-live-dot" title="Live — streaming over WebSocket"></span>
        </div>
        <div class="dk-logs-bar">
          <select v-model="logsTail" class="base-input dk-logs-tail" @change="connectLogStream">
            <option value="100">100 lines</option>
            <option value="200">200 lines</option>
            <option value="500">500 lines</option>
            <option value="1000">1000 lines</option>
            <option value="all">all</option>
          </select>
          <label class="dk-autorefresh"><input type="checkbox" v-model="logsTimestamps" @change="connectLogStream" /> Timestamps</label>
          <label class="dk-autorefresh"><input type="checkbox" v-model="logsPretty" /> Pretty</label>
          <input v-model="logsSearch" class="base-input dk-logs-search" type="search" placeholder="Filter lines…" />
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="downloadLogs">Download</button>
        </div>
        <pre v-if="!logsPretty" class="dk-logs">{{ logsLoading ? 'Loading…' : logsDisplay }}</pre>
        <div v-else class="dk-logs dk-logs--pretty">
          <div v-if="logsLoading" class="dk-muted">Loading…</div>
          <div v-for="(e, i) in logEntries" :key="i" class="dk-logline" :class="e.level ? 'dk-logline--' + e.level.toLowerCase() : ''">
            <span v-if="e.level" class="dk-loglevel">{{ e.level }}</span>
            <span v-if="e.ts" class="dk-logts">{{ e.ts.slice(11, 23) }}</span>
            <pre v-if="e.json" class="dk-logjson">{{ e.json }}</pre>
            <span v-else class="dk-logtext">{{ e.text }}</span>
            <pre v-if="e.cont.length" class="dk-logcont">{{ e.cont.join('\n') }}</pre>
          </div>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="closeLogs">Close</button>
        </div>
      </div>
    </div>

    <!-- Run container modal -->
    <div v-if="showRunForm" class="dk-modal-backdrop" @click.self="showRunForm = false">
      <div class="dk-modal dk-modal--wide page-card dk-run-modal">
        <div class="dk-modal-title">Run a container</div>
        <div class="dk-form">
          <div class="dk-form-row">
            <label class="dk-grow">Image<input v-model="runForm.image" class="base-input" placeholder="nginx:alpine" /></label>
            <label class="dk-grow">Name (optional)<input v-model="runForm.name" class="base-input" placeholder="my-nginx" /></label>
          </div>
          <label>
            Ports (optional)
            <input v-model="runForm.ports" class="base-input" placeholder="8080:80, 5432:5432/tcp" />
            <span class="dk-field-hint">Comma-separated host:container, optional /proto.</span>
          </label>
          <label>
            Volumes (optional)
            <input v-model="runForm.volumes" class="base-input" placeholder="/data:/var/lib/data, /cfg:/etc/app:ro" />
            <span class="dk-field-hint">Comma-separated host:container, optional :ro.</span>
          </label>
          <label>
            Command (optional)
            <input v-model="runForm.cmd" class="base-input dk-mono" placeholder="override CMD, e.g. sleep 3600" />
          </label>
          <label>
            Environment (optional)
            <textarea v-model="runForm.env" class="base-input dk-textarea" rows="2" placeholder="KEY=value&#10;ANOTHER=value"></textarea>
            <span class="dk-field-hint">One KEY=value per line.</span>
          </label>
          <div class="dk-form-row">
            <label class="dk-grow">
              Network
              <select v-model="runForm.network" class="base-input">
                <option value="">default (bridge)</option>
                <option v-for="n in networks" :key="n.id" :value="n.name">{{ n.name }}</option>
              </select>
            </label>
            <label class="dk-grow">
              Restart policy
              <select v-model="runForm.restartPolicy" class="base-input">
                <option value="no">no</option>
                <option value="on-failure">on-failure</option>
                <option value="unless-stopped">unless-stopped</option>
                <option value="always">always</option>
              </select>
            </label>
          </div>
          <div class="dk-form-row">
            <label class="dk-grow">Memory limit (MB, 0 = none)<input v-model.number="runForm.memoryMb" class="base-input" type="number" min="0" /></label>
            <label class="dk-grow">CPUs (0 = none)<input v-model.number="runForm.cpus" class="base-input" type="number" min="0" step="0.5" /></label>
          </div>
          <label class="dk-checkbox"><input type="checkbox" v-model="runForm.autoPull" /> Pull image automatically if not present</label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showRunForm = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="runSaving" @click="submitRun">{{ runSaving ? 'Starting…' : 'Run' }}</button>
        </div>
      </div>
    </div>

    <!-- Inspect / exec slide-over -->
    <div v-if="inspectOpen" class="dk-slide-backdrop" @click.self="inspectOpen = false">
      <aside class="dk-slide">
        <div class="dk-slide-head">
          <div>
            <div class="dk-slide-title">{{ inspectName }}</div>
            <div class="dk-id">{{ shortId(inspectCid) }}</div>
          </div>
          <button class="dk-icon-btn dk-slide-close" @click="inspectOpen = false">×</button>
        </div>

        <div v-if="inspectLoading" class="dk-loading">Loading…</div>

        <div v-else-if="inspView" class="dk-slide-body">
          <section class="dk-detail">
            <h4>Overview</h4>
            <div class="dk-kv"><span>State</span><span><span class="badge" :class="stateBadge(inspView.status)">{{ inspView.status }}</span></span></div>
            <div class="dk-kv" v-if="inspView.health"><span>Health</span><span>{{ inspView.health }}</span></div>
            <div class="dk-kv"><span>Image</span><span class="dk-mono">{{ inspView.image }}</span></div>
            <div class="dk-kv" v-if="inspView.command"><span>Command</span><span class="dk-mono">{{ inspView.command }}</span></div>
            <div class="dk-kv" v-if="inspView.workdir"><span>Workdir</span><span class="dk-mono">{{ inspView.workdir }}</span></div>
            <div class="dk-kv"><span>Hostname</span><span class="dk-mono">{{ inspView.hostname }}</span></div>
            <div class="dk-kv"><span>Restart</span><span>{{ inspView.restart }}</span></div>
            <div class="dk-kv" v-if="inspView.sizeRw != null"><span>Writable layer</span><span class="dk-mono">{{ formatBytes(inspView.sizeRw) }}</span></div>
            <div class="dk-kv" v-if="inspView.sizeRootFs != null"><span>Virtual size</span><span class="dk-mono">{{ formatBytes(inspView.sizeRootFs) }}</span></div>
            <div class="dk-kv" v-if="inspView.startedAt"><span>Started</span><span class="dk-mono">{{ inspView.startedAt }}</span></div>
            <div class="dk-kv" v-if="inspView.status !== 'running'"><span>Exit code</span><span class="dk-mono">{{ inspView.exitCode }}</span></div>
            <button v-if="canManage" class="base-btn base-btn--xs" style="margin-top: 8px; align-self: flex-start" @click="openCommitInspect">📸 Snapshot to image</button>
          </section>

          <section v-if="topData && topData.processes.length" class="dk-detail">
            <h4>Processes ({{ topData.processes.length }})</h4>
            <div class="dk-top">
              <table class="dk-top-table">
                <thead><tr><th v-for="t in topData.titles" :key="t">{{ t }}</th></tr></thead>
                <tbody>
                  <tr v-for="(p, i) in topData.processes" :key="i"><td v-for="(cell, j) in p" :key="j">{{ cell }}</td></tr>
                </tbody>
              </table>
            </div>
          </section>

          <section v-if="changesData.length" class="dk-detail">
            <h4>Filesystem changes ({{ changesData.length }})</h4>
            <div class="dk-changes">
              <div v-for="(ch, i) in changesData.slice(0, 200)" :key="i" class="dk-change" :class="`dk-change--${changeKind(ch.Kind)}`">
                <span class="dk-change-k">{{ changeKind(ch.Kind)[0].toUpperCase() }}</span>
                <span class="dk-mono">{{ ch.Path }}</span>
              </div>
              <div v-if="changesData.length > 200" class="dk-muted">…{{ changesData.length - 200 }} more</div>
            </div>
          </section>

          <section v-if="inspView.networks.length" class="dk-detail">
            <h4>Networks</h4>
            <div v-for="n in inspView.networks" :key="n.name" class="dk-net">
              <div class="dk-net-name">{{ n.name }}</div>
              <div class="dk-kv"><span>IP</span><span class="dk-mono">{{ n.ip || '—' }}</span></div>
              <div class="dk-kv"><span>Gateway</span><span class="dk-mono">{{ n.gateway || '—' }}</span></div>
            </div>
          </section>

          <section v-if="inspView.mounts.length" class="dk-detail">
            <h4>Mounts</h4>
            <div v-for="(m, i) in inspView.mounts" :key="i" class="dk-mount">
              <span class="dk-mono">{{ m.source }}</span>
              <span class="dk-arrow">→</span>
              <span class="dk-mono">{{ m.destination }}</span>
              <span class="dk-tag">{{ m.rw ? 'rw' : 'ro' }}</span>
            </div>
          </section>

          <section v-if="inspView.env.length" class="dk-detail">
            <h4>Environment</h4>
            <pre class="dk-env">{{ inspView.env.join('\n') }}</pre>
          </section>

          <section v-if="canExec && inspectRunning" class="dk-detail">
            <h4>Exec — run a command</h4>
            <div class="dk-exec-row">
              <input
                v-model="execCmd"
                class="base-input dk-mono"
                placeholder="e.g. ls -la /  ·  ps aux  ·  env"
                @keyup.enter="runExec"
              />
              <button class="base-btn base-btn--sm base-btn--primary" :disabled="execRunning || !execCmd.trim()" @click="runExec">{{ execRunning ? 'Running…' : 'Run' }}</button>
            </div>
            <pre v-if="execOutput" class="dk-logs dk-exec-out">{{ execOutput }}</pre>
            <div v-if="execExit !== null" class="dk-muted">Exit code: {{ execExit }}</div>
          </section>
          <section v-else-if="!inspectRunning" class="dk-detail">
            <p class="dk-muted">Start the container to run commands in it.</p>
          </section>
        </div>
      </aside>
    </div>

    <!-- Rename modal -->
    <div v-if="showRename" class="dk-modal-backdrop" @click.self="showRename = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">Rename container</div>
        <div class="dk-form">
          <label>New name<input v-model="renameValue" class="base-input" @keyup.enter="submitRename" /></label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showRename = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!renameValue.trim()" @click="submitRename">Rename</button>
        </div>
      </div>
    </div>

    <!-- Pull from private registry modal -->
    <div v-if="showPullAuth" class="dk-modal-backdrop" @click.self="showPullAuth = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">Pull private image</div>
        <div class="dk-form">
          <label>Image<input v-model="pullImage" class="base-input" placeholder="registry.example.com/team/app:1.0" /></label>
          <label>Username<input v-model="pullAuth.username" class="base-input" autocomplete="off" /></label>
          <label>Password / token<input v-model="pullAuth.password" class="base-input" type="password" autocomplete="off" /></label>
          <label>Registry (optional)<input v-model="pullAuth.registry" class="base-input" placeholder="registry.example.com" /></label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showPullAuth = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="pulling" @click="pullImageNow(pullAuth)">{{ pulling ? 'Pulling…' : 'Pull' }}</button>
        </div>
      </div>
    </div>

    <!-- Build image modal -->
    <div v-if="showBuild" class="dk-modal-backdrop" @click.self="showBuild = false">
      <div class="dk-modal dk-modal--wide page-card dk-run-modal">
        <div class="dk-modal-title">Build image</div>
        <div class="dk-form">
          <label>Tag<input v-model="buildForm.tag" class="base-input" placeholder="myapp:latest" /></label>
          <label>
            Git URL (optional)
            <input v-model="buildForm.gitUrl" class="base-input" placeholder="https://github.com/user/repo.git#main" />
            <span class="dk-field-hint">Build context from a git repo. Leave blank to use the Dockerfile below.</span>
          </label>
          <label>
            Dockerfile (optional)
            <textarea v-model="buildForm.dockerfile" class="base-input dk-textarea" rows="5" placeholder="FROM alpine&#10;RUN echo hi"></textarea>
            <span class="dk-field-hint">Used when no Git URL is given. No build context (COPY/ADD of local files won't work).</span>
          </label>
          <pre v-if="buildOutput" class="dk-logs dk-build-out">{{ buildOutput }}</pre>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showBuild = false">Close</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="building" @click="submitBuild">{{ building ? 'Building…' : 'Build' }}</button>
        </div>
      </div>
    </div>

    <!-- Create volume modal -->
    <div v-if="showCreateVol" class="dk-modal-backdrop" @click.self="showCreateVol = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">Create volume</div>
        <div class="dk-form">
          <label>Name<input v-model="createVolForm.name" class="base-input" placeholder="my-data" @keyup.enter="createVolume" /></label>
          <label>Driver (optional)<input v-model="createVolForm.driver" class="base-input" placeholder="local" /></label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showCreateVol = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="createVolume">Create</button>
        </div>
      </div>
    </div>

    <!-- Create network modal -->
    <div v-if="showCreateNet" class="dk-modal-backdrop" @click.self="showCreateNet = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">Create network</div>
        <div class="dk-form">
          <label>Name<input v-model="createNetForm.name" class="base-input" placeholder="my-net" @keyup.enter="createNetwork" /></label>
          <label>
            Driver
            <select v-model="createNetForm.driver" class="base-input">
              <option value="bridge">bridge</option>
              <option value="overlay">overlay</option>
              <option value="macvlan">macvlan</option>
              <option value="ipvlan">ipvlan</option>
            </select>
          </label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showCreateNet = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="createNetwork">Create</button>
        </div>
      </div>
    </div>

    <!-- File browser modal -->
    <div v-if="showFiles" class="dk-modal-backdrop" @click.self="showFiles = false">
      <div class="dk-modal dk-modal--wide page-card">
        <div class="dk-modal-title">Files — {{ filesName }}</div>
        <div class="dk-files-bar">
          <button class="base-btn base-btn--xs" :disabled="filesPath === '/'" @click="filesUp">↑ Up</button>
          <span class="dk-mono dk-files-path">{{ filesPath }}</span>
          <div class="dk-spacer"></div>
          <label v-if="canManage" class="base-btn base-btn--xs dk-import-btn">Upload here<input type="file" hidden @change="uploadFile" /></label>
        </div>
        <div class="dk-files">
          <div v-if="filesLoading" class="dk-muted">Loading…</div>
          <div v-else-if="!fileEntries.length" class="dk-muted">Empty directory.</div>
          <div v-for="e in fileEntries" :key="e.name" class="dk-file" :class="{ 'dk-file--dir': e.isDir }">
            <span class="dk-file-name" @click="fileNavigate(e)">{{ e.isDir ? '📁' : '📄' }} {{ e.name }}</span>
            <span class="dk-mono dk-muted dk-file-mode">{{ e.mode }}</span>
            <span class="dk-muted dk-file-size">{{ e.isDir ? '' : formatBytes(e.size) }}</span>
            <button v-if="!e.isDir" class="base-btn base-btn--xs" @click="downloadFile(e)">Download</button>
          </div>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showFiles = false">Close</button>
        </div>
      </div>
    </div>

    <!-- Compose deploy modal -->
    <div v-if="showCompose" class="dk-modal-backdrop" @click.self="showCompose = false">
      <div class="dk-modal dk-modal--wide page-card dk-run-modal">
        <div class="dk-modal-title">Deploy compose stack</div>
        <div class="dk-form">
          <label>Stack name<input v-model="composeForm.name" class="base-input" placeholder="my-stack" /></label>
          <label>
            docker-compose.yml
            <textarea v-model="composeForm.yaml" class="base-input dk-textarea dk-compose-yaml" rows="10" placeholder="services:&#10;  web:&#10;    image: nginx:alpine&#10;    ports:&#10;      - 8080:80"></textarea>
            <span class="dk-field-hint">Runs <code>docker compose up -d</code> on the host. Requires the compose plugin there.</span>
          </label>
          <pre v-if="composeOutput" class="dk-logs dk-build-out">{{ composeOutput }}</pre>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showCompose = false">Close</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="composing" @click="submitCompose">{{ composing ? 'Deploying…' : 'Deploy' }}</button>
        </div>
      </div>
    </div>

    <!-- Commit (snapshot) modal -->
    <div v-if="showCommit" class="dk-modal-backdrop" @click.self="showCommit = false">
      <div class="dk-modal page-card">
        <div class="dk-modal-title">📸 Snapshot container → image</div>
        <div class="dk-form">
          <div class="dk-form-row">
            <label class="dk-grow">Repository<input v-model="commitForm.repo" class="base-input" placeholder="myapp" /></label>
            <label class="dk-grow">Tag<input v-model="commitForm.tag" class="base-input" placeholder="snapshot" /></label>
          </div>
          <label>Comment (optional)<input v-model="commitForm.comment" class="base-input" placeholder="debugging snapshot" /></label>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showCommit = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="submitCommit">Snapshot</button>
        </div>
      </div>
    </div>

    <!-- Image history / layers modal -->
    <div v-if="showHistory" class="dk-modal-backdrop" @click.self="showHistory = false">
      <div class="dk-modal dk-modal--wide page-card">
        <div class="dk-modal-title">Layers — {{ historyImage }}</div>
        <div class="dk-table-wrap" style="max-height: 60vh; overflow-y: auto">
          <table class="dk-table">
            <thead><tr><th>Layer command</th><th>Size</th><th>Age</th></tr></thead>
            <tbody>
              <tr v-if="!historyData.length"><td colspan="3" class="dk-empty-row">Loading…</td></tr>
              <tr v-for="(l, i) in historyData" :key="i">
                <td class="dk-mono" style="font-size: 11px; max-width: 560px; word-break: break-all">{{ cleanLayerCmd(l.CreatedBy) }}</td>
                <td class="dk-status">{{ formatBytes(l.Size) }}</td>
                <td class="dk-status">{{ formatRelative(l.Created) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="dk-modal-actions">
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showHistory = false">Close</button>
        </div>
      </div>
    </div>

    <!-- Network detail slide-over -->
    <div v-if="showNetInspect" class="dk-slide-backdrop" @click.self="showNetInspect = false">
      <aside class="dk-slide">
        <div class="dk-slide-head">
          <div>
            <div class="dk-slide-title">{{ netInspectName }}</div>
            <div class="dk-id">network</div>
          </div>
          <button class="dk-icon-btn dk-slide-close" @click="showNetInspect = false">×</button>
        </div>
        <div v-if="!netInspView" class="dk-loading">Loading…</div>
        <div v-else class="dk-slide-body">
          <section class="dk-detail">
            <h4>Overview</h4>
            <div class="dk-kv"><span>Driver</span><span>{{ netInspView.driver }}</span></div>
            <div class="dk-kv"><span>Scope</span><span>{{ netInspView.scope }}</span></div>
            <div class="dk-kv"><span>ID</span><span class="dk-mono">{{ netInspView.id.slice(0, 20) }}</span></div>
            <div class="dk-kv"><span>Flags</span><span>
              <span v-if="netInspView.internal" class="dk-tag">internal</span>
              <span v-if="netInspView.attachable" class="dk-tag">attachable</span>
              <span v-if="netInspView.ingress" class="dk-tag">ingress</span>
              <span v-if="netInspView.ipv6" class="dk-tag">ipv6</span>
              <span v-if="!netInspView.internal && !netInspView.attachable && !netInspView.ingress && !netInspView.ipv6" class="dk-muted">—</span>
            </span></div>
            <div class="dk-kv" v-if="netInspView.created"><span>Created</span><span class="dk-mono">{{ netInspView.created }}</span></div>
          </section>
          <section v-if="netInspView.ipam.length" class="dk-detail">
            <h4>IPAM</h4>
            <div v-for="(c, i) in netInspView.ipam" :key="i" class="dk-net">
              <div class="dk-kv"><span>Subnet</span><span class="dk-mono">{{ c.subnet || '—' }}</span></div>
              <div class="dk-kv"><span>Gateway</span><span class="dk-mono">{{ c.gateway || '—' }}</span></div>
              <div class="dk-kv" v-if="c.range"><span>IP range</span><span class="dk-mono">{{ c.range }}</span></div>
            </div>
          </section>
          <section v-if="netInspView.containers.length" class="dk-detail">
            <h4>Connected containers ({{ netInspView.containers.length }})</h4>
            <div v-for="c in netInspView.containers" :key="c.name" class="dk-net">
              <div class="dk-net-name">{{ c.name }}</div>
              <div class="dk-kv"><span>IPv4</span><span class="dk-mono">{{ c.ipv4 || '—' }}</span></div>
              <div class="dk-kv" v-if="c.ipv6"><span>IPv6</span><span class="dk-mono">{{ c.ipv6 }}</span></div>
              <div class="dk-kv"><span>MAC</span><span class="dk-mono">{{ c.mac || '—' }}</span></div>
            </div>
          </section>
          <section v-if="netInspView.options.length" class="dk-detail">
            <h4>Options</h4>
            <div v-for="[k, v] in netInspView.options" :key="k" class="dk-kvrow"><span class="dk-kvk">{{ k }}</span><span class="dk-kvv">{{ v }}</span></div>
          </section>
          <section v-if="netInspView.labels.length" class="dk-detail">
            <h4>Labels</h4>
            <div v-for="[k, v] in netInspView.labels" :key="k" class="dk-kvrow"><span class="dk-kvk">{{ k }}</span><span class="dk-kvv">{{ v }}</span></div>
          </section>
        </div>
      </aside>
    </div>

    <!-- Volume detail slide-over -->
    <div v-if="showVolInspect" class="dk-slide-backdrop" @click.self="showVolInspect = false">
      <aside class="dk-slide">
        <div class="dk-slide-head">
          <div>
            <div class="dk-slide-title">{{ volInspectName }}</div>
            <div class="dk-id">volume</div>
          </div>
          <button class="dk-icon-btn dk-slide-close" @click="showVolInspect = false">×</button>
        </div>
        <div v-if="!volInspView" class="dk-loading">Loading…</div>
        <div v-else class="dk-slide-body">
          <section class="dk-detail">
            <h4>Overview</h4>
            <div class="dk-kv"><span>Driver</span><span>{{ volInspView.driver }}</span></div>
            <div class="dk-kv"><span>Scope</span><span>{{ volInspView.scope }}</span></div>
            <div class="dk-kv" v-if="volInspView.size != null"><span>Size</span><span class="dk-mono">{{ volInspView.size >= 0 ? formatBytes(volInspView.size) : '—' }}</span></div>
            <div class="dk-kv" v-if="volInspView.refCount != null"><span>Ref count</span><span>{{ volInspView.refCount >= 0 ? volInspView.refCount : '—' }}</span></div>
            <div class="dk-kv" v-if="volInspView.createdAt"><span>Created</span><span class="dk-mono">{{ volInspView.createdAt }}</span></div>
          </section>
          <section class="dk-detail">
            <h4>Mountpoint</h4>
            <pre class="dk-env">{{ volInspView.mountpoint || '—' }}</pre>
          </section>
          <section v-if="volInspView.usedBy.length" class="dk-detail">
            <h4>Used by ({{ volInspView.usedBy.length }})</h4>
            <div v-for="(u, i) in volInspView.usedBy" :key="i" class="dk-mount">
              <span :class="['dk-dot', u.state === 'running' ? 'dk-dot--ok' : '']"></span>
              <span class="dk-name">{{ u.container }}</span>
              <span class="dk-arrow">→</span>
              <span class="dk-mono">{{ u.destination }}</span>
              <span class="dk-tag">{{ u.rw ? 'rw' : 'ro' }}</span>
            </div>
          </section>
          <section v-else class="dk-detail"><p class="dk-muted">Not mounted by any container.</p></section>
          <section v-if="volInspView.options.length" class="dk-detail">
            <h4>Options</h4>
            <div v-for="[k, v] in volInspView.options" :key="k" class="dk-kvrow"><span class="dk-kvk">{{ k }}</span><span class="dk-kvv">{{ v }}</span></div>
          </section>
          <section v-if="volInspView.status.length" class="dk-detail">
            <h4>Status</h4>
            <div v-for="[k, v] in volInspView.status" :key="k" class="dk-kvrow"><span class="dk-kvk">{{ k }}</span><span class="dk-kvv">{{ v }}</span></div>
          </section>
          <section v-if="volInspView.labels.length" class="dk-detail">
            <h4>Labels</h4>
            <div v-for="[k, v] in volInspView.labels" :key="k" class="dk-kvrow"><span class="dk-kvk">{{ k }}</span><span class="dk-kvv">{{ v }}</span></div>
          </section>
        </div>
      </aside>
    </div>

    <!-- Topology map -->
    <div v-if="showTopology" class="dk-topo-backdrop">
      <div class="dk-topo">
        <div class="dk-topo-head">
          <span class="dk-modal-title dk-term-title">🗺 Topology — {{ activeHost?.name }}</span>
          <div class="dk-topo-legend">
            <span><i class="dk-lg dk-lg--net"></i> Network</span>
            <span><i class="dk-lg dk-lg--con"></i> Container</span>
            <span><i class="dk-lg dk-lg--vol"></i> Volume</span>
          </div>
          <div class="dk-term-head-actions">
            <button class="dk-icon-btn dk-term-font" title="Zoom out" @click="topoZoomBy(0.9)">−</button>
            <button class="dk-icon-btn dk-term-font" title="Zoom in" @click="topoZoomBy(1.1)">+</button>
            <button class="dk-icon-btn dk-term-font" title="Reset" @click="topoFit">⊡</button>
            <button class="dk-icon-btn" title="Refresh" @click="openTopology">⟳</button>
            <button class="dk-icon-btn dk-slide-close" @click="showTopology = false">×</button>
          </div>
        </div>
        <div
          ref="topoCanvasEl"
          class="dk-topo-canvas"
          @wheel="topoWheel"
          @mousedown="topoMouseDown"
          @mousemove="topoMouseMove"
          @mouseup="topoMouseUp"
          @mouseleave="topoMouseUp"
        >
          <div v-if="topoLoading" class="dk-topo-msg">Mapping topology…</div>
          <div v-else-if="!topoNodes.length" class="dk-topo-msg">Nothing to map on this host.</div>
          <svg v-else class="dk-topo-svg" width="100%" height="100%">
            <g :transform="`translate(${topoPan.x},${topoPan.y}) scale(${topoZoom})`">
              <path
                v-for="ed in topoLayout.edges"
                :key="ed.key"
                :d="ed.path"
                class="dk-edge"
                :class="ed.type === 'volume' ? 'dk-edge--vol' : 'dk-edge--net'"
                fill="none"
              />
              <g
                v-for="nd in topoLayout.nodes"
                :key="nd.node.id"
                :transform="`translate(${nd.x},${nd.y})`"
                class="dk-tnode-g"
                @mousedown.stop="startNodeDrag(nd, $event)"
              >
                <rect :width="nd.w" :height="nd.h" rx="8" class="dk-tnode" :class="[topoNodeClass(nd.node), { 'dk-tnode--sel': nd.node.id === selectedNodeId }]" />
                <text x="12" y="20" class="dk-tnode-label">{{ truncate(nd.node.label, nd.node.type === 'container' ? 24 : 18) }}</text>
                <text v-if="nd.node.type === 'container'" x="12" y="37" class="dk-tnode-sub">{{ truncate(nd.node.image, 26) }}</text>
                <text v-if="nd.node.type === 'container' && nd.node.ports && nd.node.ports.length" x="12" y="53" class="dk-tnode-ports">{{ truncate(nd.node.ports.join('  '), 28) }}</text>
                <text v-if="nd.node.type !== 'container'" x="12" y="33" class="dk-tnode-sub">{{ nd.node.driver }}</text>
                <text v-if="nd.node.type === 'container' && nd.node.project" :x="nd.w - 12" y="20" text-anchor="end" class="dk-tnode-proj">{{ truncate(nd.node.project, 12) }}</text>
              </g>
            </g>
          </svg>

          <!-- Selection panel -->
          <div v-if="selectedNode" class="dk-topo-panel" @mousedown.stop @wheel.stop>
            <div class="dk-topo-panel-head">
              <span class="dk-name">{{ selectedNode.label }}</span>
              <button class="dk-icon-btn" @click="selectedNodeId = null">×</button>
            </div>
            <div class="dk-topo-panel-body">
              <div class="dk-kv"><span>Type</span><span style="text-transform: capitalize">{{ selectedNode.type }}</span></div>
              <template v-if="selectedNode.type === 'container'">
                <div class="dk-kv"><span>State</span><span><span class="badge" :class="stateBadge(selectedNode.state || '')">{{ selectedNode.state }}</span></span></div>
                <div class="dk-kv"><span>Image</span><span class="dk-mono">{{ selectedNode.image }}</span></div>
                <div class="dk-kv" v-if="selectedNode.project"><span>Project</span><span>{{ selectedNode.project }}</span></div>
                <div class="dk-kv" v-if="selectedNode.ports && selectedNode.ports.length"><span>Ports</span><span class="dk-mono">{{ selectedNode.ports.join(', ') }}</span></div>
                <div v-if="!selectedTopoContainer" class="dk-muted">Container not in current list (refresh).</div>
                <div v-else class="dk-topo-actions">
                  <button class="base-btn base-btn--xs" @click="topoOpen(openInspect)">Details</button>
                  <button class="base-btn base-btn--xs" @click="topoOpen(openLogs)">Logs</button>
                  <button v-if="canExec && selectedNode.state === 'running'" class="base-btn base-btn--xs" @click="topoOpen(openTerminal)">Terminal</button>
                  <button v-if="selectedNode.state === 'running'" class="base-btn base-btn--xs" @click="topoOpen(openFiles)">Files</button>
                  <button v-if="canManage && selectedNode.state !== 'running'" class="base-btn base-btn--xs base-btn--primary" @click="topoContainerAction('start')">Start</button>
                  <button v-if="canManage && selectedNode.state === 'running'" class="base-btn base-btn--xs" @click="topoContainerAction('restart')">Restart</button>
                  <button v-if="canManage && selectedNode.state === 'running'" class="base-btn base-btn--xs base-btn--danger" @click="topoContainerAction('stop')">Stop</button>
                </div>
              </template>
              <template v-else>
                <div class="dk-kv"><span>Driver</span><span>{{ selectedNode.driver }}</span></div>
              </template>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Interactive terminal modal -->
    <div v-if="showTerminal" class="dk-modal-backdrop" @click.self="closeTerminal">
      <div class="dk-term-modal" :class="{ 'dk-term-modal--full': termFullscreen }">
        <div class="dk-term-head">
          <span class="dk-modal-title dk-term-title">Terminal — {{ termTitle }}</span>
          <div class="dk-term-head-actions">
            <button class="dk-icon-btn dk-term-font" title="Decrease font" @click="changeFont(-1)">A−</button>
            <button class="dk-icon-btn dk-term-font" title="Increase font" @click="changeFont(1)">A+</button>
            <button class="dk-icon-btn" :title="termFullscreen ? 'Exit fullscreen (Esc)' : 'Fullscreen'" @click="toggleTermFullscreen">
              {{ termFullscreen ? '🗗' : '⛶' }}
            </button>
            <button class="dk-icon-btn dk-slide-close" @click="closeTerminal">×</button>
          </div>
        </div>
        <div ref="termEl" class="dk-term"></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dk-host-select { min-width: 200px; }
.dk-muted { color: var(--text-muted); font-size: 12px; }

.dk-empty { text-align: center; padding: 64px 20px; color: var(--text-secondary); }
.dk-empty-icon { font-size: 44px; }
.dk-empty h2 { margin: 12px 0 4px; font-size: 16px; color: var(--text-primary); }
.dk-empty p { font-size: 13px; color: var(--text-muted); margin-bottom: 16px; }
.dk-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.dk-idle-icon { font-size: 22px; }

.dk-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.dk-connstat { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
.dk-meta { color: var(--text-muted); }
.dk-err { color: var(--danger); }
.dk-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-muted); }
.dk-dot--ok { background: var(--success); }
.dk-dot--err { background: var(--danger); }
.dk-count { font-size: 11px; color: var(--text-muted); background: var(--bg-hover); border-radius: 999px; padding: 0 6px; }

.dk-loading { padding: 30px; text-align: center; color: var(--text-muted); }

.dk-table-wrap { padding: 4px 6px; overflow-x: auto; }
.dk-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.dk-table th { text-align: left; padding: 10px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.dk-th-sort { cursor: pointer; user-select: none; }
.dk-table td { padding: 10px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.dk-table tbody tr:last-child td { border-bottom: none; }
.dk-empty-row { text-align: center; color: var(--text-muted); padding: 24px; }
.dk-name { font-weight: 500; color: var(--text-primary); }
.dk-id { font-family: var(--mono); font-size: 11px; color: var(--text-muted); }
.dk-image { font-family: var(--mono); font-size: 12px; color: var(--text-secondary); }
.dk-status { color: var(--text-muted); font-size: 12px; }
.dk-actions-col { text-align: right; white-space: nowrap; }
.dk-actions-col .base-btn { margin-left: 5px; }

.dk-port { display: inline-block; font-family: var(--mono); font-size: 11px; background: var(--bg-hover); border-radius: var(--r-xs); padding: 1px 5px; margin: 1px 3px 1px 0; color: var(--text-secondary); }

.dk-icon-btn { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 12px; }

.dk-stats-row td { background: var(--bg-body); }
.dk-stats { display: flex; gap: 28px; }
.dk-sparks { display: flex; gap: 16px; margin-top: 12px; flex-wrap: wrap; }
.dk-spark-card { width: 150px; }
.dk-spark-head { display: flex; justify-content: space-between; font-size: 11px; color: var(--text-muted); margin-bottom: 3px; }
.dk-spark-head b { color: var(--text-primary); font-family: var(--mono); }
.dk-spark { width: 100%; height: 30px; display: block; }
.dk-spark-line { fill: none; stroke-width: 1.5; }
.dk-spark-area { stroke: none; opacity: 0.15; }
.dk-spark--cpu { stroke: var(--brand); fill: var(--brand); }
.dk-spark--mem { stroke: #6366f1; fill: #6366f1; }
.dk-spark--net { stroke: var(--warning); fill: var(--warning); }
.dk-stat { display: flex; flex-direction: column; gap: 2px; }
.dk-stat-label { font-size: 10px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.dk-stat-val { font-family: var(--mono); font-size: 13px; color: var(--text-primary); }

.dk-host-strip { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.dk-chip { display: inline-flex; align-items: center; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 999px; overflow: hidden; }
.dk-chip--active { border-color: var(--brand); }
.dk-chip-name { background: none; border: none; padding: 4px 12px; font-size: 12px; color: var(--text-secondary); cursor: pointer; }
.dk-chip--active .dk-chip-name { color: var(--brand); }
.dk-chip-x { background: none; border: none; padding: 4px 9px; color: var(--text-muted); cursor: pointer; }
.dk-chip-x:hover { color: var(--danger); }

.dk-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.dk-modal { padding: 22px; width: 440px; max-width: 92vw; }
.dk-modal--wide { width: 760px; }
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
.dk-logs { background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 12px; font-family: var(--mono); font-size: 11px; line-height: 1.5; max-height: 60vh; overflow: auto; white-space: pre-wrap; word-break: break-all; color: var(--text-secondary); }
.dk-field-hint { font-size: 11px; color: var(--text-muted); }

/* Toolbar + controls */
.dk-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.dk-pull-input { min-width: 260px; flex: 1; max-width: 360px; }
.dk-autorefresh { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-secondary); cursor: pointer; }
.dk-live-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--success); margin-left: 8px; vertical-align: middle; animation: dk-pulse 1.6s ease-in-out infinite; }
@keyframes dk-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }

/* Inspect slide-over */
.dk-slide-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45); display: flex; justify-content: flex-end; z-index: 110; }
.dk-slide { width: 480px; max-width: 94vw; height: 100%; background: var(--bg-surface); border-left: 1px solid var(--border); box-shadow: var(--shadow-lg); display: flex; flex-direction: column; overflow: hidden; }
.dk-slide-head { display: flex; align-items: flex-start; justify-content: space-between; padding: 18px 20px; border-bottom: 1px solid var(--border); }
.dk-slide-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.dk-slide-close { font-size: 20px; line-height: 1; }
.dk-slide-body { flex: 1; overflow-y: auto; padding: 8px 20px 24px; }
.dk-detail { padding: 14px 0; border-bottom: 1px solid var(--border); }
.dk-detail:last-child { border-bottom: none; }
.dk-detail h4 { font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); margin: 0 0 10px; font-weight: 600; }
.dk-kv { display: flex; gap: 12px; font-size: 12px; padding: 3px 0; }
.dk-kv > span:first-child { width: 86px; flex-shrink: 0; color: var(--text-muted); }
.dk-kv > span:last-child { color: var(--text-secondary); word-break: break-all; }
.dk-mono { font-family: var(--mono); font-size: 11px; }
.dk-kvrow { display: flex; flex-direction: column; gap: 1px; padding: 5px 0; border-bottom: 1px dashed var(--border); }
.dk-kvrow:last-child { border-bottom: none; }
.dk-kvk { font-family: var(--mono); font-size: 10px; color: var(--text-muted); word-break: break-all; }
.dk-kvv { font-family: var(--mono); font-size: 11px; color: var(--text-primary); word-break: break-all; }
.dk-net { margin-bottom: 10px; }
.dk-net-name { font-size: 12px; font-weight: 500; color: var(--text-primary); margin-bottom: 4px; }
.dk-mount { display: flex; align-items: center; gap: 6px; font-size: 11px; padding: 3px 0; flex-wrap: wrap; }
.dk-arrow { color: var(--text-muted); }
.dk-tag { font-size: 10px; padding: 0 5px; border-radius: var(--r-xs); background: var(--bg-hover); color: var(--text-muted); }
.dk-env { background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 10px; font-family: var(--mono); font-size: 11px; line-height: 1.5; max-height: 200px; overflow: auto; white-space: pre-wrap; word-break: break-all; color: var(--text-secondary); margin: 0; }
.dk-exec-row { display: flex; gap: 8px; }
.dk-exec-out { margin-top: 10px; max-height: 240px; }

/* Compose group rows */
.dk-group-row td { background: var(--bg-body); padding: 7px 12px; font-size: 12px; font-weight: 600; color: var(--text-secondary); }
.dk-group-badge { font-size: 9px; text-transform: uppercase; letter-spacing: 0.06em; background: var(--brand-dim); color: var(--brand); padding: 1px 6px; border-radius: var(--r-xs); margin-right: 6px; }

/* Search + overview */
.dk-search { min-width: 150px; max-width: 200px; }
.dk-overview { padding: 14px 16px; }
.dk-overview-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; }
.dk-ov-card { border: 1px solid var(--border); border-radius: var(--r); padding: 10px 12px; cursor: pointer; transition: border-color var(--dur) var(--ease); }
.dk-ov-card:hover { border-color: var(--brand); }
.dk-ov-head { display: flex; align-items: center; gap: 6px; font-size: 13px; margin-bottom: 6px; }
.dk-ov-name { font-weight: 600; color: var(--text-primary); }
.dk-ov-stats { display: flex; gap: 12px; font-size: 12px; color: var(--text-secondary); flex-wrap: wrap; }
.dk-ov-stats b { color: var(--text-primary); }
.dk-ov-err { font-size: 11px; color: var(--danger); }

/* Disk usage */
.dk-df { padding: 14px 16px; }
.dk-df-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
.dk-df-card { border: 1px solid var(--border); border-radius: var(--r); padding: 12px 14px; }
.dk-df-type { font-size: 12px; color: var(--text-secondary); font-weight: 600; margin-bottom: 6px; }
.dk-df-size { font-size: 20px; font-weight: 700; color: var(--text-primary); }
.dk-df-recl { font-size: 11px; color: var(--warning); margin-top: 3px; }

/* Volume / network detail */
.dk-unused { color: var(--warning); }
.dk-netconns { display: flex; flex-wrap: wrap; gap: 14px; padding: 4px 0; }
.dk-netconn { display: inline-flex; gap: 6px; align-items: baseline; font-size: 12px; }

/* Container internals */
.dk-first-cell { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
.dk-top { overflow-x: auto; }
.dk-top-table { width: 100%; border-collapse: collapse; font-size: 11px; font-family: var(--mono); }
.dk-top-table th { text-align: left; padding: 3px 8px; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.dk-top-table td { padding: 3px 8px; color: var(--text-secondary); white-space: nowrap; }
.dk-changes { max-height: 200px; overflow-y: auto; font-size: 11px; }
.dk-change { display: flex; align-items: center; gap: 8px; padding: 2px 0; }
.dk-change-k { width: 16px; height: 16px; border-radius: 3px; display: inline-flex; align-items: center; justify-content: center; font-size: 9px; font-weight: 700; color: #fff; flex-shrink: 0; }
.dk-change--added .dk-change-k { background: var(--success); }
.dk-change--deleted .dk-change-k { background: var(--danger); }
.dk-change--modified .dk-change-k { background: var(--warning); }
.dk-net-connect { display: flex; gap: 8px; align-items: center; margin-top: 8px; }
.dk-net-sel { max-width: 220px; font-size: 12px; }

/* Events */
.dk-events { padding: 12px 16px; }
.dk-events-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.dk-event { display: flex; align-items: center; gap: 10px; font-size: 12px; padding: 3px 0; border-bottom: 1px solid var(--border); }
.dk-event:last-child { border-bottom: none; }
.dk-event-time { font-family: var(--mono); color: var(--text-muted); width: 90px; flex-shrink: 0; }
.dk-event-type { text-transform: capitalize; }
.dk-event-action { color: var(--text-secondary); width: 90px; }

/* Stack down + import */
.dk-stack-down { margin-left: 10px; }
.dk-import-btn { cursor: pointer; }

/* File browser */
.dk-files-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.dk-files-path { color: var(--text-secondary); }
.dk-files { border: 1px solid var(--border); border-radius: var(--r-sm); max-height: 50vh; overflow-y: auto; padding: 4px; }
.dk-file { display: flex; align-items: center; gap: 12px; padding: 4px 8px; border-radius: var(--r-xs); font-size: 12px; }
.dk-file:hover { background: var(--bg-hover); }
.dk-file-name { flex: 1; }
.dk-file--dir .dk-file-name { cursor: pointer; color: var(--brand); }
.dk-file-mode { font-size: 11px; }
.dk-file-size { width: 70px; text-align: right; }
.dk-compose-yaml { font-family: var(--mono); }

/* Run modal + checkbox */
.dk-run-modal { max-height: 88vh; overflow-y: auto; }
.dk-checkbox { flex-direction: row !important; align-items: center; gap: 7px !important; cursor: pointer; }

/* Logs controls */
.dk-logs-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.dk-logs-tail { width: 110px; }
.dk-logs-search { max-width: 200px; }
.dk-logs--pretty { font-family: var(--mono); font-size: 11px; line-height: 1.55; max-height: 60vh; overflow: auto; padding: 8px 10px; background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); white-space: normal; }
.dk-logline { display: flex; align-items: baseline; gap: 8px; padding: 1px 6px; border-left: 2px solid transparent; flex-wrap: wrap; }
.dk-logline:hover { background: var(--bg-hover); }
.dk-loglevel { font-weight: 700; font-size: 9px; padding: 0 4px; border-radius: var(--r-xs); flex-shrink: 0; }
.dk-logts { color: var(--text-muted); flex-shrink: 0; }
.dk-logtext { color: var(--text-secondary); word-break: break-all; flex: 1; }
.dk-logjson { color: var(--text-secondary); margin: 2px 0; white-space: pre-wrap; word-break: break-all; flex-basis: 100%; background: var(--bg-elevated); border-radius: var(--r-xs); padding: 4px 8px; }
.dk-logcont { color: var(--text-muted); margin: 0; white-space: pre-wrap; word-break: break-all; flex-basis: 100%; padding-left: 12px; }
.dk-logline--error { border-left-color: var(--danger); }
.dk-logline--error .dk-loglevel { background: var(--danger-bg); color: var(--danger); }
.dk-logline--error .dk-logtext { color: var(--danger); }
.dk-logline--fatal { border-left-color: var(--danger); background: var(--danger-bg); }
.dk-logline--fatal .dk-loglevel { background: var(--danger); color: #fff; }
.dk-logline--warn { border-left-color: var(--warning); }
.dk-logline--warn .dk-loglevel { background: var(--warning-bg); color: var(--warning); }
.dk-logline--info .dk-loglevel { background: var(--success-bg); color: var(--success); }
.dk-logline--debug .dk-loglevel, .dk-logline--trace .dk-loglevel { background: var(--bg-hover); color: var(--text-muted); }

/* Topology map */
.dk-topo-backdrop { position: fixed; inset: 0; background: var(--bg-body); z-index: 1200; display: flex; }
.dk-topo { flex: 1; background: var(--bg-surface); display: flex; flex-direction: column; overflow: hidden; }
.dk-topo-head { display: flex; align-items: center; gap: 18px; padding: 12px 16px; border-bottom: 1px solid var(--border); background: var(--bg-elevated); }
.dk-topo-legend { display: flex; gap: 14px; font-size: 11px; color: var(--text-muted); }
.dk-topo-legend span { display: inline-flex; align-items: center; gap: 5px; }
.dk-lg { width: 11px; height: 11px; border-radius: 3px; display: inline-block; }
.dk-lg--net { background: #6366f1; }
.dk-lg--con { background: var(--success); }
.dk-lg--vol { background: #f59e0b; }
.dk-topo-head .dk-term-head-actions { margin-left: auto; }
.dk-topo-canvas { position: relative; flex: 1; min-height: 0; overflow: hidden; cursor: grab; background:
  radial-gradient(circle, rgba(255,255,255,0.04) 1px, transparent 1px); background-size: 22px 22px; background-color: var(--bg-body); }
.dk-topo-canvas:active { cursor: grabbing; }
.dk-tnode-g { cursor: pointer; }
.dk-tnode--sel { stroke-width: 3; }

.dk-topo-panel { position: absolute; top: 14px; right: 14px; width: 270px; max-height: calc(100% - 28px); overflow-y: auto; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); cursor: default; }
.dk-topo-panel-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-bottom: 1px solid var(--border); }
.dk-topo-panel-body { padding: 12px 14px; display: flex; flex-direction: column; gap: 4px; }
.dk-topo-actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.dk-topo-actions .base-btn { margin: 0; }
.dk-topo-msg { padding: 40px; text-align: center; color: var(--text-muted); }
.dk-topo-svg { display: block; user-select: none; }

.dk-edge { stroke-width: 1.5; opacity: 0.55; }
.dk-edge--net { stroke: #6366f1; }
.dk-edge--vol { stroke: #f59e0b; stroke-dasharray: 4 3; }

.dk-tnode { fill: var(--bg-elevated); stroke: var(--border-2); stroke-width: 1.5; }
.dk-tnode--running { stroke: var(--success); }
.dk-tnode--exited { stroke: var(--danger); }
.dk-tnode--paused { stroke: var(--warning); }
.dk-tnode--net { fill: rgba(99, 102, 241, 0.12); stroke: #6366f1; }
.dk-tnode--vol { fill: rgba(245, 158, 11, 0.1); stroke: #f59e0b; }
.dk-tnode-label { font-size: 13px; font-weight: 600; fill: var(--text-primary); }
.dk-tnode-sub { font-size: 11px; fill: var(--text-muted); font-family: var(--mono); }
.dk-tnode-ports { font-size: 10px; fill: var(--brand); font-family: var(--mono); }
.dk-tnode-proj { font-size: 9px; fill: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; }

/* Terminal modal */
.dk-term-modal { width: 900px; max-width: 94vw; height: 560px; max-height: 86vh; background: #0d1117; border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); display: flex; flex-direction: column; overflow: hidden; }
.dk-term-modal--full { width: 100vw; max-width: 100vw; height: 100vh; max-height: 100vh; border-radius: 0; border: none; }
.dk-term-head { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; background: var(--bg-elevated); border-bottom: 1px solid var(--border); }
.dk-term-head-actions { display: flex; align-items: center; gap: 4px; }
.dk-term-font { font-size: 12px; font-weight: 600; padding: 2px 5px; }
.dk-build-out { margin-top: 6px; max-height: 280px; }
.dk-term-title { margin: 0; }
.dk-term { flex: 1; min-height: 0; padding: 8px 10px; overflow: hidden; }
.dk-term :deep(.xterm) { height: 100%; }
</style>
