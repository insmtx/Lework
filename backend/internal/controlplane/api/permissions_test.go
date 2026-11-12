package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
)

func TestPendingPermissionsEndpoint(t *testing.T) {
	env := newTestEnv(t, time.Second)
	nodeRec, _, _ := env.addNode("dev-box")
	data, _ := json.Marshal(map[string]string{"id": "perm_1", "action": "bash"})
	env.index.Apply(event.Event{NodeID: nodeRec.ID, SessionID: "ses_1", Type: "permission.asked", Data: data})

	rec := env.do(http.MethodGet, "/api/sessions/"+nodeRec.ID+"/ses_1/permissions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Permissions []overview.Permission `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Permissions) != 1 || body.Permissions[0].ID != "perm_1" || body.Permissions[0].Action != "bash" {
		t.Fatalf("permissions = %+v", body.Permissions)
	}
}

func TestPendingPermissionsEndpointIsEmptyArray(t *testing.T) {
	env := newTestEnv(t, time.Second)

	rec := env.do(http.MethodGet, "/api/sessions/node_x/ses_x/permissions", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"permissions":[]`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
