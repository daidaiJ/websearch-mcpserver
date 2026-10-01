# websearch-mcpserver — Agent Guide（省 token 版）

[English](AGENT_GUIDE.en.md) | [中文](AGENT_GUIDE.md)

Go 编写的 MCP 搜索服务：5 个工具（`smartsearch` / `academicsearch` / `cleanfetch` / `pdf_parser` / `file_search`），零 API Key 可用（`engine` 模式），HTTP daemon 或 stdio 两种接入。给人类看的完整版见 [HUMAN_GUIDE.md](HUMAN_GUIDE.md)。

> **反模式警告**：① 不要用 `cleanfetch` 抓搜索结果页来"搜索"——那是 `smartsearch` 的事；② 已持有 DOI / arXiv id 时不要拿标题再搜——直接把 id 作为 `academicsearch` 的 `query` 走精确查询；③ 不要给 `pdf_parser` 传网页 URL——网页用 `cleanfetch`。

## 部署速查（按优先级）

```bash
# 1. 下载二进制（linux-amd64 示例；其余平台见 installation.md）
curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
  | grep "browser_download_url.*linux-amd64\"" | cut -d '"' -f 4 \
  | xargs curl -sL -o /usr/local/bin/websearch-mcpserver && chmod +x /usr/local/bin/websearch-mcpserver

# 2. 跳过写配置：首次 start 未指定 -c 时自动生成预设 config.yaml（零 Key 即可运行）
#    显式 -c 指定路径时不会自动生成，文件缺失会报错

# 3. 启动（HTTP daemon，默认 127.0.0.1:8338）
./websearch-mcpserver start        # stop / kill / status / version 同级子命令

# 4. 注册客户端：url = http://127.0.0.1:8338/mcp（配置 auth_token 时带 Authorization 头）
```

**环境自检（30 秒）**：

```bash
curl -s http://127.0.0.1:8338/__admin/health   # 期望 {"ref_count":N,"message":"running"}
./websearch-mcpserver status                   # 状态、端口、引用计数
```

**stdio 替代**：无 HTTP 时下载 `websearch-mcp-cli-*`，由客户端直接拉起进程；`init` 子命令写示例配置，`version` 看版本。配置与 HTTP 共用同一 YAML。

部署细节（Docker / systemd / launchd / hooks 自启停 / 各客户端 JSON）：[installation.md](installation.md)。

## 工具速查

注册条件：`smartsearch` 需 `bing.enabled=true`；`academicsearch` 需 `academic.enabled=true`；`cleanfetch` / `pdf_parser` 各需同名 `enabled=true`。工具缺失先查这个。

### smartsearch

| 参数 | 必填 | 说明 |
|------|------|------|
| `query` | ✅ | 搜索关键词 |
| `time_range` | | 月数，默认 3；`0`=不限，`1`=近 1 月，`6`=半年，`12`=一年 |
| `fetch_top_n` | | 不传=只回标题摘要+URL；`0`=明确不要正文；`1-5`=为前 N 条抓取页面原文 |
| `intent` | | 仅 LLM 摘要启用时生效，未启用时自动移除 |

### academicsearch

| 参数 | 必填 | 说明 |
|------|------|------|
| `query` | ✅ | 关键词；或 DOI / arXiv id（`10.xxxx/...`、`2401.04085`、`doi:` / `arXiv:` 前缀、对应 URL 均可）触发单篇精确查询并忽略其余参数 |
| `engines` | | 子集：`arxiv` `crossref` `openalex` `pubmed` `europepmc` `dblp` `doaj` `semantic_scholar` `google_scholar` |
| `time_range` | | `year` / `month` / `week` / `day` |
| `page` | | 页码，默认 1 |

拿到结果里的 `pdf_url` 后直接传给 `pdf_parser` 读全文，不要用标题重搜。

### cleanfetch

| 参数 | 必填 | 说明 |
|------|------|------|
| `url` | 与 `urls` 至少一者 | 单个网页 |
| `urls` | 与 `url` 至少一者 | 批量抓取，合并去重后最多 5 个，单条失败不影响其它 |

### pdf_parser

| 参数 | 必填 | 说明 |
|------|------|------|
| `path` | ✅ | 本地 PDF 路径或 http(s) URL（学术结果的 `pdf_url` 可直接传入） |
| `pages` | | 页码范围（1-based），如 `1-10`、`1,3,5-7`；省略时只解析前 20 页并提示续读 |

原件超页或 MinerU 拒绝源 URL 时自动本地裁切所选页再上传（可 `mineru_page_batch_size` 分批、`mineru_page_budget` 限单次额度），分批进度经 MCP progress notification 实时推送；单次显式页数受 `max_pages`（默认 20）约束

## 任务 → 工具选型

| 任务 | 用什么 |
|------|--------|
| 联网查资料 / 新闻 / 时效内容 | `smartsearch`（配 `time_range`） |
| 需要页面原文而非 URL 列表 | `smartsearch` 带 `fetch_top_n: 1-5`，不必再调 `cleanfetch` |
| 找论文 / 按引用排序 | `academicsearch`（医学→`pubmed`+`europepmc`，CS→`arxiv`+`dblp`） |
| 已有 DOI / arXiv id | id 直接作 `academicsearch` 的 `query` |
| 读网页全文 / 批量读 | `cleanfetch` |
| 读 PDF / 论文全文 | `pdf_parser` |
| 已有 SearXNG / LiteLLM 工具链 | `/searxng/search` 兼容端点，零改动接入 |

## 机器契约

| 端点 | 说明 |
|------|------|
| `POST /mcp` | MCP 协议入口（streamable HTTP）；配置 `auth_token` 时需 `Authorization` 头 |
| `GET /searxng/search` | SearXNG 兼容 JSON，LiteLLM 直接可用 |
| `GET /__admin/health` | `{"ref_count":N,"message":"running"}`，远程可访问 |
| `GET /__admin/status` | 引用计数 / PID 等，仅本机 |
| `GET /__admin/api/providers` · `/api/metrics` | 来源状态机 / Prometheus 指标（只读） |
| `POST /__admin/refcount` · `/shutdown` | 引用计数 / 关停，仅本机 |

结果附带来源引擎与相关性分数（`show_meta` 控制）；缓存 6h 过期；限流默认 3/s、60/min。完整 API 见 [api.md](api.md)。

## 错误速诊

| 报错 / 现象 | 处置 |
|-------------|------|
| 工具没出现在客户端 | 注册条件未满足（见上），检查 `enabled` 配置后重启 |
| 「部分引擎本次失败，结果可能不完整」 | 预期降级：部分学术引擎超时/被反爬，结果仍可用；海外引擎需代理 |
| 结果为空 | 先 `curl /__admin/api/providers` 看哪个来源失败；`engine` 模式下 DDG 需代理，Google 默认禁用 |
| 学术搜索超时 | `network: china` 时海外引擎自动跳过；需全文可启用 `europepmc` / `dblp` / `doaj`（可直连） |
| `cleanfetch` 被反爬拦截 | 结果上会显式标注；配置 `jina.api_key` 走 Jina Reader 回退 |
| pdf_parser 提示截断 | 按 `pages` 传入页码范围继续读取 |

## 常见坑

- **Google 网页引擎默认禁用**：2025-01 起 JS 挑战全量硬化，UA/TLS 伪装全部失效——不要尝试启用或"修复"它。
- **DDG / arXiv 限流钳制是内置的**（DDG 1/s·6/min，arXiv 1/s·12/min + 3s 间隔），429 自动冷却避让——不要放宽，调整需实测校准。
- **显式 `-c` 指定配置路径时不会自动生成预设 config.yaml**，文件缺失直接报错；要自动生成就省略 `-c`。
- **`fetch_top_n` 与 `cleanfetch` 分工**：搜索时顺手要正文用前者（一次调用）；对已知 URL 读全文用后者。
- **日志在 stderr 与 `websearch.log`**（配置目录，默认 1MB 滚动），不会污染 stdio 模式 stdout 上的 JSON-RPC。
