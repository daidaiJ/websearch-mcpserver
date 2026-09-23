# websearch-mcpserver

> 轻量级 Web Search MCP Server — 零 API Key 即可运行

<p align="center">
  <a href="README.EN.md">English</a> · <a href="README.MD">中文</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go" alt="Go"></a>
  <a href="https://github.com/daidaiJ/websearch-mcpserver/releases"><img src="https://img.shields.io/github/v/release/daidaiJ/websearch-mcpserver" alt="Release"></a>
  <img src="https://img.shields.io/badge/MCP-Streamable%20HTTP%20%2F%20stdio-7C3AED" alt="MCP">
  <img src="https://img.shields.io/badge/API%20Key-optional-22C55E" alt="Zero API Key">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT"></a>
</p>

<p align="center">
  <img src="docs/images/hero-banner.png" alt="多引擎搜索融合示意：百度 / Bing / DuckDuckGo / 学术源汇入本地编排，输出结构化结果" width="900">
</p>

用 Go 编写的 MCP 搜索服务。内置百度网页、Bing、DuckDuckGo 等通用引擎和 9 个学术引擎，搜索、评分、缓存全部在本地完成。可作为 MCP 工具接入 Claude Code、Qwen Code、Cursor，也可作为 Go 模块嵌入自有服务。

**免费、国内可用、结果可直接给 LLM 消费。** 无 Key 也能搜；有 Key 才用 Key。

---

## 架构一览

分层设计：客户端只面对 4 个 MCP 工具；引擎组由 `mode` 组装；评分、缓存、代理、抓取都在本进程内完成，查询不会经过第三方聚合服务。

<p align="center">
  <img src="docs/images/architecture.png" alt="系统组成架构：接入层、协议层、编排层、通用/学术引擎、支撑组件" width="900">
</p>

| 层 | 做什么 |
|----|--------|
| **接入** | Claude Code / Qwen Code / Cursor / HTTP API / 嵌入 Go 模块 |
| **协议** | `/mcp` 四个工具 · `/searxng/search` 兼容 LiteLLM · `/__admin` 进程管理 · `/dashboard` 可选本机控制台 |
| **编排** | `factory` 按 mode 组装 · `hybrid` 并发去重合并 · RRF / Boost / MMR 评分 |
| **引擎** | 通用：百度网页 / 千帆 / Bing / DDG / Tavily / Exa / AnySearch / 豆包；学术 9 源并行 |
| **支撑** | SQLite 缓存、系统代理自动检测、webfetch（SSRF 防护）、MinerU、LLM 流式摘要 |

更完整的回退链、代理检测与嵌入方式见 [docs/architecture.md](docs/architecture.md)。

---

## 面向 LLM 的工具链

四个工具覆盖联网工作流，结果互相衔接，一次配置全链路可用：

<p align="center">
  <img src="docs/images/toolchain.png" alt="smartsearch → academicsearch → cleanfetch → pdf_parser 工具链" width="900">
</p>

---

## 核心特性

| 能力 | 说明 |
|------|------|
| 零 Key 搜索 | `engine` 模式内置百度网页搜索 + Bing 并发，无需任何 API Key |
| 多引擎融合 | 多种搜索模式、8 个通用引擎 + 9 个学术引擎，主引擎失败自动回退 |
| 相关性评分 | RRF 融合排名 + 词汇对齐 / 域名品质 / 共识 / 权威 / 时效加分，低分自动裁剪；MMR 打散转载 / 镜像 |
| 学术搜索 | 9 大学术引擎并行，按引用数 / 期刊权威 / PDF 可用性 / 新鲜度评分；DOI 跨引擎去重 |
| 网页抓取 | `cleanfetch` 内置 SSRF / DNS rebinding 防护与超大文件预检，失败回退 Jina Reader |
| PDF 解析 | 本地 PDF 文本优先提取，扫描件可回退 MinerU OCR |
| LLM 摘要 | 可选接入 OpenAI 兼容 API 生成结构化摘要，支持流式推送 |
| 系统代理 | Clash 等开启系统代理后，海外引擎 / Jina Reader 自动走代理 |
| 本机控制台 | 可选 `dashboard.enabled`，`/dashboard/` 只读观测界面：调用统计、来源健康、失败分类、白名单配置修改（默认关闭） |
| 轻量部署 | 单二进制、无 CGO、引用计数进程管理，可嵌入 Go 模块 |

---

## 搜索与评分管线

结果不是原始聚合。多引擎回传后在本地做去重、融合排名和多样性重排，再可选生成摘要：

<p align="center">
  <img src="docs/images/pipeline.png" alt="查询经 factory、并发检索、去重、RRF、Boost、阈值、MMR 后返回" width="900">
</p>

---

## 设计背景与目标

### 为什么做这个项目

LLM 需要联网搜索，但现成的 MCP 搜索方案不能满足我的偏好和需求：

- **厂商 MCP 服务（Tavily / Exa 等）**：要注册 API Key、按量付费（Tavily 约 $8/1k、Exa $7/1k），免费额度有限；数据经过第三方服务器，无法自托管；海外服务国内访问不稳定、支付不便；单一供应商限流 / 宕机无回退；只提供搜索，学术检索、网页抓取、PDF 解析、摘要都要额外接。
- **自建 SearXNG + MCP 包装**：要部署维护一个 Python 服务（Docker、配置、升级），公共实例常被限流 / 封禁；结果是原始聚合，没有为 LLM 优化（无相关性评分、无去重、无摘要）；只有通用网页搜索，没有学术引擎、抓取、PDF；代理要手动配置。

所以我从 2026-04 的「百度千帆一个引擎」起步，逐步演进为多引擎融合的通用搜索服务，目标是让搜索成为 LLM 的**免费、国内可用、结果可直接消费**的基础能力。

### 与现成方案的差别

| 维度 | 厂商 MCP（Tavily / Exa） | SearXNG MCP | 本项目 |
|------|------------------------|-------------|--------|
| 成本 | 按量付费，免费额度有限 | 免费但需自托管 | 免费，零配置 |
| 部署 | 注册即用 | Docker / Python 自建维护 | 单二进制，无 CGO |
| 国内可用 | 差（海外服务） | 需手动配代理 | 系统代理自动检测 |
| 供应商容错 | 单一供应商，无回退 | 引擎聚合 | 多引擎 + 自动回退 |
| LLM 优化 | 原始结果 | 原始结果 | 本地评分 + 去重 + 可选摘要 |
| 学术搜索 | 无 | 无 | 9 大学术引擎 |
| 抓取 / PDF | 需额外接 | 无 | 内置 cleanfetch / pdf_parser |
| 数据隐私 | 过第三方服务器 | 本地 | 本地 |

### 设计原则

**本地优先，隐私默认** — 搜索、评分、缓存全部在本地完成，查询只发给搜索引擎本身，不经过任何第三方聚合服务。数据不出本地，这是与厂商 MCP（数据过第三方服务器）最本质的区别。

**零成本起步，按需付费** — 免费引擎（百度网页 + Bing）零 Key 可用；本地启发式评分不烧 AI token；SQLite 缓存省重复请求。有 Key 才用 Key，不为用不到的能力付费。

**丰简由人** — 同一份配置，`mode` 从 `engine`（零配置）到 `hybrid`（全引擎）渐进式选择复杂度；零配置用户和重度用户各取所需，不为复杂度买单。

**解耦可组合** — 引擎、模式、工具互不耦合：`mode` 决定引擎组，4 个工具各自 `enabled` 开关，Key 可选（`sk_list` 多 Key 轮询）。配置驱动一切（per-engine 过滤、评分阈值、MMR、屏蔽站点、限流），全部可调，不写死。

**面向 LLM 的完整工具链** — 4 个工具覆盖联网工作流：`smartsearch` → `academicsearch` → `cleanfetch` → `pdf_parser`，结果互相衔接，一次配置全链路可用。

**场景化优化** — 针对真实使用场景：学术搜索（9 引擎 + 引用 / 期刊 / PDF 评分）、国内网络（直连 + 系统代理自动检测）、扫描件 PDF（MinerU OCR 回退）、时效性查询（`time_range`）。

---

## 快速开始

```bash
# 1. 下载二进制: https://github.com/daidaiJ/websearch-mcpserver/releases
# 2. 启动（无需手写配置，无需 API Key）
# Windows 开机自启动 可选
./websearch-mcpserver.exe install
#
#    首次 install 会在可执行文件目录自动生成一份可编辑的预设 config.yaml 和 autostart.vbs
./websearch-mcpserver start
# 或者点击
autostart.vbs
# 3. 注册到 MCP 客户端（见 docs/installation.md）
```

> 「零配置」= 首次启动自动生成与 `config.example.yaml` 相同的预设 `config.yaml`，改端口 / Key / 模式都改这一份文件。默认只监听 `127.0.0.1`；开放网卡（`host: 0.0.0.0`）时建议配置 `auth_token` 保护业务端点。

或通过 MCP Hooks 实现会话自动启停（Qwen Code 示例，完整说明见 [docs/installation.md](docs/installation.md)）：

```json
{
  "hooks": {
    "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver start", "timeout": 10000 }] }],
    "SessionEnd":   [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver stop",  "timeout": 10000 }] }]
  }
}
```

---

## 搜索模式速览

| 模式 | 说明 | 需要 Key |
|------|------|----------|
| `engine` | 百度网页搜索 + Bing（代理可用时加入 DuckDuckGo） | **无需** |
| `baidu` | 百度千帆搜索，失败回退百度网页搜索 | 可选 |
| `apipool` | API Key 池轮转：每次只调一个供应商，失败自动切换，支持 round-robin / priority / weighted | 各 Key 可选 |
| `tavily` | Tavily Search API（[获取 Key](https://app.tavily.com/home)） | `TAVILY_SK` |
| `exa` | Exa Web Search API（[获取 Key](https://dashboard.exa.ai/api-keys)） | `EXA_API_KEY` |
| `anysearch` | AnySearch API（[获取 Key](https://www.anysearch.com/console/api-keys)） | `ANYSEARCH_API_KEY` |
| `doubao` | 豆包联网搜索 Global / Custom（[获取 Key](https://console.volcengine.com/search-infinity/api-key)） | `DOUBAO_SEARCH_API_KEY` |
| `hybrid` | 全引擎混合（Anysearch + 百度 + Tavily + Exa + 豆包(有 Key 时) + Bing + DuckDuckGo 等） | 各 Key 可选 |

> 无 Key 时自动降级为 `engine` 模式。各模式与引擎的详细说明见 [docs/search.md](docs/search.md)。

---

## 本机控制中心（可选，默认关闭）

> 完整配置参考（快捷方式落位、品牌关于块、全部键与默认值）见 [docs/dashboard.md](docs/dashboard.md)。

`dashboard.enabled: true` 后，本机浏览器访问 `http://127.0.0.1:8338/dashboard/`，可以看到四个页面：

![控制中心总览](docs/images/dashboard-overview.jpg)

| 页面 | 能看到什么 |
|------|------------|
| **总览** | KPI（成功调用含总数与缓存命中 / 失败 / 平均耗时）、系统状态（含熔断中数量）、运行配置、四个工具模块的观测状态、客户端用量归组 |
| **事件** | Provider 事件流（最近 20 条，独立页）：时间 / 来源 / 状态 / 耗时 / 结果数 / 主题语言 / 关键词 / 错误摘要 |
| **搜索源** | 每个来源的健康、最近 20 次结果、失败构成（如 `解析失败 ×4`）、成功率、平均 / P95 延迟、额度、最近错误与熔断倒计时 |
| **调用记录** | 工具与来源分列；可按层级 / 状态 / 工具 / 来源 / 错误类型筛选；工具行可点 request id 展开这次调用的来源链 |
| **设置** | 备份当前 YAML 后写入白名单配置（模式、超时、阈值、熔断时长等），密钥永不回显；外观主题与高级操作合并为「外观与维护」卡片 |

行为边界：

- **被动观测**：只记录真实 MCP 调用产生的元数据，不主动探测、不伪造数据；未调用过的工具也显示「可调用 · 尚无观测」。
- **失败分类 + 置信度**：失败归类为限流 / 验证码 / 访问被拒 / 超时 / 网络 / 解析 / 无结果；样本少于 5 次标记「样本不足」，连续 3 次失败才显示熔断，**只报告不干预调用**。
- **请求级调用链**：一次工具调用与其触发的来源事件共享 request id，回答「这次结果是谁给的、谁失败了」。

启用方式：把 `dashboard.example.yaml` 复制为主配置同目录的 `dashboard.yaml`（推荐），或在 `config.yaml` 追加 `dashboard:` 块：

```yaml
dashboard:
  enabled: true
  storage_path: ./data/dashboard.db
  retention_days: 30                          # 明细保留天数，每日汇总长期保留
  secrets_path: ./data/dashboard-secrets.json # 私密覆盖文件，接口不返回原值
  # admin_password / allowed_networks / quotas / brand 见 dashboard.example.yaml
```

首次 `start` / `install` 会自动生成这份文件并**默认启用**（含随机本机管理员口令）；不需要时改 `enabled: false` 或删除该文件并重启，运行时即回到零遥测开销。独立文件按字段覆盖主配置，删除它并重启即完整回退；管理员口令、访问网段、额度、品牌主题只能写在 `dashboard.yaml`，**WebUI 无法读取或修改**。重启后生效；桌面快捷方式（`install` 创建）双击即「lazy 启动」：先拉起服务再打开控制台。机器可读出口（只读，不触发搜索）：

```bash
curl http://127.0.0.1:8338/__admin/api/providers   # 每个来源的状态机与失败构成
curl http://127.0.0.1:8338/__admin/api/metrics     # Prometheus 文本指标
```

### 数据与隐私

- 只保存脱敏元数据：查询以哈希 + 主题 + 语言 + 关键词落库，**完整查询与 URL 不写入数据库**；错误文本在服务端先把 URL / 邮箱 / 疑似密钥替换为占位符再截断落库。
- 密钥保存在独立的私密覆盖文件，页面与接口**永不回显原值**——是服务端不出网，不是前端掩码；配置页内置各供应商「获取 Key」直达链接。
- 默认仅本机 loopback 可访问；`allowed_networks` 放行的网段**只读**。写操作（设置 / 密钥 / 重启 / 清缓存 / 额度管理）需 `dashboard.admin_password` 且仅限本机，口令不可经 WebUI 修改。
- 总览页「客户端用量」按接入的 MCP 客户端归组展示（识别来源：initialize 握手的 clientInfo 与 User-Agent，仅用于本机展示分组，不影响搜索）。
- 不发起任何主动探测。

完整配置项见 [docs/configuration.md](docs/configuration.md)。

---

## 文档导航

| 文档 | 内容 |
|------|------|
| [docs/installation.md](docs/installation.md) | 安装部署（二进制 4 平台 / GHCR linux amd64+arm64 / 源码 / 客户端注册）、运维与排障 |
| [docs/configuration.md](docs/configuration.md) | 完整配置参考、环境变量覆盖、默认值速查 |
| [docs/search.md](docs/search.md) | 搜索模式详解、引擎对照、相关性评分、MCP 工具参数 |
| [docs/architecture.md](docs/architecture.md) | 架构设计、回退链、代理检测、缓存、Go 模块嵌入、web-researcher 扩展 |
| [docs/api.md](docs/api.md) | Go Module API 与 HTTP API（MCP / SearXNG / Admin 端点） |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更日志 |

## 相关项目

- [web-researcher](https://github.com/daidaiJ/web-researcher) — 配套 Qwen Code 扩展，将网络调研卸载到子智能体，保持主模型上下文干净（详见 [docs/architecture.md](docs/architecture.md)）
