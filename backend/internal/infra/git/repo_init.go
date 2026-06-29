// Package git 封装通过 Gitea API 操作远端 Git 仓库的基础设施能力。
package git

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"code.gitea.io/sdk/gitea"

	"github.com/ygpkg/yg-go/logs"
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

// InitRepoStructure 通过 Gitea API 向指定仓库写入初始内容文件
// (.gitignore 与 .leros/memory/.gitkeep)。
//
// 行为：
//   - client 为 nil 或 fullName 不符合 owner/repo 格式时返回 error
//   - 单个文件创建失败会记录告警并继续尝试其余文件
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
		content := base64.StdEncoding.EncodeToString([]byte(f.content))
		if _, _, err := client.CreateFile(owner, repo, f.path, gitea.CreateFileOptions{
			FileOptions: gitea.FileOptions{
				Message: f.msg,
			},
			Content: content,
		}); err != nil {
			logs.WarnContextf(ctx, "[infra/git] init file %s failed: %v", f.path, err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
