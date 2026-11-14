# dsh-websearch-mcpserver

把 [websearch-mcpserver](https://github.com/daidaiJ/websearch-mcpserver) 装进 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)：一行命令得到零 API Key、国内可用的网页 / 学术搜索 MCP 工具。这是一个 **config-only Cordis bundle**，不含第三方 Cordis 插件代码，只用 `cordis.patch.yml` 把宿主自带的 MCP client 指向一个 Go stdio 二进制。

## 安装

```bash
dsh plugin --profile web add dsh-websearch-mcpserver
dsh --profile web --dump-config     # 应能看到 mcp-websearch 行
dsh web                             # web bundle 没有 HMR，必须重启
```

默认镜像若报 404，指定官方 registry 重试：

```bash
dsh plugin --profile web add dsh-websearch-mcpserver --registry=https://registry.npmjs.org
```

工具会以 `mcp__websearch__*` 出现：`smartsearch`、`academicsearch`、`cleanfetch`、`pdf_parser`、`file_search`（长结果用 `result_ref` + `cleanfetch` 分段读回）。

卸载：`dsh plugin --profile web remove dsh-websearch-mcpserver`。

## ⚠️ 装完只是「能跑」，最佳体验一定要做个性化配置

不写配置文件时，Go 二进制只跑内存默认值（`mode: engine`）：能搜，但拿不到托管 MCP 级联、Key 池、学术源权重、抓取回退、代理适配、语言与结果预算这些真正影响质量的部分。**同一个插件，默认配置和按场景调过的配置，体验差距很大。**

请二选一：

- **自己读**：[`docs/HUMAN_GUIDE.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/HUMAN_GUIDE.md) 的调优路线 → [`docs/configuration.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/configuration.md) 的完整配置参考 → [`config.example.yaml`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/config.example.yaml)。
- **让 agent 代读**（推荐，省时间）：把下面这段直接发给你的 agent：

  > 请阅读本项目 `docs/AGENT_GUIDE.md`、`docs/configuration.md` 和 `config.example.yaml`，先问清我的网络环境（是否需要代理）、有没有各家 API Key、主要用途（中文网页 / 学术论文 / 代码资料 / 深度调研），然后为我生成一份 `~/.config/websearch/config.yaml`，只写必要的覆盖项并逐条解释为什么这样选。不要开启我不用的引擎，不要写任何密钥明文进文件。

拿到 `config.yaml` 后，启动 dsh 前导出（**绝对路径**）：

```bash
export WEBSEARCH_MCP_CONFIG=/abs/path/to/config.yaml
dsh web
```

shim 会把它转成 `-c <path>`。Go 侧原生的 `WEBSEARCH_CONFIG` 也可用，优先级高于 `-c`，两者同时设置时别写不一样的值。也可以直接覆盖本 bundle 的 `mcp-websearch` 行，把 `-c` 加进 `args`（见下方「非 web profile」）。

值得优先调的几项：`mode`（零 Key 用 `mcphub` / 有 Key 用 `apipool` / 纯本地 `engine`）、`mcp_hub.preset` 与 `fallback_engines`、`sk_list` 多 Key 轮换、系统代理、`language`、`fetch_mode`、学术引擎开关与结果条数 / 超时预算、`cache`、`dashboard.enabled`。完整字段以配置文档为准，别照抄别人的配置。

## 非 web profile / 自定义启动参数

`cordis.patch.yml` 里的 shim 路径按 `$DSH_HOME/profiles/web/` 拼。装到别的 profile 时：

1. 启动 dsh 前 `export DSH_PROFILE=<name>`；或
2. 在该 profile 自己的 `cordis.patch.yml` 覆盖整行（后应用层**整体替换** config，字段要写全）：

```yaml
- insert:
    - id: mcp-websearch
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: websearch
        transport: stdio
        command: !!js process.execPath
        args:
          - !!js ((process.env.DSH_HOME || ((process.env.USERPROFILE || process.env.HOME) + '/.dsh')) + '/profiles/my-profile/node_modules/dsh-websearch-mcpserver/bin/websearch-mcp-cli.cjs')
          - '-c'
          - '/abs/path/to/config.yaml'
```

## 支持的平台

`linux-amd64`、`windows-amd64`、`darwin-amd64`、`darwin-arm64`，与 GitHub Release 四平台一致；按 `os` / `cpu` 通过 `optionalDependencies` 只下载匹配的那个二进制包，无需 postinstall，也不会触发 pnpm 10+ 的 `allowBuilds` 门禁。平台包在 `bin/third_party/pdfium/<架构>/` 随附同版本 PDFium 动态库（固定 `chromium/8152`），本地 PDF 文本解析无需另外下载；缺失或加载失败自动回退纯 Go。

`linux-arm64` / `windows-arm64` 没有 npm 平台包：linux-arm64 请用 GHCR 镜像，或下载 Release 二进制后用 `WEBSEARCH_MCP_CLI_BINARY=/abs/path/to/websearch-mcp-cli` 指向它。

## 权限与信任边界（如实声明）

- npm 壳只有一个 ~90 行的 Node 启动 shim，不发网络请求、不写文件、无生命周期脚本；实际的 Go 二进制来自同版本平台包，附 `.sha256`，可与该 tag 的 GitHub Release 校验和比对。
- Go 二进制**会**发网络请求（搜索引擎 / 网页抓取 / 可选托管 MCP 上游），**会**在配置目录落 SQLite 缓存与日志，`file_search` **会**读本地目录索引。不是「不联网、不读文件」的插件。
- 服务端自带 SSRF / DNS rebinding 防护、目录白名单与隐私默认（查询原文与密钥不落库）；打开 `file_search` 前请自己确认白名单。
- 子进程由 dsh 的 MCP client 拉起，日志写 stderr，不占 stdout 的 JSON-RPC；不开端口、不需要 `auth_token`。

## 不用插件的等价写法

任意 MCP 客户端（含 dsh 的 `.mcp.json` / `~/.dsh/mcp.json`）都可以直接用 npm 里的通用 CLI 包装包：

```bash
npm i -g websearch-mcp-cli
```

```json
{
  "mcpServers": {
    "websearch": {
      "command": "websearch-mcp-cli",
      "env": { "WEBSEARCH_MCP_CONFIG": "/abs/path/to/config.yaml" }
    }
  }
}
```

## License

MIT，与上游 Go 项目一致。
