'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const elements = new Map();
const fields = [{ dataset: { key: 'bing.enabled' }, type: 'checkbox', checked: false, reportValidity: () => true }];
function element(selector) {
  const classes = new Set();
  if (!elements.has(selector)) elements.set(selector, {
    textContent: '', innerHTML: '', value: '', disabled: false, style: {},
    classList: {
      toggle(name, force) { if (force === undefined ? !classes.has(name) : force) classes.add(name); else classes.delete(name); },
      add(name) { classes.add(name); }, remove(name) { classes.delete(name); }, contains(name) { return classes.has(name); },
    },
    reportValidity: () => true,
    setAttribute(name, value) { this[name] = value; },
  });
  return elements.get(selector);
}
let now = 0;
const context = vm.createContext({
  console, AbortSignal,
  // 前端文案词典由服务端内联注入；测试按 zh 目录注入，与真实渲染同口径。
  window: {__I18N__: JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'i18n', 'locales', 'web.zh.json'), 'utf8'))},
  document: {
    querySelector: element,
    querySelectorAll: selector => selector === '[data-key]' ? fields : selector === 'input[name="mode"]' ? fields.filter(field => field.name === 'mode') : [],
  },
  setTimeout: callback => { now += 1000; callback(); },
});
const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
const modeEscapeListener = source.match(/document\.addEventListener\('keydown', e => \{\s*if \(e\.key === 'Escape'\) \$\$\('\.mode-info'\)\.forEach\(button => setModeHelp\(button, false, true\)\);\s*\}\);/);
assert.ok(modeEscapeListener, 'document Escape must dismiss pure hover tooltips without button focus');
assert.doesNotMatch(source, /button\.addEventListener\('keydown'/, 'mode dismissal must not depend on button focus');
assert.doesNotMatch(modeEscapeListener[0], /preventDefault|stopPropagation/, 'tooltip dismissal must not intercept modal key handling');
const html = fs.readFileSync(path.join(__dirname, 'web', 'index.html'), 'utf8');
const settingsPage = html.match(/<section id="page-settings"[\s\S]*?<\/section>/)[0];
for (const id of ['apply-note', 'apply-recovery', 'retry-apply', 'backup-select', 'restore-backup']) {
  assert.equal((html.match(new RegExp('id="' + id + '"', 'g')) || []).length, 1, id + ' must have one entry');
  assert.match(settingsPage, new RegExp('id="' + id + '"'), id + ' belongs only to Settings');
}
assert.match(settingsPage, /外观与维护[\s\S]*配置恢复[\s\S]*id="restore-backup"/);
const sourcesPage = html.match(/<section id="page-sources"[\s\S]*?<\/section>/)[0];
assert.doesNotMatch(sourcesPage, /运行 \/ 条件|参与运行|未参与/, 'source controls must not duplicate runtime participation');
assert.equal((sourcesPage.match(/<th data-i18n="web.label.enabled">启用<\/th>/g) || []).length, 3);
assert.equal((sourcesPage.match(/<th data-i18n="web.label.last_20">最近 20 次<\/th>/g) || []).length, 3);
assert.equal((sourcesPage.match(/<th(?:\s|>)/g) || []).length, 24);
assert.doesNotMatch(sourcesPage, /实际冷却|失败构成|成功 \/ 失败|P95|最近观测|<th>额度/);
assert.doesNotMatch(html, /bootstrap-switch|cdn\.|unpkg|jsdelivr/i);
const modeInputs = Array.from(html.matchAll(/<input type="radio" name="mode" value="([^"]+)" data-key="mode">/g), match => match[1]);
assert.deepEqual(modeInputs,
  ['hybrid', 'engine', 'apipool']);
for (const mode of modeInputs) {
  assert.match(html, new RegExp('value="' + mode + '" data-key="mode"><span>' + mode + '</span>'));
  assert.match(html, new RegExp('aria-label="' + mode + ' 模式说明" aria-describedby="mode-note-' + mode + '"'));
  assert.match(html, new RegExp('data-i18n-aria-label="web.mode.' + mode + '_mode_help"'));
  assert.match(html, new RegExp('id="mode-note-' + mode + '" role="tooltip" data-i18n="web.mode.[^"]+">[^<]+'));
}
assert.match(html, /round-robin、priority、weighted/);
assert.match(html, /无 Key 时走按出口 IP 限流的匿名档/);
assert.match(html, /已启用且条件满足的 API 与网页引擎混合并发/);
assert.match(html, /已启用的网页引擎并发，无需 Key；代理依赖引擎仅在条件满足时参与/);
assert.match(html, /百度网页启用时兜底/);
assert.match(html, /失败回退已启用的百度网页；无 SK 时使用已启用的百度网页/);
for (const mode of ['tavily', 'exa', 'doubao']) {
  const note = html.match(new RegExp('id="mode-note-' + mode + '" role="tooltip" data-i18n="[^"]+">([^<]+)'))[1];
  assert.match(note, /回退已启用的 Bing/);
}
assert.doesNotMatch(html, /混合检索（API与网页）|网页引擎并行|API调度池（失败换源）/);
assert.match(html, /<details[^>]*>[\s\S]*?学术搜索网络环境<select name="network"/);
assert.doesNotMatch(html, /熔断首次暂停|熔断暂停上限|网络区域/);
assert.match(html, /value="provider" id="mode-single"><span data-i18n="web.status.single_provider">单供应商/);
const providerOptions = Array.from(html.match(/id="mode-provider">([\s\S]*?)<\/select>/)[1].matchAll(/value="([^"]+)"/g), match => match[1]);
assert.deepEqual(providerOptions, ['baidu', 'tavily', 'exa', 'anysearch', 'doubao', 'yuanbao']);
assert.doesNotMatch(html, /data-key="doubao\.(version|icp_host_only)"/, 'Doubao parameters must not remain on Settings');
assert.match(source, /data-source-setting="\$\{key\}" aria-label="\$\{t\('web.label.doubao_api_version'\)\}"/);
assert.match(source, /detailed && id === 'doubao' \? doubaoVersionControl\(\)/, 'version control belongs to the Doubao source row only');
const settingsColumns = html.slice(html.indexOf('<section id="page-settings"')).split('<div class="settings-column">').slice(1);
assert.equal(settingsColumns.length, 2);
assert.match(settingsColumns[0], /常用设置[\s\S]*外观与维护/);
assert.doesNotMatch(settingsColumns[0], /CREDENTIALS|QUOTAS/);
assert.match(settingsColumns[1], /API Key 状态[\s\S]*额度管理/);
vm.runInContext(source.slice(0, source.indexOf('// Remove credentials retained')), context);
vm.runInContext('Date.now = () => testNow();', Object.assign(context, { testNow: () => now }));
function run(code) { return vm.runInContext(code, context); }
function settings() {
  run(`state.settings = {
    values: { 'bing.enabled': false, 'academic.disable_arxiv': false },
    saved_values: { 'bing.enabled': false, 'academic.disable_arxiv': false },
    revision: 'r1', write_access: { allowed: true }, admin_password_configured: true,
    secrets: {}, pending_changes: {}, locked_fields: {}, backups: []
  }; state.settingsError = null; state.draft = {}; state.draftBefore = {}; state.settingsDirty = false; state.saving = false; state.applying = false;`);
}

(async () => {
  settings();
  const standardSwitch = run(`sourceControl({id:'bing', name:'Bing', enabled:false, control_path:'bing.enabled'})`);
  assert.match(standardSwitch, /class="form-check form-switch source-toggle"/);
  assert.match(standardSwitch, /class="form-check-input" type="checkbox" role="switch"/);
  assert.match(standardSwitch, /id="source-switch-bing"/);
  assert.match(standardSwitch, /aria-label="Bing 配置启用"/);
  assert.doesNotMatch(standardSwitch, /<label|draft-mark/, 'switch has no redundant visible label');
  assert.doesNotMatch(standardSwitch, /class="toggle/);
  const sourceRow = run(`sourceRowHtml({id:'bing',enabled:false,control_path:'bing.enabled',condition:'private runtime condition'},true)`);
  assert.equal((sourceRow.match(/<td(?:\s|>)/g) || []).length, 8, 'cells must match the compact header');
  assert.doesNotMatch(sourceRow, /source-condition|private runtime condition/);
  assert.match(sourceRow, /health-strip/);
  assert.equal((run(`healthStrip({recent_outcomes:[true,false]})`).match(/<i /g) || []).length, 20);
  assert.doesNotMatch(sourceRow, /<small class="dim">bing<\/small>/, 'source names must omit ID remarks');
  run(`setDraft('bing.enabled',true)`);
  assert.doesNotMatch(run(`sourceControl({id:'bing',control_path:'bing.enabled'})`), /<label|draft-mark/);
  run('discardDraft()');
  assert.equal(run(`countsLabel([{enabled:true,active:false},{enabled:false,active:true}])`), '已启用 1 / 2');
  run(`globalThis.loadSettingsOriginal = loadSettings; state.applyResult = null;
    state.settings.pending_changes = {'bing.enabled':true}; state.settings.saved_values['bing.enabled'] = true;
    state.settings.backups = [{name:'pending.yaml',created_at:'2026-10-11'}]; fetchJSON = async () => state.settings;`);
  await run('loadSettingsOriginal()');
  assert.equal(run('state.applyResult.failed'), true, 'opening a page must surface saved pending changes');
  assert.equal(run('state.applyResult.status'), '配置已保存，尚未确认生效');
  assert.equal(run('state.applyResult.backup'), 'pending.yaml');
  assert.equal(run(`state.applyResult.targets['bing.enabled']`), true);
  assert.equal(element('#retry-apply').classList.contains('hidden'), false);
  run(`state.applyResult = {failed:false,status:'运行配置已确认生效'}; state.settings.pending_changes = {}; renderApplyResult();`);
  assert.equal(element('#apply-recovery').classList.contains('hidden'), false, 'backups remain accessible after success');
  assert.equal(element('#retry-apply').classList.contains('hidden'), true, 'success must not offer an unnecessary restart retry');
  settings();
  run('state.applyResult = null;');
  run(`setDraft('bing.enabled', true); setDraft('academic.disable_arxiv', true);`);
  assert.equal(run('Object.keys(changes()).length'), 2);
  assert.equal(run('state.draftRevision'), 'r1');
  run(`state.settings.revision = 'r2'; renderSettingsForm(); renderSourcePages();`);
  assert.equal(fields[0].checked, true, 'refresh must preserve the draft');
  assert.equal(run('state.draftRevision'), 'r1', 'refresh must not replace the optimistic revision');
  assert.match(run(`sourceControl({id:'arxiv', enabled:true, control_path:'academic.disable_arxiv', inverted:true})`), /role="switch"/);
  assert.doesNotMatch(run(`sourceControl({id:'arxiv', enabled:true, control_path:'academic.disable_arxiv', inverted:true})`), / checked/);
  run(`setDraft('bing.enabled', false);`);
  assert.equal(run('Object.keys(changes()).length'), 1, 'switching back removes only that draft');
  run(`discardDraft();`);
  assert.equal(run('Object.keys(changes()).length'), 0);
  run(`state.settings.locked_fields['bing.enabled'] = '环境变量覆盖';`);
  assert.match(run(`sourceControl({id:'bing', enabled:false, control_path:'bing.enabled'})`), /disabled/);
  run(`state.settings.write_access.allowed = false; renderSettingsForm();`);
  assert.equal(fields[0].disabled, true);
  assert.match(run(`sourceControl({id:'bing', enabled:false, control_path:'bing.enabled'})`), /disabled/);
  assert.equal(run(`healthOf({active:false,last_seen_at:'2026-10-11',status:'down'}).label`), '异常', 'historical health survives disabling');
  assert.equal(run(`healthOf({active:false,last_seen_at:'2026-10-11',status:'inactive',observation_status:'down'}).label`), '异常');
  assert.match(run(`suspendBadge({suspended:true})`), /观测告警/);
  assert.equal(run(`runtimeMatches({values:{x:true},saved_values:{x:false}}, {x:true})`), false);
  assert.equal(run(`runtimeMatches({values:{x:true},saved_values:{x:true}}, {x:true})`), true);

  settings();
  run(`Object.assign(state.settings.values, {'doubao.version':'global','doubao.icp_host_only':true});
    Object.assign(state.settings.saved_values, {'doubao.version':'global','doubao.icp_host_only':true});`);
  assert.match(run('doubaoVersionControl()'), /value="global" selected/);
  run(`setDraft('doubao.version','custom'); renderSettingsForm(); renderSourcePages();`);
  assert.equal(run(`changes()['doubao.version']`), 'custom');
  assert.match(run('doubaoVersionControl()'), /value="custom" selected/);
  assert.doesNotMatch(run('doubaoVersionControl()'), /草稿/);
  assert.match(element('#source-draft-count').textContent, /1 项草稿/, 'version edits use the shared draft feedback');
  assert.equal(run(`Object.hasOwn(changes(), 'doubao.icp_host_only')`), false);
  assert.equal(run(`savedValues()['doubao.icp_host_only']`), true, 'hidden ICP configuration must not be reset');
  run('discardDraft()');
  assert.match(run('doubaoVersionControl()'), /value="global" selected/);
  run(`discardDraft(); state.settings.locked_fields['doubao.version']='环境变量覆盖';
    state.settings.values['doubao.version']='custom'; renderSettingsForm();`);
  assert.match(run('doubaoVersionControl()'), /disabled/);
  assert.match(run('doubaoVersionControl()'), /value="custom" selected/, 'locked version displays effective value');
  run(`setDraft('doubao.version','global');`);
  assert.equal(run('Object.keys(changes()).length'), 0, 'locked values cannot create drafts');
  run(`state.settings.write_access.allowed=false; state.settings.locked_fields={}; renderWriteAccess();`);
  assert.match(run('doubaoVersionControl()'), /disabled/);

  settings();
  const modeFields = modeInputs.map(value => ({dataset:{key:'mode'},type:'radio',name:'mode',value,checked:false,reportValidity:()=>true}));
  const singleField = {dataset:{},type:'radio',name:'mode',value:'provider',checked:false};
  fields.push(...modeFields);
  fields.push(singleField);
  run(`state.settings.values.mode='hybrid'; state.settings.saved_values.mode='hybrid'; renderSettingsForm();`);
  assert.deepEqual(modeFields.filter(field => field.checked).map(field => field.value), ['hybrid']);
  context.modeField = modeFields[2];
  modeFields[2].checked = true;
  run(`markDirty({target:modeField}); renderSettingsForm();`);
  assert.equal(run('changes().mode'), 'apipool');
  assert.deepEqual(modeFields.filter(field => field.checked).map(field => field.value), ['apipool']);
  assert.deepEqual(modeFields.map(field => field.value), modeInputs, 'rendering must never overwrite radio values');
  modeFields[2].checked = false;
  run(`markDirty({target:modeField});`);
  assert.equal(run('changes().mode'), 'apipool', 'unchecked radio events must not overwrite the draft');
  for (const provider of providerOptions) {
    run(`discardDraft(); state.settings.values.mode='${provider}'; state.settings.saved_values.mode='${provider}'; renderSettingsForm();`);
    assert.equal(singleField.checked, true, 'legacy provider mode maps to the single-provider radio');
    assert.equal(element('#mode-provider').value, provider);
    assert.equal(element('#mode-provider-options').classList.contains('hidden'), false);
    assert.equal(run('Object.keys(changes()).length'), 0, 'loading a legacy mode creates no draft');
    assert.equal(element('#mode-provider-info')['aria-describedby'], 'mode-note-' + provider);
  }
  run(`state.settings.values.mode='hybrid'; state.settings.saved_values.mode='hybrid'; renderSettingsForm();`);
  singleField.checked = true;
  context.singleField = singleField;
  element('#mode-provider').value = 'anysearch';
  run('changeSingleProvider({target:singleField})');
  assert.equal(run('changes().mode'), 'anysearch');
  run(`state.settings.revision='r2'; renderSettingsForm();`);
  assert.equal(run('state.draftRevision'), 'r1');
  assert.equal(element('#mode-provider').value, 'anysearch', 'polling preserves provider drafts');
  element('#mode-provider').value = 'doubao';
  run(`changeSingleProvider({target:{type:'select-one'}});`);
  assert.equal(run('changes().mode'), 'doubao');
  run('discardDraft()');
  assert.equal(element('#mode-provider-options').classList.contains('hidden'), true);
  assert.equal(element('#mode-provider').disabled, true);
  run(`discardDraft(); state.settings.locked_fields.mode='环境变量覆盖'; state.settings.values.mode='engine'; renderSettingsForm();`);
  assert.deepEqual(modeFields.filter(field => field.checked).map(field => field.value), ['engine']);
  assert.equal(modeFields.every(field => field.disabled), true);
  assert.equal(singleField.disabled, true);
  assert.equal(element('#mode-provider').disabled, true);
  run(`setDraft('mode','doubao');`);
  assert.equal(run('Object.keys(changes()).length'), 0);
  run(`state.settings.values.mode='exa'; renderSettingsForm();`);
  assert.equal(singleField.checked, true);
  assert.equal(element('#mode-provider').value, 'exa');
  run(`state.settings.locked_fields={}; state.settings.write_access.allowed=false; renderWriteAccess();`);
  assert.equal(singleField.disabled, true);
  assert.equal(element('#mode-provider').disabled, true);
  run(`changeSingleProvider({target:{type:'select-one'}});`);
  assert.equal(run('Object.keys(changes()).length'), 0);
  fields.splice(1);
  const help = element('#mode-help-test');
  const helpButton = {parentElement:help,setAttribute(name,value) { this[name] = value; }};
  context.helpButton = helpButton;
  run('setModeHelp(helpButton,true)');
  assert.equal(helpButton['aria-expanded'], 'true');
  assert.equal(help.classList.contains('open'), true);
  run('setModeHelp(helpButton,false,true)');
  assert.equal(helpButton['aria-expanded'], 'false');
  assert.equal(help.classList.contains('dismissed'), true, 'Escape and repeat tap can dismiss focused tooltips');

  settings();
  run(`state.overview = {configured_tools:[{name:'file_search',enabled:true,state:'unprobed'},{name:'pdf_parser',enabled:true,pdf:{max_pages:4,mineru_page_limit:600,mineru_page_batch_size:2,mineru_page_budget:8,ocr_enabled:true}}],tools:[]}; renderMCPTools();`);
  assert.match(element('#mcp-tools-list').innerHTML, /尚未探测/);
  run(`state.overview.configured_tools[0].enabled = false; renderMCPTools();`);
  assert.match(element('#mcp-tools-list').innerHTML, /配置未启用 · 尚未探测/, 'disabled file search must retain its independent probe state');
  assert.match(element('#mcp-tools-list').innerHTML, /默认页数 4 · 单任务页限 600 · 批大小 2 · 调用预算 8 · OCR 开/);
  run(`state.overview.configured_tools[0].file = {allowed_root_count:2,whitelist_enforced:true,path_metadata_only:true}; renderMCPTools();`);
  assert.match(element('#mcp-tools-list').innerHTML, /白名单根目录 2 · 白名单限制 · 仅路径\/元数据/);
  assert.match(element('#mcp-tools-list').innerHTML, /尚未探测/, 'file summary must preserve probe state');
  run(`state.overview.configured_tools[0].file = {allowed_root_count:0,whitelist_enforced:false,path_metadata_only:true}; renderMCPTools();`);
  assert.match(element('#mcp-tools-list').innerHTML, /白名单根目录 0 · 目录未限制/);
  assert.doesNotMatch(element('#mcp-tools-list').innerHTML, /data-key="everything\./, 'root scope must remain read-only');
  if (html.includes('id="filter-kind"')) {
    assert.equal(run(`knownFilterEntries().tools.some(t => t[0] === 'file_search')`), true);
  }

  run(`state.overview = null; setDraft('bing.enabled', true); state.settings.revision = 'r2';
    globalThis.posts = []; securePost = async (url,payload) => { posts.push({url,payload}); return {backup:'backup.yaml'}; };
    confirmDialog = async () => true; loadSettings = async () => {}; restartAndVerifyOriginal = restartAndVerify;
    restartAndVerify = async () => { globalThis.restarted = true; };`);
  await run('saveSettings()');
  assert.equal(run('posts[0].payload.revision'), 'r1');
  assert.equal(run('posts[1].payload.revision'), 'r1');
  assert.equal(run('restarted'), true);
  assert.equal(run('Object.keys(state.draft).length'), 0);
  assert.equal(run('state.applyResult.backup'), 'backup.yaml');
  assert.equal(run('state.saving'), false);

  settings();
  fields.push({dataset:{key:'smartsearch.max_size'},type:'number',value:'0',reportValidity:()=>false});
  fields.push({dataset:{key:'dashboard.suspension.ban_time_on_fail'},type:'text',value:'',reportValidity:()=>false});
  run(`globalThis.posts=[]; securePost = async (url,payload) => { posts.push({url,payload}); return {backup:'validation.yaml'}; }; setDraft('bing.enabled',true);`);
  await run('saveSettings()');
  assert.equal(run('posts.length'), 2, 'unmodified zero/default settings must not block source-only saves');
  settings();
  run(`globalThis.posts=[]; setDraft('smartsearch.max_size',0);`);
  await run('saveSettings()');
  assert.equal(run('posts.length'), 0, 'a changed invalid setting must still prevent saving');
  fields.splice(1);

  settings();
  run(`setDraft('bing.enabled', true); securePost = async () => { throw new Error('配置版本冲突'); };`);
  await run('saveSettings()');
  assert.equal(run(`state.draft['bing.enabled']`), true, 'a conflict must preserve the draft');
  assert.equal(run('state.saving'), false);
  run(`state.settings.config_error = '配置不可读'; renderWriteAccess();`);
  assert.equal(fields[0].disabled, true, 'config read failure must fail closed');

  settings();
  run(`state.settings.backups = [{name:'restore.yaml',created_at:'2026-10-11'}];
    state.settings.pending_changes = {'bing.enabled':true};
    globalThis.posts = []; securePost = async (url,payload) => { posts.push({url,payload}); return {backup:'undo.yaml',changes:{'bing.enabled':true}}; };
    globalThis.restoreDiff = ''; confirmDialog = async (title,copy,diff) => { restoreDiff = diff; return true; };
    restartAndVerify = async () => { globalThis.restarted = true; };`);
  element('#backup-select').value = 'restore.yaml';
  await run('restoreBackup()');
  assert.equal(run('posts[0].payload.restore_backup'), 'restore.yaml');
  assert.equal(run('posts[1].payload.confirm'), true);
  assert.match(run('restoreDiff'), /bing.enabled: false → true/);
  assert.equal(run(`state.applyResult.targets['bing.enabled']`), true);
  assert.equal(run('state.applyResult.backup'), 'undo.yaml');

  settings();
  run(`restartAndVerify = restartAndVerifyOriginal; loadAll = async () => {};
    state.applyResult = {targets:{'bing.enabled':true},backup:'backup.yaml'};
    fetchJSON = async url => url.endsWith('settings') ? {values:{'bing.enabled':true},saved_values:{'bing.enabled':true}} : {system:{status:'waiting'}};`);
  await run('restartAndVerify()');
  assert.equal(run('state.applyResult.failed'), false);
  assert.equal(run('state.applyResult.status'), '运行配置已确认生效');
  settings();
  now = 0;
  run(`state.applyResult = {targets:{'bing.enabled':true},backup:'backup.yaml'};
    fetchJSON = async () => { throw new Error('离线'); };`);
  await run('restartAndVerify()');
  assert.equal(now, 60000);
  assert.equal(run('state.applyResult.failed'), true);
  assert.match(run('state.applyResult.status'), /60 秒内未确认生效/);
  assert.equal(run('state.applyResult.backup'), 'backup.yaml');
  assert.doesNotMatch(source, /(?:sessionStorage|localStorage)\.setItem\(['"]admin/);
  assert.doesNotMatch(source, /location\.reload\(\)/);
  console.log('Frontend assertions passed: grouped provider modes, Doubao source version and locks, draft, revision, inversion, permissions, tool catalog, PDF, restart verification and timeout.');
})().catch(error => { console.error(error); process.exitCode = 1; });
