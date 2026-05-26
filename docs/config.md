# 配置参考

## 配置文件路径

优先级（从高到低）：
1. 环境变量 `WEBSEARCH_CONFIG`
2. CLI 参数 `-c / --config`
3. 当前目录 `config.yaml`

> 通过 `-c` 指定后，PID 文件和日志文件自动写到配置文件所在目录。

## 完整配置

```yaml
port: 8338                  # MCP HTTP 端口
log_level: info             # debug / info / warn / error
mode: engine                # baidu / tavily / hybrid / engine
network: china              # china（跳过海外引擎） / international

# 屏蔽站点（对所有搜索引擎生效）
black_list_host:
  - "csdn.net"
  - "baidu.com"

# 百度千帆（mode=baidu 或 hybrid 时需要）
baidu:
  api_key: ""               # 环境变量: BAIDU_SK

# Tavily（mode=tavily 或 hybrid 时需要）
tavily:
  api_key: ""               # 环境变量: TAVILY_SK

# Bing 引擎（兜底 + engine 模式主力，无需 Key）
bing:
  enabled: true             # 总开关
  blocked: []               # Bing 专用屏蔽（与 black_list_host 合并）
  per_sec: 1                # 每秒限流
  per_min: 20               # 每分钟限流

# 学术引擎（无需 Key）
academic:
  enabled: true             # 总开关，开启后注册 academicsearch 工具
  bing_fallback: true       # 学术搜索用 Bing 兜底
  disable_arxiv: false
  disable_crossref: false
  disable_openalex: false
  disable_pubmed: false
  disable_semantic_scholar: true    # 需代理
  disable_google_scholar: true      # 需代理

# 代理（仅海外学术引擎）
proxy:
  enabled: false
  endpoint: "http://127.0.0.1:7897"

# LLM 摘要（可选）
llm:
  base_url: "https://api.openai.com/v1"   # 环境变量: LLM_BASE_URL
  api_key: ""                               # 环境变量: LLM_API_KEY
  model_id: "gpt-4o-mini"

# 缓存
cache:
  storage_path: "./data/search_cache.db"
  cleanup_interval: 30      # 清理间隔（分钟），最大 360

# Jina Reader（可选，启用 cleanfetch 工具）
jina:
  api_key: ""               # 留空则 cleanfetch 不注册
  base_url: ""              # 默认 https://r.jina.ai

# 日志滚动
log:
  max_size: 1               # 单文件最大 MB
  max_age: 1                # 保留天数
```

## 环境变量覆盖

| 环境变量 | 覆盖字段 | 说明 |
|----------|---------|------|
| `WEBSEARCH_CONFIG` | 配置文件路径 | 最高优先级 |
| `BAIDU_SK` | `baidu.api_key` | |
| `TAVILY_SK` | `tavily.api_key` | |
| `LLM_BASE_URL` | `llm.base_url` | |
| `LLM_API_KEY` | `llm.api_key` | |

> Viper 的 `AutomaticEnv()` 还支持 `APP_` 前缀覆盖任意配置项。

## 默认值速查

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `port` | 8338 | stop/kill/status 无配置时也用此端口 |
| `mode` | baidu | 无 Key 时自动回退 engine |
| `network` | china | |
| `bing.enabled` | true | |
| `bing.per_sec` | 1 | |
| `bing.per_min` | 20 | |
| `academic.enabled` | true | |
| `academic.bing_fallback` | true | |
| `proxy.enabled` | false | 启用后才初始化海外引擎 |
| `proxy.endpoint` | `http://127.0.0.1:7897` | |
| `cache.cleanup_interval` | 30 (min) | 最大 360 |
| 缓存过期 | 6 小时 | 基于最近命中时间，硬编码不可配置 |
| `log.max_size` | 1 (MB) | |
| `log.max_age` | 1 (day) | |

## 最小配置

```yaml
port: 8338
mode: engine
```

零 API Key 即可运行，使用 Bing + 学术搜索引擎。
