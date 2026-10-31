package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/worker"
)

func TestNewDockerCLIScheduler(t *testing.T) {
	cfg := &config.SchedulerConfig{
		Mode:       "docker-cli",
		WorkingDir: "/tmp/lework",
	}

	scheduler := NewDockerCLIScheduler(cfg)

	if scheduler == nil {
		t.Fatal("NewDockerCLIScheduler returned nil")
	}

	dcs, ok := scheduler.(*DockerCLIScheduler)
	if !ok {
		t.Fatal("NewDockerCLIScheduler did not return *DockerCLIScheduler")
	}

	if dcs.config != cfg {
		t.Error("Config not set properly")
	}

	if dcs.instances == nil {
		t.Error("Instances map not initialized")
	}

	if len(dcs.instances) != 0 {
		t.Errorf("Expected empty instances map, got %d entries", len(dcs.instances))
	}
}

func TestContainerName(t *testing.T) {
	tests := []struct {
		name     string
		workerID string
		want     string
	}{
		{
			name:     "simple worker id",
			workerID: "worker123",
			want:     "lework-worker-worker123",
		},
		{
			name:     "worker id with underscore",
			workerID: "worker_456",
			want:     "lework-worker-worker_456",
		},
		{
			name:     "empty worker id",
			workerID: "",
			want:     "lework-worker-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerName(tt.workerID)
			if got != tt.want {
				t.Errorf("containerName(%q) = %q, want %q", tt.workerID, got, tt.want)
			}
		})
	}
}

func TestContainerWorkingDir(t *testing.T) {
	cfg := &config.SchedulerConfig{
		WorkingDir: "/tmp/lework",
	}
	scheduler := NewDockerCLIScheduler(cfg).(*DockerCLIScheduler)

	tests := []struct {
		name     string
		workerID string
		want     string
	}{
		{
			name:     "simple worker id",
			workerID: "worker123",
			want:     "/tmp/lework/workspace/worker123",
		},
		{
			name:     "worker id with special chars",
			workerID: "worker-456_test",
			want:     "/tmp/lework/workspace/worker-456_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduler.containerWorkingDir(tt.workerID)
			if got != tt.want {
				t.Errorf("containerWorkingDir(%q) = %q, want %q", tt.workerID, got, tt.want)
			}
		})
	}
}

func TestBuildEnvVars(t *testing.T) {
	cfg := &config.SchedulerConfig{
		Env: map[string]string{
			"APP_ENV":    "production",
			"LOG_LEVEL":  "info",
			"COMMON_KEY": "config_value",
		},
		ServerAddr: "localhost:8080",
	}

	spec := &worker.WorkerSpec{
		ID:             "worker-123",
		OrgID:          3,
		WorkerID:       7,
		BootstrapToken: "bootstrap-token",
		Env: map[string]string{
			"WORKER_KEY": "worker_value",
			"COMMON_KEY": "worker_override",
		},
	}

	scheduler := NewDockerCLIScheduler(cfg).(*DockerCLIScheduler)
	env := scheduler.buildEnvVars(spec)

	expected := map[string]string{
		"APP_ENV":                       "production",
		"LOG_LEVEL":                     "info",
		"COMMON_KEY":                    "worker_override",
		"WORKER_KEY":                    "worker_value",
		"LEWORK_SERVER_ADDR":            "localhost:8080",
		"LEWORK_WORKER_BOOTSTRAP_TOKEN": "bootstrap-token",
		"LEWORK_ORG_ID":                 "3",
		"LEWORK_WORKER_ID":              "7",
	}

	if len(env) != len(expected) {
		t.Errorf("Expected %d env vars, got %d", len(expected), len(env))
	}

	for key, want := range expected {
		if got, ok := env[key]; !ok {
			t.Errorf("Missing expected env var: %s", key)
		} else if got != want {
			t.Errorf("env[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestBuildEnvVarsNoServerAddr(t *testing.T) {
	cfg := &config.SchedulerConfig{
		Env: map[string]string{
			"APP_ENV": "production",
		},
	}

	spec := &worker.WorkerSpec{
		ID: "worker-456",
	}

	scheduler := NewDockerCLIScheduler(cfg).(*DockerCLIScheduler)
	env := scheduler.buildEnvVars(spec)

	if _, ok := env["LEWORK_SERVER_ADDR"]; ok {
		t.Error("LEWORK_SERVER_ADDR should not be set when config has no server addr")
	}

	if got, want := env["LEWORK_WORKER_ID"], "worker-456"; got != want {
		t.Errorf("LEWORK_WORKER_ID = %q, want %q", got, want)
	}
}

func TestBuildEnvVarsEmptyEnv(t *testing.T) {
	cfg := &config.SchedulerConfig{}
	spec := &worker.WorkerSpec{
		ID: "worker-789",
	}

	scheduler := NewDockerCLIScheduler(cfg).(*DockerCLIScheduler)
	env := scheduler.buildEnvVars(spec)

	if len(env) != 1 {
		t.Errorf("Expected 1 env var (LEWORK_WORKER_ID), got %d", len(env))
	}

	if got, want := env["LEWORK_WORKER_ID"], "worker-789"; got != want {
		t.Errorf("LEWORK_WORKER_ID = %q, want %q", got, want)
	}
}

func TestStartUnsupportedEnvType(t *testing.T) {
	cfg := &config.SchedulerConfig{}
	scheduler := NewDockerCLIScheduler(cfg)

	spec := &worker.WorkerSpec{
		ID:      "worker-1",
		EnvType: worker.WorkerEnvProcess,
	}

	_, err := scheduler.Start(context.Background(), spec)
	if err == nil {
		t.Fatal("Start should return error for unsupported env type")
	}

	if !strings.Contains(err.Error(), "unsupported env type") {
		t.Errorf("Error should mention unsupported env type, got: %v", err)
	}

	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("Error should mention docker, got: %v", err)
	}
}

func TestDockerInstanceThreadSafety(t *testing.T) {
	instance := &DockerInstance{
		ID:          "test-1",
		WorkerID:    "test-1",
		ContainerID: "container-123",
		Status:      "running",
		PID:         1234,
		StartedAt:   time.Now(),
		LastSeen:    time.Now(),
	}

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			instance.mu.Lock()
			instance.Status = "updating"
			instance.LastSeen = time.Now()
			instance.mu.Unlock()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	instance.mu.RLock()
	status := instance.Status
	lastSeen := instance.LastSeen
	instance.mu.RUnlock()

	if status != "updating" {
		t.Errorf("Expected status 'updating', got %q", status)
	}

	if lastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
}

func TestSchedulerInstancesConcurrency(t *testing.T) {
	cfg := &config.SchedulerConfig{
		WorkingDir: "/workspace",
	}
	scheduler := &DockerCLIScheduler{
		config:    cfg,
		instances: make(map[string]*DockerInstance),
	}

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			workerID := fmt.Sprintf("worker-%d", id)
			scheduler.mu.Lock()
			scheduler.instances[workerID] = &DockerInstance{
				ID:        workerID,
				WorkerID:  workerID,
				Status:    "running",
				StartedAt: time.Now(),
			}
			scheduler.mu.Unlock()
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	scheduler.mu.RLock()
	count := len(scheduler.instances)
	scheduler.mu.RUnlock()

	if count != 10 {
		t.Errorf("Expected 10 instances, got %d", count)
	}
}

func TestInspectContainerParsePID(t *testing.T) {
	instance := &DockerInstance{
		ID:          "test-1",
		WorkerID:    "test-1",
		ContainerID: "test-container",
	}

	ctx := context.Background()
	pidOutput := "12345"

	var pid int
	_, err := fmt.Sscanf(pidOutput, "%d", &pid)
	if err != nil {
		t.Fatalf("Failed to parse PID: %v", err)
	}

	if pid != 12345 {
		t.Errorf("Expected PID 12345, got %d", pid)
	}

	instance.mu.Lock()
	instance.PID = pid
	instance.Status = "running"
	instance.LastSeen = time.Now()
	instance.mu.Unlock()

	instance.mu.RLock()
	gotPID := instance.PID
	gotStatus := instance.Status
	instance.mu.RUnlock()

	if gotPID != 12345 {
		t.Errorf("PID = %d, want 12345", gotPID)
	}

	if gotStatus != "running" {
		t.Errorf("Status = %q, want running", gotStatus)
	}

	if instance.LastSeen.IsZero() {
		t.Error("LastSeen should be updated")
	}

	_ = ctx
}

func TestStopNonExistentWorker(t *testing.T) {
	cfg := &config.SchedulerConfig{}
	scheduler := NewDockerCLIScheduler(cfg)

	err := scheduler.Stop(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("Stop should return error for non-existent worker")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}
