<script setup lang="ts">
import { ref, computed } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'

interface CronHost {
  id: number
  name: string
  ssh_host: string
  ssh_user: string
}

interface CrontabResponse {
  host_id: number
  host_name: string
  user: string
  exists: boolean
  user_crontab: string
  system_crontab: string
}

// A crontab is parsed into ordered items: editable cron entries plus verbatim
// "other" lines (comments, env assignments, blanks) that are preserved on save.
type CronItem =
  | { kind: 'entry'; schedule: string; command: string; enabled: boolean }
  | { kind: 'other'; text: string }

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['cron.manage']))
const canExec = computed(() => hasAnyPermission(['cron.exec']))

const hosts = ref<CronHost[]>([])
const hostsLoading = ref(false)
const selectedId = ref<number | null>(null)

const items = ref<CronItem[]>([])
const originalRaw = ref('')
const systemRaw = ref('')
const cronUser = ref('')
const crontabLoading = ref(false)
const saving = ref(false)
const rawMode = ref(false)
const rawText = ref('')
const showSystem = ref(false)

const runOutput = ref<{ command: string; exit_code: number; stdout: string; stderr: string } | null>(null)
const running = ref(false)

const SCHEDULE_PRESETS = ['* * * * *', '*/5 * * * *', '0 * * * *', '0 0 * * *', '0 0 * * 0', '@reboot', '@daily']
const CRON_SPECIALS = ['@reboot', '@yearly', '@annually', '@monthly', '@weekly', '@daily', '@midnight', '@hourly']

const selectedHost = computed(() => hosts.value.find((h) => h.id === selectedId.value) || null)
const entryRows = computed(() =>
  items.value.map((it, idx) => ({ it, idx })).filter((r) => r.it.kind === 'entry') as {
    it: Extract<CronItem, { kind: 'entry' }>
    idx: number
  }[],
)
const preservedCount = computed(() => items.value.filter((it) => it.kind === 'other' && it.text.trim() !== '').length)

const currentRaw = computed(() => (rawMode.value ? rawText.value : serialize(items.value)))
const dirty = computed(() => normalize(currentRaw.value) !== normalize(originalRaw.value))

function normalize(s: string) {
  return s.replace(/\r\n/g, '\n').replace(/\n+$/, '')
}

// ── crontab parse / serialize ──────────────────────────────────────────────

function tryParseEntry(s: string): { schedule: string; command: string } | null {
  const parts = s.trim().split(/\s+/)
  if (!parts.length || parts[0] === '') return null
  if (parts[0].startsWith('@')) {
    if (!CRON_SPECIALS.includes(parts[0].toLowerCase()) || parts.length < 2) return null
    return { schedule: parts[0], command: parts.slice(1).join(' ') }
  }
  if (parts.length < 6) return null
  const sched = parts.slice(0, 5)
  if (!sched.every((f) => /^[\d*/,\-A-Za-z]+$/.test(f))) return null
  return { schedule: sched.join(' '), command: parts.slice(5).join(' ') }
}

function parseCrontab(raw: string): CronItem[] {
  const lines = normalize(raw).split('\n')
  const out: CronItem[] = []
  for (const line of lines) {
    const trimmed = line.trim()
    if (trimmed === '') {
      out.push({ kind: 'other', text: line })
      continue
    }
    if (trimmed.startsWith('#')) {
      const body = trimmed.replace(/^#+\s?/, '')
      const e = tryParseEntry(body)
      if (e) {
        out.push({ kind: 'entry', schedule: e.schedule, command: e.command, enabled: false })
        continue
      }
      out.push({ kind: 'other', text: line })
      continue
    }
    if (/^\s*[A-Za-z_][A-Za-z0-9_]*\s*=/.test(line)) {
      out.push({ kind: 'other', text: line })
      continue
    }
    const e = tryParseEntry(trimmed)
    if (e) {
      out.push({ kind: 'entry', schedule: e.schedule, command: e.command, enabled: true })
      continue
    }
    out.push({ kind: 'other', text: line })
  }
  return out
}

function serialize(list: CronItem[]): string {
  const lines = list.map((it) => {
    if (it.kind === 'other') return it.text
    const line = `${it.schedule} ${it.command}`.trim()
    return it.enabled ? line : `# ${line}`
  })
  return normalize(lines.join('\n')) + '\n'
}

// ── data loading ────────────────────────────────────────────────────────────

async function loadHosts() {
  hostsLoading.value = true
  try {
    const { data } = await axios.get<CronHost[]>('/api/cron/hosts')
    hosts.value = data || []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load hosts')
  } finally {
    hostsLoading.value = false
  }
}

async function selectHost(h: CronHost) {
  if (dirty.value && !(await confirm('Discard unsaved changes?', `You have unsaved edits to ${selectedHost.value?.name}.`))) {
    return
  }
  selectedId.value = h.id
  await loadCrontab()
}

async function loadCrontab() {
  if (!selectedId.value) return
  crontabLoading.value = true
  rawMode.value = false
  try {
    const { data } = await axios.get<CrontabResponse>(`/api/cron/hosts/${selectedId.value}/crontab`)
    originalRaw.value = data.user_crontab || ''
    items.value = parseCrontab(originalRaw.value)
    rawText.value = serialize(items.value)
    systemRaw.value = data.system_crontab || ''
    cronUser.value = data.user || ''
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to read crontab')
    items.value = []
    originalRaw.value = ''
    systemRaw.value = ''
  } finally {
    crontabLoading.value = false
  }
}

// ── editing ─────────────────────────────────────────────────────────────────

function addEntry() {
  items.value.push({ kind: 'entry', schedule: '*/5 * * * *', command: '', enabled: true })
}

function deleteEntry(idx: number) {
  items.value.splice(idx, 1)
}

function toggleEntry(idx: number) {
  const it = items.value[idx]
  if (it.kind === 'entry') it.enabled = !it.enabled
}

function toggleRawMode() {
  if (rawMode.value) {
    // leaving raw → reparse text into items
    items.value = parseCrontab(rawText.value)
  } else {
    rawText.value = serialize(items.value)
  }
  rawMode.value = !rawMode.value
}

async function save() {
  if (!selectedId.value) return
  saving.value = true
  try {
    const raw = currentRaw.value
    await axios.put(`/api/cron/hosts/${selectedId.value}/crontab`, { raw })
    toast.success('Crontab updated')
    await loadCrontab()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to write crontab')
  } finally {
    saving.value = false
  }
}

async function runNow(entry: Extract<CronItem, { kind: 'entry' }>) {
  if (!selectedId.value || !entry.command.trim()) return
  running.value = true
  runOutput.value = null
  try {
    const { data } = await axios.post(`/api/cron/hosts/${selectedId.value}/run`, { command: entry.command })
    runOutput.value = { command: entry.command, ...data }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Run failed')
  } finally {
    running.value = false
  }
}

loadHosts()
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Infrastructure</div>
            <div class="page-title">Scheduler</div>
            <div class="page-subtitle">Read and edit the native crontab on each of your servers over SSH.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="hostsLoading" @click="loadHosts">Refresh hosts</button>
          </div>
        </section>

        <div v-if="!hostsLoading && !hosts.length" class="crn-warn">
          No SSH hosts configured yet — add a server under
          <RouterLink :to="{ name: 'ssh-hosts' }" class="crn-link">SSH Hosts</RouterLink>
          to manage its crontab.
        </div>

        <div class="crn-layout">
          <!-- Host list -->
          <div class="crn-hosts page-card">
            <div class="crn-hosts-head">Hosts</div>
            <div v-if="hostsLoading" class="crn-msg">Loading…</div>
            <div v-else-if="!hosts.length" class="crn-msg">None</div>
            <button
              v-for="h in hosts"
              :key="h.id"
              class="crn-hostbtn"
              :class="{ 'crn-hostbtn--sel': h.id === selectedId }"
              @click="selectHost(h)"
            >
              <div class="crn-hostname">{{ h.name }}</div>
              <div class="crn-hostmeta">{{ h.ssh_user }}@{{ h.ssh_host }}</div>
            </button>
          </div>

          <!-- Crontab editor -->
          <div class="crn-editor page-card">
            <div v-if="!selectedHost" class="crn-msg">Select a host to view and edit its crontab.</div>
            <template v-else>
              <div class="crn-editor-head">
                <div>
                  <div class="crn-editor-title">{{ selectedHost.name }}</div>
                  <div class="crn-editor-sub">
                    crontab for <code>{{ cronUser || selectedHost.ssh_user }}</code>
                    <span v-if="dirty" class="crn-dirty">• unsaved changes</span>
                  </div>
                </div>
                <div class="crn-editor-actions">
                  <button class="base-btn base-btn--sm" :disabled="crontabLoading" @click="loadCrontab">Reload</button>
                  <button class="base-btn base-btn--sm" @click="toggleRawMode">
                    {{ rawMode ? 'Table view' : 'Raw view' }}
                  </button>
                  <button
                    v-if="canManage"
                    class="base-btn base-btn--sm base-btn--primary"
                    :disabled="saving || !dirty"
                    @click="save"
                  >
                    {{ saving ? 'Saving…' : 'Save' }}
                  </button>
                </div>
              </div>

              <div v-if="crontabLoading" class="crn-msg">Reading crontab…</div>

              <!-- Raw editor -->
              <template v-else-if="rawMode">
                <textarea
                  v-model="rawText"
                  class="crn-raw"
                  spellcheck="false"
                  :readonly="!canManage"
                  placeholder="# m h dom mon dow   command"
                ></textarea>
              </template>

              <!-- Table editor -->
              <template v-else>
                <table v-if="entryRows.length" class="crn-table">
                  <thead>
                    <tr>
                      <th class="crn-c-on">On</th>
                      <th class="crn-c-sched">Schedule</th>
                      <th>Command</th>
                      <th class="crn-c-act"></th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in entryRows" :key="row.idx" :class="{ 'crn-off': !row.it.enabled }">
                      <td>
                        <label class="crn-switch">
                          <input
                            type="checkbox"
                            :checked="row.it.enabled"
                            :disabled="!canManage"
                            @change="toggleEntry(row.idx)"
                          />
                          <span />
                        </label>
                      </td>
                      <td>
                        <input
                          v-model="row.it.schedule"
                          class="crn-in crn-mono"
                          list="cron-presets"
                          :readonly="!canManage"
                        />
                      </td>
                      <td>
                        <input v-model="row.it.command" class="crn-in crn-mono" :readonly="!canManage" />
                      </td>
                      <td class="crn-rowact">
                        <button
                          v-if="canExec"
                          class="crn-iconbtn"
                          title="Run now"
                          :disabled="running || !row.it.command.trim()"
                          @click="runNow(row.it)"
                        >
                          ▶
                        </button>
                        <button
                          v-if="canManage"
                          class="crn-iconbtn crn-del"
                          title="Delete"
                          @click="deleteEntry(row.idx)"
                        >
                          ✕
                        </button>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <div v-else class="crn-msg">No cron entries in this crontab yet.</div>
                <datalist id="cron-presets">
                  <option v-for="p in SCHEDULE_PRESETS" :key="p" :value="p" />
                </datalist>

                <div class="crn-editor-foot">
                  <button v-if="canManage" class="base-btn base-btn--sm" @click="addEntry">+ Add entry</button>
                  <span v-if="preservedCount" class="crn-note">
                    {{ preservedCount }} comment/env line(s) preserved — use Raw view to see them
                  </span>
                </div>
              </template>

              <!-- System crons (read-only) -->
              <div v-if="systemRaw" class="crn-system">
                <button class="crn-system-toggle" @click="showSystem = !showSystem">
                  {{ showSystem ? '▾' : '▸' }} System cron (read-only) — /etc/crontab, /etc/cron.d
                </button>
                <pre v-if="showSystem" class="crn-syspre">{{ systemRaw }}</pre>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Run output -->
    <div v-if="runOutput" class="crn-overlay" @click.self="runOutput = null">
      <div class="crn-modal">
        <div class="crn-modal-head">
          Run output
          <span class="crn-badge" :class="runOutput.exit_code === 0 ? 'crn-badge--ok' : 'crn-badge--err'">
            exit {{ runOutput.exit_code }}
          </span>
        </div>
        <code class="crn-runcmd">{{ runOutput.command }}</code>
        <div class="crn-out-label">stdout</div>
        <pre class="crn-output">{{ runOutput.stdout || '(empty)' }}</pre>
        <div class="crn-out-label">stderr</div>
        <pre class="crn-output crn-output--err">{{ runOutput.stderr || '(empty)' }}</pre>
        <div class="crn-modal-actions">
          <div class="crn-spacer" />
          <button class="base-btn base-btn--sm" @click="runOutput = null">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.crn-warn {
  padding: 0.75rem 1rem;
  background: rgba(234, 179, 8, 0.12);
  border: 1px solid rgba(234, 179, 8, 0.4);
  border-radius: 8px;
  font-size: 0.85rem;
}
.crn-link {
  color: #2563eb;
  font-weight: 600;
  text-decoration: underline;
}
.crn-msg {
  padding: 1.25rem;
  color: var(--text-muted, #64748b);
  text-align: center;
}
.crn-layout {
  display: grid;
  grid-template-columns: minmax(200px, 260px) 1fr;
  gap: 1rem;
  align-items: start;
}
@media (max-width: 860px) {
  .crn-layout {
    grid-template-columns: 1fr;
  }
}
.crn-hosts {
  padding: 0.5rem;
}
.crn-hosts-head {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted, #94a3b8);
  padding: 0.4rem 0.5rem;
}
.crn-hostbtn {
  display: block;
  width: 100%;
  text-align: left;
  padding: 0.5rem;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  color: inherit;
}
.crn-hostbtn:hover {
  background: var(--surface-2, #f1f5f9);
}
.crn-hostbtn--sel {
  background: rgba(59, 130, 246, 0.12);
}
.crn-hostname {
  font-weight: 600;
  font-size: 0.88rem;
}
.crn-hostmeta {
  font-family: ui-monospace, monospace;
  font-size: 0.74rem;
  color: var(--text-muted, #64748b);
}
.crn-editor {
  padding: 1rem;
  min-height: 200px;
}
.crn-editor-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
  margin-bottom: 1rem;
  flex-wrap: wrap;
}
.crn-editor-title {
  font-size: 1.1rem;
  font-weight: 600;
}
.crn-editor-sub {
  font-size: 0.8rem;
  color: var(--text-muted, #64748b);
  margin-top: 0.2rem;
}
.crn-editor-sub code {
  font-family: ui-monospace, monospace;
}
.crn-dirty {
  color: #d97706;
  font-weight: 600;
  margin-left: 0.35rem;
}
.crn-editor-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}
.crn-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.85rem;
}
.crn-table th {
  text-align: left;
  padding: 0.4rem 0.5rem;
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted, #94a3b8);
  border-bottom: 1px solid var(--border, #e2e8f0);
}
.crn-table td {
  padding: 0.35rem 0.4rem;
  border-bottom: 1px solid var(--border, #f1f5f9);
  vertical-align: middle;
}
.crn-c-on {
  width: 42px;
}
.crn-c-sched {
  width: 190px;
}
.crn-c-act {
  width: 70px;
}
.crn-off {
  opacity: 0.55;
}
.crn-in {
  width: 100%;
  padding: 0.35rem 0.45rem;
  border: 1px solid var(--border, #cbd5e1);
  border-radius: 6px;
  background: var(--surface-2, #f8fafc);
  color: inherit;
}
.crn-mono {
  font-family: ui-monospace, monospace;
  font-size: 0.8rem;
}
.crn-rowact {
  white-space: nowrap;
  text-align: right;
}
.crn-iconbtn {
  border: 1px solid var(--border, #cbd5e1);
  background: transparent;
  border-radius: 6px;
  width: 26px;
  height: 26px;
  cursor: pointer;
  color: inherit;
  margin-left: 0.25rem;
}
.crn-iconbtn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.crn-del {
  color: #ef4444;
}
.crn-switch {
  position: relative;
  display: inline-block;
  width: 32px;
  height: 18px;
}
.crn-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.crn-switch span {
  position: absolute;
  inset: 0;
  background: #cbd5e1;
  border-radius: 999px;
  transition: 0.2s;
}
.crn-switch span::before {
  content: '';
  position: absolute;
  height: 14px;
  width: 14px;
  left: 2px;
  bottom: 2px;
  background: #fff;
  border-radius: 50%;
  transition: 0.2s;
}
.crn-switch input:checked + span {
  background: #22c55e;
}
.crn-switch input:checked + span::before {
  transform: translateX(14px);
}
.crn-editor-foot {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-top: 0.75rem;
}
.crn-note {
  font-size: 0.76rem;
  color: var(--text-muted, #94a3b8);
}
.crn-raw {
  width: 100%;
  min-height: 320px;
  padding: 0.75rem;
  border: 1px solid var(--border, #cbd5e1);
  border-radius: 8px;
  background: #0f172a;
  color: #e2e8f0;
  font-family: ui-monospace, monospace;
  font-size: 0.82rem;
  line-height: 1.5;
  resize: vertical;
}
.crn-system {
  margin-top: 1.25rem;
  border-top: 1px solid var(--border, #e2e8f0);
  padding-top: 0.75rem;
}
.crn-system-toggle {
  border: none;
  background: transparent;
  color: var(--text-muted, #64748b);
  cursor: pointer;
  font-size: 0.8rem;
  padding: 0;
}
.crn-syspre {
  margin-top: 0.5rem;
  background: var(--surface-2, #f1f5f9);
  padding: 0.75rem;
  border-radius: 8px;
  font-family: ui-monospace, monospace;
  font-size: 0.76rem;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 260px;
  overflow-y: auto;
}
.crn-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 1rem;
}
.crn-modal {
  background: var(--surface, #fff);
  border-radius: 12px;
  padding: 1.25rem;
  width: 100%;
  max-width: 720px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
}
.crn-modal-head {
  font-size: 1.05rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.crn-runcmd {
  display: block;
  font-family: ui-monospace, monospace;
  font-size: 0.8rem;
  background: var(--surface-2, #f1f5f9);
  padding: 0.5rem;
  border-radius: 6px;
  margin-bottom: 0.5rem;
  word-break: break-all;
}
.crn-badge {
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 600;
}
.crn-badge--ok {
  background: rgba(34, 197, 94, 0.15);
  color: #16a34a;
}
.crn-badge--err {
  background: rgba(239, 68, 68, 0.15);
  color: #dc2626;
}
.crn-out-label {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted, #94a3b8);
  margin: 0.5rem 0 0.25rem;
}
.crn-output {
  background: #0f172a;
  color: #e2e8f0;
  padding: 0.75rem;
  border-radius: 8px;
  font-family: ui-monospace, monospace;
  font-size: 0.78rem;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow-y: auto;
  margin: 0;
}
.crn-output--err {
  color: #fca5a5;
}
.crn-modal-actions {
  display: flex;
  align-items: center;
  margin-top: 0.75rem;
}
.crn-spacer {
  flex: 1;
}
</style>
