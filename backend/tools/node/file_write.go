package nodetools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/insmtx/Leros/backend/tools"
	"github.com/insmtx/Leros/backend/tools/node/security"
	"github.com/insmtx/Leros/backend/tools/node/util"
)

// NodeFileWriteTool writes files to the worker workspace.
type NodeFileWriteTool struct {
	tools.BaseTool
}

// NewNodeFileWriteTool creates a worker-local node file write tool.
func NewNodeFileWriteTool() *NodeFileWriteTool {
	return newNodeFileWriteToolWithExecutor(nil)
}

func newNodeFileWriteToolWithExecutor(executor nodeExecutor) *NodeFileWriteTool {
	_ = executor
	return &NodeFileWriteTool{
		BaseTool: tools.NewBaseTool(
			ToolNameNodeFileWrite,
			"Create or modify a file inside the worker workspace",
			tools.Schema{
				Type:     "object",
				Required: []string{"path", "content"},
				Properties: map[string]*tools.Property{
					"path": {
						Type:        "string",
						Description: "File path inside the workspace",
					},
					"content": {
						Type:        "string",
						Description: "File content to write",
					},
					"append": {
						Type:        "boolean",
						Description: "Append to the file instead of overwriting it",
					},
				},
			},
		),
	}
}

// Validate checks node file write tool input.
func (t *NodeFileWriteTool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateWriteInput(input)
}

func validateWriteInput(input map[string]any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	if util.StringValue(input, "path") == "" {
		return fmt.Errorf("path is required")
	}
	if _, ok := input["content"].(string); !ok {
		return fmt.Errorf("content is required")
	}
	if _, err := util.BoolValue(input["append"]); err != nil {
		return fmt.Errorf("append must be a boolean")
	}
	return nil
}

// Execute writes a file to the worker workspace.
func (t *NodeFileWriteTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateWriteInput(input); err != nil {
		return "", err
	}

	path := util.StringValue(input, "path")

	resolvedPath, err := resolveToolPath(ctx, path)
	if err != nil {
		return "", err
	}
	if err := security.IsWriteDenied(resolvedPath); err != nil {
		return "", err
	}
	if err := validateWritableToolPath(ctx, resolvedPath); err != nil {
		return "", err
	}
	content := input["content"].(string)
	appendMode, _ := util.BoolValue(input["append"])

	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	file, err := os.OpenFile(resolvedPath, flags, 0644)
	if err != nil {
		return "", fmt.Errorf("write node file: %w", err)
	}
	bytesWritten, err := file.WriteString(content)
	if err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write node file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("write node file: %w", err)
	}

	action := "written"
	if appendMode {
		action = "appended"
	}
	lineCount := util.CountContentLines(content)

	return tools.JSONString(map[string]interface{}{
		"path":          path,
		"append":        appendMode,
		"action":        action,
		"line_count":    lineCount,
		"bytes_written": bytesWritten,
		"message":       fmt.Sprintf("file %s: %s (%d lines)", action, path, lineCount),
	})
}
