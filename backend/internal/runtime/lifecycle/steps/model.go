package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
)

// modelStore caches the modelrouter store for this process.
// modelrouter.DefaultStore() returns the process-wide singleton shared by the
// lifecycle step (writer) and the model router handler (reader).
var modelStore = modelrouter.DefaultStore()

// It writes the real upstream configuration to modelrouter store and modifies the
// request's BaseURL to use the built-in worker model proxy.
type ModelStep struct{}

func (ModelStep) Name() string {
	return "model"
}

func (s ModelStep) Run(ctx context.Context, state *State) error {
	return initModelRouting(ctx, state.Request)
}

// initModelRouting validates and initializes model routing for the request.
func initModelRouting(_ context.Context, req *agent.RequestContext) error {
	if req == nil {
		return fmt.Errorf("request context is required")
	}

	// Validate required fields
	if strings.TrimSpace(req.Model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(req.Model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(req.Model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}

	// Write the original upstream config to modelrouter store
	// Keep the original BaseURL and BaseURLHasV1 from the request
	upstreamCfg := modelrouter.UpstreamConfig{
		ModelName:    strings.TrimSpace(req.Model.Model),
		Provider:     strings.TrimSpace(req.Model.Provider),
		BaseURL:      strings.TrimSpace(req.Model.BaseURL),
		BaseURLHasV1: req.Model.BaseURLHasV1,
		APIKey:       strings.TrimSpace(req.Model.APIKey),
		Temperature:  req.Model.Temperature,
	}
	modelStore.Put(upstreamCfg)

	// Change the request's BaseURL to use the built-in worker model proxy
	// Keep BaseURLHasV1 as the original value from the request
	req.Model.BaseURL = modelrouter.WorkerProxyBaseURL()

	return nil
}
