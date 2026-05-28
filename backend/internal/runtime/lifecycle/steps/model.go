package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/gorm"
)

// ModelResolver 解析单次运行的具体模型配置。
type ModelResolver interface {
	ResolveModel(ctx context.Context, req *agent.RequestContext) (*agent.ModelOptions, error)
}

// DBModelResolver 从持久化的 LLM 模型记录中解析模型设置。
type DBModelResolver struct {
	db    *gorm.DB
	orgID uint
}

type ModelStep struct {
	Resolver ModelResolver
}

func (ModelStep) Name() string {
	return "model"
}

func (s ModelStep) Run(ctx context.Context, state *State) error {
	return EnsureModelConfig(ctx, state.Request, s.Resolver)
}

// NewDBModelResolver 创建一个由模型表支持的模型解析器。
func NewDBModelResolver(db *gorm.DB, orgID uint) *DBModelResolver {
	return &DBModelResolver{db: db, orgID: orgID}
}

// ResolveModel 根据持久化的模型表填充 req.Model。
func (r *DBModelResolver) ResolveModel(ctx context.Context, req *agent.RequestContext) (*agent.ModelOptions, error) {
	if req == nil {
		return nil, fmt.Errorf("request context is required")
	}
	if req.Model.Provider != "" && req.Model.Model != "" && req.Model.APIKey != "" {
		model := req.Model
		return &model, nil
	}

	if r == nil || r.orgID == 0 {
		return nil, nil
	}
	if r.db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}

	var (
		model *types.LLMModel
		err   error
	)
	if req.Model.ID > 0 {
		model, err = infradb.GetLLMModelByID(ctx, r.db, req.Model.ID)
	} else {
		model, err = infradb.GetDefaultLLMModel(ctx, r.db, r.orgID)
	}
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, fmt.Errorf("llm model not found")
	}
	if model.OrgID != r.orgID {
		return nil, fmt.Errorf("llm model does not belong to current org")
	}
	if model.Status != string(types.LLMModelStatusActive) {
		return nil, fmt.Errorf("llm model is inactive")
	}

	resolved := req.Model
	resolved.ID = model.ID
	resolved.Provider = model.Provider
	resolved.Model = model.ModelName
	resolved.APIKey = model.APIKeyEncrypted
	resolved.BaseURL = workerModelProxyBaseURL()
	if resolved.Temperature == 0 {
		resolved.Temperature = model.Temperature
	}
	return &resolved, nil
}

func workerModelProxyBaseURL() string {
	addr := strings.TrimSpace(identity.WorkerAddr())
	if addr == "" {
		return ""
	}
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return ensureV1Suffix(addr)
	}
	if strings.HasPrefix(addr, ":") {
		return ensureV1Suffix("http://127.0.0.1" + addr)
	}
	return ensureV1Suffix("http://" + addr)
}

func ensureV1Suffix(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

// EnsureModelConfig 在需要时将解析后的模型配置应用到 req。
func EnsureModelConfig(ctx context.Context, req *agent.RequestContext, resolver ModelResolver) error {
	if req == nil {
		return fmt.Errorf("request context is required")
	}
	if req.Model.Provider != "" && req.Model.Model != "" && req.Model.APIKey != "" {
		return nil
	}
	if resolver == nil {
		return nil
	}
	resolved, err := resolver.ResolveModel(ctx, req)
	if err != nil {
		return err
	}
	if resolved == nil {
		return nil
	}
	req.Model = *resolved
	return nil
}
