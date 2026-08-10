package seed

import (
	"context"
	"testing"
)

func TestLoadEnvVarsReadsFromEnv(t *testing.T) {
	t.Setenv("SEED_TEST_VAR", "from-env")

	env, err := loadEnvVars(context.Background())
	if err != nil {
		t.Fatalf("loadEnvVars: %v", err)
	}
	if env["SEED_TEST_VAR"] != "from-env" {
		t.Errorf("expected SEED_TEST_VAR from os.Environ, got %q", env["SEED_TEST_VAR"])
	}
}
