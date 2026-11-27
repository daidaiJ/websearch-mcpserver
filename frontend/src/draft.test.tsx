import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import english from '../../pkg/i18n/locales/web.en.json';
import { DraftBar } from './DraftBar';
import { DraftProvider, useDraft, type SettingsView } from './draft';
import { setLanguage, t } from './i18n';

const path = 'bing.enabled';
const timeout = 'bing.timeout';
const locked = 'api.enabled';

function configure(revision = 'r1', enabled = true, saved = enabled, timeoutMs = 5000): SettingsView {
  return {
    revision, values: { [path]: enabled, [timeout]: timeoutMs, [locked]: true },
    saved_values: { [path]: saved, [timeout]: timeoutMs, [locked]: true },
    pending_changes: saved !== enabled ? { [path]: saved } : {},
    field_files: { [path]: 'config.yaml', [timeout]: 'config.yaml', [locked]: 'config.yaml' },
    locked_fields: { [locked]: 'Environment owned' },
    write_access: { allowed: true }, admin_password_configured: true, admin_username_configured: true,
  };
}

function Fixture() {
  const draft = useDraft();
  const [page, setPage] = useState('one');
  return <><button aria-current="page" onClick={() => setPage(page === 'one' ? 'two' : 'one')}>Switch page</button>
    <output aria-label="revision">{draft.revision}</output>
    <output aria-label="value">{String(draft.value(path))}</output>
    <output aria-label="timeout">{String(draft.value(timeout))}</output>
    {page === 'one' && <><button disabled={Boolean(draft.fieldReason(path))} onClick={() => draft.stage(path, false)}>Edit source</button>
      <button disabled={Boolean(draft.fieldReason(locked))} onClick={() => draft.stage(locked, false)}>Edit locked field</button>
      <button onClick={() => draft.stage(timeout, 8000)}>Edit timeout</button>
      <button onClick={() => draft.setFieldError(path, 'Enter a valid value', '')}>Invalidate source</button>
      <button onClick={() => draft.setFieldError(path, '')}>Clear field error</button></>}
    <button onClick={() => void draft.refresh()}>Refresh settings</button>
    <DraftBar />
  </>;
}

const response = (value: object, status = 200) => Response.json(value, { status });

function mount() {
  return render(<DraftProvider><Fixture /></DraftProvider>);
}

const openAuth = async () => {
  fireEvent.click(screen.getByRole('button', { name: t('web.draft.review') }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText(t('web.label.username')), { target: { value: 'operator' } });
  fireEvent.change(within(dialog).getByLabelText(t('web.label.password')), { target: { value: 'example-password' } });
  fireEvent.click(within(dialog).getByRole('button', { name: t('web.draft.review') }));
};

beforeEach(() => {
  window.__I18N__ = english;
  setLanguage('en');
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('configuration draft', () => {
  it('preserves the original revision across pages and refresh while blocking locked and invalid edits', async () => {
    let version = 'r1';
    const transport = vi.fn(async () => response(configure(version)));
    vi.stubGlobal('fetch', transport);
    mount();
    const edit = screen.getByRole('button', { name: 'Edit source' });
    await waitFor(() => expect(edit).toBeEnabled());
    expect(screen.getByRole('button', { name: 'Edit locked field' })).toBeDisabled();
    fireEvent.click(edit);
    fireEvent.click(screen.getByRole('button', { name: 'Invalidate source' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a valid value');
    expect(screen.getByRole('button', { name: t('web.draft.review') })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Switch page' }));
    expect(screen.getByLabelText('value')).toHaveTextContent('false');
    version = 'r2';
    fireEvent.click(screen.getByRole('button', { name: 'Refresh settings' }));
    await waitFor(() => expect(transport).toHaveBeenCalledTimes(2));
    expect(screen.getByLabelText('revision')).toHaveTextContent('r1');
    fireEvent.click(screen.getByRole('button', { name: t('web.action.discard_all_unsaved_changes') }));
    expect(screen.getByLabelText('value')).toHaveTextContent('true');
    expect(screen.queryByRole('region', { name: t('web.draft.region') })).not.toBeInTheDocument();
  });

  it('shows normalized server diff and verifies both saved and active values after restart', async () => {
    const requests: { url: string; confirm?: boolean; revision?: string }[] = [];
    let restartAccepted = false;
    let polls = 0;
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const request = typeof init?.body === 'string' ? JSON.parse(init.body) : {};
      requests.push({ url, confirm: request.confirm, revision: request.revision });
      if (url.endsWith('/restart')) {
        restartAccepted = true;
        return response({ restarting: true }, 202);
      }
      if (url.endsWith('/overview')) return response({ system: { mode: 'hybrid' }, runtime: { started_at: requests.some(write => write.url.endsWith('/restart')) ? 'second' : 'first' } });
      if (init?.method === 'POST') {
        const headers = new Headers(init.headers);
        expect(headers.get('X-Admin-User')).toBe('operator');
        expect(headers.get('X-Admin-Password')).toBe('example-password');
        return response({ applied: request.confirm, restart_required: true, revision: request.confirm ? 'r2' : 'r1', changes: { [path]: false, [timeout]: 8500 }, field_files: { [path]: 'config.yaml', [timeout]: 'dashboard.yaml' } });
      }
      if (restartAccepted) return response(configure('r2', ++polls === 1, false, 8500));
      return response(configure());
    }));
    mount();
    const edit = screen.getByRole('button', { name: 'Edit source' });
    await waitFor(() => expect(edit).toBeEnabled());
    fireEvent.click(edit);
    fireEvent.click(screen.getByRole('button', { name: 'Edit timeout' }));
    await openAuth();
    const save = await screen.findByRole('button', { name: t('web.action.save_and_restart') });
    expect(within(screen.getByRole('dialog')).getByText('8500')).toBeInTheDocument();
    expect(within(screen.getByRole('dialog')).getByText('dashboard.yaml')).toBeInTheDocument();
    fireEvent.click(save);
    await screen.findByText(t('web.msg.effective_config_confirmed'), {}, { timeout: 4000 });
    expect(polls).toBe(2);
    expect(requests.filter(request => request.confirm !== undefined)).toEqual([
      { url: '/__admin/api/settings', confirm: false, revision: 'r1' },
      { url: '/__admin/api/settings', confirm: true, revision: 'r1' },
      { url: '/__admin/api/restart', confirm: true, revision: undefined },
    ]);
    expect(JSON.stringify(localStorage)).not.toContain('example-password');
  });

  it('preserves edits across a commit conflict and requires a fresh preview after reconciliation', async () => {
    let revision = 'r1';
    const requests: { changes: object; confirm: boolean; revision: string }[] = [];
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method !== 'POST') return response(configure(revision, true, true, revision === 'r1' ? 5000 : 6000));
      const request = JSON.parse(String(init.body));
      requests.push(request);
      if (request.confirm) { revision = 'r2'; return response({ error: 'Stale revision' }, 409); }
      return response({ applied: false, restart_required: true, revision, field_files: { [path]: 'config.yaml', [timeout]: 'config.yaml' }, changes: request.changes });
    }));
    mount();
    const edit = screen.getByRole('button', { name: 'Edit source' });
    await waitFor(() => expect(edit).toBeEnabled());
    fireEvent.click(edit);
    fireEvent.click(screen.getByRole('button', { name: 'Edit timeout' }));
    await openAuth();
    fireEvent.click(await screen.findByRole('button', { name: t('web.action.save_and_restart') }));
    await screen.findByRole('heading', { name: t('web.draft.configuration_changed') });
    expect(screen.getByLabelText('timeout')).toHaveTextContent('8000');
    expect(within(screen.getByRole('dialog')).getByText('6000')).toBeInTheDocument();
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('checkbox'));
    fireEvent.click(screen.getByRole('button', { name: t('web.draft.reconcile') }));
    expect(screen.getByLabelText('revision')).toHaveTextContent('r2');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: t('web.draft.review') })); });
    expect(requests.at(-1)).toEqual({ changes: { [path]: false, [timeout]: 8000 }, confirm: false, revision: 'r2' });
  });

  it('clears rejected credentials, keeps the draft, and asks for authorization again', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => init?.method === 'POST'
      ? response({ error: 'Credentials rejected' }, 403) : response(configure())));
    mount();
    const edit = screen.getByRole('button', { name: 'Edit source' });
    await waitFor(() => expect(edit).toBeEnabled());
    fireEvent.click(edit);
    await openAuth();
    expect(await screen.findByRole('alert')).toHaveTextContent('Credentials rejected');
    fireEvent.click(screen.getByRole('button', { name: t('web.draft.review') }));
    expect(within(await screen.findByRole('dialog')).getByLabelText(t('web.label.password'))).toHaveValue('');
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: t('web.draft.review') })).toHaveFocus();
  });
});
