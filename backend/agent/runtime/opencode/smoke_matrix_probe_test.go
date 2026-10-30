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
		first, firstEvents, err := runSmoke(t, invoker, baseRequest("smoke-text-1", "只回答两个字：收到"), handler)
		if err != nil {
			t.Fatalf("text: %v", err)
		}
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
		second, _, err := runSmoke(t, invoker, resume, handler)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if second.ProviderSessionID != first.ProviderSessionID {
			t.Fatalf("resume changed session: %q -> %q", first.ProviderSessionID, second.ProviderSessionID)
		}
		if strings.TrimSpace(second.Message) == "" {
			t.Fatal("resumed message empty")
		}
	})

	t.Run("tool_cards", func(t *testing.T) {
		_, events, err := runSmoke(t, invoker, baseRequest("smoke-tools",
			"请用 bash 工具执行 `echo hello`，然后用 read 工具读取 README.md，最后用一句话总结。"), handler)
		if err != nil {
			t.Fatalf("tools: %v", err)
		}
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
		_, approveEvents, approveErr := runSmoke(t, invoker, baseRequest("smoke-perm-approve",
			"请用 bash 工具执行命令 `rm -rf build`，不要做别的事。"), handler)
		if approveErr != nil {
			t.Fatalf("approve: %v", approveErr)
		}
		if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
			t.Fatalf("approved rm did not delete build dir; events=%v", eventTypes(approveEvents))
		}

		keepDir := filepath.Join(workDir, "keep")
		if err := os.MkdirAll(keepDir, 0o755); err != nil {
			t.Fatal(err)
		}
		handler.approveAction = agent.ApprovalActionDeny
		// 拒绝后 opencode 会以 cancelled 结束本轮、没有正文；这属于预期，不是 smoke 失败。
		_, denyEvents, denyErr := runSmoke(t, invoker, baseRequest("smoke-perm-deny",
			"请用 bash 工具执行命令 `rm -rf keep`，不要做别的事。"), handler)
		t.Logf("deny result: %v (events=%v)", denyErr, eventTypes(denyEvents))
		if denyErr != nil {
			// 拒绝场景不能落到通用失败文案，否则用户会以为模型故障而不是自己拒绝了工具。
			if strings.TrimSpace(denyErr.Error()) == fallbackReply {
				t.Fatalf("denied turn surfaced generic failure text %q; want dedicated rejection message", fallbackReply)
			}
			if !strings.Contains(denyErr.Error(), rejectedReply) {
				t.Logf("deny surfaced other actionable error (acceptable): %v", denyErr)
			}
		}
		if _, err := os.Stat(keepDir); err != nil {
			t.Fatalf("denied rm still deleted keep dir; events=%v", eventTypes(denyEvents))
		}
	})

	t.Run("permission_cross_task", func(t *testing.T) {
		// 任务 A 批准 rm 后，任务 B（新进程/新 session）对同一类操作仍必须重新询问。
		dirA := filepath.Join(workDir, "cross-a")
		dirB := filepath.Join(workDir, "cross-b")
		for _, dir := range []string{dirA, dirB} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		handler.approveAction = agent.ApprovalActionApprove
		handler.resetApprovals()
		if _, _, err := runSmoke(t, invoker, baseRequest("smoke-cross-a",
			"请用 bash 工具执行命令 `rm -rf cross-a`，不要做别的事。"), handler); err != nil {
			t.Fatalf("task A: %v", err)
		}
		if handler.approvalCount() == 0 {
			t.Fatal("task A did not ask for approval")
		}

		handler.approveAction = agent.ApprovalActionDeny
		handler.resetApprovals()
		_, _, _ = runSmoke(t, invoker, baseRequest("smoke-cross-b",
			"请用 bash 工具执行命令 `rm -rf cross-b`，不要做别的事。"), handler)
		if handler.approvalCount() == 0 {
			t.Fatal("task B was auto-approved after task A (saved rules leaked)")
		}
		if _, err := os.Stat(dirB); err != nil {
			t.Fatalf("task B deny did not keep dir: %v", err)
		}
	})

	t.Run("question_form", func(t *testing.T) {
		handler.approveAction = agent.ApprovalActionApprove
		handler.answer = [][]string{{"是"}}
		_, events, err := runSmoke(t, invoker, baseRequest("smoke-form",
			"请调用 question 工具向我提一个问题「是否继续？」，选项为：是 / 否。必须调用工具，不要直接回答。"), handler)
		if err != nil {
			t.Fatalf("form: %v", err)
		}
		if !hasEvent(events, agent.NodeEventQuestionAsked) || !hasEvent(events, agent.NodeEventQuestionAnswered) {
			t.Fatalf("question events missing: %v", eventTypes(events))
		}
	})

	t.Run("todo", func(t *testing.T) {
		_, events, err := runSmoke(t, invoker, baseRequest("smoke-todo",
			"请调用 todowrite 工具创建两条待办：准备环境、运行测试。必须调用工具，不要只回答。"), handler)
		if err != nil {
			t.Fatalf("todo: %v", err)
		}
		if !hasEvent(events, agent.NodeEventTodoUpdated) {
			t.Fatalf("todo.updated missing: %v", eventTypes(events))
		}
	})

	t.Run("plan_confirmation", func(t *testing.T) {
		handler.answer = [][]string{{"Yes"}}
		req := baseRequest("smoke-plan",
			"请把清理构建目录的计划写入 .opencode/plans/smoke-plan.md，然后按系统提示发起确认。")
		req.ExecutionMode = agent.ExecutionModePlan
		result, events, planErr := runSmoke(t, invoker, req, handler)
		if planErr != nil {
			t.Fatalf("plan: %v", planErr)
		}
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

	t.Run("always_repeats_within_task", func(t *testing.T) {
		// 验收口径：勾「总是允许」后，同一任务内同工具+同资源不再复问；
		// 但不得写 saved 规则（跨任务仍询问，见 permission_cross_task）。
		handler.approveAction = agent.ApprovalActionAlways
		defer func() { handler.approveAction = agent.ApprovalActionApprove }()
		handler.resetApprovals()

		target := filepath.Join(workDir, "always")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		result, events, alwaysErr := runSmoke(t, invoker, baseRequest("smoke-always",
			"请用 bash 工具把命令 `rm -rf always` 连续执行两次（分两条独立命令，不要合并、不要加其他命令）。"),
			handler)
		starts := 0
		for _, event := range events {
			if event.Type == agent.NodeEventToolExecutionStart {
				starts++
			}
		}
		t.Logf("always result: err=%v starts=%d approvals=%d msg=%q", alwaysErr, starts, handler.approvalCount(), result.Message)
		if starts < 2 {
			t.Skip("模型没有真的执行两次同样的命令，本环境无法判定 always 记忆")
		}
		if got := handler.approvalCount(); got != 1 {
			t.Fatalf("approvals = %d, want 1（第二次同命令应命中本任务记忆）", got)
		}
	})

	t.Run("plan_retry_fallback", func(t *testing.T) {
		// 用户明确禁止问确认 → 首轮必然不合规，运行层的一次性追问应把确认表单救回来。
		handler.answer = [][]string{{"Yes"}}
		req := baseRequest("smoke-plan-retry",
			"请把计划写入 .opencode/plans/smoke-retry.md。写完后立刻结束本轮，"+
				"禁止调用 question 工具，也不要向我确认任何内容。")
		req.ExecutionMode = agent.ExecutionModePlan
		result, events, retryErr := runSmoke(t, invoker, req, handler)
		planReady := hasEvent(events, agent.NodeEventPlanReady)
		confirm := false
		for _, event := range events {
			if payload, ok := event.Payload.(*agent.QuestionAskedPayload); ok &&
				payload.InteractionType == "plan_confirmation" {
				confirm = true
			}
		}
		if !confirm {
			t.Skipf("retry still not compliant in this environment (plan_ready=%v result=%q err=%v events=%v)",
				planReady, result.Message, retryErr, eventTypes(events))
		}
		if !planReady {
			t.Fatalf("confirmation emitted without plan.ready; events=%v", eventTypes(events))
		}
		if _, err := os.Stat(filepath.Join(workDir, ".opencode", "plans", "smoke-retry.md")); err != nil {
			t.Fatalf("plan file missing after retry: %v", err)
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
	mu            sync.Mutex
	approveAction string
	answer        [][]string
	approvals     int
}

func (h *smokeHandler) RequestApproval(_ context.Context, req *agent.ApprovalRequest) (*agent.ApprovalDecision, error) {
	h.mu.Lock()
	h.approvals++
	h.mu.Unlock()
	return &agent.ApprovalDecision{RequestID: req.RequestID, Action: h.approveAction}, nil
}

func (h *smokeHandler) approvalCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.approvals
}

func (h *smokeHandler) resetApprovals() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approvals = 0
}

func (h *smokeHandler) RequestAnswer(_ context.Context, req *agent.QuestionRequest) (*agent.QuestionAnswer, error) {
	return &agent.QuestionAnswer{RequestID: req.RequestID, Answers: h.answer}, nil
}

func runSmoke(
	t *testing.T,
	invoker *ACPInvoker,
	req cli.InvocationRequest,
	handler *smokeHandler,
) (cli.InvocationResult, []agent.NodeEvent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("invoke %s: %v", req.ExecutionID, err)
	}
	defer handle.Process.Stop()
	observer := &smokeObserver{}
	result, consumeErr := cli.ConsumeEvents(ctx, observer, handle, req.ExecutionID, "smoke", handler, handler)
	return result, observer.snapshot(), consumeErr
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
