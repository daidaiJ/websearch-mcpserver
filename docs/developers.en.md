# Developer Guide — Packages, Interfaces & Key Flows

> For day-to-day development and AI agent collaboration in this repo. The canonical
> package layout is also mirrored in [AGENTS.md](../AGENTS.md); this document adds
> **per-package interface responsibilities**, a **"I want to change X" lookup table**,
> and the **end-to-end flows of the four MCP tools**.
>
> 中文版：[developers.md](developers.md)

## 1. Package Map & Dependency Direction

Dependencies point strictly downward (lower layers know nothing about upper ones):

```
cmd/ ──► server/ ──► mcp/ ──► pkg/search (composition root) ──► pkg/search/{mode,hybrid,apipool}
                                        │                                │
                                        │                                ├──► provider / adapter / enhance
                                        │                                └──► core (type contracts, leaf)
                                        └──► pkg/academic ──► antirobot
mcp/ ──► pkg/fetch/{webfetch,jina,mineru} ──► go-webfetch / MinerU
mcp/ searxng/ ──► pkg/search (root); pkg/cache  pkg/llm ──► pkg/search/core (contract leaf only)
all packages ──► pkg/config  pkg/log  pkg/client  pkg/proxy  pkg/antirobot  pkg/daemon
```

## 2. Package Responsibilities & Key Interfaces

### Top-level assembly

| Package | Responsibility | Key interfaces/types |
|---------|----------------|----------------------|
| `cmd/` | Entry point: config loading, platform init (Windows proxy detection etc.) | `main` |
| `server/` | HTTP lifecycle, routing, graceful shutdown | `Run`; shutdown order references `mcp.GetWebFetch/GetCache` |
| `searxng/` | SearXNG-compatible endpoint, reuses the same engine group | `mcp.GetSearchGroup()` |
| `mcp/` | MCP protocol layer: registration & schemas & handlers of the 4 tools, cache & summary orchestration | `server.go registerTools`, `tool.go` (params/wiring), `tool_search.go`, `tool_academic.go`, `tool_cleanfetch.go`, `tool_pdf.go`, `tool_summarize.go`, `security.go` (SSRF/HEAD pre-checks + per-hop redirect re-checks), `options.go` (webfetch lazily initializes for fetch_top_n) |

### pkg/search — search orchestration (7 subpackages)

| Subpackage | Responsibility | Key interfaces/types |
|------------|----------------|----------------------|
| `search` (root) | **Composition root**: `SearchGroup` assembly + public type aliases (external code keeps using `search.SearchResult` etc.) | `NewFromConfig(conf) *SearchGroup`, `SearchGroup{Primary, Fallback, Academic}` |
| `search/core` | Type-contract leaf: shared by all subpackages, **must not** import other search subpackages | `SearchInf`, `SearchResult`, `SearchTimeRanger`, `AcademicSearcher`, `ScoreBucket`, `FilterByScore/SortByScore`, `FormatMD` result formatters |
| `search/engine` | Low-level web engines (HTTP fetching + anti-bot params), implement `antirobot.Engine` | `Engine` + `Opts` of `baidu/bing/ddg/google` |
| `search/provider` | API provider adapters (implement `SearchInf`) + key pool | `NewTavilySearch/NewExaSearchWithResults/NewAnysearchSearch/NewDoubaoSearch/NewBaiduAISearch/NewBaiduSeach`, `KeyPool` (per-provider SK rotation), `KeyError` |
| `search/adapter` | Adapts "non-API" sources into `SearchInf` | `EngineSearchAdapter` (antirobot.Engine → SearchInf), `BingSearchAdapter`, `BaiduWithFallback`, `AcademicAdapter` |
| `search/apipool` | Cross-provider rotation strategy (sibling of hybrid) | `ApipoolSearchImpl` (round-robin/priority/weighted), `ApipoolProvider` |
| `search/hybrid` | Concurrent multi-engine strategy | `HybridSearchImpl` (concurrent calls, dedup & merge, per-engine `EngineFilter`, error logging) |
| `search/enhance` | Advanced scoring pipeline (works on `ScoreBucket`, engine-agnostic) | `EnhanceResults/EnhanceResultsMMR` (RRF/domain quality/lexical alignment/rare-terms/consensus boost/threshold), `ApplyMMR`, `EnhanceAcademicResults` (academic signals: citations/venue/PDF/recency) |
| `search/mode` | Search-mode construction: assembles Primary per `config.mode` | `BuildEngineMode/BuildBaiduMode/BuildTavilyMode/BuildExaMode/BuildAnysearchMode/BuildDoubaoMode/BuildApipoolMode/BuildHybridMode`, `InitBingEngine/InitBaiduWebEngine/InitGoogleEngine/InitDuckDuckGoEngine/InitAcademicEngine` |

### Other domain packages

| Package | Responsibility | Key interfaces |
|---------|----------------|----------------|
| `pkg/academic` | 9 academic source engines (arXiv/Crossref/OpenAlex/PubMed/S2/GS/EuropePMC/DBLP/DOAJ), DOI dedup, single-paper lookup, Unpaywall OA completion | `Engine.Search()`, `lookup.go` (DOI/arXiv id lookup) |
| `pkg/fetch/webfetch` | Enhanced fetching (SSRF/DNS-rebinding protection + PDF branch + large-text spill to file) | `Fetcher.Fetch`, `Fetcher.FetchPDFWithPages` |
| `pkg/fetch/jina` | Jina Reader fallback fetching (needs proxy) | `Reader.Fetch` |
| `pkg/fetch/mineru` | MinerU AI-enhanced PDF parsing (OCR/tables/formulas) | `Client.ParseURL/ParseFile` |
| `pkg/llm` | LLM client + search-result summarizer | `Client.Chat/ChatStream`, `Summarizer.Summarize/SummarizeStream` |
| `pkg/antirobot` | Anti-detection common layer (**cross-domain, stays top-level**) | `Engine`, `Searcher`, rate limiters, `TimeRange` |
| `pkg/cache` | SQLite cache (6h TTL, key includes intent/fetch_top_n) | `Cache.Lookup/Store/UpdateSummary` |
| `pkg/client` | Shared HTTP client for API providers (timeout/retry/proxy) | `Client` |
| `pkg/config` | All config structs + Viper loading + env overrides | `Load`, each `XxxConfig` |
| `pkg/proxy` | System proxy detection (registry/WinHTTP, 10s TTL) + transport pool | `DetectSystemProxy` |
| `pkg/daemon` | Reference-counted process management | — |
| `pkg/log` | Logging setup | — |

## 3. "I want to change X" Lookup Table

| Task | Look at / change |
|------|------------------|
| Add a low-level web engine (e.g. Sogou) | New package under `pkg/search/engine/` (implement `antirobot.Engine`) → adapter in `pkg/search/adapter` → register in modes (`pkg/search/mode`) → config switch in `pkg/config` |
| Add an API provider (e.g. Serper) | New adapter in `pkg/search/provider/` (implement `SearchInf`, use `KeyPool`) → register in `pkg/search/mode` (single-engine mode + optionally apipool/hybrid) → `pkg/config` + docs |
| Adjust mode composition / fallback order | `pkg/search/mode/factory.go`; the mode switch lives in root `factory.go` |
| Change multi-engine dedup/merge/per-engine filtering | `pkg/search/hybrid/hybrid.go` |
| Change apipool rotation strategy / key invalidation | `pkg/search/apipool/apipool.go` (key-pool primitive in `provider/keypool.go`) |
| Change scoring (RRF/boosts/threshold/MMR) | `pkg/search/enhance/` (web) and `enhance/academic_enhance.go` (academic); the ScoreBucket struct lives in `search/core` |
| Change result Markdown rendering | `pkg/search/core/format_md.go` (shared by all engines) |
| Change engine rate limits / anti-detection | `pkg/antirobot/` layer + engine Opts; **do not** re-implement rate limiting inside engine packages |
| Add/remove academic sources / DOI lookup / OA completion | `pkg/academic/`; the academic tool description is in `mcp/server.go buildAcademicToolDescription` |
| Add/modify MCP tool parameters | Matching `mcp/tool*.go` (Params struct + handler) + `mcp/server.go` (registration & description) |
| Add a config option | `pkg/config/config.go` + both `config.example.yaml` + `docs/configuration*.md` (tool params also `docs/search*.md`, `docs/api*.md`) |
| Change fetching behavior (UA/timeout/spill thresholds) | `pkg/fetch/webfetch/webfetch.go` (the underlying library is `github.com/daidaiJ/go-webfetch`) |
| Change PDF page ranges / MinerU policy | Tool params in `mcp/tool_pdf.go` (`pages`) + `pkg/fetch/webfetch` (`FetchPDFWithPages`) + config `pdf_parser.max_pages` |
| Change cache semantics (key/TTL/hit types) | `pkg/cache/` + `webSearchCacheQuery` et al. in `mcp/tool_search.go` |
| Change URL security checks / size pre-checks | `mcp/security.go` (MCP-layer pre-checks) + `pkg/fetch/webfetch` (library-level BlockPrivateIP, defense in depth) |

## 4. Key Flows of the Four Tools

### smartsearch (web search)

```
mcp/server.go registerTools (registers With/NoIntent schema pair depending on LLM flag)
  └► mcp/tool_search.go WebSearchWithIntent/NoIntent ─► doWebSearch
        ├► cache lookup cacheInst.Lookup (key = query|fetch_top_n)
        ├► searchapi.SearchRaw (= SearchGroup.Primary)
        │     └► assembled by pkg/search/factory.NewFromConfig:
        │          mode.BuildXxxMode ─► hybrid.HybridSearchImpl (concurrent multi-engine)
        │                                / apipool.ApipoolSearchImpl (rotation)
        │                                / provider.* (single provider)
        │          each provider/adapter ─► pkg/search/engine/* or external APIs
        │                                  ─► pkg/antirobot (rate limit/TLS/UA)
        ├► postSearchFilter (single-engine score/maxsize filtering, FilterByScore from core)
        ├► enrichFetchedTopN ─► ensureWebFetch (lazily initializes when cleanfetch/pdf_parser are off)
        │                      └► pkg/fetch/webfetch (fetch_top_n body extraction, SSRF+HEAD pre-checks+byte cap)
        └► finishWebSearch ─► pkg/llm Summarizer (streamed progress, falls back to raw)
                             ─► cacheInst.Store
```

### academicsearch (academic search)

```
mcp/tool_academic.go AcademicSearchHandler ─► doAcademicSearch
      ├► cache lookup (key = query|timeRange|engines)
      ├► academicSearcher.SearchAcademicRaw
      │     └► pkg/search/adapter AcademicAdapter
      │          ├► DOI/arXiv id detection (pkg/academic/paperid.go + lookup.go)
      │          │    → single-paper lookup (OpenAlex+Crossref concurrent / arXiv only)
      │          ├► otherwise 9-source Engine.Search in parallel (pkg/academic/*) ─► antirobot
      │          ├► merge & dedup + OA completion (pkg/academic/unpaywall.go, UNPAYWALL_EMAIL)
      │          └► enhance.EnhanceAcademicResults (RRF + citations/venue/PDF/recency)
      └► formatAcademicResults (AcademicAdapter.MergeContentWithErrors, per-engine error pass-through)
```

### cleanfetch (web fetching)

```
mcp/tool_cleanfetch.go CleanFetch (url + urls batch, merged & deduped, cap 5)
  └► fetchCleanPage (independent per URL)
        ├► validateURLSecurity (DNS-rebinding pre-check) + headCheck (size pre-check)
        ├► pkg/fetch/webfetch Fetcher.Fetch (TLS fingerprint + large-text spill saved_to_file)
        └► on failure ─► pkg/fetch/jina Reader.Fetch (needs proxy)
```

### pdf_parser (PDF parsing)

```
mcp/tool_pdf.go PDFParserHandler
      ├► parsePagesSpec (pages expression validation: "1-10", "1,3,5-7")
      ├► resolvePDFPath (local path/file:// vs remote http(s); remote goes through SSRF+HEAD pre-checks)
      └► pkg/fetch/webfetch Fetcher.FetchPDFWithPages (pages + pdf_parser.max_pages)
            ├► local: go-webfetch ledongthuc per-page extraction (keeps CJK cleanup / structure heuristics)
            ├► remote: MinerU standard API (page_ranges first; when requested pages exceed the per-task limit (mineru_page_limit, default 600) or source page/URL is rejected, the source is downloaded, cropped per mineru_page_batch_size batches with qpdf and uploaded serially under the mineru_page_budget with per-batch MCP progress notifications, returning a complete ZIP URL)
            │        or webfetch pipeline routing PDF by Content-Type
            └► on truncation Result.Preamble states total pages and how to continue with pages
```

## 5. Gating of Network Integration Tests (internal/testenv)

Real-network integration tests (9 academic sources, 4 engines, real fetching) go through
the `internal/testenv` gate and are dynamically enabled or skipped by network scenario,
so no environment-caused false failures:

```go
func TestBaiduSearch(t *testing.T) {
    testenv.Require(t, testenv.Baidu)          // connectivity probe gate
    resp, err := engine.Search(...)
    if testenv.HandleSearchError(t, err) {     // CAPTCHA/WAF/timeout → scenario handling
        return
    }
    ...                                        // real assertions
}
```

The mode is selected by the `WS_TEST_NETWORK` environment variable (default `auto`):

| Mode | Behavior |
|------|----------|
| `auto` (default) | Reachable → run; unreachable / blocked by anti-bot (CAPTCHA/WAF) → **Skip** (with reason) |
| `on` (CI/forced) | Must run; unreachable or blocked → **t.Fatalf** (a disabled special case must fail rather than skip, so skips cannot mask engine regressions) |
| `off` | Always skip |

Also: `go test -short` always skips network integration tests (takes precedence over `WS_TEST_NETWORK=on`).
Note: non-scenario errors (engine logic bugs, assertion failures) are **not** swallowed by
HandleSearchError — they keep failing as usual.

## 6. Where to Change What (by flow)

- Cross-cutting request logic (cache, timeouts, params) → `mcp/tool_search.go` (`doWebSearch`).
- How engine results become the final list (concurrency, dedup, filter, sort) → `pkg/search/hybrid` + `pkg/search/core`.
- How results are scored → `pkg/search/enhance`.
- How results render to Markdown → `pkg/search/core/format_md.go` (shared by search/academic), `mcp/tool_academic.go formatAcademicResults`.
- Pre-checks & fallbacks of fetching tools → `mcp/security.go` (validateURLSecurity/headCheck) + `mcp/tool_cleanfetch.go` + `pkg/fetch/webfetch`.
- LLM summaries → `mcp/tool_summarize.go streamSummarize` (streamed progress) + `pkg/llm`.
- Tool registration/schema/descriptions → `mcp/server.go`; handlers & param structs → `mcp/tool*.go`.
- Service lifecycle/routing/auth → `server/` + `mcp/server.go AuthMiddleware/RegisterRouter`.
