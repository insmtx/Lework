package types

import "time"

// SeedExecStatus 表示一个 SQL seed 文件的执行结果。
type SeedExecStatus string

const (
	// SeedExecStatusSuccess 表示文件已成功执行完。
	SeedExecStatusSuccess SeedExecStatus = "succ"
	// SeedExecStatusFailed 表示文件执行失败，记录失败行号供断点续跑。
	SeedExecStatusFailed SeedExecStatus = "fail"
)

// SeedRecord 记录某个 SQL seed 文件的执行情况，用于跳过已成功文件与失败续跑。
type SeedRecord struct {
	ID           uint           `gorm:"column:id;primarykey"`
	FileName     string         `gorm:"column:file_name;type:varchar(255);not null;index"`
	ExecStatus   SeedExecStatus `gorm:"column:exec_status;type:varchar(10);not null;index"`
	ErrorMessage string         `gorm:"column:error_message;type:text"`
	FailLineAt   int            `gorm:"column:fail_line_at;type:int;not null"`
	StartTime    time.Time      `gorm:"column:start_time"`
	EndTime      time.Time      `gorm:"column:end_time"`
	ExecTime     float64        `gorm:"column:exec_time"`
}

// TableName 返回种子执行记录的表名。
func (SeedRecord) TableName() string { return TableNameSeedRecord }
