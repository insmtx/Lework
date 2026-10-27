package acp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

const (
	// DefaultPromptTimeout 是探针单轮的上限；生产由 opencode 传入
	// progress_idle_timeout_seconds，整轮墙钟另行配置。
	DefaultPromptTimeout = 3 * time.Minute
	// defaultCancelDrain 是取消后等待 Prompt 返回的上限（上游 CancelDrainTimeout）。
	defaultCancelDrain = 5 * time.Second
	idlePollInterval   = 100 * time.Millisecond
)

// ErrIdleTimeout 与 v1 文案完全一致（invoker.go:443），前端无需改文案。
var ErrIdleTimeout = errors.New(`长时间未操作，任务已暂停，如需继续请输入"继续"。`)

// ErrorKind 是传输层错误分类，供 opencode 映射为 RunEventError.Code。
type ErrorKind string

const (
	ErrorKindAuthRequired  ErrorKind = "auth_required"
	ErrorKindInvalidParams ErrorKind = "invalid_params"
	ErrorKindProtocol      ErrorKind = "protocol_error"
	ErrorKindRuntime       ErrorKind = "runtime_error"
	ErrorKindPeerClosed    ErrorKind = "peer_closed"
	ErrorKindCanceled      ErrorKind = "canceled"
	ErrorKindIdleTimeout   ErrorKind = "idle_timeout"
)

// ClassifyError 按设计文档 §10 的分类表归类 Prompt/会话错误。
func ClassifyError(err error) ErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrIdleTimeout) {
		return ErrorKindIdleTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorKindCanceled
	}

	var requestErr *acpsdk.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.Code {
		case -32000:
			return ErrorKindAuthRequired
		case -32602:
			return ErrorKindInvalidParams
		case -32601:
			return ErrorKindProtocol
		default:
			return ErrorKindRuntime
		}
	}
	if strings.Contains(err.Error(), "peer disconnected") {
		return ErrorKindPeerClosed
	}
	return ErrorKindRuntime
}

// PromptOptions 控制单轮 Prompt 的三个时间边界。
type PromptOptions struct {
	// IdleTimeout 超过该时长没有任何通知/反向请求则主动 Cancel（0 表示关闭）。
	IdleTimeout time.Duration
	// CancelDrainTimeout 取消后等待终态的上限，超时杀进程；零值用 5s。
	CancelDrainTimeout time.Duration
	// WallClockTimeout 整轮硬超时（0 表示不设）。
	WallClockTimeout time.Duration
}

type promptResult struct {
	resp acpsdk.PromptResponse
	err  error
}

// Prompt 发送一轮文本提示，默认带 3 分钟整轮墙钟（探针行为）。
func (s *Session) Prompt(ctx context.Context, text string) (acpsdk.PromptResponse, error) {
	return s.PromptWithOptions(ctx, text, PromptOptions{WallClockTimeout: DefaultPromptTimeout})
}

// PromptWithOptions 发送一轮提示并等待终态。
//
// 取消语义（设计文档 §3.3）：
//   - ctx 取消：SDK 已自动发 session/cancel，这里只等排空，超时才杀进程；
//   - 空闲超时（ctx 仍活着）：由本函数主动发 Cancel，再等排空；
//   - 两种情况都不覆盖已经产生的终态（Prompt 返回则立即返回）。
func (s *Session) PromptWithOptions(
	ctx context.Context,
	text string,
	opts PromptOptions,
) (acpsdk.PromptResponse, error) {
	if strings.TrimSpace(text) == "" {
		return acpsdk.PromptResponse{}, fmt.Errorf("acp prompt text is required")
	}
	if opts.CancelDrainTimeout <= 0 {
		opts.CancelDrainTimeout = defaultCancelDrain
	}

	promptCtx := ctx
	cancel := func() {}
	if opts.WallClockTimeout > 0 {
		promptCtx, cancel = context.WithTimeout(ctx, opts.WallClockTimeout)
	}
	defer cancel()

	// 心跳定义包含「Prompt 未返回」，从发起时开始计时。
	s.conn.client.Touch()

	results := make(chan promptResult, 1)
	go func() {
		resp, err := s.conn.sdk.Prompt(promptCtx, acpsdk.PromptRequest{
			SessionId: s.ID,
			Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(text)},
		})
		results <- promptResult{resp: resp, err: err}
	}()

	var ticker *time.Ticker
	var idleCh <-chan time.Time
	if opts.IdleTimeout > 0 {
		interval := idlePollInterval
		if quarter := opts.IdleTimeout / 4; quarter > 0 && quarter < interval {
			interval = quarter
		}
		if interval < 10*time.Millisecond {
			interval = 10 * time.Millisecond
		}
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
		idleCh = ticker.C
	}

	idleCanceled := false
	var drainCh <-chan time.Time
	for {
		select {
		case result := <-results:
			if idleCanceled {
				return acpsdk.PromptResponse{}, ErrIdleTimeout
			}
			return result.resp, result.err

		case <-ctx.Done():
			// SDK 已发过 session/cancel；这里只负责排空与兜底。
			select {
			case result := <-results:
				if idleCanceled {
					return acpsdk.PromptResponse{}, ErrIdleTimeout
				}
				return result.resp, ctx.Err()
			case <-time.After(opts.CancelDrainTimeout):
				s.conn.Kill()
				return acpsdk.PromptResponse{}, ctx.Err()
			}

		case <-idleCh:
			if idleCanceled {
				continue
			}
			if time.Since(s.conn.client.LastActivity()) < opts.IdleTimeout {
				continue
			}
			idleCanceled = true
			if err := s.conn.Cancel(context.Background(), s.ID); err != nil {
				return acpsdk.PromptResponse{}, fmt.Errorf("%w: cancel: %v", ErrIdleTimeout, err)
			}
			drainCh = time.After(opts.CancelDrainTimeout)

		case <-drainCh:
			s.conn.Kill()
			return acpsdk.PromptResponse{}, fmt.Errorf("%w（取消未在 %s 内收敛，已终止进程）",
				ErrIdleTimeout, opts.CancelDrainTimeout)
		}
	}
}
