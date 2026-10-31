package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Lework/backend/agent"
)

func TestPermissionBridgeApproveMapsToOnce(t *testing.T) {
	events := make(chan agent.NodeEvent, 4)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	type result struct {
		resp acpsdk.RequestPermissionResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := bridge.HandlePermission(context.Background(), permissionRequest())
		done <- result{resp: resp, err: err}
	}()

	event := mustEvent(t, events)
	if event.Type != agent.NodeEventApprovalRequested {
		t.Fatalf("event type = %s", event.Type)
	}
	payload := event.Payload.(*agent.ApprovalRequestedPayload)
	if payload.RequestID == "" || payload.ToolCallID != "c1" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.ToolName != "bash" || payload.Description != "ls -la" {
		t.Fatalf("tool name/description = %q/%q", payload.ToolName, payload.Description)
	}
	if string(payload.Arguments) != `{"command":"ls -la"}` {
		t.Fatalf("arguments = %s", payload.Arguments)
	}
	if payload.Metadata["engine"] != "opencode" {
		t.Fatalf("metadata = %v", payload.Metadata)
	}

	if err := bridge.WriteDecision(payload.RequestID, agent.ApprovalActionApprove); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("handle permission: %v", res.err)
		}
		assertSelected(t, res.resp, "once")
	case <-time.After(time.Second):
		t.Fatal("permission handler did not return")
	}
	if bridge.Pending() != 0 {
		t.Fatalf("pending = %d, want 0", bridge.Pending())
	}
}

func TestPermissionBridgeActions(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{agent.ApprovalActionApprove, "once"},
		{agent.ApprovalActionAlways, "once"}, // always 在 wire 降级为 once
		{agent.ApprovalActionDeny, "reject"},
		{"unexpected", "reject"}, // 未知动作按拒绝处理
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			events := make(chan agent.NodeEvent, 1)
			bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
				events <- event
				return nil
			})
			done := make(chan acpsdk.RequestPermissionResponse, 1)
			go func() {
				resp, _ := bridge.HandlePermission(context.Background(), permissionRequest())
				done <- resp
			}()
			event := mustEvent(t, events)
			requestID := event.Payload.(*agent.ApprovalRequestedPayload).RequestID
			if err := bridge.WriteDecision(requestID, tt.action); err != nil {
				t.Fatalf("write decision: %v", err)
			}
			select {
			case resp := <-done:
				assertSelected(t, resp, tt.want)
			case <-time.After(time.Second):
				t.Fatal("permission handler did not return")
			}
		})
	}
}

func TestPermissionBridgeAlwaysRepeatsWithinExecution(t *testing.T) {
	events := make(chan agent.NodeEvent, 4)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	answer := func() acpsdk.RequestPermissionResponse {
		done := make(chan acpsdk.RequestPermissionResponse, 1)
		go func() {
			resp, _ := bridge.HandlePermission(context.Background(), permissionRequest())
			done <- resp
		}()
		event := mustEvent(t, events)
		requestID := event.Payload.(*agent.ApprovalRequestedPayload).RequestID
		if err := bridge.WriteDecision(requestID, agent.ApprovalActionAlways); err != nil {
			t.Fatalf("write decision: %v", err)
		}
		select {
		case resp := <-done:
			return resp
		case <-time.After(time.Second):
			t.Fatal("permission handler did not return")
			return acpsdk.RequestPermissionResponse{}
		}
	}

	assertSelected(t, answer(), "once")

	// 同一 execution 内、同工具+同命令再次请求：直接放行且不再发审批事件。
	done := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := bridge.HandlePermission(context.Background(), permissionRequest())
		done <- resp
	}()
	select {
	case resp := <-done:
		assertSelected(t, resp, "once")
	case <-time.After(time.Second):
		t.Fatal("remembered always rule did not auto-approve")
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected second approval event: %s", event.Type)
	default:
	}
	if bridge.Pending() != 0 {
		t.Fatalf("pending = %d, want 0", bridge.Pending())
	}

	// 资源不同（换命令）不能命中记忆，必须重新询问。
	other := permissionRequest()
	otherTitle := "pwd"
	other.ToolCall.Title = &otherTitle
	other.ToolCall.RawInput = map[string]any{"command": "pwd"}
	otherDone := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := bridge.HandlePermission(context.Background(), other)
		otherDone <- resp
	}()
	otherEvent := mustEvent(t, events)
	if err := bridge.WriteDecision(otherEvent.Payload.(*agent.ApprovalRequestedPayload).RequestID,
		agent.ApprovalActionDeny); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	select {
	case resp := <-otherDone:
		assertSelected(t, resp, "reject")
	case <-time.After(time.Second):
		t.Fatal("permission handler did not return")
	}
}

func TestPermissionBridgeAlwaysDoesNotLeakAcrossExecutions(t *testing.T) {
	events := make(chan agent.NodeEvent, 2)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	done := make(chan struct{}, 1)
	go func() {
		_, _ = bridge.HandlePermission(context.Background(), permissionRequest())
		done <- struct{}{}
	}()
	event := mustEvent(t, events)
	if err := bridge.WriteDecision(event.Payload.(*agent.ApprovalRequestedPayload).RequestID,
		agent.ApprovalActionAlways); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	<-done

	// 下一个任务/执行是新的 bridge：没有 saved 规则，必须重新询问。
	fresh := NewPermissionBridge("s2", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	freshDone := make(chan struct{}, 1)
	go func() {
		_, _ = fresh.HandlePermission(context.Background(), permissionRequest())
		freshDone <- struct{}{}
	}()
	freshEvent := mustEvent(t, events)
	if freshEvent.Type != agent.NodeEventApprovalRequested {
		t.Fatalf("event type = %s, want approval.requested", freshEvent.Type)
	}
	if err := fresh.WriteDecision(freshEvent.Payload.(*agent.ApprovalRequestedPayload).RequestID,
		agent.ApprovalActionApprove); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	<-freshDone
}

func TestPermissionBridgeContextCancelled(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := bridge.HandlePermission(ctx, permissionRequest())
		done <- resp
	}()
	mustEvent(t, events)
	cancel()

	select {
	case resp := <-done:
		if resp.Outcome.Cancelled == nil {
			t.Fatalf("outcome = %+v, want cancelled", resp.Outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled permission handler did not return")
	}
	if bridge.Pending() != 0 {
		t.Fatalf("pending = %d after cancel, want 0", bridge.Pending())
	}
}

func TestPermissionBridgeUnknownRequestID(t *testing.T) {
	bridge := NewPermissionBridge("s1", nil)
	err := bridge.WriteDecision("missing", agent.ApprovalActionApprove)
	if !errors.Is(err, ErrUnknownRequestID) {
		t.Fatalf("error = %v, want ErrUnknownRequestID", err)
	}
}

func TestPermissionBridgeRequestIDsUnique(t *testing.T) {
	events := make(chan agent.NodeEvent, 64)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})

	seen := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		done := make(chan struct{}, 1)
		go func() {
			_, _ = bridge.HandlePermission(context.Background(), permissionRequest())
			done <- struct{}{}
		}()
		event := mustEvent(t, events)
		requestID := event.Payload.(*agent.ApprovalRequestedPayload).RequestID
		if requestID == "" {
			t.Fatal("empty request id")
		}
		if _, exists := seen[requestID]; exists {
			t.Fatalf("duplicate request id %q", requestID)
		}
		seen[requestID] = struct{}{}
		if err := bridge.WriteDecision(requestID, agent.ApprovalActionApprove); err != nil {
			t.Fatalf("write decision: %v", err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("handler did not return")
		}
	}
}

func TestClientDelegatesToPermissionBridge(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	client := NewClient()
	client.SetPermissionBridge(bridge)

	done := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := client.RequestPermission(context.Background(), permissionRequest())
		done <- resp
	}()
	event := mustEvent(t, events)
	requestID := event.Payload.(*agent.ApprovalRequestedPayload).RequestID
	if err := bridge.WriteDecision(requestID, agent.ApprovalActionApprove); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	select {
	case resp := <-done:
		assertSelected(t, resp, "once")
	case <-time.After(time.Second):
		t.Fatal("client permission handler did not return")
	}
}

func permissionRequest() acpsdk.RequestPermissionRequest {
	kind := acpsdk.ToolKindExecute
	title := "ls -la"
	return acpsdk.RequestPermissionRequest{
		SessionId: "s1",
		ToolCall: acpsdk.ToolCallUpdate{
			ToolCallId: "c1",
			Kind:       &kind,
			Title:      &title,
			RawInput:   map[string]any{"command": "ls -la"},
		},
		Options: []acpsdk.PermissionOption{
			{Kind: acpsdk.PermissionOptionKindAllowOnce, OptionId: "once", Name: "Allow once"},
			{Kind: acpsdk.PermissionOptionKindAllowAlways, OptionId: "always", Name: "Always"},
			{Kind: acpsdk.PermissionOptionKindRejectOnce, OptionId: "reject", Name: "Reject"},
		},
	}
}

func mustEvent(t *testing.T, events <-chan agent.NodeEvent) agent.NodeEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval event")
		return agent.NodeEvent{}
	}
}

func assertSelected(t *testing.T, resp acpsdk.RequestPermissionResponse, wantOption string) {
	t.Helper()
	if resp.Outcome.Selected == nil {
		t.Fatalf("outcome = %+v, want selected %q", resp.Outcome, wantOption)
	}
	if got := string(resp.Outcome.Selected.OptionId); got != wantOption {
		t.Fatalf("option = %q, want %q", got, wantOption)
	}
	if strings.TrimSpace(string(resp.Outcome.Selected.OptionId)) == "" {
		t.Fatal("empty option id")
	}
}

func TestPermissionBridgeUsesKindForToolName(t *testing.T) {
	events := make(chan agent.NodeEvent, 1)
	bridge := NewPermissionBridge("s1", func(event agent.NodeEvent) error {
		events <- event
		return nil
	})
	kind := acpsdk.ToolKindEdit
	title := "/workspace/notes.md"
	request := acpsdk.RequestPermissionRequest{
		SessionId: "s1",
		ToolCall: acpsdk.ToolCallUpdate{
			ToolCallId: "c-edit",
			Kind:       &kind,
			Title:      &title,
			RawInput:   map[string]any{"path": "/workspace/notes.md"},
		},
		Options: permissionRequest().Options,
	}
	done := make(chan acpsdk.RequestPermissionResponse, 1)
	go func() {
		resp, _ := bridge.HandlePermission(context.Background(), request)
		done <- resp
	}()
	event := mustEvent(t, events)
	payload := event.Payload.(*agent.ApprovalRequestedPayload)
	if payload.ToolName != "write" {
		t.Fatalf("tool name = %q, want write (kind over title)", payload.ToolName)
	}
	if payload.Description != title {
		t.Fatalf("description = %q, want path %q", payload.Description, title)
	}
	if err := bridge.WriteDecision(payload.RequestID, agent.ApprovalActionApprove); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	<-done
}
