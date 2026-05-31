package modelrouter

import (
	"fmt"
	"sort"
)

// ItemType identifies the kind of output item being streamed.
type ItemType int

const (
	ItemUnknown ItemType = iota
	ItemText
	ItemToolCall
	ItemReasoning
)

// itemLifecycle tracks the stream lifecycle of a single output item.
// Each output_index in the Responses SSE stream gets one itemLifecycle.
type itemLifecycle struct {
	index        int
	itemID       string
	itemType     ItemType
	added        bool // output_item.added 已发出
	contentAdded bool // content_part.added 已发出 (仅 ItemText / ItemReasoning)
	stopped      bool // output_item.done 已发出
	toolCall     *toolCallLifecycle
}

type toolCallLifecycle struct {
	callID string
	name   string
}

// StreamAggregator guarantees protocol lifecycle completeness.
//
// Upstream adapters may emit IR events with missing lifecycle preamble
// (e.g. IRStreamContentDelta without preceding IRStreamContentStart).
// The aggregator intercepts IR events before they reach the entry adapter
// and ensures:
//
//  1. response.created precedes all output events.
//  2. output_item.added + content_part.added precede every text/reasoning delta.
//  3. Every open item is closed (output_item.done) before IRStreamDone.
//  4. IRStreamDone is emitted exactly once.
//
// This eliminates "OutputTextDelta without active item" errors in Codex CLI.
type StreamAggregator struct {
	responseStarted bool
	responseID      string
	responseModel   string
	stopReason      IRStopReason
	usage           *IRUsage
	doneReceived    bool
	doneSent        bool
	doneResult      []*IRStreamEvent // cached done emission

	// items keyed by output_index
	items map[int]*itemLifecycle
	// item order (sorted by output_index) for deterministic finalization
	itemIndices []int
}

// NewStreamAggregator creates a ready-to-use aggregator.
func NewStreamAggregator() *StreamAggregator {
	return &StreamAggregator{
		items: make(map[int]*itemLifecycle),
	}
}

// ProcessIREvent processes a single IR stream event and returns zero or more
// corrected IR events. Callers must feed the returned events to the entry
// adapter in order.
func (sa *StreamAggregator) ProcessIREvent(evt *IRStreamEvent) []*IRStreamEvent {
	if evt == nil {
		return nil
	}

	// Once done was sent, ignore all subsequent events.
	if sa.doneSent {
		return nil
	}

	switch evt.Type {
	case IRStreamMessageStart:
		return sa.handleMessageStart(evt)
	case IRStreamContentStart:
		return sa.handleContentStart(evt)
	case IRStreamContentDelta:
		return sa.handleContentDelta(evt)
	case IRStreamContentStop:
		return sa.handleContentStop(evt)
	case IRStreamMessageDelta:
		return sa.handleMessageDelta(evt)
	case IRStreamDone:
		return sa.handleDone()
	case IRStreamError:
		return []*IRStreamEvent{evt}
	default:
		return nil
	}
}

// ─── Event handlers ─────────────────────────────────────────────────────

func (sa *StreamAggregator) handleMessageStart(evt *IRStreamEvent) []*IRStreamEvent {
	sa.responseID = evt.ResponseID
	sa.responseModel = evt.ResponseModel
	sa.responseStarted = true
	return []*IRStreamEvent{evt}
}

func (sa *StreamAggregator) handleContentStart(evt *IRStreamEvent) []*IRStreamEvent {
	var result []*IRStreamEvent

	sa.ensureResponsePreamble(&result)

	itemType := ItemUnknown
	if evt.Part != nil {
		switch evt.Part.Type {
		case IRPartText:
			itemType = ItemText
		case IRPartToolCall:
			itemType = ItemToolCall
		case IRPartReasoning:
			itemType = ItemReasoning
		}
	}

	item := sa.ensureItem(evt.Index, itemType)

	// Track item ID and tool info from content if provided.
	if evt.Part != nil {
		if evt.Part.ID != "" && item.itemID == "" {
			item.itemID = evt.Part.ID
		}
		if evt.Part.Type == IRPartToolCall && evt.Part.ToolCall != nil {
			if item.toolCall == nil {
				item.toolCall = &toolCallLifecycle{}
			}
			if evt.Part.ToolCall.ID != "" {
				item.toolCall.callID = evt.Part.ToolCall.ID
			}
			if evt.Part.ToolCall.Name != "" {
				item.toolCall.name = evt.Part.ToolCall.Name
			}
		}
	}

	// ContentStart itself represents output_item.added — mark it as done.
	item.added = true

	// For text items, also mark content_part.added.
	if item.itemType == ItemText {
		item.contentAdded = true
	}

	return append(result, evt)
}

func (sa *StreamAggregator) handleContentDelta(evt *IRStreamEvent) []*IRStreamEvent {
	var result []*IRStreamEvent

	sa.ensureResponsePreamble(&result)

	itemType := sa.inferItemType(evt)
	if evt.Index < 0 {
		evt.Index = 0
	}
	item := sa.ensureItem(evt.Index, itemType)

	// 1. Ensure output_item.added.
	if !item.added {
		result = append(result, sa.synthesizeOutputItemAdded(item))
		item.added = true
	}

	// 2. For text items, ensure content_part.added.
	if item.itemType == ItemText && !item.contentAdded {
		item.contentAdded = true
	}

	// 3. Forward delta. If the delta has a tool call, update tracking.
	if evt.Part != nil && evt.Part.Type == IRPartToolCall && evt.Part.ToolCall != nil {
		if item.toolCall == nil {
			item.toolCall = &toolCallLifecycle{}
		}
		if evt.Part.ToolCall.ID != "" {
			item.toolCall.callID = evt.Part.ToolCall.ID
		}
		if evt.Part.ToolCall.Name != "" {
			item.toolCall.name = evt.Part.ToolCall.Name
		}
	}

	return append(result, evt)
}

func (sa *StreamAggregator) handleContentStop(evt *IRStreamEvent) []*IRStreamEvent {
	var result []*IRStreamEvent

	sa.ensureResponsePreamble(&result)

	item, ok := sa.items[evt.Index]
	if !ok {
		// Unknown index — synthesize a stop with best-effort item.
		item = sa.ensureItem(evt.Index, ItemUnknown)
		if !item.added {
			result = append(result, sa.synthesizeOutputItemAdded(item))
			item.added = true
		}
	}

	if !item.stopped {
		item.stopped = true
	}

	return append(result, evt)
}

func (sa *StreamAggregator) handleMessageDelta(evt *IRStreamEvent) []*IRStreamEvent {
	if evt.StopReason != "" {
		sa.stopReason = evt.StopReason
	}
	if evt.Usage != nil {
		sa.usage = evt.Usage
	}

	if !sa.responseStarted {
		return []*IRStreamEvent{sa.synthesizeResponseCreated(), evt}
	}
	return []*IRStreamEvent{evt}
}

func (sa *StreamAggregator) handleDone() []*IRStreamEvent {
	sa.doneReceived = true

	if sa.doneSent {
		return nil
	}
	if sa.doneResult != nil {
		return sa.doneResult
	}

	var result []*IRStreamEvent

	// 1. response.created (空响应兜底)
	sa.ensureResponsePreamble(&result)

	// 2. Dangling cleanup: close every open item.
	for _, idx := range sa.sortedItemIndices() {
		item := sa.items[idx]
		if item.stopped {
			continue
		}
		// Ensure item was properly started before stopping it.
		if !item.added {
			if item.itemType == ItemText && !item.contentAdded {
				result = append(result, sa.synthesizeOutputItemAdded(item))
				item.added = true
			}
		}
		result = append(result, &IRStreamEvent{
			Type:  IRStreamContentStop,
			Index: item.index,
		})
	}

	// 3. MessageDelta with final stop_reason + usage.
	result = append(result, &IRStreamEvent{
		Type:       IRStreamMessageDelta,
		StopReason: sa.stopReason,
		Usage:      sa.usage,
	})

	// 4. Done.
	result = append(result, &IRStreamEvent{
		Type:  IRStreamDone,
		Usage: sa.usage,
	})

	sa.doneSent = true
	sa.doneResult = result
	return result
}

// ─── Helpers ────────────────────────────────────────────────────────────

func (sa *StreamAggregator) ensureResponsePreamble(result *[]*IRStreamEvent) {
	if !sa.responseStarted {
		*result = append(*result, sa.synthesizeResponseCreated())
		sa.responseStarted = true
	}
}

func (sa *StreamAggregator) synthesizeResponseCreated() *IRStreamEvent {
	return &IRStreamEvent{
		Type:          IRStreamMessageStart,
		ResponseID:    sa.responseID,
		ResponseModel: sa.responseModel,
	}
}

func (sa *StreamAggregator) synthesizeOutputItemAdded(item *itemLifecycle) *IRStreamEvent {
	if item.itemID == "" {
		item.itemID = fmt.Sprintf("item_%d", item.index)
	}

	switch item.itemType {
	case ItemToolCall:
		callID := item.itemID
		callName := ""
		if item.toolCall != nil {
			if item.toolCall.callID != "" {
				callID = item.toolCall.callID
			}
			callName = item.toolCall.name
		}
		return &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: item.index,
			Part: &IRContentPart{
				Type: IRPartToolCall,
				ID:   callID,
				ToolCall: &IRToolCallPart{
					ID:   callID,
					Name: callName,
				},
			},
		}
	case ItemReasoning:
		return &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: item.index,
			Part: &IRContentPart{
				Type: IRPartReasoning,
			},
		}
	default:
		return &IRStreamEvent{
			Type:  IRStreamContentStart,
			Index: item.index,
			Part: &IRContentPart{
				Type: IRPartText,
			},
		}
	}
}

// ensureItem returns the itemLifecycle for the given index, creating one if
// it does not exist.
func (sa *StreamAggregator) ensureItem(index int, itemType ItemType) *itemLifecycle {
	item, ok := sa.items[index]
	if !ok {
		item = &itemLifecycle{
			index:    index,
			itemType: itemType,
		}
		sa.items[index] = item
		sa.itemIndices = append(sa.itemIndices, index)
	} else if item.itemType == ItemUnknown && itemType != ItemUnknown {
		item.itemType = itemType
	}
	return item
}

// inferItemType returns the item type implied by a delta event.
// - DeltaText + DeltaType "reasoning" → ItemReasoning
// - DeltaJSON (tool call args) → ItemToolCall
// - DeltaText → ItemText
// - DeltaType "reasoning" → ItemReasoning
// - Otherwise → ItemUnknown
func (sa *StreamAggregator) inferItemType(evt *IRStreamEvent) ItemType {
	if evt.DeltaText != "" && evt.DeltaType == "reasoning" {
		return ItemReasoning
	}
	if evt.DeltaJSON != "" {
		return ItemToolCall
	}
	if evt.DeltaText != "" {
		return ItemText
	}
	if evt.DeltaType == "reasoning" {
		return ItemReasoning
	}
	return ItemUnknown
}

func (sa *StreamAggregator) sortedItemIndices() []int {
	if len(sa.itemIndices) <= 1 {
		return sa.itemIndices
	}
	sorted := make([]int, len(sa.itemIndices))
	copy(sorted, sa.itemIndices)
	sort.Ints(sorted)
	return sorted
}

// Finalize triggers the target protocol's completion lifecycle.
//
// Call once when the upstream stream ends — either on [DONE] or on reader EOF.
// Finalize closes every open output item, emits the aggregated stop_reason
// and usage, then emits IRStreamDone.
//
// Idempotent.  Repeated calls after the first are no-ops.
func (sa *StreamAggregator) Finalize() []*IRStreamEvent {
	if sa.doneSent {
		return nil
	}
	return sa.handleDone()
}

// IsDone reports whether Finalize (or an IRStreamDone event) has already
// been processed.
func (sa *StreamAggregator) IsDone() bool {
	return sa.doneSent
}
