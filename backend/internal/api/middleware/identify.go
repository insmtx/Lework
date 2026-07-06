package middleware

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const (
	headerKeyRequestID = "X-Request-ID"
	headerKeyTraceID   = "X-Trace-ID"
)

// CallerMiddleware .
func CallerMiddleware(jwtSecret string, database *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		reqID := ctx.Request.Header.Get(headerKeyRequestID)
		if reqID == "" {
			reqID = snowflake.GenerateIDBase58()
		}
		traceID := ctx.Request.Header.Get(headerKeyTraceID)
		if traceID == "" {
			traceID = reqID
		}

		caller := parseCallerFromRequest(ctx, jwtSecret, database, reqID)

		localauth.WithGinContext(ctx, caller, &types.Trace{
			RequestID: reqID,
			TraceID:   traceID,
			SpanID:    []string{},
		})
		ctx.Next()
	}
}

func parseCallerFromRequest(ctx *gin.Context, jwtSecret string, database *gorm.DB, reqID string) *types.Caller {
	if os.Getenv("LEROS_DEV") == "true" {
		return &types.Caller{
			Uin:   1,
			OrgID: 1,
			Kind:  types.CallerKindUser,
			State: types.AuthStateSucc,
		}
	}
	authHeader := ctx.Request.Header.Get("Authorization")
	if authHeader == "" {
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateNil,
		}
	}

	tokenStr := extractTokenFromHeader(authHeader)
	if tokenStr == "" {
		logs.Debugw("no valid token found in request", "authHeader", authHeader, "reqID", reqID)
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateNil,
		}
	}

	userCaller, userErr := parseUserCaller(ctx, tokenStr, jwtSecret, database, reqID)
	if userErr == nil {
		return userCaller
	}

	if workerCaller, workerErr := parseWorkerCaller(tokenStr, jwtSecret); workerErr == nil {
		return workerCaller
	} else {
		logs.Warnw("parse auth token failed", "user_error", userErr, "worker_error", workerErr)
	}
	return failedCaller()
}

func parseUserCaller(ctx *gin.Context, tokenStr, jwtSecret string, database *gorm.DB, reqID string) (*types.Caller, error) {
	claims, err := localauth.ParseUserToken(tokenStr, jwtSecret)
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), 3*time.Second)
	defer cancel()

	userOrg, err := db.GetUserOrgByUin(queryCtx, database, claims.Uin)
	if err != nil {
		logs.Warnw("get user org by uin failed, db error", "error", err, "uin", claims.Uin, "reqID", reqID)
		return &types.Caller{
			Uin:   claims.Uin,
			Kind:  types.CallerKindUser,
			State: types.AuthStateFailed,
		}, nil
	}
	if userOrg == nil {
		logs.Warnw("user org not found", "uin", claims.Uin)
		return &types.Caller{
			Uin:   claims.Uin,
			Kind:  types.CallerKindUser,
			State: types.AuthStateFailed,
		}, nil
	}
	return &types.Caller{
		Uin:   userOrg.Uin,
		OrgID: userOrg.OrgID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, nil
}

func parseWorkerCaller(tokenStr, jwtSecret string) (*types.Caller, error) {
	claims, err := localauth.ParseWorkerToken(tokenStr, jwtSecret)
	if err != nil {
		return nil, err
	}
	return &types.Caller{
		Uin:      0,
		OrgID:    claims.OrgID,
		WorkerID: claims.WorkerID,
		Kind:     types.CallerKindWorker,
		State:    types.AuthStateSucc,
	}, nil
}

func failedCaller() *types.Caller {
	return &types.Caller{
		Uin:   0,
		OrgID: 0,
		State: types.AuthStateFailed,
	}
}

func extractTokenFromHeader(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return strings.TrimSpace(authHeader)
}
