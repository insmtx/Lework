package spark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

type workflowMemory struct {
	r               *types.SparkWorkflowOperation
	fail            bool
	afterTransition func(types.SparkWorkflowOperation)
}

func (m *workflowMemory) Get(_ context.Context, id string) (*types.SparkWorkflowOperation, error) {
	if m.fail {
		return nil, errors.New("db")
	}
	if m.r != nil && m.r.OperationID == id {
		r := *m.r
		return &r, nil
	}
	return nil, nil
}
func (m *workflowMemory) Current(ctx context.Context) (*types.SparkWorkflowOperation, error) {
	if m.r == nil {
		return nil, nil
	}
	return m.Get(ctx, m.r.OperationID)
}
func (m *workflowMemory) Begin(_ context.Context, r types.SparkWorkflowOperation) (types.SparkWorkflowOperation, bool, error) {
	if m.fail {
		return r, false, errors.New("db")
	}
	if m.r != nil {
		return r, false, types.ErrSparkWorkflowConflict
	}
	m.r = &r
	return r, true, nil
}
func (m *workflowMemory) Transition(_ context.Context, old, next types.SparkWorkflowOperation, _ bool) error {
	if m.fail {
		return errors.New("db")
	}
	if m.r.Version != old.Version || m.r.Phase != old.Phase {
		return types.ErrSparkWorkflowConflict
	}
	m.r = &next
	if m.afterTransition != nil {
		m.afterTransition(next)
	}
	return nil
}
func TestWorkflowRestartNeverReplaysIntent(t *testing.T) {
	for _, phase := range []types.SparkWorkflowPhase{types.SparkWorkflowAccepted, types.SparkWorkflowPauseIntent, types.SparkWorkflowWaitingIdle, types.SparkWorkflowModelIntent, types.SparkWorkflowWaitingModel, types.SparkWorkflowResumeIntent, types.SparkWorkflowWaitingResume} {
		t.Run(string(phase), func(t *testing.T) {
			m := &workflowMemory{r: &types.SparkWorkflowOperation{OperationID: "op", RunnerID: "dead", Kind: "workflow", Phase: phase, Version: 1, Deadline: time.Now().Add(time.Minute).Unix()}}
			c := &Coordinator{store: m, runner: "new"}
			c.step(context.Background())
			if m.r.Phase != phase || m.r.Version != 1 {
				t.Fatalf("replayed or unbounded: %+v", m.r)
			}
		})
	}
}
func TestWorkflowIntentAndExpiryRetainSlot(t *testing.T) {
	for _, phase := range []types.SparkWorkflowPhase{types.SparkWorkflowPauseIntent, types.SparkWorkflowModelIntent, types.SparkWorkflowResumeIntent, types.SparkWorkflowWaitingIdle} {
		m := &workflowMemory{r: &types.SparkWorkflowOperation{OperationID: "op", RunnerID: "same", Kind: "workflow", Phase: phase, Version: 1, Deadline: 1}}
		c := &Coordinator{store: m, runner: "same"}
		c.step(context.Background())
		if m.r.Phase != types.SparkWorkflowAttention {
			t.Fatal("expired operation did not require attention")
		}
	}
}

type workflowWorkers func(context.Context, uint, uint) (*messaging.WorkerStatusSnapshot, error)

func (f workflowWorkers) QueryWorkerStatusForPreflight(ctx context.Context, o, w uint) (*messaging.WorkerStatusSnapshot, error) {
	return f(ctx, o, w)
}

type workflowFixture struct {
	svc       *Service
	c         *Coordinator
	store     *workflowMemory
	gate      maintenanceObservation
	model     types.SparkModelControl
	posts     []string
	worker    messaging.WorkerStatusSnapshot
	statePath string
	mu        sync.Mutex
	postFault func()
}

func newWorkflowFixture(t *testing.T, paused bool, action string) *workflowFixture {
	t.Helper()
	now := time.Now().Unix()
	f := &workflowFixture{store: &workflowMemory{}, statePath: filepath.Join(t.TempDir(), "gate")}
	f.gate = maintenanceObservation{Version: 1, HostID: "host-1", BootID: "boot-1", Active: paused, Revision: 1, OperationID: "initial", ObservedAt: now, UpdatedAt: now}
	f.model = types.SparkModelControl{ProtocolVersion: 1, HostID: "host-1", BootID: "boot-1", ObservedAt: now, ModelID: "model", Label: "Model", State: "stopped", Revision: 1, OperationID: "initial", Action: "none"}
	if action == "stop" {
		f.model.State = "ready"
	}
	save := func() {
		raw, _ := json.Marshal(f.gate)
		if err := os.WriteFile(f.statePath, raw, 0600); err != nil {
			t.Error(err)
		}
	}
	save()
	path := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == "POST" {
			f.posts = append(f.posts, r.URL.Path)
			if f.postFault != nil {
				f.postFault()
			}
			switch r.URL.Path {
			case "/maintenance":
				var req types.SparkMaintenanceRequest
				json.NewDecoder(r.Body).Decode(&req)
				f.gate.Active = *req.Active
				f.gate.OperationID = req.OperationID
				f.gate.Revision++
				save()
			case "/model":
				var req types.SparkModelRequest
				json.NewDecoder(r.Body).Decode(&req)
				f.model.OperationID = req.OperationID
				f.model.Action = req.Action
				f.model.Revision++
				if req.Action == "start" {
					f.model.State = "ready"
				} else {
					f.model.State = "stopped"
				}
			}
		}
		switch r.URL.Path {
		case "/status":
			json.NewEncoder(w).Encode(validSnapshot())
		case "/maintenance":
			json.NewEncoder(w).Encode(f.gate)
		case "/model":
			raw, _ := json.Marshal(f.model)
			var wire map[string]json.RawMessage
			json.Unmarshal(raw, &wire)
			delete(wire, "can_start")
			delete(wire, "can_stop")
			json.NewEncoder(w).Encode(wire)
		}
	})
	workers := workflowWorkers(func(context.Context, uint, uint) (*messaging.WorkerStatusSnapshot, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := f.worker
		out.OrgID = 7
		out.WorkerID = 9
		if out.SnapshotAt == 0 {
			out.SnapshotAt = time.Now().Unix()
		}
		out.Maintenance = &types.SparkMaintenanceState{Configured: true, Known: true, HostID: "host-1", Active: f.gate.Active, Revision: f.gate.Revision, OperationID: f.gate.OperationID}
		return &out, nil
	})
	f.svc = NewService(config.SparkConfig{OrgID: 7, HostID: "host-1", WorkerIDs: []uint{9}, SocketPath: path, ModelControl: true, MaintenanceControl: true, ModelWorkflow: true, Maintenance: &config.SparkMaintenanceConfig{StatePath: f.statePath, HostID: "host-1"}}, workers)
	var err error
	f.c, err = NewCoordinator(f.svc, f.store)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *workflowFixture) begin(t *testing.T, action string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := f.svc.BeginWorkflow(ctx, 42, types.SparkWorkflowRequest{OperationID: "op", Action: action, BootID: "boot-1", ExpectedModelRevision: 1, ExpectedMaintenanceRevision: 1})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
}
func TestWorkflowOwnedAndPreexistingPause(t *testing.T) {
	for _, tc := range []struct {
		paused bool
		action string
		want   types.SparkWorkflowPhase
		posts  int
	}{{false, "start", types.SparkWorkflowCompleted, 3}, {true, "start", types.SparkWorkflowCompletedPaused, 1}, {false, "stop", types.SparkWorkflowCompletedPaused, 2}, {true, "stop", types.SparkWorkflowCompletedPaused, 1}} {
		t.Run(fmt.Sprintf("%v_%s", tc.paused, tc.action), func(t *testing.T) {
			f := newWorkflowFixture(t, tc.paused, tc.action)
			f.begin(t, tc.action)
			public, err := f.svc.Status(context.Background())
			if err != nil || public.CanStartModel || public.CanStopModel || public.MaintenanceControl.CanResume || public.MaintenanceControl.CanPause {
				t.Fatalf("public capability did not block: %+v %v", public, err)
			}
			for i := 0; i < 8; i++ {
				f.c.step(context.Background())
			}
			if f.store.r.Phase != tc.want || len(f.posts) != tc.posts {
				t.Fatalf("phase=%s posts=%v", f.store.r.Phase, f.posts)
			}
			if f.gate.Active != (tc.want == types.SparkWorkflowCompletedPaused) {
				t.Fatal("wrong admission")
			}
		})
	}
}
func TestWorkflowDatabaseFailureAfterSendNeverReplays(t *testing.T) {
	for _, boundary := range []string{"/maintenance", "/model"} {
		t.Run(boundary, func(t *testing.T) {
			f := newWorkflowFixture(t, false, "start")
			f.begin(t, "start")
			f.postFault = func() {
				if f.posts[len(f.posts)-1] == boundary {
					f.store.fail = true
				}
			}
			for i := 0; i < 5 && !f.store.fail; i++ {
				f.c.step(context.Background())
			}
			before := len(f.posts)
			f.store.fail = false
			f.postFault = nil
			for i := 0; i < 5; i++ {
				f.c.step(context.Background())
			}
			if len(f.posts) != before || f.store.r.Phase != types.SparkWorkflowAttention || !f.gate.Active {
				t.Fatalf("unsafe replay: %v %+v", f.posts, f.store.r)
			}
		})
	}
}
func TestWorkflowEveryWorkerCounterBlocksModel(t *testing.T) {
	for index := 0; index < 10; index++ {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			f := newWorkflowFixture(t, true, "start")
			ptrs := []*int{&f.worker.RunningCount, &f.worker.WaitingCount, &f.worker.AcceptedCount, &f.worker.AdmissionWaitingCount, &f.worker.ComputeBusyCount, &f.worker.InteractionWaitingCount, &f.worker.InboxPendingCount, &f.worker.InboxProcessingCount, &f.worker.DebounceWaitingCount, &f.worker.CoordinatorWaitingCount}
			*ptrs[index] = 1
			f.begin(t, "start")
			for i := 0; i < 4; i++ {
				f.c.step(context.Background())
			}
			if len(f.posts) != 0 || f.store.r.Phase != types.SparkWorkflowWaitingIdle {
				t.Fatalf("busy counter dispatched %v", f.posts)
			}
		})
	}
}
func TestWorkflowChangedIdentityAndOwnerNeverResume(t *testing.T) {
	for _, change := range []string{"model", "owner", "binding", "boot"} {
		t.Run(change, func(t *testing.T) {
			f := newWorkflowFixture(t, false, "start")
			f.begin(t, "start")
			f.c.step(context.Background())
			f.c.step(context.Background())
			switch change {
			case "model":
				f.model.ModelID = "different"
			case "owner":
				f.gate.OperationID = "root-cli"
				raw, _ := json.Marshal(f.gate)
				os.WriteFile(f.statePath, raw, 0600)
			case "binding":
				f.c.binding = "new"
			case "boot":
				f.store.r.BootID = "old"
			}
			f.c.step(context.Background())
			if len(f.posts) != 2 || !f.gate.Active || f.store.r.Phase != types.SparkWorkflowAttention {
				t.Fatalf("unsafe restore %v %+v", f.posts, f.store.r)
			}
		})
	}
}
func TestWorkflowDBFailureBeforeIntentDoesNotSend(t *testing.T) {
	f := newWorkflowFixture(t, false, "start")
	f.begin(t, "start")
	f.store.fail = true
	f.c.step(context.Background())
	if len(f.posts) != 0 {
		t.Fatal("sent without durable intent")
	}
}
func TestWorkflowCancelledProcessDoesNotDispatch(t *testing.T) {
	f := newWorkflowFixture(t, false, "start")
	f.begin(t, "start")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.c.Run(ctx)
	if len(f.posts) != 0 {
		t.Fatal("sent after shutdown")
	}
}

func TestWorkflowLastObservationRejectsChangedModel(t *testing.T) {
	for _, phase := range []types.SparkWorkflowPhase{types.SparkWorkflowModelIntent, types.SparkWorkflowResumeIntent} {
		t.Run(string(phase), func(t *testing.T) {
			f := newWorkflowFixture(t, false, "start")
			f.begin(t, "start")
			f.store.afterTransition = func(r types.SparkWorkflowOperation) {
				if r.Phase == phase {
					f.mu.Lock()
					f.model.ModelID = "replaced"
					f.mu.Unlock()
				}
			}
			for i := 0; i < 6; i++ {
				f.c.step(context.Background())
			}
			want := 1
			if phase == types.SparkWorkflowResumeIntent {
				want = 2
			}
			if len(f.posts) != want || !f.gate.Active {
				t.Fatalf("sent using stale model identity: %v", f.posts)
			}
		})
	}
}
func TestWorkflowResumeReceiptDBFailureStaysUnknown(t *testing.T) {
	f := newWorkflowFixture(t, false, "start")
	f.begin(t, "start")
	f.postFault = func() {
		if len(f.posts) == 3 {
			f.store.fail = true
		}
	}
	for i := 0; i < 4; i++ {
		f.c.step(context.Background())
	}
	f.store.fail = false
	f.postFault = nil
	f.c.step(context.Background())
	if len(f.posts) != 3 || f.store.r.Phase != types.SparkWorkflowAttention || f.store.r.ErrorCode != "outcome_unknown" {
		t.Fatalf("resume replay %+v %v", f.store.r, f.posts)
	}
}
func TestWorkflowAndLegacyShareRealReservation(t *testing.T) {
	f := newWorkflowFixture(t, true, "start")
	d, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "race.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.AutoMigrate(&types.SparkWorkflowHost{}, &types.SparkWorkflowOperation{}); err != nil {
		t.Fatal(err)
	}
	store, err := infradb.NewSparkWorkflowDAO(d, 7, "host-1")
	if err != nil {
		t.Fatal(err)
	}
	f.c.store = store
	other := NewService(f.svc.cfg, f.svc.workers)
	if _, err = NewCoordinator(other, store); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		_, e := f.svc.BeginWorkflow(context.Background(), 42, types.SparkWorkflowRequest{OperationID: "workflow", Action: "start", BootID: "boot-1", ExpectedModelRevision: 1, ExpectedMaintenanceRevision: 1})
		wins <- e == nil
	}()
	go func() {
		defer wg.Done()
		_, e := other.ChangeModel(context.Background(), types.SparkModelRequest{OperationID: "model", Action: "start", BootID: "boot-1", ExpectedRevision: 1, MaintenanceRevision: 1})
		wins <- e == nil
	}()
	go func() {
		defer wg.Done()
		active := false
		_, e := other.ChangeMaintenance(context.Background(), types.SparkMaintenanceRequest{OperationID: "initial", Active: &active, BootID: "boot-1", ExpectedRevision: 1})
		wins <- e == nil
	}()
	wg.Wait()
	close(wins)
	won := 0
	for w := range wins {
		if w {
			won++
		}
	}
	if won != 1 || len(f.posts) > 1 {
		t.Fatalf("won=%d posts=%v", won, f.posts)
	}
}
func TestManualModelReceiptDoesNotReleaseBusySlot(t *testing.T) {
	f := newWorkflowFixture(t, true, "start")
	_, err := f.svc.ChangeModel(context.Background(), types.SparkModelRequest{OperationID: "manual", Action: "start", BootID: "boot-1", ExpectedRevision: 1, MaintenanceRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.model.State = "starting"
	f.model.Busy = true
	f.c.step(context.Background())
	if f.store.r.Phase != types.SparkWorkflowWaitingModel {
		t.Fatalf("busy manual receipt released: %+v", f.store.r)
	}
	status, err := f.svc.Status(context.Background())
	if err != nil || status.MaintenanceControl.CanResume {
		t.Fatalf("manual reservation unprotected %v", err)
	}
}
func TestWorkflowExpiredForeignProjectionDoesNotClearSlot(t *testing.T) {
	r := types.SparkWorkflowOperation{OperationID: "op", Phase: types.SparkWorkflowWaitingModel, Version: 1, Deadline: 1}
	p := projection(r)
	if !p.NeedsAttention || p.ErrorCode != "deadline_exceeded" || r.Phase != types.SparkWorkflowWaitingModel {
		t.Fatal("unsafe expiry projection")
	}
}

func TestLegacyPauseAndResumeUseIndependentReservationIDs(t *testing.T) {
	f := newWorkflowFixture(t, false, "stop")
	d, e := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual.db")), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.AutoMigrate(&types.SparkWorkflowHost{}, &types.SparkWorkflowOperation{}); e != nil {
		t.Fatal(e)
	}
	store, e := infradb.NewSparkWorkflowDAO(d, 7, "host-1")
	if e != nil {
		t.Fatal(e)
	}
	f.c.store = store
	active := true
	_, e = f.svc.ChangeMaintenance(context.Background(), types.SparkMaintenanceRequest{BootID: "boot-1", ExpectedRevision: 1, OperationID: "shared-owner", Active: &active})
	if e != nil {
		t.Fatal(e)
	}
	first, e := store.Current(context.Background())
	if e != nil || first == nil {
		t.Fatal("missing pause reservation")
	}
	f.c.step(context.Background())
	active = false
	_, e = f.svc.ChangeMaintenance(context.Background(), types.SparkMaintenanceRequest{BootID: "boot-1", ExpectedRevision: 2, OperationID: "shared-owner", Active: &active})
	if e != nil {
		t.Fatal(e)
	}
	second, e := store.Current(context.Background())
	if e != nil || second == nil || second.OperationID == first.OperationID {
		t.Fatal("pause/resume reused reservation")
	}
	f.c.step(context.Background())
	current, e := store.Current(context.Background())
	if e != nil || current != nil {
		t.Fatal("confirmed resume failed to release")
	}
}

func TestWorkflowCancellationAtEachCommittedIntentNeverSends(t *testing.T) {
	for _, tc := range []struct {
		phase types.SparkWorkflowPhase
		posts int
	}{{types.SparkWorkflowPauseIntent, 0}, {types.SparkWorkflowModelIntent, 1}, {types.SparkWorkflowResumeIntent, 2}} {
		t.Run(string(tc.phase), func(t *testing.T) {
			f := newWorkflowFixture(t, false, "start")
			f.begin(t, "start")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.store.afterTransition = func(r types.SparkWorkflowOperation) {
				if r.Phase == tc.phase {
					cancel()
				}
			}
			for i := 0; i < 5; i++ {
				f.c.step(ctx)
			}
			if f.store.r.Phase != tc.phase || len(f.posts) != tc.posts {
				t.Fatalf("sent after committed-intent cancellation: %+v %v", f.store.r, f.posts)
			}
			f.c.step(context.Background())
			if len(f.posts) != tc.posts || f.store.r.Phase != types.SparkWorkflowAttention {
				t.Fatal("replayed abandoned intent")
			}
		})
	}
}
