package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestLoadConfigAppliesServerLogLevel(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("log:\n  level: error\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer

	cfg, loadErr := loadConfig(configPath)
	logs.Info("server-info-should-be-hidden")
	logs.Error("server-error-should-be-visible")
	_ = writer.Close()
	os.Stdout = oldStdout
	applyLogLevel("info")

	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("read log output: %v", readErr)
	}
	if loadErr != nil {
		t.Fatalf("load config: %v", loadErr)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("log level = %q, want error", cfg.Log.Level)
	}
	if strings.Contains(string(output), "server-info-should-be-hidden") {
		t.Fatalf("info log should be filtered at error level: %s", output)
	}
	if !strings.Contains(string(output), "server-error-should-be-visible") {
		t.Fatalf("error log should be visible: %s", output)
	}
}

func TestApplyServerWorkspaceRootFallsBackToSchedulerHostPathRoot(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, "")

	cfg := &config.Config{
		Scheduler: &config.SchedulerConfig{
			WorkspaceHostPathRoot: "/data/workspace",
		},
	}

	if err := applyServerWorkspaceRoot(cfg); err != nil {
		t.Fatalf("apply server workspace root: %v", err)
	}
	if got, want := os.Getenv(leros.EnvWorkspaceRoot), "/data/workspace"; got != want {
		t.Fatalf("%s = %q, want %q", leros.EnvWorkspaceRoot, got, want)
	}
}
