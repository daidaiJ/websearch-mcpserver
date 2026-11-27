import { useSyncExternalStore } from 'react'

type Locale = 'zh' | 'en'
type Language = Locale | 'auto'
type Forms = Partial<Record<Intl.LDMLPluralRule, string>>
type Catalog = Record<string, string | Forms>
declare global {
  interface Window {
    __I18N__?: Catalog
    __DEFAULT_LOCALE__?: Locale
    __I18N_CATALOGS__?: Partial<Record<Locale, Catalog>>
  }
}
const key = 'websearch.language'
const listeners = new Set<() => void>()
const bootLocale: Locale = document.documentElement.lang.startsWith('zh') ? 'zh' : 'en'
let preference: Language = readPreference()
function readPreference(): Language {
  try {
    const stored = localStorage.getItem(key)
    if (stored === 'en' || stored === 'zh') return stored
  } catch { /* Preferences remain available in memory. */ }
  return 'auto'
}
export const getLanguage = () => preference
export const getLocale = (): Locale => preference === 'auto' ? window.__DEFAULT_LOCALE__ ?? bootLocale : preference
export function setLanguage(next: Language) {
  preference = next
  document.documentElement.lang = getLocale() === 'zh' ? 'zh-CN' : 'en'
  try { localStorage.setItem(key, next) } catch { /* Restricted browser storage. */ }
  listeners.forEach(notify => notify())
}
export function useLocale() {
  useSyncExternalStore(subscribe, getLanguage)
  return getLocale()
}
function subscribe(notify: () => void) {
  listeners.add(notify)
  return () => { listeners.delete(notify) }
}
export function t(id: string, values: Record<string, unknown> = {}): string {
  const locale = getLocale()
  const catalogs = window.__I18N_CATALOGS__
  const value = catalogs?.[locale]?.[id] ?? window.__I18N__?.[id] ?? catalogs?.en?.[id] ?? catalogs?.zh?.[id]
  if (value === undefined) return id
  const count = Number(values.count ?? values.Count ?? 0)
  const pattern = typeof value === 'string' ? value : value[new Intl.PluralRules(locale).select(count)] ?? value.other ?? id
  return pattern.replace(/\{\{\s*\.(\w+)\s*\}\}/g, (match, name: string) => Object.hasOwn(values, name) ? String(values[name]) : match)
}

