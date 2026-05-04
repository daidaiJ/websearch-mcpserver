# websearch-mcp
----
## 简介
websearch mcp server 一个golang 开发的轻量级web search 服务，支持 baidu、tavily 以及 hybrid（混合）模式。

## 搜索模式
通过配置文件中的 `mode` 字段指定搜索模式：
- `baidu`（默认）：仅使用百度千帆 AI Search API
- `tavily`：仅使用 Tavily Search API
- `hybrid`：并发请求 baidu 和 tavily，按 URL 去重后合并结果

## 配置文件
```yaml
port: 8338  # mcp server端口
mode: hybrid  # 搜索模式: baidu / tavily / hybrid（默认 baidu）
baidu_sk: YOUR-BAIDU-SK  # 百度api sk  此页面获取，注意权限 https://console.bce.baidu.com/iam/#/iam/apikey/list
tavily_sk: YOUR-TAVILY-SK  # tavily api key  https://app.tavily.com/home

# LLM配置（可选，用于摘要生成）
llm:
  base_url: "https://api.openai.com/v1"  # LLM API基础地址，支持环境变量 LLM_BASE_URL
  api_key: "sk-xxx"                     # LLM API密钥，支持环境变量 LLM_API_KEY

# 缓存配置（可选）
cache:
  storage_path: "./data/search_cache.db"  # SQLite数据库路径
  cleanup_interval: 30                    # 清理间隔（分钟，默认30，最大360）

black_list_host:  # 屏蔽站点（仅对 baidu 有效）
  - "baijiahao.baidu.com"
  - "csdn.net" 
  - "csdn.com" 
  - "gitcode.com"
  - "gitcode.net"
  - "baidu.com"
```

> 所有配置均支持环境变量覆盖：
> - `BAIDU_SK` - 覆盖`baidu_sk`
> - `TAVILY_SK` - 覆盖`tavily_sk`
> - `LLM_BASE_URL` - 覆盖`llm.base_url`
> - `LLM_API_KEY` - 覆盖`llm.api_key`
>
> 完整配置样例参见项目根目录下的 `config.example.yaml`。

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

### 引用计数机制

| 操作 | 服务未运行 | 服务运行中（ref=n） |
|------|-----------|-------------------|
| `start` | 启动服务，ref=1 | ref=n+1，返回新计数 |
| `stop` | 提示已停止 | ref=n-1；若 ref=0 则触发优雅退出 |
| `kill` | 提示已停止 | 强制结束进程 |
| `status` | 显示 stopped | 显示 PID、端口、ref 计数 |

优雅退出时会自动完成：停止缓存清理协程 → 关闭 SQLite → HTTP Shutdown（5 秒超时）→ 清理 PID 文件。

### 包装脚本（推荐）

已内置跨平台包装脚本，免去直接操作二进制：

**Windows** (`websearch.bat`)
```batch
websearch.bat start
websearch.bat stop
websearch.bat kill
websearch.bat status
```

**Linux / macOS** (`websearch.sh`)
```bash
chmod +x websearch.sh
./websearch.sh start
./websearch.sh stop
```

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
    & "D:\CODE\go\websearch-mcpserver\websearch.bat" start
}
function Stop-WebSearchMCP {
    & "D:\CODE\go\websearch-mcpserver\websearch.bat" stop
}
# 使用：Start-WebSearchMCP  →  打开 Claude Desktop  →  Stop-WebSearchMCP
```

**Bash / Zsh (Linux/macOS)**
```bash
alias ws-start='~/bin/websearch-mcpserver/websearch.sh start'
alias ws-stop='~/bin/websearch-mcpserver/websearch.sh stop'
# 使用：ws-start && claude  →  结束后执行 ws-stop
```

### 方案 3：Claude Code CLI 封装脚本

如果你使用 `claude code` 命令行，可创建 wrapper 脚本：

```bash
#!/bin/bash
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
"$DIR/websearch.sh" start
sleep 1
claude "$@"
"$DIR/websearch.sh" stop
```

### 方案 4：Windows 快捷方式 / 批处理组合

创建一个启动器脚本 `launch-claude.bat`：

```batch
@echo off
set "WS_DIR=D:\CODE\go\websearch-mcpserver"
call "%WS_DIR%\websearch.bat" start
timeout /t 1 /nobreak >nul
start "" "C:\Users\%USERNAME%\AppData\Local\AnthropicClaude\claude.exe"
echo Claude 已启动，关闭后请手动运行 websearch.bat stop
```

### 方案 5：注册为系统服务（后台常驻）

如果你希望服务始终在后台运行，而不需要 hooks：

- **Windows**：使用 `nssm` 将 `websearch-mcpserver.exe start` 注册为 Windows Service，此时无需 `stop`/`kill` hooks，随系统启停。
- **Linux**：使用 systemd service 文件，设置 `Restart=always`。
- **macOS**：使用 `launchd` plist 文件。

> 提示：若采用后台常驻方案，可忽略引用计数，直接 `start` 一次后保持运行。多客户端同时使用时也不会冲突。

## 高级功能

### LLM摘要生成
当配置了有效的LLM API后，系统会自动为搜索结果生成带引用标注的专业摘要。

### 缓存系统
启用缓存后可大幅减少重复搜索的API调用：
- 基于SQLite的持久化缓存
- 支持查询和意图的双重缓存键
- 自动清理过期记录（默认30分钟）
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
      - "8338:8338"  # 宿主机8080端口映射到容器8080端口
    # 可选：环境变量（如果服务需要）
    environment:
      - TZ=Asia/Shanghai  # 设置容器时区

```
可选docker 直接部署
```shell
docker run -d \
  --name websearch-service \
  --restart unless-stopped \
  -v /your-config-dir/config.yaml:/app/config.yaml \
  -p 8338:8338 \
  -e TZ=Asia/Shanghai \
  websearch:v1
```

## 配置
以claude 为例，任选一种
**命令行**
```shell
claude mcp add  --transport http websearch-mcp   http://localhost:8338/mcp
```
**配置文件**
用户目录下找到`.claude.json` 文件的mcpServers 字段，没有可以自行添加
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
      api_base: http://websearch:8338/searxng # 以searxng 格式提供websearch 服务
```

## 升级说明

### v1.x → v2.x 重大变更
1. 增加了Tavily 搜索api支持
2. 扩展了llm 摘要支持，建议使用快速模型，减少时间延迟的同时降低成本
3. 通过sqlite 对搜索及摘要结果做了缓存管理