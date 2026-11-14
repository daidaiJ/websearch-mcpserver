# API Documentation

[English](api.en.md) | [中文](api.md)

## Table of Contents

- [Go Module API (server package)](#go-module-api-server-package)
- [HTTP API](#http-api)
  - [MCP Endpoint](#mcp-endpoint)
  - [Observability Endpoint](#observability-endpoint)
  - [Admin Endpoints](#admin-endpoints)

---

## Go Module API (server package)

External projects can embed this project as a Go module and manage the MCP service lifecycle through the `server` package.

### Install

```bash
go get websearch/server
```

### Types

```go
// Server encapsulates the MCP service lifecycle management.
type Server struct { ... }
```

### Functions

#### `New`

```go
func New() *Server
```

Creates a new Server instance with internal reference count and shutdown channel initialized.

#### `(*Server) SetRefCount`

```go
func (s *Server) SetRefCount(n int32)
```

Sets the initial reference count, typically `1` on first start.

#### `(*Server) RefCount`

```go
func (s *Server) RefCount() int32
```

Returns the current reference count value.

#### `(*Server) Run`

```go
func (s *Server) Run(conf config.Config, onListening ...func()) error
```

Full startup flow: initializes search engine, MCP routes, Admin routes, cache cleanup goroutine, then listens on the port (default `127.0.0.1:8338`) and blocks until `SIGINT`/`SIGTERM` signal or reference count reaches zero. `onListening` callbacks run only after the listener is bound (e.g. write the PID file); a listen failure (e.g. port in use) returns an error instead of panicking. On exit, performs graceful shutdown (stop cache cleanup → HTTP Shutdown → close WebFetch → close SQLite → clean PID file).

Suitable for CLI or standalone deployment scenarios.

#### `(*Server) Handler`

```go
func (s *Server) Handler(conf config.Config) http.Handler
```

Only initializes components and returns an `http.Handler` with all routes registered, **without starting the HTTP Server**.

Suitable for embedding scenarios — the caller creates its own `http.Server`, reusing existing port, TLS config, or middleware stack.

**Embedding boundary**: initialization is eager; `Run` returns search-engine/cache construction failures as errors, while `Handler` still panics (fail-fast at startup), so embedded callers should validate config first or recover at the boundary and return their own error. Assembly uses package-level singletons and is designed for one instance per process; multiple `Server` values share the same components, so run separate processes for multiple instances. `Run` returns an error for listen failures and does not panic.

### Usage Examples

#### Option 1: Full Hosting (CLI / Standalone)

```go
package main

import (
    "websearch/pkg/config"
    "websearch/server"
)

func main() {
    conf, _ := config.Load("config.yaml")
    srv := server.New()
    srv.SetRefCount(1)
    srv.Run(*conf)
}
```

#### Option 2: Embed in Existing HTTP Server

```go
package main

import (
    "context"
    "net/http"
    "os"
    "os/signal"
    "time"

    "websearch/pkg/config"
    "websearch/server"
)

func main() {
    conf, _ := config.Load("config.yaml")

    srv := server.New()
    mux := http.NewServeMux()
    mux.Handle("/", srv.Handler(*conf))

    // Register custom routes
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("ok"))
    })

    httpSrv := &http.Server{
        Addr:    ":9000",
        Handler: mux,
    }

    go httpSrv.ListenAndServe()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, os.Interrupt)
    <-quit

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    httpSrv.Shutdown(ctx)
}
```

---

## HTTP API

### MCP Endpoint

| Property | Value |
|----------|-------|
| Path | `POST /mcp` |
| Protocol | [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) |
| Content-Type | `application/json` |

MCP clients use this endpoint for protocol handshake, tool listing, and tool invocation. See [MCP Specification](https://modelcontextprotocol.io/) for protocol details.

#### Tool List

| Tool | Description | Parameters |
|------|-------------|------------|
| `smartsearch` | Web search | `query` (one of `query`/`queries`), `queries` (batch, up to 5), `intent` (optional, when LLM is enabled), `time_range` (optional, months; default 3), `depth` (`fast`/`balanced`/`deep`), `sources` (`web`/`academic`), `max_chars` (shared body budget), `fetch_top_n` (optional: omitted → same as before, only engine/provider-provided content (server default 0, configurable), 0 = title+snippet+URL only, 1-5 = obtain page-original text for top N — API engines use the fast path, web engines fetch internally, anti-bot blocks are annotated) |
| `academicsearch` | Academic paper search (arXiv / Crossref / OpenAlex / PubMed / Europe PMC / DBLP / DOAJ, etc.) | `query` (required; a DOI or arXiv id can be passed directly for single-paper lookup, ignoring `time_range`/`page` but respecting enabled sources and requested `engines`), `engines` (optional), `time_range` (optional: `year`/`month`/`week`/`day`), `page` (optional) |
| `cleanfetch` | URL fetch returns Markdown, or reads saved bodies; always registered | `url`/`urls` (fetch, deduplicated up to 5, requires `cleanfetch.enabled=true`); or `result_ref` with `offset` (default 0) and `limit` (default/0 = 4096, maximum 8192); mutually exclusive modes, paging fields only for references |
| `pdf_parser` | PDF parsing and local precise archival | `path` (PDF path/URL or existing local package), `pages` (original pages), `task_ref` (resume, mutually exclusive with other inputs except wait/project), `wait_seconds` (0–1800), `output_dir` (optional local package directory). With a token, local PDFs default to whole-document archival in a same-name `.mineru/` beside the source, bounded per call by the page budget. Reuses saved pages/tasks and persists Markdown, images and original ZIPs. Remote PDFs and text extraction without a token retain the default first 20 pages. |
| `file_search` | Local file quick search over the Everything index (read-only, Windows only) | `query` (required, Everything syntax), `folder` (optional directory scope, Git Bash `/d/code/ai` style accepted), `match_case`/`whole_word`/`match_regex`/`match_diacritics` (optional native filters), `max_results` (default 10, hard cap 20), `page` (page number, default 1), `sort`/`descending`, `exclude` (optional NOT terms), `min_alignment` (optional, overrides the server-side alignment threshold), `time_format` (`datetime`/`iso`/`filetime`) — no enabled switch: registered when the lazy probe of `everything.url` at first MCP client connection succeeds, hidden otherwise; Windows only, not recommended on Linux distributions unless WSL pointing at the Windows host; the `everything.roots` whitelist forcibly scopes searches, and second-stage filtering (alignment re-ranking / noise demotion / threshold) keeps weak matches out of the context |

Local archival uploads selected pages to MinerU even when OCR fallback is off. Its package survives temporary `result_ref` expiry; call again with the PDF/package path to obtain a fresh reading reference. An unwritable package directory fails before upload. Agents can inspect and reuse older Markdown without calling the parser.

#### Response Behavior

Local `pdf_parser.path` values must be visible to the service process. Docker can only read container or mounted paths, not host drive paths returned by `file_search`. Responses identify the actual PDF parser. When a Token and the MinerU remote switch are enabled, both `.pdf` and strict `arxiv.org/pdf/<id>` endpoints qualify. Text extraction uses upstream go-webfetch and may contain joined words, encoding artifacts or missing layout. Check readability for the task; use public HTML, another extractor or MinerU when missing content affects the conclusion.

`pdf_parser` also accepts `wait_seconds` (0-1800, default 300 per new job) and `task_ref` (`standard:<task_id>` / `batch:<batch_id>` / `agent:<task_id>`). Omit `path/pages` when resuming. By default it queries once, returning status for pending jobs and retrieving completed results without resubmission. Timeout retains the last state and known page progress. Polling backs off from 2 to at most 30 seconds. Credentials remain in server configuration.

`cleanfetch` rejects title-only and standard skip-navigation shells. Eligible failures use Jina's content mode; 404/410, security blocks and cancellation do not trigger fallback. Dynamic pages can still fail to yield a body, which is reported explicitly rather than treated as successful content.

All four tools share a unified response contract (full details in [docs/search.en.md](search.en.md#response-behavior--reliability)):

- The response ends with a structured failure list (engine / failure kind `timeout`·`rate_limit`·`challenge`·`off_topic`·`error` / short reason), so empty results are explainable; the list is exempt from any budget or dump trimming, and sparse results (≤3) carry filter diagnostics
- The request context flows through to every engine, so a client disconnect stops the wait; `smartsearch.engine_timeout_sec` (default 10s) is a per-engine soft timeout — a timed-out engine is reported as `timeout` in the failure list while other engines still return results
- Result dates carry a `date_source` provenance state; response headers include `retrieved_at` / `cache_age_seconds` and a fixed `usage_note`
- Oversize search results retain provenance, counts, candidate titles/URLs/sources/short excerpts and `result_ref`; saved webpage/PDF bodies also provide a reference. Use MCP to read the body without client access to container files
- The same endpoint mounts read-only Resources `search://capabilities` and `search://health` (no tool slots, no secrets; `mcp_resources` disables them)

#### Reading Long Results

`cleanfetch` is always registered. `cleanfetch.enabled=false` disables only URL fetching, not saved-body reads. `result_ref` cannot be mixed with `url`/`urls`; URL mode rejects `offset`/`limit`. Existing URL and batch calls remain compatible.

Pass the returned `result_ref` unchanged as `cleanfetch.result_ref`, starting at `offset=0`. Output fields are `ref`, `content`, `offset`, `next_offset`, and `eof`, provided as both JSON text content and structured content. Continue with `next_offset` until `eof=true`. Offsets count Unicode code points, not bytes or lines. Out-of-range offsets, negative values and `limit>8192` are errors.

References are random process-local capabilities, independent of MCP sessions and `project`, subject to endpoint authentication. They grant access only to the referenced service-generated file. Search references last at most 7 days; ordinary fetches use `cleanfetch.file_ttl` (default 24 hours), with expiry in the notice. Restart, cleanup or detected identity/size/modification-time changes yield `result_unavailable`; repeat the original tool call. The output directory requires a trusted host; checks do not defend against writers deliberately preserving metadata. Temporary file notices are not persisted or reused from old caches. Ordinary long search cache hits generate fresh files and references. Successful summaries retain saved-body references. These are not permanent citations, paths, or MinerU `task_ref` values.

Saved enriched content is identified by structured state, not by file-path wording in the body. Web cache keys use `web:v3:`; older entries are not reused and retain their existing TTL. The first lookup may be a cold-cache request; academic caching is unchanged. Summary progress notifications are batched by `llm.summary_progress_interval` (default 1s); each message carries the text accumulated since the last push.

#### Client Config Examples

**Claude CLI**
```bash
claude mcp add --transport http websearch-mcp http://localhost:8338/mcp
```

**Config file** (`.claude.json` / `mcp.json`)
```json
{
  "mcpServers": {
    "websearch-mcp": {
      "type": "http",
      "url": "http://localhost:8338/mcp",
      "timeoutMs": 5000
    }
  }
}
```

---

### Observability Endpoint

| Property | Value |
|------|-----|
| Path | `GET /metrics` |
| Content-Type | `text/plain; version=0.0.4; charset=utf-8` |

Read-only aggregates in Prometheus / OpenMetrics text format. In-process metrics are always available: in-flight HTTP requests, process uptime, goroutine count, heap-allocated bytes and `websearch_telemetry_enabled`. When the dashboard is enabled, today's MCP tool calls (by tool name) and search provider calls (by provider name) are appended with request, success/failure, cache-hit counts and latency buckets; that part comes from the privacy-preserving telemetry store (queries are stored as hashes and keywords only; no URLs, raw text or secrets) and nothing is actively probed.

- In-process metrics do not require the dashboard; telemetry series appear with `dashboard.enabled: true` and are omitted otherwise, with `websearch_telemetry_enabled` set to 0.
- Read-only and independent of the write guard; accepts `GET` / `HEAD` only.
- Example: `curl -s http://127.0.0.1:8338/metrics`

---

### Admin Endpoints

Admin endpoints only allow local access (`127.0.0.1` / `::1` / `localhost`). Remote requests return `403 Forbidden`.

#### `POST /__admin/refcount`

Modify reference count. When count reaches zero, triggers graceful service shutdown.

**Request Body**
```json
{ "delta": 1 }
```

| Field | Type | Description |
|-------|------|-------------|
| `delta` | `int` | Change amount, positive to increase, negative to decrease |

**Response**
```json
{
  "ref_count": 2,
  "message": ""
}
```

When count reaches zero:
```json
{
  "ref_count": 0,
  "message": "refcount reached zero, server will shutdown gracefully"
}
```

---

#### `GET /__admin/status`

Query current reference count.

**Response**
```json
{
  "ref_count": 1
}
```

---

#### `POST /__admin/shutdown`

Request immediate graceful shutdown (ignores reference count).

**Response**
```json
{ "message": "shutdown requested" }
```
