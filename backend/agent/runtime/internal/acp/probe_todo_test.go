package acp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/insmtx/Lework/backend/agent"
)

// TestOpenCodeACPTodoProbe 验证 v2 待办链路：experimental.todo 打开后，
// 模型调用 todowrite，ACP 帧里能拿到 RawInput.todos，且 Translator 产出 todo.updated。
//
//	LEWORK_ACP_PROBE=1 LEWORK_ACP_RECORD=1 ... \
//	  go test ./backend/agent/runtime/internal/acp -run TestOpenCodeACPTodoProbe -v
func TestOpenCodeACPTodoProbe(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x and network)")
	}

	binary := envOrDefault("LEWORK_ACP_PROBE_BIN", "opencode")
	model := envOrDefault("LEWORK_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")

	root, workDir := newProbeWorkspace(t)
	cfg, err := probeConfigMap(model)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	cfg["experimental"] = map[string]any{"todo": true}
	content, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal probe config: %v", err)
	}
	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+string(content))

	frameSink, closeSink := openFrameSinkNamed(t, "opencode-2.1.4-todo.ndjson")
	if closeSink != nil {
		defer closeSink()
	}
	conn, err := Start(context.Background(), Config{
		Binary:    binary,
		Dir:       workDir,
		Env:       env,
		FrameSink: frameSink,
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	sess, err := conn.NewSession(ctx, workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sess.RequireModel(model); err != nil {
		t.Fatalf("%v", err)
	}

	before := conn.Client().Snapshot().UpdateCount
	_, err = sess.Prompt(ctx,
		"请调用 todowrite 工具创建三条待办事项，内容分别是：准备环境、编写代码、运行测试。"+
			"状态依次为 pending、in_progress、pending。必须调用工具，不要只回答。")
	if err != nil {
		t.Fatalf("todo prompt: %v%s", err, stderrSuffix(conn))
	}

	snapshot := conn.Client().Snapshot()
	updates := snapshot.Updates[min(before, len(snapshot.Updates)):]
	if len(updates) == 0 {
		t.Fatalf("no session/update frames received")
	}
	t.Logf("update kinds: %v", snapshot.Kinds)

	// 1) 在 ACP 帧里找 todowrite 的 RawInput.todos。
	type todoFrame struct {
		toolCallID string
		status     string
		raw        string
		items      []agent.RuntimeTodoItem
	}
	var frames []todoFrame
	for _, update := range updates {
		var (
			toolCallID string
			status     string
			rawInput   any
		)
		switch {
		case update.Update.ToolCall != nil:
			toolCallID = string(update.Update.ToolCall.ToolCallId)
			status = string(update.Update.ToolCall.Status)
			rawInput = update.Update.ToolCall.RawInput
		case update.Update.ToolCallUpdate != nil:
			toolCallID = string(update.Update.ToolCallUpdate.ToolCallId)
			if update.Update.ToolCallUpdate.Status != nil {
				status = string(*update.Update.ToolCallUpdate.Status)
			}
			rawInput = update.Update.ToolCallUpdate.RawInput
		default:
			continue
		}
		items := ParseTodos(rawInput)
		if len(items) == 0 {
			continue
		}
		encoded, _ := json.Marshal(rawInput)
		frames = append(frames, todoFrame{
			toolCallID: toolCallID,
			status:     status,
			raw:        TruncateToolOutput(string(encoded)),
			items:      items,
		})
	}
	if len(frames) == 0 {
		t.Fatalf("no todowrite RawInput.todos observed; toolCalls=%v",
			toolCallTitles(snapshot))
	}

	first := frames[0]
	t.Logf("todowrite frames: %d, first rawInput=%s", len(frames), truncate(first.raw, 600))
	if len(first.items) != 3 {
		t.Fatalf("first snapshot items = %d, want 3", len(first.items))
	}
	statuses := make([]string, 0, len(first.items))
	for _, item := range first.items {
		if strings.TrimSpace(item.Title) == "" {
			t.Fatalf("empty todo title: %+v", item)
		}
		if item.ID == "" || !strings.HasPrefix(item.ID, "todo_") {
			t.Fatalf("todo id = %q, want todo_<n>", item.ID)
		}
		statuses = append(statuses, item.Status)
	}
	t.Logf("todos: %+v statuses=%v", first.items, statuses)

	// 2) Translator 必须产出 todo.updated（而不是工具卡）。
	translator := NewTranslator(conn.Stats())
	var todoEvents int
	for _, update := range updates {
		for _, event := range translator.Handle(update) {
			if event.Type == agent.NodeEventTodoUpdated {
				todoEvents++
				if payload, ok := event.Payload.(*agent.TodoUpdatedPayload); ok && len(payload.Items) == 0 {
					t.Fatalf("todo.updated with empty items")
				}
			}
			if event.Type == agent.NodeEventToolExecutionStart {
				if payload, ok := event.Payload.(*agent.ToolExecutionStartPayload); ok && payload.Name == "todowrite" {
					t.Fatalf("todowrite leaked into tool card")
				}
			}
		}
	}
	if todoEvents == 0 {
		t.Fatalf("translator emitted no todo.updated")
	}

	// 3) 记录重复完整快照的情况（上游是否随进度反复下发全量列表）。
	duplicates := 0
	for i := 1; i < len(frames); i++ {
		if frames[i].raw == frames[i-1].raw {
			duplicates++
		}
	}
	t.Logf("todo snapshots=%d duplicates=%d", len(frames), duplicates)
}

func toolCallTitles(snapshot ClientSnapshot) []string {
	titles := make([]string, 0, len(snapshot.ToolCalls))
	for _, call := range snapshot.ToolCalls {
		titles = append(titles, call.Kind+"/"+call.Title)
	}
	return titles
}
