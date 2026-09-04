# 配置参考

[English](configuration.en.md) | [中文](configuration.md)

## 目录

- [配置文件路径](#配置文件路径)
- [stdio CLI 配置说明](#stdio-cli-配置说明)
- [完整配置](#完整配置)
- [环境变量覆盖](#环境变量覆盖)
- [默认值速查](#默认值速查)

---

## 配置文件路径

优先级（从高到低）：
1. 环境变量 `WEBSEARCH_CONFIG`
2. CLI 参数 `-c / --config`
3. 当前目录 `config.yaml`

> HTTP daemon：通过 `-c` 指定后，PID 文件和日志文件写到配置文件所在目录。
> stdio CLI：无 PID；日志写到配置目录下的 `websearch.log`（控制台日志在 **stderr**，避免污染 stdout 上的 JSON-RPC）。

HTTP daemon（`websearch-mcpserver start`）与 stdio CLI（`websearch-mcp-cli`）**共用同一套 YAML**，搜索/工具字段含义相同。差异见下一节。

---

## stdio CLI 配置说明

stdio 二进制与 HTTP 服务使用同一配置 schema（`config.example.yaml` / `websearch-mcp-cli init` 写出的内容一致），**不必**单独维护一份 CLI 配置。

| 项 | HTTP daemon | stdio CLI（`websearch-mcp-cli`） |
|----|-------------|-------------------------------|
| 配置是否必需 | `start` **必须**能读到配置文件 | **可选**：未找到文件时用内存默认值（`mode: engine`，Bing/学术默认开） |
| `-c` / `WEBSEARCH_CONFIG` 指向不存在的文件 | 报错退出 | 报错退出（不会静默回落到默认值） |
| `port` | 监听端口（默认 8338），admin / SearXNG 依赖 | **忽略**（不监听 HTTP） |
| `host` | 监听地址（默认 `127.0.0.1`，只绑本机） | **忽略**（不监听 HTTP） |
| `auth_token` | 业务端点鉴权 token（空 = 不鉴权） | **忽略**（stdio 无 HTTP 暴露面） |
| 日志控制台 | stdout | **stderr**（文件日志仍为 `websearch.log`） |
| 进程管理 | `start`/`stop`/`kill`、refcount、PID、Windows `install` | 无；由 MCP 客户端拉起/结束进程 |
| SearXNG `/searxng/search` | 有 | 无 |

**推荐最小配置（stdio，零 Key）**：

```yaml
mode: engine
# port 可省略；写了也不生效
```

也可用环境变量注入 Key（与 HTTP 相同）：`BAIDU_SK`、`TAVILY_SK`、`EXA_API_KEY`、`LLM_BASE_URL`、`LLM_API_KEY`、`MINERU_TOKEN` 等。无配置文件走内存默认时，上述 Key 类环境变量仍会被读取；完整字段默认值仍以「读配置文件 + Viper」路径为准。

生成示例文件：

```bash
./websearch-mcp-cli init
./websearch-mcp-cli -c ~/.config/websearch/config.yaml init
```

---

## 完整配置

```yaml
port: 8338                  # MCP HTTP 端口（stdio CLI 忽略此字段）
host: "127.0.0.1"           # 监听地址（默认 127.0.0.1，只绑本机；0.0.0.0 开放所有网卡，需配 auth_token）
auth_token: ""              # 业务端点鉴权 token（空 = 不鉴权；环境变量 WEBSEARCH_TOKEN）
log_level: info             # debug / info / warn / error
mode: engine                # baidu / apipool / tavily / exa / anysearch / hybrid / engine
network: china              # china（跳过海外引擎） / international

# 全局限流（对所有搜索引擎统一生效）
rate_limit:
  per_sec: 3                # 每秒请求数上限（默认 3）
  per_min: 60               # 每分钟请求数上限（默认 60）

# 屏蔽站点（对所有搜索引擎生效）
black_list_host:
  - "csdn.net"
  - "baidu.com"

# 百度千帆（mode=baidu/apipool/hybrid 时需要）
baidu:
  api_key: ""               # 环境变量: BAIDU_SK（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key）
  enable_ai_search: true    # true=智能搜索 chat/completions（默认），false=网页搜索 web_search
  model: ""                 # 智能搜索模型名，不传=免费搜索（不产生 LLM 费用），传入=LLM 智能搜索
  search_source: "baidu_search_v2" # 搜索引擎版本
  enable_reasoning: false   # 深度思考
  enable_deep_search: false # 深搜索
  search_mode: "auto"       # auto / required / disabled

# Tavily（mode=tavily/apipool/hybrid 时需要）
tavily:
  api_key: ""               # 环境变量: TAVILY_SK（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key）

# Exa（mode=exa/apipool/hybrid 时需要）
exa:
  api_key: ""               # 环境变量: EXA_API_KEY（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key）
  num_results: 5            # 单次搜索结果数量（默认 5）
  lookback_days: 90         # 搜索时间范围（天），默认 90

# AnySearch（mode=anysearch/apipool/hybrid 时需要）
anysearch:
  api_key: ""               # 环境变量: ANYSEARCH_API_KEY（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key；重复 Key 自动去重）
  num_results: 10           # 单次搜索结果数量（默认 10）

# Bing 引擎（兜底 + engine 模式主力，无需 Key）
bing:
  enabled: true             # 总开关
  blocked: []               # Bing 专用屏蔽（与 black_list_host 合并）

# DuckDuckGo 引擎（需代理，无需 Key）
duckduckgo:
  enabled: true             # 总开关（代理可用时自动参与搜索）
  blocked: []               # DuckDuckGo 专用屏蔽（与 black_list_host 合并）

# Google 引擎（默认禁用，被反爬拦截暂不可用）
google:
  enabled: false            # 显式 true 可尝试启用，但可能返回安全挑战页面
  blocked: []               # Google 专用屏蔽（与 black_list_host 合并）

# 学术引擎（无需 Key）
academic:
  enabled: true             # 总开关，开启后注册 academicsearch 工具
  bing_fallback: true       # 学术搜索用 Bing 兜底
  enhance: true             # 学术评分增强（RRF 融合 + 引用数/期刊权威/PDF/新鲜度信号），默认 true
  threshold: 0.02           # 学术结果阀值（比通用搜索更宽松），默认 0.02
  # Semantic Scholar 可选 API key（匿名限流严格，带 key 连续 429 自动降级匿名）
  # semantic_scholar_api_key: ""   # 环境变量: SEMANTIC_SCHOLAR_API_KEY
  disable_arxiv: false
  disable_crossref: false
  disable_openalex: false
  disable_pubmed: false
  disable_semantic_scholar: true    # 默认禁用（开启后自动通过代理访问）
  disable_google_scholar: true      # 默认禁用（开启后自动通过代理访问）
  disable_europepmc: false  # Europe PMC 生物医学增补源（国内可直连）
  disable_dblp: false       # DBLP CS 会议/期刊索引（国内可直连）
  disable_doaj: false       # DOAJ 开放获取期刊（国内可直连）

# 代理（默认自动检测系统代理，无需手动配置）
proxy:
  enabled: false          # 留空→自动检测；true→使用 endpoint；false→禁用
  endpoint: "http://127.0.0.1:7897"  # 仅 enabled: true 时生效

# LLM 摘要（可选）
llm:
  base_url: "https://api.openai.com/v1"   # 环境变量: LLM_BASE_URL
  api_key: ""                               # 环境变量: LLM_API_KEY
  model_id: "gpt-4o-mini"

# 缓存
cache:
  # enabled: true            # 不设置时按 storage_path 判断；显式 false 强制禁用
  storage_path: "./data/search_cache.db"
  cleanup_interval: 30      # 清理间隔（分钟），最大 360

# Jina Reader（可选，cleanfetch 失败时回退）
jina:
  api_key: ""               # 留空则不启用 Jina 回退
  base_url: ""              # 默认 https://r.jina.ai

# 增强型网页抓取（默认关闭）
cleanfetch:
  enabled: false            # 显式 true 才启用
  file_output_dir: ""       # 默认 系统临时目录/webfetch/
  file_ttl_hours: 24        # 临时文件保留时长（小时）
  max_inline_lines: 100     # 超过此行数存文件
  max_inline_chars: 0       # 超过此字符数存文件，0=不限
  timeout_sec: 30           # 单次请求超时（秒），默认 30
  max_fetch_size_mb: 10     # HEAD 预检最大文件大小（MB），超过拒绝抓取（默认 10）
  use_system_proxy: false   # 自动使用系统代理（环境变量+注册表），默认 false
  max_retries: 3            # 最大重试次数（仅 429/502/503），默认 3

# PDF 解析工具（默认关闭，独立于 cleanfetch）
# MinerU AI 增强（可选）：有 Token 用精准 API（远程 URL，≤200MB），无 Token 用 Agent 轻量 API（本地文件，≤10MB）
# 获取 Token: https://mineru.net/apiManage | 环境变量: MINERU_TOKEN
pdf_parser:
  enabled: false            # 显式 true 才启用
  # mineru_token: ""        # JWT Token，有则启用精准 API
  # mineru_model: "pipeline" # pipeline(默认) / vlm(推荐)
  # mineru_ocr: false        # 扫描件 OCR 回退（本地库读不到文本时启用）
  # mineru_formula: true     # 公式识别（默认 true）
  # mineru_table: true       # 表格识别（默认 true）
  # mineru_lang: "ch"        # 文档语言（默认 ch）

# 搜索结果过滤与输出格式（可选）
# smartsearch:
#   max_size: 10          # 全局最大结果数（按 score 排序后截断），0 = 不限
#   show_meta: true       # 输出中显示引擎来源和相关性分数（默认 true）
#   enhance: true         # 本地评分增强（RRF 融合 + 词汇对齐 + 域名品质 + 多层 Boost + 阀值过滤），默认 true
#   relevance_threshold: 0.05  # 增强后相关性阀值，低于此值过滤（Top-1 保护），默认 0.05
#   mmr:                       # MMR 多样性重排（打散同话题高相似结果）
#     enabled: true            # 总开关（默认 true）
#     lambda: 0.7              # 相关性-多样性权衡系数 [0,1]，越高越偏相关性（默认 0.7）
#     target_count: 0          # MMR 后目标条数，0 = 不额外截断
#   engines:              # 按引擎名配置（引擎名: tavily_api, exa, baidu_api, baidu, bing, google, duckduckgo）
#     tavily_api:
#       min_score: 0.5    # Tavily API 最低相关性分数阈值（0 = 不过滤）
#       max_size: 6       # Tavily API 单引擎最大结果数（默认 4）
#       weight: 1.0       # 引擎权重，影响 RRF 融合分（enhance=true 时生效），0 = 默认 1.0
#     exa:
#       min_score: 0      # Exa 不回传 score，此字段无效
#       max_size: 4       # Exa 单引擎最大结果数
#       weight: 1.0
#     baidu_api:
#       min_score: 0      # 百度千帆搜索不回传 score（enable_ai_search 控制端点）
#       max_size: 5
#       weight: 1.0
#     baidu:
#       min_score: 0      # 百度网页搜索不回传 score
#       max_size: 5
#       weight: 1.0
#     bing:
#       min_score: 0      # Bing 不回传 score，此字段无效
#       max_size: 4
#       weight: 1.0
#     google:
#       min_score: 0      # Google 不回传 score，此字段无效
#       max_size: 4
#       weight: 1.0
#     duckduckgo:
#       min_score: 0      # DuckDuckGo 不回传 score，此字段无效
#       max_size: 4
#       weight: 1.0

# Apipool 模式配置（可选，mode=apipool 时生效）
# apipool:
#   strategy: weighted    # round-robin（默认）: 跨请求轮转起始供应商
#                         # priority: 始终从第一个供应商开始
#                         # weighted: 按权重加权随机选起始供应商（见 weights）
#   engines:              # 供应商优先级顺序（默认 [anysearch, baidu, tavily, exa]，百度网页搜索兜底始终在末尾）
#     - anysearch
#     - baidu
#     - tavily
#     - exa
#   weights:              # weighted 策略权重（单 Key 权重，实际权重按可用 Key 数累加）
#     anysearch: 30000    # 默认值: anysearch=30000, baidu=1500, tavily=1200, exa=1200
#     baidu: 1500
#     tavily: 1200
#     exa: 1200

# 日志滚动
log:
  max_size: 1               # 单文件最大 MB
  max_age: 1                # 保留天数
```

---

## 环境变量覆盖

| 环境变量 | 覆盖字段 | 说明 |
|----------|---------|------|
| `WEBSEARCH_CONFIG` | 配置文件路径 | 最高优先级 |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | |
| `EXA_API_KEY` | `exa.api_key` | Exa Web Search API Key |
| `ANYSEARCH_API_KEY` | `anysearch.api_key` | AnySearch API Key（[anysearch.com](https://www.anysearch.com/docs)） |
| `LLM_BASE_URL` | `llm.base_url` | |
| `LLM_API_KEY` | `llm.api_key` | |
| `MINERU_TOKEN` | `pdf_parser.mineru_token` | MinerU 精准解析 API Token |

> Viper 的 `AutomaticEnv()` 还支持 `APP_` 前缀覆盖任意配置项。

---

## 默认值速查

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `port` | 8338 | stop/kill/status 无配置时也用此端口 |
| `mode` | engine | 无 Key 时自动回退 engine；`apipool` 为 API Key 池轮转模式，支持 round-robin / priority / weighted 策略 |
| `network` | china | |
| `rate_limit.per_sec` | 3 | 全局限流 |
| `rate_limit.per_min` | 60 | 全局限流 |
| `apipool.strategy` | round-robin | `round-robin` 跨请求轮转供应商 / `priority` 固定优先级顺序 / `weighted` 加权随机 |
| `apipool.engines` | [anysearch, baidu, tavily, exa] | 供应商优先级顺序，百度网页搜索兜底始终在末尾 |
| `apipool.weights` | anysearch=30000, baidu=1500, tavily=1200, exa=1200 | weighted 策略单 Key 权重，实际权重按可用 Key 数累加 |
| `baidu.enable_ai_search` | true | true=智能搜索 chat/completions，false=网页搜索 web_search；不传 model 不产生 LLM 费用 |
| `bing.enabled` | true | |
| `duckduckgo.enabled` | true | 需代理，代理可用时自动参与 |
| `google.enabled` | false | 被反爬拦截，需显式启用 |
| `academic.enabled` | true | |
| `academic.bing_fallback` | true | |
| `academic.enhance` | true | 学术评分增强 |
| `academic.threshold` | 0.02 | 学术结果阀值 |
| `academic.disable_semantic_scholar` | true | 默认禁用，开启后自动走代理 |
| `academic.disable_google_scholar` | true | 默认禁用，开启后自动走代理 |
| `academic.semantic_scholar_api_key` | "" | 可选 API key，带 key 连续 429 自动降级匿名（环境变量 `SEMANTIC_SCHOLAR_API_KEY`） |
| `academic.disable_europepmc` | false | Europe PMC 生物医学增补源，国内可直连 |
| `academic.disable_dblp` | false | DBLP CS 会议/期刊索引，国内可直连 |
| `academic.disable_doaj` | false | DOAJ 开放获取期刊，国内可直连 |
| `proxy.enabled` | 未设置 | 未设置时自动检测系统代理；显式 false 禁用；显式 true 使用 endpoint |
| `proxy.endpoint` | `http://127.0.0.1:7897` | 仅 `enabled: true` 时生效 |
| `cleanfetch.enabled` | false | 旧配置不启用，需显式开启 |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `cleanfetch.timeout_sec` | 30 | |
| `cleanfetch.max_fetch_size_mb` | 10 | HEAD 预检阈值 |
| `cleanfetch.use_system_proxy` | false | 自动使用系统代理（环境变量+注册表） |
| `cleanfetch.max_retries` | 3 | 仅对 429/502/503 重试 |
| `pdf_parser.enabled` | false | 独立于 cleanfetch |
| `pdf_parser.mineru_model` | pipeline | pipeline / vlm |
| `pdf_parser.mineru_formula` | true | 公式识别 |
| `pdf_parser.mineru_table` | true | 表格识别 |
| `pdf_parser.mineru_lang` | ch | 文档语言 |
| `smartsearch.show_meta` | true | 输出中显示引擎来源和相关性分数 |
| `smartsearch.enhance` | true | 本地评分增强 |
| `smartsearch.relevance_threshold` | 0.05 | 增强后相关性阀值 |
| `smartsearch.mmr.enabled` | true | MMR 多样性重排 |
| `smartsearch.mmr.lambda` | 0.7 | 相关性-多样性权衡系数 |
| `cache.enabled` | nil | 不设置时按 storage_path 判断；显式 false 强制禁用；显式 true 强制启用 |
| `cache.cleanup_interval` | 30 (min) | 最大 360 |
| 缓存过期 | 6 小时 | 基于最近命中时间，硬编码不可配置 |
| `log.max_size` | 1 (MB) | |
| `log.max_age` | 1 (day) | |

---

## 最小配置

```yaml
port: 8338
mode: engine
```

零 API Key 即可运行，使用百度网页搜索 + Bing + 学术搜索引擎。
