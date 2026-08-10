package db

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateReliableTask writes an opaque task in the caller-owned business transaction.
func CreateReliableTask(ctx context.Context, database *gorm.DB, task *types.ReliableTask) error {
	return database.WithContext(ctx).Create(task).Error
}

// GetReliableTaskBySource returns the latest durable task associated with a business source.
func GetReliableTaskBySource(ctx context.Context, database *gorm.DB, sourceType, sourceID string) (*types.ReliableTask, error) {
	var task types.ReliableTask
	err := database.WithContext(ctx).Where("source_type = ? AND source_id = ?", sourceType, sourceID).Order("id DESC").First(&task).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &task, err
}

// LeaseReliableTasks claims due records without interpreting their payload.
func LeaseReliableTasks(ctx context.Context, database *gorm.DB, now time.Time, owner string, leaseFor time.Duration, limit int) ([]types.ReliableTask, error) {
	if limit <= 0 {
		limit = 50
	}
	var candidates []types.ReliableTask
	err := database.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ? AND deadline_at > ? AND (lease_until IS NULL OR lease_until < ?)",
			types.ReliableTaskPending, now, now, now).
		Order("next_attempt_at ASC, id ASC").
		Limit(limit * 2).
		Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(leaseFor)
	claimed := make([]types.ReliableTask, 0, limit)
	for _, candidate := range candidates {
		result := database.WithContext(ctx).Model(&types.ReliableTask{}).
			Where("id = ? AND status = ? AND next_attempt_at <= ? AND deadline_at > ? AND (lease_until IS NULL OR lease_until < ?)",
				candidate.ID, types.ReliableTaskPending, now, now, now).
			Updates(map[string]any{"lease_owner": owner, "lease_until": leaseUntil})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			candidate.LeaseOwner = owner
			candidate.LeaseUntil = &leaseUntil
			claimed = append(claimed, candidate)
			if len(claimed) == limit {
				break
			}
		}
	}
	return claimed, nil
}

// RecordReliableTaskAttempt records a transport attempt and releases the lease.
func RecordReliableTaskAttempt(ctx context.Context, database *gorm.DB, id uint, owner string, published bool, next time.Time, attempt int, lastError string) error {
	updates := map[string]any{
		"attempt_count":   attempt,
		"next_attempt_at": next,
		"last_error":      lastError,
		"lease_owner":     "",
		"lease_until":     nil,
	}
	if published {
		now := time.Now().UTC()
		updates["status"] = types.ReliableTaskPublished
		updates["published_at"] = now
	}
	return database.WithContext(ctx).Model(&types.ReliableTask{}).
		Where("id = ? AND lease_owner = ? AND status = ?", id, owner, types.ReliableTaskPending).
		Updates(updates).Error
}

// ExpireReliableTasks closes task transport lifecycle only; business failure projection is handled by its source adapter.
func ExpireReliableTasks(ctx context.Context, database *gorm.DB, now time.Time, limit int) ([]types.ReliableTask, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []types.ReliableTask
	if err := database.WithContext(ctx).
		Where("status = ? AND deadline_at <= ?", types.ReliableTaskPending, now).
		Order("deadline_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	expired := make([]types.ReliableTask, 0, len(rows))
	for _, row := range rows {
		result := database.WithContext(ctx).Model(&types.ReliableTask{}).
			Where("id = ? AND status = ?", row.ID, types.ReliableTaskPending).
			Updates(map[string]any{
				"status":      types.ReliableTaskExpired,
				"last_error":  "queue_start_timeout",
				"lease_owner": "",
				"lease_until": nil,
			})
		if result.Error != nil {
			return expired, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		expired = append(expired, row)
	}
	return expired, nil
}

// CreateProjectionReceipt inserts a consumer-scoped idempotency marker in the caller-owned transaction.
func CreateProjectionReceipt(ctx context.Context, database *gorm.DB, receipt *types.ProjectionReceipt) (bool, error) {
	result := database.WithContext(ctx).Where("consumer = ? AND event_id = ?", receipt.Consumer, receipt.EventID).FirstOrCreate(receipt)
	return result.RowsAffected == 1, result.Error
}
