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
  <img src="docs/images/hero-banner.png" alt="Multi-engine search fusion: Baidu, Bing, DuckDuckGo, API engines and 9 academic sources merge locally into search results, papers, pages/PDFs and local files" width="900">
</p>

An MCP search service written in Go. Built-in Baidu web search, Bing, DuckDuckGo and other general-purpose engines plus 9 academic search engines. Search, scoring, and caching all happen locally. Use it as MCP tools in Claude Code, Qwen Code, or Cursor, or embed it as a Go module in your own service.

**Free, China-friendly, LLM-ready.** Works with zero keys; keys are used only when you provide them.

---

## Architecture at a glance

Layered design: clients see up to five MCP tools; the engine group is assembled by `mode`; scoring, cache, proxy, and fetch all run in-process. Queries never pass through a third-party aggregator.

<p align="center">
  <img src="docs/images/architecture.png" alt="System architecture: client, retrieval protocol and dashboard, orchestration, 8 general and 9 academic engines, supporting components" width="900">
</p>

| Layer | Role |
|-------|------|
| **Client** | Claude Code / Qwen Code / Cursor / HTTP API / embed as a Go module |
| **Protocol** | `/mcp` up to five tools · `/searxng/search` for LiteLLM · `/__admin` process management · `/dashboard` optional local console |
| **Orchestration** | `factory` / `mode` · `hybrid` / `apipool` · `enhance` (RRF / Boost / MMR) |
| **Engines** | General: Baidu web / Qianfan / Bing / DDG / Tavily / Exa / AnySearch / Doubao; 9 academic sources in parallel |
| **Support** | SQLite cache, system-proxy auto-detect, webfetch (SSRF), MinerU, streaming LLM summary, Everything |

Fallback chain, proxy detection, and embedding details: [docs/architecture.en.md](docs/architecture.en.md).

---

## A complete tool chain for LLMs

Five retrieval tools feed into each other. Saved long search, webpage and PDF bodies are read through `cleanfetch` without client access to container files. Start at `offset=0`, then follow `next_offset` until `eof=true`.

<p align="center">
  <img src="docs/images/toolchain.png" alt="smartsearch → academicsearch → cleanfetch → pdf_parser toolchain, plus file_search for local files" width="900">
</p>

---

## Key Features

| Capability | Description |
|------------|-------------|
| Zero-key search | `engine` mode runs Baidu web search + Bing concurrently, no API keys required |
| Multi-engine fusion | Multiple search modes, 8 general engines + 9 academic engines, auto-fallback on primary failure |
| Relevance scoring | RRF fusion ranking + lexical alignment / domain quality / consensus / authority / recency boosts, low-score results pruned; MMR breaks up mirrors / reposts |
| Response transparency | Failure lists, date and retrieval provenance; request cancellation propagates through every engine with a per-engine soft timeout; oversize search results retain candidate previews and a `result_ref` for complete body delivery through the `cleanfetch` saved-result mode |
| Academic search | 9 academic engines in parallel, scored by citation count / journal authority / PDF availability / recency; cross-engine DOI dedup |
| Web fetching | `cleanfetch` with built-in SSRF / DNS-rebinding protection and oversized-file pre-check; fallback to Jina Reader |
| PDF parsing | Local PDFs prefer text extraction; scanned PDFs can fall back to MinerU OCR; remote MinerU returns the ZIP URL with images; when the source exceeds the per-task page limit (default 600), selected pages are cropped locally with qpdf, with configurable auto-batching and a per-call page budget ; bounded MinerU waiting with state and task references for resuming existing jobs |
| LLM summarization | Optional OpenAI-compatible API for structured summaries, with streaming progress |
| System proxy | Once Clash etc. enables the system proxy, overseas engines / Jina Reader use it automatically |
| Local file search | `file_search` uses the Everything index for millisecond path lookup, with directory allowlists and lexical filtering (requires Everything HTTP Server) |
| Local console | Optional `dashboard.enabled`, a read-only `/dashboard/` UI: call stats, source health, failure classes, whitelisted config edits (off by default) |
| Lightweight deploy | Single binary, no CGO, reference-counted process management, embeddable as a Go module |

---

## Search & scoring pipeline

Results are not raw aggregation. After engines return, the server locally dedups, fusion-ranks, and re-ranks for diversity, then optionally summarizes:

<p align="center">
  <img src="docs/images/pipeline.png" alt="Query flows through factory/mode, concurrent search, dedup, RRF, boost, threshold, MMR, then returns" width="900">
</p>

---

## Design Background & Goals

Existing MCP search solutions each fall short: vendor services (Tavily / Exa, etc.) require keys and per-use payment, route data through third-party servers, and are unstable from China; self-hosted SearXNG means maintaining a Python service and returns unoptimized raw aggregation. This project brings those capabilities back local: a single binary with multi-engine fusion, 9 academic sources, web fetch and PDF parsing — making search a **free, China-friendly, LLM-ready** basic capability.

The full project motivation, a dimension-by-dimension comparison with existing solutions, and the design principles (local-first privacy / zero cost to start / scalable complexity / decoupled and composable / a complete tool chain for LLMs / scenario-specific optimization) live in [docs/design.en.md](docs/design.en.md).

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

The single `mode` field decides the engine group: `engine` works with zero keys (Baidu web + Bing in parallel; 360 / DuckDuckGo / Wikipedia / Google News join automatically per config and proxy availability); `baidu` / `tavily` / `exa` / `anysearch` / `doubao` are single-provider modes (they participate only with a key, with auto-fallback on failure); `apipool` rotates a key pool (round-robin / priority / weighted, Baidu web as the final fallback); `hybrid` runs all engines concurrently. Missing keys auto-degrade to `engine`.

Participating engines per mode, key-signup links, `sk_list` multi-key rotation, and per-engine rate limits: [docs/search.en.md](docs/search.en.md).

---

## Local Control Center (optional, off by default)

With `dashboard.enabled: true`, open `http://127.0.0.1:8338/dashboard/` in a local browser. Four pages: overview KPIs and client usage grouping, per-source health and failure composition, request-level call chains (filterable, CSV export), and whitelisted config writes after backup; fully passive observation — failure classes are reported only, calls are never skipped.

![Control center overview](docs/images/dashboard-overview.jpg)

To enable, copy `dashboard.example.yaml` to `dashboard.yaml` next to `config.yaml` (first `start` / `install` auto-generates it **enabled by default** with a random local admin password); delete the file and restart for a clean rollback with zero telemetry overhead. Privacy defaults: queries are stored only as sanitized hash + topic metadata; secrets and raw query text never reach the database and are never echoed; loopback-only reads by default, writes require `admin_password`.

> Page details, every config key, shortcut placement, the security model and call attribution: [docs/dashboard.en.md](docs/dashboard.en.md).

---

## Documentation

> All documents default to Chinese; the same-named `.en.md` file is the English edition.

| Reader | Document | Contents |
|--------|----------|----------|
| 👤 Human users | [docs/HUMAN_GUIDE.md](docs/HUMAN_GUIDE.md) | Human edition: doc routing, mode selection, tuning path, troubleshooting quick reference |
| 🤖 AI agents | [docs/AGENT_GUIDE.md](docs/AGENT_GUIDE.md) | Token-efficient edition: deploy quick reference, tool parameters, machine contract, error diagnosis |
| 📚 Reference | [docs/installation.md](docs/installation.md) | Installation (4-platform binaries / GHCR linux amd64+arm64 / source / client registration), operations & troubleshooting |
| 📚 Reference | [docs/configuration.md](docs/configuration.md) | Full config reference, environment variable overrides, defaults quick reference |
| 📚 Reference | [docs/search.md](docs/search.md) | Search modes, engine reference, relevance scoring, MCP tool parameters |
| 📚 Reference | [docs/architecture.md](docs/architecture.md) | Architecture, fallback chain, proxy detection, caching, Go module embedding, web-researcher extension |
| 📚 Reference | [docs/design.md](docs/design.md) | Design background & goals: project motivation, comparison with existing solutions, design principles |
| 📚 Reference | [docs/api.md](docs/api.md) | Go Module API and HTTP API (MCP / SearXNG / Admin endpoints) |
| 📚 Reference | [docs/dashboard.md](docs/dashboard.md) | Local dashboard: every config key, write-operation security model, privacy boundaries |
| 📚 Reference | [docs/developers.md](docs/developers.md) | Developer guide: package layout, interface contracts, tool call chains, change touchpoints |
| 📚 Reference | [CHANGELOG.md](CHANGELOG.md) | Version changelog |

## Related Projects

- [web-researcher](https://github.com/daidaiJ/web-researcher) — companion Qwen Code extension that offloads web research to a sub-agent, keeping the main model's context clean (see [docs/architecture.md](docs/architecture.md))
