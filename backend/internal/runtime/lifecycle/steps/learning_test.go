package steps

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent"
	lifecyclejournal "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/journal"
)

func TestShouldRunLearningCheck(t *testing.T) {
	result := &agent.RunResult{Status: agent.RunStatusCompleted}

	if !ShouldRunLearningCheck(&agent.RequestContext{
		Input: agent.InputContext{Type: agent.InputTypeMessage, Messages: []agent.InputMessage{{Role: "user", Content: "remember this preference"}}},
	}, result, &lifecyclejournal.RunTrace{}) {
		t.Fatalf("expected learning check for explicit user learning cue")
	}

	if !ShouldRunLearningCheck(&agent.RequestContext{
		Input: agent.InputContext{Type: agent.InputTypeMessage, Messages: []agent.InputMessage{{Role: "user", Content: "handle this complex task"}}},
	}, result, &lifecyclejournal.RunTrace{ToolCalls: 5}) {
		t.Fatalf("expected learning check after complex tool run")
	}

	if ShouldRunLearningCheck(&agent.RequestContext{
		Input: agent.InputContext{Type: agent.InputTypeMessage, Messages: []agent.InputMessage{{Role: "user", Content: "handle this complex task"}}},
	}, result, &lifecyclejournal.RunTrace{ToolCalls: 5, ToolNames: []string{ToolNameMemory}}) {
		t.Fatalf("did not expect learning check after memory was already called")
	}
}
