# 当前能力边界（2026-11）

> 基线：`AIO` 分支，I1-B。以代码与 `operations/control-plane.md` 为准。

## 今天可用

- 控制面：`lework serve`，默认绑定回环，SQLite 存储。
- 节点：`lework agent` 主动出站 WSS 注册、心跳、断线重连。
- OpenCode：agent 管理本机 `opencode serve`，只绑定回环。
- Session：按工作目录创建、发 prompt、中断、查看消息/工具/权限请求。
- Diff / 文件 / 终端：按本轮消息读 diff；目录内文件浏览与下载；Session 级持久 PTY。
- 安全：enrollment token、workspace_roots 边界、节点无入站端口。

## 尚未实现

- 多节点调度（I2）与跨节点 Git 任务上下文（I3）。
- 配置 / Action / 插件 / MCP 的写入与回滚（I4）。
- TUI 与 Lework 共享同一 OpenCode service（I1 剩余）。
- macOS launchd / Windows Service 与 ConPTY（I2）。
- Spark USB-C 实机验收、桌面壳、自动更新（I5）。
- 组织、RBAC、多租户、计费、知识库：不在当前产品范围。

## 明确不做

- 自建 Agent Runtime。
- 复制 OpenCode 的 session/message/event 状态。
- 组织、部门、成员、邀请、审计、配额。

## 验证状态

- 以 `docs/operations/control-plane.md` 的 I1-B 说明为准；I0 结论见 `../design/2026-11-01-i0-findings.md`。
