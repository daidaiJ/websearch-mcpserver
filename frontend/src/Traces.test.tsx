import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import locale from '../../pkg/i18n/locales/web.en.json'
import { api } from './api'
import Traces from './Traces'

vi.mock('./api', () => ({ api: vi.fn() }))
const request = vi.mocked(api)
const when = '2026-11-20T12:00:00+08:00'
const cursor = (id: number) => `${Date.parse(when) / 1000}:${id}`
const call = (id: number, requestId?: string) => ({
  id, occurred_at: when, kind: 'tool', tool: 'smartsearch', provider: '', success: true, duration_ms: 980,
  cache_hit: false, result_count: 4, request_id: requestId, application: 'codex', project: 'Paper <review>',
})
const attempt = (id: number, requestId: string) => ({
  ...call(id, requestId), kind: 'provider', provider: 'arxiv', success: false, duration_ms: 430,
  error_kind: 'timeout', error_summary: 'Timeout after waiting',
})
const catalog = { sources: [{ id: 'arxiv', name: 'arXiv' }], configured_tools: [{ name: 'smartsearch', label: 'Smart search' }] }
const clients = { usage: [{ application: 'codex', project: 'Paper <review>', tool: 'smartsearch' }] }
const catalogFor = (path: string) => path === 'overview' ? catalog : clients

beforeEach(() => {
  window.__I18N_CATALOGS__ = { en: locale }
  request.mockReset()
})

afterEach(() => {
  delete window.__I18N_CATALOGS__
  vi.unstubAllGlobals()
})

it('combines all seven filters against the API and clears them together', async () => {
  const user = userEvent.setup()
  request.mockImplementation(async path => (path.startsWith('calls?') ? [call(5)] : catalogFor(path)) as never)
  render(<Traces refresh={0} />)
  expect(await screen.findByText('Paper <review>')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Success' }))
  for (const [label, choice] of [['Error kind', 'Timeout'], ['Tool', 'Smart search'], ['Source', 'arXiv'], ['Application', 'Codex'], ['Project', 'Paper <review>'], ['Cache', 'Miss']]) {
    await user.click(screen.getByRole('button', { name: label }))
    await user.click(within(screen.getByRole('dialog', { name: label })).getByRole('button', { name: choice }))
  }
  const callsPath = request.mock.calls.map(([path]) => path).filter(path => path.startsWith('calls?')).at(-1)!
  expect(Object.fromEntries(new URLSearchParams(callsPath.split('?')[1]))).toEqual({
    status: 'success', error_kind: 'timeout', tool: 'smartsearch', provider: 'arxiv', application: 'codex',
    project: 'Paper <review>', cache_hit: 'false', limit: '50',
  })
  await user.click(screen.getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(request).toHaveBeenCalledWith('calls?limit=50', expect.any(Object)))
  expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
})

it('loads full request attempts and lets the operator switch to a historical call with no ID', async () => {
  const user = userEvent.setup()
  request.mockImplementation(async path => (
    path.startsWith('calls?') ? [call(5, 'req-first'), call(4)] :
      path.startsWith('request-events?') ? [attempt(6, 'req-first'), call(5, 'req-first')] : catalogFor(path)
  ) as never)
  render(<Traces refresh={0} />)
  const rows = await screen.findAllByRole('button', { name: /^Inspect smartsearch/ })
  await user.click(rows[0])
  const drawer = screen.getByRole('dialog', { name: 'Call details' })
  expect(await within(drawer).findByText('Timeout after waiting')).toBeInTheDocument()
  expect(within(drawer).getByText('arXiv')).toBeInTheDocument()
  expect(drawer.querySelector<HTMLElement>('.trace-attempt-track i')?.style.width).toMatch(/%$/)
  expect(request).toHaveBeenCalledWith('request-events?request_id=req-first', expect.any(Object))
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.spyOn(navigator.clipboard, 'writeText').mockImplementation(writeText)
  await user.click(within(drawer).getByRole('button', { name: 'Copy request ID' }))
  expect(await within(drawer).findByText('Request ID copied.')).toBeInTheDocument()
  expect(writeText).toHaveBeenCalledWith('req-first')
  await user.keyboard('{ArrowDown}')
  expect(within(drawer).getByText(/has no request ID/)).toBeInTheDocument()
  expect(request.mock.calls.filter(([path]) => path.startsWith('request-events?'))).toHaveLength(1)
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog', { name: 'Call details' })).not.toBeInTheDocument()
  await waitFor(() => expect(rows[1]).toHaveFocus())
})

it('exports all filtered pages with retained attempts, protects formulas, and aborts a failed later detail', async () => {
  const first = Array.from({ length: 50 }, (_, index) => call(51 - index))
  const last = { ...call(1, 'req-last'), project: '=SUM(1,2)', query_hash: 'only-a-hash' }
  let fail = false
  request.mockImplementation(async path => {
    if (path.startsWith('calls?')) {
      const query = new URLSearchParams(path.split('?')[1])
      if (!query.has('before')) return first as never
      expect(query.get('status')).toBe('success')
      expect(query.get('before')).toBe(cursor(2))
      return [last] as never
    }
    if (path.startsWith('request-events?')) {
      if (fail) throw new Error('detail unavailable')
      return [last, attempt(52, 'req-last')] as never
    }
    return catalogFor(path) as never
  })
  const create = vi.fn((_blob: Blob) => 'blob:test')
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = create
    static revokeObjectURL = vi.fn()
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  render(<Traces refresh={0} />)
  await screen.findByText('50 loaded')
  fireEvent.click(screen.getByRole('button', { name: 'Success' }))
  await screen.findByText('50 loaded')
  fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }))
  expect(await screen.findByText('Exported 52 call and source records.')).toBeInTheDocument()
  expect(create).toHaveBeenCalledTimes(1)
  const contents = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = reject
    reader.readAsText(create.mock.calls[0][0])
  })
  expect(contents.split('\r\n')).toHaveLength(53)
  expect(contents).toContain('Timeout after waiting')
  expect(contents).toContain('"\'=SUM(1,2)"')
  expect(contents).toContain('only-a-hash')
  fail = true
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Export CSV' })) })
  expect(await screen.findByRole('alert')).toHaveTextContent('Export stopped before download. detail unavailable')
  expect(create).toHaveBeenCalledTimes(1)
})
