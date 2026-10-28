package acp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

type recordedFrame struct {
	Dir  string `json:"dir"`
	Line string `json:"line"`
}

type recordedMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func TestReplayProbeFrames(t *testing.T) {
	events, stats := replayUpdateFrames(t, "opencode-2.1.4-probe.ndjson")
	var messageUpdates, toolStarts, toolEnds int
	toolNames := map[string]struct{}{}
	for _, event := range events {
		switch event.Type {
		case agent.NodeEventMessageUpdate:
			messageUpdates++
			payload := event.Payload.(*agent.MessageUpdatePayload)
			if strings.TrimSpace(payload.MessageID) == "" || payload.Role != "assistant" {
				t.Fatalf("bad message.update payload: %+v", payload)
			}
		case agent.NodeEventToolExecutionStart:
			toolStarts++
			toolNames[event.Payload.(*agent.ToolExecutionStartPayload).Name] = struct{}{}
		case agent.NodeEventToolExecutionEnd:
			toolEnds++
			if event.Payload.(*agent.ToolExecutionEndPayload).Name == "" {
				t.Fatalf("tool end without name: %+v", event.Payload)
			}
		}
	}
	if messageUpdates == 0 || toolStarts == 0 || toolEnds == 0 {
		t.Fatalf("text/tool frames missing: text=%d start=%d end=%d", messageUpdates, toolStarts, toolEnds)
	}
	for _, want := range []string{"bash", "read"} {
		if _, ok := toolNames[want]; !ok {
			t.Fatalf("tool names = %v, want %q (display mapping)", toolNames, want)
		}
	}
	if stats.UnknownUpdates != 0 {
		t.Fatalf("unknown updates = %d", stats.UnknownUpdates)
	}
}

func TestReplayPlanFrames(t *testing.T) {
	events, stats := replayUpdateFrames(t, "opencode-2.1.4-plan.ndjson")
	var toolStarts int
	for _, event := range events {
		if event.Type == agent.NodeEventToolExecutionStart {
			toolStarts++
		}
	}
	if toolStarts == 0 {
		t.Fatal("plan frames produced no tool_execution.start")
	}
	if stats.UnknownUpdates != 0 {
		t.Fatalf("unknown updates = %d", stats.UnknownUpdates)
	}
}

func TestReplayTodoFrames(t *testing.T) {
	events, stats := replayUpdateFrames(t, "opencode-2.1.4-todo.ndjson")
	var todoEvents int
	for _, event := range events {
		if event.Type == agent.NodeEventTodoUpdated {
			todoEvents++
			payload := event.Payload.(*agent.TodoUpdatedPayload)
			if len(payload.Items) != 3 {
				t.Fatalf("todo items = %d, want 3", len(payload.Items))
			}
			if payload.Items[0].ID != "todo_0" || payload.Items[1].Status != "in_progress" {
				t.Fatalf("todo payload = %+v", payload.Items)
			}
		}
		if event.Type == agent.NodeEventToolExecutionStart {
			name := event.Payload.(*agent.ToolExecutionStartPayload).Name
			if name == "todowrite" {
				t.Fatal("todowrite leaked into tool card")
			}
		}
	}
	if todoEvents == 0 {
		t.Fatal("no todo.updated replayed")
	}
	if stats.UnknownUpdates != 0 {
		t.Fatalf("unknown updates = %d", stats.UnknownUpdates)
	}
}

func TestReplayFormFrame(t *testing.T) {
	request := replayElicitation(t, "opencode-2.1.4-probe.ndjson")
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("ses_replay", &Stats{}, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := bridge.HandleForm(t.Context(), request)
		done <- resp
	}()

	event := mustEvent(t, events)
	payload := event.Payload.(*agent.QuestionAskedPayload)
	if payload.RequestID == "" || len(payload.Questions) == 0 {
		t.Fatalf("question payload = %+v", payload)
	}
	if err := bridge.WriteAnswer(payload.RequestID, [][]string{{"是"}}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	resp := <-done
	if resp.Accept == nil || resp.Accept.Content["q0"] != "是" {
		t.Fatalf("form response = %+v", resp)
	}
}

func TestReplayPermissionFrame(t *testing.T) {
	request := replayPermission(t, "synthetic-permission.ndjson")
	events := make(chan agent.NodeEvent, 1)
	bridge := NewPermissionBridge("ses_replay", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	done := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := bridge.HandlePermission(t.Context(), request)
		done <- resp
	}()

	event := mustEvent(t, events)
	payload := event.Payload.(*agent.ApprovalRequestedPayload)
	if payload.RequestID == "" || payload.ToolName != "bash" {
		t.Fatalf("approval payload = %+v", payload)
	}
	if err := bridge.WriteDecision(payload.RequestID, agent.ApprovalActionAlways); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	resp := <-done
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "once" {
		t.Fatalf("permission response = %+v, want once (always downgraded)", resp.Outcome)
	}
}

func replayUpdateFrames(t *testing.T, name string) ([]agent.NodeEvent, StatsSnapshot) {
	t.Helper()
	stats := &Stats{}
	translator := NewTranslator(stats)
	var events []agent.NodeEvent
	for _, frame := range loadFrames(t, name) {
		if frame.Dir != "a2c" {
			continue
		}
		var message recordedMessage
		if err := json.Unmarshal([]byte(frame.Line), &message); err != nil {
			t.Fatalf("parse frame: %v", err)
		}
		if message.Method != "session/update" {
			continue
		}
		var notification acpsdk.SessionNotification
		if err := json.Unmarshal(message.Params, &notification); err != nil {
			t.Fatalf("parse session/update: %v", err)
		}
		events = append(events, translator.Handle(notification)...)
	}
	return events, stats.Snapshot()
}

func replayElicitation(t *testing.T, name string) acpsdk.UnstableCreateElicitationRequest {
	t.Helper()
	for _, frame := range loadFrames(t, name) {
		if frame.Dir != "a2c" {
			continue
		}
		var message recordedMessage
		if err := json.Unmarshal([]byte(frame.Line), &message); err != nil {
			t.Fatalf("parse frame: %v", err)
		}
		if message.Method != string(acpsdk.ClientMethodElicitationCreate) {
			continue
		}
		var request acpsdk.UnstableCreateElicitationRequest
		if err := json.Unmarshal(message.Params, &request); err != nil {
			t.Fatalf("parse elicitation/create: %v", err)
		}
		return request
	}
	t.Fatal("elicitation/create frame not found")
	return acpsdk.UnstableCreateElicitationRequest{}
}

func replayPermission(t *testing.T, name string) acpsdk.RequestPermissionRequest {
	t.Helper()
	for _, frame := range loadFrames(t, name) {
		if frame.Dir != "a2c" {
			continue
		}
		var message recordedMessage
		if err := json.Unmarshal([]byte(frame.Line), &message); err != nil {
			t.Fatalf("parse frame: %v", err)
		}
		if message.Method != string(acpsdk.ClientMethodSessionRequestPermission) {
			continue
		}
		var request acpsdk.RequestPermissionRequest
		if err := json.Unmarshal(message.Params, &request); err != nil {
			t.Fatalf("parse permission: %v", err)
		}
		return request
	}
	t.Fatal("session/request_permission frame not found")
	return acpsdk.RequestPermissionRequest{}
}

func loadFrames(t *testing.T, name string) []recordedFrame {
	t.Helper()
	path := filepath.Join("testdata", "acp", name)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open frames %s: %v", path, err)
	}
	defer file.Close()

	var frames []recordedFrame
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame recordedFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("parse frame line: %v", err)
		}
		frames = append(frames, frame)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan frames: %v", err)
	}
	return frames
}
