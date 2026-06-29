package events

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestMessageDeltaPayloadIncludesMessageID(t *testing.T) {
	messageID := uuid.NewString()
	event := NewMessageDelta(messageID, "hello")
	payload, err := DecodePayload[MessageDeltaPayload](event)
	if err != nil {
		t.Fatalf("decode message payload: %v", err)
	}
	if payload.MessageID != messageID || payload.Content != "hello" || payload.Role != "assistant" {
		t.Fatalf("unexpected message payload: %#v", payload)
	}
}

func TestRunEventRecordJSONUsesTimestampAndOmitsRunContext(t *testing.T) {
	record := RunEventRecord{
		Seq:       1,
		LastSeq:   2,
		Type:      EventMessageDelta,
		Timestamp: 1779243000000,
	}

	body, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got["timestamp"] != float64(1779243000000) {
		t.Fatalf("expected numeric timestamp, got %s", body)
	}
	if _, ok := got["created_at"]; ok {
		t.Fatalf("record should not include created_at: %s", body)
	}
	if _, ok := got["id"]; ok {
		t.Fatalf("record should not include id: %s", body)
	}
	if _, ok := got["run_id"]; ok {
		t.Fatalf("record should not include run_id: %s", body)
	}
	if _, ok := got["trace_id"]; ok {
		t.Fatalf("record should not include trace_id: %s", body)
	}
}
