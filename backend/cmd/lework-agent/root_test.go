package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootCommandHasRun(t *testing.T) {
	cmd := newRootCommand()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil || run == nil || run.Name() != "run" {
		t.Fatalf("run command missing: %v", err)
	}
}

func TestRootCommandHasVersion(t *testing.T) {
	cmd := newRootCommand()
	versionCmd, _, err := cmd.Find([]string{"version"})
	if err != nil || versionCmd == nil || versionCmd.Name() != "version" {
		t.Fatalf("version command missing: %v", err)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if want := "lework-agent " + version + "\n"; out.String() != want {
		t.Fatalf("version output = %q, want %q", out.String(), want)
	}
}

func TestRunRequiresConfig(t *testing.T) {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"run"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("err = %v, want config error", err)
	}
}

func TestRunRejectsIncompleteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("node_name: test-node\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"run", "--config", path})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "control_plane_url") {
		t.Fatalf("err = %v, want control_plane_url validation error", err)
	}
}

func TestRunConfigFlagDefaultsToPlatformPath(t *testing.T) {
	cmd := newRootCommand()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run: %v", err)
	}
	flag := run.Flags().Lookup("config")
	if flag == nil {
		t.Fatal("--config flag missing")
	}
	if flag.DefValue != defaultConfigPath() {
		t.Fatalf("--config default = %q, want %q", flag.DefValue, defaultConfigPath())
	}
}

func TestDefaultConfigPathFor(t *testing.T) {
	if got, want := defaultConfigPathFor("linux"), "/etc/lework/agent.yaml"; got != want {
		t.Fatalf("linux default = %q, want %q", got, want)
	}
	for _, goos := range []string{"darwin", "windows"} {
		got := defaultConfigPathFor(goos)
		normalized := strings.ReplaceAll(got, `\`, "/")
		if !strings.HasSuffix(normalized, "Lework/agent.yaml") {
			t.Fatalf("%s default = %q, want a Lework/agent.yaml suffix", goos, got)
		}
	}
}

func TestEnvSliceIsSorted(t *testing.T) {
	got := envSlice(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("envSlice = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("envSlice = %v, want %v", got, want)
		}
	}
	if got := envSlice(nil); got != nil {
		t.Fatalf("envSlice(nil) = %v, want nil", got)
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{current: time.Second, want: 2 * time.Second},
		{current: 2 * time.Second, want: 4 * time.Second},
		{current: 4 * time.Second, want: 8 * time.Second},
		{current: 8 * time.Second, want: 16 * time.Second},
		{current: 16 * time.Second, want: 30 * time.Second},
		{current: 30 * time.Second, want: 30 * time.Second},
		{current: 0, want: time.Second},
	}
	for _, tt := range tests {
		if got := nextBackoff(tt.current); got != tt.want {
			t.Errorf("nextBackoff(%s) = %s, want %s", tt.current, got, tt.want)
		}
	}
}

func TestWaitBackoffReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitBackoff(ctx, time.Hour); err == nil {
		t.Fatal("waitBackoff returned nil on canceled context")
	}
}

func TestControlPlaneHeader(t *testing.T) {
	if got := controlPlaneHeader(""); got != nil {
		t.Fatalf("controlPlaneHeader(\"\") = %v, want nil", got)
	}
	header := controlPlaneHeader("secret-token")
	if got, want := header.Get("Authorization"), "Bearer secret-token"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestRedactHidesSecret(t *testing.T) {
	err := errors.New("dial wss://host/connect?token=s3cret failed")
	got := redact(err, "s3cret")
	if strings.Contains(got, "s3cret") {
		t.Fatalf("redact leaked the secret: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("redact = %q, want a redaction marker", got)
	}
	if got := redact(nil, "s3cret"); got != "" {
		t.Fatalf("redact(nil) = %q, want empty", got)
	}
}
