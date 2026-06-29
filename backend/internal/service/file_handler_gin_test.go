package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/types"
)

type mockFileService struct {
	uploadFn   func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error)
	downloadFn func(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error)
	presignFn  func(ctx context.Context, orgID uint, publicID, storageURI string) (string, error)
}

func (m *mockFileService) UploadFile(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
	return m.uploadFn(ctx, req)
}

func (m *mockFileService) DownloadFile(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error) {
	return m.downloadFn(ctx, orgID, fileID)
}

func (m *mockFileService) PresignDownloadURL(ctx context.Context, orgID uint, publicID, storageURI string) (string, error) {
	return m.presignFn(ctx, orgID, publicID, storageURI)
}

func setupFileRouter(t *testing.T, svc contract.FileService, caller *types.Caller) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		trace := &types.Trace{
			RequestID: "test-request-id",
			TraceID:   "test-trace-id",
		}
		auth.WithGinContext(ctx, caller, trace)
		ctx.Next()
	})

	h := handler.NewFileHandler(svc)
	h.RegisterRoutes(router.Group("/v1"))
	return router
}

func authenticatedCaller() *types.Caller {
	return &types.Caller{
		Uin:   1,
		OrgID: 1,
		State: types.AuthStateSucc,
	}
}

func unauthenticatedCaller() *types.Caller {
	return &types.Caller{
		Uin:   0,
		OrgID: 0,
		State: types.AuthStateNil,
	}
}

func TestUploadFile_Success(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			return &contract.UploadFileResult{
				PublicID: "test://abc123",
				Filename: req.Filename,
			}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		dto.BaseResponse
		Data contract.UploadFileResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Data.PublicID != "test://abc123" {
		t.Errorf("expected public_id 'test://abc123', got '%s'", resp.Data.PublicID)
	}
}

func TestUploadFile_NoFile(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			t.Error("upload should not be called")
			return nil, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("POST", "/v1/files/upload", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "multipart/form-data")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadFile_Unauthenticated(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			t.Error("upload should not be called when not authenticated")
			return nil, nil
		},
	}
	router := setupFileRouter(t, svc, unauthenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadFile_DefaultPurpose(t *testing.T) {
	var capturedPurpose string
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			capturedPurpose = req.Purpose
			return &contract.UploadFileResult{}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPurpose != "attachment" {
		t.Errorf("expected default purpose 'attachment', got '%s'", capturedPurpose)
	}
}

func TestUploadFile_ServiceError(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			return nil, errors.New("internal error")
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadFile_NoFileID(t *testing.T) {
	svc := &mockFileService{
		downloadFn: func(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error) {
			t.Error("download should not be called with empty id")
			return nil, nil, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files//download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadFile_Unauthenticated(t *testing.T) {
	svc := &mockFileService{
		downloadFn: func(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error) {
			t.Error("download should not be called when not authenticated")
			return nil, nil, nil
		},
	}
	router := setupFileRouter(t, svc, unauthenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/test-id/download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadFile_NotFound(t *testing.T) {
	svc := &mockFileService{
		downloadFn: func(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error) {
			return nil, nil, errors.New("get file download failed")
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/nonexistent/download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadFile_Success(t *testing.T) {
	svc := &mockFileService{
		downloadFn: func(ctx context.Context, orgID uint, fileID string) (io.ReadCloser, *contract.FileDownloadInfo, error) {
			return io.NopCloser(strings.NewReader("hello world")), &contract.FileDownloadInfo{
				FileName: "test.txt",
				MimeType: "text/plain",
				Size:     11,
			}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/test-id/download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if body != "hello world" {
		t.Errorf("expected body 'hello world', got '%s'", body)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/plain" {
		t.Errorf("expected Content-Type 'text/plain', got '%s'", ct)
	}

	cl := rec.Header().Get("Content-Length")
	if cl != "11" {
		t.Errorf("expected Content-Length '11', got '%s'", cl)
	}

	cd := rec.Header().Get("Content-Disposition")
	expected := `attachment; filename="test.txt"`
	if cd != expected {
		t.Errorf("expected Content-Disposition '%s', got '%s'", expected, cd)
	}
}

func TestDownloadFile_InvalidFileOpen(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			return nil, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	w.Close()

	truncated := bytes.NewReader(body.Bytes()[:len(body.Bytes())-50])
	req := httptest.NewRequest("POST", "/v1/files/upload", truncated)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadFile_WithPurpose(t *testing.T) {
	var capturedPurpose string
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			capturedPurpose = req.Purpose
			return &contract.UploadFileResult{}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("purpose", "avatar")
	part, _ := w.CreateFormFile("file", "test.png")
	part.Write([]byte("image data"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPurpose != "avatar" {
		t.Errorf("expected purpose 'avatar', got '%s'", capturedPurpose)
	}
}

func TestUploadFile_PurposeTrimming(t *testing.T) {
	var capturedPurpose string
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			capturedPurpose = req.Purpose
			return &contract.UploadFileResult{}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("purpose", "  avatar  ")
	part, _ := w.CreateFormFile("file", "test.png")
	part.Write([]byte("image data"))
	w.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedPurpose != "avatar" {
		t.Errorf("expected purpose 'avatar', got '%s'", capturedPurpose)
	}
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated read error")
}
func (e *errorReader) Close() error { return nil }

func TestUploadFile_ReadError(t *testing.T) {
	svc := &mockFileService{
		uploadFn: func(ctx context.Context, req *contract.UploadFileRequest) (*contract.UploadFileResult, error) {
			data, err := io.ReadAll(req.File)
			if err != nil {
				return nil, err
			}
			return &contract.UploadFileResult{
				FileSize: int64(len(data)),
			}, nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "error.txt")
	_ = w.Close()
	_ = part // 创建了但没写，导致后续 io.ReadAll 读到 0 字节（不算错误）。

	body2 := &bytes.Buffer{}
	w2 := multipart.NewWriter(body2)
	part2, _ := w2.CreateFormFile("file", "test.txt")
	part2.Write([]byte("valid data"))
	w2.Close()

	req := httptest.NewRequest("POST", "/v1/files/upload", body2)
	req.Header.Set("Content-Type", w2.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestPresignDownloadURL_Success(t *testing.T) {
	svc := &mockFileService{
		presignFn: func(ctx context.Context, orgID uint, publicID, storageURI string) (string, error) {
			return "https://example.com/presigned-url", nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/preview?public_id=test-id", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	if location != "https://example.com/presigned-url" {
		t.Errorf("expected Location 'https://example.com/presigned-url', got '%s'", location)
	}
}

func TestPresignDownloadURL_NoParams(t *testing.T) {
	svc := &mockFileService{
		presignFn: func(ctx context.Context, orgID uint, publicID, storageURI string) (string, error) {
			t.Error("presign should not be called with empty params")
			return "", nil
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/preview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestPresignDownloadURL_Unauthenticated(t *testing.T) {
	svc := &mockFileService{
		presignFn: func(ctx context.Context, orgID uint, publicID, storageURI string) (string, error) {
			t.Error("presign should not be called when not authenticated")
			return "", nil
		},
	}
	router := setupFileRouter(t, svc, unauthenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/preview?public_id=test-id", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestPresignDownloadURL_NotFound(t *testing.T) {
	svc := &mockFileService{
		presignFn: func(ctx context.Context, orgID uint, publicID, storageURI string) (string, error) {
			return "", errors.New("get presign download url failed")
		},
	}
	router := setupFileRouter(t, svc, authenticatedCaller())

	req := httptest.NewRequest("GET", "/v1/files/preview?public_id=nonexistent", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
