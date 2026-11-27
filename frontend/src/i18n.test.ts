import { expect, it } from 'vitest'
import { getLocale, setLanguage, t } from './i18n'

it('uses the negotiated locale, substitutes placeholders and selects CLDR forms', () => {
  const original = window.__I18N_CATALOGS__
  try {
    window.__I18N_CATALOGS__ = { en: { sample: { one: '{{.Count}} result', other: '{{.Count}} results' }, fallback: 'Fallback' }, zh: { sample: '{{.Count}} 条' } }
    setLanguage('zh')
    expect(t('sample', { Count: 2 })).toBe('2 条')
    expect(t('fallback')).toBe('Fallback')
    setLanguage('auto')
    expect(getLocale()).toBe('en')
    expect(t('sample', { Count: 1 })).toBe('1 result')
    expect(t('sample', { Count: 2 })).toBe('2 results')
    expect(t('missing')).toBe('missing')
  } finally { window.__I18N_CATALOGS__ = original; setLanguage('auto') }
})
