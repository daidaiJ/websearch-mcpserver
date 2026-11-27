import { useId, useState } from 'react';
import { CapabilityFields, count, select, toggle } from './CapabilityFields';
import { settingsEqual, useDraft, type SettingValue } from './draft';
import { t } from './i18n';
import { destinationHash, Icon } from './navigation';
import './capabilities.css';
import './settings-core.css';

const routeModes = ['hybrid', 'engine', 'apipool', 'mcphub'] as const;
const singleProviders = ['baidu', 'tavily', 'exa', 'anysearch', 'doubao', 'yuanbao'] as const;
const providers = { baidu: 'Baidu Qianfan', baidu_web: 'Baidu', tavily: 'Tavily', exa: 'Exa', anysearch: 'AnySearch',
  doubao: 'Doubao', yuanbao: 'Yuanbao', tinyfish: 'TinyFish', keenable: 'Keenable', mcphub: 'MCPHub', bing: 'Bing',
  duckduckgo: 'DuckDuckGo', google: 'Google', googlenews: 'Google News', so360: '360 Search', wikipedia: 'Wikipedia', github: 'GitHub' } as const;

function providerLabel(id: string) {
  if (id === 'baidu') return t('web.name.baidu_qianfan');
  if (id === 'doubao') return t('web.name.doubao_search');
  if (id === 'yuanbao') return t('web.name.yuanbao');
  return providers[id as keyof typeof providers] ?? id;
}

function toStrings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : [];
}

function OrderedProviders({ path, label, required = false }: { path: string; label: string; required?: boolean }) {
  const draft = useDraft();
  const reason = draft.fieldLockReason(path);
  const unavailable = !draft.canWrite || Boolean(reason) || draft.phase !== 'idle';
  const active = toStrings(draft.value(path));
  const choices = [...new Set([...active, ...(draft.settings?.options?.[path] ?? [])])];

  function move(index: number, offset: number) {
    const next = [...active];
    [next[index], next[index + offset]] = [next[index + offset], next[index]];
    draft.stage(path, next);
  }

  return <fieldset className="set-list-field">
    <legend>{label}</legend>
    {reason && <p className="cap-note">{reason}</p>}
    {required && <p className="cap-note">{t('web.settingsCore.provider_required')}</p>}
    <ul className="set-order-list">
      {choices.map(id => {
        const order = active.indexOf(id);
        const checked = order !== -1;
        const name = providerLabel(id);
        return <li key={id}>
          <label><input type="checkbox" checked={checked} disabled={unavailable || (required && checked && active.length === 1)}
            onChange={() => draft.stage(path, checked ? active.filter(item => item !== id) : [...active, id])} />{name}</label>
          {checked && <div className="set-order-actions">
            <button type="button" className="cap-button" disabled={unavailable || order <= 0} aria-label={`${t('web.config.move_up')} ${name}`} onClick={() => move(order, -1)}>
              <span aria-hidden="true" className="set-arrow-up"><Icon name="arrow" size={13} /></span>
            </button>
            <button type="button" className="cap-button" disabled={unavailable || order >= active.length - 1} aria-label={`${t('web.config.move_down')} ${name}`} onClick={() => move(order, 1)}>
              <span aria-hidden="true" className="set-arrow-down"><Icon name="arrow" size={13} /></span>
            </button>
          </div>}
        </li>;
      })}
    </ul>
  </fieldset>;
}

export function MapSetting({ path, label, parseEntry }: {
  path: string; label: string; parseEntry: (key: string, value: string) => number | string | undefined;
}) {
  const draft = useDraft();
  const id = useId();
  const reason = draft.fieldLockReason(path);
  const issue = draft.errors[path];
  const value = draft.value(path);
  const [buffer, setBuffer] = useState<{ value: SettingValue; text: string }>();
  const entries = value && typeof value === 'object' && !Array.isArray(value) ? Object.entries(value) : [];
  const text = issue?.rawValue ?? (buffer && settingsEqual(buffer.value, value) ? buffer.text
    : entries.map(([key, item]) => `${key} = ${item}`).join('\n'));

  // Keep textarea formatting while typing valid values, and reset when the staged map is discarded.
  return <MappingEditor key={path} label={label} reason={reason} id={id} disabled={!draft.canWrite || Boolean(reason) || draft.phase !== 'idle'}
    text={text} error={issue?.message} setValue={raw => {
      const staged = new Map<string, string | number>();
      for (const row of raw.split('\n').map(line => line.trim()).filter(Boolean)) {
        const match = /^([^=]+?)\s*=\s*(.+)$/.exec(row);
        const key = match?.[1].trim() ?? '';
        const parsed = match && key ? parseEntry(key, match[2].trim()) : undefined;
        if (parsed === undefined || staged.has(key) || staged.size >= 512) {
          draft.setFieldError(path, t('web.config.invalid_mapping'), raw);
          return;
        }
        staged.set(key, parsed);
      }
      const next = Object.fromEntries(staged);
      if (draft.stage(path, next)) setBuffer({ value: next, text: raw });
    }} />;
}

function MappingEditor({ label, id, reason, disabled, text, error, setValue }: {
  label: string; id: string; reason: string; disabled: boolean; text: string; error?: string; setValue: (value: string) => void;
}) {
  return <div className="cap-field set-map">
    <label htmlFor={id}>{label}</label>
    <textarea id={id} rows={4} disabled={disabled} value={text} spellCheck={false} aria-invalid={Boolean(error)}
      aria-describedby={`${id}-hint${error || reason ? ` ${id}-error` : ''}`} onChange={event => setValue(event.target.value)} />
    <small id={`${id}-hint`}>{t('web.config.key_value_help')}</small>
    {(error || reason) && <small id={`${id}-error`} className={error ? 'cap-error' : undefined} role={error ? 'alert' : undefined}>{error || reason}</small>}
  </div>;
}

function ModePolicies({ mode }: { mode: string | undefined }) {
  const draft = useDraft();
  if (mode === 'apipool') return <section aria-label="API Pool" className="set-policy">
    <CapabilityFields fieldHintsOnly fields={[select('apipool.strategy', t('web.config.strategy'), ['round-robin', 'priority', 'weighted'])]} />
    <OrderedProviders path="apipool.engines" label={t('web.config.pool_order')} required />
    <details className="set-advanced" open={draft.value('apipool.strategy') === 'weighted' ? true : undefined}>
      <summary>{t('web.config.pool_weights')}</summary>
      <MapSetting path="apipool.weights" label={t('web.config.pool_weights')} parseEntry={(key, raw) => {
        if (key !== 'baidu_web' && !draft.settings?.options?.['apipool.engines']?.includes(key)) return;
        if (/^\d+$/.test(raw) && Number(raw) <= 1000000) return Number(raw);
      }} />
    </details>
  </section>;

  if (mode === 'mcphub') return <section aria-label="MCPHub" className="set-policy">
    <CapabilityFields fieldHintsOnly fields={[
      select('mcp_hub.strategy', t('web.config.strategy'), ['priority', 'round-robin']),
      count('mcp_hub.timeout_sec', t('web.config.timeout'), 1, 3600),
      count('mcp_hub.fetch_timeout_sec', t('web.config.fetch_timeout'), 1, 3600),
      count('mcp_hub.cooldown_sec', t('web.config.cooldown'), 1, 86400),
    ]} />
    <div className="set-hub-orders">
      <OrderedProviders path="mcp_hub.rotation_engines" label={t('web.config.rotation_engines')} />
      <OrderedProviders path="mcp_hub.fallback_engines" label={t('web.config.fallback_engines')} />
    </div>
  </section>;

  if (mode === 'hybrid' || mode === 'engine') return <section className="set-policy">
    <CapabilityFields fieldHintsOnly fields={[
      count('smartsearch.engine_timeout_sec', t('web.config.engine_timeout'), -1, 3600),
      toggle('smartsearch.enhance', t('web.config.enhance')),
      select('smartsearch.off_topic_guard', t('web.config.off_topic_guard'), ['shadow', 'enforce', 'off']),
    ]} />
    <a className="set-inline-link" href={destinationHash({ page: 'sources' })}>{t('web.settingsCore.configure_sources')} <Icon name="arrow" size={14} /></a>
  </section>;
  return null;
}

export default function SettingsRoute({ refresh: _refresh }: { refresh: number }) {
  const draft = useDraft();
  const mode = draft.value('mode');
  const current = typeof mode === 'string' ? mode : undefined;
  const modeReason = draft.fieldLockReason('mode');
  const locked = !draft.canWrite || Boolean(modeReason) || draft.phase !== 'idle';
  const single = singleProviders.find(provider => provider === current);
  const descriptions = {
    hybrid: t('web.mode.enabled_api_and_web_engines_run_concurrently_when_their_cond'),
    engine: t('web.mode.enabled_web_engines_run_concurrently_with_no_key_required_pr'),
    apipool: t('web.mode.one_provider_per_call_with_failover_supports_round_robin_pri'),
    mcphub: t('web.mode.mcphub_description'),
    provider: t('web.mode.prefers_the_selected_provider_while_keeping_its_original_fal'),
  };
  const providerNotes = {
    baidu: t('web.mode.baidu_qianfan_search_on_failure_it_falls_back_to_the_enabled'),
    tavily: t('web.mode.tavily_search_api_requires_tavily_sk_without_a_key_it_falls_'),
    exa: t('web.mode.exa_web_search_api_requires_exa_api_key_without_a_key_it_fal'),
    anysearch: t('web.mode.anysearch_api_with_keys_it_rotates_the_key_pool_without_keys'),
    doubao: t('web.mode.doubao_web_search_global_custom_requires_doubao_search_api_k'),
    yuanbao: t('web.mode.tencent_cloud_yuanbao_search_api_key_mode_requires_tencentcl'),
  };

  return <div className="set-core">
    <section className="cap-panel" aria-labelledby="set-route-mode">
      <header className="cap-panel-head"><h2 id="set-route-mode">{t('web.page.search_routing')}</h2><p>{t('web.settingsCore.route_help')}</p></header>
      {modeReason && <p className="cap-note cap-permission">{modeReason}</p>}
      <fieldset className="set-mode-field" disabled={locked}>
        <legend>{t('web.label.search_mode')}</legend>
        <div className="set-mode-grid">
          {([...routeModes, 'provider'] as const).map(choice => <label className="set-mode-card" key={choice}>
            <input type="radio" name="search-mode" aria-labelledby={`set-mode-${choice}`} aria-describedby={`set-desc-${choice}`}
              checked={choice === 'provider' ? Boolean(single) : current === choice}
              onChange={() => draft.stage('mode', choice === 'provider' ? single ?? singleProviders[0] : choice)} />
            <span><strong id={`set-mode-${choice}`}>{choice === 'provider' ? t('web.status.single_provider') : choice}</strong>
              <small id={`set-desc-${choice}`}>{descriptions[choice]}</small></span>
          </label>)}
        </div>
      </fieldset>
      {single && <div className="cap-field set-single-provider"><label htmlFor="set-single-select">{t('web.label.provider')}</label>
        <select id="set-single-select" value={single} disabled={locked} aria-describedby="set-provider-note" onChange={event => draft.stage('mode', event.target.value)}>
          {singleProviders.map(provider => <option key={provider} value={provider}>{providerLabel(provider)}</option>)}
        </select><small id="set-provider-note">{providerNotes[single]}</small>
      </div>}
      {draft.settings && <ModePolicies mode={current} />}
    </section>
    {draft.settings && <section className="cap-panel" aria-labelledby="set-route-defaults">
      <header className="cap-panel-head"><h2 id="set-route-defaults">{t('web.settingsCore.route_defaults')}</h2></header>
      <CapabilityFields fieldHintsOnly fields={[
        count('upstream_timeout_sec', t('web.label.upstream_timeout_seconds'), 0, 360),
        toggle('cache.enabled', t('web.label.enable_cache')),
        count('cache.cleanup_interval', t('web.settingsCore.cache_cleanup'), 0, 360),
        select('network', t('web.settingsCore.network'), { china: t('web.label.china_network'), international: t('web.label.international_network') }),
      ]} />
    </section>}
  </div>;
}
