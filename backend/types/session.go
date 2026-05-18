package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SessionType 会话类型常量
type SessionType string

const (
	SessionTypeUserChat          SessionType = "user_chat"
	SessionTypeAssistantInstance SessionType = "assistant_instance"
)

// SessionStatus 会话状态常量
type SessionStatus string

const (
	SessionStatusActive  SessionStatus = "active"
	SessionStatusPaused  SessionStatus = "paused"
	SessionStatusEnded   SessionStatus = "ended"
	SessionStatusExpired SessionStatus = "expired"
)

// MessageRole 消息角色常量
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

// MessageType 消息类型常量
type MessageType string

const (
	MessageTypeText  MessageType = "text"
	MessageTypeImage MessageType = "image"
	MessageTypeCode  MessageType = "code"
	MessageTypeFile  MessageType = "file"
)

// MessageStatus 消息状态常量
type MessageStatus string

const (
	MessageStatusSending   MessageStatus = "sending"
	MessageStatusStreaming MessageStatus = "streaming"
	MessageStatusComplete  MessageStatus = "complete"
	MessageStatusError     MessageStatus = "error"
)

// Session 会话结构体定义了用户与数字助手之间的会话信息
type Session struct {
	gorm.Model
	// session - 会话唯一标识，格式如：sess_xxx，VARCHAR(255)，NOT NULL，UNIQUE
	SessionID string `gorm:"column:session_id;type:varchar(255);not null;uniqueIndex"`

	// session - 会话类型，VARCHAR(50)，NOT NULL
	Type string `gorm:"column:type;type:varchar(50);not null"`

	// session - 关联用户UIN，0表示不关联，BIGINT，DEFAULT 0
	Uin uint `gorm:"column:uin;type:bigint;default:0;index"`

	// session - 所属组织ID，0表示未设置，INTEGER，NOT NULL，DEFAULT 0
	OrgID uint `gorm:"column:org_id;type:integer;default:0;not null;index"`

	// session - 关联数字助手ID，0表示不关联，BIGINT，DEFAULT 0
	AssistantID uint `gorm:"column:assistant_id;type:bigint;default:0;index"`

	// session - 分配的数字员工ID，0表示未分配，BIGINT，DEFAULT 0
	AllocatedAssistantID uint `gorm:"column:allocated_assistant_id;type:bigint;default:0;index"`

	// session - 会话状态，VARCHAR(50)，NOT NULL，DEFAULT 'active'
	Status string `gorm:"column:status;type:varchar(50);not null;default:'active'"`

	// session - 会话标题，VARCHAR(500)，允许为空
	Title string `gorm:"column:title;type:varchar(500)"`

	// session - 用户是否手动修改过标题
	TitleManuallySet bool `gorm:"column:title_manually_set;type:boolean;default:false;not null"`

	// session - 元数据，JSON格式存储额外信息，JSONB，允许为空
	Metadata SessionMetadata `gorm:"column:metadata;type:jsonb"`

	// session - 消息数量，INTEGER，DEFAULT 0
	MessageCount int `gorm:"column:message_count;type:integer;default:0"`

	// session - 最后消息时间，TIMESTAMP，允许为空
	LastMessageAt *time.Time `gorm:"column:last_message_at"`

	// session - 过期时间，TIMESTAMP，允许为空
	ExpiredAt *time.Time `gorm:"column:expired_at;index"`
}

// TableName 指定Session结构体对应的数据库表名
func (Session) TableName() string {
	return TableNameSession
}

// SessionMetadata 会话元数据结构
type SessionMetadata struct {
	// 元数据 - 用户代理信息
	UserAgent string `json:"user_agent,omitempty"`
	// 元数据 - IP地址
	IPAddress string `json:"ip_address,omitempty"`
	// 元数据 - 自定义标签
	Tags []string `json:"tags,omitempty"`
	// 元数据 - 其他扩展字段
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// Scan 实现 sql.Scanner 接口，用于从数据库中读取 JSON 数据
func (sm *SessionMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into SessionMetadata", value)
	}

	return json.Unmarshal(bytes, sm)
}

// Value 实现 driver.Valuer 接口，用于将元数据转换为 JSON 存储
func (sm SessionMetadata) Value() (driver.Value, error) {
	return json.Marshal(sm)
}

// SessionMessage 会话消息结构体
type SessionMessage struct {
	gorm.Model
	// session_message - 关联会话ID，VARCHAR(255)，NOT NULL，INDEX
	SessionID string `gorm:"column:session_id;type:varchar(255);not null;index"`

	// session_message - 消息角色（user/assistant/system/tool），VARCHAR(50)，NOT NULL
	Role string `gorm:"column:role;type:varchar(50);not null"`

	// session_message - 消息内容，TEXT，NOT NULL
	Content string `gorm:"column:content;type:text;not null"`

	// session_message - 消息类型（text/image/code/file），VARCHAR(50)，DEFAULT 'text'
	MessageType string `gorm:"column:message_type;type:varchar(50);default:'text'"`

	// session_message - 消息状态（sending/streaming/complete/error），VARCHAR(50)，DEFAULT 'complete'
	Status string `gorm:"column:status;type:varchar(50);default:'complete'"`

	// session_message - 流式片段（JSON数组），JSONB，允许为空
	Chunks StringSlice `gorm:"column:chunks;type:jsonb"`

	// session_message - 思维链 / reasoning，TEXT，允许为空
	Thinking string `gorm:"column:thinking;type:text"`

	// session_message - 工具调用信息（JSON数组），JSONB，允许为空
	ToolCalls ToolCallSlice `gorm:"column:tool_calls;type:jsonb"`

	// session_message - 消息元数据，JSONB，允许为空
	Metadata MessageMetadata `gorm:"column:metadata;type:jsonb"`

	// session_message - 消息序号（用于排序），BIGINT，NOT NULL
	Sequence int64 `gorm:"column:sequence;type:bigint;not null;index"`

	// session_message - 时间戳（Unix毫秒），BIGINT，允许为空
	Timestamp int64 `gorm:"column:timestamp;type:bigint"`
}

// TableName 指定SessionMessage结构体对应的数据库表名
func (SessionMessage) TableName() string {
	return TableNameSessionMessage
}

// MessageMetadata 消息元数据结构
type MessageMetadata struct {
	// 图片URL（当 MessageType 为 image 时）
	ImageURL string `json:"image_url,omitempty"`
	// 代码语言（当 MessageType 为 code 时）
	Language string `json:"language,omitempty"`
	// 文件URL（当 MessageType 为 file 时）
	FileURL string `json:"file_url,omitempty"`
	// 文件名
	FileName string `json:"file_name,omitempty"`
	// LLM 模型名称
	Model string `json:"model,omitempty"`
	// Token 数量
	Tokens int `json:"tokens,omitempty"`
	// 延迟（毫秒）
	Latency int `json:"latency,omitempty"`
	// 其他扩展字段
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// ToolCallStatus 工具调用状态常量
type ToolCallStatus string

const (
	ToolCallStatusPending ToolCallStatus = "pending"
	ToolCallStatusRunning ToolCallStatus = "running"
	ToolCallStatusSuccess ToolCallStatus = "success"
	ToolCallStatusError   ToolCallStatus = "error"
)

// ToolCall 工具调用结构
type ToolCall struct {
	// 工具调用ID
	ID string `json:"id"`
	// 工具名称
	Name string `json:"name"`
	// 工具参数
	Arguments map[string]interface{} `json:"arguments"`
	// 工具调用状态
	Status ToolCallStatus `json:"status"`
	// 工具调用结果
	Result interface{} `json:"result,omitempty"`
	// 持续时间（毫秒）
	Duration int `json:"duration,omitempty"`
}

// StringSlice 自定义字符串切片类型，支持 JSONB 存储
type StringSlice []string

// Scan 实现 sql.Scanner 接口
func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = StringSlice{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into StringSlice", value)
	}

	var result []string
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}

	*s = StringSlice(result)
	return nil
}

// Value 实现 driver.Valuer 接口
func (s StringSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal([]string(s))
}

// ToolCallSlice 自定义 ToolCall 切片类型，支持 JSONB 存储
type ToolCallSlice []ToolCall

// Scan 实现 sql.Scanner 接口
func (t *ToolCallSlice) Scan(value interface{}) error {
	if value == nil {
		*t = ToolCallSlice{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into ToolCallSlice", value)
	}

	var result []ToolCall
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}

	*t = ToolCallSlice(result)
	return nil
}

// Value 实现 driver.Valuer 接口
func (t ToolCallSlice) Value() (driver.Value, error) {
	if len(t) == 0 {
		return nil, nil
	}
	return json.Marshal([]ToolCall(t))
}

// Scan 实现 sql.Scanner 接口
func (mm *MessageMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into MessageMetadata", value)
	}

	return json.Unmarshal(bytes, mm)
}

// Value 实现 driver.Valuer 接口
func (mm MessageMetadata) Value() (driver.Value, error) {
	return json.Marshal(mm)
}
