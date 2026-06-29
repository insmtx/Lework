package assistantcontext

import (
	"context"
	"fmt"
	"strings"
	"time"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/ygpkg/yg-go/logs"
)

// ContextBuilder 统一为内部 Agent 和外部 CLI 构建运行上下文。
type ContextBuilder struct {
	SessionMessages SessionMessageProvider
	Memory          MemoryReader
}

// MemoryReader provides the persisted memory prompt block.
type MemoryReader interface {
	BuildPromptBlock(ctx context.Context) (string, error)
}

// NewContextBuilder 创建统一上下文构建器。
func NewContextBuilder(cfg ContextBuilder) *ContextBuilder {
	return &ContextBuilder{
		SessionMessages: cfg.SessionMessages,
		Memory:          cfg.Memory,
	}
}

// Prepare 克隆请求并注入 memory、skill、session 上下文。
func (b *ContextBuilder) Prepare(ctx context.Context, req *assistantdomain.RunRequest) (*assistantdomain.RunRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request context is required")
	}

	startedAt := time.Now()
	logs.InfoContextf(ctx, "Agent context prepare started: run_id=%s trace_id=%s assistant_id=%s conversation_id=%s input_type=%s messages=%d attachments=%d",
		req.RunID,
		req.TraceID,
		req.Assistant.ID,
		req.Conversation.ID,
		req.Input.Type,
		len(req.Input.Messages),
		len(req.Input.Attachments),
	)

	cloned := CloneRequest(req)
	if b.SessionMessages != nil {
		if err := b.SessionMessages.Prepare(ctx, cloned); err != nil {
			logs.WarnContextf(ctx, "Agent session context prepare failed: run_id=%s trace_id=%s error=%v",
				req.RunID, req.TraceID, err)
			return nil, err
		}
	}
	// 解析并加载用户显式调用的 skill
	if err := ApplyInvokedSkills(ctx, cloned); err != nil {
		logs.WarnContextf(ctx, "Agent invoke skills failed: run_id=%s trace_id=%s error=%v",
			req.RunID, req.TraceID, err)
		return nil, err
	}
	systemPrompt, err := b.BuildSystemPrompt(ctx, cloned)
	if err != nil {
		logs.WarnContextf(ctx, "Agent context build system prompt failed: run_id=%s trace_id=%s error=%v",
			req.RunID, req.TraceID, err)
		return nil, err
	}

	cloned.SystemPrompt = systemPrompt
	logs.InfoContextf(ctx, "Agent context prepare completed: run_id=%s trace_id=%s system_prompt_len=%d elapsed=%s",
		cloned.RunID, cloned.TraceID, len(cloned.SystemPrompt), time.Since(startedAt))
	return cloned, nil
}

// CloneRequest 深拷贝一份 RequestContext，防止 Prepare 过程污染原始请求。
func CloneRequest(req *assistantdomain.RunRequest) *assistantdomain.RunRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Assistant.Skills = append([]string{}, req.Assistant.Skills...)
	cloned.Assistant.Tools = append([]string{}, req.Assistant.Tools...)
	cloned.Conversation.Messages = append([]assistantdomain.InputMessage{}, req.Conversation.Messages...)
	cloned.Input.Messages = append([]assistantdomain.InputMessage{}, req.Input.Messages...)
	cloned.Input.Attachments = append([]assistantdomain.Attachment{}, req.Input.Attachments...)
	cloned.Capability.AllowedTools = append([]string{}, req.Capability.AllowedTools...)
	cloned.SystemPrompt = req.SystemPrompt
	return &cloned
}

// filterEmpty 过滤切片中的空白字符串，返回所有非空值。
func filterEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// TruncateForPrompt 将字符串截断到指定长度，超长时追加截断标记。
func TruncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}
