import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import locale from '../../pkg/i18n/locales/web.en.json'
import { api } from './api'
import Overview, { type OverviewResponse } from './Overview'

vi.mock('./api', () => ({ api: vi.fn() }))
const request = vi.mocked(api)
const day = { day: '2026-11-27', requests: 17, successes: 13, failures: 4, cache_hits: 6, duration_ms: 4250 }
const overview: OverviewResponse = {
  today: day, trend: [{ ...day, day: '2026-11-25' }],
  sources: [
    { id: 'ddg', name: 'DuckDuckGo', active: true, status: 'healthy', actual_cooldown: { until: '2099-11-28T00:00:00Z', reason: 'HTTP 429' } },
    { id: 'tavily', name: 'Tavily', active: true, status: 'degraded', suspended: true, actual_cooldown: null },
    { id: 'hidden', name: 'Inactive provider', active: false, status: 'down', actual_cooldown: null },
  ],
  system: { status: 'degraded', enabled: 2, observed: 2, degraded: 1, down: 0, suspended: 1 },
  runtime: {
    version: 'phase-1-test', uptime_seconds: 73, mcp_stateless: true, mcp_resources: false,
    proxy: { mode: 'disabled', resolved: false, api_providers: false },
    llm: { enabled: false, model_id: '', timeout_seconds: 8 },
    mcp_hub: { enabled: false, fetch_mode: 'off', servers: [] },
  },
  configured_tools: ['smartsearch', 'cleanfetch', 'academicsearch', 'pdf_parser', 'file_search', 'search_history'].map(name => ({
    name, label: name, enabled: name !== 'search_history', registered: name === 'smartsearch', state: name === 'file_search' ? 'unprobed' : 'pending',
  })),
  tools: [{ name: 'cleanfetch', status: 'healthy', today: day, last_seen_at: '2026-11-27T10:00:00Z' }],
}

beforeEach(() => {
  window.__I18N_CATALOGS__ = { en: locale }
  request.mockReset()
})

afterEach(() => { delete window.__I18N_CATALOGS__ })

it('renders system, tools, source incidents, exact trend and grouped usage from the API', async () => {
  request.mockImplementation(async path => (path === 'overview' ? overview : { window_days: 3, usage: [
    { application: 'codex', project: 'Project Cedar', requests: 5, successes: 4, duration_ms: 250 },
    { application: 'codex', project: '', requests: 2, successes: 2, duration_ms: 300 },
  ] }) as never)
  const openSource = vi.fn()
  render(<Overview onSource={openSource} refresh={0} />)
  expect(await screen.findByText('phase-1-test')).toBeInTheDocument()
  expect(screen.getByText('1 / 2')).toBeInTheDocument()
  expect(screen.queryByText('Inactive provider')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /DuckDuckGo/ }))
  expect(openSource).toHaveBeenCalledWith('DuckDuckGo')
  const tools = screen.getByRole('region', { name: 'Tool modules' })
  const entries = within(tools).getAllByRole('listitem')
  expect(entries).toHaveLength(6)
  expect(within(entries[0]).getByText('Registration: Registered')).toBeInTheDocument()
  expect(within(entries[0]).getByText('Observation: No observations yet')).toBeInTheDocument()
  expect(within(entries[1]).getByText('Registration: Awaiting registration')).toBeInTheDocument()
  expect(within(entries[1]).getByText('Observation: Healthy')).toBeInTheDocument()
  const chart = screen.getByRole('group', { name: /use the left and right arrow keys/i })
  const latest = within(chart).getByRole('button', { name: /November 27.*Success 13.*Failure 4.*Cache hits 6.*Calls 17/ })
  fireEvent.pointerEnter(latest)
  expect(document.querySelector('.ov-tooltip')).toHaveTextContent('Cache hits6')
  latest.focus()
  fireEvent.keyDown(latest, { key: 'ArrowLeft' })
  expect(document.activeElement).toHaveAccessibleName(/November 26.*Calls 0/)
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
  expect(document.querySelector('.ov-tooltip')).toBeNull()
  expect(await screen.findByText('Last 3 days · MCP tool calls')).toBeInTheDocument()
  expect(screen.getByRole('row', { name: /Codex 7/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Project' }))
  expect(screen.getByRole('row', { name: /Project Cedar 5/ })).toBeInTheDocument()
  expect(request).toHaveBeenCalledWith('clients?days=7', expect.objectContaining({ signal: expect.any(AbortSignal) }))
})

it('keeps telemetry and attribution failures independent, retries, and renders empty data', async () => {
  request.mockImplementation(async path => {
    if (path === 'overview') throw new Error('Telemetry unavailable')
    return { window_days: 7, usage: null } as never
  })
  render(<Overview onSource={vi.fn()} refresh={0} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('Telemetry unavailable')
  expect(await screen.findByText('No calls in this window yet')).toBeInTheDocument()
  request.mockImplementation(async path => (path === 'overview' ? { ...overview, today: { ...day, requests: 0 }, trend: null } : { window_days: 7, usage: null }) as never)
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  expect(await screen.findByText('No tool-layer events yet')).toBeInTheDocument()
  expect(screen.queryByRole('row', { name: /Project Cedar/ })).not.toBeInTheDocument()
})
