package types

import "gorm.io/gorm"

// AITeammateTemplate stores preset AI teammate profiles for marketplace-style creation.
type AITeammateTemplate struct {
	gorm.Model

	// Code is the globally unique business identifier for the template.
	Code string `gorm:"column:code;type:varchar(128);not null;uniqueIndex" json:"code"`
	// Name is the display name shown in the AI teammate template list.
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Description is the short user-facing summary.
	Description string `gorm:"column:description;type:text" json:"description"`
	// Avatar stores a URL or built-in avatar resource identifier.
	Avatar string `gorm:"column:avatar;type:varchar(500)" json:"avatar"`
	// Provider records where the preset teammate profile comes from.
	Provider string `gorm:"column:provider;type:varchar(100)" json:"provider"`
	// SystemPrompt is copied to DigitalAssistant.SystemPrompt when creating from the template.
	SystemPrompt string `gorm:"column:system_prompt;type:text;not null" json:"system_prompt"`
	// Expertise stores the template's domain tags.
	Expertise SkillStringList `gorm:"column:expertise;type:jsonb" json:"expertise"`
	// Category supports filtering in the AI teammate marketplace.
	Category string `gorm:"column:category;type:varchar(100);not null;index" json:"category"`
	// Tags stores search and display labels.
	Tags SkillStringList `gorm:"column:tags;type:jsonb" json:"tags"`
	// SortOrder controls default ordering in the marketplace.
	SortOrder int `gorm:"column:sort_order;type:integer;not null;default:0;index" json:"sort_order"`
	// UseCount records successful teammate creations from this template.
	UseCount int64 `gorm:"column:use_count;type:bigint;not null;default:0" json:"use_count"`
	// RecommendCount records template recommendation exposure actions.
	RecommendCount int64 `gorm:"column:recommend_count;type:bigint;not null;default:0" json:"recommend_count"`
	// Status controls whether the template can be listed and used.
	Status string `gorm:"column:status;type:varchar(32);not null;default:active;index" json:"status"`
	// IsSystem marks templates shipped by the platform.
	IsSystem bool `gorm:"column:is_system;type:boolean;not null;default:true" json:"is_system"`
}

// TableName returns the AI teammate template table name.
func (AITeammateTemplate) TableName() string {
	return TableNameAITeammateTemplate
}
