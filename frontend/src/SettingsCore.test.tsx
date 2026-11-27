import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DraftBar } from './DraftBar';
import { DraftProvider, useDraft, type SettingsView } from './draft';
import { NavigationContext } from './navigation';
import Settings from './Settings';
import SettingsRoute from './SettingsRoute';
import SettingsService from './SettingsService';

const settings: SettingsView = {
  values: { mode: 'hybrid', 'apipool.engines': ['tavily', 'anysearch'], 'apipool.strategy': 'priority', 'apipool.weights': {},
    language: 'en', log_level: 'info', 'proxy.enabled': true, 'dashboard.client_projects': {}, 'dashboard.brand.title': '' },
  revision: 'revision-first', admin_password_configured: true, admin_username_configured: true, write_access: { allowed: true },
  field_files: { mode: 'config.yaml', 'apipool.engines': 'config.yaml', 'apipool.strategy': 'config.yaml', 'apipool.weights': 'config.yaml',
    language: 'config.yaml', log_level: 'config.yaml', 'proxy.enabled': 'config.yaml', 'dashboard.client_projects': 'dashboard.yaml',
    'dashboard.brand.title': 'dashboard.yaml' },
  options: { mode: ['hybrid', 'engine', 'apipool', 'mcphub', 'baidu', 'tavily', 'exa', 'anysearch', 'doubao', 'yuanbao'],
    'apipool.engines': ['tavily', 'anysearch', 'exa'], 'apipool.strategy': ['round-robin', 'priority', 'weighted'],
    log_level: ['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic', 'disabled'] },
  backups: [{ name: 'settings-20260215T120000.bak', created_at: '2026-02-15T12:00:00Z' }],
};

const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const request = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>();
const targetBackup = settings.backups![0].name;

function DraftStatus() {
  const { draft, settings: current } = useDraft();
  return <>
    <output data-testid="working-draft">{JSON.stringify(draft)}</output>
    <output data-testid="runtime-settings">{JSON.stringify(current?.values ?? {})}</output>
  </>;
}

function mount(component: ReactNode, includeBar = false) {
  return render(<DraftProvider>{component}<DraftStatus />{includeBar && <DraftBar />}</DraftProvider>);
}

beforeEach(() => {
  request.mockReset();
  request.mockImplementation(async () => response(settings));
  vi.stubGlobal('fetch', request);
});
afterEach(() => vi.unstubAllGlobals());

describe('Settings routing and service configuration', () => {
  it('shows the shared administrator notice once and retains field locks across settings subpages', async () => {
    const reason = 'No administrator password is configured';
    request.mockImplementation(async () => response({ ...settings, admin_password_configured: false,
      write_access: { allowed: false, reason }, locked_fields: { log_level: 'Managed by the service environment' },
      pending_changes: { language: 'zh' },
    }));
    const page = (item: string) => <DraftProvider><NavigationContext.Provider value={vi.fn()}>
      <Settings item={item} refresh={0} /><DraftBar showWriteReason={false} />
    </NavigationContext.Provider></DraftProvider>;
    const view = render(page('service'));
    expect(await screen.findByText(reason)).toBeInTheDocument();
    expect(screen.getAllByText(reason)).toHaveLength(1);
    const logLevel = screen.getByRole('combobox', { name: 'Log level' });
    expect(logLevel).toBeDisabled();
    expect(logLevel).toHaveAccessibleDescription('Managed by the service environment');
    expect(screen.getByRole('combobox', { name: 'Proxy policy' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Restart WebSearch' })).toBeDisabled();

    view.rerender(page('routing'));
    expect(screen.getAllByText(reason)).toHaveLength(1);
    expect(screen.getByRole('radio', { name: 'hybrid' })).toBeDisabled();
  });

  it('changes route modes and stages the reordered provider list without writing', async () => {
    const user = userEvent.setup();
    mount(<SettingsRoute refresh={0} />);
    const apiPool = screen.getByRole('radio', { name: 'apipool' });
    await waitFor(() => expect(apiPool).toBeEnabled());
    expect(screen.getByRole('radio', { name: 'hybrid' })).toBeChecked();
    await user.click(apiPool);
    await user.click(screen.getByRole('button', { name: 'Move up AnySearch' }));
    expect(screen.getByTestId('working-draft')).toHaveTextContent('"apipool.engines":["anysearch","tavily"]');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Scheduling strategy' }), 'weighted');
    const weights = screen.getByRole('textbox', { name: 'Provider weights' });
    await user.type(weights, 'anysearch = 2{Enter}tavily = 1');
    expect(screen.getByTestId('working-draft')).toHaveTextContent('"apipool.weights":{"anysearch":2,"tavily":1}');
    await user.click(screen.getByRole('radio', { name: 'Single provider' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Provider' }), 'anysearch');
    expect(screen.getByTestId('working-draft')).toHaveTextContent('"mode":"anysearch"');
    expect(request.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false);
  });

  it('stages an explicit proxy back to automatic and keeps the current server settings', async () => {
    const user = userEvent.setup();
    mount(<SettingsService refresh={0} />);
    const proxy = screen.getByRole('combobox', { name: 'Proxy policy' });
    await waitFor(() => expect(proxy).toBeEnabled());
    expect(proxy).toHaveValue('enabled');
    await user.selectOptions(proxy, 'auto');
    expect(screen.getByTestId('working-draft')).toHaveTextContent('"proxy.enabled":null');
    expect(screen.getByTestId('runtime-settings')).toHaveTextContent('"proxy.enabled":true');
  });
});

describe('Settings maintenance', () => {
  it('previews an authenticated backup, handles conflict, and verifies saved and running values after restart', async () => {
    let revision = settings.revision;
    let restored = false;
    let restarted = false;
    let runtimeReads = 0;
    const restores: { confirm: boolean; revision: string; restore_backup: string }[] = [];
    request.mockImplementation(async (path, init) => {
      if (path === '/__admin/api/settings' && init?.method !== 'POST') {
        const active = restarted && runtimeReads >= 1;
        const language = restored ? (active ? 'auto' : 'zh') : revision === settings.revision ? 'en' : 'zh';
        const values = { ...settings.values, language };
        const saved_values = restored ? { ...values, language: 'auto' } : values;
        return response({ ...settings, values, saved_values, revision,
          pending_changes: restored && !active ? { language: 'auto' } : {},
          backups: restored ? [...settings.backups!, { name: 'rollback-before-restore.bak', created_at: '2026-02-15T12:01:00Z' }] : settings.backups,
        });
      }
      if (path === '/__admin/api/settings') {
        const body = JSON.parse(String(init?.body));
        restores.push(body);
        const headers = new Headers(init?.headers);
        if (headers.get('X-Admin-User') !== 'admin' || headers.get('X-Admin-Password') !== 'scope-token') return response({ error: 'Denied' }, 403);
        if (body.confirm && revision === settings.revision) {
          revision = 'revision-latest';
          return response({ error: 'Configuration changed' }, 409);
        }
        if (body.confirm) { restored = true; revision = 'revision-restored'; }
        return response({ applied: body.confirm, changes: { language: 'auto' }, field_files: { language: 'config.yaml' }, revision, restart_required: true,
          backup: body.confirm ? 'rollback-before-restore.bak' : undefined,
        });
      }
      if (path === '/__admin/api/restart') { restarted = true; return response({ restarting: true }, 202); }
      if (path === '/__admin/api/overview') return response({ runtime: { started_at: restarted && runtimeReads++ >= 1 ? 'process-new' : 'process-old' } });
      return response({ error: 'Unexpected request' }, 500);
    });
    const user = userEvent.setup();
    mount(<SettingsService refresh={0} />, true);

    const level = screen.getByRole('combobox', { name: 'Log level' });
    await waitFor(() => expect(level).toBeEnabled());
    await user.selectOptions(level, 'debug');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Choose a backup' }), targetBackup);
    await user.click(screen.getByRole('button', { name: 'Review backup changes' }));
    const dialog = screen.getByRole('dialog', { name: 'Restore config backup' });
    expect(restores).toHaveLength(0);
    await user.type(within(dialog).getByLabelText('Admin username'), 'admin');
    await user.type(within(dialog).getByLabelText('Admin password'), 'scope-token');
    await user.click(within(dialog).getByRole('button', { name: 'Review backup changes' }));
    const firstConfirm = await within(dialog).findByRole('button', { name: 'Restore backup and restart' });
    expect(within(dialog).getByText('"en"')).toBeInTheDocument();
    expect(within(dialog).getByText('"auto"')).toBeInTheDocument();
    expect(restores).toEqual([{ confirm: false, revision: 'revision-first', restore_backup: targetBackup }]);
    await user.click(firstConfirm);
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The configuration changed elsewhere');
    expect(screen.getByTestId('working-draft')).toHaveTextContent('"log_level":"debug"');
    expect(request.mock.calls.some(([path]) => path.endsWith('/restart'))).toBe(false);

    await user.click(within(dialog).getByRole('button', { name: 'Review backup changes' }));
    const secondConfirm = await within(dialog).findByRole('button', { name: 'Restore backup and restart' });
    expect(within(dialog).getByText('"zh"')).toBeInTheDocument();
    await user.click(secondConfirm);
    expect(restores.map(({ revision, confirm }) => [revision, confirm])).toEqual([
      ['revision-first', false], ['revision-first', true], ['revision-latest', false], ['revision-latest', true],
    ]);
    expect(await screen.findByText('Effective config confirmed', {}, { timeout: 4500 })).toBeInTheDocument();
    expect(runtimeReads).toBeGreaterThanOrEqual(2);
    expect(screen.getByTestId('working-draft')).toHaveTextContent('{}');
    expect(screen.getByTestId('runtime-settings')).toHaveTextContent('"language":"auto"');
    expect(screen.getByRole('combobox', { name: 'Choose a backup' })).toHaveValue('rollback-before-restore.bak');
  });

  it('requires explicit restart confirmation and leaves a backup recovery link after rejection', async () => {
    request.mockImplementation(async (url, init) => {
      if (url.endsWith('/overview')) return response({ runtime: { started_at: 'same-process' } });
      if (url.endsWith('/restart')) {
        expect(JSON.parse(String(init?.body))).toEqual({ confirm: true });
        return response({ error: 'Restart rejected' }, 403);
      }
      return response(settings);
    });
    const user = userEvent.setup();
    mount(<SettingsService refresh={0} />, true);
    const restart = screen.getByRole('button', { name: 'Restart WebSearch' });
    await waitFor(() => expect(restart).toBeEnabled());
    await user.click(restart);
    const modal = screen.getByRole('dialog', { name: 'Restart WebSearch' });
    expect(request.mock.calls.some(([url]) => url.endsWith('/restart'))).toBe(false);
    await user.type(within(modal).getByLabelText('Admin username'), 'admin');
    await user.type(within(modal).getByLabelText('Admin password'), 'scope-token');
    await user.click(within(modal).getByRole('button', { name: 'Confirm and restart the service' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Restart rejected');
    expect(screen.getByRole('link', { name: 'Config recovery' })).toHaveAttribute('href', '#settings/service%3Arecovery');
  });
});
