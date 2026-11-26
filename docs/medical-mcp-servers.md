# 医学 / 护理类免费托管 MCP 调研（2026-11-26）

> 目标：找出无需自建、可直接以 Streamable HTTP 接入，并提供免费额度的医学 / 护理 / 健康类 MCP，评估其作为 `websearch-mcpserver` 上游源的可行性。
>
> 结论：免费托管集中在医学文献与结构化健康数据；护理专属的免费托管 MCP 未发现。默认不并入 `mcphub` 级联，先作为可选 preset 评估。

## 1. 候选端点

| 服务 | 端点 | 免费方式与鉴权 | 覆盖内容 | 本次探测 |
|---|---|---|---|---|
| PubMed MCP（cyanheads） | `https://pubmed.caseyjhand.com/mcp` | 公开托管，无 Key；上游 NCBI 默认 3 次/秒 | PubMed / Europe PMC 检索、摘要、PMC 全文、MeSH、引用格式 | `initialize` 200 |
| Pipeworx PubMed | `https://gateway.pipeworx.io/pubmed/mcp` | 匿名 50 次/天；免费账号 200 次/天 | PubMed 检索、证据分布、发表趋势、撤稿完整性检查 | `initialize` 200 |
| usefulapi Healthcare | `https://usefulapi.io/category/healthcare/`；例如 `https://clinicaltrialsgov.usefulapi.io/mcp` | 每个 server 50 次/月；邮箱 + 6 位验证码 OAuth 登录 | ClinicalTrials.gov、DailyMed、openFDA、RxNorm、NPPES、Medplum 等 35 个托管源 | `initialize` 200 |
| EvidenceMD | `https://evidencemd.ai/mcp` | 连接免费，免费问答额度有限；OAuth 登录 | 带引用的临床问答、诊断/治疗、药物相互作用 | 未探测 |
| ChinaHealth | `https://chinahealth.info/api/mcp` | 免费、无鉴权、只读 | 来华就医医院、价格区间、签证/支付/病历流程 | `initialize` 超时 |

探测时间 2026-11-26，方法为向端点发送 MCP `initialize` 请求；HTTP 200 只表示端点可达，不代表工具调用或额度已完整验证。

## 2. 各端点说明

### 2.1 PubMed MCP（cyanheads）

- 仓库：<https://github.com/cyanheads/pubmed-mcp-server>
- 公共托管端点，无需安装；同一服务也支持 `npx @cyanheads/pubmed-mcp-server` 本地运行。
- 提供 12 个工具，覆盖 PubMed / Europe PMC 检索、按 PMID 取元数据、PMC / Europe PMC / Unpaywall 全文、引用指标、引用格式、相关文献、MeSH、DOI/PMID/PMCID 转换。
- 宿主端未声明鉴权；上游 NCBI E-utilities 免费，无 API Key 时默认 3 次/秒，自建时可配 `NCBI_API_KEY` 提升到 10 次/秒。
- 接入判断：**最适合无人值守级联**，无 OAuth、无 Key。属于社区托管，无 SLA；不要发送患者身份信息。

### 2.2 Pipeworx PubMed

- 仓库：<https://github.com/pipeworx-io/mcp-pubmed>
- Pipeworx gateway 的 PubMed pack；同一端点也暴露 `ask_pipeworx`、`discover_tools` 等约 30 个 gateway 元工具。
- 匿名调用上限 50 次/天；注册免费账号并配置 Bearer Key 后 200 次/天。
- 除常规 PubMed 检索外，还提供证据分布、发表趋势和撤稿/勘误完整性检查。
- 接入判断：适合需要比基础 PubMed 更强的检索/统计工具时使用；元工具会增加上下文占用，只接 PubMed 时优先用 pack 端点。

### 2.3 usefulapi Healthcare

- 目录：<https://usefulapi.io/category/healthcare/>，35 个托管 healthcare server。
- 代表端点：ClinicalTrials.gov `https://clinicaltrialsgov.usefulapi.io/mcp`、DailyMed、openFDA、RxNorm、NPPES NPI Registry、Medplum。
- 免费档 50 次/月（以具体 server 页面为准），通过邮箱 + 6 位验证码完成 OAuth 登录；不需要上游供应商 Key。
- 接入判断：适合人工在 Claude / ChatGPT / Cursor 中查结构化数据。OAuth 交互不适合当前无人值守的 `mcphub` 级联；50 次/月只够验证。

### 2.4 EvidenceMD

- 端点：<https://evidencemd.ai/mcp>。
- 提供临床推理回答，覆盖诊断、检查、治疗和药物相互作用，并给出逐条引用；面向临床医生。
- 免费连接，免费问答额度有限；更高额度为订阅制（第三方评测页面给出的 Individual 档为 50 次/月）。需要 OAuth 登录。
- 接入判断：商业服务，适合人工临床问答；OpenEvidence 没有官方 MCP，本项目不把社区浏览器桥接方案纳入评估。

### 2.5 ChinaHealth

- 端点：<https://chinahealth.info/api/mcp>，MCP Registry 名称 `info.chinahealth/chinahealth`。
- 免费、只读、无鉴权；工具覆盖治疗项目、医院、价格区间和签证/支付/病历流程。
- 内容面向来华就医信息，**不是临床知识库，也不构成医疗建议**；价格数据为 CC BY 4.0。
- 本次 `initialize` 在当前网络超时，接入前需自测出口连通性。

## 3. 护理场景的缺口

未发现专门面向护理、且免费托管的 MCP。主要原因是内容授权：

- **NANDA-I** 护理诊断、**CINAHL** 护理文献库、**UpToDate / Lippincott** 临床决策内容均为付费或机构订阅，没有公开免费 API。
- PubMed 收录大量护理期刊，可用 MeSH 缩小范围，例如 `Nursing Care[mh]`、`Nursing Diagnosis[mh]`、`Nursing Process[mh]`，或直接限定护理期刊名。
- 护理指南类内容可通过 NICE、WHO、CDC 等公开页面用 `cleanfetch` 读取；需要本地 MCP 时可自建 `medical-mcp`（NICE / WHO / PubMed，免费、本地运行）。
- 需要 MedlinePlus、PubMed、RxNorm、openFDA 的统一自建入口时，可部署 <https://github.com/GoogleCloudPlatform/hcls-mcp-servers>；它运行在自有 Cloud Run 项目中，托管与调用可能产生云费用，但数据源与代码免费。

## 4. 接入 `websearch-mcpserver` 的建议

1. **优先评估 cyanheads PubMed**：无鉴权、Streamable HTTP，最符合 `mcphub` 的上游形态；可作为默认关闭的可选 preset。
2. **需要更大免费额度或证据分析时再评估 Pipeworx**：匿名 50 次/天、免费账号 200 次/天；注意 gateway 元工具的上下文成本。
3. **usefulapi 与 EvidenceMD 只适合人工客户端**：OAuth 登录流程不适合服务端无人值守级联。
4. **不要默认开启**：医学内容垂直、免费额度小、涉及隐私与合规；默认关闭，由用户显式配置。
5. **隐私红线**：免费档通常没有 BAA / HIPAA 保障，不得把患者身份信息或可识别病历发送到这些托管端点；返回内容只作检索线索，结论需回原始指南/论文核对。

## 5. 未采纳或降级的候选

| 候选 | 原因 |
|---|---|
| Medical RAG（`mcp-registry-test-server-*.run.app`） | 注册表测试部署，名称即标注 test，不能作为生产临床源 |
| Carefluence | 企业级 FHIR 临床数据接入，未见免费托管档 |
| Google Health Developer MCP | 面向开发环境配置，非公开免费托管端点 |
| GoogleCloudPlatform/hcls-mcp-servers | 需自行部署 Cloud Run，属于自建而非托管 |
| CareJobs.Now MCP（`https://carejobs.now/api/mcp`） | 免费托管、无鉴权，但内容是医疗岗位与薪资，不是临床/护理知识 |
| npiwho MCP（`https://npiwho.com/mcp`） | 免费托管的美国 NPI 提供方查询，范围窄，可作为工具型补充 |
| Anthropic 官方 connectors（PubMed、CMS Coverage、ICD-10、NPI、Clinical Trials） | 稳定且随 Claude 订阅提供，但仅限 Claude 客户端，且需要付费订阅 |

## 6. 来源

- cyanheads PubMed MCP：<https://github.com/cyanheads/pubmed-mcp-server>（README，访问于 2026-11-26）
- Pipeworx PubMed：<https://github.com/pipeworx-io/mcp-pubmed>（README，访问于 2026-11-26）
- usefulapi Healthcare 目录：<https://usefulapi.io/category/healthcare/>；ClinicalTrials.gov 详情页：<https://usefulapi.io/clinicaltrialsgov/>（访问于 2026-11-26）
- EvidenceMD 医疗 MCP 评测：<https://evidencemd.ai/blogs/best-medical-mcp-servers>；Claude 医疗方案：<https://www.anthropic.com/news/healthcare-life-sciences>（访问于 2026-11-26）
- ChinaHealth：<https://mcp.so/servers/chinahealth>（访问于 2026-11-26）
- Google Cloud Healthcare MCP Servers：<https://github.com/GoogleCloudPlatform/hcls-mcp-servers>（访问于 2026-11-26）
- 国内医疗 AI Skills / MCP 合集：<https://github.com/JuneYaooo/awesome-medical-ai-skills-cn>（访问于 2026-11-26）

> 免费额度、鉴权方式和端点可用性会变化，接入前以服务方页面与一次真实 `tools/list` / 工具调用为准。本文是信息源调研，不构成医疗建议。
