package skillmanage

import (
	"fmt"

	"github.com/insmtx/Leros/backend/tools"
)

// Register adds the skill_manage tool to the runtime registry.
func Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is required")
	}
	tool, err := NewTool()
	if err != nil {
		return fmt.Errorf("skill_manage: %w", err)
	}
	return registry.Register(tool)
}
