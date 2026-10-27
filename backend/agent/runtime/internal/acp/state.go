package acp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

type toolRuntimeState struct {
	name      string
	kind      string
	started   bool
	ended     bool
	filtered  bool
	startedAt time.Time
	rawInput  any
	todoHash  string
}

// Translator 把 ACP 的 SessionUpdate 映射为 NodeEvent。
// SessionUpdate 由 SDK 串行回调，这里仍加锁，便于 W5 在多协程消费时复用。
type Translator struct {
	mu    sync.Mutex
	stats *Stats
	now   func() time.Time

	currentMessageID  string
	lastMessageText   map[string]string
	lastTextMessageID string
	reasoningText     map[string]string
	tools             map[string]*toolRuntimeState
	workDir           string
	planPath          string
	planDisplay       string
}

// NewTranslator 创建映射器；stats 可为 nil（单测里不关心计数时）。
func NewTranslator(stats *Stats) *Translator {
	return &Translator{
		stats:           stats,
		now:             time.Now,
		lastMessageText: map[string]string{},
		reasoningText:   map[string]string{},
		tools:           map[string]*toolRuntimeState{},
	}
}

// SetWorkDir 设置工作目录，用于把工具入参路径归一成绝对路径做计划跟踪。
func (t *Translator) SetWorkDir(dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.workDir = dir
}

// PlanArtifact 返回本轮最近一次成功写入的计划文件（绝对路径与展示路径）。
func (t *Translator) PlanArtifact() (path, display string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.planPath, t.planDisplay, t.planPath != ""
}

// Handle 映射一条通知；返回 0..n 个事件（工具卡补 start 时可能是 2 个）。
func (t *Translator) Handle(notification acpsdk.SessionNotification) []agent.NodeEvent {
	t.mu.Lock()
	defer t.mu.Unlock()

	update := notification.Update
	switch {
	case update.AgentMessageChunk != nil:
		return t.handleMessageChunk(notification, update.AgentMessageChunk)
	case update.AgentThoughtChunk != nil:
		return t.handleThoughtChunk(notification, update.AgentThoughtChunk)
	case update.ToolCall != nil:
		t.observeToolCall(update.ToolCall)
		return nil
	case update.ToolCallUpdate != nil:
		return t.handleToolCallUpdate(update.ToolCallUpdate)
	case update.UserMessageChunk != nil,
		update.Plan != nil,
		update.PlanUpdate != nil,
		update.PlanRemoved != nil,
		update.AvailableCommandsUpdate != nil,
		update.CurrentModeUpdate != nil,
		update.ConfigOptionUpdate != nil,
		update.SessionInfoUpdate != nil,
		update.UsageUpdate != nil:
		return nil
	default:
		t.countUnknown()
		return nil
	}
}

func (t *Translator) handleMessageChunk(
	notification acpsdk.SessionNotification,
	chunk *acpsdk.SessionUpdateAgentMessageChunk,
) []agent.NodeEvent {
	if isChildSession(notification) || chunk.Content.Text == nil {
		t.countDropped()
		return nil
	}
	messageID := stringValue(chunk.MessageId)
	if messageID == "" {
		messageID = t.currentMessageID
	}
	if messageID == "" {
		// 绝不发空 messageID：前端会把整条消息内容清空（runner.go:376-381）。
		t.countDropped()
		return nil
	}
	t.currentMessageID = messageID
	t.lastTextMessageID = messageID
	t.lastMessageText[messageID] += chunk.Content.Text.Text
	return []agent.NodeEvent{agent.NewMessageUpdateEvent(messageID, chunk.Content.Text.Text)}
}

func (t *Translator) handleThoughtChunk(
	notification acpsdk.SessionNotification,
	chunk *acpsdk.SessionUpdateAgentThoughtChunk,
) []agent.NodeEvent {
	if isChildSession(notification) || chunk.Content.Text == nil {
		t.countDropped()
		return nil
	}
	rawID := stringValue(chunk.MessageId)
	if rawID == "" {
		rawID = t.currentMessageID
	}
	if rawID == "" {
		t.countDropped()
		return nil
	}
	baseID, ordinal := splitReasoningMessageID(rawID)
	key := baseID + "|" + ordinal
	text := chunk.Content.Text.Text

	previous, seen := t.reasoningText[key]
	if seen && text == previous {
		// 终稿重复：同一条思考被上游二次发送。
		t.countDropped()
		return nil
	}
	delta := text
	if seen && strings.HasPrefix(text, previous) {
		delta = text[len(previous):]
	}
	t.reasoningText[key] = text
	if delta == "" {
		return nil
	}
	return []agent.NodeEvent{agent.NewReasoningUpdateEvent(baseID, delta)}
}

func (t *Translator) observeToolCall(call *acpsdk.SessionUpdateToolCall) {
	id := string(call.ToolCallId)
	if id == "" {
		return
	}
	name := ToolNameFromUpdate(string(call.Kind), call.Title)
	state := t.tools[id]
	if state == nil {
		state = &toolRuntimeState{}
		t.tools[id] = state
	}
	state.name = name
	state.kind = string(call.Kind)
	if call.RawInput != nil {
		state.rawInput = call.RawInput
	}
	if IsFilteredTool(name) {
		state.filtered = true
	}
}

func (t *Translator) handleToolCallUpdate(call *acpsdk.SessionToolCallUpdate) []agent.NodeEvent {
	id := string(call.ToolCallId)
	if id == "" {
		return nil
	}
	state := t.tools[id]
	if state == nil {
		state = &toolRuntimeState{}
		t.tools[id] = state
	}
	kind := state.kind
	if call.Kind != nil {
		kind = string(*call.Kind)
		state.kind = kind
	}
	title := state.name
	if call.Title != nil {
		title = *call.Title
	}
	name := ToolNameFromUpdate(kind, title)
	if name == "" {
		name = state.name
	}
	state.name = name
	if call.RawInput != nil {
		state.rawInput = call.RawInput
	}
	if IsFilteredTool(name) {
		state.filtered = true
	}
	if name == "todowrite" {
		return t.handleTodoInput(call, state)
	}
	if state.filtered {
		return nil
	}

	status := ""
	if call.Status != nil {
		status = string(*call.Status)
	}
	switch status {
	case string(acpsdk.ToolCallStatusInProgress):
		if state.started {
			return nil
		}
		state.started = true
		state.startedAt = t.now()
		return []agent.NodeEvent{agent.NewToolExecutionStartEvent(
			id, DisplayToolName(name), marshalArguments(state.rawInput),
		)}
	case string(acpsdk.ToolCallStatusCompleted):
		if state.ended {
			return nil
		}
		events := t.ensureStarted(id, name, state)
		state.ended = true
		text := TruncateToolOutput(toolContentText(call.Content))
		t.recordPlanArtifact(name, state)
		return append(events, agent.NewToolExecutionEndEvent(
			id, DisplayToolName(name), marshalResult(text), elapsedMS(state, t.now()),
		))
	case string(acpsdk.ToolCallStatusFailed):
		if state.ended {
			return nil
		}
		events := t.ensureStarted(id, name, state)
		state.ended = true
		text := toolContentText(call.Content)
		if strings.TrimSpace(text) == "" {
			text = "tool execution failed"
		}
		return append(events, agent.NewToolExecutionEndErrorEvent(
			id, DisplayToolName(name), text, elapsedMS(state, t.now()),
		))
	default:
		return nil
	}
}

// recordPlanArtifact 记录计划文件写入：仅成功的 edit/write/patch，
// 且路径必须落在 <workDir>/.opencode/plans/*.md（设计文档 §8.3）。
func (t *Translator) recordPlanArtifact(name string, state *toolRuntimeState) {
	if state == nil || t.workDir == "" {
		return
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if state.kind != string(acpsdk.ToolKindEdit) && lower != "write" && lower != "edit" && lower != "patch" && lower != "apply_patch" {
		return
	}
	path := rawInputPath(state.rawInput)
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workDir, path)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(t.workDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return
	}
	if filepath.Dir(relative) != filepath.Join(".opencode", "plans") || filepath.Ext(path) != ".md" {
		return
	}
	t.planPath = path
	t.planDisplay = relative
}

func rawInputPath(raw any) string {
	object, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"path", "filePath", "file_path", "file"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// handleTodoInput 把 todowrite 的 RawInput.todos 转成 todo.updated；
// 同内容重复帧不重复发事件（上游会随进度反复发送完整列表）。
func (t *Translator) handleTodoInput(
	call *acpsdk.SessionToolCallUpdate,
	state *toolRuntimeState,
) []agent.NodeEvent {
	raw := call.RawInput
	if raw == nil {
		raw = state.rawInput
	}
	todos := ParseTodos(raw)
	if len(todos) == 0 {
		return nil
	}
	encoded, err := json.Marshal(todos)
	if err != nil {
		return nil
	}
	signature := string(encoded)
	if signature == state.todoHash {
		return nil
	}
	state.todoHash = signature
	return []agent.NodeEvent{agent.NewTodoUpdatedEvent(todos)}
}

// ensureStarted 处理「没收到 in_progress 直接 completed/failed」的异常顺序，
// 先补一个 start，避免前端只有 end 没有工具卡。
func (t *Translator) ensureStarted(
	id, name string,
	state *toolRuntimeState,
) []agent.NodeEvent {
	if state.started {
		return nil
	}
	state.started = true
	state.startedAt = t.now()
	return []agent.NodeEvent{agent.NewToolExecutionStartEvent(
		id, DisplayToolName(name), marshalArguments(state.rawInput),
	)}
}

// FinalText 返回最后一条有文本的 assistant 消息（供 W5 组装 ExecutionResult）。
func (t *Translator) FinalText() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastTextMessageID == "" {
		return ""
	}
	return t.lastMessageText[t.lastTextMessageID]
}

// CurrentMessageID 返回最近一条 assistant 消息 id，用于空 messageId 兜底。
func (t *Translator) CurrentMessageID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentMessageID
}

func (t *Translator) countUnknown() {
	if t.stats != nil {
		t.stats.UnknownUpdates.Add(1)
	}
}

func (t *Translator) countDropped() {
	if t.stats != nil {
		t.stats.DroppedEvents.Add(1)
	}
}

func isChildSession(notification acpsdk.SessionNotification) bool {
	if notification.Meta == nil {
		return false
	}
	_, ok := notification.Meta[childSessionMetaKey]
	return ok
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func elapsedMS(state *toolRuntimeState, now time.Time) int64 {
	if state == nil || state.startedAt.IsZero() {
		return 0
	}
	return now.Sub(state.startedAt).Milliseconds()
}
