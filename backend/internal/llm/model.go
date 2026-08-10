// Package llm 提供统一的 LLM 模型管理、调用、调用记录和用量信息能力。
package llm

import (
	"context"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

// SchemaMessage 是 eino schema.Message 的类型别名，供其他包在不直接 import eino 的情况下使用。
type SchemaMessage = schema.Message

// CallerType 常量定义调用方类型。
const (
	CallerTypeHTTPProxy = "http_proxy"
	CallerTypeWorker    = "worker"
	CallerTypeAPI       = "api"
)

// ModelConfig 表示一个可在业务中调用的 LLM 模型配置，
// 对应持久化层 types.LLMModel 的领域类型，包含解密后的 APIKey。
type ModelConfig struct {
	ID           uint
	OrgID        uint
	Code         string
	Name         string
	Description  string
	Provider     string
	ModelName    string
	BaseURL      string
	BaseURLHasV1 bool
	APIKey       string
	MaxTokens    int
	Temperature  float64
	TimeoutSec   int
	Status       string
	IsDefault    bool
	IsSystem     bool
	Config       map[string]any
	// Vision 表示该模型是否支持图片（多模态）输入，来源为 Config["vision"]。
	Vision    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CallRecord 记录一次 LLM 调用的完整信息，用于审计和用量统计。
type CallRecord struct {
	ID              uint
	OrgID           uint
	ModelID         uint
	Provider        string
	ModelName       string
	ModelProvider   string
	EntryProtocol   string
	IsStream        bool
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	LatencyMS       int64
	PromptTokens    int64
	CacheHitTokens  int64
	CacheMissTokens int64
	StatusCode      int
	Success         bool
	Status          string
	Message         string
	CallerType      string
	ReqID           string
	TraceID         string
	RetryTimes      int64
	InputLen        int
	OutputLen       int
	InputTruncated  bool
	OutputTruncated bool
	ClientIP        string
	ProjectID       uint
	SessionID       uint
	MessageID       uint
	AssistantID     uint
	Uin             uint
	Input           string
	Output          string
	StartedAt       time.Time
	FinishedAt      time.Time
}

// Usage 表示一次 LLM 调用的 token 用量信息。
type Usage struct {
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	PromptTokens    int64
	CacheHitTokens  int64
	CacheMissTokens int64
}

// Message 表示调用 LLM 时的单条消息。
type Message struct {
	Role    string
	Content string
}

// ToolSpec 表示传递给 LLM 的工具定义。
type ToolSpec struct {
	Name        string
	Description string
	JSONSchema  map[string]any
}

// CallRequest 表示一次 LLM 调用请求的参数。
type CallRequest struct {
	ModelID         uint
	Messages        []Message
	Tools           []ToolSpec
	SystemPrompt    string
	MaxTokens       *int
	Temperature     *float64
	ResponseFormat  *einoopenai.ChatCompletionResponseFormat
	ReasoningEffort einoopenai.ReasoningEffortLevel
	IsStream        bool
	CallerType      string
	ReqID           string
	ProjectID       uint
	SessionID       uint
	MessageID       uint
	AssistantID     uint
	Uin             uint
}

// StreamSink 定义流式调用时的回调接口，用于接收增量内容。
type StreamSink interface {
	EmitMessageDelta(ctx context.Context, content string) error
	EmitReasoningDelta(ctx context.Context, content string) error
}

// CallResult 表示一次 LLM 调用的返回结果。
//
// Message 字段使用 eino schema.Message 以兼容多模型统一抽象。
type CallResult struct {
	Message         *schema.Message
	Usage           *Usage
	Record          *CallRecord
	RawResponseBody []byte
}

// --- Manager 请求/响应类型 ---

// CreateRequest 表示创建 LLM 模型配置的请求参数。
type CreateRequest struct {
	Name        string
	Description string
	Provider    string
	Model       string
	BaseURL     string
	APIKey      string
	Status      string
	IsDefault   bool
	Config      map[string]any
}

// UpdateRequest 表示更新 LLM 模型配置的请求参数。
// 指针类型的字段仅在非 nil 时表示需要更新。
type UpdateRequest struct {
	Name        string
	Description *string
	Provider    string
	Model       string
	BaseURL     *string
	APIKey      *string
	Status      string
	IsDefault   *bool
	Config      *map[string]any
}

// ListRequest 表示查询 LLM 模型配置列表的请求参数。
type ListRequest struct {
	Provider *string
	Status   *string
	Keyword  *string
	Offset   int
	Limit    int
}

// ListModelResult 表示 LLM 模型配置列表查询结果。
type ListModelResult struct {
	Total  int64
	Offset int
	Limit  int
	Items  []*ModelConfig
}

// TestRequest 表示测试 LLM 模型连通性的请求参数。
// 当 ID 非 nil 时按已有配置测试，否则使用其余字段指定的临时配置测试。
type TestRequest struct {
	ID       *uint
	Code     string
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

// TestResult 表示 LLM 模型连通性测试的结果。
type TestResult struct {
	Success      bool
	Message      string
	Endpoint     string
	LatencyMS    int64
	BaseURLHasV1 bool
}
