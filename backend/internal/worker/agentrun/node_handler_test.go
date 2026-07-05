package agentrun

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type testJournal struct {
	events []RunEventDraft
}

func (j *testJournal) Record(_ context.Context, event RunEventDraft) error {
	j.events = append(j.events, event)
	return nil
}

func (j *testJournal) Snapshot() JournalSnapshot {
	return JournalSnapshot{}
}

type planPublisherStub struct {
	event agent.NodeEvent
	body  *messaging.RunEventBody
	err   error
}

func (p *planPublisherStub) Publish(
	_ context.Context,
	event agent.NodeEvent,
) (*messaging.RunEventBody, error) {
	p.event = event
	return p.body, p.err
}

func TestNodeHandlerFiltersInternalAndMapsPublicEvents(t *testing.T) {
	journal := &testJournal{}
	handler := NewNodeHandler(journal, nil, nil, "test", "")

	if err := handler.Observe(context.Background(), agent.NodeEvent{
		Type:    agent.NodeEventPlanReady,
		Payload: &agent.PlanReadyPayload{Path: "/tmp/plan.md"},
	}); err != nil {
		t.Fatalf("Observe(plan.ready) error = %v", err)
	}
	if len(journal.events) != 0 {
		t.Fatalf("plan.ready entered business journal: %#v", journal.events)
	}

	if err := handler.Observe(context.Background(), agent.NodeEvent{
		Type: agent.NodeEventMessageUpdate,
		Payload: &agent.MessageUpdatePayload{
			MessageID: "m1", Role: "assistant", Content: "hello",
		},
	}); err != nil {
		t.Fatalf("Observe(message.delta) error = %v", err)
	}
	if len(journal.events) != 1 ||
		journal.events[0].Body.Event != messaging.RunEventMessageDelta ||
		journal.events[0].Body.Payload.Content != "hello" {
		t.Fatalf("mapped events = %#v", journal.events)
	}
}

func TestRunEventBodyFromNodeMapsPublicNodeEvents(t *testing.T) {
	tests := []struct {
		name string
		node agent.NodeEvent
		want messaging.RunEventBody
	}{
		{
			name: "message delta",
			node: agent.NodeEvent{
				Type: agent.NodeEventMessageUpdate,
				Payload: agent.MessageUpdatePayload{
					MessageID: "message-1",
					Role:      "assistant",
					Content:   "hello",
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventMessageDelta,
				Payload: messaging.RunEventPayload{
					MessageID: "message-1",
					Role:      messaging.MessageRoleAssistant,
					Content:   "hello",
				},
			},
		},
		{
			name: "message completed",
			node: agent.NodeEvent{
				Type: agent.NodeEventMessageEnd,
				Payload: agent.MessageEndPayload{
					MessageID: "message-1",
					Content:   "done",
					Usage:     &agent.Usage{TotalTokens: 99, InputTokens: 3, OutputTokens: 5, CacheInputTokens: 1, CacheOutputTokens: 1},
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventMessageCompleted,
				Payload: messaging.RunEventPayload{
					MessageID: "message-1",
					Role:      messaging.MessageRoleAssistant,
					Content:   "done",
					Usage:     &messaging.UsagePayload{TotalTokens: 8, InputTokens: 3, OutputTokens: 5, CacheInputTokens: 1, CacheOutputTokens: 1},
				},
			},
		},
		{
			name: "reasoning delta",
			node: agent.NodeEvent{
				Type: agent.NodeEventReasoningUpdate,
				Payload: agent.ReasoningUpdatePayload{
					MessageID: "message-1",
					Content:   "thinking",
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventReasoningDelta,
				Payload: messaging.RunEventPayload{
					MessageID: "message-1",
					Role:      messaging.MessageRoleAssistant,
					Content:   "thinking",
				},
			},
		},
		{
			name: "tool call started",
			node: agent.NodeEvent{
				Type: agent.NodeEventToolExecutionStart,
				Payload: agent.ToolExecutionStartPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					Arguments:  json.RawMessage(`{"path":"README.md"}`),
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventToolCallStarted,
				Payload: messaging.RunEventPayload{ToolCall: &messaging.ToolCallPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					Arguments:  json.RawMessage(`{"path":"README.md"}`),
				}},
			},
		},
		{
			name: "tool call completed",
			node: agent.NodeEvent{
				Type: agent.NodeEventToolExecutionEnd,
				Payload: agent.ToolExecutionEndPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					Result:     json.RawMessage(`{"ok":true}`),
					ElapsedMS:  12,
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventToolCallFinished,
				Payload: messaging.RunEventPayload{ToolResult: &messaging.ToolCallResultPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					Result:     json.RawMessage(`{"ok":true}`),
					ElapsedMS:  12,
				}},
			},
		},
		{
			name: "tool call failed",
			node: agent.NodeEvent{
				Type: agent.NodeEventToolExecutionEnd,
				Payload: agent.ToolExecutionEndPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					IsError:    true,
					Error:      "not found",
					ElapsedMS:  15,
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventToolCallFinished,
				Payload: messaging.RunEventPayload{ToolResult: &messaging.ToolCallResultPayload{
					ToolCallID: "tool-1",
					Name:       "read",
					Error:      "not found",
					IsError:    true,
					ElapsedMS:  15,
				}},
			},
		},
		{
			name: "todo snapshot",
			node: agent.NodeEvent{
				Type: agent.NodeEventTodoSnapshot,
				Payload: agent.TodoSnapshotPayload{Items: []agent.RuntimeTodoItem{{
					ID: "todo-1", Title: "设计", Status: "completed", Priority: "high",
				}}},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventTodoSnapshot,
				Payload: messaging.RunEventPayload{Todos: []messaging.RuntimeTodoItem{{
					ID: "todo-1", Title: "设计", Status: "completed", Priority: "high",
				}}},
			},
		},
		{
			name: "todo updated",
			node: agent.NodeEvent{
				Type: agent.NodeEventTodoUpdated,
				Payload: agent.TodoUpdatedPayload{Items: []agent.RuntimeTodoItem{{
					ID: "todo-1", Title: "实现", Status: "in_progress", Priority: "medium",
				}}},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventTodoUpdated,
				Payload: messaging.RunEventPayload{Todos: []messaging.RuntimeTodoItem{{
					ID: "todo-1", Title: "实现", Status: "in_progress", Priority: "medium",
				}}},
			},
		},
		{
			name: "approval requested",
			node: agent.NodeEvent{
				Type: agent.NodeEventApprovalRequested,
				Payload: agent.ApprovalRequestedPayload{
					RequestID:   "approval-1",
					ToolName:    "shell",
					ToolCallID:  "tool-1",
					Description: "Run command",
					Arguments:   json.RawMessage(`{"cmd":"go test ./..."}`),
					Metadata:    map[string]string{"provider": "codex"},
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventApprovalRequested,
				Payload: messaging.RunEventPayload{ApprovalRequest: &messaging.ApprovalRequestPayload{
					RequestID:   "approval-1",
					ToolName:    "shell",
					ToolCallID:  "tool-1",
					Description: "Run command",
					Arguments:   json.RawMessage(`{"cmd":"go test ./..."}`),
					Metadata:    map[string]string{"provider": "codex"},
				}},
			},
		},
		{
			name: "approval resolved",
			node: agent.NodeEvent{
				Type: agent.NodeEventApprovalResolved,
				Payload: agent.ApprovalResolvedPayload{
					RequestID: "approval-1",
					Action:    "approve",
					Reason:    "safe",
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventApprovalResolved,
				Payload: messaging.RunEventPayload{ApprovalDecision: &messaging.ApprovalDecisionPayload{
					RequestID: "approval-1",
					Action:    "approve",
					Reason:    "safe",
				}},
			},
		},
		{
			name: "question asked",
			node: agent.NodeEvent{
				Type: agent.NodeEventQuestionAsked,
				Payload: agent.QuestionAskedPayload{
					RequestID:       "question-1",
					SessionID:       "session-1",
					ToolCallID:      "tool-1",
					MessageID:       "message-1",
					InteractionType: "plan_confirmation",
					Questions: []agent.QuestionItem{{
						Question:    "继续？",
						Header:      "确认",
						MultiSelect: true,
						Custom:      true,
						Options: []agent.QuestionOption{{
							Label:       "Yes",
							Description: "继续执行",
						}},
					}},
					Metadata: map[string]string{"plan_error": "resolve_failed"},
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventQuestionAsked,
				Payload: messaging.RunEventPayload{QuestionRequest: &messaging.QuestionRequestPayload{
					RequestID:       "question-1",
					SessionID:       "session-1",
					ToolCallID:      "tool-1",
					MessageID:       "message-1",
					InteractionType: "plan_confirmation",
					Questions: []messaging.QuestionItem{{
						Question:    "继续？",
						Header:      "确认",
						MultiSelect: true,
						Custom:      true,
						Options: []messaging.QuestionOption{{
							Label:       "Yes",
							Description: "继续执行",
						}},
					}},
					Metadata: map[string]string{"plan_error": "resolve_failed"},
				}},
			},
		},
		{
			name: "question answered",
			node: agent.NodeEvent{
				Type: agent.NodeEventQuestionAnswered,
				Payload: agent.QuestionAnsweredPayload{
					RequestID: "question-1",
					Answers:   [][]string{{"Yes", "Custom"}},
				},
			},
			want: messaging.RunEventBody{
				Event: messaging.RunEventQuestionAnswered,
				Payload: messaging.RunEventPayload{QuestionAnswer: &messaging.QuestionAnswerPayload{
					RequestID: "question-1",
					Answers:   [][]string{{"Yes", "Custom"}},
				}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok, err := runEventBodyFromNode(test.node)
			if err != nil {
				t.Fatalf("runEventBodyFromNode() error = %v", err)
			}
			if !ok {
				t.Fatal("runEventBodyFromNode() ok = false")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("runEventBodyFromNode() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRunEventBodyFromNodeKeepsRuntimeOnlyEventsInternal(t *testing.T) {
	tests := []agent.NodeEvent{
		{Type: agent.NodeEventToolExecutionUpdate, Payload: agent.ToolExecutionUpdatePayload{ToolCallID: "tool-1", Content: "progress"}},
		{Type: agent.NodeEventAgentStart, Payload: agent.AgentStartedPayload{ProviderSessionID: "provider-session-1"}},
		{Type: agent.NodeEventPlanReady, Payload: agent.PlanReadyPayload{Path: "/tmp/PLAN.md"}},
	}
	for _, test := range tests {
		got, ok, err := runEventBodyFromNode(test)
		if err != nil {
			t.Fatalf("runEventBodyFromNode(%s) error = %v", test.Type, err)
		}
		if ok || got.Event != "" {
			t.Fatalf("runEventBodyFromNode(%s) = %#v, %v; want no public event", test.Type, got, ok)
		}
	}
}

func TestNodeHandlerRejectsWrongPayloadType(t *testing.T) {
	handler := NewNodeHandler(&testJournal{}, nil, nil, "test", "")
	err := handler.Observe(context.Background(), agent.NodeEvent{
		Type:    agent.NodeEventMessageUpdate,
		Payload: &agent.TodoSnapshotPayload{},
	})
	if err == nil {
		t.Fatal("Observe() error = nil for wrong payload")
	}
}

func TestNodeHandlerPublishesPlanReadyAsBusinessPlanPublished(t *testing.T) {
	published := &messaging.RunEventBody{
		Event: messaging.RunEventPlanPublished,
		Payload: messaging.RunEventPayload{
			PlanPublished: &messaging.PlanPublishedPayload{
				FileID:    "file-1",
				Directive: ":::plan{}\nsummary\n:::",
			},
		},
	}
	publisher := &planPublisherStub{body: published}
	journal := &testJournal{}
	handler := NewNodeHandler(journal, publisher, nil, "opencode", "session-1")
	occurredAt := time.Now().UTC()

	err := handler.Observe(context.Background(), agent.NodeEvent{
		ExecutionID: "run-1",
		TraceID:     "trace-1",
		Type:        agent.NodeEventPlanReady,
		OccurredAt:  occurredAt,
		Payload: &agent.PlanReadyPayload{
			Path:              "/workspace/PLAN.md",
			DisplayPath:       "PLAN.md",
			ProviderSessionID: "provider-session-1",
		},
	})
	if err != nil {
		t.Fatalf("Observe(plan.ready) error = %v", err)
	}
	if publisher.event.Type != agent.NodeEventPlanReady {
		t.Fatalf("publisher event = %#v", publisher.event)
	}
	if len(journal.events) != 1 {
		t.Fatalf("journal events = %#v, want one plan.published", journal.events)
	}
	got := journal.events[0]
	if !got.OccurredAt.Equal(occurredAt) || got.Body.Event != messaging.RunEventPlanPublished {
		t.Fatalf("journal event = %#v", got)
	}
	if got.Body.Payload.PlanPublished == nil || got.Body.Payload.PlanPublished.FileID != "file-1" {
		t.Fatalf("plan payload = %#v", got.Body.Payload.PlanPublished)
	}
	if handler.PlanError() != nil {
		t.Fatalf("PlanError() = %v", handler.PlanError())
	}
}
