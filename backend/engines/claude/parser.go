package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// ——— stream-json 类型 ———

type streamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Message   *streamMessage `json:"message,omitempty"`
	Result    string         `json:"result,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Usage     *streamUsage   `json:"usage,omitempty"`
	// control_request 相关字段
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
}

type streamMessage struct {
	ID      string          `json:"id,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content []streamContent `json:"content"`
}

type streamContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type streamUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
}

// ——— 解析状态 ———

type claudeStreamState struct {
	result             string
	isError            bool
	lastAssistantText  string
	toolNames          map[string]string
	pendingTaskCreates map[string]events.RuntimeTodoItem
}

// ——— stdout 扫描 ———

func scanClaudeStdout(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- events.Event, state *claudeStreamState) {
	engines.ScanJSONLines(r, func(line string) bool {
		for _, event := range parseClaudeLineEvents(line, state) {
			if event.Type == "" {
				continue
			}
			if !sendEvent(ctx, evtChan, event) {
				return false
			}
		}
		return true
	})
}

func parseClaudeLine(line string, state *claudeStreamState) events.Event {
	parsed := parseClaudeLineEvents(line, state)
	if len(parsed) == 0 {
		return events.Event{}
	}
	return parsed[0]
}

// ——— 事件解析 ———

func parseClaudeLineEvents(line string, state *claudeStreamState) []events.Event {
	logs.Infof("Parse Claude line: %s", line)
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var event streamEvent
	if json.Unmarshal([]byte(line), &event) != nil {
		return []events.Event{*events.NewMessageDelta("", line)}
	}
	switch event.Type {
	case "system":
		if event.Subtype == "init" && strings.TrimSpace(event.SessionID) != "" {
			return []events.Event{{
				Type:    engines.EventProviderSessionStarted,
				Content: strings.TrimSpace(event.SessionID),
			}}
		}
		return nil
	case "assistant":
		return parseAssistantEvent(&event, state)
	case "user":
		return parseUserEvent(&event, state)
	case "result":
		state.result = event.Result
		state.isError = event.IsError
		if event.IsError || event.Result == "" {
			return nil
		}
		return []events.Event{*events.NewMessageResult(event.Result, usagePayloadFromClaudeUsage(event.Usage))}
	case "control_request":
		return parseControlRequest(&event)
	}
	return nil
}

func parseAssistantEvent(event *streamEvent, state *claudeStreamState) []events.Event {
	if event.Message == nil {
		return nil
	}
	var parsed []events.Event
	var b strings.Builder
	messageID := event.Message.ID
	for _, block := range event.Message.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				state.lastAssistantText = block.Text
				b.WriteString(block.Text)
			}
		case "thinking":
			if block.Thinking != "" {
				if b.Len() > 0 {
					parsed = append(parsed, *events.NewMessageDelta(messageID, b.String()))
					b.Reset()
				}
				parsed = append(parsed, *events.NewReasoningDelta(messageID, block.Thinking))
			}
		case "tool_use":
			if b.Len() > 0 {
				parsed = append(parsed, *events.NewMessageDelta(messageID, b.String()))
				b.Reset()
			}
			if isClaudeTodoTool(block.Name) {
				rememberClaudeToolName(block, state)
			} else {
				parsed = append(parsed, claudeToolCallStartedEvent(block, state))
			}
			parsed = append(parsed, claudeTodoEventsFromToolUse(block, state)...)
		}
	}
	if b.Len() > 0 {
		parsed = append(parsed, *events.NewMessageDelta(messageID, b.String()))
	}
	return parsed
}

func parseUserEvent(event *streamEvent, state *claudeStreamState) []events.Event {
	if event.Message == nil {
		return nil
	}
	var parsed []events.Event
	for _, block := range event.Message.Content {
		if block.Type == "tool_result" {
			if !isClaudeTodoTool(claudeToolName(block.ToolUseID, state)) {
				parsed = append(parsed, claudeToolCallCompletedEvent(block, state))
			}
			parsed = append(parsed, claudeTodoEventsFromToolResult(block, state)...)
		}
	}
	return parsed
}

func parseControlRequest(event *streamEvent) []events.Event {
	if event.ToolUseID == "" || event.Name == "" {
		return nil
	}
	desc := fmt.Sprintf("%s: %s", event.Name, summarizeInput(event.Input))
	payload := events.ApprovalRequestPayload{
		RequestID:   event.ToolUseID,
		Engine:      "claude",
		ActionType:  "tool_use",
		Description: desc,
		ToolCallID:  event.ToolUseID,
		ToolName:    event.Name,
		Arguments:   event.Input,
	}
	return []events.Event{*events.NewApprovalRequested(payload)}
}

// summarizeInput 为审批提示生成可读的工具输入摘要。
func summarizeInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	// 尝试 Claude Code 工具的常见键名
	for _, key := range []string{"command", "file_path", "path", "content", "url"} {
		if v, ok := input[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 120 {
				s = s[:120] + "..."
			}
			return s
		}
	}
	return ""
}
