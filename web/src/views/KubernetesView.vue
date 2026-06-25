<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import SearchSelect from '@/components/ui/SearchSelect.vue'
import { useKubeClusters, type KubeCluster } from '@/composables/useKubeClusters'

type KubeTab = 'overview' | 'nodes' | 'namespaces' | 'pods' | 'deployments' | 'services' | 'events'

interface KNode { name: string; status: string; roles: string; version: string; os: string; cpu: string; memory: string; internal_ip: string; created: string }
interface KNamespace { name: string; status: string; created: string }
interface KPod { name: string; namespace: string; status: string; ready: string; restarts: number; node: string; pod_ip: string; created: string; containers: string[] }
interface KDeployment { name: string; namespace: string; ready: string; up_to_date: number; available: number; created: string; images: string[] }
interface KService { name: string; namespace: string; type: string; cluster_ip: string; external_ip: string; ports: string; created: string }
interface KEvent { namespace: string; type: string; reason: string; object: string; message: string; count: number; last_seen: string }

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['kube.manage']))

const { clusters, fetchClusters } = useKubeClusters()
const clusterOptions = computed(() => clusters.value.map((c) => ({ value: c.id, label: `${c.name} (${providerShort(c.provider)})` })))

const clusterId = ref<number | null>(null)
const status = ref<'unknown' | 'connecting' | 'connected' | 'error'>('unknown')
const version = ref('')
const connError = ref('')
const tab = ref<KubeTab>('overview')
const loading = ref(false)

// Namespace scope filter (empty = all namespaces)
const namespaceFilter = ref('')
const search = ref('')

// Data
const nodes = ref<KNode[]>([])
const namespaces = ref<KNamespace[]>([])
const pods = ref<KPod[]>([])
const deployments = ref<KDeployment[]>([])
const services = ref<KService[]>([])
const events = ref<KEvent[]>([])

// Pod logs modal
const showLogs = ref(false)
const logsPod = ref<KPod | null>(null)
const logsContainer = ref('')
const logsTail = ref('300')
const logsText = ref('')
const logsLoading = ref(false)

const LAST_CLUSTER_KEY = 'nias:kube:lastCluster'

function providerShort(p: string): string {
  return p === 'alibaba' ? 'ACK' : p === 'huawei' ? 'CCE' : 'k8s'
}

const nsOptions = computed(() => [
  { value: '', label: 'All namespaces' },
  ...namespaces.value.map((n) => ({ value: n.name, label: n.name })),
])

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
    // Preload namespaces for the filter, then load the active tab.
    await loadNamespaces()
    await loadTab()
  } catch (e: any) {
    status.value = 'error'
    connError.value = e?.response?.data?.error || 'Could not reach the cluster'
  }
}

function disconnect() {
  clusterId.value = null
  status.value = 'unknown'
  version.value = ''
  connError.value = ''
  localStorage.setItem(LAST_CLUSTER_KEY, 'disconnected')
  nodes.value = []; namespaces.value = []; pods.value = []
  deployments.value = []; services.value = []; events.value = []
}

const base = computed(() => `/api/kube/clusters/${clusterId.value}`)
function nsQuery(): string {
  return namespaceFilter.value ? `?namespace=${encodeURIComponent(namespaceFilter.value)}` : ''
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
      case 'services': await loadServices(); break
      case 'events': await loadEvents(); break
    }
  } catch (e: any) {
    connError.value = e?.response?.data?.error || 'Failed to load resources'
  } finally {
    loading.value = false
  }
}

async function loadNodes() { const { data } = await axios.get<KNode[]>(`${base.value}/nodes`); nodes.value = data ?? [] }
async function loadPods() { const { data } = await axios.get<KPod[]>(`${base.value}/pods${nsQuery()}`); pods.value = data ?? [] }
async function loadDeployments() { const { data } = await axios.get<KDeployment[]>(`${base.value}/deployments${nsQuery()}`); deployments.value = data ?? [] }
async function loadServices() { const { data } = await axios.get<KService[]>(`${base.value}/services${nsQuery()}`); services.value = data ?? [] }
async function loadEvents() { const { data } = await axios.get<KEvent[]>(`${base.value}/events${nsQuery()}`); events.value = data ?? [] }

function switchTab(t: KubeTab) {
  tab.value = t
  search.value = ''
  loadTab()
}

function onNamespaceChange() {
  // Re-fetch the namespace-scoped tabs.
  if (['pods', 'deployments', 'services', 'events', 'overview'].includes(tab.value)) loadTab()
}

// ── Pod logs ────────────────────────────────────────────────────
async function openLogs(p: KPod) {
  logsPod.value = p
  logsContainer.value = p.containers[0] || ''
  logsTail.value = '300'
  logsText.value = ''
  showLogs.value = true
  await fetchLogs()
}

async function fetchLogs() {
  if (!logsPod.value) return
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

// ── Filtering + formatting ──────────────────────────────────────
function matchesSearch(...fields: string[]): boolean {
  const q = search.value.trim().toLowerCase()
  if (!q) return true
  return fields.some((f) => (f || '').toLowerCase().includes(q))
}
const filteredNodes = computed(() => nodes.value.filter((n) => matchesSearch(n.name, n.roles, n.internal_ip)))
const filteredPods = computed(() => pods.value.filter((p) => matchesSearch(p.name, p.namespace, p.node, p.status)))
const filteredDeployments = computed(() => deployments.value.filter((d) => matchesSearch(d.name, d.namespace)))
const filteredServices = computed(() => services.value.filter((s) => matchesSearch(s.name, s.namespace, s.type)))
const filteredEvents = computed(() => events.value.filter((e) => matchesSearch(e.reason, e.object, e.message, e.namespace)))
const filteredNamespaces = computed(() => namespaces.value.filter((n) => matchesSearch(n.name)))

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

const overviewCounts = computed(() => ({
  nodes: nodes.value.length,
  namespaces: namespaces.value.length,
  pods: pods.value.length,
  runningPods: pods.value.filter((p) => p.status === 'Running').length,
  deployments: deployments.value.length,
  services: services.value.length,
}))

onMounted(loadClusters)
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
            <div class="page-subtitle">Browse clusters, nodes, workloads, and pod logs across Alibaba ACK and Huawei CCE.</div>
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
          <!-- Connection bar -->
          <div class="k8s-bar">
            <span class="k8s-connline">
              <span class="k8s-dot" :class="status === 'connected' ? 'k8s-dot--ok' : 'k8s-dot--err'"></span>
              <template v-if="version">Kubernetes <b>{{ version }}</b></template>
              <span v-else-if="connError" class="k8s-err">{{ connError }}</span>
              <span v-else class="k8s-muted">Connecting…</span>
            </span>
            <div class="k8s-spacer"></div>
            <SearchSelect
              v-if="namespaces.length"
              class="k8s-ns-select"
              :model-value="namespaceFilter"
              :options="nsOptions"
              placeholder="All namespaces"
              @update:model-value="namespaceFilter = String($event); onNamespaceChange()"
            />
            <input v-model="search" class="base-input k8s-search" type="search" placeholder="Filter…" />
          </div>

          <!-- Tabs -->
          <div class="k8s-tabs">
            <button v-for="t in (['overview','nodes','namespaces','pods','deployments','services','events'] as KubeTab[])"
              :key="t" class="k8s-tab" :class="{ 'k8s-tab--active': tab === t }" @click="switchTab(t)">
              {{ t.charAt(0).toUpperCase() + t.slice(1) }}
            </button>
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

            <!-- Nodes -->
            <table v-else-if="tab === 'nodes'" class="k8s-table">
              <thead><tr><th>Name</th><th>Status</th><th>Roles</th><th>Version</th><th>CPU</th><th>Memory</th><th>Internal IP</th><th>Age</th></tr></thead>
              <tbody>
                <tr v-if="!filteredNodes.length"><td colspan="8" class="k8s-msg">No nodes.</td></tr>
                <tr v-for="n in filteredNodes" :key="n.name">
                  <td class="k8s-mono">{{ n.name }}</td>
                  <td><span class="k8s-pill" :class="n.status === 'Ready' ? 'k8s-pill--ok' : 'k8s-pill--err'">{{ n.status }}</span></td>
                  <td>{{ n.roles }}</td>
                  <td class="k8s-mono">{{ n.version }}</td>
                  <td>{{ n.cpu }}</td>
                  <td>{{ n.memory }}</td>
                  <td class="k8s-mono">{{ n.internal_ip }}</td>
                  <td>{{ relAge(n.created) }}</td>
                </tr>
              </tbody>
            </table>

            <!-- Namespaces -->
            <table v-else-if="tab === 'namespaces'" class="k8s-table">
              <thead><tr><th>Name</th><th>Status</th><th>Age</th></tr></thead>
              <tbody>
                <tr v-if="!filteredNamespaces.length"><td colspan="3" class="k8s-msg">No namespaces.</td></tr>
                <tr v-for="n in filteredNamespaces" :key="n.name">
                  <td class="k8s-mono">{{ n.name }}</td>
                  <td><span class="k8s-pill" :class="n.status === 'Active' ? 'k8s-pill--ok' : 'k8s-pill--warn'">{{ n.status }}</span></td>
                  <td>{{ relAge(n.created) }}</td>
                </tr>
              </tbody>
            </table>

            <!-- Pods -->
            <table v-else-if="tab === 'pods'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Status</th><th>Ready</th><th>Restarts</th><th>Node</th><th>Pod IP</th><th>Age</th><th></th></tr></thead>
              <tbody>
                <tr v-if="!filteredPods.length"><td colspan="9" class="k8s-msg">No pods.</td></tr>
                <tr v-for="p in filteredPods" :key="p.namespace + '/' + p.name">
                  <td class="k8s-mono">{{ p.name }}</td>
                  <td>{{ p.namespace }}</td>
                  <td><span class="k8s-pill" :class="podClass(p.status)">{{ p.status }}</span></td>
                  <td>{{ p.ready }}</td>
                  <td :class="{ 'k8s-warn': p.restarts > 0 }">{{ p.restarts }}</td>
                  <td class="k8s-mono">{{ p.node }}</td>
                  <td class="k8s-mono">{{ p.pod_ip }}</td>
                  <td>{{ relAge(p.created) }}</td>
                  <td class="k8s-act"><button class="base-btn base-btn--xs" @click="openLogs(p)">Logs</button></td>
                </tr>
              </tbody>
            </table>

            <!-- Deployments -->
            <table v-else-if="tab === 'deployments'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Ready</th><th>Up-to-date</th><th>Available</th><th>Image</th><th>Age</th></tr></thead>
              <tbody>
                <tr v-if="!filteredDeployments.length"><td colspan="7" class="k8s-msg">No deployments.</td></tr>
                <tr v-for="d in filteredDeployments" :key="d.namespace + '/' + d.name">
                  <td class="k8s-mono">{{ d.name }}</td>
                  <td>{{ d.namespace }}</td>
                  <td>{{ d.ready }}</td>
                  <td>{{ d.up_to_date }}</td>
                  <td>{{ d.available }}</td>
                  <td class="k8s-mono k8s-img">{{ d.images.join(', ') }}</td>
                  <td>{{ relAge(d.created) }}</td>
                </tr>
              </tbody>
            </table>

            <!-- Services -->
            <table v-else-if="tab === 'services'" class="k8s-table">
              <thead><tr><th>Name</th><th>Namespace</th><th>Type</th><th>Cluster IP</th><th>External IP</th><th>Ports</th><th>Age</th></tr></thead>
              <tbody>
                <tr v-if="!filteredServices.length"><td colspan="7" class="k8s-msg">No services.</td></tr>
                <tr v-for="s in filteredServices" :key="s.namespace + '/' + s.name">
                  <td class="k8s-mono">{{ s.name }}</td>
                  <td>{{ s.namespace }}</td>
                  <td>{{ s.type }}</td>
                  <td class="k8s-mono">{{ s.cluster_ip }}</td>
                  <td class="k8s-mono">{{ s.external_ip }}</td>
                  <td class="k8s-mono">{{ s.ports }}</td>
                  <td>{{ relAge(s.created) }}</td>
                </tr>
              </tbody>
            </table>

            <!-- Events -->
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
        </template>

        <!-- Idle (after disconnect) -->
        <div v-else class="page-card k8s-idle">
          <div class="k8s-idle-icon">☸️</div>
          <p>Select a cluster from the dropdown above to connect.</p>
        </div>
      </div>
    </div>

    <!-- Pod logs modal -->
    <div v-if="showLogs" class="k8s-modal-backdrop" @click.self="showLogs = false">
      <div class="k8s-modal k8s-modal--wide page-card">
        <div class="k8s-modal-title">Logs — {{ logsPod?.namespace }}/{{ logsPod?.name }}</div>
        <div class="k8s-logs-bar">
          <select v-if="logsPod && logsPod.containers.length > 1" v-model="logsContainer" class="base-input k8s-logs-sel" @change="fetchLogs">
            <option v-for="c in logsPod.containers" :key="c" :value="c">{{ c }}</option>
          </select>
          <select v-model="logsTail" class="base-input k8s-logs-sel" @change="fetchLogs">
            <option value="100">100 lines</option>
            <option value="300">300 lines</option>
            <option value="1000">1000 lines</option>
            <option value="5000">5000 lines</option>
          </select>
          <button class="base-btn base-btn--sm" :disabled="logsLoading" @click="fetchLogs">{{ logsLoading ? 'Loading…' : 'Refresh' }}</button>
          <div class="k8s-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showLogs = false">Close</button>
        </div>
        <pre class="k8s-logs">{{ logsLoading ? 'Loading…' : logsText }}</pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.k8s-cluster-select { min-width: 230px; }

/* Connection badge */
.k8s-conn { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 600; }
.k8s-conn-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.k8s-conn--connected { color: var(--success); background: var(--success-bg, rgba(34,197,94,0.12)); }
.k8s-conn--error { color: var(--danger); background: var(--danger-bg, rgba(239,68,68,0.12)); }
.k8s-conn--connecting, .k8s-conn--unknown { color: var(--text-muted); background: var(--bg-hover); }
.k8s-conn--connecting .k8s-conn-dot { animation: k8s-blink 1s ease-in-out infinite; }
@keyframes k8s-blink { 0%,100% { opacity: 1; } 50% { opacity: 0.3; } }

/* Connection line */
.k8s-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.k8s-connline { display: inline-flex; align-items: center; gap: 7px; font-size: 13px; color: var(--text-secondary); }
.k8s-dot { width: 8px; height: 8px; border-radius: 50%; }
.k8s-dot--ok { background: var(--success); }
.k8s-dot--err { background: var(--danger); }
.k8s-spacer { flex: 1; }
.k8s-ns-select { min-width: 180px; }
.k8s-search { min-width: 160px; max-width: 220px; }
.k8s-muted { color: var(--text-muted); }
.k8s-err { color: var(--danger); }

/* Tabs */
.k8s-tabs { display: flex; gap: 4px; flex-wrap: wrap; border-bottom: 1px solid var(--border); }
.k8s-tab { background: none; border: none; padding: 9px 14px; font-size: 13px; color: var(--text-muted); cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -1px; }
.k8s-tab:hover { color: var(--text-secondary); }
.k8s-tab--active { color: var(--brand); border-bottom-color: var(--brand); font-weight: 600; }

/* KPI cards */
.k8s-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; }
.k8s-kpi { border: 1px solid var(--border); border-radius: var(--r); padding: 16px 18px; background: var(--bg-surface); }
.k8s-kpi-val { font-size: 28px; font-weight: 700; color: var(--text-primary); line-height: 1; }
.k8s-kpi-sub { font-size: 16px; font-weight: 400; color: var(--text-muted); }
.k8s-kpi-label { font-size: 12px; color: var(--text-muted); margin-top: 6px; text-transform: uppercase; letter-spacing: 0.04em; }

/* Tables */
.k8s-table-wrap { padding: 4px 6px; overflow-x: auto; }
.k8s-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.k8s-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; white-space: nowrap; }
.k8s-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
.k8s-table tbody tr:last-child td { border-bottom: none; }
.k8s-table tbody tr:hover { background: var(--bg-hover); }
.k8s-mono { font-family: var(--mono); font-size: 11.5px; }
.k8s-img { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.k8s-msg { text-align: center; color: var(--text-muted); padding: 24px; }
.k8s-msg-cell { max-width: 360px; color: var(--text-secondary); }
.k8s-act { text-align: right; white-space: nowrap; }
.k8s-warn { color: var(--warning); font-weight: 600; }
.k8s-conn-err { padding: 16px; color: var(--danger); font-size: 13px; }

/* Pills */
.k8s-pill { font-size: 11px; padding: 1px 8px; border-radius: 99px; font-weight: 600; }
.k8s-pill--ok { background: var(--success-bg, rgba(34,197,94,0.12)); color: var(--success); }
.k8s-pill--warn { background: var(--warning-bg, rgba(245,158,11,0.14)); color: var(--warning); }
.k8s-pill--err { background: var(--danger-bg, rgba(239,68,68,0.12)); color: var(--danger); }

/* Idle / empty */
.k8s-empty { padding: 60px 40px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 12px; }
.k8s-empty-icon { font-size: 48px; }
.k8s-empty h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.k8s-empty p { font-size: 13px; color: var(--text-muted); margin: 0; max-width: 380px; }
.k8s-idle { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 48px 20px; color: var(--text-muted); font-size: 13px; }
.k8s-idle-icon { font-size: 22px; }

/* Logs modal */
.k8s-modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.k8s-modal { padding: 20px; width: 440px; max-width: 94vw; }
.k8s-modal--wide { width: 860px; }
.k8s-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; word-break: break-all; }
.k8s-logs-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.k8s-logs-sel { width: auto; min-width: 110px; }
.k8s-logs { background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 12px; font-family: var(--mono); font-size: 11px; line-height: 1.5; max-height: 62vh; overflow: auto; white-space: pre-wrap; word-break: break-all; color: var(--text-secondary); margin: 0; }
</style>
