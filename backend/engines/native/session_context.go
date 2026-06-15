package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/insmtx/Leros/backend/internal/api/contract"
)

// loadSessionContext shells out to the leros CLI to fetch persisted session
// messages from the server. Returns nil if sessionID is empty.
func loadSessionContext(ctx context.Context, sessionID string, page, perPage int) (*contract.MessageList, error) {
	if sessionID == "" {
		return nil, nil
	}

	lerosBin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find leros binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, lerosBin,
		"session", "messages", sessionID,
		"--page", fmt.Sprintf("%d", page),
		"--per-page", fmt.Sprintf("%d", perPage),
		"--json",
	)

	// Inherit worker process environment so CLI resolves
	// server_addr and auth_token from config/env.
	cmd.Env = os.Environ()

	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("leros session messages: %w", err)
	}

	var msgList contract.MessageList
	if err := json.Unmarshal(stdout, &msgList); err != nil {
		return nil, fmt.Errorf("parse session messages JSON: %w", err)
	}

	return &msgList, nil
}
