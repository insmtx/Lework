//go:build !enterprise

package oss

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func newTestOrg(t *testing.T) *org {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return NewOrg(database, nil)
}

func TestOrgIsOrgCreator(t *testing.T) {
	s := newTestOrg(t)
	if err := s.db.AutoMigrate(&types.Organization{}); err != nil {
		t.Fatalf("migrate organization: %v", err)
	}

	org := &types.Organization{Code: "creator-test", Name: "creator test", Status: "active", CreatedByUin: 42}
	if err := s.db.Create(org).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}

	creator := uint(42)
	nonCreator := uint(7)

	ok, err := s.IsOrgCreator(context.Background(), org.ID, creator)
	if err != nil {
		t.Fatalf("IsOrgCreator(creator): %v", err)
	}
	if !ok {
		t.Error("expected true for creator")
	}

	ok, err = s.IsOrgCreator(context.Background(), org.ID, nonCreator)
	if err != nil {
		t.Fatalf("IsOrgCreator(non-creator): %v", err)
	}
	if ok {
		t.Error("expected false for non-creator")
	}

	ok, err = s.IsOrgCreator(context.Background(), 999999999, creator)
	if err != nil {
		t.Fatalf("IsOrgCreator(missing org): %v", err)
	}
	if ok {
		t.Error("expected false for missing org")
	}

	ok, err = s.IsOrgCreator(context.Background(), 0, creator)
	if err != nil {
		t.Fatalf("IsOrgCreator(zero org): %v", err)
	}
	if ok {
		t.Error("expected false for zero org")
	}
}
