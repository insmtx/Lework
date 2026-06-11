// db 包提供 Leros 的数据库初始化和管理功能
//
// 该包负责数据库连接的初始化、表结构的自动迁移，
// 以及提供获取数据库实例的方法。
package db

import (
	"fmt"

	"github.com/ygpkg/yg-go/dbtools"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/types"
)

// legacyColumnsToDrop 记录了从模型中被移除但数据库中残留的列。
// GORM AutoMigrate 不会删除列，需要手动清理。
type legacyColumn struct {
	table  string
	column string
}

var legacyColumns = []legacyColumn{
	{table: types.TableNameDigitalAssistant, column: "config"},
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
		&types.Event{},
		&types.DigitalAssistant{},
		&types.Skill{},
		&types.SkillRegistry{},
		&types.SkillExecutionLog{},
		&types.Session{},
		&types.SessionMessage{},
		&types.LLMModel{},
		&types.Project{},
		&types.ProjectMember{},
		&types.Task{},
		&types.Artifact{},
		&types.BuiltinSkillMarketplaceItem{},
	}

	if err := dbtools.InitModel(db, models...); err != nil {
		return err
	}

	if err := dropLegacyColumns(db); err != nil {
		return err
	}

	logs.Info("Database migrations completed")
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

	// 初始化内置 Skill 市场条目（从 backend/skills/server/ 下的 SKILL.md 解析）
	if err := SeedBuiltinSkillMarketplace(db); err != nil {
		return fmt.Errorf("failed to seed builtin skill marketplace: %w", err)
	}

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
