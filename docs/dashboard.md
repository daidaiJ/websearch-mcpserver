# 控制中心（WebUI）配置指南

[English](dashboard.en.md) | 中文

本文件是控制中心（dashboard.yaml / WebUI）的**唯一完整配置参考**，覆盖安装行为、访问边界、安全模型、快捷方式、额度管理、品牌呈现与数据生命周期。主配置（config.yaml）的其它项见 [configuration.md](configuration.md)。

---

## 给 AI Agent 的指引（修改配置前必读）

如果你是 AI agent 在帮人类启用、调整或排障控制中心，**动手前先逐项向人类确认**，不要替人做安全决策：

| 确认项 | 要问的问题 | 默认 |
|---|---|---|
| 是否启用 | 「要开启本机控制中心 WebUI 吗？」 | 全新部署默认启用 |
| 访问范围 | 「只在本机用，还是局域网内其它设备也要看？」「如果放行网段，是哪些 CIDR？」 | 仅本机 loopback |
| 管理员口令 | 「写操作（改设置/Key/重启/清缓存/额度）需要一个本机管理员口令，你想自己设置一个，还是让我生成随机的？」 | 预设文件随机生成 |
| 管理员用户名 | 「要不要配置可选的 admin_username？配置后写操作需要同时提供用户名」 | 不配置（只验证口令） |
| 快捷方式 | 「要在桌面 / 开始菜单创建快捷方式吗？」 | desktop（桌面） |
| 遥测保留 | 「调用明细保留 30 天可以吗？想更短/更长吗？」 | 30 天 |
| 额度管理 | 「需要按周期自动重置用量吗？各供应商上限设多少？」 | monthly，默认 1000 次 |

红线（无需确认、直接遵守）：**admin_password 永不写进主 config.yaml 之外的任何会回显的地方；WebUI 永远无法读取或修改口令；放行非本机网段必须人类明确同意。**

---

## 快速开始

```bash
# 全新部署：首次 start/install 自动生成 dashboard.yaml（默认启用 + 随机口令）
./websearch-mcpserver start

# 已有部署：手动放置配置
cp dashboard.example.yaml dashboard.yaml   # 放在 config.yaml 同目录，按需修改
./websearch-mcpserver open                 # 拉起服务并打开控制台
```

- 控制台地址：`http://127.0.0.1:8338/dashboard/`（主服务端口）
- 关闭：`dashboard.yaml` 改 `enabled: false` 或直接删除该文件 → 重启，运行时零遥测开销
- 回退：控制中心所有状态（数据库、私密覆盖文件、快捷方式）都独立于主配置，删除即回退

## 配置文件与加载顺序

| 层 | 说明 |
|---|---|
| 主 config.yaml 的 `dashboard:` 块 | 可选；老配置没有该块 = 控制中心关闭 |
| `dashboard.yaml`（独立配置） | 推荐唯一入口：按字段覆盖主配置的 dashboard 块 |
| 环境变量 `WEBSEARCH_DASHBOARD_CONFIG` | 指定 dashboard.yaml 的其它路径 |

- 文件损坏时警告并忽略，不影响主服务
- 优先级：dashboard.yaml > 主配置 dashboard 块 > 内置默认值

## 完整配置参考（dashboard.yaml）

```yaml
dashboard:
  enabled: true                 # 总开关；false = 控制台与遥测全部关闭（零开销）
  shortcut: desktop             # 快捷方式落位（Windows）：desktop / start / both / off
  storage_path: "./data/dashboard.db"      # 遥测 SQLite 数据库
  retention_days: 30            # 调用明细保留天数；每日汇总长期保留
  secrets_path: "./data/dashboard-secrets.json"  # API Key 私密覆盖文件（接口永不回显）
  config_path: ""               # dashboard.yaml 自身路径；空 = 主配置同目录

  # —— 访问边界（只读）——
  allowed_networks: []          # CIDR/裸 IP 白名单；空 = 仅本机 loopback
  # allowed_networks: ["192.168.1.0/24", "172.16.0.0/12"]
  # 非法条目 = 整体回退仅本机（fail-closed）。写操作永远仅限本机。

  # —— 管理员口令（写操作闸门）——
  admin_password: ""            # 明文；或用下面 SHA-256 形态，二选一
  admin_password_sha256: ""     # sha256(明文) 的小写十六进制
  admin_username: ""            # 可选；配置后写请求还需携带匹配的 X-Admin-User，
                                # 未配置 = 只验证口令（与上游行为一致）
  # 口令与用户名只能在 dashboard.yaml 手改，WebUI 无法读取或修改；
  # 未配置口令 = 所有写端点整体禁用（安全的默认）。

  # —— 熔断展示（只报告，不干预调用）——
  suspension:
    ban_time_on_fail: "5s"
    max_ban_time_on_fail: "24h"

  # —— 额度管理 ——
  quotas:
    reset: monthly              # monthly / weekly / daily / none（仅手动重置）
    reset_day: 1                # monthly 的每月重置日（1-28）
    limits:                     # 每供应商单 Key 上限（次/周期）；多把 Key 时展示上限自动乘以 Key 数
      tavily: 1000
      exa: 1000
      anysearch: 1000
      doubao: 1000

  # —— 品牌与呈现 ——
  brand:
    title: ""                   # 侧栏/标签页标题；空 = "WebSearch 控制中心"
    logo: ""                    # http(s) URL 或本地图片路径；空 = 内置 W+放大镜
    theme: ""                   # green（默认，绿色办公）/ blue（蓝白科技）/ mono（黑白灰度·立体）
    accent: ""                  # 自定义主色 #RRGGBB，覆盖主题预设
    icon: ""                    # 自定义快捷方式图标（.ico 路径）；空 = 主题内置图标
    footer: true                # 侧边栏「关于」项目介绍；false = 隐藏
```

### 各键说明

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `enabled` | bool | `false`（主配置）/ 预设 `true` | 总开关。关闭时无遥测、无路由、零开销 |
| `shortcut` | string | `desktop` | 快捷方式落位，详见下节 |
| `storage_path` | path | `./data/dashboard.db` | 遥测库；相对主配置目录 |
| `retention_days` | int | `30` | 明细滚动清理周期（每 6 小时执行）；日聚合不受影响 |
| `secrets_path` | path | `./data/dashboard-secrets.json` | WebUI 写入的 Key 存这里，接口只返回「已配置」状态 |
| `config_path` | path | 空 | dashboard.yaml 的自定义位置（也可用环境变量） |
| `allowed_networks` | []string | `[]` | 只读放行网段；fail-closed |
| `admin_password` | string | 空 | 写操作口令（明文形态） |
| `admin_password_sha256` | string | 空 | 写操作口令（SHA-256 形态，更推荐） |
| `admin_username` | string | 空 | 可选管理员用户名；配置后写请求必须携带匹配的 `X-Admin-User` 头（常量时间比较），未配置 = 不做用户名检查。与口令一样只住 dashboard.yaml，WebUI 无法读取或修改 |
| `suspension.*` | 见左 | `5s` / `24h` | 熔断阈值展示口径，与搜索行为无关 |
| `quotas.reset` | enum | `monthly` | 自动重置周期；`none` 表示只手动重置 |
| `quotas.reset_day` | int | `1` | monthly 的重置日（1-28） |
| `quotas.limits` | map | 单 Key 1000 | 供应商 → 单 Key 上限（次/周期）；多 Key 自动乘以 Key 数；**仅用于展示与剩余量计算，不会阻断调用** |

## 额度管理（`quotas`）

已落地：

- **本地真实调用计数**：用量 = 遥测中该供应商本周期内的真实成功调用数（非本地估算、非主动探测），设置页「额度管理」面板按供应商展示 已用 / 上限 / 剩余
- **上限由用户自设，按 Key 数自动换算**：`quotas.limits` 按供应商配置**单 Key** 每周期上限（次），未配置的用默认 1000；实际展示上限 = 单 Key 上限 × 已配置 Key 数（与 apipool 按可用 Key 数累加的实际消耗容量对齐，无需手工换算）；上限只影响展示与剩余量计算，不阻断调用
- **自动周期重置**：`monthly` / `weekly` / `daily` / `none`（`reset_day` 定月内重置日），进入新周期自动归零
- **管理员手动操作**（写闸门保护）：「重置」把某供应商（或全部）的统计窗口推到现在重新累计；「修正」把展示用量改为指定值（换算为修正量，真实调用记录不动）
- **Tavily 官方用量优先 + 启动自动同步**：Tavily 是唯一有公开用量端点的供应商；服务启动即后台预热官方额度（只读、不产生计费调用），控制台首次打开即有数据，查询失败静默、访问面板时按需重试。官方数字可用时优先展示（credits 口径），失败回退本地计数

**为什么其他供应商只有本地计数（2026-09 实测调研）**：Exa / AnySearch / 博查（豆包搜索）/ 百度千帆均未提供「用搜索 API Key 直查额度」的官方端点——Exa 的 usage 接口需要 Service Key 且只返回花费金额；博查与千帆的用量查询走各自云账号的签名体系（非搜索 Key）。这些供应商的额度请到各自控制台查看，本项目用 `quotas.limits` 自设上限 + 本地真实调用计数对照展示。

**计划中（TODO，当前版本没有，勿误解）**：

- **超限阻断/降级**：`quotas.limits` 目前**只做展示与剩余量计算**，到达上限后调用照常进行、不会自动停止或切换供应商。需要「超限即熔断」的话请通过外部配额管理（供应商控制台）或等待后续版本。

| `brand.title` | string | 内置 | 控制台标题（也用于浏览器标签页） |
| `brand.logo` | string | 内置 | URL 直接引用；本地路径经 `/__admin/api/brand/logo` 提供（同一访问边界） |
| `brand.theme` | enum | `green` | `green` / `blue` / `mono`；同时决定快捷方式图标配色 |
| `brand.accent` | color | 空 | `#RGB`/`#RRGGBB`；设置后覆盖主题主色 |
| `brand.icon` | path | 空 | 自定义 .ico；每次启动轻量检查，变化即重建快捷方式 |
| `brand.footer` | bool | `true` | 侧边栏底部「关于」项目介绍（GitHub 链接）；`false` 整块隐藏 |

## 快捷方式落位（`shortcut`）

| 值 | 行为 |
|---|---|
| `desktop`（默认） | 桌面创建 `WebSearchMCP.lnk`，双击 = 拉起服务 + 打开控制台（lazy 启动） |
| `start` | 开始菜单「Programs」创建同名快捷方式：开始屏幕可直接搜索到，也可手动「固定到开始屏幕」。程序化直接 pin 在 Win10/11 上不可靠，故以开始菜单条目为准 |
| `both` | 桌面 + 开始菜单 |
| `off` | 不创建任何快捷方式（已创建的不会被自动删除，可手动删或 uninstall） |

细节：

- 仅 Windows 生效；`start`/`open` 时自动创建，`install` 也会按此配置落位
- 图标跟随 `brand.theme`（或 `brand.icon` 自定义）；每次启动只做轻量检查（读 marker 文件比对），主题/路径/落位变化时才重建
- 更换 `shortcut` 值会自动清理旧位置的 `.lnk`，不留死链
- `uninstall` 始终清理全部已知位置（桌面 + 开始菜单 + 开机自启目录）

## 安全模型

| 操作 | 边界 |
|---|---|
| 读取（页面 + 只读 API） | 本机 loopback 始终可用；`allowed_networks` 放行的网段只读 |
| 写操作（设置/密钥/重启/清缓存/额度管理） | 仅限本机 loopback **且** 请求头带正确 `X-Admin-Password`（常量时间比较，支持明文/SHA-256）；若配置了 `admin_username`，还需匹配的 `X-Admin-User` 头 |
| 管理员口令 | 只存在于 dashboard.yaml；WebUI 与 API 永不返回，也无法修改 |
| 未配置口令 | 所有写端点整体禁用（安全的默认） |
| 查询隐私 | 遥测只存脱敏元数据（哈希/主题/语言/关键词/耗时），完整查询与 URL 不落库 |

> **Docker 部署**：桥接网络 + 端口发布时，请求来源是 bridge 网关而不是 loopback —— 写操作会被拒绝，控制台只读（页面与指标正常）。只读访问要从宿主机浏览器打开，需把网段加入 `allowed_networks`；要写操作请在 Linux 主机上用 `network_mode: host`，或直接在宿主机改配置文件。卷映射（`dashboard.yaml`、`data/`）见 [installation.md#docker](installation.md#docker)。

## 数据与生命周期

- 遥测明细按 `retention_days` 每 6 小时滚动清理；每日聚合长期保留（体积极小）
- `dashboard.db` 建表自动迁移（含 quota_state/client 列），老库升级零操作；回退旧版本时新表/新列被忽略
- 完整回退：停服务 → 删 `dashboard.yaml`（可选：`data/dashboard.db`、`dashboard-secrets.json`、快捷方式）→ 完成；主 config.yaml 全程零改动

## 环境变量

| 变量 | 作用 |
|---|---|
| `WEBSEARCH_DASHBOARD_CONFIG` | 指定 dashboard.yaml 路径（优先于 config_path） |
