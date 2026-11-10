<div align="center">

<!--
  素材说明：
  - badge（license / go / web / website）：仓库内置自包含 SVG，见 docs/images/badges/，不依赖外网。
  - logo：docs/images/logo.png（原始素材见 docs/images/logo.svg）。
-->
<img src="./docs/images/logo.png" width="120" height="120" alt="Lework" />

# Lework

开源、可自托管、企业级品质的 AI 工作平台。

> **当前阶段只服务个人用户：单用户、多节点的 OpenCode 控制面。**

[![License](./docs/images/badges/license.svg)](./LICENSE)
![Go](./docs/images/badges/go.svg)
![Web](./docs/images/badges/web.svg)
[![Website](./docs/images/badges/website.svg)](https://lework.ai/)

**目录** · [当前阶段](#当前阶段) · [企业级愿景](#企业级愿景) · [一分钟理解](#一分钟理解) · [核心能力](#核心能力) · [系统架构](#系统架构) · [快速开始](#快速开始) · [开发指南](#开发指南) · [相关文档](#相关文档) · [贡献指南](#贡献指南) · [社区与支持](#社区与支持) · [许可证](#许可证)

</div>

---

## 当前阶段

用现代 GUI 管理多台机器上的 `lework agent`，由 agent 管理本机 OpenCode：

- 统一查看节点、项目目录、Session、工具调用与权限审批；
- 查看 Diff、浏览文件、打开 Session 级终端；
- 节点主动出站连接控制面，不暴露任何入站端口，也不需要 SSH；
- 执行与会话状态以 OpenCode 为准，Lework 不实现自己的 Agent Runtime。

**目标用户**：拥有 2 台以上机器（笔记本、工作站、服务器、DGX Spark）的独立开发者 / 个人研究者。

- 今天可用：[当前能力边界](docs/product/current-limitations.md)
- 路线图：[I0–I5](docs/product/roadmap.md)
- 产品定位：[产品概览](docs/product/overview.md)

## 企业级愿景

「企业级」描述的是工程质量、私有化与可自托管能力，它属于品牌与宣传层，**不代表当前已提供服务**。

组织、部门、RBAC、多租户、计费、内置知识库不在当前阶段范围，也不作为现成能力宣传。旧企业版阶段的文档已归档到 [docs/archive/](docs/archive/README.md)。

## 一分钟理解

```text
控制面（本机浏览器）
  ├── 节点 A：lework agent → opencode serve
  ├── 节点 B：lework agent → opencode serve
  └── 节点 C（DGX Spark）：lework agent → opencode serve

选节点 → 选项目目录 → 打开或新建 Session → 发任务
  → 实时看消息 / 工具 / 审批 → 看 Diff / 文件 / 终端
```

一个窗口管理多台机器，不需要为每台机器开一个 TUI。

## 核心能力

| 能力 | 说明 | 阶段 |
|---|---|---|
| 多节点注册与心跳 | agent 主动出站 WSS；断线重连；节点无入站端口 | 已实现 |
| OpenCode 进程管理 | agent 启动并监控本机 `opencode serve`，只绑定回环 | 已实现 |
| 统一 Session | 按目录创建 / 继续 / 中断 Session，查看消息、工具、审批 | 已实现 |
| Diff / 文件 / 终端 | 按本轮消息读 diff；目录内文件浏览与下载；Session 级 PTY | 已实现 |
| 多节点调度 | 跨节点选择执行位置 | I2 |
| Git 任务上下文 | A 项目、B 执行、结果回 A | I3 |
| 配置与 Action 控制面 | 插件、MCP、模型、Action 的读取、写入、回滚 | I4 |

## 系统架构

```text
Browser (Next.js: frontend/apps/control-plane)
    │ HTTP + SSE + WebSocket
    ▼
Lework Control Plane (Go + SQLite, `lework serve`)
    │ WSS outbound
    ▼
lework agent (`lework agent`) ──► opencode serve (127.0.0.1)
```

- 控制面：节点注册表、事件中心、浏览器 API、agent 网关、SQLite 存储；
- 节点侧：`lework agent` 负责注册、心跳、OpenCode 监管、Git、PTY 与本地代理；
- 数据真相：Session、消息、diff、权限、文件均以 OpenCode 为准。

详见 [架构概览](docs/architecture/overview.md) 与 [当前实现基线](docs/architecture/as-built.md)。

## 快速开始

### 环境要求

- Go 1.25+
- Node.js / pnpm（构建控制面界面）
- 目标节点已安装 `opencode`

### 构建

```bash
git clone https://github.com/insmtx/Lework.git
cd Lework
make build          # 构建 frontend/apps/control-plane 并生成 bundles/lework
```

只构建后端：

```bash
go build -o ./bundles/lework ./backend/cmd/lework/
```

未生成前端静态资源时，浏览器访问 `/` 会看到构建提示页，API 仍可用。

### 启动控制面

```bash
./bundles/lework serve \
  --listen 127.0.0.1:8787 \
  --db ~/.config/Lework/controlplane.db \
  --enrollment-token "$LEWORK_ENROLLMENT_TOKEN"
```

默认只绑定回环地址。浏览器打开 `http://127.0.0.1:8787/`。

### 启动节点 Agent

```yaml
# /etc/lework/agent.yaml
control_plane_url: ws://127.0.0.1:8787/api/agent/connect
enrollment_token: "<与控制面一致的令牌>"
node_name: dev-box
workspace_roots:
  - /home/me/projects
opencode:
  binary: opencode
  port: 4096
  service_mode: shared
```

```bash
./bundles/lework agent --config /etc/lework/agent.yaml
```

`workspace_roots` 限制可创建 Session 和读取文件的目录。完整协议与排障见 [控制面运维](docs/operations/control-plane.md)。

## 开发指南

### 仓库结构

```text
Lework/
├── backend/
│   ├── cmd/lework/              # serve / agent 等进程入口
│   ├── internal/controlplane/   # 控制面：节点、事件、浏览器 API、agent 网关、SQLite
│   ├── internal/agentd/         # 节点 agent：注册、OpenCode 监管、PTY、WSS 传输
│   ├── agent/ tools/ types/     # 旧企业版执行内核（控制面不依赖）
│   └── config/
├── frontend/
│   ├── apps/control-plane/      # 当前主线浏览器界面
│   └── apps/web, apps/desktop/  # 旧产品，暂不维护
├── deployments/
├── docs/                        # product / architecture / design / frontend / operations / archive
├── Makefile / go.mod / AGENTS.md / CONTRIBUTING.md / LICENSE
```

### 常用命令

```bash
make build                       # 构建控制面前端 + lework 二进制
make frontend-control-plane      # 只构建控制面静态界面
go test ./...                    # 默认测试
go test ./backend/cmd/lework ./backend/internal/agentd/...
go vet ./...
```

### 约定与注意

- **模块路径**：内部 import 使用 `github.com/insmtx/Lework/...`；
- **控制面不实现 Agent Runtime**：不要为控制面新增自建 runtime、任务状态机或组织权限模型；
- **OpenCode 是唯一真相源**：不要复制 session / message / event 状态；
- **节点无入站端口**：所有连接由 agent 主动出站；
- 更完整的工程约束见根目录 [AGENTS.md](AGENTS.md)。

## 相关文档

| 文档 | 内容 |
|---|---|
| [docs/product/overview.md](docs/product/overview.md) | 产品定位与核心概念 |
| [docs/product/roadmap.md](docs/product/roadmap.md) | I0–I5 路线与退出标准 |
| [docs/product/current-limitations.md](docs/product/current-limitations.md) | 当前能力边界（I1-B） |
| [docs/architecture/overview.md](docs/architecture/overview.md) | 目标架构 |
| [docs/architecture/as-built.md](docs/architecture/as-built.md) | 当前实现基线 |
| [docs/architecture/agent-runtime.md](docs/architecture/agent-runtime.md) | lework agent 与 OpenCode 边界 |
| [docs/operations/control-plane.md](docs/operations/control-plane.md) | 安装、启动、浏览器协议 |
| [docs/operations/troubleshooting.md](docs/operations/troubleshooting.md) | 排障 |
| [docs/operations/project-structure.md](docs/operations/project-structure.md) | 仓库结构索引 |
| [docs/README.md](docs/README.md) | 文档规范与索引 |
| [frontend/README.md](frontend/README.md) | 前端开发指南 |
| [lework.ai](https://lework.ai/) | Lework 官网 |

## 贡献指南

我们欢迎各种形式的贡献——Issue、文档、代码、使用反馈。

- **提 Issue**：前往 [GitHub Issues](https://github.com/insmtx/Lework/issues) 报告缺陷或提出功能建议；
- **提 PR**：Fork 本仓库 → 创建功能分支 → 提交变更 → 发起 Pull Request。建议先开 Issue 讨论设计；
- **开发约定**：动手前请阅读根目录 [AGENTS.md](AGENTS.md)（仓库结构、工程与代码规范）与上方[开发指南](#开发指南)。

## 社区与支持

- 使用问题 / 缺陷报告：[GitHub Issues](https://github.com/insmtx/Lework/issues)
- 功能需求与讨论：同上，请带 `feature` / `discussion` 标签发起
- 产品官网：[lework.ai](https://lework.ai/)

## 许可证

Lework 依据 **[Lework Open Source License](./LICENSE)** 授权：以 Apache License 2.0 为基础，附加若干商业使用条件（英文文本具有法律效力）。涉及多租户 SaaS、LOGO 与版权信息等特定情形时，需联系维护者取得商业许可，详见 LICENSE 文件。
