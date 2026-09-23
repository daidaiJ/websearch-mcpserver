# Configuration Reference

[English](configuration.en.md) | [中文](configuration.md)

## Contents

- [Config File Path](#config-file-path)
- [stdio CLI Configuration Notes](#stdio-cli-configuration-notes)
- [Full Configuration](#full-configuration)
- [Environment Variable Overrides](#environment-variable-overrides)
- [Default Values Quick Reference](#default-values-quick-reference)

---

## Config File Path

Priority (high to low):
1. Environment variable `WEBSEARCH_CONFIG`
2. CLI flag `-c / --config`
3. Current directory `config.yaml`

> For local integration tests, put API keys in gitignored `config.test.yaml` at the repo root and load it via `WEBSEARCH_CONFIG`. **Do not commit that file.**

> HTTP daemon: with `-c`, the PID file and log file are written under the config file's directory.
> stdio CLI: no PID file; logs go to `websearch.log` in the config directory (console logs on **stderr** so they do not corrupt JSON-RPC on stdout).

The HTTP daemon (`websearch-mcpserver start`) and the stdio CLI (`websearch-mcp-cli`) share the **same YAML schema**; search/tool fields mean the same thing. Differences are below.

---

## stdio CLI Configuration Notes

The stdio binary uses the same config schema as the HTTP service (`config.example.yaml` / what `websearch-mcp-cli init` writes). You do **not** need a separate CLI-only config file.

| Item | HTTP daemon | stdio CLI (`websearch-mcp-cli`) |
|------|-------------|-------------------------------|
| Config required? | `start` **must** load a config file | **Optional**: if no file is found, in-memory defaults apply (`mode: engine`, Bing/academic on) |
| `-c` / `WEBSEARCH_CONFIG` points to a missing file | Error and exit | Error and exit (does not silently fall back to defaults) |
| `port` | Listen port (default 8338); admin / SearXNG depend on it | **Ignored** (no HTTP listener) |
| `host` | Listen address (default `127.0.0.1`, loopback only) | **Ignored** (no HTTP listener) |
| `auth_token` | Bearer token for business endpoints (empty = no auth) | **Ignored** (stdio has no HTTP surface) |
| Console logs | stdout | **stderr** (file log remains `websearch.log`) |
| Process management | `start`/`stop`/`kill`, refcount, PID, Windows `install` | None; the MCP client starts/stops the process |
| SearXNG `/searxng/search` | Yes | No |

**Recommended minimal config (stdio, zero keys):**

```yaml
mode: engine
# port may be omitted; it has no effect for stdio
```

Key env vars work the same as HTTP: `BAIDU_SK`, `TAVILY_SK`, `EXA_API_KEY`, `ANYSEARCH_API_KEY`, `DOUBAO_SEARCH_API_KEY` (also `ASK_ECHO_SEARCH_INFINITY_API_KEY`), `LLM_BASE_URL`, `LLM_API_KEY`, `MINERU_TOKEN`, etc. When running with in-memory defaults (no config file), those key-related env vars are still applied; full field defaults still follow the "load file + Viper" path when a file is present.

Write an example file:

```bash
./websearch-mcp-cli init
./websearch-mcp-cli -c ~/.config/websearch/config.yaml init
```

---

## Full Configuration

```yaml
port: 8338                  # MCP HTTP port (ignored by stdio CLI)
host: "127.0.0.1"           # Listen address (default 127.0.0.1, loopback only; 0.0.0.0 opens all interfaces, pair with auth_token)
auth_token: ""              # Bearer token for business endpoints (empty = no auth; env WEBSEARCH_TOKEN)
mcp_stateless: false        # Stateless MCP HTTP mode (default false = stateful): each POST is handled
                            # independently, no initialize handshake or Mcp-Session-Id session — easier
                            # horizontal scaling behind proxies/LBs; GET SSE returns 405. All tools are
                            # request-response, so stateless mode loses nothing
log_level: info             # debug / info / warn / error
mode: engine                # baidu / apipool / tavily / exa / anysearch / doubao / hybrid / engine
network: china              # china (skip overseas engines) / international

# Global rate limit (applies to all search engines)
rate_limit:
  per_sec: 3                # Requests per second (default 3)
  per_min: 60               # Requests per minute (default 60)

# Blocked sites (applies to all search engines)
black_list_host:
  - "csdn.net"
  - "baidu.com"

# Baidu Qianfan (required for mode=baidu/apipool/hybrid)
baidu:
  web_enabled: false        # Baidu web search engine (tn=json scraping) disabled by default: CAPTCHA-blocked
                            # in testing; deployments with clean egress IPs may enable it explicitly
  api_key: ""               # Env: BAIDU_SK (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)
  enable_ai_search: true    # true=AI search chat/completions (default), false=web search web_search
  model: ""                 # AI search model, empty=free Baidu search (no LLM cost), set name=LLM intelligent search
  search_source: "baidu_search_v2" # Search engine version
  enable_reasoning: false   # Deep reasoning
  enable_deep_search: false # Deep search
  search_mode: "auto"       # auto / required / disabled

# Tavily (required for mode=tavily/apipool/hybrid)
# Get API key: https://app.tavily.com/home
tavily:
  api_key: ""               # Env: TAVILY_SK (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)

# Exa (required for mode=exa/apipool/hybrid)
# Get API key: https://dashboard.exa.ai/api-keys
exa:
  api_key: ""               # Env: EXA_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)
  num_results: 5            # Results per search (default 5)
  lookback_days: 90         # Search time range (days), default 90

# AnySearch (required for mode=anysearch/apipool/hybrid)
# Get API key: https://www.anysearch.com/console/api-keys
anysearch:
  api_key: ""               # Env: ANYSEARCH_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key; duplicate keys are deduplicated)
  num_results: 10           # Results per search (default 10)

# Doubao Search Global / Custom (mode=doubao/hybrid; add to apipool.engines explicitly)
# Activate: https://console.volcengine.com/search-infinity/web-search
# Get API key: https://console.volcengine.com/search-infinity/api-key
# Free tier: 500 credits/month; apipool.weights.doubao defaults to 500
doubao:
  api_key: ""               # Env: DOUBAO_SEARCH_API_KEY (also ASK_ECHO_SEARCH_INFINITY_API_KEY)
  sk_list: []               # Multi-key rotation list (priority over api_key; duplicates are deduplicated)
  version: global           # global (default) / custom
  num_results: 10           # Global max 20; Custom max 50
  max_snippet_length: 500   # Global: max tokens per snippet, maximum 3000
  max_image_count_per_doc: 0 # Global: images per document, default 0
  icp_host_only: false      # Global: restrict search to ICP-filed China sites
  time_range: ""            # Custom default; MCP request-level time_range wins (mapped to OneDay/OneWeek/OneMonth/OneYear; Global ignores it)
  auth_level: 0             # Custom: 0=default, 1=highly authoritative sources only
  query_rewrite: false      # Custom: enable query rewriting
  need_content: false       # Custom: request full page content

# Bing engine (fallback + engine mode primary, no key needed)
bing:
  enabled: true             # Master switch
  blocked: []               # Bing-specific blocked domains (merged with black_list_host)

# DuckDuckGo engine (needs proxy, no key needed)
duckduckgo:
  enabled: true             # Master switch (auto-joins search when proxy is available)
  blocked: []               # DuckDuckGo-specific blocked domains (merged with black_list_host)

# Google engine (disabled by default, anti-bot blocked)
google:
  enabled: false            # Explicit true may work but can return security challenge pages
  blocked: []               # Google-specific blocked domains (merged with black_list_host)

# Academic engines (no key needed)
academic:
  enabled: true             # Master switch, registers academicsearch tool
  bing_fallback: true       # Use Bing as fallback for academic search
  enhance: true             # Academic scoring enhancement (RRF fusion + citation/journal/PDF/recency), default true
  threshold: 0.02           # Academic result threshold (more lenient than general search), default 0.02
  disable_arxiv: false
  disable_crossref: false
  disable_openalex: false
  disable_pubmed: false
  disable_semantic_scholar: true    # Disabled by default (auto-proxied when enabled)
  disable_google_scholar: true      # Disabled by default (auto-proxied when enabled)
  # Optional Semantic Scholar API key (degrades to anonymous after consecutive 429s)
  # semantic_scholar_api_key: ""   # env: SEMANTIC_SCHOLAR_API_KEY
  # Unpaywall email: completes OA full-text links when a result has a DOI but no PDF; empty = skip silently
  # unpaywall_email: "you@example.com"   # env: UNPAYWALL_EMAIL
  disable_europepmc: false  # Europe PMC biomedical supplement (direct from China)
  disable_dblp: false       # DBLP CS conference/journal index (direct from China)
  disable_doaj: false       # DOAJ open-access journals (direct from China)

# Proxy (auto-detects system proxy by default, no manual config needed)
proxy:
  enabled: false          # Empty → auto-detect; true → use endpoint; false → disable
  endpoint: "http://127.0.0.1:7897"  # Only effective when enabled: true
  # Whether upstream API-provider requests (Baidu Qianfan / Tavily / Exa / AnySearch /
  # Doubao / LLM) go through the proxy. Default false = forced direct connection:
  # HTTP_PROXY/HTTPS_PROXY env vars can never silently hijack provider requests.
  # When true, the same enabled/endpoint/auto-detect resolution as engines applies.
  api_providers: false
  # Note: niche providers like AnySearch can hit broken DNS (the vendor's GTM once
  # returned a single overseas IP). Direct connections use system DNS; if it resolves
  # to an unreachable address, pin the correct IP in hosts or enable api_providers
  # to bypass local DNS pollution.

# LLM summary (optional)
llm:
  base_url: "https://api.openai.com/v1"   # Env: LLM_BASE_URL
  api_key: ""                               # Env: LLM_API_KEY
  model_id: "gpt-4o-mini"

# Cache (disabled by default)
cache:
  # enabled: true            # Unset → disabled by default (since v3.5.0); set true to enable
  # storage_path: ""         # Unset → exe sibling dir cache/websearch-cache.db
  cleanup_interval: 30      # Cleanup interval (minutes), max 360

# Local control center (off by default; passive real-call telemetry only, no active probes,
# raw queries/URLs are never stored). Prefer a separate dashboard.yaml next to the main config
# (see dashboard.example.yaml): it overrides the dashboard: block field-by-field, and deleting
# it plus a restart is a clean rollback. Keep the admin password / allowed networks / quotas /
# branding only in dashboard.yaml — the WebUI cannot read or modify them.
# For the full reference (shortcut placement, brand.footer, every key's default) see docs/dashboard.en.md.
# dashboard:
#   enabled: false
#   storage_path: "./data/dashboard.db"
#   retention_days: 30       # Detail retention; daily aggregates are retained
#   secrets_path: "./data/dashboard-secrets.json" # Private overlay; keys are never returned
#   config_path: ""          # Overlay file path; empty = dashboard.yaml next to the main config
#   admin_password: ""       # Write-operation password (plaintext); or use the SHA-256 form
#   admin_password_sha256: "" # Write-operation password (lowercase hex SHA-256)
#   allowed_networks: []     # Read-only CIDR/IP allowlist; empty = loopback only
#   quotas:
#     reset: monthly         # monthly / weekly / daily / none (manual reset only)
#     reset_day: 1           # Day of month for the monthly reset (1-28)
#     limits: {}             # Per-provider call cap per period; default 1000
#   brand:
#     title: ""              # Console title; default "WebSearch 控制中心"
#     logo: ""               # http(s) URL or local image path; empty = built-in mark
#     theme: ""              # green (default) / blue / mono
#     accent: ""             # Custom accent color #RRGGBB, overrides the theme
#     icon: ""               # Custom shortcut icon (.ico path); empty = built-in per theme

# Jina Reader (optional, fallback for cleanfetch)
jina:
  api_key: ""               # Empty → Jina fallback disabled
  base_url: ""              # Default https://r.jina.ai

# Enhanced web fetch (disabled by default)
cleanfetch:
  enabled: false            # Must be explicitly true for the cleanfetch tool; smartsearch's fetch_top_n is not gated by this switch (webfetch lazily initializes from current config)
  file_output_dir: ""       # Default: exe sibling dir fetchdata/
  file_ttl_hours: 24        # Temp file retention (hours)
  max_inline_lines: 100     # Lines above this threshold stored to file
  max_inline_chars: 0       # Chars above this threshold stored to file, 0=unlimited
  timeout_sec: 30           # Per-request timeout (seconds), default 30
  max_fetch_size_mb: 10     # HEAD pre-check max file size (MB), reject above (default 10); also the per-result byte cap for fetch_top_n
  use_system_proxy: false   # Auto-use system proxy (env vars + Windows registry), default false
  max_retries: 3            # Max retries (429/502/503 only), default 3

# PDF parser (disabled by default, independent of cleanfetch)
# MinerU AI enhancement (optional): with Token uses Standard API (remote URL, ≤200MB), without Token uses Agent API (local file, ≤10MB)
# Get Token: https://mineru.net/apiManage | Env: MINERU_TOKEN
pdf_parser:
  # max_pages: 20            # Max pages parsed per call when pages is omitted (default 20)
  enabled: false            # Must be explicitly true to enable
  # mineru_token: ""        # JWT Token; enables Standard API when set
  # mineru_model: "pipeline" # pipeline (default) / vlm (recommended)
  # mineru_ocr: false        # OCR fallback for scanned PDFs (when local library finds no text)
  # mineru_formula: true     # Formula recognition (default true)
  # mineru_table: true       # Table recognition (default true)
  # mineru_lang: "ch"        # Document language (default ch)

# Search result filtering and output format (optional)
# smartsearch:
#   max_size: 10          # Global max results (truncated by score), 0 = unlimited
#   show_meta: true       # Show engine source and relevance score in output (default true)
#   enhance: true         # Local scoring enhancement (RRF fusion + lexical alignment + domain quality + boosts + threshold), default true
#   relevance_threshold: 0.05  # Relevance threshold after enhancement; below this is filtered (Top-1 protected), default 0.05
#   mmr:                       # MMR diversity re-ranking (breaks up same-topic similar results)
#     enabled: true            # Master switch (default true)
#     lambda: 0.7              # Relevance-diversity tradeoff [0,1], higher = more relevance (default 0.7)
#     target_count: 0          # Target count after MMR, 0 = no extra truncation
#   engines:              # Per-engine config (names: tavily_api, exa, baidu_api, baidu, bing, google, duckduckgo, anysearch, doubao)
#     tavily_api:
#       min_score: 0.5    # Tavily API minimum relevance score threshold (0 = no filter)
#       max_size: 6       # Tavily API per-engine max results (default 4)
#       weight: 1.0       # Engine weight, affects RRF fusion score (when enhance=true), 0 = default 1.0
#     exa:
#       min_score: 0      # Exa doesn't return score, this field is ignored
#       max_size: 4       # Exa per-engine max results
#       weight: 1.0
#     baidu_api:
#       min_score: 0      # Baidu Qianfan doesn't return score (enable_ai_search controls endpoint)
#       max_size: 5       # Baidu Qianfan per-engine max results
#       weight: 1.0
#     baidu:
#       min_score: 0      # Baidu web search doesn't return score
#       max_size: 5       # Baidu web search per-engine max results
#       weight: 1.0
#     bing:
#       min_score: 0      # Bing doesn't return score, this field is ignored
#       max_size: 4       # Bing per-engine max results
#       weight: 1.0
#     google:
#       min_score: 0      # Google doesn't return score, this field is ignored
#       max_size: 4       # Google per-engine max results
#       weight: 1.0
#     duckduckgo:
#       min_score: 0      # DuckDuckGo doesn't return score, this field is ignored
#       max_size: 4       # DuckDuckGo per-engine max results
#       weight: 1.0
#     anysearch:
#       min_score: 0      # AnySearch doesn't return score, this field is ignored
#       max_size: 4
#       weight: 1.0
#     doubao:
#       min_score: 0      # Custom returns score; Global does not
#       max_size: 4
#       weight: 1.0

# Apipool mode config (optional, effective when mode=apipool)
# apipool:
#   strategy: weighted    # round-robin (default): rotate starting provider across requests
#                         # priority: always start from first provider
#                         # weighted: weighted-random starting provider (see weights)
#   engines:              # Provider priority order (default [anysearch, baidu, tavily, exa], Baidu web search fallback always last)
#                         # Doubao is not in the default list; add it explicitly when you have a key
#     - anysearch
#     - baidu
#     - tavily
#     - exa
#     # - doubao
#   weights:              # weighted strategy weights (per-key, accumulated by available key count)
#     anysearch: 30000    # defaults: anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500 (monthly free-tier credits)
#     baidu: 1500
#     tavily: 1200
#     exa: 1200
#     doubao: 500

# Log rotation
log:
  max_size: 1               # Max file size (MB)
  max_age: 1                # Retention (days)
```

---

## Control Center Security & Branding (dashboard)

### Access and write model

| Scenario | Rule |
|----------|------|
| Reads (pages + read-only APIs) | Loopback only by default; networks explicitly listed in `dashboard.allowed_networks` get read-only access |
| Writes (settings / keys / restart / cache clear / quota reset & adjust) | **Loopback only + `X-Admin-Password` header**, both required |
| Admin password | Configured only in `dashboard.yaml` (`admin_password` plaintext or `admin_password_sha256` hex); **the WebUI can never read or modify it**; unset password = all write endpoints disabled (secure default) |

- Allowlist syntax: `allowed_networks: ["192.168.1.0/24", "10.0.0.3"]` (CIDR or bare IP); any invalid entry **falls the whole allowlist back to loopback-only** (fail-closed).
- Docker: when the port is published to host loopback, the connection source inside the container is the bridge gateway (e.g. `172.17.0.1`); add that network to `allowed_networks` to open the console from the host browser.
- The remote link is plain HTTP; only allow trusted LANs. Use an SSH tunnel or a TLS reverse proxy for anything else.
- Password comparison is constant-time; prefer `admin_password_sha256` (lowercase hex of `sha256sum`) so no plaintext stays on disk.

### Quota management

- Local usage = real successful calls from telemetry (per provider, per period); providers are never probed. Tavily official usage takes priority when the official endpoint is available.
- Providers without a configured `quotas.limits` entry default to **1000 calls/period**; `reset` supports monthly / weekly / daily / none (manual only).
- WebUI "Settings → Quota management" offers manual reset (recount from now) and usage adjustment (real records untouched); both require the admin password and loopback.

### Branding & themes

- `brand.theme`: `green` (default, office green) / `blue` (blue-white tech) / `mono` (greyscale, layered); `brand.accent` (`#RRGGBB`) overrides the theme accent.
- `brand.title` / `brand.logo` customize the title and logo (http(s) URL or local image path); the WebUI also offers a browser-local appearance picker.
- The desktop shortcut icon follows `brand.theme`: every startup does a lightweight check and rebuilds the shortcut when the theme changes (one .ico per theme, avoiding the Windows icon cache).

### Client usage grouping

- Identity sources: `clientInfo.name` from the initialize handshake (spec-mandated, authoritative) + User-Agent keyword normalization (fallback); unknown when neither is available.
- Display grouping only (last 7 days of tool-level calls); no active probing, never affects search. The frontend hides the panel entirely when no client data exists.

### Presets & default-enabled

- Fresh deployments (no control-center config anywhere) auto-generate `dashboard.yaml` on first `start` / `install`: enabled by default with a random local admin password (0600).
- Deployments that explicitly configured the control center are never silently modified; turning it off = `enabled: false` or deleting the file, with zero telemetry overhead at runtime.

### Migration & rollback

- Old configs without a `dashboard:` block stay fully compatible: the console stays off, upstream behavior unchanged.
- A `dashboard.db` created by earlier preview builds gains the `quota_state` table automatically; older binaries ignore it.
- Remove the desktop shortcut before rolling back to an older build (older binaries do not know the `open` subcommand).

---

## Environment Variable Overrides

| Env Var | Overrides | Notes |
|---------|-----------|-------|
| `WEBSEARCH_CONFIG` | Config file path | Highest priority |
| `WEBSEARCH_DASHBOARD_CONFIG` | Control-center overlay file path | See [dashboard.example.yaml](../dashboard.example.yaml) |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | Tavily API Key ([get key](https://app.tavily.com/home)) |
| `EXA_API_KEY` | `exa.api_key` | Exa Web Search API Key ([get key](https://dashboard.exa.ai/api-keys)) |
| `ANYSEARCH_API_KEY` | `anysearch.api_key` | AnySearch API Key ([get key](https://www.anysearch.com/console/api-keys)) |
| `DOUBAO_SEARCH_API_KEY` | `doubao.api_key` | Doubao Search API Key ([console](https://console.volcengine.com/search-infinity/api-key)) |
| `ASK_ECHO_SEARCH_INFINITY_API_KEY` | `doubao.api_key` | Official Volcengine MCP-compatible variable name |
| `LLM_BASE_URL` | `llm.base_url` | |
| `LLM_API_KEY` | `llm.api_key` | |
| `MINERU_TOKEN` | `pdf_parser.mineru_token` | MinerU Standard API Token |

> Viper's `AutomaticEnv()` also supports `APP_` prefix for overriding any config field.

---

## Default Values Quick Reference

| Field | Default | Notes |
|-------|---------|-------|
| `port` | 8338 | stop/kill/status also use this port when no config |
| `mode` | engine | Auto-degrades to engine when no keys; `apipool` = API Key pool rotation, supports round-robin / priority / weighted strategies |
| `mcp_stateless` | false | Stateless MCP HTTP mode: each POST handled independently, no session handshake, easier horizontal scaling; GET SSE returns 405 |
| `baidu.web_enabled` | false | Baidu web search engine disabled by default (CAPTCHA-blocked in testing); may be enabled explicitly with clean egress IPs |
| `network` | china | |
| `rate_limit.per_sec` | 3 | Global rate limit |
| `rate_limit.per_min` | 60 | Global rate limit |
| `apipool.strategy` | round-robin | `round-robin` rotates provider across requests / `priority` fixed order / `weighted` weighted-random |
| `apipool.engines` | [anysearch, baidu, tavily, exa] | Provider priority order, Baidu web search fallback always last; `doubao` is not in the default list — add it explicitly when you have a key |
| `apipool.weights` | anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500 | weighted per-key weights, accumulated by available key count; doubao 500 matches the free-tier monthly credits |
| `doubao.version` | global | Global / Custom; concurrent both goes through hybrid, not inside the adapter |
| `doubao.num_results` | 10 | Global max 20; Custom max 50 |
| `doubao.time_range` | "" | Custom default; MCP request-level `time_range` wins (mapped to OneDay/OneWeek/OneMonth/OneYear) |
| `doubao.need_content` | true | Custom requests full page content (API fast path); explicit false falls back to summaries |
| `tavily.include_raw_content` | true | Tavily requests raw page content (raw_content, fast path); explicit false falls back to excerpts |
| `exa.include_text` | true | Exa requests page body text (contents.text) |
| `exa.text_max_characters` | 3000 | Max characters of Exa body text |
| `baidu.enable_ai_search` | true | true=AI search chat/completions, false=web search web_search; no LLM cost when model is empty |
| `bing.enabled` | true | |
| `duckduckgo.enabled` | true | Needs proxy; auto-joins when proxy is available |
| `google.enabled` | false | Anti-bot blocked, must be explicitly enabled |
| `academic.enabled` | true | |
| `academic.bing_fallback` | true | |
| `academic.enhance` | true | Academic scoring enhancement |
| `academic.threshold` | 0.02 | Academic result threshold |
| `academic.disable_semantic_scholar` | true | Disabled by default, auto-proxied when enabled |
| `academic.disable_google_scholar` | true | Disabled by default, auto-proxied when enabled |
| `academic.semantic_scholar_api_key` | "" | Optional API key; auto-degrades to anonymous after consecutive 429s (env `SEMANTIC_SCHOLAR_API_KEY`) |
| `academic.unpaywall_email` | "" | Unpaywall email for OA link completion (when a result has a DOI but no PDF); empty = skip silently (env `UNPAYWALL_EMAIL`) |
| `academic.disable_europepmc` | false | Europe PMC biomedical supplement, reachable from China |
| `academic.disable_dblp` | false | DBLP CS conference/journal index, reachable from China |
| `academic.disable_doaj` | false | DOAJ open-access journals, reachable from China |
| `proxy.enabled` | unset | Auto-detects system proxy when not set; explicit false disables; explicit true uses endpoint |
| `proxy.endpoint` | `http://127.0.0.1:7897` | Only effective when `enabled: true` |
| `proxy.api_providers` | false | Whether upstream API-provider requests (Qianfan/Tavily/Exa/AnySearch/Doubao/LLM) use the proxy; false forces direct connections immune to proxy env vars |
| `cleanfetch.enabled` | false | Old configs don't enable; must be explicit. Only gates the cleanfetch tool — `fetch_top_n` is not gated (webfetch lazily initializes) |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `cleanfetch.timeout_sec` | 30 | |
| `cleanfetch.max_fetch_size_mb` | 10 | HEAD pre-check threshold; also the per-result byte cap for `fetch_top_n` |
| `cleanfetch.use_system_proxy` | false | Auto-use system proxy (env vars + Windows registry) |
| `cleanfetch.max_retries` | 3 | Only retries on 429/502/503 |
| `pdf_parser.enabled` | false | Independent of cleanfetch |
| `pdf_parser.max_pages` | 20 | Max pages parsed per call when pages is omitted; a single pages range is capped at 1000 pages wide |
| `pdf_parser.mineru_model` | pipeline | pipeline / vlm |
| `pdf_parser.mineru_formula` | true | Formula recognition |
| `pdf_parser.mineru_table` | true | Table recognition |
| `pdf_parser.mineru_lang` | ch | Document language |
| `smartsearch.show_meta` | true | Show engine source and relevance score in output |
| `smartsearch.fetch_top_n` | 0 | Server-side default body-fetch count (applies when the agent omits `fetch_top_n`); default 0 = no fetch (same as before), 1-5 = one search returns full text (API engines use the fast path, web engines fetch internally) |
| `smartsearch.enhance` | true | Local scoring enhancement |
| `smartsearch.relevance_threshold` | 0.05 | Relevance threshold after enhancement |
| `smartsearch.mmr.enabled` | true | MMR diversity re-ranking |
| `smartsearch.mmr.lambda` | 0.7 | Relevance-diversity tradeoff |
| `cache.enabled` | false | Unset → disabled by default (since v3.5.0); set true to enable (storage_path defaults to exe sibling dir cache/websearch-cache.db) |
| `cache.cleanup_interval` | 30 (min) | Max 360 |
| Cache expiry | 6 hours | Based on last hit time, hardcoded |
| `log.max_size` | 1 (MB) | |
| `log.max_age` | 1 (day) | |

---

## Minimal Config

```yaml
port: 8338
mode: engine
```

Runs with zero API keys using Baidu web search + Bing + academic search engines.
