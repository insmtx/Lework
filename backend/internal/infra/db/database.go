// db 包提供 Leros 的数据库初始化和管理功能
//
// 该包负责数据库连接的初始化、表结构的自动迁移，
// 以及提供获取数据库实例的方法。
package db

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ygpkg/yg-go/dbtools"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/consts"
	"github.com/insmtx/Leros/backend/types"
)

var legacyTables = []string{
	"leros_artifact",
	"leros_organization_profile",
}

// legacyColumnsToDrop 记录了从模型中被移除但数据库中残留的列。
// GORM AutoMigrate 不会删除列，需要手动清理。
// GORM AutoMigrate 不会重命名列，重命名需要手动迁移。
type legacyColumn struct {
	table  string
	column string
}

// renameColumn 记录需要重命名的列（从旧列名到新列名）
type renameColumn struct {
	table  string
	oldCol string
	newCol string
}

// renameTable 记录需要重命名的表（从旧表名到新表名）
type renameTable struct {
	oldTable string
	newTable string
}

var legacyColumns = []legacyColumn{
	{table: types.TableNameDigitalAssistant, column: "config"},
	{table: types.TableNameMessageResource, column: "resource_public_id"},
	{table: types.TableNameMessageResource, column: "resource_code"},
	{table: types.TableNameMemberDepartment, column: "user_org_id"},
	{table: types.TableNameAuthRefreshToken, column: "user_id"},
	{table: types.TableNameAuthRefreshToken, column: "user_org_id"},
	{table: types.TableNameLLMHistory, column: "caller_ref"},
	{table: types.TableNameLLMHistory, column: "cost"},
	{table: types.TableNameLLMHistory, column: "code"},
	{table: types.TableNameLLMHistory, column: "out_token"},
	{table: types.TableNameLLMHistory, column: "cache_hit_token"},
	{table: types.TableNameLLMHistory, column: "cache_miss_token"},
	{table: types.TableNameLLMHistory, column: "model_config_id"},
	{table: types.TableNameProjectFile, column: "node_type"},
	{table: types.TableNameProjectFile, column: "parent_id"},
	{table: types.TableNameProjectFile, column: "parent_ids"},
	{table: types.TableNameUser, column: "github_id"},
	{table: types.TableNameUser, column: "github_login"},
	{table: types.TableNameUser, column: "bio"},
	{table: types.TableNameUser, column: "company"},
	{table: types.TableNameUser, column: "location"},
	{table: types.TableNameUser, column: "public_repos"},
	{table: types.TableNameUser, column: "followers"},
}

var renamesToApply = []renameColumn{
	{table: types.TableNameDigitalAssistant, oldCol: "code", newCol: "public_id"},
	{table: types.TableNameFileUpload, oldCol: "storage_path", newCol: "storage_uri"},
	{table: types.TableNameLLMHistory, oldCol: "error_message", newCol: "message"},
	{table: types.TableNameLLMHistory, oldCol: "cache_hit_token", newCol: "cache_hit_tokens"},
	{table: types.TableNameLLMHistory, oldCol: "cache_miss_token", newCol: "cache_miss_tokens"},
}

var tablesToRename = []renameTable{
	{oldTable: types.TableNameLLMCallRecord, newTable: types.TableNameLLMHistory},
}

var legacyIndexes = []string{
	"idx_leros_user_org_uin",
	"idx_user_org_uin",
	"idx_leros_project_file_node_type",
	"idx_leros_project_file_parent_id",
	"idx_leros_project_file_file_public_id",
	"idx_project_file_version",
	"idx_project_file_path_version",
}

// dbName 是数据库名称常量
const dbName = "leros"

// InitDB 创建并初始化数据库连接
//
// 使用 dbtools 初始化数据库连接，并根据配置决定是否启用调试模式，
// 最后运行数据库迁移来创建所有必要的表结构。
func InitDB(cfg config.DatabaseConfig, llmCfg *config.LLMConfig) (*gorm.DB, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	db, err := dbtools.InitDBConn(dbName, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}
	db.Logger = newContextGormLogger(logs.Get("gorm"))

	if cfg.Debug {
		db = db.Debug()
	}

	// 运行数据库迁移
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// 初始化开发数据（默认组织、用户、用户组织关联、默认 LLM 模型）
	if err := InitDevData(db, llmCfg); err != nil {
		return nil, fmt.Errorf("failed to init dev data: %w", err)
	}

	logs.Info("Database connection initialized successfully")
	return db, nil
}

// runMigrations 为所有模型创建数据库表
//
// 该函数会自动为所有定义的模型创建或更新数据库表结构。
func runMigrations(db *gorm.DB) error {
	models := []interface{}{
		&types.User{},
		&types.Organization{},
		&types.UserOrg{},
		&types.AuthRefreshToken{},
		&types.AuthLoginAttempt{},
		&types.AuthPhoneVerificationCode{},
		&types.Event{},
		&types.DigitalAssistant{},
		&types.DigitalAssistantPromptBlock{},
		&types.DigitalAssistantMemory{},
		&types.AssistantPromptTrace{},
		&types.AITeammateTemplate{},
		&types.WorkerDeployment{},
		&types.Skill{},
		&types.SkillRegistry{},
		&types.SkillExecutionLog{},
		&types.Session{},
		&types.SessionMessage{},
		&types.LLMModel{},
		&types.LLMHistory{},
		&types.Project{},
		&types.ProjectMember{},
		&types.ProjectActivity{},
		&types.Resource{},
		&types.ResourceBinding{},
		&types.Task{},
		&types.WorkbenchRecentContext{},
		&types.FileUpload{},
		&types.ProjectFile{},
		&types.BuiltinSkillMarketplaceItem{},
		&types.SkillMarketplaceItem{},
		&types.OrgSkillInstallation{},
		&types.MessageResource{},
		&types.Department{},
		&types.MemberDepartment{},
	}

	if err := renameLegacyColumns(db); err != nil {
		return err
	}

	if err := renameLegacyTables(db); err != nil {
		return err
	}

	if err := backfillWorkerDeploymentPublicIDs(db); err != nil {
		return err
	}

	if err := dropLegacyIndexes(db); err != nil {
		return err
	}

	if err := dbtools.InitModel(db, models...); err != nil {
		return err
	}

	if err := backfillSystemLLMModelsForOrgs(db); err != nil {
		return err
	}

	if err := backfillSystemLLMModelsForEnterpriseOrgs(db); err != nil {
		return err
	}

	if err := backfillProjectResourceBindings(db); err != nil {
		return err
	}

	if err := verifyProjectResourceBindings(db); err != nil {
		return err
	}

	if err := backfillTaskResources(db); err != nil {
		return err
	}

	if err := backfillFileArtifactResources(db); err != nil {
		return err
	}

	if err := backfillProjectFileVersions(db); err != nil {
		return err
	}

	// 清理已从模型定义中移除的唯一索引
	if err := db.Exec("DROP INDEX IF EXISTS uni_member_dept").Error; err != nil {
		return err
	}

	if err := backfillUinFromUserOrgID(db); err != nil {
		return err
	}

	if err := backfillMemberDepartmentOrgID(db); err != nil {
		return err
	}

	if err := backfillDefaultDepartments(db); err != nil {
		return err
	}

	if err := backfillMemberDefaultDepartments(db); err != nil {
		return err
	}

	if err := dropLegacyColumns(db); err != nil {
		return err
	}

	// backfill must run before dropLegacyTables, since profile table is dropped there
	if err := backfillOrganizationProfileFields(db); err != nil {
		return err
	}

	if err := backfillBuiltinSkillAuthorBranding(db); err != nil {
		return err
	}

	if err := dropLegacyTables(db); err != nil {
		return err
	}

	logs.Info("Database migrations completed")
	return nil
}

func backfillWorkerDeploymentPublicIDs(db *gorm.DB) error {
	if !db.Migrator().HasTable(&types.WorkerDeployment{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&types.WorkerDeployment{}, "public_id") {
		if err := db.Migrator().AddColumn(&types.WorkerDeployment{}, "PublicID"); err != nil {
			return fmt.Errorf("add worker deployment public_id column: %w", err)
		}
	}

	var deployments []types.WorkerDeployment
	if err := db.Where("public_id = '' OR public_id IS NULL").Find(&deployments).Error; err != nil {
		return fmt.Errorf("list worker deployments missing public_id: %w", err)
	}
	for i := range deployments {
		publicID := fmt.Sprintf("wrk_%s", snowflake.GenerateIDBase58())
		if err := db.Model(&types.WorkerDeployment{}).
			Where("id = ?", deployments[i].ID).
			Update("public_id", publicID).Error; err != nil {
			return fmt.Errorf("backfill worker deployment public_id: %w", err)
		}
	}
	if !db.Migrator().HasIndex(&types.WorkerDeployment{}, "idx_worker_deploy_public_id") {
		if err := db.Migrator().CreateIndex(&types.WorkerDeployment{}, "idx_worker_deploy_public_id"); err != nil {
			return fmt.Errorf("create worker deployment public_id index: %w", err)
		}
	}
	return nil
}

func dropLegacyIndexes(db *gorm.DB) error {
	for _, indexName := range legacyIndexes {
		if err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", indexName)).Error; err != nil {
			logs.Warnf("[migration] drop legacy index %s: %v", indexName, err)
		}
	}
	return nil
}

// backfillOrganizationProfileFields 将 organization_profile 表中的扩展字段回填到 organization 表。
// 仅更新 organization 中对应字段为空的行，保证幂等性。
func backfillOrganizationProfileFields(db *gorm.DB) error {
	if !db.Migrator().HasTable("leros_organization_profile") {
		return nil
	}
	err := db.Exec(`
		UPDATE leros_organization AS o
		SET
			description   = CASE WHEN o.description  = '' AND p.description  != '' THEN p.description  ELSE o.description  END,
			logo          = CASE WHEN o.logo         = '' AND p.logo          != '' THEN p.logo          ELSE o.logo          END,
			address       = CASE WHEN o.address      = '' AND p.address       != '' THEN p.address       ELSE o.address       END,
			website       = CASE WHEN o.website      = '' AND p.website       != '' THEN p.website       ELSE o.website       END,
			created_by_uin = CASE WHEN o.created_by_uin = 0 AND p.uin != 0 THEN p.uin ELSE o.created_by_uin END
		FROM leros_organization_profile AS p
		WHERE p.org_id = o.id AND p.deleted_at IS NULL AND o.deleted_at IS NULL
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillOrganizationProfileFields: %v", err)
	}
	return nil
}

// backfillUinFromUserOrgID 将 user_org_id 列回填为 uin（AuthRefreshToken 与 MemberDepartment）。
func backfillUinFromUserOrgID(db *gorm.DB) error {
	relTable := types.TableNameMemberDepartment
	if db.Migrator().HasTable(relTable) && db.Migrator().HasColumn(relTable, "user_org_id") {
		err := db.Exec(`
			UPDATE leros_rel_user_org_department
			SET uin = (
				SELECT uo.uin FROM leros_user_org uo
				WHERE uo.id = leros_rel_user_org_department.user_org_id
			)
			WHERE user_org_id > 0 AND uin = 0
		`).Error
		if err != nil {
			logs.Warnf("[migration] backfillUinFromUserOrgID rel: %v", err)
		}
	}

	tokenTable := types.TableNameAuthRefreshToken
	if db.Migrator().HasTable(tokenTable) && db.Migrator().HasColumn(tokenTable, "user_org_id") {
		err := db.Exec(`
			UPDATE leros_auth_refresh_token
			SET uin = (
				SELECT uo.uin FROM leros_user_org uo
				WHERE uo.id = leros_auth_refresh_token.user_org_id
			)
			WHERE user_org_id > 0 AND uin = 0
		`).Error
		if err != nil {
			logs.Warnf("[migration] backfillUinFromUserOrgID refresh_token: %v", err)
		}
	}
	return nil
}

// backfillMemberDepartmentOrgID 从 user_org 表将 org_id 回填到 rel_user_org_department。
func backfillMemberDepartmentOrgID(db *gorm.DB) error {
	relTable := types.TableNameMemberDepartment
	if !db.Migrator().HasTable(relTable) {
		return nil
	}
	err := db.Exec(`
		UPDATE leros_rel_user_org_department
		SET org_id = (
			SELECT uo.org_id FROM leros_user_org uo
			WHERE uo.uin = leros_rel_user_org_department.uin
		)
		WHERE org_id = 0 AND uin > 0
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillMemberDepartmentOrgID: %v", err)
	}
	return nil
}

// backfillDefaultDepartments 为没有部门的组织回填默认部门（部门名称为组织名称）。
func backfillDefaultDepartments(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameDepartment) || !d.Migrator().HasTable(types.TableNameOrganization) {
		return nil
	}
	err := d.Exec(`
		INSERT INTO leros_department (name, parent_id, parent_ids, sort, org_id, created_at, updated_at)
		SELECT o.name, 0, 'null', 1000, o.id, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM leros_organization o
		WHERE o.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_department d
			WHERE d.org_id = o.id AND d.parent_id = 0 AND d.deleted_at IS NULL
		  )
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillDefaultDepartments: %v", err)
	}
	return nil
}

// backfillSystemLLMModelsForOrgs 将系统 LLM 模型回填到缺少它们的存量组织。
func backfillSystemLLMModelsForOrgs(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameLLMModel) || !d.Migrator().HasTable(types.TableNameOrganization) {
		return nil
	}
	err := d.Exec(`
		INSERT INTO ` + types.TableNameLLMModel + ` (
			org_id, code, name, description, provider, model, base_url,
			base_url_has_v1, api_key_encrypted, api_key_masked,
			max_tokens, temperature, timeout_sec, status, is_default, is_system, config,
			created_at, updated_at
		)
		SELECT o.id, src.code, src.name, src.description, src.provider, src.model, src.base_url,
		       src.base_url_has_v1, src.api_key_encrypted, src.api_key_masked,
		       src.max_tokens, src.temperature, src.timeout_sec, src.status, src.is_default, src.is_system, src.config,
		       NOW(), NOW()
		FROM ` + types.TableNameOrganization + ` o
		CROSS JOIN ` + types.TableNameLLMModel + ` src
		WHERE src.org_id = 1 AND src.is_system = true AND src.deleted_at IS NULL
		  AND o.deleted_at IS NULL AND o.id != 1
		  AND NOT EXISTS (
		      SELECT 1 FROM ` + types.TableNameLLMModel + ` t
		      WHERE t.org_id = o.id AND t.is_system = true AND t.deleted_at IS NULL
		  )
		ON CONFLICT (org_id, code) DO NOTHING
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillSystemLLMModelsForOrgs: %v", err)
	}
	return nil
}

// backfillSystemLLMModelsForEnterpriseOrgs 将系统 LLM 模型回填到缺少它们的存量企业版组织。
// 企业版组织不写入 leros_organization 表，因此从 leros_digital_assistant 表反查 org_id。
func backfillSystemLLMModelsForEnterpriseOrgs(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameLLMModel) || !d.Migrator().HasTable(types.TableNameDigitalAssistant) {
		return nil
	}
	err := d.Exec(`
		INSERT INTO ` + types.TableNameLLMModel + ` (
			org_id, code, name, description, provider, model, base_url,
			base_url_has_v1, api_key_encrypted, api_key_masked,
			max_tokens, temperature, timeout_sec, status, is_default, is_system, config,
			created_at, updated_at
		)
		SELECT da.org_id, src.code, src.name, src.description, src.provider, src.model, src.base_url,
		       src.base_url_has_v1, src.api_key_encrypted, src.api_key_masked,
		       src.max_tokens, src.temperature, src.timeout_sec, src.status, src.is_default, src.is_system, src.config,
		       NOW(), NOW()
		FROM ` + types.TableNameDigitalAssistant + ` da
		CROSS JOIN ` + types.TableNameLLMModel + ` src
		WHERE src.org_id = 1 AND src.is_system = true AND src.deleted_at IS NULL
		  AND da.deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM ` + types.TableNameOrganization + ` o
		      WHERE o.id = da.org_id AND o.deleted_at IS NULL
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM ` + types.TableNameLLMModel + ` t
		      WHERE t.org_id = da.org_id AND t.is_system = true AND t.deleted_at IS NULL
		  )
		ON CONFLICT (org_id, code) DO NOTHING
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillSystemLLMModelsForEnterpriseOrgs: %v", err)
	}
	return nil
}

// backfillProjectResourceBindings 将存量 project + project_member 回填为统一资源权限模型。
// 步骤：
//  1. 为缺失的 project 创建 leros_resource(type=project)。
//  2. 将用户成员回填为 leros_resource_binding（写 uin 列）。
//  3. 将助手成员回填为 leros_resource_binding（写 assistant_id 列）。
//  4. owner 兜底绑定，防止旧项目 owner 无 project_member 行。
//
// 所有 SQL 均幂等（NOT EXISTS 去重），失败仅告警不阻断启动。
func backfillProjectResourceBindings(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameProject) ||
		!d.Migrator().HasTable(types.TableNameResource) ||
		!d.Migrator().HasTable(types.TableNameResourceBinding) {
		return nil
	}

	// 1. 回填 project 资源
	if err := d.Exec(`
		INSERT INTO leros_resource (org_id, uin, type, biz_id, parent_resource_path_ids, created_at, updated_at)
		SELECT p.org_id, p.owner_id, 'project', p.id, '{}', NOW(), NOW()
		FROM leros_project p
		WHERE p.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource r
			WHERE r.org_id = p.org_id AND r.type = 'project' AND r.biz_id = p.id AND r.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillProjectResourceBindings resources: %v", err)
		return nil
	}

	if !d.Migrator().HasTable(types.TableNameProjectMember) {
		return nil
	}

	// 2. 回填用户绑定
	if err := d.Exec(`
		INSERT INTO leros_resource_binding (org_id, uin, resource_id, resource_role, created_at, updated_at)
		SELECT r.org_id, pm.member_id, r.id,
			CASE pm.member_role WHEN 'owner' THEN 'owner' WHEN 'admin' THEN 'admin' ELSE 'member' END,
			NOW(), NOW()
		FROM leros_project_member pm
		JOIN leros_project p ON p.id = pm.project_id AND p.deleted_at IS NULL
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = p.id AND r.org_id = p.org_id AND r.deleted_at IS NULL
		WHERE pm.deleted_at IS NULL AND pm.member_type = 'user'
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource_binding b
			WHERE b.resource_id = r.id AND b.uin = pm.member_id AND b.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillProjectResourceBindings user bindings: %v", err)
	}

	// 3. 回填助手绑定
	if err := d.Exec(`
		INSERT INTO leros_resource_binding (org_id, assistant_id, resource_id, resource_role, created_at, updated_at)
		SELECT r.org_id, pm.member_id, r.id,
			CASE pm.member_role WHEN 'owner' THEN 'owner' WHEN 'admin' THEN 'admin' ELSE 'member' END,
			NOW(), NOW()
		FROM leros_project_member pm
		JOIN leros_project p ON p.id = pm.project_id AND p.deleted_at IS NULL
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = p.id AND r.org_id = p.org_id AND r.deleted_at IS NULL
		WHERE pm.deleted_at IS NULL AND pm.member_type = 'assistant'
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource_binding b
			WHERE b.resource_id = r.id AND b.assistant_id = pm.member_id AND b.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillProjectResourceBindings assistant bindings: %v", err)
	}

	// 4. owner 兜底绑定
	if err := d.Exec(`
		INSERT INTO leros_resource_binding (org_id, uin, resource_id, resource_role, created_at, updated_at)
		SELECT r.org_id, p.owner_id, r.id, 'owner', NOW(), NOW()
		FROM leros_project p
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = p.id AND r.org_id = p.org_id AND r.deleted_at IS NULL
		WHERE p.deleted_at IS NULL AND p.owner_id > 0
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource_binding b
			WHERE b.resource_id = r.id AND b.uin = p.owner_id AND b.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillProjectResourceBindings owner fallback: %v", err)
	}

	return nil
}

// verifyProjectResourceBindings 在 backfill 后校验项目资源与 owner 绑定是否完整。
// 异常仅告警不阻断启动，便于运维排查存量迁移缺口。
func verifyProjectResourceBindings(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameProject) ||
		!d.Migrator().HasTable(types.TableNameResource) ||
		!d.Migrator().HasTable(types.TableNameResourceBinding) {
		return nil
	}

	type projectGap struct {
		ProjectID uint
	}
	var missingResource []projectGap
	if err := d.Raw(`
		SELECT p.id AS project_id
		FROM leros_project p
		WHERE p.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource r
			WHERE r.org_id = p.org_id AND r.type = 'project' AND r.biz_id = p.id AND r.deleted_at IS NULL
		  )
		ORDER BY p.id
		LIMIT 20
	`).Scan(&missingResource).Error; err != nil {
		logs.Warnf("[migration] verifyProjectResourceBindings missing resource query: %v", err)
		return nil
	}
	if len(missingResource) > 0 {
		sample := make([]uint, 0, len(missingResource))
		for _, g := range missingResource {
			sample = append(sample, g.ProjectID)
		}
		logs.Warnf("[migration] verifyProjectResourceBindings: %d+ projects missing leros_resource, sample project_ids=%v", len(missingResource), sample)
	}

	type ownerGap struct {
		ProjectID uint
	}
	var missingOwner []ownerGap
	if err := d.Raw(`
		SELECT p.id AS project_id
		FROM leros_project p
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = p.id AND r.org_id = p.org_id AND r.deleted_at IS NULL
		WHERE p.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource_binding b
			WHERE b.resource_id = r.id
			  AND b.resource_role = 'owner'
			  AND b.uin IS NOT NULL AND b.uin > 0
			  AND b.deleted_at IS NULL
		  )
		ORDER BY p.id
		LIMIT 20
	`).Scan(&missingOwner).Error; err != nil {
		logs.Warnf("[migration] verifyProjectResourceBindings missing owner query: %v", err)
		return nil
	}
	if len(missingOwner) > 0 {
		sample := make([]uint, 0, len(missingOwner))
		for _, g := range missingOwner {
			sample = append(sample, g.ProjectID)
		}
		logs.Warnf("[migration] verifyProjectResourceBindings: %d+ projects missing owner binding, sample project_ids=%v", len(missingOwner), sample)
	}

	return nil
}

// backfillTaskResources 为存量任务补写 leros_resource(type=task)，挂到父项目资源下。
func backfillTaskResources(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameTask) ||
		!d.Migrator().HasTable(types.TableNameResource) {
		return nil
	}

	if err := d.Exec(`
		INSERT INTO leros_resource (org_id, uin, type, biz_id, parent_resource_id, parent_resource_path_ids, created_at, updated_at)
		SELECT t.org_id, t.owner_id, 'task', t.id, r.id, ARRAY[r.id]::bigint[], NOW(), NOW()
		FROM leros_task t
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = t.project_id AND r.org_id = t.org_id AND r.deleted_at IS NULL
		WHERE t.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource tr
			WHERE tr.org_id = t.org_id AND tr.type = 'task' AND tr.biz_id = t.id AND tr.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillTaskResources: %v", err)
	}

	return nil
}

// backfillFileArtifactResources 为存量 project_file 补写 leros_resource(type=file|artifact)，挂到父项目资源下。
func backfillFileArtifactResources(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameProjectFile) ||
		!d.Migrator().HasTable(types.TableNameResource) ||
		!d.Migrator().HasTable(types.TableNameProject) {
		return nil
	}

	if err := d.Exec(`
		INSERT INTO leros_resource (org_id, uin, type, biz_id, parent_resource_id, parent_resource_path_ids, created_at, updated_at)
		SELECT pf.org_id, COALESCE(NULLIF(pf.uin, 0), p.owner_id), 'file', pf.id, r.id, ARRAY[r.id]::bigint[], NOW(), NOW()
		FROM leros_project_file pf
		JOIN leros_project p ON p.id = pf.project_id AND p.deleted_at IS NULL
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = pf.project_id AND r.org_id = pf.org_id AND r.deleted_at IS NULL
		WHERE pf.deleted_at IS NULL
		  AND pf.resource_type IN ('user_upload', 'plan')
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource fr
			WHERE fr.org_id = pf.org_id AND fr.type = 'file' AND fr.biz_id = pf.id AND fr.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillFileArtifactResources files: %v", err)
	}

	if err := d.Exec(`
		INSERT INTO leros_resource (org_id, uin, type, biz_id, parent_resource_id, parent_resource_path_ids, created_at, updated_at)
		SELECT pf.org_id, COALESCE(NULLIF(pf.uin, 0), p.owner_id), 'artifact', pf.id, r.id, ARRAY[r.id]::bigint[], NOW(), NOW()
		FROM leros_project_file pf
		JOIN leros_project p ON p.id = pf.project_id AND p.deleted_at IS NULL
		JOIN leros_resource r ON r.type = 'project' AND r.biz_id = pf.project_id AND r.org_id = pf.org_id AND r.deleted_at IS NULL
		WHERE pf.deleted_at IS NULL
		  AND pf.resource_type = 'artifact'
		  AND NOT EXISTS (
			SELECT 1 FROM leros_resource ar
			WHERE ar.org_id = pf.org_id AND ar.type = 'artifact' AND ar.biz_id = pf.id AND ar.deleted_at IS NULL
		  )
	`).Error; err != nil {
		logs.Warnf("[migration] backfillFileArtifactResources artifacts: %v", err)
	}

	return nil
}

// backfillMemberDefaultDepartments 将没有部门关系的组织成员挂到默认顶级部门。
func backfillMemberDefaultDepartments(d *gorm.DB) error {
	if !d.Migrator().HasTable(types.TableNameMemberDepartment) ||
		!d.Migrator().HasTable(types.TableNameUserOrg) ||
		!d.Migrator().HasTable(types.TableNameDepartment) {
		return nil
	}
	err := d.Exec(`
		WITH default_departments AS (
			SELECT id, org_id
			FROM (
				SELECT id, org_id, ROW_NUMBER() OVER (PARTITION BY org_id ORDER BY sort ASC, id ASC) AS rn
				FROM leros_department
				WHERE parent_id = 0 AND deleted_at IS NULL
			) ranked_departments
			WHERE rn = 1
		)
		INSERT INTO leros_rel_user_org_department (uin, org_id, department_id, is_primary, created_at, updated_at)
		SELECT uo.uin, uo.org_id, dd.id, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM leros_user_org uo
		JOIN default_departments dd ON dd.org_id = uo.org_id
		WHERE uo.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM leros_rel_user_org_department md
			WHERE md.uin = uo.uin AND md.org_id = uo.org_id AND md.deleted_at IS NULL
		  )
	`).Error
	if err != nil {
		logs.Warnf("[migration] backfillMemberDefaultDepartments: %v", err)
	}
	return nil
}

// backfillBuiltinSkillAuthorBranding 将内置 Skill 市场条目的 author 从旧品牌名 Leros 更新为 Lework。
func backfillBuiltinSkillAuthorBranding(db *gorm.DB) error {
	if !db.Migrator().HasTable(&types.BuiltinSkillMarketplaceItem{}) {
		return nil
	}

	result := db.Exec(
		fmt.Sprintf(`UPDATE %s SET author = ? WHERE author = ? AND deleted_at IS NULL`, types.TableNameBuiltinSkillMarketplaceItem),
		"Lework",
		"Leros",
	)
	if result.Error != nil {
		logs.Warnf("[migration] backfillBuiltinSkillAuthorBranding: %v", result.Error)
		return nil
	}
	if result.RowsAffected > 0 {
		logs.Infof("[migration] backfillBuiltinSkillAuthorBranding: updated %d rows", result.RowsAffected)
	}
	return nil
}

// dropLegacyColumns 清理从模型中被移除的数据库列
func dropLegacyColumns(db *gorm.DB) error {
	for _, lc := range legacyColumns {
		if ok := db.Migrator().HasColumn(lc.table, lc.column); ok {
			logs.Infof("[migration] dropping legacy column %s.%s", lc.table, lc.column)
			if err := db.Migrator().DropColumn(lc.table, lc.column); err != nil {
				logs.Errorf("[migration] failed to drop column %s.%s: %v", lc.table, lc.column, err)
				return err
			}
			logs.Infof("[migration] dropped legacy column %s.%s", lc.table, lc.column)
		}
	}
	return nil
}

// dropLegacyTables 删除已废弃的数据库表
func dropLegacyTables(db *gorm.DB) error {
	for _, tableName := range legacyTables {
		if ok := db.Migrator().HasTable(tableName); ok {
			logs.Infof("[migration] dropping legacy table %s", tableName)
			if err := db.Migrator().DropTable(tableName); err != nil {
				logs.Errorf("[migration] failed to drop table %s: %v", tableName, err)
				return err
			}
			logs.Infof("[migration] dropped legacy table %s", tableName)
		}
	}
	return nil
}

// renameLegacyColumns 重命名已在数据库中但模型字段名变更的列
func renameLegacyColumns(db *gorm.DB) error {
	for _, rc := range renamesToApply {
		hasOld := db.Migrator().HasColumn(rc.table, rc.oldCol)
		hasNew := db.Migrator().HasColumn(rc.table, rc.newCol)
		if hasOld && !hasNew {
			logs.Infof("[migration] renaming column %s.%s -> %s", rc.table, rc.oldCol, rc.newCol)
			if err := db.Migrator().RenameColumn(rc.table, rc.oldCol, rc.newCol); err != nil {
				logs.Errorf("[migration] failed to rename column %s.%s -> %s: %v", rc.table, rc.oldCol, rc.newCol, err)
				return err
			}
			logs.Infof("[migration] renamed column %s.%s -> %s", rc.table, rc.oldCol, rc.newCol)
		}
	}
	return nil
}

// renameLegacyTables 重命名已存在数据库中但模型表名已变更的表
func renameLegacyTables(db *gorm.DB) error {
	for _, rt := range tablesToRename {
		hasOld := db.Migrator().HasTable(rt.oldTable)
		hasNew := db.Migrator().HasTable(rt.newTable)
		if hasOld && !hasNew {
			logs.Infof("[migration] renaming table %s -> %s", rt.oldTable, rt.newTable)
			if err := db.Migrator().RenameTable(rt.oldTable, rt.newTable); err != nil {
				logs.Errorf("[migration] failed to rename table %s -> %s: %v", rt.oldTable, rt.newTable, err)
				return err
			}
			logs.Infof("[migration] renamed table %s -> %s", rt.oldTable, rt.newTable)
		}
	}
	return nil
}

// InitDevData 初始化开发环境数据（仅在数据为空时执行）
// 包括：默认组织、默认用户、用户组织关联、默认 LLM 模型
func InitDevData(db *gorm.DB, llmCfg *config.LLMConfig) error {
	// 初始化默认组织
	var orgCount int64
	db.Model(&types.Organization{}).Count(&orgCount)
	if orgCount == 0 {
		defaultOrg := &types.Organization{
			PublicID: fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
			Code:     "default_org",
			Name:     "默认组织",
			Type:     "company",
			Status:   "active",
		}
		if err := db.Create(defaultOrg).Error; err != nil {
			return fmt.Errorf("failed to create default org: %w", err)
		}
		logs.Info("Default organization created")
	}

	// 初始化默认用户
	var userCount int64
	db.Model(&types.User{}).Count(&userCount)
	if userCount == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("Admin123456"), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		defaultUser := &types.User{
			PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
			Name:     "Admin User",
			Email:    "admin@leros.local",
			Password: string(hashedPassword),
		}
		if err := db.Create(defaultUser).Error; err != nil {
			return fmt.Errorf("failed to create default user: %w", err)
		}
		logs.Info("Default user created (login: admin)")
	}

	// 初始化用户组织关联
	var userOrgCount int64
	db.Model(&types.UserOrg{}).Count(&userOrgCount)
	if userOrgCount == 0 {
		var user types.User
		var org types.Organization
		if err := db.Where("email = ?", "admin@leros.local").First(&user).Error; err != nil {
			return fmt.Errorf("failed to find default user: %w", err)
		}
		if err := db.Where("code = ?", "default_org").First(&org).Error; err != nil {
			return fmt.Errorf("failed to find default org: %w", err)
		}

		userOrg := &types.UserOrg{
			Uin:       user.ID,
			UserID:    user.ID,
			OrgID:     org.ID,
			IsDefault: true,
		}
		if err := db.Create(userOrg).Error; err != nil {
			return fmt.Errorf("failed to create default user-org: %w", err)
		}
		logs.Infof("Default user-org association created (uin=%d, user_id=%d, org_id=%d)", userOrg.Uin, userOrg.UserID, userOrg.OrgID)
	}

	if err := seedDefaultWorkerDeployment(db); err != nil {
		return err
	}

	// 初始化默认 LLM 模型（仅在表为空且配置中提供 LLM 配置时执行）
	var modelCount int64
	db.Model(&types.LLMModel{}).Count(&modelCount)
	if modelCount == 0 && llmCfg != nil && llmCfg.APIKey != "" {
		modelName := llmCfg.Model
		if modelName == "" {
			modelName = "default"
		}

		defaultLLMModel := &types.LLMModel{
			OrgID:           1,
			Code:            "llm_default",
			Name:            llmCfg.Provider,
			Description:     "Default LLM model from config",
			Provider:        llmCfg.Provider,
			ModelName:       modelName,
			BaseURL:         llmCfg.BaseURL,
			APIKeyEncrypted: llmCfg.APIKey,
			APIKeyMasked:    maskAPIKey(llmCfg.APIKey),
			MaxTokens:       4096,
			Temperature:     0.7,
			TimeoutSec:      120,
			Status:          string(types.LLMModelStatusActive),
			IsDefault:       true,
			IsSystem:        true,
		}
		if err := db.Create(defaultLLMModel).Error; err != nil {
			return fmt.Errorf("failed to create default LLM model: %w", err)
		}
		logs.Infof("Default LLM model created (provider=%s, model=%s)", llmCfg.Provider, modelName)
	}

	if err := seedSystemLLMModels(db, llmCfg); err != nil {
		return err
	}

	// 初始化内置 Skill 市场条目（从 backend/skills/server/ 下的 SKILL.md 解析）
	if err := SeedBuiltinSkillMarketplace(db); err != nil {
		return fmt.Errorf("failed to seed builtin skill marketplace: %w", err)
	}

	return nil
}

func seedSystemLLMModels(d *gorm.DB, llmCfg *config.LLMConfig) error {
	spec, ok := buildSystemTranslationLLMModelSpec(llmCfg)
	if !ok {
		logs.Warn("System translation LLM model skipped: no api_key configured")
		return nil
	}

	var existing types.LLMModel
	err := d.Where("org_id = ? AND code = ?", spec.OrgID, spec.Code).First(&existing).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find system translation LLM model: %w", err)
		}
		if err := d.Create(spec).Error; err != nil {
			return fmt.Errorf("create system translation LLM model: %w", err)
		}
		logs.Infof("System translation LLM model created (provider=%s, model=%s)", spec.Provider, spec.ModelName)
		return nil
	}

	if !existing.IsSystem {
		logs.Warnf("System translation LLM model skipped: code %q is occupied by non-system model", spec.Code)
		return nil
	}

	logs.Infof("System translation LLM model already exists, skip initialization (provider=%s, model=%s)", existing.Provider, existing.ModelName)
	return nil
}

func buildSystemTranslationLLMModelSpec(llmCfg *config.LLMConfig) (*types.LLMModel, bool) {
	if llmCfg == nil {
		return nil, false
	}

	provider := strings.TrimSpace(string(types.LLMProviderDeepSeek))
	modelName := "deepseek-v4-flash"
	baseURL := strings.TrimSpace(llmCfg.BaseURL)
	apiKey := strings.TrimSpace(llmCfg.APIKey)

	if llmCfg.Translation != nil {
		if v := strings.TrimSpace(llmCfg.Translation.Provider); v != "" {
			provider = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.Model); v != "" {
			modelName = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.BaseURL); v != "" {
			baseURL = v
		}
		if v := strings.TrimSpace(llmCfg.Translation.APIKey); v != "" {
			apiKey = v
		}
	}

	baseURL = strings.TrimRight(baseURL, "/")
	baseURLHasV1 := strings.HasSuffix(baseURL, "/v1")
	if baseURLHasV1 {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}

	if apiKey == "" {
		return nil, false
	}

	return &types.LLMModel{
		OrgID:           1,
		Code:            SystemTranslationLLMModelCode,
		Name:            "内置翻译模型",
		Description:     "用于 Skill 描述和文档翻译的快速系统模型",
		Provider:        provider,
		ModelName:       modelName,
		BaseURL:         baseURL,
		BaseURLHasV1:    baseURLHasV1,
		APIKeyEncrypted: apiKey,
		APIKeyMasked:    maskAPIKey(apiKey),
		MaxTokens:       4096,
		Temperature:     0.1,
		TimeoutSec:      60,
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       false,
		IsSystem:        true,
		Config: types.LLMModelConfig{
			"purpose": "translation",
		},
	}, true
}

func seedDefaultWorkerDeployment(d *gorm.DB) error {
	var org types.Organization
	if err := d.Where("code = ?", "default_org").First(&org).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to find default org for worker: %w", err)
		}
		if err := d.Order("id ASC").First(&org).Error; err != nil {
			return fmt.Errorf("failed to find any org for worker: %w", err)
		}
	}

	var user types.User
	if err := d.Where("email = ?", "admin@leros.local").First(&user).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to find default user for worker: %w", err)
		}
		if err := d.Order("id ASC").First(&user).Error; err != nil {
			return fmt.Errorf("failed to find any user for worker: %w", err)
		}
	}

	assistant := &types.DigitalAssistant{}
	code := fmt.Sprintf("%so%d", types.DefaultDigitalAssistantPublicIDPrefix, org.ID)
	err := d.Where("org_id = ? AND public_id = ?", org.ID, code).First(assistant).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find default worker assistant: %w", err)
		}
		assistant = &types.DigitalAssistant{
			PublicID:     code,
			OrgID:        org.ID,
			OwnerID:      user.ID,
			Name:         "lework",
			Description:  "你工作和生活中的 AI 队友",
			Status:       "active",
			SystemPrompt: "你的名称是 lework。你是用户工作和生活中的 AI 队友，让工作，乐起来。用户询问你是谁、你能做什么时，请按 lework 的身份回答，不要称自己为默认数字员工。",
		}
		if err := d.Create(assistant).Error; err != nil {
			return fmt.Errorf("create default worker assistant: %w", err)
		}
	}

	var existingDeployment types.WorkerDeployment
	err = d.Where("org_id = ? AND worker_id = ?", org.ID, 1).First(&existingDeployment).Error
	if err == nil {
		if existingDeployment.DigitalAssistantID != assistant.ID {
			existingDeployment.DigitalAssistantID = assistant.ID
			if err := d.Save(&existingDeployment).Error; err != nil {
				return fmt.Errorf("rebind default worker deployment: %w", err)
			}
			logs.Infof("Default worker deployment rebound to %s (org_id=%d, worker_id=1)", code, org.ID)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find default worker deployment: %w", err)
	}

	deployment := &types.WorkerDeployment{
		OrgID:              org.ID,
		DigitalAssistantID: assistant.ID,
		WorkerID:           1,
		DeploymentName:     fmt.Sprintf("leros-worker-o%d-w%d", org.ID, 1),
		Namespace:          "default",
		Status:             string(types.WorkerDeploymentStatusPending),
		WorkspacePath:      "/data/workspace",
	}
	if err := d.Create(deployment).Error; err != nil {
		return fmt.Errorf("create default worker deployment: %w", err)
	}
	logs.Infof("Default worker deployment created (org_id=%d, worker_id=1)", org.ID)
	return nil
}

// GetDB 获取默认的数据库实例
func GetDB() *gorm.DB {
	return dbtools.DB(dbName)
}

// maskAPIKey 将 API Key 脱敏显示
func maskAPIKey(key string) string {
	if len(key) <= 7 {
		return "***"
	}
	return key[:3] + "***" + key[len(key)-4:]
}

type projectFileVersionGroup struct {
	initialFilePublicID string
	versionNo           int
}

func backfillProjectFileVersions(db *gorm.DB) error {
	if !db.Migrator().HasTable(&types.ProjectFile{}) {
		return nil
	}

	var projectFiles []types.ProjectFile
	if err := db.Unscoped().Order("created_at ASC, id ASC").Find(&projectFiles).Error; err != nil {
		return fmt.Errorf("list project files for version backfill: %w", err)
	}
	if len(projectFiles) == 0 {
		return createProjectFileVersionIndex(db)
	}

	var uploads []types.FileUpload
	if err := db.Unscoped().Find(&uploads).Error; err != nil {
		return fmt.Errorf("list file uploads for project file version backfill: %w", err)
	}
	uploadsByID := make(map[uint]types.FileUpload, len(uploads))
	for i := range uploads {
		uploadsByID[uploads[i].ID] = uploads[i]
	}

	groups := make(map[string]projectFileVersionGroup)
	err := db.Transaction(func(tx *gorm.DB) error {
		for i := range projectFiles {
			file := &projectFiles[i]
			relativePath := strings.TrimSpace(file.RelativePath)
			if relativePath == "" {
				upload := uploadsByID[file.ResourceID]
				name := strings.TrimSpace(upload.OriginalName)
				if name == "" {
					name = strings.TrimSpace(upload.Filename)
				}
				if name == "" {
					name = file.FilePublicID
				}
				relativePath = projectFileBackfillPrefix(file.ResourceType) + filepath.Base(name)
			}
			relativePath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))

			groupKey := fmt.Sprintf("%d\x00%d\x00%s\x00%s", file.OrgID, file.ProjectID, file.ResourceType, relativePath)
			group, exists := groups[groupKey]
			if !exists {
				group.initialFilePublicID = file.FilePublicID
			}
			group.versionNo++
			groups[groupKey] = group

			updates := map[string]interface{}{
				"relative_path":          relativePath,
				"initial_file_public_id": group.initialFilePublicID,
				"version_no":             group.versionNo,
			}
			if err := tx.Unscoped().Model(&types.ProjectFile{}).Where("id = ?", file.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("backfill project file %d version fields: %w", file.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return createProjectFileVersionIndex(db)
}

func projectFileBackfillPrefix(resourceType types.ProjectFileResourceType) string {
	switch resourceType {
	case types.ProjectFileResourceTypeArtifact:
		return consts.RepoDirArtifacts + "/"
	case types.ProjectFileResourceTypeUserUpload:
		return consts.RepoDirUploads + "/"
	case types.ProjectFileResourceTypePlan:
		return consts.RepoDirPlans + "/"
	default:
		return "files/"
	}
}

func createProjectFileVersionIndex(db *gorm.DB) error {
	statements := []string{
		fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS idx_project_file_version ON %s (org_id, project_id, initial_file_public_id, version_no)",
			types.TableNameProjectFile,
		),
		fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS idx_project_file_path_version ON %s (org_id, project_id, resource_type, relative_path, version_no)",
			types.TableNameProjectFile,
		),
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("create project file version index: %w", err)
		}
	}
	return nil
}
