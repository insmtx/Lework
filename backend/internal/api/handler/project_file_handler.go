package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// ProjectFileHandler 项目文件相关接口
type ProjectFileHandler struct {
	service contract.ProjectService
}

// NewProjectFileHandler 创建项目文件处理器
func NewProjectFileHandler(service contract.ProjectService) *ProjectFileHandler {
	return &ProjectFileHandler{service: service}
}

// RegisterRoutes 注册路由
func (h *ProjectFileHandler) RegisterRoutes(r gin.IRouter) {
	r.GET("/projects/:project_id/files", h.GetProjectFileTree)
	r.GET("/projects/:project_id/files/download", h.DownloadProjectFile)
	r.GET("/projects/:project_id/memory", h.GetProjectMemory)
}

// GetProjectFileTree 获取项目文件树
// @Summary 获取项目文件树
// @Description 获取项目 artifacts/ 和 uploads/ 目录的文件树，可通过 resource_type 参数筛选。
// @Description 文件节点包含 created_at 字段（Unix 秒级时间戳），表示该文件关联记录创建的时间，未找到时为 0。
// @Tags Project
// @Produce json
// @Param project_id path string true "项目 public_id"
// @Param resource_type query string false "资源类型：user_upload | artifact，不传则返回全部"
// @Param task_id query string false "Task public ID，传入时仅返回该任务的产物文件"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/files [get]
func (h *ProjectFileHandler) GetProjectFileTree(ctx *gin.Context) {
	projectID := strings.TrimSpace(ctx.Param("project_id"))
	if projectID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "project_id is required"))
		return
	}

	resourceType := strings.TrimSpace(ctx.Query("resource_type"))
	taskID := strings.TrimSpace(ctx.Query("task_id"))

	result, err := h.service.GetProjectFileTree(ctx, projectID, resourceType, taskID)
	if err != nil {
		handleProjectFileServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// DownloadProjectFile 下载项目中的文件
// @Summary 下载项目文件
// @Description 通过文件路径下载项目仓库中的文件
// @Tags Project
// @Produce octet-stream
// @Param project_id path string true "项目 public_id"
// @Param filepath path string true "文件相对路径，如 /src/main.go"
// @Success 200 {file} binary "文件内容"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/files/{filepath} [get]
func (h *ProjectFileHandler) DownloadProjectFile(ctx *gin.Context) {
	projectID := strings.TrimSpace(ctx.Param("project_id"))
	if projectID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "project_id is required"))
		return
	}

	filePath := strings.TrimSpace(ctx.Query("path"))
	if filePath == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file path is required"))
		return
	}

	reader, contentType, size, err := h.service.DownloadProjectFile(ctx, projectID, filePath)
	if err != nil {
		handleProjectFileServiceError(ctx, err)
		return
	}
	defer reader.Close()

	ctx.Header("Content-Type", contentType)
	if size > 0 {
		ctx.Header("Content-Length", fmt.Sprintf("%d", size))
	}
	ctx.Status(http.StatusOK)
	if _, err := io.Copy(ctx.Writer, reader); err != nil {
		ctx.Error(err)
	}
}

// GetProjectMemory 获取项目记忆
// @Summary 获取项目记忆
// @Description 根据 project_id 获取项目的持久记忆条目
// @Tags Project
// @Produce json
// @Param project_id path string true "项目 public_id"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/memory [get]
func (h *ProjectFileHandler) GetProjectMemory(ctx *gin.Context) {
	projectID := strings.TrimSpace(ctx.Param("project_id"))
	if projectID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "project_id is required"))
		return
	}

	result, err := h.service.GetProjectMemory(ctx, projectID)
	if err != nil {
		handleProjectFileServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleProjectFileServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	switch errMsg {
	case "user not authenticated or org not set":
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "project not found", "file not found", "directory not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "file access denied":
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, errMsg))
	case "public_id is required",
		"file_public_id is required",
		"file path is required",
		"filename is required",
		"invalid parent path",
		"cannot download a directory":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
