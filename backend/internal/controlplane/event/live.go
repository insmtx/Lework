package event

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/pkg/messaging"
)

// CoreSubscriber 是订阅 Core NATS 字节流的订阅面。
//
// 只声明 SubscribeCore，测试可用假实现直接投递负载，不需要真实 NATS；
// 与 bridge.go 一样，这里不 import mq。
type CoreSubscriber interface {
	SubscribeCore(
		ctx context.Context,
		subject, queue string,
		handler func(context.Context, []byte) []byte,
	) error
}

// LiveSubscriber 把集群中任意实例发布的增量事件接回本地 Hub。
//
// 增量事件只走 Core NATS，不经过 JetStream；每个实例各订阅一份
// cp.live.>，因此无论事件在哪个实例产生，连到任意实例的浏览器都能收到。
type LiveSubscriber struct {
	hub *Hub

	// retryDelay 允许单测缩短重试等待，生产使用 bridgeRetryDelay。
	retryDelay time.Duration
}

// NewLiveSubscriber 组装增量事件订阅器。
func NewLiveSubscriber(hub *Hub) *LiveSubscriber {
	return &LiveSubscriber{hub: hub, retryDelay: bridgeRetryDelay}
}

// Run 订阅 cp.live.> 并转发到本地 Hub，直到 ctx 取消。
//
// 订阅可能因 NATS 抖动或重连失败；与状态事件桥接一致，失败后按固定间隔
// 重试，不能一次失败就永久停止接收增量事件。ctx 取消属于正常退出，返回
// nil，由调用方决定是否记录日志。
func (l *LiveSubscriber) Run(ctx context.Context, sub CoreSubscriber) error {
	if sub == nil {
		return errors.New("event: live subscriber is required")
	}
	if l == nil || l.hub == nil {
		return errors.New("event: live subscriber hub is required")
	}

	subject := messaging.ControlPlaneLiveWildcard()

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := sub.SubscribeCore(ctx, subject, "", l.handle)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			logs.Warnf("event live: subscribe %s: %v; retrying", subject, err)
		} else {
			logs.Warnf("event live: subscribe %s returned without error; retrying", subject)
		}

		delay := l.retryDelay
		if delay <= 0 {
			delay = bridgeRetryDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// handle 解码一条增量事件并投递给本地 Hub。
//
// subject 已经保证它来自 cp.live.>，因此即使发布端漏了 live 标记也补上
// Live=true。解码失败只记日志并返回 nil：一条坏消息不能让整条订阅退出。
// 日志不打印负载，token 增量可能包含用户输入。
func (l *LiveSubscriber) handle(_ context.Context, data []byte) []byte {
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		logs.Warnf("event live: skip malformed event: %v", err)
		return nil
	}
	ev.Live = true
	l.hub.Publish(ev)
	return nil
}
