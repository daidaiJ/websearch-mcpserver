# API Documentation

[English](api.en.md) | [中文](api.md)

## Table of Contents

- [Go Module API (server package)](#go-module-api-server-package)
- [HTTP API](#http-api)
  - [MCP Endpoint](#mcp-endpoint)
  - [SearXNG Compatible Endpoint](#searxng-compatible-endpoint)
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

Full startup flow: initializes search engine, MCP routes, SearXNG routes, Admin routes, cache cleanup goroutine, then listens on the port (default `127.0.0.1:8338`) and blocks until `SIGINT`/`SIGTERM` signal or reference count reaches zero. `onListening` callbacks run only after the listener is bound (e.g. write the PID file); a listen failure (e.g. port in use) returns an error instead of panicking. On exit, performs graceful shutdown (stop cache cleanup → HTTP Shutdown → close WebFetch → close SQLite → clean PID file).

Suitable for CLI or standalone deployment scenarios.

#### `(*Server) Handler`

```go
func (s *Server) Handler(conf config.Config) http.Handler
```

Only initializes components and returns an `http.Handler` with all routes registered, **without starting the HTTP Server**.

Suitable for embedding scenarios — the caller creates its own `http.Server`, reusing existing port, TLS config, or middleware stack.

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
| `smartsearch` | Web search | `query` (required), `intent` (optional, when LLM is enabled), `time_range` (optional, months; default 3), `fetch_top_n` (optional: omitted → same as before, only engine/provider-provided content (server default 0, configurable), 0 = title+snippet+URL only, 1-5 = obtain page-original text for top N — API engines use the fast path, web engines fetch internally, anti-bot blocks are annotated) |
| `academicsearch` | Academic paper search (arXiv / Crossref / OpenAlex / PubMed / Europe PMC / DBLP / DOAJ, etc.) | `query` (required; a DOI or arXiv id can be passed directly for single-paper lookup, ignoring `engines`/`time_range`/`page`), `engines` (optional), `time_range` (optional: `year`/`month`/`week`/`day`), `page` (optional) |
| `cleanfetch` | Web content fetch, returns Markdown | `url` (one of `url`/`urls`), `urls` (optional batch, merged & deduped, up to 5) — requires `cleanfetch.enabled` |
| `pdf_parser` | PDF parsing with MinerU AI enhancement (table/formula/multi-column recognition) | `path` (required, local path or remote URL), `pages` (optional page range, e.g. 1-10; a single range is capped at 1000 pages wide) — requires `pdf_parser.enabled`, optional `mineru_token`; when pages omitted, capped by `max_pages` (default 20). When requested pages exceed the per-task limit (default 600, tunable via `mineru_page_limit`) or MinerU rejects the source page count/URL, selected pages are cropped locally and uploaded (`mineru_page_batch_size` enables auto-batching with `mineru_page_budget` capping pages per call; batch progress is streamed via MCP progress notifications); the Standard API returns a ZIP URL with images, while Agent requests allow at most 20 pages |

#### Response Behavior

All four tools share a unified response contract (full details in [docs/search.en.md](search.en.md#response-behavior--reliability)):

- The response ends with a structured failure list (engine / failure kind `timeout`·`rate_limit`·`challenge`·`off_topic`·`error` / short reason), so empty results are explainable; the list is exempt from any budget or dump trimming, and sparse results (≤3) carry filter diagnostics
- Result dates carry a `date_source` provenance state; response headers include `retrieved_at` / `cache_age_seconds` and a fixed `usage_note`
- When the rendered result exceeds `smartsearch.inline_max_chars` (default 32768), the full text is dumped to `fetchdata/search-*.md` and the response keeps the provenance header, counts and file path
- The same endpoint mounts read-only Resources `search://capabilities` and `search://health` (no tool slots, no secrets; `mcp_resources` disables them)

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

### SearXNG Compatible Endpoint

| Property | Value |
|----------|-------|
| Path | `GET /searxng/search` |
| Parameters | `q` — search keyword |
| Content-Type | `application/json` |

Provides a SearXNG-compatible search interface, works with LiteLLM and similar frameworks.

> **Auth**: when `auth_token` is set, requests must carry `Authorization: Bearer <token>` or `X-API-Key: <token>`, otherwise 401; empty `q` returns 400; no engine available returns 503.

#### Request Example

```
GET /searxng/search?q=golang+concurrency
```

#### Response Format

```json
{
  "query": "golang concurrency",
  "results": [
    {
      "title": "...",
      "url": "https://...",
      "content": "..."
    }
  ]
}
```

#### LiteLLM Config Example

```yaml
search_tools:
  - search_tool_name: searxng-search
    litellm_params:
      search_provider: searxng
      api_base: http://localhost:8338/searxng
```

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
