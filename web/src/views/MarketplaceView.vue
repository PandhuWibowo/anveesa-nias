<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'

interface MarketEnv { key: string; value: string; description: string }
interface MarketApp {
  id: string
  name: string
  category: string
  description: string
  icon: string
  website: string
  source: string
  env: MarketEnv[]
  compose: string
}
interface Install {
  id: number
  app_id: string
  app_name: string
  host_id: number
  host_name: string
  stack_name: string
  installed_at: string
}
interface Tile { id: number; name: string; icon: string; url: string }
interface CatalogSrc { id: number; name: string; url: string; enabled: boolean }
interface CustomApp { id: number; slug: string; name: string; category: string; description: string; icon: string; website: string; compose: string; env: MarketEnv[] }
interface Host { id: number; name: string; ssh_host: string }

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['marketplace.manage']))

const tab = ref<'apps' | 'installed' | 'tools' | 'sources'>('apps')
const apps = ref<MarketApp[]>([])
const installs = ref<Install[]>([])
const tiles = ref<Tile[]>([])
const catalogs = ref<CatalogSrc[]>([])
const customApps = ref<CustomApp[]>([])
const hosts = ref<Host[]>([])
const loading = ref(false)
const search = ref('')
const category = ref('All')

const categories = computed(() => ['All', ...Array.from(new Set(apps.value.map((a) => a.category))).sort()])
const filteredApps = computed(() => {
  const q = search.value.trim().toLowerCase()
  return apps.value.filter(
    (a) =>
      (category.value === 'All' || a.category === category.value) &&
      (!q || a.name.toLowerCase().includes(q) || a.description.toLowerCase().includes(q)),
  )
})

// ── Loaders ─────────────────────────────────────────────────────
async function loadCatalog() {
  loading.value = true
  try {
    const { data } = await axios.get<{ apps: MarketApp[] }>('/api/marketplace/catalog')
    apps.value = data.apps || []
  } catch {
    toast.error('Failed to load catalog')
  } finally {
    loading.value = false
  }
}
async function loadInstalls() {
  try { installs.value = (await axios.get('/api/marketplace/installs')).data || [] } catch {}
}
async function loadTiles() {
  try { tiles.value = (await axios.get('/api/marketplace/tiles')).data || [] } catch {}
}
async function loadCatalogs() {
  try { catalogs.value = (await axios.get('/api/marketplace/catalogs')).data || [] } catch {}
}
async function loadCustomApps() {
  try { customApps.value = (await axios.get('/api/marketplace/custom-apps')).data || [] } catch {}
}
async function loadHosts() {
  try { hosts.value = (await axios.get('/api/docker/hosts')).data || [] } catch {}
}

// ── Install ─────────────────────────────────────────────────────
const showInstall = ref(false)
const installApp = ref<MarketApp | null>(null)
const installHostId = ref<number | null>(null)
const installStack = ref('')
const installEnv = ref<Record<string, string>>({})
const installing = ref(false)
const installOutput = ref('')
const showCompose = ref(false)

function openInstall(app: MarketApp) {
  installApp.value = app
  installStack.value = app.id
  installEnv.value = Object.fromEntries(app.env.map((e) => [e.key, e.value]))
  installHostId.value = hosts.value[0]?.id ?? null
  installOutput.value = ''
  showCompose.value = false
  showInstall.value = true
}
async function submitInstall() {
  if (!installApp.value || installHostId.value === null) {
    toast.error('Pick a host to deploy to')
    return
  }
  installing.value = true
  installOutput.value = 'Deploying…\n'
  try {
    const { data } = await axios.post<{ ok: boolean; output: string; error: string }>('/api/marketplace/install', {
      app_id: installApp.value.id,
      host_id: installHostId.value,
      stack_name: installStack.value.trim(),
      env: installEnv.value,
    })
    installOutput.value = data.output || '(no output)'
    if (data.error) {
      installOutput.value += `\n\nERROR: ${data.error}`
      toast.error('Install failed')
    } else {
      toast.success(`${installApp.value.name} installed`)
      await loadInstalls()
    }
  } catch (e: any) {
    installOutput.value += `\n\nERROR: ${e?.response?.data?.error || 'request failed'}`
    toast.error('Install failed')
  } finally {
    installing.value = false
  }
}
async function uninstall(i: Install) {
  const ok = await confirm(`Uninstall "${i.app_name}" from ${i.host_name}? This stops and removes its containers.`, 'Uninstall')
  if (!ok) return
  try {
    await axios.post('/api/marketplace/uninstall', { id: i.id })
    toast.success('Uninstalled')
    await loadInstalls()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Uninstall failed')
  }
}

// ── Tiles ───────────────────────────────────────────────────────
const showTile = ref(false)
const tileForm = ref({ name: '', icon: '🔗', url: '' })
function launch(t: Tile) {
  window.open(t.url, '_blank', 'noopener')
}
async function addTile() {
  if (!tileForm.value.name.trim() || !tileForm.value.url.trim()) {
    toast.error('Name and URL are required')
    return
  }
  try {
    await axios.post('/api/marketplace/tiles', tileForm.value)
    showTile.value = false
    tileForm.value = { name: '', icon: '🔗', url: '' }
    await loadTiles()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to add tile')
  }
}
async function deleteTile(t: Tile) {
  if (!(await confirm(`Remove tile "${t.name}"?`, 'Remove tile'))) return
  await axios.delete(`/api/marketplace/tiles/${t.id}`)
  await loadTiles()
}

// ── Sources ─────────────────────────────────────────────────────
const showCatalog = ref(false)
const catalogForm = ref({ name: '', url: '' })
async function addCatalog() {
  if (!catalogForm.value.url.trim()) {
    toast.error('Catalog URL is required')
    return
  }
  try {
    await axios.post('/api/marketplace/catalogs', catalogForm.value)
    showCatalog.value = false
    catalogForm.value = { name: '', url: '' }
    await loadCatalogs()
    await loadCatalog()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to add catalog')
  }
}
async function deleteCatalog(c: CatalogSrc) {
  if (!(await confirm(`Remove catalog "${c.name}"?`, 'Remove catalog'))) return
  await axios.delete(`/api/marketplace/catalogs/${c.id}`)
  await loadCatalogs()
  await loadCatalog()
}

const showCustomApp = ref(false)
const customForm = ref({ name: '', category: 'Custom', icon: '📦', description: '', website: '', compose: '', env: '' })
async function addCustomApp() {
  if (!customForm.value.name.trim() || !customForm.value.compose.trim()) {
    toast.error('Name and compose are required')
    return
  }
  // env textarea: KEY=default per line
  const env = customForm.value.env
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const i = l.indexOf('=')
      return i >= 0 ? { key: l.slice(0, i), value: l.slice(i + 1), description: '' } : { key: l, value: '', description: '' }
    })
  try {
    await axios.post('/api/marketplace/custom-apps', { ...customForm.value, env })
    showCustomApp.value = false
    customForm.value = { name: '', category: 'Custom', icon: '📦', description: '', website: '', compose: '', env: '' }
    await loadCustomApps()
    await loadCatalog()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to add app')
  }
}
async function deleteCustomApp(a: CustomApp) {
  if (!(await confirm(`Remove custom app "${a.name}"?`, 'Remove app'))) return
  await axios.delete(`/api/marketplace/custom-apps/${a.id}`)
  await loadCustomApps()
  await loadCatalog()
}

onMounted(() => {
  loadCatalog()
  loadInstalls()
  loadTiles()
  loadCatalogs()
  loadCustomApps()
  loadHosts()
})
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Marketplace</div>
            <div class="page-subtitle">One-click deploy self-hosted apps to your servers, and pin tools into Nias.</div>
          </div>
        </section>

        <div class="page-tabs">
          <button :class="['page-tab', { 'is-active': tab === 'apps' }]" @click="tab = 'apps'">Apps <span class="mk-count">{{ apps.length }}</span></button>
          <button :class="['page-tab', { 'is-active': tab === 'installed' }]" @click="tab = 'installed'">Installed <span class="mk-count">{{ installs.length }}</span></button>
          <button :class="['page-tab', { 'is-active': tab === 'tools' }]" @click="tab = 'tools'">Tools <span class="mk-count">{{ tiles.length }}</span></button>
          <button :class="['page-tab', { 'is-active': tab === 'sources' }]" @click="tab = 'sources'">Sources</button>
        </div>

        <!-- APPS -->
        <template v-if="tab === 'apps'">
          <div class="mk-bar">
            <input v-model="search" class="base-input mk-search" type="search" placeholder="Search apps…" />
            <div class="mk-cats">
              <button v-for="c in categories" :key="c" :class="['mk-cat', { 'mk-cat--on': category === c }]" @click="category = c">{{ c }}</button>
            </div>
          </div>
          <div v-if="loading" class="page-card mk-msg">Loading catalog…</div>
          <div v-else class="mk-grid">
            <div v-for="app in filteredApps" :key="app.id + app.source" class="page-card mk-app">
              <div class="mk-app-head">
                <span class="mk-app-icon">{{ app.icon || '📦' }}</span>
                <div class="mk-app-meta">
                  <div class="mk-app-name">{{ app.name }}</div>
                  <div class="mk-app-cat">{{ app.category }}<span v-if="app.source !== 'bundled'" class="mk-src">· {{ app.source }}</span></div>
                </div>
              </div>
              <p class="mk-app-desc">{{ app.description }}</p>
              <div class="mk-app-foot">
                <a v-if="app.website" :href="app.website" target="_blank" rel="noopener" class="mk-link">Website ↗</a>
                <div class="dk-spacer" style="flex:1"></div>
                <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openInstall(app)">Install</button>
              </div>
            </div>
            <div v-if="!filteredApps.length" class="page-card mk-msg">No apps match.</div>
          </div>
        </template>

        <!-- INSTALLED -->
        <div v-else-if="tab === 'installed'" class="page-card mk-table-wrap">
          <table class="mk-table">
            <thead><tr><th>App</th><th>Host</th><th>Stack</th><th>Installed</th><th></th></tr></thead>
            <tbody>
              <tr v-if="!installs.length"><td colspan="5" class="mk-msg">Nothing installed yet.</td></tr>
              <tr v-for="i in installs" :key="i.id">
                <td class="mk-name">{{ i.app_name }}</td>
                <td>{{ i.host_name }}</td>
                <td class="mk-mono">{{ i.stack_name }}</td>
                <td class="mk-muted">{{ i.installed_at }}</td>
                <td class="mk-act"><button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="uninstall(i)">Uninstall</button></td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- TOOLS -->
        <template v-else-if="tab === 'tools'">
          <div class="mk-bar">
            <span class="mk-muted">Pin any tool's URL to launch it from here.</span>
            <div class="dk-spacer" style="flex:1"></div>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="showTile = true">+ Add tile</button>
          </div>
          <div class="mk-grid">
            <div v-for="t in tiles" :key="t.id" class="page-card mk-tile" @click="launch(t)">
              <span class="mk-tile-icon">{{ t.icon }}</span>
              <div class="mk-tile-name">{{ t.name }}</div>
              <div class="mk-tile-url">{{ t.url }}</div>
              <button v-if="canManage" class="mk-tile-x" @click.stop="deleteTile(t)">×</button>
            </div>
            <div v-if="!tiles.length" class="page-card mk-msg">No tools pinned.</div>
          </div>
        </template>

        <!-- SOURCES -->
        <template v-else>
          <div class="page-card mk-section">
            <div class="mk-section-head">
              <h3>Catalog sources</h3>
              <button v-if="canManage" class="base-btn base-btn--sm" @click="showCatalog = true">+ Add catalog URL</button>
            </div>
            <p class="mk-muted">Point at a remote JSON catalog to add more apps without redeploying Nias.</p>
            <div v-for="c in catalogs" :key="c.id" class="mk-row">
              <span class="mk-name">{{ c.name }}</span>
              <span class="mk-mono mk-muted">{{ c.url }}</span>
              <div class="dk-spacer" style="flex:1"></div>
              <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="deleteCatalog(c)">Remove</button>
            </div>
            <div v-if="!catalogs.length" class="mk-muted">Only the bundled catalog is active.</div>
          </div>

          <div class="page-card mk-section">
            <div class="mk-section-head">
              <h3>Custom apps</h3>
              <button v-if="canManage" class="base-btn base-btn--sm" @click="showCustomApp = true">+ Add custom app</button>
            </div>
            <p class="mk-muted">Define your own app from a compose file — it shows up in the catalog.</p>
            <div v-for="a in customApps" :key="a.id" class="mk-row">
              <span class="mk-name">{{ a.icon }} {{ a.name }}</span>
              <span class="mk-muted">{{ a.category }}</span>
              <div class="dk-spacer" style="flex:1"></div>
              <button v-if="canManage" class="base-btn base-btn--xs base-btn--danger" @click="deleteCustomApp(a)">Remove</button>
            </div>
            <div v-if="!customApps.length" class="mk-muted">No custom apps yet.</div>
          </div>
        </template>
      </div>
    </div>

    <!-- Install modal -->
    <div v-if="showInstall && installApp" class="mk-modal-backdrop" @click.self="showInstall = false">
      <div class="mk-modal mk-modal--wide page-card">
        <div class="mk-modal-title">{{ installApp.icon }} Install {{ installApp.name }}</div>
        <div class="mk-form">
          <div class="mk-form-row">
            <label class="mk-grow">
              Deploy to host
              <select v-model.number="installHostId" class="base-input">
                <option v-for="h in hosts" :key="h.id" :value="h.id">{{ h.name }}{{ h.ssh_host ? ` (${h.ssh_host})` : ' (local)' }}</option>
              </select>
            </label>
            <label class="mk-grow">Stack name<input v-model="installStack" class="base-input" /></label>
          </div>
          <div v-if="installApp.env.length" class="mk-env">
            <div class="mk-env-title">Configuration</div>
            <label v-for="e in installApp.env" :key="e.key">
              {{ e.key }}<span v-if="e.description" class="mk-muted"> — {{ e.description }}</span>
              <input v-model="installEnv[e.key]" class="base-input" />
            </label>
          </div>
          <button class="mk-compose-toggle" @click="showCompose = !showCompose">{{ showCompose ? '▾' : '▸' }} compose preview</button>
          <pre v-if="showCompose" class="mk-pre">{{ installApp.compose }}</pre>
          <pre v-if="installOutput" class="mk-pre mk-output">{{ installOutput }}</pre>
        </div>
        <div class="mk-modal-actions">
          <a v-if="installApp.website" :href="installApp.website" target="_blank" rel="noopener" class="mk-link">About this app ↗</a>
          <div class="dk-spacer" style="flex:1"></div>
          <button class="base-btn base-btn--sm" @click="showInstall = false">Close</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="installing || !hosts.length" @click="submitInstall">{{ installing ? 'Deploying…' : 'Deploy' }}</button>
        </div>
      </div>
    </div>

    <!-- Add tile modal -->
    <div v-if="showTile" class="mk-modal-backdrop" @click.self="showTile = false">
      <div class="mk-modal page-card">
        <div class="mk-modal-title">Add tool tile</div>
        <div class="mk-form">
          <div class="mk-form-row">
            <label class="mk-icon-field">Icon<input v-model="tileForm.icon" class="base-input" /></label>
            <label class="mk-grow">Name<input v-model="tileForm.name" class="base-input" placeholder="Grafana" /></label>
          </div>
          <label>URL<input v-model="tileForm.url" class="base-input" placeholder="https://grafana.example.com" /></label>
        </div>
        <div class="mk-modal-actions">
          <div class="dk-spacer" style="flex:1"></div>
          <button class="base-btn base-btn--sm" @click="showTile = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="addTile">Add</button>
        </div>
      </div>
    </div>

    <!-- Add catalog modal -->
    <div v-if="showCatalog" class="mk-modal-backdrop" @click.self="showCatalog = false">
      <div class="mk-modal page-card">
        <div class="mk-modal-title">Add catalog URL</div>
        <div class="mk-form">
          <label>Name<input v-model="catalogForm.name" class="base-input" placeholder="Team catalog" /></label>
          <label>URL (JSON)<input v-model="catalogForm.url" class="base-input" placeholder="https://example.com/catalog.json" /></label>
          <p class="mk-muted">The JSON must have an <code>apps</code> array (same shape as the bundled catalog).</p>
        </div>
        <div class="mk-modal-actions">
          <div class="dk-spacer" style="flex:1"></div>
          <button class="base-btn base-btn--sm" @click="showCatalog = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="addCatalog">Add</button>
        </div>
      </div>
    </div>

    <!-- Add custom app modal -->
    <div v-if="showCustomApp" class="mk-modal-backdrop" @click.self="showCustomApp = false">
      <div class="mk-modal mk-modal--wide page-card">
        <div class="mk-modal-title">Add custom app</div>
        <div class="mk-form">
          <div class="mk-form-row">
            <label class="mk-icon-field">Icon<input v-model="customForm.icon" class="base-input" /></label>
            <label class="mk-grow">Name<input v-model="customForm.name" class="base-input" /></label>
            <label class="mk-grow">Category<input v-model="customForm.category" class="base-input" /></label>
          </div>
          <label>Description<input v-model="customForm.description" class="base-input" /></label>
          <label>Website (optional)<input v-model="customForm.website" class="base-input" /></label>
          <label>
            docker-compose.yml
            <textarea v-model="customForm.compose" class="base-input mk-textarea" rows="8" placeholder="services:&#10;  app:&#10;    image: nginx:alpine&#10;    ports:&#10;      - ${PORT}:80"></textarea>
            <span class="mk-muted">Use <code>${VAR}</code> placeholders; declare them below.</span>
          </label>
          <label>
            Variables (one KEY=default per line)
            <textarea v-model="customForm.env" class="base-input mk-textarea" rows="3" placeholder="PORT=8080"></textarea>
          </label>
        </div>
        <div class="mk-modal-actions">
          <div class="dk-spacer" style="flex:1"></div>
          <button class="base-btn base-btn--sm" @click="showCustomApp = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" @click="addCustomApp">Save app</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mk-count { font-size: 11px; color: var(--text-muted); background: var(--bg-hover); border-radius: 999px; padding: 0 6px; margin-left: 4px; }
.mk-bar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.mk-search { min-width: 200px; max-width: 280px; }
.mk-cats { display: flex; gap: 6px; flex-wrap: wrap; }
.mk-cat { font-size: 12px; padding: 4px 10px; border: 1px solid var(--border); border-radius: 999px; background: none; color: var(--text-muted); cursor: pointer; }
.mk-cat--on { background: var(--brand); color: var(--brand-fg); border-color: var(--brand); }
.mk-muted { color: var(--text-muted); font-size: 12px; }
.mk-mono { font-family: var(--mono); font-size: 11px; }
.mk-msg { padding: 30px; text-align: center; color: var(--text-muted); }

.mk-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
.mk-app { padding: 16px; display: flex; flex-direction: column; gap: 10px; }
.mk-app-head { display: flex; align-items: center; gap: 12px; }
.mk-app-icon { font-size: 30px; }
.mk-app-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.mk-app-cat { font-size: 11px; color: var(--text-muted); }
.mk-src { color: var(--brand); }
.mk-app-desc { font-size: 12px; color: var(--text-secondary); line-height: 1.5; flex: 1; margin: 0; }
.mk-app-foot { display: flex; align-items: center; gap: 8px; }
.mk-link { font-size: 12px; color: var(--brand); text-decoration: none; }

.mk-table-wrap { padding: 4px 6px; }
.mk-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.mk-table th { text-align: left; padding: 9px 12px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); font-weight: 600; }
.mk-table td { padding: 9px 12px; border-bottom: 1px solid var(--border); }
.mk-table tbody tr:last-child td { border-bottom: none; }
.mk-name { font-weight: 500; color: var(--text-primary); }
.mk-act { text-align: right; }

.mk-tile { padding: 18px; text-align: center; cursor: pointer; position: relative; transition: border-color var(--dur) var(--ease); }
.mk-tile:hover { border-color: var(--brand); }
.mk-tile-icon { font-size: 34px; }
.mk-tile-name { font-size: 14px; font-weight: 600; color: var(--text-primary); margin-top: 6px; }
.mk-tile-url { font-size: 10px; color: var(--text-muted); margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.mk-tile-x { position: absolute; top: 6px; right: 8px; background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 16px; }
.mk-tile-x:hover { color: var(--danger); }

.mk-section { padding: 16px 18px; }
.mk-section-head { display: flex; align-items: center; justify-content: space-between; }
.mk-section-head h3 { font-size: 14px; margin: 0; color: var(--text-primary); }
.mk-row { display: flex; align-items: center; gap: 12px; padding: 7px 0; border-bottom: 1px solid var(--border); font-size: 12px; }
.mk-row:last-child { border-bottom: none; }

.mk-modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.mk-modal { width: 420px; max-width: 92vw; max-height: 88vh; overflow-y: auto; padding: 22px; }
.mk-modal--wide { width: 600px; }
.mk-modal-title { font-size: 16px; font-weight: 600; color: var(--text-primary); margin-bottom: 14px; }
.mk-form { display: flex; flex-direction: column; gap: 10px; }
.mk-form label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-secondary); }
.mk-form-row { display: flex; gap: 10px; }
.mk-grow { flex: 1; }
.mk-icon-field { width: 64px; }
.mk-env { display: flex; flex-direction: column; gap: 8px; padding: 10px; background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); }
.mk-env-title { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.mk-textarea { font-family: var(--mono); font-size: 11px; resize: vertical; }
.mk-compose-toggle { background: none; border: none; color: var(--text-muted); font-size: 12px; cursor: pointer; text-align: left; padding: 0; }
.mk-pre { background: var(--bg-body); border: 1px solid var(--border); border-radius: var(--r-sm); padding: 10px; font-family: var(--mono); font-size: 11px; line-height: 1.5; max-height: 240px; overflow: auto; white-space: pre-wrap; word-break: break-all; color: var(--text-secondary); margin: 0; }
.mk-output { max-height: 200px; }
.mk-modal-actions { display: flex; align-items: center; gap: 8px; margin-top: 16px; }
</style>
