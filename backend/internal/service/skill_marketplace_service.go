package service

import (
	"context"
	"sync"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	"github.com/insmtx/Leros/backend/types"
)

type skillMarketplaceService struct {
	db *gorm.DB
}

// NewSkillMarketplaceService 创建 Skill 市场服务。
func NewSkillMarketplaceService(db *gorm.DB) contract.SkillMarketplaceService {
	return &skillMarketplaceService{db: db}
}

func (s *skillMarketplaceService) SearchSkillMarketplace(ctx context.Context, req *contract.SearchSkillMarketplaceRequest) (*contract.SearchSkillMarketplaceResponse, error) {
	// 规范化分页
	if req.Offset < 0 {
		req.Offset = 0
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	// 决定查询哪些源
	queryBuiltin, queryExternal := s.resolveSources(req.SourceCodes)

	var (
		mu       sync.Mutex
		allItems []contract.SkillMarketplaceItemView
		warnings []contract.SkillSourceWarning
		wg       sync.WaitGroup
	)

	// 内置源：优先排在前面
	if queryBuiltin {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := s.searchBuiltin(ctx, req.Keyword, req.Category, req.Limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, contract.SkillSourceWarning{
					SourceCode: "leros_builtin",
					Message:    err.Error(),
				})
			} else {
				allItems = append(allItems, items...)
			}
		}()
	}

	// 外部源（skills.sh）
	if queryExternal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metas, err := fetch.NewSkillsShSource().Search(ctx, req.Keyword, req.Limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, contract.SkillSourceWarning{
					SourceCode: "skills-sh",
					Message:    err.Error(),
				})
			} else {
				for _, meta := range metas {
					allItems = append(allItems, metaToView(meta))
				}
			}
		}()
	}

	wg.Wait()

	// 分页截取
	total := int64(len(allItems))
	items := applyOffsetLimit(allItems, req.Offset, req.Limit)

	return &contract.SearchSkillMarketplaceResponse{
		Items:    items,
		Total:    total,
		Warnings: warnings,
	}, nil
}

// resolveSources 根据 source_codes 决定查询哪些源。
func (s *skillMarketplaceService) resolveSources(sourceCodes []string) (builtin, external bool) {
	if len(sourceCodes) == 0 {
		return true, true
	}
	for _, code := range sourceCodes {
		switch code {
		case "leros_builtin":
			builtin = true
		case "skills-sh":
			external = true
		}
	}
	return
}

// searchBuiltin 从数据库查询内置 Skill。
func (s *skillMarketplaceService) searchBuiltin(ctx context.Context, keyword, category string, limit int) ([]contract.SkillMarketplaceItemView, error) {
	items, err := infradb.SearchBuiltinSkills(ctx, s.db, keyword, category, limit)
	if err != nil {
		return nil, err
	}

	result := make([]contract.SkillMarketplaceItemView, 0, len(items))
	for _, item := range items {
		result = append(result, builtinItemToView(item))
	}
	return result, nil
}

func builtinItemToView(item types.BuiltinSkillMarketplaceItem) contract.SkillMarketplaceItemView {
	return contract.SkillMarketplaceItemView{
		SourceCode:  "leros_builtin",
		SourceName:  "Leros 内置",
		SourceType:  "builtin",
		SkillID:     item.SkillID,
		Name:        item.Name,
		Description: item.Description,
		Version:     item.Version,
		Author:      item.Author,
		Category:    item.Category,
		Tags:        []string(item.Tags),
		Icon:        item.Icon,
		Installs:    item.Installs,
	}
}

func metaToView(meta fetch.SkillMeta) contract.SkillMarketplaceItemView {
	sourceType := "builtin"
	if meta.Source != "leros_builtin" {
		sourceType = "external"
	}
	return contract.SkillMarketplaceItemView{
		SourceCode:  meta.Source,
		SourceName:  meta.Source,
		SourceType:  sourceType,
		SkillID:     meta.SkillID,
		Name:        meta.Name,
		Description: meta.Description,
		Version:     meta.Version,
		Author:      meta.Author,
		Category:    meta.Category,
		Tags:        meta.Tags,
		Icon:        meta.Icon,
		Installs:    meta.Installs,
	}
}

func applyOffsetLimit(items []contract.SkillMarketplaceItemView, offset, limit int) []contract.SkillMarketplaceItemView {
	if offset >= len(items) {
		return nil
	}
	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

var _ contract.SkillMarketplaceService = (*skillMarketplaceService)(nil)
