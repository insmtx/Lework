package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func workflowStore(t *testing.T) *SparkWorkflowDAO {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "workflow.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.AutoMigrate(&types.SparkWorkflowHost{}, &types.SparkWorkflowOperation{}); err != nil {
		t.Fatal(err)
	}
	s, err := NewSparkWorkflowDAO(d, 1, "host")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestSparkWorkflowReservationAndImmutableIdentity(t *testing.T) {
	s := workflowStore(t)
	ctx := context.Background()
	r := types.SparkWorkflowOperation{HostID: "host", OrgID: 1, OperationID: "a", Fingerprint: "same", Phase: types.SparkWorkflowAccepted, Version: 1}
	got, created, err := s.Begin(ctx, r)
	if err != nil || !created {
		t.Fatalf("begin: %v %v", created, err)
	}
	_, created, err = s.Begin(ctx, r)
	if err != nil || created {
		t.Fatalf("duplicate: %v %v", created, err)
	}
	r.Fingerprint = "different"
	if _, _, err = s.Begin(ctx, r); !errors.Is(err, types.ErrSparkWorkflowConflict) {
		t.Fatalf("payload: %v", err)
	}
	r.OperationID = "b"
	if _, _, err = s.Begin(ctx, r); !errors.Is(err, types.ErrSparkWorkflowConflict) {
		t.Fatalf("slot: %v", err)
	}
	if _, err = NewSparkWorkflowDAO(s.db, 2, "host"); !errors.Is(err, types.ErrSparkWorkflowConflict) {
		t.Fatalf("org rebinding: %v", err)
	}
	next := got
	next.Phase = types.SparkWorkflowCompleted
	next.Version++
	if err = s.Transition(ctx, got, next, true); err != nil {
		t.Fatal(err)
	}
	if err = s.Transition(ctx, got, next, true); !errors.Is(err, types.ErrSparkWorkflowConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	if _, _, err = s.Begin(ctx, r); err != nil {
		t.Fatalf("released slot: %v", err)
	}
}
func TestSparkWorkflowConcurrentReservations(t *testing.T) {
	s := workflowStore(t)
	var wg sync.WaitGroup
	won := make(chan string, 16)
	for _, id := range []string{"workflow", "manual-model", "manual-pause", "manual-resume"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			other, _ := NewSparkWorkflowDAO(s.db, 1, "host")
			if other == nil {
				return
			}
			_, created, err := other.Begin(context.Background(), types.SparkWorkflowOperation{HostID: "host", OrgID: 1, OperationID: id, Fingerprint: id, Phase: types.SparkWorkflowAccepted, Version: 1})
			if err == nil && created {
				won <- id
			}
		}(id)
	}
	wg.Wait()
	close(won)
	if len(won) != 1 {
		t.Fatalf("winners=%d", len(won))
	}
}

func TestSparkWorkflowPostgresReservation(t *testing.T) {
	dsn := os.Getenv("SPARK_WORKFLOW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	testSparkWorkflowDialect(t, postgres.Open(dsn))
}
func TestSparkWorkflowMySQLReservation(t *testing.T) {
	dsn := os.Getenv("SPARK_WORKFLOW_TEST_MYSQL")
	if dsn == "" {
		t.Skip("isolated MySQL DSN not configured")
	}
	testSparkWorkflowDialect(t, mysql.Open(dsn))
}
func testSparkWorkflowDialect(t *testing.T, dialect gorm.Dialector) {
	d, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.AutoMigrate(&types.SparkWorkflowHost{}, &types.SparkWorkflowOperation{}); err != nil {
		t.Fatal(err)
	}
	host := uuid.NewString()
	s, err := NewSparkWorkflowDAO(d, 1, host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Where("host_id = ?", host).Delete(&types.SparkWorkflowOperation{})
		d.Where("host_id = ?", host).Delete(&types.SparkWorkflowHost{})
	})
	var wg sync.WaitGroup
	wins := make(chan types.SparkWorkflowOperation, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, created, e := s.Begin(context.Background(), types.SparkWorkflowOperation{HostID: host, OrgID: 1, OperationID: uuid.NewString(), Version: 1, Phase: types.SparkWorkflowAccepted, Fingerprint: "payload"})
			if e == nil && created {
				wins <- r
			}
		}()
	}
	wg.Wait()
	close(wins)
	if len(wins) != 1 {
		t.Fatalf("winners %d", len(wins))
	}
	r := <-wins
	if _, created, e := s.Begin(context.Background(), r); e != nil || created {
		t.Fatalf("idempotency %v %v", created, e)
	}
	changed := r
	changed.Fingerprint = "different"
	if _, _, e := s.Begin(context.Background(), changed); !errors.Is(e, types.ErrSparkWorkflowConflict) {
		t.Fatalf("fingerprint %v", e)
	}
	if _, e := NewSparkWorkflowDAO(d, 2, host); !errors.Is(e, types.ErrSparkWorkflowConflict) {
		t.Fatalf("org %v", e)
	}
	next := r
	next.Version++
	next.Phase = types.SparkWorkflowCompleted
	if e := s.Transition(context.Background(), r, next, true); e != nil {
		t.Fatal(e)
	}
	if e := s.Transition(context.Background(), r, next, true); !errors.Is(e, types.ErrSparkWorkflowConflict) {
		t.Fatalf("stale CAS %v", e)
	}
}

func TestSparkWorkflowRejectsVolatileSQLite(t *testing.T) {
	d, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.AutoMigrate(&types.SparkWorkflowHost{}, &types.SparkWorkflowOperation{}); e != nil {
		t.Fatal(e)
	}
	if _, e = NewSparkWorkflowDAO(d, 1, "host"); e == nil {
		t.Fatal("volatile database enabled durable workflows")
	}
}
