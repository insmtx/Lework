package externalcli

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupSessionStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&types.Session{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return db
}

func TestSessionMetadataProviderSessionStoreUpsertAndGet(t *testing.T) {
	db := setupSessionStoreTestDB(t)
	ctx := context.Background()
	session := &types.Session{
		PublicID: "sess_external_cli",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
		Metadata: types.SessionMetadata{
			Extra: map[string]interface{}{
				"source": "test",
			},
		},
	}
	if err := db.WithContext(ctx).Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	store := NewSessionMetadataProviderSessionStore(db)
	if err := store.Upsert(ctx, &ProviderSessionBinding{
		InternalSessionID: "sess_external_cli",
		Provider:          "claude",
		ProviderSessionID: "provider-session-1",
		WorkDir:           "/tmp/ignored",
		AssistantID:       "assistant-ignored",
	}); err != nil {
		t.Fatalf("upsert provider session: %v", err)
	}

	binding, err := store.Get(ctx, ProviderSessionKey{
		InternalSessionID: "sess_external_cli",
		Provider:          "claude",
		WorkDir:           "/different/workdir",
		AssistantID:       "different-assistant",
	})
	if err != nil {
		t.Fatalf("get provider session: %v", err)
	}
	if binding == nil {
		t.Fatal("expected provider session binding")
	}
	if binding.ProviderSessionID != "provider-session-1" {
		t.Fatalf("expected provider session id, got %q", binding.ProviderSessionID)
	}

	var persisted types.Session
	if err := db.WithContext(ctx).Where("public_id = ?", "sess_external_cli").First(&persisted).Error; err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	if persisted.Metadata.Extra["source"] != "test" {
		t.Fatalf("expected existing metadata to be preserved, got %#v", persisted.Metadata.Extra["source"])
	}
	sessions, err := providerSessionsFromMetadata(persisted.Metadata)
	if err != nil {
		t.Fatalf("decode persisted provider sessions: %v", err)
	}
	metadata, ok := sessions["claude"]
	if !ok {
		t.Fatal("expected claude provider session metadata")
	}
	if metadata.Provider != "claude" || metadata.ProviderSessionID != "provider-session-1" {
		t.Fatalf("unexpected provider metadata: %#v", metadata)
	}
	if metadata.CreatedAt.IsZero() {
		t.Fatal("expected provider metadata created_at to be set")
	}
	createdAt := metadata.CreatedAt

	if err := store.Upsert(ctx, &ProviderSessionBinding{
		InternalSessionID: "sess_external_cli",
		Provider:          "claude",
		ProviderSessionID: "provider-session-1-updated",
	}); err != nil {
		t.Fatalf("update provider session: %v", err)
	}
	var updated types.Session
	if err := db.WithContext(ctx).Where("public_id = ?", "sess_external_cli").First(&updated).Error; err != nil {
		t.Fatalf("load updated session: %v", err)
	}
	updatedSessions, err := providerSessionsFromMetadata(updated.Metadata)
	if err != nil {
		t.Fatalf("decode updated provider sessions: %v", err)
	}
	updatedMetadata := updatedSessions["claude"]
	if updatedMetadata.ProviderSessionID != "provider-session-1-updated" {
		t.Fatalf("expected updated provider session id, got %q", updatedMetadata.ProviderSessionID)
	}
	if !updatedMetadata.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at to be preserved, got %s want %s", updatedMetadata.CreatedAt, createdAt)
	}
}

func TestSessionMetadataProviderSessionStoreMarkFailedPreservesBinding(t *testing.T) {
	db := setupSessionStoreTestDB(t)
	ctx := context.Background()
	session := &types.Session{
		PublicID: "sess_failed_preserve",
		Type:      string(types.SessionTypeUserChat),
		Status:    string(types.SessionStatusActive),
	}
	if err := db.WithContext(ctx).Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	store := NewSessionMetadataProviderSessionStore(db)
	if err := store.Upsert(ctx, &ProviderSessionBinding{
		InternalSessionID: "sess_failed_preserve",
		Provider:          "claude",
		ProviderSessionID: "provider-session-2",
	}); err != nil {
		t.Fatalf("upsert provider session: %v", err)
	}
	if err := store.MarkFailed(ctx, ProviderSessionKey{
		InternalSessionID: "sess_failed_preserve",
		Provider:          "claude",
	}, "temporary failure"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	binding, err := store.Get(ctx, ProviderSessionKey{
		InternalSessionID: "sess_failed_preserve",
		Provider:          "claude",
	})
	if err != nil {
		t.Fatalf("get provider session: %v", err)
	}
	if binding == nil || binding.ProviderSessionID != "provider-session-2" {
		t.Fatalf("expected binding to be preserved, got %#v", binding)
	}
}
