# Lework 设计哲学

Lework 的本质不是一个 AI 系统，而是一个"可调度的智能执行操作系统"，其核心是 Runtime、调度、能力抽象与事件驱动，而不是模型本身。

## 核心设计原则

### Runtime First, Model Second

Lework 的核心不是模型，而是 Agent Runtime。

设计约束：

- 业务代码中禁止直接调用 LLM API
- 所有模型调用必须经过 Runtime 调度层和统一上下文管理
- LLM 作为可替换的推理引擎，Runtime 作为不可替换的系统中枢

### Everything is a Capability

所有能力必须被标准化抽象，而不是散落在代码中。

统一抽象层级：

- **Tool** - 原子能力（最小执行单元）
- **Skill** - 组合能力（编排多个 Tool）
- **Agent** - 决策能力（选择 Tool/Skill）
- **Workflow** - 编排能力（复杂业务流程）

设计约束：

- 禁止硬编码的业务逻辑（隐式能力）
- 所有能力必须可注册、可发现、可调度

### LLM 是不可靠组件

模型不能被信任，只能被控制。

设计约束：

- 强制结构化输出（JSON Schema）
- 所有 Tool 调用必须校验参数和权限
- 禁止模型直接执行系统命令、写数据库、发请求（必须通过 Tool）

### Hybrid Architecture: Event-Driven + Function Call

采用分层混合架构：事件驱动负责系统级调度，函数调用负责局部执行。

架构设计：

- **Runtime 内部（同进程）** 使用函数调用 - 简单、高性能、易调试
- **Runtime 外部（跨服务）** 使用事件驱动 - 解耦、可扩展、支持复杂流程

事件流示例：

```
[Channel] -> [Event Bus] -> [Control Plane] -> [Execution Plane] -> [Agent Runtime] -> [Skill] -> [Tool]
```

设计约束：

- 跨进程/跨服务通信必须使用事件驱动
- 同进程内的局部执行使用函数调用
- 控制事件粒度，只在关键节点发事件（避免事件风暴）
- 实现幂等性和去重机制（事件可能重复或乱序）
- 全链路 Trace ID 支持（解决事件驱动调试困难问题）

### Event-Driven Core

Lework 的核心通信机制是事件，用于解耦模块和支持分布式架构。

> **核心原则**：所有模块之间只能通过 EventBus 通信

标准事件类型：

```
TaskCreated
StepScheduled
ToolInvoked
ToolCompleted
AgentDecided
TaskFinished
TaskFailed
```

设计约束：

- 模块间通过 Event Bus 通信
- 禁止跨模块直接调用函数
- 禁止隐式共享内存状态
- 实现最终一致性（接受中间状态）

### Scheduling > Execution

任何任务都必须先被调度，而不是直接执行。

执行流程：

```
Request -> Event -> Control Plane -> Runtime Manager -> Worker -> Execution
```

设计要求：

- 支持多实例抢占（Lease 机制）
- 支持优先级队列
- 支持重试策略

### Step-Based State Machine

Agent 执行采用步骤驱动的状态机，而不是循环执行。

Step 状态：

```
Pending -> Running -> Waiting -> Completed -> Failed
```

设计要求：

- Task 必须拆解为 Step
- 支持 DAG 依赖关系
- 支持并行执行和条件分支

### State Persistence

内存不是状态，数据库才是。

核心数据表：

- Task - 任务实例
- Step - 执行步骤
- EventLog - 事件日志

设计约束：

- 禁止用内存结构保存执行状态
- Worker 重启后不丢失任务状态

### Tool as Atomic Unit

Tool 是系统中唯一可以"做事"的单位。

统一接口：

```go
type Tool interface {
    Name() string
    Schema() JSONSchema
    Execute(ctx Context, input any) (output any, err error)
}
```

### Skill is Composition, Not Encapsulation

Skill 负责编排，不直接执行具体操作。

职责：

- 编排多个 Tool
- 控制流程逻辑

关系：`Agent → Skill → Tool`

### Agent Decides, Does Not Execute

Agent 是"大脑"，不是"手脚"。

Agent 职责：

- 选择 Tool/Skill
- 决定下一步执行

Agent 禁止：

- 操作系统
- 直接调用 API
- 写数据库

### Workflow as First-Class Citizen

Workflow 是比 Agent 更高层的抽象。

设计要求：

- 支持可视化编排
- 支持 DSL 描述
- 支持 DAG 执行

### Isolation First

每个 Task 必须在隔离环境运行。

隔离级别：

- 基础：Docker 容器
- 进阶：Firecracker / 沙箱

### Stateless Worker

Worker 可以随时被销毁重建。

设计约束：

- Worker 不保存本地状态
- 所有状态外部化（DB/Cache）

### Replayable System

任意任务必须可以回放。

记录内容：

- Step 输入
- Tool 输入输出
- Agent 决策日志

### Memory Layering

Memory 必须分层，不允许单一"context"。

分层结构：

- **短期记忆** - Prompt 上下文
- **会话记忆** - Session Memory
- **长期记忆** - 知识库/向量库

设计要求：

- 独立 Memory Service
- 支持检索（RAG）、写入、权限控制

### Capability-based Security

权限绑定在 Tool，而不是用户。

Tool 必须声明：

- 权限范围
- 可访问资源
- 是否危险操作

默认策略：Deny by Default（所有能力默认不可用）

### Three-Plane Security Model

Lework 采用三层权限安全模型：

```
Edge Runtime      → 高权限（本地）
Control Plane     → 中权限（调度）
Remote Runtime    → 低权限（执行）
```

核心规则：

- Remote Runtime 不得直接访问本地资源
- 所有敏感操作必须经过 Policy
- 全链路审计

### Plugin First

所有能力必须可插拔。

可插拔组件：

- Tool
- Skill
- Agent Runtime
- Channel（GitHub/飞书等）

### Channel Abstraction

统一输入模型，屏蔽渠道差异。

统一消息结构：

```go
type Message struct {
    Source   string // github / feishu / web
    User     string
    Content  string
    Metadata map[string]any
}
```

### Multi-Agent as Scheduling Problem

Agent 之间不直接调用，通过事件或任务分发。

通信方式：

- 通过 Event Bus
- 通过 Task 分发

### Observability First

全链路可观测是系统的基础能力。

观测维度：

- Task Trace
- Step Trace
- Tool 调用日志
- Agent 决策日志

技术选型：OpenTelemetry

## 架构定位

不把 Agent 当"更强的函数调用系统"，而是把 Lework 当"分布式智能执行操作系统"。

### 三平面架构

Lework 采用三平面分离架构：

- **Control Plane（控制面）**：决策中心，负责 Session 管理、Agent 路由、上下文构建
- **Execution Plane（执行面）**：执行中心，负责 Agent 推理、Skill 调用、Tool 执行
- **Edge Plane（边缘面）**：本地交互，负责本地文件访问、GUI 自动化、用户环境交互

### 核心能力

- **多 Agent 编排**
- **多 Runtime 执行**
- **本地 + 云协同**
- **企业级安全控制**