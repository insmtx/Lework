package db

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

const (
	defaultPromptBlockLimit = 6
	defaultMemoryLimit      = 5
)

// ListDigitalAssistantPromptBlocks returns enabled persona prompt blocks for an assistant.
func ListDigitalAssistantPromptBlocks(ctx context.Context, db *gorm.DB, assistantID uint, query string, limit int) ([]*types.DigitalAssistantPromptBlock, error) {
	if assistantID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultPromptBlockLimit
	}

	var entities []*types.DigitalAssistantPromptBlock
	stmt := db.WithContext(ctx).
		Model(&types.DigitalAssistantPromptBlock{}).
		Where("assistant_id = ? AND enabled = ?", assistantID, true)

	keyword := strings.ToLower(strings.TrimSpace(query))
	if keyword != "" {
		pattern := "%" + keyword + "%"
		stmt = stmt.Where(
			"block_type IN ? OR LOWER(title) LIKE ? OR LOWER(content) LIKE ?",
			[]string{"identity", "boundary", "style"},
			pattern,
			pattern,
		)
	}

	err := stmt.
		Order("priority DESC").
		Order("block_type ASC").
		Order("id ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// ListRelevantDigitalAssistantMemories returns enabled memories matching the query with a safe fallback.
func ListRelevantDigitalAssistantMemories(ctx context.Context, db *gorm.DB, assistantID uint, query string, limit int) ([]*types.DigitalAssistantMemory, error) {
	if assistantID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultMemoryLimit
	}

	keyword := strings.ToLower(strings.TrimSpace(query))
	if keyword != "" {
		matched, err := listDigitalAssistantMemories(ctx, db, assistantID, keyword, limit)
		if err != nil {
			return nil, err
		}
		if len(matched) > 0 {
			return matched, nil
		}
	}
	return listDigitalAssistantMemories(ctx, db, assistantID, "", limit)
}

func listDigitalAssistantMemories(ctx context.Context, db *gorm.DB, assistantID uint, keyword string, limit int) ([]*types.DigitalAssistantMemory, error) {
	var entities []*types.DigitalAssistantMemory
	stmt := db.WithContext(ctx).
		Model(&types.DigitalAssistantMemory{}).
		Where("assistant_id = ? AND enabled = ?", assistantID, true)

	if keyword != "" {
		pattern := "%" + keyword + "%"
		stmt = stmt.Where(
			"LOWER(content) LIKE ? OR LOWER(memory_type) LIKE ? OR LOWER(source_type) LIKE ?",
			pattern,
			pattern,
			pattern,
		)
	}

	err := stmt.
		Order("confidence DESC").
		Order("updated_at DESC").
		Order("id ASC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// CreateDigitalAssistantMemory stores a teammate memory candidate or confirmed memory.
func CreateDigitalAssistantMemory(ctx context.Context, db *gorm.DB, memory *types.DigitalAssistantMemory) error {
	if memory == nil {
		return errors.New("digital assistant memory is required")
	}
	return db.WithContext(ctx).Create(memory).Error
}

// CreateAssistantPromptTrace stores prompt assembly trace metadata.
func CreateAssistantPromptTrace(ctx context.Context, db *gorm.DB, trace *types.AssistantPromptTrace) error {
	if trace == nil {
		return errors.New("assistant prompt trace is required")
	}
	return db.WithContext(ctx).Create(trace).Error
}
