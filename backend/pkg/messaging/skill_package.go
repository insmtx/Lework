package messaging

// SkillChangeType describes a publishable worker-side Skill change.
type SkillChangeType string

const (
	SkillChangeCreated SkillChangeType = "created"
	SkillChangeUpdated SkillChangeType = "updated"
)

// SkillPackageUploadedEvent reports a Skill package already uploaded through
// the shared presigned-storage flow. FileUpload identity remains server-owned.
type SkillPackageUploadedEvent struct {
	EventID    string          `json:"event_id"`
	WorkerID   uint            `json:"worker_id"`
	RunID      string          `json:"run_id"`
	ProjectID  uint            `json:"project_id,omitempty"`
	ActorUIN   uint            `json:"actor_uin"`
	SkillCode  string          `json:"skill_code"`
	ChangeType SkillChangeType `json:"change_type"`
	StorageURI string          `json:"storage_uri"`
	SHA256     string          `json:"sha256"`
	FileSize   int64           `json:"file_size"`
	Filename   string          `json:"filename"`
	MimeType   string          `json:"mime_type"`
}
