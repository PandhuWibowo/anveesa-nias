<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import axios from 'axios'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import SearchSelect from '@/components/ui/SearchSelect.vue'
import SortIcon from '@/components/ui/SortIcon.vue'
import { useKubeClusters } from '@/composables/useKubeClusters'
import { useListFilter } from '@/composables/useListFilter'
import { useSort } from '@/composables/useSort'

type KubeTab =
  | 'overview' | 'nodes' | 'namespaces' | 'pods' | 'deployments'
  | 'statefulsets' | 'daemonsets' | 'jobs' | 'cronjobs'
  | 'services' | 'ingresses' | 'configmaps' | 'secrets' | 'pvcs' | 'events'

interface KNode { name: string; status: string; roles: string; version: string; os: string; cpu: string; memory: string; internal_ip: string; created: string }
interface KNamespace { name: string; status: string; created: string }
interface KPod { name: string; namespace: string; status: string; ready: string; restarts: number; node: string; pod_ip: string; created: string; containers: string[] }
interface KDeployment { name: string; namespace: string; ready: string; up_to_date: number; available: number; created: string; images: string[] }
interface KStatefulSet { name: string; namespace: string; ready: string; created: string; images: string[] }
interface KDaemonSet { name: string; namespace: string; desired: number; current: number; ready: number; created: string }
interface KJob { name: string; namespace: string; completions: string; status: string; created: string }
interface KCronJob { name: string; namespace: string; schedule: string; suspend: boolean; active: number; last_schedule: string; created: string }
interface KService { name: string; namespace: string; type: string; cluster_ip: string; external_ip: string; ports: string; created: string }
interface KIngress { name: string; namespace: string; class: string; hosts: string[]; address: string; created: string }
interface KConfigMap { name: string; namespace: string; keys: string[]; created: string }
interface KSecret { name: string; namespace: string; type: string; keys: string[]; created: string }
interface KPVC { name: string; namespace: string; status: string; volume: string; capacity: string; storage_class: string; created: string }
interface KEvent { namespace: string; type: string; reason: string; object: string; message: string; count: number; last_seen: string }

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['kube.manage']))
const canExec = computed(() => hasAnyPermission(['kube.exec']))

const { clusters, fetchClusters } = useKubeClusters()
const clusterOptions = computed(() => clusters.value.map((c) => ({ value: c.id, label: `${c.name} (${providerShort(c.provider)})` })))

const clusterId = ref<number | null>(null)
const status = ref<'unknown' | 'connecting' | 'connected' | 'error'>('unknown')
const version = ref('')
const connError = ref('')
const tab = ref<KubeTab>('overview')
const loading = ref(false)

const namespaceFilter = ref('')

// Data
const nodes = ref<KNode[]>([])
const namespaces = ref<KNamespace[]>([])
const pods = ref<KPod[]>([])
const deployments = ref<KDeployment[]>([])
const statefulsets = ref<KStatefulSet[]>([])
const daemonsets = ref<KDaemonSet[]>([])
const jobs = ref<KJob[]>([])
const cronjobs = ref<KCronJob[]>([])
const services = ref<KService[]>([])
const ingresses = ref<KIngress[]>([])
const configmaps = ref<KConfigMap[]>([])
const secrets = ref<KSecret[]>([])
const pvcs = ref<KPVC[]>([])
const events = ref<KEvent[]>([])

// metrics-server usage (keyed by node name / "ns/pod")
interface Usage { cpu_milli: number; mem_bytes: number }
const nodeMetrics = ref<Record<string, Usage>>({})
const podMetrics = ref<Record<string, Usage>>({})
const metricsAvailable = ref(true)

// Auto-refresh
const autoRefresh = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | null = null

// Pod detail drawer
interface PodDetailContainer { name: string; image: string; ready: boolean; restarts: number; state: string; cpu_request: string; cpu_limit: string; mem_request: string; mem_limit: string }
interface PodDetail { name: string; namespace: string; node: string; phase: string; pod_ip: string; qos: string; start_time: string; containers: PodDetailContainer[]; events: KEvent[] }
const showPodDetail = ref(false)
const podDetail = ref<PodDetail | null>(null)
const podDetailLoading = ref(false)

// Pod logs modal
const showLogs = ref(false)
const logsPod = ref<KPod | null>(null)
const logsContainer = ref('')
const logsTail = ref('300')
const logsText = ref('')
const logsLoading = ref(false)
const logsFollow = ref(false)
const logViewEl = ref<HTMLElement | null>(null)
let logsWS: WebSocket | null = null

// Describe drawer
const showDescribe = ref(false)
const describeTitle = ref('')
const describeYaml = ref('')
const describeLoading = ref(false)

// Exec terminal
const showExec = ref(false)
const execPod = ref<KPod | null>(null)
const execContainer = ref('')
const execShell = ref('/bin/sh')
const termEl = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let execWS: WebSocket | null = null

const LAST_CLUSTER_KEY = 'nias:kube:lastCluster'

// Grouped resource navigation (left sidebar).
const NAV_GROUPS: { title: string; items: { key: KubeTab; label: string }[] }[] = [
  { title: 'Cluster', items: [
    { key: 'overview', label: 'Overview' },
    { key: 'nodes', label: 'Nodes' },
    { key: 'namespaces', label: 'Namespaces' },
  ] },
  { title: 'Workloads', items: [
    { key: 'pods', label: 'Pods' },
    { key: 'deployments', label: 'Deployments' },
    { key: 'statefulsets', label: 'StatefulSets' },
    { key: 'daemonsets', label: 'DaemonSets' },
    { key: 'jobs', label: 'Jobs' },
    { key: 'cronjobs', label: 'CronJobs' },
  ] },
  { title: 'Network', items: [
    { key: 'services', label: 'Services' },
    { key: 'ingresses', label: 'Ingresses' },
  ] },
  { title: 'Config & Storage', items: [
    { key: 'configmaps', label: 'ConfigMaps' },
    { key: 'secrets', label: 'Secrets' },
    { key: 'pvcs', label: 'PVCs' },
  ] },
  { title: 'Events', items: [
    { key: 'events', label: 'Events' },
  ] },
]
const TAB_LABELS: Record<string, string> = Object.fromEntries(
  NAV_GROUPS.flatMap((g) => g.items.map((i) => [i.key, i.label])),
)

// Namespace-scoped resources (show the namespace filter for these).
const SCOPED = new Set<KubeTab>(['pods', 'deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs', 'services', 'ingresses', 'configmaps', 'secrets', 'pvcs', 'events'])
const isScoped = computed(() => SCOPED.has(tab.value))

function providerShort(p: string): string {
  return p === 'alibaba' ? 'ACK' : p === 'huawei' ? 'CCE' : 'k8s'
}

const nsOptions = computed(() => [
  { value: '', label: 'All namespaces' },
  ...namespaces.value.map((n) => ({ value: n.name, label: n.name })),
])

const base = computed(() => `/api/kube/clusters/${clusterId.value}`)
function nsQuery(): string {
  return namespaceFilter.value ? `?namespace=${encodeURIComponent(namespaceFilter.value)}` : ''
}
function wsToken(): string { return localStorage.getItem('nias-token') || '' }
function wsProto(): string { return location.protocol === 'https:' ? 'wss' : 'ws' }

// ── Connection ──────────────────────────────────────────────────
async function loadClusters() {
  await fetchClusters()
  if (clusterId.value === null && clusters.value.length) {
    const queryId = route.query.cluster ? Number(route.query.cluster) : null
    const queryCluster = queryId ? clusters.value.find((c) => c.id === queryId) : null
    if (queryCluster) { await selectCluster(queryCluster.id); return }
    const saved = localStorage.getItem(LAST_CLUSTER_KEY)
    if (saved === 'disconnected') return
    const savedId = saved ? Number(saved) : null
    const target = (savedId && clusters.value.find((c) => c.id === savedId)) || clusters.value[0]
    await selectCluster(target.id)
  }
}

async function selectCluster(id: number) {
  clusterId.value = id
  localStorage.setItem(LAST_CLUSTER_KEY, String(id))
  status.value = 'connecting'
  version.value = ''
  connError.value = ''
  namespaceFilter.value = ''
  try {
    const { data } = await axios.get<{ version: string }>(`/api/kube/clusters/${id}/ping`)
    version.value = data.version
    status.value = 'connected'
    await loadNamespaces()
    await loadTab()
  } catch (e: any) {
    status.value = 'error'
    connError.value = e?.response?.data?.error || 'Could not reach the cluster'
  }
}

function disconnect() {
  stopAuto()
  clusterId.value = null
  status.value = 'unknown'
  version.value = ''
  connError.value = ''
  localStorage.setItem(LAST_CLUSTER_KEY, 'disconnected')
  nodes.value = []; namespaces.value = []; pods.value = []; deployments.value = []
  statefulsets.value = []; daemonsets.value = []; jobs.value = []; cronjobs.value = []
  services.value = []; ingresses.value = []; configmaps.value = []; secrets.value = []
  pvcs.value = []; events.value = []
  nodeMetrics.value = {}; podMetrics.value = {}
}

async function loadNamespaces() {
  const { data } = await axios.get<KNamespace[]>(`${base.value}/namespaces`)
  namespaces.value = data ?? []
}

async function loadTab() {
  if (clusterId.value === null) return
  loading.value = true
  connError.value = ''
  try {
    switch (tab.value) {
      case 'overview':
        await Promise.all([loadNodes(), namespaces.value.length ? Promise.resolve() : loadNamespaces(), loadPods(), loadDeployments(), loadServices()])
        break
      case 'nodes': await loadNodes(); break
      case 'namespaces': await loadNamespaces(); break
      case 'pods': await loadPods(); break
      case 'deployments': await loadDeployments(); break
      case 'statefulsets': await loadList('statefulsets', statefulsets); break
      case 'daemonsets': await loadList('daemonsets', daemonsets); break
      case 'jobs': await loadList('jobs', jobs); break
      case 'cronjobs': await loadList('cronjobs', cronjobs); break
      case 'services': await loadServices(); break
      case 'ingresses': await loadList('ingresses', ingresses); break
      case 'configmaps': await loadList('configmaps', configmaps); break
      case 'secrets': await loadList('secrets', secrets); break
      case 'pvcs': await loadList('pvcs', pvcs); break
      case 'events': await loadEvents(); break
    }
  } catch (e: any) {
    connError.value = e?.response?.data?.error || 'Failed to load resources'
  } finally {
    loading.value = false
  }
}

async function loadNodes() {
  const { data } = await axios.get<KNode[]>(`${base.value}/nodes`)
  nodes.value = data ?? []
  loadNodeMetrics()
}
async function loadPods() {
  const { data } = await axios.get<KPod[]>(`${base.value}/pods${nsQuery()}`)
  pods.value = data ?? []
  loadPodMetrics()
}
async function loadDeployments() { const { data } = await axios.get<KDeployment[]>(`${base.value}/deployments${nsQuery()}`); deployments.value = data ?? [] }
async function loadServices() { const { data } = await axios.get<KService[]>(`${base.value}/services${nsQuery()}`); services.value = data ?? [] }
async function loadEvents() { const { data } = await axios.get<KEvent[]>(`${base.value}/events${nsQuery()}`); events.value = data ?? [] }

// Metrics are best-effort — metrics-server may not be installed.
async function loadNodeMetrics() {
  try {
    const { data } = await axios.get<{ available: boolean; items: (Usage & { name: string })[] }>(`${base.value}/metrics/nodes`)
    metricsAvailable.value = data.available
    const m: Record<string, Usage> = {}
    for (const it of data.items || []) m[it.name] = { cpu_milli: it.cpu_milli, mem_bytes: it.mem_bytes }
    nodeMetrics.value = m
  } catch { /* ignore */ }
}
async function loadPodMetrics() {
  try {
    const { data } = await axios.get<{ available: boolean; items: (Usage & { name: string; namespace: string })[] }>(`${base.value}/metrics/pods${nsQuery()}`)
    metricsAvailable.value = data.available
    const m: Record<string, Usage> = {}
    for (const it of data.items || []) m[`${it.namespace}/${it.name}`] = { cpu_milli: it.cpu_milli, mem_bytes: it.mem_bytes }
    podMetrics.value = m
  } catch { /* ignore */ }
}
// Generic loader for the remaining namespace-scoped list kinds.
async function loadList(resource: string, target: { value: any[] }) {
  const { data } = await axios.get<any[]>(`${base.value}/${resource}${nsQuery()}`)
  target.value = data ?? []
}

function switchTab(t: KubeTab) {
  tab.value = t
  search.value = ''
  loadTab()
}
function onNamespaceChange() {
  if (tab.value !== 'nodes' && tab.value !== 'namespaces') loadTab()
}

// ── Describe (YAML) ─────────────────────────────────────────────
async function describe(kind: string, namespace: string, name: string) {
  showDescribe.value = true
  describeTitle.value = `${kind}/${name}`
  describeYaml.value = ''
  describeLoading.value = true
  try {
    const q = new URLSearchParams({ kind, name })
    if (namespace) q.set('namespace', namespace)
    const { data } = await axios.get<{ yaml: string }>(`${base.value}/describe?${q.toString()}`)
    describeYaml.value = data.yaml || '(empty)'
  } catch (e: any) {
    describeYaml.value = e?.response?.data?.error || 'Failed to load object'
  } finally {
    describeLoading.value = false
  }
}
function copyDescribe() {
  navigator.clipboard?.writeText(describeYaml.value)
  toast.success('YAML copied')
}

// ── Pod logs (snapshot + follow) ────────────────────────────────
async function openLogs(p: KPod) {
  logsPod.value = p
  logsContainer.value = p.containers[0] || ''
  logsTail.value = '300'
  logsText.value = ''
  logsFollow.value = false
  showLogs.value = true
  await fetchLogs()
}

async function fetchLogs() {
  if (!logsPod.value) return
  stopFollow()
  logsLoading.value = true
  try {
    const p = logsPod.value
    const q = new URLSearchParams({ tail: logsTail.value })
    if (logsContainer.value) q.set('container', logsContainer.value)
    const { data } = await axios.get<{ logs: string }>(
      `${base.value}/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}/logs?${q.toString()}`,
    )
    logsText.value = data.logs || '(no output)'
  } catch (e: any) {
    logsText.value = e?.response?.data?.error || 'Failed to load logs'
  } finally {
    logsLoading.value = false
  }
}

function toggleFollow() {
  if (logsFollow.value) { stopFollow(); return }
  startFollow()
}
function startFollow() {
  if (!logsPod.value || clusterId.value === null) return
  const p = logsPod.value
  const q = new URLSearchParams({ token: wsToken(), tail: logsTail.value })
  if (logsContainer.value) q.set('container', logsContainer.value)
  const url = `${wsProto()}://${location.host}${base.value}/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}/logstream?${q.toString()}`
  logsText.value = ''
  logsFollow.value = true
  logsWS = new WebSocket(url)
  logsWS.onmessage = (e) => {
    if (typeof e.data === 'string') {
      logsText.value += e.data
      nextTick(() => { if (logViewEl.value) logViewEl.value.scrollTop = logViewEl.value.scrollHeight })
    }
  }
  logsWS.onclose = () => { logsFollow.value = false }
  logsWS.onerror = () => { logsFollow.value = false }
}
function stopFollow() {
  logsFollow.value = false
  if (logsWS) { logsWS.close(); logsWS = null }
}
function closeLogs() {
  stopFollow()
  showLogs.value = false
}

// ── Exec terminal ───────────────────────────────────────────────
function openExec(p: KPod) {
  execPod.value = p
  execContainer.value = p.containers[0] || ''
  execShell.value = '/bin/sh'
  showExec.value = true
  nextTick(() => startExec())
}

function startExec() {
  if (!execPod.value || clusterId.value === null || !termEl.value) return
  disposeTerminal()
  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'var(--mono), Menlo, monospace',
    theme: { background: '#0d1117' },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  fitAddon.fit()

  const p = execPod.value
  const q = new URLSearchParams({ token: wsToken(), shell: execShell.value })
  if (execContainer.value) q.set('container', execContainer.value)
  const url = `${wsProto()}://${location.host}${base.value}/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}/exec?${q.toString()}`
  execWS = new WebSocket(url)
  execWS.binaryType = 'arraybuffer'
  execWS.onopen = () => sendResize()
  execWS.onmessage = (e) => {
    if (e.data instanceof ArrayBuffer) term?.write(new Uint8Array(e.data))
    else term?.write(e.data)
  }
  execWS.onclose = () => term?.write('\r\n\x1b[33m[session closed]\x1b[0m\r\n')
  execWS.onerror = () => term?.write('\r\n\x1b[31m[connection error]\x1b[0m\r\n')

  term.onData((d) => { if (execWS?.readyState === WebSocket.OPEN) execWS.send(new TextEncoder().encode(d)) })
  term.onResize(() => sendResize())
  window.addEventListener('resize', onWinResize)
}
function sendResize() {
  if (execWS?.readyState === WebSocket.OPEN && term) {
    execWS.send(JSON.stringify({ cols: term.cols, rows: term.rows }))
  }
}
function onWinResize() { fitAddon?.fit() }
function restartExec() { startExec() }
function disposeTerminal() {
  window.removeEventListener('resize', onWinResize)
  if (execWS) { execWS.close(); execWS = null }
  if (term) { term.dispose(); term = null }
  fitAddon = null
}
function closeExec() {
  disposeTerminal()
  showExec.value = false
}

// ── Filtering + formatting ──────────────────────────────────────
// One search box drives one list at a time (only the active tab's table is
// shown, and switching tabs resets the query) — but each resource type has
// its own array + matcher, so each gets its own useListFilter instance and a
// writable `search` computed fans the single input out to all of them.
function fieldsMatch(q: string, ...fields: string[]): boolean {
  return fields.some((f) => (f || '').toLowerCase().includes(q))
}
const { search: searchNodes, filtered: filteredNodes } = useListFilter(nodes, (n, q) => fieldsMatch(q, n.name, n.roles, n.internal_ip))
const { search: searchNamespaces, filtered: filteredNamespaces } = useListFilter(namespaces, (n, q) => fieldsMatch(q, n.name))
const { search: searchPods, filtered: filteredPods } = useListFilter(pods, (p, q) => fieldsMatch(q, p.name, p.namespace, p.node, p.status))
const { search: searchDeployments, filtered: filteredDeployments } = useListFilter(deployments, (d, q) => fieldsMatch(q, d.name, d.namespace))
const { search: searchStatefulSets, filtered: filteredStatefulSets } = useListFilter(statefulsets, (d, q) => fieldsMatch(q, d.name, d.namespace))
const { search: searchDaemonSets, filtered: filteredDaemonSets } = useListFilter(daemonsets, (d, q) => fieldsMatch(q, d.name, d.namespace))
const { search: searchJobs, filtered: filteredJobs } = useListFilter(jobs, (j, q) => fieldsMatch(q, j.name, j.namespace, j.status))
const { search: searchCronJobs, filtered: filteredCronJobs } = useListFilter(cronjobs, (j, q) => fieldsMatch(q, j.name, j.namespace, j.schedule))
const { search: searchServices, filtered: filteredServices } = useListFilter(services, (s, q) => fieldsMatch(q, s.name, s.namespace, s.type))
const { search: searchIngresses, filtered: filteredIngresses } = useListFilter(ingresses, (i, q) => fieldsMatch(q, i.name, i.namespace, i.hosts.join(',')))
const { search: searchConfigMaps, filtered: filteredConfigMaps } = useListFilter(configmaps, (c, q) => fieldsMatch(q, c.name, c.namespace))
const { search: searchSecrets, filtered: filteredSecrets } = useListFilter(secrets, (s, q) => fieldsMatch(q, s.name, s.namespace, s.type))
const { search: searchPVCs, filtered: filteredPVCs } = useListFilter(pvcs, (p, q) => fieldsMatch(q, p.name, p.namespace, p.status))
const { search: searchEvents, filtered: filteredEvents } = useListFilter(events, (e, q) => fieldsMatch(q, e.reason, e.object, e.message, e.namespace))
const search = computed<string>({
  get: () => searchNodes.value,
  set: (v: string) => {
    searchNodes.value = v
    searchNamespaces.value = v
    searchPods.value = v
    searchDeployments.value = v
    searchStatefulSets.value = v
    searchDaemonSets.value = v
    searchJobs.value = v
    searchCronJobs.value = v
    searchServices.value = v
    searchIngresses.value = v
    searchConfigMaps.value = v
    searchSecrets.value = v
    searchPVCs.value = v
    searchEvents.value = v
  },
})

// ── Sort (pods + deployments — the most-used resource tables) ───
function podSortValue(p: KPod, key: string): unknown {
  switch (key) {
    case 'name': return p.name.toLowerCase()
    case 'namespace': return p.namespace.toLowerCase()
    case 'status': return p.status.toLowerCase()
    case 'restarts': return p.restarts
    case 'node': return p.node.toLowerCase()
    case 'created': return p.created
    default: return ''
  }
}
const { sortKey: podSortKey, sortDir: podSortDir, toggleSort: togglePodSort, sort: sortPods } = useSort<KPod>(podSortValue)
const sortedPods = computed(() => sortPods(filteredPods.value))

function deploymentSortValue(d: KDeployment, key: string): unknown {
  switch (key) {
    case 'name': return d.name.toLowerCase()
    case 'namespace': return d.namespace.toLowerCase()
    case 'ready': return d.ready
    case 'created': return d.created
    default: return ''
  }
}
const { sortKey: deploymentSortKey, sortDir: deploymentSortDir, toggleSort: toggleDeploymentSort, sort: sortDeployments } = useSort<KDeployment>(deploymentSortValue)
const sortedDeployments = computed(() => sortDeployments(filteredDeployments.value))

function relAge(iso: string): string {
  if (!iso) return '-'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return '-'
  const diff = (Date.now() - t) / 1000
  if (diff < 60) return `${Math.floor(diff)}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}
function podClass(s: string): string {
  if (s === 'Running' || s === 'Succeeded') return 'k8s-pill--ok'
  if (s === 'Pending') return 'k8s-pill--warn'
  return 'k8s-pill--err'
}

// ── Metrics formatting ──────────────────────────────────────────
function fmtCPU(milli?: number): string {
  if (milli == null) return '—'
  if (milli < 1000) return `${milli}m`
  return `${(milli / 1000).toFixed(milli % 1000 === 0 ? 0 : 1)}`
}
function fmtMem(bytes?: number): string {
  if (!bytes) return '—'
  const u = ['B', 'Ki', 'Mi', 'Gi', 'Ti']
  let v = bytes, i = 0
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(i ? 1 : 0)}${u[i]}`
}
function cpuCoresToMilli(s: string): number { const f = parseFloat(s); return isNaN(f) ? 0 : f * 1000 }
function memToBytes(s: string): number {
  const m = String(s).match(/^([0-9.]+)\s*([A-Za-z]*)$/)
  if (!m) return 0
  const map: Record<string, number> = { '': 1, Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4, k: 1e3, M: 1e6, G: 1e9, T: 1e12 }
  return parseFloat(m[1]) * (map[m[2]] ?? 1)
}
function pct(used: number, cap: number): number { return cap > 0 ? Math.round((used / cap) * 100) : 0 }
function pctClass(p: number): string { return p >= 90 ? 'k8s-pct--hi' : p >= 70 ? 'k8s-pct--mid' : '' }

// ── Pod detail drawer ───────────────────────────────────────────
async function openPodDetail(p: KPod) {
  showPodDetail.value = true
  podDetail.value = null
  podDetailLoading.value = true
  try {
    const { data } = await axios.get<PodDetail>(`${base.value}/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}/detail`)
    podDetail.value = data
  } catch (e: any) {
    connError.value = e?.response?.data?.error || 'Failed to load pod detail'
    showPodDetail.value = false
  } finally {
    podDetailLoading.value = false
  }
}

// ── Auto-refresh ────────────────────────────────────────────────
function toggleAuto() {
  autoRefresh.value = !autoRefresh.value
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
  if (autoRefresh.value) {
    refreshTimer = setInterval(() => {
      if (clusterId.value !== null && status.value === 'connected' && !loading.value) loadTab()
    }, 5000)
  }
}
function stopAuto() {
  autoRefresh.value = false
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
}

const overviewCounts = computed(() => ({
  nodes: nodes.value.length,
  namespaces: namespaces.value.length,
  pods: pods.value.length,
  runningPods: pods.value.filter((p) => p.status === 'Running').length,
  deployments: deployments.value.length,
  services: services.value.length,
}))

// Title + item count shown in the content header for the active resource.
const currentTitle = computed(() => TAB_LABELS[tab.value] || '')
const currentCount = computed<number>(() => {
  switch (tab.value) {
    case 'nodes': return filteredNodes.value.length
    case 'namespaces': return filteredNamespaces.value.length
    case 'pods': return filteredPods.value.length
    case 'deployments': return filteredDeployments.value.length
    case 'statefulsets': return filteredStatefulSets.value.length
    case 'daemonsets': return filteredDaemonSets.value.length
    case 'jobs': return filteredJobs.value.length
    case 'cronjobs': return filteredCronJobs.value.length
    case 'services': return filteredServices.value.length
    case 'ingresses': return filteredIngresses.value.length
    case 'configmaps': return filteredConfigMaps.value.length
    case 'secrets': return filteredSecrets.value.length
    case 'pvcs': return filteredPVCs.value.length
    case 'events': return filteredEvents.value.length
    default: return -1
  }
})

onMounted(loadClusters)
onBeforeUnmount(() => { stopFollow(); disposeTerminal(); stopAuto() })
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Kubernetes</div>
            <div class="page-subtitle">Browse clusters, workloads, services, events, and pod logs across Alibaba ACK and Huawei CCE.</div>
          </div>
          <div class="page-hero__actions">
            <SearchSelect
              v-if="clusters.length"
              class="k8s-cluster-select"
              :model-value="clusterId"
              :options="clusterOptions"
              placeholder="Select cluster…"
              @update:model-value="selectCluster(Number($event))"
            />
            <div v-if="clusterId !== null" class="k8s-conn" :class="`k8s-conn--${status}`">
              <span class="k8s-conn-dot"></span>
              <span>{{ status === 'connected' ? 'Connected' : status === 'connecting' ? 'Connecting…' : status === 'error' ? 'Error' : 'Idle' }}</span>
            </div>
            <button v-if="clusterId !== null" class="base-btn base-btn--sm" :disabled="loading" @click="loadTab">Refresh</button>
            <button v-if="clusterId !== null" class="base-btn base-btn--sm" @click="disconnect">Disconnect</button>
            <button v-if="canManage" class="base-btn base-btn--sm" @click="router.push({ name: 'kube-clusters' })">Manage clusters</button>
          </div>
        </section>

        <!-- No clusters -->
        <div v-if="!clusters.length" class="page-card k8s-empty">
          <div class="k8s-empty-icon">☸️</div>
          <h2>No clusters yet</h2>
          <p>Add an Alibaba ACK, Huawei CCE, or any Kubernetes cluster under <b>Kubernetes Clusters</b>.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="router.push({ name: 'kube-clusters' })">Add your first cluster</button>
        </div>

        <!-- Connected -->
        <template v-else-if="clusterId !== null">
          <div class="k8s-shell">
            <!-- Resource sidebar -->
            <aside class="k8s-side">
              <div class="k8s-side-ver">
                <span class="k8s-dot" :class="status === 'connected' ? 'k8s-dot--ok' : 'k8s-dot--err'"></span>
                <span v-if="version" class="k8s-side-vertxt">{{ version }}</span>
                <span v-else class="k8s-muted">connecting…</span>
              </div>
              <div v-for="g in NAV_GROUPS" :key="g.title" class="k8s-side-group">
                <div class="k8s-side-grouptitle">{{ g.title }}</div>
                <button
                  v-for="it in g.items"
                  :key="it.key"
                  class="k8s-side-item"
                  :class="{ 'k8s-side-item--active': tab === it.key }"
                  @click="switchTab(it.key)"
                >{{ it.label }}</button>
              </div>
            </aside>

            <!-- Content -->
            <div class="k8s-content">
              <div class="k8s-content-head">
                <h3 class="k8s-content-title">
                  {{ currentTitle }}
                  <span v-if="currentCount >= 0" class="k8s-count">{{ currentCount }}</span>
                </h3>
                <div class="k8s-spacer"></div>
                <label class="k8s-auto" title="Auto-refresh every 5s">
                  <input type="checkbox" :checked="autoRefresh" @change="toggleAuto" /> Auto
                </label>
                <SearchSelect
                  v-if="isScoped && namespaces.length"
                  class="k8s-ns-select"
                  :model-value="namespaceFilter"
                  :options="nsOptions"
                  placeholder="All namespaces"
                  @update:model-value="namespaceFilter = String($event); onNamespaceChange()"
                />
                <input v-if="tab !== 'overview'" v-model="search" class="base-input k8s-search" type="search" placeholder="Filter…" />
              </div>

              <div v-if="connError && status === 'error'" class="page-card k8s-conn-err">{{ connError }}</div>

              <!-- Overview -->
              <div v-else-if="tab === 'overview'" class="k8s-cards">
                <div class="k8s-kpi"><div class="k8s-kpi-val">{{ overviewCounts.nodes }}</div><div class="k8s-kpi-label">Nodes</div></div>
                <div class="k8s-kpi"><div class="k8s-kpi-val">{{ overviewCounts.namespaces }}</div><div class="k8s-kpi-label">Namespaces</div></div>
                <div class="k8s-kpi"><div class="k8s-kpi-val">{{ overviewCounts.runningPods }}<span class="k8s-kpi-sub">/{{ overviewCounts.pods }}</span></div><div class="k8s-kpi-label">Pods running</div></div>
                <div class="k8s-kpi"><div class="k8s-kpi-val">{{ overviewCounts.deployments }}</div><div class="k8s-kpi-label">Deployments</div></div>
                <div class="k8s-kpi"><div class="k8s-kpi-val">{{ overviewCounts.services }}</div><div class="k8s-kpi-label">Services</div></div>
              </div>

              <!-- Tables -->
              <div v-else class="page-card k8s-table-wrap">
                <div v-if="loading" class="k8s-msg">Loading…</div>

            <table v-else-if="tab === 'nodes'" class="k8s-table">
              <thead><tr><th>Name</th><th>Status</th><th>Roles</th><th>Version</th><th>CPU</th><th>Memory</th><th>Internal IP</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredNodes.length"><td colspan="9" class="k8s-msg">No nodes.</td></tr>
                <tr v-for="n in filteredNodes" :key="n.name">
                  <td class="k8s-mono">{{ n.name }}</td>
                  <td><span class="k8s-pill" :class="n.status === 'Ready' ? 'k8s-pill--ok' : 'k8s-pill--err'">{{ n.status }}</span></td>
                  <td>{{ n.roles }}</td>
                  <td class="k8s-mono">{{ n.version }}</td>
                  <td>
                    <template v-if="nodeMetrics[n.name]">
                      {{ fmtCPU(nodeMetrics[n.name].cpu_milli) }} <span class="k8s-dim">/ {{ n.cpu }}</span>
                      <span class="k8s-pct" :class="pctClass(pct(nodeMetrics[n.name].cpu_milli, cpuCoresToMilli(n.cpu)))">{{ pct(nodeMetrics[n.name].cpu_milli, cpuCoresToMilli(n.cpu)) }}%</span>
                    </template>
                    <template v-else>{{ n.cpu }}</template>
                  </td>
                  <td>
                    <template v-if="nodeMetrics[n.name]">
                      {{ fmtMem(nodeMetrics[n.name].mem_bytes) }} <span class="k8s-dim">/ {{ fmtMem(memToBytes(n.memory)) }}</span>
                      <span class="k8s-pct" :class="pctClass(pct(nodeMetrics[n.name].mem_bytes, memToBytes(n.memory)))">{{ pct(nodeMetrics[n.name].mem_bytes, memToBytes(n.memory)) }}%</span>
                    </template>
                    <template v-else>{{ fmtMem(memToBytes(n.memory)) }}</template>
                  </td>
                  <td class="k8s-mono">{{ n.internal_ip }}</td>
                  <td>{{ relAge(n.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('node', '', n.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'namespaces'" class="k8s-table">
              <thead><tr><th>Name</th><th>Status</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredNamespaces.length"><td colspan="4" class="k8s-msg">No namespaces.</td></tr>
                <tr v-for="n in filteredNamespaces" :key="n.name">
                  <td class="k8s-mono">{{ n.name }}</td>
                  <td><span class="k8s-pill" :class="n.status === 'Active' ? 'k8s-pill--ok' : 'k8s-pill--warn'">{{ n.status }}</span></td>
                  <td>{{ relAge(n.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('namespace', '', n.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'pods'" class="k8s-table">
              <thead><tr>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'name' }" @click="togglePodSort('name')">Name <SortIcon :active="podSortKey === 'name'" :dir="podSortDir" /></th>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'namespace' }" @click="togglePodSort('namespace')">Namespace <SortIcon :active="podSortKey === 'namespace'" :dir="podSortDir" /></th>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'status' }" @click="togglePodSort('status')">Status <SortIcon :active="podSortKey === 'status'" :dir="podSortDir" /></th>
                <th>Ready</th>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'restarts' }" @click="togglePodSort('restarts')">Restarts <SortIcon :active="podSortKey === 'restarts'" :dir="podSortDir" /></th>
                <th>CPU</th>
                <th>Memory</th>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'node' }" @click="togglePodSort('node')">Node <SortIcon :active="podSortKey === 'node'" :dir="podSortDir" /></th>
                <th class="k8s-th-sort" :class="{ sorted: podSortKey === 'created' }" @click="togglePodSort('created')">Age <SortIcon :active="podSortKey === 'created'" :dir="podSortDir" /></th>
                <th></th>
              </tr></thead>
              <tbody>
                <tr v-if="!filteredPods.length"><td colspan="10" class="k8s-msg">No pods.</td></tr>
                <tr v-for="p in sortedPods" :key="p.namespace + '/' + p.name">
                  <td class="k8s-mono"><button class="k8s-link" @click="openPodDetail(p)">{{ p.name }}</button></td>
                  <td>{{ p.namespace }}</td>
                  <td><span class="k8s-pill" :class="podClass(p.status)">{{ p.status }}</span></td>
                  <td>{{ p.ready }}</td>
                  <td :class="{ 'k8s-warn': p.restarts > 0 }">{{ p.restarts }}</td>
                  <td class="k8s-mono">{{ fmtCPU(podMetrics[p.namespace + '/' + p.name]?.cpu_milli) }}</td>
                  <td class="k8s-mono">{{ fmtMem(podMetrics[p.namespace + '/' + p.name]?.mem_bytes) }}</td>
                  <td class="k8s-mono">{{ p.node }}</td>
                  <td>{{ relAge(p.created) }}</td>
                  <td class="k8s-act">
                    <button class="k8s-ico" title="Pod details" @click="openPodDetail(p)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg></button>
                    <button class="k8s-ico" title="View logs" @click="openLogs(p)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="14" y2="13"/><line x1="8" y1="17" x2="12" y2="17"/></svg></button>
                    <button v-if="canExec" class="k8s-ico" title="Exec into pod" @click="openExec(p)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg></button>
                    <button class="k8s-ico" title="View YAML" @click="describe('pod', p.namespace, p.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button>
                  </td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'deployments'" class="k8s-table">
              <thead><tr>
                <th class="k8s-th-sort" :class="{ sorted: deploymentSortKey === 'name' }" @click="toggleDeploymentSort('name')">Name <SortIcon :active="deploymentSortKey === 'name'" :dir="deploymentSortDir" /></th>
                <th class="k8s-th-sort" :class="{ sorted: deploymentSortKey === 'namespace' }" @click="toggleDeploymentSort('namespace')">Namespace <SortIcon :active="deploymentSortKey === 'namespace'" :dir="deploymentSortDir" /></th>
                <th class="k8s-th-sort" :class="{ sorted: deploymentSortKey === 'ready' }" @click="toggleDeploymentSort('ready')">Ready <SortIcon :active="deploymentSortKey === 'ready'" :dir="deploymentSortDir" /></th>
                <th>Up-to-date</th>
                <th>Available</th>
                <th>Image</th>
                <th class="k8s-th-sort" :class="{ sorted: deploymentSortKey === 'created' }" @click="toggleDeploymentSort('created')">Age <SortIcon :active="deploymentSortKey === 'created'" :dir="deploymentSortDir" /></th>
                <th></th>
              </tr></thead>
              <tbody>
                <tr v-if="!filteredDeployments.length"><td colspan="8" class="k8s-msg">No deployments.</td></tr>
                <tr v-for="d in sortedDeployments" :key="d.namespace + '/' + d.name">
                  <td class="k8s-mono">{{ d.name }}</td>
                  <td>{{ d.namespace }}</td>
                  <td>{{ d.ready }}</td>
                  <td>{{ d.up_to_date }}</td>
                  <td>{{ d.available }}</td>
                  <td class="k8s-mono k8s-img">{{ d.images.join(', ') }}</td>
                  <td>{{ relAge(d.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('deployment', d.namespace, d.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'statefulsets'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Ready</th><th>Image</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredStatefulSets.length"><td colspan="6" class="k8s-msg">No statefulsets.</td></tr>
                <tr v-for="d in filteredStatefulSets" :key="d.namespace + '/' + d.name">
                  <td class="k8s-mono">{{ d.name }}</td>
                  <td>{{ d.namespace }}</td>
                  <td>{{ d.ready }}</td>
                  <td class="k8s-mono k8s-img">{{ d.images.join(', ') }}</td>
                  <td>{{ relAge(d.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('statefulset', d.namespace, d.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'daemonsets'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Desired</th><th>Current</th><th>Ready</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredDaemonSets.length"><td colspan="7" class="k8s-msg">No daemonsets.</td></tr>
                <tr v-for="d in filteredDaemonSets" :key="d.namespace + '/' + d.name">
                  <td class="k8s-mono">{{ d.name }}</td>
                  <td>{{ d.namespace }}</td>
                  <td>{{ d.desired }}</td>
                  <td>{{ d.current }}</td>
                  <td>{{ d.ready }}</td>
                  <td>{{ relAge(d.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('daemonset', d.namespace, d.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'jobs'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Completions</th><th>Status</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredJobs.length"><td colspan="6" class="k8s-msg">No jobs.</td></tr>
                <tr v-for="j in filteredJobs" :key="j.namespace + '/' + j.name">
                  <td class="k8s-mono">{{ j.name }}</td>
                  <td>{{ j.namespace }}</td>
                  <td>{{ j.completions }}</td>
                  <td><span class="k8s-pill" :class="j.status === 'Complete' ? 'k8s-pill--ok' : j.status === 'Failed' ? 'k8s-pill--err' : 'k8s-pill--warn'">{{ j.status }}</span></td>
                  <td>{{ relAge(j.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('job', j.namespace, j.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'cronjobs'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Schedule</th><th>Suspend</th><th>Active</th><th>Last schedule</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredCronJobs.length"><td colspan="7" class="k8s-msg">No cronjobs.</td></tr>
                <tr v-for="j in filteredCronJobs" :key="j.namespace + '/' + j.name">
                  <td class="k8s-mono">{{ j.name }}</td>
                  <td>{{ j.namespace }}</td>
                  <td class="k8s-mono">{{ j.schedule }}</td>
                  <td>{{ j.suspend ? 'Yes' : 'No' }}</td>
                  <td>{{ j.active }}</td>
                  <td>{{ relAge(j.last_schedule) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('cronjob', j.namespace, j.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'services'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Type</th><th>Cluster IP</th><th>External IP</th><th>Ports</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredServices.length"><td colspan="8" class="k8s-msg">No services.</td></tr>
                <tr v-for="s in filteredServices" :key="s.namespace + '/' + s.name">
                  <td class="k8s-mono">{{ s.name }}</td>
                  <td>{{ s.namespace }}</td>
                  <td>{{ s.type }}</td>
                  <td class="k8s-mono">{{ s.cluster_ip }}</td>
                  <td class="k8s-mono">{{ s.external_ip }}</td>
                  <td class="k8s-mono">{{ s.ports }}</td>
                  <td>{{ relAge(s.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('service', s.namespace, s.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'ingresses'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Class</th><th>Hosts</th><th>Address</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredIngresses.length"><td colspan="7" class="k8s-msg">No ingresses.</td></tr>
                <tr v-for="i in filteredIngresses" :key="i.namespace + '/' + i.name">
                  <td class="k8s-mono">{{ i.name }}</td>
                  <td>{{ i.namespace }}</td>
                  <td>{{ i.class || '-' }}</td>
                  <td class="k8s-mono k8s-img">{{ i.hosts.join(', ') || '-' }}</td>
                  <td class="k8s-mono">{{ i.address || '-' }}</td>
                  <td>{{ relAge(i.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('ingress', i.namespace, i.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'configmaps'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Keys</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredConfigMaps.length"><td colspan="5" class="k8s-msg">No configmaps.</td></tr>
                <tr v-for="c in filteredConfigMaps" :key="c.namespace + '/' + c.name">
                  <td class="k8s-mono">{{ c.name }}</td>
                  <td>{{ c.namespace }}</td>
                  <td class="k8s-mono k8s-img">{{ c.keys.join(', ') || '-' }}</td>
                  <td>{{ relAge(c.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('configmap', c.namespace, c.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'secrets'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Type</th><th>Keys</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredSecrets.length"><td colspan="6" class="k8s-msg">No secrets.</td></tr>
                <tr v-for="s in filteredSecrets" :key="s.namespace + '/' + s.name">
                  <td class="k8s-mono">{{ s.name }}</td>
                  <td>{{ s.namespace }}</td>
                  <td class="k8s-mono">{{ s.type }}</td>
                  <td class="k8s-mono k8s-img">{{ s.keys.join(', ') || '-' }}</td>
                  <td>{{ relAge(s.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('secret', s.namespace, s.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'pvcs'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Status</th><th>Capacity</th><th>Storage class</th><th>Volume</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredPVCs.length"><td colspan="8" class="k8s-msg">No PVCs.</td></tr>
                <tr v-for="p in filteredPVCs" :key="p.namespace + '/' + p.name">
                  <td class="k8s-mono">{{ p.name }}</td>
                  <td>{{ p.namespace }}</td>
                  <td><span class="k8s-pill" :class="p.status === 'Bound' ? 'k8s-pill--ok' : 'k8s-pill--warn'">{{ p.status }}</span></td>
                  <td>{{ p.capacity || '-' }}</td>
                  <td>{{ p.storage_class || '-' }}</td>
                  <td class="k8s-mono k8s-img">{{ p.volume || '-' }}</td>
                  <td>{{ relAge(p.created) }}</td>
                  <td class="k8s-act"><button class="k8s-ico" title="View YAML" @click="describe('pvc', p.namespace, p.name)"><svg class="k8s-ico-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></button></td>
                </tr>
              </tbody>
            </table>

            <table v-else-if="tab === 'events'" class="k8s-table">
              <thead><tr><th>Type</th><th>Reason</th><th>Object</th><th>Namespace</th><th>Message</th><th>Count</th><th>Last seen</th></tr></thead>
              <tbody>
                <tr v-if="!filteredEvents.length"><td colspan="7" class="k8s-msg">No events.</td></tr>
                <tr v-for="(e, i) in filteredEvents" :key="i">
                  <td><span class="k8s-pill" :class="e.type === 'Warning' ? 'k8s-pill--warn' : 'k8s-pill--ok'">{{ e.type }}</span></td>
                  <td>{{ e.reason }}</td>
                  <td class="k8s-mono">{{ e.object }}</td>
                  <td>{{ e.namespace }}</td>
                  <td class="k8s-msg-cell">{{ e.message }}</td>
                  <td>{{ e.count }}</td>
                  <td>{{ relAge(e.last_seen) }}</td>
                </tr>
              </tbody>
            </table>
              </div>
            </div>
          </div>
        </template>

        <!-- Idle -->
        <div v-else class="page-card k8s-idle">
          <div class="k8s-idle-icon">☸️</div>
          <p>Select a cluster from the dropdown above to connect.</p>
        </div>
      </div>
    </div>

    <!-- Describe drawer -->
    <div v-if="showDescribe" class="k8s-drawer-backdrop" @click.self="showDescribe = false">
      <aside class="k8s-drawer">
        <div class="k8s-drawer-head">
          <span class="k8s-drawer-title">{{ describeTitle }}</span>
          <div class="k8s-spacer"></div>
          <button class="base-btn base-btn--xs" @click="copyDescribe">Copy</button>
          <button class="base-btn base-btn--xs" @click="showDescribe = false">Close</button>
        </div>
        <pre class="k8s-yaml">{{ describeLoading ? 'Loading…' : describeYaml }}</pre>
      </aside>
    </div>

    <!-- Pod detail drawer -->
    <div v-if="showPodDetail" class="k8s-drawer-backdrop" @click.self="showPodDetail = false">
      <aside class="k8s-drawer">
        <div class="k8s-drawer-head">
          <span class="k8s-drawer-title">{{ podDetail ? podDetail.namespace + '/' + podDetail.name : 'Pod' }}</span>
          <div class="k8s-spacer"></div>
          <button class="base-btn base-btn--xs" @click="showPodDetail = false">Close</button>
        </div>
        <div class="k8s-detail">
          <div v-if="podDetailLoading" class="k8s-msg">Loading…</div>
          <template v-else-if="podDetail">
            <div class="k8s-detail-grid">
              <div class="k8s-kv"><span>Status</span><span><span class="k8s-pill" :class="podClass(podDetail.phase)">{{ podDetail.phase }}</span></span></div>
              <div class="k8s-kv"><span>Node</span><span class="k8s-mono">{{ podDetail.node || '—' }}</span></div>
              <div class="k8s-kv"><span>Pod IP</span><span class="k8s-mono">{{ podDetail.pod_ip || '—' }}</span></div>
              <div class="k8s-kv"><span>QoS</span><span>{{ podDetail.qos || '—' }}</span></div>
              <div class="k8s-kv"><span>Started</span><span>{{ podDetail.start_time ? relAge(podDetail.start_time) + ' ago' : '—' }}</span></div>
            </div>

            <h4 class="k8s-detail-h">Containers ({{ podDetail.containers.length }})</h4>
            <div v-for="c in podDetail.containers" :key="c.name" class="k8s-ctr">
              <div class="k8s-ctr-head">
                <span class="k8s-dot" :class="c.ready ? 'k8s-dot--ok' : 'k8s-dot--err'"></span>
                <span class="k8s-ctr-name">{{ c.name }}</span>
                <span class="k8s-pill" :class="c.state === 'Running' ? 'k8s-pill--ok' : c.state === 'Completed' ? 'k8s-pill--ok' : 'k8s-pill--warn'">{{ c.state || '—' }}</span>
                <span v-if="c.restarts > 0" class="k8s-dim">{{ c.restarts }} restarts</span>
              </div>
              <div class="k8s-ctr-img k8s-mono">{{ c.image }}</div>
              <div class="k8s-ctr-res">
                <span>CPU <b>{{ c.cpu_request || '—' }}</b> req / <b>{{ c.cpu_limit || '—' }}</b> limit</span>
                <span>Mem <b>{{ c.mem_request || '—' }}</b> req / <b>{{ c.mem_limit || '—' }}</b> limit</span>
              </div>
            </div>

            <h4 class="k8s-detail-h">Recent events ({{ podDetail.events.length }})</h4>
            <div v-if="!podDetail.events.length" class="k8s-dim">No recent events.</div>
            <div v-for="(e, i) in podDetail.events" :key="i" class="k8s-ev">
              <span class="k8s-pill" :class="e.type === 'Warning' ? 'k8s-pill--warn' : 'k8s-pill--ok'">{{ e.reason }}</span>
              <span class="k8s-ev-msg">{{ e.message }}</span>
              <span class="k8s-dim k8s-ev-age">{{ relAge(e.last_seen) }}<template v-if="e.count > 1"> ×{{ e.count }}</template></span>
            </div>
          </template>
        </div>
      </aside>
    </div>

    <!-- Pod logs modal -->
    <div v-if="showLogs" class="k8s-modal-backdrop" @click.self="closeLogs">
      <div class="k8s-modal k8s-modal--wide page-card">
        <div class="k8s-modal-title">
          Logs — {{ logsPod?.namespace }}/{{ logsPod?.name }}
          <span v-if="logsFollow" class="k8s-live">● live</span>
        </div>
        <div class="k8s-logs-bar">
          <select v-if="logsPod && logsPod.containers.length > 1" v-model="logsContainer" class="base-input k8s-logs-sel" @change="logsFollow ? startFollow() : fetchLogs()">
            <option v-for="c in logsPod.containers" :key="c" :value="c">{{ c }}</option>
          </select>
          <select v-model="logsTail" class="base-input k8s-logs-sel" @change="logsFollow ? startFollow() : fetchLogs()">
            <option value="100">100 lines</option>
            <option value="300">300 lines</option>
            <option value="1000">1000 lines</option>
            <option value="5000">5000 lines</option>
          </select>
          <button class="base-btn base-btn--sm" :class="{ 'base-btn--primary': logsFollow }" @click="toggleFollow">
            {{ logsFollow ? 'Stop' : 'Follow' }}
          </button>
          <button class="base-btn base-btn--sm" :disabled="logsLoading || logsFollow" @click="fetchLogs">Refresh</button>
          <div class="k8s-spacer"></div>
          <button class="base-btn base-btn--sm" @click="closeLogs">Close</button>
        </div>
        <pre ref="logViewEl" class="k8s-logs">{{ logsLoading ? 'Loading…' : logsText }}</pre>
      </div>
    </div>

    <!-- Exec terminal modal -->
    <div v-if="showExec" class="k8s-modal-backdrop" @click.self="closeExec">
      <div class="k8s-term-modal">
        <div class="k8s-term-head">
          <span class="k8s-term-title">Exec — {{ execPod?.namespace }}/{{ execPod?.name }}</span>
          <div class="k8s-spacer"></div>
          <select v-if="execPod && execPod.containers.length > 1" v-model="execContainer" class="base-input k8s-term-sel" @change="restartExec">
            <option v-for="c in execPod.containers" :key="c" :value="c">{{ c }}</option>
          </select>
          <select v-model="execShell" class="base-input k8s-term-sel" @change="restartExec">
            <option value="/bin/sh">/bin/sh</option>
            <option value="/bin/bash">/bin/bash</option>
            <option value="/bin/ash">/bin/ash</option>
          </select>
          <button class="base-btn base-btn--xs" @click="restartExec">Reconnect</button>
          <button class="base-btn base-btn--xs" @click="closeExec">Close</button>
        </div>
        <div ref="termEl" class="k8s-term"></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.k8s-cluster-select { min-width: 230px; }

.k8s-conn { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 600; }
.k8s-conn-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.k8s-conn--connected { color: var(--success); background: var(--success-bg, rgba(34,197,94,0.12)); }
.k8s-conn--error { color: var(--danger); background: var(--danger-bg, rgba(239,68,68,0.12)); }
.k8s-conn--connecting, .k8s-conn--unknown { color: var(--text-muted); background: var(--bg-hover); }
.k8s-conn--connecting .k8s-conn-dot { animation: k8s-blink 1s ease-in-out infinite; }
@keyframes k8s-blink { 0%,100% { opacity: 1; } 50% { opacity: 0.3; } }

.k8s-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.k8s-dot--ok { background: var(--success); }
.k8s-dot--err { background: var(--danger); }
.k8s-spacer { flex: 1; }
.k8s-ns-select { min-width: 180px; }
.k8s-search { min-width: 150px; max-width: 200px; }
.k8s-muted { color: var(--text-muted); }
.k8s-err { color: var(--danger); }

/* Sidebar shell (Lens-style) */
.k8s-shell { display: flex; gap: 16px; align-items: flex-start; }
.k8s-side {
  position: sticky; top: 8px; align-self: flex-start;
  width: 180px; flex-shrink: 0;
  background: var(--bg-surface); border: 1px solid var(--border);
  border-radius: var(--r-lg); padding: 10px 8px;
  display: flex; flex-direction: column; gap: 12px;
}
.k8s-side-ver { display: flex; align-items: center; gap: 6px; padding: 4px 8px 8px; font-size: 11px; font-family: var(--mono); color: var(--text-muted); border-bottom: 1px solid var(--border); }
.k8s-side-vertxt { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.k8s-side-group { display: flex; flex-direction: column; gap: 1px; }
.k8s-side-grouptitle { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-muted); padding: 2px 8px 4px; }
.k8s-side-item {
  text-align: left; background: none; border: none; cursor: pointer;
  padding: 6px 10px; border-radius: var(--r-sm); font-size: 12.5px;
  color: var(--text-secondary); border-left: 2px solid transparent;
  transition: background var(--dur) var(--ease), color var(--dur) var(--ease);
}
.k8s-side-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.k8s-side-item--active { background: var(--brand-dim); color: var(--brand); border-left-color: var(--brand); font-weight: 600; }

.k8s-content { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
.k8s-content-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.k8s-content-title { display: flex; align-items: center; gap: 8px; margin: 0; font-size: 17px; font-weight: 700; color: var(--text-primary); }
.k8s-count { font-size: 12px; font-weight: 600; color: var(--text-muted); background: var(--bg-hover); border-radius: 99px; padding: 1px 9px; }

/* Compact icon row-actions (revealed on row hover) */
.k8s-ico {
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 26px; padding: 0; margin-left: 2px;
  background: none; border: 1px solid transparent; border-radius: var(--r-sm);
  color: var(--text-muted); cursor: pointer; transition: all var(--dur) var(--ease);
}
.k8s-ico:hover { background: var(--bg-hover); border-color: var(--border); color: var(--brand); }
.k8s-ico-svg { width: 15px; height: 15px; }

.k8s-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; }
.k8s-kpi { border: 1px solid var(--border); border-radius: var(--r); padding: 16px 18px; background: var(--bg-surface); }
.k8s-kpi-val { font-size: 28px; font-weight: 700; color: var(--text-primary); line-height: 1; }
.k8s-kpi-sub { font-size: 16px; font-weight: 400; color: var(--text-muted); }
.k8s-kpi-label { font-size: 12px; color: var(--text-muted); margin-top: 6px; text-transform: uppercase; letter-spacing: 0.04em; }

.k8s-table-wrap { padding: 4px 6px; overflow-x: auto; }
.k8s-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.k8s-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; white-space: nowrap; }
.k8s-th-sort { cursor: pointer; user-select: none; }
.k8s-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.k8s-table tbody tr:last-child td { border-bottom: none; }
.k8s-table tbody tr:hover { background: var(--bg-hover); }
.k8s-mono { font-family: var(--mono); font-size: 11.5px; }
.k8s-img { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.k8s-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.k8s-msg-cell { max-width: 360px; color: var(--text-secondary); }
.k8s-act { text-align: right; white-space: nowrap; opacity: 0.4; transition: opacity var(--dur) var(--ease); }
.k8s-table tbody tr:hover .k8s-act { opacity: 1; }
.k8s-warn { color: var(--warning); font-weight: 600; }
.k8s-conn-err { padding: 16px; color: var(--danger); font-size: 13px; }

.k8s-pill { font-size: 11px; padding: 1px 8px; border-radius: 99px; font-weight: 600; }
.k8s-pill--ok { background: var(--success-bg, rgba(34,197,94,0.12)); color: var(--success); }
.k8s-pill--warn { background: var(--warning-bg, rgba(245,158,11,0.14)); color: var(--warning); }
.k8s-pill--err { background: var(--danger-bg, rgba(239,68,68,0.12)); color: var(--danger); }

.k8s-empty { padding: 60px 40px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 12px; }
.k8s-empty-icon { font-size: 48px; }
.k8s-empty h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.k8s-empty p { font-size: 13px; color: var(--text-muted); margin: 0; max-width: 380px; }
.k8s-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.k8s-idle-icon { font-size: 22px; }

/* Describe drawer */
.k8s-drawer-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; justify-content: flex-end; z-index: 110; }
.k8s-drawer { width: clamp(560px, 55vw, 1200px); max-width: 96vw; height: 100%; background: var(--bg-surface); border-left: 1px solid var(--border); display: flex; flex-direction: column; box-shadow: var(--shadow-lg); }
.k8s-drawer-head { display: flex; align-items: center; gap: 8px; padding: 14px 18px; border-bottom: 1px solid var(--border); }
.k8s-drawer-title { font-size: 14px; font-weight: 600; color: var(--text-primary); font-family: var(--mono); word-break: break-all; }
.k8s-yaml { flex: 1; margin: 0; padding: 14px 18px; overflow: auto; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre; color: var(--text-secondary); background: var(--bg-body); }

/* Auto-refresh + metrics */
.k8s-auto { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-secondary); cursor: pointer; white-space: nowrap; }
.k8s-dim { color: var(--text-muted); }
.k8s-pct { margin-left: 6px; font-size: 11px; font-weight: 600; color: var(--text-muted); }
.k8s-pct--mid { color: var(--warning); }
.k8s-pct--hi { color: var(--danger); }
.k8s-link { background: none; border: none; padding: 0; font: inherit; color: var(--brand); cursor: pointer; text-align: left; }
.k8s-link:hover { text-decoration: underline; }

/* Pod detail drawer */
.k8s-detail { flex: 1; overflow: auto; padding: 16px 18px; }
.k8s-detail-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 8px 16px; margin-bottom: 18px; }
.k8s-kv { display: flex; flex-direction: column; gap: 2px; font-size: 12.5px; }
.k8s-kv > span:first-child { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.k8s-kv > span:last-child { color: var(--text-secondary); }
.k8s-detail-h { font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); margin: 18px 0 8px; font-weight: 700; }
.k8s-ctr { border: 1px solid var(--border); border-radius: var(--r); padding: 10px 12px; margin-bottom: 8px; }
.k8s-ctr-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.k8s-ctr-name { font-weight: 600; color: var(--text-primary); font-size: 13px; }
.k8s-ctr-img { font-size: 11px; color: var(--text-muted); margin: 4px 0; word-break: break-all; }
.k8s-ctr-res { display: flex; gap: 16px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-secondary); }
.k8s-ctr-res b { color: var(--text-primary); font-weight: 600; }
.k8s-ev { display: flex; align-items: baseline; gap: 8px; padding: 5px 0; border-bottom: 1px solid var(--border); font-size: 12px; }
.k8s-ev:last-child { border-bottom: none; }
.k8s-ev-msg { flex: 1; color: var(--text-secondary); }
.k8s-ev-age { white-space: nowrap; font-size: 11px; }

/* Modals */
.k8s-modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.k8s-modal { padding: 20px; width: 440px; max-width: 94vw; }
.k8s-modal--wide { width: 860px; }
.k8s-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; word-break: break-all; }
.k8s-live { font-size: 11px; color: var(--success); margin-left: 8px; animation: k8s-blink 1.4s ease-in-out infinite; }
.k8s-logs-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.k8s-logs-sel { width: auto; min-width: 110px; }
.k8s-logs { background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 12px; font-family: var(--mono); font-size: 11px; line-height: 1.5; height: 62vh; overflow: auto; white-space: pre-wrap; word-break: break-all; color: var(--text-secondary); margin: 0; }

/* Exec terminal */
.k8s-term-modal { width: 920px; max-width: 96vw; height: 580px; max-height: 88vh; background: #0d1117; border: 1px solid var(--border); border-radius: var(--r-lg); box-shadow: var(--shadow-lg); display: flex; flex-direction: column; overflow: hidden; }
.k8s-term-head { display: flex; align-items: center; gap: 8px; padding: 10px 14px; background: var(--bg-elevated); border-bottom: 1px solid var(--border); }
.k8s-term-title { font-size: 13px; font-weight: 600; color: var(--text-primary); font-family: var(--mono); }
.k8s-term-sel { width: auto; min-width: 100px; font-size: 12px; }
.k8s-term { flex: 1; min-height: 0; padding: 8px 10px; overflow: hidden; }
.k8s-term :deep(.xterm) { height: 100%; }
</style>
