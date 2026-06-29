package todo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

type Reporter interface {
	// Snapshot 替换当前待办列表并发出快照事件。
	Snapshot(ctx context.Context, items []events.RuntimeTodoItem) error
	// Update 更新当前待办列表并发出更新事件。
	Update(ctx context.Context, items []events.RuntimeTodoItem, merge bool) error
	// List 返回当前待办列表的副本。
	List() []events.RuntimeTodoItem
}

// Options 配置 Tracker。
type Options struct {
	RunID string
	Sink  events.Sink
}

// Tracker 维护一次运行的内存中待办列表。
type Tracker struct {
	mu    sync.Mutex
	runID string
	sink  events.Sink
	items []events.RuntimeTodoItem
}

// NewTracker 创建运行时待办跟踪器。
func NewTracker(opts Options) *Tracker {
	sink := opts.Sink
	if sink == nil {
		sink = events.NewNoopSink()
	}
	return &Tracker{
		runID: strings.TrimSpace(opts.RunID),
		sink:  sink,
	}
}

// Snapshot 替换当前待办列表并发出快照事件。
func (t *Tracker) Snapshot(ctx context.Context, items []events.RuntimeTodoItem) error {
	if t == nil {
		return nil
	}
	next := normalizeItems(items)
	t.mu.Lock()
	t.items = next
	t.mu.Unlock()
	return t.emit(ctx, events.NewTodoSnapshot(next))
}

// Update 更新当前待办列表并发出更新事件。
func (t *Tracker) Update(ctx context.Context, items []events.RuntimeTodoItem, merge bool) error {
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
	return t.emit(ctx, events.NewTodoUpdated(snapshot))
}

// List 返回当前待办列表的副本。
func (t *Tracker) List() []events.RuntimeTodoItem {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneItems(t.items)
}

// emit 填充运行元数据并发送事件。
func (t *Tracker) emit(ctx context.Context, event *agent.Event) error {
	if t == nil || t.sink == nil || event == nil {
		return nil
	}
	if event.RunID == "" {
		event.RunID = t.runID
	}
	return t.sink.Emit(ctx, event)
}

// normalizeItems 清理并去重待办事项。
func normalizeItems(items []events.RuntimeTodoItem) []events.RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]int, len(items))
	result := make([]events.RuntimeTodoItem, 0, len(items))
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

// normalizeStatus 将提供者状态映射为 Leros 状态。
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

// mergeItems 按 ID 合并待办更新。
func mergeItems(current []events.RuntimeTodoItem, updates []events.RuntimeTodoItem) []events.RuntimeTodoItem {
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

// cloneItems 返回待办事项的副本。
func cloneItems(items []events.RuntimeTodoItem) []events.RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	return append([]events.RuntimeTodoItem(nil), items...)
}

// stableID 为待办项生成确定性 ID。
func stableID(title string, position int) string {
	hash := sha1.Sum([]byte(fmt.Sprintf("%d:%s", position, strings.TrimSpace(title))))
	return "todo_" + hex.EncodeToString(hash[:])[:12]
}

var _ Reporter = (*Tracker)(nil)
