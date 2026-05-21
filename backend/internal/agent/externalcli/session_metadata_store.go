package externalcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// SessionMetadataProviderSessionStore stores provider session bindings in Session.Metadata.Extra.
type SessionMetadataProviderSessionStore struct {
	db *gorm.DB
}

// NewSessionMetadataProviderSessionStore creates a provider session store backed by Session.Metadata.
func NewSessionMetadataProviderSessionStore(db *gorm.DB) *SessionMetadataProviderSessionStore {
	return &SessionMetadataProviderSessionStore{db: db}
}

// Get returns the provider session binding stored on the Leros session metadata.
func (s *SessionMetadataProviderSessionStore) Get(ctx context.Context, key ProviderSessionKey) (*ProviderSessionBinding, error) {
	if s == nil || s.db == nil || key.InternalSessionID == "" || key.Provider == "" {
		return nil, nil
	}
	session, err := getSessionByInternalSessionID(ctx, s.db, key.InternalSessionID)
	if err != nil || session == nil {
		return nil, err
	}
	sessions, err := providerSessionsFromMetadata(session.Metadata)
	if err != nil {
		return nil, err
	}
	metadata, ok := sessions[key.Provider]
	if !ok || metadata.ProviderSessionID == "" {
		return nil, nil
	}
	if metadata.Provider == "" {
		metadata.Provider = key.Provider
	}
	return &ProviderSessionBinding{
		InternalSessionID: key.InternalSessionID,
		Provider:          metadata.Provider,
		ProviderSessionID: metadata.ProviderSessionID,
		Status:            providerSessionStatusActive,
		CreatedAt:         metadata.CreatedAt,
	}, nil
}

// Upsert stores or replaces the provider session binding on Session.Metadata.Extra.
func (s *SessionMetadataProviderSessionStore) Upsert(ctx context.Context, binding *ProviderSessionBinding) error {
	if s == nil || s.db == nil || binding == nil {
		return nil
	}
	if binding.InternalSessionID == "" || binding.Provider == "" || binding.ProviderSessionID == "" {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session types.Session
		query := tx
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		err := query.Where("public_id = ?", binding.InternalSessionID).First(&session).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if session.Metadata.Extra == nil {
			session.Metadata.Extra = map[string]interface{}{}
		}
		sessions, err := providerSessionsFromMetadata(session.Metadata)
		if err != nil {
			return err
		}
		createdAt := time.Now().UTC()
		if existing, ok := sessions[binding.Provider]; ok && !existing.CreatedAt.IsZero() {
			createdAt = existing.CreatedAt
		}
		sessions[binding.Provider] = ProviderSessionMetadata{
			Provider:          binding.Provider,
			ProviderSessionID: binding.ProviderSessionID,
			CreatedAt:         createdAt,
		}
		session.Metadata.Extra[externalCLISessionsMetadataKey] = sessions

		return tx.Model(&types.Session{}).
			Where("id = ?", session.ID).
			Update("metadata", session.Metadata).Error
	})
}

// MarkFailed leaves the provider binding unchanged because failure state is not persisted.
func (s *SessionMetadataProviderSessionStore) MarkFailed(_ context.Context, _ ProviderSessionKey, _ string) error {
	return nil
}

func getSessionByInternalSessionID(ctx context.Context, db *gorm.DB, sessionID string) (*types.Session, error) {
	var session types.Session
	err := db.WithContext(ctx).Where("public_id = ?", sessionID).First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func providerSessionsFromMetadata(metadata types.SessionMetadata) (map[string]ProviderSessionMetadata, error) {
	if metadata.Extra == nil {
		return map[string]ProviderSessionMetadata{}, nil
	}
	raw, ok := metadata.Extra[externalCLISessionsMetadataKey]
	if !ok || raw == nil {
		return map[string]ProviderSessionMetadata{}, nil
	}
	sessions := map[string]ProviderSessionMetadata{}
	switch typed := raw.(type) {
	case map[string]ProviderSessionMetadata:
		for provider, session := range typed {
			sessions[provider] = session
		}
		return sessions, nil
	case map[string]interface{}:
		for provider, value := range typed {
			session, err := providerSessionMetadataFromValue(value)
			if err != nil {
				return nil, fmt.Errorf("decode provider session %q: %w", provider, err)
			}
			if session.Provider == "" {
				session.Provider = provider
			}
			sessions[provider] = session
		}
		return sessions, nil
	default:
		session, err := providerSessionMetadataFromValue(raw)
		if err != nil {
			logs.Warnf("Ignoring invalid external CLI session metadata: %v", err)
			return map[string]ProviderSessionMetadata{}, nil
		}
		if session.Provider == "" {
			return map[string]ProviderSessionMetadata{}, nil
		}
		sessions[session.Provider] = session
		return sessions, nil
	}
}

func providerSessionMetadataFromValue(value interface{}) (ProviderSessionMetadata, error) {
	if value == nil {
		return ProviderSessionMetadata{}, nil
	}
	if session, ok := value.(ProviderSessionMetadata); ok {
		return session, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ProviderSessionMetadata{}, err
	}
	var session ProviderSessionMetadata
	if err := json.Unmarshal(data, &session); err != nil {
		return ProviderSessionMetadata{}, err
	}
	return session, nil
}
