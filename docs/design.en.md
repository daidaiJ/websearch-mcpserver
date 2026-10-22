# Design Background & Goals

English | [中文](design.md)

> This document explains why the project exists, how it differs from existing solutions, and the design principles behind the implementation. For a feature overview and quick start see the [README](../README.EN.md); for technical architecture see [architecture.en.md](architecture.en.md).

## Why this project

LLMs need web search, but existing MCP search solutions don't meet my preferences and needs:

- **Vendor MCP services (Tavily / Exa, etc.)**: require API key registration and per-use payment (Tavily ~$8/1k, Exa $7/1k) with limited free tiers; data passes through third-party servers and cannot be self-hosted; overseas services are unstable and hard to pay for from China; a single provider with no fallback when rate-limited or down; search only — academic search, web fetch, PDF parsing, and summarization all need separate integrations.
- **Self-hosted SearXNG + MCP wrapper**: requires deploying and maintaining a Python service (Docker, config, upgrades), and public instances are often rate-limited or blocked; results are raw aggregation with no LLM optimization (no relevance scoring, no dedup, no summarization); general web search only — no academic engines, fetch, or PDF; proxy must be configured manually.

So I started in 2026-04 with a single Baidu Qianfan engine and evolved it into a multi-engine fused general search service. The goal is to make search a **free, China-friendly, LLM-ready** basic capability.

## Differences from existing solutions

| Dimension | Vendor MCP (Tavily / Exa) | SearXNG MCP | This project |
|-----------|---------------------------|-------------|--------------|
| Cost | Per-use, limited free tier | Free but self-hosted | Free, zero config |
| Deployment | Register and use | Docker / Python self-hosted | Single binary, no CGO |
| China-friendly | Poor (overseas) | Manual proxy config | System proxy auto-detection |
| Provider resilience | Single provider, no fallback | Engine aggregation | Multi-engine + auto-fallback |
| LLM optimization | Raw results | Raw results | Local scoring + dedup + optional summary |
| Academic search | No | No | 9 academic engines |
| Local file search | No | No | `file_search` locates local files in milliseconds via the Everything index |
| Fetch / PDF | Separate integration | No | Built-in cleanfetch / pdf_parser |
| Data privacy | Third-party servers | Local | Local |

## Design principles

**Local-first, private by default** — Search, scoring, and caching all happen locally; queries go only to the search engines themselves, with no third-party aggregation service in between. Data never leaves your machine — the most fundamental difference from vendor MCP services that route data through third-party servers.

**Zero cost to start, pay only for what you use** — Free engines (Baidu web + Bing) and zero-key hosted MCP (Parallel / AnySearch / Exa / Firecrawl cascade) work without keys; local heuristic scoring burns no AI tokens; SQLite caching avoids repeated requests. Keys are used only when you provide them — you never pay for capabilities you don't use.

**Scalable complexity** — One config file, with `mode` scaling from `mcphub` / `engine` (zero keys) to `hybrid` (all engines). Zero-config and power users each get what they need without paying for complexity.

**Decoupled and composable** — Engines, modes, and tools are not coupled: `mode` decides the engine group, network capabilities follow their config switches (`cleanfetch` is always registered and its switch only gates URL fetching; `file_search` is probe-gated), keys are optional (`sk_list` multi-key rotation). Everything is config-driven (per-engine filtering, scoring thresholds, MMR, blocked sites, rate limits) — all tunable, nothing hardcoded.

**A complete tool chain for LLMs** — Five retrieval tools cover webpages, papers, PDFs and local files (`file_search` needs Windows + Everything and a successful probe). The `cleanfetch` saved-result mode delivers saved bodies through MCP.

**Scenario-specific optimization** — Optimized for real usage scenarios: academic search (9 engines + citation / journal / PDF scoring), China networking (direct connect + system proxy auto-detection), scanned PDFs (MinerU OCR fallback), recency queries (`time_range`). Web search omits the time filter by default for the last 3 months; an explicit `0` sends no time filter. The cache is isolated per time window, and date precision depends on the source.
