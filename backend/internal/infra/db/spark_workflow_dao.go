package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// SparkWorkflowDAO serializes every host writer in the shared durable database.
type SparkWorkflowDAO struct {
	db   *gorm.DB
	org  uint
	host string
}

// NewSparkWorkflowDAO verifies migrated tables and permanently binds the host to one organization.
func NewSparkWorkflowDAO(d *gorm.DB, org uint, host string) (*SparkWorkflowDAO, error) {
	if d == nil || org == 0 || host == "" {
		return nil, types.ErrSparkWorkflowConflict
	}
	// SQLite 内存连接不能提供进程重启后的 durable reservation。
	if d.Dialector.Name() == "sqlite" {
		var databases []struct {
			Name string
			File string
		}
		if err := d.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
			return nil, err
		}
		durable := false
		for _, database := range databases {
			if database.Name == "main" && database.File != "" {
				durable = true
			}
		}
		if !durable {
			return nil, types.ErrSparkWorkflowConflict
		}
	}
	s := &SparkWorkflowDAO{d, org, host}
	if err := d.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.SparkWorkflowHost{HostID: host, OrgID: org}).Error; err != nil {
		return nil, err
	}
	var row types.SparkWorkflowHost
	if err := d.Where("host_id = ? AND org_id = ?", host, org).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, types.ErrSparkWorkflowConflict
		}
		return nil, err
	}
	// 初始化必须确认两个迁移表都存在。
	if !d.Migrator().HasTable(&types.SparkWorkflowOperation{}) {
		return nil, types.ErrSparkWorkflowConflict
	}
	return s, nil
}
func (s *SparkWorkflowDAO) Get(ctx context.Context, id string) (*types.SparkWorkflowOperation, error) {
	var r types.SparkWorkflowOperation
	err := s.db.WithContext(ctx).Where("host_id = ? AND org_id = ? AND operation_id = ?", s.host, s.org, id).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}
func (s *SparkWorkflowDAO) Current(ctx context.Context) (*types.SparkWorkflowOperation, error) {
	var h types.SparkWorkflowHost
	if err := s.db.WithContext(ctx).Where("host_id = ? AND org_id = ?", s.host, s.org).First(&h).Error; err != nil {
		return nil, err
	}
	if h.ActiveOperationID == "" {
		return nil, nil
	}
	r, err := s.Get(ctx, h.ActiveOperationID)
	if err == nil && r == nil {
		return nil, types.ErrSparkWorkflowConflict
	}
	return r, err
}

// Begin atomically reserves the host and records acceptance. Duplicate IDs never create more work.
func (s *SparkWorkflowDAO) Begin(ctx context.Context, r types.SparkWorkflowOperation) (types.SparkWorkflowOperation, bool, error) {
	if r.HostID != s.host || r.OrgID != s.org {
		return r, false, types.ErrSparkWorkflowConflict
	}
	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old types.SparkWorkflowOperation
		err := tx.Where("host_id = ? AND operation_id = ?", s.host, r.OperationID).First(&old).Error
		if err == nil {
			if old.OrgID != s.org || old.Fingerprint != r.Fingerprint {
				return types.ErrSparkWorkflowConflict
			}
			r = old
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result := tx.Model(&types.SparkWorkflowHost{}).Where("host_id = ? AND org_id = ? AND active_operation_id = ?", s.host, s.org, "").Update("active_operation_id", r.OperationID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return types.ErrSparkWorkflowConflict
		}
		if err := tx.Model(&types.SparkWorkflowHost{}).Where("host_id = ?", s.host).UpdateColumn("revision", gorm.Expr("revision + 1")).Error; err != nil {
			return err
		}
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if errors.Is(err, types.ErrSparkWorkflowConflict) {
		existing, readErr := s.Get(ctx, r.OperationID)
		if readErr == nil && existing != nil && existing.Fingerprint == r.Fingerprint {
			return *existing, false, nil
		}
	}
	return r, created, err
}

// Transition allows only one intent winner and releases a confirmed terminal slot in the same transaction.
func (s *SparkWorkflowDAO) Transition(ctx context.Context, old, next types.SparkWorkflowOperation, release bool) error {
	if old.HostID != s.host || old.OrgID != s.org || next.Version != old.Version+1 {
		return types.ErrSparkWorkflowConflict
	}
	if release && next.Phase != types.SparkWorkflowCompleted && next.Phase != types.SparkWorkflowCompletedPaused {
		return types.ErrSparkWorkflowConflict
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&types.SparkWorkflowOperation{}).Where("host_id = ? AND org_id = ? AND operation_id = ? AND version = ? AND phase = ?", s.host, s.org, old.OperationID, old.Version, old.Phase).
			Select("phase", "version", "updated_at", "error_code", "owns_pause", "pause_revision", "pause_owner", "receipt_revision").Updates(&next)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return types.ErrSparkWorkflowConflict
		}
		if release {
			result = tx.Model(&types.SparkWorkflowHost{}).Where("host_id = ? AND org_id = ? AND active_operation_id = ?", s.host, s.org, old.OperationID).Update("active_operation_id", "")
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return types.ErrSparkWorkflowConflict
			}
		}
		return nil
	})
}
