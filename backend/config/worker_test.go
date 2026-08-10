package config

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestWorkerConfigParsesRunBlock(t *testing.T) {
	var cfg WorkerConfig
	body := []byte("org_id: 1\nworker_id: 2\nrun:\n  max_concurrency: 8\n  max_inflight: 16\n  max_interaction_waits: 5\n  debounce_ms: 1000\n  interaction_timeout_seconds: 300\n")
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal worker config: %v", err)
	}
	if cfg.Run == nil {
		t.Fatal("run block not parsed")
	}
	eff := cfg.Run.Effective()
	if eff.MaxConcurrency != 8 || eff.MaxInflight != 16 || eff.MaxInteractionWaits != 5 ||
		eff.DebounceMS != 1000 || eff.InteractionTimeoutSeconds != 300 {
		t.Fatalf("effective run config = %+v", eff)
	}
}

func TestRunConfigEffectiveAppliesDefaults(t *testing.T) {
	// nil run block → 默认值。
	var cfg WorkerConfig
	eff := cfg.Run.Effective()
	if eff.MaxConcurrency != 10 {
		t.Fatalf("default MaxConcurrency = %d, want 10", eff.MaxConcurrency)
	}
	if eff.MaxInflight != 20 {
		t.Fatalf("default MaxInflight = %d, want 20", eff.MaxInflight)
	}
	if eff.MaxInteractionWaits != 10 {
		t.Fatalf("default MaxInteractionWaits = %d, want 10", eff.MaxInteractionWaits)
	}
	if eff.DebounceMS != 1500 {
		t.Fatalf("default DebounceMS = %d, want 1500", eff.DebounceMS)
	}
	if eff.InteractionTimeoutSeconds != 600 {
		t.Fatalf("default InteractionTimeoutSeconds = %d, want 600", eff.InteractionTimeoutSeconds)
	}
}

func TestRunConfigEffectiveNormalizesInflightToAtLeastConcurrency(t *testing.T) {
	cfg := &RunConfig{MaxConcurrency: 10, MaxInflight: 1}
	eff := cfg.Effective()
	if eff.MaxInflight != 10 {
		t.Fatalf("MaxInflight = %d, want clamped to MaxConcurrency 10", eff.MaxInflight)
	}
	// 正常配置不受影响。
	cfg2 := &RunConfig{MaxConcurrency: 4, MaxInflight: 8}
	if got := cfg2.Effective().MaxInflight; got != 8 {
		t.Fatalf("MaxInflight = %d, want 8 (unchanged)", got)
	}
}

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
