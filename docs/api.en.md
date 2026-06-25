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
func (s *Server) Run(conf config.Config)
```

Full startup flow: initializes search engine, MCP routes, SearXNG routes, Admin routes, cache cleanup goroutine, then starts HTTP Server and blocks until `SIGINT`/`SIGTERM` signal or reference count reaches zero. On exit, performs graceful shutdown (stop cache cleanup → close SQLite → HTTP Shutdown → clean PID file).

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
| `smartsearch` | Web search, supports general and academic search | `query` (required), `intent` (optional, effective when LLM enabled), `academic` (optional, boolean) |
| `cleanfetch` | Web content fetch, returns Markdown | `url` (required) — requires `jina.api_key` config |

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
