package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/ygpkg/yg-go/logs"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	"github.com/insmtx/lework/backend/internal/agentd/credential"
	"github.com/insmtx/lework/backend/internal/agentd/handler"
	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/pair"
	"github.com/insmtx/lework/backend/internal/agentd/runner"
	"github.com/insmtx/lework/backend/internal/agentd/servicefile"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
)

const (
	// initialBackoff and maxBackoff bound the reconnect delay: 1s, 2s, 4s, ...
	// capped at 30s so a control-plane outage is retried promptly at first and
	// cheaply once it lasts.
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second

	// backoffResetAfter is the stability window a connection must survive before
	// a later drop restarts the retry ladder. Without it, a control plane that
	// accepts then immediately drops would keep the agent at a 1s retry cadence.
	backoffResetAfter = 10 * time.Second

	// heartbeatInterval matches the control plane's idle expectation.
	heartbeatInterval = 10 * time.Second

	// shutdownTimeout bounds supervisor.Stop so a wedged child cannot block the
	// agent's exit forever.
	shutdownTimeout = 10 * time.Second
)

// agentConnectOptions 是 `lework agent` 的连接期选项。零值保持旧行为：读凭证
// 文件，没有凭证时用 enrollment token 直通。
type agentConnectOptions struct {
	// CredentialPath 覆盖凭证文件位置（flag > config > 派生默认）。
	CredentialPath string
	// Code 非空时用网页出的一次性短码直接配对（`--code`）。
	Code string
	// NoQR 关闭终端二维码，只打印链接与短码。
	NoQR bool
	// Reset 表示启动前删除本地凭证与待定配对文件，强制重新配对。
	// 它只影响本地文件与本轮策略，不做任何远端写入。
	Reset bool
}

// validate 拒绝互相矛盾的来源：--code 要新建一次配对，--credential-file 指向
// 一份已经存在的凭证，两者同时生效会让「凭证优先」与「显式配对」打架。
func (o agentConnectOptions) validate() error {
	if strings.TrimSpace(o.Code) != "" && strings.TrimSpace(o.CredentialPath) != "" {
		return errors.New("--code and --credential-file are mutually exclusive")
	}
	return nil
}

// agentRuntime 是已装配但尚未连接的节点 agent，供独立 agent 与进程内组合
// 启动（bare lework）复用同一套构造/运行/关闭流程。
type agentRuntime struct {
	cfg      *agentdconfig.Config
	client   *opencode.Client
	service  *supervisor.Supervisor
	handler  *handler.Handler
	password string
	// serviceURL 是 client 实际使用的端点。附着用户共享服务时它不等于
	// 配置里的 loopback 端口，发布给 `lework opencode` 的也必须是它。
	serviceURL string
	// opts 是独立的 `lework agent` 传入的连接选项；组合启动保持零值，行为
	// 与引入配对之前完全一致。
	opts agentConnectOptions
}

// agentEndpoint 是本机 OpenCode 服务的选择结果。
//
// Attach 为 true 表示服务由用户启动（`opencode serve --service`），agent
// 只借用它的 HTTP/SSE 接口：不能 spawn、不能 stop、不能改它的注册文件。
type agentEndpoint struct {
	URL      string
	Password string
	// Env 只用于私有服务的子进程；附着共享服务时为空。
	Env    []string
	Attach bool
}

// 共享服务可能正在开机启动，给它一个有限的等待窗口；超过就回退到私有
// 服务，避免 agent 永远卡在一个不会出现的端点上。
const (
	sharedOpenCodeTimeout        = 5 * time.Second
	sharedOpenCodePoll           = 250 * time.Millisecond
	sharedOpenCodeRequestTimeout = 2 * time.Second
)

// resolveAgentEndpoint 优先借用用户已经运行的 OpenCode 共享服务。
//
// OpenCode 的执行状态与事件只存在于进程内：TUI 连的是用户自己的服务，
// agent 自持的 `serve` 看不到它的会话状态。附着同一个服务后，事件流里
// 才有 TUI 的会话；同时避免在同一数据库上再起第二个 server。
//
// 发现失败一律回退到旧行为（自持 serve + 独立密码），不回退成“无服务”。
func resolveAgentEndpoint(ctx context.Context, cfg *agentdconfig.Config) (agentEndpoint, error) {
	shared, err := servicefile.ReadOpenCode()
	switch {
	case err == nil:
		if probeErr := probeSharedOpenCode(ctx, shared); probeErr == nil {
			logs.Infof("lework-agent: attached to shared opencode service at %s", shared.URL)
			return agentEndpoint{URL: shared.URL, Password: shared.Password, Attach: true}, nil
		} else {
			logs.Warnf("lework-agent: shared opencode service at %s is unavailable, starting a private service: %s",
				shared.URL, redact(probeErr))
		}
	case !errors.Is(err, fs.ErrNotExist):
		logs.Warnf("lework-agent: read shared opencode service: %s", redact(err))
	}

	// The password is shared by the client and the child process. It is derived
	// or generated once here so a generated value reaches opencode through its
	// environment and never has to be read back from the server output.
	env, password, err := openCodeEnv(cfg.OpenCode.Env)
	if err != nil {
		return agentEndpoint{}, err
	}
	return agentEndpoint{
		URL:      fmt.Sprintf("http://127.0.0.1:%d", cfg.OpenCode.Port),
		Password: password,
		Env:      envSlice(env),
	}, nil
}

// probeSharedOpenCode 在有限窗口内等待注册的共享服务可用。
//
// 401 立即失败：密码不对等再久也不会自愈；其余错误按开机竞态重试。
func probeSharedOpenCode(ctx context.Context, endpoint servicefile.OpenCodeEndpoint) error {
	client := opencode.New(
		endpoint.URL,
		&http.Client{Timeout: sharedOpenCodeRequestTimeout},
		opencode.WithPassword(endpoint.Password),
	)
	deadline := time.Now().Add(sharedOpenCodeTimeout)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, sharedOpenCodeRequestTimeout)
		err := client.Health(probeCtx)
		cancel()

		if err == nil {
			return nil
		}
		if errors.Is(err, opencode.ErrUnauthorized) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sharedOpenCodePoll):
		}
	}
}

// buildAgent 解析 OpenCode 环境并装配客户端、监管器与请求处理器。
// 它不连控制面，也不主动启动子进程（服务探测除外）。
func buildAgent(ctx context.Context, cfg *agentdconfig.Config) (*agentRuntime, error) {
	endpoint, err := resolveAgentEndpoint(ctx, cfg)
	if err != nil {
		return nil, err
	}

	client := opencode.New(
		endpoint.URL,
		http.DefaultClient,
		opencode.WithPassword(endpoint.Password),
	)
	service := supervisor.New(client, nil, supervisor.ServiceConfig{
		Binary:     cfg.OpenCode.Binary,
		Port:       cfg.OpenCode.Port,
		Env:        endpoint.Env,
		Dir:        "",
		BaseURL:    endpoint.URL,
		AttachOnly: endpoint.Attach,
	})
	requestHandler := handler.New(client, service, handler.WithWorkspaceRoots(cfg.WorkspaceRoots...))

	return &agentRuntime{
		cfg:        cfg,
		client:     client,
		service:    service,
		handler:    requestHandler,
		password:   endpoint.Password,
		serviceURL: endpoint.URL,
	}, nil
}

// Run 启动本地 OpenCode 服务并连接控制面，阻塞到 ctx 取消。
// 信号处理由调用方负责，这里不得注册 signal。
func (a *agentRuntime) Run(ctx context.Context) error {
	if a == nil || a.cfg == nil || a.client == nil || a.service == nil || a.handler == nil {
		return errors.New("agent runtime is not initialized")
	}

	// Publish before Ensure so a concurrently started `lework opencode` can
	// wait for the service instead of reporting a missing endpoint. A write
	// failure is not fatal: the control plane path does not depend on it.
	a.publishService()

	// Bootstrap the local service once before dialing. The runner subscribes to
	// OpenCode events immediately after welcome, so without a running service
	// every connection would die at subscribe time and reconnect before the
	// control plane could send opencode.start. A failure here is not fatal: the
	// control plane may still start the service later via opencode.start, and
	// the agent can register and report the failure.
	if _, err := a.service.Ensure(ctx); err != nil {
		if ctx.Err() == nil {
			logs.Warnf("lework-agent: start opencode service: %s", redact(err, a.cfg.EnrollmentToken))
		}
	} else {
		logs.Infof("lework-agent: opencode service ready")
	}

	logs.Infof("lework-agent: starting node %q", a.cfg.NodeName)

	return reconnect(ctx, a.cfg, a.client, a.handler, a.opts)
}

// Close 停止 agent 托管的 OpenCode 子进程，避免 daemon 退出后留下孤儿进程。
// 可重复调用：supervisor.Stop 在没有受管子进程时是空操作。
func (a *agentRuntime) Close() error {
	if a == nil || a.cfg == nil || a.service == nil {
		return nil
	}
	stopService(a.service, a.cfg.EnrollmentToken)
	a.unpublishService()
	return nil
}

// publishService records the agent-owned endpoint for `lework opencode`. The
// file carries the password, so it is written 0600 by servicefile. Failure is
// logged but never fatal: the node can still serve the control plane without a
// local TUI.
func (a *agentRuntime) publishService() {
	if a == nil || a.cfg == nil {
		return
	}
	port := 0
	if parsed, err := url.Parse(a.serviceURL); err == nil {
		port, _ = strconv.Atoi(parsed.Port())
	}
	info := servicefile.Info{
		URL:      a.serviceURL,
		Password: a.password,
		Port:     port,
		PID:      os.Getpid(),
	}
	if err := servicefile.Write(info); err != nil {
		logs.Warnf("lework-agent: publish opencode service endpoint: %s",
			redact(err, a.cfg.EnrollmentToken))
	}
}

// unpublishService removes the endpoint only when this process wrote it. A
// second agent on the same node must not lose its own record because this one
// happened to shut down first.
func (a *agentRuntime) unpublishService() {
	current, err := servicefile.Read()
	if err != nil || current.PID != os.Getpid() {
		return
	}
	if err := servicefile.Remove(); err != nil {
		logs.Warnf("lework-agent: remove opencode service endpoint: %s",
			redact(err, a.cfg.EnrollmentToken))
	}
}

// runAgent wires the daemon and runs it until ctx ends. Config errors are
// returned before any network or process work happens, which keeps a broken
// configuration from leaving a half-started node behind.
func runAgent(ctx context.Context, configPath string, opts agentConnectOptions) error {
	cfg, err := loadAgentConfig(configPath)
	if err != nil {
		return err
	}
	// agent.yaml 没写 workspace_roots 时沿用组合模式的默认目录；
	// 显式配置的目录不受影响。
	if err := applyDefaultWorkspaceRoots(cfg); err != nil {
		return fmt.Errorf("%w (set workspace_roots in %s)", err, configPath)
	}
	// agentdconfig.Load fills defaults but deliberately does not validate; the
	// daemon must reject an incomplete file before dialing.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid agent config %s: %w", configPath, err)
	}
	// 凭证路径优先级：--credential-file > credential_file > 与配置文件同目录
	// 的默认值。一次配置、一处运维；flag 让容器/Ansible 能显式指定挂载点。
	opts.CredentialPath = resolveCredentialPath(opts.CredentialPath, configPath, cfg.CredentialFile)
	cfg.CredentialFile = opts.CredentialPath

	if opts.Reset {
		if err := resetCredentialState(opts.CredentialPath); err != nil {
			return err
		}
		logs.Infof("lework-agent: --reset removed local pairing state at %s", opts.CredentialPath)
	}

	runtime, err := buildAgent(ctx, cfg)
	if err != nil {
		return err
	}
	runtime.opts = opts

	// SIGINT/SIGTERM cancel the run loop; the deferred Close below turns the
	// cancellation into a clean child-process shutdown.
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// The shared OpenCode service outlives individual connections but not the
	// agent: stopping it here prevents an orphan when the daemon exits.
	defer func() { _ = runtime.Close() }()

	return runtime.Run(ctx)
}

// resolveCredentialPath 按 flag > config > 派生默认的顺序解析凭证路径。
//
// 显式 flag 也走 filepath.Abs：运维在 systemd/容器里写的相对路径以启动目录
// 为准，保存凭证与派生 claim 路径都必须落在同一个确定的绝对位置。
// 纯函数便于测试：真实路径解析涉及 home 目录与工作目录，混在 runAgent 里
// 无法稳定断言。
func resolveCredentialPath(flagPath, configPath, configValue string) string {
	if path := strings.TrimSpace(flagPath); path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
		return path
	}
	if path := strings.TrimSpace(configValue); path != "" {
		return path
	}
	return credential.DefaultPath(configPath)
}

// resolveAgentCredentialPath 为不读业务配置的维护命令（claim）解析凭证路径。
//
// 配置文件读不到时只丢给派生默认值：claim 的职责是重印一张短码，不该因为
// agent.yaml 暂时不可读而拒绝工作。
func resolveAgentCredentialPath(configPath, flagPath string) string {
	configValue := ""
	if cfg, err := loadAgentConfig(configPath); err == nil {
		configValue = cfg.CredentialFile
	}
	return resolveCredentialPath(flagPath, configPath, configValue)
}

// reconnect dials the control plane, runs one connection, and retries with
// exponential backoff until ctx is canceled. A nil error means a clean shutdown.
//
// 配对计划只在进入循环前计算一次：`--code` 成功落盘后，下一轮保存下来的凭证
// 必须按「已有凭证」正常拨号，不能被同一份 `--code` 再次判成冲突。循环内仍然
// 每轮重新读凭证，只用于决定拨号用哪个 Bearer（凭证优先，token 兜底）。
//
// 没有凭证时的策略顺序（设计 §7）：显式 `--code` 先跑一次账号出码配对；否则
// 有 enrollment token 就沿用旧直通；两者都没有才进入终端扫码配对。旧部署只要
// 配了 enrollment_token，行为与引入配对之前完全一致。
func reconnect(
	ctx context.Context,
	cfg *agentdconfig.Config,
	client *opencode.Client,
	requestHandler *handler.Handler,
	opts agentConnectOptions,
) error {
	_, source, hasCredential, err := findCredential(cfg.CredentialFile)
	if err != nil {
		return fmt.Errorf("lework-agent: 读取节点凭证 %s 失败：%w", cfg.CredentialFile, err)
	}
	plan, err := planPairing(hasCredential, source, cfg.CredentialFile, opts.Code, cfg.EnrollmentToken, opts.Reset)
	if err != nil {
		return err
	}

	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}

		// 凭证读取错误必须显式失败，不能降级成 enrollment token：那等于把节点
		// 从凭证模式静默切成 admin 直通。这里不拨号，只退避后重试读取。
		nodeCredential, _, roundHasCredential, err := findCredential(cfg.CredentialFile)
		if err != nil {
			logs.Errorf("lework-agent: 读取节点凭证 %s 失败：%s（不会回落到 enrollment token；修复文件路径/权限后重启 agent）",
				cfg.CredentialFile, redact(err, cfg.EnrollmentToken))
			if waitErr := waitBackoff(ctx, backoff); waitErr != nil {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}

		// 本轮已有凭证（刚配对落盘或别的进程写入）时，一次性配对计划必须收敛：
		// 否则凭证日后丢失会拿一张已消费的 --code 再试。
		if roundHasCredential {
			plan = pairPlanCredentialOnly
		}

		switch decidePairing(plan, roundHasCredential) {
		case pairingDecisionCode:
			// --code 的码一次性、机器不在你面前：失败就退出，不能悄悄回落到
			// 二维码（扫不到也重印不了）。
			if _, pairErr := pairWithCode(ctx, cfg, opts); pairErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return pairErr
			}
			plan = pairPlanCredentialOnly
			// 重新读一次刚落盘的凭证再拨号，确保 Bearer 与文件一致。
			continue
		case pairingDecisionInteractive:
			if _, pairErr := pairInteractive(ctx, cfg, opts); pairErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return pairErr
			}
			plan = pairPlanCredentialOnly
			continue
		}

		conn, err := transport.Dial(ctx, transport.DialConfig{
			URL:            cfg.ControlPlaneURL,
			TLSFingerprint: cfg.TLSFingerprint,
			Header:         controlPlaneHeader(nodeCredential, cfg.EnrollmentToken),
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if roundHasCredential && credentialRejected(err) {
				// 不删除凭证、不回落旧令牌、不自动转配对：自动清凭证重配对会让
				// 一台机器静默换到别人的账号下。提示之后继续退避重试；带上底层
				// 状态行（401 Unauthorized）便于确认是凭证被拒而不是网络问题。
				logs.Errorf("lework-agent: 节点凭证被拒绝，请重新配对（删除控制面里的该节点后重新加入）：%s",
					redact(err, cfg.EnrollmentToken, nodeCredential))
			} else {
				logs.Warnf("lework-agent: connect control plane: %s", redact(err, cfg.EnrollmentToken, nodeCredential))
			}
		} else {
			logs.Infof("lework-agent: connected to control plane")
			started := time.Now()
			runCfg := runner.RunnerConfig{
				// NodeID 是占位值：welcome 帧会带回控制面分配的 node_...，runner
				// 在转发事件前切换。NodeName 仅用于展示，节点身份是凭证。
				NodeID:         cfg.NodeName,
				NodeName:       cfg.NodeName,
				AgentVersion:   version,
				Heartbeat:      heartbeatInterval,
				WorkspaceRoots: cfg.WorkspaceRoots,
			}
			if cfg.CredentialFile != "" {
				runCfg.CredentialSink = func(issued string) error {
					if err := credential.Save(cfg.CredentialFile, issued); err != nil {
						return err
					}
					// 路径不是秘密：记下来便于确认凭证落到了哪里。
					logs.Infof("lework-agent: 已保存节点凭证到 %s", cfg.CredentialFile)
					return nil
				}
			}
			runErr := runner.New(conn, requestHandler, client, runCfg).Run(ctx)
			connectedFor := time.Since(started)

			if closeErr := conn.Close(); closeErr != nil {
				logs.Warnf("lework-agent: close connection: %s", redact(closeErr, cfg.EnrollmentToken, nodeCredential))
			}
			if ctx.Err() != nil || runErr == nil {
				return nil
			}
			// 凭证写不下时重试没有意义：控制面已登记 hash，旧令牌每轮都会新建
			// 一个节点。直接退出，让运维修好路径/权限再重新加入。
			if fatalRunError(runErr) {
				logs.Errorf("lework-agent: 控制面已为该节点登记凭证，但本地写入 %s 失败：%s；"+
					"修复路径/权限后删除控制面里的该节点并重新配对",
					cfg.CredentialFile, redact(runErr, cfg.EnrollmentToken, nodeCredential))
				return fmt.Errorf("lework-agent: node credential is not persisted: %w", runErr)
			}
			logs.Warnf("lework-agent: connection ended: %s", redact(runErr, cfg.EnrollmentToken, nodeCredential))

			// Only a connection that stayed up long enough earns a fresh
			// ladder. A connect-then-drop loop keeps doubling the delay.
			if shouldResetBackoff(connectedFor) {
				backoff = initialBackoff
			}
		}

		if err := waitBackoff(ctx, backoff); err != nil {
			return nil
		}
		backoff = nextBackoff(backoff)
	}
}

// nodeCredentialEnv 是凭证文件缺失时的凭证来源，供容器与 Ansible 注入。
//
// 它只在内存里使用：不写回磁盘、不进入日志或错误文本。存在文件时文件优先，
// 避免一个遗留的环境变量悄悄覆盖运维刚配好的文件。
const nodeCredentialEnv = "LEWORK_NODE_CREDENTIAL"

// credentialSource 记录凭证是从哪里读到的，用于在 --code 冲突时点名来源。
type credentialSource string

const (
	credentialSourceFile credentialSource = "file"
	credentialSourceEnv  credentialSource = "env"
)

// conflictError 把「已有凭证又要 --code」翻译成可执行的错误。
//
// 文件场景给出路径、env 场景给出变量名，并附带清理动作：只说「已有凭证」
// 用户不知道去哪删。env 分支永远不打印变量的值。
func (s credentialSource) conflictError(path string) error {
	switch s {
	case credentialSourceFile:
		return fmt.Errorf("--code given but a node credential already exists at %s; delete %s or unset %s and retry",
			path, path, nodeCredentialEnv)
	case credentialSourceEnv:
		return fmt.Errorf("--code given but %s is set; unset %s and retry", nodeCredentialEnv, nodeCredentialEnv)
	default:
		return fmt.Errorf("--code given but a node credential already exists; delete it or unset %s and retry", nodeCredentialEnv)
	}
}

// findCredential 按文件 > 环境变量的顺序寻找节点凭证。
//
// 文件优先：一个遗留的环境变量不该悄悄覆盖运维刚配好的文件。文件读取错误
// （而不是「文件不存在」）原样返回，调用方必须把它当成凭证模式失败，不能
// 降级成 enrollment token 直通。
func findCredential(path string) (raw string, source credentialSource, found bool, err error) {
	nodeCredential, hasCredential, err := credential.Load(path)
	if err != nil {
		return "", "", false, err
	}
	if hasCredential {
		return nodeCredential, credentialSourceFile, true, nil
	}
	if fromEnv := strings.TrimSpace(os.Getenv(nodeCredentialEnv)); fromEnv != "" {
		return fromEnv, credentialSourceEnv, true, nil
	}
	return "", "", false, nil
}

// pairPlan 是进程启动时确定的一次性配对策略。它必须在进入 reconnect 循环前
// 计算一次：--code 配对成功后保存的凭证，下一轮不能再被同一份 --code 判成
// 「已有凭证」冲突。
type pairPlan int

const (
	// pairPlanCredentialOnly 只用已找到的凭证/令牌拨号，永不配对。
	pairPlanCredentialOnly pairPlan = iota
	// pairPlanCode 首次拨号前用 --code 配对一次，成功后转为 CredentialOnly。
	pairPlanCode
	// pairPlanLegacy 没有凭证但有 enrollment token，沿用 admin 直通。
	pairPlanLegacy
	// pairPlanInteractive 没有凭证也没有令牌，进入终端扫码配对。
	pairPlanInteractive
)

// planPairing 把启动时的「凭证来源 + --code + enrollment token」映射成策略。
//
// 纯函数：这是用户最容易踩的优先级，测试要能逐条覆盖而不用真的拨号。
// 已有凭证时 --code 是错误而不是覆盖：静默覆盖会让一台机器悄悄换账号。
func planPairing(
	hasCredential bool,
	source credentialSource,
	credentialPath, code, enrollmentToken string,
	reset bool,
) (pairPlan, error) {
	if hasCredential {
		if strings.TrimSpace(code) != "" {
			return pairPlanCredentialOnly, fmt.Errorf("lework-agent: %w", source.conflictError(credentialPath))
		}
		return pairPlanCredentialOnly, nil
	}
	if strings.TrimSpace(code) != "" {
		return pairPlanCode, nil
	}
	// --reset 明确要求「重新配对」：这一轮忽略 enrollment token，否则
	// 「没有凭证 + 有 token」会走旧直通，登记成未认领节点，手机根本看不到。
	if !reset && strings.TrimSpace(enrollmentToken) != "" {
		return pairPlanLegacy, nil
	}
	return pairPlanInteractive, nil
}

// resetCredentialState 删除节点凭证与待定配对文件，让下一次拨号走配对。
//
// 环境变量无法删除，所以 LEWORK_NODE_CREDENTIAL 存在时直接失败而不是
// 「删了文件又用 env 里的旧凭证连回去」——那只会让用户以为重置没生效。
func resetCredentialState(credentialPath string) error {
	if strings.TrimSpace(os.Getenv(nodeCredentialEnv)) != "" {
		return fmt.Errorf("--reset cannot remove the credential from %s; unset it and retry", nodeCredentialEnv)
	}
	path := strings.TrimSpace(credentialPath)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove node credential %s: %w", path, err)
	}
	claimPath := pair.ClaimPath(path)
	if claimPath == "" {
		return nil
	}
	if err := os.Remove(claimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pairing claim %s: %w", claimPath, err)
	}
	return nil
}

// pairingDecision 是当前轮在拨号前要做的事。
type pairingDecision int

const (
	pairingDecisionDial pairingDecision = iota
	pairingDecisionCode
	pairingDecisionInteractive
)

// decidePairing 把启动计划与「本轮是否已有凭证」映射成当前动作。
//
// 纯函数：--code 成功后本轮已落盘凭证，即使计划仍残留 Code 也必须改为拨号，
// 这正是「配对成功后第二轮不再报冲突」的守卫。
func decidePairing(plan pairPlan, hasCredential bool) pairingDecision {
	if hasCredential {
		return pairingDecisionDial
	}
	switch plan {
	case pairPlanCode:
		return pairingDecisionCode
	case pairPlanInteractive:
		return pairingDecisionInteractive
	default:
		return pairingDecisionDial
	}
}

// dialPairConnection 建立一条待定连接：Pair 头只是这条连接的标识，不是凭证。
func dialPairConnection(ctx context.Context, cfg *agentdconfig.Config, pairKey string) (*transport.Client, error) {
	header := http.Header{}
	header.Set("Authorization", "Pair "+pairKey)
	return transport.Dial(ctx, transport.DialConfig{
		URL:            cfg.ControlPlaneURL,
		TLSFingerprint: cfg.TLSFingerprint,
		Header:         header,
	})
}

// pairWithCode 用网页签发的短码完成一次配对，成功即落盘凭证。
//
// 单次尝试、错误向上抛：码是一次性的，机器不在操作者面前，二维码救不了场；
// 重试只会撞限流并把真正原因埋在退避日志里。
func pairWithCode(ctx context.Context, cfg *agentdconfig.Config, opts agentConnectOptions) (string, error) {
	pairKey, err := pair.NewPairKey()
	if err != nil {
		return "", fmt.Errorf("lework-agent: %w", err)
	}
	conn, err := dialPairConnection(ctx, cfg, pairKey)
	if err != nil {
		// 码与 pair_key 都不在 dial 错误里：URL 已脱敏，握手错误只有状态行。
		return "", fmt.Errorf("lework-agent: 使用 --code 配对失败：%s：%w", pairFailureGuidance(err), err)
	}
	defer func() { _ = conn.Close() }()

	raw, pairErr := pair.Run(ctx, conn, pair.Options{
		NodeName:        cfg.NodeName,
		AgentVersion:    version,
		WorkspaceRoots:  cfg.WorkspaceRoots,
		PairKey:         pairKey,
		Code:            strings.TrimSpace(opts.Code),
		ControlPlaneURL: cfg.ControlPlaneURL,
	})
	if pairErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// 同上：pair.Run 的错误只来自关闭原因与协议校验，不含码或凭证。
		return "", fmt.Errorf("lework-agent: 使用 --code 配对失败：%s：%w", pairFailureGuidance(pairErr), pairErr)
	}
	if err := credential.Save(cfg.CredentialFile, raw); err != nil {
		return "", fmt.Errorf("lework-agent: 保存节点凭证到 %s 失败：%w", cfg.CredentialFile, err)
	}
	// --code 模式不写 claim 文件；清掉上次崩溃遗留的旧码，避免 `lework agent
	// claim` 重印一张已经无效的配对信息。仍在使用中的文件属于另一个待定 agent，
	// 不能删。
	removeDeadClaimFile(pair.ClaimPath(cfg.CredentialFile))
	logs.Infof("lework-agent: 使用 --code 配对成功，已保存节点凭证到 %s", cfg.CredentialFile)
	return raw, nil
}

// errPairCredentialSave 表示配对已成功但凭证落盘失败。目录状态不会因为换一张
// 码而变好，重试只会把刚成功的配对反复作废，因此与 claim 写失败同级处理。
var errPairCredentialSave = errors.New("persist paired node credential")

// pairAttempt 是 pairRetry 的一次完整尝试：给定本次的 pair_key，返回凭证或错误。
type pairAttempt func(ctx context.Context, pairKey string) (string, error)

// pairInteractive 在终端展示二维码/短码并等待账号认领。
//
// 每次尝试都生成新的 pair_key：过期或被接管的认领不该被复活，否则终端会
// 一直显示一张已经不可能生效的码。网络/控制面错误按既有退避阶梯重试，
// 直到拿到凭证或 ctx 取消。
//
// 无论成功、失败还是 ctx 取消，返回前都 best-effort 删除 claim 文件：它只在
// 本进程仍维持待定连接时才有意义，留着会让 `lework agent claim` 重印一张
// 已经作废的码。
func pairInteractive(ctx context.Context, cfg *agentdconfig.Config, opts agentConnectOptions) (string, error) {
	claimPath := pair.ClaimPath(cfg.CredentialFile)
	// 记录最后一次尝试的 pair_key：只有文件仍属于本次尝试时才清理，避免误删
	// 另一个共用同一路径的 agent 刚写的码。defer 读取变量本身而不是当时的值。
	var lastPairKey string
	defer func() { removeClaimFile(claimPath, lastPairKey) }()

	raw, err := pairRetry(ctx, pair.NewPairKey, func(ctx context.Context, pairKey string) (string, error) {
		// 换 key 意味着上一轮的码已经作废：先按上一轮的 key 清掉，否则这轮在
		// 写出新码之前就失败/被取消时，终端会一直重印一张不可能生效的旧码。
		// 只清归属于本进程上一轮尝试的文件，避免误删另一个 agent 刚写的码。
		if lastPairKey != "" && lastPairKey != pairKey {
			removeClaimFile(claimPath, lastPairKey)
		}
		lastPairKey = pairKey
		return pairOnce(ctx, cfg, opts, pairKey, claimPath)
	}, waitBackoff)
	if err == nil {
		return raw, nil
	}

	// 两类错误重试没有任何意义：同一个目录/节点状态不会自己变好。显式退出，
	// 把修复动作写进日志，避免无限换码。
	switch {
	case errors.Is(err, pair.ErrClaimWrite):
		logs.Errorf("lework-agent: 无法写入配对信息 %s：%s；修复目录权限后重试",
			claimPath, redact(err))
		return "", fmt.Errorf("lework-agent: 无法写入配对信息 %s，修复目录权限后重试：%w", claimPath, err)
	case errors.Is(err, pair.ErrCredentialAlreadyIssued):
		return "", fmt.Errorf("lework-agent: 该节点已有凭证，请删除控制面里的该节点后重新配对：%w", err)
	default:
		return "", err
	}
}

// pairRetry 负责换新码与退避，把「一次尝试」与「怎么等」留给调用方注入。
//
// 纯逻辑（不含真实拨号）便于测试：每次尝试必须拿到不同的 pair_key，且失败
// 后按既有阶梯等待，而不是在同一张码上重试。
func pairRetry(
	ctx context.Context,
	newKey func() (string, error),
	attempt pairAttempt,
	wait func(context.Context, time.Duration) error,
) (string, error) {
	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		pairKey, err := newKey()
		if err != nil {
			return "", fmt.Errorf("lework-agent: %w", err)
		}

		raw, err := attempt(ctx, pairKey)
		if err == nil {
			return raw, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if fatalPairError(err) {
			return "", err
		}

		logs.Warnf("lework-agent: 配对未完成：%s", redact(err))
		if waitErr := wait(ctx, backoff); waitErr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", waitErr
		}
		backoff = nextBackoff(backoff)
	}
}

// pairOnce 建立一条待定连接、跑完一次配对，成功后落盘凭证。
func pairOnce(ctx context.Context, cfg *agentdconfig.Config, opts agentConnectOptions, pairKey, claimPath string) (string, error) {
	conn, err := dialPairConnection(ctx, cfg, pairKey)
	if err != nil {
		return "", fmt.Errorf("lework-agent: 连接配对端点失败：%w", err)
	}

	raw, pairErr := pair.Run(ctx, conn, pair.Options{
		NodeName:        cfg.NodeName,
		AgentVersion:    version,
		WorkspaceRoots:  cfg.WorkspaceRoots,
		PairKey:         pairKey,
		ClaimPath:       claimPath,
		ControlPlaneURL: cfg.ControlPlaneURL,
		OnPending: func(code, link string) {
			printPendingPair(os.Stdout, code, link, claimPath, opts.NoQR)
		},
	})
	_ = conn.Close()

	if pairErr != nil {
		return "", pairErr
	}
	if err := credential.Save(cfg.CredentialFile, raw); err != nil {
		return "", fmt.Errorf("lework-agent: 保存节点凭证到 %s 失败：%w: %w", cfg.CredentialFile, errPairCredentialSave, err)
	}
	logs.Infof("lework-agent: 配对成功，已保存节点凭证到 %s", cfg.CredentialFile)
	return raw, nil
}

// removeClaimFile 删除本进程某次尝试写下的 claim 文件。文件本来就不存在是
// 正常情况（重复触发、上一轮已清理），其余删除失败只记警告：清理是 best-effort，
// 不该让一次成功的配对因为删文件而变成失败。
//
// 两个 agent 进程可能共用同一路径，因此先读 pair_key：只有文件仍属于该次尝试
// 才删，避免误删对方刚写的码。
func removeClaimFile(path, pairKey string) {
	if strings.TrimSpace(path) == "" {
		return
	}

	claim, _, err := pair.ReadClaim(path)
	if err == nil && claim.PairKey != pairKey {
		return
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logs.Warnf("lework-agent: 删除配对信息 %s 失败：%s", path, redact(err))
	}
}

// removeDeadClaimFile 只在 claim 文件已经不可用时删除（不存在、过期或不可解析）。
//
// `--code` 配对不写 claim 文件，因此不能无条件删除：同一目录下另一个仍在待定
// 连接中的 agent 可能正用这张码。只有「读出来是死码」才值得清掉，避免
// `lework agent claim` 重印一张已经无效的配对信息。
func removeDeadClaimFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if _, usable, err := pair.ReadClaim(path); err == nil && usable {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logs.Warnf("lework-agent: 删除配对信息 %s 失败：%s", path, redact(err))
	}
}

// fatalPairError reports whether a pairing error must stop the retry loop.
//
// 只有三种：目录写不下（同一目录随后还要写凭证）、节点已有凭证（换码也不会
// 生效）、以及配对成功但凭证落盘失败。其余断线/过期都是可重试的。
func fatalPairError(err error) bool {
	return errors.Is(err, pair.ErrClaimWrite) ||
		errors.Is(err, pair.ErrCredentialAlreadyIssued) ||
		errors.Is(err, errPairCredentialSave)
}

// printPendingPair 在终端打印配对二维码、链接与短码。
//
// 非 TTY 时三者都不打印：重定向到日志的二维码没有意义，短码写进日志反而扩大
// 泄露面。此时只提示 claim 文件的位置——终端不在了还能靠它重印。
func printPendingPair(out io.Writer, code, link, claimPath string, noQR bool) {
	if !isTerminal(out) {
		fmt.Fprintf(out, "Pairing information written to %s; run `lework agent claim` to reprint it.\n", claimPath)
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Pair this node with your account:")
	if !noQR {
		qrterminal.GenerateWithConfig(link, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     out,
			HalfBlocks: true,
		})
	}
	fmt.Fprintln(out, link)
	fmt.Fprintf(out, "Code: %s\n", code)
	fmt.Fprintln(out, "Run `lework agent claim` to reprint this code if the terminal scrolls away.")
	fmt.Fprintln(out)
}

// pairFailureGuidance 把配对哨兵映射成可执行的用户提示。
//
// 纯函数：提示文本直接打到用户终端，测试要能逐条确认它不含凭证占位符。
func pairFailureGuidance(err error) string {
	switch {
	case errors.Is(err, pair.ErrPairingExpired):
		return "配对码已过期，请在网页重新生成一张码后再执行 lework agent --code"
	case errors.Is(err, pair.ErrCredentialAlreadyIssued):
		return "该节点已有凭证，请先在控制面删除该节点，再重新配对"
	case errors.Is(err, pair.ErrClaimCodeUnavailable):
		return "配对码无效、已过期或已被使用，请在网页重新生成一张码"
	case errors.Is(err, pair.ErrClaimAttemptsExceeded):
		return "配对码尝试次数过多，请稍等一分钟后再试，并使用最新的一张码"
	case errors.Is(err, pair.ErrPairConnReplaced):
		return "本次配对连接已被另一个连接接管，请重新执行 lework agent --code"
	default:
		return "配对失败，请检查控制面地址与网络后重试"
	}
}

// credentialRejected reports whether a dial failure means the control plane
// rejected the node credential (401). Other statuses and network failures keep
// the quiet backoff path: only a rejected credential deserves the "re-pair"
// warning, and 401 deliberately does not distinguish unknown from revoked.
func credentialRejected(err error) bool {
	var handshake *transport.HandshakeError
	return errors.As(err, &handshake) && handshake.StatusCode == http.StatusUnauthorized
}

// fatalRunError reports whether a connection error must stop the reconnect loop
// instead of backing off.
//
// 唯一的情形是凭证落盘失败：控制面在 welcome 之前就写入了 credential_hash，
// 旧令牌路径不会复用带凭证的节点，因此继续重试只会每轮新建一个节点并再签发
// 一份本机写不下的凭证。修复办法是运维干预，不是重试。
func fatalRunError(err error) bool {
	return errors.Is(err, runner.ErrCredentialPersist)
}

// controlPlaneHeader carries the node identity in the WebSocket handshake.
//
// 凭证优先：节点身份是控制面签发的凭证，enrollment token 只是旧部署的 admin
// 直通。没有凭证时才回落到旧令牌；两者都没有时不带鉴权头。
func controlPlaneHeader(nodeCredential, enrollmentToken string) http.Header {
	token := nodeCredential
	if token == "" {
		token = enrollmentToken
	}
	if token == "" {
		return nil
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	return header
}

// redact removes every known secret from an error message before it is logged.
// Transport errors already drop userinfo, but a proxy or a mistyped config can
// copy the token into a URL, so this is defense in depth.
func redact(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

// stopService terminates a managed OpenCode process within shutdownTimeout.
// It uses a fresh context because the run context is already canceled by the
// time this deferred call executes.
func stopService(service *supervisor.Supervisor, enrollmentToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := service.Stop(ctx); err != nil {
		logs.Warnf("lework-agent: stop opencode service: %s", redact(err, enrollmentToken))
	}
}

// shouldResetBackoff reports whether a connection lived long enough to count as
// healthy. Short-lived connections keep climbing the ladder so a control plane
// that accepts and immediately drops cannot pin the agent at 1s retries.
func shouldResetBackoff(connectedFor time.Duration) bool {
	return connectedFor >= backoffResetAfter
}

// nextBackoff doubles current up to maxBackoff. A non-positive current starts
// the ladder at initialBackoff.
func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return initialBackoff
	}
	next := current * 2
	if next <= 0 || next > maxBackoff {
		return maxBackoff
	}
	return next
}

// waitBackoff sleeps for d or until ctx is canceled, whichever comes first.
func waitBackoff(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// openCodeEnv copies the configured environment, resolves the shared service
// password, and records a generated value so the client and the child process
// agree on it. The input map is never modified: it belongs to the loaded
// configuration, and mutating it would leak the password into a value other
// callers can read.
func openCodeEnv(cfgEnv map[string]string) (map[string]string, string, error) {
	env := make(map[string]string, len(cfgEnv)+1)
	for key, value := range cfgEnv {
		env[key] = value
	}
	// An empty yaml value means "unset", not "clear": the child inherits the
	// daemon's environment, and mergeEnv would let a copied empty key override
	// a non-empty inherited password with "". Dropping the empty entries keeps
	// the effective inherited value and stops an empty key reaching the child.
	for _, key := range []string{"OPENCODE_PASSWORD", "OPENCODE_SERVER_PASSWORD"} {
		if env[key] == "" {
			delete(env, key)
		}
	}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		return nil, "", err
	}
	if generated {
		// Reuse the persisted password so an agent restart keeps the endpoint
		// usable by a TUI that still holds the previous value. A failure here
		// is not fatal: the agent can still serve with a fresh password.
		saved, err := servicefile.LoadOrCreatePassword(password)
		if err != nil {
			logs.Warnf("lework-agent: persist opencode password: %s", redact(err))
		} else {
			password = saved
		}
		env["OPENCODE_SERVER_PASSWORD"] = password
	}
	return env, password, nil
}

// resolveOpenCodePassword returns the password the local server and client
// must share, and whether the agent generated it.
//
// The child does not run with only the yaml env: supervisor.mergeEnv starts
// from os.Environ() and lets spec.Env override individual keys, so a value
// inherited from systemd Environment= or a shell export is still visible to
// OpenCode. OpenCode itself checks OPENCODE_PASSWORD before
// OPENCODE_SERVER_PASSWORD. The effective value for each key is therefore a
// non-empty yaml override when present, otherwise the inherited process value.
// Looking at the yaml map alone would generate a fresh password that the child
// never uses (because it still sees an inherited OPENCODE_PASSWORD), producing
// a permanent 401 and a 30s health timeout. Generated passwords are 32 random
// bytes in unpadded base64url, which is safe inside an environment value and a
// Basic Auth header.
func resolveOpenCodePassword(env map[string]string) (password string, generated bool, err error) {
	effectivePassword := env["OPENCODE_PASSWORD"]
	if effectivePassword == "" {
		effectivePassword = os.Getenv("OPENCODE_PASSWORD")
	}
	effectiveServerPassword := env["OPENCODE_SERVER_PASSWORD"]
	if effectiveServerPassword == "" {
		effectiveServerPassword = os.Getenv("OPENCODE_SERVER_PASSWORD")
	}

	if effectivePassword != "" {
		return effectivePassword, false, nil
	}
	if effectiveServerPassword != "" {
		return effectiveServerPassword, false, nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generate opencode password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), true, nil
}

// envSlice converts the configured OpenCode environment into the KEY=VALUE form
// the supervisor passes to the child. Sorting makes the result deterministic so
// repeated runs launch an identical process specification.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}
