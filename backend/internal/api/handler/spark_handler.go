package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/spark"
	"github.com/insmtx/Leros/backend/types"
)

// SparkReader prevents this read-only API from acquiring host mutation capabilities.
type SparkReader interface {
	OrgID() uint
	Status(context.Context) (types.SparkStatus, error)
	Preflight(context.Context) (types.SparkPreflight, error)
}

// SparkAdminChecker revalidates current privileges through the active edition adapter.
type SparkAdminChecker interface {
	IsOrgCreator(context.Context, uint, uint) (bool, error)
}

// RegisterSparkRoutes exposes one installation-bound host only to its organization administrator.
func RegisterSparkRoutes(r gin.IRouter, svc SparkReader, admin SparkAdminChecker) {
	group := r.Group("/ops/spark")
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		caller, _ := auth.FromGinContext(c)
		if caller == nil || caller.State != types.AuthStateSucc || caller.Uin == 0 || caller.OrgID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, "spark_login_required"))
			return
		}
		if caller.Kind != types.CallerKindUser || caller.WorkerID != 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, "spark_admin_required"))
			return
		}
		if svc == nil || svc.OrgID() == 0 {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "spark_not_configured"))
			return
		}
		if caller.OrgID != svc.OrgID() {
			c.AbortWithStatusJSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, "spark_organization_mismatch"))
			return
		}
		if admin == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "spark_authorization_unavailable"))
			return
		}
		allowed, err := admin.IsOrgCreator(c, caller.OrgID, caller.Uin)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "spark_authorization_unavailable"))
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, "spark_admin_required"))
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "spark_target_is_installation_bound"))
			return
		}
		c.Next()
	})
	group.GET("/status", func(c *gin.Context) {
		result, err := svc.Status(c.Request.Context())
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(result))
	})
	group.POST("/preflight", func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1025))
		body = bytes.TrimSpace(body)
		if len(body) == 0 {
			body = []byte("{}")
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var request struct{}
		if err != nil || len(body) > 1024 || bytes.Equal(body, []byte("null")) || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			c.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "spark_preflight_accepts_no_parameters"))
			return
		}
		result, err := svc.Preflight(c.Request.Context())
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(result))
	})
}
func sparkError(c *gin.Context, err error) {
	status, message := http.StatusServiceUnavailable, "spark_unavailable"
	switch {
	case errors.Is(err, spark.ErrNotConfigured):
		message = "spark_not_configured"
	case errors.Is(err, spark.ErrBadResponse):
		status, message = http.StatusBadGateway, "spark_unverified_response"
	}
	c.JSON(status, dto.Error(dto.CodeInternalError, message))
}
