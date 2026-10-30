package scheduler

import (
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
)

const (
	modeProcess   = "process"
	modeDockerCLI = "docker-cli"
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
	default:
		return nil, fmt.Errorf("unsupported scheduler mode: %s", mode)
	}
}
