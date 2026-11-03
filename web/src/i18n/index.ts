// A small i18n layer with no dependencies. English is the source of truth and
// the fallback for any key a locale hasn't translated yet, so a page can be
// translated one string at a time without ever showing a raw key.
//
//   t('auth.signIn')                    typed key from en.ts
//   t('home.manageAll', { n: 12 })      {name} placeholders
//   tr(`nav.${key}`, item.label)        dynamic key with an inline English fallback
//
// Calling t()/tr() during render reads `locale`, so templates re-render on switch.

import { ref, watch } from 'vue'
import en, { type MessageKey } from './locales/en'
import zhCN from './locales/zh-CN'

export type { MessageKey }

export type Locale = 'en' | 'zh-CN'

/** Each locale names itself, so the picker is readable whatever is selected. */
export const LOCALES: { code: Locale; label: string }[] = [
  { code: 'en', label: 'English' },
  { code: 'zh-CN', label: '简体中文' },
]

const messages: Record<Locale, Partial<Record<string, string>>> = { en, 'zh-CN': zhCN }
const STORAGE_KEY = 'nias-locale'

function isLocale(v: unknown): v is Locale {
  return LOCALES.some((l) => l.code === v)
}

/** Saved choice first, then the browser's languages; English otherwise. */
function detect(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLocale(saved)) return saved
  } catch { /* storage unavailable */ }
  const langs = typeof navigator !== 'undefined' ? navigator.languages ?? [navigator.language] : []
  for (const lang of langs) {
    if (/^zh\b/i.test(lang)) return 'zh-CN'
    if (/^en\b/i.test(lang)) return 'en'
  }
  return 'en'
}

export const locale = ref<Locale>(detect())

function applyDocumentLang(l: Locale) {
  if (typeof document !== 'undefined') document.documentElement.lang = l
}
applyDocumentLang(locale.value)

watch(locale, (l) => {
  applyDocumentLang(l)
  try { localStorage.setItem(STORAGE_KEY, l) } catch { /* storage unavailable */ }
})

export function setLocale(l: Locale) {
  if (isLocale(l)) locale.value = l
}

type Params = Record<string, string | number>

function interpolate(text: string, params?: Params) {
  if (!params) return text
  return text.replace(/\{(\w+)\}/g, (m, k) => (k in params ? String(params[k]) : m))
}

/** Translate a known key; falls back to English. */
export function t(key: MessageKey, params?: Params): string {
  const text = messages[locale.value][key] ?? en[key] ?? key
  return interpolate(text, params)
}

/** Translate a dynamic key, falling back to the given English text. */
export function tr(key: string, fallback: string, params?: Params): string {
  const text = messages[locale.value][key] ?? fallback
  return interpolate(text, params)
}

/** Locale-aware formatting helpers built on Intl. */
export function formatNumber(n: number) {
  return new Intl.NumberFormat(locale.value).format(n)
}

export function useI18n() {
  return { t, tr, locale, setLocale, LOCALES, formatNumber }
}
