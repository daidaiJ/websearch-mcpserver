# DuckDuckGo 结果页快照

`ddg-serp-2026-11-19.html` 是 2026-11-19 经本地代理从

```text
GET https://html.duckduckgo.com/html/?q=golang&kl=us-en
```

抓到的真实结果页，仅保留前 3 个结果节点的原始 DOM（`result__a` /
`result__url` / `result__snippet`、`uddg` 跳转与 `rut` 参数），供
`ddg_real_snapshot_test.go` 验证选择器、重定向解码与摘要提取。
`ddg_parse_test.go` 的内联合成 fixture 继续覆盖广告过滤与直链分支。

DDG 会按出口 IP 返回 HTTP 202 `anomaly` / `challenge` 验证页；同一出口
可能隔一段时间恢复。复采时如遇 202，换出口或等冷却后重试：

```bash
curl -sS --compressed \
  -c /tmp/ddg-cookies.txt -b /tmp/ddg-cookies.txt \
  -x http://127.0.0.1:7897 \
  -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36' \
  -H 'Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8' \
  -H 'Accept-Language: en-US,en;q=0.9' \
  'https://html.duckduckgo.com/html/?q=golang&kl=us-en' \
  -o ddg-serp-YYYY-MM-DD.html
```

保存前删除搜索词、cookie、脚本和无关节点，只保留结果结构；真实快照只
用于契约回归，不进入运行时。
