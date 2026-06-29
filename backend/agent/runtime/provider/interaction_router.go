package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

// InteractionRouter 统一管理审批（approval）和问题（question）类型的待处理交互请求。
// 通过 channel 桥接前端的 HTTP 回传和引擎的阻塞 goroutine。
type InteractionRouter struct {
	mu              sync.Mutex
	pendingApproval map[string]*PendingApproval
	pendingQuestion map[string]*PendingQuestion
}

// PendingApproval represents an approval request waiting for a user decision.
type PendingApproval struct {
	Request   *agent.ApprovalRequest
	Responder ApprovalResponder
	ResultCh  chan *agent.ApprovalDecision
	CreatedAt time.Time
}

// PendingQuestion 表示一个等待前端回答的问题请求。
type PendingQuestion struct {
	Request   *agent.QuestionRequest
	Responder QuestionResponder
	ResultCh  chan *agent.QuestionAnswer
	CreatedAt time.Time
}

// NewInteractionRouter 创建交互路由器。
func NewInteractionRouter() *InteractionRouter {
	return &InteractionRouter{
		pendingApproval: make(map[string]*PendingApproval),
		pendingQuestion: make(map[string]*PendingQuestion),
	}
}

// ============================================================================
// Approval 方法（实现 ApprovalHandler 接口）
// ============================================================================

// RequestApproval 注册 pending approval 并阻塞等待前端 HTTP 回传决策。
// 实现 ApprovalHandler 接口。
func (r *InteractionRouter) RequestApproval(ctx context.Context, req *agent.ApprovalRequest) (*agent.ApprovalDecision, error) {
	ch := make(chan *agent.ApprovalDecision, 1)
	r.mu.Lock()
	r.pendingApproval[req.RequestID] = &PendingApproval{
		Request:   req,
		ResultCh:  ch,
		CreatedAt: time.Now(),
	}
	r.mu.Unlock()

	logs.Infof("InteractionRouter: registered pending approval request_id=%s tool=%s", req.RequestID, req.ToolName)

	defer func() {
		r.mu.Lock()
		delete(r.pendingApproval, req.RequestID)
		r.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case decision := <-ch:
		return decision, nil
	}
}

// ResolveApproval 由 HTTP handler / worker subscriber 调用，填入前端决策并唤醒阻塞的 goroutine。
func (r *InteractionRouter) ResolveApproval(requestID, action, reason string) error {
	r.mu.Lock()
	pending, ok := r.pendingApproval[requestID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("approval %s not found", requestID)
	}
	pending.ResultCh <- &agent.ApprovalDecision{
		RequestID: requestID,
		Action:    action,
		Reason:    reason,
	}
	return nil
}

// SetApprovalResponder 设置审批请求对应的 Responder。
func (r *InteractionRouter) SetApprovalResponder(requestID string, responder ApprovalResponder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pendingApproval[requestID]; ok {
		pending.Responder = responder
	}
}

// GetApprovalResponder 获取审批请求对应的 Responder。
func (r *InteractionRouter) GetApprovalResponder(requestID string) ApprovalResponder {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pendingApproval[requestID]; ok {
		return pending.Responder
	}
	return nil
}

// ============================================================================
// Question 方法（实现 QuestionHandler 接口）
// ============================================================================

// RequestAnswer 注册 pending question 并阻塞等待前端 HTTP 回传答案。
// 实现 QuestionHandler 接口。
func (r *InteractionRouter) RequestAnswer(ctx context.Context, req *agent.QuestionRequest) (*agent.QuestionAnswer, error) {
	ch := make(chan *agent.QuestionAnswer, 1)
	r.mu.Lock()
	r.pendingQuestion[req.RequestID] = &PendingQuestion{
		Request:   req,
		ResultCh:  ch,
		CreatedAt: time.Now(),
	}
	r.mu.Unlock()

	logs.Infof("InteractionRouter: registered pending question request_id=%s", req.RequestID)

	defer func() {
		r.mu.Lock()
		delete(r.pendingQuestion, req.RequestID)
		r.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case answer := <-ch:
		return answer, nil
	}
}

// ResolveQuestion 由 HTTP handler / worker subscriber 调用，填入用户答案并唤醒阻塞的 goroutine。
func (r *InteractionRouter) ResolveQuestion(requestID string, answers [][]string) error {
	r.mu.Lock()
	pending, ok := r.pendingQuestion[requestID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("question %s not found", requestID)
	}
	pending.ResultCh <- &agent.QuestionAnswer{
		RequestID: requestID,
		Answers:   answers,
	}
	return nil
}

// SetQuestionResponder 设置问题请求对应的 Responder。
func (r *InteractionRouter) SetQuestionResponder(requestID string, responder QuestionResponder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pendingQuestion[requestID]; ok {
		pending.Responder = responder
	}
}

// GetQuestionResponder 获取问题请求对应的 Responder。
func (r *InteractionRouter) GetQuestionResponder(requestID string) QuestionResponder {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pendingQuestion[requestID]; ok {
		return pending.Responder
	}
	return nil
}

// ============================================================================
// 接口断言
// ============================================================================

var _ ApprovalHandler = (*InteractionRouter)(nil)
var _ QuestionHandler = (*InteractionRouter)(nil)
