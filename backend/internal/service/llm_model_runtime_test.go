package service

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

type runtimeListFake struct {
	contract.LLMModelService
	err error
}

func (f runtimeListFake) ListLLMModels(context.Context, *contract.ListLLMModelsRequest) (*contract.LLMModelList, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &contract.LLMModelList{Items: []contract.LLMModel{{ID: 1, OrgID: 7, Model: "Qwen", BaseURL: "http://local"}}}, nil
}

type runtimeObserverFake struct {
	calls int
	refs  []types.LLMModelReference
}

func (f *runtimeObserverFake) ModelRuntimes(_ context.Context, refs []types.LLMModelReference) []types.LLMModelRuntime {
	f.calls++
	f.refs = refs
	return []types.LLMModelRuntime{{Source: "spark", State: "stopped"}}
}
func TestModelRuntimeOnlyObservesAuthorizedList(t *testing.T) {
	observer := &runtimeObserverFake{}
	denied := errors.New("permission denied")
	_, err := WithModelRuntime(runtimeListFake{err: denied}, observer).ListLLMModels(context.Background(), nil)
	if !errors.Is(err, denied) || observer.calls != 0 {
		t.Fatal("observed unauthorized list")
	}
	result, err := WithModelRuntime(runtimeListFake{}, observer).ListLLMModels(context.Background(), nil)
	if err != nil || observer.calls != 1 || result.Items[0].Runtime.State != "stopped" || observer.refs[0].OrgID != 7 {
		t.Fatalf("unexpected result %+v %v", result, err)
	}
}
