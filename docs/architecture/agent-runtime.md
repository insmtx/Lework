# lework agent 与 OpenCode

> 更新：2026-11-10

## 职责

- `lework agent`：节点常驻进程，负责注册、心跳、OpenCode 进程监管、Git、配置、PTY、本地代理。
- OpenCode：唯一执行面与会话真相源；agent 不实现 Agent Runtime，不复制 session 状态。

## 生命周期

- systemd / launchd / Windows Service 只管理 `lework agent`。
- agent 启动并监控本机 `opencode serve`；OpenCode 只绑定回环。
- 控制面不直接连 OpenCode，所有操作经 agent 转发。

## 边界

- agent 不持有业务状态；控制面 SQLite 只存节点注册信息与 `last_seen`（项目索引 / UI 偏好为后续阶段）。
- 不允许把 API Key 写入日志、事件或 OpenCode 配置之外的位置。

## 代码位置

- `backend/cmd/lework/agent.go` — CLI 入口
- `backend/internal/agentd/supervisor/` — OpenCode 进程监管
- `backend/internal/agentd/opencode/` — HTTP/SSE 客户端
- `backend/internal/agentd/transport/` — WSS 传输
- `backend/internal/agentd/handler/` — 请求分发

## 相关

- [overview.md](overview.md)
- [as-built.md](as-built.md)
