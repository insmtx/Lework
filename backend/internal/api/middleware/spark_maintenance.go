package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

// SparkMaintenanceReader separates admission policy from host state ownership.
type SparkMaintenanceReader interface {
	Read() types.SparkMaintenanceState
}

// SparkMaintenance rejects new interactive work while allowing cancellation, interaction replies and results.
// Worker admission is authoritative for requests already in flight and non-HTTP task sources.
func SparkMaintenance(reader SparkMaintenanceReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reader == nil || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		switch c.FullPath() {
		case "/v1/AddMessage", "/v1/CreateInitialMessage", "/v1/CreateTask", "/v1/CreateSession", "/v1/RunAutomationNow":
		default:
			c.Next()
			return
		}
		state := reader.Read()
		if !state.Known || state.Active {
			code := "spark_maintenance_active"
			if !state.Known {
				code = "maintenance_state_unavailable"
			}
			c.Header("Retry-After", "15")
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, code))
			return
		}
		c.Next()
	}
}
