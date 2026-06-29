// Package command 提供统一的 Worker Command 分发器。
//
// Dispatcher 负责启动各 lane 订阅（cmd.run、cmd.control、cmd.interaction、cmd.skill），
// 并将收到的统一 WorkerCommand 分发到对应的 handler。
package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// Subscriber 是 Dispatcher 所需的最小订阅接口。
type Subscriber interface {
	Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error
}

// RunHandler 处理 cmd.run lane 的 agent run 命令。
// msg 是原始 NATS 消息，供 handler 获取 stream seq 用于 seq tracking 和背压控制。
type RunHandler interface {
	HandleRunCommand(ctx context.Context, cmd messaging.WorkerCommand, msg *nats.Msg) error
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
// 一次性校验 worker 标识、subscriber 和全部 handler。
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
// 任一订阅异常退出时会取消其他 lane 的 context 并返回带 lane/subject 上下文的错误。
// ctx 正常取消时返回 nil。
func (d *Dispatcher) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lanes := []struct {
		lane     messaging.Lane
		consumer string
		handler  func(ctx context.Context, msg *nats.Msg)
	}{
		{messaging.LaneRun, messaging.WorkerRunConsumer(), d.handleRun},
		{messaging.LaneControl, messaging.WorkerControlConsumer(), d.handleControl},
		{messaging.LaneInteraction, messaging.WorkerInteractionConsumer(), d.handleInteraction},
		{messaging.LaneSkill, messaging.WorkerSkillConsumer(), d.handleSkill},
	}

	errCh := make(chan error, len(lanes))

	for _, l := range lanes {
		topic, err := messaging.WorkerCommandSubject(d.cfg.OrgID, d.cfg.WorkerID, l.lane)
		if err != nil {
			return fmt.Errorf("build subject for lane %s: %w", l.lane, err)
		}

		go func(lane messaging.Lane, topic, consumer string, handler func(ctx context.Context, msg *nats.Msg)) {
			logs.InfoContextf(ctx, "Command dispatcher starting lane %s on topic %s", lane, topic)
			err := d.sub.Subscribe(ctx, topic, consumer, func(msg *nats.Msg) {
				handler(ctx, msg)
			})
			if err != nil {
				errCh <- fmt.Errorf("lane %s (topic %s): %w", lane, topic, err)
			} else {
				errCh <- nil
			}
		}(l.lane, topic, l.consumer, l.handler)
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

func (d *Dispatcher) handleRun(ctx context.Context, msg *nats.Msg) {
	cmd, err := d.parseCommand(msg.Data)
	if err != nil {
		logs.WarnContextf(ctx, "Failed to parse run command: %v", err)
		return
	}
	if err := d.handlers.Run.HandleRunCommand(ctx, cmd, msg); err != nil {
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
