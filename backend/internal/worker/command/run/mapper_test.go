package run

import (
	"testing"
	"time"

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
		TaskType: messaging.TaskTypeAgentRun,
		Execution: messaging.ExecutionTarget{
			AssistantID: "assistant_1",
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
