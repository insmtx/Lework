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
}

// WorkerSpecReconciler reports whether an existing runtime worker diverges from
// the scheduler's desired spec. Schedulers that can inspect external state
// implement this optional interface.
type WorkerSpecReconciler interface {
	NeedsReconcile(ctx context.Context, spec *WorkerSpec) (bool, error)
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
