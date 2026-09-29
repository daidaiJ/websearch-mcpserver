# websearch-mcpserver — Agent Guide (Token-Efficient Edition)

English | [中文](AGENT_GUIDE.md)

MCP search server in Go: 4 tools (`smartsearch` / `academicsearch` / `cleanfetch` / `pdf_parser`), zero API key needed (`engine` mode), HTTP daemon or stdio. Full human-readable version: [HUMAN_GUIDE.en.md](HUMAN_GUIDE.en.md).

> **Anti-patterns**: ① Do not `cleanfetch` a search results page to "search" — that is `smartsearch`'s job; ② If you already hold a DOI / arXiv id, do not re-search by title — pass the id directly as `academicsearch`'s `query` for exact lookup; ③ Do not pass a web page URL to `pdf_parser` — pages go through `cleanfetch`.

## Deploy quick reference (priority order)

```bash
# 1. Download the binary (linux-amd64 example; other platforms in installation.md)
curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
  | grep "browser_download_url.*linux-amd64\"" | cut -d '"' -f 4 \
  | xargs curl -sL -o /usr/local/bin/websearch-mcpserver && chmod +x /usr/local/bin/websearch-mcpserver

# 2. Skip writing config: first start without -c auto-generates a preset config.yaml (runs with zero keys)
#    With an explicit -c path nothing is auto-generated; a missing file is an error

# 3. Start (HTTP daemon, default 127.0.0.1:8338)
./websearch-mcpserver start        # stop / kill / status / version are sibling subcommands

# 4. Register the client: url = http://127.0.0.1:8338/mcp (send Authorization header when auth_token is set)
```

**Environment self-check (30 seconds)**:

```bash
curl -s http://127.0.0.1:8338/__admin/health   # expect {"ref_count":N,"message":"running"}
./websearch-mcpserver status                   # state, port, refcount
```

**stdio alternative**: without HTTP, download `websearch-mcp-cli-*` and let the client spawn the process; the `init` subcommand writes a sample config, `version` prints the version. Config YAML is shared with HTTP mode.

Deployment details (Docker / systemd / launchd / hooks auto start-stop / per-client JSON): [installation.en.md](installation.en.md).

## Tool quick reference

Registration conditions: `smartsearch` needs `bing.enabled=true`; `academicsearch` needs `academic.enabled=true`; `cleanfetch` / `pdf_parser` each need their own `enabled=true`. Missing tool → check this first.

### smartsearch

| Param | Req | Notes |
|-------|-----|-------|
| `query` | ✅ | Search keywords |
| `time_range` | | Months, default 3; `0`=unlimited, `1`=last month, `6`=half year, `12`=one year |
| `fetch_top_n` | | Omit = titles/snippets/URLs only; `0` = explicitly no content fetch; `1-5` = fetch page content for the top N results |
| `intent` | | Only effective with LLM summarization enabled; removed automatically otherwise |

### academicsearch

| Param | Req | Notes |
|-------|-----|-------|
| `query` | ✅ | Keywords; or a DOI / arXiv id (`10.xxxx/...`, `2401.04085`, `doi:` / `arXiv:` prefix, or the corresponding URL) triggers single-paper exact lookup and ignores the other params |
| `engines` | | Subset: `arxiv` `crossref` `openalex` `pubmed` `europepmc` `dblp` `doaj` `semantic_scholar` `google_scholar` |
| `time_range` | | `year` / `month` / `week` / `day` |
| `page` | | Page number, default 1 |

Once a result carries `pdf_url`, pass it straight to `pdf_parser` for the full text — do not re-search by title.

### cleanfetch

| Param | Req | Notes |
|-------|-----|-------|
| `url` | one of `url`/`urls` | Single page |
| `urls` | one of `url`/`urls` | Batch fetch, merged and deduped, max 5; one failure does not affect the others |

### pdf_parser

| Param | Req | Notes |
|-------|-----|-------|
| `path` | ✅ | Local PDF path or http(s) URL (`pdf_url` from academic results passes directly) |
| `pages` | | Page ranges (1-based), e.g. `1-10`, `1,3,5-7`; omitted = first 20 pages only, with a continuation hint |

## Task → tool routing

| Task | Use |
|------|-----|
| General web lookup / news / time-sensitive content | `smartsearch` (with `time_range`) |
| Page content instead of URL list | `smartsearch` with `fetch_top_n: 1-5` — no separate `cleanfetch` call needed |
| Find papers / citation-ranked results | `academicsearch` (biomed → `pubmed`+`europepmc`, CS → `arxiv`+`dblp`) |
| Already have a DOI / arXiv id | Pass the id directly as `academicsearch`'s `query` |
| Read full page text / batch read | `cleanfetch` |
| Read a PDF / paper full text | `pdf_parser` |
| Existing SearXNG / LiteLLM toolchain | `/searxng/search` compatible endpoint, zero changes |

## Machine contract

| Endpoint | Notes |
|----------|-------|
| `POST /mcp` | MCP protocol entry (streamable HTTP); `Authorization` header required when `auth_token` is set |
| `GET /searxng/search` | SearXNG-compatible JSON, directly usable by LiteLLM |
| `GET /__admin/health` | `{"ref_count":N,"message":"running"}`, remotely accessible |
| `GET /__admin/status` | Refcount / PID etc., localhost only |
| `GET /__admin/api/providers` · `/api/metrics` | Per-source state machine / Prometheus metrics (read-only) |
| `POST /__admin/refcount` · `/shutdown` | Refcount / shutdown, localhost only |

Results carry source engine and relevance score (`show_meta` controls this); cache expires after 6h; rate limits default 3/s, 60/min. Full API: [api.en.md](api.en.md).

## Error quick diagnosis

| Error / symptom | Fix |
|-----------------|-----|
| Tool missing in the client | Registration condition unmet (above); fix `enabled` config and restart |
| "Some engines failed this run, results may be incomplete" | Expected degradation: some academic engines timed out / blocked; results still usable; overseas engines need a proxy |
| Empty results | `curl /__admin/api/providers` first to see which source failed; in `engine` mode DDG needs a proxy, Google is disabled by default |
| Academic search timeout | Overseas engines skip automatically under `network: china`; `europepmc` / `dblp` / `doaj` are directly reachable for full coverage |
| `cleanfetch` blocked by anti-bot | Explicitly marked on that result; set `jina.api_key` for the Jina Reader fallback |
| pdf_parser reports truncation | Pass a `pages` range to continue reading |

## Common pitfalls

- **Google web engine is disabled by default**: fully hardened JS challenges since 2025-01; UA/TLS spoofing is dead — do not try to enable or "fix" it.
- **DDG / arXiv rate-limit clamps are built in** (DDG 1/s·6/min, arXiv 1/s·12/min + 3s gap) with automatic 429 cooldown — do not loosen them; recalibrate with real tests.
- **No preset config.yaml is auto-generated when `-c` names an explicit path** — a missing file is an error; omit `-c` to get auto-generation.
- **`fetch_top_n` vs `cleanfetch` division**: content alongside a search → former (one call); full text of a known URL → latter.
- **Logs go to stderr and `websearch.log`** (config dir, 1MB rolling default) — never polluting the JSON-RPC on stdio stdout.
