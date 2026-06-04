package eino

import (
	"strings"

	"github.com/google/uuid"
)

type messageIDMapper struct {
	current string
}

func newMessageIDMapper() *messageIDMapper {
	return &messageIDMapper{}
}

func (m *messageIDMapper) StartNew() string {
	m.current = uuid.NewString()
	return m.current
}

func (m *messageIDMapper) CurrentOrNew() string {
	if m == nil {
		return uuid.NewString()
	}
	if strings.TrimSpace(m.current) == "" {
		return m.StartNew()
	}
	return m.current
}
