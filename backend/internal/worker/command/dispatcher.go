// Package command 提供统一的 Worker Command 分发器。
//
// Dispatcher 负责启动各 lane 订阅（cmd.run、cmd.control、cmd.interaction、cmd.skill），
// 并将收到的统一 WorkerCommand 分发到对应的 handler。
// 其中 cmd.run lane 使用手动确认订阅（SubscribeManualDurable），其余 lane 使用自动确认订阅（Subscribe）。
package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// Subscriber 是 Dispatcher 所需的最小订阅接口。
// 包含自动确认和手动确认两种订阅方式。
type Subscriber interface {
	Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error
	SubscribeManualDurable(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error
}

// RunHandler 处理 cmd.run lane 的 agent run 命令。
// delivery 提供手动 Ack 控制（Term/Nak/Ack/NakWithDelay/InProgress），
// handler 根据处理阶段决定确认方式：
//   - 永久错误 → Term（不再重试）
//   - 临时错误 → NakWithDelay（延迟重试）
//   - 成功持久化后 → Ack（异步执行）
type RunHandler interface {
	HandleRunCommand(ctx context.Context, cmd messaging.WorkerCommand, delivery eventbus.ManualDelivery) error
}

// ControlHandler 处理 cmd.control lane 的 cancel 命令。
type ControlHandler interface {
	HandleControlCommand(ctx context.Context, cmd messaging.WorkerCommand) error
}

// InteractionHandler 处理 cmd.interaction lane 的 approval/question 命令。
type InteractionHandler interface {
	HandleInteractionCommand(ctx context.Context, cmd messaging.WorkerCommand) error
}

// SkillHandler 处理 cmd.skill lane 的 skill 管理命令。
type SkillHandler interface {
	HandleSkillCommand(ctx context.Context, cmd messaging.WorkerCommand, msg *nats.Msg) error
}

// Handlers 显式包含四类 handler，构造时一次性校验。
type Handlers struct {
	Run         RunHandler
	Control     ControlHandler
	Interaction InteractionHandler
	Skill       SkillHandler
}

// Config 是 Dispatcher 的配置。
type Config struct {
	OrgID    uint
	WorkerID uint
}

// Dispatcher 是统一的 worker 命令分发器。
type Dispatcher struct {
	cfg      Config
	sub      Subscriber
	handlers Handlers
}

// New 创建新的 Dispatcher。
func New(cfg Config, sub Subscriber, handlers Handlers) (*Dispatcher, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if sub == nil {
		return nil, fmt.Errorf("subscriber is required")
	}
	if handlers.Run == nil {
		return nil, fmt.Errorf("run handler is required")
	}
	if handlers.Control == nil {
		return nil, fmt.Errorf("control handler is required")
	}
	if handlers.Interaction == nil {
		return nil, fmt.Errorf("interaction handler is required")
	}
	if handlers.Skill == nil {
		return nil, fmt.Errorf("skill handler is required")
	}

	return &Dispatcher{
		cfg:      cfg,
		sub:      sub,
		handlers: handlers,
	}, nil
}

// Run 并发启动四个 lane 订阅并阻塞，直到 ctx 取消或任一订阅异常退出。
//
// run lane 使用手动 Ack 订阅（SubscribeManualDurable），
// 因为 run handler 需要先将消息持久化到本地 inbox 再 Ack，
// 以实现 at-least-once 的崩溃恢复语义。
//
// 其余 lane（control、interaction、skill）使用自动 Ack 订阅
// （Subscribe），因为它们的 handler 同步完成处理，无需手动控制确认时机。
//
// 任一订阅异常退出时会取消其他 lane 的 context。
func (d *Dispatcher) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type laneCfg struct {
		lane     messaging.Lane
		consumer string
		handler  func(ctx context.Context, msg *nats.Msg)
		manual   bool // true = SubscribeManualDurable, false = Subscribe (auto-Ack)
	}

	lanes := []laneCfg{
		{messaging.LaneRun, messaging.WorkerRunConsumer(), d.handleRun, true},
		{messaging.LaneControl, messaging.WorkerControlConsumer(), d.handleControl, false},
		{messaging.LaneInteraction, messaging.WorkerInteractionConsumer(), d.handleInteraction, false},
		{messaging.LaneSkill, messaging.WorkerSkillConsumer(), d.handleSkill, false},
	}

	errCh := make(chan error, len(lanes))

	for _, l := range lanes {
		topic, err := messaging.WorkerCommandSubject(d.cfg.OrgID, d.cfg.WorkerID, l.lane)
		if err != nil {
			return fmt.Errorf("build subject for lane %s: %w", l.lane, err)
		}
		l := l
		go func() {
			logs.InfoContextf(ctx, "Command dispatcher starting lane %s on topic %s (manual=%v)", l.lane, topic, l.manual)
			var subErr error
			natsHandler := func(msg *nats.Msg) {
				l.handler(ctx, msg)
			}
			if l.manual {
				subErr = d.sub.SubscribeManualDurable(ctx, topic, l.consumer, natsHandler)
			} else {
				subErr = d.sub.Subscribe(ctx, topic, l.consumer, natsHandler)
			}
			if subErr != nil {
				errCh <- fmt.Errorf("lane %s (topic %s): %w", l.lane, topic, subErr)
			} else {
				errCh <- nil
			}
		}()
	}

	var firstErr error
	for range lanes {
		err := <-errCh
		if err != nil && firstErr == nil && !errors.Is(err, context.Canceled) {
			firstErr = err
			cancel()
		}
	}

	if firstErr != nil {
		return firstErr
	}
	return nil
}

func (d *Dispatcher) parseCommand(data []byte) (messaging.WorkerCommand, error) {
	var cmd messaging.WorkerCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return cmd, fmt.Errorf("unmarshal worker command: %w", err)
	}
	if cmd.Type != messaging.MessageTypeWorkerCommand {
		return cmd, fmt.Errorf("unexpected message type: %q", cmd.Type)
	}
	return cmd, nil
}

// handleRun 使用手动确认方式处理 run 命令。
// 将原始 *nats.Msg 包装为 ManualDelivery 后传递给 handler，
// 由 handler 根据处理阶段决定确认方式（Ack/Term/Nak/NakWithDelay/InProgress）。
// 消息解析失败时直接 Term，其余错误由 handler 内部处理。
func (d *Dispatcher) handleRun(ctx context.Context, msg *nats.Msg) {
	d.handleRunDelivery(ctx, msg.Data, eventbus.NewManualDelivery(msg))
}

func (d *Dispatcher) handleRunDelivery(ctx context.Context, data []byte, delivery eventbus.ManualDelivery) {
	cmd, err := d.parseCommand(data)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to parse run command: %v", err)
		_ = delivery.Term()
		return
	}
	if err := d.handlers.Run.HandleRunCommand(ctx, cmd, delivery); err != nil {
		logs.WarnContextf(ctx, "Run command handler error: %v", err)
	}
}

func (d *Dispatcher) handleControl(ctx context.Context, msg *nats.Msg) {
	cmd, err := d.parseCommand(msg.Data)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to parse control command: %v", err)
		return
	}
	if err := d.handlers.Control.HandleControlCommand(ctx, cmd); err != nil {
		logs.WarnContextf(ctx, "Control command handler error: %v", err)
	}
}

func (d *Dispatcher) handleInteraction(ctx context.Context, msg *nats.Msg) {
	cmd, err := d.parseCommand(msg.Data)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to parse interaction command: %v", err)
		return
	}
	if err := d.handlers.Interaction.HandleInteractionCommand(ctx, cmd); err != nil {
		logs.WarnContextf(ctx, "Interaction command handler error: %v", err)
	}
}

func (d *Dispatcher) handleSkill(ctx context.Context, msg *nats.Msg) {
	cmd, err := d.parseCommand(msg.Data)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to parse skill command: %v", err)
		return
	}
	if err := d.handlers.Skill.HandleSkillCommand(ctx, cmd, msg); err != nil {
		logs.WarnContextf(ctx, "Skill command handler error: %v", err)
	}
}
