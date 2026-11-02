package acp

import (
	"context"
	"errors"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Lework/backend/agent"
)

func TestFormBridgeBuildsQuestionsInNumericOrder(t *testing.T) {
	stats := &Stats{}
	events := make(chan agent.NodeEvent, 2)
	bridge := NewFormBridge("s1", stats, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	form := questionForm("Questions", map[string]any{
		"q10":       arrayQuestion("多选", "选多个"),
		"q0":        singleQuestion("继续确认", "是否继续？", "是", "否"),
		"q0_custom": map[string]any{"type": "string", "title": "自定义"},
		"q2":        map[string]any{"type": "string"},
	})

	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := bridge.HandleForm(context.Background(), form)
		done <- resp
	}()

	event := mustEvent(t, events)
	if event.Type != agent.NodeEventQuestionAsked {
		t.Fatalf("event type = %s", event.Type)
	}
	payload := event.Payload.(*agent.QuestionAskedPayload)
	if payload.RequestID == "" || payload.SessionID != "s1" || payload.ToolCallID != "" {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Questions) != 3 {
		t.Fatalf("questions = %d, want 3", len(payload.Questions))
	}
	// 数字序：q0, q2, q10
	if payload.Questions[0].Header != "继续确认" || payload.Questions[0].Question != "是否继续？" {
		t.Fatalf("q0 = %+v", payload.Questions[0])
	}
	if !payload.Questions[0].Custom || len(payload.Questions[0].Options) != 2 {
		t.Fatalf("q0 custom/options = %+v", payload.Questions[0])
	}
	if payload.Questions[1].Header != "q2" || payload.Questions[1].Question != "Questions" {
		t.Fatalf("q2 fallbacks = %+v", payload.Questions[1])
	}
	if !payload.Questions[2].MultiSelect {
		t.Fatalf("q10 should be multi-select: %+v", payload.Questions[2])
	}

	if err := bridge.WriteAnswer(payload.RequestID, [][]string{
		{"自定义文本"},
		{""},
		{"A", "B"},
	}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	select {
	case resp := <-done:
		if resp.Accept == nil {
			t.Fatalf("response = %+v, want accept", resp)
		}
		content := resp.Accept.Content
		if content["q0"] != "自定义文本" || content["q0_custom"] != "自定义文本" {
			t.Fatalf("q0 content = %v", content)
		}
		if content["q2"] != "" {
			t.Fatalf("q2 content = %v, want empty", content["q2"])
		}
		values, ok := content["q10"].([]string)
		if !ok || len(values) != 2 {
			t.Fatalf("q10 content = %v", content["q10"])
		}
	case <-time.After(time.Second):
		t.Fatal("form handler did not return")
	}
	if bridge.Pending() != 0 {
		t.Fatalf("pending = %d", bridge.Pending())
	}
}

func TestFormBridgeCustomWithSelectedOption(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("s1", nil, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	form := questionForm("Questions", map[string]any{
		"q0":        singleQuestion("确认", "是否继续？", "是", "否"),
		"q0_custom": map[string]any{"type": "string"},
	})

	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := bridge.HandleForm(context.Background(), form)
		done <- resp
	}()
	event := mustEvent(t, events)
	requestID := event.Payload.(*agent.QuestionAskedPayload).RequestID
	if err := bridge.WriteAnswer(requestID, [][]string{{"是", "补充说明"}}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	resp := <-done
	if resp.Accept.Content["q0"] != "是" {
		t.Fatalf("q0 = %v", resp.Accept.Content["q0"])
	}
	if resp.Accept.Content["q0_custom"] != "补充说明" {
		t.Fatalf("q0_custom = %v", resp.Accept.Content["q0_custom"])
	}
}

func TestFormBridgeDeclinesNonQuestionForm(t *testing.T) {
	stats := &Stats{}
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("s1", stats, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	resp, err := bridge.HandleForm(context.Background(), questionForm("Login", map[string]any{
		"username": map[string]any{"type": "string"},
	}))
	if err != nil {
		t.Fatalf("handle form: %v", err)
	}
	if resp.Decline == nil || resp.Accept != nil {
		t.Fatalf("response = %+v, want decline", resp)
	}
	if got := stats.Snapshot().FormDeclines; got != 1 {
		t.Fatalf("form declines = %d, want 1", got)
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected event: %s", event.Type)
	default:
	}
}

func TestFormBridgePendingQuestionToolCallOverridesFilter(t *testing.T) {
	stats := &Stats{}
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("s1", stats, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	bridge.SetPendingQuestionToolCall("c9")

	form := questionForm("Questions", map[string]any{
		"q0":    singleQuestion("确认", "继续？", "是", "否"),
		"extra": map[string]any{"type": "string"},
	})
	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := bridge.HandleForm(context.Background(), form)
		done <- resp
	}()
	event := mustEvent(t, events)
	payload := event.Payload.(*agent.QuestionAskedPayload)
	if payload.ToolCallID != "c9" {
		t.Fatalf("toolCallId = %q, want c9", payload.ToolCallID)
	}
	if err := bridge.WriteAnswer(payload.RequestID, [][]string{{"是"}}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	<-done
}

func TestFormBridgeContextCancelled(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("s1", nil, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	form := questionForm("Questions", map[string]any{"q0": singleQuestion("确认", "继续？", "是", "否")})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := bridge.HandleForm(ctx, form)
		done <- resp
	}()
	mustEvent(t, events)
	cancel()

	select {
	case resp := <-done:
		if resp.Cancel == nil {
			t.Fatalf("response = %+v, want cancel", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled form did not return")
	}
	if bridge.Pending() != 0 {
		t.Fatalf("pending = %d after cancel", bridge.Pending())
	}
}

func TestFormBridgeUnknownAnswerID(t *testing.T) {
	bridge := NewFormBridge("s1", nil, nil)
	err := bridge.WriteAnswer("missing", [][]string{{"x"}})
	if !errors.Is(err, ErrUnknownRequestID) {
		t.Fatalf("error = %v, want ErrUnknownRequestID", err)
	}
}

func TestClientDelegatesToFormBridge(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewFormBridge("s1", nil, func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	client := NewClient()
	client.SetFormBridge(bridge)

	form := questionForm("Questions", map[string]any{"q0": singleQuestion("确认", "继续？", "是", "否")})
	done := make(chan acpsdk.UnstableCreateElicitationResponse, 1)
	go func() {
		resp, _ := client.UnstableCreateElicitation(context.Background(), form)
		done <- resp
	}()
	event := mustEvent(t, events)
	requestID := event.Payload.(*agent.QuestionAskedPayload).RequestID
	if err := bridge.WriteAnswer(requestID, [][]string{{"否"}}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	resp := <-done
	if resp.Accept == nil || resp.Accept.Content["q0"] != "否" {
		t.Fatalf("response = %+v", resp.Accept)
	}
}

func questionForm(message string, properties map[string]any) acpsdk.UnstableCreateElicitationRequest {
	return acpsdk.UnstableCreateElicitationRequest{
		Form: &acpsdk.UnstableCreateElicitationForm{
			Mode:    "form",
			Message: message,
			RequestedSchema: acpsdk.UnstableElicitationSchema{
				Properties: properties,
			},
		},
	}
}

func singleQuestion(title, description string, labels ...string) map[string]any {
	oneOf := make([]any, 0, len(labels))
	for _, label := range labels {
		oneOf = append(oneOf, map[string]any{"const": label, "title": label})
	}
	return map[string]any{
		"type":        "string",
		"title":       title,
		"description": description,
		"oneOf":       oneOf,
	}
}

func arrayQuestion(title, description string) map[string]any {
	question := singleQuestion(title, description, "A", "B")
	question["type"] = "array"
	return question
}
