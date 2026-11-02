package service

import (
	"context"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

// ModelRuntimeObserver keeps deployment-specific status collection outside the model configuration manager.
type ModelRuntimeObserver interface {
	ModelRuntimes(context.Context, []types.LLMModelReference) []types.LLMModelRuntime
}

type modelRuntimeService struct {
	contract.LLMModelService
	observer ModelRuntimeObserver
}

// WithModelRuntime enriches authorized list results without weakening the existing organization checks.
func WithModelRuntime(service contract.LLMModelService, observer ModelRuntimeObserver) contract.LLMModelService {
	return &modelRuntimeService{LLMModelService: service, observer: observer}
}

func (s *modelRuntimeService) ListLLMModels(ctx context.Context, req *contract.ListLLMModelsRequest) (*contract.LLMModelList, error) {
	result, err := s.LLMModelService.ListLLMModels(ctx, req)
	if err != nil || result == nil || s.observer == nil {
		return result, err
	}
	refs := make([]types.LLMModelReference, len(result.Items))
	for i, model := range result.Items {
		refs[i] = types.LLMModelReference{ID: model.ID, OrgID: model.OrgID, Model: model.Model, BaseURL: model.BaseURL}
	}
	observations := s.observer.ModelRuntimes(ctx, refs)
	if len(observations) == len(result.Items) {
		for i := range result.Items {
			result.Items[i].Runtime = &observations[i]
		}
	}
	return result, nil
}
