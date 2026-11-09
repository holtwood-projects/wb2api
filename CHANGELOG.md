# 更新日志

wb2api —— 把 WorkBuddy/CodeBuddy 订阅包装为 OpenAI 兼容 API（非官方，仅供学习研究）。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 修复

- 修复独立 HTTP 服务的并发 map 崩溃：`sessions` 与 `traces` 是普通 map，却在多个 handler 中无锁读写，并发请求会触发 Go 运行时 `concurrent map read and map write`（`recover` 无法捕获，直接终止进程）。现由 `server.mu` 统一保护。
- 修复 `TraceCtx` 的数据竞态：同一会话的并发请求共享一个 `TraceCtx`，`NextMessage` 写 `RequestID`、`Apply` 读全部字段；现加内部互斥锁，并补充 `-race` 回归测试。
- 把包级全局 `affinityMap` 收敛为 `Manager` 实例字段：原实现下多个 `Manager` 各持不同锁却写同一张 map，存在竞态。

### 变更

- 插件元数据中的仓库地址由个人号 `holtwood/wb2api` 更正为组织仓库
  `holtwood-projects/wb2api`。
