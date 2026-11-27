import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DraftProvider } from './draft';
import { DraftBar } from './DraftBar';
import { setLanguage, t } from './i18n';
import { CommandRegistry, NavigationContext, type Command } from './navigation';
import SettingsCredentials, { CredentialCommands } from './SettingsCredentials';

// All values are isolated wire fixtures. No operational credentials or service are used.
const sampleKey = 'fixture-not-a-real-provider-key';
const adminPassword = 'fixture-not-an-admin-password';

const fromEnv = { id: 'opaque-slot-a', label: 'Primary', source: 'env' };
const fromOverlay = { id: 'opaque-slot-b', label: 'Backup', source: 'overlay' };
const tavily = { count: 2, configured: true, multi: true, read_only: false, sources: ['env', 'overlay'], slots: [fromEnv, fromOverlay] };
const auth = { count: 1, configured: true, multi: false, read_only: true, sources: ['config'], slots: [] };

type WireOptions = { conflict?: boolean; requiresRestart?: boolean; canWrite?: boolean; item?: string };

function setup(options: WireOptions = {}) {
  let revision = 'cred-r1';
  let pending = false;
  let reboot = false;
  let settingsPolls = 0;
  let active = { TAVILY_SK: tavily, WEBSEARCH_TOKEN: auth };
  let saved = active;
  const responder = vi.fn(async (path: string, request?: RequestInit) => {
    if (path.endsWith('/settings')) {
      if (reboot && ++settingsPolls >= 2) {
        pending = false;
        active = saved;
      }
      return Response.json({ revision: 'config-r1', values: {}, saved_values: {}, write_access: { allowed: options.canWrite ?? true },
        admin_password_configured: true, admin_username_configured: true, credential_status: { pending_restart: pending } });
    }
    if (path.endsWith('/overview')) return Response.json({ runtime: { started_at: reboot ? 'boot-2' : 'boot-1' } });
    if (path.endsWith('/restart')) { reboot = true; return Response.json({ restarting: true }, { status: 202 }); }
    if (path.endsWith('/secrets') && request?.method === 'POST') {
      const payload = JSON.parse(String(request.body));
      if (options.conflict && payload.confirm) {
        revision = 'cred-newer';
        active = saved = { ...active, TAVILY_SK: { ...tavily, slots: [{ ...fromEnv, label: 'Updated elsewhere' }, fromOverlay] } };
        // A hostile or accidental server error must never be rendered as credential UI copy.
        return Response.json({ error: sampleKey }, { status: 409 });
      }
      if (payload.confirm) {
        revision = 'cred-r2';
        pending = Boolean(options.requiresRestart);
        if (payload.action === 'append') saved = { ...saved, TAVILY_SK: { ...tavily, count: 3, slots: [...tavily.slots, { id: 'opaque-slot-c', label: 'Test', source: 'overlay' }] } };
        if (!pending) active = saved;
      }
      return Response.json({ saved: payload.confirm, applied: payload.confirm, restart_required: Boolean(options.requiresRestart), backup: payload.confirm,
        before_count: 2, after_count: payload.action === 'append' ? 3 : 2, revision });
    }
    if (path.endsWith('/secrets')) return Response.json({ credentials: active, pending: saved, pending_restart: pending, revision });
    throw new Error(path);
  });
  vi.stubGlobal('fetch', responder);
  let indexed: readonly Command[] = [];
  const navigate = vi.fn();
  const unsubscribe = () => { indexed = []; };
  const register = (commands: readonly Command[]) => { indexed = commands; return unsubscribe; };
  render(<DraftProvider><NavigationContext.Provider value={navigate}><CommandRegistry.Provider value={register}>
    <CredentialCommands /><SettingsCredentials item={options.item} refresh={0} /><DraftBar />
  </CommandRegistry.Provider></NavigationContext.Provider></DraftProvider>);
  const writes = () => responder.mock.calls.filter(([path, req]) => path.endsWith('/secrets') && req?.method === 'POST')
    .map(([, req]) => ({ body: JSON.parse(String(req?.body)), headers: new Headers(req?.headers) }));
  return { responder, navigate, indexed: () => indexed, writes, settingsPolls: () => settingsPolls };
}

async function drawer(name = 'Tavily') {
  const button = await screen.findByRole('button', { name: t('web.credential.manage', { name }) });
  fireEvent.click(button);
  const element = await screen.findByRole('dialog', { name });
  if (name === 'Tavily') await within(element).findByRole('button', { name: t('web.credential.preview') });
  return { element, button };
}

function fillAuth(element: HTMLElement) {
  if (within(element).queryByLabelText(t('web.label.username'))) fireEvent.change(within(element).getByLabelText(t('web.label.username')), { target: { value: 'fixture-operator' } });
  if (within(element).queryByLabelText(t('web.label.password'))) fireEvent.change(within(element).getByLabelText(t('web.label.password')), { target: { value: adminPassword } });
}

async function preview(element: HTMLElement) {
  fillAuth(element);
  fireEvent.click(within(element).getByRole('button', { name: t('web.credential.preview') }));
  return screen.findByRole('dialog', { name: t('web.credential.preview') });
}

beforeEach(() => setLanguage('en'));
afterEach(() => vi.unstubAllGlobals());

describe('credential settings', () => {
  it('shows authentic metadata and a read-only service credential with keyboard focus return', async () => {
    const { navigate, indexed } = setup();
    const name = t('web.credential.business_auth');
    const { element, button } = await drawer(name);
    expect(within(element).getByText(t('web.credential.service_read_only'))).toBeVisible();
    expect(within(element).queryByLabelText(t('web.credential.operation'))).not.toBeInTheDocument();
    expect(within(element).getByText(/1 active · Config file/)).toBeInTheDocument();
    fireEvent.keyDown(within(element).getByRole('button', { name: t('web.next.close') }), { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(button).toHaveFocus());
    expect(screen.getByRole('link', { name: t('web.credential.get_key') })).toHaveAttribute('href', 'https://app.tavily.com/home');
    indexed().find(command => command.id === 'credential-TAVILY_SK')!.run();
    expect(navigate).toHaveBeenCalledWith({ page: 'settings', item: 'credentials:TAVILY_SK' });
  });

  it('uses scoped opaque slots and a server diff without keeping or displaying credential values', async () => {
    const { writes } = setup();
    const { element } = await drawer();
    fireEvent.change(within(element).getByRole('combobox', { name: t('web.credential.operation') }), { target: { value: 'replace' } });
    fireEvent.change(within(element).getByRole('combobox', { name: t('web.credential.slot') }), { target: { value: fromOverlay.id } });
    const value = within(element).getByLabelText(t('web.label.new_value'));
    expect(value).toHaveAttribute('type', 'password');
    fireEvent.change(value, { target: { value: sampleKey } });
    const diff = await preview(element);
    expect(value).toHaveValue('');
    expect(within(diff).getByText(t('web.credential.replace_slot'))).toBeInTheDocument();
    expect(within(diff).getByText(t('web.credential.slot_number', { number: 2 }))).toBeInTheDocument();
    expect(within(diff).getByText(t('web.credential.count_diff', { before: 2, after: 2 }))).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(sampleKey);
    expect(document.body.textContent).not.toContain(adminPassword);
    expect(JSON.stringify(localStorage)).not.toContain(sampleKey);
    fireEvent.click(within(diff).getByRole('button', { name: t('web.action.save') }));
    expect(await screen.findByText(t('web.credential.backup_created'))).toBeInTheDocument();
    const expected = { name: 'TAVILY_SK', action: 'replace', slot_id: fromOverlay.id, revision: 'cred-r1', value: sampleKey };
    expect(writes().map(entry => entry.body)).toEqual([{ ...expected, confirm: false }, { ...expected, confirm: true }]);
    for (const request of writes()) {
      expect(request.headers.get('X-Admin-Password')).toBe(adminPassword);
      expect(request.headers.get('X-Admin-User')).toBe('fixture-operator');
    }
    expect(JSON.stringify(sessionStorage)).not.toContain(sampleKey);
    expect(within(await screen.findByRole('dialog', { name: 'Tavily' })).queryByLabelText(t('web.label.password'))).not.toBeInTheDocument();
  });

  it.each([
    ['delete', { slot_id: fromEnv.id }], ['label', { slot_id: fromEnv.id, label: 'Spare' }], ['clear', {}], ['reset', {}],
  ])('previews %s with only the required metadata', async (action, expected) => {
    const { writes } = setup();
    const { element } = await drawer();
    fireEvent.change(within(element).getByRole('combobox', { name: t('web.credential.operation') }), { target: { value: action } });
    if (action === 'label') fireEvent.change(within(element).getByLabelText(t('web.credential.label')), { target: { value: 'Spare' } });
    const diff = await preview(element);
    expect(writes()[0].body).toEqual({ name: 'TAVILY_SK', action, revision: 'cred-r1', confirm: false, ...expected });
    fireEvent.click(within(diff).getByRole('button', { name: t('web.action.cancel') }));
    expect(writes()).toHaveLength(1);
  });

  it('clears stale input and refreshes metadata after a commit conflict instead of resubmitting', async () => {
    const { writes } = setup({ conflict: true });
    const { element } = await drawer();
    fireEvent.change(within(element).getByLabelText(t('web.label.new_value')), { target: { value: sampleKey } });
    const diff = await preview(element);
    fireEvent.click(within(diff).getByRole('button', { name: t('web.action.save') }));
    await screen.findByText(t('web.credential.conflict_retry'));
    expect(await screen.findByText('Updated elsewhere')).toBeInTheDocument();
    expect(within(element).getByLabelText(t('web.label.new_value'))).toHaveValue('');
    expect(document.body.textContent).not.toContain(sampleKey);
    expect(writes()).toHaveLength(2);
  });

  it('waits for a changed boot marker and active credentials after saving', async () => {
    const { responder, settingsPolls } = setup({ requiresRestart: true });
    const { element } = await drawer();
    fireEvent.change(within(element).getByLabelText(t('web.label.new_value')), { target: { value: sampleKey } });
    const diff = await preview(element);
    fireEvent.click(within(diff).getByRole('button', { name: t('web.action.save_and_restart') }));
    expect(await screen.findByText(t('web.msg.effective_config_confirmed'), {}, { timeout: 4200 })).toBeInTheDocument();
    const restart = responder.mock.calls.find(([path]) => path.endsWith('/restart'))?.[1];
    expect(JSON.parse(String(restart?.body))).toEqual({ confirm: true });
    expect(new Headers(restart?.headers).get('X-Admin-User')).toBe('fixture-operator');
    expect(settingsPolls()).toBeGreaterThanOrEqual(2);
    expect(await within(element).findByText(/3 active · Environment/)).toBeInTheDocument();
  });

  it('allows inspection but hides edit entry points when the write policy denies the connection', async () => {
    setup({ canWrite: false, item: 'TAVILY_SK' });
    const element = await screen.findByRole('dialog', { name: 'Tavily' });
    expect(within(element).queryByLabelText(t('web.credential.operation'))).not.toBeInTheDocument();
  });
});
