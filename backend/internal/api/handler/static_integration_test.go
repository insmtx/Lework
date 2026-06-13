package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

func TestCurlStylePresignUpload(t *testing.T) {
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

	// 等价于 curl -X PUT "http://localhost:8080/v1/static/test-bucket/path/to/file.txt?presign=1"
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/static/test-bucket/path/to/file.txt?presign=1", nil)
	r.ServeHTTP(w, req)

	fmt.Printf("[PUT presign-upload] status=%d, url=%s, expires=%s\n",
		w.Code, w.Body.String(), w.Header().Get("X-Presign-Expires-At"))

	t.Logf("[PUT presign-upload] status=%d, url=%s", w.Code, w.Body.String())
	t.Logf("[PUT presign-upload] X-Presign-Expires-At=%s", w.Header().Get("X-Presign-Expires-At"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() == "" {
		t.Fatal("empty URL in response body")
	}
	if w.Header().Get("X-Presign-Expires-At") == "" {
		t.Fatal("missing X-Presign-Expires-At header")
	}
}

func TestCurlStylePresignDownload(t *testing.T) {
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

	// 等价于 curl -X GET "http://localhost:8080/v1/static/test-bucket/path/to/file.txt?presign=1"
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/test-bucket/path/to/file.txt?presign=1", nil)
	r.ServeHTTP(w, req)

	fmt.Printf("[GET presign-download] status=%d, url=%s, expires=%s\n",
		w.Code, w.Body.String(), w.Header().Get("X-Presign-Expires-At"))

	t.Logf("[GET presign-download] status=%d, url=%s", w.Code, w.Body.String())
	t.Logf("[GET presign-download] X-Presign-Expires-At=%s", w.Header().Get("X-Presign-Expires-At"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() == "" {
		t.Fatal("empty URL in response body")
	}
	if w.Header().Get("X-Presign-Expires-At") == "" {
		t.Fatal("missing X-Presign-Expires-At header")
	}
}

func TestCurlStylePresignRoundTrip(t *testing.T) {
	_ = context.Background()
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

	// 1. 获取预签名上传 URL
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("PUT", "/static/test-bucket/hello-world.txt?presign=1", nil)
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("step1: presign upload failed: %d %s", w1.Code, w1.Body.String())
	}
	uploadURL := w1.Body.String()
	t.Logf("[step1] presign-upload URL: %s", uploadURL)

	// 2. 获取预签名下载 URL
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/static/test-bucket/hello-world.txt?presign=1", nil)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("step2: presign download failed: %d %s", w2.Code, w2.Body.String())
	}
	downloadURL := w2.Body.String()
	t.Logf("[step2] presign-download URL: %s", downloadURL)

	// 3. 通过 storage 直接写入文件（模拟客户端上传）
	st := filestore.GetStorage()
	bucket := filestore.DefaultBucket()
	_, err := st.PutObject(context.Background(), bucket, "hello-world.txt",
		strings.NewReader("hello from curl test"), nil)
	if err != nil {
		t.Fatalf("step3: put object: %v", err)
	}
	t.Log("[step3] wrote file to storage")

	// 4. 通过 storage 直接读取文件（模拟客户端通过预签名 URL 下载）
	result, err := st.GetObject(context.Background(), bucket, "hello-world.txt")
	if err != nil {
		t.Fatalf("step4: get object: %v", err)
	}
	defer result.Body.Close()
	content := make([]byte, 100)
	n, _ := result.Body.Read(content)
	t.Logf("[step4] read file content: %s", string(content[:n]))

	if string(content[:n]) != "hello from curl test" {
		t.Fatalf("step4: expected 'hello from curl test', got '%s'", string(content[:n]))
	}
}
