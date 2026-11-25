# PDF 解析多供应商：LlamaParse 兜底与额度冷却

状态：草案（未实现）
日期：2026-11-25

## 1. 定位

LlamaParse 作为 MinerU 限流或额度不足时的第二云端解析供应商，默认关闭。首期只覆盖远程 PDF URL；本地整本精准归档继续走 MinerU，本地文本路径继续走 ledongthuc / PDFium。

它解决的是可用性问题：MinerU 返回 429、队列满或额度耗尽时，`pdf_parser` 仍有一次云端解析机会，而不是直接降级到质量较差的本地文本。LlamaParse 不替代 MinerU 的主力位置，也不参与本地归档产物（ZIP、图片、任务断点）的生成。

## 2. 供应商能力与成本

| 项 | LlamaParse |
|---|---|
| 免费档 | Pay as you go，$0/月，10,000 credits/月，组织级共享，月底清零 |
| 超额 | 按 $1.50/1k credits 购买；Pro $500/月含 400K credits |
| 并发 | 方案表 5 个 Parse 任务；Pro 20，Enterprise 100 |
| 每页 credits | `fast` 1、`cost_effective` 3、`agentic` 10、`agentic_plus` 25 |
| 缓存 | 相同请求 48 小时内命中缓存不计费 |
| 单任务限制 | 512MB、10,000 页、130+ 格式 |
| 远程输入 | `POST /api/v2/parse` 支持 `source_url` 或 `file_id`，二选一 |
| 本地输入 | `POST /api/v2/parse/upload`，multipart 的 `file` + `configuration` JSON |
| 结果 | `GET /api/v2/parse/{job_id}?expand=markdown_full,usage` |
| 版本 | v2 必填 `tier` + `version`；生产固定日期版本，`latest` 只用于联调 |
| 限流 | 创建任务 500 次/10s，查询 80 次/s；429 不带 `Retry-After` |

按 `pdf_parser.max_pages` 默认 20 页/次折算：

| 档位 | credits/页 | 10K 免费≈页数 | 20 页/次≈调用次数 |
|---|---:|---:|---:|
| `fast` | 1 | 10,000 | 500 |
| `cost_effective` | 3 | ~3,333 | ~166 |
| `agentic` | 10 | 1,000 | 50 |
| `agentic_plus` | 25 | 400 | 20 |

免费额度按 `agentic` 折算约 1,000 页/月。它适合做兜底，不能承担高频主力流量。

## 3. 额度查询：能查到什么，查不到什么

截至 2026-11-25，LlamaCloud 的公开 OpenAPI（`https://api.cloud.llamaindex.ai/api/openapi.json`）提供 Beta 用量接口，但没有“剩余 credits / 余额”端点。

| 能力 | 端点与字段 | 结论 |
|---|---|---|
| 已消耗 credits 聚合 | `GET /api/v1/beta/usage-metrics/aggregate`，必填 `organization_id`、`group_by`，可选 `project_id`、`day_on_or_after`、`day_on_or_before`、`event_types`；响应 `buckets[].total_credits` | 可查（Beta） |
| 用量明细 | `GET /api/v1/beta/usage-metrics`，返回 `UsageMetric.credits`、`value`、`day`、`event_type`、`project_id`、`organization_id` | 可查（Beta，分页） |
| 单任务实际 credits | `GET /api/v2/parse/{job_id}?expand=usage`，返回 `job.usage.credits` | 可查，但任务完成初期可能为 `null` |
| 剩余余额 | OpenAPI 无对应端点；`Organization` / `Project` schema 没有 credits 字段 | 查不到 |
| 套餐额度、购买 credits、重置日期 | UI 的 Plan & Credits 可见，API 未暴露 | 查不到 |

因此额度判断采用“本地账本为主，用量接口校准”：

```text
remaining_estimate = configured_monthly_budget
                   - month_to_date_estimated_or_actual_consumed
```

- `configured_monthly_budget` 由配置给出，免费档默认 10,000；Pro 或购买过 credits 的账号由使用者调高。
- 本地账本按 `organization_id + project_id + 月份` 记录预估和实际 credits。
- 用量接口返回的 `total_credits` 用来校准本地账本；它是 Beta 接口，可能滞后，不能作为唯一依据。
- 如果同一组织还有其他产品在消耗 credits，需按 `project_id` 或 `event_types` 过滤；具体事件类型与 Parse v2 的映射待实测。
- 已购买 credits 在 API 中不可见。账号可能因购买额度而比本地估算更充足；也可能因其他应用消耗而更少。两种情况都以 402 作为最终兜底。

`organization_id` 可从 `GET /api/v1/projects` 返回的 `organization_id` 获取，也可以直接配置。

## 4. 冷却状态机

LlamaParse 供应商维护三个状态：

| 状态 | 进入条件 | 行为 | 退出 |
|---|---|---|---|
| `AVAILABLE` | key 已配置，`remaining_estimate >= estimate + reserve` | 允许提交解析任务 | 余额不足或 402 → `COOLDOWN`；401/配置错误 → `DISABLED` |
| `COOLDOWN` | 预估余额不足、402、或连续任务失败 | 跳过 LlamaParse，按回退链走 MinerU 或本地；到点后允许下一次真实请求重新估算 | 估算通过 → `AVAILABLE`；仍不足 → 延长冷却 |
| `DISABLED` | 401、key 缺失、参数错误 | 不参与回退链 | 修改配置并重启/重载 |

### 4.1 提交前估算

```text
estimate = pages * tier_rate
if remaining_estimate < estimate + reserve_credits:
    enter_cooldown()
    return ErrInsufficientCredits
```

- `pages` 取本次 `page_ranges` 的页数；省略页码时取 `max_pages`。
- `tier_rate` 按当前配置档位取 1 / 3 / 10 / 25。
- `reserve_credits` 是每次调用保留的安全余量，默认 100，避免一次任务把余额打到 0。
- 单次页数受 `max_pages_per_call` 约束，默认 50。50 页在 `agentic` 下是 500 credits，仍留出足够余量。

### 4.2 冷却时长

- `cooldown_initial` 默认 30 分钟，与现有搜索供应商的 `provider.KeyQuotaCooldown` 对齐；配额通常按日/月恢复，短冷却会让已耗尽额度被反复打上游。
- 每次在冷却到期后仍判定不足，冷却时长翻倍，上限 `cooldown_max` 默认 6 小时。
- 任意一次成功提交并完成解析后，冷却时长重置为初始值。
- 到期后不主动发“探测任务”；下一次真实 `pdf_parser` 请求先做估算，够则提交，不够则继续冷却。这样可以避免探测本身消耗 credits。
- 402 按“余额不足”处理，不重试。401 直接进入 `DISABLED`，不参与回退。

### 4.3 与 MinerU 限流的配合

```text
MinerU 精准解析
  ├─ 成功 → 返回
  ├─ 429 / busy / queue full / quota exhausted
  │    ├─ LlamaParse AVAILABLE → LlamaParse
  │    ├─ LlamaParse COOLDOWN/DISABLED → 本地文本 / PDFium
  │    └─ LlamaParse 失败 → 本地文本 / PDFium
  ├─ page limit / source URL rejected → 现有 qpdf 裁切与分批链路
  └─ 其他失败 → 本地文本 / PDFium
```

首期只把 MinerU 的限流和额度耗尽作为 LlamaParse 的触发条件。页数超限和源 URL 拒绝继续走现有裁切链路，避免一次改动同时改变两条回退路径。

LlamaParse 的 `source_url` 由 LlamaCloud 抓取，本地的 URL 安全校验约束不到对方网络。只传已通过本地校验的公网 URL；需要鉴权、内网或来源不明的 PDF，下载后走上传接口。

### 4.4 并发与刷新

- LlamaParse 最多 5 个并发 Parse 任务，用 provider 级 semaphore 限制。
- 用量接口刷新做 single-flight，默认 10 分钟一次；冷却到期时强制刷新一次。
- 用量接口无权限或调用失败时，退回本地账本判断，不阻塞解析；失败原因记一次告警。
- 本地账本写入与刷新串行化，避免并发请求同时读到旧余额。
- `usage.credits` 可能延迟为 `null`：先按 `estimate` 记账，拿到实际值后再修正差额。

## 5. 接入设计

### 5.1 配置草案

```yaml
pdf_parser:
  fallback:
    enabled: true
    trigger_on_mineru_rate_limit: true
  llamaparse:
    enabled: false
    api_key: ""                 # 或 LLAMA_CLOUD_API_KEY
    organization_id: ""         # 用量查询用；可从 projects 接口获取
    tier: cost_effective
    version: "latest"           # 生产固定日期版本
    max_pages_per_call: 50
    monthly_credit_budget: 10000
    reserve_credits: 100
    cooldown_initial: 30m
    cooldown_max: 6h
    usage_refresh: 10m
```

配置键在实现时按仓库现有风格落到 `PDFParserConfig`；没有 key 时保持现有行为。

### 5.2 代码落点

- 新增 `pkg/fetch/llamaparse`：REST 客户端、任务轮询、错误分类、用量查询、额度估算。
- `pkg/fetch/webfetch`：供应商回退链与 provider 级 semaphore；`ParseEngine` 增加 `llamaparse-remote`。
- `mcp/tool_pdf.go`：MinerU 限流哨兵错误触发 LlamaParse；`task_ref` 增加 `llamaparse:` 命名空间。
- `pkg/config/config.go`：配置结构、默认值、校验。
- `pkg/cache` 或独立 SQLite 表：月度 credits 账本。
- i18n：新增 `fetch.llamaparse.*`、`log.fetch.llamaparse.*` 和工具描述 key。

### 5.3 错误映射

| 上游结果 | 处理 |
|---|---|
| 401 | 标记 `DISABLED`，记录配置错误，走其他供应商 |
| 402 / `You've exceeded the maximum number of credits` | 进入 `COOLDOWN`，不重试，走其他供应商 |
| 413 | 文件过大；不冷却，按本地文件上限处理 |
| 422 | 参数/配置错误；不冷却，暴露错误 |
| 429 | 请求级指数退避；不进入长冷却 |
| 5xx / 传输错误 | 有限重试后回退；连续失败可短暂冷却 |
| job `FAILED` | 回退；若错误消息含 credits 关键词，按 402 处理 |
| job `CANCELLED` | 按调用方取消处理，不计供应商故障 |

## 6. 非目标

- 不做 LlamaParse 本地整本归档；ZIP、图片和断点继续由 MinerU 提供。
- 不做 LiteParse 本地 sidecar；当前 ledongthuc + PDFium 已覆盖本地文本路径。
- 不做 PDF 供应商 round-robin；按页计费且输出形态不同，并发打多家会浪费额度并产生不一致结果。
- 不默认开启 LlamaParse auto-reload；额度耗尽时冷却并回退，产生费用需要使用者显式配置。

## 7. 安全与数据边界

- 远程 `source_url` 意味着 PDF 由 LlamaCloud 抓取；本地 SSRF 防护只能约束提交前的 URL 选择，不能约束对方网络的最终解析。
- 本地文件上传会把内容送到 LlamaCloud。官方 FAQ 称文件缓存 48 小时后永久删除；敏感文件可用 `disable_cache: true`，但会失去缓存免费额度。
- 上传前仍执行现有大小限制和路径校验；LlamaParse 的 512MB 上限不自动放宽本服务的抓取上限。
- 云端解析失败时保留原始错误摘要，便于区分限流、额度、文件格式和网络问题。
- 错误摘要走 `pkg/redact` 脱敏后再写入日志或 telemetry，避免 key 或上游凭据出现在错误文本里。

## 8. 验收标准

- MinerU 返回 429/busy/queue full 时，一次请求内切换到 LlamaParse；LlamaParse 不可用时回退本地文本。
- LlamaParse 预估余额不足时不发请求，直接进入冷却并回退；冷却到期后下一次真实请求重新估算。
- LlamaParse 返回 402 时不重试，冷却时长按指数增长到上限。
- 401 后不再调用 LlamaParse，直到配置重载。
- 并发请求共享同一份额度账本和 single-flight 刷新，不出现同时超额提交。
- 单元测试覆盖假时钟、假用量接口、402/401/429、冷却延长与成功重置。
- 真实环境验证：用量接口权限、`organization_id` 获取、`total_credits` 口径、`source_url` 抓取失败分类、`usage.credits` 延迟。

## 9. 待实测问题

1. 普通 API key 是否有权限调用 `/api/v1/beta/usage-metrics/*`，还是需要组织管理员权限。
2. `total_credits` 是否包含所有 Parse 事件；同组织有其他产品时如何按 `event_types` 精确过滤。
3. 月度重置的具体时区；API 未暴露重置日期。
4. 购买 credits 后剩余额度无法从 API 获取，本地账本如何配置更准确。
5. `usage.credits` 从 `null` 变为实际值的延迟窗口。
6. 402 的完整错误消息和 job `FAILED` 场景下的额度错误特征。
7. `source_url` 被目标站反爬、鉴权或地域限制时的错误分类。
8. 5 并发与 429 在真实账号上的表现；是否需要更保守的本地并发上限。

## 10. 参考

- LlamaParse 定价：https://www.llamaindex.ai/pricing
- LlamaParse 计费与额度：https://developers.llamaindex.ai/llamaparse/general/billing/
- LlamaParse 价格明细：https://developers.llamaindex.ai/llamaparse/general/pricing/
- LlamaParse REST API：https://developers.llamaindex.ai/llamaparse/parse/guides/api-reference/
- LlamaParse 文件与请求限制：https://developers.llamaindex.ai/llamaparse/general/limitations/
- LlamaParse 限流：https://developers.llamaindex.ai/llamaparse/general/rate_limits/
- LlamaCloud OpenAPI：https://api.cloud.llamaindex.ai/api/openapi.json
- LiteParse：https://github.com/run-llama/liteparse
- 现有 MinerU 接入：`pkg/fetch/mineru/client.go`、`pkg/fetch/webfetch/webfetch.go`、`mcp/tool_pdf.go`
