import { useId, useState } from 'react';
import { useDraft } from './draft';
import { t } from './i18n';
import { Icon } from './navigation';

type BaseField = { key: string; label: string; help?: string };
export type Field = BaseField & (
  | { kind: 'boolean' | 'hosts' }
  | { kind: 'number'; min: number; max: number; integer: boolean }
  | { kind: 'select'; options: Record<string, string> }
  | { kind: 'text'; maxLength: number; check?: (value: string) => boolean; issue?: string; sensitiveEndpoint?: boolean }
);

export const toggle = (key: string, label: string): Field => ({ key, label, kind: 'boolean' });
export const count = (key: string, label: string, min: number, max: number): Field => ({ key, label, kind: 'number', min, max, integer: true });
const ratio = (key: string, label: string, min = 0): Field => ({ key, label, kind: 'number', min, max: 1, integer: false });
export const select = (key: string, label: string, options: string[] | Record<string, string>): Field => ({
  key, label, kind: 'select', options: Array.isArray(options) ? Object.fromEntries(options.map(value => [value, value])) : options,
});

export const MODULE_IDS = ['smartsearch', 'cleanfetch', 'pdf_parser', 'academicsearch', 'search_history', 'llm', 'file_search'] as const;
export type ModuleId = typeof MODULE_IDS[number];

export function moduleNames(): Record<ModuleId, string> {
  return { smartsearch: 'SmartSearch', cleanfetch: 'CleanFetch', pdf_parser: 'PDF / MinerU', academicsearch: t('web.capability.academic'),
    search_history: t('web.capability.history'), llm: t('web.capability.llm'), file_search: 'Everything' };
}

type ModuleFields = { description: string; basic: Field[]; advanced: Field[] };

export function moduleFields(id: ModuleId): ModuleFields {
  switch (id) {
    case 'smartsearch': return {
      description: t('web.capability.smartsearch_help'), basic: [
        toggle('smartsearch.enhance', t('web.config.enhance')),
        count('smartsearch.max_size', t('web.label.max_results'), 0, 360),
        { key: 'black_list_host', label: t('web.config.blocked_hosts'), kind: 'hosts' },
      ], advanced: [
        ratio('smartsearch.relevance_threshold', t('web.label.relevance_threshold')),
        toggle('smartsearch.mmr.enabled', t('web.config.mmr_enabled')),
        ratio('smartsearch.mmr.lambda', t('web.config.mmr_lambda')),
        count('smartsearch.mmr.target_count', t('web.config.mmr_target'), 0, 360),
        select('smartsearch.off_topic_guard', t('web.config.off_topic_guard'), ['shadow', 'enforce', 'off']),
        toggle('smartsearch.show_meta', t('web.config.show_meta')),
        count('smartsearch.fetch_top_n', t('web.config.fetch_top_n'), 0, 5),
        count('smartsearch.engine_timeout_sec', t('web.config.engine_timeout'), -1, 3600),
        count('smartsearch.inline_max_chars', t('web.config.inline_max_chars'), -1, 10000000),
      ],
    };
    case 'cleanfetch': return {
      description: t('web.capability.cleanfetch_help'), basic: [
        toggle('cleanfetch.enabled', t('web.label.url_fetching')),
        select('mcp_hub.fetch_mode', t('web.config.fetch_mode'), ['off', 'fallback', 'prefer']),
        count('cleanfetch.timeout_sec', t('web.config.timeout'), 1, 3600),
      ], advanced: [
        toggle('cleanfetch.auto_proxy', t('web.label.fetch_through_the_system_proxy')),
        count('mcp_hub.fetch_timeout_sec', t('web.config.fetch_timeout'), 1, 3600),
        count('cleanfetch.max_fetch_size_mb', t('web.config.max_fetch_mb'), 1, 2048),
        count('cleanfetch.max_inline_lines', t('web.config.max_inline_lines'), 1, 100000),
        count('cleanfetch.max_inline_chars', t('web.config.inline_max_chars'), 0, 10000000),
        count('cleanfetch.file_ttl_hours', t('web.config.file_ttl'), 1, 8760),
        count('cleanfetch.max_retries', t('web.config.retries'), 1, 10),
      ],
    };
    case 'pdf_parser': return {
      description: t('web.capability.pdf_help'), basic: [
        toggle('pdf_parser.enabled', t('web.name.pdf_parsing')),
        select('pdf_parser.mineru_model', t('web.config.mineru_model'), ['pipeline', 'vlm']),
        toggle('pdf_parser.mineru_ocr', t('web.config.mineru_ocr')),
      ], advanced: [
        toggle('pdf_parser.mineru_formula', t('web.config.mineru_formula')),
        toggle('pdf_parser.mineru_table', t('web.config.mineru_table')),
        { key: 'pdf_parser.mineru_lang', label: t('web.config.mineru_language'), kind: 'text', maxLength: 64, check: value => /^[a-z0-9_]+$/.test(value) },
        toggle('pdf_parser.mineru_remote_pdf', t('web.config.mineru_remote')),
        count('pdf_parser.max_pages', t('web.label.default_pages'), 1, 10000),
        count('pdf_parser.mineru_page_limit', t('web.config.mineru_page_limit'), 1, 10000),
        { ...count('pdf_parser.mineru_page_batch_size', t('web.config.mineru_batch_size'), 0, 200), help: t('web.config.invalid_page_batch') },
        count('pdf_parser.mineru_page_budget', t('web.config.mineru_page_budget'), 0, 100000),
      ],
    };
    case 'academicsearch': return {
      description: t('web.capability.academic_help'), basic: [
        toggle('academic.enabled', t('web.name.academic_search')),
        toggle('academic.enhance', t('web.config.enhance')),
      ], advanced: [
        ratio('academic.threshold', t('web.label.relevance_threshold'), Number.MIN_VALUE),
        { ...select('network', t('web.label.academic_search_network'), { china: t('web.label.china_network'), international: t('web.label.international_network') }), help: t('web.capNext.network_scope') },
      ],
    };
    case 'search_history': return {
      description: t('web.capability.history_help'), basic: [
        toggle('search_history.enabled', t('web.config.history_enabled')),
        count('search_history.retention_days', t('web.config.retention'), 1, 36500),
      ], advanced: [],
    };
    case 'llm': return {
      description: t('web.capability.llm_help'), basic: [
        { key: 'llm.model_id', label: t('web.config.model'), kind: 'text', maxLength: 256, check: value => value === value.trim() && !/[\p{Cc}]/u.test(value) },
        { key: 'llm.base_url', label: t('web.config.base_url'), kind: 'text', maxLength: 2048, check: validEndpoint, issue: t('web.capNext.endpoint_format'), sensitiveEndpoint: true },
      ], advanced: [
        count('llm.timeout_sec', t('web.config.timeout'), 1, 3600),
        count('llm.summary_progress_interval', t('web.config.progress_interval'), 1, 3600),
      ],
    };
    case 'file_search': return {
      description: t('web.capability.everything_help'), basic: [
        count('everything.max_results', t('web.label.max_results'), 1, 20),
      ], advanced: [
        count('everything.timeout_sec', t('web.config.timeout'), 1, 3600),
        ratio('everything.min_alignment', t('web.config.min_alignment')),
      ],
    };
  }
}

function validEndpoint(value: string) {
  if (!value) return true;
  if (/[?#\s\\]|\p{Cc}/u.test(value)) return false;
  try {
    const url = new URL(value);
    return /^https?:\/\//.test(value) && !!url.hostname && !url.username && !url.password;
  } catch { return false; }
}

function normalizeHost(token: string): string | undefined {
  const raw = token.toLowerCase().replace(/\.$/, '');
  const domain = /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
  if (raw.length <= 253 && domain.test(raw)) return raw;
  if (!raw || /[/\\?#[\]@%*]|\p{Cc}/u.test(raw)) return undefined;
  try {
    const ipv6 = raw.includes(':');
    const hostname = new URL(`http://${ipv6 ? `[${raw}]` : raw}`).hostname;
    if (ipv6) return hostname.slice(1, -1);
    if (hostname.length <= 253 && domain.test(hostname)) return hostname;
  } catch { /* Validation message comes from the form. */ }
}

const separators = /[\s,，]+/u;

function BlockedHosts({ field, fieldHintsOnly }: { field: Field; fieldHintsOnly: boolean }) {
  const draft = useDraft();
  const id = useId();
  const [input, setInput] = useState<string | null>(null);
  const fieldValue = draft.value(field.key);
  const hosts = Array.isArray(fieldValue) ? fieldValue.filter((entry): entry is string => typeof entry === 'string') : [];
  const issue = draft.errors[field.key];
  const reason = fieldHintsOnly ? draft.fieldLockReason(field.key) : draft.fieldReason(field.key);
  const disabled = !draft.canWrite || Boolean(reason) || draft.phase !== 'idle';
  const raw = input ?? issue?.rawValue ?? '';

  function add(text: string) {
    const candidates = text.trim().split(separators).filter(Boolean).map(normalizeHost);
    const merged = new Map(hosts.map(host => [normalizeHost(host) ?? host, host]));
    for (const host of candidates) if (host && !merged.has(host)) merged.set(host, host);
    if (candidates.some(host => host === undefined) || merged.size > 512) {
      draft.setFieldError(field.key, merged.size > 512 ? t('web.capNext.host_limit') : t('web.config.invalid_domain'), text);
      setInput(null);
      return;
    }
    if (candidates.length) draft.stage(field.key, [...merged.values()]);
    else draft.setFieldError(field.key, '');
    setInput(null);
  }

  return <div className="cap-field cap-hosts">
    <label htmlFor={id}>{field.label}</label>
    {hosts.length > 0 && <ul className="cap-host-list">
      {hosts.map(host => <li key={host}>
        <code>{host}</code><button type="button" disabled={disabled} aria-label={t('web.capNext.remove_host', { Host: host })} onClick={() => {
          draft.stage(field.key, hosts.filter(candidate => candidate !== host));
          if (issue) draft.setFieldError(field.key, issue.message, issue.rawValue);
          document.getElementById(id)?.focus();
        }}><Icon name="close" size={13} /></button>
      </li>)}
    </ul>}
    <div className="cap-inline-input">
      <input id={id} type="text" inputMode="text" value={raw} disabled={disabled} maxLength={4096} spellCheck={false} autoComplete="off" placeholder="example.com"
        aria-invalid={Boolean(issue)} aria-describedby={`${id}-help ${id}-issue`}
        onChange={event => {
          setInput(event.target.value || null);
          if (issue && !event.target.value) draft.setFieldError(field.key, '');
        }}
        onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); add(raw); } }}
        onPaste={event => {
          const pasted = event.clipboardData.getData('text');
          if (!separators.test(pasted.trim())) return;
          event.preventDefault();
          const { selectionStart, selectionEnd } = event.currentTarget;
          add(raw.slice(0, selectionStart ?? 0) + pasted + raw.slice(selectionEnd ?? raw.length));
        }} />
      <button className="cap-button" type="button" disabled={disabled || !raw.trim()} onClick={() => add(raw)}>{t('web.capNext.add_host')}</button>
    </div>
    <small id={`${id}-help`}>{t('web.capNext.host_help')}</small>
    <small id={`${id}-issue`} className={issue ? 'cap-error' : undefined} role={issue ? 'alert' : undefined}>{issue?.message || reason}</small>
  </div>;
}

function CapabilityField({ field, fieldHintsOnly }: { field: Field; fieldHintsOnly: boolean }) {
  const store = useDraft();
  const id = useId();
  const reason = fieldHintsOnly ? store.fieldLockReason(field.key) : store.fieldReason(field.key);
  const issue = store.errors[field.key];
  const value = store.value(field.key);
  const disabled = !store.canWrite || Boolean(reason) || store.phase !== 'idle';
  const note = issue?.message || reason || field.help;
  const messageId = `${id}-issue`;
  if (field.kind === 'hosts') return <BlockedHosts field={field} fieldHintsOnly={fieldHintsOnly} />;

  function edit(raw: string) {
    if (field.kind === 'number') {
      const val = Number(raw);
      const batchTooSmall = field.key === 'pdf_parser.mineru_page_batch_size' && val > 0 && val < 10;
      if (!raw.trim() || !Number.isFinite(val) || (field.integer && !Number.isInteger(val)) || val < field.min || val > field.max || batchTooSmall) {
        store.setFieldError(field.key, batchTooSmall ? t('web.config.invalid_page_batch') : t('web.draft.invalid_value'), raw);
      } else store.stage(field.key, val);
    } else if (field.kind === 'text' && field.check && !field.check(raw)) {
      const mayContainCredentials = field.sensitiveEndpoint && /[@?#]/.test(raw);
      store.setFieldError(field.key, field.issue || t('web.draft.invalid_value'), mayContainCredentials ? undefined : raw);
    } else store.stage(field.key, raw);
  }

  const textValue = issue?.rawValue ?? (typeof value === 'string' || typeof value === 'number' ? String(value) : '');
  return <div className="cap-field">
    <label htmlFor={id}>{field.label}</label>
    {field.kind === 'boolean' ? <div className="cap-switch-line">
      <button id={id} className="cap-switch" type="button" role="switch" aria-checked={value === true} disabled={disabled}
        aria-describedby={note ? messageId : undefined} onClick={() => store.stage(field.key, value !== true)} />
      <span aria-hidden="true">{value === true ? t('web.label.on') : value === false ? t('web.label.off') : '—'}</span>
    </div> : field.kind === 'select' ? <select id={id} disabled={disabled} value={textValue} aria-describedby={note ? messageId : undefined} onChange={event => edit(event.target.value)}>
      {!Object.hasOwn(field.options, textValue) && <option value={textValue}>{textValue || '—'}</option>}
      {Object.entries(field.options).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
    </select> : <input id={id} type="text" spellCheck={false} autoComplete="off" disabled={disabled}
      inputMode={field.kind === 'number' ? (field.integer ? 'numeric' : 'decimal') : undefined}
      maxLength={field.kind === 'text' ? field.maxLength : undefined} value={textValue} onChange={event => edit(event.target.value)}
      aria-invalid={Boolean(issue)} aria-describedby={note ? messageId : undefined} />}
    {note && <small id={messageId} role={issue ? 'alert' : undefined} className={issue ? 'cap-error' : undefined}>{note}</small>}
  </div>;
}

export function CapabilityFields({ fields, fieldHintsOnly = false }: { fields: Field[]; fieldHintsOnly?: boolean }) {
  return <div className="cap-field-grid">{fields.map(field => <CapabilityField key={field.key} field={field} fieldHintsOnly={fieldHintsOnly} />)}</div>;
}
