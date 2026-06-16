# websearch-mcpserver

> 轻量级 Web Search MCP Server — 零 API Key 即可运行

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/daidaiJ/websearch-mcpserver)](https://github.com/daidaiJ/websearch-mcpserver/releases)

---

## 简介

用 Go 开发的 MCP 搜索服务，内置百度网页搜索、Bing 通用搜索引擎和 6 个学术搜索引擎。支持 Claude Code、Qwen Code、Cursor 等 MCP 客户端，也可作为 Go 模块嵌入。

## 设计亮点

- **零依赖启动** — `engine` 模式无需任何 API Key，内置百度网页搜索 + Bing 双引擎并发，代理可用时自动加入 Google
- **系统代理自动检测** — 默认读取 Windows 注册表 / 环境变量，Clash 等代理软件开启系统代理后 Google、学术引擎、Jina Reader 自动走代理，无需手动配置
- **智能限流重试** — 所有 HTTP 客户端自动处理 429 限流（读取 `Retry-After` 头等待后重试），arXiv 引擎内置 1 req/s 限流器避免触发限流
- **多引擎并行编排** — 学术搜索并发请求多个引擎，结果按 URL 去重 + 分组归一化排序；hybrid 模式下多引擎原生混合
- **智能回退** — 百度 SK 失败自动回退网页搜索；主引擎失败自动回退 Bing；LLM 摘要失败回退原始结果；cleanfetch 失败回退 Jina Reader；缓存异常跳过缓存
- **增强型网页抓取** — 基于 go-webfetch，无需代理即可抓取网页，内置 SSRF 防护和 WAF 检测，大内容自动存储到临时文件
- **引用计数进程管理** — 多客户端共享同一实例，归零自动优雅退出
- **纯 Go 无 CGO** — SQLite 使用 `modernc.org/sqlite`，单二进制部署

## 特性一览

| 类别 | 能力 |
|------|------|
| **通用搜索** | 百度千帆、百度网页搜索（内置）、Tavily、Bing（内置）、Google（自动检测代理） |
| **学术搜索** | arXiv、Crossref、OpenAlex、PubMed（国内直连）+ Semantic Scholar、Google Scholar（自动检测代理） |
| **MCP 工具** | `smartsearch` 网络检索 · `academicsearch` 论文检索 · `cleanfetch` 网页抓取 · `pdf_parser` PDF 解析 |
| **缓存** | SQLite 自动缓存，6h 过期，后台定时清理 |
| **LLM 摘要** | 可选接入 OpenAI 兼容 API，生成结构化摘要 |
| **站点屏蔽** | 全局 `black_list_host`，自动过滤低质量站点 |
| **全局限流** | `rate_limit` 统一配置所有搜索引擎的请求速率 |
| **SearXNG 兼容** | `/searxng/search` 端点，可对接 LiteLLM |

## 快速开始

```bash
# 1. 下载: https://github.com/daidaiJ/websearch-mcpserver/releases
# 2. 启动（零配置）
./websearch-mcpserver start
# 3. 添加到客户端（见下方「安装部署 > 注册 MCP 客户端」）
```

## 安装部署

### 二进制下载

| 平台 | 文件 |
|------|------|
| Linux x86_64 | `websearch-mcpserver-linux-amd64` |
| Windows x86_64 | `websearch-mcpserver-windows-amd64.exe` |
| macOS Intel | `websearch-mcpserver-darwin-amd64` |
| macOS Apple Silicon | `websearch-mcpserver-darwin-arm64` |

SHA256 校验：每个 Release 附带 `SHA256SUMS.txt`。

Release 页面：https://github.com/daidaiJ/websearch-mcpserver/releases

### Docker

```bash
git clone --depth 1 https://github.com/daidaiJ/websearch-mcpserver.git
cd websearch-mcpserver && docker build -t websearch:v1 .
```

```yaml
# docker-compose.yml
services:
  websearch:
    image: websearch:v1
    restart: always
    volumes:
      - ./config.yaml:/app/config.yaml
    ports:
      - "8338:8338"
```

### 源码构建

```bash
go build -o websearch ./cmd/
# 注入版本号
go build -ldflags="-X main.version=v1.0.0" -o websearch ./cmd/
```

### 注册 MCP 客户端

服务启动后，按需注册到对应客户端：

```bash
# Claude Code
claude mcp add --transport http websearch http://localhost:8338/mcp

# Qwen Code — 在 settings.json 的 mcpServers 中添加：
```

```json
{
  "mcpServers": {
    "websearch": {
      "type": "http",
      "url": "http://localhost:8338/mcp"
    }
  }
}
```

```bash
# Cursor / 其他 MCP 客户端 — type=http, url=http://localhost:8338/mcp
```

### Agent 快速部署

Agent 可按以下步骤自动完成安装：

```bash
# 1. 下载最新二进制
#    Linux:
curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
  | grep "browser_download_url.*linux-amd64\"" \
  | cut -d '"' -f 4 \
  | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
chmod +x /usr/local/bin/websearch-mcpserver

#    macOS (Intel):
# curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
#   | grep "browser_download_url.*darwin-amd64\"" \
#   | cut -d '"' -f 4 \
#   | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
# chmod +x /usr/local/bin/websearch-mcpserver

#    macOS (Apple Silicon):
# curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
#   | grep "browser_download_url.*darwin-arm64\"" \
#   | cut -d '"' -f 4 \
#   | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
# chmod +x /usr/local/bin/websearch-mcpserver

#    Windows (PowerShell):
# $release = Invoke-RestMethod https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest
# $asset = $release.assets | Where-Object { $_.name -match 'windows-amd64' }
# Invoke-WebRequest -Uri $asset.browser_download_url -OutFile "C:\tools\websearch-mcpserver.exe"

# 2. 写入最小配置（零 Key 即可运行）
mkdir -p ~/.config/websearch
cat > ~/.config/websearch/config.yaml << 'EOF'
port: 8338
mode: engine
EOF

# 3. 启动并注册（见上方「注册 MCP 客户端」）
./websearch-mcpserver start
```

Windows 自启动（可选）：下载后执行 `websearch-mcpserver.exe install`。

## 搜索模式

| 模式 | 说明 | 需要 Key |
|------|------|----------|
| `baidu` | 百度千帆 SK（失败自动回退百度网页搜索）；无 SK 时直接用百度网页搜索 | `BAIDU_SK`（可选） |
| `tavily` | Tavily Search API | `TAVILY_SK` |
| `hybrid` | 百度 SK + 百度网页搜索 + Tavily + Bing + Google 并发去重 | 两者（可选） |
| **`engine`** | **百度网页搜索 + Bing**（代理可用时自动加入 Google） | **无需** |

> 所有模式下主引擎失败均自动回退。无 Key 时自动降级为 `engine`。`baidu` 模式无 SK 时使用百度网页搜索（tn=json，无需 API Key）。

## MCP 工具

### `smartsearch` — 通用网络检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词 |
| `intent` | string | ❌ | 搜索意图（仅 LLM 启用时生效） |

### `academicsearch` — 学术论文检索

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | ✅ | 搜索关键词 |
| `engines` | []string | ❌ | 引擎子集：`arxiv` `crossref` `openalex` `pubmed` `semantic_scholar` `google_scholar` |
| `time_range` | string | ❌ | `year` / `month` / `week` / `day` |
| `page` | int | ❌ | 页码，默认 1 |

### `cleanfetch` — 网页内容抓取

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | ✅ | 网页 URL |

需配置 `cleanfetch.enabled: true`。基于 go-webfetch，无需代理；失败时自动回退 Jina Reader（需配置 `jina.api_key`，代理自动检测）。

### `pdf_parser` — 本地 PDF 解析

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | 本地 PDF 文件路径 |

需配置 `pdf_parser.enabled: true`。大文档自动存储到临时文件。

> 工具注册条件：`smartsearch` 需 `bing.enabled=true`；`academicsearch` 需 `academic.enabled=true`；`cleanfetch` 需 `cleanfetch.enabled=true`；`pdf_parser` 需 `pdf_parser.enabled=true`。

## 子命令

| 命令 | 说明 |
|------|------|
| `start` | 启动服务（ref=1 或 ref+1） |
| `stop` | 减少引用（ref-1，归零优雅退出） |
| `kill` | 强制结束（无视引用计数） |
| `status` | 查看状态、端口、引用计数 |
| `version` | 显示版本号（构建时注入，默认 `dev`） |
| `install` | 安装 Windows 开机自启动 |
| `uninstall` | 卸载 Windows 自启动 |

CLI 参数：`-c, --config` 指定配置文件路径。

## 配置

详见 [docs/config.md](docs/config.md) — 完整配置项、默认值、环境变量覆盖。

**最小配置（零 Key 启动）**：

```yaml
port: 8338
mode: engine
```

## 使用指南

### Windows 开机自启动

```bash
./websearch-mcpserver.exe install   # 生成 VBS 脚本 + 启动目录快捷方式
./websearch-mcpserver.exe uninstall # 移除快捷方式
```

使用 COM API（ole32.dll）创建快捷方式，不依赖 PowerShell。

### MCP Hooks 自动启停

推荐通过 Hooks 实现会话自动启停，以 Qwen Code 为例：

```json
{
  "hooks": {
    "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver start", "timeout": 10000 }] }],
    "SessionEnd":   [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver stop",  "timeout": 10000 }] }]
  }
}
```

引用计数确保多会话共享实例，全部关闭后自动退出。

### 后台常驻

| 平台 | 方案 |
|------|------|
| Windows | `nssm` 注册为 Windows Service |
| Linux | systemd `Restart=always` |
| macOS | launchd plist |

常驻模式下 `start` 一次即可，无需 hooks。

#### Linux 开机自启动（systemd）

```bash
# 1. 创建 systemd 服务文件
sudo tee /etc/systemd/system/websearch.service << 'EOF'
[Unit]
Description=WebSearch MCP Server
After=network.target

[Service]
Type=simple
User=你的用户名
ExecStart=/usr/local/bin/websearch-mcpserver start
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 2. 启用并启动服务
sudo systemctl daemon-reload
sudo systemctl enable websearch
sudo systemctl start websearch

# 3. 查看状态
sudo systemctl status websearch
```

#### macOS 开机自启动（launchd）

```bash
# 1. 创建 plist 文件
tee ~/Library/LaunchAgents/com.websearch.server.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.websearch.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/websearch-mcpserver</string>
        <string>start</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>/tmp</string>
</dict>
</plist>
EOF

# 2. 加载并启动
launchctl load ~/Library/LaunchAgents/com.websearch.server.plist
launchctl start com.websearch.server

# 3. 查看状态
launchctl list | grep websearch
```

### 学术搜索建议

- 医学/生物 → `pubmed`；CS/AI → `arxiv` + `semantic_scholar`；全学科 → `crossref` + `openalex`
- 国内保持 `network: china`，海外引擎自动跳过
- Semantic Scholar / Google Scholar 默认禁用，设置 `disable_semantic_scholar: false` / `disable_google_scholar: false` 即可启用，代理自动检测

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

详见 [docs/api.md](docs/api.md)。

## LiteLLM 集成

```yaml
search_tools:
  - search_tool_name: searxng-search
    litellm_params:
      search_provider: searxng
      api_base: http://localhost:8338/searxng
```

## 运维参考

| 项目 | 说明 |
|------|------|
| **健康检查** | `GET /__admin/health` — 返回 `{"ref_count": N, "message": "running"}`，远程可访问 |
| **Admin 端点** | `GET /__admin/status` · `POST /__admin/refcount` · `POST /__admin/shutdown` — 仅本地访问 |
| **PID 文件** | `.websearch.pid`（JSON 格式），位于配置文件目录或可执行文件目录 |
| **日志文件** | `websearch.log`，同目录，按大小滚动（默认 1MB，保留 1 天） |
| **缓存** | SQLite WAL 模式，6h 过期（基于最近命中），30min 定时清理 |

## 排障

| 问题 | 排查 |
|------|------|
| 启动后工具不可用 | 检查 `mode` 和对应 Key；`cleanfetch` 需 `cleanfetch.enabled: true` |
| 学术搜索超时 | 检查 `network` 设置；海外引擎需代理（默认自动检测系统代理） |
| 端口被占用 | `status` 查看是否已运行，或 `kill` 后重启 |
| 缓存结果过旧 | 缓存 6h 自动过期，或删除 `cache.storage_path` 文件重启 |
| Docker 容器立即退出 | 确认挂载了 `config.yaml`，检查日志输出 |
| stop 后进程仍在 | 等待最多 10s；若仍在用 `kill` 强制结束 |
| 搜索无结果或限流 | 检查 `rate_limit` 配置（默认 3/s, 60/min）；Google 等引擎代理不可用时自动跳过 |

## 变更日志

详见 [CHANGELOG.md](CHANGELOG.md)
