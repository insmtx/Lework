package inbox

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
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

	if err := store.MarkProcessing(ctx, record.ID); err != nil {
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

	if err := store.MarkCompleted(ctx, record.ID); err != nil {
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
	_, record, err := store.PutIfAbsent(ctx, "topic.run", 11, command)
	if err != nil {
		t.Fatalf("PutIfAbsent() error = %v", err)
	}
	if err := store.MarkFailed(ctx, record.ID, "failed"); err != nil {
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

func TestSQLiteRunInboxCountByStatus(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteRunInbox(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() error = %v", err)
	}
	defer store.Close()

	command := messaging.WorkerCommand{ID: "message-1"}
	// 三条不同 command_id 的记录，其中一条转为 processing（避免 command_id 唯一索引冲突）。
	var firstID uint64
	for _, seq := range []uint64{1, 2, 3} {
		command.ID = "message-" + strconv.FormatUint(seq, 10)
		_, record, err := store.PutIfAbsent(ctx, "topic.run", seq, command)
		if err != nil {
			t.Fatalf("PutIfAbsent(%d) error = %v", seq, err)
		}
		if seq == 1 {
			firstID = record.ID
		}
	}
	if err := store.MarkProcessing(ctx, firstID); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}

	pending, err := store.CountByStatus(ctx, "topic.run", StatusPending)
	if err != nil {
		t.Fatalf("CountByStatus(pending) error = %v", err)
	}
	if pending != 2 {
		t.Fatalf("pending count = %d, want 2", pending)
	}
	processing, err := store.CountByStatus(ctx, "topic.run", StatusProcessing)
	if err != nil {
		t.Fatalf("CountByStatus(processing) error = %v", err)
	}
	if processing != 1 {
		t.Fatalf("processing count = %d, want 1", processing)
	}

	// 只统计话题相关的记录。
	other, err := store.CountByStatus(ctx, "other.topic", StatusPending)
	if err != nil {
		t.Fatalf("CountByStatus(other) error = %v", err)
	}
	if other != 0 {
		t.Fatalf("other-topic pending count = %d, want 0", other)
	}
}

func TestSQLiteRunInboxMigratesLegacySequenceUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "inbox.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE worker_run_inbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			stream_seq INTEGER NOT NULL,
			command_id TEXT NOT NULL DEFAULT '',
			command TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			error_msg TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(topic, stream_seq)
		);
		INSERT INTO worker_run_inbox (topic, stream_seq, command_id, command, status, created_at, updated_at)
		VALUES ('topic.run', 82, 'msg-old', '{}', 'completed', 1, 1);
	`)
	if err != nil {
		t.Fatalf("create legacy inbox: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteRunInbox(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() migrate error = %v", err)
	}
	defer store.Close()

	newCommand := messaging.WorkerCommand{ID: "msg-new"}
	inserted, record, err := store.PutIfAbsent(ctx, "topic.run", 82, newCommand)
	if err != nil {
		t.Fatalf("PutIfAbsent() after migration error = %v", err)
	}
	if !inserted || record.ID == 0 {
		t.Fatalf("new sequence collision record = %#v, inserted=%v", record, inserted)
	}
	if err := store.MarkFailed(ctx, record.ID, "new failure"); err != nil {
		t.Fatalf("MarkFailed() new record: %v", err)
	}

	var oldStatus string
	if err := store.db.QueryRow(`SELECT status FROM worker_run_inbox WHERE command_id = 'msg-old'`).Scan(&oldStatus); err != nil {
		t.Fatalf("read old record: %v", err)
	}
	if oldStatus != string(StatusCompleted) {
		t.Fatalf("old record status = %q, want %q", oldStatus, StatusCompleted)
	}
}

func TestSQLiteRunInboxMigratesLegacyTableWithoutCommandID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "inbox.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE worker_run_inbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			stream_seq INTEGER NOT NULL,
			command TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			error_msg TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(topic, stream_seq)
		);
	`)
	if err != nil {
		t.Fatalf("create legacy inbox: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteRunInbox(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() migrate error = %v", err)
	}
	defer store.Close()

	var commandID string
	if err := store.db.QueryRow(`SELECT command_id FROM worker_run_inbox LIMIT 1`).Scan(&commandID); err != sql.ErrNoRows {
		t.Fatalf("command_id column missing or unexpected query error: %v", err)
	}
}

func TestSQLiteRunInboxDeduplicatesCommandIDAcrossSequences(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteRunInbox(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRunInbox() error = %v", err)
	}
	defer store.Close()

	command := messaging.WorkerCommand{ID: "msg-once"}
	inserted, first, err := store.PutIfAbsent(ctx, "topic.run", 10, command)
	if err != nil || !inserted {
		t.Fatalf("first PutIfAbsent() = inserted:%v record:%#v err:%v", inserted, first, err)
	}
	inserted, duplicate, err := store.PutIfAbsent(ctx, "topic.run", 99, command)
	if err != nil {
		t.Fatalf("duplicate PutIfAbsent() error = %v", err)
	}
	if inserted || duplicate == nil || duplicate.ID != first.ID || duplicate.StreamSeq != 10 {
		t.Fatalf("duplicate = inserted:%v record:%#v, want original record", inserted, duplicate)
	}
}
