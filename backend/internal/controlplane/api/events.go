package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
)

const (
	// sseBuffer 是订阅通道容量：慢消费者积压超过该数量时由 Hub 淘汰，
	// 浏览器收到 resync 指示后应重新订阅并拉取快照。
	sseBuffer = 256

	// sseHeartbeat 是空闲连接的心跳间隔，避免代理在无事件时断开。
	sseHeartbeat = 15 * time.Second

	// nodeMemberEventPrefix 是成员变更事件的类型前缀。
	//
	// 收到这类事件就立刻重算可见集合，成员被授予或撤销不必等满一个
	// 刷新周期；跨实例时事件可能丢失，周期刷新仍然必须保留。
	nodeMemberEventPrefix = "node.member."

	// resyncFrame 通知浏览器事件可能缺失，需要重新对账。
	resyncFrame = "event: resync\ndata: {\"reason\":\"slow_consumer\"}\n\n"

	// readyFrame 是建流后的首帧注释。部分反代（Next dev rewrite、nginx）
	// 在收到首块响应体之前不会下发首部，EventSource 会长时间停在 CONNECTING；
	// 先写一帧可以让客户端立刻进入 OPEN，也顺带验证链路可写。
	readyFrame = ": connected\n\n"
)

// allowedNodes 返回 p 可见的节点 ID 集合，供事件流过滤使用。
//
// SSE 与 /api/nodes 必须用同一套可见范围，否则「列表里看不到、事件里却收到」
// 会直接泄漏别人的节点；因此这里只做一层薄包装，不自行解释角色。
func (a *API) allowedNodes(ctx context.Context, p authz.Principal) (map[string]struct{}, error) {
	visible, err := authz.VisibleNodeIDs(ctx, a.store, p)
	if err != nil {
		return nil, fmt.Errorf("authorize event stream nodes: %w", err)
	}
	return visible, nil
}

// eventVisible 判定一条事件能否下发给当前订阅者。
//
// 没有 NodeID 的事件无法判定归属，按不可见处理（fail closed）——resync 不是
// Event，走的是另一条代码路径，不受这里影响。
func eventVisible(visible map[string]struct{}, nodeID string) bool {
	if nodeID == "" {
		return false
	}
	_, ok := visible[nodeID]
	return ok
}

// restrictToNode 把可见集合收敛到 ?node= 指定的节点。
//
// node 过滤只能收窄范围：请求不可见节点时返回空集合，流保持沉默而不是报错，
// 否则状态码与错误文案就成了节点存在性的探针。
func restrictToNode(visible map[string]struct{}, nodeID string) map[string]struct{} {
	if nodeID == "" {
		return visible
	}
	if _, ok := visible[nodeID]; !ok {
		return nil
	}
	return map[string]struct{}{nodeID: {}}
}

// handleEvents 提供 SSE 事件流，可选按 node 与 session 过滤。
//
// 事件先按当前身份的可见节点集合过滤再写帧：非成员既拿不到别人节点的事件，
// 也无法从「有没有事件」推断节点是否存在。可见集合在订阅时计算一次，之后
// 每个刷新周期或收到 node.member.* 事件时重算，满足设计 §9 的 30 秒时效。
//
// live=1 时额外订阅 token 级增量事件；默认只收状态事件，避免总览等常驻
// 连接被高频 token 流量拖慢。
func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming is not supported")
		return
	}

	ctx := r.Context()
	principal := a.principal(r)

	// 可见集合必须先算出来再写 SSE 头：算不出来就整体失败，绝不能带着
	// 空集合建流——那样客户端会静默收不到任何事件，比直接 500 更难排查。
	visible, err := a.allowedNodes(ctx, principal)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	query := r.URL.Query()
	nodeFilter := query.Get("node")
	sessionFilter := query.Get("session")
	includeLive := query.Get("live") == "1"

	visible = restrictToNode(visible, nodeFilter)

	var (
		events <-chan event.Event
		cancel func()
	)
	if includeLive {
		events, cancel = a.hub.Subscribe(sseBuffer, event.WithLive())
	} else {
		events, cancel = a.hub.Subscribe(sseBuffer)
	}
	defer cancel()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if err := writeSSEFrame(ctx, w, flusher, readyFrame); err != nil {
		return
	}

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	// 心跳与可见集合刷新各用各的 ticker：心跳必须稳定在 15 秒，不能被
	// 成员重算拖慢或提前。
	refresh := time.NewTicker(a.eventsRefreshInterval())
	defer refresh.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if err := writeSSEFrame(ctx, w, flusher, ": ping\n\n"); err != nil {
				return
			}

		case <-refresh.C:
			visible = restrictToNode(a.recomputeVisible(ctx, principal, visible), nodeFilter)

		case ev, open := <-events:
			if !open {
				// Hub 淘汰了本订阅者：明确告知浏览器重新订阅。
				_ = writeSSEFrame(ctx, w, flusher, resyncFrame)
				return
			}
			// 成员事件到达时先重算再判断：新授予的节点事件（包括这条成员
			// 事件本身）应当立即生效，而不是被旧集合丢掉。
			if isNodeMemberEvent(ev.Type) {
				visible = restrictToNode(a.recomputeVisible(ctx, principal, visible), nodeFilter)
			}
			if !eventVisible(visible, ev.NodeID) {
				continue
			}
			// 只有带 session_id 且不匹配才丢弃：agent.resync_required 等
			// 节点级事件没有 session_id，必须照常下发，否则页面收不到
			// 「清空缓冲 + 重拉」的信号。
			if sessionFilter != "" && ev.SessionID != "" && ev.SessionID != sessionFilter {
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

// eventsRefreshInterval 返回重算可见集合的周期；零值或负值回落到默认值，
// 避免测试或手工装配出的 API 用 time.NewTicker 直接 panic。
func (a *API) eventsRefreshInterval() time.Duration {
	if a.eventsRefresh <= 0 {
		return defaultEventsRefresh
	}
	return a.eventsRefresh
}

// recomputeVisible 重算可见节点集合，失败时保留旧集合。
//
// 重算只是一次可见性刷新，数据库抖动不该把已经建立的事件流打断；日志只记
// 错误本身，事件内容与身份凭据都不落日志。
func (a *API) recomputeVisible(
	ctx context.Context,
	p authz.Principal,
	prev map[string]struct{},
) map[string]struct{} {
	next, err := a.allowedNodes(ctx, p)
	if err != nil {
		logs.Debugf("event stream: recompute visible nodes: %v", err)
		return prev
	}
	return next
}

// isNodeMemberEvent 判断事件类型是否属于成员变更。
func isNodeMemberEvent(eventType string) bool {
	return strings.HasPrefix(eventType, nodeMemberEventPrefix)
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
