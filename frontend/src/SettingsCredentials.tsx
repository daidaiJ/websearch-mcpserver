import * as Dialog from '@radix-ui/react-dialog';
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { api, ApiError } from './api';
import { useDraft, type DraftCredentials } from './draft';
import { getLocale, t, useLocale } from './i18n';
import { Icon, useNavigate, useRegisterCommands, type Command } from './navigation';
import './settings-credentials.css';

const knownCredentials: Record<string, { name?: string; title?: string; url?: string; link?: string }> = {
  BAIDU_SK: { title: 'web.name.baidu_qianfan', url: 'https://console.bce.baidu.com/iam/#/iam/apikey/list' },
  TAVILY_SK: { name: 'Tavily', url: 'https://app.tavily.com/home' },
  EXA_API_KEY: { name: 'Exa', url: 'https://dashboard.exa.ai/api-keys' },
  ANYSEARCH_API_KEY: { name: 'AnySearch', url: 'https://www.anysearch.com/console/api-keys' },
  DOUBAO_SEARCH_API_KEY: { title: 'web.name.doubao_search', url: 'https://console.volcengine.com/search-infinity/api-key' },
  TENCENTCLOUD_WSA_APIKEY: { title: 'web.name.yuanbao', url: 'https://console.cloud.tencent.com/wsapi/index' },
  TINYFISH_API_KEY: { name: 'TinyFish', url: 'https://agent.tinyfish.ai/sign-up' },
  KEENABLE_API_KEY: { name: 'Keenable', url: 'https://app.keenable.ai/console' },
  GITHUB_TOKEN: { name: 'GitHub', url: 'https://github.com/settings/tokens' },
  JINA_API_KEY: { name: 'Jina Reader', url: 'https://jina.ai/api-keys' },
  MINERU_TOKEN: { name: 'MinerU', url: 'https://mineru.net/api/manage/token' },
  LLM_API_KEY: { name: 'LLM', url: 'https://platform.openai.com/api-keys', link: 'web.credential.openai_example' },
  SEMANTIC_SCHOLAR_API_KEY: { name: 'Semantic Scholar', url: 'https://www.semanticscholar.org/product/api' },
  OPENALEX_API_KEY: { name: 'OpenAlex', url: 'https://openalex.org/settings/api' },
  UNPAYWALL_EMAIL: { name: 'Unpaywall', url: 'https://unpaywall.org/products/api', link: 'web.credential.email_documentation' },
  WEBSEARCH_TOKEN: { title: 'web.credential.business_auth' },
};
const credentialOrder = new Map(Object.keys(knownCredentials).map((name, index) => [name, index]));

const actions = {
  append: 'web.credential.append', set: 'web.credential.set', replace: 'web.credential.replace_slot',
  label: 'web.credential.edit_label', delete: 'web.credential.delete', clear: 'web.credential.clear', reset: 'web.credential.reset',
} as const;
const originLabels: Record<string, string> = { env: 'web.credential.source_env', config: 'web.credential.source_config', overlay: 'web.credential.source_override' };

type Action = keyof typeof actions;
type Slot = { id: string; label: string; source: string };
type CredentialRow = { count: number; configured: boolean; multi: boolean; read_only: boolean; sources: string[]; slots: Slot[] };
type CredentialView = { credentials: Record<string, CredentialRow>; pending?: Record<string, CredentialRow>; pending_restart: boolean; revision: string };
type EditRequest = { name: string; action: Action; revision: string; value?: string; label?: string; slot_id?: string };
type EditResult = { saved: boolean; applied: boolean; restart_required: boolean; backup: boolean; before_count: number; after_count: number; revision: string };
type Preview = { name: string; operation: Action; slotNumber?: number; before: number; after: number; restart: boolean; revision: string };

const credentialName = (key: string) => {
  const known = knownCredentials[key];
  return known?.title ? t(known.title) : known?.name ?? key;
};
const origin = (kind: string) => originLabels[kind] ? t(originLabels[kind]) : t('web.status.unknown');
const format = (value: number) => value.toLocaleString(getLocale());
const isReadOnly = (name: string, row: CredentialRow) => name === 'WEBSEARCH_TOKEN' || row.read_only;

function pendingRecord(active: CredentialRow, planned?: CredentialRow) {
  return planned && (active.count !== planned.count || active.slots.length !== planned.slots.length || active.slots.some((slot, index) => {
    const next = planned.slots[index];
    return slot.id !== next.id || slot.label !== next.label || slot.source !== next.source;
  })) ? planned : undefined;
}

function availableOperations(row: CredentialRow): Action[] {
  const actions: Action[] = [row.multi ? 'append' : 'set'];
  if (row.slots.length) {
    if (row.multi) actions.push('replace');
    actions.push('label', 'delete', 'clear');
  }
  actions.push('reset');
  return actions;
}

function postCredential(request: EditRequest, confirm: boolean) {
  return { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...request, confirm }) };
}

function CredentialLink({ name }: { name: string }) {
  const metadata = knownCredentials[name];
  if (!metadata?.url) return null;
  return <a href={metadata.url} target="_blank" rel="noopener noreferrer">{t(metadata.link ?? 'web.credential.get_key')}</a>;
}

function CredentialForm({ name, row, revision, busy, onPreview }: { name: string; row: CredentialRow; revision: string; busy: boolean; onPreview: (payload: EditRequest, slotNumber?: number, auth?: DraftCredentials) => void }) {
  const draft = useDraft();
  const operations = availableOperations(row);
  const [selection, setSelection] = useState<Action>(operations[0]);
  const [chosen, setChosen] = useState(row.slots[0]?.id ?? '');
  const [validation, setValidation] = useState('');
  const action = operations.includes(selection) ? selection : operations[0];
  const sourceIndex = row.slots.findIndex(slot => slot.id === chosen);
  const slotIndex = sourceIndex >= 0 ? sourceIndex : row.slots.length ? 0 : undefined;
  const slot = slotIndex === undefined ? undefined : row.slots[slotIndex];
  const usesValue = action === 'append' || action === 'set' || action === 'replace';
  const usesSlot = action === 'replace' || action === 'label' || action === 'delete';
  const usesLabel = action === 'append' || action === 'set' || action === 'label';

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !draft.canWrite || draft.phase !== 'idle') return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const payload: EditRequest = { name, action, revision };
    if (usesValue) payload.value = String(data.get('newCredential') ?? '').trim();
    if (usesSlot && slot) payload.slot_id = slot.id;
    if (usesLabel) payload.label = String(data.get('slotLabel') ?? '').trim();
    const auth = draft.hasCredentials ? undefined : { password: String(data.get('administratorPassword') ?? ''), username: String(data.get('administratorUsername') ?? '').trim() };
    form.querySelectorAll<HTMLInputElement>('input[type="password"]').forEach(input => { input.value = ''; });
    if ((usesValue && !payload.value) || (usesSlot && !payload.slot_id)) {
      setValidation(t('web.credential.input_required'));
      return;
    }
    if (auth && (!auth.password || draft.settings?.admin_username_configured && !auth.username)) {
      setValidation(t(auth.password ? 'web.msg.admin_username_required' : 'web.msg.admin_password_required'));
      return;
    }
    setValidation('');
    onPreview(payload, usesSlot && slotIndex !== undefined ? slotIndex + 1 : undefined, auth);
  }

  return <form className="next-credential-form" onSubmit={submit} autoComplete="off">
    <fieldset disabled={busy || !draft.canWrite || draft.phase !== 'idle'}>
      <label>{t('web.credential.operation')}<select value={action} onChange={event => { setSelection(event.currentTarget.value as Action); setValidation(''); }}>
        {operations.map(operation => <option key={operation} value={operation}>{t(actions[operation])}</option>)}
      </select></label>
      {usesSlot && <label>{t('web.credential.slot')}<select value={slot?.id ?? ''} onChange={event => { setChosen(event.currentTarget.value); setValidation(''); }}>
        {row.slots.map((slot, index) => <option key={slot.id} value={slot.id}>{[t('web.credential.slot_number', { number: index + 1 }), origin(slot.source), slot.label].filter(Boolean).join(' · ')}</option>)}
      </select></label>}
      {usesValue && <><label>{t('web.label.new_value')}<input key={action} name="newCredential" type="password" autoComplete="off" spellCheck={false} required aria-describedby="next-credential-value-help" /></label>
        <small id="next-credential-value-help">{t(name === 'UNPAYWALL_EMAIL' ? 'web.credential.email_help' : name === 'LLM_API_KEY' ? 'web.credential.llm_help' : 'web.credential.no_value_echo')}</small>
      </>}
      {usesLabel && <><label>{t('web.credential.label')}<input key={`${action}/${slot?.id ?? ''}`} name="slotLabel" defaultValue={action === 'label' ? slot?.label : ''}
        spellCheck={false} pattern="[\p{L}\p{N} _.\-\(\)（）]*" aria-describedby="next-credential-label-help" /></label>
        <small id="next-credential-label-help">{t('web.credential.label_help')}</small>
      </>}
      {!draft.hasCredentials && <div className="next-credential-auth">
        <h4>{t('web.draft.admin_access')}</h4>
        {draft.settings?.admin_username_configured && <label>{t('web.label.username')}<input name="administratorUsername" autoComplete="off" required /></label>}
        <label>{t('web.label.password')}<input name="administratorPassword" type="password" autoComplete="off" required /></label>
      </div>}
      {validation && <p className="next-credential-error" role="alert">{validation}</p>}
      <div className="next-credential-actions"><button type="submit" className="next-credential-button">{t('web.credential.preview')}</button></div>
    </fieldset>
  </form>;
}

function CredentialSlots({ row }: { row: CredentialRow }) {
  if (!row.slots.length) return <p className="next-credential-muted">{t('web.credential.no_slots')}</p>;
  return <ol className="next-credential-slots">{row.slots.map((slot, index) => <li key={slot.id}>
    <span>{t('web.credential.slot_number', { number: index + 1 })}</span><strong>{slot.label || origin(slot.source)}</strong>{slot.label && <small>{origin(slot.source)}</small>}
  </li>)}</ol>;
}

export function CredentialCommands() {
  const navigate = useNavigate();
  const locale = useLocale();
  const commands = useMemo<Command[]>(() => Object.keys(knownCredentials).map(key => ({
    id: `credential-${key}`, label: t('web.credential.manage', { name: credentialName(key) }), group: t('web.page.settings'),
    icon: 'settings', keywords: `${key} ${credentialName(key)}`, run: () => navigate({ page: 'settings', item: `credentials:${key}` }),
  })), [locale, navigate]);
  useRegisterCommands(commands);
  return null;
}

export default function SettingsCredentials({ item, refresh }: { item?: string; refresh: number }) {
  const locale = useLocale();
  const draft = useDraft();
  const [view, setView] = useState<CredentialView>();
  const [loadError, setLoadError] = useState(false);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(item);
  const [notice, setNotice] = useState<{ label: string; error: boolean }>();
  const [preview, setPreview] = useState<Preview>();
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  const active = useRef(false);
  const editSession = useRef(0);
  // The server requires the same request for preview and save. Retain it only until the preview closes.
  const pending = useRef<EditRequest | null>(null);
  const submitting = useRef(false);
  const opener = useRef<HTMLElement | null>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);

  const reload = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    try {
      const status = await api<CredentialView>('secrets', { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]) });
      if (!status.credentials || typeof status.revision !== 'string' || typeof status.pending_restart !== 'boolean') throw new Error();
      if (controller.signal.aborted) return;
      setView(status);
      setLoadError(false);
      return status;
    } catch {
      if (!controller.signal.aborted) setLoadError(true);
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [locale]);

  useEffect(() => {
    void reload();
    return () => { request.current?.abort(); };
  }, [reload, refresh]);
  useEffect(() => {
    editSession.current++;
    pending.current = null;
    setPreview(undefined);
    setSelected(item);
  }, [item]);
  useEffect(() => {
    active.current = true;
    return () => { active.current = false; pending.current = null; };
  }, []);

  const settingsView = draft.settings?.credential_status as CredentialView | undefined;
  const displayed = view ?? settingsView;
  const ordered = Object.keys(displayed?.credentials ?? {}).sort((a, b) => (credentialOrder.get(a) ?? Infinity) - (credentialOrder.get(b) ?? Infinity) || a.localeCompare(b, locale));
  const current = selected ? displayed?.credentials?.[selected] : undefined;
  const planned = selected ? displayed?.pending?.[selected] : undefined;
  const changes = current ? pendingRecord(current, planned) : undefined;
  const editable = Boolean(selected && current && !isReadOnly(selected, current) && draft.canWrite && !loadError && view?.revision);

  function dismissPreview() {
    if (submitting.current) return;
    pending.current = null;
    setPreview(undefined);
  }

  function closeDrawer() {
    if (submitting.current) return;
    dismissPreview();
    setSelected(undefined);
    if (item && /^#settings\/credentials(?::|%3a)/i.test(location.hash)) {
      history.replaceState(null, '', '#settings/credentials');
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    }
  }

  function start() {
    if (submitting.current || !editable || draft.phase !== 'idle') return false;
    submitting.current = true;
    setBusy(true);
    setNotice(undefined);
    return true;
  }

  function finish() {
    submitting.current = false;
    setBusy(false);
  }

  async function writeFailure(error: unknown) {
    pending.current = null;
    setPreview(undefined);
    if (error instanceof ApiError && error.status === 409) {
      const status = await reload();
      setNotice({ label: status ? 'web.credential.conflict_retry' : 'web.credential.write_failed', error: true });
      return;
    }
    const denied = error instanceof ApiError && [401, 403].includes(error.status);
    const invalid = error instanceof ApiError && error.status === 400;
    setNotice({ label: denied ? 'web.credential.auth_rejected' : invalid ? 'web.credential.write_invalid' : 'web.credential.write_failed', error: true });
  }

  async function prepare(payload: EditRequest, slotNumber?: number, auth?: DraftCredentials) {
    if (!start()) return;
    const session = editSession.current;
    try {
      const result = await draft.adminRequest<EditResult>('secrets', postCredential(payload, false), auth);
      if (!active.current || session !== editSession.current) return;
      if (result.saved !== false || result.applied !== false || result.revision !== payload.revision || typeof result.restart_required !== 'boolean' ||
        !Number.isSafeInteger(result.before_count) || !Number.isSafeInteger(result.after_count) || result.before_count < 0 || result.after_count < 0) throw new Error();
      pending.current = payload;
      setPreview({ name: payload.name, operation: payload.action, slotNumber, before: result.before_count, after: result.after_count, restart: result.restart_required, revision: result.revision });
    } catch (error) {
      await writeFailure(error);
    } finally {
      finish();
    }
  }

  async function save() {
    const payload = pending.current;
    if (!payload || !preview || !start()) return;
    pending.current = null;
    setPreview(undefined);
    try {
      const result = await draft.adminRequest<EditResult>('secrets', postCredential({ ...payload, revision: preview.revision }, true));
      if (result.saved !== true || typeof result.applied !== 'boolean' || typeof result.restart_required !== 'boolean') throw new Error();
      await reload();
      if (result.restart_required) {
        const latest = await draft.refresh();
        await draft.restartAndVerify(latest?.pending_changes ?? draft.settings?.pending_changes ?? {}, undefined, { verifyCredentials: true });
        await reload();
      } else setNotice({ label: result.backup ? 'web.credential.backup_created' : result.applied ? 'web.credential.saved' : 'web.credential.unchanged', error: false });
    } catch (error) {
      await writeFailure(error);
    } finally {
      finish();
    }
  }

  const pageStatus = notice && <p role={notice.error ? 'alert' : 'status'} className={notice.error ? 'next-credential-error' : 'next-credential-success'}>{t(notice.label)}</p>;

  return <section className="next-credentials" aria-labelledby="next-credentials-title">
    <header><h2 ref={heading} tabIndex={-1} id="next-credentials-title">{t('web.credential.section')}</h2>{displayed?.pending_restart && <p className="next-credential-pending" role="status">{t('web.credential.pending_restart')}</p>}</header>
    {loadError && <p className="next-credential-error" role="alert">{t('web.credential.load_failed')} <button type="button" onClick={() => void reload()}>{t('web.action.refresh_all_data_now')}</button></p>}
    {loading && !displayed?.credentials && <p role="status">{t('web.note.loading_2')}</p>}
    {!current && pageStatus}
    {ordered.length > 0 && <ul className="next-credentials-list">{ordered.map(name => {
      const row = displayed!.credentials[name];
      const pending = pendingRecord(row, displayed!.pending?.[name]);
      return <li key={name}>
        <button type="button" aria-haspopup="dialog" aria-expanded={selected === name} aria-label={t('web.credential.manage', { name: credentialName(name) })}
          onClick={event => { if (!submitting.current) { opener.current = event.currentTarget; setSelected(name); setNotice(undefined); } }}>
          <span className="next-credential-name"><strong>{credentialName(name)}</strong><code>{name}</code></span>
          <span className="next-credential-state"><span data-configured={row.configured}>{t(row.configured ? 'web.status.configured' : 'web.status.not_configured')}</span>
            <small>{t('web.credential.count', { count: format(row.count) })}{row.sources.length > 0 && ` · ${row.sources.map(origin).join(' / ')}`}</small>
          </span>
          <span className="next-credential-badges">{isReadOnly(name, row) ? t('web.credential.read_only') : pending ? t('web.credential.saved_count', { Count: format(pending.count) }) : <Icon name="arrow" size={15} />}</span>
        </button>
        <CredentialLink name={name} />
      </li>;
    })}</ul>}

    <Dialog.Root modal={false} open={Boolean(selected && current)} onOpenChange={open => { if (!open) closeDrawer(); }}>
      {selected && current && <Dialog.Portal><div className="next-credential-scrim" aria-hidden="true" />
        <Dialog.Content className="next-credential-drawer" onInteractOutside={event => event.preventDefault()}
          onCloseAutoFocus={event => { event.preventDefault(); (opener.current?.isConnected ? opener.current : heading.current)?.focus({ preventScroll: true }); }}>
          <header><div><Dialog.Title>{credentialName(selected)}</Dialog.Title><Dialog.Description><code>{selected}</code></Dialog.Description></div>
            <Dialog.Close ref={closeButton} aria-label={t('web.next.close')} disabled={busy}><Icon name="close" /></Dialog.Close>
          </header>
          <div className="next-credential-drawer-content">
            <CredentialLink name={selected} />
            {pageStatus}
            <section><h3>{t('web.credential.active_slots')}</h3><p>{t('web.credential.count', { count: format(current.count) })}{current.sources.length > 0 && ` · ${current.sources.map(origin).join(' / ')}`}</p>
              {isReadOnly(selected, current) ? <p className="next-credential-muted">{t('web.credential.service_read_only')}</p> : <CredentialSlots row={current} />}
            </section>
            {displayed?.pending_restart && changes && !isReadOnly(selected, current) && <section><h3>{t('web.credential.saved_slots')}</h3><CredentialSlots row={changes} /><p className="next-credential-pending">{t('web.credential.pending_restart')}</p></section>}
            {!isReadOnly(selected, current) && <section><h3>{t('web.credential.manage', { name: credentialName(selected) })}</h3>{editable && view ? <CredentialForm key={`${selected}/${view.revision}`} name={selected} row={planned ?? current} revision={view.revision} busy={busy}
              onPreview={(payload, slotNumber, auth) => { void prepare(payload, slotNumber, auth); }} /> : <p className="next-credential-muted">{loadError ? t('web.credential.load_failed') : t('web.credential.read_only')}</p>}</section>}
          </div>
        </Dialog.Content>
      </Dialog.Portal>}
    </Dialog.Root>

    <Dialog.Root open={Boolean(preview)} onOpenChange={open => { if (!open) dismissPreview(); }}>
      <Dialog.Portal><Dialog.Overlay className="next-credential-dialog-overlay" />
        <Dialog.Content className="next-credential-dialog" onCloseAutoFocus={event => { event.preventDefault(); closeButton.current?.focus({ preventScroll: true }); }}>
          <Dialog.Title>{t('web.credential.preview')}</Dialog.Title><Dialog.Description>{t('web.credential.backup_help')}</Dialog.Description>
          {preview && <dl>
            <div><dt>{credentialName(preview.name)}</dt><dd>{t(actions[preview.operation])}</dd></div>
            {preview.slotNumber !== undefined && <div><dt>{t('web.credential.slot')}</dt><dd>{t('web.credential.slot_number', { number: preview.slotNumber })}</dd></div>}
            <div><dt>{t('web.credential.before_after')}</dt><dd>{t('web.credential.count_diff', { before: format(preview.before), after: format(preview.after) })}</dd></div>
          </dl>}
          <div className="next-credential-actions"><Dialog.Close>{t('web.action.cancel')}</Dialog.Close>
            <button type="button" className="next-credential-button" data-danger={Boolean(preview?.restart || preview?.operation === 'clear' || preview?.operation === 'delete') || undefined} disabled={!editable || busy || draft.phase !== 'idle'} onClick={() => void save()}>{t(preview?.restart ? 'web.action.save_and_restart' : 'web.action.save')}</button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </section>;
}
