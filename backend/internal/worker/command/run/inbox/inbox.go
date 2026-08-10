// Package inbox provides a strongly-typed durable inbox for worker run commands.
//
// The inbox stores complete WorkerCommand JSON keyed by topic + stream_seq,
// enabling at-least-once crash recovery. Non-terminal records are re-dispatched
// on worker restart.
package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	_ "github.com/mattn/go-sqlite3"
)

// Status represents the processing state of an inbox record.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Record is a durable inbox entry.
type Record struct {
	ID        uint64 `json:"id"`
	Topic     string `json:"topic"`
	StreamSeq uint64 `json:"stream_seq"`
	Command   string `json:"command"`
	Status    Status `json:"status"`
	ErrorMsg  string `json:"error_msg,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// IsTerminal returns true if the record has reached a terminal state.
func (r *Record) IsTerminal() bool {
	return r.Status == StatusCompleted || r.Status == StatusFailed
}

// RunInbox persists worker run commands for at-least-once crash recovery.
type RunInbox interface {
	// PutIfAbsent inserts a new record. Returns (true, nil) on insert,
	// (false, existing record, nil) if already exists, or an error.
	PutIfAbsent(ctx context.Context, topic string, streamSeq uint64, cmd messaging.WorkerCommand) (bool, *Record, error)

	// MarkProcessing transitions a record to processing.
	MarkProcessing(ctx context.Context, topic string, streamSeq uint64) error

	// MarkCompleted transitions a record to completed.
	MarkCompleted(ctx context.Context, topic string, streamSeq uint64) error

	// MarkFailed transitions a record to failed.
	MarkFailed(ctx context.Context, topic string, streamSeq uint64, errMsg string) error

	// GetNonTerminal returns non-terminal records for a topic, ordered by stream_seq.
	GetNonTerminal(ctx context.Context, topic string) ([]Record, error)

	// ResetProcessing returns records interrupted by a prior process to pending before recovery.
	ResetProcessing(ctx context.Context, topic string) error

	// CountByStatus returns the number of records for a topic with the given status.
	CountByStatus(ctx context.Context, topic string, status Status) (int, error)

	// DeleteTerminalBefore deletes terminal records older than the given time.
	DeleteTerminalBefore(ctx context.Context, topic string, before time.Time) (int64, error)

	// Close closes the database.
	Close() error
}

// SQLiteRunInbox implements RunInbox using SQLite.
type SQLiteRunInbox struct {
	db *sql.DB
}

// NewSQLiteRunInbox opens or creates the worker_run_inbox table.
func NewSQLiteRunInbox(dbPath string) (*SQLiteRunInbox, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open inbox db %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate inbox: %w", err)
	}

	return &SQLiteRunInbox{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS worker_run_inbox (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			topic      TEXT NOT NULL,
			stream_seq INTEGER NOT NULL,
			command_id TEXT NOT NULL DEFAULT '',
			command    TEXT NOT NULL,
			status     TEXT NOT NULL DEFAULT 'pending',
			error_msg  TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			UNIQUE(topic, stream_seq)
		);
		CREATE INDEX IF NOT EXISTS idx_inbox_topic_status ON worker_run_inbox(topic, status);
	`)
	if err != nil {
		return err
	}
	// 老库升级：补齐 command_id 列与按 command_id 去重的唯一索引
	ensureColumn(db, "worker_run_inbox", "command_id", `ALTER TABLE worker_run_inbox ADD COLUMN command_id TEXT NOT NULL DEFAULT ''`)
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ux_inbox_command_id ON worker_run_inbox(command_id) WHERE command_id != ''`)
	return err
}

// ensureColumn 若表中不存在指定列则执行迁移 SQL（忽略已存在的错误）。
func ensureColumn(db *sql.DB, table, column, alterSQL string) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == column {
			return
		}
	}
	_, _ = db.Exec(alterSQL)
}

// PutIfAbsent inserts a new record with the command serialized to JSON.
func (i *SQLiteRunInbox) PutIfAbsent(ctx context.Context, topic string, streamSeq uint64, cmd messaging.WorkerCommand) (bool, *Record, error) {
	commandJSON, err := json.Marshal(cmd)
	if err != nil {
		return false, nil, fmt.Errorf("marshal command: %w", err)
	}

	now := time.Now().Unix()
	result, err := i.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO worker_run_inbox (topic, stream_seq, command_id, command, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		topic, streamSeq, cmd.ID, string(commandJSON), string(StatusPending), now, now,
	)
	if err != nil {
		return false, nil, fmt.Errorf("insert inbox: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("rows affected: %w", err)
	}

	if rowsAffected == 0 {
		rec, err := i.get(ctx, topic, streamSeq)
		if rec == nil {
			// get 返回 (nil, nil)（ErrNoRows）或 error：说明本 (topic, stream_seq)
			// 无存量记录，但插入却被唯一索引（如 command_id）拒绝。此时按
			// command_id 归并返回已存在记录，避免返回 nil 记录导致调用方解引用 panic。
			rec = i.getByCommandID(ctx, cmd.ID)
			if rec == nil {
				if err == nil {
					err = fmt.Errorf("insert ignored but no existing record found (topic=%s stream_seq=%d command_id=%q)", topic, streamSeq, cmd.ID)
				}
				return false, nil, err
			}
		}
		return false, rec, nil
	}

	return true, &Record{
		Topic:     topic,
		StreamSeq: streamSeq,
		Command:   string(commandJSON),
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (i *SQLiteRunInbox) get(ctx context.Context, topic string, streamSeq uint64) (*Record, error) {
	rec := &Record{}
	err := i.db.QueryRowContext(ctx,
		`SELECT id, topic, stream_seq, command, status, error_msg, created_at, updated_at
		 FROM worker_run_inbox WHERE topic = ? AND stream_seq = ?`,
		topic, streamSeq,
	).Scan(&rec.ID, &rec.Topic, &rec.StreamSeq, &rec.Command, &rec.Status, &rec.ErrorMsg, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get inbox: %w", err)
	}
	return rec, nil
}

// getByCommandID 按稳定命令 ID 查询已存在的记录（用于重复投递去重）。
func (i *SQLiteRunInbox) getByCommandID(ctx context.Context, commandID string) *Record {
	if commandID == "" {
		return nil
	}
	rec := &Record{}
	err := i.db.QueryRowContext(ctx,
		`SELECT id, topic, stream_seq, command, status, error_msg, created_at, updated_at
		 FROM worker_run_inbox WHERE command_id = ? LIMIT 1`,
		commandID,
	).Scan(&rec.ID, &rec.Topic, &rec.StreamSeq, &rec.Command, &rec.Status, &rec.ErrorMsg, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil
	}
	return rec
}

// MarkProcessing transitions a record to processing.
func (i *SQLiteRunInbox) MarkProcessing(ctx context.Context, topic string, streamSeq uint64) error {
	return i.updateStatus(ctx, topic, streamSeq, StatusProcessing, "")
}

// MarkCompleted transitions a record to completed.
func (i *SQLiteRunInbox) MarkCompleted(ctx context.Context, topic string, streamSeq uint64) error {
	return i.updateStatus(ctx, topic, streamSeq, StatusCompleted, "")
}

// MarkFailed transitions a record to failed.
func (i *SQLiteRunInbox) MarkFailed(ctx context.Context, topic string, streamSeq uint64, errMsg string) error {
	return i.updateStatus(ctx, topic, streamSeq, StatusFailed, errMsg)
}

func (i *SQLiteRunInbox) updateStatus(ctx context.Context, topic string, streamSeq uint64, status Status, errMsg string) error {
	now := time.Now().Unix()
	_, err := i.db.ExecContext(ctx,
		`UPDATE worker_run_inbox SET status = ?, error_msg = ?, updated_at = ? WHERE topic = ? AND stream_seq = ?`,
		string(status), errMsg, now, topic, streamSeq,
	)
	return err
}

// GetNonTerminal returns non-terminal records for a topic, ordered by stream_seq.
func (i *SQLiteRunInbox) GetNonTerminal(ctx context.Context, topic string) ([]Record, error) {
	return i.query(ctx,
		`SELECT id, topic, stream_seq, command, status, error_msg, created_at, updated_at
		 FROM worker_run_inbox
		 WHERE topic = ? AND status NOT IN (?, ?)
		 ORDER BY stream_seq ASC`,
		topic, string(StatusCompleted), string(StatusFailed),
	)
}

func (i *SQLiteRunInbox) ResetProcessing(ctx context.Context, topic string) error {
	_, err := i.db.ExecContext(ctx,
		`UPDATE worker_run_inbox SET status = ?, updated_at = ? WHERE topic = ? AND status = ?`,
		string(StatusPending), time.Now().Unix(), topic, string(StatusProcessing),
	)
	return err
}

func (i *SQLiteRunInbox) query(ctx context.Context, query string, args ...any) ([]Record, error) {
	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query inbox: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.Topic, &r.StreamSeq, &r.Command, &r.Status, &r.ErrorMsg, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan inbox: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// DeleteTerminalBefore deletes terminal records older than the given time.
func (i *SQLiteRunInbox) DeleteTerminalBefore(ctx context.Context, topic string, before time.Time) (int64, error) {
	result, err := i.db.ExecContext(ctx,
		`DELETE FROM worker_run_inbox
		 WHERE topic = ? AND status IN (?, ?) AND updated_at < ?`,
		topic, string(StatusCompleted), string(StatusFailed), before.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CountByStatus returns the number of records for a topic with the given status.
func (i *SQLiteRunInbox) CountByStatus(ctx context.Context, topic string, status Status) (int, error) {
	var count int
	err := i.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM worker_run_inbox WHERE topic = ? AND status = ?`,
		topic, string(status),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count inbox by status %q: %w", status, err)
	}
	return count, nil
}

// Close closes the database.
func (i *SQLiteRunInbox) Close() error {
	return i.db.Close()
}
