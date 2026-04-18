<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useConnections } from '@/composables/useConnections'

const router = useRouter()
const { connections } = useConnections()

const providers = [
  { key: 'postgres', label: 'PostgreSQL', sub: 'Open-source relational DB', badge: 'PG' },
  { key: 'mysql',    label: 'MySQL',      sub: 'Popular web database',      badge: 'MY' },
  { key: 'sqlite',   label: 'SQLite',     sub: 'Embedded file-based DB',    badge: 'SQ' },
  { key: 'mssql',    label: 'SQL Server', sub: 'Microsoft enterprise DB',   badge: 'MS' },
]

const driverCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const c of connections.value) {
    counts[c.driver] = (counts[c.driver] ?? 0) + 1
  }
  return counts
})
</script>

<template>
  <div class="wv-root">
    <div class="wv-scroll">
      <div class="wv-inner">
        <!-- Hero -->
        <div class="wv-hero">
          <div class="wv-icon">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <ellipse cx="12" cy="5" rx="9" ry="3"/>
              <path d="M3 5V19A9 3 0 0 0 21 19V5"/>
              <path d="M3 12A9 3 0 0 0 21 12"/>
            </svg>
          </div>
          <h1 class="wv-title">
            {{ connections.length ? 'Select a connection to start' : 'Welcome to Anveesa Nias' }}
          </h1>
          <p class="wv-sub">
            {{ connections.length
              ? 'Choose a connection from the sidebar, or add a new one below.'
              : 'A fast, local-first database studio. Connect to PostgreSQL, MySQL, SQLite or SQL Server.' }}
          </p>
          <div class="wv-actions">
            <button class="base-btn base-btn--primary" @click="router.push({ name: 'connections' })">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              New Connection
            </button>
            <button v-if="connections.length" class="base-btn base-btn--ghost" @click="router.push({ name: 'query' })">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
              Open Query Editor
            </button>
          </div>
        </div>

        <!-- Provider cards -->
        <div class="wv-section">
          <div class="wv-section__label">Supported databases</div>
          <div class="wv-providers">
            <div v-for="p in providers" :key="p.key" class="wv-pcard">
              <div class="wv-pcard__icon" :class="`wv-pcard__icon--${p.key}`">{{ p.badge }}</div>
              <div class="wv-pcard__text">
                <span class="wv-pcard__name">{{ p.label }}</span>
                <span class="wv-pcard__sub">{{ p.sub }}</span>
              </div>
              <span v-if="driverCounts[p.key]" class="badge badge--success wv-pcard__count">
                {{ driverCounts[p.key] }}
              </span>
            </div>
          </div>
        </div>

        <!-- Quick nav (only when connections exist) -->
        <div v-if="connections.length" class="wv-section">
          <div class="wv-section__label">Quick access</div>
          <div class="wv-nav-grid">
            <button class="wv-nav-card" @click="router.push({ name: 'query' })">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="wv-nav-card__ico wv-nav-card__ico--brand"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
              <span class="wv-nav-card__label">Query Editor</span>
              <span class="wv-nav-card__sub">Write &amp; run SQL</span>
            </button>
            <button class="wv-nav-card" @click="router.push({ name: 'schema' })">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="wv-nav-card__ico wv-nav-card__ico--purple"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>
              <span class="wv-nav-card__label">Schema Browser</span>
              <span class="wv-nav-card__sub">Inspect tables &amp; columns</span>
            </button>
            <button class="wv-nav-card" @click="router.push({ name: 'data' })">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="wv-nav-card__ico wv-nav-card__ico--gold"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              <span class="wv-nav-card__label">Data Browser</span>
              <span class="wv-nav-card__sub">Browse &amp; export data</span>
            </button>
            <button class="wv-nav-card" @click="router.push({ name: 'connections' })">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="wv-nav-card__ico wv-nav-card__ico--red"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
              <span class="wv-nav-card__label">Connections</span>
              <span class="wv-nav-card__sub">Manage {{ connections.length }} connection{{ connections.length !== 1 ? 's' : '' }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ── Root fills the entire main-area slot ── */
.wv-root {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ── Scrollable layer — flex column so margin:auto centering works ── */
.wv-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 48px 32px;
}

/* ── Inner card — margin:auto centers vertically without clipping on scroll ── */
.wv-inner {
  width: 100%;
  max-width: 600px;
  margin: auto 0;
  display: flex;
  flex-direction: column;
  gap: 40px;
}

/* ── Hero ── */
.wv-hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 12px;
}

.wv-icon {
  width: 68px;
  height: 68px;
  border-radius: var(--r-lg);
  background: var(--bg-surface);
  border: 1px solid var(--border-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--brand);
  box-shadow: var(--shadow-sm);
  margin-bottom: 4px;
}

.wv-title {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
  letter-spacing: -0.6px;
  line-height: 1.2;
  margin: 0;
}

.wv-sub {
  font-size: 14px;
  color: var(--text-secondary);
  max-width: 380px;
  line-height: 1.75;
  margin: 0;
}

.wv-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: center;
  margin-top: 8px;
}

/* ── Section ── */
.wv-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.wv-section__label {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--text-muted);
}

/* ── Provider cards ── */
.wv-providers {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}

.wv-pcard {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 15px;
  border-radius: var(--r);
  border: 1.5px solid var(--border);
  background: var(--bg-surface);
  transition: border-color var(--dur), box-shadow var(--dur);
}
.wv-pcard:hover {
  border-color: var(--border-2);
  box-shadow: var(--shadow-sm);
}

.wv-pcard__icon {
  width: 34px;
  height: 34px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  flex-shrink: 0;
  font-family: var(--mono);
}
.wv-pcard__icon--postgres { background: var(--pg-bg);     color: var(--pg); }
.wv-pcard__icon--mysql    { background: var(--mysql-bg);  color: var(--mysql); }
.wv-pcard__icon--sqlite   { background: var(--sqlite-bg); color: var(--sqlite); }
.wv-pcard__icon--mssql    { background: var(--mssql-bg);  color: var(--mssql); }

.wv-pcard__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.wv-pcard__name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.wv-pcard__sub {
  font-size: 11px;
  color: var(--text-muted);
}
.wv-pcard__count {
  margin-left: auto;
  flex-shrink: 0;
}

/* ── Quick-access nav grid ── */
.wv-nav-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}

.wv-nav-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  padding: 18px;
  background: var(--bg-surface);
  border: 1.5px solid var(--border);
  border-radius: var(--r);
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  transition: border-color var(--dur), box-shadow var(--dur);
}
.wv-nav-card:hover {
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.wv-nav-card__ico { flex-shrink: 0; }
.wv-nav-card__ico--brand  { color: var(--brand); }
.wv-nav-card__ico--purple { color: #a78bfa; }
.wv-nav-card__ico--gold   { color: #f2c97d; }
.wv-nav-card__ico--red    { color: #e88080; }

.wv-nav-card__label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.wv-nav-card__sub {
  font-size: 11.5px;
  color: var(--text-muted);
  line-height: 1.4;
}
</style>
