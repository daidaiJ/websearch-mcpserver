# npm / dsh 插件分发（维护者文档）

把 Go 单二进制 `websearch-mcp-cli` 分发到 npm，并包装成 DeepSeek Harness（dsh）的 **config-only Cordis bundle**。设计依据与同类调研见仓库本地草稿 `docs/plans/2026-11-07-dsh-ecosystem-review.md`（该目录被 `.gitignore` 忽略，不入库）。

## 包结构（一组 6 个包，版本与 git tag 同源）

| npm 包 | 内容 | 谁装它 |
|---|---|---|
| `dsh-websearch-mcpserver` | dsh 插件壳：`cordis.patch.yml` + 一份启动 shim，config-only bundle，无 `apply(ctx)` | dsh 用户 `dsh plugin --profile web add ...` |
| `websearch-mcp-cli` | 通用 CLI 包装包：`bin` + 同一份 shim，非 dsh 客户端也能用 | `npm i -g websearch-mcp-cli` 或任意 MCP 客户端 |
| `websearch-mcp-cli-linux-amd64` | 该平台预编译 Go 二进制 + 同版本 PDFium 动态库 + 校验和 | 由上面两个包按 `os` / `cpu` 通过 `optionalDependencies` 自动挑选 |
| `websearch-mcp-cli-windows-amd64` | 同上（`.exe`） | 同上 |
| `websearch-mcp-cli-darwin-amd64` | 同上 | 同上 |
| `websearch-mcp-cli-darwin-arm64` | 同上 | 同上 |

平台矩阵**必须与 `.github/workflows/release.yml` 的四平台一致**（linux/amd64、windows/amd64、darwin/amd64、darwin/arm64）。文件头注释明确写了 linux-arm64 / windows-arm64 不要加回本矩阵：linux-arm64 走 GHCR，缺的平台在 README 指向容器或手动下载 Release 二进制。

## 为什么这样打

- **不用 postinstall**：`dsh plugin` 转发 pnpm，pnpm 10+ 的 `allowBuilds` 门禁会拦掉 install / postinstall 下载与 chmod，装完是「装上了但二进制没落地」。esbuild / Biome 式「父包 + `os`/`cpu` 限定的平台子包」是唯一稳的走法。
- **不用 HTTP 路径**：`cmd/cli` 无参数即 stdio，无配置文件走 `mode: engine` 内存默认值，日志写 stderr 不污染 JSON-RPC；插件形态不开端口、不需要 `auth_token`。
- **不用 `command: websearch-mcp-cli`**：dsh 的 MCP client 不保证把 profile 的 `node_modules/.bin` 放进 PATH，Windows 的 `.cmd` shim 也不一定可 spawn。`cordis.patch.yml` 用 `process.execPath` 直接启动本包内的 `.cjs` shim（照抄 `dhicoc/dsh-codex-web-search-mcp` 的已验证做法）。**已知边界**：Electron 桌面端里 `process.execPath` 可能是 Electron 可执行文件而不是 Node，此时给该行补 `env: { ELECTRON_RUN_AS_NODE: '1' }`，或退回 `.mcp.json` 写法用 `command: websearch-mcp-cli`；CLI 版 dsh 不受影响。真机验收前不要宣称桌面端已支持。
- **不声明 `@deepseek-ai/dsh-mcp-client` 为依赖**：官方说明内置组合包名称从 dsh 安装目录本身解析，pnpm 只管理树外包；钉死一个预览版 rc 号反而容易装不上。
- **不加 `engines.dsh`**：dsh 仍在 developer preview（`0.2.2-rc.1` 这类预发布版本不满足 `>=0.2.0`），写死范围会把插件挡在门外。兼容范围写 README，不写 manifest。
- **npm 包名一律小写**：npm 不接受新包用大写，`@daidaiJ/...` 这种 scope 会直接 `EINVAL`。这里统一用未加 scope 的名字，也省掉 npm 组织/用户 scope 归属的前置条件。

## 版本与前置条件

- `TAG=vX.Y.Z`（或触发 tag `vX.Y.Z-npm`）→ npm `version=X.Y.Z`。`-npm` 只是触发标记：`publish.sh` 先剥掉它，用 base tag 找 Release、用 base 版本号发 npm，不会把 `-npm` 发成 `3.10.5-npm` 这种 next 通道版本。base 版本含 `-preview` / `-rc` 后缀时自动用 `--tag next`，正式版用 `latest`。
- 前置：6 个包在 npmjs.com 各自绑定一条 Trusted Publisher（字段见下节），工作流 job 需 `id-token: write`。CI 用 **Node 24 + npm 11**（Trusted Publishing 要求 npm ≥ 11.5.1 / Node ≥ 22.14）。仓库与 CI **不保存任何长期 npm token**。首次发布前确认 6 个名字都未被占用：`npm view <name> version` 返回 404 即可用。
- **npm 版本不可复用**：同版本二次 publish 必报 403。`publish.sh` 先 `npm view <name>@<version>`，已存在就跳过并打 notice，所以重跑工作流是安全的；但内容改了就必须换版本号。

## 凭据安全（Trusted Publishing / OIDC）

**仓库与 CI 里没有任何长期 npm token。** 每次发布由 npm CLI 用 GitHub OIDC 现场换取短期凭据：绑定仓库 + 工作流文件名 +（如配置）Environment，不可从日志提取或复用；npm 还会自动生成 provenance 签名。

### npm 侧配置（6 个包逐个设置）

在 npmjs.com → 包 Settings → Trusted publishers 各加一条：

| 字段 | 值 |
|------|-----|
| Organization or user | `daidaiJ` |
| Repository | `websearch-mcpserver` |
| Workflow filename | `npm-plugin.yml`（**只填文件名**，不能填 `release.yml`） |
| Environment name | 与 workflow job 的 `environment:` 完全一致（当前 `PANDA`）；没填就把那行删掉 |
| Allowed actions | 勾 `npm publish`（我们直发）；`npm stage publish` 可选 |

**不想在网页上点 6 次**：用 `npm trust` CLI 一条循环批量配（npm ≥ 11.15 / Node ≥ 22.14 / 账号已开 2FA / 本机已 `npm login`），首次 2FA 时在 npm 网页勾「接下来 5 分钟跳过 2FA」，其余 5 个自动完成：

```bash
for pkg in dsh-websearch-mcpserver websearch-mcp-cli \
           websearch-mcp-cli-linux-amd64 websearch-mcp-cli-windows-amd64 \
           websearch-mcp-cli-darwin-amd64 websearch-mcp-cli-darwin-arm64; do
  npm trust github "$pkg" \
    --repo daidaiJ/websearch-mcpserver --file npm-plugin.yml --env PANDA \
    --allow-publish --allow-stage-publish --yes
  sleep 2   # npm 官方 bulk 用法建议间隔 2 秒防限流
done
```

官方 bulk 用法说明这个 5 分钟窗口足够配约 80 个包。注意 npm 目前**每个包只能有一条** trusted publisher：改字段要先 `npm trust list <pkg>` 取 ID、`npm trust revoke --id <id> <pkg>` 再重建；且 `npm trust` 要求包已存在于 registry，新增包必须先发一次。

几个必须记住的坑：

- **不能经 `workflow_call` 转发**。npm 官方 troubleshooting 明确：复用工作流被调用时，校验的是「调用方」工作流文件名，而不是实际跑 `npm publish` 的文件。所以 `release.yml` 已删掉 `publish-npm` job，`vX.Y.Z-npm` tag 由 `npm-plugin.yml` 自己接（`workflow_dispatch` 同理，调用方仍是它自己）。
- npm 的 trusted publisher 配置**不能编辑，只能删掉重建**。改字段前先确认，尤其是 Environment name 的大小写。
- 新建后状态是 `Pending validation`，**必须在页面提示的截止时间内用 OIDC 成功 publish 一次**才转正。npm 版本不可复用：重跑已发布的版本只会全部跳过，不算验证。首次验证已由 `v3.10.6` → `v3.10.6-npm` 完成（6 个包 exchange 全 201、provenance 自动签名），结果见文末「Trusted Publishing 首次验证」。发布后 npm 走 staged 队列逐个放行，`latest` 不会 6 个包同时更新；网页有缓存，以 `npm view <name> version` 为准。
- 验证通过后再把包 Settings → Publishing access 设为 **"Require two-factor authentication and disallow tokens"**，彻底关掉长期 token 通道；早期设的 Actions secret 那时可删（仓库里已无任何文件引用）。
- 预发布走 `--tag next`：OIDC 下改 dist-tag 需 npm 侧另勾 `Allow npm dist-tag` 且 npm ≥ 11.21.0。正式版刻意不传 `--tag`，用默认 `latest`，少一个权限依赖。

### 仓库侧仍保留的兜底

- 本机**不要**为发布建 `.npmrc`（auth token 赋值会落盘），也不要把任何 token 写进 `.env`、脚本、文档或提交信息。`.gitignore` 已忽略 `.npmrc` / `.env*` / `*.tgz`；本地出包用 `PACK_OUT` 模式，完全不需要凭据。
- 两道扫描照旧：`npm/scripts/check-credentials.sh` 在 PR 的 `npm-plugin-check` 与发布工作流都跑（本地 `check.sh` 也会调用），`publish.sh` 的 `assert_no_credentials` 在**每个包打包前**再扫一次 staged 文本文件——出现 `.npmrc`、auth token 赋值或疑似 npm token 就中止，且只报文件名、不回显命中行。OIDC 时代这防的是「有人误把长期 token 提交进包」：npm 包公开且版本不可撤回，日志打码救不回来。
- 不开 `set -x`、不打印 env、不上传 workspace artifact；`setup-node` 不设 `registry-url`，避免生成带 token 占位符的 `.npmrc` 干扰 OIDC 认证（`npm-plugin.yml` 里已注明）。

## 发布链路

发布矩阵与仓库既有约定对称：**普通 tag 只发 Release + GHCR，`vX.Y.Z-npm` 单独发 npm，正如 `vX.Y.Z-registry` 单独发 MCP Registry**。

1. 普通 `vX.Y.Z` tag：`release.yml` 的 `build` → `release` 产出 8 个裸二进制 + `.sha256` + 整包，`docker.yml` 推 GHCR；**不发 npm 包**。
2. 产物就绪后另打 `vX.Y.Z-npm`。`release.yml` 的 `on.push.tags` 已排除 `-npm`（不再产生全 job 跳过的空 run），`docker.yml` 同样跳过；只有 `npm-plugin.yml` 被这个 tag 直接触发，**调用方工作流就是它自己**，OIDC 身份才与 npm 侧绑定匹配。不重编译、不建 Release、不推镜像。
3. `publish.sh` 剥掉 `-npm` 得到 base tag `vX.Y.Z`，从 **base tag 的 GitHub Release** 下载 `websearch-mcp-cli-*`、逐个用 Release 的 `.sha256` 校验，再依次发布 **4 个平台包 → `websearch-mcp-cli` → `dsh-websearch-mcpserver`**（平台包必须先可解析，否则父包上线到能装之间会有窗口）。
4. 平台包在 `bin/third_party/pdfium/<架构>/` 随附同版本 PDFium 动态库（固定 `chromium/8152`，与 Release 整包 / GHCR 镜像 / MCPB 同源），`publish.sh` 在打包前用 `third_party/pdfium/fetch.sh` 的产出做预检；缺失直接失败，`FAKE_BINARIES=true` 时放占位文件只验证打包链路。
5. 校验和随二进制一起进 npm 包（`bin/websearch-mcp-cli*.sha256`），来源可复验。
6. 失败或需要补发：Actions 里手动跑 `npm-plugin.yml`，tag 填 `vX.Y.Z` 或 `vX.Y.Z-npm` 都行；`dry_run=true` 只打包不发布。npm 版本不可复用，`publish.sh` 对已存在版本会跳过，重跑安全。

## 本地校验

```bash
# 真实产物出包前准备四个架构的 PDFium；FAKE_BINARIES 模式会放占位文件
bash third_party/pdfium/fetch.sh linux-x64 win-x64 mac-x64 mac-arm64

# 结构检查：JSON 可解析、无生命周期脚本、shim 语法、patch 关键字段、平台 os/cpu
bash npm/scripts/check.sh

# 完整 dry-run：用占位二进制模拟 Release 产物，逐个 npm pack --dry-run，不联网发布
TAG=v0.0.0 DRY_RUN=true FAKE_BINARIES=true bash npm/scripts/publish.sh

# 手里已有 dist/ 时改用真产物（不下载 Release）
TAG=v3.10.5 DRY_RUN=true DIST_DIR=./dist bash npm/scripts/publish.sh

# 离线出 6 个真实 .tgz（不发布），供本机 dsh 真机验收用
TAG=v3.10.5 PACK_OUT=/tmp/websearch-npm DIST_DIR=./dist bash npm/scripts/publish.sh
dsh plugin --profile web add /tmp/websearch-npm/dsh-websearch-mcpserver-3.10.5.tgz
```

`PACK_OUT` 模式下 `publish.sh` 只跑 `npm pack`，不查 registry、不需要任何凭据。平台包的依赖按同版本钉死，本地验收时把 4 个平台 tgz 一起装（`dsh plugin` 会按 `os`/`cpu` 只挑一个）。

`ci.yml` 的 `npm-plugin-check` job 在 PR 上跑的正是前两条中的 fake-binaries 组合。

## 2026-11-07 本机验收结果（本地 tgz，非 npm 发布版）

用 `TAG=v0.0.0 PACK_OUT=... DIST_DIR=... bash npm/scripts/publish.sh` 出 6 个真实 tgz，本地编 `cmd/cli`（Windows amd64，`-X main.version=v0.0.0-local`），在隔离 `DSH_HOME` 与真实 `~/.dsh/profiles/web` 各装配一次，dsh CLI `0.2.2-rc.1`、Node 24 实测：

| 项 | 结果 |
|---|---|
| `dsh --profile web --dump-config` | 退出码 0，出现 `# == dsh-websearch-mcpserver` 层与 `mcp-websearch` 行；`!!js` 表达式按 dsh loader 的 `with(ctx){eval(expr)}` 求值后指向真实存在的 shim 文件 |
| config-only bundle 识别 | 通过。`dsh.bundle.patch` + `files` 含 `cordis.patch.yml` 即可贡献配置层，无需 `apply(ctx)`；空 ESM `index.js` 不影响 |
| shim 拉起 Go 二进制 | `node <shim> version` → `v0.0.0-local`；`optionalDependencies` 在 hoisted 布局下正确挑中 `websearch-mcp-cli-windows-amd64` |
| MCP 握手 | `initialize` → `websearch server v0.0.0-local`；`tools/list` → `academicsearch, cleanfetch, smartsearch`（零配置只启用 3 个工具） |
| 个性化配置 | `WEBSEARCH_MCP_CONFIG` 指向含 `mode: mcphub` / `language: en` 的 yaml 后，启动日志变为 `search mode: mcphub` 且日志切英文，握手与工具清单正常 |
| 非 web profile / `DSH_PROFILE` | 隔离 `DSH_HOME` 下求值路径正确，未做第二个具名 profile 的真机启动 |

**还没验**：`dsh plugin add` 从 npm registry 安装（包未发布）、四个平台各装一次、dsh 真实会话里模型侧工具调用与非 web profile 启动。本机 `~/.dsh/profiles/web` 目前是**本地 tgz 手工装配态**（`package.json.bak-websearch-20261107` 为改动前备份），发布后应删掉手工放进 `node_modules/` 的 6 个目录并改用 `dsh plugin --profile web add dsh-websearch-mcpserver` 重装；`pnpm install` 也可能清掉手工目录。

## 真机验收清单（发布后必做）

- [ ] `dsh plugin --profile web add dsh-websearch-mcpserver` → `dsh --profile web --dump-config` 能看到 `mcp-websearch` 行 → `dsh web` 重启。
- [ ] 工具名形如 `mcp__websearch__smartsearch` / `academicsearch` / `cleanfetch` / `pdf_parser` / `file_search`。
- [ ] `npm i -g websearch-mcp-cli` 后 `websearch-mcp-cli version` 输出与 tag 一致；任意 MCP 客户端用 `command: websearch-mcp-cli` 能握手。
- [ ] 四个平台各装一次，确认 `os`/`cpu` 只拉到本平台那一个二进制包。
- [ ] 带 `WEBSEARCH_MCP_CONFIG=/abs/config.yaml` 启动，确认自定义 mode / Key 生效（这是体验分水岭，见 README 的强调段）。
- [ ] 非 `web` profile：设 `DSH_PROFILE=<name>` 再启动，或按 `npm/plugin/README.md` 在该 profile 的 `cordis.patch.yml` 覆盖 `mcp-websearch` 行。

## Trusted Publishing 首次验证（2026-11-07，v3.10.6）

由 `v3.10.6` → `v3.10.6-npm` 完成，纯 OIDC、发布 step 不注入任何长期 token：

| 项 | 结果 |
|---|---|
| OIDC 身份 | `sub=repo:daidaiJ/websearch-mcpserver:environment:PANDA`，`workflow_ref` / `job_workflow_ref` 都是 `.github/workflows/npm-plugin.yml`，与 npm 侧 6 条配置一致 |
| 6 个包 | `npm publish` 全部成功，exchange 端点逐个返回 `201`，npm CLI 打印 `Enabling provenance` |
| provenance | 自动签名并写入 Sigstore transparency log（如 `dsh-websearch-mcpserver@3.10.6` → logIndex `3736129319`），脚本未显式传 `--provenance` |
| registry | 6 个包 `latest` 均为 `3.10.6`；首发留下的 `0.0.0-stage` 占位版本仍在，无害。staged 队列逐个放行，最后上线的 `dsh-websearch-mcpserver` 比其余晚约 1 分钟 |
| 第一次为什么断 | 首次跑时 `websearch-mcp-cli` 的 exchange 返回 `404 package not found`（当时该包还没绑定 trusted publisher），发布在它上面中止；补配后重跑，`publish.sh` 跳过已发的 4 个平台包、只补余下 2 个 |

排障：`workflow_dispatch` 勾 `verbose` 会打印 OIDC claims 与 npm 的 exchange 返回码。只看到 `ENEEDAUTH` 时先看这里——npm CLI 在 exchange 失败时只写 verbose 日志、静默退回 token 认证，表面看不出是「包没绑定」还是「Environment 不匹配」。
