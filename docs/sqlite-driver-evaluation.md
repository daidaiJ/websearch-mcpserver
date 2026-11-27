# SQLite 驱动替换评估（待定需求）

> 状态：**待评估，未实施**。本文只记录候选方案与验证口径，不改变现有依赖。
> 记录日期：2026-11-27；候选版本快照：`github.com/liustack/sqlite` v0.1.8。

## 1. 背景

项目当前统一使用 `modernc.org/sqlite v1.48.1`（纯 Go，无 CGO），落点：

| 模块 | 用途 | 连接/写入特征 |
|---|---|---|
| `pkg/cache/sqlite.go` | 搜索结果缓存，按 `(query, intent, academic)` 读写，TTL 清理 | WAL，`busy_timeout=5000`，`synchronous=NORMAL`，`SetMaxOpenConns(4)` |
| `pkg/telemetry/store.go` | 遥测事件与日聚合，仪表盘查询 | WAL，`busy_timeout=5000`，`synchronous=NORMAL`，`SetMaxOpenConns(1)` |
| `tools/guardreplay/main.go` | 只读回放 guard 事件、校准阈值 | 只读 DSN `mode=ro` |

构建约束是**单二进制、纯 Go、CGO 关闭**；发布矩阵包含 linux/windows amd64、darwin amd64/arm64，GHCR 另含 linux arm64。任何替换都必须保持这些目标可构建。

## 2. 候选方案

`github.com/liustack/sqlite`：

- CGO-free，用 LLVM 把 SQLite 的 C 源码编译成 Go 汇编，注册的驱动名同为 `sqlite`。
- DSN 示例：`file:app.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate`。
- 支持 linux/darwin/windows 的 amd64/arm64；要求 Go 1.26.6–1.27。
- 许可证 MIT；v0.1 阶段，上游明确说明 v1 前 API 可能变化。`v0.1.6` 已被上游 retract，评估应钉 `v0.1.8` 或更新的明确版本。
- 额外能力：内置 S3/R2 实时备份（`replica` 包）。本项目**不使用**该能力，评估时需确认不引入额外运行时行为。
- 依赖包含 `github.com/superfly/ltx`、`golang.org/x/arch`、`golang.org/x/sys`、`pierrec/lz4/v4`，需核对实际编译进二进制的范围。

上游 README 的基准（linux/amd64，WAL + `synchronous=NORMAL`）声称：单行点查 3.85 µs，modernc 9.61 µs；读 10 万行 34.7 ms，modernc 79.7 ms；64 并发写入 3.72 µs，modernc 30.7 µs；大文本读取略慢于 modernc。以上均为**上游自报数据，尚未在本项目环境复现**。

## 3. 为什么需要验证

- 本项目的 SQLite 调用频次低、单次数据量小，搜索链路的网络耗时通常远大于本地查询；上游微基准的倍数差未必能转化为端到端收益。
- 缓存连接池为 4、遥测为 1，当前没有高并发写竞争；候选驱动主打的“并发写合批、减少 `database is locked`”在本项目中可能收益有限。
- 候选驱动处于 v0.1，且使用生成的 Go 汇编；跨平台行为、Windows 文件锁、只读 DSN、错误码语义都需要实测。
- 只有通过验收阈值才值得承担新依赖；否则保留 modernc 并记录结论。

## 4. 评估计划

### 4.1 基准负载

用与生产一致的 schema 与查询，不先改业务代码。建议在 `pkg/cache`、`pkg/telemetry` 下用 `testing.B` 建独立基准，两个驱动通过构建标签或独立临时模块隔离（两者都注册 `sqlite`，同进程不能同时导入）。

| 场景 | 操作 | 指标 |
|---|---|---|
| 缓存命中 | 按 `(query, intent, academic)` 点查，含 JSON 反序列化 | p50/p95/p99、QPS |
| 缓存写入 | 插入/更新缓存行 + 命中时间更新 | p50/p95/p99、QPS |
| 缓存清理 | TTL 删除、`Clear`/generation 更新 | 耗时、锁等待 |
| 遥测写入 | 单条事件插入、事务内批量插入 | p50/p95/p99、QPS |
| 遥测查询 | 仪表盘聚合、最近事件分页 | p50/p95/p99 |
| 并发读写 | 4 读 + 1 写、8 读 + 2 写，持续 30 分钟 | 吞吐、`database is locked` 次数、P99 |
| 启动/迁移 | 空库建表、旧库迁移、`integrity_check` | 耗时、峰值内存 |
| 只读回放 | `guardreplay` 的 `mode=ro` 查询 | 耗时、兼容性 |

每组至少 6 次运行取中位数，固定 Go 版本、OS、磁盘与电源策略；Windows 开发机与 linux/amd64 各跑一轮，darwin arm64 有余力再补。

### 4.2 正确性与构建

- `go test ./...`、`go test -race -short ./...`、`golangci-lint`。
- 同一份代码分别以 `CGO_ENABLED=0` 构建 linux/windows amd64、darwin amd64/arm64、linux arm64，确认无新增构建标签或工具链依赖。
- 验证 DSN 兼容：`_pragma=busy_timeout(...)`、`journal_mode(WAL)`、`synchronous(NORMAL)`、`mode=ro`、`:memory:`、文件路径含空格/中文。
- 验证迁移幂等、`ALTER TABLE` 重复列错误处理、时间/布尔扫描、`PRAGMA optimize`、事务回滚、`Close` 后文件可删除（Windows 重点）。
- 记录二进制体积、启动时间、常驻内存变化。

### 4.3 供应链与合规

- 核对 MIT 许可证与 `THIRD_PARTY_NOTICES.md` 更新范围。
- 检查实际依赖树、生成的汇编来源、是否有默认网络行为（尤其 `replica` 包不应被隐式启用）。
- 记录上游 issue/发布节奏与 v0.1 API 变更风险；评估是否应等待 v1 再切换。

## 5. 验收阈值

满足全部正确性/构建条件，并达到以下性能条件之一，才进入切换评审：

- 缓存点查或缓存写入 p95 提升 ≥ 20%，或绝对延迟下降 ≥ 0.5 ms；
- 遥测单条写入 p95 提升 ≥ 20%；
- 30 分钟并发压测无新增 `database is locked`，吞吐不低于现有驱动。

同时不得出现：

- 启动/迁移耗时回退 > 10%，数据库体积增长 > 10%，二进制体积增长 > 2 MB；
- Windows 文件锁、只读打开、迁移或错误码行为回归；
- 任何数据完整性问题。

未达阈值或风险不可接受时，**保留 `modernc.org/sqlite`**，在本文补充实测数据与结论，不引入候选依赖。

## 6. 风险与回退

| 风险 | 处理 |
|---|---|
| v0.1 API 可能变化 | 钉精确版本；切换前等待 v1 或预留回退开关 |
| 生成的 Go 汇编可审计性 | 核对上游生成流程、固定 commit、记录校验信息 |
| Windows WAL/文件锁差异 | 在目标 Windows 版本上跑完整回归与并发压测 |
| 两个驱动同名 `sqlite` | 基准用构建标签/独立模块隔离，禁止同进程同时导入 |
| 额外依赖与 R2 备份能力 | 只导入驱动根包；确认二进制不包含未使用路径或默认网络行为 |
| Go 版本下限 1.26.6 | 确认 CI/开发机工具链满足；不满足则暂缓 |

## 7. 结论

当前**不做替换**。先把本文件的基准与验收口径落到测试代码，拿到本项目真实负载数据后再决定；评估结果回填到本节和 `docs/optimization-roadmap.md` 的 S1 条目。

## 8. 参考

- 候选仓库：<https://github.com/liustack/sqlite>
- 上游基准与说明：仓库 `README.md`、`docs/benchmarks.md`、`docs/architecture.md`
- 当前依赖：`modernc.org/sqlite v1.48.1`（`go.mod`）
- 现有落点：`pkg/cache/sqlite.go`、`pkg/telemetry/store.go`、`tools/guardreplay/main.go`
