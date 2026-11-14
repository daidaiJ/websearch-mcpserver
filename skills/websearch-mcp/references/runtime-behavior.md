# 参数、实现与 Docker 行为

目录：版本基线 → 搜索参数 → 学术参数 → 正文/PDF → Docker 文件 → 缓存 → 源码依据。

## 版本基线

本页以 v3.7.0 为基线，并包含本分支正文/PDF 与时间缓存修复；其他部署可能尚无这些修复。项目参数及来源开关按当前工具 schema 和能力资源确认。部署配置、可用工具、来源与额度可能不同，升级后核验受影响项。

优先发现当前工具与 schema。本分支 `cleanfetch` 始终注册，URL 抓取需 cleanfetch.enabled=true，引用读取不受此开关限制；其他工具按实际条件注册；`smartsearch.intent` 只有服务端 LLM 配置完整时才出现。不要向未暴露该参数的工具传入它。常规调用不需要重新检查 Docker 或读取全部配置。

v3.7.0 新增的 `file_search` 依 Everything 探测注册，只有探测成功时才暴露。宿主支持 Resources 时，可按需读取 `search://capabilities` 和 `search://health`；它们只读、不占工具槽位，可被配置关闭。

## smartsearch：搜索、排序与补抓

- `query` 必填；`time_range` 是月数整数；`fetch_top_n` 可省略或为 0–5。省略后使用服务端默认，当前缺省为 0。工具没有选择网页引擎、分页、按域名过滤的独立参数；不要臆造 `engines/page/domains`。
- 真实流程：可选缓存 → 配置的主搜索 → 失败时 Bing → 过滤/融合/重排 → 前 N 项补抓 → 可选 LLM 摘要 → Markdown。部分 hybrid 引擎失败可能只写服务端日志；有结果不证明所有引擎都成功。
- 模式、结果数与评分由部署配置决定；排名是启发式相关性及多样性排序，不等于事实可信度。
- **时间范围**：本分支省略或 null 为 3 个月，显式 `0` 不发送时间筛选，负数拒绝；正月数按月×30换算天数，仅支持时间接口的 provider 接收。上游 v3.7.0 仍把 0 改成 3 月。Tavily 将 1–30 天映射到 day/week/month，超过 30 天统一映射 year；百度、豆包也使用离散枚举，Bing 回退及部分网页引擎无时间参数。省略筛选不保证上游没有隐藏默认或每条结果都有准确日期，历史资料仍核验原页日期。
- `fetch_top_n` 对最终列表前 N 项操作，不因某项失败或跳过而补足 N 篇。已有 `Content` 去空白后达到 **1,000 字节**即跳过抓取；这不是全文完整性检查。
- Tavily 默认请求 raw_content，Exa 默认请求 text，独立于本次 `fetch_top_n`。设 0 仅关闭额外补抓，不清空供应商正文；无须重复读取已有足够内容。
- 自动补抓与 `cleanfetch` 共用 WebFetch 和长文本输出机制，但自动补抓不走 Jina。未已有上层 deadline 时补抓阶段设置 15 秒预算；超时取消可能保留原摘要，不能只靠没有警告判断全文成功。
- `intent` 若存在，控制服务端摘要目的而非强制来源过滤。摘要是二次生成内容；精确引述仍需要原始段落。

v3.6.2 起的结果消费：

- 头部 `retrieved_at` 是检索时刻，缓存命中可含 `cache_age_seconds`；不是来源发表日期。日期来源 `structured/snippet/undated` 必须区别，snippet 仅定位，关键日期、金额、版本读原页核实。
- 尾部失败清单说明 timeout/rate_limit/challenge/off_topic 等，结果≤3 条时可附 `filter_diagnostics` 与阈值放宽提示。低条数不证明没有资料。`off_topic_guard=shadow` 默认只标疑似整桶、不丢弃；不能把 shadow 命中当过滤完成。
- 渲染结果超过 `smartsearch.inline_max_chars`（默认 32,768 字符）整体写 `search-*.md`，不是截断；响应保留候选标题、URL、来源和最多 160 字符摘录，以及统计、溯源头、失败清单和 `result_ref`。预览用于筛选，需要原文细节时用 `cleanfetch` 读取段落。搜索文件缺省保留 7 天，落盘时惰性清理。

## academicsearch：身份、分页与覆盖

- `query`、可选 `engines` 数组、`time_range` 字符串（`year/month/week/day`）、`page` 整数。省略时间为不限，page≤0 按 1；不能传网页检索的月数整数。
- 主题查询：选择引擎 → 并行检索 → 合并去重/评分 → 可选 Unpaywall 补 PDF → Markdown。DOI/arXiv 整串识别成功时走精确 lookup，返回至多一条并忽略时间、页码，但仍遵守来源开关与指定 engines；没有可用精确来源时不绕过禁用设置。编号嵌在长句内不保证进入精确分支。
- 引擎子集以实际暴露清单为准。未知名字会被过滤，全不匹配才报错；不要把拼错的子集当作已完整执行。
- `page` 传给每个引擎各自分页，再对本页结果融合。多数引擎每页 10 条，Crossref 为 20；总结果数不固定。跨页可能重复，使用 DOI、规范 URL 及题名/作者/年份识别同文。
- 日期语义有差异：arXiv 按提交日期，Crossref/OpenAlex/Europe PMC 有日期范围；Semantic Scholar/DBLP/DOAJ/Google Scholar 退化到年份，当前 PubMed 适配未应用时间过滤。按任务核对发表与修订日期，不把提交日当出版日。
- 排名包含 RRF、引用数、期刊、PDF 和新鲜度信号，不等于系统综述的完整覆盖或质量评价。元数据融合可能来自不同引擎，版本有疑问时回到原始页面核实。
- `PDFURL` 来自引擎，或在配置了 Unpaywall 邮箱且 DOI 存在、PDFURL 为空时补全。OpenAlex 最后一层可能使用 `open_access.oa_url`，它可能是落地页；Unpaywall 不会修正已有非空错误链接。
- 主题部分失败附尾部警告，全失败/无结果报错。配置项 `academic.bing_fallback` 在此版本未接入调用链，不能期待自动网页回退。

OpenAlex 匿名查询可能耗尽共享出口 IP 的每日预算而返回 429；缩小查询或短时重试不能恢复已耗尽的日额度。具体失败原因以当前响应与服务诊断为准。部署侧可选 `academic.openalex_api_key` / `OPENALEX_API_KEY`，不是工具参数，不在查询或对话中传凭证。DuckDuckGo 的验证挑战与真正无结果不同，成功的其他来源不证明它已恢复；查看失败诊断并按覆盖需要换来源。

## cleanfetch 与 pdf_parser：共享底层，不同选项

`cleanfetch` 支持互斥的 URL 和引用读取模式：URL 模式传 `url`/`urls`，引用模式只传 `result_ref` 及可选 `offset`/`limit`。URL 模式不能传分页字段，引用模式不能传 URL。示例采用第一条放 `url`、余下放 `urls`；handler 合并、去空白、去重后最多 5 项，超限报错而不是自动分批。批量结果按输入顺序分节，单条甚至全部失败都可能以正常工具响应返回，逐条检查 `抓取失败`。

三条抓取路径共享 URL 安全预检、HEAD 大小预检及 WebFetch：

| 入口 | 底层路线 |
|---|---|
| smartsearch 补抓 | `Fetch`，失败标注在搜索条目；不走 Jina。 |
| cleanfetch | `Fetch`，空正文、仅标题或标准 skip 导航视为失败；符合回退条件且 Jina 已配置时转正文模式。404/410、安全拦截与取消不回退；恢复后仍检查正文。 |
| pdf_parser | 远程先尝试已启用的 MinerU 精准分支，否则限额下载后本地提取；本地有 Token 时走精准归档，无 Token 时沿用文本提取。不走 Jina 回退，响应标明实际解析器。 |

WebFetch 按实际 Content-Type 处理 HTML、文本、Markdown、JSON、PDF；不是带 `PDF` 字段或调用 `pdf_parser` 就一定返回 PDF。GitHub blob 链接在底层转换为 raw 链接。

PDF 细节：

- `pages` 如 `1-3`、`1,3,5-7`，去重升序，1-based 文件页序。本地有 Token 的精准归档省略时目标为整本；远程 PDF 和无 Token 的本地文本提取省略时仍默认前 20 页，显式请求受配置上限约束。
- `cleanfetch`/自动补抓的普通远程 PDF 路径没有传入这个 20 页限制，锁定依赖的缺省上限是 200 页；不要用它们代替受控页码阅读或宣称全文覆盖。
- 远程 MinerU 优先分支同时要求 token、remote 开关，以及 URL path 以 `.pdf` 结尾（忽略大小写、query/fragment），或严格的 `arxiv.org/pdf/<论文编号及可选版本>`。其他无扩展名链接不自动视为精准 API 入口；不要为了切解析器擅自改 URL。`PDF 解析器` 提示区分 `pdf-lib`、`mineru-remote` 等实际路径；配置了 token 不等于本次用了 MinerU。
- v3.6.1 起，远程 MinerU 精准 API 的 `mineru_page_limit` 默认 600；不再用“第 201 页必裁切”判断。原件超页或远程 URL 被拒时下载（上限 200MB）、qpdf 裁出所选页并签名上传。`mineru_page_batch_size` 支持 10–200，0 或 1–9 不分批；`mineru_page_budget` 默认等于单任务页限。它们是服务端配置，不是工具参数，`max_pages` 也继续约束本次请求。
- 裁切批次串行上传；客户端提供 progress token 时经 MCP progress 推送进度，未请求时只返回最终结果。预算耗尽可返回已解析内容和续读页码。看实际页数、原页序和重新编号后继续，进度不等于收到正文。裁切或上传批次失败后可尝试受限文本回退，成功时明确标注 MinerU 失败与实际解析器；回退也失败则返回错误，不能以已成功页数宣称正文已交付。精准结果的 `images/` 从对应完整 ZIP 读取。轻量 OCR 仍最多 20 页/10MB、仅 Markdown，图片路径可能无法取回。镜像包含 qpdf。
- 本地有 MinerU Token 时，`pdf_parser` 走精准归档，独立于远程开关和 OCR 回退开关；该分支不会以普通文本提取冒充精准结果。没有 Token 的部署沿用本地文本提取及可选 OCR 回退。少量查阅可直接用已有 Markdown 或宿主离线阅读，避免不必要的上传。
- 普通远程 PDF 下载遵循服务抓取代理配置，同时执行 URL 与目标地址安全校验、重定向检查及下载大小限制；代理、额度和超时仍由部署配置决定，不是工具参数。
- 普通远程 WebFetch 输出超过 100 行或底层 100,000 字节阈值会落盘；它是返回方式切换，不是全文截断。Jina、MinerU 和直接本地 PDF 分支不保证使用相同落盘阈值。

### 本地精准归档

先按任务定位并抽查已有 Markdown 或解析包；抽查够用就复用，抽查不等于逐页验证。确需完整阅读或归档且已有资料不够时，调用 `pdf_parser(path=<本地PDF>)`，省略 `pages` 以整本为目标；显式 `pages` 仅处理所选原页码。

- 默认包目录为 PDF 旁的同名 `.mineru/`。保存 Markdown、图片等配套资源、各批原始 ZIP、来源指纹与原页码覆盖记录；无需每次下载 ZIP 才能看图片。
- 服务优先复用自己记录且对应当前 PDF 的结果；只提交缺失页。返回 `mineru-local`、实际保存页码、总页数和缺页提示，不能仅凭 `full.md` 文件名判断整本已完成。
- 按 `mineru_page_batch_size`/`mineru_page_budget` 分批；本地批大小为 0 时以单任务页限分批。当前任务中有缺页就再次传原 PDF 或包目录接续。等待中的批次先按返回的 `task_ref` 续查，已提交任务和已完成页不重传。遇到额度不足或明确失败，保留包路径与引用并报告；结束对话后下次也可从包目录接续。
- 包目录不可写会在上传前报错。由用户指定可写位置后，才传 `output_dir`（精确的包目录）；不自动改存服务目录。用包目录作为 `path` 或用 `task_ref` 时省略 `output_dir`。
- PDF 内容更新后，新结果成功保存才替换原包中的自动生成文件；额外放入的笔记等资料保留。服务不保留自动结果的长期历史版本，别把个人批注直接写进自动生成的文件。
- `result_ref` 是临时读取引用；包本身长期保留。引用过期或服务重启后，重新传原 PDF/包目录获得新引用，再用 `cleanfetch` 分段读，不重新解析。
- 精准归档会把所选页面上传 MinerU；`mineru_ocr: false` 不禁止该上传。未获授权上传的私密资料使用宿主离线阅读。

### MinerU 等待与原任务续查

先读取当前工具 schema，支持时才使用以下参数：

- `wait_seconds` 为每个 MinerU 任务的轮询上限，0-1800 秒。新解析默认 300 秒，可按需求设 900；不会增加页预算。轮询间隔从 2 秒逐步拉长至最多 30 秒，progress token 可接收状态与已知页进度。
- 额度剩余不保证解析时长。等待结束保留最后状态与 `task_ref`，远程任务仍可能继续；文本回退只代表本次提供了另一个解析器的文本。
- 需要精准结果时调用 `{"task_ref":"standard:<工具实际返回的ID>"}`；上传任务使用返回的 `batch:` 引用，轻量 OCR 使用 `agent:`。只传引用与可用项目标签，省略 `path/pages`，默认只查一次。任务完成后取回原结果，不提交新任务；服务重启后也可用保留的引用查询（仍需有效认证与上游任务留存）。
- 仍处理中时用有界等待（如 `wait_seconds:60`）续查，避免紧密循环。已授权的本地整本归档在当前任务中继续等待或补页，直到完成或遇到额度不足、明确失败等阻碍；远程阅读按当前证据需求决定是否继续。状态返回不等于正文，也不触发重新提交。
- 续查沿用原任务的选页，不能改变页码。上传引用只对应那一批裁切 PDF，页码从 1 重新编号，不代表其他批次已完成；按原文页码另行读取缺失批次。新页码会创建新解析任务，应按证据需求决定。

参考 [MinerU 官方 Skill](https://github.com/opendatalab/MinerU-Ecosystem/blob/main/skills/SKILL.md) 及其 [status 实现](https://github.com/opendatalab/MinerU-Ecosystem/blob/main/cli/mineru-open-api/cmd/status.go)：区分提交、等待和查询原任务。官方 Skill 的等待说明与 CLI 的缺省值有差异，以当前 MCP schema 和实际返回为准。

预算按整批准入，不会用剩余预算裁短下一批；例如预算 12 页、批大小 10、请求 1-15，可能只处理 10 页并提示续读 11-15。核对实际页数及文字质量，不把预算提示或 OCR 任务完成状态当作完整正文的证据。

## file_search：只定位本地文件

参数含 query、folder、max_results、page、time_format；按需用 match_case、whole_word、match_regex、match_diacritics 控制匹配，sort（name/date_modified/size/path）与 descending 控制排序，exclude 数组排除路径片段，min_alignment（0–1）覆盖对齐阈值。正则用 match_regex，不用 `**/` 限定目录。Everything 语法支持通配符与 `ext:/dm:/size:`，不是语义全文检索。返回文件名、路径和元数据，不读取内容；取得宿主路径后用宿主阅读能力。

默认每页 10 条、硬上限 20，page 上限 100；命中多用 page 翻页，保留查询/页大小。候选池硬限 600，深页需收窄查询，不承诺穷尽。噪声目录降权和词汇对齐过滤影响页内数量，totalResults 不等于最终相关命中数。

服务端白名单非空时强制限定并校验 folder；无白名单可能检索全盘索引。首个 MCP 请求探测 Everything 的地址和鉴权，失败则不暴露；修复依赖后需重启 WebSearch 重新注册。Linux 未显式配置 URL 时不探测，Docker 的 127.0.0.1 指容器自身，不是 Windows 宿主。

Everything 的版本、索引磁盘、地址和鉴权以当前部署为准；不假设全盘已索引或白名单为空。检索只返回路径与元数据，不改变正文读取权限或自动上传本地资料。Skill 不隐含授权安装插件、开放网络或下载；1.4/1.5 配置机制不同，见上游接入指导。

## Docker：从文件路径取得实际证据

`pdf_parser.path` 的本地路径必须对服务进程可见。Docker 返回的文件路径属于容器；例如 `/data/fetchdata` 可能位于 `/data` 卷内，不能假设宿主存在同名目录或已挂载文档。`file_search` 返回的是索引宿主路径。先确认本次部署和文件在宿主、服务中的实际可见路径；宿主本地 PDF 优先使用已有宿主阅读能力。路径失败时先检查服务所在位置，不因中文、空格或 `&` 猜测编码问题、反复重命名。

### 用户选定的本地 PDF

只有本次任务已授权复制所选文件且确需容器解析时，才采用单文件临时复制。私密 PDF 调用前确认允许上传；有 Token 的精准归档即使关闭 OCR 也会上传页面，未获授权时使用宿主离线阅读。不要自动挂载整盘或上传私密 PDF。

下面的 PowerShell 示例只准备一个已选定文件，容器名与源文件须替换为已确认值；检查失败时停止解析并清理本次临时文件：

```powershell
$container = 'websearch-mcpserver'
$source = 'D:\Research\selected.pdf'
$target = '/tmp/websearch-localpdf-' + [guid]::NewGuid().ToString('N') + '.pdf'
if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw '所选 PDF 不存在' }
try {
    docker cp "$source" "${container}:$target"
    if ($LASTEXITCODE -ne 0) { throw '复制失败' }
    docker exec $container test -r $target
    if ($LASTEXITCODE -ne 0) { throw '容器内不可读' }
} catch {
    docker exec $container rm -f -- $target
    if ($LASTEXITCODE -ne 0) { Write-Warning "需清理本次临时文件：$target" }
    throw
}
@{ path = $target; pages = '1-3' } | ConvertTo-Json -Compress
```

将最后生成的参数交给当前发现的 `pdf_parser`，按问题调整 `pages` 并按 Skill 规则补充可用的项目标签。保留本次实际容器名与 `$target` 值；解析成功、失败或取消后，都删除这一个临时文件。若执行工具每次创建新 shell，重新填写记录的实际值，不能假设变量仍存在：

```powershell
$container = 'websearch-mcpserver'
$target = '/tmp/websearch-localpdf-实际生成的标识.pdf'
docker exec $container rm -f -- $target
if ($LASTEXITCODE -ne 0) { throw '临时文件清理失败，需完成清理' }
```

仅删除本次生成并记录的临时路径，不删除源文件、不递归删除目录。临时复制不改变 PDF 的阅读权限；服务可能生成的文本、图像或 OCR 产物按其实际返回路径和保留策略处理。

### 服务返回的正文文件

本分支搜索、网页/PDF 落盘返回 `result_ref`。将引用原样传给当前发现的 `cleanfetch.result_ref`，从 `offset:0` 开始；读取 `content`，按 `next_offset` 续读到所需范围或 `eof:true`。偏移计 Unicode 码点，默认/0 页长 4096，上限 8192；JSON text 与 structured content 内容一致。保留来源 URL 与正文的关系；引用是凭据，不作为公开来源链接或永久引用。

引用跨 MCP 会话有效，`project` 仅统计标签，鉴权沿用服务端认证。只接受服务生成文件的引用，不接受路径或 URL。搜索引用最多 7 天，普通抓取按 `cleanfetch.file_ttl`（默认 24 小时），以返回过期时间为准。重启、过期、清理或检测到文件身份/大小/修改时间变化时返回 `result_unavailable`，重新调用原工具获取新引用；与 MinerU 的 `task_ref` 不同。结果目录由可信宿主维护，校验不对抗已有文件写权限且刻意保留元数据的修改者。

常规正文读取在 MCP 内完成。旧部署只有路径、没有引用或读取工具时，才在已有授权内按下面方式兼容读取；无法读取则明确证据缺口。示例路径须替换为**本次实际返回并核验的路径**：

```powershell
docker exec websearch-mcpserver sed -n '1,100p' '/data/fetchdata/实际返回的文件名.md'
docker exec websearch-mcpserver grep -n -F -- '目标词' '/data/fetchdata/实际返回的文件名.md'
```

Git Bash 会转换 Docker 命令中的 Linux 路径；访问 `/data` 等容器路径时，在对应命令前设置 `MSYS_NO_PATHCONV=1`。例如：

```bash
MSYS_NO_PATHCONV=1 docker exec websearch-mcpserver sed -n '1,100p' '/data/fetchdata/实际返回的文件名.md'
```

同样适用于 `docker cp` 的容器目标路径和上述 `/tmp` 文件的检查、清理命令。PowerShell 示例不需要该环境变量。

参数必须按宿主 shell 安全引用；不把返回文本拼接成 shell 脚本。只读取当前任务返回的具体文件，先核对路径在已知输出目录内，不遍历无关容器文件。读取工具提示中的 `read_file/grep_search` 不是 MCP 提供的额外接口。

权限不足时遵循宿主批准机制；无法读取时说明缺口，按需改用可用的正文读取工具。无需为日常读取修改 Docker。普通抓取文件缺省保留 24 小时，search-*.md 保留 7 天；旧路径失效时重新获取原 URL，不把临时路径当永久引用。

## 缓存：仅启用时适用

缓存是否启用以服务端配置为准，缺省关闭；项目统计标签不改变缓存或检索范围。

本分支网页缓存键包含 query、生效的 time_range 和 fetch_top_n，补抓及摘要沿用同一时间范围；不复用旧版无时间范围的网页缓存，旧记录按原策略清理。上游 v3.7.0 仍缺少网页 time_range。学术键含 query、time_range、engines，但缺少 page，翻页仍可能复用旧结果；学术缓存还不保留 EngineErrors，重复响应可能不再显示原部分失败警告。接口没有 bypass-cache 参数，不清缓存来完成普通查询；严格新鲜度或完整分页按证据缺口补充来源。本分支不持久缓存含临时文件提示的补抓结果，已有此类记录也不复用；普通长搜索缓存命中生成新文件和引用。旧部署的缓存文件路径仍可能过期。

## 源码依据与验证边界

本分支新增读取契约见仓库 [tool_result.go](../../../mcp/tool_result.go) 和 [MCP 回归测试](../../../mcp/result_delivery_test.go)。协议自动化不代表各宿主已自动选择读取工具；更新部署后仍需真实验收。

- [v3.7.0](https://github.com/daidaiJ/websearch-mcpserver/releases/tag/v3.7.0)、[v3.6.2](https://github.com/daidaiJ/websearch-mcpserver/releases/tag/v3.6.2)、[v3.6.1](https://github.com/daidaiJ/websearch-mcpserver/releases/tag/v3.6.1)、[v3.5.0](https://github.com/daidaiJ/websearch-mcpserver/releases/tag/v3.5.0)。
- [工具参数/注册](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/server.go)、[搜索实际时间与诊断](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/tool_search.go)、[搜索落盘](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/search_dump.go)。
- [学术 handler/缓存](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/tool_academic.go)、[学术编排](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/pkg/search/adapter/academic_adapter.go)。
- [正文/Jina](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/tool_cleanfetch.go)、[PDF handler/分批](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/tool_pdf.go)、[共享 WebFetch](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/pkg/fetch/webfetch/webfetch.go)、[默认配置](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/pkg/config/config.go)。
- [file_search 参数与过滤](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/mcp/tool_filesearch.go)、[Everything 接入指导](https://github.com/daidaiJ/websearch-mcpserver/blob/v3.7.0/skills/everything-http-server/SKILL.md)。

源码描述不是当前供应商可用性或 OCR 质量的证明；以本次实际返回与原文核验为准。历史成功记录不能替代当前部署的对应路径验证。
