package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/nats-io/nats.go"
)

// WorkerOpsService 提供 Worker 运维状态查询。
//
// 通过 Core NATS request/reply 查询指定 Worker 的本地运行快照，
// 不写入 JetStream、不进入任务队列。服务层把 NATS/超时/响应解析错误
// 翻译为可识别的哨兵错误，由 HTTP handler 映射为对应状态码。
type WorkerOpsService struct {
	requester mq.CoreRequester
	timeout   time.Duration
}

// NewWorkerOpsService 创建 Worker 运维状态查询服务。
// timeout 为每次 request/reply 的默认超时。
func NewWorkerOpsService(requester mq.CoreRequester, timeout time.Duration) *WorkerOpsService {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &WorkerOpsService{requester: requester, timeout: timeout}
}

// Worker 状态查询错误分类，handler 据此映射 HTTP 状态码。
var (
	// ErrWorkerTimeout 表示 Worker 未在超时时间内回复。
	ErrWorkerTimeout = errors.New("worker status query timed out")
	// ErrWorkerUnavailable 表示 NATS 不可用或请求发送失败。
	ErrWorkerUnavailable = errors.New("worker status query unavailable")
	// ErrWorkerBadResponse 表示收到回复但无法解析。
	ErrWorkerBadResponse = errors.New("worker status query bad response")
)

// QueryWorkerStatus 查询指定 Worker 的本地运行状态快照。
func (s *WorkerOpsService) QueryWorkerStatus(ctx context.Context, orgID, workerID uint) (*messaging.WorkerStatusSnapshot, error) {
	if s.requester == nil {
		return nil, ErrWorkerUnavailable
	}
	subject, err := messaging.WorkerOpsStatusSubject(orgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkerBadResponse, err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	reply, err := s.requester.RequestReply(reqCtx, subject, messaging.WorkerStatusRequest{OrgID: orgID, WorkerID: workerID})
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			return nil, ErrWorkerUnavailable
		}
		if isNATSTimeout(reqCtx, err) {
			return nil, ErrWorkerTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrWorkerUnavailable, err)
	}
	if reply == nil {
		return nil, fmt.Errorf("%w: empty NATS reply", ErrWorkerBadResponse)
	}

	var snapshot messaging.WorkerStatusSnapshot
	if err := json.Unmarshal(reply.Data, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkerBadResponse, err)
	}
	if snapshot.OrgID != orgID || snapshot.WorkerID != workerID || snapshot.SnapshotAt <= 0 {
		return nil, fmt.Errorf("%w: response identity or timestamp mismatch", ErrWorkerBadResponse)
	}
	return &snapshot, nil
}

// isNATSTimeout 判定请求失败是否属于超时，用于区分 504 与 503。
func isNATSTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// NATS 超时错误可能包装了 deadline 或显式超时。
	if errors.Is(err, nats.ErrTimeout) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// 请求上下文已到期但底层错误未直接暴露 deadline。
	if ctx.Err() != nil {
		return true
	}
	return false
}
