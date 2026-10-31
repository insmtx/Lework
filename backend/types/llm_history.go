package types

import (
	"time"

	"gorm.io/gorm"
)

// LLMHistory 记录一次LLM调用的完整信息。
//
// 该表用于持久化每次LLM调用的入参摘要、耗时、Token用量和执行结果，
// 为后续的用量统计、成本分析和链路追踪提供数据支撑。
type LLMHistory struct {
	gorm.Model

	// 组织ID，用于隔离不同组织的调用记录
	OrgID uint `gorm:"column:org_id;type:integer;not null;index:idx_llm_call_org_started;sort:desc"`
	// 调用所使用的模型ID，关联LLMModel表
	ModelID uint `gorm:"column:model_id;type:integer;index:idx_llm_call_model"`
	// 模型供应商标识，例如 openai、anthropic、deepseek
	Provider string `gorm:"column:provider;type:varchar(64);not null"`
	// 实际调用的模型名称
	ModelName string `gorm:"column:model_name;type:varchar(255);not null"`
	// 模型供应商（与 provider 一致，单独存储便于检索）
	ModelProvider string `gorm:"column:model_provider;type:varchar(50);not null;default:''"`
	// 调用入口协议，如 openai_chat、anthropic_messages、openai_responses、gemini
	EntryProtocol string `gorm:"column:entry_protocol;type:varchar(32)"`
	// 是否为流式调用
	IsStream bool `gorm:"column:is_stream;type:boolean;default:false"`
	// 输入Token数量
	InputTokens int `gorm:"column:input_tokens;type:integer;default:0"`
	// 输出Token数量
	OutputTokens int `gorm:"column:output_tokens;type:integer;default:0"`
	// 总Token数量
	TotalTokens int `gorm:"column:total_tokens;type:integer;default:0"`
	// 调用耗时，单位毫秒
	LatencyMS int64 `gorm:"column:latency_ms;type:bigint;default:0"`
	// Prompt Token 用量（大整型）
	PromptTokens int64 `gorm:"column:prompt_tokens;type:bigint;default:0"`
	// 缓存命中 Token 数
	CacheHitTokens int64 `gorm:"column:cache_hit_tokens;type:bigint;default:0"`
	// 缓存未命中 Token 数
	CacheMissTokens int64 `gorm:"column:cache_miss_tokens;type:bigint;default:0"`
	// HTTP状态码或供应商返回的业务状态码
	StatusCode int `gorm:"column:status_code;type:integer;default:0"`
	// 调用是否成功
	Success bool `gorm:"column:success;type:boolean;default:false;index:idx_llm_call_success"`
	// 调用状态字符串：success / error
	Status string `gorm:"column:status;type:varchar(50);not null;default:''"`
	// 失败时的错误信息
	Message string `gorm:"column:message;type:text"`
	// 调用方类型，如 worker、api、skill
	CallerType string `gorm:"column:caller_type;type:varchar(64);not null;index:idx_llm_call_caller"`
	// 业务请求标识，用于串联链路上的 LLM 调用与业务事件
	ReqID string `gorm:"column:req_id;type:varchar(255);default:''"`
	// 追踪ID
	TraceID string `gorm:"column:trace_id;type:varchar(255);not null;default:''"`
	// 重试次数
	RetryTimes int64 `gorm:"column:retry_times;type:bigint;default:1"`
	// 输入长度
	InputLen int `gorm:"column:input_len;type:integer;default:0"`
	// 输出长度
	OutputLen int `gorm:"column:output_len;type:integer;default:0"`
	// 输入是否被截断
	InputTruncated bool `gorm:"column:input_truncated;type:boolean;default:false"`
	// 输出是否被截断
	OutputTruncated bool `gorm:"column:output_truncated;type:boolean;default:false"`
	// 客户端IP
	ClientIP string `gorm:"column:client_ip;type:varchar(64);default:''"`
	// 项目ID，关联 lework_lework_project 表
	ProjectID uint `gorm:"column:project_id;type:integer;not null;default:0;index:idx_llm_call_project"`
	// 会话ID，关联 lework_lework_session 表
	SessionID uint `gorm:"column:session_id;type:integer;not null;default:0;index:idx_llm_call_session"`
	// 消息ID，关联 lework_lework_session_message 表
	MessageID uint `gorm:"column:message_id;type:integer;not null;default:0"`
	// 数字助理ID，关联 lework_lework_assistant 表
	AssistantID uint `gorm:"column:assistant_id;type:integer;not null;default:0;index:idx_llm_call_assistant"`
	// 用户ID，关联 lework_lework_user 表
	Uin uint `gorm:"column:uin;type:integer;not null;default:0;index:idx_llm_call_uin"`
	// 模型调用时的完整输入数据（messages / prompt）
	Input string `gorm:"column:input;type:text"`
	// 模型调用时的完整输出数据（response body / assistant message）
	Output string `gorm:"column:output;type:text"`
	// 调用开始时间
	StartedAt time.Time `gorm:"column:started_at;type:timestamp;not null;index:idx_llm_call_org_started;sort:desc"`
	// 调用结束时间
	FinishedAt time.Time `gorm:"column:finished_at;type:timestamp"`
}

// TableName 指定LLMHistory对应的数据库表名
func (LLMHistory) TableName() string {
	return TableNameLLMHistory
}
