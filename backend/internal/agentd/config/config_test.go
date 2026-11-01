package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigDefaults(t *testing.T) {
	path := writeConfig(t, `
control_plane_url: ws://127.0.0.1:7443/api/agent/connect
node_name: dev
workspace_roots:
  - /tmp/work
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenCode.Port != 4096 {
		t.Fatalf("port = %d, want 4096", cfg.OpenCode.Port)
	}
	if cfg.OpenCode.ServiceMode != "shared" {
		t.Fatalf("service_mode = %q, want shared", cfg.OpenCode.ServiceMode)
	}
}

func TestConfigValidateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing url", Config{}, "control_plane_url"},
		{"bad scheme", Config{ControlPlaneURL: "http://x"}, "ws or wss"},
		{"no roots", Config{ControlPlaneURL: "ws://x", OpenCode: OpenCodeConfig{Port: 4096}}, "workspace_roots"},
		{"relative root", Config{ControlPlaneURL: "ws://x", WorkspaceRoots: []string{"rel"}, OpenCode: OpenCodeConfig{Port: 4096}}, "absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}
