package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/internal/api/middleware"
	"github.com/insmtx/Lework/backend/internal/service"
	"github.com/insmtx/Lework/backend/types"
)

type payrollCredentialStub struct {
	request service.ManagedCredentialRequest
	err     error
}

func (s *payrollCredentialStub) Resolve(_ context.Context, handler string, request service.ManagedCredentialRequest) (map[string]string, error) {
	if handler != payrollCredentialHandler {
		return nil, io.ErrUnexpectedEOF
	}
	s.request = request
	if s.err != nil {
		return nil, s.err
	}
	return map[string]string{"api_key": "secret-payroll-key"}, nil
}

func payrollProxyRouter(endpoint string, creds *payrollCredentialStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(ctx *gin.Context) {
		caller := &types.Caller{Uin: 7, OrgID: 9, State: types.AuthStateSucc}
		localauth.WithGinContext(ctx, caller, &types.Trace{}, "session-token")
		ctx.Request = ctx.Request.WithContext(localauth.WithBearerToken(ctx.Request.Context(), "session-token"))
		ctx.Next()
	})
	authed := r.Group("/v1", middleware.RequireCallerOrg())
	RegisterPayrollProxyRoutes(authed, NewPayrollProxyHandler(endpoint, creds))
	return r
}

func TestPayrollProxy_AuthenticatedJSONAndFixedActionMapping(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"success","response":{"total":0,"items":[]}}`)
	}))
	defer upstream.Close()
	creds := &payrollCredentialStub{}
	router := payrollProxyRouter(upstream.URL, creds)
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageRoster", strings.NewReader(`{"request":{"offset":0,"limit":10}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotPath != "/v1/payroll.PageRoster" || gotAuth != "Bearer secret-payroll-key" {
		t.Fatalf("status=%d path=%q auth=%q body=%s", rec.Code, gotPath, gotAuth, rec.Body.String())
	}
	if gotBody != `{"request":{"offset":0,"limit":10}}` {
		t.Fatalf("upstream body = %q", gotBody)
	}
	if creds.request.Purpose != payrollCredentialPurpose || creds.request.ConnectorCode != "payroll" {
		t.Fatalf("credential request = %#v", creds.request)
	}
	if strings.Contains(rec.Body.String(), "secret-payroll-key") {
		t.Fatal("credential leaked in response")
	}
}

func TestPayrollProxy_RejectsAnonymousAndUnknownActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/v1", middleware.RequireCallerOrg())
	RegisterPayrollProxyRoutes(authed, NewPayrollProxyHandler("http://127.0.0.1:8082", &payrollCredentialStub{}))
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageRoster", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", rec.Code)
	}

	authRouter := payrollProxyRouter("http://127.0.0.1:8082", &payrollCredentialStub{})
	unknown := httptest.NewRecorder()
	authRouter.ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/Anything", strings.NewReader(`{}`)))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown action status = %d", unknown.Code)
	}
}

func TestPayrollProxy_UpstreamAuthAndServerErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "secret-payroll-key sensitive-payload")
			}))
			defer upstream.Close()
			router := payrollProxyRouter(upstream.URL, &payrollCredentialStub{})
			req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageHisSalary", strings.NewReader(`{"request":{}}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "secret-payroll-key") || strings.Contains(rec.Body.String(), "sensitive-payload") {
				t.Fatalf("unsafe response status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPayrollProxy_PreservesUpstreamBusinessErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":40001,"message":"invalid payroll query"}`)
	}))
	defer upstream.Close()
	router := payrollProxyRouter(upstream.URL, &payrollCredentialStub{})
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageRoster", strings.NewReader(`{"request":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid payroll query") {
		t.Fatalf("business error not preserved: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPayrollProxy_CredentialFailureDoesNotCallUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	creds := &payrollCredentialStub{err: io.ErrUnexpectedEOF}
	router := payrollProxyRouter(upstream.URL, creds)
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageRoster", strings.NewReader(`{"request":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || called || strings.Contains(rec.Body.String(), "secret-payroll-key") {
		t.Fatalf("credential failure leaked or reached upstream: status=%d called=%v body=%s", rec.Code, called, rec.Body.String())
	}
}

func TestPayrollProxy_MultipartXLSXPreserved(t *testing.T) {
	var gotType, gotPath string
	var gotFile []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType, gotPath = r.Header.Get("Content-Type"), r.URL.Path
		reader, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart reader: %v", err)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Errorf("multipart part: %v", err)
			return
		}
		gotFile, _ = io.ReadAll(part)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task_id":1,"status":"success"}`)
	}))
	defer upstream.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "roster.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("xlsx-bytes"))
	_ = writer.Close()
	router := payrollProxyRouter(upstream.URL, &payrollCredentialStub{})
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/ImportRoster", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotPath != "/v1/payroll.ImportRoster" || !strings.HasPrefix(gotType, "multipart/form-data;") || string(gotFile) != "xlsx-bytes" {
		t.Fatalf("status=%d path=%s type=%s file=%q response=%s", rec.Code, gotPath, gotType, gotFile, rec.Body.String())
	}
}

func TestPayrollProxy_AllAllowedActionsHaveFixedPaths(t *testing.T) {
	var action string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action = strings.TrimPrefix(r.URL.Path, "/v1/payroll.")
		_, _ = io.WriteString(w, `{"code":0,"response":{}}`)
	}))
	defer upstream.Close()
	router := payrollProxyRouter(upstream.URL, &payrollCredentialStub{})
	for actionName := range payrollProxyActions {
		req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/"+actionName, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || action != actionName {
			t.Fatalf("action %s mapped to %s with status %d", actionName, action, rec.Code)
		}
	}
}

func TestPayrollProxy_ResponseRemainsValidJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"response":{"total":0,"items":[]}}`)
	}))
	defer upstream.Close()
	router := payrollProxyRouter(upstream.URL, &payrollCredentialStub{})
	req := httptest.NewRequest(http.MethodPost, "/v1/PayrollProxy/PageHisSalary", strings.NewReader(`{"request":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result["response"] == nil {
		t.Fatalf("response contract not preserved: %s", rec.Body.String())
	}
}
