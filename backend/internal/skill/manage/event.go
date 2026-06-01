package manage

import "context"

// SkillMutationKind 将 Skill 变更分两类：创建新 skill 和修改已有 skill。
// 投影层（外部 CLI symlink）只对创建事件建 link；修改事件通过 symlink 天然可见，无需投影动作。
type SkillMutationKind int

const (
	MutationCreate SkillMutationKind = iota // 创建新 skill 目录
	MutationModify                          // patch / write_file / remove_file
)

// Mutation 表示一次已完成的 skill 变更结果。
// 由 Manager 在执行成功 (result.Success == true) 后构造，驱动后续 handler 链。
type Mutation struct {
	Kind   SkillMutationKind // 变更类型
	Name   string            // skill 名称
	Action string            // 原始 action 字符串（create/patch/write_file/remove_file），用于日志
}

// MutationHandler 处理 skill 变更事件。handler 可组合。
// 典型用途：CatalogReloadHandler（刷新只读快照）、ProjectionHandler（维护外部 CLI symlink）。
type MutationHandler interface {
	Handle(ctx context.Context, m Mutation) error
}
