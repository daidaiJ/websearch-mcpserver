import * as Dialog from '@radix-ui/react-dialog';
import * as Select from '@radix-ui/react-select';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useCapabilities, useDiagnosticAction, type DiagnosticOption } from './CapabilityData';
import { DiagnosticResultView, serviceName } from './DiagnosticResultView';
import { useDraft } from './draft';
import { t } from './i18n';
import { Icon } from './navigation';
import './capabilities.css';

export const DIAGNOSTIC_TARGETS = { proxy: '@proxy', search: '@search', fetch: '@fetch' } as const;
const NO_SOURCES: DiagnosticOption[] = [];
const NO_SERVICES: string[] = [];

type Confirmation = { type: 'search'; source: string; name: string; query: string } | { type: 'fetch'; url: string; services: string[] };

function isPublicURLInput(text: string) {
  try {
    const url = new URL(text);
    return /^https?:\/\//i.test(text) && Boolean(url.hostname) && !url.username && !url.password;
  } catch { return false; }
}

function DiagnosticLab({ item }: { item?: string }) {
  const { settings, phase, loadError } = useDraft();
  const { data, reload, loading, error } = useCapabilities();
  const { active, outcomes, run } = useDiagnosticAction();
  const [sourceOverride, setSource] = useState<string>();
  const [confirm, setConfirm] = useState<Confirmation>();
  const [notice, setNotice] = useState('');
  const [searchLabel, setSearchLabel] = useState('');
  const resumeFocus = useRef<HTMLElement | null>(null);
  const sources = data?.diagnostics?.sources ?? NO_SOURCES;
  const services = data?.diagnostics?.fetch_services ?? NO_SERVICES;
  const fetchEnabled = data?.configured_tools?.find(tool => tool.name === 'cleanfetch')?.fetch_enabled === true;
  const proxy = data?.runtime?.proxy;
  const targetIsSource = item !== undefined && !Object.values<string>(DIAGNOSTIC_TARGETS).includes(item);
  const routeSource = targetIsSource ? sources.find(source => source.id === item || source.name.toLocaleLowerCase() === item.toLocaleLowerCase())?.id : undefined;
  const sourceId = sourceOverride ?? (targetIsSource ? routeSource : sources[0]?.id) ?? '';
  const chosen = sources.find(source => source.id === sourceId);
  const permitted = !loadError && settings?.local_action_allowed === true && phase === 'idle';
  const disabled = !permitted || Boolean(active || confirm);
  const optionsReady = Boolean(data?.diagnostics) && !loading && !error;
  const targetCard = item === DIAGNOSTIC_TARGETS.proxy ? 'proxy' : item === DIAGNOSTIC_TARGETS.fetch ? 'fetch' : item ? 'search' : undefined;

  useEffect(() => {
    if (!targetCard) return;
    const frame = requestAnimationFrame(() => document.getElementById(`cap-${targetCard}-heading`)?.scrollIntoView({ block: 'nearest' }));
    return () => cancelAnimationFrame(frame);
  }, [targetCard]);

  const proxyModes: Record<string, string> = {
    auto: t('web.config.proxy_auto'), explicit: t('web.config.proxy_explicit'), disabled: t('web.config.proxy_disabled'),
  };

  function prepare(event: FormEvent<HTMLFormElement>, next: Confirmation) {
    resumeFocus.current = event.currentTarget.querySelector<HTMLButtonElement>('button[type="submit"]');
    setConfirm(next);
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (disabled || !optionsReady || !chosen) return;
    setNotice('');
    const query = String(new FormData(event.currentTarget).get('query') ?? '').trim();
    if (!query) { setNotice(t('web.capNext.empty_query')); return; }
    if (chosen.confirm) prepare(event, { type: 'search', source: chosen.id, name: chosen.name, query });
    else {
      setSearchLabel(chosen.name);
      void run('search', { source: chosen.id, query, confirm: false });
    }
  }

  function fetchURL(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (disabled || !optionsReady || !fetchEnabled) return;
    setNotice('');
    const url = String(new FormData(event.currentTarget).get('url') ?? '').trim();
    if (!isPublicURLInput(url)) { setNotice(t('web.capNext.invalid_fetch_url')); return; }
    if (services.length) prepare(event, { type: 'fetch', url, services });
    else void run('fetch', { url, confirm: false });
  }

  function accept() {
    if (!confirm || !permitted || !optionsReady || active) { setConfirm(undefined); return; }
    const next = confirm;
    setConfirm(undefined);
    if (next.type === 'search') {
      if (!sources.some(source => source.id === next.source && source.confirm)) { setNotice(t('web.capNext.options_changed')); return; }
      setSearchLabel(next.name);
      void run('search', { source: next.source, query: next.query, confirm: true });
    } else {
      if (!fetchEnabled || next.services.length !== services.length || !next.services.every(service => services.includes(service))) {
        setNotice(t('web.capNext.options_changed'));
        return;
      }
      void run('fetch', { url: next.url, confirm: true });
    }
  }

  function outcome(kind: 'proxy' | 'search' | 'fetch') {
    const value = outcomes[kind];
    if (active === kind) return <p className="cap-note" role="status">{t('web.capNext.running')}</p>;
    if (value?.error) return <p className="cap-error" role="alert">{value.error}</p>;
    if (value?.result) return <>
      {kind === 'search' && <p className="cap-note">{t('web.capNext.result_for', { Source: searchLabel })}</p>}
      <DiagnosticResultView result={value.result} connection={kind === 'proxy'} />
    </>;
  }

  return <div className="cap-lab">
    <p className="cap-manual-note"><Icon name="shield" size={18} />{t('web.capNext.manual_boundaries')}</p>
    {!permitted && <p className="cap-note">{loadError ? t('web.runtime_action.permission_unavailable') : !settings ? t('web.runtime_action.permission_loading') : phase !== 'idle' ? t('web.note.applying_configuration') : t('web.runtime_action.source_not_allowed')}</p>}
    {loading && <p className="cap-note" role="status">{t('web.note.loading')}</p>}
    {error && <div className="cap-notice"><p className="cap-error" role="alert">{error}</p><button className="cap-button" type="button" onClick={reload}>{t('web.capNext.reload')}</button></div>}
    {notice && <p className="cap-error" role="alert">{notice}</p>}
    <div className="cap-lab-grid">
      <section className="cap-panel cap-proxy" aria-labelledby="cap-proxy-heading" data-target={targetCard === 'proxy' || undefined}>
        <header className="cap-panel-head"><h2 id="cap-proxy-heading" tabIndex={-1}>{t('web.capNext.proxy_probe')}</h2><p>{t('web.capNext.proxy_help')}</p></header>
        <div className="cap-proxy-settings">
          <dl className="cap-proxy-data">
            <div><dt>{t('web.config.proxy_mode')}</dt><dd>{proxy ? proxyModes[proxy.mode] ?? t('web.status.unknown') : '—'}</dd></div>
            <div><dt>{t('web.capNext.resolved_endpoint')}</dt><dd>{proxy?.endpoint ? <code>{proxy.endpoint}</code> : proxy?.resolved ? t('web.status.unavailable') : proxy ? t('web.diagnostics.unconfigured') : '—'}</dd></div>
          </dl>
          <button className="cap-button" type="button" disabled={disabled} onClick={() => void run('proxy', {})}>{t('web.diagnostics.probe_proxy')}</button>
        </div>
        {outcome('proxy')}
      </section>
      <section className="cap-panel" aria-labelledby="cap-search-heading" data-target={targetCard === 'search' || undefined}>
        <header className="cap-panel-head"><h2 id="cap-search-heading" tabIndex={-1}>{t('web.capNext.single_search')}</h2><p>{t('web.capNext.search_help')}</p></header>
        <form className="cap-lab-form" onSubmit={search}>
          <div className="cap-field">
            <label id="cap-diagnostic-source">{t('web.diagnostics.search_source')}</label>
            <Select.Root value={chosen?.id ?? ''} onValueChange={setSource} disabled={disabled || !optionsReady || !sources.length}>
              <Select.Trigger className="cap-source-select" aria-labelledby="cap-diagnostic-source" id="cap-diagnostic-picker">
                <Select.Value placeholder={t('web.capNext.select_source')} /><Select.Icon><svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg></Select.Icon>
              </Select.Trigger>
              <Select.Portal>
                <Select.Content position="popper" sideOffset={4} className="cap-source-options" collisionPadding={8}>
                  <Select.Viewport>
                    {sources.map(source => <Select.Item key={source.id} value={source.id} className="cap-source-option">
                      <Select.ItemText>{source.name}</Select.ItemText>
                      {source.confirm && <span className="cap-option-note" aria-hidden="true">{t('web.capNext.may_use_quota')}</span>}
                    </Select.Item>)}
                  </Select.Viewport>
                </Select.Content>
              </Select.Portal>
            </Select.Root>
            {optionsReady && targetIsSource && !sourceOverride && !chosen && <small className="cap-error">{t('web.capNext.unavailable_source', { Source: item })}</small>}
            {optionsReady && !sources.length && <small>{t('web.capNext.no_search_sources')}</small>}
          </div>
          <label className="cap-field">{t('web.diagnostics.query')}<input name="query" maxLength={300} required disabled={disabled || !optionsReady || !chosen} /></label>
          <div className="cap-actions"><button className="cap-button cap-primary" type="submit" disabled={disabled || !optionsReady || !chosen}>{t('web.diagnostics.run_search')}</button>
            {chosen?.confirm && <span className="cap-note">{t('web.capNext.may_use_quota')}</span>}</div>
        </form>
        {outcome('search')}
      </section>
      <section className="cap-panel" aria-labelledby="cap-fetch-heading" data-target={targetCard === 'fetch' || undefined}>
        <header className="cap-panel-head"><h2 id="cap-fetch-heading" tabIndex={-1}>{t('web.capNext.single_fetch')}</h2><p>{t('web.capNext.fetch_help')}</p></header>
        <form className="cap-lab-form" onSubmit={fetchURL}>
          <label className="cap-field">{t('web.diagnostics.url')}<input name="url" type="url" maxLength={2048} required spellCheck={false} autoComplete="off" disabled={disabled || !optionsReady || !fetchEnabled} /></label>
          {optionsReady && !fetchEnabled && <p className="cap-note">{t('web.capNext.fetch_disabled')}</p>}
          {services.length > 0 && <dl className="cap-remote-services">
            <dt>{t('web.capNext.possible_services')}</dt><dd>{services.map(service => <span className="cap-badge" key={service}>{serviceName(service)}</span>)}</dd>
          </dl>}
          <div className="cap-actions"><button className="cap-button cap-primary" type="submit" disabled={disabled || !optionsReady || !fetchEnabled}>{t('web.diagnostics.run_fetch')}</button></div>
        </form>
        {outcome('fetch')}
      </section>
    </div>
    <Dialog.Root open={Boolean(confirm)} onOpenChange={open => { if (!open) setConfirm(undefined); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="cap-modal-overlay" />
        <Dialog.Content className="cap-modal" onCloseAutoFocus={event => {
          event.preventDefault();
          const button = resumeFocus.current;
          if (button?.isConnected) {
            button.focus();
            if (document.activeElement !== button) button.closest('section')?.querySelector<HTMLElement>('h2')?.focus();
          }
        }}>
          <Dialog.Title>{t('web.capNext.confirm_quota')}</Dialog.Title>
          <Dialog.Description>{confirm?.type === 'search' ? t('web.diagnostics.confirm_search', { source: confirm.name })
            : confirm?.type === 'fetch' ? t('web.diagnostics.confirm_fetch', { sources: confirm.services.map(serviceName).join(', ') }) : ''}</Dialog.Description>
          <div className="cap-modal-actions">
            <Dialog.Close className="cap-button" type="button">{t('web.action.cancel')}</Dialog.Close>
            <button className="cap-button cap-primary" type="button" onClick={accept}>{t('web.capNext.confirm_run')}</button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </div>;
}

export default function Diagnostics({ item }: { item?: string; refresh: number }) {
  return <DiagnosticLab key={item} item={item} />;
}
