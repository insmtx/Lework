//go:build enterprise

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account/enterprise"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
)

type mockIAMServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	users  []storedUser
	nextID int
}

type storedUser struct {
	ID    int
	Name  string
	Email string
	Phone string
}

func newMockIAMServer() *mockIAMServer {
	m := &mockIAMServer{nextID: 100}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handler))
	return m
}

func (m *mockIAMServer) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cmd     string          `json:"cmd"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.write(w, -1, nil)
		return
	}

	switch req.Cmd {
	case "account.VerifyToken":
		m.write(w, 0, map[string]any{
			"uin":        1,
			"user_id":    1,
			"company_id": 1,
		})
	case "account.CreateDepartmentEmployee":
		m.handleCreateEmployee(w, req.Request)
	case "account.DetailPersonalCenter":
		m.handleDetailPersonalCenter(w)
	case "account.UpdateUserInfo":
		m.handleUpdateUser(w, req.Request)
	case "account.DeleteUser":
		m.handleDeleteUser(w, req.Request)
	case "account.GetDepartmentTree":
		m.handleDepartmentTree(w, req.Request)
	default:
		m.write(w, -1, nil)
	}
}

func (m *mockIAMServer) write(w http.ResponseWriter, code int, body any) {
	type envelope struct {
		Code     int             `json:"code"`
		Message  string          `json:"message"`
		Response json.RawMessage `json:"Response"`
	}
	data, _ := json.Marshal(body)
	var respBytes json.RawMessage
	if body != nil {
		respBytes = data
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envelope{Code: code, Response: respBytes})
}

func (m *mockIAMServer) handleCreateEmployee(w http.ResponseWriter, raw json.RawMessage) {
	var req struct {
		UserName string `json:"user_name"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
	}
	json.Unmarshal(raw, &req)

	m.mu.Lock()
	defer m.mu.Unlock()

	if req.Name == "" {
		m.write(w, -1, nil)
		return
	}

	for _, u := range m.users {
		if req.Phone != "" && u.Phone == req.Phone {
			m.write(w, -1, map[string]string{"error": "phone already exists"})
			return
		}
	}

	id := m.nextID
	m.nextID++
	m.users = append(m.users, storedUser{ID: id, Name: req.Name, Email: req.Email, Phone: req.Phone})

	m.write(w, 0, map[string]any{
		"employee": map[string]any{
			"uin":            id,
			"user_name":      req.UserName,
			"name":           req.Name,
			"email":          req.Email,
			"phone":          req.Phone,
			"employee_id":    id,
			"user_id":        id,
			"role":           "member",
			"department_ids": []int{},
		},
	})
}

func (m *mockIAMServer) handleDetailPersonalCenter(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) == 0 {
		m.write(w, -1, map[string]string{"error": "user not found"})
		return
	}
	u := m.users[len(m.users)-1]
	m.write(w, 0, map[string]any{
		"user_info": map[string]any{
			"name":       u.Name,
			"email":      u.Email,
			"phone":      u.Phone,
			"id":         u.ID,
			"uin":        u.ID,
			"identify":   "",
			"avatar_url": "",
			"bio":        "",
		},
		"company_info": map[string]any{
			"id":   1,
			"name": "TestOrg",
		},
		"employee_detail": map[string]any{
			"company_id": 1,
			"user_id":    u.ID,
			"uin":        u.ID,
			"user_name":  u.Name,
			"real_name":  u.Name,
			"phone":      u.Phone,
			"email":      u.Email,
		},
	})
}

func (m *mockIAMServer) handleUpdateUser(w http.ResponseWriter, raw json.RawMessage) {
	var req struct {
		Name      string  `json:"name"`
		Email     *string `json:"email"`
		AvatarURL string  `json:"avatar_url"`
	}
	_ = json.Unmarshal(raw, &req)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) == 0 {
		m.write(w, -1, map[string]string{"error": "user not found"})
		return
	}
	last := m.users[len(m.users)-1]
	last.Name = req.Name
	m.users[len(m.users)-1] = last
	m.write(w, 0, nil)
}

func (m *mockIAMServer) handleDeleteUser(w http.ResponseWriter, raw json.RawMessage) {
	var req struct {
		UserID uint `json:"user_id"`
	}
	_ = json.Unmarshal(raw, &req)

	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.ID == int(req.UserID) {
			m.users = append(m.users[:i], m.users[i+1:]...)
			m.write(w, 0, nil)
			return
		}
	}
	m.write(w, -1, map[string]string{"error": "user not found"})
}

func (m *mockIAMServer) handleDepartmentTree(w http.ResponseWriter, raw json.RawMessage) {
	var req struct {
		Keyword string `json:"keyword"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
	}
	_ = json.Unmarshal(raw, &req)

	m.mu.Lock()
	defer m.mu.Unlock()

	var filtered []storedUser
	for _, u := range m.users {
		if req.Keyword != "" && !strings.Contains(u.Name, req.Keyword) && !strings.Contains(u.Phone, req.Keyword) {
			continue
		}
		filtered = append(filtered, u)
	}

	total := int64(len(filtered))
	offset := req.Offset
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	start := offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[start:end]

	emps := make([]map[string]any, 0, len(page))
	for _, u := range page {
		emps = append(emps, map[string]any{
			"uin":            u.ID,
			"user_name":      u.Name,
			"name":           u.Name,
			"email":          u.Email,
			"phone":          u.Phone,
			"employee_id":    u.ID,
			"user_id":        u.ID,
			"role":           "member",
			"department_ids": []int{},
		})
	}

	m.write(w, 0, map[string]any{
		"employees": emps,
		"total":     total,
		"offset":    offset,
		"limit":     len(page),
	})
}

func (m *mockIAMServer) close() {
	m.srv.Close()
}

func setupEnterpriseUserHandler(t *testing.T, mock *mockIAMServer) *gin.Engine {
	t.Helper()

	iamCfg := &config.IAMConfig{BaseURL: mock.srv.URL, DomainName: "test"}
	t.Setenv("LEROS_DEV", "true")
	gin.SetMode(gin.TestMode)
	r := gin.New()

	tokenParser := enterprise.NewTokenParser(nil, iamCfg, "test", "worker-secret", nil)
	r.Use(middleware.CallerMiddleware(tokenParser, nil))

	authed := r.Group("/", middleware.RequireCallerOrg())

	iamClient := enterprise.NewIAMClient(iamCfg, "test")
	userSvc := enterprise.NewUser(iamClient, nil)
	RegisterUserRoutes(authed, userSvc)

	return r
}

func TestEnterpriseCreateUser_Success(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

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

func TestEnterpriseCreateUser_MissingName(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/CreateUser", `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseCreateUser_DuplicatePhone(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w1 := doRequest(t, router, "/CreateUser", `{"name":"李四","phone":"13800138000"}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("first CreateUser expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	w2 := doRequest(t, router, "/CreateUser", `{"name":"王五","phone":"13800138000"}`)
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("second CreateUser expected 500, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestEnterpriseGetUser_ByPublicID(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	createResult := createEnterpriseUser(t, router, "张三")

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, createResult.UserID))
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
}

func TestEnterpriseGetUser_NotFound(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/GetUser", `{"public_id":"99999"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseGetUser_MissingParams(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/GetUser", `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseUpdateUser_Success(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	createResult := createEnterpriseUser(t, router, "张三")

	uw := doRequest(t, router, "/UpdateUser", fmt.Sprintf(`{"public_id":"%d","name":"张三丰"}`, createResult.UserID))
	if uw.Code != http.StatusOK {
		t.Fatalf("UpdateUser expected 200, got %d: %s", uw.Code, uw.Body.String())
	}
	uresp := parseResponse(t, uw)
	if uresp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, uresp.Code)
	}

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, createResult.UserID))
	if gw.Code != http.StatusOK {
		t.Fatalf("GetUser after update expected 200, got %d: %s", gw.Code, gw.Body.String())
	}
	gresp := parseResponse(t, gw)
	user := parseData[userInfo](t, gresp)
	if user.Name != "张三丰" {
		t.Fatalf("expected name 张三丰, got %s", user.Name)
	}
}

func TestEnterpriseUpdateUser_MissingPublicID(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/UpdateUser", `{"name":"张三丰"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseUpdateUser_NotFound(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/UpdateUser", `{"public_id":"99999","name":"张三丰"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseDeleteUser_Success(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	createResult := createEnterpriseUser(t, router, "张三")

	dw := doRequest(t, router, "/DeleteUser", fmt.Sprintf(`{"public_id":"%d"}`, createResult.UserID))
	if dw.Code != http.StatusOK {
		t.Fatalf("DeleteUser expected 200, got %d: %s", dw.Code, dw.Body.String())
	}
	dresp := parseResponse(t, dw)
	if dresp.Code != dto.CodeSuccess {
		t.Fatalf("expected code %d, got %d", dto.CodeSuccess, dresp.Code)
	}

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, createResult.UserID))
	if gw.Code != http.StatusInternalServerError {
		t.Fatalf("GetUser after delete expected 500, got %d: %s", gw.Code, gw.Body.String())
	}
}

func TestEnterpriseDeleteUser_NotFound(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/DeleteUser", `{"public_id":"99999"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseDeleteUser_MissingPublicID(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	w := doRequest(t, router, "/DeleteUser", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseListUser_All(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	createEnterpriseUser(t, router, "张三")
	createEnterpriseUser(t, router, "李四")

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

func TestEnterpriseListUser_ByKeyword(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	createEnterpriseUser(t, router, "张三")
	createEnterpriseUser(t, router, "李四")
	createEnterpriseUser(t, router, "王五")

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

func TestEnterpriseListUser_Pagination(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	for i := 0; i < 5; i++ {
		createEnterpriseUser(t, router, fmt.Sprintf("用户%d", i))
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

	_ = doRequest(t, router, "/ListUser", `{"offset":2,"limit":2}`)
}

func TestEnterpriseUserCRUDFlow(t *testing.T) {
	mock := newMockIAMServer()
	defer mock.close()
	router := setupEnterpriseUserHandler(t, mock)

	result := createEnterpriseUser(t, router, "端到端测试")

	gw := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, result.UserID))
	if gw.Code != http.StatusOK {
		t.Fatalf("get user expected 200, got %d: %s", gw.Code, gw.Body.String())
	}
	user := parseData[userInfo](t, parseResponse(t, gw))
	if user.Name != "端到端测试" {
		t.Fatalf("expected name 端到端测试, got %s", user.Name)
	}

	uw := doRequest(t, router, "/UpdateUser", fmt.Sprintf(`{"public_id":"%d","name":"端到端已更新"}`, result.UserID))
	if uw.Code != http.StatusOK {
		t.Fatalf("update user expected 200, got %d: %s", uw.Code, uw.Body.String())
	}

	gw2 := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, result.UserID))
	if gw2.Code != http.StatusOK {
		t.Fatalf("get user after update expected 200, got %d: %s", gw2.Code, gw2.Body.String())
	}
	user2 := parseData[userInfo](t, parseResponse(t, gw2))
	if user2.Name != "端到端已更新" {
		t.Fatalf("expected name 端到端已更新, got %s", user2.Name)
	}

	dw := doRequest(t, router, "/DeleteUser", fmt.Sprintf(`{"public_id":"%d"}`, result.UserID))
	if dw.Code != http.StatusOK {
		t.Fatalf("delete user expected 200, got %d: %s", dw.Code, dw.Body.String())
	}

	gw3 := doRequest(t, router, "/GetUser", fmt.Sprintf(`{"public_id":"%d"}`, result.UserID))
	if gw3.Code != http.StatusInternalServerError {
		t.Fatalf("get user after delete expected 500, got %d: %s", gw3.Code, gw3.Body.String())
	}
}

func createEnterpriseUser(t *testing.T, router *gin.Engine, name string) createUserResult {
	t.Helper()
	w := doRequest(t, router, "/CreateUser", fmt.Sprintf(`{"name":"%s"}`, name))
	if w.Code != http.StatusOK {
		t.Fatalf("CreateUser expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	if resp.Code != dto.CodeSuccess {
		t.Fatalf("CreateUser expected code %d, got %d", dto.CodeSuccess, resp.Code)
	}
	return parseData[createUserResult](t, resp)
}
