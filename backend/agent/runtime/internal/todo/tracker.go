package todo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/agent"
)

// Options 配置 Tracker。
type Options struct {
	RunID    string
	Observer agent.NodeObserver
}

// Tracker 维护一次运行的内存中待办列表。
type Tracker struct {
	mu       sync.Mutex
	runID    string
	observer agent.NodeObserver
	items    []agent.RuntimeTodoItem
}

var _ agent.TodoReporter = (*Tracker)(nil)

// NewTracker 创建运行时待办跟踪器。
func NewTracker(opts Options) *Tracker {
	return &Tracker{
		runID:    strings.TrimSpace(opts.RunID),
		observer: opts.Observer,
	}
}

// Snapshot 替换当前待办列表并发出快照事件。
func (t *Tracker) Snapshot(ctx context.Context, items []agent.RuntimeTodoItem) error {
	if t == nil {
		return nil
	}
	next := normalizeItems(items)
	t.mu.Lock()
	t.items = next
	t.mu.Unlock()
	return t.emit(ctx, agent.NewTodoSnapshotEvent(next))
}

// Update 更新当前待办列表并发出更新事件。
func (t *Tracker) Update(ctx context.Context, items []agent.RuntimeTodoItem, merge bool) error {
	if t == nil {
		return nil
	}
	nextItems := normalizeItems(items)
	t.mu.Lock()
	if merge {
		t.items = mergeItems(t.items, nextItems)
	} else {
		t.items = nextItems
	}
	snapshot := cloneItems(t.items)
	t.mu.Unlock()
	return t.emit(ctx, agent.NewTodoUpdatedEvent(snapshot))
}

// List 返回当前待办列表的副本。
func (t *Tracker) List() []agent.RuntimeTodoItem {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneItems(t.items)
}

func (t *Tracker) emit(ctx context.Context, event agent.NodeEvent) error {
	if t == nil || t.observer == nil {
		return nil
	}
	event.ExecutionID = t.runID
	return t.observer.Observe(ctx, event)
}

func normalizeItems(items []agent.RuntimeTodoItem) []agent.RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]int, len(items))
	result := make([]agent.RuntimeTodoItem, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		if item.Title == "" {
			continue
		}
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = stableID(item.Title, len(result))
		}
		item.Status = normalizeStatus(item.Status)
		item.Priority = strings.TrimSpace(item.Priority)
		if index, ok := seen[item.ID]; ok {
			result[index] = item
			continue
		}
		seen[item.ID] = len(result)
		result = append(result, item)
	}
	return result
}

func normalizeStatus(status string) string {
	switch Status(strings.ToLower(strings.TrimSpace(status))) {
	case StatusInProgress, "running", "active", "started":
		return string(StatusInProgress)
	case StatusCompleted, "complete", "done", "success":
		return string(StatusCompleted)
	case StatusCancelled, "canceled", "deleted", "declined", "failed", "error":
		return string(StatusCancelled)
	default:
		return string(StatusPending)
	}
}

func mergeItems(current []agent.RuntimeTodoItem, updates []agent.RuntimeTodoItem) []agent.RuntimeTodoItem {
	if len(current) == 0 {
		return cloneItems(updates)
	}
	if len(updates) == 0 {
		return cloneItems(current)
	}
	result := cloneItems(current)
	indexes := make(map[string]int, len(result))
	for index, item := range result {
		indexes[item.ID] = index
	}
	for _, update := range updates {
		if index, ok := indexes[update.ID]; ok {
			result[index] = update
			continue
		}
		indexes[update.ID] = len(result)
		result = append(result, update)
	}
	return result
}

func cloneItems(items []agent.RuntimeTodoItem) []agent.RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	return append([]agent.RuntimeTodoItem(nil), items...)
}

func stableID(title string, position int) string {
	hash := sha1.Sum([]byte(fmt.Sprintf("%d:%s", position, strings.TrimSpace(title))))
	return "todo_" + hex.EncodeToString(hash[:])[:12]
}

var _ agent.TodoReporter = (*Tracker)(nil)
