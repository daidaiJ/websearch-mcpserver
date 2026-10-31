# 搜索模式、引擎与 MCP 工具

[English](search.en.md) | [中文](search.md)

## 目录

- [搜索模式](#搜索模式)
- [引擎对照](#引擎对照)
- [相关性评分](#相关性评分)
  - [通用搜索评分（Wigolo）](#通用搜索评分wigolo)
  - [MMR 多样性重排](#mmr-多样性重排)
  - [学术搜索评分](#学术搜索评分)
- [响应行为与可靠性](#响应行为与可靠性)
- [SmartSearch 高级配置](#smartsearch-高级配置)
- [Apipool 配置](#apipool-配置)
- [MCP 工具](#mcp-工具)
  - [`smartsearch` — 通用网络检索](#smartsearch--通用网络检索)
  - [`academicsearch` — 学术论文检索](#academicsearch--学术论文检索)
  - [`cleanfetch` — 网页内容抓取](#cleanfetch--网页内容抓取)
  - [`pdf_parser` — PDF 解析](#pdf_parser--pdf-解析)
  - [`file_search` — 本地文件快速检索](#file_search--本地文件快速检索)
- [学术搜索建议](#学术搜索建议)

---

## 搜索模式

| 模式 | 说明 | 需要 Key |
|------|------|----------|
| `engine` | 百度网页搜索 + Bing + 360（启用时）并发（代理可用时自动加入 DuckDuckGo / Wikipedia / Google News，启用时加入 Google；`github.enabled` 开启时加入 GitHub） | **无需** |
| `baidu` | 百度千帆搜索（`enable_ai_search` 控制端点），失败自动回退百度网页搜索；无 SK 时直接用百度网页搜索 | `BAIDU_SK`（可选） |
| `apipool` | API Key 池轮转：每次只调一个供应商，失败自动切换；支持 `round-robin` / `priority` / `weighted` 策略；网页引擎兜底 | 各 Key 可选 |
| `tavily` | Tavily Search API（[获取 Key](https://app.tavily.com/home)） | `TAVILY_SK` |
| `exa` | Exa Web Search API（[获取 Key](https://dashboard.exa.ai/api-keys)） | `EXA_API_KEY` |
| `anysearch` | AnySearch API（[获取 Key](https://www.anysearch.com/console/api-keys)）；无 Key 时走匿名档（免鉴权，按出口 IP 限流） | `ANYSEARCH_API_KEY`（可选） |
| `doubao` | 豆包联网搜索 Global / Custom（[获取 Key](https://console.volcengine.com/search-infinity/api-key)） | `DOUBAO_SEARCH_API_KEY` |
| `yuanbao` | 腾讯云元宝搜索（API KEY 方式；[文档](https://cloud.tencent.com/document/product/1806/130615)） | `TENCENTCLOUD_WSA_APIKEY` |
| `hybrid` | 全引擎混合（Anysearch + 百度智能搜索 + 百度网页搜索 + 360 + Wikipedia + Google News + GitHub(启用时) + Tavily + Exa + 豆包(有 Key 时) + 元宝(有 Key 时) + TinyFish(有 Key 时) + Keenable(有 Key 或显式开启时) + Bing + DuckDuckGo + Google） | 各 Key 可选 |
| `mcphub` | 零 Key 托管 MCP 级联：parallel → anysearch → exa → firecrawl，失败冷却后切下一个；单 server 可选 Key | **无需**（可选 Key） |

> 所有模式主引擎失败均自动回退。无 Key 时自动降级为 `engine`。`baidu`/`tavily`/`exa`/`anysearch`/`doubao`/`yuanbao` 均支持 `sk_list` 多 Key 轮询（同供应商重复 Key 自动去重），`sk_list` 为空时自动用 `api_key` 作为单元素列表。
>
> **推荐选型**：**零 Key 场景用 `mcphub`**（托管 MCP 级联，失败冷却 + 本地引擎/Bing 降级）；**有 API Key 场景用 `apipool`**（多供应商 Key 池轮转，失败自动切换，网页引擎兜底）。`engine` 适合只依赖本地网页引擎、不需要 MCP 的场景。

**各模式引擎映射**（来自 `pkg/search/factory.go`）：

| 模式 | 参与引擎 |
|------|----------|
| `engine` | 百度网页搜索 + Bing + 360（若启用）+ Google（若启用）+ DuckDuckGo / Wikipedia / Google News（若代理可用）+ GitHub（若启用），并发 |
| `baidu` | 百度千帆（`enable_ai_search` 控制端点）→ 失败回退百度网页搜索 |
| `tavily` | Tavily；无 Key 时回退 Bing |
| `exa` | Exa；无 Key 时回退 Bing |
| `anysearch` | AnySearch；有 Key 走 Key 池轮询，无 Key 走匿名档（免鉴权，按出口 IP 限流） |
| `doubao` | 豆包联网搜索 Global/Custom（`doubao.version`）；无 Key 时回退 Bing |
| `yuanbao` | 腾讯云元宝搜索（API KEY 方式）；本地月上限达到后按不可用处理并回退 Bing |
| `apipool` | 按配置顺序轮转 anysearch / baidu / tavily / exa（`doubao`/`yuanbao`/`tinyfish`/`keenable` 需显式写入 `apipool.engines`），网页引擎始终兜底 |
| `hybrid` | Anysearch + 百度智能搜索 + 百度网页搜索 + 360 + Wikipedia + Google News + GitHub(启用时) + Tavily + Exa + 豆包(有 Key 时) + 元宝(有 Key 时) + TinyFish(有 Key 时) + Keenable(有 Key 或显式开启时) + Bing + Google + DuckDuckGo，并发 |
| `mcphub` | 按 `mcp_hub.servers` 顺序级联托管 MCP（默认 parallel → anysearch → exa → firecrawl），一次请求命中一个；失败冷却后切换，全部失败回退 Bing |

---

## 引擎对照

**通用搜索引擎**：

| 配置名 | 引擎 | 回传 score | 需要代理 |
|--------|------|-----------|----------|
| `baidu` | 百度网页搜索（内置，`tn=json`） | ❌ | 否 |
| `bing` | Bing（内置） | ❌ | 否 |
| `duckduckgo` | DuckDuckGo | ❌ | 是（自动检测） |
| `google` | Google（默认禁用，被反爬拦截） | ❌ | 是 |
| `tavily_api` | Tavily Search API | ✅ | 否 |
| `exa` | Exa Web Search API | ❌ | 否 |
| `anysearch` | AnySearch API（内置本地黑名单过滤） | ❌ | 否 |
| `doubao` | 豆包联网搜索 Global/Custom API（内置本地黑名单过滤；Custom 回传 score） | ✅ | 否 |
| `yuanbao` | 腾讯云元宝搜索 API KEY 方式（内置本地黑名单过滤；回传 0~1 相关性分数） | ✅ | 否 |
| `baidu_api` | 百度千帆搜索（`enable_ai_search` 控制端点） | ❌ | 否 |
| `so360` | 360 搜索（HTML，中文第二索引；默认关闭，`so360.enabled` 开启） | ❌ | 否 |
| `wikipedia` | Wikipedia MediaWiki API（参考型来源；默认关闭，`wikipedia.enabled` 开启，`lang` 选语言版本） | ❌ | 是（自动检测） |
| `google_news` | Google News RSS（跳转链接回源发布方 URL，带结构化发布日期；默认关闭，`google_news.enabled` 开启，`edition` 选版本） | ❌ | 是（自动检测） |
| `github` | GitHub REST API 仓库搜索（零 Key 匿名档 search 10/min；配 PAT 提额 30/min；带结构化创建日期与 ★/语言元信息；默认关闭，`github.enabled` 开启，`github.api_key`/`token_list` 配 PAT；API 失败或零结果自动回退 Bing `site:github.com` 检索） | 否（PAT 可选） | 否 |

**学术搜索引擎**（无需 Key）：

| 引擎 | 说明 | 需要代理 |
|------|------|----------|
| `arxiv` | 预印本（CS/AI/物理） | 否 |
| `crossref` | 全学科 DOI 元数据 | 否 |
| `openalex` | 全学科开放学术图谱 | 否 |
| `pubmed` | 生物医学文献 | 否 |
| `europepmc` | Europe PMC（生物医学 / PubMed 增补源，含全文） | 否 |
| `dblp` | DBLP（CS 会议/期刊索引） | 否 |
| `doaj` | DOAJ（开放获取期刊目录） | 否 |
| `semantic_scholar` | 语义学术（默认禁用） | 是（自动检测） |
| `google_scholar` | 全学科学术搜索（默认禁用） | 是（自动检测） |

> **网络可用性**：Google / DuckDuckGo / Crossref / Google Scholar 在 `network: china` 且无代理时不稳定（可能超时或被反爬拦截）；Bing 网页抓取亦可能超时。失败引擎自动跳过/回退，不影响其他引擎结果；部分学术引擎失败会在结果末尾提示「部分引擎本次失败，结果可能不完整」。

---

## 相关性评分

### 通用搜索评分（Wigolo）

多引擎结果经以下管线处理（纯启发式，无需 AI 模型）：

1. **RRF 融合排名**（Reciprocal Rank Fusion, K=60）— 多引擎结果按排名融合
2. **词汇对齐** — 查询词与结果标题/内容的词级匹配
3. **稀有词 / 短语连续匹配** — 稀有词和连续短语命中加权
4. **域名品质惩罚** — 品牌 / 电商 / 词典站误匹配降权
5. **共识 / 权威 / 时效加分** — 多引擎共识、权威站点、时效性
6. **全局低分阈值过滤** — `final_score < relevance_threshold`（默认 0.05）的结果被丢弃，仅保留 Top-1（并保证最少 2 条）

配置项：`smartsearch.enhance`（默认 true）、`smartsearch.relevance_threshold`（默认 0.05）。

### MMR 多样性重排

评分过滤之后、`max_size` 截断之前执行贪心 MMR 重排（Token Jaccard 相似度），打散同一话题的高相似结果（转载站 / 镜像站 / 同源博客），Top-1 保护。

```yaml
smartsearch:
  mmr:
    enabled: true      # 总开关（默认 true）
    lambda: 0.7        # 相关性权重 [0,1]，越高越偏相关性，越低越偏多样性（默认 0.7）
    target_count: 0    # MMR 后目标条数，0 = 不额外截断（由 max_size 统一截断）
```

### 学术搜索评分

九大学术引擎结果经 RRF 融合排名，叠加学术特有信号：

- **引用数**（对数压缩，clamp [1.0, 1.7]）
- **高影响力期刊 / 会议**加分
- **PDF 全文可用性**
- **新鲜度因子**（时间敏感查询时近 1 年 ×1.15）

低分论文自动过滤（Top-1 + 每引擎保底）。配置项：`academic.enhance`（默认 true）、`academic.threshold`（默认 0.02），独立于 smartsearch。

---

## 响应行为与可靠性

一至三期引入的响应行为，`smartsearch` 与 `academicsearch` 同口径（HTTP MCP 端点同样生效）：

**失败清单（空结果可归因）**：主引擎失败不再静默回退，响应末尾附结构化失败清单——引擎名、失败类型（`timeout` / `rate_limit`（含剩余冷却秒数）/ `challenge` / `off_topic` / `error`）与短原因；被墙/熔断引擎带原因与预计恢复时间。失败清单不写入缓存、不受任何预算或落盘裁剪，与学术搜索逐引擎错误透传统一。

**请求取消与单引擎软超时**：MCP 请求的 context 全程下传搜索编排层与各引擎的 HTTP 请求，客户端断连或请求死线到达即停止等待，不再由后台慢引擎拖住整次调用。`smartsearch.engine_timeout_sec`（默认 10 秒）为单引擎软超时：超时引擎按 `timeout` 进失败清单，其余引擎结果照常返回；旧式引擎实现同样受此保护（放弃等待，不在请求路径上继续阻塞）。

**过滤诊断（filter_diagnostics）**：过滤后结果 ≤3 条时，附各过滤器（全局阈值 / MMR / per-engine score 等）的丢弃计数与放宽提示，用于区分"没搜到"与"被过滤"。

**off-topic 整桶守卫（`smartsearch.off_topic_guard`）**：某引擎整桶与查询词几乎无词汇对齐（HTTP 200 诱饵页特征）时判 `off_topic` 进失败清单。三态：`shadow`（默认，只记录不丢桶，为阈值校准积累数据）/ `enforce`（真丢桶）/ `off`。

**结果来源注记**：结果日期带 `date_source` 三态——`structured`（引擎 API 给出）/ `snippet`（摘要文本解析，弱）/ `undated`；学术结果同口径。响应头带 `retrieved_at`（检索时间，缓存命中时为原检索时间）与 `cache_age_seconds`（缓存年龄），并附固定 `usage_note`：snippet 日期仅用于定位来源，引用日期、金额、版本前请打开原页核对。

**响应超限落盘**：渲染结果超 `smartsearch.inline_max_chars`（默认 32768，负数 = 禁用）时整体写入 `fetchdata/search-*.md`（惰性清理 7 天旧文件），响应保留溯源头、统计、每条候选的标题/URL/来源/最多 160 字符摘录、文件路径和 `result_ref`；完整正文用 `cleanfetch` 分段读取，失败清单保留在初始响应。实时与普通缓存命中路径共用，缓存命中生成新引用。已落盘的补抓结果通过结构化状态识别，不进入持久缓存；普通正文包含文件路径文案不影响缓存。网页键采用 `web:v3:`，旧网页缓存不复用、按原 TTL 清理；首次查询可能冷缓存，学术缓存不变。

**freshness 感知缓存 TTL**：smartsearch `time_range` ≤1 月 → 24h，其余默认 6h；学术 `time_range` 分桶 day → 1h / week → 6h（默认）/ month → 24h。时效敏感查询不再命中陈旧缓存。

**网页缓存隔离**：smartsearch 按 query、生效的 `time_range` 和 `fetch_top_n` 分开缓存；正文补抓仅复用相同时间范围的结果，摘要也保留对应缓存键。省略或 null 为 3 个月，显式 `0` 不发送时间筛选；各请求的时间参数互不修改，普通供应商 SearchRaw 保留构造默认。升级后不复用缺少时间范围的旧网页缓存，旧记录保留并按原策略清理。时间筛选的实际精度仍取决于上游来源：部分供应商用离散枚举，部分网页引擎及 Bing 回退无此参数；不保证上游没有隐藏默认或每条结果具有准确日期。

**熔断状态落盘**：引擎冷却/限流统一注册表并落盘 `engine_health.json`（缓存库同目录，24h 记忆窗，advisory 语义），进程重启后自动采纳未过期冷却，避免反复撞死引擎。当前覆盖 DuckDuckGo / arXiv。

**运行状态 Resource**：`search://capabilities`（版本 / 引擎清单 / 能力）与 `search://health`（引擎健康 / 熔断 / 缓存命中）以只读 MCP Resource 暴露，不占工具槽位、响应不带任何密钥；`mcp_resources: false` 整体关闭（默认开启）。

**off-topic 校准回放**：`shadow` 模式下的守卫判定会以 `guard` 事件落入遥测库（只存模式、参照回声率与各引擎桶的「回声率/条数」，不含 URL 与原文）。`go run ./tools/guardreplay -db <dashboard.db>` 按 0.15/0.25/0.35/0.5 等候选阈值回放，输出「若 enforce 会丢弃哪些桶、影响多少查询」与样例，用于把阈值从初值校准到实测值；`-json` 输出机器可读结果。

**Prometheus 观测**：`GET /metrics` 以标准文本格式暴露今日工具/供应商请求数、成功失败、缓存命中与延迟分桶（只读、脱敏，来自遥测库；需 `dashboard.enabled: true`）。详见 [docs/api.md](api.md#观测端点)。

---

## SmartSearch 高级配置

`smartsearch` 节控制搜索结果的过滤、截断和输出格式：

```yaml
smartsearch:
  max_size: 10           # 全局最大结果数（按 score 排序后截断），0 = 不限
  fetch_top_n: 0         # 服务端默认抓取正文条数（agent 未传 fetch_top_n 时生效），默认 0 = 与旧版一致不抓取；
                         # 设 1-5 = agent 未传参时也默认获取正文（API 引擎走原文传参快速路径，网页引擎内部抓取）
  show_meta: true        # 输出中显示引擎来源和相关性分数（默认 true）
  enhance: true          # 本地评分增强（RRF 融合 + 词汇对齐 + 域名品质 + 多层 Boost + 阀值过滤），默认 true
  relevance_threshold: 0.05  # 增强后相关性阀值，低于此值过滤（Top-1 保护），默认 0.05
  inline_max_chars: 32768    # 内联渲染上限；超限落盘（0=默认，负数=禁用落盘）
  off_topic_guard: shadow    # off-topic 整桶守卫：shadow（默认只记录）/ enforce / off
  engine_timeout_sec: 10     # 单引擎软超时（秒），超时引擎按 timeout 进失败清单；负数 = 只依赖请求死线
  mmr:                       # MMR 多样性重排（打散同主题高相似结果）
    enabled: true            # 总开关（默认 true）
    lambda: 0.7              # 相关性权重 [0,1]，越高越偏相关性，越低越偏多样性（默认 0.7）
    target_count: 0          # MMR 后目标条数，0 = 不额外截断（由 max_size 统一截断）
  engines:
    tavily_api:        # Tavily API（回传 score，可设 min_score）
      min_score: 0.5   # 最低相关性分数阈值，0 = 不过滤
      max_size: 6      # 单引擎最大结果数（默认 4）
      weight: 1.0      # 引擎权重，影响 RRF 融合分（enhance=true 时生效），0 = 默认 1.0
    bing:              # Bing（不回传 score，min_score 无效）
      max_size: 4
    baidu_api:         # 百度千帆搜索（不回传 score，enable_ai_search 控制端点）
      max_size: 5
    baidu:             # 百度网页搜索（不回传 score）
      max_size: 5
    google:            # Google（默认禁用，被反爬拦截）
      max_size: 4
    duckduckgo:        # DuckDuckGo（不回传 score，需代理）
      max_size: 4
    anysearch:         # AnySearch（不回传 score）
      max_size: 4
    doubao:            # 豆包联网搜索（Custom 回传 score；Global 不回传）
      min_score: 0
      max_size: 4
    yuanbao:           # 腾讯云元宝搜索（回传 0~1 相关性分数）
      min_score: 0
      max_size: 4
```

**Score 过滤逻辑**：
- 引擎回传 score 时：按 `min_score` 过滤，保留 `max_size` 条
- 引擎不回传 score 时：忽略 `min_score`，取 `min(max_size, ⌈global_max_size / 引擎数⌉)` 截断
- 全局 `max_size`：有 score 时按 score 排序截断，无 score 时按引擎轮询均匀分配

---

## Apipool 配置

`apipool` 节控制 `mode: apipool` 时的供应商选择策略、优先级顺序和权重：

```yaml
apipool:
  strategy: weighted      # round-robin（默认）/ priority / weighted
  engines:                # 供应商优先级顺序（默认 [anysearch, baidu, tavily, exa]）
    - anysearch
    - baidu
    - tavily
    - exa
    # - doubao            # 不在默认列表；有 Key 时显式加入
    # - yuanbao           # 不在默认列表；有 Key 时显式加入
  weights:                # weighted 策略权重（单 Key 权重；默认值见下）
    anysearch: 30000
    baidu: 1500
    tavily: 1200
    exa: 1200
    doubao: 500           # 免费档每月默认 500 积分
    yuanbao: 0            # 后付费源默认不参与加权起始；显式配正数才会被加权选中
```

**策略说明**：
- **`round-robin`**（默认）：跨请求轮转起始供应商，每次请求内先用完当前供应商所有可用 SK 再 fallback 到下一个
- **`priority`**：始终从列表第一个供应商开始，用完所有 SK → 切换下一个供应商 → 网页引擎兜底
- **`weighted`**：按权重加权随机选起始供应商，突发请求天然分散到多家供应商。供应商有效权重 = **配置权重 × 当前可用 SK 数**（SK 失效冷却后权重自动下降，自愈）；权重表未收录的供应商按 1 计；显式 `0` 表示不参与加权起始选择（仍保留在失败切换链路）；全部权重为 0 时退化为 round-robin。网页引擎兜底无 Key 池，固定权重 1

**权重默认值**（可被 `apipool.weights` 覆盖）：`anysearch=30000`、`baidu=1500`、`tavily=1200`、`exa=1200`、`doubao=500`（对齐免费档每月积分）、`tinyfish=1200`（免费搜索 12000 次/天）、`keenable=800`（public 免 Key 档按 IP 限流）

**工作流程**：选供应商 → `pool.Next()` 取 key → 调 API → 成功返回 / 失败标记 key 冷却 30 分钟 → 同供应商下一个 SK 重试 → 全部耗尽切下一个供应商 → 全部失败用网页引擎兜底

---

## MCP Hub 零 Key 级联

`mode: mcphub` 不要求 API Key：按 `mcp_hub.servers` 顺序调用托管 MCP server，单次请求命中一个；失败后该 server 进入冷却（默认 60s），自动级联到下一个。轮转池全部失败后按 `fallback_engines` 降级，最终由 `smartsearch` 回退 Bing（`bing.enabled=true` 时）。这是**路由/降级**，不会把多家结果合并后一起返回。

| server | MCP 端点 | Keyless | 代理 | 搜索 / 抓取工具 | Key 获取（可选） |
|--------|----------|---------|-----------|-----------------|------------------|
| `parallel` | `https://search.parallel.ai/mcp` | ✅ | ❌（直连） | `web_search` / `web_fetch` | https://platform.parallel.ai/ |
| `anysearch` | `https://api.anysearch.com/mcp` | ✅（按出口 IP 限流） | ❌（直连，国内友好） | `search` / `extract` | https://www.anysearch.com/console/api-keys |
| `exa` | `https://mcp.exa.ai/mcp` | ✅（低限流） | 有代理则走 | `web_search_exa` / `web_fetch_exa` | https://dashboard.exa.ai/api-keys |
| `firecrawl` | `https://mcp.firecrawl.dev/v2/mcp` | ✅（按 IP 每日限额） | 有代理则走 | `firecrawl_search` / `firecrawl_scrape` | https://www.firecrawl.dev/app/api-keys |

- **代理是可达性优化，不是准入条件**：`exa`、`firecrawl` 默认「有代理则走代理、无代理直连」——检测到代理时用代理，否则直连，不再因为「未检测到代理」被整轮跳过。每个 server 可用 `requires_proxy` 覆盖：`true` = 硬性要求代理、无代理则跳过，`false` = 强制直连；`parallel`、`anysearch` 恒直连。代理在运行中变得可用或不可用时，下一次请求即生效，无需重启。
- **Key 可选**：每个 server 的 `api_key` 或 `sk_list` 为空时走匿名 keyless；配置后自动带鉴权头并支持多 Key 轮转。鉴权失败（401/403/unauthorized/forbidden/invalid api key）只标记该 Key 无效（30 分钟）并换下一把，Key 池耗尽后才冷却该 server；连接/超时等其它错误直接冷却该 server 并级联，其余 Key 保持可用。Key 只写入本地配置，不会出现在日志或响应中。
- **2026-10-20 国内直连实测**（安徽联通）：四个端点均可直连；AnySearch 中文结果最稳定，Parallel 延迟最低，Exa 突发约 7 次后触发匿名限流（约 60s 恢复），Firecrawl 匿名额度按出口 IP 分配。代理标记按“国内网络友好度”默认给出（`exa`/`firecrawl` 默认有代理则走、无则直连），可按实际出口用 `requires_proxy` 覆盖。
- **策略**：`priority`（默认，稳定优先）每次从第一个可用 server 开始；`round-robin` 跨请求轮转起始 server。
- **本地引擎负载轮转**：`rotation_engines` 可把 `so360` / `bing` / `baidu_web` 等本地引擎追加到主轮转池（顺序在 MCP server 之后）。例如 `[so360]` 让约 1/5 的请求走 360，降低 MCP 匿名限流概率；想提高引擎占比就减少 `servers` 数量（只留 parallel + so360 即约 50/50）。
- **降级链**：主轮转池（MCP → `rotation_engines`）全部失败后，按 `fallback_engines` 顺序降级；默认 `[bing, so360]`（未启用的自动跳过；so360 默认关闭，默认安装通常只经 Bing 降级），显式 `[]` 表示不做本地降级。`fallback_engines` 也失败后，仍由 `smartsearch` 统一回退 Bing，与项目其它模式一致。
- **冷却**：连接/限流错误冷却对应后端；鉴权错误先在该 server 内换下一把 Key，Key 池耗尽后才冷却并级联。站点级“不支持抓取”只跳过当前 URL，不连坐整个 server。全部冷却时直接返回错误，由上层回退 Bing，避免无谓重试。
- **仅内置 preset**：`servers[].name` 只接受 `parallel` / `anysearch` / `exa` / `firecrawl`。请求参数与响应解析随 preset 固定；新增上游必须在 `pkg/search/provider/mcphub` 内补 preset、参数构造和解析器，不能靠配置拼接（不同 MCP 的请求/响应结构并不统一）。
- **时间范围**：`smartsearch.time_range` 在 `mcphub` 下不发送（四家 MCP 的时间参数不统一）；需要时间过滤时用 `engine`/`apipool` 的原生引擎或直接读原页。

```yaml
mode: mcphub
mcp_hub:
  enabled: true
  strategy: priority        # priority（默认）/ round-robin
  timeout_sec: 20           # 单次搜索超时
  fetch_timeout_sec: 45     # 单次抓取超时
  cooldown_sec: 60          # 失败冷却
  max_results: 10
  max_fetch_chars: 200000
  fetch_mode: off           # off（默认）/ fallback / prefer
  rotation_engines: []      # 加入主轮转池的本地引擎，如 [so360]；MCP 之后轮转
  fallback_engines: [bing, so360]  # 主轮转失败后的降级；[] = 不降级，最终仍回退 Bing
  servers:                  # 省略即默认四家
    - name: parallel
    - name: anysearch
    - name: exa
    - name: firecrawl
```

### `mcp_hub.fetch_mode` — 让 cleanfetch 也走托管 MCP

| 值 | 行为 |
|----|------|
| `off`（默认） | 保持原有 cleanfetch：webfetch → Jina Reader；不使用 MCP |
| `fallback` | 先走 webfetch → Jina；两者都失败后，再按级联顺序尝试 MCP 抓取 |
| `prefer` | 先按级联顺序尝试 MCP 抓取；失败后降级到 webfetch → Jina |

降级与安全约束：

- **两层都失败**时返回聚合错误（MCP 与本地错误同时保留），便于定位。
- **PDF 不走 MCP**：URL 以 `.pdf` 结尾时始终走本地 PDF/MinerU 链路，避免丢失分页与 OCR 能力。
- **预检不跳过**：MCP 抓取前仍执行 `validateURLSecurity`（私网/DNS rebinding）与 `headCheck`（大小上限）。
- **输出上限**：单个网页最多返回 `mcp_hub.max_fetch_chars` 字符，超出截断并注明原长度。
- **独立冷却**：搜索与抓取的冷却状态分开记录；抓取站点级错误不会让该 server 的搜索一起冷却。`mode=mcphub` 且 `fetch_mode` 非 off 时，cleanfetch 复用搜索 Hub，Key 池与冷却表共用。
- **抓取需要 `cleanfetch.enabled=true`**；`fetch_mode` 仅增强 URL 抓取，不影响 `result_ref` 读取。

---

## MCP 工具

> 工具注册条件：`smartsearch` 需 `bing.enabled=true`；`academicsearch` 需 `academic.enabled=true`；`cleanfetch` 始终注册，`cleanfetch.enabled` 仅控制 URL 抓取；`pdf_parser` 需 `pdf_parser.enabled=true`；`file_search` 无开关——首个 MCP 客户端接入时探测 Everything HTTP Server，通过即注册，探测不过不暴露。

### `smartsearch` — 通用网络检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词 |
| `intent` | string | ❌ | 搜索意图（仅 LLM 启用时生效，未启用时自动移除该参数节省上下文） |
| `time_range` | int | ❌ | 搜索时间范围（月），默认 3。`1`=近 1 个月，`6`=近半年，`12`=近一年，`0`=不限。豆包 Custom 会映射为 `OneDay`/`OneWeek`/`OneMonth`/`OneYear`；元宝映射为 `dN`/`mN`/`yN`；豆包 Global 无时间过滤接口，忽略 |
| `fetch_top_n` | int | ❌ | 正文抓取模式：不传时与旧版一致，只返回引擎/供应商自带内容（服务端 `smartsearch.fetch_top_n` 可改默认，默认 `0` 不抓）；传 `0` 表示只要标题摘要和 URL；传 `1-5` 表示为前 N 条获取页面原文——支持原文传参的 API 引擎（Tavily/Exa/豆包）直接走快速路径，网页引擎内部抓取（无需 `cleanfetch.enabled`，webfetch 惰性初始化）；已有足量正文的条目自动跳过；抓取被反爬防护（JS 挑战/WAF）拦截时在该条结果上显式标注 |

返回结果默认附带来源引擎和相关性分数（Tavily / 豆包 Custom 等支持 score 的引擎）。可通过 `smartsearch.show_meta: false` 关闭。

**LLM 摘要**：配置 `llm` 节后，`smartsearch` 支持 `intent` 参数并生成结构化摘要；摘要阶段通过 MCP progress notification 逐 token 流式推送生成过程，客户端断开自动取消，失败自动回退非流式摘要。

**适用边界**：时效性事实、网页线索与文档检索；系统性文献综述、需要引用数据的学术问题请改用 `academicsearch`。

### `search_history` — 本地搜索历史（试验性，默认关闭）

> **试验性（idea）**：实用性尚未确认，先默认关闭供体验；真机使用确认有价值后再转正式工具。

`search_history.enabled: true` 启用后注册。FTS5 全文检索本服务自己记录的搜索历史：每次 `smartsearch` 成功后自动写入 query + 结果快照（标题/URL/截断摘要，最多 20 条/次），让模型复用已查结果而不必重抓。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ❌ | 历史检索关键词（FTS5 匹配查询与结果标题/摘要）；省略或空 = 返回最近历史（按时间倒序） |
| `limit` | int | ❌ | 返回条数上限（默认 10，硬上限 50） |
| `offset` | int | ❌ | 翻页偏移量（默认 0） |

数据只落本地 SQLite（`search_history.storage_path`，默认 exe 同目录 `history/search-history.db`），不出网、不含密钥；保留期 `search_history.retention_days` 默认 30 天，过期每小时清理。正文快照是截断摘要，完整正文请对对应 URL 用 `cleanfetch` 重取。

### `academicsearch` — 学术论文检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词；也可直接传入 DOI 或 arXiv id 走单篇精确查询（见下） |
| `engines` | []string | ❌ | 引擎子集：`arxiv` `crossref` `openalex` `pubmed` `europepmc` `dblp` `doaj` `semantic_scholar` `google_scholar` |
| `time_range` | string | ❌ | `year` / `month` / `week` / `day` |
| `page` | int | ❌ | 页码，默认 1 |

带 `time_range` 时各学术引擎走官方语法（v3.4.0 已对齐，不再把 Crossref / DOAJ / arXiv 打成上游失败）：

| 引擎 | 语法 |
|------|------|
| Crossref | `filter=from-pub-date:YYYY-MM-DD` |
| DOAJ | `bibjson.year:[起始年 TO 当前 UTC 年]`（禁止 `*`；`day`/`week`/`month` 退化成年粒度） |
| arXiv | `submittedDate:[YYYYMMDDHHMM TO YYYYMMDDHHMM]`（UTC/GMT） |

**单篇精确查询（DOI / arXiv id 短路）**：已持有 DOI 或 arXiv id 时直接将其作为 `query`，如 `10.1038/s41586-020-2649-2`、`doi:10.1038/s41586-020-2649-2`、`https://doi.org/10.1038/s41586-020-2649-2`，或 `2401.04085`、`arXiv:2401.04085`、`https://arxiv.org/abs/2401.04085`。此时走单篇精确查询并忽略 `time_range` / `page`，仍遵守已启用来源与指定 `engines`；拿到结果中的 `pdf_url` 后，可将该 URL 作为 `pdf_parser` 工具的 `path` 参数解析全文，不要再用标题做关键词搜索。

结果按学术评分增强排序（默认开启）：RRF 融合排名 + 引用数 / 期刊权威 / PDF 全文 / 新鲜度信号，低分论文自动过滤（Top-1 + 每引擎保底）。配置项：`academic.enhance`（默认 true）、`academic.threshold`（默认 0.02）。

无可用结果且存在引擎失败时，返回含逐引擎原因的失败提示，与正常空结果区分。DBLP 的已识别 Anubis HTML 挑战页报告人机验证，未识别的异常响应仍保留解析错误；不会自动绕过验证或变更启用配置。

**适用边界**：论文、预印本与 DOI 级学术检索；新闻、产品文档等通用网页信息请改用 `smartsearch`。

### `cleanfetch` — 网页内容抓取

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | URL 模式与 `urls` 至少一者 | 网页 URL，与 `result_ref` 互斥 |
| `urls` | string[] | URL 模式与 `url` 至少一者 | 批量抓取，与 `url` 合并去重后最多 5 个 |
| `result_ref` | string | 引用模式必填 | 原工具返回的引用，与 `url`/`urls` 互斥 |
| `offset` | integer | 否 | 仅引用模式，Unicode 偏移，默认 0 |
| `limit` | integer | 否 | 仅引用模式，默认/0 为 4096，上限 8192 |

URL 抓取需配置 `cleanfetch.enabled: true`；引用读取不受此开关限制。基于 go-webfetch，无需代理；内置 DNS rebinding 防护和 HEAD 预检防大文件（`max_fetch_size_mb` 控制阈值，默认 10MB；重定向逐跳复跑私网/metadata 校验，最多 5 跳）；失败时自动回退 Jina Reader（需配置 `jina.api_key`，代理自动检测）。

**站点特判**：`github.com` 页面 HTML 是 JS 渲染、常规抓取拿不到正文，命中即改走 GitHub REST API（不做 HEAD 预检）——仓库根输出元数据（描述/★/语言/许可证/最近推送）+ README 原文；`blob` 页走 contents API 取源文件（`Accept: application/vnd.github.raw`，不走国内不可达的 raw.githubusercontent CDN）；`issues`/`pull` 等动态页不在接管范围。API 失败（限流/403/未接管路径）自动回落常规 webfetch → Jina Reader 抓取链。API 匿名档 60 次/小时，配置 `github.api_key`（或 `token_list`）后提额 5000 次/小时，与 GitHub 搜索引擎共用同一批 token。

批量模式（`urls`）下每条 URL 独立预检与抓取，单条失败不影响其它，结果按 URL 分节返回；只传 `url` 时输出与旧版一致。

**适用边界**：抓取已知 URL 的正文；发现新来源请先用 `smartsearch`（本工具不做检索）。

### `pdf_parser` — PDF 解析

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | 新任务必填 | 本地 PDF 文件路径或远程 http(s) URL（学术结果的 `pdf_url` 可直接传入）；与 `task_ref` 互斥 |
| `pages` | string | ❌ | 页码范围（1-based），如 `1-10`、`1,3,5-7`；非法格式报参数错误，显式页数超过 `max_pages` 或单个区间宽度超过 1000 页时报错并要求拆分 |
| `task_ref` | string | ❌ | 查询/取回已有 MinerU 任务：`standard:<task_id>`、`batch:<batch_id>` 或 `agent:<task_id>`。省略 `path/pages`，保留原任务选页 |
| `wait_seconds` | integer | ❌ | 0-1800 秒，每个 MinerU 任务的等待上限；新任务默认 300，续查默认 0（只查一次） |

需配置 `pdf_parser.enabled: true`。大文档自动存储到临时文件。远程 URL 走与 `cleanfetch` 相同的 SSRF 预检和 HEAD 预检（含重定向逐跳复查），不会被拼成 `file://`。

**MinerU 等待与续查**：轮询间隔从 2 秒逐步拉长，最多 30 秒；有 progress token 时推送状态与已知页进度。等待上限耗尽只表示本次停止等待，不代表 MinerU 失败。文本回退同时保留最后状态及 `task_ref`；需要精准结果时调用 `{"task_ref":"standard:<返回的任务ID>"}`，仍处理中则返回状态而非正文，完成后取回原任务结果，不重新提交 PDF。可明确设置 `wait_seconds:900` 延长等待。上传批次的续查只返回该批，页码沿用裁切后的编号；其他批次仍按原文页码另行请求。未提供页进度时不推算总页数。

省略 `pages` 时请求前 `pdf_parser.max_pages`（默认 20）页。PDF 文本提取会报告已知总页数和截断情况；MinerU 不保证返回原文件总页数。远程 MinerU 精准 API 优先使用 `page_ranges`（单任务页数上限默认 600，对齐 MinerU 官方限制，可用 `pdf_parser.mineru_page_limit` 调整）；MinerU 对原件页数超限或源 URL 被拒会直接拒绝任务，此时服务下载原件，用 qpdf 裁出所需页面并上传子 PDF；主动请求超出单任务上限的页码（需调大 `pdf_parser.max_pages`，默认 20 会先拦下超宽请求）同样走该路径（原件下载上限 200MB，子文件仍须符合 MinerU 的 200MB / 600 页限制）。`pdf_parser.mineru_page_batch_size`（10-200，0=不分批）开启后超出单任务上限的页码会切成多批串行提交，累计页数受 `pdf_parser.mineru_page_budget` 预算约束，预算耗尽返回已解析部分并提示续读页码。分批全程经 MCP progress notification 推送逐批进度（下载原件、每批开始/完成），agent 无需整批盲等。结果会说明原页数与重新编号，并给出完整 ZIP 地址；Markdown 中的 `images/` 相对路径须与 ZIP 中图片一起使用。扫描件 OCR 的 Agent 轻量 API 会先裁出所需页面再上传，单次最多 20 页且子文件最多 10MB；它只提供 Markdown，不能保证相对图片资源可取回。独立运行二进制需安装 qpdf，项目 Docker 镜像已包含。

**解析策略**：本地 PDF 优先用 PDF 库（ledongthuc/pdf）提取文本；无文本层时若开启 `mineru_ocr` 再回退 MinerU OCR。
- `mineru_ocr: true`：扫描件 / 图片型 PDF 的 OCR 回退（Agent 轻量 API，≤10MB、单次最多 20 页）
- `mineru_token`：远程 URL 精准解析 API（≤200MB/600页）；也可与 OCR 回退共用
- 获取 Token：https://mineru.net/apiManage
- 环境变量：`MINERU_TOKEN`

**适用边界**：仅解析 PDF；网页正文抓取请用 `cleanfetch`。

---

#### cleanfetch 引用读取

始终注册，只接受本服务返回的 `result_ref`。调用 `cleanfetch(result_ref, offset=0, limit=4096)`，按 `next_offset` 续读到 `eof=true`；单次最多 8192 Unicode 字符，正文在 `content`。无需 Docker 或宿主文件权限，不重新请求来源；沿用 `/mcp` 鉴权。引用跨 MCP 会话有效，重启、过期/清理或检测到文件身份/大小/修改时间变化后失效。完整契约见 [API 文档](api.md#长结果读取)。

### `file_search` — 本地文件快速检索

基于 Windows 下 [Everything (voidtools)](https://www.voidtools.com/) 的 HTTP Server 索引做本地文件名/路径检索（毫秒级、只读，不读文件内容）。仅 Windows 提供；**任何 Linux 发行版不建议启用，除非 WSL**（显式配置 `everything.url` 指向 Windows 宿主，非 Windows 且未配置 url 时不探测、工具不暴露）。 启用/加固的分版本操作指导（1.4 内建 vs 1.5a 插件、ini 陷阱、curl 验证、加固清单）见 [skills/everything-http-server](../skills/everything-http-server/SKILL.md)，agent 可直接调用 `/everything-http-server`。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 检索词，支持 Everything 语法：`factory`、`*.go`、`ext:pdf report`、`dm:lastweek`、`size:>1mb` 等；正则请用 `match_regex` |
| `folder` | string | ❌ | 限定检索目录，接受 Windows（`D:\CODE\ai`）或 Git Bash（`/d/code/ai`）风格；配置白名单时必须落在白名单内，省略则在白名单全部目录内检索 |
| `match_case` | bool | ❌ | 区分大小写（透传 Everything `i` 参数） |
| `whole_word` | bool | ❌ | 全字匹配（`w` 参数） |
| `match_regex` | bool | ❌ | 正则检索（以 `regex:` 函数拼入，不影响目录限定） |
| `match_diacritics` | bool | ❌ | 区分变音符号（`m` 参数） |
| `exclude` | []string | ❌ | 排除项，每条作为一个 Everything NOT 词（含空格自动加引号）：`["\obj\", "
ode_modules\"]`；带首尾反斜杠匹配路径片段才不误伤文件名 |
| `min_alignment` | number | ❌ | 词汇对齐阈值（0~1），覆盖服务端 `everything.min_alignment`；结果过多时建议 0.3 起步，0 = 只重排不过滤 |
| `max_results` | int | ❌ | 单页返回条数（默认 10，服务端 `everything.max_results` 可改默认值；硬上限 20，超过按 20 收敛并在响应内提示） |
| `page` | int | ❌ | 页码（默认 1，上限 100），每页条数由 `max_results` 决定：翻页在二次过滤后的结果集上连续切片，页与页之间不重复不跳条 |
| `sort` | string | ❌ | `name`（默认）/ `date_modified` / `size` / `path` |
| `descending` | bool | ❌ | 降序（配合 `sort`） |
| `time_format` | string | ❌ | `datetime`（默认 `2026-01-02 15:04:05`）/ `iso`（ISO 8601 UTC）/ `filetime`（原始 FILETIME） |

**注册条件（自动探测，无 enabled 开关）**：首个 MCP 客户端接入时探测 `everything.url`（延迟探测，避免 websearch 与 Everything 双自启动的时序竞争导致工具缺失），连通且鉴权通过才注册该工具；Everything 未运行、HTTP Server 未启用或鉴权失败时静默不暴露，不影响其它工具。

**克制约束**：配置 `everything.roots`（目录白名单）后所有检索强制限定在白名单内，白名单外目录直接报错，防越界。**二次过滤（agent cost 优化）**：服务端按 3 倍超采候选（硬上限 600），`sort` 未显式指定时本地按文件名/路径词汇对齐重排（复用 smartsearch 评分管线的分词与停用词；显式指定 `sort` 则尊重服务端排序，仅保留阈值过滤），命中 `node_modules`/`.git`/`target` 等噪声目录的结果排序减半（`everything.noise_dirs` 可覆盖），`everything.min_alignment` 阈值可丢弃弱匹配——每条结果一行，弱相关不进上下文。**分页保护上下文**：单页条数封顶 20（默认 10，`max_results` 可调小但调大不生效），需要更多结果用 `page` 翻页；翻页按页码加深候选超采（`page × 单页条数 × 3`，硬上限 600），在过滤后的结果集上连续切片；候选池触顶后响应会说明无法再深翻，提示收窄 query。

## 学术搜索建议

- 医学/生物 → `pubmed` + `europepmc`；CS/AI → `arxiv` + `semantic_scholar` + `dblp`；开放获取 → `doaj`；全学科 → `crossref` + `openalex`
- 国内保持 `network: china`，海外引擎自动跳过（Europe PMC / DBLP / DOAJ 可直连）
- Semantic Scholar / Google Scholar 默认禁用，设置 `disable_semantic_scholar: false` / `disable_google_scholar: false` 即可启用，代理自动检测
