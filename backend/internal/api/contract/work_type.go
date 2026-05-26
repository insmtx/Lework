package contract

// NewMessageRequest is the homepage new-message request that atomically creates
// Project + Task + Session and dispatches to the allocated AgentWorker.
type NewMessageRequest struct {
	Content     string `json:"content" binding:"required"`
	ProjectID   string `json:"project_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	AssistantID uint   `json:"assistant_id,omitempty"`
	MessageType string `json:"message_type,omitempty"`
	Objective   string `json:"objective,omitempty"`
}

// NewMessageResponse is the homepage new-message response containing IDs of all
// created entities so the frontend can navigate to the session.
type NewMessageResponse struct {
	ProjectID   string `json:"project_id"`
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id"`
	MessageID   string `json:"message_id"`
	AssistantID uint   `json:"assistant_id"`
}
