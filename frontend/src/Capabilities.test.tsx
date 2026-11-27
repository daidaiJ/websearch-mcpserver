import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Capabilities from './Capabilities';
import { CapabilityContext, type CapabilityOverview } from './CapabilityData';
import Diagnostics from './Diagnostics';
import { DraftProvider, useDraft } from './draft';

const values = {
  'smartsearch.enhance': true, 'smartsearch.max_size': 12, 'black_list_host': ['existing.example'],
  'smartsearch.show_meta': true, 'smartsearch.relevance_threshold': 0.5, 'everything.max_results': 10,
};
const settings = {
  values, field_files: Object.fromEntries(Object.keys(values).map(key => [key, 'config.yaml'])),
  locked_fields: { 'smartsearch.show_meta': 'Managed by the service' }, revision: 'r1', admin_password_configured: true,
  admin_username_configured: true, write_access: { allowed: true }, local_action_allowed: true,
};

const data: CapabilityOverview = {
  diagnostics: { sources: [{ id: 'tavily', name: 'Tavily', confirm: true }, { id: 'pubmed', name: 'PubMed', confirm: false }], fetch_services: ['tinyfish', 'jina'] },
  configured_tools: [
    { name: 'smartsearch', enabled: true, registered: true, state: 'registered' },
    { name: 'cleanfetch', enabled: true, registered: true, state: 'registered', fetch_enabled: true },
    { name: 'file_search', enabled: true, registered: false, state: 'pending', file: { connectivity: 'available', allowed_root_count: 1, whitelist_enforced: true, path_metadata_only: true } },
  ],
  runtime: { proxy: { mode: 'auto', resolved: true, endpoint: 'http://proxy.example:80' } },
};

const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>();
const reload = vi.fn();
const noPosts = () => fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST').length === 0;

function DraftState() { return <output data-testid="draft-state">{JSON.stringify(useDraft().draft)}</output>; }

function mount(children: ReactNode) {
  return render(<DraftProvider><CapabilityContext.Provider value={{ data, loading: false, reload }}>
    {children}<DraftState />
  </CapabilityContext.Provider></DraftProvider>);
}

function prepareRequests(routes: Record<string, unknown>) {
  fetchMock.mockImplementation(async url => {
    if (url === '/__admin/api/settings') return response(settings);
    if (Object.hasOwn(routes, url)) return response(routes[url]);
    return response({ error: 'Unexpected request' }, 500);
  });
}

beforeEach(() => { fetchMock.mockReset(); reload.mockReset(); vi.stubGlobal('fetch', fetchMock); });
afterEach(() => { vi.unstubAllGlobals(); });

describe('Capabilities', () => {
  it('stages a results setting, splits multi-line hosts, removes tags and keeps locked parameters read-only', async () => {
    prepareRequests({});
    const user = userEvent.setup();
    mount(<Capabilities refresh={0} />);

    const results = await screen.findByRole('textbox', { name: 'Max results' });
    await user.clear(results);
    await user.type(results, '25');
    expect(screen.getByTestId('draft-state')).toHaveTextContent('"smartsearch.max_size":25');
    expect(screen.getByText('Registered')).toBeInTheDocument();
    expect(screen.getByText('Awaiting real calls')).toBeInTheDocument();

    const hosts = screen.getByRole('textbox', { name: 'Global blocked domains' });
    await user.click(hosts);
    await user.paste('ALPHA.example\n beta.example.\nalpha.example');
    expect(screen.getByTestId('draft-state')).toHaveTextContent('"black_list_host":["existing.example","alpha.example","beta.example"]');
    await user.click(screen.getByRole('button', { name: 'Remove existing.example' }));
    expect(screen.getByTestId('draft-state')).not.toHaveTextContent('existing.example');
    await user.type(hosts, 'https://invalid.example/path{Enter}');
    expect(screen.getByRole('alert')).toHaveTextContent('Enter domains without schemes');
    expect(screen.getByTestId('draft-state')).not.toHaveTextContent('invalid.example');

    await user.click(screen.getByText('Advanced parameters'));
    const locked = screen.getByRole('switch', { name: 'Show source and relevance score' });
    expect(locked).toBeDisabled();
    expect(locked).toHaveAccessibleDescription('Managed by the service');
    expect(noPosts()).toBe(true);
  });

  it('probes Everything without credentials and opens its read-only scope only after admin authorization', async () => {
    prepareRequests({
      '/__admin/api/diagnostics/everything': { ok: true, elapsed_ms: 3 },
      '/__admin/api/everything/scope': { url: 'https://everything.example', roots: ['D:/sample-private-root'] },
    });
    const user = userEvent.setup();
    mount(<Capabilities item="file_search" refresh={0} />);
    const probe = await screen.findByRole('button', { name: 'Probe Everything again' });
    await waitFor(() => expect(probe).toBeEnabled());
    await user.click(probe);
    expect(await screen.findByText('Reachable')).toBeInTheDocument();
    expect(reload).toHaveBeenCalledTimes(1);
    const [, probeInit] = fetchMock.mock.calls.find(([url]) => url.endsWith('/diagnostics/everything'))!;
    expect(new Headers(probeInit?.headers).has('X-Admin-Password')).toBe(false);

    await user.click(screen.getByRole('button', { name: 'View search scope' }));
    const dialog = screen.getByRole('dialog', { name: 'View search scope' });
    expect(fetchMock.mock.calls.some(([url]) => url.endsWith('/everything/scope'))).toBe(false);
    await user.type(within(dialog).getByLabelText('Admin username'), 'admin');
    await user.type(within(dialog).getByLabelText('Admin password'), 'test-password');
    await user.click(within(dialog).getByRole('button', { name: 'View search scope' }));
    expect(await within(dialog).findByText('D:/sample-private-root')).toBeInTheDocument();
    expect(within(dialog).queryByRole('textbox')).toBeNull();
    const [, scopeInit] = fetchMock.mock.calls.find(([url]) => url.endsWith('/everything/scope'))!;
    expect(new Headers(scopeInit?.headers).get('X-Admin-Password')).toBe('test-password');
    expect(new Headers(scopeInit?.headers).get('X-Admin-User')).toBe('admin');
    await user.keyboard('{Escape}');
    expect(screen.queryByText('D:/sample-private-root')).toBeNull();
  });
});

describe('Diagnostics', () => {
  it('waits for confirmation before requesting a paid source and renders bounded search results', async () => {
    prepareRequests({ '/__admin/api/diagnostics/search': {
      ok: true, elapsed_ms: 64, result_count: 4, titles: ['First match', 'Second match', 'Third match', 'Hidden fourth'],
    } });
    const user = userEvent.setup();
    mount(<Diagnostics item="tavily" refresh={0} />);
    const query = screen.getByRole('textbox', { name: 'Test query' });
    await waitFor(() => expect(query).toBeEnabled());
    expect(screen.getByRole('combobox', { name: 'Search source' })).toHaveTextContent('Tavily');
    await user.type(query, 'test phrase');
    const submit = screen.getByRole('button', { name: 'Run one search' });
    await user.click(submit);
    const dialog = screen.getByRole('dialog', { name: 'Confirm possible quota use' });
    expect(within(dialog).getByText(/one search with Tavily/)).toBeInTheDocument();
    expect(noPosts()).toBe(true);
    await user.keyboard('{Escape}');
    expect(noPosts()).toBe(true);
    await waitFor(() => expect(submit).toHaveFocus());
    await user.click(submit);
    await user.click(screen.getByRole('button', { name: 'Confirm and run' }));
    expect(await screen.findByText('Third match')).toBeInTheDocument();
    expect(screen.getByText('64 ms')).toBeInTheDocument();
    expect(screen.queryByText('Hidden fourth')).toBeNull();
    const [, request] = fetchMock.mock.calls.find(([url]) => url.endsWith('/diagnostics/search'))!;
    expect(JSON.parse(String(request?.body))).toEqual({ source: 'tavily', query: 'test phrase', confirm: true });
  });

  it('renders only PubMed article links and shows proxy reachability separately', async () => {
    prepareRequests({
      '/__admin/api/diagnostics/search': { ok: true, elapsed_ms: 6, result_count: 2, links: [
        { title: 'PMID 42', url: 'https://pubmed.ncbi.nlm.nih.gov/42/' }, { title: 'Unexpected link', url: 'https://elsewhere.example/43/' },
      ] },
      '/__admin/api/diagnostics/proxy': { ok: false, elapsed_ms: 25, endpoint: 'http://proxy.example:80', error_kind: 'timeout' },
    });
    const user = userEvent.setup();
    mount(<Diagnostics item="pubmed" refresh={0} />);
    const query = screen.getByRole('textbox', { name: 'Test query' });
    await waitFor(() => expect(query).toBeEnabled());
    await user.type(query, 'test phrase');
    await user.click(screen.getByRole('button', { name: 'Run one search' }));
    expect(await screen.findByRole('link', { name: 'PMID 42' })).toHaveAttribute('href', 'https://pubmed.ncbi.nlm.nih.gov/42/');
    expect(screen.queryByRole('link', { name: 'Unexpected link' })).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Check proxy connection' }));
    expect(await screen.findByText('Unreachable')).toBeInTheDocument();
    expect(screen.getByText('Timeout')).toBeInTheDocument();
  });

  it('confirms the live fetch services before sending and shows the node sequence, winning node and body status', async () => {
    prepareRequests({ '/__admin/api/diagnostics/fetch': {
      ok: true, elapsed_ms: 209, fetch: { attempts: ['webfetch', 'jina'], node: 'jina', body_status: 'available', characters: 590, remote_redirects: true },
    } });
    const user = userEvent.setup();
    mount(<Diagnostics refresh={0} />);
    const url = screen.getByRole('textbox', { name: 'Public page URL' });
    await waitFor(() => expect(url).toBeEnabled());
    await user.type(url, 'https://example.org/page');
    await user.click(screen.getByRole('button', { name: 'Try fetching a page' }));
    const dialog = screen.getByRole('dialog', { name: 'Confirm possible quota use' });
    expect(within(dialog).getByText(/TinyFish, Jina Reader/)).toBeInTheDocument();
    expect(noPosts()).toBe(true);
    await user.click(within(dialog).getByRole('button', { name: 'Confirm and run' }));
    const attempts = await screen.findByRole('list', { name: 'Attempt order' });
    const nodes = within(attempts).getAllByRole('listitem');
    expect(nodes[0]).toHaveTextContent('WebFetch');
    expect(nodes[1]).toHaveAttribute('aria-current', 'step');
    expect(nodes[1]).toHaveTextContent('Jina Reader');
    expect(screen.getByText('About 590 characters')).toBeInTheDocument();
    expect(screen.getByText('209 ms')).toBeInTheDocument();
  });
});
