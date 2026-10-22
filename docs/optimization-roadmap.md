# 后续优化增强方向（Todo 专题）

> 2026-10-19 调研产出的路线图。首批八项已于同日落地并推送（`b780769`），本文件收敛为**剩余事项清单**；已完成项的落地要点见文末归档表，详细实现见对应提交与 CHANGELOG。
>
> 总评：项目已过功能建设期，投入方向为四条线——搜索链路的 context 化与并发正确性、结果去重与守卫校准的质量收尾、MCP 协议新代际跟进、可观测性与测试护栏。零 Key + 反检测 + agent 成本控制是既有差异化定位，继续深耕而非扩面。

## 待办

### 结果质量收尾

- [ ] **Q3. LLM 摘要请求无 token/预算控制**
  `pkg/llm/llm.go` 的 `ChatRequest` 只有 Model/Messages/Stream，无 `max_tokens`、温度透传；摘要 prompt 拼入结果的长度控制未见配置项。
  方向：配置项 + prompt 截断预算，减少快速供应商下的无谓 token 开销。配置变更须同步两份 `config.example.yaml` 与 docs。

### MCP 生态跟进

- [ ] **E1. go-sdk v1.7.0 → v1.8.0（2026-07-28 新 spec：stateless core / extensions / elicitation / sampling / tasks）**
  对本项目有实际价值的两点：
  - **sampling**：摘要借客户端模型，把"自配 LLM key 才能摘要"的门槛降为可选项；
  - **elicitation**：查询意图不明确时向用户澄清（学术搜索 engines 选择是天然场景）。
  步骤：先升 SDK 跑回归，能力按"客户端支持才启用"的门控引入，与能力边界表的谨慎风格一致。

### 可观测性与测试护栏

- [ ] **T2. 引擎包测试密度**：`pkg/search/engine/{baidu,bing,google}` 各仅 1 个测试文件，解析函数（HTML→结构化结果）适合表驱动 + 固定 fixture 补齐。

## 已完成归档（2026-10-19）

| 项 | 落地要点 | 提交 |
|---|---|---|
| **R1** context 贯通 | 可选 `ContextSearcher` / `ContextTimeRanger` / `ContextAcademicSearcher` / `ContextDiagnosticsSearcher`，`SearchInf` 原签名不变；新配置 `smartsearch.engine_timeout_sec`（默认 10s，负数=只依赖请求死线） | `b780769` |
| **R2** 诊断 per-request | `core.DiagnosticsSearcher` 值传递，单槽降为兜底；并发请求不再互相覆盖失败清单 | `b780769` |
| **R3** 缓存并发读 | DSN 级 WAL / `busy_timeout` / `synchronous=NORMAL`，连接池 1→4；原实现只注释声明 WAL、从未执行 PRAGMA | `b780769` |
| **Q1** URL 归一化 | `antirobot.NormalizeURL`（core 转发）：scheme/host 小写、去 fragment、剥追踪参数、参数排序、去尾斜杠；接入两处 hybrid 去重键与 `DeduplicateResults` | `b780769` |
| **Q2** 守卫校准 | `guard` 遥测事件（脱敏）+ `go run ./tools/guardreplay -db <dashboard.db>` 阈值回放 | `b780769` |
| **O1** metrics 端点 | `GET /metrics` 零依赖手写 Prometheus 文本；只读脱敏，遥测未启用返回 503 | `b780769` |
| **T1** SSRF fuzz | 三个 Fuzz 目标覆盖 `validateURLSecurity` / `isPublicIP` / `isPrivateHostFast`，CI 跑 10s 短窗口 | `b780769` |
| **T3** CI 护栏 | `go test -race -short` 步骤 + golangci-lint v2 job（`--new-from-rev` 只拦新增代码） | `b780769` |

> 验证：`go build ./...`、`go vet ./...`、`go test ./...`（含网络集成）、`go test -race -short ./...`、golangci-lint（新增代码 0 issues）、actionlint 全绿；`/metrics` 已用真实二进制实机验证。

## 外部参照（2026-10 检索）

- 竞品对比（braintrust.dev 2026-09）：Parallel / You.com / Exa / TinyFish / Firecrawl / Brave Search API / Tavily——均为 API Key 商业服务；本项目的零 Key + 反检测 + 本地文件检索（Everything）是差异位，不建议追功能广度。
- MCP 2026-07-28 spec 已由 Cloudflare MCP v2、GitHub MCP Server 跟进落地；go-sdk v1.8.0 对应就绪。
