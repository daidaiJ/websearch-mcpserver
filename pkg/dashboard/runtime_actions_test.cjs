'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

test('trusted connections can confirm runtime actions without admin credentials and refresh both views', async () => {
  const elements = new Map();
  const element = selector => {
    if (!elements.has(selector)) {
      elements.set(selector, { innerHTML: '', textContent: '', style: {}, setAttribute() {}, classList: { toggle() {}, add() {} } });
    }
    return elements.get(selector);
  };
  const requests = [];
  const context = vm.createContext({
    document: { documentElement: { lang: 'en' }, querySelector: element, querySelectorAll: selector =>
      selector === '[data-cooldown-release], [data-quota-refresh]' ? [element('[data-quota-refresh]')] : [] },
    window: { __I18N__: JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'i18n', 'locales', 'web.en.json'), 'utf8')) },
    setTimeout: () => {},
    fetch: async (url, options) => {
      requests.push([url, JSON.parse(options.body), options.headers['X-Admin-Password']]);
      const result = url.endsWith('/release') ? { cleared: 1 } : {
        refreshed: true, items: [
          { provider: 'tavily', source: 'https://api.tavily.com/usage', status: 'available', unit: 'credits',
            used: 12, limit: 100, remaining: 88 },
        ],
      };
      return { ok: true, json: async () => result };
    },
  });
  const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
  vm.runInContext(source.slice(0, source.indexOf('// Remove credentials retained')), context);
  vm.runInContext(`
    state.settings = { local_action_allowed: true, write_access: { allowed: false }, admin_password_configured: false };
    state.adminPass = 'fixture-password';
    state.overview = { sources: [{ id: 'tavily', category: 'api' }, { id: 'duckduckgo',
      actual_cooldown: { engine: 'duckduckgo', kind: 'rate_limit', until: new Date(Date.now() + 30_000).toISOString() }
    }] };
    renderSourcePages();
    renderWriteAccess();
    confirmDialog = async () => false;
  `, context);
  assert.match(element('#sources-web-body').innerHTML, /data-cooldown-release="duckduckgo"/);
  assert.equal(element('[data-cooldown-release="all"]').hidden, false);
  assert.equal(element('#restart-service').disabled, true);
  assert.equal(element('[data-quota-refresh]').disabled, false);
  await vm.runInContext("releaseRoutingCooldown('duckduckgo', 'DuckDuckGo')", context);
  await vm.runInContext('refreshOfficialQuotas()', context);
  assert.deepEqual(requests, []);

  vm.runInContext(`
    confirmDialog = async () => true;
    loadOverview = async () => {
      delete state.overview.sources[1].actual_cooldown;
      renderSourcePages();
    };
  `, context);
  await vm.runInContext("releaseRoutingCooldown('duckduckgo', 'DuckDuckGo')", context);
  assert.doesNotMatch(element('#sources-web-body').innerHTML, /data-cooldown-release/);
  assert.equal(element('[data-cooldown-release="all"]').hidden, true);

  await vm.runInContext('refreshOfficialQuotas()', context);
  for (const selector of ['#sources-api-body', '#quota-manage-body']) {
    assert.match(element(selector).innerHTML, /12 \/ 100 credits/);
  }
  assert.deepEqual(requests, [
    ['/__admin/api/sources/cooldowns/release', { engine: 'duckduckgo', confirm: true }, undefined],
    ['/__admin/api/quotas/refresh', { confirm: true }, undefined],
  ]);
  vm.runInContext('state.settings.local_action_allowed = false', context);
  await vm.runInContext("releaseRoutingCooldown('duckduckgo', 'DuckDuckGo')", context);
  await vm.runInContext('refreshOfficialQuotas()', context);
  assert.equal(requests.length, 2);
});

test('clearing local data and running a paid diagnostic use source permission and confirmation', async () => {
  const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
  const elements = new Map();
  const element = selector => {
    if (!elements.has(selector)) {
      elements.set(selector, {
        dataset: {}, innerHTML: '', textContent: '', style: {}, classList: { toggle() {}, add() {} },
        addEventListener(name, handler) { this[name] = handler; },
      });
    }
    return elements.get(selector);
  };
  const search = element('#diagnostic-search');
  const fetchForm = element('#diagnostic-fetch');
  const searchButton = element('#diagnostic-search button');
  const paidQuery = { source: 'tavily', query: 'fixture query' };
  const select = { value: 'tavily', replaceChildren() {} };
  search.elements = { namedItem: () => select };
  search.querySelector = selector => selector === 'button' ? searchButton : element('#search-result');
  fetchForm.querySelector = element;
  const db = { history: { enabled:true, count:4, bytes:128 } };
  const requests = [];
  const context = vm.createContext({
    AbortSignal,
    document: { documentElement: { lang: 'en' }, querySelector: element, querySelectorAll: () => [] },
    window: { __I18N__: JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'i18n', 'locales', 'web.en.json'), 'utf8')) },
    FormData: class { [Symbol.iterator]() { return Object.entries(paidQuery)[Symbol.iterator](); } },
    Option: class {},
    setTimeout: () => {},
    fetch: async (url, options) => {
      if (url.endsWith('/storage')) return { ok: true, json: async () => db };
      requests.push({ url, payload: JSON.parse(options.body), headers: options.headers });
      if (url.endsWith('/history/clear')) db.history.count = 0;
      return { ok: true, json: async () => ({ ok: true, cleared: 4, elapsed_ms: 1, result_count: 1 }) };
    },
  });
  vm.runInContext(source.slice(0, source.indexOf('// Remove credentials retained')), context);
  vm.runInContext(`
    state.settings = {local_action_allowed: true, config_error: 'fixture configuration error'};
    state.adminPass = 'cached-admin'; state.adminUser = 'cached-user';
    state.overview = {diagnostics: {sources: [{id:'tavily',name:'Tavily',confirm:true}]}};
    renderDiagnosticControls();
    wireDiagnostics();
    globalThis.confirmations = [];
    globalThis.proceed = false;
    confirmDialog = async (title, copy) => { confirmations.push(copy); return proceed; };
  `, context);
  await vm.runInContext('loadStorage()', context);
  assert.equal(requests.length, 0, 'loading the controls must not trigger diagnostics or cleanup');
  assert.equal(searchButton.disabled, false);
  assert.equal(element('#clear-history').disabled, false);
  await search.submit({ preventDefault() {} });
  assert.equal(requests.length, 0, 'cancelling paid diagnostic must make no request');
  assert.match(vm.runInContext('confirmations.at(-1)', context), /one search with Tavily.*quota/);
  vm.runInContext('proceed = true', context);
  await search.submit({ preventDefault() {} });
  assert.deepEqual(requests[0].payload, { ...paidQuery, confirm: true });
  assert.match(element('#search-result').textContent, /Success/);
  await vm.runInContext("clearStorage($('#clear-history'), '/__admin/api/history/clear', 'History', 'Clear history?')", context);
  assert.equal(requests[1].url, '/__admin/api/history/clear');
  assert.equal(requests[1].payload.confirm, true);
  assert.equal(element('#storage-history-count').textContent, '0');
  for (const { headers } of requests) {
    assert.equal(headers['X-Admin-Password'], undefined);
    assert.equal(headers['X-Admin-User'], undefined);
  }
  vm.runInContext('state.settings.local_action_allowed = false; renderDiagnosticControls(); renderStorage();', context);
  assert.equal(searchButton.disabled, true);
  assert.equal(element('#clear-history').disabled, true);
  await search.submit({ preventDefault() {} });
  await vm.runInContext("clearStorage($('#clear-history'), '/__admin/api/history/clear', 'History', 'Clear history?')", context);
  assert.equal(requests.length, 2);
});
