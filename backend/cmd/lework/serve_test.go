package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// executeServeCommand 以给定参数执行 serve 命令并捕获输出。
func executeServeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newServeCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func TestRootCommandHasServe(t *testing.T) {
	cmd, _, err := newRootCommand().Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	if cmd == nil || cmd.Name() != "serve" {
		t.Fatalf("find serve = %v, want the serve command", cmd)
	}
}

// TestServeTokenCommandPrintsAndPersists 覆盖零配置路径：没有显式令牌时，
// `serve token` 生成新令牌、打印到 stdout，并把同一令牌持久化到文件。
func TestServeTokenCommandPrintsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrollment.token")
	cmd := newServeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"token", "--token-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("serve token: %v", err)
	}
	printed := strings.TrimSpace(out.String())
	if len(printed) != 43 {
		t.Fatalf("printed token = %q (len %d)", printed, len(printed))
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != printed {
		t.Fatalf("file = %q, %v", data, err)
	}
}

func TestServeTokenCommandHelpDoesNotLeakToken(t *testing.T) {
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", "super-secret-enrollment-token")
	output, err := executeServeCommand(t, "token", "--help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(output, "super-secret-enrollment-token") {
		t.Fatalf("help leaks token:\n%s", output)
	}
}

func TestServeRejectsMalformedListenAddr(t *testing.T) {
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", "test-enrollment-token")
	// runControlPlane 会先解析并持久化令牌；必须指向临时文件，避免覆盖开发者真实令牌。
	t.Setenv("LEWORK_TOKEN_FILE", filepath.Join(t.TempDir(), "enrollment.token"))

	// 非回环地址是合法绑定（是否告警由鉴权配置决定），这里改用畸形地址触发校验失败。
	output, err := executeServeCommand(t, "--db", ":memory:", "--listen", "8787")
	if err == nil {
		t.Fatalf("serve accepted a malformed listen address:\n%s", output)
	}
	if !strings.Contains(err.Error(), "host:port") {
		t.Fatalf("error = %v, want it to mention host:port", err)
	}
	if strings.Contains(err.Error(), "test-enrollment-token") {
		t.Fatalf("error leaks the enrollment token: %v", err)
	}
}

func TestServeHelpDoesNotLeakToken(t *testing.T) {
	const secret = "super-secret-enrollment-token"
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", secret)

	output, err := executeServeCommand(t, "--help")
	if err != nil {
		t.Fatalf("execute serve help: %v", err)
	}
	if strings.Contains(output, secret) {
		t.Fatalf("serve help leaks the enrollment token:\n%s", output)
	}
}

func TestServeConfigFlagDefaults(t *testing.T) {
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", "")
	t.Setenv("LEWORK_TOKEN_FILE", "")

	flags := newServeCommand().Flags()
	tests := []struct {
		name string
		want string
	}{
		{name: "listen", want: controlplaneconfig.DefaultListenAddr},
		{name: "db", want: controlplaneconfig.DefaultDBPath()},
		{name: "token-file", want: controlplaneconfig.DefaultTokenPath()},
		{name: "request-timeout", want: controlplaneconfig.DefaultRequestTimeout.String()},
	}

	for _, tt := range tests {
		flag := flags.Lookup(tt.name)
		if flag == nil {
			t.Fatalf("--%s flag missing", tt.name)
		}
		if flag.DefValue != tt.want {
			t.Fatalf("--%s default = %q, want %q", tt.name, flag.DefValue, tt.want)
		}
	}
}

func TestRootHelpIncludesServe(t *testing.T) {
	output, err := executeRootCommand("--help")
	if err != nil {
		t.Fatalf("execute help: %v", err)
	}
	for _, expected := range []string{"serve", "lework serve --enrollment-token <token>"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("help output missing %q:\n%s", expected, output)
		}
	}
}
