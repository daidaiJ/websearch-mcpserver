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

> 本地集成测试可把 API Key 写在仓库根目录、已被 `.gitignore` 的 `config.test.yaml`，测试通过环境变量 `WEBSEARCH_CONFIG` 加载。**不要提交该文件。**

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

也可用环境变量注入 Key（与 HTTP 相同）：`BAIDU_SK`、`TAVILY_SK`、`EXA_API_KEY`、`ANYSEARCH_API_KEY`、`DOUBAO_SEARCH_API_KEY`（兼容 `ASK_ECHO_SEARCH_INFINITY_API_KEY`）、`LLM_BASE_URL`、`LLM_API_KEY`、`MINERU_TOKEN` 等。无配置文件走内存默认时，上述 Key 类环境变量仍会被读取；完整字段默认值仍以「读配置文件 + Viper」路径为准。

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
mcp_stateless: false        # MCP 无状态 HTTP 模式（默认 false = 会话式）：true 时每个 POST 独立处理，
                            # 免 initialize 握手与 Mcp-Session-Id 会话，便于反向代理/负载均衡水平扩展；
                            # GET SSE 长连返回 405。本服务工具均为请求-响应式，无状态模式下功能无损
log_level: info             # debug / info / warn / error
mode: engine                # baidu / apipool / tavily / exa / anysearch / doubao / hybrid / engine
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
  web_enabled: false        # 百度网页搜索引擎（tn=json 直抓）默认禁用：实测被百度 CAPTCHA 识别，
                            # 出口 IP 干净的部署环境可显式开启
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
# 获取地址: https://dashboard.exa.ai/api-keys
exa:
  api_key: ""               # 环境变量: EXA_API_KEY（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key）
  num_results: 5            # 单次搜索结果数量（默认 5）
  lookback_days: 90         # 搜索时间范围（天），默认 90

# AnySearch（mode=anysearch/apipool/hybrid 时需要）
# 获取地址: https://www.anysearch.com/console/api-keys
anysearch:
  api_key: ""               # 环境变量: ANYSEARCH_API_KEY（sk_list 为空时自动作为单元素列表）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key；重复 Key 自动去重）
  num_results: 10           # 单次搜索结果数量（默认 10）

# 豆包联网搜索 Global / Custom（mode=doubao/hybrid；apipool 需显式加入 engines）
# 开通: https://console.volcengine.com/search-infinity/web-search
# API Key 获取: https://console.volcengine.com/search-infinity/api-key
# 免费档每月默认 500 积分；apipool.weights.doubao 默认 500
doubao:
  api_key: ""               # 环境变量: DOUBAO_SEARCH_API_KEY（兼容 ASK_ECHO_SEARCH_INFINITY_API_KEY）
  sk_list: []               # 多 Key 轮询列表（优先级高于 api_key；重复 Key 自动去重）
  version: global           # global（默认）/ custom
  num_results: 10           # Global 最大 20；Custom 最大 50
  max_snippet_length: 500   # Global: 单片段最大 tokens，最大 3000
  max_image_count_per_doc: 0 # Global: 图片数，默认 0
  icp_host_only: false      # Global: 仅搜索国内 ICP 备案网站
  time_range: ""            # Custom 默认时间范围；MCP 请求级 time_range 优先（映射 OneDay/OneWeek/OneMonth/OneYear；Global 忽略）
  auth_level: 0             # Custom: 0=默认，1=仅非常权威来源
  query_rewrite: false      # Custom: 是否启用查询改写
  need_content: false       # Custom: 是否请求网页正文

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
  # Unpaywall 邮箱：结果有 DOI 且无 PDF 时补 OA 全文链接；留空则跳过、不报错
  # unpaywall_email: "you@example.com"   # 环境变量: UNPAYWALL_EMAIL
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
  # API 供应商上游请求（百度千帆/Tavily/Exa/AnySearch/豆包/LLM 等）是否走代理。
  # 默认 false = 强制直连：即使系统设了 HTTP_PROXY/HTTPS_PROXY 环境变量也不会
  # 被静默劫持（供应商请求对环境变量代理免疫）。true 时与引擎代理共用
  # enabled/endpoint/自动检测同一套解析。
  api_providers: false
  # 注意：AnySearch 这类小众供应商的 DNS 解析可能异常（官方 GTM 曾只返回
  # 单个海外 IP），直连下走系统 DNS；若解析到不可达地址可在 hosts 里固定
  # 正确 IP，或开启 api_providers 走代理绕开本地 DNS 污染。

# LLM 摘要（可选）
llm:
  base_url: "https://api.openai.com/v1"   # 环境变量: LLM_BASE_URL
  api_key: ""                               # 环境变量: LLM_API_KEY
  model_id: "gpt-4o-mini"

# 缓存（默认关闭）
cache:
  # enabled: true            # 不设置时默认关闭（v3.5.0 起）；显式 true 启用
  # storage_path: ""         # 未配置时默认 exe 同目录 cache/websearch-cache.db
  cleanup_interval: 30      # 清理间隔（分钟），最大 360

# 本机控制中心（默认关闭；仅被动记录真实调用，不主动探测，不保存完整查询或 URL）
# 推荐用独立配置文件 dashboard.yaml 管理（主配置同目录，样例见 dashboard.example.yaml）：
# 按字段覆盖这里的 dashboard: 块；删除该文件并重启 = 完整回退。
# 管理员口令 / 访问网段 / 额度 / 品牌主题建议只写在 dashboard.yaml（WebUI 无法修改）。
# 完整配置项与字段说明见 docs/dashboard.md（含快捷方式落位 shortcut 与品牌 footer）。
# dashboard:
#   enabled: false
#   storage_path: "./data/dashboard.db"
#   retention_days: 30       # 明细保留天数；每日汇总长期保留
#   secrets_path: "./data/dashboard-secrets.json" # 私密覆盖文件，不回显 Key
#   config_path: ""          # 独立配置文件路径；空 = 主配置同目录 dashboard.yaml
#   admin_password: ""       # 写操作口令（明文）；与 admin_password_sha256 二选一
#   admin_password_sha256: "" # 写操作口令（SHA-256 十六进制）
#   allowed_networks: []     # 只读放行网段（CIDR/裸 IP）；空 = 仅本机 loopback
#   quotas:
#     reset: monthly         # monthly / weekly / daily / none（仅手动重置）
#     reset_day: 1           # monthly 的每月重置日（1-28）
#     limits: {}             # 每供应商上限（次/周期），未配置默认 1000
#   brand:
#     title: ""              # 控制台标题，默认 "WebSearch 控制中心"
#     logo: ""               # http(s) URL 或本地图片路径；空 = 内置 W+放大镜
#     theme: ""              # green（默认）/ blue / mono
#     accent: ""             # 自定义主色 #RRGGBB，覆盖主题主色
#     icon: ""               # 自定义快捷方式图标（.ico 路径）；空 = 主题内置

# Jina Reader（可选，cleanfetch 失败时回退）
jina:
  api_key: ""               # 留空则不启用 Jina 回退
  base_url: ""              # 默认 https://r.jina.ai

# 增强型网页抓取（默认关闭）
cleanfetch:
  enabled: false            # 显式 true 才启用 cleanfetch 工具；smartsearch 的 fetch_top_n 不受此开关约束，会按当前配置惰性初始化 webfetch
  file_output_dir: ""       # 默认 exe 同目录 fetchdata/
  file_ttl_hours: 24        # 临时文件保留时长（小时）
  max_inline_lines: 100     # 超过此行数存文件
  max_inline_chars: 0       # 超过此字符数存文件，0=不限
  timeout_sec: 30           # 单次请求超时（秒），默认 30
  max_fetch_size_mb: 10     # HEAD 预检最大文件大小（MB），超过拒绝抓取（默认 10）；也是 fetch_top_n 单条正文字节上限
  use_system_proxy: false   # 自动使用系统代理（环境变量+注册表），默认 false
  max_retries: 3            # 最大重试次数（仅 429/502/503），默认 3

# PDF 解析工具（默认关闭，独立于 cleanfetch）
# MinerU AI 增强（可选）：有 Token 用精准 API（远程 URL，≤200MB），无 Token 用 Agent 轻量 API（本地文件，≤10MB）
# 获取 Token: https://mineru.net/apiManage | 环境变量: MINERU_TOKEN
pdf_parser:
  # max_pages: 20            # 省略 pages 时一次最多解析的页数（默认 20）
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
#   engines:              # 按引擎名配置（引擎名: tavily_api, exa, baidu_api, baidu, bing, google, duckduckgo, anysearch, doubao）
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
#     anysearch:
#       min_score: 0      # AnySearch 不回传 score，此字段无效
#       max_size: 4
#       weight: 1.0
#     doubao:
#       min_score: 0      # Custom 回传 score；Global 不回传
#       max_size: 4
#       weight: 1.0

# Apipool 模式配置（可选，mode=apipool 时生效）
# apipool:
#   strategy: weighted    # round-robin（默认）: 跨请求轮转起始供应商
#                         # priority: 始终从第一个供应商开始
#                         # weighted: 按权重加权随机选起始供应商（见 weights）
#   engines:              # 供应商优先级顺序（默认 [anysearch, baidu, tavily, exa]，百度网页搜索兜底始终在末尾）
#                         # 豆包不在默认列表；有 Key 时请显式加入
#     - anysearch
#     - baidu
#     - tavily
#     - exa
#     # - doubao
#   weights:              # weighted 策略权重（单 Key 权重，实际权重按可用 Key 数累加）
#     anysearch: 30000    # 默认值: anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500（每月默认积分）
#     baidu: 1500
#     tavily: 1200
#     exa: 1200
#     doubao: 500

# 日志滚动
log:
  max_size: 1               # 单文件最大 MB
  max_age: 1                # 保留天数
```

---

## 控制中心安全与品牌（dashboard）

> 本文只覆盖安全要点；**完整配置参考**（含 `shortcut` 快捷方式落位、`brand.footer` 关于块、全部键的默认值）见 [dashboard.md](dashboard.md)。

### 访问与写操作模型

| 场景 | 规则 |
|------|------|
| 读取（页面 + 只读 API） | 默认仅本机 loopback；`dashboard.allowed_networks` 显式放行的网段只读 |
| 写操作（设置 / 密钥 / 重启 / 清缓存 / 额度重置与修正） | **仅限本机 loopback + `X-Admin-Password` 头**，二者缺一不可 |
| 管理员口令 | 只能配置在 `dashboard.yaml`（`admin_password` 明文或 `admin_password_sha256` 十六进制），**WebUI 无法读取或修改**；未配置 = 所有写端点禁用（安全的默认） |

- 放行网段写法：`allowed_networks: ["192.168.1.0/24", "10.0.0.3"]`（CIDR 或裸 IP）；任一条目非法时**整体回退为仅本机**（fail-closed）。
- Docker 部署发布到本机回环时，容器来源是 bridge 网关（如 `172.17.0.1`），需把对应网段加入 `allowed_networks` 才能从宿主机浏览器打开控制台。
- 远程链路为明文 HTTP，只建议放行受信内网；不受信网络请走 SSH 隧道或带 TLS 的反向代理。
- 口令校验为常量时间比较；推荐配置 `admin_password_sha256`（`sha256sum` 明文的小写十六进制）避免明文落盘。

### 额度管理

- 本地用量 = 遥测中的真实成功调用（按供应商、按周期计数），不主动探测供应商；Tavily 官方用量端点可用时优先展示官方数字。
- 上限 `quotas.limits` 未配置的供应商默认 **1000 次/周期**；周期 `reset` 支持 monthly / weekly / daily / none（仅手动）。
- WebUI「设置 → 额度管理」可手动重置（从现在重新累计）或把展示用量修正为指定值（真实记录不动），两者都需管理员口令且仅限本机。

### 品牌与主题

- `brand.theme`：`green`（默认，绿色办公）/ `blue`（蓝白科技）/ `mono`（黑白灰度·立体）；`brand.accent` 自定义主色（`#RRGGBB`）覆盖主题主色。
- `brand.title` / `brand.logo` 自定义标题与 logo（http(s) URL 或本地图片路径）；WebUI 内也提供仅存本浏览器的外观选择。
- 桌面快捷方式图标跟随 `brand.theme`：每次启动做一次轻量检查，主题变化即重建快捷方式（每主题独立 ico 文件，规避 Windows 图标缓存）。

### 客户端用量归组

- 识别来源：initialize 握手的 `clientInfo.name`（MCP 规范必带，权威）+ User-Agent 关键词归一化（兜底）；都拿不到记为 unknown。
- 仅用于控制台展示分组（最近 7 天工具层调用），不做主动探测，不影响搜索；前端在无任何客户端数据时自动隐藏该面板。

### 预设与默认启用

- 全新部署（无任何控制中心配置）首次 `start` / `install` 会自动生成 `dashboard.yaml`：默认启用 + 随机本机管理员口令（0600）。
- 用户显式配置过控制中心（主配置有 dashboard 块或 dashboard.yaml 已存在）时绝不静默追加；关闭 = 改 `enabled: false` 或删文件，运行时零遥测开销。

### 迁移与回退

- 老配置（无 `dashboard:` 块）零改动兼容：控制中心保持关闭，行为与上游一致。
- PR 时代已建过的 `dashboard.db` 会自动补建 `quota_state` 表；回退旧版本时该表被忽略。
- 回退到旧版本前建议删除桌面快捷方式（旧版本不识别 `open` 子命令）。

---

## 环境变量覆盖

| 环境变量 | 覆盖字段 | 说明 |
|----------|---------|------|
| `WEBSEARCH_CONFIG` | 配置文件路径 | 最高优先级 |
| `WEBSEARCH_DASHBOARD_CONFIG` | 控制中心独立配置路径 | 见 [dashboard.example.yaml](../dashboard.example.yaml) |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | Tavily API Key（[获取地址](https://app.tavily.com/home)） |
| `EXA_API_KEY` | `exa.api_key` | Exa Web Search API Key（[获取地址](https://dashboard.exa.ai/api-keys)） |
| `ANYSEARCH_API_KEY` | `anysearch.api_key` | AnySearch API Key（[获取地址](https://www.anysearch.com/console/api-keys)） |
| `DOUBAO_SEARCH_API_KEY` | `doubao.api_key` | 豆包联网搜索 API Key（[控制台](https://console.volcengine.com/search-infinity/api-key)） |
| `ASK_ECHO_SEARCH_INFINITY_API_KEY` | `doubao.api_key` | 火山官方 MCP 兼容变量名 |
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
| `mcp_stateless` | false | MCP 无状态 HTTP 模式：每个 POST 独立处理、免会话握手，便于水平扩展；GET SSE 返回 405 |
| `baidu.web_enabled` | false | 百度网页搜索引擎默认禁用（实测被 CAPTCHA 识别），出口 IP 干净时可显式开启 |
| `network` | china | |
| `rate_limit.per_sec` | 3 | 全局限流 |
| `rate_limit.per_min` | 60 | 全局限流 |
| `apipool.strategy` | round-robin | `round-robin` 跨请求轮转供应商 / `priority` 固定优先级顺序 / `weighted` 加权随机 |
| `apipool.engines` | [anysearch, baidu, tavily, exa] | 供应商优先级顺序，百度网页搜索兜底始终在末尾；`doubao` 不在默认列表，有 Key 时显式加入 |
| `apipool.weights` | anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500 | weighted 策略单 Key 权重，实际权重按可用 Key 数累加；豆包 500 对齐免费档每月积分
| `doubao.version` | global | Global / Custom；两版并发走 hybrid，适配器内不提供 both |
| `doubao.num_results` | 10 | Global 最大 20；Custom 最大 50 |
| `doubao.time_range` | "" | Custom 默认时间范围；MCP 请求级 `time_range` 优先（映射 OneDay/OneWeek/OneMonth/OneYear） |
| `doubao.need_content` | true | Custom 请求网页正文（API 快速路径）；显式 false 退回摘要 |
| `tavily.include_raw_content` | true | Tavily 请求返回页面原文（raw_content，快速路径）；显式 false 退回摘要片段 |
| `exa.include_text` | true | Exa 请求返回页面正文（contents.text） |
| `exa.text_max_characters` | 3000 | Exa 正文最大字符数 |
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
| `academic.unpaywall_email` | "" | Unpaywall OA 补链邮箱（结果有 DOI 且无 PDF 时补全）；空=跳过且不报错（环境变量 `UNPAYWALL_EMAIL`） |
| `academic.disable_europepmc` | false | Europe PMC 生物医学增补源，国内可直连 |
| `academic.disable_dblp` | false | DBLP CS 会议/期刊索引，国内可直连 |
| `academic.disable_doaj` | false | DOAJ 开放获取期刊，国内可直连 |
| `proxy.enabled` | 未设置 | 未设置时自动检测系统代理；显式 false 禁用；显式 true 使用 endpoint |
| `proxy.endpoint` | `http://127.0.0.1:7897` | 仅 `enabled: true` 时生效 |
| `proxy.api_providers` | false | API 供应商上游请求（千帆/Tavily/Exa/AnySearch/豆包/LLM）是否走代理；false 强制直连，环境变量代理不生效 |
| `cleanfetch.enabled` | false | 旧配置不启用，需显式开启；仅约束 cleanfetch 工具，`fetch_top_n` 不受限（惰性初始化 webfetch） |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `cleanfetch.timeout_sec` | 30 | |
| `cleanfetch.max_fetch_size_mb` | 10 | HEAD 预检阈值；也是 `fetch_top_n` 单条正文字节上限 |
| `cleanfetch.use_system_proxy` | false | 自动使用系统代理（环境变量+注册表） |
| `cleanfetch.max_retries` | 3 | 仅对 429/502/503 重试 |
| `pdf_parser.enabled` | false | 独立于 cleanfetch |
| `pdf_parser.max_pages` | 20 | 省略 pages 时一次最多解析的页数；单个 pages 区间宽度上限 1000 页 |
| `pdf_parser.mineru_model` | pipeline | pipeline / vlm |
| `pdf_parser.mineru_formula` | true | 公式识别 |
| `pdf_parser.mineru_table` | true | 表格识别 |
| `pdf_parser.mineru_lang` | ch | 文档语言 |
| `smartsearch.show_meta` | true | 输出中显示引擎来源和相关性分数 |
| `smartsearch.fetch_top_n` | 0 | 服务端默认抓取正文条数（agent 未传 `fetch_top_n` 参数时生效）；默认 0 = 与旧版一致不抓取，1-5 = 一次搜索即含正文（API 引擎走原文传参快速路径，网页引擎内部抓取） |
| `smartsearch.enhance` | true | 本地评分增强 |
| `smartsearch.relevance_threshold` | 0.05 | 增强后相关性阀值 |
| `smartsearch.mmr.enabled` | true | MMR 多样性重排 |
| `smartsearch.mmr.lambda` | 0.7 | 相关性-多样性权衡系数 |
| `cache.enabled` | false | 不设置时默认关闭（v3.5.0 起）；显式 true 启用（storage_path 默认 exe 同目录 cache/websearch-cache.db） |
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
