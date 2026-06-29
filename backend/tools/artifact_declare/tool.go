// Package artifact_declare provides a tool for declaring project artifacts
// produced during a task execution.
package artifact_declare

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/tools"
)

type artifactInput struct {
	Path         string
	Title        string
	Description  string
	MIMEType     string
	ArtifactType string
	IsFinal      bool
}

type artifactManifestEntry struct {
	Path         string `json:"path"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	MIMEType     string `json:"mime_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	IsFinal      bool   `json:"is_final"`
}

const (
	// ToolNameArtifactDeclare is the stable tool name for declaring artifacts.
	ToolNameArtifactDeclare = "artifact_declare"
	// ToolDescription describes the artifact declaration tool to the LLM.
	ToolDescription = `Declare a file artifact produced during task execution.

Call this after creating a file that should be shown to the user as a task artifact, such as a document, report, spreadsheet, slide deck, diagram, or other reusable deliverable.

Pass the complete file path to the created file. The file must be inside the current project repository. Runtime paths, directories, logs, caches, and temporary files are rejected. Set is_final=false only for non-final intermediate files.`
)

// Tool lets an internal Leros agent declare an artifact produced during task execution.
type Tool struct {
	tools.BaseTool
}

// NewTool creates the artifact declaration tool.
func NewTool() *Tool {
	return &Tool{
		BaseTool: tools.NewBaseTool(
			ToolNameArtifactDeclare,
			ToolDescription,
			tools.Schema{
				Type: "object",
				Required: []string{
					"path",
				},
				Properties: map[string]*tools.Property{
					"path": {
						Type:        "string",
						Description: "Complete file path for the artifact. The file must be inside the current project repository; runtime paths, directories, logs, caches, and temporary files are rejected.",
					},
					"title": {
						Type:        "string",
						Description: "Human-readable title for the artifact.",
					},
					"description": {
						Type:        "string",
						Description: "Short description of what the artifact contains.",
					},
					"mime_type": {
						Type:        "string",
						Description: "Optional MIME type. Leave empty to let the system detect it from the file.",
					},
					"artifact_type": {
						Type:        "string",
						Description: "Artifact category. Use file for normal deliverable files.",
						Enum:        []string{"file"},
					},
					"is_final": {
						Type:        "boolean",
						Description: "Whether this artifact represents the final deliverable. Defaults to true.",
					},
				},
			},
		),
	}
}

// Validate checks artifact declaration input before execution.
func (t *Tool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	_, err = parseArtifactInput(input)
	return err
}

// Execute records the artifact declaration in the manifest.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	parsed, err := parseArtifactInput(input)
	if err != nil {
		return "", err
	}

	repoDir, manifestPath, err := resolveArtifactWorkspace(ctx, parsed.Path)
	if err != nil {
		return "", err
	}

	relativePath, artifactPath, err := resolveArtifactPath(repoDir, parsed.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("artifact does not exist: %s", relativePath)
		}
		return "", fmt.Errorf("stat artifact %q: %w", relativePath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("artifact path must be a file: %s", relativePath)
	}

	entry := artifactManifestEntry{
		Path:         relativePath,
		Title:        parsed.Title,
		Description:  parsed.Description,
		MIMEType:     parsed.MIMEType,
		ArtifactType: parsed.ArtifactType,
		IsFinal:      parsed.IsFinal,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("marshal artifact manifest entry: %w", err)
	}
	line = append(line, '\n')

	file, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("open artifact manifest: %w", err)
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("append artifact manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close artifact manifest: %w", err)
	}

	return tools.JSONString(map[string]interface{}{
		"ok": true,
		"artifact": map[string]interface{}{
			"path":          relativePath,
			"filename":      filepath.Base(filepath.FromSlash(relativePath)),
			"title":         parsed.Title,
			"description":   parsed.Description,
			"mime_type":     parsed.MIMEType,
			"artifact_type": parsed.ArtifactType,
			"is_final":      parsed.IsFinal,
		},
	})
}

func parseArtifactInput(input map[string]interface{}) (artifactInput, error) {
	if input == nil {
		return artifactInput{}, fmt.Errorf("input is required")
	}

	path := strings.TrimSpace(stringValue(input["path"]))
	if path == "" {
		return artifactInput{}, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return artifactInput{}, fmt.Errorf("path must be absolute")
	}
	artifactType := strings.TrimSpace(stringValue(input["artifact_type"]))
	if artifactType != "" && artifactType != "file" {
		return artifactInput{}, fmt.Errorf("artifact_type must be file")
	}
	if artifactType == "" {
		artifactType = "file"
	}

	return artifactInput{
		Path:         filepath.Clean(filepath.FromSlash(path)),
		Title:        strings.TrimSpace(stringValue(input["title"])),
		Description:  strings.TrimSpace(stringValue(input["description"])),
		MIMEType:     strings.TrimSpace(stringValue(input["mime_type"])),
		ArtifactType: artifactType,
		IsFinal:      boolValue(input["is_final"], true),
	}, nil
}

func resolveArtifactPath(repoDir string, path string) (string, string, error) {
	repoAbs, err := filepath.Abs(repoDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository path: %w", err)
	}
	artifactAbs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve artifact path: %w", err)
	}
	relative, err := filepath.Rel(repoAbs, artifactAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve artifact relative path: %w", err)
	}
	if relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." || filepath.IsAbs(relative) {
		return "", "", fmt.Errorf("artifact path must be inside the project repository")
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if err := validateArtifactRelativePath(relative); err != nil {
		return "", "", err
	}
	return relative, artifactAbs, nil
}

func validateArtifactRelativePath(path string) error {
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." {
		return fmt.Errorf("path must be a file path")
	}
	segments := strings.Split(filepath.ToSlash(cleaned), "/")
	for _, segment := range segments {
		switch segment {
		case "..":
			return fmt.Errorf("path must not contain '..'")
		case ".git", ".leros":
			return fmt.Errorf("path must not target runtime paths")
		case "tmp", "temp", "logs", "log", "cache", ".cache":
			return fmt.Errorf("path must not target temporary, log, or cache paths")
		}
	}
	return nil
}

func resolveArtifactWorkspace(ctx context.Context, artifactPath string) (string, string, error) {
	if toolCtx, ok := tools.ToolContextFrom(ctx); ok {
		repoDir := strings.TrimSpace(toolCtx.Metadata.RepoDir)
		manifestPath := strings.TrimSpace(toolCtx.Metadata.ArtifactManifestPath)
		if repoDir != "" && manifestPath != "" {
			return repoDir, manifestPath, nil
		}
	}

	// TODO: Replace this path-based MCP fallback with run-scoped ToolContext injection
	// once external CLI MCP requests can be bound to the active Leros run.
	return inferArtifactWorkspaceFromPath(artifactPath)
}

func inferArtifactWorkspaceFromPath(artifactPath string) (string, string, error) {
	repoDir, err := findRepoDirFromArtifactPath(artifactPath)
	if err != nil {
		return "", "", err
	}
	turnDir, err := latestTurnDir(filepath.Join(repoDir, ".leros", "tasks"))
	if err != nil {
		return "", "", err
	}
	return repoDir, filepath.Join(turnDir, "artifacts.jsonl"), nil
}

func findRepoDirFromArtifactPath(artifactPath string) (string, error) {
	current := filepath.Dir(artifactPath)
	for {
		if current == "" || current == "." {
			break
		}
		if info, err := os.Stat(filepath.Join(current, ".leros")); err == nil && info.IsDir() {
			return current, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat leros workspace marker: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("project repository marker .leros not found from artifact path")
}

func latestTurnDir(tasksDir string) (string, error) {
	taskDir, err := latestChildDir(tasksDir)
	if err != nil {
		return "", fmt.Errorf("resolve latest task directory: %w", err)
	}
	turnDir, err := latestChildDir(filepath.Join(taskDir, "turns"))
	if err != nil {
		return "", fmt.Errorf("resolve latest turn directory: %w", err)
	}
	return turnDir, nil
}

func latestChildDir(parent string) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	candidates := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		candidates = append(candidates, info)
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no child directories in %s", parent)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if !left.ModTime().Equal(right.ModTime()) {
			return left.ModTime().After(right.ModTime())
		}
		return left.Name() > right.Name()
	})
	return filepath.Join(parent, candidates[0].Name()), nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func boolValue(value any, defaultValue bool) bool {
	switch typed := value.(type) {
	case nil:
		return defaultValue
	case bool:
		return typed
	default:
		return defaultValue
	}
}
