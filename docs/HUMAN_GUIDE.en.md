# HUMAN_GUIDE — User Manual (Human Edition)

English | [中文](HUMAN_GUIDE.md)

This manual is for **human users**: what to install, which mode to pick, which knobs to turn, and where to look first when something breaks. The token-efficient version for AI agents lives in [AGENT_GUIDE.en.md](AGENT_GUIDE.en.md); the project overview is in the [README](../README.EN.md).

## Contents

- [Which document should I read](#which-document-should-i-read)
- [30-second quick start](#30-second-quick-start)
- [Choosing a search mode](#choosing-a-search-mode)
- [How the four tools fit together](#how-the-four-tools-fit-together)
- [Tuning: observe first, then adjust](#tuning-observe-first-then-adjust)
- [Troubleshooting quick reference](#troubleshooting-quick-reference)
- [Security and privacy defaults](#security-and-privacy-defaults)

---

## Which document should I read

The detailed docs are split by topic; this manual is their reading router:

| I want to… | Read |
|------------|------|
| Install, register with a client, run as a daemon | [installation.en.md](installation.en.md) |
| Change settings, env-var overrides, look up defaults | [configuration.en.md](configuration.en.md) |
| Understand search modes, engines, scoring, tool parameters | [search.en.md](search.en.md) |
| Understand the architecture, fallback chain, proxy detection, Go module embedding | [architecture.en.md](architecture.en.md) |
| Call the HTTP API directly (MCP / SearXNG / Admin) | [api.en.md](api.en.md) |
| Enable the local dashboard | [dashboard.en.md](dashboard.en.md) |
| Contribute code, understand the codebase | [developers.en.md](developers.en.md) |

## 30-second quick start

```bash
# 1. Download the binary: https://github.com/daidaiJ/websearch-mcpserver/releases
# 2. Start (no config to write, no API key; first start generates a preset config.yaml)
./websearch-mcpserver start
# 3. Windows autostart (optional): ./websearch-mcpserver.exe install
# 4. Register with your MCP client (Claude Code / Qwen Code / Cursor)
```

Client registration JSON, Docker, systemd/launchd daemons and other deployment options: [installation.en.md](installation.en.md#installation).

## Choosing a search mode

`mode` decides which engines run; it is one field in `config.yaml` and can be changed anytime:

| Your situation | Suggested mode | Notes |
|----------------|----------------|-------|
| No / no desire for API keys | `engine` | Baidu web + Bing concurrent, zero-config (DuckDuckGo joins automatically when a proxy is available) |
| Only a Baidu Qianfan key | `baidu` | Qianfan search falls back to Baidu web search on failure |
| One overseas provider key | `tavily` / `exa` / `anysearch` / `doubao` | Single-provider mode; falls back to Bing without a key |
| Several keys, want quota-saving failover | `apipool` | One provider per request, auto-switch on failure, Baidu as final fallback |
| Many keys, want maximum coverage | `hybrid` | All engines concurrent; multi-engine consensus feeds the scoring |

> Missing keys auto-degrade to `engine` instead of erroring out. Engine mapping and fallback chains per mode: [search.en.md](search.en.md#search-modes).

## How the four tools fit together

Once registered, your LLM client gets 4 MCP tools covering one connected workflow:

| Tool | What it does | Typical use |
|------|--------------|-------------|
| `smartsearch` | General web search, multi-engine fusion + local scoring | Daily connected Q&A, news (`time_range` for freshness) |
| `academicsearch` | 9 academic engines in parallel | Finding papers; pass a DOI / arXiv id directly as `query` for exact lookup |
| `cleanfetch` | Fetch page content | Read a full article; `urls` batches up to 5 |
| `pdf_parser` | Parse PDFs (local text first, MinerU OCR fallback for scans; oversized sources are cropped and batched automatically) | Pass `pdf_url` from an `academicsearch` result to read the full text; use `pages` for long documents |

> If a tool is missing in the client, check the registration conditions (`bing.enabled` / `academic.enabled` / `cleanfetch.enabled` / `pdf_parser.enabled`), see [search.en.md](search.en.md#mcp-tools).

## Tuning: observe first, then adjust

**Look at the data first**: with the dashboard enabled, check the Overview and "Search Sources" pages (which source fails most, is slow, or rate-limited), or the machine-readable exits:

```bash
curl http://127.0.0.1:8338/__admin/api/providers   # per-source state machine and failure breakdown
curl http://127.0.0.1:8338/__admin/api/metrics     # Prometheus text metrics
```

**Then adjust** — each common want maps to one setting:

| Want | Where |
|------|-------|
| More / fewer results | `smartsearch.max_size` and per-engine `max_size` |
| Irrelevant results sneaking in | `smartsearch.relevance_threshold` (default 0.05, higher = stricter); or `min_score` on score-returning engines |
| Mirror / syndicated sites clustering | lower `smartsearch.mmr.lambda` (more diversity) |
| One engine is low quality | tighten its `min_score` / `max_size`, or drop it from the `engines` subset |
| Fresher content | tool parameter `time_range` (default last 3 months, `0` = unlimited) |
| Content instead of URLs | tool parameter `fetch_top_n: 1-5` (server default `smartsearch.fetch_top_n`) |
| Noisy academic results | `academic.threshold` (default 0.02, higher = stricter) |

All parameters and defaults: [configuration.en.md](configuration.en.md); scoring pipeline internals: [search.en.md](search.en.md#relevance-scoring).

## Troubleshooting quick reference

| Symptom / error | Nature | Fix |
|-----------------|--------|-----|
| Autostart fails with `0x800704C7` | Environment: SmartScreen / antivirus / UAC blocks the exe; the script itself is fine | `Unblock-File .\websearch-mcpserver.exe` → whitelist in the antivirus → run once in the foreground to see the blocked dialog (details in [installation.en.md](installation.en.md#autostart-error-0x800704c7antivirus--smartscreen-blocking)) |
| "not a valid Win32 application" | Environment: corrupted download or wrong architecture | Re-download the binary for your platform |
| Some tools missing after startup | Usage: registration conditions unmet | Check each tool's `enabled` and `bing.enabled` (table above) |
| "Some engines failed this run, results may be incomplete" | Network: some academic engines timed out / blocked | Expected degradation; overseas engines need a proxy (system proxy auto-detected), keep `network: china` in mainland China |
| Google / DuckDuckGo return nothing | Network: anti-bot / proxy unreachable | Engines skip automatically; Google is disabled by default (fully hardened anti-bot), do not attempt UA/TLS spoofing fixes |
| No results or frequent rate limiting | Config: `rate_limit` or upstream limits | Defaults 3/s, 60/min; DDG / arXiv have built-in clamps — do not loosen; 429s trigger automatic cooldown |
| Port already in use | Environment | `./websearch-mcpserver status` to check if already running; `kill` then restart |
| Stale results | Cache: SQLite cache expires after 6h | Delete the `cache.storage_path` file and restart if urgent |
| Docker container exits immediately / dashboard settings 403 | Deployment | Make sure `config.yaml` is mounted; over bridge networking the dashboard is read-only (writes require a loopback origin) |
| Process still alive after `stop` | Normal: graceful exit waits up to 10s | Use `kill` if it persists |

**General principle**: check the dashboard "Search Sources" page or `curl /__admin/api/providers` first to identify which source and failure class (rate limit / captcha / timeout / parse), then match the table above — separate environment problems (blocking / ports / corrupted transfers) from usage errors (config / parameters), consult the table before reinstalling anything.

## Security and privacy defaults

- **Listens on `127.0.0.1` only by default**; when binding a public interface (`host: 0.0.0.0`) set `auth_token` to protect business endpoints.
- **Search, scoring, and caching all happen locally**: queries go only to the search engines themselves, never through any third-party aggregation service.
- **Keys are never echoed**: dashboard pages and APIs never return raw key values; telemetry stores only sanitized metadata (query hash / topic / keywords), full queries and URLs never hit the database.
- **Writes are gated by a password**: dashboard writes (settings / restart / cache clear) require localhost + the admin password (configurable only in `dashboard.yaml`, which the WebUI itself cannot read or modify).

All dashboard settings and boundaries: [dashboard.en.md](dashboard.en.md).
