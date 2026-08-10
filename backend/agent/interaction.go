package agent

import (
	"context"
	"encoding/json"
)

// InteractionWaitObserver 观察并参与一个交互等待（审批/问答）的生命周期。
//
// 调用方在某个交互请求阻塞等待用户的决策/回答前调用 BeginInteractionWait，
// 并在等待结束后调用返回的 end。Coordinator 通过该接口实现计算槽的
// 释放/重取与交互等待容量的核算：
//
//	运行任务占用计算槽
//	  -> 触发 approval/question
//	  -> 进入交互等待槽（BeginInteractionWait）
//	  -> 释放计算槽
//	  -> 用户响应或超时
//	  -> end() 重新获取计算槽
//	  -> Runtime 继续执行
//
// 该接口由 Coordinator 注入到运行 ctx（见 WithInteractionWaitObserver）；
// 未注入时 Behavior 保持不变（end 为 nil）。
type InteractionWaitObserver interface {
	// BeginInteractionWait 开始一次交互等待。
	// 返回的 end 用于在等待结束后释放交互槽并重新获取计算槽；
	// 仅在等待正常结束时需要调用，ctx 取消时无需调用。
	// 交互等待容量已满时返回错误，调用方应以明确错误结束任务。
	BeginInteractionWait(
		ctx context.Context,
		requestID string,
		kind string,
	) (end func() error, err error)
}

type interactionWaitObserverKey struct{}

// WithInteractionWaitObserver 将交互等待观察者注入上下文。
func WithInteractionWaitObserver(ctx context.Context, observer InteractionWaitObserver) context.Context {
	return context.WithValue(ctx, interactionWaitObserverKey{}, observer)
}

// InteractionWaitObserverFromContext 从上下文中取出交互等待观察者；未注入时返回 nil。
func InteractionWaitObserverFromContext(ctx context.Context) InteractionWaitObserver {
	if ctx == nil {
		return nil
	}
	if observer, ok := ctx.Value(interactionWaitObserverKey{}).(InteractionWaitObserver); ok {
		return observer
	}
	return nil
}

// InteractionHandler handles approval and question requests from a Runtime.
// It is injected at Runtime construction time; Runtime MUST NOT depend on
// a package-level default.
type InteractionHandler interface {
	// RequestApproval asks for user approval on a tool call.
	// It blocks until a decision is made or the context is cancelled.
	RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalDecision, error)

	// RequestAnswer asks the user to answer a set of questions.
	// It blocks until answers are received or the context is cancelled.
	RequestAnswer(ctx context.Context, req *QuestionRequest) (*QuestionAnswer, error)
}

// ApprovalResponder writes an approval decision back to a provider runtime.
type ApprovalResponder interface {
	WriteDecision(requestID string, action string) error
}

// QuestionResponder writes question answers back to a provider runtime.
type QuestionResponder interface {
	WriteAnswer(requestID string, answers [][]string) error
}

const (
	// ApprovalActionApprove approves one provider operation.
	ApprovalActionApprove = "approve"
	// ApprovalActionDeny rejects one provider operation.
	ApprovalActionDeny = "deny"
	// ApprovalActionAlways approves matching future provider operations.
	ApprovalActionAlways = "always"
)

// ApprovalRequest carries the details needed for an approval decision.
type ApprovalRequest struct {
	RequestID   string
	ToolCallID  string
	ToolName    string
	Arguments   json.RawMessage
	Description string
	Runtime     string
}

// ApprovalDecision is the user's response to an approval request.
type ApprovalDecision struct {
	RequestID string
	Action    string // "approve" | "deny" | "always"
	Reason    string
}

// QuestionRequest carries one or more questions from a Runtime.
type QuestionRequest struct {
	RequestID   string
	SessionKey  string
	Questions   []QuestionItem
	ToolCallID  string
	Description string
	Runtime     string
}

// QuestionItem is a single question in a QuestionRequest.
type QuestionItem struct {
	Question    string
	Header      string
	Options     []QuestionOption
	MultiSelect bool
	Custom      bool
}

// QuestionOption is one option for a QuestionItem.
type QuestionOption struct {
	Label       string
	Description string
}

// QuestionAnswer carries the user's response to a QuestionRequest.
type QuestionAnswer struct {
	RequestID string
	Answers   [][]string
}
