// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

import (
	"time"

	"gorm.io/gorm"
)

// AutomationExecution 表示一次自动化计划触发或手动触发。
//
// 每次触发生成一条独立记录，用于追踪执行生命周期、状态、失败原因和
// 已创建的业务实体（项目/任务/会话/消息/Run）。execution 同时充当
// Server 侧的持久化 outbox，保证 NATS 失败时可重试、不重复。
type AutomationExecution struct {
	gorm.Model

	// autom_auto_exec - 对外 ID，格式如：autoexec_xxx，VARCHAR(255)，NOT NULL，UNIQUE
	PublicID string `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`

	// autom_auto_exec - 所属自动化主键，BIGINT，NOT NULL，INDEX（与 occurrence_key 组成唯一防重）
	AutomationID uint `gorm:"column:automation_id;type:bigint;not null;index;uniqueIndex:ux_auto_exec_occurrence"`

	// autom_auto_exec - 所属组织ID，BIGINT，NOT NULL，INDEX
	OrgID uint `gorm:"column:org_id;type:bigint;not null;index"`

	// autom_auto_exec - 执行身份（创建者）用户ID，BIGINT，NOT NULL，INDEX
	OwnerID uint `gorm:"column:owner_id;type:bigint;not null;index"`

	// autom_auto_exec - 触发来源：scheduled/manual，VARCHAR(16)，NOT NULL
	TriggerType AutomationExecutionTriggerType `gorm:"column:trigger_type;type:varchar(16);not null;index"`

	// autom_auto_exec - 周期防重键；周期触发用理论计划时间，手动触发用 execution public ID。
	// (automation_id, occurrence_key) 组合唯一，作为周期最终防重边界。
	OccurrenceKey string `gorm:"column:occurrence_key;type:varchar(128);not null;uniqueIndex:ux_auto_exec_occurrence"`

	// autom_auto_exec - 执行状态：queued/running/succeeded/failed/skipped，VARCHAR(16)，NOT NULL，INDEX
	Status AutomationExecutionStatus `gorm:"column:status;type:varchar(16);not null;default:'queued';index"`

	// autom_auto_exec - 理论触发时间（UTC），TIMESTAMP，NOT NULL
	ScheduledAt time.Time `gorm:"column:scheduled_at;not null;index"`

	// autom_auto_exec - Worker 最晚允许开始时间（UTC），TIMESTAMP，可空
	NotAfter *time.Time `gorm:"column:not_after;type:timestamp"`

	// autom_auto_exec - 实际开始时间（UTC），TIMESTAMP，可空
	StartedAt *time.Time `gorm:"column:started_at;type:timestamp"`

	// autom_auto_exec - 实际完成时间（UTC），TIMESTAMP，可空
	FinishedAt *time.Time `gorm:"column:finished_at;type:timestamp"`

	// autom_auto_exec - 自动化名称快照，VARCHAR(100)，NOT NULL
	NameSnapshot string `gorm:"column:name_snapshot;type:varchar(100);not null"`

	// autom_auto_exec - 指令快照，TEXT，允许为空
	InstructionSnapshot string `gorm:"column:instruction_snapshot;type:text"`

	// autom_auto_exec - 固定 AI 队友快照，BIGINT，NOT NULL
	AssistantIDSnapshot uint `gorm:"column:assistant_id_snapshot;type:bigint;not null"`

	// autom_auto_exec - 被折叠的更早遗漏数量，INTEGER，NOT NULL，DEFAULT 0
	MissedCount int `gorm:"column:missed_count;type:integer;not null;default:0"`

	// autom_auto_exec - 已创建项目主键，BIGINT，可空，INDEX
	ProjectID *uint `gorm:"column:project_id;type:bigint;index"`
	// autom_auto_exec - 已创建任务主键，BIGINT，可空，INDEX
	TaskID *uint `gorm:"column:task_id;type:bigint;index"`
	// autom_auto_exec - 已创建会话主键，BIGINT，可空，INDEX
	SessionID *uint `gorm:"column:session_id;type:bigint;index"`
	// autom_auto_exec - 已创建首条消息主键，BIGINT，可空，INDEX
	MessageID *uint `gorm:"column:message_id;type:bigint;index"`

	// autom_auto_exec - Worker Run 标识，VARCHAR(255)，可空，INDEX
	RunID string `gorm:"column:run_id;type:varchar(255);index"`

	// autom_auto_exec - Dispatcher 投递尝试次数，INTEGER，NOT NULL，DEFAULT 0
	AttemptCount int `gorm:"column:attempt_count;type:integer;not null;default:0"`

	// autom_auto_exec - 多实例执行领取租约持有者，VARCHAR(64)，可空
	LeaseOwner string `gorm:"column:lease_owner;type:varchar(64)"`
	// autom_auto_exec - 领取租约到期时间（UTC），TIMESTAMP，可空
	LeaseUntil *time.Time `gorm:"column:lease_until;type:timestamp"`

	// autom_auto_exec - 稳定错误码，VARCHAR(64)，可空
	ErrorCode string `gorm:"column:error_code;type:varchar(64)"`
	// autom_auto_exec - 可展示失败信息（入库前截断并清理敏感字段），TEXT，可空
	ErrorMsg string `gorm:"column:error_msg;type:text"`

	// autom_auto_exec - 命令成功写入 JetStream 的时间（UTC），TIMESTAMP，可空
	DispatchedAt *time.Time `gorm:"column:dispatched_at;type:timestamp"`
}

// TableName 指定AutomationExecution结构体对应的数据库表名
func (AutomationExecution) TableName() string {
	return TableNameAutomationExecution
}
