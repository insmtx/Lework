// Package git 封装通过 Gitea API 操作远端 Git 仓库的基础设施能力。
package git

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"

	"github.com/ygpkg/yg-go/logs"
)

const (
	createRepoTimeout  = 25 * time.Second
	createRepoMaxRetry = 2
)

// defaultGitignore 是新仓库初始写入的 .gitignore 内容，
// 覆盖 Leros 运行时目录、用户上传、依赖目录、构建产物、编辑器噪声等。
const defaultGitignore = `# Leros runtime
.leros/
!.leros/memory/

# User uploads (served from object storage, not committed)
uploads/

# Dependency directories
node_modules/
vendor/

# Build/cache outputs
dist/
build/
target/
.cache/
.cache*/
tmp/
temp/
logs/
log/

# OpenCode AI workspace
.opencode/

# OS/editor noise
.DS_Store
Thumbs.db
*.swp
*.swo

# Runtime logs
*.log

# Environment/secrets
.env
.env.*
!.env.example
`

// initFile 描述一个待写入仓库的初始文件。
type initFile struct {
	path    string
	content string
	msg     string
}

// defaultInitFiles 是新仓库默认写入的初始文件清单。
var defaultInitFiles = []initFile{
	{path: ".gitignore", content: defaultGitignore, msg: "chore: init .gitignore"},
}

// CreateRepoWithRetry 创建 Gitea 仓库，包含超时控制与退避重试。
// 对网络类错误（非 409/403）最多重试 createRepoMaxRetry 次。
func CreateRepoWithRetry(ctx context.Context, client *gitea.Client, opts gitea.CreateRepoOption) (*gitea.Repository, error) {
	if client == nil {
		return nil, errors.New("gitea client is nil")
	}
	var lastErr error
	for attempt := 0; attempt <= createRepoMaxRetry; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*200) * time.Millisecond
			logs.WarnContextf(ctx, "[infra_git] create repo retry attempt %d/%d for %s: %v", attempt, createRepoMaxRetry, opts.Name, lastErr)
			time.Sleep(backoff)
		}
		reqCtx, cancel := context.WithTimeout(ctx, createRepoTimeout)
		client.SetContext(reqCtx)
		repo, resp, err := client.CreateRepo(opts)
		if resp != nil {
			logs.InfoContextf(ctx, "[infra_git] create repo %s failed: status=%d response=%+v err=%v", opts.Name, resp.StatusCode, resp, err)
		}
		cancel()
		if err == nil {
			return repo, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, fmt.Errorf("create gitea repo %s: context done: %w", opts.Name, ctx.Err())
		}
		if !shouldRetry(err, resp) {
			return nil, fmt.Errorf("create gitea repo %s: %w", opts.Name, err)
		}
	}
	return nil, fmt.Errorf("create gitea repo %s after %d retries: %w", opts.Name, createRepoMaxRetry, lastErr)
}

// InitRepoStructure 通过 Gitea API 向指定仓库写入初始内容文件
// (.gitignore 与 .leros/memory/.gitkeep)。
//
// 行为：
//   - client 为 nil 或 fullName 不符合 owner/repo 格式时返回 error
//   - 单个文件创建会使用独立超时 context 与重试，不受调用方 ctx 取消影响
//   - 多次重试后仍失败会记录告警并继续尝试其余文件
//   - 若存在任一文件失败，最终返回聚合后的 error
//
// 调用方应根据业务语义决定是否容忍该 error。
func InitRepoStructure(ctx context.Context, client *gitea.Client, fullName string) error {
	if client == nil {
		return errors.New("gitea client is nil")
	}
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return errors.New("invalid repo full name: " + fullName)
	}
	owner, repo := parts[0], parts[1]

	var errs []error
	for _, f := range defaultInitFiles {
		var lastErr error
		created := false
		for attempt := 0; attempt <= createRepoMaxRetry; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(attempt*200) * time.Millisecond
				logs.WarnContextf(ctx, "[infra_git] init file %s retry attempt %d/%d: %v", f.path, attempt, createRepoMaxRetry, lastErr)
				time.Sleep(backoff)
			}
			reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), createRepoTimeout)
			client.SetContext(reqCtx)
			content := base64.StdEncoding.EncodeToString([]byte(f.content))
			_, _, err := client.CreateFile(owner, repo, f.path, gitea.CreateFileOptions{
				FileOptions: gitea.FileOptions{
					Message: f.msg,
				},
				Content: content,
			})
			cancel()
			if err == nil {
				created = true
				break
			}
			lastErr = err
		}
		if !created {
			logs.WarnContextf(ctx, "[infra_git] init file %s failed: %v", f.path, lastErr)
			errs = append(errs, lastErr)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// shouldRetry 判断 Gitea API 错误是否应重试。
// 优先使用 HTTP 状态码判断：4xx 不重试，其余重试。
func shouldRetry(err error, resp *gitea.Response) bool {
	if err == nil {
		return false
	}
	if resp == nil {
		return true
	}
	sc := resp.StatusCode
	return sc < 400 || sc >= 500
}
