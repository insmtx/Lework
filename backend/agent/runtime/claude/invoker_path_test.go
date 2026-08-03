package claude

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

func TestClaudeUsesTaskRootForSharedSkills(t *testing.T) {
	taskDir := t.TempDir()
	root := claudeConfigDirFor(cli.InvocationRequest{TaskDir: taskDir, WorkDir: t.TempDir()})
	if root != taskDir || filepath.Join(root, "skills") != filepath.Join(taskDir, "skills") {
		t.Fatalf("Claude config root = %q", root)
	}
	if filepath.Base(root) == ".claude-runtime" {
		t.Fatalf("Claude used runtime-specific Skill root %q", root)
	}
}
