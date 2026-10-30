package opencode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultProbeTimeout 是调用方未指定超时时的兜底上限。
	defaultProbeTimeout = 5 * time.Second
	// requiredMajor / requiredMinor 锁定 ACP 迁移验证过的版本窗口 2.1.x。
	// 2.1.x 内 resume/usage 形状稳定；升级窗口必须重跑契约测试。
	requiredMajor = 2
	requiredMinor = 1
	// defaultBinary 与 NewAdapter 的缺省值保持一致。
	defaultBinary  = "opencode"
	installCommand = "npm install -g @opencode/cli@2.1.4"
)

var (
	// probeWaitDelay 限制进程退出后等待管道关闭的时间。opencode 这类 SEA 程序会派生
	// 子进程，子进程继承 stdout 时 CombinedOutput 会一直卡在管道读上，单靠
	// exec.CommandContext 的 kill 不足以给探测加上硬上限。
	probeWaitDelay = 2 * time.Second
)

// versionPattern 与 backend/internal/cli/discovery.go 中 opencode 的版本正则
// （`(\d+\.\d+\.\d+)`）保持一致。这里复制而不是 import：agent 层不能向上依赖
// internal 层，两处同源即可。
var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Version 是 opencode CLI 的语义化版本，只保留门禁判断所需的三段数字。
type Version struct {
	Major int
	Minor int
	Patch int
}

// String 返回 `major.minor.patch`，用于日志与错误信息。
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ParseVersion 从 `opencode --version` 的输出中提取版本号。
// 输出形如 `2.1.4` 或 `opencode v2.1.4`；解析失败时返回 false。
func ParseVersion(output string) (Version, bool) {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) != 4 {
		return Version{}, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return Version{}, false
	}
	minor, err := strconv.Atoi(match[2])
	if err != nil {
		return Version{}, false
	}
	patch, err := strconv.Atoi(match[3])
	if err != nil {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}

// ACPCompatible 报告该版本是否落在迁移验证过的 2.1.x 窗口内。
func (v Version) ACPCompatible() bool {
	return v.Major == requiredMajor && v.Minor == requiredMinor
}

// RequireACPCompatible 把版本不匹配转成可直接执行的错误：
// 用户能看到当前版本与确切的升级命令，而不是运行到一半才失败。
func (v Version) RequireACPCompatible() error {
	if v.ACPCompatible() {
		return nil
	}
	return fmt.Errorf(
		"opencode v%d.%d.x required, found %s; run: %s",
		requiredMajor, requiredMinor, v.String(), installCommand,
	)
}

// ProbeVersion 执行 `<binary> --version` 并解析版本号，最多等待 timeout。
// binary 为空时回退到 PATH 上的 `opencode`；timeout <= 0 使用内置缺省值。
// ctx 取消会立即终止探测；进程退出后最多再等 probeWaitDelay 让管道关闭。
func ProbeVersion(ctx context.Context, binary string, timeout time.Duration) (Version, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(binary) == "" {
		binary = defaultBinary
	}
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, binary, "--version")
	// 子进程继承 stdout 时，管道在孙进程退出前不会关闭；WaitDelay 保证 Wait 有硬上限，
	// 否则“超时”只是杀了直接子进程，调用方仍会一直阻塞。
	cmd.WaitDelay = probeWaitDelay

	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil && errors.Is(err, exec.ErrWaitDelay) {
		// 进程正常退出、只是孙进程还占着管道：版本输出已经读全，按成功处理。
		if version, ok := ParseVersion(trimmed); ok {
			return version, nil
		}
	}
	if err != nil {
		if probeCtx.Err() != nil {
			return Version{}, fmt.Errorf("probe %s --version: %w", binary, probeCtx.Err())
		}
		// CLI 把不可写日志目录之类的原因写在输出里（bun 还可能带大段栈），
		// 截断后附带，避免只剩下一个 "exit status 1"。
		if detail := versionErrorDetail(trimmed); detail != "" {
			return Version{}, fmt.Errorf("probe %s --version: %w: %s", binary, err, detail)
		}
		return Version{}, fmt.Errorf("probe %s --version: %w", binary, err)
	}

	version, ok := ParseVersion(trimmed)
	if !ok {
		return Version{}, fmt.Errorf(
			"probe %s --version: unrecognized output %q",
			binary, trimmed,
		)
	}
	return version, nil
}

// versionErrorDetail 把多行输出压成一行、限制长度，只用于错误信息。
func versionErrorDetail(output string) string {
	if output == "" {
		return ""
	}
	flat := strings.Join(strings.Fields(output), " ")
	const maxLen = 300
	runes := []rune(flat)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return flat
}
