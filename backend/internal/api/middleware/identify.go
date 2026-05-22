package middleware

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	ygauth "github.com/ygpkg/yg-go/apis/runtime/auth"
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

	userClaims, err := parseJWTToken(tokenStr, jwtSecret)
	if err != nil {
		logs.Warnw("parse jwt token failed", "error", err)
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateFailed,
		}
	}

	if userClaims.Uin == 0 {
		return &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateFailed,
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), 3*time.Second)
	defer cancel()

	userOrg, err := db.GetUserOrgByUin(queryCtx, database, userClaims.Uin)
	if err != nil {
		logs.Warnw("get user org by uin failed, db error", "error", err, "uin", userClaims.Uin, "reqID", ctx.Request.Header.Get(headerKeyRequestID))
		return &types.Caller{
			Uin:   userClaims.Uin,
			OrgID: 0,
			State: types.AuthStateFailed,
		}
	}

	if userOrg == nil {
		logs.Warnw("user org not found", "uin", userClaims.Uin)
		return &types.Caller{
			Uin:   userClaims.Uin,
			OrgID: 0,
			State: types.AuthStateFailed,
		}
	}

	return &types.Caller{
		Uin:   userClaims.Uin,
		OrgID: userOrg.OrgID,
		State: types.AuthStateSucc,
	}
}

func extractTokenFromHeader(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return strings.TrimSpace(authHeader)
}

func parseJWTToken(tokenStr, jwtSecret string) (*ygauth.UserClaims, error) {
	claims := &ygauth.UserClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}
