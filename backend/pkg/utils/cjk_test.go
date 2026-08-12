package utils

import "testing"

func TestCJKRatioMarkdownIgnoresTechnicalMarkdown(t *testing.T) {
	content := "中文说明 `english_identifier`\n\n```go\nfunc main() {}\n```\n\n[文档](https://example.com/docs)"
	if got := CJKRatioMarkdown(content); got < 0.6 {
		t.Fatalf("CJKRatioMarkdown() = %v, want at least 0.6", got)
	}
}

func TestCJKRatioMarkdownDoesNotCountEnglishCodeAsChinese(t *testing.T) {
	content := "```go\nfunc translateSkill() {}\n```"
	if got := CJKRatioMarkdown(content); got != 0 {
		t.Fatalf("CJKRatioMarkdown() = %v, want 0", got)
	}
}
