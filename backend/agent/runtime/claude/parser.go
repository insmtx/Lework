package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/bytedance/sonic"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	"github.com/ygpkg/yg-go/logs"
)

// ——— stream-json 类型 ———

type streamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Message   *streamMessage `json:"message,omitempty"`
	Event     *innerEvent    `json:"event,omitempty"`
	Result    string         `json:"result,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Usage     *streamUsage   `json:"usage,omitempty"`
	// control_request 相关字段（位于顶层和 request 嵌套对象）
	RequestID string         `json:"request_id,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Request   *controlReq    `json:"request,omitempty"`
}

type innerEvent struct {
	Type  string       `json:"type"`
	Index int          `json:"index,omitempty"`
	Delta *streamDelta `json:"delta,omitempty"`
}

type streamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type controlReq struct {
	Subtype   string         `json:"subtype"`
	ToolName  string         `json:"tool_name"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
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
	result               string
	isError              bool
	lastAssistantText    string
	toolNames            map[string]string
	pendingTaskCreates   map[string]events.RuntimeTodoItem
	messageIDs           *events.MessageIDMapper
	currentTextMessageID string
	lastTextMessageID    string
	emittedTextByMessage map[string]string
	closeStdin           func() // result 事件时调用，关闭 stdin 让 Claude 进程退出
}

// ——— stdout 扫描 ———

func scanClaudeStdout(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- agent.Event, state *claudeStreamState) {
	provider.ScanJSONLines(r, func(line string) bool {
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

// ——— 事件解析 ———

func parseClaudeLineEvents(line string, state *claudeStreamState) []agent.Event {
	logs.Debugf("Parse Claude line: %s", line)
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var event streamEvent
	if sonic.Unmarshal([]byte(line), &event) != nil {
		return []agent.Event{*events.NewMessageDelta("", line)}
	}
	switch event.Type {
	case "system":
		endClaudeTextMessage(state)
		if event.Subtype == "init" && strings.TrimSpace(event.SessionID) != "" {
			return []agent.Event{{
				Type:    events.EventProviderSessionStarted,
				Content: strings.TrimSpace(event.SessionID),
			}}
		}
		return nil
	case "stream_event":
		return parseStreamEvent(&event, state)
	case "assistant":
		return parseAssistantEvent(&event, state)
	case "user":
		endClaudeTextMessage(state)
		return parseUserEvent(&event, state)
	case "result":
		endClaudeTextMessage(state)
		state.result = event.Result
		state.isError = event.IsError
		if state.closeStdin != nil {
			state.closeStdin()
		}
		if event.IsError || event.Result == "" {
			return nil
		}
		return []agent.Event{*events.NewMessageResult(event.Result, usagePayloadFromClaudeUsage(event.Usage))}
	case "control_request":
		endClaudeTextMessage(state)
		return parseControlRequest(&event)
	}
	endClaudeTextMessage(state)
	return nil
}

func parseStreamEvent(event *streamEvent, state *claudeStreamState) []agent.Event {
	if event.Event == nil || event.Event.Type != "content_block_delta" || event.Event.Delta == nil ||
		event.Event.Delta.Type != "text_delta" {
		endClaudeTextMessage(state)
		return nil
	}
	text := event.Event.Delta.Text
	if text == "" {
		return nil
	}
	messageID := currentOrStartClaudeTextMessage(state)
	rememberClaudeEmittedText(state, messageID, text)
	return []agent.Event{*events.NewMessageDelta(messageID, text)}
}

func parseAssistantEvent(event *streamEvent, state *claudeStreamState) []agent.Event {
	if event.Message == nil {
		return nil
	}
	var parsed []agent.Event
	for _, block := range event.Message.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				state.lastAssistantText = block.Text
				messageID, delta := claudeAssistantTextDelta(state, block.Text)
				if delta != "" {
					parsed = append(parsed, *events.NewMessageDelta(messageID, delta))
				}
			}
		case "thinking":
			if block.Thinking != "" {
				messageID := firstNonEmptyString(state.currentTextMessageID, event.Message.ID)
				endClaudeTextMessage(state)
				parsed = append(parsed, *events.NewReasoningDelta(messageID, block.Thinking))
			}
		case "tool_use":
			endClaudeTextMessage(state)
			if isClaudeTodoTool(block.Name) {
				rememberClaudeToolName(block, state)
			} else {
				parsed = append(parsed, claudeToolCallStartedEvent(block, state))
			}
			parsed = append(parsed, claudeTodoEventsFromToolUse(block, state)...)
		default:
			endClaudeTextMessage(state)
		}
	}
	return parsed
}

func currentOrStartClaudeTextMessage(state *claudeStreamState) string {
	if state == nil {
		return events.NewMessageIDMapper().StartNew()
	}
	if state.currentTextMessageID != "" {
		return state.currentTextMessageID
	}
	if state.messageIDs == nil {
		state.messageIDs = events.NewMessageIDMapper()
	}
	state.currentTextMessageID = state.messageIDs.StartNew()
	state.lastTextMessageID = state.currentTextMessageID
	return state.currentTextMessageID
}

func endClaudeTextMessage(state *claudeStreamState) {
	if state == nil {
		return
	}
	state.currentTextMessageID = ""
}

func rememberClaudeEmittedText(state *claudeStreamState, messageID string, text string) {
	if state == nil || messageID == "" || text == "" {
		return
	}
	if state.emittedTextByMessage == nil {
		state.emittedTextByMessage = make(map[string]string)
	}
	state.emittedTextByMessage[messageID] += text
}

func claudeAssistantTextDelta(state *claudeStreamState, cumulativeText string) (string, string) {
	messageID := ""
	if state != nil {
		messageID = state.currentTextMessageID
		if messageID == "" && state.lastTextMessageID != "" {
			last := state.emittedTextByMessage[state.lastTextMessageID]
			if last != "" && (strings.HasPrefix(cumulativeText, last) || strings.HasPrefix(last, cumulativeText)) {
				messageID = state.lastTextMessageID
			}
		}
	}
	if messageID == "" {
		messageID = currentOrStartClaudeTextMessage(state)
	}
	if messageID == "" {
		return "", cumulativeText
	}

	if state == nil {
		return messageID, cumulativeText
	}
	if state.emittedTextByMessage == nil {
		state.emittedTextByMessage = make(map[string]string)
	}
	last := state.emittedTextByMessage[messageID]
	if strings.HasPrefix(cumulativeText, last) {
		delta := cumulativeText[len(last):]
		state.emittedTextByMessage[messageID] = cumulativeText
		return messageID, delta
	}
	if strings.HasPrefix(last, cumulativeText) {
		return messageID, ""
	}
	state.emittedTextByMessage[messageID] = cumulativeText
	return messageID, cumulativeText
}

func parseUserEvent(event *streamEvent, state *claudeStreamState) []agent.Event {
	if event.Message == nil {
		return nil
	}
	var parsed []agent.Event
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

func parseControlRequest(event *streamEvent) []agent.Event {
	// 从 request 嵌套对象提取字段（新版 claude CLI 格式）
	toolUseID := event.ToolUseID
	toolName := event.Name
	input := event.Input
	if event.Request != nil {
		if toolUseID == "" {
			toolUseID = event.Request.ToolUseID
		}
		if toolName == "" {
			toolName = firstNonEmptyString(event.Request.ToolName, event.Request.Name)
		}
		if len(input) == 0 {
			input = event.Request.Input
		}
	}
	if toolUseID == "" || toolName == "" {
		return nil
	}
	// request_id 用于 control_response 回写匹配，必须是 UUID 格式
	reqID := firstNonEmptyString(event.RequestID, toolUseID)
	desc := fmt.Sprintf("%s: %s", toolName, summarizeInput(input))
	payload := events.ApprovalRequestPayload{
		RequestID:   reqID,
		ToolName:    toolName,
		ToolCallID:  toolUseID,
		Description: desc,
		Arguments:   events.MarshalRaw(input),
		Metadata:    map[string]string{"engine": "claude"},
	}
	return []agent.Event{*events.NewApprovalRequested(payload)}
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
