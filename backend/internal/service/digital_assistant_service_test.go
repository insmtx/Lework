package service

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDigitalAssistantDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&types.DigitalAssistant{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return db
}

func setupDigitalAssistantProvisioningDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return db
}

func TestCreateDigitalAssistant_ValidInput(t *testing.T) {
	db := setupDigitalAssistantDB(t)
	ctx := setupTestContextWithCaller(t)

	service := NewDigitalAssistantService(db, nil)

	req := &contract.CreateDigitalAssistantRequest{
		Code:         "test-code",
		Name:         "Test Name",
		Description:  "Test Description",
		SystemPrompt: "You are a test assistant",
	}

	result, err := service.CreateDigitalAssistant(ctx, req)
	if err != nil {
		t.Fatalf("CreateDigitalAssistant failed: %v", err)
	}

	if result.Code != "test-code" {
		t.Errorf("expected code test-code, got %s", result.Code)
	}
	if result.Name != "Test Name" {
		t.Errorf("expected name 'Test Name', got %s", result.Name)
	}
}

func TestCreateDigitalAssistant_WithoutCaller(t *testing.T) {
	db := setupDigitalAssistantDB(t)
	ctx := setupTestContextWithoutCaller(t)

	service := NewDigitalAssistantService(db, nil)

	req := &contract.CreateDigitalAssistantRequest{
		Code: "test-code",
		Name: "Test Name",
	}

	_, err := service.CreateDigitalAssistant(ctx, req)
	if err == nil {
		t.Fatal("expected error when caller is not in context")
	}
	if err.Error() != "user not authenticated or org not set" {
		t.Errorf("expected 'user not authenticated or org not set', got %v", err)
	}
}

func TestCreateDigitalAssistant_GeneratesCodeWhenMissing(t *testing.T) {
	db := setupDigitalAssistantDB(t)
	ctx := setupTestContextWithCaller(t)

	service := NewDigitalAssistantService(db, nil)

	req := &contract.CreateDigitalAssistantRequest{
		Name: "Test Name",
	}

	result, err := service.CreateDigitalAssistant(ctx, req)
	if err != nil {
		t.Fatalf("CreateDigitalAssistant failed: %v", err)
	}
	if result.Code == "" {
		t.Fatal("expected generated code")
	}
}

func TestCreateDigitalAssistant_MissingName(t *testing.T) {
	db := setupDigitalAssistantDB(t)
	ctx := setupTestContextWithCaller(t)

	service := NewDigitalAssistantService(db, nil)

	req := &contract.CreateDigitalAssistantRequest{
		Code: "test-code",
	}

	_, err := service.CreateDigitalAssistant(ctx, req)
	if err == nil {
		t.Fatal("expected error when name is missing")
	}
}

func TestCreateDigitalAssistant_LimitsPerUserToFive(t *testing.T) {
	db := setupDigitalAssistantDB(t)
	ctx := setupTestContextWithCaller(t)

	service := NewDigitalAssistantService(db, nil)
	if err := db.Create(&types.DigitalAssistant{
		Code:    defaultWorkerCode(1),
		OrgID:   1,
		OwnerID: 1,
		Name:    "lework",
		Status:  string(contract.DigitalAssistantStatusActive),
	}).Error; err != nil {
		t.Fatalf("create default assistant: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := service.CreateDigitalAssistant(ctx, &contract.CreateDigitalAssistantRequest{
			Code: "limit-test-" + string(rune('a'+i)),
			Name: "Limit Test",
		})
		if err != nil {
			t.Fatalf("CreateDigitalAssistant #%d failed: %v", i+1, err)
		}
	}

	_, err := service.CreateDigitalAssistant(ctx, &contract.CreateDigitalAssistantRequest{
		Code: "limit-test-overflow",
		Name: "Overflow",
	})
	if err == nil {
		t.Fatal("expected limit error for sixth assistant")
	}
	if err.Error() != "digital assistant limit exceeded: max 5 per user" {
		t.Fatalf("error = %q, want limit exceeded", err.Error())
	}
}

func TestUpdateDigitalAssistantStatusActiveMarksDeploymentPending(t *testing.T) {
	db := setupDigitalAssistantProvisioningDB(t)
	ctx := setupTestContextWithCaller(t)

	provisioning := NewWorkerProvisioningService(db, nil)
	service := NewDigitalAssistantServiceWithProvisioning(db, nil, provisioning)

	result, err := service.CreateDigitalAssistant(ctx, &contract.CreateDigitalAssistantRequest{
		Code:         "deploy-pending",
		Name:         "Deploy Pending",
		Description:  "wait for scheduler health",
		SystemPrompt: "stay pending until worker is ready",
	})
	if err != nil {
		t.Fatalf("CreateDigitalAssistant failed: %v", err)
	}

	if err := service.UpdateDigitalAssistantStatus(ctx, result.ID, &contract.UpdateDigitalAssistantStatusRequest{
		Status: string(contract.DigitalAssistantStatusActive),
	}); err != nil {
		t.Fatalf("UpdateDigitalAssistantStatus failed: %v", err)
	}

	var deployment types.WorkerDeployment
	if err := db.Where("digital_assistant_id = ?", result.ID).First(&deployment).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if deployment.Status != string(types.WorkerDeploymentStatusPending) {
		t.Fatalf("deployment status = %q, want pending", deployment.Status)
	}
}
