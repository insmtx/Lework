// Package controlplane 组装本地控制面的全部运行时组件。
//
// 控制面是面向单用户的本地服务：agent 通过 WebSocket 主动接入，浏览器通过
// 回环 HTTP API 管理节点。本包只负责把配置、存储、注册表、事件中心、
// agent 网关与浏览器 API 装配成一个 HTTP 处理器，并管理其启动与退出；
// 进程信号与生命周期属于 cmd/lework。
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/agentgw"
	"github.com/insmtx/lework/backend/internal/controlplane/api"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
	"github.com/insmtx/lework/backend/internal/controlplane/push"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/controlplane/webui"
	"github.com/insmtx/lework/backend/internal/infra/mq"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// readHeaderTimeout 限制读取请求头的耗时，避免慢连接长期占用处理协程。
	readHeaderTimeout = 5 * time.Second

	// shutdownTimeout 是优雅退出的上限；超时后强制关闭剩余连接。
	shutdownTimeout = 5 * time.Second

	// monitorBuffer 是总览订阅的缓冲。慢消费者被淘汰后索引落后，
	// 客户端重新拉一次快照即可，因此不需要无限缓冲。
	monitorBuffer = 256

	// snapshotInterval 是 owner 发布整节点快照的周期。新实例最迟在这个
	// 窗口之后收敛到与 owner 一致的视图。
	snapshotInterval = 10 * time.Second
)

// ClusterBus 是控制面需要的完整事件总线面：既要有 EventBus 的发布能力，
// 也要有 SubscribeBytes 供本地 Hub 桥接使用。
type ClusterBus interface {
	mq.EventBus
	mq.EventSubscriber
}

// Deps 汇总控制面依赖的集群设施。
//
// 生产装配全部指向同一个 NATS 客户端；进程内测试用 mqtest.Bus 填充五个
// 字段。拆成五个接口而不是一个具体类型，是为了让每个组件只依赖自己用到
// 的最小能力。
type Deps struct {
	// Bus 承载进入 JetStream 的节点状态事件。
	Bus ClusterBus
	// Core 发布不计入 stream 的易失数据（如 pty.output）。
	Core mq.CorePublisher
	// CoreSub 订阅跨实例转发请求。
	CoreSub mq.CoreSubscriber
	// Request 发起跨实例节点请求。
	Request mq.CoreRequesterBytes
	// Pull 消费集群内只处理一次的审批事件。
	Pull mq.PullSubscriber
}

// validate 校验五个依赖都已装配，错误文本点名缺失的字段。
func (d Deps) validate() error {
	if d.Bus == nil {
		return errors.New("controlplane: Deps.Bus is required")
	}
	if d.Core == nil {
		return errors.New("controlplane: Deps.Core is required")
	}
	if d.CoreSub == nil {
		return errors.New("controlplane: Deps.CoreSub is required")
	}
	if d.Request == nil {
		return errors.New("controlplane: Deps.Request is required")
	}
	if d.Pull == nil {
		return errors.New("controlplane: Deps.Pull is required")
	}
	return nil
}

// healthResponse 是健康检查的响应体。
type healthResponse struct {
	Status string `json:"status"`
}

// Server 是控制面 HTTP 服务，持有装配好的处理器与运行期资源。
//
// 关闭是幂等的：第一次 Close 释放注册表、HTTP 服务器与存储，后续调用
// 直接返回成功，便于 defer 与显式关闭并存。
type Server struct {
	cfg       config.Config
	store     *store.Store
	registry  *node.Registry
	hub       *event.Hub
	index     *overview.Index
	notifier  *push.Notifier
	publisher *event.Publisher
	bridge    *event.Bridge
	live      *event.LiveSubscriber
	deps      Deps
	handler   http.Handler

	monitorCancel func()

	mu         sync.Mutex
	httpServer *http.Server
	closed     bool
}

// New 校验配置与依赖并装配控制面。存储由调用方打开并在 Close 时一并关闭。
func New(cfg config.Config, st *store.Store, deps Deps) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("controlplane: invalid config: %w", err)
	}
	if st == nil {
		return nil, errors.New("controlplane: store is required")
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}

	registry := node.NewRegistry()
	hub := event.NewHub()
	index := overview.NewIndex()
	publisher := event.NewPublisher(deps.Bus, deps.Core)
	bridge := event.NewBridge(deps.Bus, hub)
	live := event.NewLiveSubscriber(hub)
	resolver := authz.Resolver{
		Store:           st,
		EnrollmentToken: cfg.EnrollmentToken,
		LocalAdmin:      cfg.LocalAdmin,
		SessionTTL:      cfg.SessionTTL,
	}
	gateway := agentgw.New(agentgw.Config{
		EnrollmentToken:  cfg.EnrollmentToken,
		HeartbeatTimeout: cfg.HeartbeatTimeout,
	}, st, registry, publisher, deps.CoreSub)
	notifier := push.NewNotifier(st, push.NewExpoSender(nil), func(nodeID string) string {
		if info, ok := registry.Get(nodeID); ok && info.Name != "" {
			return info.Name
		}
		return nodeID
	}, push.WithSessionTitle(index.Title))
	browserAPI := api.New(
		st,
		registry,
		hub,
		index,
		node.NewRouter(registry, deps.Request),
		deps.CoreSub,
		deps.Core,
		resolver,
		api.AuthConfig{SessionTTL: cfg.SessionTTL, RegistrationMode: cfg.RegistrationMode},
		api.ClaimConfig{TTL: cfg.ClaimTTL, CodeLength: cfg.ClaimCodeLength},
		cfg.RequestTimeout,
		cfg.HeartbeatTimeout,
	)
	ui := webui.Handler()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("/api/agent/connect", gateway)
	browserAPI.Register(mux)
	mux.Handle("/", ui)

	return &Server{
		cfg:       cfg,
		store:     st,
		registry:  registry,
		hub:       hub,
		index:     index,
		notifier:  notifier,
		publisher: publisher,
		bridge:    bridge,
		live:      live,
		deps:      deps,
		handler:   newAuthMiddleware(mux, resolver),
	}, nil
}

// Handler 返回可直接交给 http.Server 或 httptest 的处理器。
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run 启动 HTTP 服务并阻塞，直到监听失败或 ctx 被取消。
//
// 监听地址会写入日志，注册令牌绝不出现在日志中。ctx 取消后会先取消所有
// 请求上下文（常驻的 SSE 流因此能及时退出），再使用 5 秒超时优雅退出；
// http.ErrServerClosed 属于正常退出，不作为错误返回。
func (s *Server) Run(ctx context.Context) error {
	// Shutdown 只等待处理函数返回，不会取消请求上下文。浏览器会常驻
	// /api/events，因此必须把服务级上下文挂到 BaseContext，退出时主动取消，
	// 否则正常退出也会一直等到超时。
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return runCtx },
	}

	s.mu.Lock()
	s.httpServer = srv
	s.mu.Unlock()

	s.startMonitor(runCtx)
	// 审批推送走集群消费组：多实例共享一条消费流，同一条事件只推一次。
	go func() {
		if err := s.notifier.Run(runCtx, s.deps.Pull); err != nil && runCtx.Err() == nil {
			logs.Errorf("controlplane: push notifier stopped: %v", err)
		}
	}()
	// 心跳超时的节点可能来不及发断开事件，由对账补充 node.offline。
	go NewReconciler(s.store, s.publisher, s.cfg.HeartbeatTimeout).Run(runCtx)
	// 事件只从集群总线回到本地 Hub：节点网关发布到 Bus，桥接再扇出给浏览器。
	go s.bridge.Run(runCtx)
	// 增量事件走 Core NATS：每个实例订阅 cp.live.> 后投递给本地 Hub。
	go func() {
		if err := s.live.Run(runCtx, s.deps.CoreSub); err != nil && runCtx.Err() == nil {
			logs.Errorf("controlplane: live event subscriber stopped: %v", err)
		}
	}()
	go s.publishSnapshots(runCtx)
	// 移除节点可能发生在另一个实例：本实例只负责关掉自己持有的连接。
	go s.startNodeCloseSubscriber(runCtx)

	logs.Infof("control plane listening on %s", s.cfg.ListenAddr)
	if !config.IsLoopbackAddr(s.cfg.ListenAddr) {
		logs.Warnf("control plane is listening on non-loopback address %s; "+
			"browser API requires an account login (or an admin Bearer token)", s.cfg.ListenAddr)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("controlplane: serve %s: %w", s.cfg.ListenAddr, err)
		}
		return nil
	case <-ctx.Done():
		// 显式取消，避免依赖父上下文传播的异步延迟。
		cancelRun()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			// 残留连接未在超时内退出属于正常退出路径：强制关闭并继续。
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				logs.Warnf("control plane shutdown timed out, closing remaining connections")
				_ = srv.Close()
			} else {
				_ = srv.Close()
				return fmt.Errorf("controlplane: shutdown: %w", err)
			}
		}

		// Shutdown 返回后 ListenAndServe 必然以 ErrServerClosed 结束，
		// 取走错误避免协程悬挂。
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("controlplane: serve %s: %w", s.cfg.ListenAddr, err)
		}
		return nil
	}
}

// Close 关闭全部节点连接、HTTP 服务器与存储，可重复调用。
//
// 第一次调用返回释放过程中遇到的错误（合并上报）；之后的调用直接成功。
func (s *Server) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	srv := s.httpServer
	cancelMonitor := s.monitorCancel
	s.mu.Unlock()

	var errs []error

	if cancelMonitor != nil {
		cancelMonitor()
	}

	s.registry.CloseAll()

	if srv != nil {
		if err := srv.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("controlplane: close http server: %w", err))
		}
	}
	if err := s.store.Close(); err != nil {
		errs = append(errs, fmt.Errorf("controlplane: close store: %w", err))
	}

	return errors.Join(errs...)
}

// eventSubscriber 是事件中心的订阅面；抽成接口让重新订阅路径可测。
type eventSubscriber interface {
	Subscribe(buffer int) (<-chan event.Event, func())
}

// hubSubscriber 把 *event.Hub 适配为只订阅状态事件的内部接口。
//
// 总览不需要增量事件，因此这里刻意不暴露 WithLive；测试仍可用更小的
// 假实现替换 startMonitorWith 的订阅面。
type hubSubscriber struct{ hub *event.Hub }

func (s hubSubscriber) Subscribe(buffer int) (<-chan event.Event, func()) {
	return s.hub.Subscribe(buffer)
}

// startMonitor 把事件中心接到总览索引。
func (s *Server) startMonitor(ctx context.Context) {
	s.startMonitorWith(ctx, hubSubscriber{hub: s.hub})
}

// startMonitorWith 订阅事件并持续投影到索引。
//
// Hub 遇到慢消费者会关闭通道而不是阻塞发布方，因此这里被关闭后必须重新
// 订阅：否则一次跟不上就会让总览停在旧状态直到进程重启。
func (s *Server) startMonitorWith(ctx context.Context, sub eventSubscriber) {
	events, cancel := sub.Subscribe(monitorBuffer)
	s.setMonitorCancel(cancel)

	go func() {
		for {
			s.consumeIndex(ctx, events)
			if ctx.Err() != nil {
				return
			}
			logs.Warnf("controlplane: overview subscription lost, resubscribing")
			events, cancel = sub.Subscribe(monitorBuffer)
			s.setMonitorCancel(cancel)
		}
	}()
}

// consumeIndex 处理一个订阅通道，直到它被关闭或 ctx 取消。
func (s *Server) consumeIndex(ctx context.Context, events <-chan event.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.index.Apply(ev)
		}
	}
}

// setMonitorCancel 记录当前订阅的取消函数，供 Close 释放。
func (s *Server) setMonitorCancel(cancel func()) {
	s.mu.Lock()
	s.monitorCancel = cancel
	s.mu.Unlock()
}

// snapshotPayload 是 node.snapshot 的负载；语义是整节点替换。
type snapshotPayload struct {
	Sessions []overview.Session `json:"sessions"`
}

// startNodeCloseSubscriber 订阅跨实例的「关闭节点连接」通知。
//
// 每个实例都订阅同一 wildcard，处理器只关心本实例是否持有该连接；没有则
// 什么都不做。消息体是节点 ID，而不是从 subject 反解——handler 拿不到
// subject，payload 才是唯一可靠的信息来源。
func (s *Server) startNodeCloseSubscriber(ctx context.Context) {
	if s.deps.CoreSub == nil {
		return
	}
	subject := messaging.ControlPlaneNodeCloseWildcard()
	for ctx.Err() == nil {
		err := s.deps.CoreSub.SubscribeCore(ctx, subject, "", func(_ context.Context, payload []byte) []byte {
			handleNodeCloseMessage(s.registry, payload)
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logs.Warnf("controlplane: node close subscriber stopped: %v; retrying", err)
		} else {
			logs.Warnf("controlplane: node close subscriber returned without error; retrying")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// handleNodeCloseMessage 关闭本实例持有的节点连接；ID 不合法或没有连接都忽略。
func handleNodeCloseMessage(registry *node.Registry, payload []byte) {
	nodeID := strings.TrimSpace(string(payload))
	if registry == nil || nodeID == "" {
		return
	}
	conn, ok := registry.Connection(nodeID)
	if !ok {
		return
	}
	if err := conn.Close(); err != nil {
		logs.Warnf("controlplane: close node %q connection: %v", nodeID, err)
	}
}

// publishSnapshots 周期性地把本地节点的投影发布到集群总线。
//
// owner 之外的实例没有原始事件流，只能靠快照收敛；因此每轮 tick 都为
// registry 里的每个节点发布一次，进程退出时随 runCtx 一起停止。
func (s *Server) publishSnapshots(ctx context.Context) {
	ticker := time.NewTicker(snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.publishSnapshotOnce(ctx)
		}
	}
}

// publishSnapshotOnce 发布一轮快照。单次失败只记日志：下一轮会重发，
// 不能因为一次网络抖动让所有实例永久停留在旧视图。
//
// 快照必须携带生成它时的 index watermark：如果实时事件尚未应用到索引，
// 快照不能反过来把它覆盖掉。索引还没有水位时使用本进程 owner + seq 0，
// 表示“未知 epoch”：已有状态的接收方会忽略它，全新实例可以接受。
func (s *Server) publishSnapshotOnce(ctx context.Context) {
	for _, info := range s.registry.List() {
		sessions, owner, seq := s.index.SnapshotNodeWithWatermark(info.ID)
		if owner == "" {
			owner = s.publisher.Owner()
			seq = 0
		}
		data, err := json.Marshal(snapshotPayload{Sessions: sessions})
		if err != nil {
			logs.Warnf("controlplane: marshal snapshot for node %s: %v", info.ID, err)
			continue
		}
		ev := event.Event{
			NodeID:     info.ID,
			Type:       overview.SnapshotEvent,
			Time:       time.Now(),
			Data:       data,
			Owner:      owner,
			ClusterSeq: seq,
		}
		if err := s.publisher.PublishSnapshot(ctx, ev); err != nil {
			logs.Warnf("controlplane: publish snapshot for node %s: %v", info.ID, err)
		}
	}
}

// handleHealthz 返回固定的健康状态，用于启动探活与命令行自检。
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}
