package opencode

import (
	"encoding/base64"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

// TestBuildMessagePartsEmbedsAllDataBackedAttachments 验证所有携带内联数据的
// 多模态附件都注入为 file part，且 Filename 由 uploadRelDir 与 Name 拼接（带 uploads/ 前缀）。
func TestBuildMessagePartsEmbedsAllDataBackedAttachments(t *testing.T) {
	parts := buildMessageParts("hello", "uploads", []agent.Attachment{
		{MIME: "image/png", Name: "cat.png", Data: []byte{0x89, 0x50, 0x4E, 0x47}},
		{MIME: "application/pdf", Name: "doc.pdf", Data: []byte("pdf")},
		{MIME: "image/jpeg", Name: "", Data: []byte{0xFF, 0xD8}},
		{MIME: "image/png", Name: "empty.png", Data: nil},
	})

	if len(parts) != 4 {
		t.Fatalf("parts = %#v, want [text, png, pdf, jpeg]; empty-data attachment skipped", parts)
	}
	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Fatalf("first part = %#v, want text part", parts[0])
	}

	wantData := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
	if parts[1].Type != "file" || parts[1].MIME != "image/png" || parts[1].URL != wantData || parts[1].Filename != "uploads/cat.png" {
		t.Fatalf("png part = %#v, want file image/png data URL with uploads path", parts[1])
	}

	if parts[2].Type != "file" || parts[2].MIME != "application/pdf" || parts[2].Filename != "uploads/doc.pdf" {
		t.Fatalf("pdf part = %#v, want file application/pdf with uploads-prefixed Name", parts[2])
	}

	// Name 为空：无法拼接 uploads 前缀，Filename 应为空。
	if parts[3].Type != "file" || parts[3].MIME != "image/jpeg" || parts[3].Filename != "" {
		t.Fatalf("jpeg part = %#v, want file part with empty filename when Name is empty", parts[3])
	}
}

func TestBuildMessagePartsPrefersUploadRelDirWhenNameHasSubdir(t *testing.T) {
	parts := buildMessageParts("hi", "uploads", []agent.Attachment{
		{MIME: "audio/mpeg", Name: "mp3/music.mp3", Data: []byte("xxx")},
		{MIME: "video/mp4", Name: "clip.mp4", Data: []byte("yyy")},
	})
	if len(parts) != 3 {
		t.Fatalf("parts = %#v, want [text, mp3, mp4]", parts)
	}
	if parts[1].Filename != "uploads/mp3/music.mp3" {
		t.Fatalf("mp3 filename = %q, want uploads/mp3/music.mp3 (subdir in Name preserved)", parts[1].Filename)
	}
	if parts[2].Filename != "uploads/clip.mp4" {
		t.Fatalf("mp4 filename = %q, want uploads/clip.mp4", parts[2].Filename)
	}
}

func TestBuildMessagePartsFallsBackToNameWithoutUploadRelDir(t *testing.T) {
	parts := buildMessageParts("hi", "", []agent.Attachment{
		{MIME: "audio/mpeg", Name: "music.mp3", Data: []byte("xxx")},
		{MIME: "video/mp4", Name: "clip.mp4", Data: []byte("yyy")},
	})
	if len(parts) != 3 {
		t.Fatalf("parts = %#v, want [text, mp3, mp4]", parts)
	}
	if parts[1].Filename != "music.mp3" {
		t.Fatalf("mp3 filename = %q, want music.mp3 (Name only when uploadRelDir empty)", parts[1].Filename)
	}
	if parts[2].Filename != "clip.mp4" {
		t.Fatalf("mp4 filename = %q, want clip.mp4", parts[2].Filename)
	}
}

// TestBuildMessagePartsSkipsEmptyData 验证无内联数据的附件（大文件、空数据）
// 不生成 file part —— 其内容由上层通过 prompt 提示 location 让 runtime 读取。
func TestBuildMessagePartsSkipsEmptyData(t *testing.T) {
	parts := buildMessageParts("hi", "uploads", []agent.Attachment{
		{MIME: "video/mp4", Name: "big.mp4", Data: nil},
		{MIME: "image/png", Name: "no-data.png", Data: nil},
	})
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("parts = %#v, want text only", parts)
	}
}
