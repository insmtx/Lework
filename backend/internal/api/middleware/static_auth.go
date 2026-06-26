package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

const headerAppKey = "X-App-Key"

func StaticAuth(serverAppKey, jwtSecret string, db *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if serverAppKey != "" {
			key := strings.TrimSpace(ctx.GetHeader(headerAppKey))
			if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(serverAppKey)) == 1 {
				localauth.WithGinContext(ctx, types.SystemIdentity(), &types.Trace{})
				ctx.Next()
				return
			}
		}

		caller := parseCallerFromRequest(ctx, jwtSecret, db, "")
		if caller.State == types.AuthStateSucc {
			ctx.Next()
			return
		}

		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}
