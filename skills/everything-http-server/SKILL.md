---
name: everything-http-server
description: 当需要为 websearch-mcpserver 的 file_search 工具启用并加固 voidtools Everything 的 HTTP Server 时使用。覆盖 1.4 稳定版与 1.5 alpha 的版本识别、插件安装、ini 配置（含运行中实例覆盖配置的经典陷阱）、curl 验证与安全加固清单。Windows 专属；Linux 仅 WSL 场景。
---

# Everything HTTP Server 启用与加固指导

目标：让 Everything 在本机提供只读检索用的 HTTP 接口（`/?s=...&json=1`），供 file_search 工具探测使用。全部步骤可由 agent 通过命令行完成，无需用户碰 GUI。

## 0. 前置判断

- **平台**：仅 Windows。Linux 发行版不建议启用，除非 WSL（url 指向 Windows 宿主，WSL2 镜像网络或 localhost 转发可达时才能探测通过）。
- **版本**：先探测版本，1.4 与 1.5 的启用路径完全不同：

```powershell
# 定位安装目录（注册表 → 运行中进程）
pwsh -NoProfile -Command "(Get-Process Everything -ErrorAction SilentlyContinue | Select-Object -First 1).Path"
pwsh -NoProfile -Command "(Get-Item '<exe 路径>').VersionInfo.ProductVersion"
```

- `1.4.x` → 走 §1（内建 HTTP Server）
- `1.5.0.x`（alpha）→ 走 §2（官方插件，内建入口已移除）
- **Lite 版没有 HTTP Server 功能**，需先换标准版。

## 1. Everything 1.4：内建 HTTP Server

设置存于 `%APPDATA%\Everything\Everything.ini`；若开启了"设置随程序保存"则在安装目录。1.4 默认 `http_server_enabled=0`。

修改前**必须先退出 Everything**：

```powershell
& '<exe 路径>' -exit; Start-Sleep 5
```

> ⚠️ 经典陷阱（实测踩过）：**运行中的实例退出时会用内存里的旧值把 ini 盖回去**。
> 先改后停 = 白改。顺序永远是：退出 → 改 ini → 启动。

修改键值（不存在则追加，文件为 CRLF）：

```ini
http_server_enabled=1
http_server_port=4180
http_server_bindings=127.0.0.1
http_server_username=<用户名>
http_server_password=<强口令>
http_server_allow_file_download=0
```

然后启动 `Everything.exe`，进入 §3 验证。

## 2. Everything 1.5a：官方 HTTP Server 插件

1.5a 把 HTTP Server 从核心移成了独立插件（voidtools/http_server），Everything.ini 里的 `http_server_*` 键**全部无效**——这是从 1.4 文档迁移过来时最容易踩的坑。

### 2.1 安装插件

- 插件 dll 放 `<安装目录>\Plugins\http_server64.dll`（官方 release zip 解压，或运行官方 Setup 让 Everything 自己解包）。
- 设置文件是 `<安装目录>\Plugins.ini`（或 `%APPDATA%\Everything\Plugins.ini`），**不是 Everything.ini，也不是老帖里的 plugins-1.5a.ini**（1.0.5.x 已改写 Plugins.ini）。

### 2.2 配置（同样必须先退出 Everything）

插件优雅退出时会自己把设置回写进 Plugins.ini 的 `[http_server64.dll]` 段。推荐流程：

```powershell
& '<exe 路径>' -exit; Start-Sleep 5
# 若 Plugins.ini 已有该段：sed/Edit 直接改段内键值
# 若没有：追加整段（缺省键读插件默认值）
```

```ini
[http_server64.dll]
enabled=1
port=4180
bindings=127.0.0.1
username=<用户名>
password=<强口令>
allow_file_download=0
```

> ⚠️ 同 1.4 的陷阱：运行中改 ini 会在退出时被内存值覆盖。退出 → 改 → 启动，顺序不可颠倒。
> ⚠️ 1.0.5.x 的 `allow_full_access` 等细粒度权限键实测通过 ini 不生效（疑似账户级能力），
> 需要细粒度权限请在 GUI（选项 → HTTP Server）配置；`allow_file_download=0` 经 ini 可靠生效。

### 2.3 重启即加载

```powershell
Start-Process '<exe 路径>'; Start-Sleep 8
```

## 3. 验证

```bash
# 健康检查：200 + JSON 即成功
curl -s -u <user>:<pass> "http://127.0.0.1:4180/?s=&json=1&count=1" | grep totalResults

# 分类排障：401=凭据不对；连接拒绝=服务/插件没起来（查端口与监听地址）
curl -s -o /dev/null -w "%{http_code}\n" "http://127.0.0.1:4180/"
netstat -ano | grep ":4180" | grep LISTENING
```

响应结构：`{"totalResults":N,"results":[{"type","name","path","size","date_modified"(FILETIME)}]}`。
注意：仅支持 GET（HEAD 返回 400）；JSON 模式不传 `count` 会返回全量；`date_modified` 是
Windows FILETIME（1601 起 100ns），需要转本地时间。带 `Origin` 且非 localhost 的请求被 403。

验证通过后，在 websearch-mcpserver 的 config 里填：

```yaml
everything:
  url: "http://127.0.0.1:4180"
  username: "<用户名>"
  password: "<强口令>"
  roots: ["D:\\CODE\\ai", "D:\\CODE\\pro"]   # 目录白名单，克制推荐
```

重启本服务，日志出现 `Everything HTTP Server 探测通过，file_search 已启用` 即完成；
探测不过时 file_search 不暴露（这是设计行为，不是故障）。

## 4. 加固清单（全部实测可行）

1. **仅绑 loopback**：`bindings=127.0.0.1`，绝不暴露到局域网；
2. **启用 Basic 鉴权**：强口令；同步填到 websearch 配置（该凭据走密钥配置体系，不入库）；
3. **关闭文件下载**：`allow_file_download=0`（1.5a 实测 ini 生效，改后重启；file_search 只需要检索，不需要下载）；
4. **目录白名单**：websearch 侧 `everything.roots` 限定检索范围（如仅代码目录），白名单外的 folder 参数直接报错；
5. **换默认端口**（4180 而非 80）：降低被扫描面 + 避开 80 端口冲突/HTTP.sys 保留；
6. **Everything 常驻**：服务实例 + 客户端实例需同时存活；服务随系统自启（安装时勾选），file_search 可用性取决于此；
7. **及时升级**：1.5 alpha 迭代快，插件与主程序版本需匹配（插件页 https://github.com/voidtools/http_server/releases）。

## 5. 常见故障速查

| 现象 | 原因 |
|------|------|
| 端口不监听，ini 明明改了 | 运行中实例退出时覆盖了 ini（见 §1/§2 陷阱）；或改了 Everything.ini 但 1.5a 用的是 Plugins.ini 插件 |
| 探测 401 | username/password 与服务端不一致（注意区分主配置 ini 与 Plugins.ini 两处） |
| 80 端口 bind failed | 换 4180 等高位端口 |
| 第三方 MCP 插件（非官方）不加载 | 官方插件自动启用，第三方需 GUI 选项 → 插件里手动勾选 Enable，或经安装器 `-setup-plugin` 流程注册 |
| Linux 探测不通 | 属预期：非 Windows 默认不启用，除非 WSL 指向 Windows 宿主 |
