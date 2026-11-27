import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import english from '../../pkg/i18n/locales/web.en.json';
import { DraftProvider, useDraft, type SettingsView } from './draft';
import { setLanguage, t } from './i18n';
import { CommandRegistry, NavigationContext, type Command, type Destination } from './navigation';
import Sources from './Sources';
import SourcesCommands, { SourcesProvider, type QuotaItem, type Source } from './SourcesCommands';

const makeSource = (id: string, name: string, input: Partial<Source> = {}): Source => ({
  id, name, group: 'web', category: 'engine', enabled: true, configured: true, active: true,
  participation: 'Participates in hybrid search', condition: 'A valid configuration is required',
  status: 'healthy', observation_status: 'healthy', last_seen_at: '2026-01-01T01:00:00Z', suspended: false,
  recent_outcomes: [true, false, true], today: { day: '2026-01-01', requests: 3, successes: 2, failures: 1, duration_ms: 1905 },
  p95_ms: 900, quota_queryable: false, error_kinds: [{ kind: 'rate_limit', count: 1 }], ...input,
});

const apiSource = (id: string, name: string, input: Partial<Source> = {}) => makeSource(id, name, { category: 'api', control_path: `${id}.enabled`, quota_queryable: true, ...input });
const tavily = apiSource('tavily', 'Tavily', { suspended: true, suspend_reason: 'rate_limit', observation_status: 'degraded', score_key: 'tavily' });
const tinyfish = apiSource('tinyfish', 'TinyFish');
const bing = makeSource('bing', 'Bing', { control_path: 'bing.enabled', score_key: 'bing' });
const arxiv = makeSource('arxiv', 'arXiv', { group: 'academic', category: 'academic', inverted: true, control_path: 'academic.disable_arxiv' });
const hub = makeSource('hub', 'Content Hub', { category: 'mcphub', configured: false, enabled: false, active: false });
const weights = { 'smartsearch.engines.tavily.weight': 1, 'smartsearch.engines.tavily.min_score': .3 };
const raw = 'tavily.include_raw_content';

function setup(options: { settings?: Partial<SettingsView>; sources?: Source[]; quotas?: QuotaItem[]; item?: string; commandOnly?: boolean } = {}) {
  const values = { 'bing.enabled': true, 'tavily.enabled': true, 'tinyfish.enabled': true, 'academic.disable_arxiv': false, ...weights, [raw]: true };
  const settings: SettingsView = {
    revision: 'r1', values, saved_values: values, write_access: { allowed: true }, admin_password_configured: true,
    local_action_allowed: true, field_files: Object.fromEntries(Object.keys(values).map(key => [key, 'config.yaml'])),
    locked_fields: { 'smartsearch.engines.tavily.min_score': 'Set by environment' }, ...options.settings,
  };
  let sources = options.sources ?? [bing, tavily, tinyfish, arxiv, hub];
  const response = (payload: object) => Response.json(payload);
  const transport = vi.fn(async (url: string, request?: RequestInit) => {
    if (url.endsWith('/settings')) return response(settings);
    if (url.endsWith('/overview')) return response({
      sources: sources.filter(source => source.category !== 'academic'), academic_sources: sources.filter(source => source.category === 'academic'),
      diagnostics: { sources: [{ id: 'tavily', name: 'Tavily', confirm: true }] },
    });
    if (url.endsWith('/quotas/refresh')) {
      const { provider } = JSON.parse(String(request?.body));
      return response({ items: (options.quotas ?? []).filter(item => !provider || item.provider === provider), refreshed: true });
    }
    if (url.endsWith('/quotas')) return response(options.quotas ?? []);
    if (url.endsWith('/sources/cooldowns/release')) {
      const { engine } = JSON.parse(String(request?.body));
      sources = sources.map(source => engine === 'all' || source.actual_cooldown?.engine === engine ? { ...source, actual_cooldown: null } : source);
      return response({ cleared: 1 });
    }
    throw new Error(url);
  });
  vi.stubGlobal('fetch', transport);
  const go = vi.fn<(destination: Destination) => void>();
  let commands: readonly Command[] = [];
  const register = (entries: readonly Command[]) => { commands = entries; return () => { commands = []; }; };

  function DraftSnapshot() {
    const draft = useDraft();
    return <output aria-label="Draft values">{JSON.stringify(draft.draft)}</output>;
  }

  const rendered = render(<DraftProvider><SourcesProvider><NavigationContext.Provider value={go}><CommandRegistry.Provider value={register}>
    {!options.commandOnly && <Sources item={options.item} refresh={0} />}<SourcesCommands /><DraftSnapshot />
  </CommandRegistry.Provider></NavigationContext.Provider></SourcesProvider></DraftProvider>);
  return { ...rendered, transport, go, commands: () => commands };
}

const sourceButton = (name: string) => screen.findByRole('button', { name });
const openSource = async (name: string) => { fireEvent.click(await sourceButton(name)); return screen.findByRole('dialog', { name }); };

beforeEach(() => { window.__I18N__ = english; setLanguage('en'); });
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

describe('Sources workspace', () => {
  it('composes category, state and keyword filters across views without inventing calls', async () => {
    const { container } = setup();
    await sourceButton('Bing');
    fireEvent.click(screen.getByRole('button', { name: /^API providers / }));
    expect(screen.queryByRole('button', { name: 'Bing' })).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(t('web.sources.status_filter')), { target: { value: 'issues' } });
    expect(await sourceButton('Tavily')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'TinyFish' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: t('web.sources.list') }));
    expect(container.querySelector('[data-view="list"]')).toBeInTheDocument();
    expect(container.querySelectorAll('[data-outcome]')).toHaveLength(20);
    expect(container.querySelectorAll('[data-outcome="success"]')).toHaveLength(2);
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'tIvy' } });
    expect(screen.getByText(t('web.sources.no_matches'))).toBeInTheDocument();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'tav' } });
    expect(await sourceButton('Tavily')).toBeInTheDocument();
  });

  it('stages inverted switches and source fields while preserving the drawer and environment locks', async () => {
    const { go } = setup();
    await sourceButton('arXiv');
    const toggle = screen.getByRole('switch', { name: /^arXiv/ });
    await waitFor(() => expect(toggle).toBeEnabled());
    fireEvent.click(toggle);
    expect(screen.getByLabelText('Draft values')).toHaveTextContent('"academic.disable_arxiv":true');
    const drawer = await openSource('Tavily');
    const weight = within(drawer).getByRole('spinbutton', { name: t('web.config.engine_weight') });
    fireEvent.change(weight, { target: { value: '3.5' } });
    expect(screen.getByLabelText('Draft values')).toHaveTextContent('"smartsearch.engines.tavily.weight":3.5');
    expect(within(drawer).getByRole('spinbutton', { name: t('web.config.engine_min_score') })).toBeDisabled();
    expect(within(drawer).getByText('Set by environment')).toBeInTheDocument();
    expect(within(drawer).getAllByRole('listitem')).toHaveLength(21);
    const diagnose = within(drawer).getByRole('link', { name: t('web.sources.open_diagnostic') });
    expect(diagnose).toHaveAttribute('href', '#diagnostics/tavily');
    fireEvent.click(diagnose);
    expect(go).toHaveBeenCalledWith({ page: 'diagnostics', item: 'tavily' });
    const launch = await sourceButton('Bing');
    fireEvent.click(launch);
    const other = await screen.findByRole('dialog', { name: 'Bing' });
    fireEvent.keyDown(within(other).getByRole('button', { name: t('web.next.close') }), { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(launch).toHaveFocus());
  });

  it('separates official credits, local counts, and a partial TinyFish response', async () => {
    setup({ quotas: [
      { provider: 'tavily', configured: true, status: 'available', source: 'tavily', unit: 'credits', used: 800, limit: 1000, plan: 'Growth',
        details: { account_plan_usage: 800, account_plan_limit: 1000, key_usage: 80, key_limit: 100 }, local_usage: { status: 'available', unit: 'calls', used: 20, limit: 20, enforced: true } },
      { provider: 'tinyfish', configured: true, status: 'available', source: 'local', unit: 'calls', official_query_failed: true, used: 15, limit: 20,
        details: { fetch_used: 900, fetch_limit: 1000 }, local_usage: { status: 'available', unit: 'calls', used: 15, limit: 20, enforced: false } },
    ] });
    const drawer = await openSource('Tavily');
    const official = await within(drawer).findByRole('region', { name: t('web.quota.official') });
    expect(within(official).getByText('Growth')).toBeInTheDocument();
    expect(within(official).getByText(t('web.quota.key_usage'))).toBeInTheDocument();
    expect(within(official).getByRole('progressbar', { name: /credits · 800/ }).closest('[data-severity]')).toHaveAttribute('data-severity', 'near');
    const local = within(drawer).getByRole('region', { name: t('web.quota.local') });
    expect(within(local).getByText(t('web.quota.limit_reached'))).toBeInTheDocument();
    const tiny = await openSource('TinyFish');
    expect(within(tiny).getByText(new RegExp(t('web.sources.official_search_failed')))).toBeInTheDocument();
    expect(within(tiny).getByRole('progressbar', { name: /^Fetch · 900/ })).toBeInTheDocument();
  });

  it('keeps cooldown operations separate from administrator writes and sends no admin headers', async () => {
    const blue = makeSource('bing', 'Bing', { actual_cooldown: { engine: 'bing', kind: 'rate_limit', reason: 'HTTP 429', until: new Date(Date.now() + 18000).toISOString(), remaining_seconds: 18 } });
    const { transport } = setup({ sources: [blue, tavily], settings: { admin_password_configured: false, write_access: { allowed: false } } });
    await sourceButton('Bing');
    expect(screen.getByText(t('web.status.observation_warning'))).toHaveAttribute('data-tone', 'observed');
    expect(screen.getByText(/Routing cooldown · \d+ s/)).toHaveAttribute('data-tone', 'cooldown');
    const release = screen.getByRole('button', { name: t('web.runtime_action.release_all') });
    await waitFor(() => expect(release).toBeEnabled());
    fireEvent.click(release);
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: t('web.action.confirm') }));
    await screen.findByText(t('web.runtime_action.released', { Count: 1 }));
    expect(screen.queryByText(/Routing cooldown · \d+ s/)).not.toBeInTheDocument();
    const request = transport.mock.calls.find(([url]) => url.endsWith('/sources/cooldowns/release'))?.[1];
    expect(JSON.parse(String(request?.body))).toEqual({ engine: 'all', confirm: true });
    expect(new Headers(request?.headers).has('X-Admin-Password')).toBe(false);
  });

  it('expires timed cooldowns and disables runtime actions on disallowed connections', async () => {
    const start = Date.now();
    const blocked = makeSource('bing', 'Bing', { actual_cooldown: { engine: 'bing', kind: 'rate_limit', reason: '', until: new Date(start + 2000).toISOString(), remaining_seconds: 2 } });
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] }).setSystemTime(start);
    const { transport } = setup({ sources: [blocked], settings: { local_action_allowed: false } });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(screen.getByText(/Routing cooldown · 2 s/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: t('web.runtime_action.release_all') })).toBeDisabled();
    expect(screen.getByText(t('web.runtime_action.source_not_allowed'))).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText(/Routing cooldown · 1 s/)).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.queryByText(/Routing cooldown ·/)).not.toBeInTheDocument();
    expect(transport.mock.calls.every(([, request]) => request?.method !== 'POST')).toBe(true);
  });

  it('indexes sources and available runtime actions outside the source page', async () => {
    const { commands, go } = setup({ commandOnly: true });
    await waitFor(() => expect(commands().some(command => command.id === 'source-tavily')).toBe(true));
    await act(async () => { commands().find(command => command.id === 'source-tavily')!.run(); });
    expect(go).toHaveBeenCalledWith({ page: 'sources', item: 'tavily' });
    await act(async () => { commands().find(command => command.id === 'sources-refresh-quotas')!.run(); });
    expect(screen.getByRole('dialog', { name: t('web.runtime_action.refresh_quota') })).toBeInTheDocument();
  });
});
