//go:build !enterprise

package oss

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"github.com/insmtx/Leros/backend/types"
)

type builtinTokenParser struct {
	jwtSecret   string
	workerCfg   *config.WorkerAuthConfig
	userOrgRepo *userOrgRepo
	workerStore *workerStore
}

// NewTokenParser creates a builtin TokenParser backed by the local JWT
// secret and database.
func NewTokenParser(database *gorm.DB, jwtSecret string, workerCfg *config.WorkerAuthConfig) *builtinTokenParser {
	return &builtinTokenParser{
		jwtSecret:   strings.TrimSpace(jwtSecret),
		workerCfg:   workerCfg,
		userOrgRepo: newUserOrgRepo(database),
		workerStore: newWorkerStore(database),
	}
}

func (p *builtinTokenParser) ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error) {
	claims, err := localauth.ParseUserToken(tokenStr, p.jwtSecret)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	userOrg, err := p.userOrgRepo.GetByUin(queryCtx, claims.Uin)
	if err != nil {
		return &types.Caller{Uin: claims.Uin, Kind: types.CallerKindUser, State: types.AuthStateFailed}, nil
	}
	if userOrg == nil {
		return &types.Caller{Uin: claims.Uin, Kind: types.CallerKindUser, State: types.AuthStateFailed}, nil
	}
	return &types.Caller{
		Uin:   userOrg.ID,
		OrgID: userOrg.OrgID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, nil
}

func (p *builtinTokenParser) ParseWorker(ctx context.Context, tokenStr string) (*types.Caller, error) {
	claims, err := localauth.ParseWorkerToken(tokenStr, p.jwtSecret)
	if err != nil {
		return nil, err
	}
	return &types.Caller{
		OrgID:    claims.OrgID,
		WorkerID: claims.WorkerID,
		Kind:     types.CallerKindWorker,
		State:    types.AuthStateSucc,
	}, nil
}

func (p *builtinTokenParser) IssueWorker(ctx context.Context, orgID, workerID uint, bootstrapToken string) (string, int64, error) {
	bootstrapToken = strings.TrimSpace(bootstrapToken)
	if bootstrapToken == "" {
		return "", 0, accounterror.ErrWorkerBootstrapTokenInvalid
	}
	if err := p.validateBootstrapToken(ctx, orgID, workerID, bootstrapToken); err != nil {
		return "", 0, err
	}
	if err := p.validateWorker(ctx, orgID, workerID); err != nil {
		return "", 0, err
	}
	ttl := defaultWorkerTokenTTL
	if p.workerCfg != nil && p.workerCfg.TokenTTLSeconds > 0 {
		ttl = time.Duration(p.workerCfg.TokenTTLSeconds) * time.Second
	}
	return localauth.GenerateWorkerToken(orgID, workerID, p.jwtSecret, ttl)
}

func (p *builtinTokenParser) validateBootstrapToken(ctx context.Context, orgID, workerID uint, token string) error {
	if p.workerCfg != nil {
		for _, entry := range p.workerCfg.BootstrapTokens {
			if entry.OrgID != orgID || entry.WorkerID != workerID {
				continue
			}
			expected := strings.TrimSpace(entry.Token)
			if expected != "" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
				return nil
			}
		}
	}
	if p.workerStore != nil {
		deployment, err := p.workerStore.GetDeploymentByOrgWorkerID(ctx, orgID, workerID)
		if err != nil {
			return err
		}
		if deployment != nil && deployment.BootstrapTokenHash != "" {
			got := localauth.HashBootstrapToken(token)
			if subtle.ConstantTimeCompare([]byte(got), []byte(deployment.BootstrapTokenHash)) == 1 {
				return nil
			}
		}
	}
	return accounterror.ErrWorkerBootstrapTokenInvalid
}

func (p *builtinTokenParser) validateWorker(ctx context.Context, orgID, workerID uint) error {
	if p.workerStore == nil {
		return nil
	}
	deployment, err := p.workerStore.GetDeploymentByOrgWorkerID(ctx, orgID, workerID)
	if err != nil {
		return err
	}
	assistantID := workerID
	if deployment != nil {
		if deployment.OrgID != orgID {
			return accounterror.ErrWorkerOrgMismatch
		}
		assistantID = deployment.DigitalAssistantID
	}
	assistant, err := p.workerStore.GetDigitalAssistantByID(ctx, assistantID)
	if err != nil {
		return err
	}
	if assistant == nil {
		return accounterror.ErrWorkerNotFound
	}
	if assistant.OrgID != orgID {
		return accounterror.ErrWorkerOrgMismatch
	}
	if assistant.Status != "active" {
		return accounterror.ErrWorkerNotActive
	}
	return nil
}
