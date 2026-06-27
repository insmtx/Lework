// nats 提供基于 NATS JetStream 的事件总线实现
//
// 该部分实现了 mq 包中的 Publisher 和 Subscriber 接口，
// 使用 NATS JetStream 作为消息中间件来实现事件的发布和订阅。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// natsBus 表示一个 NATS 客户端，实现 Publisher 和 Subscriber 接口
type natsBus struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	closed bool
	mu     sync.Mutex
}

// NewNATS 创建一个新的 NATS JetStream 客户端实例
// 在初始化阶段创建所有预配置的 Streams
func NewNATS(url string) (*natsBus, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		logs.Errorf("Failed to connect to NATS: %v", err)
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		logs.Errorf("Failed to create JetStream context: %v", err)
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	bus := &natsBus{
		conn:   conn,
		js:     js,
		closed: false,
	}

	if err := bus.initStreams(); err != nil {
		conn.Close()
		logs.Errorf("Failed to initialize NATS streams: %v", err)
		return nil, fmt.Errorf("failed to initialize NATS streams: %w", err)
	}

	logs.Infof("Successfully connected to NATS at %s with JetStream", url)
	return bus, nil
}

// initStreams 在初始化阶段创建或更新所有预配置的 Stream
func (p *natsBus) initStreams() error {
	// 先清理可能冲突的旧 streams
	existingStreamsCh := p.js.StreamNames()
	var existingStreams []string
	for name := range existingStreamsCh {
		existingStreams = append(existingStreams, name)
	}
	for _, name := range existingStreams {
		info, err := p.js.StreamInfo(name)
		if err != nil {
			continue
		}
		// 检查是否与预配置的 stream 冲突（同 subjects 但不同名）
		isConfigured := false
		for _, cfg := range messaging.StreamConfigs() {
			if name == cfg.Name {
				isConfigured = true
				break
			}
			// 判断 subjects 是否重叠
			if hasOverlap(info.Config.Subjects, cfg.Subjects) {
				logs.Warnf("Deleting conflicting stream '%s' (subjects: %v)", name, info.Config.Subjects)
				if err := p.js.DeleteStream(name); err != nil {
					logs.Warnf("Failed to delete conflicting stream '%s': %v", name, err)
				}
			}
		}
		// 如果 stream 已存在但不是我们配置的，检查其 subjects
		if !isConfigured {
			for _, subj := range info.Config.Subjects {
				for _, cfg := range messaging.StreamConfigs() {
					if hasOverlap([]string{subj}, cfg.Subjects) {
						logs.Warnf("Deleting conflicting stream '%s' with subject %q", name, subj)
						_ = p.js.DeleteStream(name)
						break
					}
				}
			}
		}
	}

	for _, cfg := range messaging.StreamConfigs() {
		_, addErr := p.js.AddStream(&cfg)
		if addErr == nil {
			logs.Infof("Created JetStream stream '%s' with subjects: %v", cfg.Name, cfg.Subjects)
			continue
		}

		// AddStream 失败，尝试 UpdateStream
		if _, err := p.js.UpdateStream(&cfg); err != nil {
			return fmt.Errorf("failed to initialize stream '%s': AddStream=%v, UpdateStream=%w", cfg.Name, addErr, err)
		}
		logs.Infof("Updated JetStream stream '%s' with subjects: %v", cfg.Name, cfg.Subjects)
	}
	return nil
}

// hasOverlap 检查两组 subjects 是否有重叠
func hasOverlap(a, b []string) bool {
	for _, s1 := range a {
		for _, s2 := range b {
			if subjectsOverlap(s1, s2) {
				return true
			}
		}
	}
	return false
}

// subjectsOverlap 检查两个 NATS subject 模式是否重叠
func subjectsOverlap(s1, s2 string) bool {
	if s1 == s2 {
		return true
	}
	p1 := strings.Split(s1, ".")
	p2 := strings.Split(s2, ".")

	// 不同长度的固定部分不重叠
	if len(p1) != len(p2) {
		// 通配符情况: org.*.worker.*.task 与 org.1.worker.2.task 重叠
		return partialMatch(p1, p2)
	}

	for i := range p1 {
		if p1[i] == "*" || p2[i] == "*" {
			continue
		}
		if p1[i] == ">" || p2[i] == ">" {
			return true
		}
		if p1[i] != p2[i] {
			return false
		}
	}
	return true
}

// partialMatch 检查不同长度的 subject 是否可能重叠（通配符场景）
func partialMatch(p1, p2 []string) bool {
	// 处理 > 通配符
	for i, s := range p1 {
		if s == ">" {
			return true
		}
		if i < len(p2) && p1[i] != "*" && p2[i] != p1[i] {
			return false
		}
	}
	for i, s := range p2 {
		if s == ">" {
			return true
		}
		if i < len(p1) && p2[i] != "*" && p1[i] != p2[i] {
			return false
		}
	}
	// 短的是长的前缀且短的以 * 结尾可能重叠
	return false
}

// publishWithContext 在给定上下文环境中发布消息到指定主题
func (p *natsBus) publishWithContext(ctx context.Context, topic string, message any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return fmt.Errorf("NATS client is closed")
	}

	// 将消息序列化为 JSON
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// 发布消息
	_, err = p.js.Publish(topic, body, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to publish message to topic '%s': %w", topic, err)
	}

	return nil
}

// subscribeWithContext 在给定上下文环境中订阅特定主题的消息。
// 该函数会阻塞直到 context 被取消或订阅返回错误。
func (p *natsBus) subscribeWithContext(ctx context.Context, topic string, handler func(msg *nats.Msg)) error {
	// 使用 OrderedConsumer 不依赖 durable consumer，避免重启时 "consumer already bound" 错误。
	// OrderedConsumer 使用 AckNone 策略，无需手动 ack。
	sub, err := p.js.Subscribe(topic, func(msg *nats.Msg) {
		handler(msg)
	}, nats.OrderedConsumer(), nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic '%s': %w", topic, err)
	}

	// 阻塞直到 context 取消。
	<-ctx.Done()

	if err := sub.Unsubscribe(); err != nil {
		logs.WarnContextf(ctx, "Failed to unsubscribe from topic '%s': %v", topic, err)
	}
	logs.InfoContextf(ctx, "Unsubscribed from topic: %s", topic)

	return ctx.Err()
}

// Publish implements the eventbus.Publisher interface
func (p *natsBus) Publish(ctx context.Context, topic string, event any) error {
	return p.publishWithContext(ctx, topic, event)
}

// Request implements the eventbus.Publisher interface for request-reply.
// Publishes the event through JetStream with a Reply inbox set, subscribes to the
// inbox, and waits for a single reply message.
//
// JetStream does not preserve the NATS Reply header on delivered messages, so the
// inbox is also injected into the JSON body at body.reply_to. Worker handlers must
// read this field to know where to send their response.
func (p *natsBus) Request(ctx context.Context, topic string, event any) (*nats.Msg, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("NATS client is closed")
	}
	p.mu.Unlock()

	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request event: %w", err)
	}

	inbox := nats.NewInbox()
	sub, err := p.conn.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to inbox %s: %w", inbox, err)
	}
	defer sub.Unsubscribe()

	// JetStream strips the Reply header, so inject the inbox into the JSON body
	// so the worker knows where to send the response.
	body = injectReplyTo(body, inbox)

	// Publish through JetStream
	_, err = p.js.PublishMsg(&nats.Msg{
		Subject: topic,
		Data:    body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to publish request to topic %s: %w", topic, err)
	}

	// Wait for a single reply
	reply, err := sub.NextMsgWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to receive reply on inbox %s: %w", inbox, err)
	}
	return reply, nil
}

// injectReplyTo sets the reply_to field inside the "body" object of a JSON-encoded
// Envelope. If the JSON structure is unexpected, the original data is returned unchanged.
func injectReplyTo(data []byte, inbox string) []byte {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return data
	}
	bodyObj, ok := raw["body"].(map[string]any)
	if !ok {
		return data
	}
	bodyObj["reply_to"] = inbox
	result, err := json.Marshal(raw)
	if err != nil {
		return data
	}
	return result
}

// Conn returns the underlying NATS connection for advanced use cases such as
// publishing reply messages from a worker consumer.
func (p *natsBus) Conn() *nats.Conn {
	return p.conn
}

// Subscribe implements the eventbus.Subscriber interface.
// consumer 为空时使用临时消费者（OrderedConsumer, AckNone），非空时创建持久化消费者并自动 ACK/NAK。
func (p *natsBus) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	if consumer == "" {
		return p.subscribeWithContext(ctx, topic, handler)
	}
	return p.subscribeWithDurable(ctx, topic, consumer, handler)
}

// subscribeWithDurable 使用持久化消费者订阅，handler 正常返回后自动 Ack，panic 时自动 Nak（不传播 panic）。
func (p *natsBus) subscribeWithDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	sub, err := p.js.Subscribe(topic, func(msg *nats.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logs.ErrorContextf(ctx, "Panic in handler for topic '%s', consumer '%s': %v", topic, consumer, r)
				_ = msg.Nak()
			} else {
				_ = msg.Ack()
			}
		}()
		handler(msg)
	}, nats.Durable(consumer), nats.ManualAck(), nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic '%s' with consumer '%s': %w", topic, consumer, err)
	}

	<-ctx.Done()

	if err := sub.Unsubscribe(); err != nil {
		logs.WarnContextf(ctx, "Failed to unsubscribe from topic '%s': %v", topic, err)
	}
	logs.InfoContextf(ctx, "Unsubscribed from topic: %s (consumer: %s)", topic, consumer)

	return ctx.Err()
}

// SubscribeFrom implements the eventbus.Subscriber interface.
// startSeq == 0 时使用 DeliverNew 仅投递新消息；
// startSeq > 0 时使用 DeliverByStartSequence 从指定序列号开始投递。
func (p *natsBus) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	if startSeq <= 0 {
		return p.subscribeNewOnly(ctx, topic, handler)
	}
	streamName := messaging.StreamNameFromSubject(topic)
	if streamName != "" {
		info, err := p.js.StreamInfo(streamName)
		if err != nil {
			logs.WarnContextf(ctx, "Failed to inspect stream %s before subscribing to %s from seq %d: %v", streamName, topic, startSeq, err)
		} else if uint64(startSeq) > info.State.LastSeq+1 {
			logs.WarnContextf(ctx,
				"Local recovery seq %d for topic %s is beyond stream %s last seq %d; subscribing to new messages only",
				startSeq,
				topic,
				streamName,
				info.State.LastSeq,
			)
			return p.subscribeNewOnly(ctx, topic, handler)
		}
	}
	sub, err := p.js.Subscribe(topic, handler,
		nats.StartSequence(uint64(startSeq)),
		nats.Context(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe from seq %d on topic '%s': %w", startSeq, topic, err)
	}

	<-ctx.Done()

	if err := sub.Unsubscribe(); err != nil {
		logs.WarnContextf(ctx, "Failed to unsubscribe from topic '%s': %v", topic, err)
	}
	logs.InfoContextf(ctx, "Unsubscribed from topic: %s (from seq %d)", topic, startSeq)

	return ctx.Err()
}

// subscribeNewOnly 使用 JetStream DeliverNew 策略订阅，仅接收订阅之后的新消息。
func (p *natsBus) subscribeNewOnly(ctx context.Context, topic string, handler func(msg *nats.Msg)) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("NATS client is closed")
	}
	p.mu.Unlock()

	sub, err := p.js.Subscribe(topic, handler, nats.DeliverNew(), nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic '%s' (new only): %w", topic, err)
	}

	<-ctx.Done()

	if err := sub.Unsubscribe(); err != nil {
		logs.WarnContextf(ctx, "Failed to unsubscribe from topic '%s': %v", topic, err)
	}
	logs.InfoContextf(ctx, "Unsubscribed from topic: %s", topic)

	return ctx.Err()
}

// Close 关闭 NATS 连接并释放资源
func (p *natsBus) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	p.conn.Close()

	return nil
}
