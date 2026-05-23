# Changelog

## [v3.2.0] - 2026-05-23

### 新增
- **cleanfetch 网页抓取工具**：通过 Jina Reader API 获取指定 URL 的干净网页内容，降低反爬拦截风险
  - 仅在配置 `jina.api_key` 后注册，不影响现有功能
  - 对常见 HTTP 错误（403/404/429 等）返回简明中文提示
  - 新增 SSRF 防护：URL 协议校验、内网地址黑名单
  - 新增客户端超时（30s）防止 goroutine 泄漏

### 优化
- **学术搜索结果增强**：保留论文元数据（作者、DOI、类型），格式化时自动区分论文和网页结果
- **缓存系统改进**：
  - 支持 `academic` 参数区分，防止学术/非学术缓存混用
  - 数据库自动迁移，兼容旧版缓存
  - 查询优化：两步查询充分利用索引
- **站点屏蔽统一**：`black_list_host` 和 `bing.blocked` 自动合并，SearXNG 后端同步生效
- **字符串拼接优化**：`MergeContent` 改用 `strings.Builder`，复杂度从 O(n²) 降为 O(n)
- **排序优化**：`HybridSearchImpl` 冒泡排序改为 `sort.Slice`

### 修复
- 学术搜索失败时不再静默回退到通用搜索，返回明确错误信息
- Tavily 搜索正确使用 `exclude_domains` 过滤站点
- `describeHTTPError` 使用 `fmt.Sprintf` 替代不必要的 `fmt.Errorf`

---

## [v3.1.0] - 2026-05-20

### 新增
- LLM 摘要未启用时，`smartsearch` 工具自动移除 `intent` 参数，节省客户端上下文 token
- MCP 服务增加 30s 心跳 + 5 分钟空闲 session 自动清理
- HTTP Server 增加超时配置（ReadHeader 10s / Read 60s / Idle 120s）
- 异步摘要 goroutine 增加 panic recover

### 修复
- Dockerfile 启动参数缺失导致容器立即退出

---

## [v3.0.0] - 2026-05-15

### 新增
- `engine` 搜索模式：无需 API Key，使用 Bing 通用搜索 + 学术搜索引擎
- 学术搜索引擎集成：arXiv、Crossref、OpenAlex、Semantic Scholar
- MCP 工具新增 `academic` 参数
- `black_list_host` 屏蔽站点配置（对 Bing 和 Tavily 生效）

### 优化
- LLM 摘要提示词：主动过滤低质量内容、合并重复结果、保留关键原文并标注引用

---

## [v2.0.0] - 2026-05-01

### 新增
- Tavily 搜索 API 支持
- LLM 摘要支持（建议使用快速模型）
- SQLite 缓存管理

---

## [v1.0.0] - 2026-04-15

### 初始版本
- 百度千帆 AI Search API 支持
- 基础 MCP 服务框架
