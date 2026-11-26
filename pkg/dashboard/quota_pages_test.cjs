'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

test('API sources and quota settings share official usage, warnings and local fallback', async () => {
  const rows = [
    { provider: 'tavily', source: 'https://api.tavily.com/usage', status: 'available', unit: 'credits',
      used: 850, limit: 1000, remaining: 150, plan: 'Bootstrap',
      details: { account_plan_usage: 850, account_plan_limit: 1000, key_usage: 125, key_limit: 200 } },
    { provider: 'tinyfish', source: 'https://api.search.tinyfish.ai/usage', status: 'available', unit: 'calls',
      used: 3000, limit: 12000, remaining: 9000, reset_at: '2026-11-27T00:00:00Z',
      details: { fetch_used: 910, fetch_limit: 1000 } },
    { provider: 'exa', source: 'local', status: 'observed', unit: 'calls', used: 14, limit: 100, remaining: 86 },
  ];
  const elements = new Map();
  const element = selector => {
    if (!elements.has(selector)) {
      elements.set(selector, { innerHTML: '', textContent: '', classList: { toggle() {} } });
    }
    return elements.get(selector);
  };
  const context = vm.createContext({
    document: { documentElement: { lang: 'en' }, querySelector: element, querySelectorAll: () => [] },
    window: { __I18N__: JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'i18n', 'locales', 'web.en.json'), 'utf8')) },
    fetch: async url => {
      assert.equal(url, '/__admin/api/quotas');
      return { ok: true, json: async () => rows };
    },
  });
  const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
  vm.runInContext(source.slice(0, source.indexOf('// Remove credentials retained')), context);
  await vm.runInContext(`state.overview = { sources: ['tavily', 'tinyfish', 'exa'].map(id => ({ id, category: 'api' })) }; loadQuotas();`, context);

  const surfaces = ['#sources-api-body', '#quota-manage-body'];
  for (const surface of surfaces) {
    const html = element(surface).innerHTML;
    assert.match(html, /Official usage/);
    assert.match(html, /850 \/ 1,000 credits.*150/);
    assert.match(html, /Bootstrap.*125 \/ 200/);
    assert.match(html, /Search.*3,000 \/ 12,000/);
    assert.match(html, /Fetch.*910 \/ 1,000/);
    assert.match(html, /progress[^>]+value="91"/);
    assert.match(html, /Near limit/);
    assert.match(html, /2026-11-27T00:00:00Z/);
    assert.match(html, /Local counts.*14 \/ 100/);
    assert.doesNotMatch(html, /data-quota-(?:reset|adjust)="(?:tavily|tinyfish)"/);
  }
  assert.match(element('#quota-manage-body').innerHTML, /data-quota-reset="exa"/);

  rows[0] = { provider: 'tavily', source: 'local', status: 'observed', unit: 'calls', used: 24, limit: 100,
    remaining: 76, official_query_failed: true };
  rows[1].details = { fetch_error: 'existing upstream failure diagnostic' };
  await vm.runInContext('loadQuotas()', context);
  for (const surface of surfaces) {
    const html = element(surface).innerHTML;
    assert.match(html, /Official usage unavailable.*Local counts.*24 \/ 100/);
    assert.match(html, /Search.*3,000 \/ 12,000/);
    assert.match(html, /Fetch usage unavailable/);
  }
  assert.match(element('#quota-manage-body').innerHTML, /data-quota-adjust="tavily"/);
});
