# WebSearch 控制中心

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

维护本机 WebSearch 实例的操作者，需要在桌面或窄屏上判断服务健康、定位有问题的搜索源并安全调整参数。

## Product Purpose

控制台表面模式为 **Operate**。首屏优先展示调用量与多日趋势；异常和真实路由冷却提供次级直达入口。沿搜索源进入详情时保留筛选上下文。离开参数面板后仍能查看待保存变更。

## Operating Context

总规格 #30 规定监控、治理、系统三个工作区。Phase 0 仅演示总览和搜索源；所有指标与运维动作均为本地模拟。参考 [总规格](https://github.com/MattLYT/websearch-mcpserver-dashboard/issues/30) 与 [原型验收](https://github.com/MattLYT/websearch-mcpserver-dashboard/issues/31)。

## Product Principles

- 区分历史观测告警和当前路由阻断，指标注明时间窗口和口径。
- 配置编辑先入草稿，允许核对逐项差异、撤销和显式确认重启。
- 正式服务继续区分只读、运行态操作与验密管理。凭证零回显、遥测零查询原文。
- 发布界面须可离线内嵌，中英文可切换，配色保留品牌强调色。

## Accessibility & Inclusion

支持键盘进入、退出浮层与恢复焦点，提供可见焦点、文字状态、减少动效和浅暗主题。正式实现按 WCAG AA 验证。

