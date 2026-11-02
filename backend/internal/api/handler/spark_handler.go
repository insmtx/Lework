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

// SparkReader keeps observation usable on installations without maintenance control.
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
	group.POST("/maintenance", func(c *gin.Context) {
		controller, ok := svc.(interface {
			ChangeMaintenance(context.Context, types.SparkMaintenanceRequest) (types.SparkMaintenanceReceipt, error)
		})
		if !ok {
			sparkError(c, spark.ErrNotConfigured)
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2049))
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var request types.SparkMaintenanceRequest
		if err != nil || len(body) > 2048 || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !spark.ValidMaintenanceRequest(request) {
			c.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "spark_invalid_maintenance_request"))
			return
		}
		receipt, err := controller.ChangeMaintenance(spark.WithWorkflowActor(c.Request.Context(), sparkCaller(c)), request)
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(receipt))
	})
	group.POST("/model", func(c *gin.Context) {
		controller, ok := svc.(interface {
			ChangeModel(context.Context, types.SparkModelRequest) (types.SparkModelControl, error)
		})
		if !ok {
			sparkError(c, spark.ErrNotConfigured)
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2049))
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var request types.SparkModelRequest
		if err != nil || len(body) > 2048 || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !spark.ValidModelRequest(request) {
			c.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "spark_invalid_model_request"))
			return
		}
		result, err := controller.ChangeModel(spark.WithWorkflowActor(c.Request.Context(), sparkCaller(c)), request)
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(result))
	})
	workflow, workflowOK := svc.(interface {
		BeginWorkflow(context.Context, uint, types.SparkWorkflowRequest) (types.SparkWorkflow, error)
		WorkflowCurrent(context.Context) (types.SparkWorkflowControl, error)
		WorkflowGet(context.Context, string) (types.SparkWorkflow, error)
	})
	group.GET("/workflows/current", func(c *gin.Context) {
		if !workflowOK {
			c.JSON(http.StatusOK, dto.Success(types.SparkWorkflowControl{}))
			return
		}
		result, err := workflow.WorkflowCurrent(c.Request.Context())
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(result))
	})
	group.GET("/workflows/:operation_id", func(c *gin.Context) {
		if !workflowOK {
			sparkError(c, spark.ErrNotConfigured)
			return
		}
		result, err := workflow.WorkflowGet(c.Request.Context(), c.Param("operation_id"))
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.Success(result))
	})
	group.POST("/workflows", func(c *gin.Context) {
		if !workflowOK {
			sparkError(c, spark.ErrNotConfigured)
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2049))
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var request types.SparkWorkflowRequest
		if err != nil || len(body) > 2048 || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || !spark.ValidWorkflowRequest(request) {
			c.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "spark_invalid_workflow_request"))
			return
		}
		caller, _ := auth.FromGinContext(c)
		result, err := workflow.BeginWorkflow(c.Request.Context(), caller.Uin, request)
		if err != nil {
			sparkError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, dto.Success(result))
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
	case errors.Is(err, types.ErrSparkWorkflowConflict):
		status, message = http.StatusConflict, "spark_workflow_conflict"
	case errors.Is(err, spark.ErrModelConflict):
		status, message = http.StatusConflict, "spark_model_conflict"
	case errors.Is(err, spark.ErrMaintenanceConflict):
		status, message = http.StatusConflict, "spark_maintenance_conflict"
	case errors.Is(err, spark.ErrNotConfigured):
		message = "spark_not_configured"
	case errors.Is(err, spark.ErrBadResponse):
		status, message = http.StatusBadGateway, "spark_unverified_response"
	}
	c.JSON(status, dto.Error(dto.CodeInternalError, message))
}

func sparkCaller(c *gin.Context) uint { caller, _ := auth.FromGinContext(c); return caller.Uin }
