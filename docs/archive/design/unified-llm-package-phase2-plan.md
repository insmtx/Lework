# 统一 LLM 包 Phase 2 实现计划

> Phase 1 已完成（骨架 + 模型管理收敛）。本计划实现 Caller + Recorder + 控制面调用迁移。

**Goal:** 实现 `Recorder`（调用记录持久化）和 `Caller`（统一调用入口），迁移控制面 3 个 LLM 调用点，注册 AutoMigrate。

**Architecture:** `Caller` 封装 `pkgeino.NewChatModel` + `chatModel.Generate`（非流式）或 `pkgeino.NewFlow` + `Flow.StreamWithUsage`（流式），在调用完成后通过 `Recorder` 持久化 `CallRecord`。`Recorder` 基于 gorm 操作 `llm_call_records` 表。

**Tech Stack:** Go 1.25, gorm v1.30, sqlite（测试）, cloudwego/eino

## Global Constraints

- 禁止使用 `panic`，错误通过 `error` 返回
- `internal/llm/` 是库代码，不用 `os.Exit` / `lifecycle.Std()` / `log.Fatal`
- `ModelConfig.APIKey` 仅在 `internal/llm` 包内流转
- 导入组织：stdlib、第三方、内部包，三组用空行分隔
- 使用制表符缩进
- 提交消息用中文，约定式提交格式
- 注释采用英文

## 范围调整说明

设计文档提到迁移 5 个调用点，实际探查后发现：
- `prompts/executor_eino.go` 接收 `config.LLMConfig`（非 DB 配置），无 orgID/ModelConfigID，不适用 Caller 当前接口。**本期不迁移**，留 Phase 3 评估。
- `llm/manager_db.go` 的 `TestConnectivity` 已在 Phase 1 收敛到 llm 包内，且使用 Flow 而非 chatModel.Generate。Phase 1 未迁移其用量持久化。**本期不单独迁移**，TestConnectivity 的用量记录留 Phase 3 统一处理。

本期迁移 3 个调用点：`feedback_summarizer` / `work_title_updater` / `default_skill_description_translator`。

---

## File Map

| 文件 | 操作 | 职责 |
|---|---|---|
| `backend/internal/infra/db/database.go` | Modify | AutoMigrate 列表加入 `&types.LLMCallRecord{}` |
| `backend/internal/infra/db/llm_call_record_dao.go` | Create | DAO 函数：Create/List |
| `backend/internal/infra/db/llm_call_record_dao_test.go` | Create | DAO 测试 |
| `backend/internal/llm/recorder.go` | Create | Recorder 接口定义 |
| `backend/internal/llm/recorder_db.go` | Create | Recorder 的 gorm 实现 |
| `backend/internal/llm/recorder_db_test.go` | Create | recorder 测试 |
| `backend/internal/llm/caller.go` | Create | Caller 接口定义 |
| `backend/internal/llm/caller_eino.go` | Create | Caller 的 eino 实现 |
| `backend/internal/llm/caller_eino_test.go` | Create | caller 测试 |
| `backend/internal/service/feedback_summarizer.go` | Modify | 迁移到 llm.Caller |
| `backend/internal/service/work_title_updater.go` | Modify | 迁移到 llm.Caller |
| `backend/internal/service/default_skill_description_translator.go` | Modify | 迁移到 llm.Caller |

---

### Task 1: 注册 AutoMigrate + DAO 函数

**Files:**
- Modify: `backend/internal/infra/db/database.go`
- Create: `backend/internal/infra/db/llm_call_record_dao.go`
- Create: `backend/internal/infra/db/llm_call_record_dao_test.go`

- [ ] **Step 1: 在 database.go 的 models 列表加入 LLMCallRecord**

在 `backend/internal/infra/db/database.go:120` 的 `&types.LLMModel{},` 后面新增：
```go
		&types.LLMCallRecord{},
```

- [ ] **Step 2: 创建 llm_call_record_dao.go**

Create `backend/internal/infra/db/llm_call_record_dao.go`:

```go
package db

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

// CreateLLMCallRecord persists an LLM call record.
func CreateLLMCallRecord(ctx context.Context, db *gorm.DB, record *types.LLMCallRecord) error {
	return db.WithContext(ctx).Create(record).Error
}

// ListLLMCallRecords queries call records with optional filters.
func ListLLMCallRecords(ctx context.Context, db *gorm.DB, orgID uint, offset, limit int, modelConfigID *uint, provider, callerType *string, success *bool) ([]*types.LLMCallRecord, int64, error) {
	var records []*types.LLMCallRecord
	var total int64

	query := db.WithContext(ctx).Model(&types.LLMCallRecord{}).Where("org_id = ?", orgID)
	if modelConfigID != nil {
		query = query.Where("model_config_id = ?", *modelConfigID)
	}
	if provider != nil && *provider != "" {
		query = query.Where("provider = ?", *provider)
	}
	if callerType != nil && *callerType != "" {
		query = query.Where("caller_type = ?", *callerType)
	}
	if success != nil {
		query = query.Where("success = ?", *success)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit > 0 {
		query = query.Limit(limit)
	} else {
		query = query.Limit(50)
	}
	query = query.Offset(offset).Order("started_at DESC")

	if err := query.Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}
```

- [ ] **Step 3: 创建 llm_call_record_dao_test.go**

测试 Create + List，含筛选和排序验证。创建两条记录（不同 provider/callerType/success），验证 total、filter、order。setup 用 in-memory sqlite + AutoMigrate `LLMCallRecord`。

- [ ] **Step 4: 运行测试**

Run: `go test -v ./backend/internal/infra/db/... -run TestCreateAndListLLMCallRecords`
Expected: PASS

- [ ] **Step 5: 验证编译**

Run: `go build ./backend/cmd/lework/`
Expected: 成功

- [ ] **Step 6: 提交**

```bash
git add backend/internal/infra/db/database.go backend/internal/infra/db/llm_call_record_dao.go backend/internal/infra/db/llm_call_record_dao_test.go
git commit -m "feat(db): 新增 LLMCallRecord DAO + AutoMigrate 注册"
```

---

### Task 2: 定义 Recorder 接口 + 实现

**Files:**
- Create: `backend/internal/llm/recorder.go`
- Create: `backend/internal/llm/recorder_db.go`
- Create: `backend/internal/llm/recorder_db_test.go`

- [ ] **Step 1: 创建 recorder.go**

```go
package llm

import "context"

// Recorder persists LLM call records and provides usage queries.
type Recorder interface {
	// RecordCall persists a single call record.
	RecordCall(ctx context.Context, record *CallRecord) error
	// ListCalls queries call records with optional filters.
	ListCalls(ctx context.Context, orgID uint, offset, limit int, modelConfigID *uint, provider, callerType *string, success *bool) ([]*CallRecord, int64, error)
}
```

- [ ] **Step 2: 创建 recorder_db.go**

`RecorderDb` struct holds `db *gorm.DB`。`NewRecorder(db)` 构造函数。`var _ Recorder = (*RecorderDb)(nil)` 断言。

`RecordCall`: 将 `*CallRecord` 转换为 `*types.LLMCallRecord`（`callRecordToEntity` 函数），调用 `db.CreateLLMCallRecord`。

`ListCalls`: 调用 `db.ListLLMCallRecords`，将 `[]*types.LLMCallRecord` 转换为 `[]*CallRecord`（`callRecordFromEntity` 函数）。

两个转换函数 `callRecordToEntity` 和 `callRecordFromEntity` 做 `llm.CallRecord` ↔ `types.LLMCallRecord` 的字段映射（17 个字段，ID/OrgID/ModelConfigID/Provider/ModelName/EntryProtocol/IsStream/InputTokens/OutputTokens/TotalTokens/LatencyMS/StatusCode/Success/ErrorMessage/CallerType/CallerRef/StartedAt/FinishedAt）。注意 `callRecordToEntity` 的 `gorm.Model{ID: r.ID}` 设法避免 gorm 在 Create 时因主键已设而跳过。

- [ ] **Step 3: 创建 recorder_db_test.go**

用 in-memory sqlite 测试：RecordCall 写入 → ListCalls 查询 → 验证字段一致性 + ID 非零 + total 正确。

- [ ] **Step 4: 运行测试**

Run: `go test -v ./backend/internal/llm/... -run TestRecorder`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llm/recorder.go backend/internal/llm/recorder_db.go backend/internal/llm/recorder_db_test.go
git commit -m "feat(llm): 实现 Recorder 接口 — 调用记录持久化与查询"
```

---

### Task 3: 定义 Caller 接口 + 实现

**Files:**
- Create: `backend/internal/llm/caller.go`
- Create: `backend/internal/llm/caller_eino.go`
- Create: `backend/internal/llm/caller_eino_test.go`

- [ ] **Step 1: 创建 caller.go**

```go
package llm

import "context"

// Caller is the unified entry point for LLM calls.
type Caller interface {
	// Call executes a non-streaming LLM call.
	Call(ctx context.Context, orgID uint, req *CallRequest) (*CallResult, error)
	// Stream executes a streaming LLM call, emitting deltas via sink.
	Stream(ctx context.Context, orgID uint, req *CallRequest, sink StreamSink) (*CallResult, error)
}
```

- [ ] **Step 2: 创建 caller_eino.go**

`CallerEino` struct holds `manager Manager` + `recorder Recorder`。`NewCaller(manager, recorder)` 构造函数。`var _ Caller = (*CallerEino)(nil)` 断言。

**`Call` 方法流程**：
1. `manager.Get(ctx, orgID, req.ModelConfigID, "")` 解析 `*ModelConfig`
2. `buildEinoChatModel(ctx, cfg, req)` 构建 eino chat model — 使用 `BuildLLMEndpointURL`、`ModelConfig.APIKey/Provider/ModelName`、可选 `req.Temperature/MaxTokens` 覆盖
3. `buildEinoMessages(req)` 转换 `[]Message` → `[]*einoschema.Message`（含 SystemPrompt 前置）
4. `chatModel.Generate(ctx, messages)` 调用 LLM
5. 从 `resp.ResponseMeta.Usage` 提取 Usage（`PromptTokens/CompletionTokens/TotalTokens`）
6. 构建 `CallRecord`（含 timing、usage、success/error、caller info）
7. `recorder.RecordCall(ctx, record)` 持久化（失败仅 warn 不阻塞）
8. 返回 `CallResult{Message: resp, Usage: usage, Record: record}`

**`Stream` 方法流程**：
1. 解析 config 同 Call
2. 构建 chat model 同 Call
3. `eino.NewFlow(ctx, &FlowConfig{Model: chatModel, SystemPrompt: req.SystemPrompt})` 构建 Flow
4. `flow.StreamWithUsage(ctx, prompt, sink)` — Flow 已返回 `(*schema.Message, *Usage, error)`
5. 构建 CallRecord + 持久化 + 返回

**helper 函数**：
- `buildEinoChatModel(ctx, cfg, req)` — 构建 `pkgeino.ChatModelConfig` 并调用 `pkgeino.NewChatModel`
- `buildEinoMessages(req)` — 转换消息列表（SystemPrompt 前置为 system message）
- `mapRole(role string) einoschema.RoleType` — "user"/"assistant"/"system"/"tool" 映射
- `extractUsage(resp *einoschema.Message) *Usage` — 从 ResponseMeta.Usage 提取，nil 安全

**imports**: stdlib (context, fmt, strings, time); third-party (einoschema, einomodel, logs); internal (pkg/eino)

- [ ] **Step 3: 创建 caller_eino_test.go**

测试 pure function（无需 LLM 调用）：
- `TestBuildEinoMessages`: 验证 SystemPrompt + Messages → 正确数量和角色
- `TestExtractUsage`: 验证从 ResponseMeta 提取 tokens；nil 输入返回零值 Usage

- [ ] **Step 4: 运行测试**

Run: `go test -v ./backend/internal/llm/... -run "TestBuildEinoMessages|TestExtractUsage"`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llm/caller.go backend/internal/llm/caller_eino.go backend/internal/llm/caller_eino_test.go
git commit -m "feat(llm): 实现 Caller 接口 — 统一 LLM 调用入口，自动记录用量"
```

---

### Task 4: 迁移 feedback_summarizer 到 llm.Caller

**Files:**
- Modify: `backend/internal/service/feedback_summarizer.go`

**现状**: `summarizeFeedbackWithLLM` (line 37-83) 通过 `db.GetSystemTranslationLLMModel` 获取模型，`pkgeino.NewChatModel` 构建 chat model，`chatModel.Generate` 调用。函数签名 `(ctx, database *gorm.DB, orgID uint, typeLabel, content string) (feedbackSummary, error)`。通过包级 `var summarizeFeedback = summarizeFeedbackWithLLM` 间接调用（便于测试 mock）。

**迁移方案**: 在函数内构造 `llm.NewCaller(llm.NewManager(database), llm.NewRecorder(database))`，先解析 system translation model 获取 `model.ID`，然后调用 `caller.Call(ctx, orgID, &CallRequest{ModelConfigID: model.ID, Messages: [...], CallerType: "feedback_summarizer"})`。

- [ ] **Step 1: 重写 summarizeFeedbackWithLLM**

保留函数签名不变。删除 `pkgeino.NewChatModel` + `chatModel.Generate` 代码块，替换为：
1. `db.GetSystemTranslationLLMModel(ctx, database, orgID)` 解析模型（保留原有逻辑）
2. `caller := llm.NewCaller(llm.NewManager(database), llm.NewRecorder(database))`
3. 构建 prompt（保留原有 template 替换逻辑）
4. `caller.Call(ctx, orgID, &llm.CallRequest{ModelConfigID: model.ID, Messages: [{Role:"user", Content: prompt}], CallerType: "feedback_summarizer"})`
5. 从 `result.Message.Content` 解析返回（保留 `parseFeedbackSummary` 调用）

删除不再需要的 import：`einoopenai`, `einoschema`, `pkgeino`。新增 import：无需新增（已有 `llm`）。

- [ ] **Step 2: 验证编译**

Run: `go build ./backend/internal/service/...`
Expected: 成功

- [ ] **Step 3: 提交**

```bash
git add backend/internal/service/feedback_summarizer.go
git commit -m "refactor(service): feedback_summarizer 迁移到 llm.Caller"
```

---

### Task 5: 迁移 work_title_updater 到 llm.Caller

**Files:**
- Modify: `backend/internal/service/work_title_updater.go`

**现状**: `generateShortWorkTitlesWithLLM` (line 401-442) 同样用 `infradb.GetSystemTranslationLLMModel` + `pkgeino.NewChatModel` + `chatModel.Generate`。签名 `(ctx, database *gorm.DB, input workTitleGenerationInput) (generatedWorkTitles, error)`，`input.OrgID` 提供 orgID。

- [ ] **Step 1: 重写 generateShortWorkTitlesWithLLM**

同 Task 4 模式：解析 model → 构造 caller → `caller.Call(ctx, input.OrgID, &CallRequest{ModelConfigID: model.ID, Messages: [...], CallerType: "work_title_updater"})` → 从 `result.Message.Content` 解析。

删除不再需要的 import：`einoopenai`, `einoschema`, `pkgeino`。

- [ ] **Step 2: 验证编译**

Run: `go build ./backend/internal/service/...`
Expected: 成功

- [ ] **Step 3: 提交**

```bash
git add backend/internal/service/work_title_updater.go
git commit -m "refactor(service): work_title_updater 迁移到 llm.Caller"
```

---

### Task 6: 迁移 default_skill_description_translator 到 llm.Caller

**Files:**
- Modify: `backend/internal/service/default_skill_description_translator.go`

**现状**: 最复杂的调用点。`defaultSkillDescriptionTranslator` struct 有 `db` 字段。`buildChatModel` (line 85-108) 构建共享 chat model。`doTranslate` (line 167) 和 `doTranslateDocument` (line 317) 在并发 batch 中调用 `chatModel.Generate`。chat model 在 batch 间共享。

**迁移方案**: 在 translator struct 上新增 `caller llm.Caller` 字段，在构造函数中初始化。`buildChatModel` 不再需要（删除）。`doTranslate` 和 `doTranslateDocument` 改为调用 `t.caller.Call(ctx, orgID, &CallRequest{...})`。

由于 `Translate` 方法从 `auth.FromContext(ctx)` 获取 orgID，需要将 orgID 传到 `doTranslate`/`doTranslateDocument`。

- [ ] **Step 1: 修改 translator struct + 构造函数**

在 `defaultSkillDescriptionTranslator` struct 新增 `caller llm.Caller` 字段。在构造函数（检查是哪个函数创建 translator）中初始化：`caller: llm.NewCaller(llm.NewManager(db), llm.NewRecorder(db))`。

- [ ] **Step 2: 修改 Translate / TranslateDocument**

将 orgID 传递到 `doTranslate` / `doTranslateDocument` 签名中。删除 `buildChatModel` 调用，改为传递 `model.ID` 和 `orgID`。

- [ ] **Step 3: 重写 doTranslate / doTranslateDocument**

将 `chatModel.Generate(ctx, messages)` 替换为 `t.caller.Call(ctx, orgID, &llm.CallRequest{ModelConfigID: modelID, Messages: [...], CallerType: "skill_translator"})`。从 `result.Message.Content` 获取响应。

注意并发安全：`CallerEino` 是 stateless 的（每次 Call 独立解析 config + 创建 chatModel），可安全在 batch goroutines 间共享。

- [ ] **Step 4: 删除 buildChatModel**

`buildChatModel` 不再被调用，删除。删除相关的不再需要的 import（`einoopenai`, `einoschema`, `pkgeino`, `model` 别名）。

- [ ] **Step 5: 验证编译**

Run: `go build ./backend/internal/service/...`
Expected: 成功

- [ ] **Step 6: 提交**

```bash
git add backend/internal/service/default_skill_description_translator.go
git commit -m "refactor(service): skill_description_translator 迁移到 llm.Caller"
```

---

## 验收标准

- [ ] `go build ./backend/cmd/lework/` 成功
- [ ] `go test ./backend/internal/llm/...` 全部 PASS（含 recorder + caller 新测试 + Phase 1 旧测试）
- [ ] `go test ./backend/internal/infra/db/... -run TestCreateAndListLLMCallRecords` PASS
- [ ] `go vet ./backend/internal/llm/...` 无警告
- [ ] `gofmt -l backend/internal/llm/ backend/internal/service/` 无输出
- [ ] 控制面业务功能不变（反馈总结、标题更新、技能翻译 — 不产生编译错误为最低要求）
- [ ] DB 中 `llm_call_records` 表通过 AutoMigrate 建表成功（在下次服务启动时生效）
