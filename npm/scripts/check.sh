#!/usr/bin/env bash
# Offline structural checks for the npm / dsh plugin package templates.
# No network, no npm registry, no release assets required.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "  ok  $*"; }

echo "== shell / JS syntax =="
bash -n npm/scripts/publish.sh
bash -n npm/scripts/check-credentials.sh
node --check npm/shared/bin/websearch-mcp-cli.cjs
node --check npm/plugin/index.js
ok "publish.sh + credential guard + shim + ESM root marker"

echo "== package manifests =="
node - <<'NODE'
const fs = require('node:fs');
const path = require('node:path');

const read = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const fail = (m) => { console.error('FAIL: ' + m); process.exit(1); };

const platforms = {
  'linux-amd64': ['linux', 'x64'],
  'windows-amd64': ['win32', 'x64'],
  'darwin-amd64': ['darwin', 'x64'],
  'darwin-arm64': ['darwin', 'arm64'],
};
const platformDeps = Object.keys(platforms).map((p) => 'websearch-mcp-cli-' + p);
const manifests = [
  'npm/plugin/package.json',
  'npm/cli/package.json',
  ...Object.keys(platforms).map((p) => `npm/platform/${p}/package.json`),
];

for (const file of manifests) {
  const pkg = read(file);
  if (pkg.version !== '0.0.0') fail(`${file}: version must stay the 0.0.0 placeholder (publish.sh rewrites it)`);
  if (pkg.name !== pkg.name.toLowerCase()) fail(`${file}: npm names must be lowercase`);
  if (pkg.license !== 'MIT') fail(`${file}: license`);
  if (pkg.publishConfig?.access !== 'public') fail(`${file}: publishConfig.access`);
  const scripts = pkg.scripts || {};
  for (const hook of ['preinstall', 'install', 'postinstall', 'prepare', 'prepublish', 'prepack']) {
    if (scripts[hook]) fail(`${file}: lifecycle script "${hook}" breaks pnpm allowBuilds gating`);
  }
  if (!(pkg.files || []).includes('bin/') && !file.startsWith('npm/platform/')) {
    fail(`${file}: files must include bin/`);
  }
  if (pkg.repository?.url !== 'git+https://github.com/daidaiJ/websearch-mcpserver.git') {
    fail(`${file}: repository.url (needed for npm provenance)`);
  }
}

const plugin = read('npm/plugin/package.json');
if (plugin.dsh?.bundle?.patch !== './cordis.patch.yml') fail('plugin: dsh.bundle.patch');
if (!(plugin.files || []).includes('cordis.patch.yml')) fail('plugin: files must include cordis.patch.yml (missing = installs but never mounts)');
if (!(plugin.files || []).includes('index.js')) fail('plugin: files must include the side-effect-free ESM root marker');
if (plugin.type !== 'module' || plugin.main !== './index.js' || plugin.exports?.['.'] !== './index.js') {
  fail('plugin: package-root ESM entry must be resolvable for marketplace validation');
}
if (plugin.name !== 'dsh-websearch-mcpserver') fail('plugin: name');
if (JSON.stringify(Object.keys(plugin.optionalDependencies || {}).sort()) !== JSON.stringify([...platformDeps].sort())) {
  fail('plugin: optionalDependencies must list exactly the four platform packages');
}

const cli = read('npm/cli/package.json');
if (cli.bin?.['websearch-mcp-cli'] !== 'bin/websearch-mcp-cli.cjs') fail('cli: bin mapping');
if (JSON.stringify(Object.keys(cli.optionalDependencies || {}).sort()) !== JSON.stringify([...platformDeps].sort())) {
  fail('cli: optionalDependencies must list exactly the four platform packages');
}

for (const [p, [os, cpu]] of Object.entries(platforms)) {
  const pkg = read(`npm/platform/${p}/package.json`);
  if (pkg.name !== `websearch-mcp-cli-${p}`) fail(`${p}: name`);
  if (pkg.os?.length !== 1 || pkg.os[0] !== os) fail(`${p}: os must be [${os}]`);
  if (pkg.cpu?.length !== 1 || pkg.cpu[0] !== cpu) fail(`${p}: cpu must be [${cpu}]`);
  if (!(pkg.files || []).includes('bin/')) fail(`${p}: files must include bin/`);
}
console.log('  ok  6 manifests (naming, os/cpu, files, dsh.bundle, no lifecycle scripts)');
NODE

echo "== cordis.patch.yml =="
grep -q "id: mcp-websearch" npm/plugin/cordis.patch.yml || fail "patch: missing row id"
grep -q "name: '@deepseek-ai/dsh-mcp-client'" npm/plugin/cordis.patch.yml || fail "patch: must reuse the built-in MCP client"
grep -q "serverName: websearch" npm/plugin/cordis.patch.yml || fail "patch: serverName"
grep -q "transport: stdio" npm/plugin/cordis.patch.yml || fail "patch: stdio transport"
grep -q "command: !!js process.execPath" npm/plugin/cordis.patch.yml || fail "patch: must use process.execPath, not a PATH-dependent bin name"
grep -q "dsh-websearch-mcpserver/bin/websearch-mcp-cli.cjs" npm/plugin/cordis.patch.yml || fail "patch: shim path"
ok "patch contract"

echo "== source-of-truth shim =="
[ -f npm/shared/bin/websearch-mcp-cli.cjs ] || fail "shared shim missing"
grep -q "optionalDependencies\|websearch-mcp-cli-\${os}-\${arch}" npm/shared/bin/websearch-mcp-cli.cjs \
  || fail "shim must resolve a platform package at runtime"
ok "single shim reused by cli + plugin"

echo "== OIDC release contract =="
node - <<'NODE'
const fs = require('node:fs');
const fail = (m) => { console.error('FAIL: ' + m); process.exit(1); };
const npm = fs.readFileSync('.github/workflows/npm-plugin.yml', 'utf8');
const release = fs.readFileSync('.github/workflows/release.yml', 'utf8');

// npm Trusted Publishing 校验的是「调用方」工作流文件名。一旦把发布 job
// 改回 release.yml 的 workflow_call，npm 侧绑定的 npm-plugin.yml 就再也匹配
// 不上，发布会在 OIDC 交换凭据时失败——所以这里把契约钉成 PR 级护栏。
for (const [label, text] of [['npm-plugin.yml', npm], ['release.yml', release]]) {
  if (/^\s*workflow_call\s*:/m.test(text)) {
    fail(`${label}: workflow_call breaks Trusted Publishing identity (npm validates the CALLER workflow filename)`);
  }
  if (/NODE_AUTH_TOKEN:\s*\$\{\{|secrets:\s*inherit/.test(text)) {
    fail(`${label}: injecting a long-lived npm token is not allowed anymore`);
  }
}
if (!/tags:\s*\n\s*-\s*'v\*-npm'/.test(npm)) fail('npm-plugin.yml: must be triggered directly by v*-npm tags');
if (!/id-token:\s*write/.test(npm)) fail('npm-plugin.yml: id-token: write is required for OIDC');
if (!/environment:\s*PANDA/.test(npm)) fail('npm-plugin.yml: environment must match the npm Trusted Publisher setting');
if (!/!v\*-npm/.test(release)) fail('release.yml: must exclude v*-npm tags');
if (/^\s{2}publish-npm:/m.test(release)) fail('release.yml: publish-npm job must stay removed');
console.log('  ok  OIDC release contract (self-triggered, no token, -npm excluded from release)');
NODE

echo "== credential guard =="
bash npm/scripts/check-credentials.sh

echo "ALL CHECKS PASSED"
