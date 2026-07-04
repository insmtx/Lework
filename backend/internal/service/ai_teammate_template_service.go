package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.AITeammateTemplateService = (*aiTeammateTemplateService)(nil)

type aiTeammateTemplateService struct {
	db *gorm.DB
}

// NewAITeammateTemplateService creates an AI teammate template service.
func NewAITeammateTemplateService(db *gorm.DB) contract.AITeammateTemplateService {
	return &aiTeammateTemplateService{db: db}
}

func (s *aiTeammateTemplateService) ListAITeammateTemplates(ctx context.Context, req *contract.ListAITeammateTemplateRequest) (*contract.AITeammateTemplateList, error) {
	req.Pagination.Fill()

	opt := types.NewPageQuery(types.Caller{}, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.Category != nil && *req.Category != "" {
		opt.AddFilter("category", *req.Category)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	} else {
		opt.AddFilter("status", string(contract.AITeammateTemplateStatusActive))
	}

	entities, total, err := infradb.ListAITeammateTemplates(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.AITeammateTemplate, 0, len(entities))
	for _, entity := range entities {
		items = append(items, *convertToContractAITeammateTemplate(entity))
	}

	return &contract.AITeammateTemplateList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *aiTeammateTemplateService) GetAITeammateTemplate(ctx context.Context, req *contract.GetAITeammateTemplateRequest) (*contract.AITeammateTemplate, error) {
	entity, err := s.resolveTemplate(ctx, req.ID, req.Code)
	if err != nil {
		return nil, err
	}
	return convertToContractAITeammateTemplate(entity), nil
}

func (s *aiTeammateTemplateService) IncrementAITeammateTemplateUseCount(ctx context.Context, req *contract.IncrementAITeammateTemplateCountRequest) (*contract.AITeammateTemplate, error) {
	entity, err := s.resolveTemplate(ctx, req.ID, req.Code)
	if err != nil {
		return nil, err
	}
	if err := infradb.IncrementAITeammateTemplateUseCount(ctx, s.db, entity.ID); err != nil {
		return nil, err
	}
	entity.UseCount++
	return convertToContractAITeammateTemplate(entity), nil
}

func (s *aiTeammateTemplateService) IncrementAITeammateTemplateRecommendCount(ctx context.Context, req *contract.IncrementAITeammateTemplateCountRequest) (*contract.AITeammateTemplate, error) {
	entity, err := s.resolveTemplate(ctx, req.ID, req.Code)
	if err != nil {
		return nil, err
	}
	if err := infradb.IncrementAITeammateTemplateRecommendCount(ctx, s.db, entity.ID); err != nil {
		return nil, err
	}
	entity.RecommendCount++
	return convertToContractAITeammateTemplate(entity), nil
}

func (s *aiTeammateTemplateService) resolveTemplate(ctx context.Context, id *uint, code *string) (*types.AITeammateTemplate, error) {
	var entity *types.AITeammateTemplate
	var err error
	if id != nil && *id > 0 {
		entity, err = infradb.GetAITeammateTemplateByID(ctx, s.db, *id)
	} else if code != nil && *code != "" {
		entity, err = infradb.GetAITeammateTemplateByCode(ctx, s.db, *code)
	} else {
		return nil, errors.New("id or code is required")
	}
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New("ai teammate template not found")
	}
	return entity, nil
}

func convertToContractAITeammateTemplate(item *types.AITeammateTemplate) *contract.AITeammateTemplate {
	if item == nil {
		return nil
	}
	return &contract.AITeammateTemplate{
		ID:             item.ID,
		Code:           item.Code,
		Name:           item.Name,
		Description:    item.Description,
		Avatar:         item.Avatar,
		Provider:       item.Provider,
		SystemPrompt:   item.SystemPrompt,
		Expertise:      []string(item.Expertise),
		Category:       item.Category,
		Tags:           []string(item.Tags),
		SortOrder:      item.SortOrder,
		UseCount:       item.UseCount,
		RecommendCount: item.RecommendCount,
		Status:         item.Status,
		IsSystem:       item.IsSystem,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}
