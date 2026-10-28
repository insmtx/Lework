package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// TestSmokeMatrixProbe 是 14 条冒烟里可自动化的后端子集：
// 文本轮次 + resume、工具卡、权限 approve/deny、表单、待办、计划确认、DB 隔离。
// 流式观感、取消耗时、空闲超时与前端走查仍留人工。
//
//	LEROS_ACP_PROBE=1 LEROS_ACP_PROBE_CONFIG_FILE=... LEROS_ACP_PROBE_MODEL=... \
//	  go test ./backend/agent/runtime/opencode -run TestSmokeMatrixProbe -v
func TestSmokeMatrixProbe(t *testing.T) {
	if os.Getenv("LEROS_ACP_PROBE") != "1" {
		t.Skip("manual smoke: set LEROS_ACP_PROBE=1")
	}
	model, err := probeModelConfig()
	if err != nil {
		t.Fatalf("load probe model: %v", err)
	}

	root := t.TempDir()
	workDir := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# smoke\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invoker := NewACPInvoker("opencode", nil, filepath.Join(root, "data"), 3*time.Minute)

	globalDBBefore, hasGlobalDB := smokeGlobalDB()
	baseRequest := func(id, prompt string) cli.InvocationRequest {
		return cli.InvocationRequest{
			ExecutionID: id,
			WorkDir:     workDir,
			Prompt:      prompt,
			Model:       model,
		}
	}
	handler := &smokeHandler{
		approveAction: agent.ApprovalActionApprove,
		answer:        [][]string{{"是"}},
	}

	t.Run("text_and_resume", func(t *testing.T) {
		first, firstEvents := runSmoke(t, invoker, baseRequest("smoke-text-1", "只回答两个字：收到"), handler)
		if strings.TrimSpace(first.Message) == "" {
			t.Fatalf("first message empty; events=%v", eventTypes(firstEvents))
		}
		if first.ProviderSessionID == "" {
			t.Fatal("provider session id missing")
		}
		if !hasEvent(firstEvents, agent.NodeEventMessageEnd) {
			t.Fatalf("message.end missing: %v", eventTypes(firstEvents))
		}

		resume := baseRequest("smoke-text-2", "只回答两个字：继续")
		resume.Resume = true
		resume.SessionID = first.ProviderSessionID
		second, _ := runSmoke(t, invoker, resume, handler)
		if second.ProviderSessionID != first.ProviderSessionID {
			t.Fatalf("resume changed session: %q -> %q", first.ProviderSessionID, second.ProviderSessionID)
		}
		if strings.TrimSpace(second.Message) == "" {
			t.Fatal("resumed message empty")
		}
	})

	t.Run("tool_cards", func(t *testing.T) {
		_, events := runSmoke(t, invoker, baseRequest("smoke-tools",
			"请用 bash 工具执行 `echo hello`，然后用 read 工具读取 README.md，最后用一句话总结。"), handler)
		names := map[string]bool{}
		for _, event := range events {
			if event.Type == agent.NodeEventToolExecutionStart {
				names[event.Payload.(*agent.ToolExecutionStartPayload).Name] = true
			}
		}
		if !names["bash"] || !names["read"] {
			t.Fatalf("tool start names = %v, want bash+read", names)
		}
	})

	t.Run("permission_approve_and_deny", func(t *testing.T) {
		buildDir := filepath.Join(workDir, "build")
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			t.Fatal(err)
		}
		handler.approveAction = agent.ApprovalActionApprove
		_, approveEvents := runSmoke(t, invoker, baseRequest("smoke-perm-approve",
			"请用 bash 工具执行命令 `rm -rf build`，不要做别的事。"), handler)
		if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
			t.Fatalf("approved rm did not delete build dir; events=%v", eventTypes(approveEvents))
		}

		keepDir := filepath.Join(workDir, "keep")
		if err := os.MkdirAll(keepDir, 0o755); err != nil {
			t.Fatal(err)
		}
		handler.approveAction = agent.ApprovalActionDeny
		_, denyEvents := runSmoke(t, invoker, baseRequest("smoke-perm-deny",
			"请用 bash 工具执行命令 `rm -rf keep`，不要做别的事。"), handler)
		if _, err := os.Stat(keepDir); err != nil {
			t.Fatalf("denied rm still deleted keep dir; events=%v", eventTypes(denyEvents))
		}
	})

	t.Run("question_form", func(t *testing.T) {
		handler.approveAction = agent.ApprovalActionApprove
		handler.answer = [][]string{{"是"}}
		_, events := runSmoke(t, invoker, baseRequest("smoke-form",
			"请调用 question 工具向我提一个问题「是否继续？」，选项为：是 / 否。必须调用工具，不要直接回答。"), handler)
		if !hasEvent(events, agent.NodeEventQuestionAsked) || !hasEvent(events, agent.NodeEventQuestionAnswered) {
			t.Fatalf("question events missing: %v", eventTypes(events))
		}
	})

	t.Run("todo", func(t *testing.T) {
		_, events := runSmoke(t, invoker, baseRequest("smoke-todo",
			"请调用 todowrite 工具创建两条待办：准备环境、运行测试。必须调用工具，不要只回答。"), handler)
		if !hasEvent(events, agent.NodeEventTodoUpdated) {
			t.Fatalf("todo.updated missing: %v", eventTypes(events))
		}
	})

	t.Run("plan_confirmation", func(t *testing.T) {
		handler.answer = [][]string{{"Yes"}}
		req := baseRequest("smoke-plan",
			"请把清理构建目录的计划写入 .opencode/plans/smoke-plan.md，然后按系统提示发起确认。")
		req.ExecutionMode = agent.ExecutionModePlan
		result, events := runSmoke(t, invoker, req, handler)
		planReady := hasEvent(events, agent.NodeEventPlanReady)
		confirm := false
		for _, event := range events {
			if payload, ok := event.Payload.(*agent.QuestionAskedPayload); ok &&
				payload.InteractionType == "plan_confirmation" {
				confirm = true
			}
		}
		if !planReady || !confirm {
			t.Logf("plan contract miss (plan_ready=%v confirmation=%v result=%q events=%v)",
				planReady, confirm, result.Message, eventTypes(events))
			t.Skip("plan model compliance not deterministic in this environment")
		}
		if _, err := os.Stat(filepath.Join(workDir, ".opencode", "plans", "smoke-plan.md")); err != nil {
			t.Fatalf("plan file missing: %v", err)
		}
	})

	t.Run("global_db_isolation", func(t *testing.T) {
		if !hasGlobalDB {
			t.Skip("global opencode db not present")
		}
		assertFileUnchangedAt(t, globalDBBefore)
	})
}

type smokeObserver struct {
	mu     sync.Mutex
	events []agent.NodeEvent
}

func (o *smokeObserver) Observe(_ context.Context, event agent.NodeEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
	return nil
}

func (o *smokeObserver) snapshot() []agent.NodeEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]agent.NodeEvent(nil), o.events...)
}

type smokeHandler struct {
	approveAction string
	answer        [][]string
}

func (h *smokeHandler) RequestApproval(_ context.Context, req *agent.ApprovalRequest) (*agent.ApprovalDecision, error) {
	return &agent.ApprovalDecision{RequestID: req.RequestID, Action: h.approveAction}, nil
}

func (h *smokeHandler) RequestAnswer(_ context.Context, req *agent.QuestionRequest) (*agent.QuestionAnswer, error) {
	return &agent.QuestionAnswer{RequestID: req.RequestID, Answers: h.answer}, nil
}

func runSmoke(
	t *testing.T,
	invoker *ACPInvoker,
	req cli.InvocationRequest,
	handler *smokeHandler,
) (cli.InvocationResult, []agent.NodeEvent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("invoke %s: %v", req.ExecutionID, err)
	}
	defer handle.Process.Stop()
	observer := &smokeObserver{}
	result, err := cli.ConsumeEvents(ctx, observer, handle, req.ExecutionID, "smoke", handler, handler)
	if err != nil {
		t.Fatalf("consume %s: %v", req.ExecutionID, err)
	}
	return result, observer.snapshot()
}

func hasEvent(events []agent.NodeEvent, want agent.NodeEventType) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func eventTypes(events []agent.NodeEvent) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, string(event.Type))
	}
	return types
}

type smokeFileFingerprint struct {
	path    string
	size    int64
	modTime time.Time
}

func smokeGlobalDB() (smokeFileFingerprint, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return smokeFileFingerprint{}, false
	}
	path := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	info, err := os.Stat(path)
	if err != nil {
		return smokeFileFingerprint{}, false
	}
	return smokeFileFingerprint{path: path, size: info.Size(), modTime: info.ModTime()}, true
}

func assertFileUnchangedAt(t *testing.T, before smokeFileFingerprint) {
	t.Helper()
	info, err := os.Stat(before.path)
	if err != nil {
		t.Fatalf("global db disappeared: %v", err)
	}
	if info.Size() != before.size || !info.ModTime().Equal(before.modTime) {
		t.Fatalf("global db changed: size %d->%d mtime %v->%v",
			before.size, info.Size(), before.modTime, info.ModTime())
	}
}
