import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import en from '../../pkg/i18n/locales/web.en.json'
import zh from '../../pkg/i18n/locales/web.zh.json'

window.__I18N_CATALOGS__ = { en, zh }
window.__DEFAULT_LOCALE__ = 'en'
document.documentElement.lang = 'en'
if (!window.matchMedia) Object.defineProperty(window, 'matchMedia', { configurable: true, value: (media: string) =>
  ({ media, matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} }) })
if (!window.ResizeObserver) window.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }
if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {}
afterEach(() => cleanup())

