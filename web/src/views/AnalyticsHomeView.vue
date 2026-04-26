<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useSavedQueries, type SavedQuery } from '@/composables/useSavedQueries'
import { useAuth } from '@/composables/useAuth'
import { formatServerTimestamp } from '@/utils/datetime'

interface AIReport {
  id: number
  connection_id: number
  title: string
  summary: string
  chart_type: string
  created_at: string
}

const router = useRouter()
const { hasAnyPermission } = useAuth()
const { connections, fetchConnections } = useConnections()
const { queries, fetchAll: fetchSavedQueries } = useSavedQueries()

const loading = ref(false)
const pinnedReports = ref<AIReport[]>([])

const canQuery = computed(() => hasAnyPermission(['connections.view', 'query.execute', 'schema.browse']))
const canUseAI = computed(() => hasAnyPermission(['ai.use']))
const canSavedQueries = computed(() => hasAnyPermission(['savedqueries.manage']))
const canSchedule = computed(() => hasAnyPermission(['schedules.manage']))
const canNotifications = computed(() => hasAnyPermission(['notifications.view']))
const canDashboard = computed(() => hasAnyPermission(['connections.view']))
const canER = computed(() => hasAnyPermission(['schema.browse']))

const analyticsCards = computed(() => [
  {
    title: 'SQL Studio',
    desc: 'Run SQL, inspect results, then turn the output into reusable analysis.',
    route: 'data',
    badge: 'SQL',
    tone: 'brand',
    stat: `${connections.value.length} connection${connections.value.length === 1 ? '' : 's'}`,
    enabled: canQuery.value,
  },
  {
    title: 'Saved Queries',
    desc: 'Treat saved SQL as your dataset library for dashboards, alerts, and AI follow-up.',
    route: 'saved-queries',
    badge: 'LIB',
    tone: 'violet',
    stat: `${queries.value.length} saved quer${queries.value.length === 1 ? 'y' : 'ies'}`,
    enabled: canSavedQueries.value,
  },
  {
    title: 'Dashboards',
    desc: 'Assemble saved queries into chart blocks and lightweight BI pages.',
    route: 'dashboards',
    badge: 'BI',
    tone: 'violet',
    stat: 'Chart blocks from saved SQL',
    enabled: canSavedQueries.value,
  },
  {
    title: 'AI Analytics',
    desc: 'Ask questions in natural language and convert results into pinned reports.',
    route: 'ai-analytics',
    badge: 'AI',
    tone: 'amber',
    stat: `${pinnedReports.value.length} pinned report${pinnedReports.value.length === 1 ? '' : 's'}`,
    enabled: canUseAI.value,
  },
  {
    title: 'Operations Overview',
    desc: 'Track footprint, slow-query pressure, and environment-level health in one place.',
    route: 'dashboard',
    badge: 'OPS',
    tone: 'emerald',
    stat: 'Connection overview',
    enabled: canDashboard.value,
  },
])

const workflowCards = computed(() => [
  {
    title: 'Scheduler',
    desc: 'Run recurring query checks or AI summaries like lightweight BI jobs.',
    route: 'scheduler',
    enabled: canSchedule.value,
  },
  {
    title: 'Notifications',
    desc: 'Route summary output, failures, and approvals to Slack, Telegram, Discord, or webhook.',
    route: 'notifications',
    enabled: canNotifications.value,
  },
  {
    title: 'ER Diagram',
    desc: 'Understand joins and table relationships before building saved analysis.',
    route: 'er',
    enabled: canER.value,
  },
])

const recentSavedQueries = computed(() =>
  [...queries.value]
    .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    .slice(0, 6),
)

const recentReports = computed(() =>
  [...pinnedReports.value]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 6),
)

function openRoute(name: string) {
  router.push({ name })
}

function connectionNameByID(connID: number) {
  return connections.value.find((item) => item.id === connID)?.name ?? `Connection #${connID}`
}

async function loadReports() {
  if (!canUseAI.value) {
    pinnedReports.value = []
    return
  }
  try {
    const { data } = await axios.get<AIReport[]>('/api/ai/reports')
    pinnedReports.value = data ?? []
  } catch {
    pinnedReports.value = []
  }
}

async function loadAll() {
  loading.value = true
  try {
    await fetchConnections()
    if (canSavedQueries.value) {
      await fetchSavedQueries()
    }
    await loadReports()
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack analytics-home">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Analytics</div>
            <div class="page-title">Analytics Workspace</div>
            <div class="page-subtitle">
              The BI layer for Anveesa Nias: build reusable queries, ask AI questions, pin reports, and route insights into schedules and notifications.
            </div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadAll">
              Refresh
            </button>
          </div>
        </section>

        <section class="analytics-grid">
          <button
            v-for="card in analyticsCards"
            :key="card.title"
            class="page-panel analytics-card"
            :class="`analytics-card--${card.tone}`"
            :disabled="!card.enabled"
            @click="card.enabled && openRoute(card.route)"
          >
            <div class="analytics-card__head">
              <span class="analytics-card__badge">{{ card.badge }}</span>
              <span class="analytics-card__stat">{{ card.stat }}</span>
            </div>
            <div class="analytics-card__title">{{ card.title }}</div>
            <div class="analytics-card__desc">{{ card.desc }}</div>
          </button>
        </section>

        <section class="analytics-columns">
          <div class="page-panel analytics-panel">
            <div class="analytics-panel__head">
              <div>
                <div class="analytics-panel__title">Recent Saved Queries</div>
                <div class="analytics-panel__sub">Reusable SQL assets that already behave like datasets.</div>
              </div>
              <button v-if="canSavedQueries" class="base-btn base-btn--ghost base-btn--xs" @click="openRoute('saved-queries')">Open Library</button>
            </div>
            <div v-if="loading" class="analytics-empty">Loading analytics assets…</div>
            <div v-else-if="!canSavedQueries" class="analytics-empty">Saved query access is not enabled for this account.</div>
            <div v-else-if="recentSavedQueries.length === 0" class="analytics-empty">No saved queries yet. Start in SQL Studio, then save your best queries as reusable analysis.</div>
            <div v-else class="analytics-list">
              <button v-for="query in recentSavedQueries" :key="query.id" class="analytics-list__item" @click="openRoute('saved-queries')">
                <div class="analytics-list__title">{{ query.name }}</div>
                <div class="analytics-list__meta">
                  <span>{{ query.conn_id ? connectionNameByID(query.conn_id) : 'Any connection' }}</span>
                  <span>{{ formatServerTimestamp(query.updated_at) }}</span>
                </div>
              </button>
            </div>
          </div>

          <div class="page-panel analytics-panel">
            <div class="analytics-panel__head">
              <div>
                <div class="analytics-panel__title">Dashboard Builder</div>
                <div class="analytics-panel__sub">Turn saved queries into chart blocks and reusable dashboard layouts.</div>
              </div>
              <button v-if="canSavedQueries" class="base-btn base-btn--ghost base-btn--xs" @click="openRoute('dashboards')">Open Dashboards</button>
            </div>
            <div class="analytics-empty">Build table, KPI, bar, and line blocks from saved queries in the dashboard builder.</div>
          </div>

          <div class="page-panel analytics-panel">
            <div class="analytics-panel__head">
              <div>
                <div class="analytics-panel__title">Pinned AI Reports</div>
                <div class="analytics-panel__sub">Executive-style summaries that can evolve into dashboards and scheduled reports.</div>
              </div>
              <button v-if="canUseAI" class="base-btn base-btn--ghost base-btn--xs" @click="openRoute('ai-analytics')">Open AI</button>
            </div>
            <div v-if="loading" class="analytics-empty">Loading pinned reports…</div>
            <div v-else-if="!canUseAI" class="analytics-empty">AI Analytics is not enabled for this account.</div>
            <div v-else-if="recentReports.length === 0" class="analytics-empty">No pinned AI reports yet. Ask a question in AI Analytics, then pin the result for later reuse.</div>
            <div v-else class="analytics-list">
              <button v-for="report in recentReports" :key="report.id" class="analytics-list__item" @click="openRoute('ai-analytics')">
                <div class="analytics-list__title">{{ report.title }}</div>
                <div class="analytics-list__meta">
                  <span>{{ connectionNameByID(report.connection_id) }}</span>
                  <span>{{ formatServerTimestamp(report.created_at) }}</span>
                </div>
                <div class="analytics-report__summary">{{ report.summary }}</div>
              </button>
            </div>
          </div>
        </section>

        <section class="page-panel analytics-panel analytics-panel--wide">
          <div class="analytics-panel__head">
            <div>
              <div class="analytics-panel__title">From Query Tool To BI Surface</div>
              <div class="analytics-panel__sub">The current product is already close to a Redash-style workflow. These are the core surfaces that now work together.</div>
            </div>
          </div>
          <div class="analytics-workflow">
            <button
              v-for="card in workflowCards"
              :key="card.title"
              class="analytics-workflow__item"
              :disabled="!card.enabled"
              @click="card.enabled && openRoute(card.route)"
            >
              <div class="analytics-workflow__title">{{ card.title }}</div>
              <div class="analytics-workflow__desc">{{ card.desc }}</div>
            </button>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.analytics-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.analytics-card {
  text-align: left;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
  border: 1px solid var(--border);
  background: linear-gradient(180deg, var(--bg-surface), var(--bg-elevated));
  transition: transform .14s ease, border-color .14s ease, box-shadow .14s ease;
}

.analytics-card:not(:disabled):hover {
  transform: translateY(-1px);
  border-color: var(--border-2);
  box-shadow: var(--shadow-md);
}

.analytics-card:disabled,
.analytics-workflow__item:disabled {
  opacity: .55;
  cursor: default;
}

.analytics-card__head,
.analytics-list__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.analytics-card__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 36px;
  padding: 4px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .12em;
}

.analytics-card__stat {
  font-size: 11px;
  color: var(--text-muted);
}

.analytics-card__title,
.analytics-panel__title,
.analytics-list__title,
.analytics-workflow__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.analytics-card__desc,
.analytics-panel__sub,
.analytics-empty,
.analytics-workflow__desc,
.analytics-report__summary {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary);
}

.analytics-card--brand .analytics-card__badge {
  background: var(--brand-dim);
  color: var(--brand);
}

.analytics-card--violet .analytics-card__badge {
  background: rgba(124, 58, 237, 0.12);
  color: #7c3aed;
}

.analytics-card--amber .analytics-card__badge {
  background: rgba(217, 119, 6, 0.12);
  color: #d97706;
}

.analytics-card--emerald .analytics-card__badge {
  background: rgba(5, 150, 105, 0.12);
  color: #059669;
}

.analytics-columns {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.analytics-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 18px;
}

.analytics-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.analytics-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.analytics-list__item,
.analytics-workflow__item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  border-radius: var(--r);
  padding: 14px;
  text-align: left;
  font-family: inherit;
  cursor: pointer;
  transition: border-color .14s ease, background .14s ease, transform .14s ease;
}

.analytics-list__item:hover,
.analytics-workflow__item:hover {
  border-color: var(--border-2);
  background: var(--bg-surface);
  transform: translateY(-1px);
}

.analytics-list__meta {
  justify-content: flex-start;
  flex-wrap: wrap;
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 4px;
}

.analytics-report__summary {
  margin-top: 8px;
}

.analytics-workflow {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

@media (max-width: 1300px) {
  .analytics-columns {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 1180px) {
  .analytics-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 860px) {
  .analytics-columns,
  .analytics-workflow,
  .analytics-grid {
    grid-template-columns: 1fr;
  }

  .analytics-panel__head {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
