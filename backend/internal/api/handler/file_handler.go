package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type FileHandler struct {
	service contract.FileService
}

func NewFileHandler(service contract.FileService) *FileHandler {
	return &FileHandler{service: service}
}

func (h *FileHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/files/upload", h.UploadFile)
	r.GET("/files/:id/download", h.DownloadFile)
}

// @Summary 上传文件
// @Description 上传文件到系统
// @Tags File
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "上传文件"
// @Param purpose formData string false "文件用途（默认 attachment）"
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
		purpose = "attachment"
	}

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

	result, err := h.service.UploadFile(ctx, &contract.UploadFileRequest{
		OrgID:    caller.OrgID,
		OwnerID:  caller.Uin,
		File:     file,
		Filename: fileHeader.Filename,
		FileSize: fileHeader.Size,
		MimeType: fileHeader.Header.Get("Content-Type"),
		Purpose:  purpose,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "upload file failed"))
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 下载文件
// @Description 流式返回文件内容
// @Tags File
// @Produce octet-stream
// @Param id path string true "文件ID"
// @Success 200 {file} binary "文件内容"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "文件不存在"
// @Router /files/{id}/download [get]
func (h *FileHandler) DownloadFile(ctx *gin.Context) {
	fileID := strings.TrimSpace(ctx.Param("id"))
	if fileID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file id is required"))
		return
	}

	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "not authenticated"))
		return
	}

	reader, info, err := h.service.DownloadFile(ctx, caller.OrgID, fileID)
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
