package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/types"
)

type maintenancePublisher struct {
	eventbus.Publisher
	calls int
}

func (p *maintenancePublisher) Publish(context.Context, string, any) error { p.calls++; return nil }
func TestDispatcherMaintenancePreservesPendingAndResumes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "outbox.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.ReliableTask{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := types.ReliableTask{TaskID: "test", Destination: "test.run", Payload: []byte(`{}`), Status: types.ReliableTaskPending, NextAttemptAt: now.Add(-time.Second), DeadlineAt: now.Add(time.Minute)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	pub := &maintenancePublisher{}
	allowed := false
	d := NewReliableTaskDispatcher(db, pub).WithAdmissionGate(func() bool { return allowed })
	d.scan(context.Background())
	var actual types.ReliableTask
	db.First(&actual, row.ID)
	if pub.calls != 0 || actual.LeaseUntil != nil || actual.Status != types.ReliableTaskPending {
		t.Fatalf("maintenance claimed/published record: %+v calls=%d", actual, pub.calls)
	}
	d.publishOne(context.Background(), row)
	if pub.calls != 0 {
		t.Fatal("publish bypassed gate recheck")
	}
	allowed = true
	d.scan(context.Background())
	db.First(&actual, row.ID)
	if pub.calls != 1 || actual.Status != types.ReliableTaskPublished {
		t.Fatalf("resume did not publish: %+v calls=%d", actual, pub.calls)
	}
}
