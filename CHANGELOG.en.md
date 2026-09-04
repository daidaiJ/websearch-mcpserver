# Changelog

[English](CHANGELOG.en.md) | [中文](CHANGELOG.md)

## v3.3.0 — 2026-09-04

### Added
- **AnySearch engine**: integrated the [AnySearch API](https://www.anysearch.com/docs) (`POST /v1/search` + Bearer auth, envelope code=0/-1, invalid keys return 401/403); new `mode=anysearch` single-engine mode, and joined `apipool` / `hybrid`. The API has no exclude_domains support, so `black_list_host` is filtered locally
- **apipool weighted load balancing**: `apipool.strategy` gains a `weighted` strategy that picks the starting provider by weighted random (request bursts naturally spread across providers); a provider's effective weight = configured weight × currently available SK count (shrinks automatically while SKs cool down, self-healing); explicit `0` excludes a provider from starting selection, all-zero degrades to round-robin; the Baidu web search fallback keeps a fixed weight of 1
- **apipool.weights config**: per-key weights, defaults `anysearch=30000`, `baidu=1500`, `tavily=1200`, `exa=1200`, overridable per provider
- **Env var `ANYSEARCH_API_KEY`**: same mechanism as `BAIDU_SK` / `TAVILY_SK` / `EXA_API_KEY` (viper BindEnv + applyKnownEnv backfill)

### Changed
- **apipool.engines default order**: `[baidu, tavily, exa]` → `[anysearch, baidu, tavily, exa]` (anysearch has the largest free quota); behavior is unchanged when no anysearch key is configured
- **KeyPool per-provider SK dedup**: duplicate keys (compared after trim) keep only one entry, so the same key is no longer counted as multiple available keys for rotation and weight accumulation; an Info log is emitted when deduplication happens

## v3.2.1 — 2026-09-03

### Added
- **Stateless MCP HTTP mode**: new `mcp_stateless` config switch (default `false` = stateful); when enabled, each POST is handled independently with no initialize handshake or `Mcp-Session-Id` session, making reverse-proxy/load-balancer horizontal scaling easier; GET SSE long connections return 405. Aligns with the stateless-first direction of the MCP 2026-07-28 spec; all tools of this service are request-response, so stateless mode loses nothing
- **GHCR image publishing**: tag pushes now build and publish images to `ghcr.io` (closes #3); release artifacts include a linux/amd64 image tarball; Dockerfile fixes (golang:1.26 alignment, whole-module build, version injection)

### Changed
- **Dead engines disabled by default**: new `baidu.web_enabled` (default `false`) — the Baidu web search engine (tn=json scraping) is CAPTCHA-blocked in testing, disabled by default; deployments with clean egress IPs may enable it explicitly

## v3.2.0 — 2026-08-30

### Added
- **Academic search expanded to 9 engines**: new Europe PMC (biomedical, PubMed supplement), DBLP (CS conference/journal index), DOAJ (open-access journals) — all directly reachable from China; `academicsearch` tool description and engine-selection advice updated
- **Per-engine error passthrough**: failed academic engines are no longer swallowed silently — recorded in `EngineErrors` and surfaced as a "some engines failed this run" note appended to results; all-failed searches get a per-engine error summary
- **Semantic Scholar API key**: new `academic.semantic_scholar_api_key` (env `SEMANTIC_SCHOLAR_API_KEY`); with a key, 429/503 backs off and retries, then permanently degrades to anonymous in-process; anonymous-channel 429s error out immediately
- **Google Scholar retry hardening**: 403/429/503 exponential backoff (1.5s→3s + jitter) with rotating desktop user agents; CAPTCHA redirects fail fast without wasting retries; results now carry a DOI (extracted from the landing-page URL, falling back to abstract text)
- **Cross-engine DOI dedup (dual-key)**: academic results merge by DOI + URL keys — either key matching counts as the same paper, fixing missed merges when one side has only a DOI; new shared `antirobot.ExtractDOI` utility

### Changed
- **DDG rate-limit protection, three layers**: measured clamping (1/s, 6/min — looser configs clamped with a warning); 202/429 triggers a process-level cooldown (doubling, 2-min cap, Retry-After priority); cooldown avoidance is budget-aware — waits and retries within the search timeout budget, gives up fast when the wait is doomed to exceed it
- **arXiv rate-limit hardening**: same pattern as DDG — official Tou 1 req/3s minimum interval + built-in clamping (1/s, 12/min) + 429/503 cooldown avoidance with budget awareness
- `antirobot.RateLimiter` gained a minimum-interval constraint (`WithMinInterval`); `ParseRetryAfter` promoted to a shared antirobot utility (DDG now delegates); fixed the DDG cooldown error message double-doubling the actual wait

### Documentation
- README / AGENTS engine counts and capability descriptions updated to 9 academic engines; AGENTS stale "capability gaps" table replaced with current capability boundaries
- Google engine config now documents the JS-challenge timeline (2025-01 gray rollout → 2025 H2 full hardening) and the "cannot be bypassed by masquerading" conclusion
- Config docs (zh/en) and both `config.example.yaml` files updated for `semantic_scholar_api_key`, `disable_europepmc/dblp/doaj`
- Removed the fully-implemented planning doc `issues/academic-search-improvements.md`

## v3.1.0 — 2026-08-26

### Security
- **Loopback-only by default**: new `host` option (default `127.0.0.1`); the daemon no longer binds all interfaces by default. An Error-level warning is logged at startup when `0.0.0.0`/`::` is used without a token
- **Optional Bearer auth for business endpoints**: new `auth_token` (env `WEBSEARCH_TOKEN`); `/mcp` and `/searxng/search` accept `Authorization: Bearer` or `X-API-Key`. No token configured = no auth (backward compatible); `__admin/*` keeps the local-only protocol

### Fixed
- **30s default upstream timeout**: `pkg/client` DefaultClient now sets a timeout (configurable via `upstream_timeout_sec`, 0 = no timeout); a black-hole upstream can no longer hang MCP tools forever
- **KeyPool concurrency bug**: search adapters return errors carrying the actual key used; apipool cools down the exact key. `MarkLastInvalid` is marked not concurrency-safe and banned for new code
- **Single SearchGroup**: MCP and SearXNG now share one engine set (`GetSearchGroup`) — rate limits and key state no longer split. SearXNG returns 400 for empty `q` and 503 when no engine is available instead of panicking
- **Single-engine results no longer silently capped at 4**: without per-engine `max_size`, only the global `max_size` applies; apipool/baidu_ai result counts are configurable again
- **hybrid logs engine failures**: Warn log per failed engine + error summary when all fail (no API keys in logs)
- **Proxy transport cached per endpoint**: no per-request `Clone`, connection pooling works again; system proxy detection gets a 10s TTL cache
- **WinHTTP proxy string out-of-bounds**: UTF-16 read is bounded by the buffer up to NUL; the fixed 512-uint16 cast is gone
- **MinerU only for PDF URLs**: remote URLs go to the precise API only when the path ends with `.pdf` (disable via `mineru_remote_pdf`, default true); plain web pages no longer burn MinerU quota
- **viper env backfill**: `Load()` now calls `applyKnownEnv` explicitly, so `TAVILY_SK` etc. work even when the yaml omits the field
- **Shutdown order**: HTTP Shutdown first, then WebFetch/SQLite close — in-flight requests no longer hit `sql: database is closed`
- **start TOCTOU / stale PID**: PID is written only after the listener is bound; port-in-use returns an error instead of panicking
- **Jina private-IP check**: `net.ParseIP` + `IsPrivate()`; public ranges like `172.32.0.0/11` are no longer misclassified
- **Cache upsert**: unique index on `(query, intent, academic)` + `ON CONFLICT DO UPDATE`; legacy duplicate rows are cleaned on open
- **daemon PostShutdown leak**: response body closed properly; **CleanupScheduler.Stop** now actually waits for the goroutine (cancellable)

### Changed
- **"Zero config" = an editable preset yaml**: the first `start` (without `-c`) writes a `config.yaml` identical to `config.example.yaml` next to the executable; `install` / `cli init` share `EnsureExampleFile` (idempotent, never overwrites user edits)
- Network integration tests uniformly skip under `testing.Short()` (Bing / Crossref / arXiv / OpenAlex / Semantic Scholar / Google Scholar); `go test -short ./...` makes no external requests

### Docs
- Config / install / API docs (EN & ZH) updated for `host`, `auth_token`, `upstream_timeout_sec`, `mineru_remote_pdf`; MCP client registration shows headers examples; README notes that Google / DDG / Crossref / Google Scholar are unstable on direct China connections

## v3.0 — 2026-08-20

### Added
- **stdio CLI**: standalone `cmd/cli` entry that speaks MCP over stdin/stdout by default; uses in-memory `mode: engine` defaults when no config file is found
- Releases now also publish `websearch-mcp-cli-{linux,windows,darwin}-{amd64,arm64}` plus matching `.sha256` files
- CLI commands: `init` writes an example config, `version` prints the version; logs go to stderr so they do not corrupt JSON-RPC
- Docs: HTTP vs stdio config notes (`docs/config.en.md`)

## v2.15.0 — 2026-08-14

### Changed
- **pdf_parser local-first parsing**: local PDFs extract text via the PDF library (ledongthuc/pdf via go-webfetch) first and return immediately on success; MinerU is considered only when there is no text layer (scanned/image PDFs)
- **On-demand MinerU OCR fallback**: requires explicit `pdf_parser.mineru_ocr`; without it, errors hint to enable OCR; with it, uses MinerU Agent/Standard API and preserves the local parse error if OCR fails
- **MinerU client init condition**: changed from "`enabled` or Token" to "Token or `mineru_ocr`", so enabling pdf_parser alone no longer prefers MinerU upload for every local file
- `PDFParserConfig` adds `MinerUOCREnabled()`; tool description and config examples updated for the new strategy

### Tests
- Added unit tests for `needsOCRFallback`, MinerU OCR init conditions, and OCR hint when OCR is disabled

## v2.14.0 — 2026-08-02

### Added
- **Intelligent relevance scoring pipeline (Wigolo)**: multi-engine results use RRF (Reciprocal Rank Fusion, K=60) fusion ranking, combined with lexical alignment, rare-term/phrase contiguity matching, domain-quality penalties, and multi-engine consensus / authority-site / recency boosts — fully heuristic, no AI model required
  - Added `enhance.go` (RRF / ConsensusBoost / ApplyScoreFloor), `enhance_domain.go` (DomainQuality / AuthorityBoost), `enhance_text.go` (LexicalAlignment / RareTermsFactor), `enhance_intent.go` (intent classification)
- **MMR diversity re-ranking**: greedy MMR re-ranking (Token Jaccard similarity) runs after threshold filtering and before `max_size` truncation, breaking up highly similar results on the same topic (mirror sites / reposts / same-source blogs), with Top-1 protection
  - Added `mmr.go` (ApplyMMR); `enhance_text.go` exports `JaccardSimilarity`
  - Config: `smartsearch.mmr.enabled` (default true), `lambda` (default 0.7), `target_count` (default 0)
- **Academic search scoring enhancement**: six academic engines fused via RRF with academic-specific signals — citation count (log-compressed, clamped to [1.0, 1.7]), high-impact journal/conference boost, PDF availability, recency factor (×1.15 within 1 year for time-sensitive queries); low-score papers auto-filtered (Top-1 + per-engine floor)
  - Added `academic_enhance.go` (EnhanceAcademicResults / CiteFactor / JournalBoost / AcademicRecencyFactor); relevance scores returned by OpenAlex / Semantic Scholar participate in per-engine ranking
  - Config: `academic.enhance` (default true), `academic.threshold` (default 0.02), kept independent of `smartsearch` (decoupled tool configs)
- **LLM summary streaming**: the `smartsearch` summary stage pushes tokens in real time via MCP `notifications/progress` (search complete → stage notification → token stream); auto-cancels on client disconnect and falls back to non-streaming summary on failure
  - `pkg/llm` adds `ChatStream` (OpenAI-compatible SSE streaming); `pkg/summarizer` adds `SummarizeStream`

### Changed
- **Low-quality context is truly pruned**: fixed `ApplyScoreFloor` by removing the "per-engine floor retention" logic; results scoring below the global threshold (default 0.05) are discarded, keeping only Top-1 (with a minimum of 2 results retained)

### Fixed
- **Admin endpoints unreachable**: `config.Load` now defaults `port` to 8338 when unset, avoiding binding to random port `:0` that made daemon/CLI and agents unable to reach `http://127.0.0.1:{port}/__admin`
- **Baidu web search returning empty results**: `web_search` requests now include the required Qianfan field `search_source: "baidu_search_v2"`; `baidu.go` adds an HTTP status check that surfaces auth/quota/parameter errors on non-200 responses instead of mislabeling them as "empty content"

### Tests
- Added `TestEnhanceFiltersLowQuality` observability test verifying low-quality results are pruned (5→2)
- Added MMR tests (Top-1 protection / diversity / λ=1 degeneration / targetN truncation) and academic enhancement tests (citation / journal / recency signals / per-engine floor)
- Added `ChatStream` unit tests (httptest mock SSE server: normal stream / HTTP error / ctx cancellation / malformed line skipping)
- Baidu live-API integration tests now skip when the environment is unavailable (network/auth/quota/empty result), preventing `go test ./...` from failing on external dependency outages

## v2.13.0 — 2026-07-19

### Changed
- **Baidu AI search model optional**: free Baidu search when `baidu.model` is empty (no LLM cost); LLM-powered search only when a model name is explicitly set; `enable_ai_search` stays `true` by default
- **Upgraded go-webfetch to v0.2.0**: cleanfetch engine inherits TLS fingerprint spoofing (tls-client Chrome 131) + retry backoff + system proxy support for better anti-bot capabilities

### Added
- cleanfetch new config fields: `use_system_proxy` (default false) and `max_retries` (default 3)

## v2.12.0 — 2026-07-15

### Added
- **`apipool` search mode**: new API Key pool rotation mode, Baidu AI Search + Tavily + Exa concurrent dedup
  - Load balancing: provider selected first (round-robin), then SK rotated within that provider (KeyPool)
  - Baidu web search auto-participates as key-free fallback engine
  - Config: `mode: apipool`
- **Baidu AI Search endpoint**: new `chat/completions` intelligent search API (`baidu_ai.go`), returns LLM-generated answers + reference sources
  - `baidu.enable_ai_search` controls endpoint selection (default `true` = AI search, `false` = legacy web search)
  - Supports `model` (default `ernie-4.5-turbo-32k`), `search_source` (default `baidu_search_v2`), `enable_reasoning`, `enable_deep_search`, `search_mode` configs
- **API Key multi-key rotation (KeyPool)**: `baidu`/`tavily`/`exa` all support `sk_list` multi-key rotation
  - `sk_list` takes priority when non-empty; falls back to `api_key` as single-element list
  - KeyPool is thread-safe with round-robin rotation
  - Supports key invalidation (`MarkInvalid`), auto-recovers after 30 minutes
  - When all keys are invalid, degrades to the earliest-recovering key

### Changed
- `baidu` mode now uses AI search endpoint by default (`enable_ai_search: true`), configurable to switch back to legacy `web_search`
- `hybrid` mode Baidu engine also uses `enable_ai_search` config for endpoint selection
- `NewBaiduSeach`, `NewTavilySearch`, `NewExaSearch`/`NewExaSearchWithResults` constructors changed to accept `*KeyPool` parameter
- `lookbackDaysToRecency(0)` fixed to return `semiyear` (default) instead of `day`

## v2.11.0 — 2026-07-13

### Added
- **Exa Web Search API engine**: new Exa general search with `type: auto` automatic type selection, `highlights` summary extraction, and `excludeDomains` domain filtering
  - `mode=exa` for standalone Exa; `mode=hybrid` auto-includes Exa in mixed search
  - Config: `exa.api_key` (env `EXA_API_KEY`), `exa.num_results` (default 5), `exa.lookback_days` (default 90)
- **API engine time range config**: all API engines (Tavily, Baidu Qianfan, Exa) now support the unified `SearchTimeRanger` interface for time-filtered search results
  - Tavily: maps to `time_range` parameter (day/week/month/year)
  - Baidu Qianfan: maps to `search_recency_filter` parameter (day/week/month/semiyear/year)
  - Exa: maps to `startPublishedDate` / `endPublishedDate` date range
- **smartsearch tool time_range parameter**: new `time_range` parameter (in months) for dynamic search time range control, defaults to 3 months
  - Example: `time_range=1` for last month, `time_range=6` for last 6 months, `time_range=12` for last year
- New `SearchTimeRanger` optional interface; engines with time range support implement it automatically
- New `HybridSearchImpl.SearchRawWithTimeRange`, passes time range through to supporting sub-engines
- New Exa, Tavily, Baidu Qianfan integration tests (API keys stored in `config.test.yaml`, gitignored)

### Changed
- `config.example.yaml` mode comment now includes `exa` mode description
- Baidu Qianfan `search_recency_filter` changed from hardcoded `semiyear` to configurable, default unchanged

## v2.10.0 — 2026-07-12

### Added
- **DuckDuckGo search engine**: new DuckDuckGo general search (requires proxy), `html.duckduckgo.com/html/` POST + goquery parsing, auto-joins engine/hybrid modes
- **HEAD pre-check for large files**: cleanfetch sends HEAD request to check Content-Length before fetching, rejects if exceeds threshold (default 10MB, configurable via `max_fetch_size_mb`)
- **DNS rebinding protection**: cleanfetch resolves target domain DNS before fetching, checks all IPs against private/internal ranges, forms dual protection with go-webfetch's BlockPrivateIP
- **Jina Reader DNS protection enhancement**: `isPrivateHost()` now includes DNS resolution check, fixes bypass risk of pure string matching

### Changed
- **Google engine disabled by default**: Google search blocked by anti-bot mechanisms (TLS fingerprint + JS Challenge), defaults to `google.enabled: false`, needs explicit enable
- `CleanFetchConfig` added `MaxFetchSizeMB` field (default 10)
- `Config` added `DuckDuckGoConfig` and `GoogleConfig` structs

### Fixed
- **Google anti-bot detection enhancement**: `detectSorry()` adds JS Challenge page recognition (`/httpservice/retry/enablejs`, `SG_SS`)
- **Google parsing defense**: `parseResults()` adds `div#rso`/`div#search` container pre-check, returns empty for non-search-result pages
- **Google parsing strategy enhancement**: added SearXNG-style `a[data-ved]:not([class])` selector as primary parsing path, `div.g` as fallback

## v2.9.0 — 2026-07-12

### Added
- **MinerU AI-enhanced PDF parsing**: `pdf_parser` tool integrates [MinerU](https://mineru.net) document parsing platform, supporting intelligent table/formula/multi-column/image recognition
  - **Dual API mode with automatic switching**:
    - With Token → Standard API (`/api/v4`), supports remote URL input, ≤200MB/200 pages, ZIP output with Markdown + JSON
    - Without Token → Agent Lightweight API (`/api/v1/agent`), supports local file signed upload, ≤10MB/20 pages, Markdown output
  - **Local files prioritize MinerU**: Agent API auto-signed-uploads local PDFs, silently falls back to local parsing (go-webfetch) on failure
  - **Remote URLs prioritize MinerU**: Standard API parses remote URLs, falls back to local webfetch on failure
  - **Friendly error handling**: API error codes translated to Chinese prompts; raw errors only logged, never exposed to clients
  - Config: `mineru_token` (env `MINERU_TOKEN`), `mineru_model` (pipeline/vlm), `mineru_ocr`, `mineru_formula`, `mineru_table`, `mineru_lang`
- New `pkg/mineru/` package: MinerU API client implementation + unit tests + integration tests

### Changed
- `PDFParserConfig` extended with MinerU fields, new `MinerUEnabled()`, `GetMinerUModel()` helper methods
- `webfetch.Fetcher` auto-detects MinerU config during initialization and creates client
- `pdf_parser` MCP tool description dynamically shows MinerU enhancement status

## v2.8.0 — 2026-07-11

### Fixed
- **cleanfetch system proxy auto-detection**: fixed cleanfetch being unable to access overseas sites, now supports automatic system proxy detection

## v2.7.0 — 2026-06-25

### Added
- **Cache explicit toggle**: `CacheConfig` adds `cache.enabled` field (`*bool`), supports explicit cache enable/disable
  - Not set → judge by `storage_path` presence (backward compatible)
  - Explicit `false` → force disable cache (ignores `storage_path`)
  - Explicit `true` → force enable cache
- **English documentation**: added `README.EN.md`, `docs/config.en.md`, `docs/api.en.md`, `CHANGELOG.en.md` with language switch links at the top of each document
- Added `CacheEnabled()` unit tests (6 cases covering all branches)

## v2.6.0 — 2026-06-24

### Added
- **SmartSearch score filtering**: per-engine `min_score` / `max_size`, global `max_size`, `show_meta` controls source and score display
  - Engine returns score: filter by `min_score`, keep `max_size` results
  - Engine returns no score: ignore `min_score`, take `min(max_size, ⌈global_max_size / engine_count⌉)`
  - Global `max_size`: with scores → sort by score and truncate; without scores → round-robin distribution across engines
  - `show_meta` controls whether engine source and relevance score appear in output (default true)
- **API engine naming distinction**: Tavily → `tavily_api`, Baidu Qianfan → `baidu_api`; Baidu web search keeps `baidu`
- Engine `Name()` method unified into `SearchInf` interface

## v2.5.0 — 2026-06-03

### Added
- **System proxy auto-detection**: reads Windows registry (`ProxyEnable` / `ProxyServer`) and environment variables (`HTTP_PROXY` / `HTTPS_PROXY`) by default; Clash, V2RayN and similar proxy software work automatically without manually configuring `proxy.enabled` or restarting the service
  - `pkg/proxy/sysproxy.go` — cross-platform system proxy detection (Windows registry + WinHTTP + env vars)
  - `pkg/proxy/detector.go` — background polling detector, 30s cycle for proxy change detection with callbacks
  - `pkg/proxy/proxy.go` — `DynamicProxyTransport` request-level dynamic proxy resolution, resolves proxy endpoint per request
- **Jina Reader no longer depends on proxy.enabled**: configure `jina.api_key` to enable; proxy auto-detected by system
- **Global rate-limit retry**: all HTTP clients handle 429 responses automatically, read `Retry-After` header and wait before retrying (max wait 5s, returns 429 directly on limit exceeded)
- **arXiv engine rate limiting**: built-in 1 req/s limiter (arXiv official recommendation), waits on limit rather than triggering 429

### Changed
- **Engine initialization refactored**: Google, Semantic Scholar, Google Scholar always initialize (unless explicitly disabled via `disable_*`), proxy resolved dynamically at request level, toggling proxy no longer requires restart
- **Jina Reader removed proxy.enabled gate**: `NewFromConfig` only checks `jina.api_key`, proxy resolved by `ProxyResolver` dynamically
- **Google engine HTTP client**: changed from static `http.ProxyURL` to `DynamicProxyTransport`, resolves proxy at request time
- **Academic engine HTTP client**: changed from `proxy.NewHTTPClient(endpoint)` to `proxy.NewDynamicHTTPClient(resolver)`, supports runtime proxy switching; domestic engines also use retry-enabled client
- **Test URL updates**: `TestFetchWebPage` / `TestCleanFetch_WebFetchSuccess` test URLs changed from `arthurchiao.art` (unreachable) to `wmyskxz.cn`

### Backward Compatibility
- `proxy.enabled: true` + `proxy.endpoint` still works (explicit proxy mode)
- `proxy.enabled: false` still works (explicit proxy disable)
- `proxy.enabled` not set: behavior changed from "no proxy" to "auto-detect system proxy"

### New Files
- `pkg/proxy/sysproxy.go` — system proxy detection core logic
- `pkg/proxy/sysproxy_windows.go` — Windows registry + WinHTTP detection implementation
- `pkg/proxy/sysproxy_other.go` — non-Windows platform placeholder (env vars only)
- `pkg/proxy/detector.go` — background proxy change detector

## v2.4.0 — 2026-06-02

### Added
- **Baidu web search engine**: implemented by referencing SearXNG baidu.py, uses `tn=json` JSON API to directly fetch Baidu search results, no API key required
  - `mode=baidu` uses as primary engine when no SK; SK failure auto-falls back to web search
  - `mode=engine` searches concurrently with Bing by default
  - `mode=hybrid` automatically participates in mixed search
- **Google search engine**: implemented by referencing SearXNG google.py, parses Google search results from HTML, requires proxy
  - Auto-enables when `proxy.enabled=true`, supports CAPTCHA detection, CONSENT cookie bypass
  - `mode=engine` / `mode=hybrid` automatically joins concurrent search when proxy enabled
- **Global rate limit config**: new `rate_limit` section (`per_sec` / `per_min`), applies uniformly to all search engines (default 3/s, 60/min)

### Changed
- **`engine` mode engine combination**: from Bing-only to Baidu web search + Bing concurrent (Google joins when proxy enabled)
- **`hybrid` mode Baidu strategy**: with SK uses `BaiduWithFallback(SK, web search)`, SK failure auto-falls back; without SK uses web search directly
- **`baidu` mode enhanced**: without SK no longer falls back to Bing, uses Baidu web search engine instead
- **Rate limit defaults increased**: all engines default 3/s, 60/min (was Bing 1/s, 20/min)
- Bing rate limit config migrated from `bing.per_sec` / `bing.per_min` to global `rate_limit` section

### New Files
- `pkg/baidu/` — Baidu web search engine (engine + opts + 15 unit tests)
- `pkg/google/` — Google search engine (engine + opts + 17 unit tests)
- `pkg/search/engine_adapter.go` — generic engine adapter (antirobot.Engine → SearchInf)
- `pkg/search/baidu_fallback.go` — Baidu SK fallback wrapper

## 2026-05-28

### Added
- **cleanfetch enhanced web fetch**: integrates `go-webfetch` library, fetches web content without proxy, auto-falls back to Jina Reader on failure (requires proxy)
  - `cleanfetch.enabled` controls switch (default false, old configs don't enable)
  - Large content auto-stored to temp files, configurable output directory, TTL, inline thresholds
- **pdf_parser PDF parsing tool**: converts local PDF files to Markdown (`pdf_parser.enabled` controls, default false)
- **hybrid mode Bing mixed search**: Bing as native engine searches concurrently with API engines (Baidu/Tavily) in hybrid mode

### Changed
- cleanfetch tool now only needs `cleanfetch.enabled: true` to use, no longer requires proxy and Jina API key
- Go version upgraded to 1.26 (go-webfetch dependency requirement)

## 2026-05-26

### Added
- **Windows auto-start**: `install` / `uninstall` commands, uses COM API (ole32.dll) to create shortcuts, no PowerShell dependency
- **PubMed academic engine**: authoritative biomedical literature database, direct access from China
- **Google Scholar academic engine**: all-discipline academic search, requires proxy
- **MCP tool split**: `smartsearch` (general search) + `academicsearch` (academic search) as separate tools; `academicsearch` supports `engines` / `time_range` / `page` parameters
- **Academic search parallelization**: multi-engine concurrent requests, results deduplicated by URL + grouped normalized sorting
- **BingFallback config**: `academic.bing_fallback` controls whether to use Bing as fallback for academic search
- **proxy config**: only overseas academic engines (Semantic Scholar, Google Scholar) use proxy
- **CI auto-release**: GitHub Actions workflow, auto-builds linux/windows binaries and publishes Release on tag push, with SHA256 checksums

### Refactored
- **Extracted server package**: `RunServer`, admin handlers, reference count logic extracted from `cmd/main.go` to exportable `server` package, supports embedding as Go module
- **Academic engine independent modules**: new `pkg/academic` (6 engines independently implemented) and `pkg/antirobot` (shared engine framework: Engine interface, Searcher orchestrator, rate limiter)
- **Bing package slimmed**: `pkg/bing` retains only Bing general search engine + anti-bot logic

### Documentation
- New [docs/api.md](docs/api.md): Go Module API and HTTP API complete documentation
- New [docs/configuration.md](docs/configuration.md): config reference, defaults quick reference, environment variable overrides
- README fully rewritten: simplified structure, feature highlights, operations reference, troubleshooting guide

## 2026-05-23

### Added
- **cleanfetch web fetch tool**: obtains clean web content via Jina Reader API for specified URL, reduces anti-bot blocking risk
  - Only registers when `jina.api_key` is configured, doesn't affect existing features
  - Returns clear Chinese prompts for common HTTP errors (403/404/429 etc.)
  - SSRF protection: URL protocol validation, internal address blacklist
  - Client timeout (30s) to prevent goroutine leaks

### Optimized
- **Academic search result enhancement**: preserves paper metadata (author, DOI, type), auto-distinguishes papers and web results during formatting
- **Cache system improvements**:
  - Supports `academic` parameter distinction, prevents academic/non-academic cache mixing
  - Database auto-migration, backward compatible with old cache
  - Query optimization: two-step query fully utilizes indexes
- **Site blocking unified**: `black_list_host` and `bing.blocked` auto-merged, SearXNG backend synchronized
- **String concatenation optimization**: `MergeContent` uses `strings.Builder`, complexity reduced from O(n²) to O(n)
- **Sorting optimization**: `HybridSearchImpl` bubble sort replaced with `sort.Slice`

### Fixed
- Academic search failure no longer silently falls back to general search, returns clear error message
- Tavily search correctly uses `exclude_domains` for site filtering
- `describeHTTPError` uses `fmt.Sprintf` instead of unnecessary `fmt.Errorf`

---

## 2026-05-20

### Added
- `smartsearch` tool auto-removes `intent` parameter when LLM summary not enabled, saving client context tokens
- MCP service adds 30s heartbeat + 5min idle session auto-cleanup
- HTTP Server adds timeout config (ReadHeader 10s / Read 60s / Idle 120s)
- Async summary goroutine adds panic recover

### Fixed
- Dockerfile missing startup parameter causing container to exit immediately

---

## 2026-05-15

### Added
- `engine` search mode: no API key needed, uses Bing general search + academic search engines
- Academic search engine integration: arXiv, Crossref, OpenAlex, Semantic Scholar
- MCP tool adds `academic` parameter
- `black_list_host` site blocking config (applies to Bing and Tavily)

### Optimized
- LLM summary prompts: proactively filters low-quality content, merges duplicate results, preserves key original text with citation markers

---

## 2026-05-01

### Added
- Tavily Search API support
- LLM summary support (recommend using fast models)
- SQLite cache management

---

## 2026-04-15

### Initial Version
- Baidu Qianfan AI Search API support
- Basic MCP service framework
