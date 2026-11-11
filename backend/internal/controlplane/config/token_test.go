package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTokenGeneratesAndReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "enrollment.token")
	first, err := ResolveToken("", path)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if len(first) != 43 {
		t.Fatalf("token length = %d, want 43 (32 bytes base64url)", len(first))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token file mode = %o, want 600", got)
	}
	// 断言失败时不得回显令牌值，只报告长度与错误。
	second, err := ResolveToken("", path)
	if err != nil {
		t.Fatalf("second ResolveToken: %v", err)
	}
	if second != first {
		t.Fatalf("second ResolveToken differs from first (len=%d)", len(second))
	}
}

func TestResolveTokenExplicitWinsAndPersists(t *testing.T) {
	const explicit = "explicit-token-value-1234"
	path := filepath.Join(t.TempDir(), "enrollment.token")
	got, err := ResolveToken(explicit, path)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got != explicit {
		t.Fatalf("ResolveToken returned a different value than the explicit token (len=%d)", len(got))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != explicit {
		t.Fatalf("token file content differs from the explicit token (len=%d)", len(data))
	}
}

func TestResolveTokenRejectsShortFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrollment.token")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveToken("", path)
	if err == nil {
		t.Fatal("ResolveToken accepted a short token file")
	}
	if strings.Contains(err.Error(), "short") {
		t.Fatalf("error leaks the file contents: %v", err)
	}
}

func TestDefaultTokenPathUsesConfigDir(t *testing.T) {
	userConfigDir = func() (string, error) { return "/tmp/cfg", nil }
	t.Cleanup(func() { userConfigDir = os.UserConfigDir })
	if got := DefaultTokenPath(); got != filepath.Join("/tmp/cfg", "Lework", "enrollment.token") {
		t.Fatalf("DefaultTokenPath() = %q", got)
	}
}
