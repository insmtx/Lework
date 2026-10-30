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
	return s.queryWorkerStatus(ctx, orgID, workerID, false)
}

// QueryWorkerStatusForPreflight rejects incomplete counters instead of interpreting absent fields as idle.
func (s *WorkerOpsService) QueryWorkerStatusForPreflight(ctx context.Context, orgID, workerID uint) (*messaging.WorkerStatusSnapshot, error) {
	return s.queryWorkerStatus(ctx, orgID, workerID, true)
}

func (s *WorkerOpsService) queryWorkerStatus(ctx context.Context, orgID, workerID uint, strict bool) (*messaging.WorkerStatusSnapshot, error) {
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
	if strict && (!workerCountersPresent(reply.Data) || !workerMaintenancePresent(reply.Data)) {
		return nil, fmt.Errorf("%w: incomplete task or maintenance fields", ErrWorkerBadResponse)
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

// 预检使用字段存在性，不能把旧协议缺省整数当作零任务；普通状态接口保留兼容行为。
func workerCountersPresent(data []byte) bool {
	var c struct {
		Running     *int `json:"running_count"`
		Waiting     *int `json:"waiting_count"`
		Debounce    *int `json:"debounce_waiting_count"`
		Coordinator *int `json:"coordinator_waiting_count"`
		Admission   *int `json:"admission_waiting_count"`
		Accepted    *int `json:"accepted_count"`
		Compute     *int `json:"compute_busy_count"`
		Interaction *int `json:"interaction_waiting_count"`
		Pending     *int `json:"inbox_pending_count"`
		Processing  *int `json:"inbox_processing_count"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return false
	}
	return c.Running != nil && c.Waiting != nil && c.Debounce != nil && c.Coordinator != nil &&
		c.Admission != nil && c.Accepted != nil && c.Compute != nil && c.Interaction != nil &&
		c.Pending != nil && c.Processing != nil
}

// 旧 Worker 可缺少整个维护字段；已声明协议时，不把缺省布尔值当成明确状态。
func workerMaintenancePresent(data []byte) bool {
	var envelope struct {
		Maintenance json.RawMessage `json:"maintenance"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return false
	}
	if len(envelope.Maintenance) == 0 || string(envelope.Maintenance) == "null" {
		return true
	}
	var state struct {
		Configured  *bool   `json:"configured"`
		Known       *bool   `json:"known"`
		Active      *bool   `json:"active"`
		HostID      *string `json:"host_id"`
		Revision    *uint64 `json:"revision"`
		OperationID *string `json:"operation_id"`
	}
	if json.Unmarshal(envelope.Maintenance, &state) != nil {
		return false
	}
	return state.Configured != nil && state.Known != nil && state.Active != nil && state.HostID != nil && state.Revision != nil && state.OperationID != nil
}
