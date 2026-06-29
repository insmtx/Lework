package assistantcontext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

func setupSkillsRoot(t *testing.T) string {
	t.Helper()
	rootDir := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, rootDir)
	skillsDir := filepath.Join(rootDir, ".leros", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	return skillsDir
}

func writeTestSkill(t *testing.T, skillsDir string, name string, description string, body string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func writeTestSkillWithSupportingFile(t *testing.T, skillsDir string, name string, description string, body string, relFilePath string, fileContent string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	// Create supporting file directory if needed
	supportPath := filepath.Join(dir, filepath.FromSlash(relFilePath))
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatalf("mkdir supporting dir: %v", err)
	}
	if err := os.WriteFile(supportPath, []byte(fileContent), 0o644); err != nil {
		t.Fatalf("write supporting file: %v", err)
	}
}

func TestApplyInvokedSkillsSingleSkill(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions body")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch 查今天 AI 新闻"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "anysearch") {
		t.Fatalf("expected content to mention anysearch, got: %s", content)
	}
	if !strings.Contains(content, "Search instructions body") {
		t.Fatalf("expected content to contain SKILL.md body, got: %s", content)
	}
	if !strings.Contains(content, "查今天 AI 新闻") {
		t.Fatalf("expected content to contain user instruction '查今天 AI 新闻', got: %s", content)
	}
	if !strings.Contains(content, "[IMPORTANT:") {
		t.Fatalf("expected content to have IMPORTANT header, got: %s", content)
	}
	if !strings.Contains(content, "Skill directory:") {
		t.Fatalf("expected content to have Skill directory section, got: %s", content)
	}
	if !strings.Contains(content, "supporting files") {
		t.Fatalf("expected content to have supporting files section, got: %s", content)
	}
}

func TestApplyInvokedSkillsNoSkill(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "请帮我查资料"},
			},
		},
	}

	original := req.Input.Messages[0].Content
	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	if req.Input.Messages[0].Content != original {
		t.Fatalf("expected content unchanged, got %q", req.Input.Messages[0].Content)
	}
}

func TestApplyInvokedSkillsEmptyMessages(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: nil,
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}
}

func TestApplyInvokedSkillsMultiSkillSingleMessage(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")
	writeTestSkill(t, skillsDir, "xiaohongshu", "RedNote writer", "RedNote writing guide")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch /xiaohongshu 写一篇笔记"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "anysearch") {
		t.Fatalf("expected content to mention anysearch")
	}
	if !strings.Contains(content, "xiaohongshu") {
		t.Fatalf("expected content to mention xiaohongshu")
	}
	if !strings.Contains(content, "Search instructions") {
		t.Fatalf("expected content to contain anysearch body")
	}
	if !strings.Contains(content, "RedNote writing guide") {
		t.Fatalf("expected content to contain xiaohongshu body")
	}
	if !strings.Contains(content, "写一篇笔记") {
		t.Fatalf("expected content to contain user instruction")
	}
	if !strings.Contains(content, "2 skill(s):") {
		t.Fatalf("expected Skills loaded to mention 2 skills, got: %s", content)
	}
}

func TestApplyInvokedSkillsMultiMessagesDedup(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")
	writeTestSkill(t, skillsDir, "xiaohongshu", "RedNote writer", "RedNote writing guide")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch 查资料"},
				{Role: "user", Content: "/anysearch /xiaohongshu 写笔记"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	msg1 := req.Input.Messages[0].Content
	if !strings.Contains(msg1, "Search instructions") {
		t.Fatalf("msg1 should contain anysearch body, got: %s", msg1)
	}
	if !strings.Contains(msg1, "1 skill(s):") {
		t.Fatalf("msg1 should say 1 skill, got: %s", msg1)
	}

	msg2 := req.Input.Messages[1].Content
	if strings.Contains(msg2, "Search instructions") {
		t.Fatalf("msg2 should NOT contain anysearch body (deduped), got: %s", msg2)
	}
	if !strings.Contains(msg2, "RedNote writing guide") {
		t.Fatalf("msg2 should contain xiaohongshu body")
	}
	if !strings.Contains(msg2, "1 skill(s):") {
		t.Fatalf("msg2 should say 1 skill (only new ones), got: %s", msg2)
	}
	if strings.Contains(msg2, "anysearch") {
		t.Fatalf("msg2 should NOT contain anysearch at all (deduped, header only lists new skills), got: %s", msg2)
	}
}

func TestApplyInvokedSkillsDuplicateOnlyMessageStripsToken(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch 查资料"},
				{Role: "user", Content: "/anysearch 继续查更多"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	msg2 := req.Input.Messages[1].Content
	if msg2 != "继续查更多" {
		t.Fatalf("expected duplicate-only message to keep only stripped user text, got: %s", msg2)
	}
	if strings.Contains(msg2, "0 skill(s)") || strings.Contains(msg2, "Search instructions") {
		t.Fatalf("expected duplicate-only message to avoid prompt injection, got: %s", msg2)
	}
}

func TestApplyInvokedSkillsNoInstruction(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "Search instructions") {
		t.Fatalf("expected skill body to be loaded")
	}
	if !strings.Contains(content, "User instruction:") {
		t.Fatalf("expected User instruction section")
	}
}

func TestApplyInvokedSkillsNotFound(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/unknown 查资料"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestApplyInvokedSkillsNotAtStart(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "请使用 /anysearch 查资料"},
			},
		},
	}

	original := req.Input.Messages[0].Content
	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	if req.Input.Messages[0].Content != original {
		t.Fatalf("expected content unchanged when /skill not at start, got: %s", req.Input.Messages[0].Content)
	}
}

func TestApplyInvokedSkillsNumericStartNoMatch(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/123skill 内容"},
			},
		},
	}

	original := req.Input.Messages[0].Content
	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	if req.Input.Messages[0].Content != original {
		t.Fatalf("expected content unchanged for numeric-start skill ID, got: %s", req.Input.Messages[0].Content)
	}
}

func TestApplyInvokedSkillsSkipNonUser(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "assistant", Content: "/anysearch 我来帮你查"},
				{Role: "user", Content: "hi"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	// assistant 消息不应被改写
	if strings.Contains(req.Input.Messages[0].Content, "SKILL.md") {
		t.Fatalf("assistant message should not be rewritten, got: %s", req.Input.Messages[0].Content)
	}
	// user 消息应保持原样
	if req.Input.Messages[1].Content != "hi" {
		t.Fatalf("user message should be unchanged, got: %s", req.Input.Messages[1].Content)
	}
}

func TestApplyInvokedSkillsDashDashNotStripped(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch -- 查今天 AI 新闻"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "-- 查今天 AI 新闻") {
		t.Fatalf("expected '--' to be preserved in user instruction, got: %s", content)
	}
}

func TestApplyInvokedSkillsPreservesTokenOrder(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "skillB", "Skill B", "Body B")
	writeTestSkill(t, skillsDir, "skillA", "Skill A", "Body A")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/skillB /skillA 内容"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content

	// skillB 应该在 skillA 之前出现（保持用户声明顺序）
	idxB := strings.Index(content, "skillB")
	idxA := strings.Index(content, "skillA")
	if idxB == -1 || idxA == -1 {
		t.Fatalf("both skills should be present in content")
	}
	if idxB > idxA {
		t.Fatalf("skillB should appear before skillA (user order), got B at %d, A at %d", idxB, idxA)
	}
}

func TestApplyInvokedSkillsIntraMessageDedup(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch /anysearch 内容"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "1 skill(s):") {
		t.Fatalf("expected 1 skill loaded (intra-message dedup), got: %s", content)
	}
}

func TestApplyInvokedSkillsSupportingFiles(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkillWithSupportingFile(t, skillsDir, "review", "Code review", "Review body",
		"references/policy.md", "policy content")
	writeTestSkillWithSupportingFile(t, skillsDir, "review", "Code review", "Review body",
		"references/guide.md", "guide content")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/review 审查代码"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	if !strings.Contains(content, "references/guide.md") {
		t.Fatalf("expected supporting file guide.md listed, got: %s", content)
	}
	if !strings.Contains(content, "references/policy.md") {
		t.Fatalf("expected supporting file policy.md listed, got: %s", content)
	}
}

func TestApplyInvokedSkillsAbsoluteDir(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/anysearch 查资料"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	// 验证展示的是绝对路径（非相对）
	expectedAbsDir := filepath.Join(skillsDir, "anysearch")
	if !strings.Contains(content, expectedAbsDir) {
		t.Fatalf("expected absolute skill directory %q in content, got: %s", expectedAbsDir, content)
	}
}

func TestApplyInvokedSkillsManifestNameDisplayed(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "anysearch", "Search engine", "Search instructions")

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/ANYSEARCH 查资料"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills: %v", err)
	}

	content := req.Input.Messages[0].Content
	// Manifest 中的真实 name 是 "anysearch"，应展示在 Skills loaded 中
	if !strings.Contains(content, "anysearch") {
		t.Fatalf("expected manifest name 'anysearch' in content (not user-typed case), got: %s", content)
	}
}

func TestApplyInvokedSkillsErrorPreventsProcessing(t *testing.T) {
	setupSkillsRoot(t)

	req := &assistantdomain.RunRequest{
		Input: assistantdomain.InputContext{
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "/valid_skill 先查"},
				{Role: "user", Content: "/unknown_skill 再查"},
			},
		},
	}

	err := ApplyInvokedSkills(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error for nonexistent skill in second message")
	}
}

func TestApplyInvokedSkillsNilRequest(t *testing.T) {
	err := ApplyInvokedSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("ApplyInvokedSkills with nil request should not error: %v", err)
	}
}

func TestParseSkillTokensBasic(t *testing.T) {
	tokens, remaining := skilltoken.ParseTokens("/anysearch 查资料")
	if len(tokens) != 1 || tokens[0] != "anysearch" {
		t.Fatalf("expected [anysearch], got %v", tokens)
	}
	if remaining != "查资料" {
		t.Fatalf("expected '查资料', got %q", remaining)
	}
}

func TestParseSkillTokensMultiple(t *testing.T) {
	tokens, remaining := skilltoken.ParseTokens("/anysearch /xiaohongshu 写笔记")
	if len(tokens) != 2 || tokens[0] != "anysearch" || tokens[1] != "xiaohongshu" {
		t.Fatalf("expected [anysearch xiaohongshu], got %v", tokens)
	}
	if remaining != "写笔记" {
		t.Fatalf("expected '写笔记', got %q", remaining)
	}
}

func TestParseSkillTokensNoMatch(t *testing.T) {
	tokens, remaining := skilltoken.ParseTokens("请帮我查 /anysearch")
	if len(tokens) != 0 {
		t.Fatalf("expected no tokens, got %v", tokens)
	}
	if remaining != "请帮我查 /anysearch" {
		t.Fatalf("expected full content unchanged, got %q", remaining)
	}
}

func TestParseSkillTokensOnlySkill(t *testing.T) {
	tokens, remaining := skilltoken.ParseTokens("/anysearch")
	if len(tokens) != 1 || tokens[0] != "anysearch" {
		t.Fatalf("expected [anysearch], got %v", tokens)
	}
	if remaining != "" {
		t.Fatalf("expected empty remaining, got %q", remaining)
	}
}

func TestParseSkillTokensWithUnderscore(t *testing.T) {
	tokens, remaining := skilltoken.ParseTokens("/my_skill-01 内容")
	if len(tokens) != 1 || tokens[0] != "my_skill-01" {
		t.Fatalf("expected [my_skill-01], got %v", tokens)
	}
	if remaining != "内容" {
		t.Fatalf("expected '内容', got %q", remaining)
	}
}

func TestDedupeOrderedLower(t *testing.T) {
	result := dedupeOrderedLower([]string{"a", "b", "a", "c"})
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Fatalf("expected [a b c], got %v", result)
	}
}

func TestDedupeOrderedLowerCaseInsensitive(t *testing.T) {
	result := dedupeOrderedLower([]string{"A", "B", "a", "c"})
	if len(result) != 3 || result[0] != "A" || result[1] != "B" || result[2] != "c" {
		t.Fatalf("expected [A B c] (case-insensitive dedup), got %v", result)
	}
}

func TestDedupeOrderedLowerEmpty(t *testing.T) {
	result := dedupeOrderedLower([]string{})
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
}

func TestBuildSkillInvokePromptSingleSkill(t *testing.T) {
	entry := &skillcatalog.Entry{
		Manifest: skillcatalog.Manifest{
			Name: "anysearch",
		},
		Body:        "Search instructions",
		AbsoluteDir: "/workspace/.leros/skills/anysearch",
	}
	filesMap := map[string][]string{
		"anysearch": {},
	}

	prompt := buildSkillInvokePrompt(
		[]string{"anysearch"},
		[]*skillcatalog.Entry{entry},
		filesMap,
		"查资料",
	)

	if !strings.Contains(prompt, "1 skill(s): anysearch") {
		t.Fatalf("expected header with 1 skill, got: %s", prompt)
	}
	if !strings.Contains(prompt, "查资料") {
		t.Fatalf("expected user instruction, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Search instructions") {
		t.Fatalf("expected body, got: %s", prompt)
	}
	if !strings.Contains(prompt, "/workspace/.leros/skills/anysearch") {
		t.Fatalf("expected absolute dir, got: %s", prompt)
	}
	if !strings.Contains(prompt, "None") {
		t.Fatalf("expected None for no supporting files, got: %s", prompt)
	}
}

func TestBuildSkillInvokePromptWithSupportingFiles(t *testing.T) {
	entry := &skillcatalog.Entry{
		Manifest: skillcatalog.Manifest{
			Name: "review",
		},
		Body:        "Review body",
		AbsoluteDir: "/workspace/.leros/skills/review",
	}
	filesMap := map[string][]string{
		"review": {"references/guide.md", "references/policy.md"},
	}

	prompt := buildSkillInvokePrompt(
		[]string{"review"},
		[]*skillcatalog.Entry{entry},
		filesMap,
		"review this",
	)

	if !strings.Contains(prompt, "references/guide.md") {
		t.Fatalf("expected supporting file guide.md, got: %s", prompt)
	}
	if !strings.Contains(prompt, "references/policy.md") {
		t.Fatalf("expected supporting file policy.md, got: %s", prompt)
	}
	if strings.Contains(prompt, "None") {
		t.Fatalf("expected no 'None' when files exist, got: %s", prompt)
	}
}

func TestBuildSkillInvokePromptDedupedEntryNotInjected(t *testing.T) {
	xiaohongshuEntry := &skillcatalog.Entry{
		Manifest: skillcatalog.Manifest{
			Name: "xiaohongshu",
		},
		Body:        "RedNote writing guide",
		AbsoluteDir: "/workspace/.leros/skills/xiaohongshu",
	}
	filesMap := map[string][]string{
		"xiaohongshu": {},
	}

	prompt := buildSkillInvokePrompt(
		[]string{"xiaohongshu"}, // 只有新 skill 在 loadedNames 中
		[]*skillcatalog.Entry{xiaohongshuEntry},
		filesMap,
		"写笔记",
	)

	// Header 应只列出 xiaohongshu
	if !strings.Contains(prompt, "1 skill(s): xiaohongshu") {
		t.Fatalf("expected header with 1 skill, got: %s", prompt)
	}

	if !strings.Contains(prompt, "RedNote writing guide") {
		t.Fatalf("expected xiaohongshu body to be injected")
	}
}
