package manage

import (
	"context"
	"fmt"

	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
)

// Manager 协调 SkillStore 写入和 mutation 后处理。
//
// 架构：
//
//	Manager.Create/Patch/WriteFile/RemoveFile
//	  → SkillStore 写入 .leros/skills
//	  → after() 构建 Mutation
//	  → MutationHandler.Handle()
//	    ├── ProjectionHandler（create 时维护外部 CLI symlink）
//	    └── DebouncedHandler（500ms 防抖）
//	         └── CatalogReloadHandler（刷新只读快照）
//
// Manager 自身不知道 catalog、projection、debounce 细节，只发布 mutation 事件。
type Manager struct {
	store           *skillstore.SkillStore
	mutationHandler MutationHandler
}

func NewManager(store *skillstore.SkillStore, handler MutationHandler) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("skill store is required")
	}
	return &Manager{store: store, mutationHandler: handler}, nil
}

func (m *Manager) RootDir() string {
	if m == nil || m.store == nil {
		return ""
	}
	return m.store.RootDir()
}

func (m *Manager) Create(ctx context.Context, req skillstore.CreateRequest) (*skillstore.Result, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	result, err := m.store.Create(ctx, req)
	m.after(result, err)
	return result, err
}

func (m *Manager) Patch(ctx context.Context, req skillstore.PatchRequest) (*skillstore.Result, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	result, err := m.store.Patch(ctx, req)
	m.after(result, err)
	return result, err
}

func (m *Manager) WriteFile(ctx context.Context, req skillstore.WriteFileRequest) (*skillstore.Result, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	result, err := m.store.WriteFile(ctx, req)
	m.after(result, err)
	return result, err
}

func (m *Manager) RemoveFile(ctx context.Context, req skillstore.RemoveFileRequest) (*skillstore.Result, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	result, err := m.store.RemoveFile(ctx, req)
	m.after(result, err)
	return result, err
}

func (m *Manager) validate() error {
	if m == nil || m.store == nil {
		return fmt.Errorf("skill manager is not initialized")
	}
	return nil
}

// after 在 Store 操作成功后构建 Mutation 并通知 handler 链。
// handler 可为 nil（standalone / 测试场景），此时只写 store 不做后处理。
// handler.Handle 使用 context.Background() 异步执行，不阻塞 mutation 结果返回。
func (m *Manager) after(result *skillstore.Result, err error) {
	if err != nil || m == nil || m.mutationHandler == nil || result == nil {
		return
	}
	if !result.Success {
		return
	}

	mutation := Mutation{
		Kind:   actionToMutationKind(result.Action),
		Name:   result.Name,
		Action: result.Action,
	}
	_ = m.mutationHandler.Handle(context.Background(), mutation)
}

// actionToMutationKind 将 Store 的 action 字符串映射为 MutationKind。
// create → MutationCreate（触发投影）
// patch / write_file / remove_file → MutationModify（仅 catalog reload）
func actionToMutationKind(action string) SkillMutationKind {
	switch action {
	case skillstore.ActionCreate:
		return MutationCreate
	default:
		return MutationModify
	}
}
