package skill

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

type fakeReplyPublisher struct {
	published []struct {
		subject string
		data    []byte
	}
}

func (f *fakeReplyPublisher) Publish(subject string, data []byte) error {
	f.published = append(f.published, struct {
		subject string
		data    []byte
	}{subject, data})
	return nil
}

func (f *fakeReplyPublisher) lastResult() *messaging.WorkerCommandResult {
	if len(f.published) == 0 {
		return nil
	}
	var result messaging.WorkerCommandResult
	json.Unmarshal(f.published[len(f.published)-1].data, &result)
	return &result
}

func TestNewSkillHandlerRequiresPublisher(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil publisher")
	}
}

func TestHandleSkillCommandUnknownAction(t *testing.T) {
	pub := &fakeReplyPublisher{}
	h, err := New(pub)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload := messaging.SkillCommandPayload{Action: "unknown_action"}
	payloadBytes, _ := json.Marshal(payload)
	body := messaging.WorkerCommandBody{
		CommandType: messaging.CommandTypeSkill,
		Payload:     payloadBytes,
	}

	cmd := messaging.WorkerCommand{Body: body}
	msg := &nats.Msg{Reply: "inbox_1"}

	err = h.HandleSkillCommand(context.Background(), cmd, msg)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}

	result := pub.lastResult()
	if result == nil {
		t.Fatal("expected reply to be published")
	}
	if result.Success {
		t.Fatal("expected error result for unknown action")
	}
}

func TestIsZipContent(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		contentType string
		want        bool
	}{
		{"empty", []byte{}, "", false},
		{"short", []byte{0x50}, "", false},
		{"valid zip", []byte{0x50, 0x4B, 0x03, 0x04}, "", true},
		{"valid zip with content type", []byte{0x50, 0x4B, 0x03, 0x04}, "application/zip", true},
		{"valid zip octet-stream", []byte{0x50, 0x4B, 0x03, 0x04}, "application/octet-stream", true},
		{"not zip", []byte{0x00, 0x00, 0x00, 0x00}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isZipContent(tt.data, tt.contentType)
			if got != tt.want {
				t.Errorf("isZipContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReplySuccess(t *testing.T) {
	pub := &fakeReplyPublisher{}
	h, _ := New(pub)

	h.replySuccess("inbox_ok", "install", "skill \"test\" installed")
	result := pub.lastResult()
	if result == nil {
		t.Fatal("expected reply")
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Action != "install" {
		t.Errorf("Action = %q, want install", result.Action)
	}
}

func TestReplyError(t *testing.T) {
	pub := &fakeReplyPublisher{}
	h, _ := New(pub)

	h.replyError("inbox_err", "boom", nil)
	result := pub.lastResult()
	if result == nil {
		t.Fatal("expected reply")
	}
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}
