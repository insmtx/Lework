package nodetools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/tools"
)

type fakeNodeExecutor struct {
	calls   []nodeExecRequest
	results []nodeExecResult
	err     error
}

func (e *fakeNodeExecutor) Exec(ctx context.Context, req nodeExecRequest) (nodeExecResult, error) {
	e.calls = append(e.calls, req)
	if e.err != nil {
		return nodeExecResult{}, e.err
	}
	if len(e.results) == 0 {
		return nodeExecResult{}, nil
	}
	result := e.results[0]
	e.results = e.results[1:]
	return result, nil
}

func TestNodeShellToolExecute(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	executor := &fakeNodeExecutor{
		results: []nodeExecResult{{
			Stdout:   "ok\n",
			ExitCode: 0,
		}},
	}
	tool := newNodeShellToolWithExecutor(executor)

	rawOutput, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"command":     "pwd",
		"working_dir": workspaceRoot,
		"timeout":     1,
	}))

	if err != nil {
		t.Fatalf("execute node shell tool: %v", err)
	}
	output := decodeNodeToolOutput(t, rawOutput)

	if output["exit_code"] != float64(0) {
		t.Fatalf("expected exit code 0, got %#v", output["exit_code"])
	}
	if output["timeout"] != float64(minShellTimeout) {
		t.Fatalf("expected clamped timeout %d, got %#v", minShellTimeout, output["timeout"])
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(executor.calls))
	}
	call := executor.calls[0]
	expectedArgs := shellCommandArgs("pwd")
	if !equalStringSlices(call.Args, expectedArgs) {
		t.Fatalf("unexpected command args: %#v", call.Args)
	}
	expectedWorkDir, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		t.Fatalf("resolve workspace root: %v", err)
	}
	if call.WorkingDir != expectedWorkDir {
		t.Fatalf("unexpected working dir: %s", call.WorkingDir)
	}
}

func TestNodeShellToolDefaultsToRuntimeWorkDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)
	projectDir := filepath.Join(workspaceRoot, "projects", "42", "project_1", "repo")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	executor := &fakeNodeExecutor{
		results: []nodeExecResult{{
			Stdout:   "ok\n",
			ExitCode: 0,
		}},
	}
	tool := newNodeShellToolWithExecutor(executor)
	ctx := tools.ContextWithToolContext(context.Background(), tools.ToolContext{WorkDir: projectDir})

	_, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"command": "pwd",
	}))

	if err != nil {
		t.Fatalf("execute node shell tool: %v", err)
	}
	expectedProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("resolve project dir: %v", err)
	}
	if got := executor.calls[0].WorkingDir; got != expectedProjectDir {
		t.Fatalf("working dir = %q, want %q", got, expectedProjectDir)
	}
}

func TestShellCommandArgs(t *testing.T) {
	args := shellCommandArgs("pwd")

	if runtime.GOOS == "windows" {
		expected := []string{
			"powershell.exe",
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy",
			"Bypass",
			"-Command",
			"pwd",
		}
		if !equalStringSlices(args, expected) {
			t.Fatalf("unexpected windows shell args: %#v", args)
		}
		return
	}

	expected := []string{"sh", "-lc", "pwd"}
	if !equalStringSlices(args, expected) {
		t.Fatalf("unexpected unix shell args: %#v", args)
	}
}

func TestNodeFileReadToolExecute(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	path := filepath.Join(workspaceRoot, "app", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create test dir: %v", err)
	}
	content := strings.Join([]string{
		"line1",
		"line2",
		"alpha",
		"beta",
		"line5",
		"line6",
		"line7",
		"line8",
		"line9",
		"line10",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	tool := newNodeFileReadToolWithExecutor(nil)

	rawOutput, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":   "app/main.go",
		"offset": 3,
		"limit":  2,
	}))

	if err != nil {
		t.Fatalf("execute node file read tool: %v", err)
	}
	output := decodeNodeToolOutput(t, rawOutput)

	if output["content"] != "alpha\nbeta" {
		t.Fatalf("unexpected content: %#v", output["content"])
	}
	if output["shown_start"] != float64(3) || output["shown_end"] != float64(4) {
		t.Fatalf("unexpected shown range: %v-%v", output["shown_start"], output["shown_end"])
	}
	numbered := output["numbered_content"].(string)
	if !strings.Contains(numbered, "     3|alpha") || !strings.Contains(numbered, "     4|beta") {
		t.Fatalf("unexpected numbered content: %s", numbered)
	}
	if !output["has_more"].(bool) {
		t.Fatalf("expected has_more to be true")
	}
}

func TestNodeFileToolsResolveRelativeToRuntimeWorkDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)
	projectDir := filepath.Join(workspaceRoot, "projects", "42", "project_1", "repo")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	ctx := tools.ContextWithToolContext(context.Background(), tools.ToolContext{WorkDir: projectDir})

	writeTool := newNodeFileWriteToolWithExecutor(nil)
	_, err := writeTool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"path":    "app/main.go",
		"content": "package main\n",
	}))

	if err != nil {
		t.Fatalf("write relative to project dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "app", "main.go")); err != nil {
		t.Fatalf("expected project file to be written: %v", err)
	}

	readTool := newNodeFileReadToolWithExecutor(nil)
	rawOutput, err := readTool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"path": "app/main.go",
	}))

	if err != nil {
		t.Fatalf("read relative to project dir: %v", err)
	}
	output := decodeNodeToolOutput(t, rawOutput)
	if output["content"] != "package main" {
		t.Fatalf("unexpected content: %#v", output["content"])
	}

	_, err = writeTool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"path":    "../outside.txt",
		"content": "nope",
	}))

	if err == nil {
		t.Fatal("expected path outside runtime work dir to be rejected")
	}
}

func equalStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNodeFileWriteToolExecute(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	tool := newNodeFileWriteToolWithExecutor(nil)

	rawOutput, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":    "app/main.go",
		"content": "package main\n",
		"append":  true,
	}))

	if err != nil {
		t.Fatalf("execute node file write tool: %v", err)
	}
	output := decodeNodeToolOutput(t, rawOutput)

	if output["action"] != "appended" {
		t.Fatalf("unexpected action: %#v", output["action"])
	}
	if output["line_count"] != float64(1) {
		t.Fatalf("unexpected line count: %#v", output["line_count"])
	}
	if output["bytes_written"] != float64(len("package main\n")) {
		t.Fatalf("unexpected bytes written: %#v", output["bytes_written"])
	}
	written, err := os.ReadFile(filepath.Join(workspaceRoot, "app", "main.go"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(written) != "package main\n" {
		t.Fatalf("unexpected written content: %q", string(written))
	}
}

func TestNodeFileWriteToolAllowsExplicitEmptyContent(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	tool := newNodeFileWriteToolWithExecutor(nil)
	rawOutput, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":    "app/empty.txt",
		"content": "",
	}))

	if err != nil {
		t.Fatalf("execute node file write tool: %v", err)
	}
	output := decodeNodeToolOutput(t, rawOutput)

	if output["line_count"] != float64(0) {
		t.Fatalf("unexpected line count: %#v", output["line_count"])
	}
	if output["bytes_written"] != float64(0) {
		t.Fatalf("unexpected bytes written: %#v", output["bytes_written"])
	}
	written, err := os.ReadFile(filepath.Join(workspaceRoot, "app", "empty.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if len(written) != 0 {
		t.Fatalf("expected empty file, got %d bytes", len(written))
	}
}

func TestNodeFileReadRejectsSymlinkOutsideWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	outsideRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	outsidePath := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	createSymlinkOrSkip(t, outsidePath, filepath.Join(workspaceRoot, "secret-link.txt"))

	tool := newNodeFileReadToolWithExecutor(nil)
	_, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path": "secret-link.txt",
	}))

	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodeFileWriteRejectsSymlinkOutsideWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	outsideRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	outsidePath := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	createSymlinkOrSkip(t, outsidePath, filepath.Join(workspaceRoot, "secret-link.txt"))

	tool := newNodeFileWriteToolWithExecutor(nil)
	_, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":    "secret-link.txt",
		"content": "updated",
	}))

	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(data) != "secret" {
		t.Fatalf("outside file should not be modified: %q", string(data))
	}
}

func TestNodeFileWriteRejectsSymlinkParentOutsideWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	outsideRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)

	createSymlinkOrSkip(t, outsideRoot, filepath.Join(workspaceRoot, "outside-link"))

	tool := newNodeFileWriteToolWithExecutor(nil)
	_, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"path":    "outside-link/new.txt",
		"content": "updated",
	}))

	if err == nil {
		t.Fatal("expected symlink parent escape to be rejected")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outsideRoot, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside file should not be created, stat err=%v", statErr)
	}
}

func TestNodeToolValidateUsesLocalInputs(t *testing.T) {
	if err := newNodeShellToolWithExecutor(nil).Validate(tools.JSONInput(map[string]interface{}{
		"command": "pwd",
	})); err != nil {
		t.Fatalf("shell validate should accept local input: %v", err)
	}
	if err := newNodeFileReadToolWithExecutor(nil).Validate(tools.JSONInput(map[string]interface{}{
		"path": "app/main.go",
	})); err != nil {
		t.Fatalf("file read validate should accept local input: %v", err)
	}
	if err := newNodeFileWriteToolWithExecutor(nil).Validate(tools.JSONInput(map[string]interface{}{
		"path":    "app/main.go",
		"content": "package main\n",
	})); err != nil {
		t.Fatalf("file write validate should accept local input: %v", err)
	}
}

func decodeNodeToolOutput(t *testing.T, output string) map[string]interface{} {
	t.Helper()

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode node tool output: %v\n%s", err, output)
	}
	return decoded
}

func createSymlinkOrSkip(t *testing.T, oldname string, newname string) {
	t.Helper()

	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlinks requires elevated privileges on Windows: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}
}
