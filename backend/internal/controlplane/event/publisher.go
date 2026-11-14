package event

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/insmtx/lework/backend/pkg/messaging"
)

// PTY 事件类型。终端输出与退出信号是易失数据：它们走 Core NATS，
// 不进入 JetStream，避免高频终端内容落盘。
const (
	EventTypePTYOutput = "pty.output"
	EventTypePTYExit   = "pty.exit"
)

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
// 状态事件需要跨实例重放，因此进入 stream；PTY 事件只对当前连着浏览器的
// 实例有意义，走 Core NATS 直接丢弃未订阅的流量。
type Publisher struct {
	bus  ClusterBus
	core CorePublisher
}

// NewPublisher 组装事件发布器。bus 与 core 分别对应持久化与易失两条链路。
func NewPublisher(bus ClusterBus, core CorePublisher) *Publisher {
	return &Publisher{bus: bus, core: core}
}

// PublishEvent 发布单条节点事件。
//
// pty.output / pty.exit 从 Data 中解析 pty_id 后发到
// cp.pty.<node_id>.<pty_id>；其余类型整体交给 JetStream。pty 事件缺少
// pty_id 时直接返回错误，绝不猜测 subject。
func (p *Publisher) PublishEvent(ctx context.Context, ev Event) error {
	switch ev.Type {
	case EventTypePTYOutput, EventTypePTYExit:
		return p.publishPTY(ctx, ev)
	default:
		subject, err := messaging.ControlPlaneEventSubject(ev.NodeID)
		if err != nil {
			return fmt.Errorf("publish event %s: %w", ev.Type, err)
		}
		if err := p.bus.Publish(ctx, subject, ev); err != nil {
			return fmt.Errorf("publish event %s to %s: %w", ev.Type, subject, err)
		}
		return nil
	}
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
