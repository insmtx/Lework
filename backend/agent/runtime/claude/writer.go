package claude

import (
	"fmt"
	"github.com/bytedance/sonic"
	"io"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
)

// buildStreamUserMessage 构建 stream-json 格式的用户消息。
func buildStreamUserMessage(prompt string) string {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	}
	encoded, _ := sonic.Marshal(payload)
	return string(encoded)
}

// ——— ApprovalResponder ———

// claudeApprovalResponder 实现 engines.ApprovalResponder，将决策写入 claude stdin。
type claudeApprovalResponder struct {
	stdinW io.Writer
}

// WriteDecision 将审批决策转换为 claude control_response JSON 并写入 stdin。
// Claude CLI 的 Zod schema：
//
//	allow → {behavior: "allow", updatedInput: {...}}
//	deny  → {behavior: "deny",  message: "reason"}
func (r *claudeApprovalResponder) WriteDecision(requestID string, action string) error {
	if r.stdinW == nil {
		return fmt.Errorf("claude stdin writer is nil")
	}
	var responseBody map[string]any
	if action == provider.ApprovalActionDeny {
		responseBody = map[string]any{
			"behavior": "deny",
			"message":  "Permission denied by user",
		}
	} else {
		responseBody = map[string]any{
			"behavior":     "allow",
			"updatedInput": map[string]any{},
		}
	}

	payload := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   responseBody,
		},
	}
	encoded, _ := sonic.Marshal(payload)
	_, err := fmt.Fprintln(r.stdinW, string(encoded))
	return err
}

var _ provider.ApprovalResponder = (*claudeApprovalResponder)(nil)

// ——— 用量 ———

func usagePayloadFromClaudeUsage(usage *streamUsage) *agent.Usage {
	if usage == nil {
		return nil
	}
	inputTokens := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	outputTokens := usage.OutputTokens
	totalTokens := inputTokens + outputTokens
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}
	return &agent.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
}

// ——— 错误内容 ———

func claudeFailureContent(err error, state *claudeStreamState, stderrText string) string {
	detail := ""
	if state != nil {
		detail = strings.TrimSpace(state.result)
	}
	if detail == "" {
		detail = strings.TrimSpace(stderrText)
	}
	if err == nil {
		return detail
	}
	if detail == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s (%v)", detail, err)
}
