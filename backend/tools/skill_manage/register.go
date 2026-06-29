package skillmanage

import (
	"context"
	"fmt"

	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/tools"
)

// Register adds the skill_manage tool to the runtime registry.
func Register(registry *tools.Registry) error {
	return RegisterWithMutation(registry, nil)
}

// RegisterWithMutation adds skill_manage with an instance-scoped callback.
func RegisterWithMutation(
	registry *tools.Registry,
	onMutation func(context.Context, skillstore.MutationKind, string, string),
) error {
	if registry == nil {
		return fmt.Errorf("tool registry is required")
	}
	tool, err := NewToolWithMutation(onMutation)
	if err != nil {
		return fmt.Errorf("skill_manage: %w", err)
	}
	return registry.Register(tool)
}
