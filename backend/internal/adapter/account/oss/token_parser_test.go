//go:build !enterprise

package oss

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/config"
	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/types"
)

func setupWorkerTokenTestDatabase(t *testing.T, orgID, workerID uint) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.Create(&types.DigitalAssistant{
		Model:    gorm.Model{ID: workerID},
		PublicID: "agent-config",
		OrgID:    orgID,
		Name:     "Config Agent",
		Status:   string(types.DigitalAssistantStatusActive),
	}).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	return database
}

func TestBuiltinTokenParserIssueWorkerFromConfig(t *testing.T) {
	database := setupWorkerTokenTestDatabase(t, 3, 7)
	cfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 3, WorkerID: 7, Token: "bootstrap-token"},
		},
		TokenTTLSeconds: 3600,
	}
	p := NewTokenParser(database, "jwt-secret", cfg)
	token, _, err := p.IssueWorker(context.Background(), 3, 7, "bootstrap-token")
	if err != nil {
		t.Fatalf("IssueWorker failed: %v", err)
	}
	claims, err := localauth.ParseWorkerToken(token, "jwt-secret")
	if err != nil {
		t.Fatalf("parse worker token: %v", err)
	}
	if claims.OrgID != 3 || claims.WorkerID != 7 {
		t.Fatalf("claims = org %d worker %d, want 3/7", claims.OrgID, claims.WorkerID)
	}
}

func TestBuiltinTokenParserRejectsWrongBootstrapToken(t *testing.T) {
	database := setupWorkerTokenTestDatabase(t, 3, 7)
	cfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 3, WorkerID: 7, Token: "bootstrap-token"},
		},
	}
	p := NewTokenParser(database, "jwt-secret", cfg)
	_, _, err := p.IssueWorker(context.Background(), 3, 7, "wrong-token")
	if err == nil {
		t.Fatal("expected error for wrong bootstrap token")
	}
}

func TestBuiltinTokenParserIssueWorkerFromDeploymentHash(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assistant := &types.DigitalAssistant{PublicID: "agent-a", OrgID: 3, Name: "Agent A", Status: "active"}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	bootstrapToken := "dynamic-bootstrap-token"
	if err := database.Create(&types.WorkerDeployment{
		OrgID:              3,
		DigitalAssistantID: assistant.ID,
		WorkerID:           9,
		DeploymentName:     "lework-worker-o3-w9",
		Status:             string(types.WorkerDeploymentStatusProvisioning),
		BootstrapTokenHash: localauth.HashBootstrapToken(bootstrapToken),
	}).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	p := NewTokenParser(database, "jwt-secret", nil)
	token, _, err := p.IssueWorker(context.Background(), 3, 9, bootstrapToken)
	if err != nil {
		t.Fatalf("IssueWorker failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}
