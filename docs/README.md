# Lework 文档规范与索引

## 命名规范

| 规则 | 示例 |
|------|------|
| 文件名统一使用 **kebab-case**（小写 + 连字符） | `agent-runtime.md` |
| 目录名使用 **kebab-case** | `architecture/` |
| 每个分类目录的入口文档命名为 `overview.md` | `architecture/overview.md` |
| 计划/规范类文件使用 `YYYY-MM-DD-description.md` 日期前缀 | `design/2026-11-01-i0-findings.md` |
| 禁止 SCREAMING_SNAKE_CASE、UPPERCASE、Pascal_Snake | ❌ `ARCHITECTURE.md`、`TODO.md`、`Orbita_Arch.md` |

## 分类体系

| 目录 | 用途 | 面向读者 |
|------|------|----------|
| `product/` | 产品定位、路线图、当前能力边界 | PM、全体开发者 |
| `architecture/` | 目标架构、当前实现基线、模块边界 | 后端开发者 |
| `design/` | 技术验证与方案快照 | 后端开发者 |
| `frontend/` | 控制面浏览器界面架构与工程规范 | 前端开发者 |
| `operations/` | 安装、协议、排障、仓库结构索引 | 全体开发者 |
| `opencode/` | OpenCode V2 官方文档的中文整理，作为控制面 Agent 层扩展依据 | 控制面后端、集成、运维 |
| `archive/` | 历史文档，不维护，仅作参考 | 需要追溯历史的人 |

## 文档索引

### 产品

| 文档 | 说明 |
|------|------|
| [product/overview.md](product/overview.md) | 产品定位与核心概念：个人用户、单用户多节点 OpenCode 控制面 |
| [product/roadmap.md](product/roadmap.md) | I0–I5 路线与退出标准 |
| [product/current-limitations.md](product/current-limitations.md) | 当前实现能力边界（I1-B） |

### 架构

| 文档 | 说明 |
|------|------|
| [architecture/overview.md](architecture/overview.md) | 目标架构：控制面、lework agent、OpenCode 边界 |
| [architecture/as-built.md](architecture/as-built.md) | 当前实现基线：节点、Session、Diff、文件、PTY |
| [architecture/agent-runtime.md](architecture/agent-runtime.md) | lework agent 与 OpenCode 进程管理 |

### 技术设计

| 文档 | 说明 |
|------|------|
| [design/2026-11-01-i0-findings.md](design/2026-11-01-i0-findings.md) | I0 技术验证结论与限制 |
| [operations/control-plane.md](operations/control-plane.md) | 控制面与 agent 的安装、启动、协议 |

### 前端

| 文档 | 说明 |
|------|------|
| [frontend/overview.md](frontend/overview.md) | 控制面浏览器界面架构 |
| [frontend/engineering-standards.md](frontend/engineering-standards.md) | 旧 Web/Desktop 工程规范（历史，控制面见 overview.md） |

### 运维与规范

| 文档 | 说明 |
|------|------|
| [operations/project-structure.md](operations/project-structure.md) | 当前仓库结构与边界 |
| [operations/troubleshooting.md](operations/troubleshooting.md) | 控制面与节点排障 |
| [operations/issue-labels.md](operations/issue-labels.md) | Issue 标签体系 |

### OpenCode V2 参考资料

> 本目录是第三方资料整理，按内容主题使用中文文件名，是本文命名规范的**例外**；目录内字段、端点、事件名仍保留官方英文原名。

| 文档 | 说明 |
|------|------|
| [opencode/README.md](opencode/README.md) | 文档地图、核心结论与控制面边界 |
| [opencode/01-产品与架构.md](opencode/01-产品与架构.md) | client/server、状态归属、事件模型 |
| [opencode/02-安装与命令行.md](opencode/02-安装与命令行.md) | 安装、CLI、serve/pair/service |
| [opencode/03-配置体系.md](opencode/03-配置体系.md) | 配置位置、合并顺序、字段与热重载 |
| [opencode/04-Agent与子Agent.md](opencode/04-Agent与子Agent.md) | Agent 定义、模式、内置 Agent、子会话 |
| [opencode/05-模型与提供商.md](opencode/05-模型与提供商.md) | Provider/Model/Variant、缓存与传输 |
| [opencode/06-会话与消息模型.md](opencode/06-会话与消息模型.md) | Session/Inbox/Message、fork/move/revert |
| [opencode/07-工具系统.md](opencode/07-工具系统.md) | 内建工具、Code Mode、browser/session 工具 |
| [opencode/08-MCP服务器.md](opencode/08-MCP服务器.md) | local/remote MCP、OAuth、超时与资源 |
| [opencode/09-权限与策略.md](opencode/09-权限与策略.md) | permissions、审批流、policies 与优先级 |
| [opencode/10-技能-指令-命令.md](opencode/10-技能-指令-命令.md) | Skills、AGENTS.md、斜杠命令 |
| [opencode/11-上下文管理.md](opencode/11-上下文管理.md) | 压缩、快照、references、附件、warming |
| [opencode/12-HTTP-API参考.md](opencode/12-HTTP-API参考.md) | 认证、响应、SSE、端点总表 |
| [opencode/13-客户端-SDK-ACP.md](opencode/13-客户端-SDK-ACP.md) | 客户端 SDK 与 ACP |
| [opencode/14-插件开发.md](opencode/14-插件开发.md) | server 插件、hooks、工具注册、RPC |
| [opencode/15-控制面Agent层集成指南.md](opencode/15-控制面Agent层集成指南.md) | 落到 lework 的节点/控制面集成方案 |
| [opencode/16-故障排查与运维.md](opencode/16-故障排查与运维.md) | service、日志、代理、升级 |
| [opencode/17-V1到V2迁移.md](opencode/17-V1到V2迁移.md) | 破坏性变更与迁移对照 |
| [opencode/18-Console与Go.md](opencode/18-Console与Go.md) | Console、workspace、Go 订阅与 harness |
| [opencode/19-控制面能力对齐清单.md](opencode/19-控制面能力对齐清单.md) | 控制面 vs OpenCode 能力逐项对照与缺口优先级 |
| [opencode/20-控制面开发优先级与计划.md](opencode/20-控制面开发优先级与计划.md) | 控制面能力优先级排序、模块拆分与开发计划 |
| [opencode/附录A-配置字段速查.md](opencode/附录A-配置字段速查.md) | 49 个顶层配置字段与实验项 |
| [opencode/附录B-事件与权限动作速查.md](opencode/附录B-事件与权限动作速查.md) | 96 个业务事件、权限 action 与策略 |

### 归档

| 目录 | 说明 |
|------|------|
| [archive/README.md](archive/README.md) | 企业版阶段历史文档，仅历史参考 |

## 新增文档指引

1. 确定文档所属分类，放入对应子目录；已废弃内容放入 `archive/`。
2. 文件名使用 kebab-case；稳定文档不用日期前缀，计划/快照类使用 `YYYY-MM-DD-`。
3. 在本文档的索引表格中添加条目。
4. 如新增分类，需在 `AGENTS.md` 和本文档同步更新。
