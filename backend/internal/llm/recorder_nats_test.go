package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/insmtx/Lework/backend/internal/infra/mq"
	"github.com/insmtx/Lework/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

type usagePublishCapture struct {
	subject string
	record  *CallRecord
	body    []byte
	calls   int
}

type usageSubscriberAdapter struct {
	bus interface {
		Subscribe(context.Context, string, string, func(*nats.Msg)) error
	}
}

func (s usageSubscriberAdapter) Subscribe(ctx context.Context, topic, consumer string, handler func([]byte)) error {
	return s.bus.Subscribe(ctx, topic, consumer, func(message *nats.Msg) {
		handler(message.Data)
	})
}

func captureUsagePublisher(capture *usagePublishCapture) *LLMUsagePublisher {
	return NewLLMUsagePublisher(func(_ context.Context, subject string, event any) error {
		capture.calls++
		capture.subject = subject
		record, ok := event.(*CallRecord)
		if !ok {
			return fmt.Errorf("published usage has type %T", event)
		}
		capture.record = record
		body, err := json.Marshal(record)
		capture.body = body
		return err
	})
}

func testUsageRecord(input, output, message string) *CallRecord {
	now := time.Date(2026, time.October, 17, 9, 30, 0, 0, time.UTC)
	return &CallRecord{
		OrgID: 11, ModelID: 22, Provider: "provider", ModelName: "model",
		ModelProvider: "model-provider", EntryProtocol: "openai_chat", IsStream: true,
		InputTokens: 31, OutputTokens: 17, TotalTokens: 48, LatencyMS: 987,
		PromptTokens: 31, CacheHitTokens: 3, CacheMissTokens: 28,
		StatusCode: 200, Success: true, Status: "success", Message: message,
		CallerType: CallerTypeWorker, ReqID: "req-1", TraceID: "trace-2", RetryTimes: 1,
		InputLen: len(input), OutputLen: len(output), ClientIP: "127.0.0.1",
		ProjectID: 33, SessionID: 44, MessageID: 55, AssistantID: 66, Uin: 77,
		Input: input, Output: output, StartedAt: now, FinishedAt: now.Add(time.Second),
	}
}

func publishTestUsage(t *testing.T, input, output, message string) (*CallRecord, *CallRecord, []byte, string, error) {
	t.Helper()
	original := testUsageRecord(input, output, message)
	before := *original
	capture := &usagePublishCapture{}
	recorder := NewRecorderNATS(captureUsagePublisher(capture), original.OrgID)
	err := recorder.RecordCall(context.Background(), original)
	if err == nil && capture.calls != 1 {
		t.Fatalf("expected one publish call, got %d", capture.calls)
	}
	if err == nil && capture.record == original {
		t.Fatal("publisher received the original CallRecord pointer")
	}
	return original, &before, capture.body, capture.subject, err
}

func TestLLMUsagePublisherKeepsSmallRecordUnchanged(t *testing.T) {
	original, before, body, subject, err := publishTestUsage(t, `{"prompt":"hello"}`, `{"answer":"world"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if subject != "org.11.usage.llm" {
		t.Fatalf("unexpected subject %q", subject)
	}
	if len(body) > usageMessageMaxBytes {
		t.Fatalf("payload too large: %d", len(body))
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatal("publishing mutated the original CallRecord")
	}
	var published CallRecord
	if err := json.Unmarshal(body, &published); err != nil {
		t.Fatal(err)
	}
	if published.Input != before.Input || published.Output != before.Output ||
		published.InputTruncated || published.OutputTruncated {
		t.Fatalf("small record was unexpectedly changed: %+v", published)
	}
}

func TestLLMUsagePublisherFillsMissingOriginalLengths(t *testing.T) {
	record := testUsageRecord("request", "response", "")
	record.InputLen, record.OutputLen = 0, 0
	capture := &usagePublishCapture{}
	if err := NewRecorderNATS(captureUsagePublisher(capture), record.OrgID).
		RecordCall(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if capture.record.InputLen != len(record.Input) || capture.record.OutputLen != len(record.Output) {
		t.Fatalf("missing original lengths not filled: input=%d output=%d",
			capture.record.InputLen, capture.record.OutputLen)
	}
	if record.InputLen != 0 || record.OutputLen != 0 {
		t.Fatal("publishing changed original missing lengths")
	}
}

func TestLLMUsagePublisherSanitizesInvalidUTF8InCopy(t *testing.T) {
	input := string([]byte{0xff, 0xfe, 'x'})
	record := testUsageRecord(input, "response", "")
	before := *record
	capture := &usagePublishCapture{}
	if err := NewRecorderNATS(captureUsagePublisher(capture), record.OrgID).
		RecordCall(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(capture.record.Input) || !capture.record.InputTruncated {
		t.Fatalf("invalid UTF-8 was not safely represented: %q", capture.record.Input)
	}
	if capture.record.InputLen != len(input) || !reflect.DeepEqual(record, &before) {
		t.Fatal("UTF-8 normalization lost original byte length or mutated source")
	}
}

func TestLLMUsagePublisherBoundsLargeRequests(t *testing.T) {
	for _, size := range []int{1 << 20, 8 << 20, 20 << 20} {
		t.Run(fmt.Sprintf("%dMiB", size>>20), func(t *testing.T) {
			input := strings.Repeat("x", size)
			original := testUsageRecord(input, "small-response", "")
			before := *original
			capture := &usagePublishCapture{}
			err := NewRecorderNATS(captureUsagePublisher(capture), original.OrgID).
				RecordCall(context.Background(), original)
			if err != nil {
				t.Fatal(err)
			}
			assertCapturedUsage(t, original, &before, capture)
			if capture.record.InputLen != size || !capture.record.InputTruncated {
				t.Fatalf("original input length or truncation flag lost: len=%d truncated=%v",
					capture.record.InputLen, capture.record.InputTruncated)
			}
			if !strings.HasPrefix(capture.record.Input, strings.Repeat("x", 64)) ||
				!strings.HasSuffix(capture.record.Input, usageTruncationMarker) {
				t.Fatal("input preview did not preserve its prefix and truncation marker")
			}
			if capture.record.OutputLen != len("small-response") || capture.record.OutputTruncated {
				t.Fatalf("small output changed unexpectedly: len=%d truncated=%v",
					capture.record.OutputLen, capture.record.OutputTruncated)
			}
		})
	}
}

func TestLLMUsagePublisherBoundsOutputAndErrorMessage(t *testing.T) {
	large := strings.Repeat("响应😀\\\"", 1<<17)
	for _, tc := range []struct {
		name               string
		input, output, msg string
		truncatedInput     bool
		truncatedOutput    bool
		messageTruncated   bool
		failed             bool
	}{
		{name: "response", input: "small", output: large, truncatedOutput: true},
		{name: "upstream-error", input: "small", output: "", msg: large, messageTruncated: true, failed: true},
		{name: "escaped-and-unicode", input: strings.Repeat(`\\\"`, 1<<16), output: strings.Repeat("🧭汉", 1<<16), msg: large,
			truncatedInput: true, truncatedOutput: true},
		{name: "json-control-characters", input: strings.Repeat("\x00", 1<<16),
			output: strings.Repeat("\x01", 1<<16), truncatedInput: true, truncatedOutput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := testUsageRecord(tc.input, tc.output, tc.msg)
			if tc.failed {
				original.Success = false
				original.Status = "error"
				original.StatusCode = 502
			}
			before := *original
			capture := &usagePublishCapture{}
			if err := NewRecorderNATS(captureUsagePublisher(capture), original.OrgID).
				RecordCall(context.Background(), original); err != nil {
				t.Fatal(err)
			}
			assertCapturedUsage(t, original, &before, capture)
			if capture.record.InputTruncated != tc.truncatedInput ||
				capture.record.OutputTruncated != tc.truncatedOutput {
				t.Fatalf("unexpected truncation flags: input=%v output=%v",
					capture.record.InputTruncated, capture.record.OutputTruncated)
			}
			if !utf8.ValidString(capture.record.Input) || !utf8.ValidString(capture.record.Output) ||
				!utf8.ValidString(capture.record.Message) {
				t.Fatal("published strings are not valid UTF-8")
			}
			if tc.messageTruncated && !strings.Contains(capture.record.Message, "truncated") {
				t.Fatal("truncated error message has no visible truncation marker")
			}
		})
	}
}

func TestLLMUsagePublisherRejectsOversizedStructuredMetadata(t *testing.T) {
	record := testUsageRecord("", "", "")
	record.ReqID = strings.Repeat("sensitive", usageMessageMaxBytes)
	before := *record
	capture := &usagePublishCapture{}
	err := NewRecorderNATS(captureUsagePublisher(capture), record.OrgID).
		RecordCall(context.Background(), record)
	if err == nil {
		t.Fatal("expected oversized metadata error")
	}
	if capture.calls != 0 {
		t.Fatalf("oversized record was published %d times", capture.calls)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatal("error leaked metadata content")
	}
	if !strings.Contains(err.Error(), "metadata exceeds") || !strings.Contains(err.Error(), "serialized=") {
		t.Fatalf("error lacks size diagnostics: %v", err)
	}
	if !reflect.DeepEqual(record, &before) {
		t.Fatal("failed publication mutated the original CallRecord")
	}
}

func TestLLMUsageJetStreamServerRoundTrip(t *testing.T) {
	url := os.Getenv("LEWORK_NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("set LEWORK_NATS_INTEGRATION_URL to an isolated JetStream server")
	}
	bus, err := mq.NewNATS(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	connection, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	if got := connection.MaxPayload(); got != 1<<20 {
		t.Fatalf("expected default 1 MiB max payload, got %d", got)
	}
	runSubject, err := messaging.RunEventSubject(990, "isolated-session", messaging.RunEventLaneState)
	if err != nil {
		t.Fatal(err)
	}
	runEvent := messaging.RunEvent{
		ID: "isolated-run-event", Type: messaging.MessageTypeRunEvent, CreatedAt: time.Now(),
		Trace: messaging.TraceContext{TraceID: "isolated-run-trace"},
		Route: messaging.RouteContext{OrgID: 990, SessionID: "isolated-session"},
		Body:  messaging.RunEventBody{Seq: 1, Event: messaging.RunEventRunStarted},
	}
	if err := bus.Publish(context.Background(), runSubject, runEvent); err != nil {
		t.Fatalf("publish isolated Run Event: %v", err)
	}
	commandSubject, err := messaging.WorkerCommandSubject(990, 991, messaging.LaneControl)
	if err != nil {
		t.Fatal(err)
	}
	command := messaging.WorkerCommand{
		ID: "isolated-worker-command", Type: messaging.MessageTypeWorkerCommand, CreatedAt: time.Now(),
		Trace: messaging.TraceContext{TraceID: "isolated-command-trace"},
		Route: messaging.RouteContext{OrgID: 990, WorkerID: 991},
		Body: messaging.WorkerCommandBody{CommandType: messaging.CommandTypeCancel,
			Payload: json.RawMessage(`{"task_id":"isolated-run"}`)},
	}
	if err := bus.Publish(context.Background(), commandSubject, command); err != nil {
		t.Fatalf("publish isolated Worker Command: %v", err)
	}
	js, err := connection.JetStream()
	if err != nil {
		t.Fatal(err)
	}

	recorder := setupRecorderDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		StartUsageConsumer(ctx, recorder, usageSubscriberAdapter{bus: bus})
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("usage consumer did not stop")
		}
		_ = recorder.Shutdown(context.Background())
	})
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := js.ConsumerInfo(messaging.StreamNameLLMUsage, "llm-usage-consumer"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("usage consumer did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	record := testUsageRecord(strings.Repeat("r", 20<<20), strings.Repeat("s", 2<<20), "")
	record.OrgID = 991
	record.ModelID = 992
	record.ReqID = "isolated-usage-round-trip"
	if err := NewRecorderNATS(NewLLMUsagePublisher(bus.Publish), record.OrgID).
		RecordCall(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		persisted, total, listErr := recorder.ListCalls(context.Background(), record.OrgID,
			0, 10, nil, nil, nil, nil)
		if listErr != nil {
			t.Fatal(listErr)
		}
		if total == 1 && len(persisted) == 1 {
			got := persisted[0]
			if got.InputLen != len(record.Input) || got.OutputLen != len(record.Output) ||
				!got.InputTruncated || !got.OutputTruncated || got.InputTokens != record.InputTokens ||
				got.OutputTokens != record.OutputTokens || got.Status != record.Status ||
				got.ReqID != record.ReqID || got.TraceID != record.TraceID {
				t.Fatal("NATS-consumed usage fields differ from the original record")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("usage record was not persisted, total=%d", total)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertCapturedUsage(t *testing.T, original, before *CallRecord, capture *usagePublishCapture) {
	t.Helper()
	if capture.calls != 1 || capture.record == nil {
		t.Fatalf("expected one captured record, got calls=%d record=%v", capture.calls, capture.record)
	}
	if len(capture.body) > usageMessageMaxBytes {
		t.Fatalf("serialized payload exceeds budget: %d", len(capture.body))
	}
	if !utf8.Valid(capture.body) {
		t.Fatal("serialized JSON is not valid UTF-8")
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatal("publishing mutated the original CallRecord")
	}
	want := *before
	want.Input = capture.record.Input
	want.Output = capture.record.Output
	want.Message = capture.record.Message
	want.InputTruncated = capture.record.InputTruncated
	want.OutputTruncated = capture.record.OutputTruncated
	if !reflect.DeepEqual(capture.record, &want) {
		t.Fatal("publication changed structured audit fields")
	}
	var decoded CallRecord
	if err := json.Unmarshal(capture.body, &decoded); err != nil {
		t.Fatalf("decode final publisher payload: %v", err)
	}
	if !reflect.DeepEqual(&decoded, capture.record) {
		t.Fatal("serialized payload does not match the object passed to Publisher")
	}
}
