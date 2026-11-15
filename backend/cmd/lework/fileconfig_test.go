package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/insmtx/lework/backend/config"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// writeConfig 把内容写进临时文件，供各用例复用。
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lework.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// noFlags 模拟“命令行没有传任何相关 flag”。
func noFlags(string) bool { return false }

func TestLoadUnifiedConfig(t *testing.T) {
	path := writeConfig(t, `
controlplane:
  listen: 127.0.0.1:9999
  token_file: ~/.config/Lework/enrollment.token
  request_timeout: 12s
  heartbeat_timeout: 45s
  public_url: http://dev.ckeyer.com
  expo_url: http://dev.ckeyer.com:8082
agent:
  node_name: dev-box
  workspace_roots:
    - /opt/lework/u/workspace
  opencode:
    port: 4599
    service_mode: per-project
`)

	file, err := loadConfigFile(path, true)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}

	cfg := controlplaneconfig.Default()
	tokenFile := ""
	pair := pairOptions{}
	file.applyControlPlane(&cfg, &tokenFile, &pair, noFlags)

	if cfg.ListenAddr != "127.0.0.1:9999" {
		t.Fatalf("listen = %q", cfg.ListenAddr)
	}
	if cfg.RequestTimeout != 12*time.Second || cfg.HeartbeatTimeout != 45*time.Second {
		t.Fatalf("timeouts = %v/%v", cfg.RequestTimeout, cfg.HeartbeatTimeout)
	}
	if !strings.HasSuffix(tokenFile, filepath.Join("Lework", "enrollment.token")) {
		t.Fatalf("token file = %q", tokenFile)
	}
	if pair.publicURL != "http://dev.ckeyer.com" || pair.expoURL != "http://dev.ckeyer.com:8082" {
		t.Fatalf("pair = %+v", pair)
	}

	agent, err := file.agentBase()
	if err != nil {
		t.Fatalf("agentBase: %v", err)
	}
	if agent == nil {
		t.Fatal("agentBase = nil, want the agent section")
	}
	if agent.NodeName != "dev-box" || len(agent.WorkspaceRoots) != 1 {
		t.Fatalf("agent = %+v", agent)
	}
	if agent.OpenCode.Port != 4599 || agent.OpenCode.ServiceMode != "per-project" {
		t.Fatalf("opencode = %+v", agent.OpenCode)
	}
	// 文件没写的字段由 ApplyDefaults 补上。
	if agent.OpenCode.Binary != "opencode" {
		t.Fatalf("opencode binary = %q", agent.OpenCode.Binary)
	}
}

func TestApplyControlPlanePrefersExplicitFlags(t *testing.T) {
	path := writeConfig(t, `
controlplane:
  listen: 127.0.0.1:9999
  public_url: http://config.example
`)
	file, err := loadConfigFile(path, true)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}

	cfg := controlplaneconfig.Default()
	cfg.ListenAddr = "127.0.0.1:1234"
	pair := pairOptions{publicURL: "http://flag.example"}
	file.applyControlPlane(&cfg, nil, &pair, func(name string) bool {
		return name == "listen" || name == "public-url"
	})

	if cfg.ListenAddr != "127.0.0.1:1234" {
		t.Fatalf("listen = %q, want the flag value", cfg.ListenAddr)
	}
	if pair.publicURL != "http://flag.example" {
		t.Fatalf("public url = %q, want the flag value", pair.publicURL)
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	if _, err := loadConfigFile(missing, true); err == nil {
		t.Fatal("explicit missing config should fail")
	}
	file, err := loadConfigFile(missing, false)
	if err != nil {
		t.Fatalf("default missing config should be ignored: %v", err)
	}
	if agent, err := file.agentBase(); err != nil || agent != nil {
		t.Fatalf("agentBase = %v, %v; want nil, nil", agent, err)
	}
}

// TestAgentBaseAcceptsFlatAgentFile 钉住向后兼容：没有 agent: 段的旧
// agent.yaml 仍然按 agent 配置解析。
func TestAgentBaseAcceptsFlatAgentFile(t *testing.T) {
	path := writeConfig(t, `
control_plane_url: ws://127.0.0.1:8787/api/agent/connect
node_name: flat-node
workspace_roots:
  - /srv
`)
	file, err := loadConfigFile(path, true)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	agent, err := file.agentBase()
	if err != nil {
		t.Fatalf("agentBase: %v", err)
	}
	if agent == nil || agent.NodeName != "flat-node" {
		t.Fatalf("agent = %+v", agent)
	}
}

// TestAgentBaseIgnoresControlPlaneOnlyFile 钉住：只写 controlplane: 的文件
// 不能被当成空 agent 配置，否则会覆盖默认的 agent.yaml。
func TestAgentBaseIgnoresControlPlaneOnlyFile(t *testing.T) {
	path := writeConfig(t, `
controlplane:
  listen: 127.0.0.1:8787
`)
	file, err := loadConfigFile(path, true)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	agent, err := file.agentBase()
	if err != nil || agent != nil {
		t.Fatalf("agentBase = %v, %v; want nil, nil", agent, err)
	}
	if _, err := loadAgentConfig(path); err == nil || !strings.Contains(err.Error(), "no agent section") {
		t.Fatalf("loadAgentConfig error = %v", err)
	}
}

// TestApplyControlPlaneReadsNATSandDatabase 钉住顶层 nats:/database: 段会写入
// 控制面配置，且显式 flag（changed 为 true）永远优先于文件。
func TestApplyControlPlaneReadsNATSandDatabase(t *testing.T) {
	path := writeConfig(t, `
nats:
  url: nats://127.0.0.1:4222
database:
  url: postgres://lework:lework@127.0.0.1:5432/lework
  debug: true
`)
	file, err := loadConfigFile(path, true)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}

	cfg := controlplaneconfig.Default()
	file.applyControlPlane(&cfg, nil, nil, noFlags)

	if cfg.NATS == nil || cfg.NATS.URL != "nats://127.0.0.1:4222" {
		t.Fatalf("nats = %+v, want the url from the file", cfg.NATS)
	}
	if cfg.Database == nil {
		t.Fatal("database = nil, want the url from the file")
	}
	if cfg.Database.URL != "postgres://lework:lework@127.0.0.1:5432/lework" {
		t.Fatalf("database url = %q", cfg.Database.URL)
	}
	if !cfg.Database.Debug {
		t.Fatal("database debug = false, want true from the file")
	}

	// 真实启动路径是 applyControlPlane + applyClusterFlags：文件先写入，
	// 显式 flag 再覆盖 URL。这里复现该顺序，确认非 URL 字段不会被弄丢。
	explicit := controlplaneconfig.Default()
	explicit.NATS = &appconfig.NATSConfig{URL: "nats://flag.example:4222"}
	explicit.Database = &appconfig.DatabaseConfig{URL: "postgres://flag.example:5432/db"}
	changed := func(name string) bool {
		return name == "nats-url" || name == "database-url"
	}
	file.applyControlPlane(&explicit, nil, nil, changed)
	applyClusterFlags(&explicit, "nats://flag.example:4222", "postgres://flag.example:5432/db", changed)

	if explicit.NATS.URL != "nats://flag.example:4222" {
		t.Fatalf("nats url = %q, want the flag value", explicit.NATS.URL)
	}
	if explicit.Database.URL != "postgres://flag.example:5432/db" {
		t.Fatalf("database url = %q, want the flag value", explicit.Database.URL)
	}
	if !explicit.Database.Debug {
		t.Fatal("database debug = false, want true preserved from the file when only the flag url is set")
	}
}

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if got := expandHomePath("~/x/y"); got != filepath.Join(home, "x/y") {
		t.Fatalf("expandHomePath(~/x/y) = %q", got)
	}
	if got := expandHomePath("/abs/path"); got != "/abs/path" {
		t.Fatalf("expandHomePath(/abs/path) = %q", got)
	}
	if got := expandHomePath("relative"); got != "relative" {
		t.Fatalf("expandHomePath(relative) = %q", got)
	}
}
