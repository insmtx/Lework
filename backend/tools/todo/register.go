package todo

import (
	"fmt"

	"github.com/insmtx/Leros/backend/tools"
)

// NewTools returns all built-in runtime todo tools.
func NewTools() []tools.Tool {
	return []tools.Tool{
		NewTool(),
	}
}

// Register adds built-in runtime todo tools to the runtime registry.
func Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	for _, tool := range NewTools() {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}
