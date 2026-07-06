package run

import (
	"testing"
	"time"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func TestRequestFromWorkerTaskMapsWorkspaceContext(t *testing.T) {
	task := runTask{
		ID:        "msg_1",
		CreatedAt: time.Now().UTC(),
		Trace: messaging.TraceContext{
			TraceID:   "trace_1",
			RequestID: "req_1",
			TaskID:    "task_1",
			RunID:     "run_1",
		},
		Route: messaging.RouteContext{
			OrgID:     42,
			SessionID: "sess_1",
			WorkerID:  7,
		},
		TaskType:      messaging.TaskTypeAgentRun,
		ExecutionMode: string(agentrundomain.ExecutionModePlan),
		Execution: messaging.ExecutionTarget{
			AssistantID:   "assistant_1",
			AssistantName: "投标策略师",
			SystemPrompt:  "按投标策略师身份执行",
		},
		Workspace: messaging.WorkspaceOptions{
			ProjectID: "project_1",
		},
		Input: messaging.TaskInput{
			Type: messaging.InputTypeMessage,
			Messages: []messaging.ChatMessage{
				{Role: messaging.MessageRoleUser, Content: "hello"},
			},
		},
	}

	req := RequestFromWorkerTask(task)

	if req.Conversation.ID != "sess_1" {
		t.Fatalf("conversation id = %q, want sess_1", req.Conversation.ID)
	}
	if req.Workspace.OrgID != 42 {
		t.Fatalf("workspace org id = %d, want 42", req.Workspace.OrgID)
	}
	if req.Workspace.ProjectID != "project_1" {
		t.Fatalf("workspace project id = %q, want project_1", req.Workspace.ProjectID)
	}
	if req.Workspace.TaskID != "task_1" {
		t.Fatalf("workspace task id = %q, want task_1", req.Workspace.TaskID)
	}
	if req.Workspace.RequestID != "req_1" {
		t.Fatalf("workspace request id = %q, want req_1", req.Workspace.RequestID)
	}
	if req.ExecutionMode != agentrundomain.ExecutionModePlan {
		t.Fatalf("execution mode = %q, want %q", req.ExecutionMode, agentrundomain.ExecutionModePlan)
	}
	if req.Assistant.ID != "assistant_1" {
		t.Fatalf("assistant id = %q, want assistant_1", req.Assistant.ID)
	}
	if req.Assistant.Name != "投标策略师" {
		t.Fatalf("assistant name = %q, want 投标策略师", req.Assistant.Name)
	}
	if req.Assistant.SystemPrompt != "按投标策略师身份执行" {
		t.Fatalf("assistant system prompt = %q, want persona prompt", req.Assistant.SystemPrompt)
	}
}

func TestReplyToMessageIDsDeduplicatesInputMessageIDs(t *testing.T) {
	got := replyToMessageIDs([]messaging.ChatMessage{
		{ID: " 1 "},
		{ID: ""},
		{ID: "2"},
		{ID: "1"},
	})
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("replyToMessageIDs() = %v, want [1 2]", got)
	}
}

func TestInputMessagesFromTaskPreservesSenderName(t *testing.T) {
	got := inputMessagesFromTask([]messaging.ChatMessage{
		{ID: "1", Role: messaging.MessageRoleUser, Content: "hi", SenderName: "Alice"},
		{ID: "2", Role: messaging.MessageRoleAssistant, Content: "hello", SenderName: "Alpha"},
	})
	if len(got) != 2 || got[0].SenderName != "Alice" || got[1].SenderName != "Alpha" {
		t.Fatalf("sender names not preserved: %+v", got)
	}
}
