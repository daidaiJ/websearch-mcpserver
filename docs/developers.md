# 开发者指南 — 包结构、接口与关键链路

> 面向本仓库的日常开发与 AI 智能体协作。English version: [developers.en.md](developers.en.md)包结构的权威描述同步维护在 [AGENTS.md](../AGENTS.md)；
> 本文补充**每个包的接口职责**、**「我要改 X 该动哪个包」**和**五个 MCP 工具的调用链**。

## 1. 包地图与依赖方向

依赖方向自上而下（下层不知道上层）：

```
cmd/ ──► server/ ──► mcp/ ──► pkg/search（组合根）──► pkg/search/{mode,hybrid,apipool}
                                        │                    │
                                        │                    ├──► provider / adapter / enhance
                                        │                    └──► core（类型契约，叶子）
                                        └──► pkg/academic ──► antirobot
mcp/ ──► pkg/fetch/{webfetch,jina,mineru} ──► go-webfetch / MinerU
mcp/ searxng/ ──► pkg/search（组合根）；pkg/cache  pkg/llm ──► 仅依赖 pkg/search/core（契约叶子）
所有包 ──► pkg/config  pkg/log  pkg/client  pkg/proxy  pkg/antirobot  pkg/daemon
```

## 2. 各包职责与关键接口

### 顶层装配

| 包 | 职责 | 关键接口/类型 |
|----|------|--------------|
| `cmd/` | 入口：配置加载、平台初始化（Windows 代理检测等） | `main` |
| `server/` | HTTP 服务生命周期、路由、优雅关停 | `Run`、关机顺序引用 `mcp.GetWebFetch/GetCache` |
| `searxng/` | SearXNG 兼容端点，复用同一套引擎组 | `mcp.GetSearchGroup()` |
| `mcp/` | MCP 协议层：5 个工具的注册、schema、handler、缓存与摘要编排 | `server.go registerTools`、`tool.go`（参数/装配）、`tool_search.go`、`tool_academic.go`、`tool_cleanfetch.go`、`tool_pdf.go`、`tool_filesearch.go`、`tool_summarize.go`、`security.go`（SSRF/HEAD 预检 + 重定向逐跳复查）、`options.go` 按需装配（webfetch 支持 fetch_top_n 惰性初始化、everything 启动探测门控） |

### pkg/search — 搜索编排（分 7 个子包）

| 子包 | 职责 | 关键接口/类型 |
|------|------|--------------|
| `search`（根） | **组合根**：`SearchGroup` 装配 + 对外类型别名（外部一律用 `search.SearchResult` 等） | `NewFromConfig(conf) *SearchGroup`、`SearchGroup{Primary, Fallback, Academic}` |
| `search/core` | 类型契约叶子层：所有子包共用，**禁止** import 其它 search 子包 | `SearchInf`、`SearchResult`、`SearchTimeRanger`、`AcademicSearcher`、`ScoreBucket`、`FilterByScore/SortByScore`、`FormatMD` 系列结果格式化 |
| `search/engine` | 底层网页引擎（HTTP 抓取 + 反检测参数），实现 `antirobot.Engine` | `baidu/bing/ddg/google` 各自的 `Engine` + `Opts` |
| `search/provider` | API 供应商适配器（实现 `SearchInf`）+ Key 池 | `NewTavilySearch/NewExaSearchWithResults/NewAnysearchSearch/NewDoubaoSearch/NewBaiduAISearch/NewBaiduSeach`、`KeyPool`（单供应商 SK 轮转）、`KeyError` |
| `search/adapter` | 把「非 API 供应商」适配成 `SearchInf` | `EngineSearchAdapter`（antirobot.Engine → SearchInf）、`BingSearchAdapter`、`BaiduWithFallback`、`AcademicAdapter`（学术 → SearchInf/AcademicSearcher） |
| `search/apipool` | 跨供应商轮转策略（与 hybrid 同级实现） | `ApipoolSearchImpl`（round-robin/priority/weighted）、`ApipoolProvider` |
| `search/hybrid` | 多引擎并发编排策略 | `HybridSearchImpl`（并发调用、去重合并、`EngineFilter` per-engine 过滤、错误日志） |
| `search/enhance` | 高阶评分功能（评分流水线只吃 `ScoreBucket`，与引擎无关） | `EnhanceResults/EnhanceResultsMMR`（RRF/域名品质/词汇对齐/稀有词/共识 Boost/阀值）、`ApplyMMR`、`EnhanceAcademicResults`（学术信号：引用数/期刊/PDF/新鲜度） |
| `search/mode` | 搜索模式构建：按 `config.mode` 组装 Primary | `BuildEngineMode/BuildBaiduMode/BuildTavilyMode/BuildExaMode/BuildAnysearchMode/BuildDoubaoMode/BuildApipoolMode/BuildHybridMode`、`InitBingEngine/InitBaiduWebEngine/InitGoogleEngine/InitDuckDuckGoEngine/InitAcademicEngine` |

### 其它领域包

| 包 | 职责 | 关键接口 |
|----|------|---------|
| `pkg/academic` | 9 学术源引擎实现（arXiv/Crossref/OpenAlex/PubMed/S2/GS/EuropePMC/DBLP/DOAJ），DOI 去重、单篇 lookup、Unpaywall OA 补全 | `Engine.Search()`、`lookup.go`（DOI/arXiv id 单篇） |
| `pkg/fetch/webfetch` | 增强抓取（SSRF/DNS rebinding 防护 + PDF 解析分支 + 大文本落盘） | `Fetcher.Fetch`、`Fetcher.FetchPDFWithPages` |
| `pkg/fetch/jina` | Jina Reader 备选抓取（需代理） | `Reader.Fetch` |
| `pkg/fetch/mineru` | MinerU AI 增强 PDF 解析（OCR/表格/公式） | `Client.ParseURL/ParseFile` |
| `pkg/llm` | LLM Client + 搜索结果摘要 | `Client.Chat/ChatStream`、`Summarizer.Summarize/SummarizeStream` |
| `pkg/antirobot` | 反检测公共层（**跨层共享，保持顶层**） | `Engine`、`Searcher`、限流器、`TimeRange` |
| `pkg/cache` | SQLite 缓存（6h 过期，key 含 intent/fetch_top_n） | `Cache.Lookup/Store/UpdateSummary` |
| `pkg/client` | API 供应商共用 HTTP 客户端（超时/重试/代理） | `Client` |
| `pkg/config` | 全部配置结构体 + Viper 加载 + 环境变量覆盖 | `Load`、各 XxxConfig |
| `pkg/proxy` | 系统代理检测（注册表/WinHTTP，10s TTL）+ transport 池 | `DetectSystemProxy` |
| `pkg/daemon` | 引用计数进程管理 | — |
| `pkg/log` / `pkg/xml`(已并入 search/core) | 日志配置 / — | — |

## 3. 「我要改 X」对照表

| 想做的事 | 看 / 改哪里 |
|----------|------------|
| 新增底层网页引擎（如搜狗） | `pkg/search/engine/` 新包（实现 `antirobot.Engine`）→ `pkg/search/adapter` 加适配 → `pkg/search/mode` 注册进各模式 → `pkg/config` 加开关 |
| 新增 API 供应商（如 Serper） | `pkg/search/provider/` 新适配器（实现 `SearchInf`，用 `KeyPool`）→ `pkg/search/mode` 注册（单引擎模式 + apipool/hybrid 可选）→ `pkg/config` + 文档 |
| 调整搜索模式组合逻辑 / 兜底顺序 | `pkg/search/mode/factory.go`；模式开关在根包 `factory.go` 的 switch |
| 改多引擎去重/合并/per-engine 过滤 | `pkg/search/hybrid/hybrid.go` |
| 改 apipool 轮转策略、SK 失效标记 | `pkg/search/apipool/apipool.go`（Key 池原语在 `provider/keypool.go`） |
| 改评分（RRF/Boost/阀值/MMR） | `pkg/search/enhance/`（web）与 `enhance/academic_enhance.go`（学术）；scoreBucket 结构在 `search/core` |
| 改结果输出格式（Markdown 样式） | `pkg/search/core/format_md.go`（所有引擎共用） |
| 改某引擎限流/反检测参数 | `pkg/antirobot/` 层 + 引擎 Opts；**勿**在引擎包内重复实现限流 |
| 学术源增删 / DOI 单篇 / OA 补全 | `pkg/academic/`；学术工具描述在 `mcp/server.go buildAcademicToolDescription` |
| 新增/修改 MCP 工具参数 | 对应 `mcp/tool*.go`（Params struct + handler）+ `mcp/server.go`（注册与描述） |
| 新增配置项 | `pkg/config/config.go` + 两份 `config.example.yaml` + `docs/configuration*.md`（工具参数还要 `docs/search*.md`、`docs/api*.md`） |
| 改 URL 安全校验 / 大小预检 | `mcp/security.go`（MCP 层预检）+ `pkg/fetch/webfetch`（库层 BlockPrivateIP，双重防护） |
| 改抓取行为（UA/超时/落盘阈值） | `pkg/fetch/webfetch/webfetch.go`（底层库为 `github.com/daidaiJ/go-webfetch`） |
| 改 PDF 页范围 / MinerU 策略 | 工具参数 `mcp/tool.go`（`pages`）+ `pkg/fetch/webfetch`（`FetchPDFWithPages`）+ 配置 `pdf_parser.max_pages` |
| 改缓存语义（key/过期/命中类型） | `pkg/cache/` + `mcp/tool.go` 的 `webSearchCacheQuery` 等 |
| 新增本地检索能力 / 改过滤策略 | `pkg/fetch/everything/`（HTTP 客户端 + ScopeQuery 白名单 + EnhanceItems 二次过滤）+ `mcp/tool_filesearch.go` + 配置 `everything.*` |
| 用户问怎么启用/加固 Everything HTTP Server | `skills/everything-http-server/SKILL.md`（分版本流程 + 陷阱 + 验证 + 加固清单），agent 走 `/everything-http-server` |

## 4. 五个工具的关键链路

### smartsearch（网页搜索）

```
mcp/server.go registerTools（按 LLM 开关注册 With/NoIntent 两套 schema）
  └► mcp/tool.go WebSearchWithIntent/NoIntent ─► doWebSearch
        ├► 缓存查询 cacheInst.Lookup（key = query|fetch_top_n）
        ├► searchapi.SearchRaw（= SearchGroup.Primary）
        │     └► pkg/search/factory.NewFromConfig 装配：
        │          mode.BuildXxxMode ─► hybrid.HybridSearchImpl（多引擎并发）
        │                                / apipool.ApipoolSearchImpl（轮转）
        │                                / provider.*（单供应商）
        │          每个 provider/adapter ─► pkg/search/engine/* 或外部 API
        │                                  ─► pkg/antirobot（限流/TLS/UA）
        ├► postSearchFilter（单引擎模式的 score/maxsize 过滤，FilterByScore 来自 core）
        ├► enrichFetchedTopN ─► ensureWebFetch（未启用 cleanfetch/pdf_parser 时惰性初始化）
        │                      └► pkg/fetch/webfetch（fetch_top_n 抓正文，SSRF+HEAD 预检+字节上限）
        └► finishWebSearch ─► pkg/llm Summarizer（流式 progress，失败回退原文）
                             ─► cacheInst.Store
```

### academicsearch（学术搜索）

```
mcp/tool.go AcademicSearchHandler ─► doAcademicSearch
      ├► 缓存查询（key = query|timeRange|engines）
      ├► academicSearcher.SearchAcademicRaw
      │     └► pkg/search/adapter AcademicAdapter
      │          ├► query 识别 DOI/arXiv id（pkg/academic/paperid.go + lookup.go）
      │          │    → 单篇 lookup（OpenAlex+Crossref 并发 / 仅 arXiv）
      │          ├► 否则九源 Engine.Search 并行（pkg/academic/*）─► antirobot
      │          ├► 合并去重 + OA 补全（pkg/academic/unpaywall.go，UNPAYWALL_EMAIL）
      │          └► enhance.EnhanceAcademicResults（RRF + 引用数/期刊/PDF/新鲜度）
      └► formatAcademicResults（AcademicAdapter.MergeContentWithErrors，逐引擎错误透传）
```

### cleanfetch（网页抓取）

```
mcp/tool.go CleanFetch（url + urls 批量，合并去重上限 5）
  └► fetchCleanPage（每条独立）
        ├► validateURLSecurity（DNS rebinding 预检）+ headCheck（大小预检）
        ├► pkg/fetch/webfetch Fetcher.Fetch（TLS 指纹 + 大文本落盘 saved_to_file）
        └► 失败 ─► pkg/fetch/jina Reader.Fetch（需代理）
```

### pdf_parser（PDF 解析）

```
mcp/tool.go PDFParserHandler
      ├► parsePagesSpec（pages 表达式校验："1-10"、"1,3,5-7"）
      ├► resolvePDFPath（本地路径/file:// 与远程 http(s) 分流；远程走 SSRF+HEAD 预检）
      └► pkg/fetch/webfetch Fetcher.FetchPDFWithPages（pages + pdf_parser.max_pages）
            ├► 本地：go-webfetch ledongthuc 按页抽取（保留 CJK 清洗/结构启发式）
            ├► 远程：MinerU 精准 API（先尝试 page_ranges；请求页码超出单任务上限（mineru_page_limit，默认 600）或原件页数／URL 被拒绝时，下载原件按 mineru_page_batch_size 分批 qpdf 裁切串行上传，累计受 mineru_page_budget 预算约束，逐批经 MCP progress notification 推送进度，返回完整 ZIP 地址）
            │        或 webfetch 管线按 Content-Type 分流 PDF 解析
            └► 截断时 Result.Preamble 说明总页数与用 pages 继续
```

### file_search（本地文件检索）

```
mcp/tool_filesearch.go FileSearch
      ├► everything.ScopeQuery（folder/roots 白名单校验，路径项保留原始大小写；match_regex 走 regex: 函数避免污染范围限定）
      ├► everythingInst.Search（3x 超采候选，透传原生 i/w/m 过滤与 sort/ascending）
      └► everything.EnhanceItems（词汇对齐重排（name 1.0 / path 0.3，复用 enhance.LexicalAlignment）
            ├► 噪声目录降权（everything.noise_dirs，组件级匹配）
            └► everything.min_alignment 阈值过滤 → 截断 max_results，逐条一行输出
```

## 5. 网络集成测试的门控（internal/testenv）

真实外网集成测试（九学术源、四大引擎、真实抓取）统一走 `internal/testenv` 门控，
按网络场景动态启用或跳过，不产生环境性假失败：

```go
func TestBaiduSearch(t *testing.T) {
    testenv.Require(t, testenv.Baidu)          // 连通性探测门控
    resp, err := engine.Search(...)
    if testenv.HandleSearchError(t, err) {     // CAPTCHA/WAF/超时 → 场景类处理
        return
    }
    ...                                        // 真正的断言
}
```

模式由 `WS_TEST_NETWORK` 环境变量决定（默认 `auto`）：

| 模式 | 行为 |
|------|------|
| `auto`（默认） | 探测可达 → 运行；不可达 / 被反爬拦截（CAPTCHA/WAF）→ **Skip**（附原因） |
| `on`（CI/强制） | 必须运行；不可达或被拦截 → **t.Fatalf**（特殊 case 被禁用即报 fail，防止 Skip 掩盖引擎回归） |
| `off` | 一律 Skip |

另：`go test -short` 一律跳过网络集成测试（优先级高于 `WS_TEST_NETWORK=on`）。
注意：非场景类错误（引擎逻辑缺陷、断言失败）**不会**被 HandleSearchError 吞掉，照常 Fatal。

## 6. 改动落点速查（按调用链）

- **搜索请求进来到返回**的横切逻辑（缓存、超时、参数）→ `mcp/tool_search.go`（doWebSearch 一段）。
- **引擎结果如何变成最终列表**（并发、去重、过滤、排序）→ `pkg/search/hybrid` + `pkg/search/core`。
- **结果如何评分** → `pkg/search/enhance`。
- **结果如何渲染成 Markdown** → `pkg/search/core/format_md.go`（search/academic 共用）、`mcp/tool_academic.go formatAcademicResults`。
- **抓取/解析类工具的预检与回退** → `mcp/security.go`（validateURLSecurity/headCheck）+ `mcp/tool_cleanfetch.go` + `pkg/fetch/webfetch`。
- **LLM 摘要** → `mcp/tool_summarize.go streamSummarize`（progress 流式）+ `pkg/llm`。
- **工具注册/schema/描述** → `mcp/server.go`；handler 与参数结构 → `mcp/tool.go`。
- **服务生命周期/路由/鉴权** → `server/` + `mcp/server.go AuthMiddleware/RegisterRouter`。
