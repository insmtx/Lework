package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
)

// writeOpenCodeRegistration 写一份 OpenCode 共享服务注册文件，模拟
// `opencode serve --service` 留下的端点。
func writeOpenCodeRegistration(t *testing.T, url, password string) {
	t.Helper()

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		t.Fatal("XDG_STATE_HOME is empty; the test must set it before publishing")
	}
	dir := filepath.Join(stateHome, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create opencode state dir: %v", err)
	}
	payload := `{"id":"svc","version":"2.2.7","url":"` + url + `","pid":42,"password":"` + password + `"}`
	if err := os.WriteFile(filepath.Join(dir, "service.json"), []byte(payload), 0o600); err != nil {
		t.Fatalf("write opencode service file: %v", err)
	}
}

func TestResolveAgentEndpointAttachesToSharedService(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const password = "shared-secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/info" {
			t.Errorf("path = %q, want /api/info", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	writeOpenCodeRegistration(t, srv.URL, password)
	cfg := &agentdconfig.Config{OpenCode: agentdconfig.OpenCodeConfig{Port: 4096}}

	got, err := resolveAgentEndpoint(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resolveAgentEndpoint() error = %v", err)
	}
	if !got.Attach {
		t.Fatal("resolveAgentEndpoint().Attach = false, want true for a healthy shared service")
	}
	if got.URL != srv.URL || got.Password != password {
		t.Fatalf("resolveAgentEndpoint() = %+v, want the discovered endpoint", got)
	}
	if len(got.Env) != 0 {
		t.Fatalf("resolveAgentEndpoint().Env = %v, want nil: attaching must not configure a child", got.Env)
	}
}

func TestResolveAgentEndpointFallsBackWhenSharedRejected(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OPENCODE_PASSWORD", "")
	t.Setenv("OPENCODE_SERVER_PASSWORD", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	writeOpenCodeRegistration(t, srv.URL, "wrong-password")

	cfg := &agentdconfig.Config{OpenCode: agentdconfig.OpenCodeConfig{Port: 4123}}
	got, err := resolveAgentEndpoint(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resolveAgentEndpoint() error = %v", err)
	}
	if got.Attach {
		t.Fatal("resolveAgentEndpoint().Attach = true, want a private fallback when credentials are rejected")
	}
	if got.URL != "http://127.0.0.1:4123" {
		t.Fatalf("resolveAgentEndpoint().URL = %q, want the configured private endpoint", got.URL)
	}
	if got.Password == "" || got.Password == "wrong-password" {
		t.Fatalf("resolveAgentEndpoint().Password = %q, want a freshly generated private password", got.Password)
	}
	if !slices.Contains(got.Env, "OPENCODE_SERVER_PASSWORD="+got.Password) {
		t.Fatalf("Env = %v, want the generated password for the child process", got.Env)
	}
}
