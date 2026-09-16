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

// RunDispatchNotifier 由写入可靠任务发件箱的调用方在事务提交后触发，
// 使派发器立刻开始下一次扫描，把固定轮询间隔从首 token 关键路径上移除。
//
// 实现必须是非阻塞的：通知只用于压缩等待，丢失通知由派发器自身的兜底轮询兜住。
type RunDispatchNotifier interface {
	NotifyRunDispatch()
}

// RunDispatchNotifierAware 是可选能力：需要唤醒发件箱的服务实现它，
// 装配阶段据此注入派发器，避免把该能力放进对外契约接口。
type RunDispatchNotifierAware interface {
	SetRunDispatchNotifier(notifier RunDispatchNotifier)
}

// ReliableTaskDispatcher publishes opaque durable tasks. It deliberately has no business-command dependency.
type ReliableTaskDispatcher struct {
	db        *gorm.DB
	publisher eventbus.Publisher
	expiry    ReliableTaskExpiryProjector
	owner     string
	// notify 容量为 1：多次并发唤醒会被合并成一次扫描，扫描本身按批次取任务，
	// 不会漏掉同批写入的记录。
	notify chan struct{}
}

func NewReliableTaskDispatcher(db *gorm.DB, publisher eventbus.Publisher, expiry ...ReliableTaskExpiryProjector) *ReliableTaskDispatcher {
	var projector ReliableTaskExpiryProjector
	if len(expiry) > 0 {
		projector = expiry[0]
	}
	return &ReliableTaskDispatcher{
		db:        db,
		publisher: publisher,
		expiry:    projector,
		owner:     fmt.Sprintf("reliable-task-dispatcher-%d", time.Now().UnixNano()),
		notify:    make(chan struct{}, 1),
	}
}

// NotifyRunDispatch 请求立即扫描一次发件箱。永不阻塞。
func (d *ReliableTaskDispatcher) NotifyRunDispatch() {
	if d == nil || d.notify == nil {
		return
	}
	select {
	case d.notify <- struct{}{}:
	default:
		// 已有待处理的唤醒信号：本次写入会被那次扫描的批次一起取出。
	}
}

// Run blocks until ctx is cancelled and continuously publishes due outbox records.
// 轮询间隔只是兜底：正常情况下由 NotifyRunDispatch 驱动扫描。
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
		case <-d.notify:
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
