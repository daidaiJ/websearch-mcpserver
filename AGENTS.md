# AGENTS.md — 智能体协作指南

> 本文件帮助 AI 智能体快速理解项目结构和开发约定，确保长期维护与持续开发的一致性。

---

> 包接口、工具调用链与改动落点详见 [docs/developers.md](docs/developers.md)。

---

## 项目一句话定位

**websearch-mcpserver** 是一个用 Go 编写的轻量级 MCP 搜索服务，零 API Key 即可运行，支持 Claude Code / Qwen Code / Cursor 等 MCP 客户端。提供四大核心能力：

| 能力 | 说明 |
|------|------|
| **搜索引擎搜索** | 内置百度/Bing/DuckDuckGo/Google 多引擎并发编排，支持 Tavily/Exa/AnySearch/豆包/元宝等 API 引擎混合 |
| **API 搜索** | API Key 池轮转模式，支持百度千帆/Tavily/Exa/AnySearch/豆包/元宝等多种 API 供应商，失败自动切换 |
| **学术搜索** | arXiv/Crossref/OpenAlex/PubMed/Semantic Scholar/Google Scholar/Europe PMC/DBLP/DOAJ 九大学术引擎并发，DOI 跨引擎去重、逐引擎错误透传 |
| **网页抓取** | 增强型网页内容提取（TLS 指纹伪装 + SSRF 防护 + Jina Reader 备选） |
| **PDF 解析** | MinerU AI 增强 PDF 解析（表格/公式/多栏/图片智能识别），无 Token 自动降级 |

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
searxng/          # SearXNG 兼容 HTTP 端点

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
│   └── mineru/         # MinerU PDF 解析
├── llm/          # LLM：Client + 搜索摘要 Summarizer
├── config/       # 配置加载与结构体定义
├── cache/        # SQLite 缓存（6h 过期，后台清理）
├── client/       # HTTP 客户端（API 供应商共用）
├── proxy/        # 系统代理自动检测（Windows 注册表 / 环境变量）
├── daemon/       # 引用计数进程管理
└── log/          # 日志配置
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

- 用户可见文案一律走 `pkg/i18n` 的符号化 key，**不在调用点写裸字符串**：日志 `log.InfoT("log.<域>.<事件>", "name", v)`、Go 侧 `i18n.T` / `i18n.TCtx`、MCP 参数说明 `jsonschema:"mcp.param.<工具>.<字段>"`、前端 `data-i18n` 或 `t('web.<区域>.<名字>')`
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
| 百度 tn=json 接口（SearXNG baidu.py 路线） | 2026-09-03 实测：直连裸客户端 3/3 被 302 至 wappass 验证码，预热 cookie 无效；同 IP 下 HTML 引擎同样被 CAPTCHA。识别主因疑似 IP 信誉 + TLS 指纹，与 HTML/JSON 入口无关 | tn=json 非免检通道，JSON 接口不能替代 pkg/engine/baidu 现有反检测层 |
| DDG / arXiv 限流 | 服务端窗口限流，引擎内置钳制（DDG 1/s·6/min，arXiv 1/s·12/min + 3s 间隔）+ 429 冷却避让 + 预算感知重试 | 调整限流参数须实测校准，勿放宽内置上限 |
| 360 搜索（so360，2026-10-06 出口实测） | 直连与代理均 HTTP 200 + 自然结果（`li.res-list` + `data-mdurl` 真实 URL，`div.g-mohe` 特型卡需过滤）；解析契约对齐上游 so360.py | 已注册（`so360.enabled`，默认关闭）；验证码页 wappass 按反爬拦截报错 |
| AnySearch 匿名档（2026-10-06 出口实测） | 直连与代理均 200，POST 不带 Authorization 即匿名 IP 限流档（上游 anysearch.py 注释同口径），结果在 `data.results` | KeyPool 为空自动走匿名档，本端保守钳制 1/s·20/min；上游未公布阈值，调参须实测 |
| Wikipedia / Google News（2026-10-06 出口实测） | 直连均超时；代理下 Wikipedia MediaWiki API 200 + 正常 JSON，Google News RSS 200 + 52 item 且 batchexecute RPC 可回源发布方 URL（f.req 须三层嵌套 + ts 数字形态，否则 400） | 已注册（均需代理，无可用代理解析时跳过）；Google News 解析契约对齐上游 gnews.py |
| 腾讯云元宝搜索（2026-10-24 接入） | API KEY 方式 `POST api.wsa.cloud.tencent.com/SearchPro`，Bearer sk-...，返回 `Response.Pages[]`（JSON 字符串）；每月免费 2000 次，超出按量后付费（约 20 元/千次） | 已注册（`yuanbao.*`，默认不加入 apipool；hybrid 有 Key 时参与）；`yuanbao.monthly_limit` 默认 2000 并在请求前落盘拦截，禁止默认放开 |
| TinyFish / Keenable 供应商（2026-10-24 接入） | TinyFish 仅 Key 档（免费搜索 12000 次/天、fetch 1000 URL/天，00:00 UTC 重置，国内直连实测无需代理）；Keenable 未配 Key 自动走 public 免 Key 端点（`X-Keenable-Title` 标识，按出口 IP 限流） | 均已注册（`tinyfish.*` / `keenable.*`）；两者默认不进 `apipool.engines`，hybrid 中 TinyFish 有 Key 即参与、Keenable 的 public 档需显式 `enabled: true` 才参与（质量待观察）；fetch 能力并入 cleanfetch 远端抓取链（沿用 `mcp_hub.fetch_mode`） |
| `search_history` 本地历史（2026-10-24 接入，试验性） | smartsearch 成功后落本地 SQLite（FTS5），`search_history` MCP 工具全文检索；数据不出网，默认保留 30 天 | `search_history.enabled` 零值 = 不建库、不注册工具；实用性待真机确认，确认有价值后再转正式工具 |
| 国际化（i18n，2026-10-25 治理） | `language: auto|zh|en` 覆盖日志 / MCP 工具描述与参数 Schema / 控制中心三面；文案住 `pkg/i18n/locales/<域>.<语言>.json`，go-i18n + x/text 运行时，审计测试 `pkg/i18n/catalog_test.go` 双向比对代码引用与目录条目 | 新增文案只动 JSON + 用 key，勿在调用点写裸字符串；MCP 描述注册时固化、改语言需重启；HTTP 面按 `Accept-Language` 每请求协商 |
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
4. **学术搜索与通用搜索是独立模块**，学术引擎在 `pkg/academic/`，通用引擎在 `pkg/engine/baidu/` `pkg/engine/bing/` 等，不要混淆
5. **发布矩阵拆分，不要合成一套 6 平台**：GitHub Release = linux/windows amd64 + darwin amd64/arm64；GHCR = linux/amd64+arm64；MCP Registry mcpb 走 `vX.Y.Z-registry` tag 的**独立 Release 页**（`--expect-packages 4`，server.json 下载链接指向该页，base Release 只放二进制，两类产物分开）。**先打普通 tag（`vX.Y.Z`）发 Release 并推 GHCR 镜像**；**发完、Release 产物就绪后再单独打 `-registry` 后缀 tag 发 MCP Registry**（不推镜像，不要和版本 tag 一起推）。后补的 `vX.Y.Z-registry` 钉同一 commit（重跑工作流时需钉到含新 workflow 的 commit——打包原料从 base Release 下载，不重编译）。linux-arm64 走 GHCR，不要把 linux-arm64 / windows-arm64 加回 Release 来对齐 Docker 或旧版 v3.1.1 MCP
6. **README 顶部配图有渲染源**：`docs/images/src/`（HTML + Playwright 脚本，`python docs/images/src/render.py` 直接覆写 `docs/images/*.png`），改图改 HTML、不要手工编辑 PNG；源与用法见 `docs/images/src/README.md`

---

## WebUI（控制中心）规范

> 控制中心 = `pkg/dashboard`（后端）+ `pkg/dashboard/web`（前端）+ `pkg/telemetry`（遥测）+ `pkg/quota`（额度），独立分支 `webui` 演进。

1. **前端零第三方依赖，原生二进制内嵌**：页面资源经 `go:embed`（`//go:embed web/*`）打进 exe，无 CDN、无外部请求、无框架。**htmx 评估结论（2026-09-19）：不引入**——当前交互量（筛选/刷新/弹窗/轮询）用原生 JS（`app.js` 单文件 + CSS 变量主题）完全覆盖，htmx 需要 SSR 片段接口配合，改造收益低于依赖与重构成本；若未来页面复杂度明显上升再评估。改前端时保持"零依赖 + go:embed"约束，新增资源放 `pkg/dashboard/web/`。
2. **写操作安全模型（不可退让）**：所有写端点（设置/密钥/重启/清缓存/额度重置修正）**默认仅 loopback** + `X-Admin-Password` 头，服务端常量时间比较。用户可在 `dashboard.yaml` 配置 `write_allowed_networks` 放行任意 CIDR/IP；空值或任一非法条目均回退 loopback。读白名单及转发头不授予写权限。口令只能配在 `dashboard.yaml`（`admin_password` / `admin_password_sha256`），**永远不出现在设置页白名单和 secrets 覆盖文件**——WebUI 无法读取或修改它。未配置口令 = 写端点整体禁用。
3. **配置分层与回退**：控制中心专属配置（口令/网段/额度/品牌）住独立 `dashboard.yaml`（`config.Load` 的 overlay，`WEBSEARCH_DASHBOARD_CONFIG` 可改路径），按字段覆盖主配置；主 `config.yaml` 零改动即可升级，删文件即回退。新键一律"零值 = 安全默认"，不允许要求老配置迁移脚本。
4. **隐私出网红线**：遥测只存脱敏元数据（查询哈希/主题/语言/关键词），错误文本服务端正则脱敏后才落库；密钥值任何接口永不回显（服务端不出网，前端掩码不算数）。新增端点时先过一遍"响应体会不会带密钥/原文"。
5. **品牌可定制**：`dashboard.brand`（title/logo/theme/accent）走 overview/settings 响应注入；主题 = `green/blue/mono` 三预设（CSS 变量块）+ accent 覆盖，本浏览器选择存 localStorage。桌面快捷方式图标跟主题（`cmd/assets/app-{theme}.ico`）；`start`/`open`/`install` 读取 `.lnk` 逐项校验目标、参数、工作目录与图标，主题或图标内容变化时落盘带内容哈希的新 ico 并重建。
6. **配置文档唯一入口**：控制中心所有可配置项（含 `dashboard.shortcut` 快捷方式落位、`brand.footer` 关于块）的完整参考与「agent 向人类确认清单」住 `docs/dashboard.md` / `dashboard.en.md`；新增配置键必须同步该文档与 `dashboard.example.yaml`，不允许只在代码注释里出现。
6. **图标资产再生成**：改设计 → `go run ./tools/genicon`（在仓库根目录跑），产出 3 主题 ico + web logo，不要手工改二进制资产。

## 本地环境

- **Go 构建临时目录**：`GOTMPDIR=D:\tmp`（`go env -w` + 用户环境变量已配置）。卡巴斯基锁定系统 TEMP 会导致 `go build` 偶发 Access denied，构建/测试编译临时目录已统一指向 `D:\tmp`，在该目录配置杀软排除即可规避；`go test` 的 `t.TempDir()` 数据目录仍走系统 TEMP（改全局 TMP/TEMP 影响面大，未动）。
- **Go 源码行尾已统一 LF（2026-10-25，`f342a4d`）**：`.gitattributes` 钉死 `*.go text eol=lf`，工作树已全量转 LF、gofmt 历史格式债已清——此后 `gofmt -l` 报告皆为真实问题，直接修；勿删 `.gitattributes`，否则 autocrlf 检出 CRLF 会让 gofmt 重新全文件误报。

## 提交与发布卫生

1. **推送前必须按功能 rebase 压缩**（硬规则）：分支历史按功能聚合成少量提交（如 PR 引入层 / 安全加固层 / 功能层 / 文档层各一个），不推"一堆 WIP 碎提交"。吸收外部 PR 时保留原作者（`git commit --author`），功能分组用 `git checkout <src> -- <paths>` 按路径重建，每步过 `go build ./...`。
2. **webui 分支只发预览版**：tag 形如 `vX.Y.Z-preview.N`，release.yml 检测 `-preview` 自动 `--prerelease` 并跳过 GHCR；不进 CHANGELOG 正式版本区（记 Unreleased/预览段），不打 `-registry` tag、不进 MCP Registry。
3. **外部贡献吸收必须显式署名**（开源尊重，硬规则）：压缩聚合不能丢失作者归属——author 字段 + GitHub noreply 邮箱保证提交在 GitHub 上关联到贡献者账号与头像；相关 commit message 追加 `Credit: 来自 PR #N（作者 @handle）…` 标注来源；CHANGELOG（中英）与预览版 release notes 中必须致谢贡献者并列明其贡献范围。

## 🔄 Handoff 摘要

> 已完成任务的交接详情已归档至 `.handoff/archive/`（不入库）。此处只保留当前活跃任务、运维待办与待验证项。

### i18n 治理（日志/MCP 描述/控制中心 语言模式 auto|zh|en）— done（2026-10-25，待发布）

- **当前状态：** 三面（日志 / MCP 描述与参数 Schema / 控制中心）zh-en 双档已分三个提交落地（`d5f7780` 基础设施+日志+MCP、`7b1c246` 控制中心、`8639284` 文档），未打 tag、未推远端；`language: auto|zh|en` 已进配置与样例
- **实现要点：** `pkg/i18n` 基于 `nicksnyder/go-i18n/v2` + `golang.org/x/text/language`（原设计是 zh 当 key 的 Go map，评审后改为符号化 key + 外置 JSON）；文案住 `pkg/i18n/locales/<域>.<语言>.json`（log/mcp/dashboard/web）；调用形态 `log.*T(key, "name", v)` 与 `i18n.T`/`i18n.TCtx`；MCP 参数说明改为 `jsonschema:"mcp.param.*"` 并在注册时渲染输入 Schema（顺带修掉 `description,` 前缀透出给客户端的既有缺陷）；控制中心按 `Accept-Language` 每请求协商，`serveIndex` 改写 `<html lang>` 并把前端文案内联进页面（首帧即正确语言）
- **防漏机制：** `pkg/i18n/catalog_test.go` 强制每个域 zh/en key 集合一致、同一 key 占位符集合一致、代码引用与目录条目双向一致；纯转发包装（key 由调用方给出）须显式登记 `dynamicKeyAllowlist` 并写理由
- **验收：** `go build ./...`、`go vet ./...`、`go test -short ./...`、`go test ./pkg/i18n/`（目录审计）全绿；Windows 真机系统语言探测实测命中 zh-CN
- **遗留（第四面，未纳入本批）：** 工具**返回内容**仍是中文硬编码——搜索结果 Markdown、工具错误文案、PDF/MinerU 进度提示等；机制已就绪（登记 key 即可），下一批评估是否治理
- **详情指针：** [`.handoff/i18n-governance.md`](.handoff/i18n-governance.md)

### v3.10.1 发布后审查修复 — done（2026-10-25，v3.10.0 三线下架回收）

- **当前状态：** v3.10.0 发布次日审查发现 2 P1 + 7 P2，修复后以 `v3.10.1` 重发（Release + GHCR + MCP Registry 三线就绪）；v3.10.0 的 GitHub Release×2、远端/本地 tags、MCP Registry 条目（status=deleted）均已下架回收
- **修复清单：** fetch_top_n 接 sitefetch GitHub 特判（JS 空壳误标反爬）、sitefetch 403/429 配额冷却（Retry-After / x-ratelimit-reset，封顶 10 分钟）、blob 路径逐段 PathEscape + www 前缀归一、pushed_at 越界守卫、GitHub 引擎引号感知截断（422 防护）、search_history 缓存命中补写、quota 双端点并行、历史条目 query 展示截断；提交 `c2e5a20`（fix）+ `548a6f9`（changelog），新增 7 个针对性测试
- **审查遗留（未做，候选 roadmap）：** 全供应商把传输层错误也包 `KeyError` → `MarkInvalid` 30 分钟冷却，单 Key 供应商一次网络瞬断即 30 分钟不可用（tavily/exa/doubao 等老约定同款，属系统性问题）；smartsearch 统一检索（sources 参数：web/学术/file/wiki 分节聚合）方案已评审，推荐 Phase 1 = web+academic
- **验收：** `go build`、`go vet`、全量 short 测试（42 包）、`golangci-lint --new-from-rev=v3.10.0` 0 issues

### eco-absorb 供应商扩展批 — done（已随 v3.10.0 发布，后由 v3.10.1 修正接替）

- **当前状态：** 已随 `v3.10.0` 正式发布（2026-10-24，Release + GHCR + MCP Registry 三线就绪）；发版前经两轴审查修复 4 处（LLM withDeadline 取更早者、search_history FTS5 特殊字符、history 优雅关闭、工具描述补试验性）
- **关键证据：** 分支远端 head `3d78aa1`（本地 worktree 已删除）；TinyFish `/usage` 真机实测（total 随 start_after 过滤）；测试 Key 在 gitignored 的 `config.test.yaml` 与部署 `config.yaml`
- **合入验证（2026-10-24）：** 21 个冲突文件逐个手工合并（CHANGELOG 把 Unreleased 留在 v3.9.1 之上、config/doc 列表同时容纳 yuanbao 与 tinyfish/keenable、`BuildApipoolMode`/`BuildHybridMode` 增补 pool 形参并同步调用点）；`go build ./...`、`go vet ./...`、`go test ./...` 全绿（39 包），`golangci-lint --new-from-rev` 修掉 errorlint / staticcheck 共 4 处后仅剩 CRLF 引起的 gofmt 误报
- **文档同步：** CHANGELOG 中英 v3.10.0、roadmap N6 归档、dashboard/configuration/search 文档中英同步；合入时另补了跨批次一致性：源开关与默认值速查表、`apipool.engines`/`weights` 列表、README 引擎清单同时列上 yuanbao 与 tinyfish/keenable（TinyFish/Keenable 无独立 mode，不进模式表）
- **详情指针：** [`.handoff/eco-absorb-providers.md`](.handoff/eco-absorb-providers.md)
- **路线图权威：** [docs/optimization-roadmap.md](docs/optimization-roadmap.md) N 系列（N1-N6 已归档；智谱 preset 评审后不做——消耗套餐额度）

### shortcut-sync — done（随 v3.9.1 发布）

- **当前状态：** 已合入 master（`7c854c8`）并随 `v3.9.1` 发布；本地部署已升级，桌面快捷方式启动时自动刷新为内容哈希图标
- **关键证据：** `syncDashboardShortcut` 统一 `start`/`open`/`install`；逐项校验 `.lnk` 目标/参数/工作目录/图标路径与索引；`off`/控制台关闭时删除已知位置；真机覆盖坏链、篡改、exe 重命名、中文路径
- **验收：** `go test ./...`、`go test -race -short -p 1 ./...`、golangci-lint 0 issues
- **详情指针：** [`.handoff/shortcut-sync.md`](.handoff/shortcut-sync.md)

### github-reddit-access — done（GitHub 已随 v3.10.0 落地、v3.10.1 修正；Reddit 整线放弃，N7 归档）

- **当前状态：** GitHub 引擎 + cleanfetch 仓库/blob 特判 + 失败回退已随 `v3.10.0` 发布（v3.10.0 已下架，现行版本 `v3.10.1`）；Reddit 经评审整线放弃（官方公告进入关停周期，收益期 < 5 个月），调研结论留档 roadmap N7
- **验收结果：** GitHub 零 Key 匿名 + PAT 双模、429/403 冷却（Retry-After / x-ratelimit-reset）、blob 走 contents API、搜索失败回 Bing `site:github.com`、抓取失败回 webfetch→Jina、typed-nil 防护（InitGithubEngine 返回接口类型）——真机全部验证通过
- **详情指针：** [`.handoff/github-reddit-access.md`](.handoff/github-reddit-access.md)

### 运维待办

- 本机部署 `D:\Programs\websearch` 仍为已下架的 `v3.10.0` 构建（备份 `websearch-mcpserver.exe.bak-pre-v3.10.0`），待升级 `v3.10.1`（下载 base Release 的 `websearch-mcpserver-windows-amd64.exe` 覆盖）；`everything.max_results: 10`；`cleanfetch.auto_proxy: true`；新供应商已配测试 Key（tinyfish/keenable 进 `apipool.engines`，github PAT 提额，search_history 已开启），`mcp_hub.fetch_mode: prefer` 激活远端抓取链
- **v3.10.0 下架记录（2026-10-25）：** GitHub Release `v3.10.0` / `v3.10.0-registry` 已删、远端与本地 tags 已删、MCP Registry 经 `registry-status.yml` 置 `deleted`（工作流日志复核：列表余 3.10.1 无 3.10.0）。GHCR 的 `v3.10.0` 镜像 tag 未删（历史版本镜像一律保留，与 3.9.x 同口径）
- **发布流程教训（2026-10-24）：** `-registry` tag 推送不可重复——MCP Registry 同版本二次 publish 报 400 duplicate（首次已成功发布即占位）。误发版本用 `mcp-publisher status --status deleted`（已建 `.github/workflows/registry-status.yml` 运维工作流，OIDC 认证）下架；重发同版本 tag 必须钉同一 commit（构建可复现，产物 sha256 与 registry 元数据逐一致时可不重 publish）。registry 约束：`status active` 不允许携带 message
- **发布流程教训（2026-10-25）：** 发版 tag 必须用 annotated tag（`git tag -a vX.Y.Z -m "变更日志"`）——release.yml 的 Release 正文取自 tag message，轻量 tag 会得到空正文（v3.10.1 曾中招，后用 `gh release edit --notes-file` 补救）。版本回收重编时，CHANGELOG 与 Release 日志把被回收版本的内容与修复合并为一个条目交付，不保留已下架版本的独立条目
- 误发的 `v3.10.0-preview.1` 远端与本地 tag 已删除。MCP Registry 缺 `v3.6.1`/`v3.6.2` 的 `-registry` tag。

### 待验证

- [ ] tinyfish/keenable 经二进制真网链路长期回归（限流阈值、结果质量；当前仅有 curl 实测 + 单测）
- [ ] 新引擎/新工具真实链路长期回归：验证码页频率、anysearch 匿名档阈值、googlenews 回源失败率、search_history 写入与 FTS 命中率
