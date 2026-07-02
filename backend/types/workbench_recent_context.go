package types

import (
	"time"

	"gorm.io/gorm"
)

// WorkbenchRecentContext 记录用户在首页工作台最近明确使用的项目/任务上下文。
type WorkbenchRecentContext struct {
	gorm.Model
	OrgID     uint      `gorm:"column:org_id;type:integer;not null;uniqueIndex:uni_workbench_recent_context"`
	Uin       uint      `gorm:"column:uin;type:integer;not null;uniqueIndex:uni_workbench_recent_context"`
	ProjectID uint      `gorm:"column:project_id;type:bigint;not null;index"`
	TaskID    *uint     `gorm:"column:task_id;type:bigint;index"`
	UsedAt    time.Time `gorm:"column:used_at;not null;index"`
}

// TableName 指定 WorkbenchRecentContext 对应的数据库表名。
func (WorkbenchRecentContext) TableName() string {
	return TableNameWorkbenchRecentContext
}
