package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestProjectRunEventPreservesTerminalArchiveForAllStatuses(t *testing.T) {
	tests := []struct {
		name      string
		eventType messaging.RunEventType
		public    string
		status    string
		errorText string
	}{
		{name: "completed", eventType: messaging.RunEventRunCompleted, public: string(messaging.RunEventRunCompleted), status: "completed"},
		{name: "failed", eventType: messaging.RunEventRunFailed, public: string(messaging.RunEventRunFailed), status: "failed", errorText: "provider failed"},
		{name: "cancelled", eventType: messaging.RunEventRunCancelled, public: string(messaging.RunEventRunCancelled), status: "cancelled", errorText: "context canceled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runEvent := messaging.RunEvent{
				CreatedAt: time.UnixMilli(1234),
				Trace:     messaging.TraceContext{RunID: "run-1"},
				Route:     messaging.RouteContext{SessionID: "session-1"},
				Body: messaging.RunEventBody{
					Seq:   9,
					Event: test.eventType,
					RunCompleted: &messaging.RunCompletedPayload{
						Status: test.status,
						Result: messaging.RunResultPayload{Message: "result"},
						Artifacts: []messaging.ArtifactPayload{{
							ArtifactID: "artifact-1",
							Title:      "report",
						}},
						Usage: &messaging.UsagePayload{InputTokens: 5, OutputTokens: 6, TotalTokens: 999},
						Events: []messaging.RunEventRecord{{
							Seq:       3,
							LastSeq:   5,
							Type:      "message.delta",
							Timestamp: 100,
							Payload:   json.RawMessage(`{"message_id":"m1"}`),
						}},
						Metadata: &messaging.RunMetadataPayload{Runtime: "codex"},
					},
				},
			}
			if test.errorText != "" {
				runEvent.Body.Error = &messaging.RunEventError{Message: test.errorText}
			}
			projected, ok := ProjectRunEvent(runEvent)
			if !ok || projected.Type != test.public {
				t.Fatalf("ProjectRunEvent() = %#v, %v", projected, ok)
			}
			payload, ok := projected.Payload.(dto.RunTerminalPayload)
			if !ok {
				t.Fatalf("payload type = %T", projected.Payload)
			}
			if payload.Status != test.status ||
				payload.Result.Message != "result" ||
				payload.Error != test.errorText ||
				payload.Usage == nil ||
				payload.Usage.TotalTokens != 11 ||
				len(payload.Artifacts) != 1 ||
				len(payload.Events) != 1 ||
				payload.Metadata == nil ||
				payload.Metadata.Runtime != "codex" {
				t.Fatalf("terminal payload = %#v", payload)
			}
		})
	}
}

func TestProjectRunEventRecordMatchesLiveProjectionForPublicEvents(t *testing.T) {
	timestamp := time.UnixMilli(1779243000123).UTC()
	tests := []struct {
		name     string
		runEvent messaging.RunEvent
		payload  any
	}{
		{
			name: "message delta",
			runEvent: publicProjectionRunEvent(timestamp, 1, messaging.RunEventMessageDelta,
				messaging.RunEventPayload{MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "hello"}),
			payload: recordedMessagePayload{MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "hello"},
		},
		{
			name: "reasoning delta",
			runEvent: publicProjectionRunEvent(timestamp, 2, messaging.RunEventReasoningDelta,
				messaging.RunEventPayload{MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "thinking"}),
			payload: recordedMessagePayload{MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "thinking"},
		},
		{
			name: "message completed",
			runEvent: publicProjectionRunEvent(timestamp, 3, messaging.RunEventMessageCompleted,
				messaging.RunEventPayload{
					MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "done",
					Usage: &messaging.UsagePayload{InputTokens: 4, OutputTokens: 5, TotalTokens: 999},
				}),
			payload: recordedMessagePayload{
				MessageID: "msg_1", Role: messaging.MessageRoleAssistant, Content: "done",
				Usage: &messaging.UsagePayload{InputTokens: 4, OutputTokens: 5, TotalTokens: 999},
			},
		},
		{
			name: "tool started",
			runEvent: publicProjectionRunEvent(timestamp, 4, messaging.RunEventToolCallStarted,
				messaging.RunEventPayload{ToolCall: &messaging.ToolCallPayload{
					ToolCallID: "tool_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
				}}),
			payload: messaging.ToolCallPayload{
				ToolCallID: "tool_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
			},
		},
		{
			name: "tool finished",
			runEvent: publicProjectionRunEvent(timestamp, 5, messaging.RunEventToolCallFinished,
				messaging.RunEventPayload{ToolResult: &messaging.ToolCallResultPayload{
					ToolCallID: "tool_1", Name: "read", Result: json.RawMessage(`{"ok":true}`),
				}}),
			payload: messaging.ToolCallResultPayload{
				ToolCallID: "tool_1", Name: "read", Result: json.RawMessage(`{"ok":true}`),
			},
		},
		{
			name: "todo updated",
			runEvent: publicProjectionRunEvent(timestamp, 6, messaging.RunEventTodoUpdated,
				messaging.RunEventPayload{Todos: []messaging.RuntimeTodoItem{{ID: "todo_1", Title: "实现", Status: "in_progress"}}}),
			payload: []messaging.RuntimeTodoItem{{ID: "todo_1", Title: "实现", Status: "in_progress"}},
		},
		{
			name: "approval requested",
			runEvent: publicProjectionRunEvent(timestamp, 7, messaging.RunEventApprovalRequested,
				messaging.RunEventPayload{ApprovalRequest: &messaging.ApprovalRequestPayload{
					RequestID: "approval_1", ToolName: "shell", Description: "Run command",
					Metadata: map[string]string{"engine": "codex"},
				}}),
			payload: messaging.ApprovalRequestPayload{
				RequestID: "approval_1", ToolName: "shell", Description: "Run command",
				Metadata: map[string]string{"engine": "codex"},
			},
		},
		{
			name: "approval resolved",
			runEvent: publicProjectionRunEvent(timestamp, 8, messaging.RunEventApprovalResolved,
				messaging.RunEventPayload{ApprovalDecision: &messaging.ApprovalDecisionPayload{
					RequestID: "approval_1", Action: "approve", Reason: "ok",
				}}),
			payload: messaging.ApprovalDecisionPayload{RequestID: "approval_1", Action: "approve", Reason: "ok"},
		},
		{
			name: "question asked",
			runEvent: publicProjectionRunEvent(timestamp, 9, messaging.RunEventQuestionAsked,
				messaging.RunEventPayload{QuestionRequest: &messaging.QuestionRequestPayload{
					RequestID: "question_1", SessionID: "session-1", InteractionType: "plan_confirmation",
					Questions: []messaging.QuestionItem{{Question: "继续？", Options: []messaging.QuestionOption{{Label: "Yes"}}}},
					Metadata:  map[string]string{"plan_error": "resolve_failed"},
				}}),
			payload: messaging.QuestionRequestPayload{
				RequestID: "question_1", SessionID: "session-1", InteractionType: "plan_confirmation",
				Questions: []messaging.QuestionItem{{Question: "继续？", Options: []messaging.QuestionOption{{Label: "Yes"}}}},
				Metadata:  map[string]string{"plan_error": "resolve_failed"},
			},
		},
		{
			name: "question answered",
			runEvent: publicProjectionRunEvent(timestamp, 10, messaging.RunEventQuestionAnswered,
				messaging.RunEventPayload{QuestionAnswer: &messaging.QuestionAnswerPayload{
					RequestID: "question_1", Answers: [][]string{{"Yes"}},
				}}),
			payload: messaging.QuestionAnswerPayload{RequestID: "question_1", Answers: [][]string{{"Yes"}}},
		},
		{
			name: "artifact declared",
			runEvent: publicProjectionRunEvent(timestamp, 11, messaging.RunEventArtifactDeclared,
				messaging.RunEventPayload{Artifact: &messaging.ArtifactPayload{
					ArtifactID: "artifact_1", Title: "报告", Filename: "report.md", StorageURI: "file:///report.md",
				}}),
			payload: messaging.ArtifactPayload{ArtifactID: "artifact_1", Title: "报告", Filename: "report.md", StorageURI: "file:///report.md"},
		},
		{
			name: "plan published",
			runEvent: publicProjectionRunEvent(timestamp, 12, messaging.RunEventPlanPublished,
				messaging.RunEventPayload{PlanPublished: &messaging.PlanPublishedPayload{
					FileID: "file_plan_1", Directive: ":::plan{}\n:::", SummaryLines: 1, TotalLines: 2,
				}}),
			payload: messaging.PlanPublishedPayload{FileID: "file_plan_1", Directive: ":::plan{}\n:::", SummaryLines: 1, TotalLines: 2},
		},
		{
			name: "work title updated",
			runEvent: publicProjectionRunEvent(timestamp, 13, messaging.RunEventWorkTitleUpdated,
				messaging.RunEventPayload{WorkTitle: &messaging.WorkTitleUpdatedPayload{
					ProjectID: "project_1", ProjectName: "项目", TaskID: "task_1", TaskTitle: "任务", SessionID: "session-1",
				}}),
			payload: messaging.WorkTitleUpdatedPayload{
				ProjectID: "project_1", ProjectName: "项目", TaskID: "task_1", TaskTitle: "任务", SessionID: "session-1",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			live, ok := ProjectRunEvent(test.runEvent)
			if !ok {
				t.Fatalf("ProjectRunEvent(%s) did not project", test.runEvent.Body.Event)
			}
			replayPayload, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatalf("marshal replay payload: %v", err)
			}
			replayed, ok := ProjectRunEventRecord(test.runEvent.Route.SessionID, types.MessageChunk{
				Seq:       test.runEvent.Body.Seq,
				Type:      string(test.runEvent.Body.Event),
				Timestamp: test.runEvent.CreatedAt.UnixMilli(),
				Payload:   replayPayload,
			})
			if !ok {
				t.Fatalf("ProjectRunEventRecord(%s) did not project", test.runEvent.Body.Event)
			}
			if !reflect.DeepEqual(replayed, live) {
				t.Fatalf("replayed projection = %#v, want live %#v", replayed, live)
			}
		})
	}
}

func publicProjectionRunEvent(
	createdAt time.Time,
	seq int64,
	eventType messaging.RunEventType,
	payload messaging.RunEventPayload,
) messaging.RunEvent {
	return messaging.RunEvent{
		CreatedAt: createdAt,
		Route:     messaging.RouteContext{SessionID: "session-1"},
		Body: messaging.RunEventBody{
			Seq:     seq,
			Event:   eventType,
			Payload: payload,
		},
	}
}

func TestProjectRunEventRecordKeepsCancelledTypeAndTerminalPayload(t *testing.T) {
	raw, err := json.Marshal(recordedTerminalPayload{
		Status:  "cancelled",
		Message: "已取消",
		Error:   "context canceled",
		Usage:   &messaging.UsagePayload{InputTokens: 2, OutputTokens: 3, TotalTokens: 999},
		Artifacts: []messaging.ArtifactPayload{{
			ArtifactID: "artifact-1",
		}},
		Events: []messaging.RunEventRecord{{
			Seq:     1,
			Type:    "message.delta",
			Payload: json.RawMessage(`{"content":"partial"}`),
		}},
	})
	if err != nil {
		t.Fatalf("marshal terminal payload: %v", err)
	}
	projected, ok := ProjectRunEventRecord("session-1", types.MessageChunk{
		Seq:       7,
		Type:      string(messaging.RunEventRunCancelled),
		Timestamp: 123,
		Payload:   raw,
	})
	if !ok || projected.Type != string(messaging.RunEventRunCancelled) {
		t.Fatalf("ProjectRunEventRecord() = %#v, %v", projected, ok)
	}
	payload, ok := projected.Payload.(dto.RunTerminalPayload)
	if !ok || payload.Status != "cancelled" || payload.Result.Message != "已取消" ||
		payload.Error != "context canceled" || payload.Usage == nil ||
		payload.Usage.TotalTokens != 5 || len(payload.Artifacts) != 1 || len(payload.Events) != 1 {
		t.Fatalf("terminal payload = %#v", projected.Payload)
	}
}

func TestProjectRunEventAndPersistedChunkKeepStreamSequenceAndPayload(t *testing.T) {
	runEvent := messaging.RunEvent{
		CreatedAt: time.UnixMilli(456),
		Route:     messaging.RouteContext{SessionID: "session-1"},
		Body: messaging.RunEventBody{
			Seq:   12,
			Event: messaging.RunEventMessageDelta,
			Payload: messaging.RunEventPayload{
				MessageID: "message-1",
				Role:      messaging.MessageRoleAssistant,
				Content:   "hello",
			},
		},
	}
	live, ok := ProjectRunEvent(runEvent)
	if !ok || live.Sequence != 12 || live.Timestamp != 456 ||
		live.Type != string(messaging.RunEventMessageDelta) {
		t.Fatalf("live projection = %#v, %v", live, ok)
	}
	livePayload, ok := live.Payload.(dto.MessageDeltaPayload)
	if !ok || livePayload.MessageID != "message-1" || livePayload.Content != "hello" {
		t.Fatalf("live payload = %#v", live.Payload)
	}

	raw, err := json.Marshal(recordedMessagePayload{
		MessageID: "message-1",
		Role:      messaging.MessageRoleAssistant,
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("marshal chunk payload: %v", err)
	}
	replayed, ok := ProjectRunEventRecord("session-1", types.MessageChunk{
		Seq:       12,
		Type:      string(messaging.RunEventMessageDelta),
		Timestamp: 456,
		Payload:   raw,
	})
	if !ok || replayed.Sequence != 12 || replayed.Timestamp != 456 ||
		replayed.Type != string(messaging.RunEventMessageDelta) {
		t.Fatalf("replayed projection = %#v, %v", replayed, ok)
	}
	replayedPayload, ok := replayed.Payload.(dto.MessageDeltaPayload)
	if !ok || replayedPayload != livePayload {
		t.Fatalf("replayed payload = %#v, want %#v", replayed.Payload, livePayload)
	}
}

func TestProjectRunEventProjectsWorkTitleUpdatedPayload(t *testing.T) {
	runEvent := messaging.RunEvent{
		CreatedAt: time.UnixMilli(1779243000000).UTC(),
		Route:     messaging.RouteContext{SessionID: "sess_test"},
		Body: messaging.RunEventBody{
			Seq:   11,
			Event: messaging.RunEventWorkTitleUpdated,
			Payload: messaging.RunEventPayload{
				WorkTitle: &messaging.WorkTitleUpdatedPayload{
					ProjectID:    "prj_test",
					ProjectName:  "季度经营分析",
					TaskID:       "task_test",
					TaskTitle:    "季度经营分析",
					SessionID:    "sess_test",
					SessionTitle: "季度经营分析",
				},
			},
		},
	}

	event, ok := ProjectRunEvent(runEvent)
	if !ok {
		t.Fatal("expected work title event to project")
	}
	if event.Type != string(messaging.RunEventWorkTitleUpdated) {
		t.Fatalf("got type %q, want %q", event.Type, messaging.RunEventWorkTitleUpdated)
	}
	payload, ok := event.Payload.(messaging.WorkTitleUpdatedPayload)
	if !ok {
		t.Fatalf("unexpected payload type: %#v", event.Payload)
	}
	if payload.ProjectName != "季度经营分析" || payload.TaskTitle != "季度经营分析" || payload.SessionID != "sess_test" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestProjectRunEventProjectsPlanPublishedAsDirectPayload(t *testing.T) {
	plan := &messaging.PlanPublishedPayload{
		FileID:       "file_plan_1",
		Directive:    ":::plan{\"file_id\":\"file_plan_1\",\"summary_lines\":1,\"total_lines\":1}\nInspect\n:::",
		SummaryLines: 1,
		TotalLines:   1,
		StorageURI:   "file:///dev-bucket/projects/1/plans/file_plan_1.md",
	}
	runEvent := messaging.RunEvent{
		CreatedAt: time.UnixMilli(456),
		Route:     messaging.RouteContext{SessionID: "session-1"},
		Body: messaging.RunEventBody{
			Seq:   12,
			Event: messaging.RunEventPlanPublished,
			Payload: messaging.RunEventPayload{
				PlanPublished: plan,
			},
		},
	}

	projected, ok := ProjectRunEvent(runEvent)
	if !ok {
		t.Fatal("expected plan event to project")
	}
	if projected.Type != string(messaging.RunEventPlanPublished) {
		t.Fatalf("event type = %q", projected.Type)
	}
	payload, ok := projected.Payload.(messaging.PlanPublishedPayload)
	if !ok {
		t.Fatalf("payload type = %T", projected.Payload)
	}
	if payload != *plan {
		t.Fatalf("payload = %#v, want %#v", payload, *plan)
	}
}
