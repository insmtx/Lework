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
	r.POST("/projects/:project_id/files/upload", h.UploadProjectFile)
	r.GET("/projects/:project_id/files/download", h.DownloadProjectFile)
	r.GET("/projects/:project_id/files/preview", h.PreviewProjectFile)
	r.GET("/projects/:project_id/memory", h.GetProjectMemory)
	r.POST("/projects/:project_id/AddFile", h.DeprecatedAddProjectFile)
}

// GetProjectFileTree 获取项目文件树
// @Summary 获取项目文件树
// @Description 获取项目 artifacts/ 和 uploads/ 目录的文件树，可通过 path 参数指定子目录。
// @Description 文件节点包含 created_at 字段（Unix 秒级时间戳），表示该文件在 Gitea 仓库中的首次 commit 时间，未找到时为 0。
// @Tags Project
// @Produce json
// @Param project_id path string true "项目 public_id"
// @Param path query string false "起始目录相对路径，如 artifacts，默认返回全量"
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

	parentPath := strings.TrimSpace(ctx.Query("path"))

	result, err := h.service.GetProjectFileTree(ctx, projectID, parentPath, 0)
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

	reader, mimeType, size, err := h.service.DownloadProjectFile(ctx, projectID, filePath)
	if err != nil {
		handleProjectFileServiceError(ctx, err)
		return
	}
	defer reader.Close()

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	ctx.Header("Content-Type", mimeType)
	if size > 0 {
		ctx.Header("Content-Length", fmt.Sprintf("%d", size))
	}
	ctx.Status(http.StatusOK)
	if _, err := io.Copy(ctx.Writer, reader); err != nil {
		ctx.Error(err)
	}
}

// PreviewProjectFile 预览项目中的文件
// @Summary 预览项目文件
// @Description 代理 Gitea raw 内容，流式透传原始文件内容，浏览器可根据 Content-Type 内嵌预览
// @Tags Project
// @Produce octet-stream
// @Param project_id path string true "项目 public_id"
// @Param path query string true "文件相对路径，如 artifacts/example.png"
// @Success 200 {file} binary "文件内容"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/files/preview [get]
func (h *ProjectFileHandler) PreviewProjectFile(ctx *gin.Context) {
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

	reader, contentType, size, err := h.service.PreviewProjectFile(ctx, projectID, filePath)
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

// UploadProjectFile 上传文件到项目
// @Summary 上传项目文件
// @Description 上传文件到项目 Upload 目录，同名文件自动追加序列号
// @Tags Project
// @Accept multipart/form-data
// @Produce json
// @Param project_id path string true "项目 public_id"
// @Param file formData file true "上传的文件"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/files/upload [post]
func (h *ProjectFileHandler) UploadProjectFile(ctx *gin.Context) {
	projectID := strings.TrimSpace(ctx.Param("project_id"))
	if projectID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "project_id is required"))
		return
	}

	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file is required"))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "failed to open uploaded file"))
		return
	}
	defer file.Close()

	result, err := h.service.UploadProjectFile(ctx, projectID, file, fileHeader.Filename)
	if err != nil {
		handleProjectFileServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
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

// AddProjectFile 将已上传文件关联到项目
// @Summary 关联文件到项目
// @Description 将已通过 /v1/files/upload 上传的文件关联到指定项目
// @Tags Project
// @Accept json
// @Produce json
// @Param project_id path string true "项目 public_id"
// @Param request body contract.AddFileRequest true "文件信息"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /projects/{project_id}/AddFile [post]
func (h *ProjectFileHandler) DeprecatedAddProjectFile(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, dto.Success(nil))
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
