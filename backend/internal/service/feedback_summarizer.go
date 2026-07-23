package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"

	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

const (
	feedbackTitleMaxRunes         = 30
	feedbackUnderstandingMaxRunes = 300
)

type feedbackSummary struct {
	Title         string
	Understanding string
}

type feedbackSummaryPayload struct {
	Title         string `json:"title"`
	Understanding string `json:"understanding"`
}

var summarizeFeedback func(
	ctx context.Context, database *gorm.DB, modelInvoker modelrouter.Invoker, orgID uint, typeLabel, content string, uin uint,
) (feedbackSummary, error) = summarizeFeedbackWithLLM

func summarizeFeedbackWithLLM(ctx context.Context, database *gorm.DB, modelInvoker modelrouter.Invoker, orgID uint, typeLabel, content string, uin uint) (feedbackSummary, error) {
	model, err := llm.ResolveDefaultLLMModel(ctx, database, orgID)
	if err != nil {
		return feedbackSummary{}, fmt.Errorf("get default model: %w", err)
	}
	if model == nil {
		return feedbackSummary{}, fmt.Errorf("no default LLM model configured for org %d", orgID)
	}

	template := prompts.Get(prompts.KeyFeedbackSummarize)
	if template == "" {
		return feedbackSummary{}, fmt.Errorf("prompt %q not registered", prompts.KeyFeedbackSummarize)
	}

	prompt := strings.NewReplacer(
		"{feedback_type}", strings.TrimSpace(typeLabel),
		"{content}", strings.TrimSpace(content),
	).Replace(template)

	temperature := 0.2
	result, err := modelInvoker.Call(ctx, orgID, &llm.CallRequest{
		ModelID: model.ID,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: &temperature,
		ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
			Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		},
		ReasoningEffort: einoopenai.ReasoningEffortLevelLow,
		CallerType:      "feedback_summarizer",
		Uin:             uin,
	})
	if err != nil {
		return feedbackSummary{}, fmt.Errorf("generate feedback summary: %w", err)
	}
	if result == nil || result.Message == nil || strings.TrimSpace(result.Message.Content) == "" {
		return feedbackSummary{}, errors.New("generate feedback summary: empty response")
	}

	return parseFeedbackSummary(result.Message.Content)
}

func parseFeedbackSummary(content string) (feedbackSummary, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var payload feedbackSummaryPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return feedbackSummary{}, fmt.Errorf("parse feedback summary: %w", err)
	}

	summary := feedbackSummary{
		Title:         sanitizeFeedbackTitle(payload.Title),
		Understanding: sanitizeFeedbackUnderstanding(payload.Understanding),
	}
	if summary.Title == "" || summary.Understanding == "" {
		return feedbackSummary{}, errors.New("parse feedback summary: empty title or understanding")
	}
	return summary, nil
}

func summarizeFeedbackBestEffort(ctx context.Context, database *gorm.DB, modelInvoker modelrouter.Invoker, orgID uint, typeLabel, content string, uin uint) feedbackSummary {
	summary, err := summarizeFeedback(ctx, database, modelInvoker, orgID, typeLabel, content, uin)
	if err != nil {
		logs.WarnContextf(ctx, "feedback summary llm failed, using fallback: %v", err)
		return fallbackFeedbackSummary(content)
	}
	return summary
}

func fallbackFeedbackSummary(content string) feedbackSummary {
	title := sanitizeFeedbackTitle(truncateRunes(strings.TrimSpace(content), feedbackTitleMaxRunes))
	if title == "" {
		title = "用户反馈"
	}
	return feedbackSummary{
		Title:         title,
		Understanding: "用户提交了反馈，待进一步查看原文与附件。",
	}
}

func sanitizeFeedbackTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'`“”‘’「」『』.,，。:：;；!?！？[]【】")
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return truncateRunes(title, feedbackTitleMaxRunes)
}

func sanitizeFeedbackUnderstanding(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return truncateRunes(text, feedbackUnderstandingMaxRunes)
}

func buildFeedbackDescription(content, understanding string) string {
	content = strings.TrimSpace(content)
	understanding = strings.TrimSpace(understanding)
	if understanding == "" {
		return content
	}
	if content == "" {
		return "问题理解：\n" + understanding
	}
	return content + "\n\n---\n问题理解：\n" + understanding
}

func buildFeedbackRecordFields(
	typeLabel, content string,
	userName, userPhone, version string,
	summary feedbackSummary,
	attachmentTokens []string,
) map[string]any {
	fields := map[string]any{
		"问题名称": summary.Title,
		"问题描述": buildFeedbackDescription(content, summary.Understanding),
		"问题类型": typeLabel,
		"提交人":  userName,
		"手机号":  userPhone,
	}
	if version != "" {
		fields["版本号"] = version
	}
	if len(attachmentTokens) > 0 {
		attachments := make([]map[string]string, 0, len(attachmentTokens))
		for _, token := range attachmentTokens {
			attachments = append(attachments, map[string]string{"file_token": token})
		}
		fields["附件"] = attachments
	}
	return fields
}

func resolveFeedbackSubmitter(name, phone string) (string, string) {
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" {
		name = phone
	}
	return name, phone
}
