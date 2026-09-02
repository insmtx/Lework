package handler

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
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
// @Description 上传文件到系统。请求体以 multipart/form-data 流式解析，purpose/source_id/local-path 等表单字段必须位于 file part 之前，file 之后的字段不会被读取。
// @Tags File
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "文件（须为最后一个 form part）"
// @Param purpose formData string false "文件用途（默认 attachment；须在 file part 之前）"
// @Param source_id formData string false "来源ID（可选；须在 file part 之前）"
// @Param local-path formData string false "本地路径（composer 上传用，可选；须在 file part 之前）"
// @Success 200 {object} dto.Response "上传成功"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Router /files/upload [post]
func (h *FileHandler) UploadFile(ctx *gin.Context) {
	mr, err := ctx.Request.MultipartReader()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid multipart request"))
		return
	}

	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "not authenticated"))
		return
	}

	purpose := filestore.PurposeAttachment
	var sourceID, localPath string
	var filePart *multipart.Part

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid multipart request"))
			return
		}
		switch part.FormName() {
		case "file":
			// file 必须作为最后一个有效 part：请求体是单遍流式解析，
			// 一旦命中 file 就停止读字段（继续 NextPart 会排空 file 内容），
			// 其后的任何字段都不会生效，仅在上传成功后整体排空。
			filePart = part
		case "purpose":
			v, err := readMultipartField(part, 64)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid multipart request"))
				return
			}
			if v != "" {
				purpose = v
			}
		case "source_id":
			v, err := readMultipartField(part, 256)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid multipart request"))
				return
			}
			sourceID = v
		case "local-path":
			v, err := readMultipartField(part, 1024)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid multipart request"))
				return
			}
			localPath = v
		default:
			io.Copy(io.Discard, part)
		}
		if filePart != nil {
			break
		}
	}

	if filePart == nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file is required"))
		return
	}

	// purpose/source_id 会拼入对象存储 key 的路径段，限制字符集与长度，
	// 避免破坏 key 结构（路径分隔符、".." 等），给出明确的 4xx 而不是底层 500。
	if !validKeySegment(purpose) {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "invalid purpose"))
		return
	}
	if sourceID != "" && !validKeySegment(sourceID) {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "invalid source_id"))
		return
	}

	if localPath != "" {
		if err := filestore.ValidateComposerUploadFilename(localPath); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "unsupported file type"))
			return
		}
	}

	result, err := h.service.UploadFile(ctx, &contract.UploadFileRequest{
		OrgID:        caller.OrgID,
		OwnerID:      caller.Uin,
		File:         filePart,
		Filename:     filePart.FileName(),
		MimeType:     filePart.Header.Get("Content-Type"),
		Purpose:      purpose,
		SourceID:     sourceID,
		RelativePath: localPath,
	})
	if err != nil {
		if errors.Is(err, filestore.ErrUploadTooLarge) {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "file size exceeds maximum allowed size"))
			return
		}
		if errors.Is(err, filestore.ErrEmptyFile) {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, "empty file is not allowed"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "upload file failed"))
		return
	}

	drainMultipartReader(mr)

	ctx.JSON(http.StatusOK, dto.Success(result))
}

func readMultipartField(part *multipart.Part, maxLen int) (string, error) {
	b, err := io.ReadAll(io.LimitReader(part, int64(maxLen+1)))
	if err != nil {
		return "", err
	}
	if len(b) > maxLen {
		return "", errMultipartFieldTooLarge
	}
	return strings.TrimSpace(string(b)), nil
}

// errMultipartFieldTooLarge 表示 multipart 文本字段超过允许的最大长度。
var errMultipartFieldTooLarge = errors.New("multipart form field too large")

// keySegmentRe 允许的 multipart 字段字符集（字母数字及 _.-，用于拼入对象存储 key 的路径段）。
var keySegmentRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// validKeySegment 校验将拼入对象存储 key 的字段值（purpose / source_id），
// 拒绝路径分隔符、空白、".." 及超长输入。
func validKeySegment(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return keySegmentRe.MatchString(s)
}

// drainMultipartReader 消费 multipart 剩余内容，保证请求体被完整读取、连接可复用。
func drainMultipartReader(mr *multipart.Reader) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			return
		}
		io.Copy(io.Discard, part)
	}
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
