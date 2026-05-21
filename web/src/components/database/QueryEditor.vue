<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { EditorView, keymap, lineNumbers, drawSelection, highlightActiveLine, highlightSpecialChars } from '@codemirror/view'
import { EditorState, Compartment } from '@codemirror/state'
import { defaultKeymap, indentWithTab, history, historyKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, indentOnInput } from '@codemirror/language'
import { closeBrackets, autocompletion, closeBracketsKeymap, completionKeymap, startCompletion, type CompletionSource } from '@codemirror/autocomplete'
import { oneDark } from '@codemirror/theme-one-dark'
import { getActiveFunctionHint } from '@/utils/sqlFunctionHelp'

const props = defineProps<{
  modelValue: string
  darkMode?: boolean
  placeholder?: string
  schemaCompletion?: CompletionSource | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'run': [sql: string]
}>()

const editorEl = ref<HTMLElement>()
const functionHint = ref<string | null>(null)
let view: EditorView | null = null
const themeCompartment = new Compartment()
const completionCompartment = new Compartment()

function getActiveSQL(): string {
  if (!view) return props.modelValue
  const state = view.state
  const { from, to } = state.selection.main
  if (from !== to) return state.sliceDoc(from, to).trim()
  const full = state.doc.toString()
  const cursor = state.selection.main.head
  const stmts: Array<{ from: number; to: number }> = []
  let inSingle = false, inDouble = false, stmtStart = 0
  for (let i = 0; i < full.length; i++) {
    const ch = full[i]
    if (ch === "'" && !inDouble) inSingle = !inSingle
    else if (ch === '"' && !inSingle) inDouble = !inDouble
    else if (ch === ';' && !inSingle && !inDouble) {
      stmts.push({ from: stmtStart, to: i })
      stmtStart = i + 1
    }
  }
  stmts.push({ from: stmtStart, to: full.length })
  const match = stmts.find(s => cursor >= s.from && cursor <= s.to)
  return match ? full.slice(match.from, match.to).trim() : full.trim()
}

const baseTheme = EditorView.theme({
  '&': {
    height: '100%',
    fontSize: '13.5px',
    fontFamily: '"JetBrains Mono", "Fira Mono", "Cascadia Code", monospace',
  },
  '.cm-scroller': { overflow: 'auto', lineHeight: '1.65' },
  '.cm-content': { padding: '10px 0', minHeight: '60px' },
  '.cm-line': { padding: '0 16px' },
  '.cm-cursor': { borderLeftColor: 'var(--brand)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-gutters': {
    background: 'var(--bg-elevated)',
    borderRight: '1px solid var(--border)',
    color: 'var(--text-muted)',
    minWidth: '40px',
  },
  '.cm-activeLineGutter': { background: 'var(--bg-hover)' },
  '.cm-activeLine': { background: 'var(--bg-hover)' },
  '.cm-selectionBackground, ::selection': { background: 'var(--brand-ring) !important' },
})

const lightTheme = EditorView.theme({
  '&': { background: '#fafaf9' },
  '.cm-gutters': { background: '#f2f0ef' },
  '.cm-content': { color: '#1c1917' },
})

const darkThemeExt = oneDark

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
    completionCompartment.of(
      autocompletion({ override: props.schemaCompletion ? [props.schemaCompletion] : [] }),
    ),
    sql(),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
    baseTheme,
    themeCompartment.of(dark ? darkThemeExt : lightTheme),
    keymap.of([
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...historyKeymap,
      ...completionKeymap,
      indentWithTab,
      { key: 'Ctrl-Space', run: startCompletion },
      { key: 'Mod-Space', run: startCompletion },
      { key: 'Alt-/', run: startCompletion },
      { key: 'Ctrl-Enter', run: () => { emit('run', getActiveSQL()); return true } },
      { key: 'Mod-Enter', run: () => { emit('run', getActiveSQL()); return true } },
    ]),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        emit('update:modelValue', update.state.doc.toString())
      }
      if (update.docChanged || update.selectionSet) {
        functionHint.value = getActiveFunctionHint(
          update.state.doc.toString(),
          update.state.selection.main.head,
        )
      }
    }),
    EditorView.lineWrapping,
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
  functionHint.value = getActiveFunctionHint(props.modelValue, view.state.selection.main.head)
})

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

// Sync external value changes (e.g., when loading a history item)
watch(() => props.modelValue, (newVal) => {
  if (!view) return
  if (view.state.doc.toString() !== newVal) {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: newVal },
    })
    functionHint.value = getActiveFunctionHint(newVal, view.state.selection.main.head)
  }
})

// Switch theme on dark-mode toggle
watch(() => props.darkMode, (dark) => {
  if (!view) return
  view.dispatch({
    effects: themeCompartment.reconfigure(dark ? darkThemeExt : lightTheme),
  })
})

watch(() => props.schemaCompletion, (schemaCompletion) => {
  if (!view) return
  view.dispatch({
    effects: completionCompartment.reconfigure(
      autocompletion({ override: schemaCompletion ? [schemaCompletion] : [] }),
    ),
  })
})

defineExpose({ getActiveSQL })
</script>

<template>
  <div class="cm-shell">
    <div ref="editorEl" class="cm-host" />
    <div v-if="functionHint" class="cm-hintbar">{{ functionHint }}</div>
  </div>
</template>

<style scoped>
.cm-shell {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.cm-host {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.cm-host :deep(.cm-editor) {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.cm-host :deep(.cm-scroller) {
  flex: 1;
  min-height: 0;
}
.cm-hintbar {
  position: absolute;
  right: 12px;
  bottom: 10px;
  padding: 6px 10px;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.9);
  color: #e2e8f0;
  font-size: 11px;
  font-family: var(--mono, "JetBrains Mono", monospace);
  border: 1px solid rgba(148, 163, 184, 0.25);
  pointer-events: none;
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.18);
}
</style>
