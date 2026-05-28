package modelrouter

import (
	"strings"
	"sync"
)

// Store keeps worker-local LLM configuration received from server task messages.
type Store struct {
	mu      sync.RWMutex
	current *UpstreamConfig
	byModel map[string]*UpstreamConfig
}

var defaultStore = newStore()

func newStore() *Store {
	return &Store{byModel: make(map[string]*UpstreamConfig)}
}

// DefaultStore returns the worker-local singleton model store.
func DefaultStore() *Store {
	return defaultStore
}

func resetDefaultStoreForTest() {
	defaultStore = newStore()
}

// Put stores an upstream config as the current task model config.
func (s *Store) Put(cfg UpstreamConfig) {
	if s == nil {
		return
	}
	normalized := cfg
	if normalized.Protocol == "" {
		normalized.Protocol = DefaultProtocolForProvider(normalized.Provider)
	}
	if strings.TrimSpace(normalized.BaseURL) == "" {
		normalized.BaseURL = defaultBaseURL(normalized.Provider)
	}
	if normalized.TimeoutSec == 0 {
		normalized.TimeoutSec = 120
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := cloneConfig(&normalized)
	s.current = cloned
	if s.byModel == nil {
		s.byModel = make(map[string]*UpstreamConfig)
	}
	if key := normalizeModelName(normalized.ModelName); key != "" {
		s.byModel[key] = cloneConfig(&normalized)
	}
}

func (s *Store) resolve(modelName string) (*UpstreamConfig, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if key := normalizeModelName(modelName); key != "" {
		cfg := s.byModel[key]
		if cfg != nil {
			return cloneConfig(cfg), true
		}
		return nil, false
	}
	if s.current == nil {
		return nil, false
	}
	return cloneConfig(s.current), true
}

func cloneConfig(cfg *UpstreamConfig) *UpstreamConfig {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	return &cloned
}

func normalizeModelName(modelName string) string {
	return strings.ToLower(strings.TrimSpace(modelName))
}
