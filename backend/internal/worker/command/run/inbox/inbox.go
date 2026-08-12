// Package inbox provides a strongly-typed durable inbox for worker run commands.
//
// The inbox stores complete WorkerCommand JSON keyed by command_id. JetStream
// stream sequences are retained for observability only: a stream can be
// recreated and reuse a sequence from an earlier stream lifetime.
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
	CommandID string `json:"command_id"`
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
	MarkProcessing(ctx context.Context, recordID uint64) error

	// MarkCompleted transitions a record to completed.
	MarkCompleted(ctx context.Context, recordID uint64) error

	// MarkFailed transitions a record to failed.
	MarkFailed(ctx context.Context, recordID uint64, errMsg string) error

	// GetNonTerminal returns non-terminal records for a topic in insertion order.
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
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);
	`)
	if err != nil {
		return err
	}
	if err := ensureColumn(db, "worker_run_inbox", "command_id", `ALTER TABLE worker_run_inbox ADD COLUMN command_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if legacy, err := hasLegacyTopicSeqUnique(db); err != nil {
		return err
	} else if legacy {
		if err := rebuildWithoutTopicSeqUnique(db); err != nil {
			return err
		}
	}
	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_inbox_command_id ON worker_run_inbox(command_id) WHERE command_id != '';
		CREATE INDEX IF NOT EXISTS idx_inbox_topic_stream_seq ON worker_run_inbox(topic, stream_seq);
		CREATE INDEX IF NOT EXISTS idx_inbox_topic_status_created ON worker_run_inbox(topic, status, created_at, id);
	`)
	return err
}

// ensureColumn 若表中不存在指定列则执行迁移 SQL。
func ensureColumn(db *sql.DB, table, column, alterSQL string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			return rows.Close()
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = db.Exec(alterSQL)
	return err
}

func hasLegacyTopicSeqUnique(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA index_list(worker_run_inbox)`)
	if err != nil {
		return false, err
	}
	var uniqueIndexes []string
	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return false, err
		}
		if unique != 0 {
			uniqueIndexes = append(uniqueIndexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	for _, indexName := range uniqueIndexes {
		if indexHasColumns(db, indexName, "topic", "stream_seq") {
			return true, nil
		}
	}
	return false, nil
}

func indexHasColumns(db *sql.DB, indexName string, want ...string) bool {
	rows, err := db.Query(`PRAGMA index_info(` + indexName + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return false
		}
		got = append(got, name)
	}
	if len(got) != len(want) {
		return false
	}
	for idx := range want {
		if got[idx] != want[idx] {
			return false
		}
	}
	return true
}

func rebuildWithoutTopicSeqUnique(db *sql.DB) error {
	return withTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			CREATE TABLE worker_run_inbox_next (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				topic      TEXT NOT NULL,
				stream_seq INTEGER NOT NULL,
				command_id TEXT NOT NULL DEFAULT '',
				command    TEXT NOT NULL,
				status     TEXT NOT NULL DEFAULT 'pending',
				error_msg  TEXT NOT NULL DEFAULT '',
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL
			);
			INSERT INTO worker_run_inbox_next (id, topic, stream_seq, command_id, command, status, error_msg, created_at, updated_at)
			SELECT id, topic, stream_seq, command_id, command, status, error_msg, created_at, updated_at FROM worker_run_inbox;
			DROP TABLE worker_run_inbox;
			ALTER TABLE worker_run_inbox_next RENAME TO worker_run_inbox;
		`); err != nil {
			return err
		}
		return nil
	})
}

func withTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// PutIfAbsent inserts a new record with the command serialized to JSON.
func (i *SQLiteRunInbox) PutIfAbsent(ctx context.Context, topic string, streamSeq uint64, cmd messaging.WorkerCommand) (bool, *Record, error) {
	if cmd.ID == "" {
		return false, nil, fmt.Errorf("command_id is required")
	}
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
		rec, err := i.getByCommandID(ctx, cmd.ID)
		if err != nil {
			return false, nil, err
		}
		if rec == nil {
			return false, nil, fmt.Errorf("insert ignored but command_id %q was not found", cmd.ID)
		}
		return false, rec, nil
	}
	recordID, err := result.LastInsertId()
	if err != nil {
		return false, nil, fmt.Errorf("last insert id: %w", err)
	}

	return true, &Record{
		ID:        uint64(recordID),
		Topic:     topic,
		StreamSeq: streamSeq,
		CommandID: cmd.ID,
		Command:   string(commandJSON),
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// getByCommandID 按稳定命令 ID 查询已存在的记录（用于重复投递去重）。
func (i *SQLiteRunInbox) getByCommandID(ctx context.Context, commandID string) (*Record, error) {
	if commandID == "" {
		return nil, nil
	}
	rec := &Record{}
	err := i.db.QueryRowContext(ctx,
		`SELECT id, topic, stream_seq, command_id, command, status, error_msg, created_at, updated_at
		 FROM worker_run_inbox WHERE command_id = ? LIMIT 1`,
		commandID,
	).Scan(&rec.ID, &rec.Topic, &rec.StreamSeq, &rec.CommandID, &rec.Command, &rec.Status, &rec.ErrorMsg, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get inbox by command_id: %w", err)
	}
	return rec, nil
}

// MarkProcessing transitions a record to processing.
func (i *SQLiteRunInbox) MarkProcessing(ctx context.Context, recordID uint64) error {
	return i.updateStatus(ctx, recordID, StatusProcessing, "")
}

// MarkCompleted transitions a record to completed.
func (i *SQLiteRunInbox) MarkCompleted(ctx context.Context, recordID uint64) error {
	return i.updateStatus(ctx, recordID, StatusCompleted, "")
}

// MarkFailed transitions a record to failed.
func (i *SQLiteRunInbox) MarkFailed(ctx context.Context, recordID uint64, errMsg string) error {
	return i.updateStatus(ctx, recordID, StatusFailed, errMsg)
}

func (i *SQLiteRunInbox) updateStatus(ctx context.Context, recordID uint64, status Status, errMsg string) error {
	now := time.Now().Unix()
	_, err := i.db.ExecContext(ctx,
		`UPDATE worker_run_inbox SET status = ?, error_msg = ?, updated_at = ? WHERE id = ?`,
		string(status), errMsg, now, recordID,
	)
	return err
}

// GetNonTerminal returns non-terminal records for a topic, ordered by stream_seq.
func (i *SQLiteRunInbox) GetNonTerminal(ctx context.Context, topic string) ([]Record, error) {
	return i.query(ctx,
		`SELECT id, topic, stream_seq, command_id, command, status, error_msg, created_at, updated_at
		 FROM worker_run_inbox
		 WHERE topic = ? AND status NOT IN (?, ?)
		 ORDER BY created_at ASC, id ASC`,
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
		if err := rows.Scan(&r.ID, &r.Topic, &r.StreamSeq, &r.CommandID, &r.Command, &r.Status, &r.ErrorMsg, &r.CreatedAt, &r.UpdatedAt); err != nil {
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
