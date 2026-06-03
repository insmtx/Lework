package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/ygpkg/yg-go/logs"
)

// Invoker 启动外部 CLI 进程。
type Invoker struct {
	binary  string
	baseEnv []string
}

// NewInvoker 创建 CLI 调用器。
func NewInvoker(binary string, extraEnv map[string]string) *Invoker {
	return &Invoker{
		binary:  binary,
		baseEnv: engines.BuildBaseEnv(extraEnv),
	}
}

// Run 启动 CLI 进程并将 stdout/stderr 转换为引擎事件。
func (inv *Invoker) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	args := buildArgs(req)

	// 写入 settings.leros.{sessionId}.json，通过 --settings 覆盖用户级 ~/.claude/settings.json
	var settingsPath string
	if sp, err := lerosSettingsPath(req.SessionID); err == nil {
		if err := writeLerosSettings(sp, buildLerosSettings(req)); err == nil {
			args = append(args, "--settings", sp)
			settingsPath = sp
		} else {
			logs.WarnContextf(ctx, "write leros settings failed: %v", err)
		}
	}

	execCtx, cancel := execContext(ctx, req.Timeout)
	defer func() {
		if cancel != nil {
			// cancel is replaced below; this protects against early-return leaks
			cancel()
		}
	}()

	cmd := exec.CommandContext(execCtx, inv.binary, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = engines.BuildRunEnv(inv.baseEnv, req.ExtraEnv, claudeModelEnv(req.Model))

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
	cancel() // cancel is no longer needed after successful start
	cancel = nil

	// 以 stream-json 格式写入初始用户消息，写完关闭 stdin 发送 EOF
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		if _, err := fmt.Fprintln(stdinPipe, buildStreamUserMessage(prompt)); err != nil {
			stdinPipe.Close()
			return nil, fmt.Errorf("write initial prompt to claude stdin: %w", err)
		}
	}
	stdinPipe.Close()

	evtChan := make(chan events.Event, 32)
	proc := engines.NewCmdProcess(cmd)
	evtChan <- events.Event{Type: events.EventStarted}

	go func() {
		defer close(evtChan)
		if cancel != nil {
			defer cancel()
		}
		// 清理 settings 文件
		if settingsPath != "" {
			defer func() { _ = os.Remove(settingsPath) }()
		}

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
		Process: proc,
		Events:  evtChan,
	}, nil
}

func execContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

// scanPlainOutput 读取纯文本输出并转为事件。
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
