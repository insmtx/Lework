package nodetools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/insmtx/Leros/backend/tools"
	"github.com/insmtx/Leros/backend/tools/node/util"
)

// NodeFileReadTool reads files from the worker workspace.
type NodeFileReadTool struct {
	tools.BaseTool
}

// NewNodeFileReadTool creates a worker-local node file read tool.
func NewNodeFileReadTool() *NodeFileReadTool {
	return newNodeFileReadToolWithExecutor(nil)
}

func newNodeFileReadToolWithExecutor(executor nodeExecutor) *NodeFileReadTool {
	_ = executor
	return &NodeFileReadTool{
		BaseTool: tools.NewBaseTool(
			ToolNameNodeFileRead,
			"Read a file from the worker workspace with optional line ranges",
			tools.Schema{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]*tools.Property{
					"path": {
						Type:        "string",
						Description: "File path inside the workspace",
					},
					"offset": {
						Type:        "integer",
						Description: "Starting line number, beginning at 1",
					},
					"limit": {
						Type:        "integer",
						Description: "Number of lines to read; defaults to 200 and is clamped to 1-2000",
					},
				},
			},
		),
	}
}

// Validate checks node file read tool input.
func (t *NodeFileReadTool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateReadInput(input)
}

func validateReadInput(input map[string]any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	if util.StringValue(input, "path") == "" {
		return fmt.Errorf("path is required")
	}
	if offset, err := util.IntValue(input["offset"]); err != nil {
		return fmt.Errorf("offset must be an integer")
	} else if offset < 0 {
		return fmt.Errorf("offset must be greater than or equal to 0")
	}
	if limit, err := util.IntValue(input["limit"]); err != nil {
		return fmt.Errorf("limit must be an integer")
	} else if limit < 0 {
		return fmt.Errorf("limit must be greater than or equal to 0")
	}
	return nil
}

// Execute reads a file from the worker workspace.
func (t *NodeFileReadTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateReadInput(input); err != nil {
		return "", err
	}
	path := util.StringValue(input, "path")
	resolvedPath, err := resolveToolPath(ctx, path)
	if err != nil {
		return "", err
	}
	offset, _ := util.IntValue(input["offset"])
	limit, _ := util.IntValue(input["limit"])
	if limit == 0 {
		limit = defaultReadLimit
	}
	limit = util.ClampInt(limit, 1, maxReadLimit)

	info, err := os.Stat(resolvedPath)
	if os.IsNotExist(err) {
		return tools.JSONString(map[string]interface{}{
			"path":    path,
			"exists":  false,
			"message": fmt.Sprintf("file does not exist: %s", path),
		})
	}
	if err != nil {
		return "", fmt.Errorf("check node file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return tools.JSONString(map[string]interface{}{
			"path":    path,
			"exists":  false,
			"message": fmt.Sprintf("path is not a regular file: %s", path),
		})
	}
	resolvedPath, err = resolveExistingToolPath(ctx, path)
	if err != nil {
		return "", err
	}

	shownStart := offset
	if shownStart <= 0 {
		shownStart = 1
	}

	lines, totalLines, err := readFileLines(resolvedPath, shownStart, limit)
	if err != nil {
		return "", err
	}

	content := strings.Join(lines, "\n")
	numbered := make([]string, 0, len(lines))
	for index, line := range lines {
		numbered = append(numbered, fmt.Sprintf("%6d|%s", shownStart+index, line))
	}

	shownEnd := 0
	if len(lines) > 0 {
		shownEnd = shownStart + len(lines) - 1
	}
	hasMore := totalLines > 0 && len(lines) > 0 && shownEnd < totalLines
	numberedContent := strings.Join(numbered, "\n")
	if hasMore {
		numberedContent += fmt.Sprintf("\n\n[file has %d lines, shown %d-%d]", totalLines, shownStart, shownEnd)
	}

	return tools.JSONString(map[string]interface{}{
		"path":               path,
		"exists":             true,
		"offset":             shownStart,
		"limit":              limit,
		"content":            content,
		"numbered_content":   numberedContent,
		"total_lines":        totalLines,
		"shown_start":        shownStart,
		"shown_end":          shownEnd,
		"has_more":           hasMore,
		"display":            numberedContent,
		"display_line_count": len(lines),
	})
}

func readFileLines(path string, startLine int, limit int) ([]string, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read node file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	lines := make([]string, 0, limit)
	totalLines := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			totalLines++
			line = strings.TrimRight(line, "\r\n")
			if totalLines >= startLine && len(lines) < limit {
				lines = append(lines, line)
			}
		}
		if err == nil {
			continue
		}
		if errorsIsEOF(err) {
			break
		}
		return nil, 0, fmt.Errorf("read node file: %w", err)
	}

	return lines, totalLines, nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}
