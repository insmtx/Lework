package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type MemberDepartmentHandler struct {
	service contract.MemberDepartmentService
}

func NewMemberDepartmentHandler(service contract.MemberDepartmentService) *MemberDepartmentHandler {
	return &MemberDepartmentHandler{service: service}
}

func (h *MemberDepartmentHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateMemberDepartment", h.CreateMemberDepartment)
	r.POST("/GetMemberDepartment", h.GetMemberDepartment)
	r.POST("/UpdateMemberDepartment", h.UpdateMemberDepartment)
	r.POST("/DeleteMemberDepartment", h.DeleteMemberDepartment)
	r.POST("/ListMemberDepartments", h.ListMemberDepartments)
}

func RegisterMemberDepartmentRoutes(r gin.IRouter, service contract.MemberDepartmentService) {
	h := NewMemberDepartmentHandler(service)
	h.RegisterRoutes(r)
}

func (h *MemberDepartmentHandler) CreateMemberDepartment(ctx *gin.Context) {
	var req contract.CreateMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.CreateMemberDepartment(ctx, &req)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type getMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

func (h *MemberDepartmentHandler) GetMemberDepartment(ctx *gin.Context) {
	var req getMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetMemberDepartment(ctx, req.ID)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type updateMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateMemberDepartmentRequest
}

func (h *MemberDepartmentHandler) UpdateMemberDepartment(ctx *gin.Context) {
	var req updateMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.UpdateMemberDepartment(ctx, req.ID, &req.UpdateMemberDepartmentRequest)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type deleteMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

func (h *MemberDepartmentHandler) DeleteMemberDepartment(ctx *gin.Context) {
	var req deleteMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := h.service.DeleteMemberDepartment(ctx, req.ID); err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

func (h *MemberDepartmentHandler) ListMemberDepartments(ctx *gin.Context) {
	var req contract.ListMemberDepartmentsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	req.Fill()
	result, err := h.service.ListMemberDepartments(ctx, &req)
	if err != nil {
		handleOrganizationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}
