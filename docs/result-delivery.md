# MCP 长结果交付契约

## 目标与边界

修复长搜索、网页和 PDF 保存结果只返回服务端路径、客户端无法取得正文的问题。沿用现有保存阈值与短响应行为，不改变默认检索内容模式。常规读取仅使用 MCP，不要求客户端文件系统或 Docker 权限。

测试边界已由用户确认：真实 MCP 客户端 -> 原工具 -> 分段正文读取。审查基线为 `96062d46a495b2b0f032c6b3564bbaa1bf391c88`。协议测试不证明 Codex、DSH 或 WorkBuddy 自动发现并选择工具；这些宿主和生产入口需另行真实验收。2026-11-07 起仓库已具备 dsh 插件分发链路（npm config-only bundle，见 `npm/README.md` 与 `docs/installation.md#npm--dsh-插件分发`）。本机已用本地 tgz 在 dsh CLI `0.2.2-rc.1` 上验过 bundle 装载、`!!js` 路径求值与 MCP `initialize` / `tools/list` 握手（3 个工具），详见 `npm/README.md` 的验收结果表；但**尚未做 dsh 真实会话里的长结果分段读回**，也尚未走 npm registry 安装，这两项仍按验收清单实测后再结论。

## 交付要求

1. 超限搜索保留各候选标题、URL、来源、短摘录、统计、溯源头和失败清单；原渲染正文完整保存。短摘录最多 160 Unicode 字符加省略标记。
2. 所有已保存的正文返回 `result_ref` 与明确的 MCP 读取提示，旧文件路径保留兼容。调用 `cleanfetch(result_ref, offset, limit, project?)` 返回 `content`、`next_offset`、`eof`，JSON text 与 structured content 等价。
3. 偏移为从 0 开始的 Unicode 码点；默认/0 页长 4096，最大 8192。分页不丢字、不重复；精确 EOF 返回空正文。负数、越界偏移和超大页长报错。
4. 引用是随机、进程内的能力凭据，仅对应服务生成的文件；不接受任意路径、URL 或未知引用。沿用端点认证，`project` 仅统计标签。会话切换及无状态模式可续读。
5. 搜索引用最长 7 天；网页/PDF 保存引用沿用 `cleanfetch.file_ttl`（默认 24 小时）。重启、过期、清理或检测到文件身份/大小/修改时间变化时返回 `result_unavailable`，重新调用原工具。结果目录由可信宿主维护，不承诺抵御已有文件写权限且刻意保留元数据的修改。读取不请求上游，不消耗供应商额度。
6. 普通长搜索缓存命中生成新引用。补抓结果使用结构化 `content_saved` 标记临时文件提示，不依据正文文案判断；此类结果不持久缓存，也不从缓存文本解析路径并授予读取权限。网页键采用 `web:v3:`，不复用缺少该状态的旧网页缓存；旧记录按原 TTL 清理，不删除数据库或改变学术缓存。
7. `cleanfetch` 始终注册；`cleanfetch.enabled=false` 仅禁止 URL 抓取，不影响引用读取，不因注册初始化联网模块。`url`/`urls` 与 `result_ref` 互斥，URL 模式拒绝 `offset`/`limit`；旧 URL/批量抓取返回 Markdown 保持兼容。工具列表、能力资源、快照和控制中心保持五工具，区分 URL 抓取开关与正文读取能力；遥测 detail 区分 `url_fetch`/`saved_result`，不记录正文或引用、不把读取计成上游请求。

## 验证

`mcp/result_delivery_test.go` 通过 HTTP/官方 SDK 驱动真实 MCP 协议，供应商使用固定离线夹具。服务端文件操作仅用于构造清理/替换等故障；正文验收均从 MCP 响应读取。

已观察到的红灯包括：长搜索缺少可读引用、网页保存正文缺少可读引用、超限候选信息消失、文件清理后缓存复用旧引用，以及摘要成功后丢失正文引用。逐个切片修复后，覆盖网页/PDF、缓存、Unicode、跨会话、非法参数、符号链接及文件替换。默认页长、EOF 和鉴权有同边界回归检查。接口收敛时先观察到抓取关闭导致 cleanfetch 未注册的红灯，再通过同一 MCP 边界验证引用读取、模式互斥、关闭抓取及隐私遥测。

运行：`go test -short ./mcp ./pkg/dashboard -count=1`。离线全仓短测显式跳过两个既有无门控 PubMed 联网测试；首次还命中未修改的 `TestApipool_Weighted_FreeEngineWeightOne` 概率性失败，单独复跑 5 次与全仓复跑通过。不据此宣称真实供应商验证通过。

普通正文中的文件路径文案不影响缓存，也不作为已保存正文附加到摘要。HTTP MCP 回归覆盖此行为与旧路径缓存隔离；流式摘要成功信号仅表示上游已完成，仍需消费完正文通道，避免空摘要或丢尾段。固定摘要夹具重复 20 次在修复前出现 14 次空摘要，修复后通过。

## 运维与剩余验收

生产部署与本机 Skill 同步由独立运维记录追踪，不纳入源码贡献分支。公开正文已通过 MCP 分段读回；原生宿主验收应核对当前 schema，并使用工具连续读取所需正文，不能用 Docker 读取替代交付验收。这些记录不保证所有宿主都会主动选择续读。

读取为流式 Unicode 扫描，续页从文件头扫描到偏移，成本随 offset 增长；未做大文件性能承诺。网页缓存键升级会造成一次冷缓存，不改变时间窗口和配置代次隔离。Windows 离线交叉编译因已有依赖 go-strftime v1.0.0 未缓存而阻断；本机执行、实际时间过期和独立进程重启尚未自动验证。

## 依据

- [MCP 官方 structured content 契约](https://modelcontextprotocol.io/specification/2025-06-18/server/tools#structured-content)
- [Go os 文件访问约束](https://pkg.go.dev/os#OpenRoot)
- [API 参数及生命周期](api.md#长结果读取)
