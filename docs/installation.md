# 安装部署与运维

[English](installation.en.md) | [中文](installation.md)

## 目录

- [安装部署](#安装部署)
  - [二进制下载](#二进制下载)
  - [Docker](#docker)
  - [源码构建](#源码构建)
- [注册 MCP 客户端](#注册-mcp-客户端)
  - [Claude Code](#claude-code)
  - [Qwen Code](#qwen-code)
  - [Cursor / 其他客户端](#cursor--其他客户端)
- [stdio CLI 接入](#stdio-cli-接入)
- [Agent 快速部署](#agent-快速部署)
- [运维](#运维)
  - [子命令](#子命令)
  - [Windows 开机自启动](#windows-开机自启动)
  - [MCP Hooks 自动启停](#mcp-hooks-自动启停)
  - [后台常驻](#后台常驻)
  - [健康检查与 Admin 端点](#健康检查与-admin-端点)
- [排障](#排障)
  - [自启动报 0x800704C7（杀软 / SmartScreen 拦截）](#自启动报-0x800704c7杀软--smartscreen-拦截)

---

## 安装部署

### 二进制下载

从 [Release 页面](https://github.com/daidaiJ/websearch-mcpserver/releases) 下载，每个二进制旁附带对应 `.sha256` 校验文件。

**HTTP daemon**（`start` 后监听 `/mcp`）：

| 平台 | 文件 |
|------|------|
| Linux x86_64 | `websearch-mcpserver-linux-amd64` |
| Windows x86_64 | `websearch-mcpserver-windows-amd64.exe` |
| macOS Intel | `websearch-mcpserver-darwin-amd64` |
| macOS Apple Silicon | `websearch-mcpserver-darwin-arm64` |

> **发布矩阵拆分**：GitHub Release 只提供上表 4 个平台（linux/windows amd64 + darwin amd64/arm64）。Linux ARM64 没有对应二进制，请用下面的 GHCR 镜像。Windows ARM64 不发布。MCP Registry 的 `.mcpb` 与这 4 个平台一致。
>
> **打 tag**（两步，不要一起推）：先推普通版本 tag（如 `v3.4.0`）发 GitHub Release **并同时推送 GHCR 镜像**；**等 Release 产物就绪后再单独打** `-registry` 后缀 tag（如 `v3.4.0-registry`）发布到 MCP Registry（从已有 `v3.4.0` Release 拉二进制封 `.mcpb`），不重复推镜像。后补的 registry tag 钉在同一个 commit。

**stdio CLI**（由 MCP 客户端拉起，无 HTTP 端口）：

| 平台 | 文件 |
|------|------|
| Linux x86_64 | `websearch-mcp-cli-linux-amd64` |
| Windows x86_64 | `websearch-mcp-cli-windows-amd64.exe` |
| macOS Intel | `websearch-mcp-cli-darwin-amd64` |
| macOS Apple Silicon | `websearch-mcp-cli-darwin-arm64` |

### Docker

官方镜像在 GHCR，清单为 **linux/amd64 + linux/arm64**（Apple Silicon / ARM 服务器直接拉镜像即可）：

```bash
docker pull ghcr.io/daidaij/websearch-mcpserver:latest
# 或钉版本：ghcr.io/daidaij/websearch-mcpserver:3.6.0
```

```yaml
# docker-compose.yml
services:
  websearch:
    image: ghcr.io/daidaij/websearch-mcpserver:latest
    restart: always
    volumes:
      - ./config.yaml:/app/config.yaml               # 主配置（必须）
      - ./dashboard.yaml:/app/dashboard.yaml         # 控制中心配置（建议）
      - ./data:/app/data                             # 控制中心数据：遥测库 + 密钥覆盖文件（建议）
    ports:
      - "8338:8338"
```

**端口**：镜像只暴露 8338（`EXPOSE 8338`），MCP（`/mcp`）、SearXNG（`/searxng/search`）与控制台（`/dashboard/`）共用这一个端口。v3.6.0 的控制中心**没有新增端口**，端口映射不用改。容器内用 `network_mode: host` 时注意主机 8338 未被占用。

**卷映射**（`/app` 是镜像工作目录，配置里的相对路径都相对它解析）：

| 宿主机 | 容器 | 作用 | 不挂载的后果 |
|---|---|---|---|
| `./config.yaml` | `/app/config.yaml` | 主配置（必须） | 用镜像内置的 `config.example.yaml`，改配置只能重建镜像 |
| `./dashboard.yaml` | `/app/dashboard.yaml` | 控制中心配置：管理员口令、放行网段、额度、品牌 | 首次 `start` 会在容器内自动生成一份随机口令的 `dashboard.yaml`；容器重建后口令、放行网段、额度、品牌全部丢失 |
| `./data` | `/app/data` | 遥测库 `dashboard.db` + 密钥覆盖文件 `dashboard-secrets.json` | 调用记录、额度用量、WebUI 里写入的 API Key 随容器重建清空 |
| `./cache`（可选） | `/app/cache` | 搜索缓存（`cache.enabled: true` 时） | 缓存随容器重建清空 |
| `./fetchdata`（可选） | `/app/fetchdata` | cleanfetch 落盘的页面正文 | 落盘文件随容器重建清空 |

> 控制中心配置也可以放别处：用环境变量 `WEBSEARCH_DASHBOARD_CONFIG` 或 `dashboard.config_path` 指定路径（详见 [dashboard.md](dashboard.md)）。

**控制台在 Docker 下的访问边界**：写操作（设置 / API Key / 重启 / 清缓存 / 额度）要求请求来源是容器内的 **loopback**，而桥接网络 + 端口发布时来源是 bridge 网关（如 `172.17.0.1`）——**默认 compose 部署下控制台只读**：页面、调用记录与指标都能看，设置类操作一律返回 403。

- 只读：从宿主机浏览器打开控制台，需把 bridge 网段加入放行白名单（默认 bridge 落在 `172.16.0.0/12` 内，自定义网络按实际网段填，尽量收窄）：

  ```yaml
  # dashboard.yaml
  dashboard:
    allowed_networks: ["172.16.0.0/12"]
  ```

- 要写操作（Linux 主机）：改用 `network_mode: host`，容器与主机共用网络栈，主机浏览器访问 `127.0.0.1:8338` 来源即为 loopback，此时 `ports` 映射会被忽略。
- 不改网络也行：直接在宿主机改 `config.yaml` / `dashboard.yaml` 后重启容器——写操作只是便利，文件才是配置来源。

本地构建：

```bash
git clone --depth 1 https://github.com/daidaiJ/websearch-mcpserver.git
cd websearch-mcpserver && docker build -t websearch:v1 .
```

### 源码构建

```bash
go build -o websearch ./cmd/
go build -o websearch-mcp-cli ./cmd/cli
# 注入版本号
go build -ldflags="-X main.version=v1.0.0" -o websearch ./cmd/
go build -ldflags="-X main.version=v1.0.0" -o websearch-mcp-cli ./cmd/cli
```

---

## 注册 MCP 客户端

服务启动后（HTTP daemon 监听 `http://localhost:8338/mcp`），按客户端注册。以下同时给出 **Claude Code** 与 **Qwen Code** 两种配置方式。

> **鉴权**：若配置了 `auth_token`（或环境变量 `WEBSEARCH_TOKEN`），客户端需携带 `Authorization: Bearer <token>` 头访问 `/mcp` 与 `/searxng/search`，否则返回 401。未配置 token 时无需任何头。

### Claude Code

**命令行**：

```bash
claude mcp add --transport http websearch http://localhost:8338/mcp
# 配置了 auth_token 时：
claude mcp add --transport http websearch http://localhost:8338/mcp --header "Authorization: Bearer <token>"
```

**JSON**（`.claude.json` / `mcp.json`）：

```json
{
  "mcpServers": {
    "websearch": {
      "type": "http",
      "url": "http://localhost:8338/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

### Qwen Code

> Qwen Code 的 HTTP 传输字段是 `httpUrl`（不是 Claude Code 的 `type`+`url`），stdio 用 `command`+`args`。

**命令行**（写入用户级 `~/.qwen/settings.json`，`-s project` 可改为项目级）：

```bash
qwen mcp add --transport http websearch http://localhost:8338/mcp
```

**JSON**（`~/.qwen/settings.json` 或项目 `.qwen/settings.json`）：

```jsonc
{
  "mcpServers": {
    "websearch": {
      "httpUrl": "http://localhost:8338/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

> 未配置 `auth_token` 时省略 `headers` 即可。

Qwen Code 常用参数：

| 参数 | 说明 |
|------|------|
| `-s, --scope` | `user`（默认，全局）/ `project`（仅当前项目） |
| `--timeout` | 工具调用超时（毫秒），默认 600000（10 分钟） |
| `--trust` | 信任该服务器，跳过工具调用确认 |
| `-e KEY=value` | 注入环境变量（如 `-e BAIDU_SK=xxx`） |

配置后需重启 Qwen Code（`/exit` 再启动）生效，用 `/mcp` 查看连接状态。

### Cursor / 其他客户端

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

---

## stdio CLI 接入

不启动 HTTP 服务时，下载 `websearch-mcp-cli-*`，由客户端直接拉起进程（默认走 stdin/stdout）。无配置文件时使用 `mode: engine` 内存默认值。

```bash
# 可选：写出示例配置
./websearch-mcp-cli init
# 或指定路径
./websearch-mcp-cli -c ~/.config/websearch/config.yaml init
```

**Claude Code**：

```json
{
  "mcpServers": {
    "websearch": {
      "command": "/path/to/websearch-mcp-cli",
      "args": ["-c", "/path/to/config.yaml"]
    }
  }
}
```

**Qwen Code**：

```bash
qwen mcp add websearch /path/to/websearch-mcp-cli -c /path/to/config.yaml
```

```jsonc
{
  "mcpServers": {
    "websearch": {
      "command": "/path/to/websearch-mcp-cli",
      "args": ["-c", "/path/to/config.yaml"]
    }
  }
}
```

无配置文件时可省略 `args`。日志写到 stderr 与配置目录下的 `websearch.log`，不会污染 stdout 上的 JSON-RPC。

CLI 子命令：无参数 = stdio；`init` 写示例配置；`version` 显示版本。

配置与 HTTP 共用同一 YAML；`port` 等差异见 [configuration.md#stdio-cli-配置说明](configuration.md#stdio-cli-配置说明)。

---

## Agent 快速部署

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
# 也可以跳过本步：首次 start 会在可执行文件目录自动生成一份与 config.example.yaml
# 相同的预设 config.yaml（显式 -c 指定路径时不会自动生成，文件缺失会报错）
mkdir -p ~/.config/websearch
cat > ~/.config/websearch/config.yaml << 'EOF'
port: 8338
mode: engine
EOF

# 3. 启动并注册（见上方「注册 MCP 客户端」）
./websearch-mcpserver start
```

> **「零配置」的含义**：`install` / `websearch-mcp-cli init` / 首次 `start`（未指定 `-c`）会自动生成一份与 `config.example.yaml` 相同的预设 `config.yaml`，之后改端口、Key、模式都改这一份文件。服务**不会**在没有任何配置文件时靠内存隐式默认值运行。

Windows 自启动（可选）：下载后执行 `websearch-mcpserver.exe install`。

---

## 运维

### 子命令

HTTP daemon 二进制 `websearch-mcpserver`：

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

### 健康检查与 Admin 端点

| 项目 | 说明 |
|------|------|
| **健康检查** | `GET /__admin/health` — 返回 `{"ref_count": N, "message": "running"}`，远程可访问 |
| **Admin 端点** | `GET /__admin/status` · `POST /__admin/refcount` · `POST /__admin/shutdown` — 仅本地访问 |
| **PID 文件** | `.websearch.pid`（JSON 格式），位于配置文件目录或可执行文件目录 |
| **日志文件** | `websearch.log`，同目录，按大小滚动（默认 1MB，保留 1 天） |
| **缓存** | SQLite WAL 模式，6h 过期（基于最近命中），30min 定时清理 |

---

## 排障

| 问题 | 排查 |
|------|------|
| 启动后工具不可用 | 检查 `mode` 和对应 Key；`cleanfetch` 需 `cleanfetch.enabled: true` |
| 学术搜索超时 | 检查 `network` 设置；海外引擎需代理（默认自动检测系统代理） |
| 端口被占用 | `status` 查看是否已运行，或 `kill` 后重启 |
| 缓存结果过旧 | 缓存 6h 自动过期，或删除 `cache.storage_path` 文件重启 |
| Docker 容器立即退出 | 确认挂载了 `config.yaml`，检查日志输出 |
| Docker 下控制台设置页 403 | 写操作要求来源为 loopback；桥接网络下控制台只读，见 [Docker](#docker) 小节 |
| stop 后进程仍在 | 等待最多 10s；若仍在用 `kill` 强制结束 |
| 搜索无结果或限流 | 检查 `rate_limit` 配置（默认 3/s, 60/min）；Google 等引擎代理不可用时自动跳过 |

### 自启动报 0x800704C7（杀软 / SmartScreen 拦截）

**现象**：开机自启脚本（autostart.vbs / `install` 子命令生成的计划任务）报错误码 `0x800704C7`，指向脚本里 `WshShell.Run` 启动 exe 的那一行，例如：

```vb
WshShell.Run """D:\Programs\websearch\websearch-mcpserver.exe"" start", 0, False
```

**含义**：`0x800704C7` = `ERROR_OPERATION_ABORTED`（操作已被用户取消）——要启动的程序在启动瞬间被某个弹窗或拦截动作取消了，**脚本本身没有问题**。常见原因按概率排序：

1. **SmartScreen 拦截（Mark of the Web）**：exe 通过微信/QQ/网盘传输或浏览器下载后带了"来自网络"标记，启动时弹 SmartScreen 警告被点"否"或闪掉。解决：右键 exe → 属性 → 勾选底部"**解除锁定**"；`.vbs` 脚本也顺手解除一次。命令行方式：

   ```powershell
   Unblock-File .\websearch-mcpserver.exe
   Unblock-File .utostart.vbs
   ```

2. **杀软拦截**：360/火绒/卡巴等把无签名 exe 拦截或直接终止。查杀软的拦截记录/隔离区，把安装目录加白。

3. **UAC 提权被拒**：exe 属性 → 兼容性选项卡若勾选了"**以管理员身份运行此程序**"，取消勾选；或启动时 UAC 弹窗被点了"否"。

**定位技巧**：绕过自启脚本，在前台直接跑一次即可看到被"取消"的到底是什么弹窗：

```powershell
cd D:\Programs\websearch
.\websearch-mcpserver.exe start
```

> 注意：文件传输损坏一般报"不是有效的 Win32 程序"，而不是 `0x800704C7`。十有八九是第 1 种——exe 没解除锁定，SmartScreen 弹窗在开机自启场景下被闪掉或点了拒绝。
