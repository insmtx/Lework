package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// defaultExecutorLeaseFor Dispatcher 领取执行记录后的租约时长
	defaultExecutorLeaseFor = 2 * time.Minute
	// maxDispatchAttempts 最大派发尝试次数；超过则标记 automation_dispatch_failed
	maxDispatchAttempts = 3
	// defaultExecutionPollInterval Dispatcher 无唤醒时的兜底轮询间隔
	defaultExecutionPollInterval = 15 * time.Second
)

// dispatchBackoff 返回第 n 次尝试（0 起）失败后的重试退避。
var dispatchBackoff = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	2 * time.Minute,
}

var (
	// ErrExecutionExpired 表示执行已超过 not_after
	ErrExecutionExpired = errors.New("automation execution expired")
)

// AutomationDispatcher 把 queued execution 转换为业务实体并发布 agent.run。
//
// 幂等核心：以 execution 的 public_id 作为稳定命令 ID；以 AutomationExecutionID
// 幂等创建/恢复 Task、首条消息；重复派发不会重复创建实体。
type AutomationDispatcher struct {
	db          *gorm.DB
	poster      *MessagePoster
	provisioner *AutomationProjectProvisioner
}

// NewAutomationDispatcher 构造 Dispatcher
func NewAutomationDispatcher(db *gorm.DB, poster *MessagePoster, provisioner *AutomationProjectProvisioner) *AutomationDispatcher {
	return &AutomationDispatcher{db: db, poster: poster, provisioner: provisioner}
}

// Dispatch 尝试派发一个 queued execution；返回是否需要重试。
func (d *AutomationDispatcher) Dispatch(ctx context.Context, now time.Time) error {
	// 过期扫描：已投递但未收到 run.started 且超过 not_after → 标记失败
	d.scanDispatchedExpired(ctx, now)
	// 兜底：running 执行超过 not_after 仍未终态 → 标 failed，释放 active 占位（避免后续周期无限 skipped）
	d.scanRunningTimeout(ctx, now)

	candidates, err := db.ListLeasableQueuedExecutions(ctx, d.db, now, defaultAutomationBatchSize)
	if err != nil {
		return err
	}
	logs.InfoContextf(ctx, "dispatcher scan: %d leasable queued executions", len(candidates))
	for _, ex := range candidates {
		if ctx.Err() != nil {
			break
		}
		if err := d.dispatchOne(ctx, ex, now); err != nil {
			logs.WarnContextf(ctx, "dispatcher dispatch execution %d failed: %v", ex.ID, err)
		}
	}
	return nil
}

// scanDispatchedExpired 扫描已投递但超过 not_after、仍未收到 run.started 的 queued 执行并标记失败。
func (d *AutomationDispatcher) scanDispatchedExpired(ctx context.Context, now time.Time) {
	expired, err := db.ListDispatchedExpiredExecutions(ctx, d.db, now, defaultAutomationBatchSize)
	if err != nil {
		logs.WarnContextf(ctx, "dispatcher scan dispatched-expired failed: %v", err)
		return
	}
	for _, ex := range expired {
		if ctx.Err() != nil {
			return
		}
		if err := db.MarkExecutionDispatchExpired(ctx, d.db, ex.ID); err != nil {
			logs.WarnContextf(ctx, "dispatcher mark dispatched-expired failed for execution %d: %v", ex.ID, err)
		} else {
			logs.WarnContextf(ctx, "dispatcher marked dispatched-expired execution %d", ex.ID)
		}
	}
}

// scanRunningTimeout 兜底扫描：running 执行超过各自 not_after（命令截止时间）仍未到终态时，
// 标记为 failed/automation_run_timeout，释放该自动化的 active 占位，避免后续周期被无限 skipped。
func (d *AutomationDispatcher) scanRunningTimeout(ctx context.Context, now time.Time) {
	stuck, err := db.ListRunningTimeoutExecutions(ctx, d.db, now, defaultAutomationBatchSize)
	if err != nil {
		logs.WarnContextf(ctx, "dispatcher scan running-timeout failed: %v", err)
		return
	}
	for _, ex := range stuck {
		if ctx.Err() != nil {
			return
		}
		if err := db.MarkExecutionRunTimeout(ctx, d.db, ex.ID); err != nil {
			logs.WarnContextf(ctx, "dispatcher mark run-timeout failed for execution %d: %v", ex.ID, err)
		} else {
			logs.WarnContextf(ctx, "dispatcher marked run-timeout execution %d (ran past not_after)", ex.ID)
		}
	}
}

// dispatchOne 处理单个 queued execution。
//
// 结果分类：
//   - 永久错误（owner 失效、配置问题、权限）：直接 failed。
//   - 临时错误（DB/NATS/Gitea 暂时不可用、项目创建失败）：按退避重试；
//     尝试次数达到上限后标记 failed/automation_dispatch_failed。
func (d *AutomationDispatcher) dispatchOne(ctx context.Context, ex *types.AutomationExecution, now time.Time) error {
	// 1. 领取租约（CAS）
	leaseOwner := fmt.Sprintf("dispatcher-%d", now.UnixNano())
	acquired, err := db.AcquireExecutionLease(ctx, d.db, ex.ID, leaseOwner, defaultExecutorLeaseFor, now)
	if err != nil {
		return err
	}
	if !acquired {
		logs.DebugContextf(ctx, "dispatcher execution %d lease not acquired (skip)", ex.ID)
		return nil
	}
	logs.InfoContextf(ctx, "dispatcher processing execution %d (org=%d owner=%d trigger=%s)", ex.ID, ex.OrgID, ex.OwnerID, ex.TriggerType)

	// 2. 过期检查：超过 not_after 直接失败（不会再被投递）
	if ex.NotAfter != nil && !ex.NotAfter.IsZero() && now.After(*ex.NotAfter) {
		return d.markFailed(ctx, ex, "automation_dispatch_expired", "投递超时，命令已过期")
	}

	// 3. 加载自动化并按最新配置执行
	automation, err := d.loadAutomationByID(ctx, ex)
	if automation == nil {
		return d.markFailed(ctx, ex, "automation_not_found", "自动化不存在")
	}

	// 5. 确保自动化项目存在（首次执行懒创建 / 已删则换代）
	project, err := d.provisioner.EnsureProject(ctx, automation)
	if err != nil {
		return d.retryOrFail(ctx, ex, now, err)
	}
	logs.InfoContextf(ctx, "dispatcher execution %d project ready id=%d", ex.ID, project.ID)

	// 6. 幂等创建 Task / TaskSession / 首条消息，并发布 agent.run
	if err := d.dispatchExecution(ctx, automation, ex, project, now); err != nil {
		return d.retryOrFail(ctx, ex, now, err)
	}
	logs.InfoContextf(ctx, "dispatcher execution %d dispatched ok", ex.ID)
	return nil
}

// retryOrFail 处理一次派发失败：临时错误按退避重试，达到上限后标记失败。
func (d *AutomationDispatcher) retryOrFail(ctx context.Context, ex *types.AutomationExecution, now time.Time, err error) error {
	ex.AttemptCount++
	if ex.AttemptCount >= maxDispatchAttempts {
		logs.WarnContextf(ctx, "dispatcher execution %d exceeded max attempts (%d): %v", ex.ID, maxDispatchAttempts, err)
		return d.markFailed(ctx, ex, "automation_dispatch_failed", "多次投递失败")
	}
	// 设置下次可重试时间（复用 lease_until 作为退避门控）
	backoff := dispatchBackoff[minIntExclusive(ex.AttemptCount-1, len(dispatchBackoff)-1)]
	nextRetry := now.Add(backoff)
	if err := db.SetExecutionRetryAt(ctx, d.db, ex.ID, nextRetry, ex.AttemptCount); err != nil {
		logs.WarnContextf(ctx, "dispatcher set retry-at failed for execution %d: %v", ex.ID, err)
	}
	logs.WarnContextf(ctx, "dispatcher execution %d transient failure attempt=%d backoff=%s: %v", ex.ID, ex.AttemptCount, backoff, err)
	return nil
}

// minIntExclusive 返回 a、b 的较小值（b 为索引上限）。
func minIntExclusive(a, b int) int {
	if a > b {
		return b
	}
	return a
}

func (d *AutomationDispatcher) loadAutomationByID(ctx context.Context, ex *types.AutomationExecution) (*types.Automation, error) {
	var a types.Automation
	err := d.db.WithContext(ctx).First(&a, ex.AutomationID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (d *AutomationDispatcher) markFailed(ctx context.Context, ex *types.AutomationExecution, code, msg string) error {
	now := time.Now().UTC()
	ex.Status = types.AutomationExecutionFailed
	ex.FinishedAt = &now
	ex.ErrorCode = truncateError(code)
	ex.ErrorMsg = truncateError(msg)
	return db.UpdateAutomationExecution(ctx, d.db, ex)
}

func truncateError(s string) string {
	const maxLen = 500
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen])
	}
	return strings.TrimSpace(s)
}

// dispatchExecution 幂等创建 cron Task / TaskSession / 首条消息，并发布 agent.run。
func (d *AutomationDispatcher) dispatchExecution(ctx context.Context,
	automation *types.Automation, ex *types.AutomationExecution, project *types.Project, now time.Time) error {

	// 幂等恢复 Task（按 AutomationExecutionID）
	task, err := db.GetTaskByAutomationExecutionID(ctx, d.db, ex.OrgID, ex.ID)
	if err != nil {
		return err
	}
	if task == nil {
		task, err = d.createCronTask(ctx, automation, ex, project)
		if err != nil {
			return err
		}
	}
	// 幂等恢复 TaskSession（从 Task.SessionID）
	if task.SessionID == nil {
		sess, err := d.createTaskSession(ctx, ex, project, task)
		if err != nil {
			return err
		}
		if err := d.db.WithContext(ctx).Model(task).Update("session_id", sess.ID).Error; err != nil {
			return err
		}
	}

	// 幂等恢复首条消息（按 AutomationExecutionID），否则创建并发布
	message, err := db.GetMessageByAutomationExecutionID(ctx, d.db, ex.OrgID, ex.ID)
	if err != nil {
		return err
	}
	if message == nil {
		session, err := d.loadTaskSession(ctx, task)
		if err != nil {
			return err
		}
		// 解析固定 AI 队友路由（assistant -> worker）
		assistantID, workerID, err := resolveProjectAssistantWorker(ctx, d.db, ex.OrgID, project.ID, []uint{ex.AssistantIDSnapshot}, nil)
		if err != nil {
			return err
		}
		routing := &MessageRoutingOverride{AssistantID: assistantID, WorkerID: workerID}

		// 解析固定 AI 队友路由（assistant -> worker）
		senderUin := ex.OwnerID
		senderName := ""
		if d.poster.userRepo != nil {
			if u, uErr := d.poster.userRepo.GetUserByUin(ctx, ex.OwnerID); uErr == nil && u != nil {
				senderName = u.Name
			}
		}

		message, err = d.poster.PostMessage(ctx, session, types.ExecutionModeDefault,
			func(sequence int64) *types.SessionMessage {
				execID := ex.ID
				nowMs := time.Now().UnixMilli()
				return &types.SessionMessage{
					Role:                  string(types.MessageRoleUser),
					Content:               ex.InstructionSnapshot,
					MessageType:           string(types.MessageTypeText),
					Status:                string(types.MessageStatusPending),
					Sequence:              sequence,
					Timestamp:             nowMs,
					SenderUin:             &senderUin,
					SenderName:            senderName,
					AutomationExecutionID: &execID,
					Metadata: types.ObjectMetadata{
						Type:  "automation",
						Extra: map[string]interface{}{"source": "automation"},
					},
				}
			},
			routing,
			&MessageExecutionOptions{
				QueueDeadline: ex.NotAfter,
				Policy: messaging.TaskPolicy{
					DisabledPlugins: []types.DisabledPlugin{{
						Kind: types.DisabledPluginKindSkill,
						Code: "lework-automation-manager",
					}},
				},
			},
		)
		if err != nil {
			return err
		}
	}

	// 回写 execution 的业务实体 ID（含 SessionID，供执行历史抽屉跳转任务页）
	dispatchedAt := time.Now().UTC()
	ex.TaskID = &task.ID
	ex.SessionID = task.SessionID
	ex.MessageID = &message.ID
	ex.ProjectID = &project.ID
	ex.DispatchedAt = &dispatchedAt
	ex.AttemptCount++
	if err := db.UpdateAutomationExecution(ctx, d.db, ex); err != nil {
		return err
	}
	return nil
}

func (d *AutomationDispatcher) createCronTask(ctx context.Context,
	automation *types.Automation, ex *types.AutomationExecution, project *types.Project) (*types.Task, error) {
	loc := time.UTC
	if automation.Timezone != "" {
		if l, err := time.LoadLocation(automation.Timezone); err == nil {
			loc = l
		}
	}
	title := fmt.Sprintf("%s · %s", ex.NameSnapshot, ex.ScheduledAt.In(loc).Format("2006-01-02 15:04"))
	taskID := fmt.Sprintf("task_%s", snowflake.GenerateIDBase58())
	assistantID := ex.AssistantIDSnapshot
	task := &types.Task{
		PublicID:              taskID,
		OrgID:                 ex.OrgID,
		OwnerID:               ex.OwnerID,
		ProjectID:             project.ID,
		TaskType:              types.TaskTypeCron,
		Title:                 title,
		Description:           ex.InstructionSnapshot,
		Status:                string(types.TaskStatusCreated),
		AssigneeID:            &assistantID, // 固定 AI 队友
		AutomationExecutionID: &ex.ID,
	}
	if err := db.CreateTask(ctx, d.db, task); err != nil {
		return nil, err
	}
	if err := syncTaskResource(ctx, d.db, ex.OrgID, project.ID, task.ID, ex.OwnerID); err != nil {
		return nil, err
	}
	return task, nil
}

func (d *AutomationDispatcher) createTaskSession(ctx context.Context,
	ex *types.AutomationExecution, project *types.Project, task *types.Task) (*types.Session, error) {
	sessID := fmt.Sprintf("sess_%s", snowflake.GenerateIDBase58())
	session := &types.Session{
		PublicID:             sessID,
		Type:                 types.SessionTypeTask,
		Uin:                  ex.OwnerID,
		OrgID:                ex.OrgID,
		AssistantID:          ex.AssistantIDSnapshot, // 固定 AI 队友
		AllocatedAssistantID: ex.AssistantIDSnapshot,
		ProjectID:            &project.ID,
		TaskID:               &task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                task.Title,
	}
	if err := db.CreateSession(ctx, d.db, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (d *AutomationDispatcher) loadTaskSession(ctx context.Context, task *types.Task) (*types.Session, error) {
	if task.SessionID == nil {
		return nil, errors.New("task has no session")
	}
	var session types.Session
	if err := d.db.WithContext(ctx).First(&session, *task.SessionID).Error; err != nil {
		return nil, err
	}
	return &session, nil
}
