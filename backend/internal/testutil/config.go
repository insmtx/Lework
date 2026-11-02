//go:build integration

package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Lework/backend/config"
	"gopkg.in/yaml.v2"
)

func findConfig() string {
	if p := os.Getenv("LEWORK_TEST_CONFIG"); p != "" {
		return p
	}

	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "deployments", "dev", "server.config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func LoadTestConfig(t *testing.T) *config.Config {
	t.Helper()

	path := findConfig()
	if path == "" {
		t.Fatal("test config not found: set LEWORK_TEST_CONFIG or place deployments/dev/server.config.yaml in the project root")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve config path %s: %v", path, err)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read config %s: %v", abs, err)
	}

	data = []byte(os.ExpandEnv(string(data)))
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config %s: %v", abs, err)
	}

	if cfg.Database.URL == "" {
		t.Fatal("database.url is empty in test config")
	}
	if cfg.NATS == nil || cfg.NATS.URL == "" {
		t.Fatal("nats.url is empty in test config")
	}

	return &cfg
}
