import { getLocale } from './i18n'

// Reuse ICU objects across rows, chart tooltips and countdown ticks.
function forLocale(locale: string) {
  const date = (options: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(locale, options)
  return {
    count: new Intl.NumberFormat(locale),
    decimal: new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }),
    percent: new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }),
    day: date({ month: 'numeric', day: 'numeric', timeZone: 'UTC' }),
    longDay: date({ month: 'long', day: 'numeric', timeZone: 'UTC' }),
    timestamp: date({ dateStyle: 'short', timeStyle: 'medium' }),
    logTime: date({ month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' }),
    clock: date({ timeStyle: 'medium' }),
  }
}

const formats = new Map<string, ReturnType<typeof forLocale>>()
export function formatters(locale: string = getLocale()) {
  let result = formats.get(locale)
  if (!result) {
    result = forLocale(locale)
    formats.set(locale, result)
  }
  return result
}
