//go:build !enterprise

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/adapter/account/oss"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/infra/sms"

	"github.com/insmtx/Leros/backend/types"
)

func setupIntegrationTest(t *testing.T) *gin.Engine {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.User{},
		&types.Organization{},
		&types.UserOrg{},
		&types.Department{},
		&types.MemberDepartment{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
		&types.AuthPhoneVerificationCode{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
		&types.LLMModel{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	if err := database.Create(&types.LLMModel{
		OrgID:           1,
		Code:            "default",
		Name:            "Default",
		Provider:        "openai",
		ModelName:       "gpt-test",
		BaseURL:         "https://api.openai.com",
		BaseURLHasV1:    true,
		APIKeyEncrypted: "sk-test",
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       true,
		IsSystem:        true,
	}).Error; err != nil {
		t.Fatalf("failed to seed default llm model: %v", err)
	}
	if err := database.Create(&types.Organization{
		PublicID: "org_default",
		Code:     "default_org",
		Name:     "默认组织",
		Type:     "company",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("failed to seed default org: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	authSvc := oss.NewAuth(database, "test-secret", sms.NoopSMSSender{}, nil)
	handler := NewAuthHandler(authSvc)
	handler.RegisterRoutes(r)
	return r
}

func TestSendPhoneLoginCode_Success(t *testing.T) {
	router := setupIntegrationTest(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(`{"phone":"13800138000"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}

	data, _ := json.Marshal(resp.Data)
	var result account.SendPhoneLoginCodeOutput
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if result.Phone != "13800138000" {
		t.Fatalf("expected phone 13800138000, got %s", result.Phone)
	}
	if result.ExpiresIn != 300 {
		t.Fatalf("expected expires_in 300, got %d", result.ExpiresIn)
	}
	if result.ResendAfter != 120 {
		t.Fatalf("expected resend_after 120, got %d", result.ResendAfter)
	}
}

func TestSendPhoneLoginCode_MissingPhone(t *testing.T) {
	router := setupIntegrationTest(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendPhoneLoginCode_TooFrequent(t *testing.T) {
	router := setupIntegrationTest(t)
	phone := "13800138000"
	body := fmt.Sprintf(`{"phone":"%s"}`, phone)

	// 第一次发送应该成功
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// 立即第二次发送，应在 2 分钟重发间隔内，触发 429
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestSendPhoneLoginCode_InvalidPhone(t *testing.T) {
	router := setupIntegrationTest(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(`{"phone":"123"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendPhoneLoginAndLoginByPhoneCode(t *testing.T) {
	router := setupIntegrationTest(t)
	phone := "13800138000"

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/SendPhoneLoginCode", strings.NewReader(fmt.Sprintf(`{"phone":"%s"}`, phone)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendPhoneLoginCode expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 使用固定验证码 123456（NoopSMSSender 模式下的默认验证码）
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/LoginByPhoneCode", strings.NewReader(fmt.Sprintf(`{"phone":"%s","code":"123456"}`, phone)))
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("LoginByPhoneCode expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp dto.Response
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}

	data, _ := json.Marshal(resp.Data)
	var tokenResult account.AuthTokens
	if err := json.Unmarshal(data, &tokenResult); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	if tokenResult.JwtToken == "" {
		t.Fatal("expected jwt_token, got empty")
	}
}
