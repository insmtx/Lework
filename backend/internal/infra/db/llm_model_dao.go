package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/types"
)

const (
	// SystemTranslationLLMModelCode is the built-in LLM model code reserved for fast translation tasks.
	SystemTranslationLLMModelCode = "llm_translation"
)

// CreateLLMModel 创建LLM模型配置
func CreateLLMModel(ctx context.Context, db *gorm.DB, model *types.LLMModel) error {
	baseURLHasV1 := model.BaseURLHasV1
	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	if !baseURLHasV1 {
		if err := db.WithContext(ctx).Model(model).UpdateColumn("base_url_has_v1", false).Error; err != nil {
			return err
		}
		model.BaseURLHasV1 = false
	}
	return nil
}

// GetLLMModelByID 根据ID获取LLM模型配置
func GetLLMModelByID(ctx context.Context, db *gorm.DB, id uint) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetLLMModelByCode 根据组织ID和编码获取LLM模型配置
func GetLLMModelByCode(ctx context.Context, db *gorm.DB, orgID uint, code string) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).Where("org_id = ? AND code = ?", orgID, code).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetDefaultLLMModel 获取组织默认LLM模型配置
func GetDefaultLLMModel(ctx context.Context, db *gorm.DB, orgID uint) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).
		Where("org_id = ? AND is_default = ? AND status = ?", orgID, true, string(types.LLMModelStatusActive)).
		Order("updated_at DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetAnyActiveLLMModel 获取组织内任一处于 active 状态的模型，作为无默认模型时的回退。
func GetAnyActiveLLMModel(ctx context.Context, db *gorm.DB, orgID uint) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).
		Where("org_id = ? AND status = ?", orgID, string(types.LLMModelStatusActive)).
		Order("updated_at DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetSystemTranslationLLMModel 获取系统内置翻译模型配置。
func GetSystemTranslationLLMModel(ctx context.Context, db *gorm.DB, orgID uint) (*types.LLMModel, error) {
	model, err := getSystemTranslationLLMModelByOrg(ctx, db, orgID)
	if err != nil {
		return nil, err
	}
	if model != nil || orgID == 1 {
		return model, nil
	}
	return getSystemTranslationLLMModelByOrg(ctx, db, 1)
}

func getSystemTranslationLLMModelByOrg(ctx context.Context, db *gorm.DB, orgID uint) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).
		Where("org_id = ? AND code = ? AND is_system = ? AND status = ?",
			orgID, SystemTranslationLLMModelCode, true, string(types.LLMModelStatusActive)).
		Order("updated_at DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetActiveLLMModelByName 按组织ID和模型名称查询active状态的模型
// 多个匹配时按 is_default DESC, updated_at DESC 取第一条
func GetActiveLLMModelByName(ctx context.Context, db *gorm.DB, orgID uint, modelName string) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).
		Where("org_id = ? AND model = ? AND status = ?", orgID, modelName, string(types.LLMModelStatusActive)).
		Order("is_default DESC, updated_at DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateLLMModel 更新LLM模型配置
func UpdateLLMModel(ctx context.Context, db *gorm.DB, model *types.LLMModel) error {
	return db.WithContext(ctx).Save(model).Error
}

// DeleteLLMModel 删除LLM模型配置
func DeleteLLMModel(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.LLMModel{}, id).Error
}

// CloneSystemLLMModelsByOrg 将初始化组织的系统模型复制到新组织。
func CloneSystemLLMModelsByOrg(ctx context.Context, d *gorm.DB, fromOrgID, toOrgID uint) error {
	now := time.Now()
	return d.WithContext(ctx).Exec(`
		INSERT INTO `+types.TableNameLLMModel+` (
			org_id, code, name, description, provider, model, base_url,
			base_url_has_v1, api_key_encrypted, api_key_masked,
			max_tokens, temperature, timeout_sec, status, is_default, is_system, config,
			created_at, updated_at
		)
		SELECT ?, code, name, description, provider, model, base_url,
		       base_url_has_v1, api_key_encrypted, api_key_masked,
		       max_tokens, temperature, timeout_sec, status, is_default, is_system, config,
		       ?, ?
		FROM `+types.TableNameLLMModel+`
		WHERE org_id = ? AND is_system = true AND deleted_at IS NULL
		ON CONFLICT (org_id, code) DO NOTHING
	`, toOrgID, now, now, fromOrgID).Error
}

// ListLLMModels 查询LLM模型配置列表
func ListLLMModels(ctx context.Context, db *gorm.DB, opt *types.PageQuery) ([]*types.LLMModel, int64, error) {
	var entities []*types.LLMModel
	var total int64

	query := db.WithContext(ctx).Model(&types.LLMModel{})

	if opt.OrgID > 0 {
		query = query.Where("org_id = ?", opt.OrgID)
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "provider":
			if len(filter.Value) > 0 {
				query = query.Where("provider = ?", filter.Value[0])
			}
		case "status":
			if len(filter.Value) > 0 {
				query = query.Where("status = ?", filter.Value[0])
			}
		case "keyword":
			if len(filter.Value) > 0 {
				kw := filter.Value[0]
				query = query.Where("name LIKE ? OR code LIKE ? OR model LIKE ? OR description LIKE ?",
					"%"+kw+"%", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
			}
		default:
			logs.WarnContextf(ctx, "[llm_model][ListLLMModels] invalid filter field: %s", filter.Field)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		query = query.Order("is_default DESC, created_at DESC")
	}

	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(150)
	}
	query = query.Offset(opt.Offset)

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// LLMModelCodeExists 检查组织内LLM模型编码是否存在（排除指定ID）
func LLMModelCodeExists(ctx context.Context, db *gorm.DB, orgID uint, code string, excludeID uint) (bool, error) {
	var count int64
	query := db.WithContext(ctx).Model(&types.LLMModel{}).Where("org_id = ? AND code = ?", orgID, code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetLLMModelByModelName 按组织ID和模型名称查询 active 状态的模型
func GetLLMModelByModelName(ctx context.Context, db *gorm.DB, orgID uint, modelName string) (*types.LLMModel, error) {
	var entity types.LLMModel
	err := db.WithContext(ctx).
		Where("org_id = ? AND model_name = ? AND status = ?", orgID, modelName, string(types.LLMModelStatusActive)).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ClearOrgDefaultLLMModels 清除指定组织内其他模型配置的默认标记。
// excludeID 大于 0 时排除该 ID 对应的记录。
func ClearOrgDefaultLLMModels(ctx context.Context, db *gorm.DB, orgID uint, excludeID uint) error {
	query := db.WithContext(ctx).Model(&types.LLMModel{}).Where("org_id = ? AND is_default = ?", orgID, true)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	return query.Update("is_default", false).Error
}

// OrgHasLLMModels 检查指定组织是否已有任何模型配置记录。
func OrgHasLLMModels(ctx context.Context, db *gorm.DB, orgID uint) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).Model(&types.LLMModel{}).Where("org_id = ?", orgID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// EnsureOrgSystemLLMModels 确保指定组织拥有系统 LLM 模型。
// 若该组织缺少 is_system=true 的模型，则从 org_id=1 克隆。
// 返回 true 表示执行了克隆操作。
func EnsureOrgSystemLLMModels(ctx context.Context, d *gorm.DB, orgID uint) (bool, error) {
	if orgID == 0 || orgID == 1 {
		return false, nil
	}

	var count int64
	if err := d.WithContext(ctx).Model(&types.LLMModel{}).
		Where("org_id = ? AND is_system = ? AND deleted_at IS NULL", orgID, true).
		Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	if err := CloneSystemLLMModelsByOrg(ctx, d, 1, orgID); err != nil {
		return false, err
	}
	return true, nil
}
