# 当前实现基线（I1-B）

> 基线：`AIO` 分支，2026-11。描述已实现的控制面 + agent，不描述 I2–I5。

## 进程与连接

- 控制面：`lework serve`，Go + SQLite，默认回环绑定。
- 节点：`lework agent --config ...`，主动出站 WSS 到 `/api/agent/connect`。
- OpenCode：agent 作为子进程管理 `opencode serve`，只绑定 `127.0.0.1`。

## 已实现能力

- 节点注册与心跳；离线请求返回 `409 node_offline`。
- Session 创建、prompt、interrupt。
- 消息 / 工具卡片 / 待处理权限请求。
- Diff（依赖 git 工作区快照）。
- 文件浏览 / 文本读取 / 二进制下载（单次 512 KiB）。
- Session 级持久 PTY，同源 WebSocket。

## 未实现

- 多节点调度、Git 跨机任务上下文、配置/Action 控制面、TUI 共享、Spark 实机。

## 代码入口

- `backend/cmd/lework/serve.go` — `lework serve`
- `backend/cmd/lework/agent.go` — `lework agent`
- `backend/internal/controlplane/` — 节点注册表、事件中心、浏览器 API、agent 网关、SQLite 存储、内嵌界面
- `backend/internal/agentd/` — agent 守护、OpenCode 监管、请求处理、运行器、WSS 传输
- `frontend/apps/control-plane/` — 浏览器界面
- `docs/operations/control-plane.md` — 安装与协议细节
