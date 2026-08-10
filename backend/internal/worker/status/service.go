// Package status 提供 Worker 本地运行状态的 Core NATS 查询订阅。
//
// 该订阅独立于命令 Dispatcher 的 JetStream lane：它使用 Core NATS 直接订阅
// org.<org_id>.worker.<worker_id>.ops.status，以 request/reply 方式回答
// Server 的运维状态查询，不进入任务队列、不写入 JetStream。
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// StatusProvider 提供 Worker 本地运行快照。
type StatusProvider interface {
	Status(ctx context.Context) messaging.WorkerStatusSnapshot
}

// CoreConn 是 Status Service 所需的 Core NATS 能力子集。
type CoreConn interface {
	// Subscribe 订阅 Core NATS subject（非 JetStream）。
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// Config 控制 Worker 状态订阅服务。
type Config struct {
	OrgID                uint
	WorkerID             uint
	QueryTimeout         time.Duration
	MaxConcurrentQueries int
}

// Service 订阅 Worker 的 ops.status 查询 subject，并回答本地状态快照。
type Service struct {
	cfg        Config
	conn       CoreConn
	status     StatusProvider
	querySlots chan struct{}

	stateMu sync.Mutex
	stopped bool
	queries sync.WaitGroup
}

// New 创建 Worker 状态订阅服务。
func New(cfg Config, conn CoreConn, status StatusProvider) (*Service, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if conn == nil {
		return nil, fmt.Errorf("core nats connection is required")
	}
	if status == nil {
		return nil, fmt.Errorf("status provider is required")
	}
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = 2 * time.Second
	}
	if cfg.MaxConcurrentQueries <= 0 {
		cfg.MaxConcurrentQueries = 4
	}
	return &Service{
		cfg:        cfg,
		conn:       conn,
		status:     status,
		querySlots: make(chan struct{}, cfg.MaxConcurrentQueries),
	}, nil
}

// Start 订阅 ops.status subject 并阻塞直到 ctx 取消。
func (s *Service) Start(ctx context.Context) error {
	subject, err := messaging.WorkerOpsStatusSubject(s.cfg.OrgID, s.cfg.WorkerID)
	if err != nil {
		return fmt.Errorf("build status subject: %w", err)
	}
	sub, err := s.conn.Subscribe(subject, s.handle)
	if err != nil {
		return fmt.Errorf("subscribe status subject %s: %w", subject, err)
	}
	logs.InfoContextf(ctx, "Worker status subscriber started on %s", subject)
	<-ctx.Done()

	// 先拒绝新的回调，再等待已开始的状态查询结束，避免关闭本地 inbox 后仍有
	// 回调读取它。每个查询都受 QueryTimeout 限制，因此不会无限阻塞关闭流程。
	s.stateMu.Lock()
	s.stopped = true
	s.stateMu.Unlock()
	if err := sub.Unsubscribe(); err != nil {
		logs.WarnContextf(ctx, "Failed to unsubscribe status subject %s: %v", subject, err)
	}
	s.queries.Wait()
	logs.InfoContextf(ctx, "Worker status subscriber stopped on %s", subject)
	return ctx.Err()
}

// handle 处理一条 Core NATS 状态查询请求并回复快照。
func (s *Service) handle(msg *nats.Msg) {
	if !s.beginQuery() {
		return
	}
	defer s.queries.Done()

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.QueryTimeout)
	defer cancel()

	if err := s.verifyRequest(msg.Data); err != nil {
		logs.WarnContextf(ctx, "Worker status request rejected: subject=%s err=%v", msg.Subject, err)
		// Core NATS 没有确认语义。归属校验失败时不回复，避免泄露状态；不能调用
		// JetStream 的 Term/Ack/Nak，否则会把确认载荷错误写入请求方 inbox。
		return
	}

	select {
	case s.querySlots <- struct{}{}:
		defer func() { <-s.querySlots }()
	default:
		snapshot := messaging.WorkerStatusSnapshot{
			OrgID:      s.cfg.OrgID,
			WorkerID:   s.cfg.WorkerID,
			SnapshotAt: time.Now().UTC().Unix(),
			Degraded:   true,
			Errors:     []string{"status_query_busy"},
		}
		s.respond(ctx, msg, snapshot)
		return
	}

	snapshot := s.status.Status(ctx)
	// Worker 身份由订阅配置决定，不能信任 Provider 的零值或错误身份。
	snapshot.OrgID = s.cfg.OrgID
	snapshot.WorkerID = s.cfg.WorkerID
	if snapshot.SnapshotAt == 0 {
		snapshot.SnapshotAt = time.Now().UTC().Unix()
	}
	s.respond(ctx, msg, snapshot)
}

func (s *Service) beginQuery() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.stopped {
		return false
	}
	s.queries.Add(1)
	return true
}

func (s *Service) respond(ctx context.Context, msg *nats.Msg, snapshot messaging.WorkerStatusSnapshot) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		logs.ErrorContextf(ctx, "Marshal worker status snapshot: %v", err)
		return
	}
	if err := msg.Respond(data); err != nil {
		logs.WarnContextf(ctx, "Respond worker status snapshot: %v", err)
	}
}

// verifyRequest 校验请求的 org/worker 归属与本 Worker 一致。
func (s *Service) verifyRequest(data []byte) error {
	var req messaging.WorkerStatusRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("decode status request: %w", err)
	}
	if req.OrgID != s.cfg.OrgID {
		return fmt.Errorf("org_id %d does not match worker org_id %d", req.OrgID, s.cfg.OrgID)
	}
	if req.WorkerID != s.cfg.WorkerID {
		return fmt.Errorf("worker_id %d does not match worker worker_id %d", req.WorkerID, s.cfg.WorkerID)
	}
	return nil
}

// Subject 返回该服务订阅的 subject（用于日志与测试）。
func (s *Service) Subject() string {
	subject, _ := messaging.WorkerOpsStatusSubject(s.cfg.OrgID, s.cfg.WorkerID)
	return subject
}
