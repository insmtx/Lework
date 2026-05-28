// Package router 提供 Worker HTTP 服务的 Gin 路由组装。
//
// 该包是 worker 端 HTTP 路由的统一入口，类比 api.SetupRouter 之于控制面服务。
// 将路由定义从启动逻辑中分离，便于测试和维护。
package router

import (
	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/modelrouter"
	runtimemcp "github.com/insmtx/Leros/backend/internal/runtime/mcp"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
)

// SetupRouter 创建 worker HTTP 服务的 Gin 引擎并注册所有路由。
//
// 注册以下端点：
//   - GET /health — 健康检查
//   - /v1/mcp — MCP 协议端点（由 runtimemcp 注册）
//   - /v1/chat/completions — OpenAI Chat Completions 模型路由
//   - /v1/messages — Anthropic Messages 模型路由
//   - /v1/responses — OpenAI Responses 模型路由
func SetupRouter() *gin.Engine {
	r := gin.New()

	r.GET("/health", workerHealth)

	v1 := r.Group("/v1")
	runtimemcp.RegisterRoutes(v1, runtimemcp.NewServer())
	modelrouter.RegisterRoutes(v1)

	logs.Infof("Worker router initialized: health, /v1/mcp, /v1/chat/completions, /v1/messages, /v1/responses")
	return r
}

type healthResponse struct {
	Status   string `json:"status"`
	Healthy  bool   `json:"healthy"`
	OrgID    uint   `json:"org_id"`
	WorkerID uint   `json:"worker_id"`
}

func workerHealth(c *gin.Context) {
	orgID := identity.OrgID()
	workerID := identity.WorkerID()
	healthy := orgID != 0 && workerID != 0

	status := "healthy"
	code := 200
	if !healthy {
		status = "unhealthy"
		code = 503
	}

	c.JSON(code, healthResponse{
		Status:   status,
		Healthy:  healthy,
		OrgID:    orgID,
		WorkerID: workerID,
	})
}
