package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// DesktopPackageDownloadHandler handles desktop installer download stat APIs.
type DesktopPackageDownloadHandler struct {
	service contract.DesktopPackageDownloadService
}

// NewDesktopPackageDownloadHandler creates a desktop installer download stat handler.
func NewDesktopPackageDownloadHandler(service contract.DesktopPackageDownloadService) *DesktopPackageDownloadHandler {
	return &DesktopPackageDownloadHandler{service: service}
}

// RegisterRoutes registers desktop installer download stat routes.
func (h *DesktopPackageDownloadHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/ReportDesktopPackageDownload", h.ReportDesktopPackageDownload)
	r.POST("/GetDesktopPackageDownloadTotal", h.GetDesktopPackageDownloadTotal)
}

// RegisterDesktopPackageDownloadRoutes registers desktop installer download stat routes.
func RegisterDesktopPackageDownloadRoutes(r gin.IRouter, service contract.DesktopPackageDownloadService) {
	h := NewDesktopPackageDownloadHandler(service)
	h.RegisterRoutes(r)
}

// ReportDesktopPackageDownload records one desktop installer download report.
// @Summary 上报桌面端安装包下载
// @Description 上报一次桌面端安装包下载，同一 IP、版本、平台、架构 1 分钟内重复上报不会重复计数。
// @Tags DesktopPackageDownload
// @Accept json
// @Produce json
// @Param body body contract.ReportDesktopPackageDownloadRequest true "下载上报请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ReportDesktopPackageDownload [post]
func (h *DesktopPackageDownloadHandler) ReportDesktopPackageDownload(ctx *gin.Context) {
	var req contract.ReportDesktopPackageDownloadRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	req.ClientIP = ctx.ClientIP()

	result, err := h.service.ReportDesktopPackageDownload(ctx, &req)
	if err != nil {
		handleDesktopPackageDownloadServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// GetDesktopPackageDownloadTotal returns total desktop installer download count.
// @Summary 查询桌面端安装包总下载量
// @Description 查询桌面端安装包累计总下载量。
// @Tags DesktopPackageDownload
// @Accept json
// @Produce json
// @Success 200 {object} dto.Response "成功响应"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetDesktopPackageDownloadTotal [post]
func (h *DesktopPackageDownloadHandler) GetDesktopPackageDownloadTotal(ctx *gin.Context) {
	result, err := h.service.GetDesktopPackageDownloadTotal(ctx)
	if err != nil {
		handleDesktopPackageDownloadServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleDesktopPackageDownloadServiceError(ctx *gin.Context, err error) {
	switch err.Error() {
	case "request is required",
		"version is required",
		"client ip is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
	}
}
