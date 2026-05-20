package runnable

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
)

func TestHandleSessionCompletedMessageUsesRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	createdAt := time.Now().UTC()
	streamMsg := events.MessageStreamMessage{
		CreatedAt: createdAt,
		Route: events.RouteContext{
			SessionID: "sess_test",
		},
		Body: events.StreamBody{
			Seq:   9,
			Event: events.StreamEventRunCompleted,
			RunCompleted: &events.RunCompletedPayload{
				Result: events.RunResultPayload{Message: "done"},
				Usage: &events.UsagePayload{
					InputTokens:  11,
					OutputTokens: 22,
					TotalTokens:  33,
				},
				Events: []events.RunEventRecord{
					{
						Seq:       1,
						Type:      events.EventMessageDelta,
						Timestamp: 1779243000000,
					},
				},
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.completeReq == nil {
		t.Fatal("expected CompleteSessionMessage to be called")
	}
	if service.completeReq.SessionID != "sess_test" || service.completeReq.Content != "done" || service.completeReq.Seq != 9 {
		t.Fatalf("unexpected complete request: %#v", service.completeReq)
	}
	if len(service.completeReq.Chunks) != 1 {
		t.Fatalf("expected one chunk, got %#v", service.completeReq.Chunks)
	}
	if service.completeReq.Chunks[0].Seq != 1 || service.completeReq.Chunks[0].Type != string(events.EventMessageDelta) ||
		service.completeReq.Chunks[0].Timestamp != 1779243000000 {
		t.Fatalf("unexpected chunk: %#v", service.completeReq.Chunks[0])
	}
	if service.completeReq.Usage == nil || service.completeReq.Usage.TotalTokens != 33 {
		t.Fatalf("expected usage to be forwarded, got %#v", service.completeReq.Usage)
	}
}

func TestHandleSessionCompletedMessageRequiresRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	streamMsg := events.MessageStreamMessage{
		CreatedAt: time.Now().UTC(),
		Route:     events.RouteContext{SessionID: "sess_test"},
		Body: events.StreamBody{
			Seq:   9,
			Event: events.StreamEventRunCompleted,
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.completeReq != nil {
		t.Fatalf("expected no complete request, got %#v", service.completeReq)
	}
}

func TestHandleSessionCompletedMessageUsesFailedRunCompletedPayload(t *testing.T) {
	service := &recordingSessionService{}
	createdAt := time.Now().UTC()
	streamMsg := events.MessageStreamMessage{
		CreatedAt: createdAt,
		Route: events.RouteContext{
			SessionID: "sess_test",
		},
		Body: events.StreamBody{
			Seq:   10,
			Event: events.StreamEventRunFailed,
			RunCompleted: &events.RunCompletedPayload{
				Status: "failed",
				Result: events.RunResultPayload{
					Message: "runtime unavailable",
				},
			},
			Error: &events.StreamError{
				Code:    "runtime_error",
				Message: "runtime unavailable",
			},
		},
	}
	body, err := json.Marshal(streamMsg)
	if err != nil {
		t.Fatalf("marshal stream message: %v", err)
	}

	handleSessionCompletedMessage(context.Background(), service, &nats.Msg{Data: body})

	if service.failedReq == nil {
		t.Fatal("expected FailedSessionMessage to be called")
	}
	if service.failedReq.SessionID != "sess_test" || service.failedReq.ErrorMsg != "runtime unavailable" || service.failedReq.Seq != 10 {
		t.Fatalf("unexpected failed request: %#v", service.failedReq)
	}
	if service.failedReq.ErrorCode != "runtime_error" {
		t.Fatalf("expected error code to be forwarded, got %q", service.failedReq.ErrorCode)
	}
}

type recordingSessionService struct {
	completeReq *contract.CompleteSessionMessageRequest
	failedReq   *contract.FailedSessionMessageRequest
}

func (s *recordingSessionService) CreateSession(context.Context, *contract.CreateSessionRequest) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) GetSession(context.Context, string) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) UpdateSession(context.Context, string, *contract.UpdateSessionRequest) (*contract.Session, error) {
	return nil, nil
}
func (s *recordingSessionService) DeleteSession(context.Context, string) error { return nil }
func (s *recordingSessionService) ListSessions(context.Context, *contract.ListSessionsRequest) (*contract.SessionList, error) {
	return nil, nil
}
func (s *recordingSessionService) ActivateSession(context.Context, string) error { return nil }
func (s *recordingSessionService) PauseSession(context.Context, string) error    { return nil }
func (s *recordingSessionService) EndSession(context.Context, string) error      { return nil }
func (s *recordingSessionService) ResumeSession(context.Context, string) error   { return nil }
func (s *recordingSessionService) AddMessage(context.Context, string, *contract.AddMessageRequest) (*contract.SessionMessage, error) {
	return nil, nil
}
func (s *recordingSessionService) GetSessionMessages(context.Context, string, int, int) (*contract.MessageList, error) {
	return nil, nil
}
func (s *recordingSessionService) DeleteMessage(context.Context, uint) error          { return nil }
func (s *recordingSessionService) ClearSessionMessages(context.Context, string) error { return nil }
func (s *recordingSessionService) StreamSessionEvents(context.Context, string, int64, events.Sink) error {
	return nil
}
func (s *recordingSessionService) CompleteSessionMessage(_ context.Context, req *contract.CompleteSessionMessageRequest) error {
	s.completeReq = req
	return nil
}
func (s *recordingSessionService) FailedSessionMessage(_ context.Context, req *contract.FailedSessionMessageRequest) error {
	s.failedReq = req
	return nil
}
func (s *recordingSessionService) HandleSessionTitleRequest(context.Context, string) error {
	return nil
}

var _ contract.SessionService = (*recordingSessionService)(nil)
