package opencode

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

func TestOpenCodeAgent(t *testing.T) {
	if got := openCodeAgent(agent.ExecutionModePlan); got != "plan" {
		t.Fatalf("plan mode agent = %q, want plan", got)
	}
	if got := openCodeAgent(agent.ExecutionModeDefault); got != "build" {
		t.Fatalf("default mode agent = %q, want build", got)
	}
	if got := openCodeAgent(""); got != "build" {
		t.Fatalf("empty mode agent = %q, want build", got)
	}
}

func TestResolvePlanPathUsesWorkDirBeforeSessionDirectory(t *testing.T) {
	workDir := t.TempDir()
	session := &sessionResponse{
		Slug:      "calm-forest",
		Directory: filepath.Join(t.TempDir(), "session-directory"),
	}
	session.Time.Created = 123456
	st := &runState{workDir: workDir, session: session}

	path, _, err := st.resolvePlanPath([]events.QuestionItem{{
		Question: "Plan at .opencode/plans/123456-calm-forest.md is complete.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workDir, ".opencode", "plans", "123456-calm-forest.md")
	if path != want {
		t.Fatalf("plan path = %q, want workDir path %q", path, want)
	}
}

func TestBuildServerEnvEnablesPlanMode(t *testing.T) {
	env := buildServerEnv("secret", "{}", nil)
	assertEnvContains(t, env, "OPENCODE_EXPERIMENTAL_PLAN_MODE=true")
	assertEnvContains(t, env, "OPENCODE_CLIENT=cli")
}

func assertEnvContains(t *testing.T, env []string, expected string) {
	t.Helper()
	for _, item := range env {
		if item == expected {
			return
		}
	}
	t.Fatalf("environment does not contain %q: %#v", expected, env)
}
