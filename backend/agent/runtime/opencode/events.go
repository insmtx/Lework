package opencode

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================================
// SSE 消息事件解析
// ============================================================================

var filteredToolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^question$`),
	regexp.MustCompile(`^todowrite$`),
	regexp.MustCompile(`artifact_declare`),
}

// handleSSEEvent 解析 SSE 事件并将消息相关事件转换为引擎事件。
// 消息事件包括：文本增量、工具调用、推理内容等。
func (st *runState) handleSSEEvent(event sseEvent) {
	logs.Debugf("[opencode] SSE event: type=%s id=%s props=%+v", event.Type, event.ID, event.Properties)

	st.mu.Lock()
	defer st.mu.Unlock()

	propsJSON, err := json.Marshal(event.Properties)
	if err != nil {
		return
	}

	switch event.Type {
	case "session.next.text.delta":
		var props textDeltaProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if props.Delta != "" {
			msgID := props.AssistantMessageID
			if msgID == "" {
				msgID = st.messageID
			}
			emitMessageDelta(st.evtChan, msgID, props.Delta)
		}

	case "session.next.text.started":
		// 仅记录 textID，不产生事件

	case "session.next.text.ended":
		var props textEndedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		st.lastTextEnded = props.Text

	case "session.next.tool.called":
		var props toolCalledProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if isFilteredToolName(props.Tool) {
			st.markFilteredToolCall(props.CallID)
			return
		}
		sendEventPayloadTo(st.evtChan, events.EventToolCallStarted, events.ToolCallPayload{
			ToolCallID: props.CallID,
			Name:       props.Tool,
			Arguments:  events.MarshalRaw(props.Input),
		})

	case "session.next.tool.success":
		var props toolSuccessProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if isFilteredToolName(props.Tool) || st.isFilteredToolCall(props.CallID) {
			return
		}
		sendEventPayloadTo(st.evtChan, events.EventToolCallCompleted, events.ToolCallResultPayload{
			ToolCallID: props.CallID,
			Name:       props.Tool,
			Result:     events.MarshalRaw(props.Result),
		})

	case "session.next.tool.failed":
		var props toolFailedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if isFilteredToolName(props.Tool) || st.isFilteredToolCall(props.CallID) {
			return
		}
		sendEventPayloadTo(st.evtChan, events.EventToolCallFailed, events.ToolCallResultPayload{
			ToolCallID: props.CallID,
			Name:       props.Tool,
			Error:      props.Error.Message,
			IsError:    true,
		})

	case "session.next.reasoning.delta":
		var props reasoningDeltaProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if props.Delta != "" {
			msgID := props.AssistantMessageID
			if msgID == "" {
				msgID = st.messageID
			}
			evt := events.NewReasoningDelta(msgID, props.Delta)
			sendEventDirect(st.evtChan, evt)
		}

	case "session.next.step.ended":
		var props stepEndedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		st.tokenUsage = &agent.Usage{
			InputTokens:  props.Tokens.Input,
			OutputTokens: props.Tokens.Output,
			TotalTokens:  props.Tokens.Input + props.Tokens.Output,
		}

	case "session.next.shell.started":
		// 以 message delta 展示
		emitMessageDelta(st.evtChan, st.messageID, "[shell] 正在执行命令...")

	case "permission.asked":
		var props permissionAskedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}

		// 构建描述文本
		desc := props.Permission
		if len(props.Patterns) > 0 {
			desc = props.Permission + ": " + strings.Join(props.Patterns, ", ")
		}

		// 提取 tool_call_id（如有）
		toolCallID := ""
		if props.Tool != nil {
			toolCallID = props.Tool.CallID
		}

		payload := events.ApprovalRequestPayload{
			RequestID:   props.ID,
			ToolName:    props.Permission,
			ToolCallID:  toolCallID,
			Description: desc,
			Arguments:   events.MarshalRaw(map[string]any{"patterns": props.Patterns}),
			Metadata:    map[string]string{"engine": "opencode"},
		}
		sendEventPayloadTo(st.evtChan, events.EventApprovalRequested, payload)

	case "session.next.agent.switched":
		// 记录但不产生事件
		logs.Infof("OpenCode agent switched: %s", string(propsJSON))

	case "question.asked", "question.v2.asked":
		var props questionAskedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}

		// 映射 question items
		questions := make([]events.QuestionItem, 0, len(props.Questions))
		for _, q := range props.Questions {
			options := make([]events.QuestionOption, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, events.QuestionOption{
					Label:       o.Label,
					Description: o.Description,
				})
			}
			questions = append(questions, events.QuestionItem{
				Question:    q.Question,
				Header:      q.Header,
				Options:     options,
				MultiSelect: q.Multiple,
				Custom:      q.Custom,
			})
		}

		toolCallID := ""
		messageID := ""
		if props.Tool != nil {
			toolCallID = props.Tool.CallID
			messageID = props.Tool.MessageID
		}

		payload := events.QuestionRequestPayload{
			RequestID:  props.ID,
			SessionID:  props.SessionID,
			Questions:  questions,
			ToolCallID: toolCallID,
			MessageID:  messageID,
		}
		sendEventDirect(st.evtChan, events.NewQuestionAsked(payload))

	case "todo.updated":
		var props todoUpdatedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		items := convertOpenCodeTodoItems(props.Todos)
		if len(items) == 0 {
			return
		}
		sendEventDirect(st.evtChan, events.NewTodoUpdated(items))

	case "session.next.model.switched":
		// 记录但不产生事件
		logs.Infof("OpenCode model switched: %s", string(propsJSON))

	case "server.connected":
		logs.Infof("OpenCode SSE connected")

	case "server.heartbeat":
		// 忽略心跳

	default:
	}
}

func isFilteredToolName(toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	for _, pattern := range filteredToolPatterns {
		if pattern.MatchString(toolName) {
			return true
		}
	}
	return false
}

func (st *runState) markFilteredToolCall(callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if st.filteredToolCalls == nil {
		st.filteredToolCalls = make(map[string]struct{})
	}
	st.filteredToolCalls[callID] = struct{}{}
}

func (st *runState) isFilteredToolCall(callID string) bool {
	callID = strings.TrimSpace(callID)
	if callID == "" || st.filteredToolCalls == nil {
		return false
	}
	_, ok := st.filteredToolCalls[callID]
	return ok
}

// ============================================================================
// todo.updated 转换
// ============================================================================

// convertOpenCodeTodoItems 将 OpenCode 格式的 todo 列表转换为内部统一格式。
// 无 id 的条目按列表位置生成稳定 ID（todo_1, todo_2 ...）。
// content 为空的条目会被忽略。
func convertOpenCodeTodoItems(todos []opencodeTodoItem) []events.RuntimeTodoItem {
	items := make([]events.RuntimeTodoItem, 0, len(todos))
	for i, t := range todos {
		if strings.TrimSpace(t.Content) == "" {
			continue
		}
		id := t.ID
		if id == "" {
			id = "todo_" + strconv.Itoa(i+1)
		}
		items = append(items, events.RuntimeTodoItem{
			ID:       id,
			Title:    t.Content,
			Status:   t.Status,
			Priority: t.Priority,
		})
	}
	return items
}
