#!/bin/bash
# Qwen Code SessionEnd Hook - 停止 websearch-mcp 服务
# 用法：配置到 .qwen/settings.json 的 SessionEnd 事件中

# 消费掉 stdin（Qwen Code 会传入 JSON）
cat > /dev/null

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"${SCRIPT_DIR}/websearch.sh" stop
