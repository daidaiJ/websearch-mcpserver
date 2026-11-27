# 控制中心 API 接入指南

[English](dashboard-api.en.md) | 中文

本指南面向要替换或扩展控制中心前端的开发者。控制中心后端是 Go HTTP 服务，默认前端是仓库内构建的 React 页面；两者通过同源 `/__admin/api/` 的 JSON 接口通信。你不需要修改或重新编译内置前端，也不需要 Node 运行时——把自建静态页面放到一个目录，配置 `dashboard.ui.dir` 指向它即可。

> 设计边界：控制中心是**本机优先**的被动观测面板，不提供跨域 API，也不承担公网多租户职责。外挂 UI 与控制中心后端同源运行，读取权限由来源网段控制，管理操作由管理员口令控制。

---

## 1. 快速接入

### 1.1 启动控制中心

控制中心由 `dashboard.enabled` 控制；示例配置默认启用。手工部署时，在 `config.yaml` 同目录放置 `dashboard.yaml`：

```yaml
dashboard:
  enabled: true
  admin_password: "change-me"
```

启动服务：

```bash
./websearch-mcpserver start
```

打开 `http://127.0.0.1:8338/dashboard/` 确认内置界面可用。

### 1.2 关闭页面或去掉前端资产

`dashboard.ui.enabled: false` 只保留 `/__admin/api/`，不挂载 `/dashboard/`、不提供前端静态资源；API 与权限模型不变。适合只通过脚本或外部工具消费控制中心数据的部署。

该开关在注册路由前直接返回：不读取 `ui.dir`、不解析或注入 `web.*` 前端文案、不注册静态资源路由。控制中心后端（API、遥测、额度）仍按 `dashboard.enabled` 运行；要让 dashboard 后端也完全不启动，用 `dashboard.enabled: false`。

该开关不会改变二进制内容：内嵌页面仍在可执行文件里，只是不注册路由。需要极限体积时，在构建阶段排除前端资产：

```bash
go build -tags no_ui -o websearch-mcpserver ./cmd
```

`no_ui` 构建不包含 `pkg/dashboard/web/dist` 和内嵌 logo；此时 `ui.dir` 仍可指向外挂 UI，`ui.enabled: false` 则完全不挂载页面。若 `no_ui` 构建既没有 `ui.dir` 也没有关闭页面，`/dashboard/` 返回 500 并明确提示改用外挂目录或嵌入构建。两种方式都保留 `/__admin/api/`。

### 1.3 换成外挂 UI

外挂 UI 是一个包含 `index.html` 的静态目录。路径可以是绝对路径，也可以相对主 `config.yaml` 所在目录：

```yaml
dashboard:
  enabled: true
  ui:
    dir: "./dashboard-ui"
```

```text
dashboard-ui/
├── index.html
├── app.js
└── style.css
```

重启服务后，`/dashboard/` 会整体改用该目录，内置 React 页面不再参与路由。目录不存在或缺少 `index.html` 时，`/dashboard/` 返回 HTTP 500 并给出明确错误，不会静默回退到内置页面。

注意：

- `ui.dir` 为空 = 使用二进制内嵌的默认前端。
- 外挂目录中的文件按同源静态资源原样提供；`/dashboard/` 之外的路径不受影响。
- 以点开头的文件/目录（如 `.env`）不会对外提供，也没有目录列表；指向目录外的符号链接同样不会提供。
- 修改目录内容后刷新浏览器即可；修改 `ui.dir` 本身需要重启。该键属于启动配置，不进入控制台的设置草稿。
- 相对路径按主 `config.yaml` 所在目录解析，与 `dashboard.yaml` 的位置无关。

### 1.4 最小页面

`index.html`：

```html
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>My Dashboard</title>
  <script id="websearch-bootstrap"></script>
  <link rel="stylesheet" href="./style.css">
</head>
<body>
  <main id="app">加载中…</main>
  <script type="module" src="./app.js"></script>
</body>
</html>
```

`app.js`：

```js
const app = document.querySelector('#app');

async function read(path) {
  const response = await fetch(`/__admin/api/${path}`, {
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(body?.error ?? `HTTP ${response.status}`);
  }
  return body;
}

try {
  const overview = await read('overview');
  app.textContent = `工具调用：${overview.today.requests}`;
} catch (error) {
  app.textContent = `读取失败：${error.message}`;
}
```

页面没有内联脚本，因此符合默认 CSP。若确实需要内联脚本，必须使用服务端注入的 nonce（见 3.4），不要直接写 `<script>...</script>`。

### 1.5 本地开发

外挂 UI 可以直接用 Vite / Webpack 等工具开发，但生产构建的 `base` 必须是 `/dashboard/`，或使用相对路径 `./`。开发服务器把 API 代理到控制中心：

```js
// vite.config.js
export default {
  base: '/dashboard/',
  server: {
    proxy: {
      '/__admin/api': 'http://127.0.0.1:8338',
    },
  },
};
```

开发时打开 `http://localhost:5173/dashboard/`。发布时把构建产物放到 `dashboard.ui.dir` 指向的目录，重启后由控制中心同源提供。

---

## 2. 通用约定

### 2.1 基址与同源

所有接口都在：

```text
/__admin/api/
```

服务端不发送 CORS 头，也不接受跨域读取。外挂 UI 必须由同一个控制中心进程提供，或通过开发服务器代理访问。不要尝试从其它域名直接 `fetch`。

### 2.2 请求头

| 头 | 用途 |
|---|---|
| `Accept: application/json` | 建议显式声明；响应为 JSON |
| `Accept-Language: zh-CN` / `en` | 控制错误文案、来源名称、工具标签等本地化内容；响应中的 `language` 是实际协商结果 |
| `Content-Type: application/json` | 所有 POST 必需 |
| `X-Admin-Password` | 验密的管理操作必需 |
| `X-Admin-User` | 仅当配置了 `dashboard.admin_username` 时必需 |

浏览器会自动携带同源 Cookie，但控制中心不使用 Cookie 会话，认证只靠请求头。不要把管理员口令写入 `localStorage`、URL 或日志；页面内保存在内存变量中即可。

### 2.3 响应与缓存

- JSON 响应带 `Cache-Control: no-store`，不要缓存或持久化。
- 所有 JSON 响应和 `/__admin/api/metrics` 带 `X-WebSearch-Dashboard-API: 1`，可用于版本握手。
- 时间字段为 RFC3339 字符串；`generated_at`、`updated_at` 等带时区。
- 计数为整数；额度数字可能是小数（credits）。

### 2.4 错误格式

除二进制资源外，错误响应统一为：

```json
{ "error": "人类可读的错误说明" }
```

常见状态码：

| 状态码 | 含义 |
|---|---|
| `400` | 参数缺失、格式错误、`confirm` 未提供、备份名无效 |
| `403` | 来源不在读/写白名单，或管理员口令/用户名错误 |
| `404` | 路由或静态资源不存在 |
| `405` | HTTP 方法不允许 |
| `409` | 配置/凭证版本冲突，或功能未启用（如缓存、历史、备份） |
| `429` | 诊断并发已满 |
| `500` | 服务端内部错误或外挂 UI 目录无效 |
| `503` | 遥测未启用，相关只读接口不可用 |
| `504` | 额度刷新超时 |

### 2.5 权限模型

控制中心有两层独立边界：

1. **读取边界**：`dashboard.allowed_networks` 控制哪些来源可以访问 `/dashboard/` 和只读 API。默认仅 loopback。
2. **操作边界**：`dashboard.write_allowed_networks` 控制哪些来源可以执行 POST。默认仅 loopback，读白名单不授予操作权限。

POST 再分两类：

| 类型 | 要求 | 端点 |
|---|---|---|
| 免密运行态操作 | 通过操作来源校验；仍需 `confirm:true`（如适用） | `cache/clear`、`history/clear`、`fetchdata/clear`、`quotas/refresh`、`sources/cooldowns/release`、`diagnostics/*` |
| 验密的管理操作 | 通过操作来源校验 + `X-Admin-Password`（配置用户名时再加 `X-Admin-User`） | `settings`、`secrets`、`restart`、`quotas/reset`、`quotas/adjust`、`everything/scope` |

免密端点拒绝浏览器跨站请求（`Sec-Fetch-Site` 校验）；原生脚本调用同样检查真实连接来源，`Host` 头不参与判定。`GET settings` 的 `write_access.allowed` 只表示当前来源**有资格**写入，不代表已经通过口令校验。

### 2.6 外挂 UI 的安全边界

服务端为 `/dashboard/` 设置：

```text
default-src 'self';
script-src 'self' 'nonce-<random>';
style-src 'self' 'unsafe-inline';
img-src 'self' data:;
connect-src 'self';
object-src 'none';
base-uri 'none';
frame-ancestors 'none';
form-action 'self'
```

这意味着：

- 脚本必须来自同一控制中心源；内联脚本必须带注入的 nonce。
- 不能加载 CDN、远程字体、远程图片或 iframe。
- `fetch` 只能访问同源，正好覆盖 `/__admin/api/`。
- `frame-ancestors 'none'` 禁止被其它页面嵌入。

如果外挂 UI 需要 Worker，`worker-src` 未显式放开，默认继承 `default-src 'self'`；blob Worker 会被拦截。

---

## 3. 接口参考

下表中的“读”表示只需读取边界，“操作”表示需要操作来源校验，“管理”表示还需要管理员口令。

### 3.1 总览与运行态

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `overview` | 读 | 总览：今日/趋势、来源、工具、运行态、品牌、诊断选项 |
| GET | `providers` | 读 | 合并后的网页 + 学术来源状态；适合脚本消费 |
| GET | `settings` | 读 | 当前运行值、磁盘值、可编辑字段、备份、凭证状态、写权限提示 |
| GET | `storage` | 读 | 缓存/历史/临时抓取文件的条数与磁盘占用 |
| GET | `metrics` | 读 | Prometheus 文本格式的被动遥测 |
| GET | `brand/logo` | 读 | 配置的本地 logo；未配置或为 URL 时 404 |

`overview` 响应顶层字段：

```json
{
  "generated_at": "2026-11-27T12:00:00+08:00",
  "today": { "day": "2026-11-27", "requests": 42, "successes": 40, "failures": 2, "cache_hits": 8, "duration_ms": 12000, "result_count": 320 },
  "trend": [ { "day": "2026-11-26", "requests": 35 } ],
  "providers": [ { "name": "tavily", "status": "healthy" } ],
  "tools": [ { "name": "smartsearch", "status": "healthy" } ],
  "system": { "status": "healthy", "enabled": 8, "observed": 6, "degraded": 1, "down": 0, "suspended": 1 },
  "runtime": {
    "started_at": "2026-11-27T09:00:00Z",
    "version": "3.10.7",
    "uptime_seconds": 10800,
    "mcp_stateless": false,
    "mcp_resources": true,
    "proxy": { "mode": "auto", "resolved": true, "api_providers": false },
    "llm": { "enabled": false, "model_id": "", "timeout_seconds": 30 },
    "mcp_hub": { "enabled": false, "fetch_mode": "off", "servers": [] }
  },
  "sources": [ { "id": "tavily", "name": "Tavily", "group": "web", "active": true, "status": "healthy", "today": { "requests": 12 }, "suspended": false } ],
  "academic_sources": [],
  "configured_tools": [ { "name": "smartsearch", "label": "智能搜索", "enabled": true, "registered": true, "state": "active" } ],
  "diagnostics": { "sources": [ { "id": "tavily", "name": "Tavily", "confirm": false } ], "fetch_services": [] },
  "brand": { "title": "WebSearch 控制中心", "theme": "green", "accent": "", "logo": "", "footer": true },
  "language": "zh"
}
```

要点：

- `providers` / `tools` 是遥测观测到的原始条目；`sources` / `academic_sources` 是“配置清单 + 观测状态”的合并视图，界面应优先使用后者。
- `sources[].today` 类型为 `DailyUsage`，字段包括 `requests`、`successes`、`failures`、`cache_hits`、`duration_ms`、`result_count`。
- `sources[].actual_cooldown` 是路由层真实冷却，`suspended` / `suspension_scope` 是观测告警，两者含义不同，不要混用。
- `configured_tools` 是固定工具清单，不依赖是否产生过调用。
- `brand.logo` 为 `/__admin/api/brand/logo` 时表示本地文件，需要 `<img>` 直接加载；为 `http(s)` URL 时内置页面会清空该字段以符合 CSP，外挂 UI 可自行决定是否展示远程图片（会被 CSP 拦截）。

### 3.2 调用记录

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `calls` | 读 | 工具调用列表，支持游标分页与组合筛选 |
| GET | `events` | 读 | 来源/工具事件列表，兼容旧界面 |
| GET | `request-events` | 读 | 按 `request_id` 读取一次调用的完整来源尝试 |
| GET | `clients` | 读 | 客户端与项目维度的用量聚合 |

`calls` 查询参数：

| 参数 | 值 | 说明 |
|---|---|---|
| `limit` | `20` / `50` / `100` | 其它值回退 50；缺省 100 |
| `before` | `<unix_seconds>:<id>` | 上一页最后一条的 `occurred_at` 与 `id`，例如 `1760000000:123` |
| `tool` | 工具名 | 精确匹配 |
| `provider` | 来源名 | 匹配同一请求的来源证据 |
| `status` | `success` / `failure` | 工具成功或失败 |
| `error_kind` | 见下表 | 错误分类 |
| `cache_hit` | `true` / `false` | 缓存命中 |
| `application` | 标准应用 ID | 例如 `codex`、`cursor`、`claude` |
| `project` | 项目标签 | 精确匹配 |

响应是 `StoredEvent` 数组，按时间倒序：

```json
[
  {
    "id": 1234,
    "occurred_at": "2026-11-27T11:59:00+08:00",
    "kind": "tool",
    "tool": "smartsearch",
    "provider": "",
    "success": true,
    "duration_ms": 842,
    "cache_hit": false,
    "result_count": 8,
    "request_id": "req_abc",
    "attempt_chain": "tavily>bing",
    "query_topic": "technology",
    "query_language": "zh",
    "query_keywords": "mcp,search",
    "client": "cursor",
    "application": "cursor",
    "project": "default"
  }
]
```

`events` 额外支持 `kind=provider|tool|none`、`source`、`request_id` 和 `limit`（20/50/100）。`request-events` 必须带 `request_id`，返回该请求保留的 provider / quota_skip 事件；没有请求编号的历史记录无法关联。

`clients?days=1|7|30`：

```json
{
  "window_days": 7,
  "clients": [ { "client": "cursor", "requests": 120, "successes": 118, "failures": 2, "avg_ms": 640, "last_seen": "2026-11-27T11:00:00+08:00", "tools": [ { "tool": "smartsearch", "requests": 100, "successes": 99, "failures": 1, "avg_ms": 620 } ] } ],
  "usage": [ { "client": "cursor", "application": "cursor", "project": "default", "tool": "smartsearch", "requests": 100, "successes": 99, "failures": 1, "duration_ms": 62000 } ]
}
```

### 3.3 额度

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `quotas` | 读 | 当前额度快照（本地账本 + 官方用量） |
| POST | `quotas/refresh` | 操作 | 强制刷新官方用量；`{ "confirm": true, "provider": "tavily" }`，省略 `provider` 刷新全部 |
| POST | `quotas/reset` | 管理 | `{ "provider": "tavily" }` 或 `{ "provider": "all" }`，从当前时间重新累计 |
| POST | `quotas/adjust` | 管理 | `{ "provider": "tavily", "set_used": 120 }`，只改展示用量，不改真实调用记录 |

`GET quotas` 返回 `Item[]`：

```json
[
  {
    "provider": "tavily",
    "configured": true,
    "status": "available",
    "unit": "credits",
    "used": 120,
    "limit": 1000,
    "remaining": 880,
    "plan": "dev",
    "updated_at": "2026-11-27T11:55:00Z",
    "source": "official",
    "reset_at": "2026-12-01T00:00:00Z",
    "auto_reset": "monthly",
    "local_usage": { "status": "available", "unit": "calls", "used": 110, "limit": 1000, "remaining": 890, "reset_at": "2026-12-01T00:00:00Z", "auto_reset": "monthly", "enforced": true }
  }
]
```

- `status` 常见值：`available`、`exhausted`、`no_data`、`error`、`unconfigured`。
- `used` / `limit` / `remaining` 可能缺省（指针字段），不要假设一定存在。
- `official_query_failed` 为 `true` 时，主数字来自本地回退，界面上应标注。
- `reset_at` / `auto_reset` 描述本地周期；`source=official` 时以供应商返回为准。
- 该接口可能等待官方端点，最长约 12 秒；前端应设置 15 秒超时并先渲染旧快照。

`quotas/refresh` 返回：

```json
{ "items": [ /* Item[] */ ], "refreshed": true }
```

`quotas/reset` 返回 `{ "reset": ["tavily"] }`；`quotas/adjust` 返回 `{ "adjusted": "tavily", "set_used": 120 }`。

### 3.4 设置

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `settings` | 读 | 运行值、磁盘值、字段选项、锁、备份与写权限 |
| POST | `settings` | 管理 | 预览或保存设置；支持备份恢复 |

GET 响应的核心字段：

| 字段 | 说明 |
|---|---|
| `values` | 当前**运行**值；键是点分路径，如 `mode`、`cache.enabled` |
| `options` | 枚举字段的允许值 |
| `saved_values` | 磁盘上已保存的值 |
| `pending_changes` | 磁盘值与运行值的差异（重启待生效） |
| `locked_fields` | 字段 → 不可编辑原因（环境变量覆盖、只读文件等） |
| `field_files` | 字段 → 所属配置文件（主配置或 `dashboard.yaml`） |
| `revision` | 配置内容版本；保存时回传 |
| `backups` | `[{ "name": "...", "created_at": "..." }]` |
| `credential_status` | `CredentialView`，见 3.5 |
| `write_access` | `{ "allowed": bool, "reason": string }`；只表示当前来源是否有资格写入 |
| `local_action_allowed` | 当前来源是否通过操作边界 |
| `config_error` | 读取/解析配置文件失败时的说明 |

POST 请求：

```json
{
  "changes": { "mode": "hybrid", "cache.enabled": true },
  "revision": "配置版本",
  "confirm": false,
  "restore_backup": ""
}
```

- `confirm:false` 只预览并校验，返回 `applied:false`；`confirm:true` 才落盘。
- `restore_backup` 非空时恢复指定备份中的可编辑字段，仍先预览后保存。
- 返回：

```json
{
  "applied": true,
  "restart_required": true,
  "backup": "settings-20261127T120000.000000000Z-abc.yaml",
  "changes": { "mode": "hybrid" },
  "revision": "新版本",
  "field_files": { "mode": "config.yaml" }
}
```

- `409` 表示 `revision` 已过期；重新 GET 后比较再保存，不要覆盖。
- 保存成功后若 `restart_required:true`，需要调用 `restart` 并等待服务恢复；仅“保存成功”不代表运行值已生效。
- 预览和保存必须使用同一份 `changes` 与 `revision`。

### 3.5 凭证

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `secrets` | 读 | 生效凭证的数量、来源、槽位备注；永不返回值 |
| POST | `secrets` | 管理 | 预览/保存追加、替换、删除、清空、恢复配置来源 |

GET 返回：

```json
{
  "credentials": {
    "TAVILY_SK": {
      "count": 2,
      "configured": true,
      "multi": true,
      "read_only": false,
      "sources": ["config", "overlay"],
      "slots": [ { "id": "slot_1", "label": "primary", "source": "config" } ]
    }
  },
  "pending": {},
  "pending_restart": false,
  "revision": "凭证文件版本"
}
```

可用凭证名：

`BAIDU_SK`、`TAVILY_SK`、`EXA_API_KEY`、`ANYSEARCH_API_KEY`、`DOUBAO_SEARCH_API_KEY`、`TENCENTCLOUD_WSA_APIKEY`、`TINYFISH_API_KEY`、`KEENABLE_API_KEY`、`GITHUB_TOKEN`、`JINA_API_KEY`、`MINERU_TOKEN`、`LLM_API_KEY`、`SEMANTIC_SCHOLAR_API_KEY`、`OPENALEX_API_KEY`、`UNPAYWALL_EMAIL`。

`WEBSEARCH_TOKEN` 只读，不能通过控制台修改。

POST 请求：

```json
{
  "name": "TAVILY_SK",
  "action": "append",
  "value": "tvly-...",
  "label": "primary",
  "slot_id": "",
  "revision": "凭证文件版本",
  "confirm": false
}
```

动作：

| `action` | 适用 | 必填 | 说明 |
|---|---|---|---|
| `append` | 多 Key 凭证 | `value` | 在池尾追加，保留现有槽位 |
| `set` | 单值凭证 | `value` | 设置唯一值，原配置来源被覆盖 |
| `replace` | 多 Key 凭证 | `value`、`slot_id` | 替换指定槽位 |
| `delete` | 多 Key 凭证 | `slot_id` | 删除指定槽位 |
| `label` | 多 Key 凭证 | `slot_id`、`label` | 只改备注 |
| `clear` | 全部 | 无 | 隐藏配置/环境来源，清空覆盖槽位 |
| `reset` | 全部 | 无 | 恢复配置/环境来源 |

- `confirm:false` 返回 `saved:false`、`applied:false`，用于展示前后数量；`confirm:true` 才写文件。
- 返回 `before_count`、`after_count`、`backup`、`restart_required`、`revision`。
- `409` 表示凭证文件被外部修改或槽位已变化；重新 GET 后再操作。
- `restart_required:true` 时，新值要重启后才进入运行配置。

### 3.6 诊断

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `diagnostics/proxy` | 操作 | TCP 探测已配置代理端点 |
| POST | `diagnostics/everything` | 操作 | 重探 Everything HTTP Server |
| POST | `diagnostics/search` | 操作 | 单来源试搜，可能消耗供应商额度 |
| POST | `diagnostics/fetch` | 操作 | 单 URL 试抓，可能调用远端抓取服务 |

试搜请求：

```json
{ "source": "tavily", "query": "mcp search", "confirm": false }
```

需要确认的来源（响应 `diagnostics.sources[].confirm=true`）必须带 `confirm:true`。试抓：

```json
{ "url": "https://example.com/article", "confirm": true }
```

响应：

```json
{
  "ok": true,
  "elapsed_ms": 320,
  "result_count": 3,
  "titles": ["..."],
  "links": [ { "title": "...", "url": "https://..." } ],
  "error_kind": "",
  "error_detail": ""
}
```

试抓额外带 `fetch`：

```json
{ "fetch": { "node": "local", "attempts": ["local", "jina"], "body_status": "ok", "characters": 12000, "remote_redirects": false } }
```

诊断有全局并发 1 的限制，忙时返回 `429`。结果只用于人工排查，不代表真实搜索路由的完整重试链。

### 3.7 运行态维护

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `sources/cooldowns/release` | 操作 | `{ "engine": "tavily" }` 或 `{ "engine": "all" }`，解除路由冷却 |
| POST | `cache/clear` | 操作 | `{ "confirm": true }`，清空搜索缓存 |
| POST | `history/clear` | 操作 | `{ "confirm": true }`，清空本地搜索历史 |
| POST | `fetchdata/clear` | 操作 | `{ "confirm": true, "scope": "expired" }` 或 `scope:"all"` |
| POST | `restart` | 管理 | `{ "confirm": true }`，异步重启，返回 202 |
| GET | `everything/scope` | 管理 | 返回 Everything 服务地址与完整目录白名单 |

返回值：

- 冷却解除：`{ "cleared": 2 }`
- 清缓存/历史/临时文件：`{ "cleared": 123 }`
- 重启：HTTP 202 `{ "restarting": true }`；客户端应轮询 `overview.runtime.started_at`，变化后再确认配置生效。
- Everything 范围：`{ "url": "http://127.0.0.1:4180", "roots": ["D:/data"] }`

### 3.8 品牌与指标

`GET brand/logo` 只返回本地图片文件；`dashboard.brand.logo` 为 `http(s)` URL 时该端点 404，由页面直接引用（但内置页面受 CSP 限制不会加载远程图片）。

`GET __admin/api/metrics` 返回 Prometheus 文本，字段为进程内被动计数；控制中心关闭时该路由不存在，但主服务的 `/metrics` 端点仍可用，见 [api.md](api.md#观测端点)。

---

## 4. 外挂 UI 的启动引导

### 4.1 可选注入的全局变量

如果 `index.html` 中包含：

```html
<script id="websearch-bootstrap"></script>
```

服务端会把它替换为带 nonce 的内联脚本，注入：

| 全局变量 | 内容 |
|---|---|
| `window.__DEFAULT_LOCALE__` | 按 `Accept-Language` 协商出的 `zh` / `en` |
| `window.__I18N_CATALOGS__` | `{ zh: {...}, en: {...} }` 的 `web` 域文案 |
| `window.__BRAND__` | `{ title, theme, accent, logo, footer }` |
| `window.__I18N__` | 当前语言的文案对象（由脚本根据本地偏好选择） |

同时脚本会恢复 `localStorage` 中的主题、密度、语言和品牌覆盖，并设置 `<html data-theme>`、`data-brand` 等属性。是否使用这些变量由外挂 UI 决定；不使用也能正常工作。

如果 `index.html` 没有该占位符，服务端原样返回页面，不注入任何脚本，也不会修改 `<html lang>`。

### 4.2 渐进式渲染与防抖

后端没有 SSE 端点，数据通过 GET 拉取。推荐的界面行为：

1. 首次加载显示骨架或旧快照。
2. 请求成功后替换为新快照；失败时保留旧快照并显示“读取失败，数据可能过期”。
3. 筛选输入防抖 150–300ms，切换筛选时中止上一个请求（`AbortController`）。
4. 轮询间隔不要短于 5 秒；页面隐藏时暂停。
5. 不要在每次渲染时重新请求同一份 `overview`；多个组件共享一次在途请求。

```js
let snapshot;
let controller;
let timer;

function render(data, stale = false) {
  // 用 data 渲染；stale 时加“数据可能过期”标记
}

async function refresh() {
  controller?.abort();
  controller = new AbortController();
  try {
    const response = await fetch('/__admin/api/overview', {
      signal: controller.signal,
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    snapshot = await response.json();
    render(snapshot);
  } catch (error) {
    if (error.name !== 'AbortError') render(snapshot, true);
  }
}

function onFilterChange() {
  clearTimeout(timer);
  timer = setTimeout(refresh, 200);
}
```

### 4.3 管理口令输入

不要把口令持久化。打开管理操作时在内存中保存，失败后清除：

```js
let admin = null; // { password, username }

async function adminRequest(path, body) {
  if (!admin?.password) throw new Error('需要管理员口令');
  const headers = {
    'Content-Type': 'application/json',
    'X-Admin-Password': admin.password,
  };
  if (admin.username) headers['X-Admin-User'] = admin.username;
  const response = await fetch(`/__admin/api/${path}`, {
    method: 'POST',
    headers,
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  if (response.status === 403) admin = null;
  if (!response.ok) {
    const problem = await response.json().catch(() => null);
    throw new Error(problem?.error ?? `HTTP ${response.status}`);
  }
  return response.json();
}
```

安全建议：

- 口令只存在内存；刷新页面即清除。
- 不要写入 URL、`localStorage`、`sessionStorage`、日志或错误上报。
- 403 后不要自动重试，提示用户重新输入。
- 修改设置的预览与保存必须使用同一份 `changes` 和 `revision`。

### 4.4 设置保存流程

```text
GET settings
  → 用户编辑 values 的副本
  → POST settings { changes, revision, confirm:false }   // 预览
  → 展示 changes 前后差异
  → POST settings { changes, revision, confirm:true }    // 保存
  → restart_required ? POST restart { confirm:true } : 完成
  → 轮询 overview，确认 runtime.started_at 变化且 saved_values 生效
```

遇到 `409` 时重新 `GET settings`，把用户改动与最新值对比后再继续；不要盲目覆盖。

---

## 5. 版本与兼容

- API 基址固定为 `/__admin/api/`，当前版本通过 `X-WebSearch-Dashboard-API: 1` 声明。
- 同一主版本内只做加法：新增字段、新增端点、新增枚举值。外挂 UI 应忽略未知字段和未知枚举值。
- 删除字段、改变字段类型或语义、改变认证要求属于破坏性变更，会提升主版本并在 [CHANGELOG](../CHANGELOG.md) 中说明。
- 外挂 UI 应在启动时检查响应头版本；不匹配时给出明确提示，避免静默错位。
- 接口没有 OpenAPI 文档；本文件与 `pkg/dashboard/` 的 handler 是事实来源。发现文档与实现不一致时，以代码为准并提交文档修正。

---

## 6. 自测清单

- [ ] `/dashboard/` 返回外挂 `index.html`，静态资源 200，`.env` 等点文件 404。
- [ ] 浏览器控制台无 CSP 拦截；`fetch('/__admin/api/overview')` 成功。
- [ ] 刷新页面后旧数据先显示，新数据到达后替换；请求失败时保留旧数据并提示。
- [ ] 筛选连续输入只触发一次请求；快速切换时旧响应不会覆盖新响应。
- [ ] 管理操作未输入口令时得到 403；输入正确口令后成功。
- [ ] `GET settings` 的 `revision` 在保存时回传；模拟并发修改得到 409 后能重新加载。
- [ ] 保存后 `restart_required:true` 时执行重启，并确认 `runtime.started_at` 变化。
- [ ] 外挂目录缺少 `index.html` 时 `/dashboard/` 返回 500，不回退内置页面。
- [ ] `dashboard.ui.dir` 使用相对路径时，按主 `config.yaml` 目录解析。
- [ ] 移动端宽度下页面可操作，长表格可横向滚动。

---

## 7. 源码索引

| 关注点 | 文件 |
|---|---|
| 路由注册、鉴权、总览/设置/额度 | `pkg/dashboard/handler.go` |
| 外挂 UI 选择、静态资源、CSP、bootstrap | `pkg/dashboard/assets.go` |
| 设置文件读写与 revision | `pkg/dashboard/control_settings*.go` |
| 凭证槽位编辑 | `pkg/dashboard/credentials.go`、`pkg/config/credential_editor.go` |
| 来源/总览视图 | `pkg/dashboard/sources.go`、`pkg/dashboard/runtime.go` |
| 调用与请求事件 | `pkg/telemetry/store.go` |
| 额度模型 | `pkg/quota/quota.go` |
| 诊断 | `pkg/dashboard/diagnostics.go` |
| 配置项 | `pkg/config/config.go`、`dashboard.example.yaml` |
