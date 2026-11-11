package push

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// dedupeWindow 内同一权限只推一次。节点重连或事件重放会重复投递
	// permission.asked，用户不需要因此收到两条一样的通知。
	dedupeWindow = 2 * time.Minute

	// dedupeLimit 限制去重表大小，超过时清掉过期项，避免长期运行后无限增长。
	dedupeLimit = 256

	// concurrency 是同时在途的推送数量。推送再慢也只排队，不阻塞订阅读取。
	concurrency = 4

	// runBuffer 是 Hub 订阅缓冲；慢消费者会被 Hub 淘汰并触发客户端 resync，
	// 因此这里不需要无限缓冲。
	runBuffer = 64
)

// DeviceSource 提供推送目标。抽成接口是为了让 Notifier 在测试里不依赖数据库。
type DeviceSource interface {
	ListDevices(ctx context.Context) ([]store.Device, error)
}

// Notifier 把审批事件变成推送。它只关心“谁在等审批”，不解析会话内容。
type Notifier struct {
	devices DeviceSource
	sender  Sender
	label   func(nodeID string) string
	now     func() time.Time

	gate chan struct{}

	mu   sync.Mutex
	seen map[string]time.Time
}

// NewNotifier 组装通知器。label 用来把节点 ID 换成用户认识的名字。
func NewNotifier(devices DeviceSource, sender Sender, label func(nodeID string) string) *Notifier {
	if label == nil {
		label = func(nodeID string) string { return nodeID }
	}
	return &Notifier{
		devices: devices,
		sender:  sender,
		label:   label,
		now:     time.Now,
		gate:    make(chan struct{}, concurrency),
		seen:    make(map[string]time.Time),
	}
}

// Run 订阅事件中心并持续推送，直到 ctx 取消。
//
// 每个事件交给独立 goroutine 处理，用 gate 限流：推送慢时宁可丢弃事件，
// 也不能让 Hub 的读取循环停下来。
func (n *Notifier) Run(ctx context.Context, hub *event.Hub) {
	events, cancel := hub.Subscribe(runBuffer)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			select {
			case n.gate <- struct{}{}:
				go func() {
					defer func() { <-n.gate }()
					n.Handle(ctx, ev)
				}()
			default:
				logs.Warnf("push: queue full, dropping %s", ev.Type)
			}
		}
	}
}

// Handle 处理单个事件。调用方可以是 Run 的 worker，也可以是测试。
//
// 去重与发送都在这里完成：去重的“查 + 插”必须原子，否则并发调用会重复推送。
func (n *Notifier) Handle(ctx context.Context, ev event.Event) {
	if ev.Type != "permission.asked" || ev.SessionID == "" {
		return
	}

	var payload struct {
		ID      string `json:"id"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Session string `json:"sessionID"`
	}
	if len(ev.Data) == 0 {
		return
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.ID == "" {
		return
	}
	if !n.firstSeen(ev.NodeID, ev.SessionID, payload.ID) {
		return
	}

	devices, err := n.devices.ListDevices(ctx)
	if err != nil {
		logs.Warnf("push: list devices: %v", err)
		return
	}
	if len(devices) == 0 {
		return
	}

	body := payload.Action
	if payload.Message != "" {
		if body != "" {
			body += " · "
		}
		body += payload.Message
	}

	msgs := make([]Message, 0, len(devices))
	for _, device := range devices {
		msgs = append(msgs, Message{
			To:        device.PushToken,
			Title:     fmt.Sprintf("需要审批 · %s", n.label(ev.NodeID)),
			Body:      body,
			Tag:       ev.SessionID,
			ChannelID: "permissions",
			Priority:  "high",
			Data: map[string]string{
				"node":       ev.NodeID,
				"session":    ev.SessionID,
				"permission": payload.ID,
			},
		})
	}

	if err := n.sender.Send(ctx, msgs); err != nil {
		// 不打印令牌与消息正文。
		logs.Warnf("push: send %d messages: %v", len(msgs), err)
	}
}

// firstSeen 原子地判断并记录去重键。返回 true 表示这次应该推送。
func (n *Notifier) firstSeen(nodeID, sessionID, permissionID string) bool {
	key := nodeID + "/" + sessionID + "/" + permissionID
	now := n.now()

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.seen == nil {
		n.seen = make(map[string]time.Time)
	}
	if at, ok := n.seen[key]; ok && now.Sub(at) < dedupeWindow {
		return false
	}
	if len(n.seen) >= dedupeLimit {
		for k, at := range n.seen {
			if now.Sub(at) >= dedupeWindow {
				delete(n.seen, k)
			}
		}
	}
	n.seen[key] = now
	return true
}
