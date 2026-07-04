package scheduler

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/ygpkg/yg-go/logs"
)

// DockerCLIScheduler implements WorkerScheduler using Docker CLI commands.
// It manages containerized workers by spawning Docker containers and tracking their lifecycle.
type DockerCLIScheduler struct {
	config    *config.SchedulerConfig
	instances map[string]*DockerInstance
	mu        sync.RWMutex
}

// DockerInstance represents a running containerized worker.
type DockerInstance struct {
	ID          string
	WorkerID    string
	ContainerID string
	Status      string
	PID         int
	StartedAt   time.Time
	LastSeen    time.Time
	Image       string
	mu          sync.RWMutex
}

var _ worker.WorkerScheduler = (*DockerCLIScheduler)(nil)

// NewDockerCLIScheduler creates a new Docker CLI-based scheduler.
func NewDockerCLIScheduler(config *config.SchedulerConfig) worker.WorkerScheduler {
	return &DockerCLIScheduler{
		config:    config,
		instances: make(map[string]*DockerInstance),
	}
}

func containerName(workerID string) string {
	return fmt.Sprintf("leros-worker-%s", workerID)
}

func (ds *DockerCLIScheduler) execDocker(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), err
	}

	return strings.TrimSpace(stdout.String()), stderr.String(), nil
}

func (ds *DockerCLIScheduler) containerWorkingDir(workid string) string {
	return path.Join(ds.config.WorkingDir, "workspace", workid)
}

func (ds *DockerCLIScheduler) buildEnvVars(spec *worker.WorkerSpec) map[string]string {
	env := make(map[string]string)

	cfg := ds.config
	if cfg == nil {
		cfg = &config.SchedulerConfig{}
	}

	for key, value := range cfg.Env {
		env[key] = value
	}

	for key, value := range spec.Env {
		env[key] = value
	}

	serverAddr := strings.TrimSpace(spec.ServerAddr)
	if serverAddr == "" {
		serverAddr = strings.TrimSpace(cfg.ServerAddr)
	}
	if serverAddr != "" {
		env["LEROS_SERVER_ADDR"] = serverAddr
	}
	if spec.BootstrapToken != "" {
		env["LEROS_WORKER_BOOTSTRAP_TOKEN"] = spec.BootstrapToken
	}
	if spec.OrgID != 0 {
		env["LEROS_ORG_ID"] = fmt.Sprintf("%d", spec.OrgID)
	}
	if spec.WorkerID != 0 {
		env["LEROS_WORKER_ID"] = fmt.Sprintf("%d", spec.WorkerID)
	} else {
		env["LEROS_WORKER_ID"] = spec.ID
	}

	return env
}

func (ds *DockerCLIScheduler) createAndStartContainer(ctx context.Context, instance *DockerInstance, spec *worker.WorkerSpec, cName string) error {
	args := []string{"create", "--name", cName}
	args = append(args, "-v", ds.containerWorkingDir(spec.ID)+":/workspace")
	if spec.WorkingDir != "" {
		args = append(args, "-w", spec.WorkingDir)
	} else {
		args = append(args, "-w", "/workspace")
	}

	env := ds.buildEnvVars(spec)
	for key, value := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, spec.Image)

	if len(spec.Command) > 0 {
		args = append(args, spec.Command...)
	}
	if len(spec.Args) > 0 {
		args = append(args, spec.Args...)
	}

	stdout, stderr, err := ds.execDocker(ctx, args...)
	if err != nil {
		return fmt.Errorf("docker create failed: %w (stderr: %s)", err, stderr)
	}

	containerID := strings.TrimSpace(stdout)
	if containerID == "" {
		return fmt.Errorf("no container ID returned from docker create")
	}

	instance.mu.Lock()
	instance.ContainerID = containerID
	instance.Status = "created"
	instance.mu.Unlock()

	logs.Infof("Container created: %s", containerID)

	startArgs := []string{"start", containerID}
	_, stderr, err = ds.execDocker(ctx, startArgs...)
	if err != nil {
		return fmt.Errorf("docker start failed: %w (stderr: %s)", err, stderr)
	}

	logs.Infof("Container started: %s", containerID)
	return nil
}

func (ds *DockerCLIScheduler) inspectContainer(ctx context.Context, instance *DockerInstance) error {
	inspectArgs := []string{"inspect", "--format", "{{.State.Pid}}", instance.ContainerID}
	stdout, _, err := ds.execDocker(ctx, inspectArgs...)
	if err != nil {
		return fmt.Errorf("failed to inspect container PID: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(stdout, "%d", &pid); err != nil {
		return fmt.Errorf("failed to parse PID: %w", err)
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()
	instance.PID = pid
	instance.Status = "running"
	instance.LastSeen = time.Now()

	return nil
}

func (ds *DockerCLIScheduler) getContainerStatus(instance *DockerInstance) (string, error) {
	ctx := context.Background()
	inspectArgs := []string{"inspect", "--format", "{{.State.Status}}", instance.ContainerID}
	stdout, _, err := ds.execDocker(ctx, inspectArgs...)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(stdout)), nil
}

func (ds *DockerCLIScheduler) monitorContainer(instance *DockerInstance) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		status, err := ds.getContainerStatus(instance)
		if err != nil {
			logs.Errorf("Failed to get status for container %s: %v", instance.ContainerID, err)
			ds.removeInstance(instance.WorkerID)
			return
		}

		if status != "running" {
			logs.Infof("Container %s is no longer running (status: %s)", instance.ContainerID, status)
			instance.mu.Lock()
			instance.Status = status
			instance.mu.Unlock()
			ds.removeInstance(instance.WorkerID)
			return
		}

		instance.mu.Lock()
		instance.LastSeen = time.Now()
		instance.mu.Unlock()
	}
}

func (ds *DockerCLIScheduler) removeInstance(workerID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.instances, workerID)
}

// Start launches a new containerized worker and returns its instance information.
func (ds *DockerCLIScheduler) Start(ctx context.Context, spec *worker.WorkerSpec) (*worker.WorkerInstance, error) {
	if spec.EnvType != "" && spec.EnvType != worker.WorkerEnvDocker {
		return nil, fmt.Errorf("unsupported env type: %s, DockerCLIScheduler only supports docker runtime", spec.EnvType)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	workerID := spec.ID
	if workerID == "" {
		workerID = fmt.Sprintf("worker_%d", time.Now().UnixNano())
	}

	cName := containerName(workerID)

	instance := &DockerInstance{
		ID:        workerID,
		WorkerID:  workerID,
		Status:    "initializing",
		StartedAt: time.Now(),
		LastSeen:  time.Now(),
		Image:     spec.Image,
	}

	if err := ds.createAndStartContainer(ctx, instance, spec, cName); err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := ds.inspectContainer(ctx, instance); err != nil {
		logs.Warnf("Failed to inspect container %s: %v", instance.ContainerID, err)
	}

	ds.instances[workerID] = instance

	logs.Infof("Docker container %s for worker %s started", instance.ContainerID, workerID)

	go ds.monitorContainer(instance)

	return &worker.WorkerInstance{
		ID:        instance.ID,
		WorkerID:  instance.WorkerID,
		Status:    instance.Status,
		PID:       instance.PID,
		StartedAt: instance.StartedAt,
	}, nil
}

// Stop terminates a running containerized worker.
func (ds *DockerCLIScheduler) Stop(ctx context.Context, workerID string) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	instance, ok := ds.instances[workerID]
	if !ok {
		return fmt.Errorf("%w: %s", worker.ErrWorkerNotFound, workerID)
	}

	if err := ds.stopContainer(ctx, instance); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	instance.Status = "stopped"
	delete(ds.instances, workerID)
	logs.Infof("Worker %s stopped", workerID)
	return nil
}

func (ds *DockerCLIScheduler) stopContainer(ctx context.Context, instance *DockerInstance) error {
	instance.mu.RLock()
	containerID := instance.ContainerID
	instance.mu.RUnlock()

	if containerID == "" {
		return nil
	}

	stopArgs := []string{"stop", containerID}
	_, stderr, err := ds.execDocker(ctx, stopArgs...)
	if err != nil {
		logs.Warnf("Failed to stop container %s: %v (stderr: %s)", containerID, err, stderr)
	}

	rmArgs := []string{"rm", containerID}
	_, stderr, err = ds.execDocker(ctx, rmArgs...)
	if err != nil {
		logs.Warnf("Failed to remove container %s: %v (stderr: %s)", containerID, err, stderr)
	}

	logs.Infof("Container %s stopped and removed", containerID)
	return nil
}

// Health checks if a containerized worker is running and healthy.
func (ds *DockerCLIScheduler) Health(ctx context.Context, workerID string) error {
	ds.mu.RLock()
	instance, ok := ds.instances[workerID]
	ds.mu.RUnlock()

	if !ok {
		return fmt.Errorf("worker %s not found", workerID)
	}

	status, err := ds.getContainerStatus(instance)
	if err != nil {
		return fmt.Errorf("container health check failed: %w", err)
	}

	if status != "running" {
		return fmt.Errorf("container is not running (status: %s)", status)
	}

	instance.mu.Lock()
	instance.LastSeen = time.Now()
	instance.Status = "running"
	instance.mu.Unlock()

	return nil
}

// List returns all active containerized workers.
func (ds *DockerCLIScheduler) List(ctx context.Context) ([]*worker.WorkerInstance, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*worker.WorkerInstance, 0, len(ds.instances))
	for _, instance := range ds.instances {
		instance.mu.RLock()
		result = append(result, &worker.WorkerInstance{
			ID:        instance.ID,
			WorkerID:  instance.WorkerID,
			Status:    instance.Status,
			PID:       instance.PID,
			StartedAt: instance.StartedAt,
		})
		instance.mu.RUnlock()
	}
	return result, nil
}
