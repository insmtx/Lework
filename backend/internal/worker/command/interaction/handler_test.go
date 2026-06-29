package interaction

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type fakeResolver struct {
	approvals   []approvalCall
	questions   []questionCall
	approvalErr error
	questionErr error
}

type approvalCall struct {
	requestID, action, reason string
}

type questionCall struct {
	requestID string
	answers   [][]string
}

func (f *fakeResolver) ResolveApproval(requestID, action, reason string) error {
	f.approvals = append(f.approvals, approvalCall{requestID, action, reason})
	return f.approvalErr
}

func (f *fakeResolver) ResolveQuestion(requestID string, answers [][]string) error {
	f.questions = append(f.questions, questionCall{requestID, answers})
	return f.questionErr
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestHandleApprovalResolve(t *testing.T) {
	resolver := &fakeResolver{}
	h := New(resolver)

	payload := messaging.ApprovalResolveCommandPayload{
		Action: "approve",
		Reason: "looks good",
	}
	body := messaging.WorkerCommandBody{
		CommandType: messaging.CommandTypeApprovalResolve,
		Payload:     mustMarshal(t, payload),
	}

	cmd := messaging.WorkerCommand{
		Body:  body,
		Trace: messaging.TraceContext{RequestID: "req_1"},
		Route: messaging.RouteContext{SessionID: "sess_1"},
	}

	err := h.HandleInteractionCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("HandleInteractionCommand error = %v", err)
	}
	if len(resolver.approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(resolver.approvals))
	}
	if resolver.approvals[0].requestID != "req_1" {
		t.Errorf("requestID = %q, want req_1", resolver.approvals[0].requestID)
	}
	if resolver.approvals[0].action != "approve" {
		t.Errorf("action = %q, want approve", resolver.approvals[0].action)
	}
}

func TestHandleQuestionAnswer(t *testing.T) {
	resolver := &fakeResolver{}
	h := New(resolver)

	payload := messaging.QuestionAnswerCommandPayload{
		Answers: [][]string{{"a1"}, {"b1", "b2"}},
	}
	body := messaging.WorkerCommandBody{
		CommandType: messaging.CommandTypeQuestionAnswer,
		Payload:     mustMarshal(t, payload),
	}

	cmd := messaging.WorkerCommand{
		Body:  body,
		Trace: messaging.TraceContext{RequestID: "req_2"},
		Route: messaging.RouteContext{SessionID: "sess_2"},
	}

	err := h.HandleInteractionCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("HandleInteractionCommand error = %v", err)
	}
	if len(resolver.questions) != 1 {
		t.Fatalf("expected 1 question answer, got %d", len(resolver.questions))
	}
	if resolver.questions[0].requestID != "req_2" {
		t.Errorf("requestID = %q, want req_2", resolver.questions[0].requestID)
	}
}

func TestHandleUnknownCommandType(t *testing.T) {
	resolver := &fakeResolver{}
	h := New(resolver)

	body := messaging.WorkerCommandBody{
		CommandType: "unknown.type",
	}

	cmd := messaging.WorkerCommand{
		Body: body,
	}

	err := h.HandleInteractionCommand(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for unknown command type")
	}
}

func TestHandleApprovalResolverError(t *testing.T) {
	resolver := &fakeResolver{approvalErr: fmt.Errorf("no such approval")}
	h := New(resolver)

	payload := messaging.ApprovalResolveCommandPayload{Action: "approve"}
	body := messaging.WorkerCommandBody{
		CommandType: messaging.CommandTypeApprovalResolve,
		Payload:     mustMarshal(t, payload),
	}

	cmd := messaging.WorkerCommand{
		Body:  body,
		Trace: messaging.TraceContext{RequestID: "req_3"},
	}

	err := h.HandleInteractionCommand(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error from resolver")
	}
}
