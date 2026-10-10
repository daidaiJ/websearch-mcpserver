# AGENTS.md — 智能体协作指南

> 本文件帮助 AI 智能体快速理解项目结构和开发约定，确保长期维护与持续开发的一致性。

---

> 包接口、工具调用链与改动落点详见 [docs/developers.md](docs/developers.md)。

---

## 项目一句话定位

**websearch-mcpserver** 是一个用 Go 编写的轻量级 MCP 搜索服务，零 API Key 即可运行，支持 Claude Code / Qwen Code / Cursor 等 MCP 客户端。提供四大核心能力：

| 能力 | 说明 |
|------|------|
| **搜索引擎搜索** | 内置百度/Bing/DuckDuckGo/Google 多引擎并发编排，支持 Tavily/Exa/AnySearch/豆包等 API 引擎混合 |
| **API 搜索** | API Key 池轮转模式，支持百度千帆/Tavily/Exa/AnySearch/豆包等多种 API 供应商，失败自动切换 |
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
2. **写操作安全模型（不可退让）**：所有写端点（设置/密钥/重启/清缓存/额度重置修正）= 仅 loopback + `X-Admin-Password` 头，服务端常量时间比较；口令只能配在 `dashboard.yaml`（`admin_password` / `admin_password_sha256`），**永远不出现在设置页白名单和 secrets 覆盖文件**——WebUI 无法读取或修改它。未配置口令 = 写端点整体禁用。
3. **配置分层与回退**：控制中心专属配置（口令/网段/额度/品牌）住独立 `dashboard.yaml`（`config.Load` 的 overlay，`WEBSEARCH_DASHBOARD_CONFIG` 可改路径），按字段覆盖主配置；主 `config.yaml` 零改动即可升级，删文件即回退。新键一律"零值 = 安全默认"，不允许要求老配置迁移脚本。
4. **隐私出网红线**：遥测只存脱敏元数据（查询哈希/主题/语言/关键词），错误文本服务端正则脱敏后才落库；密钥值任何接口永不回显（服务端不出网，前端掩码不算数）。新增端点时先过一遍"响应体会不会带密钥/原文"。
5. **品牌可定制**：`dashboard.brand`（title/logo/theme/accent）走 overview/settings 响应注入；主题 = `green/blue/mono` 三预设（CSS 变量块）+ accent 覆盖，本浏览器选择存 localStorage。桌面快捷方式图标跟主题（`cmd/assets/app-{theme}.ico`），启动时轻量检查（marker 文件字符串比较）主题变化才重建。
6. **配置文档唯一入口**：控制中心所有可配置项（含 `dashboard.shortcut` 快捷方式落位、`brand.footer` 关于块）的完整参考与「agent 向人类确认清单」住 `docs/dashboard.md` / `dashboard.en.md`；新增配置键必须同步该文档与 `dashboard.example.yaml`，不允许只在代码注释里出现。
6. **图标资产再生成**：改设计 → `go run ./tools/genicon`（在仓库根目录跑），产出 3 主题 ico + web logo，不要手工改二进制资产。

## 本地环境

- **Go 构建临时目录**：`GOTMPDIR=D:	mp`（`go env -w` + 用户环境变量已配置）。卡巴斯基锁定系统 TEMP 会导致 `go build` 偶发 Access denied，构建/测试编译临时目录已统一指向 `D:	mp`，在该目录配置杀软排除即可规避；`go test` 的 `t.TempDir()` 数据目录仍走系统 TEMP（改全局 TMP/TEMP 影响面大，未动）。

## 提交与发布卫生

1. **推送前必须按功能 rebase 压缩**（硬规则）：分支历史按功能聚合成少量提交（如 PR 引入层 / 安全加固层 / 功能层 / 文档层各一个），不推"一堆 WIP 碎提交"。吸收外部 PR 时保留原作者（`git commit --author`），功能分组用 `git checkout <src> -- <paths>` 按路径重建，每步过 `go build ./...`。
2. **webui 分支只发预览版**：tag 形如 `vX.Y.Z-preview.N`，release.yml 检测 `-preview` 自动 `--prerelease` 并跳过 GHCR；不进 CHANGELOG 正式版本区（记 Unreleased/预览段），不打 `-registry` tag、不进 MCP Registry。
3. **外部贡献吸收必须显式署名**（开源尊重，硬规则）：压缩聚合不能丢失作者归属——author 字段 + GitHub noreply 邮箱保证提交在 GitHub 上关联到贡献者账号与头像；相关 commit message 追加 `Credit: 来自 PR #N（作者 @handle）…` 标注来源；CHANGELOG（中英）与预览版 release notes 中必须致谢贡献者并列明其贡献范围。

## 🔄 Handoff 摘要

### search-upgrade — done（v3.6.2 已发布，2026-10-06）

- **当前状态：** 按五期拆分推进（用户确认）；**一期已提交 `6aa7ca8`**（P0-1 失败透出统一契约 + P1-6 共识按 family 计票 + P1-7 off-topic 整桶守卫 + P1-8 freshness 缓存 TTL）；**二期已提交 `f6da753`**（P1-4 date_source 三态注记 + P1-5 熔断落盘 + P1-10 Resource 化）；**三期已提交 `9b16ef7`**（P0-2 零 Key 来源扩充：so360 直连可用 / anysearch 匿名档 / wikipedia+googlenews 需代理，逐个出口实测后注册且结论补能力边界表；超限落盘 `smartsearch.inline_max_chars`；P1-7 处置=三态 `off_topic_guard` 默认 shadow（用户拍板）；小修：学术日期 FormatDateSource 注记 + MCP 握手版本对齐；BuildEngineMode/BuildHybridMode 改用 EngineAdapters 命名结构体）；**四期已取消**；**五期已提交（P0-3 插件市场分发经评估后取消——2026-10-06 用户拍板"不折腾插件，等 agent 生态原生支持 mcpb/Registry"，调研结论留档 .handoff；五期落地 = 全量文档同步：docs/search 中英新增「响应行为与可靠性」+ 引擎对照补 so360/wikipedia/google_news + docs/api 响应契约摘要 + README 响应透明行 + 四个 MCP 工具描述补适用边界路由提示）**，已合入 master 并发 v3.6.2（提交按期聚合，一至五期各一个提交）
- **关键证据：** 计划全文 `docs/plans/2026-10-search-upgrade.md`；一期契约层在 `pkg/search/core/diagnostics.go`
- **纪律（用户 mid-turn 明示）：** 遵循项目既有设计风格与原则；提交前性能审查（避免负面性能影响的实现）；每期完成即交接
- **详情指针：** [.handoff/search-upgrade.md](.handoff/search-upgrade.md)

### file-search — done（v3.7.0 已发布，2026-10-10）

- **已发布：** `feat/file-search-everything` 合入 master（merge `4f33610`）→ `v3.7.0`（GitHub Release 四平台二进制 + GHCR `3.7.0`/`3.7`/`latest`）→ `v3.7.0-registry`（MCP Registry 已收录 `io.github.daidaiJ/websearch-mcpserver 3.7.0`，4 个 mcpb）
- **本次改动：** `file_search` 单页返回默认 10、硬上限 20（`everything.max_results` 改默认值，超限收敛并在响应内说明），新增 `page` 分页（过滤后连续切片，候选池触顶有明确提示）；真机集成测试在 `mcp/tool_filesearch_live_test.go`（`WS_EVERYTHING_URL` 门控，未设置即整包跳过，不走 `internal/testenv`）
- **待办：** 本机部署 `D:\Programs\websearch` 仍跑旧二进制，且 config.yaml 里显式的 `everything.max_results: 50` 会被新硬上限收敛成 20（要默认 10 需删该行或改 10）；registry 缺 `v3.6.1` / `v3.6.2`（历次没打 `-registry` tag），如需补发在自己的原 commit 上补 tag

### 未验证事项
- [x] 候选零 Key 引擎在国内出口的实际可用性 —— 三期已实测（so360 直连✓ / anysearch 匿名档直连✓ / wikipedia+googlenews 代理✓，结论见能力边界表）
- [x] P1-7 off-topic 守卫处置（2026-10-06 用户拍板：三态 `smartsearch.off_topic_guard`，默认 shadow 只记录不丢弃）；守卫阈值仍未校准，shadow 日志/失败清单为后续校准积累证据
- [x] 超限落盘小项 + 学术日期注记 + 握手版本对齐 —— 三期已完成
- [x] 五期：全量文档同步（docs/search/api/README 补一至三期响应行为说明 + 工具描述适用边界）—— 已完成；P0-3 插件市场分发经评估取消（结论见 .handoff）
- [ ] 三期新引擎真实链路长期回归（验证码页出现频率、anysearch 匿名档限流阈值实测校准、googlenews 回源失败率）
- [ ] 二期冷却落盘真网冷启动回归（重启后 ddg/arxiv 实际避让）；date_source 新旧行缓存往返实测
