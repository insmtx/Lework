package protocol

import (
	"encoding/json"
	"testing"
)

func TestFrameHelloRoundTrip(t *testing.T) {
	in := Frame{
		Type:   FrameHello,
		ID:     "1",
		Params: json.RawMessage(`{"agent_version":"0.1.0"}`),
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var out Frame
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Type != FrameHello || out.ID != "1" {
		t.Fatalf("unexpected frame: %+v", out)
	}
}

func TestFrameValidateRejectsUnknownType(t *testing.T) {
	if err := (Frame{Type: "nope"}).Validate(); err == nil {
		t.Fatal("expected error for unknown frame type")
	}
}

func TestFrameValidateRequiresRequestID(t *testing.T) {
	if err := (Frame{Type: FrameRequest, Method: "session.list"}).Validate(); err == nil {
		t.Fatal("expected error for missing request id")
	}
}

func TestFrameValidateRules(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr bool
	}{
		{name: "hello", frame: Frame{Type: FrameHello}},
		{name: "welcome", frame: Frame{Type: FrameWelcome}},
		{name: "heartbeat", frame: Frame{Type: FrameHeartbeat}},
		{name: "request missing method", frame: Frame{Type: FrameRequest, ID: "1"}, wantErr: true},
		{name: "request", frame: Frame{Type: FrameRequest, ID: "1", Method: "session.list"}},
		{name: "response missing id", frame: Frame{Type: FrameResponse, Result: json.RawMessage(`{}`)}, wantErr: true},
		{name: "response missing payload", frame: Frame{Type: FrameResponse, ID: "1"}, wantErr: true},
		{name: "response with result", frame: Frame{Type: FrameResponse, ID: "1", Result: json.RawMessage(`{}`)}},
		{name: "response with error", frame: Frame{Type: FrameResponse, ID: "1", Error: &FrameError{Code: "bad"}}},
		{name: "event missing event", frame: Frame{Type: FrameEvent}, wantErr: true},
		{name: "event missing type", frame: Frame{Type: FrameEvent, Event: &Event{}}, wantErr: true},
		{name: "event", frame: Frame{Type: FrameEvent, Event: &Event{Type: "session.updated"}}},
		{name: "cancel missing id", frame: Frame{Type: FrameCancel}, wantErr: true},
		{name: "cancel", frame: Frame{Type: FrameCancel, ID: "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFrameValidateUnknownTypeMessage(t *testing.T) {
	err := (Frame{Type: "nope"}).Validate()
	if err == nil {
		t.Fatal("expected error for unknown frame type")
	}
	if got, want := err.Error(), "unknown frame type: nope"; got != want {
		t.Fatalf("unexpected error: got %q, want %q", got, want)
	}
}
