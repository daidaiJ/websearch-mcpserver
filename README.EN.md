# websearch-mcpserver

> Lightweight Web Search MCP Server — runs with zero API keys

<p align="center">
  <a href="README.EN.md">English</a> · <a href="README.MD">中文</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go" alt="Go"></a>
  <a href="https://github.com/daidaiJ/websearch-mcpserver/releases"><img src="https://img.shields.io/github/v/release/daidaiJ/websearch-mcpserver" alt="Release"></a>
  <img src="https://img.shields.io/badge/MCP-Streamable%20HTTP%20%2F%20stdio-7C3AED" alt="MCP">
  <img src="https://img.shields.io/badge/API%20Key-optional-22C55E" alt="Zero API Key">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT"></a>
</p>

<p align="center">
  <img src="docs/images/hero-banner.png" alt="Multi-engine search fusion: Baidu, Bing, DuckDuckGo and academic sources merge locally into structured results" width="900">
</p>

An MCP search service written in Go. Built-in Baidu web search, Bing, DuckDuckGo and other general-purpose engines plus 9 academic search engines. Search, scoring, and caching all happen locally. Use it as MCP tools in Claude Code, Qwen Code, or Cursor, or embed it as a Go module in your own service.

**Free, China-friendly, LLM-ready.** Works with zero keys; keys are used only when you provide them.

---

## Architecture at a glance

Layered design: clients see four MCP tools; the engine group is assembled by `mode`; scoring, cache, proxy, and fetch all run in-process. Queries never pass through a third-party aggregator.

<p align="center">
  <img src="docs/images/architecture.png" alt="System architecture: client, protocol, orchestration, general/academic engines, supporting components" width="900">
</p>

| Layer | Role |
|-------|------|
| **Client** | Claude Code / Qwen Code / Cursor / HTTP API / embed as a Go module |
| **Protocol** | `/mcp` four tools · `/searxng/search` for LiteLLM · `/__admin` process management · `/dashboard` optional local console |
| **Orchestration** | `factory` by mode · `hybrid` concurrent dedup/merge · RRF / boost / MMR scoring |
| **Engines** | General: Baidu web / Qianfan / Bing / DDG / Tavily / Exa / AnySearch / Doubao; 9 academic sources in parallel |
| **Support** | SQLite cache, system-proxy auto-detect, webfetch (SSRF), MinerU, streaming LLM summary |

Fallback chain, proxy detection, and embedding details: [docs/architecture.en.md](docs/architecture.en.md).

---

## A complete tool chain for LLMs

Four tools cover the web workflow. Results feed into each other — one config enables the whole chain:

<p align="center">
  <img src="docs/images/toolchain.png" alt="smartsearch → academicsearch → cleanfetch → pdf_parser toolchain" width="900">
</p>

---

## Key Features

| Capability | Description |
|------------|-------------|
| Zero-key search | `engine` mode runs Baidu web search + Bing concurrently, no API keys required |
| Multi-engine fusion | Multiple search modes, 8 general engines + 9 academic engines, auto-fallback on primary failure |
| Relevance scoring | RRF fusion ranking + lexical alignment / domain quality / consensus / authority / recency boosts, low-score results pruned; MMR breaks up mirrors / reposts |
| Academic search | 9 academic engines in parallel, scored by citation count / journal authority / PDF availability / recency; cross-engine DOI dedup |
| Web fetching | `cleanfetch` with built-in SSRF / DNS-rebinding protection and oversized-file pre-check; fallback to Jina Reader |
| PDF parsing | Local PDFs prefer text extraction; scanned PDFs can fall back to MinerU OCR |
| LLM summarization | Optional OpenAI-compatible API for structured summaries, with streaming progress |
| System proxy | Once Clash etc. enables the system proxy, overseas engines / Jina Reader use it automatically |
| Local console | Optional `dashboard.enabled`, a read-only `/dashboard/` UI: call stats, source health, failure classes, whitelisted config edits (off by default) |
| Lightweight deploy | Single binary, no CGO, reference-counted process management, embeddable as a Go module |

---

## Search & scoring pipeline

Results are not raw aggregation. After engines return, the server locally dedups, fusion-ranks, and re-ranks for diversity, then optionally summarizes:

<p align="center">
  <img src="docs/images/pipeline.png" alt="Query flows through factory, concurrent search, dedup, RRF, boost, threshold, MMR, then returns" width="900">
</p>

---

## Design Background & Goals

### Why this project

LLMs need web search, but existing MCP search solutions don't meet my preferences and needs:

- **Vendor MCP services (Tavily / Exa, etc.)**: require API key registration and per-use payment (Tavily ~$8/1k, Exa $7/1k) with limited free tiers; data passes through third-party servers and cannot be self-hosted; overseas services are unstable and hard to pay for from China; a single provider with no fallback when rate-limited or down; search only — academic search, web fetch, PDF parsing, and summarization all need separate integrations.
- **Self-hosted SearXNG + MCP wrapper**: requires deploying and maintaining a Python service (Docker, config, upgrades), and public instances are often rate-limited or blocked; results are raw aggregation with no LLM optimization (no relevance scoring, no dedup, no summarization); general web search only — no academic engines, fetch, or PDF; proxy must be configured manually.

So I started in 2026-04 with a single Baidu Qianfan engine and evolved it into a multi-engine fused general search service. The goal is to make search a **free, China-friendly, LLM-ready** basic capability.

### Differences from existing solutions

| Dimension | Vendor MCP (Tavily / Exa) | SearXNG MCP | This project |
|-----------|---------------------------|-------------|--------------|
| Cost | Per-use, limited free tier | Free but self-hosted | Free, zero config |
| Deployment | Register and use | Docker / Python self-hosted | Single binary, no CGO |
| China-friendly | Poor (overseas) | Manual proxy config | System proxy auto-detection |
| Provider resilience | Single provider, no fallback | Engine aggregation | Multi-engine + auto-fallback |
| LLM optimization | Raw results | Raw results | Local scoring + dedup + optional summary |
| Academic search | No | No | 9 academic engines |
| Fetch / PDF | Separate integration | No | Built-in cleanfetch / pdf_parser |
| Data privacy | Third-party servers | Local | Local |

### Design principles

**Local-first, private by default** — Search, scoring, and caching all happen locally; queries go only to the search engines themselves, with no third-party aggregation service in between. Data never leaves your machine — the most fundamental difference from vendor MCP services that route data through third-party servers.

**Zero cost to start, pay only for what you use** — Free engines (Baidu web + Bing) work with zero keys; local heuristic scoring burns no AI tokens; SQLite caching avoids repeated requests. Keys are used only when you provide them — you never pay for capabilities you don't use.

**Scalable complexity** — One config file, with `mode` scaling from `engine` (zero config) to `hybrid` (all engines). Zero-config and power users each get what they need without paying for complexity.

**Decoupled and composable** — Engines, modes, and tools are not coupled: `mode` decides the engine group, the 4 tools each have their own `enabled` switch, keys are optional (`sk_list` multi-key rotation). Everything is config-driven (per-engine filtering, scoring thresholds, MMR, blocked sites, rate limits) — all tunable, nothing hardcoded.

**A complete tool chain for LLMs** — The 4 tools cover the full web workflow: `smartsearch` → `academicsearch` → `cleanfetch` → `pdf_parser`, with results feeding into each other — one config enables the whole chain.

**Scenario-specific optimization** — Optimized for real usage scenarios: academic search (9 engines + citation / journal / PDF scoring), China networking (direct connect + system proxy auto-detection), scanned PDFs (MinerU OCR fallback), recency queries (`time_range`).

---

## Quick Start

```bash
# 1. Download a binary: https://github.com/daidaiJ/websearch-mcpserver/releases
# 2. Start (no hand-written config, no API keys)
#    Windows auto-start on boot (optional)
./websearch-mcpserver.exe install
#
#    The first `install` writes an editable preset config.yaml and autostart.vbs next to the executable
./websearch-mcpserver start
#    Or double-click
autostart.vbs
# 3. Register with your MCP client (see docs/installation.md)
```

> "Zero config" = the first start writes a preset `config.yaml` identical to `config.example.yaml`; edit that file for port/keys/mode. The daemon listens on `127.0.0.1` by default; when opening the network (`host: 0.0.0.0`), configure `auth_token` to protect business endpoints.

Or use MCP Hooks for session auto start/stop (Qwen Code example; full details in [docs/installation.md](docs/installation.md)):

```json
{
  "hooks": {
    "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver start", "timeout": 10000 }] }],
    "SessionEnd":   [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver stop",  "timeout": 10000 }] }]
  }
}
```

---

## Search Modes at a Glance

| Mode | Description | Key Required |
|------|-------------|--------------|
| `engine` | Baidu web search + Bing (DuckDuckGo joins when a proxy is available) | **None** |
| `baidu` | Baidu Qianfan search, falls back to Baidu web search | Optional |
| `apipool` | API key pool rotation: one provider per request, auto-switch on failure; supports round-robin / priority / weighted | All optional |
| `tavily` | Tavily Search API ([get key](https://app.tavily.com/home)) | `TAVILY_SK` |
| `exa` | Exa Web Search API ([get key](https://dashboard.exa.ai/api-keys)) | `EXA_API_KEY` |
| `anysearch` | AnySearch API ([get key](https://www.anysearch.com/console/api-keys)) | `ANYSEARCH_API_KEY` |
| `doubao` | Doubao Search Global / Custom ([get key](https://console.volcengine.com/search-infinity/api-key)) | `DOUBAO_SEARCH_API_KEY` |
| `hybrid` | Full mix (Anysearch + Baidu + Tavily + Exa + Doubao if keyed + Bing + DuckDuckGo, etc.) | All optional |

> Auto-degrades to `engine` mode when keys are missing. See [docs/search.md](docs/search.md) for mode and engine details.

---

## Local Control Center (optional, off by default)

> Full configuration reference (shortcut placement, brand about-block, every key and default) lives in [docs/dashboard.en.md](docs/dashboard.en.md).

With `dashboard.enabled: true`, open `http://127.0.0.1:8338/dashboard/` in a local browser to see four pages:

![Control center overview](docs/images/dashboard-overview.jpg)

| Page | What it shows |
|------|---------------|
| **Overview** | KPIs (calls / success / failure / avg latency), system status (including suspended count), running configuration, observation status of the four tools |
| **Sources** | Per-source health, last 20 results, failure composition (e.g. `parse ×4`), success rate, avg / P95 latency, quotas, latest error and suspension countdown |
| **Usage** | Tools and sources in separate dimensions; filter by level / status / tool / source / error kind; click a request id on a tool row to expand the source chain of that call |
| **Settings** | Writes whitelisted config (mode, timeouts, thresholds, suspension durations, etc.) after backing up the current YAML; secrets are never echoed |

Behavior boundaries:

- **Passive observation only**: records metadata produced by real MCP calls; no active probing, no fabricated data. Tools never called still show "callable · not observed yet".
- **Failure classes + confidence**: failures are classified as rate limit / captcha / access denied / timeout / network / parse / no result; fewer than 5 samples is flagged "insufficient samples", and only 3 consecutive failures show suspension — **reporting only, calls are never skipped**.
- **Request-level correlation**: one tool call and the source events it triggered share a request id, answering "who supplied this result, who failed".

To enable: copy `dashboard.example.yaml` to `dashboard.yaml` next to the main config (recommended), or append a `dashboard:` block to `config.yaml`:

```yaml
dashboard:
  enabled: true
  storage_path: ./data/dashboard.db
  retention_days: 30                          # detail retention days; daily rollups kept long-term
  secrets_path: ./data/dashboard-secrets.json # private overlay file; the API never returns values
  # admin_password / allowed_networks / quotas / branding: see dashboard.example.yaml
```

On first `start` / `install`, the server auto-generates this file **enabled by default** (with a random local admin password); set `enabled: false` or delete the file to turn it off with zero telemetry overhead. The overlay file overrides the main config field-by-field; deleting it and restarting is a clean rollback. The admin password, allowed networks, quotas and branding live only in `dashboard.yaml` — **the WebUI can never read or modify them**. Takes effect after restart; the desktop shortcut (created by `install`) is a lazy-start entry: it launches the server if needed, then opens the console. Machine-readable endpoints (read-only, never trigger searches):

```bash
curl http://127.0.0.1:8338/__admin/api/providers   # per-source state machine and failure composition
curl http://127.0.0.1:8338/__admin/api/metrics     # Prometheus text metrics
```

### Data & privacy

- Only sanitized metadata is stored: queries are persisted as hash + topic + language + keywords; **full queries and URLs never reach the database**. Error text is sanitized server-side (URLs / emails / secret-like strings replaced with placeholders) before storage and truncation.
- Secrets live in a separate private overlay file; pages and APIs **never echo the value** — the server simply never sends it, not a client-side mask. The settings page links each provider's official "get key" console.
- Loopback-only by default; networks listed in `allowed_networks` are **read-only**. Writes (settings / keys / restart / cache clear / quota management) require `dashboard.admin_password` and loopback; the password itself cannot be changed through the WebUI.
- The overview "client usage" panel groups calls by MCP client (identified from the initialize handshake `clientInfo` and User-Agent; local display grouping only, never affects search).
- No active probing of any provider.

Full configuration reference: [docs/configuration.en.md](docs/configuration.en.md).

---

## Documentation

| Document | Contents |
|----------|----------|
| [docs/installation.md](docs/installation.md) | Installation (4-platform binaries / GHCR linux amd64+arm64 / source / client registration), operations & troubleshooting |
| [docs/configuration.md](docs/configuration.md) | Full config reference, environment variable overrides, defaults quick reference |
| [docs/search.md](docs/search.md) | Search modes, engine reference, relevance scoring, MCP tool parameters |
| [docs/architecture.md](docs/architecture.md) | Architecture, fallback chain, proxy detection, caching, Go module embedding, web-researcher extension |
| [docs/api.md](docs/api.md) | Go Module API and HTTP API (MCP / SearXNG / Admin endpoints) |
| [CHANGELOG.md](CHANGELOG.md) | Version changelog |

## Related Projects

- [web-researcher](https://github.com/daidaiJ/web-researcher) — companion Qwen Code extension that offloads web research to a sub-agent, keeping the main model's context clean (see [docs/architecture.md](docs/architecture.md))
