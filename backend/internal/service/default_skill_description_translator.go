package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

const (
	translateBatchSize  = 25 // 每批最多 25 条，避免 prompt 过长
	translateMaxWorkers = 4  // 最多 4 个并发翻译
)

// defaultSkillDescriptionTranslator 使用组织默认 LLM 翻译 Skill 描述。
type defaultSkillDescriptionTranslator struct {
	db *gorm.DB
}

// NewDefaultSkillDescriptionTranslator 创建默认翻译器。
func NewDefaultSkillDescriptionTranslator(db *gorm.DB) SkillDescriptionTranslator {
	return &defaultSkillDescriptionTranslator{db: db}
}

// translationRequest 发送给模型的翻译请求项。
type translationRequest struct {
	SkillID     string `json:"skill_id"`
	Description string `json:"description"`
}

// translationResponse 模型返回的翻译结果项。
type translationResponse struct {
	SkillID     string `json:"skill_id"`
	Description string `json:"description"`
}

// Translate 批量翻译英文 Skill 描述为中文。
// 将 items 按 20 条一组分批，最多 3 个并发调用 LLM。
func (t *defaultSkillDescriptionTranslator) Translate(ctx context.Context, items []TranslateItem) (map[string]string, error) {
	if len(items) == 0 {
		return map[string]string{}, nil
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		logs.WarnContextf(ctx, "skill translator: no authenticated caller, skip translation")
		return map[string]string{}, nil
	}

	model, err := infradb.GetDefaultLLMModel(ctx, t.db, caller.OrgID)
	if err != nil {
		logs.WarnContextf(ctx, "skill translator: get default LLM model: %v", err)
		return map[string]string{}, nil
	}
	if model == nil {
		logs.WarnContextf(ctx, "skill translator: no default LLM model for org %d", caller.OrgID)
		return map[string]string{}, nil
	}

	chatModel, err := t.buildChatModel(ctx, model)
	if err != nil {
		return map[string]string{}, nil
	}

	return t.translateBatches(ctx, chatModel, items)
}

// buildChatModel 创建 ChatModel 实例，直接连接上游 LLM。
func (t *defaultSkillDescriptionTranslator) buildChatModel(ctx context.Context, m *types.LLMModel) (model.ToolCallingChatModel, error) {
	endpointURL := buildLLMEndpointURL(m.BaseURL, m.BaseURLHasV1)

	jsonFormat := einoopenai.ChatCompletionResponseFormat{
		Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
	}

	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider:       m.Provider,
		APIKey:         m.APIKeyEncrypted,
		Model:          m.ModelName,
		BaseURL:        endpointURL,
		ResponseFormat: &jsonFormat,
	})
	if err != nil {
		logs.WarnContextf(ctx, "skill translator: create chat model: %v", err)
		return nil, err
	}
	return chatModel, nil
}

// translateBatches 将 items 按 batchSize 分组后并发翻译，合并结果。
func (t *defaultSkillDescriptionTranslator) translateBatches(ctx context.Context, chatModel model.ToolCallingChatModel, items []TranslateItem) (map[string]string, error) {
	var batches [][]TranslateItem
	for i := 0; i < len(items); i += translateBatchSize {
		end := i + translateBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	if len(batches) == 1 {
		return t.doTranslate(ctx, chatModel, batches[0])
	}

	type batchResult struct {
		translations map[string]string
		err          error
	}

	resultCh := make(chan batchResult, len(batches))
	sem := make(chan struct{}, translateMaxWorkers)
	var wg sync.WaitGroup

	for _, batch := range batches {
		batch := batch
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			tMap, err := t.doTranslate(ctx, chatModel, batch)
			select {
			case resultCh <- batchResult{translations: tMap, err: err}:
			case <-ctx.Done():
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	merged := make(map[string]string, len(items))
	for r := range resultCh {
		if r.err != nil {
			logs.WarnContextf(ctx, "skill translator: batch translate failed: %v", r.err)
			continue
		}
		for k, v := range r.translations {
			merged[k] = v
		}
	}
	return merged, nil
}

// doTranslate 对一批 items 调用 LLM 翻译，返回 skill_id → 中文描述的映射。
func (t *defaultSkillDescriptionTranslator) doTranslate(ctx context.Context, chatModel model.ToolCallingChatModel, items []TranslateItem) (map[string]string, error) {
	reqItems := make([]translationRequest, len(items))
	for i, item := range items {
		reqItems[i] = translationRequest{SkillID: item.SkillID, Description: item.Description}
	}
	reqJSON, _ := json.Marshal(reqItems)

	prompt := fmt.Sprintf(`Translate the following skill descriptions from English to Chinese (Simplified). Return ONLY a valid JSON array, no markdown, no code fences.

Format:
[{"skill_id":"...","description":"Chinese translation..."}]

The array must have exactly %d items, each skill_id must match an input skill_id.

Input:
%s`, len(items), string(reqJSON))

	messages := []*schema.Message{
		{Role: schema.User, Content: prompt},
	}
	resp, err := chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM generate: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var results []translationResponse
	if err := json.Unmarshal([]byte(content), &results); err != nil {
		return nil, fmt.Errorf("parse response JSON: %w", err)
	}

	if len(results) != len(items) {
		return nil, fmt.Errorf("response length %d != input length %d", len(results), len(items))
	}

	translationMap := make(map[string]string, len(results))
	for _, r := range results {
		if r.SkillID != "" && r.Description != "" {
			translationMap[r.SkillID] = r.Description
		}
	}
	return translationMap, nil
}
