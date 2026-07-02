package config

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestWorkerConfigParsesWorkspaceRootAndLogLevel(t *testing.T) {
	var cfg WorkerConfig
	body := []byte("org_id: 1\nworker_id: 2\nworkspace_root: /tmp/leros-workspace\nbootstrap_token: test-bootstrap\nlog:\n  level: warn\n")

	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal worker config: %v", err)
	}

	if cfg.WorkspaceRoot != "/tmp/leros-workspace" {
		t.Fatalf("workspace root = %q", cfg.WorkspaceRoot)
	}
	if cfg.BootstrapToken != "test-bootstrap" {
		t.Fatalf("bootstrap token = %q", cfg.BootstrapToken)
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("log level = %q", cfg.Log.Level)
	}
}
