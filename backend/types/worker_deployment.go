package types

import (
	"time"

	"gorm.io/gorm"
)

type WorkerDeploymentStatus string

const (
	WorkerDeploymentStatusPending      WorkerDeploymentStatus = "pending"
	WorkerDeploymentStatusProvisioning WorkerDeploymentStatus = "provisioning"
	WorkerDeploymentStatusReady        WorkerDeploymentStatus = "ready"
	WorkerDeploymentStatusFailed       WorkerDeploymentStatus = "failed"
	WorkerDeploymentStatusStopped      WorkerDeploymentStatus = "stopped"
)

// WorkerDeployment binds an AI teammate to an org-scoped worker runtime.
type WorkerDeployment struct {
	gorm.Model
	PublicID           string     `gorm:"column:public_id;type:varchar(255);not null;default:'';uniqueIndex:idx_worker_deploy_public_id"`
	OrgID              uint       `gorm:"column:org_id;type:bigint;not null;index;uniqueIndex:idx_worker_deploy_org_worker"`
	DigitalAssistantID uint       `gorm:"column:digital_assistant_id;type:bigint;not null;uniqueIndex;index"`
	WorkerID           uint       `gorm:"column:worker_id;type:bigint;not null;uniqueIndex:idx_worker_deploy_org_worker"`
	DeploymentName     string     `gorm:"column:deployment_name;type:varchar(255);not null;uniqueIndex"`
	Namespace          string     `gorm:"column:namespace;type:varchar(255);not null;default:''"`
	Status             string     `gorm:"column:status;type:varchar(50);not null;default:'pending';index"`
	BootstrapTokenHash string     `gorm:"column:bootstrap_token_hash;type:varchar(64);not null;default:''"`
	WorkspacePath      string     `gorm:"column:workspace_path;type:varchar(1000);not null;default:''"`
	LastError          string     `gorm:"column:last_error;type:text"`
	LastStartedAt      *time.Time `gorm:"column:last_started_at;index"`
	LastReconciledAt   *time.Time `gorm:"column:last_reconciled_at;index"`
}

// TableName 指定 WorkerDeployment 结构体对应的数据库表名。
func (WorkerDeployment) TableName() string {
	return TableNameWorkerDeployment
}
