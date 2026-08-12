package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/internal/llm"
)

type recordingTranslationCaller struct {
	llm.Caller
	requests []*llm.CallRequest
	response *llm.CallResult
}

func (c *recordingTranslationCaller) Call(
	_ context.Context,
	_ uint,
	req *llm.CallRequest,
) (*llm.CallResult, error) {
	c.requests = append(c.requests, req)
	return c.response, nil
}

func TestParseTranslationResponsesSupportsObjectAndFencedArray(t *testing.T) {
	objectResponses, err := parseTranslationResponses(`{"items":[{"skill_id":"1","display_name":"中文名","description":"中文描述"}]}`)
	if err != nil {
		t.Fatalf("parse object translation response: %v", err)
	}
	if len(objectResponses) != 1 || objectResponses[0].SkillID != "1" {
		t.Fatalf("object translation response = %#v", objectResponses)
	}

	arrayResponses, err := parseTranslationResponses("```json\n[{\"skill_id\":\"2\",\"display_name\":\"中文名\"}]\n```")
	if err != nil {
		t.Fatalf("parse fenced array translation response: %v", err)
	}
	if len(arrayResponses) != 1 || arrayResponses[0].SkillID != "2" {
		t.Fatalf("fenced array translation response = %#v", arrayResponses)
	}
}

func TestParseDocumentTranslationResponsesSupportsObjectPayload(t *testing.T) {
	responses, err := parseDocumentTranslationResponses(`{"items":[{"skill_id":"1","content":"---\nname: demo\n---\n\n中文正文"}]}`)
	if err != nil {
		t.Fatalf("parse document translation response: %v", err)
	}
	if len(responses) != 1 || responses[0].SkillID != "1" || responses[0].Content == "" {
		t.Fatalf("document translation response = %#v", responses)
	}
}

func TestCleanTranslatedContentRemovesMarkdownFence(t *testing.T) {
	got := cleanTranslatedContent("```markdown\n中文正文\n```")
	if got != "中文正文" {
		t.Fatalf("cleanTranslatedContent() = %q", got)
	}
}

func TestTranslationRequestsUseExpandedOutputTokenLimits(t *testing.T) {
	caller := &recordingTranslationCaller{
		response: &llm.CallResult{
			Message: &llm.SchemaMessage{
				Content: `{"items":[{"skill_id":"metadata","display_name":"中文名","description":"中文描述"}]}`,
			},
		},
	}
	translator := newDefaultSkillDescriptionTranslator(nil, caller)

	if _, err := translator.doTranslate(context.Background(), 1, 9, []TranslateItem{{
		SkillID: "metadata", Name: "English name", Description: "English description",
	}}); err != nil {
		t.Fatalf("doTranslate() error = %v", err)
	}
	if got := *caller.requests[0].MaxTokens; got != translateMetadataMaxTokens {
		t.Fatalf("metadata MaxTokens = %d, want %d", got, translateMetadataMaxTokens)
	}
	if caller.requests[0].SystemPrompt == "" || !strings.Contains(caller.requests[0].SystemPrompt, "至少 80%") ||
		!strings.Contains(caller.requests[0].SystemPrompt, "至少占 60%") ||
		!strings.Contains(caller.requests[0].SystemPrompt, "可保留必要专业名词") {
		t.Fatalf("metadata system prompt does not enforce separate name and description thresholds: %q", caller.requests[0].SystemPrompt)
	}
	if !strings.Contains(caller.requests[0].Messages[0].Content, "至少占 80%") ||
		!strings.Contains(caller.requests[0].Messages[0].Content, "至少 60%") ||
		!strings.Contains(caller.requests[0].Messages[0].Content, "Unix、PDF、API、UTC、.docx") {
		t.Fatalf("metadata prompt does not define name and description translation rules: %q", caller.requests[0].Messages[0].Content)
	}

	document := "---\nname: demo\ndescription: English description\n---\n\nEnglish body"
	caller.response.Message.Content = fmt.Sprintf(
		`{"items":[{"skill_id":"document","content":%s}]}`,
		strconv.Quote("---\nname: demo\ndescription: 中文描述\n---\n\n中文正文"),
	)
	if _, err := translator.doTranslateDocument(context.Background(), 1, 9, []TranslateDocumentItem{{
		SkillID: "document", Content: document,
	}}); err != nil {
		t.Fatalf("doTranslateDocument() error = %v", err)
	}
	if got := *caller.requests[1].MaxTokens; got != translateDocumentMaxTokens {
		t.Fatalf("document MaxTokens = %d, want %d", got, translateDocumentMaxTokens)
	}
}

func TestDoTranslateKeepsValidItemsWhenResponseIsPartial(t *testing.T) {
	caller := &recordingTranslationCaller{
		response: &llm.CallResult{
			Message: &llm.SchemaMessage{
				Content: `{"items":[{"skill_id":"valid","display_name":"中文技能","description":"中文描述"}]}`,
			},
		},
	}
	translator := newDefaultSkillDescriptionTranslator(nil, caller)
	translations, err := translator.doTranslate(context.Background(), 1, 9, []TranslateItem{
		{SkillID: "valid", Name: "Valid", Description: "Valid description"},
		{SkillID: "missing", Name: "Missing", Description: "Missing description"},
	})
	if err == nil {
		t.Fatal("partial translation response should return an error")
	}
	if got := translations["valid"]; got.DisplayName != "中文技能" || got.Description != "中文描述" {
		t.Fatalf("valid partial translation = %#v", got)
	}
	if _, exists := translations["missing"]; exists {
		t.Fatal("missing translation must not be fabricated")
	}
}
