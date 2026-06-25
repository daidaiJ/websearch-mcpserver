# Changelog

[English](CHANGELOG.en.md) | [中文](CHANGELOG.md)

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
- New [docs/config.md](docs/config.md): config reference, defaults quick reference, environment variable overrides
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
