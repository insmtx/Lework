package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

func TestResolveAgentConfigDerivesWhenNoFile(t *testing.T) {
	defaultAgentConfigPath = func() string { return filepath.Join(t.TempDir(), "absent-agent.yaml") }
	t.Cleanup(func() { defaultAgentConfigPath = defaultConfigPath })
	defaultWorkspaceBase = filepath.Join(t.TempDir(), "opt")
	t.Cleanup(func() { defaultWorkspaceBase = "/opt/lework" })
	currentUsername = func() (string, error) { return "u", nil }
	t.Cleanup(func() { currentUsername = osUsername })
	hostname = func() (string, error) { return "box", nil }
	t.Cleanup(func() { hostname = os.Hostname })

	serveCfg := controlplaneconfig.Config{ListenAddr: "0.0.0.0:8787", EnrollmentToken: "tok-value-1234567"}
	cfg, err := resolveAgentConfig(serveCfg, agentOverrides{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.ControlPlaneURL != "ws://127.0.0.1:8787/api/agent/connect" {
		t.Fatalf("url = %q", cfg.ControlPlaneURL)
	}
	if cfg.NodeName != "box" {
		t.Fatalf("node = %q", cfg.NodeName)
	}
	wantRoot := filepath.Join(defaultWorkspaceBase, "u", "workspace")
	if len(cfg.WorkspaceRoots) != 1 || cfg.WorkspaceRoots[0] != wantRoot {
		t.Fatalf("roots = %v, want [%s]", cfg.WorkspaceRoots, wantRoot)
	}
	// 默认目录在 /opt 下，首次启动必须自己建好，否则第一个会话就被拒。
	if info, err := os.Stat(wantRoot); err != nil || !info.IsDir() {
		t.Fatalf("default workspace root not created: %v", err)
	}
	if cfg.OpenCode.Port != 4096 {
		t.Fatalf("opencode port = %d", cfg.OpenCode.Port)
	}
}

func TestResolveAgentConfigFileWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	content := `control_plane_url: ws://other:9999/api/agent/connect
enrollment_token: file-token-123456
node_name: file-node
workspace_roots:
  - /srv
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}

	cfg, err := resolveAgentConfig(
		controlplaneconfig.Config{ListenAddr: "127.0.0.1:8787", EnrollmentToken: "serve-token-1234"},
		agentOverrides{configPath: path, configPathExplicit: true},
	)
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.ControlPlaneURL != "ws://other:9999/api/agent/connect" {
		t.Fatalf("url = %q", cfg.ControlPlaneURL)
	}
	if cfg.EnrollmentToken != "file-token-123456" {
		t.Fatalf("token = %q", cfg.EnrollmentToken)
	}
	if cfg.NodeName != "file-node" {
		t.Fatalf("node = %q", cfg.NodeName)
	}
	if len(cfg.WorkspaceRoots) != 1 || cfg.WorkspaceRoots[0] != "/srv" {
		t.Fatalf("roots = %v", cfg.WorkspaceRoots)
	}
}

func TestResolveAgentConfigRejectsPortZero(t *testing.T) {
	defaultAgentConfigPath = func() string { return filepath.Join(t.TempDir(), "absent-agent.yaml") }
	t.Cleanup(func() { defaultAgentConfigPath = defaultConfigPath })
	serveCfg := controlplaneconfig.Config{ListenAddr: "127.0.0.1:0", EnrollmentToken: "tok-value-1234567"}
	_, err := resolveAgentConfig(serveCfg, agentOverrides{})
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("err = %v, want a port error", err)
	}
}

// TestResolveAgentConfigRejectsZeroPaddedPort 钉住：":00" 这类前导零端口按数值
// 同样是 0，必须拒绝，而不是只比较字符串 "0" 后放过一个连不上的地址。
func TestResolveAgentConfigRejectsZeroPaddedPort(t *testing.T) {
	defaultAgentConfigPath = func() string { return filepath.Join(t.TempDir(), "absent-agent.yaml") }
	t.Cleanup(func() { defaultAgentConfigPath = defaultConfigPath })
	for _, listenAddr := range []string{"127.0.0.1:00", ":00"} {
		t.Run(listenAddr, func(t *testing.T) {
			serveCfg := controlplaneconfig.Config{ListenAddr: listenAddr, EnrollmentToken: "tok-value-1234567"}
			_, err := resolveAgentConfig(serveCfg, agentOverrides{})
			if err == nil || !strings.Contains(err.Error(), "port") {
				t.Fatalf("err = %v, want a port error", err)
			}
		})
	}
}

func TestResolveAgentConfigMissingUserIsActionable(t *testing.T) {
	defaultAgentConfigPath = func() string { return filepath.Join(t.TempDir(), "absent-agent.yaml") }
	t.Cleanup(func() { defaultAgentConfigPath = defaultConfigPath })
	currentUsername = func() (string, error) { return "", errors.New("no user") }
	t.Cleanup(func() { currentUsername = osUsername })
	serveCfg := controlplaneconfig.Config{ListenAddr: "127.0.0.1:8787", EnrollmentToken: "tok-value-1234567"}
	_, err := resolveAgentConfig(serveCfg, agentOverrides{})
	if err == nil || !strings.Contains(err.Error(), "--workspace-root") {
		t.Fatalf("err = %v", err)
	}
}

// TestApplyDefaultWorkspaceRoots 覆盖独立 `lework agent` 的默认值：agent.yaml
// 没写 workspace_roots 时也要落到 /opt/lework/<用户>/workspace 并建好目录。
func TestApplyDefaultWorkspaceRoots(t *testing.T) {
	defaultWorkspaceBase = filepath.Join(t.TempDir(), "opt")
	t.Cleanup(func() { defaultWorkspaceBase = "/opt/lework" })
	currentUsername = func() (string, error) { return "svc", nil }
	t.Cleanup(func() { currentUsername = osUsername })

	cfg := &agentdconfig.Config{}
	if err := applyDefaultWorkspaceRoots(cfg); err != nil {
		t.Fatalf("applyDefaultWorkspaceRoots: %v", err)
	}
	want := filepath.Join(defaultWorkspaceBase, "svc", "workspace")
	if len(cfg.WorkspaceRoots) != 1 || cfg.WorkspaceRoots[0] != want {
		t.Fatalf("roots = %v, want [%s]", cfg.WorkspaceRoots, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("default workspace root not created: %v", err)
	}
}

func TestApplyDefaultWorkspaceRootsKeepsExplicit(t *testing.T) {
	cfg := &agentdconfig.Config{WorkspaceRoots: []string{"/srv"}}
	if err := applyDefaultWorkspaceRoots(cfg); err != nil {
		t.Fatalf("applyDefaultWorkspaceRoots: %v", err)
	}
	if len(cfg.WorkspaceRoots) != 1 || cfg.WorkspaceRoots[0] != "/srv" {
		t.Fatalf("roots = %v", cfg.WorkspaceRoots)
	}
}

// TestResolveAgentConfigCreatesOnlyDefaultRoot 钉住：只创建默认目录，
// 用户显式给出的 --workspace-root 保持原样，不能替用户铺目录。
func TestResolveAgentConfigCreatesOnlyDefaultRoot(t *testing.T) {
	defaultAgentConfigPath = func() string { return filepath.Join(t.TempDir(), "absent-agent.yaml") }
	t.Cleanup(func() { defaultAgentConfigPath = defaultConfigPath })
	hostname = func() (string, error) { return "box", nil }
	t.Cleanup(func() { hostname = os.Hostname })

	explicit := filepath.Join(t.TempDir(), "explicit")
	cfg, err := resolveAgentConfig(
		controlplaneconfig.Config{ListenAddr: "127.0.0.1:8787", EnrollmentToken: "tok-value-1234567"},
		agentOverrides{workspaceRoots: []string{explicit}},
	)
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if len(cfg.WorkspaceRoots) != 1 || cfg.WorkspaceRoots[0] != explicit {
		t.Fatalf("roots = %v", cfg.WorkspaceRoots)
	}
	if _, err := os.Stat(explicit); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit root was created: %v", err)
	}
}

// TestResolveAgentConfigMissingExplicitFileIsError 钉住：显式 --agent-config
// 指向不存在的文件时直接报错，而不是静默退回派生默认值。
func TestResolveAgentConfigMissingExplicitFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	_, err := resolveAgentConfig(
		controlplaneconfig.Config{ListenAddr: "127.0.0.1:8787", EnrollmentToken: "tok-value-1234567"},
		agentOverrides{configPath: path, configPathExplicit: true},
	)
	if err == nil {
		t.Fatalf("err = nil, want a read error for %s", path)
	}
}

// TestResolveAgentConfigFillsEmptyFieldsOnly 确认派生阶段只补空字段：
// 文件给出的 URL 保持不变，而空 token 由 serve 配置补齐。
func TestResolveAgentConfigFillsEmptyFieldsOnly(t *testing.T) {
	hostname = func() (string, error) { return "box", nil }
	t.Cleanup(func() { hostname = os.Hostname })

	path := filepath.Join(t.TempDir(), "agent.yaml")
	content := `control_plane_url: ws://127.0.0.1:8787/api/agent/connect
node_name: file-node
workspace_roots:
  - /srv
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}

	cfg, err := resolveAgentConfig(
		controlplaneconfig.Config{ListenAddr: "127.0.0.1:8787", EnrollmentToken: "tok-value-1234567"},
		agentOverrides{configPath: path},
	)
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.EnrollmentToken != "tok-value-1234567" {
		t.Fatalf("token = %q, want the serve token", cfg.EnrollmentToken)
	}
	if cfg.NodeName != "file-node" {
		t.Fatalf("node = %q, want the file node", cfg.NodeName)
	}
}
