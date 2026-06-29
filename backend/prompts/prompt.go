package prompts

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/valyala/fasttemplate"

	"github.com/insmtx/Leros/backend/config"
)

type Executor interface {
	Execute(ctx context.Context, prompt string, cfg config.LLMConfig) (string, error)
}

type Manager struct {
	mu        sync.RWMutex
	templates map[string]string
	executor  Executor
}

func New(executor Executor) *Manager {
	return &Manager{
		templates: make(map[string]string),
		executor:  executor,
	}
}

// SetExecutor sets the Executor for the Manager. It is not safe to call this concurrently with Run.
func (m *Manager) SetExecutor(exec Executor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executor = exec
}

func (m *Manager) Register(key, template string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates[key] = template
}

func (m *Manager) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.templates[key]
}

func (m *Manager) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.templates))
	for k := range m.templates {
		keys = append(keys, k)
	}
	return keys
}

func (m *Manager) runWithConfig(ctx context.Context, tpl string, params map[string]any, cfg config.LLMConfig) (string, error) {
	if m.executor == nil {
		return "", fmt.Errorf("prompts: executor not set on Manager")
	}

	rendered := fasttemplate.New(tpl, "{", "}").ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		v, ok := params[tag]
		if !ok {
			return w.Write([]byte("{" + tag + "}"))
		}
		return w.Write([]byte(fmt.Sprint(v)))
	})

	return m.executor.Execute(ctx, rendered, cfg)
}

func (m *Manager) Run(ctx context.Context, key string, params map[string]any, opts ...RunOption) (string, error) {
	tpl := m.Get(key)
	if tpl == "" {
		return "", fmt.Errorf("prompts: unknown key %q", key)
	}

	var cfg config.LLMConfig
	for _, o := range opts {
		o(ctx, &cfg)
	}

	return m.runWithConfig(ctx, tpl, params, cfg)
}

var (
	globalManager = &Manager{
		templates: make(map[string]string),
		executor:  NewEinoExecutor(),
	}
)

func SetDefaultExecutor(exec Executor) {
	m := globalManager
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executor = exec
}

func Register(key, template string) {
	globalManager.Register(key, template)
}

func Get(key string) string {
	return globalManager.Get(key)
}

func Keys() []string {
	return globalManager.Keys()
}

func Run(ctx context.Context, key string, params map[string]any, opts ...RunOption) (string, error) {
	m := globalManager
	m.mu.RLock()
	exec := m.executor
	m.mu.RUnlock()
	if exec == nil {
		return "", fmt.Errorf("prompts: default executor not set; call SetDefaultExecutor first")
	}

	tpl := m.Get(key)
	if tpl == "" {
		return "", fmt.Errorf("prompts: unknown key %q", key)
	}
	var cfg config.LLMConfig
	defaultLLMConfigOption(ctx, &cfg)
	for _, o := range opts {
		o(ctx, &cfg)
	}
	return m.runWithConfig(ctx, tpl, params, cfg)
}
