package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	"github.com/insmtx/lework/backend/internal/agentd/handler"
	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/pair"
	"github.com/insmtx/lework/backend/internal/agentd/servicefile"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
)

// clearOpenCodePasswordEnv makes a test independent of the daemon's ambient
// environment, which resolveOpenCodePassword deliberately consults because the
// child inherits it.
func clearOpenCodePasswordEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_PASSWORD", "")
	t.Setenv("OPENCODE_SERVER_PASSWORD", "")
}

func TestResolveOpenCodePasswordPrefersOpenCodePassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)

	env := map[string]string{
		"OPENCODE_PASSWORD":        "primary",
		"OPENCODE_SERVER_PASSWORD": "fallback",
	}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "primary" {
		t.Fatalf("password = %q, want OPENCODE_PASSWORD value", password)
	}
	if generated {
		t.Fatal("generated = true, want false for a configured password")
	}
}

func TestResolveOpenCodePasswordUsesServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)

	env := map[string]string{"OPENCODE_SERVER_PASSWORD": "server"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "server" {
		t.Fatalf("password = %q, want OPENCODE_SERVER_PASSWORD value", password)
	}
	if generated {
		t.Fatal("generated = true, want false for a configured password")
	}
}

func TestResolveOpenCodePasswordUsesInheritedOpenCodePassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false when the process env provides a password")
	}
}

func TestResolveOpenCodePasswordUsesInheritedServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_SERVER_PASSWORD", "inherited-server")

	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited-server" {
		t.Fatalf("password = %q, want the inherited OPENCODE_SERVER_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false when the process env provides a password")
	}
}

func TestResolveOpenCodePasswordInheritedOpenCodePasswordBeatsYAMLServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	// The child keeps the inherited OPENCODE_PASSWORD and reads it before the
	// yaml-supplied OPENCODE_SERVER_PASSWORD, so the inherited value wins.
	env := map[string]string{"OPENCODE_SERVER_PASSWORD": "yaml-server"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false")
	}
}

func TestResolveOpenCodePasswordYAMLPasswordBeatsInheritedServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_SERVER_PASSWORD", "inherited-server")

	env := map[string]string{"OPENCODE_PASSWORD": "yaml-primary"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "yaml-primary" {
		t.Fatalf("password = %q, want the yaml OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false")
	}
}

func TestResolveOpenCodePasswordGeneratesURLSafeValue(t *testing.T) {
	clearOpenCodePasswordEnv(t)

	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if !generated {
		t.Fatal("generated = false, want true when no password is configured")
	}
	if password == "" {
		t.Fatal("generated password is empty")
	}
	if strings.ContainsAny(password, "+/=") {
		t.Fatalf("generated password %q is not unpadded base64url", password)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(password)
	if err != nil {
		t.Fatalf("generated password is not base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("generated password decodes to %d bytes, want 32", len(decoded))
	}
}

func TestOpenCodeEnvDoesNotMutateInput(t *testing.T) {
	clearOpenCodePasswordEnv(t)

	input := map[string]string{"OPENCODE_LOG": "debug"}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password == "" {
		t.Fatal("openCodeEnv() returned an empty password")
	}

	if len(input) != 1 {
		t.Fatalf("input map gained keys: %v", input)
	}
	if _, ok := input["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("input map was given the generated password")
	}

	if env["OPENCODE_LOG"] != "debug" {
		t.Fatalf("copied env lost OPENCODE_LOG: %v", env)
	}
	if env["OPENCODE_SERVER_PASSWORD"] != password {
		t.Fatalf("copied env password = %q, want %q", env["OPENCODE_SERVER_PASSWORD"], password)
	}

	// The copy must be independent, not an alias of the loaded config.
	env["OPENCODE_LOG"] = "trace"
	if input["OPENCODE_LOG"] != "debug" {
		t.Fatalf("mutating the copy changed the input: %v", input)
	}
}

func TestOpenCodeEnvKeepsConfiguredPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)

	input := map[string]string{"OPENCODE_SERVER_PASSWORD": "configured"}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "configured" {
		t.Fatalf("password = %q, want configured", password)
	}
	if env["OPENCODE_SERVER_PASSWORD"] != "configured" {
		t.Fatalf("env password = %q, want configured", env["OPENCODE_SERVER_PASSWORD"])
	}
}

func TestOpenCodeEnvUsesInheritedPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	env, password, err := openCodeEnv(map[string]string{"OPENCODE_LOG": "debug"})
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if _, ok := env["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("a generated password was injected despite the inherited password")
	}
}

func TestOpenCodeEnvEmptyYAMLPasswordDoesNotShadowInherited(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	// `OPENCODE_PASSWORD: ""` in agent.yaml must mean "unset" instead of
	// overriding the inherited value when mergeEnv overlays the child env.
	input := map[string]string{
		"OPENCODE_PASSWORD":        "",
		"OPENCODE_SERVER_PASSWORD": "",
		"OPENCODE_LOG":             "debug",
	}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if _, ok := env["OPENCODE_PASSWORD"]; ok {
		t.Fatal("an empty OPENCODE_PASSWORD reached the child env")
	}
	if _, ok := env["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("an empty OPENCODE_SERVER_PASSWORD reached the child env")
	}
	if env["OPENCODE_LOG"] != "debug" {
		t.Fatalf("unrelated env entry was dropped: %v", env)
	}
}

// newTestAgentRuntime builds the smallest runtime that can exercise the
// publish/unpublish wiring without a real OpenCode process. A canceled context
// makes Run return as soon as it reaches the reconnect loop.
func newTestAgentRuntime(t *testing.T, port int, password string) *agentRuntime {
	t.Helper()

	cfg := &agentdconfig.Config{
		NodeName:        "test-node",
		ControlPlaneURL: "ws://127.0.0.1:1",
		OpenCode:        agentdconfig.OpenCodeConfig{Port: port},
	}
	client := opencode.New("http://127.0.0.1:1", nil)
	service := supervisor.New(nil, nil, supervisor.ServiceConfig{Port: port})
	return &agentRuntime{
		cfg:      cfg,
		client:   client,
		service:  service,
		handler:  handler.New(client, service),
		password: password,
	}
}

func TestAgentRunPublishesServiceAndCloseRemovesIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runtime := newTestAgentRuntime(t, 4097, "pw")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil for a canceled context", err)
	}

	info, err := servicefile.Read()
	if err != nil {
		t.Fatalf("Read() after Run error = %v", err)
	}
	if info.URL != "http://127.0.0.1:4097" {
		t.Fatalf("published URL = %q, want http://127.0.0.1:4097", info.URL)
	}
	if info.Password != "pw" {
		t.Fatalf("published password = %q, want the agent password", info.Password)
	}
	if info.Port != 4097 {
		t.Fatalf("published port = %d, want 4097", info.Port)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("published pid = %d, want %d", info.PID, os.Getpid())
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := servicefile.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read() after Close error = %v, want fs.ErrNotExist", err)
	}
}

func TestAgentCloseKeepsAnotherProcessServiceFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runtime := newTestAgentRuntime(t, 4097, "pw")

	foreign := servicefile.Info{
		URL:      "http://127.0.0.1:4098",
		Password: "other",
		Port:     4098,
		PID:      os.Getpid() + 1,
	}
	if err := servicefile.Write(foreign); err != nil {
		t.Fatalf("Write() foreign info error = %v", err)
	}
	runtime.unpublishService()
	if _, err := servicefile.Read(); err != nil {
		t.Fatalf("foreign service file was removed: %v", err)
	}

	own := servicefile.Info{
		URL:      "http://127.0.0.1:4097",
		Password: "pw",
		Port:     4097,
		PID:      os.Getpid(),
	}
	if err := servicefile.Write(own); err != nil {
		t.Fatalf("Write() own info error = %v", err)
	}
	runtime.unpublishService()
	if _, err := servicefile.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("own service file still present: %v", err)
	}
}

func TestResetCredentialStateRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "node.credential")
	claimPath := pair.ClaimPath(credPath)
	if err := os.WriteFile(credPath, []byte("cred"), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	if err := os.WriteFile(claimPath, []byte("claim"), 0o600); err != nil {
		t.Fatalf("write claim: %v", err)
	}

	if err := resetCredentialState(credPath); err != nil {
		t.Fatalf("resetCredentialState: %v", err)
	}
	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Fatalf("credential file still exists: %v", err)
	}
	if _, err := os.Stat(claimPath); !os.IsNotExist(err) {
		t.Fatalf("claim file still exists: %v", err)
	}
	// 文件本来就不存在时也成功（幂等）。
	if err := resetCredentialState(credPath); err != nil {
		t.Fatalf("second resetCredentialState: %v", err)
	}
}

func TestResetCredentialStateRejectsEnvCredential(t *testing.T) {
	t.Setenv("LEWORK_NODE_CREDENTIAL", "from-env")
	err := resetCredentialState(filepath.Join(t.TempDir(), "node.credential"))
	if err == nil || !strings.Contains(err.Error(), "LEWORK_NODE_CREDENTIAL") {
		t.Fatalf("err = %v, want error mentioning LEWORK_NODE_CREDENTIAL", err)
	}
}

func TestResetIgnoresEnrollmentToken(t *testing.T) {
	plan, err := planPairing(false, "", "/tmp/cred", "", "token-abc", true)
	if err != nil {
		t.Fatalf("planPairing: %v", err)
	}
	if plan != pairPlanInteractive {
		t.Fatalf("plan = %v, want interactive pairing", plan)
	}
}
