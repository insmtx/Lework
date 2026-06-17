package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	dbpkg "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func setupTestWorkService(t *testing.T) (*workService, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	inferrer := &mockInferrer{assistantID: 1}
	service := NewWorkService(db, &mockEventBus{}, inferrer)
	typed, ok := service.(*workService)
	if !ok {
		t.Fatalf("expected *workService, got %T", service)
	}
	return typed, db
}

func TestWorkServiceNewMessage_PersistsAttachmentsOnFirstMessage(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	// 预先落一条 file_upload，模拟前端已完成项目文件上传。
	fileUpload := &types.FileUpload{
		PublicID:     "fu_test_attachment",
		OrgID:        1,
		OwnerID:      1,
		Filename:     "spec.pdf",
		OriginalName: "spec.pdf",
		MimeType:     "application/pdf",
		FileSize:     1024,
		StoragePath:  "project-files/spec.pdf",
		Purpose:      "project_file",
		Status:       "active",
	}
	if err := dbpkg.CreateFileUpload(context.Background(), database, fileUpload); err != nil {
		t.Fatalf("CreateFileUpload failed: %v", err)
	}

	req := &contract.NewMessageRequest{
		Content: "请基于附件开始分析",
		Attachments: []types.MessageAttachment{
			{
				FileUploadID: fileUpload.PublicID,
				Name:         "spec.pdf",
				MimeType:     "application/pdf",
				Size:         1024,
			},
		},
	}

	resp, err := service.NewMessage(ctx, req)
	if err != nil {
		t.Fatalf("NewMessage failed: %v", err)
	}

	var session types.Session
	if err := database.WithContext(context.Background()).
		Where("public_id = ?", resp.SessionID).
		First(&session).Error; err != nil {
		t.Fatalf("load session failed: %v", err)
	}

	var message types.SessionMessage
	if err := database.WithContext(context.Background()).
		Where("session_id = ? AND sequence = ?", session.ID, 1).
		First(&message).Error; err != nil {
		t.Fatalf("load first message failed: %v", err)
	}

	if len(message.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(message.Attachments))
	}

	attachment := message.Attachments[0]
	if attachment.FileUploadID != fileUpload.PublicID {
		t.Fatalf("expected file upload id %q, got %q", fileUpload.PublicID, attachment.FileUploadID)
	}
	if attachment.Name != "spec.pdf" {
		t.Fatalf("expected attachment name spec.pdf, got %q", attachment.Name)
	}

	refreshedUpload, err := dbpkg.GetFileUploadByPublicID(context.Background(), database, 1, fileUpload.PublicID)
	if err != nil {
		t.Fatalf("reload file upload failed: %v", err)
	}
	if refreshedUpload == nil {
		t.Fatal("expected file upload to exist after new message")
	}

	projectPublicID, _ := refreshedUpload.Metadata.Extra["project_public_id"].(string)
	if projectPublicID != resp.ProjectID {
		t.Fatalf("expected file upload project_public_id %q, got %q", resp.ProjectID, projectPublicID)
	}
}
