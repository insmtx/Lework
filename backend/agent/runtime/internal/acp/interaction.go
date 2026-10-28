package acp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

// ErrUnknownRequestID 表示决策到达时请求已结算或不存在。
var ErrUnknownRequestID = errors.New("unknown approval request id")

// PermissionBridge 把 ACP 的 session/request_permission 桥接成
// Leros 的 approval.requested 事件，并接收 cli.Runner 回写的决策。
//
// 它与 cli 层的分工：Bridge 只负责「事件 + 等待 + ACP 选项映射」，
// 用户交互由 cli.Runner 调 InteractionHandler 完成（runner.go:243-275）。
type PermissionBridge struct {
	sessionID string
	emit      func(agent.NodeEvent) error
	nonce     int64

	mu      sync.Mutex
	pending map[string]chan agent.ApprovalDecision
	seq     atomic.Uint64
}

// NewPermissionBridge 创建权限桥；emit 负责把 approval.requested 送入事件通道。
func NewPermissionBridge(sessionID string, emit func(agent.NodeEvent) error) *PermissionBridge {
	if emit == nil {
		emit = func(agent.NodeEvent) error { return nil }
	}
	return &PermissionBridge{
		sessionID: sessionID,
		emit:      emit,
		nonce:     time.Now().UnixNano(),
		pending:   map[string]chan agent.ApprovalDecision{},
	}
}

// HandlePermission 实现 acp.Client.RequestPermission 的桥接语义。
// ctx 取消时返回 Cancelled（不悬挂），并清理等待表。
func (b *PermissionBridge) HandlePermission(
	ctx context.Context,
	params acpsdk.RequestPermissionRequest,
) (acpsdk.RequestPermissionResponse, error) {
	requestID := b.newRequestID()
	decisions := make(chan agent.ApprovalDecision, 1)

	b.mu.Lock()
	b.pending[requestID] = decisions
	b.mu.Unlock()
	defer b.forget(requestID)

	if err := b.emit(agent.NewApprovalRequestedEvent(b.payload(requestID, params))); err != nil {
		return acpsdk.RequestPermissionResponse{}, fmt.Errorf("emit approval request: %w", err)
	}

	select {
	case decision := <-decisions:
		return permissionResponse(decision.Action, params.Options), nil
	case <-ctx.Done():
		return cancelledPermissionResponse(), nil
	}
}

// WriteDecision 实现 agent.ApprovalResponder，由 cli.Runner 在用户决策后调用。
// 同一 requestID 只允许写一次。
func (b *PermissionBridge) WriteDecision(requestID string, action string) error {
	b.mu.Lock()
	decisions, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownRequestID, requestID)
	}

	decision := agent.ApprovalDecision{RequestID: requestID, Action: normalizeAction(action)}
	select {
	case decisions <- decision:
		return nil
	default:
		return fmt.Errorf("approval %s is already resolved", requestID)
	}
}

// Pending 返回当前等待中的请求数（测试与可观测性用）。
func (b *PermissionBridge) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *PermissionBridge) forget(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, requestID)
}

func (b *PermissionBridge) newRequestID() string {
	return fmt.Sprintf("acp-%d-%d", b.nonce, b.seq.Add(1))
}

func (b *PermissionBridge) payload(
	requestID string,
	params acpsdk.RequestPermissionRequest,
) agent.ApprovalRequestedPayload {
	call := params.ToolCall
	kind := ""
	if call.Kind != nil {
		kind = string(*call.Kind)
	}
	title := ""
	if call.Title != nil {
		title = *call.Title
	}
	name := toolNameForPermission(kind, title)
	if name == "" {
		name = "tool"
	}
	description := strings.TrimSpace(title)
	if description == "" {
		description = name
	}
	return agent.ApprovalRequestedPayload{
		RequestID:   requestID,
		ToolCallID:  string(call.ToolCallId),
		ToolName:    DisplayToolName(name),
		Description: description,
		Arguments:   marshalArguments(call.RawInput),
		Metadata:    map[string]string{"engine": "opencode"},
	}
}

// toolNameForPermission 推导审批卡片上的工具名。
// permission 帧的 Title 可能是文件路径（实测 edit 就是），因此优先用 Kind：
// execute→bash、edit→write、read→read，其余按 Kind 原样；Kind 缺失才回退 Title。
func toolNameForPermission(kind, title string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(acpsdk.ToolKindExecute):
		return "bash"
	case string(acpsdk.ToolKindEdit):
		return "write"
	case string(acpsdk.ToolKindRead):
		return "read"
	case "":
		return DisplayToolName(ToolNameFromUpdate("", title))
	default:
		return DisplayToolName(kind)
	}
}

// normalizeAction 收口用户动作；未知动作按拒绝处理（安全侧）。
func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case agent.ApprovalActionApprove:
		return agent.ApprovalActionApprove
	case agent.ApprovalActionAlways:
		return agent.ApprovalActionAlways
	default:
		return agent.ApprovalActionDeny
	}
}

// permissionResponse 映射 Leros 动作到 ACP 选项：
// approve/always → allow_once（always 在 wire 降级为 once，不留保存规则），
// deny → reject_once。找不到对应选项时返回 Cancelled，避免误放行。
func permissionResponse(
	action string,
	options []acpsdk.PermissionOption,
) acpsdk.RequestPermissionResponse {
	wantKind := acpsdk.PermissionOptionKindAllowOnce
	fallbackID := "once"
	if normalizeAction(action) == agent.ApprovalActionDeny {
		wantKind = acpsdk.PermissionOptionKindRejectOnce
		fallbackID = "reject"
	}

	optionID := ""
	for _, option := range options {
		if option.Kind == wantKind {
			optionID = string(option.OptionId)
			break
		}
	}
	if optionID == "" {
		for _, option := range options {
			if string(option.OptionId) == fallbackID {
				optionID = fallbackID
				break
			}
		}
	}
	if optionID == "" {
		return cancelledPermissionResponse()
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Selected: &acpsdk.RequestPermissionOutcomeSelected{
				OptionId: acpsdk.PermissionOptionId(optionID),
			},
		},
	}
}

func cancelledPermissionResponse() acpsdk.RequestPermissionResponse {
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{},
		},
	}
}

var _ agent.ApprovalResponder = (*PermissionBridge)(nil)
