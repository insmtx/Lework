package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// Invoker starts an external CLI process.
type Invoker struct {
	binary  string
	baseEnv []string
}

// NewInvoker creates a CLI invoker.
func NewInvoker(binary string, extraEnv map[string]string) *Invoker {
	return &Invoker{
		binary:  binary,
		baseEnv: engines.BuildBaseEnv(extraEnv),
	}
}

type streamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Message   *streamMessage `json:"message,omitempty"`
	Result    string         `json:"result,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Usage     *streamUsage   `json:"usage,omitempty"`
	// control_request fields
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

// Run starts the CLI process and converts stdout/stderr into engine events.
func (inv *Invoker) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	args := buildArgs(req)

	execCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}

	cmd := exec.CommandContext(execCtx, inv.binary, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = engines.BuildRunEnv(inv.baseEnv, req.ExtraEnv, claudeModelEnv(req.Model))

	// Create stdin pipe for bidirectional communication
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create claude stdin pipe: %w", err)
	}

	// Write initial prompt if present
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		if _, err := fmt.Fprintln(stdinPipe, prompt); err != nil {
			stdinPipe.Close()
			cancel()
			return nil, fmt.Errorf("write initial prompt to claude stdin: %w", err)
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("open claude stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("open claude stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	evtChan := make(chan events.Event, 32)
	proc := engines.NewCmdProcess(cmd)
	evtChan <- events.Event{Type: events.EventStarted}

	// decisionCh carries approval decisions to the stdin writer goroutine
	decisionCh := make(chan string, 8)

	go func() {
		defer close(evtChan)
		defer cancel()
		defer close(decisionCh)
		defer stdinPipe.Close()

		// Stdin writer goroutine: writes approval decisions as they arrive
		var stdinWriteMu sync.Mutex
		go func() {
			for decision := range decisionCh {
				stdinWriteMu.Lock()
				if _, err := fmt.Fprintln(stdinPipe, decision); err != nil {
					logs.WarnContextf(ctx, "write decision to claude stdin: %v", err)
				}
				stdinWriteMu.Unlock()
			}
		}()

		parseState := &claudeStreamState{}
		var stderrText string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			scanClaudeStdout(ctx, stdout, evtChan, parseState)
		}()
		go func() {
			defer wg.Done()
			stderrText = scanPlainOutput(ctx, stderr, evtChan, events.EventMessageDelta)
		}()

		err := cmd.Wait()
		wg.Wait()
		if err != nil {
			evtChan <- events.Event{Type: events.EventFailed, Content: claudeFailureContent(err, parseState, stderrText)}
			return
		}
		if parseState.isError {
			if parseState.result == "" {
				parseState.result = "claude execution failed"
			}
			evtChan <- events.Event{Type: events.EventFailed, Content: parseState.result}
			return
		}
		if parseState.result == "" && parseState.lastAssistantText != "" {
			if !sendEvent(ctx, evtChan, *events.NewMessageResult(parseState.lastAssistantText, nil)) {
				return
			}
		}
		evtChan <- events.Event{Type: events.EventCompleted}
	}()

	return &engines.RunHandle{
		Process:     proc,
		Events:      evtChan,
		StdinWriter: &decisionWriter{ch: decisionCh},
	}, nil
}

// decisionWriter adapts io.Writer to the decisionCh channel.
type decisionWriter struct {
	ch chan<- string
}

func (w *decisionWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}
	select {
	case w.ch <- line:
		return len(p), nil
	default:
		return 0, fmt.Errorf("decision channel full, dropping: %s", line)
	}
}

func claudeModelEnv(model engines.ModelConfig) map[string]string {
	return map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                     model.APIKey,
		"ANTHROPIC_API_KEY":                        model.APIKey,
		"ANTHROPIC_BASE_URL":                       withoutV1Suffix(model.BaseURL),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
}

func withoutV1Suffix(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return strings.TrimSuffix(baseURL, "/v1")
}

type claudeStreamState struct {
	result             string
	isError            bool
	lastAssistantText  string
	toolNames          map[string]string
	pendingTaskCreates map[string]events.RuntimeTodoItem
}

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
	case "user":
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
	case "result":
		state.result = event.Result
		state.isError = event.IsError
		if event.IsError || event.Result == "" {
			return nil
		}
		return []events.Event{*events.NewMessageResult(event.Result, usagePayloadFromClaudeUsage(event.Usage))}
	case "control_request":
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
	return nil
}

// summarizeInput creates a human-readable summary of tool input for the approval prompt.
func summarizeInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	// Try common keys used by Claude Code tools
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

func buildArgs(req engines.RunRequest) []string {
	args := []string{
		"--verbose",
		"--output-format", "stream-json",
		"--disallowedTools", "AskUserQuestion",
	}

	// Permission mode determines whether to bypass approvals
	switch req.PermissionMode {
	case engines.PermissionModeBypass, "":
		args = append(args, "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions")
	case engines.PermissionModeOnRequest, engines.PermissionModeAuto:
		// on-request and auto both use default mode; auto is handled by ApprovalHandler
		args = append(args, "--permission-mode", "default")
	}

	if req.Model.Model != "" {
		args = append(args, "--model", req.Model.Model)
	}
	if systemPrompt := strings.TrimSpace(req.SystemPrompt); systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}
	if req.SessionID != "" {
		if req.Resume {
			args = append(args, "--resume", req.SessionID)
		} else {
			args = append(args, "--session-id", req.SessionID)
		}
	}
	return append(args, "--print")
}

func usagePayloadFromClaudeUsage(usage *streamUsage) *events.UsagePayload {
	if usage == nil {
		return nil
	}
	inputTokens := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	outputTokens := usage.OutputTokens
	totalTokens := inputTokens + outputTokens
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}
	return &events.UsagePayload{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
}

func claudeToolCallStartedEvent(block streamContent, state *claudeStreamState) events.Event {
	rememberClaudeToolName(block, state)
	return *events.NewToolCallStarted(block.ID, block.Name, block.Input)
}

func rememberClaudeToolName(block streamContent, state *claudeStreamState) {
	if state != nil && block.ID != "" && block.Name != "" {
		if state.toolNames == nil {
			state.toolNames = make(map[string]string)
		}
		state.toolNames[block.ID] = block.Name
	}
}

func claudeToolName(toolUseID string, state *claudeStreamState) string {
	if state == nil || state.toolNames == nil {
		return ""
	}
	return state.toolNames[toolUseID]
}

func claudeToolCallCompletedEvent(block streamContent, state *claudeStreamState) events.Event {
	name := claudeToolName(block.ToolUseID, state)
	if block.IsError {
		return *events.NewToolCallFailed(block.ToolUseID, name, fmt.Sprintf("%v", block.Content), 0)
	}
	return *events.NewToolCallCompleted(block.ToolUseID, name, block.Content, 0)
}

func isClaudeTodoTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList":
		return true
	default:
		return false
	}
}

func claudeTodoEventsFromToolUse(block streamContent, state *claudeStreamState) []events.Event {
	name := strings.TrimSpace(block.Name)
	switch name {
	case "TodoWrite":
		items := todoItemsFromClaudeTodos(block.Input["todos"])
		if len(items) == 0 {
			return nil
		}
		return []events.Event{*events.NewTodoSnapshot(items)}
	case "TaskCreate":
		item := todoItemFromClaudeTaskCreate(block.Input)
		if item.Title == "" {
			return nil
		}
		if item.ID == "" {
			item.ID = strings.TrimSpace(block.ID)
		}
		if state != nil && block.ID != "" {
			if state.pendingTaskCreates == nil {
				state.pendingTaskCreates = make(map[string]events.RuntimeTodoItem)
			}
			state.pendingTaskCreates[block.ID] = item
		}
		return []events.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	case "TaskUpdate":
		item := todoItemFromClaudeTaskUpdate(block.Input)
		if item.ID == "" && item.Title == "" {
			return nil
		}
		return []events.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	}
	return nil
}

func claudeTodoEventsFromToolResult(block streamContent, state *claudeStreamState) []events.Event {
	name := ""
	if state != nil && state.toolNames != nil {
		name = state.toolNames[block.ToolUseID]
	}
	switch strings.TrimSpace(name) {
	case "TaskCreate":
		item, ok := claudeTaskItemFromResult(block.Content)
		if state != nil && state.pendingTaskCreates != nil {
			if pending, exists := state.pendingTaskCreates[block.ToolUseID]; exists {
				if item.ID == "" {
					item.ID = pending.ID
				}
				if item.Title == "" {
					item.Title = pending.Title
				}
				if item.Status == "" {
					item.Status = pending.Status
				}
				if item.Priority == "" {
					item.Priority = pending.Priority
				}
				delete(state.pendingTaskCreates, block.ToolUseID)
				ok = true
			}
		}
		if !ok || (item.ID == "" && item.Title == "") {
			return nil
		}
		return []events.Event{*events.NewTodoUpdated([]events.RuntimeTodoItem{item})}
	case "TaskList":
		items := claudeTaskListFromResult(block.Content)
		if len(items) == 0 {
			return nil
		}
		return []events.Event{*events.NewTodoSnapshot(items)}
	}
	return nil
}

func todoItemsFromClaudeTodos(value any) []events.RuntimeTodoItem {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]events.RuntimeTodoItem, 0, len(list))
	for index, raw := range list {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := firstNonEmptyString(anyString(obj["content"]), anyString(obj["title"]), anyString(obj["description"]))
		if title == "" {
			continue
		}
		id := firstNonEmptyString(anyString(obj["id"]), fmt.Sprintf("todo_%d", index+1))
		items = append(items, events.RuntimeTodoItem{
			ID:       id,
			Title:    title,
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		})
	}
	return items
}

func todoItemFromClaudeTaskCreate(input map[string]any) events.RuntimeTodoItem {
	return events.RuntimeTodoItem{
		ID:       anyString(input["id"]),
		Title:    firstNonEmptyString(anyString(input["title"]), anyString(input["subject"]), anyString(input["description"]), anyString(input["content"])),
		Status:   firstNonEmptyString(anyString(input["status"]), "pending"),
		Priority: anyString(input["priority"]),
	}
}

func todoItemFromClaudeTaskUpdate(input map[string]any) events.RuntimeTodoItem {
	return events.RuntimeTodoItem{
		ID:       firstNonEmptyString(anyString(input["id"]), anyString(input["task_id"]), anyString(input["taskId"])),
		Title:    firstNonEmptyString(anyString(input["title"]), anyString(input["subject"]), anyString(input["description"]), anyString(input["content"])),
		Status:   anyString(input["status"]),
		Priority: anyString(input["priority"]),
	}
}

func claudeTaskItemFromResult(content any) (events.RuntimeTodoItem, bool) {
	value, ok := decodedJSONValue(content)
	if !ok {
		return events.RuntimeTodoItem{}, false
	}
	if obj, ok := value.(map[string]any); ok {
		if task, ok := obj["task"].(map[string]any); ok {
			obj = task
		}
		item := events.RuntimeTodoItem{
			ID:       firstNonEmptyString(anyString(obj["id"]), anyString(obj["task_id"]), anyString(obj["taskId"])),
			Title:    firstNonEmptyString(anyString(obj["title"]), anyString(obj["subject"]), anyString(obj["description"]), anyString(obj["content"])),
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		}
		return item, item.ID != "" || item.Title != ""
	}
	return events.RuntimeTodoItem{}, false
}

func claudeTaskListFromResult(content any) []events.RuntimeTodoItem {
	value, ok := decodedJSONValue(content)
	if !ok {
		return nil
	}
	var rawTasks []any
	switch typed := value.(type) {
	case []any:
		rawTasks = typed
	case map[string]any:
		for _, key := range []string{"tasks", "todos", "items"} {
			if list, ok := typed[key].([]any); ok {
				rawTasks = list
				break
			}
		}
	}
	if len(rawTasks) == 0 {
		return nil
	}
	items := make([]events.RuntimeTodoItem, 0, len(rawTasks))
	for index, raw := range rawTasks {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := firstNonEmptyString(anyString(obj["title"]), anyString(obj["subject"]), anyString(obj["description"]), anyString(obj["content"]))
		if title == "" {
			continue
		}
		items = append(items, events.RuntimeTodoItem{
			ID:       firstNonEmptyString(anyString(obj["id"]), anyString(obj["task_id"]), fmt.Sprintf("task_%d", index+1)),
			Title:    title,
			Status:   anyString(obj["status"]),
			Priority: anyString(obj["priority"]),
		})
	}
	return items
}

func decodedJSONValue(content any) (any, bool) {
	switch typed := content.(type) {
	case nil:
		return nil, false
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil, false
		}
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, false
		}
		return value, true
	default:
		return typed, true
	}
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func scanPlainOutput(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- events.Event, eventType events.EventType) string {
	var output strings.Builder
	messageIDs := events.NewMessageIDMapper()
	engines.ScanJSONLines(r, func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" {
			return true
		}
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(line)
		if eventType == events.EventMessageDelta {
			return sendEvent(ctx, evtChan, *events.NewMessageDelta(messageIDs.CurrentOrNew(), line))
		}
		return sendEvent(ctx, evtChan, events.Event{Type: eventType, Content: line})
	})
	return output.String()
}

func sendEvent(ctx context.Context, evtChan chan<- events.Event, event events.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case evtChan <- event:
		return true
	}
}

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
