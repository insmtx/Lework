package claude

import (
	"context"
	"errors"
	"github.com/bytedance/sonic"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
)

func TestAdapterAskCurrentTime(t *testing.T) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not found in PATH")
	}
	apiKey := firstNonEmptyEnv("LEROS_LLM_API_KEY")
	if apiKey == "" {
		t.Skip("set LEROS_LLM_API_KEY to run the real claude adapter test")
	}

	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	adapter := NewAdapter(claudePath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	handle, err := adapter.Invoke(ctx, externalcli.InvocationRequest{
		WorkDir: workDir,
		Prompt:  "Answer with the current system time. Do not modify files.",
		Model: agent.ModelConfig{
			Provider: "anthropic",
			APIKey:   apiKey,
			Model:    firstNonEmptyEnv("LEROS_LLM_MODEL"),
			BaseURL:  firstNonEmptyEnv("LEROS_LLM_BASE_URL"),
		},
	})
	if err != nil {
		t.Fatalf("run claude adapter: %v", err)
	}

	var finalEvent agent.Event
	var result string
	for event := range handle.Events {
		t.Logf("received event: type=%s, content=%s", event.Type, event.Content)
		if event.Type == events.EventResult {
			result = strings.TrimSpace(event.Content)
		}
		finalEvent = event
	}
	if finalEvent.Type == events.EventInvocationFailed {
		t.Fatalf("claude execution failed: %s", finalEvent.Content)
	}
	if finalEvent.Type != events.EventInvocationCompleted {
		t.Fatalf("unexpected final event: %#v", finalEvent)
	}

	if result == "" {
		t.Fatal("expected non-empty claude result")
	}
	t.Logf("claude current time result: %s", result)
}

func TestParseClaudeLineEmitsResultEvent(t *testing.T) {
	state := &claudeStreamState{}
	event := parseClaudeLine(`{"type":"result","result":"final","is_error":false}`, state)
	if event.Type != events.EventResult || event.Content != "final" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if state.result != "final" || state.isError {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestParseClaudeLineAttachesUsageToResultEvent(t *testing.T) {
	state := &claudeStreamState{}
	parsed := parseClaudeLineEvents(`{"type":"result","result":"final","is_error":false,"usage":{"input_tokens":10,"cache_creation_input_tokens":2,"cache_read_input_tokens":30,"output_tokens":4}}`, state)
	if len(parsed) != 1 {
		t.Fatalf("expected result event, got %#v", parsed)
	}
	if parsed[0].Type != events.EventResult {
		t.Fatalf("expected event result, got %#v", parsed[0])
	}
	result, err := events.DecodePayload[events.MessageResultPayload](&parsed[0])
	if err != nil {
		t.Fatalf("decode result payload: %v", err)
	}
	if result.Message != "final" {
		t.Fatalf("unexpected result message: %q", result.Message)
	}
	if result.Usage == nil || result.Usage.InputTokens != 42 || result.Usage.OutputTokens != 4 || result.Usage.TotalTokens != 46 {
		t.Fatalf("unexpected usage payload: %#v", result.Usage)
	}
}

func TestBuildArgsAppendsSystemPrompt(t *testing.T) {
	args := buildArgs(externalcli.InvocationRequest{
		SystemPrompt: "system only",
		Prompt:       "user only",
	})

	value, ok := argValue(args, "--append-system-prompt")
	if !ok {
		t.Fatalf("expected --append-system-prompt in args: %#v", args)
	}
	if value != "system only" {
		t.Fatalf("expected system prompt arg value, got %q", value)
	}
	if args[len(args)-1] != "--print" {
		t.Fatalf("expected claude to run in print mode, got %#v", args)
	}
	if containsArg(args, "user only") {
		t.Fatalf("expected user prompt to be passed via stdin, got %#v", args)
	}
}

func TestBuildArgsBypassesPermissionsByDefault(t *testing.T) {
	args := buildArgs(externalcli.InvocationRequest{})

	value, ok := argValue(args, "--permission-mode")
	if !ok {
		t.Fatalf("expected --permission-mode in args: %#v", args)
	}
	if value != "bypassPermissions" {
		t.Fatalf("expected bypassPermissions permission mode, got %q", value)
	}

	if !containsArg(args, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions in args: %#v", args)
	}

	if !containsArg(args, "--input-format") {
		t.Fatalf("expected --input-format in args: %#v", args)
	}
}

func TestBuildArgsIncludesPartialMessages(t *testing.T) {
	args := buildArgs(externalcli.InvocationRequest{})

	if !containsArg(args, "--include-partial-messages") {
		t.Fatalf("expected --include-partial-messages in args: %#v", args)
	}
}

func TestBuildArgsSkipsEmptySystemPrompt(t *testing.T) {
	args := buildArgs(externalcli.InvocationRequest{
		SystemPrompt: "   ",
		Prompt:       "user only",
	})

	if _, ok := argValue(args, "--append-system-prompt"); ok {
		t.Fatalf("expected empty system prompt to be skipped: %#v", args)
	}
}

func TestClaudeModelEnvDoesNotSetAuthVars(t *testing.T) {
	// API key and base URL are now injected via --settings file, not via cmd.Env.
	env := claudeModelEnv(agent.ModelConfig{
		APIKey:  "sk-test",
		BaseURL: "http://127.0.0.1:8081/v1/",
	})
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("ANTHROPIC_API_KEY should not be set via cmd.Env; use settings file instead")
	}
	if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("ANTHROPIC_BASE_URL should not be set via cmd.Env; use settings file instead")
	}
}

func TestParseClaudeLineTracksAssistantFallback(t *testing.T) {
	state := &claudeStreamState{}
	event := parseClaudeLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}`, state)
	if event.Type != events.EventMessageDelta || event.Content != "answer" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if state.lastAssistantText != "answer" {
		t.Fatalf("got %q, want answer", state.lastAssistantText)
	}
}

func TestParseClaudeStreamTextDeltasShareMessageIDUntilBoundary(t *testing.T) {
	state := &claudeStreamState{}
	first := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"也没有"}}}`, state)
	second := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"发现"}}}`, state)

	firstPayload := decodeMessageDeltaPayload(t, first)
	secondPayload := decodeMessageDeltaPayload(t, second)
	if firstPayload.Content != "也没有" || secondPayload.Content != "发现" {
		t.Fatalf("unexpected contents: %#v %#v", firstPayload, secondPayload)
	}
	if firstPayload.MessageID == "" || firstPayload.MessageID != secondPayload.MessageID {
		t.Fatalf("expected shared message_id, got %q and %q", firstPayload.MessageID, secondPayload.MessageID)
	}
}

func TestParseClaudeStreamTextDeltaStartsNewMessageAfterToolBoundary(t *testing.T) {
	state := &claudeStreamState{}
	first := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第一段"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"Bash","input":{}}}}`, state)
	second := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第二段"}}}`, state)

	firstPayload := decodeMessageDeltaPayload(t, first)
	secondPayload := decodeMessageDeltaPayload(t, second)
	if firstPayload.MessageID == "" || secondPayload.MessageID == "" || firstPayload.MessageID == secondPayload.MessageID {
		t.Fatalf("expected different message_id after tool boundary, got %q and %q", firstPayload.MessageID, secondPayload.MessageID)
	}
}

func TestParseClaudeStreamTextDeltaStartsNewMessageAfterResultBoundary(t *testing.T) {
	state := &claudeStreamState{}
	first := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第一段"}}}`, state)
	parseClaudeLineEvents(`{"type":"result","result":"","is_error":false}`, state)
	second := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第二段"}}}`, state)

	firstPayload := decodeMessageDeltaPayload(t, first)
	secondPayload := decodeMessageDeltaPayload(t, second)
	if firstPayload.MessageID == "" || secondPayload.MessageID == "" || firstPayload.MessageID == secondPayload.MessageID {
		t.Fatalf("expected different message_id after result boundary, got %q and %q", firstPayload.MessageID, secondPayload.MessageID)
	}
}

func TestParseClaudeStreamTextDeltaStartsNewMessageAfterContentBlockStopBoundary(t *testing.T) {
	state := &claudeStreamState{}
	first := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第一段"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`, state)
	second := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"第二段"}}}`, state)

	firstPayload := decodeMessageDeltaPayload(t, first)
	secondPayload := decodeMessageDeltaPayload(t, second)
	if firstPayload.MessageID == "" || secondPayload.MessageID == "" || firstPayload.MessageID == secondPayload.MessageID {
		t.Fatalf("expected different message_id after content block stop boundary, got %q and %q", firstPayload.MessageID, secondPayload.MessageID)
	}
}

func TestParseClaudeAssistantSnapshotOnlyEmitsMissingSuffixAfterPartialDeltas(t *testing.T) {
	state := &claudeStreamState{}
	first := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"你好"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"，世界"}}}`, state)

	parsed := parseClaudeLineEvents(`{"type":"assistant","message":{"content":[{"type":"text","text":"你好，世界！"}]}}`, state)
	if len(parsed) != 1 {
		t.Fatalf("expected one suffix event, got %#v", parsed)
	}
	firstPayload := decodeMessageDeltaPayload(t, first)
	suffixPayload := decodeMessageDeltaPayload(t, parsed[0])
	if suffixPayload.Content != "！" {
		t.Fatalf("expected suffix only, got %#v", suffixPayload)
	}
	if suffixPayload.MessageID != firstPayload.MessageID {
		t.Fatalf("expected suffix to use same message_id, got %q and %q", suffixPayload.MessageID, firstPayload.MessageID)
	}
}

func TestParseClaudeAssistantSnapshotDoesNotRepeatCoveredPartialDeltas(t *testing.T) {
	state := &claudeStreamState{}
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"你好"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"，世界"}}}`, state)

	parsed := parseClaudeLineEvents(`{"type":"assistant","message":{"content":[{"type":"text","text":"你好，世界"}]}}`, state)
	if len(parsed) != 0 {
		t.Fatalf("expected covered snapshot to emit nothing, got %#v", parsed)
	}
}

func TestParseClaudeAssistantSnapshotDoesNotRepeatCoveredPartialDeltasAfterStop(t *testing.T) {
	state := &claudeStreamState{}
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"你好"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"，世界"}}}`, state)
	parseClaudeLineEvents(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`, state)

	parsed := parseClaudeLineEvents(`{"type":"assistant","message":{"content":[{"type":"text","text":"你好，世界"}]}}`, state)
	if len(parsed) != 0 {
		t.Fatalf("expected covered snapshot after stop to emit nothing, got %#v", parsed)
	}
}

func TestParseClaudeLineEmitsToolCallStarted(t *testing.T) {
	state := &claudeStreamState{}
	event := parseClaudeLine(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"call_123","name":"Bash","input":{"command":"date","description":"鏌ヨ褰撳墠绯荤粺鏃堕棿"}}]}}`, state)
	if event.Type != events.EventToolCallStarted {
		t.Fatalf("unexpected event type: %#v", event)
	}

	content := decodeEventContent(t, event.Content)
	if content["tool_call_id"] != "call_123" || content["name"] != "Bash" {
		t.Fatalf("unexpected tool call content: %#v", content)
	}
	args, ok := content["arguments"].(map[string]any)
	if !ok || args["command"] != "date" {
		t.Fatalf("unexpected tool call arguments: %#v", content["arguments"])
	}
}

func TestParseClaudeLineEmitsTodoSnapshotFromTodoWrite(t *testing.T) {
	state := &claudeStreamState{}
	parsed := parseClaudeLineEvents(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"call_todo","name":"TodoWrite","input":{"todos":[{"id":"t1","content":"Inspect code","status":"in_progress","priority":"high"},{"id":"t2","content":"Run tests","status":"pending"}]}}]}}`, state)
	event := findClaudeTestEvent(parsed, events.EventTodoSnapshot)
	if event == nil {
		t.Fatalf("expected todo snapshot event, got %#v", parsed)
	}
	if findClaudeTestEvent(parsed, events.EventToolCallStarted) != nil {
		t.Fatalf("todo write should not emit tool call event: %#v", parsed)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
	if err != nil {
		t.Fatalf("decode todo snapshot: %v", err)
	}
	if len(items) != 2 || items[0].ID != "t1" || items[0].Title != "Inspect code" || items[0].Status != "in_progress" || items[0].Priority != "high" {
		t.Fatalf("unexpected todo items: %#v", items)
	}
}

func TestParseClaudeLineEmitsTodoUpdateFromTaskUpdate(t *testing.T) {
	state := &claudeStreamState{}
	parsed := parseClaudeLineEvents(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"call_task","name":"TaskUpdate","input":{"id":"task_1","title":"Implement parser","status":"completed"}}]}}`, state)
	event := findClaudeTestEvent(parsed, events.EventTodoUpdated)
	if event == nil {
		t.Fatalf("expected todo update event, got %#v", parsed)
	}
	if findClaudeTestEvent(parsed, events.EventToolCallStarted) != nil {
		t.Fatalf("task update should not emit tool call event: %#v", parsed)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
	if err != nil {
		t.Fatalf("decode todo update: %v", err)
	}
	if len(items) != 1 || items[0].ID != "task_1" || items[0].Title != "Implement parser" || items[0].Status != "completed" {
		t.Fatalf("unexpected todo update items: %#v", items)
	}
}

func TestParseClaudeLineSuppressesToolCallForTaskListResult(t *testing.T) {
	state := &claudeStreamState{toolNames: map[string]string{"call_tasks": "TaskList"}}
	parsed := parseClaudeLineEvents(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"call_tasks","type":"tool_result","content":{"tasks":[{"id":"task_1","title":"Inspect","status":"completed"}]},"is_error":false}]}}`, state)
	event := findClaudeTestEvent(parsed, events.EventTodoSnapshot)
	if event == nil {
		t.Fatalf("expected todo snapshot event, got %#v", parsed)
	}
	if findClaudeTestEvent(parsed, events.EventToolCallCompleted) != nil || findClaudeTestEvent(parsed, events.EventToolCallFailed) != nil {
		t.Fatalf("task list should not emit tool result event: %#v", parsed)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
	if err != nil {
		t.Fatalf("decode todo snapshot: %v", err)
	}
	if len(items) != 1 || items[0].ID != "task_1" || items[0].Title != "Inspect" || items[0].Status != "completed" {
		t.Fatalf("unexpected task list items: %#v", items)
	}
}

func TestParseClaudeLineEmitsToolCallCompleted(t *testing.T) {
	state := &claudeStreamState{toolNames: map[string]string{"call_123": "Bash"}}
	event := parseClaudeLine(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"call_123","type":"tool_result","content":"Thu May 14 14:19:24 CST 2026","is_error":false}]}}`, state)
	if event.Type != events.EventToolCallCompleted {
		t.Fatalf("unexpected event type: %#v", event)
	}

	content := decodeEventContent(t, event.Content)
	if content["tool_call_id"] != "call_123" || content["name"] != "Bash" || content["result"] != "Thu May 14 14:19:24 CST 2026" || content["is_error"] != false {
		t.Fatalf("unexpected tool result content: %#v", content)
	}
}

func TestClaudeFailureContentPrefersClaudeResult(t *testing.T) {
	err := errors.New("exit status 1")
	state := &claudeStreamState{result: "authentication failed"}

	content := claudeFailureContent(err, state, "stderr detail")
	if content != "authentication failed (exit status 1)" {
		t.Fatalf("got %q", content)
	}
}

func TestClaudeFailureContentFallsBackToStderr(t *testing.T) {
	err := errors.New("exit status 1")

	content := claudeFailureContent(err, &claudeStreamState{}, "stderr detail")
	if content != "stderr detail (exit status 1)" {
		t.Fatalf("got %q", content)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func argValue(args []string, name string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1], true
		}
	}
	return "", false
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func decodeEventContent(t *testing.T, content string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := sonic.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatalf("decode event content: %v", err)
	}
	return decoded
}

func decodeMessageDeltaPayload(t *testing.T, event agent.Event) events.MessageDeltaPayload {
	t.Helper()
	if event.Type != events.EventMessageDelta {
		t.Fatalf("expected message delta, got %#v", event)
	}
	payload, err := events.DecodePayload[events.MessageDeltaPayload](&event)
	if err != nil {
		t.Fatalf("decode message delta payload: %v", err)
	}
	return payload
}

func findClaudeTestEvent(eventList []agent.Event, eventType agent.EventType) *agent.Event {
	for i := range eventList {
		if eventList[i].Type == eventType {
			return &eventList[i]
		}
	}
	return nil
}

// parseClaudeLine 测试辅助：解析单行 claude JSON，返回第一个事件。
func parseClaudeLine(line string, state *claudeStreamState) agent.Event {
	parsed := parseClaudeLineEvents(line, state)
	if len(parsed) == 0 {
		return agent.Event{}
	}
	return parsed[0]
}
