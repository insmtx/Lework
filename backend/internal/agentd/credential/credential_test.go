package credential

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCredential 是测试专用值。失败消息只打印长度，避免测试日志里出现
// 与真实节点凭证同形的明文。
const fakeCredential = "cred-123"

// assertNoTempFiles 断言目录里没有 Save 留下的临时文件。
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".node.credential-") {
			t.Fatalf("stale temp file %q left behind", entry.Name())
		}
	}
}

func TestSaveThenLoadRoundTripsWithTightPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.credential")
	if err := Save(path, fakeCredential); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}

	raw, ok, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load reported a missing credential")
	}
	if raw != fakeCredential {
		t.Fatalf("Load returned %d bytes, want %d", len(raw), len(fakeCredential))
	}

	if _, ok, err := Load(filepath.Join(t.TempDir(), "missing")); err != nil || ok {
		t.Fatalf("Load(missing) = ok %t, err %v; want not found and no error", ok, err)
	}
}

func TestSaveCreatesParentDirectoryWith0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "config")
	path := filepath.Join(dir, "node.credential")
	if err := Save(path, fakeCredential); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
}

func TestSaveReplacesExistingCredentialKeepingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.credential")
	if err := Save(path, fakeCredential); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(path, "cred-456"); err != nil {
		t.Fatalf("Save replacement: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode after replacement = %o, want 600", perm)
	}

	raw, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load = ok %t, err %v; want the replacement", ok, err)
	}
	if raw != "cred-456" {
		t.Fatalf("Load returned %d bytes, want the replacement", len(raw))
	}
	assertNoTempFiles(t, dir)
}

// TestFailedSaveLeavesNoTempFile 让最终 rename 失败：目标已是一个目录，
// 文件无法覆盖它。原子写失败后目录里不能留下半成品。
func TestFailedSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.credential")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := Save(path, fakeCredential); err == nil {
		t.Fatal("Save replaced a directory, want an error")
	}
	assertNoTempFiles(t, dir)
}

func TestLoadTreatsBlankContentAsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.credential")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, ok, err := Load(path)
	if err != nil || ok {
		t.Fatalf("Load = ok %t, err %v; want no credential and no error", ok, err)
	}
	if raw != "" {
		t.Fatalf("Load returned %d bytes, want empty", len(raw))
	}
}

func TestLoadTrimsTrailingWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.credential")
	if err := os.WriteFile(path, []byte(fakeCredential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load = ok %t, err %v; want the credential", ok, err)
	}
	if raw != fakeCredential {
		t.Fatalf("Load returned %d bytes, want the trimmed value", len(raw))
	}
}

func TestLoadBlankPathIsAbsent(t *testing.T) {
	raw, ok, err := Load("")
	if err != nil || ok {
		t.Fatalf("Load(\"\") = ok %t, err %v; want not found and no error", ok, err)
	}
	if raw != "" {
		t.Fatalf("Load(\"\") returned %d bytes, want empty", len(raw))
	}
}

// TestLoadReportsReadErrors 用目录触发非 ENOENT 的读取错误，错误文本必须
// 点名路径以便排查，但不包含文件内容。
func TestLoadReportsReadErrors(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("Load(directory) returned nil error")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("Load error does not name the path: %v", err)
	}
}

func TestDefaultPathUsesPlatformConfigDir(t *testing.T) {
	original := userConfigDir
	configDir := t.TempDir()
	userConfigDir = func() (string, error) { return configDir, nil }
	t.Cleanup(func() { userConfigDir = original })

	want := filepath.Join(configDir, "Lework", "node.credential")
	if got := DefaultPath(""); got != want {
		t.Fatalf("DefaultPath(\"\") = %q, want %q", got, want)
	}
}

func TestDefaultPathFallsBackWithoutConfigDir(t *testing.T) {
	original := userConfigDir
	userConfigDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userConfigDir = original })

	// 平台配置目录不可得时退回固定文件名，再按工作目录解析成绝对路径，
	// 避免把凭证悄悄写进进程 CWD 的相对位置。
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "node.credential")
	if got := DefaultPath(""); got != want {
		t.Fatalf("DefaultPath(\"\") = %q, want %q", got, want)
	}
}

// TestDefaultPathAbsolutizesRelativeConfigFile 防止相对 agent.yaml 把凭证
// 路径也变成相对路径：Save 会因此在重启目录变化时写到别处。
func TestDefaultPathAbsolutizesRelativeConfigFile(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got := DefaultPath("agent.yaml")
	if !filepath.IsAbs(got) {
		t.Fatalf("DefaultPath(\"agent.yaml\") = %q, want an absolute path", got)
	}
	if want := filepath.Join(cwd, "node.credential"); got != want {
		t.Fatalf("DefaultPath(\"agent.yaml\") = %q, want %q", got, want)
	}
}

func TestDefaultPathSitsNextToExplicitConfigFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "etc", "lework", "agent.yaml")
	want := filepath.Join(filepath.Dir(configPath), "node.credential")
	if got := DefaultPath(configPath); got != want {
		t.Fatalf("DefaultPath(%q) = %q, want %q", configPath, got, want)
	}
}

func TestSaveRejectsBlankInput(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		raw  string
	}{
		{name: "blank path", path: "", raw: fakeCredential},
		{name: "blank credential", path: filepath.Join(dir, "node.credential"), raw: "  \n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Save(tt.path, tt.raw); err == nil {
				t.Fatal("Save accepted blank input, want an error")
			}
		})
	}
	assertNoTempFiles(t, dir)
}
