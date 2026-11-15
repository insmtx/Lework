// Package runner keeps one control-plane connection alive: it registers the
// agent, answers requests through the handler, emits OpenCode events, and sends
// heartbeats. It owns no process lifecycle; callers dial, run, and close.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/handler"
	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

// defaultHeartbeat matches the control plane's idle expectation. It is applied
// by New so a zero RunnerConfig still produces a usable runner.
const defaultHeartbeat = 10 * time.Second

// ErrCredentialPersist marks a handshake that failed because the one-shot node
// credential could not be written locally.
//
// 调用方必须把它当成终止条件，而不是可重试的连接错误：控制面在发出 welcome
// 前已经写入了 credential_hash，而旧令牌路径只复用「没有凭证、没有成员」的
// 节点——继续用旧令牌重连只会每轮新建一个节点并再签发一份写不下的凭证。
var ErrCredentialPersist = errors.New("persist node credential")

// responseSizeCheckThreshold 让精确的帧大小检查只对可能接近上限的响应生效。
//
// 编码后的 JSON 会把 <、>、& 转义成 6 字节，所以原始 result 长度不能代表
// 线上大小；小响应不值得为此多序列化一次。
const responseSizeCheckThreshold = 1 << 20

// Transport is the frame-level connection to the control plane. It must support
// one concurrent reader and one concurrent writer; the runner serializes writes
// itself. *transport.Client satisfies it.
type Transport interface {
	Write(ctx context.Context, f protocol.Frame) error
	Read(ctx context.Context) (protocol.Frame, error)
	Close() error
}

// EventSource supplies the OpenCode event feed. *opencode.Client satisfies it.
type EventSource interface {
	SubscribeEvents(ctx context.Context) (<-chan opencode.Event, error)
}

// RequestHandler is the handler surface the runner drives. It is an interface
// so tests can supply a deterministic PTY event source without a real agent
// process; *handler.Handler is the production implementation.
type RequestHandler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, error)
	PTYEvents() <-chan protocol.Event
	ClosePTYs()
}

// RunnerConfig describes the identity and liveness cadence of this agent.
type RunnerConfig struct {
	// NodeID is stamped on every forwarded event so the control plane can
	// attribute it to a node. The control plane may override it during the
	// handshake with the ID it assigns.
	NodeID string
	// NodeName is the human-readable label reported in hello and shown in the
	// browser. It is display-only: node identity is the credential the control
	// plane issued, and the node ID it assigns during the handshake, never the
	// name. Two machines may report the same name; the control plane renames
	// the newcomer instead of reusing an existing record.
	NodeName string
	// AgentVersion is reported during registration.
	AgentVersion string
	// Heartbeat is the interval between heartbeat frames. Zero selects the
	// 10s default.
	Heartbeat time.Duration
	// WorkspaceRoots are reported in hello so the browser can offer a default
	// project directory that session.create will accept.
	WorkspaceRoots []string
	// CredentialSink persists a one-shot node credential the control plane may
	// hand back in the welcome result. It runs before the connection proceeds;
	// a sink failure fails the handshake because a credential the agent never
	// wrote is a credential nobody holds, while the control plane has already
	// stored its hash — continuing would leave an unusable node record.
	//
	// A nil sink is allowed: tests and embedded callers that do not manage a
	// credential file simply ignore the field. The runner passes the raw
	// credential only to the sink and never logs it or puts it in an error.
	CredentialSink func(raw string) error
}

// Runner drives one connection. It is single-use: a Runner is created for each
// dial attempt, so no state leaks across reconnects.
type Runner struct {
	transport Transport
	handler   RequestHandler
	events    EventSource
	cfg       RunnerConfig

	// writeMu keeps heartbeat and event writes from interleaving on a transport
	// that permits only one concurrent writer.
	writeMu sync.Mutex
}

// New builds a runner for one connection attempt.
func New(transport Transport, requestHandler RequestHandler, events EventSource, cfg RunnerConfig) *Runner {
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeat
	}
	return &Runner{
		transport: transport,
		handler:   requestHandler,
		events:    events,
		cfg:       cfg,
	}
}

// Run performs the registration handshake and then serves requests, heartbeats,
// and events until ctx is canceled or the connection fails. A canceled context
// is a clean shutdown and returns nil; connection and event-stream failures are
// returned wrapped so the caller can reconnect with backoff.
//
// Run never logs request params or prompt text.
func (r *Runner) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	if err := r.sendHello(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if err := r.awaitWelcome(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	// Every worker shares this context so returning from Run stops them.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if r.handler != nil {
		defer r.handler.ClosePTYs()
	}

	frames, readErrs := r.readFrames(runCtx)
	workerErrs := make(chan error, 3)
	go func() { workerErrs <- r.heartbeat(runCtx) }()
	if r.events != nil {
		go func() { workerErrs <- r.forwardEvents(runCtx) }()
	}
	if r.handler != nil {
		go func() { workerErrs <- r.forwardPTYEvents(runCtx) }()
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-readErrs:
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("agentd: read frame: %w", err)

		case err := <-workerErrs:
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				// Only context cancellation makes a worker return nil, which is
				// already handled by the ctx.Done case.
				return nil
			}
			return err

		case frame := <-frames:
			if err := r.dispatch(runCtx, frame); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// sendHello registers this agent. Capabilities mirror the methods the handler
// dispatches so the control plane can hide features before calling them.
func (r *Runner) sendHello(ctx context.Context) error {
	params, err := json.Marshal(struct {
		NodeName       string   `json:"node_name"`
		AgentVersion   string   `json:"agent_version"`
		OS             string   `json:"os"`
		Arch           string   `json:"arch"`
		Capabilities   []string `json:"capabilities"`
		WorkspaceRoots []string `json:"workspace_roots"`
	}{
		NodeName:       r.cfg.NodeName,
		AgentVersion:   r.cfg.AgentVersion,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		WorkspaceRoots: append([]string{}, r.cfg.WorkspaceRoots...),
		Capabilities: []string{
			"node.info",
			"opencode.status",
			"opencode.start",
			"opencode.restart",
			"opencode.stop",
			"project.list",
			"session.list",
			"session.create",
			"session.rename",
			"session.delete",
			"session.prompt",
			"session.messages",
			"session.diff",
			"session.interrupt",
			"permission.reply",
			"permission.list",
			"file.list",
			"file.read",
			"pty.create",
			"pty.attach",
			"pty.write",
			"pty.resize",
			"pty.close",
		},
	})
	if err != nil {
		return fmt.Errorf("agentd: encode hello params: %w", err)
	}

	frame := protocol.Frame{
		Type:   protocol.FrameHello,
		ID:     fmt.Sprintf("hello-%d", time.Now().UnixNano()),
		Params: params,
	}
	if err := r.write(ctx, frame); err != nil {
		return fmt.Errorf("agentd: send hello: %w", err)
	}
	return nil
}

// awaitWelcome reads until the control plane acknowledges registration. Frames
// received before welcome are ignored: a peer may heartbeat while it validates
// the agent. Every inbound frame is validated first so a malformed peer fails
// fast instead of being partially processed.
//
// A welcome result carries the node ID the control plane assigned (or reused).
// It replaces the configured NodeID so forwarded events are attributed to the
// registered node; a welcome without a result keeps the configured value for
// compatibility with peers that do not assign IDs.
func (r *Runner) awaitWelcome(ctx context.Context) error {
	for {
		frame, err := r.transport.Read(ctx)
		if err != nil {
			return fmt.Errorf("agentd: read welcome: %w", err)
		}
		if err := frame.Validate(); err != nil {
			return fmt.Errorf("agentd: invalid frame: %w", err)
		}
		if frame.Type == protocol.FrameWelcome {
			return r.applyWelcome(frame)
		}
	}
}

// applyWelcome adopts the control-plane-assigned node ID from a welcome frame
// and persists a one-shot credential when the control plane sends one.
//
// An empty result is the I0 fallback; unparsable JSON is a protocol error and
// fails the handshake instead of silently keeping a possibly wrong identity.
//
// The credential bypasses the returned error text and logs entirely: it goes
// only to CredentialSink. A sink failure aborts the handshake (wrapped with the
// persist context) because the control plane has already recorded the hash and
// this agent would otherwise lose the only copy of the plaintext.
func (r *Runner) applyWelcome(frame protocol.Frame) error {
	if len(frame.Result) == 0 {
		return nil
	}
	var result struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		return fmt.Errorf("agentd: decode welcome result: %w", err)
	}
	if result.NodeID != "" {
		r.cfg.NodeID = result.NodeID
	}
	if result.Credential == "" {
		return nil
	}
	if r.cfg.CredentialSink == nil {
		// Embedded callers and tests may not own a credential file. Ignoring the
		// field keeps the handshake successful for them.
		return nil
	}
	if err := r.cfg.CredentialSink(result.Credential); err != nil {
		// 双 %w 让 errors.Is(err, ErrCredentialPersist) 与底层原因（权限、
		// 磁盘）同时可查；凭证本身仍然只进 sink，不进错误文本。
		return fmt.Errorf("agentd: %w: %w", ErrCredentialPersist, err)
	}
	return nil
}

// readFrames pumps transport reads into a channel so Run can select on them
// alongside worker failures. Without the pump a blocked Read would hide an
// event-stream failure until the next inbound frame arrived. A malformed frame
// stops the pump: the connection cannot be trusted after a protocol violation.
func (r *Runner) readFrames(ctx context.Context) (<-chan protocol.Frame, <-chan error) {
	frames := make(chan protocol.Frame)
	errs := make(chan error, 1)

	go func() {
		for {
			frame, err := r.transport.Read(ctx)
			if err != nil {
				errs <- err
				return
			}
			if err := frame.Validate(); err != nil {
				errs <- fmt.Errorf("invalid frame: %w", err)
				return
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()

	return frames, errs
}

// heartbeat keeps the connection alive while no traffic flows.
func (r *Runner) heartbeat(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.Heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.write(ctx, protocol.Frame{Type: protocol.FrameHeartbeat}); err != nil {
				return fmt.Errorf("agentd: write heartbeat: %w", err)
			}
		}
	}
}

// forwardEvents mirrors the OpenCode event stream onto the control-plane
// connection. A closed stream means the agent lost events (there is no stable
// sequence in I0), so it emits a resync marker and fails the connection; the
// caller reconnects with backoff and the control plane resynchronizes state.
func (r *Runner) forwardEvents(ctx context.Context) error {
	events, err := r.events.SubscribeEvents(ctx)
	if err != nil {
		return fmt.Errorf("agentd: subscribe opencode events: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				// Cancellation also closes the source channel; report that as a
				// clean stop instead of a resync so shutdown stays quiet.
				if ctx.Err() != nil {
					return nil
				}
				resync := protocol.Event{
					NodeID: r.cfg.NodeID,
					Seq:    0,
					Type:   "agent.resync_required",
					Time:   time.Now(),
				}
				if writeErr := r.write(ctx, protocol.Frame{Type: protocol.FrameEvent, Event: &resync}); writeErr != nil {
					return fmt.Errorf("agentd: write resync event: %w", writeErr)
				}
				return fmt.Errorf("agentd: opencode event stream closed: %w", io.EOF)
			}

			frame := protocol.Frame{
				Type: protocol.FrameEvent,
				Event: &protocol.Event{
					NodeID:    r.cfg.NodeID,
					SessionID: event.SessionID,
					Seq:       0,
					Type:      event.Type,
					Data:      event.Data,
					Time:      time.Now(),
				},
			}
			if err := r.write(ctx, frame); err != nil {
				return fmt.Errorf("agentd: write event: %w", err)
			}
		}
	}
}

// forwardPTYEvents mirrors handler-owned terminal output onto the control-plane
// connection. The handler channel is intentionally never closed: a reconnect
// reuses the same handler, and the runner's context ends this goroutine.
func (r *Runner) forwardPTYEvents(ctx context.Context) error {
	events := r.handler.PTYEvents()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.NodeID == "" {
				event.NodeID = r.cfg.NodeID
			}
			if event.Time.IsZero() {
				event.Time = time.Now().UTC()
			}
			frame := protocol.Frame{Type: protocol.FrameEvent, Event: &event}
			if err := r.write(ctx, frame); err != nil {
				return fmt.Errorf("agentd: write pty event: %w", err)
			}
		}
	}
}

// dispatch handles one inbound frame. readFrames has already validated it, so
// the default branch is defensive only: unknown frame types are rejected at the
// read boundary, and adding one requires a protocol-compatible change rather
// than being silently ignored.
func (r *Runner) dispatch(ctx context.Context, frame protocol.Frame) error {
	switch frame.Type {
	case protocol.FrameRequest:
		return r.respond(ctx, frame)
	case protocol.FrameResponse, protocol.FrameCancel, protocol.FrameHeartbeat:
		// I0 has no server-initiated requests and all agent requests are
		// short-lived, so responses, cancels, and peer heartbeats need no work.
		return nil
	default:
		return nil
	}
}

// respond executes a request and writes its result or error. A handler failure
// becomes an error frame, not a dropped connection, so one bad request cannot
// take down event forwarding or heartbeats.
func (r *Runner) respond(ctx context.Context, frame protocol.Frame) error {
	result, err := r.handler.Handle(ctx, frame.Method, frame.Params)

	response := protocol.Frame{Type: protocol.FrameResponse, ID: frame.ID}
	if err != nil {
		response.Error = &protocol.FrameError{
			Code:    errorCode(err),
			Message: err.Error(),
		}
	} else {
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			response.Error = &protocol.FrameError{
				Code:    "internal_error",
				Message: "encode response result",
			}
		} else {
			response.Result = encoded
		}
	}

	response = fitResponseFrame(response)
	if writeErr := r.write(ctx, response); writeErr != nil {
		return fmt.Errorf("agentd: write response: %w", writeErr)
	}
	return nil
}

// fitResponseFrame 把超过协议帧上限的响应降级为稳定错误帧。
//
// 直接写超限帧会让控制面按读上限关闭连接：所有在途请求一起失败，调用方还
// 会收到误导性的 node_offline。返回明确错误至少能让客户端缩小请求范围，
// 而连接与心跳保持可用。
func fitResponseFrame(response protocol.Frame) protocol.Frame {
	if len(response.Result) < responseSizeCheckThreshold {
		return response
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) <= protocol.MaxFrameBytes {
		return response
	}
	return protocol.Frame{
		Type: protocol.FrameResponse,
		ID:   response.ID,
		Error: &protocol.FrameError{
			Code:    "response_too_large",
			Message: fmt.Sprintf("response exceeds %d bytes; narrow the request", protocol.MaxFrameBytes),
		},
	}
}

// errorCode maps a handler error to a stable wire code. Unknown methods carry
// handler.ErrUnknownMethod; every other failure is reported as an internal
// error. The message is forwarded verbatim and never contains request params.
func errorCode(err error) string {
	if errors.Is(err, handler.ErrUnknownMethod) {
		return "method_not_found"
	}
	return "internal_error"
}

// write serializes outbound frames and validates them before they reach the
// transport. transport only allows one concurrent writer, and heartbeats,
// responses, and events originate on different goroutines.
func (r *Runner) write(ctx context.Context, frame protocol.Frame) error {
	if err := frame.Validate(); err != nil {
		return fmt.Errorf("agentd: invalid frame: %w", err)
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.transport.Write(ctx, frame)
}
