package types

import "errors"

// ErrSparkWorkflowConflict prevents concurrent writers or changed request identities.
var ErrSparkWorkflowConflict = errors.New("spark workflow conflict")

// SparkWorkflowPhase records intent before an external mutation can be sent.
type SparkWorkflowPhase string

const (
	SparkWorkflowAccepted        SparkWorkflowPhase = "accepted"
	SparkWorkflowPauseIntent     SparkWorkflowPhase = "pause_intent"
	SparkWorkflowWaitingIdle     SparkWorkflowPhase = "waiting_idle"
	SparkWorkflowModelIntent     SparkWorkflowPhase = "model_intent"
	SparkWorkflowWaitingModel    SparkWorkflowPhase = "waiting_model"
	SparkWorkflowResumeIntent    SparkWorkflowPhase = "resume_intent"
	SparkWorkflowWaitingResume   SparkWorkflowPhase = "waiting_resume"
	SparkWorkflowCompleted       SparkWorkflowPhase = "completed"
	SparkWorkflowCompletedPaused SparkWorkflowPhase = "completed_paused"
	SparkWorkflowAttention       SparkWorkflowPhase = "attention"
)

// SparkWorkflowRequest contains only installation-bound CAS inputs.
type SparkWorkflowRequest struct {
	OperationID                 string `json:"operation_id"`
	Action                      string `json:"action"`
	BootID                      string `json:"boot_id"`
	ExpectedModelRevision       uint64 `json:"expected_model_revision"`
	ExpectedMaintenanceRevision uint64 `json:"expected_maintenance_revision"`
}

// SparkWorkflowHost is a durable mutex, never a lease. Org binding cannot be reassigned.
type SparkWorkflowHost struct {
	HostID            string `gorm:"primaryKey;size:128"`
	OrgID             uint   `gorm:"not null"`
	ActiveOperationID string `gorm:"size:128;not null"`
	Revision          uint64 `gorm:"not null"`
}

func (SparkWorkflowHost) TableName() string { return TableNameSparkWorkflowHost }

// SparkWorkflowOperation keeps immutable identity separate from mutable execution evidence.
type SparkWorkflowOperation struct {
	HostID                      string `gorm:"primaryKey;size:128"`
	OperationID                 string `gorm:"primaryKey;size:128"`
	OrgID                       uint   `gorm:"not null"`
	Actor                       uint
	EffectOperationID           string `gorm:"size:128"`
	Fingerprint                 string `gorm:"size:64"`
	BindingDigest               string `gorm:"size:64"`
	RunnerID                    string `gorm:"size:64"`
	Kind                        string `gorm:"size:32"`
	Action                      string `gorm:"size:16"`
	BootID                      string `gorm:"size:128"`
	ModelID                     string `gorm:"size:128"`
	ExpectedModelRevision       uint64
	ExpectedMaintenanceRevision uint64
	InitialActive               bool
	InitialOwner                string `gorm:"size:128"`
	OwnsPause                   bool
	PauseRevision               uint64
	PauseOwner                  string `gorm:"size:128"`
	ReceiptRevision             uint64
	Phase                       SparkWorkflowPhase `gorm:"size:32"`
	Version                     uint64
	CreatedAt                   int64 `gorm:"autoCreateTime:false"`
	UpdatedAt                   int64 `gorm:"autoUpdateTime:false"`
	Deadline                    int64
	IdleDeadline                int64
	ErrorCode                   string `gorm:"size:64"`
}

func (SparkWorkflowOperation) TableName() string { return TableNameSparkWorkflowOperation }

// SparkWorkflow is the fixed safe projection; it excludes actor and installation identity.
type SparkWorkflow struct {
	OperationID         string             `json:"operation_id"`
	Action              string             `json:"action"`
	Kind                string             `json:"kind"`
	Phase               SparkWorkflowPhase `json:"phase"`
	Version             uint64             `json:"version"`
	CreatedAt           int64              `json:"created_at"`
	UpdatedAt           int64              `json:"updated_at"`
	Deadline            int64              `json:"deadline"`
	ErrorCode           string             `json:"error_code"`
	NeedsAttention      bool               `json:"needs_attention"`
	MaintenanceRetained bool               `json:"maintenance_retained"`
}

// SparkWorkflowControl stays readable even when the host agent is offline.
type SparkWorkflowControl struct {
	Enabled  bool           `json:"enabled"`
	Current  *SparkWorkflow `json:"current"`
	CanStart bool           `json:"can_start"`
	CanStop  bool           `json:"can_stop"`
}
