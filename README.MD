# websearch-mcp
----
## 简介
websearch mcp server 一个 golang 开发的轻量级 web search 服务，支持 baidu、tavily、hybrid（混合）以及 engine（纯引擎）模式。

内置 Bing 通用搜索引擎作为兜底，并集成 arXiv、Crossref、OpenAlex、Semantic Scholar 等学术搜索引擎，支持学术论文检索。

## 搜索模式
通过配置文件中的 `mode` 字段指定搜索模式：
- `baidu`（默认）：使用百度千帆 AI Search API，无 API Key 时自动回退到 engine 模式
- `tavily`：使用 Tavily Search API，无 API Key 时自动回退到 engine 模式
- `hybrid`：并发请求 baidu 和 tavily，按 URL 去重后合并结果
- `engine`：纯引擎模式，**无需任何 API Key**，使用 Bing 通用搜索 + 学术搜索引擎

> 所有模式下，当主搜索引擎请求失败（如 API 额度耗尽）时，会自动回退到 Bing 引擎兜底。

### 引擎与学术搜索

| 引擎 | 类型 | 网络 | API Key | 说明 |
|------|------|------|---------|------|
| Bing | 通用搜索 | 🇨🇳 国内 | 否 | 内置反爬防御，作为兜底引擎 |
| arXiv | 预印本 | 🇨🇳 国内 | 否 | AI/ML 论文主阵地 |
| Crossref | 学术元数据 | 🇨🇳 国内 | 否 | DOI 注册机构，覆盖全学科 |
| OpenAlex | 开放学术图谱 | 🇨🇳 国内 | 否 | 可选 polite pool 邮箱 |
| Semantic Scholar | AI 学术搜索 | 🌍 海外 | 否 | AI 驱动，引用数据丰富 |

学术引擎通过以下方式触发：
- MCP 工具的 `academic` 参数设为 `true`
- 查询或意图中包含学术相关关键词（论文、paper、research、arxiv 等）时自动触发

## 配置文件
```yaml
port: 8338  # mcp server 端口

# 搜索模式: baidu / tavily / hybrid / engine
mode: engine

# 百度 api sk（engine 模式下不需要）
baidu_sk: YOUR-BAIDU-SK
# tavily api key（engine 模式下不需要）
tavily_sk: YOUR-TAVILY-SK

# Bing 引擎配置（兜底引擎 + engine 模式主力引擎，无需 API Key）
# 默认所有引擎开启，通过 disable_* 子字段可单独关闭
bing:
  enabled: true            # 总开关（默认 true）
  academic: true           # 学术引擎总开关（默认 true）
  network: china           # 网络环境: china / international
  blocked: []              # Bing 屏蔽域名
  per_sec: 1               # Bing 每秒限流
  per_min: 20              # Bing 每分钟限流
  # 各引擎独立禁用（默认 false = 启用）
  disable_arxiv: false
  disable_crossref: false
  disable_openalex: false
  disable_semantic_scholar: false

# LLM 配置（可选，用于摘要生成）
llm:
  base_url: "https://api.openai.com/v1"
  api_key: "sk-xxx"
  model_id: "gpt-4o-mini"

# 缓存配置（可选）
cache:
  storage_path: "./data/search_cache.db"
  cleanup_interval: 30

# 屏蔽站点（仅对 baidu 有效）
black_list_host:
  - "csdn.net"
  - "zhihu.com"
```

> 所有配置均支持环境变量覆盖：
> - `BAIDU_SK` - 覆盖 `baidu_sk`
> - `TAVILY_SK` - 覆盖 `tavily_sk`
> - `LLM_BASE_URL` - 覆盖 `llm.base_url`
> - `LLM_API_KEY` - 覆盖 `llm.api_key`
>
> 完整配置样例参见项目根目录下的 `config.example.yaml`。

### 最小配置（零 API Key 启动）

只需以下配置即可运行，无需任何 API Key：
```yaml
port: 8338
mode: engine
```

## 进程管理（本地模式）

本项目支持通过 CLI 子命令对服务进行**引用计数**式的生命周期管理，特别适合 Windows 本地开发或多客户端共享同一服务实例的场景。

### 子命令

```bash
# 启动服务（首次启动，引用计数设为 1）
./websearch-mcpserver start

# 再次 start（引用计数 +1，服务继续运行，适合新开一个 Claude / Cursor 会话）
./websearch-mcpserver start

# 停止服务（引用计数 -1；若计数仍 >0 则服务保持运行；归零时优雅退出）
./websearch-mcpserver stop

# 强制结束进程（无视引用计数）
./websearch-mcpserver kill

# 查看当前运行状态和引用计数
./websearch-mcpserver status
```

### CLI 参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `-c, --config` | 指定配置文件路径（绝对或相对路径） | `./websearch-mcpserver -c /path/to/config.yaml start` |

> 通过 `-c` 指定配置文件后，PID 文件和日志文件会自动写到配置文件所在目录，不再依赖运行时的当前工作目录。

### 引用计数机制

| 操作 | 服务未运行 | 服务运行中（ref=n） |
|------|-----------|-------------------|
| `start` | 启动服务，ref=1 | ref=n+1，返回新计数 |
| `stop` | 提示已停止 | ref=n-1；若 ref=0 则触发优雅退出 |
| `kill` | 提示已停止 | 强制结束进程 |
| `status` | 显示 stopped | 显示 PID、端口、ref 计数 |

优雅退出时会自动完成：停止缓存清理协程 → 关闭 SQLite → HTTP Shutdown（5 秒超时）→ 清理 PID 文件。

## 配置 MCP Hooks（自动启停）

由于本服务采用 **HTTP Transport**，MCP 客户端本身不会自动拉启或停止进程。推荐通过以下方式将 `start` / `stop` 挂接到你的客户端生命周期：

### 方案 1：Qwen Code Session Hooks（推荐）

如果你使用 **Qwen Code** 作为客户端，可通过内置的 `SessionStart` / `SessionEnd` Hooks 实现完全自动化的生命周期管理：会话开始时自动拉起服务，会话结束时自动调用 `stop` 减少引用计数。

项目已内置适配脚本，复制下方配置到你的 **Qwen Code 用户配置文件**（通过 `>settings` 打开）：

**Windows**
```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "D:\\CODE\\go\\websearch-mcpserver\\hooks\\qwen-start.bat",
            "name": "websearch-start",
            "description": "Start websearch-mcp server on session start",
            "timeout": 10000
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "D:\\CODE\\go\\websearch-mcpserver\\hooks\\qwen-stop.bat",
            "name": "websearch-stop",
            "description": "Stop websearch-mcp server on session end",
            "timeout": 10000
          }
        ]
      }
    ]
  }
}
```

**Linux / macOS**
```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/websearch-mcpserver/hooks/qwen-start.sh",
            "name": "websearch-start",
            "description": "Start websearch-mcp server on session start",
            "timeout": 10000
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/websearch-mcpserver/hooks/qwen-stop.sh",
            "name": "websearch-stop",
            "description": "Stop websearch-mcp server on session end",
            "timeout": 10000
          }
        ]
      }
    ]
  }
}
```

> **说明**：
> - `SessionStart` 在新会话建立时触发，执行 `start` 启动服务（若已运行则引用计数 +1）。
> - `SessionEnd` 在会话关闭时触发，执行 `stop` 减少引用计数；当所有会话都关闭后，引用计数归零，服务自动优雅退出。
> - 请根据实际安装路径修改 `command` 字段中的绝对路径。
> - 脚本内部已处理 Qwen Code 通过 `stdin` 传入的 JSON 事件数据，无需额外配置。

#### 其他支持 Hooks 的客户端

类似的自动化启停思路也适用于其他具备 Hooks 机制的 AI 编程客户端，例如：

- **OpenCode**：支持 `pre-tool-use`、`post-tool-use` 以及会话生命周期 hooks，可参考上述结构配置外部命令来调用 `websearch.sh start/stop`。
- **Claude Code**：可通过 `.claude/settings.json` 配置 hooks（如 `on_start`、`on_exit`），同样支持在执行外部命令时传入上下文。若你的 Claude Code 版本支持 `hooks` 字段，可直接复用本项目的 hooks 脚本。

总之，只要客户端支持在「会话开始 / 工具调用前」和「会话结束 / 工具调用后」执行自定义命令，都可以采用相同的引用计数模式来管理 `websearch-mcp` 服务的生命周期。

### 方案 2：Shell Alias / 函数（通用客户端）

对于 Claude Desktop、Cursor 等其他客户端，可在启动前后手动管理：

**PowerShell (Windows)**
```powershell
function Start-WebSearchMCP {
    & "D:\CODE\go\websearch-mcpserver\websearch-mcpserver.exe" start
}
function Stop-WebSearchMCP {
    & "D:\CODE\go\websearch-mcpserver\websearch-mcpserver.exe" stop
}
# 使用：Start-WebSearchMCP  →  打开 Claude Desktop  →  Stop-WebSearchMCP
```

**Bash / Zsh (Linux/macOS)**
```bash
alias ws-start='~/bin/websearch-mcpserver/websearch-mcpserver start'
alias ws-stop='~/bin/websearch-mcpserver/websearch-mcpserver stop'
# 使用：ws-start && claude  →  结束后执行 ws-stop
```

### 方案 3：Claude Code CLI 封装脚本

如果你使用 `claude code` 命令行，可创建 wrapper 脚本：

```bash
#!/bin/bash
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
"$DIR/websearch-mcpserver" start
sleep 1
claude "$@"
"$DIR/websearch-mcpserver" stop
```

### 方案 4：Windows 快捷方式 / 批处理组合

创建一个启动器脚本 `launch-claude.bat`：

```batch
@echo off
set "WS_DIR=D:\CODE\go\websearch-mcpserver"
"%WS_DIR%\websearch-mcpserver.exe" start
timeout /t 1 /nobreak >nul
start "" "C:\Users\%USERNAME%\AppData\Local\AnthropicClaude\claude.exe"
echo Claude 已启动，关闭后请手动运行 websearch-mcpserver.exe stop
```

### 方案 5：注册为系统服务（后台常驻）

如果你希望服务始终在后台运行，而不需要 hooks：

- **Windows**：使用 `nssm` 将 `websearch-mcpserver.exe start` 注册为 Windows Service，此时无需 `stop`/`kill` hooks，随系统启停。
- **Linux**：使用 systemd service 文件，设置 `Restart=always`。
- **macOS**：使用 `launchd` plist 文件。

> 提示：若采用后台常驻方案，可忽略引用计数，直接 `start` 一次后保持运行。多客户端同时使用时也不会冲突。

## 高级功能

### LLM 摘要生成
当配置了有效的 LLM API 后，系统会自动为搜索结果生成带引用标注的专业摘要。摘要会主动过滤低质量内容，合并重复结果，并保留关键原文片段以便后续引用。

### 学术搜索
启用学术引擎后（`bing.academic: true`），可通过 MCP 工具的 `academic` 参数或自动检测学术关键词来触发学术论文检索。学术引擎包括：
- **arXiv**：AI/ML/CS 领域预印本
- **Crossref**：全学科学术元数据（DOI 注册机构）
- **OpenAlex**：开放学术图谱，含引用数据
- **Semantic Scholar**：AI 驱动的学术搜索（需海外网络）

学术搜索结果包含论文元数据（作者、期刊、DOI、引用数、PDF 链接等），与通用网页搜索结果在格式上自动区分。

### 日志系统
日志同时输出到控制台和配置文件同目录下的 `websearch.log` 文件，支持自动滚动：
- **单文件上限**：默认 1MB，超过后自动轮转
- **保留策略**：默认保留 1 天，过期自动清理
- **配置方式**：在 `config.yaml` 中通过 `log.max_size` 和 `log.max_age` 调整（单位分别为 MB 和天）

示例：
```yaml
log:
  max_size: 5  # 单个日志文件最大 5MB
  max_age: 7   # 保留最近 7 天的日志
```

### 缓存系统
启用缓存后可大幅减少重复搜索的 API 调用：
- 基于 SQLite 的持久化缓存
- 支持查询和意图的双重缓存键
- 自动清理过期记录（默认 30 分钟）
- 并发安全设计

## 构建
```shell
git clone --depth 1 --branch master https://github.com/daidaiJ/websearch-mcpserver.git
# 进入项目目录
cd websearch-mcpserver
docker build -t websearch:v1 .
```

## 部署
可选 compose 部署
```yaml
services:
  websearch:
    # 使用上一步构建的 websearch:v1 镜像
    image: websearch:v1
    # 容器名称，方便管理
    container_name: websearch-service
    # 重启策略：容器退出时自动重启（可选，根据需求调整）
    restart: always
    # 挂载外部配置文件到容器内 /app/ 目录
    volumes:
      - /your-config-dir/config.yaml:/app/config.yaml
    ports:
      - "8338:8338"  # 宿主机8338端口映射到容器8338端口
    # 可选：环境变量（如果服务需要）
    environment:
      - TZ=Asia/Shanghai  # 设置容器时区
```
可选 docker 直接部署
```shell
docker run -d \
  --name websearch-service \
  --restart unless-stopped \
  -v /your-config-dir/config.yaml:/app/config.yaml \
  -p 8338:8338 \
  -e TZ=Asia/Shanghai \
  websearch:v1
```

## 配置 MCP 客户端
以 claude 为例，任选一种
**命令行**
```shell
claude mcp add  --transport http websearch-mcp   http://localhost:8338/mcp
```
**配置文件**
用户目录下找到 `.claude.json` 文件的 mcpServers 字段，没有可以自行添加
```json
    "websearch-mcp": {
      "type": "http",
      "url": "http://localhost:8338/mcp",
      "timeoutMs": 5000
    }
```
**删除**  `claude mcp remove websearch-mcp -s user`

## litellm
配置
```yaml
search_tools:
  - search_tool_name: searxng-search
    litellm_params:
      search_provider: searxng
      api_base: http://websearch:8338/searxng # 以 searxng 格式提供 websearch 服务
```

## 升级说明

### v2.x → v3.x 重大变更
1. 新增 `engine` 搜索模式，无需 API Key 即可使用 Bing 通用搜索 + 学术搜索引擎
2. 集成 Bing 通用搜索引擎作为兜底，API Key 额度耗尽时自动回退
3. 集成学术搜索引擎（arXiv、Crossref、OpenAlex、Semantic Scholar），支持论文检索
4. MCP 工具新增 `academic` 参数，可主动请求学术搜索
5. 优化 LLM 摘要提示词：主动过滤低质量内容、合并重复结果、保留关键原文并标注引用

### v1.x → v2.x 重大变更
1. 增加了 Tavily 搜索 api 支持
2. 扩展了 llm 摘要支持，建议使用快速模型，减少时间延迟的同时降低成本
3. 通过 sqlite 对搜索及摘要结果做了缓存管理
