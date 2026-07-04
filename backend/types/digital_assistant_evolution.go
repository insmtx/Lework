package types

import "gorm.io/gorm"

// DigitalAssistantPromptBlock stores layered persona prompt blocks for future dynamic injection.
type DigitalAssistantPromptBlock struct {
	gorm.Model

	// AssistantID references the AI teammate that owns this prompt block.
	AssistantID uint `gorm:"column:assistant_id;type:bigint;not null;index:idx_da_prompt_block_assistant_type"`
	// BlockType identifies the prompt layer, such as identity, capability, boundary, style, or example.
	BlockType string `gorm:"column:block_type;type:varchar(32);not null;index:idx_da_prompt_block_assistant_type"`
	// Title is a short management label for the block.
	Title string `gorm:"column:title;type:varchar(255);not null"`
	// Content stores the prompt fragment that can be injected when this layer is selected.
	Content string `gorm:"column:content;type:text;not null"`
	// Priority controls deterministic ordering when multiple blocks are injected.
	Priority int `gorm:"column:priority;type:integer;not null;default:0;index"`
	// Enabled controls whether the block can participate in prompt assembly.
	Enabled bool `gorm:"column:enabled;type:boolean;not null;default:true;index"`
	// Version tracks prompt block edits for traceability.
	Version int `gorm:"column:version;type:integer;not null;default:1"`
}

// TableName returns the digital assistant prompt block table name.
func (DigitalAssistantPromptBlock) TableName() string {
	return TableNameDigitalAssistantPromptBlock
}

// DigitalAssistantMemory stores evolvable teammate memory for future retrieval-augmented persona.
type DigitalAssistantMemory struct {
	gorm.Model

	// AssistantID references the AI teammate that owns this memory.
	AssistantID uint `gorm:"column:assistant_id;type:bigint;not null;index:idx_da_memory_assistant_type"`
	// MemoryType identifies the memory category, such as preference, experience, template, fact, or rule.
	MemoryType string `gorm:"column:memory_type;type:varchar(32);not null;index:idx_da_memory_assistant_type"`
	// Content stores the memory text that can be retrieved and injected later.
	Content string `gorm:"column:content;type:text;not null"`
	// SourceType records where the memory came from, such as user_confirmed, task_summary, or manual.
	SourceType string `gorm:"column:source_type;type:varchar(64);not null;default:manual;index"`
	// SourceID optionally links the memory to a task, session, message, or external artifact.
	SourceID string `gorm:"column:source_id;type:varchar(255);index"`
	// Confidence records trust in this memory before it is used for prompt assembly.
	Confidence float64 `gorm:"column:confidence;type:double precision;not null;default:0.8;index"`
	// EmbeddingID stores the vector-store identifier when retrieval is enabled.
	EmbeddingID string `gorm:"column:embedding_id;type:varchar(255);index"`
	// Enabled controls whether the memory can participate in retrieval.
	Enabled bool `gorm:"column:enabled;type:boolean;not null;default:true;index"`
}

// TableName returns the digital assistant memory table name.
func (DigitalAssistantMemory) TableName() string {
	return TableNameDigitalAssistantMemory
}

// AssistantPromptTrace records which persona layers and memories were injected for an LLM request.
type AssistantPromptTrace struct {
	gorm.Model

	// SessionID references the session where this prompt was assembled.
	SessionID uint `gorm:"column:session_id;type:bigint;not null;index"`
	// MessageID references the user message that triggered this prompt assembly.
	MessageID uint `gorm:"column:message_id;type:bigint;not null;index"`
	// AssistantID references the AI teammate whose persona was assembled.
	AssistantID uint `gorm:"column:assistant_id;type:bigint;not null;index"`
	// CorePromptVersion records the mandatory identity prompt version.
	CorePromptVersion int `gorm:"column:core_prompt_version;type:integer;not null;default:1"`
	// InjectedBlockIDs records prompt block IDs injected into this request.
	InjectedBlockIDs SkillStringList `gorm:"column:injected_block_ids;type:jsonb"`
	// InjectedMemoryIDs records memory IDs injected into this request.
	InjectedMemoryIDs SkillStringList `gorm:"column:injected_memory_ids;type:jsonb"`
	// PromptHash stores a stable hash of the assembled prompt for debugging without duplicating long text.
	PromptHash string `gorm:"column:prompt_hash;type:varchar(128);index"`
}

// TableName returns the assistant prompt trace table name.
func (AssistantPromptTrace) TableName() string {
	return TableNameAssistantPromptTrace
}
