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
}

// EventBus 组合了发布和订阅能力，提供完整的事件总线功能
type EventBus interface {
	Publisher
	Subscriber
}
