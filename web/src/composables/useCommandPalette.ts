import { ref } from 'vue'

const RECENT_KEY = 'nias:recentPages'
const RECENT_MAX = 6

const open = ref(false)
const recent = ref<string[]>(readRecent())

function readRecent(): string[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]')
    return Array.isArray(parsed) ? parsed.filter((v) => typeof v === 'string') : []
  } catch {
    return []
  }
}

function pushRecent(key: string) {
  recent.value = [key, ...recent.value.filter((k) => k !== key)].slice(0, RECENT_MAX)
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(recent.value))
  } catch {
    // storage unavailable — recents just won't persist
  }
}

export function useCommandPalette() {
  return {
    open,
    recent,
    pushRecent,
    show: () => { open.value = true },
    hide: () => { open.value = false },
    toggle: () => { open.value = !open.value },
  }
}
