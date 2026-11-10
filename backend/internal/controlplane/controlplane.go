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
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/agentgw"
	"github.com/insmtx/lework/backend/internal/controlplane/api"
	"github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/controlplane/webui"
)

const (
	// readHeaderTimeout 限制读取请求头的耗时，避免慢连接长期占用处理协程。
	readHeaderTimeout = 5 * time.Second

	// shutdownTimeout 是优雅退出的上限；超时后强制关闭剩余连接。
	shutdownTimeout = 5 * time.Second
)

// healthResponse 是健康检查的响应体。
type healthResponse struct {
	Status string `json:"status"`
}

// Server 是控制面 HTTP 服务，持有装配好的处理器与运行期资源。
//
// 关闭是幂等的：第一次 Close 释放注册表、HTTP 服务器与存储，后续调用
// 直接返回成功，便于 defer 与显式关闭并存。
type Server struct {
	cfg      config.Config
	store    *store.Store
	registry *node.Registry
	hub      *event.Hub
	handler  http.Handler

	mu         sync.Mutex
	httpServer *http.Server
	closed     bool
}

// New 校验配置并装配控制面。存储由调用方打开并在 Close 时一并关闭。
func New(cfg config.Config, st *store.Store) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("controlplane: invalid config: %w", err)
	}
	if st == nil {
		return nil, errors.New("controlplane: store is required")
	}

	registry := node.NewRegistry()
	hub := event.NewHub()
	gateway := agentgw.New(agentgw.Config{
		EnrollmentToken:  cfg.EnrollmentToken,
		HeartbeatTimeout: cfg.HeartbeatTimeout,
	}, st, registry, hub)
	browserAPI := api.New(st, registry, hub, cfg.RequestTimeout)
	ui := webui.Handler()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("/api/agent/connect", gateway)
	browserAPI.Register(mux)
	mux.Handle("/", ui)

	return &Server{
		cfg:      cfg,
		store:    st,
		registry: registry,
		hub:      hub,
		handler:  mux,
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

	logs.Infof("control plane listening on %s", s.cfg.ListenAddr)

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
	s.mu.Unlock()

	var errs []error

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

// handleHealthz 返回固定的健康状态，用于启动探活与命令行自检。
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}
