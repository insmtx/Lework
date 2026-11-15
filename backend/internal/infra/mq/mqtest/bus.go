// Package mqtest 提供控制面测试用的内存事件总线。
//
// 真实部署用 NATS 承载节点事件与跨实例请求；进程内测试不需要外部服务，
// 但仍希望走与生产一致的接口（发布 → 字节订阅 → Hub 桥接），因此这里
// 用互斥锁和订阅者切片实现 mq 包的相关接口。
//
// 语义刻意贴近生产：
//   - Publish 先广播给所有匹配的 SubscribeBytes 订阅者，再向每个 durable
//     消费组投递一次（pull consumer 的集群内消费组语义）；
//   - SubscribeBytes / SubscribePull / SubscribeCore 注册后阻塞到 ctx 取消；
//   - RequestBytes 找不到应答者时返回 mq.ErrNoResponders。
//
// 本包只应被测试引用，不参与生产装配。
package mqtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/lework/backend/internal/infra/mq"
)

// errNotImplemented 标记控制面当前未使用的旧接口，避免误用时静默丢消息。
var errNotImplemented = errors.New("mqtest: not implemented")

// byteSub 是一条 SubscribeBytes 订阅。
type byteSub struct {
	subject string
	handler func(context.Context, []byte) error
}

// pullSub 是一条 SubscribePull 订阅；stream 与 durable 共同决定消费组。
type pullSub struct {
	stream  string
	subject string
	durable string
	handler func(context.Context, []byte) error
}

// coreSub 是一条 SubscribeCore 订阅，queue 用于模拟队列组分摊。
type coreSub struct {
	subject string
	queue   string
	handler func(context.Context, []byte) []byte
}

// Bus 是并发安全的内存总线。
//
// 零值不可用，必须用 New 创建。所有 handler 都在锁外调用：handler 内部
// 再次发布或订阅都不会死锁。
type Bus struct {
	mu sync.Mutex

	byteSubs []*byteSub
	pullSubs []*pullSub
	coreSubs []*coreSub

	// pullCursor 记录每个 durable 消费组的下一次投递位置，实现简单的
	// 轮转负载均衡。
	pullCursor map[string]int
}

// 编译期断言：内存总线满足控制面用到的全部接口面。
var (
	_ mq.EventBus           = (*Bus)(nil)
	_ mq.EventSubscriber    = (*Bus)(nil)
	_ mq.CorePublisher      = (*Bus)(nil)
	_ mq.CoreSubscriber     = (*Bus)(nil)
	_ mq.CoreRequesterBytes = (*Bus)(nil)
	_ mq.PullSubscriber     = (*Bus)(nil)
)

// New 创建空的内存总线。
func New() *Bus {
	return &Bus{pullCursor: make(map[string]int)}
}

// Publish 序列化事件并投递给匹配的字节订阅者与 pull 消费组。
//
// 与真实总线一致：同一 durable 消费组只收到一条，多个消费组各收到一条。
// handler 的错误会中断后续投递并返回给调用方，便于测试定位问题。
func (b *Bus) Publish(ctx context.Context, subject string, event any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("mqtest: marshal %s: %w", subject, err)
	}

	bytesHandlers, pullHandlers := b.selectHandlers(subject)

	for _, handler := range bytesHandlers {
		if err := handler(ctx, data); err != nil {
			return fmt.Errorf("mqtest: publish %s: %w", subject, err)
		}
	}
	for _, handler := range pullHandlers {
		if err := handler(ctx, data); err != nil {
			return fmt.Errorf("mqtest: publish %s: %w", subject, err)
		}
	}
	return nil
}

// byteHandler 是字节订阅 handler 的简写，便于 selectHandlers 返回列表。
type byteHandler func(context.Context, []byte) error

// selectHandlers 在锁内挑选本次投递目标，返回可直接在锁外调用的 handler 列表。
func (b *Bus) selectHandlers(subject string) ([]byteHandler, []byteHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bytesHandlers := make([]byteHandler, 0, len(b.byteSubs))
	for _, sub := range b.byteSubs {
		if matchSubject(sub.subject, subject) {
			bytesHandlers = append(bytesHandlers, sub.handler)
		}
	}

	// 按 (stream, durable) 分组，每组只挑一个订阅者。
	groups := make(map[string][]*pullSub)
	for _, sub := range b.pullSubs {
		if !matchSubject(sub.subject, subject) {
			continue
		}
		key := sub.stream + "\x00" + sub.durable
		groups[key] = append(groups[key], sub)
	}

	pullHandlers := make([]byteHandler, 0, len(groups))
	if b.pullCursor == nil {
		// 容忍零值构造：New 是推荐入口，但多一层保护不会掩盖问题。
		b.pullCursor = make(map[string]int)
	}
	for key, subs := range groups {
		cursor := b.pullCursor[key] % len(subs)
		b.pullCursor[key] = cursor + 1
		pullHandlers = append(pullHandlers, subs[cursor].handler)
	}
	return bytesHandlers, pullHandlers
}

// Request 是未使用的 JetStream 请求接口，控制面改走 Core NATS request/reply。
func (b *Bus) Request(context.Context, string, any) (*nats.Msg, error) {
	return nil, errNotImplemented
}

// Subscribe 是未使用的 push consumer 接口。
func (b *Bus) Subscribe(context.Context, string, string, func(*nats.Msg)) error {
	return errNotImplemented
}

// SubscribeFrom 是未使用的 push consumer 接口。
func (b *Bus) SubscribeFrom(context.Context, string, int64, func(*nats.Msg)) error {
	return errNotImplemented
}

// SubscribeManualDurable 是未使用的 push consumer 接口。
func (b *Bus) SubscribeManualDurable(context.Context, string, string, func(*nats.Msg)) error {
	return errNotImplemented
}

// PublishCore 是未使用的 Core NATS 发布接口。
//
// 控制面目前只有 PTY 输出走 Core NATS，端到端测试不产生终端帧；需要时再
// 补齐投递逻辑，返回错误比静默丢弃更容易发现问题。
func (b *Bus) PublishCore(context.Context, string, []byte) error {
	return errNotImplemented
}

// SubscribeCore 注册应答者并阻塞到 ctx 取消，语义与真实实现一致。
func (b *Bus) SubscribeCore(
	ctx context.Context,
	subject, queue string,
	handler func(context.Context, []byte) []byte,
) error {
	if handler == nil {
		return errors.New("mqtest: subscribe core handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sub := &coreSub{subject: subject, queue: queue, handler: handler}
	b.mu.Lock()
	b.coreSubs = append(b.coreSubs, sub)
	b.mu.Unlock()

	<-ctx.Done()

	b.mu.Lock()
	b.removeCoreSubLocked(sub)
	b.mu.Unlock()
	return ctx.Err()
}

// RequestBytes 选一个匹配的应答者同步调用，返回其非 nil 回复。
//
// 队列语义：同一 queue 组的多个应答者只挑一个；未设置 queue 的订阅者各自
// 独立参与挑选。没有匹配者时返回 mq.ErrNoResponders。
func (b *Bus) RequestBytes(ctx context.Context, subject string, request []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	b.mu.Lock()
	var candidates []*coreSub
	seenQueue := make(map[string]bool)
	for _, sub := range b.coreSubs {
		if !matchSubject(sub.subject, subject) {
			continue
		}
		// 空 queue 不是队列组，每个应答者都是独立候选。
		if sub.queue == "" {
			candidates = append(candidates, sub)
			continue
		}
		if seenQueue[sub.queue] {
			continue
		}
		seenQueue[sub.queue] = true
		candidates = append(candidates, sub)
	}
	b.mu.Unlock()

	if len(candidates) == 0 {
		return nil, fmt.Errorf("mqtest: request %s: %w", subject, mq.ErrNoResponders)
	}
	for _, sub := range candidates {
		if reply := sub.handler(ctx, request); reply != nil {
			return reply, nil
		}
	}
	return nil, fmt.Errorf("mqtest: request %s: %w", subject, mq.ErrNoResponders)
}

// SubscribePull 注册 pull 消费组成员并阻塞到 ctx 取消。
func (b *Bus) SubscribePull(
	ctx context.Context,
	stream, subject, durable string,
	handler func(context.Context, []byte) error,
) error {
	if handler == nil {
		return errors.New("mqtest: subscribe pull handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sub := &pullSub{stream: stream, subject: subject, durable: durable, handler: handler}
	b.mu.Lock()
	b.pullSubs = append(b.pullSubs, sub)
	b.mu.Unlock()

	<-ctx.Done()

	b.mu.Lock()
	b.removePullSubLocked(sub)
	b.mu.Unlock()
	return ctx.Err()
}

// SubscribeBytes 注册字节订阅者并阻塞到 ctx 取消。
//
// startSeq 被忽略：内存总线不保存历史，等价于真实实现的 DeliverNew。
func (b *Bus) SubscribeBytes(
	ctx context.Context,
	subject string,
	_ int64,
	handler func(context.Context, []byte) error,
) error {
	if handler == nil {
		return errors.New("mqtest: subscribe bytes handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sub := &byteSub{subject: subject, handler: handler}
	b.mu.Lock()
	b.byteSubs = append(b.byteSubs, sub)
	b.mu.Unlock()

	<-ctx.Done()

	b.mu.Lock()
	b.removeByteSubLocked(sub)
	b.mu.Unlock()
	return ctx.Err()
}

func (b *Bus) removeByteSubLocked(target *byteSub) {
	for i, sub := range b.byteSubs {
		if sub == target {
			b.byteSubs = append(b.byteSubs[:i], b.byteSubs[i+1:]...)
			return
		}
	}
}

func (b *Bus) removePullSubLocked(target *pullSub) {
	for i, sub := range b.pullSubs {
		if sub == target {
			b.pullSubs = append(b.pullSubs[:i], b.pullSubs[i+1:]...)
			return
		}
	}
}

func (b *Bus) removeCoreSubLocked(target *coreSub) {
	for i, sub := range b.coreSubs {
		if sub == target {
			b.coreSubs = append(b.coreSubs[:i], b.coreSubs[i+1:]...)
			return
		}
	}
}

// matchSubject 判断 subject 是否匹配 pattern。
//
// 只支持两种形式，够控制面使用：精确分段匹配，以及尾部 > 通配（匹配一个
// 或多个后续分段，因此 "a.>" 不匹配 "a"）。
func matchSubject(pattern, subject string) bool {
	patternParts := strings.Split(pattern, ".")
	subjectParts := strings.Split(subject, ".")

	fixed := len(patternParts)
	if patternParts[len(patternParts)-1] == ">" {
		// 尾部通配只约束 ">" 之前的前缀；"a.>" 不匹配 "a"。
		fixed--
		if len(subjectParts) <= fixed {
			return false
		}
	} else if len(subjectParts) != fixed {
		return false
	}

	for i := 0; i < fixed; i++ {
		if patternParts[i] != subjectParts[i] {
			return false
		}
	}
	return true
}
