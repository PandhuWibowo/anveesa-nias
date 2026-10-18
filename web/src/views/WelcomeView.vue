<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useConnections, type Connection } from '@/composables/useConnections'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { NAV_GROUPS, navKey, type NavItem } from '@/navigation'
import { DRIVER_COLOR, DRIVER_NAME, connectionRoute } from '@/utils/drivers'
import NavIcon from '@/components/ui/NavIcon.vue'
import DriverIcon from '@/components/ui/DriverIcon.vue'

defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const router = useRouter()
const { user, hasAnyPermission } = useAuth()
const { connections, loading } = useConnections()
const palette = useCommandPalette()

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
const modKey = isMac ? '⌘' : 'Ctrl'

const greeting = computed(() => {
  const h = new Date().getHours()
  const part = h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
  return user.value?.username ? `${part}, ${user.value.username}` : part
})

const groups = computed(() =>
  NAV_GROUPS.map((g) => ({
    ...g,
    items: g.items.filter((i) => !i.permissionsAny?.length || hasAnyPermission(i.permissionsAny)),
  })).filter((g) => g.items.length),
)

const recentPages = computed(() => {
  const byKey = new Map(groups.value.flatMap((g) => g.items.map((i) => [navKey(i), { ...i, group: g.label }] as const)))
  return palette.recent.value.map((k) => byKey.get(k)).filter((i): i is NavItem & { group: string } => !!i).slice(0, 4)
})

const canManageConnections = computed(() => hasAnyPermission(['connections.view']))
const shownConnections = computed(() => connections.value.slice(0, 8))

function open(item: NavItem) {
  router.push({ name: item.name, query: item.query })
}

function openConnection(c: Connection) {
  emit('set-conn', c.id)
  router.push({ name: connectionRoute(c.driver) })
}
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="home">
        <header class="home__head">
          <h1 class="home__title">{{ greeting }}</h1>
          <button class="home__search" type="button" @click="palette.show()">
            <NavIcon name="search" :size="18" />
            <span class="home__search-text">Jump to a page, connection, or action</span>
            <kbd>{{ modKey }}</kbd><kbd>K</kbd>
          </button>
        </header>

        <section v-if="recentPages.length" class="home__section">
          <h2 class="home__h2">Pick up where you left off</h2>
          <div class="home__recent">
            <button v-for="item in recentPages" :key="navKey(item)" class="home__recent-item" type="button" @click="open(item)">
              <span class="home__recent-icon"><NavIcon :name="item.icon" :size="18" /></span>
              <span class="home__recent-text">
                <span class="home__recent-label">{{ item.label }}</span>
                <span class="home__recent-group">{{ item.group }}</span>
              </span>
            </button>
          </div>
        </section>

        <section class="home__section">
          <div class="home__section-head">
            <h2 class="home__h2">Connections</h2>
            <router-link v-if="canManageConnections && connections.length" :to="{ name: 'connections' }" class="home__link">
              Manage all {{ connections.length }}
            </router-link>
          </div>

          <div v-if="loading && !connections.length" class="home__conns">
            <div v-for="n in 3" :key="n" class="skeleton home__conn-skel" />
          </div>

          <div v-else-if="!connections.length" class="home__empty">
            <p class="home__empty-title">No connections yet</p>
            <p class="home__empty-text">
              {{ canManageConnections
                ? 'Add a database, cache, queue, or bucket to start browsing and querying it here.'
                : 'Ask an administrator to share a connection with you.' }}
            </p>
            <button v-if="canManageConnections" class="base-btn base-btn--primary" type="button" @click="router.push({ name: 'connections', query: { new: '1' } })">
              <NavIcon name="plus" :size="15" /> Add connection
            </button>
          </div>

          <div v-else class="home__conns">
            <button
              v-for="c in shownConnections"
              :key="c.id"
              class="home__conn"
              :class="{ 'is-active': c.id === activeConnId }"
              type="button"
              @click="openConnection(c)"
            >
              <span class="home__driver" :style="{ background: DRIVER_COLOR[c.driver] ?? '#4b5563' }">
                <DriverIcon :driver="c.driver" :size="16" />
              </span>
              <span class="home__conn-text">
                <span class="home__conn-name">{{ c.name }}</span>
                <span class="home__conn-sub">{{ DRIVER_NAME[c.driver] ?? c.driver }}<template v-if="c.host"> · {{ c.host }}</template></span>
              </span>
              <span v-if="c.id === activeConnId" class="badge badge--success">Active</span>
            </button>
            <button
              v-if="canManageConnections"
              class="home__conn home__conn--add"
              type="button"
              @click="router.push({ name: 'connections', query: { new: '1' } })"
            >
              <span class="home__driver home__driver--add"><NavIcon name="plus" :size="16" /></span>
              <span class="home__conn-name">Add connection</span>
            </button>
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<style scoped>
.home {
  max-width: 1040px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 40px;
  padding-top: 12px;
}

.home__head {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.home__title {
  font-family: var(--display);
  font-variation-settings: 'SOFT' 50;
  font-size: 32px;
  font-weight: 500;
  letter-spacing: -0.01em;
  line-height: 1.15;
  color: var(--text-primary);
}
.home__search {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 48px;
  padding: 0 14px 0 16px;
  max-width: 620px;
  border: 1px solid var(--border-2);
  border-radius: var(--r);
  background: var(--bg-surface);
  color: var(--text-muted);
  font: inherit;
  font-size: 15px;
  text-align: left;
  cursor: text;
  transition: border-color var(--dur) var(--ease), box-shadow var(--dur) var(--ease);
}
.home__search:hover { border-color: var(--brand); box-shadow: 0 0 0 3px var(--brand-dim); }
.home__search-text { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.home__search kbd + kbd { margin-left: -6px; }

.home__section { display: flex; flex-direction: column; gap: 12px; }
.home__section-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.home__h2 {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.home__link {
  font-size: 13px;
  color: var(--brand);
  text-decoration: none;
}
.home__link:hover { text-decoration: underline; }

/* Recent */
.home__recent {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  gap: 8px;
}
.home__recent-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--dur) var(--ease), background var(--dur) var(--ease);
}
.home__recent-item:hover { border-color: var(--border-2); background: var(--bg-hover-strong); }
.home__recent-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--brand-dim);
  color: var(--brand);
  flex-shrink: 0;
}
.home__recent-text { min-width: 0; display: flex; flex-direction: column; line-height: 1.3; }
.home__recent-label { font-size: 14px; font-weight: 500; }
.home__recent-group { font-size: 12.5px; color: var(--text-muted); }

/* Connections */
.home__conns {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 8px;
}
.home__conn {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 60px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--dur) var(--ease), background var(--dur) var(--ease);
}
.home__conn:hover { border-color: var(--border-2); background: var(--bg-hover-strong); }
.home__conn.is-active { border-color: color-mix(in srgb, var(--brand) 45%, var(--border)); }
.home__conn--add { border-style: dashed; color: var(--text-secondary); }
.home__driver {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  flex-shrink: 0;
}
.home__driver--add { background: var(--bg-hover-strong); color: var(--text-muted); }
.home__conn-text { flex: 1; min-width: 0; display: flex; flex-direction: column; line-height: 1.3; }
.home__conn-name,
.home__conn-sub { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.home__conn-name { font-size: 14px; font-weight: 500; }
.home__conn-sub { font-size: 12.5px; color: var(--text-muted); }
.home__conn-skel { height: 60px; border-radius: var(--r); }

.home__empty {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 24px;
  border: 1px dashed var(--border-2);
  border-radius: var(--r-lg);
}
.home__empty-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.home__empty-text { font-size: 14px; color: var(--text-secondary); margin-bottom: 10px; max-width: 520px; }

@media (max-width: 720px) {
  .home { gap: 32px; padding-top: 0; }
  .home__title { font-size: 22px; }
  .home__search kbd { display: none; }
}
</style>
