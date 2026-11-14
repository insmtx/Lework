package controlplane

import (
	"context"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// nodeOfflineEvent 是节点心跳超时后的对账事件类型。
//
// 网关在连接断开时也会发同名事件；对账覆盖的是连接静默消失、进程被杀
// 等网关来不及清理的情况，浏览器端两种来源处理方式一致。
const nodeOfflineEvent = "node.offline"

// reconcileInterval 是对账周期。取小于默认 35s 心跳超时的值，保证节点
// 静默后最多一个周期就能被标记离线。
const reconcileInterval = 10 * time.Second

// NodeLister 提供节点及其最后心跳时间，抽象成接口便于单测注入假数据。
type NodeLister interface {
	ListNodes(ctx context.Context) ([]store.Node, error)
}

// EventPublisher 发布对账事件；与 agentgw.EventPublisher 同形，但保持
// 各自包内最小依赖。
type EventPublisher interface {
	PublishEvent(ctx context.Context, ev event.Event) error
}

// Reconciler 周期检查节点心跳并发布离线事件。
//
// 每个节点只在持续超时期间发布一次：网关自己会发断开事件，对账只补充
// 静默消失的节点，重复发布会让浏览器与推送侧收到多条相同的离线通知。
type Reconciler struct {
	nodes     NodeLister
	publisher EventPublisher
	timeout   time.Duration
	now       func() time.Time

	// lastOffline 记录已为哪些节点发布过离线事件。
	lastOffline map[string]bool

	interval time.Duration
}

// NewReconciler 组装节点对账器。timeout 与网关心跳判定保持一致。
func NewReconciler(nodes NodeLister, publisher EventPublisher, timeout time.Duration) *Reconciler {
	return &Reconciler{
		nodes:       nodes,
		publisher:   publisher,
		timeout:     timeout,
		now:         time.Now,
		lastOffline: make(map[string]bool),
		interval:    reconcileInterval,
	}
}

// Run 立即对账一次，之后按固定周期重复，直到 ctx 取消。
func (r *Reconciler) Run(ctx context.Context) {
	interval := r.interval
	if interval <= 0 {
		interval = reconcileInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

// sweep 执行一轮对账。
//
// 单次失败只记日志：下一轮会重试，不能让一次网络抖动把所有节点卡在
// “已发过离线”或“永远不发”的状态。
func (r *Reconciler) sweep(ctx context.Context) {
	// 构造时依赖由调用方保证；这里再兜底一次，避免 nil 解引用把进程打挂。
	if r.nodes == nil || r.publisher == nil {
		return
	}

	nodes, err := r.nodes.ListNodes(ctx)
	if err != nil {
		logs.Warnf("controlplane: reconcile nodes: %v", err)
		return
	}

	stale := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if r.now().Sub(node.LastSeen) <= r.timeout {
			// 节点恢复心跳：清除标记，下次超时需要重新发布。
			delete(r.lastOffline, node.ID)
			continue
		}
		stale[node.ID] = true
		if r.lastOffline[node.ID] {
			continue
		}

		ev := event.Event{NodeID: node.ID, Type: nodeOfflineEvent, Time: r.now()}
		if err := r.publisher.PublishEvent(ctx, ev); err != nil {
			// 不记录标记，下一轮重试。
			logs.Warnf("controlplane: publish offline event for node %s: %v", node.ID, err)
			continue
		}
		r.lastOffline[node.ID] = true
	}

	// 节点从存储消失（被删除或改名重建）后不再保留标记。
	for id := range r.lastOffline {
		if !stale[id] {
			delete(r.lastOffline, id)
		}
	}
}
