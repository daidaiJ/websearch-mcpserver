'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const elements = new Map();
const element = selector => {
 if (!elements.has(selector)) elements.set(selector, { textContent: '', innerHTML: '', classList: {add(){},remove(){}}, addEventListener(){} });
 return elements.get(selector);
};
const context = vm.createContext({
 document: {querySelector: element, querySelectorAll: () => []},
 window: {__I18N__: JSON.parse(fs.readFileSync(path.join(__dirname,'..','i18n','locales','web.zh.json'),'utf8'))},
 console, setTimeout,
});
const source = fs.readFileSync(path.join(__dirname,'web/app.js'),'utf8');
vm.runInContext(source.slice(0,source.search(/\/\/ Remove credentials retained|\nwire\(\);/)),context);
const run = code => vm.runInContext(code,context);
run(`state.clients.rows = [
 {client:'codex',application:'codex',project:'PZXM',tool:'smartsearch',requests:2,successes:1,failures:1,duration_ms:21},
 {client:'codex-mcp-client',application:'codex',project:'',tool:'smartsearch',requests:1,successes:1,failures:0,duration_ms:13},
 {client:'workbuddy',application:'workbuddy',project:'FP',tool:'cleanfetch',requests:1,successes:1,failures:0,duration_ms:10},
 {client:'pzxm-check',application:'unknown',project:'PZXM',tool:'smartsearch',requests:7,successes:7,failures:0,duration_ms:77},
 ]; renderClientsPanel();`);
assert.equal(run("clientUsageGroups(state.clients.rows,'application').find(r=>r.key==='codex').requests"),3);
assert.match(element('#client-stats').innerHTML,/Codex/);
assert.doesNotMatch(element('#client-stats').innerHTML,/pzxm-check/);
assert.match(element('#client-note').textContent,/10 \/ 11/);
assert.equal((element('#client-stats').innerHTML.match(/<article class="kpi">/g) || []).length,3);
assert.match(element('#client-stats').innerHTML,/<b>成功调用<\/b><\/header><strong>10<\/strong>/);
assert.match(element('#client-stats').innerHTML,/<b>失败次数<\/b><\/header><strong>1<\/strong>/);
assert.match(element('#client-stats').innerHTML,/<b>平均响应时间<\/b><\/header><strong>11 ms<\/strong>/);
assert.match(element('#client-stats').innerHTML,/共 11 次 · 最近 7 天/);
assert.match(element('#client-stats').innerHTML,/未识别应用/);
run("state.clients.dimension='project'; renderClientsPanel();");
assert.match(element('#client-stats').innerHTML,/未标注项目/);
assert.match(element('#client-stats').innerHTML,/PZXM/);
run("state.clients.application='codex'; state.clients.tab='PZXM'; renderClientsPanel();");
assert.match(element('#client-stats').innerHTML,/共 2 次 · PZXM · 最近 7 天/);
assert.match(element('#client-stats').innerHTML,/<b>成功调用<\/b><\/header><strong>1<\/strong>/);
assert.match(element('#client-stats').innerHTML,/<strong>11 ms<\/strong>/);
assert.doesNotMatch(element('#client-stats').innerHTML,/>FP</);
run("state.clients.application=''; state.clients.tab=null; renderClientsPanel();");
assert.match(element('#client-stats').innerHTML,/共 11 次 · 最近 7 天/);
run("state.clients.rows=[]; renderClientsPanel();");
assert.match(element('#client-stats').innerHTML,/暂无调用/);
run("state.clients.error='offline'; renderClientsPanel();");
assert.match(element('#client-note').textContent,/读取失败：offline/);
assert.equal(element('#client-stats').innerHTML,'');
(async () => {
 const pending = [];
 context.fetchJSON = () => new Promise(resolve => pending.push(resolve));
 run("state.clients.days=1; loadClients(); state.clients.days=30; loadClients();");
 pending[1]({window_days:30,usage:[{application:'codex',project:'',tool:'probe',requests:4,successes:4,failures:0,duration_ms:4}]});
 await Promise.resolve(); await Promise.resolve();
 pending[0]({window_days:1,usage:[]});
 await Promise.resolve(); await Promise.resolve();
 assert.equal(run('state.clients.windowDays'),30);
 assert.equal(run('state.clients.rows[0].requests'),4,'stale window response must not overwrite the latest');
 console.log('Client usage grouping, scope, empty/error and async race checks passed');
})().catch(error=>{console.error(error);process.exitCode=1;});
