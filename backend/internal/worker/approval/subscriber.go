// Package approval 订阅 NATS 审批消息并路由到 ApprovalRouter。
package approval

import (
	"context"
	"encoding/json"
	"fmt"

	nats "github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/engines"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
)

// Config 审批订阅者配置。
type Config struct {
	OrgID    uint
	WorkerID uint
}

// Subscriber 订阅 worker 审批 NATS 主题，收到消息后调用 ApprovalRouter。
type Subscriber struct {
	cfg        Config
	subscriber eventbus.Subscriber
}

// New 创建审批订阅者。
func New(cfg Config, subscriber eventbus.Subscriber) (*Subscriber, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if subscriber == nil {
		return nil, fmt.Errorf("subscriber is required")
	}
	return &Subscriber{cfg: cfg, subscriber: subscriber}, nil
}

// Start 订阅审批 NATS 消息。
func (s *Subscriber) Start(ctx context.Context) error {
	topic, err := dm.WorkerApprovalSubject(s.cfg.OrgID, s.cfg.WorkerID)
	if err != nil {
		return fmt.Errorf("build approval topic: %w", err)
	}
	logs.Infof("Starting worker approval subscription: %s", topic)
	return s.subscriber.Subscribe(ctx, topic, "worker-approval", func(msg *nats.Msg) {
		var approval struct {
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
			Action    string `json:"action"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(msg.Data, &approval); err != nil {
			logs.Warnf("Failed to parse approval message: %v", err)
			return
		}
		logs.Infof("Worker received approval: session=%s request_id=%s action=%s", approval.SessionID, approval.RequestID, approval.Action)
		if err := engines.DefaultApprovalRouter.Resolve(approval.RequestID, approval.Action, approval.Reason); err != nil {
			logs.Warnf("Failed to resolve approval: %v", err)
		}
	})
}
