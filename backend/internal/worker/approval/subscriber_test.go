package approval

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/engines"
)

func TestHandleMessageResolvesQuestionAnswer(t *testing.T) {
	oldRouter := engines.DefaultInteractionRouter
	engines.DefaultInteractionRouter = engines.NewInteractionRouter()
	defer func() { engines.DefaultInteractionRouter = oldRouter }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resultCh := make(chan *engines.QuestionAnswer, 1)
	errCh := make(chan error, 1)
	go func() {
		answer, err := engines.DefaultInteractionRouter.RequestAnswer(ctx, &engines.QuestionRequest{
			RequestID: "que_123",
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- answer
	}()

	msg, err := json.Marshal(struct {
		InteractionType string     `json:"interaction_type"`
		SessionID       string     `json:"session_id"`
		RequestID       string     `json:"request_id"`
		Answers         [][]string `json:"answers"`
	}{
		InteractionType: "question",
		SessionID:       "sess_123",
		RequestID:       "que_123",
		Answers:         [][]string{{"Use latest endpoint"}},
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	sub := &Subscriber{}
	answer := resolveQuestionWithRetry(t, sub, msg, resultCh, errCh)
	if answer.RequestID != "que_123" {
		t.Fatalf("request id = %q, want %q", answer.RequestID, "que_123")
	}
	if len(answer.Answers) != 1 || len(answer.Answers[0]) != 1 || answer.Answers[0][0] != "Use latest endpoint" {
		t.Fatalf("answers = %#v", answer.Answers)
	}
}

func TestHandleMessageResolvesApprovalDecision(t *testing.T) {
	oldRouter := engines.DefaultInteractionRouter
	engines.DefaultInteractionRouter = engines.NewInteractionRouter()
	defer func() { engines.DefaultInteractionRouter = oldRouter }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resultCh := make(chan *engines.ApprovalDecision, 1)
	errCh := make(chan error, 1)
	go func() {
		decision, err := engines.DefaultInteractionRouter.RequestApproval(ctx, &engines.ApprovalRequest{
			RequestID: "per_123",
			ToolName:  "bash",
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- decision
	}()

	msg, err := json.Marshal(struct {
		SessionID string `json:"session_id"`
		RequestID string `json:"request_id"`
		Action    string `json:"action"`
		Reason    string `json:"reason"`
	}{
		SessionID: "sess_123",
		RequestID: "per_123",
		Action:    engines.ApprovalActionApprove,
		Reason:    "ok",
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	sub := &Subscriber{}
	decision := resolveApprovalWithRetry(t, sub, msg, resultCh, errCh)
	if decision.RequestID != "per_123" {
		t.Fatalf("request id = %q, want %q", decision.RequestID, "per_123")
	}
	if decision.Action != engines.ApprovalActionApprove {
		t.Fatalf("action = %q, want %q", decision.Action, engines.ApprovalActionApprove)
	}
	if decision.Reason != "ok" {
		t.Fatalf("reason = %q, want %q", decision.Reason, "ok")
	}
}

func resolveQuestionWithRetry(t *testing.T, sub *Subscriber, msg []byte, resultCh <-chan *engines.QuestionAnswer, errCh <-chan error) *engines.QuestionAnswer {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(time.Second)
	for {
		select {
		case answer := <-resultCh:
			return answer
		case err := <-errCh:
			t.Fatalf("request answer: %v", err)
		case <-ticker.C:
			sub.handleMessage(msg)
		case <-timeout:
			t.Fatal("timed out waiting for question answer")
		}
	}
}

func resolveApprovalWithRetry(t *testing.T, sub *Subscriber, msg []byte, resultCh <-chan *engines.ApprovalDecision, errCh <-chan error) *engines.ApprovalDecision {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(time.Second)
	for {
		select {
		case decision := <-resultCh:
			return decision
		case err := <-errCh:
			t.Fatalf("request approval: %v", err)
		case <-ticker.C:
			sub.handleMessage(msg)
		case <-timeout:
			t.Fatal("timed out waiting for approval decision")
		}
	}
}
