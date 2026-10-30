package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/types"
)

type maintenanceStub struct{ state types.SparkMaintenanceState }

func (m maintenanceStub) Read() types.SparkMaintenanceState { return m.state }
func TestSparkMaintenanceOnlyBlocksNewWork(t *testing.T) {
	for _, state := range []types.SparkMaintenanceState{{Known: true}, {Known: true, Configured: true, Active: true}, {Configured: true}} {
		for _, path := range []string{"/v1/AddMessage", "/v1/CreateInitialMessage", "/v1/CreateTask", "/v1/CreateSession", "/v1/RunAutomationNow", "/v1/GetSession", "/v1/CancelRun", "/v1/SubmitInteraction", "/v1/ops/spark/status"} {
			r := gin.New()
			r.Use(SparkMaintenance(maintenanceStub{state}))
			r.POST(path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			want := http.StatusNoContent
			switch path {
			case "/v1/AddMessage", "/v1/CreateInitialMessage", "/v1/CreateTask", "/v1/CreateSession", "/v1/RunAutomationNow":
				if state.Active || !state.Known {
					want = http.StatusServiceUnavailable
				}
			}
			if w.Code != want {
				t.Errorf("%s state=%+v got=%d want=%d", path, state, w.Code, want)
			}
		}
	}
}
