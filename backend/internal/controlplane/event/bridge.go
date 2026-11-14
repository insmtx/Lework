package event

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/pkg/messaging"
)

// bridgeRetryDelay 是订阅失败后的重试间隔。抖动或 NATS 短暂不可用时不能
// 让桥接永久停止，否则本实例会再也收不到集群事件。
const bridgeRetryDelay = time.Second

// ClusterSubscriber 是订阅 JetStream 节点事件的字节订阅面。
//
// 只声明 SubscribeBytes，测试可用假实现直接投递负载，不需要真实 NATS。
type ClusterSubscriber interface {
	SubscribeBytes(
		ctx context.Context,
		subject string,
		startSeq int64,
		handler func(context.Context, []byte) error,
	) error
}

// Bridge 把集群中其他实例发布的节点事件接回本地 Hub。
//
// 浏览器连接只订阅本地 Hub；集群事件由本桥接补齐，因此无论事件在哪个
// 实例产生，都能扇出给当前实例上的客户端。
type Bridge struct {
	sub ClusterSubscriber
	hub *Hub

	// retryDelay 允许单测缩短重试等待，生产使用 bridgeRetryDelay。
	retryDelay time.Duration
}

// NewBridge 组装桥接。startSeq 固定为 0（DeliverNew）：历史事件由客户端
// 通过 OpenCode 会话状态自行恢复，桥接不重放旧事件。
func NewBridge(sub ClusterSubscriber, hub *Hub) *Bridge {
	return &Bridge{sub: sub, hub: hub, retryDelay: bridgeRetryDelay}
}

// Run 订阅所有节点事件并转发到本地 Hub，直到 ctx 取消。
//
// 订阅可能因为 NATS 抖动或 stream 重建而失败；失败后按固定间隔重试，
// 不能一次失败就永久停止接收事件。SubscribeBytes 提前返回 nil 也视为
// 需要重新订阅，避免在循环里空转。
func (b *Bridge) Run(ctx context.Context) {
	subject := messaging.ControlPlaneEventWildcard()

	for {
		if ctx.Err() != nil {
			return
		}
		err := b.sub.SubscribeBytes(ctx, subject, 0, b.handle)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logs.Warnf("event bridge: subscribe %s: %v; retrying", subject, err)
		} else {
			logs.Warnf("event bridge: subscribe %s returned without error; retrying", subject)
		}

		delay := b.retryDelay
		if delay <= 0 {
			delay = bridgeRetryDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// handle 解码一条集群事件并投递给本地 Hub。
//
// 解码失败只记日志并返回 nil：一条坏消息不能让整条订阅退出，否则该实例
// 会永久停止接收后续事件。日志不打印负载，节点事件可能包含终端内容或
// 用户输入。
func (b *Bridge) handle(_ context.Context, data []byte) error {
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		logs.Warnf("event bridge: skip malformed event: %v", err)
		return nil
	}
	b.hub.Publish(ev)
	return nil
}
