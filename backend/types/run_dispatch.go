package types

import (
	"time"

	"gorm.io/gorm"
)

// ReliableTaskStatus is transport-independent lifecycle state for a durable task.
type ReliableTaskStatus string

const (
	// ReliableTaskPending is waiting for the Server relay to obtain a JetStream PubAck.
	ReliableTaskPending   ReliableTaskStatus = "pending"
	ReliableTaskPublished ReliableTaskStatus = "published"
	ReliableTaskExpired   ReliableTaskStatus = "expired"
)

// ReliableTask is a durable, opaque handoff. Business services provide its destination and payload;
// the dispatcher must not decode either one.
type ReliableTask struct {
	gorm.Model
	TaskID        string             `gorm:"column:task_id;type:varchar(255);not null;uniqueIndex"`
	Kind          string             `gorm:"column:kind;type:varchar(64);not null;uniqueIndex:ux_reliable_task_source,priority:1"`
	Destination   string             `gorm:"column:destination;type:varchar(512);not null"`
	ContentType   string             `gorm:"column:content_type;type:varchar(64);not null;default:'application/json'"`
	Payload       []byte             `gorm:"column:payload;type:jsonb;not null"`
	SourceType    string             `gorm:"column:source_type;type:varchar(64);not null;uniqueIndex:ux_reliable_task_source,priority:2;index:idx_reliable_task_source,priority:1"`
	SourceID      string             `gorm:"column:source_id;type:varchar(255);not null;uniqueIndex:ux_reliable_task_source,priority:3;index:idx_reliable_task_source,priority:2"`
	PartitionKey  string             `gorm:"column:partition_key;type:varchar(255);index"`
	Status        ReliableTaskStatus `gorm:"column:status;type:varchar(32);not null;index:idx_reliable_task_ready,priority:1"`
	AttemptCount  int                `gorm:"column:attempt_count;not null;default:0"`
	NextAttemptAt time.Time          `gorm:"column:next_attempt_at;not null;index:idx_reliable_task_ready,priority:2"`
	DeadlineAt    time.Time          `gorm:"column:deadline_at;not null;index"`
	LeaseOwner    string             `gorm:"column:lease_owner;type:varchar(255)"`
	LeaseUntil    *time.Time         `gorm:"column:lease_until;index"`
	PublishedAt   *time.Time         `gorm:"column:published_at"`
	LastError     string             `gorm:"column:last_error;type:text"`
}

func (ReliableTask) TableName() string { return TableNameReliableTask }

// ProjectionReceipt provides a generic idempotency boundary for durable consumers.
type ProjectionReceipt struct {
	gorm.Model
	Consumer  string `gorm:"column:consumer;type:varchar(128);not null;uniqueIndex:ux_projection_receipt"`
	EventID   string `gorm:"column:event_id;type:varchar(255);not null;uniqueIndex:ux_projection_receipt"`
	RunID     string `gorm:"column:run_id;type:varchar(255);index"`
	SessionID string `gorm:"column:session_id;type:varchar(255);not null;index"`
	EventType string `gorm:"column:event_type;type:varchar(64);not null"`
}

func (ProjectionReceipt) TableName() string { return TableNameProjectionReceipt }
