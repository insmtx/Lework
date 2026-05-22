package todo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

type Reporter interface {
	// Snapshot 用完整列表替换当前内存 todo，并发送 todo.snapshot。
	Snapshot(ctx context.Context, items []RuntimeTodoItem) error
	// Update 更新当前内存 todo；merge=true 时按 id 合并。
	Update(ctx context.Context, items []RuntimeTodoItem, merge bool) error
	// List 返回当前内存 todo 的副本。
	List() []RuntimeTodoItem
}

// Options 是创建 Tracker 时需要的运行期上下文。
type Options struct {
	RunID   string
	TraceID string
	Sink    events.Sink
}

// Tracker 维护单次运行的内存 todo 列表，并负责发送标准事件。
type Tracker struct {
	mu      sync.Mutex
	runID   string
	traceID string
	sink    events.Sink
	items   []RuntimeTodoItem
}

// NewTracker 创建一个运行期 todo tracker。
func NewTracker(opts Options) *Tracker {
	sink := opts.Sink
	if sink == nil {
		sink = events.NewNoopSink()
	}
	return &Tracker{
		runID:   strings.TrimSpace(opts.RunID),
		traceID: strings.TrimSpace(opts.TraceID),
		sink:    sink,
	}
}

// Snapshot 规范化条目后替换当前列表，并发送完整快照事件。
func (t *Tracker) Snapshot(ctx context.Context, items []RuntimeTodoItem) error {
	if t == nil {
		return nil
	}
	next := normalizeItems(items)
	t.mu.Lock()
	t.items = next
	t.mu.Unlock()
	return t.emit(ctx, events.NewTodoSnapshot(next))
}

// Update 规范化条目后更新当前列表，并发送更新后的完整列表。
func (t *Tracker) Update(ctx context.Context, items []RuntimeTodoItem, merge bool) error {
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

// List 返回当前 todo 列表副本，避免调用方修改内部状态。
func (t *Tracker) List() []RuntimeTodoItem {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneItems(t.items)
}

// emit 补齐 run/trace 元信息后发送事件。
func (t *Tracker) emit(ctx context.Context, event *events.Event) error {
	if t == nil || t.sink == nil || event == nil {
		return nil
	}
	if event.RunID == "" {
		event.RunID = t.runID
	}
	if event.TraceID == "" {
		event.TraceID = t.traceID
	}
	return t.sink.Emit(ctx, event)
}

// normalizeItems 清理条目字段、补齐 id、归一化状态并按 id 去重。
func normalizeItems(items []RuntimeTodoItem) []RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]int, len(items))
	result := make([]RuntimeTodoItem, 0, len(items))
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

// normalizeStatus 将不同 provider 的状态值归一到 Leros 标准状态。
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

// mergeItems 按 id 更新已有条目，并把新条目追加到列表末尾。
func mergeItems(current []RuntimeTodoItem, updates []RuntimeTodoItem) []RuntimeTodoItem {
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

// cloneItems 返回切片副本，避免共享底层数组。
func cloneItems(items []RuntimeTodoItem) []RuntimeTodoItem {
	if len(items) == 0 {
		return nil
	}
	return append([]RuntimeTodoItem(nil), items...)
}

// stableID 为缺少 id 的条目生成位置相关的稳定 id。
func stableID(title string, position int) string {
	hash := sha1.Sum([]byte(fmt.Sprintf("%d:%s", position, strings.TrimSpace(title))))
	return "todo_" + hex.EncodeToString(hash[:])[:12]
}

var _ Reporter = (*Tracker)(nil)
