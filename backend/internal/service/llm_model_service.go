package service

import (
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/llm"
)

// NewLLMModelService 创建 LLM 模型配置服务，委托到 llm.Manager。
// orgCreatorCheck 提供组织创建者权限判定，来自 account 层（oss/enterprise 各自实现）。
func NewLLMModelService(db *gorm.DB, orgCreatorCheck account.OrgRepository) contract.LLMModelService {
	return llm.NewContractAdapter(llm.NewManager(db), orgCreatorCheck)
}
