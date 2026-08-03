package types

import "time"

// ProjectActivityAction 表示项目操作动态类型。
type ProjectActivityAction string

const (
	// ProjectActivityActionProjectCreated 记录项目创建。
	ProjectActivityActionProjectCreated ProjectActivityAction = "project.created"
	// ProjectActivityActionSkillsChanged 记录项目技能集合变化。
	ProjectActivityActionSkillsChanged ProjectActivityAction = "project.skills.changed"
	// ProjectActivityActionMCPsChanged 记录项目 MCP 连接器集合变化。
	ProjectActivityActionMCPsChanged ProjectActivityAction = "project.mcps.changed"
	// ProjectActivityActionParticipantsChanged 记录项目成员和 AI 队友集合变化。
	ProjectActivityActionParticipantsChanged ProjectActivityAction = "project.participants.changed"
)

// ProjectActivityPayload 保存不同动态类型的差异明细。
type ProjectActivityPayload struct {
	AddedSkillIDs        []string `json:"added_skill_ids"`
	RemovedSkillIDs      []string `json:"removed_skill_ids"`
	AddedMCPIDs          []string `json:"added_mcp_ids"`
	RemovedMCPIDs        []string `json:"removed_mcp_ids"`
	AddedMemberIDs       []string `json:"added_member_ids"`
	RemovedMemberIDs     []string `json:"removed_member_ids"`
	AddedAITeammateIDs   []string `json:"added_ai_teammate_ids"`
	RemovedAITeammateIDs []string `json:"removed_ai_teammate_ids"`
}

// ProjectActivity 记录项目内成员、AI 队友、技能等操作动态。
type ProjectActivity struct {
	ID         uint                   `gorm:"primaryKey" json:"id"`
	ProjectID  string                 `gorm:"column:project_id;type:varchar(255);not null;index:idx_project_activity_project_time,priority:1;index:idx_project_activity_project_operator_time,priority:1" json:"project_id"`
	OperatorID string                 `gorm:"column:operator_id;type:varchar(64);not null;index:idx_project_activity_operator_time,priority:1;index:idx_project_activity_project_operator_time,priority:2" json:"operator_id"`
	ActionType ProjectActivityAction  `gorm:"column:action_type;type:varchar(64);not null;index" json:"action_type"`
	Payload    ProjectActivityPayload `gorm:"column:payload;type:jsonb;serializer:json" json:"payload"`
	RequestID  *string                `gorm:"column:request_id;type:varchar(64);uniqueIndex:uk_project_activity_request_id,where:request_id IS NOT NULL" json:"request_id,omitempty"`
	Version    int16                  `gorm:"column:version;type:smallint;not null;default:1" json:"version"`
	CreatedAt  time.Time              `gorm:"column:created_at;not null;index:idx_project_activity_project_time,priority:2,sort:desc;index:idx_project_activity_operator_time,priority:2,sort:desc;index:idx_project_activity_project_operator_time,priority:3,sort:desc" json:"created_at"`
}

// TableName 返回项目动态表名。
func (ProjectActivity) TableName() string {
	return TableNameProjectActivity
}
