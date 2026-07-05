package todo

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestTrackerSnapshotNormalizesAndEmitsFullList(t *testing.T) {
	var emitted []agent.NodeEvent
	tracker := NewTracker(Options{
		RunID: "run_1",
		Observer: agent.NodeObserverFunc(func(_ context.Context, event agent.NodeEvent) error {
			emitted = append(emitted, event)
			return nil
		}),
	})

	err := tracker.Snapshot(context.Background(), []agent.RuntimeTodoItem{
		{Title: " inspect code ", Status: "in_progress"},
		{ID: "done", Title: "done", Status: "completed"},
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(emitted) != 1 || emitted[0].Type != agent.NodeEventTodoSnapshot {
		t.Fatalf("unexpected emitted events: %#v", emitted)
	}
	payload, ok := emitted[0].Payload.(*agent.TodoSnapshotPayload)
	if !ok {
		t.Fatalf("expected TodoSnapshotPayload, got %T", emitted[0].Payload)
	}
	items := payload.Items
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %#v", items)
	}
	if items[0].ID == "" || items[0].Title != "inspect code" || items[0].Status != "in_progress" {
		t.Fatalf("unexpected normalized item: %#v", items[0])
	}
	if emitted[0].ExecutionID != "run_1" || emitted[0].TraceID != "" {
		t.Fatalf("expected run metadata on event, got %#v", emitted[0])
	}
}

func TestTrackerUpdateMergesByID(t *testing.T) {
	tracker := NewTracker(Options{})
	if err := tracker.Snapshot(context.Background(), []agent.RuntimeTodoItem{
		{ID: "a", Title: "A", Status: "pending"},
		{ID: "b", Title: "B", Status: "pending"},
	}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := tracker.Update(context.Background(), []agent.RuntimeTodoItem{
		{ID: "b", Title: "B", Status: "completed"},
		{ID: "c", Title: "C", Status: "in_progress"},
	}, true); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	items := tracker.List()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %#v", items)
	}
	if items[1].ID != "b" || items[1].Status != "completed" {
		t.Fatalf("expected b to be updated in place, got %#v", items)
	}
	if items[2].ID != "c" || items[2].Status != "in_progress" {
		t.Fatalf("expected c appended, got %#v", items)
	}
}

func TestTrackerNormalizesFailedStatusToCancelled(t *testing.T) {
	tracker := NewTracker(Options{})
	if err := tracker.Snapshot(context.Background(), []agent.RuntimeTodoItem{
		{ID: "a", Title: "A", Status: "failed"},
		{ID: "b", Title: "B", Status: "error"},
	}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	items := tracker.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %#v", items)
	}
	if items[0].Status != string(StatusCancelled) || items[1].Status != string(StatusCancelled) {
		t.Fatalf("expected failed-like statuses to normalize to cancelled, got %#v", items)
	}
}
