<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useSort } from '@/composables/useSort'
import { useListFilter } from '@/composables/useListFilter'
import NginxEditor from '@/components/nginx/NginxEditor.vue'
import SearchSelect from '@/components/ui/SearchSelect.vue'
import SortIcon from '@/components/ui/SortIcon.vue'
import ActionIcon from '@/components/ui/ActionIcon.vue'

type NginxTab = 'config' | 'sites' | 'logs' | 'map' | 'certs' | 'status'
interface NginxCert {
  path: string
  subject: string
  not_after: string
  days_left: number
  expired: boolean
  domains: string[]
  error?: string
}
interface NginxServerInfo {
  listen: string[]
  names: string
  root: string
  cert: string
  proxy_pass: string[]
}
interface NginxUpstreamInfo {
  name: string
  servers: string[]
}
interface NginxStub {
  active: number
  accepts: number
  handled: number
  requests: number
  reading: number
  writing: number
  waiting: number
}
interface FleetHost {
  id: number
  name: string
  ssh_host: string
  reachable: boolean
  version: string
  active: string
  test_ok: boolean
  test_output: string
  certs: number
  soonest_days: number
  expiring: number
  error?: string
}

interface SshHost {
  id: number
  name: string
  ssh_host: string
}
interface NginxFile {
  path: string
  size: number
}
interface NginxSite {
  name: string
  enabled: boolean
  state: string // 'enabled' | 'disabled' | 'inactive'
  toggleable: boolean
}
interface NginxInfo {
  bin: string
  version: string
  config_root: string
  log_root: string
  active: string
  sites_layout: string
}

const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['nginx.manage']))

function copyPath(path: string) {
  navigator.clipboard?.writeText(path)
  toast.success('Path copied')
}
const canReload = computed(() => hasAnyPermission(['nginx.reload']))

const hosts = ref<SshHost[]>([])
const hostId = ref<number | null>(null)
const hostStatus = ref<'unknown' | 'connecting' | 'connected' | 'error'>('unknown')
const tab = ref<NginxTab>('config')

// Per-host detected/overridable settings
const bin = ref('nginx')
const configRoot = ref('/etc/nginx')
const logDir = ref('/var/log/nginx')
// When nginx runs inside a Docker container, log_dir isn't a path on the SSH
// host's own filesystem at all — logs are read via `docker exec <container>`
// instead of directly over SSH/SFTP.
const logContainer = ref('')
const useSudo = ref(false)
const sitesLayout = ref('') // 'symlink' | 'confd' | ''
const version = ref('')
const active = ref('')
const detecting = ref(false)

const cmdOutput = ref('')
const cmdOk = ref(true)
const busy = ref(false)

// Config
const files = ref<NginxFile[]>([])
const loadingTree = ref(false)
const editorFullscreen = ref(false)

// Open editor tabs live in a single flat list shared across all hosts, so you
// can have files open from several different hosts at once. Each tab carries
// its own hostId; activating a tab from a different host than the one
// currently selected flips the whole page (toolbar, Sites/Certs/Logs/Map) to
// that host, so the editor and the rest of the page never disagree about
// which server is "current". Keyed by `${hostId}:${path}` since the same
// relative path can exist open on two different hosts at once.
interface EditorTab {
  id: string
  hostId: number
  path: string
  content: string
  origContent: string
  loading: boolean
  backups: NginxBackup[]
  // Snapshot of this host's config root/binary/sudo at the moment the tab was
  // opened. Save/backup/revert use these instead of the live toolbar values
  // so a tab keeps working correctly even while a *different* host is the
  // one currently selected — e.g. the right pane in split view.
  root: string
  bin: string
  sudo: boolean
}
function tabKey(hid: number, p: string) {
  return `${hid}:${p}`
}
function tabBase(t: EditorTab) {
  return `/api/nginx/hosts/${t.hostId}`
}
function tabParams(t: EditorTab) {
  return { root: t.root, bin: t.bin, sudo: t.sudo ? '1' : undefined }
}
const tabs = ref<EditorTab[]>([])
const activeTabId = ref('')
// The tab strip only ever shows tabs for the currently selected host — mixing
// every host's tabs into one strip made it look "broken" whenever you
// switched hosts and none of the visible tabs matched (empty editor, nothing
// showing as active, no visible reason why). Switch hosts, the strip switches
// with it; nothing lingers from a host you've since navigated away from.
const currentHostTabs = computed(() => tabs.value.filter((t) => t.hostId === hostId.value))
// Second, independent pane for side-by-side viewing — populated only via the
// explicit "Compare with another host" picker, never by clicking a tab, so
// there's no ambiguity about which host a given click will affect.
const rightTabId = ref('')
const splitView = computed(() => rightTabId.value !== '')
// Remembers the last active tab per host so switching the host dropdown
// restores what you were last looking at on that host.
const lastActiveTabByHost: Record<number, string> = {}
const activeTab = computed<EditorTab | undefined>(() => tabs.value.find((t) => t.id === activeTabId.value))
const rightTab = computed<EditorTab | undefined>(() => tabs.value.find((t) => t.id === rightTabId.value))
const activeFile = computed(() => activeTab.value?.path ?? '')
function tabHostName(t: EditorTab): string {
  return hosts.value.find((h) => h.id === t.hostId)?.name || `host ${t.hostId}`
}
const fileContent = computed<string>({
  get: () => activeTab.value?.content ?? '',
  set: (v) => { if (activeTab.value) activeTab.value.content = v },
})
const origContent = computed<string>({
  get: () => activeTab.value?.origContent ?? '',
  set: (v) => { if (activeTab.value) activeTab.value.origContent = v },
})
const loadingFile = computed<boolean>({
  get: () => activeTab.value?.loading ?? false,
  set: (v) => { if (activeTab.value) activeTab.value.loading = v },
})
const backups = computed<NginxBackup[]>({
  get: () => activeTab.value?.backups ?? [],
  set: (v) => { if (activeTab.value) activeTab.value.backups = v },
})
const rightContent = computed<string>({
  get: () => rightTab.value?.content ?? '',
  set: (v) => { if (rightTab.value) rightTab.value.content = v },
})

// "Compare with another host" picker — the only way into split view. Loads
// its own copy of that host's settings/file tree independent of the toolbar
// above, since the toolbar always reflects the host you're actually on.
const showComparePicker = ref(false)
const compareHostId = ref<number | null>(null)
const compareRoot = ref('/etc/nginx')
const compareBin = ref('nginx')
const compareSudo = ref(false)
const compareFiles = ref<NginxFile[]>([])
const compareLoadingFiles = ref(false)
const compareHostOptions = computed(() => hosts.value.map((h) => ({ value: h.id, label: `${h.name} (${h.ssh_host})` })))

function openComparePicker() {
  showComparePicker.value = true
  compareHostId.value = null
  compareFiles.value = []
}
function closeComparePicker() {
  showComparePicker.value = false
}
async function selectCompareHost(id: number) {
  compareHostId.value = id
  compareRoot.value = '/etc/nginx'
  compareBin.value = 'nginx'
  compareSudo.value = false
  try {
    const { data } = await axios.get(`/api/nginx/hosts/${id}/settings`)
    if (data.exists) {
      if (data.config_root) compareRoot.value = data.config_root
      if (data.bin) compareBin.value = data.bin
      compareSudo.value = !!data.use_sudo
    } else {
      const { data: info } = await axios.get<NginxInfo>(`/api/nginx/hosts/${id}/info`)
      compareRoot.value = info.config_root || compareRoot.value
      compareBin.value = info.bin || compareBin.value
    }
  } catch {
    /* fall back to defaults above — the tree request below will surface any real error */
  }
  await loadCompareTree()
}
async function loadCompareTree() {
  if (compareHostId.value === null) return
  compareLoadingFiles.value = true
  try {
    const { data } = await axios.get<{ files: NginxFile[] }>(`/api/nginx/hosts/${compareHostId.value}/config/tree`, {
      params: { root: compareRoot.value, bin: compareBin.value, sudo: compareSudo.value ? '1' : undefined },
    })
    compareFiles.value = data.files || []
  } catch (e: any) {
    if (!compareSudo.value && isPermDenied(e)) {
      compareSudo.value = true
      return loadCompareTree()
    }
    toast.error(e?.response?.data?.error || 'Failed to list config')
    compareFiles.value = []
  } finally {
    compareLoadingFiles.value = false
  }
}
async function openCompareFile(p: string) {
  if (compareHostId.value === null) return
  const hid = compareHostId.value
  const root = compareRoot.value
  const binv = compareBin.value
  const sudov = compareSudo.value
  const key = tabKey(hid, p)
  const existing = tabs.value.find((t) => t.id === key)
  if (existing) {
    rightTabId.value = existing.id
    showComparePicker.value = false
    return
  }
  const newTab: EditorTab = { id: key, hostId: hid, path: p, content: '', origContent: '', loading: true, backups: [], root, bin: binv, sudo: sudov }
  tabs.value.push(newTab)
  rightTabId.value = key
  showComparePicker.value = false
  try {
    const { data } = await axios.get(`${tabBase(newTab)}/config/file`, { params: { ...tabParams(newTab), path: p } })
    newTab.content = data.content || ''
    newTab.origContent = newTab.content
    loadBackups(newTab)
  } catch (e: any) {
    const idx = tabs.value.indexOf(newTab)
    if (!sudov && isPermDenied(e)) {
      if (idx !== -1) tabs.value.splice(idx, 1)
      compareSudo.value = true
      return openCompareFile(p)
    }
    toast.error(e?.response?.data?.error || 'Failed to read file')
    if (idx !== -1) tabs.value.splice(idx, 1)
    if (rightTabId.value === key) rightTabId.value = ''
  } finally {
    newTab.loading = false
  }
}
const dirty = computed(() => fileContent.value !== origContent.value)
function tabDirty(t: EditorTab) {
  return t.content !== t.origContent
}

// Activates a tab in the primary (left) pane. The strip only ever shows tabs
// for the current host, so this never needs to switch hosts itself.
function activateTab(t: EditorTab) {
  activeTabId.value = t.id
  lastActiveTabByHost[t.hostId] = t.id
}

function closeSplit() {
  rightTabId.value = ''
}

function toggleEditorFullscreen() {
  editorFullscreen.value = !editorFullscreen.value
}
function onEditorKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && editorFullscreen.value) editorFullscreen.value = false
}

// Sites
const sites = ref<NginxSite[]>([])
const loadingSites = ref(false)
const { search: siteSearch, filtered: siteSearchFiltered } = useListFilter(sites, (s, q) => s.name.toLowerCase().includes(q) || s.state.toLowerCase().includes(q))
function siteSortValue(s: NginxSite, key: string): unknown {
  return key === 'name' ? s.name.toLowerCase() : s.state
}
const { sortKey: siteSortKey, sortDir: siteSortDir, toggleSort: toggleSiteSort, sort: sortSites } = useSort<NginxSite>(siteSortValue)
const sortedSites = computed(() => sortSites(siteSearchFiltered.value))

// Logs
const logFiles = ref<NginxFile[]>([])
const activeLog = ref('')
const logLines = ref<string[]>([])
const following = ref(false)
const logViewEl = ref<HTMLElement | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

// Config backups (revert) — NginxBackup is used by the EditorTab type above;
// the `backups` computed proxying to the active tab is defined there too.
interface NginxBackup {
  path: string
  name: string
  time: number
}

// Log search
const logQuery = ref('')
const searching = ref(false)
const searchActive = ref(false)
const LEVELS = ['error', 'warn', 'crit', 'alert', 'notice']

// Settings persistence — suppress the auto-save watch while applying loaded values
let suppressSave = false
let saveTimer: ReturnType<typeof setTimeout> | null = null

// Effective config (nginx -T)
const showEffective = ref(false)
const effectiveContent = ref('')
const loadingEffective = ref(false)

// Map (servers + upstreams)
const servers = ref<NginxServerInfo[]>([])
const upstreams = ref<NginxUpstreamInfo[]>([])
const loadingMap = ref(false)
const { search: serverSearch, filtered: filteredServers } = useListFilter(servers, (s, q) =>
  (s.names || '').toLowerCase().includes(q) || (s.listen || []).join(' ').toLowerCase().includes(q) ||
  (s.root || '').toLowerCase().includes(q) || (s.proxy_pass || []).join(' ').toLowerCase().includes(q),
)
const { search: upstreamSearch, filtered: filteredUpstreams } = useListFilter(upstreams, (u, q) =>
  u.name.toLowerCase().includes(q) || (u.servers || []).join(' ').toLowerCase().includes(q),
)

// Certs
const certs = ref<NginxCert[]>([])
const loadingCerts = ref(false)
const { search: certSearch, filtered: certSearchFiltered } = useListFilter(certs, (c, q) =>
  (c.domains || []).some((d) => d.toLowerCase().includes(q)) || (c.subject || '').toLowerCase().includes(q) || c.path.toLowerCase().includes(q),
)
function certSortValue(c: NginxCert, key: string): unknown {
  if (key === 'domains') return (c.domains?.[0] || c.subject || '').toLowerCase()
  if (key === 'not_after') return c.not_after || ''
  if (key === 'days_left') return c.days_left
  return ''
}
const { sortKey: certSortKey, sortDir: certSortDir, toggleSort: toggleCertSort, sort: sortCerts } = useSort<NginxCert>(certSortValue)
// Default (no sort clicked yet) keeps the backend's own "soonest expiry
// first" order — sort() only reorders once a header is actually clicked.
const sortedCerts = computed(() => sortCerts(certSearchFiltered.value))

// stub_status
const stub = ref<NginxStub | null>(null)
const stubUrl = ref('http://127.0.0.1/nginx_status')
const stubReqRate = ref(0)
const stubErr = ref('')
let stubTimer: ReturnType<typeof setInterval> | null = null
let lastStub: { requests: number; t: number } | null = null

// Fleet (all hosts at once)
const fleetMode = ref(false)
const fleet = ref<FleetHost[]>([])
const loadingFleet = ref(false)
function fleetSortValue(f: FleetHost, key: string): unknown {
  return f[key as keyof FleetHost]
}
const { sortKey: fleetSortKey, sortDir: fleetSortDir, toggleSort: toggleFleetSort, sort: sortFleet } = useSort<FleetHost>(fleetSortValue)
const { search: fleetSearch, filtered: filteredFleet } = useListFilter(fleet, (f, q) =>
  f.name.toLowerCase().includes(q) || f.ssh_host.toLowerCase().includes(q) || (f.version || '').toLowerCase().includes(q) || (f.active || '').toLowerCase().includes(q),
)
const sortedFleet = computed(() => sortFleet(filteredFleet.value))

// A permission/sudo error means the SSH user lacks direct access — the fix is
// to elevate. We flip `useSudo` on and retry once, transparently.
function isPermDenied(e: any): boolean {
  const msg = (e?.response?.data?.error || e?.message || '').toString().toLowerCase()
  return msg.includes('permission denied') || msg.includes('not permitted') || msg.includes('sudo')
}

const base = computed(() => `/api/nginx/hosts/${hostId.value}`)
// Rotated/compressed logs are static archives — decompressed for snapshot view,
// but there's nothing to "follow".
const isCompressed = computed(() => /\.(gz|tgz|bz2|xz|zst)$/i.test(activeLog.value))
const sudoParam = computed(() => (useSudo.value ? '1' : undefined))
// Shared query params so every call respects the (possibly overridden) host settings.
const cfgParams = computed(() => ({ root: configRoot.value, bin: bin.value, sudo: sudoParam.value }))
const logParams = computed(() => ({ dir: logDir.value, bin: bin.value, sudo: sudoParam.value, container: logContainer.value.trim() || undefined }))

// Searchable-dropdown option lists
const hostOptions = computed(() => hosts.value.map((h) => ({ value: h.id, label: `${h.name} (${h.ssh_host})` })))
const binOptions = [
  { value: 'nginx', label: 'nginx' },
  { value: 'openresty', label: 'openresty' },
]
const logOptions = computed(() => logFiles.value.map((f) => ({ value: f.path, label: `${f.path} (${formatBytes(f.size)})` })))

// ── Hosts ───────────────────────────────────────────────────────
// Remember the user's connection intent across refreshes: the last host they
// connected to, or an explicit 'disconnected' so we don't silently reconnect.
const LAST_HOST_KEY = 'nias:nginx:lastHost'

async function loadHosts() {
  try {
    const { data } = await axios.get<SshHost[]>('/api/docker/hosts')
    hosts.value = data.filter((h) => h.ssh_host)
    if (hostId.value === null && hosts.value.length) {
      // An explicit ?host= (e.g. from the SSH Hosts page) means "connect now"
      // and overrides any saved disconnect state.
      const queryId = route.query.host ? Number(route.query.host) : null
      const queryHost = queryId ? hosts.value.find((h) => h.id === queryId) : null
      if (queryHost) {
        await selectHost(queryHost.id)
        return
      }
      const saved = localStorage.getItem(LAST_HOST_KEY)
      // The user explicitly disconnected — stay on the idle screen.
      if (saved === 'disconnected') return
      // Restore the previously connected host if it still exists; otherwise
      // fall back to the first host (default for a first-ever visit).
      const savedId = saved ? Number(saved) : null
      const target = (savedId && hosts.value.find((h) => h.id === savedId)) || hosts.value[0]
      hostId.value = target.id
      await onHostChange()
      await pingHost()
    }
  } catch {
    toast.error('Failed to load hosts')
  }
}

async function selectHost(id: number) {
  hostId.value = id
  hostStatus.value = 'unknown'
  localStorage.setItem(LAST_HOST_KEY, String(id))
  // Restore whichever tab was last active for this host (if still open);
  // otherwise fall back to any other open tab for it, or none at all. Tabs
  // for other hosts stay exactly as they were — nothing here touches `tabs`.
  const remembered = lastActiveTabByHost[id]
  if (remembered && tabs.value.some((t) => t.id === remembered)) {
    activeTabId.value = remembered
  } else {
    const anyForHost = tabs.value.find((t) => t.hostId === id)
    activeTabId.value = anyForHost ? anyForHost.id : ''
  }
  await onHostChange()
  await pingHost()
}

async function pingHost() {
  if (hostId.value === null) return
  hostStatus.value = 'connecting'
  try {
    await axios.get(`/api/docker/hosts/${hostId.value}/ping`)
    hostStatus.value = 'connected'
  } catch {
    hostStatus.value = 'error'
  }
}

async function disconnectHost() {
  stopFollow()
  stopStub()
  hostId.value = null
  hostStatus.value = 'unknown'
  localStorage.setItem(LAST_HOST_KEY, 'disconnected')
  // Clear all content so stale data isn't shown after disconnect. Open editor
  // tabs are intentionally left in `tabs` so they're still there if this (or
  // any other) host is reconnected to or switched back to later.
  version.value = ''
  active.value = ''
  cmdOutput.value = ''
  files.value = []
  sites.value = []
  logFiles.value = []
  activeLog.value = ''
  logLines.value = []
  servers.value = []
  upstreams.value = []
  certs.value = []
  stub.value = null
  effectiveContent.value = ''
}

// ── Fleet (all hosts) ───────────────────────────────────────────
async function toggleFleet() {
  fleetMode.value = !fleetMode.value
  if (fleetMode.value) {
    stopFollow()
    stopStub()
    await loadFleet()
  }
}

async function loadFleet() {
  loadingFleet.value = true
  try {
    const { data } = await axios.get<{ hosts: FleetHost[] }>('/api/nginx/fleet')
    fleet.value = data.hosts || []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load fleet')
    fleet.value = []
  } finally {
    loadingFleet.value = false
  }
}

async function openFromFleet(id: number) {
  fleetMode.value = false
  hostId.value = id
  await onHostChange()
}

async function onHostChange() {
  stopFollow()
  stopStub()
  cmdOutput.value = ''
  // Editor tabs live in the flat `tabs` list (not keyed by host), and which
  // one is active was already resolved by the caller (selectHost/activateTab)
  // before this runs — nothing to clear here.
  activeLog.value = ''
  logLines.value = []
  logQuery.value = ''
  searchActive.value = false
  suppressSave = true
  const hadSaved = await loadSettings()
  // Detection still runs (for version/active + layout), but it only overrides
  // the paths/binary when the user hasn't saved their own for this host.
  await detect(!hadSaved)
  suppressSave = false
  await loadCurrentTab()
}

// Load remembered per-host settings. Returns true when settings existed.
async function loadSettings(): Promise<boolean> {
  if (hostId.value === null) return false
  try {
    const { data } = await axios.get(`${base.value}/settings`)
    if (!data.exists) return false
    useSudo.value = !!data.use_sudo
    if (data.bin) bin.value = data.bin
    if (data.config_root) configRoot.value = data.config_root
    if (data.log_dir) logDir.value = data.log_dir
    logContainer.value = data.log_container || ''
    return true
  } catch {
    return false
  }
}

async function saveSettings() {
  if (hostId.value === null) return
  try {
    await axios.put(`${base.value}/settings`, {
      use_sudo: useSudo.value,
      config_root: configRoot.value,
      log_dir: logDir.value,
      bin: bin.value,
      log_container: logContainer.value.trim(),
    })
  } catch {
    /* non-fatal — settings are a convenience */
  }
}

// Probe the host for binary, paths, and sites layout. When applyPaths is false
// the saved/overridden paths are kept; only version/active/layout refresh.
async function detect(applyPaths = true) {
  if (hostId.value === null) return
  detecting.value = true
  try {
    const { data } = await axios.get<NginxInfo>(`${base.value}/info`)
    version.value = data.version || ''
    active.value = data.active || ''
    sitesLayout.value = data.sites_layout || ''
    if (applyPaths) {
      bin.value = data.bin || 'nginx'
      configRoot.value = data.config_root || '/etc/nginx'
      logDir.value = data.log_root || '/var/log/nginx'
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Detection failed — using defaults')
  } finally {
    detecting.value = false
  }
}

// Re-scan after the user edits paths manually (always re-applies detected paths).
async function rescan() {
  await detect(true)
  await loadCurrentTab()
  toast.success('Rescanned host')
}

// Persist settings and reload the active tab (debounced) whenever the user
// changes binary, paths, or sudo — so editing Config Root/Log Directory
// refreshes the file list without needing a manual "Rescan".
watch([bin, configRoot, logDir, useSudo, logContainer], () => {
  if (suppressSave || hostId.value === null) return
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    saveSettings()
    loadCurrentTab()
  }, 600)
})

async function loadCurrentTab() {
  if (tab.value === 'config') await loadTree()
  else if (tab.value === 'sites') await loadSites()
  else if (tab.value === 'logs') await loadLogList()
  else if (tab.value === 'map') await loadMap()
  else if (tab.value === 'certs') await loadCerts()
  else if (tab.value === 'status') startStub()
}

function switchTab(t: NginxTab) {
  if (t === tab.value) return
  if (t !== 'logs') stopFollow()
  if (t !== 'status') stopStub()
  tab.value = t
  loadCurrentTab()
}

// ── Effective config (nginx -T) ─────────────────────────────────
async function openEffective() {
  showEffective.value = true
  loadingEffective.value = true
  effectiveContent.value = ''
  try {
    const { data } = await axios.get(`${base.value}/config/effective`, { params: cfgParams.value })
    effectiveContent.value = data.content || ''
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      loadingEffective.value = false
      return openEffective()
    }
    effectiveContent.value = `# Failed: ${e?.response?.data?.error || 'nginx -T error'}`
  } finally {
    loadingEffective.value = false
  }
}

// ── Map (servers + upstreams) ───────────────────────────────────
async function loadMap() {
  loadingMap.value = true
  try {
    const { data } = await axios.get<{ servers: NginxServerInfo[]; upstreams: NginxUpstreamInfo[] }>(`${base.value}/map`, {
      params: cfgParams.value,
    })
    servers.value = data.servers || []
    upstreams.value = data.upstreams || []
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      loadingMap.value = false
      return loadMap()
    }
    toast.error(e?.response?.data?.error || 'Failed to parse config')
    servers.value = []
    upstreams.value = []
  } finally {
    loadingMap.value = false
  }
}

// ── Certs ───────────────────────────────────────────────────────
async function loadCerts() {
  loadingCerts.value = true
  try {
    const { data } = await axios.get<{ certs: NginxCert[] }>(`${base.value}/certs`, { params: cfgParams.value })
    certs.value = data.certs || []
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      loadingCerts.value = false
      return loadCerts()
    }
    toast.error(e?.response?.data?.error || 'Failed to read certs')
    certs.value = []
  } finally {
    loadingCerts.value = false
  }
}

// ── stub_status ─────────────────────────────────────────────────
async function pollStub() {
  try {
    const { data } = await axios.get<NginxStub>(`${base.value}/stub_status`, {
      params: { url: stubUrl.value, sudo: sudoParam.value },
    })
    stubErr.value = ''
    const now = Date.now()
    if (lastStub && data.requests >= lastStub.requests) {
      const dt = (now - lastStub.t) / 1000
      if (dt > 0) stubReqRate.value = Math.max(0, (data.requests - lastStub.requests) / dt)
    }
    lastStub = { requests: data.requests, t: now }
    stub.value = data
  } catch (e: any) {
    stubErr.value = e?.response?.data?.error || 'stub_status unavailable'
    stopStub()
  }
}

function startStub() {
  stub.value = null
  lastStub = null
  stubReqRate.value = 0
  stubErr.value = ''
  pollStub()
  stubTimer = setInterval(pollStub, 2000)
}

function stopStub() {
  if (stubTimer !== null) {
    clearInterval(stubTimer)
    stubTimer = null
  }
}

// ── Test / reload ───────────────────────────────────────────────
async function testConfig() {
  if (hostId.value === null) return
  busy.value = true
  try {
    const { data } = await axios.post(`${base.value}/test`, null, { params: { bin: bin.value, sudo: sudoParam.value } })
    cmdOk.value = !!data.ok
    cmdOutput.value = data.output || (data.ok ? 'Configuration OK' : 'Test failed')
  } catch (e: any) {
    cmdOk.value = false
    cmdOutput.value = e?.response?.data?.error || `${bin.value} -t failed`
  } finally {
    busy.value = false
  }
}

async function reload(force = false) {
  if (hostId.value === null) return
  if (!force) {
    const ok = await confirm('Reload nginx on this host? Active connections are preserved.', 'Reload')
    if (!ok) return
  }
  busy.value = true
  try {
    const { data } = await axios.post(`${base.value}/reload`, null, {
      params: { bin: bin.value, sudo: sudoParam.value, force: force ? '1' : undefined },
    })
    // The backend validates with `nginx -t` first and refuses a broken config.
    if (data.ok === false && data.stage === 'test') {
      cmdOk.value = false
      cmdOutput.value = data.output || 'Config test failed — reload aborted.'
      busy.value = false
      const go = await confirm('Config test FAILED. Reloading may break nginx. Reload anyway?', 'Reload anyway')
      if (go) await reload(true)
      return
    }
    cmdOk.value = true
    cmdOutput.value = data.output?.trim() || 'Reloaded.'
    toast.success(`${bin.value} reloaded`)
    await detect(false)
  } catch (e: any) {
    cmdOk.value = false
    cmdOutput.value = e?.response?.data?.error || 'reload failed'
    toast.error('Reload failed')
  } finally {
    busy.value = false
  }
}

// ── Config ──────────────────────────────────────────────────────
async function loadTree() {
  if (hostId.value === null) return
  loadingTree.value = true
  try {
    const { data } = await axios.get<{ files: NginxFile[] }>(`${base.value}/config/tree`, { params: cfgParams.value })
    files.value = data.files || []
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      loadingTree.value = false
      return loadTree()
    }
    toast.error(e?.response?.data?.error || 'Failed to list config')
    files.value = []
  } finally {
    loadingTree.value = false
  }
}

// Opening a file switches to its tab if already open on this host (preserving
// whatever edits are sitting in it) or creates a new tab tagged with the
// current host and fetches its content — other open tabs, on this host or
// any other, are never touched or discarded.
async function openFile(p: string) {
  if (hostId.value === null) return
  const hid = hostId.value
  const key = tabKey(hid, p)
  const existing = tabs.value.find((t) => t.id === key)
  if (existing) {
    activateTab(existing)
    return
  }
  const newTab: EditorTab = {
    id: key, hostId: hid, path: p, content: '', origContent: '', loading: true, backups: [],
    root: configRoot.value, bin: bin.value, sudo: useSudo.value,
  }
  tabs.value.push(newTab)
  activeTabId.value = key
  lastActiveTabByHost[hid] = key
  try {
    const { data } = await axios.get(`${tabBase(newTab)}/config/file`, { params: { ...tabParams(newTab), path: p } })
    newTab.content = data.content || ''
    newTab.origContent = newTab.content
    loadBackups(newTab)
  } catch (e: any) {
    const idx = tabs.value.indexOf(newTab)
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      if (idx !== -1) tabs.value.splice(idx, 1)
      return openFile(p)
    }
    toast.error(e?.response?.data?.error || 'Failed to read file')
    if (idx !== -1) tabs.value.splice(idx, 1)
    if (activeTabId.value === key) {
      const sameHost = tabs.value.filter((t) => t.hostId === hid)
      activeTabId.value = sameHost.length ? sameHost[sameHost.length - 1].id : ''
    }
  } finally {
    newTab.loading = false
  }
}

// Closes a tab, prompting first if it has unsaved edits. When it was the
// active tab, prefers another tab on the same host next (so closing a tab
// doesn't unexpectedly flip the page to a different host); only falls back
// to a tab from another host — switching the page to match it — if that's
// all that's left.
async function closeTab(id: string) {
  const idx = tabs.value.findIndex((t) => t.id === id)
  if (idx === -1) return
  const t = tabs.value[idx]
  if (tabDirty(t) && !(await confirm(`Discard unsaved changes to ${t.path}?`, 'Discard'))) return
  tabs.value.splice(idx, 1)
  if (rightTabId.value === id) rightTabId.value = ''
  if (activeTabId.value !== id) return
  const sameHost = tabs.value.filter((t2) => t2.hostId === t.hostId)
  const fallback = sameHost.length ? sameHost[Math.min(idx, sameHost.length - 1)] : tabs.value[Math.min(idx, tabs.value.length - 1)]
  if (fallback) await activateTab(fallback)
  else activeTabId.value = ''
}

// loadBackups lists the *.bak.<unix> snapshots saved for the open file,
// writing directly into the given tab object (using that tab's own snapshot
// of root/bin/sudo, not necessarily the currently-selected host's) so a slow
// request can't land its results on the wrong tab if the user has since
// switched away, and split-pane tabs from another host still work correctly.
async function loadBackups(t: EditorTab) {
  try {
    const { data } = await axios.get<{ backups: NginxBackup[] }>(`${tabBase(t)}/config/backups`, {
      params: { ...tabParams(t), path: t.path },
    })
    t.backups = data.backups || []
  } catch {
    t.backups = []
  }
}

// loadBackup pulls an older snapshot into the editor (marking it dirty) so the
// user can review and Save to restore. The tab's own path stays the target.
async function revertTabTo(t: EditorTab, backupPath: string) {
  if (!backupPath) return
  if (tabDirty(t) && !(await confirm('Discard unsaved changes and load this backup?', 'Load backup'))) return
  try {
    const { data } = await axios.get(`${tabBase(t)}/config/file`, { params: { ...tabParams(t), path: backupPath } })
    t.content = data.content || ''
    toast.info('Backup loaded — review and Save to restore')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load backup')
  }
}
function revertTo(backupPath: string) {
  if (activeTab.value) return revertTabTo(activeTab.value, backupPath)
}

function backupLabel(b: NginxBackup): string {
  return b.time ? new Date(b.time * 1000).toLocaleString() : b.name
}

async function saveTab(t: EditorTab) {
  busy.value = true
  try {
    await axios.post(`${tabBase(t)}/config/file`, {
      root: t.root,
      path: t.path,
      content: t.content,
      sudo: t.sudo,
    })
    t.origContent = t.content
    toast.success('Saved — run Test config before reloading')
    loadBackups(t)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Save failed')
  } finally {
    busy.value = false
  }
}
async function saveFile() {
  if (activeTab.value) await saveTab(activeTab.value)
}

// ── Sites ───────────────────────────────────────────────────────
async function loadSites() {
  if (hostId.value === null) return
  loadingSites.value = true
  try {
    const { data } = await axios.get<{ layout: string; sites: NginxSite[] }>(`${base.value}/sites`, {
      params: { root: configRoot.value, layout: sitesLayout.value, sudo: sudoParam.value },
    })
    if (data.layout) sitesLayout.value = data.layout
    sites.value = data.sites || []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load sites')
    sites.value = []
  } finally {
    loadingSites.value = false
  }
}

async function toggleSite(site: NginxSite) {
  busy.value = true
  try {
    await axios.post(`${base.value}/sites/toggle`, {
      root: configRoot.value,
      name: site.name,
      layout: sitesLayout.value,
      enabled: !site.enabled,
      sudo: useSudo.value,
    })
    toast.success(`${site.name} ${!site.enabled ? 'enabled' : 'disabled'} — reload to apply`)
    await loadSites()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Toggle failed')
  } finally {
    busy.value = false
  }
}

// ── Logs ────────────────────────────────────────────────────────
async function loadLogList() {
  if (hostId.value === null) return
  try {
    const { data } = await axios.get<{ files: NginxFile[] }>(`${base.value}/logs`, { params: logParams.value })
    logFiles.value = data.files || []
    if (!activeLog.value && logFiles.value.length) {
      const def = logFiles.value.find((f) => f.path === 'access.log') || logFiles.value[0]
      await openLog(def.path)
    }
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      return loadLogList()
    }
    toast.error(e?.response?.data?.error || 'Failed to list logs')
    logFiles.value = []
  }
}

// fetchTail loads the last N lines. Returns false on an unrecovered error.
async function fetchTail(file: string, lines: number): Promise<boolean> {
  try {
    const { data } = await axios.get(`${base.value}/logs/tail`, { params: { ...logParams.value, file, lines } })
    logLines.value = (data.output || '').split('\n').filter((l: string) => l.length)
    nextTick(() => {
      if (logViewEl.value) logViewEl.value.scrollTop = logViewEl.value.scrollHeight
    })
    return true
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      return fetchTail(file, lines) // retry once, now elevated
    }
    toast.error(e?.response?.data?.error || 'Failed to tail log')
    return false
  }
}

async function openLog(file: string) {
  stopFollow()
  activeLog.value = file
  logLines.value = []
  if (searchActive.value && logQuery.value.trim()) await runSearch()
  else await fetchTail(file, 300)
}

// ── Log search ──────────────────────────────────────────────────
async function runSearch(): Promise<boolean> {
  const q = logQuery.value.trim()
  if (!q || !activeLog.value) return false
  stopFollow()
  searching.value = true
  searchActive.value = true
  try {
    const { data } = await axios.get(`${base.value}/logs/search`, {
      params: { ...logParams.value, file: activeLog.value, q, lines: 1000 },
    })
    logLines.value = (data.output || '').split('\n').filter((l: string) => l.length)
    if (!logLines.value.length) logLines.value = [`(no matches for "${q}")`]
    return true
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      searching.value = false
      return runSearch()
    }
    toast.error(e?.response?.data?.error || 'Search failed')
    return false
  } finally {
    searching.value = false
  }
}

function setLevel(level: string) {
  logQuery.value = `[${level}]`
  runSearch()
}

function clearSearch() {
  logQuery.value = ''
  searchActive.value = false
  if (activeLog.value) fetchTail(activeLog.value, 300)
}

// Live-tail by polling the snapshot endpoint. SSE is avoided on purpose: it
// doesn't survive reverse proxies / Cloudflare, which buffer streaming
// responses. Short polls are plain requests that work everywhere and respect sudo.
function toggleFollow() {
  if (following.value) {
    stopFollow()
    return
  }
  if (!activeLog.value) return
  following.value = true
  pollTimer = setInterval(async () => {
    const ok = await fetchTail(activeLog.value, 400)
    if (!ok) stopFollow()
  }, 2000)
}

function stopFollow() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  following.value = false
}

function formatBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(i ? 1 : 0)} ${u[i]}`
}

onMounted(() => {
  loadHosts()
  window.addEventListener('keydown', onEditorKeydown)
})
onBeforeUnmount(() => {
  stopFollow()
  stopStub()
  window.removeEventListener('keydown', onEditorKeydown)
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Nginx</div>
            <div class="page-subtitle">Edit configs, toggle sites, and follow access &amp; error logs across your servers over SSH.</div>
          </div>
          <div class="page-hero__actions">
            <button v-if="hosts.length" class="base-btn base-btn--sm" :class="{ 'base-btn--primary': fleetMode }" @click="toggleFleet">
              {{ fleetMode ? '← Back to host' : '▦ Fleet' }}
            </button>
            <SearchSelect
              v-if="hosts.length && !fleetMode"
              class="ng-host"
              :model-value="hostId"
              :options="hostOptions"
              placeholder="Select host…"
              @update:model-value="selectHost(Number($event))"
            />
            <!-- Connection status + connect/disconnect -->
            <template v-if="hostId !== null && !fleetMode">
              <div class="ng-conn-status" :class="`ng-conn-status--${hostStatus}`" :title="hostStatus === 'connected' ? 'SSH reachable' : hostStatus === 'error' ? 'SSH unreachable' : hostStatus === 'connecting' ? 'Testing…' : 'Not tested'">
                <span class="ng-conn-dot"></span>
                <span class="ng-conn-label">{{ hostStatus === 'connected' ? 'Connected' : hostStatus === 'error' ? 'Unreachable' : hostStatus === 'connecting' ? 'Testing…' : 'Unknown' }}</span>
              </div>
              <button
                v-if="hostStatus !== 'connected' && hostStatus !== 'connecting'"
                class="base-btn base-btn--sm"
                @click="pingHost"
              >Connect</button>
              <button
                v-if="hostStatus === 'connected'"
                class="base-btn base-btn--sm"
                @click="disconnectHost"
              >Disconnect</button>
            </template>
            <button v-if="hostId !== null && canReload && !fleetMode" class="base-btn base-btn--sm" :disabled="busy" @click="testConfig">Test config</button>
            <button v-if="hostId !== null && canReload && !fleetMode" class="base-btn base-btn--primary base-btn--sm" :disabled="busy" @click="reload()">Reload</button>
          </div>
        </section>

        <div v-if="!hosts.length" class="page-card ng-empty">
          <div class="ng-empty-icon">🌐</div>
          <h2>No SSH servers</h2>
          <p>Nginx management uses your remote SSH hosts. Add one under <b>Infrastructure → SSH Hosts</b> first.</p>
        </div>

        <!-- FLEET: all hosts at once -->
        <div v-else-if="fleetMode" class="page-card ng-sites">
          <div class="ng-sites-head">
            Fleet health <span class="ng-count">{{ fleet.length }}</span> · unhealthy first
            <input v-model="fleetSearch" class="base-input ng-searchinput" type="search" placeholder="Filter hosts…" />
            <div class="dk-spacer"></div>
            <button class="base-btn base-btn--sm" :disabled="loadingFleet" @click="loadFleet">{{ loadingFleet ? 'Probing…' : 'Refresh' }}</button>
          </div>
          <table class="ng-table">
            <thead>
              <tr>
                <th class="ng-th-sort" :class="{ sorted: fleetSortKey === 'name' }" @click="toggleFleetSort('name')">Host <SortIcon :active="fleetSortKey === 'name'" :dir="fleetSortDir" /></th>
                <th class="ng-th-sort" :class="{ sorted: fleetSortKey === 'version' }" @click="toggleFleetSort('version')">Version <SortIcon :active="fleetSortKey === 'version'" :dir="fleetSortDir" /></th>
                <th class="ng-th-sort" :class="{ sorted: fleetSortKey === 'active' }" @click="toggleFleetSort('active')">State <SortIcon :active="fleetSortKey === 'active'" :dir="fleetSortDir" /></th>
                <th class="ng-th-sort" :class="{ sorted: fleetSortKey === 'test_ok' }" @click="toggleFleetSort('test_ok')">Config test <SortIcon :active="fleetSortKey === 'test_ok'" :dir="fleetSortDir" /></th>
                <th class="ng-th-sort" :class="{ sorted: fleetSortKey === 'soonest_days' }" @click="toggleFleetSort('soonest_days')">Cert expiry <SortIcon :active="fleetSortKey === 'soonest_days'" :dir="fleetSortDir" /></th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loadingFleet"><td colspan="6" class="ng-msg">Probing all hosts over SSH…</td></tr>
              <tr v-else-if="!fleet.length"><td colspan="6" class="ng-msg">No SSH hosts.</td></tr>
              <tr v-else-if="!sortedFleet.length"><td colspan="6" class="ng-msg">No hosts match "{{ fleetSearch }}".</td></tr>
              <tr v-for="f in sortedFleet" :key="f.id">
                <td>
                  <div class="ng-sname">{{ f.name }}</div>
                  <div class="ng-fleet-sub">{{ f.ssh_host }}</div>
                </td>
                <td class="ng-mono">{{ f.version || '—' }}</td>
                <td>
                  <span v-if="!f.reachable" class="ng-pill ng-pill--err2" :title="f.error">unreachable</span>
                  <span v-else class="ng-pill" :class="f.active === 'active' ? 'ng-pill--ok' : 'ng-pill--warn'">{{ f.active || 'unknown' }}</span>
                </td>
                <td>
                  <span v-if="!f.reachable" class="ng-pill ng-pill--off">—</span>
                  <span v-else class="ng-pill" :class="f.test_ok ? 'ng-pill--ok' : 'ng-pill--err2'" :title="f.test_output">{{ f.test_ok ? 'passing' : 'FAILING' }}</span>
                </td>
                <td>
                  <span v-if="!f.reachable || f.soonest_days < 0" class="ng-pill ng-pill--off">{{ f.reachable ? 'no certs' : '—' }}</span>
                  <span v-else class="ng-pill" :class="f.soonest_days < 0 ? 'ng-pill--err2' : f.soonest_days < 14 ? 'ng-pill--warn' : f.soonest_days < 30 ? 'ng-pill--off' : 'ng-pill--ok'">
                    {{ f.soonest_days < 0 ? 'EXPIRED' : f.soonest_days + 'd' }}<template v-if="f.expiring > 0"> · {{ f.expiring }} soon</template>
                  </span>
                </td>
                <td class="ng-sact">
                  <button class="base-btn base-btn--xs" @click="openFromFleet(f.id)">Open →</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <template v-else-if="hostId !== null">
          <!-- Detected/overridable host settings -->
          <div class="page-card ng-settings">
            <div class="ng-set">
              <label>Binary</label>
              <SearchSelect v-model="bin" :options="binOptions" class="ng-binsel" />
            </div>
            <div class="ng-set ng-set--grow">
              <label>Config root</label>
              <div class="ng-pathrow">
                <input v-model="configRoot" class="base-input" spellcheck="false" />
                <button class="icon-btn" title="Copy path" @click="copyPath(configRoot)">
                  <ActionIcon name="copy" />
                </button>
              </div>
            </div>
            <div class="ng-set ng-set--grow">
              <label>Log directory</label>
              <div class="ng-pathrow">
                <input v-model="logDir" class="base-input" spellcheck="false" />
                <button class="icon-btn" title="Copy path" @click="copyPath(logDir)">
                  <ActionIcon name="copy" />
                </button>
              </div>
            </div>
            <div class="ng-set" title="If nginx runs inside a Docker container, name it here — logs are then read via `docker exec` instead of directly on the SSH host, since a container's own filesystem usually isn't visible over SFTP/SSH">
              <label>Log container <span class="ng-optional">optional</span></label>
              <input v-model="logContainer" class="base-input ng-logcontainer" spellcheck="false" placeholder="e.g. nginx-app" />
            </div>
            <label class="ng-sudo" title="Run privileged commands via sudo, using this host's SSH password (or NOPASSWD for key auth)">
              <input type="checkbox" v-model="useSudo" @change="loadCurrentTab" />
              Use sudo
            </label>
            <button class="base-btn base-btn--sm" :disabled="detecting" @click="rescan">{{ detecting ? 'Scanning…' : 'Rescan' }}</button>
          </div>

          <div v-if="version || active || cmdOutput" class="ng-statusbar">
            <span v-if="active" class="ng-pill" :class="active === 'active' ? 'ng-pill--ok' : 'ng-pill--warn'">{{ active }}</span>
            <span v-if="version" class="ng-version">{{ version }}</span>
            <pre v-if="cmdOutput" class="ng-cmdout" :class="{ 'ng-cmdout--err': !cmdOk }">{{ cmdOutput }}</pre>
          </div>

          <div class="ng-tabs">
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'config' }" @click="switchTab('config')">Config</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'map' }" @click="switchTab('map')">Map</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'sites' }" @click="switchTab('sites')">Sites</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'certs' }" @click="switchTab('certs')">Certs</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'logs' }" @click="switchTab('logs')">Logs</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'status' }" @click="switchTab('status')">Status</button>
          </div>

          <!-- CONFIG -->
          <div v-if="tab === 'config'" class="page-card ng-split">
            <div class="ng-filelist">
              <div class="ng-filelist-head">{{ loadingTree ? 'Loading…' : files.length + ' files · ' + configRoot }}</div>
              <button
                v-for="f in files"
                :key="f.path"
                class="ng-fileitem"
                :class="{ 'ng-fileitem--active': f.path === activeFile }"
                @click="openFile(f.path)"
              >
                <span class="ng-fname">{{ f.path }}</span>
                <span class="ng-fileitem-end">
                  <span class="ng-fsize">{{ formatBytes(f.size) }}</span>
                  <span class="icon-btn ng-fileitem-copy" title="Copy path" @click.stop="copyPath(f.path)">
                    <ActionIcon name="copy" />
                  </span>
                </span>
              </button>
            </div>
            <div class="ng-editor" :class="{ 'ng-editor--full': editorFullscreen }">
              <div v-if="currentHostTabs.length || splitView" class="ng-tabstrip">
                <button
                  v-for="t in currentHostTabs"
                  :key="t.id"
                  class="ng-tabchip"
                  :class="{ 'ng-tabchip--active': t.id === activeTabId }"
                  :title="t.path"
                  @click="activateTab(t)"
                >
                  <span class="ng-tabchip-name">{{ t.path.split('/').pop() }}</span>
                  <span v-if="tabDirty(t)" class="ng-tabchip-dot">●</span>
                  <span class="ng-tabchip-close" title="Close tab" @click.stop="closeTab(t.id)">×</span>
                </button>
                <button class="base-btn base-btn--sm ng-compare-btn" title="Open a file from another host side by side" @click="openComparePicker">
                  <ActionIcon name="split" /> Compare with another host
                </button>
              </div>
              <div class="ng-panes" :class="{ 'ng-panes--split': splitView }">
                <div class="ng-pane">
                  <div v-if="!activeFile" class="ng-editor-empty">Select a config file to view or edit.</div>
                  <template v-else>
                    <div class="ng-editor-bar">
                      <span class="ng-editor-path">{{ activeFile }}</span>
                      <span class="ng-tabchip-host">{{ activeTab && tabHostName(activeTab) }}</span>
                      <span v-if="dirty" class="ng-dirty">● unsaved</span>
                      <span class="ng-editor-bar-spacer"></span>
                      <button class="base-btn base-btn--sm" @click="openEffective">nginx -T</button>
                      <select
                        v-if="backups.length"
                        class="base-input base-input--sm ng-baksel"
                        :value="''"
                        title="Load a previous backup into the editor"
                        @change="revertTo(($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''"
                      >
                        <option value="" disabled>↩ Backups ({{ backups.length }})</option>
                        <option v-for="b in backups" :key="b.path" :value="b.path">{{ backupLabel(b) }}</option>
                      </select>
                      <button
                        v-if="canManage"
                        class="base-btn base-btn--primary base-btn--sm"
                        :disabled="busy || !dirty"
                        @click="saveFile"
                      >Save</button>
                      <button
                        class="icon-btn"
                        :title="editorFullscreen ? 'Exit fullscreen (Esc)' : 'Fullscreen'"
                        @click="toggleEditorFullscreen"
                      >
                        <ActionIcon :name="editorFullscreen ? 'collapse' : 'expand'" />
                      </button>
                    </div>
                    <NginxEditor
                      v-model="fileContent"
                      :readonly="!canManage || loadingFile"
                      class="ng-cm-wrap"
                    />
                  </template>
                </div>
                <div v-if="splitView" class="ng-pane">
                  <div v-if="!rightTab" class="ng-editor-empty">Loading…</div>
                  <template v-else>
                    <div class="ng-editor-bar">
                      <span class="ng-editor-path">{{ rightTab.path }}</span>
                      <span class="ng-tabchip-host">{{ tabHostName(rightTab) }}</span>
                      <span v-if="tabDirty(rightTab)" class="ng-dirty">● unsaved</span>
                      <span class="ng-editor-bar-spacer"></span>
                      <select
                        v-if="rightTab.backups.length"
                        class="base-input base-input--sm ng-baksel"
                        :value="''"
                        title="Load a previous backup into the editor"
                        @change="revertTabTo(rightTab, ($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''"
                      >
                        <option value="" disabled>↩ Backups ({{ rightTab.backups.length }})</option>
                        <option v-for="b in rightTab.backups" :key="b.path" :value="b.path">{{ backupLabel(b) }}</option>
                      </select>
                      <button
                        v-if="canManage"
                        class="base-btn base-btn--primary base-btn--sm"
                        :disabled="busy || !tabDirty(rightTab)"
                        @click="saveTab(rightTab)"
                      >Save</button>
                      <button class="icon-btn" title="Close split" @click="closeSplit">
                        <ActionIcon name="close" />
                      </button>
                    </div>
                    <NginxEditor
                      v-model="rightContent"
                      :readonly="!canManage || rightTab.loading"
                      class="ng-cm-wrap"
                    />
                  </template>
                </div>
              </div>
            </div>
          </div>

          <!-- Compare-with-another-host picker (the only way into split view) -->
          <div v-if="showComparePicker" class="ng-modal-backdrop" @click.self="closeComparePicker">
            <div class="ng-modal ng-modal--compare page-card">
              <div class="ng-modal-head">
                <h3>Compare with another host</h3>
                <button class="icon-btn" @click="closeComparePicker"><ActionIcon name="close" /></button>
              </div>
              <div class="ng-modal-body">
                <label class="ng-modal-label">Host</label>
                <SearchSelect
                  :model-value="compareHostId"
                  :options="compareHostOptions"
                  placeholder="Select a host…"
                  @update:model-value="selectCompareHost(Number($event))"
                />
                <template v-if="compareHostId !== null">
                  <label class="ng-modal-label">File</label>
                  <div class="ng-compare-files">
                    <div v-if="compareLoadingFiles" class="ng-editor-empty">Loading files…</div>
                    <div v-else-if="!compareFiles.length" class="ng-editor-empty">No files found.</div>
                    <button
                      v-for="f in compareFiles"
                      :key="f.path"
                      class="ng-fileitem"
                      @click="openCompareFile(f.path)"
                    >
                      <span class="ng-fname">{{ f.path }}</span>
                      <span class="ng-fsize">{{ formatBytes(f.size) }}</span>
                    </button>
                  </div>
                </template>
              </div>
            </div>
          </div>

          <!-- SITES -->
          <div v-else-if="tab === 'sites'" class="page-card ng-sites">
            <div class="ng-sites-head">
              Layout:
              <span class="ng-pill ng-pill--off">{{ sitesLayout || 'unknown' }}</span>
              <span class="ng-sites-hint">
                {{ sitesLayout === 'confd' ? 'enable/disable renames .conf ↔ .conf.disabled' : sitesLayout === 'symlink' ? 'sites-available ↔ sites-enabled symlinks' : '' }}
              </span>
              <input v-model="siteSearch" class="base-input ng-searchinput" type="search" placeholder="Filter sites…" />
            </div>
            <table class="ng-table">
              <thead>
                <tr>
                  <th class="ng-th-sort" :class="{ sorted: siteSortKey === 'name' }" @click="toggleSiteSort('name')">File <SortIcon :active="siteSortKey === 'name'" :dir="siteSortDir" /></th>
                  <th class="ng-th-sort" :class="{ sorted: siteSortKey === 'state' }" @click="toggleSiteSort('state')">Status <SortIcon :active="siteSortKey === 'state'" :dir="siteSortDir" /></th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="loadingSites"><td colspan="3" class="ng-msg">Loading…</td></tr>
                <tr v-else-if="!sites.length"><td colspan="3" class="ng-msg">No server blocks found.</td></tr>
                <tr v-else-if="!sortedSites.length"><td colspan="3" class="ng-msg">No sites match "{{ siteSearch }}".</td></tr>
                <tr v-for="s in sortedSites" :key="s.name">
                  <td class="ng-sname">{{ s.name }}</td>
                  <td>
                    <span
                      class="ng-pill"
                      :class="s.state === 'enabled' ? 'ng-pill--ok' : s.state === 'disabled' ? 'ng-pill--off' : 'ng-pill--warn'"
                    >{{ s.state }}</span>
                  </td>
                  <td class="ng-sact">
                    <button
                      v-if="canManage && s.toggleable"
                      class="base-btn base-btn--xs"
                      :class="s.enabled ? 'base-btn--danger' : ''"
                      :disabled="busy"
                      @click="toggleSite(s)"
                    >{{ s.enabled ? 'Disable' : 'Enable' }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- MAP -->
          <div v-else-if="tab === 'map'" class="ng-mapwrap">
            <div class="page-card ng-mapcard">
              <div class="ng-mapcard-head">
                Servers <span class="ng-count">{{ servers.length }}</span>
                <input v-model="serverSearch" class="base-input ng-searchinput ng-mapsearch" type="search" placeholder="Filter servers…" />
              </div>
              <table class="ng-table">
                <thead>
                  <tr><th>server_name</th><th>listen</th><th>root / proxy_pass</th><th>cert</th></tr>
                </thead>
                <tbody>
                  <tr v-if="loadingMap"><td colspan="4" class="ng-msg">Parsing config…</td></tr>
                  <tr v-else-if="!servers.length"><td colspan="4" class="ng-msg">No server blocks.</td></tr>
                  <tr v-else-if="!filteredServers.length"><td colspan="4" class="ng-msg">No servers match "{{ serverSearch }}".</td></tr>
                  <tr v-for="(s, i) in filteredServers" :key="i">
                    <td class="ng-sname">{{ s.names || '—' }}</td>
                    <td class="ng-mono">{{ (s.listen || []).join(', ') || '—' }}</td>
                    <td class="ng-mono">
                      <span v-if="s.root">root: {{ s.root }}</span>
                      <span v-for="p in (s.proxy_pass || [])" :key="p" class="ng-proxy">→ {{ p }}</span>
                      <span v-if="!s.root && !(s.proxy_pass || []).length">—</span>
                    </td>
                    <td class="ng-mono ng-cert">{{ s.cert ? s.cert.split('/').pop() : '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="page-card ng-mapcard">
              <div class="ng-mapcard-head">
                Upstreams <span class="ng-count">{{ upstreams.length }}</span>
                <input v-model="upstreamSearch" class="base-input ng-searchinput ng-mapsearch" type="search" placeholder="Filter upstreams…" />
              </div>
              <table class="ng-table">
                <thead><tr><th>name</th><th>servers</th></tr></thead>
                <tbody>
                  <tr v-if="loadingMap"><td colspan="2" class="ng-msg">Parsing…</td></tr>
                  <tr v-else-if="!upstreams.length"><td colspan="2" class="ng-msg">No upstreams.</td></tr>
                  <tr v-else-if="!filteredUpstreams.length"><td colspan="2" class="ng-msg">No upstreams match "{{ upstreamSearch }}".</td></tr>
                  <tr v-for="u in filteredUpstreams" :key="u.name">
                    <td class="ng-sname">{{ u.name }}</td>
                    <td class="ng-mono">{{ (u.servers || []).join(', ') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- CERTS -->
          <div v-else-if="tab === 'certs'" class="page-card ng-sites">
            <div class="ng-sites-head">
              SSL certificates <span class="ng-count">{{ certs.length }}</span> · soonest expiry first
              <input v-model="certSearch" class="base-input ng-searchinput" type="search" placeholder="Filter certificates…" />
            </div>
            <table class="ng-table">
              <thead>
                <tr>
                  <th class="ng-th-sort" :class="{ sorted: certSortKey === 'domains' }" @click="toggleCertSort('domains')">Domains <SortIcon :active="certSortKey === 'domains'" :dir="certSortDir" /></th>
                  <th class="ng-th-sort" :class="{ sorted: certSortKey === 'not_after' }" @click="toggleCertSort('not_after')">Expires <SortIcon :active="certSortKey === 'not_after'" :dir="certSortDir" /></th>
                  <th class="ng-th-sort" :class="{ sorted: certSortKey === 'days_left' }" @click="toggleCertSort('days_left')">Days left <SortIcon :active="certSortKey === 'days_left'" :dir="certSortDir" /></th>
                  <th>Certificate</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="loadingCerts"><td colspan="4" class="ng-msg">Reading certificates…</td></tr>
                <tr v-else-if="!certs.length"><td colspan="4" class="ng-msg">No ssl_certificate directives found.</td></tr>
                <tr v-else-if="!sortedCerts.length"><td colspan="4" class="ng-msg">No certificates match "{{ certSearch }}".</td></tr>
                <tr v-for="(c, i) in sortedCerts" :key="i">
                  <td class="ng-mono">{{ (c.domains || []).length ? c.domains.join(', ') : (c.subject || '—') }}</td>
                  <td class="ng-mono">{{ c.error ? '—' : c.not_after }}</td>
                  <td>
                    <span v-if="c.error" class="ng-pill ng-pill--warn">error</span>
                    <span v-else class="ng-pill" :class="c.expired ? 'ng-pill--err2' : c.days_left < 14 ? 'ng-pill--warn' : c.days_left < 30 ? 'ng-pill--off' : 'ng-pill--ok'">
                      {{ c.expired ? 'EXPIRED' : c.days_left + 'd' }}
                    </span>
                  </td>
                  <td class="ng-mono ng-cert" :title="c.error || c.path">
                    <span>{{ c.path }}</span>
                    <span v-if="c.path" class="icon-btn ng-fileitem-copy" title="Copy path" @click.stop="copyPath(c.path)">
                      <ActionIcon name="copy" />
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- STATUS -->
          <div v-else-if="tab === 'status'" class="page-card ng-status">
            <div class="ng-statusrow">
              <input v-model="stubUrl" class="base-input ng-stuburl" spellcheck="false" @keyup.enter="startStub" />
              <button class="base-btn base-btn--sm base-btn--primary" @click="startStub">Reconnect</button>
              <span v-if="stubTimer" class="ng-logmeta">polling · 2s</span>
            </div>
            <div v-if="stubErr" class="ng-cmdout ng-cmdout--err">{{ stubErr }}</div>
            <div v-else-if="!stub" class="ng-msg">Connecting to stub_status…</div>
            <div v-else class="ng-metrics">
              <div class="ng-metric"><div class="ng-metric-val">{{ stub.active }}</div><div class="ng-metric-lbl">Active conn.</div></div>
              <div class="ng-metric"><div class="ng-metric-val">{{ stubReqRate.toFixed(1) }}</div><div class="ng-metric-lbl">req/s</div></div>
              <div class="ng-metric"><div class="ng-metric-val">{{ stub.reading }}</div><div class="ng-metric-lbl">Reading</div></div>
              <div class="ng-metric"><div class="ng-metric-val">{{ stub.writing }}</div><div class="ng-metric-lbl">Writing</div></div>
              <div class="ng-metric"><div class="ng-metric-val">{{ stub.waiting }}</div><div class="ng-metric-lbl">Waiting</div></div>
              <div class="ng-metric"><div class="ng-metric-val">{{ stub.requests.toLocaleString() }}</div><div class="ng-metric-lbl">Total requests</div></div>
            </div>
          </div>

          <!-- LOGS -->
          <div v-else-if="tab === 'logs'" class="page-card ng-logs">
            <div class="ng-logbar">
              <SearchSelect
                class="ng-logsel"
                :model-value="activeLog"
                :options="logOptions"
                placeholder="Select a log…"
                @update:model-value="openLog(String($event))"
              />
              <button v-if="activeLog" class="icon-btn" title="Copy log path" @click="copyPath(`${logDir}/${activeLog}`)">
                <ActionIcon name="copy" />
              </button>
              <button
                class="base-btn base-btn--sm"
                :class="following ? 'base-btn--danger' : 'base-btn--primary'"
                :disabled="!activeLog || isCompressed"
                :title="isCompressed ? 'Compressed archives are static — nothing to follow' : ''"
                @click="toggleFollow"
              >{{ following ? '■ Stop' : '▶ Follow' }}</button>
              <div class="dk-spacer"></div>
              <span class="ng-logmeta">
                {{ logLines.length }} lines{{ searchActive ? ' · search' : following ? ' · live (polling)' : isCompressed ? ' · archive' : '' }}
              </span>
            </div>
            <div class="ng-searchbar">
              <input
                v-model="logQuery"
                class="base-input ng-searchinput"
                placeholder="Search this log (grep, case-insensitive)…"
                @keyup.enter="runSearch"
              />
              <button class="base-btn base-btn--sm base-btn--primary" :disabled="!logQuery.trim() || searching" @click="runSearch">
                {{ searching ? 'Searching…' : 'Search' }}
              </button>
              <button v-if="searchActive" class="base-btn base-btn--sm" @click="clearSearch">Clear</button>
              <span class="ng-levels">
                <button v-for="lvl in LEVELS" :key="lvl" class="ng-level" @click="setLevel(lvl)">{{ lvl }}</button>
              </span>
            </div>
            <pre ref="logViewEl" class="ng-logview">{{ logLines.join('\n') || 'No log output.' }}</pre>
          </div>
        </template>

        <!-- Idle: hosts exist but none selected (after disconnect) -->
        <div v-else class="page-card ng-idle">
          <div class="ng-idle-icon">🌐</div>
          <p>Select a host from the dropdown above to connect.</p>
        </div>
      </div>
    </div>

    <!-- Effective config (nginx -T) modal -->
    <div v-if="showEffective" class="ng-modal-backdrop" @click.self="showEffective = false">
      <div class="ng-modal page-card">
        <div class="ng-modal-head">
          <span>Effective config — <code>{{ bin }} -T</code> (merged across all includes)</span>
          <button class="base-btn base-btn--sm" @click="showEffective = false">Close</button>
        </div>
        <div v-if="loadingEffective" class="ng-msg">Running {{ bin }} -T…</div>
        <NginxEditor v-else v-model="effectiveContent" :readonly="true" class="ng-modal-editor" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.ng-host { min-width: 240px; }

/* ── Host connection status indicator ── */
.ng-conn-status {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid transparent;
}
.ng-conn-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.ng-conn-status--connected { background: rgba(34,197,94,0.12); color: var(--success); border-color: rgba(34,197,94,0.25); }
.ng-conn-status--connected .ng-conn-dot { background: var(--success); }
.ng-conn-status--error { background: rgba(239,68,68,0.1); color: var(--danger); border-color: rgba(239,68,68,0.25); }
.ng-conn-status--error .ng-conn-dot { background: var(--danger); }
.ng-conn-status--connecting { background: var(--bg-hover); color: var(--text-muted); border-color: var(--border); }
.ng-conn-status--connecting .ng-conn-dot { background: var(--text-muted); animation: blink 1s step-end infinite; }
.ng-conn-status--unknown { background: var(--bg-hover); color: var(--text-muted); border-color: var(--border); }
.ng-conn-status--unknown .ng-conn-dot { background: var(--text-muted); }
@keyframes blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.2; } }
.ng-empty { text-align: center; padding: 64px 20px; color: var(--text-secondary); }
.ng-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.ng-idle-icon { font-size: 22px; }
.ng-empty-icon { font-size: 44px; }
.ng-empty h2 { margin: 12px 0 4px; font-size: 16px; color: var(--text-primary); }
.ng-empty p { font-size: 13px; color: var(--text-muted); }

.ng-settings { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; padding: 12px 14px; }
.ng-set { display: flex; flex-direction: column; gap: 4px; }
.ng-set--grow { flex: 1 1 220px; }
.ng-set label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); font-weight: 600; }
.ng-set .base-input { width: 100%; font-family: var(--mono); font-size: 12px; }
.ng-optional { text-transform: none; letter-spacing: normal; font-weight: 400; opacity: 0.7; }
.ng-pathrow { display: flex; align-items: center; gap: 4px; }
.ng-pathrow .base-input { flex: 1; }
.ng-fileitem-end { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.ng-fileitem-copy { width: 22px; height: 22px; }
.ng-logcontainer { width: 150px; }
.ng-sudo { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-secondary); white-space: nowrap; cursor: pointer; padding-bottom: 6px; }
.ng-sudo input { cursor: pointer; }

.ng-statusbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ng-pill { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 99px; text-transform: uppercase; letter-spacing: 0.03em; }
.ng-pill--ok { background: var(--success-dim, rgba(34,197,94,0.15)); color: var(--success); }
.ng-pill--warn { background: var(--warning-dim, rgba(234,179,8,0.15)); color: var(--warning); }
.ng-pill--off { background: var(--bg-hover); color: var(--text-muted); }
.ng-version { font-family: var(--mono); font-size: 12px; color: var(--text-muted); }
.ng-cmdout { flex: 1 1 100%; margin: 0; font-family: var(--mono); font-size: 12px; white-space: pre-wrap; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--r-md); padding: 8px 10px; color: var(--text-secondary); }
.ng-cmdout--err { border-color: var(--danger); color: var(--danger); }

.ng-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--border); }
.ng-tab { background: none; border: none; padding: 8px 14px; font-size: 13px; color: var(--text-muted); cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -1px; }
.ng-tab:hover { color: var(--text-primary); }
.ng-tab--active { color: var(--brand); border-bottom-color: var(--brand); font-weight: 600; }

.ng-split { display: grid; grid-template-columns: 300px 1fr; gap: 0; padding: 0; overflow: hidden; min-height: 460px; }
.ng-filelist { border-right: 1px solid var(--border); overflow-y: auto; max-height: 70vh; }
.ng-filelist-head { padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; word-break: break-all; }
.ng-fileitem { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; background: none; border: none; border-bottom: 1px solid var(--border); padding: 8px 12px; font-size: 12px; color: var(--text-secondary); cursor: pointer; text-align: left; }
.ng-fileitem:hover { background: var(--bg-hover); }
.ng-fileitem--active { background: var(--brand-dim); color: var(--brand); }
.ng-fname { font-family: var(--mono); word-break: break-all; }
.ng-fsize { color: var(--text-muted); white-space: nowrap; font-size: 11px; }

.ng-editor { display: flex; flex-direction: column; min-width: 0; }
.ng-editor--full { position: fixed; inset: 0; z-index: 200; background: var(--bg-surface); border-radius: 0; }
.ng-editor--full .ng-cm-wrap { flex: 1; min-height: 0; }
.ng-editor-empty { display: flex; align-items: center; justify-content: center; flex: 1; color: var(--text-muted); font-size: 13px; }
.ng-editor-bar { display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-bottom: 1px solid var(--border); }
.ng-tabstrip { display: flex; align-items: center; gap: 2px; overflow-x: auto; border-bottom: 1px solid var(--border); background: var(--bg-elevated); padding: 0 4px; flex-shrink: 0; }
.ng-tabchip { display: flex; align-items: center; gap: 6px; background: none; border: none; border-right: 1px solid var(--border); padding: 7px 8px 7px 12px; font-size: 12px; color: var(--text-muted); cursor: pointer; white-space: nowrap; }
.ng-tabchip:hover { color: var(--text-primary); }
.ng-tabchip--active { color: var(--text-primary); background: var(--bg-surface); border-bottom: 2px solid var(--brand); }
.ng-tabchip-name { font-family: var(--mono); }
.ng-tabchip-host { font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; opacity: 0.8; }
.ng-tabchip-dot { color: var(--brand); font-size: 8px; }
.ng-tabchip-close { display: inline-flex; align-items: center; justify-content: center; width: 16px; height: 16px; border-radius: var(--r-sm); font-size: 14px; line-height: 1; color: var(--text-muted); }
.ng-tabchip-close:hover { background: var(--bg-hover); color: var(--danger); }
.ng-compare-btn { display: inline-flex; align-items: center; gap: 6px; margin: 4px 4px 4px auto; flex-shrink: 0; white-space: nowrap; }
.ng-panes { display: flex; flex: 1; min-height: 0; }
.ng-panes--split .ng-pane { border-right: 1px solid var(--border); }
.ng-panes--split .ng-pane:last-child { border-right: none; }
.ng-pane { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ng-editor-path { font-family: var(--mono); font-size: 12px; color: var(--text-primary); word-break: break-all; }
.ng-dirty { color: var(--warning); font-size: 11px; }
.ng-textarea { flex: 1; min-height: 420px; border: none; resize: none; padding: 12px; font-family: var(--mono); font-size: 12.5px; line-height: 1.55; background: var(--bg-base, var(--bg-surface)); color: var(--text-primary); outline: none; tab-size: 4; }

.ng-sites { padding: 4px 6px; }
.ng-sites-head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; font-size: 12px; color: var(--text-muted); }
.ng-sites-hint { font-size: 11px; color: var(--text-muted); }
.ng-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.ng-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.ng-th-sort { cursor: pointer; user-select: none; }
.ng-th-sort:hover { color: var(--text-primary); }
.ng-table td { padding: 9px 12px; border-bottom: 1px solid var(--border); }
.ng-table tbody tr:last-child td { border-bottom: none; }
.ng-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.ng-sname { font-family: var(--mono); }
.ng-sfile { font-family: var(--mono); color: var(--text-muted); font-size: 12px; }
.ng-sact { text-align: right; }

.ng-logs { display: flex; flex-direction: column; gap: 10px; }
.ng-logbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ng-logsel { min-width: 240px; flex: 1 1 320px; max-width: 560px; }
.ng-binsel { min-width: 130px; }
.ng-logmeta { font-size: 12px; color: var(--text-muted); font-family: var(--mono); }
.ng-logview { margin: 0; max-height: 62vh; overflow: auto; background: var(--bg-base, #0b0e14); color: var(--text-secondary); font-family: var(--mono); font-size: 12px; line-height: 1.5; padding: 12px; border-radius: var(--r-md); border: 1px solid var(--border); white-space: pre; }

.ng-searchbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ng-searchinput { flex: 1 1 280px; font-family: var(--mono); font-size: 12px; }
.ng-levels { display: flex; gap: 4px; }
.ng-level { background: var(--bg-hover); border: 1px solid var(--border); color: var(--text-secondary); font-size: 11px; padding: 3px 8px; border-radius: 99px; cursor: pointer; font-family: var(--mono); }
.ng-level:hover { background: var(--brand-dim); color: var(--brand); border-color: var(--brand); }
.ng-baksel { min-width: 150px; max-width: 220px; }
.base-input--sm { padding: 4px 8px; font-size: 12px; }

.ng-cm-wrap { flex: 1; min-height: 440px; }
.ng-fleet-sub { font-family: var(--mono); font-size: 11px; color: var(--text-muted); margin-top: 2px; }

/* Map */
.ng-mapwrap { display: flex; flex-direction: column; gap: 14px; }
.ng-mapcard { padding: 4px 6px; }
.ng-mapcard-head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; font-size: 12px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.04em; }
.ng-mapsearch { flex: 0 1 220px; margin-left: auto; text-transform: none; letter-spacing: normal; }
.ng-count { display: inline-block; background: var(--bg-hover); color: var(--text-muted); border-radius: 99px; padding: 1px 8px; font-size: 11px; margin-left: 4px; }
.ng-mono { font-family: var(--mono); font-size: 12px; color: var(--text-secondary); word-break: break-all; }
.ng-proxy { display: block; color: var(--brand); }
.ng-cert { color: var(--text-muted); display: flex; align-items: center; gap: 6px; }

/* Cert pill */
.ng-pill--err2 { background: var(--danger); color: #fff; }

/* Status */
.ng-status { display: flex; flex-direction: column; gap: 14px; }
.ng-statusrow { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ng-stuburl { min-width: 280px; font-family: var(--mono); font-size: 12px; }
.ng-metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; }
.ng-metric { background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--r-lg); padding: 18px 16px; text-align: center; }
.ng-metric-val { font-size: 28px; font-weight: 700; color: var(--text-primary); font-family: var(--mono); }
.ng-metric-lbl { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; margin-top: 4px; }

/* Effective config modal */
.ng-modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 120; padding: 24px; }
.ng-modal { width: 1000px; max-width: 96vw; height: 82vh; display: flex; flex-direction: column; padding: 0; overflow: hidden; }
.ng-modal-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--border); font-size: 13px; color: var(--text-secondary); }
.ng-modal-head code { font-family: var(--mono); color: var(--brand); }
.ng-modal-editor { flex: 1; min-height: 0; }
.ng-modal--compare { width: 480px; height: auto; max-height: 80vh; }
.ng-modal-body { display: flex; flex-direction: column; gap: 6px; padding: 16px; overflow-y: auto; }
.ng-modal-label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); font-weight: 600; margin-top: 8px; }
.ng-modal-label:first-child { margin-top: 0; }
.ng-compare-files { border: 1px solid var(--border); border-radius: var(--r-md); overflow-y: auto; max-height: 320px; }
.ng-editor-bar-spacer { flex: 1; }
</style>
