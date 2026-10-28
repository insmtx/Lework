package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildPinsMatchRuntimeGate 是版本漂移的机械门禁：
// Dockerfile、versions.txt、go.mod 与运行时升级提示必须指向同一个 2.1.x/0.13.x。
// 任一文件漏改都会让这条测试失败，避免镜像里装 v2 而门禁仍提示 v1 之类的静默失效。
func TestBuildPinsMatchRuntimeGate(t *testing.T) {
	root := repoRoot(t)
	dockerfile := readFile(t, filepath.Join(root, "deployments", "build", "Dockerfile.worker-base"))
	versions := readFile(t, filepath.Join(root, "deployments", "build", "worker-base.versions.txt"))
	gomod := readFile(t, filepath.Join(root, "go.mod"))

	version := "2.1.4"
	if !strings.Contains(dockerfile, "ARG OPENCODE_VERSION="+version) {
		t.Fatalf("Dockerfile OPENCODE_VERSION is not %s", version)
	}
	if !strings.Contains(dockerfile, `"@opencode/cli@${OPENCODE_VERSION}"`) {
		t.Fatal("Dockerfile does not install @opencode/cli")
	}
	if !strings.Contains(dockerfile, `test "$(opencode --version`) ||
		!strings.Contains(dockerfile, `= "${OPENCODE_VERSION}"`) {
		t.Fatal("Dockerfile lacks exact opencode --version assertion")
	}
	if !strings.Contains(versions, "OPENCODE_VERSION="+version) {
		t.Fatalf("worker-base.versions.txt is not %s", version)
	}
	if !strings.Contains(gomod, "github.com/coder/acp-go-sdk v0.13.5") {
		t.Fatal("go.mod ACP SDK pin drifted from v0.13.5")
	}
	if !strings.Contains(installCommand, "@opencode/cli@"+version) {
		t.Fatalf("runtime install hint (%q) does not match pinned %s", installCommand, version)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
