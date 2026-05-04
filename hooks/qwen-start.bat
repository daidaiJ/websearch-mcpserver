@echo off
REM Qwen Code SessionStart Hook - 启动 websearch-mcp 服务
REM 用法：配置到 .qwen/settings.json 的 SessionStart 事件中

REM 消费掉 stdin（Qwen Code 会传入 JSON）
>nul findstr "^"

set "SCRIPT_DIR=%~dp0.."
call "%SCRIPT_DIR%\websearch.bat" start
