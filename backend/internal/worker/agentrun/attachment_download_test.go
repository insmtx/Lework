package agentrun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDownloadAttachmentBytesFailsFastOnSlowResponse 验证附件下载受超时约束。
//
// 回归背景：此前使用无超时的 http.DefaultClient，一个不可达的附件域名会把
// 单次运行阻塞在 TCP 建连上（实测 ~30.6s/次，两个附件叠加成 61s 档位），
// 因为下载发生在首个 LLM 请求之前，直接表现为首 token 延迟异常。
func TestDownloadAttachmentBytesFailsFastOnSlowResponse(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 永不写出响应头，模拟只建连不响应的慢/挂死服务端。
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	restore := swapAttachmentHTTPClient(newAttachmentHTTPClient(200*time.Millisecond, 100*time.Millisecond, 300*time.Millisecond))
	defer restore()

	start := time.Now()
	_, err := downloadAttachmentBytes(context.Background(), server.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected download against a hanging server to fail")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("download took %s, expected the client timeout to bound it", elapsed)
	}
}

func TestDownloadAttachmentHonorsSlowResponseTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	restore := swapAttachmentHTTPClient(newAttachmentHTTPClient(200*time.Millisecond, 100*time.Millisecond, 300*time.Millisecond))
	defer restore()

	dest := filepath.Join(t.TempDir(), "upload.bin")
	start := time.Now()
	err := downloadAttachment(context.Background(), server.URL, dest)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected download against a hanging server to fail")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("download took %s, expected the client timeout to bound it", elapsed)
	}
}

// 正常路径不应受超时改造影响：落盘内容与响应体一致。
func TestDownloadAttachmentWritesResponseBody(t *testing.T) {
	body := []byte("attachment-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	restore := swapAttachmentHTTPClient(newAttachmentHTTPClient(time.Second, time.Second, 5*time.Second))
	defer restore()

	dest := filepath.Join(t.TempDir(), "nested", "upload.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("create dest dir: %v", err)
	}
	if err := downloadAttachment(context.Background(), server.URL, dest); err != nil {
		t.Fatalf("download attachment: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}
}

func TestDownloadAttachmentBytesReadsResponseBody(t *testing.T) {
	body := []byte("inline-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	restore := swapAttachmentHTTPClient(newAttachmentHTTPClient(time.Second, time.Second, 5*time.Second))
	defer restore()

	got, err := downloadAttachmentBytes(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("download attachment bytes: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded bytes = %q, want %q", got, body)
	}
}

// 生产默认值必须是有界的：建连超时是消除 61s 档位的关键。
func TestAttachmentHTTPClientDefaultsAreBounded(t *testing.T) {
	if attachmentDialTimeout <= 0 || attachmentDialTimeout > 30*time.Second {
		t.Fatalf("attachment dial timeout = %s, want a bounded value", attachmentDialTimeout)
	}
	if attachmentResponseHeaderTimeout <= 0 {
		t.Fatalf("attachment response header timeout must be set")
	}
	if attachmentTotalTimeout <= 0 {
		t.Fatalf("attachment total timeout must be set")
	}
	if attachmentHTTPClient == http.DefaultClient {
		t.Fatalf("attachment downloads must not share the unbounded default client")
	}
	if attachmentHTTPClient.Timeout != attachmentTotalTimeout {
		t.Fatalf("attachment client timeout = %s, want %s", attachmentHTTPClient.Timeout, attachmentTotalTimeout)
	}
}

// TestAttachmentHTTPClientPreservesDefaultTransportBehavior 回归：给下载加超时
// 不得丢掉 http.DefaultTransport 的代理与协议行为。
//
// 背景：初版实现新造了一个 Transport，导致 ProxyFromEnvironment 丢失——在需要
// 经代理访问附件域名的环境里（本机即通过 clash 代理出网），下载会绕开代理而失败。
// 替换 http.DefaultClient 的目的是加超时上限，不是改变可达性。
func TestAttachmentHTTPClientPreservesDefaultTransportBehavior(t *testing.T) {
	client := newAttachmentHTTPClient(time.Second, 2*time.Second, 5*time.Second)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("attachment transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatalf("attachment transport dropped Proxy: downloads would bypass a configured proxy")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatalf("attachment transport dropped ForceAttemptHTTP2")
	}
	if transport.DialContext == nil {
		t.Fatalf("DialContext must be set to enforce the dial timeout")
	}
	// 超时必须真正落到 transport 上，而不只是 client.Timeout。
	if transport.TLSHandshakeTimeout != time.Second {
		t.Fatalf("TLSHandshakeTimeout = %s, want 1s", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != 2*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 2s", transport.ResponseHeaderTimeout)
	}
}

// swapAttachmentHTTPClient 临时替换下载客户端，返回恢复函数。
func swapAttachmentHTTPClient(client *http.Client) func() {
	previous := attachmentHTTPClient
	attachmentHTTPClient = client
	return func() { attachmentHTTPClient = previous }
}
