package manage

import (
	"context"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/ygpkg/yg-go/logs"
)

// ---------- CatalogReloadHandler：所有 skill 变更后重新加载只读快照 ----------

type catalogReloadHandlerImpl struct {
	reloader catalog.CatalogReloader
}

func NewCatalogReloadHandler(reloader catalog.CatalogReloader) MutationHandler {
	return &catalogReloadHandlerImpl{reloader: reloader}
}

// Handle 对全部 MutationKind 都执行 Reload。
// patch/write_file/remove_file 虽然不触发外部投影，但 catalog 仍然需要刷新以反映内容变化。
func (h *catalogReloadHandlerImpl) Handle(ctx context.Context, m Mutation) error {
	if h.reloader == nil {
		return nil
	}
	if err := h.reloader.Reload(ctx); err != nil {
		return err
	}
	logs.Infof("Reloaded skill catalog after %s: %s", m.Action, m.Name)
	return nil
}

// ---------- ProjectionHandler：仅在创建 skill 时维护外部 CLI symlink ----------

type projectionHandlerImpl struct {
	cliDirs []string
}

func NewProjectionHandler(cliDirs []string) MutationHandler {
	return &projectionHandlerImpl{cliDirs: cliDirs}
}

// Handle 只处理 MutationCreate。
// - create：确保外部每个 CLI skill 目录下有 {name} → .leros/skills/{name} 的 symlink。
// - modify：不处理——symlink 指向 .leros/skills 目录，内容修改天然可见。
// cliDirs 为空时直接跳过（worker 启动时未发现任何 CLI 的场景）。
func (h *projectionHandlerImpl) Handle(ctx context.Context, m Mutation) error {
	if m.Kind != MutationCreate {
		return nil
	}
	if len(h.cliDirs) == 0 {
		return nil
	}
	return engines.EnsureExternalSkillLink(m.Name, h.cliDirs)
}

// ---------- CompositeHandler：串联多个 handler ----------

type compositeHandlerImpl struct {
	handlers []MutationHandler
}

func NewCompositeHandler(handlers ...MutationHandler) MutationHandler {
	return &compositeHandlerImpl{handlers: handlers}
}

// Handle 依次执行所有子 handler。
// 单个 handler 出错只记日志不中断后续——确保 catalog reload 失败不会阻碍 projection。
func (h *compositeHandlerImpl) Handle(ctx context.Context, m Mutation) error {
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, m); err != nil {
			logs.Warnf("Skill mutation handler failed: %v", err)
		}
	}
	return nil
}

// ---------- DebouncedHandler：500ms 防抖包装 ----------

const defaultDebounceDelay = 500 * time.Millisecond

type debouncedHandlerImpl struct {
	inner MutationHandler
	delay time.Duration

	mu      sync.Mutex
	timer   *time.Timer
	pending *Mutation
}

func NewDebouncedHandler(delay time.Duration, inner MutationHandler) MutationHandler {
	if delay <= 0 {
		delay = defaultDebounceDelay
	}
	return &debouncedHandlerImpl{inner: inner, delay: delay}
}

// Handle 将 mutation 缓存为 pending，启动 / 重置延迟定时器。
// 适用场景：高频变更（如连续 patch）不需要每次都触发昂贵的全量 catalog reload，
// 只保留最后一次 pending mutation 在延迟结束后执行。
// 注意：只保留最后一个 pending——适合 catalog reload（幂等操作），
// 不适合需精确计数的场景。
func (h *debouncedHandlerImpl) Handle(ctx context.Context, m Mutation) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	h.pending = &Mutation{Kind: m.Kind, Name: m.Name, Action: m.Action}

	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(h.delay, func() {
		h.mu.Lock()
		mut := h.pending
		h.mu.Unlock()
		if mut == nil {
			return
		}
		if err := h.inner.Handle(context.Background(), *mut); err != nil {
			logs.Warnf("Debounced skill mutation handler failed: %v", err)
		}
	})
	return nil
}
