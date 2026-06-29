# Leros 后端架构设计文档

> 面向 AI OS 的 Golang 包结构指南
>
> **版本：4.0** | **最后更新：2026-06-28**

## 1. 概述

本文档提供 Leros 后端的 **Golang 包结构设计**，与 `ARCHITECTURE.md` 配合使用。

- `ARCHITECTURE.md` - 高层架构设计、模块划分、执行链路
- `ARCHITECTURE_BACKEND.md` - **本文档** - Go 包结构、目录组织

## 2. 设计原则

### 2.1 架构定位：契约驱动服务架构

> **Contract-driven Service Architecture**

Leros 采用**契约驱动的服务架构**，而不是：

- ❌ Clean Architecture
- ❌ DDD
- ❌ Hexagonal Architecture

**特点：**

- ✔ 类 RPC 风格（类 RPC，但不绑定 RPC 实现）
- ✔ 轻抽象（无 Repository 层）
- ✔ 高工程效率
- ✔ 适合 Agent / Workflow / OS 类系统扩展

**核心原则：**

1. **contract 定义能力** - 系统能力的"语言"
2. **service 实现能力** - 直接操作 DB
3. **handler 适配输入输出** - HTTP 适配
4. **db 提供执行能力** - 真实数据库操作

### 2.2 按"领域分层"，不是按技术分层

> ❌ 旧模式：controller / service / dao / model
> ✅ 新模式：event / execution / agent / skill

**原因：**

- 技术分层导致模块间耦合严重
- 领域分层让每个模块职责清晰、可独立演进

### 2.3 核心引擎必须"内聚 + 可替换"

- Event Engine 可以单独部署
- Execution Engine 可以替换
- Agent Runtime 可扩展

### 2.4 强制隔离（Enforced Isolation）

| 目录        | 用途         | 访问控制                                |
| ----------- | ------------ | --------------------------------------- |
| `internal/` | 私有核心代码 | Go 编译器强制隔离，只能被本项目内部引用 |
| `pkg/`      | 对外公开接口 | 其他项目可安全导入                      |

## 3. 推荐的 Golang 包结构

### 3.1 完整目录结构（推荐版本）

```bash
backend/
├── cmd/leros/                 # composition root 与进程生命周期
├── agent/                     # 独立、业务无关的执行层
│   ├── executor.go
│   ├── registry.go
│   ├── runtime.go             # ExecutionRequest / Result / Runtime
│   ├── result.go              # 唯一 agent.Event
│   ├── tool.go                # Tool / approval / question
│   └── runtime/
│       ├── native/
│       ├── claude/
│       ├── codex/
│       ├── opencode/
│       ├── externalcli/
│       ├── provider/
│       ├── events/            # 活动 payload、构造器、Sink
│       └── todo/
├── internal/
│   ├── api/                   # HTTP adapters 与 API contract
│   ├── assistant/             # SingerOS 业务包装、PreparedRun、Journal
│   ├── worker/
│   │   ├── command/           # WorkerCommand adapters
│   │   ├── run/               # RunCoordinator
│   │   ├── eventpub/          # agent.Event → NATS 双 lane
│   │   ├── mcp/               # Worker MCP infrastructure
│   │   ├── client/
│   │   ├── server/
│   │   └── scheduler/
│   ├── workspace/             # WorkspacePreparation 与仓库准备
│   ├── skill/                 # Skill catalog/store
│   ├── memory/
│   ├── modelrouter/
│   ├── service/               # Server 业务服务
│   └── infra/                 # DB、NATS、filestore、provider
├── pkg/messaging/             # WorkerCommand / RunEvent wire contract
├── tools/                     # 业务 Tool 实现
├── skills/                    # SKILL.md 资源
├── types/                     # 共享领域模型
└── config/                    # 配置结构
```

## 4. 核心模块说明

### 4.1 `internal/api/` - HTTP 适配层 ⭐⭐

包含：handler / dto / contract / router

**定位：** HTTP → Command → service

#### contract 示例（在 api/contract/ 中）

```go
package contract

type EventService interface {
    CreateEvent(ctx context.Context, cmd CreateEventCommand) error
    GetEvent(ctx context.Context, id string) (*Event, error)
}

type CreateEventCommand struct {
    ChannelName string
    EventType   string
    Payload     map[string]interface{}
    Timestamp   int64
}
```

**特点：**

- ✔ 不依赖 Gin
- ✔ 不依赖 DB
- ✔ 是系统"能力语言"

---

#### handler 示例（在 api/handler/ 中）

```go
func (h *EventHandler) Create(c *gin.Context) {
    var req dto.CreateEventRequest
    _ = c.ShouldBindJSON(&req)

    cmd := convert.ToCommand(req)

    err := h.service.CreateEvent(c.Request.Context(), cmd)

    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"ok": true})
}
```

**特点：**

- ❌ 不写业务逻辑
- ❌ 不操作 DB
- ✔ 只做转换 + 调用

---

#### router 示例（在 api/ 根目录）

```go
func Register(r *gin.Engine, h *EventHandler) {
    r.POST("/events", h.Create)
    r.GET("/events/:id", h.Get)
}
```

---

### 4.2 `internal/worker/command/run` 与 `internal/worker/run`

Command Handler 只处理 wire contract、映射和 delivery 状态。RunCoordinator 负责
debounce 全 waiter、Session 串行、跨 Session 并行、并发上限、取消和关闭。
Handler 不直接调用 Runtime。

### 4.3 `internal/assistant/` - 业务执行层

Assistant 将 Session、Workspace、Memory、Skill、Artifact 和 Git 业务封装在
`PreparedRun` 中，并调用纯 `agent.Executor`。它独占 `run.*` 业务事件、Journal
归档和唯一终态。

### 4.4 `backend/agent/` - 独立 Agent 执行层

该层只定义 `ExecutionRequest`、`ExecutionResult`、`Runtime`、`Executor`、
`agent.Event` 和强类型 Tool/交互契约。四个 Runtime 直接注册；CLI Runtime 复用
`externalcli.Driver`，但 Driver 不是 Runtime。

**强制边界：**

- 禁止导入 `internal/*`、业务配置、messaging 和业务 Tool 包。
- 禁止访问 MQ、DB、Session 或 Workspace 准备逻辑。
- Runtime 只发活动事件；Executor 发 `execution.*`；Assistant 发业务 `run.*`。
- 公共执行边界不得使用 `map[string]interface{}` 或兼容 type alias。

---

### 4.5 `internal/service/` - 业务逻辑层 ⭐⭐⭐

**定位：** 业务编排 + 直接 DB 操作

#### 实现 contract

```go
type eventService struct {
    db *db.Client
}

var _ contract.EventService = (*eventService)(nil)
```

#### 核心逻辑

```go
func (s *eventService) CreateEvent(
    ctx context.Context,
    cmd contract.CreateEventCommand,
) error {
    event := &db.EventModel{
        ID:          uuid.New().String(),
        ChannelName: cmd.ChannelName,
        EventType:   cmd.EventType,
        Payload:     cmd.Payload,
        Timestamp:   cmd.Timestamp,
    }

    return s.db.CreateEvent(event)
}
```

---

### 4.6 `internal/db/` - 数据访问层 ⭐⭐

**定位：** 真实数据库操作（只是 SQL/ORM wrapper）

#### 示例

```go
type Client struct {
    db *gorm.DB
}

func (c *Client) CreateEvent(e *EventModel) error {
    return c.db.Create(e).Error
}
```

---

### 4.7 `internal/convert/` - 结构转换层 ⭐

**定位：** DTO ↔ Command ↔ Model

#### 示例

```go
func ToCommand(req dto.CreateEventRequest) contract.CreateEventCommand {
    return contract.CreateEventCommand{
        ChannelName: req.ChannelName,
        EventType:   req.EventType,
        Payload:     req.Payload,
        Timestamp:   req.Timestamp,
    }
}
```

**特点：**

- ✔ 避免 DTO 污染 service
- ✔ 保持层隔离

---

### 4.8 `internal/wire/` - 依赖组装层 ⭐

**定位：** 依赖组装，避免 main 爆炸

#### 示例

```go
func NewEventModule(db *db.Client) contract.EventService {
    svc := &eventService{db: db}
    return svc
}
```

---

### 4.9 `internal/skill/` - Skill 体系

**职责：** 技能注册、执行、管理

**子目录：**

- `registry.go` - Skill 注册中心（必须动态注册）
- `executor.go` - Skill 执行器
- `base_skill.go` - 基础 Skill 实现
- `builtin/` - 内置技能

**⚠️ 常见错误：**

- ❌ Skill 写死在代码中
- ✅ 必须 Registry 化，支持动态注册

### 4.10 `internal/connectors/` - 连接器

**职责：** 外部系统接入（GitHub、GitLab、飞书等）

**子目录：**

- `connector.go` - Connector 接口
- `github/` - GitHub 连接器
- `gitlab/` - GitLab 连接器
- `wework/` - 企业微信连接器

### 4.11 `internal/infra/` - 基础设施

**职责：** 统一基础设施访问

**子目录：**

- `mq/` - 消息队列（NATS Publisher / Subscriber）
- `db/` - 数据库连接
- `logger/` - 日志

### 4.12 `pkg/` - 对外公开接口

**职责：** 对外共享的类型和 SDK

**子目录：**

- `event/` - Event 定义（对外共享）
- `dm/` - Domain Messaging
- `client/` - Leros SDK
- `types/` - 公开类型

#### `pkg/dm` - 领域消息协议

**定位：**

- `pkg/dm` 只定义 topic 构造规则和消息结构体
- Server、Worker、UI 网关可以共同引用 `dm` 中的协议类型

**当前已确定的 topic：**

```text
# UI -> Server，用户发起需求，由 Server 路由消费
org.{org_id}.session.{session_id}.message

# Server -> Worker，Server 调度任务，指定 Worker 消费
org.{org_id}.worker.{worker_id}.task

# Worker -> Server -> UI，Worker 执行过程流式输出
org.{org_id}.session.{session_id}.message.stream
```

任务投递和回复通道使用不同可靠性策略：

- `org.{org_id}.worker.{worker_id}.task` 使用 JetStream，负责任务可靠投递和 Worker 手动 Ack。
- `org.{org_id}.session.{session_id}.message.stream` 使用实时消息 subject，负责 chunk / done / failed 推送，不要求 MQ 层持久化。
- Worker 不使用 NATS `reply subject` 返回业务消息。
- Worker 输出消息是否持久化由接收端负责，例如 Server / Gateway 订阅后写库或转发到 UI。
- `message.stream` 中的 `stream` 描述的是流式消息形态，不表示底层必须使用 JetStream。

`*` 仅用于订阅通配，不用于 publish。例如订阅某组织下所有 Worker 任务：

```text
org.{org_id}.worker.*.task
```

JetStream 的 Stream、Consumer、Durable 等名称不直接使用带 `.` 的 topic，应由基础设施模块将 `.` 替换为 `_` 后再追加业务后缀。例如：

```text
topic:       org.1001.worker.worker_1.task
stream name: org_1001_worker_worker_1_task_STREAM
durable:     org_1001_worker_worker_1_task_SUBSCRIBER
```

上述 JetStream 命名规则只适用于任务队列等需要持久化消费的 subject，不适用于 Worker 的普通回复 subject。

**topic builder 示例：**

```go
taskTopic := dm.Topic().
    Org("1001").
    Worker("worker_1").
    Task().
    Build()
// org.1001.worker.worker_1.task

streamTopic := dm.Topic().
    Org("1001").
    Session("sess_1").
    Message().
    Stream().
    Build()
// org.1001.session.sess_1.message.stream

workerTaskSubTopic := dm.Topic().
    Org("1001").
    Add("worker").
    Wildcard().
    Task().
    Build()
// org.1001.worker.*.task
```

**统一 Envelope：**

所有跨进程消息使用统一外层信封，避免 `trace_id`、`task_id`、`org_id` 等字段散落到不同消息顶层。

```go
type Envelope[T any] struct {
    ID        string      `json:"id"`
    Type      MessageType `json:"type"`
    CreatedAt time.Time   `json:"created_at"`

    Trace TraceContext `json:"trace"`
    Route RouteContext `json:"route"`

    Body     T              `json:"body"`
    Metadata map[string]any `json:"metadata,omitempty"`
}

type TraceContext struct {
    TraceID   string `json:"trace_id"`
    RequestID string `json:"request_id,omitempty"`
    TaskID    string `json:"task_id,omitempty"`
    RunID     string `json:"run_id,omitempty"`
    ParentID  string `json:"parent_id,omitempty"`
}

type RouteContext struct {
    OrgID     string `json:"org_id"`
    SessionID string `json:"session_id,omitempty"`
    WorkerID  string `json:"worker_id,omitempty"`
}
```

`TraceContext` 用于链路追踪、日志排查和幂等；`RouteContext` 用于租户隔离、topic 构造和消息投递。

**Server → Worker：任务消息结构示例**

```go
type WorkerTaskMessage = Envelope[WorkerTaskBody]

type WorkerTaskBody struct {
    TaskType TaskType `json:"task_type"`

    Actor     ActorContext    `json:"actor"`
    Execution ExecutionTarget `json:"execution"`
    Input     TaskInput       `json:"input"`

    Runtime RuntimeOptions `json:"runtime,omitempty"`
    Policy  TaskPolicy     `json:"policy,omitempty"`
}

type TaskInput struct {
    Type        InputType      `json:"type"`
    Text        string         `json:"text,omitempty"`
    Messages    []ChatMessage  `json:"messages,omitempty"`
    Attachments []Attachment   `json:"attachments,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
}
```

示例 JSON：

```json
{
  "id": "msg_1",
  "type": "worker.task",
  "created_at": "2026-04-29T12:00:00Z",
  "trace": {
    "trace_id": "trace_1",
    "request_id": "req_1",
    "task_id": "task_1",
    "run_id": "run_1"
  },
  "route": {
    "org_id": "1001",
    "session_id": "sess_1",
    "worker_id": "worker_1"
  },
  "body": {
    "task_type": "agent.run",
    "actor": {
      "user_id": "user_1",
      "channel": "web"
    },
    "execution": {
      "assistant_id": "assistant_1",
      "agent_id": "agent_1"
    },
    "input": {
      "type": "message",
      "text": "帮我总结这个 PR"
    }
  }
}
```

**Worker → Server → UI：流式消息结构示例**

```go
type MessageStreamMessage = Envelope[StreamBody]

type StreamBody struct {
    Seq     int64           `json:"seq"`
    Event   StreamEventType `json:"event"`
    Payload StreamPayload   `json:"payload"`

    Usage *UsagePayload `json:"usage,omitempty"`
    Error *StreamError  `json:"error,omitempty"`
}

type StreamPayload struct {
    Role       MessageRole      `json:"role,omitempty"`
    Content    string           `json:"content,omitempty"`
    ToolCall   *ToolCallEvent   `json:"tool_call,omitempty"`
    ToolResult *ToolResultEvent `json:"tool_result,omitempty"`
}
```

示例 JSON：

```json
{
  "id": "evt_1",
  "type": "message.stream",
  "created_at": "2026-04-29T12:00:01Z",
  "trace": {
    "trace_id": "trace_1",
    "request_id": "req_1",
    "task_id": "task_1",
    "run_id": "run_1"
  },
  "route": {
    "org_id": "1001",
    "session_id": "sess_1",
    "worker_id": "worker_1"
  },
  "body": {
    "seq": 1,
    "event": "message.delta",
    "payload": {
      "role": "assistant",
      "content": "这个 PR 主要修改了"
    }
  }
}
```

## 5. 进程拆分建议

### 5.1 为什么需要进程拆分？

| 优势     | 说明                 |
| -------- | -------------------- |
| 水平扩展 | 不同组件独立扩缩容   |
| 解耦     | 故障隔离             |
| 负载分离 | 不同负载类型分开处理 |

### 5.2 当前进程边界

同一个 `leros` 二进制通过 Cobra 子命令提供 Server 与 Worker 进程入口。进程启停、
信号和致命错误处理只存在于 `cmd/leros`；`internal/*` 不知道自己运行在哪种进程中。

```bash
leros server   # API、DB、NATS、Worker 管理
leros worker   # Worker Command、Coordinator、Assistant、Agent Runtime
```

是否同机部署属于部署配置，不改变包边界或执行契约。

**特点：**

- 完全独立的进程
- 可独立扩缩容
- 故障隔离

### 5.3 进程间通信

所有进程间通过 **Event Bus** 通信：

```
Connector Process → Event Bus → Event Engine Process → Execution Engine Process → Agent Runtime Worker
```

## 6. 常见错误与最佳实践

### 6.1 完整调用链（最重要）

```
HTTP Request
   ↓
handler (DTO → Command)
   ↓
contract.Service (interface)
   ↓
service (business logic)
   ↓
db client (SQL/ORM)
   ↓
Database
```

### 6.2 核心设计原则（架构本质）

#### ✔ 1️⃣ contract 是系统语言

不是 RPC，但类似 RPC 的"能力定义"

#### ✔ 2️⃣ service 是唯一业务执行点

不允许业务散落

#### ✔ 3️⃣ DB 不抽象 interface

直接调用

#### ✔ 4️⃣ handler 只做适配

不参与业务

#### ✔ 5️⃣ convert 保持隔离

避免 DTO 污染 service

### 6.3 常见错误

| ❌ 错误做法                            | ✅ 正确做法                               |
| -------------------------------------- | ----------------------------------------- |
| 把所有逻辑写进 Event Handler           | Handler → 调用 contract.Service           |
| Event Handler 使用 `switch` 硬编码路由 | Router 独立 + Handler 插件化（Phase 2）   |
| Agent Runtime 直接调 MQ / DB           | 通过 contract.AgentService                |
| Skill 写死在代码中                     | 必须 Registry 化，支持动态注册            |
| 按技术分层（controller/service/model） | 按领域分层（contract/handler/service/db） |
| 添加 Repository 抽象                   | service 直接调用 db.client                |
| 缺少接口定义，直接依赖实现             | contract 定义所有能力                     |

### 6.4 最佳实践

1. **contract 定义系统能力** - 每层定义 interface，支持替换
2. **service 直接操作 DB** - 无 Repository 抽象
3. **handler 只做转换 + 调用** - 不写业务逻辑
4. **convert 保持隔离** - DTO ↔ Command ↔ Model
5. **wire 组装依赖** - 避免 main 爆炸
6. **每个包只暴露必要的接口**
7. **使用 `internal/` 强制隔离核心实现**
8. **使用 `pkg/` 对外公开稳定接口**
9. **Handler 必须插件化，不写死 `switch`**
10. **Skill 必须 Registry 化**
11. **依赖注入，避免全局变量**

## 8. 下一步行动

### 立即可做（低成本高收益）

1. ✅ 创建 `internal/` 目录结构（已完成）
2. ✅ 移动现有代码到对应领域目录（已完成）
3. ✅ 定义各层 interface（API 层已完成）
4. 🔄 Event Engine Handler 插件化（Phase 2 计划）

### 中期优化（Phase 2）

1. 🔄 实现 Execution Engine 的重试/超时控制
2. 🔄 完善 Skill Registry（统一至 internal/skill/）
3. 🔄 添加 Policy Engine 基础框架

### 长期规划

1. 🔄 进程拆分（Server / Worker 逻辑分离）
2. 🔄 分布式部署
3. 🔄 水平扩展

## 9. 总结

Leros 后端应该从：

```
MVC / service-based
```

升级为：

```
Contract-driven Service Architecture
```

**核心原则：**

- **contract 定义能力** - 系统能力的"语言"
- **service 实现能力** - 直接操作 DB
- **handler 适配输入输出** - HTTP 适配
- **db 提供执行能力** - 真实数据库操作
- **convert 保持隔离** - DTO ↔ Command ↔ Model
- **wire 组装依赖** - 避免 main 爆炸
- **无 Repository 抽象** - service 直接调用 db

**当前实现状态：**

- API 层按 contract / service / handler 组织。
- Worker 主链为 Handler → Coordinator → Assistant → Executor → Runtime。
- Agent 执行层与 SingerOS 业务层物理分离。
- native、Claude、Codex、OpenCode 使用同一 Runtime 和 Event 契约。
- NATS `run.stream` / `run.state` 保持 wire compatibility。
