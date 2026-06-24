<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import NginxEditor from '@/components/nginx/NginxEditor.vue'

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

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['nginx.manage']))
const canReload = computed(() => hasAnyPermission(['nginx.reload']))

const hosts = ref<SshHost[]>([])
const hostId = ref<number | null>(null)
const tab = ref<NginxTab>('config')

// Per-host detected/overridable settings
const bin = ref('nginx')
const configRoot = ref('/etc/nginx')
const logDir = ref('/var/log/nginx')
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
const activeFile = ref('')
const fileContent = ref('')
const origContent = ref('')
const loadingTree = ref(false)
const loadingFile = ref(false)
const dirty = computed(() => fileContent.value !== origContent.value)

// Sites
const sites = ref<NginxSite[]>([])
const loadingSites = ref(false)

// Logs
const logFiles = ref<NginxFile[]>([])
const activeLog = ref('')
const logLines = ref<string[]>([])
const following = ref(false)
const logViewEl = ref<HTMLElement | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

// Config backups (revert)
interface NginxBackup {
  path: string
  name: string
  time: number
}
const backups = ref<NginxBackup[]>([])

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

// Certs
const certs = ref<NginxCert[]>([])
const loadingCerts = ref(false)

// stub_status
const stub = ref<NginxStub | null>(null)
const stubUrl = ref('http://127.0.0.1/nginx_status')
const stubReqRate = ref(0)
const stubErr = ref('')
let stubTimer: ReturnType<typeof setInterval> | null = null
let lastStub: { requests: number; t: number } | null = null

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
const logParams = computed(() => ({ dir: logDir.value, bin: bin.value, sudo: sudoParam.value }))

// ── Hosts ───────────────────────────────────────────────────────
async function loadHosts() {
  try {
    const { data } = await axios.get<SshHost[]>('/api/docker/hosts')
    hosts.value = data.filter((h) => h.ssh_host)
    if (hostId.value === null && hosts.value.length) {
      hostId.value = hosts.value[0].id
      await onHostChange()
    }
  } catch {
    toast.error('Failed to load hosts')
  }
}

async function selectHost(id: number) {
  hostId.value = id
  await onHostChange()
}

async function onHostChange() {
  stopFollow()
  stopStub()
  cmdOutput.value = ''
  activeFile.value = ''
  fileContent.value = ''
  origContent.value = ''
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

// Persist settings (debounced) whenever the user changes binary, paths, or sudo.
watch([bin, configRoot, logDir, useSudo], () => {
  if (suppressSave || hostId.value === null) return
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(saveSettings, 600)
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

async function openFile(p: string) {
  if (dirty.value && !(await confirm('Discard unsaved changes?', 'Discard'))) return
  loadingFile.value = true
  activeFile.value = p
  backups.value = []
  try {
    const { data } = await axios.get(`${base.value}/config/file`, { params: { ...cfgParams.value, path: p } })
    fileContent.value = data.content || ''
    origContent.value = fileContent.value
    loadBackups(p)
  } catch (e: any) {
    if (!useSudo.value && isPermDenied(e)) {
      useSudo.value = true
      toast.info('Permission denied — retrying with sudo')
      loadingFile.value = false
      return openFile(p)
    }
    toast.error(e?.response?.data?.error || 'Failed to read file')
    fileContent.value = ''
    origContent.value = ''
  } finally {
    loadingFile.value = false
  }
}

// loadBackups lists the *.bak.<unix> snapshots saved for the open file.
async function loadBackups(p: string) {
  try {
    const { data } = await axios.get<{ backups: NginxBackup[] }>(`${base.value}/config/backups`, {
      params: { ...cfgParams.value, path: p },
    })
    backups.value = data.backups || []
  } catch {
    backups.value = []
  }
}

// loadBackup pulls an older snapshot into the editor (marking it dirty) so the
// user can review and Save to restore. activeFile stays the original target.
async function revertTo(backupPath: string) {
  if (!backupPath) return
  if (dirty.value && !(await confirm('Discard unsaved changes and load this backup?', 'Load backup'))) return
  try {
    const { data } = await axios.get(`${base.value}/config/file`, { params: { ...cfgParams.value, path: backupPath } })
    fileContent.value = data.content || ''
    toast.info('Backup loaded — review and Save to restore')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load backup')
  }
}

function backupLabel(b: NginxBackup): string {
  return b.time ? new Date(b.time * 1000).toLocaleString() : b.name
}

async function saveFile() {
  if (!activeFile.value) return
  busy.value = true
  try {
    await axios.post(`${base.value}/config/file`, {
      root: configRoot.value,
      path: activeFile.value,
      content: fileContent.value,
      sudo: useSudo.value,
    })
    origContent.value = fileContent.value
    toast.success('Saved — run Test config before reloading')
    loadBackups(activeFile.value)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Save failed')
  } finally {
    busy.value = false
  }
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

onMounted(loadHosts)
onBeforeUnmount(() => {
  stopFollow()
  stopStub()
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
            <select
              v-if="hosts.length"
              class="base-input ng-host"
              :value="hostId ?? ''"
              @change="selectHost(Number(($event.target as HTMLSelectElement).value))"
            >
              <option v-for="h in hosts" :key="h.id" :value="h.id">{{ h.name }} ({{ h.ssh_host }})</option>
            </select>
            <button v-if="hostId !== null && canReload" class="base-btn base-btn--sm" :disabled="busy" @click="testConfig">Test config</button>
            <button v-if="hostId !== null && canReload" class="base-btn base-btn--primary base-btn--sm" :disabled="busy" @click="reload()">Reload</button>
          </div>
        </section>

        <div v-if="!hosts.length" class="page-card ng-empty">
          <div class="ng-empty-icon">🌐</div>
          <h2>No SSH servers</h2>
          <p>Nginx management uses your remote SSH hosts. Add one under <b>Docker → Add host</b> (Remote host) first.</p>
        </div>

        <template v-else>
          <!-- Detected/overridable host settings -->
          <div class="page-card ng-settings">
            <div class="ng-set">
              <label>Binary</label>
              <select v-model="bin" class="base-input">
                <option value="nginx">nginx</option>
                <option value="openresty">openresty</option>
              </select>
            </div>
            <div class="ng-set ng-set--grow">
              <label>Config root</label>
              <input v-model="configRoot" class="base-input" spellcheck="false" />
            </div>
            <div class="ng-set ng-set--grow">
              <label>Log directory</label>
              <input v-model="logDir" class="base-input" spellcheck="false" />
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
                <span class="ng-fsize">{{ formatBytes(f.size) }}</span>
              </button>
            </div>
            <div class="ng-editor">
              <div v-if="!activeFile" class="ng-editor-empty">Select a config file to view or edit.</div>
              <template v-else>
                <div class="ng-editor-bar">
                  <span class="ng-editor-path">{{ activeFile }}</span>
                  <span v-if="dirty" class="ng-dirty">● unsaved</span>
                  <div class="dk-spacer"></div>
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
                </div>
                <NginxEditor
                  v-model="fileContent"
                  :readonly="!canManage || loadingFile"
                  class="ng-cm-wrap"
                />
              </template>
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
            </div>
            <table class="ng-table">
              <thead>
                <tr><th>File</th><th>Status</th><th></th></tr>
              </thead>
              <tbody>
                <tr v-if="loadingSites"><td colspan="3" class="ng-msg">Loading…</td></tr>
                <tr v-else-if="!sites.length"><td colspan="3" class="ng-msg">No server blocks found.</td></tr>
                <tr v-for="s in sites" :key="s.name">
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
              <div class="ng-mapcard-head">Servers <span class="ng-count">{{ servers.length }}</span></div>
              <table class="ng-table">
                <thead>
                  <tr><th>server_name</th><th>listen</th><th>root / proxy_pass</th><th>cert</th></tr>
                </thead>
                <tbody>
                  <tr v-if="loadingMap"><td colspan="4" class="ng-msg">Parsing config…</td></tr>
                  <tr v-else-if="!servers.length"><td colspan="4" class="ng-msg">No server blocks.</td></tr>
                  <tr v-for="(s, i) in servers" :key="i">
                    <td class="ng-sname">{{ s.names || '—' }}</td>
                    <td class="ng-mono">{{ s.listen.join(', ') || '—' }}</td>
                    <td class="ng-mono">
                      <span v-if="s.root">root: {{ s.root }}</span>
                      <span v-for="p in s.proxy_pass" :key="p" class="ng-proxy">→ {{ p }}</span>
                      <span v-if="!s.root && !s.proxy_pass.length">—</span>
                    </td>
                    <td class="ng-mono ng-cert">{{ s.cert ? s.cert.split('/').pop() : '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="page-card ng-mapcard">
              <div class="ng-mapcard-head">Upstreams <span class="ng-count">{{ upstreams.length }}</span></div>
              <table class="ng-table">
                <thead><tr><th>name</th><th>servers</th></tr></thead>
                <tbody>
                  <tr v-if="loadingMap"><td colspan="2" class="ng-msg">Parsing…</td></tr>
                  <tr v-else-if="!upstreams.length"><td colspan="2" class="ng-msg">No upstreams.</td></tr>
                  <tr v-for="u in upstreams" :key="u.name">
                    <td class="ng-sname">{{ u.name }}</td>
                    <td class="ng-mono">{{ u.servers.join(', ') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- CERTS -->
          <div v-else-if="tab === 'certs'" class="page-card ng-sites">
            <div class="ng-sites-head">SSL certificates <span class="ng-count">{{ certs.length }}</span> · soonest expiry first</div>
            <table class="ng-table">
              <thead>
                <tr><th>Domains</th><th>Expires</th><th>Days left</th><th>Certificate</th></tr>
              </thead>
              <tbody>
                <tr v-if="loadingCerts"><td colspan="4" class="ng-msg">Reading certificates…</td></tr>
                <tr v-else-if="!certs.length"><td colspan="4" class="ng-msg">No ssl_certificate directives found.</td></tr>
                <tr v-for="(c, i) in certs" :key="i">
                  <td class="ng-mono">{{ c.domains.length ? c.domains.join(', ') : (c.subject || '—') }}</td>
                  <td class="ng-mono">{{ c.error ? '—' : c.not_after }}</td>
                  <td>
                    <span v-if="c.error" class="ng-pill ng-pill--warn">error</span>
                    <span v-else class="ng-pill" :class="c.expired ? 'ng-pill--err2' : c.days_left < 14 ? 'ng-pill--warn' : c.days_left < 30 ? 'ng-pill--off' : 'ng-pill--ok'">
                      {{ c.expired ? 'EXPIRED' : c.days_left + 'd' }}
                    </span>
                  </td>
                  <td class="ng-mono ng-cert" :title="c.error || c.path">{{ c.path }}</td>
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
              <select class="base-input ng-logsel" :value="activeLog" @change="openLog(($event.target as HTMLSelectElement).value)">
                <option v-for="f in logFiles" :key="f.path" :value="f.path">{{ f.path }} ({{ formatBytes(f.size) }})</option>
              </select>
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
.ng-empty { text-align: center; padding: 64px 20px; color: var(--text-secondary); }
.ng-empty-icon { font-size: 44px; }
.ng-empty h2 { margin: 12px 0 4px; font-size: 16px; color: var(--text-primary); }
.ng-empty p { font-size: 13px; color: var(--text-muted); }

.ng-settings { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; padding: 12px 14px; }
.ng-set { display: flex; flex-direction: column; gap: 4px; }
.ng-set--grow { flex: 1 1 220px; }
.ng-set label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); font-weight: 600; }
.ng-set .base-input { width: 100%; font-family: var(--mono); font-size: 12px; }
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
.ng-editor-empty { display: flex; align-items: center; justify-content: center; flex: 1; color: var(--text-muted); font-size: 13px; }
.ng-editor-bar { display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-bottom: 1px solid var(--border); }
.ng-editor-path { font-family: var(--mono); font-size: 12px; color: var(--text-primary); word-break: break-all; }
.ng-dirty { color: var(--warning); font-size: 11px; }
.ng-textarea { flex: 1; min-height: 420px; border: none; resize: none; padding: 12px; font-family: var(--mono); font-size: 12.5px; line-height: 1.55; background: var(--bg-base, var(--bg-surface)); color: var(--text-primary); outline: none; tab-size: 4; }

.ng-sites { padding: 4px 6px; }
.ng-sites-head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; font-size: 12px; color: var(--text-muted); }
.ng-sites-hint { font-size: 11px; color: var(--text-muted); }
.ng-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.ng-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.ng-table td { padding: 9px 12px; border-bottom: 1px solid var(--border); }
.ng-table tbody tr:last-child td { border-bottom: none; }
.ng-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.ng-sname { font-family: var(--mono); }
.ng-sfile { font-family: var(--mono); color: var(--text-muted); font-size: 12px; }
.ng-sact { text-align: right; }

.ng-logs { display: flex; flex-direction: column; gap: 10px; }
.ng-logbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ng-logsel { min-width: 240px; }
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

/* Map */
.ng-mapwrap { display: flex; flex-direction: column; gap: 14px; }
.ng-mapcard { padding: 4px 6px; }
.ng-mapcard-head { padding: 10px 12px; font-size: 12px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.04em; }
.ng-count { display: inline-block; background: var(--bg-hover); color: var(--text-muted); border-radius: 99px; padding: 1px 8px; font-size: 11px; margin-left: 4px; }
.ng-mono { font-family: var(--mono); font-size: 12px; color: var(--text-secondary); word-break: break-all; }
.ng-proxy { display: block; color: var(--brand); }
.ng-cert { color: var(--text-muted); }

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
</style>
