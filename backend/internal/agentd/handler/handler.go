// Package handler dispatches control-plane requests to the local opencode
// client and the shared service supervisor. It is the boundary between the
// wire protocol and the concrete OpenCode API, so it validates request params
// and owns prompt idempotency while the runner only moves frames.
package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/insmtx/lework/backend/internal/agentd/opencode"
	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/supervisor"
)

// AgentVersion is reported by node.info. It is a constant in I0 because the
// agent has no build-version injection yet.
const AgentVersion = "0.1.0"

// Message pagination bounds shared by the browser API and the node handler.
// The upper bound keeps one event loop iteration from decoding an unbounded
// session history.
const (
	defaultMessagesLimit = 200
	maxMessagesLimit     = 200
)

// OpenCodeAPI is the subset of opencode.Client the handler drives. Keeping it
// narrow lets tests replace the HTTP client without a server.
type OpenCodeAPI interface {
	ListSessions(ctx context.Context, directory string) ([]opencode.Session, error)
	ListProjects(ctx context.Context) ([]opencode.Project, error)
	CreateSession(ctx context.Context, directory string) (opencode.Session, error)
	RenameSession(ctx context.Context, sessionID, title string) error
	DeleteSession(ctx context.Context, sessionID string) error
	Prompt(ctx context.Context, sessionID, text string) (string, error)
	Messages(ctx context.Context, sessionID string, limit int, cursor string) (opencode.MessagePage, error)
	ReplyPermission(ctx context.Context, sessionID, permissionID, decision, message string) error
	ListPermissions(ctx context.Context) ([]opencode.PermissionRequest, error)

	SessionDiff(ctx context.Context, sessionID, from, to string, context int) ([]opencode.FileDiff, error)
	Interrupt(ctx context.Context, sessionID string, resume bool) (opencode.InterruptResult, error)
	ListFiles(ctx context.Context, directory, path string) ([]opencode.FileInfo, error)
	ReadFile(ctx context.Context, directory, path string, maxBytes int64) (opencode.FileContent, error)

	CreatePersistentTerminal(ctx context.Context, sessionID string, req opencode.CreateTerminalRequest) (opencode.PersistentTerminal, error)
	ConnectPersistentTerminal(ctx context.Context, ptyID string) (*opencode.TerminalConn, error)
	ResizePersistentTerminal(ctx context.Context, ptyID string, size opencode.TerminalSize) error
	ClosePersistentTerminal(ctx context.Context, ptyID string) error
}

// SupervisorAPI is the subset of supervisor.Supervisor the handler drives.
type SupervisorAPI interface {
	Ensure(ctx context.Context) (supervisor.Status, error)
	Restart(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() supervisor.Status
}

// SessionListParams are the parameters for session.list. Directory is an
// optional location filter; an empty value lists every session on the node.
type SessionListParams struct {
	Directory string `json:"directory"`
}

// SessionCreateParams are the parameters for session.create.
type SessionCreateParams struct {
	Directory string `json:"directory"`
}

// SessionRenameParams are the parameters for session.rename.
type SessionRenameParams struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// SessionDeleteParams are the parameters for session.delete.
type SessionDeleteParams struct {
	SessionID string `json:"session_id"`
}

// SessionPromptParams are the parameters for session.prompt. IdempotencyKey is
// mandatory so a control-plane retry cannot submit the same prompt twice.
type SessionPromptParams struct {
	SessionID      string `json:"session_id"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key"`
}

// SessionMessagesParams are the parameters for session.messages. Limit is
// clamped to the agent page size; Cursor is an opaque older-page marker.
type SessionMessagesParams struct {
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

// PermissionReplyParams are the parameters for permission.reply.
type PermissionReplyParams struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	Decision     string `json:"decision"`
	Message      string `json:"message"`
}

// PermissionListParams are the parameters for permission.list. An empty
// SessionID means every pending request on the node.
type PermissionListParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// SessionDiffParams are the parameters for session.diff. Directory is the
// session working directory and is required so the agent can confine the
// request before touching OpenCode.
type SessionDiffParams struct {
	SessionID string `json:"session_id"`
	Directory string `json:"directory"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Context   int    `json:"context,omitempty"`
}

// SessionInterruptParams are the parameters for session.interrupt.
type SessionInterruptParams struct {
	SessionID string `json:"session_id"`
}

// FileListParams are the parameters for file.list. Path is relative to
// Directory and may be empty for the root.
type FileListParams struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
}

// FileReadParams are the parameters for file.read. MaxBytes is capped by the
// handler; callers cannot request an unbounded file body.
type FileReadParams struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
}

// FileReadResult is the browser-safe projection of a file read. Content is
// text for UTF-8 files and base64 for binary files; Path is always relative.
type FileReadResult struct {
	Path      string `json:"path"`
	Encoding  string `json:"encoding"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

// PTYCreateParams are the parameters for pty.create.
type PTYCreateParams struct {
	SessionID string            `json:"session_id"`
	Directory string            `json:"directory"`
	Cols      int               `json:"cols"`
	Rows      int               `json:"rows"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// PTYIDParams are the parameters shared by pty.attach and pty.close.
type PTYIDParams struct {
	PTYID string `json:"pty_id"`
}

// PTYWriteParams are the parameters for pty.write. Data is base64 so the
// JSON protocol can carry arbitrary terminal bytes.
type PTYWriteParams struct {
	PTYID string `json:"pty_id"`
	Data  string `json:"data"`
}

// PTYResizeParams are the parameters for pty.resize.
type PTYResizeParams struct {
	PTYID string `json:"pty_id"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// NodeInfo is the payload returned by node.info.
type NodeInfo struct {
	NodeName     string `json:"node_name"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
	// WorkspaceRoots are the absolute directories this agent may serve. The
	// browser needs them to offer a valid default project directory, because
	// session.create rejects anything outside them.
	WorkspaceRoots []string `json:"workspace_roots"`
}

// ErrUnknownMethod reports a method the handler does not implement. It is a
// sentinel so the runner can map the failure to a stable method_not_found wire
// code without matching message text.
var ErrUnknownMethod = errors.New("agentd: unknown method")

// Handler dispatches one request at a time to the OpenCode API or supervisor.
// It never logs params, so prompt text stays out of agent logs.
type Handler struct {
	opencode    OpenCodeAPI
	supervisor  SupervisorAPI
	idempotency *idempotencyStore
	pty         *ptyManager
	// workspaceRoots confines session.create and file/PTY access when non-empty.
	// Roots come from config.Config.WorkspaceRoots, which Validate already
	// required to be absolute.
	workspaceRoots []string
}

// Option customizes a Handler. It exists so workspace confinement could be
// added without changing the two-argument New signature every caller uses.
type Option func(*Handler)

// WithWorkspaceRoots confines session.create to the supplied directories. An
// empty list preserves the previous pass-through behavior.
func WithWorkspaceRoots(roots ...string) Option {
	return func(h *Handler) {
		h.workspaceRoots = append([]string(nil), roots...)
	}
}

// New builds a handler around the OpenCode client and service supervisor.
func New(openCode OpenCodeAPI, service SupervisorAPI, opts ...Option) *Handler {
	h := &Handler{
		opencode:    openCode,
		supervisor:  service,
		idempotency: newIdempotencyStore(idempotencyTTL),
		pty:         newPTYManager(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// PTYEvents returns the terminal output/exit stream owned by the handler. The
// runner forwards these events to the control plane; the channel itself is
// never closed so a reconnect can reuse the same handler.
func (h *Handler) PTYEvents() <-chan protocol.Event {
	return h.pty.events
}

// ClosePTYs terminates every terminal owned by this handler. It is called when
// the control-plane connection ends so a dropped agent cannot leave shell
// processes behind.
func (h *Handler) ClosePTYs() {
	h.pty.closeAll(h.opencode)
}

// Handle executes method with params and returns the value the runner should
// encode as the response result. An unknown method or malformed params yield a
// descriptive error; no prompt text or params are included in error strings.
func (h *Handler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "node.info":
		return h.nodeInfo(), nil

	case "opencode.status":
		return h.supervisor.Status(), nil
	case "opencode.start":
		return h.supervisor.Ensure(ctx)
	case "opencode.restart":
		return nil, h.supervisor.Restart(ctx)
	case "opencode.stop":
		return nil, h.supervisor.Stop(ctx)

	case "project.list":
		return h.opencode.ListProjects(ctx)

	case "session.list":
		var p SessionListParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if p.Directory != "" {
			if err := h.confineDirectory(p.Directory); err != nil {
				return nil, err
			}
		}
		return h.opencode.ListSessions(ctx, p.Directory)

	case "session.create":
		var p SessionCreateParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if err := h.confineDirectory(p.Directory); err != nil {
			return nil, err
		}
		return h.opencode.CreateSession(ctx, p.Directory)

	case "session.rename":
		var p SessionRenameParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		title := strings.TrimSpace(p.Title)
		if p.SessionID == "" {
			return nil, errors.New("session.rename: session_id is required")
		}
		if title == "" {
			return nil, errors.New("session.rename: title is required")
		}
		return nil, h.opencode.RenameSession(ctx, p.SessionID, title)

	case "session.delete":
		var p SessionDeleteParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if p.SessionID == "" {
			return nil, errors.New("session.delete: session_id is required")
		}
		return nil, h.opencode.DeleteSession(ctx, p.SessionID)

	case "session.prompt":
		return h.prompt(ctx, params)

	case "session.messages":
		var p SessionMessagesParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultMessagesLimit
		}
		if limit > maxMessagesLimit {
			limit = maxMessagesLimit
		}
		return h.opencode.Messages(ctx, p.SessionID, limit, p.Cursor)

	case "session.diff":
		return h.sessionDiff(ctx, params)
	case "session.interrupt":
		var p SessionInterruptParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if p.SessionID == "" {
			return nil, errors.New("session.interrupt: session_id is required")
		}
		return h.opencode.Interrupt(ctx, p.SessionID, false)

	case "file.list":
		return h.listFiles(ctx, params)
	case "file.read":
		return h.readFile(ctx, params)

	case "pty.create":
		return h.createPTY(ctx, params)
	case "pty.attach":
		return h.attachPTY(params)
	case "pty.write":
		return h.writePTY(ctx, params)
	case "pty.resize":
		return h.resizePTY(ctx, params)
	case "pty.close":
		return h.closePTY(params)

	case "permission.reply":
		var p PermissionReplyParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if err := validatePermissionDecision(p.Decision); err != nil {
			return nil, err
		}
		return nil, h.opencode.ReplyPermission(ctx, p.SessionID, p.PermissionID, p.Decision, p.Message)

	case "permission.list":
		var p PermissionListParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		return h.listPermissions(ctx, p.SessionID)

	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownMethod, method)
	}
}

// listPermissions filters the node-wide snapshot by session so the browser
// can reconcile one session without seeing approvals from other worktrees.
func (h *Handler) listPermissions(ctx context.Context, sessionID string) ([]opencode.PermissionRequest, error) {
	permissions, err := h.opencode.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		return permissions, nil
	}
	filtered := make([]opencode.PermissionRequest, 0, len(permissions))
	for _, permission := range permissions {
		if permission.SessionID == sessionID {
			filtered = append(filtered, permission)
		}
	}
	return filtered, nil
}

// validatePermissionDecision keeps the OpenCode enum at the protocol edge;
// forwarding an unknown value would surface as an opaque 400 from the server.
func validatePermissionDecision(decision string) error {
	switch decision {
	case "once", "always", "reject":
		return nil
	default:
		return fmt.Errorf("permission.reply: decision must be once, always or reject")
	}
}

// prompt admits text idempotently: a retry with the same session and
// idempotency key returns the first receipt without calling opencode again,
// and concurrent retries share one in-flight admission.
func (h *Handler) prompt(ctx context.Context, params json.RawMessage) (any, error) {
	var p SessionPromptParams
	if err := decodeParams("session.prompt", params, &p); err != nil {
		return nil, err
	}
	if p.IdempotencyKey == "" {
		return nil, errors.New("idempotency_key is required")
	}

	key := p.SessionID + "\x00" + p.IdempotencyKey
	receipt, err := h.idempotency.do(ctx, key, func(admitCtx context.Context) (string, error) {
		return h.opencode.Prompt(admitCtx, p.SessionID, p.Text)
	})
	if err != nil {
		// Failures are deliberately not cached: the control plane may retry
		// the same key, and only a successful admission is safe to replay.
		return nil, err
	}
	return receipt, nil
}

// sessionDiff returns the OpenCode turn diff after enforcing the workspace
// boundary. OpenCode itself does not accept a directory for this endpoint, so
// Directory is validated here rather than forwarded.
func (h *Handler) sessionDiff(ctx context.Context, params json.RawMessage) (any, error) {
	var p SessionDiffParams
	if err := decodeParams("session.diff", params, &p); err != nil {
		return nil, err
	}
	if p.SessionID == "" {
		return nil, errors.New("session.diff: session_id is required")
	}
	if p.Directory == "" {
		return nil, errors.New("session.diff: directory is required")
	}
	if err := h.confineDirectory(p.Directory); err != nil {
		return nil, err
	}
	return h.opencode.SessionDiff(ctx, p.SessionID, p.From, p.To, p.Context)
}

// listFiles confines the requested directory/path and returns direct children.
func (h *Handler) listFiles(ctx context.Context, params json.RawMessage) (any, error) {
	var p FileListParams
	if err := decodeParams("file.list", params, &p); err != nil {
		return nil, err
	}
	if p.Directory == "" {
		return nil, errors.New("file.list: directory is required")
	}
	if err := h.confineDirectory(p.Directory); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(p.Directory)
	if err != nil {
		return nil, fmt.Errorf("file.list: resolve directory: %w", err)
	}
	target, err := h.confinePath(root, p.Path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return nil, fmt.Errorf("file.list: resolve relative path: %w", err)
	}
	if rel == "." {
		rel = ""
	}
	return h.opencode.ListFiles(ctx, root, filepath.ToSlash(rel))
}

// readFile confines the requested file and projects it into a JSON-safe result.
// The 512 KiB cap keeps the base64 response below the agent frame limit.
func (h *Handler) readFile(ctx context.Context, params json.RawMessage) (any, error) {
	var p FileReadParams
	if err := decodeParams("file.read", params, &p); err != nil {
		return nil, err
	}
	if p.Directory == "" {
		return nil, errors.New("file.read: directory is required")
	}
	if err := h.confineDirectory(p.Directory); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, errors.New("file.read: path is required")
	}

	root, err := filepath.Abs(p.Directory)
	if err != nil {
		return nil, fmt.Errorf("file.read: resolve directory: %w", err)
	}
	target, err := h.confinePath(root, p.Path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return nil, fmt.Errorf("file.read: resolve relative path: %w", err)
	}

	const maxFileBytes = 512 << 10
	maxBytes := p.MaxBytes
	if maxBytes <= 0 || maxBytes > maxFileBytes {
		maxBytes = maxFileBytes
	}

	content, err := h.opencode.ReadFile(ctx, root, filepath.ToSlash(rel), maxBytes)
	if err != nil {
		return nil, err
	}

	encoding := "base64"
	encoded := base64.StdEncoding.EncodeToString(content.Data)
	if utf8.Valid(content.Data) && !bytes.ContainsRune(content.Data, '\x00') {
		encoding = "utf-8"
		encoded = string(content.Data)
	}
	return FileReadResult{
		Path:      filepath.ToSlash(rel),
		Encoding:  encoding,
		Content:   encoded,
		Size:      content.Size,
		Truncated: content.Truncated,
	}, nil
}

// createPTY starts a session-scoped terminal. Cwd is the session directory, so
// it must be inside workspace_roots before any process is spawned.
func (h *Handler) createPTY(ctx context.Context, params json.RawMessage) (any, error) {
	var p PTYCreateParams
	if err := decodeParams("pty.create", params, &p); err != nil {
		return nil, err
	}
	if p.SessionID == "" {
		return nil, errors.New("pty.create: session_id is required")
	}
	if p.Directory == "" {
		return nil, errors.New("pty.create: directory is required")
	}
	if err := h.confineDirectory(p.Directory); err != nil {
		return nil, err
	}
	cwd, err := filepath.Abs(p.Directory)
	if err != nil {
		return nil, fmt.Errorf("pty.create: resolve directory: %w", err)
	}

	if p.Cols <= 0 {
		p.Cols = 80
	}
	if p.Rows <= 0 {
		p.Rows = 24
	}
	if err := validatePTYSize(p.Cols, p.Rows); err != nil {
		return nil, err
	}
	command := p.Command
	if command == "" {
		command = os.Getenv("SHELL")
		if command == "" {
			command = "/bin/sh"
		}
	}
	args := p.Args
	if args == nil {
		args = []string{}
	}
	env := p.Env
	if env == nil {
		env = map[string]string{}
	}

	terminal, err := h.opencode.CreatePersistentTerminal(ctx, p.SessionID, opencode.CreateTerminalRequest{
		Command: command,
		Args:    args,
		Cwd:     cwd,
		Title:   "Lework Terminal",
		Env:     env,
		Size:    opencode.TerminalSize{Cols: p.Cols, Rows: p.Rows},
	})
	if err != nil {
		return nil, err
	}
	if terminal.ID == "" {
		return nil, errors.New("pty.create: OpenCode returned an empty terminal id")
	}
	h.pty.add(&terminalSession{
		id:        terminal.ID,
		sessionID: p.SessionID,
		cols:      p.Cols,
		rows:      p.Rows,
	})
	return terminal, nil
}

// attachPTY starts the output pump once. Attaching an already-running terminal
// is idempotent so a reconnect can subscribe to the same output stream.
func (h *Handler) attachPTY(params json.RawMessage) (any, error) {
	var p PTYIDParams
	if err := decodeParams("pty.attach", params, &p); err != nil {
		return nil, err
	}
	if p.PTYID == "" {
		return nil, errors.New("pty.attach: pty_id is required")
	}
	if err := h.pty.attach(p.PTYID, h.opencode); err != nil {
		return nil, err
	}
	return struct {
		Attached bool `json:"attached"`
	}{Attached: true}, nil
}

// writePTY decodes base64 terminal input and forwards it to OpenCode.
func (h *Handler) writePTY(ctx context.Context, params json.RawMessage) (any, error) {
	var p PTYWriteParams
	if err := decodeParams("pty.write", params, &p); err != nil {
		return nil, err
	}
	if p.PTYID == "" {
		return nil, errors.New("pty.write: pty_id is required")
	}
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return nil, fmt.Errorf("pty.write: decode data: %w", err)
	}
	if len(data) > 64<<10 {
		return nil, errors.New("pty.write: data exceeds 64 KiB")
	}
	if err := h.pty.write(ctx, p.PTYID, data); err != nil {
		return nil, err
	}
	return nil, nil
}

// resizePTY validates and forwards a terminal viewport change.
func (h *Handler) resizePTY(ctx context.Context, params json.RawMessage) (any, error) {
	var p PTYResizeParams
	if err := decodeParams("pty.resize", params, &p); err != nil {
		return nil, err
	}
	if p.PTYID == "" {
		return nil, errors.New("pty.resize: pty_id is required")
	}
	if err := validatePTYSize(p.Cols, p.Rows); err != nil {
		return nil, err
	}
	if err := h.pty.resize(ctx, p.PTYID, p.Cols, p.Rows); err != nil {
		return nil, err
	}
	return nil, nil
}

// closePTY terminates a terminal. A missing ID is a successful no-op.
func (h *Handler) closePTY(params json.RawMessage) (any, error) {
	var p PTYIDParams
	if err := decodeParams("pty.close", params, &p); err != nil {
		return nil, err
	}
	if p.PTYID == "" {
		return nil, errors.New("pty.close: pty_id is required")
	}
	h.pty.close(p.PTYID, h.opencode)
	return nil, nil
}

// validatePTYSize keeps column/row values inside the two-byte protocol range
// used by OpenCode's input_protocol=1 frames.
func validatePTYSize(cols, rows int) error {
	if cols < 1 || cols > 1000 || rows < 1 || rows > 1000 {
		return fmt.Errorf("invalid terminal size %dx%d", cols, rows)
	}
	return nil
}

// confineDirectory enforces workspace_roots on a directory. It returns no
// error when no roots are configured, preserving the I0 pass-through behavior.
func (h *Handler) confineDirectory(directory string) error {
	if len(h.workspaceRoots) == 0 {
		return nil
	}
	if directory == "" {
		return errors.New("directory is required when workspace_roots is configured")
	}
	_, err := h.resolveWorkspaceDirectory(directory)
	return err
}

// resolveWorkspaceDirectory resolves directory and verifies it is inside one
// configured root. Symlinks are compared after EvalSymlinks so a link inside a
// root cannot redirect a file or PTY operation outside it.
func (h *Handler) resolveWorkspaceDirectory(directory string) (string, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	if len(h.workspaceRoots) == 0 {
		return abs, nil
	}

	resolvedTarget := abs
	if evaluated, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		resolvedTarget = evaluated
	}

	for _, root := range h.workspaceRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		resolvedRoot := absRoot
		if evaluated, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
			resolvedRoot = evaluated
		}
		if pathWithin(resolvedRoot, resolvedTarget) {
			return abs, nil
		}
	}
	return "", errors.New("directory is outside workspace_roots")
}

// confinePath resolves path relative to root and rejects lexical escapes and
// symlinks whose target resolves outside root.
func (h *Handler) confinePath(root, path string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	if len(h.workspaceRoots) > 0 {
		if _, err := h.resolveWorkspaceDirectory(absRoot); err != nil {
			return "", err
		}
	}

	target := filepath.Clean(path)
	if !filepath.IsAbs(target) {
		target = filepath.Join(absRoot, target)
	}
	if !pathWithin(absRoot, target) {
		return "", errors.New("path is outside the workspace directory")
	}

	resolvedRoot := absRoot
	if evaluated, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		resolvedRoot = evaluated
	}
	if evaluated, evalErr := filepath.EvalSymlinks(target); evalErr == nil {
		if !pathWithin(resolvedRoot, evaluated) {
			return "", errors.New("path resolves outside the workspace directory")
		}
	}
	return target, nil
}

// pathWithin reports whether target is root or a descendant of root.
func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || !escapesRoot(rel)
}

// escapesRoot reports whether a filepath.Rel result points above the root. Rel
// returns ".." or a "../..." prefix for a parent, and an absolute path only on
// platforms where the two paths cannot be expressed relatively.
func escapesRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// nodeInfo reports the static node identity used during registration.
func (h *Handler) nodeInfo() NodeInfo {
	roots := append([]string{}, h.workspaceRoots...)
	return NodeInfo{
		NodeName:       nodeName(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		AgentVersion:   AgentVersion,
		WorkspaceRoots: roots,
	}
}

// nodeName falls back to "unknown" so node.info cannot fail on a host without
// a resolvable hostname.
func nodeName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

// decodeParams unmarshals params into out. An absent body is accepted so
// parameterless methods can omit it; the method name is included in the error
// while the raw params are not.
func decodeParams(method string, params json.RawMessage, out any) error {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, out); err != nil {
		return fmt.Errorf("decode %s params: %w", method, err)
	}
	return nil
}
