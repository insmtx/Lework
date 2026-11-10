# 统一 LLM 包设计

## 背景与现状

当前 Lework 项目中 LLM 相关代码散落在三个互相独立的位置：

| 位置 | 职责 | 问题 |
|---|---|---|
| `backend/internal/service/llm_model_service.go` + `backend/types/llm_model.go` | 模型配置 CRUD（控制面） | 仅存在于控制面 service 层，Worker 进程无法直接访问 |
| `backend/internal/modelrouter/` | Worker 内 HTTP 代理 + 内存态 `ModelStore` | `ModelStore` 是内存 map，由 `preparer.resolveModelRouting` 临时写入；协议转换内核已抽到 `backend/pkg/llmprotocol` |
| `backend/pkg/eino/` | Eino 框架封装的实际 LLM 调用 + `usageAccumulator` 内存累加器 | **调用记录和用量信息没有持久化**，只有内存累加，进程重启即丢失 |

三条缺口：

1. 没有统一的"模型管理"包把控制面配置和 worker 代理串起来
2. 没有调用记录表，没有用量持久化
3. 两条调用路径（`eino.Flow` / `modelrouter` HTTP 代理）各自独立，用量信息无法统一收集

## 目标

在 `backend/internal/llm/` 下建立统一 LLM 包，覆盖：

1. **模型管理** — 统一的 `ModelConfig` 类型和 CRUD 接口
2. **统一调用** — 所有 LLM 调用走统一入口，内部仍基于 `pkgeino` + `llmprotocol`
3. **调用记录** — 每次调用一条记录，含 model、tokens、延迟、状态、错误、来源
4. **用量信息** — 基于调用记录的查询和汇总

全面收敛现有的 `llm_model_service` / `modelrouter` / `eino` 散落代码。

## 非目标

- 不引入向量化存储或 RAG 能力（Memory System 另行设计）
- 不引入多模型并发调用编排（Orchestrator 另行设计）
- 不改动 `llmprotocol` 协议转换内核（已经抽离完备，保持稳定）
- 不引入计费或配额限制（调用记录为后续计费提供数据基础，但本期不实现计费逻辑）
- 不做流式调用的实时用量上报（streaming 在结束时一次性上报）

## 包结构

```
backend/internal/llm/
├── model.go              # 领域类型：ModelConfig, CallRequest, CallResult, CallRecord, Usage, Message 等
├── manager.go             # Manager 接口 — 统一模型管理（CRUD + 解析）
├── manager_db.go          # Manager 的 gorm 实现（控制面）
├── manager_db_test.go
├── caller.go              # Caller 接口 — 统一调用入口
├── caller_eino.go         # Caller 的 eino 实现（封装 pkgeino.Flow）
├── caller_eino_test.go
├── recorder.go            # Recorder 接口 — 调用记录持久化
├── recorder_db.go         # Recorder 的 gorm 实现（控制面）
├── recorder_db_test.go
├── usage_handler.go       # Worker 用量上报的 HTTP handler（控制面端接收）
├── usage_reporter.go      # Worker 侧同步 HTTP 上报实现
├── usage_reporter_test.go
├── proxy.go               # Worker HTTP 代理实现（从 modelrouter 迁移）
├── proxy_test.go          # 从 modelrouter/handler_test.go 迁移
├── contract_adapter.go    # contract.LLMModelService → llm.Manager 的适配层
└── mock_*.go              # 测试用 mock 实现
```

### 分层依赖

```
backend/types/                    # LLMModel, LLMCallRecord 表定义（最底层，无业务逻辑）
    ↑
backend/internal/llm/            # 统一 LLM 包：类型 + 接口 + 实现
    ↑
backend/internal/service/        # contract 适配层委托到 llm 包
backend/internal/worker/         # Worker 使用 llm.Caller / llm.Proxy / llm.UsageReporter
backend/internal/api/handler/    # HTTP handler 调用 service 层
```

关键约束（遵循 AGENTS.md 分层边界）：

- `internal/llm/` 是库代码，不使用 `os.Exit()` / `lifecycle.Std()` / `log.Fatal` / `panic`
- `ModelConfig.APIKey` 仅在 `internal/llm` 包内部流转，不跨越 agent 执行内核层（不出现在 NodeEvent / RunEvent / Journal / NATS / 日志中）

## 领域类型

### ModelConfig（model.go）

从 `types.LLMModel` 迁移而来，作为统一包内部使用的核心类型：

```go
type ModelConfig struct {
    ID            uint
    OrgID         uint
    Code          string
    Name          string
    Description   string
    Provider      string          // openai | anthropic | deepseek | qwen | gemini | ark | openrouter | custom
    ModelName     string          // 实际传给供应商 API 的模型名
    BaseURL       string
    BaseURLHasV1  bool
    APIKey        string          // 调用时解密注入；禁止外泄到日志/事件
    MaxTokens     int
    Temperature   float64
    TimeoutSec    int
    Status        string          // active | inactive
    IsDefault     bool
    IsSystem      bool
    Config        map[string]any  // 扩展配置
}
```

### CallRecord 与 Usage（model.go）

```go
// CallRecord 记录一次 LLM 调用的完整信息。
type CallRecord struct {
    ID            uint
    OrgID         uint
    ModelConfigID uint           // 关联 llm_models.id；0 表示临时测试调用
    Provider      string
    ModelName     string
    EntryProtocol string         // openai_chat | anthropic_messages | openai_responses | gemini
    IsStream      bool
    InputTokens   int
    OutputTokens  int
    TotalTokens   int
    LatencyMS     int64
    StatusCode    int            // HTTP 状态码
    Success       bool
    ErrorMessage  string
    CallerType    string         // agent_run | http_proxy | test_connectivity | feedback_summarizer | ...
    CallerRef     string         // run_id 等关联引用，无则留空
    StartedAt     time.Time
    FinishedAt    time.Time
}

// Usage 是单次调用的 token 用量。
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}
```

### 调用接口类型（model.go）

```go
type Message struct {
    Role    string         // user | assistant | system | tool
    Content string
}

type ToolSpec struct {
    Name        string
    Description  string
    JSONSchema   map[string]any
}

type CallRequest struct {
    ModelConfigID uint                  // 指定使用哪个模型配置
    Messages      []Message            // 对话消息
    Tools         []ToolSpec           // 可用工具
    SystemPrompt  string               // 系统提示词（与 Messages 二选一或组合）
    MaxTokens     *int                 // 覆盖配置默认值
    Temperature   *float64             // 覆盖配置默认值
    IsStream      bool
    CallerType    string               // 记录调用来源
    CallerRef     string               // 关联引用（如 run_id）
}

type StreamSink interface {
    EmitMessageDelta(ctx context.Context, content string) error
    EmitReasoningDelta(ctx context.Context, content string) error
}

type CallResult struct {
    Message *schema.Message           // 最终消息（来自 einoschema）
    Usage   *Usage
    Record *CallRecord                // 已持久化的调用记录
}
```

### 查询类型（model.go）

```go
type CreateRequest struct {
    Name        string
    Description string
    Provider    string
    Model       string
    BaseURL     string
    APIKey      string
    Status      string
    IsDefault   bool
    Config      map[string]any
}

type UpdateRequest struct {
    Name        string
    Description *string
    Provider    string
    Model       string
    BaseURL     *string
    APIKey      *string
    Status      string
    IsDefault   *bool
    Config      *map[string]any
}

type ListRequest struct {
    Provider *string
    Status   *string
    Keyword  *string
    Offset   int
    Limit    int
}

type ListModelResult struct {
    Total  int64
    Offset int
    Limit  int
    Items  []*ModelConfig
}

type TestRequest struct {
    ID       *uint
    Code     string
    Provider string
    Model    string
    BaseURL  string
    APIKey   string
}

type TestResult struct {
    Success      bool
    Message      string
    Endpoint     string
    LatencyMS    int64
    BaseURLHasV1 bool
}

type ListCallsRequest struct {
    ModelConfigID *uint
    Provider      *string
    CallerType    *string
    Success       *bool
    StartedFrom   *time.Time
    StartedTo     *time.Time
    Offset        int
    Limit         int
}

type ListCallsResult struct {
    Total  int64
    Offset int
    Limit  int
    Items  []*CallRecord
}

type UsageSummaryRequest struct {
    ModelConfigID *uint
    Provider      *string
    From          time.Time
    To            time.Time
    GroupBy       string  // "model" | "provider" | "caller_type" | "day"
}

type UsageSummaryItem struct {
    GroupKey      string
    InputTokens   int64
    OutputTokens  int64
    TotalTokens   int64
    CallCount     int64
    SuccessCount  int64
    FailureCount  int64
}

type UsageSummary struct {
    Items []UsageSummaryItem
}
```

## 接口设计

### Manager — 模型配置管理

```go
type Manager interface {
    Create(ctx context.Context, req *CreateRequest) (*ModelConfig, error)
    Get(ctx context.Context, id uint, code string) (*ModelConfig, error)
    GetDefault(ctx context.Context) (*ModelConfig, error)
    Update(ctx context.Context, id uint, req *UpdateRequest) (*ModelConfig, error)
    Delete(ctx context.Context, id uint) error
    List(ctx context.Context, req *ListRequest) (*ListModelResult, error)
    TestConnectivity(ctx context.Context, req *TestRequest) (*TestResult, error)
}
```

实现 `manager_db.go` 基于 gorm，迁移现有 `llm_model_service.go` 的全部逻辑（含 `normalizeLLMBaseURL`、`detectURLHasV1`、`probeConnectivity`、`maskAPIKey` 等 helper）。

### Caller — 统一调用入口

```go
type Caller interface {
    // Call 执行一次非流式 LLM 调用
    Call(ctx context.Context, req *CallRequest) (*CallResult, error)
    // Stream 执行流式调用，通过 sink 回传 delta
    Stream(ctx context.Context, req *CallRequest, sink StreamSink) (*CallResult, error)
}
```

实现 `caller_eino.go` 包装 `pkgeino`：

1. 通过 `Manager` 解析 `ModelConfigID` → `ModelConfig`
2. 解密 APIKey，构建 `pkgeino.ChatModelConfig`
3. 构建 `pkgeino.FlowConfig`（含 tools、system prompt）
4. 调用 `Flow.Generate` 或 `Flow.Stream`
5. 收集 `Usage`，构建 `CallRecord`
6. 调用 `Recorder.RecordCall` 持久化
7. 返回 `CallResult`

### Recorder — 调用记录与用量

```go
type Recorder interface {
    RecordCall(ctx context.Context, record *CallRecord) error
    ListCalls(ctx context.Context, req *ListCallsRequest) (*ListCallsResult, error)
    GetUsageSummary(ctx context.Context, req *UsageSummaryRequest) (*UsageSummary, error)
}
```

实现 `recorder_db.go` 基于 gorm，操作 `llm_call_records` 表。

## 数据库表设计

### 新增表：llm_call_records

对应 `types.LLMCallRecord` 结构（放 `backend/types/llm_call_record.go`）：

```go
type LLMCallRecord struct {
    gorm.Model
    OrgID          uint      `gorm:"column:org_id;type:integer;not null;index:idx_llm_call_org_started;sort:desc"`
    ModelConfigID  uint      `gorm:"column:model_config_id;type:integer;index:idx_llm_call_model"`
    Provider       string    `gorm:"column:provider;type:varchar(64);not null"`
    ModelName      string    `gorm:"column:model_name;type:varchar(255);not null"`
    EntryProtocol  string    `gorm:"column:entry_protocol;type:varchar(32)"`
    IsStream       bool      `gorm:"column:is_stream;type:boolean;default:false"`
    InputTokens    int       `gorm:"column:input_tokens;type:integer;default:0"`
    OutputTokens   int       `gorm:"column:output_tokens;type:integer;default:0"`
    TotalTokens    int       `gorm:"column:total_tokens;type:integer;default:0"`
    LatencyMS      int64     `gorm:"column:latency_ms;type:bigint;default:0"`
    StatusCode     int       `gorm:"column:status_code;type:integer;default:0"`
    Success        bool      `gorm:"column:success;type:boolean;default:false;index:idx_llm_call_success"`
    ErrorMessage   string    `gorm:"column:error_message;type:text"`
    CallerType     string    `gorm:"column:caller_type;type:varchar(64);not null;index:idx_llm_call_caller"`
    CallerRef      string    `gorm:"column:caller_ref;type:varchar(128)"`
    StartedAt      time.Time `gorm:"column:started_at;type:timestamp;not null;index:idx_llm_call_org_started;sort:desc"`
    FinishedAt     time.Time `gorm:"column:finished_at;type:timestamp"`
}

func (LLMCallRecord) TableName() string { return TableNameLLMCallRecord }
```

索引：

- `idx_llm_call_org_started` (org_id, started_at DESC) — 按组织查最近调用
- `idx_llm_call_model` (model_config_id) — 按模型查用量
- `idx_llm_call_caller` (caller_type) — 按来源筛选
- `idx_llm_call_success` (success) — 按成功/失败筛选

`types/tables.go` 新增 `TableNameLLMCallRecord = "llm_call_records"`。

现有 `types.LLMModel` 表保持不变。

## 双侧调用路径

### 控制面（Server 端）调用路径

现有调用点（需迁移）：

- `backend/internal/service/feedback_summarizer.go`
- `backend/internal/service/work_title_updater.go`
- `backend/internal/service/default_skill_description_translator.go`
- `backend/internal/service/llm_model_service.go` 的 `TestLLMModel` 方法
- `backend/prompts/executor_eino.go`

迁移后统一为：

```go
caller := llm.NewCaller(manager, recorder)
result, err := caller.Call(ctx, &llm.CallRequest{
    ModelConfigID: modelID,
    Messages:      []llm.Message{{Role: "user", Content: "Reply: ok"}},
    CallerType:    "feedback_summarizer",
})
```

### Worker 侧调用路径

现状：`preparer.resolveModelRouting` 把 `UpstreamConfig` 写入内存 `ModelStore`，外部 CLI 通过 `WorkerProxyBaseURL()` 打到 worker HTTP 代理。

迁移后：

1. `preparer.resolveModelRouting` 改为调用 `llm.WorkerProxy.Put(modelConfig)`
2. `modelrouter.RegisterRoutes` 迁移到 `llm.proxy.RegisterRoutes`
3. HTTP 代理 handler 在每次请求完成时，调用 `usageReporter.Report(usage)` 同步 HTTP POST 到控制面
4. 控制面新增 `POST /internal/v1/llm/usage` 端点接收上报，写入 `llm_call_records`

### Worker → 控制面 用量上报

同步 HTTP 方式：

```
POST /internal/v1/llm/usage
Authorization: Bearer <worker_token>
Content-Type: application/json

{
    "org_id": 1,
    "model_config_id": 12,
    "provider": "openai",
    "model_name": "gpt-4o-mini",
    "entry_protocol": "openai_chat",
    "is_stream": false,
    "input_tokens": 1234,
    "output_tokens": 567,
    "total_tokens": 1801,
    "latency_ms": 3200,
    "status_code": 200,
    "success": true,
    "error_message": "",
    "caller_type": "http_proxy",
    "caller_ref": "run_abc123",
    "started_at": "2026-07-13T10:00:00Z",
    "finished_at": "2026-07-13T10:00:03Z"
}
```

控制面 `usage_handler.go` 接收并委托 `Recorder.RecordCall`。

失败处理：上报失败仅记日志，不阻塞 worker 调用返回（用量丢失优先级低于业务可用性）。

## contract 层处理

`contract.LLMModelService` 接口保留，实现类改为薄委托层：

```go
type llmModelService struct {
    manager llm.Manager
}

func NewLLMModelService(manager llm.Manager) contract.LLMModelService {
    return &llmModelService{manager: manager}
}
```

方法内部做 `llm.ModelConfig` → `contract.LLMModel` 的类型转换，保持前端 API 响应结构不变。

## 迁移计划（3 阶段渐进）

### Phase 1：骨架 + 模型管理收敛

**范围**：

- 建立 `backend/internal/llm/` 包骨架
- 定义全部领域类型（`model.go`）
- 实现 `Manager` 接口 + `manager_db.go`
- `contract.LLMModelService` 改为委托到 `llm.Manager`
- `types/llm_call_record.go` 新增表定义（表先建好，Phase 2 才写入）

**验证**：

- `go test ./backend/internal/llm/...` 通过
- `go test ./backend/internal/service/...` 通过（contract 适配层测试不变）
- `go build ./backend/cmd/lework/` 成功
- 前端 LLM 模型管理页功能不变

### Phase 2：Caller + Recorder + 控制面调用迁移

**范围**：

- 实现 `Recorder` 接口 + `recorder_db.go`
- 实现 `Caller` 接口 + `caller_eino.go`
- 迁移控制面 5 个调用点（`feedback_summarizer` / `work_title_updater` / `skill_translator` / `executor_eino` / `TestLLMModel`）到 `llm.Caller`
- `AutoMigrate` 加入 `LLMCallRecord`
- 前端新增调用记录查询 API（如果本期需要）

**验证**：

- `go test ./backend/internal/llm/...` 通过（含 caller_eino_test）
- `go test ./backend/internal/service/...` 通过
- 控制面各业务功能不变（反馈总结、标题更新等）
- DB 中 `llm_call_records` 表有数据写入

### Phase 3：Worker 代理迁移 + 用量上报

**范围**：

- 实现 `proxy.go`（从 `modelrouter/handler.go` 迁移）
- 实现 `usage_reporter.go` + `usage_handler.go`
- 迁移 `preparer.resolveModelRouting` 到 `llm.WorkerProxy.Put`
- 迁移 `router.SetupRouter` 引用从 `modelrouter` 到 `llm`
- 删除 `backend/internal/modelrouter/` 包（或保留薄兼容层过渡）
- 更新 `AGENTS.md` 中关于 modelrouter v1/v2 的描述

**验证**：

- `go test ./backend/internal/llm/...` 通过（含 proxy_test 从 modelrouter 迁移）
- `go test ./backend/internal/worker/...` 通过
- `go build ./backend/cmd/lework/` 成功
- Worker agent run 全链路功能不变
- Worker HTTP 代理（`/v1/chat/completions` 等）行为不变
- 用量上报成功写入控制面 `llm_call_records` 表

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| `modelrouter` 迁移影响 Worker 代理稳定性 | Phase 3 保留 `handler_test.go` 全部 golden test 迁移到 `proxy_test.go`，确保行为不变 |
| 用量上报失败导致数据丢失 | 同步 HTTP 失败仅记日志不阻塞业务；后续可演进为本地暂存+批量上报，本期不实现 |
| `pkgeino.Flow` 接口变更影响 caller 封装 | `caller_eino_test` 覆盖核心调用路径；跑通现有 service 测试作为回归 |
| AGENTS.md 提到 `modelrouter/v2` 但实际不存在 | Phase 3 同步更新文档，消除歧义 |

## 不做

- 向量化存储 / RAG
- 多模型并发编排
- 计费 / 配额限制
- `llmprotocol` 协议转换内核改动
- 流式调用的实时用量上报（streaming 在结束时一次性上报）
