# Leros Agent 开发指南

本文档包含了 AI Agent 使用 Leros 代码库的重要信息。

## 构建/检查/测试 命令

### 构建命令
- `go build -o ./bundles/leros ./backend/cmd/leros/main.go` - 构建主 Leros 后端服务（输出到 `./bundles/`）
- `make docker-build` - 构建 Docker 镜像（标签：registry.yygu.cn/insmtx/Leros:latest）
- `make docker-run` - 在本地运行 Docker 镜像
- `make run` - 以前台模式启动 docker-compose 服务
- `make run-detached` - 以分离模式（后台）启动 docker-compose 服务
- `make stop` - 停止 docker-compose 服务
- `make logs` - 查看 docker-compose 服务日志
- `make swagger` - 生成 Swagger API 文档（输出到 `docs/swagger/docs.go`）

### 测试命令
- `go test ./...` - 运行项目中所有测试
- `go test -v ./...` - 以详细输出方式运行所有测试
- `go test ./backend/path/to/package` - 运行特定包的测试
- `go test -run ^TestFunctionName$ ./backend/path` - 运行特定测试函数
- `go test -race ./...` - 运行所有检测竞态条件的测试
- `go test -cover ./...` - 运行测试并显示覆盖率信息

### 检查命令
- `go fmt ./...` - 格式化所有 Go 代码
- `go vet ./...` - 检查所有 Go 代码中的常见错误
- `golint ./...` - 检查所有 Go 代码（通过 `go install golang.org/x/lint/golint@latest` 安装）
- `gofmt -s -w .` - 简化代码并写入更改（按照现有 Makefile）
- `staticcheck ./...` - 全面的 Go 静态分析（如已安装）

## 代码风格指南

### 导入组织
- 在标准库、第三方和项目特定包之间用空行分组导入
- 仅在防止命名冲突时使用语义导入别名
- 组织成三组：stdlib，第三方，内部包
```
import (
	"fmt"
	"net/http"
	
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	
	"github.com/insmtx/Leros/backend/config"
)
```

### 格式约定
- 使用制表符进行缩进，而不是空格（从现有 Go 文件验证过）
- 提交前执行 `go fmt ./...`
- 尽可能保持每行少于 120 个字符
- 使用 `gofmt -s` 简化代码

### 命名约定
- 对于导出的函数/类型使用驼峰命名法（`GetUser`，`UserService`）
- 对于未导出/内部函数/类型使用小写驼峰命名法（`getUser`，`userService`）
- 使用清晰、描述性的名称；优先考虑清晰度而不是简洁性
- 在包之间对类似概念使用一致的名称
- 与系统相关的变量应引用 Leros 概念

### 类型和接口
- 在第一次使用附近定义接口
- 保持接口小，通常是一个或几个方法
- 在适用时，用 "-er" 后缀命名接口类型（例如，`Runner`，`Handler`）
- 在不需要接口时，在函数签名中明确使用具体类型
- 当传递给函数且会被修改时，倾向于返回结构体的指针

### 错误处理
- 显式处理错误；不要忽略它们
- 在适当的情况下使用具体的错误类型并包装错误
- 遵循以下模式："if err != nil { return err }"
- 对简单的静态字符串使用 `errors.New()`
- 使用 `fmt.Errorf()` 和 `%w` 动词来包装带有更多上下文的错误
- 适当时记录错误上下文

### 附加准则
- 所有公共函数必须有 GoDoc 注释
- 注释应采用英文，并解释原因而非内容
- 在整个应用程序中维护一致的日志格式
- 适当地使用 context.Context 进行取消和请求作用域值
- 遵循依赖注入模式而不是全局变量
- 使用 Cobra 进行命令行界面实现，如 main.go 文件所示

### 强制约束
- **禁止使用 `panic`**：整个项目（含库代码和业务代码）严禁使用 `panic`。错误必须通过返回 `error` 逐层传递，由顶层调用方统一处理。对于不可恢复的致命错误（如配置缺失导致无法启动），应在 `main` 函数中使用 `log.Fatal` 退出。
- **禁止使用 `map[string]interface{}` 传递数据**：严禁在函数签名、接口定义或跨层通信中使用 `map[string]interface{}` 传递业务数据。必须定义具名结构体（struct）或类型化 map（如 `map[string]string`），以保证编译时类型安全和代码可读性。若现有接口（如 `Skill` 接口）使用了 `map[string]interface{}`，需在后续迭代中重构为强类型参数。

### 提交准则
- 遵循约定式提交格式：<type>(<scope>): <subject>
- 在 Leros 项目中使用中文作为提交消息
- 类型选项包括：
  - `feat`：新功能
  - `fix`：修复错误
  - `docs`：文档更新
  - `style`：代码风格调整
  - `refactor`：重构代码
  - `test`：测试相关
  - `chore`：构建工具或辅助工具变更
- 适当时，在正文部分包含技术实现和业务逻辑的详细描述

## 项目结构

- `/backend` - 主要 Go 应用程序代码
  - `/backend/cmd/leros` - 主 Leros 后端服务入口点
  - `/backend/config` - 配置加载和类型
  - `/backend/gateway` - HTTP 网关包
  - `/backend/interaction` - 事件驱动交互层
    - `/backend/interaction/connectors` - 渠道连接器（GitHub 已实现；GitLab，WeWork 桩代码）
    - `/backend/internal/infra/mq` - NATS JetStream 事件总线实现
    - `/backend/interaction/gateway` - 事件网关设置
  - `/backend/skills` - Skill 接口、类型和示例
  - `/backend/types` - 核心领域类型（DigitalAssistant，Event 等）
- `/bundles` - 构建输出目录（生成；已忽略 git）
- `/deployments/build/Dockerfile` - 容器构建配置
- `/deployments/dev/` - 开发环境配置和脚本
- `/docs` - 文档文件
  - `/docs/swagger/` - Swagger API 文档（唯一生成位置）
- `/frontend` - 前端应用

## 贡献说明

- 查阅 CONTRIBUTING.md 了解提交消息风格指导
- 在提交更改前确保所有测试通过 (`go test ./...`)
- 遵循 Go 的习惯用法和标准实践
- 实现时考虑组件如何融入架构文档描述的更广泛架构

## 文档索引

项目文档位于 `docs/` 目录：

> 重要参考：**[docs/PROJECT_STRUCTURE.md](./docs/PROJECT_STRUCTURE.md)** 包含完整的目录层级、关键文件说明和按任务场景的快速索引。处理不熟悉的任务时，建议先查阅该文件快速定位。

- `ARCHITECTURE.md` - AI OS 架构设计（核心架构文档）
- `PRD.md` - 产品需求文档（AI 工作协作系统）
- `SYSTEM_DESIGN.md` - 系统架构设计（平台引擎、知识检索、外部连接）
- `TECH_DESIGN.md` - 技术设计（技能 Schema、渲染引擎）
- `PLANNING.md` - 规划项（业务板块路线图）
- `TODO.md` - 后端开发 TODO（2周 MVP 计划）
- `TODO_v1.md` - 后端开发 TODO 清单（详细任务分解）
- `GITHUB_AUTH_SETUP.md` - GitHub OAuth 集成配置指南
- `GITHUB_WEBHOOK_TROUBLESHOOTING.md` - GitHub Webhook 签名验证问题排查
- `PR_EVENT_FLOW.md` - GitHub PR 事件处理流程验证清单
- `TROUBLESHOOTING.md` - 常见问题故障排除指南

## Swagger API 文档

Swagger 文档生成到 `docs/swagger/` 目录：

```bash
# 生成 Swagger 文档
make swagger

# 生成的文件位置
docs/swagger/docs.go        # Go 代码（用于 Gin 集成）
docs/swagger/swagger.json   # JSON 格式文档
docs/swagger/swagger.yaml   # YAML 格式文档
```

**注意：** Swagger 文档只保留在 `docs/swagger/` 目录，`docs/` 根目录不再生成重复文件。

## 核心组件和架构

基于 docs/ARCHITECTURE.md 中描述的 AI OS 架构，Leros 平台包含以下主要组件：

1. **Event Gateway** - 接收来自各种渠道的外部事件（✅ 已实现）
2. **Event Bus** - 用于解耦组件的消息队列系统（✅ NATS JetStream 已实现）
3. **Orchestrator** - 核心调度和协调机制（🔄 计划中）
4. **DigitalAssistant** - 表示 AI worker 的顶级抽象（✅ 类型已定义）
5. **Agent** - DigitalAssistant 中的决策实体（🔄 计划中）
6. **Skill** - 可调用的可重用功能（✅ 接口和基础实现已完成）
7. **Model Router** - 多提供商 LLM 路由（🔄 计划中）
8. **Memory System** - 短期和长期记忆（🔄 计划中）

## 技能系统定义

Skills 代表 Leros 中的核心构建块。`Skill` 接口在 `backend/skills/skill.go` 中定义：

```go
type Skill interface {
    Info() *SkillInfo
    Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
    Validate(input map[string]interface{}) error
    GetID() string
    GetName() string
    GetDescription() string
}
```

`SkillInfo` 包含技能的元数据：

```
skill.id
skill.name
skill.description
skill.version
skill.category
skill.skill_type       // local | remote
skill.input_schema
skill.output_schema
skill.permissions
```

嵌入 `BaseSkill` 以减少实现新技能时的样板代码。

### 技能类别

- **Integration Skills** - 外部系统集成（GitHub，GitLab，WeChat，Feishu，Jira）
- **AI Skills** - 基于 LLM 的推理能力（code_review, summarize, classification）
- **Tool Skills** - 实用功能（run_shell, execute_python, http_request）
- **Workflow Skills** - 复杂的协调操作（pr_review_workflow, bug_triage_workflow）

## 渠道集成

通过 `backend/interaction/connectors/connector.go` 中的 `Connector` 接口支持多个交互渠道：

- **GitHub** （✅ 已实现） - Webhook，事件解析，签名验证
- **GitLab** （🔄 桩代码）
- **Enterprise WeChat / WeWork** （🔄 桩代码）
- **Feishu** （🔄 计划中）
- **App / Webhook** （🔄 计划中）

每个渠道都实现了 `Connector` 接口：

```go
type Connector interface {
    ChannelCode() string
    RegisterRoutes(r gin.IRouter)
}
```

事件被标准化为 `interaction.Event` 类型并发布到事件总线（NATS JetStream）。

## 权限和安全

多级别精细权限控制：

- DigitalAssistant
- Agent
- Skill
- Tool

权限模型：RBAC + Capability

## Golang 引擎结构

当前实现的实际代码结构：

```
Leros/
│
├── backend/
│   ├── cmd/
│   │   └── leros/          # 主后端服务（HTTP + 事件网关）
│   │
│   ├── config/              # 配置加载和类型（GitHub app config，等）
│   │
│   ├── gateway/             # HTTP 网关（为未来路由的占位符）
│   │
│   ├── interaction/         # 事件驱动交互层
│   │   ├── connectors/
│   │   │   ├── github/      # GitHub webhook 连接器（✅ 已实现）
│   │   │   ├── gitlab/      # GitLab 连接器（🔄 桩代码）
│   │   │   └── wework/      # WeWork/企业微信 连接器（🔄 桩代码）
│   │   └── gateway/         # 事件网关路由器设置
│   │
│   ├── skills/              # Skill 接口，BaseSkill，SkillManager 接口
│   │   └── examples/        # 示例技能实现
│   │
│   └── types/               # 核心领域类型
│       ├── digital_assistant.go          # DigitalAssistant, AssistantConfig
│       ├── digital_assistant_instance.go # DigitalAssistantInstance
│       ├── event.go                      # Event (持久存储)
│       └── tables.go                     # 数据库表名常量
│
├── proto/                   # Protobuf 定义
├── gen/                     # 来源于 protos 的生成代码
├── frontend/                # 前端应用
├── deployments/             # Docker 构建配置
└── docs/                    # 文档
```

## 最小可行性产品 (MVP)

初始 MVP 专注于这些关键组件：

1. Event Gateway (✅ 已完成)
2. Event Bus / NATS JetStream (✅ 已完成)
3. Skill System 接口 (✅ 已完成)
4. GitHub 集成 (✅ webhook + 事件解析已完成)
5. Orchestrator (🔄 计划中)
6. Agent Engine (🔄 计划中)
7. CodeAssistantDigitalAssistant (🔄 计划中)

MVP 特性：

- PR 自动审查 (🔄 计划中)
- PR 自动摘要 (🔄 计划中)
- 问题自动回复 (🔄 计划中 - GitHub issue_comment 事件已支持)
- 代码解释 (🔄 计划中)

## 技术栈

当前和计划的技术栈：

| 组件 | 技术 | 状态 |
|-----------|-----------|--------|
| 语言 | Golang | ✅ 活跃 |
| HTTP 框架 | Gin | ✅ 活跃 |
| CLI 框架 | Cobra | ✅ 活跃 |
| 消息队列 | NATS JetStream | ✅ 活跃 |
| ORM | GORM | ✅ 活跃 (类型已定义) |
| 数据库 | Postgres | 🔄 计划中 |
| 向量存储 | Qdrant | 🔄 计划中 |
| LLM | OpenAI / Claude / DeepSeek | 🔄 计划中 |

## 开发工作流程

本节概述了为 Leros 项目做出贡献的标准开发工作流程。

### 标准开发流程

在开发新特性或修复问题时，请遵循以下步骤：

1. **与上游仓库同步** 以确保在开始开发之前拥有最新更改
2. **创建功能分支** 基于更新后的主分支
3. **提交和推送更改** 到您的个人派生仓库
4. **手动提交拉取请求** 通过 GitHub 网页界面到主仓库

### 详细步骤

#### 1. 与上游仓库同步

首先，确保您的派生仓库与上游仓库保持同步：

```bash
# 如果尚未添加，添加上游仓库
git remote add upstream https://github.com/insmtx/Leros.git

# 获取上游的最新更改
git fetch upstream

# 切换到主分支
git checkout main

# 合并上游更改
git merge upstream/main

# 推送更新后的主分支到您的派生
git push origin main
```

#### 2. 创建功能分支

基于更新后的主分支创建一个新分支用于开发：

```bash
# 创建并切换到新的功能分支
git checkout -b feature/descriptive-feature-name

# 或对于 bug 修复
git checkout -b fix/descriptive-fix-description

# 对于更具体的功能或增强
git checkout -b feat/scope-descriptive-name
```

遵循分支命名约定：
- `feat/` 用于主要功能
- `fix/` 用于 bug 修复
- `enhancement/` 用于现有功能的改进
- `docs/` 用于文档更改
- `refactor/` 用于不改变功能的代码重构

#### 3. 开发并提交更改

完成开发工作后，将更改提交到您的个人仓库：

```bash
# 添加您的更改
git add .

# 按照约定式提交格式提交带有正确格式的消息：
git commit -m "type(scope): concise description of changes"

# 将您的功能分支推送到您个人的派生
git push origin feature/descriptive-feature-name
```

提交消息格式遵循约定式提交规范：
```
type(scope): description

[optional body]

[optional footer(s)]
```

常见类型：
- `feat`: 新功能
- `fix`: 修复错误
- `docs`: 仅文档更改
- `style`: 不影响意义的更改（空白、格式、缺少分号等）
- `refactor`: 既不修复 bug 也不添加功能的代码更改
- `test`: 添加缺失测试或修正现有测试
- `chore`: 不修改源代码或测试文件的其他更改

#### 4. 提交拉取请求

当您的更改推送到您的个人仓库后：

1. 导航到原始 Leros 仓库的 GitHub 页面
2. 单击 "Pull Requests" 标签
3. 单击 "New Pull Request"
4. 选择 "Compare across forks"
5. 选择您的派生和功能分支作为比较分支
6. 验证差异中显示的更改
7. 按照项目的模板填写 PR 标题和描述
8. 提交拉取请求
