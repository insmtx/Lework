package assistantcontext

import (
	"context"
	"errors"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

var ErrNoPendingSessionMessages = errors.New("no pending session messages")

// SessionMessageProvider 将持久化的会话消息注入到 Agent 运行上下文中。
type SessionMessageProvider interface {
	// Prepare 构建历史上下文、占位本轮待处理用户消息，并填充运行输入。
	Prepare(ctx context.Context, req *assistantdomain.RunRequest) error
	// CompleteClaimed 将本轮已占位的用户消息标记为已处理。
	CompleteClaimed(ctx context.Context, req *assistantdomain.RunRequest) error
}

// PassthroughSessionMessageProvider 无 DB 依赖的会话消息提供器。
// 不加载历史上下文，不占位消息，仅校验输入是否为空来决定是否跳过执行。
type PassthroughSessionMessageProvider struct{}

// NewPassthroughSessionMessageProvider 创建无 DB 依赖的会话消息提供器。
func NewPassthroughSessionMessageProvider() SessionMessageProvider {
	return &PassthroughSessionMessageProvider{}
}

// Prepare 校验是否有待处理的用户输入。有则放行，无则返回 ErrNoPendingSessionMessages 让 pipeline 跳过。
func (p *PassthroughSessionMessageProvider) Prepare(_ context.Context, req *assistantdomain.RunRequest) error {
	if req == nil {
		return nil
	}
	if req.Input.Type != assistantdomain.InputTypeMessage {
		return nil
	}
	if len(req.Input.Messages) == 0 {
		return ErrNoPendingSessionMessages
	}
	return nil
}

// CompleteClaimed 不需要做任何事。Worker 完成结果通过 NATS 事件通知 Server 侧 runnable 写入 DB。
func (p *PassthroughSessionMessageProvider) CompleteClaimed(_ context.Context, _ *assistantdomain.RunRequest) error {
	return nil
}
