'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const elements = new Map();
const callButtons = [];
function element(selector) {
  if (!elements.has(selector)) elements.set(selector, {
    innerHTML: '', textContent: '', value: '', disabled: false, style: {},
    classList: { toggle() {}, add() {}, remove() {} },
    querySelectorAll: selector => selector === '[data-call]' ? callButtons : [],
  });
  return elements.get(selector);
}
let exportedBlob;
const context = vm.createContext({
  console, AbortSignal, URLSearchParams, Blob, setTimeout: () => {},
  window: {__I18N__: JSON.parse(fs.readFileSync(path.join(__dirname,'..','i18n','locales','web.zh.json'),'utf8'))},
  URL: { createObjectURL(blob) { exportedBlob = blob; return 'blob:test'; }, revokeObjectURL() {} },
  document: {
    querySelector: element, querySelectorAll: () => [],
    createElement: () => ({click() {}, remove() {}}), body: {appendChild() {}},
  },
});
const source = fs.readFileSync(path.join(__dirname, 'web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(__dirname, 'web/index.html'), 'utf8');
const css = fs.readFileSync(path.join(__dirname, 'web/app.css'), 'utf8');
assert.doesNotMatch(html, /filter-kind|filter-limit|page-events|data-page="events"/);
assert.match(html, /class="data-table call-table"/);
assert.match(css, /\.data-table\{[^}]*table-layout:fixed/);
assert.match(css, /\.call-table tbody>tr\.call-row\{[^}]*height:auto/);
assert.doesNotMatch(css, /min-width:(1180|760)px/);
vm.runInContext(source.slice(0, source.search(/\/\/ Remove credentials retained|\nwire\(\);/)), context);
const run = code => vm.runInContext(code, context);
assert.equal(run(`csvCell('=SUM(1,2)')`), '"\'=SUM(1,2)"');
assert.equal(run(`csvCell(' @formula')`), '"\' @formula"');
assert.equal(run('csvCell(-12)'), '"-12"');

(async () => {
  const handler = source.match(/const applyFilter = ([\s\S]*?);\r?\n  \['#filter-status'/)[1];
  run('globalThis.applyFilters = ' + handler);
  run('globalThis.originalLoader = loadEvents; loadEvents = () => {};');
  element('#filter-tool').value = 'academicsearch';
  element('#filter-provider').value = 'arxiv';
  element('#filter-status').value = 'success';
  element('#filter-error-kind').value = 'timeout';
  run('applyFilters()');
  assert.equal(run('state.filters.tool'), 'academicsearch');
  assert.equal(run('state.filters.provider'), 'arxiv');
  run('loadEvents = originalLoader;');
  let requested;
  context.record = url => { requested = url; };
  run('fetchJSON = async url => { record(url); return []; };');
  await run('loadEvents()');
  const params = new URLSearchParams(requested.split('?')[1]);
  assert.equal(requested.split('?')[0], '/__admin/api/calls');
  for (const [key, value] of Object.entries({limit:'100', tool:'academicsearch',provider:'arxiv',status:'success',error_kind:'timeout'}))
    assert.equal(params.get(key), value);
  run(`state.events.rows = [{id:7,kind:'tool',tool:'academicsearch',request_id:'req-test',success:true,client:'websearch',application:'workbuddy',project:'Paper <demo>'}];
    fetchJSON = async url => { record(url); return [{kind:'provider',provider:'arxiv',success:false,error_summary:'<private>',error_kind:'timeout'}]; };`);
  callButtons.push({dataset:{call:'7'}});
  run('renderUsage()');
  assert.match(element('#usage-body').innerHTML, /data-call="7" aria-expanded="false"/);
  assert.match(element('#usage-body').innerHTML, /aria-controls="call-detail-7"/);
  const mainRow = element('#usage-body').innerHTML.split('</tr>')[0];
  assert.match(mainRow, /WorkBuddy/);
  assert.match(mainRow, /自报项目标签，未经系统核验/);
  assert.match(mainRow, /Paper &lt;demo&gt;/);
  assert.doesNotMatch(mainRow, /Paper <demo>/);
  await callButtons[0].onclick();
  assert.equal(requested, '/__admin/api/request-events?request_id=req-test');
  assert.match(element('#usage-body').innerHTML, /aria-expanded="true"/);
  assert.match(element('#usage-body').innerHTML, /&lt;private&gt;/);
  assert.match(element('#usage-body').innerHTML, /ok-text">成功/);
  callButtons.splice(0, 1, {dataset:{call:'7'}});
  run('renderUsage()');
  await callButtons[0].onclick();
  assert.equal(run('state.events.expanded.size'), 0);
  run("fetchJSON = async () => { throw new Error('offline'); };");
  await run("toggleCall('7')");
  await run("loadRequestEvents('req-test', true)").catch(() => {});
  assert.match(element('#usage-body').innerHTML, /data-retry-request="req-test"/);
  run("state.events.rows[0].request_id = ''; renderUsage();");
  assert.match(element('#usage-body').innerHTML, /没有请求 ID/);
  run(`state.events.rows[0].request_id = 'req-test';
    fetchJSON = async () => [{kind:'provider',provider:'arxiv',success:false,request_id:'req-test'}];`);
  await run('exportCSV()');
  const csv = await exportedBlob.text();
  assert.match(csv, /请求 ID/);
  assert.match(csv, /学术检索/);
  assert.match(csv, /arXiv/);
  assert.match(csv, /req-test/);
  assert.match(csv, /应用/);
  assert.match(csv, /项目/);
  assert.match(csv, /WorkBuddy/);
  assert.match(csv, /Paper <demo>/);
  exportedBlob = undefined;
  run("fetchJSON = async () => { throw new Error('offline'); };");
  await run('exportCSV()');
  assert.equal(exportedBlob, undefined, 'failed detail fetch must never export a partial chain');
  let resolveOld;
  context.oldPromise = new Promise(resolve => { resolveOld = resolve; });
  run("fetchJSON = url => url.includes('provider=arxiv') ? oldPromise : Promise.resolve([{id:8,kind:'tool',tool:'file_search',success:true}]);");
  const pending = run("state.filters.provider='arxiv'; loadEvents()");
  await run("state.filters.provider=''; loadEvents()");
  resolveOld([]);
  await pending;
  assert.equal(run('state.events.rows[0].id'), 8, 'stale filter response cannot overwrite current list');
  run("state.events.rows = []; renderUsage();");
  assert.match(element('#usage-body').innerHTML, /colspan="8"/);
  assert.equal(element('#export-csv').disabled, true);
  console.log('Unified calls: fixed 100, combined filters, detail expansion/retry/escaping, legacy IDs, CSV completeness and stale-response checks passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
