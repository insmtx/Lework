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
| [frontend/engineering-standards.md](frontend/engineering-standards.md) | 前端工程规范（如仍适用） |

### 运维与规范

| 文档 | 说明 |
|------|------|
| [operations/project-structure.md](operations/project-structure.md) | 当前仓库结构与边界 |
| [operations/troubleshooting.md](operations/troubleshooting.md) | 控制面与节点排障 |
| [operations/issue-labels.md](operations/issue-labels.md) | Issue 标签体系 |

### 归档

| 目录 | 说明 |
|------|------|
| [archive/README.md](archive/README.md) | 企业版阶段历史文档，仅历史参考 |

## 新增文档指引

1. 确定文档所属分类，放入对应子目录；已废弃内容放入 `archive/`。
2. 文件名使用 kebab-case；稳定文档不用日期前缀，计划/快照类使用 `YYYY-MM-DD-`。
3. 在本文档的索引表格中添加条目。
4. 如新增分类，需在 `AGENTS.md` 和本文档同步更新。
