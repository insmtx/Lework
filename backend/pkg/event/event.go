// event 包提供事件定义的共享类型
//
// 该包定义了 Lework 中事件的核心数据结构，
// 用于在不同模块间共享和传递事件信息。
package event

import "time"

// Event 表示系统中的一个交互事件
//
// 事件是 Lework 的核心数据结构，包含了来自不同渠道（如 GitHub、GitLab 等）
// 的交互信息，通过事件总线在系统中流转和处理。
type Event struct {
	EventID    string                 // 事件唯一标识符
	TraceID    string                 // 分布式追踪 ID
	Channel    string                 // 事件来源渠道（如 github、gitlab）
	EventType  string                 // 事件类型（如 issue_comment、pull_request）
	Actor      string                 // 事件触发者
	Repository string                 // 关联的代码仓库
	Context    map[string]interface{} // 事件上下文信息
	Payload    interface{}            // 事件原始负载数据
	CreatedAt  time.Time              // 事件创建时间
}
