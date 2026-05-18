package manage

import (
	"context"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/ygpkg/yg-go/logs"
)

const defaultPostProcessDelay = 500 * time.Millisecond

// PostProcessor 在 Skill 变更后执行非阻塞后处理。
type PostProcessor struct {
	sourceDir string
	catalog   skillcatalog.CatalogReloader
	delay     time.Duration

	mu    sync.Mutex
	timer *time.Timer
}

// NewPostProcessor 创建 Skill 变更后处理器。
func NewPostProcessor(sourceDir string, catalog skillcatalog.CatalogReloader) *PostProcessor {
	return &PostProcessor{
		sourceDir: sourceDir,
		catalog:   catalog,
		delay:     defaultPostProcessDelay,
	}
}

// AfterMutation 调度异步 CLI 同步和 Catalog reload 工作。
func (p *PostProcessor) AfterMutation(result *skillstore.Result) {
	if p == nil || result == nil || !result.Success {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.delay, func() {
		p.run(result.Action)
	})
}

func (p *PostProcessor) run(action string) {
	if p == nil {
		return
	}

	// Reload the in-memory catalog to pick up new/changed skills
	if p.catalog != nil {
		if err := p.catalog.Reload(context.Background()); err != nil {
			logs.Warnf("Reload Leros skill catalog after %s failed: %v", action, err)
		} else {
			logs.Infof("Reloaded skill catalog after %s", action)
		}
	}

	// After skill mutation, sync from ~/.leros/skills to external CLI directories
	// This ensures external CLI tools can see the newly created/modified skills
	// Default target directories: ~/.claude/skills, ~/.agents/skills
	if err := engines.SyncFromLerosToExternal(nil); err != nil {
		logs.Warnf("Sync Leros skills to external CLI after %s failed: %v", action, err)
	} else {
		logs.Infof("Synced skills to external CLI after %s", action)
	}
}
