'use strict';
/* WebSearch Control Center - dependency-free frontend. */

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

/* ---------- i18n ---------- */

// 文案 key 与词典住在 pkg/i18n/locales/web.<语言>.json，由 serveIndex 按本次请求
// 协商出的语言内联注入 window.__I18N__；语言同时写在 <html lang> 上。
// 静态文案用 data-i18n / data-i18n-<attr> 标记，动态文案统一走 t(key, data)。
const I18N_LANG = (typeof document !== 'undefined' && document.documentElement && document.documentElement.lang || '').toLowerCase().indexOf('en') === 0 ? 'en' : 'zh';
const I18N = (typeof window !== 'undefined' && window.__I18N__) || {};

// t(key, data)：data 的键对应文案里的 {{.Name}}；含 Count 时按 CLDR 复数形态取值
// （与 Go 侧 go-i18n 同口径）。词典缺条目时返回 key 本身，漏登记由目录审计拦截。
function t(key, data) {
  const value = I18N[key];
  if (value === undefined || value === null) return key;
  let text = value;
  if (typeof value === 'object') {
    const form = pluralForm(data && data.Count);
    text = value[form] !== undefined ? value[form] : (value.other !== undefined ? value.other : key);
  }
  if (!data) return text;
  return String(text).replace(/\{\{\s*\.(\w+)\s*\}\}/g, (m, name) => {
    const v = data[name];
    return v === undefined || v === null ? '' : String(v);
  });
}

function pluralForm(count) {
  if (typeof Intl === 'undefined' || !Intl.PluralRules) return 'other';
  try {
    return new Intl.PluralRules(I18N_LANG === 'en' ? 'en-US' : 'zh-CN').select(Number(count) || 0);
  } catch (e) {
    return 'other';
  }
}

// firstTextNode 返回元素的首个非空文本子节点（data-i18n 只替换这一个，保留兄弟节点）。
function firstTextNode(el) {
  for (let i = 0; i < el.childNodes.length; i++) {
    const node = el.childNodes[i];
    if (node.nodeType === 3 && String(node.nodeValue).trim()) return node;
  }
  return null;
}

// applyI18n 把标记过的静态 DOM 文案换成当前语言；zh 模式词典本身就是中文，照常渲染。
function applyI18n(root) {
  if (!root || typeof root.querySelectorAll !== 'function') return;
  root.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.getAttribute('data-i18n');
    const node = key ? firstTextNode(el) : null;
    if (node) node.nodeValue = t(key);
  });
  ['title', 'aria-label', 'placeholder'].forEach(attr => {
    root.querySelectorAll('[data-i18n-' + attr + ']').forEach(el => el.setAttribute(attr, t(el.getAttribute('data-i18n-' + attr))));
  });
}

const REFRESH_MS = 60000;
const TZ = { hour12: false, timeZone: 'Asia/Shanghai' };
const PAGES = { overview: t('web.page.overview'), sources: t('web.page.search_sources'), usage: t('web.page.call_log'), settings: t('web.page.settings') };
const HEALTH = { healthy: t('web.status.healthy'), degraded: t('web.status.degraded'), down: t('web.status.failing'), unknown: t('web.status.unknown') };
const ERROR_KINDS = {
  rate_limit: t('web.status.rate_limited'), captcha: t('web.status.captcha'), access_denied: t('web.status.access_denied'), timeout: t('web.status.timeout'),
  parse: t('web.status.parse_failed'), no_result: t('web.status.no_results'), network: t('web.status.network_server'), unknown: t('web.status.unknown_error'),
};
const SYSTEM = { healthy: t('web.status.running_normally'), degraded: t('web.status.partially_degraded'), down: t('web.status.partially_failing'), waiting: t('web.status.awaiting_real_calls') };
const DOT = { healthy: 'healthy', degraded: 'degraded', down: 'down', unknown: 'unknown' };
const DISPLAY = {
  smartsearch: t('web.name.smart_search'), academicsearch: t('web.name.academic_search'), cleanfetch: t('web.name.content_fetch'), pdf_parser: t('web.name.pdf_parsing'), file_search: t('web.name.local_file_search'),
  anysearch: 'AnySearch', baidu: t('web.name.baidu_qianfan'), tavily: 'Tavily', exa: 'Exa', doubao: t('web.name.doubao_search'),
  baidu_web: t('web.name.baidu_web'), bing: 'Bing', google: 'Google', duckduckgo: 'DuckDuckGo',
  arxiv: 'arXiv', crossref: 'Crossref', openalex: 'OpenAlex', pubmed: 'PubMed', europepmc: 'Europe PMC',
  dblp: 'DBLP', doaj: 'DOAJ', semantic_scholar: 'Semantic Scholar', google_scholar: 'Google Scholar',
  webfetch: t('web.name.web_fetcher'), jina: 'Jina Reader', pdf_pipeline: t('web.name.pdf_pipeline'),
};
const SECRETS = {
  BAIDU_SK: t('web.name.baidu_qianfan'), TAVILY_SK: 'Tavily', EXA_API_KEY: 'Exa', ANYSEARCH_API_KEY: 'AnySearch',
  DOUBAO_SEARCH_API_KEY: t('web.name.doubao'), TENCENTCLOUD_WSA_APIKEY: t('web.name.yuanbao'), JINA_API_KEY: 'Jina Reader', MINERU_TOKEN: 'MinerU',
};
// 各供应商的官方「获取 Key」入口，配置页一键直达
const SECRET_LINKS = {
  BAIDU_SK: 'https://console.bce.baidu.com/iam/#/iam/apikey/list',
  TAVILY_SK: 'https://app.tavily.com/home',
  EXA_API_KEY: 'https://dashboard.exa.ai/api-keys',
  ANYSEARCH_API_KEY: 'https://www.anysearch.com/console/api-keys',
  DOUBAO_SEARCH_API_KEY: 'https://console.volcengine.com/search-infinity/api-key',
  TENCENTCLOUD_WSA_APIKEY: 'https://console.cloud.tencent.com/wsapi/index',
  JINA_API_KEY: 'https://jina.ai/api-keys',
  MINERU_TOKEN: 'https://mineru.net/apiManage/token',
};

const state = {
  overview: null, overviewError: null, overviewStale: false,
  quotas: null, quotaError: null,
  clients: { rows: null, error: null, tab: null, dimension: 'application', application: '', days: 7, windowDays: 7, loading: false, request: 0 },
  settings: null, settingsError: null, settingsDirty: false, settingsLoaded: false,
  draft: {}, draftBefore: {}, draftRevision: '', saving: false, applying: false, applyResult: null, adminPass: '', adminUser: '',
  events: { rows: null, error: null, loading: false, exporting: false, expanded: new Set(), details: new Map() },
  filters: { status: '', errorKind: '', tool: '', provider: '' },
  lastLoad: 0,
};

/* ---------- helpers ---------- */

const ESCAPES = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
function esc(value) {
  if (value === null || value === undefined) return '';
  return String(value).replace(/[&<>"']/g, ch => ESCAPES[ch]);
}
function num(value) {
  return typeof value === 'number' && isFinite(value) ? value : null;
}
function fmtInt(value) {
  const n = num(value);
  return n === null ? '—' : new Intl.NumberFormat('zh-CN').format(n);
}
function fmtMS(value) {
  const n = num(value);
  return n === null ? '—' : n + ' ms';
}
function fmtPct(rate, sample) {
  const n = num(rate);
  return n === null || !sample ? '—' : Math.round(n * 100) + '%';
}
function fmtAt(value) {
  if (!value) return '—';
  const d = new Date(value);
  return isNaN(d.getTime()) ? '—' : d.toLocaleString('zh-CN', TZ);
}
function fmtClock() {
  return new Date().toLocaleTimeString('zh-CN', TZ);
}
function displayName(id) {
  return DISPLAY[id] || id || '';
}
function errorKindLabel(kind) {
  return ERROR_KINDS[kind] || kind || '';
}
function errorKindChip(kind) {
  if (!kind) return '<span class="dim">—</span>';
  return `<span class="kind-chip">${esc(errorKindLabel(kind))}</span>`;
}
function formatErrorKinds(list) {
  if (!Array.isArray(list) || !list.length) return '<span class="dim">—</span>';
  return list.slice(0, 3).map(item =>
    `<span class="kind-chip bad">${esc(errorKindLabel(item.kind))} ×${fmtInt(item.count)}</span>`
  ).join(' ');
}
function dot(status) {
  return `<i class="dot ${DOT[status] || 'unknown'}"></i>`;
}
function setText(sel, text) {
  const el = $(sel);
  if (el) el.textContent = text;
}
function setNote(el, text, cls) {
  if (!el) return;
  el.textContent = text || '';
  el.className = 'panel-note' + (cls ? ' ' + cls : '');
}
function emptyRow(cols, text) {
  return `<tr><td class="empty" colspan="${cols}">${esc(text)}</td></tr>`;
}
// Lucide icons, ISC license: https://github.com/lucide-icons/lucide
const ICONS = {
  'chevron-right': '<path d="m9 18 6-6-6-6"/>',
  'refresh-cw': '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
  download: '<path d="M12 15V3"/><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/>',
  'undo-2': '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5a5.5 5.5 0 0 1-5.5 5.5H11"/>',
  'rotate-ccw': '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>',
  'key-round': '<path d="M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z"/><circle cx="16.5" cy="7.5" r=".5" fill="currentColor"/>',
  pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/>',
  save: '<path d="M15.2 3a2 2 0 0 1 1.4.6l3.8 3.8a2 2 0 0 1 .6 1.4V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z"/><path d="M17 21v-7a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v7"/><path d="M7 3v4a1 1 0 0 0 1 1h7"/>',
};
function icon(name) {
  return `<svg class="control-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name] || ''}</svg>`;
}
function healthOf(s) {
  if (!s.last_seen_at) return { dot: 'unknown', label: t('web.status.no_calls_yet') };
  const status = s.observation_status || s.status;
  return { dot: DOT[status] || 'unknown', label: HEALTH[status] || t('web.status.unknown') };
}

/* ---------- transport ---------- */

async function fetchJSON(path, options = {}) {
  const headers = Object.assign({ Accept: 'application/json' }, options.headers || {});
  const res = await fetch(path, Object.assign({ cache: 'no-store' }, options, { headers }));
  let body = null;
  try { body = await res.json(); } catch (e) { body = null; }
  if (!res.ok) {
    const msg = body && body.error ? body.error : 'HTTP ' + res.status;
    throw new Error(msg);
  }
  if (body === null) throw new Error(t('web.msg.response_is_not_valid_json'));
  return body;
}

/* ---------- loaders (each panel fails on its own) ---------- */

async function loadOverview() {
  try {
    const data = await fetchJSON('/__admin/api/overview');
    state.overview = data;
    // The source catalog is the schema this page is built around; older
    // payloads are shown as-is but flagged so they are never read as current.
    state.overviewStale = !(data && data.system && Array.isArray(data.sources));
    state.overviewError = null;
    if (data && data.brand) applyBrand(data.brand);
  } catch (e) {
    state.overviewError = e.message;
  }
  renderSystem();
  renderToolKpis();
  renderMCPTools();
  renderWebSources();
  renderSourcePages();
  renderUsageSourceOptions();
  renderConfig();
  loadClients();
}

async function loadClients() {
  const request = ++state.clients.request;
  state.clients.loading = true;
  state.clients.error = null;
  renderClientsPanel();
  try {
    const data = await fetchJSON('/__admin/api/clients?days=' + state.clients.days);
    if (request !== state.clients.request) return;
    state.clients.rows = Array.isArray(data.usage) ? data.usage : [];
    state.clients.windowDays = data.window_days;
  } catch (e) {
    if (request !== state.clients.request) return;
    state.clients.error = e.message;
  }
  state.clients.loading = false;
  renderClientsPanel();
}

async function loadQuotas() {
  try {
    const data = await fetchJSON('/__admin/api/quotas');
    state.quotas = Array.isArray(data) ? data : [];
    state.quotaError = null;
  } catch (e) {
    state.quotaError = e.message;
  }
  renderTavily();
  renderWebSources();
  renderSourcePages();
  renderQuotaManage();
}

async function loadSettings() {
  try {
    const data = await fetchJSON('/__admin/api/settings', { signal: AbortSignal.timeout(5000) });
    if (!data || !data.values) throw new Error(t('web.msg.settings_response_is_missing_the_values_field'));
    state.settings = data;
    state.settingsError = null;
    const pending = data.pending_changes || {};
    if (Object.keys(pending).length && !state.applying && !state.saving && (!state.applyResult || !state.applyResult.failed)) {
      state.applyResult = {
        targets: { ...pending }, backup: (data.backups || [])[0]?.name,
        failed: true, status: t('web.msg.config_saved_not_yet_confirmed'),
      };
    }
  } catch (e) {
    state.settingsError = e.message;
  }
  renderConfig();
  renderSettingsForm();
  renderSourcePages();
  renderWriteAccess();
  renderApplyResult();
}

async function loadEvents() {
  const requestId = (state.events.requestId || 0) + 1;
  state.events.requestId = requestId;
  const f = state.filters;
  const params = new URLSearchParams();
  params.set('limit', '100');
  if (f.status) params.set('status', f.status);
  if (f.errorKind) params.set('error_kind', f.errorKind);
  if (f.tool) params.set('tool', f.tool);
  if (f.provider) params.set('provider', f.provider);
  state.events.loading = true;
  state.events.error = null;
  renderUsage();
  try {
    const rows = await fetchJSON('/__admin/api/calls?' + params.toString(), { signal: AbortSignal.timeout(10000) });
    if (state.events.requestId !== requestId) return;
    state.events.rows = Array.isArray(rows) ? rows : [];
    state.events.error = null;
    const ids = new Set(state.events.rows.map(e => String(e.id)));
    state.events.expanded.forEach(id => { if (!ids.has(id)) state.events.expanded.delete(id); });
    const requests = new Set(state.events.rows.map(e => e.request_id).filter(Boolean));
    state.events.details.forEach((detail, id) => { if (!requests.has(id) && !detail.loading) state.events.details.delete(id); });
  } catch (e) {
    if (state.events.requestId !== requestId) return;
    state.events.error = e.message;
  }
  state.events.loading = false;
  renderUsage();
  renderUsageSourceOptions();
  for (const e of state.events.rows || []) {
    if (state.events.expanded.has(String(e.id)) && e.request_id) loadRequestEvents(e.request_id, true).catch(() => {});
  }
}

async function loadAll() {
  await Promise.allSettled([loadOverview(), loadQuotas(), loadSettings(), loadEvents()]);
  state.lastLoad = Date.now();
  renderUpdated();
  refreshHScroll();
}

function anyFailure() {
  return Boolean(state.overviewError || state.quotaError || state.settingsError || state.events.error || state.overviewStale);
}

function renderUpdated() {
  const el = $('#updated');
  if (!el) return;
  el.textContent = `${t('web.note.updated')} ` + fmtClock() + (anyFailure() ? ` ${t('web.note.some_data_may_be_stale')}` : '');
  el.classList.toggle('warn', anyFailure());
}

/* ---------- overview: system + tool KPIs ---------- */

function renderSystem() {
  const o = state.overview;
  const sys = o && o.system ? o.system : null;
  const label = $('#system-status');
  const d = $('#system-dot');
  const note = $('#system-note');
  if (!o) {
    label.textContent = '—';
    d.className = 'dot unknown';
    setNote(note, state.overviewError ? t('web.note.read_failed') + state.overviewError : t('web.note.loading_2'), state.overviewError ? 'bad' : '');
  } else if (!sys) {
    label.textContent = t('web.status.unknown');
    d.className = 'dot unknown';
    setNote(note, t('web.note.data_may_be_stale_the_overview_endpoint_returned_no_system_s'), 'warn');
  } else {
    label.textContent = SYSTEM[sys.status] || t('web.status.unknown');
    d.className = 'dot ' + (sys.status === 'waiting' ? 'unknown' : (DOT[sys.status] || 'unknown'));
    const summary = t('web.note.participating_observed_failing', { enabled: fmtInt(sys.enabled), observed: fmtInt(sys.observed), down: fmtInt(sys.down) }) +
    (num(sys.suspended) > 0 ? ` ${t('web.note.observation_warnings', { count: fmtInt(sys.suspended) })}` : '');
    if (state.overviewError) setNote(note, summary + ` ${t('web.note.data_may_be_stale')}` + state.overviewError, 'warn');
    else if (sys.status === 'waiting') setNote(note, summary + ` ${t('web.note.no_recent_real_calls_observed_yet')}`, '');
    else setNote(note, summary, '');
  }
  const has = o && sys;
  setText('#system-enabled', has ? fmtInt(sys.enabled) : '—');
  setText('#system-observed', has ? fmtInt(sys.observed) : '—');
  setText('#system-degraded', has ? fmtInt(sys.degraded) : '—');
  setText('#system-down', has ? fmtInt(sys.down) : '—');
  setText('#system-suspended', has ? fmtInt(sys.suspended) : '—');
}

function renderToolKpis() {
  const raw = state.overview && state.overview.today;
  const today = raw && raw.day ? raw : null;
  const avg = today && num(today.requests) > 0 ? Math.round(today.duration_ms / today.requests) : null;
  const cards = [
    [t('web.label.successful_calls_today'), today ? fmtInt(today.successes) : '—', today ? t('web.note.calls_cache_hits', { requests: fmtInt(today.requests), cacheHits: fmtInt(today.cache_hits) }) : t('web.note.no_tool_layer_calls_yet')],
    [t('web.label.failures_today'), today ? fmtInt(today.failures) : '—', t('web.note.counts_mcp_tool_layer_events_only')],
    [t('web.label.average_response_time_today'), avg === null ? '—' : fmtMS(avg), today && num(today.requests) > 0 ? t('web.note.total_tool_layer_time_calls') : t('web.note.not_computed_without_calls')],
  ];
  const kpis = $('#overview-kpis');
  if (!kpis) return;
  kpis.innerHTML = cards.map(([label, value, detail]) =>
    `<article class="kpi"><header><b>${esc(label)}</b></header><strong>${esc(value)}</strong><small class="dim">${esc(detail)}</small></article>`
  ).join('');
}

// renderMCPTools 展示公开 MCP 工具的固定清单（不依赖是否已经产生调用），
// 并用遥测健康数据补充“已观测 / 尚无调用”的观测状态。
function renderMCPTools() {
  const list = $('#mcp-tools-list');
  if (!list) return;
  const o = state.overview;
  const configured = o && Array.isArray(o.configured_tools) ? o.configured_tools : [];
  const note = $('#mcp-tools-note');
  const help = $('#mcp-tools-help');
  if (!configured.length) {
    list.innerHTML = `<p class="empty">${esc(o ? t('web.note.the_overview_endpoint_returned_no_tool_list') : t('web.note.loading_2'))}</p>`;
    if (note) note.textContent = '—';
    if (help) setNote(help, state.overviewError ? t('web.note.read_failed') + state.overviewError : '', state.overviewError ? 'bad' : '');
    return;
  }
  const observed = new Map();
  (Array.isArray(o.tools) ? o.tools : []).forEach(h => h && observed.set(h.name, h));
  const enabledCount = configured.filter(t => t.enabled).length;
  if (note) note.textContent = enabledCount + ' / ' + configured.length + ` ${t('web.status.enabled')}`;
  if (help) setNote(help, t('web.note.switches_come_from_the_running_config_health_comes_from_real'), '');
  list.innerHTML = configured.map(tool => {
    const health = observed.get(tool.name);
    let stateLabel;
    let dotClass;
    if (!tool.enabled) {
      stateLabel = t('web.status.disabled_in_config');
      if (tool.name === 'file_search' && (tool.registration_state || tool.state) === 'unprobed') stateLabel += ` ${t('web.note.not_probed_yet')}`;
      dotClass = 'unknown';
    } else if (['unprobed', 'pending', 'unavailable', 'disabled'].includes(tool.registration_state || tool.state)) {
      stateLabel = ({ unprobed: t('web.status.not_probed'), pending: t('web.status.awaiting_registration'), unavailable: t('web.status.unavailable'), disabled: t('web.status.not_registered') })[tool.registration_state || tool.state];
      dotClass = 'unknown';
    } else if (health && health.last_seen_at) {
      stateLabel = HEALTH[health.status] || t('web.status.unknown');
      dotClass = DOT[health.status] || 'unknown';
    } else {
      stateLabel = t('web.status.callable_no_observations_yet');
      dotClass = 'unknown';
    }
    const stats = health && health.today
      ? `<small>${fmtInt(health.today.requests)} ${t('web.note.text')} ${fmtInt(health.today.successes)} ${t('web.note.success')} ${fmtInt(health.today.failures)} ${t('web.status.failure')}</small>`
      : `<small>${t('web.note.no_tool_layer_events_yet')}</small>`;
    const pdf = tool.pdf ? `<small class="pdf-params">${t('web.label.default_pages')} ${esc(tool.pdf.max_pages)} ${t('web.note.page_limit_per_task')} ${esc(tool.pdf.mineru_page_limit)} ${t('web.note.batch_size')} ${esc(tool.pdf.mineru_page_batch_size)} ${t('web.note.call_budget')} ${esc(tool.pdf.mineru_page_budget)} · OCR ${tool.pdf.ocr_enabled ? t('web.label.on') : t('web.label.off_2')}${tool.pdf.page_progress === 'not_collected' ? t('web.note.page_progress_not_collected') : ''}</small>` : '';
    const file = tool.file ? `<small class="pdf-params">${t('web.label.allowlisted_roots')} ${esc(tool.file.allowed_root_count)} · ${tool.file.whitelist_enforced ? t('web.status.allowlist_enforced') : t('web.status.directories_unrestricted')}${tool.file.path_metadata_only ? t('web.note.path_metadata_only') : ''}</small>` : '';
    const fetch = typeof tool.fetch_enabled === 'boolean' ? `<small>${t('web.note.saved_content_reads_available_url_fetching')}${tool.fetch_enabled ? t('web.label.on') : t('web.label.off_2')}</small>` : '';
    return `<div class="tool-row"><span class="tool-head">${dot(dotClass)}` +
      `<b>${esc(tool.label || displayName(tool.name))}</b><small class="mono">${esc(tool.name)}</small></span>` +
      `<span class="tool-state">${esc(stateLabel)}</span>` +
      `<p class="tool-stats">${stats}${pdf}${file}${fetch}</p></div>`;
  }).join('');
}

/* ---------- sources ---------- */

function webSources() {
  const o = state.overview;
  if (!o) return null;
  if (Array.isArray(o.sources)) return o.sources;
  // Legacy payload: show observed health rows without inventing a catalog.
  return (Array.isArray(o.providers) ? o.providers : []).map(h => Object.assign({}, h, {
    id: h.name, name: displayName(h.name), group: 'web', configured: null, active: null,
  }));
}

function academicSources() {
  const o = state.overview;
  if (!o) return null;
  return Array.isArray(o.academic_sources) ? o.academic_sources : null;
}

function tavilyItem() {
  if (!Array.isArray(state.quotas)) return null;
  return state.quotas.find(q => q && q.provider === 'tavily') || null;
}

function healthStrip(s) {
  const outcomes = Array.isArray(s.recent_outcomes) ? s.recent_outcomes.slice(-20) : [];
  const cells = Array(Math.max(0, 20 - outcomes.length)).fill(null).concat(outcomes);
  const title = outcomes.length ? t('web.note.last_real_calls', { count: outcomes.length }) : t('web.note.no_real_calls_yet');
  return `<span class="health-strip" title="${esc(title)}" aria-label="${esc(title)}">` +
    cells.map(ok => `<i class="${ok === null ? 'empty' : ok ? 'ok' : 'fail'}"></i>`).join('') + '</span>';
}

function sourceRowHtml(s, detailed) {
  const health = healthOf(s);
  const today = s.today && s.today.day ? s.today : null;
  const name = s.name || displayName(s.id);
  const id = s.id || '';
  const successRate = today && num(today.requests) > 0 ? today.successes / today.requests : null;
  const avg = today && num(today.requests) > 0 ? fmtMS(Math.round(today.duration_ms / today.requests)) : '<span class="dim">—</span>';
  const cells = [
    `<td class="src-name"><b>${esc(name)}</b>${detailed && id === 'doubao' ? doubaoVersionControl() : ''}</td>`,
    `<td>${sourceControl(s)}</td>`,
    `<td><span class="nowrap" title="${t('web.note.last_observed')}${esc(fmtAt(s.last_seen_at))}">${dot(health.dot)} ${esc(health.label)}</span>${suspendBadge(s)}</td>`,
    `<td>${healthStrip(s)}</td>`,
    `<td class="num">${today ? fmtInt(today.requests) : '<span class="dim">—</span>'}</td>`,
    `<td class="num">${successRate === null ? '<span class="dim">—</span>' : esc(Math.round(successRate * 100) + '%')}</td>`,
    `<td class="num">${avg}</td>`,
    `<td class="wrap-any">${s.last_error ? '<span class="bad-text">' + esc(s.last_error) + '</span>' : '<span class="dim">—</span>'}</td>`,
  ];
  const rowClass = s.active === true && s.suspended === true ? 'row-suspended' : (s.active === true && s.status === 'down' ? 'row-down' : '');
  return `<tr class="${rowClass}">${cells.join('')}</tr>`;
}

function suspendBadge(s) {
  if (!s || s.suspended !== true) return '';
  const reason = s.suspend_reason ? errorKindLabel(s.suspend_reason) : t('web.status.repeated_failures');
  const secs = num(s.suspend_countdown_sec);
  const countdown = secs && secs > 0 ? (secs >= 60 ? Math.ceil(secs / 60) + ` ${t('web.label.min')}` : secs + ` ${t('web.label.seconds')}`) : '';
  return `<span class="kind-chip suspend" title="${t('web.note.historical_observation_warning')}${esc(reason)}${countdown ? t('web.note.text_2') + countdown : ''}">${t('web.status.observation_warning')}</span>`;
}

function savedValues() {
  return state.settings && (state.settings.saved_values || state.settings.values) || {};
}

function doubaoVersionControl() {
  const key = 'doubao.version';
  const locked = state.settings && state.settings.locked_fields && state.settings.locked_fields[key];
  const value = locked ? state.settings.values[key] : draftValue(key);
  const reason = settingsFieldReason(key);
  return `<select class="source-version" data-source-setting="${key}" aria-label="${t('web.label.doubao_api_version')}" title="${esc(reason || t('web.label.doubao_api_version'))}" ${reason ? 'disabled' : ''}><option value="global" ${value === 'global' ? 'selected' : ''}>Global</option><option value="custom" ${value === 'custom' ? 'selected' : ''}>Custom</option></select>`;
}

function draftValue(key) {
  return Object.hasOwn(state.draft, key) ? state.draft[key] : savedValues()[key];
}

function writeAllowed() {
  return Boolean(!state.settingsError && state.settings && !state.settings.config_error && state.settings.revision && state.settings.write_access && state.settings.write_access.allowed && adminConfigured());
}

function sourceControl(s) {
  const key = s.control_path;
  const inputId = 'source-switch-' + s.id;
  const value = key && Object.hasOwn(savedValues(), key) ? draftValue(key) : s.inverted ? !s.enabled : s.enabled;
  const enabled = s.inverted ? value === false : value === true;
  const locked = key && state.settings && state.settings.locked_fields && state.settings.locked_fields[key];
  const disabled = !key || !writeAllowed() || locked || state.applying || state.saving;
  return `<div class="form-check form-switch source-toggle" title="${esc(locked || (!writeAllowed() ? writeReason() : ''))}"><input class="form-check-input" type="checkbox" role="switch" id="${esc(inputId)}" data-source-key="${esc(key || '')}" data-inverted="${s.inverted === true}" aria-label="${esc(s.name || s.id)} ${t('web.status.enabled_in_config')}" ${enabled ? 'checked' : ''} ${disabled ? 'disabled' : ''}></div>`;
}

function countsLabel(list) {
  return `${t('web.status.enabled')} ${list.filter(s => s.enabled).length} / ${list.length}`;
}

function renderWebSources() {
  const body = $('#web-sources-body');
  const note = $('#web-sources-note');
  // 总览已移除 Web 来源面板；搜索源页使用 renderSourcePages。
  if (!body) return;
  const list = webSources();
  if (list === null) {
    body.innerHTML = emptyRow(8, state.overviewError ? t('web.status.read_failed') : t('web.note.loading_2'));
    setNote(note, state.overviewError ? t('web.note.read_failed') + state.overviewError : '', state.overviewError ? 'bad' : '');
    return;
  }
  const active = list.filter(s => s.active === true);
  const shown = active;
  body.innerHTML = shown.length ? shown.map(s => sourceRowHtml(s, false)).join('') : emptyRow(8, t('web.note.no_web_source_is_enabled_in_the_current_mode'));
  if (state.overviewError) setNote(note, t('web.note.data_may_be_stale_2') + state.overviewError, 'warn');
  else if (!Array.isArray(state.overview.sources)) setNote(note, t('web.note.data_may_be_stale_the_endpoint_returned_no_source_catalog_sh'), 'warn');
  else setNote(note, '', '');
}

function renderSourcePages() {
  const webList = webSources();
  const acadList = academicSources();
  const groups = [['web', webList && webList.filter(s => s.category !== 'api')], ['api', webList && webList.filter(s => s.category === 'api')], ['academic', acadList]];
  groups.forEach(([id, list]) => {
    const body = $('#sources-' + id + '-body');
    if (!body) return;
    body.innerHTML = list === null ? emptyRow(8, t('web.note.loading_2')) : list.length ? list.map(s => sourceRowHtml(s, true)).join('') : emptyRow(8, t('web.note.no_sources_yet'));
    setText('#sources-' + id + '-counts', list ? countsLabel(list) : '—');
    setNote($('#sources-' + id + '-note'), state.overviewError ? t('web.note.data_may_be_stale_2') + state.overviewError : '', state.overviewError ? 'warn' : '');
  });
  renderDraftState();
}

/* ---------- Tavily quota card ---------- */

function renderTavily() {
  const value = $('#tavily-value');
  const note = $('#tavily-note');
  const dotEl = $('#tavily-dot');
  const details = $('#tavily-details');
  // 总览已移除额度面板；额度仍通过搜索源页的额度列展示。
  if (!value || !details) return;
  const q = tavilyItem();
  const kv = (pairs) => pairs.filter(p => p[1]).map(p => `<div><dt>${esc(p[0])}</dt><dd>${esc(p[1])}</dd></div>`).join('');
  if (state.quotaError && !q) {
    value.textContent = t('web.status.query_failed');
    dotEl.className = 'dot down';
    setNote(note, t('web.note.data_may_be_stale_2') + state.quotaError, 'warn');
    details.innerHTML = '';
    return;
  }
  if (!q) {
    value.textContent = '—';
    dotEl.className = 'dot unknown';
    setNote(note, state.quotas ? t('web.note.the_official_quota_endpoint_returned_no_tavily_entry') : t('web.note.loading_2'), state.quotas ? 'bad' : '');
    details.innerHTML = '';
    return;
  }
  details.innerHTML = kv([
    [t('web.label.used'), num(q.used) === null ? '' : fmtInt(q.used) + ' ' + (q.unit || '')],
    [t('web.label.limit'), num(q.limit) === null ? '' : fmtInt(q.limit) + ' ' + (q.unit || '')],
    [t('web.label.plan'), q.plan || ''],
    [t('web.label.updated'), q.updated_at ? fmtAt(q.updated_at) : ''],
    [t('web.label.official_api'), q.source || ''],
  ]);
  switch (q.status) {
    case 'available':
      value.textContent = `${fmtInt(q.remaining)} / ${fmtInt(q.limit)} ${q.unit || ''}`.trim();
      dotEl.className = 'dot healthy';
      setNote(note, state.quotaError ? t('web.note.data_may_be_stale_2') + state.quotaError : t('web.note.from_the_official_quota_endpoint_not_a_local_estimate'), state.quotaError ? 'warn' : '');
      break;
    case 'not_configured':
      value.textContent = t('web.status.not_configured');
      dotEl.className = 'dot unknown';
      setNote(note, t('web.note.tavily_key_not_configured_cannot_query_the_official_quota'), '');
      break;
    case 'query_failed':
      value.textContent = t('web.status.query_failed');
      dotEl.className = 'dot down';
      setNote(note, q.error || t('web.note.the_official_quota_endpoint_returned_no_usable_data'), 'bad');
      break;
    default:
      value.textContent = t('web.status.unknown');
      dotEl.className = 'dot unknown';
      setNote(note, q.error || t('web.note.the_official_quota_endpoint_returned_an_unrecognized_status'), 'warn');
  }
}

/* ---------- effective config summary ---------- */

function renderConfig() {
  const list = $('#config-list');
  const note = $('#config-note');
  const secrets = $('#config-secrets');
  const s = state.settings;
  if (!s) {
    list.innerHTML = '';
    secrets.textContent = '';
    setNote(note, state.settingsError ? t('web.note.read_failed') + state.settingsError : t('web.note.loading_2'), state.settingsError ? 'bad' : '');
    return;
  }
  const v = s.values || {};
  const yn = value => value === true ? t('web.label.enabled') : value === false ? t('web.label.off') : t('web.status.unknown');
  const rows = [
    [t('web.label.search_mode'), typeof v.mode === 'string' ? v.mode : ''],
    [t('web.label.academic_search_network'), v.network === 'china' ? t('web.label.china_network') : v.network === 'international' ? t('web.label.international_network') : ''],
    [t('web.label.upstream_timeout'), num(v.upstream_timeout_sec) === null ? '' : String(v.upstream_timeout_sec) + ` ${t('web.label.seconds')}`],
    [t('web.label.cache'), yn(v['cache.enabled'])],
    [t('web.label.observation_warning_pause_window'), [v['dashboard.suspension.ban_time_on_fail'], v['dashboard.suspension.max_ban_time_on_fail']].filter(Boolean).join(' / ')],
    [t('web.label.enabled_sources'), state.overview && state.overview.system ? fmtInt(state.overview.system.enabled) + ` ${t('web.label.sources')}` : ''],
  ];
  list.innerHTML = rows.filter(r => r[1]).map(r => `<div><dt>${esc(r[0])}</dt><dd>${esc(r[1])}</dd></div>`).join('');
  secrets.textContent = '';
  if (state.settingsError) setNote(note, t('web.note.data_may_be_stale_2') + state.settingsError, 'warn');
  else setNote(note, t('web.note.effective_config_after_a_restart'), '');
}

/* ---------- calls and request details ---------- */

async function loadRequestEvents(requestId, reload = false) {
  const cached = state.events.details.get(requestId);
  if (cached && cached.loading) return cached.promise;
  if (cached && cached.rows && !reload) return cached.rows;
  const detail = { rows: cached ? cached.rows : null, error: null, loading: true };
  state.events.details.set(requestId, detail);
  detail.promise = (async () => {
    try {
      const rows = await fetchJSON('/__admin/api/request-events?request_id=' + encodeURIComponent(requestId), { signal: AbortSignal.timeout(10000) });
      if (!Array.isArray(rows)) throw new Error(t('web.msg.malformed_request_details_response'));
      detail.rows = rows;
      return rows;
    } catch (e) {
      detail.error = e.message;
      throw e;
    } finally {
      detail.loading = false;
      renderUsage();
    }
  })();
  renderUsage();
  return detail.promise;
}

function callDetails(e) {
  const metadata = [
    [t('web.label.request'), e.request_id], [t('web.label.application'), applicationLabel(e)], [t('web.label.client'), e.client], [t('web.label.project'), e.project || t('web.label.untagged_project')],
    [t('web.label.cache'), e.cache_hit ? t('web.status.hit') : t('web.status.miss')], [t('web.label.topic_language'), [e.query_topic, e.query_language].filter(Boolean).join(' / ')],
    [t('web.label.keywords'), e.query_keywords], [t('web.label.query_hash'), e.query_hash], [t('web.label.attempt_chain'), e.attempt_chain], [t('web.label.error_kind'), errorKindLabel(e.error_kind)], [t('web.label.error_summary'), e.error_summary],
  ].filter(item => item[1]);
  let html = '<dl class="call-detail-meta">' + metadata.map(([key, value]) =>
    `<div><dt>${esc(key)}</dt><dd>${esc(value)}</dd></div>`).join('') + '</dl>';
  if (!e.request_id) return html + `<p class="panel-note">${t('web.note.this_history_entry_has_no_request_id_so_source_details_canno')}</p>`;
  const detail = state.events.details.get(e.request_id);
  if (!detail || detail.loading) return html + `<p class="panel-note" role="status">${t('web.note.loading_source_details')}</p>`;
  if (detail.error) return html + `<p class="panel-note bad">${t('web.note.read_failed')}${esc(detail.error)} <button type="button" class="icon-btn" data-retry-request="${esc(e.request_id)}" aria-label="${t('web.action.retry_loading_source_details')}" title="${t('web.action.retry_loading_source_details')}">${icon('refresh-cw')}</button></p>`;
  const providers = (detail.rows || []).filter(row => row.kind === 'provider');
  if (!providers.length) return html + `<p class="panel-note">${t('web.note.no_source_details_were_retained')}</p>`;
  html += `<table class="data-table call-detail-table"><colgroup><col style="width:24%"><col style="width:12%"><col style="width:14%"><col style="width:10%"><col style="width:40%"></colgroup><thead><tr><th>${t('web.label.source')}</th><th>${t('web.label.status')}</th><th>${t('web.label.latency')}</th><th>${t('web.label.results')}</th><th>${t('web.label.error_summary')}</th></tr></thead><tbody>`;
  html += providers.map(row => `<tr><td title="${esc(row.detail || '')}">${esc(displayName(row.provider))}</td><td class="${row.success ? 'ok-text' : 'bad-text'}">${row.success ? t('web.status.success') : t('web.status.failure')}</td><td>${esc(fmtMS(row.duration_ms))}</td><td>${fmtInt(row.result_count)}</td><td class="wrap-any">${esc([errorKindLabel(row.error_kind), row.error_summary].filter(Boolean).join(' · ') || '—')}</td></tr>`).join('');
  return html + '</tbody></table>';
}

async function toggleCall(id) {
  if (state.events.expanded.has(id)) {
    state.events.expanded.delete(id);
    renderUsage();
    return;
  }
  state.events.expanded.add(id);
  renderUsage();
  const e = (state.events.rows || []).find(row => String(row.id) === id);
  if (e && e.request_id) {
    try { await loadRequestEvents(e.request_id); } catch (error) { /* Inline retry keeps the main list usable. */ }
  }
}

function renderUsage() {
  const body = $('#usage-body');
  const note = $('#events-note');
  const rows = state.events.rows;
  const exportBtn = $('#export-csv');
  exportBtn.disabled = !rows || !rows.length || state.events.loading || state.events.exporting || Boolean(state.events.error);
  $('#reload-events').disabled = state.events.loading;
  if (rows === null) {
    body.innerHTML = emptyRow(8, state.events.error ? t('web.note.read_failed_refresh_to_retry') : t('web.note.loading_2'));
  } else {
    body.innerHTML = rows.length ? rows.map(e => {
      const id = String(e.id);
      const open = state.events.expanded.has(id);
      const buttonLabel = (open ? t('web.action.collapse') : t('web.action.expand')) + displayName(e.tool) + t('web.label.call_details');
      return `<tr class="call-row"><td><button type="button" class="icon-btn call-expand" data-call="${esc(id)}" aria-expanded="${open}" aria-controls="call-detail-${esc(id)}" aria-label="${esc(buttonLabel)}" title="${esc(buttonLabel)}">${icon('chevron-right')}</button></td><td>${esc(fmtAt(e.occurred_at))}</td><td class="source-name">${esc(displayName(e.tool))}</td><td class="call-attribution" title="${esc(e.client || t('web.label.unknown_client'))}"><b>${esc(applicationLabel(e))}</b><small title="${t('web.note.self_reported_project_label_not_verified_by_the_system')}">${esc(e.project || t('web.label.untagged_project'))}</small></td><td class="${e.success ? 'ok-text' : 'bad-text'}">${e.success ? t('web.status.success') : t('web.status.failure')}</td><td data-label="${t('web.label.latency')}">${esc(fmtMS(e.duration_ms))}</td><td data-label="${t('web.label.results')}">${fmtInt(e.result_count)}</td><td class="wrap-any"><span class="call-error" title="${esc(e.error_summary || '')}">${esc(e.error_summary || '—')}</span></td></tr><tr id="call-detail-${esc(id)}" class="call-detail"${open ? '' : ' hidden'}><td colspan="8">${open ? callDetails(e) : ''}</td></tr>`;
    }).join('') : emptyRow(8, t('web.note.no_calls_match_the_current_filters'));
  }
  $$('[data-call]', body).forEach(button => { button.onclick = () => toggleCall(button.dataset.call); });
  $$('[data-retry-request]', body).forEach(button => {
    button.onclick = () => loadRequestEvents(button.dataset.retryRequest, true).catch(() => {});
  });
  setNote(note, state.events.error ? t('web.note.read_failed_the_records_shown_may_be_stale') + state.events.error :
    state.events.loading ? t('web.note.refreshing') : state.events.exporting ? t('web.note.exporting_calls_and_source_details') : `${t('web.label.last')} ` + (rows || []).length + ` ${t('web.note.calls_up_to_100')}`, state.events.error ? 'bad' : '');
  refreshHScroll();
}

/* ---------- CSV export (UTF-8 BOM) ---------- */

function csvCell(value) {
  const s = value === null || value === undefined ? '' : String(value);
  const safe = typeof value === 'string' && /^[\s]*[=+@-]/.test(s) ? "'" + s : s;
  return '"' + safe.replace(/"/g, '""').replace(/[\r\n]+/g, ' ') + '"';
}
function stamp() {
  const d = new Date();
  const p = n => String(n).padStart(2, '0');
  return d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) + '-' + p(d.getHours()) + p(d.getMinutes()) + p(d.getSeconds());
}
async function exportCSV() {
  const calls = (state.events.rows || []).slice();
  if (state.events.loading || state.events.exporting || state.events.error) return;
  const rows = [];
  state.events.exporting = true;
  renderUsage();
  try {
    const requests = new Set();
    for (const call of calls) {
      rows.push(call);
      if (!call.request_id || requests.has(call.request_id)) continue;
      requests.add(call.request_id);
      const details = await loadRequestEvents(call.request_id, true);
      rows.push(...details.filter(e => e.kind === 'provider'));
    }
  } catch (e) {
    toast(t('web.msg.export_incomplete_reading_source_details_failed') + e.message, true);
    return;
  } finally {
    state.events.exporting = false;
    renderUsage();
  }
  if (!rows.length) {
    toast(t('web.msg.nothing_to_export_right_now'), true);
    return;
  }
  const head = [t('web.label.time'), t('web.label.kind'), t('web.label.tool'), t('web.label.source'), t('web.label.parsed_source'), t('web.label.status'), t('web.label.error_kind'), t('web.label.latency_ms'), t('web.label.results'), t('web.label.cache_hits'), t('web.label.theme'), t('web.label.language'), t('web.label.keywords'), t('web.label.query_hash'), t('web.label.query_length'), t('web.label.error_summary'), t('web.label.request_id'), t('web.label.attempt_chain'), t('web.label.application'), t('web.label.client'), t('web.label.project')];
  const lines = [head.map(csvCell).join(',')];
  rows.forEach(e => {
    lines.push([
      fmtAt(e.occurred_at),
      e.kind === 'provider' ? t('web.label.source') : t('web.label.tool'),
      e.kind === 'tool' ? displayName(e.tool) : '',
      e.provider ? displayName(e.provider) : '',
      e.detail || '',
      e.success ? t('web.status.success') : t('web.status.failure'),
      errorKindLabel(e.error_kind),
      num(e.duration_ms) === null ? '' : e.duration_ms,
      num(e.result_count) === null ? '' : e.result_count,
      e.cache_hit ? t('web.label.yes') : t('web.label.no'),
      e.query_topic || '',
      e.query_language || '',
      e.query_keywords || '',
      e.query_hash || '',
      num(e.query_chars) === null ? '' : e.query_chars,
      e.error_summary || '',
      e.request_id || '',
      e.attempt_chain || '',
      applicationLabel(e),
      e.client || '',
      e.project || '',
    ].map(csvCell).join(','));
  });
  const blob = new Blob(['\uFEFF' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'websearch-events-' + stamp() + '.csv';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
  toast(`${t('web.msg.exported')} ` + rows.length + ` ${t('web.msg.records_utf_8_bom')}`);
}

/* ---------- usage filters ---------- */

function knownFilterEntries() {
  const tools = new Map();
  const providers = new Map();
  const o = state.overview;
  const addTool = (id, label) => { if (id) tools.set(id, label || displayName(id)); };
  const addProvider = (id, label) => { if (id) providers.set(id, label || displayName(id)); };
  if (o) {
    (Array.isArray(o.configured_tools) ? o.configured_tools : []).forEach(t => t && addTool(t.name, t.label));
    (Array.isArray(o.providers) ? o.providers : []).forEach(h => h && addProvider(h.name));
    (Array.isArray(o.tools) ? o.tools : []).forEach(h => h && addTool(h.name));
    (Array.isArray(o.sources) ? o.sources : []).forEach(s => s && addProvider(s.id, s.name));
    (Array.isArray(o.academic_sources) ? o.academic_sources : []).forEach(s => s && addProvider(s.id, s.name));
  }
  [state.events.rows, ...Array.from(state.events.details.values(), detail => detail.rows)].forEach(list => {
    (list || []).forEach(e => {
      if (!e) return;
      if (e.kind === 'provider') addProvider(e.provider);
      else addTool(e.tool);
    });
  });
  const sort = map => Array.from(map.entries()).sort((a, b) => String(a[1]).localeCompare(String(b[1]), 'zh-CN'));
  return { tools: sort(tools), providers: sort(providers) };
}

function renderFilterSelect(selector, entries, current) {
  const sel = $(selector);
  if (!sel) return;
  const known = new Set(entries.map(e => e[0]));
  const options = [`<option value="">${t('web.label.all')}</option>`];
  if (current && !known.has(current)) {
    options.push(`<option value="${esc(current)}">${esc(displayName(current))}</option>`);
  }
  entries.forEach(([id, label]) => {
    const text = label || id;
    options.push(`<option value="${esc(id)}">${esc(text)}</option>`);
  });
  sel.innerHTML = options.join('');
  sel.value = current;
}

function knownErrorKinds() {
  const map = new Map(Object.entries(ERROR_KINDS));
  const add = kind => { if (kind) map.set(kind, errorKindLabel(kind)); };
  [state.events.rows, ...Array.from(state.events.details.values(), detail => detail.rows)].forEach(list => {
    (list || []).forEach(e => e && !e.success && add(e.error_kind));
  });
  return Array.from(map.entries()).sort((a, b) => String(a[1]).localeCompare(String(b[1]), 'zh-CN'));
}

function renderUsageSourceOptions() {
  const entries = knownFilterEntries();
  renderFilterSelect('#filter-tool', entries.tools, state.filters.tool);
  renderFilterSelect('#filter-provider', entries.providers, state.filters.provider);
  renderFilterSelect('#filter-error-kind', knownErrorKinds(), state.filters.errorKind);
}

/* ---------- settings & advanced actions ---------- */

function settingsFieldReason(key) {
  const locked = state.settings && state.settings.locked_fields && state.settings.locked_fields[key];
  if (locked) return locked;
  if (!writeAllowed()) return writeReason();
  if (state.applying || state.saving) return t('web.note.applying_configuration');
  const versionLocked = state.settings.locked_fields && state.settings.locked_fields['doubao.version'];
  const version = versionLocked ? state.settings.values['doubao.version'] : draftValue('doubao.version');
  return key === 'doubao.icp_host_only' && version !== 'global' ? t('web.note.applies_to_global_only_the_current_value_is_kept') : '';
}

function renderSettingsForm() {
  const s = state.settings;
  if (!s || !s.values) return;
  $$('[data-key]').forEach(el => {
    const lockedValue = ['mode', 'doubao.version', 'doubao.icp_host_only'].includes(el.dataset.key) && s.locked_fields && s.locked_fields[el.dataset.key];
    const value = lockedValue ? s.values[el.dataset.key] : draftValue(el.dataset.key);
    if (el.type === 'checkbox') el.checked = value === true;
    else if (el.type === 'radio') el.checked = el.value === value;
    else el.value = value === null || value === undefined ? '' : value;
    const reason = settingsFieldReason(el.dataset.key);
    el.disabled = Boolean(reason);
    el.title = reason;
  });

  renderModeControls();
  const flags = s.secrets || {};
  $('#secret-list').innerHTML = Object.keys(flags).map(key => {
    const on = flags[key] === true;
    return `<div class="secret-row">${dot(on ? 'healthy' : 'unknown')}` +
      `<div><b>${esc(SECRETS[key] || key)}</b><small>${on ? t('web.status.configured') : t('web.status.not_configured')}</small></div>` +
      `<button type="button" class="icon-btn" data-secret="${esc(key)}" title="${on ? t('web.action.replace') : t('web.page.settings')}${esc(SECRETS[key] || key)}${t('web.label.key')}" aria-label="${on ? t('web.action.replace') : t('web.page.settings')}${esc(SECRETS[key] || key)}${t('web.label.key')}" ${!writeAllowed() || state.applying || state.saving ? 'disabled' : ''}>${icon('key-round')}</button></div>`;
  }).join('');
  $$('[data-secret]').forEach(b => { b.onclick = () => editSecret(b.dataset.secret); });
  state.settingsLoaded = true;
  renderDraftState();
}

const PROVIDER_MODES = ['baidu', 'tavily', 'exa', 'anysearch', 'doubao', 'yuanbao'];

function renderModeControls() {
  if (!state.settings) return;
  const locked = state.settings.locked_fields && state.settings.locked_fields.mode;
  const mode = locked ? state.settings.values.mode : draftValue('mode');
  const single = PROVIDER_MODES.includes(mode);
  const select = $('#mode-provider');
  if (single) select.value = mode;
  if (!PROVIDER_MODES.includes(select.value)) select.value = 'baidu';
  const reason = settingsFieldReason('mode');
  $$('input[name="mode"]').forEach(el => {
    el.checked = el.value === (single ? 'provider' : mode);
    el.disabled = Boolean(reason);
    el.title = reason;
  });
  $('#mode-provider-options').classList.toggle('hidden', !single);
  select.disabled = !single || Boolean(reason);
  select.title = reason;
  const info = $('#mode-provider-info');
  info.setAttribute('aria-label', select.value + ` ${t('web.mode.mode_help')}`);
  info.setAttribute('aria-describedby', 'mode-note-' + select.value);
  PROVIDER_MODES.forEach(id => $('#mode-note-' + id).classList.toggle('hidden', id !== select.value));
}

function changeSingleProvider(e) {
  if (e.target.type === 'radio' && !e.target.checked) return;
  setDraft('mode', $('#mode-provider').value);
  renderSettingsForm();
}

function changes() {
  return { ...state.draft };
}

function setDraft(key, value) {
  if (!key || settingsFieldReason(key)) return;
  if (!state.settingsDirty) state.draftRevision = state.settings.revision;
  if (!Object.hasOwn(state.draftBefore, key)) state.draftBefore[key] = savedValues()[key];
  if (JSON.stringify(value) === JSON.stringify(state.draftBefore[key])) {
    delete state.draft[key];
    delete state.draftBefore[key];
  } else state.draft[key] = value;
  renderDraftState();
  if (key === 'mode') renderModeControls();
}

function markDirty(e) {
  const el = e.target;
  if (el.type === 'radio' && !el.checked) return;
  setDraft(el.dataset.key, el.type === 'checkbox' ? el.checked : el.type === 'number' ? Number(el.value) : el.value);
}

function setModeHelp(button, expanded, dismissed = false) {
  const help = button.parentElement;
  help.classList.toggle('open', expanded);
  help.classList.toggle('dismissed', dismissed);
  button.setAttribute('aria-expanded', String(expanded));
}

function renderDraftState() {
  const count = Object.keys(state.draft).length;
  const dirty = count > 0;
  state.settingsDirty = dirty;
  $('#settings-dirty').classList.toggle('hidden', !dirty);
  setText('#source-draft-count', dirty ? count + ` ${t('web.note.draft_items')}` : '');
  ['#save-settings', '#save-sources'].forEach(sel => { const el = $(sel); if (el) el.disabled = !dirty || !writeAllowed() || state.applying || state.saving; });
  ['#undo-settings', '#undo-sources'].forEach(sel => { const el = $(sel); if (el) el.disabled = !dirty || state.applying || state.saving; });
}

function discardDraft() {
  state.draft = {};
  state.draftBefore = {};
  state.draftRevision = '';
  renderSettingsForm();
  renderSourcePages();
}

function writeReason() {
  if (!state.settings) return t('web.note.reading_write_permission');
  if (state.settingsError) return t('web.note.read_only_reading_permissions_failed') + state.settingsError;
  if (state.settings.config_error) return t('web.note.read_only') + state.settings.config_error;
  if (!state.settings.revision) return t('web.note.read_only_config_revision_unavailable');
  if (!adminConfigured()) return t('web.note.read_only_admin_password_not_configured');
  return state.settings.write_access && state.settings.write_access.reason || t('web.note.read_only_this_connection_has_no_write_access');
}

function renderWriteAccess() {
  ['#source-write-note', '#settings-write-note'].forEach(sel => setNote($(sel), writeAllowed() ? '' : writeReason(), writeAllowed() ? '' : 'warn'));
  ['#restart-service', '#clear-cache', '#retry-apply', '#restore-backup'].forEach(sel => { const el = $(sel); if (el) el.disabled = !writeAllowed() || state.applying || state.saving; });
  $$('[data-quota-reset], [data-quota-adjust]').forEach(el => { el.disabled = !writeAllowed() || state.applying || state.saving; });
  $$('[data-key]').forEach(el => {
    const reason = settingsFieldReason(el.dataset.key);
    el.disabled = Boolean(reason);
    el.title = reason;
  });
  renderModeControls();
  const backups = state.settings && state.settings.backups || [];
  const select = $('#backup-select');
  if (select) {
    const previous = select.value;
    select.innerHTML = `<option value="">${t('web.action.choose_a_backup')}</option>` + backups.map(b => `<option value="${esc(b.name)}">${esc(fmtAt(b.created_at))} · ${esc(b.name)}</option>`).join('');
    if (backups.some(b => b.name === previous)) select.value = previous;
    else if (state.applyResult && backups.some(b => b.name === state.applyResult.backup)) select.value = state.applyResult.backup;
    select.disabled = !writeAllowed() || state.applying || state.saving;
    $('#restore-backup').disabled = !writeAllowed() || state.applying || state.saving || !select.value;
  }
  renderDraftState();
}

async function confirmDialog(title, copy, diff) {
  const d = $('#confirm-dialog');
  $('#dialog-title').textContent = title;
  $('#dialog-copy').textContent = copy;
  const pre = $('#dialog-diff');
  pre.textContent = diff || '';
  pre.style.display = diff ? 'block' : 'none';
  d.showModal();
  return new Promise(resolve => {
    d.addEventListener('close', () => resolve(d.returnValue === 'confirm'), { once: true });
  });
}

function toast(message, bad) {
  const el = $('#toast');
  el.textContent = message;
  el.style.background = bad ? '#9e342f' : '#17231c';
  el.classList.add('show');
  setTimeout(() => el.classList.remove('show'), 2600);
}

async function postJSON(path, payload, headers) {
  return fetchJSON(path, {
    ...(path === '/__admin/api/restart' ? { signal: AbortSignal.timeout(5000) } : {}),
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json', ...(headers || {}) },
    body: JSON.stringify(payload),
  });
}

/* ---------- 管理员口令（写操作统一闸门） ---------- */
// 服务端约束：写端点限 loopback 或独立写白名单 + X-Admin-Password 头；
// 口令只能配置在 dashboard.yaml，控制台永远读不到也改不了它。

function adminConfigured() {
  return Boolean(state.settings && state.settings.admin_password_configured);
}

function cachedAdminPass() {
  return state.adminPass;
}

function usernameConfigured() {
  return Boolean(state.settings && state.settings.admin_username_configured);
}

function cachedAdminUser() {
  return state.adminUser;
}

// adminHeaders 取得（必要时先问一次）管理员口令；未配置或用户取消时抛错。
async function adminHeaders() {
  if (!writeAllowed()) throw new Error(writeReason());
  if (!adminConfigured()) {
    throw new Error(t('web.msg.admin_password_not_configured_set_dashboard_admin_password_i'));
  }
  let pass = cachedAdminPass();
  if (!pass) {
    pass = prompt(t('web.label.enter_the_admin_password_dashboard_admin_password'));
    if (!pass) throw new Error(t('web.msg.cancelled_this_action_needs_the_admin_password'));
    state.adminPass = pass;
  }
  const headers = { 'X-Admin-Password': pass };
  // admin_username 配置了才要求用户名；未配置 = 口令-only，不做检查
  if (usernameConfigured()) {
    let user = cachedAdminUser();
    if (!user) {
      user = prompt(t('web.label.enter_the_admin_username_dashboard_admin_username'));
      if (!user) throw new Error(t('web.msg.cancelled_this_action_needs_the_admin_username'));
      state.adminUser = user;
    }
    headers['X-Admin-User'] = user;
  }
  return headers;
}

// securePost 带口令的写请求；口令错误时清除缓存，下次重新询问。
async function securePost(path, payload) {
  const headers = await adminHeaders();
  try {
    return await postJSON(path, payload, headers);
  } catch (e) {
    if (String(e.message).includes(t('web.label.password'))) state.adminPass = '';
    if (String(e.message).includes(t('web.label.username'))) state.adminUser = '';
    throw e;
  }
}

async function saveSettings() {
  if (state.applying || state.saving || !writeAllowed()) return;
  const diff = changes();
  const invalid = $$('[data-key]').find(el => Object.hasOwn(diff, el.dataset.key) && !el.reportValidity());
  if (invalid) return;
  if (!Object.keys(diff).length) {
    toast(t('web.note.no_changes_to_save'));
    return;
  }
  state.saving = true;
  renderSettingsForm();
  renderSourcePages();
  renderWriteAccess();
  try {
    const revision = state.draftRevision || state.settings.revision;
    await securePost('/__admin/api/settings', { changes: diff, revision, confirm: false });
    const detail = Object.entries(diff).map(([key, value]) => key + ': ' + JSON.stringify(state.draftBefore[key]) + ' → ' + JSON.stringify(value)).join('\n');
    if (!(await confirmDialog(t('web.action.save_and_restart'), t('web.dialog.the_current_config_is_backed_up_then_changes_are_saved_and_t'), detail))) return;
    const res = await securePost('/__admin/api/settings', { changes: diff, revision, confirm: true });
    state.applyResult = { backup: res.backup, targets: { ...(state.settings.pending_changes || {}), ...diff }, status: t('web.msg.saved_restarting') };
    renderApplyResult();
    discardDraft();
    await loadSettings();
    await restartAndVerify();
  } catch (e) {
    toast(e.message, true);
  } finally {
    state.saving = false;
    renderSettingsForm();
    renderSourcePages();
    renderWriteAccess();
  }
}

function runtimeMatches(settings, targets) {
  return Object.entries(targets).every(([key, value]) => JSON.stringify(settings.values[key]) === JSON.stringify(value) && JSON.stringify((settings.saved_values || settings.values)[key]) === JSON.stringify(value));
}

function renderApplyResult() {
  const result = state.applyResult;
  setNote($('#apply-note'), result ? result.status : '', result && result.failed ? 'warn' : '');
  const pending = Object.keys(state.settings && state.settings.pending_changes || {}).length > 0;
  const retry = Boolean(result && result.failed) || pending;
  const backups = Boolean(state.settings && state.settings.backups && state.settings.backups.length);
  $('#apply-recovery').classList.toggle('hidden', !retry && !backups);
  $('#retry-apply').classList.toggle('hidden', !retry);
  renderWriteAccess();
}

async function restartAndVerify() {
  if (state.applying) return;
  if (!writeAllowed()) {
    if (state.applyResult) {
      state.applyResult.failed = true;
      state.applyResult.status = t('web.msg.saved_not_confirmed') + writeReason();
      renderApplyResult();
    }
    return;
  }
  if (!state.applyResult) state.applyResult = { targets: { ...(state.settings.pending_changes || {}) } };
  state.applying = true;
  state.applyResult.failed = false;
  state.applyResult.status = t('web.note.restarting_and_verifying_the_effective_config');
  renderApplyResult();
  renderSettingsForm();
  renderSourcePages();
  const deadline = Date.now() + 60000;
  try {
    await securePost('/__admin/api/restart', { confirm: true });
    let confirmed = false;
    while (Date.now() < deadline) {
      await new Promise(resolve => setTimeout(resolve, 1000));
      try {
        const settings = await fetchJSON('/__admin/api/settings', { signal: AbortSignal.timeout(Math.max(1, Math.min(3000, deadline - Date.now()))) });
        const overview = await fetchJSON('/__admin/api/overview', { signal: AbortSignal.timeout(Math.max(1, Math.min(3000, deadline - Date.now()))) });
        if (settings.values && overview.system && runtimeMatches(settings, state.applyResult.targets)) {
          state.settings = settings;
          state.overview = overview;
          state.settingsError = null;
          state.overviewError = null;
          confirmed = true;
          break;
        }
      } catch (e) { /* Restart temporarily disconnects the API. */ }
    }
    if (!confirmed) throw new Error(t('web.msg.saved_not_confirmed_within_60_seconds'));
    state.applyResult.status = t('web.msg.effective_config_confirmed');
    toast(state.applyResult.status);
  } catch (e) {
    state.applyResult.failed = true;
    state.applyResult.status = t('web.note.not_confirmed') + e.message;
  } finally {
    state.applying = false;
    renderApplyResult();
    loadAll();
  }
}

async function restoreBackup() {
  const backup = $('#backup-select').value;
  if (!backup || !writeAllowed() || state.applying || state.saving) return;
  state.saving = true;
  renderWriteAccess();
  renderSourcePages();
  try {
    const revision = state.settings.revision;
    const preview = await securePost('/__admin/api/settings', { restore_backup: backup, revision, confirm: false });
    const detail = Object.entries(preview.changes || {}).map(([key, value]) => key + ': ' + JSON.stringify(savedValues()[key]) + ' → ' + JSON.stringify(value)).join('\n');
    if (!(await confirmDialog(t('web.action.restore_backup_and_restart'), backup + t('web.note.the_current_draft_will_be_discarded'), detail))) return;
    const res = await securePost('/__admin/api/settings', { restore_backup: backup, revision, confirm: true });
    discardDraft();
    await loadSettings();
    state.applyResult = { backup: res.backup, targets: { ...(state.settings.pending_changes || {}) }, status: t('web.msg.backup_restored_restarting') };
    await restartAndVerify();
  } catch (e) { toast(e.message, true); }
  finally {
    state.saving = false;
    renderSettingsForm();
    renderSourcePages();
    renderWriteAccess();
  }
}

function editSecret(name) {
  if (!writeAllowed()) return;
  const label = SECRETS[name] || name;
  const d = $('#secret-dialog');
  $('#secret-dialog-title').textContent = `${t('web.action.update')} ` + label + ' Key';
  const link = $('#secret-dialog-link');
  const linkRow = link.closest('.secret-link-row');
  if (SECRET_LINKS[name]) {
    link.href = SECRET_LINKS[name];
    linkRow.style.display = '';
  } else {
    linkRow.style.display = 'none';
  }
  $('#secret-dialog-input').value = '';
  $('#secret-dialog-pass').value = cachedAdminPass();
  $('#secret-dialog-user-row').classList.toggle('hidden', !usernameConfigured());
  $('#secret-dialog-user').value = cachedAdminUser();
  d.showModal();
  d.addEventListener('close', async () => {
    if (d.returnValue !== 'confirm') {
      $('#secret-dialog-input').value = '';
      $('#secret-dialog-pass').value = '';
      return;
    }
    const value = $('#secret-dialog-input').value;
    const pass = $('#secret-dialog-pass').value.trim();
    if (!adminConfigured()) {
      toast(t('web.msg.admin_password_not_configured_set_dashboard_admin_password_i'), true);
      return;
    }
    if (!pass) {
      toast(t('web.msg.admin_password_required'), true);
      return;
    }
    state.adminPass = pass;
    const headers = { 'X-Admin-Password': pass };
    if (usernameConfigured()) {
      const user = $('#secret-dialog-user').value.trim();
      if (!user) {
        toast(t('web.msg.admin_username_required'), true);
        return;
      }
      state.adminUser = user;
      headers['X-Admin-User'] = user;
    }
    try {
      await postJSON('/__admin/api/secrets', { name: name, value: value, confirm: true }, headers);
      toast(label + ` ${t('web.msg.saved_the_service_must_be_restarted')}`);
      await loadSettings();
    } catch (e) {
      if (String(e.message).includes(t('web.label.password'))) state.adminPass = '';
      if (String(e.message).includes(t('web.label.username'))) state.adminUser = '';
      toast(e.message, true);
    } finally {
      $('#secret-dialog-input').value = '';
      $('#secret-dialog-pass').value = '';
      $('#secret-dialog-user').value = '';
    }
  }, { once: true });
}

/* ---------- brand & theme（品牌与主题） ---------- */
// 服务器默认主题来自 dashboard.yaml 的 dashboard.brand；本浏览器的选择
// 保存在 localStorage，优先于服务器默认。

const THEMES = ['green', 'blue', 'mono'];

function pickTheme(t) {
  return THEMES.includes(t) ? t : 'green';
}

function applyBrand(brand) {
  if (!brand || typeof brand !== 'object') return;
  const theme = pickTheme(localStorage.getItem('theme') || brand.theme);
  document.documentElement.dataset.theme = theme;
  const accent = localStorage.getItem('accent') || brand.accent || '';
  if (accent) {
    document.documentElement.style.setProperty('--accent', accent);
  } else {
    document.documentElement.style.removeProperty('--accent');
  }
  const title = brand.title || t('web.page.websearch_control_center');
  document.title = title;
  $('#brand-title').textContent = title.split(/\s+/)[0] || title;
  const about = $('#app-about');
  if (about) {
    about.classList.toggle('hidden', brand.footer === false);
    $('#about-title').textContent = title;
  }
  const logo = brand.logo || 'logo-' + theme + '.png';
  $('#brand-logo').src = logo;
  $('#favicon').href = logo;
  markActiveTheme();
}

function markActiveTheme() {
  const active = document.documentElement.dataset.theme;
  $$('#theme-picker [data-theme-pick]').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.themePick === active);
  });
}

/* ---------- 额度管理（重置 / 修正，需管理员口令） ---------- */

function renderQuotaManage() {
  const body = $('#quota-manage-body');
  const note = $('#quota-manage-note');
  if (!body || !note) return;
  if (state.quotaError) {
    body.innerHTML = '';
    setNote(note, t('web.note.read_failed') + state.quotaError, 'bad');
    return;
  }
  const rows = (state.quotas || []).filter(q => q.source === 'local');
  if (!rows.length) {
    body.innerHTML = '';
    setNote(note, t('web.note.no_local_usage_data_yet_real_calls_accumulate_automatically_'), '');
    return;
  }
  setNote(note, t('web.note.usage_comes_from_local_real_calls_successful_ones_the_limit_'), '');
  body.innerHTML = rows.map(q => {
    const used = q.used == null ? '—' : fmtInt(q.used);
    const limit = q.limit == null ? '—' : fmtInt(q.limit);
    const reset = q.reset_at ? fmtAt(q.reset_at) : t('web.label.manual_only');
    const name = esc(DISPLAY[q.provider] || q.provider);
    return `<tr><td>${name}</td><td class="num">${used} / ${limit}</td><td>${esc(reset)}</td>` +
      `<td><div class="row-actions"><button type="button" class="icon-btn" title="${t('web.action.reset')}${esc(displayName(q.provider))}${t('web.label.usage')}" aria-label="${t('web.action.reset')}${esc(displayName(q.provider))}${t('web.label.usage')}" data-quota-reset="${esc(q.provider)}">${icon('rotate-ccw')}</button> ` +
      `<button type="button" class="icon-btn" title="${t('web.action.adjust')}${esc(displayName(q.provider))}${t('web.label.usage')}" aria-label="${t('web.action.adjust')}${esc(displayName(q.provider))}${t('web.label.usage')}" data-quota-adjust="${esc(q.provider)}" data-used="${q.used || 0}">${icon('pencil')}</button></div></td></tr>`;
  }).join('');
  renderWriteAccess();
}

async function quotaReset(provider) {
  try {
    await securePost('/__admin/api/quotas/reset', { provider: provider });
    toast(DISPLAY[provider] || provider + ` ${t('web.msg.reset_counting_again_from_now')}`);
    await loadQuotas();
  } catch (e) {
    toast(e.message, true);
  }
}

async function quotaAdjust(provider, current) {
  const raw = prompt(t('web.action.set') + (DISPLAY[provider] || provider) + t('web.note.used_count_corrected_to'), String(current));
  if (raw === null) return;
  const value = Number(raw);
  if (!Number.isFinite(value) || value < 0) {
    toast(t('web.msg.enter_a_non_negative_number'), true);
    return;
  }
  try {
    await securePost('/__admin/api/quotas/adjust', { provider: provider, set_used: value });
    toast(DISPLAY[provider] || provider + ` ${t('web.msg.usage_corrected_to')} ` + value);
    await loadQuotas();
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 客户端用量（User-Agent 分组，tab 切换） ---------- */

const APPLICATION_LABELS = {
  codex: 'Codex', workbuddy: 'WorkBuddy', claude: 'Claude', cursor: 'Cursor',
  qwen: 'Qwen Code', gemini: 'Gemini', windsurf: 'Windsurf', cherry: 'Cherry Studio',
  cline: 'Cline', roo: 'Roo Code', trae: 'Trae', zed: 'Zed', vscode: 'VS Code',
  dsh: 'DeepSeek Harness', opencode: 'OpenCode', openclaw: 'OpenClaw',
  hermes: 'Hermes Agent', antigravity: 'Antigravity', amp: 'Amp', factory: 'Factory Droid',
  kimi: 'Kimi', copilot: 'GitHub Copilot', pi: 'Pi', omp: 'Oh My Pi', kilo: 'Kilo Code',
  commandcode: 'Command Code', mimocode: 'MiMo Code', muse: 'Muse Code', zcode: 'ZCode',
  kiro: 'Kiro', codebuddy: 'CodeBuddy', proma: 'Proma', catpaw: 'CatPaw', qoder: 'Qoder',
  reasonix: 'Reasonix', lmstudio: 'LM Studio', unsloth: 'Unsloth Studio',
  devin: 'Devin', minimax: 'MiniMax Code', goose: 'Goose',
  unknown: t('web.label.unrecognized_application'),
};

function applicationLabel(event) {
  return APPLICATION_LABELS[event.application] || APPLICATION_LABELS.unknown;
}

function clientUsageGroups(rows, dimension) {
  const groups = new Map();
  rows.forEach(row => {
    const key = row[dimension] || '';
    const group = groups.get(key) || { key, requests: 0, successes: 0, failures: 0, duration_ms: 0 };
    for (const metric of ['requests', 'successes', 'failures', 'duration_ms']) group[metric] += row[metric] || 0;
    groups.set(key, group);
  });
  return [...groups.values()].sort((a, b) => b.requests - a.requests || a.key.localeCompare(b.key));
}

function renderClientsPanel() {
  const tabs = $('#client-tabs');
  const stats = $('#client-stats');
  const note = $('#client-note');
  if (!tabs || !stats) return;
  const all = state.clients.rows || [];
  const applications = clientUsageGroups(all, 'application');
  if (!applications.some(r => r.key === state.clients.application)) state.clients.application = '';
  const rows = all.filter(r => !state.clients.application || r.application === state.clients.application);
  const groups = clientUsageGroups(rows, state.clients.dimension);
  if (!groups.some(r => r.key === state.clients.tab)) state.clients.tab = null;
  const selected = rows.filter(r => state.clients.tab === null || (r[state.clients.dimension] || '') === state.clients.tab);
  const agg = clientUsageGroups(selected, 'total')[0] || { requests: 0, successes: 0, failures: 0, duration_ms: 0 };
  const windowLabel = `${t('web.label.last')} ` + state.clients.windowDays + ` ${t('web.label.days')}`;
  $('#client-scope').textContent = `${t('web.note.search_calls')} ` + windowLabel;
  tabs.innerHTML = `<div class="client-views" role="group" aria-label="${t('web.label.usage_dimension')}">` +
    [['application', t('web.label.application')], ['project', t('web.label.project')]].map(([key, label]) =>
      `<button type="button" class="client-tab${key === state.clients.dimension ? ' active' : ''}" aria-pressed="${key === state.clients.dimension}" data-client-dimension="${key}">${label}</button>`).join('') +
    `</div><label>${t('web.label.window')}<select id="client-window" aria-label="${t('web.label.statistics_window')}">` +
    [1, 7, 30].map(days => `<option value="${days}"${days === state.clients.days ? ' selected' : ''}>${t('web.label.last')} ${days} ${t('web.label.days')}</option>`).join('') +
    `</select></label><label>${t('web.label.application')}<select id="client-application" aria-label="${t('web.label.application_filter')}"><option value="">${t('web.label.all_applications')}</option>` +
    applications.map(r => `<option value="${esc(r.key)}"${r.key === state.clients.application ? ' selected' : ''}>${esc(APPLICATION_LABELS[r.key] || r.key)}</option>`).join('') +
    '</select></label>';
  $$('[data-client-dimension]').forEach(b => b.addEventListener('click', () => {
    state.clients.dimension = b.dataset.clientDimension;
    state.clients.tab = null;
    renderClientsPanel();
  }));
  $('#client-window').addEventListener('change', e => {
    state.clients.days = Number(e.target.value);
    state.clients.tab = null;
    loadClients();
  });
  $('#client-application').addEventListener('change', e => {
    state.clients.application = e.target.value;
    state.clients.tab = null;
    renderClientsPanel();
  });
  if (state.clients.error) {
    stats.innerHTML = '';
    setNote(note, t('web.note.read_failed') + state.clients.error, 'bad');
    return;
  }
  if (state.clients.rows === null || state.clients.loading) {
    stats.innerHTML = `<p class="muted">${t('web.note.loading')}</p>`;
    setNote(note, '');
    return;
  }
  const total = rows.reduce((sum, r) => sum + r.requests, 0);
  const tagged = rows.filter(r => r.project).reduce((sum, r) => sum + r.requests, 0);
  setNote(note, t('web.note.of_calls_attributed_to_projects', { tagged: fmtInt(tagged), total: fmtInt(total) }));
  const avg = agg.requests ? Math.round(agg.duration_ms / agg.requests) : 0;
  const label = key => state.clients.dimension === 'project' ? key || t('web.label.untagged_project') : APPLICATION_LABELS[key] || key;
  const scope = state.clients.tab === null ? windowLabel : label(state.clients.tab) + ' · ' + windowLabel;
  const cards = [
    [t('web.label.successful_calls'), fmtInt(agg.successes), t('web.note.calls', { requests: fmtInt(agg.requests), scope: scope })],
    [t('web.label.failures'), fmtInt(agg.failures), t('web.note.counts_mcp_tool_layer_events_only')],
    [t('web.label.average_response_time'), agg.requests ? fmtMS(avg) : '—', agg.requests ? t('web.note.total_tool_layer_time_calls') : t('web.note.not_computed_without_calls')],
  ];
  let html = '<div class="kpi-grid client-kpis">' + cards.map(c =>
    `<article class="kpi"><header><b>${esc(c[0])}</b></header><strong>${esc(c[1])}</strong><small class="dim">${esc(c[2])}</small></article>`).join('') + '</div>';
  html += '<div class="table-scroll"><table class="data-table client-ranking"><thead><tr><th>' +
    (state.clients.dimension === 'project' ? t('web.label.project') : t('web.label.application')) +
    `</th><th class="num">${t('web.label.calls')}</th><th>${t('web.label.share')}</th><th class="num">${t('web.label.success_rate')}</th></tr></thead><tbody>`;
  html += groups.map(r => {
    const share = total ? r.requests / total * 100 : 0;
    return `<tr${state.clients.tab === r.key ? ' class="selected"' : ''}><td><button type="button" class="client-group" data-client-group="${esc(r.key)}" aria-pressed="${state.clients.tab === r.key}">${esc(label(r.key))}</button></td><td class="num">${fmtInt(r.requests)}</td><td><div class="client-share"><meter min="0" max="100" value="${share}" aria-label="${esc(label(r.key))} ${t('web.label.share_of_calls')}"></meter><span>${share.toFixed(1)}%</span></div></td><td class="num">${(r.requests ? r.successes / r.requests * 100 : 0).toFixed(1)}%</td></tr>`;
  }).join('') || `<tr><td colspan="4" class="empty">${t('web.note.no_calls_in_this_window_yet')}</td></tr>`;
  html += '</tbody></table></div>';
  const tools = clientUsageGroups(selected, 'tool');
  if (tools.length) html += `<div class="table-scroll"><table class="data-table"><thead><tr><th>${t('web.label.mcp_tool')}</th><th class="num">${t('web.label.calls')}</th><th class="num">${t('web.status.success')}</th><th class="num">${t('web.status.failure')}</th><th class="num">${t('web.label.average_latency')}</th></tr></thead><tbody>` +
    tools.map(t => `<tr><td>${esc(DISPLAY[t.key] || t.key)}</td><td class="num">${fmtInt(t.requests)}</td><td class="num">${fmtInt(t.successes)}</td><td class="num">${fmtInt(t.failures)}</td><td class="num">${fmtInt(Math.round(t.duration_ms / t.requests))} ms</td></tr>`).join('') + '</tbody></table></div>';
  stats.innerHTML = html;
  $$('[data-client-group]').forEach(b => b.addEventListener('click', () => {
    state.clients.tab = state.clients.tab === b.dataset.clientGroup ? null : b.dataset.clientGroup;
    renderClientsPanel();
  }));
}

/* ---------- 宽表格粘性横向滚动条 ---------- */

// 溢出的 .table-scroll 后面插入一条代理滚动条（sticky 吸可视区底部），
// 双向同步 scrollLeft：长列表任何位置都能横向滚动，不必拖到表尾。
function refreshHScroll() {
  $$('.table-scroll').forEach(wrap => {
    const overflow = wrap.scrollWidth > wrap.clientWidth + 2;
    let proxy = wrap.__hproxy;
    if (!overflow) {
      if (proxy) proxy.classList.remove('on');
      wrap.classList.remove('hs-nobar');
      return;
    }
    if (!proxy) {
      proxy = document.createElement('div');
      proxy.className = 'hscroll-proxy';
      proxy.appendChild(document.createElement('div'));
      wrap.after(proxy);
      wrap.__hproxy = proxy;
      // 拖代理条期间只做 proxy→wrap 单向同步，禁止回写代理条——
      // 否则瞬时不等时 JS 会跟手指抢滑块（右半段取整误差大，粘滞感最明显）
      let dragging = false;
      let raf = 0;
      proxy.addEventListener('pointerdown', () => { dragging = true; });
      window.addEventListener('pointerup', () => { dragging = false; });
      proxy.addEventListener('scroll', () => {
        if (raf) return;
        raf = requestAnimationFrame(() => {
          raf = 0;
          wrap.scrollLeft = proxy.scrollLeft;
        });
      });
      wrap.addEventListener('scroll', () => {
        if (wrap.closest('#page-sources')) $$('#page-sources .table-scroll').forEach(other => {
          if (other.scrollLeft !== wrap.scrollLeft) other.scrollLeft = wrap.scrollLeft;
        });
        if (dragging || proxy.scrollLeft === wrap.scrollLeft) return;
        proxy.scrollLeft = wrap.scrollLeft;
      });
    }
    proxy.firstElementChild.style.width = wrap.scrollWidth + 'px';
    proxy.classList.add('on');
    // 代理条接管横向滚动，隐藏容器原生条（防同表双条）
    wrap.classList.add('hs-nobar');
  });
}

/* ---------- navigation & wiring ---------- */

function nav(page, push) {
  if (page === 'events') page = 'usage';
  if (!PAGES[page]) page = 'overview';
  if ($('.page.active')?.id !== 'page-' + page) window.scrollTo(0, 0);
  $$('.page').forEach(el => el.classList.toggle('active', el.id === 'page-' + page));
  $$('.nav').forEach(el => el.classList.toggle('active', el.dataset.page === page));
  $('#page-title').textContent = PAGES[page];
  if (push !== false) history.replaceState(null, '', '#' + page);
  // 隐藏页的表格尺寸为 0，切页后重新判定溢出并挂粘性滚动条
  refreshHScroll();
}

function wire() {
  $$('[data-icon]').forEach(el => { el.innerHTML = icon(el.dataset.icon); });
  window.addEventListener('hashchange', () => nav(location.hash.slice(1), false));
  $$('.nav').forEach(btn => btn.addEventListener('click', () => nav(btn.dataset.page)));
  $$('[data-jump]').forEach(link => link.addEventListener('click', e => { e.preventDefault(); nav(link.dataset.jump); }));
  $('#refresh').addEventListener('click', () => loadAll());
  $('#reload-events').addEventListener('click', () => loadEvents());
  $('#export-csv').addEventListener('click', exportCSV);
  $('#save-settings').addEventListener('click', saveSettings);
  $('#save-sources').addEventListener('click', saveSettings);
  $('#undo-settings').addEventListener('click', discardDraft);
  $('#undo-sources').addEventListener('click', discardDraft);
  $('#retry-apply').addEventListener('click', restartAndVerify);
  $('#restore-backup').addEventListener('click', restoreBackup);
  $('#backup-select').addEventListener('change', renderWriteAccess);
  $('#page-sources').addEventListener('change', e => {
    if (e.target.dataset.sourceSetting) {
      setDraft(e.target.dataset.sourceSetting, e.target.value);
      renderSettingsForm();
      renderSourcePages();
      return;
    }
    const el = e.target.closest('[data-source-key]');
    if (!el) return;
    setDraft(el.dataset.sourceKey, el.dataset.inverted === 'true' ? !el.checked : el.checked);
    renderSettingsForm();
    renderSourcePages();
  });

  // 外观主题：选择仅存本浏览器，立即生效
  $$('#theme-picker [data-theme-pick]').forEach(btn => btn.addEventListener('click', () => {
    localStorage.setItem('theme', btn.dataset.themePick);
    applyLocalTheme();
  }));
  $('#accent-input').addEventListener('change', () => {
    const v = $('#accent-input').value.trim();
    if (v === '') {
      localStorage.removeItem('accent');
      document.documentElement.style.removeProperty('--accent');
      toast(t('web.msg.accent_restored_to_the_theme_default'));
      return;
    }
    if (!/^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$/.test(v)) {
      toast(t('web.msg.invalid_accent_format_example_14633f'), true);
      return;
    }
    localStorage.setItem('accent', v.toLowerCase());
    document.documentElement.style.setProperty('--accent', v.toLowerCase());
    toast(t('web.msg.custom_accent_applied'));
  });

  // 额度管理操作（事件委托）
  const quotaBody = $('#quota-manage-body');
  if (quotaBody) {
    quotaBody.addEventListener('click', e => {
      const resetBtn = e.target.closest('[data-quota-reset]');
      if (resetBtn) {
        quotaReset(resetBtn.dataset.quotaReset);
        return;
      }
      const adjustBtn = e.target.closest('[data-quota-adjust]');
      if (adjustBtn) {
        quotaAdjust(adjustBtn.dataset.quotaAdjust, Number(adjustBtn.dataset.used) || 0);
      }
    });
  }
  $('#restart-service').addEventListener('click', async () => {
    if (!(await confirmDialog(t('web.action.restart_websearch'), t('web.dialog.the_current_request_is_briefly_interrupted_and_docker_restar')))) return;
    try {
      state.applyResult = { targets: { ...(state.settings.pending_changes || {}) } };
      await restartAndVerify();
    } catch (e) {
      toast(e.message, true);
    }
  });
  $('#clear-cache').addEventListener('click', async () => {
    if (!(await confirmDialog(t('web.action.clear_search_cache'), t('web.dialog.this_cannot_be_undone_but_monitoring_history_is_not_deleted')))) return;
    try {
      const res = await securePost('/__admin/api/cache/clear', { confirm: true });
      toast(`${t('web.msg.cleared')} ` + fmtInt(res.cleared) + ` ${t('web.note.cached')}`);
    } catch (e) {
      toast(e.message, true);
    }
  });
  $$('[data-key]').forEach(el => el.addEventListener('input', markDirty));
  $('#mode-single').addEventListener('change', changeSingleProvider);
  $('#mode-provider').addEventListener('change', changeSingleProvider);
  $$('.mode-info').forEach(button => {
    button.addEventListener('click', () => {
      const expanded = button.getAttribute('aria-expanded') !== 'true';
      setModeHelp(button, expanded, !expanded);
    });
    button.addEventListener('focus', () => button.parentElement.classList.remove('dismissed'));
    button.addEventListener('mouseenter', () => button.parentElement.classList.remove('dismissed'));
    button.addEventListener('blur', () => setModeHelp(button, false));
  });
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') $$('.mode-info').forEach(button => setModeHelp(button, false, true));
  });
  window.addEventListener('beforeunload', e => {
    if (!state.settingsDirty) return;
    e.preventDefault();
    e.returnValue = '';
  });
  const applyFilter = () => {
    state.filters.status = $('#filter-status').value;
    state.filters.errorKind = $('#filter-error-kind').value;
    state.filters.tool = $('#filter-tool').value;
    state.filters.provider = $('#filter-provider').value;
    loadEvents();
  };
  ['#filter-status', '#filter-error-kind', '#filter-tool', '#filter-provider'].forEach(sel => {
    $(sel).addEventListener('change', applyFilter);
  });
  $('#filter-status').value = state.filters.status;
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && Date.now() - state.lastLoad > REFRESH_MS) loadAll();
  });
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(refreshHScroll, 120);
  });
}

function applyLocalTheme() {
  const brand = (state.overview && state.overview.brand) || {};
  applyBrand({ ...brand, theme: localStorage.getItem('theme') || brand.theme });
}

// Remove credentials retained by older frontend versions; new credentials live only in memory.
sessionStorage.removeItem('adminPass');
sessionStorage.removeItem('adminUser');
applyI18n(document);
wire();
renderWriteAccess();
applyLocalTheme();
$('#accent-input').value = localStorage.getItem('accent') || '';
nav(location.hash.slice(1) || 'overview', false);
loadAll();
setInterval(() => { if (!document.hidden) loadAll(); }, REFRESH_MS);
