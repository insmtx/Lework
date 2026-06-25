// Package builtin 连接内置的外部 CLI 引擎适配器。
package builtin

import (
	"fmt"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/engines/claude"
	"github.com/insmtx/Leros/backend/engines/codex"
	"github.com/insmtx/Leros/backend/engines/native"
	"github.com/insmtx/Leros/backend/engines/opencode"
	"github.com/insmtx/Leros/backend/tools"
)

// NewRegistryFromConfig creates a registry with the native engine and every
// detected built-in CLI engine.
func NewRegistryFromConfig(cfg *config.CLIEnginesConfig, env *tools.Registry) (*engines.Registry, error) {
	registry := engines.NewRegistry()

	// Always register the native in-process engine.
	nativeEngine, err := native.NewAdapter(env)
	if err != nil {
		return nil, fmt.Errorf("create native engine: %w", err)
	}
	if err := registry.Register(native.EngineName, nativeEngine); err != nil {
		return nil, err
	}

	// Discover and register external CLI engines.
	for _, status := range engines.DiscoverAvailableCLI() {
		if !status.Installed {
			continue
		}
		engine, err := newEngine(status.Name, status.Path)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(status.Name, engine); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func newEngine(name string, path string) (engines.Engine, error) {
	switch name {
	case engines.EngineClaude:
		return claude.NewAdapter(path, nil), nil
	case engines.EngineCodex:
		return codex.NewAdapter(path, nil), nil
	case engines.EngineOpenCode:
		return opencode.NewAdapter(path, nil), nil
	default:
		return nil, fmt.Errorf("unsupported CLI engine %q", name)
	}
}
