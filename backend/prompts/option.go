package prompts

import (
	"context"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

type RunOption func(ctx context.Context, cfg *config.LLMConfig)

func WithModel(model string) RunOption {
	return func(ctx context.Context, cfg *config.LLMConfig) { cfg.Model = model }
}

func WithProvider(provider string) RunOption {
	return func(ctx context.Context, cfg *config.LLMConfig) { cfg.Provider = provider }
}

func WithBaseURL(url string) RunOption {
	return func(ctx context.Context, cfg *config.LLMConfig) { cfg.BaseURL = url }
}

var defaultLLMConfigOption = func(ctx context.Context, cfg *config.LLMConfig) {
	defer func() {
		if r := recover(); r != nil {
			logs.WarnContextf(ctx, "[prompts] DB not available: %v", r)
		}
	}()

	orgID := types.SystemOrgID
	caller, _ := auth.FromContext(ctx)
	if caller != nil {
		orgID = caller.OrgID
	}

	lm, err := db.GetDefaultLLMModel(ctx, db.GetDB(), orgID)
	if err != nil {
		logs.ErrorContextf(ctx, "[prompts] get default LLM model failed: org_id=%d error=%v", orgID, err)
		return
	}
	if lm != nil {
		cfg.Provider = lm.Provider
		cfg.Model = lm.ModelName
		cfg.BaseURL = lm.BaseURL
		cfg.APIKey = lm.APIKeyEncrypted
	}
}
