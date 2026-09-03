# AGENTS.md — 智能体协作指南

> 本文件帮助 AI 智能体快速理解项目结构和开发约定，确保长期维护与持续开发的一致性。

---

## 项目一句话定位

**websearch-mcpserver** 是一个用 Go 编写的轻量级 MCP 搜索服务，零 API Key 即可运行，支持 Claude Code / Qwen Code / Cursor 等 MCP 客户端。提供四大核心能力：

| 能力 | 说明 |
|------|------|
| **搜索引擎搜索** | 内置百度/Bing/DuckDuckGo/Google 多引擎并发编排，支持 Tavily/Exa 等 API 引擎混合 |
| **API 搜索** | API Key 池轮转模式，支持百度千帆/Tavily/Exa 等多种 API 供应商，失败自动切换 |
| **学术搜索** | arXiv/Crossref/OpenAlex/PubMed/Semantic Scholar/Google Scholar/Europe PMC/DBLP/DOAJ 九大学术引擎并发，DOI 跨引擎去重、逐引擎错误透传 |
| **网页抓取** | 增强型网页内容提取（TLS 指纹伪装 + SSRF 防护 + Jina Reader 备选） |
| **PDF 解析** | MinerU AI 增强 PDF 解析（表格/公式/多栏/图片智能识别），无 Token 自动降级 |

---

## 技术栈速览

| 项 | 值 |
|---|---|
| 语言 | Go 1.26+，纯 Go 无 CGO |
| 数据库 | SQLite（modernc.org/sqlite，纯 Go 实现） |
| 配置 | Viper（YAML + 环境变量覆盖） |
| 日志 | Zerolog 结构化日志 |
| MCP 协议 | modelcontextprotocol/go-sdk |
| HTTP 客户端 | resty.dev/v3 + go-webfetch（TLS 指纹伪装） |
| 部署 | 单二进制 / Docker 多阶段构建 |

---

## 目录结构与模块职责

```
cmd/              # 入口：main.go + 平台初始化（Windows 代理检测等）
mcp/              # MCP 协议层：工具注册、请求处理
server/           # HTTP 服务：生命周期、路由
searxng/          # SearXNG 兼容 HTTP 端点

pkg/
├── search/       # ★ 核心编排层
│   ├── inf.go          # 接口定义（SearchInf, SearchResult）
│   ├── hybrid.go       # 多引擎并发编排、去重、合并、排序
│   ├── factory.go      # 引擎工厂：根据配置选择并实例化引擎
│   ├── engine_adapter.go  # 通用引擎适配器（Tavily/Exa/Google/DDG）
│   ├── baidu_fallback.go  # 百度适配器（含智能回退）
│   ├── bing_adapter.go    # Bing 适配器
│   ├── apipool.go         # API Key 池轮转
│   └── *_test.go          # 单元测试
├── antirobot/    # 反检测公共层：Searcher 接口、限流器、TLS 指纹
├── baidu/        # 百度底层引擎实现
├── bing/         # Bing 底层引擎实现
├── ddg/          # DuckDuckGo 底层引擎实现
├── google/       # Google 底层引擎实现
├── academic/     # 学术搜索：arXiv/Crossref/OpenAlex/PubMed/S2/GS/EuropePMC/DBLP/DOAJ
├── config/       # 配置加载与结构体定义
├── cache/        # SQLite 缓存（6h 过期，后台清理）
├── webfetch/     # 增强型网页抓取（SSRF 防护、DNS rebinding 检测）
├── jina/         # Jina Reader 备选抓取
├── llm/          # LLM 摘要生成
├── mineru/       # MinerU PDF 解析
├── proxy/        # 系统代理自动检测（Windows 注册表 / 环境变量）
├── daemon/       # 引用计数进程管理
├── log/          # 日志配置
└── xml/          # XML 格式化
```

---

## 核心数据流

```
用户请求 → MCP Tool / HTTP API
         → factory.go 选择引擎组合
         → hybrid.go 并发调用多引擎
         → 各引擎适配器 → antirobot 层（反检测/限流）→ 底层 HTTP 请求
         → 结果回传 → 去重 → per-engine score 过滤 → 排序/轮询合并
         → 可选 LLM 摘要 → 返回用户
```

---

## 开发约定

### 新增搜索引擎

1. 在 `pkg/` 下创建独立包（如 `pkg/newengine/`）
2. 实现 `antirobot.Searcher` 接口（`Name()`, `Search()`, `SearchRaw()`）
3. 在 `pkg/search/` 下创建适配器文件，实现 `search.SearchInf` 接口
4. 在 `pkg/config/config.go` 中添加引擎配置结构体
5. 在 `pkg/search/factory.go` 中注册引擎到工厂

### 配置变更

- 所有配置结构体在 `pkg/config/config.go`
- 样例配置同步更新 `config.example.yaml` 和 `pkg/config/config.example.yaml`
- 环境变量覆盖格式：`WEBCRAWLER_` 前缀 + 大写路径（如 `WEBCRAWLER_PORT`）

### 测试

- 测试文件与源文件同目录，命名 `*_test.go`
- 运行测试：`go test ./...`
- 引擎测试需要对应引擎可用（或 mock）

---

## 当前系统能力边界（智能体需 aware）

以下为当前已知的能力边界，开发时不要重复踩坑：

| 模块 | 现状 | 结论 |
|------|------|------|
| 通用评分管线（RRF/域名品质/词汇对齐/稀有词/共识权威新鲜度 Boost/阈值过滤/意图分类/MMR） | v2.14.0 已全部实现 | 不要重复立项 |
| Google 网页引擎 | 2025-01 起 JS 挑战全量硬化，"凭据缺失"模型，HTTP 200 空壳零结果；UA/TLS 伪装全部失效（详见 `pkg/config/config.go` GoogleConfig 注释） | 保持默认关闭，勿再尝试伪装修复 |
| Google wml + Nokia UA 绕过（SearXNG PR #6546 路线） | 2026-09-03 实测：HK 代理出口下首请求 429 进 /sorry/，其余 200 均为 JS 挑战空壳，Google 未对 Nokia UA 返回 WML/XML | 强依赖出口 IP 信誉，非普适方案；wml 遗留端点随时可能被 Google 移除，勿照抄 |
| 百度 tn=json 接口（SearXNG baidu.py 路线） | 2026-09-03 实测：直连裸客户端 3/3 被 302 至 wappass 验证码，预热 cookie 无效；同 IP 下 HTML 引擎同样被 CAPTCHA。识别主因疑似 IP 信誉 + TLS 指纹，与 HTML/JSON 入口无关 | tn=json 非免检通道，JSON 接口不能替代 pkg/baidu 现有反检测层 |
| DDG / arXiv 限流 | 服务端窗口限流，引擎内置钳制（DDG 1/s·6/min，arXiv 1/s·12/min + 3s 间隔）+ 429 冷却避让 + 预算感知重试 | 调整限流参数须实测校准，勿放宽内置上限 |

---

## 快速上手命令

```bash
# 构建
go build -o websearch-mcpserver ./cmd/

# 运行（零 API Key 模式）
./websearch-mcpserver

# 运行测试
go test ./...

# Docker 构建
docker build -t websearch-mcpserver .
```

---

## 给智能体的提示

1. **修改搜索逻辑前**，先读 `pkg/search/inf.go` 了解接口契约，再读 `hybrid.go` 了解编排流程
2. **新增配置项时**，同步更新 `pkg/config/config.go` 和两个 `config.example.yaml`
3. **涉及反检测/限流**，修改应在 `pkg/antirobot/` 层进行，不要在各引擎包中重复实现
4. **学术搜索与通用搜索是独立模块**，学术引擎在 `pkg/academic/`，通用引擎在 `pkg/baidu/` `pkg/bing/` 等，不要混淆
