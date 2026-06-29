package memory

import (
	"fmt"

	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/tools"
)

// NewTools returns all built-in memory tools.
func NewTools() []tools.Tool {
	return []tools.Tool{
		NewTool(),
	}
}

// Register adds built-in memory tools to the runtime registry.
func Register(registry *tools.Registry) error {
	store, err := localmemory.NewStore(localmemory.Options{})
	if err != nil {
		return err
	}
	return RegisterWithStore(registry, store)
}

// RegisterWithStore adds memory tools backed by the injected store.
func RegisterWithStore(registry *tools.Registry, store *localmemory.Store) error {
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	if store == nil {
		return fmt.Errorf("memory store is nil")
	}
	for _, tool := range []tools.Tool{NewToolWithStore(store)} {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}
