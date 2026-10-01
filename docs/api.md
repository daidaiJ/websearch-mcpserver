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
func (s *Server) Run(conf config.Config, onListening ...func()) error
```

完整启动流程：初始化搜索引擎、MCP 路由、SearXNG 路由、Admin 路由、缓存清理协程，然后监听端口（默认 `127.0.0.1:8338`）并阻塞直到收到 `SIGINT`/`SIGTERM` 信号或引用计数归零。监听成功后才调用 `onListening` 回调（可用于写 PID 文件）；监听失败（如端口占用）返回错误，不 panic。退出时自动执行优雅关闭（停止缓存清理 → HTTP Shutdown → 关闭 WebFetch → 关闭 SQLite → 清理 PID 文件）。

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
| `smartsearch` | 网络检索 | `query`（必填）、`intent`（可选，LLM 启用时可用）、`time_range`（可选，月；默认 3）、`fetch_top_n`（可选：不传与旧版一致只返回自带内容（服务端默认 0 可配），传 0 只要标题摘要和 URL，传 1-5 获取前 N 条页面原文——API 引擎走快速路径、网页引擎内部抓取，反爬拦截会显式标注） |
| `academicsearch` | 学术论文检索（arXiv / Crossref / OpenAlex / PubMed / Europe PMC / DBLP / DOAJ 等） | `query`（必填；也可直接传 DOI 或 arXiv id 走单篇精确查询，忽略 `engines`/`time_range`/`page`）、`engines`（可选）、`time_range`（可选：`year`/`month`/`week`/`day`）、`page`（可选） |
| `cleanfetch` | 网页内容抓取，返回 Markdown | `url`（与 `urls` 至少一者）、`urls`（可选批量，合并去重最多 5 个） — 需配置 `cleanfetch.enabled` |
| `pdf_parser` | PDF 解析，支持 MinerU AI 增强（表格/公式/多栏识别） | `path`（必填，本地路径或远程 URL）、`pages`（可选页码范围，如 1-10；单个区间宽度上限 1000 页） — 需配置 `pdf_parser.enabled`，可选 `mineru_token`；省略 pages 时受 `max_pages`（默认 20）约束。请求页码超出单任务上限（默认 600，`mineru_page_limit` 可调）或 MinerU 拒绝原件页数／URL 时，本地裁切所选页再上传（`mineru_page_batch_size` 可开启自动分批，`mineru_page_budget` 控制单次页数预算；分批进度经 MCP progress notification 实时推送）；精准 API 返回含图片 ZIP 地址，轻量 API 单次上限 20 页 |
| `file_search` | 本地文件快速检索（Everything 索引，只读，Windows 专属） | `query`（必填，Everything 语法）、`folder`（可选目录限定，接受 Git Bash `/d/code/ai` 风格）、`match_case`/`whole_word`/`match_regex`/`match_diacritics`（可选原生过滤）、`max_results`（默认 50，硬上限 200）、`sort`/`descending`、`exclude`（可选 NOT 排除项）、`min_alignment`（可选，覆盖服务端对齐阈值）、`time_format`（`datetime`/`iso`/`filetime`） — 无 enabled 开关：启动探测 `everything.url` 通过即注册，探测不过不暴露；仅 Windows，Linux 发行版不建议启用（除非 WSL 指向 Windows 宿主）；`everything.roots` 白名单强制限定检索范围，二次过滤（对齐重排/噪声降权/阈值）防止弱匹配打爆上下文 |

#### 响应行为

四个工具共用统一响应契约（完整说明见 [docs/search.md](search.md#响应行为与可靠性)）：

- 响应末尾附结构化失败清单（引擎 / 失败类型 `timeout`·`rate_limit`·`challenge`·`off_topic`·`error` / 短原因），空结果可归因，失败清单不受任何预算或落盘裁剪；结果稀疏（≤3 条）时附过滤诊断
- 结果日期带 `date_source` 三态来源注记，响应头含 `retrieved_at` / `cache_age_seconds` 与固定 `usage_note`
- 渲染结果超 `smartsearch.inline_max_chars`（默认 32768）时整体落盘 `fetchdata/search-*.md`，响应内保留溯源头、统计与文件路径
- 同一端点挂载只读 Resource `search://capabilities` 与 `search://health`（不占工具槽位、不带密钥，`mcp_resources` 可关）

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

> **鉴权**：配置了 `auth_token` 时需携带 `Authorization: Bearer <token>` 或 `X-API-Key: <token>` 头，否则返回 401；`q` 为空返回 400；无可用引擎返回 503。

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
