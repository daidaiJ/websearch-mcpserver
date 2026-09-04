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

Key env vars work the same as HTTP: `BAIDU_SK`, `TAVILY_SK`, `EXA_API_KEY`, `LLM_BASE_URL`, `LLM_API_KEY`, `MINERU_TOKEN`, etc. When running with in-memory defaults (no config file), those key-related env vars are still applied; full field defaults still follow the "load file + Viper" path when a file is present.

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
log_level: info             # debug / info / warn / error
mode: engine                # baidu / apipool / tavily / exa / anysearch / hybrid / engine
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
  api_key: ""               # Env: BAIDU_SK (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)
  enable_ai_search: true    # true=AI search chat/completions (default), false=web search web_search
  model: ""                 # AI search model, empty=free Baidu search (no LLM cost), set name=LLM intelligent search
  search_source: "baidu_search_v2" # Search engine version
  enable_reasoning: false   # Deep reasoning
  enable_deep_search: false # Deep search
  search_mode: "auto"       # auto / required / disabled

# Tavily (required for mode=tavily/apipool/hybrid)
tavily:
  api_key: ""               # Env: TAVILY_SK (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)

# Exa (required for mode=exa/apipool/hybrid)
exa:
  api_key: ""               # Env: EXA_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key)
  num_results: 5            # Results per search (default 5)
  lookback_days: 90         # Search time range (days), default 90

# AnySearch (required for mode=anysearch/apipool/hybrid)
anysearch:
  api_key: ""               # Env: ANYSEARCH_API_KEY (falls back to single-element sk_list when empty)
  sk_list: []               # Multi-key rotation list (priority over api_key; duplicate keys are deduplicated)
  num_results: 10           # Results per search (default 10)

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
  disable_europepmc: false  # Europe PMC biomedical supplement (direct from China)
  disable_dblp: false       # DBLP CS conference/journal index (direct from China)
  disable_doaj: false       # DOAJ open-access journals (direct from China)

# Proxy (auto-detects system proxy by default, no manual config needed)
proxy:
  enabled: false          # Empty → auto-detect; true → use endpoint; false → disable
  endpoint: "http://127.0.0.1:7897"  # Only effective when enabled: true

# LLM summary (optional)
llm:
  base_url: "https://api.openai.com/v1"   # Env: LLM_BASE_URL
  api_key: ""                               # Env: LLM_API_KEY
  model_id: "gpt-4o-mini"

# Cache
cache:
  # enabled: true            # Not set → judge by storage_path; explicit false → force disable
  storage_path: "./data/search_cache.db"
  cleanup_interval: 30      # Cleanup interval (minutes), max 360

# Jina Reader (optional, fallback for cleanfetch)
jina:
  api_key: ""               # Empty → Jina fallback disabled
  base_url: ""              # Default https://r.jina.ai

# Enhanced web fetch (disabled by default)
cleanfetch:
  enabled: false            # Must be explicitly true to enable
  file_output_dir: ""       # Default: system temp dir /webfetch/
  file_ttl_hours: 24        # Temp file retention (hours)
  max_inline_lines: 100     # Lines above this threshold stored to file
  max_inline_chars: 0       # Chars above this threshold stored to file, 0=unlimited
  timeout_sec: 30           # Per-request timeout (seconds), default 30
  max_fetch_size_mb: 10     # HEAD pre-check max file size (MB), reject above (default 10)
  use_system_proxy: false   # Auto-use system proxy (env vars + Windows registry), default false
  max_retries: 3            # Max retries (429/502/503 only), default 3

# PDF parser (disabled by default, independent of cleanfetch)
# MinerU AI enhancement (optional): with Token uses Standard API (remote URL, ≤200MB), without Token uses Agent API (local file, ≤10MB)
# Get Token: https://mineru.net/apiManage | Env: MINERU_TOKEN
pdf_parser:
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
#   engines:              # Per-engine config (names: tavily_api, exa, baidu_api, baidu, bing, google, duckduckgo)
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

# Apipool mode config (optional, effective when mode=apipool)
# apipool:
#   strategy: weighted    # round-robin (default): rotate starting provider across requests
#                         # priority: always start from first provider
#                         # weighted: weighted-random starting provider (see weights)
#   engines:              # Provider priority order (default [anysearch, baidu, tavily, exa], Baidu web search fallback always last)
#     - anysearch
#     - baidu
#     - tavily
#     - exa
#   weights:              # weighted strategy weights (per-key, accumulated by available key count)
#     anysearch: 30000    # defaults: anysearch=30000, baidu=1500, tavily=1200, exa=1200
#     baidu: 1500
#     tavily: 1200
#     exa: 1200

# Log rotation
log:
  max_size: 1               # Max file size (MB)
  max_age: 1                # Retention (days)
```

---

## Environment Variable Overrides

| Env Var | Overrides | Notes |
|---------|-----------|-------|
| `WEBSEARCH_CONFIG` | Config file path | Highest priority |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | |
| `EXA_API_KEY` | `exa.api_key` | Exa Web Search API Key |
| `ANYSEARCH_API_KEY` | `anysearch.api_key` | AnySearch API Key ([anysearch.com](https://www.anysearch.com/docs)) |
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
| `network` | china | |
| `rate_limit.per_sec` | 3 | Global rate limit |
| `rate_limit.per_min` | 60 | Global rate limit |
| `apipool.strategy` | round-robin | `round-robin` rotates provider across requests / `priority` fixed order / `weighted` weighted-random |
| `apipool.engines` | [anysearch, baidu, tavily, exa] | Provider priority order, Baidu web search fallback always last |
| `apipool.weights` | anysearch=30000, baidu=1500, tavily=1200, exa=1200 | weighted per-key weights, accumulated by available key count |
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
| `academic.disable_europepmc` | false | Europe PMC biomedical supplement, reachable from China |
| `academic.disable_dblp` | false | DBLP CS conference/journal index, reachable from China |
| `academic.disable_doaj` | false | DOAJ open-access journals, reachable from China |
| `proxy.enabled` | unset | Auto-detects system proxy when not set; explicit false disables; explicit true uses endpoint |
| `proxy.endpoint` | `http://127.0.0.1:7897` | Only effective when `enabled: true` |
| `cleanfetch.enabled` | false | Old configs don't enable; must be explicit |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `cleanfetch.timeout_sec` | 30 | |
| `cleanfetch.max_fetch_size_mb` | 10 | HEAD pre-check threshold |
| `cleanfetch.use_system_proxy` | false | Auto-use system proxy (env vars + Windows registry) |
| `cleanfetch.max_retries` | 3 | Only retries on 429/502/503 |
| `pdf_parser.enabled` | false | Independent of cleanfetch |
| `pdf_parser.mineru_model` | pipeline | pipeline / vlm |
| `pdf_parser.mineru_formula` | true | Formula recognition |
| `pdf_parser.mineru_table` | true | Table recognition |
| `pdf_parser.mineru_lang` | ch | Document language |
| `smartsearch.show_meta` | true | Show engine source and relevance score in output |
| `smartsearch.enhance` | true | Local scoring enhancement |
| `smartsearch.relevance_threshold` | 0.05 | Relevance threshold after enhancement |
| `smartsearch.mmr.enabled` | true | MMR diversity re-ranking |
| `smartsearch.mmr.lambda` | 0.7 | Relevance-diversity tradeoff |
| `cache.enabled` | nil | Not set → judge by storage_path; explicit false → force disable; explicit true → force enable |
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
