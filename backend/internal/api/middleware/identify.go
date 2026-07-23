package middleware

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ygpkg/yg-go/apis/constants"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	adapteraccount "github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

const (
	headerKeyRequestID = "X-Request-ID"
	headerKeyTraceID   = "X-Trace-ID"
)

var skipAuthPaths = map[string]bool{
	"/v1/RegisterByEmail":    true,
	"/v1/LoginByEmail":       true,
	"/v1/SendPhoneLoginCode": true,
	"/v1/LoginByPhoneCode":   true,
	"/v1/RefreshToken":       true,
	"/v1/ChooseUin":          true,
	"/v1/CreateOrganization": true,
}

// TokenParser is an alias for account.TokenParser so callers in
// api/handler (which cannot import account directly due to import
// cycles) can reference the parser contract through this package.
type TokenParser = adapteraccount.TokenParser

// CallerMiddleware parses the Authorization header via the injected
// TokenParser and stores the resulting Caller/Trace in the gin context.
func CallerMiddleware(parser adapteraccount.TokenParser, database *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		reqID := ctx.Request.Header.Get(headerKeyRequestID)
		if reqID == "" {
			reqID = strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		traceID := ctx.Request.Header.Get(headerKeyTraceID)
		if traceID == "" {
			traceID = reqID
		}
		ctx.Set(constants.CtxKeyRequestID, reqID)
		ctx.Header(headerKeyRequestID, reqID)
		logs.SetContextFields(ctx, "req_id", reqID)
		requestCtx := context.WithValue(ctx.Request.Context(), constants.CtxKeyRequestID, reqID)
		ctx.Request = ctx.Request.WithContext(logs.WithContextFields(
			requestCtx, "req_id", reqID,
		))

		caller, tokenStr := parseCallerFromRequest(ctx, parser)

		trace := &types.Trace{
			RequestID: reqID,
			TraceID:   traceID,
			SpanID:    []string{},
		}
		if tokenStr != "" {
			reqCtx := localauth.WithBearerToken(ctx.Request.Context(), tokenStr)
			ctx.Request = ctx.Request.WithContext(reqCtx)
		}
		localauth.WithGinContext(ctx, caller, trace, tokenStr)
		ctx.Request = ctx.Request.WithContext(localauth.WithContext(ctx.Request.Context(), caller, trace))
		ctx.Next()
	}
}

func parseCallerFromRequest(ctx *gin.Context, parser adapteraccount.TokenParser) (*types.Caller, string) {
	if os.Getenv("LEROS_DEV") == "true" {
		return &types.Caller{
			Uin:   1,
			OrgID: 1,
			Kind:  types.CallerKindUser,
			State: types.AuthStateSucc,
		}, ""
	}

	if skipAuthPaths[ctx.Request.URL.Path] {
		authHeader := ctx.Request.Header.Get("Authorization")
		if authHeader != "" {
			tokenStr := extractTokenFromHeader(authHeader)
			if tokenStr != "" {
				reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 5*time.Second)
				defer cancel()
				userCaller, userErr := parser.ParseUser(reqCtx, tokenStr)
				if userErr == nil && userCaller != nil && userCaller.State == types.AuthStateSucc {
					return userCaller, tokenStr
				}
				return &types.Caller{State: types.AuthStateNil}, tokenStr
			}
		}
		return &types.Caller{State: types.AuthStateNil}, ""
	}

	authHeader := ctx.Request.Header.Get("Authorization")
	if authHeader == "" {
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateNil,
		}, ""
	}

	tokenStr := extractTokenFromHeader(authHeader)
	if tokenStr == "" {
		logs.DebugContextw(ctx, "no valid token found in request", "authHeader", authHeader)
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateNil,
		}, ""
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 5*time.Second)
	defer cancel()

	userCaller, userErr := parser.ParseUser(reqCtx, tokenStr)
	if userErr == nil && userCaller != nil && userCaller.State == types.AuthStateSucc {
		return userCaller, tokenStr
	}
	if userErr != nil {
		logs.DebugContextw(ctx, "parse user token failed", "error", userErr)
	}

	workerCaller, workerErr := parser.ParseWorker(reqCtx, tokenStr)
	if workerErr == nil && workerCaller != nil && workerCaller.State == types.AuthStateSucc {
		return workerCaller, tokenStr
	}
	if workerErr != nil {
		logs.WarnContextw(ctx, "parse auth token failed", "user_error", userErr, "worker_error", workerErr)
	}
	return &types.Caller{State: types.AuthStateFailed}, tokenStr
}

func extractTokenFromHeader(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return strings.TrimSpace(authHeader)
}
