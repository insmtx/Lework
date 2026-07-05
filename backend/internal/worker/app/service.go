package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	clauderuntime "github.com/insmtx/Leros/backend/agent/runtime/claude"
	codexruntime "github.com/insmtx/Leros/backend/agent/runtime/codex"
	nativeruntime "github.com/insmtx/Leros/backend/agent/runtime/native"
	opencoderuntime "github.com/insmtx/Leros/backend/agent/runtime/opencode"
	"github.com/insmtx/Leros/backend/config"
	runtimecli "github.com/insmtx/Leros/backend/internal/cli"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	skilllinks "github.com/insmtx/Leros/backend/internal/skill/links"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/worker/agentrun/context"
	"github.com/insmtx/Leros/backend/internal/worker/runtimehost"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/tools"
	artifactdeclare "github.com/insmtx/Leros/backend/tools/artifact_declare"
	memorytools "github.com/insmtx/Leros/backend/tools/memory"
	nodetools "github.com/insmtx/Leros/backend/tools/node"
	skillmanagetools "github.com/insmtx/Leros/backend/tools/skill_manage"
	skillusetools "github.com/insmtx/Leros/backend/tools/skill_use"
	todotools "github.com/insmtx/Leros/backend/tools/todo"
	"github.com/ygpkg/yg-go/logs"
)

type Options struct {
	LLMConfig         *config.LLMConfig
	CLIConfig         *config.CLIEnginesConfig
	DefaultRuntime    string
	CLISkillDirs      []string
	GiteaCfg          *config.GiteaConfig
	Env               string
	InteractionRouter *runtimehost.InteractionRouter
	ModelStore        *modelrouter.ModelStore
	MemoryStore       *localmemory.Store
	SessionDBPath     string
	ServerAddr        string
	OrgID             uint
	AuthToken         string
}

// Service is the Worker composition root.
// It constructs the full agent.Executor pipeline and the agentrun.Service pipeline.
type Service struct {
	env          *tools.Registry
	agentRunSvc  *agentrun.Service
	sessionStore *runtimehost.SQLiteSessionStore
}

func NewService(ctx context.Context, opts Options) (*Service, error) {
	env := tools.NewRegistry()
	if err := registerTools(env, opts.CLISkillDirs, opts.MemoryStore); err != nil {
		return nil, fmt.Errorf("register runtime tools: %w", err)
	}
	logs.Infof("Loaded %d tools for runtime", len(env.List()))

	// Build context builder.
	lifecycleBuilder := lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		SessionMessages: lifecyclecontext.NewPassthroughSessionMessageProvider(),
		Memory:          opts.MemoryStore,
	})

	runtimes := make(map[string]agent.Runtime)
	registeredKinds := make(map[string]struct{})

	nativeRuntime, err := nativeruntime.New()
	if err != nil {
		return nil, err
	}
	runtimes[nativeruntime.Kind] = nativeRuntime
	registeredKinds[nativeruntime.Kind] = struct{}{}
	logs.Infof("Registering agent runtime: %s", nativeruntime.Kind)

	cliNames := make([]string, 0, 3)
	for _, status := range runtimecli.DiscoverAvailableCLI() {
		if !status.Installed {
			continue
		}
		if opts.InteractionRouter == nil {
			return nil, fmt.Errorf("interaction router is required for runtime %q", status.Name)
		}
		normalized := normalizeRuntimeKind(status.Name)
		runtime, err := newRuntime(normalized, status.Path, agent.RuntimeAdapterOptions{
			InteractionHandler: opts.InteractionRouter,
			MCPServers:         buildMCPServersFromConfig(opts.CLIConfig),
		})
		if err != nil {
			return nil, err
		}
		runtimes[normalized] = runtime
		registeredKinds[normalized] = struct{}{}
		cliNames = append(cliNames, normalized)
		logs.Infof("Registering agent runtime: %s", normalized)
	}

	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no agent runtime is available")
	}

	// Select default runtime.
	selectedDefault := selectDefaultRuntime(opts.DefaultRuntime, opts, cliNames)
	if selectedDefault == "" {
		selectedDefault = agent.RuntimeKindLeros
	}
	normalizedDefault := normalizeRuntimeKind(selectedDefault)
	if _, ok := registeredKinds[normalizedDefault]; !ok {
		return nil, fmt.Errorf("default agent runtime %q is not available", selectedDefault)
	}
	// Build new architecture pipeline with workspace/attachment ports.
	var wm agentrun.WorkspaceManager
	var ai agentrun.AttachmentIngestor
	if opts.GiteaCfg != nil && opts.GiteaCfg.Enabled {
		wm = agentrun.NewWorkspaceManager(opts.Env, opts.GiteaCfg.Endpoint, opts.GiteaCfg.Owner, opts.GiteaCfg.AccessToken)
	}
	ai = agentrun.NewAttachmentIngestor()
	if opts.ModelStore == nil {
		return nil, fmt.Errorf("model store is required")
	}
	preparer := agentrun.NewPreparerWithTools(
		lifecycleBuilder,
		wm,
		ai,
		opts.ModelStore,
		agentrun.NewToolProvider(env),
	)
	finalizer := agentrun.NewFinalizer()
	journalFactory := agentrun.NewJournalFactory()

	// Build the agent-level Executor and Registry.
	agentRegistry := agent.NewRegistry()
	agentRegistry.SetDefault(normalizedDefault)
	for kind, rt := range runtimes {
		agentRegistry.Register(kind, rt)
	}
	executor := agent.NewExecutor(agentRegistry)

	planPublisher := agentrun.NewPlanPublisher(agentrun.PlanPublisherConfig{
		ServerAddr: opts.ServerAddr,
		OrgID:      opts.OrgID,
		AuthToken:  opts.AuthToken,
	})

	// Provider session store for external CLI resume support.
	var sessionStore agentrun.ProviderSessionStore
	var sqliteSessionStore *runtimehost.SQLiteSessionStore
	if strings.TrimSpace(opts.SessionDBPath) != "" {
		ss, err := runtimehost.NewSQLiteSessionStore(opts.SessionDBPath)
		if err != nil {
			return nil, fmt.Errorf("create session store: %w", err)
		}
		sessionStore = ss
		sqliteSessionStore = ss
		preparer = agentrun.NewPreparerWithSessionStore(
			lifecycleBuilder,
			wm,
			ai,
			opts.ModelStore,
			agentrun.NewToolProvider(env),
			sessionStore,
		)
	}

	s := &Service{
		env: env,
		agentRunSvc: agentrun.NewServiceWithSessionStore(
			preparer,
			executor,
			finalizer,
			journalFactory,
			planPublisher,
			sessionStore,
		),
		sessionStore: sqliteSessionStore,
	}

	return s, nil
}

// Close releases Worker-host resources created by the composition root.
func (s *Service) Close() error {
	if s == nil || s.sessionStore == nil {
		return nil
	}
	return s.sessionStore.Close()
}

func newRuntime(
	kind string,
	binary string,
	options agent.RuntimeAdapterOptions,
) (agent.Runtime, error) {
	kind = normalizeRuntimeKind(kind)
	dataDir, err := externalCLIDataDir(kind)
	if err != nil {
		return nil, err
	}
	switch kind {
	case clauderuntime.Kind:
		return clauderuntime.New(binary, options, dataDir)
	case codexruntime.Kind:
		return codexruntime.New(binary, options, dataDir)
	case opencoderuntime.Kind:
		return opencoderuntime.New(binary, options, dataDir)
	default:
		return nil, fmt.Errorf("unsupported runtime %q", kind)
	}
}

// AgentRunService returns the agentrun.Service.
func (s *Service) AgentRunService() *agentrun.Service {
	return s.agentRunSvc
}

// Environment returns the tool registry.
func (s *Service) Environment() *tools.Registry {
	return s.env
}

func selectDefaultRuntime(defaultRuntime string, opts Options, cliNames []string) string {
	if strings.TrimSpace(defaultRuntime) != "" {
		return defaultRuntime
	}
	if opts.CLIConfig != nil && strings.TrimSpace(opts.CLIConfig.Default) != "" {
		return opts.CLIConfig.Default
	}
	return agent.RuntimeKindLeros
}

func normalizeRuntimeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func externalCLIDataDir(kind string) (string, error) {
	normalized := normalizeRuntimeKind(kind)
	if normalized == "" {
		return "", fmt.Errorf("runtime kind is required")
	}
	return leros.JoinWorkspace("." + normalized)
}

func buildMCPServersFromConfig(cliCfg *config.CLIEnginesConfig) []agent.MCPServerConfig {
	if cliCfg == nil || cliCfg.MCP == nil {
		return nil
	}
	cfg := agent.MCPServerConfig{
		Name:        "leros",
		URL:         cliCfg.MCP.URL,
		BearerToken: cliCfg.MCP.BearerToken,
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.BearerToken = strings.TrimSpace(cfg.BearerToken)
	if cfg.URL == "" {
		return nil
	}
	return []agent.MCPServerConfig{cfg}
}

func registerTools(registry *tools.Registry, cliSkillDirs []string, memoryStore *localmemory.Store) error {
	if err := registry.Register(artifactdeclare.NewTool()); err != nil {
		return fmt.Errorf("register artifact declare tool: %w", err)
	}
	if err := skillusetools.Register(registry); err != nil {
		return fmt.Errorf("register skill use tool: %w", err)
	}
	onSkillMutation := func(ctx context.Context, kind skillstore.MutationKind, name, action string) {
		if len(cliSkillDirs) > 0 {
			switch kind {
			case skillstore.MutationCreate:
				_ = skilllinks.EnsureExternalSkillLink(name, cliSkillDirs)
			case skillstore.MutationDelete:
				_ = skilllinks.RemoveExternalSkillLink(name, cliSkillDirs)
			}
		}
	}

	if err := skillmanagetools.RegisterWithMutation(registry, onSkillMutation); err != nil {
		return fmt.Errorf("register skill manage tool: %w", err)
	}
	if err := memorytools.RegisterWithStore(registry, memoryStore); err != nil {
		return fmt.Errorf("register memory tool: %w", err)
	}
	if err := todotools.Register(registry); err != nil {
		return fmt.Errorf("register todo tool: %w", err)
	}
	if err := nodetools.Register(registry); err != nil {
		return fmt.Errorf("register node tools: %w", err)
	}
	return nil
}
