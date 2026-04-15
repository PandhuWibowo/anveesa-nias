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

const themeOverrides = computed<GlobalThemeOverrides>(() => ({
  common: {
    primaryColor: '#6366f1',
    primaryColorHover: '#818cf8',
    primaryColorPressed: '#4f46e5',
    primaryColorSuppl: '#6366f1',
    borderRadius: '6px',
  },
}))

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
