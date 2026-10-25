'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'web', 'app.js'), 'utf8');
new vm.Script(source);
// 前端文案词典由服务端内联注入；测试按 zh 目录注入，与真实渲染同口径。
const webZh = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'i18n', 'locales', 'web.zh.json'), 'utf8'));
const prefix = source.slice(0, source.indexOf('const SYSTEM'));
const context = vm.createContext({ window: { __I18N__: webZh } });
vm.runInContext(prefix, context);
assert.equal(vm.runInContext('ERROR_KINDS.captcha', context), '\u4eba\u673a\u9a8c\u8bc1');
assert.equal(vm.runInContext('ERROR_KINDS.no_result', context), '\u65e0\u7ed3\u679c');
console.log('retrieval error labels passed');
