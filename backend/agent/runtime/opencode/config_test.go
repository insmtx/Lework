package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureOpenCodeDBPathUsesConfiguredDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), openCodeDataDirName)

	path, err := ensureOpenCodeDBPath(dataDir)
	if err != nil {
		t.Fatalf("ensure opencode database path: %v", err)
	}

	want := filepath.Join(dataDir, openCodeDBName)
	if path != want {
		t.Fatalf("database path = %q, want %q", path, want)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat opencode data directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("opencode data path %q is not a directory", info.Name())
	}
}

func TestBuildServerEnvOverridesInheritedOpenCodeDB(t *testing.T) {
	env := buildServerEnv(
		"secret",
		"{}",
		"/workspace/.opencode/opencode.db",
		[]string{"OPENCODE_DB=/tmp/inherited.db"},
	)

	assertEnvContains(t, env, "OPENCODE_DB=/workspace/.opencode/opencode.db")
	for _, item := range env {
		if item == "OPENCODE_DB=/tmp/inherited.db" {
			t.Fatalf("inherited OPENCODE_DB was not overridden: %#v", env)
		}
	}
}
