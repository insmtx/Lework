package auth

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/types"
)

// AuthState 认证状态
type AuthState int

const (
	AuthStateNil    AuthState = 0 // 未提供 token
	AuthStateSucc   AuthState = 1 // 认证成功
	AuthStateFailed AuthState = 2 // 认证失败
)

const (
	ctxKeyCaller = "caller"
	ctxKeyTrace  = "trace"
)

// Caller 定义了一个执行身份，包含用户 ID、租户 ID 和认证状态。
type Caller struct {
	Uin   uint
	OrgID uint
	State AuthState
}

// Trace 定义了一个跟踪信息结构体，用于在请求链路中传递跟踪标识符，帮助进行分布式追踪和日志关联。
type Trace struct {
	// RequestID 是一个全局唯一的标识符，用于标识一次请求，可以用于日志关联和调试。
	RequestID string
	// TraceID 是一个全局唯一的标识符，用于跟踪请求链路中的调用关系。
	TraceID string
	// SpanID 是可选的，如果请求链路中包含多个调用，可以用来标识具体的调用跨度。
	SpanID []string
}

type IdentityContext struct {
	Caller *Caller
	Trace  *Trace
}

// WithContext 携带 Caller 和 Trace 信息的上下文对象。
func WithContext(ctx context.Context, caller *Caller, trace *Trace) context.Context {
	ctx = context.WithValue(ctx, ctxKeyCaller, caller)
	ctx = context.WithValue(ctx, ctxKeyTrace, trace)
	return ctx
}

// WithGinContext 携带 Caller 和 Trace 信息到 gin.Context 中。
func WithGinContext(ctx *gin.Context, caller *Caller, trace *Trace) {
	ctx.Set(ctxKeyCaller, caller)
	ctx.Set(ctxKeyTrace, trace)
}

// FromContext 从上下文中提取 Caller 和 Trace 信息。
func FromContext(ctx context.Context) (*Caller, *Trace) {
	if ctx == nil {
		return nil, nil
	}
	var (
		caller *Caller
		trace  *Trace
	)
	{
		val := ctx.Value(ctxKeyCaller)
		if val == nil {
			caller = nil
		} else {
			caller, _ = val.(*Caller)
		}
	}
	{
		val := ctx.Value(ctxKeyTrace)
		if val == nil {
			trace = nil
		} else {
			trace, _ = val.(*Trace)
		}
	}
	return caller, trace
}

// FromGinContext 从 gin.Context 中提取 Caller 和 Trace 信息。
func FromGinContext(ctx *gin.Context) (*Caller, *Trace) {
	callerVal, callerExists := ctx.Get(ctxKeyCaller)
	traceVal, traceExists := ctx.Get(ctxKeyTrace)

	var caller *Caller
	var trace *Trace

	if callerExists {
		caller, _ = callerVal.(*Caller)
	}
	if traceExists {
		trace, _ = traceVal.(*Trace)
	}

	return caller, trace
}

// SystemIdentity 返回一个预定义的系统身份，通常用于系统内部调用或没有特定用户上下文的场景。
func SystemIdentity() *Caller {
	return &Caller{
		Uin:   types.SystemUin,
		OrgID: types.SystemOrgID,
		State: AuthStateSucc,
	}
}
