package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

func setupStaticTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	dir := t.TempDir()
	cfg := &config.StorageConfig{
		Driver:     "local",
		LocalDir:   dir,
		Bucket:     "test-bucket",
		SignSecret: "test-secret-key-for-presign",
		BaseURL:    "http://localhost:8080",
	}
	if err := filestore.Init(cfg); err != nil {
		t.Fatalf("init storage: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterStaticRoutes(r)
	return r
}

func TestPresignUploadHandler_Success(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/static/test-bucket/path/to/file.txt?presign=1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Presign-Expires-At") == "" {
		t.Error("expected X-Presign-Expires-At header")
	}
	if w.Body.String() == "" {
		t.Error("expected non-empty URL body")
	}
}

func TestPresignDownloadHandler_Success(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/test-bucket/path/to/file.txt?presign=1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Presign-Expires-At") == "" {
		t.Error("expected X-Presign-Expires-At header")
	}
	if w.Body.String() == "" {
		t.Error("expected non-empty URL body")
	}
}

func TestPresignUploadHandler_MissingPresignParam(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/static/test-bucket/path/to/file.txt", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestPresignDownloadHandler_MissingPresignParam(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/test-bucket/path/to/file.txt", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestPresignUploadHandler_EmptyBucket(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/static//path/to/file.txt?presign=1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestPresignUploadHandler_EmptyKey(t *testing.T) {
	r := setupStaticTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/static/test-bucket/?presign=1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}
