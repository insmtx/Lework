package opencode

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Lework/backend/agent"
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

// 非合规用例：模型写了计划却问了别的问题（或根本没问），只能落 miss，
// 由 ShouldRetryPlanConfirmation 决定补一次追问，且不得把表单误标成计划确认。
func TestPlanConfirmationNonCompliantTurn(t *testing.T) {
	workDir := t.TempDir()
	planPath := filepath.Join(workDir, ".opencode", "plans", "p.md")
	payload := agent.QuestionAskedPayload{Questions: []agent.QuestionItem{{
		Header:  "其他",
		Options: []agent.QuestionOption{{Label: "继续"}},
	}}}

	events, marked, ok := PlanConfirmationBundle(workDir, planPath, "", agent.ExecutionModePlan, payload)
	if ok || len(events) != 0 {
		t.Fatalf("non-compliant turn must not confirm: ok=%v events=%d", ok, len(events))
	}
	if marked.InteractionType != "" {
		t.Fatalf("interaction type = %q, want empty", marked.InteractionType)
	}
	if !ShouldRetryPlanConfirmation(agent.ExecutionModePlan, ok, true, nil, nil) {
		t.Fatal("contract miss with plan file should trigger one retry")
	}
}

func TestShouldRetryPlanConfirmation(t *testing.T) {
	promptErr := errors.New("prompt failed")
	tests := []struct {
		name      string
		mode      agent.ExecutionMode
		confirmed bool
		hasPlan   bool
		promptErr error
		ctxErr    error
		want      bool
	}{
		{"retry on miss", agent.ExecutionModePlan, false, true, nil, nil, true},
		{"confirmed", agent.ExecutionModePlan, true, true, nil, nil, false},
		{"no plan file", agent.ExecutionModePlan, false, false, nil, nil, false},
		{"default mode", agent.ExecutionModeDefault, false, true, nil, nil, false},
		{"prompt error", agent.ExecutionModePlan, false, true, promptErr, nil, false},
		{"user cancelled", agent.ExecutionModePlan, false, true, nil, context.Canceled, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRetryPlanConfirmation(tt.mode, tt.confirmed, tt.hasPlan, tt.promptErr, tt.ctxErr)
			if got != tt.want {
				t.Fatalf("ShouldRetryPlanConfirmation = %v, want %v", got, tt.want)
			}
		})
	}
}

// 提示词是计划确认的唯一触发手段（v2 无 plan_exit），契约串被改动就等于功能退化。
func TestPlanPromptsKeepConfirmationContract(t *testing.T) {
	prompts := map[string]string{"system": PlanSystemPrompt, "retry": PlanRetryPrompt}
	for name, prompt := range prompts {
		for _, want := range []string{"计划确认", "以下是当前计划，是否执行？", `"Yes"`, `"No"`, "question"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt lost contract token %q", name, want)
			}
		}
	}
	if !strings.Contains(PlanSystemPrompt, "plan_exit") {
		t.Fatal("system prompt must warn that plan_exit does not exist")
	}
}
