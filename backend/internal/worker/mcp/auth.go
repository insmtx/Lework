package mcp

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	authorizationHeader = "Authorization"
	apiKeyHeader        = "X-API-Key"
	bearerPrefix        = "Bearer "
)

func requireToken(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		if validateToken(tokenFromRequest(c), expectedToken) {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

func validateToken(token string, expectedToken string) bool {
	// 未配置 token 时不能放行：worker 的 MCP 端口可能绑定在 0.0.0.0，
	// 空 token 等于把工具执行能力暴露给同网段任意客户端。
	if expectedToken == "" {
		return false
	}
	return tokenMatches(token, expectedToken)
}

func tokenFromRequest(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader(authorizationHeader))
	if strings.HasPrefix(authHeader, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	}
	if authHeader != "" {
		return authHeader
	}
	return strings.TrimSpace(c.GetHeader(apiKeyHeader))
}

func tokenMatches(actual string, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
