<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { EditorView, keymap, lineNumbers, drawSelection, highlightActiveLine, highlightSpecialChars } from '@codemirror/view'
import { EditorState, Compartment } from '@codemirror/state'
import { defaultKeymap, indentWithTab, history, historyKeymap } from '@codemirror/commands'
import { StreamLanguage, bracketMatching, indentOnInput } from '@codemirror/language'
import { search, searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { oneDark } from '@codemirror/theme-one-dark'

const props = defineProps<{ modelValue: string; readonly?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const editorEl = ref<HTMLElement>()
let view: EditorView | null = null
const readonlyCompartment = new Compartment()

// A small nginx-mode tokenizer: the first word of each statement is a directive
// (keyword), $vars, strings, sizes/numbers, braces and comments are coloured.
const nginxLanguage = StreamLanguage.define<{ expectDirective: boolean }>({
  startState: () => ({ expectDirective: true }),
  token(stream, state) {
    if (stream.eatSpace()) return null
    const ch = stream.peek()
    if (ch === '#') {
      stream.skipToEnd()
      return 'comment'
    }
    if (ch === '$') {
      stream.next()
      stream.eatWhile(/[\w]/)
      return 'variable-2'
    }
    if (ch === '"' || ch === "'") {
      const q = stream.next()
      while (!stream.eol()) {
        const c = stream.next()
        if (c === q) break
        if (c === '\\') stream.next()
      }
      return 'string'
    }
    if (ch === '{' || ch === '}') {
      stream.next()
      state.expectDirective = true
      return 'bracket'
    }
    if (ch === ';') {
      stream.next()
      state.expectDirective = true
      return 'punctuation'
    }
    if (stream.match(/^[0-9]+[kKmMgGsmhd]?\b/)) return 'number'
    if (stream.match(/^[~^=@*]+/)) return 'operator'
    if (stream.match(/^[^\s{};#'"]+/)) {
      if (state.expectDirective) {
        state.expectDirective = false
        return 'keyword'
      }
      return null
    }
    stream.next()
    return null
  },
})

const theme = EditorView.theme({
  '&': { height: '100%', fontSize: '12.5px' },
  '.cm-scroller': { fontFamily: 'var(--mono, monospace)', lineHeight: '1.55' },
  '.cm-content': { minHeight: '440px' },
  '&.cm-focused': { outline: 'none' },
})

function buildState(doc: string) {
  return EditorState.create({
    doc,
    extensions: [
      lineNumbers(),
      highlightSpecialChars(),
      history(),
      drawSelection(),
      indentOnInput(),
      bracketMatching(),
      highlightActiveLine(),
      highlightSelectionMatches(),
      search({ top: true }),
      nginxLanguage,
      oneDark,
      theme,
      readonlyCompartment.of(EditorState.readOnly.of(!!props.readonly)),
      EditorView.editable.of(!props.readonly),
      keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
      }),
    ],
  })
}

onMounted(() => {
  if (!editorEl.value) return
  view = new EditorView({ state: buildState(props.modelValue), parent: editorEl.value })
})

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

watch(() => props.modelValue, (val) => {
  if (view && view.state.doc.toString() !== val) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: val } })
  }
})

watch(() => props.readonly, (ro) => {
  view?.dispatch({ effects: readonlyCompartment.reconfigure(EditorState.readOnly.of(!!ro)) })
})
</script>

<template>
  <div ref="editorEl" class="ng-cm"></div>
</template>

<style scoped>
.ng-cm { height: 100%; min-height: 440px; overflow: hidden; }
.ng-cm :deep(.cm-editor) { height: 100%; }
</style>
