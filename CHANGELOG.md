# Changelog

[English](CHANGELOG.en.md) | [中文](CHANGELOG.md)

## v3.10.3 — 2026-10-31（托管 MCP 抓取：免 Key 端点真正参与 fetch）

> 让托管 MCP 抓取真正成为 cleanfetch 的优化来源：`f835d60`（上游响应头兜底超时放宽到 60s）、`f935776`（代理由准入条件改为可达性优化）、`4886046`（远端抓取链托管 MCP 优先）、`e335777` / `0315e06`（search / configuration / roadmap 文档与配置样例同步）。

### 变更
- **托管 MCP 的代理从「准入条件」改为「可达性优化」**：`exa`、`firecrawl` 预设默认「有代理则走代理、无代理直连」，不再因为未检测到代理被整轮跳过（`parallel`、`anysearch` 恒直连）；`requires_proxy` 保留为显式覆盖——`true` = 硬性要求代理、无代理则跳过，`false` = 强制直连。两轮出口实测（2026-10-20、2026-10-31）四个端点均可直连，硬性跳过只会让免 Key 托管抓取白白缺席
- **cleanfetch 远端抓取链改为托管 MCP 优先**：`fetch_mode` 非 off 时远端链顺序由「供应商 fetch API（TinyFish/Keenable）→ mcphub 托管 MCP」反转为「mcphub 托管 MCP → 供应商 fetch API → 本地 webfetch/Jina」。`tryRemote` 取第一个非空结果即返回，而供应商 fetch API 只做纯 HTTP 抓取：实测 TinyFish 对纯客户端渲染的 `open.spotify.com` 返回 56 字符应用外壳，排在前面等于把会渲染的托管 MCP 挡在门外（parallel / anysearch / exa 对同一 URL 分别返回 8906 / 29553 / 2085 字符渲染正文）。注意托管 MCP 的 `exa` / `anysearch` 与 apipool / hybrid 的同名供应商指向同一上游服务的不同入口（前者多走匿名档、按出口 IP 限流，后者用配置的 Key），额度口径独立但共用出口 IP

### 修复
- **上游响应头兜底超时不再先于调用方预算打断请求**：`pkg/proxy` 默认 transport 的 `ResponseHeaderTimeout` 由 15s 放宽到 60s。该值此前低于每个调用方自带的预算（`pkg/client` 默认 30s、`mcp_hub.fetch_timeout_sec` 默认 45s、`mcp_hub.timeout_sec` 默认 20s），托管 MCP 抓取是服务端渲染、偶发十几秒才出响应头，被误判为超时并触发该 server 60s 冷却，整条远端抓取链随即降级到本地 webfetch，表现为「可能需要 JavaScript 渲染」的空正文或 `extract_failed`；放宽后由各调用方自身的超时决定失败时机，兜底值只作上限

## v3.10.2 — 2026-10-25（国际化治理：日志 / MCP 元数据 / 控制中心 / 工具返回内容 zh-en 双档）

> 四面 i18n 一次交付：`d5f7780`（基础设施 + 日志层 + MCP 描述）、`7b1c246`（控制中心后端与前端）、`d51a0ea`（配置样例与文档同步）、`0268707`（tool 域）、`2f18a0d`（fetch 域）、`7e16572`（provider 域）、`1fac4e9`（search 域）。
>
> 本版曾于同日先行发布过「三面」版本；因 i18n 治理这一需求还包含工具返回内容一面，回收同版本号重发，本条为完整交付范围。

### 新增
- **国际化（i18n）治理：日志 / MCP 元数据 / 控制中心 / 工具返回内容四面 zh-en 双档**：新增主配置 `language: auto|zh|en`（零值 `auto` 按系统语言适配：Windows 取用户默认区域，类 Unix 取 `LC_ALL` / `LC_MESSAGES` / `LANG`，匹配不到中英时回落 `en`；环境变量 `WEBCRAWLER_LANGUAGE`）。文案数据住 `pkg/i18n/locales/<域>.<语言>.json`（`log` / `mcp` / `dashboard` / `web` + 工具返回内容的 `tool` / `fetch` / `provider` / `search`，共八域），key 为符号化稳定标识，占位符用命名模板 `{{.Name}}`，复数走 CLDR 规则；新增语言 = 新增一组同域文件并把标签加入 `pkg/i18n/language.go` 的 `supported`，改文案只动 JSON
- **三种生命周期各取所需**：日志、MCP 工具描述/参数 Schema 与工具返回内容取进程默认语言（MCP 描述在注册时固化，改语言需重启）；控制中心页面与 `/__admin/api/*` 响应按浏览器 `Accept-Language` 每请求协商，协商不出回退配置解析结果。控制台首帧即正确语言：服务端改写 `<html lang>` 并把当前语言的前端文案内联进页面，无切换闪烁
- **文案目录审计（防漏机制）**：`pkg/i18n/catalog_test.go` 强制每个域在 zh/en 下 key 集合一致、同一 key 的 `{{.占位符}}` 集合一致、代码引用的 key 与目录条目双向一致（无漏登记、无死条目）；`log.*T`、`i18n.T` / `i18n.TCtx`、`jsonschema` 标签与前端 `web.*` 引用四类落地形态都在扫描范围内，纯转发包装须显式登记理由

### 变更
- **日志 API 改为符号化 key**：`log.Info/Infof/Warnf/Errf/Error/Debug/Debugf` 统一为 `log.InfoT/WarnT/ErrorT/DebugT(key, "name", value, …)`，日志文案不再内联在调用点；未登记 key 原样输出 key 本身（便于定位），漏登记由目录审计拦截
- **MCP 工具参数说明改为目录 key**：`jsonschema` 标签从内联中文文案改为 `mcp.param.*` key，注册时按当前语言渲染输入 Schema；顺带修掉标签前缀 `description,` 被当作描述文本透出给客户端的既有缺陷
- **工具返回内容接入 i18n（第四面，390 条 key）**：MCP 工具**返回内容**里由本服务自产的提示、告警、错误与进度文案迁到符号化 key——工具层参数校验与降级说明、反爬拦截提示、MinerU 分批进度与页码说明、落盘/续读提示、SSRF 与 DNS rebinding 预检拒绝；抓取层（webfetch / Jina / MinerU 含错误码映射表 / Everything / sitefetch）的失败与回退说明；供应商层（tavily / exa / anysearch / baidu / baidu_ai / doubao / yuanbao / tinyfish / keenable / mcphub）的调用错误与配置校验；编排层（hybrid / adapter / apipool / engine）的失败清单、off-topic 守卫原因与限流冷却原因
- **工具返回内容的治理边界**：只治理**本服务自产**文案；**网页正文、API 供应商返回的结果文本、上游库自带的中文串不治理**。结果记录骨架（`pkg/search/core/format_md.go` 的 `## 结果` / `**标题**` / `**url**` / `**内容**` / 日期注记 / `usage_note` 固定行）由 `contract_test.go` 固化，保持原样；`BuildUserPrompt` 的 LLM 提示词、`enhance` 的停用词与意图关键词表、`telemetry` / `antirobot` 的判类关键词表同属算法输入，一律不动

### 修复
- **MCP 参数描述不再带 `description,` 前缀**：此前 `jsonschema:"description,…"` 会把 `description,` 一起当作描述文本发给客户端，工具列表里每条参数说明都带这个噪声前缀
- **翻译错误文案不再打断按关键词判类的逻辑**：`mcp/tool_search.go` 的 `isAntiBotFailure` 改为中英关键词并列（原先只认中文）；`pkg/search/core/diagnostics.go` 的 `ClassifyFailure` 与 `pkg/telemetry/errorkind.go` 的错误种类判定本就中英并列，核对后保持可用
- **稳定哨兵与文案分离**：`errResultUnavailable`、`errUnsafeRedirect`、`ErrPageLimit`、`ErrRemoteURLRejected`、`ErrUnavailable`、`ErrAuth` 保留 `errors.Is` 身份（改为 ASCII 标记 + 通用 wrapper），对外文案由调用点的 `i18n.T` 字面量 key 渲染，目录审计仍可静态校验

## v3.10.1 — 2026-10-25（供应商扩展、GitHub 引擎、search_history 与发布后审查修复）

> 调研吸收批次（路线图 N 系列）与发布后审查修复合并交付：`v3.10.0` 曾于 2026-10-24 短暂发布，因发布后审查发现 2 个 P1（`fetch_top_n` 未接 GitHub 站点特判、sitefetch 无配额冷却）与 7 个 P2 而全线回收（GitHub Release / tags / MCP Registry 均已下架），其全部内容与本批修复合并为 `v3.10.1` 一次发布。提交对照：`bb30253`（LLM deadline）、`a7045b3`（供应商扩展 + 远端抓取链）、`45104d7`（输出契约测试）、`7cf198e`（search_history）、`bf9053d`（dashboard 源面板与设置页）、`067fcf5`（TinyFish 官方额度直读）、`c2e5a20`（发布后审查修复）及后续 roadmap / 文档提交。

### 新增
- **GitHub 搜索引擎与 cleanfetch 站点特判**：REST API 引擎（零 Key 匿名档 search 10/min，配 PAT 提额 30/min，token 轮转去重 + 429/403 冷却，国内直连可达无需代理），`github.enabled` 默认关闭，参与 engine/hybrid/mcphub；API 失败（限流/403/零结果）自动回退 Bing `site:github.com` 检索；cleanfetch 对 `github.com/<owner>/<repo>` 优先走 API（元数据 + README raw），`blob` 页走 contents API 取源文件（`Accept: application/vnd.github.raw`，绕开国内不可达的 raw.githubusercontent CDN），失败回落常规 webfetch → Jina 链，与引擎共用 `github.api_key` / `token_list`（env `GITHUB_TOKEN`）
- **TinyFish / Keenable Search+Fetch 供应商**：双能力 API 供应商接入 apipool / hybrid 供应商池与源开关（`tinyfish.enabled` / `keenable.enabled`），fetch 能力接入 cleanfetch 远端抓取链（沿用 `mcp_hub.fetch_mode` 键治理整条远端抓取链，零破坏）。TinyFish 仅 Key 档（免费搜索 12000 次/天、fetch 1000 URL/天，00:00 UTC 重置，国内直连实测无需代理）；Keenable 无 Key 自动走 public 免 Key 端点（`X-Keenable-Title` 标识应用名，按出口 IP 限流），但**默认不自动参与 hybrid**（质量待观察）——需显式 `enabled: true` 或加入 `apipool.engines` opt-in
- **控制中心源面板与设置页同步**：源面板新增 TinyFish / Keenable 条目（Active 态、参与条件、别名归并）；设置页开放两个新开关并把 `apipool.engines` 白名单扩至新供应商；keenable 无 Key 时缺省开关按 false 处理，避免保存任意设置把 public 档静默写进配置
- **TinyFish 官方额度查询**：控制中心额度新增 TinyFish 官方 `/usage` 端点直读（api.search / api.fetch，X-API-Key，`total` 随 `start_after` 过滤返回），当日（00:00 UTC 起）搜索/抓取调用数无需翻页聚合；免费档上限按官方文档取值（搜索 12000/天、fetch 1000/天）；搜索主指标不受 fetch 查询失败影响，官方查询整体失败时回退本地用量
- **试验性 `search_history` 工具（默认关闭）**：smartsearch 成功后把 query + 结果摘要落本地 SQLite（FTS5），`search_history` MCP 工具支持全文检索「搜自己查过的东西」，数据不出网、保留期默认 30 天；`search_history.enabled` 零值 = 不建库、不注册工具。**试验性（idea）**：实用性尚未确认，体验确认有价值后再转正式工具

### 变更
- **LLM 调用有界 deadline**：新增 `llm.timeout_sec`（默认 60s），LLM 摘要不再无限等待慢上游；本端超时与调用方 ctx deadline 恒取更早者
- **MCP 输出契约测试**：`pkg/search/core/contract_test.go` 固化 MCP 工具输出契约（结果字段/格式），供应商扩展不得破坏既有输出结构
- **Reddit 接入评审后不做**：官方 2026-09-30 公告进入关停周期（新公共 API 申请 2026-10-31 截止、RSS 2026-11-13 停止支持、公共 Data API 2027-03 终止），OAuth script app 与 RSS 兜底两条候选通道均在死线上，收益期不足 5 个月，整线放弃；调研结论与重开条件留档 `docs/optimization-roadmap.md` N7

### 修复
- **smartsearch `fetch_top_n` 接入 GitHub 站点特判（P1）**：github.com 是 JS 渲染页，webfetch 抓不到正文，此前 `fetch_top_n` 对 GitHub 检索结果的正文补抓必然拿到空壳并误标"反爬拦截"。现与 cleanfetch 共用同一特判入口（REST API 优先、失败回落常规链、遥测同源），正文过统一大小截断
- **cleanfetch GitHub 特判增加配额冷却（P1）**：sitefetch 侧此前无限流无冷却，匿名 core 60/h（每个仓库页 2 次调用）耗尽后持续 403 空转；现 403/429 按 Retry-After / x-ratelimit-reset 进入冷却（封顶 10 分钟，与搜索引擎侧同口径），冷却中快速失败、立即回退 webfetch → Jina 链
- **GitHub blob 页路径编码**：owner / repo / 文件路径各段改 `PathEscape` 逐段编码（此前畸形段可产生非法请求路径）；`www.github.com` 前缀归一匹配特判，伪造后缀域（`github.com.evil.example`）不接管
- **畸形 `pushed_at` 不再 panic**：API 返回短于 10 字符的时间戳时裸切片越界会崩掉抓取链，现防御性截取、原样展示
- **GitHub 引擎超长查询引号感知截断**：截断到 5 子句时引号短语计为一个子句，不再切出不配对引号（GitHub search API 会以 422 拒绝整个查询）
- **`search_history` 缓存命中也写历史**：工具描述口径是「每次 smartsearch 成功后自动写入」，此前缓存命中路径漏写；现实时与命中路径一致（命中快照解析失败时跳过）
- **控制中心额度查询并行化**：Tavily 与 TinyFish 官方 `/usage` 端点改并行查询（各 12s 超时，串行最坏 24s 拖慢面板）
- **`search_history` 条目头部 query 截断**：超长 query（多为误粘贴）在展示层截断到 200 字符，库里仍存完整原文、FTS 检索不受影响
- **`search_history` FTS5 查询特殊字符兼容**：含引号/括号/星号/`AND`-`OR` 等操作符的检索词此前会触发 FTS5 语法错误，现按空白分词后引号包裹（隐式 AND），简单词组语义不变
- **`search_history` 库随服务优雅关闭**：此前关闭路径漏掉历史库（清理协程不停、WAL 不落盘），现与 cache/WebFetch 同一关闭序列
- **LLM 本端超时在调用方 deadline 更晚时生效**：`withDeadline` 此前在调用方带任意 deadline 时完全放弃本端超时，现修正为两者取更早者
- `search_history` 工具描述补「试验性」标注

## v3.9.1 — 2026-10-24（元宝搜索、抓取代理开关与快捷方式同步）

### 新增
- **腾讯云元宝搜索（`yuanbao`）**：新增 API KEY 方式接入（`POST https://api.wsa.cloud.tencent.com/SearchPro`），支持 `mode: yuanbao`、hybrid 并发与显式加入 `apipool.engines`；解析 `Response.Pages[]` 的标题/URL/摘要/发布日期/score，`time_range` 映射为 `dN`/`mN`/`yN`，支持 `site`/`industry`/`mode` 过滤、本地黑名单与 `sk_list` 多 Key 轮询
- **元宝月度调用硬上限**：`yuanbao.monthly_limit` 默认 2000（对齐每月免费额度），每次请求前本地计数并在达到上限后停止调用、回退其他来源；计数落盘 `data/yuanbao-quota.json`，重启后继续生效。`0` 显式关闭上限即接受后付费
- 控制中心加入元宝来源卡片与 `TENCENTCLOUD_WSA_APIKEY` 密钥状态；`apipool.weights.yuanbao` 默认 0，后付费源不参与加权起始
- **原生抓取独立代理开关**：`cleanfetch.auto_proxy`（默认 true）控制 cleanfetch/PDF 是否自动跟随全局代理，WebUI 增加“抓取自动走代理”开关；关闭后强制直连，不再受系统代理影响，与 API 供应商的 `proxy.api_providers` 解耦

### 修复
- **桌面快捷方式不随配置与程序路径更新**：`start` / `open` / `install` 统一以 `dashboard.enabled`、`dashboard.shortcut`、`brand.theme`/`brand.icon` 和当前 exe 路径为期望状态，逐个读取 `.lnk` 校验目标、参数、工作目录、图标路径与图标索引，缺失、坏链、手工改坏、exe 重命名/换目录时重写并输出 `created` / `updated` / `removed` / `unchanged`；`shortcut: off` 或控制台关闭时删除桌面与开始菜单的已知快捷方式（此前只跳过创建）；图标文件名带内容哈希（`websearch-<theme|custom>-<hash8>.ico`），主题或图标内容变化后能刷新 Windows 图标缓存

## v3.9.0 — 2026-10-22（零 Key MCP 级联与观测护栏）

> 本版包含路线图首批八项落地（`b780769`）、`feat/parallel-mcp` 分支合入的零 Key 托管 MCP 级联模式，以及吸收 [@MattLYT](https://github.com/MattLYT) 的 [PR #26](https://github.com/daidaiJ/websearch-mcpserver/pull/26)（Wikipedia 检索操作符剥离，merge `0d94295`）。

### 新增
- **零 Key 托管 MCP 级联模式 `mcphub`**：内置 Parallel / AnySearch / Exa / Firecrawl 四个 preset（请求参数与响应解析随 preset 固定），按 `priority` / `round-robin` 轮转，失败冷却后级联切换；每个上游可选 `api_key`/`sk_list`，`exa`/`firecrawl` 默认按代理可用性自动启用/跳过；`rotation_engines` 可把本地引擎加入主轮转分摊限流，`fallback_engines` 默认 `bing → so360`，全部失败仍由 smartsearch 回退 Bing。cleanfetch 另有 `fetch_mode: off/fallback/prefer` 可选复用同一批 MCP 抓取，PDF、SSRF/HEAD 预检与本地 webfetch/Jina 降级保持不变
- **`GET /metrics` 观测端点**：Prometheus/OpenMetrics 文本格式的只读聚合（今日工具/供应商请求数、成功失败、缓存命中、延迟分桶），零新依赖手写渲染，数据来自脱敏遥测库、不含 URL/原文/密钥；需 `dashboard.enabled: true`，未启用返回 503
- **off-topic 守卫校准回放**：`shadow` 判定落 `guard` 遥测事件（模式、参照回声率、各引擎桶回声率/条数），新增 `go run ./tools/guardreplay -db <dashboard.db>`，按 0.15/0.25/0.35/0.5 等候选阈值输出「若 enforce 会丢弃哪些桶、影响多少查询」与样例，`-json` 供机器消费

### 变更
- **测试场景分类补上游限流**：`internal/testenv` 把 429 / rate limit 归为场景类失败，DDG 限流测试与 CleanFetch 网络用例改走 `HandleSearchError`——环境限流或站点不可达时跳过，不再误报为回归

### 修复
- **mcphub 未启用降级引擎 panic**：`fallback_engines` 默认含 `so360`，而 `so360.enabled` 默认关闭；装配时跳过未启用/不可用的本地引擎，搜索不再因空适配器 panic
- **mcphub 单个 Key 鉴权失败连坐整个 server**：配置 `sk_list` 时，401/403/unauthorized/forbidden/invalid api key 只标记该 Key 无效（30 分钟）并换下一把，Key 池耗尽后才冷却；连接/超时错误直接冷却该 server 并级联，保留其余 Key
- **mcphub 搜索与 cleanfetch 各建一套 Hub**：`mode=mcphub` 且 `fetch_mode` 非 off 时，cleanfetch 复用搜索 Hub，冷却表与 Key 池共用；搜索/抓取仍分槽冷却
- **并发请求的失败诊断互相串扰**：编排层诊断改为随返回值传递（`core.DiagnosticsSearcher`），`SearchRaw` 旧签名保留单槽兜底；两请求并发时，失败请求只拿到自己的失败清单，成功请求不再被另一请求的引擎失败污染
- **跨引擎 URL 去重漏合**：新增 `core.NormalizeURL`（scheme/host 小写、去 fragment、剥离 `utm_*`/`fbclid`/`gclid`/`ref`/`spm`、查询参数排序、非根路径去尾斜杠），单引擎内与跨引擎两处去重键及 `antirobot.DeduplicateResults` 共用；同一页面的 scheme/尾斜杠/追踪参数变体不再各占一个 `global_max_size` 名额（展示 URL 保持原样）
- **SQLite 缓存读被写阻塞**：缓存库启用 WAL + `busy_timeout` + `synchronous=NORMAL`（DSN 按连接生效），连接池由 1 放宽到 4——读与读不再互斥，写冲突由 SQLite 锁排队；此前注释声明 WAL 但实际从未执行 PRAGMA
- **日志 caller 指向 zerolog 内部**：`pkg/log` 的 `CallerWithSkipFrameCount` 由 1 改为 3（zerolog caller hook 自带 2 层内部帧 + 包级 helper 1 层），caller 字段恢复指向真实调用点，不再出现 `github.com/rs/zerolog@…/event.go:131`
- **Wikipedia 引擎对带操作符的查询恒零命中**：MediaWiki `list=search` 不支持 `site:` / `lang:` / `after:` / `before:`，而上游检索语法会把它们拼进 query，MediaWiki 当成普通词面处理，整条查询返回 0 条。引擎现在检索前先剥离这些操作符（清洗后为空则回退原查询），`site:go.dev/ref/spec Go language specification interface types` 由 0 条恢复为正常命中。新增 `sanitizeWikiQuery` 及单测。（[PR #26](https://github.com/daidaiJ/websearch-mcpserver/pull/26)，[@MattLYT](https://github.com/MattLYT)）

### CI
- **SSRF 预检模糊测试进 CI**：`mcp/security_fuzz_test.go` 覆盖 `validateURLSecurity` / `isPublicIP` / `isPrivateHostFast`（种子含十进制/十六进制/八进制回环、IPv4-mapped IPv6、云元数据地址、非 http(s) 协议），`test` job 跑 10s 短窗口
- **PR CI 增加竞态检测与静态检查**：`test` job 追加 `go test -race -short ./...`（hybrid/apipool/进度聚合是多 goroutine 热区，竞态在普通测试下不可见）；新增 golangci-lint job（v2.12.2，`.golangci.yml`，`--new-from-rev` 只拦新增代码，PR 对比 base、push 对比上一提交），历史存量问题分批还
- **发布流程去重与模块共享缓存**（[PR #22](https://github.com/daidaiJ/websearch-mcpserver/pull/22)，[@MattLYT](https://github.com/MattLYT)）：新增 `go-module-cache.yml` 在默认分支作用域维护纯模块缓存（push 触碰 go.mod/go.sum 时重建，周一/周四定时保温防 7 天闲置回收，支持手动触发）；release 四平台构建改 restore-only 还原模块（setup-go 关闭混合缓存，不再为每个标签保存只有同一标签可用的缓存）；`mcpb-tool-cli` 从 `@latest` 两次安装改为固定 commit `3cdeb94c` 单次安装（MCP Registry 发布链路供应链加固，版本可复现）；docker.yml 触发层排除 `-registry`/`-preview` 标签（job 级保护保留）。缓存未命中回退普通模块下载，发布矩阵（Release 四平台 / GHCR 两平台 / mcpb 跟 Release）不变
- **release.yml 写权限收窄**（吸收后适配）：工作流级 `contents: write` 改为 `contents: read`，写权限下放至 release 与 publish-registry 两个 job，build job 仅保留读取

## v3.8.1 — 2026-10-15（正文交付与来源状态修复）

> 分支 `feat/absorb-pr20`：吸收 [@MattLYT](https://github.com/MattLYT)（Matt.Li）的 [PR #20](https://github.com/daidaiJ/websearch-mcpserver/pull/20)，v3.8.0 的后续修复。原提交保留原作者署名（merge `1b12267`）；吸收后追加加固提交（竞态消除、进度通知攒批与读取热点优化）。

> 致谢：本版修复来自 [@MattLYT](https://github.com/MattLYT) 的 PR #20（结构化正文交付状态、摘要缓冲消费、供应商名规范化、缓存键升级、第三方署名勘正）；其提交保留原作者署名进入本版。

### 新增
- **`llm.summary_progress_interval`**：摘要流式进度推送最小间隔（秒），默认 1；未配置或非正值回退 1s，零值即安全默认

### 变更
- **摘要进度通知攒批**（吸收后加固）：progressBatcher 按间隔攒批推送 MCP 进度通知，消除每 token 一次的通知热点（实测 ~270-450µs/次，快速供应商下成为瓶颈）
- **telemetry WAL synchronous=NORMAL**（吸收后加固）：免去逐 commit fsync（单事件 8.2ms→1.1ms）；遥测为可丢失观测数据，崩溃最多丢最近未 checkpoint 的事务
- **cleanfetch 续读偏移缓存**（吸收后加固）：分页读取缓存字符/字节偏移，顺序续读 Seek 直达，消除 O(n²/limit) 逐 rune 扫描（128k 字符完整读回 205ms→112ms）

### 修复
- **正文交付状态结构化**（PR #20，@MattLYT）：`SearchResult` 新增 `content_saved` 标记，替代正文 `**文件路径**:` 文案识别——普通文档恰好含该文案不再被误判为临时文件（不再错误跳过缓存、不再附加为落盘提示）；`fetch_top_n` 的落盘状态随结构化标记传递，`ContentSaved` 为唯一判据
- **摘要空结果/丢尾段**（PR #20，@MattLYT；吸收后加固从源头消除）：LLM `ChatStream` 生产端定序「先关 token 通道后发完成信号」，token 与完成信号严格有序，消费端排干缓冲后阻塞读取最终结果——修复前固定夹具重复 20 次出现 14 次空摘要
- **网页缓存键升级 `web:v3:`**（PR #20，@MattLYT）：旧 `web:v2` 记录（可能含无结构化状态的文件提示）不再复用，按原 TTL 清理；升级后首次查询为冷缓存，时间窗口与配置代次隔离、学术缓存均不变，无数据库迁移
- **供应商名规范化**（PR #20，@MattLYT）：`apipool.engines` 值的大小写/首尾空白在 `GetEngines` 统一归一并返回副本，不改写配置原值与顺序——修复「可路由但控制中心显示未激活」的不一致
- **第三方署名勘正**（PR #20，@MattLYT）：PDF 提取为 go-webfetch 委托（Apache-2.0），`THIRD_PARTY_NOTICES.md` 相应修正

## v3.8.0 — 2026-10-14（检索可靠性与来源控制）

> 分支 `feat/absorb-pr18`：吸收 [@MattLYT](https://github.com/MattLYT)（Matt.Li）的 [PR #18](https://github.com/daidaiJ/websearch-mcpserver/pull/18)。六个功能提交保留原作者署名；其后适配提交对齐控制中心前端与 PDF pending 语义。写操作默认仅 loopback，用户可自行配置 `write_allowed_networks`。

> 致谢：本版核心改动来自 [@MattLYT](https://github.com/MattLYT) 的 PR #18（检索失败透出、来源开关、调用归属、通用 Skill、时间窗口隔离、长结果 `result_ref`）；其提交保留原作者署名进入本版。

### 新增
- **来源开关与设置草稿保存**（PR #18，@MattLYT）：网页/API/学术来源可分组开关，草稿→差异预览→修订冲突检查后写入并重启；配置代次隔离缓存
- **调用归属与统一调用记录**（PR #18，@MattLYT）：SDK 客户端身份 + 可选 `project` 统计标签；调用记录可展开来源尝试、CSV 导出
- **通用检索 Skill**（PR #18，@MattLYT）：`skills/websearch-mcp` 指导原文核验、PDF 路径边界与有界续查
- **长结果 `result_ref` 分页读取**（PR #18，@MattLYT）：超限正文复用 `cleanfetch(result_ref, offset, limit)` 分段读取，不新增第六个工具

### 变更
- **独立写白名单 `write_allowed_networks`**（PR #18，@MattLYT）：写操作默认仅 loopback；用户可在 `dashboard.yaml` 自行放行任意 CIDR/IP，仍须管理员口令，不信任转发头，读白名单不授予写权限
- **控制中心开关改原生 CSS 变量**（吸收适配）：去掉 Bootstrap form-switch 子集，开关跟随主题 `accent`
- **MinerU pending 以工具错误交付**（吸收适配）：尚无正文时 `IsError=true` 并保留 `task_ref`，遥测不计为成功

### 修复
- **检索失败透出与 PDF 正文质量**（PR #18，@MattLYT）：引擎全失败不再伪装空成功；OpenAlex 可选 Bearer；Everything 带空格目录引用；PDF 代理钉扎公网目标
- **时间窗口与缓存隔离**（PR #18，@MattLYT）：`time_range` 省略默认 3 月、显式 `0` 不筛选；缓存键 `web:v2` 按月份隔离，供应商不再改共享默认

## v3.7.0 — 2026-10-10（本地文件检索）

> 分支 `feat/file-search-everything`：新增第五个工具 `file_search`（Everything 索引的本地文件名/路径检索），配套 `skills/everything-http-server` 启用与加固指导。工具走探测门控（无 `enabled` 开关），检索侧带目录白名单、词汇对齐二次过滤、单页条数硬上限与翻页四道上下文闸门。

### 新增
- **`file_search` 本地文件快速检索工具**：基于 Everything (voidtools) HTTP Server 索引的本地文件名/路径检索（毫秒级、只读），Windows 专属——任何 Linux 发行版不建议启用，除非 WSL 下显式配置 `everything.url` 指向 Windows 宿主。**无 enabled 开关**：首个 MCP 客户端接入时探测 `everything.url`（延迟探测，避免 websearch 与 Everything 双自启动的时序竞争导致工具缺失），连通且鉴权通过才注册工具，探测不过不暴露、不影响其它功能
- **克制约束与二次过滤**（agent cost 优化）：`everything.roots` 目录白名单强制限定检索范围（不传 folder 也不给全盘口子，支持 Git Bash 路径风格）；候选超采（单页条数的 3 倍，硬上限 600）后本地按文件名/路径词汇对齐重排（复用 smartsearch 评分管线），噪声目录（node_modules/.git/target 等）降权、`everything.min_alignment` 阈值丢弃弱匹配；每条结果一行紧凑输出
- **单页条数硬上限与翻页**【成本】：单页返回条数默认 **10**（`everything.max_results` 改默认值），硬上限 **20**——agent 传的 `max_results` 与配置值超过 20 都收敛到 20，并在响应内显式说明（不静默）；新增 `page` 参数（默认 1，上限 100）。翻页按页码加深候选超采（`page × 单页条数 × 3`，硬上限 600）后，在二次过滤后的结果集上连续切片，页与页之间不重复、不跳条；候选池触顶时响应明说「无法再深翻」并提示收窄 query。尾部提示按翻页 / 过滤 / 触顶三类去向给出，命中过多先收窄 query 而不是调大条数
- **参数灵活分层**：Everything 原生过滤透传（`match_case`/`whole_word`/`match_regex`/`match_diacritics`）、`sort`/`descending`/`max_results`/`page`、`exclude` NOT 排除项、`min_alignment` 调用级覆盖（配置作默认）、`time_format`（`datetime`/`iso`/`filetime`）
- **`skills/everything-http-server`**：分版本启用指导（1.4 内建 vs 1.5a 插件）、ini 运行中覆盖陷阱、curl 验证、加固清单（loopback / Basic 鉴权 / 禁下载 / 白名单）与故障速查

## v3.6.2 — 2026-10-06（搜索服务升级）

> 分支 `feat/search-upgrade`：对标 free-search-mcp / agent-search-mcp 设计评估的五期落地——一期失败透出统一契约与评分口径修正（P0-1 + P1-6/7/8）；二期结果来源注记、熔断落盘与 Resource 化（P1-4/P1-5/P1-10）；三期零 Key 来源扩充、响应超限落盘与守卫三态处置（P0-2）；五期全量文档同步与工具描述路由边界。四期（证据预算）与 P0-3 插件市场分发经评估取消。

### 新增
- **失败透出统一契约【稳定】**（一期 P0-1）：通用 smartsearch 部分引擎失败不再静默——响应末尾附结构化失败清单（引擎名 / 失败类型 timeout·rate_limit·challenge·off_topic / 短原因），与学术搜索逐引擎透传口径统一；限流冷却中的引擎带剩余冷却时间透出（gated/benched）；全部失败时错误信息带逐引擎分类摘要，主引擎失败 + Bing 回退也失败时两条原因都可见；空结果必须可解释
- **过滤诊断 filter_diagnostics【稳定】**（一期）：过滤后结果稀疏（≤3 条）时透出各过滤器丢弃数（去重 / min_score / engine_max_size / global_max_size / 评分管线）与放宽提示，agent 可据此决定是否放宽阈值重搜；失败清单不写入缓存、不受后续证据预算裁剪
- **off-topic 整桶守卫【稳定】**（一期 P1-7）：hybrid 合并层新增诱饵页防线——某引擎整桶几乎不 echo 查询其余词、且另一引擎证明这些词会被正常 echo 时整桶丢弃，以 off_topic 类型进失败清单（Bing 式"HTTP 200 诱饵页"不再整桶混入 RRF）；无参照或短查询时保守放行
- **缓存 TTL 按 freshness 分桶【稳定】**（一期 P1-8）：学术搜索 day → 1h、week → 6h、month → 24h；smartsearch 时间窗 ≤1 个月 → 24h、其余默认 6h；顺带修复 Lookup 从不查过期的遗留问题（原 6h 仅靠后台清理按 last_hit_at 淘汰，持续被命中的陈旧记录永不过期），过期记录惰性删除，旧库自动迁移（ttl_seconds 列）无感升级
- **结果来源注记【稳定/体验】**（二期 P1-4）：每条结果带 `date_source` 三态——`structured`（引擎 API/结构化字段，可直接采信）/ `snippet`（摘要文本解析，弱）/ `undated`（无日期）；响应头带 `retrieved_at`（缓存命中为原检索时间）与 `cache_age_seconds`（仅缓存命中），并附固定 `usage_note`（"snippet 日期仅用于定位来源，日期、金额、版本等细节必须读原页核实"）；跨引擎同 URL 去重时弱日期可被强日期覆盖（`core.PreferDate`），旧缓存行缺注记时按弱口径 `snippet` 兜底，网页引擎结果渲染新增带注记的日期行
- **引擎冷却状态落盘【稳定】**（二期 P1-5）：`pkg/antirobot` 新增冷却统一注册表，DDG / arXiv 的限流冷却随进随落盘（与缓存库同目录的 `engine_health.json`，temp+rename 写入）——单二进制频繁冷启动不再反复撞死引擎：新进程构造引擎时收养未过期冷却继续避让；已过期条目保留 24h 记忆窗，连续限流的翻倍档位跨进程续接；一次成功即全部清除；文件为 advisory 语义（损坏/缺失只损失一次试探）
- **MCP Resource 观测【体验/成本】**（二期 P1-10）：新增 `search://capabilities`（能力矩阵：已注册工具、网页引擎、API 供应商仅 Key 数量、功能开关）与 `search://health`（引擎冷却/熔断状态、进程运行时长）两个只读 Resource——不占工具槽位、不增加工具选择时的 schema token；内容结构上不含任何密钥（密钥值永不回显）；默认启用，新增 `mcp_resources` 配置项可关闭（省略 = 启用）
- **零 Key 来源扩充（P0-2）【体验/能力】**（三期）：四个候选逐个出口实测后注册——
  - **360 搜索（so360）**：中文第二索引，国内直连可用无需代理（`so360.enabled`，默认关闭）；解析 `li.res-list` + `data-mdurl` 真实 URL，过滤 g-mohe 特型卡，验证码页按反爬拦截报错
  - **AnySearch 匿名档**：`mode=anysearch` 无 Key 时自动走匿名档（省略鉴权头，上游按出口 IP 限流），apipool / hybrid 编排无 Key 也纳入；本端保守钳制 1/s·20/min，失败不冷却 Key 而是由编排层切换来源
  - **Wikipedia 引擎**：MediaWiki API 参考型来源（`wikipedia.enabled`，需代理）；`wikipedia.lang` 选语言版本（默认 zh）
  - **Google News RSS 引擎**：独立新闻索引带结构化发布日期（`google_news.enabled`，需代理）；`news.google.com` 跳转链接自动经 batchexecute RPC 回源发布方 URL（对齐上游 gnews.py 契约，实测可回源），`google_news.edition` 选新闻版本；freshness 映射 `when:` 操作符
  - 每个来源均带真网集成测试（`WS_TEST_NETWORK` 门控）；Wikipedia / Google News 与 DDG 同策略：无可用代理解析时跳过注册
- **搜索响应超限落盘【成本/稳定】**（三期）：新增 `smartsearch.inline_max_chars`（默认 32768，负数 = 禁用），渲染结果超限时**整体落盘临时文件**（零丢失，不截断）——完整结果写入 `cleanfetch.file_output_dir` 约定目录（默认 exe 同目录 `fetchdata/`）的 `search-*.md`，惰性清理超 7 天旧文件；响应内保留溯源头、统计（条数/字符数）、文件路径、读取提示与失败清单（失败清单永不因落盘裁剪）；实时搜索与缓存 query_only 命中路径共用。动机：堵 Tavily `raw_content` 默认开启的响应体积失控口（对齐 Exa `text_max_characters` 的先例，处置口径 = 落盘而非截断）

### 变更
- **off-topic 整桶守卫改为三态配置【稳定】**（三期）：新增 `smartsearch.off_topic_guard`——`shadow`（默认，照常计算回声率，疑似诱饵整桶只记失败清单与日志、**不丢弃结果**）/ `enforce`（一期行为：整桶丢弃并在失败清单透出，须显式开启）/ `off`（完全关闭）。回查结论：守卫三阈值（回声率 0.25 / 参照 0.75 / 最小桶 3）未实测校准即生效且无开关，误杀代价大于漏放——默认改为 shadow 消除误杀风险，shadow 记录为后续阈值校准积累真实证据

### 修复
- **共识计票口径修正【稳定】**（一期 P1-6）：Wigolo 共识 Boost 改按引擎家族（family）计票——百度网页引擎与百度千帆 API（baidu_api/baidu_ai/baidu_web）同上游命中同一 URL 不再双计共识；映射表留扩展位
- **学术结果日期来源注记**（三期）：`academicsearch` 的 `FormatPaperMD` 发表日期改用 `FormatDateSource` 渲染（与网页结果同口径带 `structured`/`snippet（弱）` 注记），Google Scholar 等 snippet 日期不再与结构化日期同样裸露呈现
- **MCP 握手版本对齐**（三期）：initialize 握手的 `implementation.version` 改用注入的二进制版本（原硬编码 `1.0.0`），与 `search://capabilities` 的版本同源

### 文档（五期）
- docs/search（中英）新增「响应行为与可靠性」：失败清单 / filter_diagnostics / off_topic_guard 三态 / date_source 注记与响应头 / 超限落盘 / freshness 缓存 TTL / 熔断落盘 / Resource 汇总；引擎对照表补 `so360` / `wikipedia` / `google_news`
- docs/api（中英）MCP 端点补响应行为契约摘要；README（中英）核心特性表补「响应透明」行
- 四个 MCP 工具描述补「适用边界」路由提示（smartsearch 与 academicsearch 分工、cleanfetch / pdf_parser 职责边界）

## v3.6.1 — 2026-10-01

> 致谢：本版核心改动来自 [@MattLYT](https://github.com/MattLYT)（Matt.Li）的 PR #17（MinerU 超页 PDF 本地裁切 + 图片资源包），其提交保留原作者署名进入本版（`06e28e4`）；后续提交为基于其实现的扩展（可配置化 / 分批 / 预算 / 进度推送）。

### 修复与增强
- **MinerU 超页 PDF 与图片资源包**（PR #17，@MattLYT）：远程精准 API 优先透传 `page_ranges`（不再全文返回）；原 PDF 超页或 MinerU 拒绝源 URL 时，本地下载原件用 qpdf 裁切所选页后走签名上传，返回完整 ZIP 地址（含 images/ 图片资源）；扫描件 OCR 回退同样支持选页裁切；新增回归测试与文档

### 新增
- **`pdf_parser.mineru_page_limit`**（默认 600，对齐 MinerU 官方 2026-10 限制 200MB/600 页）：精准 API 单任务页数上限，替代硬编码 200；请求页码超出时触发本地裁切回退
- **`pdf_parser.mineru_page_batch_size`**（10–200，0=不分批默认）：超出单任务上限的页码自动分批、串行提交，批内标注原页码；保护 2000 页/天优先级额度
- **`pdf_parser.mineru_page_budget`**（0=等于单任务上限）：单次调用 MinerU 页数预算，耗尽返回已解析部分并在 Preamble 提示续读页码
- **分批进度实时推送**：下载原件与每批开始/完成经 MCP progress notification 推送（Streamable HTTP 下走 SSE），agent 无需整批盲等

## v3.6.0 — 2026-09-23

> 致谢：本版核心能力（被动遥测层 + 本机控制中心）来自 [@MattLYT](https://github.com/MattLYT)（Matt.Li）的 PR #15，其功能提交保留原作者署名进入本版（`535b03a` / `62f6be4` / `beb0f9a`）；其余提交为基于其实现的适配与扩展（署名 pandazhangs）。

### 新增
- **本机控制中心（可选）**：`/dashboard/` 只读控制台（总览 / 搜索源 / 事件 / 调用记录 / 设置）+ 被动遥测（`pkg/telemetry`，SQLite 存脱敏元数据，查询只落哈希/主题/语言/关键词）；吸收自 PR #15（Matt.Li），含失败分类、置信度健康、熔断展示、request id 调用链与 Prometheus 文本指标
- **额度管理**：本地真实调用计数 + 默认上限（`dashboard.quotas.limits`，未配置 1000 次/周期）、自动周期重置（monthly/weekly/daily/none）、管理员手动重置与用量修正（真实记录不动）；Tavily 官方用量可用时优先。**注意：上限仅用于展示与剩余量计算，到达上限不会阻断调用——超限熔断为计划中能力（TODO）**
- **品牌与主题可定制**：`dashboard.brand`（title / logo / theme / accent），内置绿色办公、蓝白科技、黑白灰度·立体三主题 + 自定义主色；桌面快捷方式图标跟随主题，启动时轻量检查、变化即重建
- **桌面快捷方式 lazy 启动**：新增 `open` 子命令（先拉起服务再打开控制台），`install` 创建带自绘 W+放大镜图标的桌面快捷方式（去重、随 brand.theme 换图标、支持 brand.icon 自定义）；自启动（autostart.vbs）保留为可选常驻方案
- **全新部署默认启用控制台**：首次 `start`/`install` 自动生成默认启用的 `dashboard.yaml`（随机本机管理员口令）；改 `enabled: false` 或删文件即关闭，运行时零遥测开销
- **客户端用量归组**：总览页新增「客户端用量」面板（initialize clientInfo 权威识别 + UA 兜底，按最近 7 天工具层调用归组，内嵌 tab 切换总分/按客户端）；无数据时自动隐藏
- **前端布局现代化（重排）**：事件流拆独立「事件」页（修复表头 8 列与数据 9 格的错位遗留问题）；总览 3 KPI + 3 面板严格对齐（去掉冗余「今日调用总数」卡）；设置页 2×2 对齐网格（高级操作并入「外观与维护」）；宽表格粘性横向滚动条常驻可视区底部；侧边栏关于块吸底；内嵌资源 no-cache 防升级后旧缓存
- **搜索源分层排序**：总览与搜索源页的来源清单按「已启用 → 已配置未启用 → 未配置」排序（Web 表与学术表一致），同层内保持固定目录顺序
- **启动时自动同步官方额度**：服务启动即后台预热 Tavily 官方用量（只读、不产生计费调用），控制台首次打开即有数据；查询失败静默、访问面板时按需重试
- **额度上限按 Key 数自动换算**：`quotas.limits` 语义改为**单 Key** 每周期上限，展示上限 = 单 Key 上限 × 已配置 Key 数，与 apipool 按可用 Key 数累加的实际消耗容量对齐，用户无需手工换算
- **API 供应商默认直连（代理加固）**：上游 API 请求（千帆/Tavily/Exa/AnySearch/豆包/LLM/额度查询）显式置空 transport 代理，不再被 HTTP(S)_PROXY 等环境变量静默劫持；`proxy.api_providers: true` 可显式启用走代理；文档补充 AnySearch 等小众供应商 DNS 解析异常的说明
- **供应商额度接口调研结论（2026-09）**：Exa / AnySearch / 博查（豆包搜索）/ 百度千帆均无「用搜索 Key 直查额度」的官方端点，不做自动查询——额度上限由用户在 `dashboard.quotas.limits` 自设，配本地真实调用计数对照展示（结论已写入 docs/dashboard.md）
- **快捷方式落位可配置**：`dashboard.shortcut = desktop（默认）/ start（开始菜单，开始屏幕可搜索）/ both / off`；换值自动清理旧位置，uninstall 始终全清
- **侧边栏「关于」块**：项目自我介绍 + GitHub 链接，吸底显示于「仅限本机」上方，`dashboard.brand.footer: false` 可整块隐藏（默认启用）

### 安全
- **写操作闸门**：设置/密钥/重启/清缓存/额度管理仅限本机 loopback + `dashboard.admin_password`（或 SHA-256），口令只能配在 `dashboard.yaml`、WebUI 无法读取或修改；未配置口令 = 写端点全部禁用
- **可选管理员用户名**：`dashboard.admin_username` 配置后写请求还需携带匹配的 `X-Admin-User` 头（常量时间比较）；不配置 = 只验证口令（与上游行为一致），用户名同样只住 dashboard.yaml
- **访问白名单**：`dashboard.allowed_networks`（CIDR/裸 IP）显式放行只读网段，非法条目整体回退仅本机（fail-closed）；移除 PR 中基于 Host 头的 loopback 放宽（可伪造），`/docker` 场景改由白名单显式声明
- **独立配置文件**：控制中心专属配置住 `dashboard.yaml`（覆盖主配置 `dashboard:` 块），主 config.yaml 零改动升级、删文件即回退；损坏时警告并忽略，不拖垮主服务

### 优化
- 密钥设置弹窗内置各供应商官方「获取 Key」直达链接；控制台页面样式统一、零第三方依赖经 go:embed 内嵌
- release 工作流支持 `-preview` 后缀 tag：自动标记 Pre-release 并跳过 GHCR 镜像
- **Docker 文档补齐控制中心**：端口无需新增（控制台与 MCP/SearXNG 共用 8338），补 `dashboard.yaml` 与 `data/` 卷映射及不挂载的后果，并写明桥接网络下控制台只读（写操作要求 loopback，Linux 可用 `network_mode: host`）
## v3.5.1 — 2026-09-14

### 修复
- **CLI 帮助不再依赖配置文件**：`websearch-mcpserver -h` / `--help` / `help` 在无配置文件时即可查看完整用法（此前 `help` 在配置加载失败后直接报错退出，必须 `-c` 指向有效配置才能用；`-h` 则只打印 flag 包默认参数列表）
- **`-c` 支持放在子命令之后**：Go flag 遇到首个非 flag 参数即停止，此前 `start -c /path/config.yaml` 中的 `-c` 被静默忽略；现在子命令后的 flag 会二次解析，`-c`/`--config` 置于命令前或命令后均生效（`websearch-mcp-cli` 的 `init -c` 同步修复）
- **usage 示例对齐实际行为**：两个二进制的 help 补齐 `-c, --config` 默认查找顺序（`./config.yaml` / `$WEBSEARCH_CONFIG`）与命令示例

## v3.5.0 — 2026-09-13

### 新增
- **`fetch_top_n` 三分语义 + 双路径取正文**：不传 = 与旧版完全一致（只返回引擎/供应商自带内容，服务端 `smartsearch.fetch_top_n` 可改默认，默认 0）；传 0 = 只要标题摘要和 URL；传 1-5 = 为前 N 条获取页面原文——**支持原文传参的 API 引擎走快速路径**（Tavily `include_raw_content`、Exa `contents.text`（`exa.text_max_characters` 默认 3000）、豆包 Custom `need_content`，均默认开启、可显式关闭），**其余引擎内部抓取页面**（webfetch 惰性初始化，无需 cleanfetch 开启）；已有足量正文（≥1000 字符）的条目自动跳过内嵌抓取
- **抓取失败显式告知**：内嵌抓取被反爬防护（JS 挑战/WAF/验证码）拦截或失败时，在该条结果上标注原因与建议（换来源 / cleanfetch 重试），不再静默吞掉
- 工具参数 `fetch_top_n` 改为可区分「未传」与「显式传 0」，schema 写明语义；两个 smartsearch 参数定义改用匿名嵌入结构体共用字段，schema 单处维护

- **`fetch_top_n` 大正文落盘不再误判失败**：正文超过 `max_inline_lines` 时 webfetch 落盘返回 `saved_to_file`（`Markdown` 为空），此前被当成"抓取结果为空"标注失败；现在把文件路径与读取提示写回该条结果（与 cleanfetch 大文本输出格式一致）
- **cleanfetch 落盘文件 TTL 从未生效**：上游 `CleanFiles()` 从未被调用，输出目录无限堆积临时文件；新增空闲期清理协程（无进行中抓取且距上次活动 ≥1 分钟时执行，1 分钟检查一次，随引擎关闭停止）；只删除文件名匹配本程序保存模式（日期_时间_slug_6位hash.md）且超过 TTL 的文件，目录里的其它用户文件一律不动
### 修复
- **`smartsearch` 的 `fetch_top_n` 默认部署上真正生效**：webfetch 不再要求 `cleanfetch.enabled` / `pdf_parser.enabled` 显式开启，`fetch_top_n>0` 时按当前配置惰性初始化；抓取路径与 cleanfetch 对齐（SSRF 预检 + HEAD 体积预检），并新增单条正文字节上限（`cleanfetch.max_fetch_size_mb`，默认 10MB）；单条失败打 warning 日志并保留 snippet，不再静默吞掉
- **`pdf_parser` 的 `pages` 区间先限宽再分配**：单个区间宽度超过 1000 页（如 `pages="1-2147483647"`）立即报参数错误，不再先展开切片导致内存暴涨
- **HEAD 预检重定向逐跳安全复查**：HEAD 跟随重定向（最多 5 跳）时对每一跳目标复跑私网/云 metadata 校验；公网 URL 302 到 `169.254.169.254` 等内网地址会被拒绝，不再被当成未鉴权探针

- **缓存默认关闭（v3.5.0 起）**：`cache.enabled` 不设置时不再按 `storage_path` 判断；显式 `enabled: true` 启用，`storage_path` 未配置时默认 exe 同目录 `cache/websearch-cache.db`
- **cleanfetch 落盘目录默认 exe 同目录 `fetchdata/`**：不再用系统临时目录 `webfetch/`
- **配置示例 / 配置说明 / 模式表补齐 API Key 获取地址**：Tavily / Exa / AnySearch / 豆包（`config.example.yaml`、`docs/configuration`、`docs/search`、README，中英双语）
### 变更
- **`pdf_parser` 省略 `pages` 的行为自 v3.4.0 起为「前 `max_pages`（默认 20）页 + 截断提示」**，不再解析全文；文档已对齐，长文档请显式传 `pages`
- **`academicsearch` 工具描述与 `docs/search` 补充 DOI / arXiv id 短路查询用法**（query 直接传 id 时忽略 `engines`/`time_range`/`page`，`pdf_url` 可交给 `pdf_parser`）；配置文档补充 `academic.unpaywall_email`（环境变量 `UNPAYWALL_EMAIL`，空=跳过 Unpaywall OA 补链且不报错）

## v3.4.0 — 2026-09-11

### 新增
- **豆包联网搜索**：接入火山引擎 Doubao Search Global / Custom（`mode=doubao`）；有 Key 时参与 `hybrid`；`apipool` 需显式把 `doubao` 写入 `engines`（不改默认供应商列表）。`Name()` 与结果 `Engine` 均为 `doubao`，Custom 实现 `SearchTimeRanger`。环境变量 `DOUBAO_SEARCH_API_KEY`（兼容 `ASK_ECHO_SEARCH_INFINITY_API_KEY`）。免费档每月默认 500 积分，`apipool.weights.doubao` 默认 500。Global / Custom / Custom `time_range` 本地实测通过；集成测试对齐 Tavily（`-short` 跳过，Key 只从 gitignore 的 `config.test.yaml` 读取）

### 修复
- **学术 `time_range` 语法**：Crossref 改用 `filter=from-pub-date:`；DOAJ 改用 `bibjson.year` 闭区间（禁止 `*`）；arXiv 改用 `submittedDate:[YYYYMMDDHHMM TO YYYYMMDDHHMM]`（UTC）。带时间范围的 `academicsearch` 不再把这三家打成上游失败

### 致谢
- [@matt1060338871-pixel](https://github.com/matt1060338871-pixel)：[#5](https://github.com/daidaiJ/websearch-mcpserver/pull/5) 学术 `time_range`、[#6](https://github.com/daidaiJ/websearch-mcpserver/pull/6) 豆包联网搜索（按仓库编排改写后合入 [#7](https://github.com/daidaiJ/websearch-mcpserver/pull/7)）

### 文档
- **发布矩阵拆分**：GitHub Release 二进制 4 平台（linux/windows amd64 + darwin amd64/arm64）；GHCR `linux/amd64,linux/arm64`；MCP Registry 按 Release 封 4 个 mcpb。linux-arm64 容器走 GHCR，不提供 GitHub `linux-arm64` 二进制
- **打 tag 约定**：先打普通 tag（`vX.Y.Z`）发 GitHub Release 并推送 GHCR 镜像；发完后再单独补 `-registry` 后缀 tag 发布到 MCP Registry，不要和版本 tag 一起推。后补 tag 钉同一 commit
- **配置与工具文档对齐 v3.4.0**：豆包 Key / `apipool.engines` 默认不含 doubao / `weights.doubao=500`、academicsearch `time_range` 官方语法、stdio 环境变量与 gitignored `config.test.yaml`

## v3.3.0 — 2026-09-04

### 新增
- **AnySearch 搜索引擎**：接入 [AnySearch API](https://www.anysearch.com/docs)（`POST /v1/search` + Bearer 认证，envelope code=0/-1，Key 无效返回 401/403）；新增 `mode=anysearch` 单引擎模式，并加入 `apipool` / `hybrid`；API 不支持 exclude_domains，`black_list_host` 在本地过滤
- **apipool weighted 加权负载均衡**：`apipool.strategy` 新增 `weighted` 策略，按权重加权随机选起始供应商（突发请求天然分散）；供应商有效权重 = 配置权重 × 当前可用 SK 数（SK 冷却后权重自动下降，自愈）；显式 `0` 不参与起始选择、全 0 退化为 round-robin；百度网页搜索兜底固定权重 1
- **apipool.weights 权重配置**：单 Key 权重，默认 `anysearch=30000`、`baidu=1500`、`tavily=1200`、`exa=1200`，可按供应商覆盖
- **环境变量 `ANYSEARCH_API_KEY`**：与 `BAIDU_SK` / `TAVILY_SK` / `EXA_API_KEY` 同机制（viper BindEnv + applyKnownEnv 回填）

### 变更
- **apipool.engines 默认顺序**：`[baidu, tavily, exa]` → `[anysearch, baidu, tavily, exa]`（anysearch 免费额度最大）；未配置 anysearch Key 时行为不变
- **KeyPool 同供应商 SK 去重**：重复 Key（trim 后比对）只保留一个，避免相同 Key 被当成多个可用 Key 轮询与权重累加；去重时输出 Info 日志

## v3.2.1 — 2026-09-03

### 新增
- **MCP 无状态 HTTP 模式**：新增 `mcp_stateless` 配置开关（默认 `false` = 会话式）；开启后每个 POST 独立处理，免 initialize 握手与 `Mcp-Session-Id` 会话，便于反向代理/负载均衡水平扩展；GET SSE 长连返回 405。对齐 MCP 2026-07-28 规范的 stateless-first 方向；本服务工具均为请求-响应式，无状态模式下功能无损
- **GHCR 镜像发布**：tag 推送自动构建镜像发布 `ghcr.io`（closes #3），release 产物附带 linux/amd64 镜像 tar 包；Dockerfile 修复（golang:1.26 对齐、整包编译、版本号注入）

### 变更
- **失效引擎默认禁用**：`baidu.web_enabled` 新增（默认 `false`）——百度网页搜索引擎（tn=json 直抓）实测被 CAPTCHA 识别，默认禁用，出口 IP 干净的部署环境可显式开启

## v3.2.0 — 2026-08-30

### 新增
- **学术搜索扩展至 9 引擎**：新增 Europe PMC（生物医学 / PubMed 增补源）、DBLP（CS 会议/期刊索引）、DOAJ（开放获取期刊），国内均可直连；`academicsearch` 工具描述与引擎选择建议同步
- **逐引擎错误透传**：部分学术引擎失败不再静默吞掉——失败引擎记入 `EngineErrors`，结果末尾提示「部分引擎本次失败，结果可能不完整」；全部无结果时错误摘要带各引擎失败原因
- **Semantic Scholar API Key**：新增 `academic.semantic_scholar_api_key`（环境变量 `SEMANTIC_SCHOLAR_API_KEY`）；带 key 遇 429/503 退避重试，连续失败自动降级匿名模式（进程内不回切），匿名通道 429 直接报错
- **Google Scholar 重试强化**：403/429/503 指数退避（1.5s→3s + 随机抖动）并轮换桌面 UA 重试；CAPTCHA（/sorry 跳转）不浪费重试直接报错；结果补全 DOI（落地页 URL 提取，回退摘要内文匹配）
- **DOI 跨引擎去重（双键）**：学术结果按 DOI + URL 双键合并，任一键命中即视为同文，解决「一方有 DOI、一方只有相同 URL」的漏合并；新增 `antirobot.ExtractDOI` 通用 DOI 提取

### 变更
- **DDG 限流防护三层化**：实测校准内置钳制（1/s、6/min，宽松配置自动钳制并告警）；202/429 触发进程级冷却（连续翻倍、上限 2min、Retry-After 优先）；冷却避让与搜索超时预算联动——等待 + 重试能装进预算就等一次，注定超时快速放弃
- **arXiv 限流加强**：对齐 DDG 模式——官方 Tou 1 req/3s 最小间隔 + 内置钳制（1/s、12/min）+ 429/503 冷却避让；本地限流等待同样预算感知
- `antirobot.RateLimiter` 新增最小间隔约束（`WithMinInterval`）；`ParseRetryAfter` 提升为 antirobot 共享工具（DDG 改为委托）；修复 DDG 进入冷却时错误信息重复翻倍的问题

### 文档
- README / AGENTS 引擎数量与能力介绍同步 9 学术引擎；AGENTS 过时「能力缺口」表更新为当前能力边界
- Google 引擎配置处沉淀 JS 挑战时间线（2025-01 灰度上线 → 2025 H2 全量硬化）与「无法通过伪装绕过」的结论
- 配置文档（中英）与两份 `config.example.yaml` 同步 `semantic_scholar_api_key`、`disable_europepmc/dblp/doaj`
- 删除已实现完毕的规划文档 `issues/academic-search-improvements.md`

## v3.1.0 — 2026-08-26

### 安全
- **默认只监听本机**：新增 `host` 配置（默认 `127.0.0.1`），服务不再默认绑所有网卡；`0.0.0.0`/`::` 且未配置 token 时启动打 Error 警告
- **业务端点可选 Bearer 鉴权**：新增 `auth_token`（环境变量 `WEBSEARCH_TOKEN`），`/mcp` 与 `/searxng/search` 支持 `Authorization: Bearer` / `X-API-Key` 任一通过；未配置时不鉴权（向后兼容），`__admin/*` 保持本机协议不变

### 修复
- **API 上游默认 30s 超时**：`pkg/client` DefaultClient 设置超时（`upstream_timeout_sec` 可配置，0 = 不设超时），黑洞上游不再永久挂起 MCP 工具
- **KeyPool 并发标错 key**：搜索适配器失败时返回携带实际 key 的错误，apipool 按 key 精确冷却；`MarkLastInvalid` 标注非并发安全，新代码禁用
- **双 SearchGroup 合并**：MCP 与 SearXNG 共用同一套引擎（`GetSearchGroup`），限流与 Key 状态不再分裂；SearXNG 空 query 返回 400、无引擎返回 503，不再 panic
- **单引擎结果不再被默默截成 4 条**：per-engine `max_size` 未配置时只应用全局 `max_size`，apipool/baidu_ai 等模式结果数恢复可配置
- **hybrid 记录单引擎失败**：Warn 日志 + 全失败时错误摘要（不泄露 API Key）
- **代理 transport 按端点缓存**：不再每请求 Clone，连接池生效；系统代理检测加 10s TTL 缓存
- **WinHTTP 代理字符串越界**：按缓冲区边界读 UTF-16 至 NUL，删除定长 512 强转
- **MinerU 仅处理 PDF URL**：远程 URL 只有 `.pdf` 结尾才走精准 API（`mineru_remote_pdf` 可关闭，默认 true），普通网页不再消耗 MinerU 额度
- **viper env 回填**：`Load()` 后显式 `applyKnownEnv`，精简 yaml 缺字段时 `TAVILY_SK` 等环境变量仍生效
- **关机顺序**：先 HTTP Shutdown 再关 WebFetch/SQLite，进行中请求不再撞 `sql: database is closed`
- **start TOCTOU / 脏 PID**：监听成功后才写 PID；端口占用返回错误不 panic
- **Jina 内网判断**：`net.ParseIP` + `IsPrivate()`，`172.32.0.0/11` 等公网段不再被误判为内网
- **缓存 upsert**：`(query, intent, academic)` 唯一索引 + `ON CONFLICT DO UPDATE`，旧库先清理重复行，同 query 不再堆行
- **daemon PostShutdown 泄漏**：响应 body 正确 Close；**CleanupScheduler.Stop** 真正阻塞等待协程退出（可取消）

### 变更
- **「零配置」= 生成可编辑预设 yaml**：首次 `start`（未指定 `-c`）在可执行文件目录写出与 `config.example.yaml` 相同的 `config.yaml`；`install` / `cli init` 统一走 `EnsureExampleFile`（幂等，不覆盖用户改动）
- 外网集成测试统一 `testing.Short()` 跳过（Bing / Crossref / arXiv / OpenAlex / Semantic Scholar / Google Scholar），`go test -short ./...` 不再发起外网请求

### 文档
- 配置 / 安装 / API 文档（中英）同步 `host`、`auth_token`、`upstream_timeout_sec`、`mineru_remote_pdf`；MCP 客户端注册补 headers 示例；README 注明 Google / DDG / Crossref / Google Scholar 国内直连不稳定

## v3.0 — 2026-08-20

### 新增
- **stdio 纯 CLI**：独立入口 `cmd/cli`，默认在 stdin/stdout 上运行 MCP；无配置文件时使用 `mode: engine` 内存默认值
- Release 额外发布 `websearch-mcp-cli-{linux,windows,darwin}-{amd64,arm64}` 及对应 `.sha256`
- CLI 子命令：`init` 写出示例配置，`version` 显示版本；日志写 stderr，避免污染 JSON-RPC
- 文档补充 HTTP / stdio 配置差异说明（`docs/config.md`）

## v2.15.0 — 2026-08-14

### 变更
- **pdf_parser 本地优先解析**：本地 PDF 先用 PDF 库（ledongthuc/pdf via go-webfetch）提取文本，成功则直接返回；仅在无文本层（扫描件/图片型）时才考虑 MinerU
- **MinerU OCR 按需回退**：需显式开启 `pdf_parser.mineru_ocr`；未开启时错误提示可配置 OCR；开启后走 MinerU Agent/精准 API，失败保留本地解析错误信息
- **MinerU 客户端初始化条件**：由「`enabled` 或有 Token」改为「有 Token 或开启 `mineru_ocr`」，避免仅启用 pdf_parser 就对每个本地文件优先上传 MinerU
- `PDFParserConfig` 新增 `MinerUOCREnabled()`；工具描述与配置示例同步说明新策略

### 测试
- 新增 `needsOCRFallback`、MinerU OCR 初始化条件、无 OCR 时错误提示等单元测试

## v2.14.0 — 2026-08-02

### 新增
- **智能相关性评分管线（Wigolo）**：多引擎结果采用 RRF（Reciprocal Rank Fusion, K=60）融合排名，叠加词汇对齐、稀有词/短语连续匹配、域名品质惩罚、多引擎共识 / 权威站 / 时效性加分，纯启发式无需 AI 模型
  - 新增 `enhance.go`（RRF / ConsensusBoost / ApplyScoreFloor）、`enhance_domain.go`（DomainQuality / AuthorityBoost）、`enhance_text.go`（LexicalAlignment / RareTermsFactor）、`enhance_intent.go`（意图分类）
- **MMR 多样性重排**：评分流水线阀值过滤之后、maxSize 截断之前执行贪心 MMR 重排（Token Jaccard 相似度），打散同一话题的高相似结果（转载站/镜像站/同源博客），Top-1 保护
  - 新增 `mmr.go`（ApplyMMR）、`enhance_text.go` 导出 `JaccardSimilarity`
  - 配置项：`smartsearch.mmr.enabled`（默认 true）、`lambda`（默认 0.7）、`target_count`（默认 0）
- **学术搜索评分增强**：六大学术引擎结果采用 RRF 融合排名，叠加学术特有信号——引用数（对数压缩，clamp [1.0, 1.7]）、高影响力期刊/会议加分、PDF 全文可用性、新鲜度因子（时间敏感查询时近 1 年 ×1.15）；低分论文自动过滤（Top-1 + 每引擎保底）
  - 新增 `academic_enhance.go`（EnhanceAcademicResults / CiteFactor / JournalBoost / AcademicRecencyFactor）；OpenAlex / Semantic Scholar 回传的 relevance score 参与引擎内排名
  - 配置项：`academic.enhance`（默认 true）、`academic.threshold`（默认 0.02），独立于 smartsearch（四个工具配置解耦）
- **LLM 摘要流式推送**：`smartsearch` 摘要阶段通过 MCP `notifications/progress` 逐 token 实时推送生成过程（搜索完成 → 阶段通知 → token 流），客户端断开自动取消，失败自动回退非流式摘要
  - `pkg/llm` 新增 `ChatStream`（OpenAI 兼容 SSE 流式接口）、`pkg/summarizer` 新增 `SummarizeStream`

### 变更
- **低质量上下文真正被裁剪**：修正 `ApplyScoreFloor`，移除“每引擎保底保留”逻辑，score 低于全局阈值（默认 0.05）的结果被丢弃，仅保留 Top-1（并保证最少 2 条）

### 修复
- **admin 端点不可达**：`config.Load` 未配置 `port` 时默认为 8338，避免绑定到随机端口 `:0` 导致 daemon/CLI 与 agent 无法通过 `http://127.0.0.1:{port}/__admin` 访问端点
- **百度网页搜索返回空结果**：`web_search` 请求补充千帆必填字段 `search_source: "baidu_search_v2"`；`baidu.go` 新增 HTTP 状态码检查，非 200 时显式暴露鉴权/配额/参数错误，避免误报为“内容为空”

### 测试
- 新增 `TestEnhanceFiltersLowQuality` 观测性测试，实测低质量结果被裁剪（5→2）
- 新增 MMR 测试（Top-1 保护 / 多样性打散 / λ=1 退化 / targetN 截断）与学术增强测试（引用数 / 期刊 / 新鲜度信号 / 每引擎保底）
- 新增 `ChatStream` 单元测试（httptest mock SSE 服务：正常流 / HTTP 错误 / ctx 取消 / 畸形行跳过）
- 百度实时 API 集成测试改为环境不可用（网络/鉴权/配额/空结果）时 skip，避免 `go test ./...` 因外部依赖不可用而失败

## v2.13.0 — 2026-07-19

### 变更
- **百度智能搜索 model 可选**：不配置 `baidu.model` 时走免费百度搜索（不产生 LLM 费用），配置模型名才启用 LLM 智能搜索生成；`enable_ai_search` 保持默认 `true`
- **升级 go-webfetch v0.2.0**：cleanfetch 引擎继承 TLS 指纹伪装（tls-client Chrome 131）+ 重试退避 + 系统代理支持，增强反爬能力

### 新增
- cleanfetch 新增 `use_system_proxy`（默认 false）和 `max_retries`（默认 3）配置项

## v2.12.0 — 2026-07-15

### 新增
- **`apipool` 搜索模式**：新增 API Key 池轮转模式，百度智能搜索 + Tavily + Exa 并发去重
  - 负载策略：先选供应商（round-robin），再轮转该供应商的 SK（KeyPool）
  - 百度网页搜索作为无 Key 兜底引擎自动参与
  - 配置项：`mode: apipool`
- **百度智能搜索端点**：新增 `chat/completions` 智能搜索 API（`baidu_ai.go`），返回 LLM 生成的回答 + 参考来源
  - `baidu.enable_ai_search` 控制端点选择（默认 `true` = 智能搜索，`false` = 旧版网页搜索）
  - 支持 `model`（默认 `ernie-4.5-turbo-32k`）、`search_source`（默认 `baidu_search_v2`）、`enable_reasoning`、`enable_deep_search`、`search_mode` 等配置
- **API Key 多 Key 轮询（KeyPool）**：`baidu`/`tavily`/`exa` 三个供应商均支持 `sk_list` 多 Key 轮询
  - `sk_list` 非空时优先使用，为空时自动用 `api_key` 构造单元素列表
  - KeyPool 线程安全，round-robin 轮转
  - 支持 Key 失效标记（`MarkInvalid`），失效 30 分钟后自动恢复
  - 所有 Key 均失效时自动降级为最早恢复的 Key

### 变更
- `baidu` 模式默认使用智能搜索端点（`enable_ai_search: true`），可通过配置切回旧版 `web_search`
- `hybrid` 模式百度引擎也使用 `enable_ai_search` 配置控制端点选择
- `NewBaiduSeach`、`NewTavilySearch`、`NewExaSearch`/`NewExaSearchWithResults` 构造函数改为接收 `*KeyPool` 参数
- `lookbackDaysToRecency(0)` 修复为返回 `semiyear`（默认值）而非 `day`

## v2.11.0 — 2026-07-13

### 新增
- **Exa Web Search API 引擎**：新增 Exa 通用搜索，支持 `type: auto` 自动选择搜索类型、`highlights` 高亮摘要、`excludeDomains` 排除域名
  - `mode=exa` 独立使用 Exa；`mode=hybrid` 时自动参与混合搜索
  - 配置项：`exa.api_key`（环境变量 `EXA_API_KEY`）、`exa.num_results`（默认 5）、`exa.lookback_days`（默认 90）
- **API 引擎时间范围配置**：所有 API 引擎（Tavily、百度千帆、Exa）统一支持 `SearchTimeRanger` 接口，可按时间范围过滤搜索结果
  - Tavily：映射为 `time_range` 参数（day/week/month/year）
  - 百度千帆：映射为 `search_recency_filter` 参数（day/week/month/semiyear/year）
  - Exa：映射为 `startPublishedDate` / `endPublishedDate` 日期范围
- **smartsearch 工具 time_range 参数**：新增 `time_range` 参数（月为单位），支持动态控制搜索时间范围，默认 3 个月
  - 示例：`time_range=1` 搜索近 1 个月，`time_range=6` 搜索近半年，`time_range=12` 搜索近一年
- 新增 `SearchTimeRanger` 可选接口，支持时间范围的引擎自动实现
- 新增 `HybridSearchImpl.SearchRawWithTimeRange`，将时间范围透传给支持的子引擎
- 新增 Exa、Tavily、百度千帆集成测试（API Key 存储在 `config.test.yaml`，已 gitignore）

### 变更
- `config.example.yaml` 搜索模式注释新增 `exa` 模式说明
- 百度千帆 `search_recency_filter` 从硬编码 `semiyear` 改为可配置，默认值不变

## v2.10.0 — 2026-07-12

### 新增
- **DuckDuckGo 搜索引擎**：新增 DuckDuckGo 通用搜索（需代理），`html.duckduckgo.com/html/` POST + goquery 解析，自动参与 engine/hybrid 模式
- **HEAD 预检防大文件**：cleanfetch 抓取前先发 HEAD 请求检查 Content-Length，超过阈值（默认 10MB，`max_fetch_size_mb` 可配）直接拒绝
- **DNS rebinding 防护**：cleanfetch 抓取前 DNS 解析目标域名，检查所有 IP 是否为内网/私有地址，与 go-webfetch 的 BlockPrivateIP 形成双重防护
- **Jina Reader DNS 防护增强**：`isPrivateHost()` 增加 DNS 解析检查，修复纯字符串匹配的绕过风险

### 变更
- **Google 引擎默认禁用**：Google 搜索被反爬机制拦截（TLS 指纹+JS Challenge），默认 `google.enabled: false`，需显式启用
- `CleanFetchConfig` 新增 `MaxFetchSizeMB` 字段（默认 10）
- `Config` 新增 `DuckDuckGoConfig` 和 `GoogleConfig` 结构体

### 修复
- **Google 反爬检测增强**：`detectSorry()` 新增 JS Challenge 页面识别（`/httpservice/retry/enablejs`、`SG_SS`）
- **Google 解析防御**：`parseResults()` 增加 `div#rso`/`div#search` 容器预检，非搜索结果页面直接返回空
- **Google 解析策略增强**：新增 SearXNG 风格的 `a[data-ved]:not([class])` 选择器作为主解析路径，`div.g` 作为回退

## v2.9.0 — 2026-07-12

### 新增
- **MinerU AI 增强 PDF 解析**：`pdf_parser` 工具集成 [MinerU](https://mineru.net) 文档解析平台，支持表格/公式/多栏/图片智能识别
  - **双 API 模式自动切换**：
    - 有 Token → 精准解析 API（`/api/v4`），支持远程 URL 输入，≤200MB/200页，ZIP 输出含 Markdown + JSON
    - 无 Token → Agent 轻量 API（`/api/v1/agent`），支持本地文件签名上传，≤10MB/20页，Markdown 输出
  - **本地文件优先 MinerU**：Agent API 自动签名上传本地 PDF，失败时静默回退到本地解析（go-webfetch）
  - **远程 URL 优先 MinerU**：精准 API 解析远程 URL，失败时回退到本地 webfetch
  - **友好错误处理**：API 错误码统一翻译为中文提示，原始错误仅记录日志，不暴露给客户端
  - 配置项：`mineru_token`（环境变量 `MINERU_TOKEN`）、`mineru_model`（pipeline/vlm）、`mineru_ocr`、`mineru_formula`、`mineru_table`、`mineru_lang`
- 新增 `pkg/mineru/` 包：MinerU API 客户端实现 + 单元测试 + 集成测试

### 变更
- `PDFParserConfig` 扩展 MinerU 相关字段，新增 `MinerUEnabled()`、`GetMinerUModel()` 等辅助方法
- `webfetch.Fetcher` 初始化时自动检测 MinerU 配置并创建客户端
- `pdf_parser` MCP 工具描述动态显示 MinerU 增强状态

## v2.8.0 — 2026-07-11

### 修复
- **cleanfetch 系统代理自动检测**：修复 cleanfetch 无法访问境外站点的问题，支持自动检测系统代理配置

## v2.7.0 — 2026-06-25

### 新增
- **缓存显式开关**：`CacheConfig` 新增 `cache.enabled` 字段（`*bool`），支持显式启用/禁用缓存
  - 不设置时按 `storage_path` 是否非空判断（向后兼容旧行为）
  - 显式 `false` 强制禁用缓存（忽略 `storage_path`）
  - 显式 `true` 强制启用缓存
- **英文版文档**：新增 `README.EN.md`、`docs/config.en.md`、`docs/api.en.md`、`CHANGELOG.en.md`，中英文文档顶部互加语言切换链接
- 新增 `CacheEnabled()` 单元测试（6 个用例覆盖所有分支）

## v2.6.0 — 2026-06-24

### 新增
- **SmartSearch Score 过滤**：per-engine `min_score` / `max_size`，全局 `max_size`，`show_meta` 控制来源和分数展示
  - 引擎回传 score 时按 `min_score` 过滤，保留 `max_size` 条
  - 引擎不回传 score 时忽略 `min_score`，取 `min(max_size, ⌈global_max_size / 引擎数⌉)` 截断
  - 全局 `max_size`：有 score 时按 score 排序截断，无 score 时按引擎轮询均匀分配
  - `show_meta` 控制输出中是否显示引擎来源和相关性分数（默认 true）
- **API 引擎命名区分**：Tavily → `tavily_api`、百度千帆 → `baidu_api`；百度网页搜索保持 `baidu`
- 引擎 `Name()` 方法统一纳入 `SearchInf` 接口

## v2.5.0 — 2026-06-03

### 新增
- **系统代理自动检测**：默认读取 Windows 注册表（`ProxyEnable` / `ProxyServer`）和环境变量（`HTTP_PROXY` / `HTTPS_PROXY`），Clash、V2RayN 等代理软件开启系统代理后自动生效，无需手动配置 `proxy.enabled` 和重启服务
  - `pkg/proxy/sysproxy.go` — 跨平台系统代理检测（Windows 注册表 + WinHTTP + 环境变量）
  - `pkg/proxy/detector.go` — 后台轮询检测器，30s 周期检测代理变更并通知回调
  - `pkg/proxy/proxy.go` — `DynamicProxyTransport` 请求级动态代理解析，每次请求实时获取代理端点
- **Jina Reader 不再依赖 proxy.enabled**：配置 `jina.api_key` 即可启用，代理由系统自动检测
- **全局限流重试**：所有 HTTP 客户端自动处理 429 限流，读取 `Retry-After` 头等待后重试一次（最大等待 5s，超限直接返回 429）
- **arXiv 引擎限流**：内置 1 req/s 限流器（arXiv 官方建议），超限时等待而非触发 429

### 变更
- **引擎初始化逻辑重构**：Google、Semantic Scholar、Google Scholar 始终初始化（除非各自 `disable_*` 显式禁用），代理在请求级别动态解析，开关代理无需重启
- **Jina Reader 去除 proxy.enabled 门控**：`NewFromConfig` 仅检查 `jina.api_key`，代理由 `ProxyResolver` 动态解析
- **Google 引擎 HTTP 客户端**：从静态 `http.ProxyURL` 改为 `DynamicProxyTransport`，请求时实时解析代理
- **学术引擎 HTTP 客户端**：从 `proxy.NewHTTPClient(endpoint)` 改为 `proxy.NewDynamicHTTPClient(resolver)`，支持运行时代理切换；国内引擎也使用带 retry 的客户端
- **测试 URL 更新**：`TestFetchWebPage` / `TestCleanFetch_WebFetchSuccess` 测试 URL 从 `arthurchiao.art`（已不可达）更换为 `wmyskxz.cn`

### 向后兼容
- `proxy.enabled: true` + `proxy.endpoint` 仍正常工作（显式代理模式）
- `proxy.enabled: false` 仍正常工作（显式禁用代理）
- 未设置 `proxy.enabled` 时行为变更：从"不使用代理"变为"自动检测系统代理"

### 新增文件
- `pkg/proxy/sysproxy.go` — 系统代理检测核心逻辑
- `pkg/proxy/sysproxy_windows.go` — Windows 注册表 + WinHTTP 检测实现
- `pkg/proxy/sysproxy_other.go` — 非 Windows 平台占位（仅环境变量）
- `pkg/proxy/detector.go` — 后台代理变更检测器

## v2.4.0 — 2026-06-02

### 新增
- **百度网页搜索引擎**：参考 SearXNG baidu.py 实现，使用 `tn=json` JSON API 直接抓取百度搜索结果，无需 API Key
  - `mode=baidu` 无 SK 时作为主引擎；有 SK 时 SK 失败自动回退网页搜索
  - `mode=engine` 默认与 Bing 并发搜索
  - `mode=hybrid` 自动参与混合搜索
- **Google 搜索引擎**：参考 SearXNG google.py 实现，HTML 解析 Google 搜索结果，需代理访问
  - `proxy.enabled=true` 时自动启用，支持 CAPTCHA 检测、CONSENT Cookie 绕过
  - `mode=engine` / `mode=hybrid` 代理启用时自动加入并发搜索
- **全局限流配置**：新增 `rate_limit` 配置节（`per_sec` / `per_min`），对所有搜索引擎统一生效（默认 3/s, 60/min）

### 变更
- **`engine` 模式引擎组合**：从仅 Bing 改为百度网页搜索 + Bing 并发（代理启用时加入 Google）
- **`hybrid` 模式百度策略**：有 SK 时使用 `BaiduWithFallback(SK, 网页搜索)`，SK 失败自动回退；无 SK 时直接用网页搜索
- **`baidu` 模式增强**：无 SK 时不再回退到 Bing，而是使用百度网页搜索引擎
- **限流默认值提升**：全引擎默认 3/s, 60/min（原 Bing 1/s, 20/min）
- Bing 限流配置从 `bing.per_sec` / `bing.per_min` 迁移到全局 `rate_limit` 配置

### 新增文件
- `pkg/baidu/` — 百度网页搜索引擎（engine + opts + 15 个单元测试）
- `pkg/google/` — Google 搜索引擎（engine + opts + 17 个单元测试）
- `pkg/search/engine_adapter.go` — 通用引擎适配器（antirobot.Engine → SearchInf）
- `pkg/search/baidu_fallback.go` — 百度 SK 回退包装器

## 2026-05-28

### 新增
- **cleanfetch 增强型网页抓取**：集成 `go-webfetch` 库，无需代理即可抓取网页内容，失败时自动回退到 Jina Reader（需代理）
  - `cleanfetch.enabled` 控制开关（默认 false，旧配置不启用）
  - 大内容自动存储到临时文件，支持配置输出目录、TTL、内联阈值
- **pdf_parser PDF 解析工具**：将本地 PDF 文件转换为 Markdown（`pdf_parser.enabled` 控制，默认 false）
- **hybrid 模式 Bing 混合搜索**：hybrid 模式下 Bing 作为原生引擎与 API 引擎（Baidu/Tavily）并发搜索

### 变更
- cleanfetch 工具现在只需配置 `cleanfetch.enabled: true` 即可使用，不再强制要求代理和 Jina API Key
- Go 版本升级至 1.26（go-webfetch 依赖要求）

## 2026-05-26

### 新增
- **Windows 开机自启动**：`install` / `uninstall` 命令，使用 COM API (ole32.dll) 创建快捷方式，无需依赖 PowerShell
- **PubMed 学术引擎**：生物医学文献权威数据库，国内直连
- **Google Scholar 学术引擎**：全学科学术搜索，需代理
- **MCP 工具拆分**：`smartsearch`（通用搜索）+ `academicsearch`（学术搜索）独立工具，`academicsearch` 支持 `engines` / `time_range` / `page` 参数
- **学术搜索并行化**：多引擎并发请求，结果按 URL 去重 + 分组归一化排序
- **BingFallback 配置**：`academic.bing_fallback` 控制学术搜索时是否用 Bing 兜底
- **proxy 配置**：仅海外学术引擎（Semantic Scholar、Google Scholar）走代理
- **CI 自动发版**：GitHub Actions workflow，tag 推送后自动构建 linux/windows 二进制并发布 Release，附带 SHA256 校验

### 重构
- **提取 server 包**：`RunServer`、admin handlers、引用计数逻辑从 `cmd/main.go` 提取到可导出的 `server` 包，支持作为 Go 模块嵌入
- **学术引擎独立模块**：新增 `pkg/academic`（6 个引擎独立实现）和 `pkg/antirobot`（共享引擎框架：Engine 接口、Searcher 编排器、限流器）
- **Bing 包精简**：`pkg/bing` 仅保留 Bing 通用搜索引擎 + 反爬逻辑

### 文档
- 新增 [docs/api.md](docs/api.md)：Go Module API 和 HTTP API 完整文档
- 新增 [docs/configuration.md](docs/configuration.md)：配置参考、默认值速查、环境变量覆盖
- README 全面重写：精简结构化，补充特性亮点、运维参考、排障指南

## 2026-05-23

### 新增
- **cleanfetch 网页抓取工具**：通过 Jina Reader API 获取指定 URL 的干净网页内容，降低反爬拦截风险
  - 仅在配置 `jina.api_key` 后注册，不影响现有功能
  - 对常见 HTTP 错误（403/404/429 等）返回简明中文提示
  - 新增 SSRF 防护：URL 协议校验、内网地址黑名单
  - 新增客户端超时（30s）防止 goroutine 泄漏

### 优化
- **学术搜索结果增强**：保留论文元数据（作者、DOI、类型），格式化时自动区分论文和网页结果
- **缓存系统改进**：
  - 支持 `academic` 参数区分，防止学术/非学术缓存混用
  - 数据库自动迁移，兼容旧版缓存
  - 查询优化：两步查询充分利用索引
- **站点屏蔽统一**：`black_list_host` 和 `bing.blocked` 自动合并，SearXNG 后端同步生效
- **字符串拼接优化**：`MergeContent` 改用 `strings.Builder`，复杂度从 O(n²) 降为 O(n)
- **排序优化**：`HybridSearchImpl` 冒泡排序改为 `sort.Slice`

### 修复
- 学术搜索失败时不再静默回退到通用搜索，返回明确错误信息
- Tavily 搜索正确使用 `exclude_domains` 过滤站点
- `describeHTTPError` 使用 `fmt.Sprintf` 替代不必要的 `fmt.Errorf`

---

## 2026-05-20

### 新增
- LLM 摘要未启用时，`smartsearch` 工具自动移除 `intent` 参数，节省客户端上下文 token
- MCP 服务增加 30s 心跳 + 5 分钟空闲 session 自动清理
- HTTP Server 增加超时配置（ReadHeader 10s / Read 60s / Idle 120s）
- 异步摘要 goroutine 增加 panic recover

### 修复
- Dockerfile 启动参数缺失导致容器立即退出

---

## 2026-05-15

### 新增
- `engine` 搜索模式：无需 API Key，使用 Bing 通用搜索 + 学术搜索引擎
- 学术搜索引擎集成：arXiv、Crossref、OpenAlex、Semantic Scholar
- MCP 工具新增 `academic` 参数
- `black_list_host` 屏蔽站点配置（对 Bing 和 Tavily 生效）

### 优化
- LLM 摘要提示词：主动过滤低质量内容、合并重复结果、保留关键原文并标注引用

---

## 2026-05-01

### 新增
- Tavily 搜索 API 支持
- LLM 摘要支持（建议使用快速模型）
- SQLite 缓存管理

---

## 2026-04-15

### 初始版本
- 百度千帆 AI Search API 支持
- 基础 MCP 服务框架
