<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import axios from 'axios'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'

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
}
interface DockerNetwork {
  name: string
  id: string
  driver: string
  scope: string
  subnet: string
  containers: number
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

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['docker.manage']))
const canExec = computed(() => hasAnyPermission(['docker.exec']))

const hosts = ref<DockerHost[]>([])
const activeHostId = ref<number | null>(null)
const activeHost = computed(() => hosts.value.find((h) => h.id === activeHostId.value) ?? null)

const tab = ref<'containers' | 'images' | 'volumes' | 'networks'>('containers')
const containers = ref<DockerContainer[]>([])
const images = ref<DockerImage[]>([])
const volumes = ref<DockerVolume[]>([])
const networks = ref<DockerNetwork[]>([])
const groupByCompose = ref(true)
const search = ref('')

// ── Multi-host overview ─────────────────────────────────────────
const overview = ref<HostSummary[]>([])
const showOverview = ref(false)

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
const logsSearch = ref('')
let logsTimer: ReturnType<typeof setInterval> | undefined

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

// ── Auto-refresh ────────────────────────────────────────────────
const autoRefresh = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | undefined

// ── Terminal ────────────────────────────────────────────────────
const showTerminal = ref(false)
const termFullscreen = ref(false)
const termTitle = ref('')
const termEl = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let termSocket: WebSocket | null = null

// ── Data loading ────────────────────────────────────────────────
async function loadHosts() {
  try {
    const { data } = await axios.get<DockerHost[]>('/api/docker/hosts')
    hosts.value = data
    if (activeHostId.value === null && data.length) {
      activeHostId.value = data[0].id
      await refresh()
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load Docker hosts')
  }
}

async function selectHost(id: number) {
  if (activeHostId.value === id) return
  activeHostId.value = id
  statsMap.value = {}
  expanded.value = {}
  await refresh()
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

async function toggleStats(c: DockerContainer) {
  const open = !expanded.value[c.id]
  expanded.value = { ...expanded.value, [c.id]: open }
  if (open && !statsMap.value[c.id]) {
    try {
      const { data } = await axios.get<ContainerStats>(
        `/api/docker/hosts/${activeHostId.value}/containers/${c.id}/stats`,
      )
      statsMap.value = { ...statsMap.value, [c.id]: data }
    } catch (e: any) {
      toast.error(e?.response?.data?.error || 'Failed to load stats')
    }
  }
}

async function fetchLogs() {
  if (!logsCid.value) return
  try {
    const { data } = await axios.get<string>(
      `/api/docker/hosts/${activeHostId.value}/containers/${logsCid.value}/logs`,
      { params: { tail: logsTail.value, timestamps: logsTimestamps.value ? 1 : 0 }, responseType: 'text' },
    )
    logsText.value = data || '(no log output)'
  } catch (e: any) {
    logsText.value = e?.response?.data || 'Failed to load logs'
  } finally {
    logsLoading.value = false
  }
}

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

async function openLogs(c: DockerContainer) {
  logsTitle.value = containerName(c)
  logsCid.value = c.id
  logsText.value = ''
  logsLoading.value = true
  showLogs.value = true
  await fetchLogs()
  // Poll while the modal is open for a near-live tail.
  if (logsTimer) clearInterval(logsTimer)
  logsTimer = setInterval(fetchLogs, 2500)
}

function closeLogs() {
  showLogs.value = false
  logsCid.value = ''
  if (logsTimer) {
    clearInterval(logsTimer)
    logsTimer = undefined
  }
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
async function pullImageNow() {
  const img = pullImage.value.trim()
  if (!img) return
  pulling.value = true
  try {
    await axios.post(`/api/docker/hosts/${activeHostId.value}/images/pull`, { image: img })
    toast.success(`Pulled ${img}`)
    pullImage.value = ''
    await loadImages()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to pull image')
  } finally {
    pulling.value = false
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
    fontSize: 13,
    fontFamily: "'JetBrains Mono', Menlo, monospace",
    theme: { background: '#0d1117' },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  fitAddon.fit()
  window.addEventListener('resize', refitTerminal)

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
const filteredContainers = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return containers.value
  return containers.value.filter(
    (c) => containerName(c).toLowerCase().includes(q) || c.image.toLowerCase().includes(q),
  )
})
const filteredImages = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return images.value
  return images.value.filter((i) => (i.repoTags || []).join(' ').toLowerCase().includes(q))
})
const filteredVolumes = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return volumes.value
  return volumes.value.filter((v) => v.name.toLowerCase().includes(q))
})
const filteredNetworks = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return networks.value
  return networks.value.filter((n) => n.name.toLowerCase().includes(q))
})

// ── Compose grouping ────────────────────────────────────────────
const containerGroups = computed(() => {
  const list = filteredContainers.value
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
  if (logsTimer) clearInterval(logsTimer)
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
            <select
              v-if="hosts.length"
              class="base-input dk-host-select"
              :value="activeHostId ?? ''"
              @change="selectHost(Number(($event.target as HTMLSelectElement).value))"
            >
              <option v-for="h in hosts" :key="h.id" :value="h.id">{{ hostLabel(h) }}</option>
            </select>
            <input
              v-if="activeHost"
              v-model="search"
              class="base-input dk-search"
              type="search"
              placeholder="Filter…"
            />
            <button v-if="hosts.length" class="base-btn base-btn--sm" @click="toggleOverview">{{ showOverview ? 'Hide overview' : 'Overview' }}</button>
            <label v-if="activeHost" class="dk-autorefresh" title="Auto-refresh every 5s">
              <input type="checkbox" v-model="autoRefresh" /> Auto
            </label>
            <button v-if="activeHost" class="base-btn base-btn--sm" :disabled="loading" @click="refresh()">Refresh</button>
            <button v-if="activeHost && canManage" class="base-btn base-btn--sm" @click="openEditHost(activeHost)">Edit host</button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openAddHost">+ Add host</button>
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

        <!-- Empty state -->
        <div v-if="!hosts.length" class="page-card dk-empty">
          <div class="dk-empty-icon">🐳</div>
          <h2>No Docker hosts yet</h2>
          <p>Connect a server by SSH to browse and control its containers.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="openAddHost">Add your first host</button>
          <p v-else class="dk-muted">You don't have permission to add Docker hosts.</p>
        </div>

        <template v-else>
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
              <label class="dk-autorefresh"><input type="checkbox" v-model="groupByCompose" /> Group by Compose</label>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneContainers">Prune stopped</button>
              <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openRun">+ Run container</button>
            </template>
            <template v-else-if="tab === 'images'">
              <input
                v-model="pullImage"
                class="base-input dk-pull-input"
                placeholder="image to pull, e.g. nginx:alpine"
                @keyup.enter="pullImageNow"
              />
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pulling || !pullImage.trim()" @click="pullImageNow">{{ pulling ? 'Pulling…' : 'Pull' }}</button>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneImages">Prune dangling</button>
            </template>
            <template v-else-if="tab === 'volumes'">
              <span class="dk-muted">{{ volumes.length }} volume{{ volumes.length === 1 ? '' : 's' }}</span>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneVolumes">Prune unused</button>
            </template>
            <template v-else>
              <span class="dk-muted">{{ networks.length }} network{{ networks.length === 1 ? '' : 's' }}</span>
              <div class="dk-spacer"></div>
              <button v-if="canManage" class="base-btn base-btn--sm" :disabled="pruning" @click="pruneNetworks">Prune unused</button>
            </template>
          </div>

          <div v-if="loading" class="page-card dk-loading">Loading…</div>

          <!-- Containers -->
          <div v-else-if="tab === 'containers'" class="page-card dk-table-wrap">
            <table class="dk-table">
              <thead>
                <tr>
                  <th></th>
                  <th>Name</th>
                  <th>Image</th>
                  <th>State</th>
                  <th>Status</th>
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
                    <td colspan="7"><span class="dk-group-badge">compose</span> {{ group.project }}</td>
                  </tr>
                  <template v-for="c in group.containers" :key="c.id">
                  <tr>
                    <td>
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
                      <div v-else class="dk-muted">Loading stats…</div>
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
                  <th>Mountpoint</th>
                  <th class="dk-actions-col">Actions</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!filteredVolumes.length && !connError">
                  <td colspan="4" class="dk-empty-row">No volumes{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <tr v-for="v in filteredVolumes" :key="v.name">
                  <td class="dk-name">{{ v.name }}</td>
                  <td class="dk-status">{{ v.driver }}</td>
                  <td class="dk-mono dk-id">{{ v.mountpoint }}</td>
                  <td class="dk-actions-col">
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
                  <td colspan="6" class="dk-empty-row">No networks{{ search ? ' match' : ' on this host' }}.</td>
                </tr>
                <tr v-for="n in filteredNetworks" :key="n.id">
                  <td class="dk-name">{{ n.name }}</td>
                  <td class="dk-status">{{ n.driver }}</td>
                  <td class="dk-status">{{ n.scope }}</td>
                  <td class="dk-mono">{{ n.subnet || '—' }}</td>
                  <td class="dk-status">{{ n.containers }}</td>
                  <td class="dk-actions-col">
                    <button
                      v-if="canManage && !['bridge', 'host', 'none'].includes(n.name)"
                      class="base-btn base-btn--xs base-btn--danger"
                      @click="removeNetwork(n)"
                    >Remove</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Host chips -->
          <div class="dk-host-strip">
            <span class="dk-muted">Hosts:</span>
            <span v-for="h in hosts" :key="h.id" class="dk-chip" :class="{ 'dk-chip--active': h.id === activeHostId }">
              <button class="dk-chip-name" @click="selectHost(h.id)">{{ h.name }}</button>
              <button v-if="canManage" class="dk-chip-x" title="Delete host" @click="deleteHost(h)">×</button>
            </span>
          </div>
        </template>
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
          <span class="dk-live-dot" title="Live tail — refreshes every 2.5s"></span>
        </div>
        <div class="dk-logs-bar">
          <select v-model="logsTail" class="base-input dk-logs-tail" @change="fetchLogs">
            <option value="100">100 lines</option>
            <option value="200">200 lines</option>
            <option value="500">500 lines</option>
            <option value="1000">1000 lines</option>
            <option value="all">all</option>
          </select>
          <label class="dk-autorefresh"><input type="checkbox" v-model="logsTimestamps" @change="fetchLogs" /> Timestamps</label>
          <input v-model="logsSearch" class="base-input dk-logs-search" type="search" placeholder="Filter lines…" />
          <div class="dk-spacer"></div>
          <button class="base-btn base-btn--sm" @click="downloadLogs">Download</button>
        </div>
        <pre class="dk-logs">{{ logsLoading ? 'Loading…' : logsDisplay }}</pre>
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
            <div class="dk-kv" v-if="inspView.startedAt"><span>Started</span><span class="dk-mono">{{ inspView.startedAt }}</span></div>
            <div class="dk-kv" v-if="inspView.status !== 'running'"><span>Exit code</span><span class="dk-mono">{{ inspView.exitCode }}</span></div>
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

    <!-- Interactive terminal modal -->
    <div v-if="showTerminal" class="dk-modal-backdrop" @click.self="closeTerminal">
      <div class="dk-term-modal" :class="{ 'dk-term-modal--full': termFullscreen }">
        <div class="dk-term-head">
          <span class="dk-modal-title dk-term-title">Terminal — {{ termTitle }}</span>
          <div class="dk-term-head-actions">
            <button class="dk-icon-btn" :title="termFullscreen ? 'Exit fullscreen' : 'Fullscreen'" @click="toggleTermFullscreen">
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

/* Run modal + checkbox */
.dk-run-modal { max-height: 88vh; overflow-y: auto; }
.dk-checkbox { flex-direction: row !important; align-items: center; gap: 7px !important; cursor: pointer; }

/* Logs controls */
.dk-logs-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.dk-logs-tail { width: 110px; }
.dk-logs-search { max-width: 200px; }

/* Terminal modal */
.dk-term-modal { width: 900px; max-width: 94vw; height: 560px; max-height: 86vh; background: #0d1117; border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); display: flex; flex-direction: column; overflow: hidden; }
.dk-term-modal--full { width: 100vw; max-width: 100vw; height: 100vh; max-height: 100vh; border-radius: 0; border: none; }
.dk-term-head { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; background: var(--bg-elevated); border-bottom: 1px solid var(--border); }
.dk-term-head-actions { display: flex; align-items: center; gap: 4px; }
.dk-term-title { margin: 0; }
.dk-term { flex: 1; min-height: 0; padding: 8px 10px; overflow: hidden; }
.dk-term :deep(.xterm) { height: 100%; }
</style>
