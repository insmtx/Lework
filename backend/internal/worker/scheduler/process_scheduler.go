package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/ygpkg/yg-go/logs"
)

type ProcessScheduler struct {
	config    *config.SchedulerConfig
	instances map[string]*ProcessInstance
	mu        sync.RWMutex
}

var _ worker.WorkerScheduler = (*ProcessScheduler)(nil)

type ProcessInstance struct {
	ID        string
	WorkerID  string
	Cmd       *exec.Cmd
	Process   *os.Process
	Status    string
	StartedAt time.Time
	LastSeen  time.Time
	Endpoint  string
	mu        sync.RWMutex
}

func NewProcessScheduler(config *config.SchedulerConfig) worker.WorkerScheduler {
	return &ProcessScheduler{
		config:    config,
		instances: make(map[string]*ProcessInstance),
	}
}

func (ps *ProcessScheduler) Start(ctx context.Context, spec *worker.WorkerSpec) (*worker.WorkerInstance, error) {
	if spec.EnvType != "" && spec.EnvType != worker.WorkerEnvProcess {
		return nil, fmt.Errorf("unsupported env type: %s, ProcessScheduler only supports process runtime", spec.EnvType)
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	workerID := spec.ID
	if workerID == "" {
		workerID = fmt.Sprintf("worker_%d", time.Now().UnixNano())
	}

	instance := &ProcessInstance{
		ID:        workerID,
		WorkerID:  workerID,
		Status:    "initializing",
		StartedAt: time.Now(),
		LastSeen:  time.Now(),
	}
	instance.Endpoint = processWorkerEndpoint(spec, ps.config)

	if err := ps.startProcess(instance, spec); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	ps.instances[workerID] = instance

	return &worker.WorkerInstance{
		ID:        instance.ID,
		WorkerID:  instance.WorkerID,
		Status:    instance.Status,
		PID:       instance.Process.Pid,
		StartedAt: instance.StartedAt,
		Endpoint:  instance.Endpoint,
	}, nil
}

func (ps *ProcessScheduler) Stop(ctx context.Context, workerID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	instance, ok := ps.instances[workerID]
	if !ok {
		return fmt.Errorf("%w: %s", worker.ErrWorkerNotFound, workerID)
	}

	if err := ps.stopProcess(instance); err != nil {
		return fmt.Errorf("failed to stop process: %w", err)
	}

	instance.Status = "stopped"
	delete(ps.instances, workerID)
	return nil
}

func (ps *ProcessScheduler) Health(ctx context.Context, workerID string) error {
	ps.mu.RLock()
	instance, ok := ps.instances[workerID]
	ps.mu.RUnlock()

	if !ok {
		return fmt.Errorf("worker %s not found", workerID)
	}

	return ps.healthCheck(instance)
}

func (ps *ProcessScheduler) List(ctx context.Context) ([]*worker.WorkerInstance, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := make([]*worker.WorkerInstance, 0, len(ps.instances))
	for _, instance := range ps.instances {
		instance.mu.RLock()
		result = append(result, &worker.WorkerInstance{
			ID:        instance.ID,
			WorkerID:  instance.WorkerID,
			Status:    instance.Status,
			PID:       instance.Process.Pid,
			StartedAt: instance.StartedAt,
			Endpoint:  instance.Endpoint,
		})
		instance.mu.RUnlock()
	}
	return result, nil
}

func (ps *ProcessScheduler) startProcess(instance *ProcessInstance, spec *worker.WorkerSpec) error {
	cfg := ps.config
	if cfg == nil {
		cfg = &config.SchedulerConfig{}
	}
	cmdPath := cfg.WorkerBinary
	if cmdPath == "" {
		cmdPath = "./bundles/leros"
	}

	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		return fmt.Errorf("worker binary not found: %s", cmdPath)
	}

	env := os.Environ()
	for key, value := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	if spec.BootstrapToken != "" {
		env = append(env, "LEROS_WORKER_BOOTSTRAP_TOKEN="+spec.BootstrapToken)
	}

	workDir := cfg.WorkingDir
	if workDir == "" {
		workDir = filepath.Dir(cmdPath)
	}

	args := []string{cmdPath, "worker"}
	if spec.OrgID != 0 {
		args = append(args, "--org-id", strconv.FormatUint(uint64(spec.OrgID), 10))
	}
	if spec.WorkerID != 0 {
		args = append(args, "--worker-id", strconv.FormatUint(uint64(spec.WorkerID), 10))
	}
	serverAddr := strings.TrimSpace(spec.ServerAddr)
	if serverAddr == "" {
		serverAddr = strings.TrimSpace(cfg.ServerAddr)
	}
	if serverAddr != "" {
		args = append(args, "--server-addr", serverAddr)
	}
	if spec.BootstrapToken != "" {
		args = append(args, "--bootstrap-token", spec.BootstrapToken)
	}
	if instance.Endpoint != "" {
		args = append(args, "--listen-addr", strings.TrimPrefix(instance.Endpoint, "http://"))
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	instance.mu.Lock()
	instance.Cmd = cmd
	instance.Process = cmd.Process
	instance.Status = "running"
	instance.LastSeen = time.Now()
	instance.mu.Unlock()

	logs.Infof("Worker process for assistant %s started with PID %d", spec.ID, cmd.Process.Pid)

	go ps.monitorProcess(instance)

	return nil
}

func (ps *ProcessScheduler) stopProcess(instance *ProcessInstance) error {
	instance.mu.RLock()
	defer instance.mu.RUnlock()

	if instance.Process == nil {
		return nil
	}

	if err := instance.Process.Signal(os.Interrupt); err != nil {
		logs.Warnf("Failed to send interrupt signal to %s: %v", instance.WorkerID, err)
		if err := instance.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
	}

	logs.Infof("Sent interrupt signal to worker %s", instance.WorkerID)
	return nil
}

func (ps *ProcessScheduler) healthCheck(instance *ProcessInstance) error {
	instance.mu.RLock()
	endpoint := strings.TrimSpace(instance.Endpoint)
	cmd := instance.Cmd
	instance.mu.RUnlock()

	if endpoint == "" {
		return fmt.Errorf("worker endpoint is not configured")
	}
	if cmd != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("process exited")
	}

	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("create worker health request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("worker health request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("worker health status %d", resp.StatusCode)
	}

	instance.mu.Lock()
	instance.LastSeen = time.Now()
	instance.Status = "running"
	instance.mu.Unlock()
	return nil
}

func processWorkerEndpoint(spec *worker.WorkerSpec, cfg *config.SchedulerConfig) string {
	listenAddr := ""
	if spec != nil && spec.Env != nil {
		listenAddr = strings.TrimSpace(spec.Env["LEROS_WORKER_LISTEN_ADDR"])
	}
	if listenAddr == "" && cfg != nil && cfg.Env != nil {
		listenAddr = strings.TrimSpace(cfg.Env["LEROS_WORKER_LISTEN_ADDR"])
	}
	if listenAddr == "" {
		workerID := uint(1)
		if spec != nil && spec.WorkerID != 0 {
			workerID = spec.WorkerID
		}
		listenAddr = "127.0.0.1:" + strconv.FormatUint(uint64(18080+workerID), 10)
	}
	if strings.HasPrefix(listenAddr, "http://") || strings.HasPrefix(listenAddr, "https://") {
		return strings.TrimRight(listenAddr, "/")
	}
	if strings.HasPrefix(listenAddr, ":") {
		listenAddr = "127.0.0.1" + listenAddr
	}
	return "http://" + strings.TrimRight(listenAddr, "/")
}

func (ps *ProcessScheduler) monitorProcess(instance *ProcessInstance) {
	instance.mu.RLock()
	cmd := instance.Cmd
	instance.mu.RUnlock()

	if cmd == nil {
		return
	}

	err := cmd.Wait()

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if err != nil {
		logs.Errorf("Worker process %s exited with error: %v", instance.WorkerID, err)
		instance.Status = "error"
	} else {
		logs.Infof("Worker process %s exited normally", instance.WorkerID)
		instance.Status = "stopped"
	}

	ps.removeInstance(instance.WorkerID)
}

func (ps *ProcessScheduler) removeInstance(workerID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.instances, workerID)
}
