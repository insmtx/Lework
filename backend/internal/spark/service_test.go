package spark

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/service"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/nats-io/nats.go"
)

func validSnapshot() types.SparkSnapshot {
	return types.SparkSnapshot{ProtocolVersion: 1, HostID: "host-1", BootID: "boot-1", Hostname: "spark", ObservedAt: time.Now().Unix(), Mode: "read_only", ModelState: "unconfigured", InventoryComplete: true, Components: []types.SparkComponent{}, Checks: []types.SparkCheck{}}
}
func testAgent(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "spark-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return path
}
func snapshotAgent(t *testing.T, s types.SparkSnapshot) string {
	return testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(s)
	})
}
func TestStatusValidatesAgentIdentityFreshnessAndProtocol(t *testing.T) {
	cases := []struct {
		name   string
		change func(*types.SparkSnapshot)
	}{
		{"host", func(s *types.SparkSnapshot) { s.HostID = "other" }},
		{"boot", func(s *types.SparkSnapshot) { s.BootID = "" }},
		{"old", func(s *types.SparkSnapshot) { s.ObservedAt -= 31 }},
		{"future", func(s *types.SparkSnapshot) { s.ObservedAt += 7 }},
		{"protocol", func(s *types.SparkSnapshot) { s.ProtocolVersion = 2 }},
		{"mode", func(s *types.SparkSnapshot) { s.Mode = "execute" }},
		{"missing inventory", func(s *types.SparkSnapshot) { s.Components = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSnapshot()
			tc.change(&s)
			svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, s)}, nil)
			if _, err := svc.Status(context.Background()); !errors.Is(err, ErrBadResponse) {
				t.Fatalf("got %v", err)
			}
		})
	}
	svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot())}, nil)
	result, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.CanShutdown || result.CanStartModel || result.CanStopModel {
		t.Fatal("read-only service enabled execution")
	}
	if !hasCheck(result.Checks, "worker_mapping_missing") || !hasCheck(result.Checks, "execution_disabled") {
		t.Fatalf("missing blockers: %+v", result.Checks)
	}
	preflight, err := svc.Preflight(context.Background())
	if err != nil || preflight.Ready {
		t.Fatalf("preflight = %+v %v", preflight, err)
	}
}
func hasCheck(checks []types.SparkCheck, code string) bool {
	for _, c := range checks {
		if c.Code == code {
			return true
		}
	}
	return false
}
func TestAgentRedirectOversizeAndMalformedRejected(t *testing.T) {
	for _, kind := range []string{"redirect", "large", "invalid", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			path := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "redirect":
					http.Redirect(w, r, "http://127.0.0.1:1", 302)
				case "large":
					w.Write([]byte(strings.Repeat("x", 1024*1024+1)))
				case "invalid":
					w.Write([]byte(`{"protocol_version":1}`))
				case "trailing":
					json.NewEncoder(w).Encode(validSnapshot())
					w.Write([]byte(`{}`))
				}
			})
			_, err := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: path}, nil).Status(context.Background())
			if !errors.Is(err, ErrBadResponse) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

type workerFake struct {
	snapshot *messaging.WorkerStatusSnapshot
	err      error
}

func (f workerFake) QueryWorkerStatusForPreflight(context.Context, uint, uint) (*messaging.WorkerStatusSnapshot, error) {
	return f.snapshot, f.err
}
func TestWorkerUnknownBusyAndDegraded(t *testing.T) {
	for _, kind := range []string{"busy", "timeout", "degraded", "stale", "wrong identity", "negative"} {
		t.Run(kind, func(t *testing.T) {
			s := &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: time.Now().Unix()}
			fake := workerFake{snapshot: s}
			switch kind {
			case "busy":
				s.InteractionWaitingCount = 1
			case "timeout":
				fake.err = errors.New("secret")
			case "degraded":
				s.Degraded = true
			case "stale":
				s.SnapshotAt -= 31
			case "wrong identity":
				s.OrgID = 8
			case "negative":
				s.AcceptedCount = -1
			}
			svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot()), WorkerIDs: []uint{9}}, fake)
			result, err := svc.Preflight(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Ready {
				t.Fatal("unexpected ready")
			}
			expected := "worker_unknown"
			if kind == "busy" {
				expected = "worker_busy"
			}
			if !hasCheck(result.Status.Checks, expected) {
				t.Fatalf("missing %s: %+v", expected, result)
			}
		})
	}
}
func TestUnconfiguredAndInvalidBinding(t *testing.T) {
	for _, cfg := range []config.SparkConfig{{}, {OrgID: 7, HostID: "host-1", SocketPath: "relative.sock"}, {OrgID: 7, HostID: "host-1", SocketPath: "/tmp/a", WorkerIDs: []uint{0}}} {
		if _, err := NewService(cfg, nil).Status(context.Background()); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("got %v", err)
		}
	}
}

type incompleteRequester struct{ data []byte }

func (f incompleteRequester) RequestReply(context.Context, string, any) (*nats.Msg, error) {
	return &nats.Msg{Data: f.data}, nil
}
func TestMissingWorkerFieldsRemainUnknown(t *testing.T) {
	full, _ := json.Marshal(messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: time.Now().Unix()})
	for _, field := range []string{"all", "accepted_count", "running_count"} {
		t.Run(field, func(t *testing.T) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(full, &raw); err != nil {
				t.Fatal(err)
			}
			if field == "all" {
				raw = map[string]json.RawMessage{"org_id": raw["org_id"], "worker_id": raw["worker_id"], "snapshot_at": raw["snapshot_at"]}
			} else if field == "running_count" {
				raw[field] = json.RawMessage("null")
			} else {
				delete(raw, field)
			}
			payload, _ := json.Marshal(raw)
			workers := service.NewWorkerOpsService(incompleteRequester{data: payload}, time.Second)
			svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot()), WorkerIDs: []uint{9}}, workers)
			status, err := svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.Workers[0].State != "unknown" || status.Workers[0].RunningCount != nil {
				t.Fatalf("incomplete response became zero tasks: %+v", status.Workers[0])
			}
		})
	}
}

type delayedWorkers struct{ firstAt int64 }

func (f delayedWorkers) QueryWorkerStatusForPreflight(ctx context.Context, org, id uint) (*messaging.WorkerStatusSnapshot, error) {
	at := f.firstAt
	if id == 10 {
		select {
		case <-time.After(3200 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		at = time.Now().Unix()
	}
	return &messaging.WorkerStatusSnapshot{OrgID: org, WorkerID: id, SnapshotAt: at}, nil
}
func TestWorkerExpiringDuringAggregationBecomesUnknown(t *testing.T) {
	svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot()), WorkerIDs: []uint{9, 10}}, delayedWorkers{firstAt: time.Now().Unix() - 29})
	result, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Workers[0].State != "unknown" || result.Workers[0].RunningCount != nil {
		t.Fatalf("stale worker exposed as observed: %+v", result.Workers[0])
	}
}

func TestStatusExpiresWithEarliestWorkerObservation(t *testing.T) {
	at := time.Now().Unix() - 20
	svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot()), WorkerIDs: []uint{9}}, workerFake{snapshot: &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: at}})
	result, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidUntil != at+30 || result.Workers[0].ObservedAt != at {
		t.Fatalf("wrong expiry: %+v", result)
	}
}

func TestMaintenanceObservationRequiresMatchingHostAndFreshWorker(t *testing.T) {
	for _, tc := range []struct {
		name string
		gate *types.SparkMaintenanceState
		want string
	}{
		{"old worker", nil, "unsupported"},
		{"paused", &types.SparkMaintenanceState{Configured: true, Known: true, Active: true, HostID: "host-1", Revision: 2, OperationID: "op"}, "paused"},
		{"accepting", &types.SparkMaintenanceState{Configured: true, Known: true, HostID: "host-1", Revision: 3, OperationID: "op"}, "accepting"},
		{"foreign", &types.SparkMaintenanceState{Configured: true, Known: true, Active: true, HostID: "other", Revision: 2, OperationID: "op"}, "unknown"},
		{"failed", &types.SparkMaintenanceState{Configured: true, Active: true}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &messaging.WorkerStatusSnapshot{OrgID: 7, WorkerID: 9, SnapshotAt: time.Now().Unix(), Maintenance: tc.gate}
			svc := NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", SocketPath: snapshotAgent(t, validSnapshot()), WorkerIDs: []uint{9}}, workerFake{snapshot: w})
			s, err := svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if s.Workers[0].Maintenance != tc.want {
				t.Fatalf("maintenance=%q want=%q", s.Workers[0].Maintenance, tc.want)
			}
			if s.CanShutdown {
				t.Fatal("observation enabled execution")
			}
		})
	}
}
