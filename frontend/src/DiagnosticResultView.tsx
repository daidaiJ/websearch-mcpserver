import type { DiagnosticResult } from './CapabilityData';
import { getLocale, t } from './i18n';

const serviceNames: Readonly<Record<string, string>> = {
  mcphub: 'MCPHub', tinyfish: 'TinyFish', keenable: 'Keenable', jina: 'Jina Reader', webfetch: 'WebFetch',
};
export const serviceName = (id: string) => serviceNames[id] ?? id;

function errorLabel(kind: string) {
  const names: Record<string, string> = {
    unconfigured: t('web.diagnostics.unconfigured'), invalid_endpoint: t('web.diagnostics.invalid_endpoint'),
    blocked: t('web.diagnostics.blocked'), challenge: t('web.diagnostics.challenge'), access_denied: t('web.status.access_denied'),
    busy: t('web.capNext.diagnostic_busy'), captcha: t('web.status.captcha'), rate_limit: t('web.status.rate_limited'),
    timeout: t('web.status.timeout'), parse: t('web.status.parse_failed'), no_result: t('web.status.no_results'),
    network: t('web.status.network_server'), unknown: t('web.status.unknown_error'),
  };
  return names[kind] ?? kind;
}

export function DiagnosticResultView({ result, connection = false }: { result: DiagnosticResult; connection?: boolean }) {
  const num = new Intl.NumberFormat(getLocale());
  const links = (result.links ?? []).filter(link => /^https:\/\/pubmed\.ncbi\.nlm\.nih\.gov\/[1-9][0-9]*\/$/.test(link.url)).slice(0, 3);
  const fetch = result.fetch;
  return <div className="cap-result" role="status">
    <div className="cap-result-header">
      <span className="cap-badge" data-tone={result.ok ? 'ok' : 'danger'}>{result.ok
        ? (connection ? t('web.capNext.reachable') : t('web.status.success'))
        : (connection ? t('web.capNext.unreachable') : t('web.status.failure'))}</span>
      {Number.isFinite(result.elapsed_ms) && <span>{t('web.capNext.elapsed_ms', { Time: num.format(result.elapsed_ms) })}</span>}
      {typeof result.result_count === 'number' && <span>{t('web.diagnostics.result_count', { count: num.format(result.result_count) })}</span>}
    </div>
    {result.endpoint && <code>{result.endpoint}</code>}
    {result.error_kind && <p className="cap-error">{errorLabel(result.error_kind)}{result.error_detail && <> · {result.error_detail}</>}</p>}
    {!!result.titles?.length && <ol className="cap-result-titles">{result.titles.slice(0, 3).map((title, index) => <li key={`${title}-${index}`}>{title}</li>)}</ol>}
    {links.length > 0 && <>
      <ol className="cap-result-titles">{links.map(link => <li key={link.url}><a href={link.url} target="_blank" rel="noopener noreferrer">{link.title}</a></li>)}</ol>
      <p className="cap-note">{t('web.capNext.pubmed_links')}</p>
    </>}
    {fetch && <>
      {!!fetch.attempts?.length && <ol className="cap-fetch-steps" aria-label={t('web.capNext.nodes_tried')}>
        {fetch.attempts.map((node, index) => <li key={`${node}-${index}`} aria-current={node === fetch.node ? 'step' : undefined}>
          <span className="cap-step-index" aria-hidden="true">{index + 1}</span>{serviceName(node)}
        </li>)}
      </ol>}
      {fetch.node && <p>{t('web.diagnostics.fetch_node', { node: serviceName(fetch.node) })}</p>}
      <p>{fetch.body_status === 'available' ? t('web.diagnostics.fetch_chars', { count: num.format(fetch.characters) })
        : <>{t('web.diagnostics.no_body')} · {t('web.diagnostics.fetch_chars', { count: num.format(fetch.characters) })}</>}</p>
      {fetch.remote_redirects && <p className="cap-note">{t('web.diagnostics.remote_redirects')}</p>}
    </>}
  </div>;
}
