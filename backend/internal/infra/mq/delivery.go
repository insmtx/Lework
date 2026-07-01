// Package mq 提供消息队列的抽象层，包含 ManualDelivery 接口，
// 用于在不依赖真实 NATS 连接的情况下测试消息确认行为。
package mq

import (
	"time"
)

// ManualDelivery 抽象了一条需要手动确认的 NATS JetStream 消息。
// 由 cmd.run lane handler 使用，以精确控制 Ack、Nak、NakWithDelay 和 Term 的调用时机。
//
// 生产实现包装 *nats.Msg（见 nats_delivery.go）。
// 测试代码可通过 fake 实现来验证正确的确认序列（见 handler_test.go）。
type ManualDelivery interface {
	// Metadata 返回该消息的 JetStream 元数据。
	// 包含 stream 序列号、consumer 序列号和投递次数。
	Metadata() (*Metadata, error)

	// Ack 确认消息，通知 NATS 已成功处理，从 consumer 中移除。
	Ack() error

	// Nak 否定确认消息，请求 NATS 重新投递。
	Nak() error

	// NakWithDelay 否定确认消息，并指定一个重试延迟，
	// NATS 将在延迟过后重新投递该消息。
	NakWithDelay(delay time.Duration) error

	// Term 终止消息，阻止进一步的重新投递。
	// 用于永久性（不可重试）错误。
	Term() error

	// InProgress 延长消息的确认超时时间，防止 handler 仍在处理时
	// NATS 认为消息已超时而重新投递。
	InProgress() error
}

// Metadata 包含从 NATS 消息中提取的 JetStream 元数据。
type Metadata struct {
	// Stream 是该消息在 stream 中的序列号。
	Stream uint64
	// Consumer 是该消息在 consumer 中的序列号。
	Consumer uint64
	// NumDelivered 是该消息已被投递的次数。
	NumDelivered uint64
}
