package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/handler"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCreateDigitalAssistantViaGin simulates the exact HTTP flow:
// gin middleware sets caller via WithGinContext, then handler calls service
func TestCreateDigitalAssistantViaGin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&types.DigitalAssistant{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Simulate the middleware setting the caller via WithGinContext
	router.Use(func(ctx *gin.Context) {
		caller := &types.Caller{
			Uin:   1,
			OrgID: 1,
			State: types.AuthStateSucc,
		}
		trace := &types.Trace{
			RequestID: "test-request-id",
			TraceID:   "test-trace-id",
		}
		auth.WithGinContext(ctx, caller, trace)
		ctx.Next()
	})

	svc := NewDigitalAssistantService(db, nil)
	handler.RegisterDigitalAssistantRoutes(router.Group("/v1"), svc)

	body, _ := json.Marshal(contract.CreateDigitalAssistantRequest{
		Code:         "code-from-gin",
		Name:         "Gin Test",
		Description:  "test",
		SystemPrompt: "you are a test",
	})

	req := httptest.NewRequest("POST", "/v1/CreateDigitalAssistant", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestCreateDigitalAssistantViaGin_NoCaller simulates the exact curl scenario:
// no Authorization header, so caller has OrgID=0
func TestCreateDigitalAssistantViaGin_NoCaller(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&types.DigitalAssistant{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Simulate middleware behavior when no Authorization header is present
	router.Use(func(ctx *gin.Context) {
		caller := &types.Caller{
			Uin:   0,
			OrgID: 0,
			State: types.AuthStateNil,
		}
		trace := &types.Trace{
			RequestID: "test-request-id",
			TraceID:   "test-trace-id",
		}
		auth.WithGinContext(ctx, caller, trace)
		ctx.Next()
	})

	svc := NewDigitalAssistantService(db, nil)
	handler.RegisterDigitalAssistantRoutes(router.Group("/v1"), svc)

	body, _ := json.Marshal(contract.CreateDigitalAssistantRequest{
		Code: "code-no-auth",
		Name: "No Auth Test",
	})

	req := httptest.NewRequest("POST", "/v1/CreateDigitalAssistant", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 401 Unauthorized
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d. Body: %s", w.Code, w.Body.String())
	}
}
