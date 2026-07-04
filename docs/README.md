# Lework 文档规范与索引

## 命名规范

| 规则 | 示例 |
|------|------|
| 文件名统一使用 **kebab-case**（小写 + 连字符） | `agent-runtime.md` |
| 目录名使用 **kebab-case** | `architecture/` |
| 每个分类目录的入口文档命名为 `overview.md` | `architecture/overview.md` |
| 计划/规范类文件使用 `YYYY-MM-DD-description.md` 日期前缀 | `specs/2026-05-22-artifact-design.md` |
| 禁止 SCREAMING_SNAKE_CASE、UPPERCASE、Pascal_Snake | ❌ `ARCHITECTURE.md`、`TODO.md`、`Orbita_Arch.md` |

## 分类体系

| 目录 | 用途 | 面向读者 |
|------|------|----------|
| `architecture/` | 系统架构设计、组件间关系、设计原则 | 全体开发者 |
| `product/` | 产品需求、路线图、开发 TODO | PM、全体开发者 |
| `design/` | 技术方案设计、API 设计、存储方案 | 后端开发者 |
| `frontend/` | 前端架构、组件、状态管理、工程规范 | 前端开发者 |
| `operations/` | 运维排障、工程规范、项目结构索引 | 全体开发者 |
| `swagger/` | `make swagger` 自动生成的 API 文档 | API 调用者 |

## 文档索引

### 架构设计

| 文档 | 说明 |
|------|------|
| [architecture/overview.md](architecture/overview.md) | AI OS 架构设计（三核架构） |
| [architecture/backend.md](architecture/backend.md) | 后端包结构设计 |
| [architecture/agent-runtime.md](architecture/agent-runtime.md) | Agent Runtime 架构 |
| [architecture/workspace-artifact.md](architecture/workspace-artifact.md) | Agent 工作空间与产物设计 |
| [architecture/mq-subject.md](architecture/mq-subject.md) | MQ Subject 架构 |
| [architecture/system-design.md](architecture/system-design.md) | 系统架构设计 |
| [architecture/design-philosophy.md](architecture/design-philosophy.md) | 设计哲学 |

### 产品文档

| 文档 | 说明 |
|------|------|
| [product/prd.md](product/prd.md) | 产品需求文档 |
| [product/planning.md](product/planning.md) | 路线图规划 |
| [product/todo.md](product/todo.md) | 后端开发 TODO |

### 技术设计

| 文档 | 说明 |
|------|------|
| [design/tech-design.md](design/tech-design.md) | 技术设计（技能 Schema、渲染引擎） |
| [design/git-storage.md](design/git-storage.md) | 文件存储技术方案 |
| [design/presigned-url.md](design/presigned-url.md) | 预签名 URL 设计 |
| [design/ai-teammate-worker-deployment.md](design/ai-teammate-worker-deployment.md) | AI 队友维护与 Worker 动态部署技术设计 |

### 前端文档

| 文档 | 说明 |
|------|------|
| [frontend/overview.md](frontend/overview.md) | 前端架构总览 |
| [frontend/communication.md](frontend/communication.md) | 通信层架构 |
| [frontend/state-management.md](frontend/state-management.md) | 状态管理架构 |
| [frontend/components-layout.md](frontend/components-layout.md) | 组件与布局架构 |
| [frontend/core-mechanisms.md](frontend/core-mechanisms.md) | 核心机制详解 |
| [frontend/design-patterns.md](frontend/design-patterns.md) | 架构设计模式 |
| [frontend/engineering-standards.md](frontend/engineering-standards.md) | 工程规范 |
| [frontend/orbita-layout.md](frontend/orbita-layout.md) | Orbita 布局风格设计 |
| [frontend/todo.md](frontend/todo.md) | 前端待完成事项 |
| [frontend/ai-assistant/architecture.md](frontend/ai-assistant/architecture.md) | AI 助手子系统 - 架构规划 |
| [frontend/ai-assistant/data-model.md](frontend/ai-assistant/data-model.md) | AI 助手子系统 - 数据模型 |
| [frontend/ai-assistant/interaction.md](frontend/ai-assistant/interaction.md) | AI 助手子系统 - 交互规范 |
| [frontend/ai-assistant/roadmap.md](frontend/ai-assistant/roadmap.md) | AI 助手子系统 - 实施路线图 |

### 运维与规范

| 文档 | 说明 |
|------|------|
| [operations/troubleshooting.md](operations/troubleshooting.md) | 故障排除指南 |
| [operations/issue-labels.md](operations/issue-labels.md) | Issue 标签体系 |
| [operations/project-structure.md](operations/project-structure.md) | 项目结构与文件索引 |

### API 文档

| 目录 | 说明 |
|------|------|
| [swagger/docs.go](swagger/docs.go) | Swagger Go 集成代码（自动生成） |
| [swagger/swagger.json](swagger/swagger.json) | OpenAPI JSON 文档（自动生成） |
| [swagger/swagger.yaml](swagger/swagger.yaml) | OpenAPI YAML 文档（自动生成） |

## 新增文档指引

1. 确定文档所属分类，放入对应子目录
2. 文件名使用 kebab-case
3. 在本文档的索引表格中添加条目
4. 如新增分类，需在 `AGENTS.md` 和本文档同步更新
