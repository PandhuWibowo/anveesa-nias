<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
// Import the core editor API only — the top-level 'monaco-editor' entry point
// registers all ~70 bundled languages (TypeScript, CSS, JSON workers, etc.),
// bloating the bundle by megabytes for a view that only needs a custom
// Monarch-based nginx-conf language.
import * as monaco from 'monaco-editor/editor/editor.api.js'
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker'
import { useTheme } from '@/composables/useTheme'

const props = defineProps<{ modelValue: string; readonly?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { mode } = useTheme()
const editorEl = ref<HTMLElement>()
let editor: monaco.editor.IStandaloneCodeEditor | null = null

// Monaco boots one shared worker for all editors on the page; nginx uses no
// language service (no IntelliSense worker needed), just the base worker.
if (!(self as any).MonacoEnvironment) {
  ;(self as any).MonacoEnvironment = { getWorker: () => new EditorWorker() }
}

const LANG_ID = 'nginx-conf'
if (!monaco.languages.getLanguages().some((l) => l.id === LANG_ID)) {
  monaco.languages.register({ id: LANG_ID })
  monaco.languages.setMonarchTokensProvider(LANG_ID, {
    defaultToken: '',
    tokenizer: {
      root: [
        [/#.*$/, 'comment'],
        [/\$[a-zA-Z_][\w]*/, 'variable'],
        [/"([^"\\]|\\.)*"/, 'string'],
        [/'([^'\\]|\\.)*'/, 'string'],
        [/\b\d+[kKmMgGsmhd]?\b/, 'number'],
        [/[{}]/, 'delimiter.bracket'],
        [/;/, 'delimiter'],
        [/^\s*[a-zA-Z_][\w.]*(?=\s)/, 'keyword'],
      ],
    },
  })
  monaco.languages.setLanguageConfiguration(LANG_ID, {
    comments: { lineComment: '#' },
    brackets: [['{', '}']],
    autoClosingPairs: [
      { open: '{', close: '}' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  })
  monaco.editor.defineTheme('nginx-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: 'keyword', foreground: 'c586c0' },
      { token: 'variable', foreground: '9cdcfe' },
      { token: 'string', foreground: 'ce9178' },
      { token: 'number', foreground: 'b5cea8' },
      { token: 'comment', foreground: '6a9955' },
    ],
    colors: {},
  })
  monaco.editor.defineTheme('nginx-light', {
    base: 'vs',
    inherit: true,
    rules: [
      { token: 'keyword', foreground: 'af00db' },
      { token: 'variable', foreground: '001080' },
      { token: 'string', foreground: 'a31515' },
      { token: 'number', foreground: '098658' },
      { token: 'comment', foreground: '008000' },
    ],
    colors: {},
  })
}

function themeName() {
  return mode.value === 'dark' ? 'nginx-dark' : 'nginx-light'
}

onMounted(() => {
  if (!editorEl.value) return
  editor = monaco.editor.create(editorEl.value, {
    value: props.modelValue,
    language: LANG_ID,
    theme: themeName(),
    readOnly: !!props.readonly,
    automaticLayout: true,
    minimap: { enabled: true },
    fontSize: 12.5,
    fontFamily: 'var(--mono, monospace)',
    scrollBeyondLastLine: false,
    renderWhitespace: 'selection',
  })
  editor.onDidChangeModelContent(() => {
    const val = editor!.getValue()
    if (val !== props.modelValue) emit('update:modelValue', val)
  })
})

onBeforeUnmount(() => {
  editor?.dispose()
  editor = null
})

watch(() => props.modelValue, (val) => {
  if (editor && editor.getValue() !== val) {
    editor.setValue(val)
  }
})

watch(() => props.readonly, (ro) => {
  editor?.updateOptions({ readOnly: !!ro })
})

watch(mode, () => {
  monaco.editor.setTheme(themeName())
})
</script>

<template>
  <div ref="editorEl" class="ng-monaco"></div>
</template>

<style scoped>
.ng-monaco { height: 100%; min-height: 440px; }
</style>
