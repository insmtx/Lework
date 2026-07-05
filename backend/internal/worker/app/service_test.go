package app

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestExternalCLIDataDirUsesRuntimeKindUnderWorkspace(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	tests := []struct {
		name string
		kind string
		want string
	}{
		{name: "opencode", kind: "opencode", want: ".opencode"},
		{name: "codex", kind: "codex", want: ".codex"},
		{name: "claude", kind: "claude", want: ".claude"},
		{name: "normalizes kind", kind: "  OpenCode  ", want: ".opencode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := externalCLIDataDir(tt.kind)
			if err != nil {
				t.Fatalf("externalCLIDataDir() error = %v", err)
			}
			want := filepath.Join(workspaceRoot, tt.want)
			if got != want {
				t.Fatalf("externalCLIDataDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestExternalCLIDataDirRequiresRuntimeKind(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, t.TempDir())

	if _, err := externalCLIDataDir("  "); err == nil {
		t.Fatal("externalCLIDataDir() error = nil, want error")
	}
}
