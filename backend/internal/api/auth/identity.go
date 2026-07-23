package auth

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/types"
)

const (
	ctxKeyCaller = "caller"
	ctxKeyTrace  = "trace"
	ctxKeyToken  = "token"
)

// WithContext 携带 Caller 和 Trace 信息的上下文对象。
func WithContext(ctx context.Context, caller *types.Caller, trace *types.Trace) context.Context {
	ctx = context.WithValue(ctx, ctxKeyCaller, caller)
	ctx = context.WithValue(ctx, ctxKeyTrace, trace)
	return ctx
}

// WithGinContext 携带 Caller、Trace 和 Token 信息到 gin.Context 中。
func WithGinContext(ctx *gin.Context, caller *types.Caller, trace *types.Trace, token string) {
	ctx.Set(ctxKeyCaller, caller)
	ctx.Set(ctxKeyTrace, trace)
	ctx.Set(ctxKeyToken, token)
}

// FromContext 从上下文中提取 Caller 和 Trace 信息。
func FromContext(ctx context.Context) (*types.Caller, *types.Trace) {
	if ctx == nil {
		return nil, nil
	}
	var (
		caller *types.Caller
		trace  *types.Trace
	)
	{
		val := ctx.Value(ctxKeyCaller)
		if val == nil {
			caller = nil
		} else {
			caller, _ = val.(*types.Caller)
		}
	}
	{
		val := ctx.Value(ctxKeyTrace)
		if val == nil {
			trace = nil
		} else {
			trace, _ = val.(*types.Trace)
		}
	}
	return caller, trace
}

// FromGinContext 从 gin.Context 中提取 Caller 和 Trace 信息。
func FromGinContext(ctx *gin.Context) (*types.Caller, *types.Trace) {
	callerVal, callerExists := ctx.Get(ctxKeyCaller)
	traceVal, traceExists := ctx.Get(ctxKeyTrace)

	var caller *types.Caller
	var trace *types.Trace

	if callerExists {
		caller, _ = callerVal.(*types.Caller)
	}
	if traceExists {
		trace, _ = traceVal.(*types.Trace)
	}

	return caller, trace
}

// WithBearerToken 将原始 Bearer token 写入 context，供适配器层透传到外部 IAM 服务。
func WithBearerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxKeyToken, token)
}

// BearerTokenFromContext 从 context 中提取原始 Bearer token。
func BearerTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(ctxKeyToken).(string); ok {
		return token
	}
	return ""
}
