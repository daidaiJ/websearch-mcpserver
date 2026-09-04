# 架构与设计

[English](architecture.en.md) | [中文](architecture.md)

## 目录

- [整体架构](#整体架构)
- [回退链](#回退链)
- [系统代理自动检测](#系统代理自动检测)
- [缓存](#缓存)
- [进程管理与引用计数](#进程管理与引用计数)
- [安全防护](#安全防护)
- [作为 Go 模块嵌入](#作为-go-模块嵌入)
- [配套扩展：web-researcher](#配套扩展web-researcher)

---

## 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│  MCP 客户端（Claude Code / Qwen Code / Cursor ...）          │
└──────────────────────────┬──────────────────────────────────┘
                           │ MCP (Streamable HTTP / stdio)
┌──────────────────────────▼──────────────────────────────────┐
│  server 包（HTTP daemon / stdio CLI 共用）                   │
│  ├─ /mcp            MCP 端点（smartsearch/academicsearch/   │
│  │                   cleanfetch/pdf_parser）                 │
│  ├─ /searxng/search  SearXNG 兼容端点（对接 LiteLLM）        │
│  └─ /__admin/*       Admin 端点（status/refcount/shutdown）  │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  pkg/search 搜索引擎组（按 mode 构建）                       │
│  ├─ 通用引擎：baidu / baidu_api / bing / ddg / google /     │
│  │            tavily / exa / anysearch                      │
│  ├─ 学术引擎：arxiv / crossref / openalex / pubmed /        │
│  │            europepmc / dblp / doaj / semantic_scholar    │
│  │            / google_scholar                              │
│  └─ 评分管线：RRF 融合 → 词汇对齐 → 域名品质 → Boost →      │
│               阀值过滤 → MMR 多样性重排                      │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  支撑组件                                                   │
│  ├─ pkg/proxy      系统代理自动检测（注册表/环境变量）       │
│  ├─ pkg/cache      SQLite 缓存（6h 过期，30min 清理）        │
│  ├─ pkg/webfetch   增强型网页抓取（TLS 指纹伪装 + 安全防护） │
│  ├─ pkg/mineru     MinerU PDF 解析客户端                     │
│  ├─ pkg/llm        OpenAI 兼容 LLM 客户端（流式摘要）        │
│  └─ pkg/summarizer 摘要生成                                 │
└─────────────────────────────────────────────────────────────┘
```

**关键设计**：

- **单一配置源** — HTTP daemon 与 stdio CLI 共用同一 YAML 配置（`pkg/config`），`mode` 决定搜索引擎组如何构建
- **引擎即接口** — 所有引擎实现统一 `SearchInf` 接口，`pkg/search/factory.go` 按模式组装；`HybridSearchImpl` 负责多引擎并发编排
- **纯 Go 无 CGO** — SQLite 使用 `modernc.org/sqlite`，单二进制部署

---

## 回退链

系统在多个层级内置自动回退，任何单一组件不可用都不影响整体：

| 层级 | 回退逻辑 |
|------|----------|
| 供应商 Key | 同供应商 `sk_list` 多 Key 轮询（重复 Key 自动去重），Key 失效标记 30 分钟冷却后自动恢复 |
| 供应商 | `apipool` 模式：当前供应商所有 SK 耗尽 → 切换下一个供应商（选择策略 round-robin / priority / weighted，weighted 按配置权重 × 可用 SK 数加权随机） |
| 主引擎 | 主引擎失败 → 自动回退 Bing |
| 百度 | 百度 SK 失败 → 回退百度网页搜索 |
| 模式 | 无 Key 时自动降级为 `engine` 模式 |
| LLM 摘要 | 流式摘要失败 → 回退非流式摘要 → 回退原始结果 |
| 网页抓取 | go-webfetch 失败 → 回退 Jina Reader |
| 缓存 | 缓存查询异常 → 跳过缓存直接搜索 |

---

## 系统代理自动检测

默认读取 Windows 注册表（`ProxyEnable` / `ProxyServer`）和环境变量（`HTTP_PROXY` / `HTTPS_PROXY`），Clash、V2RayN 等代理软件开启系统代理后自动生效，无需手动配置 `proxy.enabled` 和重启服务。

- `pkg/proxy/sysproxy.go` — 跨平台系统代理检测（Windows 注册表 + WinHTTP + 环境变量）
- `pkg/proxy/detector.go` — 后台轮询检测器，30s 周期检测代理变更并通知回调
- `pkg/proxy/proxy.go` — `DynamicProxyTransport` 请求级动态代理解析，每次请求实时获取代理端点

**行为**：
- 未设置 `proxy.enabled` → 自动检测系统代理
- `proxy.enabled: true` + `proxy.endpoint` → 显式代理模式
- `proxy.enabled: false` → 显式禁用代理

代理可用时，DuckDuckGo、Semantic Scholar、Google Scholar、Jina Reader 等海外服务自动走代理。

---

## 缓存

- SQLite WAL 模式，`cache.storage_path` 指定存储路径
- 6h 过期（基于最近命中时间），30min 定时清理
- 学术 / 非学术结果按参数区分，防止混用
- `cache.enabled` 显式开关：不设置时按 `storage_path` 是否非空判断（向后兼容）；显式 `false` 强制禁用；显式 `true` 强制启用

---

## 进程管理与引用计数

多客户端共享同一实例，引用计数归零自动优雅退出：

- `start` → ref=1 或 ref+1
- `stop` → ref-1，归零优雅退出
- `kill` → 强制结束（无视引用计数）
- `status` → 查看状态、端口、引用计数

配合 MCP Hooks（SessionStart / SessionEnd）可实现会话自动启停，多会话共享实例，全部关闭后自动退出。

---

## 安全防护

| 防护 | 说明 |
|------|------|
| SSRF 防护 | `cleanfetch` URL 协议校验、内网地址黑名单 |
| DNS rebinding 检测 | 抓取前 DNS 解析目标域名，检查所有 IP 是否为内网/私有地址（与 go-webfetch 的 BlockPrivateIP 双重防护） |
| HEAD 预检 | 抓取前检查 Content-Length，超过 `max_fetch_size_mb`（默认 10MB）直接拒绝 |
| TLS 指纹伪装 | go-webfetch 使用 tls-client Chrome 131 指纹，增强反爬能力 |
| 429 限流重试 | 所有 HTTP 客户端自动处理 429，读取 `Retry-After` 头等待后重试 |
| arXiv 限流保护 | 内置 1 req/s 限流器，避免触发 arXiv 官方限流 |

---

## 作为 Go 模块嵌入

```go
import ("websearch/pkg/config"; "websearch/server")

conf, _ := config.Load("config.yaml")
srv := server.New()
srv.SetRefCount(1)
srv.Run(*conf) // 完整托管

// 或仅获取 Handler 嵌入已有 HTTP Server
handler := srv.Handler(*conf)
```

`server` 包提供两种使用方式：

- **`Run(conf)`** — 完整启动流程：初始化搜索引擎、MCP 路由、SearXNG 路由、Admin 路由、缓存清理协程，启动 HTTP Server 并阻塞直到收到信号或引用计数归零
- **`Handler(conf)`** — 仅初始化组件并返回注册了所有路由的 `http.Handler`，不启动 HTTP Server，适合嵌入已有服务（可复用端口、TLS、中间件栈）

完整 API 文档见 [api.md](api.md)。

---

## 配套扩展：web-researcher

[web-researcher](https://github.com/daidaiJ/web-researcher) 是 websearch-mcpserver 的配套 Qwen Code 扩展，将网络调研任务从主智能体卸载到专用子智能体，**保持主模型上下文窗口干净**。

### 为什么需要

Qwen Code 主模型的上下文窗口非常宝贵。直接让主智能体做网络调研——搜索、抓取网页、筛选信息——大量原始内容会涌入上下文，挤占后续编码对话的空间。

web-researcher 的解法：用一个专用的 fast model 子智能体（默认 `deepseek-v4-flash`）独立完成搜索→筛选→抓取→综合的全流程，生成结构化报告存到本地，只把精炼的摘要返回给主智能体。

### 主要功能

| 功能 | 说明 |
|------|------|
| 🔍 **网络调研卸载** | `/research:search <query>` 一键派发调研任务，子智能体自动拆解子问题、并行搜索、筛选抓取、生成报告 |
| 📊 **报告管理与回溯** | `/research:reports` 列出历史报告 · `/research:read <keyword>` 按关键词检索，分层读取 |
| 📐 **结构化报告** | 报告采用 `FINDING-*`、`DATA-*`、`CONFLICT-*` 锚点标记，主智能体按需 grep 定位，无需加载整份报告 |

### 安装

在 Qwen Code 中执行：

```bash
/extension-creator ~/.qwen/extensions/web-researcher
```

然后将 [web-researcher](https://github.com/daidaiJ/web-researcher) 的内容放入该目录。

或手动克隆：

```bash
git clone https://github.com/daidaiJ/web-researcher.git ~/.qwen/extensions/web-researcher
```

### 使用示例

```
# 一键调研
/research:search MCP protocol specification 2025

# 查看历史报告
/research:reports MCP

# 按需深入阅读
/research:read MCP protocol
```

调研结果存放在项目下 `.qwen/research/` 目录，支持跨会话回溯引用。
