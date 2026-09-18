<script setup lang="ts">
import { ref, computed, watch } from 'vue'

interface Props {
  show: boolean
  columns: string[]
  values: unknown[]
  rowIndex?: number
  /** Primary-key column name — required for saving edits. */
  pkColumn?: string
  /** Whether the Edit button is offered at all (table must be editable). */
  canEdit?: boolean
  title?: string
}

const props = withDefaults(defineProps<Props>(), {
  rowIndex: 0,
  pkColumn: '',
  canEdit: false,
  title: '',
})

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', payload: { pkValue: unknown; updates: Record<string, unknown> }): void
}>()

const editing = ref(false)
/** Draft values keyed by column — only columns the user touched land here. */
const drafts = ref<Record<string, unknown>>({})
const copiedField = ref<string>('')
let copyTimer: ReturnType<typeof setTimeout> | null = null

// Reopening on a different row (or closing) must never carry drafts over.
watch(() => [props.show, props.rowIndex, props.values] as const, () => {
  editing.value = false
  drafts.value = {}
})

function rawValue(col: string): unknown {
  return props.values[props.columns.indexOf(col)]
}

function displayValue(val: unknown): string {
  if (val === null || val === undefined) return 'NULL'
  if (typeof val === 'object') return JSON.stringify(val, null, 2)
  return String(val)
}

function valueType(val: unknown): string {
  if (val === null || val === undefined) return 'null'
  if (typeof val === 'number') return 'number'
  if (typeof val === 'boolean') return 'boolean'
  const str = String(val).trim()
  // Same guard as CellInspector: only `{`/`[` prefixes count as JSON so a long
  // numeric string isn't round-tripped through a JS number and truncated.
  if (str.startsWith('{') || str.startsWith('[')) {
    try { JSON.parse(str); return 'json' } catch { /* not JSON after all */ }
  }
  return 'text'
}

function prettyValue(val: unknown): string {
  if (valueType(val) !== 'json') return displayValue(val)
  try { return JSON.stringify(JSON.parse(String(val)), null, 2) } catch { return displayValue(val) }
}

/** Value currently shown for a column — the draft when edited, else the row's. */
function currentValue(col: string): unknown {
  return Object.prototype.hasOwnProperty.call(drafts.value, col) ? drafts.value[col] : rawValue(col)
}

function isDirty(col: string): boolean {
  if (!Object.prototype.hasOwnProperty.call(drafts.value, col)) return false
  const original = rawValue(col)
  const draft = drafts.value[col]
  if (draft === null || original === null || original === undefined) return draft !== original
  return String(draft) !== String(original ?? '')
}

const dirtyColumns = computed(() => props.columns.filter(isDirty))
const hasChanges = computed(() => dirtyColumns.value.length > 0)

function onFieldInput(col: string, event: Event) {
  drafts.value = { ...drafts.value, [col]: (event.target as HTMLInputElement | HTMLTextAreaElement).value }
}

function setNull(col: string) {
  drafts.value = { ...drafts.value, [col]: null }
}

function resetField(col: string) {
  const next = { ...drafts.value }
  delete next[col]
  drafts.value = next
}

/**
 * Multi-line / long values get a textarea instead of a single-line input.
 * Decided once when edit mode opens — re-evaluating per keystroke would swap
 * the element out mid-typing and drop focus.
 */
const longFields = ref<Set<string>>(new Set())

function isLongValue(col: string): boolean {
  return longFields.value.has(col)
}

async function copyValue(col: string) {
  await navigator.clipboard.writeText(displayValue(currentValue(col)))
  copiedField.value = col
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copiedField.value = '' }, 1200)
}

async function copyRowJson() {
  const obj: Record<string, unknown> = {}
  for (const col of props.columns) obj[col] = currentValue(col)
  await navigator.clipboard.writeText(JSON.stringify(obj, null, 2))
  copiedField.value = '__row__'
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copiedField.value = '' }, 1200)
}

function startEdit() {
  longFields.value = new Set(
    props.columns.filter(col => {
      const str = displayValue(rawValue(col))
      return str.length > 60 || str.includes('\n')
    }),
  )
  editing.value = true
}

function cancelEdit() {
  editing.value = false
  drafts.value = {}
}

function save() {
  if (!hasChanges.value) return
  const pkIdx = props.columns.indexOf(props.pkColumn)
  const pkValue = pkIdx >= 0 ? props.values[pkIdx] : null
  const updates: Record<string, unknown> = {}
  for (const col of dirtyColumns.value) updates[col] = drafts.value[col]
  emit('save', { pkValue, updates })
  editing.value = false
  drafts.value = {}
}

function requestClose() {
  if (editing.value && hasChanges.value && !confirm('Discard unsaved changes to this row?')) return
  cancelEdit()
  emit('close')
}

const canSave = computed(() => props.canEdit && props.columns.includes(props.pkColumn))
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="ri-overlay" @click.self="requestClose">
      <div class="ri-modal">
        <div class="ri-header">
          <div class="ri-title">
            <span class="ri-label">{{ title || 'Row' }}</span>
            <span class="ri-badge">#{{ rowIndex + 1 }}</span>
            <span class="ri-badge ri-badge--muted">{{ columns.length }} columns</span>
            <span v-if="editing" class="ri-badge ri-badge--edit">Editing</span>
          </div>
          <div class="ri-actions">
            <button class="ri-btn" type="button" title="Copy whole row as JSON" @click="copyRowJson">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              {{ copiedField === '__row__' ? 'Copied' : 'Copy JSON' }}
            </button>
            <button v-if="canSave && !editing" class="ri-btn ri-btn--primary" type="button" title="Edit this row" @click="startEdit">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>
              Edit
            </button>
            <button class="ri-btn ri-btn--close" type="button" title="Close" @click="requestClose">×</button>
          </div>
        </div>

        <div class="ri-body">
          <div v-for="col in columns" :key="col" class="ri-field" :class="{ 'ri-field--dirty': isDirty(col) }">
            <div class="ri-field__key">
              <span class="ri-field__name" :title="col">{{ col }}</span>
              <span v-if="pkColumn && col === pkColumn" class="ri-pk">PK</span>
              <span class="ri-type" :class="`ri-type--${valueType(currentValue(col))}`">{{ valueType(currentValue(col)) }}</span>
            </div>

            <div class="ri-field__value">
              <template v-if="editing">
                <textarea
                  v-if="isLongValue(col)"
                  class="ri-input ri-input--area"
                  :value="currentValue(col) === null || currentValue(col) === undefined ? '' : displayValue(currentValue(col))"
                  :placeholder="currentValue(col) === null ? 'NULL' : ''"
                  rows="4"
                  @input="onFieldInput(col, $event)"
                />
                <input
                  v-else
                  class="ri-input"
                  :value="currentValue(col) === null || currentValue(col) === undefined ? '' : displayValue(currentValue(col))"
                  :placeholder="currentValue(col) === null ? 'NULL' : ''"
                  @input="onFieldInput(col, $event)"
                />
              </template>
              <pre v-else class="ri-pre" :class="{ 'ri-pre--null': valueType(currentValue(col)) === 'null' }">{{ prettyValue(currentValue(col)) }}</pre>
            </div>

            <div class="ri-field__tools">
              <button class="ri-mini" type="button" :title="`Copy ${col}`" @click="copyValue(col)">
                <svg v-if="copiedField !== col" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
              <template v-if="editing">
                <button class="ri-mini" type="button" title="Set NULL" @click="setNull(col)">∅</button>
                <button class="ri-mini" type="button" title="Revert this field" :disabled="!isDirty(col)" @click="resetField(col)">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
                </button>
              </template>
            </div>
          </div>
        </div>

        <div class="ri-footer">
          <span v-if="editing" class="ri-footer__hint">
            {{ dirtyColumns.length === 0 ? 'No changes yet' : `${dirtyColumns.length} field${dirtyColumns.length > 1 ? 's' : ''} changed` }}
          </span>
          <span v-else-if="canSave" class="ri-footer__hint">Click Edit to change this row.</span>
          <span v-else-if="canEdit" class="ri-footer__hint">No primary key detected — this row can't be updated.</span>
          <div class="ri-footer__spacer" />
          <template v-if="editing">
            <button class="ri-btn" type="button" @click="cancelEdit">Cancel</button>
            <button class="ri-btn ri-btn--primary" type="button" :disabled="!hasChanges" @click="save">Save Changes</button>
          </template>
          <button v-else class="ri-btn" type="button" @click="requestClose">Close</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ri-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.ri-modal {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: min(760px, 92vw);
  max-height: 82vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
}
.ri-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.ri-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.ri-label {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ri-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  color: var(--text-secondary, var(--text-muted));
  white-space: nowrap;
}
.ri-badge--muted { color: var(--text-muted); }
.ri-badge--edit {
  color: var(--brand);
  border-color: var(--brand);
}
.ri-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.ri-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 600;
  padding: 5px 10px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-primary);
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s, opacity 0.12s;
}
.ri-btn:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); }
.ri-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.ri-btn--primary {
  background: var(--brand);
  border-color: var(--brand);
  color: #fff;
}
.ri-btn--primary:hover:not(:disabled) { filter: brightness(1.08); color: #fff; }
.ri-btn--close {
  font-size: 16px;
  line-height: 1;
  padding: 3px 9px;
}

.ri-body {
  overflow: auto;
  padding: 4px 0;
  flex: 1;
  min-height: 0;
}
.ri-field {
  display: grid;
  grid-template-columns: 200px 1fr auto;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--border);
}
.ri-field:last-child { border-bottom: none; }
.ri-field:hover { background: var(--bg-surface); }
.ri-field--dirty { background: color-mix(in srgb, var(--brand) 10%, transparent); }
.ri-field__key {
  display: flex;
  align-items: center;
  gap: 5px;
  flex-wrap: wrap;
  padding-top: 3px;
  min-width: 0;
}
.ri-field__name {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ri-pk {
  font-size: 9px;
  font-weight: 700;
  padding: 1px 4px;
  border-radius: 3px;
  background: var(--brand);
  color: #fff;
}
.ri-type {
  font-size: 9px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid var(--border);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.ri-type--null { color: #b45309; }
.ri-type--number { color: #2563eb; }
.ri-type--json { color: #7c3aed; }
.ri-type--boolean { color: #0891b2; }

.ri-field__value { min-width: 0; }
.ri-pre {
  margin: 0;
  font-size: 12px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 180px;
  overflow: auto;
}
.ri-pre--null { color: var(--text-muted); font-style: italic; }
.ri-input {
  width: 100%;
  font-size: 12px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  padding: 5px 8px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--bg-base, var(--bg-elevated));
  color: var(--text-primary);
}
.ri-input:focus { outline: none; border-color: var(--brand); }
.ri-input--area { resize: vertical; line-height: 1.45; }

.ri-field__tools {
  display: flex;
  align-items: center;
  gap: 4px;
  padding-top: 2px;
}
.ri-mini {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  font-size: 12px;
  border-radius: 4px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.ri-mini:hover:not(:disabled) {
  color: var(--brand);
  border-color: var(--border);
  background: var(--bg-elevated);
}
.ri-mini:disabled { opacity: 0.35; cursor: not-allowed; }

.ri-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.ri-footer__hint { font-size: 11px; color: var(--text-muted); }
.ri-footer__spacer { flex: 1; }
</style>
