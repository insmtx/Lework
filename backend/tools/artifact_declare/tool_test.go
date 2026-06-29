package artifact_declare

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/tools"
)

func TestToolMetadata(t *testing.T) {
	tool := NewTool()

	if got := tool.Name(); got != ToolNameArtifactDeclare {
		t.Fatalf("Name() = %q, want %q", got, ToolNameArtifactDeclare)
	}
	if got := tool.Description(); strings.TrimSpace(got) == "" {
		t.Fatal("Description() must not be empty")
	}

	schema := tool.InputSchema()
	if schema.Type != "object" {
		t.Fatalf("InputSchema().Type = %q, want object", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Fatalf("InputSchema().Required = %v, want [path]", schema.Required)
	}
	for _, key := range []string{"path", "title", "description", "mime_type", "artifact_type", "is_final"} {
		if schema.Properties[key] == nil {
			t.Fatalf("InputSchema().Properties[%q] is missing", key)
		}
	}
}

func TestValidate_ValidInput(t *testing.T) {
	tool := NewTool()
	err := tool.Validate(tools.JSONInput(map[string]interface{}{
		"path":          filepath.Join(t.TempDir(), "docs", "report.md"),
		"title":         "Report",
		"description":   "Final report",
		"mime_type":     "text/markdown",
		"artifact_type": "file",
		"is_final":      false,
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_MissingPath(t *testing.T) {
	tool := NewTool()
	err := tool.Validate(tools.JSONInput(map[string]interface{}{"title": "Report"}))
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("Validate() error = %v, want path is required", err)
	}
}

func TestValidate_RelativePath(t *testing.T) {
	tool := NewTool()
	err := tool.Validate(tools.JSONInput(map[string]interface{}{"path": "docs/report.md"}))
	if err == nil || !strings.Contains(err.Error(), "path must be absolute") {
		t.Fatalf("Validate() error = %v, want absolute path error", err)
	}
}

func TestExecute_OutsideRepoPath(t *testing.T) {
	tool := NewTool()
	ctx, _ := newToolContext(t)
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secrets.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{"path": outsidePath}))
	if err == nil || !strings.Contains(err.Error(), "inside the project repository") {
		t.Fatalf("Execute() error = %v, want outside repo error", err)
	}
}

func TestExecute_RuntimePath(t *testing.T) {
	tool := NewTool()
	tests := []string{".git/config", ".leros/runtime.json", "nested/.git/config", "tmp/report.md", "logs/report.md", "cache/report.md"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			ctx, _ := newToolContext(t)
			writeRepoFile(t, ctx, path, "runtime")
			err := tool.Validate(tools.JSONInput(map[string]interface{}{"path": repoPathFromContext(t, ctx, path)}))
			if err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
			_, err = tool.Execute(ctx, tools.JSONInput(map[string]interface{}{"path": repoPathFromContext(t, ctx, path)}))
			if err == nil {
				t.Fatalf("Execute() error = %v, want runtime path error", err)
			}
		})
	}
}

func TestValidate_InvalidArtifactType(t *testing.T) {
	tool := NewTool()
	err := tool.Validate(tools.JSONInput(map[string]interface{}{
		"path":          filepath.Join(t.TempDir(), "docs", "report.md"),
		"artifact_type": "directory",
	}))

	if err == nil || !strings.Contains(err.Error(), "artifact_type must be file") {
		t.Fatalf("Validate() error = %v, want artifact_type error", err)
	}
}

func TestExecute_FileNotFound(t *testing.T) {
	tool := NewTool()
	ctx, _ := newToolContext(t)

	_, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{"path": repoPathFromContext(t, ctx, "docs/missing.md")}))
	if err == nil || !strings.Contains(err.Error(), "artifact does not exist") {
		t.Fatalf("Execute() error = %v, want file not found", err)
	}
}

func TestExecute_Success(t *testing.T) {
	tool := NewTool()
	ctx, manifestPath := newToolContext(t)
	writeRepoFile(t, ctx, "artifacts/report.md", "hello")

	output, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"path":          repoPathFromContext(t, ctx, "artifacts/report.md"),
		"title":         "Report",
		"description":   "Summary",
		"mime_type":     "text/markdown",
		"artifact_type": "file",
	}))

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	result := decodeJSONMap(t, output)
	if result["ok"] != true {
		t.Fatalf("output ok = %v, want true", result["ok"])
	}
	artifact, ok := result["artifact"].(map[string]interface{})
	if !ok {
		t.Fatalf("output artifact = %#v, want object", result["artifact"])
	}
	if artifact["path"] != "artifacts/report.md" {
		t.Fatalf("output path = %v, want artifacts/report.md", artifact["path"])
	}
	if artifact["filename"] != "report.md" {
		t.Fatalf("output filename = %v, want report.md", artifact["filename"])
	}
	if artifact["is_final"] != true {
		t.Fatalf("output is_final = %v, want true", artifact["is_final"])
	}
	if artifact["mime_type"] != "text/markdown" {
		t.Fatalf("output mime_type = %v, want text/markdown", artifact["mime_type"])
	}
	if artifact["artifact_type"] != "file" {
		t.Fatalf("output artifact_type = %v, want file", artifact["artifact_type"])
	}

	entries := readManifestEntries(t, manifestPath)
	if len(entries) != 1 {
		t.Fatalf("manifest entries = %d, want 1", len(entries))
	}
	if entries[0].Path != "artifacts/report.md" || !entries[0].IsFinal {
		t.Fatalf("manifest entry = %+v, want path artifacts/report.md and final true", entries[0])
	}
	if entries[0].MIMEType != "text/markdown" || entries[0].ArtifactType != "file" {
		t.Fatalf("manifest entry = %+v, want MIME and artifact type", entries[0])
	}
}

func TestExecute_NormalizesNonArtifactsPath(t *testing.T) {
	tool := NewTool()
	ctx, manifestPath := newToolContext(t)
	writeRepoFile(t, ctx, "docs/report.md", "hello")

	output, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"path":        repoPathFromContext(t, ctx, "docs/report.md"),
		"title":       "Report",
		"description": "Summary",
	}))

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	result := decodeJSONMap(t, output)
	artifact, ok := result["artifact"].(map[string]interface{})
	if !ok {
		t.Fatalf("output artifact = %#v, want object", result["artifact"])
	}
	if artifact["path"] != "artifacts/report.md" {
		t.Fatalf("output path = %v, want artifacts/report.md after normalization", artifact["path"])
	}
	if artifact["filename"] != "report.md" {
		t.Fatalf("output filename = %v, want report.md", artifact["filename"])
	}

	entries := readManifestEntries(t, manifestPath)
	if len(entries) != 1 {
		t.Fatalf("manifest entries = %d, want 1", len(entries))
	}
	if entries[0].Path != "artifacts/report.md" {
		t.Fatalf("manifest entry path = %+v, want artifacts/report.md", entries[0].Path)
	}

	originalPath := repoPathFromContext(t, ctx, "docs/report.md")
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Fatalf("original file should be moved: stat err = %v", err)
	}
	normalizedPath := repoPathFromContext(t, ctx, "artifacts/report.md")
	if _, err := os.Stat(normalizedPath); err != nil {
		t.Fatalf("normalized file should exist: stat err = %v", err)
	}
}

func TestExecute_InfersWorkspaceFromArtifactPath(t *testing.T) {
	tool := NewTool()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "artifacts", "report.md"), "hello")

	oldTurn := filepath.Join(repoDir, ".leros", "tasks", "task_old", "turns", "req_old")
	latestTurn := filepath.Join(repoDir, ".leros", "tasks", "task_new", "turns", "req_new")
	mkdirWithTime(t, repoDir, oldTurn, time.Now().Add(-2*time.Hour))
	mkdirWithTime(t, repoDir, latestTurn, time.Now().Add(-1*time.Hour))

	output, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":  filepath.Join(repoDir, "artifacts", "report.md"),
		"title": "Report",
	}))

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	result := decodeJSONMap(t, output)
	artifact, ok := result["artifact"].(map[string]interface{})
	if !ok {
		t.Fatalf("output artifact = %#v, want object", result["artifact"])
	}
	if artifact["path"] != "artifacts/report.md" {
		t.Fatalf("output path = %v, want artifacts/report.md", artifact["path"])
	}

	entries := readManifestEntries(t, filepath.Join(latestTurn, "artifacts.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("manifest entries = %d, want 1", len(entries))
	}
	if entries[0].Path != "artifacts/report.md" {
		t.Fatalf("manifest path = %q, want artifacts/report.md", entries[0].Path)
	}
	if _, err := os.Stat(filepath.Join(oldTurn, "artifacts.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old turn manifest exists or stat error = %v, want not exist", err)
	}
}

func TestExecute_InferWorkspaceRequiresLerosMarker(t *testing.T) {
	tool := NewTool()
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "report.md")
	writeFile(t, artifactPath, "hello")

	_, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{"path": artifactPath}))
	if err == nil || !strings.Contains(err.Error(), ".leros not found") {
		t.Fatalf("Execute() error = %v, want .leros marker error", err)
	}
}

func TestExecute_MultipleDeclarations(t *testing.T) {
	tool := NewTool()
	ctx, manifestPath := newToolContext(t)
	writeRepoFile(t, ctx, "artifacts/one.md", "one")
	writeRepoFile(t, ctx, "artifacts/two.md", "two")

	for _, path := range []string{"artifacts/one.md", "artifacts/two.md"} {
		if _, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{"path": repoPathFromContext(t, ctx, path)})); err != nil {
			t.Fatalf("Execute(%q) error = %v", path, err)
		}
	}

	entries := readManifestEntries(t, manifestPath)
	if len(entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2", len(entries))
	}
}

func TestExecute_Concurrent(t *testing.T) {
	tool := NewTool()
	ctx, manifestPath := newToolContext(t)
	paths := []string{"artifacts/a.md", "artifacts/b.md", "artifacts/c.md", "artifacts/d.md", "artifacts/e.md"}
	for _, path := range paths {
		writeRepoFile(t, ctx, path, path)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(paths))
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			_, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{"path": repoPathFromContext(t, ctx, path)}))
			errCh <- err
		}(path)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("Execute() concurrent error = %v", err)
		}
	}

	entries := readManifestEntries(t, manifestPath)
	if len(entries) != len(paths) {
		t.Fatalf("manifest entries = %d, want %d", len(entries), len(paths))
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.Path] = true
	}
	for _, path := range paths {
		if !seen[path] {
			t.Fatalf("manifest missing path %q", path)
		}
	}
}

func newToolContext(t *testing.T) (context.Context, string) {
	t.Helper()
	repoDir := t.TempDir()
	manifestPath := filepath.Join(repoDir, "artifacts.jsonl")
	ctx := tools.ContextWithToolContext(context.Background(), tools.ToolContext{
		Metadata: tools.ToolMetadata{
			ArtifactManifestPath: manifestPath,
			RepoDir:              repoDir,
		},
	})
	return ctx, manifestPath
}

func writeRepoFile(t *testing.T, ctx context.Context, relativePath string, content string) {
	t.Helper()
	absolutePath := repoPathFromContext(t, ctx, relativePath)
	writeFile(t, absolutePath, content)
}

func writeFile(t *testing.T, absolutePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func mkdirWithTime(t *testing.T, stopAt string, path string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	stopAt = filepath.Clean(stopAt)
	for current := filepath.Clean(path); current != "" && current != filepath.Dir(current); current = filepath.Dir(current) {
		if current == stopAt {
			break
		}
		if err := os.Chtimes(current, modTime, modTime); err != nil {
			t.Fatalf("Chtimes(%q) error = %v", current, err)
		}
	}
}

func repoPathFromContext(t *testing.T, ctx context.Context, relativePath string) string {
	t.Helper()
	toolCtx, ok := tools.ToolContextFrom(ctx)
	if !ok {
		t.Fatal("tool context missing")
	}
	repoDir := toolCtx.Metadata.RepoDir
	return filepath.Join(repoDir, filepath.FromSlash(relativePath))
}

func readManifestEntries(t *testing.T, manifestPath string) []artifactManifestEntry {
	t.Helper()
	file, err := os.Open(manifestPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()

	entries := make([]artifactManifestEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry artifactManifestEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err() = %v", err)
	}
	return entries
}

func decodeJSONMap(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return decoded
}

// Ensure the tool implements the Tool interface.
var _ tools.Tool = (*Tool)(nil)
