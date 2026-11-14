package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// dedupeWindow 内同一权限只推一次。节点重连或事件重放会重复投递
	// permission.asked，用户不需要因此收到两条一样的通知。
	dedupeWindow = 2 * time.Minute

	// dedupeLimit 限制去重表大小，超过时清掉过期项，避免长期运行后无限增长。
	dedupeLimit = 256

	// concurrency 是同时在途的推送数量。推送再慢也只排队，不阻塞订阅读取。
	concurrency = 4
)

// DeviceSource 提供推送目标。抽成接口是为了让 Notifier 在测试里不依赖数据库。
type DeviceSource interface {
	ListDevices(ctx context.Context) ([]store.Device, error)
}

// Notifier 把审批事件变成推送。它只关心“谁在等审批”，不解析会话内容。
type Notifier struct {
	devices      DeviceSource
	sender       Sender
	label        func(nodeID string) string
	sessionTitle func(nodeID, sessionID string) string
	now          func() time.Time

	gate chan struct{}

	mu   sync.Mutex
	seen map[string]time.Time
}

// Option 设置通知器的可选依赖，避免为了一个标签函数改动所有调用点。
type Option func(*Notifier)

// WithSessionTitle 提供会话标题查询。手机上多节点并行时，只写节点名
// 很难判断是哪条会话在等审批，因此标题优先使用会话标题。
func WithSessionTitle(fn func(nodeID, sessionID string) string) Option {
	return func(n *Notifier) {
		if fn != nil {
			n.sessionTitle = fn
		}
	}
}

// NewNotifier 组装通知器。label 用来把节点 ID 换成用户认识的名字。
func NewNotifier(devices DeviceSource, sender Sender, label func(nodeID string) string, opts ...Option) *Notifier {
	if label == nil {
		label = func(nodeID string) string { return nodeID }
	}
	n := &Notifier{
		devices: devices,
		sender:  sender,
		label:   label,
		now:     time.Now,
		gate:    make(chan struct{}, concurrency),
		seen:    make(map[string]time.Time),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(n)
		}
	}
	return n
}

// PullSubscriber 是持久化消费组的订阅面。
//
// 同一 durable 的多个实例共享一条消费流：一条审批事件只被一个实例处理，
// 因此不需要在实例之间去重，重投则由 Handle 的内部去重兜底。
type PullSubscriber interface {
	SubscribePull(ctx context.Context, stream, subject, durable string, handler func(context.Context, []byte) error) error
}

// Run 订阅控制面审批事件消费组并持续推送，直到 ctx 取消。
//
// 订阅阻塞到 ctx 取消；取消导致的错误属于正常退出，返回 nil。其他错误
// 交给调用方记录，避免推送链路静默停摆。
func (n *Notifier) Run(ctx context.Context, sub PullSubscriber) error {
	if sub == nil {
		return errors.New("push: pull subscriber is required")
	}

	err := sub.SubscribePull(
		ctx,
		messaging.StreamNameControlPlane,
		messaging.ControlPlaneEventWildcard(),
		messaging.ControlPlanePushConsumer,
		n.handlePayload,
	)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("push: subscribe approval events: %w", err)
	}
	return nil
}

// handlePayload 解码一条审批事件并异步推送。
//
// 返回 nil 表示消息已被处理：坏负载记录后直接 Ack，否则会无限重投；
// 推送本身放进 gate 控制的协程，慢发送不会拖住消费组。
func (n *Notifier) handlePayload(ctx context.Context, payload []byte) error {
	var ev event.Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		// 不打印原始负载：事件正文可能包含用户输入或密钥。
		logs.Warnf("push: decode event payload: %v", err)
		return nil
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
	return nil
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
			Title:     n.title(ev),
			Body:      body,
			Tag:       ev.SessionID,
			ChannelID: "permissions",
			Priority:  "high",
			Data: map[string]string{
				"node":          ev.NodeID,
				"session":       ev.SessionID,
				"permission_id": payload.ID,
			},
		})
	}

	if err := n.sender.Send(ctx, msgs); err != nil {
		// 不打印令牌与消息正文。
		logs.Warnf("push: send %d messages: %v", len(msgs), err)
	}
}

// title 生成通知标题：优先用会话标题，缺省时退回节点名。
func (n *Notifier) title(ev event.Event) string {
	if n.sessionTitle != nil {
		if title := strings.TrimSpace(n.sessionTitle(ev.NodeID, ev.SessionID)); title != "" {
			return fmt.Sprintf("需要审批 · %s", title)
		}
	}
	return fmt.Sprintf("需要审批 · %s", n.label(ev.NodeID))
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
