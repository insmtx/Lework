package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestSessionMessageTaskExpiryProjectorFailsOnlyPendingSources(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	pending := &types.SessionMessage{SessionID: 1, Role: string(types.MessageRoleUser), Content: "pending", Status: string(types.MessageStatusPending), Sequence: 1}
	processing := &types.SessionMessage{SessionID: 1, Role: string(types.MessageRoleUser), Content: "processing", Status: string(types.MessageStatusProcessing), Sequence: 2}
	if err := database.Create(pending).Error; err != nil {
		t.Fatalf("create pending message: %v", err)
	}
	if err := database.Create(processing).Error; err != nil {
		t.Fatalf("create processing message: %v", err)
	}
	projector := NewSessionMessageTaskExpiryProjector(database)
	if err := projector.ProjectExpiredReliableTasks(ctx, []types.ReliableTask{
		{SourceType: reliableTaskSourceSessionMessage, SourceID: "1"},
		{SourceType: reliableTaskSourceSessionMessage, SourceID: "2"},
		{SourceType: "unrelated", SourceID: "1"},
	}); err != nil {
		t.Fatalf("project expired tasks: %v", err)
	}
	var gotPending, gotProcessing types.SessionMessage
	if err := database.First(&gotPending, pending.ID).Error; err != nil {
		t.Fatalf("reload pending message: %v", err)
	}
	if err := database.First(&gotProcessing, processing.ID).Error; err != nil {
		t.Fatalf("reload processing message: %v", err)
	}
	if gotPending.Status != string(types.MessageStatusFailed) || gotPending.ErrorMsg != "queue_start_timeout" {
		t.Fatalf("pending message = status %q error %q, want failed queue_start_timeout", gotPending.Status, gotPending.ErrorMsg)
	}
	if gotProcessing.Status != string(types.MessageStatusProcessing) {
		t.Fatalf("processing message status = %q, want processing", gotProcessing.Status)
	}
}
