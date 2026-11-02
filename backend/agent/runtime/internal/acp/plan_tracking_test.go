package acp

import (
	"path/filepath"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Lework/backend/agent"
)

func TestTranslatorTracksPlanArtifact(t *testing.T) {
	workDir := t.TempDir()
	translator := NewTranslator(nil)
	translator.SetWorkDir(workDir)

	planPath := filepath.Join(workDir, ".opencode", "plans", "plan.md")
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusPending, "write", nil)))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusInProgress, "write", map[string]any{
		"path":    planPath,
		"content": "# plan",
	})))
	// completed 帧通常不再带 RawInput，必须回落到 in_progress 的记录。
	events := translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusCompleted, "", nil)))
	if len(events) != 1 {
		t.Fatalf("events = %d, want end", len(events))
	}

	gotPath, display, ok := translator.PlanArtifact()
	if !ok {
		t.Fatal("plan artifact not tracked")
	}
	if gotPath != planPath {
		t.Fatalf("path = %q, want %q", gotPath, planPath)
	}
	if display != filepath.Join(".opencode", "plans", "plan.md") {
		t.Fatalf("display = %q", display)
	}
}

func TestTranslatorIgnoresNonPlanWrites(t *testing.T) {
	workDir := t.TempDir()
	translator := NewTranslator(nil)
	translator.SetWorkDir(workDir)

	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusPending, "write", nil)))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusInProgress, "write", map[string]any{
		"path": filepath.Join(workDir, "notes.md"),
	})))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusCompleted, "", nil)))
	if _, _, ok := translator.PlanArtifact(); ok {
		t.Fatal("non-plan write should not be tracked")
	}

	// 失败的计划写入也不计入。
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusPending, "write", nil)))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusInProgress, "write", map[string]any{
		"path": filepath.Join(workDir, ".opencode", "plans", "failed.md"),
	})))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusFailed, "", nil)))
	if _, _, ok := translator.PlanArtifact(); ok {
		t.Fatal("failed plan write should not be tracked")
	}
}

func TestTranslatorTracksRelativePlanPath(t *testing.T) {
	workDir := t.TempDir()
	translator := NewTranslator(nil)
	translator.SetWorkDir(workDir)

	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusPending, "write", nil)))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusInProgress, "write", map[string]any{
		"filePath": filepath.Join(".opencode", "plans", "relative.md"),
	})))
	translator.Handle(note(toolUpdate(acpsdk.ToolCallStatusCompleted, "", nil)))

	path, display, ok := translator.PlanArtifact()
	if !ok {
		t.Fatal("relative plan path not tracked")
	}
	want := filepath.Join(workDir, ".opencode", "plans", "relative.md")
	if path != want || display != filepath.Join(".opencode", "plans", "relative.md") {
		t.Fatalf("path/display = %q/%q", path, display)
	}
}

func toolUpdate(
	status acpsdk.ToolCallStatus,
	title string,
	rawInput any,
) acpsdk.SessionUpdate {
	if status == acpsdk.ToolCallStatusPending {
		return acpsdk.SessionUpdate{ToolCall: &acpsdk.SessionUpdateToolCall{
			ToolCallId: "c1",
			Kind:       acpsdk.ToolKindEdit,
			Title:      title,
			Status:     status,
			RawInput:   rawInput,
		}}
	}
	return acpsdk.SessionUpdate{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
		ToolCallId: "c1",
		Kind:       kindPtr(acpsdk.ToolKindEdit),
		Status:     statusPtr(status),
		RawInput:   rawInput,
	}}
}

var _ = agent.NodeEventToolExecutionEnd
