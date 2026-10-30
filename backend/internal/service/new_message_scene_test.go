package service

import (
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func TestNormalizeMessageScene(t *testing.T) {
	t.Parallel()

	scene, err := NormalizeMessageScene("")
	if err != nil || scene != "" {
		t.Fatalf("empty scene => %q, %v", scene, err)
	}
	scene, err = NormalizeMessageScene("normal")
	if err != nil || scene != "" {
		t.Fatalf("normal scene => %q, %v", scene, err)
	}
	scene, err = NormalizeMessageScene("bid_comparison")
	if err != nil || scene != string(types.MessageSceneBidComparison) {
		t.Fatalf("bid_comparison scene => %q, %v", scene, err)
	}
	_, err = NormalizeMessageScene("unknown")
	if !errors.Is(err, ErrInvalidNewMessage) {
		t.Fatalf("expected ErrInvalidNewMessage, got %v", err)
	}
}

func TestValidateNewMessageScene_BidComparison(t *testing.T) {
	t.Parallel()

	okReq := &contract.NewMessageRequest{
		Scene:        "bid_comparison",
		OutputFormat: "DOCX",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_main", Name: "main.pdf", AttachmentRole: "main"},
			{FileUploadID: "fu_cmp1", Name: "a.pdf", AttachmentRole: "compare"},
			{FileUploadID: "fu_extra", Name: "note.txt"},
		},
	}
	scene, err := ValidateNewMessageScene(okReq)
	if err != nil {
		t.Fatalf("valid bid_comparison: %v", err)
	}
	if scene != string(types.MessageSceneBidComparison) {
		t.Fatalf("scene = %q", scene)
	}
	if okReq.OutputFormat != string(types.OutputFormatDOCX) {
		t.Fatalf("output format = %q", okReq.OutputFormat)
	}

	_, err = ValidateNewMessageScene(&contract.NewMessageRequest{
		Scene:        "bid_comparison",
		OutputFormat: "pdf",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_cmp1", Name: "a.pdf", AttachmentRole: "compare"},
		},
	})
	if !errors.Is(err, ErrInvalidNewMessage) {
		t.Fatalf("missing main: got %v", err)
	}

	_, err = ValidateNewMessageScene(&contract.NewMessageRequest{
		Scene:        "bid_comparison",
		OutputFormat: "pptx",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_main", Name: "main.pdf", AttachmentRole: "main"},
		},
	})
	if !errors.Is(err, ErrInvalidNewMessage) {
		t.Fatalf("missing compare: got %v", err)
	}

	_, err = ValidateNewMessageScene(&contract.NewMessageRequest{
		Scene:        "bid_comparison",
		OutputFormat: "xlsx",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_main", Name: "main.pdf", AttachmentRole: "main"},
			{FileUploadID: "fu_cmp1", Name: "a.pdf", AttachmentRole: "compare"},
		},
	})
	if !errors.Is(err, ErrInvalidNewMessage) {
		t.Fatalf("invalid output format: got %v", err)
	}

	_, err = ValidateNewMessageScene(&contract.NewMessageRequest{
		Scene: "",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_1", Name: "a.pdf", AttachmentRole: "reference"},
		},
	})
	if err != nil {
		t.Fatalf("ordinary attachment role should remain extensible: %v", err)
	}
}

func TestValidateAddMessageScene_BidComparison(t *testing.T) {
	t.Parallel()

	okReq := &contract.AddMessageRequest{
		Role:         string(types.MessageRoleUser),
		Content:      "请进行标书对比",
		Scene:        "bid_comparison",
		OutputFormat: "PDF",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_main", Name: "main.pdf", AttachmentRole: "Main"},
			{FileUploadID: "fu_cmp1", Name: "a.pdf", AttachmentRole: "COMPARE"},
		},
	}
	scene, err := ValidateAddMessageScene(okReq)
	if err != nil {
		t.Fatalf("valid add message bid_comparison: %v", err)
	}
	if scene != string(types.MessageSceneBidComparison) {
		t.Fatalf("scene = %q", scene)
	}
	if okReq.OutputFormat != string(types.OutputFormatPDF) {
		t.Fatalf("output format = %q", okReq.OutputFormat)
	}
	if okReq.Attachments[0].AttachmentRole != string(types.AttachmentRoleMain) {
		t.Fatalf("main role not normalized: %q", okReq.Attachments[0].AttachmentRole)
	}

	_, err = ValidateAddMessageScene(&contract.AddMessageRequest{
		Role:         string(types.MessageRoleUser),
		Content:      "缺少对比文件",
		Scene:        "bid_comparison",
		OutputFormat: "docx",
		Attachments: []types.MessageAttachment{
			{FileUploadID: "fu_main", Name: "main.pdf", AttachmentRole: "main"},
		},
	})
	if !errors.Is(err, ErrInvalidNewMessage) {
		t.Fatalf("missing compare: got %v", err)
	}

	_, err = ValidateAddMessageScene(&contract.AddMessageRequest{
		Role:    string(types.MessageRoleUser),
		Content: "普通续聊",
	})
	if err != nil {
		t.Fatalf("ordinary add message should pass: %v", err)
	}
}
