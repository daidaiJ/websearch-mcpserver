# 搜索模式、引擎与 MCP 工具

[English](search.en.md) | [中文](search.md)

## 目录

- [搜索模式](#搜索模式)
- [引擎对照](#引擎对照)
- [相关性评分](#相关性评分)
  - [通用搜索评分（Wigolo）](#通用搜索评分wigolo)
  - [MMR 多样性重排](#mmr-多样性重排)
  - [学术搜索评分](#学术搜索评分)
- [SmartSearch 高级配置](#smartsearch-高级配置)
- [Apipool 配置](#apipool-配置)
- [MCP 工具](#mcp-工具)
  - [`smartsearch` — 通用网络检索](#smartsearch--通用网络检索)
  - [`academicsearch` — 学术论文检索](#academicsearch--学术论文检索)
  - [`cleanfetch` — 网页内容抓取](#cleanfetch--网页内容抓取)
  - [`pdf_parser` — PDF 解析](#pdf_parser--pdf-解析)
- [学术搜索建议](#学术搜索建议)

---

## 搜索模式

| 模式 | 说明 | 需要 Key |
|------|------|----------|
| `engine` | 百度网页搜索 + Bing 并发（代理可用时自动加入 DuckDuckGo，启用时加入 Google） | **无需** |
| `baidu` | 百度千帆搜索（`enable_ai_search` 控制端点），失败自动回退百度网页搜索；无 SK 时直接用百度网页搜索 | `BAIDU_SK`（可选） |
| `apipool` | API Key 池轮转：每次只调一个供应商，失败自动切换；支持 `round-robin` / `priority` / `weighted` 策略；百度网页搜索兜底 | 各 Key 可选 |
| `tavily` | Tavily Search API | `TAVILY_SK` |
| `exa` | Exa Web Search API | `EXA_API_KEY` |
| `anysearch` | AnySearch API（[anysearch.com](https://www.anysearch.com/docs)） | `ANYSEARCH_API_KEY` |
| `hybrid` | 全引擎混合（Anysearch + 百度智能搜索 + 百度网页搜索 + Tavily + Exa + Bing + DuckDuckGo + Google） | 各 Key 可选 |

> 所有模式主引擎失败均自动回退。无 Key 时自动降级为 `engine`。`baidu`/`tavily`/`exa`/`anysearch` 均支持 `sk_list` 多 Key 轮询（同供应商重复 Key 自动去重），`sk_list` 为空时自动用 `api_key` 作为单元素列表。

**各模式引擎映射**（来自 `pkg/search/factory.go`）：

| 模式 | 参与引擎 |
|------|----------|
| `engine` | 百度网页搜索 + Bing + Google（若启用）+ DuckDuckGo（若代理可用），并发 |
| `baidu` | 百度千帆（`enable_ai_search` 控制端点）→ 失败回退百度网页搜索 |
| `tavily` | Tavily；无 Key 时回退 Bing |
| `exa` | Exa；无 Key 时回退 Bing |
| `anysearch` | AnySearch；无 Key 时回退 Bing |
| `apipool` | 按配置顺序轮转 anysearch / baidu / tavily / exa，百度网页搜索始终兜底 |
| `hybrid` | Anysearch + 百度智能搜索 + 百度网页搜索 + Tavily + Exa + Bing + Google + DuckDuckGo，并发 |

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
| `baidu_api` | 百度千帆搜索（`enable_ai_search` 控制端点） | ❌ | 否 |

**学术搜索引擎**（无需 Key）：

| 引擎 | 说明 | 需要代理 |
|------|------|----------|
| `arxiv` | 预印本（CS/AI/物理） | 否 |
| `crossref` | 全学科 DOI 元数据 | 否 |
| `openalex` | 全学科开放学术图谱 | 否 |
| `pubmed` | 生物医学文献 | 否 |
| `semantic_scholar` | 语义学术（默认禁用） | 是（自动检测） |
| `google_scholar` | 全学科学术搜索（默认禁用） | 是（自动检测） |

> **网络可用性**：Google / DuckDuckGo / Crossref / Google Scholar 在 `network: china` 且无代理时不稳定（可能超时或被反爬拦截）；Bing 网页抓取亦可能超时。失败引擎自动跳过/回退，不影响其他引擎结果。

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

六大学术引擎结果经 RRF 融合排名，叠加学术特有信号：

- **引用数**（对数压缩，clamp [1.0, 1.7]）
- **高影响力期刊 / 会议**加分
- **PDF 全文可用性**
- **新鲜度因子**（时间敏感查询时近 1 年 ×1.15）

低分论文自动过滤（Top-1 + 每引擎保底）。配置项：`academic.enhance`（默认 true）、`academic.threshold`（默认 0.02），独立于 smartsearch。

---

## SmartSearch 高级配置

`smartsearch` 节控制搜索结果的过滤、截断和输出格式：

```yaml
smartsearch:
  max_size: 10           # 全局最大结果数（按 score 排序后截断），0 = 不限
  show_meta: true        # 输出中显示引擎来源和相关性分数（默认 true）
  enhance: true          # 本地评分增强（RRF 融合 + 词汇对齐 + 域名品质 + 多层 Boost + 阀值过滤），默认 true
  relevance_threshold: 0.05  # 增强后相关性阀值，低于此值过滤（Top-1 保护），默认 0.05
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
  weights:                # weighted 策略权重（单 Key 权重；默认值见下）
    anysearch: 30000
    baidu: 1500
    tavily: 1200
    exa: 1200
```

**策略说明**：
- **`round-robin`**（默认）：跨请求轮转起始供应商，每次请求内先用完当前供应商所有可用 SK 再 fallback 到下一个
- **`priority`**：始终从列表第一个供应商开始，用完所有 SK → 切换下一个供应商 → 百度网页搜索兜底
- **`weighted`**：按权重加权随机选起始供应商，突发请求天然分散到多家供应商。供应商有效权重 = **配置权重 × 当前可用 SK 数**（SK 失效冷却后权重自动下降，自愈）；权重表未收录的供应商按 1 计；显式 `0` 表示不参与加权起始选择（仍保留在失败切换链路）；全部权重为 0 时退化为 round-robin。百度网页搜索兜底引擎无 Key 池，固定权重 1

**权重默认值**（可被 `apipool.weights` 覆盖）：`anysearch=30000`、`baidu=1500`、`tavily=1200`、`exa=1200`

**工作流程**：选供应商 → `pool.Next()` 取 key → 调 API → 成功返回 / 失败标记 key 冷却 30 分钟 → 同供应商下一个 SK 重试 → 全部耗尽切下一个供应商 → 全部失败用百度网页搜索兜底

---

## MCP 工具

> 工具注册条件：`smartsearch` 需 `bing.enabled=true`；`academicsearch` 需 `academic.enabled=true`；`cleanfetch` 需 `cleanfetch.enabled=true`；`pdf_parser` 需 `pdf_parser.enabled=true`。

### `smartsearch` — 通用网络检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词 |
| `intent` | string | ❌ | 搜索意图（仅 LLM 启用时生效，未启用时自动移除该参数节省上下文） |
| `time_range` | int | ❌ | 搜索时间范围（月），默认 3。`1`=近 1 个月，`6`=近半年，`12`=近一年，`0`=不限 |

返回结果默认附带来源引擎和相关性分数（Tavily 等支持 score 的引擎）。可通过 `smartsearch.show_meta: false` 关闭。

**LLM 摘要**：配置 `llm` 节后，`smartsearch` 支持 `intent` 参数并生成结构化摘要；摘要阶段通过 MCP progress notification 逐 token 流式推送生成过程，客户端断开自动取消，失败自动回退非流式摘要。

### `academicsearch` — 学术论文检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词 |
| `engines` | []string | ❌ | 引擎子集：`arxiv` `crossref` `openalex` `pubmed` `semantic_scholar` `google_scholar` |
| `time_range` | string | ❌ | `year` / `month` / `week` / `day` |
| `page` | int | ❌ | 页码，默认 1 |

结果按学术评分增强排序（默认开启）：RRF 融合排名 + 引用数 / 期刊权威 / PDF 全文 / 新鲜度信号，低分论文自动过滤（Top-1 + 每引擎保底）。配置项：`academic.enhance`（默认 true）、`academic.threshold`（默认 0.02）。

### `cleanfetch` — 网页内容抓取

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | ✅ | 网页 URL |

需配置 `cleanfetch.enabled: true`。基于 go-webfetch，无需代理；内置 DNS rebinding 防护和 HEAD 预检防大文件（`max_fetch_size_mb` 控制阈值，默认 10MB）；失败时自动回退 Jina Reader（需配置 `jina.api_key`，代理自动检测）。

### `pdf_parser` — PDF 解析

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | 本地 PDF 文件路径或远程 URL |

需配置 `pdf_parser.enabled: true`。大文档自动存储到临时文件。

**解析策略**：本地 PDF 优先用 PDF 库（ledongthuc/pdf）提取文本；无文本层时若开启 `mineru_ocr` 再回退 MinerU OCR。
- `mineru_ocr: true`：扫描件 / 图片型 PDF 的 OCR 回退（无 Token 走 Agent 轻量 API，≤10MB/20页）
- `mineru_token`：远程 URL 精准解析 API（≤200MB/200页）；也可与 OCR 回退共用
- 获取 Token：https://mineru.net/apiManage
- 环境变量：`MINERU_TOKEN`

---

## 学术搜索建议

- 医学/生物 → `pubmed`；CS/AI → `arxiv` + `semantic_scholar`；全学科 → `crossref` + `openalex`
- 国内保持 `network: china`，海外引擎自动跳过
- Semantic Scholar / Google Scholar 默认禁用，设置 `disable_semantic_scholar: false` / `disable_google_scholar: false` 即可启用，代理自动检测
