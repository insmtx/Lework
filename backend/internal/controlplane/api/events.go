package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// sseBuffer 是订阅通道容量：慢消费者积压超过该数量时由 Hub 淘汰，
	// 浏览器收到 resync 指示后应重新订阅并拉取快照。
	sseBuffer = 256

	// sseHeartbeat 是空闲连接的心跳间隔，避免代理在无事件时断开。
	sseHeartbeat = 15 * time.Second

	// resyncFrame 通知浏览器事件可能缺失，需要重新对账。
	resyncFrame = "event: resync\ndata: {\"reason\":\"slow_consumer\"}\n\n"

	// readyFrame 是建流后的首帧注释。部分反代（Next dev rewrite、nginx）
	// 在收到首块响应体之前不会下发首部，EventSource 会长时间停在 CONNECTING；
	// 先写一帧可以让客户端立刻进入 OPEN，也顺带验证链路可写。
	readyFrame = ": connected\n\n"
)

// handleEvents 提供 SSE 事件流，可选按 node 与 session 过滤。
func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming is not supported")
		return
	}

	query := r.URL.Query()
	nodeFilter := query.Get("node")
	sessionFilter := query.Get("session")

	events, cancel := a.hub.Subscribe(sseBuffer)
	defer cancel()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if err := writeSSEFrame(r.Context(), w, flusher, readyFrame); err != nil {
		return
	}

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if err := writeSSEFrame(ctx, w, flusher, ": ping\n\n"); err != nil {
				return
			}

		case ev, open := <-events:
			if !open {
				// Hub 淘汰了本订阅者：明确告知浏览器重新订阅。
				_ = writeSSEFrame(ctx, w, flusher, resyncFrame)
				return
			}
			if nodeFilter != "" && ev.NodeID != nodeFilter {
				continue
			}
			if sessionFilter != "" && ev.SessionID != sessionFilter {
				continue
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if err := writeSSEFrame(ctx, w, flusher, "data: "+string(payload)+"\n\n"); err != nil {
				return
			}
		}
	}
}

// writeSSEFrame 在请求仍有效时写出一帧并立即刷新。
//
// 浏览器或代理写阻塞时，取消请求后下一次写入前会先看到 ctx 错误并退出，
// 不会让处理协程无限挂起。
func writeSSEFrame(ctx context.Context, w io.Writer, flusher http.Flusher, frame string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write event stream: %w", err)
	}
	if _, err := io.WriteString(w, frame); err != nil {
		return fmt.Errorf("write event stream: %w", err)
	}
	flusher.Flush()
	return nil
}
