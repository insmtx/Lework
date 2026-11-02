package scheduler

import (
	"fmt"
	"strings"

	"github.com/insmtx/lework/backend/config"
	"github.com/insmtx/lework/backend/internal/worker"
)

const (
	modeProcess   = "process"
	modeDockerCLI = "docker-cli"
	modeK8s       = "k8s"
)

func New(cfg *config.SchedulerConfig) (worker.WorkerScheduler, error) {
	mode := modeProcess
	if cfg != nil && strings.TrimSpace(cfg.Mode) != "" {
		mode = strings.TrimSpace(cfg.Mode)
	}
	switch mode {
	case modeProcess:
		return NewProcessScheduler(cfg), nil
	case modeDockerCLI:
		return NewDockerCLIScheduler(cfg), nil
	case modeK8s:
		return NewKubernetesScheduler(cfg)
	default:
		return nil, fmt.Errorf("unsupported scheduler mode: %s", mode)
	}
}

func IsKubernetesMode(cfg *config.SchedulerConfig) bool {
	return cfg != nil && strings.TrimSpace(cfg.Mode) == modeK8s
}
