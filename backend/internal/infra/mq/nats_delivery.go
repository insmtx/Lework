package mq

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// natsDelivery 将 *nats.Msg 适配为 ManualDelivery 接口。
// 是 ManualDelivery 的生产实现，所有方法直接委托给底层的 *nats.Msg。
type natsDelivery struct {
	msg *nats.Msg
}

// NewManualDelivery 将一条 NATS 消息包装为 ManualDelivery。
// 提供给 Dispatcher 的 handleRun 方法使用，使得 handler 可以通过
// ManualDelivery 接口控制消息的确认生命周期。
func NewManualDelivery(msg *nats.Msg) ManualDelivery {
	return &natsDelivery{msg: msg}
}

// Metadata 提取消息的 JetStream 元数据，返回应用层关注的字段。
func (d *natsDelivery) Metadata() (*Metadata, error) {
	meta, err := d.msg.Metadata()
	if err != nil {
		return nil, fmt.Errorf("nats metadata: %w", err)
	}
	return &Metadata{
		Stream:       meta.Sequence.Stream,
		Consumer:     meta.Sequence.Consumer,
		NumDelivered: meta.NumDelivered,
	}, nil
}

func (d *natsDelivery) Ack() error {
	return d.msg.Ack()
}

func (d *natsDelivery) Nak() error {
	return d.msg.Nak()
}

func (d *natsDelivery) NakWithDelay(delay time.Duration) error {
	return d.msg.NakWithDelay(delay)
}

func (d *natsDelivery) Term() error {
	return d.msg.Term()
}

func (d *natsDelivery) InProgress() error {
	return d.msg.InProgress()
}
