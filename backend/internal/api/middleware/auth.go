package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/types"
)

// RequireCallerOrg 确保请求方已通过身份验证且归属于某个组织。
// 必须在 CallerMiddleware 之后注册，依赖其写入 gin.Context 的 Caller 信息。
// 验证失败时立即以 401 中止请求链，不进入后续 handler。
func RequireCallerOrg() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, _ := localauth.FromGinContext(ctx)
		if caller == nil || caller.OrgID == 0 || caller.State != types.AuthStateSucc {
			ctx.AbortWithStatusJSON(
				http.StatusUnauthorized,
				dto.Error(dto.CodeUnauthorized, "not authenticated"),
			)
			return
		}
		ctx.Next()
	}
}
