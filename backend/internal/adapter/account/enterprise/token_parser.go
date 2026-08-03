//go:build enterprise

package enterprise

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/config"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/gorm"
)

const defaultWorkerTokenTTL = 3650 * 24 * time.Hour

type enterpriseTokenParser struct {
	db           *gorm.DB
	iamCfg       *config.IAMConfig
	iamClient    *iamClient
	workerSecret string
	workerCfg    *config.WorkerAuthConfig
}

func NewTokenParser(database *gorm.DB, iamCfg *config.IAMConfig, env string, workerSecret string, workerCfg *config.WorkerAuthConfig) *enterpriseTokenParser {
	return &enterpriseTokenParser{
		db:           database,
		iamCfg:       iamCfg,
		iamClient:    newIAMClient(iamCfg, env),
		workerSecret: strings.TrimSpace(workerSecret),
		workerCfg:    workerCfg,
	}
}

func (p *enterpriseTokenParser) ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error) {
	claims, err := p.iamClient.verifyToken(ctx, tokenStr)
	if err != nil {
		return nil, err
	}
	if claims == nil {
		return &types.Caller{State: types.AuthStateFailed}, nil
	}
	return &types.Caller{
		Uin:   claims.Uin,
		OrgID: claims.OrgID,
		Kind:  types.CallerKindUser,
		State: types.AuthStateSucc,
	}, nil
}

func (p *enterpriseTokenParser) ParseWorker(ctx context.Context, tokenStr string) (*types.Caller, error) {
	claims, err := localauth.ParseWorkerToken(tokenStr, p.workerSecret)
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

func (p *enterpriseTokenParser) IssueWorker(ctx context.Context, orgID, workerID uint, bootstrapToken string) (string, int64, error) {
	if p.workerSecret == "" {
		return "", 0, accounterror.ErrJWTSecretRequired
	}

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
	return localauth.GenerateWorkerToken(orgID, workerID, p.workerSecret, ttl)
}

func (p *enterpriseTokenParser) validateBootstrapToken(ctx context.Context, orgID, workerID uint, token string) error {
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
	if p.db != nil {
		deployment, err := db.GetWorkerDeploymentByOrgWorkerID(ctx, p.db, orgID, workerID)
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

func (p *enterpriseTokenParser) validateWorker(ctx context.Context, orgID, workerID uint) error {
	if p.db == nil {
		return nil
	}
	deployment, err := db.GetWorkerDeploymentByOrgWorkerID(ctx, p.db, orgID, workerID)
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
	assistant, err := db.GetDigitalAssistantByID(ctx, p.db, assistantID)
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
