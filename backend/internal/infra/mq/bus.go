// mq 包提供消息队列的抽象和实现
//
// 该包定义了事件发布者和订阅者的标准接口，以及基于 RabbitMQ 的实现。
// 支持多种事件总线实现。
package mq

import (
	"context"

	"github.com/nats-io/nats.go"
)

// Publisher 是事件发布者接口，定义了向指定主题发布事件的方法
type Publisher interface {
	// Publish 向指定主题发布事件
	Publish(ctx context.Context, topic string, event any) error
	// Request 向指定主题发布请求并等待单个回复。
	// 使用 NATS inbox 模式：发布方订阅唯一 inbox，发布事件时将 Reply 设为该 inbox，然后等待回复。
	// 返回原始的 NATS 消息，其中包含回复数据。
	Request(ctx context.Context, topic string, event any) (*nats.Msg, error)
}

// Subscriber 是事件订阅者接口，定义了订阅指定主题事件的方法。
type Subscriber interface {
	// Subscribe 订阅指定主题的事件，并使用提供的处理函数处理收到的事件。
	// consumer 指定消费组名称，用于区分不同的消费者组。
	// 当 consumer 为空字符串时，使用临时消费者（无持久化、自动确认）。
	// 当 consumer 非空时，使用持久化消费者，handler 返回后自动 ACK，panic 时自动 NAK。
	Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error
	// SubscribeFrom 订阅指定主题的事件，并使用提供的处理函数处理收到的事件。
	// startSeq 指定起始序列号，小于等于 startSeq 的消息不会被投递。
	// startSeq 为 0 时仅投递订阅之后的新消息。
	SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error
	// SubscribeManualDurable 使用持久化消费者订阅，由 handler 手动控制消息确认。
	// handler 接收到原始 *nats.Msg，自行负责调用 Ack、Nak、NakWithDelay 或 Term。
	// handler 中发生 panic 时自动触发 Nak。
	SubscribeManualDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error
}

// EventBus 组合了发布和订阅能力，提供完整的事件总线功能
type EventBus interface {
	Publisher
	Subscriber
}

// ErrNoResponders 表示 Core NATS request 的 subject 当前没有任何订阅者。
//
// 控制面需要在「节点未注册」时降级处理，但不希望为此直接依赖 nats 包，
// 因此在这里暴露一个可被 errors.Is 识别的哨兵错误。
var ErrNoResponders = nats.ErrNoResponders

// CorePublisher 发送不落盘的实时字节数据（如 pty.output）。
//
// 与 Publisher.Publish（走 JetStream）不同，Core NATS 不持久化：
// 没有订阅者时消息直接丢弃，适用于高频、可丢的实时流。
type CorePublisher interface {
	// PublishCore 向 subject 发布原始字节并等待写入连接缓冲。
	// 调用方应传入带超时的 ctx，以便区分刷新超时与连接错误。
	PublishCore(ctx context.Context, subject string, data []byte) error
}

// CoreSubscriber 订阅 Core NATS 的字节请求，并把 handler 的返回值作为回复。
//
// handler 返回 nil 表示该请求无需回复（不发送 respond）。
type CoreSubscriber interface {
	// SubscribeCore 订阅 subject；queue 非空时使用队列组以分摊请求。
	// 阻塞直到 ctx 取消，取消后退订并返回 ctx.Err()。
	SubscribeCore(ctx context.Context, subject, queue string, handler func(context.Context, []byte) []byte) error
}

// CoreRequesterBytes 是 CoreRequester 的字节版，避免调用方依赖 nats.Msg。
type CoreRequesterBytes interface {
	// RequestBytes 向 subject 发送 Core NATS request 并返回回复负载。
	// subject 没有订阅者时返回 ErrNoResponders；ctx 超时返回 ctx.Err()。
	RequestBytes(ctx context.Context, subject string, request []byte) ([]byte, error)
}

// PullSubscriber 用同名 durable pull consumer 实现集群内消费组。
//
// 多个实例以相同 stream/durable 调用时，同一条消息只会被其中一个实例处理。
type PullSubscriber interface {
	// SubscribePull 订阅 stream 中 subject 的消息；handler 返回 nil 后 Ack，
	// 返回错误则 Nak 等待重投。阻塞直到 ctx 取消。
	SubscribePull(ctx context.Context, stream, subject, durable string, handler func(context.Context, []byte) error) error
}

// EventSubscriber 以原始负载订阅 JetStream 事件，供不关心 nats.Msg 的调用方使用。
type EventSubscriber interface {
	// SubscribeBytes 订阅 subject 的原始负载；startSeq <= 0 时仅接收新消息，
	// startSeq > 0 时从该序列号开始投递。阻塞直到 ctx 取消。
	SubscribeBytes(ctx context.Context, subject string, startSeq int64, handler func(context.Context, []byte) error) error
}

// CoreRequester 提供基于 Core NATS 的同步 request/reply 能力。
//
// 与 Publisher.Request（走 JetStream）不同，CoreRequester 使用 Core NATS 的
// 临时 inbox 直接发送请求并等待单一回复，不进入 JetStream 任务队列、
// 不污染任务消息流。适用于低频率、需要同步结果的运维类查询（如 Worker 状态）。
type CoreRequester interface {
	// RequestReply 向 subject 发送 Core NATS request 并等待单一回复。
	// 返回原始 NATS 消息，其中包含回复数据；超时或 NATS 不可用时返回错误。
	// 调用方需保证传入的 ctx 携带超时，以区分超时与连接错误。
	RequestReply(ctx context.Context, subject string, request any) (*nats.Msg, error)
}
