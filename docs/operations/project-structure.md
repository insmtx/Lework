# Lework 项目结构

> 更新：2026-11-10 ｜ 对应 `AIO` 分支 ｜ 主线：个人用户、单用户多节点 OpenCode 控制面

## 入口

- `backend/cmd/lework/serve.go` — 控制面 `lework serve`
- `backend/cmd/lework/agent.go` / `agent_run.go` — 节点 agent `lework agent`
- `frontend/apps/control-plane/` — 浏览器界面

## 后端

- `backend/internal/controlplane/` — 节点注册表、事件中心、浏览器 API、agent 网关、SQLite 存储、内嵌界面
- `backend/internal/agentd/` — agent 守护、OpenCode 监管、HTTP/SSE 客户端、请求处理、运行器、WSS 传输
- `backend/internal/infra/` — SQLite、Git、日志等基础设施
- `backend/config/` — 控制面与 agent 配置结构

## 前端

- `frontend/apps/control-plane/` — 当前主线：节点总览、Session、Diff、文件、终端
- `frontend/apps/web/`、`frontend/apps/desktop/` — 旧产品，暂不维护

## 旧模块

以下模块来自旧企业版，控制面不依赖；与控制面冲突时直接删除：

- `backend/agent/`、`backend/tools/`、`backend/types/` — 旧 Agent Runtime 与工具系统
- `backend/internal/worker/` — 旧 Worker、调度、MCP 与事件流水线
- `backend/internal/modelrouter/`、`backend/internal/llm/` — 旧模型接入
- `backend/internal/api/`、`backend/internal/service/` — 旧 Web API 与业务服务
- `backend/internal/adapter/account/` — 旧 builtin / enterprise 认证

## 文档

- [../product/overview.md](../product/overview.md)
- [../architecture/overview.md](../architecture/overview.md)
- [../architecture/as-built.md](../architecture/as-built.md)
- [../operations/control-plane.md](control-plane.md)
