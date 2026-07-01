package inbox

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func TestSQLiteRunInboxLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteRunInbox(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() error = %v", err)
	}
	defer store.Close()

	command := messaging.NewRunCommand(
		"message-1",
		messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "session-1"},
		messaging.TraceContext{RunID: "run-1"},
		messaging.RunCommandPayload{TaskType: messaging.TaskTypeAgentRun},
		nil,
	)

	inserted, record, err := store.PutIfAbsent(ctx, "topic.run", 10, command)
	if err != nil {
		t.Fatalf("PutIfAbsent() error = %v", err)
	}
	if !inserted || record.Status != StatusPending {
		t.Fatalf("inserted = %v, status = %q", inserted, record.Status)
	}

	inserted, existing, err := store.PutIfAbsent(ctx, "topic.run", 10, command)
	if err != nil {
		t.Fatalf("duplicate PutIfAbsent() error = %v", err)
	}
	if inserted || existing == nil || existing.Command == "" {
		t.Fatalf("duplicate result = inserted:%v existing:%#v", inserted, existing)
	}

	if err := store.MarkProcessing(ctx, "topic.run", 10); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}
	records, err := store.GetNonTerminal(ctx, "topic.run")
	if err != nil {
		t.Fatalf("GetNonTerminal() error = %v", err)
	}
	if len(records) != 1 || records[0].Status != StatusProcessing {
		t.Fatalf("non-terminal records = %#v", records)
	}
	if other, err := store.GetNonTerminal(ctx, "topic.other"); err != nil || len(other) != 0 {
		t.Fatalf("other-topic records = %#v, error = %v", other, err)
	}

	if err := store.MarkCompleted(ctx, "topic.run", 10); err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}
	records, err = store.GetNonTerminal(ctx, "topic.run")
	if err != nil {
		t.Fatalf("GetNonTerminal() after completion error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("completed record remained non-terminal: %#v", records)
	}
}

func TestSQLiteRunInboxDeletesExpiredTerminalRecords(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteRunInbox(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() error = %v", err)
	}
	defer store.Close()

	command := messaging.WorkerCommand{ID: "message-1"}
	if _, _, err := store.PutIfAbsent(ctx, "topic.run", 11, command); err != nil {
		t.Fatalf("PutIfAbsent() error = %v", err)
	}
	if err := store.MarkFailed(ctx, "topic.run", 11, "failed"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	deleted, err := store.DeleteTerminalBefore(ctx, "topic.run", time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("DeleteTerminalBefore() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}
