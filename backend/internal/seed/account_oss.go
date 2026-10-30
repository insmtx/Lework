package seed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// seedAccount 初始化默认组织、默认管理员、用户-组织关联与默认 worker 部署。幂等。
func seedAccount(ctx context.Context, db *gorm.DB) error {
	if err := seedDefaultOrganization(ctx, db); err != nil {
		return err
	}
	if err := seedDefaultUser(ctx, db); err != nil {
		return err
	}
	if err := seedDefaultUserOrg(ctx, db); err != nil {
		return err
	}
	if err := seedDefaultWorkerDeployment(ctx, db); err != nil {
		return err
	}
	return nil
}

var defaultOrgCode = "default_org"

const (
	defaultAdminEmail    = "admin@leros.local"
	defaultAdminPassword = "Admin123456"
)

// adminCredentials 返回首次启动时创建管理员的凭据。
//
// 生产环境必须显式提供 LEROS_ADMIN_PASSWORD，避免一体机/私有化部署带着
// 公开的默认密码上线。本地开发保留默认值，方便 make dev-server 直接登录。
func adminCredentials() (email, password string) {
	email = strings.TrimSpace(os.Getenv("LEROS_ADMIN_EMAIL"))
	if email == "" {
		email = defaultAdminEmail
	}
	password = os.Getenv("LEROS_ADMIN_PASSWORD")
	if password == "" && !strings.EqualFold(strings.TrimSpace(os.Getenv("LEROS_ENV")), "production") {
		password = defaultAdminPassword
	}
	return email, password
}

func seedDefaultOrganization(ctx context.Context, db *gorm.DB) error {
	var orgCount int64
	if err := db.WithContext(ctx).Model(&types.Organization{}).Count(&orgCount).Error; err != nil {
		return err
	}
	if orgCount != 0 {
		return nil
	}
	defaultOrg := &types.Organization{
		PublicID: fmt.Sprintf("org_%s", snowflake.GenerateIDBase58()),
		Code:     defaultOrgCode,
		Name:     "默认组织",
		Type:     "company",
		Status:   "active",
	}
	if err := db.WithContext(ctx).Create(defaultOrg).Error; err != nil {
		return err
	}
	logs.Info("seed: default organization created")
	return nil
}

func seedDefaultUser(ctx context.Context, db *gorm.DB) error {
	var userCount int64
	if err := db.WithContext(ctx).Model(&types.User{}).Count(&userCount).Error; err != nil {
		return err
	}
	if userCount != 0 {
		return nil
	}
	email, password := adminCredentials()
	if password == "" {
		return errors.New("LEROS_ADMIN_PASSWORD is required when LEROS_ENV=production")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	defaultUser := &types.User{
		PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
		Name:     "Admin User",
		Email:    email,
		Password: string(hashedPassword),
	}
	if err := db.WithContext(ctx).Create(defaultUser).Error; err != nil {
		return err
	}
	logs.Infof("seed: default user created (email=%s)", email)
	return nil
}

func seedDefaultUserOrg(ctx context.Context, db *gorm.DB) error {
	var uoCount int64
	if err := db.WithContext(ctx).Model(&types.UserOrg{}).Count(&uoCount).Error; err != nil {
		return err
	}
	if uoCount != 0 {
		return nil
	}
	email, _ := adminCredentials()
	var user types.User
	if err := db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return err
	}
	var org types.Organization
	if err := db.WithContext(ctx).Where("code = ?", defaultOrgCode).First(&org).Error; err != nil {
		return err
	}
	userOrg := &types.UserOrg{
		UserID:    user.ID,
		OrgID:     org.ID,
		IsDefault: true,
	}
	if err := db.WithContext(ctx).Create(userOrg).Error; err != nil {
		return err
	}
	logs.Infof("seed: default user-org association created (user_id=%d, org_id=%d)", user.ID, org.ID)
	return nil
}

// seedDefaultWorkerDeployment 创建默认 lework 助手与其默认 worker 部署（worker_id=1）。
func seedDefaultWorkerDeployment(ctx context.Context, db *gorm.DB) error {
	var org types.Organization
	if err := db.WithContext(ctx).Where("code = ?", defaultOrgCode).First(&org).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := db.WithContext(ctx).Order("id ASC").First(&org).Error; err != nil {
			return err
		}
	}

	email, _ := adminCredentials()
	var user types.User
	if err := db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := db.WithContext(ctx).Order("id ASC").First(&user).Error; err != nil {
			return err
		}
	}

	assistant := &types.DigitalAssistant{}
	code := fmt.Sprintf("%so%d", types.DefaultDigitalAssistantPublicIDPrefix, org.ID)
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", org.ID, code).First(assistant).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
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
		if err := db.WithContext(ctx).Create(assistant).Error; err != nil {
			return err
		}
	}

	var existingDeployment types.WorkerDeployment
	err = db.WithContext(ctx).Where("org_id = ? AND worker_id = ?", org.ID, 1).First(&existingDeployment).Error
	if err == nil {
		if existingDeployment.DigitalAssistantID != assistant.ID {
			existingDeployment.DigitalAssistantID = assistant.ID
			if err := db.WithContext(ctx).Save(&existingDeployment).Error; err != nil {
				return err
			}
			logs.Infof("seed: default worker deployment rebound (org_id=%d, worker_id=1)", org.ID)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
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
	if err := db.WithContext(ctx).Create(deployment).Error; err != nil {
		return err
	}
	logs.Infof("seed: default worker deployment created (org_id=%d, worker_id=1)", org.ID)
	return nil
}
