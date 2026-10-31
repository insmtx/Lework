// types 包提供 Lework 的核心数据类型定义
//
// 该包定义了数字助手、事件、用户、技能等核心领域模型，
// 以及相关的常量和数据库表名定义。
package types

import "gorm.io/gorm"

// Event 表示系统中持久化存储的事件记录
//
// 该结构用于将事件信息存储到数据库中，包含事件的基本信息、
// 来源、类型、动作、参与者、目标和负载数据等。
type Event struct {
	gorm.Model

	MessageID string // 消息唯一标识符
	TraceID   string // 分布式追踪 ID
	Source    string // 事件来源
	Type      string // 事件类型（建议使用 types.EventType 定义的常量值）
	Action    string // 事件动作（建议使用 types.EventAction 定义的常量值）

	Actor  string // 事件触发者
	Target string // 事件目标

	Payload map[string]interface{} `gorm:"type:jsonb"` // 事件负载数据

	Timestamp int64 // 事件时间戳
}
