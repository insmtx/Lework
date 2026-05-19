package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines/builtin"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/externalcli"
	"github.com/insmtx/Leros/backend/internal/agent/leros"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/deps"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/lifecycle"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/ygpkg/yg-go/logs"
)

type Options struct {
	LLMConfig      *config.LLMConfig
	CLIConfig      *config.CLIEnginesConfig
	ToolsEnabled   bool
	DefaultRuntime string
}

type Service struct {
	env    *deps.Container
	router agent.Runner
}

func NewService(ctx context.Context, opts Options) (*Service, error) {
	env, err := deps.New(ctx, deps.Options{
		ToolsEnabled: opts.ToolsEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("create runtime dependencies: %w", err)
	}

	s := &Service{env: env}

	router, err := s.buildRouter(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("build runtime router: %w", err)
	}

	s.router = router
	return s, nil
}

func (s *Service) Router() agent.Runner {
	return s.router
}

// Run executes a request through the configured runtime router.
func (s *Service) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	if s == nil || s.router == nil {
		return nil, fmt.Errorf("agent runtime service is not initialized")
	}
	return s.router.Run(ctx, req)
}

func (s *Service) Environment() *deps.Container {
	return s.env
}

func (s *Service) buildRouter(ctx context.Context, opts Options) (agent.Runner, error) {
	lifecycleBuilder := lifecycle.NewContextBuilder(lifecycle.ContextBuilder{
		BaseSystemPrompt: leros.DefaultSystemPrompt(),
		Runtime:          s.env,
		SessionMessages:  lifecycle.NewDBSessionMessageProvider(infradb.GetDB(), 20),
	})
	router := lifecycle.NewRuntimeRouter(agent.RuntimeKindLeros, lifecycleBuilder, s.env)

	registered := 0
	registeredKinds := make(map[string]struct{})
	cliNames := []string{}

	logs.Info("Registering Leros agent runtime")
	lerosRunner, err := leros.NewRunner(ctx, s.env)
	if err != nil {
		return nil, err
	}
	if err := router.Register(agent.RuntimeKindLeros, lerosRunner); err != nil {
		return nil, err
	}
	registered++
	registeredKinds[agent.RuntimeKindLeros] = struct{}{}

	if opts.CLIConfig != nil {
		cliRegistry, err := builtin.NewRegistryFromConfig(opts.CLIConfig)
		if err != nil {
			return nil, fmt.Errorf("create CLI engine registry: %w", err)
		}
		cliNames = cliRegistry.Names()
		for _, name := range cliNames {
			engine, ok := cliRegistry.Get(name)
			if !ok {
				continue
			}
			runner, err := externalcli.NewRunner(name, engine)
			if err != nil {
				return nil, err
			}
			if db := infradb.GetDB(); db != nil {
				runner.SetSessionStore(externalcli.NewSessionMetadataProviderSessionStore(db))
			}
			if err := router.Register(name, runner); err != nil {
				return nil, err
			}
			registered++
			registeredKinds[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
			logs.Infof("Registering external agent CLI runtime: %s", name)
		}
	}

	if registered == 0 {
		return nil, fmt.Errorf("no agent runtime is available")
	}

	selectedDefault := s.selectDefaultRuntime(opts.DefaultRuntime, opts, cliNames)
	if selectedDefault == "" {
		selectedDefault = agent.RuntimeKindLeros
	}
	normalizedDefault := strings.ToLower(strings.TrimSpace(selectedDefault))
	if _, ok := registeredKinds[normalizedDefault]; !ok {
		return nil, fmt.Errorf("default agent runtime %q is not available", selectedDefault)
	}
	router.SetDefault(selectedDefault)

	return router, nil
}

var _ agent.Runner = (*Service)(nil)

func (s *Service) selectDefaultRuntime(defaultRuntime string, opts Options, cliNames []string) string {
	if strings.TrimSpace(defaultRuntime) != "" {
		return defaultRuntime
	}
	if opts.CLIConfig != nil && strings.TrimSpace(opts.CLIConfig.Default) != "" {
		return opts.CLIConfig.Default
	}
	return agent.RuntimeKindLeros
}
