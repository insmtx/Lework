package worker

import (
	"os"
	"strconv"
	"time"

	"github.com/insmtx/Lework/backend/pkg/lework"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"
)

func StartParentWatcher() {
	rawPID := os.Getenv(lework.EnvParentPID)
	if rawPID == "" {
		return
	}

	parentPID, err := strconv.Atoi(rawPID)
	if err != nil {
		logs.Warnf("Invalid %s value %q: %v", lework.EnvParentPID, rawPID, err)
		return
	}

	go func() {
		time.Sleep(5 * time.Second)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if os.Getppid() != parentPID {
				logs.Infof("Parent process %d exited, worker terminating", parentPID)
				lifecycle.Std().Exit()
				return
			}
		}
	}()
}
