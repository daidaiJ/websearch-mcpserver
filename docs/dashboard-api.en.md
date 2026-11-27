# Dashboard API Integration Guide

English | [中文](dashboard-api.md)

This guide is for developers who want to replace or extend the control-center frontend. The backend is a Go HTTP service; the default frontend is a React build embedded in the repository. Both communicate over same-origin JSON endpoints under `/__admin/api/`. You do not need to modify or rebuild the built-in frontend, and you do not need a Node runtime: put your static page in a directory, point `dashboard.ui.dir` at it, and restart.

> Design boundary: the control center is a **local-first, passive observability panel**. It does not provide a cross-origin API and is not intended for public multi-tenant exposure. An external UI runs same-origin with the backend; read access is controlled by source network rules, and administrative operations are controlled by the admin password.

---

## 1. Getting started

### 1.1 Start the control center

The control center is controlled by `dashboard.enabled`; the example configuration enables it. For a manual deployment, place `dashboard.yaml` next to `config.yaml`:

```yaml
dashboard:
  enabled: true
  admin_password: "change-me"
```

Start the service:

```bash
./websearch-mcpserver start
```

Open `http://127.0.0.1:8338/dashboard/` and confirm the built-in UI works.

### 1.2 Disable the page or remove the frontend assets

`dashboard.ui.enabled: false` keeps `/__admin/api/` and does not mount `/dashboard/` or serve its static assets; the API and permission model are unchanged. This suits deployments that consume control-center data from scripts or external tools.

The switch returns before route registration: it does not read `ui.dir`, parse or inject the `web.*` frontend catalog, or register static routes. The dashboard backend (API, telemetry, quota) still follows `dashboard.enabled`; use `dashboard.enabled: false` to skip that backend as well.

The switch does not change the binary: the embedded page is still present and simply is not routed. For the smallest binary, exclude the frontend at build time:

```bash
go build -tags no_ui -o websearch-mcpserver ./cmd
```

A `no_ui` build contains neither `pkg/dashboard/web/dist` nor the embedded logos. `ui.dir` can still point at an external UI, and `ui.enabled: false` mounts no page at all. If a `no_ui` build has neither `ui.dir` nor a disabled page, `/dashboard/` returns 500 with a clear instruction to use an external directory or an embedded build. Both forms keep `/__admin/api/`.

### 1.3 Switch to an external UI

An external UI is a static directory containing `index.html`. The path may be absolute or relative to the main `config.yaml` directory:

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

After a restart, `/dashboard/` serves that directory instead of the embedded React UI. If the directory is missing or has no `index.html`, `/dashboard/` returns HTTP 500 with a clear error; it never silently falls back to the built-in page.

Notes:

- An empty `ui.dir` uses the embedded default frontend.
- Files in the external directory are served as same-origin static resources; paths outside `/dashboard/` are unaffected.
- Dotfiles and dot-directories (such as `.env`) are not exposed, and directory listings are disabled; symlinks that point outside the directory are not served.
- Editing files takes effect on browser refresh; changing `ui.dir` itself requires a restart. It is bootstrap configuration and is not part of the dashboard settings draft.
- Relative paths resolve against the main `config.yaml` directory, not the `dashboard.yaml` location.

### 1.4 Minimal page

`index.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>My Dashboard</title>
  <script id="websearch-bootstrap"></script>
  <link rel="stylesheet" href="./style.css">
</head>
<body>
  <main id="app">Loading…</main>
  <script type="module" src="./app.js"></script>
</body>
</html>
```

`app.js`:

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
  app.textContent = `Tool calls: ${overview.today.requests}`;
} catch (error) {
  app.textContent = `Read failed: ${error.message}`;
}
```

The page has no inline scripts, so it satisfies the default CSP. If you really need an inline script, use the server-injected nonce (see 3.4); do not write a bare `<script>...</script>`.

### 1.5 Local development

You can develop an external UI with Vite, Webpack, or similar, but the production build `base` must be `/dashboard/` or a relative `./`. Proxy API calls to the control center during development:

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

Open `http://localhost:5173/dashboard/` during development. For production, put the build output in the `dashboard.ui.dir` directory and restart; the control center serves it same-origin.

---

## 2. Conventions

### 2.1 Base path and same-origin

All endpoints live under:

```text
/__admin/api/
```

The server sends no CORS headers and does not allow cross-origin reads. An external UI must be served by the same control-center process, or reached through a development proxy. Do not `fetch` it from another origin.

### 2.2 Request headers

| Header | Purpose |
|---|---|
| `Accept: application/json` | Recommended; responses are JSON |
| `Accept-Language: zh-CN` / `en` | Localizes error messages, source names, tool labels; the response `language` field reports the negotiated value |
| `Content-Type: application/json` | Required for every POST |
| `X-Admin-Password` | Required for authenticated administrative operations |
| `X-Admin-User` | Required only when `dashboard.admin_username` is configured |

The browser sends same-origin cookies automatically, but the control center does not use cookie sessions; authentication is header-only. Never put the admin password in `localStorage`, URLs, or logs; keep it in a memory variable.

### 2.3 Responses and caching

- JSON responses use `Cache-Control: no-store`; do not cache or persist them.
- Every JSON response and `/__admin/api/metrics` carries `X-WebSearch-Dashboard-API: 1` for version negotiation.
- Time fields are RFC3339 strings with a timezone (`generated_at`, `updated_at`, and so on).
- Counts are integers; quota numbers may be fractional (credits).

### 2.4 Error format

Except for binary resources, errors use:

```json
{ "error": "human-readable explanation" }
```

Common status codes:

| Status | Meaning |
|---|---|
| `400` | Missing/invalid parameter, missing `confirm`, invalid backup name |
| `403` | Source outside the read/write allowlist, or wrong admin password/username |
| `404` | Route or static resource not found |
| `405` | Method not allowed |
| `409` | Configuration/credential revision conflict, or feature disabled (cache, history, backup) |
| `429` | Diagnostic concurrency limit reached |
| `500` | Internal error or invalid external UI directory |
| `503` | Telemetry disabled; the read endpoint is unavailable |
| `504` | Quota refresh timed out |

### 2.5 Permission model

There are two independent boundaries:

1. **Read boundary**: `dashboard.allowed_networks` controls which sources can access `/dashboard/` and read-only APIs. Default is loopback only.
2. **Operation boundary**: `dashboard.write_allowed_networks` controls which sources can execute POSTs. Default is loopback only; the read allowlist does not grant operation access.

POSTs fall into two groups:

| Type | Requirement | Endpoints |
|---|---|---|
| Passwordless runtime action | Operation-source check; `confirm:true` where applicable | `cache/clear`, `history/clear`, `fetchdata/clear`, `quotas/refresh`, `sources/cooldowns/release`, `diagnostics/*` |
| Authenticated administrative action | Operation-source check + `X-Admin-Password` (plus `X-Admin-User` when configured) | `settings`, `secrets`, `restart`, `quotas/reset`, `quotas/adjust`, `everything/scope` |

Passwordless endpoints reject cross-site browser requests (`Sec-Fetch-Site` check); native scripts still have their real connection source checked. The `Host` header never participates. `write_access.allowed` from `GET settings` only says the current source is **eligible** to write; it does not mean the password has been verified.

### 2.6 External UI security boundary

The server sets this CSP for `/dashboard/`:

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

Therefore:

- Scripts must come from the same control-center origin; inline scripts need the injected nonce.
- CDNs, remote fonts, remote images, and iframes are blocked.
- `fetch` is limited to the same origin, which covers `/__admin/api/`.
- `frame-ancestors 'none'` prevents embedding by other pages.

If the external UI needs Workers, `worker-src` is not explicitly opened and inherits `default-src 'self'`; blob Workers are blocked.

---

## 3. Endpoint reference

In the tables below, "read" means the read boundary, "action" means the operation-source check, and "admin" additionally requires the admin password.

### 3.1 Overview and runtime

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `overview` | read | Overview: today/trend, sources, tools, runtime, brand, diagnostic options |
| GET | `providers` | read | Merged web + academic source state; convenient for scripts |
| GET | `settings` | read | Running values, disk values, editable fields, backups, credential state, write capability |
| GET | `storage` | read | Cache/history/temporary-fetch counts and disk usage |
| GET | `metrics` | read | Passive telemetry in Prometheus text format |
| GET | `brand/logo` | read | Configured local logo; 404 when unset or an HTTP(S) URL |

`overview` top-level fields:

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
  "configured_tools": [ { "name": "smartsearch", "label": "Smart Search", "enabled": true, "registered": true, "state": "active" } ],
  "diagnostics": { "sources": [ { "id": "tavily", "name": "Tavily", "confirm": false } ], "fetch_services": [] },
  "brand": { "title": "WebSearch Control Center", "theme": "green", "accent": "", "logo": "", "footer": true },
  "language": "en"
}
```

Key points:

- `providers` / `tools` are raw observed telemetry; `sources` / `academic_sources` merge the configured catalog with observations and are the preferred UI view.
- `sources[].today` is a `DailyUsage` with `requests`, `successes`, `failures`, `cache_hits`, `duration_ms`, and `result_count`.
- `sources[].actual_cooldown` is the real routing cooldown; `suspended` / `suspension_scope` is an observational warning. Do not conflate them.
- `configured_tools` is a fixed tool catalog independent of whether calls occurred.
- When `brand.logo` is `/__admin/api/brand/logo`, it is a local file loaded with `<img>`. When it is an HTTP(S) URL, the built-in page clears the field to satisfy CSP; an external UI may choose to show remote images, but CSP will block them.

### 3.2 Call records

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `calls` | read | Tool-call list with cursor pagination and combined filters |
| GET | `events` | read | Source/tool event list, kept for older UIs |
| GET | `request-events` | read | Full retained source attempts for one `request_id` |
| GET | `clients` | read | Usage grouped by client and project |

`calls` parameters:

| Parameter | Value | Meaning |
|---|---|---|
| `limit` | `20` / `50` / `100` | Other values fall back to 50; default 100 |
| `before` | `<unix_seconds>:<id>` | Last row's `occurred_at` and `id`, e.g. `1760000000:123` |
| `tool` | tool name | Exact match |
| `provider` | source name | Matches source evidence in the same request |
| `status` | `success` / `failure` | Tool success or failure |
| `error_kind` | see below | Error classification |
| `cache_hit` | `true` / `false` | Cache hit |
| `application` | standard app ID | e.g. `codex`, `cursor`, `claude` |
| `project` | project label | Exact match |

The response is a newest-first `StoredEvent` array:

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
    "query_language": "en",
    "query_keywords": "mcp,search",
    "client": "cursor",
    "application": "cursor",
    "project": "default"
  }
]
```

`events` additionally accepts `kind=provider|tool|none`, `source`, `request_id`, and `limit` (20/50/100). `request-events` requires `request_id` and returns retained provider / quota_skip events for that request; history rows without a request ID cannot be correlated.

`clients?days=1|7|30`:

```json
{
  "window_days": 7,
  "clients": [ { "client": "cursor", "requests": 120, "successes": 118, "failures": 2, "avg_ms": 640, "last_seen": "2026-11-27T11:00:00+08:00", "tools": [ { "tool": "smartsearch", "requests": 100, "successes": 99, "failures": 1, "avg_ms": 620 } ] } ],
  "usage": [ { "client": "cursor", "application": "cursor", "project": "default", "tool": "smartsearch", "requests": 100, "successes": 99, "failures": 1, "duration_ms": 62000 } ]
}
```

### 3.3 Quotas

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `quotas` | read | Current quota snapshot (local ledger + official usage) |
| POST | `quotas/refresh` | action | Force official usage refresh; `{ "confirm": true, "provider": "tavily" }`, omit `provider` for all |
| POST | `quotas/reset` | admin | `{ "provider": "tavily" }` or `{ "provider": "all" }`; recount from now |
| POST | `quotas/adjust` | admin | `{ "provider": "tavily", "set_used": 120 }`; display value only, real records untouched |

`GET quotas` returns `Item[]`:

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

- Common `status` values: `available`, `exhausted`, `no_data`, `error`, `unconfigured`.
- `used` / `limit` / `remaining` are pointer fields and may be absent; do not assume presence.
- When `official_query_failed` is `true`, the primary numbers came from the local fallback and should be labeled as such.
- `reset_at` / `auto_reset` describe the local cycle; `source=official` means the provider's response takes precedence.
- This endpoint may wait on official endpoints for up to about 12 seconds; the frontend should use a 15-second timeout and render the old snapshot first.

`quotas/refresh` returns:

```json
{ "items": [ /* Item[] */ ], "refreshed": true }
```

`quotas/reset` returns `{ "reset": ["tavily"] }`; `quotas/adjust` returns `{ "adjusted": "tavily", "set_used": 120 }`.

### 3.4 Settings

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `settings` | read | Running values, disk values, field options, locks, backups, write capability |
| POST | `settings` | admin | Preview or save settings; supports backup restore |

Core GET fields:

| Field | Meaning |
|---|---|
| `values` | Current **running** values keyed by dotted path, such as `mode`, `cache.enabled` |
| `options` | Allowed values for enum fields |
| `saved_values` | Values saved on disk |
| `pending_changes` | Differences between disk and running values (restart pending) |
| `locked_fields` | Field → reason it cannot be edited (environment override, read-only file, etc.) |
| `field_files` | Field → owning configuration file (main config or `dashboard.yaml`) |
| `revision` | Configuration content revision; send it back when saving |
| `backups` | `[{ "name": "...", "created_at": "..." }]` |
| `credential_status` | `CredentialView`, see 3.5 |
| `write_access` | `{ "allowed": bool, "reason": string }`; only says the current source is eligible |
| `local_action_allowed` | Whether the current source passes the operation boundary |
| `config_error` | Explanation when reading/parsing the configuration file fails |

POST body:

```json
{
  "changes": { "mode": "hybrid", "cache.enabled": true },
  "revision": "configuration revision",
  "confirm": false,
  "restore_backup": ""
}
```

- `confirm:false` previews and validates only and returns `applied:false`; `confirm:true` persists.
- A non-empty `restore_backup` restores editable fields from that backup, still preview first.
- Response:

```json
{
  "applied": true,
  "restart_required": true,
  "backup": "settings-20261127T120000.000000000Z-abc.yaml",
  "changes": { "mode": "hybrid" },
  "revision": "new revision",
  "field_files": { "mode": "config.yaml" }
}
```

- `409` means the `revision` is stale; GET again, compare, then save. Do not overwrite.
- If `restart_required:true`, call `restart` and wait for the service to return. A successful save alone does not mean the running values changed.
- Preview and save must use the same `changes` and `revision`.

### 3.5 Credentials

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `secrets` | read | Effective credential counts, sources, and slot labels; values are never returned |
| POST | `secrets` | admin | Preview/save append, replace, delete, clear, or restore configuration source |

GET response:

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
  "revision": "credential file revision"
}
```

Available credential names:

`BAIDU_SK`, `TAVILY_SK`, `EXA_API_KEY`, `ANYSEARCH_API_KEY`, `DOUBAO_SEARCH_API_KEY`, `TENCENTCLOUD_WSA_APIKEY`, `TINYFISH_API_KEY`, `KEENABLE_API_KEY`, `GITHUB_TOKEN`, `JINA_API_KEY`, `MINERU_TOKEN`, `LLM_API_KEY`, `SEMANTIC_SCHOLAR_API_KEY`, `OPENALEX_API_KEY`, `UNPAYWALL_EMAIL`.

`WEBSEARCH_TOKEN` is read-only and cannot be changed from the control center.

POST body:

```json
{
  "name": "TAVILY_SK",
  "action": "append",
  "value": "tvly-...",
  "label": "primary",
  "slot_id": "",
  "revision": "credential file revision",
  "confirm": false
}
```

Actions:

| `action` | Applies to | Required | Meaning |
|---|---|---|---|
| `append` | multi-key credentials | `value` | Append to the pool, preserving existing slots |
| `set` | single-value credentials | `value` | Set the only value, overriding the config source |
| `replace` | multi-key credentials | `value`, `slot_id` | Replace the selected slot |
| `delete` | multi-key credentials | `slot_id` | Delete the selected slot |
| `label` | multi-key credentials | `slot_id`, `label` | Change the label only |
| `clear` | all | none | Hide config/environment sources and clear overlay slots |
| `reset` | all | none | Restore config/environment sources |

- `confirm:false` returns `saved:false`, `applied:false` for a before/after count preview; `confirm:true` writes the file.
- The response includes `before_count`, `after_count`, `backup`, `restart_required`, and `revision`.
- `409` means the credential file changed externally or the slot moved; GET again before retrying.
- When `restart_required:true`, the new value enters the running configuration only after a restart.

### 3.6 Diagnostics

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `diagnostics/proxy` | action | TCP probe of the configured proxy endpoint |
| POST | `diagnostics/everything` | action | Re-probe the Everything HTTP Server |
| POST | `diagnostics/search` | action | Single-source test search; may consume provider quota |
| POST | `diagnostics/fetch` | action | Single-URL test fetch; may call remote fetch services |

Test search body:

```json
{ "source": "tavily", "query": "mcp search", "confirm": false }
```

Sources with `diagnostics.sources[].confirm=true` require `confirm:true`. Test fetch body:

```json
{ "url": "https://example.com/article", "confirm": true }
```

Response:

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

Test fetch adds `fetch`:

```json
{ "fetch": { "node": "local", "attempts": ["local", "jina"], "body_status": "ok", "characters": 12000, "remote_redirects": false } }
```

Diagnostics allow one concurrent run globally and return `429` when busy. Results are for manual troubleshooting only; they do not represent the full retry chain of a real search route.

### 3.7 Runtime maintenance

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `sources/cooldowns/release` | action | `{ "engine": "tavily" }` or `{ "engine": "all" }`; release routing cooldowns |
| POST | `cache/clear` | action | `{ "confirm": true }`; clear the search cache |
| POST | `history/clear` | action | `{ "confirm": true }`; clear local search history |
| POST | `fetchdata/clear` | action | `{ "confirm": true, "scope": "expired" }` or `scope:"all"` |
| POST | `restart` | admin | `{ "confirm": true }`; asynchronous restart, returns 202 |
| GET | `everything/scope` | admin | Everything endpoint and complete allowed-root list |

Responses:

- Release cooldowns: `{ "cleared": 2 }`
- Clear cache/history/temp files: `{ "cleared": 123 }`
- Restart: HTTP 202 `{ "restarting": true }`; poll `overview.runtime.started_at` and confirm it changed before declaring the configuration effective.
- Everything scope: `{ "url": "http://127.0.0.1:4180", "roots": ["D:/data"] }`

### 3.8 Brand and metrics

`GET brand/logo` returns only a local image file; it 404s when `dashboard.brand.logo` is an HTTP(S) URL (the page references it directly, but the built-in page is blocked by CSP from loading remote images).

`GET __admin/api/metrics` returns Prometheus text with in-process passive counters. When the control center is disabled this route is absent, but the main `/metrics` endpoint remains available; see [api.en.md](api.en.md#observability-endpoint).

---

## 4. External UI bootstrap

### 4.1 Optional injected globals

If `index.html` contains:

```html
<script id="websearch-bootstrap"></script>
```

the server replaces it with a nonce-bearing inline script that injects:

| Global | Content |
|---|---|
| `window.__DEFAULT_LOCALE__` | Negotiated `zh` / `en` from `Accept-Language` |
| `window.__I18N_CATALOGS__` | `{ zh: {...}, en: {...} }` for the `web` domain |
| `window.__BRAND__` | `{ title, theme, accent, logo, footer }` |
| `window.__I18N__` | The current language catalog selected from local preference |

The script also restores theme, density, language, and brand overrides from `localStorage`, and sets `<html data-theme>`, `data-brand`, and related attributes. Using these globals is optional; the UI works without them.

If `index.html` lacks the placeholder, the server returns it unchanged, injects nothing, and does not modify `<html lang>`.

### 4.2 Progressive rendering and debouncing

The backend has no SSE endpoint; data is pulled with GET. Recommended UI behavior:

1. Show a skeleton or the previous snapshot on first load.
2. On success, replace it with the new snapshot; on failure, keep the old snapshot and show a "data may be stale" note.
3. Debounce filter input for 150–300 ms and abort the previous request when filters change (`AbortController`).
4. Do not poll faster than every 5 seconds; pause while the page is hidden.
5. Share one in-flight `overview` request across components instead of refetching on every render.

```js
let snapshot;
let controller;
let timer;

function render(data, stale = false) {
  // Render data; mark it stale when needed
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

### 4.3 Admin password entry

Do not persist the password. Keep it in memory while an administrative action is open, and clear it on failure:

```js
let admin = null; // { password, username }

async function adminRequest(path, body) {
  if (!admin?.password) throw new Error('Admin password required');
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

Security notes:

- Keep the password in memory only; a page refresh clears it.
- Never write it to URLs, `localStorage`, `sessionStorage`, logs, or error reporting.
- Do not auto-retry after 403; ask the user to re-enter it.
- Preview and save must use the same `changes` and `revision`.

### 4.4 Settings save flow

```text
GET settings
  → user edits a copy of values
  → POST settings { changes, revision, confirm:false }   // preview
  → show before/after differences
  → POST settings { changes, revision, confirm:true }    // save
  → restart_required ? POST restart { confirm:true } : done
  → poll overview until runtime.started_at changes and saved_values are effective
```

On `409`, GET settings again, compare the user's edits with the latest values, and continue. Do not blindly overwrite.

---

## 5. Versioning and compatibility

- The API base is fixed at `/__admin/api/`; the current version is declared by `X-WebSearch-Dashboard-API: 1`.
- Within a major version, changes are additive: new fields, endpoints, and enum values. External UIs should ignore unknown fields and enum values.
- Removing fields, changing types or semantics, or changing authentication requirements is breaking and bumps the major version, documented in [CHANGELOG](../CHANGELOG.md).
- An external UI should check the response header at startup and show a clear message on mismatch instead of failing silently.
- There is no OpenAPI document; this guide and the handlers in `pkg/dashboard/` are the source of truth. If they disagree, the code wins; submit a documentation fix.

---

## 6. Self-test checklist

- [ ] `/dashboard/` returns the external `index.html`; static assets return 200; dotfiles such as `.env` return 404.
- [ ] No CSP violations in the browser console; `fetch('/__admin/api/overview')` succeeds.
- [ ] On refresh, old data renders first and is replaced when new data arrives; on failure, old data remains with a stale note.
- [ ] Continuous filter input triggers one request; a fast switch cannot let an old response overwrite a new one.
- [ ] An admin action without a password returns 403; the correct password succeeds.
- [ ] `GET settings` `revision` is sent on save; a simulated concurrent edit returns 409 and the UI reloads.
- [ ] When save returns `restart_required:true`, restart and confirm `runtime.started_at` changed.
- [ ] A missing `index.html` in the external directory returns 500 and does not fall back to the built-in page.
- [ ] A relative `dashboard.ui.dir` resolves against the main `config.yaml` directory.
- [ ] The page is usable at mobile width; wide tables scroll horizontally.

---

## 7. Source index

| Concern | File |
|---|---|
| Route registration, auth, overview/settings/quotas | `pkg/dashboard/handler.go` |
| External UI selection, static serving, CSP, bootstrap | `pkg/dashboard/assets.go` |
| Settings file read/write and revision | `pkg/dashboard/control_settings*.go` |
| Credential slot editing | `pkg/dashboard/credentials.go`, `pkg/config/credential_editor.go` |
| Source/overview views | `pkg/dashboard/sources.go`, `pkg/dashboard/runtime.go` |
| Calls and request events | `pkg/telemetry/store.go` |
| Quota model | `pkg/quota/quota.go` |
| Diagnostics | `pkg/dashboard/diagnostics.go` |
| Configuration | `pkg/config/config.go`, `dashboard.example.yaml` |
