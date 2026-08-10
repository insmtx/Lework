package types

import (
	"time"

	"gorm.io/gorm"
)

// Task 表示系统中的任务
//
// 任务是项目中的最小执行单元，由 AI 队友（DigitalAssistant）执行，
// 支持子任务拆分（通过 ParentTaskID 自引用）和状态生命周期管理。
type Task struct {
	gorm.Model
	PublicID  string `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`
	OrgID     uint   `gorm:"column:org_id;type:integer;not null;index"`
	OwnerID   uint   `gorm:"column:owner_id;type:integer;not null;index"`
	ProjectID uint   `gorm:"column:project_id;type:bigint;not null;index"`
	SessionID *uint  `gorm:"column:session_id;type:bigint;index"`

	// TaskType 任务类型（如：general/general、cron/定时任务等），VARCHAR(16)，NOT NULL，DEFAULT 'general'
	TaskType TaskType `gorm:"column:task_type;type:varchar(16);not null;default:'general';index"`

	AssigneeID *uint `gorm:"column:assignee_id;type:bigint;index"`

	Title       string     `gorm:"column:title;type:varchar(500);not null"`
	Description string     `gorm:"column:description;type:text"`
	Status      string     `gorm:"column:status;type:varchar(50);not null;default:'created';index"`
	Deadline    *time.Time `gorm:"column:deadline;index"`

	// cron 任务的自动化执行记录主键，BIGINT，可空，唯一索引（防止 Dispatcher 重试重复建 Task）
	AutomationExecutionID *uint `gorm:"column:automation_execution_id;type:bigint;index"`

	Metadata ObjectMetadata `gorm:"column:metadata;type:jsonb"`
}

// TableName 指定Task结构体对应的数据库表名
func (Task) TableName() string {
	return TableNameTask
}
