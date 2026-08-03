package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Cond is the query condition interface.
// Each domain-specific condition implements BuildCondition to translate itself into GORM WHERE clauses.
type Cond interface {
	// BuildCondition applies query conditions to the GORM DB instance and returns the updated instance.
	BuildCondition(db *gorm.DB, tableName string) *gorm.DB
}

// Filter represents a single filtering condition.
type Filter struct {
	Field      string   // column name (without table prefix)
	Value      []string // filter values
	ExactMatch bool     // if true, uses IN (?) ; if false, uses LIKE '%value%'
}

// OrCondGroup represents a group of AND conditions within an OR clause.
type OrCondGroup struct {
	Query string
	Args  []any
}

// OrCond represents a set of OR conditions.
// CondGroups within the same OrCond are joined with AND.
// Different OrCond entries are joined with OR.
type OrCond struct {
	CondGroups []OrCondGroup
}

// BaseCond provides common query conditions and implements the Cond interface.
// Embed this struct in domain-specific conditions to inherit basic filtering.
type BaseCond struct {
	ID           uint
	IDs          []uint
	IsDelete     bool   // when false (default), filters deleted_at IS NULL (soft-delete aware)
	Page         int    // 1-based page number
	PageSize     int    // page size, applied only when both Page > 0 and PageSize > 0
	OrderBy      string // ORDER BY clause, e.g. "created_at DESC"
	Filters      []Filter
	OrConditions []OrCond
}

// BuildCondition translates BaseCond into GORM query clauses and returns the updated instance.
func (c *BaseCond) BuildCondition(db *gorm.DB, tableName string) *gorm.DB {
	if c.ID != 0 {
		db = db.Where(tableName+".id = ?", c.ID)
	}
	if len(c.IDs) > 0 {
		db = db.Where(tableName+".id IN (?)", c.IDs)
	}
	if !c.IsDelete {
		db = db.Where(tableName + ".deleted_at IS NULL")
	}
	for _, f := range c.Filters {
		col := tableName + "." + f.Field
		if f.ExactMatch {
			db = db.Where(col+" IN (?)", f.Value)
		} else {
			db = db.Where(col+" LIKE ?", "%"+f.Value[0]+"%")
		}
	}
	if len(c.OrConditions) > 0 {
		db = buildOrClause(db, tableName, c.OrConditions)
	}
	if c.OrderBy != "" {
		db = db.Order(c.OrderBy)
	}
	if c.Page > 0 && c.PageSize > 0 {
		db = db.Offset((c.Page - 1) * c.PageSize).Limit(c.PageSize)
	}
	return db
}

// buildOrClause constructs OR condition clauses by mutating db and returns the updated instance.
func buildOrClause(db *gorm.DB, tableName string, orConditions []OrCond) *gorm.DB {
	var parts []string
	var args []any
	for _, oc := range orConditions {
		if len(oc.CondGroups) == 0 {
			continue
		}
		if len(oc.CondGroups) == 1 {
			parts = append(parts, oc.CondGroups[0].Query)
			args = append(args, oc.CondGroups[0].Args...)
		} else {
			var subParts []string
			for _, group := range oc.CondGroups {
				subParts = append(subParts, group.Query)
				args = append(args, group.Args...)
			}
			parts = append(parts, "("+strings.Join(subParts, " AND ")+")")
		}
	}
	if len(parts) == 0 {
		return db
	}
	return db.Where(strings.Join(parts, " OR "), args...)
}

// Entity is the constraint for all entity types used with GenericDao.
// All GORM models in this codebase already implement TableName().
type Entity interface {
	TableName() string
}

// GenericDao provides type-safe, generic CRUD operations for any GORM entity.
// T is the entity type (pointer to struct, e.g. *types.User).
// The DB getter pattern is deliberately omitted here — callers pass *gorm.DB directly
// via NewGenericDao, matching the existing codebase convention.
type GenericDao[T Entity] struct {
	db *gorm.DB
}

// NewGenericDao creates a GenericDao instance bound to the given DB connection.
func NewGenericDao[T Entity](db *gorm.DB) *GenericDao[T] {
	return &GenericDao[T]{db: db}
}

// WithTx returns a new GenericDao bound to the transaction connection.
// The original instance is not mutated.
func (d *GenericDao[T]) WithTx(tx *gorm.DB) *GenericDao[T] {
	return &GenericDao[T]{db: tx}
}

// DB returns the underlying *gorm.DB for complex queries that cannot be expressed
// through Cond alone (e.g. multi-table JOINs, EXISTS subqueries).
func (d *GenericDao[T]) DB() *gorm.DB {
	return d.db
}

// Insert creates a single record.
func (d *GenericDao[T]) Insert(ctx context.Context, entity *T) error {
	return d.db.WithContext(ctx).Create(entity).Error
}

// GetByCond queries a single record by condition. Returns (nil, nil) when not found.
func (d *GenericDao[T]) GetByCond(ctx context.Context, cond Cond) (*T, error) {
	var entity T
	tableName := entity.TableName()
	query := d.db.WithContext(ctx)
	query = cond.BuildCondition(query, tableName)
	err := query.First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("generic dao GetByCond: %w", err)
	}
	return &entity, nil
}

// ListByCond queries a list of records by condition.
func (d *GenericDao[T]) ListByCond(ctx context.Context, cond Cond) ([]*T, error) {
	if cond == nil {
		return nil, fmt.Errorf("generic dao ListByCond: cond cannot be nil")
	}
	var entities []*T
	var entity T
	tableName := entity.TableName()
	query := d.db.WithContext(ctx)
	query = cond.BuildCondition(query, tableName)
	if err := query.Find(&entities).Error; err != nil {
		return nil, fmt.Errorf("generic dao ListByCond: %w", err)
	}
	return entities, nil
}

// PageByCond queries a paginated list of records by condition, returning (items, total, error).
// The Cond must carry Pag and PageSize for pagination to take effect.
// Count is performed on a separate query to avoid interfering with the main query's Limit/Offset.
func (d *GenericDao[T]) PageByCond(ctx context.Context, cond Cond) ([]*T, int64, error) {
	if cond == nil {
		return nil, 0, fmt.Errorf("generic dao PageByCond: cond cannot be nil")
	}
	var entities []*T
	var total int64
	var entity T
	tableName := entity.TableName()
	countQuery := d.db.WithContext(ctx).Table(tableName)
	countQuery = cond.BuildCondition(countQuery, tableName)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("generic dao PageByCond count: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}
	dataQuery := d.db.WithContext(ctx)
	dataQuery = cond.BuildCondition(dataQuery, tableName)
	if err := dataQuery.Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("generic dao PageByCond find: %w", err)
	}
	return entities, total, nil
}

// CountByCond counts records matching the condition.
func (d *GenericDao[T]) CountByCond(ctx context.Context, cond Cond) (int64, error) {
	var count int64
	var entity T
	tableName := entity.TableName()
	query := d.db.WithContext(ctx).Table(tableName)
	query = cond.BuildCondition(query, tableName)
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("generic dao CountByCond: %w", err)
	}
	return count, nil
}

// Update performs a full save of the entity using GORM's Save method.
// The entity must have a valid primary key set.
func (d *GenericDao[T]) Update(ctx context.Context, entity *T) error {
	return d.db.WithContext(ctx).Save(entity).Error
}

// Delete performs a hard delete by primary key.
func (d *GenericDao[T]) Delete(ctx context.Context, id uint) error {
	var entity T
	return d.db.WithContext(ctx).Delete(&entity, id).Error
}
