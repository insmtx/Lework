// Package nodetools provides worker-local tools for operating an assistant node.
package nodetools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/tools"
	"github.com/insmtx/Leros/backend/tools/node/security"
)

const (
	ToolNameNodeShell     = "terminal"
	ToolNameNodeFileRead  = "read"
	ToolNameNodeFileWrite = "write"
)

const (
	defaultShellTimeout   = 120
	minShellTimeout       = 5
	maxShellTimeout       = 600
	defaultReadLimit      = 200
	maxReadLimit          = 2000
	defaultOutputMaxLines = 50
)

type nodeExecutor interface {
	Exec(ctx context.Context, req nodeExecRequest) (nodeExecResult, error)
}

type nodeExecRequest struct {
	Args       []string
	Stdin      *string
	WorkingDir string
	Env        map[string]string
	Timeout    time.Duration
}

type nodeExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

type localExecutor struct{}

func (e localExecutor) Exec(ctx context.Context, req nodeExecRequest) (nodeExecResult, error) {
	if len(req.Args) == 0 {
		return nodeExecResult{}, fmt.Errorf("command is required")
	}

	execCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(execCtx, req.Args[0], req.Args[1:]...)
	if strings.TrimSpace(req.WorkingDir) != "" {
		cmd.Dir = req.WorkingDir
	}
	cmd.Env = security.SanitizedEnv(req.Env)
	if req.Stdin != nil {
		cmd.Stdin = strings.NewReader(*req.Stdin)
	}
	configureProcessCancellation(cmd)
	cmd.WaitDelay = 5 * time.Second

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := nodeExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, nil
	}
	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	return result, err
}

func NewTools() []tools.Tool {
	return []tools.Tool{
		NewNodeShellTool(),
		NewNodeFileReadTool(),
		NewNodeFileWriteTool(),
	}
}

func SetWriteSafeRoot(root string) {
	security.SetWriteSafeRoot(root)
}

func Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is required")
	}

	for _, tool := range NewTools() {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}

	return nil
}
