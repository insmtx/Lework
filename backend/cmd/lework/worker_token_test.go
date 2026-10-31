package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/config"
)

// withTokenRetryTimings 把退避参数压到毫秒级，避免单测真的等 10 分钟。
func withTokenRetryTimings(t *testing.T, initial, max, budget time.Duration) {
	t.Helper()
	originalInitial, originalMax, originalBudget := tokenRetryInitial, tokenRetryMax, tokenRetryBudget
	tokenRetryInitial, tokenRetryMax, tokenRetryBudget = initial, max, budget
	t.Cleanup(func() {
		tokenRetryInitial, tokenRetryMax, tokenRetryBudget = originalInitial, originalMax, originalBudget
	})
}

// TestRequestWorkerAuthTokenRetriesOn401 固化“token 轮转窗口不打死进程”。
func TestRequestWorkerAuthTokenRetriesOn401(t *testing.T) {
	withTokenRetryTimings(t, 5*time.Millisecond, 20*time.Millisecond, 2*time.Second)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"message":"invalid worker bootstrap token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"auth_token":"tok-123","token_type":"bearer"}}`))
	}))
	defer server.Close()

	token, err := requestWorkerAuthTokenWithRetry(context.Background(), server.URL, "bootstrap", 1, 2)
	if err != nil {
		t.Fatalf("requestWorkerAuthTokenWithRetry() error = %v, want success after retry", err)
	}
	if token != "tok-123" {
		t.Fatalf("token = %q, want tok-123", token)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

// TestRequestWorkerAuthTokenDoesNotRetryOnBadRequest 配置错误应立即失败。
func TestRequestWorkerAuthTokenDoesNotRetryOnBadRequest(t *testing.T) {
	withTokenRetryTimings(t, time.Millisecond, time.Millisecond, time.Second)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"org_id is required"}`))
	}))
	defer server.Close()

	_, err := requestWorkerAuthTokenWithRetry(context.Background(), server.URL, "bootstrap", 0, 2)
	if err == nil {
		t.Fatal("requestWorkerAuthTokenWithRetry() error = nil, want failure")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 400)", got)
	}
}

// TestRequestWorkerAuthTokenDoesNotRetryBusinessError 业务 code 非 0 不重试。
func TestRequestWorkerAuthTokenDoesNotRetryBusinessError(t *testing.T) {
	withTokenRetryTimings(t, time.Millisecond, time.Millisecond, time.Second)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		_, _ = w.Write([]byte(`{"code":40001,"message":"worker is not active"}`))
	}))
	defer server.Close()

	_, err := requestWorkerAuthTokenWithRetry(context.Background(), server.URL, "bootstrap", 1, 2)
	if err == nil {
		t.Fatal("requestWorkerAuthTokenWithRetry() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "worker is not active") {
		t.Fatalf("error = %q, want business message", err.Error())
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on business error)", got)
	}
}

// TestRequestWorkerAuthTokenStopsAfterBudget 预算耗尽才返回错误。
func TestRequestWorkerAuthTokenStopsAfterBudget(t *testing.T) {
	withTokenRetryTimings(t, 10*time.Millisecond, 20*time.Millisecond, 60*time.Millisecond)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid worker bootstrap token"}`))
	}))
	defer server.Close()

	start := time.Now()
	_, err := requestWorkerAuthTokenWithRetry(context.Background(), server.URL, "bootstrap", 1, 2)
	if err == nil {
		t.Fatal("requestWorkerAuthTokenWithRetry() error = nil, want budget exhaustion")
	}
	if !strings.Contains(err.Error(), "rejected after") {
		t.Fatalf("error = %q, want budget exhaustion message", err.Error())
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("retry took %s, want bounded by retry budget", elapsed)
	}
}

// TestEnsureWorkerAuthTokenSkipsWhenAlreadyConfigured 保持既有短路行为。
func TestEnsureWorkerAuthTokenSkipsWhenAlreadyConfigured(t *testing.T) {
	cfg := &config.WorkerConfig{AuthToken: "existing", BootstrapToken: "bootstrap", ServerAddr: "http://127.0.0.1:1"}
	if err := ensureWorkerAuthToken(context.Background(), cfg); err != nil {
		t.Fatalf("ensureWorkerAuthToken() error = %v, want skip", err)
	}
	if cfg.AuthToken != "existing" {
		t.Fatalf("auth token = %q, want existing", cfg.AuthToken)
	}
}
