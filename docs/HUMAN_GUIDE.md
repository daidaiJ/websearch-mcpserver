# HUMAN_GUIDE — 使用手册（人类阅读版）

[English](HUMAN_GUIDE.en.md) | [中文](HUMAN_GUIDE.md)

本手册面向**人类用户**：帮你决定装什么、选哪种模式、调哪些参数、出问题先看哪里。给 AI Agent 看的省 token 版见 [AGENT_GUIDE.md](AGENT_GUIDE.md)；项目概览见 [README](../README.md)。

## 目录

- [我该读哪篇文档](#我该读哪篇文档)
- [30 秒上手](#30-秒上手)
- [怎么选搜索模式](#怎么选搜索模式)
- [五个工具怎么配合](#五个工具怎么配合)
- [调优：先观测再动手](#调优先观测再动手)
- [排障速查](#排障速查)
- [安全与隐私默认值](#安全与隐私默认值)

---

## 我该读哪篇文档

本项目的详细文档按主题拆分，本手册是它们的导读入口：

| 我想…… | 读这篇 |
|--------|--------|
| 安装、注册到客户端、部署为常驻服务 | [installation.md](installation.md) |
| 改配置项、环境变量覆盖、查默认值 | [configuration.md](configuration.md) |
| 了解搜索模式、引擎、评分、工具参数细节 | [search.md](search.md) |
| 理解架构、回退链、代理检测、嵌入 Go 模块 | [architecture.md](architecture.md) |
| 直接调 HTTP API（MCP / SearXNG / Admin） | [api.md](api.md) |
| 启用本机控制中心（dashboard） | [dashboard.md](dashboard.md) |
| 参与开发、读懂代码结构 | [developers.md](developers.md) |

## 30 秒上手

```bash
# 1. 下载二进制：https://github.com/daidaiJ/websearch-mcpserver/releases
# 2. 启动（无需手写配置，无需 API Key；首次 start 自动生成预设 config.yaml）
./websearch-mcpserver.exe start
# 3. Windows 开机自启动（可选）：./websearch-mcpserver.exe install
# 4. 注册到 MCP 客户端（Claude Code / Qwen Code / Cursor）
```

客户端注册的具体 JSON 配置、Docker、systemd/launchd 常驻等部署方式见 [installation.md](installation.md#安装部署)。

## 怎么选搜索模式

`mode` 决定用哪些引擎，同一份配置随时可改（`config.yaml` 的 `mode` 字段）：

| 你的情况 | 建议 mode | 说明 |
|----------|-----------|------|
| 没有 / 不想注册任何 Key | `engine` | 百度网页 + Bing 并发，零配置可用（代理可用时自动加入 DuckDuckGo） |
| 只有百度千帆 Key | `baidu` | 千帆搜索失败自动回退百度网页搜索 |
| 只有一家海外供应商的 Key | `tavily` / `exa` / `anysearch` / `doubao` | 对应单一供应商模式，无 Key 时回退 Bing |
| 有多家 Key，想省额度自动切换 | `apipool` | 每次只调一家，失败自动换下一家，最后百度兜底 |
| Key 多、求结果覆盖面 | `hybrid` | 全引擎并发，多引擎共识参与评分 |

> 无 Key 时自动降级为 `engine`，不会报错罢工。各模式的引擎映射与切换链路见 [search.md](search.md#搜索模式)。

## 五个工具怎么配合

接入后你的 LLM 客户端会拿到 5 个 MCP 工具，覆盖一条检索工作流：

| 工具 | 干什么 | 典型用法 |
|------|--------|----------|
| `smartsearch` | 通用网络检索，多引擎融合 + 本地评分 | 日常联网问答、查新闻（`time_range` 控时效） |
| `academicsearch` | 9 大学术引擎并行检索 | 找论文；已有 DOI / arXiv id 直接作 `query` 精确查询 |
| `cleanfetch` | 抓取网页正文 | 读某篇文章全文；`urls` 可批量（最多 5 个） |
| `pdf_parser` | 解析 PDF（本地文本优先，扫描件回退 MinerU OCR；原件超页自动裁切分批） | 把 `academicsearch` 结果里的 `pdf_url` 直接传入读论文全文；超长文档用 `pages` 分次读 |
| `file_search` | 基于 Everything 索引毫秒级定位本地文件（Windows 专属，需装 Everything） | "帮我找本地那份 XX 的 PDF"；目录白名单限定检索范围 |

> 工具没在客户端里全部出现时，先检查注册条件（`bing.enabled` / `academic.enabled` / `cleanfetch.enabled` / `pdf_parser.enabled`；`file_search` 为接入时探测门控，需 Everything 在运行并启用 HTTP Server），见 [search.md](search.md#mcp-工具)。

## 调优：先观测再动手

**先看数据**：启用控制中心后看总览页与「搜索源」页（哪个来源失败多、延迟高、被限流），或机器读出口：

```bash
curl http://127.0.0.1:8338/__admin/api/providers   # 每个来源的状态机与失败构成
curl http://127.0.0.1:8338/__admin/api/metrics     # Prometheus 文本指标
```

**再动手**，常见诉求对应一个配置点：

| 想要 | 动哪里 |
|------|--------|
| 结果更多 / 更少 | `smartsearch.max_size` 与各引擎的 `max_size` |
| 结果不相关被夹带 | `smartsearch.relevance_threshold`（默认 0.05，调高更严）；或给回传 score 的引擎设 `min_score` |
| 转载站 / 镜像站扎堆 | `smartsearch.mmr.lambda` 调低（更偏多样性） |
| 某个引擎质量差 | 该引擎的 `min_score` / `max_size` 收紧，或 `engines` 子集里移除 |
| 要最新内容 | 工具参数 `time_range`（默认近 3 个月，`0` 不限） |
| 直接拿到正文而不是 URL | 工具参数 `fetch_top_n: 1-5`（服务端默认 `smartsearch.fetch_top_n`） |
| 学术结果太杂 | `academic.threshold`（默认 0.02，调高更严） |

全部参数与默认值见 [configuration.md](configuration.md)；评分管线原理见 [search.md](search.md#相关性评分)。

## 排障速查

| 报错 / 现象 | 性质 | 处置 |
|-------------|------|------|
| 自启动报 `0x800704C7` | 环境：SmartScreen / 杀软 / UAC 拦截 exe 启动，脚本本身没问题 | `Unblock-File .\websearch-mcpserver.exe` 解除锁定 → 杀软加白 → 前台跑一次看被拦的弹窗（详见 [installation.md](installation.md#自启动报-0x800704c7杀软--smartscreen-拦截)） |
| 「不是有效的 Win32 程序」 | 环境：下载损坏或架构不匹配 | 重新下载对应平台的二进制 |
| 启动后部分工具不可用 | 使用：注册条件未满足 | 检查各工具 `enabled` 与 `bing.enabled`（见上表） |
| 「部分引擎本次失败，结果可能不完整」 | 网络：部分学术引擎超时 / 被反爬 | 属预期降级；海外引擎需代理（系统代理自动检测），国内保持 `network: china` |
| Google / DuckDuckGo 零结果 | 网络：反爬 / 代理不可达 | 引擎自动跳过不影响其它来源；Google 默认禁用（反爬全量硬化），不要尝试伪装修复 |
| 搜索无结果或频繁限流 | 配置：`rate_limit` 或上游限流 | 默认 3/s、60/min；DDG / arXiv 引擎内置钳制，勿放宽，遇 429 会自动冷却避让 |
| 端口被占用 | 环境 | `./websearch-mcpserver status` 看是否已在运行；`kill` 后重启 |
| 结果过旧 | 缓存：SQLite 缓存 6h 过期 | 急用可删 `cache.storage_path` 文件后重启 |
| Docker 容器立即退出 / 设置页 403 | 部署 | 确认挂载了 `config.yaml`；桥接网络下控制台只读（写操作要求来源为 loopback） |
| `stop` 后进程仍在 | 正常：优雅退出最多等 10s | 仍在则 `kill` 强制结束 |

**总原则**：先看控制中心「搜索源」页或 `curl /__admin/api/providers` 确认是哪个来源、哪类失败（限流 / 验证码 / 超时 / 解析），再对照上表处理——环境问题（拦截 / 端口 / 传输损坏）与使用错误（配置 / 参数）分开判断，先查表，别盲目重装。

## 安全与隐私默认值

- **默认只监听 `127.0.0.1`**；需要开放网卡（`host: 0.0.0.0`）时建议配置 `auth_token` 保护业务端点。
- **搜索、评分、缓存全部在本地完成**：查询只发给搜索引擎本身，不经过任何第三方聚合服务。
- **密钥永不回显**：dashboard 页面与接口不返回 Key 原值；遥测只存脱敏元数据（查询哈希 / 主题 / 关键词），完整查询与 URL 不落库。
- **写操作有口令闸门**：控制中心的设置 / 重启等写操作仅限本机 + 管理员口令（只能配在 `dashboard.yaml`，WebUI 自身无法读取或修改）。

控制中心的全部配置项与边界说明见 [dashboard.md](dashboard.md)。
