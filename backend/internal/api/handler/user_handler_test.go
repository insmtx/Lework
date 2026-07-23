//go:build !enterprise

package handler

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account/oss"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
	"github.com/insmtx/Leros/backend/types"
)

func setupUserTest(t *testing.T) (*gin.Engine, *gorm.DB) {
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

	t.Setenv("LEROS_DEV", "true")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tokenParser := oss.NewTokenParser(database, "test-secret", nil)
	r.Use(middleware.CallerMiddleware(tokenParser, database))

	authed := r.Group("/", middleware.RequireCallerOrg())
	userSvc := oss.NewUser(database)
	RegisterUserRoutes(authed, userSvc)
	return r, database
}

var testPhoneCounter atomic.Uint64

func createTestUser(t *testing.T, router *gin.Engine, _ *gorm.DB, name string) createUserResult {
	t.Helper()
	phone := fmt.Sprintf("1380013%04d", testPhoneCounter.Add(1))
	w := doRequest(t, router, "/CreateUser", fmt.Sprintf(`{"name":"%s","phone":"%s"}`, name, phone))
	if w.Code != http.StatusOK {
		t.Fatalf("CreateUser expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("CreateUser expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}
	return parseData[createUserResult](t, resp)
}

func TestCreateUser_Success(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/CreateUser", `{"name":"张三"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseResponse(t, w)
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}

	result := parseData[createUserResult](t, resp)
	if result.Name != "张三" {
		t.Fatalf("expected name 张三, got %s", result.Name)
	}
	if !result.IsNew {
		t.Fatal("expected IsNew=true")
	}
}

func TestCreateUser_MissingName(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/CreateUser", `{}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_DuplicatePhone(t *testing.T) {
	router, _ := setupUserTest(t)
	w1 := doRequest(t, router, "/CreateUser", `{"name":"李四","phone":"13800138000"}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("first CreateUser expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	w2 := doRequest(t, router, "/CreateUser", `{"name":"王五","phone":"13800138000"}`)
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("second CreateUser expected 500, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestGetUser_ByPublicID(t *testing.T) {
	router, db := setupUserTest(t)
	createTestUser(t, router, db, "张三")

	lw := doRequest(t, router, "/ListUser", `{}`)
	lresp := parseResponse(t, lw)
	lresult := parseData[userListResult](t, lresp)

	if len(lresult.Items) == 0 {
		t.Fatal("expected at least one user in list")
	}
	publicID := lresult.Items[0].PublicID
	if !strings.HasPrefix(publicID, "usr_") {
		t.Fatalf("expected public_id prefix usr_, got %s", publicID)
	}

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw.Code != http.StatusOK {
		t.Fatalf("GetUser expected 200, got %d: %s", gw.Code, gw.Body.String())
	}
	gresp := parseResponse(t, gw)
	if gresp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, gresp.Code)
	}
	user := parseData[userInfo](t, gresp)
	if user.Name != "张三" {
		t.Fatalf("expected name 张三, got %s", user.Name)
	}
	if user.PublicID != publicID {
		t.Fatalf("expected public_id %s, got %s", publicID, user.PublicID)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/GetUser", `{"public_id":"usr_nonexistent"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetUser_MissingParams(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/GetUser", `{}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUser_Success(t *testing.T) {
	router, db := setupUserTest(t)
	createTestUser(t, router, db, "张三")

	lw := doRequest(t, router, "/ListUser", `{}`)
	lresp := parseResponse(t, lw)
	lresult := parseData[userListResult](t, lresp)
	if len(lresult.Items) == 0 {
		t.Fatal("expected at least one user in list")
	}
	publicID := lresult.Items[0].PublicID

	uw := doRequest(t, router, "/UpdateUser", fmt.Sprintf(`{"public_id":"%s","name":"张三丰"}`, publicID))
	if uw.Code != http.StatusOK {
		t.Fatalf("UpdateUser expected 200, got %d: %s", uw.Code, uw.Body.String())
	}
	uresp := parseResponse(t, uw)
	if uresp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, uresp.Code)
	}

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw.Code != http.StatusOK {
		t.Fatalf("GetUser after update expected 200, got %d: %s", gw.Code, gw.Body.String())
	}
	gresp := parseResponse(t, gw)
	user := parseData[userInfo](t, gresp)
	if user.Name != "张三丰" {
		t.Fatalf("expected name 张三丰, got %s", user.Name)
	}
}

func TestUpdateUser_MissingPublicID(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/UpdateUser", `{"name":"张三丰"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/UpdateUser", `{"public_id":"usr_nonexistent","name":"张三丰"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser_Success(t *testing.T) {
	router, db := setupUserTest(t)
	createTestUser(t, router, db, "张三")

	lw := doRequest(t, router, "/ListUser", `{}`)
	lresp := parseResponse(t, lw)
	lresult := parseData[userListResult](t, lresp)
	if len(lresult.Items) == 0 {
		t.Fatal("expected at least one user in list")
	}
	publicID := lresult.Items[0].PublicID

	dw := doRequest(t, router, "/DeleteUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if dw.Code != http.StatusOK {
		t.Fatalf("DeleteUser expected 200, got %d: %s", dw.Code, dw.Body.String())
	}
	dresp := parseResponse(t, dw)
	if dresp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, dresp.Code)
	}

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw.Code != http.StatusInternalServerError {
		t.Fatalf("GetUser after delete expected 500, got %d: %s", gw.Code, gw.Body.String())
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/DeleteUser", `{"public_id":"usr_nonexistent"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser_MissingPublicID(t *testing.T) {
	router, _ := setupUserTest(t)
	w := doRequest(t, router, "/DeleteUser", `{}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListUser_All(t *testing.T) {
	router, db := setupUserTest(t)
	createTestUser(t, router, db, "张三")
	createTestUser(t, router, db, "李四")

	w := doRequest(t, router, "/ListUser", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ListUser expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}
	result := parseData[userListResult](t, resp)
	if result.Total < 2 {
		t.Fatalf("expected total >= 2, got %d", result.Total)
	}
	if len(result.Items) < 2 {
		t.Fatalf("expected at least 2 items, got %d", len(result.Items))
	}
}

func TestListUser_ByKeyword(t *testing.T) {
	router, db := setupUserTest(t)
	createTestUser(t, router, db, "张三")
	createTestUser(t, router, db, "李四")
	createTestUser(t, router, db, "王五")

	w := doRequest(t, router, "/ListUser", `{"keyword":"张"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ListUser by keyword expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	result := parseData[userListResult](t, resp)
	for _, item := range result.Items {
		if !strings.Contains(item.Name, "张") {
			t.Fatalf("expected all items contain '张', got %s", item.Name)
		}
	}
}

func TestListUser_Pagination(t *testing.T) {
	router, db := setupUserTest(t)
	for i := 0; i < 5; i++ {
		createTestUser(t, router, db, fmt.Sprintf("用户%d", i))
	}

	w := doRequest(t, router, "/ListUser", `{"offset":0,"limit":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ListUser page 1 expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	result := parseData[userListResult](t, resp)
	if result.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", result.Offset)
	}
	if result.Limit != 2 {
		t.Fatalf("expected limit 2, got %d", result.Limit)
	}
	if len(result.Items) > 2 {
		t.Fatalf("expected at most 2 items, got %d", len(result.Items))
	}

	_ = doRequest(t, router, "/ListUser", `{"offset":2,"limit":2}`) // 分页第二页，不报错即可
}

func TestUserCRUDFlow(t *testing.T) {
	router, db := setupUserTest(t)

	createTestUser(t, router, db, "端到端测试")

	lw := doRequest(t, router, "/ListUser", `{}`)
	lresp := parseResponse(t, lw)
	lresult := parseData[userListResult](t, lresp)
	if len(lresult.Items) == 0 {
		t.Fatal("expected at least one user")
	}
	publicID := lresult.Items[0].PublicID

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw.Code != http.StatusOK {
		t.Fatalf("get user expected 200, got %d: %s", gw.Code, gw.Body.String())
	}
	user := parseData[userInfo](t, parseResponse(t, gw))
	if user.Name != "端到端测试" {
		t.Fatalf("expected name 端到端测试, got %s", user.Name)
	}

	uw := doRequest(t, router, "/UpdateUser", fmt.Sprintf(`{"public_id":"%s","name":"端到端已更新"}`, publicID))
	if uw.Code != http.StatusOK {
		t.Fatalf("update user expected 200, got %d: %s", uw.Code, uw.Body.String())
	}

	gw2 := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw2.Code != http.StatusOK {
		t.Fatalf("get user after update expected 200, got %d: %s", gw2.Code, gw2.Body.String())
	}
	user2 := parseData[userInfo](t, parseResponse(t, gw2))
	if user2.Name != "端到端已更新" {
		t.Fatalf("expected name 端到端已更新, got %s", user2.Name)
	}

	dw := doRequest(t, router, "/DeleteUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if dw.Code != http.StatusOK {
		t.Fatalf("delete user expected 200, got %d: %s", dw.Code, dw.Body.String())
	}

	gw3 := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%s"}`, publicID))
	if gw3.Code != http.StatusInternalServerError {
		t.Fatalf("get user after delete expected 500, got %d: %s", gw3.Code, gw3.Body.String())
	}
}
