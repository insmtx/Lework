package mcp

import "github.com/gin-gonic/gin"

const routePath = "/mcp"

// RegisterRoutes 挂载 Leros MCP 可流式 HTTP 端点。
func RegisterRoutes(r gin.IRouter, srv *Server) {
	if srv == nil {
		srv = NewServer()
	}

	handlers := []gin.HandlerFunc{requireToken(srv.authToken), gin.WrapH(srv.Handler())}
	r.Any(routePath, handlers...)
	r.Any(routePath+"/*path", handlers...)
}
