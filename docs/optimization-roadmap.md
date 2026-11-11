# 后续优化增强方向（Todo 专题）

> 2026-10-19 调研产出的路线图。首批八项已于同日落地并推送（`b780769`），本文件收敛为**剩余事项清单**；已完成项的落地要点见文末归档表，详细实现见对应提交与 CHANGELOG。
>
> 总评：项目已过功能建设期，投入方向为四条线——搜索链路的 context 化与并发正确性、结果去重与守卫校准的质量收尾、MCP 协议新代际跟进、可观测性与测试护栏。零 Key + 反检测 + agent 成本控制是既有差异化定位，继续深耕而非扩面。
>
> 2026-11-07 的 DSH 生态同类评估（含 Go 二进制分发到 dsh 插件市场的路径调研）已把**代码侧改进参考**合并进下面的「DSH 生态调研衍生的改进参考」小节；原始草稿 `docs/plans/2026-11-07-dsh-ecosystem-review.md` 按 `.gitignore` 不入库，仅本地留档。该轮已落地的是 npm / dsh 插件分发、文档引导与 Trusted Publishing 发布链路，**别把「已发版」当成「改进项已完成」**；D 系列与 Q/T/E 系列有重叠，立项前先对齐。

## 待办

### 结果质量收尾

- [ ] **Q3. LLM 摘要请求无 token/预算控制**
  `pkg/llm/llm.go` 的 `ChatRequest` 只有 Model/Messages/Stream，无 `max_tokens`、温度透传；摘要 prompt 拼入结果的长度控制未见配置项。
  方向：配置项 + prompt 截断预算，减少快速供应商下的无谓 token 开销。配置变更须同步两份 `config.example.yaml` 与 docs。

### MCP 生态跟进

- [x] **N1. TinyFish Search/Fetch REST provider（2026-10-24 拍板并落地）**
  免费额度：搜索 12000 次/天（30 req/min）、fetch 1000 URL/天（150 URL/min，按成功 URL 计数），00:00 UTC 重置；仅 Key 档无匿名档。`pkg/search/provider/tinyfish.go`：GET `api.search.tinyfish.ai`（X-API-Key）+ POST `api.fetch.tinyfish.ai`；接入 apipool/hybrid 供应商池与 source switch（`tinyfish.enabled`）；fetch 能力接入 cleanfetch 远端抓取链。国内直连实测 200（1.8s），无需代理。
- [x] **N2. Keenable Search/Fetch REST provider（拍板并落地，测试 Key 实测通过）**
  免费 10 req/s；keyed 端点 + public 免 Key 端点（X-Keenable-Title，按 IP 限流不耗 credits，anysearch 匿名档同模式）。POST `api.keenable.ai/v1/search`（`mode: pro`、`max_results` 1-50）+ GET `/v1/fetch`（仅索引页，`live=true` 实抓）；接入供应商池、source switch（`keenable.enabled`）与 cleanfetch 远端抓取链；public 档默认不自动参与 hybrid（`PublicTierOptedIn`）。国内直连实测 200（3.0s）。
- [x] **N3. cleanfetch 远端抓取链扩展（拍板并落地；v3.10.3 顺序反转）**
  `fetch_mode` 非 off 时远端链按「mcphub 托管 MCP（会渲染 JS）→ 供应商 fetch API（TinyFish/Keenable）→ 本地 webfetch/Jina」顺序级联；`fetchPageWithFallback` 改链式逐级失败切换。初版把供应商 fetch API 排在前面（理由：优先消耗按 URL 计数的免费额度），实测其对客户端渲染页面只返回应用外壳（TinyFish 对 open.spotify.com 返回 56 字符空壳），而 `tryRemote` 取第一个非空结果即返回，等于把渲染能力挡在门外，故反转。
- [x] **N4. `search_history` FTS5 查询史工具（拍板并落地）**：`pkg/history` SQLite FTS5 虚表 + 触发器同步；独立保留期默认 30 天可配，每小时后台清理；MCP 只读工具 `search_history`（query/limit/offset）；**独立配置开关** `search_history.enabled` 默认关闭（不建库、不注册工具）；隐私红线：只存 query 与结果快照，不含密钥、不出网。
- [x] **N5. 输出契约测试（拍板并落地）**：`pkg/search/core/contract_test.go` 固化字段顺序、「空结果≠合法输入隐藏该行」、易变字段集中在 ProvenanceHeader；不改 ProvenanceHeader 置顶设计。
- [x] **N6. quota 多供应商额度感知（拍板并落地，2026-10-24 真机实测）**：仅接入有官方用量端点的供应商——TinyFish 接入 `pkg/quota`：官方 `GET api.search.tinyfish.ai/usage` 与 `api.fetch.tinyfish.ai/usage`（X-API-Key），`limit=1 + start_after=当日 00:00 UTC` 直读响应 `total`（真机实测 total 随过滤条件返回，无需翻页聚合），免费档上限按官方文档取值（搜索 12000/天、fetch 1000/天）；搜索主指标不受 fetch 查询失败影响，官方查询整体失败回退本地用量。智谱/Keenable 无公开用量端点不探测。
- [x] ~~智谱 webSearchPrime mcphub preset~~ **评审后不做**：调用会消耗 GLM Coding Plan 套餐额度，与零增量额度定位冲突（已从分支历史整体撤出）。
- [ ] **N7. GitHub 站点接入 + Reddit 整线放弃（2026-10-24 落地）**
  - **GitHub（落地）**：`pkg/search/engine/github` REST API 引擎——`api.github.com` 国内直连可达（无需代理）；零 Key 匿名档 search 10/min + PAT 提额 30/min（token 轮转 + 429/403 冷却，Retry-After / x-ratelimit-reset 双来源）；`github.enabled` 默认关闭（wikipedia 同口径），参与 engine/hybrid/mcphub。cleanfetch 站点特判：`pkg/fetch/sitefetch` 对 `github.com/<owner>/<repo>` 改走 API（元数据 + README raw），`blob` 页转 raw.githubusercontent 直取源文件（不耗 API 配额），token 与引擎同批（`github.api_key` / `token_list`，env `GITHUB_TOKEN`）。github.com HTML 是 JS 渲染抓不到，API 是唯一正道；code search 要求认证不做。
  - **Reddit（评审后不做，2026-10-24 用户拍板）**：官方 2026-09-30 公告进入关停周期——新公共 API 申请 2026-10-31 截止、RSS 2026-11-13 全线停止支持、未注册 app 2027-01-12 移除访问、公共 Data API 2027-03 终止。原方案（OAuth script app 100 QPM 优先 + RSS 兜底）两条通道均在死线上，建在将死的 API 上不值得；引擎失败虽可软降级，但整线收益期 < 5 个月。已完成的 OAuth/auth.ts 调研结论留档：script app 注册制（reddit.com/prefs/apps 自建）、client_credentials / password grant 均无 refresh token、过期静默重取（reddit-mcp-buddy src/core/auth.ts 模式）。上游若重新开放（Devvit/付费 API 出现普适免费档）再重立项目。
- 原 N-llm（LLM 摘要有界 deadline）已落地，见下方归档表。

- [ ] **E1. go-sdk v1.7.0 → v1.8.0（2026-07-28 新 spec：stateless core / extensions / elicitation / sampling / tasks）**
  对本项目有实际价值的两点：
  - **sampling**：摘要借客户端模型，把"自配 LLM key 才能摘要"的门槛降为可选项；
  - **elicitation**：查询意图不明确时向用户澄清（学术搜索 engines 选择是天然场景）。
  步骤：先升 SDK 跑回归，能力按"客户端支持才启用"的门控引入，与能力边界表的谨慎风格一致。

### 可观测性与测试护栏

- [ ] **T2. 引擎包测试密度**：`pkg/search/engine/{baidu,bing,google}` 各仅 1 个测试文件，解析函数（HTML→结构化结果）适合表驱动 + 固定 fixture 补齐。

### DSH 生态调研衍生的改进参考（2026-11-07）

> 横向评估 DeepSeek Harness 插件市场与开源搜索 MCP 后识别的**代码侧**方向。D1 / D2 / D5 已于 2026-11-11 落地（见勾选项与说明），D3 部分落地；其余仍未拍板。参照对象最强的是 `@fffangchenghao/dsh-web-search`（8 引擎 RRF + 20-query 离线评测集 + 81 组参数扫描）、`dsh-free-search`（20+ 引擎回退）、Wigolo（本地 rerank + citation）。本项目在零 Key + 9 学术源 + 单二进制上工程深度更高，短板集中在「质量闭环」而非「功能广度」。

- [x] **D1. 离线检索质量评测集（与 T2 合并做，2026-11-11 落地）**：`bench/quality` 固定 20 条中英 fixture（各 2 个预期域名 + 4 个干扰项，按两引擎不同排名回放），回放 `enhance.EnhanceResults` 输出逐 case 与聚合的 P@8 / R@8 / MRR@8；逐 case 断言命中数与首个相关结果排名，聚合门槛拦退化。零网络依赖。**边界**：fixture 为合成候选集，衡量评分与排序回归，不代表真实检索召回；真实引擎 HTML fixture 仍归 T2。
- [x] **D2. 失败模型哨兵化 + 命名错误码（2026-11-11 落地）**：新增叶子包 `pkg/errcode`（`timeout` / `rate_limit` / `challenge` / `blocked` / `access_denied` / `parse` / `no_result` / `network` / `off_topic`，支持 `errors.As` 穿透与 `Message` 换文案保错误链）；`core.ClassifyFailure`、`telemetry.ClassifyErrorKind`、`mcp.isAntiBotFailure` 改为错误码优先、中英关键词兜底；`antirobot.ErrLocalRateLimit`、`provider.KeyError`、`webfetch` 的 WAF/超时/空壳分类附码；`adapter/baidu_fallback.go` 双失败用 `errcode.Message` 保留主备原因。**遗留**：尚未给所有引擎/供应商的原始错误逐点附码，关键词兜底仍参与。
- [ ] **D3. 共享 per-request 预算与 abort 传播（部分落地 2026-11-11）**：`googlenews` / `wikipedia` 已补 `ContextEngine`；`hybrid` 软超时的包装 goroutine 仍存在，`cleanfetch` 抓取链尚无统一预算对象。剩余工作集中在 `hybrid` 与 `cleanfetch`，收益与并发风险需单独评估。
- [ ] **D4. citation ID 与证据包**：现有 `result_ref` + `cleanfetch(offset/next_offset/eof)` 分段读回比竞品完整，值得保留；缺稳定引用 ID、source span、cited evidence pack，以及把网页正文包进 `<untrusted-web-content>` 代码标记防提示注入。dsh research 模式明确要求「给有来源的结论」。
- [x] **D5. 工具面与 server instructions（2026-11-11 落地）**：`NewMCPServer` 在 `ServerOptions.Instructions` 下发中英双语工具选择纪律（发现 / 读正文 / 查论文 / 本地文件 / 历史），说明参数默认值与作用域；调用方显式设置时尊重其值。`skills/websearch-mcp/SKILL.md` 增补「参数纪律」。长度控制在数行，避免常驻系统提示膨胀。
- [ ] **D6. 垂直源 / 官方源窄查（只做 optional action）**：对标插件有 34 垂直源与 2.5s 官方源 lookup；本项目已有 9 学术引擎与 GitHub 特判，可只补包管理 registry + 官方发布源一路，**不要全量扩面**——与「深耕不扩面」定位冲突。
- [ ] **D7. 配置分层与热重载**：`config.go` 1,863 行 / 236 字段、`config.example.yaml` 559 行，dashboard 只能改白名单、不能热重载装配。引入 preset 分层（profile → bundle → patch）比继续堆字段划算。
- [ ] **D8. 实时指标与显式 DI**：`/metrics` 是从 SQLite 回读当天事件而非进程内 counter，全仓无 tracing；`mcp/options.go:93-108` 与 `searxng/handler.go` 的全局 service-locator 装配不支持多实例，`core.ShowMeta` 存在无锁读写。

**不跟**：Wigolo 的 1.5GB 本地模型 + 浏览器池（毁掉单二进制轻量定位）、10+ 工具面（与「客户端最多 5 工具」的省 token 设计冲突）、PTC 全套（可降维成 `search_many` 式批量 query 并发，与 E1 一起评估）。

## 已完成归档（2026-10-19）

| 项 | 落地要点 | 提交 |
|---|---|---|
| **R1** context 贯通 | 可选 `ContextSearcher` / `ContextTimeRanger` / `ContextAcademicSearcher` / `ContextDiagnosticsSearcher`，`SearchInf` 原签名不变；新配置 `smartsearch.engine_timeout_sec`（默认 10s，负数=只依赖请求死线） | `b780769` |
| **R2** 诊断 per-request | `core.DiagnosticsSearcher` 值传递，单槽降为兜底；并发请求不再互相覆盖失败清单 | `b780769` |
| **R3** 缓存并发读 | DSN 级 WAL / `busy_timeout` / `synchronous=NORMAL`，连接池 1→4；原实现只注释声明 WAL、从未执行 PRAGMA | `b780769` |
| **Q1** URL 归一化 | `antirobot.NormalizeURL`（core 转发）：scheme/host 小写、去 fragment、剥追踪参数、参数排序、去尾斜杠；接入两处 hybrid 去重键与 `DeduplicateResults` | `b780769` |
| **Q2** 守卫校准 | `guard` 遥测事件（脱敏）+ `go run ./tools/guardreplay -db <dashboard.db>` 阈值回放 | `b780769` |
| **O1** metrics 端点 | `GET /metrics` 零依赖手写 Prometheus 文本；只读脱敏，遥测未启用返回 503 | `b780769` |
| **T1** SSRF fuzz | 三个 Fuzz 目标覆盖 `validateURLSecurity` / `isPublicIP` / `isPrivateHostFast`，CI 跑 10s 短窗口 | `b780769` |
| **T3** CI 护栏 | `go test -race -short` 步骤 + golangci-lint v2 job（`--new-from-rev` 只拦新增代码） | `b780769` |
| **N-llm** LLM 调用有界 deadline | `llm.timeout_sec`（默认 60s，含流式全程）；调用方 ctx 更早 deadline 优先；超时按摘要失败回退原始结果。原拟「智谱 webSearchPrime mcphub preset」经评审不做——会消耗 GLM Coding Plan 套餐额度，与零增量额度定位冲突 | `01dd2fa` |

> 验证：`go build ./...`、`go vet ./...`、`go test ./...`（含网络集成）、`go test -race -short ./...`、golangci-lint（新增代码 0 issues）、actionlint 全绿；`/metrics` 已用真实二进制实机验证。

## 外部参照（2026-10 检索）

- 竞品对比（braintrust.dev 2026-09）：Parallel / You.com / Exa / TinyFish / Firecrawl / Brave Search API / Tavily——均为 API Key 商业服务；本项目的零 Key + 反检测 + 本地文件检索（Everything）是差异位，不建议追功能广度。
- MCP 2026-07-28 spec 已由 Cloudflare MCP v2、GitHub MCP Server 跟进落地；go-sdk v1.8.0 对应就绪。
