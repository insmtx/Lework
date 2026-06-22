package service

import "context"

// TranslateItem 待翻译的 Skill 描述条目。
type TranslateItem struct {
	SkillID     string
	Description string // 原始描述（通常是英文）
}

// SkillDescriptionTranslator 将英文 Skill 描述翻译为中文。
type SkillDescriptionTranslator interface {
	// Translate 批量翻译描述。
	// 返回 map[skill_id]translatedDescription，出错或无法翻译时返回空 map。
	Translate(ctx context.Context, items []TranslateItem) (map[string]string, error)
}
