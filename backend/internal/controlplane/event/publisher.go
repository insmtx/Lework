package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/pkg/messaging"
)

// PTY 与增量事件类型。终端输出、token 级流式增量都是易失数据：它们走
// Core NATS，不进入 JetStream，避免高频内容落盘并在重连时被重放。
const (
	EventTypePTYOutput = "pty.output"
	EventTypePTYExit   = "pty.exit"

	EventTypeSessionTextDelta      = "session.text.delta"
	EventTypeSessionReasoningDelta = "session.reasoning.delta"
	EventTypeSessionToolInputDelta = "session.tool.input.delta"
	EventTypeSessionToolProgress   = "session.tool.progress"
)

// IsLiveEvent 判断事件是否为 token 级增量。
//
// 这些事件只对正在观看会话的客户端有意义：不值得持久化，也不能参与
// 乱序判定；丢失的片段由 text.ended 或下一次快照对齐。
func IsLiveEvent(eventType string) bool {
	switch eventType {
	case EventTypeSessionTextDelta,
		EventTypeSessionReasoningDelta,
		EventTypeSessionToolInputDelta,
		EventTypeSessionToolProgress:
		return true
	default:
		return false
	}
}

// EventTypeNodeSnapshot 是 owner 周期性发布的整节点投影快照。
//
// 定义在 event 包而不是 overview，是为了让事件类型成为总线协议的一部分；
// overview.SnapshotEvent 保留为别名，避免已有调用方改动。
const EventTypeNodeSnapshot = "node.snapshot"

// ClusterBus 是发布状态事件（进入 JetStream）的集群总线。
//
// 只声明 Publish，是为了让单测用轻量假实现替换真实 NATS，避免测试依赖
// 外部服务。
type ClusterBus interface {
	Publish(ctx context.Context, topic string, event any) error
}

// CorePublisher 是发布易失字节流（Core NATS）的发布面。
type CorePublisher interface {
	PublishCore(ctx context.Context, subject string, data []byte) error
}

// Publisher 把节点事件按类型路由到 JetStream 或 Core NATS。
//
// 状态事件需要跨实例重放，因此进入 stream；PTY 与 token 级增量只对当前
// 连着浏览器的实例有意义，走 Core NATS 直接丢弃未订阅的流量。
//
// 状态事件在发布前会被盖上 owner 纪元与该节点上的单调序号，接收方据此丢弃
// 乱序或来自旧 epoch 的事件与快照。
type Publisher struct {
	bus  ClusterBus
	core CorePublisher

	owner string

	mu  sync.Mutex
	seq map[string]int64
}

// NewPublisher 组装事件发布器。bus 与 core 分别对应持久化与易失两条链路。
//
// owner 每次进程启动随机生成：重启后的实例必须被视为新 epoch，否则旧实例
// 遗留的序号会让新事件被误判为过期。
func NewPublisher(bus ClusterBus, core CorePublisher) *Publisher {
	return &Publisher{
		bus:   bus,
		core:  core,
		owner: newOwnerID(),
		seq:   make(map[string]int64),
	}
}

// Owner 返回当前发布器的纪元标识。
func (p *Publisher) Owner() string {
	if p == nil {
		return ""
	}
	return p.owner
}

// PublishEvent 发布单条节点事件。
//
// pty.output / pty.exit 从 Data 中解析 pty_id 后发到
// cp.pty.<node_id>.<pty_id>；token 级增量发到 cp.live.node.<node_id>；
// 其余类型整体交给 JetStream。pty 事件缺少 pty_id 时直接返回错误，绝不
// 猜测 subject。
func (p *Publisher) PublishEvent(ctx context.Context, ev Event) error {
	switch ev.Type {
	case EventTypePTYOutput, EventTypePTYExit:
		return p.publishPTY(ctx, ev)
	default:
		if IsLiveEvent(ev.Type) {
			return p.publishLive(ctx, ev)
		}
		ev = p.stamp(ev)
		return p.publishState(ctx, ev)
	}
}

// PublishSnapshot 发布一条整节点快照，不改变 owner 序号。
//
// 快照携带生成它时的 index watermark（owner + seq），因此它既不会因为发布
// 动作把序号推高，也不会覆盖比它更新的实时事件。
func (p *Publisher) PublishSnapshot(ctx context.Context, ev Event) error {
	return p.publishState(ctx, ev)
}

// stamp 为状态事件分配 owner 与节点内单调序号。
func (p *Publisher) stamp(ev Event) Event {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seq == nil {
		p.seq = make(map[string]int64)
	}
	p.seq[ev.NodeID]++
	ev.Owner = p.owner
	ev.ClusterSeq = p.seq[ev.NodeID]
	return ev
}

// publishState 把状态事件（含快照）写入节点对应的 JetStream subject。
func (p *Publisher) publishState(ctx context.Context, ev Event) error {
	subject, err := messaging.ControlPlaneEventSubject(ev.NodeID)
	if err != nil {
		return fmt.Errorf("publish event %s: %w", ev.Type, err)
	}
	if err := p.bus.Publish(ctx, subject, ev); err != nil {
		return fmt.Errorf("publish event %s to %s: %w", ev.Type, subject, err)
	}
	return nil
}

// publishPTY 把 PTY 事件序列化后发到 Core NATS。
//
// 负载按 Event 原样序列化，订阅端（浏览器连接）因此拿到与 Hub 一致的
// 结构，不必为 PTY 维护第二套格式。
func (p *Publisher) publishPTY(ctx context.Context, ev Event) error {
	var payload struct {
		PTYID string `json:"pty_id"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		return fmt.Errorf("publish %s: parse pty_id: %w", ev.Type, err)
	}
	if payload.PTYID == "" {
		return fmt.Errorf("publish %s: pty_id is required", ev.Type)
	}

	subject, err := messaging.ControlPlanePTYSubject(ev.NodeID, payload.PTYID)
	if err != nil {
		return fmt.Errorf("publish %s: %w", ev.Type, err)
	}

	// 不打印 ev 本身：PTY 负载是终端内容，可能包含密钥或用户输入。
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("publish %s: marshal event: %w", ev.Type, err)
	}
	if err := p.core.PublishCore(ctx, subject, data); err != nil {
		return fmt.Errorf("publish %s to %s: %w", ev.Type, subject, err)
	}
	return nil
}

// publishLive 把增量事件序列化后发到 Core NATS。
//
// 不盖章 owner / ClusterSeq：live 事件不参与乱序判定，丢失的片段由
// text.ended 或下一次快照兜底。负载保留与 Hub 一致的 Event 结构，接收端
// 只需一个解码路径。
func (p *Publisher) publishLive(ctx context.Context, ev Event) error {
	subject, err := messaging.ControlPlaneLiveSubject(ev.NodeID)
	if err != nil {
		return fmt.Errorf("publish %s: %w", ev.Type, err)
	}

	ev.Live = true
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("publish %s: marshal event: %w", ev.Type, err)
	}
	if err := p.core.PublishCore(ctx, subject, data); err != nil {
		return fmt.Errorf("publish %s to %s: %w", ev.Type, subject, err)
	}
	return nil
}

// newOwnerID 生成进程级随机 owner 标识；它不参与安全认证，只是区分 epoch。
func newOwnerID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	// rand 失败极罕见；退化为时间标识，仍能区分重启前后的进程。
	return fmt.Sprintf("%016x", uint64(time.Now().UnixNano()))
}
