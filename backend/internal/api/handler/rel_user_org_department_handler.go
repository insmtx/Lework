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

// @Summary 创建成员部门关系
// @Description 创建成员与部门的关联
// @Tags MemberDepartment
// @Accept json
// @Produce json
// @Param body body contract.CreateMemberDepartmentRequest true "创建成员部门关系请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateMemberDepartment [post]
func (h *MemberDepartmentHandler) CreateMemberDepartment(ctx *gin.Context) {
	var req contract.CreateMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.CreateMemberDepartment(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type getMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 获取成员部门关系
// @Description 根据 ID 获取成员部门关系详情
// @Tags MemberDepartment
// @Accept json
// @Produce json
// @Param body body handler.getMemberDepartmentRequest true "获取成员部门关系请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetMemberDepartment [post]
func (h *MemberDepartmentHandler) GetMemberDepartment(ctx *gin.Context) {
	var req getMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetMemberDepartment(ctx, req.ID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type updateMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateMemberDepartmentRequest
}

// @Summary 更新成员部门关系
// @Description 更新成员部门关联信息
// @Tags MemberDepartment
// @Accept json
// @Produce json
// @Param body body handler.updateMemberDepartmentRequest true "更新成员部门关系请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateMemberDepartment [post]
func (h *MemberDepartmentHandler) UpdateMemberDepartment(ctx *gin.Context) {
	var req updateMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.UpdateMemberDepartment(ctx, req.ID, &req.UpdateMemberDepartmentRequest)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type deleteMemberDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除成员部门关系
// @Description 根据 ID 删除成员部门关联
// @Tags MemberDepartment
// @Accept json
// @Produce json
// @Param body body handler.deleteMemberDepartmentRequest true "删除成员部门关系请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteMemberDepartment [post]
func (h *MemberDepartmentHandler) DeleteMemberDepartment(ctx *gin.Context) {
	var req deleteMemberDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := h.service.DeleteMemberDepartment(ctx, req.ID); err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询成员部门关系列表
// @Description 分页查询成员部门关系列表
// @Tags MemberDepartment
// @Accept json
// @Produce json
// @Param body body contract.ListMemberDepartmentsRequest true "查询成员部门关系列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListMemberDepartments [post]
func (h *MemberDepartmentHandler) ListMemberDepartments(ctx *gin.Context) {
	var req contract.ListMemberDepartmentsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	req.Fill()
	result, err := h.service.ListMemberDepartments(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}
