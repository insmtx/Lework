package cli

import (
	"strings"

	"github.com/google/uuid"
)

// MessageIDMapper maps provider-local message IDs to stable execution-local IDs.
type MessageIDMapper struct {
	byProvider map[string]string
	current    string
}

// NewMessageIDMapper creates an execution-local message ID mapper.
func NewMessageIDMapper() *MessageIDMapper {
	return &MessageIDMapper{byProvider: make(map[string]string)}
}

// ForProvider returns the stable ID assigned to one provider message.
func (m *MessageIDMapper) ForProvider(providerID string) string {
	if m == nil {
		return uuid.NewString()
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return m.CurrentOrNew()
	}
	if id := m.byProvider[providerID]; id != "" {
		m.current = id
		return id
	}
	id := uuid.NewString()
	m.byProvider[providerID] = id
	m.current = id
	return id
}

// CurrentOrNew returns the current message ID or creates one.
func (m *MessageIDMapper) CurrentOrNew() string {
	if m == nil {
		return uuid.NewString()
	}
	if m.current == "" {
		m.current = uuid.NewString()
	}
	return m.current
}

// StartNew creates and selects a new message ID.
func (m *MessageIDMapper) StartNew() string {
	if m == nil {
		return uuid.NewString()
	}
	m.current = uuid.NewString()
	return m.current
}
