package seed

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAccountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&types.Organization{},
		&types.User{},
		&types.UserOrg{},
		&types.DigitalAssistant{},
		&types.WorkerDeployment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSeedAccountCreatesDefaultOrgUserAndWorker(t *testing.T) {
	db := newAccountTestDB(t)
	if err := seedAccount(context.Background(), db); err != nil {
		t.Fatalf("seedAccount: %v", err)
	}

	var org types.Organization
	if err := db.Where("code = ?", "default_org").First(&org).Error; err != nil {
		t.Fatalf("default org missing: %v", err)
	}
	if org.ID != 1 {
		t.Fatalf("expected default org id=1, got %d", org.ID)
	}

	var user types.User
	if err := db.Where("email = ?", "admin@leros.local").First(&user).Error; err != nil {
		t.Fatalf("admin user missing: %v", err)
	}
	if string(user.Password) == "" {
		t.Fatal("admin password must be hashed")
	}

	var uo types.UserOrg
	if err := db.First(&uo).Error; err != nil {
		t.Fatalf("user-org missing: %v", err)
	}
	if uo.UserID != user.ID || uo.OrgID != org.ID || !uo.IsDefault {
		t.Fatalf("unexpected user-org: %+v", uo)
	}

	var da types.DigitalAssistant
	if err := db.Where("org_id = ?", org.ID).First(&da).Error; err != nil {
		t.Fatalf("default assistant missing: %v", err)
	}

	var dep types.WorkerDeployment
	if err := db.Where("org_id = ? AND worker_id = ?", org.ID, 1).First(&dep).Error; err != nil {
		t.Fatalf("default worker deployment missing: %v", err)
	}
	if dep.DigitalAssistantID != da.ID {
		t.Fatalf("deployment assistant id mismatch: %d != %d", dep.DigitalAssistantID, da.ID)
	}
}

func TestSeedAccountIdempotent(t *testing.T) {
	db := newAccountTestDB(t)
	if err := seedAccount(context.Background(), db); err != nil {
		t.Fatalf("first seedAccount: %v", err)
	}
	if err := seedAccount(context.Background(), db); err != nil {
		t.Fatalf("second seedAccount: %v", err)
	}
	var orgCount, userCount, uoCount, daCount, depCount int64
	db.Model(&types.Organization{}).Count(&orgCount)
	db.Model(&types.User{}).Count(&userCount)
	db.Model(&types.UserOrg{}).Count(&uoCount)
	db.Model(&types.DigitalAssistant{}).Count(&daCount)
	db.Model(&types.WorkerDeployment{}).Count(&depCount)
	if orgCount != 1 || userCount != 1 || uoCount != 1 || daCount != 1 || depCount != 1 {
		t.Fatalf("seed not idempotent: org=%d user=%d uo=%d da=%d dep=%d", orgCount, userCount, uoCount, daCount, depCount)
	}
}
