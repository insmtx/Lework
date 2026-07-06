package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type DepartmentHandler struct {
	service contract.DepartmentService
}

func NewDepartmentHandler(service contract.DepartmentService) *DepartmentHandler {
	return &DepartmentHandler{service: service}
}

func (h *DepartmentHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateDepartment", h.CreateDepartment)
	r.POST("/GetDepartment", h.GetDepartment)
	r.POST("/UpdateDepartment", h.UpdateDepartment)
	r.POST("/DeleteDepartment", h.DeleteDepartment)
	r.POST("/ListDepartments", h.ListDepartments)
}

func RegisterDepartmentRoutes(r gin.IRouter, service contract.DepartmentService) {
	h := NewDepartmentHandler(service)
	h.RegisterRoutes(r)
}

func (h *DepartmentHandler) CreateDepartment(ctx *gin.Context) {
	var req contract.CreateDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.CreateDepartment(ctx, &req)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type getDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

func (h *DepartmentHandler) GetDepartment(ctx *gin.Context) {
	var req getDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetDepartment(ctx, req.ID)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type updateDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateDepartmentRequest
}

func (h *DepartmentHandler) UpdateDepartment(ctx *gin.Context) {
	var req updateDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.UpdateDepartment(ctx, req.ID, &req.UpdateDepartmentRequest)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type deleteDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

func (h *DepartmentHandler) DeleteDepartment(ctx *gin.Context) {
	var req deleteDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := h.service.DeleteDepartment(ctx, req.ID); err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

func (h *DepartmentHandler) ListDepartments(ctx *gin.Context) {
	var req contract.ListDepartmentsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	req.Fill()
	result, err := h.service.ListDepartments(ctx, &req)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleOrganizationServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()
	switch errMsg {
	case "department not found",
		"member department relation not found",
		"parent department not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "permission denied",
		"department name already exists",
		"department has child departments":
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, errMsg))
	case "id is required",
		"user not authenticated",
		"org not set",
		"org_id is required",
		"user_id is required",
		"uin is required",
		"department_id is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
