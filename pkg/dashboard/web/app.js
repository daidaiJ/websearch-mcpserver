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
const PAGES = { overview: t('web.page.overview'), sources: t('web.page.search_sources'), tools: t('web.page.tools_capabilities'), usage: t('web.page.call_log'), settings: t('web.page.settings') };
const HEALTH = { healthy: t('web.status.healthy'), degraded: t('web.status.degraded'), down: t('web.status.failing'), unknown: t('web.status.unknown') };
const ERROR_KINDS = {
  rate_limit: t('web.status.rate_limited'), captcha: t('web.status.captcha'), access_denied: t('web.status.access_denied'), timeout: t('web.status.timeout'),
  parse: t('web.status.parse_failed'), no_result: t('web.status.no_results'), network: t('web.status.network_server'), unknown: t('web.status.unknown_error'),
};
const SYSTEM = { healthy: t('web.status.running_normally'), degraded: t('web.status.partially_degraded'), down: t('web.status.partially_failing'), waiting: t('web.status.awaiting_real_calls') };
const DOT = { healthy: 'healthy', degraded: 'degraded', down: 'down', unknown: 'unknown' };
const DISPLAY = {
  smartsearch: t('web.name.smart_search'), academicsearch: t('web.name.academic_search'), cleanfetch: t('web.name.content_fetch'), pdf_parser: t('web.name.pdf_parsing'), file_search: t('web.name.local_file_search'), search_history: t('web.storage.history'),
  anysearch: 'AnySearch', baidu: t('web.name.baidu_qianfan'), baidu_api: t('web.name.baidu_qianfan'), baidu_ai: t('web.name.baidu_qianfan'), tavily: 'Tavily', tavily_api: 'Tavily', tinyfish: 'TinyFish', tinyfish_api: 'TinyFish', exa: 'Exa', doubao: t('web.name.doubao_search'), keenable: 'Keenable', keenable_api: 'Keenable', yuanbao: t('web.name.yuanbao'), tencent_yuanbao: t('web.name.yuanbao'), mcphub: 'MCPHub',
  baidu_web: t('web.name.baidu_web'), bing: 'Bing', google: 'Google', duckduckgo: 'DuckDuckGo', wikipedia: 'Wikipedia', googlenews: 'Google News', github: 'GitHub', mojeek: 'Mojeek',
  arxiv: 'arXiv', crossref: 'Crossref', openalex: 'OpenAlex', pubmed: 'PubMed', europepmc: 'Europe PMC',
  dblp: 'DBLP', doaj: 'DOAJ', semantic_scholar: 'Semantic Scholar', google_scholar: 'Google Scholar',
  webfetch: t('web.name.web_fetcher'), jina: 'Jina Reader', pdf_pipeline: t('web.name.pdf_pipeline'),
};
const SECRETS = {
  BAIDU_SK: [t('web.name.baidu_qianfan'), 'https://console.bce.baidu.com/iam/#/iam/apikey/list'],
  TAVILY_SK: ['Tavily', 'https://app.tavily.com/home'],
  EXA_API_KEY: ['Exa', 'https://dashboard.exa.ai/api-keys'],
  ANYSEARCH_API_KEY: ['AnySearch', 'https://www.anysearch.com/console/api-keys'],
  DOUBAO_SEARCH_API_KEY: [t('web.name.doubao'), 'https://console.volcengine.com/search-infinity/api-key'],
  TENCENTCLOUD_WSA_APIKEY: [t('web.name.yuanbao'), 'https://console.cloud.tencent.com/wsapi/index'],
  TINYFISH_API_KEY: ['TinyFish', 'https://agent.tinyfish.ai/sign-up'],
  KEENABLE_API_KEY: ['Keenable', 'https://app.keenable.ai/console'],
  GITHUB_TOKEN: ['GitHub', 'https://github.com/settings/tokens'],
  JINA_API_KEY: ['Jina Reader', 'https://jina.ai/api-keys'],
  MINERU_TOKEN: ['MinerU', 'https://mineru.net/apiManage/token'],
  LLM_API_KEY: ['LLM', 'https://platform.openai.com/api-keys', t('web.credential.openai_example')],
  SEMANTIC_SCHOLAR_API_KEY: ['Semantic Scholar', 'https://webflow.semanticscholar.org/product/api'],
  OPENALEX_API_KEY: ['OpenAlex', 'https://help.openalex.org/api/authentication/'],
  UNPAYWALL_EMAIL: ['Unpaywall', 'https://data.unpaywall.org/products/api', t('web.credential.email_documentation')],
  WEBSEARCH_TOKEN: [t('web.credential.business_auth')],
};
const SECRET_ACTIONS = {
  append: t('web.credential.append'), set: t('web.credential.set'), delete: t('web.credential.delete'),
  label: t('web.credential.edit_label'), clear: t('web.credential.clear'), reset: t('web.credential.reset'),
};
const SECRET_API = '/__admin/api/secrets';

const state = {
  overview: null, overviewError: null, overviewStale: false, overviewRequest: 0,
  quotas: null, quotaError: null, quotaRequest: 0, runtimeAction: '',
  clients: { rows: null, error: null, tab: null, dimension: 'application', application: '', days: 7, windowDays: 7, loading: false, request: 0 },
  settings: null, settingsError: null, settingsDirty: false, settingsLoaded: false,
  storage: { data: null, error: null, clearing: false, request: 0 },
  draft: {}, draftBefore: {}, invalidDraft: {}, openSources: new Set(), draftRevision: '', saving: false, applying: false, applyResult: null, adminPass: '', adminUser: '',
  events: { rows: null, error: null, loading: false, exporting: false, expanded: new Set(), details: new Map() },
  filters: { status: '', errorKind: '', tool: '', provider: '', application: '', project: '', cacheHit: '' },
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
  return list.map(item =>
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
    const error = new Error(msg);
    error.status = res.status;
    throw error;
  }
  if (body === null) throw new Error(t('web.msg.response_is_not_valid_json'));
  return body;
}

/* ---------- loaders (each panel fails on its own) ---------- */

async function loadOverview() {
  const request = ++state.overviewRequest;
  try {
    const data = await fetchJSON('/__admin/api/overview');
    if (request !== state.overviewRequest) return;
    state.overview = data;
    // The source catalog is the schema this page is built around; older
    // payloads are shown as-is but flagged so they are never read as current.
    state.overviewStale = !(data && data.system && Array.isArray(data.sources));
    state.overviewError = null;
    if (data && data.brand) applyBrand(data.brand);
  } catch (e) {
    if (request !== state.overviewRequest) return;
    state.overviewError = e.message;
  }
  renderSystem();
  renderToolKpis();
  renderRuntime();
  renderCallTrend();
  renderMCPTools();
  renderCapabilityStates();
  renderDiagnosticControls();
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
  if (state.runtimeAction === 'quota') return;
  const request = ++state.quotaRequest;
  try {
    const data = await fetchJSON('/__admin/api/quotas');
    if (request !== state.quotaRequest) return;
    state.quotas = Array.isArray(data) ? data : [];
    state.quotaError = null;
  } catch (e) {
    if (request !== state.quotaRequest) return;
    state.quotaError = e.message;
  }
  renderQuotas();
}

function renderQuotas() {
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
    if ((Object.keys(pending).length || data.credential_status?.pending_restart) && !state.applying && !state.saving && (!state.applyResult || !state.applyResult.failed)) {
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
  if (f.application) params.set('application', f.application);
  if (f.project) params.set('project', f.project);
  if (f.cacheHit) params.set('cache_hit', f.cacheHit);
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

function uptimeLabel(seconds) {
  const value = num(seconds);
  if (value === null || value < 0) return '—';
  const days = Math.floor(value / 86400);
  const hours = Math.floor(value % 86400 / 3600);
  const minutes = Math.floor(value % 3600 / 60);
  if (days) return t('web.runtime.uptime_days', { days, hours });
  if (hours) return t('web.runtime.uptime_hours', { hours, minutes });
  if (minutes) return t('web.runtime.uptime_minutes', { minutes, seconds: value % 60 });
  return value + ' ' + t('web.label.seconds');
}

function renderRuntime() {
  const list = $('#runtime-list');
  if (!list) return;
  const runtime = state.overview && state.overview.runtime;
  if (!runtime) {
    list.innerHTML = '<div><dd>—</dd></div>';
    return;
  }
  const onOff = value => value ? t('web.label.on') : t('web.label.off_2');
  const proxy = runtime.proxy;
  const mode = {
    auto: t('web.runtime.proxy_auto'),
    explicit: t('web.runtime.proxy_explicit'),
    disabled: t('web.runtime.proxy_disabled'),
  }[proxy.mode];
  const route = proxy.mode === 'disabled' ? mode : [mode, proxy.resolved ? t('web.runtime.proxy_resolved') : t('web.runtime.proxy_unresolved')].join(' · ');
  const llm = runtime.llm;
  const hub = runtime.mcp_hub;
  const servers = hub.servers.length ? hub.servers.map(server => server.enabled ? server.name :
    t('web.runtime.server_disabled', { name: server.name })).join(' → ') : '—';
  const fetchMode = {
    off: t('web.label.off_2'),
    prefer: t('web.runtime.hub_fetch_prefer'),
    fallback: t('web.runtime.hub_fetch_fallback'),
  }[hub.fetch_mode] || hub.fetch_mode;
  const rows = [
    [t('web.runtime.version'), runtime.version],
    [t('web.runtime.uptime'), uptimeLabel(runtime.uptime_seconds)],
    [t('web.runtime.mcp_stateless'), onOff(runtime.mcp_stateless)],
    [t('web.runtime.mcp_resources'), onOff(runtime.mcp_resources)],
    [t('web.runtime.searxng'), runtime.searxng_enabled ? t('web.label.on') : t('web.runtime.removed')],
    [t('web.runtime.proxy'), route],
    [t('web.runtime.proxy_api_providers'), onOff(proxy.api_providers)],
    [t('web.runtime.llm_summary'), [onOff(llm.enabled), llm.model_id, llm.timeout_seconds + ' ' + t('web.label.seconds')].filter(Boolean).join(' · ')],
    ['MCPHub', onOff(hub.enabled)],
    [t('web.runtime.hub_fetch'), fetchMode],
    [t('web.runtime.hub_servers'), servers],
  ];
  list.innerHTML = rows.map(([label, value]) => `<div><dt>${esc(label)}</dt><dd>${esc(value || '—')}</dd></div>`).join('');
}

function renderCallTrend() {
  const chart = $('#call-trend');
  if (!chart) return;
  const days = state.overview && state.overview.trend;
  if (!Array.isArray(days) || !days.length) {
    chart.innerHTML = `<p class="panel-note">${esc(t('web.note.no_tool_layer_events_yet'))}</p>`;
    return;
  }
  const series = [
    ['successes', 'success', t('web.status.success')],
    ['failures', 'failure', t('web.status.failure')],
    ['cache_hits', 'cache', t('web.trend.cache_hits')],
  ];
  const daily = new Map(days.map(day => [day.day, day]));
  const startDay = Date.parse(days[0].day);
  const endDay = Date.parse(days[days.length - 1].day);
  const dayMS = 86400000;
  const count = Math.min(14, Math.round((endDay - startDay) / dayMS) + 1);
  const max = Math.max(2, Math.ceil(Math.max(...days.flatMap(day => series.map(([key]) => day[key] || 0))) / 2) * 2);
  const left = 47;
  const top = 14;
  const height = 144;
  const bottom = top + height;
  const width = 612;
  const step = width / count;
  const bar = Math.min(16, step / 4);
  const grid = [0, max / 2, max].map(value => {
    const y = bottom - value / max * height;
    return `<line x1="${left}" y1="${y}" x2="${left + width}" y2="${y}"/><text x="${left - 6}" y="${y + 4}" text-anchor="end">${fmtInt(value)}</text>`;
  }).join('');
  const groups = Array.from({ length: count }, (_, index) => {
    const day = new Date(endDay - (count - 1 - index) * dayMS).toISOString().slice(0, 10);
    const row = daily.get(day) || {};
    const x = left + step * (index + 0.5);
    const title = [day, ...series.map(([key, , label]) => label + ' ' + fmtInt(row[key] || 0))].join(' · ');
    return `<g><title>${esc(title)}</title>${series.map(([key, style], position) => {
      const size = (row[key] || 0) / max * height;
      return `<rect class="trend-${style}" x="${x + (position - 1.5) * bar}" y="${bottom - size}" width="${bar - 2}" height="${size}" rx="1"/>`;
    }).join('')}<rect class="trend-target" x="${x - step / 2}" y="${top}" width="${step}" height="${height}"/>${(count <= 7 || index % 3 === 0 || index === count - 1) ? `<text x="${x}" y="${bottom + 18}" text-anchor="middle">${day.slice(5)}</text>` : ''}</g>`;
  }).join('');
  chart.innerHTML = `<svg class="call-trend" viewBox="0 0 675 184" role="img" aria-label="${esc(t('web.page.call_trend'))}">${grid}${groups}</svg>` +
    `<ul class="trend-legend">${series.map(([, style, label]) => `<li><i class="trend-${style}" aria-hidden="true"></i>${esc(label)}</li>`).join('')}</ul>`;
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
    const registration = tool.registration_state || tool.state;
    const registrationLabel = tool.registered ? t('web.status.registered') : ({
      unprobed: t('web.status.not_probed'),
      pending: t('web.status.awaiting_registration'),
      unavailable: t('web.status.unavailable'),
    })[registration] || t('web.status.not_registered');
    const stateLabel = tool.enabled ? registrationLabel :
      t('web.status.disabled_in_config') + (registration === 'unprobed' ? ` ${t('web.note.not_probed_yet')}` : ` · ${registrationLabel}`);
    const hasObservation = health && health.last_seen_at;
    const observationLabel = hasObservation ? (HEALTH[health.status] || t('web.status.unknown')) : t('web.status.no_observations');
    const dotClass = hasObservation ? (DOT[health.status] || 'unknown') : 'unknown';
    const stats = health && health.today
      ? `<small>${fmtInt(health.today.requests)} ${t('web.note.text')} ${fmtInt(health.today.successes)} ${t('web.note.success')} ${fmtInt(health.today.failures)} ${t('web.status.failure')}</small>`
      : `<small>${t('web.note.no_tool_layer_events_yet')}</small>`;
    const pdf = tool.pdf ? `<small class="pdf-params">${t('web.label.default_pages')} ${esc(tool.pdf.max_pages)} ${t('web.note.page_limit_per_task')} ${esc(tool.pdf.mineru_page_limit)} ${t('web.note.batch_size')} ${esc(tool.pdf.mineru_page_batch_size)} ${t('web.note.call_budget')} ${esc(tool.pdf.mineru_page_budget)} · OCR ${tool.pdf.ocr_enabled ? t('web.label.on') : t('web.label.off_2')}${tool.pdf.page_progress === 'not_collected' ? t('web.note.page_progress_not_collected') : ''}</small>` : '';
    const file = tool.file ? `<small class="pdf-params">${t('web.label.allowlisted_roots')} ${esc(tool.file.allowed_root_count)} · ${tool.file.whitelist_enforced ? t('web.status.allowlist_enforced') : t('web.status.directories_unrestricted')}${tool.file.path_metadata_only ? t('web.note.path_metadata_only') : ''}</small>` : '';
    const fetch = typeof tool.fetch_enabled === 'boolean' ? `<small class="pdf-params">${t('web.note.saved_content_reads_available_url_fetching')}${tool.fetch_enabled ? t('web.label.on') : t('web.label.off_2')}</small>` : '';
    const history = tool.history ? `<small class="pdf-params">${esc(t('web.note.history_retention', { days: fmtInt(tool.history.retention_days) }))}</small>` : '';
    return `<div class="tool-row"><span class="tool-head">${dot(dotClass)}` +
      `<b>${esc(tool.label || displayName(tool.name))}</b><small class="mono">${esc(tool.name)}</small></span>` +
      `<span class="tool-state">${esc(stateLabel)}<small class="tool-observation">${esc(observationLabel)}</small></span>` +
      `<p class="tool-stats">${stats}${pdf}${file}${fetch}${history}</p></div>`;
  }).join('');
}


/* ---------- configuration controls ---------- */

const FIELD_TYPES = {
  'smartsearch.max_size': ['web.label.max_results', 'number', 0, 360],
  'smartsearch.relevance_threshold': ['web.label.relevance_threshold', 'decimal', 0, 1],
  'smartsearch.fetch_top_n': ['web.config.fetch_top_n', 'number', 0, 5],
  'smartsearch.enhance': ['web.config.enhance', 'checkbox'],
  'smartsearch.show_meta': ['web.config.show_meta', 'checkbox'],
  'smartsearch.off_topic_guard': ['web.config.off_topic_guard', 'select', ['shadow', 'enforce', 'off']],
  'smartsearch.engine_timeout_sec': ['web.config.engine_timeout', 'number', -1, 3600],
  'smartsearch.inline_max_chars': ['web.config.inline_max_chars', 'number', -1, 10000000],
  'smartsearch.mmr.enabled': ['web.config.mmr_enabled', 'checkbox'],
  'smartsearch.mmr.lambda': ['web.config.mmr_lambda', 'decimal', 0, 1],
  'smartsearch.mmr.target_count': ['web.config.mmr_target', 'number', 0, 360],
  'black_list_host': ['web.config.blocked_hosts', 'hosts'],
  'cleanfetch.enabled': ['web.label.url_fetching', 'checkbox'],
  'cleanfetch.auto_proxy': ['web.label.fetch_through_the_system_proxy', 'checkbox'],
  'mcp_hub.fetch_mode': ['web.config.fetch_mode', 'select', ['off', 'fallback', 'prefer']],
  'cleanfetch.timeout_sec': ['web.config.timeout', 'number', 1, 3600],
  'cleanfetch.max_fetch_size_mb': ['web.config.max_fetch_mb', 'number', 1, 2048],
  'cleanfetch.max_inline_lines': ['web.config.max_inline_lines', 'number', 1, 100000],
  'cleanfetch.max_inline_chars': ['web.config.inline_max_chars', 'number', 0, 10000000],
  'cleanfetch.file_ttl_hours': ['web.config.file_ttl', 'number', 1, 8760],
  'cleanfetch.max_retries': ['web.config.retries', 'number', 1, 10],
  'pdf_parser.enabled': ['web.name.pdf_parsing', 'checkbox'],
  'pdf_parser.mineru_model': ['web.config.mineru_model', 'select', ['pipeline','vlm']],
  'pdf_parser.mineru_ocr': ['web.config.mineru_ocr', 'checkbox'],
  'pdf_parser.mineru_formula': ['web.config.mineru_formula', 'checkbox'],
  'pdf_parser.mineru_table': ['web.config.mineru_table', 'checkbox'],
  'pdf_parser.mineru_lang': ['web.config.mineru_language', 'text'],
  'pdf_parser.mineru_remote_pdf': ['web.config.mineru_remote', 'checkbox'],
  'pdf_parser.max_pages': ['web.label.default_pages', 'number', 1, 10000],
  'pdf_parser.mineru_page_limit': ['web.config.mineru_page_limit', 'number', 1, 10000],
  'pdf_parser.mineru_page_batch_size': ['web.config.mineru_batch_size', 'number', 0, 200],
  'pdf_parser.mineru_page_budget': ['web.config.mineru_page_budget', 'number', 0, 100000],
  'academic.enabled': ['web.name.academic_search', 'checkbox'],
  'academic.enhance': ['web.config.enhance', 'checkbox'],
  'academic.threshold': ['web.label.relevance_threshold', 'decimal', 0, 1],
  'search_history.enabled': ['web.config.history_enabled', 'checkbox'],
  'search_history.retention_days': ['web.config.retention', 'number', 1, 36500],
  'llm.base_url': ['web.config.base_url', 'text'],
  'llm.model_id': ['web.config.model', 'text'],
  'llm.timeout_sec': ['web.config.timeout', 'number', 1, 3600],
  'llm.summary_progress_interval': ['web.config.progress_interval', 'number', 1, 3600],
  'everything.max_results': ['web.label.max_results', 'number', 1, 20],
  'everything.timeout_sec': ['web.config.timeout', 'number', 1, 3600],
  'everything.min_alignment': ['web.config.min_alignment', 'decimal', 0, 1],
  'apipool.strategy': ['web.config.strategy', 'select', ['round-robin','priority','weighted']],
  'apipool.engines': ['web.config.pool_order', 'ordered'],
  'apipool.weights': ['web.config.pool_weights', 'weights-map'],
  'mcp_hub.strategy': ['web.config.strategy', 'select', ['priority','round-robin']],
  'mcp_hub.timeout_sec': ['web.config.timeout', 'number', 1, 3600],
  'mcp_hub.fetch_timeout_sec': ['web.config.fetch_timeout', 'number', 1, 3600],
  'mcp_hub.cooldown_sec': ['web.config.cooldown', 'number', 1, 86400],
  'mcp_hub.rotation_engines': ['web.config.rotation_engines', 'ordered'],
  'mcp_hub.fallback_engines': ['web.config.fallback_engines', 'ordered'],
  'proxy.enabled': ['web.config.proxy_mode', 'tri-state', [null, true, false]],
  'proxy.endpoint': ['web.config.proxy_endpoint', 'text'],
  'proxy.api_providers': ['web.config.api_proxy', 'checkbox'],
  'rate_limit.per_sec': ['web.config.per_second', 'number', 1, 100000],
  'rate_limit.per_min': ['web.config.per_minute', 'number', 1, 1000000],
  'language': ['web.config.language', 'select', ['auto','zh','en']],
  'log_level': ['web.config.log_level', 'select', ['trace','debug','info','warn','error','fatal','panic','disabled']],
  'dashboard.retention_days': ['web.config.telemetry_retention', 'number', 1, 36500],
  'dashboard.client_projects': ['web.config.client_projects', 'text-map'],
  'dashboard.brand.title': ['web.config.brand_title', 'text'],
  'dashboard.brand.theme': ['web.config.brand_theme', 'select', ['green','blue','mono']],
  'dashboard.brand.accent': ['web.config.brand_accent', 'text'],
  'dashboard.brand.footer': ['web.config.brand_footer', 'checkbox'],
  'dashboard.quotas.reset': ['web.config.quota_reset', 'select', ['monthly','weekly','daily','none']],
  'dashboard.quotas.reset_day': ['web.config.quota_reset_day', 'number', 1, 28],
  'dashboard.quotas.limits': ['web.config.quota_limits', 'numbers-map'],
  'dashboard.quotas.enforce_providers': ['web.config.quota_enforced_providers', 'checklist'],
  'yuanbao.monthly_limit': ['web.config.yuanbao_limit', 'number', 0, 10000000],
  'yuanbao.mode': ['web.label.search_mode', 'numeric-select', [0,1,2]],
  'yuanbao.industry': ['web.config.industry', 'select', ['', 'gov','news','acad','finance']],
  'yuanbao.site': ['web.config.site', 'text'],
  'doubao.version': ['web.label.doubao_api_version', 'select', ['global','custom']],
  'doubao.icp_host_only': ['web.config.icp_only', 'checkbox'],
  'doubao.need_content': ['web.config.include_text', 'checkbox'],
  'doubao.auth_level': ['web.config.auth_level', 'numeric-select', [0,1]],
  'doubao.query_rewrite': ['web.config.rewrite_query', 'checkbox'],
  'doubao.num_results': ['web.label.max_results', 'number', 1, 50],
  'baidu.enable_ai_search': ['web.config.baidu_ai', 'checkbox'],
  'baidu.model': ['web.config.model', 'text'],
  'baidu.search_mode': ['web.label.search_mode', 'select', ['auto','required','disabled']],
  'tavily.include_raw_content': ['web.config.include_text', 'checkbox'],
  'exa.include_text': ['web.config.include_text', 'checkbox'],
  'exa.lookback_days': ['web.config.lookback_days', 'number', 1, 36500],
  'exa.num_results': ['web.label.max_results', 'number', 1, 100],
  'keenable.live': ['web.config.live', 'checkbox'],
  'wikipedia.lang': ['web.config.language', 'text'],
  'google_news.edition': ['web.config.edition', 'select', ['zh-CN','zh-TW','en-US','en-GB','ja-JP','ko-KR']],
  'so360.safe_search': ['web.config.safe_search', 'numeric-select', [0,1,2]],
};

const TOOL_CONFIGS = [
  ['smartsearch', 'web.capability.smartsearch', 'web.capability.smartsearch_help',
    ['smartsearch.enhance','smartsearch.max_size','black_list_host'],
    ['smartsearch.fetch_top_n','smartsearch.show_meta','smartsearch.off_topic_guard','smartsearch.engine_timeout_sec','smartsearch.inline_max_chars','smartsearch.relevance_threshold','smartsearch.mmr.enabled','smartsearch.mmr.lambda','smartsearch.mmr.target_count']],
  ['cleanfetch', 'web.capability.cleanfetch', 'web.capability.cleanfetch_help',
    ['cleanfetch.enabled','mcp_hub.fetch_mode','cleanfetch.timeout_sec'],
    ['cleanfetch.auto_proxy','cleanfetch.max_fetch_size_mb','cleanfetch.max_inline_lines','cleanfetch.max_inline_chars','cleanfetch.file_ttl_hours','cleanfetch.max_retries']],
  ['pdf_parser', 'web.capability.pdf', 'web.capability.pdf_help',
    ['pdf_parser.enabled','pdf_parser.mineru_model','pdf_parser.mineru_ocr'],
    ['pdf_parser.mineru_formula','pdf_parser.mineru_table','pdf_parser.mineru_lang','pdf_parser.mineru_remote_pdf','pdf_parser.max_pages','pdf_parser.mineru_page_limit','pdf_parser.mineru_page_batch_size','pdf_parser.mineru_page_budget']],
  ['academicsearch', 'web.capability.academic', 'web.capability.academic_help',
    ['academic.enabled','academic.enhance'], ['academic.threshold']],
  ['search_history', 'web.capability.history', 'web.capability.history_help',
    ['search_history.enabled','search_history.retention_days'], []],
  ['llm', 'web.capability.llm', 'web.capability.llm_help',
    ['llm.model_id','llm.base_url'], ['llm.timeout_sec','llm.summary_progress_interval']],
  ['file_search', 'web.capability.everything', 'web.capability.everything_help',
    ['everything.max_results'], ['everything.timeout_sec','everything.min_alignment']],
];

const SOURCE_CONFIGS = {
  yuanbao: ['yuanbao.monthly_limit','yuanbao.mode','yuanbao.industry','yuanbao.site'],
  doubao: ['doubao.version','doubao.icp_host_only','doubao.need_content','doubao.auth_level','doubao.query_rewrite','doubao.num_results'],
  baidu: ['baidu.enable_ai_search','baidu.model','baidu.search_mode'],
  tavily: ['tavily.include_raw_content'],
  exa: ['exa.include_text','exa.lookback_days','exa.num_results'],
  keenable: ['keenable.live'],
  wikipedia: ['wikipedia.lang'],
  googlenews: ['google_news.edition'],
  so360: ['so360.safe_search'],
};

function configField(key) {
  if (FIELD_TYPES[key]) return FIELD_TYPES[key];
  const score = /^smartsearch\.engines\.[a-z0-9_]+\.(weight|max_size|min_score)$/.exec(key);
  if (!score) return null;
  if (score[1] === 'weight') return ['web.config.engine_weight', 'decimal', 0, 1000];
  if (score[1] === 'max_size') return ['web.label.max_results', 'number', 0, 360];
  return ['web.config.engine_min_score', 'decimal', 0, 1];
}

function configFieldHtml(key) {
  const [labelKey, type, limits, max] = configField(key);
  const label = t(labelKey);
  const attrs = ' data-key="' + esc(key) + '" data-config-type="' + type + '"';
  const title = '<span>' + esc(label) + '</span>';
  if (type === 'checkbox') {
    return '<label class="settings-toggle form-check form-switch"><input type="checkbox" class="form-check-input" role="switch"' + attrs + '>' + title + '</label>';
  }
  if (type === 'ordered' || type === 'checklist') {
    const help = type === 'checklist' ? '<p class="panel-note">' + esc(t('web.config.quota_enforced_help')) + '</p>' : '';
    return '<fieldset class="ordered-field settings-wide"><legend>' + esc(label) + '</legend>' + help + '<div data-ordered-field="' + esc(key) + '"></div></fieldset>';
  }
  if (['select', 'tri-state', 'numeric-select'].includes(type)) {
    const optionLabels = type === 'tri-state'
      ? [t('web.config.proxy_auto'), t('web.config.proxy_explicit'), t('web.config.proxy_disabled')] : limits;
    const choices = limits.map((value, index) => '<option value="' + esc(String(value)) + '">' + esc(optionLabels[index] || t('web.config.any_value')) + '</option>');
    return '<label>' + title + '<select' + attrs + '>' + choices.join('') + '</select></label>';
  }
  if (type === 'hosts' || type.endsWith('-map')) {
    const help = type === 'hosts' ? t('web.config.host_list_help') : t('web.config.key_value_help');
    return '<label class="settings-wide">' + title + '<textarea rows="3" spellcheck="false" aria-describedby="help-' + esc(key) + '"' + attrs + '></textarea><small id="help-' + esc(key) + '">' + esc(help) + '</small></label>';
  }
  const numeric = type === 'number' || type === 'decimal';
  const range = (limits === undefined ? '' : ' min="' + limits + '"') + (max === undefined ? '' : ' max="' + max + '"');
  return '<label>' + title + '<input type="' + (numeric ? 'number' : 'text') + '"' + attrs + range + (numeric ? ' step="' + (type === 'decimal' ? 'any' : '1') + '" required' : ' spellcheck="false"') + '></label>';
}

function configFields(keys) {
  return '<div class="form-grid">' + keys.map(configFieldHtml).join('') + '</div>';
}

function advancedFields(keys) {
  return '<details class="settings-advanced config-advanced"><summary>' +
    esc(t('web.config.advanced')) + '</summary>' + configFields(keys) + '</details>';
}

function mountConfigControls() {
  $('#tools-directory').innerHTML = TOOL_CONFIGS.map(([id, label]) =>
    '<button type="button" data-jump-section="capability-' + id + '">' + esc(t(label)) + '</button>').join('');
  $('#tool-controls').innerHTML = TOOL_CONFIGS.map(([id, label, description, common, advanced]) => {
    const scope = id === 'file_search'
      ? '<button id="everything-scope-button" type="button" class="scope-button" aria-expanded="false" aria-controls="everything-scope">' + esc(t('web.config.show_scope')) + '</button><button id="diagnostic-everything" type="button" class="scope-button" disabled>' + esc(t('web.diagnostics.reprobe_everything')) + '</button><p id="diagnostic-everything-result" class="panel-note" role="status"></p><div id="everything-scope" aria-live="polite"></div>'
      : '';
    return '<article id="capability-' + id + '" class="panel capability"><div class="panel-head"><h2>' +
      esc(t(label)) + '</h2><span class="muted capability-state" data-tool-state="' + id + '"></span></div><p class="panel-note">' +
      esc(t(description)) + '</p>' + configFields(common) + (advanced.length ? advancedFields(advanced) : '') + scope + '</article>';
  }).join('');
  const network = $('#academic-network-template');
  const academic = $('#capability-academicsearch .settings-advanced');
  academic.append(network.content.cloneNode(true));
  applyI18n(academic);
  $('#routing-controls').innerHTML =
    '<div data-mode-config="apipool" hidden>' + configFields(['apipool.strategy','apipool.engines']) + advancedFields(['apipool.weights']) + '</div>' +
    '<div data-mode-config="mcphub" hidden>' + configFields(['mcp_hub.strategy','mcp_hub.timeout_sec','mcp_hub.fetch_timeout_sec','mcp_hub.cooldown_sec','mcp_hub.rotation_engines','mcp_hub.fallback_engines']) + '</div>';
  $('#operational-controls').innerHTML = configFields(['language','log_level','proxy.enabled','proxy.endpoint','proxy.api_providers']) +
    advancedFields(['rate_limit.per_sec','rate_limit.per_min','dashboard.retention_days','dashboard.client_projects']);
  $('#brand-controls').innerHTML = configFields(['dashboard.brand.title','dashboard.brand.theme','dashboard.brand.accent','dashboard.brand.footer']);
  $('#quota-controls').innerHTML = configFields(['dashboard.quotas.enforce_providers']) +
    advancedFields(['dashboard.quotas.reset','dashboard.quotas.reset_day','dashboard.quotas.limits']);
}

function orderedConfigHtml(key, options, chosen, disabled) {
  const active = new Set(Array.isArray(chosen) ? chosen : []);
  const ordered = configField(key)?.[1] !== 'checklist';
  const values = [...new Set(ordered ? [...active, ...options] : [...options, ...active])];
  const moveUp = t('web.config.move_up');
  const moveDown = t('web.config.move_down');
  const disable = ' disabled';
  return '<ul class="ordered-options">' + values.map((name, index) => {
    const label = esc(displayName(name));
    const checked = active.has(name);
    const attrs = ' type="button" data-order-key="' + esc(key) + '" data-order-name="' + esc(name) + '"';
    return '<li><label><input type="checkbox" data-order-key="' + esc(key) + '" value="' + esc(name) + '"' + (checked ? ' checked' : '') +
      (disabled ? disable : '') + '>' + label + '</label>' +
      (checked && ordered ? '<span><button' + attrs + ' data-order-shift="-1" aria-label="' + esc(moveUp) + ' ' + label + '"' +
        (disabled || index === 0 ? disable : '') + '>' + esc(moveUp) + '</button><button' + attrs + ' data-order-shift="1" aria-label="' + esc(moveDown) + ' ' + label + '"' +
        (disabled || index === active.size - 1 ? disable : '') + '>' + esc(moveDown) + '</button></span>' : '') + '</li>';
  }).join('') + '</ul>';
}

function renderOrderedFields() {
  $$('[data-ordered-field]').forEach(el => {
    const key = el.dataset.orderedField;
    const options = state.settings?.options?.[key] || [];
    const markup = orderedConfigHtml(key, options, displayedConfigValue(key), Boolean(settingsFieldReason(key)));
    if (el.dataset.markup !== markup) {
      const active = document.activeElement;
      const focused = el.contains(active) ? Array.from(el.querySelectorAll('input,button')).indexOf(active) : -1;
      el.innerHTML = markup;
      el.dataset.markup = markup;
      if (focused >= 0) el.querySelectorAll('input,button')[focused]?.focus();
    }
  });
}

function changeOrderedConfig(key, name, checked, shift) {
  const next = [...(draftValue(key) || [])];
  const current = next.indexOf(name);
  if (shift) {
    const index = current + shift;
    if (current < 0 || index < 0 || index >= next.length) return;
    [next[current], next[index]] = [next[index], next[current]];
  } else if (checked && current < 0) next.push(name);
  else if (!checked && current >= 0) next.splice(current, 1);
  setDraft(key, next);
  renderOrderedFields();
}

function displayedConfigValue(key) {
  return state.settings?.locked_fields?.[key] ? state.settings.values[key] : draftValue(key);
}

function displayEditValue(value, type) {
  if (type === 'tri-state') return String(value ?? null);
  if (type === 'hosts') return Array.isArray(value) ? value.join('\n') : '';
  if (type && type.endsWith('-map')) return Object.entries(value || {}).map(([key, val]) => key + ' = ' + val).join('\n');
  return value ?? '';
}

function normalizeBlockedHost(value) {
  const host = value.replace(/\.$/, '');
  const domain = /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
  if (host.length <= 253 && domain.test(host)) return host;
  if (host && !/[/\\?#[\]@%*]/u.test(host)) {
    try {
      const isIPv6 = host.includes(':');
      const hostname = new URL('http://' + (isIPv6 ? '[' + host + ']' : host)).hostname;
      if (isIPv6) return hostname.slice(1, -1);
      if (hostname.length <= 253 && domain.test(hostname)) return hostname;
    } catch (_) { /* Invalid host syntax. */ }
  }
  throw new Error(t('web.config.invalid_domain'));
}

function parseConfigInput(el) {
  const type = el.dataset.configType;
  if (el.type === 'checkbox') return el.checked;
  if (type === 'tri-state') return { null: null, true: true, false: false }[el.value];
  if (type === 'hosts') {
    const input = el.value.toLowerCase().split(/[\s,]+/u).filter(Boolean);
    return [...new Set(input.map(normalizeBlockedHost))];
  }
  if (type && type.endsWith('-map')) {
    const entries = new Map();
    for (const line of el.value.split('\n').map(line => line.trim()).filter(Boolean)) {
      const entry = /^([^=]+?)\s*=\s*(.+)$/.exec(line);
      if (!entry || !entry[1].trim() || entries.has(entry[1].trim())) throw new Error(t('web.config.invalid_mapping'));
      let value = entry[2].trim();
      if (!value) throw new Error(t('web.config.invalid_mapping'));
      if (type === 'numbers-map' || type === 'weights-map') {
        const minimum = type === 'weights-map' ? 0 : 1;
        const maximum = type === 'weights-map' ? 1000000 : 1000000000;
        if (!/^\d+$/.test(value) || Number(value) < minimum || Number(value) > maximum) {
          throw new Error(t('web.config.invalid_mapping_range', { min: minimum, max: maximum }));
        }
        value = Number(value);
      }
      entries.set(entry[1].trim(), value);
    }
    return Object.fromEntries(entries);
  }
  if (el.type === 'number' || type === 'numeric-select') {
    const value = el.value === '' ? null : Number(el.value);
    if (el.dataset.key === 'pdf_parser.mineru_page_batch_size' && value > 0 && value < 10) {
      throw new Error(t('web.config.invalid_page_batch'));
    }
    if (el.dataset.key === 'doubao.num_results' && displayedConfigValue('doubao.version') === 'global' && value > 20) {
      throw new Error(t('web.config.invalid_global_results'));
    }
    return value;
  }
  return el.value;
}

function renderCapabilityStates() {
  const configured = state.overview?.configured_tools || [];
  const llm = state.overview?.runtime?.llm;
  $$('[data-tool-state]').forEach(el => {
    const id = el.dataset.toolState;
    const tool = configured.find(item => item.name === id);
    const ready = tool ? tool.enabled : id === 'llm' ? llm?.enabled : undefined;
    const status = tool?.registration_state || tool?.state;
    const registration = tool?.registered ? t('web.status.registered') : ({
      unprobed: t('web.status.not_probed'), pending: t('web.status.awaiting_registration'),
      unavailable: t('web.status.unavailable'),
    })[status] || (tool ? t('web.status.not_registered') : '');
    const enabled = ready === undefined ? t('web.status.unknown') :
      ready ? t('web.status.enabled') : t('web.status.disabled_in_config');
    const file = tool?.file;
    const scope = file ? t('web.label.allowlisted_roots') + ' ' + fmtInt(file.allowed_root_count) : '';
    const model = id === 'llm' && llm?.enabled ? llm.model_id : '';
    const fetch = id === 'cleanfetch' && typeof tool?.fetch_enabled === 'boolean'
      ? t('web.note.saved_content_reads_available_url_fetching') + (tool.fetch_enabled ? t('web.label.on') : t('web.label.off_2')) : '';
    const connectivity = file?.connectivity === 'available' ? t('web.diagnostics.connectivity_available') :
      file?.connectivity === 'unavailable' ? t('web.diagnostics.connectivity_unavailable') : '';
    el.textContent = [enabled, registration, model, scope, fetch, connectivity].filter(Boolean).join(' · ');
  });
}

async function toggleEverythingScope() {
  const button = $('#everything-scope-button');
  const view = $('#everything-scope');
  if (button.getAttribute('aria-expanded') === 'true') {
    view.textContent = '';
    button.setAttribute('aria-expanded', 'false');
    button.textContent = t('web.config.show_scope');
    return;
  }
  button.disabled = true;
  try {
    const result = await fetchJSON('/__admin/api/everything/scope', { headers: await adminHeaders(), signal: AbortSignal.timeout(5000) });
    if (!$('#page-tools').classList.contains('active')) return;
    const roots = Array.isArray(result.roots) ? result.roots : [];
    view.innerHTML = '<dl class="scope-view"><div><dt>' + esc(t('web.config.scope_url')) +
      '</dt><dd>' + esc(result.url || '—') + '</dd></div><div><dt>' + esc(t('web.label.allowlisted_roots')) +
      '</dt><dd>' + (roots.length ? '<ul>' + roots.map(root => '<li>' + esc(root) + '</li>').join('') +
      '</ul>' : esc(t('web.status.directories_unrestricted'))) + '</dd></div></dl>';
    button.setAttribute('aria-expanded', 'true');
    button.textContent = t('web.config.hide_scope');
  } catch (e) {
    clearRejectedCredentials(e);
    toast(e.message, true);
  } finally {
    button.disabled = !writeAllowed();
  }
}


function renderDiagnosticControls() {
  const options = state.overview?.diagnostics;
  const search = $('#diagnostic-search');
  const select = search?.elements?.namedItem('source');
  if (!select) return;
  const selected = select.value;
  const sources = options?.sources || [];
  select.replaceChildren(...sources.map(({ id, name }) => new Option(name, id)));
  if (sources.some(({ id }) => id === selected)) select.value = selected;
  const file = state.overview?.configured_tools?.find(tool => tool.name === 'file_search');
  const fetch = state.overview?.configured_tools?.find(tool => tool.name === 'cleanfetch');
  for (const [selector, enabled] of [
    ['#diagnostic-proxy', true], ['#diagnostic-everything', file?.enabled],
    ['#diagnostic-search button', sources.length], ['#diagnostic-fetch button', fetch?.fetch_enabled],
  ]) {
    const button = $(selector);
    if (button) button.disabled = !enabled || !localActionAvailable() || Boolean(button.dataset.running);
  }
}

function diagnosticResultText(result) {
  const message = [result.ok ? t('web.status.success') : t('web.status.failure'), fmtMS(result.elapsed_ms)];
  if (result.endpoint) message.push(result.endpoint);
  const kinds = {
    unconfigured: t('web.diagnostics.unconfigured'),
    invalid_endpoint: t('web.diagnostics.invalid_endpoint'),
    blocked: t('web.diagnostics.blocked'),
    challenge: t('web.diagnostics.challenge'),
  };
  if (result.error_kind) message.push(result.error_detail || kinds[result.error_kind] || errorKindLabel(result.error_kind));
  if (typeof result.result_count === 'number') message.push(t('web.diagnostics.result_count', { count: fmtInt(result.result_count) }));
  if (result.titles?.length) message.push(result.titles.join(' / '));
  if (result.fetch) {
    const { node, attempts, body_status, characters, remote_redirects } = result.fetch;
    if (node) message.push(t('web.diagnostics.fetch_node', { node: displayName(node) }));
    if (attempts?.length > 1) message.push(attempts.map(displayName).join(' → '));
    message.push(body_status === 'available'
      ? t('web.diagnostics.fetch_chars', { count: fmtInt(characters) }) : t('web.diagnostics.no_body'));
    if (remote_redirects) message.push(t('web.diagnostics.remote_redirects'));
  }
  return message.join(' · ');
}

async function runDiagnostic(kind, button, view, payload = {}) {
  if (!localActionAvailable() || button.disabled) return;
  button.dataset.running = 'true';
  button.disabled = true;
  view.textContent = '';
  try {
    const result = await localActionPost('/__admin/api/diagnostics/' + kind, payload);
    setNote(view, diagnosticResultText(result), result.ok ? 'ok' : 'bad');
    for (const { title, url } of result.links || []) {
      if (!/^https:\/\/pubmed\.ncbi\.nlm\.nih\.gov\/[1-9][0-9]*\/$/.test(url)) continue;
      const link = document.createElement('a');
      link.textContent = title;
      link.href = url;
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
      view.append(' · ', link);
    }
    if (kind === 'everything') await loadOverview();
  } catch (e) {
    setNote(view, e.message, 'bad');
  } finally {
    delete button.dataset.running;
    renderDiagnosticControls();
  }
}

function wireDiagnostics() {
  for (const kind of ['proxy', 'everything']) {
    const button = $('#diagnostic-' + kind);
    button?.addEventListener('click', () => runDiagnostic(kind, button, $('#diagnostic-' + kind + '-result')));
  }
  const search = $('#diagnostic-search');
  search?.addEventListener('submit', async e => {
    e.preventDefault();
    const { source, query } = Object.fromEntries(new FormData(search));
    const chosen = state.overview?.diagnostics?.sources?.find(item => item.id === source);
    if (!chosen) return;
    const button = search.querySelector('button');
    if (button.disabled) return;
    if (chosen.confirm && !(await confirmDialog(t('web.diagnostics.run_search'), t('web.diagnostics.confirm_search', { source: chosen.name })))) return;
    await runDiagnostic('search', button, search.querySelector('[role=status]'), { source, query, confirm: Boolean(chosen.confirm) });
  });
  const fetch = $('#diagnostic-fetch');
  fetch?.addEventListener('submit', async e => {
    e.preventDefault();
    const { url } = Object.fromEntries(new FormData(fetch));
    const services = state.overview?.diagnostics?.fetch_services || [];
    const button = fetch.querySelector('button');
    if (button.disabled) return;
    if (services.length && !(await confirmDialog(t('web.diagnostics.run_fetch'), t('web.diagnostics.confirm_fetch', { sources: services.map(displayName).join(', ') })))) return;
    await runDiagnostic('fetch', button, fetch.querySelector('[role=status]'), { url, confirm: services.length > 0 });
  });
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
  const heading = key => ' data-label="' + esc(t(key)) + '"';
  const cells = [
    `<td class="src-name"><b>${esc(name)}</b>${detailed ? sourceDrawerButton(s, name) : ''}${s.participation ? `<small class="source-participation">${esc(s.participation)}</small>` : ''}${s.condition ? `<small class="source-condition">${esc(s.condition)}</small>` : ''}</td>`,
    `<td${heading('web.label.enabled')}>${sourceControl(s)}</td>`,
    `<td${heading('web.label.historical_health')}><span class="nowrap" title="${t('web.note.last_observed')}${esc(fmtAt(s.last_seen_at))}">${dot(health.dot)} ${esc(health.label)}</span>${suspendBadge(s)}${cooldownBadge(s.actual_cooldown, detailed, name)}</td>`,
    `<td${heading('web.label.last_20')}>${healthStrip(s)}</td>`,
    `<td class="num"${heading('web.label.attempts_today')}>${today ? fmtInt(today.requests) : '<span class="dim">—</span>'}</td>`,
    `<td class="num"${heading('web.label.success_rate')}>${successRate === null ? '<span class="dim">—</span>' : esc(Math.round(successRate * 100) + '%')}</td>`,
    `<td class="num"${heading('web.label.average_latency')}>${avg}${num(s.p95_ms) > 0 ? `<small>P95 ${esc(fmtMS(s.p95_ms))}</small>` : ''}</td>`,
    `<td class="wrap-any"${heading('web.label.last_error')}>${s.last_error ? '<span class="bad-text">' + esc(s.last_error) + '</span>' : '<span class="dim">—</span>'}${s.error_kinds && s.error_kinds.length ? `<div class="source-errors" aria-label="${t('web.label.failure_breakdown')}">${formatErrorKinds(s.error_kinds)}</div>` : ''}</td>`,
  ];
  if (detailed && s.category === 'api') {
    const quota = state.quotas?.find(q => q.provider === id);
    const skipped = s.quota_skip_reason ? `<small class="quota-skipped">${esc(s.quota_skip_reason)}</small>` : '';
    cells.push(`<td class="quota-cell"${heading('web.quota.heading')}>${quota ? quotaUsageHtml(quota, true) : skipped ? '' : '—'}${skipped}</td>`);
  }
  const rowClass = s.active === true && s.suspended === true ? 'row-suspended' : (s.active === true && s.status === 'down' ? 'row-down' : '');
  return `<tr class="source-entry ${rowClass}">${cells.join('')}</tr>` + (detailed ? sourceDrawer(s, cells.length) : '');
}

function sourceConfigKeys(s) {
  const score = s.score_key ? ['weight', 'max_size', 'min_score'].map(field => 'smartsearch.engines.' + s.score_key + '.' + field) : [];
  return [...(SOURCE_CONFIGS[s.id] || []), ...score].filter(key => Object.hasOwn(savedValues(), key));
}

function sourceDrawerButton(s, name) {
  if (!sourceConfigKeys(s).length) return '';
  const open = state.openSources.has(s.id);
  const label = t('web.config.source_parameters');
  return '<button type="button" class="source-params" data-source-drawer="' + esc(s.id) + '" aria-controls="source-config-' +
    esc(s.id) + '" aria-expanded="' + open + '" aria-label="' + esc(name + ' · ' + label) + '">' + esc(label) + '</button>';
}

function sourceDrawer(s, cols) {
  const keys = sourceConfigKeys(s);
  if (!keys.length) return '';
  return '<tr class="source-config-row" id="source-config-' + esc(s.id) + '"' +
    (state.openSources.has(s.id) ? '' : ' hidden') + '><td colspan="' + cols + '">' + configFields(keys) + '</td></tr>';
}

function cooldownSeconds(until) {
  return Math.ceil((Date.parse(until) - Date.now()) / 1000);
}

function cooldownLabel(seconds) {
  return t('web.label.cooldown_countdown', { count: fmtInt(seconds) });
}

function cooldownBadge(cooldown, detailed, name) {
  if (!cooldown) return '';
  const seconds = cooldownSeconds(cooldown.until);
  if (!(seconds > 0)) return '';
  const title = [errorKindLabel(cooldown.kind), fmtAt(cooldown.until)].filter(Boolean).join(' · ');
  const badge = `<span class="kind-chip route-cooldown" data-route-cooldown-until="${esc(cooldown.until)}" title="${esc(title)}">${esc(cooldownLabel(seconds))}</span>`;
  if (!detailed) return badge;
  const label = esc(t('web.runtime_action.release_source', { Name: name }));
  return `<span class="route-cooldown-control">${badge} <button type="button" class="icon-btn" data-cooldown-release="${esc(cooldown.engine)}" data-cooldown-name="${esc(name)}" title="${label}" aria-label="${label}" disabled>${icon('rotate-ccw')}</button></span>`;
}

function updateRoutingCooldowns() {
  const page = $('#page-sources');
  if (document.hidden || !page.classList.contains('active')) return;
  $$('[data-route-cooldown-until]', page).forEach(badge => {
    const seconds = cooldownSeconds(badge.dataset.routeCooldownUntil);
    if (seconds > 0) badge.textContent = cooldownLabel(seconds);
    else (badge.closest?.('.route-cooldown-control') || badge).remove();
  });
  renderRuntimeActionButtons();
}

function renderRuntimeActionButtons() {
  const disabled = !localActionAvailable() || Boolean(state.runtimeAction);
  $$('[data-cooldown-release], [data-quota-refresh]').forEach(button => { button.disabled = disabled; });
  const all = $('[data-cooldown-release="all"]');
  if (all) all.hidden = ![...(webSources() || []), ...(academicSources() || [])]
    .some(source => source.actual_cooldown && cooldownSeconds(source.actual_cooldown.until) > 0);
}

async function releaseRoutingCooldown(engine, name) {
  if (!localActionAvailable() || state.runtimeAction) return;
  state.runtimeAction = 'cooldown';
  renderRuntimeActionButtons();
  try {
    const title = engine === 'all' ? t('web.runtime_action.release_all') : t('web.runtime_action.release_source', { Name: name });
    if (!(await confirmDialog(title, t('web.runtime_action.release_confirm')))) return;
    const result = await localActionPost('/__admin/api/sources/cooldowns/release', { engine, confirm: true });
    await loadOverview();
    toast(t('web.runtime_action.released', { Count: fmtInt(result.cleared) }));
  } catch (e) {
    toast(e.message, true);
  } finally {
    state.runtimeAction = '';
    renderRuntimeActionButtons();
  }
}

async function refreshOfficialQuotas() {
  if (!localActionAvailable() || state.runtimeAction) return;
  state.runtimeAction = 'quota';
  renderRuntimeActionButtons();
  try {
    if (!(await confirmDialog(t('web.runtime_action.refresh_quota'), t('web.runtime_action.quota_confirm')))) return;
    state.quotaRequest++;
    const result = await localActionPost('/__admin/api/quotas/refresh', { confirm: true });
    state.quotas = result.items;
    state.quotaError = null;
    renderQuotas();
    const failed = result.items.some(item => item.status === 'query_failed' || item.official_query_failed || item.details?.fetch_error);
    toast(t(result.refreshed
      ? failed ? 'web.runtime_action.quota_partial' : 'web.runtime_action.quota_refreshed'
      : 'web.runtime_action.quota_debounced'), failed);
  } catch (e) {
    toast(e.message, true);
  } finally {
    state.runtimeAction = '';
    renderRuntimeActionButtons();
  }
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

function draftValue(key) {
  return Object.hasOwn(state.draft, key) ? state.draft[key] : savedValues()[key];
}

function writeAllowed() {
  return Boolean(!state.settingsError && state.settings && !state.settings.config_error && state.settings.revision && state.settings.write_access && state.settings.write_access.allowed && adminConfigured());
}

function localActionAllowed() {
  return !state.settingsError && state.settings?.local_action_allowed === true;
}

function localActionAvailable() {
  return localActionAllowed() && !state.applying && !state.saving;
}

function sourceControl(s) {
  const key = s.control_path;
  const inputId = 'source-switch-' + s.id;
  const value = key && Object.hasOwn(savedValues(), key) ? displayedConfigValue(key) : s.inverted ? !s.enabled : s.enabled;
  const enabled = value === null ? s.active === true : s.inverted ? value === false : value === true;
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

function renderSourcePages(force = false) {
  const webList = webSources();
  const acadList = academicSources();
  const groups = [
    ['web', webList && webList.filter(s => s.category !== 'api' && s.category !== 'mcphub')],
    ['api', webList && webList.filter(s => s.category === 'api')],
    ['academic', acadList],
    ['mcphub', webList && webList.filter(s => s.category === 'mcphub')],
  ];
  groups.forEach(([id, list]) => {
    const body = $('#sources-' + id + '-body');
    if (!body) return;
    const cols = id === 'api' ? 9 : 8;
    if (force || !document.activeElement?.closest('.source-config-row, .source-params') || !body.contains(document.activeElement)) {
      body.innerHTML = list === null ? emptyRow(cols, t('web.note.loading_2')) : list.length ? list.map(s => sourceRowHtml(s, true)).join('') : emptyRow(cols, t('web.note.no_sources_yet'));
    }
    setText('#sources-' + id + '-counts', list ? countsLabel(list) : '—');
    const overviewNote = state.overviewError ? t('web.note.data_may_be_stale_2') + state.overviewError : '';
    const quotaNote = id === 'api' ? quotaLoadNote() : '';
    const stale = state.overviewError || (id === 'api' && state.quotaError);
    setNote($('#sources-' + id + '-note'), [overviewNote, quotaNote].filter(Boolean).join(' · '), stale ? 'warn' : '');
  });
  renderConfigInputs();
  renderDraftState();
  renderRuntimeActionButtons();
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
  const providers = (detail.rows || []).filter(row => row.kind === 'provider' || row.kind === 'quota_skip');
  if (!providers.length) return html + `<p class="panel-note">${t('web.note.no_source_details_were_retained')}</p>`;
  html += `<table class="data-table call-detail-table"><colgroup><col style="width:24%"><col style="width:12%"><col style="width:14%"><col style="width:10%"><col style="width:40%"></colgroup><thead><tr><th>${t('web.label.source')}</th><th>${t('web.label.status')}</th><th>${t('web.label.latency')}</th><th>${t('web.label.results')}</th><th>${t('web.label.error_summary')}</th></tr></thead><tbody>`;
  html += providers.map(row => {
    const skipped = row.kind === 'quota_skip' || row.outcome === 'skipped';
    const status = skipped ? t('web.status.skipped') : row.success ? t('web.status.success') : t('web.status.failure');
    const style = skipped ? 'quota-skipped' : row.success ? 'ok-text' : 'bad-text';
    const summary = skipped ? row.skip_reason : [errorKindLabel(row.error_kind), row.error_summary].filter(Boolean).join(' · ');
    return `<tr><td title="${esc(row.detail || '')}">${esc(displayName(row.provider))}</td><td class="${style}">${esc(status)}</td><td>${skipped ? '—' : esc(fmtMS(row.duration_ms))}</td><td>${skipped ? '—' : fmtInt(row.result_count)}</td><td class="wrap-any">${esc(summary || '—')}</td></tr>`;
  }).join('');
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
      rows.push(...details.filter(e => e.kind === 'provider' || e.kind === 'quota_skip'));
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
      e.kind === 'provider' || e.kind === 'quota_skip' ? t('web.label.source') : t('web.label.tool'),
      e.kind === 'tool' ? displayName(e.tool) : '',
      e.provider ? displayName(e.provider) : '',
      e.detail || '',
      e.kind === 'quota_skip' ? t('web.status.skipped') : e.success ? t('web.status.success') : t('web.status.failure'),
      errorKindLabel(e.error_kind),
      num(e.duration_ms) === null ? '' : e.duration_ms,
      num(e.result_count) === null ? '' : e.result_count,
      e.cache_hit ? t('web.label.yes') : t('web.label.no'),
      e.query_topic || '',
      e.query_language || '',
      e.query_keywords || '',
      e.query_hash || '',
      num(e.query_chars) === null ? '' : e.query_chars,
      e.skip_reason || e.error_summary || '',
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
  renderFilterSelect('#filter-application', Object.entries(APPLICATION_LABELS), state.filters.application);
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
  const version = displayedConfigValue('doubao.version');
  if (key === 'doubao.icp_host_only' && version !== 'global') return t('web.note.applies_to_global_only_the_current_value_is_kept');
  if (['doubao.need_content', 'doubao.auth_level', 'doubao.query_rewrite'].includes(key) && version !== 'custom') return t('web.config.custom_only');
  if (key === 'proxy.endpoint' && displayedConfigValue('proxy.enabled') !== true) return t('web.config.explicit_proxy_only');
  if (key === 'dashboard.quotas.reset_day' && displayedConfigValue('dashboard.quotas.reset') !== 'monthly') return t('web.config.monthly_only');
  return '';
}

function renderConfigInputs(force = false) {
  $$('[data-key]').forEach(el => {
    const key = el.dataset.key;
    const value = displayedConfigValue(key);
    if (el.type === 'checkbox') el.checked = value === true;
    else if (el.type === 'radio') el.checked = el.value === value;
    else if (Object.hasOwn(state.invalidDraft, key)) {
      if (force || document.activeElement !== el) el.value = state.invalidDraft[key].value;
      el.setCustomValidity?.(state.invalidDraft[key].message);
    }
    else if (force || document.activeElement !== el) el.value = displayEditValue(value, el.dataset.configType);
    const reason = settingsFieldReason(key);
    showConfigInputStatus(el);
    el.disabled = Boolean(reason);
    el.title = reason;
  });
}

function showConfigInputStatus(el) {
  const key = el.dataset.key;
  const locked = state.settings?.locked_fields?.[key];
  const issue = state.invalidDraft[key];
  if (issue) el.setAttribute('aria-invalid', 'true');
  else el.removeAttribute?.('aria-invalid');
  const label = el.closest?.('label');
  if (!label || label.closest('.mode-choice')) return;
  let note = label.querySelector('.field-status');
  if ((locked || issue) && !note) {
    note = document.createElement('small');
    note.className = 'field-status';
    note.setAttribute('aria-live', 'polite');
    label.append(note);
  }
  if (note) note.textContent = locked || issue?.message || '';
}

function renderSettingsForm() {
  const s = state.settings;
  if (!s || !s.values) return;
  renderConfigInputs();
  renderOrderedFields();
  renderModeControls();
  renderCredentials();
  state.settingsLoaded = true;
  renderDraftState();
}

function credentialCount(s) {
  return t('web.credential.count', {count: fmtInt(s.count)});
}

function renderCredentials() {
  const status = state.settings?.credential_status;
  const error = state.settings?.credential_error;
  const note = $('#secret-note');
  setNote(note, error || (status?.pending_restart ? t('web.credential.pending_restart') : ''), 'warn');
  const list = $('#secret-list');
  const credentials = status?.credentials || {};
  const markup = Object.entries(SECRETS).flatMap(([key, [name, url, linkLabel]]) => {
    const row = credentials[key];
    if (!row) return [];
    const readOnly = key === 'WEBSEARCH_TOKEN' || row.read_only;
    const on = row.configured;
    const meta = [readOnly && t('web.credential.read_only'), on ? t('web.status.configured') : t('web.status.not_configured'),
      credentialCount(row), ...(row.sources || [])].filter(Boolean).join(' · ');
    const link = url ? `<a href="${esc(url)}" target="_blank" rel="noopener noreferrer">${esc(linkLabel || t('web.credential.get_key'))}</a>` : '';
    const title = t('web.credential.manage', {name});
    const edit = readOnly ? '' : `<button type="button" class="icon-btn" data-secret="${esc(key)}" title="${esc(title)}" aria-label="${esc(title)}"${error || !writeAllowed() || state.applying || state.saving ? ' disabled' : ''}>${icon('key-round')}</button>`;
    return `<div class="secret-row">${dot(on ? 'healthy' : 'unknown')}<div><b>${esc(name)}</b><small>${esc(key)} · ${esc(meta)}</small>${link}</div>${edit}</div>`;
  }).join('');
  if (list.dataset?.markup !== markup) {
    list.innerHTML = markup;
    if (list.dataset) list.dataset.markup = markup;
    $$('[data-secret]', list).forEach(b => { b.onclick = () => editSecret(b.dataset.secret); });
  }
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
  $$('[data-mode-config]').forEach(el => { el.hidden = el.dataset.modeConfig !== mode; });
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
  if (!state.draftRevision) state.draftRevision = state.settings.revision;
  if (!Object.hasOwn(state.draftBefore, key)) state.draftBefore[key] = savedValues()[key];
  if (configEqual(value, state.draftBefore[key])) {
    delete state.draft[key];
    delete state.draftBefore[key];
  } else state.draft[key] = value;
  renderDraftState();
  if (key === 'mode') renderModeControls();
}

function markDirty(e) {
  const el = e.target;
  const key = el.dataset.key;
  if (!key || (el.type === 'radio' && !el.checked) || settingsFieldReason(key)) return;
  el.setCustomValidity?.('');
  try {
    const value = parseConfigInput(el);
    if (el.validity && !el.validity.valid) throw new Error(el.validationMessage);
    delete state.invalidDraft[key];
    setDraft(key, value);
  } catch (e) {
    if (!state.draftRevision) state.draftRevision = state.settings.revision;
    state.invalidDraft[key] = { value: el.value, message: e.message };
    el.setCustomValidity?.(e.message);
    renderDraftState();
  }
  showConfigInputStatus(el);
  if (['proxy.enabled','doubao.version','dashboard.quotas.reset'].includes(key)) renderConfigInputs();
}

function configEqual(left, right) {
  if (left === right) return true;
  if (Array.isArray(left)) {
    return Array.isArray(right) && left.length === right.length && left.every((value, i) => configEqual(value, right[i]));
  }
  if (!left || !right || typeof left !== 'object' || typeof right !== 'object' || Array.isArray(right)) return false;
  return Object.keys(left).length === Object.keys(right).length && Object.entries(left).every(([key, value]) => Object.hasOwn(right, key) && configEqual(value, right[key]));
}

function setModeHelp(button, expanded, dismissed = false) {
  const help = button.parentElement;
  help.classList.toggle('open', expanded);
  help.classList.toggle('dismissed', dismissed);
  button.setAttribute('aria-expanded', String(expanded));
}

function renderDraftState() {
  const count = new Set([...Object.keys(state.draft), ...Object.keys(state.invalidDraft)]).size;
  const dirty = count > 0;
  state.settingsDirty = dirty;
  if (!dirty) state.draftRevision = '';
  ['#source-draft-count', '#tools-draft-count', '#settings-draft-count'].forEach(sel => setText(sel, dirty ? count + ` ${t('web.note.draft_items')}` : ''));
  ['#save-settings', '#save-sources', '#save-tools'].forEach(sel => { const el = $(sel); if (el) el.disabled = !dirty || !writeAllowed() || state.applying || state.saving; });
  ['#undo-settings', '#undo-sources', '#undo-tools'].forEach(sel => { const el = $(sel); if (el) el.disabled = !dirty || state.applying || state.saving; });
}

function discardDraft() {
  state.draft = {};
  state.draftBefore = {};
  state.invalidDraft = {};
  $$('[data-key]').forEach(el => el.setCustomValidity?.(''));
  state.draftRevision = '';
  renderConfigInputs(true);
  renderSettingsForm();
  renderSourcePages(true);
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
  renderRuntimeActionButtons();
  renderDiagnosticControls();
  const localReason = localActionReason();
  $$('[data-local-action-note]').forEach(note => setNote(note, localReason, localReason ? 'warn' : ''));
  ['#source-write-note', '#settings-write-note', '#tools-write-note'].forEach(sel => setNote($(sel), writeAllowed() ? '' : writeReason(), writeAllowed() ? '' : 'warn'));
  ['#restart-service', '#retry-apply', '#restore-backup', '#everything-scope-button'].forEach(sel => { const el = $(sel); if (el) el.disabled = !writeAllowed() || state.applying || state.saving; });
  renderStorage();
  $$('[data-quota-reset], [data-quota-adjust]').forEach(el => { el.disabled = !writeAllowed() || state.applying || state.saving || Boolean(state.quotaError); });
  renderConfigInputs();
  renderOrderedFields();
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

async function loadStorage() {
  const request = ++state.storage.request;
  try {
    const data = await fetchJSON('/__admin/api/storage', { signal: AbortSignal.timeout(5000) });
    if (request !== state.storage.request) return;
    state.storage.data = data;
    state.storage.error = null;
  } catch (e) {
    if (request !== state.storage.request) return;
    state.storage.error = e.message;
  }
  renderStorage();
}

function storageSize(bytes) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let unit = 0;
  while (bytes >= 1024 && unit < units.length - 1) {
    bytes /= 1024;
    unit++;
  }
  return new Intl.NumberFormat(I18N_LANG, { maximumFractionDigits: unit ? 1 : 0 }).format(bytes) + ' ' + units[unit];
}

function renderStorage() {
  const note = $('#storage-note');
  if (!note) return;
  const { data, error, clearing } = state.storage;
  const problem = error || data?.cache?.error || data?.history?.error || data?.fetchdata?.error;
  setNote(note, problem || (!data ? t('web.note.loading_2') : ''), problem ? 'bad' : '');
  for (const kind of ['cache', 'history', 'fetchdata']) {
    const entry = data?.[kind];
    const available = !error && entry?.enabled && !entry.error;
    setText('#storage-' + kind + '-count', available ? fmtInt(entry.count) : entry && !entry.enabled ? t('web.storage.disabled') : '—');
    setText('#storage-' + kind + '-bytes', available ? storageSize(entry.bytes) : '—');
  }
  setText('#storage-fetchdata-expired', !error && data?.fetchdata && !data.fetchdata.error ?
    t('web.storage.expired_count', { Count: fmtInt(data.fetchdata.expired) }) : '');
  const canClear = localActionAvailable() && !error && !clearing;
  const controls = [
    ['#clear-cache', data?.cache, 'count'],
    ['#clear-history', data?.history, 'count'],
    ['#clear-expired-fetchdata', data?.fetchdata, 'expired'],
    ['#clear-fetchdata', data?.fetchdata, 'clearable'],
  ];
  for (const [selector, entry, field] of controls) {
    const button = $(selector);
    if (button) button.disabled = !canClear || !entry?.enabled || !!entry.error || !entry[field];
  }
}

async function clearStorage(button, endpoint, title, message, payload = {}) {
  if (!localActionAvailable() || button.disabled || state.storage.clearing) return;
  state.storage.clearing = true;
  renderStorage();
  try {
    if (!(await confirmDialog(title, message))) return;
    const res = await localActionPost(endpoint, { confirm: true, ...payload });
    toast(t('web.storage.cleared', { Count: fmtInt(res.cleared) }));
  } catch (e) {
    toast(e.message, true);
  } finally {
    state.storage.clearing = false;
    await loadStorage();
  }
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

function localActionReason() {
  if (localActionAllowed()) return '';
  if (state.settingsError) return t('web.runtime_action.permission_unavailable');
  if (!state.settings) return t('web.runtime_action.permission_loading');
  return t('web.runtime_action.source_not_allowed');
}

async function localActionPost(path, payload) {
  if (!localActionAvailable()) throw new Error(localActionReason() || t('web.note.applying_configuration'));
  return postJSON(path, payload);
}

/* ---------- 管理员口令（写操作统一闸门） ---------- */
// 配置、凭证和账本等敏感操作限 loopback 或独立写白名单 + X-Admin-Password 头；
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
    clearRejectedCredentials(e);
    throw e;
  }
}

function clearRejectedCredentials(error) {
  if (error.status === 401 || error.status === 403 || String(error.message).includes(t('web.label.password'))) state.adminPass = '';
  if (error.status === 401 || error.status === 403 || String(error.message).includes(t('web.label.username'))) state.adminUser = '';
}

function configDiff(values, before, files) {
  const groups = new Map();
  for (const [key, value] of Object.entries(values)) {
    const file = files?.[key] === 'dashboard.yaml' ? 'dashboard.yaml' : 'config.yaml';
    if (!groups.has(file)) groups.set(file, []);
    groups.get(file).push(key + ': ' + (JSON.stringify(before[key]) ?? '—') + ' → ' + (JSON.stringify(value) ?? '—'));
  }
  return [...groups].map(([name, lines]) => name + '\n' + lines.join('\n')).join('\n\n');
}

async function saveSettings() {
  if (state.applying || state.saving || !writeAllowed()) return;
  const diff = changes();
  const localErrors = Object.entries(state.invalidDraft);
  if (localErrors.length) {
    const [key, error] = localErrors[0];
    toast((configField(key) ? t(configField(key)[0]) : key) + ' · ' + error.message, true);
    return;
  }
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
    const preview = await securePost('/__admin/api/settings', { changes: diff, revision, confirm: false });
    const targets = preview.changes || diff;
    const detail = configDiff(targets, state.draftBefore, preview.field_files || state.settings.field_files);
    if (!(await confirmDialog(t('web.action.save_and_restart'), t('web.dialog.the_current_config_is_backed_up_then_changes_are_saved_and_t'), detail))) return;
    const res = await securePost('/__admin/api/settings', { changes: diff, revision, confirm: true });
    state.applyResult = { backup: res.backup, targets: { ...(state.settings.pending_changes || {}), ...(res.changes || targets) }, status: t('web.msg.saved_restarting') };
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
  return Object.entries(targets).every(([key, value]) => configEqual(settings.values[key], value) && configEqual((settings.saved_values || settings.values)[key], value));
}

function renderApplyResult() {
  const result = state.applyResult;
  const credentialPending = Boolean(state.settings?.credential_status?.pending_restart);
  ['#apply-note', '#source-apply-note', '#tools-apply-note', '#settings-apply-note'].forEach(selector => {
    const el = $(selector);
    setNote(el, result?.status || '', result?.failed ? 'warn' : '');
    if (el && selector !== '#apply-note') el.classList.add('apply-inline');
  });
  const pending = Object.keys(state.settings && state.settings.pending_changes || {}).length > 0;
  const retry = Boolean(result && result.failed) || pending || credentialPending;
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
        const credentialsApplied = !state.applyResult.credentials && !state.settings?.credential_status?.pending_restart ||
          settings.credential_status?.pending_restart === false;
        if (settings.values && overview.system && credentialsApplied && runtimeMatches(settings, state.applyResult.targets)) {
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
    state.applyResult.status = state.applyResult.backupCreated ? t('web.credential.applied_with_backup') : t('web.msg.effective_config_confirmed');
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
    const detail = configDiff(preview.changes || {}, savedValues(), preview.field_files || state.settings.field_files);
    if (!(await confirmDialog(t('web.action.restore_backup_and_restart'), backup + t('web.note.the_current_draft_will_be_discarded'), detail))) return;
    const res = await securePost('/__admin/api/settings', { restore_backup: backup, revision, confirm: true });
    discardDraft();
    await loadSettings();
    state.applyResult = { backup: res.backup, targets: { ...(state.settings.pending_changes || {}), ...(res.changes || {}) }, status: t('web.msg.backup_restored_restarting') };
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
  const status = state.settings?.credential_status;
  const current = status?.pending?.[name] || status?.credentials?.[name];
  if (!current || current.read_only || name === 'WEBSEARCH_TOKEN' || state.settings?.credential_error || !writeAllowed() || state.saving || state.applying) return;
  if (!adminConfigured()) {
    toast(t('web.msg.admin_password_not_configured_set_dashboard_admin_password_i'), true);
    return;
  }
  const d = $('#secret-dialog');
  const action = $('#secret-dialog-action');
  const slot = $('#secret-dialog-slot');
  const value = $('#secret-dialog-input');
  const label = $('#secret-dialog-label');
  const pass = $('#secret-dialog-pass');
  const user = $('#secret-dialog-user');
  const slots = current.slots || [];
  const provider = SECRETS[name][0];
  $('#secret-dialog-title').textContent = t('web.credential.manage', {name: provider});
  $('#secret-dialog-help').textContent = name === 'UNPAYWALL_EMAIL' ? t('web.credential.email_help') :
    name === 'LLM_API_KEY' ? t('web.credential.llm_help') : '';
  const actions = [current.multi ? 'append' : 'set'];
  if (slots.length) {
    actions.push('delete');
    if (current.multi) actions.push('label', 'clear');
  }
  actions.push('reset');
  action.innerHTML = actions.map(op => `<option value="${op}">${esc(SECRET_ACTIONS[op])}</option>`).join('');
  action.value = actions[0];
  slot.innerHTML = slots.map((s, index) => `<option value="${esc(s.id)}">${esc([t('web.credential.slot_number', {number: index + 1}), s.source, s.label].filter(Boolean).join(' · '))}</option>`).join('');
  slot.value = slots[0]?.id || '';
  value.value = '';
  pass.value = cachedAdminPass();
  $('#secret-dialog-user-row').classList.toggle('hidden', !usernameConfigured());
  user.required = usernameConfigured();
  user.value = cachedAdminUser();
  const updateFields = () => {
    const usesValue = action.value === 'append' || action.value === 'set';
    const usesLabel = action.value === 'append' || action.value === 'label';
    const usesSlot = action.value === 'delete' || action.value === 'label';
    for (const [id, input, show] of [['value', value, usesValue], ['label', label, usesLabel], ['slot', slot, usesSlot]]) {
      $('#secret-dialog-' + id + '-row').classList.toggle('hidden', !show);
      input.disabled = !show;
    }
    value.required = usesValue;
    label.value = action.value === 'label' ? slots.find(s => s.id === slot.value)?.label || '' : '';
    if (!usesValue) value.value = '';
  };
  action.onchange = slot.onchange = updateFields;
  updateFields();
  d.returnValue = '';
  d.showModal();
  d.addEventListener('close', async () => {
    const op = action.value;
    const payload = {name, action: op, revision: status.revision};
    if (!value.disabled) payload.value = value.value;
    if (!label.disabled) payload.label = label.value;
    if (!slot.disabled) payload.slot_id = slot.value;
    const credentials = {pass: pass.value, user: user.value.trim()};
    value.value = label.value = pass.value = user.value = '';
    if (d.returnValue !== 'confirm' || !writeAllowed() || state.saving || state.applying) return;
    if (!credentials.pass || (usernameConfigured() && !credentials.user)) {
      toast(t(credentials.pass ? 'web.msg.admin_username_required' : 'web.msg.admin_password_required'), true);
      return;
    }
    state.adminPass = credentials.pass;
    state.adminUser = credentials.user;
    state.saving = true;
    renderSettingsForm();
    renderWriteAccess();
    try {
      const preview = await securePost(SECRET_API, {...payload, confirm: false});
      const diff = SECRET_ACTIONS[op] + '\n' + t('web.credential.count_diff', {before: fmtInt(preview.before_count), after: fmtInt(preview.after_count)});
      const title = preview.restart_required ? t('web.action.save_and_restart') : t('web.action.save');
      if (!(await confirmDialog(title, t('web.credential.backup_help'), diff))) return;
      const saved = await securePost(SECRET_API, {...payload, revision: preview.revision, confirm: true});
      if (saved.restart_required) {
        state.applyResult = {
          targets: {...state.settings.pending_changes}, credentials: true, backupCreated: saved.backup,
          status: saved.backup ? t('web.credential.backup_created') : t('web.msg.saved_restarting'),
        };
      }
      await loadSettings();
      if (saved.restart_required) await restartAndVerify();
      else toast(saved.backup ? t('web.credential.backup_created') : t('web.credential.saved'));
    } catch (e) {
      toast(e.message, true);
    } finally {
      state.saving = false;
      renderSettingsForm();
      renderWriteAccess();
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

function quotaLoadNote() {
  if (state.quotaError) {
    const prefix = state.quotas ? t('web.note.data_may_be_stale_2') : t('web.note.read_failed');
    return prefix + state.quotaError;
  }
  return state.quotas ? '' : t('web.quota.loading');
}

function quotaResetHtml(q) {
  if (q.reset_at) return `<time datetime="${esc(q.reset_at)}">${esc(fmtAt(q.reset_at))}</time>`;
  if (q.source === 'local') return esc(t('web.label.manual_only'));
  if (q.provider === 'tavily' && q.status === 'available') return esc(t('web.quota.tavily_reset'));
  return '—';
}

function quotaMetric(label, used, limit, remaining, unit) {
  const amount = t('web.quota.amount', { Used: fmtInt(used), Limit: fmtInt(limit), Unit: unit });
  const left = remaining == null ? '' : ' · ' + t('web.quota.remaining', { Count: fmtInt(remaining) });
  const labelHtml = label ? `<b>${esc(label)}</b> ` : '';
  if (num(used) === null || !(limit > 0)) return `<div class="quota-metric">${labelHtml}${esc(amount + left)}</div>`;
  const percent = 100 * used / limit;
  const level = percent >= 100 ? 'bad' : percent >= 80 ? 'warn' : '';
  const warning = level === 'bad' ? t('web.quota.limit_reached') : level ? t('web.quota.near_limit') : '';
  return `<div class="quota-metric ${level}">${labelHtml}${esc(amount + left)}` +
    (warning ? ` <b>${esc(warning)}</b>` : '') +
    `<progress max="100" value="${Math.max(0, Math.min(100, percent))}" aria-label="${esc((label ? label + ' · ' : '') + amount)}"></progress></div>`;
}

function quotaUsageHtml(q, showReset) {
  const local = q.source === 'local';
  if (q.status === 'not_configured') return `<span class="muted">${esc(t('web.status.not_configured'))}</span>`;
  const failed = `<span class="quota-warning">${esc(t('web.quota.official_failed'))}</span>`;
  if (q.status === 'query_failed') return failed;
  const source = `<span class="muted" title="${esc(local ? '' : q.source)}">${esc(local ? t('web.quota.local') : t('web.quota.official'))}</span>`;
  const unit = q.unit === 'credits' ? t('web.quota.credits') : t('web.quota.calls');
  const details = q.details || {};
  const amount = quotaMetric(!local && q.provider === 'tinyfish' ? t('web.quota.tinyfish_search_reference') : '', q.used, q.limit, q.remaining, unit);
  let breakdown = '';
  if (!local && q.provider === 'tinyfish') {
    breakdown = num(details.fetch_used) !== null && details.fetch_limit > 0
      ? quotaMetric(t('web.quota.fetch'), details.fetch_used, details.fetch_limit, Math.max(0, details.fetch_limit - details.fetch_used), unit)
      : `<div class="quota-warning">${esc(t('web.quota.fetch_failed'))}</div>`;
  } else if (!local && q.provider === 'tavily') {
    const counters = [
      [t('web.quota.plan_usage'), details.account_plan_usage, details.account_plan_limit],
      [t('web.quota.key_usage'), details.key_usage, details.key_limit],
      [t('web.quota.paygo_usage'), details.paygo_usage, details.paygo_limit],
    ];
    const pairs = counters.filter(([, used, limit]) => num(used) !== null || num(limit) !== null)
      .map(([label, used, limit]) => `${label} ${fmtInt(used)} / ${fmtInt(limit > 0 ? limit : null)}`);
    const search = num(details.search_usage);
    const extract = num(details.extract_usage);
    if (search !== null) pairs.push(`${t('web.quota.search')} ${fmtInt(search)}`);
    if (extract !== null) pairs.push(`${t('web.quota.extract')} ${fmtInt(extract)}`);
    const summary = [q.plan, ...pairs].filter(Boolean).join(' · ');
    if (summary) breakdown = `<div class="muted">${esc(summary)}</div>`;
  }
  const reset = showReset ? `<div class="muted">${esc(t('web.label.next_automatic_reset'))} · ${quotaResetHtml(q)}</div>` : '';
  return `${q.official_query_failed ? failed + ' · ' : ''}${source}${amount}${breakdown}${reset}`;
}

function renderQuotaManage() {
  const body = $('#quota-manage-body');
  const note = $('#quota-manage-note');
  if (!body || !note) return;
  const rows = state.quotas || [];
  setNote(note, quotaLoadNote() || (rows.length ? t('web.quota.local_policy') : t('web.quota.no_data')), state.quotaError ? 'warn' : '');
  body.innerHTML = rows.map(q => {
    const name = esc(displayName(q.provider));
    const actions = q.source === 'local' ? `<div class="row-actions"><button type="button" class="icon-btn" title="${t('web.action.reset')}${name}${t('web.label.usage')}" aria-label="${t('web.action.reset')}${name}${t('web.label.usage')}" data-quota-reset="${esc(q.provider)}">${icon('rotate-ccw')}</button> ` +
      `<button type="button" class="icon-btn" title="${t('web.action.adjust')}${name}${t('web.label.usage')}" aria-label="${t('web.action.adjust')}${name}${t('web.label.usage')}" data-quota-adjust="${esc(q.provider)}" data-used="${num(q.used) || 0}">${icon('pencil')}</button></div>` : '—';
    return `<tr><td>${name}</td><td class="quota-cell">${quotaUsageHtml(q, false)}</td><td>${quotaResetHtml(q)}</td><td>${actions}</td></tr>`;
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
  if (page === 'events') {
    page = 'usage';
    history.replaceState(null, '', '#usage');
  }
  if (!PAGES[page]) page = 'overview';
  if ($('.page.active')?.id !== 'page-' + page) window.scrollTo(0, 0);
  $$('.page').forEach(el => el.classList.toggle('active', el.id === 'page-' + page));
  $$('.nav').forEach(el => {
    const active = el.dataset.page === page;
    el.classList.toggle('active', active);
    if (active) el.setAttribute('aria-current', 'page');
    else el.removeAttribute('aria-current');
  });
  if (page !== 'tools' && $('#everything-scope-button')?.getAttribute('aria-expanded') === 'true') toggleEverythingScope();
  if (page === 'settings') loadStorage();
  $('#page-title').textContent = PAGES[page];
  if (push !== false && location.hash !== '#' + page) history.pushState(null, '', '#' + page);
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
  $('#save-tools').addEventListener('click', saveSettings);
  $('#everything-scope-button').addEventListener('click', toggleEverythingScope);
  wireDiagnostics();
  $('#undo-settings').addEventListener('click', discardDraft);
  $('#undo-sources').addEventListener('click', discardDraft);
  $('#undo-tools').addEventListener('click', discardDraft);
  $('#retry-apply').addEventListener('click', restartAndVerify);
  $('#restore-backup').addEventListener('click', restoreBackup);
  $('#backup-select').addEventListener('change', renderWriteAccess);
  $('#page-sources').addEventListener('change', e => {
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
  $('#refresh-storage').addEventListener('click', loadStorage);
  $('#clear-cache').addEventListener('click', e => clearStorage(e.currentTarget, '/__admin/api/cache/clear',
    t('web.action.clear_search_cache'), t('web.dialog.this_cannot_be_undone_but_monitoring_history_is_not_deleted')));
  $('#clear-history').addEventListener('click', e => clearStorage(e.currentTarget, '/__admin/api/history/clear',
    t('web.storage.clear_history'), t('web.storage.history_confirm')));
  $('#clear-expired-fetchdata').addEventListener('click', e => clearStorage(e.currentTarget, '/__admin/api/fetchdata/clear',
    t('web.storage.clear_expired'), t('web.storage.expired_confirm'), { scope: 'expired' }));
  $('#clear-fetchdata').addEventListener('click', e => clearStorage(e.currentTarget, '/__admin/api/fetchdata/clear',
    t('web.storage.clear_all'), t('web.storage.all_confirm'), { scope: 'all' }));
  document.addEventListener('input', markDirty);
  document.addEventListener('change', e => {
    const { orderKey } = e.target.dataset;
    if (orderKey && e.target.type === 'checkbox') changeOrderedConfig(orderKey, e.target.value, e.target.checked, 0);
  });
  document.addEventListener('click', e => {
    const release = e.target.closest('[data-cooldown-release]');
    if (release) releaseRoutingCooldown(release.dataset.cooldownRelease, release.dataset.cooldownName);
    if (e.target.closest('[data-quota-refresh]')) refreshOfficialQuotas();
    const jump = e.target.closest('[data-jump-section]');
    if (jump) {
      const target = document.getElementById(jump.dataset.jumpSection);
      target.setAttribute('tabindex', '-1');
      target.focus({ preventScroll: true });
      target.scrollIntoView({ block: 'start' });
    }
    const order = e.target.closest('[data-order-shift]');
    if (order) changeOrderedConfig(order.dataset.orderKey, order.dataset.orderName, true, Number(order.dataset.orderShift));
    const drawer = e.target.closest('[data-source-drawer]');
    if (drawer) {
      const id = drawer.dataset.sourceDrawer;
      const open = !state.openSources.has(id);
      if (open) state.openSources.add(id);
      else state.openSources.delete(id);
      document.getElementById('source-config-' + id).hidden = !open;
      drawer.setAttribute('aria-expanded', String(open));
      refreshHScroll();
    }
  });
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
    state.filters.application = $('#filter-application').value.trim();
    state.filters.project = $('#filter-project').value.trim();
    state.filters.cacheHit = $('#filter-cache').value;
    loadEvents();
  };
  ['#filter-status', '#filter-error-kind', '#filter-tool', '#filter-provider', '#filter-application', '#filter-project', '#filter-cache'].forEach(sel => {
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
mountConfigControls();
wire();
renderWriteAccess();
applyLocalTheme();
$('#accent-input').value = localStorage.getItem('accent') || '';
nav(location.hash.slice(1) || 'overview', false);
loadAll();
setInterval(() => { if (!document.hidden) loadAll(); }, REFRESH_MS);
setInterval(updateRoutingCooldowns, 1000);
