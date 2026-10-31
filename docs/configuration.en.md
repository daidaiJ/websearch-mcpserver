# Configuration Reference

[English](configuration.en.md) | [中文](configuration.md)

## Contents

- [Config File Path](#config-file-path)
- [stdio CLI Configuration Notes](#stdio-cli-configuration-notes)
- [Full Configuration](#full-configuration)
- [Search Sources and Mode Routing](#search-sources-and-mode-routing)
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
mcp_resources: true         # MCP Resource observability (default true): search://capabilities and
                            # search://health — read-only, secret-free, no tool slot; false to disable
log_level: info             # debug / info / warn / error
language: auto              # auto / zh / en; auto follows the system language (Windows user
                            # default locale, POSIX LC_ALL/LC_MESSAGES/LANG on Unix) and falls back
                            # to en when neither Chinese nor English matches; env WEBCRAWLER_LANGUAGE
mode: engine                # baidu / apipool / tavily / exa / anysearch / doubao / yuanbao / hybrid / engine / mcphub
network: china              # Academic adapter region; not web-source selection or Doubao site scope

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
  enabled: true             # Qianfan API source; false keeps keys and does not affect web_enabled
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
  enabled: true             # API source switch; false keeps api_key/sk_list
  api_key: ""               # Env: TAVILY_SK (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)

# Exa (required for mode=exa/apipool/hybrid)
# Get API key: https://dashboard.exa.ai/api-keys
exa:
  enabled: true             # API source switch; false keeps api_key/sk_list
  api_key: ""               # Env: EXA_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)
  num_results: 5            # Results per search (default 5)
  lookback_days: 90         # Search time range (days), default 90

# AnySearch (required for mode=anysearch/apipool/hybrid; also usable keyless via the anonymous tier)
# Get API key: https://www.anysearch.com/console/api-keys
anysearch:
  enabled: true             # API source switch; false keeps api_key/sk_list and disables the anonymous tier
  api_key: ""               # Env: ANYSEARCH_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key; duplicate keys are deduplicated)
  num_results: 10           # Results per search (default 10)
  # With no keys configured the anonymous tier is used automatically (no auth header, IP-rate-limited upstream,
  # conservative local clamp 1/s·20/min); failures hand over to the next source in the orchestration layer
  # (verified reachable from a CN egress on 2026-10-06)

# Doubao Search Global / Custom (mode=doubao/hybrid; add to apipool.engines explicitly)
# Activate: https://console.volcengine.com/search-infinity/web-search
# Get API key: https://console.volcengine.com/search-infinity/api-key
# Free tier: 500 credits/month; apipool.weights.doubao defaults to 500
doubao:
  enabled: true             # API source switch; false keeps api_key/sk_list
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

# Tencent Yuanbao Search (API KEY mode; mode=yuanbao/hybrid; add to apipool.engines explicitly)
# Docs: https://cloud.tencent.com/document/product/1806/130615
# Console: https://console.cloud.tencent.com/wsapi/index
# 2,000 free calls per month; usage beyond that is billed pay-as-you-go (about CNY 20 per 1,000 calls,
# subject to official pricing). The local monthly cap stops calls at 2,000 by default to avoid surprise bills.
yuanbao:
  enabled: true             # API source switch; false keeps api_key/sk_list
  api_key: ""               # Env: TENCENTCLOUD_WSA_APIKEY (single key is used as sk_list when sk_list is empty)
  sk_list: []               # Multi-key rotation list (priority over api_key; duplicates are deduplicated)
  num_results: 0            # 0=omit Cnt (upstream default 10); other values clamp to 10/20/30/40/50
  mode: 0                   # 0=public web / 1=authoritative vertical sources / 2=mixed; 0 is omitted (lite-compatible)
  site: ""                  # One site (qq.com) or exclusion (exclude:qq.com|sohu.com, up to 5)
  industry: ""              # gov / news / acad / finance
  freshness: ""             # Default dN/mN/yN/date; MCP request-level time_range wins
  monthly_limit: 2000       # Local monthly cap; 0=unlimited (accepts pay-as-you-go risk); positive=custom cap
  state_path: ""            # Counter file; empty = data/yuanbao-quota.json beside the executable

# TinyFish Search/Fetch API (mode=apipool requires explicit engines entry; hybrid auto-joins when keyed)
# Free allowance: search 12,000/day (30 req/min), fetch 1,000 URLs/day (150 URLs/min,
# counted per successfully fetched URL), reset at 00:00 UTC. Keyed tier only, no anonymous tier.
# Its fetch capability joins the cleanfetch remote-fetch chain as a fallback behind hosted MCP (per-URL counting).
tinyfish:
  enabled: true             # API source switch; false keeps api_key/sk_list stored
  api_key: ""               # TinyFish API key (https://docs.tinyfish.ai)
  sk_list: []               # multi-key rotation list (takes precedence over api_key)

# Keenable Search/Fetch API (mode=apipool requires explicit engines entry)
# Free 10 req/s; the keyed endpoint bills credits, while without a key the keyless public endpoint
# is used automatically (per-IP rate limit, no credits); the public tier does not auto-join hybrid — set enabled: true explicitly.
# Fetch covers indexed URLs only (live: true forces a live crawl); also joins the cleanfetch remote-fetch chain.
keenable:
  enabled: false            # API source switch; the keyless public tier needs explicit true to join hybrid
  api_key: ""               # Keenable API key (keen_ prefix, https://docs.keenable.ai)
  sk_list: []               # multi-key rotation list (takes precedence over api_key)
  live: false               # force live crawl on fetch (true can fetch unindexed URLs)

# Bing engine (fallback + engine mode primary, no key needed)
bing:
  enabled: true             # Master switch
  blocked: []               # Bing-specific blocked domains (merged with black_list_host)

# 360 engine (so.com, second Chinese index, no key, no proxy needed; verified reachable from a CN egress on 2026-10-06)
so360:
  enabled: false            # Master switch (default false)
  blocked: []               # 360-specific blocked domains (merged with black_list_host)
  # per_sec: 3              # Rate limit per second (default 3)
  # per_min: 60             # Rate limit per minute (default 60)
  # safe_search: 0          # Safe search: 0=off, 1/2=on (sends secure=1)

# Wikipedia engine (MediaWiki API, keyless, reference-type source; needs proxy)
wikipedia:
  enabled: false            # Master switch (default false)
  # lang: zh                # Language edition (default zh)
  # num_results: 10         # Results per search (default 10, max 50)
  # per_sec: 1              # Rate limit per second (default 1, conservative per MediaWiki etiquette)
  # per_min: 30             # Rate limit per minute (default 30)

# Google News RSS engine (keyless, independent news index with structured dates; needs proxy)
google_news:
  enabled: false            # Master switch (default false)
  # edition: zh-CN          # News edition (default zh-CN; zh-CN/zh-TW/en-US/en-GB/ja-JP/ko-KR)
  # num_results: 10         # Results per search (default 10)
  # per_sec: 1              # Rate limit per second (default 1)
  # per_min: 30             # Rate limit per minute (default 30)

# GitHub engine (REST API, zero-key anonymous tier, direct connection from CN — no proxy needed)
# Anonymous search 10/min; a PAT raises it to 30/min (core 60/h → 5000/h);
# cleanfetch's github.com repo/blob special-casing shares the same tokens
github:
  enabled: false            # Master switch (default false)
  # api_key: ""             # PAT (github_pat_/ghp_ prefix); env GITHUB_TOKEN
  # token_list:             # Multi-PAT rotation list (takes priority over api_key)
  #   - "github_pat_xxxx"
  # num_results: 10         # Results per search (default 10, cap 50)
  # per_min: 30             # Search rate limit per minute (default 10/30 depending on PAT)

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
  bing_fallback: true       # Compatibility key; academicsearch currently does not call Bing
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
  # openalex_api_key: ""          # env: OPENALEX_API_KEY
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
  summary_progress_interval: 1              # Min interval (seconds) between summary progress notifications, default 1
  timeout_sec: 60                           # Overall timeout (seconds) per LLM call including the whole stream, default 60;
                                            # on timeout the summary fails and search falls back to raw results

# Cache (disabled by default)
cache:
  # enabled: true            # Unset → disabled by default (since v3.5.0); set true to enable
  # storage_path: ""         # Unset → exe sibling dir cache/websearch-cache.db
  cleanup_interval: 30      # Cleanup interval (minutes), max 360

# Search history (disabled by default; FTS5 full-text over "what you already searched")
# When enabled, every successful smartsearch writes the query + result snapshot
# (title/URL/truncated snippet, up to 20 per search) and registers the search_history MCP tool;
# data stays in local SQLite, never leaves the machine, and never contains keys.
# search_history:
#   enabled: true             # explicit true to enable; when off no DB is created and the tool is not registered
#   storage_path: ""          # Unset → exe sibling dir history/search-history.db
#   retention_days: 30        # retention days, default 30; expired rows are cleaned hourly

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
  enabled: false            # Only enables URL fetching; cleanfetch is always registered, saved-result reads and fetch_top_n are not gated
  file_output_dir: ""       # Default: exe sibling dir fetchdata/
  file_ttl_hours: 24        # Temp file retention (hours)
  max_inline_lines: 100     # Lines above this threshold stored to file
  max_inline_chars: 0       # Chars above this threshold stored to file, 0=unlimited
  timeout_sec: 30           # Per-request timeout (seconds), default 30
  max_fetch_size_mb: 10     # HEAD pre-check max file size (MB), reject above (default 10); also the per-result byte cap for fetch_top_n
  use_system_proxy: false   # Auto-use system proxy (env vars + Windows registry), default false
  auto_proxy: true          # Native cleanfetch/PDF fetches follow the global proxy (including system-proxy detection), default true; false = force direct
  max_retries: 3            # Max retries (429/502/503 only), default 3

# PDF parser (disabled by default, independent of cleanfetch)
# MinerU AI enhancement (optional): with Token uses Standard API (remote URL, ≤200MB/600 pages), without Token uses Agent API (local file, ≤10MB/20 pages)
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
  # mineru_page_limit: 600   # Standard API per-task page limit (default 600, per MinerU official docs)
  # mineru_page_batch_size: 0 # Auto batch size for local crop fallback: 0=no batching (over-limit rejected); 10-200=pages per batch, submitted serially
  # mineru_page_budget: 0    # MinerU page budget per call (0=same as mineru_page_limit); on exhaustion returns parsed part with continuation hint

# Search result filtering and output format (optional)
# smartsearch:
#   max_size: 10          # Global max results (truncated by score), 0 = unlimited
#   show_meta: true       # Show engine source and relevance score in output (default true)
#   enhance: true         # Local scoring enhancement (RRF fusion + lexical alignment + domain quality + boosts + threshold), default true
#   relevance_threshold: 0.05  # Relevance threshold after enhancement; below this is filtered (Top-1 protected), default 0.05
#   inline_max_chars: 32768    # Inline char limit for the rendered response; over the limit the whole result is dumped to a
#                              # temp file (search-*.md under cleanfetch.file_output_dir, kept 7 days). The response keeps
#                              # the provenance header / stats / file path / read hint / failure list. 0 = default 32768,
#                              # negative = disable dumping (always inline)
#   off_topic_guard: shadow    # Off-topic bucket guard: shadow (default, suspected decoy buckets are only logged to the
#                              # failure list, results kept) / enforce (whole bucket dropped and reported) / off
#   engine_timeout_sec: 10     # Per-engine soft timeout (seconds): a timed-out engine is reported as timeout in the
#                              # failure list while other engines still return results. 0 = default 10s, negative = rely
#                              # on the request ctx deadline only
#   mmr:                       # MMR diversity re-ranking (breaks up same-topic similar results)
#     enabled: true            # Master switch (default true)
#     lambda: 0.7              # Relevance-diversity tradeoff [0,1], higher = more relevance (default 0.7)
#     target_count: 0          # Target count after MMR, 0 = no extra truncation
#   engines:              # Per-engine config (names: tavily_api, exa, baidu_api, baidu, so360, bing, google, duckduckgo, anysearch, doubao, yuanbao)
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
#     yuanbao:
#       min_score: 0      # Upstream returns a 0-1 relevance score
#       max_size: 4
#       weight: 1.0

# Keyless MCP Hub cascade (optional; search engine when mode=mcphub)
# When fetch_mode is not off, the cleanfetch remote-fetch chain tries the MCP servers first
# (they render JS), then provider fetch APIs (tinyfish/keenable, plain HTTP fetching)
# (requires cleanfetch.enabled=true; in mode=mcphub it reuses the search hub)
# Note: the hosted MCP exa/anysearch and the same-named apipool providers point at different
# entrances of the same upstream (the former usually the anonymous per-egress-IP tier, the
# latter your configured key); allowances are separate but share the egress IP.
# mcp_hub:
#   enabled: true
#   strategy: priority      # priority (default, stability first) / round-robin
#   timeout_sec: 20
#   fetch_timeout_sec: 45
#   cooldown_sec: 60        # skip a failed server for this long
#   max_results: 10
#   max_fetch_chars: 200000 # truncate a single page beyond this many characters
#   fetch_mode: off         # off (default) / fallback / prefer
#   rotation_engines: []    # local engines in the main rotation (after MCP), e.g. [so360]
#   fallback_engines: [bing, so360]  # after rotation (unavailable engines are skipped); [] disables local fallback (Bing still applies)
#   servers:                # omit for parallel → anysearch → exa → firecrawl
#                           # empty api_key/sk_list uses the anonymous tier; auth failures try the next key, connection/timeout cools down and cascades
#     - name: parallel      # optional key: https://platform.parallel.ai/
#     - name: anysearch     # optional key: https://www.anysearch.com/console/api-keys
#     - name: exa           # proxy by default; optional key: https://dashboard.exa.ai/api-keys
#     - name: firecrawl     # proxy by default; optional key: https://www.firecrawl.dev/app/api-keys

# Apipool mode config (optional, effective when mode=apipool)
# apipool:
#   strategy: weighted    # round-robin (default): rotate starting provider across requests
#                         # priority: always start from first provider
#                         # weighted: weighted-random starting provider (see weights)
#   engines:              # Provider priority order (default [anysearch, baidu, tavily, exa], web-engine fallback always last)
#                         # Doubao/Yuanbao are not in the default list; add them explicitly when you have a key
#     - anysearch
#     - baidu
#     - tavily
#     - exa
#     # - doubao
#     # - yuanbao
#   weights:              # weighted strategy weights (per-key, accumulated by available key count)
#     anysearch: 30000    # defaults: anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500, yuanbao=0
#     baidu: 1500
#     tavily: 1200
#     exa: 1200
#     doubao: 500
#     yuanbao: 0          # Pay-as-you-go source stays out of weighted starts unless given a positive weight

# Log rotation

# Everything local file search (file_search tool, optional; Windows + Everything HTTP Server only)
# Note: not recommended on any Linux distribution unless running under WSL with everything.url explicitly pointing at the Windows host; on non-Windows platforms without an explicit url there is no probe and the tool stays hidden.
# No enabled switch: the server lazily probes everything.url when the first MCP client connects
# (avoiding the autostart race between websearch and Everything) and registers file_search
# only when the HTTP Server is reachable and authentication passes; otherwise the tool
# stays hidden without affecting anything else.
# everything:
#   url: "http://127.0.0.1:4180"   # HTTP Server address (Everything Tools -> Options -> HTTP Server)
#   username: ""                   # Basic username/password when the server has auth enabled
#   password: ""
#   roots: []                      # Directory whitelist (absolute paths): when set, searches are forcibly scoped to it
#   max_results: 10                # Results per page (default 10, hard cap 20; use page for more)
#   timeout_sec: 5                 # Per-request timeout in seconds
#   noise_dirs: ~                  # Noise-directory demotion: nil = built-in defaults (node_modules/.git/target...); [] = disable
#   min_alignment: 0               # Lexical alignment threshold (0-1): weak results below it are dropped; 0 = re-rank only

log:
  max_size: 1               # Max file size (MB)
  max_age: 1                # Retention (days)
```

---

## Search Sources and Mode Routing

`baidu.enabled`, `tavily.enabled`, `exa.enabled`, `anysearch.enabled`, `doubao.enabled`, `yuanbao.enabled`, `tinyfish.enabled`, and `keenable.enabled` independently control the API sources. Omitted switches mean `true`, preserving existing configurations. Disabling a source removes it from routing without deleting `api_key`, `sk_list`, or secret-overlay keys. AnySearch uses the same switch for keyed and anonymous access; Keenable falls back to its keyless public tier but does not join hybrid by default — set `enabled: true` or list it in `apipool.engines`; TinyFish has no anonymous tier and stays out when keyless. `baidu.web_enabled` independently controls the Baidu web source. Yuanbao's `yuanbao.monthly_limit` is enforced locally before each request; once the cap is reached the source is treated as unavailable and other sources take over.

Source switches permit or deny a source; `mode` selects among permitted sources:

| Mode | Routing |
|------|---------|
| `engine` | Enabled web sources only; enabling an API does not add it to this mode |
| `baidu` | Enabled, keyed Qianfan API, optionally falling back to independently enabled Baidu web; enabled Bing if no primary source is usable |
| `tavily` / `exa` / `doubao` / `yuanbao` | The enabled, keyed API; enabled Bing when that source is unavailable |
| `anysearch` | Enabled AnySearch with key rotation or the anonymous tier; disabled AnySearch can only fall back to enabled Bing |
| `apipool` | Filter disabled/unusable APIs from `apipool.engines`, then rotate and fail over; enabled web engines serve as the final fallback; enabled Bing can be used if the pool is empty |
| `hybrid` | Combine enabled, usable APIs and web sources concurrently; disabled sources, including anonymous AnySearch, are excluded |
| `mcphub` | Cascade hosted MCP servers in `mcp_hub.servers` order; a failed server cools down and the hub switches to the next, degrading through `fallback_engines` (unavailable engines are skipped) and finally enabled Bing when all fail; per-server `api_key`/`sk_list` are optional, with auth failures rotating to the next key |

> **Recommended**: use `mcphub` for zero-key deployments (hosted-MCP cascade with local-engine/Bing degradation) and `apipool` when API keys are available (multi-provider key rotation). `mcphub.rotation_engines` can share load with local engines, and `exa`/`firecrawl` are enabled/skipped automatically based on proxy availability.

`bing.enabled` controls only the Bing source and its fallback, not the `smartsearch` tool itself. With Bing disabled, another usable route in the configured mode keeps `smartsearch` available. The tool is not registered only when that mode has no usable primary or fallback.

The console offers `hybrid`, `engine`, `apipool` and a single-provider entry; the latter selects the six existing providers while preserving the ten persisted `mode` values. Mode and selected-provider descriptions support hover, focus or click. The pool's first provider also depends on `apipool.strategy` and weights, so list-order dispatch is not guaranteed. Provider preference can still use an enabled Bing fallback; it is not an exclusive-provider promise.

`network` currently feeds only the independent academic adapter: China region excludes Semantic Scholar / Google Scholar, while international region still requires each source switch and proxy prerequisites. It is not a global domestic/international web toggle or an automatic proxy switch. The console places it under advanced academic connection settings. Doubao scope is separate: Global `doubao.icp_host_only: true` restricts results to China ICP-filed sites; `false` means unrestricted, not foreign-only. Custom does not consume this field. Version and scope live under provider startup parameters, take effect after save/restart, and invalidate previous search caches. [Official Global API](https://docs.volcengine.com/docs/Networkedsearch/doubao-search-global-edition?lang=zh)

Academic search filters registered sources using request-level `engines` before exact lookup. A DOI lookup uses only Crossref/OpenAlex in that selected set; an arXiv ID lookup uses only selected arXiv. When no selected source supports that exact lookup, ordinary search uses only the selected usable sources. An entirely unavailable selection returns an error; exact lookup never bypasses disabled sources.

`academic.bing_fallback` is retained for configuration compatibility but is not consumed by the current academic path, which never calls Bing. Web sources do not keep `academicsearch` registered when all academic sources are unavailable.

---

## Control Center Security & Branding (dashboard)

### Access and write model

| Scenario | Rule |
|----------|------|
| Reads (pages + read-only APIs) | Loopback only by default; networks explicitly listed in `dashboard.allowed_networks` get read-only access |
| Writes (settings / keys / restart / cache clear / quota reset & adjust) | **Loopback only by default**; explicit `dashboard.write_allowed_networks` permits specified sources; every write still requires `X-Admin-Password`, plus matching `X-Admin-User` when a username is configured |
| Admin password | Configured only in `dashboard.yaml` (`admin_password` plaintext or `admin_password_sha256` hex); **the WebUI can never read or modify it**; unset password = all write endpoints disabled (secure default) |

- Both allowlists accept CIDRs or bare IPs. Any invalid entry **falls that entire allowlist back to loopback-only** (fail-closed). `allowed_networks` does not grant writes; `write_allowed_networks` does not replace the page/API read-access gate or the admin password.
- Docker: keep the host publication fixed to loopback, for example `-p 127.0.0.1:8338:8338`. Host-browser connections may appear as a bridge gateway inside the container. Use the request source IP actually observed by the server; the gateway from `docker inspect` is only a cross-check, not proof of the request source. For host writes, put only that observed address as a `/32` in both allowlists, never the whole bridge subnet.

```yaml
# dashboard.yaml; illustrative address only, replace with the observed host-request source
dashboard:
  allowed_networks: ["172.17.0.1/32"]
  write_allowed_networks: ["172.17.0.1/32"]
  admin_password: "replace-with-a-private-admin-password"
```

- Keep `write_allowed_networks: []` until the source is verified; do not copy the illustrative IP unchanged. An explicit `[]` in the independent overlay clears a write allowlist inherited from the main config.
- The remote link is plain HTTP; only allow trusted LANs. Use an SSH tunnel or a TLS reverse proxy for anything else.
- Password comparison is constant-time; prefer `admin_password_sha256` (lowercase hex of `sha256sum`) so no plaintext stays on disk.

### Quota management

- Local usage = real successful calls from telemetry (per provider, per period); providers without official endpoints are never probed. Tavily / TinyFish official usage takes priority when the official endpoint is available (TinyFish reads today's call counts from the official `/usage` endpoints; free tier 12,000 searches/day and 1,000 fetch URLs/day).
- Providers without a configured `quotas.limits` entry default to **1000 calls/period**; `reset` supports monthly / weekly / daily / none (manual only).
- WebUI "Settings → Quota management" offers manual reset (recount from now) and usage adjustment (real records untouched); both use the same source-allowlist and admin-password rules.

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
| `TENCENTCLOUD_WSA_APIKEY` | `yuanbao.api_key` | Tencent Yuanbao Search service API KEY ([console](https://console.cloud.tencent.com/wsapi/index)) |
| `LLM_BASE_URL` | `llm.base_url` | |
| `LLM_API_KEY` | `llm.api_key` | |
| `LLM_SUMMARY_PROGRESS_INTERVAL` | `llm.summary_progress_interval` | |
| `MINERU_TOKEN` | `pdf_parser.mineru_token` | MinerU Standard API Token |

> Viper's `AutomaticEnv()` also supports `APP_` prefix for overriding any config field.

---

## Default Values Quick Reference

| Field | Default | Notes |
|-------|---------|-------|
| `port` | 8338 | stop/kill/status also use this port when no config |
| `mode` | engine | Selects permitted sources; only enabled fallbacks can be used, while keyless AnySearch can use its anonymous tier |
| `baidu.enabled` / `tavily.enabled` / `exa.enabled` / `anysearch.enabled` / `doubao.enabled` / `yuanbao.enabled` / `tinyfish.enabled` / `keenable.enabled` | true (omitted) | Independent API source switches; false keeps keys and applies to every mode, including anonymous AnySearch |
| `dashboard.write_allowed_networks` | [] | Independent write allowlist; loopback-only by default; invalid entries fail closed; admin password still required |
| `mcp_resources` | true | MCP Resource observability toggle: `search://capabilities` and `search://health`, read-only, secret-free, no tool slot |
| `mcp_stateless` | false | Stateless MCP HTTP mode: each POST handled independently, no session handshake, easier horizontal scaling; GET SSE returns 405 |
| `baidu.web_enabled` | false | Baidu web search engine disabled by default (CAPTCHA-blocked in testing); may be enabled explicitly with clean egress IPs |
| `network` | china | Academic adapter region, not web-source selection, proxy control or Doubao site filtering |
| `doubao.icp_host_only` | false | Global only: true restricts to China ICP-filed sites; false is unrestricted; unused by Custom |
| `rate_limit.per_sec` | 3 | Global rate limit |
| `rate_limit.per_min` | 60 | Global rate limit |
| `apipool.strategy` | round-robin | `round-robin` rotates provider across requests / `priority` fixed order / `weighted` weighted-random |
| `apipool.engines` | [anysearch, baidu, tavily, exa] | Provider priority order, web-engine fallback always last; `doubao`/`yuanbao`/`tinyfish`/`keenable` are not in the default list — add them explicitly when you have a key (keenable may also run keyless) |
| `apipool.weights` | anysearch=30000, baidu=1500, tavily=1200, exa=1200, doubao=500, yuanbao=0, tinyfish=1200, keenable=800 | weighted per-key weights, accumulated by available key count; doubao 500 matches the free-tier monthly credits; yuanbao defaults to 0 (pay-as-you-go source stays out of weighted starts unless configured); tinyfish 1200 matches its 12,000 searches/day free tier and keenable 800 its keyless public tier |
| `doubao.version` | global | Global / Custom; concurrent both goes through hybrid, not inside the adapter |
| `doubao.num_results` | 10 | Global max 20; Custom max 50 |
| `doubao.time_range` | "" | Custom default; MCP request-level `time_range` wins (mapped to OneDay/OneWeek/OneMonth/OneYear) |
| `doubao.need_content` | true | Custom requests full page content (API fast path); explicit false falls back to summaries |
| `yuanbao.monthly_limit` | 2000 | Local monthly call cap; once reached, requests stop and other sources take over; 0=unlimited (incurs pay-as-you-go charges) |
| `yuanbao.num_results` | 0 | 0=omit Cnt (upstream default 10); other values clamp to 10/20/30/40/50 |
| `yuanbao.mode` | 0 | 0=public web / 1=authoritative vertical sources / 2=mixed; 0 is omitted (lite-compatible) |
| `yuanbao.freshness` | "" | Default dN/mN/yN/date; MCP request-level `time_range` wins |
| `yuanbao.state_path` | `data/yuanbao-quota.json` beside the executable | Counter file; the cap survives restarts |
| `tinyfish.enabled` | true (omitted) | TinyFish source switch; keyless TinyFish never participates (keyed tier only) |
| `keenable.enabled` | true (omitted) | Keenable source switch; keyless use of the public tier still needs an explicit `true` to join hybrid |
| `keenable.live` | false | Force a live crawl on fetch (true can fetch unindexed URLs instead of the index copy) |
| `tavily.include_raw_content` | true | Tavily requests raw page content (raw_content, fast path); explicit false falls back to excerpts |
| `exa.include_text` | true | Exa requests page body text (contents.text) |
| `exa.text_max_characters` | 3000 | Max characters of Exa body text |
| `baidu.enable_ai_search` | true | true=AI search chat/completions, false=web search web_search; no LLM cost when model is empty |
| `bing.enabled` | true | Controls only the Bing source and fallback, not the smartsearch tool |
| `duckduckgo.enabled` | true | Needs proxy; auto-joins when proxy is available |
| `google.enabled` | false | Anti-bot blocked, must be explicitly enabled |
| `academic.enabled` | true | |
| `academic.bing_fallback` | true | Compatibility key; academicsearch currently does not use Bing |
| `academic.enhance` | true | Academic scoring enhancement |
| `academic.threshold` | 0.02 | Academic result threshold |
| `academic.disable_semantic_scholar` | true | Disabled by default, auto-proxied when enabled |
| `academic.disable_google_scholar` | true | Disabled by default, auto-proxied when enabled |
| `academic.semantic_scholar_api_key` | "" | Optional API key; auto-degrades to anonymous after consecutive 429s (env `SEMANTIC_SCHOLAR_API_KEY`) |
| `academic.openalex_api_key` | "" | Optional OpenAlex key shared by keyword search and exact DOI lookup; empty uses the outgoing IP's shared anonymous daily budget (env `OPENALEX_API_KEY`) |
| `academic.unpaywall_email` | "" | Unpaywall email for OA link completion (when a result has a DOI but no PDF); empty = skip silently (env `UNPAYWALL_EMAIL`) |
| `academic.disable_europepmc` | false | Europe PMC biomedical supplement, reachable from China |
| `academic.disable_dblp` | false | DBLP CS conference/journal index, reachable from China |
| `academic.disable_doaj` | false | DOAJ open-access journals, reachable from China |
| `proxy.enabled` | unset | Auto-detects system proxy when not set; explicit false disables; explicit true uses endpoint |
| `proxy.endpoint` | `http://127.0.0.1:7897` | Only effective when `enabled: true` |
| `proxy.api_providers` | false | Whether upstream API-provider requests (Qianfan/Tavily/Exa/AnySearch/Doubao/Yuanbao/LLM) use the proxy; false forces direct connections immune to proxy env vars |
| `cleanfetch.enabled` | false | Only gates URL fetching; cleanfetch is always registered, result_ref reads and `fetch_top_n` are not gated |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `cleanfetch.timeout_sec` | 30 | |
| `cleanfetch.max_fetch_size_mb` | 10 | HEAD pre-check threshold; also the per-result byte cap for `fetch_top_n` |
| `cleanfetch.use_system_proxy` | false | Auto-use system proxy (env vars + Windows registry) |
| `cleanfetch.auto_proxy` | true | Native cleanfetch/PDF fetches follow the global proxy; false = force direct (independent of the API-provider `proxy.api_providers` switch) |
| `cleanfetch.max_retries` | 3 | Only retries on 429/502/503 |
| `pdf_parser.enabled` | false | Independent of cleanfetch |
| `pdf_parser.max_pages` | 20 | Max pages parsed per call when pages is omitted; a single pages range is capped at 1000 pages wide |
| `pdf_parser.mineru_model` | pipeline | pipeline / vlm |
| `pdf_parser.mineru_formula` | true | Formula recognition |
| `pdf_parser.mineru_table` | true | Table recognition |
| `pdf_parser.mineru_lang` | ch | Document language |
| `pdf_parser.mineru_page_limit` | 600 | Standard API per-task page limit; requests beyond it trigger the local qpdf crop fallback |
| `pdf_parser.mineru_page_batch_size` | 0 | Auto batch size for crop fallback: 0=no batching; 10-200=pages per batch (1-9 treated as 0, >200 clamped to 200) |
| `pdf_parser.mineru_page_budget` | 0 | MinerU page budget per call (0=same as page_limit); on exhaustion returns the parsed part and suggests continuation pages |
| `smartsearch.show_meta` | true | Show engine source and relevance score in output |
| `smartsearch.fetch_top_n` | 0 | Server-side default body-fetch count (applies when the agent omits `fetch_top_n`); default 0 = no fetch (same as before), 1-5 = one search returns full text (API engines use the fast path, web engines fetch internally) |
| `smartsearch.enhance` | true | Local scoring enhancement |
| `smartsearch.relevance_threshold` | 0.05 | Relevance threshold after enhancement |
| `smartsearch.inline_max_chars` | 32768 | Inline char limit for the rendered response; over the limit the whole result is dumped to a temp file (search-*.md under fetchdata/, kept 7 days; the response keeps path and failure list). 0 = default, negative = disable dumping |
| `smartsearch.off_topic_guard` | shadow | Off-topic bucket guard mode: shadow logs without dropping / enforce drops the whole bucket and reports / off disables |
| `smartsearch.engine_timeout_sec` | 10 | Per-engine soft timeout (seconds): a timed-out engine is reported as `timeout` in the failure list while other engines still return results; negative = rely on the request ctx deadline only |
| `so360.enabled` | false | 360 search engine (so.com, second Chinese index, keyless, direct-connect reachable from CN; verified 2026-10-06) |
| `so360.blocked` | [] | 360-specific blocked domains (merged with black_list_host) |
| `wikipedia.enabled` | false | Wikipedia engine (MediaWiki API, keyless, reference-type source; needs proxy, verified 2026-10-06) |
| `wikipedia.lang` | zh | Language edition |
| `google_news.enabled` | false | Google News RSS engine (keyless news index with structured dates; needs proxy, verified 2026-10-06; redirect links are resolved to publisher URLs) |
| `google_news.edition` | zh-CN | News edition (zh-CN/zh-TW/en-US/en-GB/ja-JP/ko-KR) |
| `github.enabled` | false | GitHub engine (REST API repository search, zero-key anonymous tier 10 searches/min; direct connection from CN, verified 2026-10-24) |
| `github.api_key` | "" | PAT (github_pat_/ghp_ prefix); raises search to 30/min and core to 5000/h; shared with cleanfetch repo/blob special-casing; env `GITHUB_TOKEN` |
| `github.token_list` | [] | Multi-PAT rotation list (takes priority over api_key) |
| `github.num_results` | 10 | Results per search (cap 50) |
| `github.per_min` | 10/30 | Search rate limit per minute (default 10/30 depending on PAT) |
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
