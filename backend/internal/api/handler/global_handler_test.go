package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter"
)

type globalEditionInterface = adapter.Edition
type globalTestEdition struct{ globalEditionInterface }

func (globalTestEdition) Edition() string     { return "oss" }
func (globalTestEdition) DeployMode() string  { return "private" }
func (globalTestEdition) MaxOrgsPerUser() int { return 1 }

func TestGlobalConfigSparkDeclaration(t *testing.T) {
	for _, tt := range []struct{ name, spark, device string }{
		{"ordinary", "{}", ""},
		{"explicit", "{device_id: spark-001, device_name: 工作站}", `{"type":"spark","protocol_version":1,"id":"spark-001","name":"工作站","management":false}`},
		{"existing binding", "{host_id: bound-001, org_id: 4, socket_path: /run/private.sock, worker_ids: [8]}", `{"type":"spark","protocol_version":1,"id":"bound-001","name":"Spark","management":true}`},
		{"incomplete binding", "{host_id: bound-001, org_id: 4}", ""},
		{"name alone", "{device_name: Spark}", ""},
		{"malformed identity", "{device_id: 'bad/id'}", ""},
		{"invalid workers", "{host_id: bound-001, org_id: 4, socket_path: /run/private.sock, worker_ids: [8, 8]}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cfg config.Config
			if err := yaml.Unmarshal([]byte("spark: "+tt.spark), &cfg); err != nil {
				t.Fatal(err)
			}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			NewGlobalHandler(globalTestEdition{}, &cfg).RegisterRoutes(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/GlobalConfig", nil))
			var payload struct {
				Data struct {
					Device json.RawMessage `json:"device"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload.Data.Device) != tt.device {
				t.Fatalf("device = %s, want %s", payload.Data.Device, tt.device)
			}
			for _, secret := range []string{"socket_path", "org_id", "worker_ids", "host_id", "/run/private.sock"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("leaked %s", secret)
				}
			}
		})
	}
}
