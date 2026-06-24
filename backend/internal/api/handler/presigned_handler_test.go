package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

func setupPresignedTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.StorageConfig{
		Driver:     "local",
		LocalDir:   dir,
		Bucket:     "test-bucket",
		SignSecret: "test-secret",
		BaseURL:    "http://localhost:8080",
	}
	if err := filestore.Init(cfg); err != nil {
		t.Fatalf("init storage: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	staticGroup := r.Group("/static")
	RegisterStaticRoutes(staticGroup)
	RegisterPresignedRoutes(r)
	return r
}

func TestPresignedPutAndGetRoundTrip(t *testing.T) {
	r := setupPresignedTestRouter(t)

	// Step 1: get presign upload URL
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("PUT", "/static/test-bucket/hello.txt?presign=1", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("step1: presign upload URL generation failed: %d %s", w1.Code, w1.Body.String())
	}
	uploadURLStr := w1.Body.String()
	t.Logf("[step1] upload URL: %s", uploadURLStr)

	// Step 2: upload via presigned URL
	parsedURL, err := url.Parse(uploadURLStr)
	if err != nil {
		t.Fatalf("parse upload URL: %v", err)
	}
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", parsedURL.RequestURI(), strings.NewReader("hello presigned world"))
	req2.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("step2: presign put failed: %d %s", w2.Code, w2.Body.String())
	}
	t.Logf("[step2] put result: %d %s", w2.Code, w2.Body.String())

	// Step 3: get presign download URL
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/static/test-bucket/hello.txt?presign=1", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("step3: presign download URL generation failed: %d %s", w3.Code, w3.Body.String())
	}
	downloadURLStr := w3.Body.String()
	t.Logf("[step3] download URL: %s", downloadURLStr)

	// Step 4: download via presigned URL
	parsedDownloadURL, err := url.Parse(downloadURLStr)
	if err != nil {
		t.Fatalf("parse download URL: %v", err)
	}
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest("GET", parsedDownloadURL.RequestURI(), nil)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("step4: presign get failed: %d %s", w4.Code, w4.Body.String())
	}
	body, _ := io.ReadAll(w4.Result().Body)
	t.Logf("[step4] download result: %d %s", w4.Code, string(body))

	if string(body) != "hello presigned world" {
		t.Fatalf("expected 'hello presigned world', got '%s'", string(body))
	}
}

func TestPresignedPutInvalidToken(t *testing.T) {
	r := setupPresignedTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/test-bucket/hello.txt?token=fake.fake&expires=9999999999", strings.NewReader("data"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestPresignedPutMissingToken(t *testing.T) {
	r := setupPresignedTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/test-bucket/hello.txt", strings.NewReader("data"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPresignedGetWithoutToken_ObjectNotFound(t *testing.T) {
	r := setupPresignedTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test-bucket/nonexistent.txt", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPresignedGetWithoutToken_Success(t *testing.T) {
	r := setupPresignedTestRouter(t)

	// Upload a file first
	body := strings.NewReader("public content")
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("PUT", "/static/test-bucket/public.txt?presign=1", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("presign upload URL generation failed: %d", w1.Code)
	}
	parsedURL, err := url.Parse(w1.Body.String())
	if err != nil {
		t.Fatalf("parse upload URL: %v", err)
	}
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", parsedURL.RequestURI(), body)
	req2.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("upload failed: %d", w2.Code)
	}

	// Access without token
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/test-bucket/public.txt", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w3.Code)
	}
	data, _ := io.ReadAll(w3.Result().Body)
	if string(data) != "public content" {
		t.Fatalf("expected 'public content', got '%s'", string(data))
	}
}

func TestPresignedGetObjectNotFound(t *testing.T) {
	r := setupPresignedTestRouter(t)

	// Generate a valid download token for a non-existent file
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/static/test-bucket/nonexistent.txt?presign=1", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("presign URL generation failed: %d", w1.Code)
	}
	downloadURLStr := w1.Body.String()

	parsedURL, err := url.Parse(downloadURLStr)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", parsedURL.RequestURI(), nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w2.Code)
	}
}

func TestPresignedPutOpMismatch(t *testing.T) {
	r := setupPresignedTestRouter(t)

	// Get a download token, then try to use it for put
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/static/test-bucket/hello.txt?presign=1", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("presign URL generation failed: %d", w1.Code)
	}
	downloadURLStr := w1.Body.String()

	parsedURL, err := url.Parse(downloadURLStr)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	// Use GET token for PUT
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", parsedURL.RequestURI(), strings.NewReader("data"))
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for op mismatch, got %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "operation mismatch") {
		t.Fatalf("expected 'operation mismatch', got '%s'", w2.Body.String())
	}
}
