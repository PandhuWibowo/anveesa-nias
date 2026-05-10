<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import { pendingAIAnalytics } from '@/composables/usePendingAIAnalytics'

interface AnalyticsResponse {
  connection_id: number
  database: string
  driver: string
  question: string
  title: string
  summary: string
  chart_type: string
  sql: string
  columns: string[]
  rows: any[][]
  row_count: number
  duration_ms: number
  assumptions: string[]
  follow_up_questions: string[]
  report_cards: string[]
  compare_preset: string
}

interface AIReport {
  id: number
  connection_id: number
  title: string
  question: string
  summary: string
  chart_type: string
  sql: string
  columns: string[]
  rows: any[][]
  report_cards: string[]
  follow_ups: string[]
  created_at: string
}

const toast = useToast()
const { connections, fetchConnections } = useConnections()
const { token, user } = useAuth()

// ── State ──────────────────────────────────────────────────────────────────
const loading = ref(false)
const selectedConnId = ref<number | null>(null)
const question = ref('')
const comparePreset = ref('')
const providedSQL = ref('')
const providedTitle = ref('')
const analysisSource = ref<'saved_query' | 'query_result' | ''>('')
const result = ref<AnalyticsResponse | null>(null)
const error = ref('')
const pinnedReports = ref<AIReport[]>([])
const pinning = ref(false)
const sqlCopied = ref(false)
const showPinnedReports = ref(false)
const inputCollapsed = ref(false)

// ── Stream state ───────────────────────────────────────────────────────────
const streamStep = ref<'planning' | 'executing' | 'summarizing' | ''>('')
const streamMessage = ref('')
const streamPlan = ref<{ title: string; sql: string; chart_type: string; assumptions: string[]; follow_up_questions: string[] } | null>(null)
const streamQuery = ref<{ columns: string[]; rows: any[][]; row_count: number; duration_ms: number } | null>(null)
const streamSummary = ref<{ summary: string; chart_type: string; follow_up_questions: string[]; report_cards: string[] } | null>(null)
const displayedSummary = ref('')
let typewriterTimer: ReturnType<typeof setInterval> | null = null

// ── Prompts ────────────────────────────────────────────────────────────────
const prompts = [
  'Top 10 tables by row count',
  'Categories with highest total sales this month',
  'Daily new orders trend, last 30 days',
  'Users with most failed transactions this week',
  'What changed most in the last 7 days?',
]

const opsPrompts = [
  'Slowest queries and likely causes',
  'Scheduled jobs failing most often',
  'Pending approvals and bottlenecks',
  'Recent backup requests — anything unusual?',
]

const compareOptions = [
  { value: '', label: 'No comparison' },
  { value: 'last_7_days_vs_previous_7_days', label: 'Last 7d vs prev 7d' },
  { value: 'this_month_vs_last_month', label: 'This month vs last month' },
  { value: 'today_vs_yesterday', label: 'Today vs yesterday' },
]

// ── Computed ───────────────────────────────────────────────────────────────
const selectedConnection = computed(() =>
  selectedConnId.value != null ? connections.value.find((c) => c.id === selectedConnId.value) ?? null : null
)

const chartTone = computed(() => {
  const t = streamSummary.value?.chart_type || result.value?.chart_type || streamPlan.value?.chart_type
  switch (t) {
    case 'line': return { label: 'Line trend', color: '#2563eb' }
    case 'area': return { label: 'Area trend', color: '#0f766e' }
    case 'bar': return { label: 'Bar comparison', color: '#7c3aed' }
    case 'horizontal-bar': return { label: 'Horizontal bar', color: '#7c3aed' }
    case 'scatter': return { label: 'Scatter plot', color: '#0891b2' }
    case 'pie': return { label: 'Pie distribution', color: '#ea580c' }
    case 'donut': return { label: 'Donut chart', color: '#ea580c' }
    case 'kpi': return { label: 'KPI metric', color: '#059669' }
    default: return { label: 'Table view', color: 'var(--brand)' }
  }
})

const hasAnyResult = computed(() => !!(streamPlan.value || streamQuery.value || streamSummary.value || result.value))

const activeColumns = computed(() => streamQuery.value?.columns || result.value?.columns || [])
const activeRows = computed(() => streamQuery.value?.rows || result.value?.rows || [])
const activeRowCount = computed(() => streamQuery.value?.row_count ?? result.value?.row_count ?? 0)
const activeDuration = computed(() => streamQuery.value?.duration_ms ?? result.value?.duration_ms ?? 0)
const activeSQL = computed(() => streamPlan.value?.sql || result.value?.sql || '')
const activeTitle = computed(() => streamPlan.value?.title || result.value?.title || '')
const activeAssumptions = computed(() => streamPlan.value?.assumptions || result.value?.assumptions || [])
const activeFollowUps = computed(() => {
  const su = streamSummary.value?.follow_up_questions || result.value?.follow_up_questions || []
  const pl = streamPlan.value?.follow_up_questions || []
  const merged = [...su, ...pl]
  return merged.filter((v, i) => v && merged.indexOf(v) === i)
})
const activeReportCards = computed(() => streamSummary.value?.report_cards || result.value?.report_cards || [])

const steps = computed(() => [
  { id: 'planning', label: 'Plan', done: !!streamPlan.value },
  { id: 'executing', label: 'Query', done: !!streamQuery.value },
  { id: 'summarizing', label: 'Summary', done: !!streamSummary.value },
])

// ── Helpers ────────────────────────────────────────────────────────────────
function startTypewriter(text: string) {
  if (typewriterTimer) clearInterval(typewriterTimer)
  displayedSummary.value = ''
  let i = 0
  typewriterTimer = setInterval(() => {
    if (i < text.length) {
      displayedSummary.value += text[i++]
    } else {
      if (typewriterTimer) clearInterval(typewriterTimer)
    }
  }, 10)
}

function getAuthHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token.value) {
    h['Authorization'] = `Bearer ${token.value}`
    if (user.value) {
      h['X-User-ID'] = String(user.value.id)
      h['X-User-Role'] = user.value.role
      h['X-Username'] = user.value.username
    }
  }
  return h
}

function usePrompt(value: string) {
  if (providedSQL.value) {
    providedSQL.value = ''
    providedTitle.value = ''
    analysisSource.value = ''
  }
  question.value = value
  if (inputCollapsed.value) inputCollapsed.value = false
}

function clearResult() {
  result.value = null
  streamPlan.value = null
  streamQuery.value = null
  streamSummary.value = null
  displayedSummary.value = ''
  streamStep.value = ''
  streamMessage.value = ''
  error.value = ''
  inputCollapsed.value = false
}

function loadPinnedReport(report: AIReport) {
  selectedConnId.value = report.connection_id
  question.value = report.question || ''
  providedSQL.value = report.sql || ''
  providedTitle.value = report.title || ''
  analysisSource.value = 'saved_query'
  result.value = {
    connection_id: report.connection_id,
    database: '',
    driver: '',
    question: report.question,
    title: report.title,
    summary: report.summary,
    chart_type: report.chart_type,
    sql: report.sql,
    columns: report.columns || [],
    rows: report.rows || [],
    row_count: report.rows?.length || 0,
    duration_ms: 0,
    assumptions: [],
    follow_up_questions: report.follow_ups || [],
    report_cards: report.report_cards || [],
    compare_preset: '',
  }
  displayedSummary.value = report.summary
  streamPlan.value = null
  streamQuery.value = null
  streamSummary.value = null
  inputCollapsed.value = true
  showPinnedReports.value = false
}

// ── Streaming analysis ────────────────────────────────────────────────────
async function runAnalysis() {
  if (!selectedConnId.value) { toast.error('Select a connection first'); return }
  if (!question.value.trim() && !providedSQL.value.trim()) { toast.error('Enter an analytics question'); return }

  loading.value = true
  error.value = ''
  result.value = null
  streamPlan.value = null
  streamQuery.value = null
  streamSummary.value = null
  displayedSummary.value = ''
  streamStep.value = 'planning'
  streamMessage.value = 'Starting analysis…'
  inputCollapsed.value = true

  try {
    const res = await fetch('/api/ai/analytics/stream', {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({
        conn_id: selectedConnId.value,
        question: question.value.trim(),
        sql: providedSQL.value.trim() || undefined,
        title: providedTitle.value.trim() || undefined,
        compare_preset: comparePreset.value || undefined,
      }),
    })

    if (!res.ok || !res.body) {
      const text = await res.text()
      let msg = 'Analysis failed'
      try { msg = JSON.parse(text)?.error || msg } catch {}
      throw new Error(msg)
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const parts = buf.split('\n\n')
      buf = parts.pop() ?? ''

      for (const chunk of parts) {
        if (!chunk.trim()) continue
        let evtType = 'message'
        let dataStr = ''
        for (const line of chunk.split('\n')) {
          if (line.startsWith('event: ')) evtType = line.slice(7).trim()
          else if (line.startsWith('data: ')) dataStr = line.slice(6).trim()
        }
        if (!dataStr) continue
        const data = JSON.parse(dataStr)

        switch (evtType) {
          case 'progress':
            streamStep.value = data.step
            streamMessage.value = data.message
            break
          case 'plan':
            streamPlan.value = data
            break
          case 'query':
            streamQuery.value = data
            break
          case 'summary':
            streamSummary.value = data
            startTypewriter(data.summary)
            break
          case 'done':
            result.value = data
            displayedSummary.value = data.summary
            if (typewriterTimer) clearInterval(typewriterTimer)
            loading.value = false
            streamStep.value = ''
            break
          case 'error':
            throw new Error(data.error || 'Analysis failed')
        }
      }
    }
  } catch (e: any) {
    error.value = e?.message || 'Failed to run AI analytics'
    toast.error(error.value)
    inputCollapsed.value = false
  } finally {
    loading.value = false
    if (!result.value) streamStep.value = ''
  }
}

// ── Report actions ─────────────────────────────────────────────────────────
async function loadPinnedReports() {
  try {
    const { data } = await axios.get<AIReport[]>('/api/ai/reports')
    pinnedReports.value = data || []
  } catch {
    pinnedReports.value = []
  }
}

async function pinCurrentReport() {
  const r = result.value
  if (!r) return
  pinning.value = true
  try {
    await axios.post('/api/ai/reports', {
      connection_id: r.connection_id,
      title: r.title,
      question: r.question,
      summary: r.summary,
      chart_type: r.chart_type,
      sql: r.sql,
      columns: r.columns,
      rows: r.rows,
      report_cards: r.report_cards,
      follow_ups: r.follow_up_questions,
    })
    await loadPinnedReports()
    toast.success('Report saved')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save report')
  } finally {
    pinning.value = false
  }
}

async function deletePinnedReport(id: number) {
  try {
    await axios.delete(`/api/ai/reports/${id}`)
    await loadPinnedReports()
    toast.success('Report removed')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete report')
  }
}

async function copySQL() {
  const sql = activeSQL.value
  if (!sql) return
  try {
    await navigator.clipboard.writeText(sql)
    sqlCopied.value = true
    setTimeout(() => { sqlCopied.value = false }, 2000)
  } catch {
    toast.error('Failed to copy SQL')
  }
}

// ── Lifecycle ──────────────────────────────────────────────────────────────
function applyPendingAnalysis() {
  const pending = pendingAIAnalytics.value
  if (!pending) return
  selectedConnId.value = pending.connId
  question.value = pending.question || ''
  providedSQL.value = pending.sql || ''
  providedTitle.value = pending.title || ''
  analysisSource.value = pending.source || ''
  pendingAIAnalytics.value = null
}

watch(connections, (items) => {
  if (!selectedConnId.value && items.length > 0) selectedConnId.value = items[0].id
}, { immediate: true })

onMounted(() => {
  applyPendingAnalysis()
  fetchConnections()
  loadPinnedReports()
})
</script>

<template>
  <div class="page-shell aia-root">
    <div class="page-scroll">
      <div class="page-stack">

        <!-- ── Header ── -->
        <header class="aia-header page-panel">
          <div class="aia-header__inner">
            <div class="aia-header__icon">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 2a10 10 0 1 0 10 10"/><path d="M18 2v4h4"/><path d="M12 12V8"/><path d="M12 12l3-3"/>
              </svg>
            </div>
            <div>
              <div class="aia-header__title">AI Analytics</div>
              <div class="aia-header__sub">Ask questions in plain language — AI generates safe read-only SQL and a summary.</div>
            </div>
          </div>
          <div class="aia-header__actions">
            <button v-if="pinnedReports.length" class="base-btn base-btn--ghost base-btn--sm" @click="showPinnedReports = !showPinnedReports">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>
              Saved
              <span class="aia-badge-count">{{ pinnedReports.length }}</span>
            </button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="fetchConnections">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.5"/></svg>
              Refresh
            </button>
          </div>
        </header>

        <!-- ── Saved reports ── -->
        <Transition name="slide-down">
          <section v-if="showPinnedReports && pinnedReports.length" class="page-panel aia-saved-panel">
            <div class="aia-saved-head">
              <span class="aia-section-title">Saved Reports</span>
              <button class="icon-btn" @click="showPinnedReports = false">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>
            <div class="aia-saved-grid">
              <article v-for="report in pinnedReports" :key="report.id" class="aia-saved-card" @click="loadPinnedReport(report)">
                <div class="aia-saved-card__title">{{ report.title }}</div>
                <div class="aia-saved-card__time">{{ new Date(report.created_at).toLocaleDateString() }}</div>
                <p class="aia-saved-card__summary">{{ report.summary }}</p>
                <div class="aia-saved-card__actions" @click.stop>
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="loadPinnedReport(report)">Open</button>
                  <button class="aia-btn-danger base-btn--xs" @click="deletePinnedReport(report.id)">Delete</button>
                </div>
              </article>
            </div>
          </section>
        </Transition>

        <!-- ── Input panel ── -->
        <section class="page-panel aia-input-panel" :class="{ 'is-collapsed': inputCollapsed }">

          <!-- Collapsed bar -->
          <div v-if="inputCollapsed" class="aia-collapsed-bar">
            <span class="aia-driver-badge">{{ (selectedConnection?.driver || 'DB').charAt(0).toUpperCase() }}</span>
            <span class="aia-collapsed-conn">{{ selectedConnection?.name || 'Connection' }}</span>
            <span class="aia-collapsed-q">{{ question || activeTitle || 'Analysis' }}</span>
            <div class="aia-collapsed-actions">
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="inputCollapsed = false">Edit</button>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="clearResult">New</button>
            </div>
          </div>

          <!-- Full form -->
          <div v-else class="aia-form">

            <!-- Context banner -->
            <div v-if="providedSQL" class="aia-ctx-banner">
              <span class="aia-badge-brand">{{ analysisSource === 'saved_query' ? 'Saved Query' : 'Query Result' }}</span>
              <strong>{{ providedTitle || 'Untitled' }}</strong>
              <span class="aia-ctx-hint">AI will run this SQL instead of generating one.</span>
              <button class="aia-ctx-clear" @click="providedSQL = ''; providedTitle = ''; analysisSource = ''">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>

            <div class="aia-form-row">
              <div class="aia-field">
                <label class="aia-label">Connection</label>
                <div class="aia-select-wrap">
                  <span v-if="selectedConnection" class="aia-driver-badge aia-driver-badge--inset">{{ selectedConnection.driver.charAt(0).toUpperCase() }}</span>
                  <select v-model.number="selectedConnId" class="base-select" :class="{ 'has-badge': selectedConnection }">
                    <option :value="null">Choose connection…</option>
                    <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.name }} · {{ conn.database }}</option>
                  </select>
                </div>
              </div>
              <div class="aia-field">
                <label class="aia-label">Compare</label>
                <select v-model="comparePreset" class="base-select">
                  <option v-for="opt in compareOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                </select>
              </div>
            </div>

            <div class="aia-field">
              <label class="aia-label">{{ providedSQL ? 'Focus (optional)' : 'Question' }}</label>
              <textarea
                v-model="question"
                class="base-textarea aia-textarea"
                rows="3"
                :placeholder="providedSQL
                  ? 'Tell AI what to focus on, e.g. summarize the biggest risk.'
                  : 'Ask a business question, e.g. top 5 products by revenue last 30 days?'"
              ></textarea>
            </div>

            <div v-if="!providedSQL" class="aia-prompts-section">
              <div class="aia-prompts-group">
                <div class="aia-prompts-label">Suggestions</div>
                <div class="aia-chips">
                  <button v-for="p in prompts" :key="p" class="aia-chip" @click="usePrompt(p)">{{ p }}</button>
                </div>
              </div>
              <div class="aia-prompts-group">
                <div class="aia-prompts-label">Ops &amp; Audit</div>
                <div class="aia-chips">
                  <button v-for="p in opsPrompts" :key="p" class="aia-chip" @click="usePrompt(p)">{{ p }}</button>
                </div>
              </div>
            </div>

            <div class="aia-form-footer">
              <button class="base-btn base-btn--primary aia-run-btn" :disabled="loading" @click="runAnalysis">
                <svg v-if="!loading" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
                <svg v-else class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                {{ loading ? 'Analyzing…' : (providedSQL ? 'Analyze Query' : 'Run Analysis') }}
              </button>
            </div>

          </div>
        </section>

        <!-- ── Error ── -->
        <Transition name="fade">
          <div v-if="error" class="aia-error-bar">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            {{ error }}
            <button class="aia-error-dismiss" @click="error = ''">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
        </Transition>

        <!-- ── Progress stepper (visible while loading) ── -->
        <Transition name="fade">
          <div v-if="loading" class="aia-stepper page-panel">
            <div class="aia-stepper__steps">
              <div
                v-for="(step, i) in steps"
                :key="step.id"
                class="aia-step"
                :class="{ 'is-done': step.done, 'is-active': streamStep === step.id }"
              >
                <div class="aia-step__circle">
                  <svg v-if="step.done" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <svg v-else-if="streamStep === step.id" class="spin" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  <span v-else>{{ i + 1 }}</span>
                </div>
                <span class="aia-step__label">{{ step.label }}</span>
                <div v-if="i < steps.length - 1" class="aia-step__line" :class="{ 'is-done': step.done }"></div>
              </div>
            </div>
            <div class="aia-stepper__msg">{{ streamMessage }}</div>
          </div>
        </Transition>

        <!-- ── Empty state ── -->
        <div v-if="!loading && !hasAnyResult" class="page-panel aia-empty">
          <div class="aia-empty__icon">
            <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" opacity="0.35">
              <rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>
              <path d="M7 7h10M7 11h6"/>
            </svg>
          </div>
          <div class="aia-empty__title">No analysis yet</div>
          <div class="aia-empty__sub">Select a connection, type a question, and click Run Analysis to get started.</div>
        </div>

        <!-- ── Progressive results ── -->
        <template v-if="hasAnyResult">

          <!-- Report cards (AI highlights) -->
          <Transition name="slide-up">
            <div v-if="activeReportCards.length" class="aia-report-cards">
              <div v-for="(card, i) in activeReportCards" :key="i" class="aia-report-card">
                <div class="aia-report-card__dot"></div>
                <div class="aia-report-card__text">{{ card }}</div>
              </div>
            </div>
          </Transition>

          <!-- SQL — appears as soon as plan event arrives -->
          <Transition name="slide-up">
            <section v-if="activeSQL" class="page-panel aia-sql-panel">
              <div class="aia-panel-hd">
                <div>
                  <div class="aia-section-title">
                    {{ activeTitle || 'Generated SQL' }}
                    <span v-if="loading && streamStep === 'executing'" class="aia-inline-badge">Running…</span>
                    <span v-else-if="loading && streamStep === 'summarizing'" class="aia-inline-badge aia-inline-badge--ok">Done</span>
                  </div>
                  <div class="aia-section-sub">Read-only query · {{ chartTone.label }}</div>
                </div>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="copySQL">
                  <svg v-if="!sqlCopied" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                  <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                  {{ sqlCopied ? 'Copied!' : 'Copy SQL' }}
                </button>
              </div>
              <pre class="aia-code"><code>{{ activeSQL }}</code></pre>
              <div v-if="activeAssumptions.length" class="aia-assumptions">
                <span v-for="a in activeAssumptions" :key="a" class="aia-assumption-tag">{{ a }}</span>
              </div>
            </section>
          </Transition>

          <!-- Data table — appears as soon as query event arrives -->
          <Transition name="slide-up">
            <section v-if="streamQuery || (result && activeColumns.length)" class="page-panel aia-table-section">
              <div class="aia-panel-hd aia-panel-hd--border">
                <div>
                  <div class="aia-section-title">Result Preview</div>
                  <div class="aia-section-sub">
                    {{ activeRowCount }} row{{ activeRowCount !== 1 ? 's' : '' }}
                    · {{ activeColumns.length }} col{{ activeColumns.length !== 1 ? 's' : '' }}
                    · {{ activeDuration }} ms
                  </div>
                </div>
                <button v-if="result" class="base-btn base-btn--ghost base-btn--sm" :disabled="pinning" @click="pinCurrentReport">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>
                  {{ pinning ? 'Saving…' : 'Save Report' }}
                </button>
              </div>

              <div v-if="!activeColumns.length" class="aia-table-empty">No columns returned.</div>
              <div v-else class="aia-table-wrap">
                <table class="aia-table">
                  <thead>
                    <tr>
                      <th v-for="col in activeColumns" :key="col">{{ col }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, ri) in activeRows" :key="ri">
                      <td v-for="(val, ci) in row" :key="`${ri}-${ci}`">
                        <span v-if="val === null || val === undefined" class="aia-td-null">null</span>
                        <span v-else>{{ val }}</span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>
          </Transition>

          <!-- Narrative — appears as soon as summary event arrives, with typewriter -->
          <Transition name="slide-up">
            <section v-if="displayedSummary || (loading && streamStep === 'summarizing')" class="page-panel aia-narrative">
              <div class="aia-panel-hd">
                <div>
                  <div class="aia-section-title">
                    Summary
                    <span v-if="loading && streamStep === 'summarizing'" class="aia-typing-cursor">|</span>
                  </div>
                  <div class="aia-section-sub">AI-generated insight grounded on the result<span v-if="result?.compare_preset"> · {{ result.compare_preset.replace(/_/g, ' ') }}</span></div>
                </div>
              </div>
              <blockquote class="aia-narrative__text">
                {{ displayedSummary }}
                <span v-if="loading && streamStep === 'summarizing'" class="aia-cursor">▌</span>
              </blockquote>
            </section>
          </Transition>

          <!-- Follow-up questions -->
          <Transition name="slide-up">
            <section v-if="activeFollowUps.length" class="page-panel aia-followups">
              <div class="aia-panel-hd">
                <div class="aia-section-title">Follow-up Ideas</div>
                <div class="aia-section-sub">Click to use as the next question</div>
              </div>
              <div class="aia-followup-list">
                <button
                  v-for="item in activeFollowUps"
                  :key="item"
                  class="aia-followup-item"
                  @click="usePrompt(item)"
                >
                  <svg class="aia-fu-arrow" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/></svg>
                  {{ item }}
                </button>
              </div>
            </section>
          </Transition>

        </template>

      </div>
    </div>
  </div>
</template>

<style scoped>
/* ── Root ── */
.aia-root { background: var(--bg-body); }

/* ── Header ── */
.aia-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 18px;
  flex-wrap: wrap;
}
.aia-header__inner { display: flex; align-items: center; gap: 12px; }
.aia-header__icon {
  width: 38px; height: 38px;
  border-radius: 10px;
  background: var(--brand-dim);
  color: var(--brand);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.aia-header__title { font-size: 16px; font-weight: 700; color: var(--text-primary); letter-spacing: -0.3px; }
.aia-header__sub { font-size: 12px; color: var(--text-muted); margin-top: 1px; max-width: 500px; }
.aia-header__actions { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.aia-badge-count {
  background: var(--brand-dim); color: var(--brand);
  border-radius: 999px; padding: 1px 7px;
  font-size: 10px; font-weight: 700;
}

/* ── Saved reports drawer ── */
.aia-saved-panel { padding: 16px 18px; }
.aia-saved-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.aia-saved-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 10px;
}
.aia-saved-card {
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: rgba(255,255,255,0.02);
  cursor: pointer;
  transition: border-color var(--dur), background var(--dur);
  display: flex; flex-direction: column; gap: 6px;
}
.aia-saved-card:hover { border-color: var(--brand-ring); background: var(--brand-dim); }
.aia-saved-card__title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.aia-saved-card__time { font-size: 11px; color: var(--text-muted); }
.aia-saved-card__summary {
  font-size: 12px; color: var(--text-secondary); line-height: 1.4;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.aia-saved-card__actions { display: flex; gap: 6px; margin-top: 4px; }
.aia-btn-danger {
  padding: 3px 8px; font-size: 11px;
  background: transparent; color: var(--danger);
  border: 1px solid transparent; border-radius: var(--r-sm);
  cursor: pointer; font-family: inherit;
  transition: background var(--dur);
}
.aia-btn-danger:hover { background: var(--danger-bg); }

/* ── Input panel ── */
.aia-input-panel { padding: 18px; }
.aia-input-panel.is-collapsed { padding: 0; overflow: hidden; }

.aia-collapsed-bar {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 16px; flex-wrap: wrap;
}
.aia-collapsed-conn {
  font-size: 12px; font-weight: 600;
  color: var(--text-secondary); flex-shrink: 0;
}
.aia-collapsed-q {
  flex: 1; min-width: 0; font-size: 13px;
  color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.aia-collapsed-actions { display: flex; gap: 6px; flex-shrink: 0; }

.aia-ctx-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px; border: 1px solid var(--border);
  border-radius: var(--r); background: rgba(255,255,255,0.02);
  font-size: 12px; color: var(--text-secondary); flex-wrap: wrap;
}
.aia-badge-brand {
  padding: 2px 8px; border-radius: 999px;
  background: var(--brand-dim); color: var(--brand);
  font-size: 10px; font-weight: 700; letter-spacing: 0.05em;
  text-transform: uppercase; flex-shrink: 0;
}
.aia-ctx-hint { color: var(--text-muted); font-size: 11px; }
.aia-ctx-clear {
  margin-left: auto; background: none; border: none;
  color: var(--text-muted); cursor: pointer; padding: 2px;
  display: flex; align-items: center; border-radius: 4px;
  transition: color var(--dur);
}
.aia-ctx-clear:hover { color: var(--text-primary); }

.aia-form { display: flex; flex-direction: column; gap: 12px; }
.aia-form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.aia-field { display: flex; flex-direction: column; gap: 4px; }
.aia-label {
  font-size: 11px; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.07em;
  color: var(--text-muted);
}
.aia-select-wrap { position: relative; display: flex; align-items: center; }
.aia-driver-badge--inset {
  position: absolute; left: 10px; z-index: 1;
  pointer-events: none;
}
.aia-select-wrap .base-select.has-badge { padding-left: 32px; }

.aia-driver-badge {
  width: 20px; height: 20px; border-radius: 5px;
  background: var(--brand-dim); color: var(--brand);
  font-size: 9px; font-weight: 800;
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}

.aia-textarea { font-family: inherit; font-size: 13px; min-height: 68px; resize: vertical; }

.aia-prompts-section { display: flex; flex-direction: column; gap: 8px; }
.aia-prompts-group { display: flex; flex-direction: column; gap: 5px; }
.aia-prompts-label {
  font-size: 10px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-muted);
}
.aia-chips { display: flex; flex-wrap: wrap; gap: 5px; }
.aia-chip {
  padding: 4px 11px; border-radius: 999px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.02);
  color: var(--text-secondary); font-size: 12px; font-family: inherit;
  cursor: pointer; white-space: nowrap;
  transition: all var(--dur) var(--ease);
}
.aia-chip:hover { border-color: var(--brand-ring); background: var(--brand-dim); color: var(--brand); }

.aia-form-footer { display: flex; justify-content: flex-end; }
.aia-run-btn { padding: 9px 20px; font-size: 13px; font-weight: 600; gap: 7px; border-radius: 10px; }

/* ── Error ── */
.aia-error-bar {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px; border-radius: var(--r);
  background: var(--danger-bg); border: 1px solid rgba(232,128,128,0.2);
  color: var(--danger); font-size: 13px;
}
.aia-error-dismiss {
  margin-left: auto; background: none; border: none;
  color: var(--danger); cursor: pointer; display: flex;
  align-items: center; opacity: 0.7;
  transition: opacity var(--dur);
}
.aia-error-dismiss:hover { opacity: 1; }

/* ── Stepper ── */
.aia-stepper {
  padding: 16px 20px;
  display: flex; align-items: center; gap: 20px; flex-wrap: wrap;
}
.aia-stepper__steps { display: flex; align-items: center; gap: 0; }
.aia-step {
  display: flex; align-items: center; gap: 7px;
  font-size: 12px; font-weight: 500;
  color: var(--text-muted);
  transition: color var(--dur);
}
.aia-step.is-done { color: var(--brand); }
.aia-step.is-active { color: var(--text-primary); }
.aia-step__circle {
  width: 22px; height: 22px; border-radius: 50%;
  border: 1.5px solid currentColor;
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; font-weight: 700; flex-shrink: 0;
  transition: background var(--dur), border-color var(--dur);
}
.aia-step.is-done .aia-step__circle {
  background: var(--brand); border-color: var(--brand); color: var(--brand-fg);
}
.aia-step__label { white-space: nowrap; }
.aia-step__line {
  width: 36px; height: 1.5px;
  background: var(--border); margin: 0 8px;
  transition: background var(--dur);
}
.aia-step__line.is-done { background: var(--brand); }
.aia-stepper__msg { font-size: 12px; color: var(--text-muted); margin-left: auto; font-style: italic; }

/* ── Empty ── */
.aia-empty {
  display: flex; flex-direction: column;
  align-items: center; justify-content: center;
  gap: 10px; padding: 56px 24px; text-align: center;
}
.aia-empty__title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.aia-empty__sub { font-size: 13px; color: var(--text-muted); max-width: 400px; line-height: 1.6; }

/* ── Report cards ── */
.aia-report-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
}
.aia-report-card {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 12px 14px; border: 1px solid var(--border);
  border-radius: var(--r-lg); background: rgba(255,255,255,0.02);
}
.aia-report-card__dot {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--brand); flex-shrink: 0; margin-top: 5px;
}
.aia-report-card__text { font-size: 13px; color: var(--text-primary); line-height: 1.5; }

/* ── Panel shared ── */
.aia-panel-hd {
  display: flex; align-items: flex-start;
  justify-content: space-between; gap: 12px;
  padding: 14px 18px 10px;
}
.aia-panel-hd--border {
  border-bottom: 1px solid var(--border);
  padding-bottom: 14px; margin-bottom: 0;
}
.aia-section-title { font-size: 14px; font-weight: 700; color: var(--text-primary); display: flex; align-items: center; gap: 6px; }
.aia-section-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }

.aia-inline-badge {
  padding: 2px 7px; border-radius: 999px;
  background: var(--warning-bg); color: var(--warning);
  font-size: 10px; font-weight: 600; font-style: normal;
}
.aia-inline-badge--ok {
  background: var(--success-bg); color: var(--success);
}

/* ── SQL panel ── */
.aia-sql-panel { overflow: hidden; }
.aia-code {
  margin: 0 18px 16px;
  padding: 13px 15px; border-radius: var(--r);
  background: rgba(0,0,0,0.18); border: 1px solid var(--border);
  color: var(--text-primary); font-family: var(--mono);
  font-size: 12px; line-height: 1.7; overflow-x: auto; white-space: pre;
}
.aia-assumptions {
  display: flex; flex-wrap: wrap; gap: 6px;
  padding: 0 18px 14px;
}
.aia-assumption-tag {
  padding: 3px 9px; border-radius: 999px;
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
  font-size: 11px; color: var(--text-muted);
}

/* ── Table ── */
.aia-table-section { overflow: hidden; }
.aia-table-wrap { overflow-x: auto; }
.aia-table-empty { padding: 28px; text-align: center; font-size: 13px; color: var(--text-muted); }
.aia-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.aia-table thead th {
  padding: 8px 14px; text-align: left;
  font-size: 10.5px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--text-muted); background: rgba(255,255,255,0.02);
  border-bottom: 1px solid var(--border); white-space: nowrap;
}
.aia-table tbody td {
  padding: 9px 14px; border-bottom: 1px solid var(--border);
  color: var(--text-primary); font-family: var(--mono);
  font-size: 12px; max-width: 260px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.aia-table tbody tr:last-child td { border-bottom: none; }
.aia-table tbody tr:hover td { background: rgba(255,255,255,0.02); }
.aia-td-null { color: var(--text-muted); font-style: italic; }

/* ── Narrative ── */
.aia-narrative { overflow: hidden; }
.aia-narrative__text {
  margin: 0 18px 16px;
  padding: 14px 16px;
  border-left: 3px solid var(--brand);
  border-radius: 0 var(--r) var(--r) 0;
  background: rgba(255,255,255,0.02);
  font-size: 14px; line-height: 1.75;
  color: var(--text-primary); font-style: normal;
  min-height: 48px;
}
.aia-cursor {
  display: inline-block; color: var(--brand);
  animation: blink 0.8s step-start infinite;
  font-weight: 300;
}
@keyframes blink { 50% { opacity: 0; } }
.aia-typing-cursor {
  display: inline-block; color: var(--brand);
  animation: blink 0.8s step-start infinite;
  font-weight: 300; margin-left: 4px;
}

/* ── Follow-ups ── */
.aia-followups { overflow: hidden; padding-bottom: 8px; }
.aia-followup-list { display: flex; flex-direction: column; gap: 2px; padding: 0 10px 8px; }
.aia-followup-item {
  display: flex; align-items: flex-start; gap: 8px;
  padding: 9px 10px; border-radius: var(--r);
  border: none; background: transparent;
  color: var(--text-secondary); font-size: 12.5px;
  font-family: inherit; cursor: pointer; text-align: left;
  transition: all var(--dur) var(--ease); line-height: 1.4;
}
.aia-followup-item:hover { background: var(--brand-dim); color: var(--brand); }
.aia-fu-arrow {
  flex-shrink: 0; margin-top: 1px; opacity: 0.35;
  transition: opacity var(--dur), transform var(--dur);
}
.aia-followup-item:hover .aia-fu-arrow { opacity: 1; transform: translateX(3px); }

/* ── Transitions ── */
.slide-up-enter-active { transition: opacity 0.28s ease, transform 0.28s ease; }
.slide-up-enter-from { opacity: 0; transform: translateY(10px); }

.slide-down-enter-active, .slide-down-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.slide-down-enter-from, .slide-down-leave-to { opacity: 0; transform: translateY(-8px); }

.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

/* ── Responsive ── */
@media (max-width: 860px) {
  .aia-form-row { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .aia-header { flex-direction: column; align-items: flex-start; }
  .aia-stepper { flex-direction: column; align-items: flex-start; gap: 10px; }
  .aia-stepper__msg { margin-left: 0; }
}
</style>
