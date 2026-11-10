package main

import (
	"bytes"
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

func TestServeRequiresEnrollmentToken(t *testing.T) {
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", "")

	output, err := executeServeCommand(t, "--db", ":memory:")
	if err == nil {
		t.Fatalf("serve without a token succeeded:\n%s", output)
	}
	if !strings.Contains(err.Error(), "enrollment_token") {
		t.Fatalf("error = %v, want it to mention enrollment_token", err)
	}
}

func TestServeRejectsNonLoopback(t *testing.T) {
	t.Setenv("LEWORK_ENROLLMENT_TOKEN", "test-enrollment-token")

	output, err := executeServeCommand(t, "--db", ":memory:", "--listen", "0.0.0.0:8787")
	if err == nil {
		t.Fatalf("serve bound a non-loopback address:\n%s", output)
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v, want it to mention loopback", err)
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

	flags := newServeCommand().Flags()
	tests := []struct {
		name string
		want string
	}{
		{name: "listen", want: controlplaneconfig.DefaultListenAddr},
		{name: "db", want: controlplaneconfig.DefaultDBPath()},
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
