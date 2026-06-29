package events

import (
	"strings"

	"github.com/google/uuid"
)

// MessageIDMapper 在一次运行中将提供者消息标识符映射到 Leros 消息 ID。
type MessageIDMapper struct {
	byProvider map[string]string
	current    string
}

// NewMessageIDMapper 创建一次运行时执行的映射器。
func NewMessageIDMapper() *MessageIDMapper {
	return &MessageIDMapper{
		byProvider: make(map[string]string),
	}
}

// ForProvider 返回提供者本地消息 ID 对应的稳定 Leros 消息 ID。
func (m *MessageIDMapper) ForProvider(providerID string) string {
	if m == nil {
		return uuid.NewString()
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return m.CurrentOrNew()
	}
	if m.byProvider == nil {
		m.byProvider = make(map[string]string)
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

// CurrentOrNew 返回当前消息 ID，如有需要则创建。
func (m *MessageIDMapper) CurrentOrNew() string {
	if m == nil {
		return uuid.NewString()
	}
	if m.current == "" {
		m.current = uuid.NewString()
	}
	return m.current
}

// StartNew 创建一个新的消息 ID 并将其标记为当前。
func (m *MessageIDMapper) StartNew() string {
	if m == nil {
		return uuid.NewString()
	}
	m.current = uuid.NewString()
	return m.current
}
