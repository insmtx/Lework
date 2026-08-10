package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

const (
	reliableTaskScanInterval = time.Second
	reliableTaskLease        = 30 * time.Second
	reliableTaskBatchSize    = 50
)

var reliableTaskRetrySchedule = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, time.Minute, 5 * time.Minute}

// ReliableTaskExpiryProjector translates a generic task timeout to a source-specific outcome.
// It keeps the dispatcher independent from source schemas and business parameters.
type ReliableTaskExpiryProjector interface {
	ProjectExpiredReliableTasks(ctx context.Context, tasks []types.ReliableTask) error
}

// TransactionalReliableTaskExpiryProjector keeps source-state updates in the
// same transaction as the transport timeout transition.
type TransactionalReliableTaskExpiryProjector interface {
	ProjectExpiredReliableTasksTx(ctx context.Context, tx *gorm.DB, tasks []types.ReliableTask) error
}

// ReliableTaskDispatcher publishes opaque durable tasks. It deliberately has no business-command dependency.
type ReliableTaskDispatcher struct {
	db        *gorm.DB
	publisher eventbus.Publisher
	expiry    ReliableTaskExpiryProjector
	owner     string
}

func NewReliableTaskDispatcher(db *gorm.DB, publisher eventbus.Publisher, expiry ...ReliableTaskExpiryProjector) *ReliableTaskDispatcher {
	var projector ReliableTaskExpiryProjector
	if len(expiry) > 0 {
		projector = expiry[0]
	}
	return &ReliableTaskDispatcher{db: db, publisher: publisher, expiry: projector, owner: fmt.Sprintf("reliable-task-dispatcher-%d", time.Now().UnixNano())}
}

// Run blocks until ctx is cancelled and continuously publishes due outbox records.
func (d *ReliableTaskDispatcher) Run(ctx context.Context) {
	if d == nil || d.db == nil || d.publisher == nil {
		logs.WarnContextf(ctx, "run dispatch outbox disabled: missing database or publisher")
		return
	}
	ticker := time.NewTicker(reliableTaskScanInterval)
	defer ticker.Stop()
	for {
		d.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *ReliableTaskDispatcher) scan(ctx context.Context) {
	now := time.Now().UTC()
	d.expireDue(ctx, now)
	rows, err := infradb.LeaseReliableTasks(ctx, d.db, now, d.owner, reliableTaskLease, reliableTaskBatchSize)
	if err != nil {
		logs.WarnContextf(ctx, "lease run dispatches: %v", err)
		return
	}
	for _, row := range rows {
		d.publishOne(ctx, row)
	}
}

func (d *ReliableTaskDispatcher) expireDue(ctx context.Context, now time.Time) {
	for range reliableTaskBatchSize {
		var expired []types.ReliableTask
		err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			expired, err = infradb.ExpireReliableTasks(ctx, tx, now, 1)
			if err != nil || len(expired) == 0 || d.expiry == nil {
				return err
			}
			if projector, ok := d.expiry.(TransactionalReliableTaskExpiryProjector); ok {
				return projector.ProjectExpiredReliableTasksTx(ctx, tx, expired)
			}
			return d.expiry.ProjectExpiredReliableTasks(ctx, expired)
		})
		if err != nil {
			logs.WarnContextf(ctx, "expire reliable tasks: %v", err)
			return
		}
		if len(expired) == 0 {
			return
		}
		logs.WarnContextf(ctx, "expired %d queued reliable tasks", len(expired))
	}
}

func (d *ReliableTaskDispatcher) publishOne(ctx context.Context, row types.ReliableTask) {
	// json.RawMessage keeps the durable JSON payload intact while using the existing EventBus transport.
	err := d.publisher.Publish(ctx, row.Destination, json.RawMessage(row.Payload))
	attempt := row.AttemptCount + 1
	next := time.Now().UTC().Add(reliableTaskRetryDelay(attempt))
	if next.After(row.DeadlineAt) {
		next = row.DeadlineAt
	}
	lastError := ""
	if err != nil {
		lastError = err.Error()
		logs.WarnContextf(ctx, "publish reliable task task_id=%s attempt=%d: %v", row.TaskID, attempt, err)
	}
	if recordErr := infradb.RecordReliableTaskAttempt(ctx, d.db, row.ID, d.owner, err == nil, next, attempt, lastError); recordErr != nil {
		logs.WarnContextf(ctx, "record reliable task attempt task_id=%s: %v", row.TaskID, recordErr)
	}
}

func reliableTaskRetryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return reliableTaskRetrySchedule[0]
	}
	index := attempt - 1
	if index >= len(reliableTaskRetrySchedule) {
		index = len(reliableTaskRetrySchedule) - 1
	}
	return reliableTaskRetrySchedule[index]
}
