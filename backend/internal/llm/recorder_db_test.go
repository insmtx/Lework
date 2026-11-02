package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/types"
)

func setupRecorderDB(t *testing.T) *RecorderDb {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(&types.LLMHistory{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return NewRecorder(database)
}

func TestRecorderRecordCallAndListCalls(t *testing.T) {
	recorder := setupRecorderDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	record := &CallRecord{
		OrgID:         testOrgID,
		ModelID:       10,
		Provider:      "openai",
		ModelName:     "gpt-4o-mini",
		EntryProtocol: "eino_chat",
		IsStream:      false,
		InputTokens:   100,
		OutputTokens:  50,
		TotalTokens:   150,
		LatencyMS:     2000,
		StatusCode:    200,
		Success:       true,
		Status:        "success",
		CallerType:    "worker",
		ReqID:         "task-123",
		ProjectID:     1,
		SessionID:     2,
		MessageID:     3,
		AssistantID:   4,
		Uin:           5,
		StartedAt:     now,
		FinishedAt:    now.Add(2 * time.Second),
	}

	if err := recorder.RecordCall(ctx, record); err != nil {
		t.Fatalf("RecordCall failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	records, total, err := recorder.ListCalls(ctx, testOrgID, 0, 50, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListCalls failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.OrgID != record.OrgID {
		t.Fatalf("expected OrgID=%d, got %d", record.OrgID, got.OrgID)
	}
	if got.ModelID != record.ModelID {
		t.Fatalf("expected ModelID=%d, got %d", record.ModelID, got.ModelID)
	}
	if got.Provider != record.Provider {
		t.Fatalf("expected Provider=%q, got %q", record.Provider, got.Provider)
	}
	if got.ModelName != record.ModelName {
		t.Fatalf("expected ModelName=%q, got %q", record.ModelName, got.ModelName)
	}
	if got.EntryProtocol != record.EntryProtocol {
		t.Fatalf("expected EntryProtocol=%q, got %q", record.EntryProtocol, got.EntryProtocol)
	}
	if got.IsStream != record.IsStream {
		t.Fatalf("expected IsStream=%v, got %v", record.IsStream, got.IsStream)
	}
	if got.InputTokens != record.InputTokens {
		t.Fatalf("expected InputTokens=%d, got %d", record.InputTokens, got.InputTokens)
	}
	if got.OutputTokens != record.OutputTokens {
		t.Fatalf("expected OutputTokens=%d, got %d", record.OutputTokens, got.OutputTokens)
	}
	if got.TotalTokens != record.TotalTokens {
		t.Fatalf("expected TotalTokens=%d, got %d", record.TotalTokens, got.TotalTokens)
	}
	if got.LatencyMS != record.LatencyMS {
		t.Fatalf("expected LatencyMS=%d, got %d", record.LatencyMS, got.LatencyMS)
	}
	if got.StatusCode != record.StatusCode {
		t.Fatalf("expected StatusCode=%d, got %d", record.StatusCode, got.StatusCode)
	}
	if got.Success != record.Success {
		t.Fatalf("expected Success=%v, got %v", record.Success, got.Success)
	}
	if got.Message != record.Message {
		t.Fatalf("expected Message=%q, got %q", record.Message, got.Message)
	}
	if got.CallerType != record.CallerType {
		t.Fatalf("expected CallerType=%q, got %q", record.CallerType, got.CallerType)
	}
	if got.ReqID != record.ReqID {
		t.Fatalf("expected ReqID=%q, got %q", record.ReqID, got.ReqID)
	}
	if got.ProjectID != record.ProjectID {
		t.Fatalf("expected ProjectID=%d, got %d", record.ProjectID, got.ProjectID)
	}
	if got.SessionID != record.SessionID {
		t.Fatalf("expected SessionID=%d, got %d", record.SessionID, got.SessionID)
	}
	if got.MessageID != record.MessageID {
		t.Fatalf("expected MessageID=%d, got %d", record.MessageID, got.MessageID)
	}
	if got.AssistantID != record.AssistantID {
		t.Fatalf("expected AssistantID=%d, got %d", record.AssistantID, got.AssistantID)
	}
	if got.Uin != record.Uin {
		t.Fatalf("expected Uin=%d, got %d", record.Uin, got.Uin)
	}
}

func TestRecorderListCallsWithFilters(t *testing.T) {
	recorder := setupRecorderDB(t)
	ctx := context.Background()
	now := time.Now()

	records := []*CallRecord{
		{OrgID: testOrgID, Provider: "openai", CallerType: "worker", Success: true, StartedAt: now, FinishedAt: now},
		{OrgID: testOrgID, Provider: "openai", CallerType: "api", Success: false, Message: "timeout", StartedAt: now, FinishedAt: now},
		{OrgID: testOrgID, Provider: "deepseek", CallerType: "worker", Success: true, StartedAt: now, FinishedAt: now},
	}
	for i, r := range records {
		if err := recorder.RecordCall(ctx, r); err != nil {
			t.Fatalf("RecordCall[%d] failed: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	provider := "openai"
	records, total, err := recorder.ListCalls(ctx, testOrgID, 0, 50, nil, &provider, nil, nil)
	if err != nil {
		t.Fatalf("ListCalls by provider failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 openai records, got %d", total)
	}

	callerType := "worker"
	_, total, err = recorder.ListCalls(ctx, testOrgID, 0, 50, nil, nil, &callerType, nil)
	if err != nil {
		t.Fatalf("ListCalls by callerType failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 worker records, got %d", total)
	}

	failed := false
	_, total, err = recorder.ListCalls(ctx, testOrgID, 0, 50, nil, nil, nil, &failed)
	if err != nil {
		t.Fatalf("ListCalls by success=false failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 failed record, got %d", total)
	}
}

func TestRecorderRecordCallNilRecord(t *testing.T) {
	recorder := setupRecorderDB(t)
	if err := recorder.RecordCall(context.Background(), nil); err != nil {
		t.Fatalf("RecordCall(nil) should not error, got: %v", err)
	}
}

func TestCallRecordToEntityNil(t *testing.T) {
	if e := callRecordToEntity(nil); e != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestCallRecordFromEntityNil(t *testing.T) {
	if r := callRecordFromEntity(nil); r != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestCallRecordToEntityPreservesIDWhenSet(t *testing.T) {
	r := &CallRecord{ID: 42, OrgID: 1}
	e := callRecordToEntity(r)
	if e.ID != 42 {
		t.Fatalf("expected ID=42, got %d", e.ID)
	}
}

func TestCallRecordToEntityPreservesOriginalLengthsAndTruncation(t *testing.T) {
	input := strings.Repeat("输入😀", historyOutputLimit+10)
	output := strings.Repeat("响应", historyOutputLimit+20)
	record := &CallRecord{
		Input: input, Output: output, InputLen: 1 << 20, OutputLen: 8 << 20,
		InputTruncated: true, OutputTruncated: true,
	}
	entity := callRecordToEntity(record)
	if entity.InputLen != record.InputLen || entity.OutputLen != record.OutputLen {
		t.Fatalf("original lengths overwritten: input=%d output=%d", entity.InputLen, entity.OutputLen)
	}
	if !entity.InputTruncated || !entity.OutputTruncated {
		t.Fatalf("truncation flags lost: input=%v output=%v", entity.InputTruncated, entity.OutputTruncated)
	}
	if len([]rune(entity.Input)) != historyOutputLimit+3 || len([]rune(entity.Output)) != historyOutputLimit+3 {
		t.Fatal("database preview truncation did not retain its existing limit")
	}
}

func TestCallRecordToEntityFallsBackForMissingLengths(t *testing.T) {
	record := &CallRecord{Input: "request", Output: "response"}
	entity := callRecordToEntity(record)
	if entity.InputLen != len(record.Input) || entity.OutputLen != len(record.Output) {
		t.Fatalf("missing lengths did not fall back to content bytes: input=%d output=%d",
			entity.InputLen, entity.OutputLen)
	}
}

func TestRecorderDBPersistsTruncatedUsageOriginalLengths(t *testing.T) {
	recorder := setupRecorderDB(t)
	ctx := context.Background()
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	input := strings.Repeat("i", 1<<20)
	output := strings.Repeat("o", 8<<20)
	record := &CallRecord{
		OrgID: testOrgID, ModelID: 91, Provider: "test-provider", ModelName: "test-model",
		Input: input, Output: output, InputLen: 1 << 20, OutputLen: 8 << 20,
		InputTruncated: true, OutputTruncated: true, InputTokens: 123, OutputTokens: 45,
		TotalTokens: 168, StatusCode: 200, Success: true, Status: "success",
		CallerType: CallerTypeWorker, ReqID: "run-usage-1", TraceID: "trace-usage-1",
		StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(),
	}
	publication, size, err := boundedUsageRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if size > usageMessageMaxBytes {
		t.Fatalf("NATS usage copy exceeds budget: %d", size)
	}
	payload, err := json.Marshal(publication)
	if err != nil {
		t.Fatal(err)
	}
	var consumed CallRecord
	if err := json.Unmarshal(payload, &consumed); err != nil {
		t.Fatal(err)
	}
	entity := callRecordToEntity(&consumed)
	if err := recorder.db.Create(entity).Error; err != nil {
		t.Fatal(err)
	}
	records, total, err := recorder.ListCalls(ctx, testOrgID, 0, 10, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("expected one persisted record, total=%d records=%d", total, len(records))
	}
	got := records[0]
	if got.InputLen != record.InputLen || got.OutputLen != record.OutputLen ||
		!got.InputTruncated || !got.OutputTruncated {
		t.Fatalf("persisted lengths/flags changed: in=%d out=%d in_truncated=%v out_truncated=%v",
			got.InputLen, got.OutputLen, got.InputTruncated, got.OutputTruncated)
	}
	if got.InputTokens != record.InputTokens || got.OutputTokens != record.OutputTokens ||
		got.StatusCode != record.StatusCode || got.Success != record.Success ||
		got.ReqID != record.ReqID || got.TraceID != record.TraceID {
		t.Fatal("persisted usage metadata changed")
	}
	if len([]rune(got.Input)) != historyOutputLimit+3 || len([]rune(got.Output)) != historyOutputLimit+3 {
		t.Fatal("persisted previews do not match the existing content limit")
	}
}
