package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

type FileHandler struct {
	service contract.FileService
}

func NewFileHandler(service contract.FileService) *FileHandler {
	return &FileHandler{service: service}
}

// RegisterAuthedRoutes 注册需要登录（RequireCallerOrg）的文件写操作路由。
func (h *FileHandler) RegisterAuthedRoutes(r gin.IRouter) {
	r.POST("/files/upload", h.UploadFile)
}

// RegisterAnonymousRoutes 注册允许匿名访问的文件读操作路由。
// 匿名访问仅放行体系级（system）共享资源；组织私有文件仍需登录。
func (h *FileHandler) RegisterAnonymousRoutes(r gin.IRouter) {
	r.GET("/files/download", h.DownloadFile)
	r.GET("/files/preview", h.PresignDownloadURL)
}

// @Summary 上传文件
// @Description 上传文件到系统
// @Tags File
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "上传文件"
// @Param purpose formData string false "文件用途（默认 attachment）"
// @Param source_id formData string false "来源ID（可选）"
// @Success 200 {object} dto.Response "上传成功"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Router /files/upload [post]
func (h *FileHandler) UploadFile(ctx *gin.Context) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file is required"))
		return
	}

	purpose := strings.TrimSpace(ctx.PostForm("purpose"))
	if purpose == "" {
		purpose = filestore.PurposeAttachment
	}

	sourceID := strings.TrimSpace(ctx.PostForm("source_id"))

	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "failed to open file"))
		return
	}
	defer file.Close()

	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "not authenticated"))
		return
	}

	localPath := strings.TrimSpace(ctx.PostForm("local-path"))
	if localPath != "" {
		if err := filestore.ValidateComposerUploadFilename(localPath); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "unsupported file type"))
			return
		}
		if fileHeader.Size == 0 {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "empty file is not allowed"))
			return
		}
	}

	result, err := h.service.UploadFile(ctx, &contract.UploadFileRequest{
		OrgID:    caller.OrgID,
		OwnerID:  caller.Uin,
		File:     file,
		Filename: fileHeader.Filename,
		FileSize: fileHeader.Size,
		MimeType: fileHeader.Header.Get("Content-Type"),
		Purpose:  purpose,
		SourceID: sourceID,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "upload file failed"))
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 下载文件
// @Description 通过 publicID 或 storageURI 流式返回文件内容
// @Tags File
// @Produce octet-stream
// @Param public_id query string false "文件公共ID"
// @Param storage_uri query string false "文件存储URI（格式 {schema}://{bucket}/{key}）"
// @Success 200 {file} binary "文件内容"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "文件不存在"
// @Router /files/download [get]
func (h *FileHandler) DownloadFile(ctx *gin.Context) {
	fileID := strings.TrimSpace(ctx.Query("public_id"))
	storageURI := strings.TrimSpace(ctx.Query("storage_uri"))
	if fileID == "" && storageURI == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id or storage_uri is required"))
		return
	}

	caller, _ := auth.FromGinContext(ctx)
	orgID := uint(0)
	if caller != nil && caller.OrgID != 0 {
		orgID = caller.OrgID
	}

	var reader io.ReadCloser
	var info *contract.FileDownloadInfo
	var err error

	if fileID != "" {
		reader, info, err = h.service.DownloadFile(ctx, orgID, fileID)
	} else {
		// 按 storage_uri 直读对象不区分归属，仅允许已登录方使用。
		if orgID == 0 {
			ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "not authenticated"))
			return
		}
		reader, info, err = h.service.DownloadFileByURI(ctx, orgID, storageURI)
	}
	if err != nil {
		if err.Error() == "get file download failed" {
			ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, "file not found"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "get file download failed"))
		return
	}
	defer reader.Close()

	mimeType := info.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ctx.Header("Content-Type", mimeType)
	ctx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, info.FileName))
	if info.Size > 0 {
		ctx.Header("Content-Length", fmt.Sprintf("%d", info.Size))
	}
	ctx.Status(http.StatusOK)
	if _, err := io.Copy(ctx.Writer, reader); err != nil {
		ctx.Error(err)
	}
}

// @Summary 获取预签名下载地址
// @Description 通过 publicID 或 storageURI 生成预签名下载 URL 并 302 重定向
// @Tags File
// @Produce json
// @Param public_id query string false "文件公共ID"
// @Param storage_uri query string false "文件存储URI（格式 {schema}://{bucket}/{key}）"
// @Success 302 {string} string "重定向到预签名下载 URL"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "文件不存在"
// @Router /files/preview [get]
func (h *FileHandler) PresignDownloadURL(ctx *gin.Context) {
	fileID := strings.TrimSpace(ctx.Query("public_id"))
	storageURI := strings.TrimSpace(ctx.Query("storage_uri"))
	if fileID == "" && storageURI == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id or storage_uri is required"))
		return
	}

	caller, _ := auth.FromGinContext(ctx)
	orgID := uint(0)
	if caller != nil && caller.OrgID != 0 {
		orgID = caller.OrgID
	}

	// 匿名请求仅允许通过 public_id 访问体系级（system）共享资源；
	// 直接按 storage_uri 预签名（不区分归属）只限已登录方使用。
	if fileID == "" && orgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "not authenticated"))
		return
	}

	url, err := h.service.PresignDownloadURL(ctx, orgID, fileID, storageURI)
	if err != nil {
		if err.Error() == "get presign download url failed" {
			ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, "file not found"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "get presign download url failed"))
		return
	}

	logs.InfoContextf(ctx, "redirect to presigned url: %s", url)
	ctx.Redirect(http.StatusFound, url)
}
