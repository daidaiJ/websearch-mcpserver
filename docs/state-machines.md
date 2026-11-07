# 状态机清单与可达非法状态

> 2026-11-07 审查产出。范围：`websearch-mcpserver` 里所有带持久或跨请求状态的子系统。
> 每条状态机给出状态集合、不变式、转换表；随后列出**代码实际可达**的非法状态与转换，
> 附可执行证据与影响排序。
>
> 本文件初版只记录缺陷、不含修复方案；B1–B12 与性能项已于同批修复并转正为常驻用例，
> 见下方「0.1 修复状态」。§7 保留原始复现口径（临时探针已并入正式测试）。
>
> 修复后的不变式由下列常驻测试（`*_test.go`，非一次性探针）锁定，全部经「改回旧实现
> 必须失败」的双向验证：
>
> | 缺陷 / 项 | 常驻测试 |
> |---|---|
> | B1 会话轮换竞态 | `ddg/bing/baidu/google/so360` 的 `*_concurrency_test.go`（`-race`） |
> | B2 成功清空并发冷却/失败 | 各引擎 `RecordSuccessKeepsConcurrent*` |
> | B3 KeyError 分类 | `pkg/search/apipool/apipool_test.go` 的 transport/auth/rate_limit 三例 |
> | B4 软超时后副作用 | `ddg`/`baidu`/`bing`/`google`/`so360` `Test*SoftTimeoutDoesNotAdvanceState`、`hybrid/ctx_cancel_test.go`、`antirobot/cooldown_test.go` |
> | B5 refcount 非原子 / 重复 stop | `server/refcount_test.go` |
> | B6 冷却读-改-写 | `ddg` `TestConcurrentCooldownEscalation`、`antirobot/cooldown_test.go` |
> | B7 academicsearch 缓存键 | `mcp/tool_academic_cache_test.go` |
> | B8/B9 KeyPool 下发失效 key / 标错 key | `pkg/search/apipool/apipool_test.go` |
> | B10 代次写入静默丢弃 | `pkg/cache/generation_test.go` |
> | B11 mcphub 空结果不冷却 | `pkg/search/provider/mcphub/hub_test.go` |
> | B12 本端限流静默 | 各引擎 `*LocalRateLimitIsAttributable`、`hybrid/local_limit_test.go` |
> | 性能：tokenizer / MMR / URL 归一化 | `enhance/enhance_text_oracle_test.go`、`enhance/mmr_target_test.go` |

---

## 0. 汇总：按影响排序的已确认缺陷

| # | 缺陷 | 可达性 | 影响 | 证据 |
|---|---|---|---|---|
| **B1** | 会话轮换竞态：`rotateSession` 在锁外写 `client`/`ua`/`reqCount` | 并发搜索即可，`rate_limit.per_sec=3` 时必现 | **P0**。`-race` 确认三引擎，`go test -race` 在 CI 已开却未覆盖该路径。已证实的后果：① `reqCount++`（锁内）与 `reqCount = 0`（锁外）竞争 → 轮换节奏错乱，会话寿命不再是配置的 25/30 次；② 已读到旧 `client` 的请求会用它发出，而轮换已换了新的 cookie jar → 请求用的 `http.Client`/UA/代理与「本应轮换后的」状态不一致，反检测的会话隔离失效。指针撕裂在 amd64 上不成立，故非内存安全问题 | `ddg`/`baidu`/`bing` 三处 `-race` 报告 |
| **B2** | 一次成功清空并发建立的限流冷却 | 429 与 200 并发返回 | **P0**。冷却被静默解除 → 上游 429 风暴；`AdoptCooldown` 的跨进程翻倍记忆被单次成功清零 | `TestEvidence_RecordSuccessClearsConcurrentCooldown` |
| **B3** | 传输层瞬断即冻结 API Key 30 分钟 | 单 Key 供应商一次网络抖动 | **P0（AGENTS.md 已登记为「审查遗留」）**。`tavily`/`exa`/`doubao`/`baidu`/`yuanbao` 把 timeout/DNS/连接重置包成 `KeyError`，`apipool` 据此 `MarkInvalid`。这次补上了量化证据：单 Key 供应商一次瞬断 → 30 分钟不可用，且期间第二次搜索**仍会打上游一次**（key 已被冷却，但 `Next()` 仍下发它，见 B8） | `TestEvidence_TransportErrorDisablesKeyFor30Min` |
| **B4** | 软超时放弃后后台 goroutine 继续改状态 | hybrid 任意慢引擎 | **P1**。观测层记「超时失败」、上游记「成功」；失败清单与真实结果不符，且被放弃的调用仍在推进冷却/会话状态 | `TestEvidence_SoftTimeoutLeavesGoroutineMutatingState` |
| **B5** | `refcount` 的 Add-then-clamp 非原子 + 重复 stop 非幂等 | 重复 `stop`/脚本重试 | **P1**。多一次 `-1` 就把别人的服务关掉；`Store(0)` 覆盖并发 `Add` 造成计数漂移 | `TestEvidence_RefCountNegativeTriggersShutdown` 等 |
| **B6** | 冷却「读-改-写」非原子，后写者可缩短冷却 | 并发 429 | **P1**。翻倍升级丢失；40s 冷却被 20s 覆盖 | `TestEvidence_CooldownShorterOverridesLonger` |
| **B7** | `academicsearch` 缓存键不含 `page` | 任何 `page≥2` 且命中缓存 | **P1**。分页静默退化为「永远返回第一页」 | `TestEvidence_AcademicCacheKeyOmitsPage` |
| **B8** | 全部 key 失效时仍下发已知失效 key（带伤重试） | 池内全部 key 冷却 + 新请求 | **P2**。必然被上游再拒一次；纯浪费配额 | `TestEvidence_NextServesKnownInvalidKey` |
| **B9** | `MarkLastInvalid` 在并发下标错 key | 非 `KeyError` 失败路径 + 并发 | **P2**。坏 key 留池、好 key 被误冻；代码注释已自认「非并发安全」但仍被 `apipool` 作为兜底调用 | `TestEvidence_MarkLastInvalidClobbered` |
| **B10** | 缓存代次写入静默丢弃 | 并发「清空缓存」 | **P2**。调用方无法区分「写成功」与「被代次丢弃」 | `TestEvidence_GenerationStoreDropsSilently` |
| **B11** | mcphub 空结果算失败但不冷却 | 上游 200 + 零结果 | **P2**。语义不对称：进失败清单却不触发避让 | `TestEvidence_EmptyResultsTriggerCooldown` |
| **B12** | 本端限流返回空结果 + nil error | 6 个引擎 | **P2**。失败清单把它记成「零结果」，归因失真（`ClassifyFailure` 看不到限流） | `TestB12_SilentVsLoudLocalLimitInDiagnostics`（实测：静默型 `failures=[]`、聚合错误是「均未返回有效结果」；响亮型正确给出 `kind=rate_limit`） |

## 0.1 修复状态（2026-11-07 同批修复）

| # | 修复要点 | 落点 |
|---|---|---|
| B1 | 会话快照 `takeSession()` 同临界区取 client+UA；轮换（计数+判定+重写）整体入锁 | 五引擎 `*_engine.go` |
| B2 | 引入状态代次 `stateEpoch`：请求开始取快照，成功只在「期间无并发进入冷却/记录失败」时清理 | 五引擎 + `pkg/academic/arxiv.go` |
| B3 | `KeyError` 加 `Kind`（auth/rate_limit/transport/content）+ `NewKeyError`；apipool 按分类查表：auth 冷却 30min、rate_limit 短冷却、transport/content 不冷却且不换 key | `keypool.go`、8 个 provider、`apipool.go` |
| B4 | 新增 `antirobot.ContextEngine` 可选接口；`ddg`/`baidu`/`bing`/`google`/`so360`/`arxiv`/`github` 实现 `SearchContext`；ctx 取消后请求中止、且失败/成功/进入冷却均不再推进共享状态；`preDelay` 改为可取消等待；编排层 `execOne` 优先走 ctx 入口 | `antirobot/{engine,searcher}.go`、上述引擎、`hybrid.go`（既有 ctx 下传保持不变） |
| B5 | `refMu` + `applyRefDelta`：锁内完成「检查 + 更新」，按饱和处理，`1→0` 迁移才发一次信号 → 重复 stop 真 no-op | `server/server.go` |
| B6 | 冷却档位计算收敛为纯函数 `antirobot.NextCooldownDuration`，读写在同一临界区内 | `antirobot/cooldown.go`、五引擎、`arxiv.go` |
| B7 | 缓存键并入 `page`（`academicCacheKey`），Lookup/Store 共用 | `mcp/tool_academic.go` |
| B8 | `Next()` 全冷却返回 `""`，新增 `NextUsable()`；8 个 provider 与 mcphub 均不再下发失效 key | `keypool.go`、8 个 provider、`mcphub/hub.go` |
| B9 | 删除 `MarkLastInvalid` 与池级 `lastKey`；兜底按 `KeyError` 分类处理 | `keypool.go`、`apipool.go` |
| B10 | 新增哨兵 `cache.ErrStaleGeneration`，代次作废的写入显式返回；`mcp` 调用方按竞态静默处理 | `pkg/cache/sqlite.go`、`mcp/tool_search.go`、`mcp/tool_academic.go` |
| B11 | 新增哨兵 `mcphub.ErrEmptyResults`，「200 + 零结果/空正文」按失败同待遇触发冷却（判定走哨兵、不靠文本关键词） | `mcphub/{client,hub,parse}.go` |
| B12 | 新增哨兵 `antirobot.ErrLocalRateLimit` + `LocalLimitSkipped`，七个静默引擎改为返回可归因错误 | `antirobot/local_limit.go`、七引擎 |

性能项（梯队四）已完成三项：tokenizer 去正则（手写字节扫描，字节级等价已验证）、MMR `target_count` 缺省跟随 `max_size`（消掉二次方重排，贪心前缀性等价已验证）、URL 归一化统一（`enhance.normalizeURLKey` 委托 `core.NormalizeURL`）。「冷却状态机五份合一」收敛为 `antirobot.NextCooldownDuration` 一处纯函数实现。**未做：遥测移出请求路径**（编排开销收益真实但改动面涉及关闭时 flush，本轮取舍为不动）。

---

## 1. 限流与反检测状态机

### 1.1 `RateLimiter` — 滑动窗口（`pkg/antirobot/limiter.go`）

**状态**

| 字段 | 含义 |
|---|---|
| `perSec` / `perMin` | 窗口容量（构造时固定，**无运行时变更路径**） |
| `minInterval` | 相邻请求最小间隔（0 = 不约束） |
| `last` | 上次**被放行**请求的时刻 |
| `secWindow` / `minWindow` | 1 秒 / 1 分钟窗口内的放行时间戳 |

**不变式**

- I1：`len(secWindow) ≤ perSec`、`len(minWindow) ≤ perMin`（`Allow` 返回 true 时才 append）
- I2：`secWindow`/`minWindow` 中所有时刻都在各自窗口内（`filterAfter` 惰性裁剪，**只在 `Allow` 被调用时执行**）
- I3：`minInterval > 0` 时相邻两次「返回 true」的间隔 ≥ `minInterval`
- I4：`Allow` 返回 false 时**不记录**任何时间戳（注释明确承诺）

**转换表**

| from | 事件 | to | 副作用 |
|---|---|---|---|
| 任意 | `Allow()`, 窗口未满且间隔满足 | 已记录 | `last=now`，两个窗口各 append |
| 任意 | `Allow()`, `len(secWindow) ≥ perSec` | 拒绝 | 无（窗口已裁剪） |
| 任意 | `Allow()`, `now-last < minInterval` | 拒绝 | **无，且窗口不裁剪**（提前 return） |

**问题**

- I2 在「长时间不调用」后失效：窗口内的陈旧时间戳一直留到下次 `Allow`。无害（下次会裁掉），但窗口内存随 `perSec` 线性增长。
- `minInterval` 拒绝路径（第 3 行）**跳过 `filterAfter`**：常量内存不涨，但与 I2 的「窗口始终有效」表述不符。

### 1.2 引擎冷却（`ddg` / `arxiv` 为完整实现）

**状态**

| 字段 | 含义 |
|---|---|
| `cooldown` | 当前翻倍档位（首档 = `ddgRateCooldown` 20s） |
| `cooldownUntil` | 冷却截止（墙钟） |
| `consecFails` / `backoff` | 连续失败数与前置延迟 |
| 落盘副本 | `antirobot.healthStates` → `engine-health.json` |

**不变式（设计意图）**

- I5：`cooldownUntil` 之后 `cooldownRemaining() == 0`
- I6：冷却期内不打上游
- I7：连续 429 时 `cooldown` **单调不减**（翻倍升级），封顶 `ddgRateCooldownMax`
- I8：成功则 `cooldown=0`、`cooldownUntil=zero`，并清落盘记录
- I9：`AdoptCooldown` 后，档位跨进程续接（已过期则只续档位、不续避让）

**转换表**

| from | 事件 | to | 实现位置 |
|---|---|---|---|
| 正常 | `Search` 入口，`cooldownRemaining()>0` | 冷却拒绝 | `ddg_engine.go:97` |
| 正常 | 上游 202/429 | 预算内等待重试 或 进冷却 | `handleRateLimited` |
| 冷却 | `cooldownUntil` 过期 | 正常 | 时间驱动，无显式转移 |
| 任意 | `recordSuccess()` | `cooldown=0`, `cooldownUntil=zero` | `ddg_engine.go:257` |
| 启动 | `adoptPersistedCooldown()` | 续接档位/避让 | `ddg_engine.go:280` |

**违反 I7 / I8 的可达路径** → 见 B2、B6。

### 1.3 各引擎的分级（现状不一致）

| 引擎 | 冷却档位 | 落盘收养 | 本端限流返回 |
|---|---|---|---|
| `ddg` | 翻倍（20s → 2min） | 是 | `error` |
| `arxiv` | 翻倍（30s → 2min）+ 预算内等待重试 | 是 | 预算内等待 / `error` |
| `github` | `Retry-After`/`x-ratelimit-reset`，封顶 10min | **否** | **空结果 + nil** |
| `sitefetch/github` | 同上 | **否** | n/a |
| `mcphub` | 固定 `cooldown`（无翻倍） | **否** | 进 `errs` 列表 |
| `baidu`/`bing`/`so360`/`google` | 仅 `backoff` 前置延迟（无冷却期） | 否 | **空结果 + nil** |
| `wikipedia`/`googlenews` | 无 | 否 | **空结果 + nil** |
| `anysearch` 匿名档 | 无（固定 1/s·20/min） | 否 | `error` |

---

## 2. API Key 池状态机

### 2.1 `KeyPool`（`pkg/search/provider/keypool.go`）

**状态**：每个 key 一个 `invalidUntil`（零值 = 有效）；`idx` 轮转游标；`lastKey` 池级最近返回。

**不变式**

- I10：`Next()` 优先返回 `invalidUntil` 为空或已过期的 key
- I11：`Available()` == 0 ⟺ 所有 key 都在冷却中
- I12（**代码注释自认未满足**）：`MarkLastInvalid` 应标记「本请求实际使用的 key」

**转换表**

| from | 事件 | to | 备注 |
|---|---|---|---|
| 有效 | `Next()` | 有效（被选中） | `invalidUntil` 若已过期则**顺手清零**（隐式写） |
| 有效 | `MarkInvalid(k)` | 冷却 30min | |
| 冷却中 | 30min 到期 | 有效（惰性） | 无定时器：靠下次 `Next()`/`Available()` 观察 |
| **全部冷却** | `Next()` | **返回已知失效 key** ⚠️ | 违反 I10 的意图 → **B8** |
| 有效 | `MarkLastInvalid()` | 冷却**别的** key ⚠️ | 违反 I12 → **B9** |

### 2.2 `apipool` 重试状态机（`pkg/search/apipool/apipool.go`）

**转换表**

| from | 事件 | to |
|---|---|---|
| 起始供应商 | `callSingle` 失败，`errors.As(*KeyError)` | `MarkInvalid(ke.Key)` → 同 pool 下一 key |
| 起始供应商 | `callSingle` 失败，非 `KeyError` | `MarkLastInvalid()` → 同 pool 下一 key ⚠️ |
| 同 pool 耗尽（`Available()==0`） | — | 下一个供应商 |
| 全部供应商失败 | — | 返回聚合错误 |

**问题**

- `callSingle` 内部的 `Next()` 不等待冷却恢复，因此「同 pool 下一 key」可能是已知失效的（B8）。
- `MarkInvalid` 的 30min TTL 对传输层错误过重（B3）。
- 单次搜索的上游调用数 = Σ(每个供应商的 key 数)：3 供应商 × 2 key = 6 次（已实测）。这是**有意的广播**，但在供应商整体故障时会放大成小型重试风暴。

---

## 3. 缓存状态机

### 3.1 代次（generation）（`pkg/cache/sqlite.go`）

**状态**：`cache_generation.generation`（单行）；请求视图持有其**快照** `*int64`。

**不变式（设计意图）**

- I13：`Clear()` 之前的请求写入的记录，不被 `Clear()` 之后的请求读到
- I14：`Clear()` 之前的在飞请求不会把陈旧记录写回表

**转换表**

| from | 事件 | to | 实现 |
|---|---|---|---|
| gen=N | `Clear()` | gen=N+1，表清空 | `Clear()` |
| gen=N | 请求 `ForRequest()` | 视图固定 gen=N | `ForRequest()` |
| 视图 gen=N | `Store()`（gen 已变 N+1） | **影响 0 行，返回 nil** | `WHERE ... generation = ?` |
| 视图 gen=N | `Lookup()`（gen 已变 N+1） | 用 gen=N 的键查（**无守卫**） | `Lookup()` |

**问题**：I13 成立于「表已清空」而非「键含代次」——`Lookup` 没有代次守卫，正确性依赖 `Clear` 的 `DELETE` 已执行。当前实现下两者等价，但**不变式的保护来自一个巧合**：若将来 `Clear` 改成软删除/延迟清理，旧视图会立即读到跨代记录。写入丢弃（I14）成立，但完全静默（B10）。

### 3.2 smartsearch 缓存键（`mcp/tool_search.go`）

`webSearchCacheQuery` = `web:v3:` + `{query, months, fetchTopN}`。`fetchTopN` 参与键，因此 `n=0` 与 `n=3` 是两条记录；未命中的 `n` 会回退查 `n=0` 的 `query_only` 记录再补抓（`tool_search.go:101`）。**该回退路径正确**。

### 3.3 academicsearch 缓存键（`mcp/tool_academic.go`）

`cacheKey = query|timeRange|engines|page`。初版键**不含 page**（B7），导致 `page≥2`
在 TTL 内命中第一页记录；已修复，由 `mcp/tool_academic_cache_test.go` 固定。

---

## 4. 编排层状态机

### 4.1 hybrid 软超时（`pkg/search/hybrid/hybrid.go`）

**状态**：`lastDiag`（单槽，并发请求互相覆盖）；每个引擎调用一个不可取消的子 goroutine。

**不变式**

- I15：`lastDiag` 反映「最近一次 `SearchRaw` 的失败清单」（注释自认并发下会串请求，已提供 per-request 入口作替代）
- I16：软超时后引擎的 goroutine 应停止或至少不再影响共享状态

**转换表**

| from | 事件 | to |
|---|---|---|
| 引擎执行中 | `engineTimeout` 到达 | 调用方收到 `errEngineTimeout`；**引擎 goroutine 继续** |
| 引擎执行中 | 请求 ctx 取消（`ContextSearcher`） | 引擎收到取消 |
| 引擎执行中 | 请求 ctx 取消（旧接口） | 调用方放弃等待；**引擎 goroutine 继续** ⚠️ |

**违反 I16** → **B4**（已实测：超时后副作用计数=1，上游最终成功）。

### 4.2 mcphub 轮转（`pkg/search/provider/mcphub/hub.go`）

**状态**：每个 backend 的 `searchCooldown` / `fetchCooldown`（`atomic.Int64` 纳秒 + 0 哨兵）。

**转换表**

| from | 事件 | to |
|---|---|---|
| 可用 | `Search` 成功且非空 | 清冷却 |
| 可用 | `Search` 返回非空错误 且 `shouldCooldown(err)` | 冷却 `cooldown` 时长 |
| 可用 | `Search` 返回空结果（`err == nil`） | **记失败但 `shouldCooldown("空结果")==false` → 不冷却** ⚠️ |
| 全部冷却 | `candidates()` | 直接返回错误（含最早解冻时刻），**不排队等待** |

**问题**：语义不对称（B11）；`ErrMsg` 提到「所有后端处于冷却中」时上层只看到一个字符串错误，无法区分「真失败」与「正在避让」。

### 4.3 本端限流返回值的分歧

`limiter.Allow()==false` 时各引擎的返回：

```
返回空结果 + nil error（6 处）
  baidu_engine.go:84   bing_engine.go:69   github_engine.go:119
  google_engine.go:74  googlenews_engine...:115  wikipedia_engine.go:93
   → hybrid 侧：err == nil && len(results) == 0
   → mergeResults 计入「成功但空」，不产生 EngineFailure
   → ClassifyFailure 永远看不到限流

返回 error（2 处）
  ddg_engine.go:92   → FailureRateLimit
  arxiv.go:92        → 预算内等待重试，超预算才 error
```

后果（已实测，见 `TestB12_SilentVsLoudLocalLimitInDiagnostics`）：两个静默型引擎同时被本端限流时，`diag.Failures` 为**空**，聚合错误是「所有搜索引擎均未返回有效结果」——与控制中心看到的「零结果」同义；对照的响亮型引擎正确产出 `engine=duckduckgo kind=rate_limit`。`telemetry.ClassifyErrorKind` 也因此对本端限流完全无感知，控制中心的失败构成里看不到这一大类。

---

## 5. 进程生命周期状态机

### 5.1 refcount ↔ 进程（`server/server.go` + `cmd/main.go`）

**状态**：`refCount atomic.Int32`；`shutdownCh`（容量 1）；PID 文件。

**不变式（设计意图）**

- I17：`refCount == 0` ⟹ 触发优雅关闭
- I18：`refCount` 是各客户端 `start`/`stop` 的代数和，且 `≥ 0`
- I19：重复 `stop` 应为幂等 no-op

**转换表**

| from | 事件 | to | 副作用 |
|---|---|---|---|
| n>0 | `start` → `POST +1` | n+1 | — |
| n>0 | `stop` → `POST -1` | n-1 | — |
| n=1 | `POST -1` | 0 | **发 `shutdownCh`** |
| n=0 | `POST -1` | `Add`→-1，`Store(0)` | **再次发 `shutdownCh`** ⚠️ 违反 I19 → **B5** |
| 任意 | 并发 `Add` 与 `Store(0)` | 计数漂移 ⚠️ | 违反 I18 → **B5** |

### 5.2 PID 文件（`pkg/daemon/daemon.go`）

**状态**：存在 / 不存在；内容 `{pid, port}`。

**不变式**

- I20：PID 文件存在 ⟹ 该进程在运行（`cmd/main.go:48` 的「监听成功后才写」保证启动方向）
- I21：PID 文件不存在 ⟹ 无服务在跑

**违反 I21 的可达路径**：`runStart` 在 `GetHealth` 失败后无条件 `RemovePID()`（`main.go:42`），随后才 `srv.Run`。若 health 探测因**瞬时网络抖动**失败，而服务其实在运行，PID 文件被删掉——此后 `kill` 的 PID 兜底路径失效（`main.go:106` 读不到 PID）。反向也成立：`status`/`stop`/`kill` 都在 health 失败时 `RemovePID()`（`main.go:64/95/114/126`），把「探测不到」当成「不存在」。

---

## 6. 其他带状态的子系统

| 子系统 | 状态 | 不变式 | 问题 |
|---|---|---|---|
| `quota` 本地用量 | `quota_state(provider, period_start, adjust)` + `usage_events` 派生 | 展示用量 = `adjust + COUNT(成功调用 occurred_at ≥ period_start)` | `SetProviderUsed` 在「读 period_start → 数调用 → 写 adjust」之间无事务；并发管理与调用会造成临时负 `adjust` |
| MinerU 任务 | `task_ref`（`standard:`/`batch:`/`agent:`），`TaskPendingError{TaskRef, Status, Wait}` | 待定态必须带 `task_ref` 才能续查 | `pdfTaskResult` 把 pending 变成 `IsError=true` 且 `err=nil`；上层若只看 `err` 会误判为成功 |
| `telemetry` 熔断展示 | `SuspensionPolicy` + 派生 `suspension` | `consecutiveFailures ≥ 3` 才展示挂起；**从不影响是否发起调用** | 与引擎自身的冷却**是两套独立状态**，可能给出相反结论（面板显示 healthy，引擎实际在冷却） |
| `health.go` 落盘 | `engine-health.json` | advisory：损坏/缺失只损失一次试探 | `persistHealth` 用 `Remove` + `Rename`，Windows 下存在窗口文件缺失；并发写者互相覆盖（每次全量重写，后写者赢） |
| `cache.CleanupScheduler` | `started atomic.Bool` + `stop`/`done` channel | `Stop` 幂等；未 `Start` 时 `Stop` 立即返回 | `Stop` 用 `stopOnce` 关 channel、`started` 判断是否等待 —— `Start` 里先 `Store(true)` 再起 goroutine，窗口内 `Stop` 会等到 `done`（由新 goroutine 关闭），正确 |

---

## 7. 复现方式

证据测试文件（已从工作树删除，需要时按下述方式重建）：

| 文件 | 覆盖 |
|---|---|
| `pkg/search/engine/ddg/zz_evidence_probe_test.go` | B1（真实引擎 + httptest，`-race`） |
| `pkg/search/engine/ddg/zz_cooldown_probe_test.go` | B2、B6 |
| `pkg/search/engine/baidu/zz_evidence_probe_test.go` | B1（baidu） |
| `pkg/search/engine/bing/zz_evidence_probe_test.go` | B1（bing） |
| `pkg/search/provider/zz_evidence_probe_test.go` | B8、B9 |
| `pkg/search/apipool/zz_evidence_probe_test.go` | B3、重试放大 |
| `pkg/search/hybrid/zz_evidence_probe_test.go` | B4 |
| `pkg/cache/zz_evidence_probe_test.go` | B10 |
| `pkg/search/provider/mcphub/zz_evidence_probe_test.go` | B11 |
| `server/zz_evidence_probe_test.go` | B5（需要 `RemoteAddr=127.0.0.1` 过 `localOnlyMiddleware`） |
| `mcp/zz_evidence_probe_test.go` | B7 |
| `pkg/search/hybrid/zz_b12_probe_test.go` | B12 |

运行：

```bash
go test -race -run 'TestEvidence_' ./pkg/search/engine/ddg/ ./pkg/search/engine/baidu/ ./pkg/search/engine/bing/
go test -run 'TestEvidence_' ./pkg/search/provider/ ./pkg/search/provider/mcphub/ ./pkg/search/apipool/ \
                             ./pkg/search/hybrid/ ./pkg/cache/ ./server/ ./mcp/
```

**当前 CI 拦不下任何一条**（已实测）：CI 的 race 步骤是 `go test -race -short ./...`（`.github/workflows/ci.yml:43`），本机跑该命令**全绿**，而上面 12 条缺陷全部可达。原因是这些包内既有的并发测试都不驱动会话轮换 / 冷却 / Key 池的并发路径——`-race` 需要测试**实际并发触发**目标路径才有意义，仅开启检测器不够。

---

## 8. 设计层面的共性根因

1. **「检查-使用」跨锁边界**（B1、B6、B5）：状态在锁内验证、在锁外改。统一修法是让状态迁移成为一个**原子操作**（`WithLock(func(){...})` 或 `compare-and-swap`），而不是「读出来、判断、再写回」。
2. **成功路径无条件覆盖失败路径**（B2）：`recordSuccess` 清空一切，没有「本次成功只针对本次请求」的作用域概念。正确形态是**逐次调用记录结果、由窗口聚合**，而不是共享字段的直接赋值。
3. **错误分类与状态迁移耦合**（B3、B9、B12）：`KeyError` 同时表达「key 坏了」和「这次调用失败了」，`MarkLastInvalid` 兜底把「与 key 无关的失败」也变成 key 冷却。分类应是显式的（auth / rate_limit / transport / content），迁移策略按分类查表。
4. **两套并行的健康语义**（§6 `telemetry` vs 引擎冷却）：面板与引擎各自维护「是否健康」，可能互相矛盾。
5. **不变式缺少断言**：上述所有违反都能在单元测试里以「小规模 + 并发」捕获，说明缺的是**并发不变式测试**这一层，而不是更多功能测试。
