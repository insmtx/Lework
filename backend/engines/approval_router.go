package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

// ApprovalRouter 管理审批请求的异步等待和 HTTP 回传。
// 替代阻塞式 ApprovalHandler，通过 channel 桥接前端 HTTP 决策和引擎 goroutine。
type ApprovalRouter struct {
	mu      sync.Mutex
	pending map[string]*PendingApproval
}

// PendingApproval 表示一个等待前端决策的审批请求。
type PendingApproval struct {
	Request   *ApprovalRequest
	Responder ApprovalResponder
	ResultCh  chan *ApprovalDecision
	CreatedAt time.Time
}

// NewApprovalRouter 创建审批路由器。
func NewApprovalRouter() *ApprovalRouter {
	return &ApprovalRouter{
		pending: make(map[string]*PendingApproval),
	}
}

// Wait 注册 pending 并阻塞等待前端 HTTP 回传决策。
// 实现 ApprovalHandler 接口，供 consumeEvents 调用。
func (r *ApprovalRouter) RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalDecision, error) {
	ch := make(chan *ApprovalDecision, 1)
	r.mu.Lock()
	r.pending[req.RequestID] = &PendingApproval{
		Request:   req,
		ResultCh:  ch,
		CreatedAt: time.Now(),
	}
	r.mu.Unlock()

	logs.Infof("ApprovalRouter: registered pending request_id=%s tool=%s", req.RequestID, req.ToolName)

	defer func() {
		r.mu.Lock()
		delete(r.pending, req.RequestID)
		r.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case decision := <-ch:
		return decision, nil
	}
}

// Resolve 由 HTTP handler 调用，填入前端决策并唤醒阻塞的 goroutine。
func (r *ApprovalRouter) Resolve(requestID, action, reason string) error {
	r.mu.Lock()
	pending, ok := r.pending[requestID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("approval %s not found", requestID)
	}
	pending.ResultCh <- &ApprovalDecision{
		RequestID: requestID,
		Action:    action,
		Reason:    reason,
	}
	return nil
}

// SetResponder 设置审批请求对应的 Responder（由 consumeEvents 在发送审批请求后调用）。
func (r *ApprovalRouter) SetResponder(requestID string, responder ApprovalResponder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pending[requestID]; ok {
		pending.Responder = responder
	}
}

// GetResponder 获取审批请求对应的 Responder。
func (r *ApprovalRouter) GetResponder(requestID string) ApprovalResponder {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pending, ok := r.pending[requestID]; ok {
		return pending.Responder
	}
	return nil
}

var _ ApprovalHandler = (*ApprovalRouter)(nil)

// DefaultApprovalRouter 是全局审批路由器实例，供 HTTP handler 和 runtime 共享。
var DefaultApprovalRouter = NewApprovalRouter()
