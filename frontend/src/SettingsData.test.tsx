import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import locale from '../../pkg/i18n/locales/web.en.json'
import { api } from './api'
import { DraftProvider, useDraft, type SettingsView } from './draft'
import SettingsQuota from './SettingsQuota'
import SettingsStorage from './SettingsStorage'

vi.mock('./api', () => ({ api: vi.fn(), ApiError: class extends Error {} }))
const request = vi.mocked(api)
const reset = 'dashboard.quotas.reset'
const resetDay = 'dashboard.quotas.reset_day'
const enforce = 'dashboard.quotas.enforce_providers'
const limits = 'dashboard.quotas.limits'
const baseSettings: SettingsView = {
  values: { [reset]: 'monthly', [resetDay]: 2, [enforce]: ['tavily'], [limits]: { tavily: 100 } },
  field_files: { [reset]: 'dashboard.yaml', [resetDay]: 'dashboard.yaml', [enforce]: 'dashboard.yaml', [limits]: 'dashboard.yaml' },
  options: { [reset]: ['monthly', 'weekly', 'daily', 'none'], [enforce]: ['tavily', 'tinyfish', 'exa'] },
  revision: 'fixture-revision',
  admin_password_configured: true,
  admin_username_configured: true,
  write_access: { allowed: true },
  local_action_allowed: true,
}

function DraftOutput() {
  const draft = useDraft()
  return <output data-testid="staged-settings">{JSON.stringify(draft.draft)}</output>
}

beforeEach(() => {
  window.__I18N_CATALOGS__ = { en: locale }
  request.mockReset()
})
afterEach(() => { delete window.__I18N_CATALOGS__ })

it('stages quota changes and writes only the confirmed local ledger with shared admin credentials', async () => {
  const user = userEvent.setup()
  let used = 42
  request.mockImplementation(async (path, init) => {
    if (path === 'settings') return baseSettings as never
    if (path === 'quotas') return [
      { provider: 'tavily', configured: true, status: 'available', source: 'https://official.invalid', unit: 'credits', used: 15, limit: 50,
        local_usage: { status: 'available', unit: 'calls', used, limit: 100, remaining: 100 - used, auto_reset: 'monthly', enforced: true },
      },
      { provider: 'tinyfish', configured: true, status: 'available', source: 'https://official.invalid', unit: 'calls', used: 10, limit: 1000 },
    ] as never
    const body = JSON.parse(String(init?.body))
    expect(new Headers(init?.headers).get('X-Admin-Password')).toBe('fixture-password')
    expect(new Headers(init?.headers).get('X-Admin-User')).toBe('test-user')
    expect(body.provider).toBe('tavily')
    used = path === 'quotas/adjust' ? body.set_used : 0
    return {} as never
  })
  render(<DraftProvider><SettingsQuota refresh={0} /><DraftOutput /></DraftProvider>)
  const line = await screen.findByRole('row', { name: /Tavily/ })
  expect(within(line).getByText('15 / 50 credits')).toBeInTheDocument()
  expect(within(line).getByText('42 / 100 calls')).toBeInTheDocument()
  expect(within(screen.getByRole('row', { name: /TinyFish/ })).queryByRole('button')).not.toBeInTheDocument()

  const period = screen.getByRole('combobox', { name: 'Quota reset cycle' })
  await user.selectOptions(period, 'weekly')
  expect(screen.getByRole('spinbutton', { name: 'Monthly reset day' })).toBeDisabled()
  const cap = screen.getByRole('spinbutton', { name: 'Tavily per-key limit' })
  fireEvent.change(cap, { target: { value: '0' } })
  expect(cap).toHaveAttribute('aria-invalid', 'true')
  fireEvent.change(cap, { target: { value: '200' } })
  await user.click(screen.getByRole('checkbox', { name: 'Exa' }))
  expect(JSON.parse(screen.getByTestId('staged-settings').textContent!)).toMatchObject({
    [reset]: 'weekly', [enforce]: ['tavily', 'exa'], [limits]: { tavily: 200 },
  })

  const adjust = screen.getByRole('button', { name: 'Adjust Tavily local usage' })
  await user.click(adjust)
  const dialog = screen.getByRole('dialog', { name: 'Adjust Tavily local usage' })
  expect(within(dialog).getByText('Current local usage: 42')).toBeInTheDocument()
  expect(request.mock.calls.some(([route]) => route === 'quotas/adjust')).toBe(false)
  fireEvent.change(within(dialog).getByRole('spinbutton', { name: 'New used count' }), { target: { value: '19' } })
  await user.type(within(dialog).getByLabelText('Username'), 'test-user')
  await user.type(within(dialog).getByLabelText('Password'), 'fixture-password')
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
  expect(await screen.findByText('Tavily local ledger now shows 19 used in the current period.')).toBeInTheDocument()
  expect(within(line).getByText('15 / 50 credits')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Reset Tavily local usage' }))
  const resetDialog = screen.getByRole('dialog', { name: 'Reset Tavily local usage' })
  expect(within(resetDialog).queryByLabelText('Password')).not.toBeInTheDocument()
  await user.click(within(resetDialog).getByRole('button', { name: 'Confirm' }))
  expect(await screen.findByText('Tavily local ledger now shows 0 used in the current period.')).toBeInTheDocument()
  expect(within(line).getByText('15 / 50 credits')).toBeInTheDocument()
})

it('confirms and rechecks expired, eligible, and total filesystem counts plus both SQLite counts without admin headers', async () => {
  const user = userEvent.setup()
  let cache = 6
  let history = 5
  let files = 5
  let expired = 2
  let clearable = 4
  request.mockImplementation(async (path, init) => {
    if (path === 'settings') return { ...baseSettings, admin_password_configured: false } as never
    if (path === 'storage') return {
      cache: { enabled: true, count: cache, bytes: 8192 }, history: { enabled: true, count: history, bytes: 4096 },
      fetchdata: { enabled: true, count: files, bytes: 3072, expired, clearable },
    } as never
    const body = JSON.parse(String(init?.body))
    expect(body.confirm).toBe(true)
    expect(new Headers(init?.headers).has('X-Admin-Password')).toBe(false)
    let cleared: number
    if (path === 'cache/clear') { cleared = cache; cache = 0 }
    else if (path === 'history/clear') { cleared = history; history = 0 }
    else if (body.scope === 'expired') { cleared = expired; clearable -= cleared; files -= cleared; expired = 0 }
    else { expect(body.scope).toBe('all'); cleared = clearable; files -= cleared; clearable = 0 }
    return { cleared } as never
  })
  render(<DraftProvider><SettingsStorage refresh={0} /></DraftProvider>)
  const temporary = await screen.findByRole('region', { name: 'Temporary outputs' })
  expect(within(temporary).getByText('3 KiB')).toBeInTheDocument()
  for (const [button, expected] of [
    ['Clear expired', 'Latest: 3 files, 0 expired, 2 eligible for cleanup.'],
    ['Clear all', 'Latest: 1 files, 0 expired, 0 eligible for cleanup.'],
    ['Clear search cache', 'Cleared 6 items. Latest count: 0 items.'],
    ['Clear search history', 'Cleared 5 items. Latest count: 0 items.'],
  ]) {
    await user.click(screen.getByRole('button', { name: button }))
    const dialog = screen.getByRole('dialog', { name: button })
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText(expected, { exact: false })).toBeInTheDocument()
  }
  expect(screen.getByRole('button', { name: 'Clear expired' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Clear all' })).toBeDisabled()
  const cachePanel = screen.getByRole('region', { name: 'Search cache' })
  expect(within(cachePanel).getByText('8 KiB')).toBeInTheDocument()
})

it('fails closed when runtime permission is unavailable', async () => {
  request.mockImplementation(async route => (route === 'settings' ? { ...baseSettings, local_action_allowed: false } : {
    cache: { enabled: true, count: 8, bytes: 1024 }, history: { enabled: true, count: 3, bytes: 1024 },
    fetchdata: { enabled: true, count: 4, bytes: 1024, expired: 2, clearable: 3 },
  }) as never)
  render(<DraftProvider><SettingsStorage refresh={0} /></DraftProvider>)
  await waitFor(() => expect(screen.getByRole('button', { name: 'Clear expired' })).toBeDisabled())
  expect(await screen.findByText('Runtime actions are available from loopback or an allowed write network.')).toBeInTheDocument()
  expect(request.mock.calls.every(([, init]) => init?.method !== 'POST')).toBe(true)
})
