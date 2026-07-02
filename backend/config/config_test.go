package config

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestConfigParsesWorkspaceRootAndLogLevel(t *testing.T) {
	var cfg Config
	body := []byte("workspace_root: /tmp/leros\nlog:\n  level: error\nserver:\n  port: \"8080\"\n")

	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if cfg.WorkspaceRoot != "/tmp/leros" {
		t.Fatalf("workspace root = %q", cfg.WorkspaceRoot)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("log level = %q", cfg.Log.Level)
	}
}
