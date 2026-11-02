package spark

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestMaintenanceControlRequiresConsumerAgreementAndCAS(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mismatch  bool
		boot      string
		revision  uint64
		wantWrite bool
	}{
		{"confirmed", false, "boot-1", 1, true}, {"worker behind", true, "boot-1", 1, false},
		{"rebooted", false, "old-boot", 1, false}, {"stale revision", false, "boot-1", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state")
			state := maintenanceObservation{Version: 1, HostID: "host-1", BootID: "boot-1", Revision: 1, OperationID: "initialized", ObservedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
			raw, _ := json.Marshal(state)
			if err := os.WriteFile(statePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			writes := 0
			path := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/status" {
					json.NewEncoder(w).Encode(validSnapshot())
					return
				}
				if r.Method == "POST" {
					writes++
					state.Active = true
					state.Revision = 2
					state.OperationID = "test-op"
				}
				json.NewEncoder(w).Encode(state)
			})
			gate := &types.SparkMaintenanceState{Configured: true, Known: true, HostID: "host-1", Revision: 1, OperationID: "initialized"}
			if tc.mismatch {
				gate.Revision = 2
			}
			worker := workerFake{snapshot: &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: time.Now().Unix(), Maintenance: gate}}
			svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: path, WorkerIDs: []uint{9}, MaintenanceControl: true, Maintenance: &config.SparkMaintenanceConfig{StatePath: statePath, HostID: "host-1"}}, worker)
			status, err := svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.MaintenanceControl == nil || status.MaintenanceControl.Confirmed == tc.mismatch {
				t.Fatalf("status: %+v", status)
			}
			active := true
			_, err = svc.ChangeMaintenance(context.Background(), types.SparkMaintenanceRequest{BootID: tc.boot, ExpectedRevision: tc.revision, OperationID: "test-op", Active: &active})
			if tc.wantWrite {
				if err != nil || writes != 1 {
					t.Fatalf("writes=%d err=%v", writes, err)
				}
			} else if writes != 0 || !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestMaintenanceWriterNeverRetriesUnknownOutcome(t *testing.T) {
	calls := 0
	path := testAgent(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) })
	svc := NewService(config.SparkConfig{HostID: "host-1", SocketPath: path}, nil)
	active := true
	_, err := svc.maintenanceRequest(context.Background(), &types.SparkMaintenanceRequest{BootID: "boot-1", ExpectedRevision: 1, OperationID: "op", Active: &active})
	if !errors.Is(err, ErrUnavailable) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestMaintenanceObservationRejectsMissingActiveAndBadIdentity(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"host_id":"host-1","boot_id":"boot-1","revision":1,"operation_id":"op","updated_at":1,"observed_at":1}`,
		`{"version":1,"host_id":"other","boot_id":"boot-1","active":false,"revision":1,"operation_id":"op","updated_at":1,"observed_at":1}`,
	} {
		path := testAgent(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) })
		svc := NewService(config.SparkConfig{HostID: "host-1", SocketPath: path}, nil)
		if _, err := svc.maintenanceRequest(context.Background(), nil); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
}
