# Firecrawl Local 抓取层优化项（调研与待办）

> 对象：`firecrawl/apps/local`（Rust crate `firecrawl-local` 0.1.1，AGPL-3.0，2026-11-18 发布）。
> 本文只定义优化项、改动边界与验收标准，不含代码实现；状态：待评审。
> 结论：37x / 230x 的对比基线是五容器自托管栈（api + playwright + redis + rabbitmq + postgres），README 未给基准方法；本项目已是单 Go 二进制，参考重点是抓取质量门控、本地渲染降级与资源边界，语言重写不在范围内。

## 1. 参考对象速览

Firecrawl Local 的定位是“一台机器上的抓取原语”：

| 维度 | 事实 |
|---|---|
| 发布形态 | 单个 Rust 二进制，`cargo install firecrawl-local`，无数据库、队列或容器 |
| 本地能力 | scrape / crawl / map / batch scrape / PDF 与 Office 文档解析 |
| 浏览器 | 复用系统 Chrome/Chromium；不存在时静态页仍可用，JS 页返回 warning |
| 云端边界 | search、被墙页回退、Alexandria 默认走 Firecrawl Cloud；`FIRECRAWL_CLOUD=off` 可全关，但 search 失效 |
| MCP 方式 | `firecrawl-local mcp` 先起本地 API，再用 npx 运行 `firecrawl-mcp` 指向它；Rust 重写的是抓取引擎，MCP 层仍是 Node 包 |
| 许可 | AGPL-3.0 |

关键源码：

- `apps/local/src/scrape.rs`：抓取编排、质量判定、正文清洗兜底
- `apps/local/src/browser.rs`：Chrome 池、settle 判定、JS 壳识别
- `apps/local/src/fetch.rs`：HTTP 抓取、DNS/重定向守卫、body 上限、重试
- `apps/local/src/crawl.rs`：frontier/visited、robots、sitemap、并发
- `apps/local/src/guard.rs` / `netguard.rs`：私网/元数据地址策略与浏览器守卫代理
- `apps/api/native/src/html.rs`：与云端共用的清洗、元数据、链接提取

口径提醒：官方“11 MB idle”未说明是否包含 `mcp` 模式拉起的 Node 子进程，也不代表本项目目标值；本项目按 Go 进程自身 RSS 建立基线。

## 2. 与本项目的对照

| 能力 | Firecrawl Local | 本项目现状 | 差距/机会 |
|---|---|---|---|
| 静态页抓取 | 本地 HTTP + 原生清洗 | go-webfetch + readability + html-to-markdown | 清洗规则、体积边界 |
| JS 页 | 本地 Chrome，auto/never/always | 无浏览器层；Jina（Key+代理）或供应商正文 | 零 Key 本地渲染缺口 |
| 结果质量 | visible_chars / link_text_share / needs_browser / bot check | `HasPageBody` 只判空 | 壳页会被当成功 |
| 重试 | 瞬时网络错误 1 次、250ms | 3 次、2/4/5s 退避 | 失败页降级慢 |
| body 上限 | 32MB 流式中止 | `MaxBodyBytes` 未使用，`io.ReadAll` | 内存风险 |
| 连接池 | idle 60s / 64 per host / tcp_nodelay | `defaultBaseTransport` 未设 per-host | 并发建连 |
| 清理 | 空闲归还内存（mimalloc） | 无 | Go 可用 FreeOSMemory |
| 站点级工具 | crawl/map | 无 | 本轮不做，仅留设计参考 |

## 3. 优化项

### F1 内容质量门控与降级（P0，S/M）

**现状**

- `pkg/fetch/content.go:11` 的 `HasPageBody` 只跳过空行、标题行和 skip-to-content；只有导航链接的 SPA 壳会返回 true。
- `mcp/tool_cleanfetch.go:284` 的本地链路和 `mcp/tool_search.go:590` 的 `fetch_top_n` 都只判空。
- `mcp/tool_cleanfetch.go:333` 的 `allowFetchFallback` 对 WAF/challenge 直接返回 false，不尝试 Jina/远端。
- 结果：agent 可能把导航壳、Cloudflare 200 验证页当成正文，后续自行重试，放大延迟和 token。

**参考做法**

- `browser.rs` 的 `needs_browser`：正文不足 1500 非空白字符 + 有 script + 命中 “enable javascript”/框架挂载点，或正文占 HTML 不足 1% 且 HTML 超过 20KB。
- `browser.rs` 的 `looks_like_nav_only_shell`：正文 1500–12000 字符 + 框架标记 + 链接文字占比超过 60%。
- `scrape.rs` 的 `visible_chars` 与渲染采纳阈值：挑战页要求渲染正文大于 max(plain, 200)，壳页要求不低于 plain 的 80%。
- `cloud.rs` 的 `looks_like_bot_check`：正文不足 2000 字符时匹配 “just a moment” / “verifying you are human” / “captcha” 等。

**改动细项**

1. 新增 `pkg/fetch/quality.go`，纯函数、无 I/O：
   - `VisibleChars(markdown) int`
   - `LinkTextShare(markdown) float64`（解析 Markdown 链接，URL 不计入可见文字）
   - `LooksLikeBotCheck(markdown) bool`（长度门槛 + 中英关键词表）
   - `LooksLikeNavOnlyShell(markdown, rawHTML) bool`（rawHTML 可选；无 rawHTML 时退化为长度 + 链接占比）
   - `NeedsBrowser(rawHTML, markdown) bool`（框架挂载点、script、正文/HTML 比例）
2. 给 `webfetch.Result` 增加质量诊断字段（建议独立结构体 `Quality`，避免把整页 rawHTML 常驻内存）：`VisibleChars`、`LinkTextShare`、`NeedsBrowser`、`BotCheck`。上游 go-webfetch 在解析前保留必要的 HTML 片段或统计值即可。
3. `cleanfetch` 单条链路：抓取成功后先过质量门控。
   - BotCheck → 返回 `errcode.Challenge`，复用现有反爬文案与遥测分类。
   - NeedsBrowser / NavOnly → 按 `mcp_hub.fetch_mode` 尝试 Jina/远端；全部失败时返回正文 + i18n warning，明确“正文可能不完整”。
   - 正常正文 → 行为不变，保证零额外网络请求。
4. `fetch_top_n`：质量不合格的条目不再静默当成功；按现有 `annotateFetchFailure` 标注，或在预算内尝试下一个 provider。
5. 新增 i18n key（`tool.cleanfetch.*` / `tool.search.*`），中文/英文目录同步；错误码沿用 `errcode.Challenge` / `errcode.NoResult`。

**验收**

- 固定 fixture：Next.js/React 壳、导航壳、Cloudflare 200 挑战页、正常长文、短文档页、链接列表页（Wikipedia 类）。
- 壳页在 `cleanfetch` 中不再作为成功正文返回；`fetch_top_n` 条目带明确 warning 或降级结果。
- 正常页零额外请求、输出不变；短文档页不误判（阈值有测试钉住）。
- 新增表驱动测试 + 至少一条真实快照回归。

**风险**

- 误判短正文/链接列表页；阈值取保守值，并允许按配置关闭（若新增配置，按 AGENTS.md 同步两份 example 与 docs）。
- 质量门控需要 rawHTML 时注意大页面内存；只保留统计值和前 N KB 片段。

### F2 抓取层硬修复（P0，S）

**现状**

- HEAD 预检：`mcp/security.go` 的 `headCheck` 对每个 URL 先发 HEAD；`pdfHTTPClient` 每次新建 transport 且 `DisableKeepAlives=true`，等于每页多一次 TLS 握手 + RTT。
- 体积上限：go-webfetch 的 `Config.MaxBodyBytes`（`webfetch.go:35/66`）只定义未使用；`parse/router.go` 的 HTML/JSON/text/PDF 分支都是 `io.ReadAll`。
- 重试：默认 3 次、2/4/5s 退避（`pkg/config/config.go:1433`），失败页最坏多等约 11s 才降级；证书错误也会重试。

**改动细项**

1. **HEAD 预检**
   - 方案 A（推荐）：移除 `headCheck` 的常规调用，保留 SSRF 预检；下载层用流式上限兜底。大文件最多多读 N 字节，省掉每页一次握手。
   - 方案 B：保留 HEAD，但复用带连接池的共享 client，去掉每次新建 transport / `DisableKeepAlives`；仅对 `Content-Length` 已知且超限的 URL 早拒。
   - PDF 下载路径单独评估：PDF 允许更大上限，继续保留 `Content-Length` 早拒。
2. **流式 body 上限**
   - 在 `parse/router.go` 各分支统一走 `io.LimitReader(resp.Body, max+1)`；超限返回稳定错误码（建议新增 `errcode.TooLarge` 并补 telemetry 映射与中英文案，或复用 `errcode.Blocked`）。
   - 读取前检查 `Content-Length` 超过上限直接拒绝；chunked 走流式计数。
   - 上限取 `cleanfetch.max_fetch_size_mb`（默认 10MB），PDF 路径沿用现有独立上限。
3. **重试策略**
   - 仅对瞬时网络错误（连接重置、EOF、DNS 临时失败）重试 1 次，间隔 250ms。
   - 429/502/503 读取 `Retry-After`，在预算内重试；证书错误、超时、SSRF/策略拒绝不重试。
   - 总抓取预算与 cleanfetch 现有超时/`mcp_hub` 预算对齐，避免层层重试叠加。

**验收**

- 单页抓取的 HEAD 请求数为 0（或复用同一连接）；固定站点 p50 延迟下降，跨境样本收益更明显。
- 1GB chunked 响应在 N+1 字节处中止，进程 RSS 不随响应体线性增长。
- 不可达/证书错误页面在约 1s 内失败并降级，不再等约 11s。

**风险**

- 去掉 HEAD 会增加大文件下载量；流式上限保证有界。若线上大文件占比高，保留方案 B。
- 重试收敛可能降低弱网成功率；用真实网络回归对比，必要时按错误类型配置。

### F3 本地 Chrome 渲染层（P2，L；或 provider 方案 M）

**现状**

- 仓库未引入 chromedp/rod/playwright。
- JS 页两条路：Jina Reader（`pkg/fetch/jina/reader.go`，需要 API Key + 代理）或 API 供应商自带正文；两者不可用时 cleanfetch 只能返回壳或报错。
- 与“零 API Key 本地运行”的定位存在缺口。

**参考做法**

- `browser.rs`：系统 Chrome 发现（含 `FIRECRAWL_CHROME_PATH`）、懒启动、headless、临时 profile、进程复用、页面信号量 6。
- `settle`：readyState=complete + fetch/XHR 在途数（超过 1.5s 的长连接不计）+ DOM 500ms 无变更 + 文本长度稳定；空文本最多等 3s；滚动默认关闭。
- `scrape.rs`：渲染结果采纳阈值（挑战页大于 max(plain, 200)；壳页不低于 plain 的 80%；否则保留 plain）。
- `netguard.rs`：Chrome 所有连接走本地 HTTP 代理，逐连接检查 IP；重定向、子资源、WebSocket 都覆盖；`guard.rs` 永远阻断 link-local/云元数据。

**方案 A：自建 chromedp（产品化路线）**

1. 配置：`cleanfetch.browser: auto|never|always`（默认 auto；Chrome 不存在时等价 never 并 warning）。新增配置按 AGENTS.md 同步两份 example 与 docs。
2. Chrome 发现：Windows/macOS/Linux 常见路径 + `PATH` + 环境变量覆盖；不下载浏览器。
3. 进程：懒启动、headless、临时 profile、退出清理；页面并发 4–6；参考启动超时 20s、请求超时 45s，最终按现有超时预算调整。
4. settle：注入 fetch/XHR 计数器 + MutationObserver；CDP 网络事件；文本稳定 2 个采样周期；空文本 3s 预算。
5. 触发：仅在 F1 判定为壳/挑战时触发；`always` 用于调试；`never` 完全关闭。
6. 采纳：渲染正文不低于 plain 的 80%，或 plain 是 nav-only；挑战页要求渲染正文大于 max(plain, 200) 且非 bot check；否则保留 plain 并 warning。
7. 安全：本地 CONNECT 代理逐连接检查解析后的 IP，链路上游代理时同时校验目标；永远阻断 169.254/100.100.100.200/link-local/metadata；测试覆盖重定向到内网、DNS rebinding、WebSocket。
8. 资源：页面信号量、浏览器空闲回收、进程崩溃重启；Windows 服务/无桌面会话下验证 headless。

**方案 B：接入 Firecrawl Local 作为可选 provider（先验证）**

1. 在 `remoteFetchChain` 增加 `firecrawl_local` provider；配置 `cleanfetch.firecrawl_local.endpoint`（默认空 = 关闭）。
2. 请求 `POST {endpoint}/v2/scrape`，body `{"url": "...", "formats": ["markdown"]}`，取 `data.markdown`；超时与现有远端一致。
3. 健康检查：启动时/首次使用时 `GET /health`；失败自动跳过，不影响本地链路。
4. 顺序：建议排在 Jina 之前、供应商 fetch 之后（本地优先，省外部额度）；可按 `fetch_mode` 配置。
5. 许可：独立进程 HTTP 调用；不拷贝 AGPL 代码进 MIT 仓库；文档明确安装前提（Rust 二进制 + Chrome）。

**验收**

- 方案 B：endpoint 未配置时行为零变化；配置后固定 JS 页面能拿到正文；endpoint 不可达时约 1s 内跳过并走原链路。
- 方案 A：JS 页面成功率提升；普通静态页不启动 Chrome（零额外内存）；渲染失败/低质时保留 plain；SSRF 用例全部阻断。
- 性能：记录首解/热解延迟、Chrome 常驻 RSS、并发 4/6 页时的 p95。

**风险**

- Chrome 体积/内存/启动成本；Windows 杀软与无桌面会话；AGPL；浏览器指纹与站点条款。
- 不建议默认对所有页面渲染；只在质量门控命中时触发。

### F4 清洗规则与单次解析（P1，M/L）

**现状**

- `parse/html.go:48` 的 `ProcessHTML` 至少 4 次解析：`preCleanHTML`、`anchorStructure`、`readability.FromReader`、`converter.ConvertString`；heuristic fallback 时第 5 次。
- `preCleanHTML`（`parse/html.go:130`）直接删所有 `header/footer/nav/aside/form`，article 内的标题 header 也会丢。
- class 子串匹配 `ad-` 会误删 `read-more` 这类元素；`-ad`、`advert` 等同样可能误伤。
- 没有正文丢失兜底，也没有首尾链接块/版权行的条件删除。

**参考做法**

- `scrape.rs` 的 `process_html`：一次 parse 后复用同一棵树做 metadata、links、cleaning、markdown；CPU-bound parse 用 `available_parallelism` 信号量。
- `keep_content_headers`：article/main 内的 header 含 h1–h3 且不含 nav 时，解包保留。
- `strip_page_furniture`：consent/cookie 块不足 2500 字符且不含 h1/article/main/table/pre 才删；小表单不足 500 字符、含控件、不含正文结构才删。
- `strip_edge_link_blocks`：页面有至少 2 段 60 字以上正文；块在正文前后；至少 3 个短链接且 80% 是链接文字；无标题引导；版权行不超过 200 字符。
- 正文丢失兜底：主内容规则把 2000 字以上段落删到不足 35% 时，回退到“只删 header/footer/nav/aside/[role=...]”重做。
- `html.rs` 的 `transform_document` 复用已解析文档；`markdown.rs` 处理代码块语言、表格、permalink、data URI。

**改动细项**

1. 修误删：class/id 按空白 token 匹配，去掉 `ad-`/`-ad` 子串规则；保留明确的 ad 容器选择器并加测试。
2. 保留内容 header：`article header` / `main header` / `[role=main] header` 含标题且无 nav 时解包。
3. consent/cookie：用明确选择器 + 体积/内容守卫，替换“删所有 form/header”。
4. 小表单：仅当不足 500 字符、含 input/select/textarea/button、且不含 h1/h2/article/main/table/pre 时删。
5. 首尾链接块与版权行：按 DOM 文本统计（非空白字符、链接文字占比、链接数、短链接）实现；先做保守阈值，配 fixture 调。
6. 正文丢失兜底：解析前统计 `<p>` 文本量；清洗后不足 35% 时重做（只删语义噪声标签）。
7. Markdown 后处理：链接标签内换行转义；删除 skip-to-content 链接；保留代码块语言。
8. 单次解析：第一阶段合并 `preCleanHTML` 与 `anchorStructure` 为一次 goquery 解析；第二阶段评估 fork readability 接受已解析 DOM，或用 `html.Parse` + 自研抽取。先 pprof 确认解析 CPU/alloc 占比，再决定投入。

**验收**

- fixture：article header 标题、cookie 横幅、小搜索表单、首尾链接块、版权行、`read-more` class、正文在 widget 内（兜底）、代码块/表格。
- 清洗后 markdown 在固定 corpus 上人工 diff；标题保留率、正文保留率、噪声删除率三项指标不回退。
- 解析次数：常规页从 4 次降到不超过 2 次（第一阶段目标），p50 解析耗时/alloc 有基准对比。
- 正常页面输出与现状兼容；不引入新的外部依赖。

**风险**

- 规则过严会删正文；所有规则加体积/结构守卫，并用 fixture 钉住。
- readability fork 维护成本高；先做低风险规则与解析合并，单次解析按 profile 决策。

### F5 长驻进程资源调优（P2，S/M）

**现状**

- 无 `debug.FreeOSMemory`；Go 堆在突发抓取后可能长期保持高位。
- `pkg/proxy/proxy.go:66` 的 `defaultBaseTransport` 未设 `MaxIdleConnsPerHost`（Go 默认 2）、`IdleConnTimeout`；多引擎并发打同一 host 会反复建连。
- 无 CPU-bound 解析并发上限；当前单批最多 5 个 URL，暂时够用。

**参考做法**

- `mem.rs`：空闲 2s 检查 in-flight=0 后 `mi_collect(true)`；`scrape.rs` 的 `trim_memory_when_idle` 只在有活动后触发。
- `fetch.rs`：connect 10s、pool idle 60s、64 idle/host、tcp_nodelay。
- `scrape.rs`：parse 信号量 = `available_parallelism`。

**改动细项**

1. 空闲归还：复用 `webfetch.go:659-676` 的 `inflight`/`lastActivity`；空闲 1–2 分钟后调用 `debug.FreeOSMemory()`；用 CAS 防止并发重复调用；只在有抓取活动后触发（dirty 标记）。
2. 连接池：`defaultBaseTransport` 增加 `MaxIdleConns=100`、`MaxIdleConnsPerHost=16–64`、`IdleConnTimeout=60–90s`、`MaxConnsPerHost` 按需；动态代理 transport 同样生效；用多引擎并发基准验证 p95 与 fd 数。
3. 解析并发：如果后续加 crawl 或批量抓取超过 10，增加 `runtime.NumCPU()` 信号量；当前不引入。
4. 可选：文档化 `GOMEMLIMIT`/`GOGC` 调优建议；不硬编码，避免 GC 抖动。

**验收**

- 突发 100 页后空闲 2 分钟，RSS 回落到基线附近（记录前后值）；STW 不影响进行中的请求。
- 多引擎并发（4 引擎以上）p95 延迟下降或持平，`TIME_WAIT`/新建连接数下降。
- 无请求时零额外 goroutine/定时器泄漏。

**风险**

- `FreeOSMemory` 有 STW 和缺页成本；只在空闲触发，先在生产同规模数据上测量。
- 连接池过大会占 fd；按平台默认 fd 上限设置并加监控。

## 4. 其它可选小项

- [ ] **M1 meta refresh 跟随**：HTML 响应中 `<meta http-equiv="refresh">`，delay 不超过 5s、最多 3 跳；相对 URL 解析后重新过 SSRF 检查。参考 `scrape.rs` 的 `meta_refresh_target`。
- [ ] **M2 成功响应结构化 warning**：JS 不完整、格式被跳过、正文截断等写入结果字段（不只是日志），并加 i18n key；参考 Firecrawl 的 `warning` 字段。
- [ ] **M3 batch URL canonical 去重**：`mergeFetchURLs` 目前只做精确字符串去重；去 fragment、尾斜杠、默认端口后再去重（复用 `core.NormalizeURL`/`antirobot.NormalizeURL`）。
- [ ] **M4 站点级 crawl/map 设计参考**：本轮不做；若未来立项，参考 `crawl.rs` 的 frontier/visited、canonical、并发 8（clamp 64）、robots crawl-delay（clamp 30s）、sitemap 上限 50、失败页不占 limit。

## 5. 明确不做

- **Rust/mimalloc 重写**：瓶颈在抓取质量与降级策略，单 Go 二进制已满足轻量定位；不引入 CGO/新分配器。
- **npx MCP 桥接**：`firecrawl-local mcp` 会拉起 Node 子进程；本项目原生 MCP 更轻，保持现状。
- **search 回云端**：本地多引擎搜索是差异点；不照搬 Firecrawl Local 的云端 search 默认。
- **AGPL 代码拷贝**：只借鉴算法与设计；不把 `apps/local` 代码复制进 MIT 仓库。方案 B 仅做进程隔离的 HTTP 调用。
- **默认全量浏览器渲染**：只在质量门控命中时触发；避免静态页也付 Chrome 成本。
- **charset 解码**：本轮不纳入；现有链路继续按 UTF-8 处理，遇到编码问题另行立项。

## 6. 落地顺序与总验收

**顺序**

1. P0：F1 + F2（预计 1–2 个工程日）。先做质量门控与抓取层硬修复，风险低、收益直接。
2. P1：F4 的清洗规则（预计 2–3 个工程日）；单次解析按 pprof 结果决定。
3. P2：F3（方案 B 先验证，1–2 天；方案 A 1–2 周）与 F5（按需）。

**总验收 corpus**

- 50–100 个 URL，覆盖：静态长文、SPA 壳、导航壳、Cloudflare 200/403、chunked 大响应、PDF、链接列表页。
- 离线 fixture 优先（保存 HTML 快照）；线上冒烟单独标记，默认不阻塞 CI。
- 指标：内容成功率（过质量门控）、p50/p95 延迟、HEAD 请求数、下载字节数、空闲/峰值 RSS、markdown token 数、正常页回归 diff。
- 所有新增用户可见文案走 i18n；新增配置同步 `pkg/config/config.go`、两份 `config.example.yaml`、`docs/configuration*.md`（涉及搜索/抓取参数再同步 `docs/search*.md`、`docs/api*.md`、README）。

## 7. 参考

- Firecrawl Local README: <https://github.com/firecrawl/firecrawl/tree/main/apps/local>
- Launch changelog: <https://www.firecrawl.dev/changelog/firecrawl-local>
- 第三方对比（含基准口径提醒）: <https://dev.to/hassann/firecrawl-self-hosted-vs-firecrawl-local-what-changed-1c4d>
- 本项目相关文件：`pkg/fetch/content.go`、`mcp/tool_cleanfetch.go`、`mcp/tool_search.go`、`mcp/security.go`、`pkg/proxy/proxy.go`、`pkg/fetch/webfetch/webfetch.go`、`pkg/fetch/jina/reader.go`、`go-webfetch/parse/html.go`、`go-webfetch/parse/router.go`。
