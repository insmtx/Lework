package messaging

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/types"
)

// GlobalEventType 标识全局通知事件的类型。
type GlobalEventType string

// GlobalEventMessageCreated 在用户消息或 AI 队友回复持久化成功后触发，
// 通知群聊成员有新消息到达。
const GlobalEventMessageCreated GlobalEventType = "message.created"

// SenderType 区分消息发送方是真人员工还是 AI 队友。
type SenderType string

const (
	SenderTypeHuman     SenderType = "human"     // 真人员工发言
	SenderTypeAssistant SenderType = "assistant" // AI 队友回复
)

// GlobalEventPayload 是 GlobalEvents SSE 端点下发的统一信封。
//
// Seq 字段由消费侧从 NATS JetStream message metadata 填充，
// 发布方无需设置。
type GlobalEventPayload struct {
	Type      GlobalEventType `json:"type"`
	ProjectID uint            `json:"project_id"`
	SessionID string          `json:"session_id"`
	Seq       uint64          `json:"seq"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// MessageCreatedData 是 message.created 事件的 payload。
//
// human 场景 Content 为完整消息内容（前端可直接渲染）；
// assistant 场景（run.started 时发布）Content 为空，仅通知"AI 开始回复"，
// 前端收到后用 session_id + assistant_id 订阅 SessionEvents 获取流式输出。
type MessageCreatedData struct {
	MessageID     uint       `json:"message_id"`
	Sequence      int64      `json:"sequence"`
	SenderType    SenderType `json:"sender_type"`
	SenderUin     *uint      `json:"sender_uin,omitempty"`
	SenderName    string     `json:"sender_name"`
	AssistantID   *uint      `json:"assistant_id,omitempty"`
	AssistantName string     `json:"assistant_name,omitempty"`
	Content       string     `json:"content"`
	RunID         string     `json:"run_id,omitempty"`

	// 前端 human 消息渲染所需字段（assistant 事件不设置，omitempty 保证不出现）
	MessageType string                  `json:"message_type,omitempty"`
	Attachments []types.MessageAttachment `json:"attachments,omitempty"`
	Metadata    *types.ObjectMetadata     `json:"metadata,omitempty"`
}

// HumanMessageData 是 sender_type=human 时的 message.created payload。
// 携带完整消息数据，前端可直接渲染，无需再调 GetSessionMessages。
type HumanMessageData struct {
	SenderType  SenderType `json:"sender_type"`
	SenderUin   *uint      `json:"sender_uin,omitempty"`
	SenderName  string     `json:"sender_name"`
	Content     string     `json:"content"`
	MessageType string     `json:"message_type"`
	Sequence    int64      `json:"sequence"`
	RunID       string     `json:"run_id,omitempty"`
	CreatedAt   string     `json:"created_at"`
}

// AssistantMessageTrigger 是 sender_type=assistant 时的 message.created payload。
type AssistantMessageTrigger struct {
	SenderType    SenderType `json:"sender_type"`
	AssistantID   *uint      `json:"assistant_id,omitempty"`
	AssistantName string     `json:"assistant_name"`
	RunID         string     `json:"run_id,omitempty"`
}
