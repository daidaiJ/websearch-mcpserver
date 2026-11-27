# AGENTS.md — 智能体协作指南

> 本文件帮助 AI 智能体快速理解项目结构和开发约定，确保长期维护与持续开发的一致性。

---

> 包接口、工具调用链与改动落点详见 [docs/developers.md](docs/developers.md)。

---

## 项目一句话定位

**websearch-mcpserver** 是一个用 Go 编写的轻量级 MCP 搜索服务，零 API Key 即可运行，支持 Claude Code / Qwen Code / Cursor 等 MCP 客户端。提供五大核心能力：

| 能力 | 说明 |
|------|------|
| **搜索引擎搜索** | 内置百度/Bing/DuckDuckGo/Google 多引擎并发编排，支持 Tavily/Exa/AnySearch/豆包/元宝等 API 引擎混合 |
| **API 搜索** | API Key 池轮转模式，支持百度千帆/Tavily/Exa/AnySearch/豆包/元宝等多种 API 供应商，失败自动切换 |
| **学术搜索** | arXiv/Crossref/OpenAlex/PubMed/Semantic Scholar/Google Scholar/Europe PMC/DBLP/DOAJ 九大学术引擎并发，DOI 跨引擎去重、逐引擎错误透传 |
| **网页抓取** | 增强型网页内容提取（TLS 指纹伪装 + SSRF 防护 + Jina Reader 备选） |
| **PDF 解析** | MinerU AI 增强 PDF 解析（表格/公式/多栏/图片智能识别），无 Token 自动降级；可选实验性 PDFium 动态库文本后端 |

---

## 技术栈速览

| 项 | 值 |
|---|---|
| 语言 | Go 1.26+，纯 Go 无 CGO |
| 数据库 | SQLite（modernc.org/sqlite，纯 Go 实现） |
| 配置 | Viper（YAML + 环境变量覆盖） |
| 日志 | Zerolog 结构化日志 |
| MCP 协议 | modelcontextprotocol/go-sdk |
| HTTP 客户端 | resty.dev/v3 + go-webfetch（TLS 指纹伪装） |
| 部署 | 单二进制 / Docker 多阶段构建；Release 与 GHCR / MCP Registry 矩阵拆分（见下方提示） |

---

## 目录结构与模块职责

```
cmd/              # 入口：main.go + 平台初始化（Windows 代理检测等）
mcp/              # MCP 协议层：工具注册、请求处理
server/           # HTTP 服务：生命周期、路由

pkg/
├── search/       # ★ 搜索编排层（factory.go 为组合根，只做装配与类型别名）
│   ├── core/           # 类型契约：SearchInf/SearchResult/ScoreBucket + 结果格式化
│   ├── engine/         # 底层网页引擎（HTTP 抓取层）：baidu/ bing/ ddg/ google/
│   ├── provider/       # API 供应商适配器：tavily/ exa/ anysearch/ doubao/ baidu*，KeyPool 轮转
│   ├── adapter/        # 本地引擎/学术适配器：EngineSearchAdapter/ Bing/ 百度回退/ 学术
│   ├── apipool/        # 跨供应商轮转策略（round-robin/priority/weighted）
│   ├── hybrid/         # 多引擎并发编排策略（去重、合并、per-engine 过滤）
│   ├── enhance/        # 高阶评分功能：RRF/域名品质/词汇对齐/MMR/学术评分增强
│   └── mode/           # 搜索模式构建：按 config.mode 组装 Primary（engine/baidu/apipool/hybrid…）
├── antirobot/    # 反检测公共层：Searcher 接口、限流器、TLS 指纹（跨层公共，保持顶层）
├── academic/     # 学术搜索：arXiv/Crossref/OpenAlex/PubMed/S2/GS/EuropePMC/DBLP/DOAJ
├── fetch/        # 抓取与解析族
│   ├── webfetch/       # 增强型网页抓取（SSRF 防护、DNS rebinding 检测、PDF 解析分支）
│   ├── jina/           # Jina Reader 备选抓取
│   ├── mineru/         # MinerU PDF 解析
│   └── pdfium/         # 实验性 PDFium 动态库文本后端（purego 懒加载）
├── llm/          # LLM：Client + 搜索摘要 Summarizer
├── config/       # 配置加载与结构体定义
├── cache/        # SQLite 缓存（6h 过期，后台清理）
├── client/       # HTTP 客户端（API 供应商共用）
├── proxy/        # 系统代理自动检测（Windows 注册表 / 环境变量）
├── daemon/       # 引用计数进程管理
├── log/          # 日志配置
└── third_party/  # 可选预编译动态库（PDFium，按架构；CI 下载后随整包分发）
```

---

## 核心数据流

```
用户请求 → MCP Tool / HTTP API
         → factory.go 选择引擎组合
         → hybrid.go 并发调用多引擎
         → 各引擎适配器 → antirobot 层（反检测/限流）→ 底层 HTTP 请求
         → 结果回传 → 去重 → per-engine score 过滤 → 排序/轮询合并
         → 可选 LLM 摘要 → 返回用户
```

---

## 开发约定

### 新增搜索引擎

1. 在 `pkg/search/engine/` 下创建独立包（如 `pkg/search/engine/newengine/`）
2. 实现 `antirobot.Searcher` 接口（`Name()`, `Search()`, `SearchRaw()`）
3. 在 `pkg/search/` 下创建适配器文件，实现 `search.SearchInf` 接口
4. 在 `pkg/config/config.go` 中添加引擎配置结构体
5. 在 `pkg/search/factory.go` 中注册引擎到工厂

### 配置变更

- 所有配置结构体在 `pkg/config/config.go`
- 样例配置同步更新 `config.example.yaml` 和 `pkg/config/config.example.yaml`
- 用户文档同步更新 `docs/configuration.md` / `docs/configuration.en.md`；搜索模式或 MCP 工具参数改动还要改 `docs/search.md`、`docs/search.en.md`、`docs/api.md`、README
- 环境变量覆盖格式：`WEBCRAWLER_` 前缀 + 大写路径（如 `WEBCRAWLER_PORT`）

### 测试

- 测试文件与源文件同目录，命名 `*_test.go`
- 运行测试：`go test ./...`
- 引擎测试需要对应引擎可用（或 mock）

### 国际化（i18n）

- 用户可见文案一律走 `pkg/i18n` 的符号化 key，**不在调用点写裸字符串**：日志 `log.InfoT("log.<域>.<事件>", "name", v)`、Go 侧 `i18n.T` / `i18n.TCtx`、MCP 参数说明 `jsonschema:"mcp.param.<工具>.<字段>"`、前端 `t('web.<区域>.<名字>')`
- 文案数据住 `pkg/i18n/locales/<域>.<语言>.json`（域：`log` / `mcp` / `dashboard` / `web`）；加文案只动 JSON，加语言 = 加一组同域文件 + `pkg/i18n/language.go` 的 `supported`
- 改完必须跑 `go test ./pkg/i18n/`：目录审计会拦截漏登记、死条目、zh/en key 集合不一致、占位符集合不一致
- 完整约定与生命周期（日志/MCP 为进程默认语言、HTTP 面按 `Accept-Language` 每请求协商）见 [docs/developers.md](docs/developers.md) 第 7 节

---

## 当前系统能力边界（智能体需 aware）

以下为当前已知的能力边界，开发时不要重复踩坑：

| 模块 | 现状 | 结论 |
|------|------|------|
| 通用评分管线（RRF/域名品质/词汇对齐/稀有词/共识权威新鲜度 Boost/阈值过滤/意图分类/MMR） | v2.14.0 已全部实现 | 不要重复立项 |
| Google 网页引擎 | 2025-01 起 JS 挑战全量硬化，"凭据缺失"模型，HTTP 200 空壳零结果；UA/TLS 伪装全部失效（详见 `pkg/config/config.go` GoogleConfig 注释） | 保持默认关闭，勿再尝试伪装修复 |
| Google wml + Nokia UA 绕过（SearXNG PR #6546 路线） | 2026-09-03 实测：HK 代理出口下首请求 429 进 /sorry/，其余 200 均为 JS 挑战空壳，Google 未对 Nokia UA 返回 WML/XML | 强依赖出口 IP 信誉，非普适方案；wml 遗留端点随时可能被 Google 移除，勿照抄 |
| 百度 tn=json 接口（SearXNG baidu.py 路线） | 2026-09-03 实测：直连裸客户端 3/3 被 302 至 wappass 验证码，预热 cookie 无效；同 IP 下 HTML 引擎同样被 CAPTCHA。识别主因疑似 IP 信誉 + TLS 指纹，与 HTML/JSON 入口无关 | tn=json 非免检通道，JSON 接口不能替代 pkg/search/engine/baidu 现有反检测层 |
| DDG / arXiv 限流 | 服务端窗口限流，引擎内置钳制（DDG 1/s·6/min，arXiv 1/s·12/min + 3s 间隔）+ 429 冷却避让 + 预算感知重试 | 调整限流参数须实测校准，勿放宽内置上限 |
| 360 搜索（so360，2026-10-06 出口实测） | 直连与代理均 HTTP 200 + 自然结果（`li.res-list` + `data-mdurl` 真实 URL，`div.g-mohe` 特型卡需过滤）；解析契约对齐上游 so360.py | 已注册（`so360.enabled`，默认关闭）；验证码页 wappass 按反爬拦截报错 |
| AnySearch 匿名档（2026-10-06 出口实测） | 直连与代理均 200，POST 不带 Authorization 即匿名 IP 限流档（上游 anysearch.py 注释同口径），结果在 `data.results` | KeyPool 为空自动走匿名档，本端保守钳制 1/s·20/min；上游未公布阈值，调参须实测 |
| Wikipedia / Google News（2026-10-06 出口实测） | 直连均超时；代理下 Wikipedia MediaWiki API 200 + 正常 JSON，Google News RSS 200 + 52 item 且 batchexecute RPC 可回源发布方 URL（f.req 须三层嵌套 + ts 数字形态，否则 400） | 已注册（均需代理，无可用代理解析时跳过）；Google News 解析契约对齐上游 gnews.py |
| 腾讯云元宝搜索（2026-10-24 接入） | API KEY 方式 `POST api.wsa.cloud.tencent.com/SearchPro`，Bearer sk-...，返回 `Response.Pages[]`（JSON 字符串）；每月免费 2000 次，超出按量后付费（约 20 元/千次） | 已注册（`yuanbao.*`，默认不加入 apipool；hybrid 有 Key 时参与）；`yuanbao.monthly_limit` 默认 2000 并在请求前落盘拦截，禁止默认放开 |
| TinyFish / Keenable 供应商（2026-10-24 接入） | TinyFish 仅 Key 档（免费搜索 12000 次/天、fetch 1000 URL/天，00:00 UTC 重置，国内直连实测无需代理）；Keenable 未配 Key 自动走 public 免 Key 端点（`X-Keenable-Title` 标识，按出口 IP 限流） | 均已注册（`tinyfish.*` / `keenable.*`）；两者默认不进 `apipool.engines`，hybrid 中 TinyFish 有 Key 即参与、Keenable 的 public 档需显式 `enabled: true` 才参与（质量待观察）；fetch 能力并入 cleanfetch 远端抓取链（沿用 `mcp_hub.fetch_mode`） |
| `search_history` 本地历史（2026-10-24 接入，试验性） | smartsearch 成功后落本地 SQLite（FTS5），`search_history` MCP 工具全文检索；数据不出网，默认保留 30 天 | `search_history.enabled` 零值 = 不建库、不注册工具；实用性待真机确认，确认有价值后再转正式工具 |
| PDFium DLL 文本后端（2026-11-14 接入，实验性） | purego 懒加载 `pdfium.dll` / `libpdfium.so` / `libpdfium.dylib`；Release 整包、GHCR 镜像、MCPB 与 npm 平台包都携带固定 `chromium/8152` 标准库（分别位于 `third_party/pdfium/<架构>/`、`/app/third_party/pdfium/<架构>/`、`server/third_party/pdfium/<架构>/`、`bin/third_party/pdfium/<架构>/`）；`pdf_parser.pdfium` 省略 enabled = 库存在才启用 fallback，`mode: primary` 可设为本地默认；34 条病态输入无挂死/panic，20p warm DLL 9.15ms vs ledongthuc 2.40ms；Linux `.so` 与真实 PDF 长跑未验证 | 默认 auto 只在库存在时启用；C 层 SIGSEGV 无法 recover，生产需 worker 隔离；wasm 退路冷启动 4.17s、峰值 503MB，勿当生产方案；CI 下载失败应让对应发布流程失败，不要静默跳过 PDFium |
| 国际化（i18n，2026-10-25 治理） | `language: auto|zh|en` 覆盖日志 / MCP 工具描述与参数 Schema / 控制中心 / 工具返回内容四面（八域）；文案住 `pkg/i18n/locales/<域>.<语言>.json`，go-i18n + x/text 运行时，审计测试 `pkg/i18n/catalog_test.go` 双向比对代码引用与目录条目 | 新增文案只动 JSON + 用 key，勿在调用点写裸字符串；MCP 描述注册时固化、改语言需重启；HTTP 面按 `Accept-Language` 每请求协商；工具返回内容只治理本服务自产文案（外部内容与 `format_md.go` 骨架不动）；改错误文案必须同步核对三处关键词判类 |
| GitHub（2026-10-24 接入） | `api.github.com` 国内直连可达无需代理；匿名 search 10/min、core 60/h，PAT 提额 search 30/min、core 5000/h（官方 docs 核对）；github.com HTML 是 JS 渲染抓不到，搜索与 cleanfetch 仓库/blob 特判均优先走 REST API（`blob` 走 contents API 取原文——raw.githubusercontent CDN 国内无代理不可达，勿改回），API 失败自动回退（搜索回 Bing `site:github.com`，抓取回 webfetch → Jina 链）；code search 要求认证不做 | 已注册（`github.enabled`，默认关闭；`github.api_key`/`token_list` 配 PAT，env `GITHUB_TOKEN`）；限流冷却取 Retry-After / x-ratelimit-reset；EngineAdapters.GitHub 收接口类型（typed-nil 陷阱见 InitGithubEngine 注释） |
| Reddit（2026-10-24 评审后整线放弃） | 浏览器 UA 全拦（HTML/.json/api 均 403），`.rss` 用脚本 UA 可达但 PullPush 被 Cloudflare 挡死；官方 2026-09-30 公告：新公共 API 申请 2026-10-31 截止、RSS 2026-11-13 全线停、未注册 app 2027-01-12 移除、公共 Data API 2027-03 终止 | 两条候选通道（OAuth script app / RSS 兜底）均在死线上，收益期 < 5 个月，不立项；OAuth 调研结论留档 docs/optimization-roadmap.md N7，上游重开（Devvit/普适免费档）再议 |

---

## 快速上手命令

```bash
# 构建
go build -o websearch-mcpserver ./cmd/

# 运行（零 API Key 模式）
./websearch-mcpserver

# 运行测试（网络集成测试按场景自动跳过；WS_TEST_NETWORK=on 强制执行、不可达即 fail）
go test ./...
go test -short ./...   # 快速模式，跳过所有网络集成测试

# Docker 构建
docker build -t websearch-mcpserver .
```

---

## 给智能体的提示

1. **修改搜索逻辑前**，先读 `pkg/search/inf.go` 了解接口契约，再读 `hybrid.go` 了解编排流程
2. **新增配置项时**，同步更新 `pkg/config/config.go`、两个 `config.example.yaml`，以及 `docs/configuration.md` / `docs/configuration.en.md`（搜索/工具参数还要改 `docs/search.md`、`docs/api.md`、README）
3. **涉及反检测/限流**，修改应在 `pkg/antirobot/` 层进行，不要在各引擎包中重复实现
4. **学术搜索与通用搜索是独立模块**，学术引擎在 `pkg/academic/`，通用引擎在 `pkg/search/engine/baidu/` `pkg/search/engine/bing/` 等，不要混淆
5. **发布矩阵拆分，不要合成一套 6 平台**：GitHub Release = linux/windows amd64 + darwin amd64/arm64；GHCR = linux/amd64+arm64；MCP Registry mcpb 走 `vX.Y.Z-registry` tag 的**独立 Release 页**（`--expect-packages 4`，server.json 下载链接指向该页，base Release 只放裸二进制与每平台整包（含 `skills/`），不放 mcpb，两类产物分开）。**先打普通 tag（`vX.Y.Z`）发 Release 并推 GHCR 镜像**；**发完、Release 产物就绪后再单独打 `-registry` 后缀 tag 发 MCP Registry**（不推镜像，不要和版本 tag 一起推）。后补的 `vX.Y.Z-registry` 钉同一 commit（重跑工作流时需钉到含新 workflow 的 commit——打包原料从 base Release 下载，不重编译）。linux-arm64 走 GHCR，不要把 linux-arm64 / windows-arm64 加回 Release 来对齐 Docker 或旧版 v3.1.1 MCP
6. **README 顶部配图有渲染源**：`docs/images/src/`（HTML + Playwright 脚本，`python docs/images/src/render.py` 直接覆写 `docs/images/*.png`），改图改 HTML、不要手工编辑 PNG；源与用法见 `docs/images/src/README.md`
7. **发布矩阵对称：npm 走 `-npm` 后缀 tag，不随普通 tag 发**。普通 tag（`vX.Y.Z`）只发 Release + GHCR；产物就绪后再单独打 `vX.Y.Z-npm`，由 **`npm-plugin.yml` 自己的 `on.push.tags` 接**（`release.yml` 的 tags 已排除 `-npm`，`docker.yml` 也跳过），从 **base tag** 的 Release 下载并校验 `websearch-mcp-cli-*`，按「4 平台包 → `websearch-mcp-cli` → `dsh-websearch-mcpserver`」顺序发 6 个包，npm 版本取 base 版本（`v3.10.5-npm` → `3.10.5`，`publish.sh` 剥 `-npm` 后缀）。这与 `-registry` 发 MCP Registry 的链路对称：两种后缀 tag 都不重编译、不建 base Release、不推 GHCR 镜像。**发布认证是 npm Trusted Publishing（OIDC）**：`npm-plugin.yml` job 需 `id-token: write` + Node 24 + npm 11，仓库不存任何 npm token；**不要改回 `release.yml` 的 `workflow_call` 复用**——npm 校验的是「调用方」工作流文件名，复用会与 npm 侧绑定的 `npm-plugin.yml` 失配。**平台矩阵必须跟 Release 四平台一致**，不要为对齐 Docker 加 linux-arm64 / windows-arm64；npm 版本不可复用，补发用 `npm-plugin.yml` 的 `workflow_dispatch`（tag 填 base tag 或 `-npm` tag 均可，`publish.sh` 对已存在版本跳过）。改 `npm/` 下的模板后本地跑 `bash npm/scripts/check.sh` 与 `TAG=v0.0.0 DRY_RUN=true FAKE_BINARIES=true bash npm/scripts/publish.sh`。详见 `npm/README.md`

---

## WebUI（控制中心）规范

> 控制中心 = `pkg/dashboard`（后端）+ `pkg/dashboard/web`（前端）+ `pkg/telemetry`（遥测）+ `pkg/quota`（额度），随主分支演进。

1. **前端构建与离线嵌入**：维护入口是 `frontend/src/`（React、TypeScript、Radix 与 Tailwind）。在 `frontend/` 运行 `npm ci`、`npm test`、`npm run build`，将生成的 `pkg/dashboard/web/dist/` 与源码一同提交。Go 直接嵌入页面、脚本、样式与本地 WOFF2 字体，普通 Go 构建和运行均无需 Node 或 CDN。正式入口为 `/dashboard/`。外挂替换用 `dashboard.ui.dir`；极限体积用 `-tags no_ui`，此时未配外挂目录的 `/dashboard/` 会返回明确的 500。中英文文案由 `pkg/i18n` 目录注入，运行 `go test ./pkg/i18n/` 审计 TS / TSX 引用。
2. **访问与操作安全模型（不可退让）**：页面和只读 API 由 loopback 与 `allowed_networks` 放行。
   - 两类操作都按真实连接来源校验，默认仅 loopback，可在 `dashboard.yaml` 用 `write_allowed_networks` 放行 CIDR/IP。空值或任一非法条目均回退 loopback。读白名单和转发头不授予操作权限。
   - **免密运行态操作仅限**解除单源或全部路由冷却、强刷 Tavily / TinyFish 官方额度、清搜索缓存、清空搜索历史、清理过期或全部合规临时抓取文件、手动重探 Everything、代理连通检查、单源试搜、单 URL 试抓。免密 POST 只要求通过操作来源校验。浏览器跨站请求会被拒绝，无 `Origin` 的原生客户端仍按 `RemoteAddr` 校验。可能消耗供应商额度的试搜和试抓仍会提示并要求确认。试抓可能依次调用多个远端服务。
   - **其余管理操作**（设置保存与备份恢复、密钥管理、重启、额度重置与修正、查看 Everything 完整索引范围）须通过页面/API 访问边界和操作来源校验，并提供 `X-Admin-Password`，配置用户名时还须 `X-Admin-User`，服务端常量时间比较。口令只能配置在 `dashboard.yaml`（`admin_password` / `admin_password_sha256`），不能从界面读取或修改，也不写入 secrets 覆盖文件。未配置口令时仅禁用需要验密的操作。
3. **配置分层与回退**：控制中心专属配置（口令/网段/额度/品牌）住独立 `dashboard.yaml`（`config.Load` 的 overlay，`WEBSEARCH_DASHBOARD_CONFIG` 可改路径），按字段覆盖主配置；主 `config.yaml` 零改动即可升级，删文件即回退。新键一律"零值 = 安全默认"，不允许要求老配置迁移脚本。
4. **隐私出网红线**：遥测只存脱敏元数据（查询哈希/主题/语言/关键词），错误文本服务端正则脱敏后才落库；密钥值任何接口永不回显（服务端不出网，前端掩码不算数）。新增端点时先过一遍"响应体会不会带密钥/原文"。
5. **品牌可定制**：`dashboard.brand`（title/logo/theme/accent）走 overview/settings 响应注入；主题 = `green/blue/mono` 三预设（CSS 变量块）+ accent 覆盖，本浏览器选择存 localStorage。桌面快捷方式图标跟主题（`cmd/assets/app-{theme}.ico`）；`start`/`open`/`install` 读取 `.lnk` 逐项校验目标、参数、工作目录与图标，主题或图标内容变化时落盘带内容哈希的新 ico 并重建。
6. **配置文档唯一入口**：控制中心所有可配置项（含 `dashboard.shortcut` 快捷方式落位、`brand.footer` 关于块）的完整参考与「agent 向人类确认清单」住 `docs/dashboard.md` / `dashboard.en.md`；新增配置键必须同步该文档与 `dashboard.example.yaml`，不允许只在代码注释里出现。
7. **图标资产再生成**：改设计 → `go run ./tools/genicon`（在仓库根目录跑），产出 3 主题 ico + web logo，不要手工改二进制资产。

## 本地环境

- **Go 构建临时目录**：`GOTMPDIR=D:\tmp`（`go env -w` + 用户环境变量已配置）。卡巴斯基锁定系统 TEMP 会导致 `go build` 偶发 Access denied，构建/测试编译临时目录已统一指向 `D:\tmp`，在该目录配置杀软排除即可规避；`go test` 的 `t.TempDir()` 数据目录仍走系统 TEMP（改全局 TMP/TEMP 影响面大，未动）。
- **Go 源码行尾已统一 LF（2026-10-25，`f342a4d`）**：`.gitattributes` 钉死 `*.go text eol=lf`，工作树已全量转 LF、gofmt 历史格式债已清——此后 `gofmt -l` 报告皆为真实问题，直接修；勿删 `.gitattributes`，否则 autocrlf 检出 CRLF 会让 gofmt 重新全文件误报。

## 提交与发布卫生

1. **推送前必须按功能 rebase 压缩**（硬规则）：分支历史按功能聚合成少量提交（如 PR 引入层 / 安全加固层 / 功能层 / 文档层各一个），不推"一堆 WIP 碎提交"。吸收外部 PR 时保留原作者（`git commit --author`），功能分组用 `git checkout <src> -- <paths>` 按路径重建，每步过 `go build ./...`。
2. **预览版只发预览 tag**：tag 形如 `vX.Y.Z-preview.N`，release.yml 检测 `-preview` 自动 `--prerelease` 并跳过 GHCR；不进 CHANGELOG 正式版本区（记 Unreleased/预览段），不打 `-registry` tag、不进 MCP Registry。
3. **外部贡献吸收必须显式署名**（开源尊重，硬规则）：压缩聚合不能丢失作者归属——author 字段 + GitHub noreply 邮箱保证提交在 GitHub 上关联到贡献者账号与头像；相关 commit message 追加 `Credit: 来自 PR #N（作者 @handle）…` 标注来源；CHANGELOG（中英）与预览版 release notes 中必须致谢贡献者并列明其贡献范围。

## 当前状态与活跃待办（2026-11-27）

> 已完成任务的详细交接已归档到 `.handoff/archive/agents-md-handoff-snapshot-2026-11-16.md`（不入库）；各专题证据保留在 `.handoff/<topic>.md`。

### 当前版本与未发布改动

- 最新正式发布：`v3.11.0`（Release + GHCR + MCP Registry + npm 六包）；React 控制中心、外挂 UI / `no_ui`、PR #35 / #36 / #37、额度热路径优化与 go-webfetch v0.3.0 随该版本落地。
- `v3.10.9` 之后的改动按功能聚合：控制中心后端与 React 控制台（@MattLYT，PR #35 / #36）、混合检索并行与学术缓存修复（@MattLYT，PR #37）、cleanfetch 代码块保留、额度准入退出热路径、控制中心外挂 UI 与 `no_ui` 构建、README 亮色配图。
- 全新部署首次 `start` / `install` 会生成默认启用的 `dashboard.yaml`；已有配置不迁移。`dashboard.enabled:false` 跳过控制中心后端，`dashboard.ui.enabled:false` 只保留 API，`-tags no_ui` 从二进制移除前端资产。

### 待验证

- [ ] macOS `libpdfium.dylib` + purego smoke（Linux `.so` 已在 WSL 验证）
- [ ] 真实 PDF 混合负载长跑：稳定性与内存
- [ ] PDFium 独立 worker 隔离 C 层崩溃（当前为进程内实验性接入）
- [ ] TinyFish / Keenable 二进制真网链路长期回归（限流阈值、结果质量）
- [x] 控制中心中文六页真实浏览器走查（2026-11-27，桌面 + 390px 移动视口，无横向溢出）
- [ ] 控制中心英文六页真实浏览器走查
- [ ] i18n 工具返回内容在 `language: en` 下真机走查（smartsearch / cleanfetch / pdf_parser / file_search / search_history）
- [ ] 新引擎/新工具真实链路长期回归：验证码页频率、AnySearch 匿名档阈值、Google News 回源失败率、search_history 写入与 FTS 命中率
- [ ] 平台整包真机走查剩余部分：按包内 README 启动，安装 `skills/websearch-mcp` 并确认工具清单与项目标签
- [ ] `log_level` 与失败留痕真机复验：`error` 下无 info/debug 落盘，并发失败有 `search_failed` / `smartsearch_failed`

### 候选 / 未开工

- PDFium 加载路线对比（进程内 / 独立 worker / 启动预加载 / 短生命周期 / 纯 Go 替代），按首解与热解延迟、内存、畸形存活率、部署复杂度出结论；见 `docs/optimization-roadmap.md` P1。
- 供应商传输层错误统一包 `KeyError` → 30 分钟冷却：单 Key 供应商一次网络瞬断即不可用（系统性问题）。
- smartsearch 统一检索 `sources`（web/学术/file/wiki 分节聚合）已评审，推荐 Phase 1 = web + academic。
- 搜索质量闭环暂缓项：D4 citation/untrusted 边界、D6 垂直源、D7 配置分层、D8 tracing 与显式 DI；D3 剩余 hybrid 包装 goroutine 与 cleanfetch 统一预算。
- 上游限流导致的并发失败入口侧治理；缓存空闲连接参数（`ConnMaxIdleTime` / `MaxIdleConns`）复核。

### 运维状态

本机与远程部署的路径、主机名、端口和备份文件名只保留在本地交接，不入库。

### 发布红线

- tag 必须 annotated（Release 正文取 tag message）；预览版仅 `vX.Y.Z-preview.N`，不进正式 CHANGELOG / GHCR / MCP Registry。
- `-registry` 独立 tag、钉同一 commit、不与版本 tag 同推；同版本不可复用，duplicate 走幂等分支并显式 warning；回收流程 base + registry 双删后重打。
- `-npm` 独立 tag，六包按平台包 → CLI → dsh 顺序发；OIDC trusted publishing，勿改回 `workflow_call`。
- 含中文断言的测试包加 `zz_lang_pin_test.go`；改 i18n 后必须用 `LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 go test -short ./...` 复验。

### 已完成议题索引

| 议题 | 主要证据 / 交接 |
|---|---|
| PDFium 后端 / 本地 PDF | `.handoff/pdf-parser-backends.md`、`docs/pdf-parser.md`、`bench/pdfcorpus/` |
| i18n 四面治理 | `.handoff/i18n-governance.md` |
| 控制中心与外部 PR（#35/#36/#37） | CHANGELOG v3.11.0；`docs/dashboard.md`、`docs/dashboard-api.md` |
| 外部 PR 吸收（#30/#31、#33） | `.handoff/eco-absorb-providers.md`；#33 见 CHANGELOG v3.11.0 |
| GitHub 引擎 / Reddit 调研 | `.handoff/github-reddit-access.md`、`docs/optimization-roadmap.md` N7 |
| 控制中心快捷方式 | `.handoff/shortcut-sync.md` |
| 本机环境 / grok CLI | `.handoff/local-env-and-grok-cli.md`（v3.10.2 时期，部署现状以上方运维待办为准） |
| 历史发布 / 回收 / 整包 | `.handoff/archive/agents-md-handoff-snapshot-2026-11-16.md` |
