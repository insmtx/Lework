package service

import (
	"gorm.io/gorm"
)

// PermissionForDB returns a PermissionService bound to db.
// Inside an open transaction, pass tx so auth reads uncommitted bindings
// and avoids SQLite single-connection deadlocks.
//
// A new core bound to tx is created via newCore so that all resource and
// binding lookups happen inside the same transaction.
func PermissionForDB(db *gorm.DB, base *PermissionService) *PermissionService {
	if db == nil || base == nil || db == base.db {
		return base
	}
	return &PermissionService{
		db:      db,
		core:    base.newCore(db),
		newCore: base.newCore,
	}
}
