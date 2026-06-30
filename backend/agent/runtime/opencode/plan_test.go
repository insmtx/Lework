package opencode

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPlanHandoffReadsSessionPlan(t *testing.T) {
	workDir := t.TempDir()
	session := &sessionResponse{
		Slug:      "calm-forest",
		Directory: workDir,
	}
	session.Time.Created = 123456
	planDir := filepath.Join(workDir, ".opencode", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const content = "# Plan\n\n- Implement plan mode\n"
	if err := os.WriteFile(filepath.Join(planDir, "123456-calm-forest.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	st := &runState{workDir: workDir, session: session}
	handoff := st.planHandoff(nil)
	if handoff.Error != "" {
		t.Fatalf("plan handoff error = %q", handoff.Error)
	}
	if handoff.Content != content {
		t.Fatalf("plan content = %q", handoff.Content)
	}
	if handoff.FilePath != filepath.Join(".opencode", "plans", "123456-calm-forest.md") {
		t.Fatalf("plan path = %q", handoff.FilePath)
	}
}

func TestPlanHandoffReadsUpdatedPlanContent(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, ".opencode", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(planDir, "123456-calm-forest.md")
	if err := os.WriteFile(planPath, []byte("# Initial plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &sessionResponse{Slug: "calm-forest", Directory: filepath.Join(workDir, "nested")}
	session.Time.Created = 123456
	st := &runState{workDir: workDir, session: session}
	questions := []events.QuestionItem{{
		Question: "Plan at .opencode/plans/123456-calm-forest.md is complete.",
	}}

	first := st.planHandoff(questions)
	if first.Content != "# Initial plan" || first.Error != "" {
		t.Fatalf("initial handoff = %#v", first)
	}

	if err := os.WriteFile(planPath, []byte("# Revised plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := st.planHandoff(questions)
	if second.Content != "# Revised plan" || second.Error != "" {
		t.Fatalf("revised handoff = %#v", second)
	}
}

func TestPlanHandoffUsesQuestionPathFallback(t *testing.T) {
	workDir := t.TempDir()
	planDir := filepath.Join(workDir, ".opencode", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	relativePath := filepath.Join(".opencode", "plans", "123456-calm-forest.md")
	if err := os.WriteFile(filepath.Join(workDir, relativePath), []byte("# Plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := &sessionResponse{Slug: "calm-forest", Directory: workDir}
	session.Time.Created = 123456
	st := &runState{workDir: workDir, session: session}
	handoff := st.planHandoff([]events.QuestionItem{{
		Question: "Plan at " + relativePath + " is complete. Would you like to switch?",
	}})
	if handoff.Error != "" || handoff.Content != "# Plan" {
		t.Fatalf("unexpected handoff: %#v", handoff)
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

func TestPlanHandoffReportsReadFailure(t *testing.T) {
	workDir := t.TempDir()
	session := &sessionResponse{Slug: "calm-forest", Directory: workDir}
	session.Time.Created = 123456
	st := &runState{workDir: workDir, session: session}

	handoff := st.planHandoff([]events.QuestionItem{{
		Question: "Plan at .opencode/plans/123456-calm-forest.md is complete.",
	}})
	if handoff.Error == "" || !strings.Contains(handoff.Error, "read plan file") {
		t.Fatalf("expected read error, got %#v", handoff)
	}
	if handoff.Content != "" {
		t.Fatalf("failed handoff content = %q, want empty", handoff.Content)
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
