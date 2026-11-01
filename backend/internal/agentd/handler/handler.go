// Package handler dispatches control-plane requests to the local opencode
// client and the shared service supervisor. It is the boundary between the
// wire protocol and the concrete OpenCode API, so it validates request params
// and owns prompt idempotency while the runner only moves frames.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/insmtx/Lework/backend/internal/agentd/opencode"
	"github.com/insmtx/Lework/backend/internal/agentd/supervisor"
)

// AgentVersion is reported by node.info. It is a constant in I0 because the
// agent has no build-version injection yet.
const AgentVersion = "0.1.0"

// OpenCodeAPI is the subset of opencode.Client the handler drives. Keeping it
// narrow lets tests replace the HTTP client without a server.
type OpenCodeAPI interface {
	ListSessions(ctx context.Context) ([]opencode.Session, error)
	CreateSession(ctx context.Context, directory string) (opencode.Session, error)
	Prompt(ctx context.Context, sessionID, text string) (string, error)
	Messages(ctx context.Context, sessionID string) ([]opencode.Message, error)
	ReplyPermission(ctx context.Context, sessionID, permissionID, decision, message string) error
}

// SupervisorAPI is the subset of supervisor.Supervisor the handler drives.
type SupervisorAPI interface {
	Ensure(ctx context.Context) (supervisor.Status, error)
	Restart(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() supervisor.Status
}

// SessionListParams are the parameters for session.list. Directory is accepted
// for wire compatibility; I0 has no directory filter and lists every session.
type SessionListParams struct {
	Directory string `json:"directory"`
}

// SessionCreateParams are the parameters for session.create.
type SessionCreateParams struct {
	Directory string `json:"directory"`
}

// SessionPromptParams are the parameters for session.prompt. IdempotencyKey is
// mandatory so a control-plane retry cannot submit the same prompt twice.
type SessionPromptParams struct {
	SessionID      string `json:"session_id"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key"`
}

// SessionMessagesParams are the parameters for session.messages.
type SessionMessagesParams struct {
	SessionID string `json:"session_id"`
}

// PermissionReplyParams are the parameters for permission.reply.
type PermissionReplyParams struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	Decision     string `json:"decision"`
	Message      string `json:"message"`
}

// NodeInfo is the payload returned by node.info.
type NodeInfo struct {
	NodeName     string `json:"node_name"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
}

// Handler dispatches one request at a time to the OpenCode API or supervisor.
// It never logs params, so prompt text stays out of agent logs.
type Handler struct {
	opencode    OpenCodeAPI
	supervisor  SupervisorAPI
	idempotency *idempotencyStore
}

// New builds a handler around the OpenCode client and service supervisor.
func New(openCode OpenCodeAPI, service SupervisorAPI) *Handler {
	return &Handler{
		opencode:    openCode,
		supervisor:  service,
		idempotency: newIdempotencyStore(idempotencyTTL),
	}
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

	case "session.list":
		var p SessionListParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		return h.opencode.ListSessions(ctx)

	case "session.create":
		var p SessionCreateParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		return h.opencode.CreateSession(ctx, p.Directory)

	case "session.prompt":
		return h.prompt(ctx, params)

	case "session.messages":
		var p SessionMessagesParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		return h.opencode.Messages(ctx, p.SessionID)

	case "permission.reply":
		var p PermissionReplyParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		return nil, h.opencode.ReplyPermission(ctx, p.SessionID, p.PermissionID, p.Decision, p.Message)

	default:
		return nil, fmt.Errorf("agentd: unknown method %q", method)
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

// nodeInfo reports the static node identity used during registration.
func (h *Handler) nodeInfo() NodeInfo {
	return NodeInfo{
		NodeName:     nodeName(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: AgentVersion,
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
