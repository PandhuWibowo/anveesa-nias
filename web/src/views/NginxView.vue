<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'

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
const tab = ref<'config' | 'sites' | 'logs'>('config')

// Per-host detected/overridable settings
const bin = ref('nginx')
const configRoot = ref('/etc/nginx')
const logDir = ref('/var/log/nginx')
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
let es: EventSource | null = null

const base = computed(() => `/api/nginx/hosts/${hostId.value}`)
// Shared query params so every call respects the (possibly overridden) host settings.
const cfgParams = computed(() => ({ root: configRoot.value, bin: bin.value }))
const logParams = computed(() => ({ dir: logDir.value, bin: bin.value }))

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
  cmdOutput.value = ''
  activeFile.value = ''
  fileContent.value = ''
  origContent.value = ''
  activeLog.value = ''
  logLines.value = []
  await detect()
  await loadCurrentTab()
}

// Probe the host for binary, paths, and sites layout.
async function detect() {
  if (hostId.value === null) return
  detecting.value = true
  try {
    const { data } = await axios.get<NginxInfo>(`${base.value}/info`)
    bin.value = data.bin || 'nginx'
    configRoot.value = data.config_root || '/etc/nginx'
    logDir.value = data.log_root || '/var/log/nginx'
    sitesLayout.value = data.sites_layout || ''
    version.value = data.version || ''
    active.value = data.active || ''
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Detection failed — using defaults')
  } finally {
    detecting.value = false
  }
}

// Re-scan after the user edits paths manually.
async function rescan() {
  await detect()
  await loadCurrentTab()
  toast.success('Rescanned host')
}

async function loadCurrentTab() {
  if (tab.value === 'config') await loadTree()
  else if (tab.value === 'sites') await loadSites()
  else await loadLogList()
}

function switchTab(t: 'config' | 'sites' | 'logs') {
  if (t === tab.value) return
  if (t !== 'logs') stopFollow()
  tab.value = t
  loadCurrentTab()
}

// ── Test / reload ───────────────────────────────────────────────
async function testConfig() {
  if (hostId.value === null) return
  busy.value = true
  try {
    const { data } = await axios.post(`${base.value}/test`, null, { params: { bin: bin.value } })
    cmdOk.value = !!data.ok
    cmdOutput.value = data.output || (data.ok ? 'Configuration OK' : 'Test failed')
  } catch (e: any) {
    cmdOk.value = false
    cmdOutput.value = e?.response?.data?.error || `${bin.value} -t failed`
  } finally {
    busy.value = false
  }
}

async function reload() {
  if (hostId.value === null) return
  const ok = await confirm('Reload nginx on this host? Active connections are preserved.', 'Reload')
  if (!ok) return
  busy.value = true
  try {
    const { data } = await axios.post(`${base.value}/reload`, null, { params: { bin: bin.value } })
    cmdOk.value = true
    cmdOutput.value = data.output?.trim() || 'Reloaded.'
    toast.success(`${bin.value} reloaded`)
    await detect()
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
  try {
    const { data } = await axios.get(`${base.value}/config/file`, { params: { ...cfgParams.value, path: p } })
    fileContent.value = data.content || ''
    origContent.value = fileContent.value
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to read file')
    fileContent.value = ''
    origContent.value = ''
  } finally {
    loadingFile.value = false
  }
}

async function saveFile() {
  if (!activeFile.value) return
  busy.value = true
  try {
    await axios.post(`${base.value}/config/file`, {
      root: configRoot.value,
      path: activeFile.value,
      content: fileContent.value,
    })
    origContent.value = fileContent.value
    toast.success('Saved — run Test config before reloading')
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
      params: { root: configRoot.value, layout: sitesLayout.value },
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

async function openLog(file: string) {
  stopFollow()
  activeLog.value = file
  logLines.value = []
  try {
    const { data } = await axios.get(`${base.value}/logs/tail`, { params: { ...logParams.value, file, lines: 300 } })
    logLines.value = (data.output || '').split('\n').filter((l: string) => l.length)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to tail log')
  }
}

function toggleFollow() {
  if (following.value) {
    stopFollow()
    return
  }
  if (!activeLog.value) return
  const token = localStorage.getItem('nias-token') || ''
  const qs = new URLSearchParams({ dir: logDir.value, file: activeLog.value, token })
  logLines.value = []
  es = new EventSource(`${base.value}/logs/stream?${qs.toString()}`)
  following.value = true
  es.onmessage = (ev) => {
    try {
      const { line } = JSON.parse(ev.data)
      if (typeof line === 'string') {
        logLines.value.push(line)
        if (logLines.value.length > 2000) logLines.value.splice(0, logLines.value.length - 2000)
      }
    } catch {
      /* ignore non-JSON keepalives */
    }
  }
  es.onerror = () => {
    stopFollow()
    toast.error('Live tail disconnected')
  }
}

function stopFollow() {
  if (es) {
    es.close()
    es = null
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
onBeforeUnmount(stopFollow)
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
            <button v-if="hostId !== null && canReload" class="base-btn base-btn--primary base-btn--sm" :disabled="busy" @click="reload">Reload</button>
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
            <button class="base-btn base-btn--sm" :disabled="detecting" @click="rescan">{{ detecting ? 'Scanning…' : 'Rescan' }}</button>
          </div>

          <div v-if="version || active || cmdOutput" class="ng-statusbar">
            <span v-if="active" class="ng-pill" :class="active === 'active' ? 'ng-pill--ok' : 'ng-pill--warn'">{{ active }}</span>
            <span v-if="version" class="ng-version">{{ version }}</span>
            <pre v-if="cmdOutput" class="ng-cmdout" :class="{ 'ng-cmdout--err': !cmdOk }">{{ cmdOutput }}</pre>
          </div>

          <div class="ng-tabs">
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'config' }" @click="switchTab('config')">Config</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'sites' }" @click="switchTab('sites')">Sites</button>
            <button class="ng-tab" :class="{ 'ng-tab--active': tab === 'logs' }" @click="switchTab('logs')">Logs</button>
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
                  <button
                    v-if="canManage"
                    class="base-btn base-btn--primary base-btn--sm"
                    :disabled="busy || !dirty"
                    @click="saveFile"
                  >Save</button>
                </div>
                <textarea
                  v-model="fileContent"
                  class="ng-textarea"
                  spellcheck="false"
                  :readonly="!canManage || loadingFile"
                  :placeholder="loadingFile ? 'Loading…' : ''"
                ></textarea>
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

          <!-- LOGS -->
          <div v-else class="page-card ng-logs">
            <div class="ng-logbar">
              <select class="base-input ng-logsel" :value="activeLog" @change="openLog(($event.target as HTMLSelectElement).value)">
                <option v-for="f in logFiles" :key="f.path" :value="f.path">{{ f.path }} ({{ formatBytes(f.size) }})</option>
              </select>
              <button
                class="base-btn base-btn--sm"
                :class="following ? 'base-btn--danger' : 'base-btn--primary'"
                :disabled="!activeLog"
                @click="toggleFollow"
              >{{ following ? '■ Stop' : '▶ Follow' }}</button>
              <div class="dk-spacer"></div>
              <span class="ng-logmeta">{{ logLines.length }} lines{{ following ? ' · live' : '' }}</span>
            </div>
            <pre class="ng-logview">{{ logLines.join('\n') || 'No log output.' }}</pre>
          </div>
        </template>
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
</style>
