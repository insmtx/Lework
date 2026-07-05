// Package runtimehost provides Worker-specific implementations of agent ports.
package runtimehost

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteSessionStore implements agentrun.ProviderSessionStore using a local SQLite database.
// The database path is injected by the composition root, not resolved internally.
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore opens a SQLite session store at the given path.
func NewSQLiteSessionStore(dbPath string) (*SQLiteSessionStore, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("session store db path is required")
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if err := migrateProviderSessionBindings(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate provider_sessions: %w", err)
	}
	return &SQLiteSessionStore{db: db}, nil
}

func migrateProviderSessionBindings(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS provider_session_bindings (
			internal_session_id TEXT NOT NULL,
			provider            TEXT NOT NULL,
			provider_session_id TEXT NOT NULL,
			status              TEXT NOT NULL DEFAULT 'active',
			last_error          TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (internal_session_id, provider)
		)
	`)
	return err
}

// GetProviderSession implements agentrun.ProviderSessionStore.
func (s *SQLiteSessionStore) GetProviderSession(_ context.Context, key agentrun.ProviderSessionKey) (*agentrun.ProviderSessionBinding, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	binding := &agentrun.ProviderSessionBinding{}
	err := s.db.QueryRow(
		`SELECT internal_session_id, provider, provider_session_id, status, last_error
		 FROM provider_session_bindings
		 WHERE internal_session_id = ? AND provider = ?`,
		key.InternalSessionID, key.Provider,
	).Scan(
		&binding.InternalSessionID, &binding.Provider, &binding.ProviderSessionID,
		&binding.Status, &binding.LastError,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return binding, nil
}

// UpsertProviderSession implements agentrun.ProviderSessionStore.
func (s *SQLiteSessionStore) UpsertProviderSession(_ context.Context, binding *agentrun.ProviderSessionBinding) error {
	if s == nil || s.db == nil || binding == nil {
		return nil
	}
	if strings.TrimSpace(binding.InternalSessionID) == "" ||
		strings.TrimSpace(binding.Provider) == "" ||
		strings.TrimSpace(binding.ProviderSessionID) == "" {
		return nil
	}
	if binding.Status == "" {
		binding.Status = "active"
	}
	_, err := s.db.Exec(
		`INSERT INTO provider_session_bindings
			(internal_session_id, provider, provider_session_id, status, last_error)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(internal_session_id, provider)
		 DO UPDATE SET provider_session_id = excluded.provider_session_id,
		               status = excluded.status,
		               last_error = excluded.last_error`,
		binding.InternalSessionID, binding.Provider, binding.ProviderSessionID,
		binding.Status, binding.LastError,
	)
	return err
}

// MarkProviderSessionFailed implements agentrun.ProviderSessionStore.
func (s *SQLiteSessionStore) MarkProviderSessionFailed(_ context.Context, key agentrun.ProviderSessionKey, reason string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE provider_session_bindings
		 SET status = 'failed', last_error = ?
		 WHERE internal_session_id = ? AND provider = ?`,
		reason, key.InternalSessionID, key.Provider,
	)
	return err
}

// Close closes the underlying database.
func (s *SQLiteSessionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

var _ agentrun.ProviderSessionStore = (*SQLiteSessionStore)(nil)
