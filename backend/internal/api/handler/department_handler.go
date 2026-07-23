package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type DepartmentHandler struct {
	service account.DepartmentRepository
}

func NewDepartmentHandler(service account.DepartmentRepository) *DepartmentHandler {
	return &DepartmentHandler{service: service}
}

func (h *DepartmentHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateDepartment", h.CreateDepartment)
	r.POST("/GetDepartment", h.GetDepartment)
	r.POST("/UpdateDepartment", h.UpdateDepartment)
	r.POST("/DeleteDepartment", h.DeleteDepartment)
	r.POST("/ListDepartment", h.ListDepartment)
}

func RegisterDepartmentRoutes(r gin.IRouter, service account.DepartmentRepository) {
	h := NewDepartmentHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 创建部门
// @Description 创建一个新部门
// @Tags Department
// @Accept json
// @Produce json
// @Param body body contract.CreateDepartmentRequest true "创建部门请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateDepartment [post]
func (h *DepartmentHandler) CreateDepartment(ctx *gin.Context) {
	var req contract.CreateDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.CreateDepartment(ctx, &req.CreateDepartmentInput)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type getDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 获取部门
// @Description 根据 ID 获取部门详情
// @Tags Department
// @Accept json
// @Produce json
// @Param body body handler.getDepartmentRequest true "获取部门请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetDepartment [post]
func (h *DepartmentHandler) GetDepartment(ctx *gin.Context) {
	var req getDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetDepartment(ctx, req.ID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type updateDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateDepartmentRequest
}

// @Summary 更新部门
// @Description 更新部门信息
// @Tags Department
// @Accept json
// @Produce json
// @Param body body handler.updateDepartmentRequest true "更新部门请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateDepartment [post]
func (h *DepartmentHandler) UpdateDepartment(ctx *gin.Context) {
	var req updateDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.UpdateDepartment(ctx, req.ID, &req.UpdateDepartmentInput)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type deleteDepartmentRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除部门
// @Description 根据 ID 删除部门
// @Tags Department
// @Accept json
// @Produce json
// @Param body body handler.deleteDepartmentRequest true "删除部门请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteDepartment [post]
func (h *DepartmentHandler) DeleteDepartment(ctx *gin.Context) {
	var req deleteDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := h.service.DeleteDepartment(ctx, req.ID); err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 部门列表
// @Description 查询部门列表
// @Tags Department
// @Accept json
// @Produce json
// @Param body body contract.ListDepartmentRequest true "查询部门列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListDepartment [post]
func (h *DepartmentHandler) ListDepartment(ctx *gin.Context) {
	var req contract.ListDepartmentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	req.Fill()
	result, err := h.service.ListDepartment(ctx, &req.ListDepartmentInput)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}
