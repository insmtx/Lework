package opencode

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

func TestOpenCodeUsesTaskRootForSharedSkills(t *testing.T) {
	taskDir := t.TempDir()
	root := openCodeConfigDir(cli.InvocationRequest{TaskDir: taskDir, WorkDir: t.TempDir()})
	if root != taskDir || filepath.Join(root, "skills") != filepath.Join(taskDir, "skills") {
		t.Fatalf("OpenCode config root = %q", root)
	}
	if filepath.Base(root) == ".opencode-runtime" {
		t.Fatalf("OpenCode used runtime-specific Skill root %q", root)
	}
}
