package service

import "context"

// TranslateItem 待翻译的 Skill 市场文案条目。
type TranslateItem struct {
	SkillID     string
	Name        string
	Description string // 原始描述（通常是英文）
}

// TranslatedSkillText 是 Skill 面向中文用户展示的文案。
type TranslatedSkillText struct {
	DisplayName string
	Description string
}

// TranslateDocumentItem 待翻译的整篇 SKILL.md 条目。
type TranslateDocumentItem struct {
	SkillID string // 用于映射返回结果
	Content string // 原始 SKILL.md 完整内容（含 YAML frontmatter）
}

// SkillDescriptionTranslator 将英文 Skill 市场文案翻译为中文。
type SkillDescriptionTranslator interface {
	// Translate 批量生成中文展示名并翻译描述。
	// 返回 map[skill_id]TranslatedSkillText，出错或无法翻译时返回空 map。
	Translate(ctx context.Context, items []TranslateItem) (map[string]TranslatedSkillText, error)

	// TranslateDocument 批量翻译整篇 SKILL.md。
	// 保留 YAML frontmatter、标题层级、列表、代码块、链接、表格等 Markdown 结构，
	// 只翻译自然语言为简体中文。
	// 返回 map[skill_id]translatedFullContent。
	// 某篇翻译结果无法被 catalog.ParseDocument 解析时，该条目不出现在结果 map 中
	// 并记录 warning（调用方据此回退原始 SKILL.md）。
	TranslateDocument(ctx context.Context, items []TranslateDocumentItem) (map[string]string, error)
}
