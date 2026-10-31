# 统一 LLM 包 Phase 1 实现计划

> 性质：Phase 1（骨架 + 模型管理收敛）。Phase 2/3 见设计文档后续阶段。

**Goal:** 建立 `backend/internal/llm/` 包骨架，定义全部领域类型，实现 `Manager` 接口的 gorm 实现，将 `contract.LLMModelService` 改为委托到 `llm.Manager`，并新增 `LLMCallRecord` 表定义。

**Architecture:** 新建 `internal/llm/` 包，不依赖 `internal/api/` 层。Manager 方法签名显式接受 `orgID`，由 `contract_adapter.go`（在 service 层）从 context 解析后传入。不破坏现有前端 API 契约。

**Tech Stack:** Go 1.25, gorm v1.30, sqlite（测试）, cloudwego/eino, bytedance/sonic

## Global Constraints

- 禁止使用 `panic`，错误通过 `error` 返回
- 禁止使用 `map[string]interface{}` 传递业务数据（`LLMModelConfig` 的 Config 字段除外，已有类型）
- `internal/llm/` 是库代码，不用 `os.Exit` / `lifecycle.Std()` / `log.Fatal`
- `ModelConfig.APIKey` 仅在 `internal/llm` 包内流转，不出现在日志/事件/NATS
- 导入组织：stdlib、第三方、内部包，三组用空行分隔
- 使用制表符缩进
- 提交消息用中文，约定式提交格式

## 设计偏离说明

设计文档中 `Manager` 接口方法签名未显式包含 `orgID` 参数（隐含从 context 获取）。实际实现中，因为 `internal/llm/` 作为库代码不应依赖 `internal/api/auth` 包（`requireCallerOrg` 所在层），所以 `Manager` 方法签名显式接受 `orgID uint` 参数。组织隔离由调用方（contract_adapter）负责从 context 解析后传入。

---

## File Map

| 文件 | 操作 | 职责 |
|---|---|---|
| `backend/types/llm_call_record.go` | Create | LLMCallRecord 表定义 |
| `backend/types/tables.go` | Modify | 新增 TableNameLLMCallRecord 常量 |
| `backend/internal/llm/model.go` | Create | 领域类型：ModelConfig, Usage, CallRecord, Message, CallRequest 等 |
| `backend/internal/llm/manager.go` | Create | Manager 接口定义 |
| `backend/internal/llm/manager_db.go` | Create | Manager 的 gorm 实现（helper + CRUD） |
| `backend/internal/llm/manager_db_test.go` | Create | manager_db 测试（从 service/llm_model_service_test.go 迁移） |
| `backend/internal/llm/contract_adapter.go` | Create | contract.LLMModelService → llm.Manager 适配层 |
| `backend/internal/service/llm_model_service.go` | Modify | 改为委托到 contract_adapter |
| `backend/internal/api/router.go` | Modify | 调整 NewLLMModelService 调用方式 |

---

### Task 1: 新增 LLMCallRecord 表定义

**Files:**
- Create: `backend/types/llm_call_record.go`
- Modify: `backend/types/tables.go`

**Interfaces:**
- Produces: `types.LLMCallRecord` struct, `types.TableNameLLMCallRecord` constant

- [ ] **Step 1: 在 tables.go 新增表名常量**

在 `backend/types/tables.go` 的常量块中，`TableNameLLMModel` 后面新增：

```go
// TableNameLLMCallRecord LLM调用记录表名
TableNameLLMCallRecord = tablenamePrefix + "llm_call_record"
```

- [ ] **Step 2: 创建 llm_call_record.go**

Create `backend/types/llm_call_record.go`:

```go
package types

import (
	"time"

	"gorm.io/gorm"
)

// LLMCallRecord 记录一次LLM调用的完整信息。
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

// TableName 指定LLMCallRecord对应的数据库表名
func (LLMCallRecord) TableName() string {
	return TableNameLLMCallRecord
}
```

- [ ] **Step 3: 验证编译**

Run: `go build ./backend/types/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add backend/types/llm_call_record.go backend/types/tables.go
git commit -m "feat(types): 新增 LLMCallRecord 表定义 — 调用记录持久化结构"
```

---

### Task 2: 定义 llm 包领域类型（model.go）

**Files:**
- Create: `backend/internal/llm/model.go`

**Interfaces:**
- Produces: `llm.ModelConfig`, `llm.CallRecord`, `llm.Usage`, `llm.Message`, `llm.CallRequest`, `llm.CallResult`, `llm.StreamSink`, 以及全部 CRUD 请求/响应类型

- [ ] **Step 1: 创建 model.go**

Create `backend/internal/llm/model.go`，内容包含：
- 包注释
- `ModelConfig` 结构体（对应 `types.LLMModel` 的领域类型，含 `APIKey` 字段）
- `CallRecord` 结构体
- `Usage` 结构体
- `Message`, `ToolSpec`, `StreamSink` 接口
- `CallRequest`, `CallResult` 结构体（`CallResult.Message` 类型为 `*einoschema.Message`，即 `github.com/cloudwego/eino/schema.Message`）
- Manager 的请求/响应类型：`CreateRequest`, `UpdateRequest`, `ListRequest`, `ListModelResult`, `TestRequest`, `TestResult`

```go
// Package llm 提供统一的 LLM 模型管理、调用、调用记录和用量信息能力。
package llm

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"
)

type ModelConfig struct {
	ID            uint
	OrgID         uint
	Code          string
	Name          string
	Description   string
	Provider      string
	ModelName     string
	BaseURL       string
	BaseURLHasV1  bool
	APIKey        string
	MaxTokens     int
	Temperature   float64
	TimeoutSec    int
	Status        string
	IsDefault     bool
	IsSystem      bool
	Config        map[string]any
}

type CallRecord struct {
	ID            uint
	OrgID         uint
	ModelConfigID uint
	Provider      string
	ModelName     string
	EntryProtocol string
	IsStream      bool
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	LatencyMS     int64
	StatusCode    int
	Success       bool
	ErrorMessage  string
	CallerType    string
	CallerRef     string
	StartedAt     time.Time
	FinishedAt    time.Time
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type Message struct {
	Role    string
	Content string
}

type ToolSpec struct {
	Name        string
	Description string
	JSONSchema  map[string]any
}

type CallRequest struct {
	ModelConfigID uint
	Messages      []Message
	Tools         []ToolSpec
	SystemPrompt  string
	MaxTokens     *int
	Temperature   *float64
	IsStream      bool
	CallerType    string
	CallerRef     string
}

type StreamSink interface {
	EmitMessageDelta(ctx context.Context, content string) error
	EmitReasoningDelta(ctx context.Context, content string) error
}

type CallResult struct {
	Message *schema.Message
	Usage   *Usage
	Record  *CallRecord
}

// --- Manager 请求/响应类型 ---

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
```

- [ ] **Step 2: 验证编译**

Run: `go build ./backend/internal/llm/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add backend/internal/llm/model.go
git commit -m "feat(llm): 定义统一 LLM 包领域类型 — ModelConfig, CallRecord, Usage 等"
```

---

### Task 3: 定义 Manager 接口（manager.go）

**Files:**
- Create: `backend/internal/llm/manager.go`

**Interfaces:**
- Produces: `llm.Manager` interface

- [ ] **Step 1: 创建 manager.go**

```go
package llm

import "context"

// Manager 定义统一的 LLM 模型配置管理接口。
// orgID 由调用方从认证上下文中解析后传入，Manager 实现不做认证。
type Manager interface {
	Create(ctx context.Context, orgID uint, req *CreateRequest) (*ModelConfig, error)
	Get(ctx context.Context, orgID uint, id uint, code string) (*ModelConfig, error)
	GetDefault(ctx context.Context, orgID uint) (*ModelConfig, error)
	Update(ctx context.Context, orgID uint, id uint, req *UpdateRequest) (*ModelConfig, error)
	Delete(ctx context.Context, orgID uint, id uint) error
	List(ctx context.Context, orgID uint, req *ListRequest) (*ListModelResult, error)
	TestConnectivity(ctx context.Context, orgID uint, req *TestRequest) (*TestResult, error)
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./backend/internal/llm/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add backend/internal/llm/manager.go
git commit -m "feat(llm): 定义 Manager 接口 — 统一模型配置管理"
```

---

### Task 4: 实现 Manager — helper + CRUD（manager_db.go）

**Files:**
- Create: `backend/internal/llm/manager_db.go`

**Interfaces:**
- Consumes: `types.LLMModel`, `infra/db` DAO 函数, `pkg/eino`, `snowflake`
- Produces: `llm.ManagerDb` struct, `llm.NewManager(db)` constructor — 实现 `llm.Manager` 接口

这个 Task 迁移 `llm_model_service.go` 中的全部 helper 函数和 CRUD 逻辑到 `manager_db.go`，保持行为不变。完整代码量大，分步写入。

- [ ] **Step 1: 创建 manager_db.go — helper 函数 + 类型转换**

文件内容包含：
1. import 块（stdlib: context, errors, fmt, strings, time, unicode/utf8；第三方: gorm.io/gorm；内部: infra/db, pkg/eino, types, snowflake）
2. `modelConfigFromEntity(m *types.LLMModel) *ModelConfig` — 实体转领域类型
3. helper 函数（从 service 迁移，保持不变）：
   - `llmEndpointSuffixes` 切片
   - `normalizeLLMBaseURL(baseURL string) string`
   - `detectURLHasV1(rawURL string) bool`
   - `buildLLMEndpointURL(baseURL string, hasV1 bool) string`
   - `generateLLMModelCode() string` — `fmt.Sprintf("llm_%s", snowflake.GenerateIDBase58())`
   - `maskAPIKey(apiKey string) string`
   - `firstRunes(value string, count int) string`
   - `lastRunes(value string, count int) string`
   - `probeResult` 结构体
   - `probeConnectivity(ctx, provider, modelName, apiKey, baseURL, preferV1) *probeResult` — 通过 `eino.NewChatModel` + `eino.NewFlow` 验证连通性
4. `clearOrgDefaultLLMModels(ctx, db, orgID, excludeID) error` — 清除默认标记
5. `orgHasLLMModels(ctx, db, orgID) (bool, error)` — 检查组织是否已有模型

以上函数实现与 `backend/internal/service/llm_model_service.go` 中完全一致，逐行迁移。

- [ ] **Step 2: 追加 ManagerDb 结构体和构造函数**

在同一文件追加：

```go
type ManagerDb struct {
	db        *gorm.DB
	probeFunc func(ctx context.Context, provider, modelName, apiKey, baseURL string, preferV1 bool) *probeResult
}

func NewManager(db *gorm.DB) *ManagerDb {
	return &ManagerDb{db: db, probeFunc: probeConnectivity}
}

var _ Manager = (*ManagerDb)(nil)
```

- [ ] **Step 3: 实现 Create 方法**

逻辑与现有 `llmModelService.CreateLLMModel` 一致，签名改为 `Create(ctx, orgID uint, req *CreateRequest) (*ModelConfig, error)`：
1. 验证 model/base_url/api_key 必填
2. 生成 code（`generateLLMModelCode()`），默认 name=model，默认 provider=openai
3. `normalizeLLMBaseURL` + `detectURLHasV1`
4. provider 为 openai/custom 时调用 `m.probeFunc` 探测连通性
5. 构建 `types.LLMModel`，默认 MaxTokens=4096, Temperature=0.7, TimeoutSec=120
6. 事务内：非 default 时检查是否首条（自动设为 default），default 时清除其他 default 标记
7. 调用 `db.CreateLLMModel(ctx, tx, model)`
8. 返回 `modelConfigFromEntity(model)`

- [ ] **Step 4: 实现 Get / GetDefault 方法**

- `Get(ctx, orgID, id, code)`: id>0 时按 ID 查，否则按 code 查（`db.GetLLMModelByID` / `db.GetLLMModelByCode`）；校验 OrgID 匹配
- `GetDefault(ctx, orgID)`: 调用 `db.GetDefaultLLMModel(ctx, m.db, orgID)`

- [ ] **Step 5: 实现 Update 方法**

逻辑与现有 `llmModelService.UpdateLLMModel` 一致：
1. 事务内查模型，校验 OrgID
2. 按 req 字段更新（Name/Description/Provider/Model/BaseURL/APIKey/Status/Config/IsDefault）
3. provider/model/baseURL/apiKey 变更时 `needsReDetect=true`
4. re-detect 时 openai/custom 调用 `m.probeFunc`，其他用 `detectURLHasV1`
5. IsDefault 设为 true 时清除其他默认标记
6. 调用 `db.UpdateLLMModel(ctx, tx, model)`

- [ ] **Step 6: 实现 Delete / List / TestConnectivity 方法**

- `Delete(ctx, orgID, id)`: 事务内查+校验+`db.DeleteLLMModel`
- `List(ctx, orgID, req)`: 构建 `types.NewPageQuery(types.Caller{OrgID: orgID}, ...)` + AddFilter，调用 `db.ListLLMModels`，转换为 `[]*ModelConfig`
- `TestConnectivity(ctx, orgID, req)`: 与现有 `TestLLMModel` 一致 — 查模型（可选）、构建 endpointURL、调用 `eino.NewChatModel`+`eino.NewFlow`、返回 `TestResult`

- [ ] **Step 7: 验证编译**

Run: `go build ./backend/internal/llm/...`
Expected: 编译成功

- [ ] **Step 8: 提交**

```bash
git add backend/internal/llm/manager_db.go
git commit -m "feat(llm): 实现 ManagerDb — CRUD + helper 函数迁移自 llm_model_service"
```

---

### Task 5: 迁移测试到 manager_db_test.go

**Files:**
- Create: `backend/internal/llm/manager_db_test.go`

**Interfaces:**
- Consumes: `llm.ManagerDb`, `llm.NewManager`, `types.LLMModel`

从 `backend/internal/service/llm_model_service_test.go` 迁移全部测试，适配新的签名（显式 orgID 参数）。

- [ ] **Step 1: 创建 manager_db_test.go — setup + mock**

```go
package llm

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(&types.LLMModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return database
}

func setupManager(t *testing.T) (*ManagerDb, *gorm.DB) {
	t.Helper()
	m := NewManager(setupTestDB(t))
	m.probeFunc = mockProbeSuccessV1
	return m, m.db
}

func managerWithProbe(db *gorm.DB, probe func(context.Context, string, string, string, string, bool) *probeResult) *ManagerDb {
	m := NewManager(db)
	m.probeFunc = probe
	return m
}

func mockProbeSuccessV1(context.Context, string, string, string, string, bool) *probeResult {
	return &probeResult{v1Success: true}
}
func mockProbeSuccessNoV1(context.Context, string, string, string, string, bool) *probeResult {
	return &probeResult{noV1Success: true}
}
func mockProbeAlwaysFail(context.Context, string, string, string, string, bool) *probeResult {
	return &probeResult{}
}

const testOrgID uint = 1
```

- [ ] **Step 2: 迁移 Create 测试**

从 service 测试迁移，关键变化：
- `service.CreateLLMModel(ctx, req)` → `m.Create(ctx, testOrgID, req)`
- 请求类型从 `contract.CreateLLMModelRequest` → `llm.CreateRequest`（字段名一致）
- 验证返回 `*llm.ModelConfig` 而非 `*contract.LLMModel`

迁移的测试函数：
- `TestCreateLLMModelGeneratesCodeDefaultsNameAndMasksAPIKey`
- `TestCreateLLMModelRequiresAPIKey`
- `TestCreateLLMModelRequiresBaseURL`
- `TestCreateLLMModelTrimsChatCompletionsPath`
- `TestCreateLLMModelForcesFirstOrgModelDefault`
- `TestCreateLLMModelKeepsSingleDefault`
- `TestCreateLLMModelStoresBaseURLHasV1WhenProbeV1Succeeds`
- `TestCreateLLMModelStoresBaseURLHasV1FalseWhenNoV1Succeeds`
- `TestCreateLLMModelFailsWhenBothProbesFail`

- [ ] **Step 3: 迁移 Update/Delete/List/Helper 测试**

迁移：
- `TestUpdateLLMModelKeepsAPIKeyWhenOmitted`
- `TestUpdateLLMModelKeepsSingleDefault`
- `TestUpdateLLMModelTrimsChatCompletionsPath`
- `TestUpdateLLMModelUpdatesMaskedAPIKeyWhenProvided`
- `TestUpdateLLMModelRedetectsBaseURLHasV1WhenBaseURLChanges`
- `TestUpdateLLMModelFailsWhenProbeFailsAfterRelevantChange`
- `TestDeleteLLMModelDoesNotLeaveMultipleDefaults`
- `TestNormalizeLLMBaseURLTrimsKnownEndpointSuffixes`
- `TestDetectURLHasV1`
- `TestBuildLLMEndpointURL`

DB 验证方式不变：通过 `db.GetLLMModelByID(ctx, database, model.ID)` 直接查 DB。testOrgID=1 与 service 测试一致。

- [ ] **Step 4: 运行测试**

Run: `go test -v ./backend/internal/llm/...`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llm/manager_db_test.go
git commit -m "test(llm): 迁移 manager_db 测试 — 全部 CRUD + helper 覆盖"
```

---

### Task 6: 实现 contract_adapter（contract_adapter.go）

**Files:**
- Create: `backend/internal/llm/contract_adapter.go`

**Interfaces:**
- Consumes: `llm.Manager`, `contract.LLMModelService`（依赖 contract 类型）
- Produces: `llm.NewContractAdapter(manager Manager) contract.LLMModelService`

适配层将 `contract.LLMModelService` 接口方法委托到 `llm.Manager`，从 context 解析 orgID 后传入。

- [ ] **Step 1: 创建 contract_adapter.go**

```go
package llm

import (
	"context"

	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/types"
)

// ContractAdapter 将 llm.Manager 适配为 contract.LLMModelService。
type ContractAdapter struct {
	manager Manager
}

// NewContractAdapter 创建适配 contract.LLMModelService 的实现。
func NewContractAdapter(manager Manager) contract.LLMModelService {
	return &ContractAdapter{manager: manager}
}

func (a *ContractAdapter) CreateLLMModel(ctx context.Context, req *contract.CreateLLMModelRequest) (*contract.LLMModel, error) {
	orgID, err := orgIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := a.manager.Create(ctx, orgID, &CreateRequest{
		Name:        req.Name,
		Description: req.Description,
		Provider:    req.Provider,
		Model:       req.Model,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Status:      req.Status,
		IsDefault:   req.IsDefault,
		Config:      req.Config,
	})
	if err != nil {
		return nil, err
	}
	return modelConfigToContract(cfg), nil
}

// orgIDFromContext 从认证上下文解析 orgID。
func orgIDFromContext(ctx context.Context) (uint, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return 0, errNotAuthenticated
	}
	return caller.OrgID, nil
}
```

完整方法列表（全部委托到 Manager，做 `llm.ModelConfig` ↔ `contract.LLMModel` 转换）：
- `CreateLLMModel` → `manager.Create`
- `GetLLMModel` → `manager.Get`
- `GetDefaultLLMModel` → `manager.GetDefault`
- `UpdateLLMModel` → `manager.Update`
- `DeleteLLMModel` → `manager.Delete`
- `ListLLMModels` → `manager.List`
- `TestLLMModel` → `manager.TestConnectivity`

`modelConfigToContract(cfg *ModelConfig) *contract.LLMModel` 做字段映射，注意 `APIKey` 映射为 masked 值（`maskAPIKey(cfg.APIKey)`）。

- [ ] **Step 2: 验证编译**

Run: `go build ./backend/internal/llm/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add backend/internal/llm/contract_adapter.go
git commit -m "feat(llm): 实现 ContractAdapter — contract.LLMModelService 适配层"
```

---

### Task 7: 修改 service/llm_model_service.go 委托到 ContractAdapter

**Files:**
- Modify: `backend/internal/service/llm_model_service.go` — 删除全部实现逻辑，改为委托
- Modify: `backend/internal/api/router.go` — 调整初始化方式

- [ ] **Step 1: 重写 llm_model_service.go**

将 `backend/internal/service/llm_model_service.go` 替换为薄委托层：

```go
package service

import (
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/llm"
)

// NewLLMModelService 创建 LLM 模型配置服务，委托到 llm.Manager。
func NewLLMModelService(db *gorm.DB) contract.LLMModelService {
	return llm.NewContractAdapter(llm.NewManager(db))
}
```

删除：`llmModelService` 结构体、全部方法实现、全部 helper 函数（`normalizeLLMBaseURL` / `detectURLHasV1` / `probeConnectivity` / `maskAPIKey` 等）— 这些已迁移到 `internal/llm/`。

- [ ] **Step 2: 删除 llm_model_service_test.go 中已迁移的测试**

`backend/internal/service/llm_model_service_test.go` 中的测试已迁移到 `internal/llm/manager_db_test.go`。删除 service 层的重复测试。但保留 service 包的 setup helper 函数定义（被其他测试引用）。

检查是否有其他 test 文件引用 `setupLLMModelService` / `mockProbeSuccessV1` 等 helper：
- 如果有引用，保留 helper 定义在 service test 包
- 如果没有引用，删除

Run: `grep -r "setupLLMModelService\|mockProbeSuccess" --include="*_test.go" ./backend/internal/service/` 检查引用。

- [ ] **Step 3: 验证 router.go 编译**

`router.go:105` 现有 `service.NewLLMModelService(db)` 调用签名不变，无需修改。

Run: `go build ./backend/internal/api/...`
Expected: 编译成功

- [ ] **Step 4: 运行全部测试**

Run: `go test ./backend/internal/llm/... ./backend/internal/service/...`
Expected: 全部 PASS

- [ ] **Step 5: 运行全量构建检查**

Run: `go build ./backend/cmd/lework/`
Expected: 编译成功

- [ ] **Step 6: 提交**

```bash
git add backend/internal/service/llm_model_service.go backend/internal/service/llm_model_service_test.go
git commit -m "refactor(service): llm_model_service 委托到 llm.Manager — 删除冗余实现逻辑"
```

---

## 验收标准

- [ ] `go build ./backend/cmd/lework/` 成功
- [ ] `go test ./backend/internal/llm/...` 全部 PASS
- [ ] `go test ./backend/internal/service/...` 全部 PASS
- [ ] `go vet ./backend/internal/llm/...` 无警告
- [ ] 前端 LLM 模型管理页功能不变（Create/Get/Update/Delete/List/Test 均正常）
- [ ] `types.LLMCallRecord` 表定义就绪（AutoMigrate 在 Phase 2 启用）
