package skilluse

import (
	"fmt"

	"github.com/insmtx/Lework/backend/tools"
)

// Register registers all skill catalog tools into the provided registry.
// Tools dynamically scan the skills directory on each invocation — no cached state.
func Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is required")
	}

	return registry.Register(NewSkillUseTool())
}
