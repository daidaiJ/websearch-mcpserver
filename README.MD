# websearch-mcp
----
## 简介
websearch mcp server 一个golang 开发的轻量级web search 服务，目前暂时只支持baidu，后续预期支持tavily

## 配置文件
```yaml
port: 8338  # mcp server端口
baidu_sk: YOUR-BAIDU-SK  # 百度api sk  此页面获取，注意权限 https://console.bce.baidu.com/iam/#/iam/apikey/list
black_list_host:  # 屏蔽站点
  - "baijiahao.baidu.com"
  - "csdn.net" 
  - "csdn.com" 
  - "gitcode.com"
  - "gitcode.net"
  - "baidu.com"
```
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
claude mcp add websearch-mcp --type http --url http://localhost:8338
```
**配置文件**
用户目录下找到`.claude.json` 文件的mcpServers 字段，没有可以自行添加
```json
    "websearch-mcp": {
      "type": "http",
      "url": "http://localhost:8338",
      "timeoutMs": 5000
    }

```