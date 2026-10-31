# 统一 LLM 包 Phase 3 实现计划

> Phase 1-2 已完成（骨架 + 模型管理 + Caller/Recorder + 控制面调用迁移）。本计划实现 Worker 代理迁移 + 用量上报。

**Goal:** 将 `modelrouter` 迁移到 `llm/proxy` 子包，解分层违规；实现用量上报（usage_reporter + usage_handler）；迁移 worker 集成点。

**Architecture:** `llm/proxy` 子包承载 HTTP 代理逻辑（handler/config/debug），通过 `ProxyBaseURL(workerAddr)` 纯函数解耦 identity 依赖。`WorkerProxyBaseURL` 留在 worker 包作为薄封装。`UsageReporter` 模仿 `PlanPublisher` 注入式模式，同步 HTTP 上报到控制面 `POST /internal/v1/llm/usage`。

**Tech Stack:** Go 1.25, gorm v1.30, gin, cloudwego/eino, NATS

## Global Constraints

- 禁止使用 `panic`，错误通过 `error` 返回
- `internal/llm/` 是库代码，不用 `os.Exit` / `lifecycle.Std()` / `log.Fatal`
- `internal/llm/` 不依赖 `internal/worker/`（分层边界）
- `ModelConfig.APIKey` 仅在 `internal/llm` 包内流转
- 导入组织：stdlib、第三方、内部包，三组用空行分隔
- 使用制表符缩进
- 提交消息用中文，约定式提交格式

## 设计决策

1. **`llm/proxy` 子包**：`backend/internal/llm/proxy/`，包含 handler/config/debug 从 modelrouter 逐行迁移。`proxy.UpstreamConfig` / `proxy.ModelStore` / `proxy.RegisterRoutes` / `proxy.HandleModelRoute` 等。
2. **解分层**：`proxy.ProxyBaseURL(workerAddr string) string` 纯函数。`modelrouter.WorkerProxyBaseURL` 留在 worker 包（或直接内联到 preparer），调用 `proxy.ProxyBaseURL(identity.WorkerAddr())`。
3. **UsageReporter 注入式**：`llm.UsageReporter` struct 持有 `ServerAddr` + `OrgID` + `AuthToken` + `HTTPClient`。`NewUsageReporter(cfg UsageReporterConfig)`。`Report(ctx, record *CallRecord)` 同步 HTTP POST。
4. **usage_handler 控制面端**：`POST /internal/v1/llm/usage` 端点接收上报，委托 `Recorder.RecordCall`。
5. **modelrouter 删除**：迁移完成后删除 `backend/internal/modelrouter/` 包，更新所有 import。

---

## File Map

| 文件 | 操作 | 职责 |
|---|---|---|
| `backend/internal/llm/proxy/config.go` | Create | UpstreamConfig（从 modelrouter/config.go 迁移） |
| `backend/internal/llm/proxy/handler.go` | Create | ModelStore + RegisterRoutes + handleModelRoute（从 modelrouter/handler.go 迁移） |
| `backend/internal/llm/proxy/debug.go` | Create | DebugLogger（从 modelrouter/debug.go 迁移） |
| `backend/internal/llm/proxy/proxy.go` | Create | ProxyBaseURL 纯函数（替代 modelrouter/proxy.go） |
| `backend/internal/llm/proxy/handler_test.go` | Create | 全部 17 个测试（从 modelrouter/handler_test.go 迁移） |
| `backend/internal/llm/usage_reporter.go` | Create | UsageReporter 同步 HTTP 上报 |
| `backend/internal/llm/usage_reporter_test.go` | Create | reporter 测试 |
| `backend/internal/llm/usage_handler.go` | Create | 控制面 HTTP handler 接收上报 |
| `backend/internal/api/router.go` | Modify | 注册 usage handler 路由 |
| `backend/internal/worker/agentrun/preparer_impl.go` | Modify | import 改为 llm/proxy |
| `backend/internal/worker/agentrun/preparer_impl_test.go` | Modify | import 改为 llm/proxy |
| `backend/internal/worker/agentrun/service_test.go` | Modify | import 改为 llm/proxy |
| `backend/internal/worker/router/router.go` | Modify | import 改为 llm/proxy |
| `backend/internal/worker/app/service.go` | Modify | Options.ModelStore 类型改为 `*proxy.ModelStore` |
| `backend/cmd/lework/worker.go` | Modify | import 改为 llm/proxy + 替换 NewModelStore |
| `backend/internal/modelrouter/` | Delete | 整个包删除 |

---

### Task 1: 迁移 modelrouter → llm/proxy（文件移动 + import 更新）

**Files:**
- Create: `backend/internal/llm/proxy/config.go`, `handler.go`, `debug.go`, `proxy.go`, `handler_test.go`
- Delete: `backend/internal/modelrouter/` (all 5 files)

**范围：** 逐文件迁移，保持逻辑不变。核心改动：
1. `package modelrouter` → `package proxy`
2. `proxy.go` 中删除 `identity` import，`ProxyBaseURL(workerAddr string) string` 改为接受参数
3. 测试文件 `package modelrouter` → `package proxy`
4. 所有 `modelrouter.UpstreamConfig` / `modelrouter.ModelStore` 等引用方改为 `proxy.UpstreamConfig` / `proxy.ModelStore`

`proxy.go` 新签名：
```go
package proxy

import "strings"

// ProxyBaseURL returns the built-in worker model proxy BaseURL.
// workerAddr is the worker's listen address (e.g., ":8081" or "127.0.0.1:8081").
// Returns empty string when workerAddr is empty.
func ProxyBaseURL(workerAddr string) string {
	addr := strings.TrimSpace(workerAddr)
	if addr == "" {
		return ""
	}
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return ensureV1Suffix(addr)
	}
	if strings.HasPrefix(addr, ":") {
		return ensureV1Suffix("http://127.0.0.1" + addr)
	}
	return ensureV1Suffix("http://" + addr)
}
```

- [ ] **Step 1: 创建 proxy/config.go**（从 modelrouter/config.go 迁移，改 package 名）
- [ ] **Step 2: 创建 proxy/debug.go**（从 modelrouter/debug.go 迁移，改 package 名）
- [ ] **Step 3: 创建 proxy/proxy.go**（重写，ProxyBaseURL 接受 workerAddr 参数，删除 identity import）
- [ ] **Step 4: 创建 proxy/handler.go**（从 modelrouter/handler.go 迁移，改 package 名，其余逻辑不变）
- [ ] **Step 5: 创建 proxy/handler_test.go**（从 modelrouter/handler_test.go 迁移，改 package 名）
- [ ] **Step 6: 验证 proxy 包独立编译**

Run: `go build ./backend/internal/llm/proxy/... && go test ./backend/internal/llm/proxy/...`
Expected: 编译成功 + 全部 17 个测试 PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/llm/proxy/
git commit -m "feat(llm/proxy): 迁移 modelrouter 到 llm/proxy 子包 — 解分层违规，ProxyBaseURL 改纯函数"
```

---

### Task 2: 更新 worker 集成点 import + WorkerProxyBaseURL 薄封装

**Files:**
- Modify: `backend/internal/worker/agentrun/preparer_impl.go`
- Modify: `backend/internal/worker/agentrun/preparer_impl_test.go`
- Modify: `backend/internal/worker/agentrun/service_test.go`
- Modify: `backend/internal/worker/router/router.go`
- Modify: `backend/internal/worker/app/service.go`
- Modify: `backend/cmd/lework/worker.go`

**改动模式：** 所有 `modelrouter` import 改为 `proxy "github.com/insmtx/Lework/backend/internal/llm/proxy"`。类型引用 `modelrouter.ModelStore` → `proxy.ModelStore` 等。

`preparer_impl.go` 的 `resolveModelRouting` 中：
- `modelrouter.UpstreamConfig` → `proxy.UpstreamConfig`
- `modelrouter.WorkerProxyBaseURL()` → `proxy.ProxyBaseURL(identity.WorkerAddr())` （需 import identity，但 preparer 已在 worker 包内，依赖 identity 合法）

`router.go` 的 `SetupRouter` 签名：`modelStore *proxy.ModelStore`。`modelrouter.RegisterRoutes` → `proxy.RegisterRoutes`。

`worker.go` 的 `modelrouter.NewModelStore()` → `proxy.NewModelStore()`。`startWorkerHTTPServer` 参数类型改为 `*proxy.ModelStore`。

`app/service.go` 的 `Options.ModelStore` 类型改为 `*proxy.ModelStore`。

- [ ] **Step 1-6: 批量更新 6 个文件的 import 和类型引用**
- [ ] **Step 7: 验证编译 + worker 测试**

Run: `go build ./backend/cmd/lework/ && go vet ./backend/internal/worker/... && go test ./backend/internal/worker/...`
Expected: 成功

- [ ] **Step 8: 删除 modelrouter 包**

```bash
rm -rf backend/internal/modelrouter/
```

验证编译：`go build ./backend/...`

- [ ] **Step 9: 更新 modelrouter README（如有需要）或删除**
- [ ] **Step 10: 提交**

```bash
git add -A
git commit -m "refactor(worker): 迁移 modelrouter 引用到 llm/proxy — 删除 modelrouter 包"
```

---

### Task 3: 实现 UsageReporter + usage_handler

**Files:**
- Create: `backend/internal/llm/usage_reporter.go`
- Create: `backend/internal/llm/usage_reporter_test.go`
- Create: `backend/internal/llm/usage_handler.go`
- Modify: `backend/internal/api/router.go` — 注册 usage handler 路由

### usage_reporter.go

模仿 plan_publisher 模式：

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

// UsageReporterConfig holds connection settings for reporting usage to the control plane.
type UsageReporterConfig struct {
	ServerAddr string
	OrgID     uint
	AuthToken string
	HTTPClient *http.Client
}

// UsageReporter sends call records to the control plane via synchronous HTTP.
type UsageReporter struct {
	cfg UsageReporterConfig
}

// NewUsageReporter creates a reporter. Falls back to http.DefaultClient.
func NewUsageReporter(cfg UsageReporterConfig) *UsageReporter {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &UsageReporter{cfg: cfg}
}

// Report sends a single call record to the control plane.
// Failure logs a warning but does not block the caller.
func (r *UsageReporter) Report(ctx context.Context, record *CallRecord) {
	if r == nil || record == nil || r.cfg.ServerAddr == "" || r.cfg.OrgID == 0 {
		return
	}
	payload, err := json.Marshal(usageReportPayload{
		OrgID:          r.cfg.OrgID,
		ModelConfigID:  record.ModelConfigID,
		Provider:       record.Provider,
		ModelName:      record.ModelName,
		EntryProtocol:  record.EntryProtocol,
		IsStream:       record.IsStream,
		InputTokens:    record.InputTokens,
		OutputTokens:   record.OutputTokens,
		TotalTokens:    record.TotalTokens,
		LatencyMS:      record.LatencyMS,
		StatusCode:     record.StatusCode,
		Success:        record.Success,
		ErrorMessage:   record.ErrorMessage,
		CallerType:     record.CallerType,
		CallerRef:      record.CallerRef,
		StartedAt:      record.StartedAt,
		FinishedAt:     record.FinishedAt,
	})
	if err != nil {
		logs.WarnContextf(ctx, "usage reporter: marshal failed: %v", err)
		return
	}

	url := fmt.Sprintf("http://%s/internal/v1/llm/usage",
		strings.TrimPrefix(strings.TrimPrefix(r.cfg.ServerAddr, "http://"), "https://"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		logs.WarnContextf(ctx, "usage reporter: create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.cfg.AuthToken)
	}

	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		logs.WarnContextf(ctx, "usage reporter: send: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logs.WarnContextf(ctx, "usage reporter: server returned %d", resp.StatusCode)
	}
}

type usageReportPayload struct {
	OrgID          uint      `json:"org_id"`
	ModelConfigID  uint      `json:"model_config_id"`
	Provider       string    `json:"provider"`
	ModelName      string    `json:"model_name"`
	EntryProtocol  string    `json:"entry_protocol"`
	IsStream       bool      `json:"is_stream"`
	InputTokens    int       `json:"input_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	LatencyMS      int64     `json:"latency_ms"`
	StatusCode     int       `json:"status_code"`
	Success        bool      `json:"success"`
	ErrorMessage   string    `json:"error_message"`
	CallerType     string    `json:"caller_type"`
	CallerRef      string    `json:"caller_ref"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
}
```

### usage_handler.go（控制面端）

```go
package llm

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterUsageRoute registers the internal usage report endpoint.
func RegisterUsageRoute(r gin.IRouter, recorder Recorder) {
	r.POST("/internal/v1/llm/usage", handleUsageReport(recorder))
}

func handleUsageReport(recorder Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload usageReportPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		record := &CallRecord{
			OrgID:          payload.OrgID,
			ModelConfigID:  payload.ModelConfigID,
			Provider:       payload.Provider,
			ModelName:      payload.ModelName,
			EntryProtocol:  payload.EntryProtocol,
			IsStream:       payload.IsStream,
			InputTokens:    payload.InputTokens,
			OutputTokens:   payload.OutputTokens,
			TotalTokens:    payload.TotalTokens,
			LatencyMS:      payload.LatencyMS,
			StatusCode:     payload.StatusCode,
			Success:        payload.Success,
			ErrorMessage:   payload.ErrorMessage,
			CallerType:     payload.CallerType,
			CallerRef:      payload.CallerRef,
			StartedAt:      payload.StartedAt,
			FinishedAt:     payload.FinishedAt,
		}
		if err := recorder.RecordCall(c.Request.Context(), record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
```

### usage_reporter_test.go

测试 `Report` 函数：
1. 用 `httptest.NewServer` 模拟控制面端
2. 构造 `UsageReporter` 指向 test server
3. 调用 `Report(ctx, record)`
4. 验证 test server 收到正确的 payload
5. 验证 nil reporter / empty config 不 panic

### router.go 修改

在 `backend/internal/api/router.go` 的路由注册中新增：
```go
llmUsageRecorder := llm.NewRecorder(db)  // 或复用已有 recorder
llm.RegisterUsageRoute(r, llmUsageRecorder)  // 注意：注册在非 authed 组上，内部端点
```

- [ ] **Step 1: 创建 usage_reporter.go**
- [ ] **Step 2: 创建 usage_handler.go**
- [ ] **Step 3: 创建 usage_reporter_test.go**
- [ ] **Step 4: 在 router.go 注册 usage route**
- [ ] **Step 5: 验证编译 + 测试**

Run: `go build ./backend/cmd/lework/ && go test ./backend/internal/llm/...`
Expected: 成功

- [ ] **Step 6: 提交**

```bash
git add backend/internal/llm/usage_reporter.go backend/internal/llm/usage_handler.go backend/internal/llm/usage_reporter_test.go backend/internal/api/router.go
git commit -m "feat(llm): 实现 UsageReporter + usage_handler — Worker 用量同步上报到控制面"
```

---

### Task 4: 在 proxy handler 中接入 UsageReporter

**Files:**
- Modify: `backend/internal/llm/proxy/handler.go`

在 `handleModelRoute` 中，每次请求完成后（stream 和 non-stream 两条路径），构造 `CallRecord` 并调用 `UsageReporter.Report(ctx, record)`。

需要修改 `ModelStore` 以持有 `UsageReporter`（可选注入），或修改 `RegisterRoutes` 签名增加 `reporter *UsageReporter` 参数。

设计选择：`RegisterRoutes(r gin.IRouter, store *ModelStore, reporter *llm.UsageReporter)`。reporter 为 nil 时不报（开发/测试环境）。

用量数据来源：
- `InputTokens`/`OutputTokens`/`TotalTokens`：从上游响应中解析（non-stream 直接从响应 JSON；stream 从最后一个 chunk 的 `usage` 字段）
- `LatencyMS`：请求开始时间到响应完成
- `StatusCode`：上游 HTTP 状态码
- `Success`：status < 400
- `CallerType`：`"http_proxy"`
- `EntryProtocol`：`entryProtocol` 参数值

注意：proxy 包不应 import `llm` 包（循环依赖），`UsageReporter` 类型需要定义在 proxy 包内或作为接口注入。

**方案：** 定义 `proxy.UsageReporter` 接口（`Report(ctx, *CallRecordLike)`），`llm.UsageReporter` 实现它。或更简单：proxy 持有一个 `func(ctx context.Context, record *ProxyCallRecord)` 回调函数。

选择回调函数方案最简洁：

```go
// proxy/handler.go
type UsageReportFunc func(ctx context.Context, record *ProxyCallRecord)

type ProxyCallRecord struct {
	OrgID         uint
	ModelConfigID uint
	Provider      string
	ModelName     string
	// ... 同 llm.CallRecord 的字段子集
}

// ModelStore 新增 reportUsage 字段
type ModelStore struct {
	configs     map[string]*UpstreamConfig
	httpClient  *http.Client
	mu          sync.RWMutex
	reportUsage UsageReportFunc
}

func (s *ModelStore) SetUsageReporter(fn UsageReportFunc) { s.reportUsage = fn }
```

在 `worker.go` 或 `app/service.go` 初始化时调用 `modelStore.SetUsageReporter(...)` 注入回调，回调内调用 `llm.UsageReporter.Report`。

- [ ] **Step 1: 在 proxy/handler.go 定义 ProxyCallRecord + UsageReportFunc + SetUsageReporter**
- [ ] **Step 2: 在 handleModelRoute 的 non-stream 和 stream 路径末尾调用 reportUsage**
- [ ] **Step 3: 在 worker.go 初始化时注入回调**
- [ ] **Step 4: 验证编译 + proxy 测试不破坏**

Run: `go build ./backend/cmd/lework/ && go test ./backend/internal/llm/proxy/...`
Expected: 成功

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llm/proxy/handler.go backend/cmd/lework/worker.go
git commit -m "feat(llm/proxy): 在 HTTP 代理中接入用量上报回调"
```

---

### Task 5: 在 worker.go 初始化 UsageReporter 并注入

**Files:**
- Modify: `backend/cmd/lework/worker.go`

在 `modelStore := proxy.NewModelStore()` 之后，构造 `UsageReporter` 并通过 `SetUsageReporter` 注入回调：

```go
modelStore := proxy.NewModelStore()
usageReporter := llm.NewUsageReporter(llm.UsageReporterConfig{
	ServerAddr: cfg.ServerAddr,
	OrgID:      cfg.OrgID,
	AuthToken:  cfg.AuthToken,
})
modelStore.SetUsageReporter(func(ctx context.Context, record *proxy.ProxyCallRecord) {
	usageReporter.Report(ctx, &llm.CallRecord{
		OrgID:         record.OrgID,
		ModelConfigID: record.ModelConfigID,
		Provider:      record.Provider,
		ModelName:     record.ModelName,
		// ... 字段映射
		CallerType:    "http_proxy",
	})
})
```

- [ ] **Step 1: 修改 worker.go 注入 usage reporter**
- [ ] **Step 2: 验证编译**

Run: `go build ./backend/cmd/lework/`
Expected: 成功

- [ ] **Step 3: 提交**

```bash
git add backend/cmd/lework/worker.go
git commit -m "feat(worker): 注入 UsageReporter 到 proxy — 完成用量上报链路"
```

---

## 验收标准

- [ ] `go build ./backend/cmd/lework/` 成功
- [ ] `go test ./backend/internal/llm/...` 全部 PASS（含 proxy 的 17 个迁移测试 + usage_reporter 测试 + Phase 1-2 旧测试）
- [ ] `go test ./backend/internal/worker/...` 通过
- [ ] `go vet ./backend/internal/llm/... ./backend/internal/worker/...` 无警告
- [ ] `gofmt -l backend/internal/llm/ backend/internal/worker/ backend/cmd/lework/` 无输出
- [ ] `backend/internal/modelrouter/` 目录已删除
- [ ] Worker HTTP 代理（`/v1/chat/completions` 等）行为不变
- [ ] Worker agent run 全链路功能不变
- [ ] 用量上报链路：proxy handler → UsageReporter → HTTP POST → usage_handler → Recorder → DB
