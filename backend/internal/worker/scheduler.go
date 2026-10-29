package worker

import (
	"context"
	"errors"
	"time"
)

var ErrWorkerNotFound = errors.New("worker not found")

type WorkerEnvType string

const (
	WorkerEnvProcess  WorkerEnvType = "process"
	WorkerEnvDocker   WorkerEnvType = "docker"
	WorkerEnvKubeVirt WorkerEnvType = "kubevirt"
)

type WorkerScheduler interface {
	Start(ctx context.Context, spec *WorkerSpec) (*WorkerInstance, error)
	Stop(ctx context.Context, workerID string) error
	Health(ctx context.Context, workerID string) error
	List(ctx context.Context) ([]*WorkerInstance, error)
	Shutdown(ctx context.Context) error
}

// WorkerSpecReconciler reports whether an existing runtime worker diverges from
// the scheduler's desired spec. Schedulers that can inspect external state
// implement this optional interface.
type WorkerSpecReconciler interface {
	NeedsReconcile(ctx context.Context, spec *WorkerSpec) (bool, error)
}

// WorkerSpecDriftReporter 在 NeedsReconcile 的基础上报告具体漂移字段，
// 便于把“为什么又重建了 Pod”写进一行日志。
type WorkerSpecDriftReporter interface {
	DriftFields(ctx context.Context, spec *WorkerSpec) ([]string, error)
}

// WorkerBootstrapTokenReader 暴露线上 Deployment 正在使用的 bootstrap token。
// reconciler 用它来避免“每次 Start 都生成新 token”：token 会进 Pod template，
// 轮转一次就等于强制重建一次 Pod。Schedulers that cannot read it may skip this.
type WorkerBootstrapTokenReader interface {
	CurrentBootstrapToken(ctx context.Context, deploymentName string) (string, error)
}

type WorkerSpec struct {
	ID             string
	OrgID          uint
	WorkerID       uint
	Name           string
	Labels         map[string]string
	Annotations    map[string]string
	ServerAddr     string
	BootstrapToken string
	EnvType        WorkerEnvType
	Image          string
	Command        []string
	Args           []string
	Env            map[string]string
	WorkingDir     string
}

type WorkerInstance struct {
	ID        string
	WorkerID  string
	Status    string
	PID       int
	StartedAt time.Time
	Endpoint  string
}
