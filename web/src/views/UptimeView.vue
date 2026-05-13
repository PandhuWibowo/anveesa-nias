<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useObsSettings, fetchIndices, UPTIME_DEFAULTS } from '@/composables/useObsSettings'
import IndexPicker from '@/components/ui/IndexPicker.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit  = defineEmits<{ (e: 'set-conn', id: number): void }>()

// ── Types ─────────────────────────────────────────────────────────────────────
interface Monitor {
  id: string; name: string; url: string; type: string
  status: 'up' | 'down' | 'unknown'
  durationMs: number; avgDurationMs: number
  httpCode: number | null
  tlsExpiry: string | null; tlsDaysLeft: number | null
  tags: string[]; location: string
  totalChecks: number; upChecks: number; downChecks: number; uptimePct: number
  lastChecked: string
  sparkline: SparkBar[]   // 24h hourly downtime sparkline
}
interface SparkBar { hasDown: boolean; pct: number }
interface TimelineBucket { key: number; key_as_string: string; avgMs: number; isDown: boolean }
interface PingBucket     { key: number; total: number; down: number }
type AutoRefresh = 0 | 30 | 60 | 300
type TabFilter   = 'all' | 'up' | 'down'

// ── Connections ───────────────────────────────────────────────────────────────
const { connections, fetchConnections } = useConnections()
const searchConns = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn  = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch    = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

// ── Persistent settings ───────────────────────────────────────────────────────
const { settings: uptimeSettings } = useObsSettings('uptime', UPTIME_DEFAULTS)
const heartbeatIndex = computed({
  get: () => uptimeSettings.value.heartbeatIndex || 'heartbeat-*',
  set: (v: string) => { uptimeSettings.value.heartbeatIndex = v },
})
const showSettings  = ref(false)
const settingsIndex = ref('')
const autoDetecting = ref(false)

// ── State ─────────────────────────────────────────────────────────────────────
const loading       = ref(false)
const lastUpdated   = ref<Date | null>(null)
const autoRefresh   = ref<AutoRefresh>(30)
const refreshTimer  = ref<ReturnType<typeof setInterval> | null>(null)
const tabFilter     = ref<TabFilter>('all')
const activeTag     = ref('')

const monitors      = ref<Monitor[]>([])
const availableTags = ref<string[]>([])
const pingHistory   = ref<PingBucket[]>([])   // overall pings over time (24h)

const selectedMonitor = ref<Monitor | null>(null)
const detailTimeline  = ref<TimelineBucket[]>([])
const loadingDetail   = ref(false)

// ── Computed ──────────────────────────────────────────────────────────────────
const upCount   = computed(() => monitors.value.filter(m => m.status === 'up').length)
const downCount = computed(() => monitors.value.filter(m => m.status === 'down').length)
const tlsWarn   = computed(() => monitors.value.filter(m => m.tlsDaysLeft !== null && m.tlsDaysLeft <= 60).length)
const overallUp = computed(() => downCount.value === 0 && monitors.value.length > 0)
const avgLatency = computed(() => {
  const list = monitors.value.filter(m => m.durationMs > 0)
  if (!list.length) return 0
  return Math.round(list.reduce((s, m) => s + m.durationMs, 0) / list.length)
})

const filteredMonitors = computed(() => {
  let list = monitors.value
  if (tabFilter.value === 'up')   list = list.filter(m => m.status === 'up')
  if (tabFilter.value === 'down') list = list.filter(m => m.status === 'down')
  if (activeTag.value) list = list.filter(m => m.tags.includes(activeTag.value))
  return list
})

// Group monitors by first meaningful tag (production / sandbox / other)
const monitorGroups = computed(() => {
  const groups: Record<string, typeof filteredMonitors.value> = {}
  for (const m of filteredMonitors.value) {
    const env = m.tags.find(t => t === 'production' || t === 'sandbox') ?? 'other'
    ;(groups[env] ??= []).push(m)
  }
  const order = ['production', 'sandbox', 'other']
  return order.filter(k => groups[k]).map(k => ({ label: k, monitors: groups[k] }))
})

const pingHistMax = computed(() => Math.max(...pingHistory.value.map(b => b.total), 1))

// ── Lifecycle ─────────────────────────────────────────────────────────────────
onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConns.value.length === 1) { emit('set-conn', searchConns.value[0].id); return }
  if (isSearch.value) {
    if (!uptimeSettings.value.heartbeatIndex) await autoDetectHeartbeat()
    settingsIndex.value = heartbeatIndex.value
    await loadAll()
  }
  startTimer()
})
watch(() => props.activeConnId, () => { monitors.value = []; if (isSearch.value) loadAll() })
watch(autoRefresh, () => { clearTimer(); startTimer() })
onBeforeUnmount(clearTimer)
function startTimer() { if (autoRefresh.value > 0) refreshTimer.value = setInterval(loadAll, autoRefresh.value * 1000) }
function clearTimer()  { if (refreshTimer.value) { clearInterval(refreshTimer.value); refreshTimer.value = null } }

// ── Auto-detect heartbeat index ───────────────────────────────────────────────
// Uses a real search probe — _cat/indices doesn't list data streams, so
// index-listing is unreliable for detecting heartbeat-* data streams.
async function probePattern(pattern: string): Promise<boolean> {
  try {
    const { data } = await axios.post(
      `/api/connections/${activeConn.value!.id}/search/aggregate`,
      { index: pattern, query: { range: { '@timestamp': { gte: 'now-1d' } } }, aggs: null, size: 0 },
    )
    const total = data?.hits?.total?.value ?? data?.hits?.total ?? 0
    return total > 0
  } catch { return false }
}

async function autoDetectHeartbeat() {
  if (!activeConn.value) return
  autoDetecting.value = true
  try {
    for (const candidate of ['heartbeat-*', '.ds-heartbeat-*', 'heartbeat*']) {
      if (await probePattern(candidate)) {
        uptimeSettings.value.heartbeatIndex = candidate
        return
      }
    }
    // Nothing found — keep default so the empty state shows a useful message
    uptimeSettings.value.heartbeatIndex = 'heartbeat-*'
  } finally { autoDetecting.value = false }
}

// ── API ───────────────────────────────────────────────────────────────────────
async function agg(query: any, aggs: any, size = 0) {
  const { data } = await axios.post(`/api/connections/${activeConn.value!.id}/search/aggregate`, {
    index: heartbeatIndex.value, query, aggs, size,
  })
  return data
}

// ── Load ──────────────────────────────────────────────────────────────────────
async function loadAll() {
  if (!activeConn.value) return
  loading.value = true
  try {
    await Promise.all([loadMonitors(), loadPingHistory()])
    lastUpdated.value = new Date()
  } finally { loading.value = false }
}

async function loadMonitors() {
  // Step 1: current status per monitor (last 5 min)
    const statusData = await agg(
    { range: { '@timestamp': { gte: 'now-15m' } } },
    {
      by_monitor: {
        terms: { field: 'monitor.id', size: 100 },
        aggs: {
          latest: { top_hits: { size: 1, sort: [{ '@timestamp': 'desc' }],
            _source: ['monitor.name','monitor.status','monitor.type','monitor.duration.us',
              'url.full','tags','tls.certificate_not_valid_after',
              'http.response.status_code','state','monitoring_location','@timestamp'] } },
          avg_dur: { avg: { field: 'monitor.duration.us' } },
        },
      },
    },
  ).catch(() => null)

  // Step 2: 24h sparkline per monitor (hourly down count)
  const sparkData = await agg(
    { range: { '@timestamp': { gte: 'now-24h' } } },
    {
      by_monitor: {
        terms: { field: 'monitor.id', size: 100 },
        aggs: {
          over_time: {
            date_histogram: { field: '@timestamp', fixed_interval: '1h', min_doc_count: 0 },
            aggs: { down: { filter: { term: { 'monitor.status': 'down' } } } },
          },
        },
      },
    },
  ).catch(() => null)

  // Build sparklines map
  const sparkMap: Record<string, SparkBar[]> = {}
  for (const b of sparkData?.aggregations?.by_monitor?.buckets ?? []) {
    const buckets: any[] = b.over_time?.buckets ?? []
    const max = Math.max(...buckets.map((x: any) => x.doc_count), 1)
    sparkMap[b.key] = buckets.map((x: any) => ({
      hasDown: x.down?.doc_count > 0,
      pct: Math.max(4, Math.round((x.doc_count / max) * 100)),
    }))
  }

  const allTags = new Set<string>()
  const buckets: any[] = statusData?.aggregations?.by_monitor?.buckets ?? []

  monitors.value = buckets.map(b => {
    const src = b.latest?.hits?.hits?.[0]?._source ?? {}
    const tlsExpiry   = src.tls?.certificate_not_valid_after ?? null
    const tlsDaysLeft = tlsExpiry ? Math.ceil((new Date(tlsExpiry).getTime() - Date.now()) / 86400000) : null
    const tags: string[] = src.tags ?? [];
    tags.forEach(t => allTags.add(t))
    const totalChecks = src.state?.checks ?? 0
    const upChecks    = src.state?.up ?? 0
    const downChecks  = src.state?.down ?? 0
    return {
      id: b.key, name: src.monitor?.name ?? b.key, url: src.url?.full ?? '',
      type: src.monitor?.type ?? 'http',
      status: (src.monitor?.status ?? 'unknown') as 'up' | 'down' | 'unknown',
      durationMs: Math.round((src.monitor?.duration?.us ?? 0) / 1000),
      avgDurationMs: Math.round((b.avg_dur?.value ?? 0) / 1000),
      httpCode: src.http?.response?.status_code ?? null,
      tlsExpiry, tlsDaysLeft, tags,
      location: src.monitoring_location ?? '',
      totalChecks, upChecks, downChecks,
      uptimePct: totalChecks > 0 ? (upChecks / totalChecks) * 100 : 100,
      lastChecked: src['@timestamp'] ?? '',
      sparkline: sparkMap[b.key] ?? [],
    }
  }).sort((a, b) => {
    if (a.status === 'down' && b.status !== 'down') return -1
    if (b.status === 'down' && a.status !== 'down') return 1
    return a.name.localeCompare(b.name)
  })

  availableTags.value = [...allTags].sort()
}

async function loadPingHistory() {
  const d = await agg(
    { range: { '@timestamp': { gte: 'now-24h' } } },
    {
      over_time: {
        date_histogram: { field: '@timestamp', fixed_interval: '1h', min_doc_count: 0 },
        aggs: { down: { filter: { term: { 'monitor.status': 'down' } } } },
      },
    },
  ).catch(() => null)
  pingHistory.value = (d?.aggregations?.over_time?.buckets ?? []).map((b: any) => ({
    key: b.key, total: b.doc_count, down: b.down?.doc_count ?? 0,
  }))
}

// ── Detail ────────────────────────────────────────────────────────────────────
async function selectMonitor(m: Monitor) {
  selectedMonitor.value = m; detailTimeline.value = []; loadingDetail.value = true
  try {
    const d = await agg(
      { bool: { filter: [{ range: { '@timestamp': { gte: 'now-24h' } } }, { term: { 'monitor.id': m.id } }] } },
      { over_time: { date_histogram: { field: '@timestamp', fixed_interval: '30m', min_doc_count: 0 },
          aggs: { avg_dur: { avg: { field: 'monitor.duration.us' } }, down_count: { filter: { term: { 'monitor.status': 'down' } } } } } },
    ).catch(() => null)
    const tlMax = Math.max(...(d?.aggregations?.over_time?.buckets ?? []).map((b: any) => Math.round((b.avg_dur?.value ?? 0) / 1000)), 1)
    detailTimeline.value = (d?.aggregations?.over_time?.buckets ?? []).map((b: any) => ({
      key: b.key, key_as_string: b.key_as_string,
      avgMs: Math.round((b.avg_dur?.value ?? 0) / 1000),
      isDown: (b.down_count?.doc_count ?? 0) > 0,
    }))
  } finally { loadingDetail.value = false }
}
function closeDetail() { selectedMonitor.value = null; detailTimeline.value = [] }

// ── Settings ──────────────────────────────────────────────────────────────────
function openSettings() { settingsIndex.value = heartbeatIndex.value; showSettings.value = true }
function applySettings() { uptimeSettings.value.heartbeatIndex = settingsIndex.value; showSettings.value = false; monitors.value = []; loadAll() }
function resetSettings() {
  uptimeSettings.value.heartbeatIndex = 'heartbeat-*'
  settingsIndex.value = 'heartbeat-*'
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function tlsLabel(days: number | null) {
  if (days === null) return null
  if (days <= 0)  return { text: 'Expired', cls: 'tls-crit' }
  if (days <= 7)  return { text: `Expires in ${days}d`, cls: 'tls-crit' }
  if (days <= 30) return { text: `Expires in ${days}d`, cls: 'tls-warn' }
  if (days <= 60) return { text: `Expires in ${days}d`, cls: 'tls-warn' }
  const months = Math.round(days / 30)
  return { text: `Expires in ${months} month${months !== 1 ? 's' : ''}`, cls: 'tls-ok' }
}
function durationClass(ms: number) { return ms > 2000 ? 'dur-crit' : ms > 500 ? 'dur-warn' : 'dur-ok' }
function formatTs(v: string) { if (!v) return '–'; const d = new Date(v); return isNaN(d.getTime()) ? v : `Checked ${d.toLocaleTimeString()}` }
function formatDate(v: string | null) { return v ? new Date(v).toLocaleDateString() : '–' }
function barTime(b: PingBucket) { return new Date(b.key).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
function tlBucketTime(b: TimelineBucket) { return new Date(b.key).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
const tlMax = computed(() => Math.max(...detailTimeline.value.map(b => b.avgMs), 1))
</script>

<template>
  <div class="up-root">

    <!-- ── Header ──────────────────────────────────────────── -->
    <header class="up-header">
      <div class="up-header-left">
        <div class="up-status-globe" :class="loading ? 'globe-loading' : overallUp ? 'globe-ok' : downCount > 0 ? 'globe-down' : 'globe-idle'" />
        <div>
          <h1 class="up-title">Uptime Monitors</h1>
          <p class="up-subtitle">{{ activeConn?.name ?? '—' }} &nbsp;·&nbsp; <code>{{ heartbeatIndex }}</code>
            <span v-if="lastUpdated" class="up-updated">&nbsp;· updated {{ lastUpdated.toLocaleTimeString() }}</span>
          </p>
        </div>
      </div>
      <div class="up-header-right">
        <select class="base-input up-sel" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Cluster…</option>
          <option v-for="c in searchConns" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <select v-model.number="autoRefresh" class="base-input up-sel-sm">
          <option :value="0">Off</option>
          <option :value="30">30s</option>
          <option :value="60">1m</option>
          <option :value="300">5m</option>
        </select>
        <span v-if="autoRefresh > 0" class="up-live-dot" title="Auto-refresh on" />
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || loading" @click="loadAll">{{ loading ? '…' : 'Refresh' }}</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="openSettings">Settings</button>
      </div>
    </header>

    <!-- ── Settings inline panel ───────────────────────────── -->
    <div v-if="showSettings" class="up-settings-panel">
      <div class="up-settings-field">
        <label class="up-settings-label">Heartbeat Index</label>
        <IndexPicker :conn-id="activeConnId" v-model="settingsIndex" placeholder="heartbeat-*" />
      </div>
      <div class="up-settings-actions">
        <button class="base-btn base-btn--primary base-btn--sm" @click="applySettings">Save</button>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="autoDetecting"
          @click="autoDetectHeartbeat().then(() => { settingsIndex = heartbeatIndex; showSettings = false; loadAll() })">
          {{ autoDetecting ? 'Detecting…' : 'Auto-detect' }}
        </button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="resetSettings">Reset</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="showSettings = false">Cancel</button>
      </div>
    </div>

    <!-- ── No connection ───────────────────────────────────── -->
    <div v-if="!isSearch" class="up-placeholder">
      <p>Select an Elasticsearch / OpenSearch connection above.</p>
    </div>

    <template v-else>

      <!-- ── Stat strip ───────────────────────────────────── -->
      <div class="up-stats">
        <div class="up-stat">
          <span class="up-stat-val">{{ monitors.length }}</span>
          <span class="up-stat-lbl">Total</span>
        </div>
        <div class="up-stat up-stat--up">
          <span class="up-stat-val">{{ upCount }}</span>
          <span class="up-stat-lbl">Up</span>
        </div>
        <div class="up-stat" :class="downCount > 0 ? 'up-stat--down' : ''">
          <span class="up-stat-val">{{ downCount }}</span>
          <span class="up-stat-lbl">Down</span>
        </div>
        <div class="up-stat" :class="tlsWarn > 0 ? 'up-stat--warn' : ''">
          <span class="up-stat-val">{{ tlsWarn }}</span>
          <span class="up-stat-lbl">TLS ≤60d</span>
        </div>
        <div class="up-stat">
          <span class="up-stat-val">{{ avgLatency }}<span class="up-stat-unit">ms</span></span>
          <span class="up-stat-lbl">Avg latency</span>
        </div>

        <!-- 24h pings mini-chart -->
        <div class="up-stat up-stat--chart" v-if="pingHistory.length">
          <div class="up-mini-bars">
            <div v-for="b in pingHistory" :key="b.key"
              class="up-mini-bar"
              :class="b.down > 0 ? 'mini-bar-has-down' : ''"
              :style="{ height: `${Math.max(15, Math.round((b.total / pingHistMax) * 100))}%` }"
              :title="`${barTime(b)}: ${b.total} pings${b.down ? ', ' + b.down + ' down' : ''}`"
            >
              <span v-if="b.down > 0" class="mini-bar-down-overlay" :style="{ height: `${Math.round((b.down/b.total)*100)}%` }" />
            </div>
          </div>
          <span class="up-stat-lbl">Pings 24h</span>
        </div>
      </div>

      <!-- ── Filter bar ───────────────────────────────────── -->
      <div class="up-filter-bar">
        <div class="up-filter-seg">
          <button class="up-seg-btn" :class="{ active: tabFilter === 'all' }" @click="tabFilter = 'all'">All</button>
          <button class="up-seg-btn up-seg-up" :class="{ active: tabFilter === 'up' }" @click="tabFilter = 'up'">Up</button>
          <button class="up-seg-btn up-seg-down" :class="{ active: tabFilter === 'down' }" @click="tabFilter = 'down'">Down</button>
        </div>
        <div v-if="availableTags.length" class="up-tag-strip">
          <button class="up-chip" :class="{ 'up-chip-active': activeTag === '' }" @click="activeTag = ''">All</button>
          <button v-for="tag in availableTags" :key="tag"
            class="up-chip"
            :class="{ 'up-chip-active': activeTag === tag }"
            @click="activeTag = tag === activeTag ? '' : tag">{{ tag }}</button>
        </div>
      </div>

      <!-- ── Monitor groups ───────────────────────────────── -->
      <div v-if="filteredMonitors.length" class="up-groups">
        <section v-for="grp in monitorGroups" :key="grp.label" class="up-group">
          <div class="up-group-header">
            <span class="up-group-label">{{ grp.label }}</span>
            <span class="up-group-count">{{ grp.monitors.length }}</span>
            <span v-if="grp.monitors.some(m => m.status === 'down')" class="up-group-down-badge">
              {{ grp.monitors.filter(m => m.status === 'down').length }} down
            </span>
          </div>

          <div class="up-cards">
            <div
              v-for="m in grp.monitors"
              :key="m.id"
              class="up-card"
              :class="m.status === 'down' ? 'up-card-down' : 'up-card-up'"
              @click="selectMonitor(m)"
            >
              <!-- Left accent bar -->
              <div class="up-card-accent" />

              <!-- Status dot + name -->
              <div class="up-card-main">
                <div class="up-card-row1">
                  <span class="up-dot" :class="m.status === 'down' ? 'dot-down' : 'dot-up'" />
                  <span class="up-card-name">{{ m.name }}</span>
                  <span class="up-card-type">{{ m.type.toUpperCase() }}</span>
                </div>
                <a :href="m.url" target="_blank" rel="noopener" class="up-card-url" @click.stop>{{ m.url }}</a>
                <div class="up-card-tags">
                  <span v-for="tag in m.tags.filter(t => t !== grp.label)" :key="tag"
                    class="up-chip up-chip-sm"
                    :class="{ 'up-chip-active': activeTag === tag }"
                    @click.stop="activeTag = tag === activeTag ? '' : tag">{{ tag }}</span>
                </div>
              </div>

              <!-- Right metrics block -->
              <div class="up-card-meta">
                <!-- Uptime bar -->
                <div class="up-uptime-row">
                  <div class="up-uptime-bar-wrap">
                    <div class="up-uptime-bar-fill"
                      :class="m.uptimePct >= 99.9 ? 'fill-ok' : m.uptimePct >= 90 ? 'fill-warn' : 'fill-down'"
                      :style="{ width: `${m.uptimePct}%` }"
                    />
                  </div>
                  <span class="up-uptime-pct"
                    :class="m.uptimePct >= 99.9 ? 'pct-ok' : m.uptimePct >= 90 ? 'pct-warn' : 'pct-down'">
                    {{ m.uptimePct.toFixed(m.uptimePct === 100 || m.uptimePct === 0 ? 0 : 2) }}%
                  </span>
                </div>

                <!-- Sparkline + latency -->
                <div class="up-card-bottom">
                  <div v-if="m.sparkline.length" class="up-spark">
                    <div v-for="(bar, i) in m.sparkline" :key="i"
                      class="up-spark-bar"
                      :class="bar.hasDown ? 'spark-down' : 'spark-up'"
                      :style="{ height: `${bar.pct}%` }"
                    />
                  </div>
                  <div class="up-card-right-nums">
                    <span :class="['up-latency', durationClass(m.durationMs)]">{{ m.durationMs }}ms</span>
                    <template v-if="tlsLabel(m.tlsDaysLeft)">
                      <span :class="['up-tls-badge', tlsLabel(m.tlsDaysLeft)!.cls]">🔒 {{ tlsLabel(m.tlsDaysLeft)!.text }}</span>
                    </template>
                  </div>
                </div>

                <div class="up-card-checked">{{ formatTs(m.lastChecked) }}<span v-if="m.location"> · {{ m.location }}</span></div>
              </div>
            </div>
          </div>
        </section>
      </div>

      <div v-else-if="!loading" class="up-placeholder">
        <p>No heartbeat data in the last 15 minutes for <code>{{ heartbeatIndex }}</code>.</p>
        <button class="base-btn base-btn--primary base-btn--sm" @click="openSettings">Configure Index</button>
      </div>

    </template>

    <!-- ── Detail side panel ───────────────────────────────── -->
    <Teleport to="body">
      <div v-if="selectedMonitor" class="up-panel-overlay" @click.self="closeDetail">
        <div class="up-panel">
          <div class="up-panel-head">
            <div class="up-panel-title">
              <span class="up-dot up-dot-lg" :class="selectedMonitor.status === 'down' ? 'dot-down' : 'dot-up'" />
              <span>{{ selectedMonitor.name }}</span>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="closeDetail">✕</button>
          </div>

          <a :href="selectedMonitor.url" target="_blank" rel="noopener" class="up-panel-url">{{ selectedMonitor.url }}</a>

          <div class="up-panel-grid">
            <div class="up-pg-item"><span class="up-pg-lbl">Type</span><strong>{{ selectedMonitor.type.toUpperCase() }}</strong></div>
            <div class="up-pg-item"><span class="up-pg-lbl">Location</span><strong>{{ selectedMonitor.location || '—' }}</strong></div>
            <div class="up-pg-item"><span class="up-pg-lbl">HTTP Status</span>
              <strong :class="(selectedMonitor.httpCode ?? 0) >= 400 ? 'c-down' : 'c-up'">{{ selectedMonitor.httpCode ?? '—' }}</strong>
            </div>
            <div class="up-pg-item"><span class="up-pg-lbl">Uptime</span>
              <strong :class="selectedMonitor.uptimePct < 99 ? 'c-down' : 'c-up'">{{ selectedMonitor.uptimePct.toFixed(3) }}%</strong>
            </div>
            <div class="up-pg-item"><span class="up-pg-lbl">Checks</span><strong>{{ selectedMonitor.totalChecks.toLocaleString() }}</strong></div>
            <div class="up-pg-item"><span class="up-pg-lbl">Up / Down</span>
              <strong><span class="c-up">{{ selectedMonitor.upChecks.toLocaleString() }}</span> / <span :class="selectedMonitor.downChecks > 0 ? 'c-down' : ''">{{ selectedMonitor.downChecks.toLocaleString() }}</span></strong>
            </div>
            <div class="up-pg-item"><span class="up-pg-lbl">Avg Latency</span>
              <strong :class="durationClass(selectedMonitor.avgDurationMs)">{{ selectedMonitor.avgDurationMs }}ms</strong>
            </div>
            <div v-if="selectedMonitor.tlsExpiry" class="up-pg-item">
              <span class="up-pg-lbl">TLS Expires</span>
              <strong :class="tlsLabel(selectedMonitor.tlsDaysLeft)?.cls">{{ formatDate(selectedMonitor.tlsExpiry) }} ({{ selectedMonitor.tlsDaysLeft }}d)</strong>
            </div>
          </div>

          <div class="up-panel-section">Response Time — last 24h</div>
          <div v-if="loadingDetail" class="up-panel-loading">Loading…</div>
          <div v-else-if="detailTimeline.length" class="up-resp-chart">
            <div class="up-resp-bars">
              <div v-for="b in detailTimeline" :key="b.key"
                class="up-resp-col"
                :class="{ 'up-resp-col-down': b.isDown }"
                :title="`${tlBucketTime(b)}: ${b.avgMs}ms${b.isDown ? ' ⚠ DOWN' : ''}`">
                <div class="up-resp-bar" :class="b.isDown ? 'bar-down' : durationClass(b.avgMs)"
                  :style="{ height: `${Math.max(3, Math.round((b.avgMs / tlMax) * 100))}%` }" />
              </div>
            </div>
            <div class="up-resp-labels"><span>-24h</span><span>-12h</span><span>now</span></div>
            <div class="up-resp-legend">
              <span class="dur-ok">● &lt;500ms</span>
              <span class="dur-warn">● 500ms–2s</span>
              <span class="dur-crit">● &gt;2s</span>
              <span class="c-down">● Down</span>
            </div>
          </div>
          <div v-else class="up-panel-loading">No timeline data.</div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* ── Root ──────────────────────────────────────────────────── */
.up-root {
  display: flex; flex-direction: column; gap: 16px;
  padding: 20px; background: var(--bg-body);
  height: 100%; overflow-y: auto; box-sizing: border-box;
  -webkit-overflow-scrolling: touch;
}

/* ── Header ────────────────────────────────────────────────── */
.up-header { display:flex; align-items:center; justify-content:space-between; gap:12px; flex-wrap:wrap; }
.up-header-left { display:flex; align-items:center; gap:14px; }
.up-title  { margin:0; font-size:18px; font-weight:800; color:var(--text-primary); letter-spacing:-.3px; }
.up-subtitle { margin:3px 0 0; font-size:11.5px; color:var(--text-muted); }
.up-subtitle code { font-family:var(--mono); background:var(--bg-elevated); padding:1px 5px; border-radius:3px; font-size:10.5px; }
.up-updated { color:var(--text-muted); }
.up-header-right { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.up-sel    { width:180px; }
.up-sel-sm { width:72px; }
.up-live-dot { width:7px; height:7px; border-radius:50%; background:var(--success); box-shadow:0 0 5px var(--success); animation:pulse 2s infinite; flex-shrink:0; }
@keyframes pulse { 0%,100%{opacity:1}50%{opacity:.3} }

/* Globe status indicator */
.up-status-globe {
  width:44px; height:44px; border-radius:50%; flex-shrink:0;
  display:flex; align-items:center; justify-content:center;
  transition:background .4s, box-shadow .4s;
}
.globe-ok      { background:color-mix(in srgb,var(--success) 20%,var(--bg-elevated)); box-shadow:0 0 0 3px color-mix(in srgb,var(--success) 30%,transparent); }
.globe-down    { background:color-mix(in srgb,var(--danger) 20%,var(--bg-elevated));  box-shadow:0 0 0 3px color-mix(in srgb,var(--danger) 30%,transparent); animation:globe-pulse 1.5s infinite; }
.globe-loading { background:var(--bg-elevated); box-shadow:0 0 0 3px var(--border); animation:pulse 1s infinite; }
.globe-idle    { background:var(--bg-elevated); box-shadow:0 0 0 3px var(--border); }
@keyframes globe-pulse { 0%,100%{box-shadow:0 0 0 3px color-mix(in srgb,var(--danger) 30%,transparent)} 50%{box-shadow:0 0 0 8px color-mix(in srgb,var(--danger) 10%,transparent)} }

/* ── Settings panel ────────────────────────────────────────── */
.up-settings-panel { display:flex; align-items:flex-end; gap:12px; flex-wrap:wrap; background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; padding:14px 16px; }
.up-settings-field { display:flex; flex-direction:column; gap:5px; flex:1; min-width:260px; }
.up-settings-label { font-size:11px; font-weight:700; text-transform:uppercase; letter-spacing:.05em; color:var(--text-muted); }
.up-settings-actions { display:flex; gap:8px; align-items:center; }

/* ── Stat strip ────────────────────────────────────────────── */
.up-stats { display:flex; gap:12px; flex-wrap:wrap; }
.up-stat {
  display:flex; flex-direction:column; align-items:center; justify-content:center;
  gap:3px; padding:14px 20px; border-radius:10px;
  border:1px solid var(--border); background:var(--bg-elevated);
  min-width:72px;
}
.up-stat--up   { border-color:color-mix(in srgb,var(--success) 35%,transparent); background:color-mix(in srgb,var(--success) 7%,var(--bg-elevated)); }
.up-stat--down { border-color:color-mix(in srgb,var(--danger)  35%,transparent); background:color-mix(in srgb,var(--danger)  7%,var(--bg-elevated)); }
.up-stat--warn { border-color:color-mix(in srgb,var(--warning) 35%,transparent); background:color-mix(in srgb,var(--warning) 7%,var(--bg-elevated)); }
.up-stat-val  { font-size:24px; font-weight:800; color:var(--text-primary); line-height:1; }
.up-stat-unit { font-size:13px; font-weight:600; color:var(--text-muted); }
.up-stat-lbl  { font-size:10.5px; font-weight:600; color:var(--text-muted); text-transform:uppercase; letter-spacing:.04em; white-space:nowrap; }
.up-stat--down .up-stat-val { color:var(--danger); }
.up-stat--up   .up-stat-val { color:var(--success); }
.up-stat--warn .up-stat-val { color:var(--warning); }
.up-stat--chart { flex:1; min-width:140px; max-width:300px; padding:10px 14px; }

/* Mini pings chart inside stat */
.up-mini-bars { display:flex; align-items:flex-end; height:36px; gap:1.5px; width:100%; }
.up-mini-bar  { flex:1; background:color-mix(in srgb,var(--text-muted) 25%,transparent); border-radius:1px 1px 0 0; position:relative; transition:background .2s; }
.mini-bar-has-down { background:color-mix(in srgb,var(--danger) 40%,transparent); }
.mini-bar-down-overlay { position:absolute; bottom:0; left:0; right:0; background:var(--danger); border-radius:0 0 1px 1px; opacity:.7; }

/* ── Filter bar ────────────────────────────────────────────── */
.up-filter-bar { display:flex; align-items:center; gap:12px; flex-wrap:wrap; }
.up-filter-seg { display:flex; border:1px solid var(--border); border-radius:8px; overflow:hidden; }
.up-seg-btn { border:0; background:var(--bg-elevated); color:var(--text-muted); padding:6px 16px; cursor:pointer; font-size:12px; font-weight:600; transition:all .15s; }
.up-seg-btn:not(:last-child) { border-right:1px solid var(--border); }
.up-seg-btn.active { background:var(--text-primary); color:var(--bg-body); }
.up-seg-up.active  { background:var(--success); color:#fff; }
.up-seg-down.active{ background:var(--danger);  color:#fff; }
.up-tag-strip { display:flex; align-items:center; gap:5px; flex-wrap:wrap; }

/* ── Chips ─────────────────────────────────────────────────── */
.up-chip { border:1px solid var(--border); background:transparent; color:var(--text-muted); padding:3px 10px; border-radius:20px; cursor:pointer; font-size:11px; font-weight:600; transition:all .15s; white-space:nowrap; }
.up-chip:hover { border-color:var(--text-muted); color:var(--text-primary); }
.up-chip-active { background:var(--text-primary); border-color:var(--text-primary); color:var(--bg-body); }
.up-chip-sm { padding:1px 7px; font-size:10px; }

/* ── Monitor groups ────────────────────────────────────────── */
.up-groups { display:flex; flex-direction:column; gap:20px; }
.up-group-header { display:flex; align-items:center; gap:8px; margin-bottom:8px; }
.up-group-label { font-size:11px; font-weight:800; text-transform:uppercase; letter-spacing:.06em; color:var(--text-muted); }
.up-group-count { font-size:11px; font-weight:700; padding:1px 7px; border-radius:9px; background:var(--bg-elevated); color:var(--text-muted); border:1px solid var(--border); }
.up-group-down-badge { font-size:10.5px; font-weight:700; padding:1px 8px; border-radius:9px; background:color-mix(in srgb,var(--danger) 15%,transparent); color:var(--danger); border:1px solid color-mix(in srgb,var(--danger) 30%,transparent); }

/* ── Monitor cards ─────────────────────────────────────────── */
.up-cards { display:flex; flex-direction:column; gap:6px; }
.up-card {
  display:flex; align-items:stretch; gap:0;
  border:1px solid var(--border); border-radius:10px;
  background:var(--bg-elevated); cursor:pointer;
  transition:box-shadow .15s, border-color .15s;
  overflow:hidden;
}
.up-card:hover { box-shadow:0 2px 12px rgba(0,0,0,.12); border-color:var(--text-muted); }
.up-card-down { border-color:color-mix(in srgb,var(--danger) 40%,var(--border)); background:color-mix(in srgb,var(--danger) 3%,var(--bg-elevated)); }
.up-card-down:hover { border-color:var(--danger); }

.up-card-accent { width:4px; flex-shrink:0; }
.up-card-up   .up-card-accent { background:var(--success); }
.up-card-down .up-card-accent { background:var(--danger); }

.up-card-main { flex:1; display:flex; flex-direction:column; gap:4px; padding:12px 14px; min-width:0; }
.up-card-row1 { display:flex; align-items:center; gap:8px; }
.up-card-name { font-size:13.5px; font-weight:700; color:var(--text-primary); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.up-card-type { font-size:10px; font-weight:700; padding:1px 6px; border-radius:3px; background:var(--bg-body); color:var(--text-muted); border:1px solid var(--border); flex-shrink:0; }
.up-card-url  { font-family:var(--mono); font-size:11px; color:var(--text-muted); text-decoration:none; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; display:block; }
.up-card-url:hover { color:var(--text-primary); text-decoration:underline; }
.up-card-tags { display:flex; flex-wrap:wrap; gap:3px; margin-top:2px; }

.up-card-meta { display:flex; flex-direction:column; justify-content:space-between; gap:6px; padding:10px 14px; min-width:240px; max-width:300px; border-left:1px solid var(--border); }

/* Uptime bar */
.up-uptime-row { display:flex; align-items:center; gap:8px; }
.up-uptime-bar-wrap { flex:1; height:6px; background:var(--bg-body); border-radius:3px; overflow:hidden; }
.up-uptime-bar-fill { height:100%; border-radius:3px; transition:width .4s; }
.fill-ok   { background:var(--success); }
.fill-warn { background:var(--warning); }
.fill-down { background:var(--danger); }
.up-uptime-pct { font-size:12px; font-weight:800; font-family:var(--mono); white-space:nowrap; min-width:38px; text-align:right; }
.pct-ok   { color:var(--success); }
.pct-warn { color:var(--warning); }
.pct-down { color:var(--danger); }

/* Card bottom row */
.up-card-bottom { display:flex; align-items:flex-end; justify-content:space-between; gap:8px; }
.up-card-right-nums { display:flex; flex-direction:column; align-items:flex-end; gap:2px; }
.up-latency { font-family:var(--mono); font-weight:700; font-size:12px; }
.up-tls-badge { font-size:10px; font-weight:600; white-space:nowrap; }
.tls-ok   { color:var(--text-muted); }
.tls-warn { color:var(--warning); }
.tls-crit { color:var(--danger); }
.up-card-checked { font-size:10px; color:var(--text-muted); }

/* Sparkline */
.up-spark { display:flex; align-items:flex-end; gap:1px; height:28px; flex:1; }
.up-spark-bar { flex:1; border-radius:1px; min-height:2px; transition:height .3s; }
.spark-up   { background:var(--success); opacity:.5; }
.spark-down { background:var(--danger); opacity:.8; }

/* ── Status dots ───────────────────────────────────────────── */
.up-dot { width:9px; height:9px; border-radius:50%; flex-shrink:0; }
.up-dot-lg { width:12px; height:12px; }
.dot-up   { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb,var(--success) 25%,transparent); animation:dot-pulse-up 3s infinite; }
.dot-down { background:var(--danger);  box-shadow:0 0 0 3px color-mix(in srgb,var(--danger)  25%,transparent); animation:dot-pulse-dn 1.5s infinite; }
@keyframes dot-pulse-up { 0%,100%{box-shadow:0 0 0 3px color-mix(in srgb,var(--success) 25%,transparent)} 50%{box-shadow:0 0 0 5px color-mix(in srgb,var(--success) 8%,transparent)} }
@keyframes dot-pulse-dn { 0%,100%{box-shadow:0 0 0 3px color-mix(in srgb,var(--danger) 30%,transparent)}  50%{box-shadow:0 0 0 7px color-mix(in srgb,var(--danger) 8%,transparent)} }

/* ── Latency colors ────────────────────────────────────────── */
.dur-ok   { color:var(--success); }
.dur-warn { color:var(--warning); }
.dur-crit { color:var(--danger); }
.c-up     { color:var(--success); }
.c-down   { color:var(--danger); }

/* ── Placeholder ───────────────────────────────────────────── */
.up-placeholder { border:1px dashed var(--border); border-radius:10px; padding:32px; text-align:center; color:var(--text-muted); display:flex; flex-direction:column; align-items:center; gap:10px; }
.up-placeholder code { font-family:var(--mono); background:var(--bg-elevated); padding:1px 6px; border-radius:3px; }

/* ── Detail side panel ─────────────────────────────────────── */
.up-panel-overlay { position:fixed; inset:0; background:rgba(0,0,0,.4); z-index:9999; display:flex; justify-content:flex-end; }
.up-panel { width:480px; max-width:100%; background:var(--bg-elevated); border-left:1px solid var(--border); height:100%; overflow-y:auto; display:flex; flex-direction:column; gap:16px; padding:22px; box-shadow:-8px 0 28px rgba(0,0,0,.15); }
.up-panel-head { display:flex; align-items:center; justify-content:space-between; }
.up-panel-title { display:flex; align-items:center; gap:10px; font-size:15px; font-weight:800; color:var(--text-primary); }
.up-panel-url { font-family:var(--mono); font-size:11.5px; color:var(--text-muted); text-decoration:none; word-break:break-all; }
.up-panel-url:hover { color:var(--text-primary); text-decoration:underline; }
.up-panel-grid { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
.up-pg-item { background:var(--bg-body); border:1px solid var(--border); border-radius:8px; padding:10px 12px; display:flex; flex-direction:column; gap:3px; }
.up-pg-lbl  { font-size:10px; font-weight:700; text-transform:uppercase; letter-spacing:.05em; color:var(--text-muted); }
.up-pg-item strong { font-size:13px; color:var(--text-primary); }
.up-panel-section { font-size:11px; font-weight:800; text-transform:uppercase; letter-spacing:.06em; color:var(--text-muted); border-top:1px solid var(--border); padding-top:12px; }
.up-panel-loading { text-align:center; color:var(--text-muted); font-size:12.5px; padding:16px; }
.up-resp-chart { display:flex; flex-direction:column; gap:6px; background:var(--bg-body); border:1px solid var(--border); border-radius:8px; padding:12px; }
.up-resp-bars { display:flex; align-items:flex-end; height:90px; gap:2px; }
.up-resp-col  { flex:1; height:100%; display:flex; align-items:flex-end; }
.up-resp-col-down { background:color-mix(in srgb,var(--danger) 8%,transparent); border-radius:2px; }
.up-resp-bar  { width:100%; border-radius:2px 2px 0 0; min-height:2px; transition:height .3s; }
.bar-down { background:var(--danger); }
.up-resp-labels { display:flex; justify-content:space-between; font-size:10px; color:var(--text-muted); font-family:var(--mono); }
.up-resp-legend { display:flex; gap:12px; flex-wrap:wrap; font-size:10.5px; font-weight:600; }

/* ── Responsive ────────────────────────────────────────────── */
@media(max-width:860px) {
  .up-card-meta { min-width:180px; }
  .up-panel { width:100%; }
}
@media(max-width:600px) {
  .up-card { flex-direction:column; }
  .up-card-meta { border-left:0; border-top:1px solid var(--border); max-width:100%; }
  .up-stats { gap:8px; }
  .up-stat { padding:10px 12px; }
}
</style>
