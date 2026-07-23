package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

const (
	translateBatchSize  = 25
	translateMaxWorkers = 4
)

type defaultSkillDescriptionTranslator struct {
	db           *gorm.DB
	modelInvoker modelrouter.Invoker
}

func NewDefaultSkillDescriptionTranslator(db *gorm.DB, modelInvoker modelrouter.Invoker) SkillDescriptionTranslator {
	return &defaultSkillDescriptionTranslator{
		db:           db,
		modelInvoker: modelInvoker,
	}
}

type translationRequest struct {
	SkillID     string `json:"skill_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// translationResponse 模型返回的翻译结果项。
type translationResponse struct {
	SkillID     string `json:"skill_id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// Translate 批量生成中文展示名并翻译英文 Skill 描述。
// 将 items 按 20 条一组分批，最多 3 个并发调用 LLM。
func (t *defaultSkillDescriptionTranslator) Translate(ctx context.Context, items []TranslateItem) (map[string]TranslatedSkillText, error) {
	if len(items) == 0 {
		return map[string]TranslatedSkillText{}, nil
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		logs.WarnContextf(ctx, "skill translator: no authenticated caller, skip translation")
		return map[string]TranslatedSkillText{}, nil
	}

	model, err := llm.ResolveSystemTranslationLLMModel(ctx, t.db, caller.OrgID)
	if err != nil {
		logs.WarnContextf(ctx, "skill translator: get system translation LLM model: %v", err)
		return map[string]TranslatedSkillText{}, nil
	}
	if model == nil {
		logs.WarnContextf(ctx, "skill translator: no system translation LLM model for org %d", caller.OrgID)
		return map[string]TranslatedSkillText{}, nil
	}

	return t.translateBatches(ctx, caller.OrgID, model.ID, model.Code, items, caller.Uin)
}

// translateBatches 将 items 按 batchSize 分组后并发翻译，合并结果。
func (t *defaultSkillDescriptionTranslator) translateBatches(ctx context.Context, orgID, modelID uint, modelCode string, items []TranslateItem, uin uint) (map[string]TranslatedSkillText, error) {
	var batches [][]TranslateItem
	for i := 0; i < len(items); i += translateBatchSize {
		end := i + translateBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	if len(batches) == 1 {
		return t.doTranslate(ctx, orgID, modelID, modelCode, batches[0], uin)
	}

	type batchResult struct {
		translations map[string]TranslatedSkillText
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

			tMap, err := t.doTranslate(ctx, orgID, modelID, modelCode, batch, uin)
			select {
			case resultCh <- batchResult{translations: tMap, err: err}:
			case <-ctx.Done():
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	merged := make(map[string]TranslatedSkillText, len(items))
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

// doTranslate 对一批 items 调用 LLM 翻译，返回 skill_id → 中文展示文案的映射。
func (t *defaultSkillDescriptionTranslator) doTranslate(ctx context.Context, orgID, modelID uint, modelCode string, items []TranslateItem, uin uint) (map[string]TranslatedSkillText, error) {
	reqItems := make([]translationRequest, len(items))
	for i, item := range items {
		reqItems[i] = translationRequest{SkillID: item.SkillID, Name: item.Name, Description: item.Description}
	}
	reqJSON, _ := json.Marshal(reqItems)

	prompt := fmt.Sprintf(`Translate the following skill marketplace copy from English to Simplified Chinese and generate a concise Chinese display name. Return ONLY a valid JSON array, no markdown, no code fences.

Format:
[{"skill_id":"...","display_name":"短中文名","description":"Chinese translation..."}]

The array must have exactly %d items, each skill_id must match an input skill_id.

Rules for display_name:
1. Generate it from both name and description.
2. Prefer 2-8 Chinese characters or a short Chinese noun phrase.
3. Do not include punctuation.
4. Do not append "技能" unless the name would be unclear without it.
5. Preserve product names, brand names, and file format names such as PDF, Excel, Word, Notion, GitHub.

Input:
%s`, len(items), string(reqJSON))

	temperature := 0.1
	result, err := t.modelInvoker.Call(ctx, orgID, &llm.CallRequest{
		ModelID:     modelID,
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: &temperature,
		ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
			Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		},
		ReasoningEffort: einoopenai.ReasoningEffortLevelLow,
		CallerType:      "skill_translator",
		Uin:             uin,
	}, modelrouter.WithModelCode(modelCode))
	if err != nil {
		return nil, fmt.Errorf("LLM generate: %w", err)
	}

	content := strings.TrimSpace(result.Message.Content)
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

	translationMap := make(map[string]TranslatedSkillText, len(results))
	for _, r := range results {
		displayName := strings.TrimSpace(r.DisplayName)
		description := strings.TrimSpace(r.Description)
		if r.SkillID != "" && (displayName != "" || description != "") {
			translationMap[r.SkillID] = TranslatedSkillText{
				DisplayName: displayName,
				Description: description,
			}
		}
	}
	return translationMap, nil
}

// TranslateDocument 批量翻译整篇 SKILL.md，保留 Markdown 结构只翻译自然语言。
func (t *defaultSkillDescriptionTranslator) TranslateDocument(ctx context.Context, items []TranslateDocumentItem) (map[string]string, error) {
	if len(items) == 0 {
		return map[string]string{}, nil
	}

	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		logs.WarnContextf(ctx, "skill translator: no authenticated caller, skip document translation")
		return map[string]string{}, nil
	}

	model, err := llm.ResolveSystemTranslationLLMModel(ctx, t.db, caller.OrgID)
	if err != nil {
		logs.WarnContextf(ctx, "skill translator: get system translation LLM model: %v", err)
		return map[string]string{}, nil
	}
	if model == nil {
		logs.WarnContextf(ctx, "skill translator: no system translation LLM model for org %d", caller.OrgID)
		return map[string]string{}, nil
	}

	return t.translateDocumentBatches(ctx, caller.OrgID, model.ID, model.Code, items, caller.Uin)
}

// translateDocumentBatches 将全篇 SKILL.md 按批分组并发翻译。
func (t *defaultSkillDescriptionTranslator) translateDocumentBatches(ctx context.Context, orgID, modelID uint, modelCode string, items []TranslateDocumentItem, uin uint) (map[string]string, error) {
	var batches [][]TranslateDocumentItem
	for i := 0; i < len(items); i += translateBatchSize {
		end := i + translateBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	if len(batches) == 1 {
		return t.doTranslateDocument(ctx, orgID, modelID, modelCode, batches[0], uin)
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

			tMap, err := t.doTranslateDocument(ctx, orgID, modelID, modelCode, batch, uin)
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
			logs.WarnContextf(ctx, "skill translator: batch document translate failed: %v", r.err)
			continue
		}
		for k, v := range r.translations {
			merged[k] = v
		}
	}
	return merged, nil
}

// doTranslateDocument 对一批整篇 SKILL.md 调用 LLM 翻译，只翻译自然语言为简体中文。
// 保留 YAML frontmatter、标题层级、列表、代码块、链接、表格等 Markdown 结构。
// 翻译结果需要能被 catalog.ParseDocument 解析，否则丢弃并记录 warning。
func (t *defaultSkillDescriptionTranslator) doTranslateDocument(ctx context.Context, orgID, modelID uint, modelCode string, items []TranslateDocumentItem, uin uint) (map[string]string, error) {
	// 构造请求，每篇之间用分隔线隔开
	var inputBuilder strings.Builder
	inputBuilder.WriteString(fmt.Sprintf("Translate %d skill document(s) below.\n\n", len(items)))
	for i, item := range items {
		inputBuilder.WriteString(fmt.Sprintf("=== DOCUMENT %d (ID: %s) ===\n", i+1, item.SkillID))
		inputBuilder.WriteString(item.Content)
		inputBuilder.WriteString("\n\n")
	}

	prompt := fmt.Sprintf(`You are translating SKILL.md documents from English to Simplified Chinese.

Rules:
1. Keep the YAML frontmatter structure intact (delimiters, field names, indentation). Do NOT change field names.
2. **IMPORTANT: The YAML "description:" field inside the frontmatter MUST be translated to Simplified Chinese.** This is the only frontmatter field that gets translated.
3. All other frontmatter fields (name, version, metadata, etc.) must remain UNCHANGED.
4. Keep all Markdown structure: heading levels (#, ##), lists (-, *), "fenced code blocks", "inline code", links ([text](url)), tables, blockquotes, and horizontal rules.
5. Only translate natural language text (paragraphs, list item text, heading text, table cell text, link text, alt text) to Simplified Chinese.
6. Preserve all code blocks and their content exactly as-is — never translate code, comments, or code examples.
7. Preserve all URLs, file paths, and technical identifiers.
8. Keep the exact same number of documents in the output as the input.
9. Return ONLY valid JSON, no markdown fences, no extra text.

Output format:
[{"skill_id":"...","content":"full translated SKILL.md with frontmatter preserved..."}]

Documents to translate:
%s`, inputBuilder.String())

	temperature := 0.1
	result, err := t.modelInvoker.Call(ctx, orgID, &llm.CallRequest{
		ModelID:     modelID,
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: &temperature,
		ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
			Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		},
		ReasoningEffort: einoopenai.ReasoningEffortLevelLow,
		CallerType:      "skill_translator",
		Uin:             uin,
	}, modelrouter.WithModelCode(modelCode))
	if err != nil {
		return nil, fmt.Errorf("LLM generate: %w", err)
	}

	content := strings.TrimSpace(result.Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var results []struct {
		SkillID string `json:"skill_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(content), &results); err != nil {
		return nil, fmt.Errorf("parse response JSON: %w", err)
	}

	translationMap := make(map[string]string, len(results))
	for _, r := range results {
		if r.SkillID == "" || r.Content == "" {
			continue
		}
		// 清理 LLM 输出格式后再验证
		cleaned := cleanTranslatedContent(r.Content)
		if _, _, parseErr := catalog.ParseDocument([]byte(cleaned)); parseErr != nil {
			logs.WarnContextf(context.Background(), "TranslateDocument: result for %q failed ParseDocument (%v), skipping", r.SkillID, parseErr)
			continue
		}
		translationMap[r.SkillID] = cleaned
	}
	return translationMap, nil
}

// cleanTranslatedContent 清理 LLM 返回翻译内容的常见格式问题。
// 处理：去除 markdown 代码围栏、去除 frontmatter 前的空行。
func cleanTranslatedContent(raw string) string {
	s := strings.TrimSpace(raw)

	// 尝试去除包裹的 markdown 代码围栏
	for _, fence := range []string{"```markdown", "```md", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}

	return s
}
