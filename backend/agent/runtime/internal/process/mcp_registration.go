package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
)

const mcpRegisterTimeout = 10 * time.Second
const lerosMCPTokenEnvVar = "LEROS_MCP_TOKEN"

// NormalizeMCPServerConfig fills defaults for an MCP server registration.
func NormalizeMCPServerConfig(cfg agent.MCPServerConfig) agent.MCPServerConfig {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "leros"
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.BearerToken = strings.TrimSpace(cfg.BearerToken)
	cfg.Command = strings.TrimSpace(cfg.Command)
	return cfg
}

// LerosMCPTokenEnvVar returns the env var name used for CLI MCP bearer token registration.
func LerosMCPTokenEnvVar() string {
	return lerosMCPTokenEnvVar
}

// RunCLICommand runs a CLI command with a bounded timeout.
func RunCLICommand(ctx context.Context, cliPath string, args []string, extraEnv []string) error {
	if strings.TrimSpace(cliPath) == "" {
		return fmt.Errorf("cli path is required")
	}
	execCtx, cancel := context.WithTimeout(ctx, mcpRegisterTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cliPath, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return execCtx.Err()
		}
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
