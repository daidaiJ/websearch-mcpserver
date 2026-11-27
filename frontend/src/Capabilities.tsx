import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useCapabilities, useDiagnosticAction, type CapabilityOverview } from './CapabilityData';
import { CapabilityFields, MODULE_IDS, moduleFields, moduleNames, type ModuleId } from './CapabilityFields';
import { DiagnosticResultView } from './DiagnosticResultView';
import { useDraft, type DraftCredentials } from './draft';
import { getLocale, t } from './i18n';
import { destinationHash, Icon } from './navigation';
import './capabilities.css';

const number = (value: number) => new Intl.NumberFormat(getLocale()).format(value);

function CapabilityStatus({ module, data }: { module: ModuleId; data?: CapabilityOverview }) {
  const tool = data?.configured_tools?.find(entry => entry.name === module);
  const observation = data?.tools?.find(entry => entry.name === module);
  const llm = data?.runtime?.llm;
  const active = module === 'llm' ? llm?.enabled : module === 'cleanfetch' ? tool?.fetch_enabled : tool?.enabled;
  const config = active === undefined ? t('web.status.unknown') : active ? t('web.status.enabled_in_config') : t('web.status.disabled_in_config');
  const registrations: Record<string, string> = {
    unprobed: t('web.status.not_probed'), pending: t('web.status.awaiting_registration'), unavailable: t('web.status.unavailable'),
  };
  const registration = tool?.registered ? t('web.status.registered') : tool ? registrations[tool.state] ?? t('web.status.not_registered') : t('web.status.unknown');
  const observations: Record<string, string> = {
    healthy: t('web.status.running_normally'), degraded: t('web.status.degraded'), down: t('web.status.failing'),
  };
  const file = tool?.file;
  const connectivity = {
    available: t('web.diagnostics.connectivity_available'), unavailable: t('web.diagnostics.connectivity_unavailable'),
    unprobed: t('web.status.not_probed'), not_configured: t('web.status.not_configured'),
  };

  return <>
    <dl className="cap-state-grid">
      <div><dt>{module === 'cleanfetch' ? t('web.label.url_fetching') : t('web.capNext.configuration')}</dt><dd><span className="cap-badge" data-tone={active ? 'ok' : undefined}>{config}</span></dd></div>
      {module !== 'llm' && <>
        <div><dt>{t('web.capNext.registration')}</dt><dd><span className="cap-badge" data-tone={tool?.registered ? 'ok' : tool?.state === 'unavailable' ? 'warn' : undefined}>{registration}</span></dd></div>
        <div><dt>{t('web.capNext.observation')}</dt><dd><span className="cap-badge" data-tone={observation?.status === 'healthy' ? 'ok' : observation?.status === 'down' ? 'danger' : observation?.status === 'degraded' ? 'warn' : undefined}>
          {observation?.today?.requests ? (observations[observation.status] ?? t('web.status.unknown')) : data ? t('web.status.awaiting_real_calls') : t('web.status.unknown')}
        </span></dd></div>
      </>}
    </dl>
    <div className="cap-runtime-notes">
      {module === 'llm' && llm?.enabled && llm.model_id && <p>{t('web.config.model')} <code>{llm.model_id}</code></p>}
      {module === 'cleanfetch' && data?.runtime?.mcp_hub?.fetch_mode && <p>{t('web.config.fetch_mode')} <code>{data.runtime.mcp_hub.fetch_mode}</code></p>}
      {tool?.pdf && <p>{t('web.capNext.pdf_runtime', { Pages: number(tool.pdf.max_pages), Limit: number(tool.pdf.mineru_page_limit) })}</p>}
      {module === 'pdf_parser' && <p>{t('web.capNext.pdf_progress')}</p>}
      {tool?.history && <p>{t('web.capNext.retention_days', { Days: number(tool.history.retention_days) })}</p>}
      {file && <p>{t('web.label.allowlisted_roots')} <strong>{number(file.allowed_root_count)}</strong>{file.connectivity && <> · {connectivity[file.connectivity as keyof typeof connectivity] ?? t('web.status.unknown')}</>}</p>}
    </div>
  </>;
}

function EverythingScope() {
  const { settings, canWrite, writeReason, hasCredentials, adminRequest } = useDraft();
  const [open, setOpen] = useState(false);
  const [scope, setScope] = useState<{ url: string; roots?: string[] }>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const request = useRef<AbortController | null>(null);
  const usernameRequired = settings?.admin_username_configured === true;
  const allowed = canWrite;

  useEffect(() => () => request.current?.abort(), []);

  async function read(credentials?: DraftCredentials) {
    if (!allowed || request.current) return;
    const abort = new AbortController();
    request.current = abort;
    setError('');
    setLoading(true);
    try {
      const result = await adminRequest<{ url: string; roots?: string[] }>('everything/scope', {
        signal: AbortSignal.any([abort.signal, AbortSignal.timeout(5000)]),
      }, credentials);
      if (!abort.signal.aborted) setScope(result);
    } catch (caught) {
      if (!abort.signal.aborted) setError(caught instanceof Error ? caught.message : t('web.status.read_failed'));
    } finally {
      if (!abort.signal.aborted) {
        request.current = null;
        setLoading(false);
      }
    }
  }

  function change(next: boolean) {
    setOpen(next);
    if (next) { if (hasCredentials) void read(); }
    else {
      request.current?.abort();
      request.current = null;
      setLoading(false);
      setError('');
      setScope(undefined);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const input = { password: String(data.get('password') ?? ''), username: String(data.get('username') ?? '') };
    event.currentTarget.reset();
    if (input.password && (!usernameRequired || input.username.trim())) void read(input);
  }

  return <Dialog.Root open={open} onOpenChange={change}>
    <Dialog.Trigger className="cap-button" type="button" disabled={!allowed} title={allowed ? undefined : writeReason}>{t('web.config.show_scope')}</Dialog.Trigger>
    <Dialog.Portal>
      <Dialog.Overlay className="cap-modal-overlay" />
      <Dialog.Content className="cap-modal">
        <Dialog.Title>{t('web.config.show_scope')}</Dialog.Title>
        <Dialog.Description>{t('web.capNext.scope_read_only')}</Dialog.Description>
        {error && <p role="alert" className="cap-error">{error}</p>}
        {loading && <p role="status">{t('web.note.loading')}</p>}
        {scope ? <dl className="cap-scope">
          <div><dt>{t('web.config.scope_url')}</dt><dd><code>{scope.url || '—'}</code></dd></div>
          <div><dt>{t('web.label.allowlisted_roots')}</dt><dd>{scope.roots?.length ? <ul>{scope.roots.map(root => <li key={root}><code>{root}</code></li>)}</ul> : t('web.status.directories_unrestricted')}</dd></div>
        </dl> : !loading && (hasCredentials
          ? <button className="cap-button" type="button" disabled={!allowed} onClick={() => void read()}>{t('web.config.show_scope')}</button>
          : <form className="cap-auth" onSubmit={submit}>
            {usernameRequired && <label>{t('web.label.admin_username')}<input name="username" type="text" autoComplete="username" required /></label>}
            <label>{t('web.label.admin_password')}<input name="password" type="password" autoComplete="current-password" required /></label>
            <button className="cap-button cap-primary" type="submit" disabled={!allowed}>{t('web.config.show_scope')}</button>
          </form>)}
        <div className="cap-modal-actions"><Dialog.Close type="button" className="cap-button">{t('web.next.close')}</Dialog.Close></div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

function EverythingActions({ configured }: { configured: boolean }) {
  const { reload } = useCapabilities();
  const { settings, phase, loadError } = useDraft();
  const { active, outcomes, run } = useDiagnosticAction();
  const accessible = !loadError && settings?.local_action_allowed === true && phase === 'idle';
  const outcome = outcomes.everything;

  return <div className="cap-everything">
    <div className="cap-actions">
      <button className="cap-button" type="button" disabled={!configured || !accessible || Boolean(active)}
        onClick={() => void run('everything', {}, reload)}>
        <Icon name="refresh" size={15} />{t('web.diagnostics.reprobe_everything')}
      </button>
      <EverythingScope />
    </div>
    {active && <p className="cap-note" role="status">{t('web.capNext.running')}</p>}
    {outcome?.error && <p className="cap-error" role="alert">{outcome.error}</p>}
    {outcome?.result && <DiagnosticResultView result={outcome.result} connection />}
  </div>;
}

export default function Capabilities({ item }: { item?: string; refresh: number }) {
  const module = MODULE_IDS.find(id => item === id) ?? MODULE_IDS[0];
  const names = moduleNames();
  const fields = moduleFields(module);
  const { data, loading, error, reload } = useCapabilities();
  const { settings, canWrite, writeReason } = useDraft();
  const loadedEverything = data?.configured_tools?.find(tool => tool.name === 'file_search');
  return <div className="cap-workspace">
    <nav className="cap-module-menu" aria-label={t('web.capNext.module_navigation')}>
      {MODULE_IDS.map(id => <a key={id} href={destinationHash({ page: 'capabilities', item: id })} aria-current={module === id ? 'page' : undefined}>{names[id]}</a>)}
    </nav>
    <article key={module} className="cap-panel" aria-labelledby="cap-module-heading">
      <header className="cap-panel-head"><h2 id="cap-module-heading">{names[module]}</h2><p>{fields.description}</p></header>
      {loading && <p className="cap-note" role="status">{t('web.note.loading')}</p>}
      {error && <div className="cap-notice"><p className="cap-error" role="alert">{error}</p><button className="cap-button" type="button" onClick={reload}>{t('web.capNext.reload')}</button></div>}
      <CapabilityStatus module={module} data={data} />
      {!canWrite && <p className="cap-permission cap-note">{writeReason}</p>}
      {settings && <div className="cap-settings">
        <CapabilityFields fields={fields.basic} />
        {!!fields.advanced.length && <details className="cap-advanced"><summary>{t('web.config.advanced')}</summary><CapabilityFields fields={fields.advanced} /></details>}
      </div>}
      {module === 'file_search' && <EverythingActions configured={loadedEverything?.enabled === true && !error && !loading} />}
    </article>
  </div>;
}
