package opencode

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestIsPlanConfirmation(t *testing.T) {
	yesNo := []agent.QuestionItem{{Header: "计划确认", Options: []agent.QuestionOption{
		{Label: "Yes"}, {Label: "No"},
	}}}
	if !IsPlanConfirmation(agent.ExecutionModePlan, "/w/.opencode/plans/p.md", yesNo) {
		t.Fatal("expected plan confirmation")
	}
	if IsPlanConfirmation(agent.ExecutionModeDefault, "/w/.opencode/plans/p.md", yesNo) {
		t.Fatal("default mode must not count as plan confirmation")
	}
	if IsPlanConfirmation(agent.ExecutionModePlan, "", yesNo) {
		t.Fatal("missing plan path must not count")
	}
	if IsPlanConfirmation(agent.ExecutionModePlan, "/w/.opencode/plans/p.md", yesNo[:0]) {
		t.Fatal("empty questions must not count")
	}
	if IsPlanConfirmation(agent.ExecutionModePlan, "/w/.opencode/plans/p.md", []agent.QuestionItem{{Options: []agent.QuestionOption{{Label: "Yes"}}}}) {
		t.Fatal("single option must not count")
	}
	reversed := []agent.QuestionItem{{Options: []agent.QuestionOption{{Label: "No"}, {Label: "Yes"}}}}
	if IsPlanConfirmation(agent.ExecutionModePlan, "/w/.opencode/plans/p.md", reversed) {
		t.Fatal("reversed options must not count")
	}
}

func TestValidatePlanArtifact(t *testing.T) {
	workDir := t.TempDir()
	if err := ValidatePlanArtifact(filepath.Join(workDir, ".opencode", "plans", "p.md"), workDir); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	if err := ValidatePlanArtifact(filepath.Join(".opencode", "plans", "p.md"), workDir); err != nil {
		t.Fatalf("relative valid plan rejected: %v", err)
	}
	cases := []string{
		filepath.Join(workDir, "notes.md"),
		filepath.Join(workDir, ".opencode", "plans", "p.txt"),
		filepath.Join(workDir, ".opencode", "plans", "sub", "p.md"),
		filepath.Join(t.TempDir(), ".opencode", "plans", "p.md"),
	}
	for _, path := range cases {
		if err := ValidatePlanArtifact(path, workDir); err == nil {
			t.Fatalf("invalid plan accepted: %s", path)
		}
	}
}

func TestPlanConfirmationBundleEmitsPlanReadyFirst(t *testing.T) {
	workDir := t.TempDir()
	planPath := filepath.Join(workDir, ".opencode", "plans", "p.md")
	payload := agent.QuestionAskedPayload{
		RequestID: "req-1",
		SessionID: "s1",
		Questions: []agent.QuestionItem{{Options: []agent.QuestionOption{
			{Label: "Yes"}, {Label: "No"},
		}}},
	}

	events, marked, ok := PlanConfirmationBundle(workDir, planPath, "sess-1", agent.ExecutionModePlan, payload)
	if !ok || len(events) != 1 {
		t.Fatalf("ok=%v events=%d", ok, len(events))
	}
	if events[0].Type != agent.NodeEventPlanReady {
		t.Fatalf("first event = %s, want plan.ready", events[0].Type)
	}
	ready := events[0].Payload.(*agent.PlanReadyPayload)
	if ready.Path != planPath || ready.DisplayPath != filepath.Join(".opencode", "plans", "p.md") {
		t.Fatalf("plan.ready payload = %+v", ready)
	}
	if marked.InteractionType != PlanConfirmationInteractionType {
		t.Fatalf("interaction type = %q", marked.InteractionType)
	}
}

func TestPlanConfirmationBundleMisses(t *testing.T) {
	workDir := t.TempDir()
	planPath := filepath.Join(workDir, ".opencode", "plans", "p.md")
	payload := agent.QuestionAskedPayload{Questions: []agent.QuestionItem{{Options: []agent.QuestionOption{
		{Label: "Yes"}, {Label: "No"},
	}}}}
	if _, _, ok := PlanConfirmationBundle(workDir, "", "", agent.ExecutionModePlan, payload); ok {
		t.Fatal("missing plan path must not confirm")
	}
	if _, _, ok := PlanConfirmationBundle(workDir, planPath, "", agent.ExecutionModeDefault, payload); ok {
		t.Fatal("default mode must not confirm")
	}
	badPayload := agent.QuestionAskedPayload{Questions: []agent.QuestionItem{{Options: []agent.QuestionOption{{Label: "OK"}}}}}
	if _, _, ok := PlanConfirmationBundle(workDir, planPath, "", agent.ExecutionModePlan, badPayload); ok {
		t.Fatal("wrong options must not confirm")
	}
}
