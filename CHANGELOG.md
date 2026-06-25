# Changelog

[English](CHANGELOG.en.md) | [中文](CHANGELOG.md)

## v2.7.0 — 2026-06-25

### 新增
- **缓存显式开关**：`CacheConfig` 新增 `cache.enabled` 字段（`*bool`），支持显式启用/禁用缓存
  - 不设置时按 `storage_path` 是否非空判断（向后兼容旧行为）
  - 显式 `false` 强制禁用缓存（忽略 `storage_path`）
  - 显式 `true` 强制启用缓存
- **英文版文档**：新增 `README.EN.md`、`docs/config.en.md`、`docs/api.en.md`、`CHANGELOG.en.md`，中英文文档顶部互加语言切换链接
- 新增 `CacheEnabled()` 单元测试（6 个用例覆盖所有分支）

## v2.6.0 — 2026-06-24

### 新增
- **SmartSearch Score 过滤**：per-engine `min_score` / `max_size`，全局 `max_size`，`show_meta` 控制来源和分数展示
  - 引擎回传 score 时按 `min_score` 过滤，保留 `max_size` 条
  - 引擎不回传 score 时忽略 `min_score`，取 `min(max_size, ⌈global_max_size / 引擎数⌉)` 截断
  - 全局 `max_size`：有 score 时按 score 排序截断，无 score 时按引擎轮询均匀分配
  - `show_meta` 控制输出中是否显示引擎来源和相关性分数（默认 true）
- **API 引擎命名区分**：Tavily → `tavily_api`、百度千帆 → `baidu_api`；百度网页搜索保持 `baidu`
- 引擎 `Name()` 方法统一纳入 `SearchInf` 接口

## v2.5.0 — 2026-06-03

### 新增
- **系统代理自动检测**：默认读取 Windows 注册表（`ProxyEnable` / `ProxyServer`）和环境变量（`HTTP_PROXY` / `HTTPS_PROXY`），Clash、V2RayN 等代理软件开启系统代理后自动生效，无需手动配置 `proxy.enabled` 和重启服务
  - `pkg/proxy/sysproxy.go` — 跨平台系统代理检测（Windows 注册表 + WinHTTP + 环境变量）
  - `pkg/proxy/detector.go` — 后台轮询检测器，30s 周期检测代理变更并通知回调
  - `pkg/proxy/proxy.go` — `DynamicProxyTransport` 请求级动态代理解析，每次请求实时获取代理端点
- **Jina Reader 不再依赖 proxy.enabled**：配置 `jina.api_key` 即可启用，代理由系统自动检测
- **全局限流重试**：所有 HTTP 客户端自动处理 429 限流，读取 `Retry-After` 头等待后重试一次（最大等待 5s，超限直接返回 429）
- **arXiv 引擎限流**：内置 1 req/s 限流器（arXiv 官方建议），超限时等待而非触发 429

### 变更
- **引擎初始化逻辑重构**：Google、Semantic Scholar、Google Scholar 始终初始化（除非各自 `disable_*` 显式禁用），代理在请求级别动态解析，开关代理无需重启
- **Jina Reader 去除 proxy.enabled 门控**：`NewFromConfig` 仅检查 `jina.api_key`，代理由 `ProxyResolver` 动态解析
- **Google 引擎 HTTP 客户端**：从静态 `http.ProxyURL` 改为 `DynamicProxyTransport`，请求时实时解析代理
- **学术引擎 HTTP 客户端**：从 `proxy.NewHTTPClient(endpoint)` 改为 `proxy.NewDynamicHTTPClient(resolver)`，支持运行时代理切换；国内引擎也使用带 retry 的客户端
- **测试 URL 更新**：`TestFetchWebPage` / `TestCleanFetch_WebFetchSuccess` 测试 URL 从 `arthurchiao.art`（已不可达）更换为 `wmyskxz.cn`

### 向后兼容
- `proxy.enabled: true` + `proxy.endpoint` 仍正常工作（显式代理模式）
- `proxy.enabled: false` 仍正常工作（显式禁用代理）
- 未设置 `proxy.enabled` 时行为变更：从"不使用代理"变为"自动检测系统代理"

### 新增文件
- `pkg/proxy/sysproxy.go` — 系统代理检测核心逻辑
- `pkg/proxy/sysproxy_windows.go` — Windows 注册表 + WinHTTP 检测实现
- `pkg/proxy/sysproxy_other.go` — 非 Windows 平台占位（仅环境变量）
- `pkg/proxy/detector.go` — 后台代理变更检测器

## v2.4.0 — 2026-06-02

### 新增
- **百度网页搜索引擎**：参考 SearXNG baidu.py 实现，使用 `tn=json` JSON API 直接抓取百度搜索结果，无需 API Key
  - `mode=baidu` 无 SK 时作为主引擎；有 SK 时 SK 失败自动回退网页搜索
  - `mode=engine` 默认与 Bing 并发搜索
  - `mode=hybrid` 自动参与混合搜索
- **Google 搜索引擎**：参考 SearXNG google.py 实现，HTML 解析 Google 搜索结果，需代理访问
  - `proxy.enabled=true` 时自动启用，支持 CAPTCHA 检测、CONSENT Cookie 绕过
  - `mode=engine` / `mode=hybrid` 代理启用时自动加入并发搜索
- **全局限流配置**：新增 `rate_limit` 配置节（`per_sec` / `per_min`），对所有搜索引擎统一生效（默认 3/s, 60/min）

### 变更
- **`engine` 模式引擎组合**：从仅 Bing 改为百度网页搜索 + Bing 并发（代理启用时加入 Google）
- **`hybrid` 模式百度策略**：有 SK 时使用 `BaiduWithFallback(SK, 网页搜索)`，SK 失败自动回退；无 SK 时直接用网页搜索
- **`baidu` 模式增强**：无 SK 时不再回退到 Bing，而是使用百度网页搜索引擎
- **限流默认值提升**：全引擎默认 3/s, 60/min（原 Bing 1/s, 20/min）
- Bing 限流配置从 `bing.per_sec` / `bing.per_min` 迁移到全局 `rate_limit` 配置

### 新增文件
- `pkg/baidu/` — 百度网页搜索引擎（engine + opts + 15 个单元测试）
- `pkg/google/` — Google 搜索引擎（engine + opts + 17 个单元测试）
- `pkg/search/engine_adapter.go` — 通用引擎适配器（antirobot.Engine → SearchInf）
- `pkg/search/baidu_fallback.go` — 百度 SK 回退包装器

## 2026-05-28

### 新增
- **cleanfetch 增强型网页抓取**：集成 `go-webfetch` 库，无需代理即可抓取网页内容，失败时自动回退到 Jina Reader（需代理）
  - `cleanfetch.enabled` 控制开关（默认 false，旧配置不启用）
  - 大内容自动存储到临时文件，支持配置输出目录、TTL、内联阈值
- **pdf_parser PDF 解析工具**：将本地 PDF 文件转换为 Markdown（`pdf_parser.enabled` 控制，默认 false）
- **hybrid 模式 Bing 混合搜索**：hybrid 模式下 Bing 作为原生引擎与 API 引擎（Baidu/Tavily）并发搜索

### 变更
- cleanfetch 工具现在只需配置 `cleanfetch.enabled: true` 即可使用，不再强制要求代理和 Jina API Key
- Go 版本升级至 1.26（go-webfetch 依赖要求）

## 2026-05-26

### 新增
- **Windows 开机自启动**：`install` / `uninstall` 命令，使用 COM API (ole32.dll) 创建快捷方式，无需依赖 PowerShell
- **PubMed 学术引擎**：生物医学文献权威数据库，国内直连
- **Google Scholar 学术引擎**：全学科学术搜索，需代理
- **MCP 工具拆分**：`smartsearch`（通用搜索）+ `academicsearch`（学术搜索）独立工具，`academicsearch` 支持 `engines` / `time_range` / `page` 参数
- **学术搜索并行化**：多引擎并发请求，结果按 URL 去重 + 分组归一化排序
- **BingFallback 配置**：`academic.bing_fallback` 控制学术搜索时是否用 Bing 兜底
- **proxy 配置**：仅海外学术引擎（Semantic Scholar、Google Scholar）走代理
- **CI 自动发版**：GitHub Actions workflow，tag 推送后自动构建 linux/windows 二进制并发布 Release，附带 SHA256 校验

### 重构
- **提取 server 包**：`RunServer`、admin handlers、引用计数逻辑从 `cmd/main.go` 提取到可导出的 `server` 包，支持作为 Go 模块嵌入
- **学术引擎独立模块**：新增 `pkg/academic`（6 个引擎独立实现）和 `pkg/antirobot`（共享引擎框架：Engine 接口、Searcher 编排器、限流器）
- **Bing 包精简**：`pkg/bing` 仅保留 Bing 通用搜索引擎 + 反爬逻辑

### 文档
- 新增 [docs/api.md](docs/api.md)：Go Module API 和 HTTP API 完整文档
- 新增 [docs/config.md](docs/config.md)：配置参考、默认值速查、环境变量覆盖
- README 全面重写：精简结构化，补充特性亮点、运维参考、排障指南

## 2026-05-23

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

## 2026-05-20

### 新增
- LLM 摘要未启用时，`smartsearch` 工具自动移除 `intent` 参数，节省客户端上下文 token
- MCP 服务增加 30s 心跳 + 5 分钟空闲 session 自动清理
- HTTP Server 增加超时配置（ReadHeader 10s / Read 60s / Idle 120s）
- 异步摘要 goroutine 增加 panic recover

### 修复
- Dockerfile 启动参数缺失导致容器立即退出

---

## 2026-05-15

### 新增
- `engine` 搜索模式：无需 API Key，使用 Bing 通用搜索 + 学术搜索引擎
- 学术搜索引擎集成：arXiv、Crossref、OpenAlex、Semantic Scholar
- MCP 工具新增 `academic` 参数
- `black_list_host` 屏蔽站点配置（对 Bing 和 Tavily 生效）

### 优化
- LLM 摘要提示词：主动过滤低质量内容、合并重复结果、保留关键原文并标注引用

---

## 2026-05-01

### 新增
- Tavily 搜索 API 支持
- LLM 摘要支持（建议使用快速模型）
- SQLite 缓存管理

---

## 2026-04-15

### 初始版本
- 百度千帆 AI Search API 支持
- 基础 MCP 服务框架
