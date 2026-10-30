package spark

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/maintenance"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

// ErrNotConfigured prevents partially configured deployments from selecting arbitrary targets.
var ErrNotConfigured = errors.New("spark management not configured")

// WorkerQuerier reuses authenticated deployment bindings instead of caller-supplied worker IDs.
type WorkerQuerier interface {
	QueryWorkerStatusForPreflight(context.Context, uint, uint) (*messaging.WorkerStatusSnapshot, error)
}

// Service exposes observation only until maintenance and restore adapters are verified.
type Service struct {
	cfg         config.SparkConfig
	workers     WorkerQuerier
	maintenance *maintenance.Reader
}

// NewService keeps Spark management optional for both existing backend editions.
func NewService(cfg config.SparkConfig, workers WorkerQuerier) *Service {
	cfg.WorkerIDs = append([]uint(nil), cfg.WorkerIDs...)
	return &Service{cfg: cfg, workers: workers, maintenance: maintenance.NewReader(cfg.Maintenance)}
}

// OrgID supplies the immutable installation binding for server-side authorization.
func (s *Service) OrgID() uint { return s.cfg.OrgID }

// Status never infers a safe-to-power-off state from an empty inventory or idle worker.
func (s *Service) Status(ctx context.Context) (types.SparkStatus, error) {
	var result types.SparkStatus
	if s.cfg.OrgID == 0 || !shortText(s.cfg.HostID) || !filepath.IsAbs(s.cfg.SocketPath) || len(s.cfg.WorkerIDs) > 32 {
		return result, ErrNotConfigured
	}
	seen := make(map[uint]bool)
	for _, id := range s.cfg.WorkerIDs {
		if id == 0 || seen[id] {
			return result, ErrNotConfigured
		}
		seen[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snapshot, err := readSnapshot(ctx, s.cfg.SocketPath, s.cfg.HostID)
	if err != nil {
		return result, err
	}
	result.Snapshot = snapshot
	gate := s.maintenance.Read()
	result.Maintenance = maintenancePhase(&gate, s.cfg.HostID)
	result.Workers = make([]types.SparkWorkerState, 0, len(s.cfg.WorkerIDs))
	result.Checks = append([]types.SparkCheck{}, snapshot.Checks...)
	add := func(code, message string) {
		for _, c := range result.Checks {
			if c.Code == code {
				return
			}
		}
		result.Checks = append(result.Checks, types.SparkCheck{Code: code, Message: message})
	}
	// 能力由后端决定，不信任采集器通过清空阻塞项开启变更操作。
	add("execution_disabled", "当前版本仅支持只读预检，尚未启用执行能力。")
	add("model_unconfigured", "未配置可管理的本机模型。")
	add("maintenance_unavailable", "任务维护控制入口和完整排空流程尚未启用。")
	add("recovery_unavailable", "服务停止顺序和开机恢复尚未完成验证。")
	if !snapshot.InventoryComplete {
		add("inventory_incomplete", "工作负载清单不完整，无法确认关机影响。")
	}
	for _, c := range snapshot.Components {
		if !c.Registered {
			add("unregistered_workloads", "存在尚未登记停止与恢复策略的业务工作负载。")
		}
	}
	if len(s.cfg.WorkerIDs) == 0 {
		add("worker_mapping_missing", "尚未登记本机 Worker，任务状态未知。")
	}
	for _, id := range s.cfg.WorkerIDs {
		item := types.SparkWorkerState{WorkerID: id, State: "unknown", Maintenance: "unknown"}
		var w *messaging.WorkerStatusSnapshot
		var queryErr error
		if s.workers != nil {
			queryCtx, queryCancel := context.WithTimeout(ctx, 3*time.Second)
			w, queryErr = s.workers.QueryWorkerStatusForPreflight(queryCtx, s.cfg.OrgID, id)
			queryCancel()
		}
		if queryErr != nil || w == nil || w.OrgID != s.cfg.OrgID || w.WorkerID != id || !fresh(w.SnapshotAt) || w.Degraded || !validCounts(w) {
			add("worker_unknown", "部分 Worker 状态无法确认；不能按零任务处理。")
		} else {
			item.State = "observed"
			item.Maintenance = maintenancePhase(w.Maintenance, s.cfg.HostID)
			item.ObservedAt = w.SnapshotAt
			running, waiting := w.RunningCount, w.WaitingCount
			item.RunningCount = &running
			item.WaitingCount = &waiting
			if busy(w) {
				item.State = "busy"
				add("worker_busy", "仍有正在运行、排队或等待交互的任务。")
			}
		}
		result.Workers = append(result.Workers, item)
	}
	// 聚合期间快照可能过期；过期不能作为新的预检返回。
	if !fresh(snapshot.ObservedAt) {
		return types.SparkStatus{}, ErrBadResponse
	}
	result.ValidUntil = snapshot.ObservedAt + 30
	for i := range result.Workers {
		item := &result.Workers[i]
		if item.State == "unknown" {
			continue
		}
		if !fresh(item.ObservedAt) {
			item.State = "unknown"
			item.Maintenance = "unknown"
			item.RunningCount = nil
			item.WaitingCount = nil
			item.ObservedAt = 0
			add("worker_unknown", "部分 Worker 状态无法确认；不能按零任务处理。")
			continue
		}
		if until := item.ObservedAt + 30; until < result.ValidUntil {
			result.ValidUntil = until
		}
	}
	return result, nil
}

// Preflight performs a fresh read and does not enqueue or execute any mutation.
func (s *Service) Preflight(ctx context.Context) (types.SparkPreflight, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return types.SparkPreflight{}, err
	}
	return types.SparkPreflight{Ready: false, CheckedAt: time.Now().Unix(), Status: status}, nil
}
func counts(w *messaging.WorkerStatusSnapshot) []int {
	return []int{w.RunningCount, w.WaitingCount, w.AcceptedCount, w.AdmissionWaitingCount, w.ComputeBusyCount, w.InteractionWaitingCount, w.InboxPendingCount, w.InboxProcessingCount, w.DebounceWaitingCount, w.CoordinatorWaitingCount}
}
func validCounts(w *messaging.WorkerStatusSnapshot) bool {
	for _, n := range counts(w) {
		if n < 0 {
			return false
		}
	}
	return true
}
func busy(w *messaging.WorkerStatusSnapshot) bool {
	for _, n := range counts(w) {
		if n > 0 {
			return true
		}
	}
	return false
}

func maintenancePhase(state *types.SparkMaintenanceState, hostID string) string {
	if state == nil || !state.Configured {
		return "unsupported"
	}
	if !state.Known || state.HostID != hostID || state.Revision == 0 || state.Revision > 9007199254740991 || !shortText(state.OperationID) {
		return "unknown"
	}
	if state.Active {
		return "paused"
	}
	return "accepting"
}
