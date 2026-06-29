package externalcli

import (
	"context"
	"sync"
	"time"
)

// InMemoryProviderSessionStore 在进程内存中存储提供者会话绑定。
type InMemoryProviderSessionStore struct {
	mu       sync.RWMutex
	bindings map[ProviderSessionKey]*ProviderSessionBinding
}

// NewInMemoryProviderSessionStore 创建内存中的提供者会话存储。
func NewInMemoryProviderSessionStore() *InMemoryProviderSessionStore {
	return &InMemoryProviderSessionStore{
		bindings: make(map[ProviderSessionKey]*ProviderSessionBinding),
	}
}

// Get 返回对应键的提供者会话绑定。
func (s *InMemoryProviderSessionStore) Get(_ context.Context, key ProviderSessionKey) (*ProviderSessionBinding, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.bindings[key]
	if !ok || binding == nil {
		return nil, nil
	}
	cloned := *binding
	return &cloned, nil
}

// Upsert 创建或替换提供者会话绑定。
func (s *InMemoryProviderSessionStore) Upsert(_ context.Context, binding *ProviderSessionBinding) error {
	if s == nil || binding == nil {
		return nil
	}
	key := ProviderSessionKey{
		InternalSessionID: binding.InternalSessionID,
		Provider:          binding.Provider,
		WorkDir:           binding.WorkDir,
		AssistantID:       binding.AssistantID,
	}
	if key.InternalSessionID == "" || key.Provider == "" || binding.ProviderSessionID == "" {
		return nil
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.bindings[key]; ok && existing != nil && !existing.CreatedAt.IsZero() {
		binding.CreatedAt = existing.CreatedAt
	} else if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	cloned := *binding
	s.bindings[key] = &cloned
	return nil
}

// MarkFailed 将提供者会话绑定标记为失败。
func (s *InMemoryProviderSessionStore) MarkFailed(_ context.Context, key ProviderSessionKey, reason string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[key]
	if !ok || binding == nil {
		return nil
	}
	cloned := *binding
	cloned.Status = providerSessionStatusFailed
	cloned.LastError = reason
	cloned.UpdatedAt = time.Now().UTC()
	s.bindings[key] = &cloned
	return nil
}
