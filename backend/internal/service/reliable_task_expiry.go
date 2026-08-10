package service

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

const reliableTaskSourceSessionMessage = "session_message"

// SessionMessageTaskExpiryProjector owns the session-message interpretation of a generic task timeout.
// It intentionally lives outside the dispatcher so other task sources can provide their own projector.
type SessionMessageTaskExpiryProjector struct {
	db *gorm.DB
}

func NewSessionMessageTaskExpiryProjector(db *gorm.DB) *SessionMessageTaskExpiryProjector {
	return &SessionMessageTaskExpiryProjector{db: db}
}

// ProjectExpiredReliableTasks marks only still-pending source messages as failed. Terminal and started
// messages are left untouched so a delayed transport timeout can never reverse a real run outcome.
func (p *SessionMessageTaskExpiryProjector) ProjectExpiredReliableTasks(ctx context.Context, tasks []types.ReliableTask) error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.ProjectExpiredReliableTasksTx(ctx, p.db, tasks)
}

// ProjectExpiredReliableTasksTx projects the source failure with the caller's
// transport transaction, so neither side can be committed independently.
func (p *SessionMessageTaskExpiryProjector) ProjectExpiredReliableTasksTx(ctx context.Context, tx *gorm.DB, tasks []types.ReliableTask) error {
	if p == nil || tx == nil {
		return nil
	}
	ids := make([]uint, 0, len(tasks))
	for _, task := range tasks {
		if task.SourceType != reliableTaskSourceSessionMessage {
			continue
		}
		id, err := strconv.ParseUint(task.SourceID, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		ids = append(ids, uint(id))
	}
	if len(ids) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&types.SessionMessage{}).
		Where("id IN ? AND status = ?", ids, types.MessageStatusPending).
		Updates(map[string]any{
			"status":    types.MessageStatusFailed,
			"error_msg": "queue_start_timeout",
		}).Error
}
