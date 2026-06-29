package opencode

import (
	"context"
	"fmt"

	"github.com/insmtx/Leros/backend/agent/runtime/provider"
)

// serverResponder 通过 OpenCode HTTP API 响应审批请求。
type serverResponder struct {
	srv *OpenCodeServer
}

// WriteDecision 将审批决策转换为 OpenCode HTTP API 权限响应。
// Leros 审批动作 → OpenCode PermissionV1.Reply:
//
//	"approve" → "once"    仅本次允许
//	"always"  → "always"  始终允许
//	"deny"    → "reject"  拒绝
func (r *serverResponder) WriteDecision(requestID string, action string) error {
	var reply string
	switch action {
	case provider.ApprovalActionApprove:
		reply = "once"
	case provider.ApprovalActionAlways:
		reply = "always"
	default:
		reply = "reject"
	}

	if err := r.srv.SendPermissionDecision(context.Background(), requestID, reply); err != nil {
		return fmt.Errorf("respond approval: %w", err)
	}
	return nil
}

// questionResponder 通过 OpenCode HTTP API 响应问题请求。
type questionResponder struct {
	srv *OpenCodeServer
}

// WriteAnswer 将用户答案发送回 OpenCode。
func (r *questionResponder) WriteAnswer(requestID string, answers [][]string) error {
	if err := r.srv.SendQuestionAnswer(context.Background(), requestID, answers); err != nil {
		return fmt.Errorf("respond question: %w", err)
	}
	return nil
}
