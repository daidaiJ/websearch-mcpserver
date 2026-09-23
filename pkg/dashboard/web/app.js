'use strict';
/* WebSearch Control Center - dependency-free frontend. */

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const REFRESH_MS = 60000;
const TZ = { hour12: false, timeZone: 'Asia/Shanghai' };
const PAGES = { overview: '运行总览', sources: '搜索源', events: '事件', usage: '调用记录', settings: '设置' };
const HEALTH = { healthy: '正常', degraded: '不稳定', down: '异常', unknown: '未知' };
const ERROR_KINDS = {
  rate_limit: '限流', captcha: '验证码', access_denied: '访问被拒', timeout: '超时',
  parse: '解析失败', no_result: '无结果', network: '网络/服务端', unknown: '未知错误',
};
const SYSTEM = { healthy: '运行正常', degraded: '部分降级', down: '部分异常', waiting: '等待真实调用' };
const DOT = { healthy: 'healthy', degraded: 'degraded', down: 'down', unknown: 'unknown' };
const DISPLAY = {
  smartsearch: '智能搜索', academicsearch: '学术检索', cleanfetch: '网页抓取', pdf_parser: 'PDF 解析',
  anysearch: 'AnySearch', baidu: '百度千帆', tavily: 'Tavily', exa: 'Exa', doubao: '豆包搜索',
  baidu_web: '百度网页', bing: 'Bing', google: 'Google', duckduckgo: 'DuckDuckGo',
  arxiv: 'arXiv', crossref: 'Crossref', openalex: 'OpenAlex', pubmed: 'PubMed', europepmc: 'Europe PMC',
  dblp: 'DBLP', doaj: 'DOAJ', semantic_scholar: 'Semantic Scholar', google_scholar: 'Google Scholar',
  webfetch: '网页抓取器', jina: 'Jina Reader', pdf_pipeline: 'PDF 流水线',
};
const SECRETS = {
  BAIDU_SK: '百度千帆', TAVILY_SK: 'Tavily', EXA_API_KEY: 'Exa', ANYSEARCH_API_KEY: 'AnySearch',
  DOUBAO_SEARCH_API_KEY: '豆包', JINA_API_KEY: 'Jina Reader', MINERU_TOKEN: 'MinerU',
};
// 各供应商的官方「获取 Key」入口，配置页一键直达
const SECRET_LINKS = {
  BAIDU_SK: 'https://console.bce.baidu.com/iam/#/iam/apikey/list',
  TAVILY_SK: 'https://app.tavily.com/home',
  EXA_API_KEY: 'https://dashboard.exa.ai/api-keys',
  ANYSEARCH_API_KEY: 'https://www.anysearch.com/console/api-keys',
  DOUBAO_SEARCH_API_KEY: 'https://console.volcengine.com/search-infinity/api-key',
  JINA_API_KEY: 'https://jina.ai/api-keys',
  MINERU_TOKEN: 'https://mineru.net/apiManage/token',
};

const state = {
  overview: null, overviewError: null, overviewStale: false,
  quotas: null, quotaError: null,
  clients: { rows: null, error: null, tab: '__total__' },
  settings: null, settingsError: null, settingsDirty: false, settingsLoaded: false,
  events: { rows: null, error: null, serverFiltered: true },
  providerEvents: { rows: null, error: null },
  filters: { kind: 'provider', status: '', errorKind: '', tool: '', provider: '', requestId: '', limit: 50 },
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
function formatP95(s) {
  return s && num(s.p95_ms) > 0 ? fmtMS(s.p95_ms) : '<span class="dim">—</span>';
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
function hasFilter() {
  const f = state.filters;
  return Boolean(f.kind || f.status || f.errorKind || f.tool || f.provider || f.requestId);
}
function matchesFilters(e) {
  const f = state.filters;
  if (f.status === 'success' && !e.success) return false;
  if (f.status === 'failure' && e.success) return false;
  if (f.errorKind && (e.error_kind || '') !== f.errorKind) return false;
  if (f.requestId && (e.request_id || '') !== f.requestId) return false;
  if (f.tool && (e.kind !== 'tool' || e.tool !== f.tool)) return false;
  if (f.provider && (e.kind !== 'provider' || e.provider !== f.provider)) return false;
  if (f.tool && f.provider) return false;
  if (!f.tool && !f.provider && f.kind && e.kind !== f.kind) return false;
  return true;
}
function activationOf(s) {
  if (s.configured === false) return { key: 'not_configured', label: '未配置' };
  if (s.active === true) return { key: 'active', label: '当前启用' };
  if (s.active === false) return { key: 'inactive', label: '已配置未使用' };
  return { key: 'unknown', label: '未知' };
}
function healthOf(s) {
  if (s.active === false || s.configured === false) return { dot: 'unknown', label: '—' };
  if (!s.last_seen_at) return { dot: 'unknown', label: '尚无调用' };
  return { dot: DOT[s.status] || 'unknown', label: HEALTH[s.status] || '未知' };
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
  if (body === null) throw new Error('响应不是有效的 JSON');
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
  try {
    const data = await fetchJSON('/__admin/api/clients');
    state.clients.rows = Array.isArray(data.clients) ? data.clients : [];
    state.clients.error = null;
  } catch (e) {
    state.clients.error = e.message;
  }
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
    const data = await fetchJSON('/__admin/api/settings');
    if (!data || !data.values) throw new Error('设置响应缺少 values 字段');
    state.settings = data;
    state.settingsError = null;
  } catch (e) {
    state.settingsError = e.message;
  }
  renderConfig();
  if (!state.settingsLoaded || !state.settingsDirty) renderSettingsForm();
}

async function loadEvents() {
  const requestId = (state.events.requestId || 0) + 1;
  state.events.requestId = requestId;
  const f = state.filters;
  const params = new URLSearchParams();
  params.set('limit', String(f.limit));
  if (f.kind) params.set('kind', f.kind);
  if (f.status) params.set('status', f.status);
  if (f.errorKind) params.set('error_kind', f.errorKind);
  if (f.requestId) params.set('request_id', f.requestId);
  const tool = f.tool || '';
  const provider = f.provider || '';
  if (tool && provider) {
    params.set('kind', 'none');
    params.set('source', '');
  } else if (tool) {
    params.set('kind', 'tool');
    params.set('source', tool);
  } else if (provider) {
    params.set('kind', 'provider');
    params.set('source', provider);
  }
  try {
    const rows = await fetchJSON('/__admin/api/events?' + params.toString());
    if (state.events.requestId !== requestId) return;
    state.events.rows = Array.isArray(rows) ? rows : [];
    state.events.error = null;
    state.events.serverFiltered = !hasFilter() || state.events.rows.every(matchesFilters);
  } catch (e) {
    if (state.events.requestId !== requestId) return;
    state.events.error = e.message;
  }
  renderUsage();
  renderUsageSourceOptions();
}

async function loadProviderEvents() {
  try {
    const rows = await fetchJSON('/__admin/api/events?limit=20&kind=provider');
    state.providerEvents.rows = (Array.isArray(rows) ? rows : []).filter(e => e && e.kind === 'provider');
    state.providerEvents.error = null;
  } catch (e) {
    state.providerEvents.error = e.message;
  }
  renderOverviewEvents();
}

async function loadAll() {
  await Promise.allSettled([loadOverview(), loadQuotas(), loadSettings(), loadEvents(), loadProviderEvents()]);
  state.lastLoad = Date.now();
  renderUpdated();
  refreshHScroll();
}

function anyFailure() {
  return Boolean(state.overviewError || state.quotaError || state.settingsError || state.events.error || state.providerEvents.error || state.overviewStale);
}

function renderUpdated() {
  const el = $('#updated');
  if (!el) return;
  el.textContent = '更新于 ' + fmtClock() + (anyFailure() ? ' · 部分数据可能已过期' : '');
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
    setNote(note, state.overviewError ? '读取失败：' + state.overviewError : '正在读取…', state.overviewError ? 'bad' : '');
  } else if (!sys) {
    label.textContent = '未知';
    d.className = 'dot unknown';
    setNote(note, '数据可能已过期：概览接口未返回系统摘要（system）', 'warn');
  } else {
    label.textContent = SYSTEM[sys.status] || '未知';
    d.className = 'dot ' + (sys.status === 'waiting' ? 'unknown' : (DOT[sys.status] || 'unknown'));
    const summary = `已启用 ${fmtInt(sys.enabled)} 个 · 已观察 ${fmtInt(sys.observed)} 个 · 异常 ${fmtInt(sys.down)} 个` +
    (num(sys.suspended) > 0 ? ` · 熔断 ${fmtInt(sys.suspended)} 个` : '');
    if (state.overviewError) setNote(note, summary + ' · 数据可能已过期：' + state.overviewError, 'warn');
    else if (sys.status === 'waiting') setNote(note, summary + ' · 尚未观察到近期真实调用', '');
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
    ['今日成功调用', today ? fmtInt(today.successes) : '—', today ? `共 ${fmtInt(today.requests)} 次 · 缓存命中 ${fmtInt(today.cache_hits)}` : '暂无工具层调用'],
    ['今日失败次数', today ? fmtInt(today.failures) : '—', '仅统计 MCP 工具层事件'],
    ['今日平均响应时间', avg === null ? '—' : fmtMS(avg), today && num(today.requests) > 0 ? '工具层总耗时 ÷ 调用数' : '无调用时不计算'],
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
    list.innerHTML = `<p class="empty">${esc(o ? '概览接口未返回工具清单' : '正在读取…')}</p>`;
    if (note) note.textContent = '—';
    if (help) setNote(help, state.overviewError ? '读取失败：' + state.overviewError : '', state.overviewError ? 'bad' : '');
    return;
  }
  const observed = new Map();
  (Array.isArray(o.tools) ? o.tools : []).forEach(h => h && observed.set(h.name, h));
  const enabledCount = configured.filter(t => t.enabled).length;
  if (note) note.textContent = enabledCount + ' / ' + configured.length + ' 已启用';
  if (help) setNote(help, '开关状态来自运行配置；观测状态来自真实调用，尚无调用不会被当成异常', '');
  list.innerHTML = configured.map(tool => {
    const health = observed.get(tool.name);
    let stateLabel;
    let dotClass;
    if (!tool.enabled) {
      stateLabel = '配置未启用';
      dotClass = 'unknown';
    } else if (health && health.last_seen_at) {
      stateLabel = HEALTH[health.status] || '未知';
      dotClass = DOT[health.status] || 'unknown';
    } else {
      stateLabel = '可调用 · 尚无观测';
      dotClass = 'unknown';
    }
    const stats = health && health.today
      ? `<small>${fmtInt(health.today.requests)} 次 · ${fmtInt(health.today.successes)} 成功 · ${fmtInt(health.today.failures)} 失败</small>`
      : '<small>尚未产生工具层事件</small>';
    return `<div class="tool-row"><span class="tool-head">${dot(dotClass)}` +
      `<b>${esc(tool.label || displayName(tool.name))}</b><small class="mono">${esc(tool.name)}</small></span>` +
      `<span class="tool-state">${esc(stateLabel)}</span>` +
      `<p class="tool-stats">${stats}</p></div>`;
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

function quotaCell(s) {
  if (!s || s.quota_queryable !== true) return '<span class="dim">不可查询</span>';
  const q = tavilyItem();
  if (!q) return state.quotaError ? '<span class="dim">查询失败</span>' : '<span class="dim">尚未返回</span>';
  if (q.status === 'available') return `${esc(fmtInt(q.remaining))} / ${esc(fmtInt(q.limit))} ${esc(q.unit || '')}`.trim();
  if (q.status === 'not_configured') return '未配置';
  if (q.status === 'query_failed') return '<span class="bad-text">查询失败</span>';
  return '未知';
}

function healthStrip(s) {
  const outcomes = Array.isArray(s.recent_outcomes) ? s.recent_outcomes.slice(-20) : [];
  const cells = Array(Math.max(0, 20 - outcomes.length)).fill(null).concat(outcomes);
  const title = outcomes.length ? `最近 ${outcomes.length} 次真实调用` : '暂无真实调用';
  return `<span class="health-strip" title="${esc(title)}" aria-label="${esc(title)}">` +
    cells.map(ok => `<i class="${ok === null ? 'empty' : ok ? 'ok' : 'fail'}"></i>`).join('') + '</span>';
}

function sourceRowHtml(s, detailed) {
  const act = activationOf(s);
  const health = healthOf(s);
  const today = s.today && s.today.day ? s.today : null;
  const name = s.name || displayName(s.id);
  const id = s.id || '';
  const cells = [
    `<td class="src-name"><b>${esc(name)}</b>${id ? `<small class="dim">${esc(id)}</small>` : ''}</td>`,
    `<td><span class="tag ${esc(act.key)}">${esc(act.label)}</span></td>`,
    `<td class="nowrap">${s.active === false || s.configured === false ? '<span class="dim">—</span>' : dot(health.dot) + ' ' + esc(health.label) + suspendBadge(s)}</td>`,
    `<td>${healthStrip(s)}</td>`,
    `<td class="num">${today ? fmtInt(today.requests) : '<span class="dim">—</span>'}</td>`,
  ];
  if (detailed) {
    cells.push(`<td class="wrap-any">${formatErrorKinds(s.error_kinds)}</td>`);
  }
  if (detailed) {
    cells.push(`<td class="num">${today ? fmtInt(today.successes) + ' / ' + fmtInt(today.failures) : '<span class="dim">—</span>'}</td>`);
  }
  const successRate = today && num(today.requests) > 0 ? today.successes / today.requests : null;
  cells.push(`<td class="num">${successRate === null ? '<span class="dim">—</span>' : esc(Math.round(successRate * 100) + '%')}</td>`);
  const avg = today && num(today.requests) > 0 ? fmtMS(Math.round(today.duration_ms / today.requests)) : '<span class="dim">—</span>';
  cells.push(`<td class="num">${avg} <small class="dim">/ ${formatP95(s)}</small></td>`);
  if (detailed) {
    cells.push(`<td class="nowrap">${esc(fmtAt(s.last_seen_at))}</td>`);
  }
  cells.push(`<td>${quotaCell(s)}</td>`);
  if (detailed) {
    cells.push(`<td class="wrap-any">${s.last_error ? '<span class="bad-text">' + esc(s.last_error) + '</span>' : '<span class="dim">—</span>'}</td>`);
  }
  const rowClass = s.active === true && s.suspended === true ? 'row-suspended' : (s.active === true && s.status === 'down' ? 'row-down' : '');
  return `<tr class="${rowClass}">${cells.join('')}</tr>`;
}

function suspendBadge(s) {
  if (!s || s.suspended !== true) return '';
  const reason = s.suspend_reason ? errorKindLabel(s.suspend_reason) : '连续失败';
  const secs = num(s.suspend_countdown_sec);
  const countdown = secs && secs > 0 ? (secs >= 60 ? Math.ceil(secs / 60) + ' 分' : secs + ' 秒') : '';
  return `<span class="kind-chip suspend" title="${esc(reason)}${countdown ? '，约 ' + countdown + ' 后恢复' : ''}">熔断${countdown ? ' · ' + esc(countdown) : ''}</span>`;
}

function countsLabel(list) {
  let active = 0, inactive = 0, unconfigured = 0;
  list.forEach(s => {
    if (s.configured === false) unconfigured++;
    else if (s.active === true) active++;
    else if (s.active === false) inactive++;
    else unconfigured++;
  });
  return `启用 ${active} · 已配置未用 ${inactive} · 未配置 ${unconfigured}`;
}

function renderWebSources() {
  const body = $('#web-sources-body');
  const note = $('#web-sources-note');
  // 总览已移除 Web 来源面板；搜索源页使用 renderSourcePages。
  if (!body) return;
  const list = webSources();
  if (list === null) {
    body.innerHTML = emptyRow(6, state.overviewError ? '读取失败' : '正在读取…');
    setNote(note, state.overviewError ? '读取失败：' + state.overviewError : '', state.overviewError ? 'bad' : '');
    return;
  }
  const active = list.filter(s => s.active === true);
  const shown = active;
  body.innerHTML = shown.length ? shown.map(s => sourceRowHtml(s, false)).join('') : emptyRow(8, '当前模式没有已启用的 Web 来源');
  if (state.overviewError) setNote(note, '数据可能已过期：' + state.overviewError, 'warn');
  else if (!Array.isArray(state.overview.sources)) setNote(note, '数据可能已过期：接口未返回来源清单（sources），按已观测 Provider 展示', 'warn');
  else setNote(note, '', '');
}

function renderSourcePages() {
  const webList = webSources();
  const acadList = academicSources();

  const webBody = $('#sources-web-body');
  if (webList === null) {
    webBody.innerHTML = emptyRow(11, state.overviewError ? '读取失败' : '正在读取…');
    setNote($('#sources-web-note'), state.overviewError ? '读取失败：' + state.overviewError : '', state.overviewError ? 'bad' : '');
    setText('#sources-web-counts', '—');
  } else {
    webBody.innerHTML = webList.length ? webList.map(s => sourceRowHtml(s, true)).join('') : emptyRow(11, '暂无 Web 来源');
    setText('#sources-web-counts', countsLabel(webList));
    if (state.overviewError) setNote($('#sources-web-note'), '数据可能已过期：' + state.overviewError, 'warn');
    else if (!Array.isArray(state.overview.sources)) setNote($('#sources-web-note'), '数据可能已过期：接口未返回来源清单（sources）', 'warn');
    else setNote($('#sources-web-note'), '', '');
  }

  const acadBody = $('#sources-academic-body');
  if (acadList === null) {
    acadBody.innerHTML = emptyRow(11, state.overviewError ? '读取失败' : '正在读取…');
    setText('#sources-academic-counts', '—');
    setNote($('#sources-academic-note'), state.overviewError
      ? '读取失败：' + state.overviewError
      : '数据可能已过期：概览接口未返回学术来源清单（academic_sources）', state.overviewError ? 'bad' : 'warn');
  } else {
    acadBody.innerHTML = acadList.length ? acadList.map(s => sourceRowHtml(s, true)).join('') : emptyRow(11, '暂无学术来源');
    setText('#sources-academic-counts', countsLabel(acadList));
    setNote($('#sources-academic-note'), state.overviewError ? '数据可能已过期：' + state.overviewError : '', state.overviewError ? 'warn' : '');
  }
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
    value.textContent = '查询失败';
    dotEl.className = 'dot down';
    setNote(note, '数据可能已过期：' + state.quotaError, 'warn');
    details.innerHTML = '';
    return;
  }
  if (!q) {
    value.textContent = '—';
    dotEl.className = 'dot unknown';
    setNote(note, state.quotas ? '官方额度接口未返回 Tavily 条目' : '正在读取…', state.quotas ? 'bad' : '');
    details.innerHTML = '';
    return;
  }
  details.innerHTML = kv([
    ['已用', num(q.used) === null ? '' : fmtInt(q.used) + ' ' + (q.unit || '')],
    ['上限', num(q.limit) === null ? '' : fmtInt(q.limit) + ' ' + (q.unit || '')],
    ['套餐', q.plan || ''],
    ['更新时间', q.updated_at ? fmtAt(q.updated_at) : ''],
    ['官方接口', q.source || ''],
  ]);
  switch (q.status) {
    case 'available':
      value.textContent = `${fmtInt(q.remaining)} / ${fmtInt(q.limit)} ${q.unit || ''}`.trim();
      dotEl.className = 'dot healthy';
      setNote(note, state.quotaError ? '数据可能已过期：' + state.quotaError : '来自官方额度接口，非本地估算', state.quotaError ? 'warn' : '');
      break;
    case 'not_configured':
      value.textContent = '未配置';
      dotEl.className = 'dot unknown';
      setNote(note, '未配置 Tavily Key，无法查询官方额度', '');
      break;
    case 'query_failed':
      value.textContent = '查询失败';
      dotEl.className = 'dot down';
      setNote(note, q.error || '官方额度接口未返回可用数据', 'bad');
      break;
    default:
      value.textContent = '未知';
      dotEl.className = 'dot unknown';
      setNote(note, q.error || '官方额度接口返回了未识别的状态', 'warn');
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
    setNote(note, state.settingsError ? '读取失败：' + state.settingsError : '正在读取…', state.settingsError ? 'bad' : '');
    return;
  }
  const v = s.values || {};
  const yn = value => value === true ? '启用' : value === false ? '关闭' : '未知';
  const rows = [
    ['搜索模式', typeof v.mode === 'string' ? v.mode : ''],
    ['网络区域', v.network === 'china' ? '中国' : v.network === 'international' ? '国际' : ''],
    ['上游超时', num(v.upstream_timeout_sec) === null ? '' : String(v.upstream_timeout_sec) + ' 秒'],
    ['缓存', yn(v['cache.enabled'])],
    ['熔断暂停', [v['dashboard.suspension.ban_time_on_fail'], v['dashboard.suspension.max_ban_time_on_fail']].filter(Boolean).join(' / ')],
    ['已启用来源', state.overview && state.overview.system ? fmtInt(state.overview.system.enabled) + ' 个' : ''],
  ];
  list.innerHTML = rows.filter(r => r[1]).map(r => `<div><dt>${esc(r[0])}</dt><dd>${esc(r[1])}</dd></div>`).join('');
  secrets.textContent = '';
  if (state.settingsError) setNote(note, '数据可能已过期：' + state.settingsError, 'warn');
  else setNote(note, '重启后生效的当前配置', '');
}

/* ---------- events ---------- */

function eventCells(e, cols) {
  const out = [`<td class="nowrap">${esc(fmtAt(e.occurred_at))}</td>`];
  if (cols === 'usage') {
    out.push(`<td>${esc(e.kind === 'provider' ? '来源' : '工具')}</td>`);
  }
  const detail = e.detail ? `<small class="source-detail">${esc(e.detail)}</small>` : '';
  const requestLine = e.request_id
    ? `<small class="mono request-id" data-request="${esc(e.request_id)}" title="只看这一次调用">${esc(e.request_id)}</small>`
    : '';
  const chainLine = e.attempt_chain ? `<small class="dim">${esc(e.attempt_chain)}</small>` : '';
  const toolLabel = e.kind === 'tool'
    ? `<span class="source-name">${esc(displayName(e.tool))}</span>${requestLine}${chainLine}`
    : '<span class="dim">—</span>';
  const providerLabel = e.provider
    ? `<span class="source-name">${esc(displayName(e.provider))}</span>${detail}`
    : '<span class="dim">—</span>';
  // 事件页只有 provider 事件流，不带恒为空的工具列
  if (cols === 'usage') {
    out.push(`<td>${toolLabel}</td>`);
  }
  out.push(
    `<td>${providerLabel}</td>`,
    `<td class="${e.success ? 'ok-text' : 'bad-text'}">${e.success ? '成功' : '失败'}</td>`,
  );
  if (cols === 'usage') {
    out.push(`<td>${errorKindChip(e.error_kind)}</td>`);
  }
  out.push(
    `<td class="num">${esc(fmtMS(e.duration_ms))}</td>`,
    `<td class="num">${fmtInt(e.result_count)}</td>`,
  );
  if (cols !== 'usage') {
    out.push(`<td>${esc([e.query_topic, e.query_language].filter(Boolean).join(' / ') || '—')}</td>`);
    out.push(`<td class="wrap-any">${esc(e.query_keywords || '—')}</td>`);
    out.push(`<td class="wrap-any">${e.error_summary ? '<span class="bad-text">' + esc(e.error_summary) + '</span>' : '<span class="dim">—</span>'}</td>`);
  } else {
    out.push(`<td>${e.cache_hit ? '是' : '否'}</td>`);
    out.push(`<td>${esc([e.query_topic, e.query_language].filter(Boolean).join(' / ') || '—')}</td>`);
    out.push(`<td class="wrap-any">${esc(e.query_keywords || '—')}</td>`);
    out.push(`<td class="mono">${esc(e.query_hash || '—')}</td>`);
    out.push(`<td class="wrap-any">${e.error_summary ? '<span class="bad-text">' + esc(e.error_summary) + '</span>' : '<span class="dim">—</span>'}</td>`);
  }
  return out;
}

function renderOverviewEvents() {
  const body = $('#overview-events-body');
  const note = $('#overview-events-note');
  const rows = state.providerEvents.rows;
  if (rows === null) {
    body.innerHTML = emptyRow(8, state.providerEvents.error ? '读取失败' : '正在读取…');
    setNote(note, state.providerEvents.error ? '读取失败：' + state.providerEvents.error : '', state.providerEvents.error ? 'bad' : '');
    return;
  }
  const top = rows.slice(0, 20);
  body.innerHTML = top.length
    ? top.map(e => `<tr>${eventCells(e, 'events').join('')}</tr>`).join('')
    : emptyRow(8, '暂无 Provider 事件（明细保留 30 天）');
  if (state.providerEvents.error) setNote(note, '数据可能已过期：' + state.providerEvents.error, 'warn');
  else if (!top.length) setNote(note, '来源层调用后才会出现真实事件，不做模拟', '');
  else setNote(note, '', '');
}

function renderUsage() {
  const body = $('#usage-body');
  const note = $('#events-note');
  const rows = state.events.rows;
  const exportBtn = $('#export-csv');
  if (rows === null) {
    body.innerHTML = emptyRow(13, state.events.error ? '读取失败' : '正在读取…');
    exportBtn.disabled = true;
    setNote(note, state.events.error ? '读取失败：' + state.events.error : '', state.events.error ? 'bad' : '');
    return;
  }
  const filtered = rows.filter(matchesFilters);
  body.innerHTML = filtered.length
    ? filtered.map(e => `<tr>${eventCells(e, 'usage').join('')}</tr>`).join('')
    : emptyRow(13, rows.length ? '当前筛选条件下没有匹配记录' : '暂无调用记录');
  exportBtn.disabled = filtered.length === 0;
  const parts = [];
  if (state.events.error) parts.push(['数据可能已过期：' + state.events.error, 'warn']);
  if (hasFilter() && !state.events.serverFiltered) parts.push(['后端未应用筛选参数，当前按已获取的 ' + rows.length + ' 条在本地筛选', 'warn']);
  if (state.filters.requestId) {
    parts.push(['仅看请求 ' + state.filters.requestId + ' · <a href="#usage" data-clear-request="1">清除</a>', 'warn']);
  }
  parts.push(['匹配 ' + filtered.length + ' 条 / 已获取 ' + rows.length + ' 条 · 查询全文不保存', '']);
  note.innerHTML = parts.filter(p => p[0]).map(p => `<span${p[1] ? ' class="' + p[1] + '"' : ''}>${p[0]}</span>`).join(' · ');
  $$('[data-clear-request]', note).forEach(link => {
    link.onclick = event => {
      event.preventDefault();
      state.filters.requestId = '';
      loadEvents();
    };
  });
}

/* ---------- CSV export (UTF-8 BOM) ---------- */

function csvCell(value) {
  const s = value === null || value === undefined ? '' : String(value);
  return '"' + s.replace(/"/g, '""').replace(/[\r\n]+/g, ' ') + '"';
}
function stamp() {
  const d = new Date();
  const p = n => String(n).padStart(2, '0');
  return d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) + '-' + p(d.getHours()) + p(d.getMinutes()) + p(d.getSeconds());
}
function exportCSV() {
  const rows = (state.events.rows || []).filter(matchesFilters);
  if (!rows.length) {
    toast('当前没有可导出的记录', true);
    return;
  }
  const head = ['时间', '层级', '工具', '来源', '解析来源', '状态', '错误类型', '耗时(ms)', '结果数', '缓存命中', '主题', '语言', '关键词', '查询哈希', '查询字符数', '错误摘要'];
  const lines = [head.map(csvCell).join(',')];
  rows.forEach(e => {
    lines.push([
      fmtAt(e.occurred_at),
      e.kind === 'provider' ? '来源' : '工具',
      e.kind === 'tool' ? displayName(e.tool) : '',
      e.provider ? displayName(e.provider) : '',
      e.detail || '',
      e.success ? '成功' : '失败',
      errorKindLabel(e.error_kind),
      num(e.duration_ms) === null ? '' : e.duration_ms,
      num(e.result_count) === null ? '' : e.result_count,
      e.cache_hit ? '是' : '否',
      e.query_topic || '',
      e.query_language || '',
      e.query_keywords || '',
      e.query_hash || '',
      num(e.query_chars) === null ? '' : e.query_chars,
      e.error_summary || '',
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
  toast('已导出 ' + rows.length + ' 条记录（UTF-8 BOM）');
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
  [state.events.rows, state.providerEvents.rows].forEach(list => {
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
  const options = ['<option value="">全部</option>'];
  if (current && !known.has(current)) {
    options.push(`<option value="${esc(current)}">${esc(displayName(current))}</option>`);
  }
  entries.forEach(([id, label]) => {
    const text = label && label !== id ? label + '（' + id + '）' : id;
    options.push(`<option value="${esc(id)}">${esc(text)}</option>`);
  });
  sel.innerHTML = options.join('');
  sel.value = current;
}

function knownErrorKinds() {
  const map = new Map();
  const add = kind => { if (kind) map.set(kind, errorKindLabel(kind)); };
  [state.events.rows, state.providerEvents.rows].forEach(list => {
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

function renderSettingsForm() {
  const s = state.settings;
  if (!s || !s.values) return;
  $$('[data-key]').forEach(el => {
    const value = s.values[el.dataset.key];
    if (el.type === 'checkbox') el.checked = value === true;
    else el.value = value === null || value === undefined ? '' : value;
  });
  $('#settings-dirty').classList.add('hidden');
  state.settingsDirty = false;

  const flags = s.secrets || {};
  $('#secret-list').innerHTML = Object.keys(flags).map(key => {
    const on = flags[key] === true;
    return `<div class="secret-row">${dot(on ? 'healthy' : 'unknown')}` +
      `<div><b>${esc(SECRETS[key] || key)}</b><small>${on ? '已配置' : '未配置'}</small></div>` +
      `<button type="button" data-secret="${esc(key)}">${on ? '替换' : '设置'}</button></div>`;
  }).join('');
  $$('[data-secret]').forEach(b => { b.onclick = () => editSecret(b.dataset.secret); });
  $$('[data-request]').forEach(el => {
    el.onclick = () => {
      state.filters.requestId = el.dataset.request;
      loadEvents();
    };
  });
  state.settingsLoaded = true;
}

function changes() {
  if (!state.settings) return {};
  const out = {};
  $$('[data-key]').forEach(el => {
    const value = el.type === 'checkbox' ? el.checked : el.type === 'number' ? Number(el.value) : el.value;
    const before = state.settings.values[el.dataset.key];
    if (JSON.stringify(value) !== JSON.stringify(before)) out[el.dataset.key] = value;
  });
  return out;
}

function markDirty() {
  const dirty = Object.keys(changes()).length > 0;
  state.settingsDirty = dirty;
  $('#settings-dirty').classList.toggle('hidden', !dirty);
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
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json', ...(headers || {}) },
    body: JSON.stringify(payload),
  });
}

/* ---------- 管理员口令（写操作统一闸门） ---------- */
// 服务端约束：写端点仅限本机 loopback + X-Admin-Password 头；
// 口令只能配置在 dashboard.yaml，控制台永远读不到也改不了它。

function adminConfigured() {
  return Boolean(state.settings && state.settings.admin_password_configured);
}

function cachedAdminPass() {
  return sessionStorage.getItem('adminPass') || '';
}

function usernameConfigured() {
  return Boolean(state.settings && state.settings.admin_username_configured);
}

function cachedAdminUser() {
  return sessionStorage.getItem('adminUser') || '';
}

// adminHeaders 取得（必要时先问一次）管理员口令；未配置或用户取消时抛错。
async function adminHeaders() {
  if (!adminConfigured()) {
    throw new Error('管理员口令未配置：请在 dashboard.yaml 设置 dashboard.admin_password 后重启服务');
  }
  let pass = cachedAdminPass();
  if (!pass) {
    pass = prompt('输入管理员口令（dashboard.admin_password）');
    if (!pass) throw new Error('已取消：本操作需要管理员口令');
    sessionStorage.setItem('adminPass', pass);
  }
  const headers = { 'X-Admin-Password': pass };
  // admin_username 配置了才要求用户名；未配置 = 口令-only，不做检查
  if (usernameConfigured()) {
    let user = cachedAdminUser();
    if (!user) {
      user = prompt('输入管理员用户名（dashboard.admin_username）');
      if (!user) throw new Error('已取消：本操作需要管理员用户名');
      sessionStorage.setItem('adminUser', user);
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
    if (String(e.message).includes('口令')) sessionStorage.removeItem('adminPass');
    if (String(e.message).includes('用户名')) sessionStorage.removeItem('adminUser');
    throw e;
  }
}

async function saveSettings() {
  const diff = changes();
  if (!Object.keys(diff).length) {
    toast('没有需要保存的更改');
    return;
  }
  try {
    await securePost('/__admin/api/settings', { changes: diff, confirm: false });
    if (!(await confirmDialog('保存设置', '将备份当前 YAML，再写入以下更改。重启后生效。', JSON.stringify(diff, null, 2)))) return;
    const res = await securePost('/__admin/api/settings', { changes: diff, confirm: true });
    toast(res.backup ? '已保存，备份：' + res.backup : '已保存');
    await loadSettings();
  } catch (e) {
    toast(e.message, true);
  }
}

function editSecret(name) {
  const label = SECRETS[name] || name;
  const d = $('#secret-dialog');
  $('#secret-dialog-title').textContent = '更新 ' + label + ' Key';
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
    if (d.returnValue !== 'confirm') return;
    const value = $('#secret-dialog-input').value;
    const pass = $('#secret-dialog-pass').value.trim();
    if (!adminConfigured()) {
      toast('管理员口令未配置：请在 dashboard.yaml 设置 dashboard.admin_password 后重启服务', true);
      return;
    }
    if (!pass) {
      toast('需要管理员口令', true);
      return;
    }
    sessionStorage.setItem('adminPass', pass);
    const headers = { 'X-Admin-Password': pass };
    if (usernameConfigured()) {
      const user = $('#secret-dialog-user').value.trim();
      if (!user) {
        toast('需要管理员用户名', true);
        return;
      }
      sessionStorage.setItem('adminUser', user);
      headers['X-Admin-User'] = user;
    }
    try {
      await postJSON('/__admin/api/secrets', { name: name, value: value, confirm: true }, headers);
      toast(label + ' 已保存，需要重启服务');
      await loadSettings();
    } catch (e) {
      if (String(e.message).includes('口令')) sessionStorage.removeItem('adminPass');
      if (String(e.message).includes('用户名')) sessionStorage.removeItem('adminUser');
      toast(e.message, true);
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
  const title = brand.title || 'WebSearch 控制中心';
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
    setNote(note, '读取失败：' + state.quotaError, 'bad');
    return;
  }
  const rows = (state.quotas || []).filter(q => q.source === 'local');
  if (!rows.length) {
    body.innerHTML = '';
    setNote(note, '暂无本地用量数据：启用 dashboard 后的真实调用会自动累计（单 Key 上限默认 1000 次/周期，展示上限自动乘以 Key 数；可在 dashboard.yaml 的 dashboard.quotas 调整）。', '');
    return;
  }
  setNote(note, '用量来自本地真实调用（成功次数）；上限 = 单 Key 上限 × 已配置 Key 数，与 apipool 实际消耗容量对齐。重置与修正需管理员口令，且仅限本机操作。', '');
  body.innerHTML = rows.map(q => {
    const used = q.used == null ? '—' : fmtInt(q.used);
    const limit = q.limit == null ? '—' : fmtInt(q.limit);
    const reset = q.reset_at ? fmtAt(q.reset_at) : '仅手动';
    const name = esc(DISPLAY[q.provider] || q.provider);
    return `<tr><td>${name}</td><td class="num">${used} / ${limit}</td><td>${esc(reset)}</td>` +
      `<td class="nowrap"><button type="button" data-quota-reset="${esc(q.provider)}">重置</button> ` +
      `<button type="button" data-quota-adjust="${esc(q.provider)}" data-used="${q.used || 0}">修正</button></td></tr>`;
  }).join('');
}

async function quotaReset(provider) {
  try {
    await securePost('/__admin/api/quotas/reset', { provider: provider });
    toast(DISPLAY[provider] || provider + ' 已重置，从现在重新累计');
    await loadQuotas();
  } catch (e) {
    toast(e.message, true);
  }
}

async function quotaAdjust(provider, current) {
  const raw = prompt('把「' + (DISPLAY[provider] || provider) + '」的已用次数修正为：', String(current));
  if (raw === null) return;
  const value = Number(raw);
  if (!Number.isFinite(value) || value < 0) {
    toast('请输入非负数字', true);
    return;
  }
  try {
    await securePost('/__admin/api/quotas/adjust', { provider: provider, set_used: value });
    toast(DISPLAY[provider] || provider + ' 用量已修正为 ' + value);
    await loadQuotas();
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 客户端用量（User-Agent 分组，tab 切换） ---------- */

function renderClientsPanel() {
  const panel = $('#clients-panel');
  const tabs = $('#client-tabs');
  const stats = $('#client-stats');
  const note = $('#client-note');
  if (!panel || !tabs || !stats) return;
  const rows = state.clients.rows || [];
  if (!rows.length) {
    // 没有可识别的客户端数据时整块隐藏，不出空 tab 页
    panel.classList.add('hidden');
    return;
  }
  panel.classList.remove('hidden');
  const tabDefs = [{ key: '__total__', label: '总计' }].concat(rows.map(r => ({ key: r.client, label: r.client })));
  tabs.innerHTML = tabDefs.map(t =>
    `<button type="button" class="client-tab${t.key === state.clients.tab ? ' active' : ''}" data-client-tab="${esc(t.key)}">${esc(t.label)}</button>`).join('');
  $$('#client-tabs [data-client-tab]').forEach(b => b.addEventListener('click', () => {
    state.clients.tab = b.dataset.clientTab;
    renderClientsPanel();
  }));

  if (state.clients.error) {
    stats.innerHTML = '';
    setNote(note, '读取失败：' + state.clients.error, 'bad');
    return;
  }
  if (!rows.length) {
    stats.innerHTML = '<div class="data-table"><table><tbody><tr><td class="empty">还没有可识别的客户端调用：接入 MCP 客户端后按 User-Agent 自动归组</td></tr></tbody></table></div>';
    setNote(note, '');
    return;
  }
  setNote(note, '');
  const selected = tabDefs.find(t => t.key === state.clients.tab) || tabDefs[0];
  let agg;
  if (selected.key === '__total__') {
    agg = rows.reduce((acc, r) => ({
      requests: acc.requests + r.requests, successes: acc.successes + r.successes, failures: acc.failures + r.failures,
      weighted: acc.weighted + r.avg_ms * r.requests,
      tools: acc.tools,
    }), { requests: 0, successes: 0, failures: 0, weighted: 0, tools: null });
  } else {
    const r = rows.find(x => x.client === selected.key);
    agg = r ? { requests: r.requests, successes: r.successes, failures: r.failures, weighted: r.avg_ms * r.requests, tools: r.tools } : null;
  }
  if (!agg) {
    stats.innerHTML = '';
    return;
  }
  const avg = agg.requests ? Math.round(agg.weighted / agg.requests) : 0;
  const cards = [
    ['调用数', fmtInt(agg.requests)],
    ['成功', fmtInt(agg.successes)],
    ['失败', fmtInt(agg.failures)],
    ['平均耗时', agg.requests ? fmtInt(avg) + ' ms' : '—'],
  ];
  let html = '<div class="kpi-grid client-kpis">' + cards.map((c, i) =>
    `<div class="kpi"><header><b>${esc(c[0])}</b></header><strong>${c[1]}</strong><small>最近 7 天</small></div>`).join('') + '</div>';
  if (agg.tools && agg.tools.length) {
    html += '<div class="table-scroll"><table class="data-table"><thead><tr><th>工具</th><th class="num">调用</th><th class="num">成功</th><th class="num">失败</th><th class="num">平均耗时</th></tr></thead><tbody>' +
      agg.tools.map(t => `<tr><td>${esc(DISPLAY[t.tool] || t.tool)}</td><td class="num">${fmtInt(t.requests)}</td><td class="num">${fmtInt(t.successes)}</td><td class="num">${fmtInt(t.failures)}</td><td class="num">${t.avg_ms ? fmtInt(t.avg_ms) + ' ms' : '—'}</td></tr>`).join('') +
      '</tbody></table></div>';
  }
  stats.innerHTML = html;
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
  if (!PAGES[page]) page = 'overview';
  $$('.page').forEach(el => el.classList.toggle('active', el.id === 'page-' + page));
  $$('.nav').forEach(el => el.classList.toggle('active', el.dataset.page === page));
  $('#page-title').textContent = PAGES[page];
  if (push !== false) history.replaceState(null, '', '#' + page);
  // 隐藏页的表格尺寸为 0，切页后重新判定溢出并挂粘性滚动条
  refreshHScroll();
}

function wire() {
  $$('.nav').forEach(btn => btn.addEventListener('click', () => nav(btn.dataset.page)));
  $$('[data-jump]').forEach(link => link.addEventListener('click', e => { e.preventDefault(); nav(link.dataset.jump); }));
  $('#refresh').addEventListener('click', () => loadAll());
  $('#reload-events').addEventListener('click', () => loadEvents());
  $('#export-csv').addEventListener('click', exportCSV);
  $('#save-settings').addEventListener('click', saveSettings);

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
      toast('已恢复主题默认主色');
      return;
    }
    if (!/^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$/.test(v)) {
      toast('主色格式无效，示例 #14633f', true);
      return;
    }
    localStorage.setItem('accent', v.toLowerCase());
    document.documentElement.style.setProperty('--accent', v.toLowerCase());
    toast('已应用自定义主色');
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
    if (!(await confirmDialog('重启 WebSearch', '当前请求会短暂中断，Docker 将自动重新启动服务。'))) return;
    try {
      await securePost('/__admin/api/restart', { confirm: true });
      toast('正在重启…');
      setTimeout(() => location.reload(), 3500);
    } catch (e) {
      toast(e.message, true);
    }
  });
  $('#clear-cache').addEventListener('click', async () => {
    if (!(await confirmDialog('清空搜索缓存', '此操作不可撤销，但不会删除监控历史。'))) return;
    try {
      const res = await securePost('/__admin/api/cache/clear', { confirm: true });
      toast('已清理 ' + fmtInt(res.cleared) + ' 条缓存');
    } catch (e) {
      toast(e.message, true);
    }
  });
  $$('[data-key]').forEach(el => el.addEventListener('change', markDirty));
  const applyFilter = () => {
    state.filters.status = $('#filter-status').value;
    state.filters.errorKind = $('#filter-error-kind').value;
    const tool = $('#filter-tool').value;
    const provider = $('#filter-provider').value;
    if (tool || provider) {
      // 工具/来源下拉优先；层级仅在下拉未选时生效。两者不可能同时命中。
      state.filters.kind = '';
      state.filters.tool = tool;
      state.filters.provider = provider;
    } else {
      state.filters.kind = $('#filter-kind').value;
      state.filters.tool = '';
      state.filters.provider = '';
    }
    state.filters.limit = Number($('#filter-limit').value) || 20;
    loadEvents();
  };
  ['#filter-kind', '#filter-status', '#filter-error-kind', '#filter-tool', '#filter-provider', '#filter-limit'].forEach(sel => {
    $(sel).addEventListener('change', applyFilter);
  });
  $('#filter-limit').value = String(state.filters.limit);
  $('#filter-kind').value = state.filters.kind;
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

wire();
applyLocalTheme();
$('#accent-input').value = localStorage.getItem('accent') || '';
nav(location.hash.slice(1) || 'overview', false);
loadAll();
setInterval(() => { if (!document.hidden) loadAll(); }, REFRESH_MS);
