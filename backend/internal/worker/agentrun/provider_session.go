package agentrun

import "context"

// ProviderSessionKey identifies a provider session binding.
type ProviderSessionKey struct {
	InternalSessionID string
	Provider          string
	WorkDir           string
	AssistantID       string
}

// ProviderSessionBinding maps a SingerOS session to a provider native session.
type ProviderSessionBinding struct {
	InternalSessionID string
	Provider          string
	ProviderSessionID string
	WorkDir           string
	AssistantID       string
	Status            string
	LastError         string
}

// ProviderSessionStore persists provider session bindings for external CLI resume.
// Implementations are provided by the Worker host layer (e.g. SQLite).
type ProviderSessionStore interface {
	GetProviderSession(ctx context.Context, key ProviderSessionKey) (*ProviderSessionBinding, error)
	UpsertProviderSession(ctx context.Context, binding *ProviderSessionBinding) error
	MarkProviderSessionFailed(ctx context.Context, key ProviderSessionKey, reason string) error
}
