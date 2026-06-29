package nodetools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/tools"
	"github.com/insmtx/Leros/backend/tools/node/security"
	"github.com/insmtx/Leros/backend/tools/node/util"
)

// NodeShellTool executes shell commands in the worker environment.
type NodeShellTool struct {
	tools.BaseTool
	executor nodeExecutor
}

// NewNodeShellTool creates a worker-local node shell tool.
func NewNodeShellTool() *NodeShellTool {
	return newNodeShellToolWithExecutor(localExecutor{})
}

func newNodeShellToolWithExecutor(executor nodeExecutor) *NodeShellTool {
	return &NodeShellTool{
		BaseTool: tools.NewBaseTool(
			ToolNameNodeShell,
			"Execute a shell command in the worker environment",
			tools.Schema{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]*tools.Property{
					"command": {
						Type:        "string",
						Description: "Shell command to execute",
					},
					"working_dir": {
						Type:        "string",
						Description: "Working directory inside the workspace; defaults to the workspace root",
					},
					"timeout": {
						Type:        "integer",
						Description: "Timeout in seconds; defaults to 120 and is clamped to 5-600",
					},
				},
			},
		),
		executor: executor,
	}
}

// Validate checks node shell tool input.
func (t *NodeShellTool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateShellInput(input)
}

func validateShellInput(input map[string]any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	if util.StringValue(input, "command") == "" {
		return fmt.Errorf("command is required")
	}
	if _, err := util.IntValue(input["timeout"]); err != nil {
		return fmt.Errorf("timeout must be an integer")
	}
	if workingDir, ok := input["working_dir"].(string); ok && strings.TrimSpace(workingDir) == "" {
		return fmt.Errorf("working_dir must be a non-empty string")
	}
	return nil
}

// Execute runs the shell command in the worker environment.
func (t *NodeShellTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.executor == nil {
		return "", fmt.Errorf("node executor is required")
	}
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateShellInput(input); err != nil {
		return "", err
	}

	command := util.StringValue(input, "command")

	approval := security.CheckDangerousCommand(command)
	if !approval.Approved {
		return tools.JSONString(map[string]interface{}{
			"command":     command,
			"approved":    false,
			"status":      approval.Status,
			"pattern_key": approval.PatternKey,
			"description": approval.Description,
			"message":     approval.Message,
		})
	}

	workingDir := util.StringValue(input, "working_dir")
	workingDir, err = resolveToolWorkDir(ctx, workingDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	timeoutSeconds, _ := util.IntValue(input["timeout"])
	if timeoutSeconds == 0 {
		timeoutSeconds = defaultShellTimeout
	}
	timeoutSeconds = util.ClampInt(timeoutSeconds, minShellTimeout, maxShellTimeout)

	result, err := t.executor.Exec(ctx, nodeExecRequest{
		Args:       shellCommandArgs(command),
		WorkingDir: workingDir,
		Timeout:    time.Duration(timeoutSeconds) * time.Second,
	})
	if err != nil {
		return "", fmt.Errorf("execute node shell command: %w", err)
	}
	if result.TimedOut {
		return tools.JSONString(map[string]interface{}{
			"command":     command,
			"working_dir": workingDir,
			"timeout":     timeoutSeconds,
			"timed_out":   true,
			"message":     fmt.Sprintf("command timed out after %ds", timeoutSeconds),
		})
	}

	combined := util.CombineOutput(result.Stdout, result.Stderr)
	output, truncated, totalLines := util.TruncateOutput(combined, defaultOutputMaxLines)
	display := fmt.Sprintf("[exit_code=%d]", result.ExitCode)
	if output != "" {
		display += "\n" + output
	}

	return tools.JSONString(map[string]interface{}{
		"command":     command,
		"working_dir": workingDir,
		"timeout":     timeoutSeconds,
		"exit_code":   result.ExitCode,
		"stdout":      result.Stdout,
		"stderr":      result.Stderr,
		"output":      output,
		"display":     display,
		"truncated":   truncated,
		"total_lines": totalLines,
	})
}
