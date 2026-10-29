import { ref, computed, watch, onMounted } from 'vue'
import { darkTheme, type GlobalTheme, type GlobalThemeOverrides } from 'naive-ui'

type Theme = 'dark' | 'light'

const theme = ref<Theme>('dark')

// Mode alias used by some components
const mode = computed(() => theme.value)

// Naive UI theme integration
const naiveTheme = computed<GlobalTheme | null>(() =>
  theme.value === 'dark' ? darkTheme : null,
)

// Mirrors the Anveesa family tokens in styles/main.css (see DESIGN.md) so Naive UI widgets match.
const PALETTE = {
  dark: {
    primary: '#38bdf8', primaryHover: '#7dd3fc', primaryPressed: '#0ea5e9',
    body: '#0b1422', card: '#0f1a2c', modal: '#14213a', popover: '#14213a',
    border: 'rgba(148, 190, 235, 0.17)', divider: 'rgba(148, 190, 235, 0.09)',
    text1: '#e6eef8', text2: '#a3b4c9', text3: '#8a9cb4',
    error: '#fb7185', warning: '#fbbf24', success: '#4ade80', info: '#38bdf8',
  },
  light: {
    primary: '#0369a1', primaryHover: '#075985', primaryPressed: '#0c4a6e',
    body: '#ffffff', card: '#ffffff', modal: '#ffffff', popover: '#ffffff',
    border: '#d0dce8', divider: '#e3ebf3',
    text1: '#0d1b2a', text2: '#4b5d71', text3: '#5f7186',
    error: '#be123c', warning: '#b45309', success: '#166534', info: '#0369a1',
  },
} as const

const themeOverrides = computed<GlobalThemeOverrides>(() => {
  const p = PALETTE[theme.value]
  return {
    common: {
      fontFamily: "'Figtree', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
      fontFamilyMono: "'IBM Plex Mono', ui-monospace, 'SF Mono', Menlo, Consolas, monospace",
      primaryColor: p.primary,
      primaryColorHover: p.primaryHover,
      primaryColorPressed: p.primaryPressed,
      primaryColorSuppl: p.primary,
      errorColor: p.error,
      warningColor: p.warning,
      successColor: p.success,
      infoColor: p.info,
      bodyColor: p.body,
      cardColor: p.card,
      modalColor: p.modal,
      popoverColor: p.popover,
      borderColor: p.border,
      dividerColor: p.divider,
      textColor1: p.text1,
      textColor2: p.text2,
      textColor3: p.text3,
      borderRadius: '8px',
      borderRadiusSmall: '6px',
    },
  }
})

function applyTheme(t: Theme) {
  document.documentElement.setAttribute('data-theme', t)
  localStorage.setItem('nias-theme', t)
}

watch(theme, applyTheme)

/** Called once on app mount to load persisted preference */
function syncTheme() {
  const saved = localStorage.getItem('nias-theme') as Theme | null
  if (saved === 'light' || saved === 'dark') {
    theme.value = saved
  } else {
    theme.value = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  applyTheme(theme.value)
}

function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
}

export function useTheme() {
  onMounted(() => {
    // Reapply in case composable is used before App.vue mounts
    const saved = localStorage.getItem('nias-theme') as Theme | null
    if (saved) applyTheme(saved)
  })

  return { theme, mode, naiveTheme, themeOverrides, syncTheme, toggleTheme }
}
