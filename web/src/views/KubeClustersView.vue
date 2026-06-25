<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'
import { useKubeClusters, type KubeCluster, type KubeClusterInput, type KubeSummary } from '@/composables/useKubeClusters'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['kube.manage']))

const { clusters, loading, fetchClusters, createCluster, updateCluster, deleteCluster } = useKubeClusters()

const summaryMap = ref<Map<number, KubeSummary>>(new Map())
const overviewLoading = ref(false)
const pingingId = ref<number | null>(null)

// Form
const showForm = ref(false)
const editingId = ref<number | null>(null)
const testing = ref(false)
const saving = ref(false)
const form = ref<KubeClusterInput>(emptyForm())

function emptyForm(): KubeClusterInput {
  return { name: '', provider: 'alibaba', context: '', kubeconfig: '' }
}

const providerLabels: Record<string, string> = {
  alibaba: 'Alibaba ACK',
  huawei: 'Huawei CCE',
  other: 'Other',
}

function summaryFor(c: KubeCluster): KubeSummary | undefined {
  return summaryMap.value.get(c.id)
}

async function loadOverview() {
  overviewLoading.value = true
  try {
    const { data } = await axios.get<KubeSummary[]>('/api/kube/overview')
    const m = new Map<number, KubeSummary>()
    for (const s of data ?? []) m.set(s.cluster_id, s)
    summaryMap.value = m
  } catch {
    // best-effort
  } finally {
    overviewLoading.value = false
  }
}

async function pingCluster(c: KubeCluster) {
  pingingId.value = c.id
  try {
    const { data } = await axios.get<{ version: string }>(`/api/kube/clusters/${c.id}/ping`)
    toast.success(`Reachable — Kubernetes ${data.version || ''}`)
    await loadOverview()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Cluster unreachable')
    const existing = summaryMap.value.get(c.id)
    summaryMap.value.set(c.id, {
      cluster_id: c.id, name: c.name, provider: c.provider, reachable: false,
      version: existing?.version ?? '', nodes: existing?.nodes ?? 0,
      namespaces: existing?.namespaces ?? 0, pods: existing?.pods ?? 0,
      error: e?.response?.data?.error || 'unreachable',
    })
  } finally {
    pingingId.value = null
  }
}

function openAdd() {
  editingId.value = null
  form.value = emptyForm()
  showForm.value = true
}

function openEdit(c: KubeCluster) {
  editingId.value = c.id
  form.value = { name: c.name, provider: c.provider, context: c.context, kubeconfig: '' }
  showForm.value = true
}

async function testConnection() {
  if (!form.value.kubeconfig.trim()) {
    toast.error('Paste a kubeconfig to test')
    return
  }
  testing.value = true
  try {
    const { data } = await axios.post<{ version: string }>('/api/kube/clusters/test', form.value)
    toast.success(`Connected — Kubernetes ${data.version || ''}`)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Connection test failed')
  } finally {
    testing.value = false
  }
}

async function save() {
  if (!form.value.name.trim()) {
    toast.error('Name is required')
    return
  }
  if (editingId.value === null && !form.value.kubeconfig.trim()) {
    toast.error('Kubeconfig is required')
    return
  }
  saving.value = true
  try {
    if (editingId.value === null) {
      await createCluster(form.value)
      toast.success('Cluster added')
    } else {
      await updateCluster(editingId.value, form.value)
      toast.success('Cluster updated')
    }
    showForm.value = false
    await fetchClusters()
    await loadOverview()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save cluster')
  } finally {
    saving.value = false
  }
}

async function remove(c: KubeCluster) {
  const ok = await confirm(
    `Remove "${c.name}"? This only deletes the saved connection, not the cluster itself.`,
    'Delete cluster',
  )
  if (!ok) return
  try {
    await deleteCluster(c.id)
    toast.success('Cluster removed')
    summaryMap.value.delete(c.id)
    await fetchClusters()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete cluster')
  }
}

function openCluster(c: KubeCluster) {
  router.push({ name: 'kubernetes', query: { cluster: c.id } })
}

function onKubeconfigFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => { form.value.kubeconfig = String(reader.result || '') }
  reader.readAsText(file)
  input.value = ''
}

onMounted(async () => {
  await fetchClusters()
  loadOverview()
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Kubernetes Clusters</div>
            <div class="page-subtitle">
              Manage cluster connections via kubeconfig — works with Alibaba ACK, Huawei CCE, or any standard kubeconfig.
            </div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="overviewLoading" @click="loadOverview">
              {{ overviewLoading ? 'Refreshing…' : 'Refresh status' }}
            </button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openAdd">+ Add cluster</button>
          </div>
        </section>

        <div v-if="loading" class="k8s-loading">Loading clusters…</div>

        <!-- Cluster cards -->
        <div v-else-if="clusters.length" class="k8s-grid">
          <div v-for="c in clusters" :key="c.id" class="k8s-card page-card">
            <div class="k8s-card-head">
              <div class="k8s-status-row">
                <span
                  class="k8s-dot"
                  :class="summaryFor(c) === undefined ? 'k8s-dot--unknown' : summaryFor(c)!.reachable ? 'k8s-dot--ok' : 'k8s-dot--err'"
                  :title="summaryFor(c) === undefined ? 'Status unknown' : summaryFor(c)!.reachable ? 'Reachable' : (summaryFor(c)!.error || 'Unreachable')"
                ></span>
                <span class="k8s-name">{{ c.name }}</span>
                <span class="k8s-provider" :class="`k8s-provider--${c.provider}`">{{ providerLabels[c.provider] || c.provider }}</span>
              </div>
            </div>

            <div v-if="summaryFor(c)" class="k8s-stats">
              <template v-if="summaryFor(c)!.reachable">
                <div class="k8s-stat"><span class="k8s-stat-val">{{ summaryFor(c)!.nodes }}</span><span class="k8s-stat-label">nodes</span></div>
                <div class="k8s-stat"><span class="k8s-stat-val">{{ summaryFor(c)!.namespaces }}</span><span class="k8s-stat-label">namespaces</span></div>
                <div class="k8s-stat"><span class="k8s-stat-val">{{ summaryFor(c)!.pods }}</span><span class="k8s-stat-label">pods</span></div>
                <div v-if="summaryFor(c)!.version" class="k8s-stat"><span class="k8s-stat-val k8s-ver">{{ summaryFor(c)!.version }}</span><span class="k8s-stat-label">version</span></div>
              </template>
              <div v-else class="k8s-err">{{ summaryFor(c)!.error || 'Unreachable' }}</div>
            </div>
            <div v-else class="k8s-stats k8s-stats--pending">
              <span class="k8s-muted">{{ overviewLoading ? 'Checking…' : 'Status unknown' }}</span>
            </div>

            <div class="k8s-actions">
              <button class="base-btn base-btn--primary base-btn--xs" @click="openCluster(c)">Open →</button>
              <button class="base-btn base-btn--xs" :disabled="pingingId === c.id" @click="pingCluster(c)">
                {{ pingingId === c.id ? 'Testing…' : 'Test' }}
              </button>
              <button v-if="canManage" class="base-btn base-btn--xs" @click="openEdit(c)">Edit</button>
              <button v-if="canManage" class="base-btn base-btn--danger base-btn--xs" @click="remove(c)">Delete</button>
            </div>
          </div>
        </div>

        <!-- Empty -->
        <div v-else class="page-card k8s-empty">
          <div class="k8s-empty-icon">☸️</div>
          <h2>No clusters yet</h2>
          <p>Add an Alibaba ACK, Huawei CCE, or any Kubernetes cluster by pasting its kubeconfig.</p>
          <button v-if="canManage" class="base-btn base-btn--primary" @click="openAdd">Add your first cluster</button>
        </div>
      </div>
    </div>

    <!-- Form modal -->
    <div v-if="showForm" class="k8s-modal-backdrop" @click.self="showForm = false">
      <div class="k8s-modal page-card">
        <div class="k8s-modal-title">{{ editingId === null ? 'Add Kubernetes cluster' : 'Edit cluster' }}</div>
        <div class="k8s-form">
          <div class="k8s-form-row">
            <label class="k8s-grow">Name<input v-model="form.name" class="base-input" placeholder="prod-ack-sg" /></label>
            <label class="k8s-provider-field">
              Provider
              <select v-model="form.provider" class="base-input">
                <option value="alibaba">Alibaba ACK</option>
                <option value="huawei">Huawei CCE</option>
                <option value="other">Other</option>
              </select>
            </label>
          </div>
          <label>
            Context <span class="k8s-opt">(optional — defaults to current-context)</span>
            <input v-model="form.context" class="base-input" placeholder="leave blank to use current-context" />
          </label>
          <label>
            Kubeconfig
            <span v-if="editingId !== null" class="k8s-opt">(leave blank to keep the existing one)</span>
            <textarea
              v-model="form.kubeconfig"
              class="base-input k8s-textarea"
              rows="9"
              spellcheck="false"
              :placeholder="editingId !== null ? '(unchanged)' : 'apiVersion: v1\nclusters:\n- cluster:\n    server: https://…'"
            ></textarea>
          </label>
          <label class="k8s-file">
            <span class="base-btn base-btn--sm">Upload kubeconfig file…</span>
            <input type="file" accept=".yaml,.yml,.conf,.config,.txt,text/*" hidden @change="onKubeconfigFile" />
          </label>
        </div>
        <div class="k8s-modal-actions">
          <button class="base-btn base-btn--sm" :disabled="testing" @click="testConnection">{{ testing ? 'Testing…' : 'Test connection' }}</button>
          <div class="k8s-spacer"></div>
          <button class="base-btn base-btn--sm" @click="showForm = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="save">{{ saving ? 'Saving…' : 'Save' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.k8s-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 14px; }
.k8s-card { padding: 18px 20px; display: flex; flex-direction: column; gap: 12px; }
.k8s-card-head { display: flex; flex-direction: column; gap: 4px; }
.k8s-status-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.k8s-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.k8s-dot--ok { background: var(--success); }
.k8s-dot--err { background: var(--danger); }
.k8s-dot--unknown { background: var(--text-muted); opacity: 0.5; }
.k8s-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.k8s-provider { font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; padding: 1px 7px; border-radius: 99px; background: var(--bg-hover); color: var(--text-muted); }
.k8s-provider--alibaba { background: rgba(255,103,0,0.12); color: #ff6700; }
.k8s-provider--huawei { background: rgba(220,38,38,0.12); color: #dc2626; }

.k8s-stats { display: flex; gap: 16px; align-items: flex-end; min-height: 40px; flex-wrap: wrap; }
.k8s-stats--pending { align-items: center; }
.k8s-stat { display: flex; flex-direction: column; gap: 2px; }
.k8s-stat-val { font-size: 20px; font-weight: 700; color: var(--text-primary); line-height: 1; }
.k8s-ver { font-size: 13px; font-family: var(--mono); font-weight: 400; color: var(--text-secondary); }
.k8s-stat-label { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; }
.k8s-err { font-size: 12px; color: var(--danger); word-break: break-word; }
.k8s-muted { font-size: 12px; color: var(--text-muted); }

.k8s-actions { display: flex; gap: 6px; flex-wrap: wrap; padding-top: 4px; border-top: 1px solid var(--border); }

.k8s-loading { padding: 40px; text-align: center; color: var(--text-muted); font-size: 14px; }
.k8s-empty { padding: 60px 40px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 12px; }
.k8s-empty-icon { font-size: 48px; }
.k8s-empty h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.k8s-empty p { font-size: 13px; color: var(--text-muted); margin: 0; max-width: 380px; }

.k8s-modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.k8s-modal { padding: 22px; width: 560px; max-width: 94vw; }
.k8s-modal-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 14px; }
.k8s-form { display: flex; flex-direction: column; gap: 10px; }
.k8s-form label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-secondary); }
.k8s-form-row { display: flex; gap: 10px; }
.k8s-grow { flex: 1; }
.k8s-provider-field { width: 150px; }
.k8s-opt { color: var(--text-muted); font-weight: 400; }
.k8s-textarea { font-family: var(--mono); font-size: 11px; resize: vertical; }
.k8s-file { flex-direction: row !important; align-items: center; cursor: pointer; }
.k8s-modal-actions { display: flex; gap: 8px; margin-top: 16px; align-items: center; }
.k8s-spacer { flex: 1; }
</style>
