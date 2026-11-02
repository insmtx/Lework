package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/spark"
	"github.com/insmtx/Leros/backend/types"
)

type sparkAdminFake struct {
	allowed bool
	err     error
	calls   int
}

func (f *sparkAdminFake) IsOrgCreator(ctx context.Context, orgID, uin uint) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

type sparkReaderFake struct {
	calls int
	org   uint
	err   error
}

func (f *sparkReaderFake) OrgID() uint { return f.org }
func (f *sparkReaderFake) Status(context.Context) (types.SparkStatus, error) {
	f.calls++
	return types.SparkStatus{}, f.err
}
func (f *sparkReaderFake) Preflight(context.Context) (types.SparkPreflight, error) {
	f.calls++
	return types.SparkPreflight{Ready: false}, f.err
}
func TestSparkAuthorization(t *testing.T) {
	user := types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}
	for _, tc := range []struct {
		name     string
		caller   *types.Caller
		admin    bool
		adminErr error
		want     int
	}{
		{"anonymous", nil, false, nil, 401},
		{"member", &user, false, nil, 403},
		{"admin", &user, true, nil, 200},
		{"iam failure", &user, true, errors.New("secret"), 503},
		{"other org", &types.Caller{Uin: 11, OrgID: 8, Kind: types.CallerKindUser, State: types.AuthStateSucc}, true, nil, 403},
		{"worker", &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindWorker, State: types.AuthStateSucc}, true, nil, 403},
		{"failed auth", &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateFailed}, true, nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) { auth.WithGinContext(c, tc.caller, nil, "") })
			reader := &sparkReaderFake{org: 7}
			admin := &sparkAdminFake{allowed: tc.admin, err: tc.adminErr}
			RegisterSparkRoutes(r.Group("/v1"), reader, admin)
			for _, action := range []struct{ method, path, body string }{{"GET", "status", ""}, {"POST", "preflight", "{}"}} {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(action.method, "/v1/ops/spark/"+action.path, strings.NewReader(action.body)))
				if w.Code != tc.want {
					t.Fatalf("%s got %d: %s", action.path, w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "secret") {
					t.Fatal("leaked error")
				}
			}
			if tc.want != 200 && reader.calls != 0 {
				t.Fatal("unauthorized inventory access")
			}
		})
	}
}
func TestSparkPreflightRejectsCallerTargetsAndMutations(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		auth.WithGinContext(c, &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}, nil, "")
	})
	reader := &sparkReaderFake{org: 7}
	RegisterSparkRoutes(r.Group("/v1"), reader, &sparkAdminFake{allowed: true})
	for _, body := range []string{`{"host_id":"other"}`, `{} {}`, `null`, strings.Repeat("x", 2048)} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/v1/ops/spark/preflight", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("got %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/v1/ops/spark/shutdown", nil))
	if w.Code != 404 {
		t.Fatalf("mutation registered: %d", w.Code)
	}
	if reader.calls != 0 {
		t.Fatal("invalid request reached service")
	}
}
func TestSparkErrorsAreSanitized(t *testing.T) {
	for _, err := range []error{spark.ErrNotConfigured, spark.ErrUnavailable, spark.ErrBadResponse, errors.New("/private/socket token secret")} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			auth.WithGinContext(c, &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}, nil, "")
		})
		RegisterSparkRoutes(r.Group("/v1"), &sparkReaderFake{org: 7, err: err}, &sparkAdminFake{allowed: true})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/ops/spark/status", nil))
		if w.Code < 500 || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}

func (f *sparkReaderFake) ChangeMaintenance(context.Context, types.SparkMaintenanceRequest) (types.SparkMaintenanceReceipt, error) {
	f.calls++
	return types.SparkMaintenanceReceipt{}, f.err
}
func TestMaintenanceMutationAuthorizationAndStrictBody(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			auth.WithGinContext(c, &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}, nil, "")
		})
		svc := &sparkReaderFake{org: 7}
		RegisterSparkRoutes(r.Group("/v1"), svc, &sparkAdminFake{allowed: allowed})
		for _, body := range []string{`null`, `{}`, `{"active":true,"host_id":"other"}`, `{"active":false,"boot_id":"boot","expected_revision":1,"operation_id":"op"} {}`} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/v1/ops/spark/maintenance", strings.NewReader(body)))
			want := 400
			if !allowed {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("got %d body=%s", w.Code, w.Body)
			}
		}
		if svc.calls != 0 {
			t.Fatal("invalid mutation reached service")
		}
	}
}

func (f *sparkReaderFake) ChangeModel(context.Context, types.SparkModelRequest) (types.SparkModelControl, error) {
	f.calls++
	return types.SparkModelControl{}, f.err
}
func TestModelMutationAuthorizationAndStrictBody(t *testing.T) {
	valid := `{"action":"start","boot_id":"boot","expected_revision":1,"maintenance_revision":2,"operation_id":"op"}`
	for _, allowed := range []bool{true, false} {
		for _, body := range []string{valid, `null`, `{}`, strings.Replace(valid, `"start"`, `"shutdown"`, 1), strings.Replace(valid, `"action"`, `"host_id":"other","action"`, 1), valid + ` {}`, strings.Repeat(" ", 2049) + valid} {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				auth.WithGinContext(c, &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}, nil, "")
			})
			svc := &sparkReaderFake{org: 7}
			RegisterSparkRoutes(r.Group("/v1"), svc, &sparkAdminFake{allowed: allowed})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/ops/spark/model", strings.NewReader(body)))
			want := 400
			if body == valid {
				want = 200
			}
			if !allowed {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("got %d want %d: %s", w.Code, want, w.Body)
			}
			if want != 200 && svc.calls != 0 {
				t.Fatal("invalid or unauthorized model mutation reached service")
			}
		}
	}
}

type sparkWorkflowFake struct {
	sparkReaderFake
	actor   uint
	request types.SparkWorkflowRequest
}

func (f *sparkWorkflowFake) BeginWorkflow(_ context.Context, actor uint, r types.SparkWorkflowRequest) (types.SparkWorkflow, error) {
	f.actor = actor
	f.request = r
	return types.SparkWorkflow{OperationID: r.OperationID, Action: r.Action}, f.err
}
func (f *sparkWorkflowFake) WorkflowCurrent(context.Context) (types.SparkWorkflowControl, error) {
	return types.SparkWorkflowControl{Enabled: true}, f.err
}
func (f *sparkWorkflowFake) WorkflowGet(_ context.Context, id string) (types.SparkWorkflow, error) {
	return types.SparkWorkflow{OperationID: id}, f.err
}
func TestSparkWorkflowHTTPContracts(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprint(admin), func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				auth.WithGinContext(c, &types.Caller{Uin: 11, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}, nil, "")
			})
			f := &sparkWorkflowFake{sparkReaderFake: sparkReaderFake{org: 7}}
			RegisterSparkRoutes(r.Group("/v1"), f, &sparkAdminFake{allowed: admin})
			for _, tc := range []struct {
				method, path, body string
				want               int
			}{{"GET", "workflows/current", "", 200}, {"GET", "workflows/op", "", 200}, {"POST", "workflows", `{"operation_id":"op","action":"start","boot_id":"boot","expected_model_revision":1,"expected_maintenance_revision":1}`, 202}, {"POST", "workflows", `{"operation_id":"op","action":"shutdown","boot_id":"boot","expected_model_revision":1,"expected_maintenance_revision":1}`, 400}, {"POST", "workflows", `{"operation_id":"op","action":"start","boot_id":"boot","expected_model_revision":1,"expected_maintenance_revision":1,"actor":99}`, 400}} {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(tc.method, "/v1/ops/spark/"+tc.path, strings.NewReader(tc.body)))
				want := tc.want
				if !admin {
					want = 403
				}
				if w.Code != want {
					t.Fatalf("%s %d %s", tc.path, w.Code, w.Body.String())
				}
			}
			if admin && f.actor != 11 {
				t.Fatal("actor did not come from auth")
			}
			f.err = errors.New("raw secret database error")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/v1/ops/spark/workflows/current", nil))
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("raw DB error leaked")
			}
		})
	}
}
