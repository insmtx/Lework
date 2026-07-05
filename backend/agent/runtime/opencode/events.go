package opencode

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================================
// SSE 消息事件解析
// ============================================================================

var filteredToolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^question$`),
	regexp.MustCompile(`^plan_exit$`),
	regexp.MustCompile(`^todowrite$`),
	regexp.MustCompile(`artifact_declare`),
}

const sessionErrorFallbackMessage = "session error"

func sessionErrorMessage(props sessionErrorProps) string {
	if msg := strings.TrimSpace(props.Error.Message); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(props.Error.Data.Message); msg != "" {
		return msg
	}
	return sessionErrorFallbackMessage
}

func usageFromOpenCodeTokens(tokens *v1Tokens) *agent.Usage {
	if tokens == nil {
		return agent.EnsureUsage(nil)
	}
	inputTokens := tokens.Input
	outputTokens := tokens.Output
	return agent.EnsureUsage(&agent.Usage{
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		CacheInputTokens:  tokens.Cache.Read,
		CacheOutputTokens: tokens.Cache.Write,
	})
}

// handleSSEEvent 解析 SSE 事件并将消息相关事件转换为引擎事件。
// 处理新版 OpenCode V2 session 发布的 V1 事件（message.part.* 等）。
func (st *runState) handleSSEEvent(ctx context.Context, event sseEvent) {
	logs.Debugf("[opencode] SSE event: type=%s id=%s props=%+v", event.Type, event.ID, event.Properties)

	st.mu.Lock()
	defer st.mu.Unlock()

	propsJSON, err := json.Marshal(event.Properties)
	if err != nil {
		return
	}

	switch event.Type {
	// ============================================================
	// message.updated — 消息元数据更新（最终 token usage 通常在这里出现）
	// ============================================================
	case "message.updated":
		var props messageUpdatedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if props.Info.ID != "" {
			st.messageID = props.Info.ID
		}
		if props.Info.Role == "assistant" {
			if usage := usageFromOpenCodeTokens(props.Info.Tokens); usage != nil {
				st.tokenUsage = usage
			}
		}

	// ============================================================
	// message.part.delta — 流式增量（文本 / 推理）
	// ============================================================
	case "message.part.delta":
		var props messagePartDeltaProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if props.Field != "text" || props.Delta == "" {
			return
		}
		if st.isReasoningPart(props.PartID) {
			// 推理内容增量，暂不产生事件；
			// 推理完成时通过 message.part.updated (reasoning) 发送完整文本。
			return
		}
		emitMessageDelta(st.evtChan, props.MessageID, props.Delta)

	// ============================================================
	// message.part.updated — Part 状态更新（文本、工具、步骤等）
	// ============================================================
	case "message.part.updated":
		var props messagePartUpdatedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		part := props.Part

		switch part.Type {
		case "text":
			// 记录 messageID（首次 text part 出现时）
			if st.messageID == "" && part.MessageID != "" {
				st.messageID = part.MessageID
			}
			// 完整文本（非 synthetic）
			if !isTrue(part.Synthetic) && part.Text != "" {
				st.lastTextEnded = part.Text
			}

		case "step-start":
			if part.MessageID != "" {
				st.messageID = part.MessageID
			}

		case "step-finish":
			if usage := usageFromOpenCodeTokens(part.Tokens); usage != nil {
				st.tokenUsage = usage
			}
			if part.Reason == "error" && st.runErr == "" {
				st.runErr = "step finished with error"
			}

		case "tool":
			if part.State == nil {
				return
			}
			callID := part.CallID
			toolName := part.Tool

			switch part.State.Status {
			case "pending":
				if isFilteredToolName(toolName) {
					st.markFilteredToolCall(callID, toolName)
				}

			case "running":
				if isFilteredToolName(toolName) || st.isFilteredToolCall(callID) {
					return
				}
				sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionStart, &agent.ToolExecutionStartPayload{
					ToolCallID: callID,
					Name:       toolName,
					Arguments:  agent.MarshalRawJSON(part.State.Input),
				})

			case "completed":
				if isFilteredToolName(toolName) || st.isFilteredToolCall(callID) {
					st.clearFilteredToolCall(callID)
					return
				}
				sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionEnd, &agent.ToolExecutionEndPayload{
					ToolCallID: callID,
					Name:       toolName,
					IsError:    false,
					Result:     agent.MarshalRawJSON(part.State.Output),
				})

			case "error":
				if isFilteredToolName(toolName) || st.isFilteredToolCall(callID) {
					st.clearFilteredToolCall(callID)
					return
				}
				toolErr := part.State.Error
				if toolErr == "" {
					toolErr = "tool execution failed"
				}
				sendEventPayloadTo(st.evtChan, agent.NodeEventToolExecutionEnd, &agent.ToolExecutionEndPayload{
					ToolCallID: callID,
					Name:       toolName,
					IsError:    true,
					Error:      toolErr,
				})
			}

		case "reasoning":
			// 记录 reasoning part，以便 message.part.delta 过滤
			st.markReasoningPart(part.ID)
			// reasoning-end：发送完整推理文本
			if part.Text != "" {
				msgID := part.MessageID
				if msgID == "" {
					msgID = st.messageID
				}
				evt := agent.NewReasoningUpdateEvent(msgID, part.Text)
				sendEventDirect(st.evtChan, evt)
			}
		}

	// ============================================================
	// permission.asked — 权限请求
	// ============================================================
	case "permission.asked":
		var props permissionAskedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}

		desc := props.Permission
		if len(props.Patterns) > 0 {
			desc = props.Permission + ": " + strings.Join(props.Patterns, ", ")
		}

		toolCallID := ""
		if props.Tool != nil {
			toolCallID = props.Tool.CallID
		}

		payload := agent.ApprovalRequestedPayload{
			RequestID:   props.ID,
			ToolName:    props.Permission,
			ToolCallID:  toolCallID,
			Description: desc,
			Arguments:   agent.MarshalRawJSON(map[string]any{"patterns": props.Patterns}),
			Metadata:    map[string]string{"engine": "opencode"},
		}
		sendEventPayloadTo(st.evtChan, agent.NodeEventApprovalRequested, &payload)

	// ============================================================
	// question.asked / question.v2.asked — 问题/确认
	// ============================================================
	case "question.asked", "question.v2.asked":
		var props questionAskedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}

		questions := make([]agent.QuestionItem, 0, len(props.Questions))
		for _, q := range props.Questions {
			options := make([]agent.QuestionOption, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, agent.QuestionOption{
					Label:       o.Label,
					Description: o.Description,
				})
			}
			questions = append(questions, agent.QuestionItem{
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

		isPlanConfirmation := st.filteredToolName(toolCallID) == "plan_exit"
		if isPlanConfirmation {
			logs.Infof("[plan] question.asked detected plan confirmation: session_id=%s request_id=%s tool_call_id=%s", props.SessionID, props.ID, toolCallID)

			path, displayPath, resolveErr := st.resolvePlanPath(questions)
			if resolveErr != nil {
				logs.WarnContextf(ctx, "[plan] question.asked resolve path failed, emitting confirmation with error: session_id=%s request_id=%s err=%s", props.SessionID, props.ID, resolveErr)
				payload := agent.QuestionAskedPayload{
					RequestID:       props.ID,
					SessionID:       props.SessionID,
					Questions:       planConfirmationQuestions(),
					ToolCallID:      toolCallID,
					MessageID:       messageID,
					InteractionType: "plan_confirmation",
					Metadata:        map[string]string{"plan_error": "resolve_failed"},
				}
				sendEventDirect(st.evtChan, agent.NewQuestionAskedEvent(payload))
				return
			}

			logs.Infof("[plan] question.asked emitting plan.ready: session_id=%s request_id=%s path=%s", props.SessionID, props.ID, path)
			sendEventDirect(st.evtChan, agent.NewPlanReadyEvent(path, displayPath, props.SessionID))
			questions = planConfirmationQuestions()
		}

		payload := agent.QuestionAskedPayload{
			RequestID:  props.ID,
			SessionID:  props.SessionID,
			Questions:  questions,
			ToolCallID: toolCallID,
			MessageID:  messageID,
		}
		if isPlanConfirmation {
			payload.InteractionType = "plan_confirmation"
		}
		sendEventDirect(st.evtChan, agent.NewQuestionAskedEvent(payload))

	// ============================================================
	// todo.updated — 待办事项更新
	// ============================================================
	case "todo.updated":
		var props todoUpdatedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		items := convertOpenCodeTodoItems(props.Todos)
		if len(items) == 0 {
			return
		}
		sendEventDirect(st.evtChan, agent.NewTodoUpdatedEvent(items))

	// ============================================================
	// session.updated — 会话元数据更新，作为 message.updated 缺失时的 usage 兜底
	// ============================================================
	case "session.updated":
		var props sessionUpdatedProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		if st.tokenUsage == nil {
			if usage := usageFromOpenCodeTokens(props.Info.Tokens); usage != nil {
				st.tokenUsage = usage
			}
		}

	// ============================================================
	// session.error — 会话错误
	// ============================================================
	case "session.error":
		var props sessionErrorProps
		if err := json.Unmarshal(propsJSON, &props); err != nil {
			return
		}
		errMsg := sessionErrorMessage(props)
		if st.runErr == "" {
			st.runErr = errMsg
		}
		logs.Errorf("[opencode] session error: session=%s error=%s", props.SessionID, errMsg)
		select {
		case <-st.sseTerminal:
		default:
			close(st.sseTerminal)
		}

	// ============================================================
	// session.idle — SSE 空闲信号，不再作为终态
	// ============================================================
	case "session.idle":
		logs.Debugf("[opencode] session idle (ignored for termination)")

	// ============================================================
	// 生命周期事件
	// ============================================================
	case "server.connected":
		logs.Infof("OpenCode SSE connected")

	case "server.heartbeat":
		// 忽略心跳

	default:
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func isTrue(b *bool) bool {
	return b != nil && *b
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

func (st *runState) markFilteredToolCall(callID, toolName string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if st.filteredToolCalls == nil {
		st.filteredToolCalls = make(map[string]string)
	}
	st.filteredToolCalls[callID] = strings.TrimSpace(toolName)
}

func (st *runState) isFilteredToolCall(callID string) bool {
	return st.filteredToolName(callID) != ""
}

func (st *runState) filteredToolName(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" || st.filteredToolCalls == nil {
		return ""
	}
	return st.filteredToolCalls[callID]
}

func (st *runState) clearFilteredToolCall(callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" || st.filteredToolCalls == nil {
		return
	}
	delete(st.filteredToolCalls, callID)
}

// markReasoningPart 标记 reasoning partID，用于 message.part.delta 区分文本和推理。
// 调用方必须持有 st.mu。
func (st *runState) markReasoningPart(partID string) {
	partID = strings.TrimSpace(partID)
	if partID == "" {
		return
	}
	if st.reasoningParts == nil {
		st.reasoningParts = make(map[string]struct{})
	}
	st.reasoningParts[partID] = struct{}{}
}

// isReasoningPart 检查 partID 是否为 reasoning part。
// 调用方必须持有 st.mu。
func (st *runState) isReasoningPart(partID string) bool {
	partID = strings.TrimSpace(partID)
	if partID == "" || st.reasoningParts == nil {
		return false
	}
	_, ok := st.reasoningParts[partID]
	return ok
}

func planConfirmationQuestions() []agent.QuestionItem {
	return []agent.QuestionItem{{
		Header:   "计划确认",
		Question: "以下是当前计划，是否执行？",
		Options: []agent.QuestionOption{
			{Label: "Yes"},
			{Label: "No"},
		},
		MultiSelect: false,
		Custom:      false,
	}}
}

// ============================================================================
// todo.updated 转换
// ============================================================================

func convertOpenCodeTodoItems(todos []opencodeTodoItem) []agent.RuntimeTodoItem {
	items := make([]agent.RuntimeTodoItem, 0, len(todos))
	for i, t := range todos {
		if strings.TrimSpace(t.Content) == "" {
			continue
		}
		id := t.ID
		if id == "" {
			id = "todo_" + strconv.Itoa(i+1)
		}
		items = append(items, agent.RuntimeTodoItem{
			ID:       id,
			Title:    t.Content,
			Status:   t.Status,
			Priority: t.Priority,
		})
	}
	return items
}
