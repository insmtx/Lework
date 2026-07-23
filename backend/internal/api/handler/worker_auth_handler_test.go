//go:build !enterprise

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account/oss"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

func TestWorkerAuthHandlerIssueToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 3, WorkerID: 7, Token: "bootstrap-token"},
		},
		TokenTTLSeconds: 3600,
	}
	parser := oss.NewTokenParser(nil, "jwt-secret", cfg)
	RegisterWorkerAuthRoutes(router, parser)

	body, err := json.Marshal(map[string]uint{
		"org_id":    3,
		"worker_id": 7,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/workers/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerWorkerBootstrapToken, "bootstrap-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AuthToken string `json:"auth_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.AuthToken == "" {
		t.Fatal("auth_token should not be empty")
	}

	claims, err := localauth.ParseWorkerToken(resp.Data.AuthToken, "jwt-secret")
	if err != nil {
		t.Fatalf("parse worker token: %v", err)
	}
	if claims.OrgID != 3 || claims.WorkerID != 7 {
		t.Fatalf("claims identity = org %d worker %d, want org 3 worker 7", claims.OrgID, claims.WorkerID)
	}
}

func TestWorkerAuthHandlerRejectsWrongBootstrapToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cfg := &config.WorkerAuthConfig{
		BootstrapTokens: []config.WorkerBootstrapToken{
			{OrgID: 3, WorkerID: 7, Token: "bootstrap-token"},
		},
	}
	parser := oss.NewTokenParser(nil, "jwt-secret", cfg)
	RegisterWorkerAuthRoutes(router, parser)

	body := []byte(`{"org_id":3,"worker_id":7}`)
	req := httptest.NewRequest(http.MethodPost, "/workers/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerWorkerBootstrapToken, "wrong-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestWorkerAuthHandlerIssueTokenFromDeploymentHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.WorkerDeployment{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	assistant := &types.DigitalAssistant{
		PublicID: "agent-a",
		OrgID:    3,
		Name:     "Agent A",
		Status:   "active",
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	bootstrapToken := "dynamic-bootstrap-token"
	if err := database.Create(&types.WorkerDeployment{
		OrgID:              3,
		DigitalAssistantID: assistant.ID,
		WorkerID:           9,
		DeploymentName:     "leros-worker-o3-w9",
		Status:             string(types.WorkerDeploymentStatusProvisioning),
		BootstrapTokenHash: localauth.HashBootstrapToken(bootstrapToken),
	}).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	router := gin.New()
	parser := oss.NewTokenParser(database, "jwt-secret", nil)
	RegisterWorkerAuthRoutes(router, parser)

	body := []byte(`{"org_id":3,"worker_id":9}`)
	req := httptest.NewRequest(http.MethodPost, "/workers/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerWorkerBootstrapToken, bootstrapToken)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
