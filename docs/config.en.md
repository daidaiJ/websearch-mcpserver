# Configuration Reference

[English](config.en.md) | [中文](config.md)

## Config File Path

Priority (high to low):
1. Environment variable `WEBSEARCH_CONFIG`
2. CLI flag `-c / --config`
3. Current directory `config.yaml`

> When specified via `-c`, PID file and log file are automatically written to the config file's directory.

## Full Configuration

```yaml
port: 8338                  # MCP HTTP port
log_level: info             # debug / info / warn / error
mode: engine                # baidu / tavily / hybrid / engine
network: china              # china (skip overseas engines) / international

# Blocked sites (applies to all search engines)
black_list_host:
  - "csdn.net"
  - "baidu.com"

# Baidu Qianfan (required for mode=baidu or hybrid)
baidu:
  api_key: ""               # Env: BAIDU_SK

# Tavily (required for mode=tavily or hybrid)
tavily:
  api_key: ""               # Env: TAVILY_SK

# Bing engine (fallback + engine mode primary, no key needed)
bing:
  enabled: true             # Master switch
  blocked: []               # Bing-specific blocked domains (merged with black_list_host)
  per_sec: 1                # Rate limit per second
  per_min: 20               # Rate limit per minute

# Academic engines (no key needed)
academic:
  enabled: true             # Master switch, registers academicsearch tool
  bing_fallback: true       # Use Bing as fallback for academic search
  disable_arxiv: false
  disable_crossref: false
  disable_openalex: false
  disable_pubmed: false
  disable_semantic_scholar: true    # Disabled by default (auto-proxied when enabled)
  disable_google_scholar: true      # Disabled by default (auto-proxied when enabled)

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

# PDF parser (disabled by default, independent of cleanfetch)
pdf_parser:
  enabled: false            # Must be explicitly true to enable

# Log rotation
log:
  max_size: 1               # Max file size (MB)
  max_age: 1                # Retention (days)
```

## Environment Variable Overrides

| Env Var | Overrides | Notes |
|---------|-----------|-------|
| `WEBSEARCH_CONFIG` | Config file path | Highest priority |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | |
| `LLM_BASE_URL` | `llm.base_url` | |
| `LLM_API_KEY` | `llm.api_key` | |

> Viper's `AutomaticEnv()` also supports `APP_` prefix for overriding any config field.

## Default Values Quick Reference

| Field | Default | Notes |
|-------|---------|-------|
| `port` | 8338 | stop/kill/status also use this port when no config |
| `mode` | baidu | Auto-degrades to engine when no keys |
| `network` | china | |
| `bing.enabled` | true | |
| `bing.per_sec` | 1 | |
| `bing.per_min` | 20 | |
| `academic.enabled` | true | |
| `academic.bing_fallback` | true | |
| `proxy.enabled` | false | Auto-detects system proxy when not set; explicit false disables |
| `proxy.endpoint` | `http://127.0.0.1:7897` | Only effective when `enabled: true` |
| `cleanfetch.enabled` | false | Old configs don't enable; must be explicit |
| `cleanfetch.file_ttl_hours` | 24 | |
| `cleanfetch.max_inline_lines` | 100 | |
| `pdf_parser.enabled` | false | Independent of cleanfetch |
| `cache.enabled` | nil | Not set → judge by storage_path; explicit false → force disable; explicit true → force enable |
| `cache.cleanup_interval` | 30 (min) | Max 360 |
| Cache expiry | 6 hours | Based on last hit time, hardcoded |
| `log.max_size` | 1 (MB) | |
| `log.max_age` | 1 (day) | |

## Minimal Config

```yaml
port: 8338
mode: engine
```

Runs with zero API keys using Bing + academic search engines.
