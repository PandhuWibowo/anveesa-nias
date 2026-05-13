<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import {
  OBS_PRESETS, SERVICE_HEALTH_DEFAULTS,
  useObsSettings, fetchIndices, suggestFields,
  type ServiceHealthSettings,
} from '@/composables/useObsSettings'
import IndexPicker from '@/components/ui/IndexPicker.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit  = defineEmits<{ (e: 'set-conn', id: number): void }>()

// ── Color palette ─────────────────────────────────────────────────────────────
const COLOR_PALETTE = [
  '#00bfb3','#6366f1','#f59e0b','#10b981','#ef4444',
  '#3b82f6','#d946ef','#14b8a6','#f97316','#84cc16',
  '#8b5cf6','#06b6d4','#ec4899','#a3e635','#fb923c',
]

// ── Types ─────────────────────────────────────────────────────────────────────
interface ServiceDef  { name: string; color: string }
interface ServiceCard { name: string; color: string; total: number; errors: number; errorRate: number; status: 'healthy'|'degraded'|'critical'|'unknown' }
interface TimelineBucket { key: number; key_as_string: string; doc_count: number; services: Record<string,number>; errorCount: number }
interface HostMetric  { host: string; cpu: number; mem: number }
interface AlertEntry  { rule: string; ts: string; matches: number; host?: string }
type TimeRange   = '1h'|'6h'|'24h'|'7d'
type AutoRefresh = 0|30|60|300

// ── Connections ───────────────────────────────────────────────────────────────
const { connections, fetchConnections } = useConnections()
const searchConns = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn  = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch    = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

// ── Persistent settings ───────────────────────────────────────────────────────
const { settings, reset: resetSettings } = useObsSettings<ServiceHealthSettings>('service-health', SERVICE_HEALTH_DEFAULTS)
const configured = computed(() => !!settings.value.logIndex.trim())

// ── Setup wizard state ────────────────────────────────────────────────────────
type WizardStep = 'preset' | 'configure' | 'done'
const wizardStep    = ref<WizardStep>('preset')
const showWizard    = ref(false)
const wizardForm    = ref<ServiceHealthSettings>({ ...settings.value })
const detectLoading = ref(false)
const detectError   = ref('')

function openWizard() {
  wizardForm.value = { ...settings.value }
  wizardStep.value = configured.value ? 'configure' : 'preset'
  showWizard.value = true
  detectError.value = ''
}

function pickPreset(id: string) {
  const p = OBS_PRESETS.find(x => x.id === id)!
  wizardForm.value = {
    ...wizardForm.value,
    presetId:      p.id,
    logIndex:      p.logIndex,
    serviceField:  p.serviceField,
    envField:      p.envField,
    errorKeywords: p.errorKeywords,
    metricIndex:   p.metricIndex,
    cpuField:      p.cpuField,
    memField:      p.memField,
  }
  wizardStep.value = 'configure'
}

async function autoDetectFields() {
  if (!activeConn.value || !wizardForm.value.logIndex) return
  detectLoading.value = true
  detectError.value = ''
  try {
    const { data } = await axios.get(
      `/api/connections/${activeConn.value.id}/search/fields`,
      { params: { index: wizardForm.value.logIndex } },
    )
    const flat: string[] = (data as any[]).map((f: any) => f.name)
    const suggested = suggestFields(flat)
    if (!wizardForm.value.serviceField) wizardForm.value.serviceField = suggested.serviceField
    if (!wizardForm.value.envField)     wizardForm.value.envField     = suggested.envField
  } catch {
    detectError.value = 'Could not auto-detect fields. Enter them manually below.'
  } finally {
    detectLoading.value = false
  }
}

function applyWizard() {
  settings.value = { ...wizardForm.value }
  showWizard.value = false
  resetAll()
  loadAll()
}

// ── Runtime state ─────────────────────────────────────────────────────────────
const timeRange    = ref<TimeRange>('1h')
const autoRefresh  = ref<AutoRefresh>(0)
const loading      = ref(false)
const discovering  = ref(false)
const lastUpdated  = ref<Date | null>(null)
const refreshTimer = ref<ReturnType<typeof setInterval> | null>(null)

const services     = ref<ServiceDef[]>([])
const serviceCards = ref<ServiceCard[]>([])
const timeline     = ref<TimelineBucket[]>([])
const infra        = ref<HostMetric[]>([])
const alerts       = ref<AlertEntry[]>([])
const alertSummary = ref<{ rule: string; count: number }[]>([])
const availableEnvs= ref<string[]>([])
const activeEnv    = ref('')

const overallStatus = computed<'healthy'|'degraded'|'critical'|'unknown'>(() => {
  if (!serviceCards.value.length) return 'unknown'
  if (serviceCards.value.some(s => s.status === 'critical')) return 'critical'
  if (serviceCards.value.some(s => s.status === 'degraded')) return 'degraded'
  return 'healthy'
})
const errorKeywordList  = computed(() => settings.value.errorKeywords.split(',').map(k=>k.trim()).filter(Boolean))
const timelineIntervals: Record<TimeRange,string> = { '1h':'5m','6h':'30m','24h':'1h','7d':'6h' }
const timeRangeLabel:   Record<TimeRange,string>  = { '1h':'Last 1h','6h':'Last 6h','24h':'Last 24h','7d':'Last 7d' }
const timelineMax = computed(() => Math.max(...timeline.value.map(b=>b.doc_count), 1))

// ── Lifecycle ─────────────────────────────────────────────────────────────────
onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConns.value.length === 1) { emit('set-conn', searchConns.value[0].id); return }
  if (isSearch.value && configured.value) await loadAll()
})
watch(() => props.activeConnId, () => { resetAll(); if (isSearch.value && configured.value) loadAll() })
watch(timeRange, () => { if (isSearch.value && configured.value) loadAll() })
watch(activeEnv, () => { if (isSearch.value && configured.value) loadAll() })
watch(autoRefresh, (n) => { clearTimer(); if (n>0) refreshTimer.value = setInterval(loadAll, n*1000) })
onBeforeUnmount(clearTimer)

function clearTimer() { if (refreshTimer.value) { clearInterval(refreshTimer.value); refreshTimer.value=null } }
function resetAll() {
  services.value=[]; serviceCards.value=[]; timeline.value=[]
  infra.value=[]; alerts.value=[]; alertSummary.value=[]
  availableEnvs.value=[]; activeEnv.value=''
}

// ── API ───────────────────────────────────────────────────────────────────────
async function agg(index: string, query: any, aggs: any, size=0) {
  const { data } = await axios.post(`/api/connections/${activeConn.value!.id}/search/aggregate`, {
    index, query, aggs, size,
  })
  return data
}

function buildQ(gte: string) {
  const must: any[] = [{ range:{ '@timestamp':{ gte } } }]
  if (activeEnv.value && settings.value.envField)
    must.push({ term:{ [settings.value.envField]: activeEnv.value } })
  return { bool:{ must } }
}

function buildErrorFilter() {
  return { bool:{ should: errorKeywordList.value.map(kw=>({ match_phrase:{ message: kw } })), minimum_should_match:1 } }
}

// ── Loaders ───────────────────────────────────────────────────────────────────
async function loadAll() {
  if (!activeConn.value || !configured.value) return
  loading.value = true
  try {
    await discoverEnvironments()
    await discoverServices()
    await Promise.all([
      loadServiceHealth(),
      loadTimeline(),
      settings.value.metricIndex ? loadInfra()  : Promise.resolve(),
      settings.value.alertIndex  ? loadAlerts() : Promise.resolve(),
    ])
    lastUpdated.value = new Date()
  } finally { loading.value = false }
}

async function discoverEnvironments() {
  if (!settings.value.envField) return
  const d = await agg(settings.value.logIndex,
    { range:{ '@timestamp':{ gte:'now-24h' } } },
    { envs:{ terms:{ field: settings.value.envField, size:20 } } },
  ).catch(()=>null)
  const buckets: { key:string }[] = d?.aggregations?.envs?.buckets ?? []
  availableEnvs.value = buckets.map(b=>b.key)
  if (availableEnvs.value.length && !activeEnv.value)
    activeEnv.value = availableEnvs.value[0]
}

async function discoverServices() {
  if (!settings.value.logIndex) return
  discovering.value = true
  try {
    const d = await agg(settings.value.logIndex, buildQ(`now-${timeRange.value}`),
      { services:{ terms:{ field: settings.value.serviceField, size: settings.value.serviceLimit } } },
    ).catch(()=>null)
    const buckets: { key:string }[] = d?.aggregations?.services?.buckets ?? []
    services.value = buckets.map((b,i) => ({ name:b.key, color: COLOR_PALETTE[i%COLOR_PALETTE.length] }))
  } finally { discovering.value = false }
}

async function loadServiceHealth() {
  if (!services.value.length) return
  const d = await agg(settings.value.logIndex, buildQ(`now-${timeRange.value}`), {
    by_service:{ terms:{ field: settings.value.serviceField, size: settings.value.serviceLimit }, aggs:{ errors:{ filter: buildErrorFilter() } } },
  }).catch(()=>null)
  const byName: Record<string,any> = {}
  for (const b of d?.aggregations?.by_service?.buckets ?? []) byName[b.key]=b
  serviceCards.value = services.value.map(svc => {
    const b=byName[svc.name]; const total=b?.doc_count??0; const errors=b?.errors?.doc_count??0
    const errorRate=total>0?(errors/total)*100:0
    const status=total===0?'unknown':errorRate>5?'critical':errorRate>1?'degraded':'healthy'
    return { ...svc, total, errors, errorRate, status }
  })
}

async function loadTimeline() {
  const d = await agg(settings.value.logIndex, buildQ(`now-${timeRange.value}`), {
    over_time:{ date_histogram:{ field:'@timestamp', fixed_interval: timelineIntervals[timeRange.value], min_doc_count:0 },
      aggs:{ by_service:{ terms:{ field: settings.value.serviceField, size: settings.value.serviceLimit } }, errors:{ filter: buildErrorFilter() } } },
  }).catch(()=>null)
  timeline.value = (d?.aggregations?.over_time?.buckets ?? []).map((b:any) => {
    const svcMap: Record<string,number>={}
    for (const sb of b.by_service?.buckets??[]) svcMap[sb.key]=sb.doc_count
    return { key:b.key, key_as_string:b.key_as_string, doc_count:b.doc_count, errorCount:b.errors?.doc_count??0, services:svcMap }
  })
}

async function loadInfra() {
  const tq=(m:string)=>({ bool:{ filter:[{ range:{ '@timestamp':{ gte:'now-5m' } } },{ term:{ 'metricset.name':m } }] } })
  const [cd,md] = await Promise.all([
    agg(settings.value.metricIndex, tq('cpu'),    { by_host:{ terms:{ field:'host.name',size:30 }, aggs:{ v:{ avg:{ field: settings.value.cpuField } } } } }).catch(()=>null),
    agg(settings.value.metricIndex, tq('memory'), { by_host:{ terms:{ field:'host.name',size:30 }, aggs:{ v:{ avg:{ field: settings.value.memField } } } } }).catch(()=>null),
  ])
  const memMap: Record<string,number>={}
  for (const b of md?.aggregations?.by_host?.buckets??[]) memMap[b.key]=b.v?.value??0
  infra.value=(cd?.aggregations?.by_host?.buckets??[])
    .map((b:any)=>({ host:b.key, cpu:(b.v?.value??0)*100, mem:(memMap[b.key]??0)*100 }))
    .sort((a:HostMetric,b:HostMetric)=>b.cpu-a.cpu)
}

async function loadAlerts() {
  const [rd,sd]=await Promise.all([
    agg(settings.value.alertIndex, { range:{ '@timestamp':{ gte:'now-24h' } } }, {}, 20).catch(()=>null),
    agg(settings.value.alertIndex, { range:{ '@timestamp':{ gte:`now-${timeRange.value}` } } },
      { by_rule:{ terms:{ field: settings.value.alertRuleField, size:15 }, aggs:{ total_matches:{ sum:{ field:'matches' } } } } }).catch(()=>null),
  ])
  alerts.value=(rd?.hits?.hits??[])
    .map((h:any)=>({ rule:h._source?.[settings.value.alertRuleField]??'-', ts:h._source?.['@timestamp']??'', matches:h._source?.matches??0, host:getField(h._source, settings.value.alertHostField) }))
    .sort((a:AlertEntry,b:AlertEntry)=>new Date(b.ts).getTime()-new Date(a.ts).getTime())
  alertSummary.value=(sd?.aggregations?.by_rule?.buckets??[])
    .map((b:any)=>({ rule:b.key, count: Math.round(b.total_matches?.value??b.doc_count) }))
    .filter((r:{count:number})=>r.count>0)
}

function getField(obj:any,path:string):string|undefined { return path.split('.').reduce((o,k)=>o?.[k],obj) }

// ── Display helpers ───────────────────────────────────────────────────────────
function statusClass(s:string) { return { 'sh-healthy':s==='healthy','sh-degraded':s==='degraded','sh-critical':s==='critical' } }
function statusDot(s:string)   { return { 'dot-green':s==='healthy','dot-yellow':s==='degraded','dot-red':s==='critical','dot-gray':s==='unknown' } }
function pctClass(v:number)    { return { 'pct-ok':v<75,'pct-warn':v>=75&&v<90,'pct-crit':v>=90 } }
function fmt(n:number)  { return n.toLocaleString() }
function pct(n:number)  { return n.toFixed(1)+'%' }
function barH(b:TimelineBucket) { return Math.max(2,Math.round((b.doc_count/timelineMax.value)*100)) }
function errPct(b:TimelineBucket) { return b.doc_count?Math.round((b.errorCount/b.doc_count)*100):0 }
function svcPct(b:TimelineBucket,n:string) { return b.doc_count?Math.round(((b.services[n]??0)/b.doc_count)*100):0 }
function barTime(b:TimelineBucket) { const d=new Date(b.key); return isNaN(d.getTime())?'':d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'}) }
function formatTs(v:string) { if(!v)return'-'; const d=new Date(v); return isNaN(d.getTime())?v:d.toLocaleString() }
function alertRuleClass(rule:string) {
  const r=rule.toLowerCase()
  if(r.includes('down')||r.includes('unavailable'))return'alert-down'
  if(r.includes('cpu'))return'alert-cpu'
  if(r.includes('memory')||r.includes('mem'))return'alert-mem'
  if(r.includes('disk'))return'alert-disk'
  return'alert-other'
}
</script>

<template>
  <div class="sh-root page-shell">

    <!-- ── Header ────────────────────────────────────────── -->
    <header class="sh-topbar">
      <div class="sh-title">
        <span class="sh-logo" :class="statusDot(overallStatus)" />
        <div>
          <h1>Service Health</h1>
          <p>{{ activeConn ? activeConn.name : 'No connection selected' }}</p>
        </div>
      </div>
      <div class="sh-controls">
        <select class="base-input sh-conn-sel" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Select cluster</option>
          <option v-for="c in searchConns" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>

        <!-- Env quick-filter -->
        <div v-if="availableEnvs.length" class="sh-env-bar">
          <button class="sh-env-btn" :class="{ active: activeEnv === '' }" @click="activeEnv = ''">All</button>
          <button v-for="env in availableEnvs" :key="env" class="sh-env-btn" :class="{ active: activeEnv === env }" @click="activeEnv = env">{{ env }}</button>
        </div>

        <div class="sh-seg">
          <button v-for="tr in (['1h','6h','24h','7d'] as const)" :key="tr" :class="{ active: timeRange === tr }" @click="timeRange = tr">{{ tr }}</button>
        </div>
        <select v-model.number="autoRefresh" class="base-input sh-refresh-sel">
          <option :value="0">Auto-refresh off</option>
          <option :value="30">Every 30s</option>
          <option :value="60">Every 1m</option>
          <option :value="300">Every 5m</option>
        </select>
        <span v-if="autoRefresh > 0" class="sh-live-dot" />
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || !configured || loading" @click="loadAll">
          {{ loading ? '…' : 'Refresh' }}
        </button>
        <button class="base-btn base-btn--primary base-btn--sm" @click="openWizard">
          {{ configured ? '⚙ Settings' : '⚙ Setup' }}
        </button>
      </div>
    </header>

    <p v-if="lastUpdated" class="sh-last-updated">
      Updated {{ lastUpdated.toLocaleTimeString() }}
      <span v-if="settings.presetId" class="sh-preset-badge">{{ OBS_PRESETS.find(p=>p.id===settings.presetId)?.label ?? settings.presetId }}</span>
    </p>

    <section v-if="!isSearch" class="sh-empty">
      <h2>Select an Elasticsearch / OpenSearch connection</h2>
      <p>Use the dropdown above to connect to a cluster.</p>
    </section>

    <section v-else-if="!configured" class="sh-empty sh-onboard">
      <div class="sh-onboard-icon">📊</div>
      <h2>Welcome to Service Health</h2>
      <p>Monitor log error rates, infrastructure health, and alerts across all your services — in one dashboard.</p>
      <p class="sh-onboard-sub">To get started, tell us where your logs live.</p>
      <button class="base-btn base-btn--primary" @click="openWizard">Get Started →</button>
    </section>

    <template v-else>
      <!-- ── Service Cards ──────────────────────────────── -->
      <div class="sh-cards" :style="{ '--card-cols': Math.min(serviceCards.length || 2, 6) }">
        <div v-for="svc in serviceCards" :key="svc.name" class="sh-card" :class="statusClass(svc.status)" :style="{ '--svc-color': svc.color }">
          <div class="sh-card-head">
            <span class="sh-card-dot" :class="statusDot(svc.status)" />
            <span class="sh-card-label" :title="svc.name">{{ svc.name }}</span>
            <span class="sh-card-status">{{ svc.status }}</span>
          </div>
          <div class="sh-card-stats">
            <div class="sh-card-stat"><span>Logs</span><strong>{{ fmt(svc.total) }}</strong></div>
            <div class="sh-card-stat"><span>Errors</span><strong :class="svc.errors > 0 ? 'text-danger' : ''">{{ fmt(svc.errors) }}</strong></div>
            <div class="sh-card-stat"><span>Error Rate</span><strong :class="svc.errorRate > 5 ? 'text-danger' : svc.errorRate > 1 ? 'text-warn' : ''">{{ pct(svc.errorRate) }}</strong></div>
          </div>
          <div class="sh-card-bar-bg"><div class="sh-card-bar-fill" :style="{ width: `${Math.min(100, svc.errorRate * 10)}%`, background: svc.color }" /></div>
        </div>
        <div v-if="!serviceCards.length && (loading || discovering)" v-for="i in 4" :key="'sk'+i" class="sh-card sh-card-skeleton"><div class="sh-card-head"><span class="sh-card-label">Discovering…</span></div></div>
        <div v-if="!serviceCards.length && !loading && !discovering" class="sh-card sh-card-empty">
          <p>No services found in <code>{{ settings.logIndex }}</code>. Try a wider time range or <button class="sh-inline-btn" @click="openWizard">adjust settings</button>.</p>
        </div>
      </div>

      <!-- ── Legend ─────────────────────────────────────── -->
      <div v-if="services.length" class="sh-legend-bar">
        <span v-for="svc in services" :key="'leg'+svc.name" class="sh-leg-item">
          <span class="sh-leg-dot" :style="{ background: svc.color }" />{{ svc.name }}
        </span>
        <span class="sh-leg-item"><span class="sh-leg-dot sh-leg-error" />Errors</span>
      </div>

      <!-- ── Main grid ──────────────────────────────────── -->
      <div class="sh-main" :class="{ 'sh-main-full': !settings.metricIndex && !settings.alertIndex }">
        <div class="sh-panel sh-panel-timeline">
          <div class="sh-panel-head">
            <span class="sh-panel-title">
              Log Volume &amp; Errors — {{ timeRangeLabel[timeRange] }}
              <span v-if="activeEnv" class="sh-env-badge">{{ activeEnv }}</span>
            </span>
            <span class="sh-muted">by <code>{{ settings.serviceField }}</code></span>
          </div>
          <div v-if="timeline.length" class="sh-timeline">
            <div class="sh-tl-bars">
              <div v-for="b in timeline" :key="b.key" class="sh-tl-col" :title="`${barTime(b)}: ${fmt(b.doc_count)} logs, ${fmt(b.errorCount)} errors`">
                <div class="sh-tl-bar-wrap" :style="{ height: `${barH(b)}%` }">
                  <div v-for="svc in services" :key="svc.name" class="sh-tl-seg" :style="{ height: `${svcPct(b, svc.name)}%`, background: svc.color }" />
                  <div v-if="b.errorCount > 0" class="sh-tl-error-overlay" :style="{ height: `${errPct(b)}%` }" />
                </div>
              </div>
            </div>
            <div class="sh-tl-labels">
              <span>{{ timeline.length ? barTime(timeline[0]) : '' }}</span>
              <span>{{ timeline.length > 2 ? barTime(timeline[Math.floor(timeline.length/2)]) : '' }}</span>
              <span>{{ timeline.length ? barTime(timeline[timeline.length-1]) : '' }}</span>
            </div>
          </div>
          <div v-else-if="!loading" class="sh-panel-empty">No log data found for this time range.</div>
        </div>

        <div v-if="settings.metricIndex || settings.alertIndex" class="sh-side">
          <div v-if="settings.metricIndex" class="sh-panel">
            <div class="sh-panel-head"><span class="sh-panel-title">Infrastructure — Last 5 min</span></div>
            <div v-if="infra.length" class="sh-infra-list">
              <div v-for="node in infra" :key="node.host" class="sh-infra-row">
                <div class="sh-infra-host">{{ node.host }}</div>
                <div class="sh-infra-metrics">
                  <div class="sh-infra-metric"><span>CPU</span><div class="sh-pct-bar"><div class="sh-pct-fill" :class="pctClass(node.cpu)" :style="{ width: `${Math.min(100,node.cpu)}%` }" /></div><span class="sh-pct-val" :class="pctClass(node.cpu)">{{ node.cpu.toFixed(0) }}%</span></div>
                  <div class="sh-infra-metric"><span>MEM</span><div class="sh-pct-bar"><div class="sh-pct-fill" :class="pctClass(node.mem)" :style="{ width: `${Math.min(100,node.mem)}%` }" /></div><span class="sh-pct-val" :class="pctClass(node.mem)">{{ node.mem.toFixed(0) }}%</span></div>
                </div>
              </div>
            </div>
            <div v-else class="sh-panel-empty">No infrastructure data in the last 5 min.</div>
          </div>
          <div v-if="settings.alertIndex" class="sh-panel">
            <div class="sh-panel-head"><span class="sh-panel-title">Alert Summary — {{ timeRangeLabel[timeRange] }}</span></div>
            <div v-if="alertSummary.length" class="sh-alert-summary">
              <div v-for="a in alertSummary" :key="a.rule" class="sh-alert-sum-row" :class="alertRuleClass(a.rule)">
                <span class="sh-alert-rule">{{ a.rule }}</span><span class="sh-alert-count">{{ fmt(a.count) }}</span>
              </div>
            </div>
            <div v-else class="sh-panel-empty">No alerts in this period.</div>
          </div>
        </div>
      </div>

      <div v-if="settings.alertIndex" class="sh-panel">
        <div class="sh-panel-head"><span class="sh-panel-title">Recent Alert Events</span><span class="sh-muted">{{ settings.alertIndex }} · {{ alerts.length }} records</span></div>
        <div v-if="alerts.length" class="sh-alert-table-wrap">
          <table class="sh-alert-table">
            <thead><tr><th>Rule</th><th>Host</th><th>Matches</th><th>Time</th></tr></thead>
            <tbody>
              <tr v-for="(a,i) in alerts" :key="i" :class="a.matches > 0 ? 'sh-alert-row-hit' : ''">
                <td><span class="sh-rule-badge" :class="alertRuleClass(a.rule)">{{ a.rule }}</span></td>
                <td class="sh-mono sh-muted">{{ a.host || '-' }}</td>
                <td class="sh-center" :class="a.matches > 0 ? 'text-danger' : 'sh-muted'">{{ a.matches }}</td>
                <td class="sh-muted sh-mono">{{ formatTs(a.ts) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="sh-panel-empty">No alert events found.</div>
      </div>
    </template>

    <!-- ══════════════ Setup Wizard Modal ════════════════ -->
    <Teleport to="body">
      <div v-if="showWizard" class="wz-overlay" @click.self="showWizard = false">
        <div class="wz-modal">

          <!-- Wizard header -->
          <div class="wz-head">
            <div class="wz-head-left">
              <span class="wz-icon">📊</span>
              <div>
                <div class="wz-title">Service Health Setup</div>
                <div class="wz-subtitle">
                  <span :class="{ 'wz-step-active': wizardStep === 'preset' }">1. Data source</span>
                  <span class="wz-step-sep">›</span>
                  <span :class="{ 'wz-step-active': wizardStep === 'configure' }">2. Configure</span>
                </div>
              </div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showWizard = false">✕</button>
          </div>

          <!-- Step 1: Preset picker -->
          <div v-if="wizardStep === 'preset'" class="wz-body">
            <p class="wz-desc">Choose how your logs are shipped. This pre-fills the right field names for you.</p>
            <div class="wz-presets">
              <button
                v-for="p in OBS_PRESETS"
                :key="p.id"
                class="wz-preset-card"
                :class="{ 'wz-preset-selected': wizardForm.presetId === p.id }"
                @click="pickPreset(p.id)"
              >
                <span class="wz-preset-icon">{{ p.icon }}</span>
                <span class="wz-preset-label">{{ p.label }}</span>
                <span class="wz-preset-desc">{{ p.desc }}</span>
                <span v-if="wizardForm.presetId === p.id" class="wz-preset-check">✓</span>
              </button>
            </div>
          </div>

          <!-- Step 2: Configure -->
          <div v-if="wizardStep === 'configure'" class="wz-body">

            <!-- Log source -->
            <div class="wz-section">
              <div class="wz-section-title">
                <span class="wz-section-num">1</span>
                Log Source <span class="wz-required">*</span>
              </div>
              <div class="wz-fields-grid-2">
                <label class="wz-lbl">
                  Index Pattern
                  <span class="wz-hint">Supports wildcards (e.g. <code>.ds-filebeat-*</code>)</span>
                  <IndexPicker
                    :conn-id="activeConnId"
                    v-model="wizardForm.logIndex"
                    placeholder="e.g. .ds-filebeat-*, k8s-logs-*"
                  />
                </label>
                <label class="wz-lbl">
                  Service Field
                  <span class="wz-hint">The field that identifies each service</span>
                  <div class="wz-field-row">
                    <input v-model="wizardForm.serviceField" class="base-input wz-input" placeholder="e.g. app_name" />
                    <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!wizardForm.logIndex || detectLoading" @click="autoDetectFields" title="Auto-detect from mapping">
                      {{ detectLoading ? '…' : '⚡ Detect' }}
                    </button>
                  </div>
                  <span v-if="detectError" class="wz-detect-error">{{ detectError }}</span>
                </label>
                <label class="wz-lbl">
                  Environment Field
                  <span class="wz-hint">Enables per-environment filtering (optional)</span>
                  <input v-model="wizardForm.envField" class="base-input wz-input" placeholder="e.g. environment, kubernetes.namespace" />
                </label>
                <label class="wz-lbl">
                  Error Keywords
                  <span class="wz-hint">Comma-separated words matched in the message field</span>
                  <input v-model="wizardForm.errorKeywords" class="base-input wz-input" placeholder="ERROR,Exception,FATAL,CRITICAL" />
                </label>
                <label class="wz-lbl">
                  Max Services
                  <span class="wz-hint">How many services to display (top N by volume)</span>
                  <input v-model.number="wizardForm.serviceLimit" type="number" min="1" max="50" class="base-input wz-input" />
                </label>
              </div>
            </div>

            <!-- Infrastructure -->
            <div class="wz-section">
              <div class="wz-section-title">
                <span class="wz-section-num">2</span>
                Infrastructure Metrics
                <span class="wz-optional">optional</span>
              </div>
              <p class="wz-section-desc">Show CPU and memory per host from Metricbeat. Leave blank to skip.</p>
              <div class="wz-fields-grid-3">
                <label class="wz-lbl">
                  Metricbeat Index Pattern
                  <IndexPicker :conn-id="activeConnId" v-model="wizardForm.metricIndex" placeholder=".ds-metricbeat-*, metricbeat-*" />
                </label>
                <label class="wz-lbl">
                  CPU Field
                  <input v-model="wizardForm.cpuField" class="base-input wz-input wz-mono" placeholder="host.cpu.usage" />
                </label>
                <label class="wz-lbl">
                  Memory Used % Field
                  <input v-model="wizardForm.memField" class="base-input wz-input wz-mono" placeholder="system.memory.actual.used.pct" />
                </label>
              </div>
            </div>

            <!-- Alerts -->
            <div class="wz-section">
              <div class="wz-section-title">
                <span class="wz-section-num">3</span>
                Alert History
                <span class="wz-optional">optional</span>
              </div>
              <p class="wz-section-desc">Show recent ElastAlert or similar alert events. Leave blank to skip.</p>
              <div class="wz-fields-grid-3">
                <label class="wz-lbl">
                  Alert Index Pattern
                  <IndexPicker :conn-id="activeConnId" v-model="wizardForm.alertIndex" placeholder="elastalert_status, .alerts-*" />
                </label>
                <label class="wz-lbl">
                  Rule Name Field
                  <input v-model="wizardForm.alertRuleField" class="base-input wz-input wz-mono" placeholder="rule_name" />
                </label>
                <label class="wz-lbl">
                  Host Field in Alert Doc
                  <input v-model="wizardForm.alertHostField" class="base-input wz-input wz-mono" placeholder="match_body.host.name" />
                </label>
              </div>
            </div>
          </div>

          <!-- Wizard footer -->
          <div class="wz-footer">
            <button v-if="wizardStep === 'configure'" class="base-btn base-btn--ghost" @click="wizardStep = 'preset'">← Back</button>
            <div style="flex:1" />
            <button v-if="configured" class="base-btn base-btn--ghost" @click="resetSettings(); showWizard=false; resetAll()">Reset</button>
            <button
              v-if="wizardStep === 'configure'"
              class="base-btn base-btn--primary"
              :disabled="!wizardForm.logIndex.trim()"
              @click="applyWizard"
            >Save &amp; Load Dashboard</button>
          </div>

        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sh-root { background: var(--bg-body); padding: 18px; gap: 14px; }

/* Header */
.sh-topbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.sh-title { display: flex; align-items: center; gap: 12px; }
.sh-title h1 { margin: 0; font-size: 20px; color: var(--text-primary); }
.sh-title p  { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.sh-logo { width: 38px; height: 38px; border-radius: 50%; flex-shrink: 0; transition: background 0.3s; }
.sh-controls { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sh-conn-sel { width: 210px; }
.sh-refresh-sel { width: 145px; }
.sh-last-updated { margin: 0; font-size: 11px; color: var(--text-muted); display: flex; align-items: center; gap: 8px; }
.sh-preset-badge { font-size: 10px; font-weight: 700; padding: 1px 8px; border-radius: 9px; background: color-mix(in srgb, #00bfb3 18%, transparent); color: #00bfb3; border: 1px solid color-mix(in srgb, #00bfb3 35%, transparent); }
.sh-live-dot { width: 7px; height: 7px; border-radius: 50%; background: #00bfb3; box-shadow: 0 0 5px #00bfb3; flex-shrink: 0; }

/* Dots */
.dot-green  { background: var(--success); }
.dot-yellow { background: var(--warning); }
.dot-red    { background: var(--danger); }
.dot-gray   { background: var(--text-muted); }

/* Seg */
.sh-seg { display: flex; border: 1px solid var(--border); border-radius: 7px; overflow: hidden; }
.sh-seg button { border:0; background:transparent; color:var(--text-muted); padding:6px 10px; cursor:pointer; font-size:12px; font-weight:600; transition:background .15s,color .15s; }
.sh-seg button.active { background:#00bfb3; color:#fff; }

/* Env bar */
.sh-env-bar { display:flex; align-items:center; gap:4px; border:1px solid var(--border); border-radius:7px; padding:3px; background:var(--bg-elevated); }
.sh-env-btn { border:0; background:transparent; color:var(--text-muted); padding:4px 10px; cursor:pointer; font-size:12px; font-weight:600; border-radius:5px; transition:background .15s,color .15s; white-space:nowrap; }
.sh-env-btn.active { background:#6366f1; color:#fff; }
.sh-env-btn:hover:not(.active) { background:var(--bg-body); color:var(--text-primary); }
.sh-env-badge { display:inline-flex; align-items:center; font-size:10px; font-weight:700; padding:1px 7px; border-radius:9px; background:color-mix(in srgb,#6366f1 18%,transparent); color:#6366f1; border:1px solid color-mix(in srgb,#6366f1 35%,transparent); margin-left:8px; vertical-align:middle; text-transform:uppercase; letter-spacing:.04em; }

/* Empty / onboard */
.sh-empty { border:1px solid var(--border); background:var(--bg-elevated); border-radius:8px; padding:40px; text-align:center; color:var(--text-muted); }
.sh-empty h2 { margin:0 0 8px; color:var(--text-primary); font-size:16px; }
.sh-onboard { padding:56px 40px; }
.sh-onboard-icon { font-size:48px; margin-bottom:12px; }
.sh-onboard h2 { font-size:22px; margin-bottom:10px; }
.sh-onboard-sub { font-size:13px; margin-bottom:20px; }
.sh-empty code { font-family:var(--mono); background:var(--bg-body); padding:1px 5px; border-radius:3px; font-size:12px; }
.sh-inline-btn { background:none; border:none; color:#00bfb3; cursor:pointer; text-decoration:underline; font-size:inherit; padding:0; }

/* Cards */
.sh-cards { display:grid; grid-template-columns:repeat(var(--card-cols,4),minmax(0,1fr)); gap:12px; }
.sh-card { border:1px solid var(--border); background:var(--bg-elevated); border-radius:10px; padding:14px; display:flex; flex-direction:column; gap:10px; border-top:3px solid var(--svc-color,var(--border)); transition:box-shadow .2s; }
.sh-card:hover { box-shadow:0 4px 16px rgba(0,0,0,.12); }
.sh-card.sh-healthy  { border-top-color:var(--success); }
.sh-card.sh-degraded { border-top-color:var(--warning); }
.sh-card.sh-critical { border-top-color:var(--danger); }
.sh-card-skeleton,.sh-card-empty { opacity:.5; }
.sh-card-empty { grid-column:1/-1; text-align:center; color:var(--text-muted); font-size:13px; padding:20px; }
.sh-card-head  { display:flex; align-items:center; gap:8px; }
.sh-card-dot   { width:8px; height:8px; border-radius:50%; flex-shrink:0; }
.sh-card-label { font-size:13px; font-weight:700; color:var(--text-primary); flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.sh-card-status { font-size:10px; font-weight:700; text-transform:uppercase; letter-spacing:.05em; color:var(--text-muted); }
.sh-card-stats { display:grid; grid-template-columns:repeat(3,1fr); gap:6px; }
.sh-card-stat  { display:flex; flex-direction:column; gap:2px; }
.sh-card-stat span   { font-size:10px; color:var(--text-muted); text-transform:uppercase; }
.sh-card-stat strong { font-size:15px; font-weight:700; color:var(--text-primary); }
.sh-card-bar-bg  { height:4px; background:var(--border); border-radius:2px; overflow:hidden; }
.sh-card-bar-fill { height:100%; border-radius:2px; transition:width .5s; }

/* Legend */
.sh-legend-bar { display:flex; align-items:center; gap:12px; flex-wrap:wrap; padding:4px 0; }
.sh-leg-item   { display:flex; align-items:center; gap:5px; font-size:11px; color:var(--text-muted); }
.sh-leg-dot    { width:8px; height:8px; border-radius:2px; flex-shrink:0; }
.sh-leg-error  { background:var(--danger)!important; opacity:.85; }

/* Main layout */
.sh-main      { display:grid; grid-template-columns:1fr 300px; gap:14px; }
.sh-main-full { grid-template-columns:1fr; }
.sh-panel       { border:1px solid var(--border); background:var(--bg-elevated); border-radius:8px; padding:14px; display:flex; flex-direction:column; gap:10px; }
.sh-panel-head  { display:flex; align-items:center; justify-content:space-between; gap:8px; flex-wrap:wrap; }
.sh-panel-title { font-size:13px; font-weight:700; color:var(--text-primary); }
.sh-panel-empty { color:var(--text-muted); font-size:12.5px; text-align:center; padding:24px 0; }
.sh-muted       { color:var(--text-muted); font-size:11.5px; }
.sh-muted code  { font-family:var(--mono); font-size:10.5px; }

/* Timeline */
.sh-timeline { display:flex; flex-direction:column; gap:4px; }
.sh-tl-bars  { display:flex; align-items:flex-end; height:130px; gap:2px; }
.sh-tl-col   { flex:1; height:100%; display:flex; align-items:flex-end; cursor:crosshair; }
.sh-tl-bar-wrap { width:100%; position:relative; display:flex; flex-direction:column; justify-content:flex-end; min-height:2px; overflow:hidden; border-radius:2px 2px 0 0; background:color-mix(in srgb,#00bfb3 12%,transparent); }
.sh-tl-seg    { width:100%; flex-shrink:0; opacity:.55; transition:opacity .15s; }
.sh-tl-col:hover .sh-tl-seg { opacity:.9; }
.sh-tl-error-overlay { position:absolute; bottom:0; left:0; right:0; background:var(--danger); opacity:.4; }
.sh-tl-labels { display:flex; justify-content:space-between; }
.sh-tl-labels span { font-size:10px; color:var(--text-muted); font-family:var(--mono); }
.sh-side { display:flex; flex-direction:column; gap:14px; }

/* Infra */
.sh-infra-list   { display:flex; flex-direction:column; gap:10px; max-height:340px; overflow-y:auto; }
.sh-infra-row    { display:flex; flex-direction:column; gap:5px; }
.sh-infra-host   { font-size:11.5px; font-weight:600; color:var(--text-primary); font-family:var(--mono); }
.sh-infra-metrics { display:flex; flex-direction:column; gap:4px; }
.sh-infra-metric  { display:grid; grid-template-columns:28px 1fr 36px; align-items:center; gap:6px; font-size:10.5px; color:var(--text-muted); font-weight:700; text-transform:uppercase; }
.sh-pct-bar  { height:6px; background:var(--border); border-radius:3px; overflow:hidden; }
.sh-pct-fill { height:100%; border-radius:3px; transition:width .5s; }
.pct-ok   { background:var(--success); }
.pct-warn { background:var(--warning); }
.pct-crit { background:var(--danger); }
.sh-pct-val { font-family:var(--mono); font-size:10.5px; text-align:right; }

/* Alert summary */
.sh-alert-summary  { display:flex; flex-direction:column; gap:4px; }
.sh-alert-sum-row  { display:flex; align-items:center; justify-content:space-between; padding:6px 10px; border-radius:6px; border:1px solid var(--border); }
.sh-alert-rule     { font-size:12px; color:var(--text-primary); font-weight:600; }
.sh-alert-count    { font-size:14px; font-weight:800; color:var(--text-primary); }
.sh-alert-table-wrap { overflow:auto; border:1px solid var(--border); border-radius:6px; }
.sh-alert-table { width:100%; border-collapse:collapse; font-size:12px; }
.sh-alert-table th { background:var(--bg-body); color:var(--text-muted); font-weight:700; font-size:10.5px; text-transform:uppercase; letter-spacing:.04em; padding:7px 12px; text-align:left; border-bottom:1px solid var(--border); white-space:nowrap; }
.sh-alert-table td { padding:7px 12px; border-bottom:1px solid var(--border); vertical-align:middle; }
.sh-alert-table tr:last-child td { border-bottom:none; }
.sh-alert-table tbody tr:hover td { background:var(--bg-elevated); }
.sh-alert-row-hit td { background:color-mix(in srgb,var(--danger) 5%,transparent); }
.sh-rule-badge { font-size:11px; font-weight:600; padding:2px 8px; border-radius:4px; display:inline-block; }
.sh-mono   { font-family:var(--mono); font-size:11.5px; }
.sh-center { text-align:center; }
.alert-down  { border-color:color-mix(in srgb,var(--danger) 35%,var(--border)); background:color-mix(in srgb,var(--danger) 8%,transparent); }
.alert-down  .sh-alert-count { color:var(--danger); }
.alert-cpu   { border-color:color-mix(in srgb,var(--warning) 35%,var(--border)); background:color-mix(in srgb,var(--warning) 8%,transparent); }
.alert-cpu   .sh-alert-count { color:var(--warning); }
.alert-mem   { border-color:color-mix(in srgb,#6366f1 35%,var(--border)); background:color-mix(in srgb,#6366f1 8%,transparent); }
.alert-mem   .sh-alert-count { color:#6366f1; }
.alert-disk  { border-color:color-mix(in srgb,#f59e0b 35%,var(--border)); background:color-mix(in srgb,#f59e0b 8%,transparent); }
.alert-disk  .sh-alert-count { color:#d97706; }
.alert-other.sh-rule-badge { background:var(--bg-body); color:var(--text-muted); border:1px solid var(--border); }
.alert-down.sh-rule-badge  { background:color-mix(in srgb,var(--danger) 14%,transparent); color:var(--danger); border:1px solid color-mix(in srgb,var(--danger) 28%,transparent); }
.alert-cpu.sh-rule-badge   { background:color-mix(in srgb,var(--warning) 14%,transparent); color:var(--warning); border:1px solid color-mix(in srgb,var(--warning) 28%,transparent); }
.alert-mem.sh-rule-badge   { background:color-mix(in srgb,#6366f1 14%,transparent); color:#6366f1; border:1px solid color-mix(in srgb,#6366f1 28%,transparent); }
.alert-disk.sh-rule-badge  { background:color-mix(in srgb,#f59e0b 14%,transparent); color:#d97706; border:1px solid color-mix(in srgb,#f59e0b 28%,transparent); }
.text-danger { color:var(--danger)!important; }
.text-warn   { color:var(--warning)!important; }

/* ─── Wizard ─────────────────────────────────────────── */
.wz-overlay { position:fixed; inset:0; background:rgba(0,0,0,.55); z-index:9999; display:flex; align-items:center; justify-content:center; padding:24px; }
.wz-modal   { background:var(--bg-elevated); border:1px solid var(--border); border-radius:12px; width:760px; max-width:100%; max-height:90vh; display:flex; flex-direction:column; overflow:hidden; box-shadow:0 20px 60px rgba(0,0,0,.25); }
.wz-head    { display:flex; align-items:center; justify-content:space-between; padding:18px 20px; border-bottom:1px solid var(--border); gap:12px; }
.wz-head-left { display:flex; align-items:center; gap:14px; }
.wz-icon    { font-size:28px; }
.wz-title   { font-size:16px; font-weight:700; color:var(--text-primary); }
.wz-subtitle { font-size:12px; color:var(--text-muted); margin-top:2px; display:flex; align-items:center; gap:6px; }
.wz-step-active { color:var(--text-primary); font-weight:700; }
.wz-step-sep    { opacity:.4; }

.wz-body { flex:1; overflow-y:auto; padding:20px; display:flex; flex-direction:column; gap:20px; }
.wz-desc { margin:0; font-size:13px; color:var(--text-muted); }

.wz-presets { display:grid; grid-template-columns:repeat(2,1fr); gap:12px; }
.wz-preset-card { display:flex; flex-direction:column; align-items:flex-start; gap:4px; padding:16px; border:2px solid var(--border); border-radius:10px; cursor:pointer; background:var(--bg-body); transition:border-color .15s,box-shadow .15s; position:relative; text-align:left; }
.wz-preset-card:hover { border-color:#00bfb3; box-shadow:0 0 0 3px color-mix(in srgb,#00bfb3 18%,transparent); }
.wz-preset-selected { border-color:#00bfb3!important; background:color-mix(in srgb,#00bfb3 8%,var(--bg-body))!important; }
.wz-preset-icon  { font-size:24px; }
.wz-preset-label { font-size:14px; font-weight:700; color:var(--text-primary); }
.wz-preset-desc  { font-size:12px; color:var(--text-muted); line-height:1.4; }
.wz-preset-check { position:absolute; top:10px; right:12px; font-size:16px; color:#00bfb3; font-weight:700; }

.wz-section       { display:flex; flex-direction:column; gap:10px; }
.wz-section-title { display:flex; align-items:center; gap:8px; font-size:13px; font-weight:700; color:var(--text-primary); }
.wz-section-num   { width:22px; height:22px; border-radius:50%; background:#00bfb3; color:#fff; font-size:11px; font-weight:800; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
.wz-section-desc  { margin:0; font-size:12.5px; color:var(--text-muted); }
.wz-optional      { font-size:10px; font-weight:600; padding:1px 8px; border-radius:9px; background:var(--bg-body); color:var(--text-muted); border:1px solid var(--border); }
.wz-required      { color:var(--danger); }
.wz-fields-grid-2 { display:grid; grid-template-columns:repeat(2,1fr); gap:12px; }
.wz-fields-grid-3 { display:grid; grid-template-columns:repeat(3,1fr); gap:12px; }
.wz-lbl  { display:flex; flex-direction:column; gap:5px; font-size:11px; font-weight:700; color:var(--text-muted); text-transform:uppercase; letter-spacing:.03em; }
.wz-hint { font-weight:400; text-transform:none; font-size:10.5px; color:var(--text-muted); }
.wz-hint code { font-family:var(--mono); background:var(--bg-body); padding:0 3px; border-radius:3px; }
.wz-input { font-size:12.5px; }
.wz-mono  { font-family:var(--mono)!important; }
.wz-field-row { display:flex; gap:6px; }
.wz-field-row input { flex:1; }
.wz-detect-error { font-size:11px; color:var(--warning); font-weight:400; text-transform:none; }

.wz-footer { display:flex; align-items:center; gap:10px; padding:14px 20px; border-top:1px solid var(--border); background:var(--bg-body); }

@media(max-width:1100px) {
  .sh-cards { grid-template-columns:repeat(3,1fr)!important; }
  .sh-main  { grid-template-columns:1fr; }
  .wz-fields-grid-3 { grid-template-columns:1fr 1fr; }
}
@media(max-width:700px) {
  .sh-cards { grid-template-columns:1fr 1fr!important; }
  .sh-topbar,.sh-controls { flex-direction:column; align-items:stretch; }
  .wz-presets,.wz-fields-grid-2,.wz-fields-grid-3 { grid-template-columns:1fr; }
}
</style>
