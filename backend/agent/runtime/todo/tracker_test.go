package todo

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

func TestTrackerSnapshotNormalizesAndEmitsFullList(t *testing.T) {
	var emitted []agent.Event
	tracker := NewTracker(Options{
		RunID: "run_1",
		Sink: events.SinkFunc(func(_ context.Context, event *agent.Event) error {
			emitted = append(emitted, *event)
			return nil
		}),
	})

	err := tracker.Snapshot(context.Background(), []events.RuntimeTodoItem{
		{Title: " inspect code ", Status: "in_progress"},
		{ID: "done", Title: "done", Status: "completed"},
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(emitted) != 1 || emitted[0].Type != events.EventTodoSnapshot {
		t.Fatalf("unexpected emitted events: %#v", emitted)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](&emitted[0])
	if err != nil {
		t.Fatalf("decode todo payload: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %#v", items)
	}
	if items[0].ID == "" || items[0].Title != "inspect code" || items[0].Status != "in_progress" {
		t.Fatalf("unexpected normalized item: %#v", items[0])
	}
	if emitted[0].RunID != "run_1" || emitted[0].TraceID != "" {
		t.Fatalf("expected run metadata on event, got %#v", emitted[0])
	}
}

func TestTrackerUpdateMergesByID(t *testing.T) {
	tracker := NewTracker(Options{})
	if err := tracker.Snapshot(context.Background(), []events.RuntimeTodoItem{
		{ID: "a", Title: "A", Status: "pending"},
		{ID: "b", Title: "B", Status: "pending"},
	}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := tracker.Update(context.Background(), []events.RuntimeTodoItem{
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
	if err := tracker.Snapshot(context.Background(), []events.RuntimeTodoItem{
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
