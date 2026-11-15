package main

import (
	"bytes"
	"strings"
	"testing"

	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// TestAnnounceStartupPrintsNoCredential 固化设计 §8/§12 的不变量：控制面启动
// 不得打印任何凭据。即使用户显式给了 enrollment token，输出也必须为空，更
// 不能出现配对链接或 token 参数。
func TestAnnounceStartupPrintsNoCredential(t *testing.T) {
	var out bytes.Buffer
	cfg := controlplaneconfig.Default()
	cfg.ListenAddr = "127.0.0.1:8787"
	cfg.EnrollmentToken = "secret-token"

	announceStartup(&out, cfg)

	if out.Len() != 0 {
		t.Fatalf("startup output = %q, want empty", out.String())
	}
	for _, needle := range []string{"lework://connect", "token=", "secret-token"} {
		if strings.Contains(out.String(), needle) {
			t.Fatalf("startup output contains %q: %q", needle, out.String())
		}
	}
}
