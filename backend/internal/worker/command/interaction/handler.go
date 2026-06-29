// Package interaction 处理 cmd.interaction lane 的交互式命令
// （approval.resolve 和 question.answer），通过 Resolver 接口转发。
package interaction

import (
	"context"
	"fmt"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

// Resolver 是 approval/question 的解析接口。
// 生产环境由进程装配时创建的 InteractionRouter 实现。
type Resolver interface {
	ResolveApproval(requestID, action, reason string) error
	ResolveQuestion(requestID string, answers [][]string) error
}

// Handler 处理 cmd.interaction lane 的命令。
// 实现 command.InteractionHandler 接口。
type Handler struct {
	resolver Resolver
}

// New 创建 interaction handler。
func New(resolver Resolver) *Handler {
	return &Handler{resolver: resolver}
}

// HandleInteractionCommand 处理 approval.resolve 或 question.answer 命令。
func (h *Handler) HandleInteractionCommand(ctx context.Context, cmd messaging.WorkerCommand) error {
	switch cmd.Body.CommandType {
	case messaging.CommandTypeApprovalResolve:
		return h.handleApproval(ctx, cmd)
	case messaging.CommandTypeQuestionAnswer:
		return h.handleQuestion(ctx, cmd)
	default:
		logs.WarnContextf(ctx, "unknown interaction command type: %s", cmd.Body.CommandType)
		return fmt.Errorf("unknown interaction command type: %s", cmd.Body.CommandType)
	}
}

func (h *Handler) handleApproval(ctx context.Context, cmd messaging.WorkerCommand) error {
	payload, err := messaging.DecodeCommandPayload[messaging.ApprovalResolveCommandPayload](&cmd.Body)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to decode approval payload: %v", err)
		return err
	}

	requestID := cmd.Trace.RequestID
	logs.InfoContextf(ctx, "Worker received approval: session=%s request_id=%s action=%s",
		cmd.Route.SessionID, requestID, payload.Action)

	if err := h.resolver.ResolveApproval(requestID, payload.Action, payload.Reason); err != nil {
		logs.WarnContextf(ctx, "Failed to resolve approval: %v", err)
		return err
	}
	return nil
}

func (h *Handler) handleQuestion(ctx context.Context, cmd messaging.WorkerCommand) error {
	payload, err := messaging.DecodeCommandPayload[messaging.QuestionAnswerCommandPayload](&cmd.Body)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to decode question payload: %v", err)
		return err
	}

	requestID := cmd.Trace.RequestID
	logs.InfoContextf(ctx, "Worker received question answer: session=%s request_id=%s",
		cmd.Route.SessionID, requestID)

	if err := h.resolver.ResolveQuestion(requestID, payload.Answers); err != nil {
		logs.WarnContextf(ctx, "Failed to resolve question: %v", err)
		return err
	}
	return nil
}
