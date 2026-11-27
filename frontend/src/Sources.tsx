import { formatters } from './formatters'
import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useRef, useState, type ChangeEvent } from 'react';
import { useDraft } from './draft';
import { t, useLocale } from './i18n';
import { Icon, destinationHash, useNavigate, type IconName } from './navigation';
import { cooldownSeconds, useCooldownClock, useSourcesData, type QuotaItem, type Source, type SourceCategory } from './SourcesCommands';

type CategoryFilter = 'all' | SourceCategory;
type StatusFilter = 'all' | 'enabled' | 'issues' | 'cooling' | 'unconfigured';
const categories: Record<CategoryFilter, { label: string; icon: IconName }> = {
  all: { label: 'web.label.all', icon: 'layers' }, engine: { label: 'web.page.web_engines', icon: 'globe' },
  api: { label: 'web.page.api_providers', icon: 'box' }, academic: { label: 'web.page.academic_sources', icon: 'activity' },
  mcphub: { label: 'web.page.mcphub_sources', icon: 'layers' },
};
const statusLabels: Record<StatusFilter, string> = { all: 'web.sources.all_statuses', enabled: 'web.label.enabled', issues: 'web.sources.degraded_filter', cooling: 'web.sources.cooldown_filter', unconfigured: 'web.status.not_configured' };
const errors: Record<string, string> = {
  rate_limit: 'web.status.rate_limited', captcha: 'web.status.captcha', access_denied: 'web.status.access_denied', timeout: 'web.status.timeout',
  parse: 'web.status.parse_failed', no_result: 'web.status.no_results', network: 'web.status.network_server', unknown: 'web.status.unknown_error',
};
const unknown = '—';
const errorLabel = (kind?: string) => kind ? errors[kind] ? t(errors[kind]) : kind : t('web.status.unknown_error');
const num = (value?: number) => typeof value === 'number' && Number.isFinite(value) ? formatters().decimal.format(value) : unknown;
const ms = (value?: number) => value === undefined ? unknown : value >= 1000 ? `${num(value / 1000)} s` : `${num(value)} ms`;
const date = (value: string) => Number.isFinite(Date.parse(value)) ? formatters().timestamp.format(new Date(value)) : unknown;
const observation = (source: Source) => source.observation_status ?? source.status;
const warning = (source: Source, now: number) => source.suspended && (!source.suspended_until || Date.parse(source.suspended_until) > now);

type Field = { key: string; label: string; min?: number; max?: number; step?: number | 'any'; numericChoices?: number[]; pattern?: string; maxLength?: number };
const sourceFields: Record<string, Field[]> = {
  yuanbao: [
    { key: 'yuanbao.monthly_limit', label: 'web.config.yuanbao_limit', min: 0, max: 10000000 },
    { key: 'yuanbao.mode', label: 'web.label.search_mode', numericChoices: [0, 1, 2] },
    { key: 'yuanbao.industry', label: 'web.config.industry' }, { key: 'yuanbao.site', label: 'web.config.site', maxLength: 1032 },
  ],
  doubao: [
    { key: 'doubao.version', label: 'web.label.doubao_api_version' }, { key: 'doubao.icp_host_only', label: 'web.config.icp_only' },
    { key: 'doubao.need_content', label: 'web.config.include_text' }, { key: 'doubao.auth_level', label: 'web.config.auth_level', numericChoices: [0, 1] },
    { key: 'doubao.query_rewrite', label: 'web.config.rewrite_query' }, { key: 'doubao.num_results', label: 'web.label.max_results', min: 1, max: 50 },
  ],
  baidu: [{ key: 'baidu.enable_ai_search', label: 'web.config.baidu_ai' }, { key: 'baidu.model', label: 'web.config.model', maxLength: 256 }, { key: 'baidu.search_mode', label: 'web.label.search_mode' }],
  tavily: [{ key: 'tavily.include_raw_content', label: 'web.config.include_text' }],
  exa: [{ key: 'exa.include_text', label: 'web.config.include_text' }, { key: 'exa.lookback_days', label: 'web.config.lookback_days', min: 1, max: 36500 }, { key: 'exa.num_results', label: 'web.label.max_results', min: 1, max: 100 }],
  keenable: [{ key: 'keenable.live', label: 'web.config.live' }], wikipedia: [{ key: 'wikipedia.lang', label: 'web.config.language', pattern: '[a-z][a-z0-9]{0,14}(-[a-z0-9]{1,15}){0,3}' }],
  googlenews: [{ key: 'google_news.edition', label: 'web.config.edition' }], so360: [{ key: 'so360.safe_search', label: 'web.config.safe_search', numericChoices: [0, 1, 2] }],
};

function fieldsFor(source: Source) {
  const prefix = `smartsearch.engines.${source.score_key}`;
  const score = source.score_key ? [
    { key: `${prefix}.weight`, label: 'web.config.engine_weight', min: 0, max: 1000, step: 'any' as const },
    { key: `${prefix}.max_size`, label: 'web.label.max_results', min: 0, max: 360 },
    { key: `${prefix}.min_score`, label: 'web.config.engine_min_score', min: 0, max: 1, step: 'any' as const },
  ] : [];
  return [...sourceFields[source.id] ?? [], ...score];
}

function FieldEdit({ field }: { field: Field }) {
  const draft = useDraft();
  const label = t(field.label);
  const locked = draft.settings?.locked_fields?.[field.key];
  const value = locked ? draft.settings?.values[field.key] : draft.value(field.key);
  const version = draft.settings?.locked_fields?.['doubao.version'] ? draft.settings.values['doubao.version'] : draft.value('doubao.version');
  const globalOnly = field.key === 'doubao.icp_host_only' && version !== 'global';
  const customOnly = ['doubao.need_content', 'doubao.auth_level', 'doubao.query_rewrite'].includes(field.key) && version !== 'custom';
  const globalLimit = field.key === 'doubao.num_results' && version === 'global';
  const reason = locked || draft.fieldReason(field.key) || (draft.phase !== 'idle' ? t('web.note.applying_configuration') : '') ||
    (globalOnly ? t('web.note.applies_to_global_only_the_current_value_is_kept') : customOnly ? t('web.config.custom_only') : '');
  const choices = field.numericChoices ?? draft.settings?.options?.[field.key];
  const numeric = typeof value === 'number' || field.min !== undefined;
  const issue = draft.errors[field.key];
  const id = `next-field-${field.key}`;
  const note = reason || issue?.message;
  const attrs = { id, disabled: Boolean(reason), 'aria-describedby': note ? `${id}-note` : undefined, 'aria-invalid': Boolean(issue) };

  function change(event: ChangeEvent<HTMLInputElement>) {
    const input = event.currentTarget;
    const message = !input.validity.valid ? globalLimit && input.validity.rangeOverflow ? t('web.config.invalid_global_results') : input.validationMessage : '';
    if (message) draft.setFieldError(field.key, message, input.value);
    else draft.stage(field.key, numeric ? input.valueAsNumber : input.value);
  }

  return <div className="next-source-field">
    <label htmlFor={id}>{label}</label>
    {typeof value === 'boolean' ? <input {...attrs} className="next-source-toggle" type="checkbox" role="switch" checked={value} onChange={event => draft.stage(field.key, event.currentTarget.checked)} />
      : choices ? <select {...attrs} value={String(value ?? '')} onChange={event => draft.stage(field.key, field.numericChoices ? Number(event.currentTarget.value) : event.currentTarget.value)}>
        {!choices.some(choice => choice === value) && <option value={String(value ?? '')} disabled>{String(value ?? unknown)}</option>}
        {choices.map(option => <option key={option} value={option}>{option === '' ? t('web.config.any_value') : option}</option>)}
      </select> : <input {...attrs} type={numeric ? 'number' : 'text'} min={field.min} max={globalLimit ? 20 : field.max} step={numeric ? field.step ?? 1 : undefined}
        pattern={field.pattern} maxLength={field.maxLength} required={numeric} value={issue?.rawValue ?? String(value ?? '')} onChange={change} />}
    {note && <small id={`${id}-note`} data-error={Boolean(issue) || undefined}>{note}</small>}
  </div>;
}

function SourceSwitch({ source }: { source: Source }) {
  const draft = useDraft();
  const path = source.control_path;
  const locked = path && draft.settings?.locked_fields?.[path];
  const value = path ? locked ? draft.settings?.values[path] : draft.value(path) : undefined;
  const checked = value === undefined ? source.enabled : value === null ? source.active : source.inverted ? value === false : value === true;
  const reason = path ? locked || draft.fieldReason(path) || (draft.phase !== 'idle' ? t('web.note.applying_configuration') : '') : t('web.sources.managed_switch');
  return <span className="next-source-switch" title={reason || undefined}>
    <input type="checkbox" role="switch" className="next-source-toggle" disabled={Boolean(reason)} checked={checked}
      aria-label={`${source.name} · ${t('web.status.enabled_in_config')}`} onChange={event => { if (path) draft.stage(path, source.inverted ? !event.currentTarget.checked : event.currentTarget.checked); }} />
  </span>;
}

function SourceState({ source, now }: { source: Source; now: number }) {
  const activeCooldown = cooldownSeconds(source, now);
  const health = observation(source);
  const healthLabel = !source.last_seen_at ? 'web.status.no_calls_yet' : health === 'healthy' ? 'web.status.healthy' : health === 'down' ? 'web.status.failing' : health === 'degraded' ? 'web.status.degraded' : 'web.status.unknown';
  return <div className="next-source-states">
    <span className="next-source-health" data-tone={health}><i aria-hidden="true" />{t(healthLabel)}</span>
    {warning(source, now) && <span className="next-source-badge" data-tone="observed" title={t('web.note.historical_observation_warning') + errorLabel(source.suspend_reason)}>{t('web.status.observation_warning')}</span>}
    {activeCooldown > 0 && <span className="next-source-badge" data-tone="cooldown" title={[errorLabel(source.actual_cooldown?.kind), source.actual_cooldown?.reason, date(source.actual_cooldown!.until)].filter(Boolean).join(' · ')}>{t('web.label.cooldown_countdown', { count: num(activeCooldown) })}</span>}
    {!source.configured && <span className="next-source-badge">{t('web.status.not_configured')}</span>}
  </div>;
}

function HealthStrip({ source, detail = false }: { source: Source; detail?: boolean }) {
  const outcomes = (source.recent_outcomes ?? []).slice(-20);
  const padded = [...Array<null>(20 - outcomes.length).fill(null), ...outcomes];
  const label = outcomes.length ? t('web.note.last_real_calls', { count: outcomes.length }) : t('web.note.no_real_calls_yet');
  const describe = (value: boolean | null) => value === null ? t('web.sources.no_sample') : t(value ? 'web.status.success' : 'web.status.failure');
  return <div className="next-source-strip" data-detail={detail || undefined} role={detail ? 'list' : 'img'} aria-label={label} title={detail ? undefined : label}>
    {padded.map((value, index) => <span data-outcome={value === null ? 'empty' : value ? 'success' : 'failure'} key={index} role={detail ? 'listitem' : undefined}
      aria-hidden={detail ? undefined : true} title={detail ? describe(value) : undefined}>{detail && <span className="next-sr-only">{describe(value)}</span>}</span>)}
  </div>;
}

function FailureTags({ source }: { source: Source }) {
  if (!source.error_kinds?.length) return null;
  return <ul className="next-source-failures" aria-label={t('web.label.failure_breakdown')}>
    {source.error_kinds.map(error => <li key={error.kind}>{errorLabel(error.kind)} ×{num(error.count)}</li>)}
  </ul>;
}

function Metrics({ source }: { source: Source }) {
  const daily = source.today?.day ? source.today : undefined;
  const count = daily?.requests;
  const rate = daily && daily.requests > 0 ? formatters().percent.format(daily.successes / daily.requests) : unknown;
  const avg = daily && daily.requests > 0 ? Math.round(daily.duration_ms / daily.requests) : undefined;
  return <dl className="next-source-metrics">
    <div><dt>{t('web.label.attempts_today')}</dt><dd>{num(count)}</dd></div><div><dt>{t('web.label.success_rate')}</dt><dd>{rate}</dd></div>
    <div><dt>{t('web.label.average_latency')}</dt><dd title={t('web.sources.avg_today')}>{ms(avg)}</dd></div><div><dt>P95</dt><dd title={t('web.sources.p95_latest')}>{ms(source.recent_outcomes?.length || source.p95_ms ? source.p95_ms : undefined)}</dd></div>
  </dl>;
}

function QuotaMeter({ label, used, limit, remaining, unit, compact }: { label: string; used?: number; limit?: number; remaining?: number; unit: string; compact?: boolean }) {
  const measured = typeof used === 'number' && typeof limit === 'number' && limit > 0;
  const ratio = measured ? used / limit : undefined;
  const severity = ratio === undefined ? undefined : ratio >= 1 ? 'exhausted' : ratio >= .8 ? 'near' : undefined;
  const amount = t('web.quota.amount', { Used: num(used), Limit: num(limit && limit > 0 ? limit : undefined), Unit: unit });
  return <div className="next-source-quota-meter" data-severity={severity}>
    <div><span>{label}</span><strong>{amount}</strong></div>
    {ratio !== undefined && <progress max={100} value={Math.max(0, Math.min(100, ratio * 100))} aria-label={`${label} · ${amount}`} />}
    {(severity || !compact && remaining !== undefined) && <small>
      {severity && <span>{t(severity === 'exhausted' ? 'web.quota.limit_reached' : 'web.quota.near_limit')}</span>}
      {!compact && remaining !== undefined && <span>{t('web.quota.remaining', { Count: num(remaining) })}</span>}
    </small>}
  </div>;
}

function Quotas({ source, item, compact = false }: { source: Source; item?: QuotaItem; compact?: boolean }) {
  if (source.category !== 'api') return null;
  const details = item?.details ?? {};
  const number = (key: string) => typeof details[key] === 'number' ? Number(details[key]) : undefined;
  const localOnly = item?.source === 'local';
  const failed = item?.status === 'query_failed' || item?.official_query_failed;
  const oldLocalUsed = number('local_search_used');
  const local = item?.local_usage ?? (localOnly ? item : oldLocalUsed !== undefined ? { used: oldLocalUsed, limit: number('local_search_limit'), status: 'available' } : undefined);
  const official = item && !localOnly && item.status === 'available' ? item : undefined;
  const unit = t(item?.unit === 'credits' ? 'web.quota.credits' : 'web.quota.calls');
  const fetchUsed = number('fetch_used');
  const fetchLimit = number('fetch_limit');
  const fetchAvailable = source.id === 'tinyfish' && fetchUsed !== undefined && Boolean(fetchLimit);
  const fetchReset = typeof details.fetch_reset_at === 'string' ? details.fetch_reset_at : undefined;
  const reset = local && 'reset_at' in local ? local.reset_at : undefined;
  const QuotaHeading = compact ? 'h3' : 'h4';
  return <div className="next-source-quotas" data-compact={compact || undefined}>
    {failed && <p className="next-source-quota-warning">{t(fetchAvailable ? 'web.sources.official_search_failed' : 'web.quota.official_failed')}{local?.status === 'available' && ` · ${t('web.sources.local_fallback')}`}</p>}
    {(official || fetchAvailable || typeof details.fetch_error === 'string') && <section aria-label={t('web.quota.official')}>
      <QuotaHeading>{t('web.quota.official')}{!compact && official?.plan && <small>{official.plan}</small>}</QuotaHeading>
      {official && <QuotaMeter label={source.id === 'tinyfish' ? t('web.quota.tinyfish_search_reference') : t('web.quota.credits')} {...official} unit={unit} compact={compact} />}
      {source.id === 'tinyfish' && (fetchUsed !== undefined && fetchLimit ? <QuotaMeter label={t('web.quota.fetch')} used={fetchUsed} limit={fetchLimit} remaining={Math.max(0, fetchLimit - fetchUsed)} unit={unit} compact={compact} /> : <p className="next-source-quota-warning">{t('web.quota.fetch_failed')}</p>)}
      {!compact && source.id === 'tinyfish' && fetchAvailable && fetchReset && fetchReset !== official?.reset_at && <p>{t('web.quota.fetch')} · {t('web.label.next_automatic_reset')} · <time dateTime={fetchReset}>{date(fetchReset)}</time></p>}
      {!compact && official && source.id === 'tavily' && <>
        {[['web.quota.plan_usage', 'account_plan_usage', 'account_plan_limit'], ['web.quota.key_usage', 'key_usage', 'key_limit'], ['web.quota.paygo_usage', 'paygo_usage', 'paygo_limit']].map(([label, used, limit]) =>
          number(used) !== undefined || number(limit) !== undefined ? <QuotaMeter key={label} label={t(label)} used={number(used)} limit={number(limit)} unit={unit} /> : null)}
        <div className="next-source-usage-detail">{[['web.quota.search', 'search_usage'], ['web.quota.extract', 'extract_usage']].map(([label, key]) => number(key) !== undefined && <span key={key}>{t(label)} {num(number(key))}</span>)}</div>
      </>}
      {!compact && official && (official.reset_at || source.id === 'tavily') && <p>{t('web.label.next_automatic_reset')} · {official.reset_at ? <time dateTime={official.reset_at}>{date(official.reset_at)}</time> : t('web.quota.tavily_reset')}</p>}
    </section>}
    {local?.status === 'available' ? <section aria-label={t('web.quota.local')}>
      <QuotaHeading>{t('web.quota.local')}</QuotaHeading><QuotaMeter label={t('web.quota.search')} {...local} unit={t('web.quota.calls')} compact={compact} />
      {!compact && reset && <p>{t('web.label.next_automatic_reset')} · <time dateTime={reset}>{date(reset)}</time></p>}
    </section> : (!compact && (source.quota_queryable || item)) && <p>{t('web.quota.local')} · {t('web.quota.no_data')}</p>}
    {!official && !local && !failed && item?.status === 'not_configured' && <p>{t('web.status.not_configured')}</p>}
    {!compact && item?.error && <p>{item.error}</p>}
    {source.quota_skip_reason && <p className="next-source-quota-warning">{source.quota_skip_reason}</p>}
  </div>;
}

function SourceEntry({ source, now, item, selected, open }: { source: Source; now: number; item?: QuotaItem; selected: boolean; open: (id: string, opener?: HTMLElement) => void }) {
  return <article className="next-source-entry" data-current={selected || undefined} data-source={source.id} onClick={event => {
    if (!(event.target as HTMLElement).closest('button,input,label,.next-source-switch')) open(source.id, event.currentTarget.querySelector('button') ?? undefined);
  }}>
    <div className="next-source-identity">
      <h2><button type="button" aria-label={source.name} aria-expanded={selected} aria-haspopup="dialog" onClick={event => open(source.id, event.currentTarget)}><i aria-hidden="true"><Icon name={categories[source.category]?.icon ?? 'layers'} /></i><span><strong>{source.name}</strong><small>{t(categories[source.category]?.label ?? 'web.label.sources')}</small></span></button></h2>
      <SourceSwitch source={source} />
    </div>
    <div className="next-source-participation"><SourceState source={source} now={now} /><p title={source.condition}>{source.participation || source.condition}</p></div>
    <div className="next-source-activity"><small>{t('web.label.last_20')}</small><HealthStrip source={source} /><FailureTags source={source} /></div>
    <Metrics source={source} />
    <Quotas source={source} item={item} compact />
  </article>;
}

function SourceInspector({ source, now, item, close, restoreFocus }: { source?: Source; now: number; item?: QuotaItem; close: () => void; restoreFocus: () => void }) {
  const workspace = useSourcesData();
  const draft = useDraft();
  const navigate = useNavigate();
  const closeRef = useRef<HTMLButtonElement>(null);
  const values = draft.settings?.saved_values ?? draft.settings?.values;
  const fields = source ? fieldsFor(source).filter(field => values && Object.hasOwn(values, field.key)) : [];
  const canRelease = source && cooldownSeconds(source, now) > 0 && source.actual_cooldown;
  const canQuota = source?.quota_queryable && source.configured && (source.id === 'tavily' || source.id === 'tinyfish') ? source.id : undefined;
  const diagnostic = source && workspace.diagnosticOptions.some(option => option.id === source.id);
  useEffect(() => { if (source) closeRef.current?.focus({ preventScroll: true }); }, [source?.id]);
  return <Dialog.Root modal={false} open={Boolean(source)} onOpenChange={open => { if (!open) close(); }}>
    {source && <Dialog.Portal>
      <div className="next-source-scrim" aria-hidden="true" />
      <Dialog.Content className="next-source-inspector" onInteractOutside={event => event.preventDefault()} onCloseAutoFocus={event => { event.preventDefault(); restoreFocus(); }}>
        <header><div><Dialog.Title>{source.name}</Dialog.Title><Dialog.Description>{t(categories[source.category]?.label ?? 'web.label.sources')}</Dialog.Description></div><Dialog.Close ref={closeRef} data-close-source aria-label={t('web.next.close')}><Icon name="close" /></Dialog.Close></header>
        <div className="next-source-inspector-content" key={source.id}>
          <section><SourceState source={source} now={now} /><p>{source.participation}</p><p>{source.condition}</p>
            <div className="next-sources-inline-actions">
              {canRelease && <button type="button" disabled={!workspace.allowed} onClick={() => workspace.requestAction({ type: 'release', engine: canRelease.engine, name: source.name })}>{t('web.runtime_action.release_source', { Name: source.name })}</button>}
              {canQuota && <button type="button" disabled={!workspace.allowed} onClick={() => workspace.requestAction({ type: 'quota', provider: canQuota })}>{t('web.runtime_action.refresh_quota')}</button>}
              {diagnostic ? <a href={destinationHash({ page: 'diagnostics', item: source.id })} onClick={event => { if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return; event.preventDefault(); navigate({ page: 'diagnostics', item: source.id }); }}>{t('web.sources.open_diagnostic')} <Icon name="arrow" size={14} /></a>
                : <button type="button" disabled title={t('web.sources.diagnostic_unavailable')}>{t('web.sources.open_diagnostic')}</button>}
            </div>
            {!workspace.allowed && (canRelease || canQuota) && <p className="next-source-muted">{workspace.reason}</p>}
          </section>
          <section><h3>{t('web.label.last_20')}</h3><HealthStrip source={source} detail /><p className="next-source-muted">{t('web.sources.oldest_first')}</p><Metrics source={source} />
            {source.error_kinds?.length ? <><h4>{t('web.label.failure_breakdown')}</h4><FailureTags source={source} /></> : null}
            {source.last_error && <p className="next-source-last-error">{t('web.label.last_error')} · {source.last_error}</p>}
          </section>
          {source.category === 'api' && <section><h3>{t('web.quota.heading')}</h3>{workspace.quotas.error && <p className="next-source-last-error">{workspace.quotas.error}</p>}
            {!item && (workspace.quotas.loading || !source.quota_skip_reason) && <p className="next-source-muted">{t(workspace.quotas.loading ? 'web.quota.loading' : 'web.quota.no_data')}</p>}<Quotas source={source} item={item} /></section>}
          <section><h3>{t('web.config.source_parameters')}</h3>
            <div className="next-source-field"><span>{t('web.status.enabled_in_config')}</span><SourceSwitch source={source} /></div>
            {!draft.canWrite && <p className="next-source-muted">{draft.writeReason}</p>}
            {source.control_path && draft.settings?.locked_fields?.[source.control_path] && <p className="next-source-muted">{draft.settings.locked_fields[source.control_path]}</p>}
            {!source.control_path && <p className="next-source-muted">{t('web.sources.managed_switch')}</p>}
            {draft.canWrite && (source.control_path || fields.length > 0) && <p className="next-source-muted">{t('web.sources.use_draft')}</p>}
            {fields.map(field => <FieldEdit key={field.key} field={field} />)}
          </section>
        </div>
      </Dialog.Content>
    </Dialog.Portal>}
  </Dialog.Root>;
}

export default function Sources({ item, refresh }: { item?: string; refresh: number }) {
  useLocale();
  const workspace = useSourcesData();
  const { sources, readQuotas, readOverview, quotas } = workspace;
  const now = useCooldownClock(sources);
  const [category, setCategory] = useState<CategoryFilter>('all');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [query, setQuery] = useState('');
  const [view, setView] = useState<'cards' | 'list'>('cards');
  const [selected, setSelected] = useState(item);
  const opener = useRef<HTMLElement | null>(null);
  const keyword = useRef<HTMLInputElement | null>(null);
  const source = selected ? sources.find(entry => entry.id === selected || entry.name === selected || entry.actual_cooldown?.engine === selected) : undefined;
  const byProvider = new Map(quotas.data?.map(item => [item.provider, item]));
  const hasCooldown = sources.some(source => cooldownSeconds(source, now) > 0);
  const canRefreshQuota = sources.some(source => source.quota_queryable && source.configured);
  const terms = query.trim().toLocaleLowerCase().split(/\s+/);
  const visible = sources.filter(source => (category === 'all' || source.category === category) && terms.every(term => `${source.name} ${source.id}`.toLocaleLowerCase().includes(term)) &&
    (status === 'all' || status === 'enabled' && source.enabled || status === 'cooling' && cooldownSeconds(source, now) > 0 || status === 'unconfigured' && !source.configured ||
      status === 'issues' && source.active && (warning(source, now) || ['degraded', 'down'].includes(observation(source)))));

  useEffect(() => { void readQuotas(); }, [readQuotas, refresh]);
  useEffect(() => { setSelected(item); }, [item]);

  function close() {
    setSelected(undefined);
    if (item && location.hash.startsWith('#sources/')) {
      history.replaceState(null, '', '#sources');
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    }
  }

  return <section className="next-sources" aria-label={t('web.page.search_sources')}>
    <div className="next-sources-controls">
      <div className="next-sources-categories" role="group" aria-label={t('web.sources.categories')}>
        {(Object.keys(categories) as CategoryFilter[]).map(group => {
          const counts = sources.filter(source => group === 'all' || source.category === group);
          return <button key={group} type="button" aria-pressed={category === group} onClick={() => setCategory(group)}>{t(categories[group].label)} <small title={t('web.sources.enabled_count_help')}>{counts.filter(source => source.enabled).length}/{counts.length}</small></button>;
        })}
      </div>
      <div className="next-sources-filters">
        <label className="next-sources-keyword"><Icon name="search" size={15} /><input type="search" ref={keyword} value={query} aria-label={t('web.sources.keyword')} placeholder={t('web.sources.keyword')} onChange={event => setQuery(event.currentTarget.value)} /></label>
        <select value={status} aria-label={t('web.sources.status_filter')} onChange={event => setStatus(event.currentTarget.value as StatusFilter)}>{Object.entries(statusLabels).map(([key, label]) => <option key={key} value={key}>{t(label)}</option>)}</select>
        <div className="next-sources-view" role="group" aria-label={t('web.sources.view')}>
          <button type="button" aria-pressed={view === 'cards'} aria-label={t('web.sources.cards')} title={t('web.sources.cards')} onClick={() => setView('cards')}><Icon name="grid" /></button>
          <button type="button" aria-pressed={view === 'list'} aria-label={t('web.sources.list')} title={t('web.sources.list')} onClick={() => setView('list')}><Icon name="menu" /></button>
        </div>
      </div>
    </div>
    <div className="next-sources-context"><span>{t('web.sources.count', { Count: visible.length })}</span><div className="next-sources-inline-actions">
      {hasCooldown && <button type="button" disabled={!workspace.allowed} onClick={() => workspace.requestAction({ type: 'release', engine: 'all' })}>{t('web.runtime_action.release_all')}</button>}
      {canRefreshQuota && <button type="button" disabled={!workspace.allowed} onClick={() => workspace.requestAction({ type: 'quota' })}>{t('web.runtime_action.refresh_quota')}</button>}
    </div></div>
    {!workspace.allowed && (hasCooldown || canRefreshQuota) && <p className="next-source-muted next-sources-permission">{workspace.reason}</p>}
    {(workspace.overview.error || quotas.error) && <div className="next-sources-load-error" role="alert">{workspace.overview.error && <span>{workspace.overview.error}</span>}{quotas.error && <span>{t('web.quota.heading')} · {quotas.error}</span>}
      <button type="button" onClick={() => { if (workspace.overview.error) void readOverview(); if (quotas.error) void readQuotas(); }}>{t('web.action.refresh_all_data_now')}</button></div>}
    {workspace.overview.loading && !sources.length ? <p className="next-sources-empty" role="status">{t('web.note.loading_2')}</p>
      : !visible.length ? <p className="next-sources-empty">{t(sources.length ? 'web.sources.no_matches' : 'web.note.no_sources_yet')}</p>
      : <div className="next-sources-results" data-view={view}>{visible.map(entry => <SourceEntry key={entry.id} source={entry} now={now} item={byProvider.get(entry.id)} selected={source?.id === entry.id} open={(id, target) => { opener.current = target ?? null; setSelected(id); }} />)}</div>}
    <SourceInspector source={source} now={now} item={source && byProvider.get(source.id)} close={close} restoreFocus={() => (opener.current?.isConnected ? opener.current : keyword.current)?.focus({ preventScroll: true })} />
  </section>;
}
