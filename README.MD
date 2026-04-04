# websearch-mcp
----
## 简介
websearch mcp server 一个golang 开发的轻量级web search 服务，支持 baidu、tavily 以及 hybrid（混合）模式。

## 搜索模式
通过配置文件中的 `mode` 字段指定搜索模式：
- `baidu`（默认）：仅使用百度千帆 AI Search API
- `tavily`：仅使用 Tavily Search API
- `hybrid`：并发请求 baidu 和 tavily，按 URL 去重后合并结果

## 配置文件
```yaml
port: 8338  # mcp server端口
mode: hybrid  # 搜索模式: baidu / tavily / hybrid（默认 baidu）
baidu_sk: YOUR-BAIDU-SK  # 百度api sk  此页面获取，注意权限 https://console.bce.baidu.com/iam/#/iam/apikey/list
tavily_sk: YOUR-TAVILY-SK  # tavily api key  https://app.tavily.com/home

# LLM配置（可选，用于摘要生成）
llm:
  base_url: "https://api.openai.com/v1"  # LLM API基础地址，支持环境变量 LLM_BASE_URL
  api_key: "sk-xxx"                     # LLM API密钥，支持环境变量 LLM_API_KEY

# 缓存配置（可选）
cache:
  storage_path: "./data/search_cache.db"  # SQLite数据库路径
  cleanup_interval: 30                    # 清理间隔（分钟，默认30，最大360）

black_list_host:  # 屏蔽站点（仅对 baidu 有效）
  - "baijiahao.baidu.com"
  - "csdn.net" 
  - "csdn.com" 
  - "gitcode.com"
  - "gitcode.net"
  - "baidu.com"
```

> 所有配置均支持环境变量覆盖：
> - `BAIDU_SK` - 覆盖`baidu_sk`
> - `TAVILY_SK` - 覆盖`tavily_sk`
> - `LLM_BASE_URL` - 覆盖`llm.base_url`
> - `LLM_API_KEY` - 覆盖`llm.api_key`

## 高级功能

### LLM摘要生成
当配置了有效的LLM API后，系统会自动为搜索结果生成带引用标注的专业摘要。

### 缓存系统
启用缓存后可大幅减少重复搜索的API调用：
- 基于SQLite的持久化缓存
- 支持查询和意图的双重缓存键
- 自动清理过期记录（默认30分钟）
- 并发安全设计

## 构建
```shell
git clone --depth 1 --branch master https://github.com/daidaiJ/websearch-mcpserver.git
# 进入项目目录
cd websearch-mcpserver
docker build -t websearch:v1 .
```

## 部署
可选 compose 部署
```yaml
services:
  websearch:
    # 使用上一步构建的 websearch:v1 镜像
    image: websearch:v1
    # 容器名称，方便管理
    container_name: websearch-service
    # 重启策略：容器退出时自动重启（可选，根据需求调整）
    restart: always
    # 挂载外部配置文件到容器内 /app/ 目录
    volumes:
      - /your-config-dir/config.yaml:/app/config.yaml
    ports:
      - "8338:8338"  # 宿主机8080端口映射到容器8080端口
    # 可选：环境变量（如果服务需要）
    environment:
      - TZ=Asia/Shanghai  # 设置容器时区

```
可选docker 直接部署
```shell
docker run -d \
  --name websearch-service \
  --restart unless-stopped \
  -v /your-config-dir/config.yaml:/app/config.yaml \
  -p 8338:8338 \
  -e TZ=Asia/Shanghai \
  websearch:v1
```

## 配置
以claude 为例，任选一种
**命令行**
```shell
claude mcp add  --transport http websearch-mcp   http://localhost:8338/mcp
```
**配置文件**
用户目录下找到`.claude.json` 文件的mcpServers 字段，没有可以自行添加
```json
    "websearch-mcp": {
      "type": "http",
      "url": "http://localhost:8338/mcp",
      "timeoutMs": 5000
    }

```
**删除**  `claude mcp remove websearch-mcp -s user`

## litellm
配置
```yaml
search_tools:
  - search_tool_name: searxng-search
    litellm_params:
      search_provider: searxng
      api_base: http://websearch:8338/searxng # 以searxng 格式提供websearch 服务
```

## 升级说明

### v1.x → v2.x 重大变更
1. 增加了Tavily 搜索api支持
2. 扩展了llm 摘要支持，建议使用快速模型，减少时间延迟的同时降低成本
3. 通过sqlite 对搜索及摘要结果做了缓存管理