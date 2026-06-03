package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

type codexEvent struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id,omitempty"`
	Item     *codexItem `json:"item,omitempty"`
	// JSON-RPC 2.0 fields for approval requests
	JSONRPC string          `json:"jsonrpc,omitempty"`
	Method  string          `json:"method,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type codexApprovalParams struct {
	RequestID  string `json:"request_id"`
	Type       string `json:"type"`
	Command    string `json:"command,omitempty"`
	Path       string `json:"path,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type codexItem struct {
	ID          string          `json:"id,omitempty"`
	Type        string          `json:"type"`
	Text        json.RawMessage `json:"text,omitempty"`
	Items       []codexTodoItem `json:"items,omitempty"`
	Command     string          `json:"command,omitempty"`
	CommandLine string          `json:"command_line,omitempty"`
	Name        string          `json:"name,omitempty"`
	Output      string          `json:"output,omitempty"`
	Aggregated  string          `json:"aggregated_output,omitempty"`
}

type codexTodoItem struct {
	ID        string `json:"id,omitempty"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

// Run starts the CLI process and converts stdout into engine events.
func (inv *Invoker) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	threadID, resume := resolveThread(req.SessionID, req.Resume)
	args := buildArgs(threadID, resume, req)

	execCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}

	cmd := exec.CommandContext(execCtx, inv.binary, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = engines.BuildRunEnv(inv.baseEnv, req.ExtraEnv, codexModelEnv(req.Model))

	// Create stdin pipe for bidirectional communication
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create codex stdin pipe: %w", err)
	}

	// Write initial prompt if present (Codex reads prompt from stdin in exec mode)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		if _, err := fmt.Fprintln(stdinPipe, prompt); err != nil {
			stdinPipe.Close()
			cancel()
			return nil, fmt.Errorf("write initial prompt to codex stdin: %w", err)
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("open codex stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("open codex stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
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
		go func() {
			for decision := range decisionCh {
				if _, err := fmt.Fprintln(stdinPipe, decision); err != nil {
					logs.WarnContextf(ctx, "write decision to codex stdin: %v", err)
				}
			}
		}()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			scanStdout(ctx, stdout, evtChan)
		}()
		go func() {
			defer wg.Done()
			logPlainOutput(ctx, stderr)
		}()

		err := cmd.Wait()
		wg.Wait()
		if err != nil {
			evtChan <- events.Event{Type: events.EventFailed, Content: err.Error()}
			return
		}
		evtChan <- events.Event{Type: events.EventCompleted}
	}()

	return &engines.RunHandle{
		Process:     proc,
		Events:      evtChan,
		StdinWriter: &codexDecisionWriter{ch: decisionCh},
	}, nil
}

// codexDecisionWriter adapts io.Writer to the decisionCh channel.
type codexDecisionWriter struct {
	ch chan<- string
}

func (w *codexDecisionWriter) Write(p []byte) (int, error) {
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

func codexModelEnv(model engines.ModelConfig) map[string]string {
	baseURL := ensureV1Suffix(model.BaseURL)
	return map[string]string{
		"CODEX_QUIET_MODE": "1",
		"OPENAI_API_KEY":   model.APIKey,
		"OPENAI_API_BASE":  baseURL,
		"OPENAI_BASE_URL":  baseURL,
	}
}

func scanStdout(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- events.Event) {
	state := &codexStreamState{}
	engines.ScanJSONLines(r, func(line string) bool {
		event := parseCodexLineWithState(line, state)
		if event.Type == "" {
			return true
		}
		return sendEvent(ctx, evtChan, event)
	})
}

type codexStreamState struct {
}

func parseCodexLine(line string) events.Event {
	return parseCodexLineWithState(line, &codexStreamState{})
}

func parseCodexLineWithState(line string, state *codexStreamState) events.Event {
	logs.Infof("Parse Codex line: %s", line)
	line = strings.TrimSpace(line)
	if line == "" {
		return events.Event{}
	}
	var event codexEvent
	if json.Unmarshal([]byte(line), &event) != nil {
		return events.Event{}
	}

	// JSON-RPC 2.0 approval request detection
	if event.JSONRPC == "2.0" && event.Method == "approval/request" {
		var params codexApprovalParams
		if event.Params != nil && json.Unmarshal(event.Params, &params) == nil && params.RequestID != "" {
			requestID := params.RequestID
			if rid := string(event.ID); rid != "" && rid != "null" {
				requestID = rid
			}
			desc := fmt.Sprintf("Approval request: %s", params.Reason)
			return *events.NewApprovalRequested(events.ApprovalRequestPayload{
				RequestID:   requestID,
				Engine:      "codex",
				ActionType:  params.Type,
				Description: desc,
				Command:     params.Command,
				FilePath:    params.Path,
			})
		}
	}

	if event.Type == "thread.started" && event.ThreadID != "" {
		return events.Event{Type: engines.EventProviderSessionStarted, Content: event.ThreadID}
	}
	if event.Item == nil {
		return events.Event{}
	}

	item := event.Item
	switch item.Type {
	case "agent_message":
		text := decodeCodexText(item.Text)
		if text == "" {
			return events.Event{}
		}
		messageID := item.ID
		eventType := events.EventMessageDelta
		if event.Type == "item.completed" {
			eventType = events.EventResult
		}
		if eventType == events.EventMessageDelta {
			return *events.NewMessageDelta(messageID, text)
		}
		return events.Event{Type: eventType, Content: text}
	case "command_execution", "tool_call", "shell_command":
		command := firstNonEmptyString(item.Command, item.CommandLine, item.Name)
		if command != "" {
			return *events.NewMessageDelta(item.ID, "$ "+command)
		}
		output := firstNonEmptyString(item.Output, item.Aggregated, decodeCodexText(item.Text))
		if output != "" {
			return *events.NewMessageDelta(item.ID, truncateOutput(output, 300))
		}
	case "command_output", "tool_output", "shell_output":
		output := firstNonEmptyString(item.Output, item.Aggregated, decodeCodexText(item.Text))
		if output != "" {
			return *events.NewMessageDelta(item.ID, truncateOutput(output, 300))
		}
	case "todo_list":
		items := todoItemsFromCodex(item.Items)
		if len(items) != 0 {
			return *events.NewTodoSnapshot(items)
		}
	}
	return events.Event{}
}

func todoItemsFromCodex(items []codexTodoItem) []events.RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	result := make([]events.RuntimeTodoItem, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Text)
		if title == "" {
			continue
		}
		status := "pending"
		if item.Completed {
			status = "completed"
		}
		result = append(result, events.RuntimeTodoItem{
			ID:     strings.TrimSpace(item.ID),
			Title:  title,
			Status: status,
		})
	}
	return result
}

func decodeCodexText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []any
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			if value, ok := part.(string); ok {
				b.WriteString(value)
			}
		}
		return b.String()
	}
	return ""
}

func logPlainOutput(ctx context.Context, r interface{ Read([]byte) (int, error) }) {
	engines.ScanJSONLines(r, func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" {
			return true
		}
		logs.WarnContextf(ctx, "Codex stderr: %s", line)
		return true
	})
}

func sendEvent(ctx context.Context, evtChan chan<- events.Event, event events.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case evtChan <- event:
		return true
	}
}

func truncateOutput(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func buildArgs(threadID string, resume bool, req engines.RunRequest) []string {
	args := []string{"exec"}
	args = append(args, lerosProviderConfigArgs(req)...)
	if req.Model.Model != "" {
		args = append(args, "--model", req.Model.Model)
	}

	// Permission mode determines whether to bypass approvals
	bypassArgs := ""
	switch req.PermissionMode {
	case engines.PermissionModeBypass, "":
		bypassArgs = "--dangerously-bypass-approvals-and-sandbox"
	case engines.PermissionModeOnRequest, engines.PermissionModeAuto:
		// on-request and auto: remove bypass; let Codex issue approval/request notifications
	}

	if resume && threadID != "" {
		args = append(args, "resume", threadID, "--json", "--skip-git-repo-check")
		if bypassArgs != "" {
			args = append(args, bypassArgs)
		}
		if req.Prompt != "" {
			args = append(args, "-")
		}
		return args
	}

	args = append(args, "-", "--json", "--skip-git-repo-check")
	if bypassArgs != "" {
		args = append(args, bypassArgs)
	}
	return args
}

func lerosProviderConfigArgs(req engines.RunRequest) []string {
	baseURL := ensureV1Suffix(firstNonEmptyString(
		req.Model.BaseURL,
		os.Getenv("OPENAI_BASE_URL"),
	))
	return []string{
		"-c", `model_provider="leros"`,
		"-c", `model_providers.leros.name="leros"`,
		"-c", fmt.Sprintf(`model_providers.leros.base_url=%q`, baseURL),
		"-c", `model_providers.leros.env_key="OPENAI_API_KEY"`,
	}
}

// ensureV1Suffix appends /v1 when it is missing.
func ensureV1Suffix(url string) string {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return url
	}
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	return url
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveThread(sessionID string, resume bool) (string, bool) {
	if !resume {
		return "", false
	}
	threadID := strings.TrimSpace(sessionID)
	return threadID, threadID != ""
}
