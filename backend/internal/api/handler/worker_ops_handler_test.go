package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/nats-io/nats.go"
)

// setupWorkerOpsRouter 构造带 caller 中间件的 gin router，便于注入 caller 身份。
func setupWorkerOpsRouter(t *testing.T, caller *types.Caller) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if caller != nil {
		router.Use(func(ctx *gin.Context) {
			auth.WithGinContext(ctx, caller, &types.Trace{RequestID: "req"}, "")
			ctx.Next()
		})
	}
	return router
}

// mqCoreRequesterFake 是 mq.CoreRequester 的测试替身。
type mqCoreRequesterFake struct {
	replyData []byte
	err       error
}

func (f mqCoreRequesterFake) RequestReply(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &nats.Msg{Data: f.replyData}, nil
}

func registerOps(router *gin.Engine, requester mqCoreRequesterFake) {
	svc := service.NewWorkerOpsService(requester, time.Second)
	RegisterWorkerOpsRoutes(router.Group("/v1"), svc)
}

func getWorkerStatus(router *gin.Engine, orgID, workerID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/ops/workers/status?orgid="+orgID+"&workerid="+workerID, nil)
	router.ServeHTTP(w, req)
	return w
}

func TestWorkerStatusSuccess(t *testing.T) {
	snapshot := messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: 1700000000, MaxConcurrency: 4, RunningCount: 1, WaitingCount: 2}
	data, _ := json.Marshal(snapshot)
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, mqCoreRequesterFake{replyData: data})

	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data messaging.WorkerStatusSnapshot `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.MaxConcurrency != 4 || resp.Data.RunningCount != 1 || resp.Data.WaitingCount != 2 {
		t.Fatalf("data = %+v", resp.Data)
	}
}

func TestWorkerStatusMissingParams(t *testing.T) {
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, mqCoreRequesterFake{})

	for _, tc := range []struct{ org, worker string }{
		{"", "9"},
		{"7", ""},
		{"0", "9"},
		{"7", "0"},
		{"abc", "9"},
		{"7", "9x"},
	} {
		w := getWorkerStatus(router, tc.org, tc.worker)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("orgid=%q workerid=%q => status %d, want 400", tc.org, tc.worker, w.Code)
		}
	}
}

func TestWorkerStatusUnauthorizedNoCaller(t *testing.T) {
	router := setupWorkerOpsRouter(t, nil)
	registerOps(router, mqCoreRequesterFake{})
	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWorkerStatusUnauthorizedZeroOrg(t *testing.T) {
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 0, State: types.AuthStateSucc})
	registerOps(router, mqCoreRequesterFake{})
	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWorkerStatusOrgMismatchForbidden(t *testing.T) {
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, mqCoreRequesterFake{})
	// 查询的是 org 99，而 caller 属于 org 7 → 403。
	w := getWorkerStatus(router, "99", "9")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestWorkerStatusTimeoutGatewayTimeout(t *testing.T) {
	requester := mqCoreRequesterFake{err: context.DeadlineExceeded}
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, requester)
	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", w.Code)
	}
}

func TestWorkerStatusUnavailableServiceUnavailable(t *testing.T) {
	requester := mqCoreRequesterFake{err: service.ErrWorkerUnavailable}
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, requester)
	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestWorkerStatusBadResponseBadGateway(t *testing.T) {
	requester := mqCoreRequesterFake{replyData: []byte("{not-json")}
	router := setupWorkerOpsRouter(t, &types.Caller{OrgID: 7, State: types.AuthStateSucc})
	registerOps(router, requester)
	w := getWorkerStatus(router, "7", "9")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}
