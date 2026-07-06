package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type WorkerProvisioningService struct {
	db        *gorm.DB
	scheduler *config.SchedulerConfig
}

func NewWorkerProvisioningService(database *gorm.DB, scheduler *config.SchedulerConfig) *WorkerProvisioningService {
	return &WorkerProvisioningService{db: database, scheduler: scheduler}
}

func (s *WorkerProvisioningService) EnsureDefaultWorkerForOrg(ctx context.Context, orgID, ownerID uint) (*types.WorkerDeployment, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("worker provisioning database is required")
	}
	if orgID == 0 {
		return nil, fmt.Errorf("org_id is required")
	}

	code := defaultWorkerCode(orgID)
	assistant, err := db.GetDigitalAssistantByCode(ctx, s.db, code)
	if err != nil {
		return nil, err
	}
	if assistant == nil {
		assistant = &types.DigitalAssistant{
			Code:         code,
			OrgID:        orgID,
			OwnerID:      ownerID,
			Name:         "lework",
			Description:  "你工作和生活中的 AI 队友",
			Status:       string(contract.DigitalAssistantStatusActive),
			Version:      0,
			SystemPrompt: "你的名称是 lework。你是用户工作和生活中的 AI 队友，让工作，乐起来。用户询问你是谁、你能做什么时，请按 lework 的身份回答，不要称自己为默认数字员工。",
		}
		if err := db.CreateDigitalAssistant(ctx, s.db, assistant); err != nil {
			return nil, err
		}
	}
	if assistant.Status != string(contract.DigitalAssistantStatusActive) {
		assistant.Status = string(contract.DigitalAssistantStatusActive)
		if err := db.UpdateDigitalAssistant(ctx, s.db, assistant); err != nil {
			return nil, err
		}
	}

	existing, err := db.GetDefaultWorkerDeployment(ctx, s.db, orgID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := s.ensureWorkerDeploymentPublicID(ctx, existing); err != nil {
			return nil, err
		}
		if existing.DigitalAssistantID != assistant.ID {
			existing.DigitalAssistantID = assistant.ID
			if err := db.UpdateWorkerDeployment(ctx, s.db, existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}
	return s.EnsureForAssistant(ctx, assistant)
}

func (s *WorkerProvisioningService) EnsureForAssistant(ctx context.Context, da *types.DigitalAssistant) (*types.WorkerDeployment, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("worker provisioning database is required")
	}
	if da == nil {
		return nil, fmt.Errorf("digital assistant is required")
	}
	existing, err := db.GetWorkerDeploymentByAssistantID(ctx, s.db, da.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := s.ensureWorkerDeploymentPublicID(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	workerID, err := db.NextWorkerID(ctx, s.db, da.OrgID)
	if err != nil {
		return nil, err
	}
	status := string(types.WorkerDeploymentStatusStopped)
	if da.Status == string(contract.DigitalAssistantStatusActive) {
		status = string(types.WorkerDeploymentStatusPending)
	}
	deployment := &types.WorkerDeployment{
		PublicID:           generateWorkerDeploymentPublicID(),
		OrgID:              da.OrgID,
		DigitalAssistantID: da.ID,
		WorkerID:           workerID,
		DeploymentName:     workerDeploymentName(da.OrgID, workerID),
		Namespace:          s.namespace(),
		Status:             status,
		WorkspacePath:      s.workspacePath(da.OrgID, workerID),
	}
	if err := db.CreateWorkerDeployment(ctx, s.db, deployment); err != nil {
		return nil, err
	}
	return deployment, nil
}

func (s *WorkerProvisioningService) ensureWorkerDeploymentPublicID(ctx context.Context, deployment *types.WorkerDeployment) error {
	if deployment == nil || strings.TrimSpace(deployment.PublicID) != "" {
		return nil
	}
	deployment.PublicID = generateWorkerDeploymentPublicID()
	return db.UpdateWorkerDeployment(ctx, s.db, deployment)
}

func (s *WorkerProvisioningService) MarkAssistantActive(ctx context.Context, da *types.DigitalAssistant) error {
	deployment, err := s.EnsureForAssistant(ctx, da)
	if err != nil {
		return err
	}
	deployment.Status = string(types.WorkerDeploymentStatusPending)
	deployment.LastError = ""
	return db.UpdateWorkerDeployment(ctx, s.db, deployment)
}

func (s *WorkerProvisioningService) MarkAssistantReady(ctx context.Context, da *types.DigitalAssistant) error {
	deployment, err := s.EnsureForAssistant(ctx, da)
	if err != nil {
		return err
	}
	deployment.Status = string(types.WorkerDeploymentStatusReady)
	deployment.LastError = ""
	return db.UpdateWorkerDeployment(ctx, s.db, deployment)
}

func (s *WorkerProvisioningService) MarkAssistantStopped(ctx context.Context, da *types.DigitalAssistant) error {
	deployment, err := db.GetWorkerDeploymentByAssistantID(ctx, s.db, da.ID)
	if err != nil || deployment == nil {
		return err
	}
	deployment.Status = string(types.WorkerDeploymentStatusStopped)
	deployment.LastError = ""
	return db.UpdateWorkerDeployment(ctx, s.db, deployment)
}

func workerDeploymentName(orgID, workerID uint) string {
	return fmt.Sprintf("leros-worker-o%d-w%d", orgID, workerID)
}

func generateWorkerDeploymentPublicID() string {
	return fmt.Sprintf("wrk_%s", snowflake.GenerateIDBase58())
}

func defaultWorkerCode(orgID uint) string {
	return fmt.Sprintf("default_o%d", orgID)
}

func (s *WorkerProvisioningService) namespace() string {
	if s.scheduler != nil && strings.TrimSpace(s.scheduler.Namespace) != "" {
		return strings.TrimSpace(s.scheduler.Namespace)
	}
	return "default"
}

func (s *WorkerProvisioningService) workspacePath(_, _ uint) string {
	root := "/data/workspace"
	if s.scheduler != nil && strings.TrimSpace(s.scheduler.WorkspaceHostPathRoot) != "" {
		root = strings.TrimSpace(s.scheduler.WorkspaceHostPathRoot)
	}
	return root
}
