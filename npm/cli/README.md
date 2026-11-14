# websearch-mcp-cli (npm)

[websearch-mcpserver](https://github.com/daidaiJ/websearch-mcpserver) 的 **stdio CLI 二进制**的 npm 分发：Go 单文件、零 API Key、无 CGO，Node 只负责按平台定位并拉起预编译二进制。dsh 用户请改装 [`dsh-websearch-mcpserver`](https://www.npmjs.com/package/dsh-websearch-mcpserver)（一键插件）。

## 安装

```bash
npm i -g websearch-mcp-cli
websearch-mcp-cli version
```

或作为项目依赖安装后，在 MCP 客户端里指向 `node_modules/.bin/websearch-mcp-cli`。

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

无参数即 stdio MCP server；`init` 写示例配置，`version` 显示版本。日志走 stderr 与配置目录下的 `websearch.log`，不污染 stdout 的 JSON-RPC。完整客户端注册示例见 [`docs/installation.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/installation.md#stdio-cli-接入)。

## ⚠️ 零配置只是起点，请务必做个性化配置

不提供配置文件时只跑内存默认值（`mode: engine`），能搜但拿不到托管 MCP 级联、Key 池、学术源权重、抓取回退、代理适配和结果预算。**请自己读** [`docs/configuration.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/configuration.md) / [`docs/HUMAN_GUIDE.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/HUMAN_GUIDE.md)，**或让你的 agent 读** `docs/AGENT_GUIDE.md` + `docs/configuration.md` + `config.example.yaml`，按你的网络环境、Key 和主要用途生成一份专属 `config.yaml`；再通过 `WEBSEARCH_MCP_CONFIG`（shim 转成 `-c`）、Go 原生 `WEBSEARCH_CONFIG` 或直接 `-c` 传进去。

优先调：`mode`（零 Key `mcphub` / 有 Key `apipool` / 纯本地 `engine`）、`mcp_hub.preset`、`sk_list`、代理、`language`、`fetch_mode`、学术引擎开关、结果条数与超时预算、`cache`。

## 环境变量

| 变量 | 作用 |
|---|---|
| `WEBSEARCH_MCP_CONFIG` | 配置文件绝对路径，shim 转成 `-c <path>`；文件不存在直接报错 |
| `WEBSEARCH_CONFIG` | Go 原生配置路径覆盖，优先级高于 `-c` |
| `WEBSEARCH_MCP_CLI_BINARY` | 手动下载的二进制绝对路径，用于没有 npm 平台包的平台 |

## 平台与校验

`optionalDependencies` 按 `os` / `cpu` 只安装 `websearch-mcp-cli-<linux-amd64|windows-amd64|darwin-amd64|darwin-arm64>` 中的一个。包内 `bin/*.sha256` 与同版本 GitHub Release 的校验和一致。平台包还在 `bin/third_party/pdfium/<架构>/` 随附同版本 PDFium 动态库（固定 `chromium/8152`），本地 PDF 文本解析无需另行下载；库缺失或加载失败会自动回退纯 Go。**没有 postinstall / prepare 脚本**，因此不会触发 pnpm 10+ 的 `allowBuilds` 门禁，也不会在安装时联网下载。

`linux-arm64` / `windows-arm64` 请用 GHCR 镜像或手动下载 Release 二进制 + `WEBSEARCH_MCP_CLI_BINARY`。

## License

MIT。
