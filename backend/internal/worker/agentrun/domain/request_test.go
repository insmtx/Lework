package domain

import (
	"strings"
	"testing"
)

func TestBuildUserInputPrefersSenderName(t *testing.T) {
	req := &RunRequest{Input: InputContext{Type: InputTypeMessage, Messages: []InputMessage{
		{ID: "m1", Role: "user", Content: "帮我写一个 HTTP server", SenderName: "A"},
		{ID: "m2", Role: "assistant", Content: "好的，以下是代码...", SenderName: "AI队友Alpha"},
		{ID: "m3", Role: "user", Content: "加上 /health 端点", SenderName: "B"},
	}}}
	got := BuildUserInput(req)
	want := "【用户问题】\n[message_id=m1] 用户「A」发送：「帮我写一个 HTTP server」\n【AI 队友回复】\n[message_id=m2] AI 队友「AI队友Alpha」发送：「好的，以下是代码...」\n【用户问题】\n[message_id=m3] 用户「B」发送：「加上 /health 端点」"
	if got != want {
		t.Fatalf("BuildUserInput = %q, want %q", got, want)
	}
}

func TestBuildUserInputFallsBackToRole(t *testing.T) {
	req := &RunRequest{Input: InputContext{Type: InputTypeMessage, Messages: []InputMessage{
		{Role: "user", Content: "hello"},
		{Content: "no role"},
	}}}
	got := BuildUserInput(req)
	if !strings.Contains(got, "用户「user」发送：「hello」") || !strings.Contains(got, "用户「user」发送：「no role」") {
		t.Fatalf("expected role fallback in %q", got)
	}
}

func TestBuildAttachmentText_SingleAttachment(t *testing.T) {
	attachments := []Attachment{
		{Name: "foo.txt", URL: "http://example.com/foo.txt", MimeType: "text/plain"},
	}
	got := BuildAttachmentText(attachments)

	if !strings.Contains(got, "attached by the user in this message") {
		t.Fatalf("expected 'attached by the user in this message' in %q", got)
	}
	if !strings.Contains(got, "- foo.txt") {
		t.Fatalf("expected '- foo.txt' in %q", got)
	}
	if !strings.Contains(got, "Location: uploads/foo.txt") {
		t.Fatalf("expected 'Location: uploads/foo.txt' in %q", got)
	}
	if !strings.Contains(got, "URL: http://example.com/foo.txt") {
		t.Fatalf("expected 'URL: http://example.com/foo.txt' in %q", got)
	}
	if !strings.Contains(got, "Type: text/plain") {
		t.Fatalf("expected 'Type: text/plain' in %q", got)
	}
}

func TestBuildAttachmentText_MultipleAttachments(t *testing.T) {
	attachments := []Attachment{
		{Name: "a.txt", URL: "http://a", MimeType: "text/plain"},
		{Name: "b.png", URL: "http://b", MimeType: "image/png", Data: []byte{0x89, 0x50}},
	}
	got := BuildAttachmentText(attachments)

	if !strings.Contains(got, "- a.txt") {
		t.Fatalf("expected '- a.txt' in %q", got)
	}
	if !strings.Contains(got, "- b.png") {
		t.Fatalf("expected '- b.png' in %q", got)
	}
	if !strings.Contains(got, "Location: uploads/a.txt") {
		t.Fatalf("expected uploads location for text attachment in %q", got)
	}
	// 图片已内联（Data 非空）为视觉内容，不应再提示按路径读取
	if strings.Contains(got, "Location: uploads/b.png") {
		t.Fatalf("image attachment should not carry a read location hint, got %q", got)
	}
	if !strings.Contains(got, "do NOT call the read tool on them") {
		t.Fatalf("expected inline-visual hint in %q", got)
	}
}

func TestBuildAttachmentText_ImageDoesNotPromptRead(t *testing.T) {
	attachments := []Attachment{
		{Name: "screen.png", URL: "http://a", MimeType: "image/png", Data: []byte{0x89, 0x50}},
	}
	got := BuildAttachmentText(attachments)

	if !strings.Contains(got, "visual content is already provided") {
		t.Fatalf("expected inline-visual wording for image in %q", got)
	}
	if strings.Contains(got, "read them on disk") {
		t.Fatalf("image should not be routed to on-disk read instructions, got %q", got)
	}
	if strings.Contains(got, "Location:") {
		t.Fatalf("image should not carry a read location hint, got %q", got)
	}
}

// TestBuildAttachmentText_ImageWithoutDataRoutedToRead 验证内联失败（Data 为空）
// 的图片降级为按工作区路径 read，避免模型在无像素无路径的情况下凭空脑补。
func TestBuildAttachmentText_ImageWithoutDataRoutedToRead(t *testing.T) {
	attachments := []Attachment{
		{Name: "avatar.jpeg", URL: "http://unreachable/avatar.jpeg", MimeType: "image/jpeg"},
	}
	got := BuildAttachmentText(attachments)

	if strings.Contains(got, "visual content is already provided") {
		t.Fatalf("failed image should not be treated as inline visual, got %q", got)
	}
	if !strings.Contains(got, "read them on disk") {
		t.Fatalf("expected on-disk read instructions for failed image, got %q", got)
	}
	if !strings.Contains(got, "Location: uploads/avatar.jpeg") {
		t.Fatalf("expected uploads location for failed image in %q", got)
	}
}

// TestBuildAttachmentText_NonImageMultimodalRoutedToRead 验证在能力收窄（仅 image 内联
// 为视觉）后，PDF/音视频等其它多模态附件不再提示"已内联"，而按路径读取。
func TestBuildAttachmentText_NonImageMultimodalRoutedToRead(t *testing.T) {
	attachments := []Attachment{
		{Name: "clip.mp4", URL: "http://v", MimeType: "video/mp4"},
		{Name: "doc.pdf", URL: "http://p", MimeType: "application/pdf"},
		{Name: "audio.mp3", URL: "http://a3", MimeType: "audio/mpeg"},
	}
	got := BuildAttachmentText(attachments)

	if strings.Contains(got, "visual content is already provided") {
		t.Fatalf("non-image multimodal should not be treated as inline visual, got %q", got)
	}
	if !strings.Contains(got, "read them on disk") {
		t.Fatalf("expected on-disk read instructions for non-image multimodal, got %q", got)
	}
	for _, wanted := range []string{
		"Location: uploads/clip.mp4",
		"Location: uploads/doc.pdf",
		"Location: uploads/audio.mp3",
	} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("expected %q in %q", wanted, got)
		}
	}
}

func TestBuildAttachmentText_EmptyAttachment(t *testing.T) {
	got := BuildAttachmentText(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil, got %q", got)
	}

	got = BuildAttachmentText([]Attachment{})
	if got != "" {
		t.Fatalf("expected empty string for empty slice, got %q", got)
	}
}

func TestBuildAttachmentText_NoURL(t *testing.T) {
	attachments := []Attachment{
		{Name: "foo.txt", MimeType: "text/plain"},
	}
	got := BuildAttachmentText(attachments)

	if !strings.Contains(got, "- foo.txt") {
		t.Fatalf("expected '- foo.txt' in %q", got)
	}
	if !strings.Contains(got, "Type: text/plain") {
		t.Fatalf("expected 'Type: text/plain' in %q", got)
	}
	if strings.Contains(got, "URL:") {
		t.Fatalf("expected no URL line when URL is empty, got %q", got)
	}
}

func TestBuildAttachmentText_NoMimeType(t *testing.T) {
	attachments := []Attachment{
		{Name: "foo.txt", URL: "http://example.com"},
	}
	got := BuildAttachmentText(attachments)

	if !strings.Contains(got, "URL: http://example.com") {
		t.Fatalf("expected 'URL: http://example.com' in %q", got)
	}
	if strings.Contains(got, "Type:") {
		t.Fatalf("expected no Type line when MimeType is empty, got %q", got)
	}
}

func TestBuildAttachmentText_AttachmentRoleLabels(t *testing.T) {
	attachments := []Attachment{
		{Name: "main.pdf", MimeType: "application/pdf", AttachmentRole: "main"},
		{Name: "cmp.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", AttachmentRole: "compare"},
	}
	got := BuildAttachmentText(attachments)
	if !strings.Contains(got, "- main.pdf [main]") {
		t.Fatalf("expected main attachment role label in %q", got)
	}
	if !strings.Contains(got, "- cmp.docx [compare]") {
		t.Fatalf("expected compare attachment role label in %q", got)
	}
	if !strings.Contains(got, "Attachment role: main") {
		t.Fatalf("expected attachment role field for main in %q", got)
	}
	if !strings.Contains(got, "Attachment role: compare") {
		t.Fatalf("expected attachment role field for compare in %q", got)
	}
}
