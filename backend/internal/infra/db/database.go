// db 包提供 Leros 的数据库初始化和管理功能
//
// 该包负责数据库连接的初始化、表结构的自动迁移，
// 以及提供获取数据库实例的方法。
package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ygpkg/yg-go/dbtools"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
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

var legacyColumns = []legacyColumn{
	{table: types.TableNameDigitalAssistant, column: "config"},
	{table: types.TableNameMessageResource, column: "resource_public_id"},
	{table: types.TableNameMessageResource, column: "resource_code"},
	{table: types.TableNameMemberDepartment, column: "user_org_id"},
	{table: types.TableNameAuthRefreshToken, column: "user_id"},
	{table: types.TableNameAuthRefreshToken, column: "user_org_id"},
}

var renamesToApply = []renameColumn{
	{table: types.TableNameFileUpload, oldCol: "storage_path", newCol: "storage_uri"},
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
		&types.Project{},
		&types.ProjectMember{},
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

	if err := dbtools.InitModel(db, models...); err != nil {
		return err
	}

	if err := backfillUinFromUserOrgID(db); err != nil {
		return err
	}

	if err := backfillMemberDepartmentOrgID(db); err != nil {
		return err
	}

	if err := dropLegacyColumns(db); err != nil {
		return err
	}

	// backfill must run before dropLegacyTables, since profile table is dropped there
	if err := backfillOrganizationProfileFields(db); err != nil {
		return err
	}

	if err := dropLegacyTables(db); err != nil {
		return err
	}

	logs.Info("Database migrations completed")
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
			PublicID:    fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
			GithubID:    0,
			GithubLogin: "admin",
			Name:        "Admin User",
			Email:       "admin@leros.local",
			Password:    string(hashedPassword),
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
		if err := db.Where("github_login = ?", "admin").First(&user).Error; err != nil {
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
		BaseURL:         strings.TrimRight(baseURL, "/"),
		BaseURLHasV1:    true,
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
	if err := d.Where("github_login = ?", "admin").First(&user).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to find default user for worker: %w", err)
		}
		if err := d.Order("id ASC").First(&user).Error; err != nil {
			return fmt.Errorf("failed to find any user for worker: %w", err)
		}
	}

	assistant := &types.DigitalAssistant{}
	code := fmt.Sprintf("default_o%d", org.ID)
	err := d.Where("org_id = ? AND code = ?", org.ID, code).First(assistant).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find default worker assistant: %w", err)
		}
		assistant = &types.DigitalAssistant{
			Code:         code,
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
