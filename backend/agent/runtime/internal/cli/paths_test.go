package cli

import "testing"

func TestTaskRuntimeRootPrefersTaskAndFallsBackToWorkDirectory(t *testing.T) {
	if got := TaskRuntimeRoot(" /workspace/task ", "/workspace/repo"); got != "/workspace/task" {
		t.Fatalf("task runtime root = %q", got)
	}
	if got := TaskRuntimeRoot("", " /workspace/temp "); got != "/workspace/temp" {
		t.Fatalf("temporary runtime root = %q", got)
	}
}
