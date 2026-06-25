# API 文档

[English](api.en.md) | [中文](api.md)

## 目录

- [Go Module API（server 包）](#go-module-api-server-包)
- [HTTP API](#http-api)
  - [MCP 端点](#mcp-端点)
  - [SearXNG 兼容端点](#searxng-兼容端点)
  - [Admin 端点](#admin-端点)

---

## Go Module API（server 包）

外部项目可将本项目作为 Go 模块嵌入，通过 `server` 包管理 MCP 服务的生命周期。

### 安装

```bash
go get websearch/server
```

### 类型

```go
// Server 封装了 MCP 服务的生命周期管理。
type Server struct { ... }
```

### 函数

#### `New`

```go
func New() *Server
```

创建一个新的 Server 实例，内部初始化引用计数和关闭通道。

#### `(*Server) SetRefCount`

```go
func (s *Server) SetRefCount(n int32)
```

设置初始引用计数，通常在首次启动时设为 `1`。

#### `(*Server) RefCount`

```go
func (s *Server) RefCount() int32
```

返回当前引用计数值。

#### `(*Server) Run`

```go
func (s *Server) Run(conf config.Config)
```

完整启动流程：初始化搜索引擎、MCP 路由、SearXNG 路由、Admin 路由、缓存清理协程，然后启动 HTTP Server 并阻塞直到收到 `SIGINT`/`SIGTERM` 信号或引用计数归零。退出时自动执行优雅关闭（停止缓存清理 → 关闭 SQLite → HTTP Shutdown → 清理 PID 文件）。

适合 CLI 或独立部署场景。

#### `(*Server) Handler`

```go
func (s *Server) Handler(conf config.Config) http.Handler
```

仅初始化组件并返回注册了所有路由的 `http.Handler`，**不启动 HTTP Server**。

适合嵌入场景——调用方自行创建 `http.Server`，可复用已有端口、TLS 配置或中间件栈。

### 使用示例

#### 方式一：完整托管（CLI / 独立部署）

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

#### 方式二：嵌入已有 HTTP Server

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

    // 注册自己的路由
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

### MCP 端点

| 属性 | 值 |
|------|-----|
| 路径 | `POST /mcp` |
| 协议 | [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) |
| Content-Type | `application/json` |

MCP 客户端通过此端点完成协议握手、工具列表获取和工具调用。具体协议细节参见 [MCP 规范](https://modelcontextprotocol.io/)。

#### 工具列表

| 工具名 | 说明 | 参数 |
|--------|------|------|
| `smartsearch` | 网络检索，支持通用搜索和学术搜索 | `query`（必填）、`intent`（可选，LLM 启用时可用）、`academic`（可选，布尔） |
| `cleanfetch` | 网页内容抓取，返回 Markdown | `url`（必填） — 需配置 `jina.api_key` |

#### 客户端配置示例

**Claude CLI**
```bash
claude mcp add --transport http websearch-mcp http://localhost:8338/mcp
```

**配置文件**（`.claude.json` / `mcp.json`）
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

### SearXNG 兼容端点

| 属性 | 值 |
|------|-----|
| 路径 | `GET /searxng/search` |
| 参数 | `q` — 搜索关键词 |
| Content-Type | `application/json` |

提供与 SearXNG 兼容的搜索接口，可对接 LiteLLM 等框架。

#### 请求示例

```
GET /searxng/search?q=golang+concurrency
```

#### 响应格式

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

#### LiteLLM 配置示例

```yaml
search_tools:
  - search_tool_name: searxng-search
    litellm_params:
      search_provider: searxng
      api_base: http://localhost:8338/searxng
```

---

### Admin 端点

Admin 接口仅允许本地访问（`127.0.0.1` / `::1` / `localhost`），远程请求返回 `403 Forbidden`。

#### `POST /__admin/refcount`

变更引用计数。当计数归零时触发服务优雅关闭。

**请求体**
```json
{ "delta": 1 }
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `delta` | `int` | 变更量，正数增加，负数减少 |

**响应**
```json
{
  "ref_count": 2,
  "message": ""
}
```

计数归零时：
```json
{
  "ref_count": 0,
  "message": "refcount reached zero, server will shutdown gracefully"
}
```

---

#### `GET /__admin/status`

查询当前引用计数。

**响应**
```json
{
  "ref_count": 1
}
```

---

#### `POST /__admin/shutdown`

请求服务立即优雅关闭（无视引用计数）。

**响应**
```json
{ "message": "shutdown requested" }
```
