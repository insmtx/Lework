package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/bytedance/sonic"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
	"github.com/ygpkg/yg-go/logs"
)

// ——— stream-json types ———

type streamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Message   *streamMessage `json:"message,omitempty"`
	Event     *innerEvent    `json:"event,omitempty"`
	Result    string         `json:"result,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Usage     *streamUsage   `json:"usage,omitempty"`
	// control_request fields (top-level and nested request object)
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

// ——— parse state ———

type claudeStreamState struct {
	result               string
	isError              bool
	sessionID            string
	usage                *agent.Usage
	lastAssistantText    string
	toolNames            map[string]string
	pendingTaskCreates   map[string]agent.RuntimeTodoItem
	messageIDs           *cli.MessageIDMapper
	currentTextMessageID string
	lastTextMessageID    string
	emittedTextByMessage map[string]string
	closeStdin           func() // close stdin on result event to let Claude process exit
}

// ——— stdout scanning ———

func scanClaudeStdout(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- agent.NodeEvent, state *claudeStreamState) {
	runtimeprocess.ScanJSONLines(r, func(line string) bool {
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

// ——— event parsing ———

func parseClaudeLineEvents(line string, state *claudeStreamState) []agent.NodeEvent {
	logs.Debugf("Parse Claude line: %s", line)
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var event streamEvent
	if sonic.Unmarshal([]byte(line), &event) != nil {
		return []agent.NodeEvent{agent.NewMessageUpdateEvent("", line)}
	}
	switch event.Type {
	case "system":
		endClaudeTextMessage(state)
		if event.Subtype == "init" && strings.TrimSpace(event.SessionID) != "" {
			state.sessionID = strings.TrimSpace(event.SessionID)
			return []agent.NodeEvent{agent.NewAgentStartEvent(strings.TrimSpace(event.SessionID))}
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
		state.usage = usagePayloadFromClaudeUsage(event.Usage)
		if state.closeStdin != nil {
			state.closeStdin()
		}
		if event.IsError || event.Result == "" {
			return nil
		}
		return []agent.NodeEvent{agent.NewMessageEndEvent(event.Result, state.usage)}
	case "control_request":
		endClaudeTextMessage(state)
		return parseControlRequest(&event)
	}
	endClaudeTextMessage(state)
	return nil
}

func parseStreamEvent(event *streamEvent, state *claudeStreamState) []agent.NodeEvent {
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
	return []agent.NodeEvent{agent.NewMessageUpdateEvent(messageID, text)}
}

func parseAssistantEvent(event *streamEvent, state *claudeStreamState) []agent.NodeEvent {
	if event.Message == nil {
		return nil
	}
	var parsed []agent.NodeEvent
	for _, block := range event.Message.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				state.lastAssistantText = block.Text
				messageID, delta := claudeAssistantTextDelta(state, block.Text)
				if delta != "" {
					parsed = append(parsed, agent.NewMessageUpdateEvent(messageID, delta))
				}
			}
		case "thinking":
			if block.Thinking != "" {
				messageID := firstNonEmptyString(state.currentTextMessageID, event.Message.ID)
				endClaudeTextMessage(state)
				parsed = append(parsed, agent.NewReasoningUpdateEvent(messageID, block.Thinking))
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
		return cli.NewMessageIDMapper().StartNew()
	}
	if state.currentTextMessageID != "" {
		return state.currentTextMessageID
	}
	if state.messageIDs == nil {
		state.messageIDs = cli.NewMessageIDMapper()
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

func parseUserEvent(event *streamEvent, state *claudeStreamState) []agent.NodeEvent {
	if event.Message == nil {
		return nil
	}
	var parsed []agent.NodeEvent
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

func parseControlRequest(event *streamEvent) []agent.NodeEvent {
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
	reqID := firstNonEmptyString(event.RequestID, toolUseID)
	desc := fmt.Sprintf("%s: %s", toolName, summarizeInput(input))
	return []agent.NodeEvent{agent.NewApprovalRequestedEvent(agent.ApprovalRequestedPayload{
		RequestID:   reqID,
		ToolName:    toolName,
		ToolCallID:  toolUseID,
		Description: desc,
		Arguments:   agent.MarshalRawJSON(input),
		Metadata:    map[string]string{"engine": "claude"},
	})}
}

// summarizeInput generates a readable summary of tool input for approval prompts.
func summarizeInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
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
