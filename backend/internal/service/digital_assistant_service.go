package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/types"
)

var _ contract.DigitalAssistantService = (*digitalAssistantService)(nil)

const maxDigitalAssistantsPerUser int64 = 5

type digitalAssistantService struct {
	db                 *gorm.DB
	workerScheduler    worker.WorkerScheduler
	workerProvisioning *WorkerProvisioningService
}

func NewDigitalAssistantService(db *gorm.DB, workerScheduler worker.WorkerScheduler) contract.DigitalAssistantService {
	return &digitalAssistantService{
		db:              db,
		workerScheduler: workerScheduler,
	}
}

func NewDigitalAssistantServiceWithProvisioning(db *gorm.DB, workerScheduler worker.WorkerScheduler, provisioning *WorkerProvisioningService) contract.DigitalAssistantService {
	return &digitalAssistantService{
		db:                 db,
		workerScheduler:    workerScheduler,
		workerProvisioning: provisioning,
	}
}

func (s *digitalAssistantService) CreateDigitalAssistant(ctx context.Context, req *contract.CreateDigitalAssistantRequest) (*contract.DigitalAssistant, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return nil, errors.New("user not authenticated or org not set")
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if req.Code == "" {
		req.Code = generateAssistantCode()
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}

	count, err := db.CountDigitalAssistantsByOwner(ctx, s.db, caller.OrgID, caller.Uin, defaultWorkerCode(caller.OrgID))
	if err != nil {
		return nil, err
	}
	if count >= maxDigitalAssistantsPerUser {
		return nil, fmt.Errorf("digital assistant limit exceeded: max %d per user", maxDigitalAssistantsPerUser)
	}

	if s.workerProvisioning != nil {
		if _, err := s.workerProvisioning.EnsureDefaultWorkerForOrg(ctx, caller.OrgID, caller.Uin); err != nil {
			return nil, fmt.Errorf("ensure default worker deployment: %w", err)
		}
	}

	exists, err := db.DigitalAssistantCodeExists(ctx, s.db, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("digital assistant with this code already exists")
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "custom"
	}
	expertise := normalizeExpertise(req.Expertise)
	if len(expertise) == 0 {
		expertise = inferAssistantExpertise(req.Name, req.Description, req.SystemPrompt)
	}

	da := &types.DigitalAssistant{
		Code:         req.Code,
		OrgID:        caller.OrgID,
		OwnerID:      caller.Uin,
		Name:         req.Name,
		Description:  req.Description,
		Avatar:       req.Avatar,
		Status:       string(contract.DigitalAssistantStatusDraft),
		Version:      0,
		SystemPrompt: req.SystemPrompt,
		Expertise:    types.SkillStringList(expertise),
		TemplateID:   req.TemplateID,
		Source:       source,
	}

	if err := db.CreateDigitalAssistant(ctx, s.db, da); err != nil {
		return nil, err
	}

	if s.workerProvisioning != nil {
		if _, err := s.workerProvisioning.EnsureForAssistant(ctx, da); err != nil {
			return nil, fmt.Errorf("ensure worker deployment: %w", err)
		}
	}

	return s.convertToContractDigitalAssistant(ctx, da), nil
}

func (s *digitalAssistantService) GetDigitalAssistantByID(ctx context.Context, id uint) (*contract.DigitalAssistantDetail, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	da, err := db.GetDigitalAssistantByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if da == nil {
		return nil, errors.New("digital assistant not found")
	}

	if err := verifyOrgPermission(da.OrgID, caller.OrgID); err != nil {
		return nil, err
	}
	if err := verifyUserPermission(da.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	return &contract.DigitalAssistantDetail{
		DigitalAssistant: *s.convertToContractDigitalAssistant(ctx, da),
	}, nil
}

func (s *digitalAssistantService) GetDigitalAssistantByCode(ctx context.Context, code string) (*contract.DigitalAssistantDetail, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	da, err := db.GetDigitalAssistantByCode(ctx, s.db, code)
	if err != nil {
		return nil, err
	}
	if da == nil {
		return nil, errors.New("digital assistant not found")
	}

	if err := verifyOrgPermission(da.OrgID, caller.OrgID); err != nil {
		return nil, err
	}
	if err := verifyUserPermission(da.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	return &contract.DigitalAssistantDetail{
		DigitalAssistant: *s.convertToContractDigitalAssistant(ctx, da),
	}, nil
}

func (s *digitalAssistantService) UpdateDigitalAssistant(ctx context.Context, id uint, req *contract.UpdateDigitalAssistantRequest) (*contract.DigitalAssistant, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	da, err := db.GetDigitalAssistantByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if da == nil {
		return nil, errors.New("digital assistant not found")
	}

	if err := verifyOrgPermission(da.OrgID, caller.OrgID); err != nil {
		return nil, err
	}
	if err := verifyUserPermission(da.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	if req.Name != "" {
		da.Name = req.Name
	}
	if req.Description != "" {
		da.Description = req.Description
	}
	if req.Avatar != "" {
		da.Avatar = req.Avatar
	}
	if req.SystemPrompt != nil {
		da.SystemPrompt = *req.SystemPrompt
	}
	if req.Expertise != nil {
		da.Expertise = types.SkillStringList(normalizeExpertise(*req.Expertise))
	} else if len(da.Expertise) == 0 {
		da.Expertise = types.SkillStringList(inferAssistantExpertise(da.Name, da.Description, da.SystemPrompt))
	}
	da.UpdatedAt = time.Now()

	if err := db.UpdateDigitalAssistant(ctx, s.db, da); err != nil {
		return nil, err
	}

	return s.convertToContractDigitalAssistant(ctx, da), nil
}

func (s *digitalAssistantService) DeleteDigitalAssistant(ctx context.Context, id uint) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}

	da, err := db.GetDigitalAssistantByID(ctx, s.db, id)
	if err != nil {
		return err
	}
	if da == nil {
		return errors.New("digital assistant not found")
	}

	if err := verifyOrgPermission(da.OrgID, caller.OrgID); err != nil {
		return err
	}
	if err := verifyUserPermission(da.OwnerID, caller.Uin); err != nil {
		return err
	}

	return db.DeleteDigitalAssistant(ctx, s.db, id)
}

func (s *digitalAssistantService) ListDigitalAssistant(ctx context.Context, req *contract.ListDigitalAssistantRequest) (*contract.DigitalAssistantList, error) {
	caller, err := getCallerFromContext(ctx)
	if err != nil {
		return nil, err
	}

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	if req.Status != nil {
		opt.AddFilter("status", *req.Status)
	}
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.Source != nil && *req.Source != "" {
		opt.AddFilter("source", *req.Source)
	}

	entities, total, err := db.ListDigitalAssistant(ctx, s.db, opt)

	if err != nil {
		return nil, err
	}

	items := make([]contract.DigitalAssistant, 0, len(entities))
	for _, entity := range entities {
		items = append(items, *s.convertToContractDigitalAssistant(ctx, entity))
	}

	return &contract.DigitalAssistantList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *digitalAssistantService) UpdateDigitalAssistantStatus(ctx context.Context, id uint, req *contract.UpdateDigitalAssistantStatusRequest) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}

	da, err := db.GetDigitalAssistantByID(ctx, s.db, id)
	if err != nil {
		return err
	}
	if da == nil {
		return errors.New("digital assistant not found")
	}

	if err := verifyOrgPermission(da.OrgID, caller.OrgID); err != nil {
		return err
	}
	if err := verifyUserPermission(da.OwnerID, caller.Uin); err != nil {
		return err
	}

	da.Status = req.Status
	da.UpdatedAt = time.Now()

	if err := db.UpdateDigitalAssistant(ctx, s.db, da); err != nil {
		return err
	}

	if s.workerProvisioning != nil {
		switch da.Status {
		case string(contract.DigitalAssistantStatusActive):
			if err := s.markAssistantActive(ctx, da); err != nil {
				return err
			}
		case string(contract.DigitalAssistantStatusInactive), string(contract.DigitalAssistantStatusArchived), string(contract.DigitalAssistantStatusDraft):
			if err := s.workerProvisioning.MarkAssistantStopped(ctx, da); err != nil {
				return fmt.Errorf("mark worker deployment stopped: %w", err)
			}
			if s.workerScheduler != nil {
				if deployment, err := db.GetWorkerDeploymentByAssistantID(ctx, s.db, da.ID); err != nil {
					return err
				} else if deployment != nil {
					if err := s.workerScheduler.Stop(ctx, deployment.DeploymentName); err != nil {
						return fmt.Errorf("stop worker deployment: %w", err)
					}
				}
			}
		}
	}
	return nil
}

func (s *digitalAssistantService) CreateDigitalAssistantFromTemplate(ctx context.Context, req *contract.CreateDigitalAssistantFromTemplateRequest) (*contract.DigitalAssistant, error) {
	if _, err := requireCallerOrg(ctx); err != nil {
		return nil, err
	}

	tpl, err := db.GetAITeammateTemplateByID(ctx, s.db, req.TemplateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil {
		return nil, errors.New("ai teammate template not found")
	}
	if tpl.Status != string(contract.AITeammateTemplateStatusActive) {
		return nil, errors.New("ai teammate template is inactive")
	}

	createReq := &contract.CreateDigitalAssistantRequest{
		Code:         req.Code,
		Name:         firstNonEmpty(req.Name, tpl.Name),
		Description:  firstNonEmpty(req.Description, tpl.Description),
		Avatar:       firstNonEmpty(req.Avatar, tpl.Avatar),
		SystemPrompt: firstNonEmpty(req.SystemPrompt, tpl.SystemPrompt),
		TemplateID:   &tpl.ID,
		Source:       "template",
	}
	if len(req.Expertise) > 0 {
		createReq.Expertise = req.Expertise
	} else {
		createReq.Expertise = []string(tpl.Expertise)
	}

	result, err := s.CreateDigitalAssistant(ctx, createReq)
	if err != nil {
		return nil, err
	}
	if err := s.UpdateDigitalAssistantStatus(ctx, result.ID, &contract.UpdateDigitalAssistantStatusRequest{
		Status: string(contract.DigitalAssistantStatusActive),
	}); err != nil {
		return nil, err
	}
	if err := db.IncrementAITeammateTemplateUseCount(ctx, s.db, tpl.ID); err != nil {
		return nil, err
	}
	detail, err := s.GetDigitalAssistantByID(ctx, result.ID)
	if err != nil {
		return nil, err
	}
	return &detail.DigitalAssistant, nil
}

func (s *digitalAssistantService) markAssistantActive(ctx context.Context, da *types.DigitalAssistant) error {
	if s.workerProvisioning == nil {
		return nil
	}
	if err := s.workerProvisioning.MarkAssistantActive(ctx, da); err != nil {
		return fmt.Errorf("mark worker deployment active: %w", err)
	}
	return nil
}

func (s *digitalAssistantService) convertToContractDigitalAssistant(ctx context.Context, da *types.DigitalAssistant) *contract.DigitalAssistant {
	item := &contract.DigitalAssistant{
		ID:           da.ID,
		Code:         da.Code,
		OrgID:        da.OrgID,
		OwnerID:      da.OwnerID,
		Name:         da.Name,
		Description:  da.Description,
		Avatar:       da.Avatar,
		Status:       da.Status,
		Version:      da.Version,
		SystemPrompt: da.SystemPrompt,
		Expertise:    []string(da.Expertise),
		TemplateID:   da.TemplateID,
		Source:       da.Source,
		CreatedAt:    da.CreatedAt,
		UpdatedAt:    da.UpdatedAt,
	}
	if s != nil && s.db != nil {
		deployment, err := db.GetWorkerDeploymentByAssistantID(ctx, s.db, da.ID)
		if err == nil && deployment != nil {
			item.Deployment = &contract.WorkerDeploymentStatus{
				Status:    deployment.Status,
				LastError: deployment.LastError,
			}
		}
	}
	return item
}

func generateAssistantCode() string {
	return fmt.Sprintf("assistant_%s", snowflake.GenerateIDBase58())
}

func normalizeExpertise(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
		if len(result) >= 8 {
			break
		}
	}
	return result
}

func inferAssistantExpertise(parts ...string) []string {
	text := strings.ToLower(strings.Join(parts, " "))
	rules := []struct {
		keywords []string
		label    string
	}{
		{[]string{"热点", "热搜", "新媒体", "自媒体", "选题", "内容", "传播"}, "新媒体运营"},
		{[]string{"代码", "编程", "研发", "前端", "后端", "python", "go", "react", "测试", "运维"}, "技术研发"},
		{[]string{"数据", "分析", "报表", "可视化", "指标", "增长"}, "数据分析"},
		{[]string{"产品", "prd", "需求", "用户", "路线图"}, "产品规划"},
		{[]string{"营销", "投放", "转化", "品牌", "增长"}, "营销增长"},
		{[]string{"招聘", "绩效", "组织", "hr", "人力"}, "人力资源"},
		{[]string{"合同", "合规", "法务", "风险"}, "法务合规"},
		{[]string{"文档", "会议", "办公", "协同", "效率"}, "办公协同"},
	}

	result := make([]string, 0, 4)
	for _, rule := range rules {
		for _, keyword := range rule.keywords {
			if strings.Contains(text, strings.ToLower(keyword)) {
				result = append(result, rule.label)
				break
			}
		}
		if len(result) >= 5 {
			break
		}
	}
	if len(result) == 0 {
		return []string{"通用助理"}
	}
	return normalizeExpertise(result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
