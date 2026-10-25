'use strict';
// 前端 i18n 运行时用例：词典查表、缺失回退、{{.Name}} 占位符、CLDR 复数形态、
// applyI18n 的文本节点替换。覆盖面审计（key 双向一致、占位符一致）在 Go 侧
// pkg/i18n/catalog_test.go，此处不重复。

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
const prefix = source.slice(0, source.indexOf('const REFRESH_MS'));
new vm.Script(source);

// load 在隔离上下文里跑 app.js 的前缀，返回 (code) => 求值结果。
function load(lang, dict) {
  const context = vm.createContext({
    document: { documentElement: { lang }, querySelectorAll: () => [] },
    window: { __I18N__: dict },
    Intl,
  });
  vm.runInContext(prefix, context);
  return { run: code => vm.runInContext(code, context), context };
}

const dict = {
  'web.sample.plain': 'Plain',
  'web.sample.zh_only': '只有中文',
  'web.sample.named': '{{.Count}} result(s) for {{.Query}}',
  'web.sample.spaced': '{{ .Name }} here',
  'web.sample.count': { one: '{{.Count}} result', other: '{{.Count}} results' },
  'web.sample.object_other_only': { other: '{{.Count}} items' },
};

const en = load('en', dict).run;
assert.equal(en("t('web.sample.plain')"), 'Plain', 'en 查表');
assert.equal(en("t('web.sample.zh_only')"), '只有中文', 'en 缺条目回退 zh（词典里就是 zh 文案）');
assert.equal(en("t('web.sample.missing')"), 'web.sample.missing', '两种语言都缺才返回 key');
assert.equal(en("t('web.sample.named', { Count: 3, Query: 'go' })"), '3 result(s) for go', '命名占位符替换');
assert.equal(en("t('web.sample.named', { Query: 'go' })"), ' result(s) for go', '缺失的占位符渲染为空串');
assert.equal(en("t('web.sample.spaced', { Name: 'x' })"), 'x here', '容忍 {{ .Name }} 的空格写法');
assert.equal(en("t('web.sample.count', { Count: 1 })"), '1 result', '复数形态 one');
assert.equal(en("t('web.sample.count', { Count: 5 })"), '5 results', '复数形态 other');
assert.equal(en("t('web.sample.object_other_only', { Count: 5 })"), '5 items', '复数对象缺形态时回落 other');
assert.equal(en('I18N_LANG'), 'en', 'en 模式识别 <html lang>');

const zh = load('zh-CN', { 'web.sample.plain': '普通' }).run;
assert.equal(zh('I18N_LANG'), 'zh', 'zh 模式识别 <html lang>');
assert.equal(zh("t('web.sample.plain')"), '普通', 'zh 模式同样查词典（词典即中文）');
assert.equal(zh("t('web.sample.absent')"), 'web.sample.absent', 'zh 模式缺条目同样返回 key');

// applyI18n：只替换元素的首个非空文本节点，保留兄弟节点
function element(key, attr, children) {
  return {
    getAttribute: name => (name === 'data-i18n' ? key : name === 'data-i18n-title' ? attr : null),
    setAttribute(name, value) { this[name] = value; },
    childNodes: children,
  };
}
const blankThenText = [{ nodeType: 3, nodeValue: ' ' }, { nodeType: 3, nodeValue: '普通' }];
const plainText = [{ nodeType: 3, nodeValue: '普通' }];
const nodes = [
  element('web.sample.plain', null, plainText),
  element('web.sample.plain', null, blankThenText),
  element(null, 'web.sample.plain', plainText),
];
const root = {
  querySelectorAll: sel => (sel === '[data-i18n]' ? nodes.slice(0, 2) : sel === '[data-i18n-title]' ? nodes.slice(2) : []),
};
const withRoot = load('en', dict);
withRoot.context.__root = root;
withRoot.run('applyI18n(__root)');
assert.equal(plainText[0].nodeValue, 'Plain', 'data-i18n 文本节点被替换');
assert.equal(blankThenText[1].nodeValue, 'Plain', '跳过空白文本节点，替换其后首个非空节点');
assert.equal(nodes[2].title, 'Plain', 'data-i18n-title 属性被替换');

// zh 模式词典就是中文，applyI18n 结果不变
const zhRoot = load('zh-CN', { 'web.sample.plain': '普通' });
const zhText = [{ nodeType: 3, nodeValue: '普通' }];
zhRoot.context.__root = { querySelectorAll: sel => (sel === '[data-i18n]' ? [element('web.sample.plain', null, zhText)] : []) };
zhRoot.run('applyI18n(__root)');
assert.equal(zhText[0].nodeValue, '普通', 'zh 模式渲染中文原文');

console.log('frontend i18n runtime checks passed');
