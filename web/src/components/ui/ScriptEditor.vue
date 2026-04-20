<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { EditorView, keymap, lineNumbers, drawSelection, highlightActiveLine, highlightSpecialChars } from '@codemirror/view'
import { EditorState, Compartment } from '@codemirror/state'
import { defaultKeymap, indentWithTab, history, historyKeymap } from '@codemirror/commands'
import { bracketMatching, indentOnInput } from '@codemirror/language'
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap, snippetCompletion, startCompletion, type Completion, type CompletionContext } from '@codemirror/autocomplete'
import { oneDark } from '@codemirror/theme-one-dark'

const props = defineProps<{
  modelValue: string
  darkMode?: boolean
  placeholder?: string
  schemaTables?: Array<{ name: string; columns: Array<{ name: string; type?: string }> }>
  language?: string
  fileLabel?: string
  showPreviewButton?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'preview-request': []
}>()

const editorEl = ref<HTMLElement>()
const lineCount = ref(1)
const cursorLine = ref(1)
const cursorColumn = ref(1)
const sidePanelOpen = ref(true)
const schemaQuery = ref('')
const expandedSchemaTable = ref<string | null>(null)
let view: EditorView | null = null
const themeCompartment = new Compartment()

function currentLanguage() {
  return (props.language || 'javascript').toLowerCase()
}

function languageLabel() {
  switch (currentLanguage()) {
    case 'python':
      return 'Python'
    case 'php':
      return 'PHP'
    default:
      return 'JavaScript'
  }
}

function propertyInsert(name: string) {
  if (currentLanguage() === 'php') return `"${name}" => `
  return `"${name}": `
}

function runtimeSnippetCompletions() {
  switch (currentLanguage()) {
    case 'python':
      return [
        snippetCompletion('plan.update("${table}", {"${pk}": ${keyValue}}, {"${column}": ${value}})', {
          label: 'plan.update',
          type: 'function',
          detail: 'Update a row by key',
        }),
        snippetCompletion('plan.insert("${table}", {"${column}": ${value}})', {
          label: 'plan.insert',
          type: 'function',
          detail: 'Insert a row',
        }),
        snippetCompletion('plan.delete("${table}", {"${pk}": ${keyValue}})', {
          label: 'plan.delete',
          type: 'function',
          detail: 'Delete a row by key',
        }),
      ]
    case 'php':
      return [
        snippetCompletion('$plan->update("${table}", ["${pk}" => ${keyValue}], ["${column}" => ${value}]);', {
          label: '$plan->update',
          type: 'function',
          detail: 'Update a row by key',
        }),
        snippetCompletion('$plan->insert("${table}", ["${column}" => ${value}]);', {
          label: '$plan->insert',
          type: 'function',
          detail: 'Insert a row',
        }),
        snippetCompletion('$plan->delete("${table}", ["${pk}" => ${keyValue}]);', {
          label: '$plan->delete',
          type: 'function',
          detail: 'Delete a row by key',
        }),
      ]
    default:
      return [
        snippetCompletion('plan.update("${table}", {"${pk}": ${keyValue}}, {"${column}": ${value}});', {
          label: 'plan.update',
          type: 'function',
          detail: 'Update a row by key',
        }),
        snippetCompletion('plan.insert("${table}", {"${column}": ${value}});', {
          label: 'plan.insert',
          type: 'function',
          detail: 'Insert a row',
        }),
        snippetCompletion('plan.delete("${table}", {"${pk}": ${keyValue}});', {
          label: 'plan.delete',
          type: 'function',
          detail: 'Delete a row by key',
        }),
      ]
  }
}

function staticScriptCompletions(): Completion[] {
  return [
    ...runtimeSnippetCompletions(),
    {
      label: '"id"',
      type: 'property',
      apply: propertyInsert('id'),
    },
    {
      label: '"username"',
      type: 'property',
      apply: propertyInsert('username'),
    },
    {
      label: '"status"',
      type: 'property',
      apply: currentLanguage() === 'php' ? '"status" => "active"' : '"status": "active"',
    },
    {
      label: currentLanguage() === 'php' ? '[]' : '{}',
      type: 'text',
      apply: currentLanguage() === 'php' ? '[]' : '{}',
    },
  ]
}

function currentLinePrefix(context: CompletionContext) {
  const line = context.state.doc.lineAt(context.pos)
  return line.text.slice(0, context.pos - line.from)
}

function insertText(content: string) {
  if (!view) return
  const selection = view.state.selection.main
  view.dispatch({
    changes: { from: selection.from, to: selection.to, insert: content },
    selection: { anchor: selection.from + content.length },
  })
  view.focus()
}

function insertSchemaTemplate(tableName: string, columnName?: string) {
  if (currentLanguage() === 'php') {
    insertText(columnName ? `"${columnName}" => ` : `$plan->update("${tableName}", ["id" => 1], ["column" => "value"]);`)
    return
  }
  if (currentLanguage() === 'python') {
    insertText(columnName ? `"${columnName}": ` : `plan.update("${tableName}", {"id": 1}, {"column": "value"})`)
    return
  }
  insertText(columnName ? `"${columnName}": ` : `plan.update("${tableName}", { id: 1 }, { column: "value" });`)
}

function activeScriptTables() {
  const matches = props.modelValue.matchAll(/(?:plan\.|\$plan->)(?:insert|update|delete)\(\s*["']([^"']+)["']/g)
  return new Set(Array.from(matches, (match) => match[1]))
}

const filteredSchemaTables = computed(() => {
  const tables = props.schemaTables ?? []
  const query = schemaQuery.value.trim().toLowerCase()
  const activeTables = activeScriptTables()

  const ranked = tables
    .map((table) => {
      const nameMatch = !query || table.name.toLowerCase().includes(query)
      const matchedColumns = query
        ? table.columns.filter((column) => column.name.toLowerCase().includes(query))
        : table.columns

      const isRelevant = activeTables.has(table.name)
      if (!nameMatch && matchedColumns.length === 0) return null

      return {
        ...table,
        isRelevant,
        previewColumns: query ? matchedColumns.slice(0, 6) : table.columns.slice(0, 4),
      }
    })
    .filter((table): table is NonNullable<typeof table> => !!table)
    .sort((a, b) => {
      if (a.isRelevant !== b.isRelevant) return a.isRelevant ? -1 : 1
      return a.name.localeCompare(b.name)
    })

  return ranked.slice(0, query ? 24 : 12)
})

const schemaSummary = computed(() => {
  const totalTables = props.schemaTables?.length ?? 0
  const visibleTables = filteredSchemaTables.value.length
  if (!totalTables) return 'No schema loaded'
  if (!schemaQuery.value.trim()) return `${totalTables} tables available`
  return `${visibleTables} of ${totalTables} tables matched`
})

function toggleSchemaTable(tableName: string) {
  expandedSchemaTable.value = expandedSchemaTable.value === tableName ? null : tableName
}

function currentScriptTable(beforeCursor: string): { name: string; columns: Array<{ name: string; type?: string }> } | null {
  const tableMatch = beforeCursor.match(/(?:plan\.|\$plan->)(?:insert|update|delete)\(\s*["']([^"']+)["']/)
  const tableName = tableMatch?.[1]
  if (!tableName) return null
  return props.schemaTables?.find((table) => table.name === tableName) ?? null
}

function splitTopLevelArgs(input: string) {
  const args: string[] = []
  let current = ''
  let depth = 0
  let inString = false
  let stringChar = ''

  for (let i = 0; i < input.length; i++) {
    const ch = input[i]
    current += ch
    if (inString) {
      if (ch === stringChar && input[i - 1] !== '\\') {
        inString = false
      }
      continue
    }
    if (ch === '"' || ch === '\'') {
      inString = true
      stringChar = ch
      continue
    }
    if (ch === '{' || ch === '[' || ch === '(') {
      depth++
      continue
    }
    if (ch === '}' || ch === ']' || ch === ')') {
      depth--
      continue
    }
    if (ch === ',' && depth === 1) {
      args.push(current.slice(0, -1).trim())
      current = ''
    }
  }

  if (current.trim()) {
    args.push(current.trim())
  }

  return args
}

function currentArgumentIndex(beforeCursor: string) {
  const match = beforeCursor.match(/(?:plan\.|\$plan->)(insert|update|delete)\((.*)$/)
  if (!match) return null
  const kind = match[1]
  const body = `(${match[2]}`
  const args = splitTopLevelArgs(body)
  return {
    kind,
    index: Math.max(0, args.length - 1),
    body,
  }
}

function tableNameCompletions() {
  return (props.schemaTables ?? []).map((table) => ({
    label: table.name,
    type: 'class' as const,
    detail: 'table',
    apply: table.name,
    boost: 8,
  }))
}

function columnNameCompletions(table: { name: string; columns: Array<{ name: string; type?: string }> }, detail: string) {
  return table.columns.map((column) => ({
    label: `"${column.name}"`,
    type: 'property' as const,
    detail: `${detail}${column.type ? ` · ${column.type}` : ''}`,
    apply: propertyInsert(column.name),
    boost: 10,
  }))
}

function valueCompletions(table: { name: string; columns: Array<{ name: string; type?: string }> }, beforeCursor: string) {
  const keyMatch = beforeCursor.match(/"([^"]+)"\s*(?::|=>)\s*[^,\]}]*$/)
  const columnName = keyMatch?.[1]
  const column = table.columns.find((item) => item.name === columnName)
  const type = column?.type?.toLowerCase() ?? ''
  const options: Completion[] = [
    { label: 'null', type: 'constant', apply: 'null', boost: 6 },
    { label: '""', type: 'text', apply: '""', boost: 5 },
    { label: '0', type: 'constant', apply: '0', boost: 4 },
  ]
  if (type.includes('bool')) {
    options.unshift(
      { label: 'true', type: 'constant', apply: 'true', boost: 10 },
      { label: 'false', type: 'constant', apply: 'false', boost: 10 },
    )
  }
  if (type.includes('int') || type.includes('numeric') || type.includes('decimal') || type.includes('float')) {
    options.unshift({ label: '1', type: 'constant', apply: '1', boost: 9 })
  }
  if (type.includes('char') || type.includes('text') || type.includes('uuid')) {
    options.unshift({ label: '"value"', type: 'text', apply: '"value"', boost: 9 })
  }
  return options
}

const baseTheme = EditorView.theme({
  '&': {
    height: '100%',
    fontSize: '13px',
    fontFamily: '"JetBrains Mono", "Fira Mono", "Cascadia Code", monospace',
    background: 'transparent',
    color: '#e6edf7',
  },
  '.cm-scroller': {
    overflow: 'auto',
    lineHeight: '1.75',
  },
  '.cm-content': {
    minHeight: '420px',
    padding: '18px 0 28px',
    color: '#e6edf7',
    caretColor: '#8ee6d4',
  },
  '.cm-line': {
    padding: '0 20px',
    color: '#e6edf7',
  },
  '.cm-cursor': {
    borderLeftColor: '#8ee6d4',
  },
  '&.cm-focused': {
    outline: 'none',
  },
  '.cm-gutters': {
    background: 'rgba(9, 12, 18, 0.55)',
    borderRight: '1px solid rgba(255,255,255,0.06)',
    color: 'var(--text-muted)',
    minWidth: '56px',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    padding: '0 12px 0 10px',
  },
  '.cm-activeLineGutter': {
    background: 'rgba(255,255,255,0.04)',
    color: 'var(--text-primary)',
  },
  '.cm-activeLine': {
    background: 'rgba(255,255,255,0.03)',
  },
  '.cm-selectionBackground, ::selection': {
    background: 'var(--brand-ring) !important',
  },
  '.cm-matchingBracket': {
    background: 'rgba(142, 230, 212, 0.16)',
    color: '#f3fffc',
    outline: '1px solid rgba(142, 230, 212, 0.26)',
  },
  '.cm-nonmatchingBracket': {
    background: 'rgba(239, 68, 68, 0.16)',
    color: '#ffe7e7',
  },
  '.cm-panels': {
    background: 'transparent',
  },
  '.cm-tooltip-autocomplete': {
    border: '1px solid rgba(255,255,255,0.08)',
    borderRadius: '14px',
    overflow: 'hidden',
    background: 'rgba(19, 24, 32, 0.98)',
    boxShadow: '0 20px 40px rgba(0,0,0,0.35)',
  },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    background: 'rgba(92, 184, 165, 0.18)',
  },
})

const lightTheme = EditorView.theme({
  '&': { background: '#fcfcfb', color: '#1c1917' },
  '.cm-gutters': { background: '#f2f0ef', borderRight: '1px solid #e7e5e4' },
  '.cm-content': { color: '#1c1917', caretColor: '#2f6f66' },
  '.cm-line': { color: '#1c1917' },
  '.cm-activeLineGutter': { background: '#ece7e2' },
  '.cm-activeLine': { background: '#f6f1ec' },
  '.cm-cursor': { borderLeftColor: '#2f6f66' },
  '.cm-matchingBracket': {
    background: 'rgba(58, 157, 143, 0.12)',
    color: '#1c1917',
    outline: '1px solid rgba(58, 157, 143, 0.18)',
  },
  '.cm-tooltip-autocomplete': {
    background: '#ffffff',
    border: '1px solid #e7e5e4',
    boxShadow: '0 20px 40px rgba(28,25,23,0.10)',
  },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    background: 'rgba(58, 157, 143, 0.14)',
  },
})

function completionSource(context: CompletionContext) {
  const beforeCursor = currentLinePrefix(context)
  const word = context.matchBefore(/[A-Za-z0-9_."{}-]+/)
  const from = word ? word.from : context.pos
  const options: Completion[] = [...staticScriptCompletions()]
  const callInfo = currentArgumentIndex(beforeCursor)

  if (/(?:plan\.|\$plan->)(?:insert|update|delete)\(\s*["'][^"']*$/.test(beforeCursor)) {
    options.unshift(...tableNameCompletions())
  }

  const table = currentScriptTable(beforeCursor)
  if (table && /(?::|=>)\s*[^,\]}]*$/.test(beforeCursor)) {
    options.unshift(...valueCompletions(table, beforeCursor))
  }

  if (table && callInfo && /(?:{[^}]*$|\[[^\]]*$)/.test(beforeCursor)) {
    if (callInfo.kind === 'insert' && callInfo.index === 1) {
      options.unshift(...columnNameCompletions(table, 'insert field'))
    }
    if (callInfo.kind === 'update' && callInfo.index === 1) {
      options.unshift(...columnNameCompletions(table, 'match key'))
    }
    if (callInfo.kind === 'update' && callInfo.index === 2) {
      options.unshift(...columnNameCompletions(table, 'updated field'))
    }
    if (callInfo.kind === 'delete' && callInfo.index === 1) {
      options.unshift(...columnNameCompletions(table, 'match key'))
    }
  }

  if (!word && !context.explicit && options.length === 0) return null
  return {
    from,
    options,
  }
}

function makeExtensions(dark: boolean) {
  return [
    lineNumbers(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    indentOnInput(),
    bracketMatching(),
    closeBrackets(),
    highlightActiveLine(),
    autocompletion({ override: [completionSource] }),
    baseTheme,
    themeCompartment.of(dark ? oneDark : lightTheme),
    keymap.of([
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...historyKeymap,
      ...completionKeymap,
      indentWithTab,
      { key: 'Ctrl-Space', run: startCompletion },
      { key: 'Mod-Space', run: startCompletion },
      { key: 'Alt-/', run: startCompletion },
    ]),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        emit('update:modelValue', update.state.doc.toString())
        lineCount.value = update.state.doc.lines
      }
      if (update.docChanged || update.selectionSet) {
        const line = update.state.doc.lineAt(update.state.selection.main.head)
        cursorLine.value = line.number
        cursorColumn.value = update.state.selection.main.head - line.from + 1
      }
    }),
  ]
}

onMounted(() => {
  if (!editorEl.value) return
  const dark = props.darkMode ?? document.documentElement.getAttribute('data-theme') !== 'light'
  view = new EditorView({
    state: EditorState.create({
      doc: props.modelValue,
      extensions: makeExtensions(dark),
    }),
    parent: editorEl.value,
  })
  lineCount.value = view.state.doc.lines
  const line = view.state.doc.lineAt(view.state.selection.main.head)
  cursorLine.value = line.number
  cursorColumn.value = view.state.selection.main.head - line.from + 1
})

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

watch(() => props.modelValue, (newVal) => {
  if (!view) return
  if (view.state.doc.toString() !== newVal) {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: newVal },
    })
    lineCount.value = view.state.doc.lines
  }
})

watch(() => props.darkMode, (dark) => {
  if (!view) return
  view.dispatch({
    effects: themeCompartment.reconfigure(dark ? oneDark : lightTheme),
  })
})

watch(() => props.schemaTables, (tables) => {
  if (!tables?.length) {
    expandedSchemaTable.value = null
    return
  }
  if (expandedSchemaTable.value && tables.some((table) => table.name === expandedSchemaTable.value)) return
  expandedSchemaTable.value = tables[0]?.name ?? null
}, { immediate: true })
</script>

<template>
  <div class="script-editor-shell">
    <div class="script-editor-topbar">
      <div class="script-editor-window">
        <span class="script-editor-dot script-editor-dot--red" />
        <span class="script-editor-dot script-editor-dot--amber" />
        <span class="script-editor-dot script-editor-dot--green" />
      </div>
      <div class="script-editor-tab">
        <span class="script-editor-tab__icon">{{ currentLanguage() === 'python' ? 'PY' : currentLanguage() === 'php' ? 'PHP' : 'JS' }}</span>
        <span class="script-editor-tab__name">{{ fileLabel || `data-script.${currentLanguage() === 'python' ? 'py' : currentLanguage() === 'php' ? 'php' : 'js'}` }}</span>
      </div>
      <div class="script-editor-toolbar">
        <button class="script-editor-action" type="button" @click="sidePanelOpen = !sidePanelOpen">{{ sidePanelOpen ? 'Hide Panel' : 'Show Panel' }}</button>
        <button v-if="showPreviewButton !== false" class="script-editor-action script-editor-action--primary" type="button" @click="emit('preview-request')">Preview</button>
        <span class="script-editor-pill">{{ languageLabel() }}</span>
        <span class="script-editor-pill">{{ lineCount }} lines</span>
      </div>
    </div>
    <div class="script-editor-body" :class="{ 'script-editor-body--full': !sidePanelOpen }">
      <div ref="editorEl" class="script-editor-host" />
      <aside v-if="sidePanelOpen" class="script-editor-panel">
        <div class="script-editor-panel__head">
          <div class="script-editor-panel__title-row">
            <span>Schema</span>
            <span class="script-editor-panel__meta">{{ schemaSummary }}</span>
          </div>
          <input v-model="schemaQuery" class="script-editor-panel__search" type="text" placeholder="Search tables or columns" />
          <span class="script-editor-panel__hint">Click a table to insert a starter update. Expand only when you need columns.</span>
        </div>
        <div class="script-editor-panel__content">
          <div v-if="!schemaTables?.length" class="script-editor-empty">Select a connection and schema to load tables.</div>
          <div v-else-if="!filteredSchemaTables.length" class="script-editor-empty">No tables or columns matched your search.</div>
          <div v-for="table in filteredSchemaTables" :key="table.name" class="script-editor-schema" :class="{ 'script-editor-schema--active': table.isRelevant }">
            <div class="script-editor-schema__row">
              <button type="button" class="script-editor-schema__title" @click="insertSchemaTemplate(table.name)">{{ table.name }}</button>
              <div class="script-editor-schema__actions">
                <span class="script-editor-schema__count">{{ table.columns.length }} cols</span>
                <button type="button" class="script-editor-schema__toggle" @click="toggleSchemaTable(table.name)">
                  {{ expandedSchemaTable === table.name ? 'Hide' : 'Show' }}
                </button>
              </div>
            </div>
            <div class="script-editor-schema__preview">
              <button v-for="column in table.previewColumns" :key="column.name" type="button" class="script-editor-schema__col" @click="insertSchemaTemplate(table.name, column.name)">
                {{ column.name }}
              </button>
            </div>
            <div v-if="expandedSchemaTable === table.name" class="script-editor-schema__cols">
              <button v-for="column in table.columns" :key="column.name" type="button" class="script-editor-schema__col script-editor-schema__col--full" @click="insertSchemaTemplate(table.name, column.name)">
                <span>{{ column.name }}</span>
                <span v-if="column.type" class="script-editor-schema__type">{{ column.type }}</span>
              </button>
            </div>
          </div>
        </div>
      </aside>
    </div>
    <div class="script-editor-foot">
      <div class="script-editor-foot__left">
        <span>{{ placeholder || 'Use plan.insert / plan.update / plan.delete' }}</span>
      </div>
      <div class="script-editor-foot__right">
        <span>Ln {{ cursorLine }}, Col {{ cursorColumn }}</span>
        <span>`Tab` snippet jump</span>
        <span>`Ctrl+Space` autocomplete</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.script-editor-shell {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid color-mix(in srgb, var(--border) 84%, #5cb8a5 16%);
  border-radius: 18px;
  overflow: hidden;
  background:
    linear-gradient(180deg, rgba(255,255,255,0.03), rgba(255,255,255,0.01)),
    linear-gradient(180deg, rgba(8, 12, 18, 0.96), rgba(18, 22, 30, 0.98));
  box-shadow:
    inset 0 1px 0 rgba(255,255,255,0.04),
    0 18px 42px rgba(0,0,0,0.24);
}

.script-editor-topbar {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 14px;
  padding: 10px 14px;
  border-bottom: 1px solid rgba(255,255,255,0.06);
  background:
    linear-gradient(180deg, rgba(255,255,255,0.03), rgba(255,255,255,0)),
    rgba(5, 8, 14, 0.75);
}

.script-editor-window {
  display: flex;
  align-items: center;
  gap: 6px;
}

.script-editor-dot {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  display: inline-flex;
}

.script-editor-dot--red { background: #ff5f57; }
.script-editor-dot--amber { background: #febc2e; }
.script-editor-dot--green { background: #28c840; }

.script-editor-tab {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  max-width: fit-content;
  padding: 8px 12px;
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 12px 12px 0 0;
  background: rgba(255,255,255,0.04);
}

.script-editor-tab__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 22px;
  padding: 0 8px;
  border-radius: 7px;
  background: rgba(92, 184, 165, 0.16);
  color: #9de0d2;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.08em;
}

.script-editor-tab__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #e8edf5;
  font-size: 12px;
  font-weight: 600;
}

.script-editor-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.script-editor-pill {
  display: inline-flex;
  align-items: center;
  padding: 6px 10px;
  border-radius: 999px;
  background: rgba(255,255,255,0.04);
  border: 1px solid rgba(255,255,255,0.06);
  color: #b6c1cf;
  font-size: 11px;
  font-weight: 600;
}

.script-editor-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 7px 11px;
  border-radius: 10px;
  border: 1px solid rgba(255,255,255,0.08);
  background: rgba(255,255,255,0.04);
  color: #d9e2ee;
  font-size: 11px;
  font-weight: 700;
  cursor: pointer;
}

.script-editor-action--primary {
  background: linear-gradient(135deg, rgba(92, 184, 165, 0.26), rgba(92, 184, 165, 0.14));
  color: #f3fffc;
  border-color: rgba(92, 184, 165, 0.28);
}

.script-editor-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  min-height: 0;
}

.script-editor-body--full {
  grid-template-columns: minmax(0, 1fr);
}

.script-editor-host {
  min-height: 0;
  overflow: hidden;
  background:
    linear-gradient(90deg, rgba(255,255,255,0.02) 0, rgba(255,255,255,0.02) 56px, transparent 56px),
    linear-gradient(180deg, rgba(255,255,255,0.01), transparent);
}

.script-editor-host :deep(.cm-editor) {
  min-height: 420px;
}

.script-editor-host :deep(.cm-editor),
.script-editor-host :deep(.cm-scroller),
.script-editor-host :deep(.cm-content),
.script-editor-host :deep(.cm-line) {
  color: #e6edf7;
}

:global(html[data-theme='light']) .script-editor-host :deep(.cm-editor),
:global(html[data-theme='light']) .script-editor-host :deep(.cm-scroller),
:global(html[data-theme='light']) .script-editor-host :deep(.cm-content),
:global(html[data-theme='light']) .script-editor-host :deep(.cm-line) {
  color: #1c1917;
}

.script-editor-panel {
  border-left: 1px solid rgba(255,255,255,0.06);
  background:
    linear-gradient(180deg, rgba(255,255,255,0.03), rgba(255,255,255,0)),
    rgba(8, 12, 18, 0.78);
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.script-editor-panel__head {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  border-bottom: 1px solid rgba(255,255,255,0.06);
  color: #dce5f0;
  font-size: 11px;
  font-weight: 700;
}

.script-editor-panel__title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.script-editor-panel__meta {
  color: #91a0b1;
  font-size: 10px;
  font-weight: 600;
}

.script-editor-panel__search {
  width: 100%;
  padding: 8px 10px;
  border-radius: 10px;
  border: 1px solid rgba(255,255,255,0.08);
  background: rgba(255,255,255,0.04);
  color: #e6edf7;
  font-size: 11px;
}

.script-editor-panel__content {
  flex: 1;
  overflow: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.script-editor-schema {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border-radius: 12px;
  border: 1px solid rgba(255,255,255,0.06);
  background: rgba(255,255,255,0.03);
}

.script-editor-schema--active {
  border-color: rgba(92, 184, 165, 0.24);
  background: rgba(92, 184, 165, 0.08);
}

.script-editor-schema__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.script-editor-schema__title {
  border: none;
  background: none;
  color: #e6edf7;
  font-size: 12px;
  font-weight: 700;
  padding: 0;
  text-align: left;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1 1 auto;
}

.script-editor-schema__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  min-width: 0;
}

.script-editor-schema__count {
  color: #91a0b1;
  font-size: 10px;
  font-weight: 600;
}

.script-editor-schema__toggle {
  padding: 4px 8px;
  border-radius: 999px;
  border: 1px solid rgba(255,255,255,0.08);
  background: rgba(255,255,255,0.04);
  color: #d9e2ee;
  font-size: 10px;
  font-weight: 700;
  white-space: nowrap;
}

.script-editor-schema__preview {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.script-editor-schema__cols {
  display: grid;
  gap: 8px;
}

.script-editor-schema__col {
  padding: 5px 8px;
  border-radius: 999px;
  border: 1px solid rgba(255,255,255,0.06);
  background: rgba(255,255,255,0.04);
  color: #a8b4c2;
  font-size: 10px;
}

.script-editor-schema__col--full {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  border-radius: 10px;
  padding: 8px 10px;
  text-align: left;
}

.script-editor-schema__type {
  color: #91a0b1;
  font-size: 10px;
}

.script-editor-empty {
  padding: 12px;
  border-radius: 12px;
  border: 1px dashed rgba(255,255,255,0.10);
  color: #91a0b1;
  font-size: 11px;
}

.script-editor-panel__hint {
  color: #91a0b1;
  font-size: 10px;
  font-weight: 500;
}

.script-editor-foot {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 14px;
  border-top: 1px solid rgba(255,255,255,0.06);
  background:
    linear-gradient(180deg, rgba(255,255,255,0.02), rgba(255,255,255,0)),
    rgba(7, 11, 17, 0.85);
  font-size: 11px;
  color: #95a0ae;
}

.script-editor-foot__left,
.script-editor-foot__right {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

:global(html[data-theme='light']) .script-editor-shell {
  background:
    linear-gradient(180deg, rgba(255,255,255,0.82), rgba(255,255,255,0.95)),
    #f8f7f4;
  box-shadow:
    inset 0 1px 0 rgba(255,255,255,0.9),
    0 18px 42px rgba(28,25,23,0.08);
}

:global(html[data-theme='light']) .script-editor-topbar {
  border-bottom-color: #e7e5e4;
  background:
    linear-gradient(180deg, rgba(255,255,255,0.92), rgba(255,255,255,0.74)),
    #f5f5f4;
}

:global(html[data-theme='light']) .script-editor-tab {
  border-color: #e7e5e4;
  background: #ffffff;
}

:global(html[data-theme='light']) .script-editor-tab__name {
  color: #1c1917;
}

:global(html[data-theme='light']) .script-editor-pill {
  background: rgba(58, 157, 143, 0.08);
  border-color: rgba(58, 157, 143, 0.12);
  color: #2f6f66;
}

:global(html[data-theme='light']) .script-editor-action {
  background: #ffffff;
  border-color: #e7e5e4;
  color: #44403c;
}

:global(html[data-theme='light']) .script-editor-action--primary {
  background: rgba(58, 157, 143, 0.12);
  border-color: rgba(58, 157, 143, 0.18);
  color: #255d55;
}

:global(html[data-theme='light']) .script-editor-host {
  background:
    linear-gradient(90deg, rgba(28,25,23,0.02) 0, rgba(28,25,23,0.02) 56px, transparent 56px),
    #fcfcfb;
}

:global(html[data-theme='light']) .script-editor-foot {
  border-top-color: #e7e5e4;
  background: #f8f7f4;
  color: #6b645d;
}

:global(html[data-theme='light']) .script-editor-panel {
  border-left-color: #e7e5e4;
  background: #f8f7f4;
}

:global(html[data-theme='light']) .script-editor-panel__search {
  border-color: #e7e5e4;
  background: #ffffff;
  color: #1c1917;
}

:global(html[data-theme='light']) .script-editor-panel__head {
  border-bottom-color: #e7e5e4;
}
:global(html[data-theme='light']) .script-editor-schema {
  border-color: #e7e5e4;
  background: #ffffff;
}

:global(html[data-theme='light']) .script-editor-schema--active {
  border-color: rgba(58, 157, 143, 0.24);
  background: rgba(58, 157, 143, 0.08);
}

:global(html[data-theme='light']) .script-editor-schema__title {
  color: #1c1917;
}

:global(html[data-theme='light']) .script-editor-empty,
:global(html[data-theme='light']) .script-editor-schema__col,
:global(html[data-theme='light']) .script-editor-panel__hint,
:global(html[data-theme='light']) .script-editor-panel__meta,
:global(html[data-theme='light']) .script-editor-schema__count,
:global(html[data-theme='light']) .script-editor-schema__type {
  color: #6b645d;
}

:global(html[data-theme='light']) .script-editor-schema__col {
  border-color: #e7e5e4;
  background: #f8f7f4;
}

:global(html[data-theme='light']) .script-editor-schema__toggle {
  border-color: #e7e5e4;
  background: #ffffff;
  color: #44403c;
}

@media (max-width: 720px) {
  .script-editor-topbar {
    grid-template-columns: 1fr;
    justify-items: start;
  }

  .script-editor-body {
    grid-template-columns: 1fr;
  }

  .script-editor-panel {
    border-left: 0;
    border-top: 1px solid rgba(255,255,255,0.06);
  }

  .script-editor-foot {
    flex-direction: column;
    align-items: flex-start;
  }

  .script-editor-schema__row {
    align-items: flex-start;
  }

  .script-editor-schema__actions {
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
  }
}
</style>
