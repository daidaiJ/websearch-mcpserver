import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useId, useRef, useState, type FormEvent, type MouseEvent } from 'react';
import { ApiError } from './api';
import { CapabilityFields, count, select, toggle, type Field } from './CapabilityFields';
import { useDraft, type DraftCredentials, type SettingValue } from './draft';
import { DraftDiff } from './DraftBar';
import { getLocale, t } from './i18n';
import { Icon } from './navigation';
import { MapSetting } from './SettingsRoute';
import './settings-core.css';

type Values = Record<string, SettingValue>;
type RestoreResult = { revision: string; applied: boolean; restart_required: boolean; changes: Values; field_files?: Record<string, string>; backup?: string };
type RestoreReview = { result: RestoreResult; before: Values };
type Action = 'restart' | 'restore';

const jsonPost: HeadersInit = { 'Content-Type': 'application/json' };

function validProxy(raw: string) {
  if (!raw) return true;
  if (!/^(?:https?|socks5h?):\/\/[^/]/.test(raw) || /[?#\s\\]|\p{Cc}/u.test(raw)) return false;
  try {
    const endpoint = new URL(raw);
    return Boolean(endpoint.hostname) && !endpoint.username && !endpoint.password && (!endpoint.pathname || endpoint.pathname === '/')
      && (!endpoint.port || Number(endpoint.port) > 0 && Number(endpoint.port) <= 65535);
  } catch { return false; }
}

const configText = (key: string, label: string, maxLength: number, check?: (value: string) => boolean): Field => ({
  kind: 'text', key, label, maxLength, check,
});

function ProxyPolicy() {
  const draft = useDraft();
  const id = useId();
  const value = draft.value('proxy.enabled');
  const mode = value === true ? 'enabled' : value === false ? 'disabled' : 'auto';
  const reason = draft.fieldLockReason('proxy.enabled');
  const note = draft.errors['proxy.enabled']?.message || reason;
  return <div className="cap-field"><label htmlFor={id}>{t('web.config.proxy_mode')}</label>
    <select id={id} value={mode} disabled={!draft.canWrite || Boolean(reason) || draft.phase !== 'idle'} aria-describedby={note ? `${id}-issue` : undefined}
      onChange={event => draft.stage('proxy.enabled', event.target.value === 'auto' ? null : event.target.value === 'enabled')}>
      <option value="auto">{t('web.config.proxy_auto')}</option>
      <option value="enabled">{t('web.config.proxy_explicit')}</option>
      <option value="disabled">{t('web.config.proxy_disabled')}</option>
    </select>{note && <small id={`${id}-issue`}>{note}</small>}
  </div>;
}

function backupLabel(backup: { name: string; created_at: string }) {
  const date = new Date(backup.created_at);
  return Number.isNaN(date.getTime()) ? backup.name : `${new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'medium' }).format(date)} · ${backup.name}`;
}

function validRestoreResult(result: RestoreResult, applied: boolean) {
  return result?.applied === applied && typeof result.revision === 'string' && result.revision !== ''
    && typeof result.restart_required === 'boolean' && Boolean(result.changes && typeof result.changes === 'object' && !Array.isArray(result.changes));
}

function ServiceMaintenance() {
  const draft = useDraft();
  const [selection, setSelection] = useState('');
  const [action, setAction] = useState<Action>();
  const [review, setReview] = useState<RestoreReview>();
  const [error, setError] = useState('');
  const [working, setWorking] = useState(false);
  const busy = useRef(false);
  const opener = useRef<HTMLButtonElement | null>(null);
  const backups = draft.settings?.backups ?? [];
  const backup = backups.find(item => item.name === selection);
  const ready = draft.canWrite && draft.phase === 'idle';

  function close() {
    setAction(undefined);
    setReview(undefined);
    setError('');
  }

  async function onError(caught: unknown) {
    setReview(undefined);
    if (caught instanceof ApiError && caught.status === 409) {
      await draft.refresh();
      setError(t('web.settingsCore.restore_conflict'));
    } else setError(caught instanceof Error ? caught.message : t('web.status.unknown_error'));
  }

  async function preview(credentials?: DraftCredentials) {
    const settings = draft.settings;
    if (!settings || !ready || !backup || busy.current) return;
    busy.current = true;
    setWorking(true);
    setError('');
    try {
      const { revision } = settings;
      const result = await draft.adminRequest<RestoreResult>('settings', {
        method: 'POST', headers: jsonPost, body: JSON.stringify({ restore_backup: backup.name, revision, confirm: false }),
      }, credentials);
      if (!validRestoreResult(result, false) || revision !== result.revision) throw new Error(t('web.draft.preview_incomplete'));
      setReview({ before: settings.saved_values ?? settings.values, result });
    } catch (caught) { await onError(caught); }
    finally { busy.current = false; setWorking(false); }
  }

  async function restore() {
    if (!review || !backup || !ready || busy.current) return;
    busy.current = true;
    setWorking(true);
    setError('');
    try {
      const result = await draft.adminRequest<RestoreResult>('settings', {
        method: 'POST', headers: jsonPost, body: JSON.stringify({ restore_backup: backup.name, revision: review.result.revision, confirm: true }),
      });
      if (!validRestoreResult(result, true)) throw new Error(t('web.settingsCore.restore_not_confirmed'));
      const targets = { ...draft.settings?.pending_changes, ...result.changes };
      const verifyCredentials = draft.settings?.credential_status?.pending_restart === true;
      if (result.backup) setSelection(result.backup);
      close();
      const confirmed = await draft.restartAndVerify(targets, undefined, { verifyCredentials, discardDraft: true });
      if (!confirmed) await draft.refresh();
    } catch (caught) { await onError(caught); }
    finally { busy.current = false; setWorking(false); }
  }

  function start(event: MouseEvent<HTMLButtonElement>, next: Action) {
    opener.current = event.currentTarget;
    setAction(next);
    if (next === 'restore' && draft.hasCredentials) void preview();
  }

  function authorize(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!ready || busy.current) return;
    const input = new FormData(event.currentTarget);
    const credentials = draft.hasCredentials ? undefined : {
      password: String(input.get('password') ?? ''), username: String(input.get('username') ?? ''),
    };
    event.currentTarget.reset();
    if (action === 'restore') { void preview(credentials); return; }
    const targets = draft.settings?.pending_changes ?? {};
    const verifyCredentials = draft.settings?.credential_status?.pending_restart === true;
    close();
    void draft.restartAndVerify(targets, credentials, { verifyCredentials });
  }

  const lines = Object.entries(review?.result.changes ?? {}).map(([key, after]) => ({
    key, after, before: review?.before[key], file: review?.result.field_files?.[key],
  }));

  return <>
    <section className="cap-panel" aria-labelledby="set-service-maintenance">
      <header className="cap-panel-head"><h2 id="set-service-maintenance" tabIndex={-1}>{t('web.page.advanced_actions')}</h2></header>
      <div className="set-action-line">
        <div><h3>{t('web.action.restart_service')}</h3><p className="cap-note">{t('web.note.applies_saved_settings_or_new_keys')}</p></div>
        <button type="button" className="cap-button" disabled={!ready || working} onClick={event => start(event, 'restart')}><Icon name="refresh" size={15} />{t('web.action.restart_websearch')}</button>
      </div>
      <div className="set-action-line">
        <div><h3>{t('web.page.config_recovery')}</h3><p className="cap-note">{t('web.settingsCore.recovery_help')}</p></div>
        {backups.length === 0 ? <p className="cap-note">{t('web.settingsCore.no_backups')}</p> : <div className="set-recovery-actions">
          <label className="cap-field">{t('web.action.choose_a_backup')}
            <select value={backup?.name ?? ''} onChange={event => setSelection(event.target.value)} disabled={working}>
              <option value="">{t('web.settingsCore.select_backup')}</option>
              {backups.map(item => <option key={item.name} value={item.name}>{backupLabel(item)}</option>)}
            </select>
          </label>
          <button type="button" className="cap-button" disabled={!ready || !backup || working} onClick={event => start(event, 'restore')}>{t('web.settingsCore.review_backup')}</button>
        </div>}
      </div>
    </section>

    <Dialog.Root open={Boolean(action)} onOpenChange={next => { if (!next && !working) close(); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="cap-modal-overlay" />
        <Dialog.Content className="cap-modal set-config-dialog" onCloseAutoFocus={event => {
          event.preventDefault();
          const button = opener.current;
          if (button?.isConnected && !button.disabled) button.focus();
          else document.getElementById('set-service-maintenance')?.focus();
        }}>
          <Dialog.Title>{action === 'restart' ? t('web.action.restart_websearch') : t('web.action.restore_config_backup')}</Dialog.Title>
          <Dialog.Description>{action === 'restart' ? t('web.settingsCore.restart_prompt')
            : t('web.settingsCore.restore_prompt', { Backup: backup?.name ?? '' })}</Dialog.Description>
          {error && <p className="cap-error" role="alert">{error}</p>}
          {working && <p role="status" className="cap-note">{review ? t('web.draft.saving') : t('web.draft.validating')}</p>}
          {review ? <>
            {lines.length > 0 ? <DraftDiff lines={lines} /> : <p className="cap-note">{t('web.settingsCore.no_backup_changes')}</p>}
            {draft.count > 0 && <p className="cap-note">{t('web.settingsCore.restore_discards')}</p>}
            <div className="cap-modal-actions">
              <Dialog.Close type="button" className="cap-button" disabled={working}>{t('web.action.cancel')}</Dialog.Close>
              <button type="button" className="cap-button set-danger" disabled={!ready || working} onClick={() => void restore()}>{t('web.action.restore_backup_and_restart')}</button>
            </div>
          </> : <form className="cap-auth" onSubmit={authorize}>
            {!draft.hasCredentials && <>
              {draft.settings?.admin_username_configured && <label>{t('web.label.admin_username')}<input type="text" name="username" autoComplete="username" required disabled={working} /></label>}
              <label>{t('web.label.admin_password')}<input type="password" name="password" autoComplete="current-password" required disabled={working} /></label>
            </>}
            <div className="cap-modal-actions">
              <Dialog.Close type="button" className="cap-button" disabled={working}>{t('web.action.cancel')}</Dialog.Close>
              <button type="submit" className={action === 'restart' ? 'cap-button set-danger' : 'cap-button cap-primary'} disabled={!ready || working || (action === 'restore' && !backup)}>
                {action === 'restart' ? t('web.action.confirm_and_restart_the_service') : t('web.settingsCore.review_backup')}
              </button>
            </div>
          </form>}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </>;
}

export default function SettingsService({ item, refresh: _refresh }: { item?: string; refresh: number }) {
  useEffect(() => {
    if (item !== 'recovery') return;
    const frame = requestAnimationFrame(() => {
      const heading = document.getElementById('set-service-maintenance');
      heading?.scrollIntoView({ block: 'start' });
      heading?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [item]);
  const proxyHelp = t('web.settingsCore.proxy_format');
  const durationHelp = t('web.settingsCore.duration_help');
  const duration: Record<string, string> = {
    'dashboard.suspension.ban_time_on_fail': t('web.label.first_pause'),
    'dashboard.suspension.max_ban_time_on_fail': t('web.label.pause_ceiling'),
  };
  return <div className="set-core">
    <section className="cap-panel" aria-labelledby="set-service-config">
      <header className="cap-panel-head"><h2 id="set-service-config">{t('web.page.service_operations')}</h2><p>{t('web.settingsCore.service_help')}</p></header>
      <CapabilityFields fieldHintsOnly fields={[
        select('language', t('web.config.language'), { auto: t('web.next.auto'), zh: '中文', en: 'English' }),
        select('log_level', t('web.config.log_level'), ['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic', 'disabled']),
      ]} />
      <div className="set-service-proxy">
        <ProxyPolicy />
        <CapabilityFields fieldHintsOnly fields={[
          { kind: 'text', key: 'proxy.endpoint', label: t('web.config.proxy_endpoint'), maxLength: 2048, check: validProxy, issue: proxyHelp, help: proxyHelp, sensitiveEndpoint: true },
          toggle('proxy.api_providers', t('web.config.api_proxy')),
        ]} />
      </div>
    </section>
    <section className="cap-panel" aria-labelledby="set-service-observations">
      <header className="cap-panel-head"><h2 id="set-service-observations">{t('web.settingsCore.observation')}</h2></header>
      <CapabilityFields fieldHintsOnly fields={[
        count('dashboard.retention_days', t('web.config.telemetry_retention'), 1, 36500),
        count('rate_limit.per_sec', t('web.config.per_second'), 1, 100000),
        count('rate_limit.per_min', t('web.config.per_minute'), 1, 1000000),
      ]} />
      <MapSetting path="dashboard.client_projects" label={t('web.config.client_projects')} parseEntry={(key, raw) => {
        if ([...key].length <= 128 && key === key.toLowerCase() && !/[\p{Cc}]/u.test(key) && [...raw].length <= 64 && /^[\p{L}\p{Nd} ._-]+$/u.test(raw)) return raw;
      }} />
      <details className="set-advanced">
        <summary>{t('web.label.observation_warning_pause_window')}</summary>
        <p className="cap-note">{durationHelp}</p>
        <CapabilityFields fieldHintsOnly fields={Object.entries(duration).map(([key, label]) => configText(key, label, 32))} />
      </details>
    </section>
    <section className="cap-panel" aria-labelledby="set-service-brand">
      <header className="cap-panel-head"><h2 id="set-service-brand">{t('web.settingsCore.server_appearance')}</h2><p>{t('web.settingsCore.brand_help')}</p></header>
      <CapabilityFields fieldHintsOnly fields={[
        configText('dashboard.brand.title', t('web.config.brand_title'), 256, raw => [...raw].length <= 128 && raw.trim() === raw && !/[\p{Cc}]/u.test(raw)),
        select('dashboard.brand.theme', t('web.config.brand_theme'), { green: t('web.next.pine'), blue: t('web.next.blue'), mono: t('web.next.graphite') }),
        { kind: 'text', key: 'dashboard.brand.accent', label: t('web.config.brand_accent'), maxLength: 7, check: raw => !raw || /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i.test(raw),
          help: t('web.settingsCore.accent_format'), issue: t('web.settingsCore.accent_format') },
        toggle('dashboard.brand.footer', t('web.config.brand_footer')),
      ]} />
    </section>
    <ServiceMaintenance />
  </div>;
}
