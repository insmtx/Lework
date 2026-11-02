package spark

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestModelCapabilitiesRequireConfirmedMaintenanceAndIdleWorkers(t *testing.T) {
	for _, tc := range []struct {
		name, state      string
		active, mismatch bool
		busy             int
		start, stop      bool
	}{
		{"stopped", "stopped", true, false, 0, true, false},
		{"ready", "ready", true, false, 0, false, true},
		{"accepting", "stopped", false, false, 0, false, false},
		{"worker behind", "stopped", true, true, 0, false, false},
		{"coordinator busy", "ready", true, false, 1, false, false},
		{"unknown", "unknown", true, false, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Unix()
			gate := maintenanceObservation{Version: 1, HostID: "host-1", BootID: "boot-1", Active: tc.active, Revision: 1, OperationID: "initialized", UpdatedAt: now, ObservedAt: now}
			raw, _ := json.Marshal(gate)
			statePath := filepath.Join(t.TempDir(), "gate")
			if err := os.WriteFile(statePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			path := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/status":
					json.NewEncoder(w).Encode(validSnapshot())
				case "/maintenance":
					json.NewEncoder(w).Encode(gate)
				case "/model":
					json.NewEncoder(w).Encode(struct {
						ProtocolVersion int    `json:"protocol_version"`
						HostID          string `json:"host_id"`
						BootID          string `json:"boot_id"`
						ObservedAt      int64  `json:"observed_at"`
						ModelID         string `json:"model_id"`
						Label           string `json:"label"`
						State           string `json:"state"`
						Revision        int    `json:"revision"`
						OperationID     string `json:"operation_id"`
						Action          string `json:"action"`
						ErrorCode       string `json:"error_code"`
						Busy            bool   `json:"busy"`
					}{1, "host-1", "boot-1", now, "qwen", "Qwen", tc.state, 1, "initialized", "none", "", false})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			workerGate := &types.SparkMaintenanceState{Configured: true, Known: true, HostID: "host-1", Revision: 1, OperationID: "initialized", Active: tc.active}
			if tc.mismatch {
				workerGate.Revision = 2
			}
			worker := workerFake{snapshot: &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: now, Maintenance: workerGate, CoordinatorWaitingCount: tc.busy}}
			cfg := config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: path, WorkerIDs: []uint{9}, MaintenanceControl: true, Maintenance: &config.SparkMaintenanceConfig{StatePath: statePath, HostID: "host-1"}}
			if err := json.Unmarshal([]byte(`{"model_control":true}`), &cfg); err != nil {
				t.Fatal(err)
			}
			status, err := NewService(cfg, worker).Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.CanStartModel != tc.start || status.CanStopModel != tc.stop || status.CanShutdown {
				t.Fatalf("capabilities %+v", status)
			}
		})
	}
}

func modelFixture(t *testing.T, model types.SparkModelControl, post func(http.ResponseWriter, *http.Request)) *Service {
	t.Helper()
	now := time.Now().Unix()
	gate := maintenanceObservation{Version: 1, HostID: "host-1", BootID: "boot-1", Active: true, Revision: 3, OperationID: "pause-op", ObservedAt: now, UpdatedAt: now}
	raw, _ := json.Marshal(gate)
	statePath := filepath.Join(t.TempDir(), "gate")
	if err := os.WriteFile(statePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	path := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			post(w, r)
			return
		}
		switch r.URL.Path {
		case "/status":
			json.NewEncoder(w).Encode(validSnapshot())
		case "/maintenance":
			json.NewEncoder(w).Encode(gate)
		case "/model":
			raw, _ := json.Marshal(model)
			// 控制能力只存在于后端对外响应，agent 协议不带能力字段。
			var fields map[string]json.RawMessage
			json.Unmarshal(raw, &fields)
			delete(fields, "can_start")
			delete(fields, "can_stop")
			json.NewEncoder(w).Encode(fields)
		}
	})
	worker := workerFake{snapshot: &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: now, Maintenance: &types.SparkMaintenanceState{Configured: true, Known: true, HostID: "host-1", Active: true, Revision: 3, OperationID: "pause-op"}}}
	return NewService(config.SparkConfig{ModelControl: true, MaintenanceControl: true, OrgID: 7, HostID: "host-1", SocketPath: path, WorkerIDs: []uint{9}, Maintenance: &config.SparkMaintenanceConfig{HostID: "host-1", StatePath: statePath}}, worker)
}
func validModel() types.SparkModelControl {
	return types.SparkModelControl{ProtocolVersion: 1, HostID: "host-1", BootID: "boot-1", ObservedAt: time.Now().Unix(), ModelID: "qwen", Label: "Qwen", State: "stopped", Revision: 1, OperationID: "initialized", Action: "none"}
}
func TestModelChangeChecksFreshBothRevisionsAndNeverRetries(t *testing.T) {
	for _, tc := range []struct {
		name, boot            string
		revision, maintenance uint64
		state                 string
		outcome               int
		want                  int
	}{
		{"start", "boot-1", 1, 3, "stopped", 200, 1},
		{"old boot", "old", 1, 3, "stopped", 200, 0},
		{"old model revision", "boot-1", 2, 3, "stopped", 200, 0},
		{"old maintenance revision", "boot-1", 1, 2, "stopped", 200, 0},
		{"already ready", "boot-1", 1, 3, "ready", 200, 0},
		{"conflict", "boot-1", 1, 3, "stopped", 409, 1},
		{"unknown outcome", "boot-1", 1, 3, "stopped", 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := validModel()
			model.State = tc.state
			writes := 0
			svc := modelFixture(t, model, func(w http.ResponseWriter, r *http.Request) {
				writes++
				if r.URL.Path != "/model" {
					t.Errorf("unexpected mutation %s", r.URL.Path)
				}
				var payload struct {
					types.SparkModelRequest
					HostID string `json:"host_id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.HostID != "host-1" || payload.MaintenanceRevision != 3 || payload.ExpectedRevision != 1 || payload.Action != "start" || payload.OperationID != "start-op" || payload.BootID != "boot-1" {
					t.Errorf("payload %+v", payload)
				}
				if tc.outcome != 200 {
					w.WriteHeader(tc.outcome)
					return
				}
				model.State = "starting"
				model.Busy = true
				model.Revision = 2
				model.OperationID = "start-op"
				model.Action = "start"
				raw, _ := json.Marshal(model)
				var fields map[string]json.RawMessage
				json.Unmarshal(raw, &fields)
				delete(fields, "can_start")
				delete(fields, "can_stop")
				json.NewEncoder(w).Encode(fields)
			})
			result, err := svc.ChangeModel(context.Background(), types.SparkModelRequest{BootID: tc.boot, ExpectedRevision: tc.revision, MaintenanceRevision: tc.maintenance, OperationID: "start-op", Action: "start"})
			if writes != tc.want {
				t.Fatalf("writes=%d expected=%d", writes, tc.want)
			}
			if tc.want == 1 && tc.outcome == 200 {
				if err != nil || result.State != "starting" || result.CanStart || result.CanStop {
					t.Fatalf("result %+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("unsafe model mutation accepted")
			}
		})
	}
}
func TestMaintenanceResumeBlockedByBusyOrUnknownModel(t *testing.T) {
	for _, state := range []string{"starting", "draining", "stopping", "unknown", "stopped", "ready"} {
		t.Run(state, func(t *testing.T) {
			model := validModel()
			model.State = state
			model.Busy = state == "starting" || state == "draining" || state == "stopping"
			writes := 0
			svc := modelFixture(t, model, func(w http.ResponseWriter, r *http.Request) {
				writes++
				json.NewEncoder(w).Encode(maintenanceObservation{Version: 1, HostID: "host-1", BootID: "boot-1", Active: false, Revision: 4, OperationID: "pause-op", UpdatedAt: time.Now().Unix(), ObservedAt: time.Now().Unix()})
			})
			status, err := svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			blocked := model.Busy || state == "unknown"
			if status.MaintenanceControl.CanResume == blocked {
				t.Fatalf("resume=%v state=%s", status.MaintenanceControl.CanResume, state)
			}
			active := false
			_, err = svc.ChangeMaintenance(context.Background(), types.SparkMaintenanceRequest{BootID: "boot-1", ExpectedRevision: 3, OperationID: "pause-op", Active: &active})
			if blocked && (err == nil || writes != 0) {
				t.Fatalf("unsafe resume writes=%d err=%v", writes, err)
			}
			if !blocked && (err != nil || writes != 1) {
				t.Fatalf("safe resume writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestConfiguredModelDoesNotReportUnconfiguredBlocker(t *testing.T) {
	svc := modelFixture(t, validModel(), func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected write") })
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hasCheck(status.Checks, "model_unconfigured") || hasCheck(status.Checks, "execution_disabled") {
		t.Fatalf("configured model shows disabled diagnostics: %+v", status.Checks)
	}
}
func TestModelObservationRejectsMalformedAndUntrustedResponses(t *testing.T) {
	base, _ := json.Marshal(validModel())
	var baseFields map[string]json.RawMessage
	json.Unmarshal(base, &baseFields)
	delete(baseFields, "can_start")
	delete(baseFields, "can_stop")
	good, _ := json.Marshal(baseFields)
	for _, field := range []string{"protocol_version", "host_id", "boot_id", "observed_at", "model_id", "label", "state", "revision", "operation_id", "action", "error_code", "busy"} {
		t.Run("missing "+field, func(t *testing.T) {
			var fields map[string]json.RawMessage
			json.Unmarshal(good, &fields)
			delete(fields, field)
			raw, _ := json.Marshal(fields)
			path := testAgent(t, func(w http.ResponseWriter, r *http.Request) { w.Write(raw) })
			svc := NewService(config.SparkConfig{HostID: "host-1", SocketPath: path}, nil)
			if _, err := svc.modelRequest(context.Background(), nil); !errors.Is(err, ErrBadResponse) {
				t.Fatalf("missing %s accepted: %v", field, err)
			}
		})
	}
	for _, tc := range []struct{ name, field, value string }{
		{"wrong host", "host_id", `"other"`}, {"old observation", "observed_at", "1"},
		{"unsafe revision", "revision", "9007199254740992"}, {"zero revision", "revision", "0"},
		{"null busy", "busy", "null"}, {"string busy", "busy", `"false"`},
		{"unknown field", "can_start", "true"}, {"bad action", "action", `"shutdown"`},
		{"bad state", "state", `"running"`}, {"busy stopped", "busy", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			json.Unmarshal(good, &fields)
			fields[tc.field] = json.RawMessage(tc.value)
			raw, _ := json.Marshal(fields)
			path := testAgent(t, func(w http.ResponseWriter, r *http.Request) { w.Write(raw) })
			svc := NewService(config.SparkConfig{HostID: "host-1", SocketPath: path}, nil)
			if _, err := svc.modelRequest(context.Background(), nil); !errors.Is(err, ErrBadResponse) {
				t.Fatalf("bad response accepted: %v", err)
			}
		})
	}
	for _, raw := range []string{string(good) + " {}", strings.Repeat(" ", 16385) + string(good)} {
		path := testAgent(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) })
		svc := NewService(config.SparkConfig{HostID: "host-1", SocketPath: path}, nil)
		if _, err := svc.modelRequest(context.Background(), nil); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("bad body accepted: %v", err)
		}
	}
}
func TestModelBootMismatchAndUnavailablePreventResume(t *testing.T) {
	for _, kind := range []string{"boot", "host", "stale", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			model := validModel()
			switch kind {
			case "boot":
				model.BootID = "other"
			case "host":
				model.HostID = "other"
			case "stale":
				model.ObservedAt = 1
			case "unavailable":
				model.ModelID = ""
			}
			svc := modelFixture(t, model, func(http.ResponseWriter, *http.Request) { t.Error("unexpected write") })
			status, err := svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.ModelControl != nil || status.MaintenanceControl.CanResume || status.CanStartModel || status.CanStopModel {
				t.Fatalf("unverified model enabled action: %+v", status)
			}
		})
	}
}

func TestModelStopAndReceiptMustMatchRequestedIdentity(t *testing.T) {
	for _, field := range []string{"", "boot", "model", "revision", "operation", "action"} {
		t.Run(field, func(t *testing.T) {
			model := validModel()
			model.State = "ready"
			writes := 0
			svc := modelFixture(t, model, func(w http.ResponseWriter, r *http.Request) {
				writes++
				var payload types.SparkModelRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.Action != "stop" || payload.ExpectedRevision != 1 || payload.MaintenanceRevision != 3 {
					t.Errorf("stop payload %+v", payload)
				}
				result := model
				result.Revision = 2
				result.Action = "stop"
				result.OperationID = "stop-op"
				result.State = "draining"
				result.Busy = true
				switch field {
				case "boot":
					result.BootID = "other"
				case "model":
					result.ModelID = "other"
				case "revision":
					result.Revision = 3
				case "operation":
					result.OperationID = "other"
				case "action":
					result.Action = "start"
				}
				raw, _ := json.Marshal(result)
				var fields map[string]json.RawMessage
				json.Unmarshal(raw, &fields)
				delete(fields, "can_start")
				delete(fields, "can_stop")
				json.NewEncoder(w).Encode(fields)
			})
			result, err := svc.ChangeModel(context.Background(), types.SparkModelRequest{BootID: "boot-1", ExpectedRevision: 1, MaintenanceRevision: 3, OperationID: "stop-op", Action: "stop"})
			if writes != 1 {
				t.Fatalf("writes=%d", writes)
			}
			if field == "" {
				if err != nil || result.State != "draining" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if !errors.Is(err, ErrBadResponse) {
				t.Fatalf("mismatched receipt accepted: %+v %v", result, err)
			}
		})
	}
}
