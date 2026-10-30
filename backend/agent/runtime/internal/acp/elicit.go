package acp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

var questionKeyPattern = regexp.MustCompile(`^q(\d+)(_custom)?$`)

// FormBridge 把 ACP 的 elicitation 表单桥接成 question.asked 事件，
// 并把用户答案按 q0..qN 回填（设计文档 §7）。
type FormBridge struct {
	sessionID string
	stats     *Stats
	emit      func(agent.NodeEvent) error
	nonce     int64

	mu                      sync.Mutex
	pending                 map[string]chan agent.QuestionAnswer
	pendingQuestionToolCall string
	seq                     atomic.Uint64
}

// NewFormBridge 创建表单桥；stats 可为 nil。
func NewFormBridge(sessionID string, stats *Stats, emit func(agent.NodeEvent) error) *FormBridge {
	if emit == nil {
		emit = func(agent.NodeEvent) error { return nil }
	}
	return &FormBridge{
		sessionID: sessionID,
		stats:     stats,
		emit:      emit,
		nonce:     time.Now().UnixNano(),
		pending:   map[string]chan agent.QuestionAnswer{},
	}
}

// SetPendingQuestionToolCall 记录最近一个未结算的 question 工具调用。
func (b *FormBridge) SetPendingQuestionToolCall(toolCallID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pendingQuestionToolCall = strings.TrimSpace(toolCallID)
}

// ResolveQuestionToolCall 在工具结算后清空绑定（只清匹配的 id）。
func (b *FormBridge) ResolveQuestionToolCall(toolCallID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pendingQuestionToolCall == strings.TrimSpace(toolCallID) {
		b.pendingQuestionToolCall = ""
	}
}

// HandleForm 实现 acp.Client.UnstableCreateElicitation 的桥接语义。
// 非 question 表单返回 Decline 并计数；ctx 取消返回 Cancel（上游可能因此中断整轮）。
func (b *FormBridge) HandleForm(
	ctx context.Context,
	req acpsdk.UnstableCreateElicitationRequest,
) (acpsdk.UnstableCreateElicitationResponse, error) {
	if req.Form == nil {
		b.decline()
		return declineElicitation(), nil
	}
	slots := questionSlots(req.Form)
	if len(slots) == 0 || !b.isQuestionForm(req.Form) {
		b.decline()
		return declineElicitation(), nil
	}

	requestID := b.newRequestID()
	answers := make(chan agent.QuestionAnswer, 1)

	b.mu.Lock()
	b.pending[requestID] = answers
	toolCallID := b.pendingQuestionToolCall
	b.mu.Unlock()
	defer b.forget(requestID)

	items := make([]agent.QuestionItem, 0, len(slots))
	for _, slot := range slots {
		items = append(items, slot.item)
	}
	payload := agent.QuestionAskedPayload{
		RequestID:  requestID,
		SessionID:  b.sessionID,
		Questions:  items,
		ToolCallID: toolCallID,
		Metadata:   map[string]string{"engine": "opencode"},
	}
	if err := b.emit(agent.NewQuestionAskedEvent(payload)); err != nil {
		return acpsdk.UnstableCreateElicitationResponse{}, fmt.Errorf("emit question request: %w", err)
	}

	select {
	case answer := <-answers:
		return acceptElicitation(slots, answer.Answers), nil
	case <-ctx.Done():
		return cancelElicitation(), nil
	}
}

// WriteAnswer 实现 agent.QuestionResponder，由 cli.Runner 回写用户答案。
func (b *FormBridge) WriteAnswer(requestID string, answers [][]string) error {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownRequestID, requestID)
	}
	select {
	case ch <- agent.QuestionAnswer{RequestID: requestID, Answers: answers}:
		return nil
	default:
		return fmt.Errorf("question %s is already resolved", requestID)
	}
}

// Pending 返回等待中的表单数量。
func (b *FormBridge) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *FormBridge) isQuestionForm(form *acpsdk.UnstableCreateElicitationForm) bool {
	b.mu.Lock()
	hasPendingToolCall := b.pendingQuestionToolCall != ""
	b.mu.Unlock()
	if hasPendingToolCall {
		return true
	}
	properties := form.RequestedSchema.Properties
	if len(properties) == 0 {
		return false
	}
	for key := range properties {
		if !questionKeyPattern.MatchString(key) {
			return false
		}
	}
	return true
}

func (b *FormBridge) decline() {
	if b.stats != nil {
		b.stats.FormDeclines.Add(1)
	}
}

func (b *FormBridge) forget(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, requestID)
}

func (b *FormBridge) newRequestID() string {
	return fmt.Sprintf("acp-q-%d-%d", b.nonce, b.seq.Add(1))
}

type questionSlot struct {
	key  string
	item agent.QuestionItem
}

// questionSlots 按 q<i> 的数字序构造问题（不能按 map 迭代序），并保留原始 key，
// 避免 q0/q2/q10 这类非连续编号被压成 q0/q1/q2。
func questionSlots(form *acpsdk.UnstableCreateElicitationForm) []questionSlot {
	properties := form.RequestedSchema.Properties
	type indexedKey struct {
		index int
		key   string
	}
	keys := make([]indexedKey, 0, len(properties))
	for key := range properties {
		match := questionKeyPattern.FindStringSubmatch(key)
		if len(match) != 3 || match[2] == "_custom" {
			continue
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		keys = append(keys, indexedKey{index: index, key: key})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].index < keys[j].index })

	slots := make([]questionSlot, 0, len(keys))
	for _, entry := range keys {
		property, _ := properties[entry.key].(map[string]any)
		header := stringField(property, "title")
		if header == "" {
			header = fmt.Sprintf("q%d", entry.index)
		}
		question := stringField(property, "description")
		if question == "" {
			question = form.Message
		}
		_, hasCustom := properties[fmt.Sprintf("q%d_custom", entry.index)]
		slots = append(slots, questionSlot{key: entry.key, item: agent.QuestionItem{
			Question:    question,
			Header:      header,
			Options:     schemaOptions(property),
			MultiSelect: stringField(property, "type") == "array",
			Custom:      hasCustom,
		}})
	}
	return slots
}

func schemaOptions(property map[string]any) []agent.QuestionOption {
	options := make([]agent.QuestionOption, 0, 4)
	for _, key := range []string{"oneOf", "anyOf"} {
		entries, _ := property[key].([]any)
		for _, raw := range entries {
			entry, _ := raw.(map[string]any)
			label := stringField(entry, "const")
			if label == "" {
				label = stringField(entry, "title")
			}
			if label == "" {
				continue
			}
			options = append(options, agent.QuestionOption{
				Label:       label,
				Description: stringField(entry, "description"),
			})
		}
	}
	if len(options) == 0 {
		entries, _ := property["enum"].([]any)
		for _, raw := range entries {
			label := fmt.Sprint(raw)
			if label == "" || label == "<nil>" {
				continue
			}
			options = append(options, agent.QuestionOption{Label: label})
		}
	}
	return options
}

func acceptElicitation(
	slots []questionSlot,
	answers [][]string,
) acpsdk.UnstableCreateElicitationResponse {
	content := map[string]any{}
	for index, slot := range slots {
		item := slot.item
		key := slot.key
		answer := []string{}
		if index < len(answers) {
			answer = answers[index]
		}
		labels := map[string]struct{}{}
		for _, option := range item.Options {
			labels[option.Label] = struct{}{}
		}
		selected := make([]string, 0, len(answer))
		custom := ""
		for _, value := range answer {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := labels[value]; ok {
				selected = append(selected, value)
				continue
			}
			if item.Custom {
				custom = value
			}
		}
		if item.MultiSelect {
			content[key] = selected
		} else {
			switch {
			case len(selected) > 0:
				content[key] = selected[0]
			case custom != "":
				content[key] = custom
			default:
				content[key] = ""
			}
		}
		if custom != "" {
			content[key+"_custom"] = custom
		}
	}
	return acpsdk.UnstableCreateElicitationResponse{
		Accept: &acpsdk.UnstableCreateElicitationAccept{Content: content},
	}
}

func declineElicitation() acpsdk.UnstableCreateElicitationResponse {
	return acpsdk.UnstableCreateElicitationResponse{
		Decline: &acpsdk.UnstableCreateElicitationDecline{},
	}
}

func cancelElicitation() acpsdk.UnstableCreateElicitationResponse {
	return acpsdk.UnstableCreateElicitationResponse{
		Cancel: &acpsdk.UnstableCreateElicitationCancel{},
	}
}

func stringField(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

var _ agent.QuestionResponder = (*FormBridge)(nil)
