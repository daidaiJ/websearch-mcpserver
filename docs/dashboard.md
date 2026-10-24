# 控制中心（WebUI）配置指南

[English](dashboard.en.md) | 中文

本文件是控制中心（dashboard.yaml / WebUI）的**唯一完整配置参考**，覆盖源管理、工具运行态、安装行为、访问边界、安全模型、快捷方式、额度管理、品牌呈现与数据生命周期。主配置（config.yaml）的其它项见 [configuration.md](configuration.md)。本文说明配置与操作机制，不代表某个部署已经上线或完成验收。

---

## 给 AI Agent 的指引（修改配置前必读）

如果你是 AI agent 在帮人类启用、调整或排障控制中心，**动手前先逐项向人类确认**，不要替人做安全决策：

| 确认项 | 要问的问题 | 默认 |
|---|---|---|
| 是否启用 | 「要开启本机控制中心 WebUI 吗？」 | 全新部署默认启用 |
| 访问范围 | 「只在本机用，还是局域网内其它设备也要看？」「如果放行网段，是哪些 CIDR？」 | 仅本机 loopback |
| 写入来源 | 「是否需要放行非 loopback 的写入来源？Docker 宿主请求在服务端实测是什么 IP？」 | `write_allowed_networks: []`；读白名单不授权写入 |
| 管理员口令 | 「写操作（改设置/Key/重启/清缓存/额度）需要一个本机管理员口令，你想自己设置一个，还是让我生成随机的？」 | 预设文件随机生成 |
| 管理员用户名 | 「要不要配置可选的 admin_username？配置后写操作需要同时提供用户名」 | 不配置（只验证口令） |
| 快捷方式 | 「要在桌面 / 开始菜单创建快捷方式吗？」 | desktop（桌面） |
| 遥测保留 | 「调用明细保留 30 天可以吗？想更短/更长吗？」 | 30 天 |
| 额度管理 | 「需要按周期自动重置用量吗？各供应商上限设多少？」 | monthly，默认 1000 次 |

红线（无需确认、直接遵守）：**管理员口令只在 dashboard.yaml 管理，不写入任何回显内容；WebUI 永远无法读取或修改口令；放行非本机读/写来源必须人类明确同意。**

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
- 关闭控制中心不会撤销 WebUI 已保存至主 config.yaml 的源开关与设置；这些设置须按备份恢复流程回退，不要删除数据来替代配置回滚

## 控制台页面一览

![控制中心总览](images/dashboard-overview.jpg)

| 页面 | 能看到什么 |
|------|------------|
| **总览** | KPI（成功调用含总数与缓存命中 / 失败 / 平均耗时）、系统状态（含熔断中数量）、运行配置、工具模块的观测状态、客户端用量归组 |
| **搜索源** | 每个来源的健康、最近 20 次结果、失败构成（如 `解析失败 ×4`）、成功率、平均 / P95 延迟、额度、最近错误与熔断倒计时 |
| **调用记录** | 工具与来源分列；可按层级 / 状态 / 工具 / 来源 / 错误类型筛选；工具行可点 request id 展开这次调用的来源链（原独立「事件」页已并入此页） |
| **设置** | 备份当前 YAML 后写入白名单配置（模式、超时、阈值、熔断时长等），密钥永不回显；外观主题与高级操作合并为「外观与维护」卡片 |

行为边界：

- **被动观测**：只记录真实 MCP 调用产生的元数据，不主动探测、不伪造数据；未调用过的工具也显示「可调用 · 尚无观测」。
- **失败分类 + 置信度**：失败归类为限流 / 验证码 / 访问被拒 / 超时 / 网络 / 解析 / 无结果；样本少于 5 次标记「样本不足」，连续 3 次失败才显示熔断，**只报告不干预调用**。
- **请求级调用链**：一次工具调用与其触发的来源事件共享 request id，回答「这次结果是谁给的、谁失败了」。

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

  # —— 独立读/写访问边界 ——
  allowed_networks: []          # CIDR/裸 IP 白名单；空 = 仅本机 loopback
  write_allowed_networks: []    # 独立写白名单；空 = 仅 loopback，仍需管理员口令
  # 每份白名单任一条目非法 = 该份整体回退 loopback（fail-closed）。
  # Docker 例外只放行服务端实测网关单地址 /32，参见安全模型。

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
      yuanbao: 2000             # 对齐每月免费额度；实际拦截以 yuanbao.monthly_limit 为准

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
| `client_projects` | map<string,string> | `{}` | 精确客户端名到项目简称的显式映射；仅用于缺少项目头时的统计归属 |
| `secrets_path` | path | `./data/dashboard-secrets.json` | WebUI 写入的 Key 存这里，接口只返回「已配置」状态 |
| `config_path` | path | 空 | dashboard.yaml 的自定义位置（也可用环境变量） |
| `allowed_networks` | []string | `[]` | 只读放行网段；fail-closed |
| `write_allowed_networks` | []string | `[]` | 独立写白名单；空/非法 = 仅 loopback；口令校验不变，读白名单不授予写权限；overlay 显式 `[]` 可撤销主配置写白名单 |
| `admin_password` | string | 空 | 写操作口令（明文形态） |
| `admin_password_sha256` | string | 空 | 写操作口令（SHA-256 形态，更推荐） |
| `admin_username` | string | 空 | 可选管理员用户名；配置后写请求必须携带匹配的 `X-Admin-User` 头（常量时间比较），未配置 = 不做用户名检查。与口令一样只住 dashboard.yaml，WebUI 无法读取或修改 |
| `suspension.*` | 见左 | `5s` / `24h` | 熔断阈值展示口径，与搜索行为无关 |
| `quotas.reset` | enum | `monthly` | 自动重置周期；`none` 表示只手动重置 |
| `quotas.reset_day` | int | `1` | monthly 的重置日（1-28） |
| `quotas.limits` | map | 单 Key 1000 | 供应商 → 单 Key 上限（次/周期）；多 Key 自动乘以 Key 数；**仅用于展示与剩余量计算，不会阻断调用** |

## 额度管理（`quotas`）

当前机制：

- **本地真实调用计数**：用量 = 遥测中该供应商本周期内的真实成功调用数（非本地估算、非主动探测），设置页「额度管理」面板按供应商展示 已用 / 上限 / 剩余
- **上限由用户自设，按 Key 数自动换算**：`quotas.limits` 按供应商配置**单 Key** 每周期上限（次），未配置的用默认 1000；实际展示上限 = 单 Key 上限 × 已配置 Key 数（与 apipool 按可用 Key 数累加的实际消耗容量对齐，无需手工换算）；上限只影响展示与剩余量计算，不阻断调用
- **自动周期重置**：`monthly` / `weekly` / `daily` / `none`（`reset_day` 定月内重置日），进入新周期自动归零
- **管理员手动操作**（写闸门保护）：「重置」把某供应商（或全部）的统计窗口推到现在重新累计；「修正」把展示用量改为指定值（换算为修正量，真实调用记录不动）
- **Tavily / TinyFish 官方用量优先 + 启动自动同步**：Tavily 与 TinyFish 是有公开用量端点的供应商——TinyFish 官方 `/usage` 端点（api.search / api.fetch，X-API-Key）返回分页调用记录，本服务取当日（00:00 UTC 起）记录数折算已用量，免费档上限按官方文档取值（搜索 12000 次/天、fetch 1000 URL/天）；服务启动即后台预热官方额度（只读、不产生计费调用），控制台首次打开即有数据，查询失败静默、访问面板时按需重试。官方数字可用时优先展示（Tavily 为 credits 口径、TinyFish 为当日 calls 口径），失败回退本地计数

**为什么其他供应商只有本地计数（2026-09 实测调研）**：Exa / AnySearch / 博查（豆包搜索）/ 百度千帆 / 元宝 / Keenable 均未提供「用搜索 API Key 直查额度」的官方端点——Exa 的 usage 接口需要 Service Key 且只返回花费金额；博查、千帆与元宝的用量查询走各自云账号的签名体系（非搜索 Key）。这些供应商的额度请到各自控制台查看，本项目用 `quotas.limits` 自设上限 + 本地真实调用计数对照展示。元宝另由 `yuanbao.monthly_limit` 在发请求前硬拦截，面板数字仅作展示。

**计划中（TODO，当前版本没有，勿误解）**：

- **超限阻断/降级**：`quotas.limits` 目前**只做展示与剩余量计算**，到达上限后调用照常进行、不会自动停止或切换供应商。需要「超限即熔断」的话请通过外部配额管理（供应商控制台）或等待后续版本。

| `brand.title` | string | 内置 | 控制台标题（也用于浏览器标签页） |
| `brand.logo` | string | 内置 | URL 直接引用；本地路径经 `/__admin/api/brand/logo` 提供（同一访问边界） |
| `brand.theme` | enum | `green` | `green` / `blue` / `mono`；同时决定快捷方式图标配色 |
| `brand.accent` | color | 空 | `#RGB`/`#RRGGBB`；设置后覆盖主题主色 |
| `brand.icon` | path | 空 | 自定义 .ico；每次启动轻量检查，变化即重建快捷方式 |
| `brand.footer` | bool | `true` | 侧边栏底部「关于」项目介绍（GitHub 链接）；`false` 整块隐藏 |

## 调用追踪

侧栏仅保留调用记录，默认显示最近 100 次工具调用。每次调用一行，展开按钮显示请求编号、脱敏元数据及完整已保留的来源尝试；历史记录没有请求编号时不推测关联。原来的 #events 链接兼容转到调用记录。

工具、来源、状态、错误类型可以组合筛选。状态表示工具最终结局；来源和错误类型按同一请求的来源证据筛选，局部来源失败不把成功的工具调用改为失败。CSV 导出当前工具列表和这些请求的完整来源明细，读取失败时中止而不输出不完整文件。

GET /__admin/api/calls 缺省最多 100 条工具记录，接受 tool、provider、status、error_kind 和 limit；原 /events 契约保留兼容。GET /__admin/api/request-events?request_id= 按精确非空请求编号读取完整保留明细，不受列表条数或其它筛选限制。数据仍受实际保留期限制，接口沿用现有读取授权和脱敏字段，不返回查询全文或密钥。

DBLP 返回已识别的 Anubis 挑战页时，工具和来源事件记为人机验证，不当作正常空检索。没有可用结果且存在引擎失败时，保留逐源原因；历史解析失败记录不会被重写。此识别不绕过上游验证，也不自动改变来源开关或实际冷却。

### 外部观测接入（Prometheus）

`GET /metrics` 由主服务提供（不依赖控制台访问边界），输出今日工具/供应商请求数、成功失败、缓存命中与延迟分桶的 Prometheus 文本，数据来自同一遥测库且已脱敏。控制台关闭（`dashboard.enabled: false`）时该端点返回 503。端点只读、只接受 GET/HEAD，不暴露查询原文、URL 或密钥；契约见 [docs/api.md](api.md#观测端点)。

## 源管理与保存

设置提供 `hybrid`、`engine`、`apipool` 和“单供应商”四个模式入口；单供应商下再选 `baidu`、`tavily`、`exa`、`anysearch`、`doubao` 或 `yuanbao`。保存仍沿用既有 `mode` 值，旧配置无需迁移；`mcphub` 目前通过 `config.yaml` 设置，设置页暂未提供入口。“单供应商”表示优先使用所选供应商，不禁用既有回退。模式及所选供应商的信息标注支持悬停、键盘聚焦或点击；说明依据 [v3.7.0 搜索模式文档](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/docs/search.md#搜索模式)，并明确本地来源开关约束。API 调度池受其调度策略与权重影响。高级“学术搜索网络环境”只对应独立学术适配区域，不是豆包或全局网页地域筛选。桌面设置页左右两栏各按内容高度排列，不强制等高：左栏常用设置、外观与维护，右栏 API Key 状态、额度管理；窄屏按单列排列。“抓取自动走代理”（`cleanfetch.auto_proxy`，默认开）独立控制原生 cleanfetch/PDF 是否跟随全局代理，与 API 供应商的 `proxy.api_providers` 互不影响。

“搜索源 → API 供应商 → 豆包”的接口版本选择管理 `doubao.version`（Global / Custom），独立于检索模式。复用共享草稿、差异预览、保存重启和备份恢复；环境覆盖时锁定，变更清理搜索缓存，不变更 Key。页面不提供 ICP 过滤编辑，不重置已有 `doubao.icp_host_only` 配置。

源列表分为网页/API 与学术源，显示启用开关和被动健康观测。开关采用本地嵌入的 [Bootstrap 5.3.8 Switch](https://getbootstrap.com/docs/5.3/forms/checks-radios/#switches) HTML/CSS 子集，无 CDN、脚本或整库依赖；适配桌面鼠标操作，不额外展示运行/条件列。已开启但缺 Key、未被当前模式选用或未满足运行条件的源不等于已参与；尚无调用也不等于异常。关闭源会保留其历史记录与 Key。五 API 独立开关和 AnySearch 匿名档约束、百度网页独立开关及精确查询规则见 [搜索源与模式路由](configuration.md#搜索源与模式路由)。

1. 修改源开关或常用设置只生成浏览器内共享草稿；页面轮询和切换标签不会立即写配置。撤销只丢弃未保存草稿，不撤销已有运行配置。
2. 「保存并重启」先向服务端预览/校验，再显示字段的前后差异供确认。当前模式必须保留至少一个可参与网页源；学术工具开启时也须至少保留一个可参与学术源。校验不会把必需代理无法解析的源计为最后一个可用源，但不探测代理或上游服务的健康。配置错误、只读环境或锁定字段不会通过确认写入。
3. 确认后备份当前主配置、精确修改可编辑字段并读回验证；影响源路由的变更会清理旧搜索缓存，随后请求重启。缓存通过持久化代际与源配置指纹防止旧在途请求重新写回已失效结果，重启后同样有效。界面核对磁盘已保存值与重启后的运行值，二者一致才显示「运行配置已确认生效」。仅保存成功、API暂时恢复或未出现错误都不足以说明生效。
4. 重启未确认时保留「未确认生效」状态并提供重试；备份列表非空时，正常成功状态也可使用「恢复备份并重启」，恢复前仍预览差异并确认。备份位于遥测数据库同目录的 `dashboard-backups/`。恢复只恢复可编辑设置，不覆盖当前其它字段、Key、管理员口令、dashboard.yaml 或遥测记录；备份文件可能含原配置凭证，须按私密文件保护。

保存及恢复都携带配置内容版本 `revision`。配置在预览后被其他会话或外部编辑改动时，服务端返回 **HTTP 409**，不覆盖新内容；刷新后重新比较/编写草稿再保存。被环境变量覆盖的字段标为锁定；由独立 dashboard.yaml 管理的 dashboard 字段同样不可从主配置写路径覆盖。需在真正的环境/overlay 来源修改并重启，不通过 WebUI 绕过。

## 工具与观测边界

工具清单展示五个公开工具，注册状态来自当前 MCP 运行快照，调用次数与健康来自真实工具事件。配置开启、已经注册、尚无观测是不同状态；未采集或无调用不是成功记录。

| 工具 | 边界 |
|---|---|
| `smartsearch` | 当前模式的可用网页/API组合；Bing 是源与回退，不是工具总开关 |
| `academicsearch` | 已注册学术源及请求指定子集；关闭源也约束 DOI/arXiv 精确查询 |
| `cleanfetch` | 始终注册；已保存正文读取可用，URL 抓取独立展示开关（`fetch_enabled`）；开关不限制 smartsearch 的 `fetch_top_n`。遥测 detail 为 `url_fetch` 或 `saved_result`，不记录引用/正文 |
| `pdf_parser` | PDF 解析；展示 `max_pages`（省略 `pages` 时默认 20 页）、`mineru_page_limit`（`GetMinerUPageLimit()` 默认 600 页）、分批大小、单次页预算和 OCR 配置，不代表本次已执行 OCR |
| `file_search` | Everything 只读文件名/路径索引检索，不读取文件正文；接入 MCP 时按既有机制延迟探测，dashboard 读取不触发探测 |

`file_search` 的目录白名单 `everything.roots`、每页 `max_results`（默认 10、硬上限 20）和 `timeout_sec`（默认 5 秒）以实际配置为准。`everything.roots: []` 表示无目录白名单限制，界面显示「目录未限制」；非空列表才限制检索范围。控制中心不编辑 roots；页数多时用分页，不扩大目录权限。配置 URL 不等于工具已可调用，应区分尚未探测、等待注册、不可用与已注册；鉴权材料不回显。

PDF 页进度当前为 `not_collected`，控制中心不能据此展示真实逐页进度、已处理页数或预算消耗。未采集的页级信息和上游覆盖不应被当成完整遥测；参数展示只说明运行配置，不说明某次调用的结果。

源上的观测警示（`suspension.*` / `suspension_scope: observation`）根据真实事件展示，**不跳过调用、不改变路由**。实际限流/冷却独立来自 `antirobot` 运行快照 `actual_cooldown`，包含原因、到期时间、剩余时间与持久化状态；不能用观测警示倒计时冒充实际冷却，未报告快照也不证明上游健康。

## 快捷方式落位（`shortcut`）

| 值 | 行为 |
|---|---|
| `desktop`（默认） | 桌面创建 `WebSearchMCP.lnk`，双击 = 拉起服务 + 打开控制台（lazy 启动） |
| `start` | 开始菜单「Programs」创建同名快捷方式：开始屏幕可直接搜索到，也可手动「固定到开始屏幕」。程序化直接 pin 在 Win10/11 上不可靠，故以开始菜单条目为准 |
| `both` | 桌面 + 开始菜单 |
| `off` | 不创建快捷方式，并删除桌面/开始菜单中的已知快捷方式；`dashboard.enabled: false` 行为相同 |

细节：

- 仅 Windows 生效；`start`、`open`、`install` 共用同一同步逻辑，重复执行幂等
- 期望状态由当前 exe 路径（目标 + `open` 参数 + exe 目录）与 `brand.theme` / `brand.icon` 计算；逐个读取 `.lnk` 校验目标、参数、工作目录、图标路径与图标索引，任一不符（手工改坏、exe 重命名或换目录、主题/图标变化、落位变化）即重写
- 已正确的快捷方式只读取校验并报 `unchanged`；缺失报 `created`，被改坏或需要换图标/路径报 `updated`，`off` 清理报 `removed`
- 图标文件名含内容哈希（`websearch-<theme|custom>-<hash8>.ico`）：主题或图标文件内容变化会换新文件名并重建 `.lnk`，绕开 Windows 图标缓存
- 更换 `shortcut` 值会清理不再需要的位置，不留死链；旧版 `websearch-shortcut.theme` marker 自动移除
- `uninstall` 始终清理全部已知位置（桌面 + 开始菜单 + 开机自启目录）

## 安全模型

| 操作 | 边界 |
|---|---|
| 读取（页面 + 只读 API） | 本机 loopback 始终可用；`allowed_networks` 放行的网段只读 |
| 写操作（设置/密钥/重启/清缓存/额度管理） | 默认仅 loopback；独立 `write_allowed_networks` 可放行明确来源；所有写入仍须正确 `X-Admin-Password`（常量时间比较，支持明文/SHA-256），配置用户名时还须匹配 `X-Admin-User` |
| 管理员口令 | 只存在于 dashboard.yaml；WebUI 与 API 永不返回，也无法修改 |
| 未配置口令 | 所有写端点整体禁用（安全的默认） |
| 查询隐私 | 遥测只存脱敏元数据（哈希/主题/语言/关键词/耗时），完整查询与 URL 不落库 |

读白名单不授权写入，写白名单不替代页面/API的读取访问门槛，也不替代口令；任一白名单条目非法时该白名单整体 fail-closed。白名单与管理员认证不在WebUI可编辑范围。

**Docker 部署**：保持宿主端口固定发布到回环，例如 `-p 127.0.0.1:8338:8338`。宿主浏览器在容器内可能表现为 bridge 网关；以服务端实际看到的请求来源 IP 为准，不能只据 `docker inspect` 网关值认定来源。需要宿主写入时，将这个实测单地址 `/32` 同时加入读/写白名单，并配置管理员口令；不放行整个 bridge 子网。以下地址仅示意，未经实测不能照抄：

```yaml
dashboard:
  allowed_networks: ["172.17.0.1/32"]
  write_allowed_networks: ["172.17.0.1/32"]
  admin_password: "replace-with-a-private-admin-password"
```

未确认来源前保持 `write_allowed_networks: []`。卷映射（`dashboard.yaml`、`data/`）见 [installation.md#docker](installation.md#docker)。

## 数据与生命周期

### 搜索调用用量

总览按应用或项目显示全部工具层调用、成功率、平均耗时和调用占比，可筛选最近 1 / 7 / 30 天及应用。Codex、WorkBuddy 等已知客户端别名合并展示；脚本调用也计入项目使用。调用记录主行直接显示应用与项目，详情保留原始客户端名，CSV 同样导出归属。应用身份使用 SDK 提供的标准逐调用 clientInfo，缺失时回退握手身份，适用于 HTTP 与 stdio；无身份时再参考 User-Agent。通用 `websearch` 名称不能单独证明宿主应用。

已知应用名单参考 [Token Monitor 的应用列表](https://github.com/Javis603/token-monitor#supported-tools)，覆盖 DeepSeek Harness（`dsh-mcp-client`）、OpenCode、OpenClaw、Hermes Agent、Antigravity、Amp、Factory Droid、Kimi、GitHub Copilot、Pi、Oh My Pi、Kilo Code、Command Code、MiMo Code、Muse Code、ZCode、Kiro、CodeBuddy、Proma、CatPaw、Qoder、Reasonix、LM Studio、Unsloth Studio、Devin、MiniMax Code，以及 Goose。已有 Codex、WorkBuddy 等分组保持不变。客户端必须自报匹配的应用名或已知别名。所有调用采用相同归组规则，不按测试、验收或诊断用途分组或排除；通用客户端和模型供应商不能单独证明宿主应用。保留期内的历史调用按同一规则实时归组，不修改原始记录。

HTTP 客户端若只发送通用名称，可在其 WebSearch 配置中添加 `X-WebSearch-Client: workbuddy` 等已确认的应用简称。合法显式标签优先于协议身份；只接受最多 32 个字母、数字、空格及 `._-`，不接受路径、URL、控制字符或 `.` / `..`。无法确认时显示「未识别应用」。所有归属都是客户端自报的统计标签，不是可信身份或权限依据。

项目归属优先取每次工具调用合法的 `project` 参数，其次使用兼容的 `X-WebSearch-Project` 请求头；缺少或无效时可采用 `dashboard.client_projects` 已确认的精确客户端映射，其余保持「未标注项目」。标签最多 64 个字母、数字、空格及 `._-`，支持中文，不接受路径、URL、控制字符及 `.` / `..`。标签只用于统计，不作为权限、检索范围或配额依据；调用主行悬停明确其为自报信息。

本仓库 [WebSearch Skill](../skills/websearch-mcp/SKILL.md) 在当前项目明确时传递非敏感简称，不要求逐项目配置，不读取会话日志。Skill 未加载、上下文未知或 Agent 漏传时仍可能未标注，不承诺全应用自动识别。项目属于当前调用，不从 initialize 或其他请求继承；不采集对话标识，不把缺失标签判为独立对话，不猜测或重写历史。接入方式见 [Skill 说明](installation.md#skill--应用项目归属)。

这不是模型 Token、金额或账户剩余额度。供应商额度仍在原额度面板按供应商展示，不按项目平分。应用 / 项目统计依赖保留明细，窗口以 `retention_days` 为上限，返回实际 `window_days`；不展示不完整的「全部历史」。`GET /__admin/api/clients?days=1|7|30` 保留原 `clients` 字段，新增按客户端、项目、MCP 工具聚合的 `usage`，不把 provider 并发计成额外客户端调用。

- 遥测明细按 `retention_days` 每 6 小时滚动清理；每日聚合长期保留（体积极小）
- `dashboard.db` 建表自动迁移（含 quota_state/client 列），老库升级零操作；回退旧版本时新表/新列被忽略
- 移除控制中心配置不会撤销已保存的源设置。先按备份恢复流程确认主配置运行值，再按需要停服务并移除控制中心配置/快捷方式；数据库与私密覆盖文件应先备份，不为回滚源设置而删除它们

## 环境变量

| 变量 | 作用 |
|---|---|
| `WEBSEARCH_DASHBOARD_CONFIG` | 指定 dashboard.yaml 路径（优先于 config_path） |
