# websearch-mcp-cli-@PLATFORM@

`@PLATFORM@` 平台的预编译 [websearch-mcp-cli](https://www.npmjs.com/package/websearch-mcp-cli) 二进制，来自 [daidaiJ/websearch-mcpserver](https://github.com/daidaiJ/websearch-mcpserver) 的 GitHub Release（Go 单文件、零 API Key、无 CGO 的 MCP 搜索服务）。

这个包由 `optionalDependencies` 按 `os` / `cpu` 自动挑选，普通用户**不要**直接安装；请装 `websearch-mcp-cli`（通用 MCP 客户端）或 `dsh-websearch-mcpserver`（DeepSeek Harness 插件）。

`bin/websearch-mcp-cli*.sha256` 是同版本 Release 附带的校验和，可用 `sha256sum -c` 复验二进制来源。没有 postinstall / prepare 脚本，不在安装时联网或改权限。

二进制会发网络请求（搜索引擎 / 网页抓取），会落 SQLite 缓存与日志；`file_search` 会读本地目录索引。配置与信任边界见上游 [`docs/configuration.md`](https://github.com/daidaiJ/websearch-mcpserver/blob/master/docs/configuration.md)。

MIT。
