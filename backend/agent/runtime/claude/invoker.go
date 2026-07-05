package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	runtimeprocess "github.com/insmtx/Leros/backend/agent/runtime/internal/process"
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
		baseEnv: runtimeprocess.BuildBaseEnv(extraEnv),
	}
}

// Invoke starts the CLI process and converts stdout/stderr into node events.
func (inv *Invoker) Invoke(ctx context.Context, req cli.InvocationRequest) (*cli.Invocation, error) {
	args := buildArgs(req)

	var settingsPath string
	if sp, err := lerosSettingsPath(req.SessionID); err == nil {
		if err := writeLerosSettings(sp, buildLerosSettings(req)); err == nil {
			args = append(args, "--settings", sp)
			settingsPath = sp
		} else {
			logs.WarnContextf(ctx, "write leros settings failed: %v", err)
		}
	}

	var mcpConfigPath string
	if len(req.MCPServers) > 0 && req.TaskDir != "" {
		mcpDir := filepath.Join(req.TaskDir, ".claude")
		if path, err := writeMCPConfig(mcpDir, req.MCPServers); err == nil {
			args = append(args, "--mcp-config", path)
			mcpConfigPath = path
		} else {
			logs.WarnContextf(ctx, "write claude mcp config failed: %v", err)
		}
	}

	cmd := exec.CommandContext(ctx, inv.binary, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = runtimeprocess.BuildRunEnv(inv.baseEnv, req.ExtraEnv, claudeModelEnv(req.Model))

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create claude stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("open claude stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("open claude stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	needsApproval := req.PermissionMode == agent.PermissionModeOnRequest ||
		req.PermissionMode == agent.PermissionModeAuto

	evtChan := make(chan agent.NodeEvent, 32)
	resultChan := make(chan cli.InvocationResult, 1)
	processDone := make(chan struct{})
	proc := runtimeprocess.NewCmdProcess(cmd, processDone)

	var responder agent.ApprovalResponder
	if needsApproval {
		responder = &claudeApprovalResponder{stdinW: stdinPipe}
	}

	go func() {
		defer close(evtChan)
		defer close(resultChan)
		closeStdinOnce := &sync.Once{}
		closeStdin := func() { closeStdinOnce.Do(func() { stdinPipe.Close() }) }
		defer closeStdin()
		if settingsPath != "" {
			defer func() { _ = os.Remove(settingsPath) }()
		}
		if mcpConfigPath != "" {
			defer func() { _ = os.Remove(mcpConfigPath) }()
		}

		if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
			if _, err := fmt.Fprintln(stdinPipe, buildStreamUserMessage(prompt)); err != nil {
				resultChan <- cli.InvocationResult{Err: fmt.Errorf("write prompt: %w", err)}
				return
			}
		}
		if !needsApproval {
			closeStdin()
		}

		parseState := &claudeStreamState{}
		if needsApproval {
			parseState.closeStdin = closeStdin
		}
		var stderrText string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			scanClaudeStdout(ctx, stdout, evtChan, parseState)
		}()
		go func() {
			defer wg.Done()
			stderrText = scanPlainOutput(ctx, stderr, evtChan)
		}()

		err := cmd.Wait()
		close(processDone)
		wg.Wait()
		if err != nil {
			runErr := fmt.Errorf("%s", claudeFailureContent(err, parseState, stderrText))
			if ctx.Err() != nil {
				runErr = ctx.Err()
			}
			resultChan <- cli.InvocationResult{
				Message:           firstNonEmptyString(parseState.result, parseState.lastAssistantText),
				Usage:             parseState.usage,
				ProviderSessionID: parseState.sessionID,
				Err:               runErr,
			}
			return
		}
		if parseState.isError {
			if parseState.result == "" {
				parseState.result = "claude execution failed"
			}
			resultChan <- cli.InvocationResult{
				Message:           parseState.result,
				Usage:             parseState.usage,
				ProviderSessionID: parseState.sessionID,
				Err:               fmt.Errorf("%s", parseState.result),
			}
			return
		}
		if parseState.result == "" && parseState.lastAssistantText != "" {
			if !sendEvent(ctx, evtChan, agent.NewMessageEndEvent(parseState.lastAssistantText, nil)) {
				return
			}
		}
		resultChan <- cli.InvocationResult{
			Message:           firstNonEmptyString(parseState.result, parseState.lastAssistantText),
			Usage:             parseState.usage,
			ProviderSessionID: parseState.sessionID,
		}
	}()

	return &cli.Invocation{
		Process:   proc,
		Events:    evtChan,
		Result:    resultChan,
		Responder: responder,
	}, nil
}

// scanPlainOutput reads plain text output and converts to events.
func scanPlainOutput(ctx context.Context, r interface{ Read([]byte) (int, error) }, evtChan chan<- agent.NodeEvent) string {
	var output strings.Builder
	messageIDs := cli.NewMessageIDMapper()
	runtimeprocess.ScanJSONLines(r, func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" {
			return true
		}
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(line)
		return sendEvent(ctx, evtChan, agent.NewMessageUpdateEvent(messageIDs.CurrentOrNew(), line))
	})
	return output.String()
}

func sendEvent(ctx context.Context, evtChan chan<- agent.NodeEvent, event agent.NodeEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case evtChan <- event:
		return true
	}
}
