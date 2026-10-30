package handler

import (
	"context"
	"errors"
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
