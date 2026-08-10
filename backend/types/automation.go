// types 包提供 Leros 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Automation 表示系统中的自动化定时任务配置
//
// 自动化是将一条 Agent 指令按日历规则或固定间隔自动执行的长期调度配置。
// 自动化本身不是任务（Task），它只在每次触发时生成一次执行记录（AutomationExecution，
// Phase 2 引入）和对应的 cron 任务。
type Automation struct {
	gorm.Model

	// automation - 自动化唯一标识，格式如：auto_xxx，VARCHAR(255)，NOT NULL，UNIQUE
	PublicID string `gorm:"column:public_id;type:varchar(255);not null;uniqueIndex"`

	// automation - 所属组织ID，INTEGER，NOT NULL，INDEX
	OrgID uint `gorm:"column:org_id;type:integer;not null;index"`

	// automation - 创建者用户ID（执行身份），INTEGER，NOT NULL，INDEX
	OwnerID uint `gorm:"column:owner_id;type:integer;not null;index"`

	// automation - 自动化名称，VARCHAR(100)，NOT NULL
	Name string `gorm:"column:name;type:varchar(100);not null"`

	// automation - 每轮发送给 Agent 的完整指令，TEXT，允许为空
	Instruction string `gorm:"column:instruction;type:text"`

	// automation - 是否接受周期触发，BOOLEAN，NOT NULL，DEFAULT TRUE
	Enabled bool `gorm:"column:enabled;type:boolean;not null;default:true"`

	// automation - 调度模式（calendar/interval），只表示底层计算模式，VARCHAR(16)，NOT NULL
	ScheduleMode string `gorm:"column:schedule_mode;type:varchar(16);not null"`

	// automation - 版本化规范化调度规则，JSONB，NOT NULL，DEFAULT '{}'
	ScheduleSpec AutomationScheduleSpec `gorm:"column:schedule_spec;type:jsonb;not null;default:'{}'"`

	// automation - IANA 时区，VARCHAR(64)，NOT NULL，DEFAULT 'Asia/Shanghai'
	Timezone string `gorm:"column:timezone;type:varchar(64);not null;default:'Asia/Shanghai'"`

	// automation - 创建时固定的默认 AI 队友 ID，BIGINT，NOT NULL，INDEX
	AssistantID uint `gorm:"column:assistant_id;type:bigint;not null;index"`

	// automation - 下一次计划执行时间（UTC），TIMESTAMP，可空
	NextRunAt *time.Time `gorm:"column:next_run_at;type:timestamp"`

	// automation - 最近一次实际生成 execution 的时间（UTC），TIMESTAMP，可空
	LastRunAt *time.Time `gorm:"column:last_run_at;type:timestamp"`

	// automation - 当前自动化项目主键，BIGINT，可空，INDEX
	ProjectID *uint `gorm:"column:project_id;type:bigint;index"`

	// automation - 当前项目代数，初始为 0，INTEGER，NOT NULL，DEFAULT 0
	ProjectGeneration int `gorm:"column:project_generation;type:integer;not null;default:0"`
}

// TableName 指定Automation结构体对应的数据库表名
func (Automation) TableName() string {
	return TableNameAutomation
}

// AutomationScheduleFormConfig 表示前端表单的编辑模型（用于回显）
//
// 其中 mode 仅用于编辑界面展示，最终调度语义由 AutomationScheduleSpec.Spec 决定。
type AutomationScheduleFormConfig struct {
	// Mode 调度模式（calendar/interval），与 Automation 的 schedule_mode 保持一致
	Mode string `json:"mode"`
	// Calendar 日历类预设配置（mode == "calendar" 时有效）
	Calendar *AutomationCalendarConfig `json:"calendar,omitempty"`
	// Interval 固定间隔配置（mode == "interval" 时有效）
	Interval *AutomationIntervalConfig `json:"interval,omitempty"`
	// Timezone 表单发起时的 IANA 时区
	Timezone string `json:"timezone,omitempty"`
}

// AutomationCalendarConfig 表示日历类预设的表单配置
type AutomationCalendarConfig struct {
	// Preset 预设类型：daily/weekly/monthly/hourly
	Preset string `json:"preset"`
	// Hour 小时（0-23）
	Hour int `json:"hour"`
	// Minute 分钟（0-59）
	Minute int `json:"minute"`
	// DaysOfWeek 每周预设的星期集合（0=周日，6=周六），0-6
	DaysOfWeek []int `json:"days_of_week,omitempty"`
	// DaysOfMonth 每月预设的日期集合（1-31）
	DaysOfMonth []int `json:"days_of_month,omitempty"`
}

// AutomationIntervalConfig 表示固定间隔配置的表单配置
type AutomationIntervalConfig struct {
	// IntervalSeconds 间隔秒数
	IntervalSeconds int64 `json:"interval_seconds,omitempty"`
	// AnchorAt 本地时区锚点时间（ISO8601，不带时区偏移，解释为指定时区）
	AnchorAt string `json:"anchor_at,omitempty"`
	// IntervalMinutes 间隔分钟数（表单友好字段，服务端换算为秒并生成 interval_seconds）
	IntervalMinutes int `json:"interval_minutes,omitempty"`
	// IntervalUnit 间隔单位，只用于回显：minute/hour/day
	IntervalUnit string `json:"interval_unit,omitempty"`
}

// AutomationScheduleSpecItem 表示编译后的规范化调度规则（Phase 2 直接使用）
type AutomationScheduleSpecItem struct {
	// Version 规则版本
	Version int `json:"version"`
	// Mode 底层计算模式：calendar/interval
	Mode string `json:"mode"`
	// Expression 日历模式下的 cron 表达式（mode == "calendar" 时有效）
	Expression string `json:"expression,omitempty"`
	// Policy 边界策略（月末回退等）
	Policy *AutomationSchedulePolicy `json:"policy,omitempty"`
	// AnchorAt 固定间隔模式的本地锚点时间
	AnchorAt string `json:"anchor_at,omitempty"`
	// IntervalSeconds 固定间隔模式的间隔秒数
	IntervalSeconds int64 `json:"interval_seconds,omitempty"`
	// Timezone 规则生效时区
	Timezone string `json:"timezone"`
}

// AutomationSchedulePolicy 表示日历规则的边界策略
type AutomationSchedulePolicy struct {
	// MonthDayOverflow 当日历配置的日期超过目标月份实际天数时的处理方式
	// 取值为 last_day（月末回退），默认 last_day
	MonthDayOverflow string `json:"month_day_overflow,omitempty"`
}

// AutomationScheduleSpec 表示自动化的完整调度配置，jsonb 存储
type AutomationScheduleSpec struct {
	// FormConfig 前端表单编辑模型，用于编辑回显
	FormConfig *AutomationScheduleFormConfig `json:"form_config"`
	// Spec 编译后的规范化规则
	Spec AutomationScheduleSpecItem `json:"spec"`
}

// Scan 实现 sql.Scanner 接口，用于从数据库中读取 JSON 数据
func (s *AutomationScheduleSpec) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into AutomationScheduleSpec", value)
	}

	return json.Unmarshal(bytes, s)
}

// Value 实现 driver.Valuer 接口，用于将调度配置转换为 JSON 存储
func (s AutomationScheduleSpec) Value() (driver.Value, error) {
	return json.Marshal(s)
}
