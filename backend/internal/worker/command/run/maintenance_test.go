package run

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/worker/agentrun"
	"github.com/insmtx/Leros/backend/internal/worker/command/run/inbox"
	"github.com/insmtx/Leros/backend/types"
)

type testMaintenance struct {
	mu    sync.Mutex
	state types.SparkMaintenanceState
}

func (m *testMaintenance) Read() types.SparkMaintenanceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}
func (m *testMaintenance) set(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = types.SparkMaintenanceState{Configured: true, Known: true, Active: active, Revision: 1, OperationID: "test"}
}
func maintenanceHandler(t *testing.T, gate *testMaintenance) (*Handler, *handlerRuntime) {
	t.Helper()
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	svc := agentrun.NewService(handlerPreparer{}, agent.NewExecutor(registry), handlerFinalizer{}, agentrun.NewJournalFactory(), nil)
	h, err := New(Config{OrgID: 1, WorkerID: 2, MaxConcurrency: 1, DebounceWindow: time.Millisecond, InboxDBPath: ":memory:", Maintenance: gate}, &handlerPublisher{}, svc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-runtime.release:
		default:
			close(runtime.release)
		}
		h.StopAdmission()
		if !h.Drain(2 * time.Second) {
			t.Error("drain timed out")
		}
		h.Close()
	})
	return h, runtime
}
func TestMaintenanceRejectsWithoutPersistenceAndResumes(t *testing.T) {
	gate := &testMaintenance{}
	gate.set(true)
	h, runtime := maintenanceHandler(t, gate)
	d := newFakeDelivery(1)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), d); err == nil {
		t.Fatal("maintenance admitted command")
	}
	ack, nak, term, _ := d.snapshot()
	if ack || !nak || term {
		t.Fatalf("ack=%v nak=%v term=%v", ack, nak, term)
	}
	records, err := h.runInbox.GetNonTerminal(context.Background(), h.RunSubject())
	if err != nil || len(records) != 0 {
		t.Fatalf("persisted rejected command: %v %v", records, err)
	}
	gate.set(false)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), newFakeDelivery(1)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not execute")
	}
	gate.set(true)
	s := h.Status(context.Background())
	if s.Maintenance == nil || !s.Maintenance.Active || s.AcceptedCount == 0 {
		t.Fatalf("missing paused busy status: %+v", s)
	}
	close(runtime.release)
	deadline := time.Now().Add(2 * time.Second)
	for h.Status(context.Background()).AcceptedCount != 0 {
		if time.Now().After(deadline) {
			t.Fatal("pause prevented accepted run completing")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestMaintenanceDelaysRecoveryAndStopExits(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "resume"}[resume], func(t *testing.T) {
			gate := &testMaintenance{}
			gate.set(true)
			h, runtime := maintenanceHandler(t, gate)
			_, _, err := h.runInbox.PutIfAbsent(context.Background(), h.RunSubject(), 100, standardCommand())
			if err != nil {
				t.Fatal(err)
			}
			if err := h.RecoverNonTerminal(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-runtime.started:
				t.Fatal("recovered during maintenance")
			case <-time.After(150 * time.Millisecond):
			}
			s := h.Status(context.Background())
			if s.InboxPendingCount != 1 || s.AcceptedCount != 0 {
				t.Fatalf("pending recovery lost: %+v", s)
			}
			if resume {
				gate.set(false)
				select {
				case <-runtime.started:
				case <-time.After(2 * time.Second):
					t.Fatal("recovery did not resume")
				}
			} else {
				h.StopAdmission()
				if !h.Drain(time.Second) {
					t.Fatal("maintenance wait blocked stop")
				}
			}
		})
	}
}

type blockingMaintenanceInbox struct {
	inbox.RunInbox
	entered chan struct{}
	release chan struct{}
}

// Count is inside admission so the status barrier must wait for this snapshot-invisible admission.
func (b *blockingMaintenanceInbox) CountByStatus(ctx context.Context, topic string, status inbox.Status) (int, error) {
	if status == inbox.StatusPending {
		select {
		case b.entered <- struct{}{}:
			<-b.release
		default:
		}
	}
	return b.RunInbox.CountByStatus(ctx, topic, status)
}
func TestMaintenanceStatusWaitsForAdmission(t *testing.T) {
	gate := &testMaintenance{}
	gate.set(false)
	h, _ := maintenanceHandler(t, gate)
	b := &blockingMaintenanceInbox{RunInbox: h.runInbox, entered: make(chan struct{}, 1), release: make(chan struct{})}
	h.runInbox = b
	done := make(chan error, 1)
	go func() { done <- h.HandleRunCommand(context.Background(), standardCommand(), newFakeDelivery(1)) }()
	<-b.entered
	gate.set(true)
	statusDone := make(chan struct{})
	go func() { h.Status(context.Background()); close(statusDone) }()
	select {
	case <-statusDone:
		t.Fatal("status bypassed in-flight admission")
	case <-time.After(50 * time.Millisecond):
	}
	close(b.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-statusDone:
	case <-time.After(time.Second):
		t.Fatal("status barrier stuck")
	}
}

type resetTrackingInbox struct {
	inbox.RunInbox
	resets int
	mu     sync.Mutex
}

func (b *resetTrackingInbox) ResetProcessing(ctx context.Context, topic string) error {
	b.mu.Lock()
	b.resets++
	b.mu.Unlock()
	return b.RunInbox.ResetProcessing(ctx, topic)
}
func (b *resetTrackingInbox) count() int { b.mu.Lock(); defer b.mu.Unlock(); return b.resets }
func TestMaintenanceRecoveryResetsInterruptedWorkBeforeSubscriptions(t *testing.T) {
	gate := &testMaintenance{}
	gate.set(true)
	h, runtime := maintenanceHandler(t, gate)
	b := &resetTrackingInbox{RunInbox: h.runInbox}
	h.runInbox = b
	if _, _, err := h.runInbox.PutIfAbsent(context.Background(), h.RunSubject(), 100, standardCommand()); err != nil {
		t.Fatal(err)
	}
	if err := h.RecoverNonTerminal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.count() != 1 {
		t.Fatal("crash reset must happen before subscriptions, not after resume with new live work")
	}
	gate.set(false)
	select {
	case <-runtime.started:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not execute")
	}
	if b.count() != 1 {
		t.Fatal("deferred recovery reset already-running records")
	}
}

type failedRecoveryInbox struct{ inbox.RunInbox }

func (b failedRecoveryInbox) GetNonTerminal(context.Context, string) ([]inbox.Record, error) {
	return nil, fmt.Errorf("injected storage failure")
}
func TestMaintenanceDeferredRecoveryFailureIsDegraded(t *testing.T) {
	gate := &testMaintenance{}
	gate.set(true)
	h, _ := maintenanceHandler(t, gate)
	h.runInbox = failedRecoveryInbox{h.runInbox}
	if err := h.RecoverNonTerminal(context.Background()); err != nil {
		t.Fatal(err)
	}
	gate.set(false)
	deadline := time.Now().Add(time.Second)
	for !h.Status(context.Background()).Degraded {
		if time.Now().After(deadline) {
			t.Fatal("recovery failure hidden")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestMaintenanceUnknownStateDoesNotAdmit(t *testing.T) {
	gate := &testMaintenance{state: types.SparkMaintenanceState{Configured: true}}
	h, _ := maintenanceHandler(t, gate)
	if err := h.HandleRunCommand(context.Background(), standardCommand(), newFakeDelivery(1)); err == nil {
		t.Fatal("unknown state admitted work")
	}
	s := h.Status(context.Background())
	if !s.Degraded || s.InboxPendingCount != 0 {
		t.Fatalf("unknown state not reflected: %+v", s)
	}
}
