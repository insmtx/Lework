package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/worker"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

var _ contract.DigitalAssistantService = (*digitalAssistantService)(nil)

type digitalAssistantService struct {
	db              *gorm.DB
	workerScheduler worker.WorkerScheduler
}

func NewDigitalAssistantService(db *gorm.DB, workerScheduler worker.WorkerScheduler) contract.DigitalAssistantService {
	return &digitalAssistantService{
		db:              db,
		workerScheduler: workerScheduler,
	}
}

func (s *digitalAssistantService) CreateDigitalAssistant(ctx context.Context, req *contract.CreateDigitalAssistantRequest) (*contract.DigitalAssistant, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return nil, errors.New("user not authenticated or org not set")
	}

	if req.Code == "" {
		return nil, errors.New("code is required")
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}

	exists, err := db.DigitalAssistantCodeExists(ctx, s.db, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("digital assistant with this code already exists")
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
	}

	if err := db.CreateDigitalAssistant(ctx, s.db, da); err != nil {
		return nil, err
	}

	if s.workerScheduler != nil && da.Status == string(contract.DigitalAssistantStatusActive) {
		spec := &worker.WorkerSpec{
			ID:      da.Code,
			Name:    da.Name,
			EnvType: worker.WorkerEnvProcess,
		}
		if _, err := s.workerScheduler.Start(ctx, spec); err != nil {
			logs.Warnf("Failed to start worker for assistant %s: %v", da.Code, err)
		}
	}

	return convertToContractDigitalAssistant(da), nil
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
		DigitalAssistant: *convertToContractDigitalAssistant(da),
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
		DigitalAssistant: *convertToContractDigitalAssistant(da),
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
	da.UpdatedAt = time.Now()

	if err := db.UpdateDigitalAssistant(ctx, s.db, da); err != nil {
		return nil, err
	}

	return convertToContractDigitalAssistant(da), nil
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

	entities, total, err := db.ListDigitalAssistant(ctx, s.db, opt)

	if err != nil {
		return nil, err
	}

	items := make([]contract.DigitalAssistant, 0, len(entities))
	for _, entity := range entities {
		items = append(items, *convertToContractDigitalAssistant(entity))
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

	return db.UpdateDigitalAssistant(ctx, s.db, da)
}

func convertToContractDigitalAssistant(da *types.DigitalAssistant) *contract.DigitalAssistant {
	return &contract.DigitalAssistant{
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
		CreatedAt:    da.CreatedAt,
		UpdatedAt:    da.UpdatedAt,
	}
}
